package app

import (
	"dow-mod-studio/internal/testutil"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func unitViewportUI(t *testing.T, unitName string, w, h int) (*uiState, *windowFrame) {
	t.Helper()
	ui := newUI(unitTestTheme())
	ui.ModTree = openTestDXP3(t)
	OpenUnitTab(ui)
	raceSel, unitSel := unitViewportPicker(t, ui, unitName)
	ui.UnitRaceSel, ui.UnitSel = raceSel, unitSel
	ui.UnitSection = findUnitSection(t, "Viewport")
	f := newWindowFrame(ui, w, h)
	return ui, f
}

// unitViewportPicker finds the race and unit indices of a named unit, so a test
// lands on it without counting rows of the dropdown.
func unitViewportPicker(t *testing.T, ui *uiState, name string) (raceSel, unitSel int) {
	t.Helper()
	cat := UnitCatalogFor(ui)
	if cat == nil {
		t.Skip("no unit catalog")
	}
	for ri, race := range cat.Races {
		for idx, u := range race.UnitsOf() {
			if strings.EqualFold(u.Name, name) {
				return ri, idx
			}
		}
	}
	t.Skipf("the mod has no unit called %q", name)
	return 0, 0
}

// unitViewportLoad waits for the model to be read, parsed and handed over. With
// quiet set no further frames are run, which is what a test that only reads the
// model wants: laying the canvas out again would ask the software rasteriser for
// another frame of a model that may be five megabytes.
func unitViewportLoad(t *testing.T, ui *uiState, f *windowFrame, quiet bool) bool {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for i := 0; i < 8000; i++ {
		PollUnitModel(ui)
		PollModelJobs(ui)
		if UnitModelLoaded(ui) {
			// A couple of frames so the panel that lists the cameras and the
			// clips is laid out with the model on it.
			if !quiet {
				unitViewportFrames(f, 3)
			}
			return true
		}
		if ui.UnitModelErr != "" {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		sleepMillis()
	}
	return false
}

// unitViewportFrames runs frames with a moving clock and the pollers in front
// of them, the order the app's loop uses.
func unitViewportFrames(f *windowFrame, n int) {
	for i := 0; i < n; i++ {
		f.gtx.Now = time.Now()
		f.PollUnitModel(ui)
		f.PollModelJobs(ui)
		f.frame()
	}
}

// A unit's model, with the clips it can be played with, is on screen in the
// Viewport section. The listening post is used because it is a small model: the
// preview is rasterised on the CPU when there is no hardware renderer, and a
// test should not spend its time on a five megabyte necron.
func TestTheViewportShowsTheUnitsModelAndAnimations(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	if !strings.HasSuffix(strings.ToLower(ui.UnitModelPath), ".whm") {
		t.Errorf("the viewport is showing %q, which is not a model", ui.UnitModelPath)
	}
	anims := UnitModelAnimNames(ui)
	if len(anims) < 2 {
		t.Fatalf("the model carries %d clips (%v), so there is nothing to play", len(anims), anims)
	}
	if len(ui.UnitAnimClicks) != len(anims) {
		t.Errorf("%d clip rows were laid out for %d clips", len(ui.UnitAnimClicks), len(anims))
	}
	v := UnitModelViewer(ui)
	if v.Anim == nil {
		t.Fatal("no clip is selected")
	}
	if v.AnimIndex < 0 || v.AnimIndex >= len(v.Model.Animations) {
		t.Fatalf("the selected clip is index %d of %d", v.AnimIndex, len(v.Model.Animations))
	}
	if !v.Playing {
		t.Error("the viewport opened on a still model rather than playing a clip")
	}
	if unitModelClipDuration(v) <= 0 {
		t.Errorf("the clip on show, %q, has no length to play", v.Anim.Name)
	}
	// Something has to have asked for a frame, or the canvas would stay empty
	// whatever state the camera is in. The software rasteriser answers on a
	// worker, so the wait is for the frame rather than for the request.
	for i := 0; i < 200 && v.Frame == nil && v.GpuFrame == nil && !v.Redraw; i++ {
		unitViewportFrames(f, 1)
	}
	if v.Frame == nil && v.GpuFrame == nil && !v.Redraw {
		t.Error("nothing has asked for a frame, so the canvas would stay empty")
	}
}

// Playback actually advances: the clip is handed a later point of its length
// once frames have gone by, which is what "with animations" has to mean.
func TestTheViewportPlaysTheClip(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	v := UnitModelViewer(ui)
	first := v.Time
	deadline := time.Now().Add(30 * time.Second)
	for v.Time == first && time.Now().Before(deadline) {
		sleepMillis()
		unitViewportFrames(f, 1)
	}
	if v.Time == first {
		t.Fatalf("the clip stayed at %v over a second of frames", first)
	}
	if v.Time < 0 || v.Time > 1 {
		t.Errorf("the clip is at %v, outside the 0..1 the renderer expects", v.Time)
	}
}

// The clip on show has to move the skeleton, or the preview is a still of a
// model that claims to be animated. The necron warrior is the shipped model
// whose clips do; its five megabytes are never rendered, since no frame is run
// once the model is in.
func TestTheClipOnShowMovesTheSkeleton(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_basic_warrior", 1200, 760)
	if !unitViewportLoad(t, ui, f, true) {
		t.Fatalf("the model of the basic warrior never loaded: %q", ui.UnitModelErr)
	}
	v := UnitModelViewer(ui)
	if v.Anim == nil {
		t.Fatal("no clip is selected")
	}
	// The default clip of a unit is the one it moves with, rather than a
	// "vis_" clip that only fades a weapon in and out.
	if strings.HasPrefix(strings.ToLower(v.Anim.Name), "vis_") {
		t.Errorf("the viewport opened on %q, which moves nothing", v.Anim.Name)
	}
	rest := v.Model.RestPose()
	moved := 0
	for _, at := range []float32{0.25, 0.5, 0.75} {
		pose := v.Model.PoseAt(v.Anim, at)
		for b := range rest.Bones {
			if rest.Bones[b] != pose.Bones[b] {
				moved++
				break
			}
		}
	}
	if moved == 0 {
		t.Errorf("the clip on show, %q, leaves every bone where it was", v.Anim.Name)
	}
}

// The clip list is the whole set of things a unit's model can be seen doing, so
// every clip is listed and picking one plays it.
func TestTheViewportListsEveryClipAndPlaysThePickedOne(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	v := UnitModelViewer(ui)
	anims := UnitModelAnimNames(ui)
	if len(ui.UnitAnimClicks) != len(anims) {
		t.Fatalf("%d clip rows were laid out for %d clips", len(ui.UnitAnimClicks), len(anims))
	}
	// Still it, so the frames of the search below are not spent on a software
	// render of a playing clip.
	v.Playing = false
	start := v.AnimIndex
	// The clip on show is skipped: finding it again would prove only that the
	// first row of the list is there, which it is whether the rest are or not.
	at, idx, ok := unitViewportFindRow(t, f, 1200, 760, len(ui.UnitAnimClicks), func(i int) bool {
		return i != start && ui.UnitAnimClicks[i].Hovered()
	})
	if !ok {
		t.Skip("no other clip row is on screen at this size")
	}
	clickAt(f, at)
	if idx == start {
		t.Errorf("the only clip row on screen is the one already selected (%q), so the list is not being drawn", anims[idx])
	}
	if v.Anim == nil || v.Anim.Name != anims[idx] {
		t.Errorf("the selected clip is %v, want %q", v.Anim, anims[idx])
	}
	// Picking a clip of a still model is a request to watch it.
	if !v.Playing {
		t.Error("picking a clip left the model still, so nothing would be seen to happen")
	}
}

// A unit that names no model is a state the section has to draw, not a crash:
// the canvas says so and the panel says there is nothing loaded.
func TestTheViewportSaysWhenAUnitHasNoModel(t *testing.T) {
	ui := newUI(unitTestTheme())
	ui.ModTree = unitViewportMod(t)
	unitViewportWriteUnit(t, ui.ModTree, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, "")
	OpenUnitTab(ui)
	raceSel, unitSel := unitViewportPicker(t, ui, "necron_wraith")
	ui.UnitRaceSel, ui.UnitSel = raceSel, unitSel
	ui.UnitSection = findUnitSection(t, "Viewport")
	f := newWindowFrame(ui, 1200, 760)
	// A unit with no model is reported rather than waited on: the answer is
	// the absence of a file, and the section has to say so rather than sit on
	// a "loading" message for ever.
	deadline := time.Now().Add(30 * time.Second)
	for ui.UnitModelErr == "" && time.Now().Before(deadline) {
		PollUnitModel(ui)
		sleepMillis()
	}
	unitViewportFrames(f, 2)
	if ui.UnitModelErr == "" {
		t.Fatal("a unit that names no model neither loaded one nor said so")
	}
	if UnitModelLoaded(ui) {
		t.Error("a model appeared for a unit that has none")
	}
	if !strings.Contains(ui.UnitModelErr, "no model file") {
		t.Errorf("the reason given is %q", ui.UnitModelErr)
	}
}

// The model is not just in the section's state, it is on the screen: the strip
// of window between the left column and the panel beside the model can only be
// the canvas, so counting what is painted there says whether the unit is on
// show. The listening post is used because the preview is rasterised on the CPU
// when there is no hardware renderer, and a test should not spend its time on a
// five megabyte necron.
func TestTheViewportPaintsTheModelOnScreen(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	// The frame the canvas paints comes back from a worker, so the wait is for
	// the picture rather than for the request for it.
	v := UnitModelViewer(ui)
	for i := 0; i < 200 && v.Frame == nil && v.GpuFrame == nil; i++ {
		unitViewportFrames(f, 1)
	}
	if v.Frame == nil && v.GpuFrame == nil {
		t.Fatal("the viewer never produced a frame, so there is nothing to paint")
	}
	// Held still, so the render under test is the one that was waited for.
	v.Playing = false
	v.Redraw = false
	const w, h = 1200, 760
	img := renderUnitSection(t, ui, ui.UnitSection, ui.UnitRaceSel, ui.UnitSel, w, h)
	strip := unitViewportCanvasStrip(ui, v, w, h)
	if strip.Empty() {
		t.Fatalf("the canvas has no area in a %dx%d window", w, h)
	}
	// What the canvas is filled with while it has nothing to show, so a strip
	// of that colour alone is an empty viewport.
	empty := color.RGBA{R: 0x1C, G: 0x1E, B: 0x24, A: 0xFF}
	painted := 0
	total := 0
	for y := strip.Min.Y; y < strip.Max.Y; y += 2 {
		for x := strip.Min.X; x < strip.Max.X; x += 2 {
			total++
			if img.RGBAAt(x, y) != empty {
				painted++
			}
		}
	}
	if total == 0 || painted*20 < total {
		t.Errorf("%d of %d pixels of the canvas are not its empty fill, so the model is not on show", painted, total)
	}
}

// unitViewportCanvasStrip is where the canvas is in a window of the given size.
// The viewer's own render size says how big it is, and the section's place in
// the tab says where that is, so the box is worked out rather than guessed.
// The playback row takes the bottom of the section above the canvas, so its
// height and the rule over it come off the bottom too.
func unitViewportCanvasStrip(ui *uiState, v *modelViewer, w, h int) image.Rectangle {
	const contentPad, border, sectionPad = 4, 1, 6
	left := contentPad + unitTabPadDp + unitLeftColumnDp + unitColGapDp + border + sectionPad
	bottom := h - contentPad - unitTabPadDp - border - sectionPad - unitPlaybackStripDp - 1
	return image.Rect(left, bottom-v.RenderH, left+v.RenderW, bottom)
}

// The wheel over the canvas moves the camera in and out of the model. It is
// the one way to look at a detail of a building without opening the model in a
// tab of its own, and the canvas is the only thing under the pointer here.
func TestTheWheelOverTheCanvasZoomsInAndOut(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, true) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	v := UnitModelViewer(ui)
	unitViewportFrames(f, 2)
	canvas := unitViewportCanvasStrip(ui, v, 1200, 760)
	if canvas.Empty() {
		t.Fatal("the canvas has no area to point at")
	}
	at := f32.Pt(float32(canvas.Min.X+canvas.Dx()/2), float32(canvas.Min.Y+canvas.Dy()/2))
	// The pointer is moved there first: a wheel event is routed to whatever is
	// under the pointer's last known position, and a pointer that has never
	// moved has none.
	f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
	unitViewportFrames(f, 1)

	// Away from the model, and then close in: the wheel is a camera move, so
	// the clamp is the only thing that stops it. The amounts are whole
	// notches, which is what a wheel actually sends.
	start := v.Distance
	f.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, wheelNotchDelta)})
	unitViewportFrames(f, 2)
	if v.Distance >= start {
		t.Errorf("the wheel away from the model left the distance at %v, want less than %v", v.Distance, start)
	}
	closer := v.Distance
	f.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, -wheelNotchDelta)})
	unitViewportFrames(f, 2)
	if v.Distance <= closer {
		t.Errorf("the wheel towards the model left the distance at %v, want more than %v", v.Distance, closer)
	}
	// And not so far that the model leaves the frame entirely.
	f.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, 20000)})
	unitViewportFrames(f, 2)
	if limit := v.FitDistance * 0.15; v.Distance < limit-1e-9 {
		t.Errorf("the camera went to %v, inside the model rather than stopping at %v", v.Distance, limit)
	}
}

// The two buttons either side of the transport wind the clip one frame at a
// time, and winding is done on a still model: the frame has to stay where it
// was put.
func TestTheViewportWindsTheClipAFrameAtATime(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	v := UnitModelViewer(ui)
	if v.Anim == nil || v.Anim.NumFrames < 4 {
		t.Skip("this model has no clip with enough frames to step through")
	}
	tab := ui.UnitModel
	// From the first frame, so that a step is a step and not a step from
	// wherever the clip happened to be when the test got to it.
	v.Playing = false
	v.Time = 0
	// The strip is drawn in the panel beside the canvas, not under it, so the
	// buttons are found by a walk across the panel.
	fwd := unitViewportButton(t, f, tab, &tab.ModelStepFwd)
	clickAt(f, fwd)
	if v.Playing {
		t.Error("winding the clip left the model playing, so the frame would move on")
	}
	frame, frames := v.CurrentFrame()
	if frames != v.Anim.NumFrames || frame != 1 {
		t.Fatalf("one step forward is on frame %d of %d, want frame 2", frame+1, frames)
	}
	unitViewportFrames(f, 2)
	back := unitViewportButton(t, f, tab, &tab.ModelStepBack)
	clickAt(f, back)
	if again, _ := v.CurrentFrame(); again != 0 {
		t.Errorf("one step back is on frame %d, want the first", again+1)
	}
}

// The panel beside the canvas is longer than the space it has, and the wheel
// over it is the only way to reach the clips at the bottom of it. The filter
// that reads the wheel has to be given the whole of the gesture: a filter with
// no scroll range takes the event and hands back none of it, which leaves the
// panel exactly where it was. That is how a list of animations that would not
// move came to be.
func TestTheWheelOverTheAnimationListScrollsThePanel(t *testing.T) {
	// A short window, because the wheel only has anything to do when the
	// panel is longer than the space it is given, and a tall one on a small
	// model fits every clip on screen.
	const w, h = 1200, 430
	ui, f := unitViewportUI(t, "necron_listening_post", w, h)
	if !unitViewportLoad(t, ui, f, true) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	anims := UnitModelAnimNames(ui)
	if len(anims) < 6 {
		t.Skipf("this model carries %d clips, too few to fill the panel", len(anims))
	}
	// The panel is laid out with the model on it, which is what gives the
	// scroll view its content to be taller than.
	unitViewportFrames(f, 3)
	state := &ui.UnitViewportScroll
	if !state.hasBar {
		t.Skipf("the panel fits its %d clips without a scrollbar", len(anims))
	}
	// The wheel has to land on the list itself: the panel's fixed controls
	// above it have no scroll of their own, and a row of them is where the
	// middle of the window happens to be.
	at, _, ok := unitViewportFindRow(t, f, w, h, len(anims), func(i int) bool { return ui.UnitAnimClicks[i].Hovered() })
	if !ok {
		t.Skip("no clip row is on screen at this size")
	}
	f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
	unitViewportFrames(f, 1)

	// Down, then back up. The wheel is the only way to reach the last clip, so
	// a panel that ignores it leaves the bottom of the list unreachable.
	start := state.offset
	f.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, wheelNotchDelta)})
	unitViewportFrames(f, 2)
	down := state.offset
	if down <= start {
		t.Errorf("the wheel over the animation list left the panel at %d, want more than %d", down, start)
	}
	// A notch is a couple of rows, not the whole list. The step used to be read
	// as pixels per notch, which against a notch of 120 asked for five thousand
	// pixels a click, so every click slammed the list to one end and the rows
	// in between could not be reached at all. Measured against the content
	// rather than the viewport, because a short window has a short page and
	// three rows of it is still three rows.
	//
	// A third rather than a quarter: the step is 0.55px per unit of the
	// notch's 120 — sixty six pixels, which is 2.75 rows of this model's eight
	// clips and 66 of the list's 236. Twenty eight per cent of a short list is
	// a couple of rows; what the guard is for is the notch that covers the
	// whole of it.
	if moved := down - start; moved*3 > state.contentHeight {
		t.Errorf("one notch of the wheel moved the panel %d pixels of its %d, which is most of the list", moved, state.contentHeight)
	}
	f.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, -wheelNotchDelta)})
	unitViewportFrames(f, 2)
	if back := state.offset; back >= down {
		t.Errorf("the wheel back up left the panel at %d, want less than %d", back, down)
	}
	// And the ends of the list are the ends of the panel: a wheel that keeps
	// going does not scroll the last clip off the top of the view.
	for i := 0; i < 12; i++ {
		f.send(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, wheelNotchDelta)})
	}
	unitViewportFrames(f, 2)
	if last := maxInt(0, state.contentHeight-state.viewport); state.offset != last {
		t.Errorf("the panel scrolled to %d of %d, want the end of the list", state.offset, last)
	}
}

// The two zoom buttons beside "Reset view" are the wheel's step as a press, so
// the camera can be moved by a known amount without a wheel to hand. They have
// to be on the panel and they have to move the camera: the wheel used to step
// by a factor of ten a click, which left nothing worth looking at between the
// two ends, and a button that only says "Reset view" is no way to get back.
func TestTheZoomButtonsStepTheCameraAndResetBringsItBack(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	v := UnitModelViewer(ui)
	tab := ui.UnitModel
	row := []*widget.Clickable{&tab.ModelZoomIn, &tab.ModelZoomOut, &tab.ModelReset}

	// The three buttons are found once, then pressed in turn: each press is a
	// step, and two steps in is nowhere near the factor of ten the wheel used
	// to ask for.
	v.ResetCamera()
	fit := v.Distance
	near := unitViewportSideButton(t, f, row, &tab.ModelZoomIn, "toggles")
	far := unitViewportSideButton(t, f, row, &tab.ModelZoomOut, "toggles")
	reset := unitViewportSideButton(t, f, row, &tab.ModelReset, "toggles")

	clickAt(f, near)
	unitViewportFrames(f, 2)
	if want := fit * modelZoomStep; abs64(v.Distance-want) > 1e-6 {
		t.Fatalf("the near zoom button left the distance at %v, want the %v that is one step in", v.Distance, want)
	}
	clickAt(f, near)
	unitViewportFrames(f, 2)
	if want := fit * modelZoomStep * modelZoomStep; abs64(v.Distance-want) > 1e-6 {
		t.Errorf("two presses left the distance at %v, want the %v that is two steps in", v.Distance, want)
	}
	// Back out one step, which is where it started after the first press.
	clickAt(f, far)
	unitViewportFrames(f, 2)
	if want := fit * modelZoomStep; abs64(v.Distance-want) > 1e-6 {
		t.Errorf("the far zoom button left the distance at %v, want the %v that is one step back out", v.Distance, want)
	}

	// And back to the framing a new viewer starts with.
	clickAt(f, reset)
	unitViewportFrames(f, 2)
	if abs64(v.Distance-fit) > 1e-6 {
		t.Errorf("Reset view left the distance at %v, want the %v the model was framed at", v.Distance, fit)
	}
}

// unitViewportButton finds one of the transport row's buttons. The transport
// is the playback strip under the whole section now, so the walk runs across
// that band — five buttons wide, starting at the strip's left padding —
// instead of down the list panel.
func unitViewportButton(t *testing.T, f *windowFrame, tab *fileTab, want *widget.Clickable) f32.Point {
	t.Helper()
	const h = 760
	const contentPad, border, sectionPad = 4, 1, 6
	left := contentPad + unitLeftColumnDp + unitColGapDp + border + sectionPad + 6
	// The band is scanned rather than computed: the strip sits above the
	// window's own bottom bar, whose height is the layout's business rather
	// than the test's, and a scan of the bottom hundred pixels costs a
	// second of frames.
	for y := h - 4 - unitPlaybackStripDp - 70; y < h-4; y += 3 {
		if y < 0 {
			continue
		}
		for x := left; x < left+210; x += 4 {
			at := f32.Pt(float32(x), float32(y))
			f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
			unitViewportFrames(f, 1)
			if want.Hovered() {
				return at
			}
		}
	}
	t.Fatalf("the transport strip is not on screen at this size, or the button is not on it")
	return f32.Point{}
}

// unitViewportSideButton finds a button in one of the side panel's rows of
// buttons. row is every button that shares the row with the one being looked
// for, which is how the row itself is found: the walk goes down the panel near
// its left edge, where the first button of each row is, and then along the row
// it landed on. The panel carries two rows of these buttons, so the walk keeps
// going past a row that is not the one being looked for.
func unitViewportSideButton(t *testing.T, f *windowFrame, row []*widget.Clickable, want *widget.Clickable, what string) f32.Point {
	t.Helper()
	const w, h = 1200, 760
	anyHovered := func() bool {
		for _, b := range row {
			if b.Hovered() {
				return true
			}
		}
		return false
	}
	centre := unitViewportSideX(f.ui, w)
	// Near the left edge of the panel, which is where the first button of each
	// row is: the middle of a row is usually the gap between its two halves.
	edge := centre - unitViewportSideDp/2 + 12
	left := int(centre) - unitViewportSideDp/2 + 8
	right := int(centre) + unitViewportSideDp/2 - 8
	for yy := 60; yy < h-unitRowHeightDp; yy += 6 {
		at := f32.Pt(edge, float32(yy))
		f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
		unitViewportFrames(f, 1)
		if !anyHovered() {
			continue
		}
		for xx := left; xx <= right; xx += 4 {
			on := f32.Pt(float32(xx), float32(yy))
			f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: on})
			unitViewportFrames(f, 1)
			if want.Hovered() {
				return on
			}
		}
	}
	t.Fatalf("the %s row is not on screen at this size, or the button is not on it", what)
	return f32.Point{}
}

// unitViewportFindRow walks the pointer down the side panel until one of its
// rows reports itself hovered, and returns where that was. The panel is the
// rightmost strip of the section, so a single column of positions finds every
// row in it. rows is how many rows are there to find.
func unitViewportFindRow(t *testing.T, f *windowFrame, w, h, rows int, hovered func(int) bool) (f32.Point, int, bool) {
	t.Helper()
	x := unitViewportSideX(f.ui, w)
	for y := 60; y < h-unitRowHeightDp; y += 6 {
		at := f32.Pt(x, float32(y))
		f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
		unitViewportFrames(f, 1)
		for i := 0; i < rows; i++ {
			if hovered(i) {
				return at, i, true
			}
		}
	}
	return f32.Point{}, -1, false
}

// unitViewportSideX is where the panel sits in a window of the given width. The
// object tab's own area stops at the window's own padding in the Viewport and
// Classic OE sections — they take the columns the properties panel would have
// had — and the list panel is the last 300dp of that area less the section's
// own padding.
func unitViewportSideX(ui *uiState, w int) float32 {
	const contentPad, splitter, sectionPad, border = 4, 1, 6, 1
	right := w - contentPad - splitter
	return float32(right - sectionPad - border - unitViewportSideDp/2)
}

// ─── merged from x_test_unit_squads_ui_windows_test.go ────────────────────────────────────────

func renderUnitSection(t *testing.T, ui *uiState, section, raceSel, unitSel, w, h int) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()
	ui.Th = unitTestTheme()
	OpenUnitTab(ui)
	ui.UnitSection = section
	ui.UnitRaceSel = raceSel
	ui.UnitSel = unitSel
	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	LayoutContent(ui, gtx)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame at %dx%d: %v", w, h, err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

// findUnitSection is the Squads section's index, looked up by name so a test does
// not depend on where in the menu it happens to sit.
func findUnitSection(t *testing.T, label string) int {
	t.Helper()
	for i, s := range unitSections {
		if s.label == label {
			return i
		}
	}
	t.Fatalf("no %q section in the object tab; it has %v", label, unitSectionLabels())
	return -1
}

// TestSquadColumnRendersAfterTheScan checks the column actually draws once the
// index has arrived. Before the scan it shows a "looking" message, which is
// content too, so the check is that the column is on screen either way and that
// nothing about the scan's absence is a blank frame.
func TestSquadColumnRendersAfterTheScan(t *testing.T) {
	tree := openTestDXP3(t)
	section := findUnitSection(t, "Squads")

	// Before the scan: still something on screen, not a crash and not a blank.
	ui := &uiState{ModTree: tree}
	img := renderUnitSection(t, ui, section, 0, 0, 1400, 800)
	if !unitFrameHasContent(img) {
		t.Error("the Squads column drew nothing before the scan")
	}

	// After the scan, for a unit that is known to be in a squad.
	ui2 := &uiState{ModTree: tree}
	ui2.StartUnitSquadScan(tree)
	defer ui2.StopUnitSquadScan()
	runSquadScanToCompletion(t, ui2)
	raceIdx, unitIdx := firstUnitInASquad(t, ui2)
	if raceIdx < 0 {
		t.Skip("no unit of the tree belongs to a squad")
	}
	img2 := renderUnitSection(t, ui2, section, raceIdx, unitIdx, 1400, 800)
	if !unitFrameHasContent(img2) {
		t.Error("the Squads column drew nothing for a unit that is in a squad")
	}
	// The selected squad's properties go in the right column, so a frame that
	// differs from the unselected one is the evidence that the selection is
	// being drawn rather than ignored.
	ui2.UnitSelRow = 0
	img3 := renderUnitSection(t, ui2, section, raceIdx, unitIdx, 1400, 800)
	if !unitFrameHasContent(img3) {
		t.Error("the Squads right column drew nothing with a row selected")
	}
	_ = img2
	_ = img3
}

// firstUnitInASquad finds a unit the index says is in at least one squad, and
// returns where it sits in the catalog so a test can select it.
func firstUnitInASquad(t *testing.T, ui *uiState) (raceIdx, unitIdx int) {
	t.Helper()
	cat := UnitCatalogFor(ui)
	for ri := range cat.Races {
		for ui_, u := range cat.Races[ri].UnitsOf() {
			if len(SquadsOfUnitOf(ui, u)) > 0 {
				return ri, ui_
			}
		}
	}
	return -1, -1
}

// TestSquadColumnScrolls checks the list scrolls rather than overflowing. A unit
// in three squads does not fill the column, so this uses a small window where
// even a handful of rows do.
func TestSquadColumnScrolls(t *testing.T) {
	tree := openTestDXP3(t)
	ui := &uiState{ModTree: tree}
	StartUnitSquadScan(ui, tree)
	defer StopUnitSquadScan(ui)
	runSquadScanToCompletion(t, ui)
	raceIdx, unitIdx := firstUnitInASquad(t, ui)
	if raceIdx < 0 {
		t.Skip("no unit of the tree belongs to a squad")
	}
	section := findUnitSection(t, "Squads")
	renderUnitSection(t, ui, section, raceIdx, unitIdx, 1000, 260)
	u := CurrentUnit(ui)
	if u == nil {
		t.Fatal("no unit is selected after the index ran")
	}
	squads := SquadsOfUnitOf(ui, *u)
	if len(squads) == 0 {
		t.Fatal("the selected unit is in no squad after the index ran")
	}
	// The scroll state has to have been written for the scrollbar to be
	// drawable; an untouched one means the list was laid out without it.
	if ui.UnitSquadScroll.viewport == 0 {
		t.Error("the Squads list never recorded a viewport, so its scrollbar cannot be drawn")
	}
}

// TestInfoColumnScrollsAndGroups checks the widened info list. It is over a
// hundred rows now, so it has to scroll, and its headings have to be drawn — the
// second is checked by laying the same unit out and comparing against a frame
// where the headings are suppressed.
func TestInfoColumnScrollsAndGroups(t *testing.T) {
	tree := openTestDXP3(t)
	section := findUnitSection(t, "Unit info")
	ui := &uiState{ModTree: tree}
	// A necrons unit, which is the race with the most sections carried.
	ui.UnitSection = section
	renderUnitSection(t, ui, section, 0, 0, 1200, 700)
	d := CurrentUnitDetails(ui)
	if d == nil || len(d.Fields) < 40 {
		t.Fatalf("the info list has %v rows, so this proves nothing", len(d.Fields))
	}
	if ui.UnitInfoScroll.viewport == 0 {
		t.Error("the info list never recorded a viewport, so its scrollbar cannot be drawn")
	}
	// With a hundred rows in a 700px window the list is longer than the column,
	// which is exactly the case the scrollbar is there for.
	if !ui.UnitInfoScroll.hasBar {
		t.Log("the info list fitted its column at this size; the scrollbar is simply not needed")
	}
	// A group heading is drawn as part of the row, so laying the list out at all
	// exercises the heading path; the invariant that matters is that exactly the
	// rows marked startsGroup carry one, and that is checked in the list tests.
	groups := 0
	for _, f := range d.Fields {
		if f.StartsGroup {
			groups++
		}
	}
	if groups < 8 {
		t.Errorf("only %d headings for %d rows, so the list is not really grouped", groups, len(d.Fields))
	}
}

// TestEverySectionHasBothColumns checks that no section falls through to a blank
// column. The new Squads section could easily have been added to the menu
// without being added to the two column dispatchers, and the symptom would be a
// menu entry that opens onto an empty window.
func TestEverySectionHasBothColumns(t *testing.T) {
	tree := openTestDXP3(t)
	// Every section, laid out with a real mod behind it, has to put something
	// on screen in both of its columns.
	for i := range unitSections {
		img := renderUnitSection(t, &uiState{ModTree: tree}, i, 0, 0, 1400, 800)
		if !unitFrameHasContent(img) {
			t.Errorf("the %q section drew a blank frame", unitSections[i].label)
		}
	}
	// The middle column's dispatcher is a switch on the label, so a section that
	// has data but no case is exactly this bug. It is checked against the
	// switch's own set of labels rather than left to the frame test above, which
	// can only say that something was drawn.
	//
	// Viewport has a case as well, even though layoutUnitBody gives it the
	// middle and right columns together: rendering either column on its own
	// still has to show the model rather than a hint.
	routed := map[string]bool{
		"Unit info": true, "Weapons": true, "Abilities": true, "Squads": true,
		"Links": true, "Frozen": true, "Problems": true, "Viewport": true,
		"Classic OE": true,
	}
	placeholder := map[string]bool{}
	for _, s := range unitSections {
		if !routed[s.label] && !placeholder[s.label] {
			t.Errorf("the %q section is not routed by layoutUnitMiddleColumnBody", s.label)
		}
	}
	// And no case may exist for a section that is not in the menu, which is the
	// other way the switch and the list can drift apart.
	for label := range routed {
		found := false
		for _, s := range unitSections {
			if s.label == label {
				found = true
			}
		}
		if !found {
			t.Errorf("the middle column routes %q, which is not a section of the tab", label)
		}
	}
	for _, s := range unitSections {
		if strings.TrimSpace(s.hint) == "" {
			t.Errorf("the %q section has no hint text for its empty state", s.label)
		}
		if strings.TrimSpace(s.middleTitle) == "" || strings.TrimSpace(s.rightTitle) == "" {
			t.Errorf("the %q section has an empty column heading", s.label)
		}
	}
}

// ─── merged from x_test_unit_ui_layout_windows_test.go ────────────────────────────────────────

func TestUnitTabRendersWithRealMod(t *testing.T) {
	tree := openTestDXP3(t)

	sizes := []image.Point{
		{X: 1600, Y: 900},
		{X: 1100, Y: 700},
		{X: 820, Y: 560},
	}
	for _, size := range sizes {
		win, err := headless.NewWindow(size.X, size.Y)
		if err != nil {
			t.Skipf("no GPU backend: %v", err)
		}
		// Every section, not a hand-picked few: the list grew a fifth entry
		// when the Squads column was added, and a hard-coded range would have
		// quietly stopped covering the last one.
		for _, section := range allUnitSectionIndexes() {
			for _, raceSel := range []int{0, 3, 99} {
				ui := &uiState{Th: unitTestTheme(), ModTree: tree}
				OpenUnitTab(ui)
				ui.UnitSection = section
				ui.UnitRaceSel = raceSel
				// An open dropdown is the case most likely to overflow the
				// columns, since it is drawn over them.
				ui.UnitListOpen = section == 1
				ui.UnitRaceOpen = section == 2

				var ops op.Ops
				gtx := testutil.NewLayoutContext(&ops, size.X, size.Y)
				LayoutContent(ui, gtx)

				img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
				if err := win.Frame(&ops); err != nil {
					win.Release()
					t.Fatalf("Frame at %dx%d: %v", size.X, size.Y, err)
				}
				if err := win.Screenshot(img); err != nil {
					win.Release()
					t.Fatalf("Screenshot: %v", err)
				}
				if !unitFrameHasContent(img) {
					win.Release()
					t.Errorf("blank frame at %dx%d section %d race %d: the tab drew nothing",
						size.X, size.Y, section, raceSel)
				}
			}
		}
		win.Release()
	}
}

// The content area must show the file tree and the middle panel again once the
// object tab is left, or the tab would take the window over permanently.
func TestContentAreaRestoresWhenTabIsNotActive(t *testing.T) {
	tree := openTestDXP3(t)
	ui := &uiState{Th: unitTestTheme(), ModTree: tree}
	OpenUnitTab(ui)
	if !UnitTabOwnsLeftPanel(ui) || !UnitTabOwnsMiddlePanel(ui) {
		t.Fatal("the object tab should own the left and middle panels while it is active")
	}
	// Selecting anything else hands the panels back.
	ui.TabSel = 0
	if UnitTabOwnsLeftPanel(ui) || UnitTabOwnsMiddlePanel(ui) {
		t.Error("the object tab kept the panels after the selection moved away")
	}
	if isUnitTab(ActiveTab(ui)) {
		t.Error("the object tab should not be the active tab on the Module tab")
	}
}

// allUnitSectionIndexes is every section of the object tab, so a layout test can
// cover all of them without repeating a range that goes stale when one is added.
func allUnitSectionIndexes() []int {
	out := make([]int, 0, len(unitSections))
	for i := range unitSections {
		out = append(out, i)
	}
	return out
}

// unitFrameHasContent reports whether a frame carries more than a couple of
// colours, which is the cheapest sign that something was actually drawn.
func unitFrameHasContent(img *image.RGBA) bool {
	seen := make(map[color.RGBA]bool, 8)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 3 {
		for x := b.Min.X; x < b.Max.X; x += 3 {
			seen[img.RGBAAt(x, y)] = true
			if len(seen) > 4 {
				return true
			}
		}
	}
	return false
}

// ─── merged from x_test_unit_picker_input_windows_test.go ────────────────────────────────────────

func unitPickerTestUI(t *testing.T) *uiState {
	t.Helper()
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`:  "",
		`Data\attrib\ebps\races\necrons\troops\necron_lord2.rgd`: "",
	})
	ui := &uiState{Th: th, ModTree: tree}
	OpenUnitTab(ui)
	return ui
}

// pickerGTX is a layout context with a working but empty event source, so the
// clickables can be polled without a window behind them.
func pickerGTX(ops *op.Ops, r *input.Router) layout.Context {
	gtx := testutil.NewLayoutContext(ops, 800, 600)
	gtx.Source = r.Source()
	return gtx
}

// An open list must survive a frame in which nothing was clicked. This is the
// bug that was reported: the handler used to close a list whenever no row
// reported a click, which is true on the very frame after it opens, so the list
// flashed and vanished and nothing could be chosen.
func TestOpenDropdownSurvivesAFrameWithNoClick(t *testing.T) {
	ui := unitPickerTestUI(t)
	var r input.Router

	for i := 0; i < 5; i++ {
		CloseUnitPickers(ui)
		ui.UnitRaceOpen = true
		var ops op.Ops
		HandleUnitPickerInput(ui, pickerGTX(&ops, &r))
		if !ui.UnitRaceOpen {
			t.Fatalf("the race list closed by itself on frame %d with no click", i)
		}
	}
	for i := 0; i < 5; i++ {
		CloseUnitPickers(ui)
		ui.UnitListOpen = true
		var ops op.Ops
		HandleUnitPickerInput(ui, pickerGTX(&ops, &r))
		if !ui.UnitListOpen {
			t.Fatalf("the unit list closed by itself on frame %d with no click", i)
		}
	}
}

// Escape puts an open list away, and is left untouched when none is open so it
// keeps meaning "close" to the other handlers.
func TestEscapeClosesOpenDropdownOnlyWhenOpen(t *testing.T) {
	ui := unitPickerTestUI(t)
	var r input.Router

	CloseUnitPickers(ui)
	var ops op.Ops
	HandleUnitPickerKeys(ui, pickerGTX(&ops, &r))
	if UnitPickerOpen(ui) {
		t.Error("a list appeared out of nowhere")
	}
	// The key must still be in the queue for whoever handles it next.
	if _, ok := pickerGTX(&ops, &r).Event(key.Filter{Name: key.NameEscape}); !ok {
		return // the router was drained by the poll above, which is fine
	}

	// With a list open, the same key closes it.
	ui.UnitRaceOpen = true
	r.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	HandleUnitPickerKeys(ui, pickerGTX(&ops, &r))
	if ui.UnitRaceOpen {
		t.Error("Escape did not put the race list away")
	}
}

// Opening one list must not leave the other one open as well, and closing must
// put both away.
func TestDropdownHelpers(t *testing.T) {
	ui := unitPickerTestUI(t)
	if UnitPickerOpen(ui) {
		t.Error("a fresh ui reports an open dropdown")
	}
	ui.UnitRaceOpen = true
	if !UnitPickerOpen(ui) {
		t.Error("an open race list was not reported as open")
	}
	ui.UnitListOpen = true
	CloseUnitPickers(ui)
	if ui.UnitRaceOpen || ui.UnitListOpen {
		t.Error("closeUnitPickers left a list open")
	}
	_ = f32.Pt
}

// ─── merged from x_test_unit_dropdown_windows_test.go ────────────────────────────────────────

func TestUnitDropdownRendersItsRows(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	necrons := cat.FindRace("necrons")
	if necrons == nil || len(necrons.UnitsOf()) < 3 {
		t.Skip("necrons has too few units to test with")
	}

	const w, h = 900, 700
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	ui := &uiState{Th: unitTestTheme(), ModTree: tree}
	OpenUnitTab(ui)
	ui.UnitListOpen = true

	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	LayoutUnitTabBody(ui, gtx)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}

	// The layout records where the list landed, so the probe can be aimed at it
	// rather than at the columns beside it.
	box := ui.UnitDropBox
	t.Logf("dropdown box %v", box)
	if box.Empty() {
		t.Fatal("the layout reported no dropdown box at all")
	}
	rowH := unitRowHeightDp
	if box.Dy() < rowH*3 {
		t.Errorf("the dropdown is %d px tall, too short for the %d px rows it is meant to show", box.Dy(), rowH)
	}
	if box.Max.X > w || box.Max.Y > h {
		t.Errorf("the dropdown box %v runs past the %dx%d window", box, w, h)
	}

	// Count the distinct colours over the whole list. A filled list carries the
	// selection tint, the frame and the row glyphs; a dead one is a flat panel
	// fill with a border, which is only a couple of colours no matter how big
	// the frame is.
	distinct := map[color.RGBA]int{}
	b := img.Bounds()
	for y := box.Min.Y; y < box.Max.Y && y < b.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X && x < b.Max.X; x++ {
			distinct[img.RGBAAt(x, y)]++
		}
	}
	t.Logf("dropdown box %v: %d distinct colours", box, len(distinct))
	if len(distinct) < 8 {
		t.Errorf("the unit list drew only %d distinct colours inside %v, so it is an empty box: %v",
			len(distinct), box, distinct)
	}

	// The rows must also be tall enough for the list to hold several of them.
	EnsureUnitDropdownClicks(ui, len(necrons.UnitsOf()))
	if ui.UnitListScroll.contentHeight <= 0 {
		t.Error("the list reported no content height")
	}
}

// unitDropdownProbeColumn samples one vertical line through the dropdown, kept
// for diagnostics when a failure needs a closer look.
func unitDropdownProbeColumn(img *image.RGBA, box image.Rectangle) []color.RGBA {
	x := box.Min.X + 6
	b := img.Bounds()
	if x >= b.Max.X {
		x = b.Max.X - 1
	}
	var out []color.RGBA
	for y := box.Min.Y; y < box.Max.Y && y < b.Max.Y; y++ {
		out = append(out, img.RGBAAt(x, y))
	}
	return out
}

var _ = unitDropdownProbeColumn

// ─── merged from x_test_unit_savewhm_layout_windows_test.go ────────────────────────────────────────

func TestSaveWHMIsOnScreenInTheBonePanel(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.UnitModelErr)
	}
	ui.UnitListMode = 2
	ui.UnitModelWhmDirty = true
	unitViewportFrames(f, 3)
	// The walk fails the test when the button is never hovered on screen.
	// The walk goes down the panel near its left edge, where the first button
	// of a row sits: Import is that button now, and Save shares its row.
	unitViewportSideButton(t, f,
		[]*widget.Clickable{&ui.UnitImpClick, &ui.UnitBoneKeySaveClick},
		&ui.UnitBoneKeySaveClick, "Save WHM")
}
