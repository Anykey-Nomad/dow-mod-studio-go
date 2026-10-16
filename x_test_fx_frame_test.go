package main

import (
	"math"
	"testing"

	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whe"
	"github.com/fogleman/fauxgl"
)

func TestFXNoRotateDefaultsToFalseAndReadsRootFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		lua  string
		want bool
	}{
		{name: "missing flag inherits marker", lua: `fxtypes = { { effect = "root", fx = { properties = {}, style = "STYLE_COMBO" } } }`},
		{name: "explicit false inherits marker", lua: `fxtypes = { { effect = "root", fx = { properties = { property_00 = { name = "No_Rotate", type = "VARTYPE_BOOL", value = 0, }, }, style = "STYLE_COMBO" } } }`},
		{name: "explicit true opts out", lua: `fxtypes = { { effect = "root", fx = { properties = { property_00 = { name = "No_Rotate", type = "VARTYPE_BOOL", value = 1, }, }, style = "STYLE_COMBO" } } }`, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := fx.ParseFxLua([]byte(tc.lua))
			if err != nil {
				t.Fatalf("parse FX: %v", err)
			}
			sim := fx.NewFxSim(file)
			if sim.NoRotate != tc.want {
				t.Fatalf("NoRotate=%v, want %v", sim.NoRotate, tc.want)
			}
		})
	}
}

func TestFXLocalAxesFollowMarkerUnlessNoRotate(t *testing.T) {
	model := &WhmModel{}
	model.Markers = []WhmMarker{{
		Name:   "turned",
		Matrix: [9]float32{0, 0, 1, 0, 1, 0, 1, 0, 0},
	}}
	pose := model.RestPose()
	markerFrame := fxOriginAxisFrames(fxOverlayEffect{}, model, 0, pose)[0]
	if markerFrame == fxFallbackFrame {
		t.Fatal("default FX local axes should use the marker frame, not file/world fallback")
	}
	if markerFrame[0].Sub(fauxgl.Vector{Y: -1}).Length() > 1e-6 {
		t.Errorf("local X=%v, want marker/root-frame -Y", markerFrame[0])
	}

	noRotate := fxOriginAxisFrames(fxOverlayEffect{Sim: &fx.FxSim{NoRotate: true}}, model, 0, pose)[0]
	if noRotate != fxFallbackFrame {
		t.Errorf("No_Rotate local axes=%v, want the authored FX frame %v", noRotate, fxFallbackFrame)
	}
}

func TestFXLocalParticleOffsetMapsThroughMarkerFrame(t *testing.T) {
	frame := [3]fauxgl.Vector{{Y: 1}, {X: 1}, {Z: 1}}
	e := fxOverlayEffect{Origin: fauxgl.Vector{X: 10, Y: 20, Z: 30}, Frame: frame}
	sp := e.fxTurnSprite(fx.Sprite{Pos: [3]float64{2, 3, 4}})
	if want := [3]float64{3, 2, 4}; sp.Pos != want {
		t.Fatalf("local particle offset = %v, want marker-frame result %v", sp.Pos, want)
	}
	world := e.Origin.Add(fauxgl.Vector{X: sp.Pos[0], Y: sp.Pos[1], Z: sp.Pos[2]})
	if want := (fauxgl.Vector{X: 13, Y: 22, Z: 34}); world != want {
		t.Errorf("world particle position = %v, want marker origin + mapped local offset %v", world, want)
	}
}

func TestFXMarkerlessEmitterDirectionUsesAuthoredFXAxis(t *testing.T) {
	file, err := fx.ParseFxLua([]byte(`fxtypes = {
		{ effect = "root>puff", fx = { properties = {
			{ name = "Emitter_Rate", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 1, 1, 1 }, },
		}, style = "STYLE_SPRAY" },
		{ effect = "root", fx = { properties = {
			{ name = "Emitter_Direction", type = "VARTYPE_VECTOR3", value = { 1, 0, 0 }, },
		}, style = "STYLE_COMBO" },
		{ effect = "root>leaf", fx = { properties = {
			{ name = "Duration", type = "VARTYPE_FLOAT", value = 1, },
		}, style = "STYLE_RING" },
	}`))
	if err != nil {
		t.Fatalf("parse FX: %v", err)
	}
	sim := fx.NewFxSim(file)
	if sim.Axis.Sub(fauxgl.Vector{X: 1}).Length() > 1e-6 {
		t.Fatalf("authored direction = %v, want renderer-local +X", sim.Axis)
	}
	e := fxOverlayEffect{Sim: sim, Origin: fauxgl.Vector{Z: 5}, Marker: -1, Frame: fxFallbackFrame}
	e.Axis = e.fxMarkerVector(sim.Axis)
	far := e.fxFarPoint()
	got := far.Sub(e.Origin).Normalize()
	if want := (fauxgl.Vector{X: 1}); got.Sub(want).Length() > 1e-6 {
		t.Errorf("markerless beam direction = %v, want authored direction %v", got, want)
	}
}

func TestFXTransformSelfToOtherIsRecognized(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "self to other", value: "self_to_other", want: true},
		{name: "other transform", value: "self", want: false},
		{name: "case and spaces", value: " SELF_TO_OTHER ", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "fx_transform", Value: tc.value}}}
			if got := fxEventAlignsToOther(ev); got != tc.want {
				t.Errorf("fxEventAlignsToOther(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestFXSelfToOtherAlignsTheLocalFXForwardWithTheMarkerSpan(t *testing.T) {
	frame := [3]fauxgl.Vector{{X: 1}, {Y: 1}, {Z: 1}}
	direction := fauxgl.Vector{X: 1, Y: 1}.Normalize()
	aligned := fxFrameToward(frame, direction)
	if got := aligned[2].Sub(direction).Length(); got > 1e-6 {
		t.Fatalf("self_to_other forward=%v, want marker-span direction %v", aligned[2], direction)
	}
	for i, axis := range aligned {
		if math.Abs(axis.Length()-1) > 1e-6 {
			t.Errorf("aligned axis %d is not unit length: %v", i, axis)
		}
	}
	if math.Abs(aligned[0].Dot(aligned[1])) > 1e-6 || math.Abs(aligned[0].Dot(aligned[2])) > 1e-6 || math.Abs(aligned[1].Dot(aligned[2])) > 1e-6 {
		t.Errorf("self_to_other frame is not orthogonal: %v", aligned)
	}
	// No_Rotate preserves the authored frame, even when the event carries
	// self_to_other; it is the explicit FX-level rotation opt-out.
	e := fxOverlayEffect{Sim: &fx.FxSim{NoRotate: true}, Frame: frame}
	if got := e.fxMarkerVector(fauxgl.Vector{Z: 1}); got != (fauxgl.Vector{Z: 1}) {
		t.Errorf("No_Rotate changed the authored forward axis to %v", got)
	}
}

func TestFXSelfToOtherAlignsGeneratedBeamWithTheMarkerSpan(t *testing.T) {
	origin := fauxgl.Vector{X: 1, Y: -2, Z: 3}
	far := fauxgl.Vector{X: 5, Y: 4, Z: 3}
	span := far.Sub(origin)
	frame := fxFrameToward(fxFallbackFrame, span)
	sim := &fx.FxSim{BeamNodes: []fx.Beam{{
		Length:   []fx.FxKey{{V: 0.5}},
		Width:    []fx.FxKey{{V: 0.25}},
		Lifetime: 1,
	}}}
	sim.Reset()
	effect := fxOverlayEffect{
		Sim: sim, Origin: origin, Far: far, HasOther: true, Spans: true,
		AlignToOther: true, Frame: frame,
	}
	scene := fxBuildGpuScene([]fxOverlayEffect{effect}, nil, fauxgl.Vector{Z: 5}, fauxgl.Vector{Z: 1})
	if scene == nil {
		t.Fatal("self_to_other beam produced no GPU scene")
	}
	var beam *fx.BeamRibbon
	for i := range scene.Groups {
		if len(scene.Groups[i].Beams) > 0 {
			beam = &scene.Groups[i].Beams[0].Ribbon
			break
		}
	}
	if beam == nil {
		t.Fatal("self_to_other GPU scene contains no beam ribbon")
	}
	gotDirection := beam.To.Sub(beam.From).Normalize()
	wantDirection := span.Normalize()
	if gotDirection.Sub(wantDirection).Length() > 1e-6 {
		t.Errorf("beam direction=%v, want marker-span direction=%v", gotDirection, wantDirection)
	}
	if got, want := beam.To.Sub(beam.From).Length(), span.Length()*0.5; math.Abs(got-want) > 1e-6 {
		t.Errorf("beam span=%.6f, want half the marker gap %.6f", got, want)
	}
}

func TestFXNoRotateKeepsLocalParticleOffsetAndRingFrame(t *testing.T) {
	frame := [3]fauxgl.Vector{{Y: 1}, {Z: 1}, {X: 1}}
	e := fxOverlayEffect{
		Sim:   &fx.FxSim{NoRotate: true},
		Frame: frame,
	}
	local := fauxgl.Vector{X: 2, Y: 3, Z: 4}
	if got := e.fxMarkerVector(local); got != local {
		t.Errorf("No_Rotate transformed local displacement to %v, want %v", got, local)
	}
	if x, y, z := e.fxRingFrame(); x != fxFallbackFrame[0] || y != fxFallbackFrame[1] || z != fxFallbackFrame[2] {
		t.Errorf("No_Rotate ring frame = (%v,%v,%v), want authored FX frame %v", x, y, z, fxFallbackFrame)
	}
}
