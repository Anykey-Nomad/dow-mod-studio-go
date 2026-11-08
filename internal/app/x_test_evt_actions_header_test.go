package app

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestTheNewActionButtonTakesItsOwnLineInANarrowColumn covers the header row
// of the Actions block: Copy action, Paste action and New action side by side
// are wider than a narrow middle column, and a button drawn past the column's
// edge is one that cannot be pressed. The block gives the new-action button a
// line of its own when they do not fit, and keeps the three together once the
// column is wide enough — measured as the height of the pinned part, which is
// the only thing that changes: the rows below are the same either way.
func TestTheNewActionButtonTakesItsOwnLineInANarrowColumn(t *testing.T) {
	ui := eventsSectionUI(t, 1400, 800)
	heightAt := func(w, h int) int {
		t.Helper()
		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Constraints{Min: image.Pt(0, 0), Max: image.Pt(w, h)},
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		}
		return LayoutUnitEvtActions(ui, gtx, ui.UnitEvtDoc.Actions()).Size.Y
	}

	// Wide enough for the three on one line, and wide enough by a margin:
	// the row cannot be hanging over the edge in either.
	wide := heightAt(1000, 5000)
	if again := heightAt(1400, 5000); again != wide {
		t.Errorf("the pinned part is %dpx at a 1000px column and %dpx at 1400px: the buttons are not fitting the wide column as one line",
			wide, again)
	}
	// Too narrow for the three: the pinned part grows by the line the
	// new-action button then takes of its own.
	narrow := heightAt(200, 5000)
	if narrow <= wide {
		t.Errorf("at a 200px column the pinned part is %dpx against %dpx wide: the three buttons cannot share a line there and have to wrap",
			narrow, wide)
	}
	if diff := narrow - wide; diff < 16 || diff > 64 {
		t.Errorf("the wrapped line of its own measures %dpx (narrow %d, wide %d), want about one button's height",
			diff, narrow, wide)
	}
}
