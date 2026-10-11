package main

import (
	"image"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// ── the Object tab ──────────────────────────────────────────────────────────
//
// The object interface used to be a modal window over the whole app. It is a
// tab now: the Object button in the menu bar opens it, it sits in the tab bar
// beside the file tabs, and the standard right panel stays where it was.
//
// While it is active the content area gives up its file tree and its middle
// panel: the Object's three columns take the space of the left and middle
// panels, which is where they are widest and where nothing else is wanted. The
// right panel is untouched, so the usual properties panel is still on screen.

// unitTabKind is the fileTab kind of the object interface. It is a tab kind
// rather than a tab of its own type so that it rides the existing tab bar,
// selection, closing and click handling without a second mechanism.
const unitTabKind = "unit"

// unitTabLabel is what the tab calls itself in the bar.
const unitTabLabel = "Object"

// unitTabPath is the virtual path recorded for the tab. It is not a real file:
// the tab is not backed by one, and the empty path is what marks it as such,
// so session saving skips it and a mod reload leaves it alone.
const unitTabPath = ""

// isUnitTab reports whether a tab is the object interface.
func isUnitTab(t *fileTab) bool {
	return t != nil && t.kind == unitTabKind
}

// unitTabIndex is where the object tab sits in the tab list, or -1 when it is
// not open.
func (ui *uiState) unitTabIndex() int {
	for i, t := range ui.openTabs {
		if isUnitTab(t) {
			return i
		}
	}
	return -1
}

// unitTabActive reports whether the object interface is the tab on screen.
func (ui *uiState) unitTabActive() bool {
	return isUnitTab(ui.activeTab())
}

// openUnitTab shows the object interface, opening its tab the first time. It is
// a singleton: asking for it again just brings the existing tab forward, so the
// user cannot end up with two copies browsing the same units.
func (ui *uiState) openUnitTab() {
	if idx := ui.unitTabIndex(); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		return
	}
	ui.openTabs = append(ui.openTabs, &fileTab{kind: unitTabKind, path: unitTabPath})
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
}

// closeUnitTab removes the object tab if it is open. Closing it leaves the
// selection alone, so reopening it lands on the same unit.
func (ui *uiState) closeUnitTab() {
	idx := ui.unitTabIndex()
	if idx < 0 {
		// The tab is already gone, but a dropdown it left open would still be
		// drawn over whatever replaced it, so the state is cleared either way.
		ui.unitRaceOpen = false
		ui.unitListOpen = false
		return
	}
	ui.closeTabAt(idx)
	// An open dropdown belongs to the tab that went away.
	ui.unitRaceOpen = false
	ui.unitListOpen = false
}

// ── the body ────────────────────────────────────────────────────────────────

// layoutUnitTabBody is the object interface as the content area's middle: the
// picker strip above the three columns.
//
// The order of the layers is what makes an open list usable. The columns come
// first and the strip and its lists last, so a list taller than the strip paints
// over the columns instead of being painted over by them — a strip laid out as
// a row above the columns would be drawn first, and the columns would then
// cover everything the list hangs below it.
func (ui *uiState) layoutUnitTabBody(gtx layout.Context) layout.Dimensions {
	stripH := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	pad := gtx.Dp(unit.Dp(unitTabPadDp))
	return layout.Stack{}.Layout(gtx,
		// The columns, with the strip's height kept clear at the top.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(stripH + pad), Left: unit.Dp(pad), Right: unit.Dp(pad), Bottom: unit.Dp(pad)}.Layout(gtx, ui.layoutUnitBody)
		}),
		// While a list is open, a transparent catcher over the columns: a click
		// that misses the strip and the list lands here and puts the list away.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.unitPickerOpen() {
				return layout.Dimensions{}
			}
			pass := pointer.PassOp{}.Push(gtx.Ops)
			dims := ui.unitDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			pass.Pop()
			return dims
		}),
		// The strip, and any open list hanging below it, on top of both.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			ui.layoutUnitPickerLayer(gtx, stripH, pad)
			// The layer reports the whole area, so the columns are laid out at
			// full size and the strip is free to overhang them.
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
	)
}

// layoutUnitPickerLayer draws the picker strip at the top of its layer and then
// draws whichever list is open underneath it, offset to sit under the field the
// list belongs to.
//
// The two trigger fields are measured first, against a throwaway op stream, so
// the list can be lined up with its own field. The fields carry no clickable of
// their own — the clickable is wrapped around them at the call site — so
// measuring them here neither draws anything twice nor counts a click twice.
func (ui *uiState) layoutUnitPickerLayer(gtx layout.Context, stripH, pad int) {
	measure := func(label, value string) int {
		var scratch op.Ops
		mg := gtx
		mg.Ops = &scratch
		mg.Constraints.Min = image.Pt(0, stripH)
		mg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
		return ui.unitPickerField(mg, label, value, true, false).Size.X
	}
	// How far the unit field sits from the strip's own left edge. The list is
	// drawn at the same offset, so it hangs under the field that opened it.
	unitX := measure("Race", ui.pickerRaceLabel()) + gtx.Dp(unit.Dp(unitColGapDp))

	// The strip itself, at the top of the layer with the usual padding.
	sg := gtx
	sg.Constraints.Min = image.Pt(0, stripH)
	sg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
	padOff := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
	ui.layoutUnitPickerRow(sg)
	padOff.Pop()

	if !ui.unitPickerOpen() {
		ui.unitDropBox = image.Rectangle{}
		return
	}
	// The list hangs below the strip, over the columns. Its height is not taken
	// from the layer, so it can be as tall as it needs.
	lg := gtx
	lg.Constraints.Min = image.Pt(0, 0)
	lg.Constraints.Max = image.Pt(gtx.Constraints.Max.X-pad, gtx.Constraints.Max.Y-stripH)
	listOff := op.Offset(image.Pt(pad, stripH)).Push(gtx.Ops)
	offset := unitX
	if ui.unitRaceOpen {
		offset = 0
	}
	dims := ui.layoutUnitDropdown(lg, offset)
	listOff.Pop()
	// Recorded for tests, which otherwise have no way to know where the list
	// landed and end up probing the columns beside it.
	ui.unitDropBox = image.Rect(pad+offset, stripH, pad+offset+dims.Size.X, stripH+dims.Size.Y)
}

// ── the content area ────────────────────────────────────────────────────────

// unitTabOwnsLeftPanel reports whether the object tab is drawn over the area
// the file tree normally occupies. It does, since the tree has nothing to say
// about browsing units and the section menu is the left column instead.
func (ui *uiState) unitTabOwnsLeftPanel() bool { return ui.unitTabActive() }

// unitTabOwnsMiddlePanel is the same question for the middle panel, and the
// answer matches: the object columns take that space too.
func (ui *uiState) unitTabOwnsMiddlePanel() bool { return ui.unitTabActive() }

// unitInfoTable is the object tab's right hand panel: what the selected unit
// is and where it came from. It deliberately repeats only the identifying rows
// — the race, the kind and the file — because the columns already carry the
// unit's properties, and a panel that listed them again would say nothing new.
func (ui *uiState) unitInfoTable(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.th, "No unit selected", unit.Sp(10), cTextDim)
			}),
		)
	}
	rows := []struct{ label, value string }{
		{"Name", d.entry.name},
		{"Race", d.entry.race},
		{"Kind", d.entry.kind.label},
		{"File", d.entry.path},
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
	}
	for i, r := range rows {
		r := r
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, r.label, r.value, i%2 == 1)
		}))
	}
	// The screen name is the one row here that is a localisation rather than a
	// fact about the file, so it is drawn as an editable one: the text, the id
	// on hover, and the presses that change it. A unit with no screen name still
	// gets the row, because "this unit has no name yet" is the thing a reader
	// most wants to be able to fix from here.
	screenNameID := d.screenNameID()
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.localePanelRow(gtx, "Screen name", screenNameID, localeFieldForID(d, screenNameID))
	}))
	if d.error != "" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "Could not read this unit:\n"+d.error)
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitTabLabelFor is the tab bar's name for a tab of the object kind, kept apart
// so the bar's label chain does not grow another kind case.
func unitTabLabelFor(t *fileTab) string {
	if isUnitTab(t) {
		return unitTabLabel
	}
	return ""
}

// unitTabHasNoFile reports that the object tab is not backed by a file, which
// is what the mod reload check and the session writer need to know: a tab with
// no file must not be closed when a file disappears, and must not be persisted
// as a path.
func unitTabHasNoFile(t *fileTab) bool {
	return isUnitTab(t) || strings.TrimSpace(t.path) == ""
}
