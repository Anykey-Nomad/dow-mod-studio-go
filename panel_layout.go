package main

import (
	"image"
	"math"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

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
