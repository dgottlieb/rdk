package main

import (
	"context"
	_ "embed"
	"fmt"

	"go.viam.com/rdk/components/gripper"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/spatialmath"
)

// testGripperModel is a modular gripper whose distinguishing feature is that it implements
// `Geometries`. It lets tests assert that a modular component's geometry is incorporated into the
// robot's frame system (so it can be modeled as part of the arm / an obstacle for motion planning).
var testGripperModel = resource.NewModel("rdk", "test", "gripper")

// testGripperModelJSON is a zero-DoF kinematic model carrying a single box geometry. The geometry
// must travel as model JSON (not a programmatically-built model) because GetKinematics only
// serializes a model's original file bytes across the module boundary — a model built in code has
// no original file and would arrive on the parent as an empty model.
//
//go:embed gripper_model.json
var testGripperModelJSON []byte

func newTestGripper(
	_ context.Context, _ resource.Dependencies, conf resource.Config, _ logging.Logger,
) (resource.Resource, error) {
	model, err := referenceframe.UnmarshalModelJSON(testGripperModelJSON, conf.ResourceName().ShortName())
	if err != nil {
		return nil, err
	}
	// Cache the model's geometries at its (empty) zero-DoF input so Geometries is a cheap lookup.
	gif, err := model.Geometries(make([]referenceframe.Input, len(model.DoF())))
	if err != nil {
		return nil, err
	}
	return &testGripper{
		Named:      conf.ResourceName().AsNamed(),
		model:      model,
		geometries: gif.Geometries(),
	}, nil
}

type testGripper struct {
	resource.Named
	resource.TriviallyReconfigurable
	resource.TriviallyCloseable
	model      referenceframe.Model
	geometries []spatialmath.Geometry
}

var _ gripper.Gripper = &testGripper{}

// Geometries returns the gripper's geometry, expressed relative to the gripper's own frame.
func (g *testGripper) Geometries(_ context.Context, _ map[string]interface{}) ([]spatialmath.Geometry, error) {
	return g.geometries, nil
}

// Kinematics returns the zero-DoF model carrying the gripper's geometry.
func (g *testGripper) Kinematics(_ context.Context) (referenceframe.Model, error) {
	return nil, fmt.Errorf("for now Kinematics errors to work around bug?")
}

// CurrentInputs returns the (empty) inputs for this zero-DoF gripper.
func (g *testGripper) CurrentInputs(_ context.Context) ([]referenceframe.Input, error) {
	return []referenceframe.Input{}, nil
}

// GoToInputs is a no-op for this zero-DoF gripper.
func (g *testGripper) GoToInputs(_ context.Context, _ ...[]referenceframe.Input) error {
	return nil
}

// Open is a no-op.
func (g *testGripper) Open(_ context.Context, _ map[string]interface{}) error {
	return fmt.Errorf("obstacle can't open")
}

// Grab is a no-op that never grabs anything.
func (g *testGripper) Grab(_ context.Context, _ map[string]interface{}) (bool, error) {
	return false, fmt.Errorf("obstacle can't grab")
}

// IsHoldingSomething always reports an empty holding status.
func (g *testGripper) IsHoldingSomething(_ context.Context, _ map[string]interface{}) (gripper.HoldingStatus, error) {
	return gripper.HoldingStatus{false, nil}, nil
}

// Stop is a no-op.
func (g *testGripper) Stop(_ context.Context, _ map[string]interface{}) error {
	return nil
}

// IsMoving always reports false.
func (g *testGripper) IsMoving(_ context.Context) (bool, error) {
	return false, nil
}
