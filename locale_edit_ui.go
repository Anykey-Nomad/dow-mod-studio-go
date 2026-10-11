package main

import (
	"dow-mod-studio/internal/rgd"
	"fmt"
	"path/filepath"
	"strings"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// The dialog behind Alt+Ctrl+click on a localised row, and behind a plain click
// on one that has no text yet.
//
// A change never goes into the game's own .ucs. Those files are the game's data
// to overwrite, not ours: they live under Program Files, they are shared by
// every mod on the machine, and a default install will not let us write them at
// all. So a change is an override in the open mod's own Locale folder, which is
// read before the engine's — the first definition of an id wins, which is
// exactly how a modder's own table overrides a string.

// localeRowAction is what a press on a localised row means.
type localeRowAction int

const (
	// localeRowNothing is a press that is not asking for anything: a plain
	// click on a row that already has its text, which is a selection.
	localeRowNothing localeRowAction = iota
	// localeRowEdit is Alt+Ctrl on a row with text: change what it says.
	localeRowEdit
	// localeRowCreate is a press on a row with no text: write some, under an id
	// of its own.
	localeRowCreate
	// localeRowJump is Alt on a row with text: open the .ucs it came from with
	// the caret on the line. It is what Alt+click already did in the file view.
	localeRowJump
)

// localeFieldForID is the rgd.RgdEntry behind a localisation row, found by the id
// the row shows. It is what the dialog changes when it has to give a field a
// new id, and a nil result is not a failure: a row with no entry behind it — a
// weapon's settings, say — can still have its text written, it just cannot be
// repointed.
func localeFieldForID(d *unitDetails, id string) *rgd.RgdEntry {
	if d == nil || id == "" {
		return nil
	}
	for _, f := range d.fields {
		if f.isLocID && f.locID == id && f.entry != nil {
			return f.entry
		}
	}
	return nil
}

// localeRowActionFor reads a press on a localised row.
//
// The rule is the one the object window asks for and it is a function of three
// booleans rather than something buried in a layout: a row with no text takes a
// plain click, because there is nothing to select and a string to write, and a
// row that has text takes Alt+Ctrl, so that a plain click on it stays a
// selection and the editing gesture is always deliberate.
func localeRowActionFor(hasText, alt, ctrl bool) localeRowAction {
	if !hasText {
		return localeRowCreate
	}
	switch {
	case alt && ctrl:
		return localeRowEdit
	case alt:
		return localeRowJump
	}
	return localeRowNothing
}

// localeEditRequest is what a press on a row asked for. It is passed by value
// because a press is noticed during layout and acted on in the input pass, one
// frame later, and a request that can be dropped in between would be a press
// that silently did nothing.
type localeEditRequest struct {
	id     string
	action localeRowAction
}

// ── the request ─────────────────────────────────────────────────────────────

// openLocaleEdit puts the dialog up for an id. create asks for a new id to be
// allocated on save, which is what a field with no string of its own needs; it
// is only meaningful when the field's id is the game's empty marker.
func (ui *uiState) openLocaleEdit(id string, action localeRowAction, field *rgd.RgdEntry, key string) {
	if action != localeRowEdit && action != localeRowCreate {
		return
	}
	text := localisedString(id)
	if action == localeRowEdit && text == "" {
		// Asked to edit something that is not there, which is a create.
		action = localeRowCreate
	}
	ui.localeEditDialog = true
	ui.localeEditID = id
	ui.localeEditCreate = action == localeRowCreate
	ui.localeEditField = field
	ui.localeEditFieldKey = key
	ui.localeEditError = ""
	ui.localeEditEditor.SetText(text)
}

// closeLocaleEdit puts the dialog away.
func (ui *uiState) closeLocaleEdit() {
	ui.localeEditDialog = false
	ui.localeEditID = ""
	ui.localeEditCreate = false
	ui.localeEditField = nil
	ui.localeEditFieldKey = ""
	ui.localeEditError = ""
	ui.localeEditEditor.SetText("")
}

// saveLocaleEdit writes the dialog's text and reloads the table, so the change
// is on screen before the dialog closes rather than after the next module load.
//
// A create also points the field at the new id. Without that the string would
// be written and never read: the field would still hold $0, which is how the
// game spells "no string", and the unit would stay untranslated in the game
// while the app showed it translated.
func (ui *uiState) saveLocaleEdit() (string, error) {
	text := strings.TrimSpace(ui.localeEditEditor.Text())
	if text == "" {
		return "", fmt.Errorf("the text is empty — a localisation with no text shows as a blank in game")
	}
	id := ui.localeEditID
	if ui.localeEditCreate {
		free := allocateLocaleID()
		if free == "" {
			return "", fmt.Errorf("no free localisation id could be found")
		}
		id = free
	}
	path, line, err := writeLocaleEntry(ui.projectDir, ui.localeLabel(), id, text)
	if err != nil {
		return "", err
	}
	// The table is cached per language, so without this the string that was
	// just written is missing from the copy every view reads and the edit looks
	// like it did nothing.
	invalidateLocaleCache()
	ui.applyLocale()

	if ui.localeEditCreate && ui.localeEditField != nil {
		// The field is what makes the new string reachable: until it holds the
		// id, the game still reads $0 and shows nothing. The parsed unit is
		// rebuilt from it so the panel shows the new text straight away.
		ui.localeEditField.WStrVal = "$" + id
		if d := ui.currentUnitDetails(); d != nil {
			d.rebuildFromFile()
		}
		ui.markRgdFieldDirty(ui.localeEditFieldKey)
		// The same change, seen from the object window: the field that names
		// the new string is part of a unit nobody has written back yet, and
		// the string is worth nothing to the game until they are.
		ui.markUnitFieldDirty(ui.localeEditField)
	}
	return fmt.Sprintf("Saved $%s to %s line %d", id, filepath.Base(path), line), nil
}

// markRgdFieldDirty tells an open file view that its data has been changed and
// needs writing back. It is only that: the object window keeps the unit parsed
// in memory, so pointing one of its fields at a new id is a change to the
// parsed copy, and whether it reaches the .rgd on disk is the writer's business
// — the status line says which id was allocated so nothing about it is silent.
func (ui *uiState) markRgdFieldDirty(key string) {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return
	}
	t.rgdDirty = true
}

// ── the dialog ──────────────────────────────────────────────────────────────

func (ui *uiState) handleLocaleEditInput(gtx layout.Context) {
	if !ui.localeEditDialog {
		return
	}
	// Escape has to be taken before the editor's own Update, or the editor uses
	// it to drop a text selection instead of closing the dialog.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		ui.closeLocaleEdit()
		return
	}
	for {
		ev, ok := ui.localeEditEditor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			ui.saveLocaleEditAndClose()
			return
		}
	}
	if ui.localeEditCancel.Clicked(gtx) {
		ui.closeLocaleEdit()
		return
	}
	if ui.localeEditSave.Clicked(gtx) {
		ui.saveLocaleEditAndClose()
	}
}

func (ui *uiState) saveLocaleEditAndClose() {
	msg, err := ui.saveLocaleEdit()
	if err != nil {
		ui.localeEditError = err.Error()
		return
	}
	ui.closeLocaleEdit()
	ui.statusText = msg
}

// layoutLocaleEditDialog draws the editor over the window, the way the other
// dialogs do: a dimmed field behind, a card in the middle.
func (ui *uiState) layoutLocaleEditDialog(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 110)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(460))
				cardH := gtx.Dp(unit.Dp(240))
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = cardW, cardW
				gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = cardH, cardH
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(18), Right: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return ui.localeEditCaption(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return widgetBorderTextEditor(gtx, ui.th, &ui.localeEditEditor, false, "")
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return ui.localeEditNote(gtx)
									}),
									layout.Flexed(1, flexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return ui.localeEditButtons(gtx)
									}),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

// localeEditCaption says which string is being changed, and whether it is a
// new one. An empty id is what a create starts from, because the field holds
// the game's "$0" marker and there is no id to name yet.
func (ui *uiState) localeEditCaption(gtx layout.Context) layout.Dimensions {
	title := "Localisation text"
	if ui.localeEditCreate {
		title = "New localisation"
	}
	rows := []struct{ label, value string }{
		{"String", title},
		{"Language", ui.localeLabel()},
	}
	if id := strings.TrimSpace(ui.localeEditID); id != "" && id != "0" {
		rows = append(rows, struct{ label, value string }{"Id", "$" + id})
	}
	children := make([]layout.FlexChild, 0, len(rows))
	for _, r := range rows {
		r := r
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(11), r.label)
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(11), r.value)
					l.Color = cText
					l.Font.Weight = font.Medium
					return l.Layout(gtx)
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// localeEditNote says where the text is going, and says it plainly: the mod's
// own folder, not the game's file. A translation that silently did not reach
// the game is worse than one that was never written.
func (ui *uiState) localeEditNote(gtx layout.Context) layout.Dimensions {
	where := ui.projectDir
	if strings.TrimSpace(where) == "" {
		return layout.Dimensions{}
	}
	lang := ui.localeLabel()
	note := fmt.Sprintf("Saved to the mod's own file: %s", filepath.Join(where, localeDirName, lang, localeEditFileName))
	if ui.localeEditCreate {
		note += "\nA free id will be allocated and written into the unit's field."
	}
	l := material.Label(ui.th, unit.Sp(10), note)
	l.Color = cTextDim
	return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, l.Layout)
}

func (ui *uiState) localeEditButtons(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.localeEditError == "" {
				return layout.Dimensions{}
			}
			l := material.Label(ui.th, unit.Sp(10), ui.localeEditError)
			l.Color = rgb(0xE8A33D)
			l.MaxLines = 2
			return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, l.Layout)
		}),
		layout.Flexed(1, flexGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButton(gtx, &ui.localeEditCancel, "Cancel")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButton(gtx, &ui.localeEditSave, "Save")
		}),
	)
}

// ── the row ─────────────────────────────────────────────────────────────────

// localeEditTag is the pointer target of one localised row. The rows are drawn
// inside lists that scroll and are rebuilt every frame, so the target is keyed
// by the field it belongs to rather than held per widget.
func (ui *uiState) localeEditTag(key string) event.Tag {
	if key == "" {
		return nil
	}
	if ui.localeEditTags == nil {
		ui.localeEditTags = make(map[string]event.Tag)
	}
	tag, ok := ui.localeEditTags[key]
	if !ok {
		tag = newPointerTag(key)
		ui.localeEditTags[key] = tag
	}
	return tag
}

// pollLocaleEditPress reads a press on a localised row and turns it into a
// request for the input pass. The press is caught here rather than on a
// clickable because only the raw event carries the modifiers, and the request
// is left on the state because opening a dialog is not a layout's business.
func (ui *uiState) pollLocaleEditPress(gtx layout.Context, tag event.Tag, id string) {
	if tag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || e.Kind != pointer.Press {
			continue
		}
		action := localeRowActionFor(localisedString(id) != "", e.Modifiers.Contain(key.ModAlt), e.Modifiers.Contain(key.ModCtrl))
		if action == localeRowNothing {
			continue
		}
		ui.localePendingRequest = localeEditRequest{id: id, action: action}
	}
}

// localeEditHover registers the hover target that puts the id in the tooltip.
// The tooltip is the shared one the file view already uses, so a localised row
// says the same thing wherever it is drawn.
func (ui *uiState) localeEditHover(gtx layout.Context, tag event.Tag, tooltip string) {
	if tag == nil || tooltip == "" {
		return
	}
	pointer.CursorPointer.Add(gtx.Ops)
	event.Op(gtx.Ops, tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			ui.rgdTooltipText = tooltip
		case pointer.Leave:
			if ui.rgdTooltipText == tooltip {
				ui.rgdTooltipText = ""
			}
		}
	}
}

// localeRowTooltip is what a localised row says on hover: the id, which is the
// one thing the row itself cannot show, and where the text came from so a
// reader can tell the engine's string from the mod's override of it.
func (ui *uiState) localeRowTooltip(id string) string {
	if strings.TrimSpace(id) == "" || id == "0" {
		return "This field has no localisation id — click to give it one"
	}
	lines := []string{"$" + id}
	if entry, ok := localeLookup(id); ok {
		lines = append(lines, filepath.Base(entry.File)+fmt.Sprintf(" line %d", entry.Line))
		if localeOverrideIsOurs(entry) {
			lines = append(lines, "from this mod's own file")
		}
	} else {
		lines = append(lines, fmt.Sprintf("not in the %s string table", ui.localeLabel()))
	}
	lines = append(lines, "", "Alt+Ctrl+click to change the text")
	return strings.Join(lines, "\n")
}

// localeRowText is what a localised row shows: the text, or why there is none.
// The id is deliberately not in it — the hover tooltip carries that, and a row
// that led with "$705754" would be unreadable.
func (ui *uiState) localeRowText(id string) string {
	if strings.TrimSpace(id) == "" || id == "0" {
		return "— (no localisation — click to add one)"
	}
	if text := localisedString(id); text != "" {
		return text
	}
	return fmt.Sprintf("not in the %s string table — click to add one", ui.localeLabel())
}

// localeEditableRow draws a localised field as a line of the object window: the
// text it resolves to, the id on hover, and the presses that edit it.
func (ui *uiState) localeEditableRow(gtx layout.Context, label, id string, field *rgd.RgdEntry, key string) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return ui.localeRowBody(gtx, label, id, 22, unit.Sp(10), 64)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			ui.localeRowAffordances(gtx, id, key)
			return layout.Dimensions{}
		}),
	)
}

// localePanelRow is the same row in the right hand panel's two column shape.
func (ui *uiState) localePanelRow(gtx layout.Context, label, id string, field *rgd.RgdEntry) layout.Dimensions {
	key := localeRowKey(field, label)
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return ui.localeRowBody(gtx, label, id, 24, unit.Sp(11), 40)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			ui.localeRowAffordances(gtx, id, key)
			return layout.Dimensions{}
		}),
	)
}

// localeRowBody is the visible part of a localised row: its label and what it
// says. It is apart from the pointer handling so the panel and the list can
// share the text and differ only in size.
func (ui *uiState) localeRowBody(gtx layout.Context, label, id string, rowDp unit.Dp, size unit.Sp, maxText int) layout.Dimensions {
	text := ui.localeRowText(id)
	if maxText > 3 && len(text) > maxText {
		text = text[:maxText-3] + "…"
	}
	rowH := gtx.Dp(rowDp)
	gtx.Constraints.Min.Y = rowH
	gtx.Constraints.Max.Y = rowH
	hasText := localisedString(id) != ""
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, size, label)
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, size, text)
			// A string is text; a missing one names something that is not
			// there, and reads as a reference rather than as a value.
			if hasText {
				l.Color = cText
			} else {
				l.Color = cTextDim
			}
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// localeRowAffordances puts the hover tooltip and the press handling on a
// localised row. Both live in one Expanded layer over the row's own area, which
// is what lets a press carry its modifiers: only a raw pointer event has them.
func (ui *uiState) localeRowAffordances(gtx layout.Context, id, key string) {
	tag := ui.localeEditTag(key)
	if tag == nil {
		return
	}
	ui.localeEditHover(gtx, tag, ui.localeRowTooltip(id))
	ui.pollLocaleEditPress(gtx, tag, id)
}
