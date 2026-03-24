package worksheet

import (
	"fmt"
	"testing"

	"github.com/golang/geo/r3"
	viz "github.com/viam-labs/motion-tools/client/client"
	"github.com/viam-labs/motion-tools/client/colorutil"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/spatialmath"
	"go.viam.com/test"
)

func TestComposePoints(t *testing.T) {
	logger := logging.NewTestLogger(t)
	origin := spatialmath.NewPoseFromPoint(r3.Vector{0, 0, 0})
	pt1 := spatialmath.NewPoseFromPoint(r3.Vector{100, 100, 0})
	pt2 := spatialmath.NewPoseFromPoint(r3.Vector{0, 0, 100})
	composed := spatialmath.Compose(pt1, pt2)
	logger.Infof("\nA: %v\nB: %v\nComposed: %v", pt1, pt2, composed)
	err := viz.RemoveAllSpatialObjects()
	test.That(t, err, test.ShouldBeNil)

	//err = viz.DrawPoses([]spatialmath.Pose{pt1, pt2, composed}, []string{"red", "blue", "purple"}, true)
	// test.That(t, err, test.ShouldBeNil)
	red, err := colorutil.NamedColorToRGB("red")
	test.That(t, err, test.ShouldBeNil)
	blue, err := colorutil.NamedColorToRGB("blue")
	test.That(t, err, test.ShouldBeNil)
	purple, err := colorutil.NamedColorToRGB("purple")
	test.That(t, err, test.ShouldBeNil)

	err = viz.DrawLine("pt1", []spatialmath.Pose{origin, pt1}, &red, &red)
	test.That(t, err, test.ShouldBeNil)
	err = viz.DrawLine("pt2", []spatialmath.Pose{origin, pt2}, &blue, &blue)
	test.That(t, err, test.ShouldBeNil)
	err = viz.DrawLine("composed", []spatialmath.Pose{origin, composed}, &purple, &purple)
	test.That(t, err, test.ShouldBeNil)
}

func TestComposePointFromOriginWithOrientation(t *testing.T) {
	logger := logging.NewTestLogger(t)
	origin := r3.Vector{0, 0, 0}

	// Add some Z to raise the resulting line off the X/Y axis. The visualizer gives priority to
	// drawing the axis over user lines.
	pt := spatialmath.NewPoseFromPoint(r3.Vector{100, 0, 30})

	err := viz.RemoveAllSpatialObjects()
	test.That(t, err, test.ShouldBeNil)
	for idx, theta := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		originWithTheta := spatialmath.NewPose(origin, &spatialmath.OrientationVectorDegrees{theta, 0, 0, 0})
		composed := spatialmath.Compose(originWithTheta, pt)
		logger.Infof("\nOrigin: %v\nPt: %v\nComposed: %v", originWithTheta, pt, composed)

		var color [3]uint8
		switch idx {
		case 0:
			color, err = colorutil.NamedColorToRGB("red")
			test.That(t, err, test.ShouldBeNil)
		case 1:
			color, err = colorutil.NamedColorToRGB("blue")
			test.That(t, err, test.ShouldBeNil)
		default:
			color, err = colorutil.NamedColorToRGB("green")
			test.That(t, err, test.ShouldBeNil)
		}

		err = viz.DrawLine(fmt.Sprintf("pt%d", idx),
			[]spatialmath.Pose{spatialmath.NewPoseFromPoint(origin), composed}, &color, &color)
		test.That(t, err, test.ShouldBeNil)
	}
}

func TestComposePointsWithThetaOrientations(t *testing.T) {
	logger := logging.NewTestLogger(t)
	origin := r3.Vector{0, 0, 0}
	originPose := spatialmath.NewPoseFromPoint(origin)

	pt1 := r3.Vector{100, 100, 50}
	pt2 := r3.Vector{100, 0, 0}
	err := viz.RemoveAllSpatialObjects()
	test.That(t, err, test.ShouldBeNil)

	for idx, theta := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		pt1WithTheta := spatialmath.NewPose(pt1, &spatialmath.OrientationVectorDegrees{theta, 0, 0, 0})
		composed := spatialmath.Compose(pt1WithTheta, spatialmath.NewPoseFromPoint(pt2))
		logger.Infof("\nPt: %v\nComposed: %v", pt1WithTheta, composed)

		var color [3]uint8
		switch idx {
		case 0:
			color, err = colorutil.NamedColorToRGB("red")
			test.That(t, err, test.ShouldBeNil)
		case 1:
			color, err = colorutil.NamedColorToRGB("blue")
			test.That(t, err, test.ShouldBeNil)
		default:
			color, err = colorutil.NamedColorToRGB("green")
			test.That(t, err, test.ShouldBeNil)
		}

		err = viz.DrawLine(fmt.Sprintf("pt_origin%d", idx),
			[]spatialmath.Pose{originPose, pt1WithTheta}, &color, &color)
		test.That(t, err, test.ShouldBeNil)
		err = viz.DrawLine(fmt.Sprintf("pt%d", idx),
			[]spatialmath.Pose{pt1WithTheta, composed}, &color, &color)
		test.That(t, err, test.ShouldBeNil)
	}
}

func TestComposePointsWithNondefaultOZ(t *testing.T) {
	logger := logging.NewTestLogger(t)
	origin := r3.Vector{0, 0, 0}
	originPose := spatialmath.NewPoseFromPoint(origin)

	pt1 := r3.Vector{100, 100, 50}
	pt2 := r3.Vector{100, 0, 0}
	err := viz.RemoveAllSpatialObjects()
	test.That(t, err, test.ShouldBeNil)

	for tIdx, theta := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		for xIdx, ox := range []float64{1} {
			// Set oy to ox. This creates a 45 degree orientation on the X/Y plane. Applying an
			// X=100 pose to that and incrementing theta will create a circle "reaching" for the
			// positive X and Y axis'.
			oy := ox
			pt1WithOrientation := spatialmath.NewPose(pt1, &spatialmath.OrientationVectorDegrees{theta, ox, oy, 0})
			composed := spatialmath.Compose(pt1WithOrientation, spatialmath.NewPoseFromPoint(pt2))
			logger.Infof("\nPt: %v\nComposed: %v", pt1WithOrientation, composed)

			var color [3]uint8
			switch tIdx {
			case 0:
				color, err = colorutil.NamedColorToRGB("red")
				test.That(t, err, test.ShouldBeNil)
			case 1:
				color, err = colorutil.NamedColorToRGB("blue")
				test.That(t, err, test.ShouldBeNil)
			default:
				color, err = colorutil.NamedColorToRGB("green")
				test.That(t, err, test.ShouldBeNil)
			}

			err = viz.DrawLine(fmt.Sprintf("pt_origin%d_%d", tIdx, xIdx),
				[]spatialmath.Pose{originPose, pt1WithOrientation}, &color, &color)
			test.That(t, err, test.ShouldBeNil)
			err = viz.DrawLine(fmt.Sprintf("pt%d_%d", tIdx, xIdx),
				[]spatialmath.Pose{pt1WithOrientation, composed}, &color, &color)
			test.That(t, err, test.ShouldBeNil)
		}
	}
}

func MustNamedColorToRGB(name string) [3]uint8 {
	ret, err := colorutil.NamedColorToRGB(name)
	if err != nil {
		panic(err)
	}

	return ret
}

var greenRBG = MustNamedColorToRGB("green")
var blueRBG = MustNamedColorToRGB("blue")
var redRBG = MustNamedColorToRGB("red")

func TestPoseBetween(t *testing.T) {
	logger := logging.NewTestLogger(t)
	origin := r3.Vector{0, 0, 0}
	originPose := spatialmath.NewPoseFromPoint(origin)

	// Start with a line straight "up" from the origin. Suppose it's a "hand" is holding something
	// (e.g: a cup) 90 degrees to the right. Towards the X axis.
	thetaZero := 0.0
	ox1 := 1.0
	hand := spatialmath.NewPose(r3.Vector{0, 0, 100}, &spatialmath.OrientationVectorDegrees{thetaZero, ox1, 0, 0})

	// In the reference frame of the hand, X becomes is the new Z. By moving 100 units along the Z
	// axis, the new point will actually move in the +X direction. We'll call this position the cup.
	oz1 := 1.0

	// The cup is 100 units in the +Z of the hand (+X in world-space). We'll compose this "link" of
	// 100 units in the +Z. But force the cup orientation.
	cupPoint := spatialmath.Compose(hand, spatialmath.NewPoseFromPoint(r3.Vector{0, 0, 100}))
	// Keep the cup's orientation as up. We don't want to spill!
	cup := spatialmath.NewPose(cupPoint.Point(), &spatialmath.OrientationVectorDegrees{0, 0, 0, oz1})

	// Draw the first line as green. Do not draw the second line. Instead just draw its pose.
	err := viz.RemoveAllSpatialObjects()
	test.That(t, err, test.ShouldBeNil)

	err = viz.DrawLine(fmt.Sprintf("hand"),
		[]spatialmath.Pose{originPose, hand}, &greenRBG, &greenRBG)
	test.That(t, err, test.ShouldBeNil)
	err = viz.DrawPoints("cup", []spatialmath.Pose{cup}, [][3]uint8{greenRBG}, nil)
	test.That(t, err, test.ShouldBeNil)

	// Now we can take the difference, or `PoseBetween`. Not to be confused with `PoseDelta`, which
	// won't do what we want. The idea is that if we want the cup to be in some pose, we can
	// derive a hand pose that gets us there.
	cupToHand := spatialmath.PoseBetween(cup, hand)
	handToCup := spatialmath.PoseBetween(hand, cup)

	// The difference itself is not meaningful in the 3-d space. Just like subtracting two dates on
	// a calendar does not create a new meaningful calendar date.
	//
	// But if we compose that difference with the cup's position, we should get back where the hand
	// is.
	handDerived := spatialmath.Compose(cup, cupToHand)
	err = viz.DrawLine("between", []spatialmath.Pose{cup, handDerived}, &greenRBG, &greenRBG)
	test.That(t, err, test.ShouldBeNil)

	logger.Infof("\nHand: %v\nCup: %v\nBetween: %v\nDerived hand: %v", hand, cup, cupToHand, handDerived)

	// If we want to find different ways to approach the cup, we can rotate the cup's theta. And
	// re-use the `between` pose to compute where the hand would need to be.
	for idx, theta := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		cupRotated := spatialmath.NewPose(cupPoint.Point(), &spatialmath.OrientationVectorDegrees{theta, 0, 0, oz1})
		candidatePose := spatialmath.Compose(cupRotated, cupToHand)
		logger.Infof("Candidate hand pose: %v", candidatePose)

		var color [3]uint8
		switch idx {
		case 0:
			color = greenRBG
		case 1:
			color = blueRBG
		default:
			color = redRBG
		}

		err = viz.DrawLine(fmt.Sprintf("candidate_%v", idx), []spatialmath.Pose{cupRotated, candidatePose}, &color, &color)
		test.That(t, err, test.ShouldBeNil)
	}

	// Similarly, if we want to move the cup elsewhere, we can compute where the hand must be.
	newCupPt := r3.Vector{-100, -50, 100}
	// And we can play a similar game of rotating the cup to find a bunch of candidate hand positions.
	for idx, theta := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		cupRotated := spatialmath.NewPose(newCupPt, &spatialmath.OrientationVectorDegrees{theta, 0, 0, oz1})
		candidatePose := spatialmath.Compose(cupRotated, cupToHand)
		logger.Infof("Moving Candidate hand pose: %v", candidatePose)

		var color [3]uint8
		switch idx {
		case 0:
			color = greenRBG
		case 1:
			color = blueRBG
		default:
			color = redRBG
		}

		err = viz.DrawLine(fmt.Sprintf("moving_candidate_%v", idx), []spatialmath.Pose{cupRotated, candidatePose}, &color, &color)
		test.That(t, err, test.ShouldBeNil)
	}

	// If the hand starts having an OZ != 0, we can see the cup will start tipping.
	handTipping := spatialmath.NewPose(
		r3.Vector{-100, 100, 100},
		&spatialmath.OrientationVectorDegrees{thetaZero, ox1, 0, oz1})
	cupTipping := spatialmath.Compose(handTipping, handToCup)
	err = viz.DrawPoses([]spatialmath.Pose{cupTipping}, []string{"red"}, false)
	test.That(t, err, test.ShouldBeNil)
	logger.Infof("\nHandTipping: %v\nCupTipping: %v", handTipping, cupTipping)
}
