package main

import (
	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/whm"
	"image"
	"image/color"
	"image/draw"

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

// modelShader is fauxgl's per vertex transform plus a Lambert term over an
// optional diffuse texture. Lighting happens in model space, so the light
// direction never has to follow the camera.
type modelShader struct {
	view    fauxgl.Matrix
	texture fauxgl.Texture
	light   fauxgl.Vector
	ambient float64
	diffuse float64
	base    fauxgl.Color
	final   bool
}

func (s *modelShader) Vertex(v fauxgl.Vertex) fauxgl.Vertex {
	v.Output = s.view.MulPositionW(v.Position)
	return v
}

func (s *modelShader) Fragment(v fauxgl.Vertex) fauxgl.Color {
	c := s.base
	if s.texture != nil {
		t := s.texture.Sample(v.Texture.X, v.Texture.Y)
		c.R *= t.R
		c.G *= t.G
		c.B *= t.B
	}
	// Draft frames are unlit: shading every pixel costs more than it is worth
	// while the camera is moving.
	lambert := 1.0
	if s.final {
		lambert = 0
		if d := v.Normal.Normalize().Dot(s.light); d > 0 {
			lambert = d
		}
	}
	f := s.ambient + s.diffuse*lambert
	if f > 1 {
		f = 1
	}
	return fauxgl.Color{R: c.R * f, G: c.G * f, B: c.B * f, A: c.A}
}

// RenderWhmModel rasterises one frame of a posed model. It is safe to call from
// a background goroutine: it touches only its arguments.
func RenderWhmModel(model *WhmModel, pose WhmPose, opts RenderOptions) *image.RGBA {
	if model == nil || opts.Width < 1 || opts.Height < 1 {
		return image.NewRGBA(image.Rect(0, 0, maxInt(opts.Width, 0), maxInt(opts.Height, 0)))
	}
	if opts.Fov <= 0 {
		opts.Fov = 45
	}
	if opts.Ambient == 0 && opts.Diffuse == 0 {
		opts.Ambient, opts.Diffuse = 0.28, 0.72
	}
	if opts.Light == (fauxgl.Vector{}) {
		// Key light from above and to the front, in the Z-up model space.
		opts.Light = fauxgl.Vector{X: 0.35, Y: -0.5, Z: 0.78}
	}
	if opts.Light.Length() > 0 {
		opts.Light = opts.Light.Normalize()
	}
	// A zero BaseColor would be fully transparent, and since the context
	// alpha blends, the model would be invisible.
	if opts.BaseColor == (color.RGBA{}) {
		opts.BaseColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}

	ctx := fauxgl.NewContext(opts.Width, opts.Height)
	ctx.ClearColorBufferWith(fauxgl.Color{R: 0.11, G: 0.12, B: 0.14, A: 1})
	ctx.ClearDepthBuffer()
	ctx.Wireframe = false
	// A .whm winds its triangles the way the model was authored: seen from
	// outside, counter clockwise. fauxgl measures the winding in normalised
	// device coordinates, where that is a positive signed area, so the outside
	// of a face is its front face. Taking the front face to be the other way
	// round culls the outside of every model and leaves the inside of its
	// shell on screen, which is what makes a building come out hollow.
	ctx.FrontFace = fauxgl.FaceCCW
	ctx.Cull = fauxgl.CullBack

	view := buildViewMatrix(opts)
	if opts.viewOverride != nil {
		view = *opts.viewOverride
	}
	shader := &modelShader{
		view:    view,
		texture: opts.Texture,
		light:   opts.Light,
		ambient: opts.Ambient,
		diffuse: opts.Diffuse,
		base:    rgbaToColor(opts.BaseColor),
		final:   opts.Quality >= modelQualityFinal,
	}
	ctx.Shader = shader

	// Per mesh materials: the shader is shared, so switching material only
	// means swapping its texture and base colour between draw calls.
	meshMaterial := func(mesh *WhmMesh) {
		mat, ok := opts.MaterialOverrides[mesh.Material]
		if !ok {
			shader.texture = nil
			shader.base = rgbaToColor(opts.BaseColor)
			return
		}
		shader.texture = mat.Texture
		shader.base = rgbaToColor(mat.Color)
	}
	if len(opts.MaterialOverrides) == 0 {
		meshMaterial = nil
	}

	// Skin once, then reuse the transformed vertex for every mesh that uses it.
	// The texture coordinates are not filled in here: they are per material,
	// so they are mapped per sub mesh below, from the mapping that sub mesh's
	// material is drawn with.
	skinned := make([]fauxgl.Vertex, len(model.Vertices))
	for i := range model.Vertices {
		pos, normal := whm.SkinnedVertex(&model.Model, pose, &model.Vertices[i])
		skinned[i] = fauxgl.Vertex{Position: pos, Normal: normal}
	}

	// mapped is one vertex with its texture coordinates put in the way the
	// current material's mapping puts them. It is filled per corner rather
	// than per vertex because the same vertex is shared by sub meshes drawn
	// with different maps, which is the whole reason this is per material.
	mapped := func(vert fauxgl.Vertex, index uint32, m uvMapping) fauxgl.Vertex {
		u, w := uvTransform(model.Vertices[index].UV[0], model.Vertices[index].UV[1], m)
		vert.Texture = fauxgl.Vector{X: float64(u), Y: float64(w)}
		return vert
	}

	tri := &fauxgl.Triangle{}
	for i := range model.Meshes {
		mesh := &model.Meshes[i]
		// A mesh the clip holds back is not drawn at all, and one it fades
		// is drawn through the alpha in its own colour: the software
		// renderer blends what it draws, so the fade is a real one rather
		// than a dimmed mesh standing where a translucent one should be.
		//
		// The colour is set for every mesh, not only the fading ones: the
		// shader is shared, so a mesh drawn solid after a fading one would
		// otherwise be drawn with whatever that one left behind.
		alpha := meshAlphaFor(opts, mesh.Name)
		if alpha <= 0 {
			continue
		}
		if meshMaterial != nil {
			meshMaterial(mesh)
		} else {
			shader.base = rgbaToColor(opts.BaseColor)
		}
		if alpha < 1 {
			base := shader.base
			base.A *= float64(alpha)
			shader.base = base
		}
		mapping := renderUVFor(opts, mesh.Material)
		idx := mesh.Indices
		for i := 0; i+2 < len(idx); i += 3 {
			a, b, c := idx[i], idx[i+1], idx[i+2]
			if int(a) >= len(skinned) || int(b) >= len(skinned) || int(c) >= len(skinned) {
				continue
			}
			tri.V1 = mapped(skinned[a], a, mapping)
			tri.V2 = mapped(skinned[b], b, mapping)
			tri.V3 = mapped(skinned[c], c, mapping)
			ctx.DrawTriangle(tri)
		}
	}

	drawModelOverlay(ctx, model, pose, opts, view)

	// fauxgl hands back an *image.NRGBA, so convert rather than assert.
	src := ctx.Image()
	if b, ok := src.(*image.NRGBA); ok {
		return nrgbaToRGBA(b)
	}
	out := image.NewRGBA(image.Rect(0, 0, opts.Width, opts.Height))
	draw.Draw(out, out.Bounds(), src, src.Bounds().Min, draw.Src)
	return out
}

// drawModelOverlay draws the frame's debug overlay — the skeleton and the
// picked marker — on the software path. It reads the same segment list the
// hardware renderer draws, so the two cannot drift apart, and fauxgl's line
// width is in pixels, which is the same measure the segments' world widths
// were chosen against.
func drawModelOverlay(ctx *fauxgl.Context, model *WhmModel, pose WhmPose, opts RenderOptions, view fauxgl.Matrix) {
	segments := modelOverlayLines(model, pose, opts)
	if len(segments) == 0 {
		return
	}
	prevShader := ctx.Shader
	prevWidth := ctx.LineWidth
	line := &fauxgl.Line{}
	for _, s := range segments {
		ctx.Shader = &modelShader{
			view:    view,
			light:   opts.Light,
			ambient: 1,
			base:    fauxgl.Color{R: s.R, G: s.G, B: s.B, A: s.A},
			final:   false,
		}
		// fauxgl measures a line's thickness in pixels, so the segment's
		// world width is turned back into the pixels it was derived from.
		// A screen narrower than a pixel would draw nothing at all.
		px := s.HalfWidth * 2 / modelOverlayHalfWidth(opts, 1)
		if px < 1 {
			px = 1
		}
		ctx.LineWidth = px
		line.V1 = fauxgl.Vertex{Position: s.From, Normal: fauxgl.Vector{X: 0, Y: 0, Z: 1}}
		line.V2 = fauxgl.Vertex{Position: s.To, Normal: fauxgl.Vector{X: 0, Y: 0, Z: 1}}
		ctx.DrawLine(line)
	}
	ctx.Shader = prevShader
	ctx.LineWidth = prevWidth
}

// buildViewMatrix turns the orbit camera into a clip space transform.
func buildViewMatrix(opts RenderOptions) fauxgl.Matrix {
	return engine.ViewMatrix(engine.Camera{
		Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch,
		Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov,
	})
}

// nrgbaToRGBA converts fauxgl's premultiplied-free NRGBA buffer to the RGBA
// layout the image widget in Gio expects.
func nrgbaToRGBA(src *image.NRGBA) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		row := src.Pix[y*src.Stride : y*src.Stride+b.Dx()*4]
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+b.Dx()*4], row)
	}
	return dst
}

func rgbaToColor(c color.RGBA) fauxgl.Color {
	return fauxgl.Color{
		R: float64(c.R) / 255,
		G: float64(c.G) / 255,
		B: float64(c.B) / 255,
		A: float64(c.A) / 255,
	}
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
