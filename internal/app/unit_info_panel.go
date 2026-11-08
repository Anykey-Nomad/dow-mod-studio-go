package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/rgd"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"image"
)

// layoutUnitMiddleColumnBody fills the middle column. The sections that have
// data show it. "Viewport" is here as well even though layoutUnitBody gives it
// both columns, so that rendering this column on its own still shows the model.
func LayoutUnitMiddleColumnBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	sec := CurrentUnitSection(ui)
	switch sec.Label {
	case unitSectionViewport:
		return LayoutUnitViewportArea(ui, gtx)
	case unitSectionClassicOE:
		// Rendered here as well as in layoutUnitBody, the same way Viewport
		// is: laying this column out on its own still has to show the
		// editor's panels rather than fall through to a hint.
		return LayoutClassicOEArea(ui, gtx)
	case unitSectionEvents:
		return LayoutUnitEventTimelines(ui, gtx)
	case unitSectionVoices:
		return LayoutUnitVoiceCategories(ui, gtx)
	case "Unit info":
		return LayoutUnitInfoList(ui, gtx)
	case "Weapons":
		return LayoutUnitWeaponList(ui, gtx)
	case "Abilities":
		return LayoutUnitAbilityList(ui, gtx)
	case "Squads":
		return LayoutUnitSquadList(ui, gtx)
	case "Links":
		return LayoutUnitLinksList(ui, gtx)
	case "Frozen":
		return LayoutFrozenList(ui, gtx)
	case "Problems":
		return LayoutUnitProblemsList(ui, gtx)
	default:
		return UnitColumnBody(ui, gtx, sec.Hint)
	}
}

// unitEmptyState is what a populated column shows when there is no unit, or the
// unit could not be read.
func UnitEmptyState(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	msg := "No unit selected."
	if d != nil && d.Error != "" {
		msg = "Could not read this unit:\n" + d.Error
	}
	return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(10), msg)
		l.Color = uikit.CTextDim
		return l.Layout(gtx)
	})
}

// unitKeyValueRow is one label/value line of a list, the value in a dimmer
// colour so a long value reads as secondary to the label.
//
// A value that is a localisation id is drawn as an editable row instead: it
// shows the text rather than the id, says the id on hover, and takes the
// presses that change the text. Everything else is a plain line.
func UnitKeyValueRow(ui *uiState, gtx layout.Context, label, value string, isLocID bool) layout.Dimensions {
	return UnitKeyValueRowFor(ui, gtx, label, value, isLocID, "", nil)
}

// unitKeyValueRowFor is unitKeyValueRow with the field behind a localisation
// row, which is what the presses act on. A non-localised row ignores them.
func UnitKeyValueRowFor(ui *uiState, gtx layout.Context, label, value string, isLocID bool, locID string, field *rgd.RgdEntry) layout.Dimensions {
	if isLocID {
		return LocaleEditableRow(ui, gtx, label, locID, field, localeRowKey(field, label))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), label)
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := value
			if len(text) > 64 {
				text = text[:61] + "…"
			}
			l := material.Label(ui.Th, unit.Sp(10), text)
			// A localisation id is a reference, not text, so it is shown in the
			// dimmer colour that marks a reference elsewhere in the app.
			if isLocID {
				l.Color = uikit.CTextDim
			} else {
				l.Color = uikit.CText
			}
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// layoutUnitInfoList is the "Unit info" column: the selected unit's own fields.
func LayoutUnitInfoList(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	if d.Error != "" {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return UnitEmptyState(ui, gtx)
		})
	}
	if len(d.Fields) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "This unit declares no readable properties.")
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		})
	}
	EnsureUnitInfoClicks(ui, len(d.Fields))
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutUnitInfoIconHeader(ui, gtx) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return LayoutListWithScrollbar(ui, gtx, &ui.UnitInfoScroll, &ui.UnitInfoList, unitInfoRowHeightDp, len(d.Fields), func(gtx layout.Context, index int) layout.Dimensions {
						return LayoutUnitInfoRow(ui, gtx, index, unitInfoFieldAt(d, index))
					})
				})
			}),
		)
	})
}

// layoutUnitInfoIconHeader is the top of the info page: the object's own icon
// beside its name, the way the game presents a unit in its own dialogs. The
// box is fixed at 40dp so switching units does not move the list underneath.
func LayoutUnitInfoIconHeader(ui *uiState, gtx layout.Context) layout.Dimensions {
	u := CurrentUnit(ui)
	if u == nil {
		return layout.Dimensions{}
	}
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return unitIconLayout(gtx, UnitIcon(ui, *u), 52, 40)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(12), UnitLabel(ui, *u))
						l.Color = uikit.CText
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), unitFileLabel(*u)+"  ·  "+u.Kind.Label)
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			}),
		)
	})
}

// LayoutUnitWeaponList is the "Weapons" column: the unit's weapons as a tree,
// each opening onto the damage it does against every armour type.
func LayoutUnitWeaponList(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitWeaponTree(ui, gtx)
}

// layoutUnitAbilityList is the "Abilities" column.
func LayoutUnitAbilityList(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	rows := unitAbilityRows(d)
	if len(rows) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "This unit declares no abilities.")
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		})
	}
	EnsureUnitRowClicks(ui, len(rows))
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range rows {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitSelectableRow(ui, gtx, i, len(rows), rows[i].Hardpoint, rows[i].Name, "missing", !rows[i].Found)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// layoutUnitSelectableRow is one clickable row of the weapons or abilities list.
// missingLabel says what to print for a row whose reference did not resolve.
func LayoutUnitSelectableRow(ui *uiState, gtx layout.Context, index, count int, slot, name, missingLabel string, missing bool) layout.Dimensions {
	sel := index == ui.UnitSelRow
	bg := uikit.CPanelBg
	switch {
	case sel:
		bg = uikit.CSelectBg
	case ui.UnitRowClicks[index].Hovered():
		bg = uikit.CMenuHover
	}
	return ui.UnitRowClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), slot)
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							text := name
							if text == "" {
								text = "(unnamed)"
							}
							if len(text) > 30 {
								text = text[:27] + "…"
							}
							l := material.Label(ui.Th, unit.Sp(11), text)
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !missing {
								return layout.Dimensions{}
							}
							l := material.Label(ui.Th, unit.Sp(9), missingLabel)
							l.Color = rgb(0xC98A8A)
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

// layoutUnitRightColumnBody is the right column: the properties of whatever the
// middle column has selected.
func LayoutUnitRightColumnBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	sec := CurrentUnitSection(ui)
	var rows []unitWeaponRow
	var fields []unitField
	switch sec.Label {
	case "Unit info":
		return LayoutUnitFieldPanel(ui, gtx, sec)
	case "Weapons":
		return LayoutUnitWeaponDetail(ui, gtx)
	case "Abilities":
		d := CurrentUnitDetails(ui)
		rows = unitAbilityRows(d)
	case "Squads":
		return LayoutUnitSquadRightColumn(ui, gtx, sec)
	case unitSectionEvents:
		return LayoutUnitEventPanel(ui, gtx)
	case unitSectionVoices:
		return LayoutUnitVoicePanel(ui, gtx)
	case unitSectionViewport:
		return LayoutUnitModelSide(ui, gtx)
	case unitSectionClassicOE:
		// The classic editor had no separate properties column of its own
		// here; the section draws left panel and preview across both
		// columns, so this branch is only for the column drawn alone.
		return LayoutClassicOEPreview(ui, gtx)
	default:
		return UnitColumn(ui, gtx, sec.RightTitle, "Selected entry properties will appear here.")
	}
	if len(rows) == 0 {
		return UnitColumn(ui, gtx, sec.RightTitle, "No entry is selected.")
	}
	idx := clampInt(ui.UnitSelRow, 0, len(rows)-1)
	row := rows[idx]
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "Slot", row.Hardpoint, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "Name", row.Name, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								// The unit writes a .lua because the game
								// refers to the .rgd it converts in memory;
								// what is shown is the file that exists.
								refValue := row.Ref
								if row.Found {
									refValue = row.Path
								}
								return UnitKeyValueRow(ui, gtx, "Reference", refValue, false)
							})
						}),
					}
					if !row.Found {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(10), "The weapon file was not found in this mod.")
								l.Color = rgb(0xC98A8A)
								return l.Layout(gtx)
							})
						}))
					}
					for _, f := range fields {
						f := f
						if f.StartsGroup && f.Group != "" {
							group := f.Group
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return UnitGroupHeading(ui, gtx, group)
							}))
						}
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, f.Label, f.Value, false)
							})
						}))
					}
					if len(fields) == 0 {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(10), "No readable settings for this entry.")
								l.Color = uikit.CTextDim
								return l.Layout(gtx)
							})
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			}),
		)
	})
}

// handleUnitPickerInput owns the pickers' clicks. It runs before the section
// list's, and a click on a dropdown row is consumed here so it cannot also
// change the section underneath.
//
// A list that is open stays open until something closes it: a row is picked,
// the other trigger is used, the click lands outside the list, or Escape
// arrives. It is deliberately never closed just because a frame passed with no
// row clicked — that would shut the list on the frame after it opened, which
// is what made it flash and vanish.
func HandleUnitPickerInput(ui *uiState, gtx layout.Context) {
	// The filter's caret is only wanted while its list is on screen: an editor
	// left focused after the list went away would go on swallowing whatever is
	// typed next, and the next search would open on those characters.
	if !UnitPickerOpen(ui) && ui.UnitPickerFilter.Focused(gtx) {
		ui.UnitPickerFilter.Blur(gtx)
	}
	// The triggers toggle their own list and put the other one away, so
	// clicking Race while the unit list is open swaps them rather than
	// stacking two lists over the columns.
	if ui.UnitRaceTrigger.Clicked(gtx) {
		ui.UnitRaceOpen = !ui.UnitRaceOpen
		ui.UnitListOpen = false
		return
	}
	if ui.UnitListTrigger.Clicked(gtx) {
		ui.UnitListOpen = !ui.UnitListOpen
		ui.UnitRaceOpen = false
		return
	}
	// The new object buttons sit with the pickers, so a dropdown that is open is
	// put away before the dialog goes up rather than over it.
	if ui.UnitNewTrigger.Clicked(gtx) {
		OpenNewObjDialog(ui, newObjUnit)
		return
	}
	if ui.UnitBuildingTrigger.Clicked(gtx) {
		OpenNewObjDialog(ui, newObjBuilding)
		return
	}
	// The freeze buttons are in the same strip, so they are read here with the
	// rest of it. A dropdown that is open is put away first, for the same
	// reason: the dialog and the lists are all one strip.
	HandleFreezeInput(ui, gtx)
	if ui.UnitListOpen {
		// The filter belongs to the list: it takes the caret, pastes a copied
		// name and narrows the rows the clicks below are read from. A press
		// that landed on it is consumed here, so the catch-all underneath
		// does not read it as a click outside the list and shut it.
		if unitPickerUpdateFilter(ui, gtx) {
			return
		}
		cat := UnitCatalogFor(ui)
		if cat == nil {
			ui.UnitListOpen = false
			return
		}
		hits := UnitPickerHits(ui)
		EnsureUnitDropdownClicks(ui, len(hits))
		for i := range hits {
			if i >= len(ui.UnitClicks) || !ui.UnitClicks[i].Clicked(gtx) {
				continue
			}
			hit := hits[i]
			if hit.race < 0 || hit.race >= len(cat.Races) {
				return
			}
			u, ok := unitPickerUnitAt(&cat.Races[hit.race], hit.unit)
			if !ok {
				return
			}
			// A hit may belong to another race: the search that found it
			// looked everywhere, so both halves of the selection move.
			ui.UnitRaceSel = hit.race
			ui.UnitSel = hit.unit
			ui.UnitListOpen = false
			ui.UnitSelRow = 0
			ui.StatusText = "Unit: " + UnitLabel(ui, u)
			// The pickers are part of the saved session, so that the
			// Object window comes back on the unit it was left on.
			MarkStateDirty(ui)
			return
		}
	}
	if ui.UnitRaceOpen {
		cat := UnitCatalogFor(ui)
		if cat == nil {
			ui.UnitRaceOpen = false
			return
		}
		EnsureUnitRaceClicks(ui, len(cat.Races))
		for i := range cat.Races {
			if i < len(ui.UnitRaceClicks) && ui.UnitRaceClicks[i].Clicked(gtx) {
				ui.UnitRaceSel = i
				ui.UnitRaceOpen = false
				// The unit list belongs to the race, so a new race starts
				// at its first unit.
				ui.UnitSel = 0
				ui.UnitSelRow = 0
				ui.StatusText = "Race: " + cat.Races[i].Name
				MarkStateDirty(ui)
				return
			}
		}
	}
	// A click anywhere else puts an open list away. The catcher is laid over
	// the columns but under the strip and the lists, so it only ever sees the
	// clicks that landed on neither.
	if UnitPickerOpen(ui) && ui.UnitDismissClick.Clicked(gtx) {
		CloseUnitPickers(ui)
	}
}

// handleUnitRowInput owns the selectable rows of the populated columns.
func HandleUnitRowInput(ui *uiState, gtx layout.Context) {
	sec := CurrentUnitSection(ui)
	var rows []unitWeaponRow
	switch sec.Label {
	case "Weapons":
		rows = unitWeaponRows(CurrentUnitDetails(ui))
	case "Abilities":
		rows = unitAbilityRows(CurrentUnitDetails(ui))
	case "Frozen":
		// The rows of this list are the way back into the mod, so a press on
		// one acts on it rather than selecting it.
		HandleFrozenRowInput(ui, gtx)
		return
	default:
		return
	}
	EnsureUnitRowClicks(ui, len(rows))
	for i := range rows {
		if i < len(ui.UnitRowClicks) && ui.UnitRowClicks[i].Clicked(gtx) {
			ui.UnitSelRow = i
			return
		}
	}
}
