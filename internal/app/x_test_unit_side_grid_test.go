package app

import (
	"image"
	"testing"

	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/testutil"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

// The panel beside the canvas lays its buttons out as a grid: three cells to a
// row, every cell the same size however many buttons the row holds, and every
// button one line of text tall. Five buttons across the panel left "Animations"
// wrapped over two lines; three across leaves every cell the width of
// "Reset view", which is the longest label on it.
func TestTheViewportSidePanelsButtonsShareEqualCells(t *testing.T) {
	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, 1200, 760)
	// The panel is unitViewportSideDp across, padded unitSideButtons' 6dp on
	// each side, which is what a row of its cells is divided between.
	inner := gtx.Dp(unit.Dp(unitViewportSideDp - 12))
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = image.Pt(inner, 4000)
	want := image.Pt((inner-2*gtx.Dp(unit.Dp(unitSideButtonGapDp)))/unitSideButtonCells, 0)

	// Two rows of three cells, as the tabs and the row below them are laid
	// out. A cell reports the size it was offered rather than draw into it.
	var cells []image.Point
	grab := func(gtx layout.Context) layout.Dimensions {
		cells = append(cells, gtx.Constraints.Min)
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
	}
	unitSideButtonGrid(gtx,
		[]layout.Widget{grab, grab, grab},
		[]layout.Widget{grab, grab, grab},
	)
	if len(cells) != 6 {
		t.Fatalf("the grid laid out %d cells, want the six of two rows", len(cells))
	}
	for i, cell := range cells {
		if cell != want {
			t.Errorf("cell %d of the grid is %v, want %v", i, cell, want)
		}
	}

	// Every button laid into one of those cells is the same size: the same
	// width as the cell, and the same height whether its label is the
	// longest on the panel or the shortest. A label that wrapped would make
	// its button a line taller than the rest.
	ui := newUI(testutil.UnitTestTheme())
	var clicks [2]widget.Clickable
	measure := func(label string) image.Point {
		g := gtx
		g.Constraints.Min = want
		g.Constraints.Max.Y = 4000
		return SmallButton(ui, g, &clicks[0], label).Size
	}
	bones, resetView := measure("Bones"), measure("Reset view")
	if bones != resetView {
		t.Errorf("\"Reset view\" came out %v against the %v of \"Bones\", so the row is not one height", resetView, bones)
	}
	if bones.X != want.X {
		t.Errorf("a button of the row is %d wide, want its cell's %d", bones.X, want.X)
	}
	for _, label := range []string{"Animations", "Markers", "Bones", "FX", "Meshes"} {
		g := gtx
		g.Constraints.Min = want
		g.Constraints.Max.Y = 4000
		if got := ToggleButton(ui, g, &clicks[1], label, false, uikit.CSelectBg).Size; got != bones {
			t.Errorf("the %q tab came out %v, want the %v of the buttons beside it", label, got, bones)
		}
	}
}
