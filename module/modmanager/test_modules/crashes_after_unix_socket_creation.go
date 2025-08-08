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
	utils.ContextualMain(mainWithArgs, logging.NewLogger("crashes_after_unix_socket_creation"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)

	socketFilename := os.Args[1]
	lis, err := net.Listen("unix", socketFilename)
	_ = lis
	if err != nil {
		logger.Warnw("Error listening", "file", socketFilename, "err", err)
		return err
	} else {
		logger.Info("Listening successfully", "file", socketFilename)
	}

	_, err = lis.Accept()
	if err == nil {
		panic("intentional crash")
	}

	return nil
}
