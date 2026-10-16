package main

import (
	"image"
	"math"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/fogleman/fauxgl"
)

const markerArrowWorldLength = 0.5

func (v *modelViewer) paintMarkerOverlay(gtx layout.Context, w, h int) {
	at, ok := v.markerHighlight(w, h)
	if !ok {
		return
	}
	paintMarkerRing(gtx, at)
	if tip, ok := v.markerArrow(w, h); ok {
		paintMarkerArrow(gtx, at, tip)
	}
}

func (v *modelViewer) markerHighlight(w, h int) (fauxgl.Vector, bool) {
	if v == nil || v.model == nil || v.markerIndex < 0 || v.markerIndex >= len(v.model.Markers) {
		return fauxgl.Vector{}, false
	}
	world, ok := v.model.MarkerWorld(v.markerIndex, v.pose(), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	p := fxProject(buildViewMatrix(v.options(w, h)), world, w, h)
	if p == nil {
		return fauxgl.Vector{}, false
	}
	return *p, true
}

func (v *modelViewer) markerArrow(w, h int) (fauxgl.Vector, bool) {
	if v == nil || v.model == nil || v.markerIndex < 0 || v.markerIndex >= len(v.model.Markers) {
		return fauxgl.Vector{}, false
	}
	axis, ok := v.model.MarkerAxis(v.markerIndex, v.pose(), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	world, ok := v.model.MarkerWorld(v.markerIndex, v.pose(), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	view := buildViewMatrix(v.options(w, h))
	at := fxProject(view, world, w, h)
	tip := fxProject(view, world.Add(axis.MulScalar(markerArrowWorldLength)), w, h)
	if at == nil || tip == nil {
		return fauxgl.Vector{}, false
	}
	return *tip, true
}

func paintMarkerArrow(gtx layout.Context, from, to fauxgl.Vector) {
	drawBar := func(ax, ay, bx, by float64) {
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
		paint.ColorOp{Color: cMarkerHi}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		a.Pop()
	}
	dx, dy := to.X-from.X, to.Y-from.Y
	length := math.Hypot(dx, dy)
	if length < 1 {
		return
	}
	ux, uy := dx/length, dy/length
	head := math.Min(11, length*0.45)
	baseX, baseY := to.X-ux*head, to.Y-uy*head
	const wing = 6.0
	drawBar(from.X, from.Y, baseX, baseY)
	drawBar(baseX-uy*wing, baseY+ux*wing, to.X, to.Y)
	drawBar(baseX+uy*wing, baseY-ux*wing, to.X, to.Y)
}

func paintMarkerRing(gtx layout.Context, at fauxgl.Vector) {
	const half, thick = 9.0, 2.0
	x0, y0 := int(at.X-half), int(at.Y-half)
	x1, y1 := int(at.X+half), int(at.Y+half)
	bars := [4]image.Rectangle{
		image.Rect(x0-thick/2, y0-thick/2, x1+thick/2, y0+thick/2),
		image.Rect(x0-thick/2, y1-thick/2, x1+thick/2, y1+thick/2),
		image.Rect(x0-thick/2, y0-thick/2, x0+thick/2, y1+thick/2),
		image.Rect(x1-thick/2, y0-thick/2, x1+thick/2, y1+thick/2),
	}
	for _, r := range bars {
		a := clip.Rect(r).Push(gtx.Ops)
		paint.ColorOp{Color: cMarkerHi}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		a.Pop()
	}
}

// paintFXAttributeMarkers draws optional origin rings and direction arrows for
// effects whose attribute editor has those diagnostics enabled.
func (v *modelViewer) paintFXAttributeMarkers(gtx layout.Context, w, h int) {
	if v == nil || v.model == nil || w < 1 || h < 1 {
		return
	}
	view := buildViewMatrix(v.options(w, h))
	pose := v.pose()
	for i := range v.fxEffects {
		e := &v.fxEffects[i]
		if !e.ShowMarker && !e.ShowDirection {
			continue
		}
		origin := e.Origin
		if ov, ok := v.fxOverrides[i]; ok && ov.HasOrigin {
			origin = ov.Origin
		}
		at := fxProject(view, origin, w, h)
		if at == nil {
			continue
		}
		if e.ShowMarker {
			paintMarkerRing(gtx, *at)
		}
		if e.ShowDirection {
			axis := e.Direction
			if axis.Length() < 1e-6 && e.Marker >= 0 {
				axis, _ = v.model.MarkerAxis(e.Marker, pose, true)
			}
			if axis.Length() > 1e-6 {
				if tip := fxProject(view, origin.Add(axis.Normalize().MulScalar(markerArrowWorldLength)), w, h); tip != nil {
					paintMarkerArrow(gtx, *at, *tip)
				}
			}
		}
	}
}
