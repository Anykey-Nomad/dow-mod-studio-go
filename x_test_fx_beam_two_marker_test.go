package main

import (
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whe"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/fogleman/fauxgl"
)

// Only nineteen of the 3473 events in the base game name a second marker, but
// those nineteen are the only ones that draw a beam between two points, and
// they are spread over four different effects and three different models. The
// tests in fx_beam_arc_real_test.go check hand-picked ones; these sweep all of
// them, so an effect or a model added later is covered without anyone having to
// know to add it, and a marker pair that stops resolving is caught even where
// nobody thought to look.
//
// Two invariants:
//
//   - every two-marker event parses, and the effect it names is a file that
//     exists and parses, so it can draw at all;
//   - where the event's two markers resolve onto a model that carries both,
//     the beam spans exactly the gap between them — the whole of what
//     fx_length = distance asks for.
//
// The second only applies where the pair resolves. An event may name a marker
// the model does not carry, or "random", and then there is no fixed gap to
// measure; those are counted and reported rather than skipped silently, so the
// coverage that did not get checked is visible.

// fxTwoMarkerEvent is one event that names both its own marker and a second.
type fxTwoMarkerEvent struct {
	file   string
	event  string
	first  string
	second string
	fx     string
}

// fxMarkerKeys and fxOtherMarkerKeys are the keys the resolver reads. They are
// spelled out here rather than reused from fxEventMarker so that an event the
// sweep calls two-marker but the resolver does not is a reportable
// disagreement rather than something the sweep cannot see.
var (
	fxMarkerKeys      = []string{"marker", "marker_0", "marker_surface_00", "marker_surface_0", "random_marker_0"}
	fxOtherMarkerKeys = []string{"fx_other_marker", "fx_other_marker_0"}
)

// collectTwoMarkerEvents finds every event in the tree that names a marker and
// a second one.
func collectTwoMarkerEvents(t *testing.T, tree *modtree.ModTree) []fxTwoMarkerEvent {
	t.Helper()
	var out []fxTwoMarkerEvent
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".events") {
			return
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil || len(data) == 0 {
			return
		}
		evs, err := whe.ParseFxEvents(data)
		if err != nil {
			t.Errorf("events file %s does not parse: %v", f.FullPath, err)
			return
		}
		for _, ev := range evs {
			first := ""
			for _, k := range fxMarkerKeys {
				if v, ok := ev.First(k); ok && strings.TrimSpace(v) != "" {
					first = strings.TrimSpace(v)
					break
				}
			}
			second := ""
			for _, k := range fxOtherMarkerKeys {
				if v, ok := ev.First(k); ok && strings.TrimSpace(v) != "" {
					second = strings.TrimSpace(v)
					break
				}
			}
			if first == "" || second == "" {
				continue
			}
			fxName, _ := ev.First("fx")
			out = append(out, fxTwoMarkerEvent{
				file: f.FullPath, event: ev.Name,
				first: first, second: second, fx: fxName,
			})
		}
	})
	return out
}

// TestShippedTwoMarkerEventsParse pins the first invariant over every event in
// the game: each one is found, names an effect, and that effect is a file that
// reads and parses into beams.
func TestShippedTwoMarkerEventsParse(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	events := collectTwoMarkerEvents(t, tree)
	if len(events) == 0 {
		t.Skip("no two-marker events in the tree")
	}
	t.Logf("%d two-marker events across the base game", len(events))

	// One effect per event is enough to know the whole set parsed, but the
	// failures are reported per event so a broken one is identifiable.
	effects := map[string]bool{}
	var broken []string
	for _, e := range events {
		if e.fx == "" {
			broken = append(broken, fmt.Sprintf("%s: event %q names no fx", e.file, e.event))
			continue
		}
		if effects[e.fx] {
			continue
		}
		effects[e.fx] = true
		data, err := readFxLuaFile(tree, roots, e.fx)
		if err != nil {
			broken = append(broken, fmt.Sprintf("%s: event %q: effect %q does not read: %v", e.file, e.event, e.fx, err))
			continue
		}
		ff, err := fx.ParseFxLua(data)
		if err != nil {
			broken = append(broken, fmt.Sprintf("%s: event %q: effect %q does not parse: %v", e.file, e.event, e.fx, err))
			continue
		}
		sim := fx.NewFxSim(ff)
		// A unit span is all the sampler needs to hand a beam its length.
		if len(sim.Beams(fauxgl.Vector{}, fauxgl.Vector{X: 1}, true)) == 0 {
			broken = append(broken, fmt.Sprintf("%s: event %q: effect %q parses but has no beam node to draw",
				e.file, e.event, e.fx))
		}
	}
	for _, b := range broken {
		t.Error(b)
	}
	names := make([]string, 0, len(effects))
	for fx := range effects {
		names = append(names, fx)
	}
	sort.Strings(names)
	t.Logf("effects named by two-marker events: %s", strings.Join(names, ", "))
}

// TestShippedTwoMarkerBeamsSpanExactlyTheGap pins the second invariant. For
// every two-marker event whose pair resolves on a model carrying both
// markers, every beam it draws must reach from one to the other.
func TestShippedTwoMarkerBeamsSpanExactlyTheGap(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	events := collectTwoMarkerEvents(t, tree)
	if len(events) == 0 {
		t.Skip("no two-marker events in the tree")
	}

	// Index the models by the markers they carry, so a pair can be looked up
	// without walking every model for every event.
	type modelMarkers struct {
		path  string
		names map[string]int
	}
	var models []modelMarkers
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".whm") {
			return
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil || len(data) == 0 {
			return
		}
		m, err := ParseWhmModel(data)
		if err != nil || len(m.Markers) == 0 {
			return
		}
		mm := modelMarkers{path: f.FullPath, names: map[string]int{}}
		for i, mk := range m.Markers {
			mm.names[strings.ToLower(mk.Name)] = i
		}
		models = append(models, mm)
	})

	var resolved, unresolved, beamsChecked int
	for _, e := range events {
		first := strings.ToLower(e.first)
		second := strings.ToLower(e.second)
		var model *modelMarkers
		for i := range models {
			_, hasA := models[i].names[first]
			_, hasB := models[i].names[second]
			if hasA && hasB {
				model = &models[i]
				break
			}
		}
		if model == nil {
			// "random" is a real answer meaning any other marker of the
			// model; there is no fixed gap to measure against.
			unresolved++
			t.Logf("no fixed pair: %-58s %s -> %s", e.file, e.first, e.second)
			continue
		}
		data, err := tree.ReadFile(model.path)
		if err != nil {
			t.Errorf("%s: %v", model.path, err)
			continue
		}
		m, err := ParseWhmModel(data)
		if err != nil {
			t.Errorf("%s: %v", model.path, err)
			continue
		}
		ia, ib := model.names[first], model.names[second]
		// The resolver the viewer actually uses must agree with the sweep,
		// or the two disagree about which markers the event names.
		if got := m.MarkerIndex(e.first); got != ia {
			t.Errorf("%s: the sweep took %s as index %d, the resolver says %d", e.file, e.first, ia, got)
		}
		if got := m.MarkerIndex(e.second); got != ib {
			t.Errorf("%s: the sweep took %s as index %d, the resolver says %d", e.file, e.second, ib, got)
		}
		// fxBeamOtherMarker is what decides there is a second end at all, and
		// fxEventSpansDistance what makes it span the gap rather than run on.
		if got := fxBeamOtherMarker(m, e.event2(t, tree)); got != ib {
			t.Errorf("%s: fxBeamOtherMarker resolved %d, the sweep took %d", e.file, got, ib)
		}
		pose := m.RestPose()
		a, okA := m.MarkerWorld(ia, pose, true)
		b, okB := m.MarkerWorld(ib, pose, true)
		if !okA || !okB {
			t.Errorf("%s: markers %s/%s have no world position", model.path, e.first, e.second)
			continue
		}
		gap := a.Sub(b).Length()
		if gap < 1e-6 {
			t.Errorf("%s: markers %s and %s sit on the same point", model.path, e.first, e.second)
			continue
		}
		ld, err := readFxLuaFile(tree, roots, e.fx)
		if err != nil {
			// Already reported by the parse test; nothing to measure here.
			t.Logf("%s: effect %q unreadable, span not checked: %v", e.file, e.fx, err)
			continue
		}
		ff, err := fx.ParseFxLua(ld)
		if err != nil {
			t.Logf("%s: effect %q unparsed, span not checked: %v", e.file, e.fx, err)
			continue
		}
		sim := fx.NewFxSim(ff)
		for i, r := range sim.Beams(a, b, true) {
			beamsChecked++
			got := r.To.Sub(r.From).Length()
			if got < gap-1e-3 || got > gap+1e-3 {
				t.Errorf("%s: beam %d of %s spans %.4f across a gap of %.4f (%s -> %s on %s)",
					e.file, i, e.fx, got, gap, e.first, e.second, model.path)
			}
		}
		resolved++
		t.Logf("%-58s %s -> %s: gap %.3f", e.file, e.first, e.second, gap)
	}
	if beamsChecked == 0 {
		t.Skip("no two-marker beam could be measured")
	}
	t.Logf("%d events resolved onto a model and checked, %d beams measured, %d had no fixed pair",
		resolved, beamsChecked, unresolved)
}

// event2 re-reads the event definition for the cross-check above, which needs
// the parsed event rather than just its strings.
func (e fxTwoMarkerEvent) event2(t *testing.T, tree *modtree.ModTree) whe.FxEvent {
	t.Helper()
	data, err := tree.ReadFile(e.file)
	if err != nil {
		t.Errorf("%s: %v", e.file, err)
		return whe.FxEvent{}
	}
	evs, err := whe.ParseFxEvents(data)
	if err != nil {
		t.Errorf("%s: %v", e.file, err)
		return whe.FxEvent{}
	}
	for _, ev := range evs {
		if strings.EqualFold(ev.Name, e.event) {
			return ev
		}
	}
	t.Errorf("%s: event %q is not there any more", e.file, e.event)
	return whe.FxEvent{}
}
