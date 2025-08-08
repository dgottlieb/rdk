package modmanager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"go.viam.com/rdk/logging"
	modlib "go.viam.com/rdk/module"
	"go.viam.com/test"
	"go.viam.com/utils/pexec"
)

func BuildTempModule(tb testing.TB, modFile string) string {
	base := filepath.Base(modFile) // `./dir/file.go` -> `file.go`
	exeName := base[:len(base)-len(filepath.Ext(base))]
	// fmt.Println("Base:", filepath.Base(modFile))
	// fmt.Println("Ext:", filepath.Ext(modFile))
	// fmt.Println("Dir:", filepath.Dir(modFile))
	// fmt.Println("ExeName:", exeName)
	exePath := filepath.Join(tb.TempDir(), exeName)

	//nolint:gosec
	builder := exec.Command("go", "build", "-o", exePath, base)
	builder.Dir = filepath.Dir(modFile)

	out, err := builder.CombinedOutput()
	fmt.Println("Output:", string(out))
	if err != nil {
		tb.Error(err)
	}

	if tb.Failed() {
		tb.Fatalf("failed to build temporary module for testing")
	}

	return exePath
}

func setup(t *testing.T, testModuleNoDotGo string, logger logging.Logger) *moduleProcess {
	programPath := BuildTempModule(t, fmt.Sprintf("./test_modules/%v.go", testModuleNoDotGo))

	fileSocketPath, err := modlib.CreateSocketAddress("./", testModuleNoDotGo)
	test.That(t, err, test.ShouldBeNil)
	// Cleanup previous tests.
	_ = os.Remove(fileSocketPath)

	// Assert file does not exist.
	_, err = os.Stat(fileSocketPath)
	test.That(t, os.IsNotExist(err), test.ShouldBeTrue)

	logger.Info("Socket:", fileSocketPath)
	return NewModuleProcess(pexec.ProcessConfig{
		ID:           "id",
		Name:         programPath,
		Args:         []string{fileSocketPath},
		CWD:          "./",
		Log:          true,
		StdOutLogger: logger.Sublogger("stdout"),
		StdErrLogger: logger.Sublogger("stderr"),
	}, logger)
}

func TestProcessWellBehaved(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)
	mp := setup(t, "well_behaved", logger)

	conns, err := mp.Start()
	test.That(t, err, test.ShouldBeNil)

	connTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case connGen := <-conns:
		test.That(t, connGen.Generation, test.ShouldEqual, 0)
		break
	case <-connTimeout.Done():
		logger.Error("Failed to dial to module.")
		mp.Stop()
		t.FailNow()
	}

	test.That(t, mp.Stop(), test.ShouldBeNil)
	test.That(t, mp.exitCode(), test.ShouldEqual, 0)
}

func TestProcessCrashesAfterUnixSocketCreation(t *testing.T) {
	ctx := context.Background()

	logger := logging.NewTestLogger(t)
	mp := setup(t, "crashes_after_unix_socket_creation", logger)

	conns, err := mp.Start()
	test.That(t, err, test.ShouldBeNil)

	// Initial connection will succeed.
	connTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case connGen := <-conns:
		test.That(t, connGen.Generation, test.ShouldEqual, 0)
		break
	case <-connTimeout.Done():
		logger.Error("Failed to dial to module.")
		mp.Stop()
		t.FailNow()
	}

	// We expect the module to crash after creating a connection. The moduleProcess logic will
	// restart in the background and pass back a new connection on the `conns` channel.
	select {
	case connGen := <-conns:
		test.That(t, connGen.Generation, test.ShouldEqual, 1)
		break
	case <-connTimeout.Done():
		logger.Error("Failed to redial after crash.")
		mp.Stop()
		t.FailNow()
	}

	test.That(t, mp.Stop(), test.ShouldBeNil)
	// The test module explicitly exits with code 10.
	test.That(t, mp.exitCode(), test.ShouldEqual, 10)
}
