package main

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
)

// The open unit list must actually fill its frame. A list whose rows collapsed
// to a height of zero still draws its border and background, so it looks like a
// perfectly framed empty box — the frame alone proves nothing, and that is how
// the bug reached a build. What tells the two apart is the content: rows carry
// a selection tint and text, a dead list is one flat colour.
func TestUnitDropdownRendersItsRows(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	necrons := cat.findRace("necrons")
	if necrons == nil || len(necrons.unitsOf()) < 3 {
		t.Skip("necrons has too few units to test with")
	}

	const w, h = 900, 700
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	ui := &uiState{th: unitTestTheme(), modTree: tree}
	ui.openUnitTab()
	ui.unitListOpen = true

	var ops op.Ops
	gtx := newLayoutContextForTest(&ops, w, h)
	ui.layoutUnitTabBody(gtx)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}

	// The layout records where the list landed, so the probe can be aimed at it
	// rather than at the columns beside it.
	box := ui.unitDropBox
	t.Logf("dropdown box %v", box)
	if box.Empty() {
		t.Fatal("the layout reported no dropdown box at all")
	}
	rowH := unitRowHeightDp
	if box.Dy() < rowH*3 {
		t.Errorf("the dropdown is %d px tall, too short for the %d px rows it is meant to show", box.Dy(), rowH)
	}
	if box.Max.X > w || box.Max.Y > h {
		t.Errorf("the dropdown box %v runs past the %dx%d window", box, w, h)
	}

	// Count the distinct colours over the whole list. A filled list carries the
	// selection tint, the frame and the row glyphs; a dead one is a flat panel
	// fill with a border, which is only a couple of colours no matter how big
	// the frame is.
	distinct := map[color.RGBA]int{}
	b := img.Bounds()
	for y := box.Min.Y; y < box.Max.Y && y < b.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X && x < b.Max.X; x++ {
			distinct[img.RGBAAt(x, y)]++
		}
	}
	t.Logf("dropdown box %v: %d distinct colours", box, len(distinct))
	if len(distinct) < 8 {
		t.Errorf("the unit list drew only %d distinct colours inside %v, so it is an empty box: %v",
			len(distinct), box, distinct)
	}

	// The rows must also be tall enough for the list to hold several of them.
	ui.ensureUnitDropdownClicks(len(necrons.unitsOf()))
	if ui.unitListScroll.contentHeight <= 0 {
		t.Error("the list reported no content height")
	}
}

// unitDropdownProbeColumn samples one vertical line through the dropdown, kept
// for diagnostics when a failure needs a closer look.
func unitDropdownProbeColumn(img *image.RGBA, box image.Rectangle) []color.RGBA {
	x := box.Min.X + 6
	b := img.Bounds()
	if x >= b.Max.X {
		x = b.Max.X - 1
	}
	var out []color.RGBA
	for y := box.Min.Y; y < box.Max.Y && y < b.Max.Y; y++ {
		out = append(out, img.RGBAAt(x, y))
	}
	return out
}

var _ = unitDropdownProbeColumn
