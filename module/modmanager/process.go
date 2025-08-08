package modmanager

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/logging"
	"go.viam.com/utils/pexec"
)

type ConnGeneration struct {
	Conn       net.Conn
	Generation int
}

type moduleProcess struct {
	conf   pexec.ProcessConfig
	connCh chan ConnGeneration
	logger logging.Logger

	wg        sync.WaitGroup
	isAlive   bool
	process   *processLifetime
	restartMu sync.Mutex
}

func NewModuleProcess(conf pexec.ProcessConfig, logger logging.Logger) *moduleProcess {
	return &moduleProcess{
		conf:    conf,
		connCh:  make(chan ConnGeneration),
		logger:  logger,
		isAlive: true,
	}
}

// Start returns immediately:
//   - If the error is nil, Start has started the process. The returned channel provides a connection
//     every time the module restarts.
//   - If the error is not nil, the module process cannot be run. A new process config is required.
//
// Dan: Because I'm lazy, connections may be returned out of order in crash-heavy scenarios. The
// caller must only replace their connection handle if the next `ConnGeneration` has a higher
// `Generation` number.
func (mp *moduleProcess) Start() (<-chan ConnGeneration, error) {
	const firstGenerationId = 0
	generationLogger := mp.logger.Sublogger(fmt.Sprintf("generation_%v", firstGenerationId))
	mp.process = newProcessLifetime(firstGenerationId, generationLogger)

	socketFilename := mp.conf.Args[0]
	if err := mp.process.Start(socketFilename, mp.conf, mp.connCh); err != nil {
		return nil, err
	}

	// `process.Start` returned without an error. `mp.process.cmd` is guaranteed to be non-nil.
	mp.wg.Add(1)
	go func() {
		defer mp.wg.Done()
		nextGenerationId := firstGenerationId + 1
		for {
			mp.process.wait()

			// If the `moduleProcess` is stopped, `isAlive` is guaranteed to be set to false before
			// the `cmd` is interrupted.
			mp.restartMu.Lock()
			if !mp.isAlive {
				mp.restartMu.Unlock()
				return
			}

			generationLogger = mp.logger.Sublogger(fmt.Sprintf("generation_%v", nextGenerationId))
			mp.process = newProcessLifetime(nextGenerationId, generationLogger)
			nextGenerationId++

			// TODO: Describe what a `Start` failure is and what our options are. Note the
			// inconsistency between an initial `Start` error for `firstGenerationId`
			// (moduleProcess.Start returns an error) versus this `Start` failing (we retry).
			//
			// We do this because we don't have a good way to inform the higher level caller that
			// something that used to work is now failing at a much earlier step.
			//
			// Also, if firstGenerationId's `Start` succeeds, and this retry `Start` fails, the
			// underlying problem may become reversed without us doing anything. E.g: a `Start` can
			// fail because a binary was moved out of place. It's possible it will be moved back
			// into place.
			//
			// Broadly, we're providing a contract that if `moduleProcess.Start` returns success,
			// `moduleProcess` will try its hardest to keep the process running. Even if that's
			// looking to be unlikely. It's too late now to change our minds.
			//
			// It's not a correctness problem to keep trying. It's an inefficiency. Ideally we'd
			// only retry when we have reason to believe a retry will succeed.
			if startErr := mp.process.Start(socketFilename, mp.conf, mp.connCh); startErr != nil {
				generationLogger.Warn("Error starting process. Retrying.")
			}
			mp.restartMu.Unlock()

			// After every crash, lets have some backoff. In case a module program spits out a bunch
			// of output and immediately crashes. We don't want to unnecessarily spam the viam logs.
			time.Sleep(time.Second)
		}
	}()

	return mp.connCh, nil
}

// Stop returns an error if the underlying module process may still be running.
func (mp *moduleProcess) Stop() error {
	// Take the `restartMu` to take ownership of the `process` value. Such that we can `Stop` it
	// without racing with the restart checker.
	mp.restartMu.Lock()
	mp.isAlive = false
	// Save the error for a return value. In case we're not sure the process has exited.
	stopErr := mp.process.Stop()
	mp.restartMu.Unlock()

	// Inform the `moduleProcess` owner that there will be no more connections to the module
	// process.
	close(mp.connCh)

	// Wait on the process restart goroutine to exit.
	mp.wg.Wait()
	return stopErr
}

// exitCode must be called after `Stop` succeeds. Otherwise Go will not observe/fill in the exit
// code to return. It instead returns -1.
//
// This method is a helper exposed for internal tests to assert/confirm expected behavior. In the
// general case the caller does not know the runtime stability of the underlying process. And it
// will receive the exit code of an arbitrary process lifetime generation.
func (mp *moduleProcess) exitCode() int {
	// Because `Stop` was already called, the `process` variable is safe to read without a mutex.
	return mp.process.cmd.ProcessState.ExitCode()
}

// CheckSocketOwner verifies that UID of a filepath/socket matches the current process's UID.
func CheckSocketOwner(address string) error {
	// check that the module socket has the same ownership as our process
	info, err := os.Stat(address)
	if err != nil {
		return err
	}

	sockUID := int(info.Sys().(*syscall.Stat_t).Uid)
	if serverUID := os.Getuid(); serverUID != sockUID {
		sockUser, err := user.LookupId(strconv.Itoa(sockUID))
		if err != nil {
			return errors.Wrap(err, "error looking up user")
		}

		serverUser, err := user.LookupId(strconv.Itoa(serverUID))
		if err != nil {
			return errors.Wrap(err, "error looking up user")
		}

		return errors.Errorf("socket owned by %s while process is owned by %s", sockUser.Name, serverUser.Name)
	}

	return nil
}
