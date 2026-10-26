package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"image"
	"image/color"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func LayoutContent(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			fillRect(gtx, uikit.CContentBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				total := gtx.Constraints.Max.X
				EnsurePanelWidths(ui, total)
				// The object interface takes the whole content width: its
				// own right-hand panel is hidden on every section, and the
				// properties panel belongs to the plain modtree view below.
				if UnitTabOwnsLeftPanel(ui) {
					return LayoutUnitTabBody(ui, gtx)
				}
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(FixedPanel(ui, ui.LeftPanelWidth, func(gtx layout.Context) layout.Dimensions { return LayoutLeftPanel(ui, gtx) })),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutPanelSplitter(ui, gtx, 0, total) }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutMiddlePanel(ui, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutPanelSplitter(ui, gtx, 1, total) }),
					layout.Rigid(FixedPanel(ui, ui.RightPanelWidth, func(gtx layout.Context) layout.Dimensions { return LayoutRightPanel(ui, gtx) })),
				)
			})
		}),
	)
}

// flexGap is a flexible gap inside a Flex row or column, used to push the
// children after it to the far end of the line.
//
// It has to report the size it was given. Gio measures a Flexed child with
// exact constraints for its share of the free space, so a widget that returns
// an empty Dimensions collapses to zero and everything after it slides back
// against the preceding children instead of being pushed away.
func flexGap() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
}

// RowGap lives in uikit; the alias keeps every call site as it was.
var rowGap = uikit.RowGap

// PanelFrame lives in uikit; the alias keeps every call site as it was.
var panelFrame = uikit.PanelFrame

func LayoutLeftPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		if !ui.ContextMenuVisible {
			return LayoutLeftPanelContent(ui, gtx)
		}
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions { return LayoutLeftPanelContent(ui, gtx) }),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				pass := pointer.PassOp{}.Push(gtx.Ops)
				dims := ui.ContextDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
				pass.Pop()
				return dims
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions { return LayoutFileContextMenu(ui, gtx) }),
		)
	})
}

func LayoutLeftPanelContent(ui *uiState, gtx layout.Context) layout.Dimensions {
	if ui.ModTree == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return centeredPanelLabel(gtx, ui.Th, "No module loaded", unit.Sp(12), uikit.CTextDim)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return centeredPanelWidget(gtx, func(gtx layout.Context) layout.Dimensions {
								return material.Clickable(gtx, &widget.Clickable{}, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(11), "File → Open Module…")
									l.Color = rgb(0x0066CC)
									return l.Layout(gtx)
								})
							})
						})
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						extra := ui.StatusText
						if extra == "" {
							extra = "Pick a .module from Dawn of War Definitive Edition"
						}
						return centeredPanelLabel(gtx, ui.Th, extra, unit.Sp(10), uikit.CTextDim)
					}),
				)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(22)))
					fillRect(gtx, uikit.CHeaderBg, sz)
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						modName := ui.ModulePath
						if modName != "" {
							modName = filepath.Base(modName)
						} else {
							modName = "Data"
						}
						l := material.Label(ui.Th, unit.Sp(10), modName)
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						return LayoutListWithScrollbar(ui, gtx, &ui.LeftScroll, &ui.LeftList, 22, len(ui.VisibleNodes), func(gtx layout.Context, index int) layout.Dimensions {
							return LayoutTreeRow(ui, gtx, index, ui.VisibleNodes[index])
						})
					}),
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						if ui.LeftTreePointerTag == nil {
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}
						// The one target of the tree that is not a row: it
						// carries the right press, which no click gesture sees.
						// Both wrappers are load bearing — the clip keeps the
						// area to the panel, and the PassOp keeps the rows
						// underneath it clickable.
						pass := pointer.PassOp{}.Push(gtx.Ops)
						area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
						event.Op(gtx.Ops, ui.LeftTreePointerTag)
						area.Pop()
						pass.Pop()
						return layout.Dimensions{Size: gtx.Constraints.Max}
					}),
				)
			})
		}),
	)
}

func LayoutFileContextMenu(ui *uiState, gtx layout.Context) layout.Dimensions {
	if !ui.ContextMenuVisible {
		return layout.Dimensions{}
	}
	menuW := gtx.Dp(unit.Dp(196))
	menuH := gtx.Dp(unit.Dp(86))
	y := 32 + ui.ContextMenuAnchor*gtx.Dp(unit.Dp(19)) - ui.LeftScroll.Offset
	if y < 28 {
		y = 28
	}
	if maxY := gtx.Constraints.Max.Y - menuH - 4; y > maxY {
		y = maxY
	}
	if y < 0 {
		y = 0
	}
	gtx2 := gtx
	deleteDisabled := !CanDeleteCurrentModFile(ui, ui.ContextMenuPath)
	defer op.Offset(image.Pt(gtx.Dp(unit.Dp(18)), y)).Push(gtx2.Ops).Pop()
	gtx2.Constraints.Min = image.Pt(menuW, menuH)
	gtx2.Constraints.Max = image.Pt(menuW, menuH)
	return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(3))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							_, base := modtree.VirtualDirAndBase(ui.ContextMenuPath)
							l := material.Label(ui.Th, unit.Sp(11), base)
							l.Color = uikit.CText
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.ContextDuplicate, "Duplicate")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.ContextDelete, "Delete", deleteDisabled)
						}),
					)
				})
			}),
		)
	})
}

// ─── merged from panel_layout.go ───────────────────────────────────────────────

// absorbDialogCardPress covers a dialog card with an input area that takes
// pointer events and gives nothing back.
//
// A card is stacked over the dialog's dim, and a press on the card's own empty
// space reaches no control, so without this the press falls through to the dim
// — which is the dialog's close target — and the dialog shut while the user was
// reaching for a field. Registering this area over the card, ahead of the
// controls laid out after it, is what makes the card modal: controls stay on
// top of it, and only a press outside the card's border reaches the dim, which
// is where a press is meant to close.
func absorbDialogCardPress(gtx layout.Context, tag event.Tag) {
	st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, tag)
	for {
		if _, ok := gtx.Event(pointer.Filter{
			Target: tag,
			Kinds:  pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll,
		}); !ok {
			break
		}
	}
	st.Pop()
}

type panelSplitterState = uistate.PanelSplitterState

type panelScrollState = uistate.PanelScrollState

// ClampInt lives in uikit; the alias keeps every call site as it was.
var clampInt = uikit.ClampInt

func EnsurePanelWidths(ui *uiState, total int) {
	if total <= 0 {
		return
	}
	if ui.LeftPanelWidth <= 0 {
		ui.LeftPanelWidth = total * 25 / 100
	}
	if ui.RightPanelWidth <= 0 {
		ui.RightPanelWidth = total * 40 / 100
	}
	ClampPanelWidths(ui, total)
}

func ClampPanelWidths(ui *uiState, total int) {
	if total <= 0 {
		return
	}
	minLeft := PanelWidthDp(ui, total, panelSplitterMinDp)
	minMid := PanelWidthDp(ui, total, panelSplitterMinMidDp)
	minRight := minLeft
	// Use the available width conservatively on small windows. The middle
	// panel keeps the larger minimum because it is the editing surface.
	if total < minLeft+minMid+minRight+2*PanelSplitterWidth(ui, total) {
		minLeft = total / 5
		minMid = total / 3
		minRight = total / 5
	}
	ui.LeftPanelWidth = clampInt(ui.LeftPanelWidth, minLeft, total-minMid-minRight-2*PanelSplitterWidth(ui, total))
	ui.RightPanelWidth = clampInt(ui.RightPanelWidth, minRight, total-ui.LeftPanelWidth-minMid-PanelSplitterWidth(ui, total))
	if total-ui.LeftPanelWidth-ui.RightPanelWidth-2*PanelSplitterWidth(ui, total) < minMid {
		ui.RightPanelWidth = total - ui.LeftPanelWidth - minMid - 2*PanelSplitterWidth(ui, total)
		if ui.RightPanelWidth < minRight {
			ui.RightPanelWidth = minRight
			ui.LeftPanelWidth = total - minRight - minMid - 2*PanelSplitterWidth(ui, total)
		}
	}
}

func PanelWidthDp(ui *uiState, total, width int) int {
	if total <= 0 {
		return width
	}
	return width
}

func PanelSplitterWidth(ui *uiState, total int) int {
	if total <= 0 {
		return panelSplitterWidthDp
	}
	return panelSplitterWidthDp
}

func panelSplitterLayoutOrigin(index, total, splitterWidth, leftWidth, rightWidth int) int {
	if index == 1 {
		return total - rightWidth - splitterWidth
	}
	return leftWidth
}

func panelSplitterWidthFromDrag(index int, pointerX, pressPointerX float32, startLeft, startRight int) (int, int) {
	delta := int(pointerX - pressPointerX)
	if index == 0 {
		return startLeft + delta, startRight
	}
	return startLeft, startRight - delta
}

func LayoutPanelSplitter(ui *uiState, gtx layout.Context, index int, total int) layout.Dimensions {
	w := gtx.Dp(unit.Dp(panelSplitterWidthDp))
	gtx.Constraints.Min.X = w
	gtx.Constraints.Max.X = w
	gtx.Constraints.Min.Y = 0
	s := &ui.PanelSplitters[index]
	// Input is handled before layout, so event positions are local to the
	// splitter's position in the previous frame. Keep that rendered origin
	// separately from the widths being updated by this frame's input; using
	// the new origin with old local coordinates makes slow drags oscillate.
	eventOrigin := s.LayoutOrigin
	rect := clip.Rect{Max: image.Pt(w, gtx.Constraints.Max.Y)}.Push(gtx.Ops)
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
			s.StartPointerX = float32(eventOrigin) + e.Position.X
			s.StartLeft = ui.LeftPanelWidth
			s.StartRight = ui.RightPanelWidth
		case pointer.Drag:
			if !s.Dragging {
				break
			}
			pointerX := float32(eventOrigin) + e.Position.X
			ui.LeftPanelWidth, ui.RightPanelWidth = panelSplitterWidthFromDrag(
				index, pointerX, s.StartPointerX, s.StartLeft, s.StartRight,
			)
			ClampPanelWidths(ui, total)
		case pointer.Release, pointer.Cancel:
			s.Dragging = false
		}
	}
	// Record the origin of the operations emitted below. Pointer positions
	// received before the next layout will be relative to this origin.
	s.LayoutOrigin = panelSplitterLayoutOrigin(index, total, w, ui.LeftPanelWidth, ui.RightPanelWidth)
	col := uikit.CBorder
	if s.Hover || s.Dragging {
		col = rgb(0x0078D4)
	}
	fillRect(gtx, col, image.Pt(w, gtx.Constraints.Max.Y))
	rect.Pop()
	return layout.Dimensions{Size: image.Pt(w, gtx.Constraints.Max.Y)}
}

// FixedPanel lives in uikit; the alias keeps every call site as it was.
var FixedPanel = uikit.FixedPanel

func PanelWithScroll(ui *uiState, gtx layout.Context, state *panelScrollState, content layout.Widget) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return PanelScrollView(ui, gtx, state, content)
	})
}

// PanelScrollMeasureHeight lives in uikit; the alias keeps every call site as it was.
const panelScrollMeasureHeight = uikit.PanelScrollMeasureHeight

// PanelScrollWheelStepPx lives in uikit; the alias keeps every call site as it was.
const panelScrollWheelStepPx = uikit.PanelScrollWheelStepPx

// PanelScrollView lives in uikit; the alias keeps every call site as it was.
var PanelScrollView = uikit.PanelScrollView

// PanelScrollThumbSize lives in uikit; the alias keeps every call site as it was.
var panelScrollThumbSize = uikit.PanelScrollThumbSize

// DrawPanelScrollbar lives in uikit; the alias keeps every call site as it was.
var drawPanelScrollbar = uikit.DrawPanelScrollbar

func LayoutListWithScrollbar(ui *uiState, gtx layout.Context, state *panelScrollState, list *layout.List, rowHeight, count int, element layout.ListElement) layout.Dimensions {
	if list == nil {
		return layout.Dimensions{}
	}
	list.Axis = layout.Vertical
	dims := list.Layout(gtx, count, element)
	if state != nil {
		state.Viewport = gtx.Constraints.Max.Y
		state.ContentHeight = list.Position.Length
		state.HasBar = state.ContentHeight > state.Viewport
		if state.HasBar {
			state.Offset = clampInt(state.Offset, 0, state.ContentHeight-state.Viewport)
			// List owns wheel scrolling; mirror its position for the thumb.
			state.Offset = list.Position.First*rowHeight + list.Position.Offset
			state.Offset = clampInt(state.Offset, 0, maxInt(0, state.ContentHeight-state.Viewport))
		} else {
			state.Offset = 0
		}
		barW := 0
		if state.HasBar {
			barW = gtx.Dp(unit.Dp(panelScrollbarWidthDp))
		}
		if state.HasBar {
			barRect := clip.Rect{Min: image.Pt(gtx.Constraints.Max.X-barW, 0), Max: gtx.Constraints.Max}.Push(gtx.Ops)
			event.Op(gtx.Ops, &state.Tag)
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: &state.Tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave})
				if !ok {
					break
				}
				e, ok := ev.(pointer.Event)
				if !ok {
					continue
				}
				switch e.Kind {
				case pointer.Enter:
					state.HoverBar = true
				case pointer.Leave:
					if !state.Dragging {
						state.HoverBar = false
					}
				case pointer.Press:
					state.Dragging = true
					state.StartY = e.Position.Y
					state.StartOffset = state.Offset
				case pointer.Drag:
					if state.Dragging {
						thumb := panelScrollThumbSize(state, gtx.Constraints.Max.Y)
						track := gtx.Constraints.Max.Y - thumb
						if track > 0 {
							delta := e.Position.Y - state.StartY
							items := int(delta / float32(maxInt(1, rowHeight)))
							list.ScrollBy(float32(items))
						}
					}
				case pointer.Release, pointer.Cancel:
					state.Dragging = false
				}
			}
			barRect.Pop()
		}
		if state.HasBar {
			// The list already occupies the panel; draw only the bar over it.
			drawPanelScrollbar(gtx, state, gtx.Constraints.Max, barW)
		}
	}
	return dims
}

// ─── merged from tree_middle_ui.go ───────────────────────────────────────────────

func LayoutTreeRow(ui *uiState, gtx layout.Context, idx int, n modtree.VisibleNode) layout.Dimensions {
	selected := n.FullPath == ui.SelectedTreePath
	contextSelected := ui.ContextMenuVisible && n.IsFile && n.FullPath == ui.ContextMenuPath
	if n.IsFile && n.FullPath == ui.SelectedPath {
		selected = true
	}
	expandable := n.IsDir && n.HasKids
	open := ui.Expanded[n.FullPath]
	h := gtx.Dp(unit.Dp(19))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	ui.LeftRowHeightPx = h
	RegisterTreeRowClick(ui, gtx, idx, h)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if contextSelected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, uikit.CMenuOpen, sz)
				b := widget.Border{Color: uikit.CMenuHover, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				return layout.Dimensions{Size: sz}
			}
			if selected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, uikit.CSelectBg, sz)
				bd := uikit.CSelectBd
				if TreeHasKeyboard(ui) {
					// The row the keys act on, which is worth telling apart
					// from a row that is merely selected: the middle tree can
					// hold the keyboard while this row stays selected.
					bd = uikit.CFocusBd
				}
				b := widget.Border{Color: bd, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				return layout.Dimensions{Size: sz}
			}
			if ui.RowHover[idx] && !selected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, uikit.CMenuHover, sz)
				return layout.Dimensions{Size: sz}
			}
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			indentW := gtx.Dp(unit.Dp(float32(n.Depth)*14 + 4))
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: unit.Dp(float32(indentW))}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !expandable {
						return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
					}
					sz := gtx.Dp(unit.Dp(9))
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: uikit.CPanelBg}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					b := widget.Border{Color: rgb(0x808080), Width: unit.Dp(1)}
					gtx.Constraints.Min = box
					gtx.Constraints.Max = box
					return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							s := "-"
							if !open {
								s = "+"
							}
							l := material.Label(ui.Th, unit.Sp(9), s)
							l.Color = uikit.CText
							return l.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(unit.Dp(12))
					var col color.NRGBA
					if n.IsDir {
						col = rgb(0xFFD54F)
					} else {
						if hasSuffix(n.Name, ".rgd") {
							col = rgb(0x9DB7D0)
						} else if hasSuffix(n.Name, ".fda") {
							col = rgb(0xE57373)
						} else if hasSuffix(n.Name, ".lua") {
							col = rgb(0x6BB5A0)
						} else if hasSuffix(n.Name, ".tga") || hasSuffix(n.Name, ".dds") {
							col = rgb(0xD07DBB)
						} else {
							col = rgb(0xC8C8C8)
						}
					}
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: col}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					return layout.Dimensions{Size: box}
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := n.Name
					if len(lbl) > 38 && n.IsFile {
						lbl = lbl[:35] + "…"
					}
					l := material.Label(ui.Th, unit.Sp(11), lbl)
					if selected {
						l.Color = uikit.CText
						l.Font.Weight = font.Medium
					} else if n.IsFile {
						switch n.Kind {
						case modtree.KindThisMod:
							l.Color = uikit.CTreeThis
						case modtree.KindEngine:
							l.Color = uikit.CTreeEngine
						default:
							l.Color = uikit.CTreeOther
						}
					} else {
						l.Color = uikit.CText
					}
					if n.IsDir {
						l.Font.Weight = font.Medium
					}
					return l.Layout(gtx)
				}),
			)
		}))
}

// registerTreeRowClick gives the row its own pointer area and its own click
// gesture over it. One area per row, rather than one area over the whole list
// with the row worked out from the position afterwards, is what makes a click
// a property of a row: the release has to land on the row the press did, and
// Gio says so without the app keeping any of it.
//
// The clip is what makes the area the row and not the window, and it is the
// part that matters: without it every row is hit by every position and the last
// one laid out answers for all of them. The PassOp says the row is stacked over
// the targets below it rather than in place of them — the list's own wheel
// target and the tree's file menu are both registered underneath the rows.
func RegisterTreeRowClick(ui *uiState, gtx layout.Context, idx, h int) {
	if idx < 0 || idx >= len(ui.RowClicks) {
		return
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Push(gtx.Ops)
	ui.RowClicks[idx].Add(gtx.Ops)
	area.Pop()
	pass.Pop()
}

// treeHasKeyboard reports whether the arrow keys, Enter and Delete act on the
// file tree. It is a click away and stays there: a click in the middle tree
// takes the keyboard over, and nothing else does, so the tree keeps answering
// to the keys the way a file list is expected to.
func TreeHasKeyboard(ui *uiState) bool {
	return ui.KeyboardArea == "files"
}

func hasSuffix(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	return s[len(s)-len(suf):] == suf
}

func MiddleTreeRow(ui *uiState, gtx layout.Context, indent int, txt string, expandable, open bool) layout.Dimensions {
	h := gtx.Dp(unit.Dp(18))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			indentW := gtx.Dp(unit.Dp(float32(indent)*14 + 4))
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: unit.Dp(float32(indentW))}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !expandable {
						return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
					}
					sz := gtx.Dp(unit.Dp(9))
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: uikit.CPanelBg}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					b := widget.Border{Color: rgb(0x808080), Width: unit.Dp(1)}
					gtx.Constraints.Min = box
					gtx.Constraints.Max = box
					return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							s := "-"
							if !open {
								s = "+"
							}
							l := material.Label(ui.Th, unit.Sp(9), s)
							l.Color = uikit.CText
							return l.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(unit.Dp(12))
					col := rgb(0xFFD54F)
					if hasSuffix(txt, ".lua") {
						col = rgb(0x6BB5A0)
					}
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: col}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					return layout.Dimensions{Size: box}
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), txt)
					l.Color = uikit.CText
					return l.Layout(gtx)
				}),
			)
		}),
	)
}

func LayoutRgdMidRow(ui *uiState, gtx layout.Context, t *fileTab, idx int, v rgdVisible) layout.Dimensions {
	selected := v.Selected
	expandable := v.IsTable && v.HasKids
	open := v.Expanded
	h := gtx.Dp(unit.Dp(19))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return t.RgdMidClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if selected {
					sz := image.Pt(gtx.Constraints.Max.X, h)
					fillRect(gtx, uikit.CSelectBg, sz)
					b := widget.Border{Color: uikit.CSelectBd, Width: unit.Dp(1)}
					gtx.Constraints.Min = sz
					gtx.Constraints.Max = sz
					_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
					return layout.Dimensions{Size: sz}
				}
				if t.RgdMidClicks[idx].Hovered() && !selected {
					sz := image.Pt(gtx.Constraints.Max.X, h)
					fillRect(gtx, uikit.CMenuHover, sz)
					return layout.Dimensions{Size: sz}
				}
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				indentW := gtx.Dp(unit.Dp(float32(v.Depth)*14 + 4))
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(layout.Spacer{Width: unit.Dp(float32(indentW))}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !expandable {
							return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
						}
						sz := gtx.Dp(unit.Dp(9))
						box := image.Pt(sz, sz)
						st := clip.Rect{Max: box}.Push(gtx.Ops)
						paint.ColorOp{Color: uikit.CPanelBg}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						st.Pop()
						b := widget.Border{Color: rgb(0x808080), Width: unit.Dp(1)}
						gtx.Constraints.Min = box
						gtx.Constraints.Max = box
						return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								s := "-"
								if !open {
									s = "+"
								}
								l := material.Label(ui.Th, unit.Sp(9), s)
								l.Color = uikit.CText
								return l.Layout(gtx)
							})
						})
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(unit.Dp(12))
						var col color.NRGBA
						if v.IsTable {
							col = rgb(0xFFD54F)
						} else {
							switch v.Type {
							case rgd.RgdTypeFloat, rgd.RgdTypeInt:
								col = rgb(0x6BB5A0)
							case rgd.RgdTypeBool:
								col = rgb(0x9DB7D0)
							case rgd.RgdTypeString, rgd.RgdTypeWString:
								col = rgb(0xD07DBB)
							default:
								col = rgb(0xC8C8C8)
							}
						}
						box := image.Pt(sz, sz)
						st := clip.Rect{Max: box}.Push(gtx.Ops)
						paint.ColorOp{Color: col}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						st.Pop()
						return layout.Dimensions{Size: box}
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := v.Name
						if len(lbl) > 48 {
							lbl = lbl[:45] + "…"
						}
						l := material.Label(ui.Th, unit.Sp(11), lbl)
						if selected {
							l.Color = uikit.CText
							l.Font.Weight = font.Medium
						} else {
							l.Color = uikit.CText
							if v.Depth == 0 {
								l.Font.Weight = font.Medium
							}
						}
						return l.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func MiddleHeader(ui *uiState, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, uikit.CHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(unit.Dp(14))
						box := image.Pt(sz, sz)
						st := clip.Rect{Max: box}.Push(gtx.Ops)
						paint.ColorOp{Color: rgb(0x6B6B6B)}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						st.Pop()
						return layout.Dimensions{Size: box}
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						title := "GameData"
						if tab := ActiveTab(ui); tab != nil {
							if tab.Kind == "fda" {
								title = filepath.Base(tab.Path)
								if title == "" {
									title = "FDA"
								}
							} else if tab.Kind == "text" {
								title = filepath.Base(tab.Path)
							} else if tab.Kind == "rgd" {
								if tab.RgdFile != nil {
									title = filepath.Base(tab.Path)
									if e := RgdSelectedEntryFor(ui, tab); e != nil && e != tab.RgdFile.Root {
										title = e.Name
										if idx := strings.LastIndex(tab.RgdSelected, "/"); idx >= 0 {
											title = tab.RgdSelected[idx+1:]
										}
									}
								} else {
									title = filepath.Base(tab.Path)
								}
							}
						} else if ui.SelectedPath != "" {
							title = ui.SelectedPath
							if strings.Contains(title, "\\") {
								title = title[strings.LastIndex(title, "\\")+1:]
							}
						}
						l := material.Label(ui.Th, unit.Sp(11), title)
						l.Color = uikit.CText
						l.Font.Weight = font.Medium
						return l.Layout(gtx)
					}),
				)
			})
		}),
	)
}
