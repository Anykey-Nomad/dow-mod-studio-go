//go:build windows || linux

package main

import (
	"image"
	"image/color"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whm"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"github.com/fogleman/fauxgl"
)

func requireVulkanRenderer(t *testing.T) (*headless.Window, *modelGpuVkRenderer, func() []byte, func()) {
	t.Helper()
	const size = 256
	win, err := headless.NewVulkanWindow(size, size)
	if err != nil {
		t.Skipf("no Vulkan backend available: %v", err)
	}
	v, err := win.Vulkan()
	if err != nil {
		win.Release()
		t.Skipf("Vulkan is not the backend: %v", err)
	}
	r, err := newModelGpuVkRenderer(v)
	if err != nil {
		win.Release()
		t.Fatalf("newModelGpuVkRenderer: %v", err)
	}
	readback := func() []byte {
		t.Helper()
		px, err := r.offscreen.color.ReadPixels()
		if err != nil {
			t.Fatalf("ReadPixels: %v", err)
		}
		return px
	}
	release := func() {
		r.release()
		win.Release()
	}
	return win, r, readback, release
}

// TestVulkanRendererDrawsTheSameShapeAsTheSoftwareRenderer frames a model
// with more than one sub mesh on both renderers: a mesh that never reaches
// the frame halves the coverage, which no amount of shading explains.
func TestVulkanRendererRebuildsInPlaceEditedGeometry(t *testing.T) {
	model := singleQuad(t, true)
	const w, h = 120, 120
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)
	renderCoverage := func() float64 {
		t.Helper()
		if _, err := r.Render(model, model.RestPose(), opts, nil, nil); err != nil {
			t.Fatalf("Render: %v", err)
		}
		return coveredFraction(readback(), w, h)
	}
	before := renderCoverage()
	if before <= 0.001 {
		t.Fatalf("initial geometry only covered %.3f of the frame", before)
	}

	// Move the WHM geometry without changing the model pointer or mesh list,
	// which matches SGB drag/rotate's in-place scene edits.
	for i := range model.Vertices {
		model.Vertices[i].Pos[0] += 1000
	}
	r.geometryChanged(model)
	after := renderCoverage()
	t.Logf("covered fraction before/after in-place vertex edit: %.4f / %.4f", before, after)
	if after >= before*0.1 {
		t.Fatalf("edited geometry still covers %.3f of the frame (before %.3f): stale GPU buffers were reused", after, before)
	}
}

func TestVulkanRendererDrawsTheSameShapeAsTheSoftwareRenderer(t *testing.T) {
	model := twoBoxModel(t)
	center, distance := model.ModelCenter()
	const w, h = 200, 160
	opts := RenderOptions{
		Width:    w,
		Height:   h,
		Yaw:      0.6,
		Pitch:    0.3,
		Distance: distance,
		Target:   center,
		Fov:      modelDefaultFov,
		Quality:  modelQualityFinal,
	}
	soft := RenderWhmModel(model, model.RestPose(), opts)
	softCover := coveredPixels(soft, image.Rect(0, 0, w, h))
	if softCover == 0 {
		t.Fatal("the software renderer drew nothing at all")
	}
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	if _, err := r.Render(model, model.RestPose(), opts, nil, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := coveredFraction(readback(), w, h)
	want := float64(softCover) / float64(w*h)
	// The two rasterisers differ a little in faces and shading, so this is
	// a shape check: losing a sub mesh is half the model.
	if got < want/2 || got > want*1.5 {
		t.Errorf("the Vulkan frame covers %.1f%% of it and the software frame %.1f%%, which is not the same model",
			got*100, want*100)
	}
}

// TestVulkanShowsTheOutsideOfAModel pins the winding decision: front faces
// are declared clockwise, but Vulkan's clip space runs Y
// downward and the projection is mirrored to compensate, and a mirror
// reverses apparent winding. Get either half wrong and the quad that should
// be visible is culled or the one behind it shows through.
func TestVulkanShowsTheOutsideOfAModel(t *testing.T) {
	const w, h = 120, 120
	opts := RenderOptions{
		Width:    w,
		Height:   h,
		Distance: 6,
		Fov:      modelDefaultFov,
		Quality:  modelQualityFinal,
	}
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	for _, tc := range []struct {
		name string
		out  bool
	}{{name: "wound outwards", out: true}, {name: "wound inwards", out: false}} {
		model := singleQuad(t, tc.out)
		if _, err := r.Render(model, model.RestPose(), opts, nil, nil); err != nil {
			t.Fatalf("Render(%s): %v", tc.name, err)
		}
		got := coveredFraction(readback(), w, h)
		t.Logf("%s: %.3f of the frame covered", tc.name, got)
		if visible := got > 0.001; visible != tc.out {
			if tc.out {
				t.Errorf("a quad %s drew %.3f of the frame on Vulkan: the far side of the model is on screen", tc.name, got)
			} else {
				t.Errorf("a quad %s drew %.3f of the frame on Vulkan: it should have been culled", tc.name, got)
			}
		}
	}
}

// TestVulkanIsOrientedLikeTheSoftwareRenderer checks which way up the model
// comes out. The camera aims below the model, so a correctly oriented
// renderer puts it in the upper part of the frame; the mirrored projection
// Vulkan needs would drop it to the lower part if the mirror were missing,
// doubled, or applied to the wrong row of the matrix.
func TestVulkanIsOrientedLikeTheSoftwareRenderer(t *testing.T) {
	model := twoBoxModel(t)
	center, distance := model.ModelCenter()
	const w, h = 200, 160
	// Aim a quarter of the way below the model: it lands clearly above the
	// centre of the frame, and clearly off centre is what makes a mirror
	// measurable.
	opts := RenderOptions{
		Width:    w,
		Height:   h,
		Yaw:      0.6,
		Pitch:    0.3,
		Target:   fauxgl.Vector{X: center.X, Y: center.Y, Z: center.Z - distance*0.25},
		Distance: distance,
		Fov:      modelDefaultFov,
		Quality:  modelQualityFinal,
	}
	soft := RenderWhmModel(model, model.RestPose(), opts)
	softRow := coveredCentroidRow(soft.Pix, soft.Stride, w, h)
	if softRow < 0 {
		t.Fatal("the software renderer drew nothing at all")
	}
	// Without an off centre model this test proves nothing; refuse to pass
	// on a technicality.
	if math.Abs(softRow-float64(h)/2) < float64(h)/8 {
		t.Skipf("the model sits at %.0f of %d rows, too centred to tell up from down", softRow, h)
	}
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	if _, err := r.Render(model, model.RestPose(), opts, nil, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	vkRow := coveredCentroidRow(readback(), w*4, w, h)
	if vkRow < 0 {
		t.Fatal("the Vulkan renderer drew nothing at all")
	}
	mirrored := float64(h-1) - softRow
	t.Logf("covered row: software %.0f, Vulkan %.0f, mirrored would be %.0f (frame %d tall)", softRow, vkRow, mirrored, h)
	if math.Abs(vkRow-mirrored) < math.Abs(vkRow-softRow) {
		t.Errorf("the Vulkan frame puts the model at row %.0f where the software renderer puts it at %.0f: the model is upside down",
			vkRow, softRow)
	}
}

// TestVulkanFramePaintsThroughGio adopts the rendered texture the way the 3D
// view does: paint.NewImageOpFromTexture, then a real frame, then a
// screenshot. A texture the backend refuses to adopt comes back blank here,
// and so does one whose layout was left in the wrong state by the pass.
func TestVulkanFramePaintsThroughGio(t *testing.T) {
	model := twoBoxModel(t)
	center, distance := model.ModelCenter()
	const w, h = 200, 160
	opts := RenderOptions{
		Width:    w,
		Height:   h,
		Yaw:      0.6,
		Pitch:    0.3,
		Distance: distance,
		Target:   center,
		Fov:      modelDefaultFov,
		Quality:  modelQualityFinal,
	}
	win, r, _, release := requireVulkanRenderer(t)
	defer release()
	frame, err := r.Render(model, model.RestPose(), opts, nil, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	gf, ok := frame.(*modelGpuVkFrame)
	if !ok {
		t.Fatalf("Render returned %T, want the Vulkan frame", frame)
	}
	var ops op.Ops
	clip.Rect{Max: image.Pt(w, h)}.Push(&ops)
	gf.Paint(newLayoutContextForTest(&ops, w, h), image.Rect(0, 0, w, h))
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
		t.Errorf("the painted frame is empty (%.1f%% covered), the model did not reach the window", frac*100)
	}
	if colours < 8 {
		t.Errorf("only %d distinct colours, the frame looks like a flat fill", colours)
	}
}

// coveredCentroidRow is the average row of the pixels the frame's background
// is clear of — the vertical middle of what was drawn. Two renderers of the
// same model agree on it, and an upside down renderer lands on the mirror of
// it. The predicate is the tolerant covered() the other render comparisons
// use: the software rasteriser rounds its clear colour to 0.14*255 = 35 in
// blue where the panel background is 36, and a strict comparison there
// counts every background pixel and reports the middle of the frame.
func coveredCentroidRow(px []byte, stride, w, h int) float64 {
	sum, n := 0.0, 0.0
	for y := 0; y < h; y++ {
		row := px[y*stride:]
		for x := 0; x < w; x++ {
			i := x * 4
			if i+2 >= len(row) {
				break
			}
			if covered(color.RGBA{R: row[i], G: row[i+1], B: row[i+2], A: 255}) {
				sum += float64(y)
				n++
			}
		}
	}
	if n == 0 {
		return -1
	}
	return sum / n
}

// TestVulkanSamplesBlockCompressedTexture uploads a real .rsh mip chain — the
// staging and barrier path no synthetic texture exercises — and draws frames
// with it bound. The first textured frame is the moment the model preview
// crashed on real hardware, so several frames are drawn: a driver fault shows
// up either at upload, at the first sampled draw, or on the next pass, and a
// single frame would only see the first of those.
func TestVulkanSamplesBlockCompressedTexture(t *testing.T) {
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
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)
	mats := map[string]WhmMaterial{material: {Color: rgbaWhite}}
	rsh := func(string) ([]byte, bool) { return data, true }
	for i := 0; i < 3; i++ {
		if _, err := r.Render(model, model.RestPose(), opts, mats, rsh); err != nil {
			t.Fatalf("Render %d: %v", i, err)
		}
	}
	px := readback()
	colours := distinctColours(px)
	frac := coveredFraction(px, w, h)
	t.Logf("material %q: %.1f%% covered, %d distinct colours", material, frac*100, colours)
	if frac < 0.02 {
		t.Fatalf("only %.1f%% of the frame was drawn", frac*100)
	}
	if colours < 64 {
		t.Errorf("only %d distinct colours, the texture does not look sampled", colours)
	}
}

// TestSoftAndVulkanSampleTheSameTexels pins the one thing no coverage check
// can see: WHICH texel each renderer picks. fauxgl's ImageTexture.Sample
// turns v into 1-v on the way in (an OpenGL style bottom-up convention), so
// the software path samples the map the other way round from a shader that
// takes the coordinate as written. If the two disagree, one of them draws
// every texture upside down and the shape tests above will never notice.
//
// The check is indirect because a flipped texture is not a flipped frame:
// rendering the software frame twice, once with the texture as it is and once
// with the texture's rows reversed, and comparing both against the same
// Vulkan frame says which convention the two follow between them.
//
// The comparison is a correlation rather than a mean difference. The hardware
// paths light in linear values and encode on write while the software path
// lights the sRGB bytes it decoded, so the two frames differ in brightness by
// a fixed amount over the whole frame — enough on its own to swamp a mean
// difference and leave the flip invisible. A correlation asks the question
// this test is actually about: does every pixel of one frame rise and fall
// with the same pixel of the other.
func TestSoftAndVulkanSampleTheSameTexels(t *testing.T) {
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
	img, _, _, err := decodeImageDataWithMips(data, "rsh")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	flip := verticalFlipImage(img)
	mats := map[string]WhmMaterial{}
	for _, m := range model.Meshes {
		mats[m.Material] = WhmMaterial{Color: rgbaWhite}
	}

	softRender := func(tex image.Image) []byte {
		o := gpuTestOptions(model, w, h)
		o.MaterialOverrides = map[string]WhmMaterial{}
		for _, m := range model.Meshes {
			o.MaterialOverrides[m.Material] = WhmMaterial{Texture: newModelTexture(tex), Color: rgbaWhite}
		}
		return rgbaPixels(RenderWhmModel(model, model.RestPose(), o))
	}
	normal, flipped := softRender(img), softRender(flip)

	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	o := gpuTestOptions(model, w, h)
	if _, err := r.Render(model, model.RestPose(), o, mats, func(string) ([]byte, bool) { return data, true }); err != nil {
		t.Fatalf("Render: %v", err)
	}
	gpu := readback()

	// correlation is the Pearson coefficient over the pixels both frames
	// cover, which is 1 for two frames that differ only in brightness and
	// falls away as soon as they disagree about which texel is where.
	correlation := func(a, b []byte) float64 {
		var sa, sb, saa, sbb, sab, n float64
		for i := 0; i+3 < len(a) && i+3 < len(b); i += 4 {
			if !covered(color.RGBA{R: a[i], G: a[i+1], B: a[i+2], A: 255}) ||
				!covered(color.RGBA{R: b[i], G: b[i+1], B: b[i+2], A: 255}) {
				continue
			}
			x := float64(a[i]) + float64(a[i+1]) + float64(a[i+2])
			y := float64(b[i]) + float64(b[i+1]) + float64(b[i+2])
			sa, sb, saa, sbb, sab, n = sa+x, sb+y, saa+x*x, sbb+y*y, sab+x*y, n+1
		}
		if n < 2 {
			return 0
		}
		num := n*sab - sa*sb
		den := math.Sqrt((n*saa - sa*sa) * (n*sbb - sb*sb))
		if den == 0 {
			return 0
		}
		return num / den
	}
	cDirect := correlation(normal, gpu)
	cFlipped := correlation(flipped, gpu)
	cSelf := correlation(normal, flipped)
	luma := func(px []byte) float64 {
		sum, n := 0.0, 0.0
		for i := 0; i+3 < len(px); i += 4 {
			if !covered(color.RGBA{R: px[i], G: px[i+1], B: px[i+2], A: 255}) {
				continue
			}
			sum += (float64(px[i]) + float64(px[i+1]) + float64(px[i+2])) / 3
			n++
		}
		if n == 0 {
			return 0
		}
		return sum / n
	}
	t.Logf("material %q: correlation of the Vulkan frame with the software one, texture as authored %.4f, texture row-reversed %.4f, and the two software frames with each other %.4f",
		material, cDirect, cFlipped, cSelf)
	t.Logf("mean luma of covered pixels: software %.1f, software-flipped %.1f, vulkan %.1f", luma(normal), luma(flipped), luma(gpu))
	if cFlipped > cDirect+0.02 {
		t.Errorf("the Vulkan frame matches the software render only when the software texture is row-reversed: %.4f > %.4f — the two sample opposite ends of v, and one of them is upside down against the other",
			cFlipped, cDirect)
	}
}

// verticalFlipImage is the same pixels with the rows in the other order.
func verticalFlipImage(src image.Image) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, b.Dy()-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

// TestVulkanFadesAMeshOverWhatIsBehindIt is the reason the fragment shader
// writes premultiplied colour: a clip that holds a mesh at half alpha has to
// show the panel through it, and the only way to tell that from a mesh tinted
// towards the panel colour is the arithmetic.
//
// A frame at a half alpha is measured against the mean of the two frames it
// sits between, in the space the blend actually happened in. The target is an
// sRGB texture and the hardware mixes in linear, so the mean is taken on the
// decoded values and encoded back: a blend done on the bytes instead would land
// about thirty of a shade brighter, which is what this would catch.
func TestVulkanRSHAlphaLeavesBackgroundVisible(t *testing.T) {
	model := singleQuad(t, true)
	model.Meshes[0].Material = "wire"
	const w, h = 120, 120
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)
	opts.Distance = 3
	opts.Yaw = 0
	opts.Pitch = 0
	texture := makeRSHWithFormat(11, solidDXT5AlphaBlock())
	materials := map[string]WhmMaterial{"wire": {Color: rgbaWhite}}
	if _, err := r.Render(model, model.RestPose(), opts, materials, func(string) ([]byte, bool) {
		return texture, true
	}); err != nil {
		t.Fatalf("Render textured mesh: %v", err)
	}
	px := readback()
	at := (h/2)*w*4 + (w/2)*4
	got := [3]byte{px[at], px[at+1], px[at+2]}
	want := [3]byte{bgR, bgG, bgB}
	if got != want {
		t.Fatalf("transparent RSH texel rendered as %v, want panel background %v", got, want)
	}
}

func TestVulkanDrawsSgbSelectionBoundsIntoGpuFrame(t *testing.T) {
	const w, h = 160, 160
	model := singleQuad(t, true)
	model.Meshes[0].Name = "entity-4"
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)
	_, err := r.Render(model, model.RestPose(), opts, nil, nil)
	if err != nil {
		t.Fatalf("Render without selection: %v", err)
	}
	plain := append([]byte(nil), readback()...)
	opts.SgbSelectedEntities = map[int]bool{4: true}
	opts.SgbEntityMeshIDs = map[string]int{"entity-4": 4}
	if _, err := r.Render(model, model.RestPose(), opts, nil, nil); err != nil {
		t.Fatalf("Render selected entity: %v", err)
	}
	selected := readback()
	changed := 0
	golden := 0
	var changedMin, changedMax [3]byte
	changedMin = [3]byte{255, 255, 255}
	for i := 0; i+3 < len(plain) && i+3 < len(selected); i += 4 {
		if plain[i] != selected[i] || plain[i+1] != selected[i+1] || plain[i+2] != selected[i+2] {
			changed++
			for c := 0; c < 3; c++ {
				if selected[i+c] < changedMin[c] {
					changedMin[c] = selected[i+c]
				}
				if selected[i+c] > changedMax[c] {
					changedMax[c] = selected[i+c]
				}
			}
			if selected[i] > selected[i+1]+8 && selected[i+1] > selected[i+2]+8 {
				golden++
			}
		}
	}
	t.Logf("GPU selection pixel changes: %d; yellow-like: %d; RGB bounds %v..%v", changed, golden, changedMin, changedMax)
	if changed == 0 || golden < 8 {
		t.Fatalf("GPU selection outline changed %d pixels (%d yellow), want visible yellow outline in the Vulkan color target", changed, golden)
	}
}

func TestVulkanFadesAMeshOverWhatIsBehindIt(t *testing.T) {
	model := singleQuad(t, true)
	const w, h = 120, 120
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	centre := func(alpha float32) [3]float64 {
		t.Helper()
		o := gpuTestOptions(model, w, h)
		if alpha < 1 {
			o.MeshAlpha = map[string]float32{model.Meshes[0].Name: alpha}
		}
		if _, err := r.Render(model, model.RestPose(), o, nil, nil); err != nil {
			t.Fatalf("Render at alpha %.2f: %v", alpha, err)
		}
		px := readback()
		at := (h/2)*w*4 + (w/2)*4
		return [3]float64{float64(px[at]), float64(px[at+1]), float64(px[at+2])}
	}
	gone := centre(0)
	half := centre(0.5)
	whole := centre(1)
	t.Logf("centre pixel: nothing drawn %v, half %v, whole %v", gone, half, whole)

	// A mesh the clip holds back is not drawn at all, so the frame is the
	// cleared background and nothing else.
	for i, want := range [3]float64{float64(bgR), float64(bgG), float64(bgB)} {
		if math.Abs(gone[i]-want) > 2 {
			t.Errorf("channel %d with the mesh hidden is %.0f, want the background %.0f", i, gone[i], want)
		}
	}
	for i := range whole {
		if whole[i] <= gone[i]+10 {
			t.Fatalf("channel %d drew %.0f over a background of %.0f: there is nothing here to fade",
				i, whole[i], gone[i])
		}
		// One part in two of the way from the background to the solid
		// colour, in linear light. The eight bit target costs about one
		// of a shade either way, and the alpha byte is rounded down to
		// 128 rather than up, so the allowance is a few.
		mid := float64(srgbToLinear(uint8(whole[i])))*0.5 + float64(srgbToLinear(uint8(gone[i])))*0.5
		want := linearToSrgb(mid)
		t.Logf("channel %d: %.0f measured, %.0f expected", i, half[i], want)
		if math.Abs(half[i]-want) > 4 {
			t.Errorf("channel %d at a half alpha is %.0f, want %.0f: the mesh is not being put over the background",
				i, half[i], want)
		}
	}
}

// TestModelVkOrderPutsSolidMeshesFirstAndFadingBackToFront is the order a
// frame issues its draws in. Solid geometry goes down first so that a fading
// mesh is measured against everything already behind it; among the fading ones
// the further away goes first, so the nearer ends up on top.
func TestModelVkOrderPutsSolidMeshesFirstAndFadingBackToFront(t *testing.T) {
	resolved := []modelGpuVkResolvedDraw{
		{draw: modelGpuVkDraw{name: "fading far"}, alpha: 0.5, depth: 0.9},
		{draw: modelGpuVkDraw{name: "solid near"}, alpha: 1, depth: 0.1},
		{draw: modelGpuVkDraw{name: "fading near"}, alpha: 0.5, depth: 0.2},
		{draw: modelGpuVkDraw{name: "solid far"}, alpha: 1, depth: 0.8},
	}
	order := modelVkOrder(resolved, nil)
	got := make([]string, len(order))
	for i, at := range order {
		got[i] = resolved[at].draw.name
	}
	want := []string{"solid near", "solid far", "fading far", "fading near"}
	if len(got) != len(want) {
		t.Fatalf("ordered %d draws, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("draw order is %v, want %v", got, want)
		}
	}
	// Two fading meshes at the same distance have no order between them, and
	// swapping them frame to frame is what makes a translucent pair flicker.
	tied := []modelGpuVkResolvedDraw{
		{draw: modelGpuVkDraw{name: "first"}, alpha: 0.25, depth: 0.4},
		{draw: modelGpuVkDraw{name: "second"}, alpha: 0.25, depth: 0.4},
	}
	order = modelVkOrder(tied, order)
	if len(order) != 2 || tied[order[0]].draw.name != "first" || tied[order[1]].draw.name != "second" {
		t.Errorf("two equally deep fading meshes came out as %d, %d, want the order they went in",
			order[0], order[1])
	}
}

// TestModelVkOrderReusesTheCallersSlice keeps a playing animation from giving
// the collector a slice per frame, which is the same argument the resolved
// draw list makes for itself.
func TestModelVkOrderReusesTheCallersSlice(t *testing.T) {
	resolved := make([]modelGpuVkResolvedDraw, 8)
	order := make([]int, 0, 8)
	got := modelVkOrder(resolved, order)
	if &got[:1][0] != &order[:1][0] {
		t.Error("the order came back in a new slice rather than the one it was handed")
	}
	if allocs := testing.AllocsPerRun(50, func() {
		scratch := make([]int, 0, 8)
		modelVkOrder(resolved, scratch)
	}); allocs != 0 {
		t.Errorf("ordering %d draws allocates %.1f times a run, want none", len(resolved), allocs)
	}
}

// TestModelVkDepthCountsDistanceFromTheCamera is what a fading mesh is sorted
// by. It has to grow with the distance and not merely with the position, or a
// model whose pieces are spread along the view axis comes out inside out.
func TestModelVkDepthCountsDistanceFromTheCamera(t *testing.T) {
	view := buildViewMatrix(RenderOptions{Distance: 10, Yaw: 0, Pitch: 0, Fov: 45})
	// The camera sits ten units out along +X looking at the origin, so
	// anything on the negative X axis is in front of it and further along
	// that axis is further away.
	near := modelVkDepth(view, [3]float64{-5, 0, 0})
	far := modelVkDepth(view, [3]float64{-100, 0, 0})
	// The sort only has to get the parts the right way round, so the depths
	// have to be ordered and not calibrated. A camera ten units out still
	// has to tell a part five units away from one a hundred away.
	if !(near < far) {
		t.Errorf("a point five units away is %.4f deep and one a hundred units away is %.4f, want the first nearer",
			near, far)
	}
	if d := modelVkDepth(view, [3]float64{-5, 0, 0}); d != near {
		t.Errorf("the same point measured %.6f and %.6f", d, near)
	}
}

// TestModelMeshCentreAveragesItsVertices is what the sort is fed. A sub mesh
// is the vertices its indices name, and its centre is the middle of those.
func TestModelMeshCentreAveragesItsVertices(t *testing.T) {
	model := &WhmModel{}
	for _, p := range [4][3]float32{
		{0, 0, 0}, {4, 0, 0}, {4, 6, 0}, {0, 6, 0},
	} {
		model.Vertices = append(model.Vertices, WhmVertex{Pos: p})
	}
	all := modelGroupCentre(model, []uint32{0, 1, 2, 3})
	if want := [3]float64{2, 3, 0}; all != want {
		t.Errorf("the centre of the whole quad is %v, want %v", all, want)
	}
	half := modelGroupCentre(model, []uint32{2, 3})
	if want := [3]float64{2, 6, 0}; half != want {
		t.Errorf("the centre of two of its corners is %v, want %v", half, want)
	}
	// An index past the end of the vertex array is not a position to average;
	// buildMesh has already refused to draw that mesh.
	bad := modelGroupCentre(model, []uint32{1, 99})
	if want := [3]float64{4, 0, 0}; bad != want {
		t.Errorf("a mesh with an index out of range centres at %v, want %v", bad, want)
	}
	if got := modelGroupCentre(model, nil); got != [3]float64{} {
		t.Errorf("a mesh with no indexes centres at %v, want the origin", got)
	}
}

// twoColouredQuads is two quads across the YZ plane a unit apart, the first
// nearer a camera sitting on +X and coloured red, the second further away and
// coloured blue. Overlapping and translucent, they are the case the draw order
// exists for: whichever is drawn last is the one mixed on top.
func twoColouredQuads(t *testing.T) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "quads"}}
	for i, x := range [2]float32{0.5, -0.5} {
		base := uint32(len(m.Vertices))
		for _, p := range [4][3]float32{
			{x, -0.8, -0.8}, {x, 0.8, -0.8}, {x, 0.8, 0.8}, {x, -0.8, 0.8},
		} {
			m.Vertices = append(m.Vertices, WhmVertex{
				Pos:    p,
				Normal: [3]float32{1, 0, 0},
				Bones:  [4]int32{-1, -1, -1, -1},
			})
		}
		material := "red"
		if i == 1 {
			material = "blue"
		}
		m.Meshes = append(m.Meshes, WhmMesh{
			Name:       material,
			Material:   material,
			Indices:    []uint32{base, base + 1, base + 2, base, base + 2, base + 3},
			SingleBone: -1,
		})
	}
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// TestVulkanPutsTheNearerOfTwoFadingMeshesOnTop is the order the frame issues
// its draws in, measured rather than argued: two quads at a half alpha, one in
// front of the other, and the pixel where they overlap. Drawn near first, the
// blue behind it would go on top and the pixel would come out blue heavy; drawn
// far first, the red in front does, and it comes out red heavy.
func TestVulkanPutsTheNearerOfTwoFadingMeshesOnTop(t *testing.T) {
	model := twoColouredQuads(t)
	mats := map[string]WhmMaterial{
		"red":  {Color: color.RGBA{R: 255, A: 255}},
		"blue": {Color: color.RGBA{B: 255, A: 255}},
	}
	const w, h = 120, 120
	// No yaw and no pitch, so the camera sits on +X and the quad it can see
	// is the one it is looking at.
	o := gpuTestOptions(model, w, h)
	o.Yaw, o.Pitch = 0, 0
	o.MeshAlpha = map[string]float32{"red": 0.5, "blue": 0.5}
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	if _, err := r.Render(model, model.RestPose(), o, mats, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	px := readback()
	at := (h/2)*w*4 + (w/2)*4
	got := [3]int{int(px[at]), int(px[at+1]), int(px[at+2])}
	t.Logf("centre pixel where the two fading quads overlap: %v", got)
	// Each one writes a half of itself: the top one carries its colour, the
	// one under it half its colour and half the background, which is dark.
	if got[0] <= got[2] {
		t.Errorf("the overlap is %v: the blue quad behind has been mixed on top of the red one in front", got)
	}
	if got[0] <= int(bgR)+5 {
		t.Errorf("the overlap is %v, which is no redder than the background: nothing was drawn there", got)
	}
}

// ─── merged from x_test_model3d_gpu_vulkan_overlay_test.go ────────────────────────────────────────

func gpuOverlayModel(t *testing.T) *WhmModel {
	t.Helper()
	m := overlayTestModel()
	// The box grows to twice its size and the chain stays where it was, so
	// every bone and the marker sit deep inside the mesh rather than near its
	// surface. Near the surface a depth tested overlay would still show
	// through, which is what makes the check below able to pass by accident.
	for i := range m.Vertices {
		for k := 0; k < 3; k++ {
			m.Vertices[i].Pos[k] *= 4
		}
	}
	// Every face of the box, not the half of them the shared test model
	// carries. A cube with faces missing has holes in it, and a hole is a
	// place where the skeleton behind it shows through whatever the depth
	// test says — which would let this file pass on a renderer that buries
	// the overlay inside the mesh, the one thing it exists to rule out.
	m.Meshes = []WhmMesh{{Name: "box", Indices: []uint32{
		0, 2, 1, 0, 3, 2, // bottom
		4, 5, 6, 4, 6, 7, // top
		0, 1, 5, 0, 5, 4, // -y
		1, 2, 6, 1, 6, 5, // +x
		2, 3, 7, 2, 7, 6, // +y
		3, 0, 4, 3, 4, 7, // -x
	}}}
	// Three links off the root, each a fifth of the box, so the whole chain
	// stays within the cube's middle.
	m.Bones = []WhmBone{
		{Name: "root", Parent: -1},
		{Name: "child", Parent: 0, Pos: [3]float32{0.4, 0, 0}},
		{Name: "tip", Parent: 1, Pos: [3]float32{0.4, 0.4, 0}},
		{Name: "end", Parent: 2, Pos: [3]float32{0, 0.4, 0.4}},
	}
	m.Markers = []WhmMarker{{
		Name:   "marker_tip",
		Parent: "end",
		Matrix: [9]float32{1, 0, 0, 0, 1, 0, 0, 0, 1},
		Pos:    [3]float32{0.2, 0, 0},
	}}
	return m
}

// gpuOverlayOptions frames the model from a real camera: an overlay width is
// derived from the field of view and the distance, so an unset one gives every
// segment the same width whatever the camera is doing.
func gpuOverlayOptions(m *WhmModel, w, h int) RenderOptions {
	center, _ := m.ModelCenter()
	// The distance is set rather than taken from ModelCenter: the box here is
	// scaled up to swallow the skeleton, and the framing distance that fits the
	// original box puts the camera inside this one — with the marker behind the
	// eye and its overlay off the frame entirely.
	return RenderOptions{
		Width: w, Height: h,
		Yaw: 0.6, Pitch: 0.3,
		Distance:  9,
		Target:    center,
		Fov:       modelDefaultFov,
		Quality:   modelQualityFinal,
		BaseColor: rgbaWhite,
	}
}

// overlayChangedPixels counts the pixels two frames disagree on, which is what
// says whether a pass drew anything at all. Every pixel counts, background
// included: the overlay is drawn over the model and the background alike, and
// a count that skipped the background would miss lines that run off the mesh.
func overlayChangedPixels(a, b []byte) int {
	if len(a) != len(b) {
		return -1
	}
	n := 0
	for i := 0; i+3 < len(a); i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			n++
		}
	}
	return n
}

// The skeleton must be drawn by the GPU renderer, in the same pass as the
// model. Turning it on is compared frame against frame: a renderer that
// ignores it produces two identical frames, which is exactly the failure this
// is here to catch.
func TestVulkanDrawsTheSkeletonOnTheGpu(t *testing.T) {
	model := gpuOverlayModel(t)
	const w, h = 240, 200
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuOverlayOptions(model, w, h)
	pose := model.RestPose()
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render without the skeleton: %v", err)
	}
	without := readback()

	opts.Skeleton = true
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render with the skeleton: %v", err)
	}
	with := readback()
	changed := overlayChangedPixels(without, with)
	t.Logf("the skeleton changed %d pixels of a %dx%d frame", changed, w, h)
	if changed == 0 {
		t.Fatal("turning the skeleton on changed nothing: the Vulkan renderer drew no bone lines")
	}
	// The bones are inside the mesh, so if the overlay were depth tested
	// against the model almost all of it would be hidden. A handful of
	// pixels would mean it is being drawn but buried.
	if changed < 50 {
		t.Errorf("only %d pixels changed; the bones are mostly inside the mesh, which suggests the overlay is being depth tested against it", changed)
	}
}

// DATASEUI's selection rectangle is part of the Vulkan overlay pass, not a
// CPU-rendered decoration. This compares GPU readbacks with the indicator
// disabled and enabled while the rectangle lies inside the cube's footprint.
func TestVulkanDrawsSelectionIndicatorOnTheGpu(t *testing.T) {
	model := gpuOverlayModel(t)
	const w, h = 240, 200
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuOverlayOptions(model, w, h)
	pose := model.RestPose()
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render without selection indicator: %v", err)
	}
	without := readback()

	opts.SelectionIndicator = &engine.SelectionIndicator{
		DisplayType: 1,
		Scale:       [2]float32{1.4, 1.1},
		Offset:      [2]float32{0.25, -0.2},
	}
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render with selection indicator: %v", err)
	}
	with := readback()
	if changed := overlayChangedPixels(without, with); changed == 0 {
		t.Fatal("enabling the selection indicator changed no pixels: the Vulkan renderer did not draw it")
	}
}

// The picked marker is drawn on the GPU path too. Without it a reader who
// picked a marker on the Vulkan renderer saw the 3D frame and nothing at all
// to say which attachment point they had picked.
func TestVulkanDrawsThePickedMarkerOnTheGpu(t *testing.T) {
	model := gpuOverlayModel(t)
	const w, h = 240, 200
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := gpuOverlayOptions(model, w, h)
	pose := model.RestPose()
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render without a marker: %v", err)
	}
	without := readback()

	opts.Marker = pickedMarker(0)
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render with a marker: %v", err)
	}
	with := readback()
	changed := overlayChangedPixels(without, with)
	t.Logf("the picked marker changed %d pixels of a %dx%d frame", changed, w, h)
	if changed == 0 {
		t.Fatal("picking a marker changed nothing: the Vulkan renderer drew neither the frame nor the arrow")
	}
}

// The overlay and the particles share one vertex buffer and one pass, so
// drawing both at once is the case neither of the tests above covers: a
// bounds check that stops at the particles would silently drop the bones, and
// a group count that assumed one draw would drop whichever came second.
func TestVulkanDrawsParticlesAndTheOverlayTogether(t *testing.T) {
	model := gpuOverlayModel(t)
	const w, h = 240, 200
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	base := gpuOverlayOptions(model, w, h)
	pose := model.RestPose()

	sim := &fx.FxSim{}
	sim.Anchored = []fx.Visual{{
		Lifetime: 1,
		Radius:   []fx.FxKey{{V: 0.6}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}
	withFx := base
	front := 2.6 // the cube has half-extent 2; put the puff in front of its surface.
	withFx.FX = &fxGpuScene{
		Groups: []fxGpuGroup{{
			Origin: [3]float64{
				base.Target.X + math.Cos(base.Yaw)*math.Cos(base.Pitch)*front,
				base.Target.Y + math.Sin(base.Yaw)*math.Cos(base.Pitch)*front,
				base.Target.Z + math.Sin(base.Pitch)*front,
			},
			Sprites:  repeatedSprite(sim.Sprites(), 12),
			Additive: true,
		}},
	}
	if _, err := r.Render(model, pose, withFx, nil, nil); err != nil {
		t.Fatalf("Render with particles: %v", err)
	}
	particlesFrame := readback()
	particlesOnly := fxMeanLuma(particlesFrame, w, h)

	withFx.Skeleton = true
	if _, err := r.Render(model, pose, withFx, nil, nil); err != nil {
		t.Fatalf("Render with particles and the skeleton: %v", err)
	}
	both := readback()

	// The skeleton may darken pixels it crosses, so mean luminance is not a
	// reliable coexistence check. The combined frame must differ from the
	// particles-only frame, while the comparison below verifies the particle
	// pass was not displaced by the overlay.
	t.Logf("mean luma: particles %.1f, particles and skeleton %.1f", particlesOnly, fxMeanLuma(both, w, h))
	if changed := overlayChangedPixels(particlesFrame, both); changed == 0 {
		t.Error("adding the skeleton changed no pixels in the particle frame")
	}

	// And the skeleton on its own against the same particles, which is what
	// says the bones survived being written after them.
	opts := base
	opts.Skeleton = true
	if _, err := r.Render(model, pose, opts, nil, nil); err != nil {
		t.Fatalf("Render with the skeleton: %v", err)
	}
	if changed := overlayChangedPixels(both, readback()); changed == 0 {
		t.Error("the frame with particles and the frame with the skeleton alone are identical: one of the two passes drew nothing")
	}
}

// The overlay's segments are turned into camera facing quads by the same
// tessellator the beams use. A bone that came out as a quad on its side, or
// with no width at all, would still be "drawn" and still be useless, so the
// quads are measured rather than counted.
func TestOverlayQuadsFaceTheEyeAndHaveWidth(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Skeleton = true
	opts.Target = vec3From(m.BoundMin).Add(vec3From(m.BoundMax)).MulScalar(0.5)
	eye := fxEyePosition(opts)
	segments := modelOverlayLines(m, pose, opts)
	if len(segments) == 0 {
		t.Fatal("no segments to tessellate")
	}
	for i, s := range segments {
		verts := fx.BeamQuads(fx.BeamRibbon{
			From: s.From, To: s.To, Width: s.HalfWidth, Segments: 1,
		}, eye)
		if len(verts) < 4 {
			t.Errorf("segment %d (%v -> %v) tessellated to %d corners, which is no quad at all",
				i, s.From, s.To, len(verts))
			continue
		}
		// The four corners of one quad: two at each end, HalfWidth apart
		// across the segment. A quad that came out flat would have the two
		// ends of one edge on top of each other.
		a := verts[0].Pos.Sub(verts[1].Pos)
		b := verts[2].Pos.Sub(verts[3].Pos)
		want := s.HalfWidth * 2
		for _, edge := range []fauxgl.Vector{a, b} {
			if got := edge.Length(); got < want*0.5 || got > want*1.5 {
				t.Errorf("segment %d tessellated to a quad %v wide, want about %v", i, got, want)
			}
			// And the cross-section has to lie across the segment rather
			// than along it, or the quad is edge on to the eye.
			dir := s.To.Sub(s.From)
			if n := dir.Normalize(); n != (fauxgl.Vector{}) {
				if dot := math.Abs(edge.Normalize().Dot(n)); dot > 0.1 {
					t.Errorf("segment %d tessellated along itself (dot=%.3f), so the line would be seen edge on", i, dot)
				}
			}
		}
	}
}

// ─── merged from x_test_model3d_gpu_test.go ────────────────────────────────────────

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

// singleQuad is one flat quad across the YZ plane with its normal along the X
// axis, which is where the orbit camera sits with no yaw and no pitch.
func singleQuad(t *testing.T, outwards bool) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "quad"}}
	for _, p := range [4][3]float32{
		{0, -0.8, -0.8}, {0, 0.8, -0.8}, {0, 0.8, 0.8}, {0, -0.8, 0.8},
	} {
		m.Vertices = append(m.Vertices, WhmVertex{
			Pos:    p,
			Normal: [3]float32{1, 0, 0},
			Bones:  [4]int32{-1, -1, -1, -1},
		})
	}
	// Counter clockwise seen from the front, which puts the outside of the
	// quad on the side the camera is on.
	idx := []uint32{0, 1, 2, 0, 2, 3}
	if !outwards {
		idx = []uint32{0, 2, 1, 0, 3, 2}
	}
	m.Meshes = append(m.Meshes, WhmMesh{Name: "quad", Indices: idx, SingleBone: -1})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// covered is whether a pixel is clear of the background a frame is cleared to.
// The arithmetic is done on ints: the background is darker than the threshold
// in the blue channel, and unsigned bytes would count that as a large number.
func covered(c color.RGBA) bool {
	return int(c.R)-0x1c > 12 || int(c.G)-0x1e > 12 || int(c.B)-0x24 > 12
}

// openBoxModel is a box with its top and bottom left off.
func openBoxModel(t *testing.T) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "open_box"}}
	min, max := [3]float32{-1, -1, -1}, [3]float32{1, 1, 1}
	corners := [8][3]float32{
		{min[0], min[1], min[2]}, {max[0], min[1], min[2]}, {max[0], max[1], min[2]}, {min[0], max[1], min[2]},
		{min[0], min[1], max[2]}, {max[0], min[1], max[2]}, {max[0], max[1], max[2]}, {min[0], max[1], max[2]},
	}
	centre := [3]float32{0, 0, 0}
	for _, c := range corners {
		n := [3]float32{c[0] - centre[0], c[1] - centre[1], c[2] - centre[2]}
		l := float32(sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]))
		m.Vertices = append(m.Vertices, WhmVertex{
			Pos:    c,
			Normal: [3]float32{n[0] / l, n[1] / l, n[2] / l},
			Bones:  [4]int32{-1, -1, -1, -1},
		})
	}
	// The four sides only. The opening is what the frame looks through.
	faces := [][3]int{
		{0, 1, 5}, {0, 5, 4}, // front
		{1, 2, 6}, {1, 6, 5}, // right
		{2, 3, 7}, {2, 7, 6}, // back
		{3, 0, 4}, {3, 4, 7}, // left
	}
	var idx []uint32
	for _, f := range faces {
		if facePointsOutwards(m.Vertices[f[0]], m.Vertices[f[1]], m.Vertices[f[2]]) {
			idx = append(idx, uint32(f[0]), uint32(f[1]), uint32(f[2]))
		} else {
			idx = append(idx, uint32(f[0]), uint32(f[2]), uint32(f[1]))
		}
	}
	m.Meshes = append(m.Meshes, WhmMesh{Name: "walls", Indices: idx, SingleBone: -1})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// twoBoxModel is two cubes far apart, as two sub meshes. Which piece is drawn
// is therefore obvious at a glance, and so is which side of each is.
func twoBoxModel(t *testing.T) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "two_boxes"}}
	add := func(name string, min, max [3]float32) {
		base := uint32(len(m.Vertices))
		// Every face of a cube as two triangles, wound so that the normal
		// each one carries points away from the middle of it.
		corners := [8][3]float32{
			{min[0], min[1], min[2]}, {max[0], min[1], min[2]}, {max[0], max[1], min[2]}, {min[0], max[1], min[2]},
			{min[0], min[1], max[2]}, {max[0], min[1], max[2]}, {max[0], max[1], max[2]}, {min[0], max[1], max[2]},
		}
		centre := [3]float32{(min[0] + max[0]) / 2, (min[1] + max[1]) / 2, (min[2] + max[2]) / 2}
		for i, c := range corners {
			n := [3]float32{c[0] - centre[0], c[1] - centre[1], c[2] - centre[2]}
			length := float32(1)
			if l := n[0]*n[0] + n[1]*n[1] + n[2]*n[2]; l > 0 {
				length = float32(sqrt(l))
			}
			m.Vertices = append(m.Vertices, WhmVertex{
				Pos:    c,
				Normal: [3]float32{n[0] / length, n[1] / length, n[2] / length},
				UV:     [2]float32{0, 0},
				Bones:  [4]int32{-1, -1, -1, -1},
			})
			_ = i
		}
		faces := [][3]int{
			{0, 2, 1}, {0, 3, 2}, // bottom
			{4, 5, 6}, {4, 6, 7}, // top
			{0, 1, 5}, {0, 5, 4}, // front
			{1, 2, 6}, {1, 6, 5}, // right
			{2, 3, 7}, {2, 7, 6}, // back
			{3, 0, 4}, {3, 4, 7}, // left
		}
		var idx []uint32
		for _, f := range faces {
			// Wound so that cross(b-a, c-a) points along the face normal.
			if facePointsOutwards(m.Vertices[base+uint32(f[0])], m.Vertices[base+uint32(f[1])], m.Vertices[base+uint32(f[2])]) {
				idx = append(idx, base+uint32(f[0]), base+uint32(f[1]), base+uint32(f[2]))
			} else {
				idx = append(idx, base+uint32(f[0]), base+uint32(f[2]), base+uint32(f[1]))
			}
		}
		m.Meshes = append(m.Meshes, WhmMesh{Name: name, Indices: idx, SingleBone: -1})
	}
	add("left", [3]float32{-1.4, -0.5, -0.5}, [3]float32{-0.4, 0.5, 0.5})
	add("right", [3]float32{0.4, -0.5, -0.5}, [3]float32{1.4, 0.5, 0.5})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// facePointsOutwards reports whether the winding of a triangle agrees with the
// normal its vertices carry.
func facePointsOutwards(a, b, c WhmVertex) bool {
	u1 := [3]float64{float64(b.Pos[0] - a.Pos[0]), float64(b.Pos[1] - a.Pos[1]), float64(b.Pos[2] - a.Pos[2])}
	u2 := [3]float64{float64(c.Pos[0] - a.Pos[0]), float64(c.Pos[1] - a.Pos[1]), float64(c.Pos[2] - a.Pos[2])}
	g := [3]float64{
		u1[1]*u2[2] - u1[2]*u2[1],
		u1[2]*u2[0] - u1[0]*u2[2],
		u1[0]*u2[1] - u1[1]*u2[0],
	}
	n := [3]float64{
		float64(a.Normal[0]) + float64(b.Normal[0]) + float64(c.Normal[0]),
		float64(a.Normal[1]) + float64(b.Normal[1]) + float64(c.Normal[1]),
		float64(a.Normal[2]) + float64(b.Normal[2]) + float64(c.Normal[2]),
	}
	return g[0]*n[0]+g[1]*n[1]+g[2]*n[2] > 0
}

// coveredPixels counts the pixels of a frame that are not the background the
// renderer clears to.
func coveredPixels(img *image.RGBA, area image.Rectangle) int {
	n := 0
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if covered(img.RGBAAt(x, y)) {
				n++
			}
		}
	}
	return n
}

func sqrt(v float32) float64 { return math.Sqrt(float64(v)) }

// ─── merged from x_test_model3d_gpu_vulkan_cache_test.go ────────────────────────────────────────

func TestModelGpuVkGeometryChangesAreDeferredUntilRender(t *testing.T) {
	model := &WhmModel{}
	mesh := &modelGpuVkMesh{draws: []modelGpuVkDraw{{count: 3}}}
	r := &modelGpuVkRenderer{
		meshes:      map[*WhmModel]*modelGpuVkMesh{model: mesh},
		dirtyMeshes: make(map[*WhmModel]bool),
	}

	r.geometryChanged(model)
	if r.meshes[model] != mesh || !r.dirtyMeshes[model] {
		t.Fatal("geometryChanged must retain the in-use mesh and mark it dirty for the next render")
	}
}

func TestModelGpuVkForgetRetiresTheCachedMesh(t *testing.T) {
	model := &WhmModel{}
	mesh := &modelGpuVkMesh{}
	r := &modelGpuVkRenderer{meshes: map[*WhmModel]*modelGpuVkMesh{model: mesh}}

	r.forget(model)
	if _, ok := r.meshes[model]; ok {
		t.Fatal("forgotten model remains in the GPU mesh cache")
	}
}
