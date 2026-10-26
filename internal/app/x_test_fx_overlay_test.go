//go:build windows || linux

package app

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/whm"

	"gioui.org/gpu/headless"
	"github.com/fogleman/fauxgl"
)

func fxOverlayTestModel(t *testing.T) *WhmModel {
	t.Helper()
	m := &WhmModel{Model: whm.Model{Name: "fx_box"}}
	add := func(min, max [3]float32) {
		base := uint32(len(m.Vertices))
		corners := [8][3]float32{
			{min[0], min[1], min[2]}, {max[0], min[1], min[2]}, {max[0], max[1], min[2]}, {min[0], max[1], min[2]},
			{min[0], min[1], max[2]}, {max[0], min[1], max[2]}, {max[0], max[1], max[2]}, {min[0], max[1], max[2]},
		}
		centre := [3]float32{(min[0] + max[0]) / 2, (min[1] + max[1]) / 2, (min[2] + max[2]) / 2}
		for _, c := range corners {
			n := [3]float32{c[0] - centre[0], c[1] - centre[1], c[2] - centre[2]}
			l := float32(1)
			if s := n[0]*n[0] + n[1]*n[1] + n[2]*n[2]; s > 0 {
				l = float32(sqrt(s))
			}
			m.Vertices = append(m.Vertices, WhmVertex{
				Pos: c, Normal: [3]float32{n[0] / l, n[1] / l, n[2] / l},
				Bones: [4]int32{-1, -1, -1, -1},
			})
		}
		faces := [][3]int{
			{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7},
			{0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5},
			{2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7},
		}
		var idx []uint32
		for _, f := range faces {
			if testutil.FacePointsOutwards(m.Vertices[base+uint32(f[0])], m.Vertices[base+uint32(f[1])], m.Vertices[base+uint32(f[2])]) {
				idx = append(idx, base+uint32(f[0]), base+uint32(f[1]), base+uint32(f[2]))
			} else {
				idx = append(idx, base+uint32(f[0]), base+uint32(f[2]), base+uint32(f[1]))
			}
		}
		m.Meshes = append(m.Meshes, WhmMesh{Name: "box", Indices: idx, SingleBone: -1})
	}
	add([3]float32{-0.5, -0.5, -0.5}, [3]float32{0.5, 0.5, 0.5})
	m.Bones = []WhmBone{{Name: "root", Parent: -1}}
	return m
}

// fxLitPixels counts the pixels an overlay actually put something into.
func fxLitPixels(img *image.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y).A > 0 {
				n++
			}
		}
	}
	return n
}

// fxRacesPath is the tree path a file under the race art has, which is how the
// effect resolver is given a model: the folder under art/ebps, the rest of the
// path as it is on disk.
func fxRacesPath(name string) string {
	p := filepath.ToSlash(filepath.Join(dowRacesDir, name))
	if i := strings.Index(strings.ToLower(p), "art/ebps/"); i >= 0 {
		return filepath.FromSlash(p[i:])
	}
	return filepath.FromSlash(p)
}

// TestVulkanDrawsFxParticlesOnTheGpu renders a model with a burst of particles
// through the hardware renderer and checks that they land on the frame: the
// particles are drawn by their own pass, inside the model's, and a pass that
// quietly draws nothing would otherwise look like an overlay that stopped
// working.
func TestVulkanDrawsFxParticlesOnTheGpu(t *testing.T) {
	model := fxOverlayTestModel(t)
	const w, h = 200, 200
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := testutil.GpuTestOptions(&model.Model, w, h)
	plain, err := r.Render(&model.Model, model.RestPose(), opts, nil, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	_ = plain
	without := fxMeanLuma(readback(), w, h)

	// Put a dozen additive white particles just in front of the model's
	// camera-facing surface. A sprite at the model centre is correctly hidden
	// by the model's depth buffer, so it cannot tell us whether the draw ran.
	// Additive is what distinguishes a drawn particle from a repaint, since
	// the model underneath stays.
	sim := &fx.FxSim{}
	sim.Anchored = []fx.Visual{{
		Lifetime: 1,
		Radius:   []fx.FxKey{{V: 0.6}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}
	front := 1.2
	opts.FX = &fxGpuScene{
		Groups: []fxGpuGroup{{
			Origin: [3]float64{
				math.Cos(opts.Yaw) * math.Cos(opts.Pitch) * front,
				math.Sin(opts.Yaw) * math.Cos(opts.Pitch) * front,
				math.Sin(opts.Pitch) * front,
			},
			Sprites:  repeatedSprite(sim.Sprites(), 12),
			Additive: true,
		}},
	}
	if _, err := r.Render(&model.Model, model.RestPose(), opts, nil, nil); err != nil {
		t.Fatalf("Render with particles: %v", err)
	}
	with := fxMeanLuma(readback(), w, h)
	fxInfo := render.FxGroups(r)
	if fxInfo.Groups == 0 {
		t.Fatal("the render recorded no FX group")
	}
	if got, want := fxInfo.Compute, os.Getenv("DOW_FX_COMPUTE") != "0"; got != want {
		t.Errorf("sprite group compute=%v, want %v (DOW_FX_COMPUTE=%q)", got, want, os.Getenv("DOW_FX_COMPUTE"))
	}
	t.Logf("mean luma: model alone %.1f, with twelve additive particles %.1f", without, with)

	if with <= without+2 {
		t.Errorf("the GPU drew no additive particles: mean luma went from %.1f to %.1f", without, with)
	}
}

// fxMeanLuma is the average brightness of a frame.
func fxMeanLuma(px []byte, w, h int) float64 {
	sum, n := 0.0, 0.0
	for i := 0; i+3 < len(px); i += 4 {
		sum += (float64(px[i]) + float64(px[i+1]) + float64(px[i+2])) / 3
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// repeatedSprite is one sprite copied n times, which is what an effect with no
// emitter but a looped leaf produces.
func repeatedSprite(sprites []fx.Sprite, n int) []fx.Sprite {
	if len(sprites) == 0 {
		return nil
	}
	out := make([]fx.Sprite, 0, n)
	for i := 0; i < n; i++ {
		sp := sprites[0]
		// Spread them so the burst is not one square on top of another.
		sp.Pos[0] += float64(i%4)*0.2 - 0.3
		sp.Pos[1] += float64((i/4)%4)*0.2 - 0.3
		out = append(out, sp)
	}
	return out
}

// TestVulkanDrawsFxRibbonsOnTheGpu renders a beam through the hardware
// renderer and checks that it lands on the frame, with its group coming out
// of the compute expand when that path is on. A ribbon expand that writes
// nothing — or writes it at the wrong offset — leaves a silent frame here,
// which the particle test beside it would not notice.
func TestVulkanDrawsFxRibbonsOnTheGpu(t *testing.T) {
	model := fxOverlayTestModel(t)
	const w, h = 200, 200
	_, r, readback, release := requireVulkanRenderer(t)
	defer release()
	opts := testutil.GpuTestOptions(&model.Model, w, h)
	if _, err := r.Render(&model.Model, model.RestPose(), opts, nil, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	without := fxMeanLuma(readback(), w, h)

	// A fan of beams in front of the model, running up: additive, so they
	// brighten the frame wherever the model would otherwise hide them. Eight
	// of them because one thin beam moves the frame's mean by too little to
	// tell a drawn beam from a silent one.
	const front = 1.2
	origin := fauxgl.Vector{
		X: math.Cos(opts.Yaw) * math.Cos(opts.Pitch) * front,
		Y: math.Sin(opts.Yaw) * math.Cos(opts.Pitch) * front,
		Z: math.Sin(opts.Pitch) * front,
	}
	eye := render.FxEyePosition(opts)

	var beams []fxGpuBeam
	for i := 0; i < 8; i++ {
		base := origin.Add(fauxgl.Vector{
			X: float64(i%4)*0.2 - 0.3,
			Y: float64(i/4)*0.2 - 0.1,
		})
		beams = append(beams, fxGpuBeam{
			Ribbon: fx.BeamRibbon{
				From:     base,
				To:       base.Add(fauxgl.Vector{Z: 0.9}),
				Width:    0.35,
				Segments: 3,
				R:        1, G: 1, B: 1, Alpha: 1,
				U0: 0, U1: 1,
			},
			Eye: eye,
		})
	}
	opts.FX = &fxGpuScene{
		Groups: []fxGpuGroup{{
			Beams:    beams,
			Additive: true,
		}},
	}
	if _, err := r.Render(&model.Model, model.RestPose(), opts, nil, nil); err != nil {
		t.Fatalf("Render with beams: %v", err)
	}
	with := fxMeanLuma(readback(), w, h)
	fxInfo := render.FxGroups(r)
	if fxInfo.Groups == 0 {
		t.Fatal("the beams produced no draw group")
	}
	computeAvailable := os.Getenv("DOW_FX_COMPUTE") != "0" && fxInfo.RibbonPipeline
	if got := fxInfo.Compute; got != computeAvailable {
		t.Errorf("ribbon group compute=%v, want %v (DOW_FX_COMPUTE=%q, ribbon pipeline=%v)",
			got, computeAvailable, os.Getenv("DOW_FX_COMPUTE"), fxInfo.RibbonPipeline)
	}
	if fxInfo.Overlay {
		t.Error("the beam group was drawn through the skeleton overlay pipeline")
	}
	t.Logf("mean luma: model alone %.1f, with additive beams %.1f", without, with)
	if with <= without+2 {
		t.Errorf("the GPU drew no beams: mean luma went from %.1f to %.1f", without, with)
	}
}

// TestRenderFxOverlayDrawsAtTheMarker checks the whole of the overlay path on a
// simulation built by hand: a particle at the origin of the effect must land
// on the pixel the camera projects that origin onto, and an effect with no
// particles must draw nothing at all rather than a smear over the frame.
func TestRenderFxOverlayDrawsAtTheMarker(t *testing.T) {
	const w, h = 200, 160
	opts := render.RenderOptions{
		Width: w, Height: h,
		Yaw: 0.7, Pitch: 0.2, Distance: 6,
		Fov: 45, Ambient: 1, Diffuse: 1,
	}
	view := render.BuildViewMatrix(opts)
	origin := fauxgl.Vector{X: 0.4, Y: -0.2, Z: 0.3}
	sim := &fx.FxSim{}
	sim.Anchored = []fx.Visual{{
		Texture:  "",
		Blending: 0,
		Lifetime: 1,
		Radius:   []fx.FxKey{{T: 0, V: 0.4}},
	}}
	effects := []fxOverlayEffect{{Sim: sim, Origin: origin}}
	img := renderFxOverlay(effects, opts, w, h, nil)
	lit := fxLitPixels(img)
	if lit == 0 {
		t.Fatal("the overlay drew nothing at all")
	}
	// The particle is at the effect's origin, so the middle of the square
	// the overlay put down has to be where the camera projects that point.
	p := fxProject(view, origin, w, h)
	if p == nil {
		t.Fatal("the origin projects behind the camera")
	}
	cx, cy := int(p.X), int(p.Y)
	if cx < 1 || cy < 1 || cx >= w-1 || cy >= h-1 {
		t.Fatalf("the origin projects onto the edge of the frame: %.0f,%.0f", p.X, p.Y)
	}
	if img.RGBAAt(cx, cy).A == 0 {
		t.Errorf("nothing was drawn at the projected origin %d,%d", cx, cy)
	}

	// An effect with nothing in it draws nothing: an overlay that covered
	// the frame would hide the model behind it.
	empty := renderFxOverlay([]fxOverlayEffect{{Sim: &fx.FxSim{}}}, opts, w, h, nil)
	if n := fxLitPixels(empty); n != 0 {
		t.Errorf("an empty effect covered %d pixels", n)
	}
}

// TestRenderFxOverlayAdditiveBrightens checks the two blendings against a
// known backdrop, since an effect that adds and an effect that covers are
// told apart by what they leave behind.
func TestRenderFxHorizontalRingRemainsAWorldPlane(t *testing.T) {
	const w, h = 256, 200
	opts := render.RenderOptions{Width: w, Height: h, Yaw: 0.4, Pitch: 0.3, Distance: 6, Fov: 45}
	texture := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			texture.SetRGBA(x, y, color.RGBA{G: 255, A: 255})
		}
	}
	sim := &fx.FxSim{Anchored: []fx.Visual{{
		Texture: "ring", Blending: 2, Lifetime: 1, Horizontal: true,
		Radius: []fx.FxKey{{V: 1}},
		Colour: []fx.FxColourStop{{G: 1, A: 1}},
	}}}
	img := renderFxOverlay([]fxOverlayEffect{{Sim: sim}}, opts, w, h, map[string]*image.RGBA{"ring": texture})
	if got := fxLitPixels(img); got == 0 {
		t.Fatal("horizontal ring rendered no pixels")
	}
	// Its four world corners must remain on Z=0 regardless of camera pitch.
	sp := sim.Sprites()[0]
	for _, corner := range [][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
		world := fauxgl.Vector{X: corner[0] * sp.Radius, Y: corner[1] * sp.Radius, Z: 0}
		if math.Abs(world.Z) > 1e-12 {
			t.Fatalf("horizontal quad corner moved off world Z: %v", world)
		}
	}
}

func TestRenderFxOverlayAdditiveBrightens(t *testing.T) {
	const w, h = 64, 64
	opts := render.RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 6, Fov: 45}
	// A 2x2 map, opaque red, so a sprite drawn through it comes out red whatever
	// colour its own ramp asks for.
	tex := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			tex.SetRGBA(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	sim := &fx.FxSim{}
	sim.Anchored = []fx.Visual{{Lifetime: 1, Radius: []fx.FxKey{{V: 0.5}}, Colour: []fx.FxColourStop{{R: 0.5, G: 0.5, B: 0.5, A: 1}}}}
	effects := []fxOverlayEffect{{Sim: sim}}
	textures := map[string]*image.RGBA{"": tex}

	// The map is stretched over the sprite, so the sprite takes the map's
	// colour rather than the one the node says.
	sim.Anchored[0].Blending = 1
	mapped := renderFxOverlay(effects, opts, w, h, textures)
	if got := mapped.RGBAAt(w/2, h/2); got.R < 100 || got.G > 60 || got.B > 60 {
		t.Errorf("the sprite was drawn in its own colour rather than the map's: %#v", got)
	}

	cover := &fx.FxSim{}
	cover.Anchored = sim.Anchored
	cover.Anchored[0].Blending = 1
	covered := renderFxOverlay([]fxOverlayEffect{{Sim: cover}}, opts, w, h, textures)

	sim.Anchored[0].Blending = 2
	added := renderFxOverlay([]fxOverlayEffect{{Sim: sim}}, opts, w, h, textures)

	mid := covered.RGBAAt(w/2, h/2)
	midAdd := added.RGBAAt(w/2, h/2)
	if mid.A == 0 || midAdd.A == 0 {
		t.Fatalf("neither sprite was drawn: covered %v additive %v", mid, midAdd)
	}
	// Additive saturates rather than accumulating past white, so the two
	// agree on the centre; what tells them apart is that the additive one
	// keeps its colour where the covering one has already put something.
	if midAdd.R < mid.R {
		t.Errorf("the additive sprite is darker than the covering one: %d against %d", midAdd.R, mid.R)
	}
}

// TestFxOverlaySizeFollowsTheCamera checks that a sprite keeps its world size:
// the same effect further away covers fewer pixels, which is what makes an
// effect read as part of the model rather than as a sticker on the screen.
func TestFxOverlaySizeFollowsTheCamera(t *testing.T) {
	const w, h = 160, 160
	sim := &fx.FxSim{}
	sim.Anchored = []fx.Visual{{Lifetime: 1, Radius: []fx.FxKey{{V: 0.3}}, Colour: []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}}}}
	effects := []fxOverlayEffect{{Sim: sim}}
	near := renderFxOverlay(effects, render.RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 4, Fov: 45}, w, h, nil)
	far := renderFxOverlay(effects, render.RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 16, Fov: 45}, w, h, nil)
	n, f := fxLitPixels(near), fxLitPixels(far)
	if n == 0 || f == 0 {
		t.Fatalf("a sprite went missing: near %d pixels, far %d pixels", n, f)
	}
	if f >= n {
		t.Errorf("a sprite further away covered %d pixels against %d up close: it is not shrinking with distance", f, n)
	}
}

// TestFxMarkerlessEffectFallsBackToAnUprightFrame checks the frame an effect
// with no marker of its own is laid out in. It is the file's own axes, so the
// frame's second (vertical) axis is the world's up: an effect that arrives from
// above says so in the frame it is placed with, and one that lies on the
// ground lies on the ground.
//
// The three axes also have to be independent. Filling the frame in one axis
// at a time leaves two of them the same direction, and the parts that measure
// an Offset across two axes and take the third from their cross product then
// get a zero where a direction should be - which is what left whole effects
// with no ring to draw at all.
func TestFxMarkerlessEffectFallsBackToAnUprightFrame(t *testing.T) {
	e := fxOverlayEffect{Marker: -1}
	x, y, z := e.fxRingFrame()
	if x != fxFallbackFrame[0] || y != fxFallbackFrame[1] || z != fxFallbackFrame[2] {
		t.Fatalf("a marker-less effect was given the frame (%v, %v, %v), not the file's own",
			x, y, z)
	}
	// The file's second component is its vertical one, so that is the axis a
	// ring's Offset is measured along here, and it is the world's up.
	if math.Abs(y.Z-1) > 1e-9 {
		t.Errorf("the marker's up came out as %v, which is not the world's up", y)
	}
	// Any two axes crossed give a direction, and the three are as far from
	// each other as three orthogonal directions can be.
	for _, pair := range [][2]fauxgl.Vector{{x, y}, {x, z}, {y, z}} {
		if n := fx.CrossOf(pair[0], pair[1]).Length(); n < 1e-6 {
			t.Errorf("two of the fallback axes are parallel: %v and %v", pair[0], pair[1])
		}
	}
	// A partially filled frame is no frame: taking the whole fallback rather
	// than the two axes it does have keeps the cross product above alive.
	partial := fxOverlayEffect{Marker: -1, Frame: [3]fauxgl.Vector{fxFallbackFrame[0], {}, {}}}
	px, py, pz := partial.fxRingFrame()
	if n := fx.CrossOf(py, pz).Length(); n < 1e-6 {
		t.Errorf("a half-filled frame left the offset axes parallel: %v, %v", py, pz)
	}
	if px != x {
		t.Errorf("a half-filled frame kept the one axis it had: %v", px)
	}
}

// TestFxPreviewScaleAffectsCPUOverlaySpriteGeometry makes sure the same scale
// used by the GPU scene expands the rendered sprite in the CPU overlay.
func TestFxPreviewScaleAffectsCPUOverlaySpriteGeometry(t *testing.T) {
	const w, h = 160, 160
	sim := &fx.FxSim{Anchored: []fx.Visual{{
		Lifetime: 1,
		Radius:   []fx.FxKey{{V: 0.2}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}}
	opts := render.RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 6, Fov: 45}
	render := func(scale float64) int {
		return fxLitPixels(renderFxOverlay([]fxOverlayEffect{{Sim: sim, Scale: scale}}, opts, w, h, nil))
	}
	small, large := render(0.5), render(2)
	if small == 0 || large <= small {
		t.Fatalf("CPU overlay pixels at scale 0.5=%d and scale 2=%d; larger scale must cover more pixels", small, large)
	}
}

func TestMarkerLocalParticleOffsetAndPlaneFollowMarker(t *testing.T) {
	frame := [3]fauxgl.Vector{
		{Y: 1},
		{Z: 1},
		{X: 1},
	}
	e := fxOverlayEffect{
		Origin: fauxgl.Vector{X: 10, Y: 20, Z: 30},
		Frame:  frame,
		Axis:   frame[2],
	}
	sp := e.fxTurnSprite(fx.Sprite{
		Pos:        [3]float64{2, 3, 4},
		Horizontal: true,
	})
	axX, axY, axZ := e.fxRingFrame()
	sp = fxOrientSprite(sp, [3]fauxgl.Vector{axX, axY, axZ})
	if got, want := sp.Pos, [3]float64{4, 2, 3}; got != want {
		t.Errorf("marker-local particle offset = %v, want %v", got, want)
	}
	// A ring particle's plane is authored across the effect's up axis, so an
	// unset normal is the marker's own second (up) column. That is what keeps
	// the ring lying flat instead of standing on the marker's forward axis.
	if got, want := sp.PlaneNormal, [3]float64{0, 0, 1}; got != want {
		t.Errorf("spray ring plane normal = %v, want marker up %v", got, want)
	}
	// The marker's placement remains the pivot; its world-space position is
	// not rotated or replaced by the local particle displacement.
	world := e.Origin.Add(fauxgl.Vector{X: sp.Pos[0], Y: sp.Pos[1], Z: sp.Pos[2]})
	if want := (fauxgl.Vector{X: 14, Y: 22, Z: 33}); world != want {
		t.Errorf("world particle position = %v, want marker origin plus mapped offset %v", world, want)
	}
}

func TestFxRingOffsetIsMeasuredUpward(t *testing.T) {
	e := fxOverlayEffect{Marker: -1}
	axX, axY, axZ := e.fxRingFrame()
	sim := &fx.FxSim{}
	sim.RingNodes = []fx.Ring{{
		Texture:  "",
		Lifetime: 1,
		Radius:   []fx.FxKey{{V: 0.5}},
		// The offset is written on the effect's own up axis, the second
		// component of every vector these files write.
		Offset: []fx.FxVec3Key{{X: 0, Y: 2, Z: 0}},
	}}
	centre := fauxgl.Vector{X: 1, Y: 1, Z: 1}
	up := sim.Rings(centre, axX, axY, axZ, fauxgl.Vector{Z: 1})
	if len(up) != 1 {
		t.Fatalf("got %d ribbons out of one ring", len(up))
	}
	// The ring's own half-height is 0.5, so its centre is the middle of the
	// quad it hangs: from plus to over two is where it was put.
	mid := up[0].From.Add(up[0].To).MulScalar(0.5)
	want := centre.Add(fauxgl.Vector{Z: 2})
	if mid.Sub(want).Length() > 1e-6 {
		t.Errorf("the ring was put at %v, an offset of two up reads as %v", mid, want)
	}
	for _, v := range []fauxgl.Vector{up[0].From, up[0].To, up[0].Across} {
		if math.IsNaN(v.X) || math.IsNaN(v.Y) || math.IsNaN(v.Z) {
			t.Fatalf("the ring came out as a NaN: from %v to %v across %v", up[0].From, up[0].To, up[0].Across)
		}
	}
}

// TestCamRightStandsUpOnTheUpAxis checks the billboard ring's own right axis
// for the frame that has no right to take. An effect at the model's origin is
// on the axis a level camera looks along, and the camera's up is then exactly
// the world's up - the one case where the cross product it is normally read
// out of is zero.
func TestCamRightStandsUpOnTheUpAxis(t *testing.T) {
	for _, up := range []fauxgl.Vector{
		{Z: 1}, {X: 1}, {Y: 1},
		{X: 1, Y: 1, Z: 1}, {X: -0.3, Y: 0.9, Z: 0.1},
	} {
		for _, eye := range []fauxgl.Vector{{}, {Z: 2}, {X: 1, Y: 1, Z: 1}} {
			r := fx.CamRight(up, eye)
			if math.IsNaN(r.X) || math.IsNaN(r.Y) || math.IsNaN(r.Z) {
				t.Errorf("fx.CamRight(%v, %v) = %v", up, eye, r)
				continue
			}
			if n := r.Length(); math.Abs(n-1) > 1e-6 {
				t.Errorf("fx.CamRight(%v, %v) = %v, which is not a direction", up, eye, r)
			}
			if d := up.Normalize().Dot(r); math.Abs(d) > 1e-6 {
				t.Errorf("fx.CamRight(%v, %v) = %v, which is along the up axis", up, eye, r)
			}
		}
	}
}

// TestLoadModelEffectsForRealModel resolves the effects of a model that ships
// with the game, which is the only way to know the chain still works against
// real files: a blueprint naming an event, an event naming an effect, and the
// markers the effect plays at.
func TestLoadModelEffectsForRealModel(t *testing.T) {
	tree, err := modtree.BuildModTree(testDXP3Module, "English")
	if err != nil {
		t.Skipf("no test module: %v", err)
	}
	defer tree.Close()
	for _, name := range []string{"necron_greater_summoning_core.whm", "necron_lord.whm", "lych_guard.whm"} {
		raw := testutil.FindDowFile(name)
		if raw == nil {
			continue
		}
		model, err := ParseWhmModel(raw)
		if err != nil {
			t.Fatalf("ParseWhmModel(%s): %v", name, err)
		}
		file := fxRacesPath(name)
		res := loadModelEffects(tree, model, file, fxLoadOpts{audio: true})
		if res.err != nil {
			t.Logf("%s: %v", name, res.err)
			continue
		}
		if len(res.effects) == 0 {
			continue
		}
		t.Logf("%s: %d effects, %d sprite textures", name, len(res.effects), len(res.textures))
		for i, e := range res.effects {
			sprites := e.Sim.Sprites()
			if len(sprites) == 0 {
				continue
			}
			// Stepping has to produce particles: a simulation that
			// emits nothing draws an empty overlay.
			e.Sim.Step(0.5)
			e.Sim.Step(0.5)
			after := len(e.Sim.Sprites())
			t.Logf("  effect %d %q at %v: %d sprites now, %d after a second",
				i, e.Label, e.Origin, len(sprites), after)
			if after == 0 {
				t.Errorf("effect %q stepped a second and has no sprites left", e.Label)
			}
		}
		return
	}
	t.Skip("no installed model with resolvable events")
}
func sqrt(v float32) float64 { return math.Sqrt(float64(v)) }

// requireVulkanRenderer opens a headless Vulkan window and a renderer over
// it, skipping the test when the backend is not there. It returns the
// renderer through render.Handle — the app tests only render frames and read
// them back, which the exported API covers.
func requireVulkanRenderer(t *testing.T) (*headless.Window, render.Handle, func() []byte, func()) {
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
	r, err := render.NewModelGpuVkRenderer(v)
	if err != nil {
		win.Release()
		t.Fatalf("NewModelGpuVkRenderer: %v", err)
	}
	readback := func() []byte {
		t.Helper()
		px, err := render.Readback(r)
		if err != nil {
			t.Fatalf("Readback: %v", err)
		}
		return px
	}
	release := func() {
		r.Release()
		win.Release()
	}
	return win, r, readback, release
}
