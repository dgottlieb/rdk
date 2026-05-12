package spatialmath

import (
	"fmt"

	"github.com/golang/geo/r3"
)

// NewEmptyBox returns a Mesh shaped like a box with no top. It is built conceptually from 5 box
// walls (bottom, +X, -X, +Y, -Y) of the given thickness; the resulting triangles are unioned into a
// single Mesh positioned at the given pose. outerDims are the exterior dimensions; the +Z face is
// omitted, so the opening points along local +Z.
func NewEmptyBox(pose Pose, outerDims r3.Vector, thickness float64, label string) (*Mesh, error) {
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

	var allTriangles []*Triangle
	for _, spec := range wallSpecs {
		wallLabel := spec.name
		if label != "" {
			wallLabel = label + ":" + spec.name
		}

		wall, err := NewBox(NewPoseFromPoint(spec.offset), spec.dims, wallLabel)
		if err != nil {
			return nil, err
		}
		allTriangles = append(allTriangles, wall.(*box).toMesh().Triangles()...)
	}

	return NewMesh(pose, allTriangles, label), nil
}
