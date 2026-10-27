// Package uikit holds the widgets and small helpers the editor draws its
// panels, rows and buttons with. Every feature package below internal/app
// shares them, so they live at the bottom, not in package app.
package uikit

import (
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modifiers"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/unitdef"
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
	"image"
	"image/color"
	"math"
)

// uiState keeps the moved signatures identical to the ones app uses.
type uiState = uistate.State

// fileTab keeps the moved signatures identical to the ones app uses.
type fileTab = uistate.Tab

// panelScrollState keeps the moved signatures identical to the ones app uses.
type panelScrollState = uistate.PanelScrollState

// rgdVisible keeps the moved signatures identical to the ones app uses.
type rgdVisible = uistate.RgdVisible

// rgdEditorClickState keeps the moved signatures identical to the ones app uses.
type rgdEditorClickState = uistate.RgdEditorClickState

func TableRow2(ui *uiState, gtx layout.Context, a, b string, alt bool) layout.Dimensions {
	return TableRow2WithTooltip(ui, gtx, a, b, alt, "")
}

func TableRow2WithTooltip(ui *uiState, gtx layout.Context, a, b string, alt bool, tooltip string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(24))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := CPanelBg
	if alt {
		bg = CSaveBg
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			FillRect(gtx, bg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if tooltip == "" {
				return layout.Dimensions{}
			}
			if ui.RgdTooltipTags == nil {
				ui.RgdTooltipTags = make(map[string]event.Tag)
			}
			tag, ok := ui.RgdTooltipTags[tooltip]
			if !ok {
				tag = NewPointerTag(tooltip)
				ui.RgdTooltipTags[tooltip] = tag
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
					ui.RgdTooltipText = tooltip
				case pointer.Leave:
					ui.RgdTooltipText = ""
				}
			}
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), a)
						l.Color = CText
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return VLine(gtx, CBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), b)
						l.Color = CText
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

func SmallButton(ui *uiState, gtx layout.Context, btn *widget.Clickable, label string) layout.Dimensions {
	return SmallButtonDisabled(ui, gtx, btn, label, false)
}

func SmallButtonDisabled(ui *uiState, gtx layout.Context, btn *widget.Clickable, label string, disabled bool) layout.Dimensions {
	hover := btn.Hovered() && !disabled
	pressed := btn.Pressed() && !disabled
	bg := CSaveBg
	if disabled {
		bg = CSaveBg
	} else if pressed {
		bg = CMenuOpen
	} else if hover {
		bg = CSaveHover
	}
	if disabled {
		gtx = gtx.Disabled()
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: CSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
					Rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), label)
						if disabled {
							l.Color = CTextDim
						} else {
							l.Color = CText
						}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}

func TableHead(ui *uiState, gtx layout.Context, a, b string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(20))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			FillRect(gtx, CHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), a)
						l.Color = CTextDim
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return VLine(gtx, CHeaderLine) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), b)
						l.Color = CTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

func CenteredPanelLabel(gtx layout.Context, th *material.Theme, text string, size unit.Sp, col color.NRGBA) layout.Dimensions {
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

func Rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xFF}
}

func FillRect(gtx layout.Context, col color.NRGBA, sz image.Point) {
	stack := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func HLine(gtx layout.Context, col color.NRGBA, h int) layout.Dimensions {
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	sz := image.Pt(gtx.Constraints.Max.X, h)
	stack := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
	return layout.Dimensions{Size: sz}
}

// MinInt and MaxInt are general helpers, not image code: files across the app
// reach for them, so they stayed behind with the call sites rather than move
// into the decoder package.
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ItoaPlain is a plain decimal integer as text.
func ItoaPlain(n int) string { return unitdef.ItoaPlain(n) }

// WheelNotchDelta is the magnitude of one click of a mouse wheel, which is
// what a scroll amount is counted in. Windows reports WHEEL_DELTA (120) for
// each notch and Gio passes it through with the sign flipped, so a wheel event
// carrying ±120 is one click and anything smaller is a trackpad's finer report.
//
// It is named here because the amount is not a pixel count, and treating it as
// one is what makes a wheel that is meant to move a list by a few lines move it
// by five thousand instead.
const WheelNotchDelta = 120

// HeldModifiers reads the live state of the modifier keys straight from the
// OS, for platforms where the window backend does not report it in events.
// The implementation lives in internal/modifiers.
func HeldModifiers() key.Modifiers { return modifiers.HeldModifiers() }

// FrameMeasured draws content inside a rounded, bordered frame that hugs it.
// The content is laid out into a recorded op stream first to learn its size,
// the frame is painted at that size, and the recording is replayed on top.
func FrameMeasured(gtx layout.Context, content layout.Widget, bg, border color.NRGBA, radius unit.Dp) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := content(gtx)
	call := macro.Stop()
	sz := dims.Size
	Rrect(gtx, bg, sz, gtx.Dp(radius))
	frame := gtx
	frame.Constraints.Min = sz
	frame.Constraints.Max = sz
	widget.Border{Color: border, Width: unit.Dp(1), CornerRadius: radius}.Layout(frame, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	})
	clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: sz}
}

// RowGap is the same spacer for a row of controls, with one difference: it
// takes the width left over on the line and none of the height. A row that
// uses flexGap is as tall as the space it was given, which for a row under a
// canvas means taking the canvas's height and leaving it nothing.
func RowGap() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
	}
}

func FixedPanel(ui *uiState, width int, content layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = width
		gtx.Constraints.Max.X = width
		return content(gtx)
	}
}

func PanelScrollView(ui *uiState, gtx layout.Context, state *panelScrollState, content layout.Widget) layout.Dimensions {
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
	measureGTX.Constraints.Max.Y = PanelScrollMeasureHeight
	measured := content(measureGTX)
	if measured.Size.Y >= PanelScrollMeasureHeight {
		// Something in there is elastic, and left to itself it swallows the
		// room. Measure it the ordinary way instead of believing a height
		// that is really just the number that was offered.
		measureGTX.Constraints.Max.Y = viewport.Y
		measured = content(measureGTX)
	}
	macro.Stop()
	state.ContentHeight = measured.Size.Y
	state.Viewport = viewport.Y
	state.HasBar = state.ContentHeight > state.Viewport
	barW := 0
	if state.HasBar {
		barW = gtx.Dp(unit.Dp(PanelScrollbarWidthDp))
	}
	state.Offset = ClampInt(state.Offset, 0, MaxInt(0, state.ContentHeight-state.Viewport))

	area := clip.Rect{Max: viewport}.Push(gtx.Ops)
	// Wheel events cover the whole panel, but are pass-through so buttons and
	// editors inside the panel still receive their own pointer events.
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &state.ScrollTag)
	pass.Pop()
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &state.ScrollTag,
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
		if e.Kind == pointer.Scroll && state.HasBar {
			// Gio's wheel is positive downwards, which is the direction the
			// offset grows in, and Windows negates the raw wheel delta on the
			// way in, so this is the whole of the sign there is to it.
			state.Offset += int(e.Scroll.Y * PanelScrollWheelStepPx)
		}
	}
	// Only the actual scrollbar strip accepts press/drag events. Registering a
	// full-panel handler here would cover the tree rows and prevent selection.
	if state.HasBar {
		barRect := clip.Rect{Min: image.Pt(viewport.X-barW, 0), Max: viewport}.Push(gtx.Ops)
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
					thumb := PanelScrollThumbSize(state, viewport.Y)
					track := viewport.Y - thumb
					if track > 0 {
						delta := e.Position.Y - state.StartY
						state.Offset = state.StartOffset + int(delta/float32(track)*float32(MaxInt(0, state.ContentHeight-state.Viewport)))
					}
				}
			case pointer.Release, pointer.Cancel:
				state.Dragging = false
			}
		}
		barRect.Pop()
	}
	state.Offset = ClampInt(state.Offset, 0, MaxInt(0, state.ContentHeight-state.Viewport))

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
	renderH := state.ContentHeight
	if renderH < viewport.Y {
		renderH = viewport.Y
	}
	renderGTX.Constraints.Max.Y = renderH
	if state.Offset > 0 {
		shift := op.Offset(image.Pt(0, -state.Offset)).Push(gtx.Ops)
		content(renderGTX)
		shift.Pop()
	} else {
		content(renderGTX)
	}
	area.Pop()
	if state.HasBar {
		DrawPanelScrollbar(gtx, state, viewport, barW)
	}
	return layout.Dimensions{Size: viewport}
}

func PanelFrame(gtx layout.Context, content layout.Widget) layout.Dimensions {
	b := widget.Border{Color: CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				Rrect(gtx, CPanelBg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(content),
		)
	})
}

// PanelScrollMeasureHeight is how much room a panel's content is offered when
// it is measured for scrolling. Large enough that no panel ever fills it, small
// enough that a widget scaling a length by it stays inside a float32.
const PanelScrollMeasureHeight = 1 << 20

func EnsureTabClicks(ui *uiState) {
	need := 1 + len(ui.OpenTabs)
	if len(ui.TabClicks) < need {
		old := ui.TabClicks
		n := make([]widget.Clickable, need)
		copy(n, old)
		ui.TabClicks = n
	} else if len(ui.TabClicks) > need {
		// shrink handled explicitly in closeTabAt; if reached here via unexpected path, truncate preserving Module+prefix
		ui.TabClicks = ui.TabClicks[:need]
	}
	if len(ui.TabCloses) < len(ui.OpenTabs) {
		old := ui.TabCloses
		n := make([]widget.Clickable, len(ui.OpenTabs))
		copy(n, old)
		ui.TabCloses = n
	} else if len(ui.TabCloses) > len(ui.OpenTabs) {
		ui.TabCloses = ui.TabCloses[:len(ui.OpenTabs)]
	}
	if ui.TabSel < 0 {
		ui.TabSel = 0
	}
	if ui.TabSel > len(ui.OpenTabs) {
		ui.TabSel = len(ui.OpenTabs)
	}
}

func FindTabIndex(ui *uiState, path string) int {
	for i, t := range ui.OpenTabs {
		if t.Path == path {
			return i
		}
	}
	return -1
}

func Rrect(gtx layout.Context, col color.NRGBA, sz image.Point, r int) {
	stack := clip.RRect{Rect: image.Rectangle{Max: sz}, SE: r, SW: r, NE: r, NW: r}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func VLine(gtx layout.Context, col color.NRGBA) layout.Dimensions {
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

func ClampInt(v, lo, hi int) int {
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

func DrawPanelScrollbar(gtx layout.Context, state *panelScrollState, viewport image.Point, width int) {
	if width <= 0 || state == nil {
		return
	}
	x := viewport.X - width
	track := clip.Rect{Min: image.Pt(x, 0), Max: viewport}.Push(gtx.Ops)
	FillRect(gtx, CContentBg, image.Pt(width, viewport.Y))
	thumb := PanelScrollThumbSize(state, viewport.Y)
	maxOffset := MaxInt(1, state.ContentHeight-state.Viewport)
	y := state.Offset * (viewport.Y - thumb) / maxOffset
	col := CBorder
	if state.HoverBar || state.Dragging {
		col = Rgb(0x0078D4)
	}
	// Offset the thumb into the track.
	// The tracking transform is kept separate so the rounded rectangle is
	// positioned in panel coordinates.
	track.Pop()
	// Draw at the correct vertical position using a clipped transform.
	shift := op.Offset(image.Pt(x+1, y)).Push(gtx.Ops)
	Rrect(gtx, col, image.Pt(width-2, thumb), gtx.Dp(unit.Dp(3)))
	shift.Pop()
}

func PanelScrollThumbSize(state *panelScrollState, viewport int) int {
	if state == nil || state.ContentHeight <= viewport || viewport <= 0 {
		return viewport
	}
	thumb := viewport * viewport / state.ContentHeight
	if thumb < 24 {
		thumb = 24
	}
	if thumb > viewport {
		thumb = viewport
	}
	return thumb
}

// PanelScrollWheelStepPx is how many pixels of the panel one unit of the
// wheel's delta moves it by. One notch is WheelNotchDelta, so a click moves
// about three list rows, which is what a wheel is for. It used to be forty
// eight, read as pixels per notch, which against a delta of 120 asked for five
// thousand pixels a click: every notch slammed the list to one end or the
// other, and the rows in between could not be reached at all.
const PanelScrollWheelStepPx = 0.55

func GetVisibleNodes(ui *uiState) []modtree.VisibleNode {
	if ui.ModTree == nil {
		return nil
	}
	return ui.ModTree.Flatten(ui.Expanded, ui.SelectedPath)
}

var (
	// Dark is the default application theme. Keep these in one palette so
	// panels, controls, and text remain visually consistent.
	CBg          = Rgb(0x1E1E1E)
	CMenuBg      = Rgb(0x252526)
	CMenuHover   = Rgb(0x343B45)
	CMenuOpen    = Rgb(0x234A63)
	CBorder      = Rgb(0x555555)
	CBorderLight = Rgb(0x414141)
	CToolbarBg   = Rgb(0x2D2D30)
	CTabBarBg    = Rgb(0x333333)
	CTabOff      = Rgb(0x292929)
	CTabOn       = Rgb(0x1E1E1E)
	CContentBg   = Rgb(0x181818)
	CPanelBg     = Rgb(0x252526)
	CHeaderBg    = Rgb(0x2D2D30)
	CHeaderLine  = Rgb(0x4A4A4A)
	CSelectBg    = Rgb(0x264A63)
	// CMarkerHi outlines the marker picked in the markers list. Red, because
	// nothing else in the panel is drawn in it and the point is to find one
	// small thing on a model quickly.
	CMarkerHi = Rgb(0xFF3020)
	CSelectBd = Rgb(0x3C78A8)
	// CFocusBd marks the row the keyboard is on, which is not the same thing as
	// the row that is selected: a panel can keep the selection while the middle
	// tree has the keyboard.
	CFocusBd   = Rgb(0x8FC7FF)
	CText      = Rgb(0xE6E6E6)
	CTextDim   = Rgb(0xA0A0A0)
	CSaveBg    = Rgb(0x303236)
	CSaveBd    = Rgb(0x555555)
	CSaveHover = Rgb(0x3A4652)
	// CProblem marks a reference that names a file which is not there, which
	// is the one thing in the object window that is certainly wrong.
	CProblem = Rgb(0xE06C75)

	// tree source colors (like Corsix frmFiles: ThisMod, OtherMod, Engine)
	CTreeThis   = Rgb(0xE06C75) // reddish — this mod
	CTreeOther  = Rgb(0x82AAFF) // bluish — foreign / RequiredMod
	CTreeEngine = Rgb(0xB0B0B0) // grey — engine
)

func SetColorTheme(dark bool) {
	if dark {
		CBg, CMenuBg, CMenuHover, CMenuOpen = Rgb(0x1E1E1E), Rgb(0x252526), Rgb(0x343B45), Rgb(0x234A63)
		CBorder, CBorderLight, CToolbarBg, CTabBarBg = Rgb(0x555555), Rgb(0x414141), Rgb(0x2D2D30), Rgb(0x333333)
		CTabOff, CTabOn, CContentBg, CPanelBg = Rgb(0x292929), Rgb(0x1E1E1E), Rgb(0x181818), Rgb(0x252526)
		CHeaderBg, CHeaderLine, CSelectBg, CSelectBd = Rgb(0x2D2D30), Rgb(0x4A4A4A), Rgb(0x264A63), Rgb(0x3C78A8)
		CFocusBd = Rgb(0x8FC7FF)
		CText, CTextDim, CSaveBg, CSaveBd, CSaveHover = Rgb(0xE6E6E6), Rgb(0xA0A0A0), Rgb(0x303236), Rgb(0x555555), Rgb(0x3A4652)
		CTreeThis, CTreeOther, CTreeEngine = Rgb(0xE06C75), Rgb(0x82AAFF), Rgb(0xB0B0B0)
		return
	}
	CBg, CMenuBg, CMenuHover, CMenuOpen = Rgb(0xECECEC), Rgb(0xF0F0F0), Rgb(0xE5F3FF), Rgb(0xCCE8FF)
	CBorder, CBorderLight, CToolbarBg, CTabBarBg = Rgb(0xA0A0A0), Rgb(0xC8C8C8), Rgb(0xE8E8E8), Rgb(0xD4D0C8)
	CTabOff, CTabOn, CContentBg, CPanelBg = Rgb(0xE0E0E0), Rgb(0xFFFFFF), Rgb(0xD4D0C8), Rgb(0xFFFFFF)
	CHeaderBg, CHeaderLine, CSelectBg, CSelectBd = Rgb(0xE8E8E8), Rgb(0xB0B0B0), Rgb(0xCCE8FF), Rgb(0x99CCFF)
	CFocusBd = Rgb(0x1A5FB4)
	CText, CTextDim, CSaveBg, CSaveBd, CSaveHover = Rgb(0x000000), Rgb(0x505050), Rgb(0xE1E1E1), Rgb(0xADADAD), Rgb(0xE5F3FF)
	CTreeThis, CTreeOther, CTreeEngine = Rgb(0x802020), Rgb(0x202080), Rgb(0x808080)
}

const (
	PanelSplitterWidthDp  = 6
	PanelScrollbarWidthDp = 10
	PanelSplitterMinDp    = 140
	PanelSplitterMinMidDp = 220
)

func NewPointerTag(name string) event.Tag {
	return &struct{ name string }{name: name}
}

// UnitRowHeightDp is one row of a list or a button strip, in device-independent
// pixels. Panels that lay out rows of equal height share it so a row reads the
// same wherever it appears.
const UnitRowHeightDp = 26
