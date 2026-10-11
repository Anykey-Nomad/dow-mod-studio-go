package main

import (
	"dow-mod-studio/internal/whe"
	"os"
	"path/filepath"
	"testing"
)

// readEventsFile loads one of the game's .events files, skipping when the
// install is not there.
func readEventsFile(t *testing.T, rel string) []byte {
	t.Helper()
	p := filepath.Join(dowInstallDir, "art", "events", filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("game events not available: %v", err)
	}
	return data
}

// TestParseFxEventsReal checks the layout of DATAEVNT against a file that
// only has an fx entry and one that carries a pile of marker selectors.
func TestParseFxEventsReal(t *testing.T) {
	evs, err := whe.ParseFxEvents(readEventsFile(t, "structure_fx/relocation.events"))
	if err != nil {
		t.Fatalf("whe.ParseFxEvents(relocation): %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("relocation events = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Name != "relocate_building" {
		t.Errorf("name = %q, want relocate_building", ev.Name)
	}
	fx, ok := ev.First("fx")
	if !ok {
		t.Fatal("relocation has no fx entry")
	}
	if want := `eldar\structure fx\eldar_transport`; fx != want {
		t.Errorf("fx = %q, want %q", fx, want)
	}
}

// TestParseFxEventsWithMarkers is the shape a viewport preview cares about:
// an effect path and the markers it may play at.
func TestParseFxEventsWithMarkers(t *testing.T) {
	evs, err := whe.ParseFxEvents(readEventsFile(t, "necron/monolith_hit.events"))
	if err != nil {
		t.Fatalf("whe.ParseFxEvents(monolith_hit): %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	ev := evs[0]
	t.Logf("event %q: %d entries", ev.Name, len(ev.Entries))
	for _, e := range ev.Entries {
		t.Logf("  %s = %s", e.Key, e.Value)
	}
	if _, ok := ev.First("fx"); !ok {
		t.Errorf("event %q has no fx entry", ev.Name)
	}
	// At least one entry must name a marker, or there is nowhere to put the
	// effect on the model.
	found := false
	for _, e := range ev.Entries {
		if len(e.Key) >= 6 && e.Key[:6] == "marker" {
			found = true
		}
	}
	if !found {
		t.Errorf("event %q names no marker entry", ev.Name)
	}
}
