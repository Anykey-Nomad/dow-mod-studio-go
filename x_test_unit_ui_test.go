package main

import (
	"testing"
)

// TestUnitSectionsAreInOrder pins the left menu of the object interface. The
// order is the interface's contract with the user, so it is worth failing a
// build if a section is added in the wrong place or renamed silently.
//
// Squads sits after Abilities because it is the same shape of list: a row in the
// middle column, the selected entry's properties on the right. Viewport is last
// because it is still the one section with nothing in it.
func TestUnitSectionsAreInOrder(t *testing.T) {
	// The order is the reading order: what the object is, then what it uses,
	// then what it belongs to, then its relationships both ways, then what has
	// been moved out of the mod, and only then the mod as a whole.
	want := []string{"Unit info", "Weapons", "Abilities", "Squads", "Links", "Frozen", "Problems", "Events", "Viewport", "Classic OE"}
	if len(unitSections) != len(want) {
		t.Fatalf("got %d sections, want %d: %v", len(unitSections), len(want), unitSectionLabels())
	}
	for i, label := range want {
		if unitSections[i].label != label {
			t.Errorf("section %d = %q, want %q", i, unitSections[i].label, label)
		}
	}
	// Every section needs both column headings, or a column comes out blank.
	for _, s := range unitSections {
		if s.middleTitle == "" || s.rightTitle == "" || s.hint == "" {
			t.Errorf("section %q is incomplete: middle=%q right=%q hint=%q", s.label, s.middleTitle, s.rightTitle, s.hint)
		}
	}
}

func unitSectionLabels() []string {
	out := make([]string, 0, len(unitSections))
	for _, s := range unitSections {
		out = append(out, s.label)
	}
	return out
}

// TestCurrentUnitSectionSurvivesBadIndex guards the layout against an index
// left over from a future, longer section list: a bad one must fall back to
// the first section rather than panic in the middle of a frame.
func TestCurrentUnitSectionSurvivesBadIndex(t *testing.T) {
	for _, bad := range []int{-1, len(unitSections), len(unitSections) + 5} {
		ui := &uiState{unitSection: bad}
		if got := ui.currentUnitSection().label; got != unitSections[0].label {
			t.Errorf("section index %d gave %q, want fallback %q", bad, got, unitSections[0].label)
		}
	}
	ui := &uiState{unitSection: 1}
	if got := ui.currentUnitSection().label; got != "Weapons" {
		t.Errorf("section index 1 gave %q, want Weapons", got)
	}
}

// TestUnitColumnWidths checks the proportions the dialog was specified with:
// a narrow fixed menu on the left and a middle column of about a third.
func TestUnitColumnWidths(t *testing.T) {
	const (
		gap   = 8
		minW  = 140
		leftW = 132
	)
	for _, bodyW := range []int{1600, 1200, 900, 700} {
		left, mid, right := unitColumnWidths(bodyW, leftW, gap, minW)
		if left != leftW {
			t.Errorf("body %d: left = %d, want the fixed %d", bodyW, left, leftW)
		}
		usable := bodyW - leftW - 2*gap
		if mid < minW || right < minW {
			t.Errorf("body %d: a column fell below the minimum: mid=%d right=%d", bodyW, mid, right)
			continue
		}
		if mid+right != usable {
			t.Errorf("body %d: mid+right = %d, want the usable %d", bodyW, mid+right, usable)
		}
		// A third of the window, to within the rounding of one pixel.
		third := usable / 3
		if diff := mid - third; diff > 1 || diff < -1 {
			t.Errorf("body %d: mid = %d, want about a third (%d)", bodyW, mid, third)
		}
		// The right column must be the widest one, or the detail pane loses.
		if right <= mid {
			t.Errorf("body %d: right = %d is not wider than mid = %d", bodyW, right, mid)
		}
	}
}

// TestUnitColumnWidthsNarrowWindow checks the floor: a body too small for the
// specified proportions keeps the minimums instead of going negative.
func TestUnitColumnWidthsNarrowWindow(t *testing.T) {
	const gap, minW, leftW = 8, 140, 132
	left, mid, right := unitColumnWidths(300, leftW, gap, minW)
	if left != leftW || mid < minW || right < minW {
		t.Fatalf("narrow body gave left=%d mid=%d right=%d, want left=%d and both others >= %d", left, mid, right, leftW, minW)
	}
	// A negative width would panic in clip.Rect, so nothing may go below zero.
	for _, bodyW := range []int{0, 10, 100, 200} {
		_, m, r := unitColumnWidths(bodyW, leftW, gap, minW)
		if m < 0 || r < 0 {
			t.Errorf("body %d produced a negative width: mid=%d right=%d", bodyW, m, r)
		}
	}
}

// TestUnitObjectLabelFallsBack checks the dialog title never comes out blank,
// since an unnamed card would read as a rendering bug. The dialog now browses
// the mod's units rather than the open file, so the label names the selected
// unit — and with no mod loaded there is nothing to name.
func TestUnitObjectLabelFallsBack(t *testing.T) {
	ui := &uiState{}
	if got := ui.unitObjectLabel(); got != "No unit selected" {
		t.Errorf("with no mod loaded the label is %q, want %q", got, "No unit selected")
	}
	// With a catalog but a stale selection, the label still resolves to a real
	// unit rather than coming out blank.
	ui.modTree = testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`: "",
	})
	if got := ui.unitObjectLabel(); got != "necron_lord" {
		t.Errorf("label = %q, want the selected unit's name", got)
	}
	// An out of range index must not panic and must still name a unit.
	ui.unitRaceSel = 99
	ui.unitSel = 99
	if got := ui.unitObjectLabel(); got == "" {
		t.Error("a stale selection index produced an empty label")
	}
}
