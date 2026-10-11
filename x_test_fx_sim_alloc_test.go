package main

import (
	"dow-mod-studio/internal/fx"
	"testing"

	"github.com/fogleman/fauxgl"
)

// BenchmarkFxDrawsPerFrame measures what one frame of an effect costs to ask
// for: the sprite list, the beams and the rings.
//
// The counts are the point, not the time. Playback asks for these for every
// effect on show sixty times a second, so a frame that allocates is a
// collection a moment later, and a collection in the middle of an animation
// is the stutter a reader feels rather than sees. The lists are written into
// buffers kept on the simulation, so a frame after the first should not
// allocate at all.
func BenchmarkFxDrawsPerFrame(b *testing.B) {
	sim := &fx.FxSim{}
	sim.Emitters = []fx.Emitter{{
		Rate:     200,
		Duration: 2,
		Volume:   []fx.FxVec3Key{{T: 0, X: 2, Y: 1, Z: 2}},
		Visuals: []fx.Visual{{
			Texture:  "fx\\smoke.tga",
			Lifetime: 2,
			Radius:   []fx.FxKey{{V: 0.4}},
			Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
		}},
	}}
	sim.BeamNodes = []fx.Beam{{
		Texture:  "fx\\beam.tga",
		Lifetime: 2,
		Width:    []fx.FxKey{{V: 0.2}},
	}}
	sim.RingNodes = []fx.Ring{{
		Texture:  "fx\\ring.tga",
		Lifetime: 2,
		Radius:   []fx.FxKey{{V: 1}},
	}}
	for i := 0; i < 4; i++ {
		sim.Step(1.0 / 60)
	}
	if len(sim.Particles) == 0 {
		b.Fatal("the effect produced no particles to draw")
	}
	up := fauxgl.Vector{Z: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sim.Step(1.0 / 60)
		_ = sim.Sprites()
		_ = sim.Beams(fauxgl.Vector{}, fauxgl.Vector{X: 1}, false)
		_ = sim.Rings(fauxgl.Vector{}, fauxgl.Vector{X: 1}, up, up, up)
	}
}
