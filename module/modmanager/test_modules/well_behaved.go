package main

import (
	"context"
	"net"
	"os"
	"syscall"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/utils"
)

func main() {
	utils.ContextualMain(mainWithArgs, logging.NewLogger("well_behaved"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)

	socketFilename := os.Args[1]
	lis, err := net.Listen("unix", socketFilename)
	if err != nil {
		logger.Warnw("Error listening", "file", socketFilename, "err", err)
		return err
	} else {
		logger.Info("Listening successfully", "file", socketFilename)
	}

	_, err = lis.Accept()
	if err != nil {
		panic(err)
	} else {
		logger.Info("Connection received")
	}

	<-ctx.Done()
	logger.Info("Exiting:", ctx.Err())

	return nil
}
