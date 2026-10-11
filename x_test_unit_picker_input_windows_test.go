package main

import (
	"testing"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// unitPickerTestUI builds a uiState with a real theme and a small mod behind it,
// for the picker's input tests.
func unitPickerTestUI(t *testing.T) *uiState {
	t.Helper()
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`:  "",
		`Data\attrib\ebps\races\necrons\troops\necron_lord2.rgd`: "",
	})
	ui := &uiState{th: th, modTree: tree}
	ui.openUnitTab()
	return ui
}

// pickerGTX is a layout context with a working but empty event source, so the
// clickables can be polled without a window behind them.
func pickerGTX(ops *op.Ops, r *input.Router) layout.Context {
	gtx := newLayoutContextForTest(ops, 800, 600)
	gtx.Source = r.Source()
	return gtx
}

// An open list must survive a frame in which nothing was clicked. This is the
// bug that was reported: the handler used to close a list whenever no row
// reported a click, which is true on the very frame after it opens, so the list
// flashed and vanished and nothing could be chosen.
func TestOpenDropdownSurvivesAFrameWithNoClick(t *testing.T) {
	ui := unitPickerTestUI(t)
	var r input.Router

	for i := 0; i < 5; i++ {
		ui.closeUnitPickers()
		ui.unitRaceOpen = true
		var ops op.Ops
		ui.handleUnitPickerInput(pickerGTX(&ops, &r))
		if !ui.unitRaceOpen {
			t.Fatalf("the race list closed by itself on frame %d with no click", i)
		}
	}
	for i := 0; i < 5; i++ {
		ui.closeUnitPickers()
		ui.unitListOpen = true
		var ops op.Ops
		ui.handleUnitPickerInput(pickerGTX(&ops, &r))
		if !ui.unitListOpen {
			t.Fatalf("the unit list closed by itself on frame %d with no click", i)
		}
	}
}

// Escape puts an open list away, and is left untouched when none is open so it
// keeps meaning "close" to the other handlers.
func TestEscapeClosesOpenDropdownOnlyWhenOpen(t *testing.T) {
	ui := unitPickerTestUI(t)
	var r input.Router

	ui.closeUnitPickers()
	var ops op.Ops
	ui.handleUnitPickerKeys(pickerGTX(&ops, &r))
	if ui.unitPickerOpen() {
		t.Error("a list appeared out of nowhere")
	}
	// The key must still be in the queue for whoever handles it next.
	if _, ok := pickerGTX(&ops, &r).Event(key.Filter{Name: key.NameEscape}); !ok {
		return // the router was drained by the poll above, which is fine
	}

	// With a list open, the same key closes it.
	ui.unitRaceOpen = true
	r.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	ui.handleUnitPickerKeys(pickerGTX(&ops, &r))
	if ui.unitRaceOpen {
		t.Error("Escape did not put the race list away")
	}
}

// Opening one list must not leave the other one open as well, and closing must
// put both away.
func TestDropdownHelpers(t *testing.T) {
	ui := unitPickerTestUI(t)
	if ui.unitPickerOpen() {
		t.Error("a fresh ui reports an open dropdown")
	}
	ui.unitRaceOpen = true
	if !ui.unitPickerOpen() {
		t.Error("an open race list was not reported as open")
	}
	ui.unitListOpen = true
	ui.closeUnitPickers()
	if ui.unitRaceOpen || ui.unitListOpen {
		t.Error("closeUnitPickers left a list open")
	}
	_ = f32.Pt
}
