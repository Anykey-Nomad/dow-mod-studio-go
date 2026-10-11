package main

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (ui *uiState) tableHead(gtx layout.Context, a, b string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(20))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), a)
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, cHeaderLine) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), b)
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

func vLine(gtx layout.Context, col color.NRGBA) layout.Dimensions {
	w := gtx.Dp(unit.Dp(1))
	h := gtx.Dp(unit.Dp(20))
	sz := image.Pt(w, h)
	gtx.Constraints.Min.X = w
	gtx.Constraints.Max.X = w
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func (ui *uiState) tableRow2(gtx layout.Context, a, b string, alt bool) layout.Dimensions {
	return ui.tableRow2WithTooltip(gtx, a, b, alt, "")
}

func (ui *uiState) tableRow2WithTooltip(gtx layout.Context, a, b string, alt bool, tooltip string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(24))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := cPanelBg
	if alt {
		bg = cSaveBg
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, bg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if tooltip == "" {
				return layout.Dimensions{}
			}
			if ui.rgdTooltipTags == nil {
				ui.rgdTooltipTags = make(map[string]event.Tag)
			}
			tag, ok := ui.rgdTooltipTags[tooltip]
			if !ok {
				tag = newPointerTag(tooltip)
				ui.rgdTooltipTags[tooltip] = tag
			}
			event.Op(gtx.Ops, tag)
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Enter | pointer.Leave})
				if !ok {
					break
				}
				e, ok := ev.(pointer.Event)
				if !ok {
					continue
				}
				switch e.Kind {
				case pointer.Enter:
					ui.rgdTooltipText = tooltip
				case pointer.Leave:
					ui.rgdTooltipText = ""
				}
			}
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), a)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, cBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), b)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}
