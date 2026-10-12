package main

import (
	"image/color"
	"log"
	"math"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// modelDefaultFov is the vertical field of view the orbit camera opens with.
// It matches what a .whm camera stores, so a model that has one of its own and
// is put on it does not jump in size.
const modelDefaultFov = 45.0

// Interactive 3D preview for a decoded .whm.
//
// Rendering is a CPU rasterisation (see model3d.go) and costs real time, so a
// frame is produced only when the camera or the animation actually changes, and
// the work happens on a worker goroutine. While the camera is moving the last
// frame stays on screen; the full quality frame replaces it when the movement
// stops.

// meshAlphas is how much of each mesh the clip on show draws at the playhead,
// for the clips that hide or fade part of a model: the restored monolith
// holds its pieces back through phase_out, and the Obelisk fades its body in
// over the first frames of build_phasing.
//
// The map is built per frame rather than kept, because a model has a few dozen
// meshes at most and one that says nothing is the common case: MeshAlphas
// returns nil for it, and nil means every mesh is drawn whole.
func (v *modelViewer) meshAlphas() map[string]float32 {
	if v == nil || v.model == nil || v.anim == nil {
		return nil
	}
	// The playhead is normalised over the clip, which is the measure the
	// visibility keys are written in.
	return v.model.MeshAlphas(v.anim.Name, float32(v.time))
}

// The range the three mapping sliders cover. An offset of a whole tile is
// already past anything a reader would be looking for, and a scale of a fifth
// of the map is a quarter of the model repeated five times over.
const (
	modelUVMinOffset = -1
	modelUVMaxOffset = 1
	modelUVMinScale  = 0.2
	modelUVMaxScale  = 4
)

// modelControlClicks applies whichever button of the strip was pressed. It is
// the one place the presses are read, so a strip laid out in a different shape —
// the viewport's two narrow rows — does exactly the same as the one under a
// canvas.
func (ui *uiState) modelControlClicks(gtx layout.Context, t *fileTab) {
	v := t.model
	if t.modelPlay.Clicked(gtx) {
		if v.anim == nil {
			v.selectAnim(0)
		}
		v.playing = !v.playing
		v.lastFrame = time.Time{}
		v.invalidate()
	}
	if t.modelPrevAnim.Clicked(gtx) {
		v.selectAnim(v.animIndex - 1)
	}
	if t.modelNextAnim.Clicked(gtx) {
		v.selectAnim(v.animIndex + 1)
	}
	if t.modelStepBack.Clicked(gtx) {
		v.stepFrame(-1)
		ui.pickFrameKeyOnTimeline(v)
	}
	if t.modelStepFwd.Clicked(gtx) {
		v.stepFrame(1)
		ui.pickFrameKeyOnTimeline(v)
	}
	if t.modelReset.Clicked(gtx) {
		v.resetCamera()
	}
	if t.modelZoomIn.Clicked(gtx) {
		v.zoomBy(modelZoomStep)
	}
	if t.modelZoomOut.Clicked(gtx) {
		v.zoomBy(1 / modelZoomStep)
	}
	if t.modelSkeleton.Clicked(gtx) {
		v.showSkeleton = !v.showSkeleton
		log.Printf("3D preview: skeleton overlay %s", onOff(v.showSkeleton))
		v.invalidate()
	}
	if t.modelFX.Clicked(gtx) {
		v.toggleFX()
	}
	if t.modelGround.Clicked(gtx) {
		v.showGround = !v.showGround
		log.Printf("3D preview: ground %s", onOff(v.showGround))
		v.invalidate()
	}
	if t.modelTexture.Clicked(gtx) {
		v.textured = !v.textured
		log.Printf("3D preview: textures %s", onOff(v.textured))
		v.invalidate()
	}
}

// modelRowHeight keeps a strip of buttons one row high. A small button fills
// whatever height it is handed, and a row of them under a canvas would take the
// canvas's height and leave it nothing.
func modelRowHeight(gtx layout.Context) layout.Context {
	if h := gtx.Dp(unit.Dp(unitRowHeightDp)); gtx.Constraints.Max.Y > h {
		gtx.Constraints.Max.Y = h
	}
	gtx.Constraints.Min.Y = 0
	return gtx
}

// The two zoom buttons are labelled with the signs rather than with words: the
// strip they sit in is one row of small buttons, and in the object window's
// side panel there is no room for "Zoom in". They are pressed one at a time
// with "Reset view" beside them, which is what says what they are for.
const (
	modelZoomInLabel  = "+"
	modelZoomOutLabel = "−"
)

// uvTextureLabel is what a texture is called in the list: the shared mapping is
// named as such, and a material is named by its own base name, which is what
// the .whm refers to it by.
func uvTextureLabel(material string) string {
	if material == "" {
		return "All textures"
	}
	name := strings.TrimSuffix(filepath.Base(material), filepath.Ext(material))
	if name == "" || name == "." {
		return material
	}
	return name
}

// uvTextureButton is one entry of the texture list. The pressed look is the one
// the selected rows use, so the choice reads as a choice without a word saying
// so.
func (ui *uiState) uvTextureButton(gtx layout.Context, btn *widget.Clickable, label string, on bool) layout.Dimensions {
	bg, bd, text := cSaveBg, cSaveBd, cText
	if on {
		bg, bd, text = cSelectBg, cSelectBd, rgb(0xFFFFFF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return frameMeasured(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), label)
				l.Color = text
				l.MaxLines = 1
				return l.Layout(gtx)
			})
		}, bg, bd, unit.Dp(3))
	})
}

// ensureUVTextureClicks sizes the texture list's click targets. They are
// rebuilt per model, so a list of the wrong length is replaced rather than
// grown into.
func (v *modelViewer) ensureUVTextureClicks(n int) {
	if len(v.uvTextureClicks) != n {
		v.uvTextureClicks = make([]widget.Clickable, n)
	}
}

// uvWholeTiles reports whether an offset is a whole number of map widths, which
// the map repeating that often makes indistinguishable from no offset at all.
func uvWholeTiles(v float32) bool {
	if v == 0 {
		return false
	}
	n := float64(v)
	return math.Abs(n-math.Round(n)) < 0.0005
}

// modelMaterialsNeedLoad reports whether this viewer has never completed its
// texture lookup and has no worker in progress.
func modelMaterialsNeedLoad(t *fileTab) bool {
	return t != nil && t.model != nil && t.model.model != nil &&
		t.modelMaterialCh == nil && !t.model.loading && !t.model.materialsLoaded
}

// layoutModelViewer draws the canvas plus the playback controls.
func (ui *uiState) layoutModelViewer(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	if v == nil || v.model == nil {
		return centeredPanelLabel(gtx, ui.th, "No model", unit.Sp(10), cTextDim)
	}
	if modelMaterialsNeedLoad(t) {
		t.modelMaterialCh = make(chan modelMaterialResult, 1)
		ui.loadModelMaterials(v, t.modelMaterialCh)
	}
	// The canvas is always laid out, even before the first frame arrives: it is
	// the canvas that starts the render, so skipping it here would leave the
	// tab permanently blank.
	if v.sideControls {
		return ui.modelCanvasWithFxList(gtx, t)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.modelCanvasWithFxList(gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.modelControlsWithUV(gtx, t) }),
	)
}

// fxListLabel lays out one block of the readout, for layout.Rigid.
func fxListLabel(th *material.Theme, s string, col color.NRGBA) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(11), s)
		l.Color = col
		l.MaxLines = 14
		return l.Layout(gtx)
	}
}

func clampFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

var _ = widget.Clickable{}

// renderModelOnGpu draws a frame with the hardware renderer, which is a few
// orders of magnitude cheaper than the software one and can animate. It reports
// false when there is no hardware renderer, or when the skeleton overlay is
// asked for, since the bone lines are drawn by the software path.
//
// The pass has to run here, on the goroutine that calls Frame and before it, so
// the device commands are ordered ahead of Gio's own drawing.
// modelGpuSkipLogged is the reason the software path was last used, so the
// log records the switch rather than one line per frame.
var modelGpuSkipLogged string

// modelRenderingLogged throttles the "nothing on screen yet" line to once a
// second, since a stuck preview asks every frame.
var modelRenderingLogged time.Time

// modelGPUPaintSkipLogged marks that the DOW_NO_GPU_PAINT diagnostic has been
// reported once.
var modelGPUPaintSkipLogged time.Time

// modelGpuUnavailable says why the hardware path will not draw this frame, or
// "" when it will.
//
// It is the decision renderModelOnGpu acts on, factored out because two
// places now ask it: the renderer itself, and the FX step, which skips
// building the software picture of the particles when the hardware frame is
// going to draw them instead. Two copies of this list would drift, and the
// drift that matters is the one where the picture was not built while the
// hardware path failed anyway.
func (ui *uiState) modelGpuUnavailable(v *modelViewer) string {
	switch {
	case v == nil || v.model == nil:
		return "no model"
	case ui.forceSoftRenderer:
		// The settings checkbox wins over every hardware path, in the
		// textured mode as in the skeleton one.
		return "software rendering enabled in settings"
	case v.gpuBroken:
		return "hardware path marked broken"
	case ui.modelGpuRenderer() == nil:
		return "no hardware renderer on this window"
	}
	return ""
}
