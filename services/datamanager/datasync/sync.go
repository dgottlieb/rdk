// Package datasync contains interfaces for syncing data from robots to the app.viam.com cloud.
package datasync

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/atomic"
	v1 "go.viam.com/api/app/datasync/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"go.viam.com/rdk/internal/cloud"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/services/datamanager"
	"go.viam.com/rdk/services/datamanager/datacapture"
)

var (
	// InitialWaitTimeMillis defines the time to wait on the first retried upload attempt.
	InitialWaitTimeMillis = atomic.NewInt32(1000)
	// RetryExponentialFactor defines the factor by which the retry wait time increases.
	RetryExponentialFactor = atomic.NewInt32(2)
	// OfflineWaitTimeSeconds defines the amount of time to wait to retry if the machine is offline.
	OfflineWaitTimeSeconds = atomic.NewInt32(60)
	maxRetryInterval       = 24 * time.Hour
)

// MaxParallelSyncRoutines is the maximum number of sync goroutines that can be running at once.
const MaxParallelSyncRoutines = 1000

// Syncer is responsible for enqueuing files in captureDir and uploading them to the cloud.
type Syncer interface {
	Reconfigure(
		ctx context.Context,
		cloudConn cloud.ConnectionService,
		captureDir string,
		tags []string,
	)
	SyncFile(ctx context.Context, path string)
	MarkInProgress(path string) bool
	UnmarkInProgress(path string)
}

type config struct {
	client     v1.DataSyncServiceClient
	partID     string
	captureDir string
	fileTags   []string

	// For diffing/regenerating `client` and `partID`
	cloudConn cloud.ConnectionService
}

// syncer is responsible for uploading files in captureDir to the cloud.
type syncer struct {
	config atomic.Pointer[config]

	progressLock sync.Mutex
	inProgress   map[string]bool

	logger logging.Logger
}

// NewSyncer returns a new syncer.
func NewSyncer(logger logging.Logger) Syncer {
	ret := syncer{
		logger:     logger,
		inProgress: make(map[string]bool),
	}
	ret.config.Store(&config{})

	return &ret
}

func (s *syncer) Reconfigure(
	ctx context.Context,
	cloudConn cloud.ConnectionService,
	captureDir string,
	tags []string,
) {
	oldConfig := s.config.Load()

	// Make a shallow copy of the existing config.
	newConfig := *oldConfig
	if oldConfig.cloudConn != cloudConn {
		ctx, cancel := context.WithTimeout(ctx, grpcConnectionTimeout)
		defer cancel()

		partID, conn, err := cloudConn.AcquireConnection(ctx)
		if err != nil {
			return
		}

		newConfig.cloudConn = cloudConn
		newConfig.partID = partID
		newConfig.client = v1.NewDataSyncServiceClient(conn)
	}

	newConfig.captureDir = captureDir
	newConfig.fileTags = tags

	s.config.Store(&newConfig)
}

func (s *syncer) SyncFile(ctx context.Context, path string) {
	// If the file is already being synced, do not kick off a new goroutine.
	// The goroutine will again check and return early if sync is already in progress.
	if !s.MarkInProgress(path) {
		return
	}
	defer s.UnmarkInProgress(path)
	//nolint:gosec
	osFile, err := os.Open(path)
	if err != nil {
		// Don't log if the file does not exist, because that means it was successfully synced and deleted
		// in between paths being built and this executing.
		if !errors.Is(err, os.ErrNotExist) {
			s.logger.Errorw("error opening file", "error", err)
		}
		return
	}

	if datacapture.IsDataCaptureFile(osFile) {
		captureFile, err := datacapture.ReadFile(osFile)
		if err != nil {
			if err = osFile.Close(); err != nil {
				s.logger.Errorw("error closing data capture file", "err", err)
			}

			captureDir := s.config.Load().captureDir
			if err := moveFailedData(osFile.Name(), captureDir); err != nil {
				s.logger.Errorw("error moving corrupted data", "file", osFile.Name(), "err", err)
			}
			return
		}
		s.syncDataCaptureFile(ctx, captureFile)
	} else {
		s.syncArbitraryFile(ctx, osFile)
	}
}

func (s *syncer) syncDataCaptureFile(ctx context.Context, file *datacapture.File) {
	config := s.config.Load()
	client, partID, captureDir := config.client, config.partID, config.captureDir

	uploadErr := exponentialRetry(
		ctx,
		func(ctx context.Context) error {
			err := uploadDataCaptureFile(ctx, client, file, partID)
			if err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Errorw("error uploading file", "file", file.GetPath(), "err", err)
			}
			return err
		},
	)
	if uploadErr != nil {
		err := file.Close()
		if err != nil {
			s.logger.Errorw("error closing data capture file", "file", file.GetPath(), "err", err)
		}

		if !isRetryableGRPCError(uploadErr) {
			if err := moveFailedData(file.GetPath(), captureDir); err != nil {
				s.logger.Errorw("error moving corrupted data", "file", file.GetPath(), "err", err)
			}
		}
		return
	}
	if err := file.Delete(); err != nil {
		s.logger.Errorw("error deleting data capture file")
		return
	}
}

func (s *syncer) syncArbitraryFile(ctx context.Context, file *os.File) {
	config := s.config.Load()
	client, partID, fileTags := config.client, config.partID, config.fileTags

	uploadErr := exponentialRetry(
		ctx,
		func(ctx context.Context) error {
			uploadErr := uploadArbitraryFile(ctx, client, file, partID, fileTags)
			if uploadErr != nil && !errors.Is(uploadErr, context.Canceled) {
				s.logger.Errorw("error uploading file", "file", file.Name(), "err", uploadErr)
			}

			if !isRetryableGRPCError(uploadErr) {
				if err := moveFailedData(file.Name(), path.Dir(file.Name())); err != nil {
					s.logger.Errorw("error moving corrupted data", "file", file.Name(), "err", err)
				}
			}
			return uploadErr
		})
	if uploadErr != nil {
		err := file.Close()
		if err != nil {
			s.logger.Errorw("error closing data capture file", "err", err)
		}
		return
	}
	if err := os.Remove(file.Name()); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Errorw("error deleting file", "file", file.Name(), "err", err)
		return
	}
}

// MarkInProgress marks path as in progress in s.inProgress. It returns true if it changed the progress status,
// or false if the path was already in progress.
func (s *syncer) MarkInProgress(path string) bool {
	s.progressLock.Lock()
	defer s.progressLock.Unlock()
	if s.inProgress[path] {
		s.logger.Debugw("File already in progress, trying to mark it again", "file", path)
		return false
	}
	s.inProgress[path] = true
	return true
}

// UnmarkInProgress unmarks a path as in progress in s.inProgress.
func (s *syncer) UnmarkInProgress(path string) {
	s.progressLock.Lock()
	defer s.progressLock.Unlock()
	delete(s.inProgress, path)
}

// exponentialRetry calls fn and retries with exponentially increasing waits from initialWait to a
// maximum of maxRetryInterval.
func exponentialRetry(cancelCtx context.Context, fn func(cancelCtx context.Context) error) error {
	// Only create a ticker and enter the retry loop if we actually need to retry.
	var err error
	if err = fn(cancelCtx); err == nil {
		return nil
	}

	// Don't retry non-retryable errors.
	if !isRetryableGRPCError(err) {
		return err
	}

	// First call failed, so begin exponentialRetry with a factor of RetryExponentialFactor
	nextWait := time.Millisecond * time.Duration(InitialWaitTimeMillis.Load())
	ticker := time.NewTicker(nextWait)
	for {
		if err := cancelCtx.Err(); err != nil {
			return err
		}

		select {
		// If cancelled, return nil.
		case <-cancelCtx.Done():
			ticker.Stop()
			return cancelCtx.Err()
			// Otherwise, try again after nextWait.
		case <-ticker.C:
			if err := fn(cancelCtx); err != nil {
				// If error, retry with a new nextWait.
				ticker.Stop()
				nextWait = getNextWait(nextWait, isOfflineGRPCError(err))
				ticker = time.NewTicker(nextWait)
				continue
			}
			// If no error, return.
			ticker.Stop()
			return nil
		}
	}
}

func isOfflineGRPCError(err error) bool {
	errStatus := status.Convert(err)
	return errStatus.Code() == codes.Unavailable
}

// isRetryableGRPCError returns true if we should retry syncing and otherwise
// returns false so that the data gets moved to the corrupted data directory.
func isRetryableGRPCError(err error) bool {
	errStatus := status.Convert(err)
	return errStatus.Code() != codes.InvalidArgument && !errors.Is(err, proto.Error)
}

// moveFailedData takes any data that could not be synced in the parentDir and
// moves it to a new subdirectory "failed" that will not be synced.
func moveFailedData(path, parentDir string) error {
	// Remove the parentDir part of the path to the corrupted data
	relativePath, err := filepath.Rel(parentDir, path)
	if err != nil {
		return errors.Wrapf(err, fmt.Sprintf("error getting relative path of corrupted data: %s", path))
	}
	// Create a new directory parentDir/corrupted/pathToFile
	newDir := filepath.Join(parentDir, datamanager.FailedDir, filepath.Dir(relativePath))
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		return errors.Wrapf(err, fmt.Sprintf("error making new directory for corrupted data: %s", path))
	}
	// Move the file from parentDir/pathToFile/file.ext to parentDir/corrupted/pathToFile/file.ext
	newPath := filepath.Join(newDir, filepath.Base(path))
	if err := os.Rename(path, newPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return errors.Wrapf(err, fmt.Sprintf("error moving corrupted data: %s", path))
		}
	}
	return nil
}

func getNextWait(lastWait time.Duration, isOffline bool) time.Duration {
	if lastWait == time.Duration(0) {
		return time.Millisecond * time.Duration(InitialWaitTimeMillis.Load())
	}

	if isOffline {
		return time.Second * time.Duration(OfflineWaitTimeSeconds.Load())
	}

	nextWait := lastWait * time.Duration(RetryExponentialFactor.Load())
	if nextWait > maxRetryInterval {
		return maxRetryInterval
	}
	return nextWait
}
