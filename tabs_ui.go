package main

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"

	"gioui.org/f32"
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

// tabBarHeightDp and menuBarHeightDp are the two fixed bands above the tab bar.
// The tab context menu is drawn in the window's own coordinates, so it has to
// know where the bar starts; keeping the two heights named here is what stops
// that from drifting when either band changes.
const (
	tabBarHeightDp   = 28
	menuBarHeightDp  = 24
	tabContextWDP    = 170
	tabContextRowDp  = 26
	tabContextPadDp  = 6
	tabContextInsetP = 8
)

// tabWheelStepPx is how many pixels of the row one unit of the wheel's delta
// scrolls it by. A notch is wheelNotchDelta, so this is a little under two tabs
// a click: a row of tabs is easy to lose track of, and a wheel that jumped a
// dozen of them at once could not be aimed at one.
const tabWheelStepPx = 0.9

// tabBarTop is where the tab bar starts in the window: the menu bar and the one
// pixel line under it.
func tabBarTop(gtx layout.Context) int {
	return gtx.Dp(unit.Dp(menuBarHeightDp)) + 1
}

func (ui *uiState) layoutTabBar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(tabBarHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	ui.ensureTabClicks()

	// The offset is clamped against the width measured on the previous frame.
	// The row cannot be measured earlier in this frame instead: it holds the
	// tab clickables, and both ways of looking at it twice go wrong — laying it
	// out again registers their areas twice, and replaying a recorded copy of
	// the ops moves the frame's input handlers around after they were placed.
	// One frame of lag on the clamp is invisible; a misplaced handler is not.
	ui.tabBarWidth = gtx.Constraints.Max.X
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cTabBarBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			// The row is emitted once, with the scroll already in force, so the
			// tab clickables' handlers are registered once and in the right
			// place. When the bar is scrolled the row is wider than the bar, so
			// it is clipped to the bar and slid left; the bar's own background
			// covers whatever is past the end.
			if ui.tabScrollX != 0 {
				st := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Push(gtx.Ops)
				off := op.Offset(image.Pt(-ui.tabScrollX, 0)).Push(gtx.Ops)
				ui.layoutTabRow(gtx)
				off.Pop()
				st.Pop()
			} else {
				ui.layoutTabRow(gtx)
			}
			// The row's real width is known now, for the next frame's clamp.
			ui.tabRowWidth = ui.tabRowWidthPx
			// The bar's own pointer target goes in after the row, not before
			// it. Gio's hit test walks a frame's input stack from the top and
			// stops descending at the first target it meets, so a target
			// registered before the tabs is never reached with the pointer
			// over one of them: the wheel and the right press would do nothing
			// on a tab. Being last, and inside a PassOp, it sees the press
			// first and the tabs underneath still see it after it.
			ui.registerTabBarPointer(gtx)
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
	)
}

// registerTabBarPointer covers the bar with one pointer target. It is pushed
// inside a PassOp, so the wheel and the right press reach it while the tabs
// underneath keep their own clicks — the same arrangement the file tree uses
// for its scroll area. It must be called after the tabs are laid out; see
// layoutTabBar for why the order matters.
func (ui *uiState) registerTabBarPointer(gtx layout.Context) {
	if ui.tabBarPointerTag == nil {
		return
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(tabBarHeightDp)))}.Push(gtx.Ops)
	event.Op(gtx.Ops, ui.tabBarPointerTag)
	area.Pop()
	pass.Pop()
}

// layoutTabRow is the tabs themselves, at their natural width, which is wider
// than the bar as soon as there are enough of them. It records each tab's
// rectangle and leaves the row's total width in tabRowWidthPx, because the bar
// needs both after the row has been drawn and the bar's own size is not
// something the row can be measured against beforehand.
//
// Each tab is laid out under its own offset, which is what puts it where the
// recorded rectangle says it is. Without it every tab is drawn and hit-tested
// at the row's left edge: they cover one another, only the last one is under
// the pointer, and the row looks like a single tab with pieces of the others
// sticking out from behind it.
func (ui *uiState) layoutTabRow(gtx layout.Context) layout.Dimensions {
	x := gtx.Dp(unit.Dp(6))
	for idx, t := range ui.openTabs {
		idx, t := idx, t
		x += gtx.Dp(unit.Dp(2))
		off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		tabW := ui.tab(gtx, idx+1, tabLabelFor(t), idx).Size.X
		off.Pop()
		if len(ui.tabRects) <= idx {
			ui.tabRects = append(ui.tabRects, image.Rectangle{})
		}
		// The rects are recorded in the bar's own coordinates, which is the
		// space a pointer event on the bar is reported in, so a press can be
		// matched against them without translating anything.
		ui.tabRects[idx] = image.Rectangle{
			Min: image.Pt(x, 0),
			Max: image.Pt(x+tabW, gtx.Dp(unit.Dp(tabBarHeightDp))),
		}
		x += tabW
	}
	ui.tabRects = ui.tabRects[:len(ui.openTabs)]
	ui.tabRowWidthPx = x
	return layout.Dimensions{Size: image.Pt(x, gtx.Dp(unit.Dp(tabBarHeightDp)))}
}

// tabLabelFor is what a tab says. It is apart from the layout because both the
// bar and the tab's own tooltip want it.
func tabLabelFor(t *fileTab) string {
	if t == nil {
		return ""
	}
	if isUnitTab(t) {
		return unitTabLabel
	}
	switch t.kind {
	case "fda":
		return fmt.Sprintf("FDA [%s]", filepath.Base(t.path))
	case "wav":
		return fmt.Sprintf("WAV [%s]", filepath.Base(t.path))
	case "whm":
		return fmt.Sprintf("Model [%s]", filepath.Base(t.path))
	case "image":
		return fmt.Sprintf("Image [%s]", filepath.Base(t.path))
	case "sgb":
		return fmt.Sprintf("Map [%s]", filepath.Base(t.path))
	case "rgd":
		return fmt.Sprintf("RGD [%s]", filepath.Base(t.path))
	}
	return filepath.Base(t.path)
}

// tabMaxScroll is how far the row can be slid: the part of it that does not
// fit. It is zero while the tabs do fit, which is what makes the bar scroll
// only when it has to.
func (ui *uiState) tabMaxScroll(barWidth, rowWidth int) int {
	if d := rowWidth - barWidth; d > 0 {
		return d
	}
	return 0
}

func (ui *uiState) clampTabScroll(barWidth, rowWidth int) {
	ui.tabScrollX = clampInt(ui.tabScrollX, 0, ui.tabMaxScroll(barWidth, rowWidth))
}

// tabAt is which tab a position in the bar lands on, or -1 for the gap beside
// the last one. The comparison is in floats because a pointer event reports
// fractional pixels, and rounding first would put the rightmost pixel column of
// a tab on the tab beside it.
func (ui *uiState) tabAt(p f32.Point) int {
	for i, r := range ui.tabRects {
		if p.X >= float32(r.Min.X) && p.X < float32(r.Max.X) &&
			p.Y >= float32(r.Min.Y) && p.Y < float32(r.Max.Y) {
			return i
		}
	}
	return -1
}

func (ui *uiState) tab(gtx layout.Context, idx int, label string, closeIdx int) layout.Dimensions {
	sel := ui.tabSel == idx
	bg := cTabOff
	closable := closeIdx >= 0
	if sel {
		bg = cTabOn
	} else if ui.tabClicks[idx].Hovered() {
		bg = cMenuHover
	}
	return ui.tabClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		inner := func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(2), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(12), label)
						if sel {
							l.Color = cText
							l.Font.Weight = font.Medium
						} else {
							l.Color = cTextDim
						}
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !closable {
						return layout.Dimensions{}
					}
					return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.tabCloses[closeIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							hover := ui.tabCloses[closeIdx].Hovered()
							sz := gtx.Dp(unit.Dp(16))
							if hover {
								rrect(gtx, rgb(0xE81123), image.Pt(sz, sz), gtx.Dp(2))
							}
							l := material.Label(ui.th, unit.Sp(12), "×")
							if hover {
								l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
							} else {
								l.Color = cTextDim
							}
							return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, l.Layout)
						})
					})
				}),
			)
		}
		macro := op.Record(gtx.Ops)
		dims := inner(gtx)
		call := macro.Stop()
		sz := dims.Size
		rr := clip.RRect{Rect: image.Rectangle{Max: sz}, NE: gtx.Dp(4), NW: gtx.Dp(4)}.Push(gtx.Ops)
		paint.ColorOp{Color: bg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rr.Pop()
		st := clip.Rect(image.Rect(0, 0, sz.X, 1)).Push(gtx.Ops)
		paint.ColorOp{Color: cBorder}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		st = clip.Rect(image.Rect(0, 0, 1, sz.Y)).Push(gtx.Ops)
		paint.ColorOp{Color: cBorder}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		st = clip.Rect(image.Rect(sz.X-1, 0, sz.X, sz.Y)).Push(gtx.Ops)
		paint.ColorOp{Color: cBorder}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		if sel {
			st = clip.Rect(image.Rect(0, sz.Y-1, sz.X, sz.Y)).Push(gtx.Ops)
			paint.ColorOp{Color: bg}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
		}
		call.Add(gtx.Ops)
		return dims
	})
}

// ── tab context menu ───────────────────────────────────────────────────────

// openTabContextMenu puts the menu on a tab, at the point it was clicked. The
// tab is selected as well, so the menu belongs to the tab under the pointer
// even when it was not the active one.
func (ui *uiState) openTabContextMenu(idx int, at image.Point) {
	if idx < 0 || idx >= len(ui.openTabs) {
		return
	}
	ui.tabContextIndex = idx
	ui.tabContextPos = at
	ui.tabContextVisible = true
	// Selecting is not what the menu acts on yet, but a context menu on an
	// unselected tab is a trap: the reader has no way to tell which one it is
	// for, and the next item to be added will act on the selection.
	ui.tabSel = idx + 1
	ui.markStateDirty()
}

// closeTabContextMenu puts the menu away.
func (ui *uiState) closeTabContextMenu() {
	ui.tabContextVisible = false
	ui.tabContextIndex = -1
}

// tabContextItems are the menu's rows, in order. One for now; the list is here
// so the next one is a line rather than a second layout pass.
var tabContextItems = []string{"Close all tabs"}

// tabContextMenuOrigin is where the menu goes for a click at p in a window of
// winW by winH. It hangs below the click at the click's own x, then is turned
// back inside the window: to the left when there is no room to the right, and
// up when there is no room below. A menu that opened off screen would take its
// only item with it, which is the one thing it must never do.
//
// It is apart from the layout so the placement can be pinned for every corner —
// the layout only ever sees one of them at a time.
func tabContextMenuOrigin(p image.Point, winW, winH, menuW, menuH, inset int) image.Point {
	x, y := p.X, p.Y
	if x+menuW > winW-inset {
		// Flip to the left of the click rather than off the right edge.
		x = p.X - menuW
	}
	if x < inset {
		// Not enough room on either side, so sit against the left edge.
		x = inset
	}
	if y+menuH > winH-inset {
		y = winH - menuH - inset
	}
	if y < 0 {
		y = 0
	}
	return image.Pt(x, y)
}

// layoutTabContextMenu is the menu itself, drawn over the window. It is
// positioned in window coordinates — the bar's own top plus where the click
// landed — and nudged back inside the window, because a click near the right or
// bottom edge would otherwise open it off screen.
func (ui *uiState) layoutTabContextMenu(gtx layout.Context) layout.Dimensions {
	if !ui.tabContextVisible {
		return layout.Dimensions{}
	}
	menuW := gtx.Dp(unit.Dp(tabContextWDP))
	pad := gtx.Dp(unit.Dp(tabContextPadDp))
	rowH := gtx.Dp(unit.Dp(tabContextRowDp))
	menuH := pad*2 + rowH*len(tabContextItems)
	inset := gtx.Dp(unit.Dp(tabContextInsetP))
	origin := tabContextMenuOrigin(ui.tabContextPos, gtx.Constraints.Max.X, gtx.Constraints.Max.Y, menuW, menuH, inset)

	gtx2 := gtx
	defer op.Offset(origin).Push(gtx2.Ops).Pop()
	gtx2.Constraints.Min = image.Pt(menuW, menuH)
	gtx2.Constraints.Max = image.Pt(menuW, menuH)
	// A shadow, so the menu reads as sitting above the tab bar rather than
	// being printed on it.
	shadow := gtx.Dp(unit.Dp(2))
	st := clip.Rect{Max: image.Pt(menuW+shadow, menuH+shadow)}.Push(gtx2.Ops)
	paint.ColorOp{Color: rgba(0, 0, 0, 28)}.Add(gtx2.Ops)
	paint.PaintOp{}.Add(gtx2.Ops)
	st.Pop()
	return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(3))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{
					Top: unit.Dp(tabContextPadDp), Bottom: unit.Dp(tabContextPadDp),
					Left: unit.Dp(tabContextInsetP), Right: unit.Dp(tabContextInsetP),
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.tabContextCloseAll, tabContextItems[0])
						}),
					)
				})
			}),
		)
	})
}
