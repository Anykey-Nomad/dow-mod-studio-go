package main

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/unit"
)

// ── scrolling ──────────────────────────────────────────────────────────────

// The bar only scrolls when the tabs do not fit. A row narrower than the bar
// has nothing to reveal, so the offset stays at zero however hard the wheel is
// turned — otherwise the bar would slide its own background out of view.
func TestTabScrollIsOnlyPossibleWhenTheRowOverflows(t *testing.T) {
	ui := &uiState{}

	if got := ui.tabMaxScroll(900, 400); got != 0 {
		t.Errorf("maxScroll = %d for a 400px row in a 900px bar, want 0", got)
	}
	if got := ui.tabMaxScroll(900, 900); got != 0 {
		t.Errorf("maxScroll = %d for an exactly fitting row, want 0", got)
	}
	if got := ui.tabMaxScroll(900, 1300); got != 400 {
		t.Errorf("maxScroll = %d, want 400", got)
	}
}

func TestClampTabScrollHoldsTheOffsetInsideTheRow(t *testing.T) {
	ui := &uiState{tabScrollX: 5000}
	ui.clampTabScroll(900, 1300)
	if ui.tabScrollX != 400 {
		t.Errorf("scrollX = %d, want it pulled back to the 400px of overflow", ui.tabScrollX)
	}

	// A negative offset would slide the row off its left edge, leaving a gap
	// where the first tab should be.
	ui.tabScrollX = -200
	ui.clampTabScroll(900, 1300)
	if ui.tabScrollX != 0 {
		t.Errorf("scrollX = %d, want 0", ui.tabScrollX)
	}
}

// A row that shrinks under a scrolled bar has to pull the offset back, or the
// tabs slide out of sight and the bar shows nothing. This is the path a closed
// tab takes, and it is checked here because the bar does not watch the tab list.
func TestTabScrollFollowsTheRowShrinking(t *testing.T) {
	ui := &uiState{tabScrollX: 600, tabBarWidth: 900, tabRowWidth: 1600}
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)
	if ui.tabScrollX != 600 {
		t.Fatalf("scrollX = %d, want 600 to start with", ui.tabScrollX)
	}
	// Last tab closed: the row is now narrower than the bar.
	ui.clampTabScroll(900, 700)
	if ui.tabScrollX != 0 {
		t.Errorf("scrollX = %d after the row shrank below the bar, want 0", ui.tabScrollX)
	}
}

// The wheel is vertical and the row is horizontal, so the amount it moved is
// the vertical one. A trackpad reporting a sideways gesture as well must not
// double the movement, which is why only Y is read. Gio's wheel is positive
// downwards, and the row has to travel with it: a wheel up walks back towards
// the first tab, a wheel down towards the last.
func TestTabWheelMovesTheRowSideways(t *testing.T) {
	const bar, row = 400, 1400
	ui := &uiState{tabBarWidth: bar, tabRowWidth: row, tabScrollX: 400}
	overflow := row - bar

	// A notch is wheelNotchDelta, and the step is a fraction of a pixel per
	// unit of that. Reading the step as pixels per notch — which is what it
	// used to be — asks for 48×120 pixels a click, and every click slammed the
	// row to one end. A notch has to be a move, not a jump.
	notch := int(wheelNotchDelta * tabWheelStepPx)
	if notch <= 0 {
		t.Fatalf("a notch of the wheel moves the row %d pixels", notch)
	}
	if notch*5 > overflow {
		t.Errorf("a notch moves the row %d pixels of its %d pixels of overflow, which is most of the row", notch, overflow)
	}

	// Starting at the end, so a wheel that moves the row the wrong way shows
	// up as a clamp rather than as nothing happening.
	ui.tabScrollX += -2 * notch
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)
	if want := 400 - 2*notch; ui.tabScrollX != want {
		t.Errorf("scrollX = %d after two notches up, want %d", ui.tabScrollX, want)
	}

	// And down again, back to the end of the row.
	ui.tabScrollX += 2 * notch
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)
	if ui.tabScrollX != 400 {
		t.Errorf("scrollX = %d after two notches down, want 400", ui.tabScrollX)
	}
}

// ── which tab ──────────────────────────────────────────────────────────────

// A right press is resolved by position, not by a clickable per tab, so the
// recorded rects have to line up with where the tabs were drawn.
func TestTabAtFindsTheTabUnderAPoint(t *testing.T) {
	ui := &uiState{tabRects: []image.Rectangle{
		{Min: image.Pt(0, 0), Max: image.Pt(80, 28)},
		{Min: image.Pt(82, 0), Max: image.Pt(200, 28)},
		{Min: image.Pt(202, 0), Max: image.Pt(260, 28)},
	}}

	for _, tc := range []struct {
		p    image.Point
		want int
	}{
		{image.Pt(0, 0), 0},     // the very first pixel
		{image.Pt(40, 14), 0},   // inside the first
		{image.Pt(79, 14), 0},   // the last pixel column of the first
		{image.Pt(80, 14), -1},  // the gap after it is not a tab
		{image.Pt(81, 14), -1},  // and the next pixel of it either
		{image.Pt(82, 14), 1},   // the second starts here
		{image.Pt(199, 14), 1},  // its right edge is inside it
		{image.Pt(200, 14), -1}, // its last column belongs to it, the next does not
		{image.Pt(230, 27), 2},  // the third
		{image.Pt(230, 28), -1}, // below the bar is nothing
		{image.Pt(500, 14), -1}, // past the last tab
	} {
		got := ui.tabAt(f32.Point{X: float32(tc.p.X), Y: float32(tc.p.Y)})
		if got != tc.want {
			t.Errorf("tabAt(%v) = %d, want %d", tc.p, got, tc.want)
		}
	}
}

// The rects are in the bar's own coordinates while the menu is placed in the
// window's, so the bar's top has to be added. Getting that wrong puts the menu
// 25px above the tab it belongs to.
func TestTabContextMenuOpensBelowTheBar(t *testing.T) {
	ui := &uiState{}
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}

	pos := ui.tabContextPoint(gtx, 40)

	// The bar sits under the 24px menu bar and its 1px line, so its own bottom
	// is at 25+28 and the menu has to start below that, not on it.
	wantY := menuBarHeightDp + 1 + tabBarHeightDp
	if pos.Y != wantY {
		t.Errorf("menu at y=%d, want %d: it is drawn on top of the bar", pos.Y, wantY)
	}
	if pos.X != 40 {
		t.Errorf("menu at x=%d, want 40", pos.X)
	}
}

// ── the context menu ───────────────────────────────────────────────────────

func TestTabContextMenuSelectsTheTabItWasOpenedOn(t *testing.T) {
	ui := &uiState{openTabs: []*fileTab{
		{path: "a.rgd", kind: "rgd"},
		{path: "b.rgd", kind: "rgd"},
		{path: "c.rgd", kind: "rgd"},
	}}
	ui.tabSel = 1

	ui.openTabContextMenu(2, image.Pt(300, 60))

	if !ui.tabContextVisible {
		t.Fatal("the menu did not open")
	}
	if ui.tabContextIndex != 2 {
		t.Errorf("index = %d, want 2", ui.tabContextIndex)
	}
	// The tab is selected too, so the menu is unambiguous about which tab it is
	// for even though its one item does not act on the selection.
	if ui.tabSel != 3 {
		t.Errorf("tabSel = %d, want 3 (the third tab)", ui.tabSel)
	}
}

func TestTabContextMenuRefusesATabThatIsNotThere(t *testing.T) {
	ui := &uiState{openTabs: []*fileTab{{path: "a.rgd", kind: "rgd"}}}
	ui.openTabContextMenu(5, image.Pt(10, 10))
	if ui.tabContextVisible {
		t.Error("a menu opened for a tab that does not exist")
	}
}

func TestCloseTabContextMenuForgetsWhichTabItWasOn(t *testing.T) {
	ui := &uiState{openTabs: []*fileTab{{path: "a.rgd", kind: "rgd"}, {path: "b.rgd", kind: "rgd"}}}
	ui.openTabContextMenu(1, image.Pt(10, 10))
	ui.closeTabContextMenu()

	if ui.tabContextVisible {
		t.Error("the menu is still open")
	}
	// A stale index left behind would open the next menu on the wrong tab.
	if ui.tabContextIndex != -1 {
		t.Errorf("index = %d after closing, want -1", ui.tabContextIndex)
	}
}

// ── closing every tab ──────────────────────────────────────────────────────

// Closing them all has to give back what each tab was holding, exactly as
// closing one at a time does. A tab dropped without this leaks its decode, its
// audio and its GPU buffers, which is why the two paths share one function.
func TestCloseAllFileTabsReleasesWhatEachTabHeld(t *testing.T) {
	stopped := 0
	tab := &fileTab{
		path: "a.rgd", kind: "rgd",
		imageDecodeStop: func() { stopped++ },
		model:           &modelViewer{model: &WhmModel{}},
	}
	ui := &uiState{openTabs: []*fileTab{tab}, tabSel: 1}
	ui.ensureTabClicks()

	ui.closeAllFileTabs()

	if stopped != 1 {
		t.Errorf("the decode was stopped %d times, want 1", stopped)
	}
	if tab.model != nil {
		t.Error("the 3D preview is still referenced after the tab went away")
	}
	if len(ui.openTabs) != 0 {
		t.Errorf("%d tabs left open", len(ui.openTabs))
	}
	if ui.tabSel != 0 {
		t.Errorf("tabSel = %d, want 0", ui.tabSel)
	}
}

// A bar with nothing left in it has nothing to scroll, so the offset goes back
// to the left rather than leaving the bar showing empty background.
func TestCloseAllFileTabsResetsTheScroll(t *testing.T) {
	ui := &uiState{
		openTabs:   []*fileTab{{path: "a.rgd", kind: "rgd"}},
		tabSel:     1,
		tabScrollX: 240,
	}
	ui.ensureTabClicks()

	ui.closeAllFileTabs()

	if ui.tabScrollX != 0 {
		t.Errorf("scrollX = %d on an empty bar, want 0", ui.tabScrollX)
	}
	if ui.tabContextVisible {
		t.Error("the context menu is still open after every tab went away")
	}
}

// The object tab's dropdowns belong to that tab, so they go with it rather than
// being left open over whatever takes its place.
func TestCloseAllFileTabsClosesTheObjectDropdowns(t *testing.T) {
	unit := &fileTab{kind: unitTabKind}
	ui := &uiState{openTabs: []*fileTab{unit}, tabSel: 1, unitRaceOpen: true, unitListOpen: true}
	ui.ensureTabClicks()

	ui.closeAllFileTabs()

	if ui.unitRaceOpen || ui.unitListOpen {
		t.Errorf("a dropdown is still open (race=%v list=%v)", ui.unitRaceOpen, ui.unitListOpen)
	}
}
