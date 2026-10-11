package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// The freeze buttons are found by hovering, not by being told where they are,
// so a test fails if a button ends up somewhere a user cannot reach — which is
// the only way a button that rewrites other files should be allowed to move.

// clickAt presses and releases at a point and gives the window a frame.
func clickAt(f *windowFrame, at f32.Point) {
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
}

// TestTheFreezeButtonIsReachableInTheWindow puts a mod in the object window
// and finds the freeze button by walking the pointer along the strip.
func TestTheFreezeButtonIsReachableInTheWindow(t *testing.T) {
	ui, _ := freezeTestUI(t)
	freezeTestMod(t, ui.modTree)
	ui.openUnitTab()
	f := newWindowFrame(ui, 1000, 700)
	at, ok := findStripButton(t, f, &f.ui.unitFreezeTrigger)
	if !ok {
		t.Fatal("the Freeze button was not found in the picker strip")
	}
	if at.X <= 0 || at.Y <= 0 {
		t.Errorf("the Freeze button was found at %v, which is the window corner rather than the strip", at)
	}
}

// TestFreezingFromTheWindowMovesTheSelectedUnitAndSaysSo presses the button and
// checks the two things a user would check afterwards: that the unit has left
// the mod, and that the status line says what happened to it.
//
// It acts on whatever the Unit field is showing rather than on a unit named in
// the test, because that is what the button does.
func TestFreezingFromTheWindowMovesTheSelectedUnitAndSaysSo(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	ui.openUnitTab()
	sel := ui.currentUnit()
	if sel == nil {
		t.Fatal("no unit is selected in the object window")
	}
	f := newWindowFrame(ui, 1000, 700)

	at, ok := findStripButton(t, f, &f.ui.unitFreezeTrigger)
	if !ok {
		t.Fatal("the Freeze button was not found in the picker strip")
	}
	clickAt(f, at)

	if tree.FindFile(sel.path) != nil {
		t.Errorf("%s is still in the mod after the button was pressed", sel.name)
	}
	if !ui.isFrozen(sel.path) {
		t.Error("the record does not say the unit was frozen")
	}
	if ui.statusText == "" {
		t.Error("nothing was said about what the freeze did")
	}
	// The window sends the user where the way back is, because the unit is no
	// longer in the list it was just frozen from.
	if got := unitSections[ui.unitSection].label; got != "Frozen" {
		t.Errorf("after freezing the window shows %q, want the Frozen section", got)
	}
	// And it still draws.
	f.frame()
}

// TestAFrozenUnitIsListedSoItCanBePutBack is the other half of the round trip.
// A frozen unit is not in the mod, so the record is the only place it exists,
// and without a list of them a freeze could not be undone from the program.
func TestAFrozenUnitIsListedSoItCanBePutBack(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	sel := ui.currentUnit()
	if sel == nil {
		t.Fatal("no unit is selected in the object window")
	}
	if _, err := ui.freezeEntity(sel.path); err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	rows := ui.frozenRows()
	if len(rows) != 1 {
		t.Fatalf("the Frozen list has %d rows, want the one that was frozen", len(rows))
	}
	if rows[0].name != sel.name {
		t.Errorf("the Frozen list names %q, want %s", rows[0].name, sel.name)
	}
	if !strings.EqualFold(rows[0].ref, sel.path) {
		t.Errorf("the row carries %q as the path to put back, want %s", rows[0].ref, sel.path)
	}
	// A mod that has never frozen anything says so rather than showing a gap.
	if got := (freezeTestUIRows(t)); len(got) != 0 {
		t.Errorf("a mod with nothing frozen has %d rows", len(got))
	}
}

// freezeTestUIRows is the Frozen list of a fresh mod, which is empty.
func freezeTestUIRows(t *testing.T) []unitWeaponRow {
	t.Helper()
	ui, _ := freezeTestUI(t)
	freezeTestMod(t, ui.modTree)
	return ui.frozenRows()
}

// TestPuttingAFrozenUnitBackFromTheListPressesItHome is the whole round trip
// through the window: freeze from the strip, put back from the list.
func TestPuttingAFrozenUnitBackFromTheListPressesItHome(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	ui.openUnitTab()
	sel := ui.currentUnit()
	if sel == nil {
		t.Fatal("no unit is selected in the object window")
	}
	f := newWindowFrame(ui, 1000, 700)

	at, ok := findStripButton(t, f, &f.ui.unitFreezeTrigger)
	if !ok {
		t.Fatal("the Freeze button was not found in the picker strip")
	}
	clickAt(f, at)
	if tree.FindFile(sel.path) != nil {
		t.Fatalf("%s was not moved out of the mod", sel.name)
	}

	// Put it back by pressing its row in the Frozen list. The row is found by
	// hovering it, the same way the strip buttons are.
	ui.unitSection = unitSectionNamed("Frozen")
	rowClicks := len(ui.frozenRows())
	ui.ensureUnitRowClicks(rowClicks)
	at, ok = findHoverPoint(t, f, ui.unitRowClicks[0].Hovered, frozenRowProbeRow(t, f), 4)
	if !ok {
		t.Fatal("the row of the frozen unit was not found in the Frozen list")
	}
	clickAt(f, at)

	if tree.FindFile(sel.path) == nil {
		t.Errorf("%s was not put back into the mod", sel.name)
	}
	if ui.isFrozen(sel.path) {
		t.Error("the record still says the unit is frozen after it was put back")
	}
	if len(ui.frozenRows()) != 0 {
		t.Error("the Frozen list is not empty after the last unit was put back")
	}
	// Nothing is left in the project folder either.
	unitsDir, err := freezeUnitsDir(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(unitsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".rgd" {
			t.Errorf("%s is still in the project folder after the unit was put back", e.Name())
		}
	}
}

// frozenRowProbeRow is the band the Frozen list's first row sits in, which is
// the same place every row of the object window's lists sits: under the header
// of the column, below the pickers.
func frozenRowProbeRow(t *testing.T, f *windowFrame) image.Rectangle {
	t.Helper()
	top := unitMenuBarHeightPx + unitTabBarHeightPx
	y := top + int(unitPickerStripHeightDp) + unitTabPadDp*2 + unitColHeaderHeight + 10
	return image.Rect(20, y, 900, y)
}

// TestTheRelationshipsViewDraws checks that the new sections lay out over a
// real window, because a column that panics or draws nothing is not a view.
func TestTheRelationshipsViewDraws(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	ui.openUnitTab()
	f := newWindowFrame(ui, 1000, 700)
	for _, name := range []string{"Links", "Problems", "Frozen"} {
		ui.unitSection = unitSectionNamed(name)
		f.frame()
	}
	waitForLinkScan(t, ui)
	f.frame()
}
