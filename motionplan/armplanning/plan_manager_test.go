package armplanning

import (
	"context"
	"testing"
	"time"

	"github.com/golang/geo/r3"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/rdk/utils"
	"go.viam.com/test"
)

func TestJointGoalDetour(t *testing.T) {
	fs, startJoints, goalJoints, req := nudgeBlockedScene(t)
	idle, err := referenceframe.ParseModelJSONFile(utils.ResolveFile("components/arm/kinematics/xarm6.json"), "idle")
	test.That(t, err, test.ShouldBeNil)

	mount, err := referenceframe.NewStaticFrame("idle-mount", spatialmath.NewPoseFromPoint(r3.Vector{X: 2000}))
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(mount, fs.World())
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(idle, mount)
	test.That(t, err, test.ShouldBeNil)

	req.StartState = NewPlanState(nil, referenceframe.FrameSystemInputs{"arm": startJoints, "idle": startJoints})
	idleGoal := startJoints
	_ = idleGoal

	req.Goals = []*PlanState{NewPlanState(nil, referenceframe.FrameSystemInputs{"arm": goalJoints, "idle": idleGoal})}
	// req.Goals = []*PlanState{NewPlanState(nil, referenceframe.FrameSystemInputs{"arm": goalJoints})}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	plan, _, err := PlanMotion(ctx, logging.NewTestLogger(t), req)
	test.That(t, err, test.ShouldBeNil)

	trajectory := plan.Trajectory()
	// expected a detour around the post
	test.That(t, len(trajectory), test.ShouldBeGreaterThanOrEqualTo, 3)

	for _, step := range trajectory {
		// waypoint moves the unchanged arm
		test.That(t, step["idle"], test.ShouldResemble, startJoints)
	}
}
