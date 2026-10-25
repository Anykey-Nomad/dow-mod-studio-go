package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// The phase effects are the report this was fixed for: phase_in's smoke rings
// drop down through the monolith and phase_out's climb back up. Both events
// name no marker (fx_motion_orphan), so the effect is laid out in
// fxFallbackFrame, which maps the simulation's Y up axis onto the world's Z.
//
// The spray writes its Emitter_Offset along the file's second component:
// phase_in runs 5 -> 0 and phase_out 0 -> 5. Reading the simulated vectors as
// model-space and then mapping them through that frame swapped the vertical a
// second time, so both sprays drifted sideways along the ground instead of
// falling and rising. This pins the direction the first emitter's particles
// are born in, which has no volume or deviation to hide it.
func TestPhaseRingSpraysDescendAndRiseVertically(t *testing.T) {
	cases := []struct {
		file string
		// descends is what phase_in does; phase_out climbs.
		descends bool
	}{
		{file: "phase_in_main_combo", descends: true},
		{file: "phase_out_main_combo", descends: false},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			f, err := fx.ParseFxLua(readFxLua(t, "necron/monolith/"+c.file+".lua"))
			if err != nil {
				t.Fatalf("parse %s: %v", c.file, err)
			}
			sim := fx.NewFxSim(f)
			if len(sim.Emitters) == 0 {
				t.Fatalf("%s has no emitter to play", c.file)
			}
			// The first emitter of both files writes Emitter_Volume away and
			// Particle_Speed 0, so a particle born at a given moment sits
			// exactly at the authored offset: no spread, no launch cone.
			e := &sim.Emitters[0]
			birthAt := func(phase float64) fauxgl.Vector {
				e.Age = e.Duration * phase
				sim.Spawn(e)
				p := sim.Particles[len(sim.Particles)-1]
				return fxFallbackFrame[0].MulScalar(p.Pos[0]).
					Add(fxFallbackFrame[1].MulScalar(p.Pos[1])).
					Add(fxFallbackFrame[2].MulScalar(p.Pos[2]))
			}
			early, late := birthAt(0.1), birthAt(0.9)
			rise := late.Z - early.Z
			if c.descends && rise > -1 {
				t.Errorf("%s rings rose %.2f world units (from Z=%.2f to Z=%.2f); they are "+
					"authored to descend the effect's up axis", c.file, rise, early.Z, late.Z)
			}
			if !c.descends && rise < 1 {
				t.Errorf("%s rings rose only %.2f world units (from Z=%.2f to Z=%.2f); they "+
					"are authored to climb the effect's up axis", c.file, rise, early.Z, late.Z)
			}
			// The offset has no horizontal component at all, so anything the
			// attachment frame puts across the ground is the double swap
			// coming back: the vertical would then arrive as a sideways
			// trail, which is the bug this test exists for.
			for _, p := range []fauxgl.Vector{early, late} {
				if drift := p.X*p.X + p.Y*p.Y; drift > 0.25 {
					t.Errorf("%s ring born at (%+.2f %+.2f %+.2f): it drifted %.2f units "+
						"across the ground from a purely vertical offset",
						c.file, p.X, p.Y, p.Z, math.Sqrt(drift))
				}
			}
		})
	}
}
