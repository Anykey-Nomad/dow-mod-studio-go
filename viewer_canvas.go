package main

import (
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"strings"
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
	"github.com/fogleman/fauxgl"
)

// modelCanvasWithFxList stacks the readout of the effects playing on the clip
// on show over the canvas, top left, where it does not cover the model the way
// the camera axes in the corner do not.
//
// The canvas is laid out first and fills the stack, so the render still gets
// the whole area; the list is what is drawn on top of it.
func (ui *uiState) modelCanvasWithFxList(gtx layout.Context, t *fileTab) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelCanvas(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelFxListOverlay(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelAnimScrubOverlay(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelGizmoOverlay(gtx, t) }),
	)
}

// modelCanvas paints the last rendered frame and handles orbiting.
func (ui *uiState) modelCanvas(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	area := gtx.Constraints.Max
	if area.X < 8 || area.Y < 8 {
		// The panel is collapsed; try again with real dimensions next frame.
		v.redraw = true
		return layout.Dimensions{}
	}
	// Cap the render resolution: a software rasteriser over a whole 4K panel
	// would take seconds per frame.
	w, h := area.X, area.Y
	if w > 1280 {
		h = int(float64(h) * 1280 / float64(w))
		w = 1280
	}
	if h < 1 {
		h = 1
	}
	v.setRenderSize(w, h)

	// The model's sounds are resolved from the first frame it is on screen, not
	// from the moment the FX button is pressed: decoding an .fda is far more
	// work than drawing a frame, and a reader may never press the button.
	if ui.fxStartSoundLoad(v) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	// The effects are stepped before the frame is asked for, so a hardware
	// renderer draws this frame's particles with this frame's model rather
	// than the set it had when the button was pressed.
	fxDrawn := false
	if v.showFX {
		if ui.fxStartLoad(v) {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
		// The hardware frame draws this frame's particles itself, against
		// the model's own depth buffer, so the software picture of them is
		// only for the path that paints one: building both is a full
		// rasterisation into an image the hardware frame never shows. The
		// reason is the same string the renderer below acts on, so a path
		// that turns out to be off — or that breaks mid-frame — rebuilds
		// the picture from the next frame on.
		fxDrawn = v.fxAdvance(gtx.Now, w, h, v.options(w, h, modelQualityFinal), ui.modelGpuUnavailable(v) != "")
		if fxDrawn {
			// The particles are moving even when the model is not, so
			// the canvas keeps asking for itself until the button goes
			// off.
			v.redraw = true
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
	}

	// Advance playback.
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

	rect := clip.Rect{Max: area}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &v.tag,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			// A filter with no scroll range takes the whole of a wheel
			// gesture and hands the widget none of it, so the range has to
			// be opened up: the wheel is how the camera moves in and out.
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
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			// A press on the frame strip winds the playhead rather than
			// starting a camera drag: the strip is over the model, and
			// dragging from it would turn the view by a few pixels for
			// what the reader meant as a click on a frame.
			if scrubHit(v.scrubRect, e.Position) {
				v.seeking = true
				v.dragging = false
				picked := v.scrubSeek(e.Position.X, t != ui.unitModel || ui.unitListMode == 2)
				// A press that landed on one of the picked bone's keymarks
				// is the handle its drag moves by: the press alone selects
				// the key as the playhead winds to it, and a drag that
				// follows carries the key rather than the playhead. Only
				// the unit editor has a Save to carry the edit that far,
				// so anywhere else the strip stays a plain scrub.
				v.keyDrag = picked && t == ui.unitModel
				v.keyDragMoved = false
				LogInfo("3D preview: strip press pos=(%.1f, %.1f) keymark=%v", e.Position.X, e.Position.Y, v.keyDrag)
				continue
			}
			v.dragging = true
			v.lastX, v.lastY = e.Position.X, e.Position.Y
		case pointer.Drag:
			// Dragging along the strip scrubs: the pointer can be anywhere
			// between the bar and the numbers, and every position is a frame.
			if v.seeking {
				if v.keyDrag {
					// The press was on a keymark of the picked bone, so the
					// drag moves that key and not the playhead: the strip is
					// a track the key slides along, and the pose on show
					// stays where the click put it so the change is read
					// against the frame it left.
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
			v.keyDrag = false
			v.keyDragMoved = false
			v.dragging = false
			v.seeking = false
		}
	}

	if v.gpuFrame != nil {
		if v.dragging || (v.playing && v.anim != nil) {
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
		// The picked marker is painted in the frame's own transform, so
		// it lands on the model exactly where the frame put it. It used
		// to be reachable only on the software path, because the hardware
		// branch returned before reaching it.
		v.gpuFrame.Overlay(gtx, image.Rectangle{Max: area}, func(gtx layout.Context) {
			v.paintMarkerOverlay(gtx, w, h)
		})
		rect.Pop()
		return layout.Dimensions{Size: area}
	}
	img := v.frame
	if img == nil {
		// Nothing on screen yet. Keep asking for frames, because the first
		// render is what fills this in.
		if modelRenderingLogged.IsZero() || time.Since(modelRenderingLogged) >= time.Second {
			modelRenderingLogged = time.Now()
			log.Printf("3D preview: no frame on screen, showing Rendering… (gpuBroken=%v skeleton=%v loading=%v rendering=%v serial=%d size=%dx%d)",
				v.gpuBroken, v.showSkeleton, v.loading, v.rendering, v.serial, w, h)
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
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					msg := "Rendering…"
					if v.loading {
						msg = "Loading textures…"
					}
					l := material.Label(ui.th, unit.Sp(10), msg)
					l.Color = cTextDim
					return l.Layout(gtx)
				})
			}),
		)
	}
	if v.dragging || (v.playing && v.anim != nil) {
		// Keep the frame refreshing while something is moving.
		gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
	}
	b := img.Bounds()
	scale := float32(area.X) / float32(b.Dx())
	if s := float32(area.Y) / float32(b.Dy()); s < scale {
		scale = s
	}
	dx := (float32(area.X) - float32(b.Dx())*scale) / 2
	dy := (float32(area.Y) - float32(b.Dy())*scale) / 2
	aff := op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale)))
	tr := aff.Push(gtx.Ops)
	paint.NewImageOp(img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if fxDrawn && v.gpuFrame == nil && v.fxOverlay != nil {
		// Inside the frame's own transform: the overlay is the size of
		// the frame, so the two line up exactly. A frame drawn by the
		// hardware renderer already carries its particles, so the CPU
		// overlay is only for the software path — and the hardware path
		// does not build it. The nil check covers the frame in which the
		// hardware render broke: the picture comes back with the next
		// frame, when the broken flag makes the software path the one
		// that runs.
		paint.NewImageOp(v.fxOverlay).Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	// The picked marker, outlined on both render paths: it is painted into the
	// same transform as the frame, so it needs no idea what the canvas is.
	// The arrow beside it says which way the marker looks, which is the half of
	// a marker the ring cannot show.
	v.paintMarkerOverlay(gtx, w, h)
	tr.Pop()
	rect.Pop()
	return layout.Dimensions{Size: area}
}

// modelGizmoOverlay puts the axis gizmo in the bottom right corner of the
// canvas, which is where 3ds Max and Blender keep theirs: it is read often,
// it is small, and it must not sit over the model.
//
// It is stacked rather than drawn into the frame so it can be real text. The
// arrows are bars and the letters and degrees are labels, laid out at offsets
// because Gio has no absolute layout of its own.
func (ui *uiState) modelGizmoOverlay(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	if v == nil || v.model == nil {
		return layout.Dimensions{}
	}
	// The gizmo asks the frame how big it is, so the axes project against
	// the same size the canvas was rendered at.
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	opts := v.options(w, h, 0)
	// Measured first without drawing, then drawn once at the corner. Laying the
	// gizmo out to learn its size and then laying it out again to place it would
	// paint it twice — once at the origin, over whatever else is in that
	// corner — because the bars and the labels are ops, not measurements.
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

// gizmoMargin keeps the gizmo off the frame's edge.
const gizmoMargin = 10.0

// modelFxListOverlay lists the effects the current clip plays: the event name,
// the effect it plays and the marker it hangs off. An effect that is wired to
// no marker is the one that sends a developer looking, so it is shown with a
// dash rather than hidden.
//
// It needs a widget rather than a bar overlay because it is names and paths,
// not marks, and this Gio draws text only through the widget layer.
func (ui *uiState) modelFxListOverlay(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	if v == nil || v.model == nil {
		return layout.Dimensions{}
	}
	active := v.fxActiveEffects(v.fxAnim)
	if len(active) == 0 {
		return layout.Dimensions{}
	}
	lines := make([]string, 0, len(active))
	for i := range active {
		e := &active[i]
		marker := "no marker"
		if e.Marker >= 0 && e.Marker < len(v.model.Markers) {
			marker = v.model.Markers[e.Marker].Name
		}
		if e.HasOther && e.OtherMarker >= 0 && e.OtherMarker < len(v.model.Markers) {
			marker += " -> " + v.model.Markers[e.OtherMarker].Name
		}
		lines = append(lines, fmt.Sprintf("%s  |  %s  |  %s", e.Label, e.FxName, marker))
	}
	// The list is a debugging readout, not a panel: a model with a hundred
	// events must not push the frame it is describing off the screen.
	const maxLines = 12
	shown := len(lines)
	if shown > maxLines {
		lines = lines[:maxLines]
	}

	const pad = 6
	list := func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(pad).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(fxListLabel(ui.th, fmt.Sprintf("FX on %s (%d)", v.fxAnim, shown), cTextDim)),
				layout.Rigid(fxListLabel(ui.th, strings.Join(lines, "\n"), cText)),
			)
		})
	}
	// The list is given a width it can lay text into: left to itself it would
	// take the whole canvas and the model would vanish under it.
	gtx.Constraints.Max.X = gtx.Constraints.Min.X + 420
	if gtx.Constraints.Max.X < 80 {
		gtx.Constraints.Max.X = 80
	}
	return list(gtx)
}

// markerHighlight is where the picked marker lands on the frame, in the frame's
// own pixels. It reports false when no marker is picked, when the model carries
// none, or when the marker is behind the eye — which is the case where a ring
// drawn anyway would sit in the middle of the model and read as a mistake.
//
// The ring says a marker is here. This says which way it faces, which is what
// an effect hanging off it actually uses: a muzzle's effects leave along its
// forward axis, and a ring on its own cannot tell one direction from another.
//
// A marker's forward is not a column of its matrix that can simply be picked —
// see modelMarkerForwards, which works the direction out per marker by scoring
// its six frame directions against the mesh of the bone it hangs off. So the
// arrow reuses that answer rather than guessing an axis, and it reports false
// when the marker has no direction to show: a marker on no bone, or one whose
// forward points away from the eye and lands outside the frame.

// paintMarkerOverlay draws the picked marker's ring and its direction arrow,
// both already in the frame's own pixels. It is called inside the frame's
// transform on either render path, which is what keeps the ring on the marker
// itself rather than wherever the canvas happens to be scaled to.
func (v *modelViewer) paintMarkerOverlay(gtx layout.Context, w, h int) {
	at, ok := v.markerHighlight(w, h)
	if !ok {
		return
	}
	paintMarkerRing(gtx, at)
	if tip, hasDir := v.markerArrow(w, h); hasDir {
		paintMarkerArrow(gtx, at, tip)
	}
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
	view := buildViewMatrix(v.options(w, h, 0))
	// Both ends are projected and the difference taken, rather than projecting
	// a point at a fixed distance along the axis: a direction has no length, so
	// there is no distance to project it to that would mean anything. Taking it
	// between the marker and a point one marker-length along the axis gives an
	// arrow whose length tracks how big the marker is on screen.
	at := fxProject(view, world, w, h)
	if at == nil {
		return fauxgl.Vector{}, false
	}
	tip := fxProject(view, world.Add(axis.MulScalar(markerArrowWorldLength)), w, h)
	if tip == nil {
		return fauxgl.Vector{}, false
	}
	return *tip, true
}

// markerArrowWorldLength is how far along a marker's forward axis the arrow's
// tip is placed, in the units the model's own geometry is in. It is only a
// length to give the projection something to measure: the arrow is drawn
// between the two projected points, so what reaches the screen is whatever
// looks right there, not this number.
const markerArrowWorldLength = 0.5

// paintMarkerArrow draws the shaft and head of the direction arrow, from the
// marker's ring at from towards to. Both ends are already in the frame's own
// pixels.
//
// Like the ring it is four bars and not a stroked path, because stroking needs
// the clipper package this Gio does not carry. The head is two bars meeting at
// the tip, which reads as an arrowhead at the size this is ever drawn and costs
// the same as the shaft.
func paintMarkerArrow(gtx layout.Context, from, to fauxgl.Vector) {
	const thick = 2.0
	drawBar := func(ax, ay, bx, by float64) {
		// A bar is a rectangle between the two ends, so whichever way the bar
		// runs the flat sides get the thickness and the other pair do not.
		r := image.Rect(int(math.Min(ax, bx)), int(math.Min(ay, by)),
			int(math.Max(ax, bx))+1, int(math.Max(ay, by))+1)
		if r.Dx() < int(thick) {
			r.Min.X -= (int(thick) - r.Dx()) / 2
			r.Max.X = r.Min.X + int(thick)
		}
		if r.Dy() < int(thick) {
			r.Min.Y -= (int(thick) - r.Dy()) / 2
			r.Max.Y = r.Min.Y + int(thick)
		}
		area := clip.Rect(r).Push(gtx.Ops)
		paint.ColorOp{Color: cMarkerHi}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		area.Pop()
	}
	// The head stops short of the tip, and the two bars are drawn along the
	// axis turned back into each wing, so the arrowhead stays the same width
	// however far the arrow runs.
	dx, dy := to.X-from.X, to.Y-from.Y
	length := math.Hypot(dx, dy)
	if length < 1 {
		// Too short to aim: a head on its own would be a blob, which reads as
		// a smudge on the model rather than a direction.
		return
	}
	ux, uy := dx/length, dy/length
	headLen := math.Min(11, length*0.45)
	baseX, baseY := to.X-ux*headLen, to.Y-uy*headLen
	// A quarter width either side of the axis at the base of the head.
	const wing = 6.0
	drawBar(from.X, from.Y, baseX, baseY)
	drawBar(baseX-uy*wing, baseY+ux*wing, to.X, to.Y)
	drawBar(baseX+uy*wing, baseY-ux*wing, to.X, to.Y)
}

// markerHighlight is where the picked marker lands on the frame, in the frame's
// own pixels. It reports false when no marker is picked, when the model carries
// none, or when the marker is behind the eye — which is the case where a ring
// drawn anyway would sit in the middle of the model and read as a mistake.
func (v *modelViewer) markerHighlight(w, h int) (fauxgl.Vector, bool) {
	if v == nil || v.model == nil || v.markerIndex < 0 || v.markerIndex >= len(v.model.Markers) {
		return fauxgl.Vector{}, false
	}
	world, ok := v.model.MarkerWorld(v.markerIndex, v.pose(), true)
	if !ok {
		return fauxgl.Vector{}, false
	}
	p := fxProject(buildViewMatrix(v.options(w, h, 0)), world, w, h)
	if p == nil {
		return fauxgl.Vector{}, false
	}
	return *p, true
}

// paintMarkerRing draws the red outline around a picked marker. It is painted
// in the frame's own transform rather than the canvas's, so it lands on the
// model exactly where the frame put it whatever the canvas is scaled to.
//
// The ring is four bars rather than a stroked path: stroking needs the
// clipper package, which this Gio does not carry, and a square outline four
// bars wide reads the same at the size this is ever drawn.
func paintMarkerRing(gtx layout.Context, at fauxgl.Vector) {
	const (
		half  = 9.0
		thick = 2.0
	)
	x0, y0 := int(at.X-half), int(at.Y-half)
	x1, y1 := int(at.X+half), int(at.Y+half)
	bars := [4]image.Rectangle{
		image.Rect(x0-thick/2, y0-thick/2, x1+thick/2, y0+thick/2),
		image.Rect(x0-thick/2, y1-thick/2, x1+thick/2, y1+thick/2),
		image.Rect(x0-thick/2, y0-thick/2, x0+thick/2, y1+thick/2),
		image.Rect(x1-thick/2, y0-thick/2, x1+thick/2, y1+thick/2),
	}
	for _, b := range bars {
		area := clip.Rect(b).Push(gtx.Ops)
		paint.ColorOp{Color: cMarkerHi}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		area.Pop()
	}
}
