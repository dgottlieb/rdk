package armplanning

import (
	"context"
	"testing"

	"github.com/golang/geo/r3"
	"github.com/viam-labs/motion-tools/client/client"
	commonpb "go.viam.com/api/common/v1"
	"go.viam.com/test"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/motionplan"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/rdk/utils"
)

func TestIKTolerances(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewTestLogger(t).Sublogger("mp")

	m, err := referenceframe.ParseModelJSONFile(utils.ResolveFile("components/arm/fake/kinematics/ur5e.json"), "")
	test.That(t, err, test.ShouldBeNil)
	fs := referenceframe.NewEmptyFrameSystem("")
	fs.AddFrame(m, fs.World())

	goal := referenceframe.FrameSystemPoses{m.Name(): referenceframe.NewPoseInFrame(
		referenceframe.World,
		spatialmath.NewPoseFromProtobuf(&commonpb.Pose{X: -46, Y: 0, Z: 372, OX: -1.78, OY: -3.3, OZ: -1.11}),
	)}

	seed := referenceframe.NewLinearInputs()
	seed.Put(m.Name(), make([]referenceframe.Input, 6))

	// Create PlanRequest to use the new API
	request := &PlanRequest{
		FrameSystem:    fs,
		Goals:          []*PlanState{NewPlanState(goal, nil)},
		StartState:     NewPlanState(nil, seed.ToFrameSystemInputs()),
		PlannerOptions: NewBasicPlannerOptions(),
		Constraints:    &motionplan.Constraints{},
	}

	pc, err := newPlanContext(ctx, logger, request, &PlanMeta{})
	test.That(t, err, test.ShouldBeNil)

	psc, err := newPlanSegmentContext(ctx, pc, seed, goal)
	test.That(t, err, test.ShouldBeNil)

	mp, err := newCBiRRTMotionPlanner(ctx, pc, psc, logger.Sublogger("cbirrt"))
	test.That(t, err, test.ShouldBeNil)

	// Test inability to arrive at another position due to orientation
	_, err = mp.planForTest(ctx)
	test.That(t, err, test.ShouldNotBeNil)

	// Now verify that setting tolerances to zero allows the same arm to reach that position
	opt := NewBasicPlannerOptions()
	opt.GoalMetricType = motionplan.PositionOnly
	opt.SetMaxSolutions(50)

	request2 := &PlanRequest{
		FrameSystem:    fs,
		Goals:          []*PlanState{NewPlanState(goal, nil)},
		StartState:     NewPlanState(nil, seed.ToFrameSystemInputs()),
		PlannerOptions: opt,
		Constraints:    &motionplan.Constraints{},
	}

	pc2, err := newPlanContext(ctx, logger, request2, &PlanMeta{})
	test.That(t, err, test.ShouldBeNil)

	psc2, err := newPlanSegmentContext(ctx, pc2, seed, goal)
	test.That(t, err, test.ShouldBeNil)

	mp2, err := newCBiRRTMotionPlanner(ctx, pc2, psc2, logger.Sublogger("cbirrt"))
	test.That(t, err, test.ShouldBeNil)
	_, err = mp2.planForTest(ctx)
	test.That(t, err, test.ShouldBeNil)
}

func TestArmWithGripperViz(t *testing.T) {
	logger := logging.NewTestLogger(t)
	fs := referenceframe.NewEmptyFrameSystem("arm_with_gripper")

	lite6, err := referenceframe.ParseModelJSONFile(
		utils.ResolveFile("components/arm/sim/kinematics/lite6.json"), "lite6")
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(lite6, fs.World())
	test.That(t, err, test.ShouldBeNil)

	gripperOffset, err := referenceframe.NewStaticFrame(
		"gripper_offset", spatialmath.NewPoseFromPoint(r3.Vector{Z: 40}))
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(gripperOffset, lite6)
	test.That(t, err, test.ShouldBeNil)

	gripper, err := referenceframe.ParseModelJSONFile(
		utils.ResolveFile("referenceframe/testfiles/test_gripper.json"), "gripper")
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(gripper, gripperOffset)
	test.That(t, err, test.ShouldBeNil)

	inputs := referenceframe.FrameSystemInputs{
		"lite6":   []referenceframe.Input{0, 0, 0, 0, -1.6, 0},
		"gripper": []referenceframe.Input{50, 50},
	}

	heldBox, err := spatialmath.NewEmptyBox(
		spatialmath.NewPose(r3.Vector{Z: 1}, &spatialmath.OrientationVector{OX: -1}),
		r3.Vector{X: 40, Y: 40, Z: 40},
		5, "held_box")
	test.That(t, err, test.ShouldBeNil)

	heldBoxFrame, err := referenceframe.NewStaticFrameWithGeometry(
		"held_box",
		spatialmath.NewPose(r3.Vector{Z: 1}, &spatialmath.OrientationVector{OX: -1}),
		heldBox,
	)
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(heldBoxFrame, gripper)
	test.That(t, err, test.ShouldBeNil)

	const floorSize = 1500.0
	const floorThickness = 20.0
	floor, err := spatialmath.NewBox(
		spatialmath.NewZeroPose(),
		r3.Vector{X: floorSize, Y: floorSize, Z: floorThickness},
		"floor",
	)
	test.That(t, err, test.ShouldBeNil)

	floorFrame, err := referenceframe.NewStaticFrameWithGeometry(
		"floor",
		spatialmath.NewPoseFromPoint(r3.Vector{Z: -floorThickness / 2}),
		floor,
	)
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(floorFrame, fs.World())
	test.That(t, err, test.ShouldBeNil)

	// Creates a frother that has a base, back, top and a stick hanging from the top out the side.
	createFrother(t, fs, floorFrame)

	err = client.RemoveAllSpatialObjects()
	test.That(t, err, test.ShouldBeNil)

	err = client.DrawFrameSystem(fs, inputs)
	test.That(t, err, test.ShouldBeNil)

	idealInputs := referenceframe.FrameSystemInputs{
		"lite6":   []referenceframe.Input{5.761739365860415, 1.4095370300288768, 1.608159059662642, -1.0968735049309546, -1.4002610417860264, -3.3},
		"gripper": []referenceframe.Input{30, 25},
	}
	_ = idealInputs
	idealBoxPose := spatialmath.NewPose(
		r3.Vector{X: 395.0, Y: -97.36, Z: 65.2786},
		&spatialmath.OrientationVectorDegrees{
			Theta: -4.10, OX: -0.192, OY: 0.258, OZ: 0.94})

	ctx := context.Background()
	req := &PlanRequest{
		FrameSystem: fs,
		Goals: []*PlanState{
			NewPlanState(referenceframe.FrameSystemPoses{
				"held_box": referenceframe.NewPoseInFrameWithGoalCloud(
					referenceframe.World,
					idealBoxPose,
					&referenceframe.PoseCloud{
						X: 10, Y: 10, Z: 10, OX: 0.2, OY: 0.2, OZ: 0.2, Theta: 15,
					},
				),
			}, nil),
		},
		StartState: NewPlanState(nil, inputs),
		PlannerOptions: &PlannerOptions{
			Timeout: defaultTimeout + 1,
		},
	}
	err = req.WriteToFile("/home/dgottlieb/viam/rdk/box-to-frother-failed-plan.json")
	test.That(t, err, test.ShouldBeNil)

	plan, _, err := PlanMotion(ctx, logger.Sublogger("heldbox-to-ideal"), req)
	test.That(t, err, test.ShouldBeNil)

	trajectory := plan.Trajectory()
	finalInputs := trajectory[len(trajectory)-1]
	err = client.DrawFrameSystem(fs, finalInputs)
	test.That(t, err, test.ShouldBeNil)

	heldBoxInWorld, err := fs.Transform(
		finalInputs.ToLinearInputs(),
		referenceframe.NewPoseInFrame("held_box", spatialmath.NewZeroPose()),
		referenceframe.World,
	)
	test.That(t, err, test.ShouldBeNil)

	heldBoxPose := heldBoxInWorld.(*referenceframe.PoseInFrame).Pose()
	logger.Infof(
		"held_box world pose: position=%v orientation=%v",
		heldBoxPose.Point(),
		heldBoxPose.Orientation().OrientationVectorDegrees(),
	)
}

func createFrother(t *testing.T, fs *referenceframe.FrameSystem, floorFrame referenceframe.Frame) {
	basePos := spatialmath.NewPoseFromPoint(r3.Vector{X: 400, Z: 30})
	base, err := spatialmath.NewBox(basePos, r3.Vector{X: 150, Y: 150, Z: 30}, "base")
	test.That(t, err, test.ShouldBeNil)

	baseFrame, err := referenceframe.NewStaticFrameWithGeometry("base", basePos, base)
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(baseFrame, floorFrame)
	test.That(t, err, test.ShouldBeNil)

	backPos := spatialmath.NewPoseFromPoint(r3.Vector{X: 50, Z: 75})
	back, err := spatialmath.NewBox(backPos, r3.Vector{X: 50, Y: 150, Z: 150}, "back")

	backFrame, err := referenceframe.NewStaticFrameWithGeometry("back", backPos, back)
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(backFrame, baseFrame)
	test.That(t, err, test.ShouldBeNil)

	topPos := spatialmath.NewPoseFromPoint(r3.Vector{X: -25, Z: 75})
	top, err := spatialmath.NewBox(topPos, r3.Vector{X: 100, Y: 150, Z: 30}, "top")

	topFrame, err := referenceframe.NewStaticFrameWithGeometry("top", topPos, top)
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(topFrame, backFrame)
	test.That(t, err, test.ShouldBeNil)

	stickPos := spatialmath.NewPose(
		r3.Vector{X: -30, Y: -75, Z: -60},
		&spatialmath.OrientationVector{OZ: -2, OY: -1})
	stickRadius, stickLen := 5., 100.
	stick, err := spatialmath.NewCapsule(stickPos, stickRadius, stickLen, "frother")
	test.That(t, err, test.ShouldBeNil)

	stickFrame, err := referenceframe.NewStaticFrameWithGeometry(
		"frother", stickPos, stick)
	test.That(t, err, test.ShouldBeNil)

	err = fs.AddFrame(stickFrame, topFrame)
	test.That(t, err, test.ShouldBeNil)

	// testBoxPos := spatialmath.NewPose(
	//  	r3.Vector{Z: stickLen / 2},
	//  	&spatialmath.OrientationVectorDegrees{OX: -0.4, OY: -.4, OZ: -2, Theta: 75},
	// )
	// testBox, err := spatialmath.NewEmptyBox(
	//  	testBoxPos,
	//  	r3.Vector{X: 40, Y: 40, Z: 40},
	//  	5, "testBox")
	// test.That(t, err, test.ShouldBeNil)
	//
	// testBoxFrame, err := referenceframe.NewStaticFrameWithGeometry(
	//  	"testBox", testBoxPos, testBox)
	// test.That(t, err, test.ShouldBeNil)
	// err = fs.AddFrame(testBoxFrame, stickFrame)
}
