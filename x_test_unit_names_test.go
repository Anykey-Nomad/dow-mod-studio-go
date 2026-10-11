package main

import (
	"dow-mod-studio/internal/rgd"
	"strings"
	"testing"
	"time"
)

// unitRGDWith builds the smallest parsed unit that carries a screen_name_id, in
// the same in-memory shape the other RGD tests use. value is the raw field: a
// real id like "$705754", the unset marker "$0", or nil for no field at all.
func unitRGDWith(value string) *rgd.RgdFile {
	info := &rgd.RgdEntry{Name: "ui_info", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
		{Name: "icon_name", Type: rgd.RgdTypeString, StrVal: "necron_lord"},
	}}
	if value != "" {
		info.Children = append(info.Children, &rgd.RgdEntry{
			Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: value,
		})
	}
	return &rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
		{Name: "ui_ext", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{info}},
	}}}
}

// ── the id ──────────────────────────────────────────────────────────────────

// TestUnitScreenNameIDDropsTheDollar pins what the index is allowed to hold: the
// id with no "$" and no text. The text is looked up on the UI thread so a
// language change costs nothing, and a test that let text in here would quietly
// make every language switch need a full rescan of the mod.
func TestUnitScreenNameIDDropsTheDollar(t *testing.T) {
	id, ok := unitScreenNameIDOf(unitRGDWith("$705754"))
	if !ok {
		t.Fatal("the id was not read out of a unit that carries one")
	}
	if id != "705754" {
		t.Errorf("got id %q, want %q", id, "705754")
	}
}

// TestUnitScreenNameIDRejectsUnitsWithNoName covers the fallback the lists rely
// on. An unset field and a missing one both mean "this unit has no name", and
// neither may be cached: "$0" is the marker for nothing, not the id of a string.
func TestUnitScreenNameIDRejectsUnitsWithNoName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"the unset marker", "$0"},
		{"no screen_name_id at all", ""},
		{"a value that is not an id", "necron_lord"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if id, ok := unitScreenNameIDOf(unitRGDWith(tc.value)); ok {
				t.Errorf("read id %q from a unit with %s", id, tc.name)
			}
		})
	}
	if _, ok := unitScreenNameIDOf(nil); ok {
		t.Error("read an id out of no file")
	}
	if _, ok := unitScreenNameIDOf(&rgd.RgdFile{}); ok {
		t.Error("read an id out of a file with no root")
	}
}

// ── the label ───────────────────────────────────────────────────────────────

// testUnitEntry is a unit as the lists see it: a path and the name the file
// gives it.
func testUnitEntry(name string) unitEntry {
	return unitEntry{
		path: `Data\attrib\ebps\races\necron\troops\` + name + ".rgd",
		name: name,
		race: "necron",
	}
}

// TestUnitLabelUsesTheLocalisedText is the request itself: with the id resolved
// and the string table holding it, the list shows the text and not the id.
func TestUnitLabelUsesTheLocalisedText(t *testing.T) {
	lord := testUnitEntry("necron_lord")
	ui := newUI(unitTestTheme())
	ui.unitNames = map[string]string{strings.ToLower(lord.path): "705754"}
	ui.testLocaleTable(t, "705754", "Necron Lord")

	got := ui.unitLabel(lord)
	if got != "Necron Lord" {
		t.Errorf("the list reads %q, want the localised text", got)
	}
	if strings.Contains(got, "$") || strings.Contains(got, "705754") {
		t.Errorf("the list shows the localisation id in %q; it was asked for the name only", got)
	}
}

// TestUnitLabelFallsBackToTheFileName is what the reader sees when there is no
// localisation: the .rgd's own name, which is the name the game uses for the
// unit's hotkey, so it is a real name rather than a placeholder.
func TestUnitLabelFallsBackToTheFileName(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  map[string]string
	}{
		{"nothing scanned yet", nil},
		{"the unit has no id of its own", map[string]string{strings.ToLower(testUnitEntry("necron_lord").path): "999"}},
		{"the chosen language has no entry for it", map[string]string{strings.ToLower(testUnitEntry("necron_lord").path): "705754"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lord := testUnitEntry("necron_lord")
			ui := newUI(unitTestTheme())
			ui.unitNames = tc.ids
			// A table that knows nothing, so the third case is a real miss
			// rather than a missing table.
			ui.testLocaleTable(t, "1", "One")

			if got := ui.unitLabel(lord); got != "necron_lord" {
				t.Errorf("the list reads %q, want the file name", got)
			}
		})
	}
}

// TestUnitFileLabelIsTheFilesOwnName: the dim name beside the display name is
// the file's, and it is what tells two same-named units of one race apart.
func TestUnitFileLabelIsTheFilesOwnName(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`Data\a\b\necron_nexus.rgd`, "necron_nexus"},
		{`Data\attrib\ebps\races\necrons\structures\necron_dreadnought.rgd`, "necron_dreadnought"},
		{"plain.rgd", "plain"},
	} {
		if got := unitFileLabel(unitEntry{path: tc.path}); got != tc.want {
			t.Errorf("unitFileLabel(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ── the background scan ─────────────────────────────────────────────────────

// TestUnitNameScanIsNotRestartedForTheSameTree: the catalog is asked for on
// every frame the object tab is open, so a scan that restarted each time would
// read every .rgd in the mod over and over, forever.
func TestUnitNameScanIsNotRestartedForTheSameTree(t *testing.T) {
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necron\troops\necron_lord.rgd`: "not a real rgd",
	})
	cat := buildUnitCatalog(tree)
	ui := newUI(unitTestTheme())

	ui.startUnitNameScan(tree, cat)
	first := ui.unitNamesCh
	if first == nil {
		t.Fatal("the scan did not start")
	}
	ui.startUnitNameScan(tree, cat)
	if ui.unitNamesCh != first {
		t.Error("a second scan was started for the same tree")
	}
	ui.stopUnitNameScan()
}

// TestUnitNameScanStopsWhenTheModGoesAway is the case that matters: a reload
// closes the tree the scan is reading, so the scan has to be told rather than
// left to find out by itself.
func TestUnitNameScanStopsWhenTheModGoesAway(t *testing.T) {
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necron\troops\necron_lord.rgd`: "not a real rgd",
	})
	cat := buildUnitCatalog(tree)
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)

	// What the catalog asks for once there is no mod at all.
	ui.modTree = nil
	if got := ui.unitCatalogFor(); got != nil {
		t.Fatal("the catalog survived the mod going away")
	}
	if ui.unitNamesRunning {
		t.Error("the scan is still running against a tree that is being closed")
	}
	if ui.unitNames != nil {
		t.Error("the name index outlived the mod it belonged to")
	}
}

// TestPollUnitNamesIsQuietWithNothingToSay: a finished scan, or no scan at all,
// must not report work, or every frame would ask for a redraw it does not need.
func TestPollUnitNamesIsQuietWithNothingToSay(t *testing.T) {
	ui := newUI(unitTestTheme())
	if ui.pollUnitNames() {
		t.Error("pollUnitNames reported work before any scan was started")
	}

	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necron\troops\necron_lord.rgd`: "not a real rgd",
	})
	cat := buildUnitCatalog(tree)
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)
	if ui.pollUnitNames() {
		t.Error("pollUnitNames reported work after the scan had finished")
	}
}

// waitForUnitNames polls the way the frame loop does until the scan is done.
// waitForUnitNames gives the asynchronous name scan time to finish. The scan
// reads a real catalogue off a real disk, so the budget is wall clock and
// generous: a fixed number of polls turns a slow disk into a failed test, which
// says nothing about the code.
func waitForUnitNames(t *testing.T, ui *uiState) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		ui.pollUnitNames()
		if ui.unitNamesDone || (!ui.unitNamesRunning && ui.unitNamesCh == nil) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("the scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// testLocaleTable installs a string table holding exactly the ids given, so a
// test can say what the language knows without reading a .ucs. The tables are
// package level state, so they are reset afterwards.
func (ui *uiState) testLocaleTable(t *testing.T, entries ...string) {
	t.Helper()
	resetLocales(t)
	if len(entries)%2 != 0 {
		t.Fatalf("testLocaleTable got %d values, want id and text in pairs", len(entries))
	}
	index := make(map[string]localeEntry, len(entries)/2)
	for i := 0; i < len(entries); i += 2 {
		index[entries[i]] = localeEntry{Text: entries[i+1]}
	}
	localeState.mu.Lock()
	localeState.cur = &localeTable{Lang: "Test", Index: index, Files: 1, Lines: len(index)}
	localeState.mu.Unlock()
}
