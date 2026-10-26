package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/widget"
)

// The object window's Viewport section, against the real game. A unit's model
// is read and parsed on a worker, so these tests drive the pollers the way the
// app's own loop does, and they set the clock the preview advances playback
// from, which a window normally does and a bare frame does not.

// unitViewportUI opens the object window on a named unit of the real game and
// returns the state and the frame that draws it.
func unitViewportUI(t *testing.T, unitName string, w, h int) (*uiState, *windowFrame) {
	t.Helper()
	ui := newUI(unitTestTheme())
	ui.modTree = openTestDXP3(t)
	ui.openUnitTab()
	raceSel, unitSel := unitViewportPicker(t, ui, unitName)
	ui.unitRaceSel, ui.unitSel = raceSel, unitSel
	ui.unitSection = findUnitSection(t, "Viewport")
	f := newWindowFrame(ui, w, h)
	return ui, f
}

// unitViewportPicker finds the race and unit indices of a named unit, so a test
// lands on it without counting rows of the dropdown.
func unitViewportPicker(t *testing.T, ui *uiState, name string) (raceSel, unitSel int) {
	t.Helper()
	cat := ui.unitCatalogFor()
	if cat == nil {
		t.Skip("no unit catalog")
	}
	for ri, race := range cat.races {
		for idx, u := range race.unitsOf() {
			if strings.EqualFold(u.name, name) {
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
		ui.pollUnitModel()
		ui.pollModelJobs()
		if ui.unitModelLoaded() {
			// A couple of frames so the panel that lists the cameras and the
			// clips is laid out with the model on it.
			if !quiet {
				unitViewportFrames(f, 3)
			}
			return true
		}
		if ui.unitModelErr != "" {
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
		f.ui.pollUnitModel()
		f.ui.pollModelJobs()
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
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	if !strings.HasSuffix(strings.ToLower(ui.unitModelPath), ".whm") {
		t.Errorf("the viewport is showing %q, which is not a model", ui.unitModelPath)
	}
	anims := ui.unitModelAnimNames()
	if len(anims) < 2 {
		t.Fatalf("the model carries %d clips (%v), so there is nothing to play", len(anims), anims)
	}
	if len(ui.unitAnimClicks) != len(anims) {
		t.Errorf("%d clip rows were laid out for %d clips", len(ui.unitAnimClicks), len(anims))
	}
	v := ui.unitModelViewer()
	if v.anim == nil {
		t.Fatal("no clip is selected")
	}
	if v.animIndex < 0 || v.animIndex >= len(v.model.Animations) {
		t.Fatalf("the selected clip is index %d of %d", v.animIndex, len(v.model.Animations))
	}
	if !v.playing {
		t.Error("the viewport opened on a still model rather than playing a clip")
	}
	if unitModelClipDuration(v) <= 0 {
		t.Errorf("the clip on show, %q, has no length to play", v.anim.Name)
	}
	// Something has to have asked for a frame, or the canvas would stay empty
	// whatever state the camera is in. The software rasteriser answers on a
	// worker, so the wait is for the frame rather than for the request.
	for i := 0; i < 200 && v.frame == nil && v.gpuFrame == nil && !v.redraw; i++ {
		unitViewportFrames(f, 1)
	}
	if v.frame == nil && v.gpuFrame == nil && !v.redraw {
		t.Error("nothing has asked for a frame, so the canvas would stay empty")
	}
}

// Playback actually advances: the clip is handed a later point of its length
// once frames have gone by, which is what "with animations" has to mean.
func TestTheViewportPlaysTheClip(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	v := ui.unitModelViewer()
	first := v.time
	deadline := time.Now().Add(30 * time.Second)
	for v.time == first && time.Now().Before(deadline) {
		sleepMillis()
		unitViewportFrames(f, 1)
	}
	if v.time == first {
		t.Fatalf("the clip stayed at %v over a second of frames", first)
	}
	if v.time < 0 || v.time > 1 {
		t.Errorf("the clip is at %v, outside the 0..1 the renderer expects", v.time)
	}
}

// The clip on show has to move the skeleton, or the preview is a still of a
// model that claims to be animated. The necron warrior is the shipped model
// whose clips do; its five megabytes are never rendered, since no frame is run
// once the model is in.
func TestTheClipOnShowMovesTheSkeleton(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_basic_warrior", 1200, 760)
	if !unitViewportLoad(t, ui, f, true) {
		t.Fatalf("the model of the basic warrior never loaded: %q", ui.unitModelErr)
	}
	v := ui.unitModelViewer()
	if v.anim == nil {
		t.Fatal("no clip is selected")
	}
	// The default clip of a unit is the one it moves with, rather than a
	// "vis_" clip that only fades a weapon in and out.
	if strings.HasPrefix(strings.ToLower(v.anim.Name), "vis_") {
		t.Errorf("the viewport opened on %q, which moves nothing", v.anim.Name)
	}
	rest := v.model.RestPose()
	moved := 0
	for _, at := range []float32{0.25, 0.5, 0.75} {
		pose := v.model.PoseAt(v.anim, at)
		for b := range rest.Bones {
			if rest.Bones[b] != pose.Bones[b] {
				moved++
				break
			}
		}
	}
	if moved == 0 {
		t.Errorf("the clip on show, %q, leaves every bone where it was", v.anim.Name)
	}
}

// The clip list is the whole set of things a unit's model can be seen doing, so
// every clip is listed and picking one plays it.
func TestTheViewportListsEveryClipAndPlaysThePickedOne(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	v := ui.unitModelViewer()
	anims := ui.unitModelAnimNames()
	if len(ui.unitAnimClicks) != len(anims) {
		t.Fatalf("%d clip rows were laid out for %d clips", len(ui.unitAnimClicks), len(anims))
	}
	// Still it, so the frames of the search below are not spent on a software
	// render of a playing clip.
	v.playing = false
	start := v.animIndex
	// The clip on show is skipped: finding it again would prove only that the
	// first row of the list is there, which it is whether the rest are or not.
	at, idx, ok := unitViewportFindRow(t, f, 1200, 760, len(ui.unitAnimClicks), func(i int) bool {
		return i != start && ui.unitAnimClicks[i].Hovered()
	})
	if !ok {
		t.Skip("no other clip row is on screen at this size")
	}
	clickAt(f, at)
	if idx == start {
		t.Errorf("the only clip row on screen is the one already selected (%q), so the list is not being drawn", anims[idx])
	}
	if v.anim == nil || v.anim.Name != anims[idx] {
		t.Errorf("the selected clip is %v, want %q", v.anim, anims[idx])
	}
	// Picking a clip of a still model is a request to watch it.
	if !v.playing {
		t.Error("picking a clip left the model still, so nothing would be seen to happen")
	}
}

// A unit that names no model is a state the section has to draw, not a crash:
// the canvas says so and the panel says there is nothing loaded.
func TestTheViewportSaysWhenAUnitHasNoModel(t *testing.T) {
	ui := newUI(unitTestTheme())
	ui.modTree = unitViewportMod(t)
	unitViewportWriteUnit(t, ui.modTree, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, "")
	ui.openUnitTab()
	raceSel, unitSel := unitViewportPicker(t, ui, "necron_wraith")
	ui.unitRaceSel, ui.unitSel = raceSel, unitSel
	ui.unitSection = findUnitSection(t, "Viewport")
	f := newWindowFrame(ui, 1200, 760)
	// A unit with no model is reported rather than waited on: the answer is
	// the absence of a file, and the section has to say so rather than sit on
	// a "loading" message for ever.
	deadline := time.Now().Add(30 * time.Second)
	for ui.unitModelErr == "" && time.Now().Before(deadline) {
		ui.pollUnitModel()
		sleepMillis()
	}
	unitViewportFrames(f, 2)
	if ui.unitModelErr == "" {
		t.Fatal("a unit that names no model neither loaded one nor said so")
	}
	if ui.unitModelLoaded() {
		t.Error("a model appeared for a unit that has none")
	}
	if !strings.Contains(ui.unitModelErr, "no model file") {
		t.Errorf("the reason given is %q", ui.unitModelErr)
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
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	// The frame the canvas paints comes back from a worker, so the wait is for
	// the picture rather than for the request for it.
	v := ui.unitModelViewer()
	for i := 0; i < 200 && v.frame == nil && v.gpuFrame == nil; i++ {
		unitViewportFrames(f, 1)
	}
	if v.frame == nil && v.gpuFrame == nil {
		t.Fatal("the viewer never produced a frame, so there is nothing to paint")
	}
	// Held still, so the render under test is the one that was waited for.
	v.playing = false
	v.redraw = false
	const w, h = 1200, 760
	img := renderUnitSection(t, ui, ui.unitSection, ui.unitRaceSel, ui.unitSel, w, h)
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
	return image.Rect(left, bottom-v.renderH, left+v.renderW, bottom)
}

// The wheel over the canvas moves the camera in and out of the model. It is
// the one way to look at a detail of a building without opening the model in a
// tab of its own, and the canvas is the only thing under the pointer here.
func TestTheWheelOverTheCanvasZoomsInAndOut(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, true) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	v := ui.unitModelViewer()
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
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	v := ui.unitModelViewer()
	if v.anim == nil || v.anim.NumFrames < 4 {
		t.Skip("this model has no clip with enough frames to step through")
	}
	tab := ui.unitModel
	// From the first frame, so that a step is a step and not a step from
	// wherever the clip happened to be when the test got to it.
	v.playing = false
	v.time = 0
	// The strip is drawn in the panel beside the canvas, not under it, so the
	// buttons are found by a walk across the panel.
	fwd := unitViewportButton(t, f, tab, &tab.modelStepFwd)
	clickAt(f, fwd)
	if v.playing {
		t.Error("winding the clip left the model playing, so the frame would move on")
	}
	frame, frames := v.currentFrame()
	if frames != v.anim.NumFrames || frame != 1 {
		t.Fatalf("one step forward is on frame %d of %d, want frame 2", frame+1, frames)
	}
	unitViewportFrames(f, 2)
	back := unitViewportButton(t, f, tab, &tab.modelStepBack)
	clickAt(f, back)
	if again, _ := v.currentFrame(); again != 0 {
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
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	anims := ui.unitModelAnimNames()
	if len(anims) < 6 {
		t.Skipf("this model carries %d clips, too few to fill the panel", len(anims))
	}
	// The panel is laid out with the model on it, which is what gives the
	// scroll view its content to be taller than.
	unitViewportFrames(f, 3)
	state := &ui.unitViewportScroll
	if !state.hasBar {
		t.Skipf("the panel fits its %d clips without a scrollbar", len(anims))
	}
	// The wheel has to land on the list itself: the panel's fixed controls
	// above it have no scroll of their own, and a row of them is where the
	// middle of the window happens to be.
	at, _, ok := unitViewportFindRow(t, f, w, h, len(anims), func(i int) bool { return ui.unitAnimClicks[i].Hovered() })
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
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	v := ui.unitModelViewer()
	tab := ui.unitModel
	row := []*widget.Clickable{&tab.modelZoomIn, &tab.modelZoomOut, &tab.modelReset}

	// The three buttons are found once, then pressed in turn: each press is a
	// step, and two steps in is nowhere near the factor of ten the wheel used
	// to ask for.
	v.resetCamera()
	fit := v.Distance
	near := unitViewportSideButton(t, f, row, &tab.modelZoomIn, "toggles")
	far := unitViewportSideButton(t, f, row, &tab.modelZoomOut, "toggles")
	reset := unitViewportSideButton(t, f, row, &tab.modelReset, "toggles")

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
