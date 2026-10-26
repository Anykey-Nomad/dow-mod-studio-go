package app

import (
	"dow-mod-studio/internal/testutil"
	"fmt"
	"image"
	"image/color"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// barConstraints is the constraint the tab bar is really laid out with: a
// maximum width and nothing else. The shared newLayoutContextForTest uses
// layout.Exact, which asks for the full width as a minimum, and a Flex returns
// at least the minimum it was handed — so under Exact every tab is laid out as
// wide as the window and the row is measured at a size the app never produces.
//
// It is constraints and not a whole context so a render test can apply it to
// the frame's own context, keeping the frame's op list: a fresh op.Ops would
// record the bar into nothing at all and the screenshot would come back blank.
func barConstraints(w, h int) layout.Constraints {
	return layout.Constraints{Max: image.Pt(w, h)}
}

// asTabBar turns a context into the one the tab bar gets inside the real
// window, which is a maximum and a zero minimum.
func asTabBar(gtx layout.Context, w, h int) layout.Context {
	gtx.Constraints = barConstraints(w, h)
	return gtx
}

// manyTabs fills the bar with more tabs than fit, which is the only state in
// which scrolling means anything.
func manyTabs(ui *uiState, n int) {
	ui.openTabs = nil
	for i := 0; i < n; i++ {
		ui.openTabs = append(ui.openTabs, &fileTab{
			path: fmt.Sprintf(`Data\attrib\ebps\races\necrons\troops\unit_number_%02d.rgd`, i),
			kind: "rgd",
		})
	}
	ui.ensureTabClicks()
	ui.tabSel = 1
}

func tabBarTestUI(t *testing.T, tabs int) *uiState {
	t.Helper()
	ui := newUI(unitTestTheme())
	manyTabs(ui, tabs)
	return ui
}

// renderFrame lays the function out over a fresh window and returns the
// screenshot, so a test can look at what was painted rather than at what the
// code says it painted.
func renderFrame(t *testing.T, w, h int, draw func(gtx layout.Context)) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	draw(gtx)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

// imagesDifferIn counts the pixels that are not the same in two shots of the
// same box. Scrolling has to change what is drawn; a bar that reported an offset
// but drew the same tabs would be indistinguishable from one that worked.
func imagesDifferIn(a, b *image.RGBA, box image.Rectangle) int {
	box = box.Intersect(a.Bounds()).Intersect(b.Bounds())
	diff := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				diff++
			}
		}
	}
	return diff
}

// countTabColours is how a frame that drew something is told from a frame that
// drew a flat fill: the border and the background alone look the same either
// way.
func countTabColours(img *image.RGBA, box image.Rectangle) int {
	box = box.Intersect(img.Bounds())
	if box.Empty() {
		return 0
	}
	seen := map[color.RGBA]bool{}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			seen[img.RGBAAt(x, y)] = true
		}
	}
	return len(seen)
}

func tabContextMenuSizePx() (int, int) {
	return tabContextWDP, tabContextPadDp*2 + tabContextRowDp*len(tabContextItems)
}

// ── the bar scrolls ────────────────────────────────────────────────────────

// A tab is as wide as its own text. If it were not, the row would grow by the
// width of the window a tab, and the second tab would be off screen before the
// bar had anything to scroll.
func TestTabsAreAsWideAsTheirLabels(t *testing.T) {
	ui := tabBarTestUI(t, 3)

	gtx := asTabBar(testutil.NewLayoutContext(new(op.Ops), 1400, tabBarHeightDp), 1400, tabBarHeightDp)
	ui.layoutTabRow(gtx)

	if len(ui.tabRects) != 3 {
		t.Fatalf("%d rects for 3 tabs", len(ui.tabRects))
	}
	for i, r := range ui.tabRects {
		// A tab's own text is around 100px; anything near the window's width
		// means the label was asked to fill the space instead of being measured.
		if r.Dx() > 200 {
			t.Errorf("tab %d is %d px wide, want about the width of %q", i, r.Dx(), tabLabelFor(ui.openTabs[i]))
		}
	}
	if ui.tabRowWidth > 800 {
		t.Errorf("3 tabs came to %d px, which is far more than their text", ui.tabRowWidth)
	}
}

// The bar has to end up with an offset it could act on, and the layout is what
// discovers that the row is wider than the bar in the first place.
func TestTabBarMeasuresTheRowAndOffersToScroll(t *testing.T) {
	const w, h = 700, 300
	ui := tabBarTestUI(t, 12)

	gtx := asTabBar(testutil.NewLayoutContext(new(op.Ops), w, h), w, h)
	ui.layoutTabBar(gtx)

	if ui.tabRowWidth <= ui.tabBarWidth {
		t.Fatalf("12 tabs came to %d px in a %d px bar, so there is nothing to scroll", ui.tabRowWidth, ui.tabBarWidth)
	}
	if got, want := ui.tabMaxScroll(ui.tabBarWidth, ui.tabRowWidth), ui.tabRowWidth-ui.tabBarWidth; got != want {
		t.Errorf("maxScroll = %d, want %d", got, want)
	}
	if ui.tabScrollX != 0 {
		t.Errorf("a freshly drawn bar is scrolled to %d", ui.tabScrollX)
	}
	if len(ui.tabRects) != len(ui.openTabs) {
		t.Fatalf("%d rects for %d tabs", len(ui.tabRects), len(ui.openTabs))
	}
	// The rects have to be in one piece and in order, or a right press cannot
	// be resolved to a tab.
	for i, r := range ui.tabRects {
		if r.Empty() {
			t.Errorf("tab %d has no rect", i)
		}
		if i > 0 && r.Min.X <= ui.tabRects[i-1].Max.X {
			t.Errorf("tab %d starts at %d, not after tab %d ends at %d",
				i, r.Min.X, i-1, ui.tabRects[i-1].Max.X)
		}
	}
}

// The point of the whole thing: the row moves sideways when the bar is scrolled.
func TestTabBarActuallyScrollsSideways(t *testing.T) {
	const w, h = 700, 300
	ui := tabBarTestUI(t, 12)

	gtx := asTabBar(testutil.NewLayoutContext(new(op.Ops), w, h), w, h)
	ui.layoutTabBar(gtx)
	strip := image.Rectangle{Max: image.Pt(w, tabBarHeightDp)}

	left := renderFrame(t, w, h, func(gtx layout.Context) { ui.layoutTabBar(asTabBar(gtx, w, h)) })

	// A notch's worth, which is what the wheel produces.
	ui.tabScrollX += int(wheelNotchDelta * tabWheelStepPx)
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)
	if ui.tabScrollX == 0 {
		t.Fatalf("the bar refused to scroll at all (row %d, bar %d)", ui.tabRowWidth, ui.tabBarWidth)
	}

	scrolled := renderFrame(t, w, h, func(gtx layout.Context) { ui.layoutTabBar(asTabBar(gtx, w, h)) })

	diff := imagesDifferIn(left, scrolled, strip)
	if diff == 0 {
		t.Error("scrolling by one notch changed nothing in the tab strip: the row is not being moved")
	} else {
		t.Logf("%d pixels of the %d px strip changed after scrolling %d px", diff, strip.Dx(), ui.tabScrollX)
	}
}

// Scrolled to the end, the last tab has to end at the bar's own width and
// nothing may be drawn past it. A missing clip shows up as the row's tabs
// painted into the panel below.
func TestScrolledTabBarClipsToItsWidth(t *testing.T) {
	const w, h = 700, 300
	ui := tabBarTestUI(t, 12)

	gtx := asTabBar(testutil.NewLayoutContext(new(op.Ops), w, h), w, h)
	ui.layoutTabBar(gtx)
	ui.tabScrollX = ui.tabMaxScroll(ui.tabBarWidth, ui.tabRowWidth)
	last := ui.tabRects[len(ui.tabRects)-1]

	// Scrolled to the end, the row's right edge lands exactly on the bar's.
	if onScreen := last.Max.X - ui.tabScrollX; onScreen != w {
		t.Errorf("the last tab ends at %d on screen, want the bar's %d px", onScreen, w)
	}

	// A clip that was not applied would leave the row's own background or tabs
	// past the bar. Both are the bar's background there, so the strip right at
	// the edge is compared with the same strip on an unscrolled bar.
	scrolledShot := renderFrame(t, w, h, func(gtx layout.Context) { ui.layoutTabBar(asTabBar(gtx, w, h)) })
	ui.tabScrollX = 0
	plainShot := renderFrame(t, w, h, func(gtx layout.Context) { ui.layoutTabBar(asTabBar(gtx, w, h)) })

	// The first tab is 100px of text, so at the left of the bar the unscrolled
	// shot is busy and the fully scrolled one is not: that is the tabs having
	// moved, and it is the same clip either way.
	// A strip at the left of the bar, wide enough to hold the first tab and
	// the start of the second. It used to be two wheel steps wide, which made
	// it a measure of the wheel rather than of the tabs.
	const headWidthPx = 120
	head := image.Rectangle{Min: image.Pt(0, 0), Max: image.Pt(headWidthPx, tabBarHeightDp)}
	before := countTabColours(plainShot, head)
	after := countTabColours(scrolledShot, head)
	if before <= after {
		t.Errorf("the left end has %d colours scrolled and %d unscrolled: the first tabs did not move out of it", after, before)
	}
}

// A handful of tabs fit, so the bar must not scroll at all: sliding its own
// background out of view would leave a gap with nothing in it.
func TestShortTabBarDoesNotScroll(t *testing.T) {
	const w, h = 1400, 300
	ui := tabBarTestUI(t, 2)

	gtx := asTabBar(testutil.NewLayoutContext(new(op.Ops), w, h), w, h)
	ui.layoutTabBar(gtx)
	if ui.tabRowWidth > ui.tabBarWidth {
		t.Skipf("two tabs took %d px in a %d px bar, the fixture is too narrow", ui.tabRowWidth, ui.tabBarWidth)
	}
	ui.tabScrollX = 500
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)
	if ui.tabScrollX != 0 {
		t.Errorf("a bar that fits scrolled to %d", ui.tabScrollX)
	}
}

// ── the context menu ───────────────────────────────────────────────────────

// The menu is a frame and a label until something is drawn inside it, so the
// test compares the open menu against the same spot with it closed.
func TestTabContextMenuRendersItsItem(t *testing.T) {
	const w, h = 900, 600
	ui := tabBarTestUI(t, 4)
	pos := ui.tabContextPoint(layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}, 300)
	menuW, menuH := tabContextMenuSizePx()

	ui.tabContextPos = pos
	closed := renderFrame(t, w, h, func(gtx layout.Context) { ui.layoutTabContextMenu(gtx) })
	ui.tabContextVisible = true
	open := renderFrame(t, w, h, func(gtx layout.Context) { ui.layoutTabContextMenu(gtx) })

	box := image.Rectangle{Min: pos, Max: image.Pt(pos.X+menuW, pos.Y+menuH)}
	if diff := imagesDifferIn(closed, open, box); diff == 0 {
		t.Error("the open menu is identical to the closed frame: nothing was drawn")
	}
	if colours := countTabColours(open, box); colours < 5 {
		t.Errorf("only %d distinct colours in the menu, so no item was drawn", colours)
	}
}

// A tab near the right edge has no room to the right of it, and a click near
// the bottom has none below; the menu flips rather than opening off screen,
// which would take its only item out of reach. Every corner is checked because
// the layout only ever sees one of them at a time.
func TestTabContextMenuStaysInsideTheWindow(t *testing.T) {
	const w, h = 900, 300
	menuW, menuH := tabContextMenuSizePx()

	for _, tc := range []struct {
		name string
		at   image.Point
	}{
		{"the far right", image.Pt(w-6, 100)},
		{"the bottom right", image.Pt(w-6, h-6)},
		{"the bottom left", image.Pt(4, h-6)},
		{"the top left", image.Pt(2, 2)},
		{"a tab in the middle", image.Pt(400, 60)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tabContextMenuOrigin(tc.at, w, h, menuW, menuH, tabContextInsetP)
			if got.X < 0 || got.Y < 0 {
				t.Errorf("menu at %v has its top left off screen", got)
			}
			if got.X+menuW > w {
				t.Errorf("menu at %v runs %d px past the right edge", got, got.X+menuW-w)
			}
			if got.Y+menuH > h {
				t.Errorf("menu at %v runs %d px past the bottom", got, got.Y+menuH-h)
			}
		})
	}
}

// With room on the right the menu opens at the click rather than being flipped
// needlessly to the left of it.
func TestTabContextMenuOpensAtTheClickWhenThereIsRoom(t *testing.T) {
	const w, h = 900, 300
	menuW, menuH := tabContextMenuSizePx()

	got := tabContextMenuOrigin(image.Pt(120, 60), w, h, menuW, menuH, tabContextInsetP)
	if got.X != 120 || got.Y != 60 {
		t.Errorf("menu at %v, want it at the click (120,60)", got)
	}
}

// The menu is placed by an offset op, so where it really landed is worth
// checking against a frame rather than only against the arithmetic.
func TestTabContextMenuLandsWhereItWasPlaced(t *testing.T) {
	const w, h = 900, 300
	ui := tabBarTestUI(t, 4)
	ui.tabContextVisible = true
	ui.tabContextPos = image.Pt(500, 80)
	menuW, menuH := tabContextMenuSizePx()
	want := tabContextMenuOrigin(ui.tabContextPos, w, h, menuW, menuH, tabContextInsetP)

	// Painted last over a known background, so anything inside the menu's box
	// that is not the background is the menu having landed there.
	img := renderFrame(t, w, h, func(gtx layout.Context) {
		fillRect(gtx, cBg, gtx.Constraints.Max)
		ui.layoutTabContextMenu(gtx)
	})

	box := image.Rectangle{Min: want, Max: want.Add(image.Pt(menuW, menuH))}
	if countTabColours(img, box) < 5 {
		t.Errorf("nothing but the background was painted inside %v, the menu's own box", box)
	}
}
