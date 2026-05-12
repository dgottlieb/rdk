package spatialmath

import (
	"encoding/json"
	"fmt"

	"github.com/golang/geo/r3"
	commonpb "go.viam.com/api/common/v1"
)

// EmptyBox is a Geometry shaped like an open-topped container. It is composed of 5 *box walls
// (bottom, +X, -X, +Y, -Y). The +Z face is missing, so the interior is open at the top.
type EmptyBox struct {
	center    Pose
	outerDims r3.Vector
	thickness float64
	label     string
	walls     []Geometry
}

// NewEmptyBox returns an EmptyBox: a Geometry shaped like a box with no top, implemented as 5 box
// walls of the given thickness. outerDims are the exterior dimensions of the container; the +Z
// face is omitted.
func NewEmptyBox(pose Pose, outerDims r3.Vector, thickness float64, label string) (*EmptyBox, error) {
	if outerDims.X <= 0 || outerDims.Y <= 0 || outerDims.Z <= 0 {
		return nil, fmt.Errorf("empty box outer dimensions must be positive, got %v", outerDims)
	}
	if thickness <= 0 {
		return nil, fmt.Errorf("empty box thickness must be positive, got %v", thickness)
	}
	if 2*thickness >= outerDims.X || 2*thickness >= outerDims.Y || thickness >= outerDims.Z {
		return nil, fmt.Errorf("empty box thickness %v too large for outer dimensions %v", thickness, outerDims)
	}

	sideHeight := outerDims.Z - thickness
	sideCenterZ := thickness / 2
	bottomCenterZ := -outerDims.Z/2 + thickness/2

	wallSpecs := []struct {
		name   string
		offset r3.Vector
		dims   r3.Vector
	}{
		{"bottom", r3.Vector{X: 0, Y: 0, Z: bottomCenterZ}, r3.Vector{X: outerDims.X, Y: outerDims.Y, Z: thickness}},
		{"x_pos", r3.Vector{X: outerDims.X/2 - thickness/2, Y: 0, Z: sideCenterZ}, r3.Vector{X: thickness, Y: outerDims.Y, Z: sideHeight}},
		{"x_neg", r3.Vector{X: -outerDims.X/2 + thickness/2, Y: 0, Z: sideCenterZ}, r3.Vector{X: thickness, Y: outerDims.Y, Z: sideHeight}},
		{"y_pos", r3.Vector{X: 0, Y: outerDims.Y/2 - thickness/2, Z: sideCenterZ}, r3.Vector{X: outerDims.X - 2*thickness, Y: thickness, Z: sideHeight}},
		{"y_neg", r3.Vector{X: 0, Y: -outerDims.Y/2 + thickness/2, Z: sideCenterZ}, r3.Vector{X: outerDims.X - 2*thickness, Y: thickness, Z: sideHeight}},
	}

	walls := make([]Geometry, 0, len(wallSpecs))
	for _, spec := range wallSpecs {
		wallPose := Compose(pose, NewPoseFromPoint(spec.offset))
		wallLabel := spec.name
		if label != "" {
			wallLabel = label + ":" + spec.name
		}
		wall, err := NewBox(wallPose, spec.dims, wallLabel)
		if err != nil {
			return nil, err
		}
		walls = append(walls, wall)
	}

	return &EmptyBox{
		center:    pose,
		outerDims: outerDims,
		thickness: thickness,
		label:     label,
		walls:     walls,
	}, nil
}

// Walls returns the 5 box geometries (bottom, +X, -X, +Y, -Y) that compose the EmptyBox, in world
// coordinates. The returned slice is a copy; mutating it will not affect the EmptyBox.
func (eb *EmptyBox) Walls() []Geometry {
	out := make([]Geometry, len(eb.walls))
	copy(out, eb.walls)
	return out
}

func (eb *EmptyBox) Pose() Pose {
	return eb.center
}

func (eb *EmptyBox) Label() string {
	return eb.label
}

func (eb *EmptyBox) SetLabel(label string) {
	eb.label = label
	for idx, wall := range eb.walls {
		suffix := wall.Label()
		if colon := lastColon(suffix); colon >= 0 {
			suffix = suffix[colon+1:]
		}
		if label == "" {
			eb.walls[idx].SetLabel(suffix)
		} else {
			eb.walls[idx].SetLabel(label + ":" + suffix)
		}
	}
}

func lastColon(s string) int {
	for idx := len(s) - 1; idx >= 0; idx-- {
		if s[idx] == ':' {
			return idx
		}
	}
	return -1
}

func (eb *EmptyBox) Transform(toPremultiply Pose) Geometry {
	newWalls := make([]Geometry, len(eb.walls))
	for idx, wall := range eb.walls {
		newWalls[idx] = wall.Transform(toPremultiply)
	}
	return &EmptyBox{
		center:    Compose(toPremultiply, eb.center),
		outerDims: eb.outerDims,
		thickness: eb.thickness,
		label:     eb.label,
		walls:     newWalls,
	}
}

func (eb *EmptyBox) CollidesWith(other Geometry, buffer float64) (bool, float64, error) {
	minDist := buffer
	first := true
	for _, wall := range eb.walls {
		collides, dist, err := wall.CollidesWith(other, buffer)
		if err != nil {
			return false, 0, err
		}
		if collides {
			return true, -1, nil
		}
		if first || dist < minDist {
			minDist = dist
			first = false
		}
	}
	return false, minDist, nil
}

func (eb *EmptyBox) DistanceFrom(other Geometry) (float64, error) {
	minDist := 0.0
	first := true
	for _, wall := range eb.walls {
		dist, err := wall.DistanceFrom(other)
		if err != nil {
			return 0, err
		}
		if first || dist < minDist {
			minDist = dist
			first = false
		}
	}
	return minDist, nil
}

func (eb *EmptyBox) EncompassedBy(other Geometry) (bool, error) {
	for _, wall := range eb.walls {
		encompassed, err := wall.EncompassedBy(other)
		if err != nil {
			return false, err
		}
		if !encompassed {
			return false, nil
		}
	}
	return true, nil
}

func (eb *EmptyBox) ToPoints(resolution float64) []r3.Vector {
	var points []r3.Vector
	for _, wall := range eb.walls {
		points = append(points, wall.ToPoints(resolution)...)
	}
	return points
}

// ToProtobuf converts the empty box to a Geometry proto. The proto schema has no native empty-box
// type, so this returns the outer bounding box. This is a conservative (lossy) representation; for
// faithful serialization the caller should serialize the underlying walls individually.
func (eb *EmptyBox) ToProtobuf() *commonpb.Geometry {
	return &commonpb.Geometry{
		Center: PoseToProtobuf(eb.center),
		GeometryType: &commonpb.Geometry_Box{
			Box: &commonpb.RectangularPrism{DimsMm: &commonpb.Vector3{
				X: eb.outerDims.X,
				Y: eb.outerDims.Y,
				Z: eb.outerDims.Z,
			}},
		},
		Label: eb.label,
	}
}

func (eb *EmptyBox) Hash() int {
	hash := HashPose(eb.center)
	for _, wall := range eb.walls {
		hash += wall.Hash()
	}
	return hash
}

func (eb *EmptyBox) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type      string    `json:"type"`
		Pose      Pose      `json:"pose"`
		OuterDims r3.Vector `json:"outer_dims"`
		Thickness float64   `json:"thickness"`
		Label     string    `json:"label,omitempty"`
	}{
		Type:      "empty_box",
		Pose:      eb.center,
		OuterDims: eb.outerDims,
		Thickness: eb.thickness,
		Label:     eb.label,
	})
}
