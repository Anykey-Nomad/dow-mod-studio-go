package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// An event that names no marker gives the beam nothing to aim at, so the
// viewer picks a direction for it. That choice is visible: the monolith's
// awakening_lighting_ground has no marker, writes a twelve unit
// LIGHTNING_BEAM, and used to leave the building sideways at ground height —
// a green slab lying across the ground rather than a bolt out of the sky.
//
// These pin the fallback so it cannot drift back to an arbitrary axis.

// markerlessBeam is what that effect produces: no marker, so no axis.
func markerlessBeam() fxOverlayEffect {
	return fxOverlayEffect{
		Label:    "awakening_lighting_ground",
		Marker:   -1,
		HasOther: false,
		Origin:   fauxgl.Vector{X: 0, Y: 0, Z: 6.3},
	}
}

func TestMarkerlessBeamRunsUpNotSideways(t *testing.T) {
	e := markerlessBeam()
	far := e.fxFarPoint()
	d := far.Sub(e.Origin)
	if n := d.Length(); n < 1e-9 {
		t.Fatalf("the far point fell on the origin, so the beam has no direction at all")
	}
	dir := d.Normalize()
	if math.Abs(dir.Z) < 0.99 {
		t.Errorf("a markerless beam runs (%.3f %.3f %.3f): it is not along up, and with no "+
			"marker to aim it a ground strike leaves the model sideways", dir.X, dir.Y, dir.Z)
	}
}

// The fallback is the model's own up, not a hardcoded guess, so it follows the
// renderer's axis rather than being written out twice in different places.
func TestMarkerlessBeamUsesTheRenderersUpAxis(t *testing.T) {
	if fxAxisZ.Z != 1 || fxAxisZ.X != 0 || fxAxisZ.Y != 0 {
		t.Errorf("the renderer's up is (%.1f %.1f %.1f), which is not +Z", fxAxisZ.X, fxAxisZ.Y, fxAxisZ.Z)
	}
	e := markerlessBeam()
	d := e.fxFarPoint().Sub(e.Origin).Normalize()
	want := fxAxisZ.Normalize()
	if d.Sub(want).Length() > 1e-9 {
		t.Errorf("the beam runs (%.3f %.3f %.3f), the up axis is (%.3f %.3f %.3f)",
			d.X, d.Y, d.Z, want.X, want.Y, want.Z)
	}
}

// A marker that does resolve must still win: the fallback is for the events
// that name none, and it must not quietly override one that names a barrel.
func TestMarkeredBeamStillUsesItsOwnAxis(t *testing.T) {
	e := markerlessBeam()
	e.Axis = fauxgl.Vector{X: 1}
	d := e.fxFarPoint().Sub(e.Origin).Normalize()
	if math.Abs(d.X) < 0.99 {
		t.Errorf("a beam with its own axis runs (%.3f %.3f %.3f), not along it", d.X, d.Y, d.Z)
	}
}

// The far point is a lever, not the answer: the length in the file is what
// decides how far the beam reaches, and a ground strike is twelve units long.
func TestMarkerlessBeamLengthComesFromTheFile(t *testing.T) {
	e := markerlessBeam()
	e.Sim = nil
	// fxBeamFreeLength is only the distance the axis is probed at; the beam's
	// own Length curve is read as model units because there is no gap to be a
	// fraction of.
	sim := fx.NewFxSim(mustParseFx(t, awakeningLightingGroundLua))
	for _, r := range sim.Beams(e.Origin, e.fxFarPoint(), e.fxSpansMarkers()) {
		got := r.To.Sub(r.From).Length()
		if math.Abs(got-12) > 0.01 {
			t.Errorf("the beam spans %.3f, the file asks for 12", got)
		}
	}
}

const awakeningLightingGroundLua = `
fxtypes =
{
	{
		effect = "root>__fxtool_private_copy",
		fx =
		{
			properties =
			{
				{
					name = "Duration",
					type = "VARTYPE_FLOAT",
					value = 3,
				},
				{
					name = "Length",
					type = "VARTYPE_ARRAY_TIMEFLOAT",
					value = { 0, 12, 1, 12 },
				},
				{
					name = "Width",
					type = "VARTYPE_ARRAY_TIMEFLOAT",
					value = { 0, 5, 1, 5 },
				},
			},
		},
		style = "STYLE_BEAM",
	},
	{
		effect = "root",
		fx = { properties = {} },
		style = "STYLE_COMBO",
	},
}
`

func mustParseFx(t *testing.T, lua string) *fx.FxFile {
	t.Helper()
	f, err := fx.ParseFxLua([]byte(lua))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}
