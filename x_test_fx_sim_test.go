package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"
)

const fxSyntheticLua = `
fxtypes =
{
	{
		effect = "root",
		fx = { properties = { }, style = "STYLE_COMBO", },
	},
	{
		effect = "root>spray",
		fx = { properties = {
			property_00 = { name = "Emitter_Rate", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 100, 1, 100, }, },
			property_01 = { name = "Emitter_Duration", type = "VARTYPE_FLOAT", value = 1, },
			property_02 = { name = "Emitter_Loop", type = "VARTYPE_BOOL", value = 1, },
			property_03 = { name = "Emitter_Deviation", type = "VARTYPE_FLOAT", value = 10, },
			property_04 = { name = "Emitter_Offset", type = "VARTYPE_ARRAY_TIMEVECTOR3", value = { entry_00 = { 0, 0, 5, 0, }, entry_01 = { 1, 0, 1, 0, }, }, },
			property_05 = { name = "Particle_Gravity", type = "VARTYPE_FLOAT", value = 1, },
		}, style = "STYLE_SPRAY", },
	},
	{
		effect = "root>spray>ring",
		fx = { properties = {
			property_00 = { name = "Texture", type = "VARTYPE_STRING", value = "DATA:ART/FX/GLOW.TGA", },
			property_01 = { name = "Duration", type = "VARTYPE_FLOAT", value = 2, },
			property_02 = { name = "Blending", type = "VARTYPE_INT", value = 2, },
			property_03 = { name = "Radius", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 0.5, 1, 1.5, }, },
			property_04 = { name = "Colour", type = "VARTYPE_ARRAY_TIMECOLOUR", value = { entry_00 = { 0, 0, 255, 0, 128, }, entry_01 = { 1, 0, 255, 0, 0, }, }, },
			property_05 = { name = "Particle_Speed", type = "VARTYPE_FLOAT", value = 3, },
			property_06 = { name = "Billboard", type = "VARTYPE_BOOL", value = 0, },
			property_07 = { name = "SpinY", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 0.25, 1, 0.25, }, },
			property_08 = { name = "SpinZ", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 0.125, 1, 0.125, }, },
			property_09 = { name = "SpinEpsilonZ", type = "VARTYPE_FLOAT", value = 0.75, },
			property_10 = { name = "SpinRandom", type = "VARTYPE_BOOL", value = 0, },
		}, style = "STYLE_RING", },
	},
	{
		effect = "root>glow",
		fx = { properties = {
			property_00 = { name = "Texture", type = "VARTYPE_STRING", value = "DATA:ART/FX/GLOW.TGA", },
			property_01 = { name = "Duration", type = "VARTYPE_FLOAT", value = 2, },
			property_02 = { name = "Blending", type = "VARTYPE_INT", value = 2, },
			property_03 = { name = "Radius", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 1, 1, 2, }, },
		}, style = "STYLE_RING", },
	},
}
`

// TestFxSimSynthetic parses a hand written effect and checks the whole
// preview path: a spray births particles that move along +Z and sample the
// radius and colour curves. The two rings are read as different things on
// purpose — the one under the spray describes the particles the emitter
// births, and the one under the root stands on its own as a flat quad, which
// is what the two shapes are in the game.
func TestFxSimSynthetic(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxSyntheticLua))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	s := fx.NewFxSim(f)
	if len(s.Emitters) != 1 {
		t.Fatalf("emitters = %d, want 1", len(s.Emitters))
	}
	if len(s.Emitters[0].Visuals) != 1 {
		t.Fatalf("spray visuals = %d, want 1: the ring under the spray is its particle", len(s.Emitters[0].Visuals))
	}
	if len(s.RingNodes) != 1 {
		t.Fatalf("rings = %d, want 1: the ring under the root stands on its own", len(s.RingNodes))
	}
	if len(s.Anchored) != 0 {
		t.Errorf("anchored = %d, want 0: both rings are rings, not particles", len(s.Anchored))
	}
	for i := 0; i < 120; i++ {
		s.Step(1.0 / 60)
	}
	if len(s.Particles) == 0 {
		t.Fatal("no particles spawned")
	}
	if len(s.Particles) > fx.MaxParticles {
		t.Errorf("particles = %d, over the cap %d", len(s.Particles), fx.MaxParticles)
	}
	sprites := s.Sprites()
	if len(sprites) != len(s.Particles) {
		t.Fatalf("sprites = %d, want the %d particles", len(sprites), len(s.Particles))
	}
	// The particle sprite: new ones are born green and growing.
	p := sprites[0]
	if p.Texture != "DATA:ART/FX/GLOW.TGA" {
		t.Errorf("texture = %q", p.Texture)
	}
	if !p.Additive {
		t.Errorf("blending = additive expected")
	}
	if p.Radius <= 0 {
		t.Errorf("radius = %v", p.Radius)
	}
	if p.Pos[2] <= 0 {
		t.Errorf("particle did not remain above the origin while moving: %+v", p.Pos)
	}
	if !p.Horizontal {
		t.Errorf("non-billboard STYLE_RING particle should stay in the horizontal XY plane")
	}
	if math.Abs(p.Rotation-math.Pi/2) > 1e-6 {
		t.Errorf("particle rotation = %.6f rad, want the authored 0.25-turn SpinY angle %.6f", p.Rotation, math.Pi/2)
	}
	// Colour ramp: green fading out over life.
	if p.G < 0.5 || p.R > 0.5 {
		t.Errorf("colour = (%v %v %v %v), want fading green", p.R, p.G, p.B, p.A)
	}
}

func TestFxSpraySamplesAnimatedOffsetAndStableRingSpin(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxSyntheticLua))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	sim := fx.NewFxSim(f)
	if len(sim.Emitters) != 1 || len(sim.Emitters[0].Visuals) != 1 {
		t.Fatalf("synthetic spray parsed as %d emitters / %d visuals", len(sim.Emitters), len(sim.Emitters[0].Visuals))
	}
	e := &sim.Emitters[0]
	first := e.SpawnIndex
	e.Age = 0
	sim.Spawn(e)
	start := sim.Particles[len(sim.Particles)-1]
	e.Age = e.Duration * 0.8
	sim.Spawn(e)
	late := sim.Particles[len(sim.Particles)-1]
	if start.Pos[2] < 4.9 || start.Pos[2] > 5.1 {
		t.Errorf("initial emitter offset Z = %.3f, want 5", start.Pos[2])
	}
	if late.Pos[2] < 1.7 || late.Pos[2] > 1.9 {
		t.Errorf("late emitter offset Z = %.3f, want interpolated 1.8 at 80%% of its curve", late.Pos[2])
	}
	if start.SpriteID != first || late.SpriteID != first+1 {
		t.Fatalf("particle ids = %d, %d; want monotonic %d, %d", start.SpriteID, late.SpriteID, first, first+1)
	}
	angle := fx.SpriteSpin(start.V, 0, start.SpriteID)
	if again := fx.SpriteSpin(start.V, 0, start.SpriteID); math.Abs(angle-again) > 1e-12 {
		t.Errorf("particle spin changed from %.6f to %.6f between samples", angle, again)
	}
	if angle0, angle1 := fx.SpriteSpin(start.V, 0, 0), fx.SpriteSpin(start.V, 0, 1); math.Abs(angle0-angle1) < 1e-6 {
		t.Errorf("ordered epsilon produced identical neighboring turns %.4f", angle0)
	}
	// Expiring/removing old particles must not recycle ids or perturb the
	// next deterministic spin and emitter-volume sample.
	sim.Particles = sim.Particles[:0]
	e.Age = e.Duration * 0.8
	sim.Spawn(e)
	if got := sim.Particles[0].SpriteID; got != first+2 {
		t.Errorf("spawn after particle removal reused id %d, want %d", got, first+2)
	}
}

// TestFxHorizontalSprayRingUsesItsVerticalSpinAxis checks the axis conversion
// for the non-billboard rings used by phase_in_main_combo: Billboard=false pins
// the sprite to the horizontal plane, SpinY becomes the renderer's world-Z
// heading, and the authored epsilon can spread particles without tilting them.
func TestFxHorizontalSprayRingUsesItsVerticalSpinAxis(t *testing.T) {
	n := &fx.FxNode{
		Effect: "root>spray>ring",
		Style:  "STYLE_RING",
		Props: map[string]fx.FxProp{
			"Billboard":    {Type: "VARTYPE_BOOL", Num: 0},
			"SpinY":        {Type: "VARTYPE_ARRAY_TIMEFLOAT", Curve: []fx.FxKey{{T: 0, V: 5}, {T: 1, V: 5}}},
			"SpinZ":        {Type: "VARTYPE_ARRAY_TIMEFLOAT", Curve: []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0}}},
			"SpinEpsilonZ": {Type: "VARTYPE_FLOAT", Num: 1},
			"SpinRandom":   {Type: "VARTYPE_BOOL", Num: 0},
		},
	}
	v, ok := fx.VisualFromNode(n)
	if !ok {
		t.Fatal("STYLE_RING was not parsed as a particle visual")
	}
	if !v.Horizontal {
		t.Fatal("Billboard=false ring should be pinned perpendicular to the renderer's up axis")
	}
	if got := math.Remainder(fx.SpriteSpin(v, 0, 0), 2*math.Pi); math.Abs(got) > 1e-9 {
		t.Errorf("first ring heading = %.6f rad, want SpinY's five full turns = 0 modulo 2pi", got)
	}
	for i := 1; i < fx.RingSpreadBuckets; i++ {
		got := fx.SpriteSpin(v, 0, i)
		want := 2 * math.Pi * (5 + float64(i)/float64(fx.RingSpreadBuckets-1))
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("particle %d in-plane heading = %.6f rad, want %.6f from SpinY and the stable authored spread", i, got, want)
		}
	}
}

func TestFxStepSamplesEmitterOffsetForSuccessiveBirths(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxSyntheticLua))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	sim := fx.NewFxSim(f)
	if len(sim.Emitters) != 1 {
		t.Fatalf("emitters = %d, want 1", len(sim.Emitters))
	}
	e := &sim.Emitters[0]
	// Isolate the animated emitter offset from launch cone/gravity motion, so
	// each particle's position directly reports its birth height.
	e.Dev = 0
	e.Gravity = 0
	e.Visuals[0].Speed = 0
	e.Visuals[0].SpeedCurve = nil
	sim.Step(0.1)
	if len(sim.Particles) == 0 {
		t.Fatal("first step did not emit any particles")
	}
	firstBirth := sim.Particles[len(sim.Particles)-1].Pos[2]
	firstCount := len(sim.Particles)
	sim.Step(0.1)
	if len(sim.Particles) <= firstCount {
		t.Fatal("second step did not emit any particles")
	}
	secondBirth := sim.Particles[len(sim.Particles)-1].Pos[2]
	if !(firstBirth > secondBirth) {
		t.Errorf("successive emission heights = %.3f then %.3f; expected the animated emitter offset to descend", firstBirth, secondBirth)
	}
	// The authored curve runs from file Y=5 to Y=1; file Y maps to world Z.
	if math.Abs(firstBirth-4.6) > 0.05 || math.Abs(secondBirth-4.2) > 0.05 {
		t.Errorf("sampled emission heights = %.3f then %.3f, want approximately 4.6 then 4.2 world-Z units", firstBirth, secondBirth)
	}
}

// TestFxSimRealFiles is a smoke test: the game's own effects build and
// produce sprites.
func TestFxSimRealFiles(t *testing.T) {
	for _, name := range []string{
		"aeonic_orb_blasts.lua",
		"flame_fx/flame_sprays/flame_ground_strip_combo.lua",
	} {
		f, err := fx.ParseFxLua(readFxLua(t, name))
		if err != nil {
			t.Fatalf("fx.ParseFxLua(%s): %v", name, err)
		}
		s := fx.NewFxSim(f)
		if len(s.Emitters) == 0 && len(s.Anchored) == 0 {
			t.Errorf("%s: nothing to play", name)
			continue
		}
		for i := 0; i < 60; i++ {
			s.Step(1.0 / 60)
		}
		sprites := s.Sprites()
		t.Logf("%s: emitters=%d anchored=%d sprites after 1s=%d",
			name, len(s.Emitters), len(s.Anchored), len(sprites))
		if len(sprites) == 0 {
			t.Errorf("%s: no sprites", name)
		}
	}
}
