package modmanager

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/pkg/errors"
	"go.uber.org/multierr"
	pb "go.viam.com/api/module/v1"
	"go.viam.com/rdk/config"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	rutils "go.viam.com/rdk/utils"
)

func (mgr *Manager) addNew(ctx context.Context, conf config.Module, moduleLogger logging.Logger) error {
	_, exists := mgr.modules.Load(conf.Name)
	if exists {
		// Keeping this as a manager logger since it is dealing with manager behavior
		mgr.logger.CWarnw(ctx, "Not adding module that already exists", "module", conf.Name)
		return nil
	}

	exists, existingName := mgr.execPathAlreadyExists(&conf)
	if exists {
		return errors.Errorf("An existing module %s already exists with the same executable path as module %s", existingName, conf.Name)
	}

	var moduleDataDir string
	// only set the module data directory if the parent dir is present (which it might not be during tests)
	if mgr.moduleDataParentDir != "" {
		var err error
		// TODO: why isn't conf.Name being sanitized like PackageConfig.SanitizedName?
		moduleDataDir, err = rutils.SafeJoinDir(mgr.moduleDataParentDir, conf.Name)
		if err != nil {
			return err
		}
	}

	mod := &module{
		cfg:       conf,
		dataDir:   moduleDataDir,
		resources: map[resource.Name]*addedResource{},
		logger:    moduleLogger,
		ftdc:      mgr.ftdc,
	}
	mod.shutdownCtx, mod.restartCancel = context.WithCancel(mgr.restartCtx)

	if err := mgr.startModuleNew(ctx, mod); err != nil {
		return err
	}
	return nil
}

func (mgr *Manager) startModuleNew(ctx context.Context, mod *module) error {
	// create the module's data directory
	if mod.dataDir != "" {
		mod.logger.Debugf("Creating data directory %q for module %q", mod.dataDir, mod.cfg.Name)
		if err := os.MkdirAll(mod.dataDir, 0o750); err != nil {
			return errors.WithMessage(err, "error while creating data directory for module "+mod.cfg.Name)
		}
	}

	cleanup := rutils.SlowLogger(
		ctx, "Waiting for module to complete startup and registration", "module", mod.cfg.Name, mod.logger)
	defer cleanup()

	connCh, err := mod.startProcessNew(
		mgr.restartCtx,
		mgr.parentAddr(mod),
		mgr.viamHomeDir,
		mgr.packagesDir,
	)
	if err != nil {
		return errors.WithMessage(err, "error while starting module "+mod.cfg.Name)
	}

	firstLoadCh := make(chan struct{})
	firstLoadDo := sync.Once{}
	go func() {
		var latestConnGen ConnGeneration
		for {
			select {
			case connGen, more := <-connCh:
				if !more {
					return
				}

				if connGen.Generation == -1 {
					mod.cleanupAfterCrash(mgr)
					continue
				}

				if connGen.Generation > latestConnGen.Generation {
					latestConnGen = connGen
				}
			case <-mgr.restartCtx.Done():
				return
			}

			// New code: startModuleProcess will `dial` under the hood.
			mod.dialNew(latestConnGen.Conn)

			// Sends a ReadyRequest and waits on a ReadyResponse. The PeerConnection will async connect
			// after this, so long as the module supports it.
			if err := mod.checkReady(ctx, mgr.parentAddr(mod)); err != nil {
				mod.logger.Warnw("Error while waiting for module to be ready. Waiting for a new connection.",
					"module", mod.cfg.Name)
				continue
			}

			if pc := mod.sharedConn.PeerConn(); mgr.modPeerConnTracker != nil && pc != nil {
				mgr.modPeerConnTracker.Add(mod.cfg.Name, pc)
			}

			mod.logger.Infow("Module successfully started", "module", mod.cfg.Name)
			mod.registerResourceModels(mgr)
			firstLoadDo.Do(func() {
				mgr.modules.Store(mod.cfg.Name, mod)
				close(firstLoadCh)
			})

			mgr.readdResources(mod)
		}
	}()

	startupTimeout := rutils.GetModuleStartupTimeout(mod.logger)
	ctxTimeout, cancelFunc := context.WithTimeout(ctx, startupTimeout)
	defer cancelFunc()

	select {
	case <-firstLoadCh:
		break
	case <-ctxTimeout.Done():
		err = fmt.Errorf("Module startup timed out. Timeout: %v", startupTimeout)
	case <-mgr.restartCtx.Done():
		err = errors.New("Modmanager stopped. Module startup interrupted.")
	}

	if err != nil {
		mod.restartCancel()
		mod.stopProcess()
		return err
	}

	return nil
}

func (mgr *Manager) readdResources(mod *module) {
	mod.resourcesMu.Lock()
	defer mod.resourcesMu.Unlock()
	if len(mod.resources) == 0 {
		return
	}

	var orphanedResourceNames []resource.Name
	var restoredResourceNamesStr []string
	mod.logger.Info("DBG. Resources to re-add:", mod.resources)
	for name, res := range mod.resources {
		confProto, err := config.ComponentConfigToProto(&res.conf)
		if err != nil {
			mod.logger.Errorw(
				"Failed to re-add resource after module restarted due to config conversion error",
				"module",
				mod.cfg.Name,
				"resource",
				name.String(),
				"error",
				err,
			)
			orphanedResourceNames = append(orphanedResourceNames, name)
			continue
		}

		_, err = mod.client.AddResource(mgr.restartCtx, &pb.AddResourceRequest{Config: confProto, Dependencies: res.deps})
		if err != nil {
			mod.logger.Errorw(
				"Failed to re-add resource after module restarted",
				"module",
				mod.cfg.Name,
				"resource",
				name.String(),
				"error",
				err,
			)
			orphanedResourceNames = append(orphanedResourceNames, name)

			// At this point, the modmanager is no longer managing this resource and should remove it
			// from its state.
			mgr.rMap.Delete(name)
			delete(mod.resources, name)
			continue
		}

		restoredResourceNamesStr = append(restoredResourceNamesStr, name.String())
	}

	// `mgr.handleOrphanedResources` maps to `localRobot.handleOrphanedResources`
	if len(orphanedResourceNames) > 0 && mgr.handleOrphanedResources != nil {
		orphanedResourceNamesStr := make([]string, len(orphanedResourceNames))
		for _, n := range orphanedResourceNames {
			orphanedResourceNamesStr = append(orphanedResourceNamesStr, n.String())
		}

		// What does it mean to rebuild? Isn't that the same as calling `AddResource`??
		mod.logger.Warnw("Some resources failed to re-add after crashed module restart and will be rebuilt",
			"module", mod.cfg.Name,
			"resources_to_be_rebuilt", orphanedResourceNamesStr)
		mgr.handleOrphanedResources(mgr.restartCtx, orphanedResourceNames)
	}

	mod.logger.Infow("Module resources successfully re-added after module restart",
		"module", mod.cfg.Name,
		"resources", restoredResourceNamesStr)
	return
}

func (mgr *Manager) AddNew(ctx context.Context, confs ...config.Module) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if mgr.untrustedEnv {
		allowed, newConfs := checkIfAllowed(confs...)
		if !allowed {
			return errModularResourcesDisabled
		}
		// overwrite with just the modules we've allowed
		confs = newConfs
		mgr.logger.CWarnw(
			ctx, "Running in an untrusted environment; will only add some modules", "modules",
			confs)
	}

	var (
		wg   sync.WaitGroup
		errs = make([]error, len(confs))
		seen = make(map[string]struct{}, len(confs))
	)
	for i, conf := range confs {
		if _, dupe := seen[conf.Name]; dupe {
			continue
		}
		seen[conf.Name] = struct{}{}

		// The config was already validated, but we must check again before attempting to add.
		if err := conf.Validate(""); err != nil {
			mgr.logger.CErrorw(ctx, "Module config validation error; skipping", "module", conf.Name, "error", err)
			errs[i] = err
			continue
		}

		// setup valid, new modules in parallel
		wg.Add(1)
		go func(i int, conf config.Module) {
			defer wg.Done()
			moduleLogger := mgr.logger.Sublogger(conf.Name)

			moduleLogger.CInfow(ctx, "Now adding module", "module", conf.Name)
			err := mgr.addNew(ctx, conf, moduleLogger)
			if err != nil {
				moduleLogger.CErrorw(ctx, "Error adding module", "module", conf.Name, "error", err)
				errs[i] = err
				return
			}
		}(i, conf)
	}
	wg.Wait()

	combinedErr := multierr.Combine(errs...)
	if combinedErr == nil {
		var addedModNames []string
		for modName := range seen {
			addedModNames = append(addedModNames, modName)
		}
		mgr.logger.CInfow(ctx, "Modules successfully added", "modules", addedModNames)
	}
	return combinedErr
}
