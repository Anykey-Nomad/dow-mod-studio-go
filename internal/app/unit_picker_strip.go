package app

import (
	"dow-mod-studio/internal/app/uikit"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"strings"
)

// layoutUnitPickerStrip draws the race and unit pickers. It is a Rigid above
// the columns, so its height is fixed and an open dropdown is drawn as an
// overlay on top rather than pushing the columns down.
func LayoutUnitPickerStrip(ui *uiState, gtx layout.Context) layout.Dimensions {
	cat := UnitCatalogFor(ui)
	race := CurrentRace(ui)
	sel := CurrentUnit(ui)

	raceLabel := PickerRaceLabel(ui)
	unitLabel := PickerUnitLabel(ui)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(unitColGapDp)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.UnitRaceTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return UnitPickerField(ui, gtx, "Race", raceLabel, cat != nil && len(cat.Races) > 0, ui.UnitRaceOpen)
						})
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.UnitListTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return UnitPickerField(ui, gtx, "Unit", unitLabel, cat != nil && unitPickerAllUnits(cat) > 0, ui.UnitListOpen)
					})
				}),
				// The buttons that start a new unit or a new building of the
				// race on show. They sit with the pickers because that is
				// where the race they write into comes from.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(unitColGapDp)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						h := gtx.Dp(unit.Dp(unitPickerStripHeightDp - 8))
						gtx.Constraints.Min.Y = h
						gtx.Constraints.Max.Y = h
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return SmallButtonDisabled(ui, gtx, &ui.UnitNewTrigger, "New unit…", race == nil)
							}), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return SmallButtonDisabled(ui, gtx, &ui.UnitBuildingTrigger, "New building…", race == nil)
								})
							}),
							// The freeze buttons. They sit with the
							// pickers because they act on whatever the
							// Unit field is showing, and that is the
							// one place the selected object is named.
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return LayoutFreezeButtons(ui, gtx, sel)
								})
							}),
						)
					})
				}),
				layout.Flexed(1, flexGap()),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if race == nil {
						return layout.Dimensions{}
					}
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), raceCountLabel(race))
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

// pickerRaceLabel is what the Race field reads. It is a plain string so the
// strip can be measured and drawn from the same value.
func PickerRaceLabel(ui *uiState) string {
	if r := CurrentRace(ui); r != nil {
		return r.Name
	}
	return "No race"
}

// pickerUnitLabel is what the Unit field reads: the unit's own name and which
// of the two folders it came from. The name is the localised one where the
// string table has it, the same text the open list shows, so the field and the
// rows it opens never disagree.
func PickerUnitLabel(ui *uiState) string {
	u := CurrentUnit(ui)
	if u == nil {
		return "No unit"
	}
	return UnitLabel(ui, *u) + "  (" + u.Kind.Label + ")"
}

func raceCountLabel(r *raceEntry) string {
	if r == nil {
		return ""
	}
	return itoaCount(r.UnitCount(), "unit", "units")
}

// layoutUnitRaceDropdown is the open race list. It hangs below the strip, over
// the columns, and its own rows carry the click targets. xOffset is where the
// Race field sits, so the list lines up with the field that opened it.
func LayoutUnitRaceDropdown(ui *uiState, gtx layout.Context, xOffset int) layout.Dimensions {
	cat := UnitCatalogFor(ui)
	if cat == nil || len(cat.Races) == 0 {
		return layout.Dimensions{}
	}
	EnsureUnitRaceClicks(ui, len(cat.Races))
	rowH := gtx.Dp(unit.Dp(unitRowHeightDp))
	listH := rowH * len(cat.Races)
	if maxH := gtx.Constraints.Max.Y; listH > maxH && maxH > 0 {
		listH = maxH
	}
	width := gtx.Dp(unit.Dp(240))
	if width > gtx.Constraints.Max.X-xOffset {
		width = gtx.Constraints.Max.X - xOffset
	}
	if width <= 0 {
		return layout.Dimensions{}
	}
	off := op.Offset(image.Pt(xOffset, 0)).Push(gtx.Ops)
	defer off.Pop()
	gtx.Constraints.Min = image.Pt(width, listH)
	gtx.Constraints.Max = gtx.Constraints.Min
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var children []layout.FlexChild
				for i := range cat.Races {
					i := i
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sel := i == cat.ClampRaceSel(ui.UnitRaceSel)
						return ui.UnitRaceClicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							h := gtx.Dp(unit.Dp(unitRowHeightDp))
							sz := image.Pt(gtx.Constraints.Max.X, h)
							gtx.Constraints.Min = sz
							gtx.Constraints.Max = sz
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									if sel {
										rrect(gtx, uikit.CSelectBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
									} else if ui.UnitRaceClicks[i].Hovered() {
										rrect(gtx, uikit.CMenuHover, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
									}
									return layout.Dimensions{Size: gtx.Constraints.Max}
								}),
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(11), cat.Races[i].Name)
												l.Color = uikit.CText
												l.MaxLines = 1
												return l.Layout(gtx)
											}),
											layout.Flexed(1, flexGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(10), itoaPlain(cat.Races[i].UnitCount()))
												l.Color = uikit.CTextDim
												return l.Layout(gtx)
											}),
										)
									})
								}),
							)
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
		}),
	)
}

// layoutUnitDropdown draws whichever list is open, so the caller does not have
// to branch and so both lists are positioned by the same code.
func LayoutUnitDropdown(ui *uiState, gtx layout.Context, xOffset int) layout.Dimensions {
	if ui.UnitRaceOpen {
		return LayoutUnitRaceDropdown(ui, gtx, xOffset)
	}
	return LayoutUnitListDropdown(ui, gtx, xOffset)
}

// unitPickerHit is one row of the unit dropdown: which race, and which unit of
// that race, the row stands for. Rows are addressed by these rather than by
// their own position, because a filtered list can hold units of several races
// and picking one has to move both halves of the selection.
type unitPickerHit struct {
	race int
	unit int
}

// unitPickerFilterDp is the height of the filter row the dropdown carries
// above its rows — the field itself and the padding around it.
const unitPickerFilterDp = 32

// unitPickerUnitAt is the race's unit at an index of the list the picker
// shows — troops before structures, the order UnitsOf builds it in — read in
// place so a row is drawn without building that list again for every row.
func unitPickerUnitAt(r *raceEntry, i int) (unitEntry, bool) {
	if r == nil || i < 0 {
		return unitEntry{}, false
	}
	if i < len(r.Troops) {
		return r.Troops[i], true
	}
	if j := i - len(r.Troops); j < len(r.Structures) {
		return r.Structures[j], true
	}
	return unitEntry{}, false
}

// unitPickerAllUnits is every unit of every race, which is what the count
// beside the filter is out of once it is searching everywhere.
func unitPickerAllUnits(cat *unitCatalog) int {
	n := 0
	for i := range cat.Races {
		n += cat.Races[i].UnitCount()
	}
	return n
}

// UnitPickerHits is what the unit dropdown lists. With the filter empty it is
// the units of the race on show in their own order, which is the list the
// dropdown has always been. With text in it, it is every unit of every race
// whose name, file name or race matches — so a unit can be found without
// knowing which race holds it, and picking one moves the race with it.
func UnitPickerHits(ui *uiState) []unitPickerHit {
	cat := UnitCatalogFor(ui)
	if cat == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(ui.UnitPickerFilter.Text()))
	if q == "" {
		raceIdx := cat.ClampRaceSel(ui.UnitRaceSel)
		if raceIdx >= len(cat.Races) {
			return nil
		}
		n := cat.Races[raceIdx].UnitCount()
		hits := make([]unitPickerHit, n)
		for i := range hits {
			hits[i] = unitPickerHit{race: raceIdx, unit: i}
		}
		return hits
	}
	var hits []unitPickerHit
	for ri := range cat.Races {
		r := &cat.Races[ri]
		race := strings.ToLower(r.Name)
		for i := 0; i < r.UnitCount(); i++ {
			u, ok := unitPickerUnitAt(r, i)
			if !ok {
				continue
			}
			if strings.Contains(strings.ToLower(UnitLabel(ui, u)), q) ||
				strings.Contains(strings.ToLower(u.Name), q) ||
				strings.Contains(race, q) {
				hits = append(hits, unitPickerHit{race: ri, unit: i})
			}
		}
	}
	return hits
}

// unitPickerFilterRow is the filter pinned above the dropdown's rows: the
// field that narrows them, and how many are left of how many there are — the
// count is what says the filter is doing anything, and what says it found
// nothing rather than the race being empty.
func unitPickerFilterRow(ui *uiState, gtx layout.Context, shown, total int, filtering bool) layout.Dimensions {
	gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(unitPickerFilterDp))
	gtx.Constraints.Max.Y = gtx.Constraints.Min.Y
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return UnitListFilterField(ui, gtx, &ui.UnitPickerFilter, "filter units")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := fmt.Sprintf("%d units", total)
				if filtering {
					text = fmt.Sprintf("%d of %d", shown, total)
				}
				l := material.Label(ui.Th, unit.Sp(9), text)
				l.Color = uikit.CTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
		)
	})
}

// unitPickerUpdateFilter owns the filter's own input: a press takes the caret,
// typing narrows the rows as it goes, a right press pastes a copied name, and a
// filter that just changed starts the list again at the top — the rows it
// keeps are not the rows the old offset was measured against. It reports
// whether the press landed on the field, so its caller can consume it rather
// than let the click catcher underneath read it as a click outside the list.
func unitPickerUpdateFilter(ui *uiState, gtx layout.Context) bool {
	ed := &ui.UnitPickerFilter
	ed.SingleLine = true
	uikit.MarkClipPasteAny(ui, ed)
	pasted := uikit.PasteClipField(ui, gtx, ed)
	clicked := RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx)
	if clicked {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		if _, ok := ed.Update(gtx); !ok {
			break
		}
	}
	if text := ed.Text(); text != ui.UnitPickerLastFilter {
		ui.UnitList.Position = layout.Position{}
		ui.UnitListScroll.Offset = 0
		ui.UnitPickerLastFilter = text
	}
	return clicked || pasted
}

// layoutUnitListDropdown is the open unit list: the filter and, below it, the
// units it keeps — the race on show, or every race once there is a filter.
// It hangs below the strip, over the columns, and scrolls with the same list
// helper the rest of the app uses. xOffset is where the Unit field sits.
func LayoutUnitListDropdown(ui *uiState, gtx layout.Context, xOffset int) layout.Dimensions {
	cat := UnitCatalogFor(ui)
	race := CurrentRace(ui)
	if cat == nil || race == nil {
		return layout.Dimensions{}
	}
	hits := UnitPickerHits(ui)
	filtering := strings.TrimSpace(ui.UnitPickerFilter.Text()) != ""
	total := race.UnitCount()
	if filtering {
		total = unitPickerAllUnits(cat)
	}
	EnsureUnitDropdownClicks(ui, len(hits))
	rowH := gtx.Dp(unit.Dp(unitRowHeightDp))
	// The filter sits above the rows and is part of the box's own height, so
	// the rows lose the room it takes rather than the list being pushed past
	// the columns it hangs over.
	filterH := gtx.Dp(unit.Dp(unitPickerFilterDp))
	listH := rowH * unitListVisibleRows
	if maxH := gtx.Constraints.Max.Y - filterH; listH > maxH && maxH > 0 {
		listH = maxH
	}
	if listH < rowH*3 {
		listH = rowH * 3
	}
	// Wide enough for a display name, the file name behind it and the kind
	// beside that. The rows were sized for the file name alone, which is already
	// too narrow for the longer ones.
	width := gtx.Dp(unit.Dp(340))
	if width > gtx.Constraints.Max.X-xOffset {
		width = gtx.Constraints.Max.X - xOffset
	}
	if width <= 0 {
		return layout.Dimensions{}
	}
	off := op.Offset(image.Pt(xOffset, 0)).Push(gtx.Ops)
	defer off.Pop()
	gtx.Constraints.Min = image.Pt(width, filterH+listH)
	gtx.Constraints.Max = gtx.Constraints.Min
	return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				// The filter is pinned at the top of the box and only the rows
				// below it scroll: a filter that scrolled away with its rows
				// could not be reached once the list had moved.
				unitPickerFilterRow(ui, gtx, len(hits), total, filtering)
				lg := gtx
				lg.Constraints.Min = image.Pt(width, listH)
				lg.Constraints.Max = lg.Constraints.Min
				listOff := op.Offset(image.Pt(0, filterH)).Push(gtx.Ops)
				dims := LayoutListWithScrollbar(ui, lg, &ui.UnitListScroll, &ui.UnitList, rowH, len(hits), func(gtx layout.Context, index int) layout.Dimensions {
					if index < 0 || index >= len(hits) {
						return layout.Dimensions{}
					}
					hit := hits[index]
					if hit.race < 0 || hit.race >= len(cat.Races) {
						return layout.Dimensions{}
					}
					rowRace := &cat.Races[hit.race]
					u, ok := unitPickerUnitAt(rowRace, hit.unit)
					if !ok {
						return layout.Dimensions{}
					}
					sel := hit.race == cat.ClampRaceSel(ui.UnitRaceSel) && hit.unit == rowRace.ClampUnitSel(ui.UnitSel)
					// What the last column reads: the kind the file sits in
					// for the race on show, and the race itself for a row a
					// search found in another one — the kind would not say
					// which race that was.
					endLabel := u.Kind.Label
					if hit.race != cat.ClampRaceSel(ui.UnitRaceSel) {
						endLabel = rowRace.Name
					}
					return ui.UnitClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						// The row's height is rowH, not gtx.Constraints.Min.Y:
						// inside a layout.List the element is handed its own
						// position as the minimum, which is 0 for the first row
						// and negative once the list has scrolled. Pinning the
						// size to that gave every row a height of zero, and the
						// list rendered as an empty box.
						sz := image.Pt(gtx.Constraints.Max.X, rowH)
						gtx.Constraints.Min = sz
						gtx.Constraints.Max = sz
						return layout.Stack{}.Layout(gtx,
							layout.Expanded(func(gtx layout.Context) layout.Dimensions {
								if sel {
									rrect(gtx, uikit.CSelectBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
								} else if ui.UnitClicks[index].Hovered() {
									rrect(gtx, uikit.CMenuHover, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
								}
								return layout.Dimensions{Size: gtx.Constraints.Max}
							}), layout.Stacked(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										// The icon the game shows for this
										// object, in a fixed box so rows
										// stay aligned when a mod ships
										// none of them.
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return unitIconLayout(gtx, UnitIcon(ui, u), 24, 18)
											})
										}),
										// The display name takes what is left and is
										// the only thing that may be cut short:
										// several units of one race share a name in
										// the string table (Dais of Destruction and
										// its _sp_dxp3 both read "Dais of
										// Destruction"), so the file name beside it
										// is what tells two rows apart and must
										// never be the part that disappears.
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											l := material.Label(ui.Th, unit.Sp(11), UnitLabel(ui, u))
											l.Color = uikit.CText
											l.MaxLines = 1
											return l.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											l := material.Label(ui.Th, unit.Sp(9), unitFileLabel(u))
											l.Color = uikit.CTextDim
											l.MaxLines = 1
											return l.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											l := material.Label(ui.Th, unit.Sp(10), endLabel)
											l.Color = uikit.CTextDim
											l.MaxLines = 1
											return l.Layout(gtx)
										}),
									)
								})
							}),
						)
					})
				})
				listOff.Pop()
				return layout.Dimensions{Size: image.Pt(width, filterH+dims.Size.Y)}
			}),
		)
	})
}
