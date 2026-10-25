package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"

	"github.com/fogleman/fauxgl"
)

// The necron monolith's gun highlights and hull lights played their beams in
// the wrong direction: the gun ones crossed the barrels, and the corner lights
// left the hull horizontally instead of running down the chamfer they sit on.
//
// Both are single-marker beams, so the direction comes from the marker's own
// frame — the second column, see whm.MarkerBeamAxis — and until it was read
// that way the renderer used the axis MarkerAxis scores a marker as facing,
// which for a gun is across the barrel and for a corner light is straight out
// of the model.
//
// These are the events the report named, resolved on the shipped NPC monolith
// the viewer loads for races/npc/necron_monolith.

// monolithFxEvent is one event of the monolith's .whe, with the marker and
// effect an event entry names.
type monolithFxEvent struct {
	name   string
	marker string
	fx     string
}

// monolithEvents reads the NPC monolith's .whe out of the archives.
func monolithEvents(t *testing.T, tree *modtree.ModTree) []monolithFxEvent {
	t.Helper()
	raw, err := tree.ReadFile("Data/art/ebps/races/npc/troops/necron_monolith.whe")
	if err != nil {
		t.Skipf("no NPC monolith .whe in the archives: %v", err)
	}
	events, perr := whe.ParseFxEvents(raw)
	if perr != nil {
		t.Fatalf("the NPC monolith .whe does not parse: %v", perr)
	}
	out := make([]monolithFxEvent, 0, len(events))
	for _, ev := range events {
		marker, _ := ev.First("marker")
		fxPath, _ := ev.First("fx")
		out = append(out, monolithFxEvent{
			name:   strings.ToLower(strings.TrimSpace(ev.Name)),
			marker: strings.TrimSpace(marker),
			fx:     strings.TrimSpace(fxPath),
		})
	}
	return out
}

// monolithEvent finds one event by name. Names are exact: base_light_l_05 has a
// _lwr twin on the lower light strip, and the two play different effects.
func monolithEvent(t *testing.T, events []monolithFxEvent, name string) monolithFxEvent {
	t.Helper()
	for _, ev := range events {
		if ev.name == strings.ToLower(name) {
			return ev
		}
	}
	t.Fatalf("the monolith .whe carries no event %q", name)
	return monolithFxEvent{}
}

// gunBarrel returns the direction from the barrel's base marker to its muzzle.
// The monolith writes marker_gun_<corner>_base_<n> and marker_gun_<corner>_muzzle_<n>
// in pairs, and that span is the barrel itself: measured over the 63 tube pairs
// of the installed tree it is what the markers' second frame column follows in
// all 63. A highlight marker shares the gun and the index, whatever its own
// name in the middle is.
// gun_fl_hi_01 names marker_gun_fr_hi_10 — a near-copy of the right gun's
// marker, in the left gun's event — so a marker without a pair of its own is
// measured against the barrels on its bone.
func gunBarrel(m *WhmModel, pose whm.Pose, marker string) (fauxgl.Vector, bool) {
	dir := func(base, muzzle int) (fauxgl.Vector, bool) {
		a, okA := m.MarkerWorld(base, pose, true)
		b, okB := m.MarkerWorld(muzzle, pose, true)
		if !okA || !okB {
			return fauxgl.Vector{}, false
		}
		span := b.Sub(a)
		if span.Length() < 1e-6 {
			return fauxgl.Vector{}, false
		}
		return span.Normalize(), true
	}

	lower := strings.ToLower(marker)
	parts := strings.Split(lower, "_")
	// marker_gun_<corner>_<what>_<n>
	if len(parts) >= 5 && parts[0] == "marker" && parts[1] == "gun" {
		corner, index := parts[2], parts[len(parts)-1]
		base := m.MarkerIndex("marker_gun_" + corner + "_base_" + index)
		muzzle := m.MarkerIndex("marker_gun_" + corner + "_muzzle_" + index)
		if base >= 0 && muzzle >= 0 {
			if v, ok := dir(base, muzzle); ok {
				return v, true
			}
		}
	}

	i := m.MarkerIndex(marker)
	if i < 0 {
		return fauxgl.Vector{}, false
	}
	bone := strings.ToLower(strings.TrimSpace(m.Markers[i].Parent))
	for j := range m.Markers {
		name := strings.ToLower(strings.TrimSpace(m.Markers[j].Name))
		if !strings.Contains(name, "_base_") || strings.ToLower(strings.TrimSpace(m.Markers[j].Parent)) != bone {
			continue
		}
		muzzle := m.MarkerIndex(strings.Replace(name, "_base_", "_muzzle_", 1))
		if muzzle < 0 {
			continue
		}
		if v, ok := dir(j, muzzle); ok {
			return v, true
		}
	}
	return fauxgl.Vector{}, false
}

// TestMonolithGunHilightBeamsRunAlongTheirBarrels covers gun_fl_hi_01..03 and
// gun_rl_energy_01..03: every beam the effect draws has to run the way the
// barrel under it runs, not across it.
func TestMonolithGunHilightBeamsRunAlongTheirBarrels(t *testing.T) {
	tree := sgaTree(t)
	raw, err := tree.ReadFile("Data/art/ebps/races/npc/troops/necron_monolith.whm")
	if err != nil {
		t.Skipf("no NPC monolith model in the archives: %v", err)
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("the NPC monolith model does not parse: %v", perr)
	}
	pose := m.RestPose()
	events := monolithEvents(t, tree)
	roots := materialSearchRoots(tree)

	for _, name := range []string{
		"gun_fl_hi_01", "gun_fl_hi_02", "gun_fl_hi_03",
		"gun_rl_energy_01", "gun_rl_energy_02", "gun_rl_energy_03",
	} {
		t.Run(name, func(t *testing.T) {
			ev := monolithEvent(t, events, name)
			i := m.MarkerIndex(ev.marker)
			if i < 0 {
				t.Fatalf("event %q plays at %q, a marker the model does not carry", name, ev.marker)
			}
			barrel, ok := gunBarrel(m, pose, ev.marker)
			if !ok {
				t.Fatalf("no base/muzzle pair around %q to measure the barrel against", ev.marker)
			}
			origin, _ := m.MarkerWorld(i, pose, true)
			axis, ok := m.MarkerBeamAxis(i, pose, true)
			if !ok {
				t.Fatalf("marker %q has no frame", ev.marker)
			}
			if d := math.Abs(axis.Dot(barrel)); d < 0.99 {
				t.Errorf("the beam at %q runs (%.2f %.2f %.2f), |dot| %.3f with the barrel "+
					"(%.2f %.2f %.2f): it crosses the gun instead of running along it",
					ev.marker, axis.X, axis.Y, axis.Z, d, barrel.X, barrel.Y, barrel.Z)
			}

			// And the effect the event plays draws its beams that way too,
			// rather than taking a direction from the file.
			data, err := readFxLuaFile(tree, roots, ev.fx)
			if err != nil {
				t.Skipf("effect %q is not readable: %v", ev.fx, err)
			}
			ff, ferr := fx.ParseFxLua(data)
			if ferr != nil {
				t.Fatalf("effect %q does not parse: %v", ev.fx, ferr)
			}
			longest := fauxgl.Vector{}
			for _, rb := range fx.NewFxSim(ff).Beams(origin, origin.Add(axis), false) {
				if d := rb.To.Sub(rb.From); d.Length() > longest.Length() {
					longest = d
				}
			}
			if longest.Length() < 1e-6 {
				t.Fatalf("effect %q draws no beam to check", ev.fx)
			}
			if d := math.Abs(longest.Normalize().Dot(barrel)); d < 0.9 {
				t.Errorf("the longest beam of %q runs |dot| %.3f with the barrel: the effect "+
					"is drawing athwart the gun", ev.fx, d)
			}
		})
	}
}

// TestMonolithBaseLightBeamsFallAlongTheChamfer covers base_light_l_05 and
// base_light_l_06, whose markers hang off the root bone. MarkerAxis reads a
// root marker by aiming away from the model's middle, which put the beam
// horizontally out of the hull; the chamfer they sit on is the markers' second
// column and runs down and outward.
func TestMonolithBaseLightBeamsFallAlongTheChamfer(t *testing.T) {
	tree := sgaTree(t)
	raw, err := tree.ReadFile("Data/art/ebps/races/npc/troops/necron_monolith.whm")
	if err != nil {
		t.Skipf("no NPC monolith model in the archives: %v", err)
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("the NPC monolith model does not parse: %v", perr)
	}
	pose := m.RestPose()
	events := monolithEvents(t, tree)
	roots := materialSearchRoots(tree)

	for _, name := range []string{"base_light_l_05", "base_light_l_06"} {
		t.Run(name, func(t *testing.T) {
			ev := monolithEvent(t, events, name)
			i := m.MarkerIndex(ev.marker)
			if i < 0 {
				t.Fatalf("event %q plays at %q, a marker the model does not carry", name, ev.marker)
			}
			axis, ok := m.MarkerBeamAxis(i, pose, true)
			if !ok {
				t.Fatalf("marker %q has no frame", ev.marker)
			}
			if axis.Z > -0.9 {
				t.Errorf("the beam at %q runs (%.2f %.2f %.2f): it is not falling down the "+
					"chamfer, and a horizontal one leaves the hull sideways",
					ev.marker, axis.X, axis.Y, axis.Z)
			}
			if axis.X < 0 {
				t.Errorf("the beam at %q runs (%.2f %.2f %.2f): it points back into the "+
					"hull rather than down the outside of the chamfer",
					ev.marker, axis.X, axis.Y, axis.Z)
			}

			origin, _ := m.MarkerWorld(i, pose, true)
			data, err := readFxLuaFile(tree, roots, ev.fx)
			if err != nil {
				t.Skipf("effect %q is not readable: %v", ev.fx, err)
			}
			ff, ferr := fx.ParseFxLua(data)
			if ferr != nil {
				t.Fatalf("effect %q does not parse: %v", ev.fx, ferr)
			}
			beams := fx.NewFxSim(ff).Beams(origin, origin.Add(axis), false)
			if len(beams) == 0 {
				t.Fatalf("effect %q draws no beam to check", ev.fx)
			}
			for _, rb := range beams {
				d := rb.To.Sub(rb.From).Normalize()
				if d.Z > -0.9 {
					t.Errorf("a beam of %q runs (%.2f %.2f %.2f): it lies along the ground "+
						"instead of dropping down the chamfer", ev.fx, d.X, d.Y, d.Z)
				}
			}
		})
	}
}

// The two monolith models are separate files with their own marker frames, and
// the restored one is the other half of the same report. Its guns keep their
// barrels along column two as well; only the sign of the highlight markers'
// own frame differs, which is the file's business and not the renderer's.
func TestRestoredMonolithGunBeamsRunAlongTheirBarrels(t *testing.T) {
	// The restored monolith ships in the definitive-edition patch archives
	// rather than loose in the mod, so it is read out of whichever copy is on
	// this machine; see restoredMonolithWhe for the same list.
	var raw []byte
	for _, path := range []string{
		".freebuff/phase_in_inspect/restored/art/ebps/races/necrons/troops/necron_restored_monolith.whm",
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3/Data/art/ebps/races/necrons/troops/necron_restored_monolith.whm`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP2/data/art/ebps/races/necrons/troops/necron_restored_monolith.whm`,
	} {
		if data, err := os.ReadFile(filepath.FromSlash(path)); err == nil && len(data) > 0 {
			raw = data
			break
		}
	}
	if raw == nil {
		t.Skip("the restored monolith model is not available on this machine")
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("the restored monolith model does not parse: %v", perr)
	}
	pose := m.RestPose()
	for _, marker := range []string{
		"marker_gun_fl_hi_02", "marker_gun_fr_hi_01",
		"marker_gun_rl_base_01", "marker_gun_rr_base_02",
	} {
		i := m.MarkerIndex(marker)
		if i < 0 {
			continue
		}
		barrel, ok := gunBarrel(m, pose, marker)
		if !ok {
			t.Fatalf("no base/muzzle pair around %q to measure the barrel against", marker)
		}
		axis, ok := m.MarkerBeamAxis(i, pose, true)
		if !ok {
			t.Fatalf("marker %q has no frame", marker)
		}
		if d := math.Abs(axis.Dot(barrel)); d < 0.99 {
			t.Errorf("the beam at %q runs (%.2f %.2f %.2f), |dot| %.3f with its barrel: it "+
				"crosses the gun instead of running along it", marker, axis.X, axis.Y, axis.Z, d)
		}
	}
}
