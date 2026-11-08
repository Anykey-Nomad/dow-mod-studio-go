package app

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"fmt"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"path/filepath"
	"strings"
)

// layoutClassicOEArea is the whole section: the panel column, then the
// preview with its playback row underneath it, then the same list panel the
// Viewport section draws, on the right — the list tabs and the preview toggles
// of the unit's model. The playback row belongs to the preview alone rather
// than to the three columns, so nothing under the panel column or the list is
// covered by controls that do not drive it. The model load is kicked off
// here the same way the Viewport section kicks it off, so opening the section
// on a unit that has not been previewed yet still ends with a model on screen.
// The Classic OE columns are dragged, so a narrow window can give the panel
// column the room a long name needs while the preview keeps enough to show a
// model in. These are the floors a drag and a small window are held to.
const (
	classicOEMinColumnDp  = 120
	classicOEMinPreviewDp = 200
)

// classicOEColumnWidths holds the two dragged widths inside those floors and
// hands back the row's three widths. The preview takes what is left, so the
// two columns cannot both grow into it.
func classicOEColumnWidths(ui *uiState, gtx layout.Context, total int) (panel, preview, side int) {
	if ui.ClassicOEPanelWidth <= 0 {
		ui.ClassicOEPanelWidth = gtx.Dp(unit.Dp(classicOEPanelDp))
	}
	if ui.ClassicOESideWidth <= 0 {
		ui.ClassicOESideWidth = gtx.Dp(unit.Dp(unitViewportSideDp))
	}
	splitter := 2 * gtx.Dp(unit.Dp(panelSplitterWidthDp))
	minColumn := gtx.Dp(unit.Dp(classicOEMinColumnDp))
	minPreview := gtx.Dp(unit.Dp(classicOEMinPreviewDp))
	// A window too narrow for the floors is left to overflow rather than to
	// collapse a column: a negative width would panic in the clip that frames
	// the column, and a column of nothing cannot be dragged back.
	room := total - splitter - minPreview
	panel = ui.ClassicOEPanelWidth
	if panel < minColumn {
		panel = minColumn
	}
	if maxPanel := room - minColumn; maxPanel > minColumn && panel > maxPanel {
		panel = maxPanel
	}
	side = ui.ClassicOESideWidth
	if side < minColumn {
		side = minColumn
	}
	if maxSide := room - panel; maxSide > minColumn && side > maxSide {
		side = maxSide
	}
	ui.ClassicOEPanelWidth, ui.ClassicOESideWidth = panel, side
	preview = total - panel - side - splitter
	if preview < minPreview {
		preview = minPreview
	}
	return panel, preview, side
}

// LayoutClassicOESplitter is the grab bar between two of those columns.
// Between frames the pointer is local to where the bar was drawn, so a drag
// keeps that rendered origin beside the widths it is dragging: using the new
// origin with old local coordinates makes a slow drag oscillate.
func LayoutClassicOESplitter(ui *uiState, gtx layout.Context, index, total int) layout.Dimensions {
	w := gtx.Dp(unit.Dp(panelSplitterWidthDp))
	gtx.Constraints.Min = image.Pt(w, gtx.Constraints.Max.Y)
	gtx.Constraints.Max = image.Pt(w, gtx.Constraints.Max.Y)
	sz := image.Pt(w, gtx.Constraints.Max.Y)
	if index < 0 || index >= len(ui.ClassicOESplitters) {
		return layout.Dimensions{Size: sz}
	}
	s := &ui.ClassicOESplitters[index]
	origin := s.LayoutOrigin
	rect := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.Tag)
	pointer.CursorColResize.Add(gtx.Ops)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &s.Tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			s.Hover = true
		case pointer.Leave:
			if !s.Dragging {
				s.Hover = false
			}
		case pointer.Press:
			s.Dragging = true
			s.Hover = true
			s.StartPointerX = float32(origin) + e.Position.X
			s.StartLeft = ui.ClassicOEPanelWidth
			s.StartRight = ui.ClassicOESideWidth
		case pointer.Drag:
			if !s.Dragging {
				break
			}
			delta := int(float32(origin) + e.Position.X - s.StartPointerX)
			if index == 0 {
				ui.ClassicOEPanelWidth = s.StartLeft + delta
			} else {
				ui.ClassicOESideWidth = s.StartRight - delta
			}
		case pointer.Release, pointer.Cancel:
			s.Dragging = false
		}
	}
	if index == 0 {
		s.LayoutOrigin = ui.ClassicOEPanelWidth
	} else {
		s.LayoutOrigin = total - ui.ClassicOESideWidth - w
	}
	col := uikit.CBorder
	if s.Hover || s.Dragging {
		col = uikit.CSelectBd
	}
	fill := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	fill.Pop()
	rect.Pop()
	return layout.Dimensions{Size: sz}
}

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
			total := gtx.Constraints.Max.X
			panelW, _, sideW := classicOEColumnWidths(ui, gtx, total)
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(FixedPanel(ui, panelW, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOELeftPanel(ui, gtx) })),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutClassicOESplitter(ui, gtx, 0, total) }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutClassicOEPreview(ui, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutClassicOESplitter(ui, gtx, 1, total) }),
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
			// The Object Editor opens a pair: the model as .sgm and the
			// object as .ebp. A mod ships the game's .whm and .whe instead,
			// so both exports sit here together, beside Save .whe.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return uikit.SmallButton(ui, gtx, &ui.ClassicOEEbpSave, "Save .ebp")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return uikit.SmallButton(ui, gtx, &ui.ClassicOESgmSave, "Save .sgm")
						}),
					)
				})
			}),
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
		whmClips := unitWhmClipSet(ui)
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
			// A motion whose blend entries name clips the model does
			// not carry is marked the way the Events list marks its
			// timeline, and the note says which ones.
			missing := unitAnimMissingList(whmClips, t.Animations)
			note := classicOEUseNote(len(t.Events), "event", "events")
			tint := uikit.CText
			if len(missing) > 0 {
				note += "  ·  not in .whm: " + strings.Join(missing, ", ")
				tint = uikit.CProblem
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				dims := UnitViewportRowTinted(ui, gtx, c, false, t.Name, note, tint)
				// A right press copies the motion's name into the
				// shared clipboard for a name field to paste.
				uikit.RowNameText(ui, gtx, uikit.RowKey("oe-motion", i), uikit.NameSpaceMotion, t.Name, dims.Size)
				return dims
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	case classicOEFolderActions:
		// The folder's own new-action button: the same record the Events
		// tab's button creates, and this section goes straight on to the
		// condition editor for it, since an empty action needs its choices
		// added there.
		if ui.ClassicOENewAct.Clicked(gtx) {
			if name, idx, ok := NewUnitAction(ui); ok {
				OpenClassicOEEventDialog(ui, classicOEDialogAction, name, idx)
			}
		}
		children := make([]layout.FlexChild, 0, len(actions)+1)
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Top: unit.Dp(2), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return SmallButton(ui, gtx, &ui.ClassicOENewAct, "New action")
			})
		}))
		if len(actions) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutClassicOEHint(ui, gtx, docMsg, "This .whe defines no actions.")
			}))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}
		if len(ui.ClassicOEActionClicks) != len(actions) {
			ui.ClassicOEActionClicks = make([]widget.Clickable, len(actions))
		}
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
				dims := UnitViewportRowIcon(ui, gtx, c, false, a.Name, note, ActionIcon(ui, a.Name), true, uikit.CText)
				// A right press copies the action's name into the
				// shared clipboard for a name field to paste.
				uikit.RowNameText(ui, gtx, uikit.RowKey("oe-action", i), uikit.NameSpaceAction, a.Name, dims.Size)
				return dims
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
	// A branch or leaf name is cut to the column's width; hovering says the
	// whole of it, which is the only way to tell two long action names apart.
	uikit.HoverTip(ui, hover, row.Name)
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
	dims := content(gtx)
	// A right press copies the node's name: a branch is an action, a leaf
	// a motion — the split the double-click already uses.
	space := uikit.NameSpaceMotion
	if row.Key != "" {
		space = uikit.NameSpaceAction
	}
	uikit.RowNameText(ui, gtx, "oe-tree:"+jumpKey, space, row.Name, dims.Size)
	return dims
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
	if ui.ClassicOEEbpSave.Clicked(gtx) {
		SaveClassicOEEbp(ui)
	}
	if ui.ClassicOESgmSave.Clicked(gtx) {
		SaveClassicOESgm(ui)
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

// SaveClassicOEEbp exports the object in the Object Editor's own .ebp format
// beside the .whe it was read from. The two files hold the same object — FX,
// events, clauses, conditions, modifiers, motions, actions and the selection
// UI: the .whe is what the game loads, the .ebp is what the official Object
// Editor opens, so a model worked on here can go into that editor without a
// conversion step in between.
func SaveClassicOEEbp(ui *uiState) {
	if ui.UnitEvtDoc == nil {
		ui.StatusText = "Save failed: no object is loaded"
		return
	}
	if ui.UnitEvtPath == "" {
		ui.StatusText = "Save failed: no object path to export"
		return
	}
	if ui.ModTree == nil {
		ui.StatusText = "Save failed: no mod loaded"
		return
	}
	data, err := ui.UnitEvtDoc.MarshalEBP()
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	path := strings.TrimSuffix(ui.UnitEvtPath, filepath.Ext(ui.UnitEvtPath)) + ".ebp"
	disk, err := ui.ModTree.WriteOverlayFile(path, data)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = fmt.Sprintf("Saved EBP %s → %s (%d bytes)", filepath.Base(path), disk, len(data))
}

// SaveClassicOESgm exports the unit's model in the Object Editor's own .sgm
// container, beside the .whm it was read from. The .whm is what the game
// loads; the .sgm is that same model in the container the editor opens, and
// the editor opens nothing else — which is why a mod of .whm files alone shows
// no model to edit in it.
func SaveClassicOESgm(ui *uiState) {
	if ui.ModTree == nil {
		ui.StatusText = "Save failed: no mod loaded"
		return
	}
	if ui.UnitModelPath == "" {
		ui.StatusText = "Save failed: this unit has no model"
		return
	}
	if !strings.EqualFold(filepath.Ext(ui.UnitModelPath), ".whm") {
		ui.StatusText = "Save — " + filepath.Base(ui.UnitModelPath) + " is already an Object Editor .sgm"
		return
	}
	path := strings.TrimSuffix(ui.UnitModelPath, filepath.Ext(ui.UnitModelPath)) + ".sgm"
	// Resolve the destination first, so a mod folder that cannot take the file
	// fails before any work and leaves nothing half written behind.
	if _, err := ui.ModTree.OverlayPath(path); err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	data, err := ui.ModTree.ReadFile(ui.UnitModelPath)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	sgm, info, err := whm.ConvertToSgm(data)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	disk, err := ui.ModTree.WriteOverlayFile(path, sgm)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	if info != nil {
		ui.StatusText = fmt.Sprintf("Saved SGM %s → %s (%d bytes, %d meshes, %d bones, %d anims)", filepath.Base(path), disk, len(sgm), info.Meshes, info.Bones, info.Animations)
	} else {
		ui.StatusText = fmt.Sprintf("Saved SGM %s → %s (%d bytes)", filepath.Base(path), disk, len(sgm))
	}
	// The new .sgm belongs in the tree beside the .whm it came from, so the
	// pair the Object Editor wants is visible where the model already was.
	if err := RefreshModTreePreservingUI(ui); err != nil {
		LogInfo("classic oe: tree refresh after %s failed: %v", filepath.Base(path), err)
	}
}
