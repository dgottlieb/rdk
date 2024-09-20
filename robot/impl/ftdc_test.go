package robotimpl

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
	"go.viam.com/rdk/services/datamanager"
	dmBuiltin "go.viam.com/rdk/services/datamanager/builtin"
	"go.viam.com/rdk/services/navigation"
	navBuiltin "go.viam.com/rdk/services/navigation/builtin"
	rutils "go.viam.com/rdk/utils"
	"go.viam.com/test"
	"go.viam.com/utils/pexec"
)

func TestFTDC(t *testing.T) {
	logger, _, registry := logging.NewObservedTestLoggerWithRegistry(t, "rdk")
	logger.SetLevel(logging.INFO)
	ctx := context.Background()

	cfg := &config.Config{
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

	robot := setupLocalRobot(t, ctx, cfg, logger)
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
	defer robot.Close(ctx)
}
