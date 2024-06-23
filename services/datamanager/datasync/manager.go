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
	"github.com/pkg/errors"
	v1 "go.viam.com/api/app/datasync/v1"
	"go.viam.com/rdk/components/sensor"
	"go.viam.com/rdk/internal/cloud"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/datamanager"
	"go.viam.com/rdk/services/datamanager/datacapture"
	"go.viam.com/rdk/utils"
	goutils "go.viam.com/utils"
)

var grpcConnectionTimeout = 10 * time.Second

// Default time to wait in milliseconds to check if a file has been modified.
const defaultFileLastModifiedMillis = 10000.0

// Dan: Remove in favor of a simple `sensor.Sensor`?
type selectiveSyncer interface {
	resource.Sensor
}

type SyncManager struct {
	mu                  sync.Mutex
	syncer              Syncer
	captureDir          string
	logger              logging.Logger
	clk                 clock.Clock
	syncerConstructor   SyncerConstructor
	filesToSync         chan string
	syncDisabled        bool
	syncIntervalMins    float64
	syncRoutineCancelFn context.CancelFunc
	backgroundWorkers   sync.WaitGroup

	// Dan: This now includes the capture dir. We should change the name to syncPaths.
	additionalSyncPaths []string

	// Dan: Rename to triggerSyncSensor
	syncSensor selectiveSyncer

	// New
	closeCtx context.Context
	closeFn  context.CancelFunc

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
	closeCtx, closeFn := context.WithCancel(context.Background())
	ret := &SyncManager{
		closeCtx:            closeCtx,
		closeFn:             closeFn,
		logger:              logger,
		clk:                 clk,
		syncerConstructor:   NewSyncer,
		syncIntervalMins:    0,
		additionalSyncPaths: []string{},
		filesToSync:         make(chan string, 1000),
	}
	go ret.SyncIntervalWorker()

	return ret
}

func (sm *SyncManager) Reconfigure(ctx context.Context, deps resource.Dependencies, resConfig resource.Config, syncConfig Config) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var err error

	sm.syncDisabled = syncConfig.ScheduledSyncDisabled
	sm.syncIntervalMins = syncConfig.SyncIntervalMins

	if sm.syncer == nil {
		sm.syncer, err = sm.syncerConstructor(sm.closeCtx, sm.filesToSync, sm.logger)
	}

	if sm.syncer != nil {
		sm.syncer.Reconfigure(syncConfig.CloudConnSvc, syncConfig.Tags, syncConfig.MaximumNumSyncThreads, syncConfig.CaptureDir)
	}

	syncPaths := append(syncConfig.AdditionalSyncPaths, syncConfig.CaptureDir)
	sm.additionalSyncPaths = syncPaths

	var syncSensor selectiveSyncer
	if syncConfig.SelectiveSyncerName != "" {
		sm.syncSensor, err = sensor.FromDependencies(deps, syncConfig.SelectiveSyncerName)
		if err != nil {
			sm.logger.CErrorw(
				ctx, "unable to initialize selective syncer; will not sync at all until fixed or removed from config", "error", err.Error())
		}
	} else {
		sm.syncSensor = nil
	}

	return nil
}

// readyToSync is a method for getting the bool reading from the selective sync sensor
// for determining whether the key is present and what its value is.
func readyToSync(ctx context.Context, s selectiveSyncer, logger logging.Logger) (readyToSync bool) {
	readyToSync = false
	readings, err := s.Readings(ctx, nil)
	if err != nil {
		logger.CErrorw(ctx, "error getting readings from selective syncer", "error", err.Error())
		return
	}
	readyToSyncVal, ok := readings[datamanager.ShouldSyncKey]
	if !ok {
		logger.CErrorf(ctx, "value for should sync key %s not present in readings", datamanager.ShouldSyncKey)
		return
	}
	readyToSyncBool, err := utils.AssertType[bool](readyToSyncVal)
	if err != nil {
		logger.CErrorw(ctx, "error converting should sync key to bool", "key", datamanager.ShouldSyncKey, "error", err.Error())
		return
	}
	readyToSync = readyToSyncBool
	return
}

func (sm *SyncManager) Close(giveupCtx context.Context) {
	sm.closeFn()
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.closeSyncer()
}

func (sm *SyncManager) closeSyncer() {
	// Must be holding lock
	sm.cancelSyncScheduler()
	if sm.syncer != nil {
		// If previously we were syncing, close the old syncer and cancel the old updateCollectors goroutine.
		sm.syncer.Close()
		close(sm.filesToSync)
		sm.syncer = nil
	}
	if sm.cloudConn != nil {
		goutils.UncheckedError(sm.cloudConn.Close())
	}
}

// initSyncer constructs Callers must serialize access to `initSyncer`.
func (sm *SyncManager) initSyncer(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, grpcConnectionTimeout)
	defer cancel()
	identity, conn, err := sm.cloudConnSvc.AcquireConnection(ctx)
	if errors.Is(err, cloud.ErrNotCloudManaged) {
		sm.logger.CDebug(ctx, "Using no-op sync manager when not cloud managed")
		sm.syncer = NewNoopManager()
	}
	if err != nil {
		return err
	}

	client := v1.NewDataSyncServiceClient(conn)
	sm.filesToSync = make(chan string, sm.maxSyncThreads)
	syncer, err := sm.syncerConstructor(identity, client, sm.logger, sm.captureDir, sm.maxSyncThreads, sm.filesToSync)
	if err != nil {
		return errors.Wrap(err, "failed to initialize new syncer")
	}
	sm.syncer = syncer
	return nil
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
		case <-sm.closeCtx.Done():
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

	if !readyToSync(sm.closeCtx, sm.syncSensor, sm.logger) || isOffline(sm.closeCtx) {
		return
	}

	if isOffline(sm.closeCtx) {
		return
	}

	sm.Sync(sm.closeCtx, nil)
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

// cancelSyncScheduler cancels the goroutine that calls Sync repeatedly if scheduled sync is enabled.
// It does not close the syncer or any in progress uploads.
func (sm *SyncManager) cancelSyncScheduler() {
	if sm.syncRoutineCancelFn != nil {
		sm.syncRoutineCancelFn()
		sm.syncRoutineCancelFn = nil
		// DATA-2664: A goroutine calling this method must currently be holding the data manager
		// lock. The `uploadData` background goroutine can also acquire the data manager lock prior
		// to learning to exit. Thus we release the lock such that the `uploadData` goroutine can
		// make progress and exit.
		sm.mu.Unlock()
		sm.backgroundWorkers.Wait()
		sm.mu.Lock()
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

// Replace with Reconfigure + synconfig?
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
