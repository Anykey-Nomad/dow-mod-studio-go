package app

import (
	"dow-mod-studio/internal/app/uikit"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"image"
)

// LayoutUnitPointerTrack follows the pointer across the object window's body,
// which is where the hover tip gets its position from. The area is
// pass-through: the rows and splitters underneath keep their presses and their
// own hover, the same contract the file tree's right-press area relies on.
// Positions are local to this area, and every tip is drawn in that space.
func LayoutUnitPointerTrack(ui *uiState, gtx layout.Context) layout.Dimensions {
	if ui.UnitPointerTag == nil {
		ui.UnitPointerTag = uikit.NewPointerTag("unit-hover-tip")
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, ui.UnitPointerTag)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: ui.UnitPointerTag,
			Kinds:  pointer.Move | pointer.Enter | pointer.Drag,
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		ui.HoverTipX, ui.HoverTipY = e.Position.X, e.Position.Y
		ui.HoverTipMoved = true
	}
	area.Pop()
	pass.Pop()
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// LayoutUnitHoverTip draws the full name of the row under the pointer just
// above it, clamped to the body. The classic editor's panel column is a fifth
// of the window at most, so it cuts clip, action and branch names short —
// the cut is exactly where a name stops saying which clip it is, and hovering
// is how the rest of it is read.
func LayoutUnitHoverTip(ui *uiState, gtx layout.Context) layout.Dimensions {
	text := ui.HoverTipText
	if text == "" || gtx.Constraints.Max.X <= 0 || gtx.Constraints.Max.Y <= 0 {
		return layout.Dimensions{}
	}
	// The width follows the text instead of a measurement pass: the tip is
	// one line of a proportional font, and a box that is a little wider than
	// the text reads better than one twice as wide as it needs to be.
	boxW := gtx.Dp(unit.Dp(float32(len([]rune(text))*5 + 18)))
	if maxW := gtx.Constraints.Max.X - gtx.Dp(unit.Dp(8)); boxW > maxW {
		boxW = maxW
	}
	if boxW < gtx.Dp(unit.Dp(40)) {
		boxW = gtx.Dp(unit.Dp(40))
	}
	boxH := gtx.Dp(unit.Dp(22))
	x := int(ui.HoverTipX) - boxW/2
	if minX := gtx.Dp(unit.Dp(4)); x < minX {
		x = minX
	}
	if maxX := gtx.Constraints.Max.X - boxW - gtx.Dp(unit.Dp(4)); x > maxX {
		x = maxX
	}
	y := int(ui.HoverTipY) - boxH - gtx.Dp(unit.Dp(6))
	if y < gtx.Dp(unit.Dp(4)) {
		// No room above the pointer — at the top of the list — so the tip
		// goes just under it instead of off the edge.
		y = int(ui.HoverTipY) + gtx.Dp(unit.Dp(18))
	}
	child := gtx
	child.Constraints = layout.Exact(image.Pt(boxW, boxH))
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	return frameMeasured(child, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), text)
			l.Color = uikit.CText
			l.MaxLines = 1
			return l.Layout(gtx)
		})
	}, uikit.CMenuBg, uikit.CBorder, unit.Dp(3))
}
