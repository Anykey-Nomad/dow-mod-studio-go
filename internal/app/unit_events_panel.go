package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"strings"
)

// unitWhmClipSet is the set of clips the unit's model carries, lowered and
// trimmed the way names are compared elsewhere in the editor, or nil when no
// model is on screen: with no model there is no file to check names against
// and nothing can be called missing.
func unitWhmClipSet(ui *uiState) map[string]bool {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return nil
	}
	set := make(map[string]bool, len(v.Model.Animations))
	for _, a := range v.Model.Animations {
		set[strings.ToLower(strings.TrimSpace(a.Name))] = true
	}
	return set
}

// unitAnimMissingInWhm reports whether a blend entry names a clip the model
// does not carry. An empty name is not a clip reference at all, and a nil set
// means no model is on screen, so neither is ever marked.
func unitAnimMissingInWhm(clips map[string]bool, name string) bool {
	if clips == nil {
		return false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return name != "" && !clips[name]
}

// unitAnimMissingList is the clip names of a timeline the model does not
// carry, in the file's own order.
func unitAnimMissingList(clips map[string]bool, anims []string) []string {
	var out []string
	for _, a := range anims {
		if unitAnimMissingInWhm(clips, a) {
			out = append(out, a)
		}
	}
	return out
}

// unitEvtFilterRows is the rows of a list the filter keeps: every entry whose
// name contains its text, compared without regard to case or spacing. An empty
// filter keeps the whole list, so the row indices handed on are always the
// entry's own index in the file — clicks, selection and the copy key all stay
// addressed the way they are without a filter.
func UnitEvtFilterRows[T any](filter string, items []T, name func(T) string) []int {
	q := strings.ToLower(strings.TrimSpace(filter))
	rows := make([]int, 0, len(items))
	for i := range items {
		if q == "" || strings.Contains(strings.ToLower(name(items[i])), q) {
			rows = append(rows, i)
		}
	}
	return rows
}

// unitListFilterField is the one-line filter drawn above a list: a bordered
// field that takes the caret on a press, takes a copied name of its list's own
// space on a right press, and whose text the list is narrowed by.
func UnitListFilterField(ui *uiState, gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	gtx.Constraints.Min.Y = gtx.Dp(20)
	gtx.Constraints.Max.Y = gtx.Dp(22)
	b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
	dims := b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			w := material.Editor(ui.Th, ed, hint)
			w.TextSize = unit.Sp(10)
			w.HintColor = uikit.CTextDim
			return w.Layout(gtx)
		})
	})
	// The whole box is the click target that takes the caret, in its own
	// pass so the editor's own caret hit-testing keeps working.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	click := RgdEnsureEditorClick(ui, ed)
	click.Click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: dims.Size}
	})
	pass.Pop()
	// A right press pastes a copied name of the list's space into it.
	uikit.LayoutClipPaste(ui, gtx, ed, dims.Size)
	return dims
}

// layoutUnitEventTimelines is the middle column: every DATAMTON timeline of
// the .whe, the selected one highlighted, and which clips each drives. The
// .whe's actions are drawn under that list — they are file-wide and their
// motion lines name these very timelines, so they belong beside the list of
// them rather than inside the events of whichever timeline is picked.
func LayoutUnitEventTimelines(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitEvents(ui)
	// The rows are marked against the clips the unit's .whm carries, so
	// this section loads the model the way Classic OE does — a reader who
	// opens Events first would otherwise never see the marking.
	EnsureUnitModel(ui, gtx)
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
			// The block always shows while a file is loaded: a .whe that has
			// never had an action still needs the button that gives it one,
			// and hiding the block until a clipboard held one made the first
			// action unreachable.
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitEvtActions(ui, gtx, acts)
				}),
			)
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
//
// The header, the copy/paste/new row and the filter stay pinned above that region
// the way the timeline block pins them above its own list: a filter that
// scrolls away with its rows is a filter that cannot be reached once the list
// has moved, and it hides the buttons with it.
func LayoutUnitEvtActions(ui *uiState, gtx layout.Context, acts []whe.FxAction) layout.Dimensions {
	EnsureUnitEvtActionClicks(ui, len(acts))
	SelectedUnitEvtAction(ui, acts)
	rows := UnitEvtFilterRows(ui.UnitActFilter.Text(), acts, func(a whe.FxAction) string { return a.Name })
	// What stays above the scrolling rows: the header, the copy/paste/new
	// row and the filter, laid out as one fixed block the way the timeline
	// column lays out its own header, copy row and filter.
	pinned := func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.Th, "Actions")
				})
			}),
			// An action moves between units the way a timeline does: one is
			// copied whole here and pastes into another unit's list, and a new
			// one is created here for a file that carries none. Three buttons
			// do not fit a narrow column, and a button drawn past the column's
			// edge is a button that cannot be pressed, so when they do not fit
			// the new-action button takes a line of its own.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				inset := layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}
				copyPaste := func(gtx layout.Context) layout.Dimensions {
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
				}
				// A file with no action yet gets its first from here: an
				// empty record under a name the file does not carry, picked
				// so the panel below offers its choices. Remove takes the
				// picked one back out — refused by the editor while another
				// action still calls it.
				newAction := func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitEvtNewAct, "New action")
				}
				removeAction := func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitEvtRemoveAct, "Remove action", len(acts) == 0)
				}
				creationRow := func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(newAction),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(removeAction),
					)
				}
				both := func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(copyPaste),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(creationRow),
					)
				}
				// The three are measured in a wide, minimum-free box with input
				// off, so the buttons report their own widths rather than the
				// column's — a box as narrow as the column would answer with
				// its own width and always claim to fit.
				macro := op.Record(gtx.Ops)
				measure := gtx.Disabled()
				measure.Constraints.Min = image.Pt(0, 0)
				measure.Constraints.Max = image.Pt(4096, gtx.Constraints.Max.Y)
				wide := both(measure).Size.X
				macro.Stop()
				if wide <= gtx.Constraints.Max.X-gtx.Dp(unit.Dp(16)) {
					return inset.Layout(gtx, both)
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return inset.Layout(gtx, copyPaste)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return inset.Layout(gtx, creationRow)
					}),
				)
			}),
			// The filter narrows the rows the same way it narrows the timeline
			// list above; a row it keeps keeps its index in the file, so the
			// picked action stays picked and a press still lands on the action
			// it names.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitListFilterField(ui, gtx, &ui.UnitActFilter, "filter actions")
				})
			}),
		)
	}
	// Only the rows scroll, so only the rows are the scrolling content.
	content := func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		if len(rows) == 0 && strings.TrimSpace(ui.UnitActFilter.Text()) != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitEventsMessage(ui, gtx, "No action matches the filter.")
			}))
		} else if len(rows) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitEventsMessage(ui, gtx, "This .whe carries no actions. New action gives it one.")
			}))
		}
		for _, i := range rows {
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
	pinnedH := pinned(measureGTX).Size.Y
	natural := pinnedH + content(measureGTX).Size.Y
	if natural >= panelScrollMeasureHeight {
		measureGTX.Constraints.Max.Y = maxInt(1, gtx.Constraints.Max.Y)
		natural = pinnedH + content(measureGTX).Size.Y
	}
	macro.Stop()
	height := natural
	// Two fifths of the column is the most the block may claim, which leaves
	// the timelines list with the majority however many actions there are. A
	// cap that would swallow the pinned part is no cap at all: the pinned
	// part cannot scroll away, so the rows keep their natural height then.
	if cap := gtx.Constraints.Max.Y * 2 / 5; cap > pinnedH && height > cap {
		height = cap
	}
	viewport := height - pinnedH
	if viewport < 0 {
		viewport = 0
	}
	agtx := gtx
	agtx.Constraints.Min.Y = viewport
	agtx.Constraints.Max.Y = viewport
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return pinned(gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return PanelScrollView(ui, agtx, &ui.UnitEvtActionsScroll, content)
		}),
	)
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
	key := uikit.RowKey("evt-act", i)
	dims := c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
	// The row's right-press area copies the action's name into the shared
	// clipboard, where a name field of matching type pastes it back.
	uikit.LayoutRowNameCopy(ui, gtx, key, dims.Size)
	return dims
}

func LayoutUnitEventTimelineList(ui *uiState, gtx layout.Context) layout.Dimensions {
	if ui.UnitEvtDoc == nil {
		return UnitEventsMessage(ui, gtx, ui.UnitEvtErr)
	}
	tls := ui.UnitEvtDoc.Timelines()
	selected := SelectedUnitEventTimeline(ui)
	selEvents := 0
	if selected != nil {
		selEvents = len(selected.Events)
	}
	EnsureUnitEventClicks(ui, len(tls), selEvents)
	rows := UnitEvtFilterRows(ui.UnitEvtTlFilter.Text(), tls, func(t whe.FxTimeline) string { return t.Name })
	// The header stays with its two buttons and the new-timeline row even
	// when the file carries no timeline at all — a file with none is exactly
	// the one that has to be given the button that creates its first.
	children := []layout.FlexChild{
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
		// Remove takes the picked one back out of this file.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitEvtCopyTl, "Copy timeline", selected == nil)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitEvtRemoveTl, "Remove timeline", selected == nil)
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
		// New timeline: the name is typed, so a motion can be pointed at the
		// timeline under a name that means something, and Enter in the field
		// creates it the way the button does.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return UnitListFilterField(ui, gtx, &ui.UnitEvtNewTlEd, "new timeline name")
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButton(ui, gtx, &ui.UnitEvtNewTl, "New timeline")
					}),
				)
			})
		}),
		// The filter narrows the rows to the names containing it, so a file
		// with a hundred motions is one substring from the one wanted.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return UnitListFilterField(ui, gtx, &ui.UnitEvtTlFilter, "filter timelines")
			})
		}),
	}
	switch {
	case len(tls) == 0:
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return UnitEventsMessage(ui, gtx, "This .whe carries no animation timelines. New timeline gives it one.")
		}))
	case len(rows) == 0:
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return UnitEventsMessage(ui, gtx, "No timeline matches the filter.")
		}))
	default:
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return LayoutListWithScrollbar(ui, gtx, &ui.UnitEvtScroll, &ui.UnitEvtList, unitRowHeightDp-4, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
				// The row keeps its index in the file: its click
				// target, its selection and its copy key are all
				// addressed by that index, not by its place in the
				// filtered view.
				return LayoutUnitEventTimelineRow(ui, gtx, rows[i], tls[rows[i]])
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
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
		// The model's clips, so a blend entry naming one the .whm does
		// not carry is marked in the row that lists it.
		animClips := unitWhmClipSet(ui)
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
						dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(10), tl.Animations[i])
								l.Color = uikit.CText
								if unitAnimMissingInWhm(animClips, tl.Animations[i]) {
									// The model carries no clip of this
									// name — the one thing certainly
									// wrong with this motion.
									l.Color = uikit.CProblem
								}
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return SmallButton(ui, gtx, &ui.UnitEvtAnimClicks[i], "Remove")
							}),
						)
						// The row's right-press area copies the animation's name
						// into the shared clipboard for a name field to paste.
						uikit.LayoutRowNameCopy(ui, gtx, uikit.RowKey("evt-anim", i), dims.Size)
						return dims
					})
				}))
			}
		}
		// The add field appends to the same list: an animation name
		// typed or pasted from a copied row, committed by the button or
		// Enter — the inverse of the Remove beside each entry.
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.Y = gtx.Dp(20)
						gtx.Constraints.Max.Y = gtx.Dp(22)
						b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
						dims := b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								w := material.Editor(ui.Th, &ui.UnitAnimAddEd, "animation name")
								w.TextSize = unit.Sp(10)
								w.HintColor = uikit.CTextDim
								return w.Layout(gtx)
							})
						})
						// A right click pastes a copied animation name here.
						uikit.LayoutClipPaste(ui, gtx, &ui.UnitAnimAddEd, dims.Size)
						return dims
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						// The list button raises the model's clips to pick
						// from, for a name that is not typed by hand.
						return SmallButton(ui, gtx, &ui.UnitAnimChoose, "\u25be")
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButton(ui, gtx, &ui.UnitAnimAddClick, "Add")
					}),
				)
			})
		}))
		// How the motion itself plays: the tail of the DATAMTON payload, which
		// the game reads for looping, delays and transitions even though the
		// events are what a reader edits most.
		if tl.Tail.Present {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.Th, "Motion")
				})
			}))
			// Type and modifier are the two of those a reader changes, so they
			// are controls rather than rows copied out of the file: the type
			// cycles on a press, the modifier is typed and committed. The rest
			// keep being read-only — they are timings no one tunes by hand.
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return unitEvtTailTypeRow(ui, gtx, tl.Tail)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return unitEvtTailModifierRow(ui, gtx)
					})
				}),
			)
			for _, row := range whe.MotionTailRows(tl.Tail) {
				if row[0] == "Type" || row[0] == "Modifier" {
					continue
				}
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

// unitEvtTailTypeRow is the motion type as a value that cycles on a press: the
// three types the engine defines are one chip apart, and the press is the whole
// edit — no menu for a field with three states.
func unitEvtTailTypeRow(ui *uiState, gtx layout.Context, tail whe.FxMotionTail) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(112)
			l := material.Label(ui.Th, unit.Sp(10), "Type")
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &ui.UnitTailType, whe.MotionTypeName(tail.Type))
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "click to cycle")
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// unitEvtTailModifierRow edits the modifier the motion drives. A right press
// pastes a copied name into the field, the way the other name fields take one.
func unitEvtTailModifierRow(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(112)
			l := material.Label(ui.Th, unit.Sp(10), "Modifier")
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(20)
			gtx.Constraints.Max.Y = gtx.Dp(22)
			b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
			dims := b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := material.Editor(ui.Th, &ui.UnitTailModEd, "no modifier")
					w.TextSize = unit.Sp(10)
					w.HintColor = uikit.CTextDim
					return w.Layout(gtx)
				})
			})
			uikit.LayoutClipPaste(ui, gtx, &ui.UnitTailModEd, dims.Size)
			return dims
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &ui.UnitTailModSet, "Set")
		}),
	)
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
	key := uikit.RowKey("evt-ev", i)
	dims := c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
	// The row's right-press area copies the event's name into the shared
	// clipboard, where a name field of matching type pastes it back.
	uikit.LayoutRowNameCopy(ui, gtx, key, dims.Size)
	return dims
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
	// The model's clips, so a timeline blending one the .whm does not
	// carry is marked here and in the list of its animations below.
	whmClips := unitWhmClipSet(ui)
	missing := unitAnimMissingList(whmClips, tl.Animations)
	clips := strings.Join(tl.Animations, ", ")
	key := uikit.RowKey("evt-tl", i)
	dims := c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
							if len(missing) > 0 {
								// The model carries no clip this timeline
								// blends, so the row itself is marked.
								l.Color = uikit.CProblem
							}
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							note := fmt.Sprintf("%d events", len(tl.Events))
							noteColor := uikit.CTextDim
							if len(missing) > 0 {
								// The note names the clips the model does
								// not carry rather than all of them.
								note += "  ·  not in .whm: " + strings.Join(missing, ", ")
								noteColor = uikit.CProblem
							} else if clips != "" {
								note += "  \u00b7  " + clips
							}
							l := material.Label(ui.Th, unit.Sp(9), note)
							l.Color = noteColor
							l.MaxLines = 1

							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
	// The row's right-press area copies the timeline's name into the shared
	// clipboard, where a name field of matching type pastes it back.
	uikit.LayoutRowNameCopy(ui, gtx, key, dims.Size)
	return dims
}
