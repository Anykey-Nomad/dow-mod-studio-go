package main

import (
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// The freeze buttons, next to the Unit field.
//
// They act on whatever the Unit field is showing, which is the one place the
// object window names the object every other part of it is about. Which of the
// two is offered depends on the object: an object that is in the mod can be
// frozen, and one that is not is already frozen, so the button says so rather
// than refusing when it is pressed.

// layoutFreezeButtons draws the freeze or unfreeze button for the selected
// object. A nil selection draws nothing, because there is nothing to act on.
func (ui *uiState) layoutFreezeButtons(gtx layout.Context, sel *unitEntry) layout.Dimensions {
	if sel == nil || ui.modTree == nil {
		return layout.Dimensions{}
	}
	if ui.isFrozen(sel.Path) {
		return ui.smallButtonDisabled(gtx, &ui.unitUnfreezeTrigger, "Unfreeze…", false)
	}
	// Only a file the mod itself owns can be moved out of it, so a unit that
	// comes from the base game gets no button rather than one that fails.
	return ui.smallButtonDisabled(gtx, &ui.unitFreezeTrigger, "Freeze…", !ui.canFreeze(sel.Path))
}

// isFrozen reports whether an object has been moved out of the mod.
func (ui *uiState) isFrozen(path string) bool {
	if ui == nil || ui.modTree == nil {
		return false
	}
	return ui.freezeBookFor().isFrozen(path)
}

// canFreeze reports whether an object is one this mod owns, which is the only
// kind that can be moved into the project's folder. It is asked on the frame
// the strip is drawn, so it reads the record rather than caching it: the record
// changes when a freeze happens, and a stale answer would offer a button that
// does the wrong thing.
func (ui *uiState) canFreeze(path string) bool {
	if ui == nil || ui.modTree == nil {
		return false
	}
	_, err := ui.managedFileDiskPath(path)
	return err == nil
}

// frozenRows is the list of what has been moved out of the mod, read from the
// record rather than from the mod: a frozen unit is not in the mod, so this is
// the only place it is.
func (ui *uiState) frozenRows() []unitWeaponRow {
	if ui == nil || ui.modTree == nil {
		return nil
	}
	book := ui.freezeBookFor()
	out := make([]unitWeaponRow, 0, len(book.Frozen))
	for _, r := range book.Frozen {
		out = append(out, unitWeaponRow{
			hardpoint: r.Kind,
			name:      r.Name,
			ref:       r.Path,
		})
	}
	return out
}

// layoutFrozenList is the "Frozen" column. Each row is one object that has been
// moved out of the mod, and pressing it puts it back — the list is the way back,
// because the object itself is no longer anywhere else.
func (ui *uiState) layoutFrozenList(gtx layout.Context) layout.Dimensions {
	rows := ui.frozenRows()
	if len(rows) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "Nothing has been frozen. Freezing a unit moves it out of the mod and clears what pointed at it; unfreezing puts both back.")
				l.Color = cTextDim
				l.MaxLines = 6
				return l.Layout(gtx)
			})
		})
	}
	ui.ensureUnitRowClicks(len(rows))
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range rows {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutUnitSelectableRow(gtx, i, len(rows), rows[i].hardpoint, rows[i].name, "put back", false)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// handleFrozenRowInput puts a frozen object back when its row is pressed.
//
// It is a single click rather than a button on each row because the rows are
// the only thing in the column, and because the operation is the same for all
// of them: the row says which object, and pressing it undoes that one freeze.
func (ui *uiState) handleFrozenRowInput(gtx layout.Context) {
	rows := ui.frozenRows()
	ui.ensureUnitRowClicks(len(rows))
	for i := range rows {
		if i < len(ui.unitRowClicks) && ui.unitRowClicks[i].Clicked(gtx) {
			ui.unitSelRow = i
			record, err := ui.unfreezeEntity(rows[i].ref)
			if err != nil {
				ui.statusText = "Could not put " + rows[i].name + " back: " + err.Error()
			} else {
				ui.statusText = freezeStatusLine("Put back into the mod", record)
			}
			ui.markStateDirty()
			return
		}
	}
}

// handleFreezeInput acts on the freeze buttons. Both are one-way operations
// that rewrite other files, so neither of them is a toggle: the button says
// which one it is, and the status line says what happened.
func (ui *uiState) handleFreezeInput(gtx layout.Context) {
	sel := ui.currentUnit()
	if sel == nil {
		return
	}
	if ui.unitFreezeTrigger.Clicked(gtx) {
		record, err := ui.freezeEntity(sel.Path)
		if err != nil {
			ui.statusText = "Could not freeze " + sel.Name + ": " + err.Error()
			ui.markStateDirty()
			return
		}
		ui.statusText = freezeStatusLine("Moved out of the mod", record)
		// The object is no longer in the mod, so the section that lists what
		// was frozen is where the user is sent to undo it.
		ui.unitSection = unitSectionNamed("Frozen")
		ui.markStateDirty()
		return
	}
	if ui.unitUnfreezeTrigger.Clicked(gtx) {
		record, err := ui.unfreezeEntity(sel.Path)
		if err != nil {
			ui.statusText = "Could not unfreeze " + sel.Name + ": " + err.Error()
			ui.markStateDirty()
			return
		}
		ui.statusText = freezeStatusLine("Put back into the mod", record)
		ui.markStateDirty()
	}
}

// freezeStatusLine says what a freeze or an unfreeze did, in the terms the
// operation uses: the files that moved, and the references that were cleared
// or put back. It is the only account of the change that survives the object
// leaving the mod, so it says what happened rather than that something did.
func freezeStatusLine(what string, record *freezeRecord) string {
	if record == nil {
		return what
	}
	files := itoa(len(record.Files))
	if len(record.Files) == 1 {
		files = "1 file"
	} else {
		files += " files"
	}
	mentions := itoa(len(record.Mentions))
	switch {
	case len(record.Mentions) == 0:
		return record.Name + ": " + what + " (" + files + ", nothing referred to it)"
	case len(record.Mentions) == 1:
		return record.Name + ": " + what + " (" + files + ", 1 reference cleared)"
	}
	return record.Name + ": " + what + " (" + files + ", " + mentions + " references cleared)"
}
