package main

import (
	"dow-mod-studio/internal/whe"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"github.com/fogleman/fauxgl"
)

// These tests read models that are not on disk at all: the .whm and the .whe
// that names a unit's FX events both live inside the mod's SGA archives, so
// the only way to see a marker in a real file is through the mod tree the app
// itself opens.
//
// uaModule is the module whose ModFolder is UltimateApocalypse_THB_test, the
// folder the viewport is pointed at. That folder carries no .module of its
// own, so this file is how both the app and a test reach it.
const uaModule = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/UA_DC.module`

// warriorWhmPath is a necron_basic_warrior, whose gun tube beams are the ones
// that used to draw across the weapon rather than along it.
const warriorWhmPath = "Data/art/ebps/races/necrons/troops/necron_basic_warrior.whm"

// immortalWhmPath is a necron_immortal, whose gauss markers carry a leading
// space in the file and are asked for without one by the events.
const immortalWhmPath = "Data/art/ebps/races/necrons/troops/necron_immortal.whm"

// sgaTree opens the installed mod, skipping when it is not there so the normal
// test run stays hermetic.
func sgaTree(t *testing.T) *modtree.ModTree {
	t.Helper()
	tree, err := modtree.BuildModTree(uaModule, "English")
	if err != nil {
		t.Skipf("no installed UltimateApocalypse module: %v", err)
	}
	t.Cleanup(tree.Close)
	return tree
}

// sgaModel reads one model out of the archives.
func sgaModel(t *testing.T, path string) *WhmModel {
	t.Helper()
	raw, err := sgaTree(t).ReadFile(path)
	if err != nil {
		t.Skipf("%s not in the archives: %v", path, err)
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("%s parses: %v", path, perr)
	}
	return m
}

// sgaModels lists every .whm under the necron troop folders, which is how the
// sweeps below get a population rather than a hand-picked case.
func sgaModels(t *testing.T) []string {
	t.Helper()
	tree := sgaTree(t)
	var out []string
	seen := map[string]bool{}
	for _, dir := range []string{
		"Data/art/ebps/races/necrons/troops",
		"Data/art/ebps/races/necron/troops",
	} {
		folder := tree.FindFolder(dir)
		if folder == nil {
			continue
		}
		for _, tf := range folder.Files {
			if strings.EqualFold(filepath.Ext(tf.Name), ".whm") && !seen[tf.FullPath] {
				seen[tf.FullPath] = true
				out = append(out, tf.FullPath)
			}
		}
	}
	if len(out) == 0 {
		t.Skip("no necron models in the archives")
	}
	return out
}

// A model that lives only inside an archive has to be readable through the
// tree, markers and all: everything the FX preview hangs off a marker is
// invisible to a test that can only open loose files.
func TestShippedModelInsideAnArchiveParsesWithItsMarkers(t *testing.T) {
	m := sgaModel(t, warriorWhmPath)
	if len(m.Markers) == 0 {
		t.Fatal("the warrior model came out with no markers, so nothing could hang off it")
	}
	if len(m.Bones) == 0 {
		t.Error("the warrior model came out with no bones")
	}
	t.Logf("%s: %d bones, %d markers, %d vertices",
		filepath.Base(warriorWhmPath), len(m.Bones), len(m.Markers), len(m.Vertices))
}

// The gun tube is the case the marker's own geometry settles. Its base and end
// markers are the two ends of the barrel, so the direction between them is the
// direction a beam at that marker has to run: along the tube.
//
// It used to run across the weapon instead. The marker's frame is conjugated
// by diag(-1, 1, 1) — see MarkerFrameAxes — and without it the column this
// test measures pointed sideways.
func TestGunTubeMarkerPointsAlongTheWeapon(t *testing.T) {
	m := sgaModel(t, warriorWhmPath)
	pose := m.RestPose()

	base := m.MarkerIndex("marker_gun_tube_ctr_base")
	end := m.MarkerIndex("marker_gun_tube_ctr_end")
	if base < 0 || end < 0 {
		t.Skip("this model carries no gun tube markers")
	}
	pb, ok1 := m.MarkerWorld(base, pose, true)
	pe, ok2 := m.MarkerWorld(end, pose, true)
	if !ok1 || !ok2 {
		t.Fatal("the tube markers have no position under the rest pose")
	}
	span := pe.Sub(pb)
	if span.Length() < 1e-6 {
		t.Fatal("the tube's two markers are in the same place")
	}
	truth := span.Normalize()

	// The frame's columns are the candidate directions; one of them has to be
	// the barrel's. Scoring all six is what MarkerAxis does internally, and
	// what makes this a check of the frame rather than of one column.
	axX, axY, axZ, ok := m.MarkerFrameAxes(base, pose, true)
	if !ok {
		t.Fatal("the tube marker has no frame of its own")
	}
	best := -2.0
	for _, c := range []fauxgl.Vector{axX, axY, axZ, axX.MulScalar(-1), axY.MulScalar(-1), axZ.MulScalar(-1)} {
		if c.Length() < 1e-6 {
			continue
		}
		if d := c.Normalize().Dot(truth); d > best {
			best = d
		}
	}
	if best < 0.99 {
		t.Errorf("no column of the tube marker's frame points along the barrel: "+
			"best dot %.3f with the base->end direction (%.2f %.2f %.2f)",
			best, truth.X, truth.Y, truth.Z)
	}

	// And the axis a single-marker beam at this marker actually runs along has
	// to follow the tube rather than cross it. That is MarkerBeamAxis's second
	// column; the forward MarkerAxis scores the marker as facing is a different
	// question — here it points straight across the barrel, which is why tube
	// beams used to draw athwart the weapon.
	ax, aok := m.MarkerBeamAxis(base, pose, true)
	if !aok {
		t.Fatal("the tube marker has no frame")
	}
	if d := math.Abs(ax.Dot(truth)); d < 0.99 {
		t.Errorf("the beam axis (%.2f %.2f %.2f) runs across the weapon rather "+
			"than along it: |dot| %.3f with (%.2f %.2f %.2f)",
			ax.X, ax.Y, ax.Z, d, truth.X, truth.Y, truth.Z)
	}
}

// Two of the shipped necron immortals write 44 of their marker names with a
// leading space. An event asks for the name without one, so an exact match
// found nothing: every gauss effect fell back to the model's own axes and the
// tube ran up Z instead of along the barrel.
func TestMarkerLookupIgnoresStrayWhitespace(t *testing.T) {
	for _, path := range sgaModels(t) {
		m := sgaModel(t, path)
		for i := range m.Markers {
			name := m.Markers[i].Name
			trimmed := strings.TrimSpace(name)
			if trimmed == name {
				continue
			}
			if m.MarkerIndex(trimmed) != i {
				t.Errorf("%s: marker %q is unreachable by its own name %q",
					filepath.Base(path), name, trimmed)
			}
			if m.MarkerIndex(name) != i {
				t.Errorf("%s: marker %q is unreachable by its exact name",
					filepath.Base(path), name)
			}
		}
	}
}

// A tube beam that names no second marker has to take its direction from the
// marker's own frame. This is the check that the warrior's tube events really
// are single-marker, which is why they lean on the frame rather than on a
// span between two markers — and that the column read is the one the tube
// runs along, which is MarkerBeamAxis's second column rather than the axis
// MarkerAxis scores the marker as facing.
func TestGunTubeBeamEventsNameOnlyOneMarker(t *testing.T) {
	tree := sgaTree(t)
	m := sgaModel(t, warriorWhmPath)

	raw, err := tree.ReadFile("Data/art/ebps/races/necrons/troops/necron_basic_warrior.whe")
	if err != nil {
		t.Skipf("no warrior .whe in the archives: %v", err)
	}
	events, perr := whe.ParseFxEvents(raw)
	if perr != nil {
		t.Fatalf("the warrior .whe does not parse: %v", perr)
	}

	base := m.MarkerIndex("marker_gun_tube_ctr_base")
	end := m.MarkerIndex("marker_gun_tube_ctr_end")
	pb, _ := m.MarkerWorld(base, m.RestPose(), true)
	pe, _ := m.MarkerWorld(end, m.RestPose(), true)
	truth := pe.Sub(pb).Normalize()

	seen := 0
	for i, ev := range events {
		name := strings.ToLower(ev.Name)
		if !strings.Contains(name, "tube") || !strings.Contains(name, "beam") {
			continue
		}
		m1 := fxEventMarker(m, ev, i)
		m2 := fxBeamOtherMarker(m, ev)
		seen++
		if m1 < 0 {
			t.Errorf("event %q names a marker the model does not carry", ev.Name)
			continue
		}
		if m2 >= 0 {
			continue
		}
		ax, aok := m.MarkerBeamAxis(m1, m.RestPose(), true)
		if !aok {
			t.Errorf("event %q plays at a marker with no frame", ev.Name)
			continue
		}
		if d := math.Abs(ax.Dot(truth)); d < 0.9 {
			t.Errorf("event %q has no second marker, so its beam runs along the "+
				"marker's frame column two, which points across the weapon: |dot| %.3f",
				ev.Name, d)
		}
	}
	if seen == 0 {
		t.Skip("this .whe names no tube beam events")
	}
	t.Logf("checked %d tube beam events", seen)
}
