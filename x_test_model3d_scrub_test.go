package main

import (
	"dow-mod-studio/internal/whe"
	"image"
	"math"
	"testing"

	"gioui.org/f32"
)

// The strip is a ruler over the model: a frame's tick, the number above it and
// the frame a click on it lands on must all be the same frame, or the reader is
// told one thing and shown another.

func TestScrubBarSplitsTheWidthIntoOneSlicePerFrame(t *testing.T) {
	const frames = 8
	const left, width = 10.0, 800.0
	cases := []struct {
		x    float32
		want int
	}{
		{left, 0},
		{left + 1, 0},
		// A pixel under frame 1's tick is frame 1: the strip rounds to the
		// nearest tick rather than splitting the width into slices, so the
		// click lands on the tick it was aimed at — and a click on a tick
		// reads that tick's frame, which is what the header above demands.
		{left + width/frames - 1, 1},
		{left + width/frames, 1},
		{left + width/2, 4},
		{left + width - 1, 7},
	}
	for _, c := range cases {
		if got := scrubBarAt(c.x, left, width, frames); got != c.want {
			t.Errorf("a click at %.0f lands on frame %d, want %d", c.x, got+1, c.want+1)
		}
	}
	// Past the ends is clamped rather than refused: a press on the margin next
	// to the strip is the nearest frame, not nothing at all.
	if got := scrubBarAt(left-50, left, width, frames); got != 0 {
		t.Errorf("a click left of the strip gave frame %d, want the first", got)
	}
	if got := scrubBarAt(left+width+50, left, width, frames); got != frames-1 {
		t.Errorf("a click right of the strip gave frame %d, want the last", got)
	}
}

// The number under a tick, the tick itself and the frame the playhead reports
// are three readings of one position, so a timeline event at 0.5 of a 10 frame
// clip is frame 6 and not frame 5 or 7.
func TestTimelineTimesLandOnTheFrameThePlayheadReports(t *testing.T) {
	const frames = 10
	cases := []struct {
		at   float64
		want int
	}{
		{-1, 0}, // on entering the clip
		{0, 0},
		{0.05, 1}, // rounds
		{0.5, 5},
		{0.55, 6}, // rounds up
		{0.999, 9},
		{3.4e38, 9}, // on leaving it
	}
	for _, c := range cases {
		if got := frameAt(c.at, frames); got != c.want {
			t.Errorf("a normalized timeline time of %g lands on frame %d, want %d", c.at, got, c.want)
		}
	}
	// And the playhead put at that frame reads the same frame back.
	v := &modelViewer{anim: &WhmAnimation{Name: "c", NumFrames: frames}}
	for _, c := range cases {
		v.seekFrame(frameAt(c.at, frames))
		got, _ := v.currentFrame()
		if got != c.want {
			t.Errorf("seeking to the frame for %g reads back as %d, want %d", c.at, got, c.want)
		}
	}
}

func TestSeekingStopsPlaybackAndClampsToTheClip(t *testing.T) {
	v := &modelViewer{anim: &WhmAnimation{Name: "land", NumFrames: 62}, playing: true}
	v.seekFrame(61)
	if v.playing {
		t.Error("a seek left playback running; the reader asked to stop on a frame")
	}
	if f, _ := v.currentFrame(); f != 61 {
		t.Errorf("seeked to the last frame reads back as %d", f)
	}
	// Past either end is the nearest frame, not a playhead off the clip.
	v.seekFrame(1000)
	if f, _ := v.currentFrame(); f != 61 {
		t.Errorf("a seek past the end landed on %d, want the last frame", f)
	}
	v.seekFrame(-5)
	if f, _ := v.currentFrame(); f != 0 {
		t.Errorf("a seek before the start landed on %d, want the first frame", f)
	}
}

func TestTheStripCatchesAPressAndLeavesTheCameraAlone(t *testing.T) {
	// The overlay pads the bar by a label's height on each side, so the strip
	// a press is tested against is taller than the five pixel line drawn on
	// it: a drag that runs off the bar is still a seek.
	hit := int(scrubLabelHeight)
	strip := image.Rect(0, 100-hit, 800, 116+hit)
	if !scrubHit(strip, f32.Pt(400, 108)) {
		t.Error("a press on the bar was not caught")
	}
	// Above and below the numbers is still the strip: a drag that runs off the
	// five pixel bar must not suddenly become a camera turn.
	for _, y := range []float32{92, 124} {
		if !scrubHit(strip, f32.Pt(400, y)) {
			t.Errorf("a press at y=%.0f, beside the bar, was not caught", y)
		}
	}
	if scrubHit(strip, f32.Pt(400, 60)) {
		t.Error("a press on the model above the strip was taken for a seek")
	}
	var empty = image.Rectangle{}
	if scrubHit(empty, f32.Pt(400, 108)) {
		t.Error("an undrawn strip caught a press")
	}
}

// The frames worth stopping at are the ones the .whe fires something on, and
// the bar has to say which kind of thing it was.
func TestTheStripMarksTheFramesEventsFireOn(t *testing.T) {
	v := &modelViewer{
		// Sixty frames at the default twenty a second is three seconds
		// long, which is what the timeline's own seconds are read against:
		// the moments in a .whe are seconds into the clip, not a fraction
		// of it.
		anim: &WhmAnimation{Name: "landing", NumFrames: 60},
		fxTimelines: []whe.FxTimeline{
			{Name: "landing", Animations: []string{"landing"}, Events: []whe.FxTimelineEvent{
				{Name: "land_dust", Time: -1},
				{Name: "land_halo", Time: 0.5},
			}},
			{Name: "idle", Animations: []string{"idle"}, Events: []whe.FxTimelineEvent{
				{Name: "crystal_glow", Time: 0},
			}},
		},
	}
	got := v.clipScrubEvents("landing")
	if len(got) != 2 {
		t.Fatalf("landing has %d marked frames, want 2", len(got))
	}
	// -1 is on entering, which is the first frame; half a second of a three
	// second clip is the eleventh.
	if got[0].Frame != 0 || got[1].Frame != 10 {
		t.Errorf("landing is marked on frames %d and %d, want 0 and 10", got[0].Frame, got[1].Frame)
	}
	if got[0].Name != "land_dust" || got[1].Name != "land_halo" {
		t.Errorf("landing is marked as %q and %q", got[0].Name, got[1].Name)
	}
	// Another clip's events do not leak onto this one.
	if other := v.clipScrubEvents("idle"); len(other) != 1 || other[0].Name != "crystal_glow" {
		t.Errorf("idle is marked as %v", other)
	}
	if none := v.clipScrubEvents("phase_in"); len(none) != 0 {
		t.Errorf("a clip nothing fires on is marked %v", none)
	}
}

func TestAModelWithoutTimelinesHasNothingToMark(t *testing.T) {
	v := &modelViewer{anim: &WhmAnimation{Name: "any", NumFrames: 10}}
	if got := v.clipScrubEvents("any"); len(got) != 0 {
		t.Errorf("a model with no .whe marked %d frames", len(got))
	}
}

// The thinning: a 362 frame clip cannot carry a number under every tick, and
// the step that keeps the labels readable must still reach the last frame.
func TestFrameSpacingThinsButKeepsTheEnds(t *testing.T) {
	for _, n := range []int{4, 62, 362} {
		step := int(math.Max(1, math.Floor(float64(n)/float64(float32(900)/scrubLabelPx))))
		if step < 1 {
			t.Fatalf("%d frames: step %d", n, step)
		}
		if float64(step) < 1 {
			t.Fatalf("%d frames: step %d", n, step)
		}
		// Every frame is still reachable by a click, however thin the ticks.
		for f := 0; f < n; f++ {
			// The bar is drawn inside an offset, so a frame's place on it
			// is measured from its left edge while the click carries the
			// position of the whole canvas.
			const left = float32(10)
			x := left + frameX(f, n, 800)
			if got := scrubBarAt(x, left, 800, n); got != f {
				t.Errorf("%d frames: the tick for frame %d clicks to %d", n, f, got)
			}
		}
	}
}
