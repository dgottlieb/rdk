package datasync

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
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
	"go.viam.com/utils/rpc"
)

var grpcConnectionTimeout = 10 * time.Second

// Default time to wait in milliseconds to check if a file has been modified.
const defaultFileLastModifiedMillis = 10000.0

// Dan: Remove in favor of a simple `sensor.Sensor`?
type selectiveSyncer interface {
	resource.Sensor
}

type SyncManager struct {
	mu                     sync.Mutex
	syncer                 Syncer
	captureDir             string
	logger                 logging.Logger
	clk                    clock.Clock
	selectiveSyncEnabled   bool
	syncerConstructor      SyncerConstructor
	filesToSync            chan string
	maxSyncThreads         int
	syncDisabled           bool
	syncIntervalMins       float64
	syncRoutineCancelFn    context.CancelFunc
	tags                   []string
	fileLastModifiedMillis int
	backgroundWorkers      sync.WaitGroup

	// Dan: This now includes the capture dir. We should change the name to syncPaths.
	additionalSyncPaths []string
	syncTicker          *clock.Ticker

	// Dan: Rename to triggerSyncSensor
	syncSensor   selectiveSyncer
	cloudConnSvc cloud.ConnectionService
	cloudConn    rpc.ClientConn
}

func (sm *SyncManager) Syncer() Syncer {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.syncer
}

func NewSyncManager(logger logging.Logger, clk clock.Clock) *SyncManager {
	return &SyncManager{
		logger:                 logger,
		clk:                    clk,
		fileLastModifiedMillis: defaultFileLastModifiedMillis,
		selectiveSyncEnabled:   false,
		syncerConstructor:      NewSyncer,
		syncIntervalMins:       0,
		additionalSyncPaths:    []string{},
		tags:                   []string{},
	}
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

func (sm *SyncManager) Reconfigure(ctx context.Context, deps resource.Dependencies, resConfig resource.Config, syncConfig Config) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var err error

	// fmt.Println("SyncManager.Reconfigure")

	// Syncer should be reinitialized if the max sync threads are updated in the config
	newMaxSyncThreadValue := MaxParallelSyncRoutines
	if syncConfig.MaximumNumSyncThreads != 0 {
		newMaxSyncThreadValue = syncConfig.MaximumNumSyncThreads
	}
	reinitSyncer := sm.cloudConnSvc != syncConfig.CloudConnSvc || sm.maxSyncThreads != syncConfig.MaximumNumSyncThreads
	sm.cloudConnSvc = syncConfig.CloudConnSvc

	syncPaths := append(syncConfig.AdditionalSyncPaths, syncConfig.CaptureDir)
	sm.additionalSyncPaths = syncPaths

	fileLastModifiedMillis := syncConfig.FileLastModifiedMillis
	if fileLastModifiedMillis <= 0 {
		fileLastModifiedMillis = defaultFileLastModifiedMillis
	}

	var syncSensor selectiveSyncer
	if syncConfig.SelectiveSyncerName != "" {
		sm.selectiveSyncEnabled = true
		syncSensor, err = sensor.FromDependencies(deps, syncConfig.SelectiveSyncerName)
		if err != nil {
			sm.logger.CErrorw(
				ctx, "unable to initialize selective syncer; will not sync at all until fixed or removed from config", "error", err.Error())
		}
	} else {
		sm.selectiveSyncEnabled = false
	}
	if sm.syncSensor != syncSensor {
		sm.syncSensor = syncSensor
	}

	syncConfigUpdated := sm.syncDisabled != syncConfig.ScheduledSyncDisabled || sm.syncIntervalMins != syncConfig.SyncIntervalMins ||
		!reflect.DeepEqual(sm.tags, syncConfig.Tags) || sm.fileLastModifiedMillis != fileLastModifiedMillis ||
		sm.maxSyncThreads != newMaxSyncThreadValue

	// fmt.Println("  SyncConfigUpdated:", syncConfigUpdated, "WasDisabled:", sm.syncDisabled, "IsDisabled:", syncConfig.ScheduledSyncDisabled)
	// fmt.Println(sm.syncDisabled, syncConfig.ScheduledSyncDisabled, sm.syncIntervalMins, syncConfig.SyncIntervalMins,
	//  	!reflect.DeepEqual(sm.tags, syncConfig.Tags), sm.fileLastModifiedMillis, fileLastModifiedMillis,
	//  	sm.maxSyncThreads, newMaxSyncThreadValue)
	if syncConfigUpdated {
		sm.syncDisabled = syncConfig.ScheduledSyncDisabled
		sm.syncIntervalMins = syncConfig.SyncIntervalMins
		sm.tags = syncConfig.Tags
		sm.fileLastModifiedMillis = fileLastModifiedMillis
		sm.maxSyncThreads = newMaxSyncThreadValue

		sm.cancelSyncScheduler()
		if !sm.syncDisabled && sm.syncIntervalMins != 0.0 {
			if sm.syncer == nil {
				if err := sm.initSyncer(ctx); err != nil {
					return err
				}
			} else if reinitSyncer {
				sm.closeSyncer()
				if err := sm.initSyncer(ctx); err != nil {
					return err
				}
			}
			sm.syncer.SetArbitraryFileTags(sm.tags)
			sm.startSyncScheduler(sm.syncIntervalMins)
		} else {
			if sm.syncTicker != nil {
				sm.syncTicker.Stop()
				sm.syncTicker = nil
			}
			sm.closeSyncer()
		}
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
	sm.cloudConn = conn
	return nil
}

// TODO: Determine desired behavior if sync is disabled. Do we wan to allow manual syncs, then?
//       If so, how could a user cancel it?

// Sync performs a non-scheduled sync of the data in the capture directory.
// If automated sync is also enabled, calling Sync will upload the files,
// regardless of whether or not is the scheduled time.
func (sm *SyncManager) Sync(ctx context.Context, _ map[string]interface{}) error {
	sm.mu.Lock()
	if sm.syncer == nil {
		err := sm.initSyncer(ctx)
		if err != nil {
			sm.mu.Unlock()
			return err
		}
	}
	fileLastModifiedMillis := sm.fileLastModifiedMillis
	syncPaths := sm.additionalSyncPaths
	syncer := sm.syncer
	sm.mu.Unlock()

	// Retrieve all files in capture dir and send them to the syncer
	getAllFilesToSync(ctx, syncPaths, fileLastModifiedMillis, syncer)

	return nil
}

func (sm *SyncManager) uploadData(cancelCtx context.Context, intervalMins float64) {
	// time.Duration loses precision at low floating point values, so turn intervalMins to milliseconds.
	intervalMillis := 60000.0 * intervalMins
	// The ticker must be created before uploadData returns to prevent race conditions between clock.Ticker and
	// clock.Add in sync_test.go.
	sm.syncTicker = sm.clk.Ticker(time.Millisecond * time.Duration(intervalMillis))
	sm.backgroundWorkers.Add(1)
	goutils.PanicCapturingGo(func() {
		defer sm.backgroundWorkers.Done()
		defer sm.syncTicker.Stop()

		for {
			if err := cancelCtx.Err(); err != nil {
				if !errors.Is(err, context.Canceled) {
					sm.logger.Errorw("data manager context closed unexpectedly", "error", err)
				}
				return
			}

			select {
			case <-cancelCtx.Done():
				return
			case <-sm.syncTicker.C:
				sm.mu.Lock()
				if sm.syncer != nil {
					// If selective sync is disabled, sync. If it is enabled, check the condition below.
					shouldSync := !sm.selectiveSyncEnabled
					// If selective sync is enabled and the sensor has been properly initialized,
					// try to get the reading from the selective sensor that indicates whether to sync
					if sm.syncSensor != nil && sm.selectiveSyncEnabled {
						shouldSync = readyToSync(cancelCtx, sm.syncSensor, sm.logger)
					}
					sm.mu.Unlock()

					if !isOffline() && shouldSync {
						sm.Sync(cancelCtx, nil)
					}
				} else {
					sm.mu.Unlock()
				}
			}
		}
	})
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

// startSyncScheduler starts the goroutine that calls Sync repeatedly if scheduled sync is enabled.
func (sm *SyncManager) startSyncScheduler(intervalMins float64) {
	cancelCtx, fn := context.WithCancel(context.Background())
	sm.syncRoutineCancelFn = fn
	sm.uploadData(cancelCtx, intervalMins)
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

func isOffline() bool {
	timeout := 5 * time.Second
	_, err := net.DialTimeout("tcp", "app.viam.com:443", timeout)
	// If there's an error, the system is likely offline.
	return err != nil
}

func (sm *SyncManager) SetSyncerConstructor(fn SyncerConstructor) {
	sm.syncerConstructor = fn
}

func (sm *SyncManager) SetFileLastModifiedMillis(s int) {
	sm.fileLastModifiedMillis = s
}

func (sm *SyncManager) SyncTicker() *clock.Ticker {
	return sm.syncTicker
}

func (sm *SyncManager) MaxSyncThreads() int {
	return sm.maxSyncThreads
}
