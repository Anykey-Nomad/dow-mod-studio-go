package main

import (
	"image"
	"image/color"
	"math"
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

func (ui *uiState) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			fillRect(gtx, cContentBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				total := gtx.Constraints.Max.X
				ui.ensurePanelWidths(total)
				// The object interface takes the whole content width: its
				// own right-hand panel is hidden on every section, and the
				// properties panel belongs to the plain modtree view below.
				if ui.unitTabOwnsLeftPanel() {
					return ui.layoutUnitTabBody(gtx)
				}
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(ui.fixedPanel(ui.leftPanelWidth, ui.layoutLeftPanel)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutPanelSplitter(gtx, 0, total) }),
					layout.Flexed(1, ui.layoutMiddlePanel),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutPanelSplitter(gtx, 1, total) }),
					layout.Rigid(ui.fixedPanel(ui.rightPanelWidth, ui.layoutRightPanel)),
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

// rowGap is the same spacer for a row of controls, with one difference: it
// takes the width left over on the line and none of the height. A row that
// uses flexGap is as tall as the space it was given, which for a row under a
// canvas means taking the canvas's height and leaving it nothing.
func rowGap() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
	}
}

func panelFrame(gtx layout.Context, content layout.Widget) layout.Dimensions {
	b := widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				rrect(gtx, cPanelBg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(content),
		)
	})
}

func (ui *uiState) layoutLeftPanel(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		if !ui.contextMenuVisible {
			return ui.layoutLeftPanelContent(gtx)
		}
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(ui.layoutLeftPanelContent),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				pass := pointer.PassOp{}.Push(gtx.Ops)
				dims := ui.contextDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
				pass.Pop()
				return dims
			}),
			layout.Stacked(ui.layoutFileContextMenu),
		)
	})
}

func (ui *uiState) layoutLeftPanelContent(gtx layout.Context) layout.Dimensions {
	if ui.modTree == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return centeredPanelLabel(gtx, ui.th, "No module loaded", unit.Sp(12), cTextDim)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return centeredPanelWidget(gtx, func(gtx layout.Context) layout.Dimensions {
								return material.Clickable(gtx, &widget.Clickable{}, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.th, unit.Sp(11), "File → Open Module…")
									l.Color = rgb(0x0066CC)
									return l.Layout(gtx)
								})
							})
						})
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						extra := ui.statusText
						if extra == "" {
							extra = "Pick a .module from Dawn of War Definitive Edition"
						}
						return centeredPanelLabel(gtx, ui.th, extra, unit.Sp(10), cTextDim)
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
					fillRect(gtx, cHeaderBg, sz)
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						modName := ui.modulePath
						if modName != "" {
							modName = filepath.Base(modName)
						} else {
							modName = "Data"
						}
						l := material.Label(ui.th, unit.Sp(10), modName)
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						return ui.layoutListWithScrollbar(gtx, &ui.leftScroll, &ui.leftList, 22, len(ui.visibleNodes), func(gtx layout.Context, index int) layout.Dimensions {
							return ui.layoutTreeRow(gtx, index, ui.visibleNodes[index])
						})
					}),
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						if ui.leftTreePointerTag == nil {
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}
						// The one target of the tree that is not a row: it
						// carries the right press, which no click gesture sees.
						// Both wrappers are load bearing — the clip keeps the
						// area to the panel, and the PassOp keeps the rows
						// underneath it clickable.
						pass := pointer.PassOp{}.Push(gtx.Ops)
						area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
						event.Op(gtx.Ops, ui.leftTreePointerTag)
						area.Pop()
						pass.Pop()
						return layout.Dimensions{Size: gtx.Constraints.Max}
					}),
				)
			})
		}),
	)
}

func (ui *uiState) layoutFileContextMenu(gtx layout.Context) layout.Dimensions {
	if !ui.contextMenuVisible {
		return layout.Dimensions{}
	}
	menuW := gtx.Dp(unit.Dp(196))
	menuH := gtx.Dp(unit.Dp(86))
	y := 32 + ui.contextMenuAnchor*gtx.Dp(unit.Dp(19)) - ui.leftScroll.offset
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
	deleteDisabled := !ui.canDeleteCurrentModFile(ui.contextMenuPath)
	defer op.Offset(image.Pt(gtx.Dp(unit.Dp(18)), y)).Push(gtx2.Ops).Pop()
	gtx2.Constraints.Min = image.Pt(menuW, menuH)
	gtx2.Constraints.Max = image.Pt(menuW, menuH)
	return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(3))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							_, base := modtree.VirtualDirAndBase(ui.contextMenuPath)
							l := material.Label(ui.th, unit.Sp(11), base)
							l.Color = cText
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.contextDuplicate, "Duplicate")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.contextDelete, "Delete", deleteDisabled)
						}),
					)
				})
			}),
		)
	})
}

// ─── merged from panel_layout.go ───────────────────────────────────────────────

const (
	panelSplitterWidthDp  = 6
	panelScrollbarWidthDp = 10
	panelSplitterMinDp    = 140
	panelSplitterMinMidDp = 220
)

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

type panelSplitterState struct {
	tag           event.Tag
	hover         bool
	dragging      bool
	layoutOrigin  int
	startPointerX float32

	startLeft  int
	startRight int
}

type panelScrollState struct {
	tag           event.Tag
	scrollTag     event.Tag
	offset        int
	contentHeight int
	viewport      int
	hasBar        bool
	hoverBar      bool
	dragging      bool
	startY        float32

	startOffset int
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (ui *uiState) ensurePanelWidths(total int) {
	if total <= 0 {
		return
	}
	if ui.leftPanelWidth <= 0 {
		ui.leftPanelWidth = total * 25 / 100
	}
	if ui.rightPanelWidth <= 0 {
		ui.rightPanelWidth = total * 40 / 100
	}
	ui.clampPanelWidths(total)
}

func (ui *uiState) clampPanelWidths(total int) {
	if total <= 0 {
		return
	}
	minLeft := ui.panelWidthDp(total, panelSplitterMinDp)
	minMid := ui.panelWidthDp(total, panelSplitterMinMidDp)
	minRight := minLeft
	// Use the available width conservatively on small windows. The middle
	// panel keeps the larger minimum because it is the editing surface.
	if total < minLeft+minMid+minRight+2*ui.panelSplitterWidth(total) {
		minLeft = total / 5
		minMid = total / 3
		minRight = total / 5
	}
	ui.leftPanelWidth = clampInt(ui.leftPanelWidth, minLeft, total-minMid-minRight-2*ui.panelSplitterWidth(total))
	ui.rightPanelWidth = clampInt(ui.rightPanelWidth, minRight, total-ui.leftPanelWidth-minMid-ui.panelSplitterWidth(total))
	if total-ui.leftPanelWidth-ui.rightPanelWidth-2*ui.panelSplitterWidth(total) < minMid {
		ui.rightPanelWidth = total - ui.leftPanelWidth - minMid - 2*ui.panelSplitterWidth(total)
		if ui.rightPanelWidth < minRight {
			ui.rightPanelWidth = minRight
			ui.leftPanelWidth = total - minRight - minMid - 2*ui.panelSplitterWidth(total)
		}
	}
}

func (ui *uiState) panelWidthDp(total, width int) int {
	if total <= 0 {
		return width
	}
	return width
}

func (ui *uiState) panelSplitterWidth(total int) int {
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

func (ui *uiState) layoutPanelSplitter(gtx layout.Context, index int, total int) layout.Dimensions {
	w := gtx.Dp(unit.Dp(panelSplitterWidthDp))
	gtx.Constraints.Min.X = w
	gtx.Constraints.Max.X = w
	gtx.Constraints.Min.Y = 0
	s := &ui.panelSplitters[index]
	// Input is handled before layout, so event positions are local to the
	// splitter's position in the previous frame. Keep that rendered origin
	// separately from the widths being updated by this frame's input; using
	// the new origin with old local coordinates makes slow drags oscillate.
	eventOrigin := s.layoutOrigin
	rect := clip.Rect{Max: image.Pt(w, gtx.Constraints.Max.Y)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.tag)
	pointer.CursorColResize.Add(gtx.Ops)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &s.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			s.hover = true
		case pointer.Leave:
			if !s.dragging {
				s.hover = false
			}
		case pointer.Press:
			s.dragging = true
			s.hover = true
			s.startPointerX = float32(eventOrigin) + e.Position.X
			s.startLeft = ui.leftPanelWidth
			s.startRight = ui.rightPanelWidth
		case pointer.Drag:
			if !s.dragging {
				break
			}
			pointerX := float32(eventOrigin) + e.Position.X
			ui.leftPanelWidth, ui.rightPanelWidth = panelSplitterWidthFromDrag(
				index, pointerX, s.startPointerX, s.startLeft, s.startRight,
			)
			ui.clampPanelWidths(total)
		case pointer.Release, pointer.Cancel:
			s.dragging = false
		}
	}
	// Record the origin of the operations emitted below. Pointer positions
	// received before the next layout will be relative to this origin.
	s.layoutOrigin = panelSplitterLayoutOrigin(index, total, w, ui.leftPanelWidth, ui.rightPanelWidth)
	col := cBorder
	if s.hover || s.dragging {
		col = rgb(0x0078D4)
	}
	fillRect(gtx, col, image.Pt(w, gtx.Constraints.Max.Y))
	rect.Pop()
	return layout.Dimensions{Size: image.Pt(w, gtx.Constraints.Max.Y)}
}

func (ui *uiState) fixedPanel(width int, content layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = width
		gtx.Constraints.Max.X = width
		return content(gtx)
	}
}

func (ui *uiState) panelWithScroll(gtx layout.Context, state *panelScrollState, content layout.Widget) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return ui.panelScrollView(gtx, state, content)
	})
}

// panelScrollMeasureHeight is how much room a panel's content is offered when
// it is measured for scrolling. Large enough that no panel ever fills it, small
// enough that a widget scaling a length by it stays inside a float32.
const panelScrollMeasureHeight = 1 << 20

// panelScrollWheelStepPx is how many pixels of the panel one unit of the
// wheel's delta moves it by. One notch is wheelNotchDelta, so a click moves
// about three list rows, which is what a wheel is for. It used to be forty
// eight, read as pixels per notch, which against a delta of 120 asked for five
// thousand pixels a click: every notch slammed the list to one end or the
// other, and the rows in between could not be reached at all.
const panelScrollWheelStepPx = 0.55

func (ui *uiState) panelScrollView(gtx layout.Context, state *panelScrollState, content layout.Widget) layout.Dimensions {
	if state == nil {
		return content(gtx)
	}
	viewport := gtx.Constraints.Max
	if viewport.X <= 0 || viewport.Y <= 0 {
		return content(gtx)
	}

	// First lay out with input disabled to learn the natural height. The
	// resulting operations are discarded; the real pass below is the only one
	// visible to the user and therefore does not duplicate input handling.
	//
	// The height has to be released for the measure to mean anything. A
	// widget that fills the space it is handed — a button, a list that is
	// meant to take what is left — reports the viewport back as its own
	// height, and then the content is never taller than the panel, no
	// scrollbar appears, and whatever is at the bottom of it is quietly
	// squeezed off screen instead. Giving the measure an unbounded height
	// and reading the answer back is what the scrollbar is for.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	measured := content(measureGTX)
	if measured.Size.Y >= panelScrollMeasureHeight {
		// Something in there is elastic, and left to itself it swallows the
		// room. Measure it the ordinary way instead of believing a height
		// that is really just the number that was offered.
		measureGTX.Constraints.Max.Y = viewport.Y
		measured = content(measureGTX)
	}
	macro.Stop()
	state.contentHeight = measured.Size.Y
	state.viewport = viewport.Y
	state.hasBar = state.contentHeight > state.viewport
	barW := 0
	if state.hasBar {
		barW = gtx.Dp(unit.Dp(panelScrollbarWidthDp))
	}
	state.offset = clampInt(state.offset, 0, maxInt(0, state.contentHeight-state.viewport))

	area := clip.Rect{Max: viewport}.Push(gtx.Ops)
	// Wheel events cover the whole panel, but are pass-through so buttons and
	// editors inside the panel still receive their own pointer events.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &state.scrollTag)
	pass.Pop()
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &state.scrollTag,
			Kinds:  pointer.Scroll,
			// A filter with no scroll range takes the whole of a wheel
			// gesture and hands the panel none of it, so the range has to be
			// opened up. This is what the wheel over a list does, and the
			// lists of the object window are the longest thing in the app.
			ScrollX: pointer.ScrollRange{Min: math.MinInt, Max: math.MaxInt},
			ScrollY: pointer.ScrollRange{Min: math.MinInt, Max: math.MaxInt},
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if e.Kind == pointer.Scroll && state.hasBar {
			// Gio's wheel is positive downwards, which is the direction the
			// offset grows in, and Windows negates the raw wheel delta on the
			// way in, so this is the whole of the sign there is to it.
			state.offset += int(e.Scroll.Y * panelScrollWheelStepPx)
		}
	}
	// Only the actual scrollbar strip accepts press/drag events. Registering a
	// full-panel handler here would cover the tree rows and prevent selection.
	if state.hasBar {
		barRect := clip.Rect{Min: image.Pt(viewport.X-barW, 0), Max: viewport}.Push(gtx.Ops)
		event.Op(gtx.Ops, &state.tag)
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &state.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			switch e.Kind {
			case pointer.Enter:
				state.hoverBar = true
			case pointer.Leave:
				if !state.dragging {
					state.hoverBar = false
				}
			case pointer.Press:
				state.dragging = true
				state.startY = e.Position.Y
				state.startOffset = state.offset
			case pointer.Drag:
				if state.dragging {
					thumb := panelScrollThumbSize(state, viewport.Y)
					track := viewport.Y - thumb
					if track > 0 {
						delta := e.Position.Y - state.startY
						state.offset = state.startOffset + int(delta/float32(track)*float32(maxInt(0, state.contentHeight-state.viewport)))
					}
				}
			case pointer.Release, pointer.Cancel:
				state.dragging = false
			}
		}
		barRect.Pop()
	}
	state.offset = clampInt(state.offset, 0, maxInt(0, state.contentHeight-state.viewport))

	renderGTX := gtx
	renderGTX.Constraints.Min.Y = 0
	if barW > 0 {
		renderGTX.Constraints.Min.X = 0
		renderGTX.Constraints.Max.X -= barW
	}
	// The content is laid out against the measured height rather than the
	// viewport. A Flex clamps the size it reports to the space it was handed
	// while still drawing every child at its natural position, so rendering
	// tall content against the viewport's height made each section report the
	// viewport back and the next section start inside the previous one — the
	// Object window's side panel drew the Import rows over the tail of the
	// bone list, with no background on either to say which was on top. The
	// measured height is what the layout pass above believed; rendering at
	// the same numbers keeps the sections in their own rows, and the clip
	// below is what cuts the overflow off at the viewport.
	renderH := state.contentHeight
	if renderH < viewport.Y {
		renderH = viewport.Y
	}
	renderGTX.Constraints.Max.Y = renderH
	if state.offset > 0 {
		shift := op.Offset(image.Pt(0, -state.offset)).Push(gtx.Ops)
		content(renderGTX)
		shift.Pop()
	} else {
		content(renderGTX)
	}
	area.Pop()
	if state.hasBar {
		drawPanelScrollbar(gtx, state, viewport, barW)
	}
	return layout.Dimensions{Size: viewport}
}

func panelScrollThumbSize(state *panelScrollState, viewport int) int {
	if state == nil || state.contentHeight <= viewport || viewport <= 0 {
		return viewport
	}
	thumb := viewport * viewport / state.contentHeight
	if thumb < 24 {
		thumb = 24
	}
	if thumb > viewport {
		thumb = viewport
	}
	return thumb
}

func drawPanelScrollbar(gtx layout.Context, state *panelScrollState, viewport image.Point, width int) {
	if width <= 0 || state == nil {
		return
	}
	x := viewport.X - width
	track := clip.Rect{Min: image.Pt(x, 0), Max: viewport}.Push(gtx.Ops)
	fillRect(gtx, cContentBg, image.Pt(width, viewport.Y))
	thumb := panelScrollThumbSize(state, viewport.Y)
	maxOffset := maxInt(1, state.contentHeight-state.viewport)
	y := state.offset * (viewport.Y - thumb) / maxOffset
	col := cBorder
	if state.hoverBar || state.dragging {
		col = rgb(0x0078D4)
	}
	// Offset the thumb into the track.
	// The tracking transform is kept separate so the rounded rectangle is
	// positioned in panel coordinates.
	track.Pop()
	// Draw at the correct vertical position using a clipped transform.
	shift := op.Offset(image.Pt(x+1, y)).Push(gtx.Ops)
	rrect(gtx, col, image.Pt(width-2, thumb), gtx.Dp(unit.Dp(3)))
	shift.Pop()
}

func (ui *uiState) layoutListWithScrollbar(gtx layout.Context, state *panelScrollState, list *layout.List, rowHeight, count int, element layout.ListElement) layout.Dimensions {
	if list == nil {
		return layout.Dimensions{}
	}
	list.Axis = layout.Vertical
	dims := list.Layout(gtx, count, element)
	if state != nil {
		state.viewport = gtx.Constraints.Max.Y
		state.contentHeight = list.Position.Length
		state.hasBar = state.contentHeight > state.viewport
		if state.hasBar {
			state.offset = clampInt(state.offset, 0, state.contentHeight-state.viewport)
			// List owns wheel scrolling; mirror its position for the thumb.
			state.offset = list.Position.First*rowHeight + list.Position.Offset
			state.offset = clampInt(state.offset, 0, maxInt(0, state.contentHeight-state.viewport))
		} else {
			state.offset = 0
		}
		barW := 0
		if state.hasBar {
			barW = gtx.Dp(unit.Dp(panelScrollbarWidthDp))
		}
		if state.hasBar {
			barRect := clip.Rect{Min: image.Pt(gtx.Constraints.Max.X-barW, 0), Max: gtx.Constraints.Max}.Push(gtx.Ops)
			event.Op(gtx.Ops, &state.tag)
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: &state.tag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave})
				if !ok {
					break
				}
				e, ok := ev.(pointer.Event)
				if !ok {
					continue
				}
				switch e.Kind {
				case pointer.Enter:
					state.hoverBar = true
				case pointer.Leave:
					if !state.dragging {
						state.hoverBar = false
					}
				case pointer.Press:
					state.dragging = true
					state.startY = e.Position.Y
					state.startOffset = state.offset
				case pointer.Drag:
					if state.dragging {
						thumb := panelScrollThumbSize(state, gtx.Constraints.Max.Y)
						track := gtx.Constraints.Max.Y - thumb
						if track > 0 {
							delta := e.Position.Y - state.startY
							items := int(delta / float32(maxInt(1, rowHeight)))
							list.ScrollBy(float32(items))
						}
					}
				case pointer.Release, pointer.Cancel:
					state.dragging = false
				}
			}
			barRect.Pop()
		}
		if state.hasBar {
			// The list already occupies the panel; draw only the bar over it.
			drawPanelScrollbar(gtx, state, gtx.Constraints.Max, barW)
		}
	}
	return dims
}

// ─── merged from tree_middle_ui.go ───────────────────────────────────────────────

func (ui *uiState) layoutTreeRow(gtx layout.Context, idx int, n modtree.VisibleNode) layout.Dimensions {
	selected := n.FullPath == ui.selectedTreePath
	contextSelected := ui.contextMenuVisible && n.IsFile && n.FullPath == ui.contextMenuPath
	if n.IsFile && n.FullPath == ui.selectedPath {
		selected = true
	}
	expandable := n.IsDir && n.HasKids
	open := ui.expanded[n.FullPath]
	h := gtx.Dp(unit.Dp(19))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	ui.leftRowHeightPx = h
	ui.registerTreeRowClick(gtx, idx, h)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if contextSelected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, cMenuOpen, sz)
				b := widget.Border{Color: cMenuHover, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				return layout.Dimensions{Size: sz}
			}
			if selected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, cSelectBg, sz)
				bd := cSelectBd
				if ui.treeHasKeyboard() {
					// The row the keys act on, which is worth telling apart
					// from a row that is merely selected: the middle tree can
					// hold the keyboard while this row stays selected.
					bd = cFocusBd
				}
				b := widget.Border{Color: bd, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				return layout.Dimensions{Size: sz}
			}
			if ui.rowHover[idx] && !selected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, cMenuHover, sz)
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
					paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
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
							l := material.Label(ui.th, unit.Sp(9), s)
							l.Color = cText
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
					l := material.Label(ui.th, unit.Sp(11), lbl)
					if selected {
						l.Color = cText
						l.Font.Weight = font.Medium
					} else if n.IsFile {
						switch n.Kind {
						case modtree.KindThisMod:
							l.Color = cTreeThis
						case modtree.KindEngine:
							l.Color = cTreeEngine
						default:
							l.Color = cTreeOther
						}
					} else {
						l.Color = cText
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
func (ui *uiState) registerTreeRowClick(gtx layout.Context, idx, h int) {
	if idx < 0 || idx >= len(ui.rowClicks) {
		return
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Push(gtx.Ops)
	ui.rowClicks[idx].Add(gtx.Ops)
	area.Pop()
	pass.Pop()
}

// treeHasKeyboard reports whether the arrow keys, Enter and Delete act on the
// file tree. It is a click away and stays there: a click in the middle tree
// takes the keyboard over, and nothing else does, so the tree keeps answering
// to the keys the way a file list is expected to.
func (ui *uiState) treeHasKeyboard() bool {
	return ui.keyboardArea == "files"
}

func hasSuffix(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	return s[len(s)-len(suf):] == suf
}

func (ui *uiState) middleTreeRow(gtx layout.Context, indent int, txt string, expandable, open bool) layout.Dimensions {
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
					paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
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
							l := material.Label(ui.th, unit.Sp(9), s)
							l.Color = cText
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
					l := material.Label(ui.th, unit.Sp(11), txt)
					l.Color = cText
					return l.Layout(gtx)
				}),
			)
		}),
	)
}

func (ui *uiState) layoutRgdMidRow(gtx layout.Context, t *fileTab, idx int, v rgdVisible) layout.Dimensions {
	selected := v.Selected
	expandable := v.IsTable && v.HasKids
	open := v.Expanded
	h := gtx.Dp(unit.Dp(19))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return t.rgdMidClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if selected {
					sz := image.Pt(gtx.Constraints.Max.X, h)
					fillRect(gtx, cSelectBg, sz)
					b := widget.Border{Color: cSelectBd, Width: unit.Dp(1)}
					gtx.Constraints.Min = sz
					gtx.Constraints.Max = sz
					_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
					return layout.Dimensions{Size: sz}
				}
				if t.rgdMidClicks[idx].Hovered() && !selected {
					sz := image.Pt(gtx.Constraints.Max.X, h)
					fillRect(gtx, cMenuHover, sz)
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
						paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
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
								l := material.Label(ui.th, unit.Sp(9), s)
								l.Color = cText
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
						l := material.Label(ui.th, unit.Sp(11), lbl)
						if selected {
							l.Color = cText
							l.Font.Weight = font.Medium
						} else {
							l.Color = cText
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

func (ui *uiState) middleHeader(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cHeaderBg, sz)
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
						if tab := ui.activeTab(); tab != nil {
							if tab.kind == "fda" {
								title = filepath.Base(tab.path)
								if title == "" {
									title = "FDA"
								}
							} else if tab.kind == "text" {
								title = filepath.Base(tab.path)
							} else if tab.kind == "rgd" {
								if tab.rgdFile != nil {
									title = filepath.Base(tab.path)
									if e := ui.rgdSelectedEntryFor(tab); e != nil && e != tab.rgdFile.Root {
										title = e.Name
										if idx := strings.LastIndex(tab.rgdSelected, "/"); idx >= 0 {
											title = tab.rgdSelected[idx+1:]
										}
									}
								} else {
									title = filepath.Base(tab.path)
								}
							}
						} else if ui.selectedPath != "" {
							title = ui.selectedPath
							if strings.Contains(title, "\\") {
								title = title[strings.LastIndex(title, "\\")+1:]
							}
						}
						l := material.Label(ui.th, unit.Sp(11), title)
						l.Color = cText
						l.Font.Weight = font.Medium
						return l.Layout(gtx)
					}),
				)
			})
		}),
	)
}
