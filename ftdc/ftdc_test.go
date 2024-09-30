// Robots depend on the `ftdc` package, therefore this test cannot depend on robots. Use this test
// file as black box testing of how robots use ftdc.
package ftdc_test

import (
	"context"
	"testing"
	"time"

	"go.viam.com/rdk/components/base"
	"go.viam.com/rdk/components/base/wheeled"
	"go.viam.com/rdk/components/motor"
	fakemotor "go.viam.com/rdk/components/motor/fake"
	"go.viam.com/rdk/config"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	robotimpl "go.viam.com/rdk/robot/impl"
	"go.viam.com/rdk/services/datamanager"
	dmBuiltin "go.viam.com/rdk/services/datamanager/builtin"
	"go.viam.com/rdk/services/navigation"
	navBuiltin "go.viam.com/rdk/services/navigation/builtin"
	rutils "go.viam.com/rdk/utils"
	"go.viam.com/test"
	"go.viam.com/utils/pexec"
)

func TestRobotFTDC(t *testing.T) {
	logger, _, registry := logging.NewObservedTestLoggerWithRegistry(t, "rdk")
	logger.SetLevel(logging.INFO)
	ctx := context.Background()

	// for ftdcConfig := range []string{"json", "custom", "mdb"} {
	for _, ftdcConfig := range []string{"json"} {
		cfg := &config.Config{
			FTDC: ftdcConfig,
			Components: []resource.Config{
				{
					Name:  "myBase",
					API:   base.API,
					Model: wheeled.Model,
					Attributes: rutils.AttributeMap{
						"motorL": "motor1",
						"motorR": "motor2",
					},
					ConvertedAttributes: &wheeled.Config{
						WidthMM:              10,
						WheelCircumferenceMM: 75,
						Left:                 []string{"motor1"},
						Right:                []string{"motor2"},
					},
				},
				{
					Name:  "motor1",
					API:   motor.API,
					Model: fakemotor.Model,
					ConvertedAttributes: &fakemotor.Config{
						TicksPerRotation: 100,
						MaxRPM:           1000,
					},
				},
				{
					Name:  "motor2",
					API:   motor.API,
					Model: fakemotor.Model,
					ConvertedAttributes: &fakemotor.Config{
						TicksPerRotation: 100,
						MaxRPM:           1000,
					},
				},
			},
			Processes: []pexec.ProcessConfig{
				{
					ID:      "1",
					Name:    "bash",
					Args:    []string{"-c", "echo heythere"},
					Log:     true,
					OneShot: true,
				},
			},
			Services: []resource.Config{
				{
					Name:                "fake1",
					API:                 datamanager.API,
					Model:               resource.DefaultServiceModel,
					ConvertedAttributes: &dmBuiltin.Config{},
				},
				{
					Name:  "builtin",
					API:   navigation.API,
					Model: resource.DefaultServiceModel,
					ConvertedAttributes: &navBuiltin.Config{
						BaseName: "myBase",
					},
				},
			},
			LogConfig: []logging.LoggerPatternConfig{
				logging.LoggerPatternConfig{
					Pattern: "rdk.ftdc",
					Level:   "DEBUG",
				},
			},
			Network:             config.NetworkConfig{},
			Auth:                config.AuthConfig{},
			DisablePartialStart: false,
		}
		config.UpdateLoggerRegistryFromConfig(registry, cfg, logger)

		// `setupLocalRobot` sets up a test handler to close the robot on test exit. Avoid
		// double-closing.
		robot := robotimpl.SetupLocalRobot(t, ctx, cfg, logger)
		done := make(chan struct{})
		go func() {
			wb, err := base.FromRobot(robot, "myBase")
			test.That(t, err, test.ShouldBeNil)
			err = wb.MoveStraight(ctx, 100, 40, nil)
			test.That(t, err, test.ShouldBeNil)
			close(done)
		}()
		time.Sleep(5 * time.Second)
		<-done
	}
}
