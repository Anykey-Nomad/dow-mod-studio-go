//go:build windows

package main

import (
	"image"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/gpu"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"github.com/fogleman/fauxgl"
)

// newLayoutContextForTest builds the minimal context Paint needs, which is only
// the op stream, the constraints and the metric scale.
func newLayoutContextForTest(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}

// The clear colour of a pass is the panel background.
const (
	bgR, bgG, bgB = 0x1c, 0x1e, 0x24
)

// hslslMul is what a vertex shader's mul(M, v) does with a matrix written by
// hslslMatrix. It exists to pin the memory layout: if the transpose is wrong,
// this disagrees with fauxgl and the model ends up somewhere else entirely.
func hslslMul(m fauxgl.Matrix, v fauxgl.Vector) fauxgl.Vector {
	var b [16]float32
	hslslMatrix(b[:], m)
	// float4x4 is stored as four columns of four rows, so column j starts at
	// b[4*j] and the output component i is the sum over columns of the i-th
	// row times that column's w.
	col := func(j int) (x, y, z, w float64) {
		return float64(b[4*j]), float64(b[4*j+1]), float64(b[4*j+2]), float64(b[4*j+3])
	}
	x0, y0, z0, w0 := col(0)
	x1, y1, z1, w1 := col(1)
	x2, y2, z2, w2 := col(2)
	x3, y3, z3, w3 := col(3)
	_ = w0 + w1 + w2 + w3
	return fauxgl.Vector{
		X: x0*v.X + x1*v.Y + x2*v.Z + x3,
		Y: y0*v.X + y1*v.Y + y2*v.Z + y3,
		Z: z0*v.X + z1*v.Y + z2*v.Z + z3,
	}
}

func TestHslslMatrixMatchesFauxgl(t *testing.T) {
	mats := []fauxgl.Matrix{
		fauxgl.Identity(),
		fauxgl.Translate(fauxgl.Vector{X: 5, Y: 6, Z: 7}),
		fauxgl.Scale(fauxgl.Vector{X: 2, Y: 0.5, Z: 3}),
		fauxgl.Rotate(fauxgl.Vector{X: 0, Y: 0, Z: 1}, 0.7),
		fauxgl.Rotate(fauxgl.Vector{X: 1, Y: 0, Z: 0}, -math.Pi/2),
		fauxgl.Perspective(45, 1.5, 0.1, 100),
		fauxgl.LookAt(fauxgl.Vector{X: 1, Y: 2, Z: 3}, fauxgl.Vector{}, fauxgl.Vector{X: 0, Y: 0, Z: 1}),
		boneRootFrame,
	}
	points := []fauxgl.Vector{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 2, Z: 3},
		{X: -0.5, Y: 0.25, Z: -4},
		{X: 0.1, Y: 0.2, Z: 0.3},
	}
	for mi, m := range mats {
		for pi, p := range points {
			want := m.MulPosition(p)
			got := hslslMul(m, p)
			if math.Abs(got.X-want.X) > 1e-4 || math.Abs(got.Y-want.Y) > 1e-4 || math.Abs(got.Z-want.Z) > 1e-4 {
				t.Errorf("matrix %d point %d: mul(M, v) = %v, fauxgl gives %v", mi, pi, got, want)
			}
		}
	}
}

// TestHslslMatrixMatchesFauxglOnBonePoses is the case that matters: a real
// skeleton's bone matrices have to survive the round trip, because that is what
// skins the mesh.
func TestHslslMatrixMatchesFauxglOnBonePoses(t *testing.T) {
	model := requireGpuTestModel(t, "lychguard.whm")
	pose := model.RestPose()
	for i, bone := range model.Bones {
		m := pose.SkinMatrix(int32(i))
		for _, p := range []fauxgl.Vector{{}, {X: 1, Y: 1, Z: 1}, {X: 0.2, Y: -0.7, Z: 0.4}} {
			want := m.MulPosition(p)
			got := hslslMul(m, p)
			if math.Abs(got.X-want.X) > 1e-4 || math.Abs(got.Y-want.Y) > 1e-4 || math.Abs(got.Z-want.Z) > 1e-4 {
				t.Fatalf("bone %q: mul(M, v) = %v, fauxgl gives %v", bone.Name, got, want)
			}
		}
	}
}

// findDowFile looks a file up by base name anywhere under the installed race
// folders, because the game scatters the same kind of file across troops,
// structures and shared texture folders.
func findDowFile(name string) []byte {
	want := strings.ToLower(name)
	var found []byte
	filepath.WalkDir(dowRacesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != nil {
			return nil //nolint:nilerr // an unreadable folder just means no match
		}
		if d.IsDir() || !strings.EqualFold(d.Name(), want) {
			return nil
		}
		if data, err := os.ReadFile(path); err == nil {
			found = data
		}
		return nil
	})
	return found
}

// requireGpuTestModel reads a model from the game install, skipping the test
// when there is none.
func requireGpuTestModel(t *testing.T, name string) *WhmModel {
	t.Helper()
	raw := findDowFile(name)
	if raw == nil {
		t.Skipf("no %s under %s", name, dowRacesDir)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatalf("ParseWhmModel(%s): %v", name, err)
	}
	return model
}

// requireGpuRenderer opens a headless window and a renderer on it. The returned
// readback copies the last frame back to the CPU, and the returned function
// tears everything down.
func requireGpuRenderer(t *testing.T) (r *modelGPURenderer, readback func() []byte, release func()) {
	t.Helper()
	const size = 256
	win, err := headless.NewWindow(size, size)
	if err != nil {
		t.Skipf("no GPU backend available: %v", err)
	}
	d3d, err := win.D3D11()
	if err != nil {
		win.Release()
		t.Skipf("Direct3D 11 is not the backend: %v", err)
	}
	r, err = newModelGpuRenderer(d3d)
	if err != nil {
		win.Release()
		t.Fatalf("newModelGpuRenderer: %v", err)
	}
	readback = func() []byte {
		t.Helper()
		px, err := r.offscreen.color.ReadPixels()
		if err != nil {
			t.Fatalf("ReadPixels: %v", err)
		}
		return px
	}
	release = func() {
		r.release()
		win.Release()
	}
	return r, readback, release
}

// gpuTestOptions frames a model the way a freshly opened viewer does.
func gpuTestOptions(model *WhmModel, w, h int) RenderOptions {
	center, dist := model.ModelCenter()
	return RenderOptions{
		Width:     w,
		Height:    h,
		Yaw:       0.7,
		Pitch:     0.2,
		Distance:  dist,
		Target:    center,
		Fov:       45,
		Ambient:   0.32,
		Diffuse:   0.68,
		BaseColor: rgbaWhite,
	}
}

// coveredFraction counts the pixels that are not the clear colour, which is
// how much of the frame the model actually reached.
func coveredFraction(px []byte, w, h int) float64 {
	hits := 0
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] != bgR || px[i+1] != bgG || px[i+2] != bgB {
			hits++
		}
	}
	return float64(hits) / float64(w*h)
}

// distinctColours counts the unique opaque colours in a frame, which is how a
// textured model tells itself apart from one painted in a flat colour.
func distinctColours(px []byte) int {
	seen := map[uint32]bool{}
	for i := 0; i+3 < len(px); i += 4 {
		if px[i] == bgR && px[i+1] == bgG && px[i+2] == bgB {
			continue
		}
		seen[uint32(px[i])<<16|uint32(px[i+1])<<8|uint32(px[i+2])] = true
	}
	return len(seen)
}

// difference counts the pixels two frames disagree on.
func difference(a, b []byte) int {
	if len(a) != len(b) {
		return len(a)
	}
	n := 0
	for i := 0; i+3 < len(a); i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			n++
		}
	}
	return n
}

// findRsh locates a material's .rsh by base name: a .whm stores a full virtual
// path, but the file itself may sit in a different mod than the model.
func findRsh(material string) []byte {
	if material == "" {
		return nil
	}
	return findDowFile(filepath.Base(filepath.FromSlash(material)) + ".rsh")
}

// findMaterialWithTexture returns the first material of a model whose .rsh is
// actually installed, since a model can reference a texture the install does
// not ship.
func findMaterialWithTexture(model *WhmModel) (string, []byte) {
	for _, m := range model.Meshes {
		if data := findRsh(m.Material); data != nil {
			return m.Material, data
		}
	}
	return "", nil
}

// TestGpuRendersRealModel renders a character model on the GPU and checks that
// something of a plausible size actually lands in the frame.
func TestGpuRendersRealModel(t *testing.T) {
	for _, name := range []string{"lychguard.whm", "necron_energy_core.whm"} {
		t.Run(name, func(t *testing.T) {
			model := requireGpuTestModel(t, name)
			const w, h = 256, 256
			r, readback, release := requireGpuRenderer(t)
			defer release()
			opts := gpuTestOptions(model, w, h)
			if _, err := r.Render(model, model.RestPose(), opts, nil, nil); err != nil {
				t.Fatalf("Render: %v", err)
			}
			px := readback()
			frac := coveredFraction(px, w, h)
			t.Logf("%s: %d vertices, %d meshes, %d bones, %.1f%% of the frame covered",
				name, len(model.Vertices), len(model.Meshes), len(model.Bones), frac*100)
			if frac < 0.02 || frac > 0.75 {
				logFrame(t, px, w, h)
			}
			if frac < 0.02 {
				t.Errorf("only %.1f%% of the frame was drawn, the model is missing", frac*100)
			}
			if frac > 0.75 {
				t.Errorf("%.1f%% of the frame was drawn, the camera is inside the model", frac*100)
			}
		})
	}
}

// TestGpuSkinningAnimatesTheMesh renders every clip of a model at two points
// in time and checks that at least one of them moves the mesh, which is the
// whole point of doing this on the GPU.
func TestGpuSkinningAnimatesTheMesh(t *testing.T) {
	model := requireGpuTestModel(t, "lychguard.whm")
	if len(model.Animations) == 0 {
		t.Skip("no animation clips in this model")
	}
	const w, h = 256, 256
	r, readback, release := requireGpuRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)
	best, bestName := 0, ""
	for i := range model.Animations {
		clip := &model.Animations[i]
		var frames [2][]byte
		for j, at := range [2]float32{0, 0.5} {
			if _, err := r.Render(model, model.PoseAt(clip, at), opts, nil, nil); err != nil {
				t.Fatalf("Render %q at %v: %v", clip.Name, at, err)
			}
			frames[j] = readback()
		}
		if n := difference(frames[0], frames[1]); n > best {
			best, bestName = n, clip.Name
		}
	}
	t.Logf("%d clips, busiest is %q at %d of %d pixels moved", len(model.Animations), bestName, best, w*h)
	if best < w*h/200 {
		t.Errorf("the busiest clip moved only %d pixels, the skeleton is not reaching the mesh", best)
	}
}

// TestGpuSamplesBlockCompressedTexture uploads a real .rsh and checks the frame
// carries actual image detail rather than a flat tint.
func TestGpuSamplesBlockCompressedTexture(t *testing.T) {
	var (
		model    *WhmModel
		material string
		data     []byte
	)
	for _, name := range []string{"lychguard.whm", "necron_energy_core.whm", "necron_lord.whm"} {
		m := requireGpuTestModel(t, name)
		if mat, d := findMaterialWithTexture(m); mat != "" {
			model, material, data = m, mat, d
			break
		}
	}
	if model == nil {
		t.Skip("no installed model has a .rsh next to it")
	}
	const w, h = 256, 256
	r, readback, release := requireGpuRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)
	// Every sub mesh carries its own material now, so all of them are given
	// the map; texturing one would draw only that material's face range with
	// image detail and the rest as a flat tint.
	mats := map[string]WhmMaterial{}
	for _, m := range model.Meshes {
		mats[m.Material] = WhmMaterial{Color: rgbaWhite}
	}
	rsh := func(string) ([]byte, bool) { return data, true }
	if _, err := r.Render(model, model.RestPose(), opts, mats, rsh); err != nil {
		t.Fatalf("Render: %v", err)
	}
	px := readback()
	colours := distinctColours(px)
	frac := coveredFraction(px, w, h)
	t.Logf("material %q: %.1f%% covered, %d distinct colours", material, frac*100, colours)
	if frac < 0.02 {
		t.Fatalf("only %.1f%% of the frame was drawn", frac*100)
	}
	// A flat tint would give a handful of shades from the lighting alone; a
	// diffuse map gives far more.
	if colours < 64 {
		t.Errorf("only %d distinct colours, the texture does not look sampled", colours)
	}
}

// logFrame draws a frame as a character map, which is the quickest way to see
// what a render actually produced.
func logFrame(t *testing.T, px []byte, w, h int) {
	t.Helper()
	const ramp = " .:-=+*#%@"
	for y := 0; y < h; y += maxInt(1, h/32) {
		line := make([]byte, 0, w/maxInt(1, w/64))
		for x := 0; x < w; x += maxInt(1, w/64) {
			o := (y*w + x) * 4
			if px[o] == bgR && px[o+1] == bgG && px[o+2] == bgB {
				line = append(line, ' ')
				continue
			}
			lum := (int(px[o])*3 + int(px[o+1])*6 + int(px[o+2])) / 10
			line = append(line, ramp[minInt(len(ramp)-1, lum*len(ramp)/256)])
		}
		t.Logf("|%s|", line)
	}
}

// TestGpuFrameSurvivesGioBlit is the whole chain the 3D preview uses: a model is
// drawn by the hardware renderer, the resulting texture is painted through
// Gio's normal op stream, and the frame that comes out of the window still
// carries the model. It is the only test that covers the hand over between the
// two, which is where a format or a blend mistake would show up as an empty or
// black preview.
func TestGpuFrameSurvivesGioBlit(t *testing.T) {
	model := requireGpuTestModel(t, "lychguard.whm")
	const w, h = 256, 256
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend available: %v", err)
	}
	defer win.Release()
	d3d, err := win.D3D11()
	if err != nil {
		t.Skipf("Direct3D 11 is not the backend: %v", err)
	}
	r, err := newModelGpuRenderer(d3d)
	if err != nil {
		t.Fatal(err)
	}
	defer r.release()
	frame, err := r.Render(model, model.RestPose(), gpuTestOptions(model, w, h), nil, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Paint it the way the 3D view does, and take the window's own frame back.
	var ops op.Ops
	clip.Rect{Max: image.Pt(w, h)}.Push(&ops)
	gpuFrame, ok := frame.(*modelGpuWindowsFrame)
	if !ok {
		t.Fatalf("Render returned %T, want the Windows frame", frame)
	}
	gpuFrame.Paint(newLayoutContextForTest(&ops, w, h), image.Rect(0, 0, w, h))
	paint.PaintOp{}.Add(&ops)
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	px := rgbaPixels(img)
	frac := coveredFraction(px, w, h)
	colours := distinctColours(px)
	t.Logf("after Gio's blit: %.1f%% covered, %d distinct colours", frac*100, colours)
	if frac < 0.02 {
		t.Errorf("the painted frame is empty, the model did not reach the window")
	}
	if colours < 8 {
		t.Errorf("only %d distinct colours, the frame looks like a flat fill", colours)
	}
}

// TestD3D11WritesEachMipLevelWhereItWasAsked uploads a texture whose levels
// each carry their own colour and reads every level back. A writer that drops
// the destination subresource puts them all on level 0, so levels 1 and up
// come back holding whatever level 0 last held: the uninitialised memory the
// GPU would then sample at a distance, which showed up as a field of small
// purple squares.
func TestD3D11WritesEachMipLevelWhereItWasAsked(t *testing.T) {
	const size = 64
	const levels = 4
	win, err := headless.NewWindow(size, size)
	if err != nil {
		t.Skipf("no GPU backend available: %v", err)
	}
	defer win.Release()
	d3d, err := win.D3D11()
	if err != nil {
		t.Skipf("Direct3D 11 is not the backend: %v", err)
	}
	tex, err := d3d.NewTexture(gpu.D3D11TextureDesc{
		Width: size, Height: size, MipLevels: levels,
		Format: gpu.FormatR8G8B8A8UNorm, Sampled: true,
	})
	if err != nil {
		t.Fatalf("NewTexture: %v", err)
	}
	defer tex.Release()
	// One flat colour per level, far enough apart that nothing a copy or a
	// blend could produce would land on the wrong one by chance.
	want := [levels][3]byte{
		{255, 0, 0}, {0, 255, 0}, {0, 0, 255}, {255, 255, 0},
	}
	for i, c := range want {
		w := maxInt(1, size>>uint(i))
		h := maxInt(1, size>>uint(i))
		px := make([]byte, 0, w*h*4)
		for range w * h {
			px = append(px, c[0], c[1], c[2], 255)
		}
		if err := tex.UploadMip(i, px); err != nil {
			t.Fatalf("UploadMip(%d): %v", i, err)
		}
	}
	for i, c := range want {
		px, err := tex.ReadPixelsMip(i)
		if err != nil {
			t.Fatalf("ReadPixelsMip(%d): %v", i, err)
		}
		if len(px) == 0 {
			t.Fatalf("mip %d read back empty", i)
		}
		// The middle texel, so a partial or misaligned write shows up.
		o := len(px) / 2 &^ 3
		got := [3]byte{px[o], px[o+1], px[o+2]}
		t.Logf("mip %d: got %v, want %v", i, got, c)
		if got != c {
			t.Errorf("mip %d holds %v, want %v: the level was written somewhere else", i, got, c)
		}
	}
}

// rgbaPixels copies an image into the tightly packed layout the readback
// helpers expect.
func rgbaPixels(img *image.RGBA) []byte {
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		out = append(out, img.Pix[y*img.Stride:y*img.Stride+b.Dx()*4]...)
	}
	return out
}
