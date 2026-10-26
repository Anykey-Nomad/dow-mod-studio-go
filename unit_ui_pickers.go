package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dow-mod-studio/internal/rgd"
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
	mod := ui.modTree.FileMapName + "|" + ui.modTree.ModFilePath
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
	for i := range ui.unitCatalog.Races {
		r := &ui.unitCatalog.Races[i]
		if race != "" && !strings.EqualFold(r.Name, race) {
			continue
		}
		ui.unitRaceSel = i
		if path == "" {
			return
		}
		units := r.UnitsOf()
		for j := range units {
			if strings.EqualFold(units[j].Path, path) {
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
	return ui.unitCatalogFor().SelectedUnit(ui.unitRaceSel, ui.unitSel)
}

// currentRace is the race the dialog is showing, or nil when there is none.
func (ui *uiState) currentRace() *raceEntry {
	return ui.unitCatalogFor().SelectedRace(ui.unitRaceSel)
}

// currentUnitDetails parses the selected unit once and caches it, so switching
// between the sections does not re-read and re-parse the file each time.
func (ui *uiState) currentUnitDetails() *unitDetails {
	u := ui.currentUnit()
	if u == nil {
		return nil
	}
	if ui.unitDetails != nil && strings.EqualFold(ui.unitDetailsPath, u.Path) {
		return ui.unitDetails
	}
	d := loadUnitDetails(ui.modTree, *u)
	ui.unitDetails = d
	ui.unitDetailsPath = u.Path
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
							return ui.unitPickerField(gtx, "Race", raceLabel, cat != nil && len(cat.Races) > 0, ui.unitRaceOpen)
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
		return r.Name
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
	return ui.unitLabel(*u) + "  (" + u.Kind.Label + ")"
}

func raceCountLabel(r *raceEntry) string {
	if r == nil {
		return ""
	}
	return itoaCount(r.UnitCount(), "unit", "units")
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
	if cat == nil || len(cat.Races) == 0 {
		return layout.Dimensions{}
	}
	ui.ensureUnitRaceClicks(len(cat.Races))
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
			rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var children []layout.FlexChild
				for i := range cat.Races {
					i := i
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sel := i == cat.ClampRaceSel(ui.unitRaceSel)
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
												l := material.Label(ui.th, unit.Sp(11), cat.Races[i].Name)
												l.Color = cText
												l.MaxLines = 1
												return l.Layout(gtx)
											}),
											layout.Flexed(1, flexGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(10), itoaPlain(cat.Races[i].UnitCount()))
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
	units := race.UnitsOf()
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
					sel := index == race.ClampUnitSel(ui.unitSel)
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
											l := material.Label(ui.th, unit.Sp(10), u.Kind.Label)
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
	case unitSectionClassicOE:
		// Rendered here as well as in layoutUnitBody, the same way Viewport
		// is: laying this column out on its own still has to show the
		// editor's panels rather than fall through to a hint.
		return ui.layoutClassicOEArea(gtx)
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
	if d != nil && d.Error != "" {
		msg = "Could not read this unit:\n" + d.Error
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
	if d.Error != "" {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return ui.unitEmptyState(gtx)
		})
	}
	if len(d.Fields) == 0 {
		return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "This unit declares no readable properties.")
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		})
	}
	ui.ensureUnitInfoClicks(len(d.Fields))
	return ui.unitSectionFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(ui.layoutUnitInfoIconHeader),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutListWithScrollbar(gtx, &ui.unitInfoScroll, &ui.unitInfoList, unitInfoRowHeightDp, len(d.Fields), func(gtx layout.Context, index int) layout.Dimensions {
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
						l := material.Label(ui.th, unit.Sp(9), unitFileLabel(*u)+"  ·  "+u.Kind.Label)
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
	if d == nil || d.Error != "" || len(d.Weapons) == 0 {
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
	for _, h := range d.Weapons {
		for _, w := range h.Weapons {
			out = append(out, unitWeaponRow{hardpoint: h.Hardpoint, name: w.Name, ref: w.Ref, path: w.Path, found: w.Path != ""})
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
	for _, a := range d.Abilities {
		if a.Ref == "" {
			continue
		}
		out = append(out, unitWeaponRow{hardpoint: a.Slot, name: a.Name(), ref: a.Ref, path: a.Path, found: a.Resolved()})
	}
	return out
}

// layoutUnitAbilityList is the "Abilities" column.
func (ui *uiState) layoutUnitAbilityList(gtx layout.Context) layout.Dimensions {
	d := ui.currentUnitDetails()
	if d == nil || d.Error != "" {
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
				fields = w.RgdFields
				if len(fields) == 0 {
					fields = w.Fields
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
	case unitSectionClassicOE:
		// The classic editor had no separate properties column of its own
		// here; the section draws left panel and preview across both
		// columns, so this branch is only for the column drawn alone.
		return ui.layoutClassicOEPreview(gtx)
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
						if f.StartsGroup && f.Group != "" {
							group := f.Group
							children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return ui.unitGroupHeading(gtx, group)
							}))
						}
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitKeyValueRow(gtx, f.Label, f.Value, false)
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
	for hi := range d.Weapons {
		for si := range d.Weapons[hi].Weapons {
			if seen == rowIndex {
				return &d.Weapons[hi].Weapons[si]
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
		return w.Fields
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
		units := race.UnitsOf()
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
		ui.ensureUnitRaceClicks(len(cat.Races))
		for i := range cat.Races {
			if i < len(ui.unitRaceClicks) && ui.unitRaceClicks[i].Clicked(gtx) {
				ui.unitRaceSel = i
				ui.unitRaceOpen = false
				// The unit list belongs to the race, so a new race starts
				// at its first unit.
				ui.unitSel = 0
				ui.unitSelRow = 0
				ui.statusText = "Race: " + cat.Races[i].Name
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

// ─── merged from unit_import.go ───────────────────────────────────────────────

// This file is the "Import frame's timeline" keyframe clipboard: a second
// .whm is read for the keys its clips hold, the panel lists those clips and
// the frames the picked bone has keys on, and a multi-selection of those
// frames is copied or written over the picked bone's track in the clip on
// show. The two files are assumed to carry the same skeleton — the match is
// by bone name — so a frame's keys are the same pose in both.

// unitImpRef names one frame of one animation of the imported file: which
// clip it belongs to and which frame it stands on. The selection is keyed
// this way because several clips can be open at once and a frame number on
// its own says which of them it came from.
type unitImpRef struct {
	anim  int
	frame int
}

// openUnitImportDialog shows the file dialog over the module's .whm files; a
// path typed in full or a file from the list is read when the dialog is
// confirmed.
func (ui *uiState) openUnitImportDialog() {
	ui.openFilePicker(pickConfig{
		title:    "Import frame's timeline",
		subtitle: "Choose the .whm to read keyframes from — a path, or a file from the module",
		hint:     `Data\art\ebps\...\model.whm`,
		exts:     []string{".whm"},
		modTree:  true,
		onPick: func(path string) (bool, string) {
			before := ui.unitImpModel
			ui.unitImportRead(path)
			if ui.unitImpModel == before {
				return false, ui.unitImpStatus
			}
			return true, ""
		},
	})
}

// unitImportRead loads the picked .whm from disk or from the module and
// keeps its parsed model for the panel to browse. Nothing of the source is
// written back: it is a clipboard, read once per dialog confirmation.
func (ui *uiState) unitImportRead(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		ui.unitImpStatus = "No file was chosen"
		return
	}
	var data []byte
	var err error
	if filepath.IsAbs(path) {
		data, err = os.ReadFile(path)
	} else if ui.modTree != nil {
		data, err = ui.modTree.ReadFile(path)
		if err != nil {
			// A bare file name may still be on disk beside the module.
			if data2, err2 := os.ReadFile(path); err2 == nil {
				data, err = data2, nil
			}
		}
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		ui.unitImpStatus = "Read failed: " + err.Error()
		LogInfo("bone: import: read %q failed: %v", path, err)
		return
	}
	m, perr := ParseWhmModel(data)
	if perr != nil {
		ui.unitImpStatus = "Not a .whm: " + perr.Error()
		LogInfo("bone: import: parse %q failed: %v", path, perr)
		return
	}
	ui.unitImpModel = m
	ui.unitImpPath = path
	ui.unitImpOpen = make([]bool, len(m.Animations))
	ui.unitImpAnimClicks = make([]widget.Clickable, len(m.Animations))
	ui.unitImpFrameClicks = make(map[unitImpRef]*widget.Clickable)
	ui.unitImpSel = make(map[unitImpRef]bool)
	ui.unitImpAnchor = unitImpRef{anim: -1}
	ui.unitImpStatus = fmt.Sprintf("Imported %s — %d clip(s)", filepath.Base(path), len(m.Animations))
	LogInfo("bone: import: loaded %q (%d animations, %d bones)", path, len(m.Animations), len(m.Bones))
}

// unitImportBoneIndex is the source file's bone matching the picked bone of
// the edited model, by name — the two files are assumed to carry the same
// skeleton, which is the premise of the whole feature. -1 means no match.
func (ui *uiState) unitImportBoneIndex(v *modelViewer) int {
	if ui.unitImpModel == nil || v == nil || v.model == nil || v.boneIndex < 0 || v.boneIndex >= len(v.model.Bones) {
		return -1
	}
	name := v.model.Bones[v.boneIndex].Name
	if name == "" {
		return -1
	}
	return ui.unitImpModel.boneIndexByName(name)
}

// unitImportFrames lists the frames the matched bone has keys on in one
// source clip: position and rotation keys together, in frame order, which
// is the same walk the panel's own keyframe list takes over a track. A
// frame carrying only one of the two kinds is still a frame worth copying —
// the note on its row says which kind it holds.
func unitImportFrames(a *WhmAnimation, bone int) []int {
	if a == nil || bone < 0 {
		return nil
	}
	var track *WhmTrack
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			track = &a.Bones[i]
			break
		}
	}
	if track == nil {
		return nil
	}
	seen := make(map[int]bool)
	for i := range track.PosKey {
		seen[frameAt(float64(track.PosKey[i].Time), a.NumFrames)] = true
	}
	for i := range track.RotKey {
		seen[frameAt(float64(track.RotKey[i].Time), a.NumFrames)] = true
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]int, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Ints(out)
	return out
}

// unitImportFrameKinds says which key kinds a source frame carries for the
// matched bone, as the row's note: "pos+rot", "pos" or "rot".
func unitImportFrameKinds(a *WhmAnimation, bone, frame int) string {
	if a == nil || bone < 0 {
		return ""
	}
	var track *WhmTrack
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			track = &a.Bones[i]
			break
		}
	}
	if track == nil {
		return ""
	}
	pos, rot := false, false
	for i := range track.PosKey {
		if frameAt(float64(track.PosKey[i].Time), a.NumFrames) == frame {
			pos = true
			break
		}
	}
	for i := range track.RotKey {
		if frameAt(float64(track.RotKey[i].Time), a.NumFrames) == frame {
			rot = true
			break
		}
	}
	switch {
	case pos && rot:
		return "pos+rot"
	case pos:
		return "pos"
	case rot:
		return "rot"
	}
	return ""
}

// unitImportRowClick returns the row's click target, allocating it the
// first time a frame is shown. The map rather than a slice: frames appear
// and vanish as clips are opened, and a click target has to stay the same
// widget between frames for a press to complete.
func (ui *uiState) unitImportRowClick(ref unitImpRef) *widget.Clickable {
	if ui.unitImpFrameClicks == nil {
		ui.unitImpFrameClicks = make(map[unitImpRef]*widget.Clickable)
	}
	c := ui.unitImpFrameClicks[ref]
	if c == nil {
		c = &widget.Clickable{}
		ui.unitImpFrameClicks[ref] = c
	}
	return c
}

// unitImportRowClicked is a press on a frame row: a plain click picks that
// frame alone, Ctrl adds or takes it away, Shift picks every frame of the
// same clip between the anchor and this one. Shift only spans one clip —
// a range across two clips has no timeline to be a range on.
func (ui *uiState) unitImportRowClicked(ref unitImpRef, frames []int, mods key.Modifiers) {
	if ui.unitImpSel == nil {
		ui.unitImpSel = make(map[unitImpRef]bool)
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	switch {
	case shift && ui.unitImpAnchor.anim == ref.anim && ui.unitImpAnchor.anim >= 0:
		lo, hi := -1, -1
		for i, f := range frames {
			if f == ui.unitImpAnchor.frame {
				lo = i
			}
			if f == ref.frame {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			ui.unitImpSel = map[unitImpRef]bool{ref: true}
			ui.unitImpAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.unitImpSel = make(map[unitImpRef]bool, hi-lo+1)
		for _, f := range frames[lo : hi+1] {
			ui.unitImpSel[unitImpRef{anim: ref.anim, frame: f}] = true
		}
		LogInfo("bone: import: Shift+click spans %d frame(s) of clip %d", hi-lo+1, ref.anim)
	case ctrl:
		if ui.unitImpSel[ref] {
			delete(ui.unitImpSel, ref)
		} else {
			ui.unitImpSel[ref] = true
			ui.unitImpAnchor = ref
		}
	default:
		ui.unitImpSel = map[unitImpRef]bool{ref: true}
		ui.unitImpAnchor = ref
	}
}

// unitImportApply writes the selected source frames into the picked bone's
// track of the clip on show. With replace the key on the frame is
// overwritten and a missing one is created; without it only the missing
// ones are created, so a first pass fills gaps and a second overwrites.
// Frame numbers are mapped by frame number, clamped to the clip's length —
// the two files are assumed to carry clips of the same length, which is
// what "analogous set of bones" means in practice.
func (ui *uiState) unitImportApply(v *modelViewer, replace bool) (added, replaced, skipped int) {
	what := "copy"
	if replace {
		what = "replace"
	}
	if ui.unitImpModel == nil {
		ui.unitBoneKeyErr = "no file imported"
		return 0, 0, 0
	}
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return 0, 0, 0
	}
	if v.boneIndex < 0 || v.boneIndex >= len(v.model.Bones) {
		ui.unitBoneKeyErr = "no bone is picked"
		return 0, 0, 0
	}
	src := ui.unitImportBoneIndex(v)
	if src < 0 {
		ui.unitBoneKeyErr = "the imported file has no bone named " + v.model.Bones[v.boneIndex].Name
		return 0, 0, 0
	}
	// The refs are walked in a fixed order — clip then frame — so the
	// result does not depend on map iteration.
	refs := make([]unitImpRef, 0, len(ui.unitImpSel))
	for ref := range ui.unitImpSel {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].anim != refs[j].anim {
			return refs[i].anim < refs[j].anim
		}
		return refs[i].frame < refs[j].frame
	})
	if len(refs) == 0 {
		ui.unitBoneKeyErr = "no frame is picked"
		return 0, 0, 0
	}
	t := v.boneTrack()
	if t == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return 0, 0, 0
	}
	// The keys written below are also what the copy history keeps: a Copy or
	// Replace records the source keys it took, so they can be found again.
	var keys boneKeyClip
	for _, ref := range refs {
		if ref.anim < 0 || ref.anim >= len(ui.unitImpModel.Animations) {
			skipped++
			continue
		}
		sa := &ui.unitImpModel.Animations[ref.anim]
		var st *WhmTrack
		for i := range sa.Bones {
			if sa.Bones[i].Bone == src {
				st = &sa.Bones[i]
				break
			}
		}
		if st == nil {
			skipped++
			continue
		}
		frame := ref.frame
		if frame < 0 {
			skipped++
			continue
		}
		if frame >= v.anim.NumFrames {
			// The source clip runs longer than the one on show:
			// the frame has nowhere to land.
			skipped++
			continue
		}
		// Position keys the source frame carries.
		for i := range st.PosKey {
			if frameAt(float64(st.PosKey[i].Time), sa.NumFrames) != frame {
				continue
			}
			keys.Pos = append(keys.Pos, st.PosKey[i])
			a, r := writeImportPos(t, v, frame, st.PosKey[i].V, replace)
			added += a
			replaced += r
		}
		// Rotation keys, the same way.
		for i := range st.RotKey {
			if frameAt(float64(st.RotKey[i].Time), sa.NumFrames) != frame {
				continue
			}
			keys.Rot = append(keys.Rot, st.RotKey[i])
			a, r := writeImportRot(t, v, frame, st.RotKey[i].V, replace)
			added += a
			replaced += r
		}
	}
	v.poseValid = false
	v.invalidate()
	if added+replaced > 0 {
		ui.unitModelWhmDirty = true
		ui.unitBoneKeyErr = ""
		ui.unitClips.Push(clipEntry{
			Kind:  clipImportKeys,
			Label: fmt.Sprintf("%d key(s) from %s", len(keys.Pos)+len(keys.Rot), filepath.Base(ui.unitImpPath)),
			At:    time.Now(),
			Keys:  &keys,
		})
		ui.fillBoneKeyFields(v, 0)
		ui.fillBoneKeyFields(v, 1)
	} else if ui.unitBoneKeyErr == "" {
		ui.unitBoneKeyErr = "nothing to write"
	}
	LogInfo("bone: import %s: %d key(s) added, %d replaced, %d skipped (bone=%q clip=%q)",
		what, added, replaced, skipped, t.Name, v.anim.Name)
	return added, replaced, skipped
}

// writeImportPos puts one source position value on the frame of the picked
// bone's track. It reports whether a key was created and whether an
// existing one was written over.
func writeImportPos(t *WhmTrack, v *modelViewer, frame int, val [3]float32, replace bool) (added, replaced int) {
	if idx := v.boneKeyIndexAtFrame(frame, 0); idx >= 0 {
		if !replace {
			return 0, 0
		}
		t.PosKey[idx].V = val
		return 0, 1
	}
	now := timeForFrame(frame, v.anim.NumFrames)
	at := 0
	for at < len(t.PosKey) && t.PosKey[at].Time <= now {
		at++
	}
	t.PosKey = append(t.PosKey, WhmKey3{})
	copy(t.PosKey[at+1:], t.PosKey[at:])
	t.PosKey[at] = WhmKey3{Time: now, V: val}
	return 1, 0
}

// writeImportRot is writeImportPos for rotation keys.
func writeImportRot(t *WhmTrack, v *modelViewer, frame int, val [4]float32, replace bool) (added, replaced int) {
	if idx := v.boneKeyIndexAtFrame(frame, 1); idx >= 0 {
		if !replace {
			return 0, 0
		}
		t.RotKey[idx].V = val
		return 0, 1
	}
	now := timeForFrame(frame, v.anim.NumFrames)
	at := 0
	for at < len(t.RotKey) && t.RotKey[at].Time <= now {
		at++
	}
	t.RotKey = append(t.RotKey, WhmKey4{})
	copy(t.RotKey[at+1:], t.RotKey[at:])
	t.RotKey[at] = WhmKey4{Time: now, V: val}
	return 1, 0
}

// unitImportSection is the panel under the keyframe list: the imported
// file's clips, each opening onto the frames the picked bone has keys on,
// and the two buttons that write a multi-selection into the clip on show.
// With nothing imported it is not drawn at all, so the panel looks as it
// did before the feature existed.
func (ui *uiState) unitImportSection(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.unitImpModel == nil {
		return layout.Dimensions{}
	}
	// Clicks are drained before the buttons are laid out, the order the
	// pose bar uses: an update read after Layout has already been spent.
	copyClicked := ui.unitImpCopyClick.Clicked(gtx)
	replaceClicked := ui.unitImpReplaceClick.Clicked(gtx)
	if copyClicked {
		ui.unitImportApply(v, false)
	}
	if replaceClicked {
		ui.unitImportApply(v, true)
	}
	src := ui.unitImportBoneIndex(v)
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.th, "Imported · "+filepath.Base(ui.unitImpPath))
		}),
	}
	// The buttons sit directly under the header rather than after every clip
	// and frame: a model's imported file lists dozens of clips and hundreds of
	// frames, and at the far end of that the buttons were effectively
	// unreachable. With nothing picked they are drawn disabled rather than
	// hidden, so the row does not jump under the mouse.
	n := len(ui.unitImpSel)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &ui.unitImpCopyClick, "Copy", n == 0)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &ui.unitImpReplaceClick, "Replace", n == 0)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := fmt.Sprintf("%d picked", n)
						if n == 0 {
							txt = "Ctrl+click picks more frames"
						}
						l := material.Label(ui.th, unit.Sp(9), txt)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
	}))
	if src < 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			msg := "Select a bone to see its frames from the imported file."
			if v == nil || v.boneIndex < 0 || v.model == nil || v.boneIndex >= len(v.model.Bones) {
				// The message already says it; keep it short.
			} else {
				msg = "No bone named " + v.model.Bones[v.boneIndex].Name + " in the imported file."
			}
			l := material.Label(ui.th, unit.Sp(9), msg)
			l.Color = cTextDim
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	} else {
		for i := range ui.unitImpModel.Animations {
			i := i
			if i >= len(ui.unitImpOpen) {
				break
			}
			c := &ui.unitImpAnimClicks[i]
			if c.Clicked(gtx) {
				ui.unitImpOpen[i] = !ui.unitImpOpen[i]
			}
			a := &ui.unitImpModel.Animations[i]
			frames := unitImportFrames(a, src)
			note := fmt.Sprintf("%d frame(s)", len(frames))
			if len(frames) == 0 {
				note = "no keys for this bone"
			}
			open := ui.unitImpOpen[i]
			mark := "▸ "
			if open {
				mark = "▾ "
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitViewportRow(gtx, c, open, mark+a.Name, note)
			}))
			if !open {
				continue
			}
			if len(frames) == 0 {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(9), "This clip gives "+v.model.Bones[v.boneIndex].Name+" no keys.")
					l.Color = cTextDim
					return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
				}))
				continue
			}
			for _, f := range frames {
				f := f
				ref := unitImpRef{anim: i, frame: f}
				rc := ui.unitImportRowClick(ref)
				if rc.Clicked(gtx) {
					ui.unitImportRowClicked(ref, frames, ui.unitListModifiers())
				}
				selected := ui.unitImpSel[ref]
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitViewportRow(gtx, rc, selected,
						fmt.Sprintf("frame %d", f+1), unitImportFrameKinds(a, src, f))
				}))
			}
		}
	}
	if ui.unitBoneKeyErr != "" {
		msg := ui.unitBoneKeyErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), msg)
			l.Color = rgb(0xB00020)
			l.MaxLines = 1
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
