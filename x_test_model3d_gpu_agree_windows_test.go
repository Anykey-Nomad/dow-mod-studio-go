package main

import (
	"dow-mod-studio/internal/whm"
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/paint"
)

// The hardware and the software renderer have to show the same model, and they
// are two entirely separate pieces of code: one samples a block compressed
// texture through a pipeline, the other rasterises on the CPU. A model with
// more than one sub mesh is what tells them apart.
//
// A draw call has no index offset, so each sub mesh is drawn from its own
// buffer. Sharing one buffer meant every sub mesh drew the first one, and the
// Summoning Core lost seven of its eight pieces without a word. The coverage
// of the two renders is the check: a model missing a part covers much less of
// the canvas than the same model drawn whole.
func TestTheHardwareAndSoftwareRenderersAgree(t *testing.T) {
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
	pose := model.RestPose()

	soft := RenderWhmModel(model, pose, opts)
	softCover := coveredPixels(soft, image.Rect(0, 0, w, h))
	if softCover == 0 {
		t.Fatal("the software renderer drew nothing at all")
	}

	hard := renderOnGpu(t, model, pose, opts, w, h)
	hardCover := coveredPixels(hard, image.Rect(0, 0, w, h))
	if hardCover == 0 {
		t.Fatal("the hardware renderer drew nothing at all")
	}
	// Both faces and the lighting differ a little between the two, so this is
	// a shape check rather than a pixel for pixel one. Missing a whole sub mesh
	// is a difference of half the model, which no amount of shading explains.
	lo := softCover / 2
	hi := softCover + softCover/2 + 16
	if hardCover < lo || hardCover > hi {
		t.Errorf("the hardware frame covers %d pixels and the software frame %d, which is not the same model",
			hardCover, softCover)
	}
}

// The hardware renderer has to drop the faces inside a model, or a building
// reads as a solid block: every interior wall is drawn, and the ones nearest the
// camera win, so the openings the model has are filled in. The model here is a
// box with its top and bottom left out, which is an opening straight through, so
// the two renderers have to agree on how much of the frame it covers: the
// inside faces fill the opening, and a renderer that keeps them covers more.
func TestTheHardwareAndSoftwareRenderersKeepTheSameFaces(t *testing.T) {
	model := openBoxModel(t)
	center, distance := model.ModelCenter()
	const w, h = 160, 160
	opts := RenderOptions{
		Width:    w,
		Height:   h,
		Yaw:      0.5,
		Pitch:    0.4,
		Distance: distance * 2.4,
		Target:   center,
		Fov:      modelDefaultFov,
		Quality:  modelQualityFinal,
	}
	soft := RenderWhmModel(model, model.RestPose(), opts)
	hard := renderOnGpu(t, model, model.RestPose(), opts, w, h)
	softCover := coveredPixels(soft, image.Rect(0, 0, w, h))
	hardCover := coveredPixels(hard, image.Rect(0, 0, w, h))
	if softCover == 0 || hardCover == 0 {
		t.Fatalf("a renderer drew nothing: software %d, hardware %d", softCover, hardCover)
	}
	// The two rasterisers agree on the shape of a model down to the pixel on
	// flat colour, and a filled in opening is thousands of pixels, so a
	// difference of one percent is the whole of what shading can account for.
	if diff := hardCover - softCover; diff > softCover/100 || -diff > softCover/100 {
		t.Errorf("the hardware frame covers %d pixels and the software frame %d: they are not keeping the same faces",
			hardCover, softCover)
	}
}

// A model has to show the faces that point at the camera, not the far side of
// its own shell. Which side of a triangle that is comes down to the winding in
// the file against the convention of the rasteriser, and the two here disagree:
// Direct3D calls the front face what fauxgl calls the back one, and a cull mode
// chosen by eye is what had every model showing the inside of its shell.
//
// The model is one flat quad with the camera straight on its normal, wound
// outwards, and the same quad wound inwards. Coverage says which one was kept,
// and there is no shading and no depth to confuse the answer: a quad either
// covers the middle of the frame or it does not.
func TestBothRenderersShowTheOutsideOfAModel(t *testing.T) {
	const w, h = 120, 120
	opts := RenderOptions{
		Width:    w,
		Height:   h,
		Distance: 6,
		Fov:      modelDefaultFov,
		Quality:  modelQualityFinal,
	}
	area := image.Rect(0, 0, w, h)
	for _, tc := range []struct {
		name string
		out  bool
	}{{name: "wound outwards", out: true}, {name: "wound inwards", out: false}} {
		model := singleQuad(t, tc.out)
		soft := coveredPixels(RenderWhmModel(model, model.RestPose(), opts), area)
		hard := coveredPixels(renderOnGpu(t, model, model.RestPose(), opts, w, h), area)
		t.Logf("%s: software %d, hardware %d", tc.name, soft, hard)
		wantVisible := tc.out
		for _, r := range []struct {
			which string
			got   int
		}{{"software", soft}, {"hardware", hard}} {
			if visible := r.got > 0; visible != wantVisible {
				if wantVisible {
					t.Errorf("a quad %s drew %d pixels on the %s renderer: the far side of the model is on screen",
						tc.name, r.got, r.which)
				} else {
					t.Errorf("a quad %s drew %d pixels on the %s renderer: it should have been culled",
						tc.name, r.got, r.which)
				}
			}
		}
	}
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

// renderOnGpu draws a model with the hardware renderer and reads the frame back
// through a headless window, which is the only way to see what it drew.
func renderOnGpu(t *testing.T, model *WhmModel, pose WhmPose, opts RenderOptions, w, h int) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	t.Cleanup(win.Release)
	d3d, err := win.D3D11()
	if err != nil {
		t.Skipf("no Direct3D 11 device: %v", err)
	}
	r, err := newModelGpuRenderer(d3d)
	if err != nil {
		t.Skipf("no hardware renderer: %v", err)
	}
	t.Cleanup(r.release)
	frame, err := r.Render(model, pose, opts, nil, nil)
	if err != nil {
		t.Fatalf("the hardware renderer refused the model: %v", err)
	}
	wf, ok := frame.(*modelGpuWindowsFrame)
	if !ok {
		t.Fatalf("the hardware renderer produced a %T", frame)
	}
	var ops op.Ops
	paint.NewImageOpFromTexture(wf.tex).Add(&ops)
	paint.PaintOp{}.Add(&ops)
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
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
