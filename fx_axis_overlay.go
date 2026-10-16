package main

import (
	"image"
	"image/color"
	"math"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

const (
	fxAxesPairGap  = 52.0
	fxAxesTriadLen = 18.0
	fxAxesProbe    = 1.0
)

type fxOriginAxesEntry struct {
	origin fauxgl.Vector
	frames [2][3]fauxgl.Vector
}

func fxOriginAxesKey(origin fauxgl.Vector, frames [2][3]fauxgl.Vector) [21]uint64 {
	key := [21]uint64{math.Float64bits(origin.X), math.Float64bits(origin.Y), math.Float64bits(origin.Z)}
	at := 3
	for _, frame := range frames {
		for _, axis := range frame {
			key[at], key[at+1], key[at+2] = math.Float64bits(axis.X), math.Float64bits(axis.Y), math.Float64bits(axis.Z)
			at += 3
		}
	}
	return key
}

func fxOriginAxisFrames(e fxOverlayEffect, model *WhmModel, marker int, pose WhmPose) [2][3]fauxgl.Vector {
	local := fxFallbackFrame
	noRotate := e.Sim != nil && e.Sim.NoRotate
	if !noRotate && model != nil {
		if x, y, z, ok := model.MarkerFrameAxes(marker, pose, true); ok {
			local = [3]fauxgl.Vector{x, y, z}
		}
	}
	// The attribute editor's correction is applied to the effect geometry
	// after its attachment frame, so show the corrected local frame here too.
	for i := range local {
		local[i] = e.fxTurnVector(local[i])
	}
	return [2][3]fauxgl.Vector{local, {{X: 1}, {Y: 1}, {Z: 1}}}
}

func (ui *uiState) fxAxesButton(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v == nil {
		return layout.Dimensions{}
	}
	if v.fxAxesClick.Clicked(gtx) {
		v.showFXAxes = !v.showFXAxes
		v.invalidate()
	}
	label := "FX axes"
	if v.showFXAxes {
		label = "Axes on"
	}
	return ui.smallButton(gtx, &v.fxAxesClick, label)
}

func (ui *uiState) modelFxAxesOverlay(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.model == nil || t.model.model == nil || !t.model.showFXAxes {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	return t.model.layoutFXOriginAxes(ui.th, gtx, w, h)
}

func (v *modelViewer) layoutFXOriginAxes(th *material.Theme, gtx layout.Context, w, h int) layout.Dimensions {
	if v == nil || v.model == nil || w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	anim := ""
	if v.anim != nil {
		anim = v.anim.Name
	}
	v.fxSelectionAnimName(anim)
	indices := v.fxActiveIndices(anim)
	if len(indices) == 0 {
		return layout.Dimensions{}
	}
	view := buildViewMatrix(v.options(w, h))
	pose := v.pose()
	origins := make(map[[21]uint64]fxOriginAxesEntry, len(indices))
	for _, index := range indices {
		e := &v.fxEffects[index]
		origin := e.Origin
		if override, ok := v.fxOverrides[index]; ok && override.HasOrigin {
			origin = override.Origin
		} else if p, ok := v.model.MarkerWorld(e.Marker, pose, true); ok {
			origin = p
		}
		if e.Ground {
			if override, ok := v.fxOverrides[index]; !ok || !override.HasOrigin {
				origin = fxGroundOrigin(origin, float64(v.model.BoundMin[2]))
			}
		}
		frames := fxOriginAxisFrames(*e, v.model, e.Marker, pose)
		if e.HasManualOrigin && !e.ShowMarker && !e.ShowDirection {
			continue
		}
		origins[fxOriginAxesKey(origin, frames)] = fxOriginAxesEntry{origin: origin, frames: frames}
	}
	area := gtx.Constraints.Max
	canvasScale := math.Min(float64(area.X)/float64(w), float64(area.Y)/float64(h))
	if canvasScale <= 0 {
		return layout.Dimensions{}
	}
	offsetX := (float64(area.X) - float64(w)*canvasScale) / 2
	offsetY := (float64(area.Y) - float64(h)*canvasScale) / 2
	for _, entry := range origins {
		at := fxProject(view, entry.origin, w, h)
		if at == nil {
			continue
		}
		centreX, centreY := offsetX+at.X*canvasScale, offsetY+at.Y*canvasScale
		paintFXAxesDot(gtx, centreX, centreY, modelMarkerFwdColor)
		for frameIndex, frame := range entry.frames {
			cx := centreX + (float64(frameIndex)-0.5)*fxAxesPairGap
			for axisIndex, axis := range frame {
				probe := fxProject(view, entry.origin.Add(axis.MulScalar(fxAxesProbe)), w, h)
				if probe == nil {
					continue
				}
				dx, dy := (probe.X-at.X)*canvasScale, (probe.Y-at.Y)*canvasScale
				length := math.Hypot(dx, dy)
				if length < 1e-6 {
					continue
				}
				endX, endY := cx+dx/length*fxAxesTriadLen, centreY+dy/length*fxAxesTriadLen
				col := [3][4]float64{modelMarkerXColor, modelMarkerYColor, modelMarkerZColor}[axisIndex]
				paintFXAxesLine(gtx, cx, centreY, endX, endY, col)
				paintFXAxesText(gtx, th, [3]string{"X", "Y", "Z"}[axisIndex], endX-3, endY-7, col)
			}
			label := "local"
			if frameIndex == 1 {
				label = "world"
			}
			paintFXAxesText(gtx, th, label, cx-15, centreY+11, modelMarkerFwdColor)
		}
	}
	return layout.Dimensions{Size: area}
}

func paintFXAxesLine(gtx layout.Context, ax, ay, bx, by float64, col [4]float64) {
	r := image.Rect(int(math.Min(ax, bx)), int(math.Min(ay, by)), int(math.Max(ax, bx))+1, int(math.Max(ay, by))+1)
	if r.Dx() < 2 {
		r.Min.X--
		r.Max.X = r.Min.X + 2
	}
	if r.Dy() < 2 {
		r.Min.Y--
		r.Max.Y = r.Min.Y + 2
	}
	a := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: fxAxesColor(col)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	a.Pop()
}

func paintFXAxesDot(gtx layout.Context, x, y float64, col [4]float64) {
	r := image.Rect(int(math.Round(x))-2, int(math.Round(y))-2, int(math.Round(x))+3, int(math.Round(y))+3)
	a := clip.Rect(r).Push(gtx.Ops)
	paint.ColorOp{Color: fxAxesColor(col)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	a.Pop()
}

func paintFXAxesText(gtx layout.Context, th *material.Theme, text string, x, y float64, col [4]float64) {
	l := material.Label(th, unit.Sp(9), text)
	l.Color = fxAxesColor(col)
	l.MaxLines = 1
	measure := gtx
	measure.Constraints.Min = image.Point{}
	measure.Constraints.Max = image.Pt(128, 32)
	d := l.Layout(measure)
	if d.Size.X <= 0 || d.Size.Y <= 0 {
		return
	}
	place := gtx
	place.Constraints.Min = image.Point{}
	place.Constraints.Max = d.Size
	tr := op.Offset(image.Pt(int(math.Round(x)), int(math.Round(y)))).Push(place.Ops)
	l.Layout(place)
	tr.Pop()
}

func fxAxesColor(c [4]float64) color.NRGBA {
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
