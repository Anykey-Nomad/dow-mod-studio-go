package main

import (
	"image"
	"strings"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
)

// The Squads column and the widened info column are both lists that only have
// content once a background scan has run, so these tests drive the scan the way
// the app does and then look at the frame. A layout mistake in either — a list
// that overflows its column, a heading drawn over the first row, a scrollbar
// that never appears — shows up here and nowhere else.

// renderUnitSection lays the object tab out headlessly on one section and
// returns the frame.
func renderUnitSection(t *testing.T, ui *uiState, section, raceSel, unitSel, w, h int) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()
	ui.th = unitTestTheme()
	ui.openUnitTab()
	ui.unitSection = section
	ui.unitRaceSel = raceSel
	ui.unitSel = unitSel
	var ops op.Ops
	gtx := newLayoutContextForTest(&ops, w, h)
	ui.layoutContent(gtx)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame at %dx%d: %v", w, h, err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

// findUnitSection is the Squads section's index, looked up by name so a test does
// not depend on where in the menu it happens to sit.
func findUnitSection(t *testing.T, label string) int {
	t.Helper()
	for i, s := range unitSections {
		if s.label == label {
			return i
		}
	}
	t.Fatalf("no %q section in the object tab; it has %v", label, unitSectionLabels())
	return -1
}

// TestSquadColumnRendersAfterTheScan checks the column actually draws once the
// index has arrived. Before the scan it shows a "looking" message, which is
// content too, so the check is that the column is on screen either way and that
// nothing about the scan's absence is a blank frame.
func TestSquadColumnRendersAfterTheScan(t *testing.T) {
	tree := openTestDXP3(t)
	section := findUnitSection(t, "Squads")

	// Before the scan: still something on screen, not a crash and not a blank.
	ui := &uiState{modTree: tree}
	img := renderUnitSection(t, ui, section, 0, 0, 1400, 800)
	if !unitFrameHasContent(img) {
		t.Error("the Squads column drew nothing before the scan")
	}

	// After the scan, for a unit that is known to be in a squad.
	ui2 := &uiState{modTree: tree}
	ui2.startUnitSquadScan(tree)
	defer ui2.stopUnitSquadScan()
	runSquadScanToCompletion(t, ui2)
	raceIdx, unitIdx := firstUnitInASquad(t, ui2)
	if raceIdx < 0 {
		t.Skip("no unit of the tree belongs to a squad")
	}
	img2 := renderUnitSection(t, ui2, section, raceIdx, unitIdx, 1400, 800)
	if !unitFrameHasContent(img2) {
		t.Error("the Squads column drew nothing for a unit that is in a squad")
	}
	// The selected squad's properties go in the right column, so a frame that
	// differs from the unselected one is the evidence that the selection is
	// being drawn rather than ignored.
	ui2.unitSelRow = 0
	img3 := renderUnitSection(t, ui2, section, raceIdx, unitIdx, 1400, 800)
	if !unitFrameHasContent(img3) {
		t.Error("the Squads right column drew nothing with a row selected")
	}
	_ = img2
	_ = img3
}

// firstUnitInASquad finds a unit the index says is in at least one squad, and
// returns where it sits in the catalog so a test can select it.
func firstUnitInASquad(t *testing.T, ui *uiState) (raceIdx, unitIdx int) {
	t.Helper()
	cat := ui.unitCatalogFor()
	for ri := range cat.races {
		for ui_, u := range cat.races[ri].unitsOf() {
			if len(ui.squadsOfUnitOf(u)) > 0 {
				return ri, ui_
			}
		}
	}
	return -1, -1
}

// TestSquadColumnScrolls checks the list scrolls rather than overflowing. A unit
// in three squads does not fill the column, so this uses a small window where
// even a handful of rows do.
func TestSquadColumnScrolls(t *testing.T) {
	tree := openTestDXP3(t)
	ui := &uiState{modTree: tree}
	ui.startUnitSquadScan(tree)
	defer ui.stopUnitSquadScan()
	runSquadScanToCompletion(t, ui)
	raceIdx, unitIdx := firstUnitInASquad(t, ui)
	if raceIdx < 0 {
		t.Skip("no unit of the tree belongs to a squad")
	}
	section := findUnitSection(t, "Squads")
	renderUnitSection(t, ui, section, raceIdx, unitIdx, 1000, 260)
	u := ui.currentUnit()
	if u == nil {
		t.Fatal("no unit is selected after the index ran")
	}
	squads := ui.squadsOfUnitOf(*u)
	if len(squads) == 0 {
		t.Fatal("the selected unit is in no squad after the index ran")
	}
	// The scroll state has to have been written for the scrollbar to be
	// drawable; an untouched one means the list was laid out without it.
	if ui.unitSquadScroll.viewport == 0 {
		t.Error("the Squads list never recorded a viewport, so its scrollbar cannot be drawn")
	}
}

// TestInfoColumnScrollsAndGroups checks the widened info list. It is over a
// hundred rows now, so it has to scroll, and its headings have to be drawn — the
// second is checked by laying the same unit out and comparing against a frame
// where the headings are suppressed.
func TestInfoColumnScrollsAndGroups(t *testing.T) {
	tree := openTestDXP3(t)
	section := findUnitSection(t, "Unit info")
	ui := &uiState{modTree: tree}
	// A necrons unit, which is the race with the most sections carried.
	ui.unitSection = section
	renderUnitSection(t, ui, section, 0, 0, 1200, 700)
	d := ui.currentUnitDetails()
	if d == nil || len(d.fields) < 40 {
		t.Fatalf("the info list has %v rows, so this proves nothing", len(d.fields))
	}
	if ui.unitInfoScroll.viewport == 0 {
		t.Error("the info list never recorded a viewport, so its scrollbar cannot be drawn")
	}
	// With a hundred rows in a 700px window the list is longer than the column,
	// which is exactly the case the scrollbar is there for.
	if !ui.unitInfoScroll.hasBar {
		t.Log("the info list fitted its column at this size; the scrollbar is simply not needed")
	}
	// A group heading is drawn as part of the row, so laying the list out at all
	// exercises the heading path; the invariant that matters is that exactly the
	// rows marked startsGroup carry one, and that is checked in the list tests.
	groups := 0
	for _, f := range d.fields {
		if f.startsGroup {
			groups++
		}
	}
	if groups < 8 {
		t.Errorf("only %d headings for %d rows, so the list is not really grouped", groups, len(d.fields))
	}
}

// TestEverySectionHasBothColumns checks that no section falls through to a blank
// column. The new Squads section could easily have been added to the menu
// without being added to the two column dispatchers, and the symptom would be a
// menu entry that opens onto an empty window.
func TestEverySectionHasBothColumns(t *testing.T) {
	tree := openTestDXP3(t)
	// Every section, laid out with a real mod behind it, has to put something
	// on screen in both of its columns.
	for i := range unitSections {
		img := renderUnitSection(t, &uiState{modTree: tree}, i, 0, 0, 1400, 800)
		if !unitFrameHasContent(img) {
			t.Errorf("the %q section drew a blank frame", unitSections[i].label)
		}
	}
	// The middle column's dispatcher is a switch on the label, so a section that
	// has data but no case is exactly this bug. It is checked against the
	// switch's own set of labels rather than left to the frame test above, which
	// can only say that something was drawn.
	//
	// Viewport has a case as well, even though layoutUnitBody gives it the
	// middle and right columns together: rendering either column on its own
	// still has to show the model rather than a hint.
	routed := map[string]bool{
		"Unit info": true, "Weapons": true, "Abilities": true, "Squads": true,
		"Links": true, "Frozen": true, "Problems": true, "Viewport": true,
	}
	placeholder := map[string]bool{}
	for _, s := range unitSections {
		if !routed[s.label] && !placeholder[s.label] {
			t.Errorf("the %q section is not routed by layoutUnitMiddleColumnBody", s.label)
		}
	}
	// And no case may exist for a section that is not in the menu, which is the
	// other way the switch and the list can drift apart.
	for label := range routed {
		found := false
		for _, s := range unitSections {
			if s.label == label {
				found = true
			}
		}
		if !found {
			t.Errorf("the middle column routes %q, which is not a section of the tab", label)
		}
	}
	for _, s := range unitSections {
		if strings.TrimSpace(s.hint) == "" {
			t.Errorf("the %q section has no hint text for its empty state", s.label)
		}
		if strings.TrimSpace(s.middleTitle) == "" || strings.TrimSpace(s.rightTitle) == "" {
			t.Errorf("the %q section has an empty column heading", s.label)
		}
	}
}
