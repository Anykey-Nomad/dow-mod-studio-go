package main

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/whm"
	"image"
	"image/color"

	viewer "dow-mod-studio/internal/3d_viewer"
	"gioui.org/layout"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

// Software rendering of a decoded .whm model.

// modelOverlayLine and the colour names preserve the root-level interface used
// by the GPU renderer and viewer diagnostics.
type modelOverlayLine = engine.OverlayLine

var (
	modelSkeletonColor  = engine.SkeletonColor
	modelBonePickColor  = engine.BonePickColor
	modelMarkerXColor   = engine.MarkerXColor
	modelMarkerYColor   = engine.MarkerYColor
	modelMarkerZColor   = engine.MarkerZColor
	modelMarkerFwdColor = engine.MarkerForwardColor
)

func pickedMarker(index int) *int {
	if index < 0 {
		return nil
	}
	return &index
}

func pickedBone(index int) *int {
	if index < 0 {
		return nil
	}
	return &index
}

var cameraAxisColor = [3][4]float64{
	modelMarkerXColor, modelMarkerYColor, modelMarkerZColor,
}

func cameraAxesAngles(opts RenderOptions) [3]float64 {
	return engine.AxisAngles(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch})
}

func cameraOrbitDirection(opts RenderOptions) fauxgl.Vector {
	return engine.OrbitDirection(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch})
}

func cameraViewDirection(opts RenderOptions) fauxgl.Vector {
	return engine.ViewDirection(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch})
}

func cameraAxesLayout(th *material.Theme, opts RenderOptions, draw bool) layout.Widget {
	camera := engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}
	return viewer.CameraAxesLayout(th, camera, cameraAxisColor, modelMarkerFwdColor, draw)
}

func modelOverlayHalfWidth(opts RenderOptions, pixels float64) float64 {
	return engine.OverlayHalfWidth(engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}}, pixels)
}

func modelOverlayLines(model *WhmModel, pose WhmPose, opts RenderOptions) []modelOverlayLine {
	if model == nil {
		return nil
	}
	eo := engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}, Skeleton: opts.Skeleton, Marker: opts.Marker, PickedBone: opts.PickedBone}
	return engine.OverlayLines(&model.Model, pose, eo)
}

func modelMarkerOverlayLines(model *WhmModel, pose WhmPose, opts RenderOptions) []modelOverlayLine {
	if model == nil {
		return nil
	}
	eo := engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}, Marker: opts.Marker}
	return engine.MarkerOverlayLines(&model.Model, pose, eo)
}

// vec3From converts the format's float array to the renderer's vector type.
//
// The texture bridge also lives here: callers keep using the root helper while
// image sampling itself belongs to internal/3d_engine.
type modelTexture = engine.Texture

func newModelTexture(img image.Image) fauxgl.Texture { return engine.NewTexture(img) }

func vec3From(v [3]float32) fauxgl.Vector {
	return fauxgl.Vector{X: float64(v[0]), Y: float64(v[1]), Z: float64(v[2])}
}

// boneInRange is retained for the root's renderer diagnostics; the WHM
// implementation lives with the model package.
func boneInRange(bone int32, count int) bool {
	return whm.BoneInRange(bone, count)
}

// SkinnedVertex keeps the renderer-facing WhmModel API while skinning is
// implemented next to whm.Model.
func SkinnedVertex(m *WhmModel, pose WhmPose, v *WhmVertex) (fauxgl.Vector, fauxgl.Vector) {
	if m == nil {
		return fauxgl.Vector{}, fauxgl.Vector{}
	}
	return whm.SkinnedVertex(&m.Model, pose, v)
}

// quatMatrix is shared by the viewer's key editor and delegates the actual
// conversion to the WHM package.
func quatMatrix(q [4]float32) fauxgl.Matrix { return whm.QuatMatrix(q) }

// RenderOptions is the app-side render request. The engine owns the common
// render state; only the FX scene and material types remain app-specific.
type RenderOptions struct {
	engine.RenderOptions
	FX                *fxGpuScene
	MaterialOverrides map[string]WhmMaterial
	viewOverride      *fauxgl.Matrix
}

const (
	modelQualityDraft          = 0
	modelQualityFinal          = 1
	modelMarkerAxisWorldLength = engine.MarkerAxisWorldLength
)

// meshAlphaFor is how much of the named mesh this frame draws, 1 for a mesh
// the clip says nothing about.
func meshAlphaFor(opts RenderOptions, name string) float32 {
	return engine.MeshAlpha(opts.MeshAlpha, name)
}

// meshAlphaByte is an alpha as the byte a tint carries. It is rounded to
// nearest rather than truncated, which is what keeps a fully faded mesh fully
// invisible and a whole one exactly whole: a hardware premultiply of 254/255
// leaves a mesh one step short of opaque, and a truncated 0.999 is 0.9978,
// which shows up as a mesh that never quite reaches full colour.
func meshAlphaByte(alpha float32) uint8 { return engine.MeshAlphaByte(alpha) }

// uvMapping preserves the app's established UV settings interface while the
// shared mapping implementation lives in internal/3d_engine.
type uvMapping = engine.UVMapping

func uvIdentityMapping() uvMapping { return uvMapping{Scale: 1} }

func renderUVFor(opts RenderOptions, material string) uvMapping {
	if m, ok := opts.UVPerMaterial[material]; ok && m != nil {
		return *m
	}
	return opts.UVDefault
}

// uvTransform delegates the renderer's UV mapping to the 3D engine package.
func buildViewMatrix(opts RenderOptions) fauxgl.Matrix {
	return engine.ViewMatrix(engine.Camera{Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch, Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov})
}

func uvTransform(u, v float32, m uvMapping) (float32, float32) {
	return engine.TransformUV(u, v, m)
}

// uvExtentOf and modelHasUVs are the viewer's side of modelUVExtent: the range
// as the four floats the panel prints, and whether there was one to print.
func uvExtentOf(model *WhmModel) [4]float32 {
	minU, minV, maxU, maxV, ok := modelUVExtent(model)
	if !ok {
		return [4]float32{}
	}
	return [4]float32{minU, minV, maxU, maxV}
}

func modelHasUVs(model *WhmModel) bool {
	_, _, _, _, ok := modelUVExtent(model)
	return ok
}

// modelUVExtent is the range the model's own texture coordinates cover, which
// is the first thing worth knowing about a map that is landing in the wrong
// place. A model whose coordinates run from 0 to 1 uses its map once; one whose
// V runs from −1.4 to 1 tiles it about two and a half times, and an offset
// applied to the second tiling lands in the wrong place for a reason that has
// nothing to do with the offset.
//
// It is measured rather than read out of a field because there is no such
// field: a .whm stores a coordinate per vertex and nothing about what those
// coordinates were meant to span.
func modelUVExtent(model *WhmModel) (minU, minV, maxU, maxV float32, ok bool) {
	if model == nil {
		return engine.UVBounds(nil)
	}
	return engine.UVBounds(&model.Model)
}

// WhmMaterial is the resolved material of one mesh: a diffuse map and a tint
// that multiplies it.
type WhmMaterial = engine.Material

// RenderWhmModel adapts the main package's combined UI/GPU options to the
// standalone software renderer in internal/3d_engine.
func RenderWhmModel(model *WhmModel, pose WhmPose, opts RenderOptions) *image.RGBA {
	var modelData *whm.Model
	if model != nil {
		modelData = &model.Model
	}
	engineOpts := opts.RenderOptions
	engineOpts.MaterialOverrides = opts.MaterialOverrides
	engineOpts.ViewOverride = opts.viewOverride
	return engine.Render(modelData, pose, engineOpts)
}

// ModelCenter returns the midpoint of the model bounds and a distance that
// frames the whole model, which is what the viewer needs to frame a new model.
func (m *WhmModel) ModelCenter() (fauxgl.Vector, float64) {
	if m == nil {
		return engine.ModelCenter(nil)
	}
	return engine.ModelCenter(&m.Model)
}

// NewCameraState creates the initial orbit-camera frame for a model without
// exposing any application UI state to the viewer package.
func NewCameraState(m *WhmModel, yaw, pitch, fov float64) viewer.CameraState {
	var modelData *whm.Model
	if m != nil {
		modelData = &m.Model
	}
	center, distance := engine.ModelCenter(modelData)
	return viewer.CameraState{
		Center: center, Distance: distance, FitDistance: distance,
		Yaw: yaw, Pitch: pitch, FOV: fov,
	}
}

// rgbaWhite is the default tint for a textured material.
var rgbaWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
