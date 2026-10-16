//go:build windows || linux

package main

import (
	"dow-mod-studio/internal/whm"
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/fogleman/fauxgl"
)

// Passing the software renderer's tests proves nothing about the Vulkan
// renderer: it is a separate port with its own shaders, its own uniform block
// layout, a projection mirrored into Vulkan's downward Y, and a winding
// decision that has to be re-derived for that mirror. These tests put it
// through the same checks the software renderer is held to — the shape of what it draws,
// which side of a model faces the camera, which way up it comes out — and
// one of them paints the frame through Gio, which is the only way to see
// that an adopted texture reaches the screen at all.

// requireVulkanRenderer opens a headless window on the Vulkan backend and a
// model renderer on it. Everything skips rather than fails when the machine
// has no Vulkan: the software tests carry the rest of the coverage.
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
	all := modelMeshCentre(model, []uint32{0, 1, 2, 3})
	if want := [3]float64{2, 3, 0}; all != want {
		t.Errorf("the centre of the whole quad is %v, want %v", all, want)
	}
	half := modelMeshCentre(model, []uint32{2, 3})
	if want := [3]float64{2, 6, 0}; half != want {
		t.Errorf("the centre of two of its corners is %v, want %v", half, want)
	}
	// An index past the end of the vertex array is not a position to average;
	// buildMesh has already refused to draw that mesh.
	bad := modelMeshCentre(model, []uint32{1, 99})
	if want := [3]float64{4, 0, 0}; bad != want {
		t.Errorf("a mesh with an index out of range centres at %v, want %v", bad, want)
	}
	if got := modelMeshCentre(model, nil); got != [3]float64{} {
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
