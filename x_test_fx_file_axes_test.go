package main

import (
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whe"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// deathDustFile is a shipped ground effect used as the axis reference. Its
// Emitter_Volume is (6, 0, 6) — the zero on the file's second component — and
// its Emitter_Offset climbs -2 to +1 along that same component. Flat along an
// axis and climbing along it only reads as a dust cloud off the ground if that
// axis is up, and up is the effect's own Y.
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

// FX vectors are authored in the effect's own Y-up frame and stay there
// through the simulation; the frame the effect is attached to is what turns
// them into model space. The marker-less effect uses fxFallbackFrame, whose
// second axis is the world's up, so a ground cloud is flat along the file's Y
// and, once mapped, flat in the renderer's ground plane as well.
//
// Converting the vectors inside the simulation instead was the double swap
// that made a falling spray drift sideways: its vertical had already become
// the model's Z, and the frame then turned that onto a horizontal axis.
func TestShippedGroundDustLiesFlatInTheRenderersGroundPlane(t *testing.T) {
	sim := dustEmitter(t)

	// Birth a batch and read the spread of the cloud itself, which is what
	// the viewport draws.
	const batch = 400
	for i := 0; i < batch; i++ {
		sim.Spawn(&sim.Emitters[0])
	}
	minLocal, maxLocal := math.Inf(1), math.Inf(-1)
	minWorld, maxWorld := math.Inf(1), math.Inf(-1)
	for _, p := range sim.Particles {
		if p.Pos[1] < minLocal {
			minLocal = p.Pos[1]
		}
		if p.Pos[1] > maxLocal {
			maxLocal = p.Pos[1]
		}
		world := fxFallbackFrame[0].MulScalar(p.Pos[0]).
			Add(fxFallbackFrame[1].MulScalar(p.Pos[1])).
			Add(fxFallbackFrame[2].MulScalar(p.Pos[2]))
		if world.Z < minWorld {
			minWorld = world.Z
		}
		if world.Z > maxWorld {
			maxWorld = world.Z
		}
	}
	if thickness := maxLocal - minLocal; thickness > 1e-6 {
		t.Errorf("dust cloud is %.3f thick along the effect's up axis, want flat: "+
			"the volume's zero extent must land on Y, not on X or Z", thickness)
	}
	if thickness := maxWorld - minWorld; thickness > 1e-6 {
		t.Errorf("dust cloud is %.3f thick along the world's up axis, want flat: "+
			"the fallback frame must carry the file's Y onto Z", thickness)
	}
}

// The offset's sign carries through the authored frame too: -2 down the file's
// up axis is below the marker, not above it.
func TestShippedDustOffsetKeepsItsSignOnTheUpAxis(t *testing.T) {
	sim := dustEmitter(t)
	keys := sim.Emitters[0].Offset
	if len(keys) == 0 {
		t.Fatal("the emitter carries no Emitter_Offset to read")
	}
	if keys[0].Y >= 0 {
		t.Errorf("Emitter_Offset start (%.2f, %.2f, %.2f) is not negative on the file's "+
			"up axis, so the dust cannot start below its marker",
			keys[0].X, keys[0].Y, keys[0].Z)
	}
	sim.Emitters[0].Age = 0
	sim.Spawn(&sim.Emitters[0])
	if got := sim.Particles[len(sim.Particles)-1].Pos[1]; got >= 0 {
		t.Errorf("a particle was born at Y=%.2f, want the file's negative offset to put it "+
			"below the emitter instead of above it", got)
	}
}

// The volume is the cleanest statement of the convention: the shipped ground
// effect writes its zero on the second component, and that zero has to stay on
// the effect's Y axis with both horizontal extents intact.
func TestShippedDustVolumeZeroLandsOnTheUpAxis(t *testing.T) {
	vol := dustEmitter(t).Emitters[0].Volume
	if len(vol) == 0 {
		t.Skip("death_dust_02 declares no Emitter_Volume")
	}
	if vol[0].Y != 0 {
		t.Errorf("Emitter_Volume (%.2f, %.2f, %.2f) has its zero on the file's second "+
			"component, so the slab has to stay flat along Y at %.2f",
			vol[0].X, vol[0].Y, vol[0].Z, vol[0].Y)
	}
	if vol[0].X == 0 || vol[0].Z == 0 {
		t.Errorf("volume (%.2f, %.2f, %.2f) lost its flat spread, want both horizontal "+
			"extents kept", vol[0].X, vol[0].Y, vol[0].Z)
	}
}

func TestFxGroundClassificationUsesEventAndPathHints(t *testing.T) {
	cases := []struct {
		name string
		ev   whe.FxEvent
		path string
		want bool
	}{
		{name: "dust event", ev: whe.FxEvent{Name: "land_dust"}, want: true},
		{name: "ground beam path stays anchored by its own markers", ev: whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "marker", Value: "marker_ground_01"}, {Key: "fx_other_marker", Value: "marker_chest_front"}}}, path: `necron\ground_lightning_01`, want: false},
		{name: "ground effect path", path: `necron\structures\ground_impact`, want: true},
		{name: "ground marker path is normalized", ev: whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "marker", Value: "marker_surface_01_ground"}}}, want: true},
		{name: "ground attachment marker", ev: whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "marker", Value: "marker_ground_01"}}}, want: true},
		{name: "ordinary muzzle effect", ev: whe.FxEvent{Name: "muzzle_flash"}, path: `weapon\muzzle_flash`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fxIsGroundEffect(tc.ev, tc.path); got != tc.want {
				t.Errorf("fxIsGroundEffect(%q, %q) = %v, want %v", tc.ev.Name, tc.path, got, tc.want)
			}
		})
	}
}

func TestGroundFXOriginKeepsHorizontalAttachment(t *testing.T) {
	origin := fauxgl.Vector{X: 3, Y: -4, Z: 12}
	got := fxGroundOrigin(origin, -2)
	want := fauxgl.Vector{X: 3, Y: -4, Z: -2}
	if got != want {
		t.Errorf("grounded origin = %+v, want %+v", got, want)
	}
	if origin.Z != 12 {
		t.Errorf("grounding mutated its input: %+v", origin)
	}
}

// A beam's Arc and Offset are written by the same files as the emitter's
// volume and offset, so they are local vectors in the same Y-up frame. The
// simulation leaves them as authored and the attachment frame maps them into
// model space; converting them here as well is exactly the second swap that
// laid a vertical spray along the ground.
func TestShippedBeamArcIsMappedByTheAttachmentFrame(t *testing.T) {
	// aspect_lightning_prod writes Arc (0.16866, 0.44179, 0.67612): its third
	// component is the largest, which is the file's depth, so the frame has to
	// carry it onto the world's Y and keep the bow across a vertical beam
	// rather than collapsing it onto the beam's own length. Its beam also
	// sits under a spray, so this doubles as the check that the walk
	// descends into one.
	const file = "aspect_lightning_prod.lua"
	f, err := fx.ParseFxLua(readFxLua(t, file))
	if err != nil {
		t.Fatalf("%s does not parse: %v", file, err)
	}
	sim := fx.NewFxSim(f)
	if sim == nil || len(sim.BeamNodes) == 0 {
		t.Fatalf("%s carries a STYLE_BEAM node the simulation never collected", file)
	}
	b := &sim.BeamNodes[0]
	if len(b.Arc) == 0 {
		t.Fatalf("%s no longer writes an Arc to test", file)
	}

	from := fauxgl.Vector{X: 0, Y: 0, Z: 0}
	to := fauxgl.Vector{X: 0, Y: 0, Z: 8}
	rb := fx.NewBeamRibbon(*b, from, to, 0, true)
	authored := b.Arc[0]
	if got, want := rb.Arc, (fauxgl.Vector{X: authored.X, Y: authored.Y, Z: authored.Z}); got != want {
		t.Errorf("the beam's arc came out as %+v, want the authored local vector %+v", got, want)
	}
	// Mapped the way the overlay maps it, the arc has to keep a component
	// across the span of a vertical beam.
	mapped := fxFallbackFrame[0].MulScalar(rb.Arc.X).
		Add(fxFallbackFrame[1].MulScalar(rb.Arc.Y)).
		Add(fxFallbackFrame[2].MulScalar(rb.Arc.Z))
	dir := to.Sub(from).Normalize()
	bow := mapped.Sub(dir.MulScalar(mapped.Dot(dir)))
	if bow.Length() < 1e-6 {
		t.Errorf("arc %+v lies entirely along the span of a vertical beam after mapping, so "+
			"the ribbon draws straight: the attachment frame was not applied", rb.Arc)
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
