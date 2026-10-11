package engine

import (
	"image"
	"image/color"
	"image/draw"

	"dow-mod-studio/internal/whm"
	"github.com/fogleman/fauxgl"
)

// RenderOptions contains only the state needed by the CPU renderer.
type RenderOptions struct {
	Width, Height         int
	Yaw, Pitch, Distance  float64
	Target                fauxgl.Vector
	Fov, Ambient, Diffuse float64
	Light                 fauxgl.Vector
	Texture               fauxgl.Texture
	BaseColor             color.RGBA
	Quality               int
	Skeleton              bool
	Marker, PickedBone    *int
	UVDefault             UVMapping
	UVPerMaterial         map[string]*UVMapping
	MaterialOverrides     map[string]Material
	MeshAlpha             map[string]float32
	ViewOverride          *fauxgl.Matrix
}

// Material is a resolved diffuse image and its tint.
type Material struct {
	Texture fauxgl.Texture
	Color   color.RGBA
}

func renderUVFor(opts RenderOptions, material string) UVMapping {
	if m, ok := opts.UVPerMaterial[material]; ok && m != nil {
		return *m
	}
	return opts.UVDefault
}

func overlayOptions(opts RenderOptions) OverlayOptions {
	return OverlayOptions{Camera: Camera{Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch, Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov}, Skeleton: opts.Skeleton, Marker: opts.Marker, PickedBone: opts.PickedBone}
}

func meshAlphaFor(opts RenderOptions, name string) float32 {
	if a, ok := opts.MeshAlpha[name]; ok {
		return a
	}
	return 1
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

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

// Render rasterises one frame of a posed model. It is safe to call from
// a background goroutine: it touches only its arguments.
func Render(model *whm.Model, pose whm.Pose, opts RenderOptions) *image.RGBA {
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

	view := ViewMatrix(overlayOptions(opts).Camera)
	if opts.ViewOverride != nil {
		view = *opts.ViewOverride
	}
	shader := &modelShader{
		view:    view,
		texture: opts.Texture,
		light:   opts.Light,
		ambient: opts.Ambient,
		diffuse: opts.Diffuse,
		base:    rgbaToColor(opts.BaseColor),
		final:   opts.Quality >= 1,
	}
	ctx.Shader = shader

	// Per mesh materials: the shader is shared, so switching material only
	// means swapping its texture and base colour between draw calls.
	meshMaterial := func(mesh *whm.Mesh) {
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
		pos, normal := whm.SkinnedVertex(model, pose, &model.Vertices[i])
		skinned[i] = fauxgl.Vertex{Position: pos, Normal: normal}
	}

	// mapped is one vertex with its texture coordinates put in the way the
	// current material's mapping puts them. It is filled per corner rather
	// than per vertex because the same vertex is shared by sub meshes drawn
	// with different maps, which is the whole reason this is per material.
	mapped := func(vert fauxgl.Vertex, index uint32, m UVMapping) fauxgl.Vertex {
		u, w := TransformUV(model.Vertices[index].UV[0], model.Vertices[index].UV[1], m)
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

	drawSoftwareOverlay(ctx, model, pose, opts, view)

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
func drawSoftwareOverlay(ctx *fauxgl.Context, model *whm.Model, pose whm.Pose, opts RenderOptions, view fauxgl.Matrix) {
	segments := OverlayLines(model, pose, overlayOptions(opts))
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
		px := s.HalfWidth * 2 / OverlayHalfWidth(overlayOptions(opts), 1)
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
