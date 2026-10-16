package main

import (
	"testing"

	"gioui.org/widget"
	"github.com/fogleman/fauxgl"
)

func TestFXAttributeOriginAxisMatchesViewerAxis(t *testing.T) {
	origin := fauxgl.Vector{X: 1, Y: 2, Z: 3}
	for axis, value := range []float64{11, 22, 33} {
		got := fxAttributeOriginAxis(origin, axis, value)
		want := origin
		switch axis {
		case 0:
			want.X = value
		case 1:
			want.Y = value
		case 2:
			want.Z = value
		}
		if got != want {
			t.Errorf("editing viewer axis %d: got %+v, want %+v", axis, got, want)
		}
	}
}

func TestFXAttributeRotationUsesWorldAxes(t *testing.T) {
	// fxOrigins applies attribute rotations around the fixed viewer XYZ axes;
	// this is the same basis as the canvas gizmo and not marker-local axes.
	axes := [3]fauxgl.Vector{{X: 1}, {Y: 1}, {Z: 1}}
	vectors := [3]fauxgl.Vector{{Y: 1}, {Z: 1}, {X: 1}}
	want := [3]fauxgl.Vector{{Z: 1}, {X: 1}, {Y: 1}}
	for axis := range axes {
		angles := [3]float64{}
		angles[axis] = 3.141592653589793 / 2
		got := fxRotated(vectors[axis], angles[0], angles[1], angles[2], axes)
		if got.Sub(want[axis]).Length() > 1e-9 {
			t.Errorf("rotation about viewer axis %d: got %+v, want %+v", axis, got, want[axis])
		}
	}
}

func TestApplyFXAttributeFieldSetsOnlyMatchingPositionAxis(t *testing.T) {
	v := &modelViewer{
		fxEffects:   []fxOverlayEffect{{Origin: fauxgl.Vector{X: 1, Y: 2, Z: 3}}},
		fxAttrIndex: 0,
		fxOverrides: map[int]fxAttributeOverride{},
	}
	v.fxAttrPosY = widget.Editor{}
	v.fxAttrPosY.SetText("12.5")
	v.applyFXAttributeField(1)
	got := v.fxOverrides[0]
	if !got.HasOrigin || got.Origin != (fauxgl.Vector{X: 1, Y: 12.5, Z: 3}) {
		t.Fatalf("editing Y should change only model-space Y, got %+v", got.Origin)
	}
}
