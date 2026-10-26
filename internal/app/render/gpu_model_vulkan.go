//go:build windows || linux

package render

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/whm"
	"dow-mod-studio/internal/sgb"
	_ "embed"

	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/gpu"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"github.com/fogleman/fauxgl"
)

// Hardware rendering of a decoded .whm through Gio's Vulkan backend — the
// designed specifically for Vulkan, with its frame and draw data laid out for
// Vulkan uniform buffers and push constants:
//
//   - the per frame block (view projection, light, bones) is a uniform
//     buffer read by both shader stages;
//   - the per draw block (shade term, tint, texture mapping) is a push
//     constant block, so changing a tint costs no upload;
//   - the shaders are SPIR-V compiled from model3d_vk.vert and
//     model3d_vk.frag.
//
// The layout of the uniform block is std140 as the shader declares it:
// mat4 ViewProj at byte 0, vec4 Light at 64, mat4 Bones[256] at 80. The
// frame and push data are kept apart, which is why writeFrame and writePush
// exist instead of one writeConstants.

//go:embed shaders/model3d_vk.vert.spv
var modelVkVertSPIRV []byte

//go:embed shaders/model3d_vk.frag.spv
var modelVkFragSPIRV []byte

//go:embed shaders/model3d_vk_ground.vert.spv
var modelVkGroundVertSPIRV []byte

const modelGpuVkGroundVertexStride = 32

// Model coordinates use Z as the vertical axis; the ground plane is Z=0.
type modelGpuVkGroundVertex struct {
	position [3]float32
	normal   [3]float32
	uv       [2]float32
}

// modelGpuVkFrameFloats is the size of the frame uniform block in floats:
// 16 for ViewProj, 4 for Light, and 16 per bone.
const modelGpuVkFrameFloats = 16 + 4 + modelGpuMaxBones*16

// modelGpuVkPushFloats is the size of the push constant block in floats:
// Shade, Tint and UV, one vec4 each.
const modelGpuVkPushFloats = 12

// modelGpuVkDraw is one sub mesh: the vertex buffer holding its corners,
// the index buffer that walks its triangles, and the material whose texture
// it samples. Each sub mesh owns both buffers because a draw binds its own
// and has no offset of its own to share with the next one.
type modelGpuVkDraw struct {
	vertices     *gpu.VulkanBuffer
	indexes      *gpu.VulkanBuffer
	count        int
	material     string
	materialPath string
	// name is the sub mesh this draw is, which is what a clip's visibility
	// track is named after. The geometry never changes, so the name is
	// carried here rather than looked up by index every frame.
	name string
	// centre is where the sub mesh sits, averaged over the vertices it
	// indexes. It is what a fading mesh is sorted by, so that a model whose
	// two translucent parts overlap shows the near one through the far one
	// rather than whichever happened to come first in the file.
	centre [3]float64
}

// modelGpuVkMesh is the GPU copy of a model's geometry. It is built once per
// model and explicitly retired when mutable scene geometry changes.
type modelGpuVkMesh struct {
	draws []modelGpuVkDraw
}

// modelGpuVkResolvedDraw is one sub mesh with its material already settled:
// the texture uploaded, the tint and the mapping chosen. Resolving happens
// before the pass begins because an upload needs a command buffer of its own,
// and while a pass is recording the only command buffer in flight is the
// pass's — the Vulkan backend refuses an upload there rather than let it
// end the pass early, which on a driver without validation is a crash on
// the first textured frame.
type modelGpuVkResolvedDraw struct {
	draw    modelGpuVkDraw
	tex     *gpu.VulkanTexture
	tint    color.RGBA
	mapping UVMapping
	// alpha is how much of this sub mesh the clip lets through at this frame,
	// and depth is how far its centre is from the camera. A draw below one
	// goes through the blended pipeline, after every solid one. Texture
	// transparency is not routed here: the fragment shader alpha-tests it
	// in the solid pipeline, so geometry keeps its depth writes.
	alpha float32
	depth float64
}

// modelGpuVkTarget is the offscreen colour and depth pair a frame is drawn
// into. It is recreated when the panel changes size.
type modelGpuVkTarget struct {
	color *gpu.VulkanTexture
	depth *gpu.VulkanTexture
	size  image.Point
}

// modelGpuVkTexture is one material's diffuse map, still in the block
// compressed form the .rsh file stores it in.
type modelGpuVkTexture struct {
	tex    *gpu.VulkanTexture
	mips   []raster.RshMip
	format int32
	data   []byte
}

// modelGpuVkRenderer holds the device resources shared by every 3D preview:
// the pipeline, the uniform buffer, and the cache of uploaded models and
// textures.
type modelGpuVkRenderer struct {
	vk             *gpu.VulkanPass
	pipeline       *gpu.VulkanPipeline
	groundPipeline *gpu.VulkanPipeline
	groundVertices *gpu.VulkanBuffer
	groundTexture  *gpu.VulkanTexture
	groundSource   *image.RGBA
	groundWidth    int
	groundHeight   int
	groundCenter   [2]float64
	// blend is the same shader pair drawing the same way, with the colour
	// target mixing rather than replacing and with depth left unwritten, so
	// a mesh a clip is fading shows what is behind it instead of hiding it.
	// Texture cutouts do not come here: the fragment shader discards them
	// against Shade.z, which keeps even a cutout mesh on the solid pipeline
	// with its depth writes intact.
	blend        *gpu.VulkanPipeline
	uniforms     *gpu.VulkanBuffer
	uniformsNext *gpu.VulkanBuffer
	white        *gpu.VulkanTexture

	meshes map[*whm.Model]*modelGpuVkMesh
	// dirtyMeshes defers buffer retirement until the next Render call, which
	// begins after Gio has submitted the layout frame that may still reference
	// the current mesh's buffers.
	dirtyMeshes map[*whm.Model]bool
	// textures is keyed by material and holds both the texture and the
	// file bytes its mip slices point into.
	textures  map[string]*modelGpuVkTexture
	offscreen *modelGpuVkTarget
	// fx draws the particles of the model's effects, and is built the
	// first time a frame carries any.
	fx           *fxVkRenderer
	fxGroups     []fxVkGroup
	frame        []float32
	frameBuf     []byte
	uniformFrame int
	push         []float32
	pushBuf      []byte
	// resolved is the frame's list of sub meshes with their textures
	// settled, kept between frames so drawing a model does not make the
	// collector a per-frame cost. order is the same list as the sequence the
	// draws are issued in.
	resolved []modelGpuVkResolvedDraw
	order    []int
}

// NewModelGpuVkRenderer compiles the pipeline and the buffers a frame needs.
func NewModelGpuVkRenderer(v *gpu.VulkanPass) (*modelGpuVkRenderer, error) {
	// The two pipelines differ in nothing but how a fragment reaches the
	// target. The shader pair is the same, and the fragment shader writes
	// premultiplied colour, so a fully opaque draw blends to exactly what it
	// would have replaced.
	desc := gpu.VulkanPipelineDesc{
		Vertex:   modelVkVertSPIRV,
		Fragment: modelVkFragSPIRV,
		Stride:   modelGpuVertexStride,
		// Mirror the projection into Vulkan's downward Y in writeFrame;
		// this winding keeps the outside of models visible.
		Cull:       gpu.VulkanCullFront,
		DepthTest:  true,
		DepthWrite: true,
		PushSize:   modelGpuVkPushFloats * 4,
		Inputs: []gpu.VulkanInput{
			{Location: 0, Format: gpu.VulkanFormatFloat32x3, Offset: 0},
			{Location: 1, Format: gpu.VulkanFormatFloat32x3, Offset: 12},
			{Location: 2, Format: gpu.VulkanFormatFloat32x2, Offset: 24},
			{Location: 3, Format: gpu.VulkanFormatFloat32x4, Offset: 32},
			{Location: 4, Format: gpu.VulkanFormatFloat32x4, Offset: 48},
		},
	}
	p, err := v.NewPipeline(desc)
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}
	// The blended pipeline leaves depth alone: a mesh that is only half
	// there must not stop what is behind it from being drawn, and the draws
	// behind it are the ones that have not happened yet.
	desc.DepthWrite = false
	desc.Blend = gpu.VulkanBlendPremultiplied
	blend, err := v.NewPipeline(desc)
	if err != nil {
		p.Release()
		return nil, fmt.Errorf("blended pipeline: %w", err)
	}
	groundDesc := gpu.VulkanPipelineDesc{
		Vertex:   modelVkGroundVertSPIRV,
		Fragment: modelVkFragSPIRV,
		Stride:   modelGpuVkGroundVertexStride,
		Inputs: []gpu.VulkanInput{
			{Location: 0, Format: gpu.VulkanFormatFloat32x3, Offset: 0},
			{Location: 1, Format: gpu.VulkanFormatFloat32x3, Offset: 12},
			{Location: 2, Format: gpu.VulkanFormatFloat32x2, Offset: 24},
		},
		Cull:       gpu.VulkanCullNone,
		DepthTest:  true,
		DepthWrite: true,
		PushSize:   modelGpuVkPushFloats * 4,
	}
	groundPipeline, err := v.NewPipeline(groundDesc)
	if err != nil {
		blend.Release()
		p.Release()
		return nil, fmt.Errorf("ground pipeline: %w", err)
	}
	uniforms, err := v.NewBuffer(gpu.VulkanConstantBuffer, modelGpuVkFrameFloats*4, true)
	if err != nil {
		groundPipeline.Release()
		blend.Release()
		p.Release()
		return nil, fmt.Errorf("uniform buffer: %w", err)
	}
	uniformsNext, err := v.NewBuffer(gpu.VulkanConstantBuffer, modelGpuVkFrameFloats*4, true)
	if err != nil {
		uniforms.Release()
		groundPipeline.Release()
		blend.Release()
		p.Release()
		return nil, fmt.Errorf("second uniform buffer: %w", err)
	}
	// A one by one white map stands in for a missing material, so
	// untextured meshes need no second pipeline.
	white, err := v.NewTexture(gpu.VulkanTextureDesc{
		Width:     1,
		Height:    1,
		MipLevels: 1,
		Format:    gpu.VulkanFormatR8G8B8A8SRGB,
		Sampled:   true,
	})
	if err != nil {
		uniformsNext.Release()
		uniforms.Release()
		groundPipeline.Release()
		blend.Release()
		p.Release()
		return nil, fmt.Errorf("white texture: %w", err)
	}
	if err := white.UploadMip(0, []byte{0xff, 0xff, 0xff, 0xff}); err != nil {
		white.Release()
		uniforms.Release()
		groundPipeline.Release()
		blend.Release()
		p.Release()
		return nil, fmt.Errorf("white texture upload: %w", err)
	}
	if err := white.UploadDone(); err != nil {
		white.Release()
		uniforms.Release()
		groundPipeline.Release()
		blend.Release()
		p.Release()
		return nil, fmt.Errorf("white texture: %w", err)
	}
	return &modelGpuVkRenderer{
		vk:             v,
		pipeline:       p,
		groundPipeline: groundPipeline,
		blend:          blend,
		uniforms:       uniforms,
		uniformsNext:   uniformsNext,
		white:          white,
		meshes:         map[*whm.Model]*modelGpuVkMesh{},
		dirtyMeshes:    map[*whm.Model]bool{},
		textures:       map[string]*modelGpuVkTexture{},
		frame:          make([]float32, modelGpuVkFrameFloats),
		frameBuf:       make([]byte, modelGpuVkFrameFloats*4),
		push:           make([]float32, modelGpuVkPushFloats),
		pushBuf:        make([]byte, modelGpuVkPushFloats*4),
	}, nil
}

// retireMesh removes a model's cached device buffers only after queued draws
// can no longer reference them.
func (r *modelGpuVkRenderer) retireMesh(model *whm.Model) error {
	m, ok := r.meshes[model]
	if !ok {
		return nil
	}
	if r.vk == nil {
		delete(r.meshes, model)
		m.release()
		return nil
	}
	if err := r.vk.WaitForPreviousFrame(); err != nil {
		return err
	}
	delete(r.meshes, model)
	m.release()
	return nil
}

// forget drops the cached geometry of a model, which is what a closing tab
// needs.
func (r *modelGpuVkRenderer) Forget(model *whm.Model) {
	if err := r.retireMesh(model); err != nil {
		log.Printf("3D preview: wait before forgetting mesh: %v", err)
		return
	}
	delete(r.dirtyMeshes, model)
}

// geometryChanged marks in-place geometry edits for retirement at the start
// of the next Render. This method can run from pointer handling in the same
// Gio layout that recorded draws using the old buffers; freeing them here
// would leave the frame submitted after Layout with references to destroyed
// Vulkan buffers.
func (r *modelGpuVkRenderer) GeometryChanged(model *whm.Model) {
	if model == nil {
		return
	}
	if r.dirtyMeshes == nil {
		r.dirtyMeshes = make(map[*whm.Model]bool)
	}
	r.dirtyMeshes[model] = true
}

// release frees every device resource the renderer owns.
func (r *modelGpuVkRenderer) Release() {
	if r == nil {
		return
	}
	for _, m := range r.meshes {
		m.release()
	}
	r.meshes = nil
	r.dirtyMeshes = nil
	for _, t := range r.textures {
		if t.tex != nil {
			t.tex.Release()
		}
	}
	if r.offscreen != nil {
		r.offscreen.color.Release()
		r.offscreen.depth.Release()
	}
	r.white.Release()
	if r.groundPipeline != nil {
		r.groundPipeline.Release()
	}
	if r.groundTexture != nil {
		r.groundTexture.Release()
	}
	if r.groundVertices != nil {
		r.groundVertices.Release()
	}
	if r.fx != nil {
		r.fx.release()
	}
	r.uniforms.Release()
	r.uniformsNext.Release()
	r.blend.Release()
	r.pipeline.Release()
	r.vk.Release()
}

// release frees the buffers of one mesh.
func (m *modelGpuVkMesh) release() {
	if m == nil {
		return
	}
	for _, d := range m.draws {
		d.vertices.Release()
		d.indexes.Release()
	}
}

// mesh returns the GPU copy of a model, building it on first use.
func (r *modelGpuVkRenderer) mesh(model *whm.Model) (*modelGpuVkMesh, error) {
	if r.dirtyMeshes[model] {
		if err := r.retireMesh(model); err != nil {
			return nil, fmt.Errorf("retire changed mesh: %w", err)
		}
		delete(r.dirtyMeshes, model)
	}
	if m, ok := r.meshes[model]; ok {
		return m, nil
	}
	start := time.Now()
	m, err := r.buildMesh(model)
	if err != nil {
		return nil, err
	}
	log.Printf("3D preview: GPU mesh built in %v: %d verts, %d draws", time.Since(start), len(model.Vertices), len(m.draws))
	r.meshes[model] = m
	return m, nil
}

// buildMesh splits a model into draws, each carrying its own vertex buffer
// and a 16 bit index buffer over that buffer's own vertices. A sub mesh
// whose geometry would not fit a 16 bit index space is split into several
// draws, so the limit that once capped a whole model at 0xffff vertices is
// now a limit per draw — which is what lets a map scene carry hundreds of
// placed meshes.
func (r *modelGpuVkRenderer) buildMesh(model *whm.Model) (*modelGpuVkMesh, error) {
	draws := make([]modelGpuVkDraw, 0, len(model.Meshes))
	cleanup := func() {
		for _, d := range draws {
			d.vertices.Release()
			d.indexes.Release()
		}
	}
	for _, m := range model.Meshes {
		if len(m.Indices) == 0 {
			continue
		}
		for _, group := range modelVkIndexGroups(m.Indices, modelGpuVkMaxGroupVerts) {
			verts := make([]byte, len(group.local)*modelGpuVertexStride)
			put32 := func(at int, v float32) {
				binary.LittleEndian.PutUint32(verts[at:], math.Float32bits(v))
			}
			for i, global := range group.local {
				if int(global) >= len(model.Vertices) {
					cleanup()
					return nil, fmt.Errorf("mesh %q indexes vertex %d, out of range", m.Name, global)
				}
				v := &model.Vertices[global]
				at := i * modelGpuVertexStride
				put32(at, v.Pos[0])
				put32(at+4, v.Pos[1])
				put32(at+8, v.Pos[2])
				put32(at+12, v.Normal[0])
				put32(at+16, v.Normal[1])
				put32(at+20, v.Normal[2])
				put32(at+24, v.UV[0])
				put32(at+28, v.UV[1])
				for k := range 4 {
					bone, w := v.Bones[k], v.Weights[k]
					if bone < 0 || bone >= int32(modelGpuMaxBones) {
						w = 0
					}
					put32(at+32+4*k, float32(bone))
					put32(at+48+4*k, w)
				}
			}
			vbuf, err := r.vk.NewBuffer(gpu.VulkanVertexBuffer, len(verts), false)
			if err != nil {
				cleanup()
				return nil, fmt.Errorf("vertex buffer: %w", err)
			}
			if err := vbuf.Upload(verts); err != nil {
				vbuf.Release()
				cleanup()
				return nil, fmt.Errorf("vertex upload: %w", err)
			}
			idx := make([]byte, 0, len(group.idx)*2)
			for _, i := range group.idx {
				idx = binary.LittleEndian.AppendUint16(idx, uint16(i))
			}
			ibuf, err := r.vk.NewBuffer(gpu.VulkanIndexBuffer, len(idx), false)
			if err != nil {
				vbuf.Release()
				cleanup()
				return nil, fmt.Errorf("index buffer: %w", err)
			}
			if err := ibuf.Upload(idx); err != nil {
				ibuf.Release()
				vbuf.Release()
				cleanup()
				return nil, fmt.Errorf("index upload: %w", err)
			}
			draws = append(draws, modelGpuVkDraw{
				vertices:     vbuf,
				indexes:      ibuf,
				count:        len(group.idx),
				material:     m.Material,
				materialPath: m.MaterialPath,
				name:         m.Name,
				centre:       modelGroupCentre(model, group.local),
			})
		}
	}
	return &modelGpuVkMesh{draws: draws}, nil
}

// modelGpuVkMaxGroupVerts is how many vertices one draw may index; below the
// 0xffff a 16 bit index buffer can name.
const modelGpuVkMaxGroupVerts = 60000

// modelVkIndexGroup is one draw's worth of a sub mesh: the model vertices it
// uses, in first-use order, and the triangle indices rebased onto that list.
type modelVkIndexGroup struct {
	local []uint32
	idx   []uint32
}

// modelVkIndexGroups walks a sub mesh's triangles and cuts the index list
// into groups of at most maxVerts vertices each. The cut lands on a triangle
// boundary, so no group ever holds a partial face.
func modelVkIndexGroups(indices []uint32, maxVerts int) []modelVkIndexGroup {
	if maxVerts < 3 {
		maxVerts = 3
	}
	var out []modelVkIndexGroup
	var cur modelVkIndexGroup
	slot := make(map[uint32]uint32)
	flush := func() {
		if len(cur.idx) == 0 {
			return
		}
		out = append(out, cur)
		cur = modelVkIndexGroup{}
		slot = make(map[uint32]uint32)
	}
	for i := 0; i+2 < len(indices); i += 3 {
		if len(cur.local)+3 > maxVerts {
			flush()
		}
		for k := 0; k < 3; k++ {
			g := indices[i+k]
			l, ok := slot[g]
			if !ok {
				l = uint32(len(cur.local))
				slot[g] = l
				cur.local = append(cur.local, g)
			}
			cur.idx = append(cur.idx, l)
		}
	}
	flush()
	return out
}

// modelGroupCentre is where a draw's geometry sits, averaged over the model
// vertices it references. It is the rest pose's centre, which is what a
// fading mesh is sorted by: a skinned part moves, but the pieces a clip
// fades are the rigid ones it assembles from, and their order is decided by
// where they were built.
//
// A vertex past the end of the vertex array is ignored rather than clamped:
// a draw that had been built already threw that vertex out, and this only
// ever reads what survived.
func modelGroupCentre(model *whm.Model, local []uint32) [3]float64 {
	var sum [3]float64
	n := 0
	for _, i := range local {
		if int(i) >= len(model.Vertices) {
			continue
		}
		p := model.Vertices[i].Pos
		sum[0] += float64(p[0])
		sum[1] += float64(p[1])
		sum[2] += float64(p[2])
		n++
	}
	if n == 0 {
		return sum
	}
	sum[0] /= float64(n)
	sum[1] /= float64(n)
	sum[2] /= float64(n)
	return sum
}

// modelVkDepth is how far a point is from the camera in the units clip space
// counts in: zero at the near plane, one at the far one. It is the dividing
// that makes the number comparable across the frame, since a point twice as
// far away is twice as deep in clip space but not twice as far in the world.
func modelVkDepth(view fauxgl.Matrix, p [3]float64) float64 {
	w := view.X30*p[0] + view.X31*p[1] + view.X32*p[2] + view.X33
	if w == 0 {
		return 0
	}
	z := (view.X20*p[0] + view.X21*p[1] + view.X22*p[2] + view.X23) / w
	if z < 0 {
		// Behind the eye, which happens to a mesh the camera has orbited
		// inside. It is drawn early rather than not drawn at all.
		return -z
	}
	return z
}

// modelVkDrawsAfter says whether one draw has to be issued after another.
//
// Solid geometry comes first, so that a fading mesh is measured against
// everything the frame has already put behind it. Among the fading ones the
// nearer comes last: what goes down first is what the one after it shows
// through, so the piece in front is the piece that has to be mixed on top.
//
// Two solid draws are left in the order they went in. Which of them wins is
// the depth test's business, not the sort's, and a frame whose clip hides
// nothing is every frame of most animations — that one keeps issuing its
// draws in the same sequence it always did.
func modelVkDrawsAfter(a, b *modelGpuVkResolvedDraw) bool {
	fa := a.alpha < 1 || a.draw.name == sgb.WaterMeshName
	fb := b.alpha < 1 || b.draw.name == sgb.WaterMeshName
	if fa != fb {
		return fa
	}
	if !fa {
		return false
	}
	if a.draw.name == sgb.WaterMeshName {
		return b.draw.name != sgb.WaterMeshName
	}
	if b.draw.name == sgb.WaterMeshName {
		return false
	}
	// Equal depth means the order is left alone too: swapping two meshes
	// that are the same distance away every frame is what makes a pair of
	// them flicker.
	return a.depth < b.depth
}

// modelVkOrder is the order the frame's draws are issued in: modelVkDrawsAfter
// applied to every pair.
//
// It is an insertion sort on purpose. The list is a model's sub meshes, which
// is a few dozen at the outside, and it runs on every frame of a playing
// animation; sort.SliceStable would box the comparison into an interface and
// give the collector something to do every frame to save a few hundred
// comparisons. The slice is the renderer's, so this allocates nothing.
func modelVkOrder(resolved []modelGpuVkResolvedDraw, order []int) []int {
	if cap(order) < len(resolved) {
		order = make([]int, len(resolved))
	}
	order = order[:len(resolved)]
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		// Walk this one left for as long as the draw it is passing has to
		// come after it. Two draws neither of which has to follow the
		// other stop the walk, which is what leaves the order alone.
		for j := i; j > 0 && modelVkDrawsAfter(&resolved[order[j-1]], &resolved[order[j]]); j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	return order
}

// rshVkFormat maps a .rsh surface format onto the sRGB variant of its block
// compressed Vulkan format, so the shader can sample it directly.
func rshVkFormat(format int32) (gpu.VulkanFormat, bool) {
	switch format {
	case 8:
		return gpu.VulkanFormatBC1SRGB, true
	case 10:
		return gpu.VulkanFormatBC2SRGB, true
	case 11:
		return gpu.VulkanFormatBC3SRGB, true
	}
	return 0, false
}

// materialTexture returns the texture for a material, uploading the mip chain
// of the .rsh file as it is stored the first time it is asked for. The file
// bytes are kept by the returned entry, because the mip slices point into them.
func (r *modelGpuVkRenderer) materialTexture(material string, data []byte) (*gpu.VulkanTexture, error) {
	if material == "" || len(data) == 0 {
		return r.white, nil
	}
	entry, ok := r.textures[material]
	if !ok {
		info, mips, err := raster.RshRawImage(data, 0)
		if err != nil {
			// A material that cannot be read is not fatal: the mesh is drawn
			// with the base colour instead.
			log.Printf("3D preview: texture %q unreadable, using white: %v", material, err)
			r.textures[material] = &modelGpuVkTexture{}
			return r.white, nil
		}
		vkfmt, ok := rshVkFormat(info.Format)
		if !ok || len(mips) == 0 {
			log.Printf("3D preview: texture %q unsupported (format=%d mips=%d), using white", material, info.Format, len(mips))
			r.textures[material] = &modelGpuVkTexture{}
			return r.white, nil
		}
		// Uploaded as an sRGB texture, which is what a diffuse map is: the
		// shader lights linear values and Gio's blit puts the result back
		// through the same curve.
		tex, err := r.vk.NewTexture(gpu.VulkanTextureDesc{
			Width:     mips[0].Width,
			Height:    mips[0].Height,
			MipLevels: len(mips),
			Format:    vkfmt,
			Sampled:   true,
		})
		if err != nil {
			return nil, fmt.Errorf("texture %q: %w", material, err)
		}
		for i, mip := range mips {
			if err := tex.UploadMip(i, mip.Data); err != nil {
				tex.Release()
				return nil, fmt.Errorf("texture %q mip %d: %w", material, i, err)
			}
		}
		if err := tex.UploadDone(); err != nil {
			tex.Release()
			return nil, fmt.Errorf("texture %q: %w", material, err)
		}
		entry = &modelGpuVkTexture{tex: tex, mips: mips, format: info.Format, data: data}
		r.textures[material] = entry
		// The first blocks of the top level say whether the bytes about to
		// be sampled hold an image: two zero endpoints decode to solid
		// black, which is what a frame full of black texture looks like.
		head := ""
		if d := mips[0].Data; len(d) >= 32 {
			var sb strings.Builder
			for i := 0; i < 4; i++ {
				c0 := binary.LittleEndian.Uint16(d[i*8:])
				c1 := binary.LittleEndian.Uint16(d[i*8+2:])
				fmt.Fprintf(&sb, " %04x/%04x", c0, c1)
			}
			// Average brightness of the first endpoint of every block: a
			// real map lands in the tens or hundreds, an all-zero chain
			// lands on 0.
			blocks, sum := len(d)/8, 0
			for i := 0; i < blocks; i++ {
				c := binary.LittleEndian.Uint16(d[i*8:])
				r5 := (c >> 11) & 0x1f
				g6 := (c >> 5) & 0x3f
				b5 := c & 0x1f
				sum += int(r5)*255/31 + int(g6)*255/63 + int(b5)*255/31
			}
			avg := 0
			if blocks > 0 {
				avg = sum / (blocks * 3)
			}
			fmt.Fprintf(&sb, " avgC0Luma=%d blocks=%d", avg, blocks)
			head = ";" + sb.String()
		}
		log.Printf("3D preview: texture %q uploaded %dx%d format=%d mips=%d bytes=%d mips0=%d%s",
			material, mips[0].Width, mips[0].Height, info.Format, len(mips), len(data), len(mips[0].Data), head)
	}
	if entry.tex == nil {
		return r.white, nil
	}
	return entry.tex, nil
}

// materialImageTexture uploads a decoded in-memory diffuse image for a
// material whose .rsh does not exist — a terrain mesh built from a scenario's
// own relief is the first of those. Like the .rsh path it is cached by
// material name, and an entry that holds nothing reads as the white texture
// rather than failing the frame.
func (r *modelGpuVkRenderer) materialImageTexture(material string, img *image.RGBA) (*gpu.VulkanTexture, error) {
	if material == "" || img == nil {
		return r.white, nil
	}
	if entry, ok := r.textures[material]; ok {
		if entry.tex == nil {
			return r.white, nil
		}
		return entry.tex, nil
	}
	b := img.Bounds()
	if b.Empty() {
		r.textures[material] = &modelGpuVkTexture{}
		return r.white, nil
	}
	pixels := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(pixels, pixels.Bounds(), img, b.Min, draw.Src)
	tex, err := r.vk.NewTexture(gpu.VulkanTextureDesc{
		Width: b.Dx(), Height: b.Dy(), MipLevels: 1,
		Format: gpu.VulkanFormatR8G8B8A8SRGB, Sampled: true,
	})
	if err != nil {
		return nil, fmt.Errorf("texture %q: %w", material, err)
	}
	if err := tex.UploadMip(0, pixels.Pix); err != nil {
		tex.Release()
		return nil, fmt.Errorf("texture %q upload: %w", material, err)
	}
	if err := tex.UploadDone(); err != nil {
		tex.Release()
		return nil, fmt.Errorf("texture %q: %w", material, err)
	}
	r.textures[material] = &modelGpuVkTexture{tex: tex}
	log.Printf("3D preview: image texture %q uploaded %dx%d", material, b.Dx(), b.Dy())
	return tex, nil
}

// renderTarget returns the offscreen render target, recreated when the size
// changes.
func (r *modelGpuVkRenderer) renderTarget(size image.Point) (*modelGpuVkTarget, error) {
	size.X = max(1, size.X)
	size.Y = max(1, size.Y)
	if r.offscreen != nil && r.offscreen.size == size {
		return r.offscreen, nil
	}
	if r.offscreen != nil {
		// Gio may still be holding the texture the frame on screen was
		// painted from, so a recreate during a drag resize is worth
		// seeing in the log. The Vulkan texture stays alive until Gio
		// lets go of its copy.
		log.Printf("3D preview: render target %dx%d -> %dx%d", r.offscreen.size.X, r.offscreen.size.Y, size.X, size.Y)
		r.offscreen.color.Release()
		r.offscreen.depth.Release()
	}
	color, err := r.vk.NewTexture(gpu.VulkanTextureDesc{
		Width:        size.X,
		Height:       size.Y,
		MipLevels:    1,
		Format:       gpu.VulkanFormatR8G8B8A8SRGB,
		RenderTarget: true,
		Sampled:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("colour target: %w", err)
	}
	depth, err := r.vk.NewTexture(gpu.VulkanTextureDesc{
		Width:     size.X,
		Height:    size.Y,
		MipLevels: 1,
		Format:    gpu.VulkanFormatD32Float,
		Depth:     true,
	})
	if err != nil {
		color.Release()
		return nil, fmt.Errorf("depth target: %w", err)
	}
	r.offscreen = &modelGpuVkTarget{color: color, depth: depth, size: size}
	return r.offscreen, nil
}

// Render draws one frame and returns the texture Gio should paint. The caller
// passes it to paint.NewImageOpFromTexture, which hands the same GPU memory
// to Gio's blitter with nothing copied in between. rsh resolves a material
// name to raw bytes of a block-compressed .rsh file; decoded material images
// provide the fallback for compiled RTX and other texture formats.
//
// The pass is recorded on Gio's queue and left for Gio's immediately following
// window frame to submit. Queue ordering makes the blit sample the completed
// image without a CPU-side fence wait.
func (r *modelGpuVkRenderer) Render(
	model *whm.Model,
	pose WhmPose,
	opts RenderOptions,
	materials map[string]WhmMaterial,
	rsh func(material string) ([]byte, bool),
) (frame Frame, err error) {
	if model == nil {
		return nil, fmt.Errorf("no model")
	}
	if opts.Width < 1 || opts.Height < 1 {
		return nil, fmt.Errorf("frame is %d by %d", opts.Width, opts.Height)
	}
	mesh, err := r.mesh(model)
	if err != nil {
		return nil, err
	}
	target, err := r.renderTarget(image.Point{X: opts.Width, Y: opts.Height})
	if err != nil {
		return nil, err
	}
	view := BuildViewMatrix(opts)
	if opts.Ground && opts.GroundImage != nil {
		if err := r.uploadGroundTexture(opts.GroundImage); err != nil {
			return nil, err
		}
		if err := r.prepareGroundVertices(opts); err != nil {
			return nil, err
		}
	}

	// Materials are resolved first, before anything is recorded: see
	// modelGpuVkResolvedDraw for why an upload may not happen inside the
	// pass below.
	resolved := r.resolved[:0]
	if cap(resolved) < len(mesh.draws) {
		resolved = make([]modelGpuVkResolvedDraw, 0, len(mesh.draws))
	}
	for _, d := range mesh.draws {
		// A clip that holds a mesh back does not draw it: the depth and
		// colour behind it are already what the frame should show there.
		alpha := MeshAlphaFor(opts, d.name)
		if alpha <= 0 {
			continue
		}
		waterMesh := d.name == sgb.WaterMeshName
		st := modelGpuVkResolvedDraw{
			draw:    d,
			tex:     r.white,
			tint:    opts.BaseColor,
			mapping: RenderUVFor(opts, d.material),
			alpha:   alpha,
			depth:   modelVkDepth(view, d.centre),
		}
		if waterMesh {
			st.tint = color.RGBA{R: sgb.WaterColor[0], G: sgb.WaterColor[1], B: sgb.WaterColor[2], A: sgb.WaterColor[3]}
			st.alpha = 1
		} else if st.tint == (color.RGBA{}) {
			st.tint = RgbaWhite
		}
		if !waterMesh {
			materialKey := d.material
			if _, ok := materials[materialKey]; !ok {
				if alias := opts.MaterialAliases[materialKey]; alias != "" {
					materialKey = alias
				} else if d.materialPath != "" {
					materialKey = d.materialPath
				}
			}
			if mat, ok := materials[materialKey]; ok {
				if mat.Color != (color.RGBA{}) {
					st.tint = mat.Color
				}
				if rsh != nil {
					if data, ok := rsh(materialKey); ok {
						tex, terr := r.materialTexture(materialKey, data)
						if terr != nil {
							return nil, terr
						}
						st.tex = tex
					}
				}
				if st.tex == r.white {
					// A decoded diffuse image is used for synthesized terrain
					// materials and compiled RTX/DDS/TGA assets that do not
					// have raw RSH bytes to upload directly.
					if img := engine.ImageOf(mat.Texture); img != nil {
						tex, terr := r.materialImageTexture(materialKey, img)
						if terr != nil {
							return nil, terr
						}
						st.tex = tex
					}
				}
			}
		}
		// The tint's alpha is the clip fade, which the fragment shader
		// premultiplies the colour by. Texture transparency is handled by
		// the shader's alpha test instead, so a mesh a clip holds whole is
		// drawn solid on the opaque pipeline.
		if !waterMesh {
			st.tint.A = MeshAlphaByte(alpha)
		}
		resolved = append(resolved, st)
	}
	// The buffer is kept for the next frame rather than left to the
	// collector: a frame draws every sub mesh of the model, and a model
	// with thirty of them makes thirty of these a second.
	r.resolved = resolved
	// Solid geometry first, the fading ones from the back of the camera
	// forward. The order is worked out here, before the pass is recorded,
	// because it costs nothing next to the draw and it decides which
	// pipeline each of them goes through.
	order := modelVkOrder(resolved, r.order)
	r.order = order

	// The particles and the overlay are prepared before the pass begins,
	// because a copy may not be recorded while one is: the driver refuses
	// the upload rather than end the pass early, which on a driver without
	// validation is a crash. What prepare writes is the vertex buffer and
	// the camera; the draws themselves go into the pass below.
	//
	// The overlay is the optional selection indicator, skeleton and picked
	// marker frame, prepared through the ribbon machinery as world-space
	// quads with a colour and drawn through its depth-independent pipeline.
	overlay := ModelOverlayLines(model, pose, opts)
	if len(opts.SgbSelectedEntities) > 0 {
		selectionPose := pose
		if len(opts.AnimatedEntities) > 0 {
			selectionPose = sgb.CombineEntityPoses(len(pose.Bones), opts.AnimatedEntities)
		}
		camera := engine.Camera{Width: opts.Width, Height: opts.Height, Yaw: opts.Yaw, Pitch: opts.Pitch, Distance: opts.Distance, Target: opts.Target, Fov: opts.Fov}
		overlay = append(overlay, PrepareSgbSelectionBounds(model, opts.SgbSelectedEntities, opts.SgbEntityMeshIDs, view, camera, selectionPose, len(selectionPose.Bones) > 0)...)
	}
	r.fxGroups = nil
	if (opts.FX != nil && len(opts.FX.Groups) > 0) || len(overlay) > 0 {
		if r.fx == nil {
			fx, err := newFxVkRenderer(r.vk)
			if err != nil {
				return nil, err
			}
			r.fx = fx
		}
		groups, err := r.fx.prepare(r.vk, opts.FX, overlay, view, opts)
		if err != nil {
			return nil, err
		}
		r.fxGroups = groups
	}

	// The pass clears the colour target to the panel background: the clear
	// value is linear and the sRGB target encodes it back to the bytes the
	// background is, which is the same arithmetic an explicit clear does.
	bg := panelBackground
	clearColor := [4]float32{
		srgbToLinear(bg.R), srgbToLinear(bg.G), srgbToLinear(bg.B), 1,
	}
	if err := r.vk.BeginPass(target.color, target.depth, image.Rectangle{Max: target.size}, true, clearColor); err != nil {
		return nil, fmt.Errorf("begin pass: %w", err)
	}
	// Always close the custom pass. The success path leaves its work queued
	// for Gio's immediately following frame submission; draw errors submit
	// synchronously to restore a clean command state.
	passQueued := false
	defer func() {
		var endErr error
		if passQueued && err == nil {
			endErr = r.vk.EndPassQueued()
		} else {
			endErr = r.vk.EndPass()
		}
		if endErr != nil && err == nil {
			frame, err = nil, fmt.Errorf("end pass: %w", endErr)
		}
	}()
	if err := r.vk.SetPipeline(r.pipeline); err != nil {
		return nil, err
	}
	// Each uniform buffer is used once per two frames. The upload below waits
	// only for the submission that last read this slot, and returns at once
	// when it has finished, so preparing the next frame does not stall behind
	// the one the GPU is still drawing.
	r.uniformFrame++
	if r.uniformFrame%2 == 0 {
		r.uniforms, r.uniformsNext = r.uniformsNext, r.uniforms
	}
	r.writeFrame(view, pose, opts)
	if err := r.uploadFrame(); err != nil {
		return nil, err
	}
	if err := r.vk.SetUniforms(r.uniforms); err != nil {
		return nil, err
	}
	if opts.Ground && r.groundTexture != nil {
		if err := r.drawGround(opts); err != nil {
			return nil, err
		}
		if err := r.vk.SetPipeline(r.pipeline); err != nil {
			return nil, err
		}
		if err := r.vk.SetUniforms(r.uniforms); err != nil {
			return nil, err
		}
		if err := r.vk.SetTexture(0, r.white); err != nil {
			return nil, err
		}
	}

	// The camera and the skeleton are the same for every sub mesh, so the
	// uniform block is written once. What changes between sub meshes is
	// the tint and the mapping, and those live in the push constants: the
	// values stay in effect until the next push, so a model with one map
	// pushes once.
	//
	// The pipeline changes once, where the order crosses from solid to
	// fading geometry, and not at all on a frame whose clip hides nothing.
	// Cutout textures need no switch: the shader discards their transparent
	// texels against Shade.z before either pipeline's blend state matters.
	blending := false
	written := false
	var writtenTint color.RGBA
	var writtenUV UVMapping
	waterPipeline := false
	for _, at := range order {
		st := &resolved[at]
		waterMesh := st.draw.name == sgb.WaterMeshName
		wantBlending := st.alpha < 1
		if waterMesh {
			wantBlending = true
		}
		if wantBlending != blending || waterMesh != waterPipeline {
			blending, waterPipeline = wantBlending, waterMesh
			pipe := r.pipeline
			if blending {
				pipe = r.blend
			}
			if err := r.vk.SetPipeline(pipe); err != nil {
				return nil, err
			}
		}
		if !written || st.tint != writtenTint || st.mapping != writtenUV || waterMesh {
			alphaRef := float32(modelVkAlphaRef)
			if waterMesh {
				alphaRef = 0
			}
			r.writePush(opts, st.tint, st.mapping, alphaRef)
			if err := r.uploadPush(); err != nil {
				return nil, err
			}
			if err := r.vk.SetDrawConstants(r.pushBuf); err != nil {
				return nil, err
			}
			written, writtenTint, writtenUV = true, st.tint, st.mapping
		}
		if err := r.vk.SetTexture(0, st.tex); err != nil {
			return nil, err
		}
		if err := r.vk.SetVertexBuffer(st.draw.vertices, 0); err != nil {
			return nil, err
		}
		if err := r.vk.SetIndexBuffer(st.draw.indexes); err != nil {
			return nil, err
		}
		if err := r.vk.DrawIndexed(st.draw.count); err != nil {
			return nil, err
		}
	}
	if err := r.drawFx(r.fxGroups); err != nil {
		return nil, err
	}
	passQueued = true
	return &modelGpuVkFrame{tex: target.color, size: target.size}, nil
}

// uploadGroundTexture prepares the decoded mod image as an sRGB Vulkan texture.
// Uploads happen before BeginPass, because Vulkan cannot upload during a render pass.
func (r *modelGpuVkRenderer) uploadGroundTexture(img *image.RGBA) error {
	if img == nil {
		return nil
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return fmt.Errorf("ground texture has empty bounds")
	}
	if r.groundTexture != nil && r.groundSource == img && r.groundWidth == bounds.Dx() && r.groundHeight == bounds.Dy() {
		return nil
	}
	pixels := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(pixels, pixels.Bounds(), img, bounds.Min, draw.Src)
	tex, err := r.vk.NewTexture(gpu.VulkanTextureDesc{
		Width: bounds.Dx(), Height: bounds.Dy(), MipLevels: 1,
		Format: gpu.VulkanFormatR8G8B8A8SRGB, Sampled: true,
	})
	if err != nil {
		return fmt.Errorf("ground texture: %w", err)
	}
	if err := tex.UploadMip(0, pixels.Pix); err != nil {
		tex.Release()
		return fmt.Errorf("ground texture upload: %w", err)
	}
	if err := tex.UploadDone(); err != nil {
		tex.Release()
		return fmt.Errorf("ground texture: %w", err)
	}
	if r.groundTexture != nil {
		r.groundTexture.Release()
	}
	r.groundTexture = tex
	r.groundSource = img
	r.groundWidth, r.groundHeight = bounds.Dx(), bounds.Dy()
	return nil
}

// prepareGroundVertices uploads the XY plane before the render pass begins.
func (r *modelGpuVkRenderer) prepareGroundVertices(opts RenderOptions) error {
	center := [2]float64{opts.Target.X, opts.Target.Y}
	if r.groundVertices != nil && r.groundCenter == center {
		return nil
	}
	const halfExtent = float32(10000)
	const tileWorldSize = float32(10)
	cx, cy := float32(opts.Target.X), float32(opts.Target.Y)
	verts := []modelGpuVkGroundVertex{
		{position: [3]float32{cx - halfExtent, cy - halfExtent, 0}, normal: [3]float32{0, 0, 1}, uv: [2]float32{0, 0}},
		{position: [3]float32{cx + halfExtent, cy - halfExtent, 0}, normal: [3]float32{0, 0, 1}, uv: [2]float32{halfExtent / tileWorldSize, 0}},
		{position: [3]float32{cx + halfExtent, cy + halfExtent, 0}, normal: [3]float32{0, 0, 1}, uv: [2]float32{halfExtent / tileWorldSize, halfExtent / tileWorldSize}},
		{position: [3]float32{cx - halfExtent, cy + halfExtent, 0}, normal: [3]float32{0, 0, 1}, uv: [2]float32{0, halfExtent / tileWorldSize}},
	}
	vertices := make([]byte, 6*modelGpuVkGroundVertexStride)
	corners := [...]int{0, 1, 2, 0, 2, 3}
	for i, corner := range corners {
		v := verts[corner]
		at := i * modelGpuVkGroundVertexStride
		values := [...]float32{v.position[0], v.position[1], v.position[2], v.normal[0], v.normal[1], v.normal[2], v.uv[0], v.uv[1]}
		for j, value := range values {
			binary.LittleEndian.PutUint32(vertices[at+j*4:], math.Float32bits(value))
		}
	}
	if r.groundVertices == nil {
		buffer, err := r.vk.NewBuffer(gpu.VulkanVertexBuffer, len(vertices), true)
		if err != nil {
			return fmt.Errorf("ground vertex buffer: %w", err)
		}
		r.groundVertices = buffer
	}
	if err := r.groundVertices.Upload(vertices); err != nil {
		return fmt.Errorf("ground vertex upload: %w", err)
	}
	r.groundCenter = center
	return nil
}

// drawGround renders the ground quad into the current color and depth target.
func (r *modelGpuVkRenderer) drawGround(opts RenderOptions) error {
	if err := r.vk.SetPipeline(r.groundPipeline); err != nil {
		return err
	}
	if err := r.vk.SetVertexBuffer(r.groundVertices, 0); err != nil {
		return err
	}
	if err := r.vk.SetTexture(0, r.groundTexture); err != nil {
		return err
	}
	r.writePush(opts, RgbaWhite, UVMapping{Scale: 1}, 0)
	if err := r.uploadPush(); err != nil {
		return err
	}
	if err := r.vk.SetDrawConstants(r.pushBuf); err != nil {
		return err
	}
	if err := r.vk.Draw(6); err != nil {
		return err
	}
	return nil
}

// drawFx draws the frame's particles and overlay, if the render request
// carries any. They go through the same pass as the model, against the same
// depth buffer, which is what lets geometry in front of a glow hide it. The
// overlay is the exception: it is depth tested against nothing, so a bone
// inside the mesh is still drawn.
//
// The vertex data was written before the pass began — a copy may not be
// recorded while one is — so what is left here is the draws themselves.
func (r *modelGpuVkRenderer) drawFx(groups []fxVkGroup) error {
	if len(groups) == 0 || r.fx == nil {
		return nil
	}
	if err := r.vk.SetUniforms(r.fx.frame); err != nil {
		return err
	}
	if err := r.vk.SetDrawConstants(r.fx.pushBytes); err != nil {
		return err
	}
	lastAdd, haveLast := false, false
	lastOverlay := false
	for _, g := range groups {
		pipe := r.fx.cover
		switch {
		case g.overlay:
			pipe = r.fx.overlay
		case g.add:
			pipe = r.fx.add
		}
		// One pipeline switch per blend mode rather than per group: the
		// groups are already sorted, so the covering ones and the
		// additive ones each need exactly one change. The overlay is
		// last and only ever one group, so it costs one more.
		if !haveLast || g.add != lastAdd || g.overlay != lastOverlay {
			if err := r.vk.SetPipeline(pipe); err != nil {
				return err
			}
			lastAdd, lastOverlay, haveLast = g.add, g.overlay, true
		}
		vertexBuffer := r.fx.verts
		if g.compute {
			vertexBuffer = r.fx.computeVerts
		}
		if err := r.vk.SetVertexBuffer(vertexBuffer, g.at*fxVertexStride); err != nil {
			return err
		}
		if err := r.vk.SetTexture(0, g.tex); err != nil {
			return err
		}
		if err := r.vk.SetDrawConstants(r.fx.pushBytes); err != nil {
			return err
		}
		if err := r.vk.Draw(g.verts); err != nil {
			return err
		}
	}
	return nil
}

// writeFrame fills the frame uniform block: the view projection, the light
// and the bone matrices, in the std140 order model3d_vk.vert declares.
func (r *modelGpuVkRenderer) writeFrame(view fauxgl.Matrix, pose WhmPose, opts RenderOptions) {
	if len(opts.AnimatedEntities) > 0 {
		pose = sgb.CombineEntityPoses(len(pose.Bones), opts.AnimatedEntities)
	}
	c := r.frame
	hslslMatrix(c[0:16], view)
	// Vulkan's clip space has Y running downward,
	// so the projection is mirrored vertically — without this the model
	// would come out upside down. The mirror also reverses the apparent
	// winding of every triangle, which is exactly what the pipeline's
	// clockwise front faces and front face culling were chosen to
	// compensate for, so together they put the same triangles on screen
	// that the original front-face convention draws.
	for i := 1; i <= 13; i += 4 {
		c[i] = -c[i]
	}
	light := opts.Light
	if light == (fauxgl.Vector{}) {
		// The same key light the software renderer defaults to.
		light = fauxgl.Vector{X: 0.35, Y: -0.5, Z: 0.78}
	}
	if n := light.Length(); n > 0 {
		light = light.Normalize()
	}
	ambient := opts.Ambient
	if opts.Ambient == 0 && opts.Diffuse == 0 {
		ambient = 0.28
	}
	c[16], c[17], c[18], c[19] = float32(light.X), float32(light.Y), float32(light.Z), float32(ambient)
	bones := c[20:]
	clear(bones)
	// A bone past the uniform block has its weights stripped when the
	// vertices are built, so a zero matrix never collapses anything: it
	// simply never gets the chance to.
	for i := range pose.Bones {
		if i >= modelGpuMaxBones {
			break
		}
		hslslMatrix(bones[i*16:], pose.SkinMatrix(int32(i)))
	}
}

// modelVkAlphaRef is the fragment shader's alpha test reference for model
// draws: a texel below it is discarded before anything is written, which is
// what cuts barbed wire and foliage out of their transparent texels while
// every opaque map — whose alpha is a full 255 — draws exactly as before.
// The ground passes zero, which never discards: its texture's alpha carries
// no cutout and is not to be trusted with one.
const modelVkAlphaRef = 0.5

// writePush fills the push constant block: the shade term, the tint and the
// mapping of the material being drawn. The shade term carries the diffuse
// strength in x and the texture rotation in quarter turns in y, the two
// values the shaders read outside the uniform block, and the alpha test
// reference in z, which the fragment shader discards texels below.
//
// The tint's alpha is the fourth value, and it is what the fragment shader
// premultiplies the colour by, so a mesh a clip is fading writes less of
// itself over what is behind it rather than a darker version of itself.
func (r *modelGpuVkRenderer) writePush(opts RenderOptions, tint color.RGBA, mapping UVMapping, alphaRef float32) {
	p := r.push
	diffuse := opts.Diffuse
	if opts.Ambient == 0 && opts.Diffuse == 0 {
		diffuse = 0.72
	}
	p[0], p[1], p[2], p[3] = float32(diffuse), float32(mapping.Rotate), alphaRef, 0
	p[4], p[5], p[6], p[7] = float32(tint.R)/255, float32(tint.G)/255, float32(tint.B)/255, float32(tint.A)/255
	// The mapping of the material being drawn: move, scale, mirror. A zero
	// scale would collapse the map to a line, which is never what a reader
	// means, so an unset one is the map as the file has it.
	scale := mapping.Scale
	if scale <= 0 {
		scale = 1
	}
	mirror := float32(0)
	if mapping.Mirror {
		mirror = 1
	}
	p[8], p[9], p[10], p[11] = mapping.Offset[0], mapping.Offset[1], scale, mirror
}

// uploadFrame writes the uniform block into the buffer the shaders read.
func (r *modelGpuVkRenderer) uploadFrame() error {
	for i, v := range r.frame {
		binary.LittleEndian.PutUint32(r.frameBuf[i*4:], math.Float32bits(v))
	}
	if err := r.uniforms.UploadNoWait(r.frameBuf); err != nil {
		return fmt.Errorf("uniforms: %w", err)
	}
	return nil
}

// uploadPush writes the push block into the scratch bytes the draw pushes.
func (r *modelGpuVkRenderer) uploadPush() error {
	for i, v := range r.push {
		binary.LittleEndian.PutUint32(r.pushBuf[i*4:], math.Float32bits(v))
	}
	return nil
}

// modelGpuVkFrame is one rendered frame, still in video memory. Painting it
// hands the texture to Gio's blitter, which is the only copy in the chain.
type modelGpuVkFrame struct {
	tex  *gpu.VulkanTexture
	size image.Point
}

// Paint scales the frame to fit rect, the same way the software path scales
// its image, so switching renderers does not move anything on screen.
func (f *modelGpuVkFrame) Paint(gtx layout.Context, rect image.Rectangle) {
	if f == nil || f.tex == nil || rect.Dx() < 1 || rect.Dy() < 1 {
		return
	}
	tr := f.overlayTransform(rect).Push(gtx.Ops)
	paint.NewImageOpFromTexture(f.tex).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
}

// Overlay runs fn in the frame's own transform, which is what the software
// path gets for free from the affine it pushes around its image.
func (f *modelGpuVkFrame) Overlay(gtx layout.Context, rect image.Rectangle, fn func(layout.Context)) {
	if f == nil || f.tex == nil || rect.Dx() < 1 || rect.Dy() < 1 || fn == nil {
		return
	}
	tr := f.overlayTransform(rect).Push(gtx.Ops)
	fn(gtx)
	tr.Pop()
}

// overlayTransform is the scale and centring Paint lays the frame down with.
// Paint and Overlay share it rather than each deriving it, because the marker
// ring is only in the right place if both put the frame's pixels in the same
// place on screen.
func (f *modelGpuVkFrame) overlayTransform(rect image.Rectangle) op.TransformOp {
	scale := float32(rect.Dx()) / float32(f.size.X)
	if s := float32(rect.Dy()) / float32(f.size.Y); s < scale {
		scale = s
	}
	dx := (float32(rect.Dx()) - float32(f.size.X)*scale) / 2
	dy := (float32(rect.Dy()) - float32(f.size.Y)*scale) / 2
	return op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale)))
}

// Release does nothing: the texture belongs to the renderer, which recycles
// it.
func (f *modelGpuVkFrame) Release() {}

var TooEarly bool

func NewForWindow(w *app.Window) Handle {
	if w == nil {
		return nil
	}
	vkDev, err := w.Vulkan()
	if err != nil {
		if errors.Is(err, app.ErrNoGPUYet) {
			TooEarly = true
			return nil
		}
		log.Printf("3D preview: Vulkan GPU unavailable: %v", err)
		return nil
	}
	TooEarly = false
	r, err := NewModelGpuVkRenderer(vkDev)
	if err != nil {
		log.Printf("3D preview: Vulkan renderer unavailable: %v", err)
		return nil
	}
	log.Printf("3D preview: Vulkan renderer ready")
	return r
}
