package render

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/fxscene"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"image"
	"image/color"

	viewer "dow-mod-studio/internal/3d_viewer"
	"gioui.org/layout"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

// Software rendering of a decoded .whm model.

// The FX scene types belong to the leaf fxscene package the overlay builds
// them for and both renderers read from; the names below keep this file's
// original wording.
type (
	fxGpuScene   = fxscene.Scene
	fxGpuGroup   = fxscene.Group
	fxGpuBeam    = fxscene.Beam
	fxSceneLight = fxscene.Light
)

// WhmPose is the animation pose a render is taken at.
type WhmPose = whm.Pose

// DefaultFov is the vertical field of view the orbit camera opens with,
// shared by the viewer that frames a model and the renderers that draw it.
// It matches what a .whm camera stores, so a model that has one of its own
// and is put on it does not jump in size.
const DefaultFov = 45.0

// fxSceneLightOf is the light a render request asks for, with the same
// defaults the model shader falls back on.
func fxSceneLightOf(opts RenderOptions) fxSceneLight {
	l := fxSceneLight{
		Dir:     opts.Light,
		Ambient: opts.Ambient,
		Diffuse: opts.Diffuse,
	}
	if l.Dir == (fauxgl.Vector{}) {
		l.Dir = fauxgl.Vector{X: 0.35, Y: -0.5, Z: 0.78}
	}
	if n := l.Dir.Length(); n > 0 {
		l.Dir = l.Dir.Normalize()
	}
	if l.Ambient == 0 && l.Diffuse == 0 {
		l.Ambient, l.Diffuse = 0.28, 0.72
	}
	return l
}

// ModelOverlayLine and the colour names preserve the root-level interface used
// by the GPU renderer and viewer diagnostics.
type ModelOverlayLine = engine.OverlayLine

var (
	ModelSkeletonColor  = engine.SkeletonColor
	ModelBonePickColor  = engine.BonePickColor
	ModelMarkerXColor   = engine.MarkerXColor
	ModelMarkerYColor   = engine.MarkerYColor
	ModelMarkerZColor   = engine.MarkerZColor
	ModelMarkerFwdColor = engine.MarkerForwardColor
)

func PickedMarker(index int) *int {
	if index < 0 {
		return nil
	}
	return &index
}

func PickedBone(index int) *int {
	if index < 0 {
		return nil
	}
	return &index
}

var CameraAxisColor = [3][4]float64{
	ModelMarkerXColor, ModelMarkerYColor, ModelMarkerZColor,
}

func CameraAxesAngles(opts RenderOptions) [3]float64 {
	return engine.AxisAngles(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch})
}

func CameraOrbitDirection(opts RenderOptions) fauxgl.Vector {
	return engine.OrbitDirection(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch})
}

func CameraViewDirection(opts RenderOptions) fauxgl.Vector {
	return engine.ViewDirection(engine.Camera{Yaw: opts.Yaw, Pitch: opts.Pitch})
}

func CameraAxesLayout(th *material.Theme, opts RenderOptions, draw bool) layout.Widget {
	camera := engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}
	return viewer.CameraAxesLayout(th, camera, CameraAxisColor, ModelMarkerFwdColor, draw)
}

func ModelOverlayHalfWidth(opts RenderOptions, pixels float64) float64 {
	return engine.OverlayHalfWidth(engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}}, pixels)
}

func ModelOverlayLines(model *whm.Model, pose WhmPose, opts RenderOptions) []ModelOverlayLine {
	if model == nil {
		return nil
	}
	eo := engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}, Skeleton: opts.Skeleton, Marker: opts.Marker, PickedBone: opts.PickedBone, Selection: opts.SelectionIndicator}
	return engine.OverlayLines(model, pose, eo)
}

func ModelMarkerOverlayLines(model *whm.Model, pose WhmPose, opts RenderOptions) []ModelOverlayLine {
	if model == nil {
		return nil
	}
	eo := engine.OverlayOptions{Camera: engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	}, Marker: opts.Marker}
	return engine.MarkerOverlayLines(model, pose, eo)
}

// Vec3From converts the format's float array to the renderer's vector type.
//
// The texture bridge also lives here: callers keep using the root helper while
// image sampling itself belongs to internal/3d_engine.
type ModelTexture = engine.Texture

func NewModelTexture(img image.Image) fauxgl.Texture { return engine.NewTexture(img) }

func Vec3From(v [3]float32) fauxgl.Vector {
	return fauxgl.Vector{X: float64(v[0]), Y: float64(v[1]), Z: float64(v[2])}
}

// BoneInRange is retained for the root's renderer diagnostics; the WHM
// implementation lives with the model package.
func BoneInRange(bone int32, count int) bool {
	return whm.BoneInRange(bone, count)
}

// SkinnedVertex keeps the renderer-facing WhmModel API while skinning is
// implemented next to whm.Model.
func SkinnedVertex(m *whm.Model, pose WhmPose, v *whm.Vertex) (fauxgl.Vector, fauxgl.Vector) {
	if m == nil {
		return fauxgl.Vector{}, fauxgl.Vector{}
	}
	return whm.SkinnedVertex(m, pose, v)
}

// QuatMatrix is shared by the viewer's key editor and delegates the actual
// conversion to the WHM package.
func QuatMatrix(q [4]float32) fauxgl.Matrix { return whm.QuatMatrix(q) }

// RenderOptions is the app-side render request. The engine owns the common
// render state; only the FX scene and material types remain app-specific.
type RenderOptions struct {
	engine.RenderOptions
	FX                  *fxGpuScene
	GroundImage         *image.RGBA
	Ground              bool
	MaterialOverrides   map[string]WhmMaterial
	MaterialAliases     map[string]string
	viewOverride        *fauxgl.Matrix
	AnimatedEntities    []sgb.AnimatedEntity
	SgbSelectedEntities map[int]bool
	SgbEntityMeshIDs    map[string]int
	SelectionIndicator  *engine.SelectionIndicator
}

const (
	ModelQualityDraft          = 0
	ModelQualityFinal          = 1
	ModelMarkerAxisWorldLength = engine.MarkerAxisWorldLength
)

// MeshAlphaFor is how much of the named mesh this frame draws, 1 for a mesh
// the clip says nothing about.
func MeshAlphaFor(opts RenderOptions, name string) float32 {
	return engine.MeshAlpha(opts.MeshAlpha, name)
}

// MeshAlphaByte is an alpha as the byte a tint carries. It is rounded to
// nearest rather than truncated, which is what keeps a fully faded mesh fully
// invisible and a whole one exactly whole: a hardware premultiply of 254/255
// leaves a mesh one step short of opaque, and a truncated 0.999 is 0.9978,
// which shows up as a mesh that never quite reaches full colour.
func MeshAlphaByte(alpha float32) uint8 { return engine.MeshAlphaByte(alpha) }

// UVMapping preserves the app's established UV settings interface while the
// shared mapping implementation lives in internal/3d_engine.
type UVMapping = engine.UVMapping

func UVIdentityMapping() UVMapping { return UVMapping{Scale: 1} }

func RenderUVFor(opts RenderOptions, material string) UVMapping {
	if m, ok := opts.UVPerMaterial[material]; ok && m != nil {
		return *m
	}
	return opts.UVDefault
}

// UVTransform delegates the renderer's UV mapping to the 3D engine package.
func BuildViewMatrix(opts RenderOptions) fauxgl.Matrix {
	return engine.ViewMatrix(engine.Camera{Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch, Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov})
}

func UVTransform(u, v float32, m UVMapping) (float32, float32) {
	return engine.TransformUV(u, v, m)
}

// UVExtentOf and ModelHasUVs are the viewer's side of ModelUVExtent: the range
// as the four floats the panel prints, and whether there was one to print.
func UVExtentOf(model *whm.Model) [4]float32 {
	minU, minV, maxU, maxV, ok := ModelUVExtent(model)
	if !ok {
		return [4]float32{}
	}
	return [4]float32{minU, minV, maxU, maxV}
}

func ModelHasUVs(model *whm.Model) bool {
	_, _, _, _, ok := ModelUVExtent(model)
	return ok
}

// ModelUVExtent is the range the model's own texture coordinates cover, which
// is the first thing worth knowing about a map that is landing in the wrong
// place. A model whose coordinates run from 0 to 1 uses its map once; one whose
// V runs from −1.4 to 1 tiles it about two and a half times, and an offset
// applied to the second tiling lands in the wrong place for a reason that has
// nothing to do with the offset.
//
// It is measured rather than read out of a field because there is no such
// field: a .whm stores a coordinate per vertex and nothing about what those
// coordinates were meant to span.
func ModelUVExtent(model *whm.Model) (minU, minV, maxU, maxV float32, ok bool) {
	if model == nil {
		return engine.UVBounds(nil)
	}
	return engine.UVBounds(model)
}

// WhmMaterial is the resolved material of one mesh: a diffuse map and a tint
// that multiplies it.
type WhmMaterial = engine.Material

// RenderWhmModel adapts the main package's combined UI/GPU options to the
// standalone software renderer in internal/3d_engine.
func RenderWhmModel(model *whm.Model, pose WhmPose, opts RenderOptions) *image.RGBA {
	var modelData *whm.Model
	if model != nil {
		modelData = model
	}
	engineOpts := opts.RenderOptions
	if opts.Ground && opts.GroundImage != nil {
		engineOpts.Texture = NewModelTexture(opts.GroundImage)
		engineOpts.Ground = true
	}
	engineOpts.MaterialOverrides = opts.MaterialOverrides
	engineOpts.MaterialAliases = opts.MaterialAliases
	engineOpts.ViewOverride = opts.viewOverride
	engineOpts.Selection = opts.SelectionIndicator
	return engine.Render(modelData, pose, engineOpts)
}

// NewCameraState creates the initial orbit-camera frame for a model without
// exposing any application UI state to the viewer package.
func NewCameraState(m *whm.Model, yaw, pitch, fov float64) viewer.CameraState {
	var modelData *whm.Model
	if m != nil {
		modelData = m
	}
	center, distance := engine.ModelCenter(modelData)
	return viewer.CameraState{
		Center: center, Distance: distance, FitDistance: distance,
		Yaw: yaw, Pitch: pitch, FOV: fov,
	}
}

// RgbaWhite is the default tint for a textured material.
var RgbaWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
