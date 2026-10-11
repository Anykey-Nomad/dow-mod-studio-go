package main

import (
	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/whm"
	"image"
	"image/color"

	"github.com/fogleman/fauxgl"
)

// Software rendering of a decoded .whm model.

// vec3From converts the format's float array to the renderer's vector type.
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

// Software rendering of a decoded .whm model.
//
// Rendering runs on the CPU through fauxgl, which keeps the build free of CGO
// and of any GPU driver dependency. Frames are produced off the UI goroutine
// and only when the camera or the animation actually changes, so a viewer at
// rest costs nothing.

// --- rendering --------------------------------------------------------------

// RenderOptions describes one frame.
type RenderOptions struct {
	Width, Height int
	Yaw           float64 // radians around the up axis, which is Z
	Pitch         float64 // radians up from the horizon, clamped away from the poles
	Distance      float64
	Target        fauxgl.Vector
	Fov           float64 // vertical field of view in degrees
	Ambient       float64
	Diffuse       float64
	Light         fauxgl.Vector
	Texture       fauxgl.Texture
	BaseColor     color.RGBA
	Quality       int // 0 = draft while dragging, 1 = final
	Skeleton      bool
	// Marker is the marker the markers list has picked, or nil for none. Its
	// frame and forward arrow are drawn with the skeleton, on whichever
	// renderer draws the frame, which is what makes the picked attachment
	// point visible on the hardware path too.
	//
	// It is a pointer rather than an index so that a frame nobody asked
	// about carries no marker: an index cannot say "none" without a
	// sentinel, and a zero would silently mean the model's first marker.
	Marker *int
	// PickedBone is the bone the bones list has picked, or nil for none.
	// Its segment is drawn in the pick colour on top of the skeleton, and
	// a root bone gets a small cross instead, since it has no segment of
	// its own. The overlay is built by modelOverlayLines, so the Vulkan
	// renderer draws it in the same pass as the rest of the overlay.
	PickedBone *int
	// UVDefault is how a map is placed over the model, and UVPerMaterial
	// replaces that for the materials named in it.
	//
	// A model can carry more than one map, and they do not have to be wrong
	// in the same way. The Obelisk is the case that started this: turning its
	// map a half turn and mirroring U puts it right, and a second map on the
	// same model then comes out mirrored instead, because a setting for the
	// whole model can only be wrong for one of its textures at a time. So the
	// mapping is per material, and a material with no entry of its own is
	// drawn with the default — which is what keeps the common case of one map
	// behaving the way it always did.
	UVDefault     uvMapping
	UVPerMaterial map[string]*uvMapping

	// MaterialOverrides replaces the flat base colour per mesh, keyed by the
	// same material path the .whm stores. Meshes without an entry keep
	// BaseColor, which is what an untextured model wants.
	MaterialOverrides map[string]WhmMaterial
	// MeshAlpha is how much of each named mesh this frame draws, for the
	// clips that hide or fade part of a model: a monolith's phase_out holds
	// its pieces back and the Obelisk fades its body in as it builds. A
	// mesh with no entry is drawn whole.
	//
	// It is keyed by mesh name rather than index because that is what an
	// animation's visibility track carries, and the frame that draws it is
	// told which clip and how far into it the playhead is.
	MeshAlpha map[string]float32
	// FX is the frame's particles, when the effects are playing. A
	// hardware renderer draws them as billboards inside the same pass as
	// the model; the software path ignores them and draws the overlay it
	// rasterises itself.
	FX *fxGpuScene

	// viewOverride replaces the orbit camera with a caller supplied transform,
	// which the tests use to check a specific orientation.
	viewOverride *fauxgl.Matrix
}

// meshAlphaFor is how much of the named mesh this frame draws, 1 for a mesh
// the clip says nothing about.
func meshAlphaFor(opts RenderOptions, name string) float32 {
	if a, ok := opts.MeshAlpha[name]; ok {
		return a
	}
	return 1
}

// meshAlphaByte is an alpha as the byte a tint carries. It is rounded to
// nearest rather than truncated, which is what keeps a fully faded mesh fully
// invisible and a whole one exactly whole: a hardware premultiply of 254/255
// leaves a mesh one step short of opaque, and a truncated 0.999 is 0.9978,
// which shows up as a mesh that never quite reaches full colour.
func meshAlphaByte(alpha float32) uint8 {
	if alpha <= 0 {
		return 0
	}
	if alpha >= 1 {
		return 255
	}
	return uint8(float64(alpha)*255 + 0.5)
}

// uvMapping is how one texture is placed over the model it is drawn on: a
// mirror, a scale, a number of quarter turns and a shift. The zero value is
// not usable — a scale of zero would collapse the map to a line — so a
// mapping is made with uvIdentityMapping or by copying another one.
type uvMapping struct {
	rotate int
	mirror bool
	offset [2]float32
	scale  float32
}

// uvIdentityMapping is the map as the file has it: no turn, no mirror, no
// shift, and a scale of one.
func uvIdentityMapping() uvMapping { return uvMapping{scale: 1} }

// renderUVFor is the mapping a material is drawn with: its own if it has one,
// and the frame's default otherwise.
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
	return engine.TransformUV(u, v, engine.UVMapping{
		Rotate: m.rotate, Mirror: m.mirror, Offset: m.offset, Scale: m.scale,
	})
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
	if model == nil || len(model.Vertices) == 0 {
		return 0, 0, 0, 0, false
	}
	minU, minV = model.Vertices[0].UV[0], model.Vertices[0].UV[1]
	maxU, maxV = minU, minV
	for i := range model.Vertices {
		u, v := model.Vertices[i].UV[0], model.Vertices[i].UV[1]
		if u < minU {
			minU = u
		}
		if u > maxU {
			maxU = u
		}
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	return minU, minV, maxU, maxV, true
}

// WhmMaterial is the resolved material of one mesh: a diffuse map and a tint
// that multiplies it.
type WhmMaterial struct {
	Texture fauxgl.Texture
	Color   color.RGBA
}

const (
	// modelQualityDraft trades shading detail for latency while the camera
	// moves; modelQualityFinal is used for the frame that stays on screen.
	modelQualityDraft = 0
	modelQualityFinal = 1
)

// RenderWhmModel adapts the main package's combined UI/GPU options to the
// standalone software renderer in internal/3d_engine.
func RenderWhmModel(model *WhmModel, pose WhmPose, opts RenderOptions) *image.RGBA {
	if model == nil {
		return engine.Render(nil, pose, engine.RenderOptions{})
	}
	var uv map[string]*engine.UVMapping
	if opts.UVPerMaterial != nil {
		uv = make(map[string]*engine.UVMapping, len(opts.UVPerMaterial))
		for key, value := range opts.UVPerMaterial {
			if value == nil {
				uv[key] = nil
				continue
			}
			uv[key] = &engine.UVMapping{Rotate: value.rotate, Mirror: value.mirror, Offset: value.offset, Scale: value.scale}
		}
	}
	return engine.Render(&model.Model, pose, engine.RenderOptions{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
		Ambient: opts.Ambient, Diffuse: opts.Diffuse, Light: opts.Light,
		Texture: opts.Texture, BaseColor: opts.BaseColor, Quality: opts.Quality,
		Skeleton: opts.Skeleton, Marker: opts.Marker, PickedBone: opts.PickedBone,
		UVDefault:     engine.UVMapping{Rotate: opts.UVDefault.rotate, Mirror: opts.UVDefault.mirror, Offset: opts.UVDefault.offset, Scale: opts.UVDefault.scale},
		UVPerMaterial: uv, MaterialOverrides: convertMaterials(opts.MaterialOverrides),
		MeshAlpha: opts.MeshAlpha, ViewOverride: opts.viewOverride,
	})
}

func convertMaterials(in map[string]WhmMaterial) map[string]engine.Material {
	if in == nil {
		return nil
	}
	out := make(map[string]engine.Material, len(in))
	for key, value := range in {
		out[key] = engine.Material{Texture: value.Texture, Color: value.Color}
	}
	return out
}

// ModelCenter returns the midpoint of the model bounds and a distance that
// frames the whole model, which is what the viewer needs to frame a new model.
func (m *WhmModel) ModelCenter() (fauxgl.Vector, float64) {
	if m == nil || len(m.Vertices) == 0 {
		return fauxgl.Vector{}, 1
	}
	min, max := vec3From(m.BoundMin), vec3From(m.BoundMax)
	center := min.Add(max).DivScalar(2)
	size := max.Sub(min).Length()
	if size <= 1e-6 {
		size = 1
	}
	return center, size * 1.4
}

// rgbaWhite is the default tint for a textured material.
var rgbaWhite = color.RGBA{R: 255, G: 255, B: 255, A: 255}
