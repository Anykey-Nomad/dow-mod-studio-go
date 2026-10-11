//go:build windows

package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/fogleman/fauxgl"
)

// The necron monolith's crystal is a set of STYLE_RING panels, and each panel's
// face direction is its SpinZ: crystal_interior.lua carries SpinZ = 1.06667,
// which is a turn of about 383 degrees. Nothing animates it — it is a fixed
// orientation of one panel, and it is what tells the six faces of a crystal
// apart from one another.
//
// So the angle in an FX file is a Spin curve, in turns over the effect's life,
// and it is read per style. This checks that the shipped crystal's own value
// survives the parse and comes out as the turn it names.
func TestCrystalRingSpinIsReadFromTheFile(t *testing.T) {
	const path = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE/Data/art/fx/necron/monolith/crystal_interior.lua`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no crystal file: %v", err)
	}
	file, err := fx.ParseFxLua(data)
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	nodes := file.Nodes
	if len(nodes) == 0 {
		t.Fatalf("parsed no nodes out of %d bytes", len(data))
	}
	var rings int
	for i := range nodes {
		n := &nodes[i]
		r, ok := fx.RingFromNode(n)
		if !ok {
			continue
		}
		rings++
		if len(r.SpinZ) == 0 {
			t.Errorf("%s: ring has no SpinZ curve, so it can only be drawn square to its marker", n.Effect)
			continue
		}
		u := 0.0
		turns := fx.SampleCurve(r.SpinZ, u)
		deg := turns * 360
		t.Logf("%s: SpinZ at u=0 is %.5f turns = %.2f degrees", n.Effect, turns, deg)
		if math.Abs(turns-1.06667) > 0.01 {
			t.Errorf("%s: SpinZ came out %.5f, want about 1.06667 (383 degrees)", n.Effect, turns)
		}
	}
	if rings == 0 {
		t.Errorf("the crystal parsed %d nodes but none of them was a ring", len(nodes))
	}
}

// Being read is one thing; being used is another. A Spin curve is a number of
// turns taken up along the effect's life, so a curve reaching a quarter turn by
// the end has to have turned the quad a quarter turn by then.
func TestRingSpinIsAppliedOverTheEffectLife(t *testing.T) {
	r := fx.Ring{
		Radius: []fx.FxKey{{T: 0, V: 1}, {T: 1, V: 1}},
		SpinZ:  []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0.25}},
	}
	centre := fauxgl.Vector{}
	right := fauxgl.Vector{X: 1}
	up := fauxgl.Vector{Y: 1}
	at := func(u float64) fauxgl.Vector {
		out := fx.RingRibbon(r, centre, fauxgl.Vector{Z: 1}, right, up, fauxgl.Vector{Y: 1}, u, 0)
		return out.Across.Normalize()
	}
	// The angle the geometry actually turned through, from the first sample to
	// the last, measured between the two faces themselves rather than in any
	// one plane: a ring's quad is laid out in the marker's own frame, whose
	// axes are not the viewer's.
	first, last := at(0), at(1)
	deg := math.Acos(clampFloat64(first.Dot(last), -1, 1)) * 180 / math.Pi
	t.Logf("a curve of 0.25 turns turned the quad by %.2f degrees, want 90", deg)
	if math.Abs(deg-90) > 1 {
		t.Errorf("a 0.25-turn spin curve turned the quad by %.2f degrees, not 90: the spin is being applied as the value at the sampled moment rather than accumulated", deg)
	}
}

// SpinEpsilon is the half of the crystal that Spin alone does not explain. The
// panels set SpinEpsilonZ = 1 with SpinRandom = 1, which asks for the faces to
// be scattered over a whole turn rather than sitting at the angles the Spin
// curves name. A preview that reads only Spin draws every panel square and the
// crystal comes out as one flat sheet.
func TestRingReadsItsSpinEpsilonAndSpinRandom(t *testing.T) {
	const path = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE/Data/art/fx/necron/monolith/crystal_top_ctr.lua`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no crystal file: %v", err)
	}
	file, err := fx.ParseFxLua(data)
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	var rings int
	for i := range file.Nodes {
		r, ok := fx.RingFromNode(&file.Nodes[i])
		if !ok {
			continue
		}
		rings++
		if len(r.EpsZ) == 0 {
			t.Errorf("%s: no SpinEpsilonZ, so its face cannot be spread", file.Nodes[i].Effect)
		}
		if !r.SpinRandom {
			t.Errorf("%s: SpinRandom was not read", file.Nodes[i].Effect)
		}
		if got := fx.SampleCurve(r.EpsZ, 0); math.Abs(got-1) > 0.001 {
			t.Errorf("%s: SpinEpsilonZ came out %.4f, want 1", file.Nodes[i].Effect, got)
		}
	}
	if rings == 0 {
		t.Fatalf("the crystal file parsed %d nodes but none was a ring", len(file.Nodes))
	}
	t.Logf("%s: %d ring(s), SpinEpsilon and SpinRandom both read", filepath.Base(path), rings)
}

// A spread has to be inside 0..1 and it has to be the same every time it is
// asked for. The second half is the one that matters on screen: a panel that
// drew a different face each frame would flicker, and the crystal's faces
// would never settle.
func TestRingSpreadIsInRangeAndStable(t *testing.T) {
	r := fx.Ring{EpsZ: []fx.FxKey{{T: 0, V: 1}}, SpinRandom: true}
	for i := 0; i < 32; i++ {
		first := r.Spread(i)
		if first < 0 || first >= 1 {
			t.Errorf("panel %d: share %.6f is outside 0..1", i, first)
		}
		if again := r.Spread(i); again != first {
			t.Errorf("panel %d: share changed from %.9f to %.9f between calls", i, first, again)
		}
	}
	// Neighbouring panels must not land on the same face, which is the whole
	// point of spreading them.
	seen := map[float64]int{}
	for i := 0; i < 8; i++ {
		seen[r.Spread(i)] = i
	}
	if len(seen) < 6 {
		t.Errorf("eight panels shared only %d distinct faces: %v", len(seen), seen)
	}
}

// An ordered spread is what a node without SpinRandom gets: the faces are laid
// out in slices rather than scattered. A ring with no epsilon at all is asked
// for nothing, so it must be given nothing.
func TestRingSpreadOrderedAndAbsent(t *testing.T) {
	none := fx.Ring{}
	if got := none.Spread(3); got != 0 {
		t.Errorf("a ring with no SpinEpsilon was given a share of %v, want 0", got)
	}
	ordered := fx.Ring{EpsZ: []fx.FxKey{{T: 0, V: 1}}}
	first := ordered.Spread(0)
	second := ordered.Spread(1)
	if first == second {
		t.Errorf("two ordered panels got the same share %v, so they draw the same face", first)
	}
	t.Logf("ordered shares: panel 0 = %.3f, panel 1 = %.3f", first, second)
}

// The epsilon has to reach the geometry, not just the struct: a panel told to
// spread over a quarter turn must come out of fx.RingRibbon turned by it.
//
// The spread is sampled at a quarter rather than at 1, because a full turn
// brings a panel back to where it started - a spread of exactly one looks
// identical to no spread at all, which is the one value that cannot tell the
// two apart.
func TestRingRibbonGeometryRespondsToTheSpread(t *testing.T) {
	r := fx.Ring{
		Radius: []fx.FxKey{{T: 0, V: 1}, {T: 1, V: 1}},
		SpinZ:  []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0}},
		EpsZ:   []fx.FxKey{{T: 0, V: 1}},
	}
	centre := fauxgl.Vector{}
	right := fauxgl.Vector{X: 1}
	up := fauxgl.Vector{Y: 1}
	at := func(spread float64) fauxgl.Vector {
		return fx.RingRibbon(r, centre, fauxgl.Vector{Z: 1}, right, up, fauxgl.Vector{Y: 1}, 0, spread).Across.Normalize()
	}
	none, quarter := at(0), at(0.25)
	dot := none.Dot(quarter)
	t.Logf("across with no spread %v, with a quarter of the spread %v, dot=%.4f (90 degrees apart is 0)", none, quarter, dot)
	if math.Abs(dot) > 0.01 {
		t.Errorf("a quarter of a whole-turn spread left the panel at dot=%.4f with itself: the epsilon never reached the geometry", dot)
	}
	// And a full turn really does come back to the start, which is why the
	// test above cannot use it.
	if full, back := at(1), none; math.Abs(full.Dot(back)-1) > 0.01 {
		t.Errorf("a whole turn did not return the panel to where it started: dot=%.4f", full.Dot(back))
	}
}
