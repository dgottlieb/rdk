package modmanager

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/logging"
	"go.viam.com/utils"
	"go.viam.com/utils/pexec"
)

type processLifetime struct {
	generationId int
	cmd          *exec.Cmd

	// See `wait()` function documentation.
	waitMu  sync.Mutex
	waitErr error

	workers *utils.StoppableWorkers
	logger  logging.Logger
}

func newProcessLifetime(generationId int, logger logging.Logger) *processLifetime {
	return &processLifetime{
		generationId: generationId,
		workers:      utils.NewBackgroundStoppableWorkers(),
		logger:       logger,
	}
}

func (pl *processLifetime) Start(socketFilename string, conf pexec.ProcessConfig, onConn chan<- ConnGeneration) error {
	// Cleanup previous socket.
	_ = os.Remove(socketFilename)

	// Assert file does not exist.
	_, err := os.Stat(socketFilename)
	if err == nil {
		return fmt.Errorf("Socket still exists after removal attempt. Filename: %q", socketFilename)
	}

	// After removing the socket file, we can kick off a goroutine to connect to the module.
	pl.workers.Add(func(ctx context.Context) {
		for {
			// Either the viam-server is shutting down, or a new lifetime was started. No need to
			// keep trying.
			if ctx.Err() != nil {
				return
			}

			// We're waiting for the module to start listening on the
			// `socketFilename`. `CheckSocketOwner` essentially errors if the file has not yet been
			// created.
			if err := CheckSocketOwner(socketFilename); err != nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			pl.logger.Infow("Socket owned", "file", socketFilename)
			conn, err := net.Dial("unix", socketFilename)
			if err != nil {
				pl.logger.Warnw("Error dialing to socket file", "file", socketFilename, "err", err)
			} else {
				pl.logger.Infow("Successfully dialed socket file", "file", socketFilename)
				onConn <- ConnGeneration{conn, pl.generationId}
				return
			}
		}
	})

	pl.cmd = exec.Command(conf.Name, conf.Args...)
	stdout, err := pl.cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := pl.cmd.StderrPipe()
	if err != nil {
		return err
	}
	// TODO: Create stderr read loop.
	_ = stderr

	pl.workers.Add(func(ctx context.Context) {
		stdoutReader := bufio.NewReader(stdout)
		for {
			line, _, err := stdoutReader.ReadLine()
			if errors.Is(err, io.EOF) {
				return
			} else if err != nil {
				// Dan: When modules crash, I also see errors such as `*fs.PathError`. With a string
				// value of `read |0: file already closed`. I expect all errors here are
				// terminal. But choosing to log these to satisfy my curiosity.
				//
				// Can run `TestCrashesAfterUnixSocketCreation` to reproduce.
				conf.StdErrLogger.Debugf("Error on readline. Type: %T Err: %v", err, err)
				return
			}

			conf.StdOutLogger.Info(string(line))
		}
	})

	if startErr := pl.cmd.Start(); startErr != nil {
		pl.Stop()
		return startErr
	}

	return nil
}

// wait is a wrapper around `exec.Cmd.Wait()`. We want to wait on a process to exit for two
// purposes:
//   - When a module is explicitly stopped, we want to wait for the process to exit before
//     returning.
//   - A "restart watcher" waits for modules to crash such that it can restart them. It uses `wait`
//     to know when to restart.
//
// Go's `Cmd.Wait` unfortunately allows exactly one call into `Wait`. We provide this wrapper such
// that multiple readers/watchers can wait on a process.
func (pl *processLifetime) wait() error {
	pl.waitMu.Lock()
	defer pl.waitMu.Unlock()
	if pl.waitErr != nil {
		return pl.waitErr
	}

	// Wait, with a timeout, for the program to exit.
	pl.cmd.WaitDelay = 10 * time.Second

	pl.waitErr = pl.cmd.Wait()
	return pl.waitErr
}

// Stop returns an error if the process may still be running.
func (pl *processLifetime) Stop() error {
	// Send a signal to the program.
	pl.cmd.Process.Signal(os.Interrupt)

	// Save the error. An error here may mean the program is still running.
	stopErr := pl.wait()

	// Stop the background workers. This can include the connection making goroutine and the logging
	// goroutine.
	pl.workers.Stop()

	// Only return an error if the program may still be running.
	//
	// Dan: I think this only happens if we hit the `WaitDelay`
	if errors.Is(stopErr, exec.ErrWaitDelay) {
		return stopErr
	}

	return nil
}
