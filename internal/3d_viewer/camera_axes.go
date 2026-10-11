package viewer

import (
	"fmt"
	"image"
	"image/color"
	"math"

	engine "dow-mod-studio/internal/3d_engine"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

// The axis gizmo, drawn the way 3ds Max and Blender draw theirs: a small triad
// of the three world axes from a common origin, each arrow ending in its own
// letter, with the numbers that say how the camera is turned written beside
// them.
//
// It is drawn in the widget layer rather than into the frame, because it is
// mostly text. The first attempt drew the letters and the degrees as bars in
// the frame's own transform — no font is reachable from there, and a widget
// cannot be laid out into that transform — and the result was a coloured blob
// nobody could read, which is worse than nothing. Here the arrows are still
// bars, because a line is a line, and everything that has to be read is real
// text.

// The gizmo's box, in pixels. The triad sits in the top part and the readout
// under it, so the whole thing reads as one widget.
const (
	gizmoBox      = 104.0
	gizmoArrowLen = 30.0
	gizmoCentre   = gizmoBox / 2
	gizmoBarThick = 2.0
)

// CameraAxesLayout lays out the orientation gizmo independently of the viewer.
func CameraAxesLayout(th *material.Theme, camera engine.Camera, axisColors [3][4]float64, forwardColor [4]float64, draw bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		gtx.Constraints.Max = image.Point{X: int(gizmoBox), Y: int(gizmoBox) + 46}

		view := engine.ViewMatrix(camera)
		centre := camera.Target
		angles := engine.AxisAngles(camera)

		// Where each axis points on screen, from the difference of two
		// projections: a direction has no length to project to, and an axis
		// pointing at the camera must collapse rather than pick a direction.
		tips := [3]fauxgl.Vector{}
		live := [3]bool{}
		axes := [3]fauxgl.Vector{{X: 1}, {Y: 1}, {Z: 1}}
		for i, a := range axes {
			from := engine.Project(view, centre, camera.Width, camera.Height)
			to := engine.Project(view, centre.Add(a.MulScalar(1)), camera.Width, camera.Height)
			if from == nil || to == nil {
				continue
			}
			d := to.Sub(*from)
			n := d.Length()
			if n < 1e-6 {
				continue
			}
			d = d.MulScalar(1 / n)
			tips[i] = fauxgl.Vector{X: gizmoCentre + d.X*gizmoArrowLen, Y: gizmoCentre - d.Y*gizmoArrowLen}
			live[i] = true
			if draw {
				paintGizmoBar(gtx, gizmoCentre, gizmoCentre, tips[i].X, tips[i].Y, axisColors[i])
			}
		}
		// The origin the three axes meet at, which is what the whole triad is
		// read against and what Blender and 3ds Max both draw.
		if draw {
			paintGizmoDot(gtx, gizmoCentre, gizmoCentre, forwardColor)
		}

		// The camera's own line of sight, in the forward colour, so the three
		// numbers underneath have something they are measured against.
		fwd := engine.ViewDirection(camera)
		if draw {
			paintGizmoBar(gtx, gizmoCentre, gizmoCentre,
				gizmoCentre+fwd.X*gizmoArrowLen, gizmoCentre-fwd.Y*gizmoArrowLen,
				forwardColor)
		}

		// The letter at each tip, nudged back towards the middle so it sits
		// just past the arrow rather than on it.
		letters := [3]string{"X", "Y", "Z"}
		var items []placedLabel
		for i := range axes {
			if !live[i] {
				continue
			}
			dx := tips[i].X + gizmoCentre - tips[i].X
			dy := tips[i].Y - gizmoCentre
			l := math.Hypot(dx, dy)
			var lx, ly float64
			if l > 1e-6 {
				// Push the letter out along the arrow by half a glyph.
				lx = tips[i].X + dx/l*6
				ly = tips[i].Y + dy/l*6
			}
			items = append(items, placedLabel{
				dx: lx, dy: ly,
				label: gizmoLabel(th, letters[i], 13, axisColors[i]),
			})
		}

		// The readout: one line per axis, letter and degrees, in the axis's
		// colour. Three digits zero padded would be unreadable here and
		// wrong anyway — the angles have a decimal in them.
		for i := range axes {
			items = append(items, placedLabel{
				dx: 0,
				dy: gizmoBox + float64(i)*15,
				label: gizmoLabel(th,
					fmt.Sprintf("%s %6.1f°", letters[i], angles[i]),
					11, axisColors[i]),
			})
		}
		if draw {
			placeLabels(gtx, items)
		}

		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
}

// placedLabel is one bit of text at an offset inside the gizmo's box. Gio has
// no absolute layout of its own, so the gizmo measures each label and then
// lays it out again under an offset.
type placedLabel struct {
	dx, dy float64
	label  material.LabelStyle
}

// placeLabels draws each label at its offset, measuring it first so the box it
// draws into is its own size rather than the gizmo's.
func placeLabels(gtx layout.Context, items []placedLabel) {
	for _, it := range items {
		// Measure: the label sizes itself to its text.
		m := gtx
		m.Constraints.Min = image.Point{}
		m.Constraints.Max = image.Point{X: 1 << 20, Y: 1 << 20}
		d := it.label.Layout(m)
		if d.Size.X == 0 || d.Size.Y == 0 {
			continue
		}
		// Place: the same label again, this time only as big as itself and
		// pushed to where the triad wanted it.
		p := gtx
		p.Constraints.Min = image.Point{}
		p.Constraints.Max = d.Size
		macro := op.Offset(image.Point{X: roundToInt(it.dx), Y: roundToInt(it.dy)}).Push(p.Ops)
		it.label.Layout(p)
		macro.Pop()
	}
}

// gizmoLabel makes one label in the gizmo's style.
func gizmoLabel(th *material.Theme, s string, size unit.Sp, col [4]float64) material.LabelStyle {
	l := material.Label(th, size, s)
	l.Color = axisPaintColor(col)
	l.MaxLines = 1
	return l
}

// paintGizmoBar draws a line of the triad, thickening it whichever way it runs
// so a diagonal arrow is not a hairline.
func paintGizmoBar(gtx layout.Context, ax, ay, bx, by float64, col [4]float64) {
	thick := int(gizmoBarThick)
	r := image.Rect(int(math.Min(ax, bx)), int(math.Min(ay, by)),
		int(math.Max(ax, bx))+1, int(math.Max(ay, by))+1)
	if r.Dx() < thick {
		r.Min.X -= (thick - r.Dx()) / 2
		r.Max.X = r.Min.X + thick
	}
	if r.Dy() < thick {
		r.Min.Y -= (thick - r.Dy()) / 2
		r.Max.Y = r.Min.Y + thick
	}
	area := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: axisPaintColor(col)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	area.Pop()
}

// paintGizmoDot draws the origin the three axes meet at.
func paintGizmoDot(gtx layout.Context, x, y float64, col [4]float64) {
	r := image.Rect(roundToInt(x)-2, roundToInt(y)-2, roundToInt(x)+3, roundToInt(y)+3)
	area := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: axisPaintColor(col)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	area.Pop()
}

func roundToInt(v float64) int { return int(math.Round(v)) }

// axisPaintColor turns one of the 2D overlay's 0..1 colours into the byte form
// paint.ColorOp wants. The GPU overlay keeps its colours as floats so the
// shader reads them as written; the 2D side has to be bytes.
func axisPaintColor(c [4]float64) color.NRGBA {
	to8 := func(v float64) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(v*255 + 0.5)
	}
	return color.NRGBA{R: to8(c[0]), G: to8(c[1]), B: to8(c[2]), A: to8(c[3])}
}
