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

func TestRunWellBehaved(t *testing.T) {
	ctx := context.Background()
	_ = ctx

	logger := logging.NewTestLogger(t)
	programPath := BuildTempModule(t, "./test_modules/well_behaved.go")

	fileSocketPath, err := modlib.CreateSocketAddress("./", "well-behaved")
	test.That(t, err, test.ShouldBeNil)
	// Cleanup previous tests.
	_ = os.Remove(fileSocketPath)

	// Assert file does not exist.
	_, err = os.Stat(fileSocketPath)
	test.That(t, os.IsNotExist(err), test.ShouldBeTrue)

	logger.Info("Socket:", fileSocketPath)
	mp := NewModuleProcess(pexec.ProcessConfig{
		ID:           "id",
		Name:         programPath,
		Args:         []string{fileSocketPath},
		CWD:          "./",
		Log:          true,
		StdOutLogger: logger.Sublogger("stdout"),
		StdErrLogger: logger.Sublogger("stderr"),
	}, logger)
	conns, err := mp.Start()
	test.That(t, err, test.ShouldBeNil)

	select {
	case <-conns:
		break
	case <-time.After(5 * time.Second):
		logger.Error("Failed to dial to module.")
		mp.Stop()
		t.FailNow()
	}

	test.That(t, mp.Stop(), test.ShouldBeNil)
}
