package main

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func centeredPanelLabel(gtx layout.Context, th *material.Theme, text string, size unit.Sp, col color.NRGBA) layout.Dimensions {
	width := gtx.Constraints.Max.X
	if width <= 0 {
		width = gtx.Constraints.Min.X
	}
	if width <= 0 {
		return layout.Dimensions{}
	}
	label := func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, size, text)
		l.Color = col
		return l.Layout(gtx)
	}
	macro := op.Record(gtx.Ops)
	measure := gtx
	measure.Constraints.Min = image.Point{}
	dims := label(measure)
	macro.Stop()
	x := (width - dims.Size.X) / 2
	if x < 0 {
		x = 0
	}
	defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
	draw := gtx
	draw.Constraints.Min = image.Point{}
	label(draw)
	return layout.Dimensions{Size: image.Pt(width, dims.Size.Y)}
}

func centeredPanelWidget(gtx layout.Context, widget layout.Widget) layout.Dimensions {
	width := gtx.Constraints.Max.X
	height := gtx.Constraints.Max.Y
	if width <= 0 {
		width = gtx.Constraints.Min.X
	}
	if height <= 0 {
		height = gtx.Constraints.Min.Y
	}
	if width <= 0 || height <= 0 {
		return widget(gtx)
	}
	macro := op.Record(gtx.Ops)
	measure := gtx
	measure.Constraints.Min = image.Point{}
	dims := widget(measure)
	macro.Stop()
	x := (width - dims.Size.X) / 2
	y := (height - dims.Size.Y) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	draw := gtx
	draw.Constraints.Min = image.Point{}
	widget(draw)
	return layout.Dimensions{Size: image.Pt(width, height)}
}

func (ui *uiState) iconButton(gtx layout.Context, btn *widget.Clickable, glyph string, active bool) layout.Dimensions {
	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := cSaveBg
	bd := cSaveBd
	if active {
		bg = rgb(0x0078D4)
		bd = rgb(0x005A9E)
		if pressed {
			bg = rgb(0x005A9E)
		} else if hover {
			bg = rgb(0x106EBE)
		}
	} else if pressed {
		bg = cSaveBg
	} else if hover {
		bg = cSaveHover
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: bd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(13), glyph)
						if active {
							l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
						} else {
							l.Color = cText
						}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}

func (ui *uiState) miniButton(gtx layout.Context, btn *widget.Clickable, label string, on bool, bgOn color.NRGBA) layout.Dimensions {
	return ui.toggleButton(gtx, btn, label, on, bgOn)
}
func (ui *uiState) miniButtonDisabled(gtx layout.Context, btn *widget.Clickable, label string, disabled bool) layout.Dimensions {
	return ui.smallButtonDisabled(gtx, btn, label, disabled)
}

func (ui *uiState) smallButton(gtx layout.Context, btn *widget.Clickable, label string) layout.Dimensions {
	return ui.smallButtonDisabled(gtx, btn, label, false)
}
func (ui *uiState) smallButtonDisabled(gtx layout.Context, btn *widget.Clickable, label string, disabled bool) layout.Dimensions {
	hover := btn.Hovered() && !disabled
	pressed := btn.Pressed() && !disabled
	bg := cSaveBg
	if disabled {
		bg = cSaveBg
	} else if pressed {
		bg = cMenuOpen
	} else if hover {
		bg = cSaveHover
	}
	if disabled {
		gtx = gtx.Disabled()
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: cSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						if disabled {
							l.Color = cTextDim
						} else {
							l.Color = cText
						}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}
func (ui *uiState) primaryButton(gtx layout.Context, btn *widget.Clickable, label string) layout.Dimensions {
	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := rgb(0x0078D4)
	if pressed {
		bg = rgb(0x005A9E)
	} else if hover {
		bg = rgb(0x106EBE)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: rgb(0x005A9E), Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}
func (ui *uiState) toggleButton(gtx layout.Context, btn *widget.Clickable, label string, on bool, bgOn color.NRGBA) layout.Dimensions {
	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := cSaveBg
	if on {
		bg = bgOn
	}
	if pressed {
		bg = cMenuOpen
	} else if hover {
		bg = cSaveHover
		if on {
			bg = cSelectBg
		}
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: cSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}
