package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

func TestFxScaleSpriteScalesRadiusAndLocalOffsetTogether(t *testing.T) {
	sp := fx.Sprite{Pos: [3]float64{1, -2, 0.5}, Radius: 0.25}
	got := fxScaleSprite(sp, 2)
	if got.Pos != [3]float64{2, -4, 1} || got.Radius != 0.5 {
		t.Fatalf("scaled sprite = pos %v radius %v, want pos [2 -4 1] radius 0.5", got.Pos, got.Radius)
	}
	if sp.Pos != [3]float64{1, -2, 0.5} || sp.Radius != 0.25 {
		t.Fatalf("scaling modified the simulation sprite: %+v", sp)
	}

	fallback := fxScaleSprite(fx.Sprite{}, 2)
	if fallback.Radius != 2*fxSpriteFallbackRadius {
		t.Errorf("missing-radius fallback = %v, want %v in model units", fallback.Radius, 2*fxSpriteFallbackRadius)
	}
}

func TestFxScaleRibbonScalesAuthoredDimensionsButKeepsMarkerGap(t *testing.T) {
	origin := fauxgl.Vector{X: 10, Y: 2, Z: -1}
	e := fxOverlayEffect{Origin: origin, Scale: 2}
	rb := fx.BeamRibbon{
		From:  origin.Add(fauxgl.Vector{X: 0.5}),
		To:    origin.Add(fauxgl.Vector{X: 2.5}),
		Width: 0.25,
		Arc:   fauxgl.Vector{Y: 0.2},
		Noise: 0.1,
	}
	got := e.fxScaleRibbon(rb, true)
	if math.Abs(got.From.X-11) > 1e-9 || math.Abs(got.To.X-13) > 1e-9 {
		t.Errorf("scaled beam endpoints = %v to %v, want translated offset and fixed two-unit marker gap", got.From, got.To)
	}
	if math.Abs(got.To.Sub(got.From).Length()-2) > 1e-9 {
		t.Errorf("marker-to-marker span changed to %v", got.To.Sub(got.From).Length())
	}
	if got.Width != 0.5 || got.Arc.Y != 0.4 || got.Noise != 0.2 {
		t.Errorf("scaled ribbon dimensions width=%v arc=%v noise=%v", got.Width, got.Arc, got.Noise)
	}
}

func TestFxScaleRibbonScalesIntrinsicBeamLength(t *testing.T) {
	e := fxOverlayEffect{Origin: fauxgl.Vector{Y: -3}, Scale: 1.5}
	rb := fx.BeamRibbon{
		From:  e.Origin,
		To:    e.Origin.Add(fauxgl.Vector{Z: 2}),
		Width: 0.2,
	}
	got := e.fxScaleRibbon(rb, false)
	if math.Abs(got.To.Sub(got.From).Length()-3) > 1e-9 {
		t.Errorf("intrinsic beam length = %v, want 3 model units", got.To.Sub(got.From).Length())
	}
	if math.Abs(got.From.Sub(e.Origin).Length()) > 1e-9 {
		t.Errorf("intrinsic beam moved off its anchored origin: %v", got.From)
	}
}

func TestFxScaleFXControlSetsTheGeometryMultiplier(t *testing.T) {
	v := &modelViewer{fxScale: fxScaleDefaultValue}
	v.scaleFX(modelFXScaleStep)
	if math.Abs(v.fxScale-modelFXScaleStep) > 1e-9 {
		t.Fatalf("UI scale control set %.3f, want %.3f", v.fxScale, modelFXScaleStep)
	}
	effects := fxScaledEffects([]fxOverlayEffect{{}}, v.fxScale)
	if len(effects) != 1 || math.Abs(effects[0].fxScaleOrOne()-modelFXScaleStep) > 1e-9 {
		t.Errorf("displayed scale did not reach FX geometry: %+v", effects)
	}
	v.scaleFX(1 / modelFXScaleStep)
	if math.Abs(v.fxScale-fxScaleDefaultValue) > 1e-9 {
		t.Errorf("resetting with inverse step returned %.3f, want 1", v.fxScale)
	}
}

func TestFxBuildGpuSceneUsesUnifiedScaleAndPreservesMarkerSpans(t *testing.T) {
	origin := fauxgl.Vector{X: 10}
	sim := &fx.FxSim{
		Particles: []fx.Particle{{
			V:   fx.Visual{Lifetime: 1, Radius: []fx.FxKey{{V: 0.25}}, SelfLit: true},
			Pos: [3]float64{1, 2, 3},
		}},
		BeamNodes: []fx.Beam{{
			Lifetime: 1,
			Length:   []fx.FxKey{{V: 0.5}},
			Width:    []fx.FxKey{{V: 0.25}},
		}},
	}
	effect := fxOverlayEffect{
		Sim: sim, Origin: origin, Far: fauxgl.Vector{X: 14},
		HasOther: true, Spans: true, Scale: 1.5, EventScale: 2,
	}
	scene := fxBuildGpuScene([]fxOverlayEffect{effect}, nil, fauxgl.Vector{}, fauxgl.Vector{Z: 1})
	if scene == nil {
		t.Fatal("GPU scene is empty")
	}
	var sprite *fx.Sprite
	var beam *fx.BeamRibbon
	for i := range scene.Groups {
		g := &scene.Groups[i]
		if len(g.Sprites) != 0 {
			sprite = &g.Sprites[0]
		}
		if len(g.Beams) != 0 {
			beam = &g.Beams[0].Ribbon
		}
	}
	if sprite == nil || beam == nil {
		t.Fatalf("GPU scene has sprite=%v beam=%v", sprite != nil, beam != nil)
	}
	if sprite.Pos != [3]float64{3, 6, 9} || sprite.Radius != 0.75 {
		t.Errorf("GPU sprite pos=%v radius=%v, want pos [3 6 9] radius 0.75", sprite.Pos, sprite.Radius)
	}
	if math.Abs(beam.Width-0.75) > 1e-9 {
		t.Errorf("GPU beam width = %v, want 0.75", beam.Width)
	}
	if got := beam.To.Sub(beam.From).Length(); math.Abs(got-2) > 1e-9 {
		t.Errorf("GPU marker-spanning beam length = %v, want unchanged 2-unit marker fraction", got)
	}
	if scene.Groups[0].Origin != [3]float64{10, 0, 0} {
		t.Errorf("GPU effect origin changed with scale: %v", scene.Groups[0].Origin)
	}
}

// TestFxBuildGpuSceneGroupsByTextureBlendAndSite pins what the group key
// keeps apart, now that it is a struct rather than a formatted string: two
// effects reading the same map on different markers are two draws (each
// sprite is positioned relative to its own site), one site merges into one
// draw, and a covering and an additive copy of the same map never share.
func TestFxBuildGpuSceneGroupsByTextureBlendAndSite(t *testing.T) {
	simWith := func(blending int) *fx.FxSim {
		return &fx.FxSim{Anchored: []fx.Visual{{
			Lifetime: 1,
			Blending: blending,
			Radius:   []fx.FxKey{{V: 0.3}},
			SelfLit:  true,
		}}}
	}
	build := func(effects []fxOverlayEffect) *fxGpuScene {
		return fxBuildGpuScene(effects, nil, fauxgl.Vector{}, fauxgl.Vector{Z: 1})
	}
	sites := []fxOverlayEffect{
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 2}},
	}
	two := build(sites)
	if two == nil {
		t.Fatal("two effects on two markers built no scene")
	}
	if len(two.Groups) != 2 {
		t.Fatalf("two markers, same map: %d groups, want 2 (sprites are relative to their own site)", len(two.Groups))
	}
	for i := range two.Groups {
		g := &two.Groups[i]
		if len(g.Sprites) != 1 || len(g.Beams) != 0 {
			t.Errorf("group %d has %d sprites and %d beams, want one sprite each", i, len(g.Sprites), len(g.Beams))
		}
	}
	sameSite := []fxOverlayEffect{
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
	}
	if one := build(sameSite); one == nil || len(one.Groups) != 1 || len(one.Groups[0].Sprites) != 2 {
		t.Fatalf("one marker twice: scene %v, want a single group of two sprites", one)
	}
	mix := []fxOverlayEffect{
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
		{Sim: simWith(2), Origin: fauxgl.Vector{X: 1}},
	}
	mixed := build(mix)
	if mixed == nil || len(mixed.Groups) != 2 {
		t.Fatalf("covering and additive on one marker: %v, want two groups", mixed)
	}
	if mixed.Groups[0].Additive || !mixed.Groups[1].Additive {
		t.Errorf("blend modes did not sort covering-first: %v then %v", mixed.Groups[0].Additive, mixed.Groups[1].Additive)
	}
}
