package modmanager

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/errors"
	pb "go.viam.com/api/module/v1"
	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/config"
	"go.viam.com/rdk/logging"
	modlib "go.viam.com/rdk/module"
	modmanageroptions "go.viam.com/rdk/module/modmanager/options"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/utils"
	"go.viam.com/test"
	"go.viam.com/utils/pexec"
	"go.viam.com/utils/testutils"
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
	if err != nil {
		fmt.Println("BuildTempModule Output:", string(out))
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
		mp.Stop()
		test.That(t, errors.New("Failed to dial to module"), test.ShouldBeNil)
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
		mp.Stop()
		test.That(t, errors.New("Failed to dial to module"), test.ShouldBeNil)
	}

	// We expect the module to crash after creating a connection. The moduleProcess logic will
	// restart in the background and pass back a new connection on the `conns` channel.
	select {
	case connGen := <-conns:
		test.That(t, connGen.Generation, test.ShouldEqual, 1)
		break
	case <-connTimeout.Done():
		mp.Stop()
		test.That(t, errors.New("Failed to redial after crash."), test.ShouldBeNil)
	}

	test.That(t, mp.Stop(), test.ShouldBeNil)
	// The test module explicitly exits with code 10.
	test.That(t, mp.exitCode(), test.ShouldEqual, 10)
}

func TestProcessCrashesBeforeUnixSocketCreation(t *testing.T) {
	ctx := context.Background()

	logger := logging.NewTestLogger(t)
	mp := setup(t, "crashes_before_unix_socket_creation", logger)

	conns, err := mp.Start()
	test.That(t, err, test.ShouldBeNil)

	// Initial connection will succeed.
	connTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-conns:
		test.That(t, errors.New("Incorrectly dialed to module that never listened to a socket."), test.ShouldBeNil)
	case <-connTimeout.Done():
	}

	// Assert that the program definitively stopped.
	test.That(t, mp.Stop(), test.ShouldBeNil)
	// Assert the hard coded exit code the program exits with.
	test.That(t, mp.exitCode(), test.ShouldEqual, 6)
}

func TestModuleIntegration(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)
	programPath := BuildTempModule(t, fmt.Sprintf("./test_modules/publish_models.go"))
	mod := &module{
		cfg: config.Module{
			Name:     "publish_modules",
			ExePath:  programPath,
			LogLevel: "debug",
			Type:     "local",
			TCPMode:  false,
		},
		dataDir:   "", //moduleDataDir,
		resources: make(map[resource.Name]*addedResource),
		logger:    logger.Sublogger("publish_modules"),
		ftdc:      nil, // mgr.ftdc,
	}

	fileSocketPath, err := modlib.CreateSocketAddress("./", "module_integration.sock")
	test.That(t, err, test.ShouldBeNil)
	// Cleanup previous tests.
	_ = os.Remove(fileSocketPath)

	// Assert file does not exist.
	_, err = os.Stat(fileSocketPath)
	test.That(t, os.IsNotExist(err), test.ShouldBeTrue)

	connTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conns, err := mod.startProcessNew(connTimeout, fileSocketPath, "", "")
	test.That(t, err, test.ShouldBeNil)

	conn := <-conns
	test.That(t, conn.Generation, test.ShouldEqual, 0)

	// Initialize the `sharedConn` + clients.
	mod.dialNew(conn.Conn)

	returnSocketPath := setupSocketWithRobot(t)
	err = mod.checkReady(ctx, returnSocketPath)
	test.That(t, err, test.ShouldBeNil)

	confProto, err := config.ComponentConfigToProto(&resource.Config{
		Name:      "shortName",
		API:       generic.API,
		Model:     resource.NewModel("rdk", "publish", "simple"),
		DependsOn: []string{},
		LogConfiguration: &resource.LogConfig{
			Level: logging.DEBUG,
		},
		Attributes: make(utils.AttributeMap),
	})
	test.That(t, err, test.ShouldBeNil)

	_, err = mod.client.AddResource(ctx, &pb.AddResourceRequest{Config: confProto, Dependencies: []string{}})
	test.That(t, err, test.ShouldBeNil)

	resClient, err := generic.NewClientFromConn(
		ctx,
		&mod.sharedConn,
		"",
		resource.Name{
			API:    generic.API,
			Remote: "",
			Name:   "shortName",
		},
		logger.Sublogger("shortNameClient"),
	)
	test.That(t, err, test.ShouldBeNil)

	res, err := resClient.DoCommand(ctx, map[string]any{})
	test.That(t, err, test.ShouldBeNil)
	test.That(t, res["command"].(string), test.ShouldEqual, "hello world")

	mod.killProcessGroupNew()
	test.That(t, mod.processNew.exitCode(), test.ShouldEqual, 0)
}

func TestModManagerIntegration(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t)
	programPath := BuildTempModule(t, fmt.Sprintf("./test_modules/publish_models.go"))

	returnSocketPath := setupSocketWithRobot(t)
	viamHomeTemp := t.TempDir()
	modmanager := setupModManager(t, ctx, returnSocketPath, logger.Sublogger("modmanager"),
		modmanageroptions.Options{UntrustedEnv: false, ViamHomeDir: viamHomeTemp})
	defer modmanager.Close(ctx)

	err := modmanager.AddNew(ctx, config.Module{
		Name:     "publish_modules",
		ExePath:  programPath,
		LogLevel: "debug",
		Type:     "local",
		TCPMode:  false,
	})
	test.That(t, err, test.ShouldBeNil)

	res, err := modmanager.AddResource(ctx,
		resource.Config{
			Name:      "shortName",
			API:       generic.API,
			Model:     resource.NewModel("rdk", "publish", "simple"),
			DependsOn: []string{},
			LogConfiguration: &resource.LogConfig{
				Level: logging.DEBUG,
			},
			Attributes: make(utils.AttributeMap),
		},
		[]string{},
	)
	test.That(t, err, test.ShouldBeNil)

	resp, err := res.DoCommand(ctx, map[string]any{})
	test.That(t, err, test.ShouldBeNil)
	test.That(t, resp["command"].(string), test.ShouldEqual, "hello world")

	resp, err = res.DoCommand(ctx, map[string]any{"kill": true})
	test.That(t, err, test.ShouldNotBeNil)

	testutils.WaitForAssertion(t, func(tb testing.TB) {
		resp, err = res.DoCommand(ctx, map[string]any{})
		test.That(tb, err, test.ShouldBeNil)
		// All assertions in a `WaitForAssertion` are executed. Explicitly check for avoid a bad map
		// access/value type assertion.
		if err == nil {
			test.That(tb, resp["command"].(string), test.ShouldEqual, "hello world")
		}
	})
}
