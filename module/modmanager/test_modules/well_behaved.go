package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"syscall"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/utils"
)

func MakeSelfOwnedFilesFunc(f func() error) error {
	return f()
}

func main() {
	utils.ContextualMain(mainWithArgs, logging.NewLogger("well_behaved"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	fmt.Println(os.Args)

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

	<-ctx.Done()
	logger.Info("Exiting:", ctx.Err())

	return nil
}
