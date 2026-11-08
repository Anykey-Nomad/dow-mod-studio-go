package app

import (
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// The Weapons section reads a unit's weapons as a tree: the hardpoint's weapon,
// then the damage it does against each armour type. A weapon's own .rgd is where
// those numbers live — the unit's hardpoint only says which weapon is mounted —
// so the tree keeps the parsed weapon file with the row and writes an edit back
// to that file rather than to the unit.

// unitWeaponTreeRow is one line of the tree: a weapon, or one of its armour
// rows. Kind says which, and the pair (Weapon, Armour) addresses it in the
// flattened lists the model returns.
type unitWeaponTreeRow struct {
	Weapon int
	// Armour is the index of the armour row inside the weapon's damage list,
	// -1 on the weapon's own row.
	Armour int
	Slot   *unitdef.WeaponSlot
	Damage unitdef.WeaponArmour
	Field  unitdef.WeaponDamageField
	// Kind is 0 for a weapon, 1 for one of its damage scalars and 2 for a value
	// against one armour type.
	Kind int
}

// unitWeaponOpen reports whether a weapon's damage is shown. New weapons open:
// a tree that starts closed says nothing about what it holds.
func unitWeaponOpen(ui *uiState, weapon int) bool {
	if weapon < 0 || weapon >= len(ui.UnitWeaponOpen) {
		return true
	}
	return ui.UnitWeaponOpen[weapon]
}

// EnsureUnitWeaponRows sizes the tree's own state to the unit on screen. The
// rows are rebuilt from the details every frame, so only the open flags and the
// click targets are kept, and both are resized here rather than in a layout that
// might not run.
func EnsureUnitWeaponRows(ui *uiState) int {
	d := CurrentUnitDetails(ui)
	n := 0
	if d != nil {
		n = d.WeaponCount()
	}
	if len(ui.UnitWeaponOpen) != n {
		open := make([]bool, n)
		for i := range open {
			open[i] = true
		}
		ui.UnitWeaponOpen = open
	}
	return n
}

// unitWeaponTreeRows flattens the unit's weapons into the lines the column
// draws: each weapon, and under it the damage it does to every armour type.
func unitWeaponTreeRows(ui *uiState) []unitWeaponTreeRow {
	d := CurrentUnitDetails(ui)
	if d == nil {
		return nil
	}
	EnsureUnitWeaponRows(ui)
	var out []unitWeaponTreeRow
	for i := 0; i < d.WeaponCount(); i++ {
		slot := unitWeaponAt(d, i)
		if slot == nil {
			continue
		}
		out = append(out, unitWeaponTreeRow{Weapon: i, Armour: -1, Slot: slot})
		if !unitWeaponOpen(ui, i) {
			continue
		}
		for _, f := range unitdef.WeaponDamageFields(slot.File) {
			out = append(out, unitWeaponTreeRow{Weapon: i, Armour: -1, Slot: slot, Field: f, Kind: 1})
		}
		for ai, a := range unitdef.WeaponDamages(slot.File) {
			out = append(out, unitWeaponTreeRow{Weapon: i, Armour: ai, Slot: slot, Damage: a, Kind: 2})
		}
	}
	return out
}

// LayoutUnitWeaponTree is the "Weapons" column: one line per weapon, and under
// each the damage it does against each armour type.
func LayoutUnitWeaponTree(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" || d.WeaponCount() == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	rows := unitWeaponTreeRows(ui)
	if len(ui.UnitWeaponRowClicks) != len(rows) {
		ui.UnitWeaponRowClicks = make([]widget.Clickable, len(rows))
	}
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		warnings := unitWeaponModelWarnings(ui, d)
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(3), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d weapon(s) · damage by armour type", d.WeaponCount()))
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		}
		if len(warnings) > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return unitWeaponWarningStrip(ui, gtx, warnings)
			}))
		}
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			ui.UnitWeaponList.Axis = layout.Vertical
			return ui.UnitWeaponList.Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
				return unitWeaponTreeRowLayout(ui, gtx, i, rows[i])
			})
		}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// unitWeaponModelWarnings refreshes the diagnostics for the open unit's model,
// once per unit. A weapon choice switches a visibility group of the unit's model
// on, so a mesh in that group whose material the mod cannot supply is exactly
// what turns the whole vehicle into the game's placeholder cube once the weapon
// is mounted — and nothing in the .rgd says so.
func unitWeaponModelWarnings(ui *uiState, d *unitdef.UnitDetails) []string {
	if d == nil || d.Tree == nil || d.File == nil {
		return nil
	}
	key := d.Entry.Path
	if ui.UnitWeaponModelKey == key {
		return ui.UnitWeaponModelWarn
	}
	ui.UnitWeaponModelKey = key
	var out []string
	for _, w := range unitdef.UnitWeaponModelWarnings(d.Tree, d.File) {
		groups := strings.Join(w.Groups, ", ")
		if groups == "" {
			groups = "no visibility group"
		}
		out = append(out, fmt.Sprintf("%s: material not found (%s) — shown by %s", w.Mesh, w.Material, groups))
		if len(out) >= 6 {
			break
		}
	}
	ui.UnitWeaponModelWarn = out
	return out
}

// unitWeaponWarningStrip draws those diagnostics above the tree: what a weapon
// choice would show in game, said here before the game says it with a cube.
func unitWeaponWarningStrip(ui *uiState, gtx layout.Context, warnings []string) layout.Dimensions {
	head := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("Model: %d mesh(es) with a missing material", len(warnings)))
	head.Color = uikit.CText
	head.MaxLines = 1
	lines := []layout.FlexChild{layout.Rigid(func(gtx layout.Context) layout.Dimensions { return head.Layout(gtx) })}
	for i := range warnings {
		text := unitVoiceClip(warnings[i], 58)
		l := material.Label(ui.Th, unit.Sp(9), text)
		l.Color = uikit.CTextDim
		l.MaxLines = 1
		lines = append(lines, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return l.Layout(gtx) }))
	}
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, lines...)
		})
}

// unitWeaponTreeRowLayout draws one line: a weapon with its open marker and
// hardpoint, or an armour row indented under it with its value on the right.
func unitWeaponTreeRowLayout(ui *uiState, gtx layout.Context, i int, r unitWeaponTreeRow) layout.Dimensions {
	sel := i == ui.UnitWeaponSel
	bg := uikit.CPanelBg
	switch {
	case sel:
		bg = uikit.CSelectBg
	case i < len(ui.UnitWeaponRowClicks) && ui.UnitWeaponRowClicks[i].Hovered():
		bg = uikit.CMenuHover
	}
	click := &ui.UnitWeaponRowClicks[i]
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if r.Kind != 0 {
						label, value := r.Damage.Armour, r.Damage.Value
						if r.Kind == 1 {
							label, value = r.Field.Label, r.Field.Value
						}
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(layout.Spacer{Width: unit.Dp(20)}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(9), unitVoiceClip(label, 28))
								l.Color = uikit.CTextDim
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(10), value)
								l.Color = uikit.CText
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
						)
					}
					mark := "▾ "
					if !unitWeaponOpen(ui, r.Weapon) {
						mark = "▸ "
					}
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(11), mark+unitVoiceClip(r.Slot.Name, 24))
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), strings.TrimPrefix(r.Slot.Slot, "weapon_"))
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

// SelectedUnitWeaponRow is the tree row a press selected, if the selection is
// still inside the tree that was drawn.
func SelectedUnitWeaponRow(ui *uiState) (unitWeaponTreeRow, bool) {
	rows := unitWeaponTreeRows(ui)
	if len(rows) == 0 {
		return unitWeaponTreeRow{}, false
	}
	idx := clampInt(ui.UnitWeaponSel, 0, len(rows)-1)
	return rows[idx], true
}

// SelectedUnitWeaponEdit is the damage row the panel edits: the amount itself,
// or the value against one armour type. The label says which, so the editor's
// heading reads as what is being changed.
func SelectedUnitWeaponEdit(ui *uiState) (unitWeaponTreeRow, string, bool) {
	r, ok := SelectedUnitWeaponRow(ui)
	if !ok {
		return unitWeaponTreeRow{}, "", false
	}
	switch r.Kind {
	case 1:
		if r.Field.Entry == nil {
			return unitWeaponTreeRow{}, "", false
		}
		return r, "Damage · " + r.Field.Label, true
	case 2:
		if r.Damage.Entry == nil {
			return unitWeaponTreeRow{}, "", false
		}
		return r, "Damage vs " + r.Damage.Armour, true
	}
	return unitWeaponTreeRow{}, "", false
}

// LayoutUnitWeaponDetail is the right column of the Weapons section: the weapon
// a row stands for, its damage against the armour rows under it, and the field
// that edits the selected one.
func LayoutUnitWeaponDetail(ui *uiState, gtx layout.Context) layout.Dimensions {
	sec := CurrentUnitSection(ui)
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" || d.WeaponCount() == 0 {
		return UnitColumn(ui, gtx, sec.RightTitle, "Select a unit to see its weapons.")
	}
	rows := unitWeaponTreeRows(ui)
	if len(rows) == 0 {
		return UnitColumn(ui, gtx, sec.RightTitle, "No weapon is selected.")
	}
	idx := clampInt(ui.UnitWeaponSel, 0, len(rows)-1)
	r := rows[idx]
	weapon := r.Slot
	damage := unitdef.WeaponDamages(weapon.File)
	mark := "▾ "
	if !unitWeaponOpen(ui, r.Weapon) {
		mark = "▸ "
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(12), mark+weapon.Name)
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							text := weapon.Path
							if text == "" {
								text = "the weapon file was not found in the mod"
							}
							l := material.Label(ui.Th, unit.Sp(9), unitVoiceClip(text, 46))
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return unitWeaponArmourEditor(ui, gtx, r, damage) }),
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return uikit.SmallButton(ui, gtx, &ui.UnitWeaponAddClick, "Add weapon…")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							// Removing takes the weapon out of its slot, which is
							// what a hardpoint with no weapon is: the slot stays,
							// the reference goes.
							return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitWeaponDelClick, "Remove", r.Kind != 0)
						}),
					)
				})
			}),
		)
	})
}

// unitWeaponArmourEditor is the armour row's value: a heading naming what is
// being edited, the field, and the two buttons that write it to the weapon file
// or put the value back.
func unitWeaponArmourEditor(ui *uiState, gtx layout.Context, r unitWeaponTreeRow, damage []unitdef.WeaponArmour) layout.Dimensions {
	if r.Kind == 0 {
		// A weapon row shows the weapon's own settings, which the section has
		// always listed, and a line saying where its damage went: the values are
		// on the armour rows below it in the tree, and editing starts by picking
		// one of those.
		fields := r.Slot.RgdFields
		if len(fields) == 0 {
			fields = r.Slot.Fields
		}
		ui.UnitWeaponFieldList.Axis = layout.Vertical
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d armour type(s) under it in the tree — pick one to change its damage", len(damage)))
				l.Color = uikit.CTextDim
				l.MaxLines = 2
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.UnitWeaponFieldList.Layout(gtx, len(fields), func(gtx layout.Context, i int) layout.Dimensions {
					f := fields[i]
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), unitVoiceClip(f.Label, 26))
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), unitVoiceClip(f.Value, 24))
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	}
	edit, label, hasEdit := SelectedUnitWeaponEdit(ui)
	if !hasEdit {
		return layout.Dimensions{}
	}
	entry, value := edit.Damage.Entry, edit.Damage.Value
	if edit.Kind == 1 {
		entry, value = edit.Field.Entry, edit.Field.Value
	}
	key := fmt.Sprintf("%p", entry)
	if ui.UnitWeaponValBound != key {
		ui.UnitWeaponValEditor.SetText(value)
		ui.UnitWeaponValBound = key
		ui.UnitWeaponValError = ""
	}
	ui.UnitWeaponValEditor.SingleLine = true
	ui.UnitWeaponValEditor.Submit = true
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), unitVoiceClip(label, 40))
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			h := gtx.Dp(unit.Dp(26))
			gtx.Constraints.Min.Y = h
			gtx.Constraints.Max.Y = h
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(120))
			return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(ui.Th, &ui.UnitWeaponValEditor, "")
					ed.TextSize = unit.Sp(11)
					ed.HintColor = uikit.CTextDim
					return ed.Layout(gtx)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.UnitWeaponValError == "" {
				return layout.Dimensions{}
			}
			l := material.Label(ui.Th, unit.Sp(9), ui.UnitWeaponValError)
			l.Color = rgb(0xB00020)
			l.MaxLines = 2
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return uikit.SmallButton(ui, gtx, &ui.UnitWeaponSaveClick, "Save weapon")
				}),
			)
		}),
	)
}

// saveUnitWeaponFile writes the weapon's own .rgd back into the mod, which is
// where its damage lives.
func saveUnitWeaponFile(ui *uiState, slot *unitdef.WeaponSlot) error {
	if slot == nil || slot.File == nil {
		return fmt.Errorf("the weapon file is not loaded")
	}
	if ui.ModTree == nil {
		return fmt.Errorf("no mod is loaded")
	}
	data, err := slot.File.MarshalBinary()
	if err != nil {
		return err
	}
	disk, err := ui.ModTree.WriteOverlayFile(slot.Path, data)
	if err != nil {
		return err
	}
	ui.StatusText = fmt.Sprintf("Saved %s → %s", filepath.Base(slot.Path), disk)
	return nil
}

// ApplyUnitWeaponEditText writes a value into the selected damage row — the
// amount itself or a value against one armour type — and saves the weapon file,
// so the number is on disk as soon as it is committed.
func ApplyUnitWeaponEditText(ui *uiState, text string) error {
	edit, _, ok := SelectedUnitWeaponEdit(ui)
	if !ok {
		return fmt.Errorf("no damage row is selected")
	}
	entry := edit.Damage.Entry
	if edit.Kind == 1 {
		entry = edit.Field.Entry
	}
	changed, err := rgdApplyScalarText(entry, text)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return saveUnitWeaponFile(ui, edit.Slot)
}

// unitWeaponLocation finds the hardpoint and slot name a flattened weapon index
// came from, which is what an add or a remove has to name.
func unitWeaponLocation(d *unitdef.UnitDetails, rowIndex int) (hardpoint, slot string, ok bool) {
	seen := 0
	for _, h := range d.Weapons {
		for _, w := range h.Weapons {
			if seen == rowIndex {
				return h.Hardpoint, w.Slot, true
			}
			seen++
		}
	}
	return "", "", false
}

// RemoveUnitWeapon empties the selected weapon's slot and saves the unit. The
// slot itself stays — a hardpoint has a fixed number of them — so this is the
// same operation the game's own editor calls removing a weapon.
func RemoveUnitWeapon(ui *uiState) {
	d := CurrentUnitDetails(ui)
	r, ok := SelectedUnitWeaponRow(ui)
	if d == nil || d.File == nil || !ok || r.Kind != 0 {
		ui.StatusText = "Select a weapon to remove"
		return
	}
	hardpoint, slotName, found := unitWeaponLocation(d, r.Weapon)
	if !found {
		ui.StatusText = "The weapon is not in any hardpoint"
		return
	}
	for _, s := range unitdef.WeaponTableSlots(d.File, hardpoint) {
		if !strings.EqualFold(s.Name, slotName) {
			continue
		}
		ref := unitdef.WeaponSlotRefEntry(s)
		if ref == nil {
			ui.StatusText = "That slot names no weapon"
			return
		}
		if _, err := rgdApplyScalarText(ref, ""); err != nil {
			ui.StatusText = "Could not empty the slot: " + err.Error()
			return
		}
		ui.UnitInfoDirty = true
		if _, err := SaveUnitDetails(ui); err != nil {
			ui.StatusText = "Remove failed: " + err.Error()
			return
		}
		RevertUnitDetails(ui)
		ui.UnitWeaponSel = 0
		ui.UnitWeaponOpen = nil
		ui.StatusText = fmt.Sprintf("Removed %s from %s", r.Slot.Name, hardpoint)
		return
	}
	ui.StatusText = "The slot is no longer in the unit"
}

// OpenUnitWeaponAddPicker asks for a weapon .rgd to put into the first empty slot
// of the selected weapon's hardpoint.
func OpenUnitWeaponAddPicker(ui *uiState) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		ui.StatusText = "No unit is open"
		return
	}
	hardpoint := ""
	if r, ok := SelectedUnitWeaponRow(ui); ok {
		hardpoint, _, _ = unitWeaponLocation(d, r.Weapon)
	}
	if hardpoint == "" && len(d.Weapons) > 0 {
		hardpoint = d.Weapons[0].Hardpoint
	}
	if hardpoint == "" {
		ui.StatusText = "This unit has no hardpoint to mount a weapon on"
		return
	}
	OpenFilePicker(ui, pickConfig{
		Title:    "Add a weapon",
		Subtitle: "A weapon .rgd from the mod, mounted in " + hardpoint,
		Hint:     `Data\attrib\weapon\weapon.rgd`,
		Exts:     []string{".rgd"},
		Start:    d.Entry.Path,
		OnPick: func(path string) (bool, string) {
			return addUnitWeaponPick(ui, hardpoint, path)
		},
	})
}

// addUnitWeaponPick mounts the chosen weapon in the first slot of the hardpoint
// that carries none, and saves the unit.
func addUnitWeaponPick(ui *uiState, hardpoint, path string) (bool, string) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		return false, "No unit is open"
	}
	if !strings.EqualFold(unitVoiceExt(path), ".rgd") {
		return false, "Choose a weapon .rgd"
	}
	name := unitVoiceBaseName(path)
	for _, s := range unitdef.WeaponTableSlots(d.File, hardpoint) {
		ref := unitdef.WeaponSlotRefEntry(s)
		if ref == nil {
			// An unused slot ships with no `weapon` field at all: its table names
			// the schema and holds nothing else. Skipping it, as an earlier version
			// did, left "every slot already carries a weapon" as the only possible
			// answer, so the field is created here instead.
			ref = &rgd.RgdEntry{Name: "weapon", Type: rgd.RgdTypeString, Hash: rgd.Hash("weapon")}
			s.Children = append(s.Children, ref)
		}
		if !unitdef.IsFreeWeaponSlot(ref.DisplayValue()) {
			continue
		}
		if _, err := rgdApplyScalarText(ref, `weapon\`+name+`.lua`); err != nil {
			return false, "Could not name the weapon: " + err.Error()
		}
		ui.UnitInfoDirty = true
		if _, err := SaveUnitDetails(ui); err != nil {
			return false, "Save failed: " + err.Error()
		}
		RevertUnitDetails(ui)
		ui.UnitWeaponSel = 0
		ui.UnitWeaponOpen = nil
		ui.StatusText = fmt.Sprintf("Added %s to %s", name, hardpoint)
		return true, ""
	}
	return false, "Every slot of " + hardpoint + " already carries a weapon"
}

// HandleUnitWeaponTreeInput reads the section's presses: a tree row, the value
// editor, Save weapon, and the add and remove buttons.
func HandleUnitWeaponTreeInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != "Weapons" {
		return
	}
	rows := unitWeaponTreeRows(ui)
	for i := range ui.UnitWeaponRowClicks {
		if i >= len(rows) {
			break
		}
		if ui.UnitWeaponRowClicks[i].Clicked(gtx) {
			ui.UnitWeaponSel = i
			if rows[i].Kind == 0 && rows[i].Weapon >= 0 && rows[i].Weapon < len(ui.UnitWeaponOpen) {
				ui.UnitWeaponOpen[rows[i].Weapon] = !ui.UnitWeaponOpen[rows[i].Weapon]
			}
			return
		}
	}
	if ui.UnitWeaponAddClick.Clicked(gtx) {
		OpenUnitWeaponAddPicker(ui)
		return
	}
	if ui.UnitWeaponDelClick.Clicked(gtx) {
		RemoveUnitWeapon(ui)
		return
	}
	if _, _, ok := SelectedUnitWeaponEdit(ui); ok {
		for {
			ev, more := ui.UnitWeaponValEditor.Update(gtx)
			if !more {
				break
			}
			if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
				ui.UnitWeaponSaveClick.Clicked(gtx)
			}
		}
	}
	if ui.UnitWeaponSaveClick.Clicked(gtx) {
		if err := ApplyUnitWeaponEditText(ui, ui.UnitWeaponValEditor.Text()); err != nil {
			ui.UnitWeaponValError = err.Error()
			return
		}
		ui.UnitWeaponValError = ""
	}
}
