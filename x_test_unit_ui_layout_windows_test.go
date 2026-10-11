package main

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// TestUnitTabRendersWithRealMod lays the object tab out headlessly with a real
// mod behind it, for every section and at several window widths. The tab
// browses the mod's own units, so the pickers and the populated columns only
// exist on this path: a layout mistake — a column collapsing to zero, a
// dropdown overflowing, a stale index panicking the frame — would not be
// caught by the pure functions the other tests cover.
func TestUnitTabRendersWithRealMod(t *testing.T) {
	tree := openTestDXP3(t)

	sizes := []image.Point{
		{X: 1600, Y: 900},
		{X: 1100, Y: 700},
		{X: 820, Y: 560},
	}
	for _, size := range sizes {
		win, err := headless.NewWindow(size.X, size.Y)
		if err != nil {
			t.Skipf("no GPU backend: %v", err)
		}
		// Every section, not a hand-picked few: the list grew a fifth entry
		// when the Squads column was added, and a hard-coded range would have
		// quietly stopped covering the last one.
		for _, section := range allUnitSectionIndexes() {
			for _, raceSel := range []int{0, 3, 99} {
				ui := &uiState{th: unitTestTheme(), modTree: tree}
				ui.openUnitTab()
				ui.unitSection = section
				ui.unitRaceSel = raceSel
				// An open dropdown is the case most likely to overflow the
				// columns, since it is drawn over them.
				ui.unitListOpen = section == 1
				ui.unitRaceOpen = section == 2

				var ops op.Ops
				gtx := newLayoutContextForTest(&ops, size.X, size.Y)
				ui.layoutContent(gtx)

				img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
				if err := win.Frame(&ops); err != nil {
					win.Release()
					t.Fatalf("Frame at %dx%d: %v", size.X, size.Y, err)
				}
				if err := win.Screenshot(img); err != nil {
					win.Release()
					t.Fatalf("Screenshot: %v", err)
				}
				if !unitFrameHasContent(img) {
					win.Release()
					t.Errorf("blank frame at %dx%d section %d race %d: the tab drew nothing",
						size.X, size.Y, section, raceSel)
				}
			}
		}
		win.Release()
	}
}

// The content area must show the file tree and the middle panel again once the
// object tab is left, or the tab would take the window over permanently.
func TestContentAreaRestoresWhenTabIsNotActive(t *testing.T) {
	tree := openTestDXP3(t)
	ui := &uiState{th: unitTestTheme(), modTree: tree}
	ui.openUnitTab()
	if !ui.unitTabOwnsLeftPanel() || !ui.unitTabOwnsMiddlePanel() {
		t.Fatal("the object tab should own the left and middle panels while it is active")
	}
	// Selecting anything else hands the panels back.
	ui.tabSel = 0
	if ui.unitTabOwnsLeftPanel() || ui.unitTabOwnsMiddlePanel() {
		t.Error("the object tab kept the panels after the selection moved away")
	}
	if isUnitTab(ui.activeTab()) {
		t.Error("the object tab should not be the active tab on the Module tab")
	}
}

// unitTestTheme builds a material theme with a real font collection, without
// which every label in the tab would panic on a nil shaper.
func unitTestTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return th
}

// allUnitSectionIndexes is every section of the object tab, so a layout test can
// cover all of them without repeating a range that goes stale when one is added.
func allUnitSectionIndexes() []int {
	out := make([]int, 0, len(unitSections))
	for i := range unitSections {
		out = append(out, i)
	}
	return out
}

// unitFrameHasContent reports whether a frame carries more than a couple of
// colours, which is the cheapest sign that something was actually drawn.
func unitFrameHasContent(img *image.RGBA) bool {
	seen := make(map[color.RGBA]bool, 8)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 3 {
		for x := b.Min.X; x < b.Max.X; x += 3 {
			seen[img.RGBAAt(x, y)] = true
			if len(seen) > 4 {
				return true
			}
		}
	}
	return false
}
