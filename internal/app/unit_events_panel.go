package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"strings"
)

// layoutUnitEventTimelines is the middle column: every DATAMTON timeline of
// the .whe, the selected one highlighted, and which clips each drives. The
// .whe's actions are drawn under that list — they are file-wide and their
// motion lines name these very timelines, so they belong beside the list of
// them rather than inside the events of whichever timeline is picked.
func LayoutUnitEventTimelines(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitEvents(ui)
	sec := CurrentUnitSection(ui)
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.MiddleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitEventTimelineList(ui, gtx) }),
		}
		if ui.UnitEvtDoc != nil {
			acts := ui.UnitEvtDoc.Actions()
			// The block also shows for a .whe that has no actions of its own
			// while one is on the clipboard: a copied action is the only way to
			// give such a unit its first one.
			_, _, hasClip := ui.UnitClips.Pick(clipActions, ui.UnitClipCur)
			if len(acts) > 0 || hasClip {
				children = append(children,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return LayoutUnitEvtActions(ui, gtx, acts)
					}),
				)
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutUnitEvtActions is the .whe's action list under the timelines list: the
// header, then one row per DATAACTS action, the picked one highlighted. What
// each action guards its choices with is not here — those conditions belong to
// one action at a time and are drawn in the Events column beside this list.
//
// The list is laid out after the timelines, so it is a rigid child — it claims
// its height first and the list keeps what is left. A .whe with many actions is
// taller than that room, so the block is measured at its natural height and
// capped at a share of the column when it does not fit: long action lists scroll
// inside their own region rather than squeezing the animations off the column.
func LayoutUnitEvtActions(ui *uiState, gtx layout.Context, acts []whe.FxAction) layout.Dimensions {
	EnsureUnitEvtActionClicks(ui, len(acts))
	SelectedUnitEvtAction(ui, acts)
	content := func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.Th, "Actions")
				})
			}),
			// An action moves between units the way a timeline does: one is
			// copied whole here and pastes into another unit's list.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitEvtCopyAct, "Copy action", len(acts) == 0)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							_, _, has := ui.UnitClips.Pick(clipActions, ui.UnitClipCur)
							return SmallButtonDisabled(ui, gtx, &ui.UnitEvtPasteAct, "Paste action", !has)
						}),
					)
				})
			}),
		}
		for i := range acts {
			i := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitEvtActionRow(ui, gtx, i, acts[i])
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	// Measure it the way panelScrollView measures its own content: against an
	// unbounded height with input off, so a block that fills the room it is
	// handed cannot pass the room off as its own height.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	natural := content(measureGTX).Size.Y
	if natural >= panelScrollMeasureHeight {
		measureGTX.Constraints.Max.Y = maxInt(1, gtx.Constraints.Max.Y)
		natural = content(measureGTX).Size.Y
	}
	macro.Stop()
	height := natural
	// Two fifths of the column is the most the block may claim, which leaves
	// the timelines list with the majority however many actions there are.
	if cap := gtx.Constraints.Max.Y * 2 / 5; cap > 0 && height > cap {
		height = cap
	}
	agtx := gtx
	agtx.Constraints.Min.Y = height
	agtx.Constraints.Max.Y = height
	return PanelScrollView(ui, agtx, &ui.UnitEvtActionsScroll, content)
}

// layoutUnitEvtActionRow is one action of the list: its name and how many
// choices it carries, the picked one highlighted. Pressing it shows that
// action's conditions in the Events column.
func LayoutUnitEvtActionRow(ui *uiState, gtx layout.Context, i int, a whe.FxAction) layout.Dimensions {
	if i < 0 || i >= len(ui.UnitEvtActionClicks) {
		return layout.Dimensions{}
	}
	c := &ui.UnitEvtActionClicks[i]
	selected := i == ui.UnitEvtActionSel
	bg := uikit.CPanelBg
	switch {
	case selected:
		bg = uikit.CSelectBg
	case c.Hovered():
		bg = uikit.CMenuHover
	}
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
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
							l := material.Label(ui.Th, unit.Sp(10), unitViewportRowText(a.Name))
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							note := fmt.Sprintf("%d choices", len(a.Motions)+len(a.Subactions))
							l := material.Label(ui.Th, unit.Sp(9), note)
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

func LayoutUnitEventTimelineList(ui *uiState, gtx layout.Context) layout.Dimensions {
	if ui.UnitEvtDoc == nil {
		return UnitEventsMessage(ui, gtx, ui.UnitEvtErr)
	}
	tls := ui.UnitEvtDoc.Timelines()
	if len(tls) == 0 {
		return UnitEventsMessage(ui, gtx, "This .whe carries no animation timelines.")
	}
	selected := SelectedUnitEventTimeline(ui)
	selEvents := 0
	if selected != nil {
		selEvents = len(selected.Events)
	}
	EnsureUnitEventClicks(ui, len(tls), selEvents)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := ui.UnitEvtPath
			if ui.UnitEvtDirty {
				text += "  \u2022 unsaved"
			}
			l := material.Label(ui.Th, unit.Sp(9), text)
			l.Color = uikit.CTextDim
			l.MaxLines = 2
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}),
		// A timeline is copied whole from here — its events, its clips and the
		// definitions they name — and pastes into the timeline of the same name
		// in any unit, which is what carries one unit's effects onto another.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitEvtCopyTl, "Copy timeline", selected == nil)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), "copies it whole, for another unit")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						})
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return LayoutListWithScrollbar(ui, gtx, &ui.UnitEvtScroll, &ui.UnitEvtList, unitRowHeightDp-4, len(tls), func(gtx layout.Context, i int) layout.Dimensions {
				return LayoutUnitEventTimelineRow(ui, gtx, i, tls[i])
			})
		}),
	)
}

// layoutUnitEventPanel is the right column. It carries the tools of whatever
// the reader is working on: the sound transport, the selected timeline's
// events and the form that edits one while a timeline is picked, and the
// picked action's conditions while an action is — never both tool sets at
// once, since an action carries no timeline and no event of its own.
func LayoutUnitEventPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitEvents(ui)
	sec := CurrentUnitSection(ui)
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		}
		if !ui.UnitEvtFocusAction {
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitEvtTransport(ui, gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			)
		}
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return PanelScrollView(ui, gtx, &ui.UnitEvtPanelScroll, func(gtx layout.Context) layout.Dimensions {
				return UnitEventPanelBody(ui, gtx)
			})
		}))
		if !ui.UnitEvtFocusAction {
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitEventPanelForm(ui, gtx)
				}),
			)
		} else {
			// The action panel carries the same commit and file pair the
			// timeline form does, so an action's conditions are written the
			// way a timeline's events are.
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitActPanelFooter(ui, gtx)
				}),
			)
		}
		// The mesh list is pinned below both editors, so it stays on screen
		// whether a timeline or an action is being worked on.
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitMeshVisibilitySection(ui, gtx)
			}),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// unitEventPanelBody is the scrollable content of the right column.
func UnitEventPanelBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	if ui.UnitEvtDoc == nil {
		return UnitEventsMessage(ui, gtx, ui.UnitEvtErr)
	}
	inset := layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}
	tl := SelectedUnitEventTimeline(ui)
	var children []layout.FlexChild
	// The panel carries one editor at a time. While an action is being worked
	// on it shows that action's conditions and none of the timeline's events,
	// and the other way round: stacking the two put both editors in one panel
	// and left the reader hunting for the one they were in.
	if ui.UnitEvtFocusAction {
		children = UnitEvtActionPanelBody(ui, gtx)
	} else if tl == nil {
		msg := ui.UnitEvtErr
		if msg == "" {
			msg = "Select a timeline in the middle column."
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitEventsMessage(ui, gtx, msg)
		}))
		// A copied timeline needs no target picked: it carries its own name, so
		// a .whe whose timelines do not include it can still receive it whole.
		if entry, _, ok := ui.UnitClips.Pick(clipEvents, ui.UnitClipCur); ok && entry.Events != nil && entry.Events.Timeline != "" {
			name := entry.Events.Timeline
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitEvtPaste, "Paste timeline "+name)
				})
			}))
		}
	} else {
		// The body below this point belongs to the selected timeline: its header,
		// the motion's own properties, its event rows and the selected event's
		// definition.
		// Which file and timeline this is, and whether it has unsaved changes.
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := "Timeline  " + tl.Name
			if ui.UnitEvtDirty {
				text += "  • unsaved"
			}
			l := material.Label(ui.Th, unit.Sp(9), text)
			l.Color = uikit.CTextDim
			l.MaxLines = 2
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}))
		// The clips this motion blends, one row each with its own Remove
		// button — the leading strings of the DATAMTON payload, and the thing
		// a reader most often wants to prune. Edited in memory only: Save .whe
		// writes the change into the mod.
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.Th, "Animations")
			})
		}))
		EnsureUnitEventAnimClicks(ui, len(tl.Animations))
		if len(tl.Animations) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "This motion blends no animations — it chains to other motions instead.")
				l.Color = uikit.CTextDim
				l.MaxLines = 2
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
			}))
		} else {
			for i := range tl.Animations {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(10), tl.Animations[i])
								l.Color = uikit.CText
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return SmallButton(ui, gtx, &ui.UnitEvtAnimClicks[i], "Remove")
							}),
						)
					})
				}))
			}
		}
		// How the motion itself plays: the tail of the DATAMTON payload, which
		// the game reads for looping, delays and transitions even though the
		// events are what a reader edits most.
		if tl.Tail.Present {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.Th, "Motion")
				})
			}))
			for _, row := range whe.MotionTailRows(tl.Tail) {
				row := row
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return UnitKeyValueRow(ui, gtx, row[0], row[1], false)
					})
				}))
			}
		}
		EnsureUnitEventClicks(ui, len(ui.UnitEvtDoc.Timelines()), len(tl.Events))
		defText := "No events on this timeline yet. Add one below."
		if len(tl.Events) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), defText)
				l.Color = uikit.CTextDim
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
			}))
		} else {
			defs := ui.UnitEvtDoc.Events()
			for i := range tl.Events {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitEventRow(ui, gtx, i, tl.Events[i], defs)
				}))
			}
		}
	}
	dims := inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	return dims
}

// updateUnitEvtEntryEditors drains the value fields: Enter writes the typed
// value into the document and lets go of the field, the same way the bone
// key fields do.
func UpdateUnitEvtEntryEditors(ui *uiState, gtx layout.Context, name string) {
	for i := range ui.UnitEvtEntryEds {
		for {
			ev, ok := ui.UnitEvtEntryEds[i].Update(gtx)
			if !ok {
				break
			}
			if _, submit := ev.(widget.SubmitEvent); submit {
				ApplyUnitEvtEntry(ui, name, i, ui.UnitEvtEntryEds[i].Text())
				ui.UnitEvtEntryEds[i].Blur(gtx)
			}
		}
	}
}

// applyUnitEvtEntry writes one typed value into the definition in memory —
// the DATAEVNT payload is rebuilt, the document is marked dirty, and the
// viewport re-resolves from the document so the change is on screen before
// the file is saved.
func ApplyUnitEvtEntry(ui *uiState, name string, index int, value string) {
	if ui.UnitEvtDoc == nil {
		ui.StatusText = "No .whe is loaded"
		return
	}
	if err := ui.UnitEvtDoc.SetEventEntry(name, index, strings.TrimSpace(value)); err != nil {
		ui.StatusText = "Edit failed: " + err.Error()
		return
	}
	ui.UnitEvtDirty = true
	// The field takes back what was stored, so what is shown is what the chunk
	// now holds rather than what was typed — and the fill text moves with it,
	// so the axis counts as untouched until it is changed again.
	if def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(name)]; ok && index < len(def.Entries) && index < len(ui.UnitEvtEntryEds) {
		ui.UnitEvtEntryEds[index].SetText(def.Entries[index].Value)
		if index < len(ui.UnitEvtEntryFillText) {
			ui.UnitEvtEntryFillText[index] = strings.TrimSpace(def.Entries[index].Value)
		}
	}
	ui.StatusText = "Edited " + name + " — Save .whe to write it into the mod"
	ReloadUnitModelFx(ui)
}

// setUnitEvtEntryValues applies every value field the user typed over, the
// way the bone panel's Set does: Enter in a field is meant to do the same,
// but a button does not depend on the field having focus. Fields left as they
// were filled are skipped, and the viewport re-resolves once at the end.
func SetUnitEvtEntryValues(ui *uiState, name string) {
	if ui.UnitEvtDoc == nil {
		ui.StatusText = "No .whe is loaded"
		return
	}
	def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(name)]
	if !ok {
		ui.StatusText = "No definition of " + name + " in this .whe"
		return
	}
	changed := 0
	for i := range def.Entries {
		if i >= len(ui.UnitEvtEntryEds) {
			break
		}
		text := strings.TrimSpace(ui.UnitEvtEntryEds[i].Text())
		if i < len(ui.UnitEvtEntryFillText) && text == ui.UnitEvtEntryFillText[i] {
			continue
		}
		if err := ui.UnitEvtDoc.SetEventEntry(name, i, text); err != nil {
			ui.StatusText = "Edit failed: " + err.Error()
			if changed > 0 {
				ReloadUnitModelFx(ui)
			}
			return
		}
		ui.UnitEvtDirty = true
		if i < len(ui.UnitEvtEntryFillText) {
			ui.UnitEvtEntryFillText[i] = text
		}
		ui.UnitEvtEntryEds[i].SetText(text)
		changed++
	}
	if changed == 0 {
		ui.StatusText = "no values were changed"
		return
	}
	ui.StatusText = "Edited " + name + " — Save .whe to write it into the mod"
	ReloadUnitModelFx(ui)
}

// reloadUnitModelFx re-resolves the unit viewport's effects and sounds from
// the edited .whe document, so a change made in the Events section shows on
// the model before it is saved. The document is only handed over when it is
// the same file the viewport's model sits beside — a .whe read for another
// reason would put its events on this model.
func ReloadUnitModelFx(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil {
		return
	}
	var doc *whe.Document
	if ui.UnitEvtDoc != nil && ui.UnitEvtPath != "" && v.ModelFile != "" {
		base := func(p string) string {
			if j := strings.LastIndexAny(p, "/\\"); j >= 0 {
				return p[j+1:]
			}
			return p
		}
		name := base(v.ModelFile)
		if k := strings.LastIndexByte(name, '.'); k >= 0 {
			name = name[:k]
		}
		if strings.EqualFold(base(ui.UnitEvtPath), name+".whe") {
			doc = ui.UnitEvtDoc
		}
	}
	FxReload(ui, v, doc)
}

// unitEventPanelForm draws the static input form and action buttons at the bottom of the right column.
func UnitEventPanelForm(ui *uiState, gtx layout.Context) layout.Dimensions {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil {
		return layout.Dimensions{}
	}
	var children []layout.FlexChild

	// The add form.
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Event name", &ui.UnitEvtNameEd, false, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Time (s / enter / leave)", &ui.UnitEvtTimeEd, true, "")
		}),
		// The same moment, moved rather than set: a number of seconds added to
		// every selected row, with the button beside the field that reads it.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(112)
					l := material.Label(ui.Th, unit.Sp(9), "Shift (s)")
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.Y = gtx.Dp(20)
					gtx.Constraints.Max.Y = gtx.Dp(22)
					b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
					return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							w := material.Editor(ui.Th, &ui.UnitEvtShiftEd, "e.g. 0.1 or -0.1")
							w.TextSize = unit.Sp(10)
							w.HintColor = uikit.CTextDim
							return w.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtShift, "Shift", len(ui.UnitEvtSelSet) == 0)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "FX path", &ui.UnitEvtFxEd, false, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Sound path", &ui.UnitEvtSndEd, false, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Marker (FX only)", &ui.UnitEvtMarkerEd, true, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			noSel := ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events)
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtSet, "Set", noSel)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtApply, "Apply", noSel)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.EvtPlayer.PlayEv, "▶ Sound", !UnitEvtSelectedIsSound(ui))
				}),
			)
		}),
	)
	// While several rows are selected the moment field is a batch edit, so the
	// panel says so where the button that does it sits.
	if n := len(UnitEvtSelectedIndices(ui)); n > 1 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, fmt.Sprintf("%d rows selected — Apply sets the typed time on all of them, Shift moves it.", n))
			})
		}))
	}
	if ui.UnitEvtFormErr != "" {
		err := ui.UnitEvtFormErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(9), err)
				l.Color = rgb(0xC98A8A)
				l.MaxLines = 4
				return l.Layout(gtx)
			})
		}))
	}
	// Add and remove, then the one-press crystal copy, then save/revert.
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitEvtAddEvent, "Add event")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitEvtAddFX, "Add FX")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitEvtAddSound, "Add sound")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtRemove, "Remove", len(tl.Events) == 0 || len(ui.UnitEvtSelSet) == 0)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtCopy, "Copy", len(ui.UnitEvtSelSet) == 0)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, _, has := ui.UnitClips.Pick(clipEvents, ui.UnitClipCur)
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtPaste, "Paste", !has)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, cur, ok := ui.UnitClips.Pick(clipEvents, ui.UnitClipCur)
					first := !ok || ui.UnitClips.Step(clipEvents, cur, -1) == cur
					return SmallButtonDisabled(ui, gtx, &ui.UnitClipPrev, "\u25c2", first)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, cur, ok := ui.UnitClips.Pick(clipEvents, ui.UnitClipCur)
					last := !ok || ui.UnitClips.Step(clipEvents, cur, 1) == cur
					return SmallButtonDisabled(ui, gtx, &ui.UnitClipNext, "\u25b8", last)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), ui.UnitClips.Label(clipEvents, ui.UnitClipCur))
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					})
				}),
			)
		})
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return PrimaryButton(ui, gtx, &ui.UnitEvtSave, "Save .whe")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtRevert, "Revert", !ui.UnitEvtDirty)
				}),
			)
		})
	}))
	note := "Saved into this mod's own .whe; the game's files are never written."
	if ui.UnitEvtDirty {
		note = "Unsaved changes. " + note
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return unitFieldNote(gtx, ui.Th, note)
		})
	}))

	inset := layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutUnitEventRow is one event of the timeline: when it fires, what it
// fires, and what kind of event it is.
func LayoutUnitEventRow(ui *uiState, gtx layout.Context, i int, ev whe.FxTimelineEvent, defs map[string]whe.FxEvent) layout.Dimensions {
	if i < 0 || i >= len(ui.UnitEvtClicks) {
		return layout.Dimensions{}
	}
	c := &ui.UnitEvtClicks[i]
	selected := UnitEvtRowSelected(ui, i)
	bg := uikit.CPanelBg
	switch {
	case selected:
		bg = uikit.CSelectBg
	case c.Hovered():
		bg = uikit.CMenuHover
	}
	kind := unitEventKindTag(ev.Name, defs)
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
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
				return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), unitEventTimeLabel(ev.Time))
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), unitViewportRowText(ev.Name))
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), kind)
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

// layoutUnitEventTimelineRow is one selectable timeline: its name, and the
// clips it drives.
func LayoutUnitEventTimelineRow(ui *uiState, gtx layout.Context, i int, tl whe.FxTimeline) layout.Dimensions {
	if i < 0 || i >= len(ui.UnitEvtTlClicks) {
		return layout.Dimensions{}
	}
	c := &ui.UnitEvtTlClicks[i]
	selected := strings.EqualFold(ui.UnitEvtTimeline, tl.Name)
	bg := uikit.CPanelBg
	switch {
	case selected:
		bg = uikit.CSelectBg
	case c.Hovered():
		bg = uikit.CMenuHover
	}
	clips := strings.Join(tl.Animations, ", ")
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
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
							l := material.Label(ui.Th, unit.Sp(10), unitViewportRowText(tl.Name))
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							note := fmt.Sprintf("%d events", len(tl.Events))
							if clips != "" {
								note += "  \u00b7  " + clips
							}
							l := material.Label(ui.Th, unit.Sp(9), note)
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
