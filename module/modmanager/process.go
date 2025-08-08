package modmanager

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/logging"
	"go.viam.com/utils"
	"go.viam.com/utils/pexec"
)

type moduleProcess struct {
	conf    pexec.ProcessConfig
	cmd     *exec.Cmd
	connCh  chan net.Conn
	workers *utils.StoppableWorkers
	logger  logging.Logger
}

func NewModuleProcess(conf pexec.ProcessConfig, logger logging.Logger) *moduleProcess {
	return &moduleProcess{
		conf:   conf,
		connCh: make(chan net.Conn),
		logger: logger,
	}
}

// Start returns immediately:
//   - If the error is nil, Start has started the process. The returned channel provides a connection
//     every time the module restarts.
//   - If the error is not nil, the module process cannot be run. A new process config is required.
func (mp *moduleProcess) Start() (<-chan net.Conn, error) {
	socketFilename := mp.conf.Args[0]
	mp.cmd = exec.Command(mp.conf.Name, mp.conf.Args...)
	stdout, err := mp.cmd.StdoutPipe()
	if err != nil {
		// stop(mp.process)
		return nil, err
	}

	mp.workers = utils.NewBackgroundStoppableWorkers(func(ctx context.Context) {
		stdoutReader := bufio.NewReader(stdout)
		for {
			line, _, err := stdoutReader.ReadLine()
			if errors.Is(err, io.EOF) {
				return
			}

			mp.conf.StdOutLogger.Info(string(line))
		}
	})

	if err := mp.cmd.Start(); err != nil {
		return nil, err
	}

	mp.workers.Add(func(ctx context.Context) {
		for ctx.Err() == nil {
			if err := CheckSocketOwner(socketFilename); err != nil {
				fmt.Println("Owner error:", err)
			} else {
				mp.logger.Infow("Socket owned", "file", socketFilename)
				conn, err := net.Dial("unix", socketFilename)
				if err != nil {
					mp.logger.Warnw("Error dialing to socket file", "file", socketFilename, "err", err)
				} else {
					mp.logger.Infow("Successfully dialed socket file", "file", socketFilename)
					mp.connCh <- conn
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	return mp.connCh, nil
}

func (mp *moduleProcess) Stop() error {
	fmt.Println("Interrupting")
	mp.cmd.Process.Signal(os.Interrupt)
	mp.cmd.WaitDelay = time.Second
	stopErr := mp.cmd.Wait()
	mp.workers.Stop()
	return stopErr
}

func (mp *moduleProcess) ExitCode() int {
	return mp.cmd.ProcessState.ExitCode()
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
