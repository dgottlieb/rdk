package main

import (
	"context"
	"fmt"
	"os"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/utils"
)

func main() {
	utils.ContextualMain(mainWithArgs, module.NewLoggerFromArgs("publush_models"))
}

func mainWithArgs(ctx context.Context, args []string, logger logging.Logger) error {
	logger.Debug("debug mode enabled")
	var err error
	myMod, err := module.NewModuleFromArgs(ctx)
	if err != nil {
		return err
	}

	publishSimpleModel := resource.NewModel("rdk", "publish", "simple")
	resource.RegisterComponent(
		generic.API,
		publishSimpleModel,
		resource.Registration[resource.Resource, *publishSimpleConfig]{
			Constructor: newPublishSimple,
		})
	err = myMod.AddModelFromRegistry(ctx, generic.API, resource.NewModel("rdk", "publish", "simple"))
	if err != nil {
		return err
	}

	if err = myMod.Start(ctx); err != nil {
		logger.Error("Failed to start module:", err)
	}

	<-ctx.Done()
	return nil
}

type publishSimple struct {
	deps     resource.Dependencies
	baseConf resource.Config
	conf     *publishSimpleConfig
	logger   logging.Logger
}

type publishSimpleConfig struct {
	Arg string `json:"arg"`
}

// Validate ensures that `Arg1` is a non-empty string.
// Validation error will stop the associated resource from building.
func (cfg *publishSimpleConfig) Validate(path string) ([]string, []string, error) {
	if cfg.Arg != "" {
		return nil, nil, fmt.Errorf("Expected empty args. Received: %v", cfg.Arg)
	}

	// there are no dependencies for this model, so we return an empty list of strings
	return []string{}, nil, nil
}

func newPublishSimple(ctx context.Context,
	deps resource.Dependencies,
	baseConf resource.Config,
	logger logging.Logger,
) (resource.Resource, error) {
	conf, err := resource.NativeConfig[*publishSimpleConfig](baseConf)
	if err != nil {
		return nil, err
	}

	return &publishSimple{
		deps:     deps,
		baseConf: baseConf,
		conf:     conf,
		logger:   logger,
	}, nil
}

// Get the Name of the resource.
func (ps *publishSimple) Name() resource.Name {
	return ps.baseConf.ResourceName()
}

// Reconfigure must reconfigure the resource atomically and in place. If this
// cannot be guaranteed, then usage of AlwaysRebuild or TriviallyReconfigurable
// is permissible.
func (ps *publishSimple) Reconfigure(ctx context.Context, deps resource.Dependencies, conf resource.Config) error {
	return resource.NewMustRebuildError(conf.ResourceName())
}

// DoCommand sends/receives arbitrary data
func (ps *publishSimple) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	if _, exists := cmd["kill"]; exists {
		ps.logger.Info("Kill command. Exiting.")
		os.Exit(1)
	}

	return map[string]any{"command": "hello world"}, nil
}

// Close must safely shut down the resource and prevent further use.
// Close must be idempotent.
// Later reconfiguration may allow a resource to be "open" again.
func (ps *publishSimple) Close(ctx context.Context) error {
	return nil
}
