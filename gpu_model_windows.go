//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
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

// Hardware rendering of a decoded .whm through Gio's Direct3D 11 backend.
//
// The software path in model3d.go rasterises on the CPU, which costs real time
// on a character model and rules out animation at any usable rate. This path
// puts the same geometry on the GPU instead:
//
//   - skinning happens in the vertex shader, so a new frame is just a new set
//     of bone matrices;
//   - textures stay in the block compressed form the .rsh file holds, so
//     nothing is decoded on the CPU and a 1024 square diffuse map costs 1.3
//     megabytes of upload instead of four;
//   - the result is a texture Gio paints directly, with no readback and no
//     copy in between.
//
// Everything runs on the same device and immediate context as Gio, on the
// goroutine that calls Frame and before it, so no synchronisation is needed:
// the immediate context is ordered and these commands simply come first.

// modelGpuVertexStride is the size of one skinned vertex in bytes.
const modelGpuVertexStride = 64

// modelGpuMaxBones is how many bone matrices fit in the constant buffer. A
// Dawn of War skeleton has a few dozen, so this is generous; a bone past the
// end loses its weight when the vertices are built, which leaves those
// vertices where they are instead of collapsing them to the origin.
const modelGpuMaxBones = 256

// modelGpuConstFloats is the size of the shared constant buffer: the view
// projection, the light, the shade term, a tint, the texture mapping, and the
// bone matrices. The order has to match modelGpuVertexShader, so the writes in
// writeConstants are the contract.
const modelGpuConstFloats = 32 + modelGpuMaxBones*16

// modelGpuVertexShader skins with up to four bones per vertex and transforms
// with the view projection. A vertex with no weights is left where it is, so an
// unbound mesh still shows up.
//
// Shade.y carries the texture rotation in quarter turns and UV carries the rest
// of the mapping — its xy move the map over the model, z scales it, w mirrors
// it. It is applied here rather than to the vertex buffer because the buffer is
// built once per model and the reader changes the mapping without a reload.
// uvTransform on the CPU is the same function, in the same order, which is what
// keeps the two paths sampling the same texel of the same map.
const modelGpuVertexShader = `
cbuffer Consts : register(b0) {
	float4x4 ViewProj;
	float4   Light; // xyz points towards the light, w is the ambient term
	float4   Shade; // x is the diffuse term, y the texture rotation in quarter turns
	float4   Tint;
	float4   UV;    // xy offset, z scale, w mirror
	float4x4 Bones[256];
};

struct VSIn {
	float3 pos : POSITION;
	float3 nrm : NORMAL;
	float2 uv  : TEXCOORD0;
	float4 bon : TEXCOORD1;
	float4 wgt : TEXCOORD2;
};

struct VSOut {
	float4 pos : SV_POSITION;
	float3 nrm : TEXCOORD0;
	float2 uv  : TEXCOORD1;
};

// rotateUV turns the coordinates a quarter turn at a time. The turns are
// folded into 0..3 with float arithmetic rather than an integer modulus: the
// shader compiler warns about the modulus, and this compiler turns a warning
// into a failed compile, which is how a texture turn cost the whole pipeline.
float2 rotateUV(float2 uv, float turns) {
	float k = floor(abs(turns) + 0.5);
	k = k - floor(k / 4.0) * 4.0;
	if (k == 1.0) {
		return float2(uv.y, -uv.x);
	}
	if (k == 2.0) {
		return float2(-uv.x, -uv.y);
	}
	if (k == 3.0) {
		return float2(-uv.y, uv.x);
	}
	return uv;
}

float2 uvTransform(float2 uv) {
	if (UV.w != 0) {
		uv.x = -uv.x;
	}
	if (UV.z > 0 && UV.z != 1) {
		uv *= UV.z;
	}
	return rotateUV(uv, Shade.y) + UV.xy;
}

VSOut main(VSIn v) {
	float4 p = float4(v.pos, 1);
	float3 n = v.nrm;
	float total = v.wgt.x + v.wgt.y + v.wgt.z + v.wgt.w;
	if (total > 0) {
		float4 acc = float4(0, 0, 0, 0);
		float3 nacc = float3(0, 0, 0);
		[unroll]
		for (int i = 0; i < 4; i++) {
			float w = v.wgt[i];
			if (w <= 0) {
				continue;
			}
			float4x4 m = Bones[int(v.bon[i])];
			acc += mul(m, p) * (w / total);
			nacc += mul((float3x3)m, n) * (w / total);
		}
		p = acc;
		n = nacc;
	}
	VSOut o;
	o.pos = mul(ViewProj, p);
	o.nrm = n;
	o.uv = uvTransform(v.uv);
	return o;
}
`

// modelGpuPixelShader is the same Lambert term the software renderer uses, over
// the sampled diffuse map, so a model looks the same on either path. A mesh
// with no material samples a one by one white map and is left to the tint.
//
// The coordinates are wrapped before they are sampled. A model's UVs run past
// the edge of its texture — a necron turret is a quarter of the map, tiled —
// and the sampler clamps, which smears the last row of pixels over the whole
// overhang. The software path wraps too, so this is what keeps the two looking
// the same.
//
// v is taken as the model wrote it and from mip 0: a .rsh stores its map top
// down, which is the convention the coordinate was authored against, and the
// software path samples the top level and nothing else.
const modelGpuPixelShader = `
cbuffer Consts : register(b0) {
	float4x4 ViewProj;
	float4   Light; // xyz points towards the light, w is the ambient term
	float4   Shade; // x is the diffuse term
	float4   Tint;
	float4x4 Bones[256];
};

Texture2D    tex0  : register(t0);
SamplerState samp0 : register(s0);

float4 main(float3 nrm : TEXCOORD0, float2 uv : TEXCOORD1) : SV_TARGET {
	float3 n = normalize(nrm);
	float lambert = max(dot(n, Light.xyz), 0);
	float f = min(Light.w + Shade.x * lambert, 1);
	float3 c = tex0.SampleLevel(samp0, frac(uv), 0).rgb * Tint.rgb * f;
	return float4(c, 1);
}
`

// panelBackground is the colour the 3D panel is filled with, which is also what
// a pass clears to.
var panelBackground = color.NRGBA{R: 0x1c, G: 0x1e, B: 0x24, A: 0xff}

// srgbToLinear decodes an sRGB byte into the linear value that encodes back to
// it, which is what a write to an sRGB target expects.
func srgbToLinear(v uint8) float32 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return float32(c)
	}
	return float32(math.Pow((c+0.055)/1.055, 2.4))
}

// linearToSrgb is the other half of srgbToLinear: it puts a linear value back
// into the byte an sRGB target would show for it.
//
// It is the same curve the hardware applies to a write, which is what makes it
// worth having: two frames that were blended by the GPU meet in linear space,
// so comparing them means decoding both and encoding the answer, and comparing
// the bytes directly would call a correct blend thirty shades out.
func linearToSrgb(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	if v <= 0.0031308 {
		return v * 12.92 * 255
	}
	return (1.055*math.Pow(v, 1/2.4) - 0.055) * 255
}

// fadeTintToBackground pulls a tint towards the panel colour by 1 - alpha, so
// a mesh a clip is fading out of is drawn as the panel showing through it.
//
// The frame's target is sRGB, which means what a draw writes is linear, and a
// tint multiplies the map in that same space. Fading towards the background is
// therefore the background's own linear value, not its byte value: mixing in
// the bytes of a dark panel as if they were linear darkens it twice over and
// the mesh sinks into black before it fades at all.
//
// It is not real transparency, and it is honest about where that shows: a
// fading mesh drawn over another one does not show it through itself, because
// this path's pipeline cannot mix. That is the whole of what the Direct3D
// renderer falls back to; the Vulkan path writes premultiplied colour and lets
// the hardware mix, which is why this is the only one left here. Against the
// panel, which is what a fading piece of a model is seen against, the two
// agree.
func fadeTintToBackground(tint color.RGBA, alpha float32) color.RGBA {
	if alpha >= 1 {
		return tint
	}
	if alpha <= 0 {
		bg := panelBackground
		return color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: tint.A}
	}
	a := float64(alpha)
	mix := func(v uint8, bg uint8) uint8 {
		lin := float64(v) / 255
		out := lin*a + float64(srgbToLinear(bg))*(1-a)
		if out <= 0 {
			return 0
		}
		if out >= 1 {
			return 255
		}
		return uint8(out*255 + 0.5)
	}
	bg := panelBackground
	return color.RGBA{
		R: mix(tint.R, bg.R),
		G: mix(tint.G, bg.G),
		B: mix(tint.B, bg.B),
		A: tint.A,
	}
}

// modelGpuDraw is one sub mesh: a range of the index buffer and the material
// whose texture it samples.
// modelGpuDraw is one sub mesh: the index buffer that holds its triangles and
// how many of them there are, with the material whose texture it samples.
//
// There is no index offset in a draw call, so each sub mesh gets a buffer of
// its own rather than a range of one shared buffer. Sharing worked as long as
// a model had a single sub mesh, which is how it managed to draw the first
// mesh of the Summoning Core eight times and nothing else.
type modelGpuDraw struct {
	indexes  *gpu.D3D11Buffer
	count    int
	material string
	// name is the sub mesh this draw is, which is what a clip's visibility
	// track is named after.
	name string
}

// modelGpuMesh is the GPU copy of a model's geometry. It never changes, so it
// is built once per model and kept for as long as the tab is open.
type modelGpuMesh struct {
	vertices *gpu.D3D11Buffer
	draws    []modelGpuDraw
}

// modelGpuTarget is the offscreen colour and depth pair a frame is drawn into.
// It is recreated when the panel changes size.
type modelGpuTarget struct {
	color *gpu.D3D11Texture
	depth *gpu.D3D11Texture
	size  image.Point
}

// modelGpuRenderer holds the device resources shared by every 3D preview: the
// pipeline, the constant buffer, and the cache of uploaded models and textures.
type modelGPURenderer struct {
	d3d      *gpu.D3D11
	pipeline *gpu.D3D11Pipeline
	uniforms *gpu.D3D11Buffer
	white    *gpu.D3D11Texture
	meshes   map[*WhmModel]*modelGpuMesh
	// textures is keyed by material and holds both the texture and the file
	// bytes its mip slices point into.
	textures  map[string]*modelGpuTexture
	offscreen *modelGpuTarget
	consts    []float32
	scratch   []byte
}

// modelGpuTexture is one material's diffuse map, still in the block compressed
// form the .rsh file stores it in.
type modelGpuTexture struct {
	tex    *gpu.D3D11Texture
	mips   []RshMip
	format int32
	data   []byte
}

// newModelGpuRenderer compiles the pipeline and the buffers a frame needs.
func newModelGpuRenderer(d3d *gpu.D3D11) (*modelGPURenderer, error) {
	vs, err := d3d.CompileHLSL(modelGpuVertexShader, "main", "vs_5_0")
	if err != nil {
		return nil, fmt.Errorf("vertex shader: %w", err)
	}
	ps, err := d3d.CompileHLSL(modelGpuPixelShader, "main", "ps_5_0")
	if err != nil {
		return nil, fmt.Errorf("pixel shader: %w", err)
	}
	pipeline, err := d3d.NewPipeline(gpu.D3D11PipelineDesc{
		Vertex:   vs,
		Fragment: ps,
		Stride:   modelGpuVertexStride,
		// Direct3D's rasteriser treats a triangle as front facing the other
		// way round from what the winding in the file means, so culling the
		// front faces is what keeps the outside of a model on screen. Cull
		// the other side and every model shows the inside of its shell.
		Cull:       gpu.CullFront,
		DepthTest:  true,
		DepthWrite: true,
		Inputs: []gpu.D3D11Input{
			{Semantic: "POSITION", Format: gpu.VertexFormatFloat32x3, Offset: 0},
			{Semantic: "NORMAL", Format: gpu.VertexFormatFloat32x3, Offset: 12},
			{Semantic: "TEXCOORD", Index: 0, Format: gpu.VertexFormatFloat32x2, Offset: 24},
			{Semantic: "TEXCOORD", Index: 1, Format: gpu.VertexFormatFloat32x4, Offset: 32},
			{Semantic: "TEXCOORD", Index: 2, Format: gpu.VertexFormatFloat32x4, Offset: 48},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("pipeline: %w", err)
	}
	uniforms, err := d3d.NewBuffer(gpu.ConstantBuffer, modelGpuConstFloats*4, true)
	if err != nil {
		pipeline.Release()
		return nil, fmt.Errorf("constant buffer: %w", err)
	}
	// A one by one white map stands in for a missing material, so untextured
	// meshes need no second pipeline.
	white, err := d3d.NewTexture(gpu.D3D11TextureDesc{
		Width:   1,
		Height:  1,
		Format:  gpu.FormatR8G8B8A8UNormSRGB,
		Sampled: true,
	})
	if err != nil {
		uniforms.Release()
		pipeline.Release()
		return nil, fmt.Errorf("white texture: %w", err)
	}
	if err := white.UploadMip(0, []byte{0xff, 0xff, 0xff, 0xff}); err != nil {
		white.Release()
		uniforms.Release()
		pipeline.Release()
		return nil, fmt.Errorf("white texture upload: %w", err)
	}
	return &modelGPURenderer{
		d3d:      d3d,
		pipeline: pipeline,
		uniforms: uniforms,
		white:    white,
		meshes:   map[*WhmModel]*modelGpuMesh{},
		textures: map[string]*modelGpuTexture{},
		consts:   make([]float32, modelGpuConstFloats),
		scratch:  make([]byte, modelGpuConstFloats*4),
	}, nil
}

// forget drops the cached geometry of a model, which is what a closing tab
// needs: the buffers are sized after the model, and a session that opens a few
// hundred of them would otherwise never give any of it back.
func (r *modelGPURenderer) forget(model *WhmModel) {
	m, ok := r.meshes[model]
	if !ok {
		return
	}
	delete(r.meshes, model)
	m.release()
}

// release frees the buffers of one mesh.
func (m *modelGpuMesh) release() {
	if m == nil {
		return
	}
	m.vertices.Release()
	for _, d := range m.draws {
		d.indexes.Release()
	}
}

// release frees every device resource the renderer owns.
func (r *modelGPURenderer) release() {
	if r == nil {
		return
	}
	for _, m := range r.meshes {
		m.release()
	}
	for _, t := range r.textures {
		t.tex.Release()
	}
	if r.offscreen != nil {
		r.offscreen.color.Release()
		r.offscreen.depth.Release()
	}
	r.white.Release()
	r.uniforms.Release()
	r.pipeline.Release()
}

// mesh returns the GPU copy of a model, building it on first use.
func (r *modelGPURenderer) mesh(model *WhmModel) (*modelGpuMesh, error) {
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

// buildMesh interleaves the vertices into the layout the vertex shader reads
// and collects one draw range per sub mesh.
func (r *modelGPURenderer) buildMesh(model *WhmModel) (*modelGpuMesh, error) {
	if len(model.Vertices) > 0xffff {
		// The index buffer is 16 bit. A model this large has to go through the
		// software path, whose indices are 32 bit.
		return nil, fmt.Errorf("model has %d vertices, more than a 16 bit index buffer holds", len(model.Vertices))
	}
	verts := make([]byte, len(model.Vertices)*modelGpuVertexStride)
	put32 := func(at int, v float32) {
		binary.LittleEndian.PutUint32(verts[at:], math.Float32bits(v))
	}
	for i := range model.Vertices {
		v := &model.Vertices[i]
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
	total := 0
	for _, m := range model.Meshes {
		total += len(m.Indices)
	}
	draws := make([]modelGpuDraw, 0, len(model.Meshes))
	vbuf, err := r.d3d.NewBuffer(gpu.VertexBuffer, len(verts), false)
	if err != nil {
		return nil, fmt.Errorf("vertex buffer: %w", err)
	}
	if err := vbuf.Upload(verts); err != nil {
		vbuf.Release()
		return nil, fmt.Errorf("vertex upload: %w", err)
	}
	for _, m := range model.Meshes {
		if len(m.Indices) == 0 {
			continue
		}
		idx := make([]byte, 0, len(m.Indices)*2)
		for _, i := range m.Indices {
			if i > 0xffff {
				vbuf.Release()
				for _, d := range draws {
					d.indexes.Release()
				}
				return nil, fmt.Errorf("mesh %q indexes vertex %d, out of range", m.Name, i)
			}
			idx = binary.LittleEndian.AppendUint16(idx, uint16(i))
		}
		ibuf, err := r.d3d.NewBuffer(gpu.IndexBuffer, len(idx), false)
		if err != nil {
			vbuf.Release()
			for _, d := range draws {
				d.indexes.Release()
			}
			return nil, fmt.Errorf("index buffer: %w", err)
		}
		if err := ibuf.Upload(idx); err != nil {
			vbuf.Release()
			ibuf.Release()
			for _, d := range draws {
				d.indexes.Release()
			}
			return nil, fmt.Errorf("index upload: %w", err)
		}
		draws = append(draws, modelGpuDraw{
			indexes:  ibuf,
			count:    len(m.Indices),
			material: m.Material,
			name:     m.Name,
		})
	}
	return &modelGpuMesh{vertices: vbuf, draws: draws}, nil
}

// materialTexture returns the texture for a material, uploading the mip chain
// of the .rsh file as it is stored the first time it is asked for. The file
// bytes are kept by the returned entry, because the mip slices point into them.
func (r *modelGPURenderer) materialTexture(material string, data []byte) (*gpu.D3D11Texture, error) {
	if material == "" || len(data) == 0 {
		return r.white, nil
	}
	entry, ok := r.textures[material]
	if !ok {
		info, mips, err := RshRawImage(data, 0)
		if err != nil {
			// A material that cannot be read is not fatal: the mesh is drawn
			// with the base colour instead.
			log.Printf("3D preview: texture %q unreadable, using white: %v", material, err)
			r.textures[material] = &modelGpuTexture{}
			return r.white, nil
		}
		dxgi, ok := rshDxgiFormat(info.Format)
		if !ok || len(mips) == 0 {
			log.Printf("3D preview: texture %q unsupported (format=%d mips=%d), using white", material, info.Format, len(mips))
			r.textures[material] = &modelGpuTexture{}
			return r.white, nil
		}
		// Uploaded as an sRGB texture, which is what a diffuse map is: the
		// shader lights linear values and Gio's blit puts the result back
		// through the same curve.
		tex, err := r.d3d.NewTexture(gpu.D3D11TextureDesc{
			Width:     mips[0].Width,
			Height:    mips[0].Height,
			MipLevels: len(mips),
			Format:    dxgi,
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
		entry = &modelGpuTexture{tex: tex, mips: mips, format: info.Format, data: data}
		r.textures[material] = entry
		// The first blocks of the top level say whether the bytes about to be
		// sampled hold an image: two zero endpoints decode to solid black,
		// which is what a frame full of black texture looks like.
		head := ""
		if d := mips[0].Data; len(d) >= 32 {
			var sb strings.Builder
			for i := 0; i < 4; i++ {
				c0 := binary.LittleEndian.Uint16(d[i*8:])
				c1 := binary.LittleEndian.Uint16(d[i*8+2:])
				fmt.Fprintf(&sb, " %04x/%04x", c0, c1)
			}
			// Average brightness of the first endpoint of every block: a real
			// map lands in the tens or hundreds, an all-zero chain lands on 0.
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

// rshDxgiFormat maps a .rsh surface format onto the sRGB variant of its block
// compressed DXGI format, so the shader can sample it directly.
func rshDxgiFormat(format int32) (gpu.D3D11Format, bool) {
	switch format {
	case 8:
		return gpu.FormatBC1UNormSRGB, true
	case 10:
		return gpu.FormatBC2UNormSRGB, true
	case 11:
		return gpu.FormatBC3UNormSRGB, true
	}
	return 0, false
}

// target returns the offscreen render target, recreated when the size changes.
func (r *modelGPURenderer) renderTarget(size image.Point) (*modelGpuTarget, error) {
	size.X = maxInt(1, size.X)
	size.Y = maxInt(1, size.Y)
	if r.offscreen != nil && r.offscreen.size == size {
		return r.offscreen, nil
	}
	if r.offscreen != nil {
		// Gio may still be holding the texture the frame on screen was
		// painted from, so a recreate during a drag resize is worth
		// seeing in the log.
		log.Printf("3D preview: render target %dx%d -> %dx%d", r.offscreen.size.X, r.offscreen.size.Y, size.X, size.Y)
		r.offscreen.color.Release()
		r.offscreen.depth.Release()
	}
	color, err := r.d3d.NewTexture(gpu.D3D11TextureDesc{
		Width:        size.X,
		Height:       size.Y,
		Format:       gpu.FormatR8G8B8A8UNormSRGB,
		RenderTarget: true,
		Sampled:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("colour target: %w", err)
	}
	depth, err := r.d3d.NewTexture(gpu.D3D11TextureDesc{
		Width:  size.X,
		Height: size.Y,
		Format: gpu.FormatD32Float,
		Depth:  true,
	})
	if err != nil {
		color.Release()
		return nil, fmt.Errorf("depth target: %w", err)
	}
	r.offscreen = &modelGpuTarget{color: color, depth: depth, size: size}
	return r.offscreen, nil
}

// Render draws one frame and returns the texture Gio should paint. The caller
// passes it to paint.NewImageOpFromTexture, which hands the same GPU memory to
// Gio's blitter with nothing copied in between. rsh resolves a material name to
// the raw bytes of its .rsh file; it may be nil, which leaves every mesh
// untextured.
func (r *modelGPURenderer) Render(
	model *WhmModel,
	pose WhmPose,
	opts RenderOptions,
	materials map[string]WhmMaterial,
	rsh func(material string) ([]byte, bool),
) (modelGpuFrame, error) {
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
	view := buildViewMatrix(opts)

	r.d3d.BeginPass(target.color, target.depth, image.Rectangle{
		Min: image.Point{},
		Max: target.size,
	}, true, color.NRGBA{})
	// The pass clears the depth buffer, which the draws need. The colour is
	// then cleared again in floating point: the target is an sRGB texture, so
	// a write to it goes through the sRGB curve, and a dark panel colour has no
	// room left once it has been squeezed into an eight bit linear value.
	bg := panelBackground
	target.color.Clear(
		srgbToLinear(bg.R), srgbToLinear(bg.G), srgbToLinear(bg.B), 1,
	)
	defer r.d3d.EndPass()
	r.d3d.SetPipeline(r.pipeline)
	r.d3d.SetVertexBuffer(mesh.vertices, 0)

	// The camera and the skeleton are the same for every sub mesh, so the
	// constant buffer is only rewritten when something in it changes: the tint,
	// or the mapping, which is per material and so can differ from one draw to
	// the next. A model with one map still writes it once.
	written := false
	var writtenTint color.RGBA
	var writtenUV uvMapping
	for _, d := range mesh.draws {
		// A clip that holds a mesh back does not draw it, and one it fades
		// is drawn towards the panel colour: see fadeTintToBackground.
		alpha := meshAlphaFor(opts, d.name)
		if alpha <= 0 {
			continue
		}
		tex := r.white
		tint := opts.BaseColor
		if tint == (color.RGBA{}) {
			tint = rgbaWhite
		}
		if mat, ok := materials[d.material]; ok {
			if mat.Color != (color.RGBA{}) {
				tint = mat.Color
			}
			if rsh != nil {
				if data, ok := rsh(d.material); ok {
					if tex, err = r.materialTexture(d.material, data); err != nil {
						return nil, err
					}
				}
			}
		}
		if alpha < 1 {
			tint = fadeTintToBackground(tint, alpha)
		}
		mapping := renderUVFor(opts, d.material)
		if !written || tint != writtenTint || mapping != writtenUV {
			r.writeConstants(view, pose, opts, tint, mapping)
			if err := r.uploadConstants(); err != nil {
				return nil, err
			}
			written, writtenTint, writtenUV = true, tint, mapping
		}
		r.d3d.SetUniforms(r.uniforms)
		r.d3d.SetTexture(0, tex)
		r.d3d.SetIndexBuffer(d.indexes)
		r.d3d.DrawIndexed(d.count)
	}
	return &modelGpuWindowsFrame{tex: target.color, size: target.size}, nil
}

// modelGpuWindowsFrame is one rendered frame, still in video memory. Painting
// it hands the texture to Gio's blitter, which is the only copy in the chain.
type modelGpuWindowsFrame struct {
	tex  *gpu.D3D11Texture
	size image.Point
}

// probeGpuFrame reads a finished frame back and reports what it actually
// contains: how much is the cleared panel background, how much is dark but not
// background, and how much is visibly lit. A grey window used to be
// indistinguishable between draws that never landed, textures that sample to
// black, and a texture that is fine and simply never painted.
//
// A probe is a diagnostic, and a diagnostic may not be the thing that kills
// the window. Reading the texture back is a blocking submitAndWait, and on a
// device that has already been lost — which is what a card answers with when a
// frame has taken it past the point of no return — Gio panics from inside
// ReadPixels rather than returning an error, so the error check below never
// sees it. Pressing play on an animation with the effects on was enough to take
// the whole program down over a log line nobody asked for.
//
// So the readback is contained here: a panic becomes a log line and a false,
// and the caller falls back to the software renderer. Nothing is lost but the
// measurement, which is the whole point of making it survivable.
func probeGpuFrame(f modelGpuFrame, context string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("3D preview: probe %s: GPU readback failed, not fatal: %v", context, r)
			ok = false
		}
	}()
	probeGpuFrameRead(f, context)
	return true
}

// probeGpuFrameRead is the measurement itself.
func probeGpuFrameRead(f modelGpuFrame, context string) {
	if os.Getenv("DOW_NO_GPU_PROBE") != "" {
		return
	}
	var (
		px   []byte
		err  error
		size image.Point
	)
	switch gf := f.(type) {
	case *modelGpuWindowsFrame:
		if gf == nil || gf.tex == nil {
			log.Printf("3D preview: probe %s: frame is not a GPU frame", context)
			return
		}
		size = gf.size
		px, err = gf.tex.ReadPixels()
	case *modelGpuVkFrame:
		if gf == nil || gf.tex == nil {
			log.Printf("3D preview: probe %s: frame is not a GPU frame", context)
			return
		}
		size = gf.size
		px, err = gf.tex.ReadPixels()
	default:
		log.Printf("3D preview: probe %s: frame is not a GPU frame", context)
		return
	}
	if err != nil {
		log.Printf("3D preview: probe %s: readback failed: %v", context, err)
		return
	}
	stride := size.X * 4
	rows := len(px) / stride
	bgR, bgG, bgB := int(panelBackground.R), int(panelBackground.G), int(panelBackground.B)
	total, clearPix, darkPix, litPix := 0, 0, 0, 0
	lumaSum, lumaMax := 0, 0
	for y := 0; y < rows; y++ {
		row := px[y*stride : (y+1)*stride]
		for x := 0; x+3 < len(row); x += 4 {
			r, g, b := int(row[x]), int(row[x+1]), int(row[x+2])
			total++
			if absInt(r-bgR) <= 2 && absInt(g-bgG) <= 2 && absInt(b-bgB) <= 2 {
				clearPix++
				continue
			}
			l := (r*299 + g*587 + b*114) / 1000
			lumaSum += l
			if l > lumaMax {
				lumaMax = l
			}
			if l >= 40 {
				litPix++
			} else {
				darkPix++
			}
		}
	}
	if total == 0 {
		log.Printf("3D preview: probe %s: empty readback (%d bytes)", context, len(px))
		return
	}
	modelPix := total - clearPix
	avg := 0
	if modelPix > 0 {
		avg = lumaSum / modelPix
	}
	log.Printf("3D preview: probe %s: clear=%d%% model=%d%% (dark=%d%% lit=%d%%) modelAvgLuma=%d max=%d",
		context, clearPix*100/total, modelPix*100/total,
		darkPix*100/total, litPix*100/total, avg, lumaMax)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Paint scales the frame to fit rect, the same way the software path scales its
// image, so switching renderers does not move anything on screen.
func (f *modelGpuWindowsFrame) Paint(gtx layout.Context, rect image.Rectangle) {
	if f == nil || f.tex == nil || rect.Dx() < 1 || rect.Dy() < 1 {
		return
	}
	tr := f.overlayTransform(rect).Push(gtx.Ops)
	paint.NewImageOpFromTexture(f.tex).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
}

// Overlay runs fn in the frame's own transform, so the marker ring lands on
// the model exactly where the frame put it. The Direct3D renderer is on its
// way out and does not draw the overlay itself; this keeps the frame type
// interchangeable with the Vulkan one.
func (f *modelGpuWindowsFrame) Overlay(gtx layout.Context, rect image.Rectangle, fn func(layout.Context)) {
	if f == nil || f.tex == nil || rect.Dx() < 1 || rect.Dy() < 1 || fn == nil {
		return
	}
	tr := f.overlayTransform(rect).Push(gtx.Ops)
	fn(gtx)
	tr.Pop()
}

func (f *modelGpuWindowsFrame) overlayTransform(rect image.Rectangle) op.TransformOp {
	scale := float32(rect.Dx()) / float32(f.size.X)
	if s := float32(rect.Dy()) / float32(f.size.Y); s < scale {
		scale = s
	}
	dx := (float32(rect.Dx()) - float32(f.size.X)*scale) / 2
	dy := (float32(rect.Dy()) - float32(f.size.Y)*scale) / 2
	return op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale)))
}

// Release does nothing: the texture belongs to the renderer, which recycles it.
func (f *modelGpuWindowsFrame) Release() {}

// modelGpuTooEarly records that the window had no device yet, which is a
// timing problem rather than a missing backend.
var modelGpuTooEarly bool

// newModelGpuForWindow opens a hardware renderer for the window — through
// Direct3D 11 when that is the window's backend, through Vulkan when that is
// — or returns nil when neither is there and the software renderer has to be
// used instead.
func newModelGpuForWindow(w *app.Window) modelGpuHandle {
	if w == nil {
		return nil
	}
	d3d, err := w.D3D11()
	switch {
	case err == nil:
		modelGpuTooEarly = false
		r, err := newModelGpuRenderer(d3d)
		if err != nil {
			log.Printf("3D preview: hardware renderer unavailable, falling back to software: %v", err)
			return nil
		}
		log.Printf("3D preview: hardware renderer ready, models are drawn on the GPU")
		return r
	case errors.Is(err, app.ErrNoGPUYet):
		modelGpuTooEarly = true
		return nil
	}
	// Not Direct3D 11 — the backend may be Vulkan.
	vkDev, err := w.Vulkan()
	if err != nil {
		if errors.Is(err, app.ErrNoGPUYet) {
			modelGpuTooEarly = true
			return nil
		}
		log.Printf("3D preview: no Vulkan device on the window either, using the software renderer: %v", err)
		return nil
	}
	modelGpuTooEarly = false
	r, err := newModelGpuVkRenderer(vkDev)
	if err != nil {
		log.Printf("3D preview: Vulkan renderer unavailable, falling back to software: %v", err)
		return nil
	}
	log.Printf("3D preview: Vulkan renderer ready, models are drawn on the GPU")
	return r
}

// uploadConstants writes the shared constant buffer from the scratch floats.
func (r *modelGPURenderer) uploadConstants() error {
	out := r.scratch
	for i, v := range r.consts {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	if err := r.uniforms.Upload(out); err != nil {
		return fmt.Errorf("uniforms: %w", err)
	}
	return nil
}

// writeConstants fills the shared constant buffer. The order has to match
// modelGpuVertexShader.
func (r *modelGPURenderer) writeConstants(view fauxgl.Matrix, pose WhmPose, opts RenderOptions, tint color.RGBA, mapping uvMapping) {
	c := r.consts
	hslslMatrix(c[0:16], view)
	light := opts.Light
	if light == (fauxgl.Vector{}) {
		// The same key light the software renderer defaults to.
		light = fauxgl.Vector{X: 0.35, Y: -0.5, Z: 0.78}
	}
	if n := light.Length(); n > 0 {
		light = light.Normalize()
	}
	ambient, diffuse := opts.Ambient, opts.Diffuse
	if ambient == 0 && diffuse == 0 {
		ambient, diffuse = 0.28, 0.72
	}
	c[16], c[17], c[18], c[19] = float32(light.X), float32(light.Y), float32(light.Z), float32(ambient)
	c[20], c[21], c[22], c[23] = float32(diffuse), float32(mapping.Rotate), 0, 0
	c[24], c[25], c[26], c[27] = float32(tint.R)/255, float32(tint.G)/255, float32(tint.B)/255, 1
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
	c[28], c[29], c[30], c[31] = mapping.Offset[0], mapping.Offset[1], scale, mirror
	bones := c[32:]
	clear(bones)
	// A bone past the constant buffer keeps the identity, so it leaves its
	// vertices alone rather than collapsing them to the origin.
	for i := range pose.Bones {
		if i >= modelGpuMaxBones {
			break
		}
		hslslMatrix(bones[i*16:], pose.SkinMatrix(int32(i)))
	}
}

// hslslMatrix writes a fauxgl matrix in the order HLSL reads a float4x4, which
// is column by column. fauxgl keeps its elements in the OpenGL row order, so
// this is a transpose, and what comes out is what mul(M, v) needs to reproduce
// m.MulPosition(v).
func hslslMatrix(dst []float32, m fauxgl.Matrix) {
	row := [16]float64{
		m.X00, m.X01, m.X02, m.X03,
		m.X10, m.X11, m.X12, m.X13,
		m.X20, m.X21, m.X22, m.X23,
		m.X30, m.X31, m.X32, m.X33,
	}
	for r := range 4 {
		for c := range 4 {
			dst[c*4+r] = float32(row[r*4+c])
		}
	}
}
