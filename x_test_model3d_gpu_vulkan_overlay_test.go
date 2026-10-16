//go:build windows || linux

package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// The skeleton and the marker frame used to mean "fall back to the software
// renderer": the hardware renderers read nothing but the triangles of the
// mesh, so the one thing the 3D preview could draw on top of a model was the
// one thing only the CPU could show. These tests put the overlay through the
// Vulkan pass and read the frame back, which is the only way to see whether it
// was drawn at all.

// gpuOverlayModel is a chain of four bones with a marker on the leaf, so the
// overlay has several lines to draw and a frame to hang them on.
//
// The box is deliberately much larger than the skeleton: every bone and the
// marker sit well inside the mesh. That is what makes the depth test
// observable — an overlay tested against the model is hidden by the near face
// of the box and draws nothing at all, so a frame that shows the bones at all
// is a frame whose overlay skipped the depth test. A skeleton poking out of
// the mesh would be drawn either way and would prove nothing.
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
