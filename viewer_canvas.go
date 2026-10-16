package main

import (
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// modelCanvasWithFxList stacks readouts and diagnostics over the rendered frame.
func (ui *uiState) modelCanvasWithFxList(gtx layout.Context, t *fileTab) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelCanvas(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelFxAttributeDialog(gtx, t.model) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelAnimScrubOverlay(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelFxAxesOverlay(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelGizmoOverlay(gtx, t) }),
	)
}

// modelCanvas paints the last rendered frame and handles orbiting.
func (ui *uiState) modelCanvas(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	area := gtx.Constraints.Max
	if area.X < 8 || area.Y < 8 {
		v.redraw = true
		return layout.Dimensions{}
	}
	w, h := area.X, area.Y
	if w > 1280 {
		h = int(float64(h) * 1280 / float64(w))
		w = 1280
	}
	if h < 1 {
		h = 1
	}
	v.setRenderSize(w, h)
	if ui.fxStartSoundLoad(v) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	fxDrawn := false
	if v.showFX || v.fxAttrIndex >= 0 || (v.showFXAxes && len(v.fxEffects) == 0) {
		if ui.fxStartLoad(v) {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
		if v.showFX {
			// The hardware frame draws this frame's particles itself, so the
			// software picture is only built for the path that paints one.
			fxDrawn = v.fxAdvance(gtx.Now, w, h, v.options(w, h), ui.modelGpuUnavailable(v) != "")
		} else if len(v.fxEffects) > 0 {
			v.fxOrigins()
		}
		if fxDrawn {
			v.redraw = true
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
	}

	now := gtx.Now
	if v.playing && v.anim != nil {
		if v.lastFrame.IsZero() {
			v.lastFrame = now
		}
		frames := maxInt(1, v.anim.NumFrames)
		dt := now.Sub(v.lastFrame).Seconds()
		v.lastFrame = now
		v.time += float32(dt * animFramesPerSecond(v.anim) / float64(frames))
		if v.time > 1 {
			v.time -= 1
		}
		v.redraw = true
	}
	if v.redraw {
		v.redraw = false
		if ui.renderModelOnGpu(gtx, t, w, h) {
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		} else {
			v.requestRender(w, h, modelQualityFinal)
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		}
	}
	// A frame finished on a worker goroutine after the last layout ran, so
	// schedule one more frame to actually put it on screen.
	if v.frame != nil && v.painted != v.serial {
		v.painted = v.serial
		gtx.Execute(op.InvalidateCmd{})
	}
	if v.rendering {
		// Keep polling until the background render lands.
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}

	rect := clip.Rect{Max: area}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &v.tag,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
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
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonSecondary) {
				v.panning = true
				v.dragging = false
				v.lastX, v.lastY = e.Position.X, e.Position.Y
				continue
			}
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			if scrubHit(v.scrubRect, e.Position) {
				v.seeking = true
				v.dragging = false
				picked := v.scrubSeek(e.Position.X, t != ui.unitModel || ui.unitListMode == 2)
				v.keyDrag = picked && t == ui.unitModel
				v.keyDragMoved = false
				LogInfo("3D preview: strip press pos=(%.1f, %.1f) keymark=%v", e.Position.X, e.Position.Y, v.keyDrag)
				continue
			}
			v.dragging = true
			v.lastX, v.lastY = e.Position.X, e.Position.Y
		case pointer.Drag:
			if v.panning {
				dx := float64(e.Position.X - v.lastX)
				dy := float64(e.Position.Y - v.lastY)
				v.lastX, v.lastY = e.Position.X, e.Position.Y
				v.panCamera(dx, dy, h)
				continue
			}
			if v.seeking {
				if v.keyDrag {
					if v.movePickedBoneKeyToX(e.Position.X) {
						ui.unitModelWhmDirty = true
						v.keyDragMoved = true
					}
					continue
				}
				v.scrubSeek(e.Position.X, t != ui.unitModel || ui.unitListMode == 2)
				continue
			}
			if !v.dragging {
				continue
			}
			dx := float64(e.Position.X - v.lastX)
			dy := float64(e.Position.Y - v.lastY)
			v.lastX, v.lastY = e.Position.X, e.Position.Y
			v.Yaw -= dx * 0.01
			v.Pitch = clampFloat64(v.Pitch+dy*0.01, -1.45, 1.45)
			v.redraw = true
		case pointer.Scroll:
			v.zoomBy(modelZoomByDelta(e.Scroll.Y))
			v.redraw = true
		case pointer.Release, pointer.Cancel:
			if v.keyDrag && v.keyDragMoved {
				if f, ok := v.boneKeyFrameAt(); ok {
					LogInfo("3D preview: bone key drag ended on frame %d", f+1)
					ui.statusText = fmt.Sprintf("Moved bone key to frame %d", f+1)
				}
			}
			v.keyDrag, v.keyDragMoved = false, false
			v.dragging, v.panning, v.seeking = false, false, false
		}
	}

	if v.gpuFrame != nil {
		if v.dragging || v.panning || (v.playing && v.anim != nil) {
			// Keep the frame refreshing while something is moving.
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		}
		if os.Getenv("DOW_NO_GPU_PAINT") != "" {
			// Diagnostic: keep the raw pass running but leave the frame out of
			// the op stream, which separates a bad render from a bad hand over.
			if modelGPUPaintSkipLogged.IsZero() {
				modelGPUPaintSkipLogged = time.Now()
				log.Printf("3D preview: GPU paint skipped (DOW_NO_GPU_PAINT)")
			}
			fillRect(gtx, rgb(0x1C1E24), area)
		} else {
			v.gpuFrame.Paint(gtx, image.Rectangle{Max: area})
		}
		// The picked marker is painted in the frame's own transform, so it
		// lands on the model exactly where the frame put it.
		v.gpuFrame.Overlay(gtx, image.Rectangle{Max: area}, func(gtx layout.Context) {
			v.paintMarkerOverlay(gtx, w, h)
		})
		rect.Pop()
		return layout.Dimensions{Size: area}
	}
	img := v.frame
	if img == nil {
		if modelRenderingLogged.IsZero() || time.Since(modelRenderingLogged) >= time.Second {
			modelRenderingLogged = time.Now()
			log.Printf("3D preview: no frame on screen (skeleton=%v loading=%v)", v.showSkeleton, v.loading)
		}
		gtx.Execute(op.InvalidateCmd{At: now.Add(32 * time.Millisecond)})
		rect.Pop()
		fillRect(gtx, rgb(0x1C1E24), area)
		return layout.Stack{}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				fillRect(gtx, rgb(0x1C1E24), area)
				return layout.Dimensions{Size: area}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				msg := "Rendering…"
				if v.loading {
					msg = "Loading textures…"
				}
				l := material.Label(ui.th, unit.Sp(10), msg)
				l.Color = cTextDim
				return layout.Center.Layout(gtx, l.Layout)
			}),
		)
	}
	if v.dragging || v.panning || (v.playing && v.anim != nil) {
		gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
	}
	b := img.Bounds()
	scale := float32(area.X) / float32(b.Dx())
	if s := float32(area.Y) / float32(b.Dy()); s < scale {
		scale = s
	}
	dx := (float32(area.X) - float32(b.Dx())*scale) / 2
	dy := (float32(area.Y) - float32(b.Dy())*scale) / 2
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale))).Push(gtx.Ops)
	paint.NewImageOp(img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if fxDrawn && v.fxOverlay != nil {
		paint.NewImageOp(v.fxOverlay).Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	v.paintMarkerOverlay(gtx, w, h)
	v.paintFXAttributeMarkers(gtx, w, h)
	tr.Pop()
	rect.Pop()
	return layout.Dimensions{Size: area}
}

// modelGizmoOverlay puts the camera orientation gizmo in the bottom right.
func (ui *uiState) modelGizmoOverlay(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	if v == nil || v.model == nil {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	opts := v.options(w, h)
	measure := cameraAxesLayout(ui.th, opts, false)
	gtx.Constraints.Min = image.Point{}
	d := measure(gtx)
	if d.Size.X == 0 || d.Size.Y == 0 {
		return d
	}
	at := image.Point{X: w - d.Size.X - gizmoMargin, Y: h - d.Size.Y - gizmoMargin}
	if at.X < 0 {
		at.X = 0
	}
	if at.Y < 0 {
		at.Y = 0
	}
	macro := op.Offset(at).Push(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = d.Size
	cameraAxesLayout(ui.th, opts, true)(gtx)
	macro.Pop()
	return d
}

const gizmoMargin = 10.0
