package main

import (
	"dow-mod-studio/internal/rgd"
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ── browsing the mod's units ────────────────────────────────────────────────
//
// The dialog's own selectors, and the columns filled from the selected unit.
// The pickers sit in a strip above the three columns, below the title, so the
// window says which unit it is about before its sections are read.

// unitPickerStripHeightDp is the strip's own height, used to keep the dropdowns
// from overlapping the columns when they open.
const unitPickerStripHeightDp = 34

// unitTabPadDp is the padding around the columns and the strip in the tab's
// content area.
const unitTabPadDp = 8

// unitListVisibleRows is how many unit rows the dropdown shows before it
// starts scrolling.
const unitListVisibleRows = 12

// unitCatalogFor returns the catalog for the open mod, building it the first
// time it is asked for and again when the mod changes. Building walks every
// race folder, so it is kept off the per-frame path and keyed by the mod's name.
//
// It resets the *selection* when the mod changes, because the old indexes and
// the cached unit then mean nothing. It deliberately does not touch whether a
// dropdown is open: the first call happens lazily, on whatever frame first asks
// for the catalog — which is the frame the user clicked the picker — so
// clearing the open state here would shut the list the instant it opened.
func (ui *uiState) unitCatalogFor() *unitCatalog {
	if ui.modTree == nil {
		ui.unitCatalog = nil
		ui.unitCatalogMod = ""
		ui.modGraph = nil
		ui.modGraphMod = ""
		// With no mod there is nothing for a running scan to be reading, and
		// a tree that is being closed is the last place to find that out.
		ui.stopUnitNameScan()
		ui.unitNames = nil
		ui.unitNamesDone = false
		ui.stopUnitSquadScan()
		ui.unitSquads = nil
		ui.unitSquadsDone = false
		ui.forgetFreezeBook()
		return nil
	}
	mod := ui.modTree.FileMapName
	if ui.unitCatalog != nil && ui.unitCatalogMod == mod {
		return ui.unitCatalog
	}
	// A different mod means a different set of files, so the graph of the old
	// one is meaningless as well.
	ui.modGraph = nil
	ui.modGraphMod = ""
	ui.forgetFreezeBook()
	ui.unitCatalog = buildUnitCatalog(ui.modTree)
	ui.unitCatalogMod = mod
	// The catalog is only the file names; the names the lists show come from
	// each unit's own localisation id, which is a .rgd read per unit. That
	// starts here, in the background, so the dropdown is never the thing that
	// waits for it.
	ui.unitNamesDone = false
	ui.startUnitNameScan(ui.modTree, ui.unitCatalog)
	// The squad index has the same shape of problem and the same answer: the
	// link from a unit to its squads exists only in the squad files, so those
	// are walked in the background too. Measured at 880ms for the 431 squads of
	// the DXP3 tree, which is far too long to spend on the frame the dialog
	// opens.
	ui.unitSquadsDone = false
	ui.startUnitSquadScan(ui.modTree)
	// A different mod means a different set of races and units, so the old
	// indexes and the cached unit are meaningless.
	ui.unitRaceSel = 0
	ui.unitSel = 0
	ui.unitDetails = nil
	ui.unitDetailsPath = ""
	ui.unitSelRow = 0
	ui.applyPendingUnitPick()
	return ui.unitCatalog
}

// applyPendingUnitPick puts the pickers back where the last session left them.
//
// They are restored by name rather than by index because the catalog is built
// afresh from whatever the mod holds: an index saved in one session would point
// at whatever unit happens to sit there in the next. A name the new mod does
// not have — a unit that was deleted, or a mod that was swapped since — is
// simply not used, and the window opens on the first unit as it always did.
//
// This is the only place that consumes them, so a mod opened later in the
// session starts from the top rather than being dragged back to a unit of the
// one that was open when the app started.
func (ui *uiState) applyPendingUnitPick() {
	if ui.unitCatalog == nil || (ui.unitPickRace == "" && ui.unitPickPath == "") {
		return
	}
	race, path := ui.unitPickRace, ui.unitPickPath
	ui.unitPickRace, ui.unitPickPath = "", ""
	for i := range ui.unitCatalog.races {
		r := &ui.unitCatalog.races[i]
		if race != "" && !strings.EqualFold(r.name, race) {
			continue
		}
		ui.unitRaceSel = i
		if path == "" {
			return
		}
		units := r.unitsOf()
		for j := range units {
			if strings.EqualFold(units[j].path, path) {
				ui.unitSel = j
				return
			}
		}
		// The race is here but the unit is not, which is a mod that has
		// changed since: the race is still the right one to open on.
		return
	}
}

// graphFor is the mod read as a graph: every .rgd of Data\attrib as an entity,
// and every reference a unit or building holds as a link between two of them.
//
// It is built on first use rather than when the module opens, because indexing
// is cheap but reading the six hundred unit and building files to find their
// references is not, and a session that never opens the relationships view
// should never pay for it. The links themselves are read even later, by
// ensureLinks, for the same reason.
func (ui *uiState) graphFor() *modGraph {
	if ui.modTree == nil {
		ui.modGraph = nil
		ui.modGraphMod = ""
		return nil
	}
	mod := ui.modTree.FileMapName
	if ui.modGraph == nil || ui.modGraphMod != mod {
		ui.modGraph = newModGraph(ui.modTree)
		ui.modGraphMod = mod
	}
	return ui.modGraph
}

// currentUnit is the unit the dialog is showing, or nil when the mod has none.
func (ui *uiState) currentUnit() *unitEntry {
	return ui.unitCatalogFor().selectedUnit(ui.unitRaceSel, ui.unitSel)
}

// currentRace is the race the dialog is showing, or nil when there is none.
func (ui *uiState) currentRace() *raceEntry {
	return ui.unitCatalogFor().selectedRace(ui.unitRaceSel)
}

// currentUnitDetails parses the selected unit once and caches it, so switching
// between the sections does not re-read and re-parse the file each time.
func (ui *uiState) currentUnitDetails() *unitDetails {
	u := ui.currentUnit()
	if u == nil {
		return nil
	}
	if ui.unitDetails != nil && strings.EqualFold(ui.unitDetailsPath, u.path) {
		return ui.unitDetails
	}
	d := loadUnitDetails(ui.modTree, *u)
	ui.unitDetails = d
	ui.unitDetailsPath = u.path
	// A different unit means different fields, so the pointer targets of the
	// previous unit's localised rows are dead weight. Dropping them here keeps
	// the map the size of one unit's rows rather than one per unit ever opened.
	ui.localeEditTags = nil
	// A different unit means the editor's field is not in this file any more,
	// and any change that had not been written is the previous unit's. The
	// unsaved state is deliberately not carried over: unitInfoDirty says the
	// unit on screen has been changed, and it is asking about a different one.
	ui.unitInfoDirty = false
	ui.unitValError = ""
	ui.unitValBound = ""
	ui.unitValEditor.SetText("")
	ui.unitInfoSel = 0
	// A different unit means the selected row of the populated columns points
	// at nothing, so the selection starts over.
	ui.unitSelRow = 0
	// The columns are scrolled to the top for a new unit: the previous unit's
	// offset is meaningless against a different list, and keeping it would drop
	// the reader into the middle of the fields with no way to know they had
	// scrolled at all.
	ui.unitInfoList = layout.List{}
	ui.unitSquadList = layout.List{}
	// A different unit is a different model, so the viewport's viewer and the
	// textures it loaded go with the previous one. The next load only happens
	// if the Viewport section is looked at again, which is what keeps browsing
	// units from parsing five megabytes of geometry per click.
	ui.resetUnitModel()
	return d
}

// ── picker strip ────────────────────────────────────────────────────────────

// layoutUnitPickerStrip draws the race and unit pickers. It is a Rigid above
// the columns, so its height is fixed and an open dropdown is drawn as an
// overlay on top rather than pushing the columns down.
func (ui *uiState) layoutUnitPickerStrip(gtx layout.Context) layout.Dimensions {
	cat := ui.unitCatalogFor()
	race := ui.currentRace()
	sel := ui.currentUnit()

	raceLabel := ui.pickerRaceLabel()
	unitLabel := ui.pickerUnitLabel()
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(unitColGapDp)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.unitRaceTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return ui.unitPickerField(gtx, "Race", raceLabel, cat != nil && len(cat.races) > 0, ui.unitRaceOpen)
						})
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitListTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.unitPickerField(gtx, "Unit", unitLabel, sel != nil, ui.unitListOpen)
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
								return ui.smallButtonDisabled(gtx, &ui.unitNewTrigger, "New unit…", race == nil)
							}), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return ui.smallButtonDisabled(gtx, &ui.unitBuildingTrigger, "New building…", race == nil)
								})
							}),
							// The freeze buttons. They sit with the
							// pickers because they act on whatever the
							// Unit field is showing, and that is the
							// one place the selected object is named.
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return ui.layoutFreezeButtons(gtx, sel)
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
						l := material.Label(ui.th, unit.Sp(10), raceCountLabel(race))
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

// pickerRaceLabel is what the Race field reads. It is a plain string so the
// strip can be measured and drawn from the same value.
func (ui *uiState) pickerRaceLabel() string {
	if r := ui.currentRace(); r != nil {
		return r.name
	}
	return "No race"
}

// pickerUnitLabel is what the Unit field reads: the unit's own name and which
// of the two folders it came from. The name is the localised one where the
// string table has it, the same text the open list shows, so the field and the
// rows it opens never disagree.
func (ui *uiState) pickerUnitLabel() string {
	u := ui.currentUnit()
	if u == nil {
		return "No unit"
	}
	return ui.unitLabel(*u) + "  (" + u.kind.label + ")"
}

func raceCountLabel(r *raceEntry) string {
	if r == nil {
		return ""
	}
	return itoaCount(r.unitCount(), "unit", "units")
}

func itoaCount(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoaPlain(n) + " " + many
}

func itoaPlain(n int) string {
	if n < 0 {
		return "-" + itoaPlain(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoaPlain(n/10) + string(rune('0'+n%10))
}

// unitPickerField is one labelled dropdown trigger. The label sits in front of
// the value so the strip reads "Race  necrons" without needing a header row.
func (ui *uiState) unitPickerField(gtx layout.Context, label, value string, enabled, open bool) layout.Dimensions {
	return frameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(10), label)
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					text := value
					if len(text) > 42 {
						text = text[:39] + "…"
					}
					l := material.Label(ui.th, unit.Sp(11), text)
					l.Color = cText
					if !enabled {
						l.Color = cTextDim
					}
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// A caret, drawn as text so it needs no icon font.
					l := material.Label(ui.th, unit.Sp(9), "▼")
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
			)
		})
	}, cSaveBg, unitPickerBorder(open), unit.Dp(3))
}

// unitPickerBorder is a picker field's frame colour: the accent colour while
// its list is open, the normal one otherwise.
func unitPickerBorder(open bool) color.NRGBA {
	if open {
		return cSelectBd
	}
	return cSaveBd
}

// layoutUnitRaceDropdown is the open race list. It hangs below the strip, over
// the columns, and its own rows carry the click targets. xOffset is where the
// Race field sits, so the list lines up with the field that opened it.
func (ui *uiState) layoutUnitRaceDropdown(gtx layout.Context, xOffset int) layout.Dimensions {
	cat := ui.unitCatalogFor()
	if cat == nil || len(cat.races) == 0 {
		return layout.Dimensions{}
	}
	ui.ensureUnitRaceClicks(len(cat.races))
	rowH := gtx.Dp(unit.Dp(unitRowHeightDp))
	listH := rowH * len(cat.races)
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
			rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var children []layout.FlexChild
				for i := range cat.races {
					i := i
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sel := i == cat.clampRaceSel(ui.unitRaceSel)
						return ui.unitRaceClicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							h := gtx.Dp(unit.Dp(unitRowHeightDp))
							sz := image.Pt(gtx.Constraints.Max.X, h)
							gtx.Constraints.Min = sz
							gtx.Constraints.Max = sz
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									if sel {
										rrect(gtx, cSelectBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
									} else if ui.unitRaceClicks[i].Hovered() {
										rrect(gtx, cMenuHover, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
									}
									return layout.Dimensions{Size: gtx.Constraints.Max}
								}),
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(11), cat.races[i].name)
												l.Color = cText
												l.MaxLines = 1
												return l.Layout(gtx)
											}),
											layout.Flexed(1, flexGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(10), itoaPlain(cat.races[i].unitCount()))
												l.Color = cTextDim
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
func (ui *uiState) layoutUnitDropdown(gtx layout.Context, xOffset int) layout.Dimensions {
	if ui.unitRaceOpen {
		return ui.layoutUnitRaceDropdown(gtx, xOffset)
	}
	return ui.layoutUnitListDropdown(gtx, xOffset)
}

// layoutUnitListDropdown is the open unit list of the selected race. It hangs
// below the strip, over the columns, and scrolls with the same list helper the
// rest of the app uses. xOffset is where the Unit field sits.
func (ui *uiState) layoutUnitListDropdown(gtx layout.Context, xOffset int) layout.Dimensions {
	race := ui.currentRace()
	if race == nil {
		return layout.Dimensions{}
	}
	units := race.unitsOf()
	if len(units) == 0 {
		return layout.Dimensions{}
	}
	ui.ensureUnitDropdownClicks(len(units))
	rowH := gtx.Dp(unit.Dp(unitRowHeightDp))
	listH := rowH * unitListVisibleRows
	if maxH := gtx.Constraints.Max.Y; listH > maxH && maxH > 0 {
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
	gtx.Constraints.Min = image.Pt(width, listH)
	gtx.Constraints.Max = gtx.Constraints.Min
	return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutListWithScrollbar(gtx, &ui.unitListScroll, &ui.unitList, rowH, len(units), func(gtx layout.Context, index int) layout.Dimensions {
					if index < 0 || index >= len(units) {
						return layout.Dimensions{}
					}
					u := units[index]
					sel := index == race.clampUnitSel(ui.unitSel)
					return ui.unitClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
									rrect(gtx, cSelectBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
								} else if ui.unitClicks[index].Hovered() {
									rrect(gtx, cMenuHover, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
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
												return unitIconLayout(gtx, ui.unitIcon(u), 24, 18)
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
											l := material.Label(ui.th, unit.Sp(11), ui.unitLabel(u))
											l.Color = cText
											l.MaxLines = 1
											return l.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											l := material.Label(ui.th, unit.Sp(9), unitFileLabel(u))
											l.Color = cTextDim
											l.MaxLines = 1
											return l.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											l := material.Label(ui.th, unit.Sp(10), u.kind.label)
											l.Color = cTextDim
											l.MaxLines = 1
											return l.Layout(gtx)
										}),
									)
								})
							}),
						)
					})
				})
			}),
		)
	})
}

func (ui *uiState) ensureUnitRaceClicks(n int) {
	if len(ui.unitRaceClicks) != n {
		ui.unitRaceClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitDropdownClicks sizes the unit dropdown's row click targets. It is
// distinct from the section list's ensureUnitClicks, which takes no count.
func (ui *uiState) ensureUnitDropdownClicks(n int) {
	if len(ui.unitClicks) != n {
		ui.unitClicks = make([]widget.Clickable, n)
	}
}

// ── populated columns ───────────────────────────────────────────────────────

// layoutUnitMiddleColumnBody fills the middle column. The sections that have
// data show it. "Viewport" is here as well even though layoutUnitBody gives it
// both columns, so that rendering this column on its own still shows the model.
func (ui *uiState) layoutUnitMiddleColumnBody(gtx layout.Context) layout.Dimensions {
	sec := ui.currentUnitSection()
	switch sec.label {
	case unitSectionViewport:
		return ui.layoutUnitViewportArea(gtx)
	case unitSectionEvents:
		return ui.layoutUnitEventTimelines(gtx)
	case "Unit info":
		return ui.layoutUnitInfoList(gtx)
	case "Weapons":
		return ui.layoutUnitWeaponList(gtx)
	case "Abilities":
		return ui.layoutUnitAbilityList(gtx)
	case "Squads":
		return ui.layoutUnitSquadList(gtx)
	case "Links":
		return ui.layoutUnitLinksList(gtx)
	case "Frozen":
		return ui.layoutFrozenList(gtx)
	case "Problems":
		return ui.layoutUnitProblemsList(gtx)
	default:
		return ui.unitColumnBody(gtx, sec.hint)
	}
}

// unitColumnBody is the column's shared frame: header, rule, and a body that is
// either the section's list or its hint.
func (ui *uiState) unitColumnBody(gtx layout.Context, hint string) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, ui.currentUnitSection().middleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(10), hint)
					l.Color = cTextDim
					return l.Layout(gtx)
				})
			}),
		)
	})
}

// unitEmptyState is what a populated column shows when there is no unit, or the
// unit could not be read.
func (ui *uiState) unitEmptyState(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	msg := "No unit selected."
	if d != nil && d.error != "" {
		msg = "Could not read this unit:\n" + d.error
	}
	return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.th, unit.Sp(10), msg)
		l.Color = cTextDim
		return l.Layout(gtx)
	})
}

// unitSectionFrame is the frame of a column that carries a list.
func (ui *uiState) unitSectionFrame(gtx layout.Context, body layout.Widget) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, ui.currentUnitSection().middleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, body),
		)
	})
}

// unitKeyValueRow is one label/value line of a list, the value in a dimmer
// colour so a long value reads as secondary to the label.
//
// A value that is a localisation id is drawn as an editable row instead: it
// shows the text rather than the id, says the id on hover, and takes the
// presses that change the text. Everything else is a plain line.
func (ui *uiState) unitKeyValueRow(gtx layout.Context, label, value string, isLocID bool) layout.Dimensions {
	return ui.unitKeyValueRowFor(gtx, label, value, isLocID, "", nil)
}

// unitKeyValueRowFor is unitKeyValueRow with the field behind a localisation
// row, which is what the presses act on. A non-localised row ignores them.
func (ui *uiState) unitKeyValueRowFor(gtx layout.Context, label, value string, isLocID bool, locID string, field *rgd.RgdEntry) layout.Dimensions {
	if isLocID {
		return ui.localeEditableRow(gtx, label, locID, field, localeRowKey(field, label))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), label)
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := value
			if len(text) > 64 {
				text = text[:61] + "…"
			}
			l := material.Label(ui.th, unit.Sp(10), text)
			// A localisation id is a reference, not text, so it is shown in the
			// dimmer colour that marks a reference elsewhere in the app.
			if isLocID {
				l.Color = cTextDim
			} else {
				l.Color = cText
			}
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// layoutUnitInfoList is the "Unit info" column: the selected unit's own fields.
func (ui *uiState) layoutUnitInfoList(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	if d.error != "" {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return ui.unitEmptyState(gtx)
		})
	}
	if len(d.fields) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "This unit declares no readable properties.")
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		})
	}
	ui.ensureUnitInfoClicks(len(d.fields))
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(ui.layoutUnitInfoIconHeader),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutListWithScrollbar(gtx, &ui.unitInfoScroll, &ui.unitInfoList, unitInfoRowHeightDp, len(d.fields), func(gtx layout.Context, index int) layout.Dimensions {
						return ui.layoutUnitInfoRow(gtx, index, unitInfoFieldAt(d, index))
					})
				})
			}),
		)
	})
}

// layoutUnitInfoIconHeader is the top of the info page: the object's own icon
// beside its name, the way the game presents a unit in its own dialogs. The
// box is fixed at 40dp so switching units does not move the list underneath.
func (ui *uiState) layoutUnitInfoIconHeader(gtx layout.Context) layout.Dimensions {
	u := ui.currentUnit()
	if u == nil {
		return layout.Dimensions{}
	}
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return unitIconLayout(gtx, ui.unitIcon(*u), 52, 40)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(12), ui.unitLabel(*u))
						l.Color = cText
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), unitFileLabel(*u)+"  ·  "+u.kind.label)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			}),
		)
	})
}

// unitInfoRowHeightDp is the row height the info list's scrollbar is drawn at. The
// rows are not all this tall — a localisation row is two lines, a heading is
// another two — so this is the plain row's height and only feeds the thumb's
// size, which is what the panel's other lists do with it too.
const unitInfoRowHeightDp = 17

// layoutUnitWeaponList is the "Weapons" column. The rows are the hardpoints that
// actually hold a weapon, and selecting one is what the right column describes.
func (ui *uiState) layoutUnitWeaponList(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil || d.error != "" || len(d.weapons) == 0 {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	// One selectable row per weapon slot, so the right column can describe a
	// single weapon rather than a whole hardpoint.
	rows := unitWeaponRows(d)
	ui.ensureUnitRowClicks(len(rows))
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range rows {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutUnitSelectableRow(gtx, i, len(rows), rows[i].hardpoint, rows[i].name, "not found", !rows[i].found)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// unitWeaponRow is one selectable row of the weapons or abilities list.
type unitWeaponRow struct {
	// hardpoint is the owning field, e.g. "hardpoint_01" or "ability_02".
	hardpoint string
	// name is the weapon's or ability's file name.
	name string
	// ref is the raw reference as the unit wrote it.
	ref string
	// path is the file the reference resolved to, "" when it did not.
	path string
	// found reports whether ref resolved to a file in the tree.
	found bool
}

// unitWeaponRows flattens the unit's hardpoints into one row per weapon slot.
func unitWeaponRows(d *unitDetails) []unitWeaponRow {
	if d == nil {
		return nil
	}
	var out []unitWeaponRow
	for _, h := range d.weapons {
		for _, w := range h.weapons {
			out = append(out, unitWeaponRow{hardpoint: h.hardpoint, name: w.name, ref: w.ref, path: w.path, found: w.path != ""})
		}
	}
	return out
}

// unitAbilityRows flattens the unit's ability slots into rows. A slot with an
// empty value is left out: it is not an ability the unit has, and 115 of the
// race unit .rgds ship an empty ability_01.
func unitAbilityRows(d *unitDetails) []unitWeaponRow {
	if d == nil {
		return nil
	}
	var out []unitWeaponRow
	for _, a := range d.abilities {
		if a.ref == "" {
			continue
		}
		out = append(out, unitWeaponRow{hardpoint: a.slot, name: a.name(), ref: a.ref, path: a.path, found: a.resolved()})
	}
	return out
}

// layoutUnitAbilityList is the "Abilities" column.
func (ui *uiState) layoutUnitAbilityList(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil || d.error != "" {
		return ui.unitSectionFrame(gtx, ui.unitEmptyState)
	}
	rows := unitAbilityRows(d)
	if len(rows) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "This unit declares no abilities.")
				l.Color = cTextDim
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
					return ui.layoutUnitSelectableRow(gtx, i, len(rows), rows[i].hardpoint, rows[i].name, "missing", !rows[i].found)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// layoutUnitSelectableRow is one clickable row of the weapons or abilities list.
// missingLabel says what to print for a row whose reference did not resolve.
func (ui *uiState) layoutUnitSelectableRow(gtx layout.Context, index, count int, slot, name, missingLabel string, missing bool) layout.Dimensions {
	sel := index == ui.unitSelRow
	bg := cPanelBg
	switch {
	case sel:
		bg = cSelectBg
	case ui.unitRowClicks[index].Hovered():
		bg = cMenuHover
	}
	return ui.unitRowClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), slot)
							l.Color = cTextDim
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
							l := material.Label(ui.th, unit.Sp(11), text)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !missing {
								return layout.Dimensions{}
							}
							l := material.Label(ui.th, unit.Sp(9), missingLabel)
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
func (ui *uiState) layoutUnitRightColumnBody(gtx layout.Context) layout.Dimensions {
	sec := ui.currentUnitSection()
	var rows []unitWeaponRow
	var fields []unitField
	switch sec.label {
	case "Unit info":
		return ui.layoutUnitFieldPanel(gtx, sec)
	case "Weapons":
		d := ui.currentUnitDetails()
		rows = unitWeaponRows(d)
		if len(rows) > 0 {
			idx := clampInt(ui.unitSelRow, 0, len(rows)-1)
			if w := unitWeaponAt(d, idx); w != nil {
				// The weapon's own file is the detail worth showing; the
				// slot's scalars are only a fallback for a reference that
				// does not resolve.
				fields = w.rgdFields
				if len(fields) == 0 {
					fields = w.fields
				}
			}
		}
	case "Abilities":
		d := ui.currentUnitDetails()
		rows = unitAbilityRows(d)
	case "Squads":
		return ui.layoutUnitSquadRightColumn(gtx, sec)
	case unitSectionEvents:
		return ui.layoutUnitEventPanel(gtx)
	case unitSectionViewport:
		return ui.layoutUnitModelSide(gtx)
	default:
		return ui.unitColumn(gtx, sec.rightTitle, "Selected entry properties will appear here.")
	}
	if len(rows) == 0 {
		return ui.unitColumn(gtx, sec.rightTitle, "No entry is selected.")
	}
	idx := clampInt(ui.unitSelRow, 0, len(rows)-1)
	row := rows[idx]
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, "Slot", row.hardpoint, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, "Name", row.name, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								// The unit writes a .lua because the game
								// refers to the .rgd it converts in memory;
								// what is shown is the file that exists.
								refValue := row.ref
								if row.found {
									refValue = row.path
								}
								return ui.unitKeyValueRow(gtx, "Reference", refValue, false)
							})
						}),
					}
					if !row.found {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(10), "The weapon file was not found in this mod.")
								l.Color = rgb(0xC98A8A)
								return l.Layout(gtx)
							})
						}))
					}
					for _, f := range fields {
						f := f
						if f.startsGroup && f.group != "" {
							group := f.group
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return ui.unitGroupHeading(gtx, group)
							}))
						}
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, f.label, f.value, false)
							})
						}))
					}
					if len(fields) == 0 {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(10), "No readable settings for this entry.")
								l.Color = cTextDim
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

// unitGroupHeading is the dimmed, semibold heading above the first row of a
// group in a properties column — the same shape the info list draws, so a
// grouped column reads the same wherever it appears.
func (ui *uiState) unitGroupHeading(gtx layout.Context, text string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.th, unit.Sp(11), text)
		l.Color = cTextDim
		l.Font.Weight = font.SemiBold
		l.MaxLines = 1
		return l.Layout(gtx)
	})
}

// unitWeaponAt is the weapon a row of the Weapons list stands for, matching
// the row back to the hardpoint and slot it came from.
func unitWeaponAt(d *unitDetails, rowIndex int) *weaponSlot {
	if d == nil {
		return nil
	}
	seen := 0
	for hi := range d.weapons {
		for si := range d.weapons[hi].weapons {
			if seen == rowIndex {
				return &d.weapons[hi].weapons[si]
			}
			seen++
		}
	}
	return nil
}

// unitWeaponSlotFields finds the scalar fields of the weapon a row stands for,
// matching the row back to the hardpoint and slot it came from.
func unitWeaponSlotFields(d *unitDetails, rowIndex int) []unitField {
	if w := unitWeaponAt(d, rowIndex); w != nil {
		return w.fields
	}
	return nil
}

func (ui *uiState) ensureUnitRowClicks(n int) {
	if len(ui.unitRowClicks) != n {
		ui.unitRowClicks = make([]widget.Clickable, n)
	}
}

// ── input ───────────────────────────────────────────────────────────────────

// unitPickerOpen reports whether either dropdown is showing.
func (ui *uiState) unitPickerOpen() bool { return ui.unitRaceOpen || ui.unitListOpen }

// closeUnitPickers puts both dropdowns away.
func (ui *uiState) closeUnitPickers() {
	ui.unitRaceOpen = false
	ui.unitListOpen = false
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
func (ui *uiState) handleUnitPickerInput(gtx layout.Context) {
	// The triggers toggle their own list and put the other one away, so
	// clicking Race while the unit list is open swaps them rather than
	// stacking two lists over the columns.
	if ui.unitRaceTrigger.Clicked(gtx) {
		ui.unitRaceOpen = !ui.unitRaceOpen
		ui.unitListOpen = false
		return
	}
	if ui.unitListTrigger.Clicked(gtx) {
		ui.unitListOpen = !ui.unitListOpen
		ui.unitRaceOpen = false
		return
	}
	// The new object buttons sit with the pickers, so a dropdown that is open is
	// put away before the dialog goes up rather than over it.
	if ui.unitNewTrigger.Clicked(gtx) {
		ui.openNewObjDialog(newObjUnit)
		return
	}
	if ui.unitBuildingTrigger.Clicked(gtx) {
		ui.openNewObjDialog(newObjBuilding)
		return
	}
	// The freeze buttons are in the same strip, so they are read here with the
	// rest of it. A dropdown that is open is put away first, for the same
	// reason: the dialog and the lists are all one strip.
	ui.handleFreezeInput(gtx)
	if ui.unitListOpen {
		race := ui.currentRace()
		if race == nil {
			ui.unitListOpen = false
			return
		}
		units := race.unitsOf()
		ui.ensureUnitDropdownClicks(len(units))
		for i := range units {
			if i < len(ui.unitClicks) && ui.unitClicks[i].Clicked(gtx) {
				ui.unitSel = i
				ui.unitListOpen = false
				ui.unitSelRow = 0
				ui.statusText = "Unit: " + ui.unitLabel(units[i])
				// The pickers are part of the saved session, so that the
				// Object window comes back on the unit it was left on.
				ui.markStateDirty()
				return
			}
		}
	}
	if ui.unitRaceOpen {
		cat := ui.unitCatalogFor()
		if cat == nil {
			ui.unitRaceOpen = false
			return
		}
		ui.ensureUnitRaceClicks(len(cat.races))
		for i := range cat.races {
			if i < len(ui.unitRaceClicks) && ui.unitRaceClicks[i].Clicked(gtx) {
				ui.unitRaceSel = i
				ui.unitRaceOpen = false
				// The unit list belongs to the race, so a new race starts
				// at its first unit.
				ui.unitSel = 0
				ui.unitSelRow = 0
				ui.statusText = "Race: " + cat.races[i].name
				ui.markStateDirty()
				return
			}
		}
	}
	// A click anywhere else puts an open list away. The catcher is laid over
	// the columns but under the strip and the lists, so it only ever sees the
	// clicks that landed on neither.
	if ui.unitPickerOpen() && ui.unitDismissClick.Clicked(gtx) {
		ui.closeUnitPickers()
	}
}

// handleUnitPickerKeys puts an open list away on Escape. The key is only read
// while a list is showing, so Escape keeps meaning "close" everywhere else.
func (ui *uiState) handleUnitPickerKeys(gtx layout.Context) {
	if !ui.unitPickerOpen() {
		return
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if e.State == key.Press {
			ui.closeUnitPickers()
			return
		}
	}
}

// handleUnitRowInput owns the selectable rows of the populated columns.
func (ui *uiState) handleUnitRowInput(gtx layout.Context) {
	sec := ui.currentUnitSection()
	var rows []unitWeaponRow
	switch sec.label {
	case "Weapons":
		rows = unitWeaponRows(ui.currentUnitDetails())
	case "Abilities":
		rows = unitAbilityRows(ui.currentUnitDetails())
	case "Frozen":
		// The rows of this list are the way back into the mod, so a press on
		// one acts on it rather than selecting it.
		ui.handleFrozenRowInput(gtx)
		return
	default:
		return
	}
	ui.ensureUnitRowClicks(len(rows))
	for i := range rows {
		if i < len(ui.unitRowClicks) && ui.unitRowClicks[i].Clicked(gtx) {
			ui.unitSelRow = i
			return
		}
	}
}
