package data

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/services/datamanager"
	"go.viam.com/rdk/services/datamanager/datacapture"
	"go.viam.com/rdk/utils"
)

// Threshold number of files to check if sync is backed up (defined as >1000 files).
var minNumFiles = 1000

// Default time between checking and logging number of files in capture dir.
var captureDirSizeLogInterval = time.Minute

type CaptureDirPoller struct {
	logger logging.Logger

	mu         sync.Mutex
	captureDir string
	workers    utils.StoppableWorkers
}

func NewCaptureDirPoller(logger logging.Logger) *CaptureDirPoller {
	return &CaptureDirPoller{logger: logger}
}

func (poller *CaptureDirPoller) Reconfigure(ctx context.Context, captureDir string) error {
	poller.mu.Lock()
	defer poller.mu.Unlock()
	if captureDir == poller.captureDir {
		return nil
	}

	poller.captureDir = captureDir
	if poller.workers != nil {
		poller.workers.Stop()
	}

	poller.workers = utils.NewStoppableWorkers(func(stopCtx context.Context) {
		t := time.NewTicker(captureDirSizeLogInterval)
		defer t.Stop()
		for {
			select {
			case <-stopCtx.Done():
				return
			case <-t.C:
				numFiles := countCaptureDirFiles(stopCtx, captureDir)
				if numFiles > minNumFiles {
					poller.logger.Infof("Capture dir contains %d files", numFiles)
				}
			}
		}
	})

	return nil
}

func (poller *CaptureDirPoller) Close() {
	poller.mu.Lock()
	defer poller.mu.Unlock()
	poller.workers.Stop()
}

func countCaptureDirFiles(ctx context.Context, captureDir string) int {
	numFiles := 0
	//nolint:errcheck
	_ = filepath.Walk(captureDir, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		//nolint:nilerr
		if err != nil {
			return nil
		}

		// Do not count the files in the corrupted data directory.
		if info.IsDir() && info.Name() == datamanager.FailedDir {
			return filepath.SkipDir
		}

		if info.IsDir() {
			return nil
		}
		// this is intentionally not doing as many checkas as getAllFilesToSync because
		// this is intended for debugging and does not need to be 100% accurate.
		isCompletedCaptureFile := filepath.Ext(path) == datacapture.FileExt
		if isCompletedCaptureFile {
			numFiles++
		}
		return nil
	})
	return numFiles
}
