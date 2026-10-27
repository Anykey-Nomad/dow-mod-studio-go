package app

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"fmt"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// layoutClassicOEArea is the whole section: the panel column, then the
// preview with its playback row underneath it, then the same list panel the
// Viewport section draws, on the right — the list tabs and the preview toggles
// of the unit's model. The playback row belongs to the preview alone rather
// than to the three columns, so nothing under the panel column or the list is
// covered by controls that do not drive it. The model load is kicked off
// here the same way the Viewport section kicks it off, so opening the section
// on a unit that has not been previewed yet still ends with a model on screen.
func LayoutClassicOEArea(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitModel(ui, gtx)
	// The .whe behind the condition, event and modifier panels: the Events
	// section's loader, which reads the file once per unit and remembers why
	// there is nothing to read. Calling it here means the panels are filled on
	// a first visit to Classic OE rather than only after the Events section
	// has been opened once.
	EnsureUnitEvents(ui)
	EnsureClassicOEClicks(ui)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			panelW := gtx.Dp(unit.Dp(classicOEPanelDp))
			sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
			gap := gtx.Dp(unit.Dp(unitColGapDp))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(FixedPanel(ui, panelW, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOELeftPanel(ui, gtx) })),
					layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOEPreview(ui, gtx) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
					layout.Rigid(FixedPanel(ui, sideW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitModelSide(ui, gtx) })),
				)
			}),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.ClassicOEDialog == classicOEDialogNone {
				return layout.Dimensions{}
			}
			return LayoutClassicOEEventDialog(ui, gtx)
		}),
	)
}

// layoutClassicOELeftPanel is the column of collapsible headers, framed and
// scrollable: the anims tree alone is taller than a small window, and the
// panels below it must stay reachable when it is.
func LayoutClassicOELeftPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, "Object Editor")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return PanelWithScroll(ui, gtx, &ui.ClassicOEScroll, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOEHeaderList(ui, gtx) })
			}),
		)
	})
}

// layoutClassicOEHeaderList stacks every panel: its header row, and the body
// underneath when the panel is open.
func LayoutClassicOEHeaderList(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureClassicOEClicks(ui)
	var children []layout.FlexChild
	for i := range classicOEHeaders {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutClassicOEHeaderRow(ui, gtx, i)
		}))
		if ui.ClassicOEOpen[i] {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutClassicOEHeaderBody(ui, gtx, i)
			}))
		}
	}
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutClassicOEHeaderBody is what an open panel shows: the list its kind
// names, or the panel's hint for the ones whose content is not built yet.
func LayoutClassicOEHeaderBody(ui *uiState, gtx layout.Context, index int) layout.Dimensions {
	switch classicOEHeaders[index].Kind {
	case classicOEKindAnims:
		return LayoutClassicOEAnimTree(ui, gtx)
	case classicOEKindEvents:
		return LayoutClassicOEEventList(ui, gtx)
	case classicOEKindConditions:
		return LayoutClassicOEConditionEditorList(ui, gtx)
	case classicOEKindClauses:
		return LayoutClassicOEClauseList(ui, gtx)
	case classicOEKindModifiers:
		return LayoutClassicOEModifierList(ui, gtx)
	case classicOEKindMeshes:
		return UnitMeshRows(ui, gtx)
	case classicOEKindSelectedUI:
		return LayoutClassicOESelectedUI(ui, gtx)
	}
	return classicOEHint(gtx, ui.Th, classicOEHeaders[index].Hint)
}

// layoutClassicOEAnimList is the anims panel's body: the model's clips, the
// one being played marked, each press playing it in the preview beside the
// panel. It is the Viewport section's own list with its own click targets, so
// the two views cannot fight over which clip a press meant.
func LayoutClassicOEAnimList(ui *uiState, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		l := material.Label(ui.Th, unit.Sp(9), "Loading the model…")
		l.Color = uikit.CTextDim
		return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, l.Layout)
	}
	return UnitAnimListWithClicks(ui, gtx, v, &ui.ClassicOEAnimClicks)
}

// layoutClassicOEAnimTree is the body of the Anims / Motions / Actions
// header: the four folders with their counts, each toggling its contents
// inline, so the whole panel reads as the editor's tree rather than a plain
// animation list.
func LayoutClassicOEAnimTree(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureClassicOEFolders(ui)
	v := UnitModelViewer(ui)
	doc, docMsg := ClassicOEListDoc(ui)
	var timelines []whe.FxTimeline
	var actions []whe.FxAction
	if doc != nil {
		timelines = doc.Timelines()
		actions = doc.Actions()
	}
	roots := classicOETreeRoots(actions)
	counts := [classicOEFolderCount]int{}
	if v != nil && v.Model != nil {
		counts[classicOEFolderAnims] = len(v.Model.Animations)
	}
	counts[classicOEFolderMotions] = len(timelines)
	counts[classicOEFolderActions] = len(actions)
	counts[classicOEFolderTree] = len(roots)
	labels := [classicOEFolderCount]string{"Animations", "Motions", "Actions", "Action Tree"}

	var children []layout.FlexChild
	for folder := 0; folder < classicOEFolderCount; folder++ {
		folder := folder
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutClassicOEFolderRow(ui, gtx, folder, labels[folder], counts[folder])
		}))
		if !ui.ClassicOEFolderOpen[folder] {
			continue
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutClassicOEFolderBody(ui, gtx, folder, v, timelines, actions, docMsg)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutClassicOEFolderBody renders what one open folder holds: the clip
// list, the motion rows, the action rows, or the derived action tree.
func LayoutClassicOEFolderBody(ui *uiState, gtx layout.Context, folder int, v *modelViewer, timelines []whe.FxTimeline, actions []whe.FxAction, docMsg string) layout.Dimensions {
	switch folder {
	case classicOEFolderAnims:
		return LayoutClassicOEAnimList(ui, gtx)
	case classicOEFolderMotions:
		if len(timelines) == 0 {
			return LayoutClassicOEHint(ui, gtx, docMsg, "This .whe defines no motions.")
		}
		if len(ui.ClassicOEMotionClicks) != len(timelines) {
			ui.ClassicOEMotionClicks = make([]widget.Clickable, len(timelines))
		}
		children := make([]layout.FlexChild, 0, len(timelines))
		for i := range timelines {
			t := timelines[i]
			c := &ui.ClassicOEMotionClicks[i]
			if c.Clicked(gtx) {
				// A motion is named for the animation it drives, so
				// one press jumps exactly as the tree's leaf does —
				// and a double press opens the timeline's editor in
				// the dialog instead.
				if ClassicOEDoubleClick(ui, "motion:"+strings.ToLower(t.Name)) {
					OpenClassicOEEventDialog(ui, classicOEDialogTimeline, t.Name, 0)
				} else {
					ClassicOETreeActivate(ui, classicOETreeRow{Name: t.Name})
				}
			}
			note := classicOEUseNote(len(t.Events), "event", "events")
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitViewportRow(ui, gtx, c, false, t.Name, note)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	case classicOEFolderActions:
		if len(actions) == 0 {
			return LayoutClassicOEHint(ui, gtx, docMsg, "This .whe defines no actions.")
		}
		if len(ui.ClassicOEActionClicks) != len(actions) {
			ui.ClassicOEActionClicks = make([]widget.Clickable, len(actions))
		}
		children := make([]layout.FlexChild, 0, len(actions))
		for i := range actions {
			a := actions[i]
			c := &ui.ClassicOEActionClicks[i]
			if c.Clicked(gtx) {
				// One press jumps to the action's clip — the same
				// resolution the Action Tree's branch rows use —
				// and a double press opens its condition editor in
				// the dialog instead.
				if ClassicOEDoubleClick(ui, "action:"+strings.ToLower(a.Name)) {
					OpenClassicOEEventDialog(ui, classicOEDialogAction, a.Name, i)
				} else {
					key := "/" + strings.ToLower(strings.TrimSpace(a.Name))
					ClassicOETreeActivate(ui, classicOETreeRow{Name: a.Name, Key: key})
				}
			}
			note := classicOEUseNote(len(a.Motions)+len(a.Subactions), "choice", "choices")
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// Every action row reserves the icon column — the game's
				// own command picture where the name says what the action
				// is, blank space where it does not — so the names stay
				// in one line either way, as the unit list does.
				return UnitViewportRowIcon(ui, gtx, c, false, a.Name, note, ActionIcon(ui, a.Name), true)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	case classicOEFolderTree:
		if len(classicOETreeRoots(actions)) == 0 {
			return LayoutClassicOEHint(ui, gtx, docMsg, "This .whe defines no actions to build a tree from.")
		}
		rows := classicOETreeRows(actions, ui.ClassicOEExpandOpen)
		children := make([]layout.FlexChild, 0, len(rows))
		for i := range rows {
			row := rows[i]
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutClassicOETreeRow(ui, gtx, row)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	return layout.Dimensions{}
}

// layoutClassicOETreeRow is one row of the Action Tree: indentation by
// depth, the expand marker where the node has children, the name, and the
// condition dimmed beside it. The row carries two separate press targets —
// the marker column toggles a branch open, everything else plays the
// animation the node points at — so navigation and expansion never fight
// over one press. A leaf's blank marker column belongs to the jump target,
// which keeps the whole line live.
func LayoutClassicOETreeRow(ui *uiState, gtx layout.Context, row classicOETreeRow) layout.Dimensions {
	EnsureClassicOEFolders(ui)
	indent := unit.Dp(8 + row.Depth*10)

	// A map index cannot be addressed, so each node's press targets are
	// allocated once and kept beside the expansion state.
	var expand *widget.Clickable
	if row.Key != "" {
		expand = ui.ClassicOEExpandClicks[row.Key]
		if expand == nil {
			expand = &widget.Clickable{}
			ui.ClassicOEExpandClicks[row.Key] = expand
		}
		if expand.Clicked(gtx) {
			// Toggle from the same default the derivation used, so a
			// key the map has never seen flips to its opposite once.
			ui.ClassicOEExpandOpen[row.Key] = !row.Expanded
		}
	}
	// The jump target is keyed by node path for a branch — its subtree
	// decides the clip — and by name for a leaf: two leaves of one name
	// play the same clip anyway.
	jumpKey := "\x00leaf:" + strings.ToLower(strings.TrimSpace(row.Name))
	if row.Key != "" {
		jumpKey = row.Key
	}
	jump := ui.ClassicOEJumpClicks[jumpKey]
	if jump == nil {
		jump = &widget.Clickable{}
		ui.ClassicOEJumpClicks[jumpKey] = jump
	}
	if jump.Clicked(gtx) {
		// A second press opens the row's editor — an action's condition
		// editor, a motion leaf's timeline editor — the way the Actions
		// and Motions folders do; the first press has already played it.
		if !ClassicOETreeRowOpensDialog(ui, row) {
			ClassicOETreeActivate(ui, row)
		}
	}

	hover := jump.Hovered() || (expand != nil && expand.Hovered())
	content := func(gtx layout.Context) layout.Dimensions {
		// Pinned like the folder bar: the hover background has to span
		// the whole row, not the name's width.
		h := gtx.Dp(unit.Dp(18))
		gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Max = image.Pt(gtx.Constraints.Max.X, h)
		return layout.Stack{Alignment: layout.Center}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if hover {
					rrect(gtx, uikit.CMenuHover, gtx.Constraints.Max, gtx.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: indent, Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							marker := ""
							if row.Key != "" {
								marker = "▸"
								if row.Expanded {
									marker = "▾"
								}
							}
							gtx.Constraints.Min.X = gtx.Dp(unit.Dp(14))
							gtx.Constraints.Max.X = gtx.Dp(unit.Dp(14))
							gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
							draw := func(gtx layout.Context) layout.Dimensions {
								return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(9), marker)
									l.Color = uikit.CTextDim
									return l.Layout(gtx)
								})
							}
							if expand != nil {
								return expand.Layout(gtx, draw)
							}
							// A leaf's blank marker is part of the jump
							// target, so no part of the line is dead.
							return jump.Layout(gtx, draw)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return jump.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										// Branch rows are actions: they carry a folder
										// to say the row opens into a list, then the
										// same small command icon the Actions folder
										// shows. A motion leaf has neither, so the
										// column belongs to branches alone.
										if row.Key == "" {
											return layout.Dimensions{}
										}
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return folderIconLayout(gtx, 14, 11)
												})
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return unitIconLayout(gtx, ActionIcon(ui, row.Name), 20, 15)
												})
											}),
										)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(10), row.Name)
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, rowGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if row.Note == "" {
											return layout.Dimensions{}
										}
										l := material.Label(ui.Th, unit.Sp(9), classicOENote(row.Note))
										l.Color = uikit.CTextDim
										l.MaxLines = 1
										return l.Layout(gtx)
									}),
								)
							})
						}),
					)
				})
			}),
		)
	}
	return content(gtx)
}

// classicOETreeRowOpensDialog reads a press on an Action Tree row for a
// double: the second press on an action row raises its condition editor and
// the second press on a motion leaf raises its timeline editor — the same
// dialogs the Actions and Motions folders raise, each duplicating its area
// of the Events tab. It answers false for a first press, so the caller plays
// the row instead, and true when the press was a double — whether or not the
// row turned out to have an editor.
func ClassicOETreeRowOpensDialog(ui *uiState, row classicOETreeRow) bool {
	name := strings.ToLower(strings.TrimSpace(row.Name))
	key := "motion:" + name
	if row.Key != "" {
		// Branches are actions; a leaf's Key is empty.
		key = "action:" + name
	}
	if !ClassicOEDoubleClick(ui, key) {
		return false
	}
	// The double belongs to this row: open its editor when it has one.
	doc, _ := ClassicOEListDoc(ui)
	if doc == nil {
		return true
	}
	kind, itemName, actionIndex, ok := classicOETreeRowDialog(doc, row)
	if ok {
		OpenClassicOEEventDialog(ui, kind, itemName, actionIndex)
	}
	return true
}

// openClassicOEEventDialog raises the dialog on the clicked item by pointing
// the shared Events editor at it: the picked timeline for a motion, the
// picked action for an action.
func OpenClassicOEEventDialog(ui *uiState, kind int, name string, actionIndex int) {
	EnsureUnitEvents(ui)
	ui.ClassicOEDialog = kind
	ui.ClassicOEDialogName = name
	if kind == classicOEDialogAction {
		// The same switch the Events section's action rows make: the panel
		// shows the action's conditions and none of the timeline's events.
		if !ui.UnitEvtFocusAction {
			ui.EvtPlayer.StopAll()
		}
		ui.UnitEvtFocusAction = true
		if ui.UnitEvtDoc != nil {
			if acts := ui.UnitEvtDoc.Actions(); actionIndex >= 0 && actionIndex < len(acts) {
				ui.UnitEvtActionSel = actionIndex
			}
		}
		return
	}
	ui.UnitEvtFocusAction = false
	SelectUnitEventTimeline(ui, name)
}

// layoutClassicOEEventDialog is the dialog itself: a dimmed card over the
// Classic OE panel with the Events section's own editor body and footer
// inside. A press on the dim or the Close button shuts it; Save .whe and
// Revert act on the same document the Events section would act on. A press on
// the card's own empty space is neither: the card takes it, so clicking the
// whitespace of the editor no longer closes it.
func LayoutClassicOEEventDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	if ui.ClassicOEevtClose.Clicked(gtx) || ui.ClassicOEevtDim.Clicked(gtx) {
		ui.ClassicOEDialog = classicOEDialogNone
		return layout.Dimensions{}
	}
	title := "Timeline editor"
	if ui.ClassicOEDialog == classicOEDialogAction {
		title = "Action editor"
	}
	sub := ui.ClassicOEDialogName
	if ui.UnitEvtDirty {
		sub += "  \u2022 unsaved"
	}
	body := func(gtx layout.Context) layout.Dimensions {
		// The action body reads straight from the document, so a file that
		// never loaded stays on the guarded timeline body, which carries
		// the reason instead of a nil dereference.
		if ui.ClassicOEDialog == classicOEDialogAction && ui.UnitEvtDoc != nil {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, UnitEvtActionPanelBody(ui, gtx)...)
		}
		return UnitEventPanelBody(ui, gtx)
	}
	footer := func(gtx layout.Context) layout.Dimensions {
		if ui.ClassicOEDialog == classicOEDialogAction {
			return UnitActPanelFooter(ui, gtx)
		}
		return UnitEventPanelForm(ui, gtx)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// The dim is the dialog's own click target: a press on it
			// closes the card and never reaches the panel underneath.
			return ui.ClassicOEevtDim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				st := clip.Rect{Max: sz}.Push(gtx.Ops)
				paint.ColorOp{Color: rgba(0, 0, 0, 70)}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				st.Pop()
				return layout.Dimensions{Size: sz}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := minInt(gtx.Dp(unit.Dp(700)), gtx.Constraints.Max.X-gtx.Dp(unit.Dp(30)))
				cardH := minInt(gtx.Dp(unit.Dp(680)), gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)))
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							// The card takes the press before the dim beneath can
							// have it: the editor rarely fills the card, and a
							// click on what is left used to shut the dialog.
							absorbDialogCardPress(gtx, &ui.ClassicOEevtCardTag)
							inset := layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}
							return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(12), title)
												l.Color = uikit.CText
												return l.Layout(gtx)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(9), sub)
												l.Color = uikit.CTextDim
												return l.Layout(gtx)
											}),
											layout.Flexed(1, rowGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return SmallButton(ui, gtx, &ui.ClassicOEevtClose, "Close")
											}),
										)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return PanelScrollView(ui, gtx, &ui.ClassicOEevtScroll, body)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
									layout.Rigid(footer),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

// layoutClassicOEPreview is the grey area of the classic editor, filled with
// the unit's model: the same canvas the Viewport section draws, so camera
// drags, playback and the scrub bar all work here without a second viewer.
// layoutClassicOEPreview is the middle column: the model, with the playback
// row under it — the same row the Viewport section draws under its own canvas,
// and only as wide as the preview above it, so neither the panel column nor
// the list panel is covered by controls that do not belong to them.
func LayoutClassicOEPreview(ui *uiState, gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, "Preview")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitCanvasColumn(ui, gtx) })
			}),
		)
	})
}

func HandleClassicOEClauseConditionInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != unitSectionClassicOE || ui.UnitEvtDoc == nil {
		return
	}
	for i := range ui.ClassicOEClauseEditors {
		UpdateClassicOEWheEditor(ui, gtx, &ui.ClassicOEClauseEditors[i], func() { ApplyClassicOEClause(ui) })
	}
	for i := range ui.ClassicOEConditionEditors {
		UpdateClassicOEWheEditor(ui, gtx, &ui.ClassicOEConditionEditors[i], func() { ApplyClassicOECondition(ui) })
	}
	if ui.ClassicOEClauseApply.Clicked(gtx) {
		ApplyClassicOEClause(ui)
	}
	if ui.ClassicOEConditionApply.Clicked(gtx) {
		ApplyClassicOECondition(ui)
	}
	if ui.ClassicOEConditionAdd.Clicked(gtx) {
		if _, ok := EnsureClassicOECondition(ui); ok {
			ui.ClassicOEConditionEditors = append(ui.ClassicOEConditionEditors, widget.Editor{SingleLine: true, Submit: true})
			ui.ClassicOEConditionFill = append(ui.ClassicOEConditionFill, "")
		}
	}
	if ui.ClassicOEConditionRemove.Clicked(gtx) && len(ui.ClassicOEConditionEditors) > 0 {
		ui.ClassicOEConditionEditors = ui.ClassicOEConditionEditors[:len(ui.ClassicOEConditionEditors)-1]
		ui.ClassicOEConditionFill = ui.ClassicOEConditionFill[:len(ui.ClassicOEConditionFill)-1]
	}
}

func EnsureClassicOESelectedUI(ui *uiState) (whe.FxSelectedUI, bool, error) {
	SyncUnitSelectionIndicator(ui, UnitModelViewer(ui))
	if ui.UnitEvtDoc == nil {
		return whe.FxSelectedUI{}, false, fmt.Errorf("%s", ui.UnitEvtErr)
	}
	v, present, err := ui.UnitEvtDoc.SelectedUI()
	if err != nil {
		return v, present, err
	}
	if !present {
		return v, false, nil
	}
	values := selectedUIValues(v)
	if ui.ClassicOESelectedUIType == nil {
		ui.ClassicOESelectedUIType = &widget.Enum{}
	}
	bound := ui.UnitEvtPath
	for i := range values {
		bound += "|" + values[i]
	}
	if bound != ui.ClassicOESelectedUIBound {
		ui.ClassicOESelectedUIBound = bound
		ui.ClassicOESelectedUIError = ""
		for i := range values {
			ui.ClassicOESelectedUIEditors[i].SetText(values[i])
			ui.ClassicOESelectedUIFill[i] = values[i]
		}
		if displayType, err := selectedUIDisplayTypeCodeFromValue(v.DisplayType); err == nil {
			ui.ClassicOESelectedUIType.Value = displayType
		} else {
			ui.ClassicOESelectedUIType.Value = "circle"
		}
	}
	if model := UnitModelViewer(ui); model != nil {
		indicator := &engine.SelectionIndicator{DisplayType: v.DisplayType, Scale: v.Scale, Offset: v.Offset}
		if model.SelectionIndicator == nil || *model.SelectionIndicator != *indicator {
			model.SelectionIndicator = indicator
			Invalidate(model)
		}
	}
	return v, true, nil
}

func LayoutClassicOESelectedUI(ui *uiState, gtx layout.Context) layout.Dimensions {
	_, present, err := EnsureClassicOESelectedUI(ui)
	if err != nil {
		return classicOEHint(gtx, ui.Th, "Could not read DATASEUI: "+err.Error())
	}
	if !present {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return classicOEHint(gtx, ui.Th, "This .whe has no DATASEUI chunk.")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButton(ui, gtx, &ui.ClassicOESelectedUICreate, "Create Selected UI")
			}),
		)
	}
	var children []layout.FlexChild
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUIVisibility(ui, gtx)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUIDisplayType(ui, gtx)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUIPair(ui, gtx, "Indicator scale", "X", "Z", 1)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUIPair(ui, gtx, "Indicator offset", "X", "Z", 3)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUITriple(ui, gtx, "Volume offset", "X", "Y", "Z", 5)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUITriple(ui, gtx, "Volume scale", "X", "Y", "Z", 8)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LayoutClassicOESelectedUIMatrix(ui, gtx)
	}))
	if ui.ClassicOESelectedUIError != "" {
		msg := ui.ClassicOESelectedUIError
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.Th, msg) }))
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ClassicOEWheButtons(ui, gtx, &ui.ClassicOESelectedUIApply)
	}))
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func HandleClassicOEWheInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != unitSectionClassicOE {
		return
	}
	if ui.UnitEvtDoc == nil {
		return
	}
	for i := range ui.ClassicOESelectedUIEditors {
		if i == 0 {
			continue // Display type is selected with the Circle/Square radio buttons.
		}
		UpdateClassicOEWheEditor(ui, gtx, &ui.ClassicOESelectedUIEditors[i], func() { ApplyClassicOESelectedUI(ui) })
	}
	for i := range ui.ClassicOEModifierEditors {
		UpdateClassicOEWheEditor(ui, gtx, &ui.ClassicOEModifierEditors[i], func() { ApplyClassicOEModifier(ui) })
	}
	if ui.ClassicOESelectedUICreate.Clicked(gtx) {
		CreateClassicOESelectedUI(ui)
	}
	if ui.ClassicOESelectedUIVisible != nil && ui.ClassicOESelectedUIVisible.Update(gtx) {
		if v := UnitModelViewer(ui); v != nil {
			v.ShowSelection = ui.ClassicOESelectedUIVisible.Value
			Invalidate(v)
		}
	}
	if ui.ClassicOESelectedUIApply.Clicked(gtx) {
		ApplyClassicOESelectedUI(ui)
	}
	if ui.ClassicOEModifierApply.Clicked(gtx) {
		ApplyClassicOEModifier(ui)
	}
	if ui.ClassicOEWheSave.Clicked(gtx) {
		SaveClassicOEWhe(ui)
	}
	if ui.ClassicOEWheRevert.Clicked(gtx) {
		RevertUnitEvents(ui)
		ui.ClassicOESelectedUIBound = ""
		ui.ClassicOEModifierBound = ""
	}
}

func UpdateClassicOEWheEditor(ui *uiState, gtx layout.Context, ed *widget.Editor, submit func()) {
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			submit()
			ed.Blur(gtx)
		}
	}
}

// syncUnitSelectionIndicator loads the selected unit's DATASEUI values for
// either model panel and clears the previous unit's indicator when none exists.
func SyncUnitSelectionIndicator(ui *uiState, v *modelViewer) {
	if v == nil {
		return
	}
	EnsureUnitEvents(ui)
	var indicator *engine.SelectionIndicator
	if ui.UnitEvtDoc != nil {
		value, present, err := ui.UnitEvtDoc.SelectedUI()
		if err == nil && present {
			indicator = &engine.SelectionIndicator{DisplayType: value.DisplayType, Scale: value.Scale, Offset: value.Offset}
		}
	}
	if v.SelectionIndicator == nil && indicator == nil {
		return
	}
	if v.SelectionIndicator != nil && indicator != nil && *v.SelectionIndicator == *indicator {
		return
	}
	v.SelectionIndicator = indicator
	Invalidate(v)
}

func SaveClassicOEWhe(ui *uiState) {
	line, err := SaveUnitEvents(ui)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = line
	MarkStateDirty(ui)
}

// ensureUnitEvents reads the .whe of the unit on screen, once per unit, and
// remembers why it could not when there is nothing to edit.
func EnsureUnitEvents(ui *uiState) {
	if ui.ModTree == nil {
		ui.UnitEvtTree, ui.UnitEvtFor, ui.UnitEvtDoc = nil, "", nil
		ui.UnitEvtPath, ui.UnitEvtErr = "", "No module is open."
		return
	}
	u := CurrentUnit(ui)
	if u == nil {
		ui.UnitEvtTree, ui.UnitEvtFor, ui.UnitEvtDoc = ui.ModTree, "", nil
		ui.UnitEvtPath, ui.UnitEvtErr = "", "No unit selected."
		return
	}
	if ui.UnitEvtTree == ui.ModTree && ui.UnitEvtTried && strings.EqualFold(ui.UnitEvtFor, u.Path) {
		return
	}
	// Only another module invalidates the clipboard. Another unit does not:
	// every copy carries a snapshot of the definitions it needs, so the events
	// copied in one unit pastes into the next one. That is the whole point of
	// the snapshot, and dropping the entries here quietly emptied the buffer
	// exactly when it was about to be used.
	if ui.UnitEvtTree != nil && ui.UnitEvtTree != ui.ModTree {
		ui.UnitClips.DropKind(clipEvents)
		ui.UnitClipCur = -1
	}
	ui.UnitEvtTree = ui.ModTree
	ui.UnitEvtFor = u.Path
	ui.UnitEvtTried = true
	ui.UnitEvtDirty = false
	ui.UnitEvtFormErr = ""
	ui.UnitEvtSel = 0
	ui.UnitEvtSelSet = map[int]bool{0: true}
	ui.UnitEvtSelAnchor = 0
	ui.UnitEvtBound = ""
	ui.UnitEvtEntryBound = ""
	ui.UnitActCondBound = ""
	ui.UnitEvtPath = unitEventsWhePath(ui.ModTree, CurrentUnitDetails(ui))
	if ui.UnitEvtPath == "" {
		ui.UnitEvtDoc = nil
		ui.UnitEvtErr = "This unit's model has no .whe file, so it carries no event timelines."
		return
	}
	data, err := ui.ModTree.ReadFile(ui.UnitEvtPath)
	if err != nil {
		ui.UnitEvtDoc = nil
		ui.UnitEvtErr = "Could not read " + ui.UnitEvtPath + ": " + err.Error()
		return
	}
	doc, err := whe.ParseDocument(data)
	if err != nil {
		ui.UnitEvtDoc = nil
		ui.UnitEvtErr = err.Error()
		return
	}
	ui.UnitEvtDoc = doc
	ui.UnitEvtErr = ""
	if len(doc.Timelines()) == 0 {
		ui.UnitEvtErr = "This .whe carries no animation timelines to edit."
		return
	}
	// Start on the timeline of the clip the viewport is showing, so opening
	// the section after looking at phase_in lands on phase_in.
	want := ""
	if v := UnitModelViewer(ui); v != nil && v.Anim != nil {
		want = v.Anim.Name
	}
	for _, tl := range doc.Timelines() {
		if want != "" && timelineMatchesClip(tl, want) {
			ui.UnitEvtTimeline = tl.Name
			return
		}
	}
	ui.UnitEvtTimeline = doc.Timelines()[0].Name
}

// unitMeshVisibilitySection is the list at the bottom of the Events panel:
// every mesh of the unit's model with an eye toggle, present whether a
// timeline or an action is on show. The eye is open while the mesh draws and
// closed while it is hidden, and the hidden set is the shared one every
// viewer reads.
func UnitMeshVisibilitySection(ui *uiState, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	count := 0
	if v != nil && v.Model != nil {
		count = len(modelMeshNames(v.Model))
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, fmt.Sprintf("Meshes (%d)", count))
		}),
	}
	if count == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshRows(ui, gtx) }))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	// Measure the rows unbounded, then hand the scroller at most the cap: a
	// short list keeps its own height and a long one scrolls instead of
	// taking the whole panel. The measure's operations are discarded.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	natural := UnitMeshRows(ui, measureGTX).Size.Y
	macro.Stop()
	viewport := natural
	if cap := gtx.Dp(unitMeshSectionMaxDp); viewport > cap {
		viewport = cap
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.Y = viewport
		return PanelScrollView(ui, gtx, &ui.UnitMeshScroll, func(gtx layout.Context) layout.Dimensions { return UnitMeshRows(ui, gtx) })
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// clipEventDefinition is the definition a copied event names, for
// AddTimedEvent to write where the .whe has never had it. The snapshot goes
// over whole, sound entry and all: a definition that names both a sound and an
// FX path is one event, and handing AddTimedEvent its sound path alone made a
// mixed event fail the "sound names start with sound_" guard, which is how a
// copied timeline lost its main effect. A name the copy has no snapshot of
// returns nil, which leaves the .whe's own definition to be used.
func clipEventDefinition(clip *wheEventClip, name string) *whe.FxEvent {
	if clip == nil {
		return nil
	}
	d, ok := clip.Defs[clipEventKey(name)]
	if !ok || len(d.Entries) == 0 {
		return nil
	}
	snapshot := d
	return &snapshot
}

// unitEvtTrackModifiers follows Ctrl and Shift while the section is on
// screen: a pointer press does not carry usable modifiers here, so
// Ctrl+click and Shift+click read the state these key events keep up to date.
//
// Both filters allow any modifier state: a filter's match requires the
// event's modifiers to be exactly the ones it names as Required plus the ones
// it allows as Optional, so a name-only filter would reject the Ctrl press
// itself the moment that event starts carrying ModCtrl — the event never
// reaches this section and the held flag stays false forever. Every modifier
// event seen is logged on change, so a multi-select that never fires shows
// here whether the key events arrived at all and what they carried.
func UnitEvtTrackModifiers(ui *uiState, gtx layout.Context) {
	const anyMod = key.ModCtrl | key.ModShift | key.ModAlt | key.ModSuper
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameCtrl, Optional: anyMod},
			key.Filter{Name: key.NameShift, Optional: anyMod},
		)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if s := fmt.Sprintf("%q %v mods=%v", e.Name, e.State, e.Modifiers); s != ui.UnitEvtModLog {
			LogInfo("evt: modifier key %s", s)
			ui.UnitEvtModLog = s
		}
		switch e.Name {
		case key.NameCtrl:
			ui.UnitEvtCtrlHeld = e.State != key.Release
		case key.NameShift:
			ui.UnitEvtShiftHeld = e.State != key.Release
		}
	}
}

// unitEvtFieldFocused says whether a field of this section holds the caret:
// while one does, Ctrl+C and Ctrl+V belong to the text being edited, not to
// the event clipboard.
func UnitEvtFieldFocused(ui *uiState, gtx layout.Context) bool {
	if TextActive(ui) {
		return true
	}
	for i := range ui.UnitEvtEntryEds {
		if ui.UnitEvtEntryEds[i].Focused(gtx) {
			return true
		}
	}
	if ui.UnitActNameEd.Focused(gtx) {
		return true
	}
	for i := range ui.UnitActCondEds {
		if ui.UnitActCondEds[i].Focused(gtx) {
			return true
		}
	}
	for i := range ui.UnitActNameEds {
		if ui.UnitActNameEds[i].Focused(gtx) {
			return true
		}
	}
	for _, ed := range []*widget.Editor{&ui.UnitEvtNameEd, &ui.UnitEvtTimeEd, &ui.UnitEvtFxEd, &ui.UnitEvtSndEd, &ui.UnitEvtMarkerEd, &ui.UnitEvtShiftEd} {
		if ed.Focused(gtx) {
			return true
		}
	}
	return false
}

// handleUnitEventsInput owns the presses and typing of the Events section. It
// runs on every frame while the section is on screen, ahead of the layout that
// draws what it decided.
func HandleUnitEventsInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != unitSectionEvents && !ClassicOEDialogOpen(ui) {
		// Leaving the section silences whatever it was playing: a timeline
		// that keeps sounding behind another section is a leak, not a feature.
		// The Classic OE double-click dialog is the exception: while it is
		// up, the editors it embeds are the section, wherever the panel sits.
		if ui.EvtPlayer.Playing || ui.EvtPlayer.Audition != nil {
			ui.EvtPlayer.StopAll()
		}
		return
	}
	EnsureUnitEvents(ui)
	// The sound transport first: its clicks are its own, and the playhead it
	// drives must advance before the rows below are read.
	UnitEvtPlayerTick(ui, gtx)
	// The modifiers first: the clicks below are read in this same pass, and a
	// Ctrl held down for a click arrives as key events of its own.
	UnitEvtTrackModifiers(ui, gtx)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtNameEd, evtPickName, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtTimeEd, evtPickNone, true)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtFxEd, evtPickFX, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtSndEd, evtPickSound, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtMarkerEd, evtPickMarker, false)
	UnitEvtUpdateShiftEditor(ui, gtx)
	if ui.UnitEvtDoc == nil {
		return
	}
	// Fill the form from the selected row whenever the selection moves, so
	// re-firing an existing effect is a time edit rather than a retyping.
	SyncUnitEventForm(ui)
	timelines := ui.UnitEvtDoc.Timelines()
	tl := SelectedUnitEventTimeline(ui)
	events := 0
	if tl != nil {
		events = len(tl.Events)
	}
	EnsureUnitEventClicks(ui, len(timelines), events)
	for i := range ui.UnitEvtTlClicks {
		if i < len(timelines) && ui.UnitEvtTlClicks[i].Clicked(gtx) {
			ui.UnitEvtFocusAction = false
			SelectUnitEventTimeline(ui, timelines[i].Name)
			tl = SelectedUnitEventTimeline(ui)
			events = 0
			if tl != nil {
				events = len(tl.Events)
			}
			EnsureUnitEventClicks(ui, len(timelines), events)
		}
	}
	// The action list under the timelines picks which action's conditions the
	// Events column draws beside it, and moves that column onto the actions —
	// the panel shows one or the other, never both.
	acts := ui.UnitEvtDoc.Actions()
	EnsureUnitEvtActionClicks(ui, len(acts))
	for i := range ui.UnitEvtActionClicks {
		if i < len(acts) && ui.UnitEvtActionClicks[i].Clicked(gtx) {
			UnitEvtBlurFields(ui, gtx)
			// The panel leaves the timeline behind completely, and a sound
			// it was auditioning belongs to that timeline.
			if !ui.UnitEvtFocusAction {
				ui.EvtPlayer.StopAll()
			}
			ui.UnitEvtFocusAction = true
			ui.UnitEvtActionSel = i
			// Selecting an action also plays it: every clip its graph
			// resolves to, combined in the viewport beside the panel.
			ActivateUnitAction(ui, acts[i].Name)
		}
	}
	// While an action is the panel on screen, its conditions are the fields
	// being edited: their typing, the guard chips and the commit below.
	if ui.UnitEvtFocusAction {
		UnitActConditionsInput(ui, gtx)
	}
	for i := range ui.UnitEvtClicks {
		if i >= events || !ui.UnitEvtClicks[i].Clicked(gtx) {
			continue
		}
		// A row click is a selection, and it lets go of any field being typed
		// into so the caret does not hold the clipboard shortcuts hostage.
		// Plain picks the one row, Ctrl toggles it in the group, and Shift
		// takes the range from the anchor the last plain or Ctrl click left.
		UnitEvtBlurFields(ui, gtx)
		// The modifiers are read from the OS at the click itself. The
		// tracked flags depend on this section's key filters winning the
		// Ctrl/Shift events ahead of every other filter in the frame, and a
		// selection that appears only when that race is won is no selection
		// at all; the live key state cannot race. The tracked flags stay as
		// the fallback where there is no live reader.
		live := heldModifiers()
		ctrl := ui.UnitEvtCtrlHeld || live.Contain(key.ModCtrl)
		shift := ui.UnitEvtShiftHeld || live.Contain(key.ModShift)
		LogInfo("evt: row %d click ctrl=%v shift=%v (tracked %v/%v live=%v anchor=%d)",
			i, ctrl, shift, ui.UnitEvtCtrlHeld, ui.UnitEvtShiftHeld, live, ui.UnitEvtSelAnchor)
		switch {
		case ctrl:
			if ui.UnitEvtSelSet == nil {
				ui.UnitEvtSelSet = map[int]bool{}
			}
			if ui.UnitEvtSelSet[i] {
				delete(ui.UnitEvtSelSet, i)
			} else {
				ui.UnitEvtSelSet[i] = true
			}
			ui.UnitEvtSelAnchor = i
		case shift:
			lo, hi := i, ui.UnitEvtSelAnchor
			if lo > hi {
				lo, hi = hi, lo
			}
			ui.UnitEvtSelSet = map[int]bool{}
			for j := lo; j <= hi && j < events; j++ {
				ui.UnitEvtSelSet[j] = true
			}
		default:
			ui.UnitEvtSelSet = map[int]bool{i: true}
			ui.UnitEvtSelAnchor = i
		}
		ui.UnitEvtSel = i
		ui.UnitEvtBound = ""
		break
	}
	// Rows can disappear under a Remove, so the group loses what no longer
	// exists before anything reads it.
	for i := range ui.UnitEvtSelSet {
		if i >= events {
			delete(ui.UnitEvtSelSet, i)
		}
	}
	if ui.UnitEvtAddFX.Clicked(gtx) {
		AddUnitEventFromForm(ui, "fx")
		return
	}
	if ui.UnitEvtAddEvent.Clicked(gtx) {
		AddUnitEventFromForm(ui, "event")
		return
	}
	if ui.UnitEvtAddSound.Clicked(gtx) {
		AddUnitEventFromForm(ui, "sound")
		return
	}
	if ui.UnitEvtRemove.Clicked(gtx) {
		RemoveSelectedUnitEvent(ui)
		return
	}
	// The animation list's per-row Remove buttons each drop one blend entry
	// from the selected timeline. They are only laid out for a selected
	// timeline, so with none (or while an action is shown) no press can land.
	if tl != nil {
		EnsureUnitEventAnimClicks(ui, len(tl.Animations))
		for i := range ui.UnitEvtAnimClicks {
			if i < len(tl.Animations) && ui.UnitEvtAnimClicks[i].Clicked(gtx) {
				RemoveUnitTimelineAnimation(ui, i)
				return
			}
		}
	}
	if ui.UnitEvtSave.Clicked(gtx) {
		SaveUnitEventsFromPanel(ui)
		return
	}
	if ui.UnitEvtRevert.Clicked(gtx) {
		RevertUnitEvents(ui)
		return
	}
	if ui.UnitEvtSet.Clicked(gtx) || ui.UnitEvtApply.Clicked(gtx) {
		ApplySelectedEventForm(ui)
		return
	}
	if ui.UnitEvtShift.Clicked(gtx) {
		ShiftSelectedUnitEventTimes(ui)
		return
	}
	if ui.UnitEvtCopyAct.Clicked(gtx) {
		CopySelectedUnitAction(ui)
		return
	}
	if ui.UnitEvtPasteAct.Clicked(gtx) {
		PasteUnitAction(ui)
		return
	}
	if ui.UnitEvtCopy.Clicked(gtx) {
		CopySelectedUnitEvents(ui)
		return
	}
	if ui.UnitEvtCopyTl.Clicked(gtx) {
		CopySelectedUnitTimeline(ui)
		return
	}
	if ui.UnitEvtPaste.Clicked(gtx) {
		PasteUnitEvents(ui)
		return
	}
	if ui.UnitClipPrev.Clicked(gtx) {
		StepUnitClip(ui, -1)
	}
	if ui.UnitClipNext.Clicked(gtx) {
		StepUnitClip(ui, 1)
	}
	// Ctrl+C and Ctrl+V repeat those two buttons — unless a field holds the
	// caret, for while it does the text being edited keeps its own clipboard.
	if !UnitEvtFieldFocused(ui, gtx) {
		for {
			ev, ok := gtx.Event(
				key.Filter{Name: "C", Required: key.ModCtrl, Optional: key.ModShift},
				key.Filter{Name: "V", Required: key.ModCtrl},
			)
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			switch e.Name {
			case "C":
				CopySelectedUnitEvents(ui)
			case "V":
				PasteUnitEvents(ui)
			}
		}
	}
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtNameEd, evtPickName, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtTimeEd, evtPickNone, true)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtFxEd, evtPickFX, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtSndEd, evtPickSound, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtMarkerEd, evtPickMarker, false)
	UnitEvtUpdateShiftEditor(ui, gtx)
}

// unitEvtUpdateEditor processes one form field's typing. A press into the row
// focuses it, the same way the attribute editor's value field does. Enter
// submits: it applies the fields the way Set does and never drops a line
// break into the value.
//
// batchTime says the field belongs to the whole selection rather than to the
// one row the form shows. Only the moment does: Enter in the path or marker
// field while several rows are selected must not quietly move all of their
// times to whichever moment the primary row happened to have.
func UnitEvtUpdateEditor(ui *uiState, gtx layout.Context, ed *widget.Editor, pick evtPickTarget, batchTime bool) {
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		// A field with a list opens its picker instead of taking the caret: the
		// picker's own filter takes the typing.
		if pick != evtPickNone {
			OpenEvtPicker(ui, pick)
		} else {
			gtx.Execute(key.FocusCmd{Tag: ed})
		}
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			if batchTime {
				ApplySelectedEventForm(ui)
			} else {
				ApplySelectedEventFormSingle(ui)
			}
			ed.Blur(gtx)
		}
	}
}

// unitEvtUpdateShiftEditor owns the batch shift field: a click takes the
// caret, Enter shifts the selection and consumes the number. It is kept out of
// unitEvtUpdateEditor because submitting it is not an apply of the form — the
// name, path and marker fields have nothing to do with a shift.
func UnitEvtUpdateShiftEditor(ui *uiState, gtx layout.Context) {
	ed := &ui.UnitEvtShiftEd
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ShiftSelectedUnitEventTimes(ui)
			ed.Blur(gtx)
		}
	}
}

// addUnitEventFromForm fires an event on the selected timeline. An FX keeps
// its marker binding from the definition it names; a sound names the folder
// under data/sound it resolves to. Filling in no path at all re-fires an
// event the .whe already defines, which is how an existing effect is put on a
// second clip.
func AddUnitEventFromForm(ui *uiState, kind string) {
	ApplyUnitEvent(ui, kind)
}

func ApplyUnitEvent(ui *uiState, kind string) {
	ui.LastFormAction = kind
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.UnitEvtFormErr = "No .whe is loaded."
		ui.LastFormAction = ""
		return
	}
	if ui.UnitEvtTimeline == "" {
		ui.UnitEvtFormErr = "Select a timeline first."
		ui.LastFormAction = ""
		return
	}
	name := strings.TrimSpace(ui.UnitEvtNameEd.Text())
	if name == "" {
		ui.UnitEvtFormErr = "Event name is required."
		ui.LastFormAction = ""
		return
	}
	at, err := unitEventTime(ui.UnitEvtTimeEd.Text())
	if err != nil {
		ui.UnitEvtFormErr = err.Error()
		ui.LastFormAction = ""
		return
	}
	// A sound takes its path from the sound field, an effect from the FX field;
	// an event that is only being placed takes neither.
	target := strings.TrimSpace(ui.UnitEvtFxEd.Text())
	if kind == "sound" {
		target = strings.TrimSpace(ui.UnitEvtSndEd.Text())
	}
	var definition *whe.FxEvent
	soundPath := ""
	switch kind {
	case "sound":
		if target == "" {
			ui.UnitEvtFormErr = "A sound path is required (relative to data\\sound)."
			ui.LastFormAction = ""
			return
		}
		if !strings.HasPrefix(strings.ToLower(name), "sound") {
			name = "sound_" + name
			ui.UnitEvtNameEd.SetText(name)
		}
		soundPath = target
	case "event":
		// Placing an event on the timeline needs no definition of its own: the
		// event must already be defined in this .whe, which AddTimedEvent checks.
	default:
		marker := strings.TrimSpace(ui.UnitEvtMarkerEd.Text())
		var entries []whe.FxEventEntry
		if marker != "" {
			entries = append(entries, whe.FxEventEntry{Key: "marker", Value: marker})
		}
		if target != "" {
			entries = append(entries, whe.FxEventEntry{Key: "fx", Value: target})
		}
		if len(entries) > 0 {
			definition = &whe.FxEvent{Name: name, Entries: entries}
		}
	}

	if err := doc.AddTimedEvent(ui.UnitEvtTimeline, whe.FxTimelineEvent{Name: name, Time: at}, definition, soundPath); err != nil {
		ui.UnitEvtFormErr = err.Error()
		ui.LastFormAction = ""
		return
	}

	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	ui.LastFormAction = kind
	// Land the selection on the row that was just added.
	if tl := SelectedUnitEventTimeline(ui); tl != nil {
		for i := range tl.Events {
			if strings.EqualFold(tl.Events[i].Name, name) && tl.Events[i].Time == at {
				ui.UnitEvtSel = i
				ui.UnitEvtSelSet = map[int]bool{i: true}
				ui.UnitEvtSelAnchor = i
			}
		}
		ui.UnitEvtBound = ""
	}
	ReloadUnitModelFx(ui)
	StatusEventAdded(ui, name, at)
}

// removeSelectedUnitEvent drops the selected rows from its timeline — every
// row of the group, not just the one showing below. The definitions themselves
// are kept: other clips and the blueprint may still fire them, and an orphan
// definition is harmless where a missing one is a broken effect.
func RemoveSelectedUnitEvent(ui *uiState) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.UnitEvtFormErr = "Select an event to remove."
		ui.LastFormAction = ""
		return
	}
	indices := UnitEvtSelectedIndices(ui)
	if len(indices) == 0 {
		ui.UnitEvtFormErr = "Select an event to remove."
		ui.LastFormAction = ""
		return
	}
	// Descending, so taking a row cannot shift the ones still to come.
	removed := 0
	for k := len(indices) - 1; k >= 0; k-- {
		i := indices[k]
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		if err := doc.RemoveTimedEvent(ui.UnitEvtTimeline, i); err != nil {
			ui.UnitEvtFormErr = err.Error()
			ui.LastFormAction = ""
			return
		}
		removed++
	}
	if removed == 0 {
		ui.UnitEvtFormErr = "Select an event to remove."
		ui.LastFormAction = ""
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	ui.LastFormAction = "remove"
	ui.UnitEvtSelSet = map[int]bool{}
	if tl := SelectedUnitEventTimeline(ui); tl != nil {
		if ui.UnitEvtSel >= len(tl.Events) {
			ui.UnitEvtSel = len(tl.Events) - 1
		}
		// The selection lands on whichever row took the removed one's place,
		// so the panel below and the buttons keep something to act on.
		if ui.UnitEvtSel >= 0 {
			ui.UnitEvtSelSet[ui.UnitEvtSel] = true
		}
	}
	ui.UnitEvtBound = ""
	ReloadUnitModelFx(ui)
	ui.StatusText = fmt.Sprintf("Removed %d event(s) from %s — save to write it into the mod", removed, ui.UnitEvtTimeline)
}

// removeUnitTimelineAnimation drops one entry from the selected timeline's
// animation blend list. It edits the in-memory document only — Save .whe
// writes it into the mod, matching how every other timeline edit behaves.
func RemoveUnitTimelineAnimation(ui *uiState, index int) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.UnitEvtFormErr = "Select a timeline's animation to remove."
		ui.LastFormAction = ""
		return
	}
	if index < 0 || index >= len(tl.Animations) {
		ui.UnitEvtFormErr = "Animation selection is no longer valid."
		ui.LastFormAction = ""
		return
	}
	if err := doc.RemoveTimelineAnimation(ui.UnitEvtTimeline, index); err != nil {
		ui.UnitEvtFormErr = err.Error()
		ui.LastFormAction = ""
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	ui.LastFormAction = "remove"
	ReloadUnitModelFx(ui)
	ui.StatusText = fmt.Sprintf("Removed an animation from %s — save to write it into the mod", ui.UnitEvtTimeline)
}

// pasteTimelineClip writes a copied timeline into doc under its own name: it is
// created from the copied payload when the file has never carried it, and
// merged into the one it has when it does. Rows the destination already fires
// at the same moment are left alone, and a definition the file already defines
// is used as it stands — reconciling two authored effects is the reader's call.
func pasteTimelineClip(doc *whe.Document, clip *wheEventClip) (timelinePaste, error) {
	var res timelinePaste
	if doc == nil || clip == nil || strings.TrimSpace(clip.Timeline) == "" {
		return res, fmt.Errorf("nothing to paste")
	}
	res.Name = strings.TrimSpace(clip.Timeline)
	if _, exists := doc.TimelinePayload(res.Name); !exists {
		if err := doc.AddTimeline(res.Name, clip.Payload); err != nil {
			return res, err
		}
		res.Created = true
	}
	for _, anim := range clip.Animations {
		wrote, err := doc.AddTimelineAnimation(res.Name, anim)
		if err != nil {
			return res, err
		}
		if wrote {
			res.Anims++
		}
	}
	// The definitions this .whe already carries, kept up to date as the paste
	// writes more of them; only the created path needs the set. An existing
	// timeline is read by slot, so a paste into one that half carries the copy
	// cannot double every row.
	known := map[string]bool{}
	if res.Created {
		for name := range doc.Events() {
			known[name] = true
		}
	} else if tl, ok := timelineOf(doc, res.Name); ok {
		for _, ev := range tl.Events {
			known[fxEventSlot(ev)] = true
		}
	}
	for _, ev := range clip.Events {
		if res.Created {
			// The timeline was written from the copied payload, so its events
			// are already listed; only the definitions behind them still have
			// to come over. One this .whe already has is left exactly as it is.
			key := clipEventKey(ev.Name)
			if d, have := clip.Defs[key]; have && !known[key] {
				if err := doc.AddEventDefinition(d); err != nil {
					res.Skipped++
					if res.FirstSkip == "" {
						res.FirstSkip = err.Error()
					}
					continue
				}
				known[key] = true
			}
			res.Events++
			continue
		}
		if known[fxEventSlot(ev)] {
			res.Present++
			continue
		}
		if err := doc.AddTimedEvent(res.Name, ev, clipEventDefinition(clip, ev.Name), ""); err != nil {
			res.Skipped++
			if res.FirstSkip == "" {
				res.FirstSkip = err.Error()
			}
			continue
		}
		known[fxEventSlot(ev)] = true
		res.Events++
	}
	return res, nil
}

// pasteUnitEvents writes the buffer's events into a timeline at their original
// moments, bringing the definitions Copy snapshotted along. A name the .whe
// already defines differently is not overwritten — AddTimedEvent says so — and
// that event is skipped and reported instead, so reconciling two authored
// markers stays the reader's call.
//
// A copy of a few rows goes into the timeline that is selected. A copy of a
// whole timeline goes to the timeline of its own name: merged into the one
// this .whe already carries, or created here from the copied payload when the
// file has never had one — its name is the clip it drives, so that is where it
// belongs. The copied name is never silently written under another one.
func PasteUnitEvents(ui *uiState) {
	entry, _, ok := ui.UnitClips.Pick(clipEvents, ui.UnitClipCur)
	if !ok || entry.Events == nil || len(entry.Events.Events) == 0 {
		ui.StatusText = "The event clipboard is empty — Copy first"
		return
	}
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.StatusText = "No .whe is loaded — select a unit whose model has one first"
		return
	}
	clip := entry.Events
	if clip.Timeline != "" {
		res, err := pasteTimelineClip(doc, clip)
		if err != nil {
			ui.StatusText = "Paste failed: " + err.Error()
			return
		}
		if res.Events == 0 && res.Present == 0 {
			if res.FirstSkip != "" {
				ui.StatusText = "Paste failed: " + res.FirstSkip
			} else {
				ui.StatusText = fmt.Sprintf("Nothing to paste — %s carries no events", res.Name)
			}
			return
		}
		ui.UnitEvtDirty = true
		ui.UnitEvtBound = ""
		// A .whe read as carrying no timelines may have just gained one.
		ui.UnitEvtErr = ""
		ui.UnitEvtTimeline = res.Name
		ui.UnitEvtSel = 0
		ui.UnitEvtSelSet = map[int]bool{0: true}
		ui.UnitEvtSelAnchor = 0
		ReloadUnitModelFx(ui)
		ui.StatusText = timelinePasteMessage(res)
		return
	}
	target := ui.UnitEvtTimeline
	if target == "" {
		ui.StatusText = "Select a timeline first"
		return
	}
	added, skipped, present := 0, 0, 0
	firstSkip := ""
	var pasted []whe.FxTimelineEvent
	// An event the destination already fires at the same moment is not a
	// conflict to report, it is already there: pasting the same rows twice
	// must not double every row.
	known := map[string]bool{}
	if tl, ok := timelineOf(doc, target); ok {
		for _, ev := range tl.Events {
			known[fxEventSlot(ev)] = true
		}
	}
	for _, ev := range clip.Events {
		if known[fxEventSlot(ev)] {
			present++
			continue
		}
		if err := doc.AddTimedEvent(target, ev, clipEventDefinition(clip, ev.Name), ""); err != nil {
			skipped++
			if firstSkip == "" {
				firstSkip = err.Error()
			}
			continue
		}
		known[fxEventSlot(ev)] = true
		added++
		pasted = append(pasted, ev)
	}
	if added == 0 {
		if present > 0 {
			ui.StatusText = fmt.Sprintf("Nothing to paste — %d event(s) are already in %s", present, target)
			return
		}
		ui.StatusText = "Paste failed: " + firstSkip
		return
	}
	ui.UnitEvtDirty = true
	ui.UnitEvtBound = ""
	// A .whe read as carrying no timelines may have just gained one.
	ui.UnitEvtErr = ""
	// Pasted rows land at the end of the timeline; the selection follows them
	// so what was just pasted is what the panels below show.
	ui.UnitEvtSelSet = map[int]bool{}
	if tl := SelectedUnitEventTimeline(ui); tl != nil {
		for _, ev := range pasted {
			for i := len(tl.Events) - 1; i >= 0; i-- {
				if tl.Events[i].Name == ev.Name && tl.Events[i].Time == ev.Time {
					ui.UnitEvtSelSet[i] = true
					ui.UnitEvtSel = i
					break
				}
			}
		}
	}
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Pasted %d event(s) into %s", added, target)
	if present > 0 {
		msg += fmt.Sprintf(", %d already there", present)
	}
	if skipped > 0 {
		msg += fmt.Sprintf(", %d skipped: %s", skipped, firstSkip)
	}
	ui.StatusText = msg
}

// applySelectedEventForm is the panel's Apply and Set: with a group selected
// it writes the typed moment into every selected row, and with a single row
// selected it writes the whole form — time, path and marker — into that row.
func ApplySelectedEventForm(ui *uiState) {
	ApplyEventForm(ui, true)
}

// applySelectedEventFormSingle is the path, marker and name fields' own Enter:
// those describe the one row the form shows, so they never fan out over the
// selection.
func ApplySelectedEventFormSingle(ui *uiState) {
	ApplyEventForm(ui, false)
}

// applyEventForm writes the form's typed fields into the selected event of the
// in-memory document: the moment it fires at on the timeline and the path and
// marker its definition carries. Nothing touches the file — Save .whe does
// that — and the viewport re-resolves from the document, so the edited event
// shows on the model before it is written out. The event's name is its identity
// across the file, so a changed name is refused rather than half-renamed.
func ApplyEventForm(ui *uiState, allowBatchTime bool) {
	if ui.UnitEvtDoc == nil {
		ui.UnitEvtFormErr = "No .whe is loaded."
		return
	}
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events) {
		ui.UnitEvtFormErr = "Select an event to apply the fields to."
		return
	}
	// A group selection is the batch form of this button. The moment an event
	// fires at is the one field that means the same thing for every row — a
	// name is each row's own identity, and a path and marker belong to the
	// definition they came from — so a multi-row Apply writes the typed time
	// to all selected rows and leaves the rest of the form to the one row it
	// shows.
	if indices := UnitEvtSelectedIndices(ui); allowBatchTime && len(indices) > 1 {
		at, err := unitEventTime(ui.UnitEvtTimeEd.Text())
		if err != nil {
			ui.UnitEvtFormErr = err.Error()
			return
		}
		ApplyUnitEventTimes(ui, indices, at)
		return
	}
	ev := tl.Events[ui.UnitEvtSel]
	if name := strings.TrimSpace(ui.UnitEvtNameEd.Text()); !strings.EqualFold(name, ev.Name) {
		ui.UnitEvtFormErr = "The event name is this event's identity — keep it as selected when applying."
		return
	}
	at, err := unitEventTime(ui.UnitEvtTimeEd.Text())
	if err != nil {
		ui.UnitEvtFormErr = err.Error()
		return
	}
	def0 := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	path := strings.TrimSpace(ui.UnitEvtFxEd.Text())
	if _, isSound := def0.First("sound"); isSound || strings.HasPrefix(strings.ToLower(ev.Name), "sound") {
		path = strings.TrimSpace(ui.UnitEvtSndEd.Text())
	}
	marker := strings.TrimSpace(ui.UnitEvtMarkerEd.Text())
	def, hasDef := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !hasDef && (path != "" || marker != "") {
		ui.UnitEvtFormErr = "This event has no definition in the .whe — path and marker cannot be applied."
		return
	}
	pathKey := "fx"
	if _, isSound := def.First("sound"); isSound || strings.HasPrefix(strings.ToLower(ev.Name), "sound") {
		pathKey = "sound"
	}
	pathCur, _ := def.First(pathKey)
	markerCur, _ := def.First("marker")
	timeChanged := at != ev.Time
	pathChanged := path != strings.TrimSpace(pathCur)
	markerChanged := marker != strings.TrimSpace(markerCur)
	if !timeChanged && !pathChanged && !markerChanged {
		ui.UnitEvtFormErr = ""
		ui.StatusText = "no changes to apply"
		return
	}
	// The time is written first and the definition after it, so a definition
	// that cannot be written leaves the moment already correct in memory and
	// marked as an edit rather than silently dropped.
	wrote := false
	if timeChanged {
		if err := ui.UnitEvtDoc.SetTimedEventTime(ui.UnitEvtTimeline, ui.UnitEvtSel, at); err != nil {
			ui.UnitEvtFormErr = err.Error()
			if wrote {
				ReloadUnitModelFx(ui)
			}
			return
		}
		wrote = true
	}
	if pathChanged {
		if err := ui.UnitEvtDoc.SetEventEntryByKey(ev.Name, pathKey, path); err != nil {
			ui.UnitEvtFormErr = err.Error()
			if wrote {
				ReloadUnitModelFx(ui)
			}
			return
		}
		wrote = true
	}
	if markerChanged {
		if err := ui.UnitEvtDoc.SetEventEntryByKey(ev.Name, "marker", marker); err != nil {
			ui.UnitEvtFormErr = err.Error()
			if wrote {
				ReloadUnitModelFx(ui)
			}
			return
		}
		wrote = true
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	// The form and the definition's value fields refill from what was just
	// written, so what is shown is what the document holds.
	ui.UnitEvtBound = ""
	ui.UnitEvtEntryBound = ""
	ui.UnitActCondBound = ""
	ReloadUnitModelFx(ui)
	ui.StatusText = "Applied to " + ev.Name + " — Save .whe to write it into the mod"
}

// shiftSelectedUnitEventTimes moves every selected row of the timeline by the
// typed number of seconds. Everything the selection holds moves together — an
// FX, a sound, an FX bound to a marker — because the shift acts on when a row
// fires, not on what it fires.
//
// The entry and exit sentinels stay where they are: "on enter" and "on leave"
// are not moments a shift can move. A row that would land before the clip
// starts stops at 0s instead of firing at a negative time. The field is
// emptied by a shift that moved anything, so a second press cannot move the
// same rows again.
func ShiftSelectedUnitEventTimes(ui *uiState) {
	if ui.UnitEvtDoc == nil {
		ui.UnitEvtFormErr = "No .whe is loaded."
		return
	}
	delta, err := unitEventShift(ui.UnitEvtShiftEd.Text())
	if err != nil {
		ui.UnitEvtFormErr = err.Error()
		return
	}
	tl := SelectedUnitEventTimeline(ui)
	indices := UnitEvtSelectedIndices(ui)
	if len(indices) == 0 && ui.UnitEvtSel >= 0 {
		// A single row is a selection of one: the shift works without the
		// Ctrl+click ritual the batch tools otherwise need.
		indices = []int{ui.UnitEvtSel}
	}
	if tl == nil || len(indices) == 0 {
		ui.UnitEvtFormErr = "Select the events to shift."
		return
	}
	if delta == 0 {
		ui.UnitEvtFormErr = ""
		ui.StatusText = "A shift of 0s moves nothing"
		return
	}
	moved, sentinel, clamped := 0, 0, 0
	firstErr := ""
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		cur := tl.Events[i].Time
		if cur < 0 || math.IsInf(cur, 1) || cur > 1e30 {
			sentinel++
			continue
		}
		at := cur + delta
		if at < 0 {
			at = 0
			clamped++
		}
		if err := ui.UnitEvtDoc.SetTimedEventTime(ui.UnitEvtTimeline, i, at); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		moved++
	}
	if moved == 0 {
		if firstErr != "" {
			ui.UnitEvtFormErr = firstErr
			return
		}
		ui.UnitEvtFormErr = ""
		if sentinel > 0 {
			ui.StatusText = fmt.Sprintf("Nothing shifted — %d event(s) fire on enter/leave, which have no moment to move", sentinel)
			return
		}
		ui.StatusText = "Nothing shifted"
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	// The form refills from the row it shows, whose moment has just moved.
	ui.UnitEvtBound = ""
	ui.UnitEvtShiftEd.SetText("")
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Shifted %d event(s) by %s", moved, unitEventShiftLabel(delta))
	if sentinel > 0 {
		msg += fmt.Sprintf(", %d on enter/leave left as they were", sentinel)
	}
	if clamped > 0 {
		msg += fmt.Sprintf(", %d stopped at 0s", clamped)
	}
	if firstErr != "" {
		msg += ", some refused: " + firstErr
	}
	ui.StatusText = msg + " — Save .whe to write it into the mod"
}

// applyUnitEventTimes writes one moment into every selected row of the
// timeline — the batch edit behind Apply while a group is selected. Events
// that already fire at that moment are counted rather than rewritten, and a
// row the document refuses is reported without stopping the rest: one damaged
// row must not leave the group half-moved with no word about it.
func ApplyUnitEventTimes(ui *uiState, indices []int, at float64) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.UnitEvtFormErr = "Select an event to apply the fields to."
		return
	}
	moved, same := 0, 0
	firstErr := ""
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		if tl.Events[i].Time == at {
			same++
			continue
		}
		if err := doc.SetTimedEventTime(ui.UnitEvtTimeline, i, at); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		moved++
	}
	if moved == 0 {
		if firstErr != "" {
			ui.UnitEvtFormErr = firstErr
			return
		}
		ui.UnitEvtFormErr = ""
		ui.StatusText = fmt.Sprintf("All %d selected event(s) already start at %s", same, unitEventTimeLabel(at))
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	// The time field refills from the row the form shows, which has just moved.
	ui.UnitEvtBound = ""
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Set %s on %d event(s)", unitEventTimeLabel(at), moved)
	if same > 0 {
		msg += fmt.Sprintf(", %d already there", same)
	}
	if firstErr != "" {
		msg += ", some refused: " + firstErr
	}
	ui.StatusText = msg + " — Save .whe to write it into the mod"
}

// saveUnitEvents writes the edited .whe into the mod's own overlay, leaving
// the installed file and the SGA archive untouched.
func SaveUnitEvents(ui *uiState) (string, error) {
	if ui.UnitEvtDoc == nil {
		return "", fmt.Errorf("no .whe is loaded")
	}
	if ui.UnitEvtPath == "" {
		return "", fmt.Errorf("no .whe path to save")
	}
	if !ui.UnitEvtDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.ModTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	data, err := ui.UnitEvtDoc.MarshalBinary()
	if err != nil {
		return "", err
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitEvtPath, data)
	if err != nil {
		return "", err
	}
	ui.UnitEvtDirty = false
	ReloadUnitModelFx(ui)
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(ui.UnitEvtPath), disk, len(data)), nil
}

func SaveUnitEventsFromPanel(ui *uiState) {
	line, err := SaveUnitEvents(ui)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = line
	MarkStateDirty(ui)
}

// revertUnitEvents throws the in-memory edits away and re-reads the file from
// the tree, which is the way back from an event added by mistake.
func RevertUnitEvents(ui *uiState) {
	forced := ui.UnitEvtFor
	ui.UnitEvtDoc = nil
	ui.UnitEvtFor = ""
	ui.UnitEvtTried = false
	EnsureUnitEvents(ui)
	if ui.UnitEvtFor == "" {
		ui.UnitEvtFor = forced
	}
	ui.UnitEvtDirty = false
	ui.UnitEvtFormErr = ""
	ui.LastFormAction = ""
	ui.UnitEvtSel = 0
	ui.UnitEvtSelSet = map[int]bool{0: true}
	ui.UnitEvtSelAnchor = 0
	ui.UnitEvtBound = ""
	ui.UnitEvtEntryBound = ""
	ui.UnitActCondBound = ""
	ui.StatusText = "Changes to " + filepath.Base(ui.UnitEvtPath) + " thrown away"
	// The document is the file's own copy again, so the viewport resolves it
	// rather than the edits that were thrown away.
	ReloadUnitModelFx(ui)
}

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

// pasteUnitAction writes the copied action, its timelines and its sub-actions
// into this unit's .whe. The timelines go in first — created when this file has
// never had them, merged into the ones it has — so the action's motions resolve
// the moment it lands, then the sub-actions, and the action itself last.
//
// An action of the same name here is refused before anything is written rather
// than merged: an action's choices are an if / else chain in file order, and
// splicing one into another would change what this unit plays.
func PasteUnitAction(ui *uiState) {
	entry, _, ok := ui.UnitClips.Pick(clipActions, ui.UnitClipCur)
	if !ok || entry.Actions == nil {
		ui.StatusText = "The action clipboard is empty — Copy an action first"
		return
	}
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.StatusText = "No .whe is loaded — select a unit whose model has one first"
		return
	}
	clip := entry.Actions
	if actionIndexByName(doc, clip.Action.Name) >= 0 {
		ui.StatusText = fmt.Sprintf("action %s already exists in this .whe — reconcile it by hand instead of pasting over it",
			clip.Action.Name)
		return
	}
	createdTl, mergedTl, events, anims := 0, 0, 0, 0
	firstErr := ""
	for i := range clip.Timelines {
		res, err := pasteTimelineClip(doc, &clip.Timelines[i])
		if err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		if res.Created {
			createdTl++
		} else {
			mergedTl++
		}
		events += res.Events
		anims += res.Anims
	}
	addedSubs, presentSubs := 0, 0
	for _, sub := range clip.Subacts {
		if actionIndexByName(doc, sub.Name) >= 0 {
			// The target has its own version of that action: keep it, and let
			// the reader settle the two by hand.
			presentSubs++
			continue
		}
		if err := doc.AddAction(sub); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		addedSubs++
	}
	if err := doc.AddAction(clip.Action); err != nil {
		// The timelines and sub-actions may already have landed, and Save .whe
		// has to know the document no longer matches the file.
		if createdTl+mergedTl+addedSubs > 0 {
			ui.UnitEvtDirty = true
			ui.UnitActCondBound = ""
		}
		ui.StatusText = "Paste failed: " + err.Error()
		return
	}
	ui.UnitEvtDirty = true
	ui.UnitActCondBound = ""
	// The pasted action is what the list should show as picked, so the panel
	// below it describes the thing that just landed.
	if i := actionIndexByName(doc, clip.Action.Name); i >= 0 {
		ui.UnitEvtActionSel = i
	}
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Pasted action %s", clip.Action.Name)
	if createdTl+mergedTl > 0 {
		msg += fmt.Sprintf(" with %d timeline(s) (%d new, %d merged, %d event(s)", createdTl+mergedTl, createdTl, mergedTl, events)
		if anims > 0 {
			msg += fmt.Sprintf(", %d animation(s)", anims)
		}
		msg += ")"
	}
	if addedSubs > 0 {
		msg += fmt.Sprintf(" and %d sub-action(s)", addedSubs)
	}
	if presentSubs > 0 {
		msg += fmt.Sprintf(", %d sub-action(s) already here", presentSubs)
	}
	if firstErr != "" {
		msg += ", some skipped: " + firstErr
	}
	ui.StatusText = msg + " — Save .whe to write it in"
}

// unitActConditionsInput runs the presses and typing of the action panel: the
// guard chips cycle a choice's if / else if / else / always step, the
// condition and name fields take the typing, Apply or Enter commits them,
// and the chain tools add, move and delete choices around the caret.
func UnitActConditionsInput(ui *uiState, gtx layout.Context) {
	EnsureUnitActConditions(ui)
	UnitActUpdateNameEditor(ui, gtx)
	if ui.UnitActRename.Clicked(gtx) {
		RenameSelectedUnitAction(ui)
	}
	for i := range ui.UnitActGuardClicks {
		if i >= len(ui.UnitActCondStaged) || !ui.UnitActGuardClicks[i].Clicked(gtx) {
			continue
		}
		ui.UnitActSel = i
		cmp := ui.UnitActCondStaged[i]
		if cmp < whe.FxCompareIf || cmp >= whe.FxCompareNone {
			// A press starts the chain over from if — including for a compare
			// value outside the four the format defines.
			ui.UnitActCondStaged[i] = whe.FxCompareIf
			continue
		}
		ui.UnitActCondStaged[i] = cmp + 1
	}
	for i := range ui.UnitActCondEds {
		UnitActUpdateEditor(ui, gtx, &ui.UnitActCondEds[i], i)
	}
	for i := range ui.UnitActNameEds {
		UnitActUpdateEditor(ui, gtx, &ui.UnitActNameEds[i], i)
	}
	// Whichever field holds the caret is the selected choice: the move, add
	// and delete buttons of the footer act on it.
	for i := range ui.UnitActCondEds {
		if ui.UnitActCondEds[i].Focused(gtx) || (i < len(ui.UnitActNameEds) && ui.UnitActNameEds[i].Focused(gtx)) {
			ui.UnitActSel = i
		}
	}
	if ui.UnitActApply.Clicked(gtx) {
		ApplyUnitActConditions(ui)
	}
	if ui.UnitActAddIf.Clicked(gtx) {
		UnitActAddChoice(ui, actAddIf)
	}
	if ui.UnitActAddElseIf.Clicked(gtx) {
		UnitActAddChoice(ui, actAddElseIf)
	}
	if ui.UnitActAddElse.Clicked(gtx) {
		UnitActAddChoice(ui, actAddElse)
	}
	if ui.UnitActMoveUp.Clicked(gtx) {
		UnitActMoveChoice(ui, -1)
	}
	if ui.UnitActMoveDown.Clicked(gtx) {
		UnitActMoveChoice(ui, 1)
	}
	if ui.UnitActDelete.Clicked(gtx) {
		UnitActDeleteChoice(ui)
	}
}

// unitActUpdateNameEditor owns the action's own name field: a click takes the
// caret, Enter renames, and the field refills from the picked action whenever
// the selection moves — but not while it holds the caret, so what is being
// typed is never overwritten.
func UnitActUpdateNameEditor(ui *uiState, gtx layout.Context) {
	ed := &ui.UnitActNameEd
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	if !ed.Focused(gtx) {
		var act whe.FxAction
		have := false
		if ui.UnitEvtDoc != nil {
			if a := SelectedUnitEvtAction(ui, ui.UnitEvtDoc.Actions()); a != nil {
				act = *a
				have = true
			}
		}
		bound := strconv.Itoa(ui.UnitEvtActionSel) + "|" + act.Name
		if bound != ui.UnitActNameBound {
			ui.UnitActNameBound = bound
			if have {
				ed.SetText(act.Name)
			} else {
				ed.SetText("")
			}
		}
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			RenameSelectedUnitAction(ui)
			ed.Blur(gtx)
		}
	}
}

// renameSelectedUnitAction renames the picked action in the in-memory
// document. The rename also follows the name into every sub-action link of the
// file that called it, so a chain another action drives keeps working; a name
// another action already has is refused by the document rather than merged.
// Save .whe writes it into the mod.
func RenameSelectedUnitAction(ui *uiState) {
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.UnitActErr = "No .whe is loaded."
		return
	}
	acts := doc.Actions()
	if ui.UnitEvtActionSel < 0 || ui.UnitEvtActionSel >= len(acts) {
		ui.UnitActErr = "Select an action to rename."
		return
	}
	old := acts[ui.UnitEvtActionSel].Name
	next := strings.TrimSpace(ui.UnitActNameEd.Text())
	if next == "" {
		ui.UnitActErr = "An action needs a name."
		return
	}
	if strings.EqualFold(next, old) {
		ui.UnitActErr = ""
		ui.StatusText = "The action is already called " + old
		return
	}
	if err := doc.RenameAction(old, next); err != nil {
		ui.UnitActErr = err.Error()
		return
	}
	ui.UnitActErr = ""
	ui.UnitEvtDirty = true
	// The conditions and the name field refill from what was just written.
	ui.UnitActCondBound = ""
	ui.UnitActNameBound = ""
	if i := actionIndexByName(doc, next); i >= 0 {
		ui.UnitEvtActionSel = i
	}
	ReloadUnitModelFx(ui)
	ui.StatusText = fmt.Sprintf("Renamed action %s to %s — Save .whe to write it into the mod", old, next)
}

// unitActUpdateEditor processes one field of a choice line: a click takes
// the caret and selects that line, and Enter commits the conditions the way
// Apply does, never dropping a line break into the value.
func UnitActUpdateEditor(ui *uiState, gtx layout.Context, ed *widget.Editor, row int) {
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		ui.UnitActSel = row
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.UnitActSel = row
			ApplyUnitActConditions(ui)
			ed.Blur(gtx)
		}
	}
}

// unitActPanelFooter is the bottom of the right column while an action is
// being worked on: the commit for the condition fields above, and the same
// Save .whe / Revert pair the timeline form carries — an action edit is
// written to the file exactly the way a timeline edit is.
func UnitActPanelFooter(ui *uiState, gtx layout.Context) layout.Dimensions {
	hasAction := false
	rows := 0
	selName := ""
	if ui.UnitEvtDoc != nil {
		if a := SelectedUnitEvtAction(ui, ui.UnitEvtDoc.Actions()); a != nil {
			hasAction = true
			rows = len(a.Motions) + len(a.Subactions)
			if ui.UnitActSel >= 0 && ui.UnitActSel < rows {
				if list, j, _, ok := actLocate(*a, ui.UnitActSel); ok {
					selName = list[j].Name
				}
			}
		}
	}
	var children []layout.FlexChild
	if ui.UnitActErr != "" {
		err := ui.UnitActErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), err)
			l.Color = rgb(0xC98A8A)
			l.MaxLines = 4
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	// The chain tools act on the selected choice — whichever line held the
	// caret last — and keep the if / else if / else shape intact: a head
	// moves with its whole chain, a continuation slides inside its own.
	selLabel := ""
	if selName != "" {
		selLabel = "selected:  " + unitViewportRowText(selName)
	} else if hasAction && rows > 0 {
		selLabel = "pick a line first"
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActAddIf, "Add if", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActAddElseIf, "+elif", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActAddElse, "+else", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActMoveUp, "\u25b2", !UnitActMoveEnabled(ui, -1))
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActMoveDown, "\u25bc", !UnitActMoveEnabled(ui, 1))
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitActDelete, "Delete", rows == 0)
				}),
				layout.Flexed(1, rowGap()),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), selLabel)
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
			)
		})
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &ui.UnitActApply, "Apply", !hasAction)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return PrimaryButton(ui, gtx, &ui.UnitEvtSave, "Save .whe")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &ui.UnitEvtRevert, "Revert", !ui.UnitEvtDirty)
			}),
		)
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

// unitEvtActionPanelBody is the right column while an action is being worked
// on: that action's own properties — its name and the choices the game walks
// when it runs, each guarded by its step of the if / else if / else chain —
// and none of the timeline's events. The panel shows one or the other, so a
// timeline being edited gives the whole panel to its events instead.
func UnitEvtActionPanelBody(ui *uiState, gtx layout.Context) []layout.FlexChild {
	acts := ui.UnitEvtDoc.Actions()
	a := SelectedUnitEvtAction(ui, acts)
	if a == nil {
		return []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitEventsMessage(ui, gtx, "This .whe carries no actions.")
			}),
		}
	}
	act := *a
	text := "Action  " + act.Name
	if ui.UnitEvtDirty {
		text += "  • unsaved"
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), text)
			l.Color = uikit.CTextDim
			l.MaxLines = 2
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		// The action's name is its identity across the file, and it is what a
		// paste from another unit lands on — so it can be changed here, which
		// is how a name this .whe already uses is settled without editing the
		// file by hand.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(112)
					l := material.Label(ui.Th, unit.Sp(9), "Name")
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
							w := material.Editor(ui.Th, &ui.UnitActNameEd, act.Name)
							w.TextSize = unit.Sp(10)
							w.HintColor = uikit.CTextDim
							return w.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitActRename, "Rename")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.Th, "Conditions")
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "One line per choice — press a guard to cycle it, type the condition inline. Apply or Enter writes the document; Save .whe writes the file.")
			l.Color = uikit.CTextDim
			l.MaxLines = 3
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitActionConditions(ui, gtx, act)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "Pick a timeline above to edit its events instead.")
			l.Color = uikit.CTextDim
			l.MaxLines = 2
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
		}),
	}
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

// handleEvtPickerInput is the picker's input: typing narrows the list, a row
// picks its value, Use typed and Enter take the filter text as it is, and
// Cancel closes without writing anything.
func HandleEvtPickerInput(ui *uiState, gtx layout.Context) {
	p := &ui.EvtPick
	submitted := false
	for {
		ev, ok := p.Ed.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if text := strings.TrimSpace(p.Ed.Text()); text != p.Filter {
		p.Filter = text
		RefreshEvtPickerHits(ui)
	}
	if p.Cancel.Clicked(gtx) {
		p.Open = false
		return
	}
	if p.Target == evtPickSound && p.Browse.Clicked(gtx) {
		p.Open = false
		OpenSoundFilePicker(ui)
		return
	}
	for i := range p.Clicks {
		if p.Clicks[i].Clicked(gtx) && i < len(p.Hits) {
			ApplyEvtPick(ui, p.Target, p.Hits[i])
			return
		}
	}
	if p.UseTyped.Clicked(gtx) || submitted {
		ApplyEvtPick(ui, p.Target, p.Filter)
	}
}

// openSoundFilePicker shows the file dialog for the sound field. It uses the
// mod file tree, the sounds the game and the mod hold, and takes .fda or .wav.
// The conversion of a .wav happens here, on the editor's side, not in the
// dialog.
func OpenSoundFilePicker(ui *uiState) {
	OpenFilePicker(ui, pickConfig{
		Title:    "Choose a sound",
		Subtitle: "An .fda from the mod file tree; a .wav is converted to .fda in this mod",
		Hint:     `Data\sound\...`,
		Exts:     []string{".fda", ".wav"},
		ModTree:  true,
		OnPick: func(path string) (bool, string) {
			field, err := SoundFieldFor(ui, path)
			if err != nil {
				return false, err.Error()
			}
			ui.UnitEvtSndEd.SetText(field)
			return true, ""
		},
	})
}

// soundRelUnderData is the part of a path after its data\sound folder, without
// the extension, backslash separated. The path may be a disk path or a mod
// tree path, which starts at Data. It is "" when the path is not under the
// sound folder.
func soundRelUnderData(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	lower := strings.ToLower(p)
	const marker = `data\sound\`
	var rel string
	if strings.HasPrefix(lower, marker) {
		rel = p[len(marker):]
	} else if i := strings.LastIndex(lower, `\`+marker); i >= 0 {
		rel = p[i+1+len(marker):]
	} else {
		return ""
	}
	if e := pickExt(rel); e != "" {
		rel = rel[:len(rel)-len(e)]
	}
	return rel
}

// soundFieldFor turns a chosen file into the sound field's value: its path
// under data\sound without the extension. A .wav is first encoded to .fda at
// the same place in the mod folder, which is what the game plays; an .fda
// must already sit under data\sound.
func SoundFieldFor(ui *uiState, path string) (string, error) {
	ext := pickExt(path)
	rel := soundRelUnderData(path)
	if rel == "" {
		if ext == ".fda" {
			return "", fmt.Errorf("pick an .fda from data\\sound")
		}
		rel = strings.TrimSuffix(filepath.Base(path), ext)
	}
	if ext == ".wav" {
		var raw []byte
		var err error
		if filepath.IsAbs(path) || ui.ModTree == nil {
			raw, err = os.ReadFile(path)
		} else {
			raw, err = ui.ModTree.ReadFile(path)
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %v", path, err)
		}
		w, err := wav.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("not a WAV: %v", err)
		}
		fdaData, err := fda.EncodeWAV(w, 2048)
		if err != nil {
			return "", err
		}
		target := `Data\sound\` + rel + ".fda"
		if _, err := writeFDAOverlay(ui.ModTree, target, fdaData); err != nil {
			return "", err
		}
		ui.StatusText = fmt.Sprintf("Converted %s to %s", filepath.Base(path), target)
	}
	return rel, nil
}

// layoutEvtPicker draws the picker over the window, the same card the
// Import dialog uses.
func LayoutEvtPicker(ui *uiState, gtx layout.Context) layout.Dimensions {
	p := &ui.EvtPick
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			st := clip.Rect{Max: sz}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 60)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(420))
				cardH := (gtx.Constraints.Max.Y * 9) / 10
				if minH := gtx.Dp(unit.Dp(300)); cardH < minH {
					cardH = minH
				}
				gtx.Constraints.Max.X = cardW
				gtx.Constraints.Min.X = cardW
				gtx.Constraints.Max.Y = cardH
				gtx.Constraints.Min.Y = cardH
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Constraints.Max
							rrect(gtx, uikit.CPanelBg, sz, gtx.Dp(4))
							return layout.Dimensions{Size: sz}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(14), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(14), evtPickerTitle(p.Target))
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										h := gtx.Dp(unit.Dp(28))
										gtx.Constraints.Min.Y = h
										gtx.Constraints.Max.Y = h
										return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												Ed := material.Editor(ui.Th, &p.Ed, "")
												Ed.TextSize = unit.Sp(11)
												Ed.HintColor = uikit.CTextDim
												Ed.Hint = "filter, or type a value"
												return Ed.Layout(gtx)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										note := "Nothing to choose from here: type the value."
										if len(p.All) > 0 {
											note = strconv.Itoa(len(p.Hits)) + " of " + strconv.Itoa(len(p.All)) + " shown"
										}
										l := material.Label(ui.Th, unit.Sp(9), note)
										l.Color = uikit.CTextDim
										l.MaxLines = 1
										return l.Layout(gtx)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return LayoutListWithScrollbar(ui, gtx, nil, &p.List, unitRowHeightDp, len(p.Hits), func(gtx layout.Context, i int) layout.Dimensions {
											return p.Clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(10), p.Hits[i])
												l.Color = uikit.CText
												l.MaxLines = 1
												return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6)}.Layout(gtx, l.Layout)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										children := []layout.FlexChild{}
										if p.Target == evtPickSound {
											children = append(children,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return SmallButton(ui, gtx, &p.Browse, "Browse file...")
												}),
												layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
											)
										}
										children = append(children,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return SmallButton(ui, gtx, &p.UseTyped, "Use typed")
											}),
											layout.Flexed(1, layout.Spacer{Width: unit.Dp(0)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return SmallButton(ui, gtx, &p.Cancel, "Cancel")
											}),
										)
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
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

// unitEvtLoadSound resolves one sound path against the mod tree, through the
// same rules the game uses, and returns the decoded sample for it. The first
// file of the bundle plays: a folder of variants sounds different each time in
// the game, but a preview the user is checking wants the same answer twice.
func UnitEvtLoadSound(ui *uiState, p string) (*fx.SoundSample, string, error) {
	pl := &ui.EvtPlayer
	if pl.Cache == nil {
		pl.Cache = map[string]*fx.SoundSample{}
	}
	if ui.ModTree == nil {
		return nil, "", fmt.Errorf("no mod is loaded")
	}
	roots := materialSearchRoots(ui.ModTree)
	b, ok := fx.ResolveSound(ui.ModTree, roots, p)
	if !ok || len(b.Files) == 0 {
		return nil, "", whe.SoundMissing(p)
	}
	file := b.Files[0]
	if s, done := pl.Cache[file]; done {
		return s, file, nil
	}
	s, err := fxSoundDecodeForPlayback(ui.ModTree, roots, file)
	if err != nil {
		return nil, file, err
	}
	pl.Cache[file] = s
	return s, file, nil
}

// unitEvtPlaySelectedSound plays the selected event's sound. The path typed
// into the form wins over the saved one, so a retyped path can be auditioned
// before Set writes it into the document.
func UnitEvtPlaySelectedSound(ui *uiState) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || ui.UnitEvtDoc == nil || ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events) {
		ui.StatusText = "Select a sound event first"
		return
	}
	ev := tl.Events[ui.UnitEvtSel]
	def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !ok {
		ui.StatusText = ev.Name + " has no definition in this .whe"
		return
	}
	path, _ := def.First("sound")
	if typed := strings.TrimSpace(ui.UnitEvtSndEd.Text()); typed != "" {
		path = typed
	}
	path = strings.TrimSpace(path)
	if path == "" {
		ui.StatusText = ev.Name + " names no sound"
		return
	}
	p := &ui.EvtPlayer
	if p.Audition != nil {
		p.Audition.Stop()
		p.Audition = nil
	}
	s, file, err := UnitEvtLoadSound(ui, path)
	if err != nil {
		ui.StatusText = "Play failed: " + err.Error()
		return
	}
	v, err := unitEvtStartVoice(s)
	if err != nil {
		ui.StatusText = "Play failed: " + err.Error()
		return
	}
	p.Audition = v
	ui.StatusText = "Playing " + filepath.Base(file)
}

// unitEvtPlayerTick is one frame of the transport. It reads the three buttons,
// advances the playhead, and starts whatever the playhead has crossed. While a
// timeline plays it asks for the next frame, which is what keeps the clock
// running when the user touches nothing.
func UnitEvtPlayerTick(ui *uiState, gtx layout.Context) {
	p := &ui.EvtPlayer
	if p.Playing && (p.ForDoc != ui.UnitEvtDoc || p.ForTimeline != ui.UnitEvtTimeline) {
		p.StopAll()
	}
	if p.TlStop.Clicked(gtx) {
		p.StopAll()
		ui.StatusText = "Playback stopped"
	}
	if p.TlPlay.Clicked(gtx) {
		if p.Playing {
			p.Paused = !p.Paused
		} else {
			UnitEvtTimelinePlay(ui, gtx)
		}
	}
	if p.PlayEv.Clicked(gtx) {
		UnitEvtPlaySelectedSound(ui)
	}
	if p.Audition != nil && !time.Now().Before(p.Audition.EndsAt) {
		p.Audition.Stop()
		p.Audition = nil
	}
	if !p.Playing {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	dt := now.Sub(p.Last).Seconds()
	p.Last = now
	if dt < 0 {
		dt = 0
	}
	// A stall — a modal dialog, a slow decode on the first play — must not
	// skip the sounds it put the playhead past; it counts as the frame it is.
	if dt > 0.25 {
		dt = 0.25
	}
	if !p.Paused && !p.Dragging {
		p.Pos += dt
	}
	for p.Next < len(p.Order) && p.Order[p.Next].At <= p.Pos {
		f := p.Order[p.Next]
		p.Next++
		s, _, err := UnitEvtLoadSound(ui, f.Path)
		if err != nil {
			log.Printf("evt play: %s: %v", f.Name, err)
			continue
		}
		v, err := unitEvtStartVoice(s)
		if err != nil {
			log.Printf("evt play: %s: %v", f.Name, err)
			continue
		}
		p.Voices = append(p.Voices, v)
	}
	p.Expire()
	if p.Pos >= p.Duration && len(p.Voices) == 0 {
		p.Playing = false
		p.Paused = false
	}
	gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
}

// layoutUnitEvtTransport is the row above the events: play/pause, stop, the
// playhead's time, and the scrub strip.
func LayoutUnitEvtTransport(ui *uiState, gtx layout.Context) layout.Dimensions {
	p := &ui.EvtPlayer
	noTl := ui.UnitEvtDoc == nil || SelectedUnitEventTimeline(ui) == nil
	btn := "Play"
	if p.Playing {
		if p.Paused {
			btn = "Resume"
		} else {
			btn = "Pause"
		}
	}
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &p.TlPlay, btn, noTl)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &p.TlStop, "Stop", !p.Playing)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := ""
				if p.Playing || p.Pos > 0 {
					text = fmt.Sprintf("%.2f / %.2f s", p.Pos, p.Duration)
				}
				l := material.Label(ui.Th, unit.Sp(9), text)
				l.Color = uikit.CTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitEvtScrub(ui, gtx, 10)
				})
			}),
		)
	})
}

// layoutUnitEvtScrub is the strip itself: a played portion, a tick per sound
// in timeline order, and the playhead. Press or drag winds to the pointer's
// position — the whole strip is the target, so it needs no handle to aim at.
func LayoutUnitEvtScrub(ui *uiState, gtx layout.Context, hDp int) layout.Dimensions {
	p := &ui.EvtPlayer
	w := gtx.Constraints.Max.X
	if w < 1 {
		w = 1
	}
	h := gtx.Dp(unit.Dp(hDp))
	gtx.Constraints.Min = image.Pt(w, h)
	gtx.Constraints.Max = image.Pt(w, h)
	frac := 0.0
	if p.Duration > 0 {
		frac = math.Max(0, math.Min(1, p.Pos/p.Duration))
	}
	posX := int(float64(w) * frac)
	// Input first: the strip registers itself, reads whatever the pointer did
	// to it, and only then draws at the position those events settled on.
	stack := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.Scrub)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.Scrub, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press, pointer.Drag:
			if !p.Playing {
				break
			}
			f := float64(e.Position.X) / float64(w)
			p.Dragging = true
			p.Seek(math.Max(0, math.Min(1, f)))
		case pointer.Release, pointer.Cancel:
			p.Dragging = false
		}
	}
	stack.Pop()
	unitEvtFillRect(gtx, image.Rect(0, 0, w, h), uikit.CContentBg)
	if posX > 0 {
		unitEvtFillRect(gtx, image.Rect(0, 0, posX, h), uikit.CSelectBg)
	}
	if p.Duration > 0 {
		for _, f := range p.Order {
			x := int(float64(w) * (f.At / p.Duration))
			if x < 0 {
				x = 0
			}
			if x >= w {
				x = w - 1
			}
			unitEvtFillRect(gtx, image.Rect(x, 0, x+1, h), cEventSnd)
		}
	}
	unitEvtFillRect(gtx, image.Rect(posX, 0, minInt(posX+2, w), h), rgba(255, 255, 255, 200))
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// layoutUnitSquadList is the middle column of the Squads section.
func LayoutUnitSquadList(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	if len(squads) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return SquadEmptyMessage(ui, gtx, ui.UnitSquadsDone)
			})
		})
	}
	EnsureUnitRowClicks(ui, len(squads))
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return LayoutListWithScrollbar(ui, gtx, &ui.UnitSquadScroll, &ui.UnitSquadList, gtx.Dp(unit.Dp(squadRowHeightDp)), len(squads), func(gtx layout.Context, index int) layout.Dimensions {
				if index < 0 || index >= len(squads) {
					return layout.Dimensions{}
				}
				return LayoutUnitSquadRow(ui, gtx, index, squads[index])
			})
		})
	})
}

// layoutUnitSquadRow is one squad: its name, and the size the squad is fielded
// at. The size is shown on the row because it is the answer to the question a
// reader opens this column with — a necron lord is 1, a necron warrior is 3 to 8
// — and finding it means opening the squad otherwise.
func LayoutUnitSquadRow(ui *uiState, gtx layout.Context, index int, s unitSquadRef) layout.Dimensions {
	sel := index == ui.UnitSelRow
	bg := uikit.CPanelBg
	switch {
	case sel:
		bg = uikit.CSelectBg
	case ui.UnitRowClicks[index].Hovered():
		bg = uikit.CMenuHover
	}
	return ui.UnitRowClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(squadRowHeightDp))
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
							name := squadNameOf(s)
							if len(name) > 30 {
								name = name[:27] + "…"
							}
							l := material.Label(ui.Th, unit.Sp(11), name)
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), squadSizeLabel(s))
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

// layoutUnitSquadRightColumn is the right column: the selected squad's own
// properties, read out of its .rgd. This is the part of the squad the unit's own
// file cannot say anything about — its stance, its morale, its cap cost, how it
// captures — none of which belongs to an individual trooper.
func LayoutUnitSquadRightColumn(ui *uiState, gtx layout.Context, sec unitSection) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" {
		return UnitColumn(ui, gtx, sec.RightTitle, "No entry is selected.")
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	if len(squads) == 0 {
		return UnitColumn(ui, gtx, sec.RightTitle, "No entry is selected.")
	}
	idx := clampInt(ui.UnitSelRow, 0, len(squads)-1)
	s := squads[idx]
	fields := squadFields(ui.ModTree, s)
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
								return UnitKeyValueRow(ui, gtx, "Name", squadNameOf(s), false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "Race", s.Race, false)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, "File", s.Path, false)
							})
						}),
					}
					for _, f := range fields {
						f := f
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitKeyValueRow(ui, gtx, f.Label, f.Value, f.IsLocID)
							})
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			}),
		)
	})
}

// handleUnitSquadInput owns the Squads column's row clicks. It is separate from
// handleUnitRowInput because the rows come from a different list and a stale
// index from the weapons column must not select a squad.
func HandleUnitSquadInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != "Squads" {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil {
		return
	}
	squads := SquadsOfUnitOf(ui, d.Entry)
	EnsureUnitRowClicks(ui, len(squads))
	for i := range ui.UnitRowClicks {
		if i < len(squads) && ui.UnitRowClicks[i].Clicked(gtx) {
			ui.UnitSelRow = i
			return
		}
	}
}

// squadCounts are the squad's list properties, counted the same way the unit's
// are: the table ships every slot, so only the slots that say something count.
var squadCounts = []struct {
	group  string
	label  string
	path   []string
	filled func(*rgd.RgdEntry) bool
}{
	{"Lists", "Requirements", []string{"squad_requirement_ext", "requirements"}, requirementSlotUsed},
	{"Lists", "Attack priorities", []string{"squad_combat_stance_ext", "attack_priorities"}, refSlotUsed},
	{"Lists", "Modifiers", []string{"squad_modifier_apply_ext", "modifiers"}, modifierSlotUsed},
	{"Lists", "Broken modifiers", []string{"squad_morale_ext", "broken_modifiers"}, modifierSlotUsed},
	{"Lists", "Leaders", []string{"squad_leader_ext"}, refSlotUsed},
	{"Lists", "Research shown", []string{"squad_ui_ext", "completed_research_filter"}, refSlotUsed},
}

// squadFields reads a squad's own properties, in one read of its .rgd. The rows
// are the squad's, not the unit's, which is the whole point of the column: none of
// them could be read out of the unit's file.
func squadFields(tree *modtree.ModTree, s unitSquadRef) []unitField {
	if tree == nil || s.Path == "" {
		return nil
	}
	data, err := tree.ReadFile(s.Path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf.Root == nil {
		return nil
	}
	var out []unitField
	add := func(group, label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		f := unitField{Group: group, Label: label, Value: value}
		f.StartsGroup = group != "" && (len(out) == 0 || out[len(out)-1].Group != group)
		out = append(out, f)
	}
	// The birth cost is stated first because it is the one number here that is
	// not in the unit's own file, and a mod that charges for a squad puts it
	// there. It is skipped when it is zero, which is what every one of the 431
	// squads of the DXP3 tree carries: the game charges for the troopers through
	// their own cost_ext and leaves birth_resource empty, so five rows of zero
	// would be the whole group on every squad of the base game.
	for _, cur := range birthResourceOrder {
		if s.BirthResource[cur] == "0" {
			continue
		}
		add("Birth cost", birthResourceLabels[cur], s.BirthResource[cur])
	}
	for _, sc := range squadScalars {
		add(sc.Group, sc.Label, rgdEntryStr(rgdFindPath(rf.Root, sc.Path...)))
	}
	for _, c := range squadCounts {
		n, ok := unitListCount(rf.Root, c.path, c.filled)
		if !ok || n == 0 {
			continue
		}
		add(c.group, c.label, itoaCount(n, "entry", "entries"))
	}
	return out
}

// startUnitSquadScan walks every squad .rgd in the background and builds the
// index of which squads use which unit. A scan already running for the same tree
// is left alone; one for a different tree is stopped, so a module reload does not
// leave a goroutine reading a tree that is being closed.
func StartUnitSquadScan(ui *uiState, tree *modtree.ModTree) {
	if tree == nil {
		return
	}
	if ui.UnitSquadsRunning && ui.UnitSquadsTree == tree {
		return
	}
	StopUnitSquadScan(ui)

	ui.UnitSquadsTree = tree
	ui.UnitSquads = make(map[string][]unitSquadRef)
	ui.UnitSquadsCh = make(chan unitSquadBatch, 8)
	stop := make(chan struct{})
	ui.UnitSquadsStop = stop
	ui.UnitSquadsRunning = true

	files := collectSquadRgds(tree)
	ch := ui.UnitSquadsCh
	go func() {
		batch := make(map[string][]unitSquadRef)
		flush := func(done bool) bool {
			out := make(map[string][]unitSquadRef, len(batch))
			for k, v := range batch {
				out[k] = v
			}
			select {
			case ch <- unitSquadBatch{ByUnit: out, Done: done}:
				batch = make(map[string][]unitSquadRef)
				return true
			case <-stop:
				return false
			}
		}
		for _, f := range files {
			for _, ref := range squadsOfUnit(tree, f) {
				key := strings.ToLower(ref.UnitRef)
				batch[key] = append(batch[key], ref)
			}
			if len(batch) >= unitSquadBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
}

// squadsOfUnit reads one squad file and returns one entry per trooper_base it
// declares, each naming the unit that trooper is. The unit path is carried in
// unitRef rather than returned separately, because the caller keys its batch by
// it and the entry it stores is the enriched copy: a squad that is in the list
// also has a size and a birth cost, and reading the file a second time to get
// those would be a wasted read per squad.
func squadsOfUnit(tree *modtree.ModTree, f unitSquadRef) []unitSquadRef {
	if tree == nil || f.Path == "" {
		return nil
	}
	data, err := tree.ReadFile(f.Path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	loadout := rgdFindPath(rf.Root, "squad_loadout_ext")
	if loadout == nil {
		return nil
	}
	// The size and the birth cost belong to the squad rather than to any one
	// trooper, so they are read once here and copied onto every entry.
	base := f
	base.UnitMin = strings.TrimSpace(rgdEntryStr(rgdFindChild(loadout, "unit_min")))
	base.UnitMax = strings.TrimSpace(rgdEntryStr(rgdFindChild(loadout, "unit_max")))
	base.BirthResource = readBirthResource(loadout)

	var out []unitSquadRef
	for _, c := range loadout.Children {
		if c == nil || !c.IsTable() || !strings.EqualFold(c.Name, "trooper_base") {
			continue
		}
		ref := strings.TrimSpace(rgdEntryStr(rgdFindChild(c, "type")))
		if ref == "" {
			continue
		}
		p := resolveRgdRef(tree, ref)
		if p == "" {
			continue
		}
		entry := base
		entry.UnitRef = p
		out = append(out, entry)
	}
	return out
}

// unitTabActive reports whether the object interface is the tab on screen.
func UnitTabActive(ui *uiState) bool {
	return isUnitTab(ActiveTab(ui))
}

// closeUnitTab removes the object tab if it is open. Closing it leaves the
// selection alone, so reopening it lands on the same unit.
func CloseUnitTab(ui *uiState) {
	idx := UnitTabIndex(ui)
	if idx < 0 {
		// The tab is already gone, but a dropdown it left open would still be
		// drawn over whatever replaced it, so the state is cleared either way.
		ui.UnitRaceOpen = false
		ui.UnitListOpen = false
		return
	}
	CloseTabAt(ui, idx)
	// An open dropdown belongs to the tab that went away.
	ui.UnitRaceOpen = false
	ui.UnitListOpen = false
}

// layoutUnitTabBody is the object interface as the content area's middle: the
// picker strip above the three columns.
//
// The order of the layers is what makes an open list usable. The columns come
// first and the strip and its lists last, so a list taller than the strip paints
// over the columns instead of being painted over by them — a strip laid out as
// a row above the columns would be drawn first, and the columns would then
// cover everything the list hangs below it.
func LayoutUnitTabBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	stripH := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	pad := gtx.Dp(unit.Dp(unitTabPadDp))
	return layout.Stack{}.Layout(gtx,
		// The columns, with the strip's height kept clear at the top.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(stripH + pad), Left: unit.Dp(pad), Right: unit.Dp(pad), Bottom: unit.Dp(pad)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitBody(ui, gtx) })
		}),
		// While a list is open, a transparent catcher over the columns: a click
		// that misses the strip and the list lands here and puts the list away.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !UnitPickerOpen(ui) {
				return layout.Dimensions{}
			}
			pass := pointer.PassOp{}.Push(gtx.Ops)
			dims := ui.UnitDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			pass.Pop()
			return dims
		}),
		// The strip, and any open list hanging below it, on top of both.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			LayoutUnitPickerLayer(ui, gtx, stripH, pad)
			// The layer reports the whole area, so the columns are laid out at
			// full size and the strip is free to overhang them.
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
	)
}

// layoutUnitPickerLayer draws the picker strip at the top of its layer and then
// draws whichever list is open underneath it, offset to sit under the field the
// list belongs to.
//
// The two trigger fields are measured first, against a throwaway op stream, so
// the list can be lined up with its own field. The fields carry no clickable of
// their own — the clickable is wrapped around them at the call site — so
// measuring them here neither draws anything twice nor counts a click twice.
func LayoutUnitPickerLayer(ui *uiState, gtx layout.Context, stripH, pad int) {
	measure := func(label, value string) int {
		var scratch op.Ops
		mg := gtx
		mg.Ops = &scratch
		mg.Constraints.Min = image.Pt(0, stripH)
		mg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
		return UnitPickerField(ui, mg, label, value, true, false).Size.X
	}
	// How far the unit field sits from the strip's own left edge. The list is
	// drawn at the same offset, so it hangs under the field that opened it.
	unitX := measure("Race", PickerRaceLabel(ui)) + gtx.Dp(unit.Dp(unitColGapDp))

	// The strip itself, at the top of the layer with the usual padding.
	sg := gtx
	sg.Constraints.Min = image.Pt(0, stripH)
	sg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
	padOff := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
	LayoutUnitPickerRow(ui, sg)
	padOff.Pop()

	if !UnitPickerOpen(ui) {
		ui.UnitDropBox = image.Rectangle{}
		return
	}
	// The list hangs below the strip, over the columns. Its height is not taken
	// from the layer, so it can be as tall as it needs.
	lg := gtx
	lg.Constraints.Min = image.Pt(0, 0)
	lg.Constraints.Max = image.Pt(gtx.Constraints.Max.X-pad, gtx.Constraints.Max.Y-stripH)
	listOff := op.Offset(image.Pt(pad, stripH)).Push(gtx.Ops)
	offset := unitX
	if ui.UnitRaceOpen {
		offset = 0
	}
	dims := LayoutUnitDropdown(ui, lg, offset)
	listOff.Pop()
	// Recorded for tests, which otherwise have no way to know where the list
	// landed and end up probing the columns beside it.
	ui.UnitDropBox = image.Rect(pad+offset, stripH, pad+offset+dims.Size.X, stripH+dims.Size.Y)
}

// unitTabOwnsLeftPanel reports whether the object tab is drawn over the area
// the file tree normally occupies. It does, since the tree has nothing to say
// about browsing units and the section menu is the left column instead.
func UnitTabOwnsLeftPanel(ui *uiState) bool { return UnitTabActive(ui) }

// unitTabOwnsMiddlePanel is the same question for the middle panel, and the
// answer matches: the object columns take that space too.
func UnitTabOwnsMiddlePanel(ui *uiState) bool { return UnitTabActive(ui) }

// unitInfoTable is the object tab's right hand panel: what the selected unit
// is and where it came from. It deliberately repeats only the identifying rows
// — the race, the kind and the file — because the columns already carry the
// unit's properties, and a panel that listed them again would say nothing new.
func UnitInfoTable(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.Th, "No unit selected", unit.Sp(10), uikit.CTextDim)
			}),
		)
	}
	rows := []struct{ label, value string }{
		{"Name", d.Entry.Name},
		{"Race", d.Entry.Race},
		{"Kind", d.Entry.Kind.Label},
		{"File", d.Entry.Path},
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
	}
	for i, r := range rows {
		r := r
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, r.label, r.value, i%2 == 1)
		}))
	}
	// The screen name is the one row here that is a localisation rather than a
	// fact about the file, so it is drawn as an editable one: the text, the id
	// on hover, and the presses that change it. A unit with no screen name still
	// gets the row, because "this unit has no name yet" is the thing a reader
	// most wants to be able to fix from here.
	screenNameID := d.ScreenNameID()
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LocalePanelRow(ui, gtx, "Screen name", screenNameID, localeFieldForID(d, screenNameID))
	}))
	if d.Error != "" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "Could not read this unit:\n"+d.Error)
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitLocalisedName is the text the chosen language gives a unit, or "" when
// the unit has no name yet, carries none, or the string table has no entry for
// it. The id is only a cache key here; the text is read on the UI thread, so a
// language change needs no rescan.
func UnitLocalisedName(ui *uiState, u unitEntry) string {
	if len(ui.UnitNames) == 0 || u.Path == "" {
		return ""
	}
	id, ok := ui.UnitNames[strings.ToLower(u.Path)]
	if !ok {
		return ""
	}
	return localisedString(id)
}

// unitLabel is what the unit lists call a unit: its localised screen name where
// the string table has one, and its .rgd file name where it does not. The file
// name is a real fallback rather than a placeholder — it is what the game uses
// for the unit's hotkey.
func UnitLabel(ui *uiState, u unitEntry) string {
	if text := UnitLocalisedName(ui, u); text != "" {
		return text
	}
	return u.Name
}

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
func UnitInfoFieldSelected(ui *uiState) *unitField {
	d := CurrentUnitDetails(ui)
	if d == nil || len(d.Fields) == 0 {
		return nil
	}
	idx := clampInt(ui.UnitInfoSel, 0, len(d.Fields)-1)
	f := d.Fields[idx]
	if !unitInfoRowSelectable(&f) {
		return nil
	}
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
func ApplyUnitFieldText(ui *uiState, f *unitField, text string) error {
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
	ui.UnitInfoDirty = true
	d := CurrentUnitDetails(ui)
	if d == nil {
		return nil
	}
	d.RebuildFromFile()
	if i := unitFieldIndex(d.Fields, f.Entry); i >= 0 {
		ui.UnitInfoSel = i
	}
	return nil
}

// saveUnitDetails writes the parsed unit back into the mod's own overlay, and
// says where it went. A unit that is not dirty is left alone rather than
// written out again: rewriting an unchanged file is what makes a tool's idea of
// "modified" and a modder's drift apart for no reason.
func SaveUnitDetails(ui *uiState) (string, error) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		return "", fmt.Errorf("no unit is open")
	}
	if !ui.UnitInfoDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.UnitValError != "" {
		return "", fmt.Errorf("correct %s first", ui.UnitValError)
	}
	data, err := d.File.MarshalBinary()
	if err != nil {
		return "", err
	}
	if ui.ModTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	disk, err := ui.ModTree.WriteOverlayFile(d.Entry.Path, data)
	if err != nil {
		return "", err
	}
	ui.UnitInfoDirty = false
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(d.Entry.Path), disk, len(data)), nil
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
func MarkUnitFieldDirty(ui *uiState, f *rgd.RgdEntry) {
	if f == nil {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil || rgdPathOf(d.File.Root, f) == "" {
		return
	}
	ui.UnitInfoDirty = true
}

// unitSyncValEditor reads the editor's events and applies what it says to the
// selected field. Enter saves rather than inserting a line break, so a value can
// be typed and committed without reaching for the button.
//
// The editor is bound to a field rather than to a row index, because the row a
// field sits at moves when the list is rebuilt and a bound-to-index editor
// would then write into whatever took its place.
func UnitSyncValEditor(ui *uiState, gtx layout.Context, f *unitField) {
	if f == nil || f.Entry == nil || !rgdIsEditableType(f.Entry.Type) || f.Entry.Type == rgd.RgdTypeBool {
		return
	}
	ed := &ui.UnitValEditor
	fieldKey := unitFieldKey(*f)
	if ui.UnitValBound != fieldKey {
		ed.SetText(f.Entry.DisplayValue())
		ui.UnitValBound = fieldKey
		ui.UnitValError = ""
	}
	ed.SingleLine = true
	ed.Submit = true
	// The editor is a one line field, so a press into it has to be given focus
	// or the reader would type into the window and wonder where it went.
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
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
	if err := ApplyUnitFieldText(ui, f, ed.Text()); err != nil {
		ui.UnitValError = err.Error()
	} else {
		ui.UnitValError = ""
	}
	if submit {
		SaveUnitFromPanel(ui)
	}
}

// saveUnitFromPanel is the Save button's action: it writes the unit and puts
// what happened in the status line, which is where every other write in the app
// reports itself.
func SaveUnitFromPanel(ui *uiState) {
	if !ui.UnitInfoDirty {
		ui.StatusText = "This unit has no unsaved changes"
		return
	}
	line, err := SaveUnitDetails(ui)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = line
	MarkStateDirty(ui)
}

// handleUnitFieldInput owns the presses of the attribute editor: the rows of
// the info list that select a field, the boolean toggle, the editor's typing
// and the three buttons. It runs after the section list, so a press that
// changed section is already seen here.
func HandleUnitFieldInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != "Unit info" {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil {
		return
	}
	EnsureUnitInfoClicks(ui, len(d.Fields))
	for i := range ui.UnitInfoClicks {
		if i < len(ui.UnitInfoClicks) && ui.UnitInfoClicks[i].Clicked(gtx) {
			ui.UnitInfoSel = i
		}
	}
	if ui.UnitOpenRgdClick.Clicked(gtx) {
		OpenRgd(ui, d.Entry.Path)
		return
	}
	if ui.UnitRevertClick.Clicked(gtx) {
		RevertUnitDetails(ui)
		ui.StatusText = "Changes to this unit thrown away"
		return
	}
	if ui.UnitSaveClick.Clicked(gtx) {
		SaveUnitFromPanel(ui)
		return
	}
	f := UnitInfoFieldSelected(ui)
	if f == nil {
		return
	}
	if f.Entry.Type == rgd.RgdTypeBool {
		if ui.UnitBoolClick.Clicked(gtx) {
			if err := ApplyUnitFieldText(ui, f, unitBoolText(f.Entry.BoolVal)); err != nil {
				ui.UnitValError = err.Error()
			} else {
				ui.UnitValError = ""
			}
		}
		return
	}
	UnitSyncValEditor(ui, gtx, f)
}

// layoutUnitFieldPanel is the right column of the info section: which field the
// list has selected, where it sits in the file, what type it is, and the editor
// that changes it.
func LayoutUnitFieldPanel(ui *uiState, gtx layout.Context, sec unitSection) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	body := func(gtx layout.Context) layout.Dimensions {
		if d == nil || d.Error != "" {
			return UnitEmptyState(ui, gtx)
		}
		f := UnitInfoFieldSelected(ui)
		if f == nil {
			return unitFieldNote(gtx, ui.Th, "Select a row in the info list to change it.")
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "Field", f.Label, false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "In file", rgdPathOf(d.File.Root, f.Entry), false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "Type", f.Entry.TypeLabel(), false)
				})
			}),
		}
		children = append(children, UnitFieldValueWidget(ui, gtx, f)...)
		children = append(children, UnitFieldButtons(ui, gtx)...)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
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
func UnitFieldValueWidget(ui *uiState, gtx layout.Context, f *unitField) []layout.FlexChild {
	out := make([]layout.FlexChild, 0, 3)
	if f.Entry.Type == rgd.RgdTypeBool {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			value := "False"
			if f.Entry.BoolVal {
				value = "True"
			}
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ToggleButton(ui, gtx, &ui.UnitBoolClick, value, f.Entry.BoolVal, uikit.CSelectBg)
			})
		}))
	} else {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Value", &ui.UnitValEditor, false, ui.UnitValError)
		}))
	}
	if ui.UnitValError != "" {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, ui.UnitValError)
			})
		}))
	}
	return out
}

// unitFieldButtons is the row of buttons under the value: save, throw away, and
// the way out to the raw view for the fields this panel does not show.
func UnitFieldButtons(ui *uiState, gtx layout.Context) []layout.FlexChild {
	note := "Enter saves. The change is written into this mod, not the game."
	if ui.UnitInfoDirty {
		note = "Unsaved changes. " + note
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return PrimaryButton(ui, gtx, &ui.UnitSaveClick, "Save unit")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitRevertClick, "Revert", !ui.UnitInfoDirty)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitOpenRgdClick, "Open .rgd")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, note)
			})
		}),
	}
}

// layoutUnitInfoRow is one row of the info list: the section's heading where a
// section starts, and the value line itself.
func LayoutUnitInfoRow(ui *uiState, gtx layout.Context, index int, f *unitField) layout.Dimensions {
	body := func(gtx layout.Context) layout.Dimensions {
		if f == nil {
			return layout.Dimensions{}
		}
		value := func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return UnitKeyValueRowFor(ui, gtx, f.Label, f.Value, f.IsLocID, f.LocID, f.Entry)
			})
		}
		if !f.StartsGroup {
			return value(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), f.Group)
					l.Color = uikit.CTextDim
					l.Font.Weight = font.SemiBold
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(value),
		)
	}
	if !unitInfoRowSelectable(f) || index < 0 || index >= len(ui.UnitInfoClicks) {
		return body(gtx)
	}
	return UnitInfoRowFrame(ui, gtx, index, body)
}

// layoutUnitPickerRow places the race and unit strip above the three columns.
// The strip's height is fixed and the dropdowns are drawn over the columns, so
// opening one does not move the columns the user is reading.
func LayoutUnitPickerRow(ui *uiState, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return LayoutUnitPickerStrip(ui, gtx)
}

// unitObjectLabel is what the dialog calls the object it was opened for. It
// names the unit the pickers have selected, because that is what the columns
// below show; the unit's own display name is preferred, and its file name is
// the fallback, since that is the name the game itself uses.
func UnitObjectLabel(ui *uiState) string {
	if d := CurrentUnitDetails(ui); d != nil {
		if d.Name != "" {
			return d.Name
		}
		if d.Entry.Name != "" {
			return d.Entry.Name
		}
	}
	if u := CurrentUnit(ui); u != nil {
		return u.DisplayName()
	}
	return "No unit selected"
}

// layoutUnitPanelSlot places the tool panel left of the search field, with the
// same margins so both sit on one baseline in the menu bar.
func LayoutUnitPanelSlot(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unitPanelGapDp, Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitPanel(ui, gtx) })
}

// layoutUnitPanel draws the frame around the tool buttons. It measures its
// content first: the panel is a Rigid in the menu bar's flex row, so its
// constraints still hold all the free width of the bar, and a background sized
// from them would stretch from the buttons to the right edge.
func LayoutUnitPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitClicks(ui)
	buttons := func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range unitToolButtons {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return UnitToolButton(ui, gtx, &ui.UnitToolClicks[i], unitToolButtons[i].Label, UnitTabActive(ui))
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
		})
	}
	return frameMeasured(gtx, buttons, uikit.CSaveBg, uikit.CSaveBd, unit.Dp(3))
}

// handleUnitToolInput opens the object interface from the menu bar. It runs on
// every frame, so a click here must not be swallowed by another branch. The
// interface is a tab now, so the button selects or opens that tab rather than
// raising a window.
func HandleUnitToolInput(ui *uiState, gtx layout.Context) {
	EnsureUnitClicks(ui)
	for i := range ui.UnitToolClicks {
		if !ui.UnitToolClicks[i].Clicked(gtx) {
			continue
		}
		switch unitToolButtons[i].Action {
		case "unit-interface":
			alreadyOpen := UnitTabActive(ui)
			OpenUnitTab(ui)
			ui.UnitSection = 0
			MarkStateDirty(ui)
			if alreadyOpen {
				// Pressing the button while the tab is on screen would
				// otherwise leave it looking like nothing happened.
				ui.StatusText = "Object interface is already open"
			} else {
				ui.StatusText = "Object interface: " + UnitObjectLabel(ui)
			}
		}
		return
	}
}

// handleUnitTabInput owns the object tab's own clicks: the pickers, the
// selectable rows of the populated columns, and the section list. It runs as
// part of the ordinary input path, so each part consumes only its own clicks.
func HandleUnitTabInput(ui *uiState, gtx layout.Context) {
	EnsureUnitClicks(ui)
	// The pickers come first: a click on an open dropdown row belongs to the
	// dropdown, not to the section list or the columns underneath it.
	HandleUnitPickerKeys(ui, gtx)
	HandleUnitPickerInput(ui, gtx)
	HandleUnitRowInput(ui, gtx)
	HandleUnitSquadInput(ui, gtx)
	for i := range ui.UnitSectionClicks {
		if i < len(unitSections) && ui.UnitSectionClicks[i].Clicked(gtx) {
			ui.UnitSection = i
			// The section the Object window was left on is part of the
			// saved session along with its pickers.
			MarkStateDirty(ui)
		}
	}
	// Last, because it acts on the section that was just chosen: a press that
	// left the info section must not be read as a press inside it.
	HandleUnitFieldInput(ui, gtx)
	// The mesh eye toggles belong to whichever section shows the mesh list —
	// Events and Classic OE both — so they are drained here rather than
	// inside the Events handler, which returns early for other sections.
	HandleUnitMeshToggles(ui, gtx)
	HandleUnitEventsInput(ui, gtx)
	HandleClassicOEWheInput(ui, gtx)
	HandleClassicOEClauseConditionInput(ui, gtx)
}

// layoutUnitBody splits the given space into the three columns. The widths are
// worked out in pixels rather than with flex weights, because the middle column
// is specified as a third of the space and the left one as a fixed menu width.
func LayoutUnitBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	// The columns themselves; the material dialog's dim and card wrap them
	// below, so a double press on a mesh row raises that editor over
	// whichever section is on show — Viewport and Classic OE both.
	columns := func(gtx layout.Context) layout.Dimensions {
		gap := gtx.Dp(unit.Dp(unitColGapDp))
		minW := gtx.Dp(unit.Dp(unitColMinWidthDp))
		// The card's own padding was already consumed by the caller's inset.
		// The caller's own padding was already consumed before this was called.
		leftW, midW, rightW := unitColumnWidths(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, minW)
		if CurrentUnitSection(ui).Label == unitSectionViewport {
			// The 3D view takes the middle and right columns together. A model in a
			// third of the window is a thumbnail, and the cameras the section lists
			// need a column of their own to be picked out of.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitViewportArea(ui, gtx) }),
			)
		}
		if CurrentUnitSection(ui).Label == unitSectionClassicOE {
			// The classic editor's layout takes the middle and right columns
			// together for the same reason the Viewport section does: its panel
			// column plus a model preview needs the room, and a properties panel
			// beside a preview would squeeze both.
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
				layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOEArea(ui, gtx) }),
			)
		}
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(FixedPanel(ui, leftW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitLeftColumn(ui, gtx) })),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(FixedPanel(ui, midW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitMiddleColumn(ui, gtx) })),
			layout.Rigid(layout.Spacer{Width: unit.Dp(gap)}.Layout),
			layout.Rigid(FixedPanel(ui, rightW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitRightColumn(ui, gtx) })),
		)
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(columns),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.UnitMeshMatOpen {
				return layout.Dimensions{}
			}
			return LayoutUnitMeshMatDialog(ui, gtx)
		}),
	)
}

func LayoutUnitMiddleColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitMiddleColumnBody(ui, gtx)
}

func LayoutUnitRightColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return LayoutUnitRightColumnBody(ui, gtx)
}

// actionIcon returns the decoded command icon of an action or motion, or nil
// when the game's vocabulary has none for it.
func ActionIcon(ui *uiState, name string) image.Image {
	file := actionIconFile(name)
	if file == "" || ui.ModTree == nil {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.ActionIcons == nil || ui.ActionIconsMod != mod {
		ui.ActionIcons = make(map[string]iconCacheEntry)
		ui.ActionIconsMod = mod
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if e, ok := ui.ActionIcons[key]; ok {
		return e.Img
	}
	img := ReadIconAt(ui, actionIconCommandsDir+`\`+file)
	ui.ActionIcons[key] = iconCacheEntry{Img: img, Tried: true}
	return img
}

// unitIcon returns the decoded icon of a unit or building, or nil when the mod
// has none.
func UnitIcon(ui *uiState, u unitEntry) image.Image {
	if ui.ModTree == nil || u.Path == "" {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.UnitIcons == nil || ui.UnitIconsMod != mod {
		ui.UnitIcons = make(map[string]iconCacheEntry)
		ui.UnitIconsMod = mod
	}
	if e, ok := ui.UnitIcons[u.Path]; ok {
		return e.Img
	}
	img := LoadUnitIcon(ui, u)
	ui.UnitIcons[u.Path] = iconCacheEntry{Img: img, Tried: true}
	return img
}

// loadUnitIcon resolves and decodes the icon of one unit, cheapest lookup
// first: the game's naming convention, then the file anywhere in the tree,
// then the name the unit's own .rgd records — reading the unit is the
// expensive part, so it is last.
func LoadUnitIcon(ui *uiState, u unitEntry) image.Image {
	if ui.ModTree == nil {
		return nil
	}
	stem := unitTemplateIconBase(u.Race) + `\` + u.Name + "_icon"
	if img := ReadIconAt(ui, `art\ui\ingame\`+stem); img != nil {
		return img
	}
	if img := ReadIconByName(ui, u.Name+"_icon"); img != nil {
		return img
	}
	data, err := ui.ModTree.ReadFile(u.Path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	icon := rgdEntryStr(rgdFindPath(rf.Root, "ui_ext", "ui_info", "icon_name"))
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return nil
	}
	icon = strings.ReplaceAll(icon, "/", `\`)
	if img := ReadIconAt(ui, `art\ui\ingame\`+icon); img != nil {
		return img
	}
	// icon_name carries the race folder in front ("necrons/necron_icons/...")
	// while the file sits directly under the icons folder.
	if i := strings.Index(icon, `\`); i >= 0 {
		if img := ReadIconAt(ui, `art\ui\ingame\`+icon[i+1:]); img != nil {
			return img
		}
	}
	if i := strings.LastIndex(icon, `\`); i >= 0 {
		icon = icon[i+1:]
	}
	return ReadIconByName(ui, icon)
}

// readIconAt looks for the given tree path without an extension, trying the
// two forms an icon ships in.
func ReadIconAt(ui *uiState, virtualPathNoExt string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		data, err := ui.ModTree.ReadFile(virtualPathNoExt + ext)
		if err != nil {
			continue
		}
		if img := decodeIconData(ext, data); img != nil {
			return img
		}
	}
	return nil
}

// readIconByName finds an icon file by its base name anywhere in the tree,
// which covers a mod that filed its icons somewhere of its own.
func ReadIconByName(ui *uiState, stem string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		f := ui.ModTree.FindByName(stem + ext)
		if f == nil {
			continue
		}
		data, err := ui.ModTree.ReadFile(f.FullPath)
		if err != nil {
			continue
		}
		if img := decodeIconData(ext, data); img != nil {
			return img
		}
	}
	return nil
}

// decodeIconData decodes one icon file. Both forms are ordinary images; a file
// that will not decode simply yields no icon rather than an error line in a
// list that is about names.
func decodeIconData(ext string, data []byte) image.Image {
	switch ext {
	case ".tga":
		if img, err := decodeTGA(data); err == nil {
			return img
		}
	case ".rsh":
		if img, _, err := decodeRshImage(data, 0); err == nil {
			return img
		}
	}
	return nil
}

// unitCatalogFor returns the catalog for the open mod, building it the first
// time it is asked for and again when the mod changes. Building walks every
// race folder, so it is kept off the per-frame path and keyed by the mod's name.
//
// It resets the *selection* when the mod changes, because the old indexes and
// the cached unit then mean nothing. It deliberately does not touch whether a
// dropdown is open: the first call happens lazily, on whatever frame first asks
// for the catalog — which is the frame the user clicked the picker — so
// clearing the open state here would shut the list the instant it opened.
func UnitCatalogFor(ui *uiState) *unitCatalog {
	if ui.ModTree == nil {
		ui.UnitCatalog = nil
		ui.UnitCatalogMod = ""
		ui.ModGraph = nil
		ui.ModGraphMod = ""
		// With no mod there is nothing for a running scan to be reading, and
		// a tree that is being closed is the last place to find that out.
		StopUnitNameScan(ui)
		ui.UnitNames = nil
		ui.UnitNamesDone = false
		StopUnitSquadScan(ui)
		ui.UnitSquads = nil
		ui.UnitSquadsDone = false
		ForgetFreezeBook(ui)
		return nil
	}
	mod := ui.ModTree.FileMapName + "|" + ui.ModTree.ModFilePath
	if ui.UnitCatalog != nil && ui.UnitCatalogMod == mod {
		return ui.UnitCatalog
	}
	// A different mod means a different set of files, so the graph of the old
	// one is meaningless as well.
	ui.ModGraph = nil
	ui.ModGraphMod = ""
	ForgetFreezeBook(ui)
	ui.UnitCatalog = buildUnitCatalog(ui.ModTree)
	ui.UnitCatalogMod = mod
	// The catalog is only the file names; the names the lists show come from
	// each unit's own localisation id, which is a .rgd read per unit. That
	// starts here, in the background, so the dropdown is never the thing that
	// waits for it.
	ui.UnitNamesDone = false
	StartUnitNameScan(ui, ui.ModTree, ui.UnitCatalog)
	// The squad index has the same shape of problem and the same answer: the
	// link from a unit to its squads exists only in the squad files, so those
	// are walked in the background too. Measured at 880ms for the 431 squads of
	// the DXP3 tree, which is far too long to spend on the frame the dialog
	// opens.
	ui.UnitSquadsDone = false
	StartUnitSquadScan(ui, ui.ModTree)
	// A different mod means a different set of races and units, so the old
	// indexes and the cached unit are meaningless.
	ui.UnitRaceSel = 0
	ui.UnitSel = 0
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	ui.UnitSelRow = 0
	ApplyPendingUnitPick(ui)
	return ui.UnitCatalog
}

// graphFor is the mod read as a graph: every .rgd of Data\attrib as an entity,
// and every reference a unit or building holds as a link between two of them.
//
// It is built on first use rather than when the module opens, because indexing
// is cheap but reading the six hundred unit and building files to find their
// references is not, and a session that never opens the relationships view
// should never pay for it. The links themselves are read even later, by
// ensureLinks, for the same reason.
func GraphFor(ui *uiState) *modGraph {
	if ui.ModTree == nil {
		ui.ModGraph = nil
		ui.ModGraphMod = ""
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.ModGraph == nil || ui.ModGraphMod != mod {
		ui.ModGraph = newModGraph(ui.ModTree)
		ui.ModGraphMod = mod
	}
	return ui.ModGraph
}

// currentUnit is the unit the dialog is showing, or nil when the mod has none.
func CurrentUnit(ui *uiState) *unitEntry {
	return UnitCatalogFor(ui).SelectedUnit(ui.UnitRaceSel, ui.UnitSel)
}

// currentRace is the race the dialog is showing, or nil when there is none.
func CurrentRace(ui *uiState) *raceEntry {
	return UnitCatalogFor(ui).SelectedRace(ui.UnitRaceSel)
}

// currentUnitDetails parses the selected unit once and caches it, so switching
// between the sections does not re-read and re-parse the file each time.
func CurrentUnitDetails(ui *uiState) *unitDetails {
	u := CurrentUnit(ui)
	if u == nil {
		return nil
	}
	if ui.UnitDetails != nil && strings.EqualFold(ui.UnitDetailsPath, u.Path) {
		return ui.UnitDetails
	}
	d := loadUnitDetails(ui.ModTree, *u)
	ui.UnitDetails = d
	ui.UnitDetailsPath = u.Path
	// A different unit means different fields, so the pointer targets of the
	// previous unit's localised rows are dead weight. Dropping them here keeps
	// the map the size of one unit's rows rather than one per unit ever opened.
	ui.LocaleEditTags = nil
	// A different unit means the editor's field is not in this file any more,
	// and any change that had not been written is the previous unit's. The
	// unsaved state is deliberately not carried over: unitInfoDirty says the
	// unit on screen has been changed, and it is asking about a different one.
	ui.UnitInfoDirty = false
	ui.UnitValError = ""
	ui.UnitValBound = ""
	ui.UnitValEditor.SetText("")
	ui.UnitInfoSel = 0
	// A different unit means the selected row of the populated columns points
	// at nothing, so the selection starts over.
	ui.UnitSelRow = 0
	// The columns are scrolled to the top for a new unit: the previous unit's
	// offset is meaningless against a different list, and keeping it would drop
	// the reader into the middle of the fields with no way to know they had
	// scrolled at all.
	ui.UnitInfoList = layout.List{}
	ui.UnitSquadList = layout.List{}
	// A different unit is a different model, so the viewport's viewer and the
	// textures it loaded go with the previous one. The next load only happens
	// if the Viewport section is looked at again, which is what keeps browsing
	// units from parsing five megabytes of geometry per click.
	ResetUnitModel(ui)
	return d
}

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
						return UnitPickerField(ui, gtx, "Unit", unitLabel, sel != nil, ui.UnitListOpen)
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

// layoutUnitListDropdown is the open unit list of the selected race. It hangs
// below the strip, over the columns, and scrolls with the same list helper the
// rest of the app uses. xOffset is where the Unit field sits.
func LayoutUnitListDropdown(ui *uiState, gtx layout.Context, xOffset int) layout.Dimensions {
	race := CurrentRace(ui)
	if race == nil {
		return layout.Dimensions{}
	}
	units := race.UnitsOf()
	if len(units) == 0 {
		return layout.Dimensions{}
	}
	EnsureUnitDropdownClicks(ui, len(units))
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
	return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return LayoutListWithScrollbar(ui, gtx, &ui.UnitListScroll, &ui.UnitList, rowH, len(units), func(gtx layout.Context, index int) layout.Dimensions {
					if index < 0 || index >= len(units) {
						return layout.Dimensions{}
					}
					u := units[index]
					sel := index == race.ClampUnitSel(ui.UnitSel)
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
											l := material.Label(ui.Th, unit.Sp(10), u.Kind.Label)
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
			}),
		)
	})
}

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

// layoutUnitWeaponList is the "Weapons" column. The rows are the hardpoints that
// actually hold a weapon, and selecting one is what the right column describes.
func LayoutUnitWeaponList(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Error != "" || len(d.Weapons) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	// One selectable row per weapon slot, so the right column can describe a
	// single weapon rather than a whole hardpoint.
	rows := unitWeaponRows(d)
	EnsureUnitRowClicks(ui, len(rows))
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range rows {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitSelectableRow(ui, gtx, i, len(rows), rows[i].Hardpoint, rows[i].Name, "not found", !rows[i].Found)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
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
		d := CurrentUnitDetails(ui)
		rows = unitWeaponRows(d)
		if len(rows) > 0 {
			idx := clampInt(ui.UnitSelRow, 0, len(rows)-1)
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
		d := CurrentUnitDetails(ui)
		rows = unitAbilityRows(d)
	case "Squads":
		return LayoutUnitSquadRightColumn(ui, gtx, sec)
	case unitSectionEvents:
		return LayoutUnitEventPanel(ui, gtx)
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
		race := CurrentRace(ui)
		if race == nil {
			ui.UnitListOpen = false
			return
		}
		units := race.UnitsOf()
		EnsureUnitDropdownClicks(ui, len(units))
		for i := range units {
			if i < len(ui.UnitClicks) && ui.UnitClicks[i].Clicked(gtx) {
				ui.UnitSel = i
				ui.UnitListOpen = false
				ui.UnitSelRow = 0
				ui.StatusText = "Unit: " + UnitLabel(ui, units[i])
				// The pickers are part of the saved session, so that the
				// Object window comes back on the unit it was left on.
				MarkStateDirty(ui)
				return
			}
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

// openUnitImportDialog shows the file dialog over the module's .whm files; a
// path typed in full or a file from the list is read when the dialog is
// confirmed.
func OpenUnitImportDialog(ui *uiState) {
	OpenFilePicker(ui, pickConfig{
		Title:    "Import frame's timeline",
		Subtitle: "Choose the .whm to read keyframes from — a path, or a file from the module",
		Hint:     `Data\art\ebps\...\model.whm`,
		Exts:     []string{".whm"},
		ModTree:  true,
		OnPick: func(path string) (bool, string) {
			before := ui.UnitImpModel
			UnitImportRead(ui, path)
			if ui.UnitImpModel == before {
				return false, ui.UnitImpStatus
			}
			return true, ""
		},
	})
}

// unitImportRead loads the picked .whm from disk or from the module and
// keeps its parsed model for the panel to browse. Nothing of the source is
// written back: it is a clipboard, read once per dialog confirmation.
func UnitImportRead(ui *uiState, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		ui.UnitImpStatus = "No file was chosen"
		return
	}
	var data []byte
	var err error
	if filepath.IsAbs(path) {
		data, err = os.ReadFile(path)
	} else if ui.ModTree != nil {
		data, err = ui.ModTree.ReadFile(path)
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
		ui.UnitImpStatus = "Read failed: " + err.Error()
		LogInfo("bone: import: read %q failed: %v", path, err)
		return
	}
	m, perr := ParseWhmModel(data)
	if perr != nil {
		ui.UnitImpStatus = "Not a .whm: " + perr.Error()
		LogInfo("bone: import: parse %q failed: %v", path, perr)
		return
	}
	ui.UnitImpModel = m
	ui.UnitImpPath = path
	ui.UnitImpOpen = make([]bool, len(m.Animations))
	ui.UnitImpAnimClicks = make([]widget.Clickable, len(m.Animations))
	ui.UnitImpFrameClicks = make(map[unitImpRef]*widget.Clickable)
	ui.UnitImpSel = make(map[unitImpRef]bool)
	ui.UnitImpAnchor = unitImpRef{Anim: -1}
	ui.UnitImpStatus = fmt.Sprintf("Imported %s — %d clip(s)", filepath.Base(path), len(m.Animations))
	LogInfo("bone: import: loaded %q (%d animations, %d bones)", path, len(m.Animations), len(m.Bones))
}

// unitImportRowClicked is a press on a frame row: a plain click picks that
// frame alone, Ctrl adds or takes it away, Shift picks every frame of the
// same clip between the anchor and this one. Shift only spans one clip —
// a range across two clips has no timeline to be a range on.
func UnitImportRowClicked(ui *uiState, ref unitImpRef, frames []int, mods key.Modifiers) {
	if ui.UnitImpSel == nil {
		ui.UnitImpSel = make(map[unitImpRef]bool)
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	switch {
	case shift && ui.UnitImpAnchor.Anim == ref.Anim && ui.UnitImpAnchor.Anim >= 0:
		lo, hi := -1, -1
		for i, f := range frames {
			if f == ui.UnitImpAnchor.Frame {
				lo = i
			}
			if f == ref.Frame {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			ui.UnitImpSel = map[unitImpRef]bool{ref: true}
			ui.UnitImpAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.UnitImpSel = make(map[unitImpRef]bool, hi-lo+1)
		for _, f := range frames[lo : hi+1] {
			ui.UnitImpSel[unitImpRef{Anim: ref.Anim, Frame: f}] = true
		}
		LogInfo("bone: import: Shift+click spans %d frame(s) of clip %d", hi-lo+1, ref.Anim)
	case ctrl:
		if ui.UnitImpSel[ref] {
			delete(ui.UnitImpSel, ref)
		} else {
			ui.UnitImpSel[ref] = true
			ui.UnitImpAnchor = ref
		}
	default:
		ui.UnitImpSel = map[unitImpRef]bool{ref: true}
		ui.UnitImpAnchor = ref
	}
}

// unitImportApply writes the selected source frames into the picked bone's
// track of the clip on show. With replace the key on the frame is
// overwritten and a missing one is created; without it only the missing
// ones are created, so a first pass fills gaps and a second overwrites.
// Frame numbers are mapped by frame number, clamped to the clip's length —
// the two files are assumed to carry clips of the same length, which is
// what "analogous set of bones" means in practice.
func UnitImportApply(ui *uiState, v *modelViewer, replace bool) (added, replaced, skipped int) {
	what := "copy"
	if replace {
		what = "replace"
	}
	if ui.UnitImpModel == nil {
		ui.UnitBoneKeyErr = "no file imported"
		return 0, 0, 0
	}
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return 0, 0, 0
	}
	if v.BoneIndex < 0 || v.BoneIndex >= len(v.Model.Bones) {
		ui.UnitBoneKeyErr = "no bone is picked"
		return 0, 0, 0
	}
	src := UnitImportBoneIndex(ui, v)
	if src < 0 {
		ui.UnitBoneKeyErr = "the imported file has no bone named " + v.Model.Bones[v.BoneIndex].Name
		return 0, 0, 0
	}
	// The refs are walked in a fixed order — clip then frame — so the
	// result does not depend on map iteration.
	refs := make([]unitImpRef, 0, len(ui.UnitImpSel))
	for ref := range ui.UnitImpSel {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Anim != refs[j].Anim {
			return refs[i].Anim < refs[j].Anim
		}
		return refs[i].Frame < refs[j].Frame
	})
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no frame is picked"
		return 0, 0, 0
	}
	t := BoneTrack(v)
	if t == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return 0, 0, 0
	}
	// The keys written below are also what the copy history keeps: a Copy or
	// Replace records the source keys it took, so they can be found again.
	var keys boneKeyClip
	for _, ref := range refs {
		if ref.Anim < 0 || ref.Anim >= len(ui.UnitImpModel.Animations) {
			skipped++
			continue
		}
		sa := &ui.UnitImpModel.Animations[ref.Anim]
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
		Frame := ref.Frame
		if Frame < 0 {
			skipped++
			continue
		}
		if Frame >= v.Anim.NumFrames {
			// The source clip runs longer than the one on show:
			// the frame has nowhere to land.
			skipped++
			continue
		}
		// Position keys the source frame carries.
		for i := range st.PosKey {
			if frameAt(float64(st.PosKey[i].Time), sa.NumFrames) != Frame {
				continue
			}
			keys.Pos = append(keys.Pos, st.PosKey[i])
			a, r := writeImportPos(t, v, Frame, st.PosKey[i].V, replace)
			added += a
			replaced += r
		}
		// Rotation keys, the same way.
		for i := range st.RotKey {
			if frameAt(float64(st.RotKey[i].Time), sa.NumFrames) != Frame {
				continue
			}
			keys.Rot = append(keys.Rot, st.RotKey[i])
			a, r := writeImportRot(t, v, Frame, st.RotKey[i].V, replace)
			added += a
			replaced += r
		}
	}
	v.PoseValid = false
	Invalidate(v)
	if added+replaced > 0 {
		ui.UnitModelWhmDirty = true
		ui.UnitBoneKeyErr = ""
		ui.UnitClips.Push(clipEntry{
			Kind:  clipImportKeys,
			Label: fmt.Sprintf("%d key(s) from %s", len(keys.Pos)+len(keys.Rot), filepath.Base(ui.UnitImpPath)),
			At:    time.Now(),
			Keys:  &keys,
		})
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
	} else if ui.UnitBoneKeyErr == "" {
		ui.UnitBoneKeyErr = "nothing to write"
	}
	LogInfo("bone: import %s: %d key(s) added, %d replaced, %d skipped (bone=%q clip=%q)",
		what, added, replaced, skipped, t.Name, v.Anim.Name)
	return added, replaced, skipped
}

// unitImportSection is the panel under the keyframe list: the imported
// file's clips, each opening onto the frames the picked bone has keys on,
// and the two buttons that write a multi-selection into the clip on show.
// With nothing imported it is not drawn at all, so the panel looks as it
// did before the feature existed.
func UnitImportSection(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.UnitImpModel == nil {
		return layout.Dimensions{}
	}
	// Clicks are drained before the buttons are laid out, the order the
	// pose bar uses: an update read after Layout has already been spent.
	copyClicked := ui.UnitImpCopyClick.Clicked(gtx)
	replaceClicked := ui.UnitImpReplaceClick.Clicked(gtx)
	if copyClicked {
		UnitImportApply(ui, v, false)
	}
	if replaceClicked {
		UnitImportApply(ui, v, true)
	}
	src := UnitImportBoneIndex(ui, v)
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, "Imported · "+filepath.Base(ui.UnitImpPath))
		}),
	}
	// The buttons sit directly under the header rather than after every clip
	// and frame: a model's imported file lists dozens of clips and hundreds of
	// frames, and at the far end of that the buttons were effectively
	// unreachable. With nothing picked they are drawn disabled rather than
	// hidden, so the row does not jump under the mouse.
	n := len(ui.UnitImpSel)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitImpCopyClick, "Copy", n == 0)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitImpReplaceClick, "Replace", n == 0)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := fmt.Sprintf("%d picked", n)
						if n == 0 {
							txt = "Ctrl+click picks more frames"
						}
						l := material.Label(ui.Th, unit.Sp(9), txt)
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
	}))
	if src < 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			msg := "Select a bone to see its frames from the imported file."
			if v == nil || v.BoneIndex < 0 || v.Model == nil || v.BoneIndex >= len(v.Model.Bones) {
				// The message already says it; keep it short.
			} else {
				msg = "No bone named " + v.Model.Bones[v.BoneIndex].Name + " in the imported file."
			}
			l := material.Label(ui.Th, unit.Sp(9), msg)
			l.Color = uikit.CTextDim
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	} else {
		for i := range ui.UnitImpModel.Animations {
			i := i
			if i >= len(ui.UnitImpOpen) {
				break
			}
			c := &ui.UnitImpAnimClicks[i]
			if c.Clicked(gtx) {
				ui.UnitImpOpen[i] = !ui.UnitImpOpen[i]
			}
			a := &ui.UnitImpModel.Animations[i]
			frames := unitImportFrames(a, src)
			note := fmt.Sprintf("%d frame(s)", len(frames))
			if len(frames) == 0 {
				note = "no keys for this bone"
			}
			open := ui.UnitImpOpen[i]
			mark := "▸ "
			if open {
				mark = "▾ "
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitViewportRow(ui, gtx, c, open, mark+a.Name, note)
			}))
			if !open {
				continue
			}
			if len(frames) == 0 {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), "This clip gives "+v.Model.Bones[v.BoneIndex].Name+" no keys.")
					l.Color = uikit.CTextDim
					return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
				}))
				continue
			}
			for _, f := range frames {
				f := f
				ref := unitImpRef{Anim: i, Frame: f}
				rc := UnitImportRowClick(ui, ref)
				if rc.Clicked(gtx) {
					UnitImportRowClicked(ui, ref, frames, UnitListModifiers(ui))
				}
				selected := ui.UnitImpSel[ref]
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitViewportRow(ui, gtx, rc, selected,
						fmt.Sprintf("frame %d", f+1), unitImportFrameKinds(a, src, f))
				}))
			}
		}
	}
	if ui.UnitBoneKeyErr != "" {
		msg := ui.UnitBoneKeyErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), msg)
			l.Color = rgb(0xB00020)
			l.MaxLines = 1
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutUnitViewportArea is the whole section: the canvas on the left, the
// camera and animation panel on the right. It is what layoutUnitBody puts where
// the middle and right columns would be.
func LayoutUnitViewportArea(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitModel(ui, gtx)
	sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, CurrentUnitSection(ui).MiddleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							// The playback row belongs to the canvas rather than to
							// the section: it drives what is on the view, so it is
							// as wide as the view and stops where the list panel
							// begins. The list keeps the full height of the section.
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitCanvasColumn(ui, gtx) }),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(FixedPanel(ui, sideW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitModelSide(ui, gtx) })),
						)
					})
			}),
		)
	})
}

// layoutUnitCanvasColumn is the canvas with its own playback row under it:
// the two belong together, and the row stops at the canvas's edges rather than
// running under the list panel beside it. The row keeps the height
// unitPlaybackStripDp promises, so the canvas above it is the same size in
// every window — the section's tests work that size out arithmetically.
func LayoutUnitCanvasColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitModelCanvas(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitModelTransport(ui, gtx, ui.UnitModel)
		}),
	)
}

// layoutUnitModelCanvas is the 3D view itself, or the reason there is not one
// yet: a unit whose model is still being read, one that has no model file, and
// one whose model would not decode all say so rather than showing an empty
// panel.
func LayoutUnitModelCanvas(ui *uiState, gtx layout.Context) layout.Dimensions {
	if t := ui.UnitModel; t != nil && t.Model != nil {
		return LayoutModelViewer(ui, gtx, t)
	}
	msg := "Loading the model…"
	switch {
	case ui.UnitModelErr == errNoUnitModel.Error():
		msg = "This unit names no model file, and none of the same name is in the mod."
	case ui.UnitModelErr != "":
		msg = "The model could not be read:\n" + ui.UnitModelErr
	case CurrentUnit(ui) == nil:
		msg = UnitEmptyStateText(ui)
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), msg)
			l.Color = uikit.CTextDim
			l.MaxLines = 4
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, l.Layout)
		})
	})
}

// unitEmptyStateText is what the viewport says when the mod has no unit to show
// a model of.
func UnitEmptyStateText(ui *uiState) string {
	if d := CurrentUnitDetails(ui); d != nil && d.Error != "" {
		return "Could not read this unit:\n" + d.Error
	}
	return "No unit selected."
}

// layoutUnitModelSide is the panel beside the canvas: the state of the preview,
// the file the geometry came from, the cameras the model declares and every
// clip it can be played with.
//
// The module-listing side panel already carries the Model header over in the
// right-hand panel of the window, so this side panel does not repeat it: the
// static playback area sits first, then the scrollable bone/animation lists.
func LayoutUnitModelSide(ui *uiState, gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// The playback controls stay at the top of this side panel, ahead of
			// the place where the Model header used to begin, so they stay visible
			// while the bone and keyframe lists are scrolled.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelSideControls(ui, gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return UnitModelSideScrollArea(ui, gtx)
			}),
		)
	})
}

// unitModelSideControls is the part of the side panel that does not scroll:
// the model-list tabs, then the preview toggles. The playback row is not part
// of this column — it sits under the canvas, where it belongs — so the tabs
// take the place the section's header used to, at the top of the column where
// they cannot scroll away either. The effects and ground switches moved into
// that row as well; what is left here is the view and the axes of what the
// preview draws.
func UnitModelSideControls(ui *uiState, gtx layout.Context) layout.Dimensions {
	t := ui.UnitModel
	v := UnitModelViewer(ui)
	if t == nil || v == nil {
		return UnitModelSideEmpty(ui, gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelListTabs(ui, gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelToggles(ui, gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelSelectionToggle(ui, gtx, v) }),
	)
}

func UnitModelSelectionToggle(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v == nil {
		return layout.Dimensions{}
	}
	SyncUnitSelectionIndicator(ui, v)
	if v.SelectionIndicator == nil {
		return layout.Dimensions{}
	}
	if ui.ClassicOESelectedUIVisible == nil {
		ui.ClassicOESelectedUIVisible = &widget.Bool{Value: v.ShowSelection}
	} else if ui.ClassicOESelectedUIVisible.Value != v.ShowSelection {
		ui.ClassicOESelectedUIVisible.Value = v.ShowSelection
	}
	check := material.CheckBox(ui.Th, ui.ClassicOESelectedUIVisible, "Show selection outline")
	check.TextSize = unit.Sp(9)
	if ui.ClassicOESelectedUIVisible.Update(gtx) {
		v.ShowSelection = ui.ClassicOESelectedUIVisible.Value
		Invalidate(v)
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, check.Layout)
}

// unitModelSideScrollArea is the scrollable body of the side panel. It holds
// the selected model list and any controls belonging to that list.
func UnitModelSideScrollArea(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBonePoseBar(ui, gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return PanelScrollView(ui, gtx, &ui.UnitViewportScroll, func(gtx layout.Context) layout.Dimensions {
				return UnitModelSideBody(ui, gtx)
			})
		}),
	)
}

// unitModelSideBody is the scrollable content of the side panel.
func UnitModelSideBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	// The modifier keys first: the rows below read Ctrl and Shift in this
	// same pass, and a Ctrl held down for a click arrives as key events of
	// its own.
	UnitListTrackModifiers(ui, gtx)
	t := ui.UnitModel
	v := UnitModelViewer(ui)
	if t == nil || v == nil {
		l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
		l.Color = uikit.CTextDim
		return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
	}
	// The model's own facts — the file, the state, the clip on show, the
	// triangles and the frame the playhead stands on — report in the Object
	// panel on the right, not here: this column is three hundred pixels wide
	// and carries the controls, while the Object panel has the room and was
	// mostly empty. The column opens on the Playback strip directly.
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitClipOrMarkerSection(ui, gtx, v)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.UnitFXListMode || ui.UnitMeshListMode {
				return layout.Dimensions{}
			}
			return UnitImportSection(ui, gtx, v)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.UnitFXListMode {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return SmallButton(ui, gtx, &ui.UnitOpenModelClick, "Open model in a tab")
			})
		}),
	)
}

// unitModelTransport is the playback row: a single horizontal line under the
// canvas — Viewport and Classic OE both draw it there, and only there — with
// the transport, the frame readout, the clip's frame count and the switches
// for what the canvas shows. Everything on it is drained before it is laid
// out, in the order the app drains them everywhere else: the switches and the
// transport's presses, then the frames field's Enter.
func UnitModelTransport(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	// A unit with no model still gives the row its height: the canvas above is
	// measured against it, and a strip that came and went would move the view
	// every time a model finished loading.
	if t == nil || t.Model == nil {
		return unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	ModelControlClicks(ui, gtx, t)
	// The clip's frame count sits beside the transport: the buttons wind the
	// playhead through the clip, and this field says how many frames it is cut
	// into. Its clicks and its Enter are drained here, before the row is laid
	// out, the way every other button in the app is.
	framesSetClicked := ui.UnitAnimFramesSetClick.Clicked(gtx)
	framesSubmit := false
	for {
		ev, ok := ui.UnitAnimFramesEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			framesSubmit = true
			ui.UnitAnimFramesEd.Blur(gtx)
		}
	}
	SyncAnimFramesField(ui, t.Model)
	dims := unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelTransportButtons(ui, gtx, t)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelFrameLabel(ui, gtx, t.Model)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return UnitAnimFramesRow(ui, gtx, t.Model)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelPlaybackToggles(ui, gtx, t)
			}),
		)
	})
	if framesSetClicked || framesSubmit {
		LogInfo("bone: frames pressed: %q", ui.UnitAnimFramesEd.Text())
		ApplyAnimFrameCount(ui, t.Model)
	}
	return dims
}

// unitModelToggles holds what is left of the preview's controls in this
// column: how the view is framed, and the axes the effects are drawn against.
// The effects switch and the ground plane are playback switches and live in
// the playback row under the canvas — see unitModelPlaybackToggles.
func UnitModelToggles(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	ModelControlClicks(ui, gtx, t)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitModelViewButtons(ui, gtx, t, false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitModelFXRow(ui, gtx, t)
		}),
	)
}

// unitModelPlaybackToggles are the two switches that change what the canvas is
// showing rather than how it is looked at — the effects and the ground plane
// — which is why they sit at the end of the playback row instead of in the
// list panel. Their presses are already drained by modelControlClicks, which
// the row's caller runs before laying anything out.
func UnitModelPlaybackToggles(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &t.ModelFX, modelFXLabel(v))
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelGround, modelGroundLabel(v), v == nil || v.GroundImage == nil)
		}),
	)
}

// unitModelFXRow is the axes switch: where in the world the effects being
// shown stand. The switch that turns the effects on is a playback switch and
// sits in the playback row under the canvas.
func UnitModelFXRow(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FxAxesButton(ui, gtx, v) }),
		layout.Flexed(1, rowGap()),
	)
}

func UnitModelListTabs(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	HandleUnitModelListTabClicks(ui, gtx, t)
	labels := []string{"Animations", "Markers", "Bones", "FX", "Meshes"}
	selected := ui.UnitListMode
	if ui.UnitFXListMode {
		selected = 3
	}
	if ui.UnitMeshListMode {
		selected = 4
	}
	return ModelListTabButtons(ui, gtx, labels, ui.UnitListTabClicks[:], selected)
}

func HandleUnitModelListTabClicks(ui *uiState, gtx layout.Context, t *fileTab) {
	UnitListTrackModifiers(ui, gtx)
	for i := range ui.UnitListTabClicks {
		if ui.UnitListTabClicks[i].Clicked(gtx) {
			ui.UnitFXListMode = i == 3
			ui.UnitMeshListMode = i == 4
			if i < 3 {
				ui.UnitListMode = i
			}
			ui.UnitViewportScroll = panelScrollState{}
			if t != nil && t.Model != nil {
				Invalidate(t.Model)
			}
		}
	}
}

func ModelListTabButtons(ui *uiState, gtx layout.Context, labels []string, clicks []widget.Clickable, selected int) layout.Dimensions {
	if len(clicks) < len(labels) {
		return layout.Dimensions{}
	}
	if selected < 0 || selected >= len(labels) {
		selected = 0
	}
	children := make([]layout.FlexChild, 0, len(labels))
	for i, label := range labels {
		i, label := i, label
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ToggleButton(ui, gtx, &clicks[i], label, selected == i, uikit.CSelectBg)
		}))
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// unitAnimList is every clip the model carries, the one being played marked, so
// the list is also the answer to "what can this thing do".
func UnitAnimList(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return UnitAnimListWithClicks(ui, gtx, v, &ui.UnitAnimClicks)
}

func UnitAnimListWithClicks(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	anims := v.Model.Animations
	if len(*clicks) != len(anims) {
		*clicks = make([]widget.Clickable, len(anims))
	}
	if len(anims) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This model carries no animations.")
		l.Color = uikit.CTextDim
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range anims {
		i := i
		a := anims[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			SelectAnim(v, i)
			v.Playing = true
			v.LastFrame = time.Time{}
		}
		right := fmt.Sprintf("%d frames", a.NumFrames)
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitViewportRow(ui, gtx, c, v.AnimIndex == i, a.Name, right)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitClipOrMarkerSection is the one section of the panel that holds the
// clip list, marker list, bone list, FX list, or mesh list. All five are
// lists of the same model's contents, and all have to be reachable while
// looking at the others: which clip plays an effect is a marker question,
// which marker a beam leaves is a clip question, and a bone sits under both.
//
// The header says which list is on show; the button that swaps it lives in
// the Playback strip above, which never scrolls away.
func UnitClipOrMarkerSection(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.UnitMeshListMode {
		return UnitMeshListSection(ui, gtx, v)
	}
	return UnitModelListSectionOrFX(ui, gtx, v, ui.UnitListMode, ui.UnitFXListMode,
		&ui.UnitAnimClicks, &ui.UnitMarkerClicks, &ui.UnitBoneClicks, &ui.UnitFXClicks)
}

// unitMeshListSection is the Meshes tab of the side panel: the count header
// over the same eye-toggle rows the Events and Classic OE panels draw, so a
// mesh hidden here is hidden in every viewer of this model. The rows carry no
// scroller of their own — the side panel's body, which holds this section, is
// already scrolled.
func UnitMeshListSection(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	count := 0
	if v != nil && v.Model != nil {
		count = len(modelMeshNames(v.Model))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, fmt.Sprintf("Meshes (%d)", count))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshRows(ui, gtx) }),
	)
}

func UnitModelListContent(ui *uiState, gtx layout.Context, t *fileTab, mode int) layout.Dimensions {
	UnitListTrackModifiers(ui, gtx)
	return UnitModelListSectionOrFX(ui, gtx, t.Model, mode, mode == 3, &t.ModelAnimClicks, &t.ModelMarkerClicks, &t.ModelBoneClicks, &t.ModelFXClicks)
}

func UnitModelListSectionOrFX(ui *uiState, gtx layout.Context, v *modelViewer, mode int, fxMode bool, animClicks, markerClicks, boneClicks, fxClicks *[]widget.Clickable) layout.Dimensions {
	if fxMode {
		return UnitFXListWithHeader(ui, gtx, v, fxClicks)
	}
	return UnitModelListSection(ui, gtx, v, mode, animClicks, markerClicks, boneClicks)
}

func UnitFXListWithHeader(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	if v == nil || v.Model == nil {
		return sectionHeader(gtx, ui.Th, "FX")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, fmt.Sprintf("FX (%d)", len(v.FxEffects)))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ModelFxChecklist(ui, gtx, v, clicks) }),
	)
}

func UnitModelListSection(ui *uiState, gtx layout.Context, v *modelViewer, mode int, animClicks, markerClicks, boneClicks *[]widget.Clickable) layout.Dimensions {
	labels := [3]string{"Animations", "Markers", "Bones"}
	if mode < 0 || mode >= len(labels) {
		mode = 0
	}
	list := func(gtx layout.Context) layout.Dimensions { return UnitAnimListWithClicks(ui, gtx, v, animClicks) }
	switch mode {
	case 1:
		list = func(gtx layout.Context) layout.Dimensions { return UnitMarkerListWithClicks(ui, gtx, v, markerClicks) }
	case 2:
		list = func(gtx layout.Context) layout.Dimensions { return UnitBoneListWithClicks(ui, gtx, v, boneClicks) }
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, labels[mode]) }),
		layout.Rigid(list),
	)
}

// unitMarkerList is every attachment point the model carries, its name and the
// bone it hangs off. Picking one draws a red ring on the model at that marker,
// which is the only way to tell a dozen identically named muzzle points apart
// on a mesh that shows none of them.
func UnitMarkerList(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return UnitMarkerListWithClicks(ui, gtx, v, &ui.UnitMarkerClicks)
}

func UnitMarkerListWithClicks(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	markers := v.Model.Markers
	if len(*clicks) != len(markers) {
		*clicks = make([]widget.Clickable, len(markers))
	}
	if len(markers) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This model carries no markers.")
		l.Color = uikit.CTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range markers {
		i := i
		mk := markers[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			if v.MarkerIndex == i {
				v.MarkerIndex = -1
			} else {
				v.MarkerIndex = i
			}
			v.Redraw = true
		}
		note := mk.Parent
		if note == "" {
			note = "no bone"
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitViewportRow(ui, gtx, c, v.MarkerIndex == i, mk.Name, note)
		}))
	}
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
}

// unitBoneList is every bone the model's skeleton carries, its name and the
// bone it hangs off. Picking one outlines that bone on the model in the pick
// colour and shows its parameters under the list: which joint is picked is a
// question a picture of a skeleton answers badly, since every bone looks like
// every other bone once the mesh is skinned over it.
func UnitBoneList(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return UnitBoneListWithClicks(ui, gtx, v, &ui.UnitBoneClicks)
}

func UnitBoneListWithClicks(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	bones := v.Model.Bones
	if len(*clicks) != len(bones) {
		*clicks = make([]widget.Clickable, len(bones))
	}
	if len(bones) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This model carries no skeleton.")
		l.Color = uikit.CTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range bones {
		i := i
		bone := bones[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			if v.BoneIndex == i {
				v.BoneIndex = -1
			} else {
				v.BoneIndex = i
			}
			v.Redraw = true
		}
		note := "root"
		if bone.Parent >= 0 && bone.Parent < len(bones) && bones[bone.Parent].Name != "" {
			note = bones[bone.Parent].Name
		} else if bone.Parent >= 0 && bone.Parent < len(bones) {
			note = fmt.Sprintf("bone %d", bone.Parent)
		}
		name := bone.Name
		if name == "" {
			name = fmt.Sprintf("bone %d", i)
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitViewportRow(ui, gtx, c, v.BoneIndex == i, name, note)
		}))
	}
	if v.BoneIndex >= 0 && v.BoneIndex < len(bones) {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneParams(ui, gtx, v, v.BoneIndex)
		}))
	}
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
}

// unitBoneParams is the picked bone's own numbers: where its joint sits under
// the pose on show, the rest position as the file stores it, and how many
// keys the current clip holds for it.
func UnitBoneParams(ui *uiState, gtx layout.Context, v *modelViewer, i int) layout.Dimensions {
	bone := v.Model.Bones[i]
	pose := Pose(v)
	pos := "—"
	if i < len(pose.Bones) {
		p := pose.Bones[i].MulPosition(fauxgl.Vector{})
		pos = fmt.Sprintf("%.3f, %.3f, %.3f", p.X, p.Y, p.Z)
	}
	rest := fmt.Sprintf("%.3f, %.3f, %.3f", bone.Pos[0], bone.Pos[1], bone.Pos[2])
	rows := [][2]string{
		{"Position", pos},
		{"Rest pose", rest},
		{"Keys here", fmt.Sprintf("%d", boneTrackKeys(v.Anim, i))},
	}
	children := make([]layout.FlexChild, 0, len(rows)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) })
	}))
	for idx, row := range rows {
		row, idx := row, idx
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, row[0], row[1], idx%2 == 0)
		}))
	}

	if v.Anim != nil {
		t := BoneTrack(v)
		if t != nil && v.BoneIndex == i && (len(t.PosKey) > 0 || len(t.RotKey) > 0) {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) })
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitBoneKeyListHeader(ui, gtx, v)
			}))
			refs := BoneKeyRefs(v)
			EnsureUnitBoneKeyClicks(ui, len(refs))
			BindBoneKeySelection(ui, v)
			keyIdx := 0
			for idx, k := range t.PosKey {
				ref := boneKeyRef{Mode: 0, Frame: frameAt(float64(k.Time), v.Anim.NumFrames)}
				c := &ui.UnitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					BoneKeyRowClicked(ui, v, ref, refs, UnitListModifiers(ui))
				}
				selected := BoneKeyRowPicked(ui, v, ref)
				text := fmt.Sprintf("pos %d  Frame %d  %.3f, %.3f, %.3f", idx+1, ref.Frame+1, k.V[0], k.V[1], k.V[2])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyRow(ui, gtx, c, selected, text)
				}))
				keyIdx++
			}
			for idx, k := range t.RotKey {
				ref := boneKeyRef{Mode: 1, Frame: frameAt(float64(k.Time), v.Anim.NumFrames)}
				c := &ui.UnitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					BoneKeyRowClicked(ui, v, ref, refs, UnitListModifiers(ui))
				}
				selected := BoneKeyRowPicked(ui, v, ref)
				text := fmt.Sprintf("rot %d  Frame %d  %.3f, %.3f, %.3f, %.3f", idx+1, ref.Frame+1, k.V[0], k.V[1], k.V[2], k.V[3])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyRow(ui, gtx, c, selected, text)
				}))
				keyIdx++
			}

		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitBoneKeyListHeader is the "Keyframes" line over the picked bone's rows,
// with how many of them are picked on the right. The rows are the selection's
// only other showing, and a highlight spread over a scrolled list does not say
// how far it reaches.
//
// The spacer is rowGap and not flexGap: this line lives inside the panel's
// scroll content, which is measured with an unbounded height, and a spacer
// that reports back the height it was offered measures the whole panel as
// full — no scrollbar, no wheel — the moment the picked bone has keys. The
// picked count still reaches the right edge because rowGap takes all the
// width and none of the height.
func UnitBoneKeyListHeader(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "Keyframes")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		}),
		layout.Flexed(1, rowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if n := len(PickedBoneKeyRefs(ui, v)); n > 1 {
				l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d picked", n))
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			}
			return layout.Dimensions{}
		}),
	)
}

// unitListTrackModifiers follows Ctrl and Shift while the side panel is on
// screen: a pointer press does not carry usable modifiers here, so Ctrl+click
// and Shift+click on the keyframe rows read the state these key events keep up
// to date. Both filters allow any modifier state — a filter's match requires
// the event's modifiers to be exactly the ones it names as Required plus the
// ones it allows as Optional, so a name-only filter would reject the Ctrl
// press itself the moment that event starts carrying ModCtrl, and the held
// flag would stay false for ever. Every modifier event seen is logged on
// change, so a multi-select that never fires shows here whether the key events
// arrived at all and what they carried.
func UnitListTrackModifiers(ui *uiState, gtx layout.Context) {
	const anyMod = key.ModCtrl | key.ModShift | key.ModAlt | key.ModSuper
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameCtrl, Optional: anyMod},
			key.Filter{Name: key.NameShift, Optional: anyMod},
		)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if s := fmt.Sprintf("%q %v mods=%v", e.Name, e.State, e.Modifiers); s != ui.UnitBoneKeyModLog {
			LogInfo("bone: modifier key %s", s)
			ui.UnitBoneKeyModLog = s
		}
		switch e.Name {
		case key.NameCtrl:
			ui.UnitBoneKeyCtrlHeld = e.State != key.Release
		case key.NameShift:
			ui.UnitBoneKeyShiftHeld = e.State != key.Release
		}
	}
}

// bindBoneKeySelection keeps the picked rows with the bone they were picked
// on: another bone or another clip starts an empty selection rather than
// carrying a set of frames over to keys that are not there. It runs where the
// rows are built, which is the only place both the selection and the bone it
// belongs to are on show.
func BindBoneKeySelection(ui *uiState, v *modelViewer) {
	bound := ""
	if v != nil {
		bound = fmt.Sprintf("%p|%d", v.Anim, v.BoneIndex)
	}
	if bound == ui.UnitBoneKeySelBound {
		return
	}
	LogInfo("bone: keyframe selection bound to %q -> %q: cleared", ui.UnitBoneKeySelBound, bound)
	ui.UnitBoneKeySelBound = bound
	ui.UnitBoneKeySel = nil
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
}

// pickedBoneKeyRefs is the rows of the list that are picked and whose keys
// are still on the track, in list order. Rows whose key has gone — a deletion
// elsewhere, a drag that carried it off its frame — leave the selection rather
// than pointing at whatever frame now carries that number.
func PickedBoneKeyRefs(ui *uiState, v *modelViewer) []boneKeyRef {
	if len(ui.UnitBoneKeySel) == 0 {
		return nil
	}
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeySel = nil
		ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
		return nil
	}
	var out []boneKeyRef
	for _, ref := range BoneKeyRefs(v) {
		if ui.UnitBoneKeySel[ref] {
			out = append(out, ref)
		}
	}
	if len(out) == len(ui.UnitBoneKeySel) {
		return out
	}
	fresh := make(map[boneKeyRef]bool, len(out))
	for _, ref := range out {
		fresh[ref] = true
	}
	LogInfo("bone: keyframe selection dropped %d row(s) whose keys are gone", len(ui.UnitBoneKeySel)-len(out))
	ui.UnitBoneKeySel = fresh
	if len(fresh) == 0 {
		ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
	}
	return out
}

// boneKeyActionRefs is what the pose bar's key buttons act on: the rows still
// picked on the list, and — when nothing on the list is picked — the single
// key the strip or the playhead had picked, which is all the buttons had
// before the list could hold a selection.
func BoneKeyActionRefs(ui *uiState, v *modelViewer) []boneKeyRef {
	if v == nil || v.Anim == nil {
		return nil
	}
	if refs := PickedBoneKeyRefs(ui, v); len(refs) > 0 {
		return refs
	}
	if ref, ok := PickedBoneKeyRef(v); ok {
		return []boneKeyRef{ref}
	}
	return nil
}

// boneKeyRowClicked is what a press on a row of the keyframe list does. A plain
// click picks that row alone; Ctrl+click adds or takes it away, which is how a
// set of rows to delete or to add keys around is gathered; Shift+click picks
// every row between the row the anchor stands on and this one. Whatever the
// modifiers did, the key the typed fields and the strip work on ends up on the
// row that was clicked while that row is among the picked ones — and on the
// last row still picked when it is not, or on none when nothing is left.
func BoneKeyRowClicked(ui *uiState, v *modelViewer, ref boneKeyRef, refs []boneKeyRef, mods key.Modifiers) {
	if v == nil || v.Anim == nil {
		return
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	if ui.UnitBoneKeySel == nil {
		ui.UnitBoneKeySel = make(map[boneKeyRef]bool)
	}
	switch {
	case shift && ui.UnitBoneKeySelAnchor.Mode >= 0:
		lo, hi := -1, -1
		for i, r := range refs {
			if r == ui.UnitBoneKeySelAnchor {
				lo = i
			}
			if r == ref {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			// The anchor is not on the list any more: a Shift+click with
			// nothing to extend from picks the row it lands on.
			ui.UnitBoneKeySel = map[boneKeyRef]bool{ref: true}
			ui.UnitBoneKeySelAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.UnitBoneKeySel = make(map[boneKeyRef]bool, hi-lo+1)
		for _, r := range refs[lo : hi+1] {
			ui.UnitBoneKeySel[r] = true
		}
		LogInfo("bone: Shift+click spans %d row(s), frames %d..%d", hi-lo+1, refs[lo].Frame, refs[hi].Frame)
	case ctrl:
		if ui.UnitBoneKeySel[ref] {
			delete(ui.UnitBoneKeySel, ref)
			LogInfo("bone: Ctrl+click takes Frame %d (%s) out of the selection", ref.Frame, boneGroupLog(ref.Mode))
		} else {
			ui.UnitBoneKeySel[ref] = true
			ui.UnitBoneKeySelAnchor = ref
			LogInfo("bone: Ctrl+click adds Frame %d (%s) to the selection", ref.Frame, boneGroupLog(ref.Mode))
		}
	default:
		ui.UnitBoneKeySel = map[boneKeyRef]bool{ref: true}
		ui.UnitBoneKeySelAnchor = ref
	}
	// The picked key follows the click: the clicked row while it is still
	// among the picked ones, otherwise the last row still picked — in list
	// order, so the choice does not depend on map iteration — and none when
	// the selection is empty.
	picked := boneKeyRef{Mode: -1}
	if ui.UnitBoneKeySel[ref] {
		picked = ref
	} else {
		for _, r := range refs {
			if ui.UnitBoneKeySel[r] {
				picked = r
			}
		}
	}
	if at := BoneKeyIndexAtFrame(v, picked.Frame, picked.Mode); picked.Mode >= 0 && at >= 0 {
		v.BoneKeyIndex, v.BoneKeyMode = at, picked.Mode
	} else {
		v.BoneKeyIndex, v.BoneKeyMode = -1, -1
	}
	v.Redraw = true
}

// pickFrameKeyOnTimeline is what the step buttons do after winding the
// playhead: with a bone picked, every key that bone holds on the frame they
// landed on joins the selection and one of them becomes the key the strip
// shows picked, so stepping through the clip walks the selection along the
// keys instead of leaving it behind where it started. A frame carrying no
// key of this bone leaves the selection as it was.
func PickFrameKeyOnTimeline(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil || v.BoneIndex < 0 || BoneTrack(v) == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	posAt := BoneKeyIndexAtFrame(v, frame, 0)
	rotAt := BoneKeyIndexAtFrame(v, frame, 1)
	if posAt < 0 && rotAt < 0 {
		return
	}
	// The key the strip picks follows the kind that was picked before
	// when it has a key on this frame, otherwise the other kind.
	mode := 1
	if rotAt < 0 || (v.BoneKeyMode == 0 && posAt >= 0) {
		mode = 0
	}
	if mode == 0 {
		v.BoneKeyIndex, v.BoneKeyMode = posAt, 0
	} else {
		v.BoneKeyIndex, v.BoneKeyMode = rotAt, 1
	}
	sel := map[boneKeyRef]bool{}
	if posAt >= 0 {
		sel[boneKeyRef{Mode: 0, Frame: frame}] = true
	}
	if rotAt >= 0 {
		sel[boneKeyRef{Mode: 1, Frame: frame}] = true
	}
	ui.UnitBoneKeySel = sel
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: mode, Frame: frame}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	LogInfo("bone: step picked frame %d: posKey=%v rotKey=%v picked=%s",
		frame+1, posAt >= 0, rotAt >= 0, boneGroupLog(mode))
}

// unitBonePoseBar is the pose-editing menu shown above the list while the bone
// list is on show. The keyframe list scrolls with the rest of the panel, so
// the buttons appended to it slid out of reach exactly when a key picked near
// the top of the list needed them; this bar sits outside the scroll area, and
// showing it shortens the list, so the menu stays visible at any scroll
// position.
//
// The picked key's position and rotation are also typed fields: Enter in a
// field applies its triple to the key, and Save WHM writes the edited keys
// back into the .whm. Both groups show the key a typed apply would write —
// the picked key for its own kind, the nearest key of the other kind to the
// frame on show for the other.
func UnitBonePoseBar(ui *uiState, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	if ui.UnitListMode != 2 || v == nil {
		// If the bar is not laid out, its buttons cannot see a press — while
		// the last drawn frame still shows them. The log says when this
		// becomes the case, which turns a dead click into a visible cause.
		if state := fmt.Sprintf("mode=%d viewer=%v", ui.UnitListMode, v != nil); state != ui.UnitBonePoseBarLog {
			LogInfo("bone: pose bar is not laid out (%s)", state)
			ui.UnitBonePoseBarLog = state
		}
		return layout.Dimensions{}
	}
	ui.UnitBonePoseBarLog = ""
	// The buttons' clicks are drained before they are laid out
	// below, never after. Clickable.Layout also updates the gesture, and an
	// update that completes a click inside Layout hands the click to Layout —
	// which discards it, so a Clicked call later in the same frame finds
	// nothing. The log showed Set and Set all pressed (press=true→false with
	// hover held) without ever reaching their handlers, while Save — whose
	// release happened to land between its Layout and its Clicked — fired;
	// draining first inverts that race: a release waiting from the previous
	// frame is read here, before Layout can swallow it. This is the order the
	// rest of the app uses, with handleInput running ahead of Layout.
	setClicked := ui.UnitBoneKeySetClick.Clicked(gtx)
	setAllClicked := ui.UnitBoneKeySetAllClick.Clicked(gtx)
	saveClicked := ui.UnitBoneKeySaveClick.Clicked(gtx)
	importClicked := ui.UnitImpClick.Clicked(gtx)
	addPosClicked := ui.UnitBoneKeyAddPosClick.Clicked(gtx)
	addRotClicked := ui.UnitBoneKeyAddRotClick.Clicked(gtx)
	beforeClicked := ui.UnitBoneKeyBeforeClick.Clicked(gtx)
	afterClicked := ui.UnitBoneKeyAfterClick.Clicked(gtx)
	delClicked := ui.UnitBoneKeyDelClick.Clicked(gtx)
	mirClicked := [3]bool{
		ui.UnitBoneMirClick[0].Clicked(gtx),
		ui.UnitBoneMirClick[1].Clicked(gtx),
		ui.UnitBoneMirClick[2].Clicked(gtx),
	}
	shiftBackClicked := ui.UnitBoneKeyShiftBack.Clicked(gtx)
	shiftFwdClicked := ui.UnitBoneKeyShiftFwd.Clicked(gtx)
	undoClicked := ui.UnitBoneKeyUndoClick.Clicked(gtx)
	// Events first, before the fields are laid out below: an Enter pressed
	// since the last frame applies its triple, and the group it belongs to
	// is refilled with the values now in the key.
	UpdateBoneKeyEditors(ui, gtx, v)
	SyncBoneKeyFields(ui, gtx, v)
	// The frames field keeps its events drained like the typed groups:
	// a press inside it takes the keyboard, Enter lets go of it again.
	for {
		ev, ok := ui.UnitBoneKeyShiftEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.UnitBoneKeyShiftEd.Blur(gtx)
		}
	}
	// What the keyframe buttons act on — the picked bone's track to add a key
	// to, the rows of the list (or, with none picked, the key the strip picked)
	// to take away or to add keys beside — is read here, before the layout below
	// draws them disabled.
	canAddKey := v != nil && v.Anim != nil && v.BoneIndex >= 0 && BoneTrack(v) != nil
	actionRefs := BoneKeyActionRefs(ui, v)
	canDelKey := len(actionRefs) > 0
	canAddAround := canAddKey && len(actionRefs) > 0
	canShift := canDelKey
	canMir := false
	if v.Anim != nil {
		frame, _ := CurrentFrame(v)
		canMir = BoneKeyTarget(v, frame, 1) >= 0
	}
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) })
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyFieldsRow(ui, gtx, "Pos", &ui.UnitBonePosEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyFieldsRow(ui, gtx, "Rot", &ui.UnitBoneRotEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The angle sliders dial the rotation key the Rot fields
			// show about one axis each, across the full -180..180
			// turn the fields work in. They follow the fields, and
			// dragging one writes the key the same way Enter in a
			// field does.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Ang")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
					}
					for i := range ui.UnitBoneAngleSlid {
						i := i
						children = append(children,
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(9), boneFieldAxes[i])
								l.Color = uikit.CTextDim
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(56))
								gtx.Constraints.Max.X = gtx.Dp(unit.Dp(64))
								sl := material.Slider(ui.Th, &ui.UnitBoneAngleSlid[i])
								sl.Color = rgb(0x6B6B6B)
								// The slider's own Layout folds the drag into
								// its Value; the write to the key happens once,
								// after the whole row has laid out.
								return sl.Layout(gtx)
							}),
						)
					}
					dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
					// All three sliders have laid out, so their drags are in
					// Value now. While any is under the hand, store the trio
					// in the rotation key — the bone and the mesh skinned to
					// it turn to the new angle on this very frame.
					ApplyBoneAngleSliders(ui, v)
					return dims
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The mirror buttons flip the angle of the rotation key
			// the Rot fields show about their own axis: that axis's
			// angle comes back negated, the other two are left as
			// they were.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Mirror")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[0], "mirror X", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[1], "mirror Y", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[2], "mirror Z", !canMir)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The shift row moves the picked keys along the strip:
			// the field holds how many frames, and back/forward move
			// them by that many without reselecting.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Shift")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return UnitBoneKeyField(ui, gtx, "f", &ui.UnitBoneKeyShiftEd)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyShiftBack, "back", !canShift)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyShiftFwd, "fwd", !canShift)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "frames each press moves")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The keyframe buttons: give the picked bone a key on the frame
			// the playhead is on, or take the picked key away. The hint on
			// the right is how the keys already on show are moved — the
			// strip's marks are the handles their drag carries along.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAddPosClick, "+ pos", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAddRotClick, "+ rot", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyDelClick, "Delete key", !canDelKey)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "drag a mark on the strip to move its key")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The buttons that add a key either side of the picked rows:
			// one of the row's own kind on the nearest frame that carries
			// none yet, holding what the row holds. They take their own row
			// because the one above is full, and the hint on the right is
			// how a set of rows is gathered in the first place.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyBeforeClick, "+ before", !canAddAround)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAfterClick, "+ after", !canAddAround)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "Ctrl+click picks more rows")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyUndoClick, "Undo", len(ui.UnitBoneUndo) == 0)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitBoneKeySetClick, "Set")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitBoneKeySetAllClick, "Set all")
						}),
						layout.Flexed(1, layout.Spacer{Width: unit.Dp(0)}.Layout),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// Import and Save share one row: they are the two things that
			// write out of this bar, and one of them opens a dialog rather
			// than editing. The note beside them is whichever is worth
			// reading now — a failure first, since it says why nothing was
			// written, and the imported file otherwise.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitImpClick, "Import frame's timeline")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeySaveClick, "Save WHM", !ui.UnitModelWhmDirty)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text, noteColor := ui.UnitImpPath, uikit.CTextDim
							if ui.UnitBoneKeyErr != "" {
								text, noteColor = ui.UnitBoneKeyErr, rgb(0xB00020)
							}
							if text == "" {
								return layout.Dimensions{}
							}
							if text == ui.UnitImpPath {
								text = filepath.Base(text)
							}
							l := material.Label(ui.Th, unit.Sp(9), text)
							l.Color = noteColor
							l.MaxLines = 2
							return l.Layout(gtx)
						}),
					)
				})
		}),
	)
	// The hover and press states of the buttons are logged on every
	// change: a button the mouse crosses without ever hovering has its hit
	// area somewhere else, a press that never becomes a click broke between
	// the frames, and seeing them settles where a dead click is not.
	btnState := [26]bool{
		ui.UnitBoneKeySetClick.Hovered(), ui.UnitBoneKeySetClick.Pressed(),
		ui.UnitBoneKeySetAllClick.Hovered(), ui.UnitBoneKeySetAllClick.Pressed(),
		ui.UnitBoneKeySaveClick.Hovered(), ui.UnitBoneKeySaveClick.Pressed(),
		ui.UnitBoneKeyAddPosClick.Hovered(), ui.UnitBoneKeyAddPosClick.Pressed(),
		ui.UnitBoneKeyAddRotClick.Hovered(), ui.UnitBoneKeyAddRotClick.Pressed(),
		ui.UnitBoneKeyDelClick.Hovered(), ui.UnitBoneKeyDelClick.Pressed(),
		ui.UnitBoneKeyBeforeClick.Hovered(), ui.UnitBoneKeyBeforeClick.Pressed(),
		ui.UnitBoneKeyAfterClick.Hovered(), ui.UnitBoneKeyAfterClick.Pressed(),
		ui.UnitBoneMirClick[0].Hovered(), ui.UnitBoneMirClick[0].Pressed(),
		ui.UnitBoneMirClick[1].Hovered(), ui.UnitBoneMirClick[1].Pressed(),
		ui.UnitBoneMirClick[2].Hovered(), ui.UnitBoneMirClick[2].Pressed(),
		ui.UnitBoneKeyShiftBack.Hovered(), ui.UnitBoneKeyShiftBack.Pressed(),
		ui.UnitBoneKeyShiftFwd.Hovered(), ui.UnitBoneKeyShiftFwd.Pressed(),
	}
	if btnState != ui.UnitBoneKeyBtnLog {
		LogInfo("bone: buttons [Set hover press | SetAll hover press | Save hover press | +pos hover press | +rot hover press | Del hover press | Before hover press | After hover press | MirX | MirY | MirZ | Back | Fwd] = %v", btnState)
		ui.UnitBoneKeyBtnLog = btnState
	}
	if importClicked {
		LogInfo("bone: Import frame's timeline pressed")
		OpenUnitImportDialog(ui)
	}
	if setClicked {
		LogInfo("bone: Set pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.UnitBonePosEd), boneFieldTexts(&ui.UnitBoneRotEd))
		// Set reads what was typed over: only a group with a changed axis is
		// applied, so a bone with keys of just one kind does not error on the
		// other, and an untouched group is a no-op rather than a surprise.
		any := false
		var firstErr string
		for mode := 0; mode < 2; mode++ {
			changed, _ := ModifiedBoneAxes(ui, mode)
			if len(changed) == 0 {
				continue
			}
			any = true
			LogInfo("bone: Set applies the %s group, changed axes %v", boneGroupLog(mode), changed)
			ApplyBoneKeyFields(ui, v, mode)
			if ui.UnitBoneKeyErr != "" && firstErr == "" {
				firstErr = ui.UnitBoneKeyErr
			}
		}
		switch {
		case !any:
			ui.UnitBoneKeyErr = "no values were changed"
		case firstErr != "":
			ui.UnitBoneKeyErr = firstErr
		}
	}
	if setAllClicked {
		LogInfo("bone: Set all pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.UnitBonePosEd), boneFieldTexts(&ui.UnitBoneRotEd))
		SetAllBoneKeyFields(ui, v)
	}
	if saveClicked {
		LogInfo("bone: Save pressed (dirty=%v)", ui.UnitModelWhmDirty)
		SaveUnitModelWhm(ui)
	}
	if addPosClicked {
		AddBoneKeyAtPlayhead(ui, v, 0)
	}
	if addRotClicked {
		AddBoneKeyAtPlayhead(ui, v, 1)
	}
	if beforeClicked {
		AddBoneKeysAround(ui, v, -1)
	}
	if afterClicked {
		AddBoneKeysAround(ui, v, 1)
	}
	if delClicked {
		DeleteBoneKeysAction(ui, v)
	}
	for axis := 0; axis < 3; axis++ {
		if mirClicked[axis] {
			MirrorBoneRotationAction(ui, v, axis)
		}
	}
	if shiftBackClicked {
		ShiftBoneKeysAction(ui, v, -1)
	}
	if shiftFwdClicked {
		ShiftBoneKeysAction(ui, v, 1)
	}
	if undoClicked {
		LogInfo("bone: Undo pressed (%d step(s) on the stack)", len(ui.UnitBoneUndo))
		UndoBoneKeyEdit(ui, v)
	}
	return dims
}

// updateBoneKeyEditors drains the typed fields' events, applying an Enter:
// the position group writes its XYZ triple into the target position key, the
// rotation group turns its XYZ degrees into the quaternion that key holds.
func UpdateBoneKeyEditors(ui *uiState, gtx layout.Context, v *modelViewer) {
	// Which field holds the caret is logged on change: typing needs it and
	// Enter submits through it, so a caret that never lands on a field
	// explains both of them going quiet.
	var focus [6]bool
	for m, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		for i := range eds {
			focus[m*3+i] = eds[i].Focused(gtx)
		}
	}
	if focus != ui.UnitBoneKeyFocusLog {
		LogInfo("bone: field focus — Pos X=%v Y=%v Z=%v, Rot X=%v Y=%v Z=%v",
			focus[0], focus[1], focus[2], focus[3], focus[4], focus[5])
		ui.UnitBoneKeyFocusLog = focus
	}
	// Tab moves the caret to the next of the six fields and Shift+Tab to the
	// previous one, wrapping round. The editor does not take Tab itself, so it
	// is read here, before the editors drain their own events.
	fields := [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd}
	for flat := 0; flat < 6; flat++ {
		tag := &fields[flat/3][flat%3]
		for {
			ev, ok := gtx.Event(key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift})
			if !ok {
				break
			}
			ke, isKey := ev.(key.Event)
			if !isKey || ke.State != key.Press {
				continue
			}
			next := (flat + 1) % 6
			if ke.Modifiers.Contain(key.ModShift) {
				next = (flat + 5) % 6
			}
			gtx.Execute(key.FocusCmd{Tag: &fields[next/3][next%3]})
		}
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		for i := range eds {
			for {
				ev, ok := eds[i].Update(gtx)
				if !ok {
					break
				}
				if _, submit := ev.(widget.SubmitEvent); submit {
					LogInfo("bone: Enter in the %s group: fields=[%s]", boneGroupLog(mode), boneFieldTexts(eds))
					ApplyBoneKeyFields(ui, v, mode)
					// Enter applies, then moves the caret to the next field: the
					// six fields run Pos X..Z then Rot X..Z and wrap round, so a
					// row of values is typed with Enter alone.
					flat := (mode*3 + i + 1) % 6
					fields := [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd}
					gtx.Execute(key.FocusCmd{Tag: &fields[flat/3][flat%3]})
				}
			}
		}
	}
}

// applyBoneKeyFields reads one triple out of the typed fields and writes it
// into the key it belongs to — the picked key when it is of that kind, the
// nearest key of that kind to the frame on show when it is not. A value that
// is not a number, or a bone with no key of that kind, is reported in the
// note beside Save rather than thrown away silently.
func ApplyBoneKeyFields(ui *uiState, v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	val, err := ParseBoneGroup(ui, mode)
	if err != nil {
		ui.UnitBoneKeyErr = err.Error()
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, mode)
	LogInfo("bone: apply %s: parsed=[%.3f %.3f %.3f] frame=%d target=%d picked(idx=%d mode=%d)",
		boneGroupLog(mode), val[0], val[1], val[2], frame, target, v.BoneKeyIndex, v.BoneKeyMode)
	if target < 0 {
		LogInfo("bone: apply %s: no target key in this clip", boneGroupLog(mode))
		ui.UnitBoneKeyErr = fmt.Sprintf("this bone has no %s key in the clip", kind)
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, mode
	ok := SetBoneKeyPosition(v, [3]float32{float32(val[0]), float32(val[1]), float32(val[2])})
	if mode == 1 {
		ok = SetBoneKeyRotation(v, quatFromEulerDeg(val[0], val[1], val[2]))
	}
	if !ok {
		LogWarn("bone: apply %s: the write was refused (bone=%d mode=%d idx=%d)", boneGroupLog(mode), v.BoneIndex, v.BoneKeyMode, v.BoneKeyIndex)
		ui.UnitBoneKeyErr = fmt.Sprintf("the %s key could not be written", kind)
		return
	}
	LogInfo("bone: apply %s: written ok, dirty=%v", boneGroupLog(mode), ui.UnitModelWhmDirty)
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	// The playhead is wound to the key that was just written so the edit is
	// on show, then the group shows what the key now holds.
	WindToPickedBoneKey(v)
	// The group is refilled from the key, so what is shown is what was
	// stored: a rotation comes back as the degrees its quaternion means.
	FillBoneKeyFields(ui, v, mode)
}

// setAllBoneKeyFields writes only the axes the user changed into every key of
// that kind the selected bone has in the clip: a Rot X of 180 typed over one
// frame becomes a Rot X of 180 on every rotation key, while the axes left
// alone keep their own per-key values. Everything is read before anything is
// written, so a field that does not parse leaves the keys untouched.
func SetAllBoneKeyFields(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil || v.BoneIndex < 0 {
		ui.UnitBoneKeyErr = "no bone is picked"
		return
	}
	t := BoneTrack(v)
	if t == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	axes0, ch0 := ModifiedBoneAxes(ui, 0)
	axes1, ch1 := ModifiedBoneAxes(ui, 1)
	LogInfo("bone: Set all: changed pos-axes=%v rot-axes=%v", axes0, axes1)
	if !ch0 && !ch1 {
		ui.UnitBoneKeyErr = "no values were changed"
		return
	}
	var val0, val1 [3]float64
	var err error
	if ch0 {
		if val0, err = ParseBoneGroup(ui, 0); err != nil {
			ui.UnitBoneKeyErr = err.Error()
			return
		}
	}
	if ch1 {
		if val1, err = ParseBoneGroup(ui, 1); err != nil {
			ui.UnitBoneKeyErr = err.Error()
			return
		}
	}
	snap, haveSnap := SnapshotBoneTrack(v)
	written := 0
	if ch0 {
		for i := range t.PosKey {
			p := t.PosKey[i].V
			for _, ax := range axes0 {
				p[ax] = float32(val0[ax])
			}
			t.PosKey[i].V = p
		}
		written += len(t.PosKey)
	}
	if ch1 {
		for i := range t.RotKey {
			e := eulerDegFromQuat(t.RotKey[i].V)
			for _, ax := range axes1 {
				e[ax] = val1[ax]
			}
			t.RotKey[i].V = quatFromEulerDeg(e[0], e[1], e[2])
		}
		written += len(t.RotKey)
	}
	if written == 0 {
		LogInfo("bone: Set all: no keys of the changed kind to write")
		ui.UnitBoneKeyErr = "the bone has no keys of the changed kind in this clip"
		return
	}
	// The pose cache predates the writes, so the viewport evaluates it again.
	v.PoseValid = false
	Invalidate(v)
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	name := "?"
	if v.BoneIndex < len(v.Model.Bones) {
		name = v.Model.Bones[v.BoneIndex].Name
	}
	LogInfo("bone: Set all: %d key(s) written for bone %s, dirty=%v", written, name, ui.UnitModelWhmDirty)
	ui.StatusText = fmt.Sprintf("Set changed values on %d key(s) of bone %s", written, name)
}

// syncBoneKeyFields keeps the typed fields on the keys a typed apply would
// write: the picked key for its own group, the nearest key of the other kind
// to the frame on show for the other.
//
// A change of selection — another clip, another bone, another picked key —
// refills both groups whatever the cursor is doing: the fields must show the
// bone that is now picked, not the one that was typed into before the switch,
// or Enter would write the old bone's values into the new one. A change of
// frame alone only refills the groups the cursor is not in, so typing
// survives the playhead being moved.
func SyncBoneKeyFields(ui *uiState, gtx layout.Context, v *modelViewer) {
	if v == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	sel := "no clip"
	if v.Anim != nil {
		sel = fmt.Sprintf("%s|%d|%d|%d", v.Anim.Name, v.BoneIndex, v.BoneKeyMode, v.BoneKeyIndex)
	}
	selChanged := sel != ui.UnitBoneKeyBound
	if !selChanged && frame == ui.UnitBoneKeyFrame {
		return
	}
	prevSel := ui.UnitBoneKeyBound
	ui.UnitBoneKeyBound = sel
	ui.UnitBoneKeyFrame = frame
	if selChanged {
		LogInfo("bone: selection changed %q -> %q: refilling both groups", prevSel, sel)
		ui.UnitBoneKeyErr = ""
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
		return
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		if eds[0].Focused(gtx) || eds[1].Focused(gtx) || eds[2].Focused(gtx) {
			continue
		}
		FillBoneKeyFields(ui, v, mode)
	}
}

// fillBoneKeyFields puts one group of fields on the values of the key a typed
// apply of that kind would write, or empties the group when there is no such
// key: an apply into nothing is not offered, and the fields say so by being
// blank.
func FillBoneKeyFields(ui *uiState, v *modelViewer, mode int) {
	eds := &ui.UnitBonePosEd
	if mode == 1 {
		eds = &ui.UnitBoneRotEd
	}
	var vals [3]float64
	ok := false
	if v != nil {
		frame, _ := CurrentFrame(v)
		if idx := BoneKeyTarget(v, frame, mode); idx >= 0 {
			if t := BoneTrack(v); t != nil {
				if mode == 0 && idx < len(t.PosKey) {
					p := t.PosKey[idx].V
					vals = [3]float64{float64(p[0]), float64(p[1]), float64(p[2])}
					ok = true
				} else if mode == 1 && idx < len(t.RotKey) {
					vals = boneAngleDegs(eulerDegFromQuat(t.RotKey[idx].V))
					ok = true
				}
			}
		}
	}
	prev := ui.UnitBoneKeyFillText[mode]
	if mode == 1 {
		// While a slider is under the hand the three sliders hold the
		// trio being written and the fields follow them. Reading the
		// key back mid-drag would show its canonical decomposition —
		// a different triple for the same rotation once an axis passes
		// ±90° — and refilling the two idle sliders from that fed the
		// flipped triple into the next frame's write, so all three
		// jumped back and forth every frame. With no hand on a slider,
		// everything follows the key again.
		held := false
		for i := range ui.UnitBoneAngleSlid {
			if ui.UnitBoneAngleSlid[i].Dragging() {
				held = true
				break
			}
		}
		if held && ok {
			vals = [3]float64{
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[0].Value),
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[1].Value),
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[2].Value),
			}
		} else if !held {
			for i := range ui.UnitBoneAngleSlid {
				frac := float32(0.5)
				if ok {
					frac = boneAngleSliderFrac(vals[i])
				}
				ui.UnitBoneAngleSlid[i].Value = frac
			}
		}
	}
	for i := range eds {
		text := ""
		if ok {
			if vals[i] == 0 {
				vals[i] = 0 // keep -0 from printing as "-0.000"
			}
			text = fmt.Sprintf("%.3f", vals[i])
		}
		// What the field showed is what a later change is measured against:
		// Set and Set all write only the axes typed over.
		ui.UnitBoneKeyFillText[mode][i] = text
		eds[i].SetText(text)
	}
	if now := ui.UnitBoneKeyFillText[mode]; now != prev {
		frame := -1
		if v != nil {
			frame, _ = CurrentFrame(v)
		}
		LogInfo("bone: fill %s frame=%d: [%s] (was [%s])", boneGroupLog(mode), frame,
			strings.Join(now[:], " "), strings.Join(prev[:], " "))
	}
}

// applyAnimFrameCount is what Set beside the frames field and Enter in it do:
// the clip is counted out in the number typed in, and the file is marked dirty
// for Save WHM.
//
// Key times are stored normalised over the clip, so the count is a grid rather
// than a hand on a clock: the same clip counted in twenty frames and in forty
// holds the same poses at the same fractions, and the strip, the frame readout
// and the keyframe rows simply count them out in the new number. The rate is
// what is held fixed: fps is frames over seconds, so a new count keeps the
// rate it had only by moving the duration by the same ratio — the clip is
// counted out at the same rate over a longer or shorter length — and the
// header keeps showing the rate the clip had before the change.
func ApplyAnimFrameCount(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil {
		ui.UnitAnimFramesErr = "no clip is on show"
		return
	}
	text := strings.TrimSpace(ui.UnitAnimFramesEd.Text())
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 {
		ui.UnitAnimFramesErr = fmt.Sprintf("%q is not a frame count — a whole number of at least 1", text)
		return
	}
	if n > maxAnimFrames {
		ui.UnitAnimFramesErr = fmt.Sprintf("%d is past the %d frames a clip is given here", n, maxAnimFrames)
		return
	}
	old := v.Anim.NumFrames
	if n == old {
		ui.UnitAnimFramesErr = ""
		ui.StatusText = fmt.Sprintf("Clip %q is already %d frames", v.Anim.Name, n)
		return
	}
	// The rate is taken before the count moves and the duration is put back
	// to match the new count: fps is frames over seconds, so holding the rate
	// means the length follows the count by the same ratio. A clip with no
	// duration of its own rates at the 20 fps fallback and is given the
	// length that goes with it.
	fps := animFramesPerSecond(v.Anim)
	v.Anim.NumFrames = n
	if fps > 0 {
		v.Anim.Duration = float32(float64(n) / fps)
	}
	v.PoseValid = false
	Invalidate(v)
	ui.UnitModelWhmDirty = true
	ui.UnitAnimFramesErr = ""
	ui.StatusText = fmt.Sprintf("Clip %q: %d frames (was %d) at %.1f fps — Save WHM writes it",
		v.Anim.Name, n, old, animFramesPerSecond(v.Anim))
	LogInfo("bone: clip %q frames %d -> %d (%.2f s at %.1f fps, dirty=%v)",
		v.Anim.Name, old, n, animClipSeconds(v.Anim), animFramesPerSecond(v.Anim), ui.UnitModelWhmDirty)
}

// saveUnitModelWhm writes the edited bone keys back into the .whm: the file
// is read from the mod (overlay first, so an earlier save is included), the
// current animation's bone keys are rebuilt into that copy — values, times
// and counts alike, since keys can be added and removed on the strip — and
// the whole file goes back through the overlay. Everything this editor does
// not read survives intact: only the animation's bone section is rewritten,
// with the chunk sizes above it moved to match.
func SaveUnitModelWhm(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil || v.AnimIndex < 0 || v.AnimIndex >= len(v.Model.Animations) {
		ui.UnitBoneKeyErr = "no animation to save"
		return
	}
	// A press can reach the handler even while the button is drawn disabled,
	// and a save of untouched keys must not be a reason to rewrite the file.
	if !ui.UnitModelWhmDirty {
		LogInfo("bone: Save skipped — no key was changed")
		ui.UnitBoneKeyErr = "nothing to save — no key was changed"
		return
	}
	if ui.ModTree == nil || ui.UnitModelPath == "" {
		ui.UnitBoneKeyErr = "no module is loaded"
		return
	}
	data, err := ui.ModTree.ReadFile(ui.UnitModelPath)
	if err != nil {
		ui.UnitBoneKeyErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchAnimClip(data, &v.Model.Model, v.AnimIndex, v.Anim.NumFrames)
	if err != nil {
		ui.UnitBoneKeyErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitModelPath, patched)
	if err != nil {
		ui.UnitBoneKeyErr = "save failed: " + err.Error()
		return
	}
	ui.UnitModelWhmDirty = false
	ui.UnitBoneKeyErr = ""
	LogInfo("bone: Save WHM ok: %s (%d bytes patched)", filepath.Base(ui.UnitModelPath), len(patched))
	ui.StatusText = fmt.Sprintf("Saved bone keys → %s (%d bytes)", disk, len(patched))
	MarkStateDirty(ui)
	log.Printf("saved whm bone keys %q -> %q (%d bytes)", ui.UnitModelPath, disk, len(patched))
}

// addBoneKeyAtPlayhead is what the "+ pos" and "+ rot" buttons do: the picked
// bone gets a key of that kind on the frame the playhead is on, holding the
// pose the viewport is showing — the same value "Set ... from pose" would
// write — and the file is marked dirty for Save WHM. A key of that kind
// already on the frame is picked instead of a second one being created, and
// the note beside Save says which of the two happened.
func AddBoneKeyAtPlayhead(ui *uiState, v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if v.BoneIndex < 0 {
		ui.UnitBoneKeyErr = "no bone is picked"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	snap, haveSnap := SnapshotBoneTrack(v)
	frame, _ := CurrentFrame(v)
	if !AddBoneKeyAtFrame(v, frame, mode) {
		// The key at the frame was picked all the same, so the fields show
		// what it holds and the note says why nothing was created.
		ui.UnitBoneKeyErr = fmt.Sprintf("frame %d already has a %s key", frame+1, kind)
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
		return
	}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	ui.StatusText = fmt.Sprintf("Added a %s key on frame %d", kind, frame+1)
	LogInfo("bone: %s key added on frame %d for bone %q (dirty=%v)",
		kind, frame+1, BoneTrack(v).Name, ui.UnitModelWhmDirty)
}

// deleteBoneKeysAction is what the "Delete key" button does: every row the
// list has picked leaves the picked bone's track — or, with nothing picked on
// the list, the single key the strip or the playhead had picked — and the file
// is marked dirty for Save WHM. The note and the status line say how many keys
// went and whose they were, so removing several is something the reader can
// count back from the log.
func DeleteBoneKeysAction(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := BoneKeyActionRefs(ui, v)
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no key is picked"
		return
	}
	name := BoneTrack(v).Name
	snap, haveSnap := SnapshotBoneTrack(v)
	removed := 0
	for _, ref := range refs {
		if DeleteBoneKeyAt(v, ref.Frame, ref.Mode) {
			removed++
		}
	}
	if removed == 0 {
		ui.UnitBoneKeyErr = "no key could be removed"
		return
	}
	// Nothing on the list is picked any more: the keys the selection named
	// are gone, and leaving the selection behind would aim the next button at
	// frames that now hold other keys or nothing at all.
	ui.UnitBoneKeySel = nil
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	msg := fmt.Sprintf("Removed %d key(s) of bone %s", removed, name)
	ui.StatusText = msg
	LogInfo("bone: %s (dirty=%v)", msg, ui.UnitModelWhmDirty)
}

// addBoneKeysAround is what "+ before" and "+ after" do (dir −1 and +1): every
// picked row gets a key of its own kind on the nearest frame in that direction
// that carries no key of that kind yet, holding what the row holds — the clip's
// motion is extended rather than pulled towards the pose the viewport happens
// to be showing.
//
// The rows are walked outermost first, so two picked rows standing next to
// each other each get a frame of their own instead of the second one finding
// the first's taken. What the list shows picked afterwards is the keys that
// were just added, since editing or taking them away is what comes next.
func AddBoneKeysAround(ui *uiState, v *modelViewer, dir int) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := BoneKeyActionRefs(ui, v)
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no key is picked"
		return
	}
	side := "before"
	if dir > 0 {
		side = "after"
	}
	sort.Slice(refs, func(i, j int) bool {
		if dir < 0 {
			return refs[i].Frame > refs[j].Frame
		}
		return refs[i].Frame < refs[j].Frame
	})
	added := make([]boneKeyRef, 0, len(refs))
	blocked := 0
	for _, ref := range refs {
		f := FreeBoneKeyFrame(v, ref.Frame, ref.Mode, dir)
		if f < 0 || !InsertBoneKeyCopy(v, f, ref.Mode, ref.Frame) {
			blocked++
			continue
		}
		added = append(added, boneKeyRef{Mode: ref.Mode, Frame: f})
	}
	if len(added) == 0 {
		ui.UnitBoneKeyErr = fmt.Sprintf("no free frame %s those keys", side)
		LogInfo("bone: + %s: nothing was added, %d row(s) had no free frame", side, blocked)
		return
	}
	ui.UnitBoneKeySel = make(map[boneKeyRef]bool, len(added))
	for _, ref := range added {
		ui.UnitBoneKeySel[ref] = true
	}
	ui.UnitBoneKeySelAnchor = added[0]
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	if blocked > 0 {
		ui.UnitBoneKeyErr = fmt.Sprintf("%d row(s) had no free frame", blocked)
	}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	msg := fmt.Sprintf("Added %d key(s) %s the picked rows", len(added), side)
	ui.StatusText = msg
	LogInfo("bone: %s (dirty=%v)", msg, ui.UnitModelWhmDirty)
}

// boneAngleDegs applies boneAngleDeg to the three axes of a rotation.
func boneAngleDegs(v [3]float64) [3]float64 {
	for i := range v {
		v[i] = boneAngleDeg(v[i])
	}
	return v
}

// boneAngleSliderFrac is an axis angle as the slider's 0..1 position across
// the field range 1..360 degrees.
func boneAngleSliderFrac(angle float64) float32 {
	f := (boneAngleDeg(angle) - 1) / 359
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return float32(f)
}

// applyBoneAngleSliders writes the three angle sliders into the rotation key
// the Rot fields show, while any of them is under the hand. The sliders' own
// Layout has already folded each drag into its Value, so this only reads the
// Value and stores it — the same key Enter in the Rot fields would write, with
// all three axes at once so the axes no slider touched keep what they hold.
// setBoneKeyRotation drops the pose cache, so the viewport turns the bone (and
// the mesh skinned to it) to the new angle on the very frame it is written.
func ApplyBoneAngleSliders(ui *uiState, v *modelViewer) {
	dragging := false
	for i := range ui.UnitBoneAngleSlid {
		if ui.UnitBoneAngleSlid[i].Dragging() {
			dragging = true
			break
		}
	}
	if !dragging {
		ui.UnitBoneSliderUndoArmed = false
		return
	}
	if v == nil || v.Anim == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, 1)
	if target < 0 {
		ui.UnitBoneKeyErr = "this bone has no rotation key in the clip"
		return
	}
	t := BoneTrack(v)
	if t == nil || target >= len(t.RotKey) {
		ui.UnitBoneKeyErr = "the rotation key could not be written"
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, 1
	ang := [3]float64{
		boneAngleSliderDeg(ui.UnitBoneAngleSlid[0].Value),
		boneAngleSliderDeg(ui.UnitBoneAngleSlid[1].Value),
		boneAngleSliderDeg(ui.UnitBoneAngleSlid[2].Value),
	}
	// Only a real move writes, so holding still mid-drag does not redraw
	// the viewport or add a log line every frame. The key is compared as
	// a rotation rather than as angles: the same turn has two euler
	// triples — (x, y, z) and (x±180, 180−y, z±180) — and an angle
	// comparison saw those as a change and rewrote the key every frame.
	if quatSame(t.RotKey[target].V, quatFromEulerDeg(ang[0], ang[1], ang[2])) {
		return
	}
	// One undo step for the whole drag: the first write of a gesture records
	// the angle as it was before the hand touched the slider, and the rest of
	// the drag reuses it until the button is let go.
	if !ui.UnitBoneSliderUndoArmed {
		if snap, ok := SnapshotBoneTrack(v); ok {
			PushBoneUndo(ui, snap, true)
		}
		ui.UnitBoneSliderUndoArmed = true
	}
	if !SetBoneKeyRotation(v, quatFromEulerDeg(ang[0], ang[1], ang[2])) {
		ui.UnitBoneKeyErr = "the rotation key could not be written"
		return
	}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	// The typed Rot fields follow the key; the sliders are left alone
	// (fillBoneKeyFields skips the one being dragged).
	FillBoneKeyFields(ui, v, 1)
	LogInfo("bone: angle sliders wrote key %d: rot %.3f %.3f %.3f (dirty=%v)",
		target, ang[0], ang[1], ang[2], ui.UnitModelWhmDirty)
}

// mirrorBoneRotationAction is what a "mirror X/Y/Z" button does: the rotation
// key the Rot fields show comes back with its angle about the chosen axis
// negated and the two other axes left as they were — the angle about that
// axis mirrored, which is then what the fields show.
func MirrorBoneRotationAction(ui *uiState, v *modelViewer, axis int) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, 1)
	if target < 0 {
		ui.UnitBoneKeyErr = "this bone has no rotation key in the clip"
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, 1
	t := BoneTrack(v)
	before := eulerDegFromQuat(t.RotKey[target].V)
	after := before
	after[axis] = -after[axis]
	snap, haveSnap := SnapshotBoneTrack(v)
	if !SetBoneKeyRotation(v, quatFromEulerDeg(after[0], after[1], after[2])) {
		ui.UnitBoneKeyErr = "the rotation key could not be written"
		return
	}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	WindToPickedBoneKey(v)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	msg := fmt.Sprintf("Mirrored %s about %s", t.Name, boneFieldAxes[axis])
	ui.StatusText = msg
	LogInfo("bone: %s (key=%d, rot %.3f %.3f %.3f -> %.3f %.3f %.3f, dirty=%v)",
		msg, target, before[0], before[1], before[2], after[0], after[1], after[2], ui.UnitModelWhmDirty)
}

// shiftBoneKeysAction is what the back/forward buttons beside the frames field
// do: every picked row of the keyframe list — or, with none picked, the single
// key the strip or the playhead had picked — moves that many frames along the
// clip. A key whose new frame is already held by a key that is not shifting
// stays where it is rather than landing on top of it, and the selection
// follows the keys to the frames they ended on, so a run of presses carries a
// block along the strip without reselecting.
func ShiftBoneKeysAction(ui *uiState, v *modelViewer, dir int) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := BoneKeyActionRefs(ui, v)
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no key is picked"
		return
	}
	n := BoneKeyShiftFrames(ui)
	pickedOld, hasPicked := PickedBoneKeyRef(v)
	snap, haveSnap := SnapshotBoneTrack(v)
	newRefs, moved, blocked := ShiftBoneKeys(v, refs, dir*n)
	if moved == 0 && blocked == 0 {
		ui.UnitBoneKeyErr = "no key could be shifted"
		LogInfo("bone: shift %+d of %d key(s): nothing moved", dir*n, len(refs))
		return
	}
	// The selection follows the keys: it now names the frames they landed
	// on, so pressing again shifts them further without reselecting.
	ui.UnitBoneKeySel = make(map[boneKeyRef]bool, len(newRefs))
	for _, ref := range newRefs {
		ui.UnitBoneKeySel[ref] = true
	}
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
	if len(newRefs) > 0 {
		ui.UnitBoneKeySelAnchor = newRefs[0]
	}
	// The picked key follows its row to the frame it landed on, so the
	// strip marks and the typed fields point at it again after the move.
	for i, r := range refs {
		if hasPicked && r == pickedOld && i < len(newRefs) {
			if at := BoneKeyIndexAtFrame(v, newRefs[i].Frame, newRefs[i].Mode); at >= 0 {
				v.BoneKeyIndex, v.BoneKeyMode = at, newRefs[i].Mode
			}
			break
		}
	}
	if moved > 0 {
		ui.UnitModelWhmDirty = true
		PushBoneUndo(ui, snap, haveSnap)
	}
	ui.UnitBoneKeyErr = ""
	if blocked > 0 {
		ui.UnitBoneKeyErr = fmt.Sprintf("%d key(s) stayed put (no room)", blocked)
	}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	side := "forward"
	if dir < 0 {
		side = "back"
	}
	msg := fmt.Sprintf("Shifted %d key(s) %d frame(s) %s", moved, n, side)
	ui.StatusText = msg
	LogInfo("bone: %s (%d row(s) shifted, %d held back, dirty=%v)", msg, len(newRefs), blocked, ui.UnitModelWhmDirty)
}

// undoBoneKeyEdit rolls the last key edit back: the most recent snapshot is
// restored into the clip and bone it was taken from, the selection is dropped
// (the frames it named may not exist in the restored track) and the typed
// fields are refilled from the restored keys. It reports whether anything was
// undone, which is what the note beside Save says.
func UndoBoneKeyEdit(ui *uiState, v *modelViewer) bool {
	if len(ui.UnitBoneUndo) == 0 {
		ui.UnitBoneKeyErr = "nothing to undo"
		return false
	}
	s := ui.UnitBoneUndo[len(ui.UnitBoneUndo)-1]
	ui.UnitBoneUndo = ui.UnitBoneUndo[:len(ui.UnitBoneUndo)-1]
	if v == nil || !RestoreBoneTrack(v, s) {
		ui.UnitBoneKeyErr = "could not restore the previous keys"
		return false
	}
	// The restored track may not carry the keys the selection and picked key
	// pointed at, so both are cleared and the fields refill from what is
	// actually there now.
	ui.UnitBoneKeySel = nil
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	ui.StatusText = "Restored the keys before the last edit"
	LogInfo("bone: undo restored track of bone %d: %d pos, %d rot keys (dirty=%v)",
		s.BoneIndex, len(s.PosKey), len(s.RotKey), ui.UnitModelWhmDirty)
	return true
}

// unitViewportRow is one selectable line of the camera panel: the name, and a
// dimmer note on the right that says what it is.
func UnitViewportRow(ui *uiState, gtx layout.Context, c *widget.Clickable, selected bool, name, note string) layout.Dimensions {
	return UnitViewportRowIcon(ui, gtx, c, selected, name, note, nil, false)
}

// unitViewportRowIcon is unitViewportRow with an optional icon column before
// the name. A list that shows an icon for every row — the Classic OE's
// Actions folder — reserves the column whether or not a row's icon was found
// (iconCol), so its names stay in one line; a list whose icons come and go
// passes iconCol false and shows them as they come.
func UnitViewportRowIcon(ui *uiState, gtx layout.Context, c *widget.Clickable, selected bool, name, note string, icon image.Image, iconCol bool) layout.Dimensions {
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
					Rrect(ui, gtx, bg, gtx.Constraints.Max, unit.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if icon == nil && !iconCol {
								return layout.Dimensions{}
							}
							return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return unitIconLayout(gtx, icon, 24, 18)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), unitViewportRowText(name))
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if note == "" {
								return layout.Dimensions{}
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

// ensureUnitModel starts loading the current unit's model if it is not already
// loaded or on its way. It is called from the Viewport section's layout, so
// browsing units never pays for a model that is not being looked at.
//
// While the read is on a worker the section has no model to animate, and the
// canvas below has nothing to ask a redraw for — the window repaints on input
// and on the invalidates a drawn model schedules, neither of which exist here.
// The poll that would pick the result up runs at the top of the frame loop, so
// without an invalidate of its own the finished answer waits in its channel
// until the mouse moves. That is what made a switch on this section look many
// seconds slower than the same switch done from another one and read afterwards:
// the read cost the same, but one of them was waiting for input to show it.
func EnsureUnitModel(ui *uiState, gtx layout.Context) {
	if ui.ModTree == nil {
		return
	}
	src, ok := newUnitModelSource(ui.ModTree, CurrentUnitDetails(ui))
	if !ok {
		ResetUnitModel(ui)
		return
	}
	if src.Unit != ui.UnitModelFor || (ui.UnitModel == nil && ui.UnitModelCh == nil && ui.UnitModelErr == "") {
		StartUnitModel(ui, src)
	}
	if ui.UnitModelCh != nil {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(30 * time.Millisecond)})
	}
}

// startUnitModel kicks the read and the parse off on a worker. Both are worth
// moving off the UI goroutine: a unit model is megabytes, and the necron warrior's
// alone is five of them.
func StartUnitModel(ui *uiState, src unitModelSource) {
	ResetUnitModel(ui)
	ui.UnitModelFor = src.Unit
	tree := ui.ModTree
	ch := make(chan unitModelResult, 1)
	ui.UnitModelCh = ch
	ui.UnitModelStart = time.Now()
	go func() {
		res := unitModelResult{Unit: src.Unit}
		workStart := time.Now()
		res.Path = unitModelPath(tree, src)
		if res.Path == "" {
			res.Err = errNoUnitModel
			ch <- res
			return
		}
		data, err := tree.ReadFile(res.Path)
		if err != nil {
			res.Err = err
			ch <- res
			return
		}
		model, err := ParseWhmModel(data)
		if err != nil {
			res.Err = err
			ch <- res
			return
		}
		res.Model = model
		res.GroundImage = loadWhmGroundImage(tree)
		res.Work = time.Since(workStart)
		ch <- res
	}()
}

func PollUnitModel(ui *uiState) {
	if ui.UnitModelCh == nil {
		return
	}
	select {
	case res := <-ui.UnitModelCh:
		ui.UnitModelCh = nil
		if res.Unit != ui.UnitModelFor {
			// The reader has moved on to another unit while this one loaded.
			return
		}
		if res.Err != nil {
			ui.UnitModelErr = res.Err.Error()
			return
		}
		// The gap between what the worker spent and what the reader waited is
		// the answer arriving without a frame to be picked up in, which is the
		// whole of the difference between a switch that looked slow and one
		// that did not.
		total := time.Since(ui.UnitModelStart)
		LogInfo("3D preview: model of %q on screen after %v (worker %v, waiting for a frame %v)",
			res.Path, total, res.Work, total-res.Work)
		tab := &fileTab{Path: res.Path, Kind: "whm"}
		tab.Model = newObjectModelViewer(res.Model, res.Path, res.GroundImage)
		selectObjectAction(tab.Model, ui.ModTree, res.Path, "idle")
		// The canvas is a third of the window wide here, so the playback strip
		// is drawn in the panel beside it rather than under it.
		tab.Model.SideControls = true
		ui.UnitModel = tab
		ui.UnitModelPath = res.Path
		// A freshly read model starts clean, and its keys rebind the typed
		// fields: the previous model's edits are not its edits.
		ui.UnitModelWhmDirty = false
		ui.UnitBoneKeyBound = ""
		ui.UnitBoneKeyErr = ""
	default:
	}
}

// saveUnitMeshMaterialDialog writes the fields into the .whm: the references
// that differ are spliced in, the whole file goes to the overlay, and the
// model on show is set to the same so the view agrees with what was written.
// The dialog stays up — a modder correcting a path usually tries another —
// and Cancel after a save reverts to nothing, because the file now says this.
func SaveUnitMeshMaterialDialog(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		ui.UnitMeshMatErr = "no model is loaded"
		return
	}
	if ui.ModTree == nil || ui.UnitModelPath == "" {
		ui.UnitMeshMatErr = "no module is loaded"
		return
	}
	refs := UnitMeshMatFieldText(ui)
	data, err := ui.ModTree.ReadFile(ui.UnitModelPath)
	if err != nil {
		ui.UnitMeshMatErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchMeshMaterialPaths(data, ui.UnitMeshMatName, refs)
	if err != nil {
		ui.UnitMeshMatErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitModelPath, patched)
	if err != nil {
		ui.UnitMeshMatErr = "save failed: " + err.Error()
		return
	}
	applyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, refs)
	ReloadUnitModelMaterials(ui, v)
	Invalidate(v)
	ui.UnitMeshMatErr = ""
	// The file holds these now: Cancel after this point reverts to them.
	ui.UnitMeshMatOrig = refs
	for i := range ui.UnitMeshMatEditors {
		if i < len(refs) {
			ui.UnitMeshMatEditors[i].SetText(refs[i])
		}
	}
	ui.StatusText = fmt.Sprintf("Saved materials → %s", disk)
	LogInfo("mesh material: Save WHM ok: %s (mesh=%q, %d path(s))",
		disk, ui.UnitMeshMatName, len(refs))
	MarkStateDirty(ui)
}

// layoutUnitMeshMatDialog is the dialog itself: a dimmed card over the unit
// body with the mesh's references in editable fields and Preview / Save / Cancel
// beneath. The buttons and the dim were laid out last frame, so a press on any
// of them is read here, ahead of redrawing what they decided — the order the
// Classic OE event dialog takes.
//
// The card is sized to its content rather than to the window, and it takes
// every press on its own area, so clicking the card's whitespace neither
// leaves a dialog that fills the screen nor closes it under the user's hand.
func LayoutUnitMeshMatDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	cancel := ui.UnitMeshMatCancel.Clicked(gtx)
	dim := ui.UnitMeshMatDim.Clicked(gtx)
	if cancel || dim {
		CloseUnitMeshMaterialDialog(ui, true)
	}
	if ui.UnitMeshMatPreview.Clicked(gtx) {
		PreviewUnitMeshMaterialDialog(ui)
	}
	if ui.UnitMeshMatSave.Clicked(gtx) {
		SaveUnitMeshMaterialDialog(ui)
	}
	if !ui.UnitMeshMatOpen {
		return layout.Dimensions{}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return ui.UnitMeshMatDim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				st := clip.Rect{Max: sz}.Push(gtx.Ops)
				paint.ColorOp{Color: rgba(0, 0, 0, 70)}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				st.Pop()
				return layout.Dimensions{Size: sz}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				inset := layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}
				cardW := minInt(gtx.Dp(unit.Dp(720)), gtx.Constraints.Max.X-gtx.Dp(unit.Dp(30)))
				contentW := cardW - gtx.Dp(inset.Left+inset.Right)
				// The card is as tall as what it holds, not as tall as the
				// window. One material was drawing a 540dp card that was nine
				// tenths empty; only when the fields run past the room the
				// window has does the card fill it, and then they scroll.
				cardH := clampInt(
					UnitMeshMatNaturalHeight(ui, gtx, contentW)+gtx.Dp(inset.Top+inset.Bottom),
					gtx.Dp(unit.Dp(150)),
					gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)),
				)
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							// The card takes the press before the dim beneath
							// can have it: the fields rarely fill the card, and
							// a click on what is left used to shut the dialog.
							absorbDialogCardPress(gtx, &ui.UnitMeshMatCardTag)
							return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitMeshMatColumn(ui, gtx, func(gtx layout.Context) layout.Dimensions {
									return PanelScrollView(ui, gtx, &ui.UnitMeshMatScroll, func(gtx layout.Context) layout.Dimensions { return UnitMeshMatFields(ui, gtx) })
								})
							})
						}),
					)
				})
			})
		}),
	)
}
