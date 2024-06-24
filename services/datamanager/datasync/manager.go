package datasync

import (
	"context"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/benbjohnson/clock"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/internal/cloud"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/datamanager"
	"go.viam.com/rdk/services/datamanager/datacapture"
	"go.viam.com/rdk/utils"
)

var grpcConnectionTimeout = 10 * time.Second

// Default time to wait in milliseconds to check if a file has been modified.
const defaultFileLastModifiedMillis = 10000.0

type SyncManager struct {
	mu                sync.Mutex
	syncer            Syncer
	logger            logging.Logger
	clk               clock.Clock
	syncerConstructor SyncerConstructor

	syncDisabled           bool
	syncIntervalMins       float64
	captureDir             string
	additionalSyncPaths    []string
	fileLastModifiedMillis int
	filesToSync            chan string
	// Dan: Rename to performSyncSensor?
	syncSensor selectiveSyncer

	// New
	isAliveCtx        context.Context
	shutdownFn        context.CancelFunc
	backgroundWorkers sync.WaitGroup

	// Dead
	syncTicker *clock.Ticker
}

func (sm *SyncManager) Syncer() Syncer {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.syncer
}

type Config struct {
	FileLastModifiedMillis int
	MaximumNumSyncThreads  int
	CloudConnSvc           cloud.ConnectionService
	CaptureDir             string
	AdditionalSyncPaths    []string
	SelectiveSyncerName    string
	ScheduledSyncDisabled  bool
	SyncIntervalMins       float64
	Tags                   []string
}

func NewSyncManager(logger logging.Logger, clk clock.Clock) *SyncManager {
	isAliveCtx, shutdownFn := context.WithCancel(context.Background())
	ret := &SyncManager{
		isAliveCtx:        isAliveCtx,
		shutdownFn:        shutdownFn,
		logger:            logger,
		clk:               clk,
		syncerConstructor: NewSyncer,
		syncIntervalMins:  0,
		filesToSync:       make(chan string, 1000),
	}
	go ret.SyncIntervalWorker()

	return ret
}

func (sm *SyncManager) Reconfigure(ctx context.Context, deps resource.Dependencies, resConfig resource.Config, syncConfig Config) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var err error

	// Dan: Consider logging state transitions of these member variables.
	sm.syncDisabled = syncConfig.ScheduledSyncDisabled
	sm.syncIntervalMins = syncConfig.SyncIntervalMins
	sm.captureDir = syncConfig.CaptureDir
	sm.additionalSyncPaths = syncConfig.AdditionalSyncPaths
	sm.fileLastModifiedMillis = syncConfig.FileLastModifiedMillis

	if syncConfig.SelectiveSyncerName != "" {
		sm.syncSensor, err = sensor.FromDependencies(deps, syncConfig.SelectiveSyncerName)
		if err != nil {
			sm.syncSensor = neverSyncSensor{}
			sm.logger.CErrorw(
				ctx, "unable to initialize selective syncer; will not sync at all until fixed or removed from config", "error", err.Error())
		}
	} else {
		sm.syncSensor = nil
	}

	if sm.syncer == nil {
		sm.syncer, err = sm.syncerConstructor(sm.isAliveCtx, sm.filesToSync, sm.logger)
	}

	// Dan: TODO -- manage syncThreads here -- syncer will just deal with reading files/pushing to
	// web.
	if sm.syncer != nil {
		sm.syncer.Reconfigure(
			ctx,
			syncConfig.CloudConnSvc,
			syncConfig.CaptureDir,
			syncConfig.Tags,
		)
	}

	return nil
}

// readyToSync is a method for getting the bool reading from the selective sync sensor
// for determining whether the key is present and what its value is.
func readyToSync(ctx context.Context, selectiveSyncer selectiveSyncer, logger logging.Logger) bool {
	if selectiveSyncer == nil {
		// The config did not specify a selective syncer.
		return true
	}

	readings, err := selectiveSyncer.Readings(ctx, nil)
	if err != nil {
		logger.CErrorw(ctx, "error getting readings from selective syncer", "error", err.Error())
		return false
	}

	readyToSyncVal, ok := readings[datamanager.ShouldSyncKey]
	if !ok {
		logger.CErrorf(ctx, "value for should sync key %s not present in readings", datamanager.ShouldSyncKey)
		return false
	}

	readyToSync, err := utils.AssertType[bool](readyToSyncVal)
	if err != nil {
		logger.CErrorw(ctx, "error converting should sync key to bool", "key", datamanager.ShouldSyncKey, "error", err.Error())
		return false
	}

	return readyToSync
}

func (sm *SyncManager) Close(giveupCtx context.Context) {
	sm.shutdownFn()
	sm.backgroundWorkers.Wait()
	close(sm.filesToSync)
}

// TODO: Determine desired behavior if sync is disabled. Do we wan to allow manual syncs, then?
//       If so, how could a user cancel it?

// Sync performs a non-scheduled sync of the data in the capture directory.
// If automated sync is also enabled, calling Sync will upload the files,
// regardless of whether or not is the scheduled time.
func (sm *SyncManager) Sync(ctx context.Context, _ map[string]interface{}) error {
	sm.mu.Lock()
	fileLastModifiedMillis := sm.fileLastModifiedMillis
	syncPaths := sm.additionalSyncPaths
	syncer := sm.syncer
	sm.mu.Unlock()

	// Retrieve all files in capture dir and send them to the syncer
	getAllFilesToSync(ctx, syncPaths, fileLastModifiedMillis, syncer)

	return nil
}

func (sm *SyncManager) SyncIntervalWorker() {
	var ticker *clock.Ticker
	defer func() {
		fmt.Println("Worker quit")
		if ticker != nil {
			ticker.Stop()
		}
	}()

	var currSyncIntervalMins float64
	var tickerCh <-chan time.Time
	for {
		sm.mu.Lock()
		syncDisabled := sm.syncDisabled
		configSyncIntervalMins := sm.syncIntervalMins
		sm.mu.Unlock()

		if syncDisabled || configSyncIntervalMins < 1e-9 {
			// If sync is disabled, recheck config values roughly once cloud config refresh.
			configSyncIntervalMins = 0.1
		}

		if math.Abs(currSyncIntervalMins-configSyncIntervalMins) > 1e-9 {
			// If the sync interval changed, recreate the ticker with the new value. For floats we
			// check if the values are within some epsilon rather than using equality.
			intervalMillis := 60000.0 * configSyncIntervalMins
			ticker = sm.clk.Ticker(time.Duration(intervalMillis) * time.Millisecond)
			tickerCh = ticker.C

			currSyncIntervalMins = configSyncIntervalMins
		}

		select {
		case <-sm.isAliveCtx.Done():
			break
		case tm := <-tickerCh:
			sm.logger.Debugw("Datasync interval hit", "tickerTime", tm, "syncEnabled", !syncDisabled)
			if syncDisabled {
				continue
			}
			sm.uploadData()
		}
	}
}

func (sm *SyncManager) uploadData() {
	sm.mu.Lock()
	syncSensor := sm.syncSensor
	sm.mu.Unlock()

	if !readyToSync(sm.isAliveCtx, syncSensor, sm.logger) || isOffline(sm.isAliveCtx) {
		return
	}

	if isOffline(sm.isAliveCtx) {
		return
	}

	sm.Sync(sm.isAliveCtx, nil)
}

//nolint:errcheck,nilerr
func getAllFilesToSync(ctx context.Context, dirs []string, lastModifiedMillis int, syncer Syncer) {
	for _, dir := range dirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if ctx.Err() != nil {
				return filepath.SkipAll
			}
			if err != nil {
				return nil
			}

			// Do not sync the files in the corrupted data directory.
			if info.IsDir() && info.Name() == datamanager.FailedDir {
				return filepath.SkipDir
			}

			if info.IsDir() {
				return nil
			}
			// If a file was modified within the past lastModifiedMillis, do not sync it (data
			// may still be being written).
			timeSinceMod := time.Since(info.ModTime())
			// When using a mock clock in tests, this can be negative since the file system will still use the system clock.
			// Take max(timeSinceMod, 0) to account for this.
			if timeSinceMod < 0 {
				timeSinceMod = 0
			}
			isStuckInProgressCaptureFile := filepath.Ext(path) == datacapture.InProgressFileExt &&
				timeSinceMod >= defaultFileLastModifiedMillis*time.Millisecond
			isNonCaptureFileThatIsNotBeingWrittenTo := filepath.Ext(path) != datacapture.InProgressFileExt &&
				timeSinceMod >= time.Duration(lastModifiedMillis)*time.Millisecond
			isCompletedCaptureFile := filepath.Ext(path) == datacapture.FileExt
			if isCompletedCaptureFile || isStuckInProgressCaptureFile || isNonCaptureFileThatIsNotBeingWrittenTo {
				syncer.SendFileToSync(path)
			}
			return nil
		})
	}
}

func isOffline(ctx context.Context) bool {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var dialer net.Dialer
	_, err := dialer.DialContext(timeoutCtx, "tcp", "app.viam.com:443")
	// If there's an error, the system is likely offline.
	return err != nil
}

func (sm *SyncManager) SetSyncerConstructor(fn SyncerConstructor) {
	sm.syncerConstructor = fn
}

// Replace existing callers with Reconfigure + synconfig?
// func (sm *SyncManager) SetFileLastModifiedMillis(s int) {
//  	sm.fileLastModifiedMillis = s
// }

func (sm *SyncManager) SyncTicker() *clock.Ticker {
	return sm.syncTicker
}

// Replace with Reconfigure + synconfig?
// func (sm *SyncManager) MaxSyncThreads() int {
//  	return sm.maxSyncThreads
// }
