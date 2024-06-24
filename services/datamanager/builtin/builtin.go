// Package builtin contains a service type that can be used to capture data from a robot's components.
package builtin

import (
	"context"
	"runtime"
	"sync"
	"time"

	clk "github.com/benbjohnson/clock"
	"github.com/pkg/errors"

	"go.viam.com/rdk/data"
	"go.viam.com/rdk/internal/cloud"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/datamanager"
	"go.viam.com/rdk/services/datamanager/datasync"
	"go.viam.com/rdk/services/slam"
)

// Default time between disk size checks.
var filesystemPollInterval = 30 * time.Second

func init() {
	resource.RegisterService(
		datamanager.API,
		resource.DefaultServiceModel,
		resource.Registration[datamanager.Service, *Config]{
			// Wrap NewBuiltIn in a lambda that returns a `datamanager.Service`. `NewBuiltIn`
			// returns the structure that implements `datamanager.Service`. Not the interface
			// itself. Go generics aren't expressive enough to know this.
			Constructor: func(
				ctx context.Context,
				deps resource.Dependencies,
				conf resource.Config,
				logger logging.Logger,
			) (datamanager.Service, error) {
				return NewBuiltIn(ctx, deps, conf, logger)
			},
			WeakDependencies: []resource.Matcher{
				resource.TypeMatcher{Type: resource.APITypeComponentName},
				resource.SubtypeMatcher{Subtype: slam.SubtypeName},
			},
		})
}

var (
	clock          = clk.New()
	deletionTicker = clk.New()
)

// Config describes how to configure the service.
type Config struct {
	CaptureDir                  string   `json:"capture_dir"`
	CaptureDisabled             bool     `json:"capture_disabled"`
	Tags                        []string `json:"tags"`
	FileLastModifiedMillis      int      `json:"file_last_modified_millis"`
	SelectiveSyncerName         string   `json:"selective_syncer_name"`
	MaximumNumSyncThreads       int      `json:"maximum_num_sync_threads"`
	DeleteEveryNthWhenDiskFull  int      `json:"delete_every_nth_when_disk_full"`
	MaximumCaptureFileSizeBytes int64    `json:"maximum_capture_file_size_bytes"`
	SyncIntervalMins            float64  `json:"sync_interval_mins"`
	AdditionalSyncPaths         []string `json:"additional_sync_paths"`
	ScheduledSyncDisabled       bool     `json:"sync_disabled"`
}

// Validate returns components which will be depended upon weakly due to the above matcher.
func (c *Config) Validate(path string) ([]string, error) {
	return []string{cloud.InternalServiceName.String()}, nil
}

// builtIn initializes and orchestrates data capture collectors for registered component/methods.
type builtIn struct {
	resource.Named
	logger            logging.Logger
	lock              sync.Mutex
	backgroundWorkers sync.WaitGroup

	fileDeletionRoutineCancelFn   context.CancelFunc
	fileDeletionBackgroundWorkers *sync.WaitGroup
	captureManager                *data.CaptureManager
	syncManager                   *datasync.SyncManager
}

// NewBuiltIn returns a new data manager service for the given robot.
func NewBuiltIn(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	logger logging.Logger,
) (*builtIn, error) {
	return NewBuiltInWithSyncManager(ctx, deps, conf, datasync.NewManager(logger.Sublogger("sync"), clock), logger)
}

func NewBuiltInWithSyncManager(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	syncManager *datasync.SyncManager,
	logger logging.Logger,
) (*builtIn, error) {
	svc := &builtIn{
		Named:          conf.ResourceName().AsNamed(),
		logger:         logger,
		captureManager: data.NewCaptureManager(logger.Sublogger("capture"), clock),
		syncManager:    syncManager,
	}

	if err := svc.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}

	return svc, nil
}

func (svc *builtIn) Sync(ctx context.Context, extra map[string]interface{}) error {
	return svc.syncManager.Sync(ctx, extra)
}

// Close releases all resources managed by data_manager.
func (svc *builtIn) Close(ctx context.Context) error {
	svc.lock.Lock()
	if svc.fileDeletionRoutineCancelFn != nil {
		svc.fileDeletionRoutineCancelFn()
	}

	fileDeletionBackgroundWorkers := svc.fileDeletionBackgroundWorkers
	svc.lock.Unlock()
	svc.captureManager.Close()
	svc.syncManager.Close(ctx)
	svc.backgroundWorkers.Wait()

	if fileDeletionBackgroundWorkers != nil {
		fileDeletionBackgroundWorkers.Wait()
	}

	return nil
}

var grpcConnectionTimeout = 10 * time.Second

// Reconfigure updates the data manager service when the config has changed.
func (svc *builtIn) Reconfigure(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
) error {
	svc.lock.Lock()
	defer svc.lock.Unlock()
	svcConfig, err := resource.NativeConfig[*Config](conf)
	if err != nil {
		return err
	}

	cloudConnSvc, err := resource.FromDependencies[cloud.ConnectionService](deps, cloud.InternalServiceName)
	if err != nil {
		return err
	}

	dataConfig := data.Config{
		CaptureDisabled:             svcConfig.CaptureDisabled,
		CaptureDir:                  svcConfig.CaptureDir,
		Tags:                        svcConfig.Tags,
		MaximumCaptureFileSizeBytes: svcConfig.MaximumCaptureFileSizeBytes,
	}

	if err = svc.captureManager.Reconfigure(ctx, deps, conf, dataConfig); err != nil {
		svc.logger.Warnw("DataCapture reconfigure error", "err", err)
		return err
	}

	syncConfig := datasync.Config{
		ScheduledSyncDisabled:  svcConfig.ScheduledSyncDisabled,
		SyncIntervalMins:       svcConfig.SyncIntervalMins,
		FileLastModifiedMillis: svcConfig.FileLastModifiedMillis,
		Tags:                   svcConfig.Tags,
		MaximumNumSyncThreads:  svcConfig.MaximumNumSyncThreads,
		CloudConnSvc:           cloudConnSvc,
		CaptureDir:             svcConfig.CaptureDir,
		AdditionalSyncPaths:    svcConfig.AdditionalSyncPaths,
		SelectiveSyncerName:    svcConfig.SelectiveSyncerName,
	}
	if err = svc.syncManager.Reconfigure(ctx, deps, conf, syncConfig); err != nil {
		svc.logger.Warnw("DataSync reconfigure error", "err", err)
		return err
	}

	if svc.fileDeletionRoutineCancelFn != nil {
		svc.fileDeletionRoutineCancelFn()
	}
	if svc.fileDeletionBackgroundWorkers != nil {
		svc.fileDeletionBackgroundWorkers.Wait()
	}
	deleteEveryNthValue := defaultDeleteEveryNth
	if svcConfig.DeleteEveryNthWhenDiskFull != 0 {
		deleteEveryNthValue = svcConfig.DeleteEveryNthWhenDiskFull
	}

	if !svcConfig.CaptureDisabled {
	} else {
		svc.fileDeletionRoutineCancelFn = nil
		svc.fileDeletionBackgroundWorkers = nil
	}

	// if datacapture is enabled, kick off a go routine to check if disk space is filling due to
	// cached datacapture files
	if !svcConfig.CaptureDisabled {
		fileDeletionCtx, cancelFunc := context.WithCancel(context.Background())
		svc.fileDeletionRoutineCancelFn = cancelFunc
		svc.fileDeletionBackgroundWorkers = &sync.WaitGroup{}
		svc.fileDeletionBackgroundWorkers.Add(1)
		go pollFilesystem(fileDeletionCtx, svc.fileDeletionBackgroundWorkers,
			svc.captureManager.CaptureDir(), deleteEveryNthValue, svc.syncManager.Syncer(), svc.logger)
	}

	return nil
}

func pollFilesystem(ctx context.Context, wg *sync.WaitGroup, captureDir string,
	deleteEveryNth int, syncer datasync.Syncer, logger logging.Logger,
) {
	if runtime.GOOS == "android" {
		logger.Debug("file deletion if disk is full is not currently supported on Android")
		return
	}
	t := deletionTicker.Ticker(filesystemPollInterval)
	defer t.Stop()
	defer wg.Done()
	for {
		if err := ctx.Err(); err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Errorw("data manager context closed unexpectedly in filesystem polling", "error", err)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			logger.Debug("checking disk usage")
			shouldDelete, err := shouldDeleteBasedOnDiskUsage(ctx, captureDir, logger)
			if err != nil {
				logger.Warnw("error checking file system stats", "error", err)
			}
			if shouldDelete {
				start := time.Now()
				deletedFileCount, err := deleteFiles(ctx, syncer, deleteEveryNth, captureDir, logger)
				duration := time.Since(start)
				if err != nil {
					logger.Errorw("error deleting cached datacapture files", "error", err, "execution time", duration.Seconds())
				} else {
					logger.Infof("%v files have been deleted to avoid the disk filling up, execution time: %f", deletedFileCount, duration.Seconds())
				}
			}
		}
	}
}

func (svc *builtIn) sync(ctx context.Context) {
	svc.captureManager.FlushCollectors()
	svc.syncManager.Sync(ctx, nil)
}
