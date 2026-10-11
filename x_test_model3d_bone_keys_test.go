package main

import (
	"testing"

	"github.com/fogleman/fauxgl"
)

// clipKeyFrames is the set of frames the strip marks: every key of every
// track, sorted and with the repeats other tracks leave behind removed.
func TestClipKeyFramesCollectsAndDeduplicates(t *testing.T) {
	anim := &WhmAnimation{
		Name:      "move",
		NumFrames: 100,
		Bones: []WhmTrack{
			{Bone: 0, PosKey: []WhmKey3{{Time: 0}, {Time: 0.5}}},
			// The second bone keys the same first frame: one mark, not two.
			{Bone: 1, PosKey: []WhmKey3{{Time: 0}, {Time: 0.25}}},
			{Bone: 1, RotKey: []WhmKey4{{Time: 0.75}}},
		},
		Meshes: []WhmMeshTrack{
			{Name: "body", Keys: []WhmKey3{{Time: 0.5}, {Time: 1}}},
		},
	}
	got := clipKeyFrames(anim)
	want := []int{0, 25, 50, 75, 99}
	if len(got) != len(want) {
		t.Fatalf("clipKeyFrames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("clipKeyFrames[%d] = %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
	// Sorted, because the strip draws them in order and the snap walks them.
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("clipKeyFrames is not sorted: %v", got)
		}
	}
}

// A clip with no keys marks nothing; neither does a clip with no frame count.
func TestClipKeyFramesEmptyWhenThereAreNoKeys(t *testing.T) {
	if got := clipKeyFrames(&WhmAnimation{Name: "idle", NumFrames: 10}); len(got) != 0 {
		t.Errorf("a keyless clip marked %v", got)
	}
	if got := clipKeyFrames(nil); got != nil {
		t.Errorf("a nil clip marked %v", got)
	}
	if got := clipKeyFrames(&WhmAnimation{Name: "odd", NumFrames: 0, Bones: []WhmTrack{{PosKey: []WhmKey3{{Time: 0.5}}}}}); got != nil {
		t.Errorf("a clip with no frames marked %v", got)
	}
}

// A click on a keyframe mark winds to the keyframe; a click away from every
// mark winds to the frame under it instead.
func TestSnapKeyFrameFindsTheMarkUnderTheClick(t *testing.T) {
	const width = 800
	keys := []int{0, 50, 99}
	// On the middle keyframe's pixel.
	x := float32(scrubBarMargin) + frameX(50, 100, width)
	if f, ok := snapKeyFrame(x, keys, scrubBarMargin, width, 100); !ok || f != 50 {
		t.Errorf("a click on the mark found key %d (ok=%v), want 50", f, ok)
	}
	// A few pixels off it still snaps: the mark is thin and the hand is not.
	if f, ok := snapKeyFrame(x+scrubKeySnapPx-1, keys, scrubBarMargin, width, 100); !ok || f != 50 {
		t.Errorf("a click %dpx off the mark found key %d (ok=%v), want 50", scrubKeySnapPx-1, f, ok)
	}
	// Halfway between two marks: no snap at all.
	gap := frameX(25, 100, width)
	if _, ok := snapKeyFrame(scrubBarMargin+gap, keys, scrubBarMargin, width, 100); ok {
		t.Error("a click between marks snapped to a keyframe; free seeking is lost")
	}
	// No keys: no snap.
	if _, ok := snapKeyFrame(x, nil, scrubBarMargin, width, 100); ok {
		t.Error("a click snapped to a keyframe in a clip that has none")
	}
}

// The picked bone gets its segment in the pick colour, whether or not the
// skeleton is also on — the bone is picked out of a list, and a reader who
// opened only the bones list still has to see which one they picked.
func TestOverlayDrawsThePickedBone(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.PickedBone = pickedBone(1)

	segments := modelOverlayLines(m, pose, opts)
	if len(segments) == 0 {
		t.Fatal("picking a bone built no overlay at all")
	}
	found := false
	for _, s := range segments {
		if s.R == modelBonePickColor[0] && s.G == modelBonePickColor[1] && s.B == modelBonePickColor[2] {
			found = true
			if s.HalfWidth <= 0 {
				t.Error("the picked bone's segment draws nothing: zero width")
			}
		}
	}
	if !found {
		t.Fatalf("none of the %d segments is in the pick colour %v", len(segments), modelBonePickColor)
	}

	// The root bone has no parent-to-self segment, so it gets a cross at
	// its joint instead — three segments, all through the origin.
	opts.PickedBone = pickedBone(0)
	cross := 0
	origin := pose.Bones[0].MulPosition(fauxgl.Vector{})
	for _, s := range modelOverlayLines(m, pose, opts) {
		if s.R != modelBonePickColor[0] {
			continue
		}
		cross++
		// The origin is the arm's middle, not its end: a cross spans
		// through the joint it marks.
		mid := s.From.Add(s.To).MulScalar(0.5)
		if mid.Sub(origin).Length() > 1e-6 {
			t.Errorf("the root cross arm runs %v..%v, centred on %v, want it through %v", s.From, s.To, mid, origin)
		}
	}
	if cross != 3 {
		t.Errorf("the picked root bone drew %d arms, want 3", cross)
	}
}

// A stale bone index — a model replaced under a viewer that still remembers
// the old list — draws nothing rather than reading past the end.
func TestOverlayPickedBoneStaleIndexDrawsNothing(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	stale := len(m.Bones) + 3
	opts.PickedBone = pickedBone(stale)
	if got := modelOverlayLines(m, pose, opts); len(got) != 0 {
		t.Errorf("bone %d of a model with %d built %d segments", stale, len(m.Bones), len(got))
	}
	opts.PickedBone = pickedBone(-1)
	if got := modelOverlayLines(m, pose, opts); len(got) != 0 {
		t.Errorf("\"nothing picked\" built %d segments", len(got))
	}
}
