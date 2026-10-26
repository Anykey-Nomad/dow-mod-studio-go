//go:build windows || linux

package app

import (
	"dow-mod-studio/internal/fx"
	_ "embed"
	"encoding/binary"
	"fmt"
	"image"
	"log"
	"math"
	"os"

	"gioui.org/gpu"
	"github.com/fogleman/fauxgl"
)

// The particles of a model's effects, drawn by the GPU.
//
// The software path rasterises its overlay on the CPU, which costs a
// millisecond or two for a few hundred particles — fine once, not fine sixty
// times a second. This is the same simulation, handed to the renderer that
// already draws the model: the emitter maths still runs on the CPU, where it
// lives, but the quads are built and shaded on the GPU inside the same pass
// and against the same depth buffer, so a glow behind the model is hidden by
// it rather than pasted over it.

//go:embed shaders/fx_vk.vert.spv
var fxVkVertSPIRV []byte

//go:embed shaders/fx_vk.frag.spv
var fxVkFragSPIRV []byte

//go:embed shaders/fx_vk_expand.comp.spv
var fxVkExpandSPIRV []byte

//go:embed shaders/fx_vk_ribbon.comp.spv
var fxVkRibbonSPIRV []byte

// One FX vertex has a 12-float base payload plus orientation and surface
// normal vec4s. Keeping the stride 16-byte aligned also matches both compute
// shaders' storage-buffer writes.
const (
	fxVertexStride = 80
	fxVertsPerQuad = 6
	fxFrameFloats  = 32 // mat4 ViewProj, then Right, Up, Light and Diffuse
	fxPushFloats   = 4
	// fxComputeSpriteCountFloats is the size of the count/padding header at
	// the front of the sprite input buffer, in floats. The header holds one uint
	// (the sprite count) plus padding to the std430 runtime-array alignment, so
	// the first sprite record starts on a 16-byte boundary.
	fxComputeSpriteCountFloats = 4
	// fxComputeSpriteStrideFloats is the size of one compact sprite record the
	// CPU writes into the storage buffer, in floats. It matches the std430
	// structure: vec4 pos, float radius, padding, vec4 tint, orientation and
	// plane normal.
	fxComputeSpriteStrideFloats = 20
	// One emitter is capped at fx.MaxParticles; a model can carry this many
	// effects at once, so keep the compute input capacity in step with the
	// existing model-wide vertex buffer limit.
	fxComputeMaxSprites = fx.MaxParticles * fxMaxModelEffects
	// fxComputeSpriteInputFloats is the size of the CPU-written sprite input
	// buffer: a 4-float count/padding header, then one sprite record per sprite.
	fxComputeSpriteInputFloats = fxComputeSpriteCountFloats + fxComputeMaxSprites*fxComputeSpriteStrideFloats
	// fxComputeRibbonCountFloats is the count/padding header of the ribbon
	// input buffer, the same std430 runtime-array header the sprite input has.
	fxComputeRibbonCountFloats = 4
	// fxComputeRibbonStrideFloats is one compact ribbon record: from, to,
	// arc, across, tint, geom and eye (a vec4 each) plus a uvec4 of
	// properties — 32 floats matching the std430 struct in fx_vk_ribbon.comp.
	fxComputeRibbonStrideFloats = 32
	// fxComputeMaxRibbons caps the ribbons one frame hands the expand pass:
	// beams, rings and skeleton lines together. Past it the frame draws the
	// ribbons that fit, as the sprite cap already does.
	fxComputeMaxRibbons = 128
	// fxComputeRibbonInputFloats is the CPU-written ribbon input buffer.
	fxComputeRibbonInputFloats = fxComputeRibbonCountFloats + fxComputeMaxRibbons*fxComputeRibbonStrideFloats
	// fxComputeMaxRibbonVerts is the share of the vertex output buffer
	// reserved for ribbons: fxComputeMaxRibbons of them, each writing at most
	// fxBeamVertsPerRibbon vertices — the same ceiling the CPU path sizes its
	// beam room against.
	fxComputeMaxRibbonVerts = fxComputeMaxRibbons * fxBeamVertsPerRibbon
	// fxBeamVertsPerRibbon is what one beam writes: (Segments+1) pairs of
	// corners, each pair joined into two triangles, with Segments held to the
	// ceiling the tessellator enforces. The +1 covers the first segment.
	fxBeamVertsPerRibbon = (fx.MaxBeamSegments + 1) * 6
	// fxMaxBeamRibbons caps how many ribbons one frame may write, so a file
	// with many beams cannot ask for an unbounded buffer.
	fxMaxBeamRibbons = 32
	// fxVertsPerModelEffect is what one effect's share of the buffer is
	// worth: fx.MaxParticles bounds a single simulation, but every effect on
	// the model draws into the same block, so the buffer is sized for a model
	// carrying many effects at once rather than for one. A necron monolith
	// plays around a hundred, and each of those may sit at the cap.
	fxVertsPerModelEffect = fxVertsPerQuad * fx.MaxParticles
	// fxMaxModelEffects is how many effects' worth of particles the buffer
	// holds. Past it a frame draws the effects that fit and leaves the rest,
	// which is a preview deciding what to show rather than a failure.
	fxMaxModelEffects = 96
	// fxMaxVerts is the whole vertex buffer: every particle's quad across
	// every effect that fits, plus room for the beams.
	fxMaxVerts = fxVertsPerModelEffect*fxMaxModelEffects + fxBeamVertsPerRibbon*fxMaxBeamRibbons
)

// The corners of one quad, and where each of them reads on the map: two
// triangles, wound the way the pipeline takes them.
//
// The map's V runs the other way from the corner's Y, and deliberately so. The
// vertex shader puts corner.y = +1 along the camera's own up, which is the top
// of the quad on screen, and a map's row zero is its top — so the top of the
// quad has to read v = 0, not v = 1. The software overlay gets this for free
// because it walks the picture's rows downwards from the sprite's top edge;
// the GPU path has to say it in the corner table. With v the other way round
// every effect is drawn upside down here while the CPU path draws it right,
// which is how a particle map with a direction in it comes out mirrored.
var fxQuadCorners = [fxVertsPerQuad][4]float32{
	{-1, -1, 0, 1}, {1, -1, 1, 1}, {1, 1, 1, 0},
	{-1, -1, 0, 1}, {1, 1, 1, 0}, {-1, 1, 0, 0},
}

// fxVkRenderer draws particles on the same device as the model.
type fxVkRenderer struct {
	cover *gpu.VulkanPipeline
	add   *gpu.VulkanPipeline
	// overlay draws the skeleton and the picked marker's frame. It is the
	// covering pipeline without the depth test, because the software
	// renderer draws its bones over the mesh rather than inside it, and a
	// reader who asked for the skeleton wants to see it wherever it is.
	overlay      *gpu.VulkanPipeline
	frame        *gpu.VulkanBuffer
	verts        *gpu.VulkanBuffer // CPU-built beams and overlay
	computeVerts *gpu.VulkanBuffer // compute-built sprite vertices

	// expand is the compute pipeline that turns the compact sprite list into
	// the vertex buffer the existing pipelines draw. It is a first compute
	// pass: for now it only expands sprites, while beams and the overlay stay
	// on the CPU, because the full simulation state the beams need is still on
	// the CPU and moving it is a bigger change than the bridge itself.
	expand *gpu.VulkanComputePipeline
	// spriteBuf is the compact input the CPU simulation writes into and the
	// compute shader reads. It is a host visible buffer because the CPU fills
	// it every frame before dispatch.
	spriteBuf *gpu.VulkanBuffer
	// spriteBytes is the shadow copy the CPU uses to upload spriteBuf.
	spriteBytes []byte
	// ribbonExpand is the compute pipeline that tessellates ribbons — beams,
	// rings and the skeleton overlay — from compact records, beside the
	// sprite expand. It is nil when the driver refused it, and the ribbons
	// then keep the CPU tessellation without taking the particle pass down.
	ribbonExpand *gpu.VulkanComputePipeline
	// ribbonBuf is the compact input the CPU writes and the ribbon expand
	// reads; ribbonBytes its shadow copy, as with the sprites.
	ribbonBuf   *gpu.VulkanBuffer
	ribbonBytes []byte
	// frameFloats, frameBytes, pushFloats, pushBytes and vertsData are the
	// same scratch buffers the existing path already keeps between frames.
	frameFloats []float32
	frameBytes  []byte
	pushFloats  []float32
	pushBytes   []byte
	// vertsData is the vertex data of the frame being recorded. It is kept
	// between frames so a steady stream of particles allocates nothing.
	vertsData []byte
	// textures is keyed by the map's own path; a nil entry is a map that
	// could not be read, which draws as a plain coloured disc and is not
	// looked for again.
	textures map[string]*gpu.VulkanTexture
	white    *gpu.VulkanTexture
	// solid is one opaque white texel. The overlay samples it at a fixed
	// coordinate so a bone is the flat colour it asks for: the round soft
	// falloff in white would shade a straight line into a smudge.
	solid *gpu.VulkanTexture

	frameBuffers []fxVkFrameBuffers
	frameIndex   int
}

type fxVkFrameBuffers struct {
	frame        *gpu.VulkanBuffer
	verts        *gpu.VulkanBuffer
	computeVerts *gpu.VulkanBuffer
	spriteBuf    *gpu.VulkanBuffer
	ribbonBuf    *gpu.VulkanBuffer
}

func (b fxVkFrameBuffers) release() {
	if b.ribbonBuf != nil {
		b.ribbonBuf.Release()
	}
	if b.spriteBuf != nil {
		b.spriteBuf.Release()
	}
	if b.computeVerts != nil {
		b.computeVerts.Release()
	}
	if b.verts != nil {
		b.verts.Release()
	}
	if b.frame != nil {
		b.frame.Release()
	}
}

// fxFallbackSprite is the map a particle whose own texture could not be read
// is drawn with. It is a soft round puff rather than the flat white square a
// one pixel texture samples to: the shader samples whatever this map holds at
// every corner of the quad, so a constant map fills the whole quad and a
// particle with a missing map reads as a hard white block sitting over the
// model. A radial falloff keeps the particle the round, soft shape the eye
// expects, which is what tells a viewer that an effect is playing while
// saying which of its maps is absent.
const fxFallbackSprite = 64

// fxFallbackSpriteBytes is that map: white in the middle, fading to nothing at
// the edge, laid out the way fxTextureBytes lays a decoded map out, so the
// sampler and the blend see exactly what they see for a real texture.
func fxFallbackSpriteBytes() []byte {
	const n = fxFallbackSprite
	out := make([]byte, n*n*4)
	c := float64(n-1) / 2
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-c, float64(y)-c
			d := math.Hypot(dx, dy) / c
			// A smoothstep from the middle to the rim: full strength
			// over the inner half, nothing at the edge.
			var a float64
			if d < 1 {
				t := 1 - d
				a = t * t * (3 - 2*t)
			}
			i := (y*n + x) * 4
			out[i] = 0xff
			out[i+1] = 0xff
			out[i+2] = 0xff
			out[i+3] = clampByte(a * 255)
		}
	}
	return out
}

// fxVkGroup is one draw: every particle that reads the same map.
type fxVkGroup struct {
	tex   *gpu.VulkanTexture
	verts int // how many vertices this group wrote
	at    int // where they start
	add   bool
	// overlay draws through the debug overlay pipeline instead of the
	// particle ones: the same vertex layout and the same shader, but
	// tested against nothing, because an overlay the model hid would be
	// no overlay at all. A bone inside a mesh has to be readable.
	overlay bool
	// compute marks sprite vertices written to computeVerts instead of verts.
	compute bool
}

// newFxVkRenderer compiles the two pipelines and the buffers they read.
func newFxVkRenderer(v *gpu.VulkanPass) (*fxVkRenderer, error) {
	inputs := []gpu.VulkanInput{
		{Location: 0, Format: gpu.VulkanFormatFloat32x3, Offset: 0},
		{Location: 1, Format: gpu.VulkanFormatFloat32x2, Offset: 12},
		{Location: 2, Format: gpu.VulkanFormatFloat32, Offset: 20},
		{Location: 3, Format: gpu.VulkanFormatFloat32x4, Offset: 24},
		{Location: 4, Format: gpu.VulkanFormatFloat32x2, Offset: 40},
		{Location: 5, Format: gpu.VulkanFormatFloat32x4, Offset: 48},
		{Location: 6, Format: gpu.VulkanFormatFloat32x4, Offset: 64},
	}
	desc := func(blend gpu.VulkanBlendMode) gpu.VulkanPipelineDesc {
		return gpu.VulkanPipelineDesc{
			Vertex:   fxVkVertSPIRV,
			Fragment: fxVkFragSPIRV,
			Stride:   fxVertexStride,
			Inputs:   inputs,
			Cull:     gpu.VulkanCullNone,
			// A particle is tested against the model, so geometry in
			// front of it hides it, and writes no depth, so the
			// particles behind one another still blend.
			DepthTest:  true,
			DepthWrite: false,
			Blend:      blend,
			PushSize:   fxPushFloats * 4,
		}
	}
	cover, err := v.NewPipeline(desc(gpu.VulkanBlendPremultiplied))
	if err != nil {
		return nil, fmt.Errorf("particle pipeline: %w", err)
	}
	add, err := v.NewPipeline(desc(gpu.VulkanBlendAdd))
	if err != nil {
		cover.Release()
		return nil, fmt.Errorf("additive particle pipeline: %w", err)
	}
	overlayDesc := desc(gpu.VulkanBlendPremultiplied)
	// The one setting that separates the overlay from a particle: no depth
	// test, and no depth write either, so the lines drawn over each other
	// still blend rather than cutting holes in one another.
	overlayDesc.DepthTest = false
	overlay, err := v.NewPipeline(overlayDesc)
	if err != nil {
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("overlay pipeline: %w", err)
	}
	frame, err := v.FrameBuffer(gpu.VulkanConstantBuffer, fxFrameFloats*4)
	if err != nil {
		overlay.Release()
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("particle frame buffer: %w", err)
	}
	verts, err := v.FrameBuffer(gpu.VulkanVertexBuffer, fxVertexStride*fxMaxVerts)
	if err != nil {
		frame.Release()
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("particle vertex buffer: %w", err)
	}
	white, err := v.NewTexture(gpu.VulkanTextureDesc{
		Width: fxFallbackSprite, Height: fxFallbackSprite, MipLevels: 1,
		Format: gpu.VulkanFormatR8G8B8A8SRGB, Sampled: true,
	})
	if err != nil {
		verts.Release()
		frame.Release()
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("particle white texture: %w", err)
	}
	solid, err := v.NewTexture(gpu.VulkanTextureDesc{
		Width: 1, Height: 1, MipLevels: 1,
		Format: gpu.VulkanFormatR8G8B8A8SRGB, Sampled: true,
	})
	if err != nil {
		white.Release()
		verts.Release()
		frame.Release()
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("overlay white texture: %w", err)
	}
	if err := solid.UploadMip(0, []byte{0xff, 0xff, 0xff, 0xff}); err != nil {
		solid.Release()
		white.Release()
		verts.Release()
		frame.Release()
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("overlay white texture upload: %w", err)
	}
	if err := solid.UploadDone(); err != nil {
		solid.Release()
		white.Release()
		verts.Release()
		frame.Release()
		add.Release()
		cover.Release()
		return nil, fmt.Errorf("overlay white texture: %w", err)
	}
	if err := white.UploadMip(0, fxFallbackSpriteBytes()); err != nil {
		white.Release()
		verts.Release()
		frame.Release()
		add.Release()
		cover.Release()
		return nil, err
	}
	if err := white.UploadDone(); err != nil {
		white.Release()
		verts.Release()
		frame.Release()
		add.Release()
		cover.Release()
		return nil, err
	}
	// Compute is the normal sprite path. Set DOW_FX_COMPUTE=0 to use the
	// conservative CPU expansion while debugging a driver-specific issue.
	var computeVerts *gpu.VulkanBuffer
	var expand *gpu.VulkanComputePipeline
	var spriteBuf *gpu.VulkanBuffer
	var spriteBytes []byte
	var ribbonExpand *gpu.VulkanComputePipeline
	var ribbonBuf *gpu.VulkanBuffer
	var ribbonBytes []byte
	if os.Getenv("DOW_FX_COMPUTE") != "0" {
		spriteBytes = make([]byte, fxComputeSpriteInputFloats*4)
		computeVerts, err = v.NewVertexStorageBuffer(fxVertexStride * (fxVertsPerQuad*fxComputeMaxSprites + fxComputeMaxRibbonVerts))
		if err != nil {
			solid.Release()
			white.Release()
			verts.Release()
			frame.Release()
			overlay.Release()
			add.Release()
			cover.Release()
			return nil, fmt.Errorf("particle compute vertex buffer: %w", err)
		}
		expand, err = v.NewComputePipeline(gpu.VulkanComputePipelineDesc{
			Compute:  fxVkExpandSPIRV,
			Storage:  []int{0, 1},
			PushSize: 16, // input base, output base, sprite count, reserved
		})
		if err != nil {
			computeVerts.Release()
			solid.Release()
			white.Release()
			verts.Release()
			frame.Release()
			overlay.Release()
			add.Release()
			cover.Release()
			return nil, fmt.Errorf("particle expand compute pipeline: %w", err)
		}
		spriteBuf, err = v.NewStorageBuffer(fxComputeSpriteInputFloats * 4)
		if err != nil {
			expand.Release()
			computeVerts.Release()
			solid.Release()
			white.Release()
			verts.Release()
			frame.Release()
			overlay.Release()
			add.Release()
			cover.Release()
			return nil, fmt.Errorf("particle sprite input buffer: %w", err)
		}
		// The ribbon expand is an addition to a working sprite path: a
		// driver that refuses this pipeline still draws sprites and beams
		// (the beams through their CPU tessellation), so a failure here is
		// logged and left out rather than failing the whole renderer.
		ribbonBytes = make([]byte, fxComputeRibbonInputFloats*4)
		ribbonExpand, err = v.NewComputePipeline(gpu.VulkanComputePipelineDesc{
			Compute:  fxVkRibbonSPIRV,
			Storage:  []int{0, 1},
			PushSize: 16, // input base, output base, ribbon count, reserved
		})
		if err != nil {
			log.Printf("3D preview: ribbon expand pipeline: %v (ribbons stay on the CPU)", err)
			ribbonExpand, ribbonBytes = nil, nil
			err = nil
		}
		if ribbonExpand != nil {
			ribbonBuf, err = v.NewStorageBuffer(fxComputeRibbonInputFloats * 4)
			if err != nil {
				ribbonExpand.Release()
				ribbonExpand, ribbonBuf, ribbonBytes = nil, nil, nil
				log.Printf("3D preview: ribbon input buffer: %v (ribbons stay on the CPU)", err)
				err = nil
			}
		}
	}
	frameBuffers := []fxVkFrameBuffers{{
		frame: frame, verts: verts, computeVerts: computeVerts,
		spriteBuf: spriteBuf, ribbonBuf: ribbonBuf,
	}}
	second, frameErr := newFxVkFrameBuffers(v, computeVerts != nil, ribbonBuf != nil)
	if frameErr != nil {
		log.Printf("3D preview: second frame resource set unavailable, reusing buffers with synchronization: %v", frameErr)
	} else {
		frameBuffers = append(frameBuffers, second)
	}
	return &fxVkRenderer{
		cover:        cover,
		add:          add,
		overlay:      overlay,
		frame:        frame,
		verts:        verts,
		computeVerts: computeVerts,
		white:        white,
		solid:        solid,
		expand:       expand,
		spriteBuf:    spriteBuf,
		spriteBytes:  spriteBytes,
		ribbonExpand: ribbonExpand,
		ribbonBuf:    ribbonBuf,
		ribbonBytes:  ribbonBytes,
		textures:     map[string]*gpu.VulkanTexture{},
		frameFloats:  make([]float32, fxFrameFloats),
		frameBytes:   make([]byte, fxFrameFloats*4),
		pushFloats:   make([]float32, fxPushFloats),
		pushBytes:    make([]byte, fxPushFloats*4),
		vertsData:    make([]byte, 0, fxVertexStride*fxVertsPerQuad*64),
		frameBuffers: frameBuffers,
	}, nil
}

func newFxVkFrameBuffers(v *gpu.VulkanPass, compute, ribbon bool) (buffers fxVkFrameBuffers, err error) {
	defer func() {
		if err != nil {
			buffers.release()
		}
	}()
	alloc := func(kind gpu.VulkanBufferKind, size int) (*gpu.VulkanBuffer, error) {
		return v.FrameBuffer(kind, size)
	}
	if buffers.frame, err = alloc(gpu.VulkanConstantBuffer, fxFrameFloats*4); err != nil {
		return buffers, err
	}
	if buffers.verts, err = alloc(gpu.VulkanVertexBuffer, fxVertexStride*fxMaxVerts); err != nil {
		return buffers, err
	}
	if compute {
		if buffers.computeVerts, err = v.FrameVertexStorageBuffer(fxVertexStride * (fxVertsPerQuad*fxComputeMaxSprites + fxComputeMaxRibbonVerts)); err != nil {
			return buffers, err
		}
		if buffers.spriteBuf, err = v.FrameStorageBuffer(fxComputeSpriteInputFloats * 4); err != nil {
			return buffers, err
		}
	}
	if ribbon {
		if buffers.ribbonBuf, err = v.FrameStorageBuffer(fxComputeRibbonInputFloats * 4); err != nil {
			return buffers, err
		}
	}
	return buffers, nil
}

func (r *fxVkRenderer) uploadFrameBuffer(buffer *gpu.VulkanBuffer, data []byte) error {
	if buffer == nil {
		return nil
	}
	if len(r.frameBuffers) > 1 {
		return buffer.UploadNoWait(data)
	}
	return buffer.Upload(data)
}

func (r *fxVkRenderer) release() {
	if r == nil {
		return
	}
	for _, t := range r.textures {
		if t != nil {
			t.Release()
		}
	}
	if r.expand != nil {
		r.expand.Release()
	}
	if r.ribbonExpand != nil {
		r.ribbonExpand.Release()
	}
	for _, buffers := range r.frameBuffers {
		buffers.release()
	}
	r.solid.Release()

	r.white.Release()
	r.overlay.Release()
	r.add.Release()
	r.cover.Release()
}

// texture is the map a particle reads, uploaded the first time it is asked for.
func (r *fxVkRenderer) texture(pass *gpu.VulkanPass, name string, cache map[string]*image.RGBA) *gpu.VulkanTexture {
	if name == "" {
		return r.white
	}
	if tex, ok := r.textures[name]; ok {
		if tex == nil {
			return r.white
		}
		return tex
	}
	img := cache[name]
	if img == nil {
		r.textures[name] = nil
		return r.white
	}
	b := img.Bounds()
	tex, err := pass.NewTexture(gpu.VulkanTextureDesc{
		Width: b.Dx(), Height: b.Dy(), MipLevels: 1,
		Format: gpu.VulkanFormatR8G8B8A8SRGB, Sampled: true,
	})
	if err != nil {
		log.Printf("3D preview: particle texture %q: %v", name, err)
		r.textures[name] = nil
		return r.white
	}
	if err := tex.UploadMip(0, fxTextureBytes(img)); err != nil {
		tex.Release()
		r.textures[name] = nil
		return r.white
	}
	if err := tex.UploadDone(); err != nil {
		tex.Release()
		r.textures[name] = nil
		return r.white
	}
	r.textures[name] = tex
	return tex
}

// fxTextureBytes is a decoded map as the GPU wants it: the rows as stored.
func fxTextureBytes(img *image.RGBA) []byte {
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := 0; y < b.Dy(); y++ {
		row := img.Pix[y*img.Stride:]
		out = append(out, row[:b.Dx()*4]...)
	}
	return out
}

// prepare writes the frame and vertex data before the render pass begins. On
// Vulkan, compute expands particle records by default; DOW_FX_COMPUTE=0 selects
// the CPU expansion fallback for driver debugging. Sprite compute work is queued
// into the command buffer used by the following graphics pass, avoiding an
// intermediate submit-and-wait.
//
// Nothing here waits for the GPU: every upload retires only the submission that
// last read the buffer being written, and a rotated frame resource whose reader
// has already finished does not wait at all.
func (r *fxVkRenderer) prepare(pass *gpu.VulkanPass, scene *fxGpuScene, overlay []modelOverlayLine, view fauxgl.Matrix, opts RenderOptions) ([]fxVkGroup, error) {
	nGroups := 0
	if scene != nil {
		nGroups = len(scene.Groups)
	}
	groups := make([]fxVkGroup, 0, nGroups*2+1)
	if len(r.frameBuffers) > 1 {
		r.frameIndex = (r.frameIndex + 1) % len(r.frameBuffers)
		buffers := r.frameBuffers[r.frameIndex]
		r.frame, r.verts = buffers.frame, buffers.verts
		r.computeVerts, r.spriteBuf, r.ribbonBuf = buffers.computeVerts, buffers.spriteBuf, buffers.ribbonBuf
	}
	r.vertsData = r.vertsData[:0]
	computeEnabled := os.Getenv("DOW_FX_COMPUTE") != "0" && r.expand != nil && r.computeVerts != nil && r.spriteBuf != nil
	totalSprites := 0
	if computeEnabled && scene != nil {
		for i := range scene.Groups {
			if totalSprites >= fxComputeMaxSprites {
				break
			}
			g := &scene.Groups[i]
			for _, sp := range g.Sprites {
				if totalSprites >= fxComputeMaxSprites {
					break
				}
				r.appendSpriteInput(g.Origin, sp, totalSprites)
				totalSprites++
			}
		}
	}
	if totalSprites > 0 {
		binary.LittleEndian.PutUint32(r.spriteBytes, uint32(totalSprites))
		inputBytes := fxComputeSpriteCountFloats*4 + totalSprites*fxComputeSpriteStrideFloats*4
		if err := r.uploadFrameBuffer(r.spriteBuf, r.spriteBytes[:inputBytes]); err != nil {
			return nil, fmt.Errorf("particle sprite input upload: %w", err)
		}
	}
	// Ribbons expand on the GPU beside the sprites when their own pipeline
	// came up; without it they keep the CPU tessellation exactly as before.
	ribbonCompute := computeEnabled && r.ribbonExpand != nil && r.ribbonBuf != nil
	ribbonCount := 0
	ribbonVerts := 0
	// spriteBlockVerts is where the ribbon block sits in the shared vertex
	// output: right behind the sprites counted above.
	spriteBlockVerts := totalSprites * fxVertsPerQuad

	// Frame-wide light data is uploaded once; each particle and ribbon is
	// shaded by the vertex shader instead of multiplying its colour on CPU.
	right, up := fxCameraBasis(opts)
	light := fxSceneLightOf(opts)
	hslslMatrix(r.frameFloats[0:16], view)
	for i := 1; i <= 13; i += 4 {
		r.frameFloats[i] = -r.frameFloats[i]
	}
	r.frameFloats[16], r.frameFloats[17], r.frameFloats[18], r.frameFloats[19] = float32(right.X), float32(right.Y), float32(right.Z), 1
	r.frameFloats[20], r.frameFloats[21], r.frameFloats[22], r.frameFloats[23] = float32(up.X), float32(up.Y), float32(up.Z), 0
	r.frameFloats[24], r.frameFloats[25], r.frameFloats[26], r.frameFloats[27] = float32(light.dir.X), float32(light.dir.Y), float32(light.dir.Z), float32(light.ambient)
	r.frameFloats[28] = float32(light.diffuse)
	for i, v := range r.frameFloats {
		binary.LittleEndian.PutUint32(r.frameBytes[i*4:], math.Float32bits(v))
	}
	if err := r.uploadFrameBuffer(r.frame, r.frameBytes); err != nil {
		return nil, fmt.Errorf("particle frame uniform: %w", err)
	}
	for i, v := range []float32{1, 1, 1, 1} {
		r.pushFloats[i] = v
		binary.LittleEndian.PutUint32(r.pushBytes[i*4:], math.Float32bits(v))
	}

	nextComputeSprite := 0
	cpuSprites := 0
	for i := 0; i < nGroups; i++ {
		g := &scene.Groups[i]
		if len(g.Sprites) == 0 && len(g.Beams) == 0 {
			continue
		}
		tex := r.texture(pass, g.Texture, scene.Textures)
		if computeEnabled && nextComputeSprite < totalSprites && len(g.Sprites) > 0 {
			count := len(g.Sprites)
			if count > totalSprites-nextComputeSprite {
				count = totalSprites - nextComputeSprite
			}
			groups = append(groups, fxVkGroup{
				tex: tex, add: g.Additive,
				at:      nextComputeSprite * fxVertsPerQuad,
				verts:   count * fxVertsPerQuad,
				compute: true,
			})
			nextComputeSprite += count
		} else if !computeEnabled && len(g.Sprites) > 0 {
			start := len(r.vertsData) / fxVertexStride
			for _, sp := range g.Sprites {
				if cpuSprites >= fxComputeMaxSprites || len(r.vertsData)/fxVertexStride+fxVertsPerQuad > fxMaxVerts {
					break
				}
				r.appendSprite(g.Origin, sp)
				cpuSprites++
			}
			if n := len(r.vertsData)/fxVertexStride - start; n > 0 {
				groups = append(groups, fxVkGroup{tex: tex, add: g.Additive, at: start, verts: n})
			}
		}
		if len(g.Beams) > 0 {
			if ribbonCompute {
				start := spriteBlockVerts + ribbonVerts
				for _, bm := range g.Beams {
					if ribbonCount >= fxComputeMaxRibbons {
						break
					}
					if n := r.appendRibbonInput(bm.Ribbon, bm.Eye, ribbonCount, ribbonVerts); n > 0 {
						ribbonVerts += n
						ribbonCount++
					}
				}
				if n := spriteBlockVerts + ribbonVerts - start; n > 0 {
					groups = append(groups, fxVkGroup{tex: tex, add: g.Additive, at: start, verts: n, compute: true})
				}
			} else {
				start := len(r.vertsData) / fxVertexStride
				for _, bm := range g.Beams {
					if len(r.vertsData)/fxVertexStride+fxBeamVertsPerRibbon > fxMaxVerts {
						break
					}
					r.appendBeam(bm)
				}
				if n := len(r.vertsData)/fxVertexStride - start; n > 0 {
					groups = append(groups, fxVkGroup{tex: tex, add: g.Additive, at: start, verts: n})
				}
			}
		}
	}
	if len(overlay) > 0 {
		if ribbonCompute {
			start := spriteBlockVerts + ribbonVerts
			for _, line := range overlay {
				if ribbonCount >= fxComputeMaxRibbons {
					break
				}
				n := r.appendRibbonInput(fx.BeamRibbon{
					From:     line.From,
					To:       line.To,
					Width:    line.HalfWidth,
					R:        line.R,
					G:        line.G,
					B:        line.B,
					Alpha:    line.A,
					SelfLit:  true,
					Segments: 1,
				}, fxEyePosition(opts), ribbonCount, ribbonVerts)
				if n > 0 {
					ribbonVerts += n
					ribbonCount++
				}
			}
			if n := spriteBlockVerts + ribbonVerts - start; n > 0 {
				groups = append(groups, fxVkGroup{tex: r.solid, at: start, verts: n, overlay: true, compute: true})
			}
		} else {
			start := len(r.vertsData) / fxVertexStride
			for _, line := range overlay {
				if len(r.vertsData)/fxVertexStride+fxVertsPerQuad > fxMaxVerts {
					break
				}
				r.appendOverlayLine(line, fxEyePosition(opts))
			}
			if n := len(r.vertsData)/fxVertexStride - start; n > 0 {
				groups = append(groups, fxVkGroup{tex: r.solid, at: start, verts: n, overlay: true})
			}
		}
	}
	if len(r.vertsData) > 0 {
		if err := r.uploadFrameBuffer(r.verts, r.vertsData); err != nil {
			return nil, fmt.Errorf("particle CPU vertices: %w", err)
		}
	}
	// Resolve any first-use texture uploads and CPU vertex copies before
	// queuing compute. The queued dispatch and following render pass can then
	// share one command buffer and one fence, without an upload forcing the
	// compute work to submit on its own.
	// The storage bindings are sampled when a dispatch is recorded, so each
	// expand binds its own input immediately before dispatching: the two
	// share the vertex output but read different records.
	if totalSprites > 0 {
		if err := pass.SetStorageBuffer(0, r.spriteBuf); err != nil {
			return nil, fmt.Errorf("particle sprite input bind: %w", err)
		}
		if err := pass.SetStorageBuffer(1, r.computeVerts); err != nil {
			return nil, fmt.Errorf("particle vertex output bind: %w", err)
		}
		var dispatch [16]byte
		binary.LittleEndian.PutUint32(dispatch[8:], uint32(totalSprites))
		if err := pass.DispatchComputeQueued(r.expand, dispatch[:], divCeil(totalSprites, 64), 1, 1); err != nil {
			return nil, fmt.Errorf("particle expand dispatch: %w", err)
		}
	}
	if ribbonCount > 0 {
		binary.LittleEndian.PutUint32(r.ribbonBytes, uint32(ribbonCount))
		inputBytes := fxComputeRibbonCountFloats*4 + ribbonCount*fxComputeRibbonStrideFloats*4
		if err := r.uploadFrameBuffer(r.ribbonBuf, r.ribbonBytes[:inputBytes]); err != nil {
			return nil, fmt.Errorf("ribbon input upload: %w", err)
		}
		if err := pass.SetStorageBuffer(0, r.ribbonBuf); err != nil {
			return nil, fmt.Errorf("ribbon input bind: %w", err)
		}
		if err := pass.SetStorageBuffer(1, r.computeVerts); err != nil {
			return nil, fmt.Errorf("ribbon vertex output bind: %w", err)
		}
		var dispatch [16]byte
		// The ribbon block starts behind the sprites in the shared output.
		binary.LittleEndian.PutUint32(dispatch[4:], uint32(spriteBlockVerts))
		binary.LittleEndian.PutUint32(dispatch[8:], uint32(ribbonCount))
		if err := pass.DispatchComputeQueued(r.ribbonExpand, dispatch[:], divCeil(ribbonCount, 64), 1, 1); err != nil {
			return nil, fmt.Errorf("ribbon expand dispatch: %w", err)
		}
	}
	if totalSprites > 0 || ribbonCount > 0 {
		if err := pass.PrepareVertexBuffer(r.computeVerts); err != nil {
			return nil, fmt.Errorf("particle compute vertex barrier: %w", err)
		}
	}
	if len(groups) == 0 {
		return nil, nil
	}
	return groups, nil
}

// appendSprite writes one quad: six vertices of the same particle, including
// its optional fixed-horizontal orientation for the vertex shader.
func (r *fxVkRenderer) appendSprite(origin [3]float64, sp fx.Sprite) {
	pos := [3]float32{
		float32(origin[0] + sp.Pos[0]),
		float32(origin[1] + sp.Pos[1]),
		float32(origin[2] + sp.Pos[2]),
	}
	radius := float32(sp.Radius)
	if radius <= 0 {
		radius = fxSpriteFallbackRadius
	}
	a := float32(clampFloat64(sp.A, 0, 1))
	tint := [4]float32{
		float32(clampFloat64(sp.R, 0, 1)),
		float32(clampFloat64(sp.G, 0, 1)),
		float32(clampFloat64(sp.B, 0, 1)),
		a,
	}
	orientationMode := float32(0)
	if sp.Horizontal {
		orientationMode = 1
		if sp.PlaneNormal != [3]float64{} && sp.PlaneNormal != [3]float64{0, 0, 1} {
			orientationMode = 2
		}
	}
	selfLit := float32(0)

	if sp.SelfLit {
		selfLit = 1
	}
	orientation := [4]float32{float32(sp.Rotation), orientationMode, selfLit, 0}
	planeNormal := sp.PlaneNormal
	if planeNormal == [3]float64{} {
		planeNormal = [3]float64{0, 0, 1}
	}
	for _, c := range fxQuadCorners {
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(pos[0]))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(pos[1]))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(pos[2]))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(c[0]))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(c[1]))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(radius))
		for _, ch := range tint {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(ch))
		}
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(c[2]))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(c[3]))
		for _, ch := range orientation {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(ch))
		}
		if !sp.Horizontal {
			planeNormal = [3]float64{0, 0, 1}
		}
		for _, ch := range []float64{planeNormal[0], planeNormal[1], planeNormal[2], 0} {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(ch)))
		}
	}
}

// appendSpriteInput writes one compact sprite record into the compute input
// buffer. It matches the std430 Sprite record in fx_vk_expand.comp, including
// the orientation the graphics vertex shader applies after expansion.
func (r *fxVkRenderer) appendSpriteInput(origin [3]float64, sp fx.Sprite, idx int) {
	if idx < 0 || idx >= fxComputeMaxSprites {
		return
	}
	pos := [3]float32{
		float32(origin[0] + sp.Pos[0]),
		float32(origin[1] + sp.Pos[1]),
		float32(origin[2] + sp.Pos[2]),
	}
	radius := float32(sp.Radius)
	if radius <= 0 {
		radius = fxSpriteFallbackRadius
	}
	a := float32(clampFloat64(sp.A, 0, 1))
	tint := [4]float32{
		float32(clampFloat64(sp.R, 0, 1)),
		float32(clampFloat64(sp.G, 0, 1)),
		float32(clampFloat64(sp.B, 0, 1)),
		a,
	}
	// The std430 runtime array starts after a 16-byte count header. Each Sprite
	// has vec4 pos at 0, radius at 16, vec4 tint at 32 and orientation at 48.
	base := fxComputeSpriteCountFloats*4 + idx*fxComputeSpriteStrideFloats*4
	end := base + fxComputeSpriteStrideFloats*4
	if end > len(r.spriteBytes) {
		return
	}
	rec := r.spriteBytes[base:end]
	// One word per field rather than a byte at a time: a frame writes
	// thousands of these, and shifting each float into four separate byte
	// stores was a measurable share of the upload cost.
	put := func(at int, v float32) {
		binary.LittleEndian.PutUint32(rec[at:], math.Float32bits(v))
	}
	// pos (vec4, 16 bytes): the w lane is unused by the shader and stays zero.
	put(0, pos[0])
	put(4, pos[1])
	put(8, pos[2])
	put(12, 0)
	// radius (float, 4 bytes) at offset 16 within the record.
	put(16, radius)
	// 12 bytes of std430 padding before tint, kept zero however often the
	// record is rewritten.
	clear(rec[20:32])
	// tint (vec4, 16 bytes) at offset 32.
	put(32, tint[0])
	put(36, tint[1])
	put(40, tint[2])
	put(44, tint[3])
	// orientation (vec4) at offset 48: angle, fixed-plane flag,
	// self-lit flag, padding.
	put(48, float32(sp.Rotation))
	mode := float32(0)
	if sp.Horizontal {
		mode = 1
		if sp.PlaneNormal != [3]float64{} && sp.PlaneNormal != [3]float64{0, 0, 1} {
			mode = 2
		}
	}
	put(52, mode)
	if sp.SelfLit {
		put(56, 1)
	} else {
		put(56, 0)
	}
	put(60, 0)
	// Plane normal (vec4) at offset 64. A zero normal in older/default sprite
	// values means the renderer's original world-Z horizontal plane.
	planeNormal := sp.PlaneNormal
	if planeNormal == [3]float64{} {
		planeNormal = [3]float64{0, 0, 1}
	}
	put(64, float32(planeNormal[0]))
	put(68, float32(planeNormal[1]))
	put(72, float32(planeNormal[2]))
	put(76, 0)
}

// appendRibbonInput writes one compact ribbon record into the compute input
// buffer, and reports how many vertices the expand pass will write for it.
// It matches the std430 Ribbon struct in fx_vk_ribbon.comp.
//
// The record carries what fx.BeamQuads needs to tessellate without asking the
// CPU anything: the ends, the bow, the cross-section a ring pins to its own
// plane (with w set), the camera a beam's eye-facing one is measured from,
// and where this ribbon's output starts, in vertices from the beginning of
// the ribbon block. A ribbon the tessellator would refuse — an empty span, a
// width of nothing, a cross-section with no perpendicular left — produces no
// record and no vertices, which is what fx.BeamQuads returning no quads
// amounts to, and keeps the group's declared vertex range honest.
func (r *fxVkRenderer) appendRibbonInput(rb fx.BeamRibbon, eye fauxgl.Vector, idx, vertOffset int) int {
	if idx < 0 || idx >= fxComputeMaxRibbons {
		return 0
	}
	axis := rb.To.Sub(rb.From)
	length := axis.Length()
	if length < 1e-9 || rb.Width <= 0 {
		return 0
	}
	segments := int(rb.Segments)
	if segments < 1 {
		segments = 1
	}
	if segments > fx.MaxBeamSegments {
		segments = fx.MaxBeamSegments
	}
	// The cross-section check fx.BeamQuads makes before it has anything to
	// widen: a ribbon with no plane of its own, seen exactly along its span,
	// has no perpendicular to speak of, and there the CPU path draws nothing.
	dir := axis.MulScalar(1 / length)
	across := rb.Across
	if !rb.Fixed {
		across = eye.Sub(rb.From).Cross(dir)
	}
	if n := across.Length(); n < 1e-6 {
		across = dir.Cross(fauxgl.Vector{Z: 1})
		if n := across.Length(); n < 1e-6 {
			across = dir.Cross(fauxgl.Vector{X: 1})
		}
		if n := across.Length(); n < 1e-6 {
			return 0
		}
	}
	base := fxComputeRibbonCountFloats*4 + idx*fxComputeRibbonStrideFloats*4
	end := base + fxComputeRibbonStrideFloats*4
	if end > len(r.ribbonBytes) {
		return 0
	}
	rec := r.ribbonBytes[base:end]
	put := func(at int, v float32) {
		binary.LittleEndian.PutUint32(rec[at:], math.Float32bits(v))
	}
	putVec := func(at int, v fauxgl.Vector) {
		put(at, float32(v.X))
		put(at+4, float32(v.Y))
		put(at+8, float32(v.Z))
		put(at+12, 0)
	}
	putVec(0, rb.From)
	putVec(16, rb.To)
	putVec(32, rb.Arc)
	putVec(48, rb.Across)
	fixed := float32(0)
	if rb.Fixed {
		fixed = 1
	}
	put(60, fixed) // across.w: the shader's flag for a self-pinned plane
	put(64, float32(clampFloat64(rb.R, 0, 1)))
	put(68, float32(clampFloat64(rb.G, 0, 1)))
	put(72, float32(clampFloat64(rb.B, 0, 1)))
	put(76, float32(clampFloat64(rb.Alpha, 0, 1)))
	put(80, float32(rb.Width))
	put(84, float32(rb.Noise))
	put(88, float32(rb.U0))
	put(92, float32(rb.U1))
	putVec(96, eye)
	// props is a uvec4: segments, flags, the output offset, padding.
	binary.LittleEndian.PutUint32(rec[112:], uint32(segments))
	var flags uint32
	if rb.FlipV {
		flags |= 1
	}
	if rb.SelfLit {
		flags |= 2
	}
	binary.LittleEndian.PutUint32(rec[116:], flags)
	binary.LittleEndian.PutUint32(rec[120:], uint32(vertOffset))
	binary.LittleEndian.PutUint32(rec[124:], 0)
	return segments * 6
}

// divCeil returns a/b rounded up.
func divCeil(a, b int) int {
	return (a + b - 1) / b
}

// appendBeam writes one ribbon as a triangle strip of quads.
//
// The particle path tells the vertex shader how big a quad is and lets the
// shader place it along the camera's own right and up. A beam has no such
// thing: its corners were worked out on the CPU, already in model space and
// already turned to face the eye. So the billboard term is switched off by
// sending a zero corner, which leaves the shader adding nothing to the
// position it was given, and the position goes through as it stands.
//
// The reason a beam is built here rather than in the shader is that its
// cross-section has to be perpendicular to the beam and as close to the eye as
// that allows — a per-vertex decision that needs the axis, the eye and the
// noise curve together. A beam is a dozen vertices, so it is cheap; the part
// that costs anything, filling in the ribbon and shading it, stays on the GPU.
func (r *fxVkRenderer) appendBeam(bm fxGpuBeam) {
	verts := fx.BeamQuads(bm.Ribbon, bm.Eye)
	if len(verts) < 4 {
		return
	}
	// The vertex buffer is indexed by u16, and one beam never comes close to
	// the limit, but the strip is written as its own triangle list so a beam
	// cannot read past its own corners.
	idx := fx.BeamTriangles(verts)
	if len(idx) == 0 {
		return
	}
	alpha := float32(clampFloat64(bm.Ribbon.Alpha, 0, 1))
	tint := [4]float32{
		float32(clampFloat64(bm.Ribbon.R, 0, 1)),
		float32(clampFloat64(bm.Ribbon.G, 0, 1)),
		float32(clampFloat64(bm.Ribbon.B, 0, 1)),
		alpha,
	}
	dir := bm.Ribbon.To.Sub(bm.Ribbon.From).Normalize()
	across := verts[1].Pos.Sub(verts[0].Pos).Normalize()
	normal := dir.Cross(across).Normalize()
	if bm.Ribbon.Fixed {
		normal = across.Cross(dir).Normalize()
	}
	litFlag := float32(0)
	if bm.Ribbon.SelfLit {
		litFlag = 1
	}
	for i := range idx {
		v := verts[idx[i]]
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.Pos.X)))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.Pos.Y)))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.Pos.Z)))
		// The zero corner: this is what turns the billboard off.
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(0))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(0))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(1))
		for _, ch := range tint {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(ch))
		}
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.U)))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.V)))
		orientation := [4]float32{0, 2, litFlag, 0}
		for _, component := range orientation {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(component))
		}
		for _, component := range []float64{normal.X, normal.Y, normal.Z, 0} {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(component)))
		}
	}
}

// appendOverlayLine writes one skeleton bone or marker arm as a camera facing
// quad.
//
// The cross-section comes from the beam tessellator rather than from a second
// implementation: a segment turned across to face the eye is exactly the
// problem fx.BeamQuads already solves, and a line drawn by its own arithmetic
// would be a line that slowly disagreed with every beam beside it as the
// camera moved. So the segment is dressed up as a ribbon of one step — the
// width is the half width, there is no bow and no noise — and the corners come
// back already in model space and already facing the eye.
//
// What differs from a beam is the map. An overlay has no texture of its own,
// so every corner reads the middle of the one texel the overlay map holds,
// which is what keeps a bone the flat colour it asks for.
func (r *fxVkRenderer) appendOverlayLine(s modelOverlayLine, eye fauxgl.Vector) {
	verts := fx.BeamQuads(fx.BeamRibbon{
		From:     s.From,
		To:       s.To,
		Width:    s.HalfWidth,
		R:        s.R,
		G:        s.G,
		B:        s.B,
		Alpha:    s.A,
		Segments: 1,
	}, eye)
	if len(verts) < 4 {
		return
	}
	idx := fx.BeamTriangles(verts)
	if len(idx) == 0 {
		return
	}
	tint := [4]float32{
		float32(clampFloat64(s.R, 0, 1)),
		float32(clampFloat64(s.G, 0, 1)),
		float32(clampFloat64(s.B, 0, 1)),
		float32(clampFloat64(s.A, 0, 1)),
	}
	for i := range idx {
		v := verts[idx[i]]
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.Pos.X)))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.Pos.Y)))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(float32(v.Pos.Z)))
		// The zero corner again: the quad is already built, so the
		// billboard term has to add nothing to it.
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(0))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(0))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(1))
		for _, ch := range tint {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(ch))
		}
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(0.5))
		r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(0.5))
		for _, component := range []float32{0, 2, 1, 0, 0, 0, 1, 0} {
			r.vertsData = binary.LittleEndian.AppendUint32(r.vertsData, math.Float32bits(component))
		}
	}
}
