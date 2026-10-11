package main

import (
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"testing"

	"github.com/fogleman/fauxgl"
)

// The synthetic cases above pin the rule; this pins the effects the rule was
// found on, so a change that keeps the rule but breaks these two is caught
// against the shipped data rather than against a made-up beam.
//
// Of the nineteen two-marker events in the base game, these are the ones whose
// span runs close to the direction the file wrote its arc on, which is what
// made the bow disappear. necron\ground_lightning_01 arcs on Y between a
// ground marker and a chest marker six units apart and nearly vertical;
// orks\abilities\surge_04_combo arcs between two meks on a diagonal, which
// was already working and must stay working.

const fxProbeModule = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`

type fxBeamCase struct {
	model, first, second, fx string
}

// fxProbeTrees builds the mod tree once for the tests that need it.
func fxProbeTree(t *testing.T) *modtree.ModTree {
	t.Helper()
	path := filepath.FromSlash(fxProbeModule)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no installed module: %v", err)
	}
	tree, err := modtree.BuildModTree(path, "English")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)
	return tree
}

// TestShippedTwoMarkerBeamsBowAcrossTheirSpan resolves each case against the
// real model, runs the effect the event names, and checks that every beam
// drawn bows across the gap rather than along it.
func TestShippedTwoMarkerBeamsBowAcrossTheirSpan(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	cases := []fxBeamCase{
		{`data\art\ebps\environment\cinematics\aa_necron_crawling.whm`,
			"marker_ground_01", "marker_chest_front", `necron\ground_lightning_01`},
		{`data\art\ebps\environment\cinematics\aa_necron_crawling.whm`,
			"marker_ground_04", "marker_chest_front", `necron\ground_lightning_01`},
		{`data\art\ebps\races\orks\troops\mega_armoured_nobz.whm`,
			"mrkr_surge_01a", "mrkr_surge_02a", `orks\abilities\surge_01_combo`},
		{`data\art\ebps\races\orks\troops\mega_armoured_nobz.whm`,
			"mrkr_surge_02a", "mrkr_surge_02b", `orks\abilities\surge_04_combo`},
	}
	checked := 0
	for _, c := range cases {
		f := tree.FindFile(c.model)
		if f == nil {
			t.Errorf("model %s is not in the tree", c.model)
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		m, err := ParseWhmModel(data)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		ia, ib := m.MarkerIndex(c.first), m.MarkerIndex(c.second)
		if ia < 0 || ib < 0 {
			t.Errorf("%s: carries %s=%d and %s=%d", c.model, c.first, ia, c.second, ib)
			continue
		}
		pose := m.RestPose()
		a, okA := m.MarkerWorld(ia, pose, true)
		b, okB := m.MarkerWorld(ib, pose, true)
		if !okA || !okB {
			t.Errorf("%s: markers %s/%s have no world position", c.model, c.first, c.second)
			continue
		}
		ld, err := readFxLuaFile(tree, roots, c.fx)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		ff, err := fx.ParseFxLua(ld)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		sim := fx.NewFxSim(ff)
		// An eye off to the side: a ribbon seen end-on has no width, and
		// with it no bow to measure.
		eye := a.Add(fauxgl.Vector{X: 2, Y: 1, Z: 3})
		for i, r := range sim.Beams(a, b, true) {
			if r.Arc.Length() < 1e-9 {
				continue
			}
			share, peak, ok := arcLateralShare(r, eye)
			if !ok {
				t.Errorf("%s beam %d: produced no comparable geometry", c.fx, i)
				continue
			}
			checked++
			if share < 0.99 {
				t.Errorf("%s beam %d: arc %v bowed %.0f%% across the span, not all of it",
					c.fx, i, r.Arc, share*100)
			}
			if peak < 1e-4 {
				t.Errorf("%s beam %d: the arc bowed %.5f, which is no visible curve at all",
					c.fx, i, peak)
			}
			t.Logf("%-34s beam %d: arc %v bows %.3f across a %.2f unit span (%.0f%%)",
				c.fx, i, r.Arc, peak, a.Sub(b).Length(), share*100)
		}
	}
	if checked == 0 {
		t.Skip("none of the probe effects carried an arc")
	}
	t.Logf("checked %d shipped beams", checked)
}

// The distance half of the two-marker rule, on the same real models: a beam
// that named a second marker and asked to span the gap has to reach exactly
// from one to the other, which is what the effect files' Length of 1 means.
func TestShippedTwoMarkerBeamsSpanTheGap(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	cases := []fxBeamCase{
		{`data\art\ebps\environment\cinematics\aa_necron_crawling.whm`,
			"marker_ground_01", "marker_chest_front", `necron\ground_lightning_01`},
		{`data\art\ebps\races\orks\troops\mega_armoured_nobz.whm`,
			"mrkr_surge_01a", "mrkr_surge_02a", `orks\abilities\surge_01_combo`},
		{`data\art\ebps\environment\cinematics\aa_necron_lord.whm`,
			"mrkr_veil_of_darkness_r", "mrkr_veil_of_darkness_l01", `necron\monolith\tower_arc_05`},
	}
	checked := 0
	for _, c := range cases {
		f := tree.FindFile(c.model)
		if f == nil {
			t.Errorf("model %s is not in the tree", c.model)
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		m, err := ParseWhmModel(data)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		ia, ib := m.MarkerIndex(c.first), m.MarkerIndex(c.second)
		if ia < 0 || ib < 0 {
			t.Errorf("%s: markers %s/%s are missing", c.model, c.first, c.second)
			continue
		}
		pose := m.RestPose()
		a, _ := m.MarkerWorld(ia, pose, true)
		b, _ := m.MarkerWorld(ib, pose, true)
		gap := a.Sub(b).Length()

		ld, err := readFxLuaFile(tree, roots, c.fx)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		ff, err := fx.ParseFxLua(ld)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		sim := fx.NewFxSim(ff)
		for i, r := range sim.Beams(a, b, true) {
			got := r.To.Sub(r.From).Length()
			checked++
			if got < gap-1e-3 || got > gap+1e-3 {
				t.Errorf("%s beam %d: spans %.4f across a gap of %.4f", c.fx, i, got, gap)
			}
		}
	}
	if checked == 0 {
		t.Skip("none of the probe effects drew a beam")
	}
	t.Logf("checked %d shipped beams span their gap exactly", checked)
}
