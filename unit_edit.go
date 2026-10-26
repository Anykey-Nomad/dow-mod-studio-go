package main

import (
	"dow-mod-studio/internal/rgd"
	"fmt"
	"path/filepath"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Changing a unit's attributes from the object window.
//
// The object window is where a modder looks at a unit, so it is where they
// should be able to change it. The raw RGD editor can already do the job, but
// it asks the reader to find health_ext inside a tree of tables and click
// through it, which is exactly the work the info column has already done: it
// knows that "Hit points" is health_ext.hitpoints. What it could not do was
// write anything back, because its rows were strings cut out of the parsed
// file with nothing pointing at the field they came from. So a row now carries
// that field, the right column binds one editor to the selected row, and
// saving marshals the parsed unit and writes it into the mod's own overlay.
//
// The overlay is the whole safety story. The unit being edited is often one the
// mod inherits from the base game, and its installed file lives under Program
// Files: writing it would change it for every mod on the machine. Writing the
// mod's own copy instead makes the change the mod's, which is the way a modder
// overrides a unit anyway, and leaves the installation alone.

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
func (ui *uiState) unitInfoFieldSelected() *unitField {
	d := ui.currentUnitDetails()
	if d == nil || len(d.Fields) == 0 {
		return nil
	}
	idx := clampInt(ui.unitInfoSel, 0, len(d.Fields)-1)
	f := d.Fields[idx]
	if !unitInfoRowSelectable(&f) {
		return nil
	}
	return &f
}

// unitInfoFieldAt is the same row by index, which is what laying the list out
// needs: a row that cannot be edited still has to be drawn.
func unitInfoFieldAt(d *unitDetails, index int) *unitField {
	if d == nil || index < 0 || index >= len(d.Fields) {
		return nil
	}
	f := d.Fields[index]
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
func (ui *uiState) applyUnitFieldText(f *unitField, text string) error {
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
	ui.unitInfoDirty = true
	d := ui.currentUnitDetails()
	if d == nil {
		return nil
	}
	d.RebuildFromFile()
	if i := unitFieldIndex(d.Fields, f.Entry); i >= 0 {
		ui.unitInfoSel = i
	}
	return nil
}

// saveUnitDetails writes the parsed unit back into the mod's own overlay, and
// says where it went. A unit that is not dirty is left alone rather than
// written out again: rewriting an unchanged file is what makes a tool's idea of
// "modified" and a modder's drift apart for no reason.
func (ui *uiState) saveUnitDetails() (string, error) {
	d := ui.currentUnitDetails()
	if d == nil || d.File == nil {
		return "", fmt.Errorf("no unit is open")
	}
	if !ui.unitInfoDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.unitValError != "" {
		return "", fmt.Errorf("correct %s first", ui.unitValError)
	}
	data, err := d.File.MarshalBinary()
	if err != nil {
		return "", err
	}
	if ui.modTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	disk, err := ui.modTree.WriteOverlayFile(d.Entry.Path, data)
	if err != nil {
		return "", err
	}
	ui.unitInfoDirty = false
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(d.Entry.Path), disk, len(data)), nil
}

// revertUnitDetails throws the changes away and reads the unit from the file
// again, which is the only way back once a value has been typed wrongly: undo
// per keystroke would need the history the editor does not keep.
func (ui *uiState) revertUnitDetails() {
	ui.unitDetails = nil
	ui.unitDetailsPath = ""
	ui.unitInfoDirty = false
	ui.unitValError = ""
	ui.unitValBound = ""
	ui.unitValEditor.SetText("")
	ui.unitInfoSel = 0
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
func (ui *uiState) markUnitFieldDirty(f *rgd.RgdEntry) {
	if f == nil {
		return
	}
	d := ui.currentUnitDetails()
	if d == nil || d.File == nil || rgdPathOf(d.File.Root, f) == "" {
		return
	}
	ui.unitInfoDirty = true
}

// rgdPathOf is where an entry sits in its file, as the dotted path the freeze
// record and the raw editor both speak. It is shown next to the editor because
// a modder who wants to check a value in the raw view needs to know where to
// look, and hunting for it again is the part they would rather not do.
func rgdPathOf(root *rgd.RgdEntry, target *rgd.RgdEntry) string {
	if root == nil || target == nil || target == root {
		return ""
	}
	for _, ch := range root.Children {
		if ch == nil {
			continue
		}
		if ch == target {
			return ch.Name
		}
		if p := rgdPathOf(ch, target); p != "" {
			return ch.Name + "." + p
		}
	}
	return ""
}

// ── the editor ──────────────────────────────────────────────────────────────

// unitBoolText is the text a boolean is flipped with, which is the same
// spelling the parser accepts so that the toggle and a typed value go through
// one door.
func unitBoolText(v bool) string {
	if v {
		return "false"
	}
	return "true"
}

// unitSyncValEditor reads the editor's events and applies what it says to the
// selected field. Enter saves rather than inserting a line break, so a value can
// be typed and committed without reaching for the button.
//
// The editor is bound to a field rather than to a row index, because the row a
// field sits at moves when the list is rebuilt and a bound-to-index editor
// would then write into whatever took its place.
func (ui *uiState) unitSyncValEditor(gtx layout.Context, f *unitField) {
	if f == nil || f.Entry == nil || !rgdIsEditableType(f.Entry.Type) || f.Entry.Type == rgd.RgdTypeBool {
		return
	}
	ed := &ui.unitValEditor
	fieldKey := unitFieldKey(*f)
	if ui.unitValBound != fieldKey {
		ed.SetText(f.Entry.DisplayValue())
		ui.unitValBound = fieldKey
		ui.unitValError = ""
	}
	ed.SingleLine = true
	ed.Submit = true
	// The editor is a one line field, so a press into it has to be given focus
	// or the reader would type into the window and wonder where it went.
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
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
	if err := ui.applyUnitFieldText(f, ed.Text()); err != nil {
		ui.unitValError = err.Error()
	} else {
		ui.unitValError = ""
	}
	if submit {
		ui.saveUnitFromPanel()
	}
}

// saveUnitFromPanel is the Save button's action: it writes the unit and puts
// what happened in the status line, which is where every other write in the app
// reports itself.
func (ui *uiState) saveUnitFromPanel() {
	if !ui.unitInfoDirty {
		ui.statusText = "This unit has no unsaved changes"
		return
	}
	line, err := ui.saveUnitDetails()
	if err != nil {
		ui.statusText = "Save failed: " + err.Error()
		return
	}
	ui.statusText = line
	ui.markStateDirty()
}

// handleUnitFieldInput owns the presses of the attribute editor: the rows of
// the info list that select a field, the boolean toggle, the editor's typing
// and the three buttons. It runs after the section list, so a press that
// changed section is already seen here.
func (ui *uiState) handleUnitFieldInput(gtx layout.Context) {
	if ui.currentUnitSection().label != "Unit info" {
		return
	}
	d := ui.currentUnitDetails()
	if d == nil {
		return
	}
	ui.ensureUnitInfoClicks(len(d.Fields))
	for i := range ui.unitInfoClicks {
		if i < len(ui.unitInfoClicks) && ui.unitInfoClicks[i].Clicked(gtx) {
			ui.unitInfoSel = i
		}
	}
	if ui.unitOpenRgdClick.Clicked(gtx) {
		ui.openRgd(d.Entry.Path)
		return
	}
	if ui.unitRevertClick.Clicked(gtx) {
		ui.revertUnitDetails()
		ui.statusText = "Changes to this unit thrown away"
		return
	}
	if ui.unitSaveClick.Clicked(gtx) {
		ui.saveUnitFromPanel()
		return
	}
	f := ui.unitInfoFieldSelected()
	if f == nil {
		return
	}
	if f.Entry.Type == rgd.RgdTypeBool {
		if ui.unitBoolClick.Clicked(gtx) {
			if err := ui.applyUnitFieldText(f, unitBoolText(f.Entry.BoolVal)); err != nil {
				ui.unitValError = err.Error()
			} else {
				ui.unitValError = ""
			}
		}
		return
	}
	ui.unitSyncValEditor(gtx, f)
}

func (ui *uiState) ensureUnitInfoClicks(n int) {
	if len(ui.unitInfoClicks) != n {
		ui.unitInfoClicks = make([]widget.Clickable, n)
	}
}

// ── the right column ────────────────────────────────────────────────────────

// layoutUnitFieldPanel is the right column of the info section: which field the
// list has selected, where it sits in the file, what type it is, and the editor
// that changes it.
func (ui *uiState) layoutUnitFieldPanel(gtx layout.Context, sec unitSection) layout.Dimensions {
	d := ui.currentUnitDetails()
	body := func(gtx layout.Context) layout.Dimensions {
		if d == nil || d.Error != "" {
			return ui.unitEmptyState(gtx)
		}
		f := ui.unitInfoFieldSelected()
		if f == nil {
			return unitFieldNote(gtx, ui.th, "Select a row in the info list to change it.")
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.unitKeyValueRow(gtx, "Field", f.Label, false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.unitKeyValueRow(gtx, "In file", rgdPathOf(d.File.Root, f.Entry), false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.unitKeyValueRow(gtx, "Type", f.Entry.TypeLabel(), false)
				})
			}),
		}
		children = append(children, ui.unitFieldValueWidget(gtx, f)...)
		children = append(children, ui.unitFieldButtons(gtx)...)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
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
func (ui *uiState) unitFieldValueWidget(gtx layout.Context, f *unitField) []layout.FlexChild {
	out := make([]layout.FlexChild, 0, 3)
	if f.Entry.Type == rgd.RgdTypeBool {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			value := "False"
			if f.Entry.BoolVal {
				value = "True"
			}
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.toggleButton(gtx, &ui.unitBoolClick, value, f.Entry.BoolVal, cSelectBg)
			})
		}))
	} else {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowEditor(gtx, "Value", &ui.unitValEditor, false, ui.unitValError)
		}))
	}
	if ui.unitValError != "" {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.th, ui.unitValError)
			})
		}))
	}
	return out
}

// unitFieldButtons is the row of buttons under the value: save, throw away, and
// the way out to the raw view for the fields this panel does not show.
func (ui *uiState) unitFieldButtons(gtx layout.Context) []layout.FlexChild {
	note := "Enter saves. The change is written into this mod, not the game."
	if ui.unitInfoDirty {
		note = "Unsaved changes. " + note
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.primaryButton(gtx, &ui.unitSaveClick, "Save unit")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitRevertClick, "Revert", !ui.unitInfoDirty)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &ui.unitOpenRgdClick, "Open .rgd")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.th, note)
			})
		}),
	}
}

// unitFieldNote is the small dimmed line the panel uses to explain itself. It
// is a function rather than a label so the two places that print one — the
// "select a row" case and the buttons — read the same.
func unitFieldNote(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(9), text)
		l.Color = cTextDim
		l.MaxLines = 6
		return l.Layout(gtx)
	})
}

// ── the rows of the info list ───────────────────────────────────────────────

// unitInfoRowSelectable reports whether a row is one the panel can change. A
// localised row keeps its own presses — they open the localisation dialog — and
// a counted row is a statement about a list rather than a field, so neither is
// given a selection click that would sit on top of the other one.
func unitInfoRowSelectable(f *unitField) bool {
	return f != nil && f.Entry != nil && !f.IsLocID
}

// layoutUnitInfoRow is one row of the info list: the section's heading where a
// section starts, and the value line itself.
func (ui *uiState) layoutUnitInfoRow(gtx layout.Context, index int, f *unitField) layout.Dimensions {
	body := func(gtx layout.Context) layout.Dimensions {
		if f == nil {
			return layout.Dimensions{}
		}
		value := func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.unitKeyValueRowFor(gtx, f.Label, f.Value, f.IsLocID, f.LocID, f.Entry)
			})
		}
		if !f.StartsGroup {
			return value(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(11), f.Group)
					l.Color = cTextDim
					l.Font.Weight = font.SemiBold
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(value),
		)
	}
	if !unitInfoRowSelectable(f) || index < 0 || index >= len(ui.unitInfoClicks) {
		return body(gtx)
	}
	return ui.unitInfoRowFrame(gtx, index, body)
}

// unitInfoRowFrame is the selection and hover of a row, under the row itself.
// It is the same shape the selectable rows of the weapon lists use, so a row
// looks selected or not in the same way wherever it is.
func (ui *uiState) unitInfoRowFrame(gtx layout.Context, index int, body layout.Widget) layout.Dimensions {
	bg := cPanelBg
	switch {
	case index == ui.unitInfoSel:
		bg = cSelectBg
	case ui.unitInfoClicks[index].Hovered():
		bg = cMenuHover
	}
	return ui.unitInfoClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, body)
			}),
		)
	})
}
