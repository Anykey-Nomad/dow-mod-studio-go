package main

import (
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// The overlay is described once and drawn by two very different rasterisers,
// so what these tests pin down is the description: which segments exist, where
// they start and end, and which way they point. A renderer that draws the
// description wrongly shows up as a geometry test that still passes.

// overlayTestModel is a two bone chain with a marker on the child, so both
// halves of the overlay have something to draw: a bone has a parent to reach,
// and the marker is on a bone that is not the root.
func overlayTestModel() *WhmModel {
	return &WhmModel{
		Name: "overlay_box",
		Vertices: []WhmVertex{
			{Pos: [3]float32{-0.5, -0.5, -0.5}, Normal: [3]float32{-1, -1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, -0.5, -0.5}, Normal: [3]float32{1, -1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, 0.5, -0.5}, Normal: [3]float32{1, 1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{-0.5, 0.5, -0.5}, Normal: [3]float32{-1, 1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{-0.5, -0.5, 0.5}, Normal: [3]float32{-1, -1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, -0.5, 0.5}, Normal: [3]float32{1, -1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, 0.5, 0.5}, Normal: [3]float32{1, 1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{-0.5, 0.5, 0.5}, Normal: [3]float32{-1, 1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
		},
		Meshes: []WhmMesh{{Name: "box", Indices: []uint32{0, 2, 1, 0, 3, 2, 4, 5, 6, 4, 6, 7, 0, 1, 5, 0, 5, 4}}},
		Bones: []WhmBone{
			{Name: "root", Parent: -1},
			{Name: "child", Parent: 0, Pos: [3]float32{0.4, 0, 0}},
		},
		Markers: []WhmMarker{{
			Name:   "marker_tip",
			Parent: "child",
			Matrix: [9]float32{1, 0, 0, 0, 1, 0, 0, 0, 1},
			Pos:    [3]float32{0.3, 0, 0},
		}},
	}
}

// overlayTestOptions is a camera that frames the box: without a field of view
// and a distance every overlay width works out to zero or to nonsense.
func overlayTestOptions() RenderOptions {
	return RenderOptions{
		Width: 400, Height: 300,
		Yaw: 0.6, Pitch: 0.3, Distance: 6,
		Fov: modelDefaultFov, Quality: modelQualityFinal,
	}
}

// A bone is drawn from its parent's origin to its own, which is what makes a
// skeleton read as a skeleton rather than as a cloud of dots: the line has to
// start where the joint above it is.
func TestOverlayBonesJoinTheParentToTheBone(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Skeleton = true
	got := modelOverlayLines(m, pose, opts)
	if len(got) == 0 {
		t.Fatal("no bone lines were built for a two bone skeleton")
	}
	// One bone has one parent, so a two bone chain has one line: the root has
	// no parent and so has nothing to be joined to.
	if len(got) != 1 {
		t.Fatalf("built %d bone lines, want 1 for a two bone chain", len(got))
	}
	s := got[0]
	wantFrom := pose.Bones[0].MulPosition(fauxgl.Vector{})
	wantTo := pose.Bones[1].MulPosition(fauxgl.Vector{})
	if s.From.Sub(wantFrom).Length() > 1e-9 {
		t.Errorf("the line starts at %v, the root's origin is %v", s.From, wantFrom)
	}
	if s.To.Sub(wantTo).Length() > 1e-9 {
		t.Errorf("the line ends at %v, the child's origin is %v", s.To, wantTo)
	}
	if s.HalfWidth <= 0 {
		t.Errorf("the line is %v wide, which draws nothing at all", s.HalfWidth)
	}
}

// The overlay's width is asked for in pixels and converted, so a bone is the
// same thickness on screen at any camera distance. A fixed world width would
// be a different length of line here and there, which is the bug this rule
// exists to prevent.
func TestOverlayWidthIsInScreenPixels(t *testing.T) {
	opts := overlayTestOptions()
	near := modelOverlayHalfWidth(opts, 2)
	opts.Distance *= 4
	far := modelOverlayHalfWidth(opts, 2)
	if math.Abs(near*4-far) > 1e-9 {
		t.Errorf("four times the distance gave a half width of %v against %v, which is not four times %v", far, near, near)
	}
	// Half the pixels is half the width, at the same camera.
	if half := modelOverlayHalfWidth(opts, 1); math.Abs(half*2-far) > 1e-9 {
		t.Errorf("two pixels gave %v and one gave %v, which are not in step", far, half)
	}
}

// The picked marker gets its frame drawn, and its forward arrow points along
// the axis its effects would leave by. The direction is the whole point of the
// arrow, so it is compared against the axis itself rather than merely checked
// for existing.
func TestOverlayMarkerArrowPointsAlongItsAxis(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Marker = pickedMarker(0)
	segments := modelOverlayLines(m, pose, opts)
	if len(segments) == 0 {
		t.Fatal("no overlay was built for a picked marker")
	}
	axis, ok := m.MarkerAxis(0, pose, true)
	if !ok {
		t.Skip("the test marker has no usable forward axis")
	}
	at, _ := m.MarkerWorld(0, pose, true)

	// The arrow is the yellow segment that leaves the marker; the axes are
	// the six shorter ones around it.
	var shaft *modelOverlayLine
	for i := range segments {
		s := &segments[i]
		if s.From.Sub(at).Length() > 1e-9 || s.R != modelMarkerFwdColor[0] {
			continue
		}
		if dir := s.To.Sub(s.From); dir.Dot(axis) > 0.99*dir.Length() {
			shaft = s
			break
		}
	}
	if shaft == nil {
		t.Fatalf("none of the %d overlay segments runs from the marker along its forward %v", len(segments), axis)
	}
	dir := shaft.To.Sub(shaft.From)
	want := axis.MulScalar(modelMarkerAxisWorldLength)
	if math.Abs(dir.Length()-want.Length()) > 1e-9 {
		t.Errorf("the arrow is %v long, the frame reaches %v", dir.Length(), want.Length())
	}
	dot := dir.Normalize().Dot(axis)
	if math.IsNaN(dot) {
		t.Fatalf("the arrow direction came out as %v and cannot be compared", dir)
	}
	if dot < 0.999 {
		t.Errorf("the arrow points %v but the marker's forward is %v (dot=%.4f)", dir.Normalize(), axis, dot)
	}
}

// Nothing asked for, nothing drawn. A frame with no skeleton and no picked
// marker must carry no overlay at all, or every model would come out wearing
// a skeleton the reader never turned on.
func TestOverlayIsEmptyWhenNothingIsAskedFor(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Skeleton = false
	opts.Marker = nil
	if got := modelOverlayLines(m, pose, opts); len(got) != 0 {
		t.Errorf("built %d overlay segments for a frame that asked for none", len(got))
	}
	// A stale marker index, which is what a model replaced under a viewer
	// that still remembers the old one looks like, must not read past the end.
	stale := len(m.Markers) + 3
	opts.Marker = &stale
	if got := modelOverlayLines(m, pose, opts); len(got) != 0 {
		t.Errorf("built %d overlay segments for marker %d of a model with %d", len(got), stale, len(m.Markers))
	}
	if got := modelOverlayLines(nil, pose, opts); len(got) != 0 {
		t.Errorf("built %d overlay segments for no model at all", len(got))
	}
}

// The software renderer draws the same segments, so the skeleton it has always
// drawn must still be there. This is the regression guard on the rewrite: the
// bones used to be drawn by drawSkeleton, which this replaced with a shared
// builder, and a builder that produced nothing would leave the software frame
// silently bare.
func TestSoftwareFrameStillDrawsTheSkeleton(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Target = vec3From(m.BoundMin).Add(vec3From(m.BoundMax)).MulScalar(0.5)
	without := RenderWhmModel(m, pose, opts)
	opts.Skeleton = true
	with := RenderWhmModel(m, pose, opts)
	changed := 0
	for i := 0; i+3 < len(without.Pix) && i+3 < len(with.Pix); i += 4 {
		if without.Pix[i] != with.Pix[i] || without.Pix[i+1] != with.Pix[i+1] || without.Pix[i+2] != with.Pix[i+2] {
			changed++
		}
	}
	t.Logf("%d pixels differ between the frame with and without the skeleton", changed)
	if changed == 0 {
		t.Error("turning the skeleton on changed nothing on the software path")
	}
}
