package armplanning

import (
	"context"
	"testing"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/rdk/utils"
	"go.viam.com/test"
)

// TestPlanningWithIntermediateFrame tests that the motion planner can plan for a frame
// attached to an intermediate joint of a flattened model (not the end-effector).
func TestPlanningWithIntermediateFrame(t *testing.T) {
	logger := logging.NewTestLogger(t)

	// Load UR5e arm model
	ur5e, err := referenceframe.ParseModelJSONFile(utils.ResolveFile("components/arm/fake/kinematics/ur5e.json"), "ur5e")
	test.That(t, err, test.ShouldBeNil)

	// Build FS via NewFrameSystem so flattening occurs.
	// Attach a tool to "ur5e:forearm_link" which is after the first 3 joints.
	armLIF := referenceframe.NewLinkInFrame(referenceframe.World, spatialmath.NewZeroPose(), "ur5e", nil)
	logger.Infof("FrameConfig: %+v\nModel: %+v\n", armLIF, ur5e)

	toolLIF := referenceframe.NewLinkInFrame("ur5e:forearm_link", spatialmath.NewZeroPose(), "tool", nil)

	parts := []*referenceframe.FrameSystemPart{
		{FrameConfig: armLIF, ModelFrame: ur5e},
	}
	fs, err := referenceframe.NewFrameSystem("test", parts, []*referenceframe.LinkInFrame{toolLIF})
	logger.Info("Frames:")
	for fname, _ := range fs.Frames() {
		logger.Info("  ", fname)
	}
	logger.Info("Parents:")
	for fname, parent := range fs.Parents() {
		logger.Info("  ", fname, "->", parent)
	}

	test.That(t, err, test.ShouldBeNil)

	// Start every joint at 0.5 rad.
	startInputs := referenceframe.NewZeroInputs(fs)
	startInputs["ur5e"] = []referenceframe.Input{0.5, 0.5, 0.5, 0.5, 0.5, 0.5}
	startLI := startInputs.ToLinearInputs()

	// Goal: move the first 3 joints to 0.6 rad (these are the joints that affect the tool).
	// This guarantees reachability since only joints before forearm_link matter.
	goalInputs := referenceframe.NewZeroInputs(fs)
	goalInputs["ur5e"] = []referenceframe.Input{0.6, 0.6, 0.6, 0.5, 0.5, 0.5}
	goalLI := goalInputs.ToLinearInputs()

	// Compute the tool's pose at the goal configuration — this is our planning target.
	toolPIF := referenceframe.NewPoseInFrame("tool", spatialmath.NewZeroPose())
	goalResult, err := fs.Transform(goalLI, toolPIF, referenceframe.World)
	test.That(t, err, test.ShouldBeNil)
	goalPose := goalResult.(*referenceframe.PoseInFrame).Pose()
	logger.Infof("tool at goal config: %v", goalPose.Point())

	// Also log the start for comparison.
	startResult, err := fs.Transform(startLI, toolPIF, referenceframe.World)
	test.That(t, err, test.ShouldBeNil)
	logger.Infof("tool at start config: %v", startResult.(*referenceframe.PoseInFrame).Pose().Point())

	goal := &PlanState{poses: referenceframe.FrameSystemPoses{
		"tool": referenceframe.NewPoseInFrame(referenceframe.World, goalPose),
	}}

	plan, _, err := PlanMotion(context.Background(), logger, &PlanRequest{
		FrameSystem:    fs,
		Goals:          []*PlanState{goal},
		StartState:     &PlanState{structuredConfiguration: startInputs},
		PlannerOptions: NewBasicPlannerOptions(),
	})
	test.That(t, err, test.ShouldBeNil)
	test.That(t, len(plan.Trajectory()), test.ShouldBeGreaterThanOrEqualTo, 2)
}
