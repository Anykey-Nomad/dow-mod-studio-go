package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"image"
	"path/filepath"
	"strings"
)

// unitTabActive reports whether the object interface is the tab on screen.
func UnitTabActive(ui *uiState) bool {
	return isUnitTab(ActiveTab(ui))
}

// closeUnitTab removes the object tab if it is open. Closing it leaves the
// selection alone, so reopening it lands on the same unit.
func CloseUnitTab(ui *uiState) {
	idx := UnitTabIndex(ui)
	if idx < 0 {
		// The tab is already gone, but a dropdown it left open would still be
		// drawn over whatever replaced it, so the state is cleared either way.
		ui.UnitRaceOpen = false
		ui.UnitListOpen = false
		return
	}
	CloseTabAt(ui, idx)
	// An open dropdown belongs to the tab that went away.
	ui.UnitRaceOpen = false
	ui.UnitListOpen = false
}

// layoutUnitTabBody is the object interface as the content area's middle: the
// picker strip above the three columns.
//
// The order of the layers is what makes an open list usable. The columns come
// first and the strip and its lists last, so a list taller than the strip paints
// over the columns instead of being painted over by them — a strip laid out as
// a row above the columns would be drawn first, and the columns would then
// cover everything the list hangs below it.
func LayoutUnitTabBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	stripH := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	pad := gtx.Dp(unit.Dp(unitTabPadDp))
	return layout.Stack{}.Layout(gtx,
		// The columns, with the strip's height kept clear at the top.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(stripH + pad), Left: unit.Dp(pad), Right: unit.Dp(pad), Bottom: unit.Dp(pad)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitBody(ui, gtx) })
		}),
		// While a list is open, a transparent catcher over the columns: a click
		// that misses the strip and the list lands here and puts the list away.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !UnitPickerOpen(ui) {
				return layout.Dimensions{}
			}
			pass := pointer.PassOp{}.Push(gtx.Ops)
			dims := ui.UnitDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			pass.Pop()
			return dims
		}),
		// The strip, and any open list hanging below it, on top of both.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			LayoutUnitPickerLayer(ui, gtx, stripH, pad)
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
func LayoutUnitPickerLayer(ui *uiState, gtx layout.Context, stripH, pad int) {
	measure := func(label, value string) int {
		var scratch op.Ops
		mg := gtx
		mg.Ops = &scratch
		mg.Constraints.Min = image.Pt(0, stripH)
		mg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
		return UnitPickerField(ui, mg, label, value, true, false).Size.X
	}
	// How far the unit field sits from the strip's own left edge. The list is
	// drawn at the same offset, so it hangs under the field that opened it.
	unitX := measure("Race", PickerRaceLabel(ui)) + gtx.Dp(unit.Dp(unitColGapDp))

	// The strip itself, at the top of the layer with the usual padding.
	sg := gtx
	sg.Constraints.Min = image.Pt(0, stripH)
	sg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
	padOff := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
	LayoutUnitPickerRow(ui, sg)
	padOff.Pop()

	if !UnitPickerOpen(ui) {
		ui.UnitDropBox = image.Rectangle{}
		return
	}
	// The list hangs below the strip, over the columns. Its height is not taken
	// from the layer, so it can be as tall as it needs.
	lg := gtx
	lg.Constraints.Min = image.Pt(0, 0)
	lg.Constraints.Max = image.Pt(gtx.Constraints.Max.X-pad, gtx.Constraints.Max.Y-stripH)
	listOff := op.Offset(image.Pt(pad, stripH)).Push(gtx.Ops)
	offset := unitX
	if ui.UnitRaceOpen {
		offset = 0
	}
	dims := LayoutUnitDropdown(ui, lg, offset)
	listOff.Pop()
	// Recorded for tests, which otherwise have no way to know where the list
	// landed and end up probing the columns beside it.
	ui.UnitDropBox = image.Rect(pad+offset, stripH, pad+offset+dims.Size.X, stripH+dims.Size.Y)
}

// unitTabOwnsLeftPanel reports whether the object tab is drawn over the area
// the file tree normally occupies. It does, since the tree has nothing to say
// about browsing units and the section menu is the left column instead.
func UnitTabOwnsLeftPanel(ui *uiState) bool { return UnitTabActive(ui) }

// unitTabOwnsMiddlePanel is the same question for the middle panel, and the
// answer matches: the object columns take that space too.
func UnitTabOwnsMiddlePanel(ui *uiState) bool { return UnitTabActive(ui) }

// unitInfoTable is the object tab's right hand panel: what the selected unit
// is and where it came from. It deliberately repeats only the identifying rows
// — the race, the kind and the file — because the columns already carry the
// unit's properties, and a panel that listed them again would say nothing new.
func UnitInfoTable(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.Th, "No unit selected", unit.Sp(10), uikit.CTextDim)
			}),
		)
	}
	rows := []struct{ label, value string }{
		{"Name", d.Entry.Name},
		{"Race", d.Entry.Race},
		{"Kind", d.Entry.Kind.Label},
		{"File", d.Entry.Path},
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
	}
	for i, r := range rows {
		r := r
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, r.label, r.value, i%2 == 1)
		}))
	}
	// The screen name is the one row here that is a localisation rather than a
	// fact about the file, so it is drawn as an editable one: the text, the id
	// on hover, and the presses that change it. A unit with no screen name still
	// gets the row, because "this unit has no name yet" is the thing a reader
	// most wants to be able to fix from here.
	screenNameID := d.ScreenNameID()
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LocalePanelRow(ui, gtx, "Screen name", screenNameID, localeFieldForID(d, screenNameID))
	}))
	if d.Error != "" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "Could not read this unit:\n"+d.Error)
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitLocalisedName is the text the chosen language gives a unit, or "" when
// the unit has no name yet, carries none, or the string table has no entry for
// it. The id is only a cache key here; the text is read on the UI thread, so a
// language change needs no rescan.
func UnitLocalisedName(ui *uiState, u unitEntry) string {
	if len(ui.UnitNames) == 0 || u.Path == "" {
		return ""
	}
	id, ok := ui.UnitNames[strings.ToLower(u.Path)]
	if !ok {
		return ""
	}
	return localisedString(id)
}

// unitLabel is what the unit lists call a unit: its localised screen name where
// the string table has one, and its .rgd file name where it does not. The file
// name is a real fallback rather than a placeholder — it is what the game uses
// for the unit's hotkey.
func UnitLabel(ui *uiState, u unitEntry) string {
	if text := UnitLocalisedName(ui, u); text != "" {
		return text
	}
	return u.Name
}

// unitFieldKey names the field the right column's editor is bound to. The
// entry's own address is used rather than the label, because two fields of one
// unit can carry the same label, and an editor bound to the wrong one of them
// would write a value into the wrong place.
func unitFieldKey(f unitField) string {
	return localeRowKey(f.Entry, f.Label)
}

// unitInfoFieldSelected is the row of the info list the right column is
// editing, or nil when there is none — no unit, an empty info list, or a row
// that is a count rather than a field.
func UnitInfoFieldSelected(ui *uiState) *unitField {
	d := CurrentUnitDetails(ui)
	if d == nil || len(d.Fields) == 0 {
		return nil
	}
	idx := clampInt(ui.UnitInfoSel, 0, len(d.Fields)-1)
	f := d.Fields[idx]
	if !unitInfoRowSelectable(&f) {
		return nil
	}
	return &f
}

// applyUnitFieldText writes typed text into the field a row came from and
// rebuilds the rows so the list shows what was just changed.
//
// The text is turned into a value by the same code the raw editor uses, so a
// float typed here is refused on exactly the terms it would be refused there,
// and a value that parses to what the field already holds is not a change. The
// selection is put back on the same field rather than the same index: a change
// that empties a row drops it from the list, and the row after it would
// otherwise slide into the selection.
func ApplyUnitFieldText(ui *uiState, f *unitField, text string) error {
	if f == nil || f.Entry == nil {
		return fmt.Errorf("this row is a count, not a field of the file")
	}
	changed, err := rgdApplyScalarText(f.Entry, text)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	ui.UnitInfoDirty = true
	d := CurrentUnitDetails(ui)
	if d == nil {
		return nil
	}
	d.RebuildFromFile()
	if i := unitFieldIndex(d.Fields, f.Entry); i >= 0 {
		ui.UnitInfoSel = i
	}
	return nil
}

// saveUnitDetails writes the parsed unit back into the mod's own overlay, and
// says where it went. A unit that is not dirty is left alone rather than
// written out again: rewriting an unchanged file is what makes a tool's idea of
// "modified" and a modder's drift apart for no reason.
func SaveUnitDetails(ui *uiState) (string, error) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		return "", fmt.Errorf("no unit is open")
	}
	if !ui.UnitInfoDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.UnitValError != "" {
		return "", fmt.Errorf("correct %s first", ui.UnitValError)
	}
	data, err := d.File.MarshalBinary()
	if err != nil {
		return "", err
	}
	if ui.ModTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	disk, err := ui.ModTree.WriteOverlayFile(d.Entry.Path, data)
	if err != nil {
		return "", err
	}
	ui.UnitInfoDirty = false
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(d.Entry.Path), disk, len(data)), nil
}

// markUnitFieldDirty marks the unit on screen as changed when a field of it has
// been changed by something other than this panel. Giving an untranslated field
// a new localisation id is the case that matters: the string itself is written
// to the .ucs, but the field that has to name it only exists in memory until
// the unit is saved, and without this the unit keeps showing no name in game
// while the app shows it translated.
//
// A field of another file is ignored, because a change to a file that is not on
// screen is that file's own business.
func MarkUnitFieldDirty(ui *uiState, f *rgd.RgdEntry) {
	if f == nil {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil || rgdPathOf(d.File.Root, f) == "" {
		return
	}
	ui.UnitInfoDirty = true
}

// unitSyncValEditor reads the editor's events and applies what it says to the
// selected field. Enter saves rather than inserting a line break, so a value can
// be typed and committed without reaching for the button.
//
// The editor is bound to a field rather than to a row index, because the row a
// field sits at moves when the list is rebuilt and a bound-to-index editor
// would then write into whatever took its place.
func UnitSyncValEditor(ui *uiState, gtx layout.Context, f *unitField) {
	if f == nil || f.Entry == nil || !rgdIsEditableType(f.Entry.Type) || f.Entry.Type == rgd.RgdTypeBool {
		return
	}
	ed := &ui.UnitValEditor
	fieldKey := unitFieldKey(*f)
	if ui.UnitValBound != fieldKey {
		ed.SetText(f.Entry.DisplayValue())
		ui.UnitValBound = fieldKey
		ui.UnitValError = ""
	}
	ed.SingleLine = true
	ed.Submit = true
	// The editor is a one line field, so a press into it has to be given focus
	// or the reader would type into the window and wonder where it went.
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	submit := false
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEnter})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if ok && e.State == key.Press {
			submit = true
		}
	}
	for {
		if _, ok := ed.Update(gtx); !ok {
			break
		}
	}
	if err := ApplyUnitFieldText(ui, f, ed.Text()); err != nil {
		ui.UnitValError = err.Error()
	} else {
		ui.UnitValError = ""
	}
	if submit {
		SaveUnitFromPanel(ui)
	}
}

// saveUnitFromPanel is the Save button's action: it writes the unit and puts
// what happened in the status line, which is where every other write in the app
// reports itself.
func SaveUnitFromPanel(ui *uiState) {
	if !ui.UnitInfoDirty {
		ui.StatusText = "This unit has no unsaved changes"
		return
	}
	line, err := SaveUnitDetails(ui)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = line
	MarkStateDirty(ui)
}

// handleUnitFieldInput owns the presses of the attribute editor: the rows of
// the info list that select a field, the boolean toggle, the editor's typing
// and the three buttons. It runs after the section list, so a press that
// changed section is already seen here.
func HandleUnitFieldInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != "Unit info" {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil {
		return
	}
	EnsureUnitInfoClicks(ui, len(d.Fields))
	for i := range ui.UnitInfoClicks {
		if i < len(ui.UnitInfoClicks) && ui.UnitInfoClicks[i].Clicked(gtx) {
			ui.UnitInfoSel = i
		}
	}
	if ui.UnitOpenRgdClick.Clicked(gtx) {
		OpenRgd(ui, d.Entry.Path)
		return
	}
	if ui.UnitRevertClick.Clicked(gtx) {
		RevertUnitDetails(ui)
		ui.StatusText = "Changes to this unit thrown away"
		return
	}
	if ui.UnitSaveClick.Clicked(gtx) {
		SaveUnitFromPanel(ui)
		return
	}
	f := UnitInfoFieldSelected(ui)
	if f == nil {
		return
	}
	if f.Entry.Type == rgd.RgdTypeBool {
		if ui.UnitBoolClick.Clicked(gtx) {
			if err := ApplyUnitFieldText(ui, f, unitBoolText(f.Entry.BoolVal)); err != nil {
				ui.UnitValError = err.Error()
			} else {
				ui.UnitValError = ""
			}
		}
		return
	}
	UnitSyncValEditor(ui, gtx, f)
}

// layoutUnitFieldPanel is the right column of the info section: which field the
// list has selected, where it sits in the file, what type it is, and the editor
// that changes it.
func LayoutUnitFieldPanel(ui *uiState, gtx layout.Context, sec unitSection) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	body := func(gtx layout.Context) layout.Dimensions {
		if d == nil || d.Error != "" {
			return UnitEmptyState(ui, gtx)
		}
		f := UnitInfoFieldSelected(ui)
		if f == nil {
			return unitFieldNote(gtx, ui.Th, "Select a row in the info list to change it.")
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "Field", f.Label, false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "In file", rgdPathOf(d.File.Root, f.Entry), false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "Type", f.Entry.TypeLabel(), false)
				})
			}),
		}
		children = append(children, UnitFieldValueWidget(ui, gtx, f)...)
		children = append(children, UnitFieldButtons(ui, gtx)...)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, body)
			}),
		)
	})
}

// unitFieldValueWidget is the value control for the selected field: a toggle for
// a boolean, since a boolean has exactly two values and typing one of them is
// both slower and easier to get wrong, and a text editor for everything else.
// The error line is under either of them, because a value that would not parse
// has to be shown as refused rather than simply not applied.
func UnitFieldValueWidget(ui *uiState, gtx layout.Context, f *unitField) []layout.FlexChild {
	out := make([]layout.FlexChild, 0, 3)
	if f.Entry.Type == rgd.RgdTypeBool {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			value := "False"
			if f.Entry.BoolVal {
				value = "True"
			}
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ToggleButton(ui, gtx, &ui.UnitBoolClick, value, f.Entry.BoolVal, uikit.CSelectBg)
			})
		}))
	} else {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Value", &ui.UnitValEditor, false, ui.UnitValError)
		}))
	}
	if ui.UnitValError != "" {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, ui.UnitValError)
			})
		}))
	}
	return out
}

// unitFieldButtons is the row of buttons under the value: save, throw away, and
// the way out to the raw view for the fields this panel does not show.
func UnitFieldButtons(ui *uiState, gtx layout.Context) []layout.FlexChild {
	note := "Enter saves. The change is written into this mod, not the game."
	if ui.UnitInfoDirty {
		note = "Unsaved changes. " + note
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return PrimaryButton(ui, gtx, &ui.UnitSaveClick, "Save unit")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitRevertClick, "Revert", !ui.UnitInfoDirty)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitOpenRgdClick, "Open .rgd")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, note)
			})
		}),
	}
}

// layoutUnitInfoRow is one row of the info list: the section's heading where a
// section starts, and the value line itself.
func LayoutUnitInfoRow(ui *uiState, gtx layout.Context, index int, f *unitField) layout.Dimensions {
	body := func(gtx layout.Context) layout.Dimensions {
		if f == nil {
			return layout.Dimensions{}
		}
		value := func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return UnitKeyValueRowFor(ui, gtx, f.Label, f.Value, f.IsLocID, f.LocID, f.Entry)
			})
		}
		if !f.StartsGroup {
			return value(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), f.Group)
					l.Color = uikit.CTextDim
					l.Font.Weight = font.SemiBold
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(value),
		)
	}
	if !unitInfoRowSelectable(f) || index < 0 || index >= len(ui.UnitInfoClicks) {
		return body(gtx)
	}
	return UnitInfoRowFrame(ui, gtx, index, body)
}

// layoutUnitPickerRow places the race and unit strip above the three columns.
// The strip's height is fixed and the dropdowns are drawn over the columns, so
// opening one does not move the columns the user is reading.
func LayoutUnitPickerRow(ui *uiState, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return LayoutUnitPickerStrip(ui, gtx)
}

// unitObjectLabel is what the dialog calls the object it was opened for. It
// names the unit the pickers have selected, because that is what the columns
// below show; the unit's own display name is preferred, and its file name is
// the fallback, since that is the name the game itself uses.
func UnitObjectLabel(ui *uiState) string {
	if d := CurrentUnitDetails(ui); d != nil {
		if d.Name != "" {
			return d.Name
		}
		if d.Entry.Name != "" {
			return d.Entry.Name
		}
	}
	if u := CurrentUnit(ui); u != nil {
		return u.DisplayName()
	}
	return "No unit selected"
}

// layoutUnitPanelSlot places the tool panel left of the search field, with the
// same margins so both sit on one baseline in the menu bar.
func LayoutUnitPanelSlot(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unitPanelGapDp, Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitPanel(ui, gtx) })
}

// layoutUnitPanel draws the frame around the tool buttons. It measures its
// content first: the panel is a Rigid in the menu bar's flex row, so its
// constraints still hold all the free width of the bar, and a background sized
// from them would stretch from the buttons to the right edge.
func LayoutUnitPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitClicks(ui)
	buttons := func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range unitToolButtons {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return UnitToolButton(ui, gtx, &ui.UnitToolClicks[i], unitToolButtons[i].Label, UnitTabActive(ui))
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		})
	}
	return frameMeasured(gtx, buttons, uikit.CSaveBg, uikit.CSaveBd, unit.Dp(3))
}

// handleUnitToolInput opens the object interface from the menu bar. It runs on
// every frame, so a click here must not be swallowed by another branch. The
// interface is a tab now, so the button selects or opens that tab rather than
// raising a window.
func HandleUnitToolInput(ui *uiState, gtx layout.Context) {
	EnsureUnitClicks(ui)
	for i := range ui.UnitToolClicks {
		if !ui.UnitToolClicks[i].Clicked(gtx) {
			continue
		}
		switch unitToolButtons[i].Action {
		case "unit-interface":
			alreadyOpen := UnitTabActive(ui)
			OpenUnitTab(ui)
			ui.UnitSection = 0
			MarkStateDirty(ui)
			if alreadyOpen {
				// Pressing the button while the tab is on screen would
				// otherwise leave it looking like nothing happened.
				ui.StatusText = "Object interface is already open"
			} else {
				ui.StatusText = "Object interface: " + UnitObjectLabel(ui)
			}
		}
		return
	}
}

// handleUnitTabInput owns the object tab's own clicks: the pickers, the
// selectable rows of the populated columns, and the section list. It runs as
// part of the ordinary input path, so each part consumes only its own clicks.
func HandleUnitTabInput(ui *uiState, gtx layout.Context) {
	EnsureUnitClicks(ui)
	// The pickers come first: a click on an open dropdown row belongs to the
	// dropdown, not to the section list or the columns underneath it.
	HandleUnitPickerKeys(ui, gtx)
	HandleUnitPickerInput(ui, gtx)
	HandleUnitRowInput(ui, gtx)
	HandleUnitSquadInput(ui, gtx)
	for i := range ui.UnitSectionClicks {
		if i < len(unitSections) && ui.UnitSectionClicks[i].Clicked(gtx) {
			ui.UnitSection = i
			// The section the Object window was left on is part of the
			// saved session along with its pickers.
			MarkStateDirty(ui)
		}
	}
	// Last, because it acts on the section that was just chosen: a press that
	// left the info section must not be read as a press inside it.
	HandleUnitFieldInput(ui, gtx)
	// The mesh eye toggles belong to whichever section shows the mesh list —
	// Events and Classic OE both — so they are drained here rather than
	// inside the Events handler, which returns early for other sections.
	HandleUnitMeshToggles(ui, gtx)
	HandleUnitEventsInput(ui, gtx)
	HandleClassicOEWheInput(ui, gtx)
	HandleClassicOEClauseConditionInput(ui, gtx)
}

// layoutUnitBody splits the given space into the three columns. The widths are
// worked out in pixels rather than with flex weights, because the middle column
// is specified as a third of the space and the left one as a fixed menu width.
func LayoutUnitBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	// The columns themselves; the material dialog's dim and card wrap them
	// below, so a double press on a mesh row raises that editor over
	// whichever section is on show — Viewport and Classic OE both.
	columns := func(gtx layout.Context) layout.Dimensions {
		gap := gtx.Dp(unit.Dp(unitColGapDp))
		minW := gtx.Dp(unit.Dp(unitColMinWidthDp))
		// The card's own padding was already consumed by the caller's inset.
		// The caller's own padding was already consumed before this was called.
		leftW, midW, rightW := unitColumnWidths(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, minW)
		if CurrentUnitSection(ui).Label == unitSectionViewport {
			// The 3D view takes the middle and right columns together. A model in a
			// third of the window is a thumbnail, and the cameras the section lists
			// need a column of their own to be picked out of.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitViewportArea(ui, gtx) }),
			)
		}
		if CurrentUnitSection(ui).Label == unitSectionClassicOE {
			// The classic editor's layout takes the middle and right columns
			// together for the same reason the Viewport section does: its panel
			// column plus a model preview needs the room, and a properties panel
			// beside a preview would squeeze both.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOEArea(ui, gtx) }),
			)
		}
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(FixedPanel(ui, midW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitMiddleColumn(ui, gtx) })),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(FixedPanel(ui, rightW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitRightColumn(ui, gtx) })),
		)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(columns),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.UnitMeshMatOpen {
				return layout.Dimensions{}
			}
			return LayoutUnitMeshMatDialog(ui, gtx)
		}),
	)
}

func LayoutUnitMiddleColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitMiddleColumnBody(ui, gtx)
}

func LayoutUnitRightColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitRightColumnBody(ui, gtx)
}
