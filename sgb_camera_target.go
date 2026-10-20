package main

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/fogleman/fauxgl"
)

// sgbCameraTargetScreen projects the orbit target into the renderer's pixel
// space, shared by the software and GPU overlay paths.
func sgbCameraTargetScreen(v *modelViewer, width, height int) (fauxgl.Vector, bool) {
	if v == nil || width < 1 || height < 1 {
		return fauxgl.Vector{}, false
	}
	at := fxProject(buildViewMatrix(v.options(width, height)), v.Center, width, height)
	if at == nil {
		return fauxgl.Vector{}, false
	}
	return *at, true
}

// paintSgbCameraTargetOverlay draws nested contrasting circles so the current
// surface point remains easy to spot against both terrain and placed objects.
func (v *modelViewer) paintSgbCameraTargetOverlay(gtx layout.Context, width, height int) {
	at, ok := sgbCameraTargetScreen(v, width, height)
	if !ok {
		return
	}
	x, y := int(at.X+0.5), int(at.Y+0.5)
	for _, mark := range []struct {
		radius int
		color  color.NRGBA
	}{
		{15, color.NRGBA{R: 12, G: 14, B: 18, A: 245}},
		{11, color.NRGBA{R: 255, G: 194, B: 40, A: 255}},
		{7, color.NRGBA{R: 16, G: 20, B: 25, A: 255}},
		{3, color.NRGBA{R: 255, G: 255, B: 240, A: 255}},
	} {
		bounds := image.Rect(x-mark.radius, y-mark.radius, x+mark.radius+1, y+mark.radius+1)
		paint.FillShape(gtx.Ops, mark.color, clip.Ellipse(bounds).Op(gtx.Ops))
	}
}
