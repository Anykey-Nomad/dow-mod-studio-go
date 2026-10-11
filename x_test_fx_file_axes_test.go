package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// deathDustFile is a shipped ground effect used as the axis reference. Its
// Emitter_Volume is (6, 0, 6) — the zero on the file's second component — and
// its Emitter_Offset climbs -2 to +1 along that same component. Flat along an
// axis and climbing along it only reads as a dust cloud off the ground if that
// axis is the vertical one.
const deathDustFile = "necron/structures/summoning_core/death_dust_02.lua"

// dustEmitter parses the reference effect and returns its root emitter.
func dustEmitter(t *testing.T) *fx.FxSim {
	t.Helper()
	f, err := fx.ParseFxLua(readFxLua(t, deathDustFile))
	if err != nil {
		t.Fatalf("death_dust_02 does not parse: %v", err)
	}
	sim := fx.NewFxSim(f)
	if sim == nil || len(sim.Emitters) == 0 {
		t.Fatal("death_dust_02 produced no emitter to spawn from")
	}
	return sim
}

// The FX files are written Y up and this renderer is Z up, so a ground cloud
// has to end up lying flat in the renderer's own ground plane with nothing
// along Z. Reading the volume the other way stood it on edge as a wall and
// sent the offset sideways, which is what the viewport showed.
func TestShippedGroundDustLiesFlatInTheRenderersGroundPlane(t *testing.T) {
	sim := dustEmitter(t)

	// Birth a batch and read the spread of the cloud itself, which is what
	// the viewport draws.
	const batch = 400
	for i := 0; i < batch; i++ {
		sim.Spawn(&sim.Emitters[0])
	}
	var minZ, maxZ float64 = math.Inf(1), math.Inf(-1)
	for _, p := range sim.Particles {
		if p.Pos[2] < minZ {
			minZ = p.Pos[2]
		}
		if p.Pos[2] > maxZ {
			maxZ = p.Pos[2]
		}
	}
	if thickness := maxZ - minZ; thickness > 1e-6 {
		t.Errorf("dust cloud is %.3f thick along the renderer's up axis, want flat: "+
			"the volume's zero extent must land on Z, not on Y", thickness)
	}
}

// The swap has to carry the offset's sign too: -2 down the file's up axis is
// below the marker, not above it.
func TestShippedDustOffsetKeepsItsSignAcrossTheAxisSwap(t *testing.T) {
	keys := dustEmitter(t).Emitters[0].Offset
	if len(keys) == 0 {
		t.Fatal("the emitter carries no Emitter_Offset to convert")
	}
	got := fx.FileVec(fauxgl.Vector{X: keys[0].X, Y: keys[0].Y, Z: keys[0].Z})
	if got.Z >= 0 {
		t.Errorf("Emitter_Offset start (%.2f, %.2f, %.2f) converts to Z=%.2f, want the "+
			"file's -2 down-axis to stay below the marker after the swap",
			keys[0].X, keys[0].Y, keys[0].Z, got.Z)
	}
}

// The volume is the cleanest statement of the convention: the shipped ground
// effect writes its zero on the second component, and that zero has to arrive
// on the renderer's up axis with both horizontal extents intact.
func TestShippedDustVolumeZeroLandsOnTheUpAxis(t *testing.T) {
	vol := dustEmitter(t).Emitters[0].Volume
	if len(vol) == 0 {
		t.Skip("death_dust_02 declares no Emitter_Volume")
	}
	got := fx.FileVec(fauxgl.Vector{X: vol[0].X, Y: vol[0].Y, Z: vol[0].Z})
	if got.Z != 0 {
		t.Errorf("Emitter_Volume (%.2f, %.2f, %.2f) has its zero on the file's second "+
			"component, so after the swap the slab stands on edge at Z=%.2f, want Z=0",
			vol[0].X, vol[0].Y, vol[0].Z, got.Z)
	}
	if got.X == 0 || got.Y == 0 {
		t.Errorf("converted volume (%.2f, %.2f, %.2f) lost its flat spread, want both "+
			"horizontal extents kept", got.X, got.Y, got.Z)
	}
}

// fx.FileVec is a swap of the second and third components: the FX files keep
// their vertical on the second one and the renderer keeps its own on the third.
func TestFxFileVecMovesTheSecondComponentOntoUp(t *testing.T) {
	got := fx.FileVec(fauxgl.Vector{X: 1, Y: 2, Z: 3})
	want := fauxgl.Vector{X: 1, Y: 3, Z: 2}
	if got != want {
		t.Errorf("fx.FileVec((1,2,3)) = %+v, want %+v", got, want)
	}
}

// A beam's Arc and Offset are written by the same files as the emitter's
// volume and offset, so they are converted the same way. This one used to go
// into the ribbon raw, on a note that said most beams write their arc on Y;
// across the shipped effects no arc is written on a single axis at all, and
// where one is written on all three the third is the largest every time.
func TestShippedBeamArcIsConvertedIntoTheRenderersAxes(t *testing.T) {
	// aspect_lightning_prod writes Arc (0.16866, 0.44179, 0.67612): its third
	// component is the largest, which is the file's depth, so once converted
	// it has to be the renderer's Y and the bow has to stay a real bow rather
	// than collapsing onto the beam's own length. Its beam also sits under a
	// spray, so this doubles as the check that the walk descends into one.
	file := "aspect_lightning_prod.lua"
	f, err := fx.ParseFxLua(readFxLua(t, file))
	if err != nil {
		t.Fatalf("%s does not parse: %v", file, err)
	}
	sim := fx.NewFxSim(f)
	if sim == nil || len(sim.BeamNodes) == 0 {
		t.Fatalf("%s carries a STYLE_BEAM node the simulation never collected", file)
	}
	b := &sim.BeamNodes[0]

	from := fauxgl.Vector{X: 0, Y: 0, Z: 0}
	to := fauxgl.Vector{X: 0, Y: 0, Z: 8}
	rb := fx.NewBeamRibbon(*b, from, to, 0, true)
	if rb.Arc.Length() < 1e-6 {
		t.Fatal("the beam came out with no arc at all")
	}
	// A beam running straight up spends any arc written along the file's
	// depth on its own length. The part across the span is what is left, and
	// it is the whole of what the arc means.
	dir := to.Sub(from).Normalize()
	bow := rb.Arc.Sub(dir.MulScalar(rb.Arc.Dot(dir)))
	if bow.Length() < 1e-6 {
		t.Errorf("arc %+v lies entirely along the span of a vertical beam, so the "+
			"ribbon draws straight: the file's axes were not converted", rb.Arc)
	}

	// And the offset, which shifts where the beam starts, converts with it.
	off := sim.BeamNodes[0].Offset
	if len(off) > 0 {
		got := fx.FileVec(fauxgl.Vector{X: off[0].X, Y: off[0].Y, Z: off[0].Z})
		if got.Z == 0 && off[0].Y != 0 {
			t.Errorf("Emitter offset (%.2f, %.2f, %.2f) has its only vertical on the "+
				"file's second axis and lost it: converted %+v", off[0].X, off[0].Y, off[0].Z, got)
		}
	}
}

// A beam is only ever drawn if the walk reaches it, and the walk used to stop
// at a STYLE_SPRAY. This file is a spray holding a combo holding the beam, so
// before the fix its beam parsed and was reachable by name yet never reached
// the simulation: the effect was listed in the viewport and drew nothing.
func TestShippedBeamUnderASprayIsCollected(t *testing.T) {
	f, err := fx.ParseFxLua(readFxLua(t, "aspect_lightning_prod.lua"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := 0
	for i := range f.Nodes {
		if f.Nodes[i].Style == "STYLE_BEAM" {
			want++
		}
	}
	if want == 0 {
		t.Skip("the file no longer carries a beam node")
	}
	sim := fx.NewFxSim(f)
	if len(sim.BeamNodes) != want {
		t.Errorf("the file declares %d beam node(s), the simulation collected %d: "+
			"the walk is not descending into a spray's subtree", want, len(sim.BeamNodes))
	}
}
