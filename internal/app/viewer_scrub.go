package app

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// The scrub bar is a clickable strip of the clip on show, laid over the bottom
// of the viewport: every frame has a place on it, the frame on screen is
// marked, and the frames the .whe fires something on are ticked. Clicking a
// frame winds the playhead to it, which is the thing a reader wants when a
// clip is four seconds long and the thing that fires a sound is at second two.
//
// The numbers alternate above and below the bar. Consecutive frames are a few
// pixels apart on a long clip, so a number under every tick would be a smear of
// overlapping text; putting every other one on the other side of the bar gives
// each of them twice the room, which is what makes the whole clip readable
// without thinning the numbers out.

// scrubBarThickness is the bar's own height. It is thin on purpose — it is a
// ruler laid over the model, not a control the reader has to aim at, since a
// click anywhere in the strip is a seek.
const scrubBarThickness = 5.0

// scrubBarMargin keeps the strip off the frame's edge, matching the gizmo's.
const scrubBarMargin = 10.0

// scrubLabelPx is roughly the width of a two or three digit frame number. It
// decides how many numbers fit, and so how often one is drawn.
const scrubLabelPx = 26.0

// scrubLabelHeight is the box a number is laid out in, above or below.
const scrubLabelHeight = 13.0

// The two event marks, kept apart because they mean different things: one is
// something seen, the other something heard.
var (
	cEventFX  = rgb(0x7BD88F)
	cEventSnd = rgb(0x6FB7E8)
	// The clip's own keyframes — where any track of the animation holds a
	// key — are amber, the same warm colour as a picked bone, because both
	// answer "which spot is this" about the same skeleton.
	cKeyFrame = rgb(0xE8A33D)
	// The picked bone's own keyframes are rose, so they read as the key
	// frames the bone parameters panel is editing rather than as the clip's.
	cBoneKey = rgb(0xE27B9D)
)

// animScrubEvent is one moment in a clip that something fires on.
type animScrubEvent struct {
	// Frame is the frame the event lands on, counted from zero.
	Frame int
	Name  string
	// Sound says whether it is audio rather than something visible.
	Sound bool
	// Time is the normalized point in the clip, before it was turned into a
	// frame. It is carried so the same value can be re-read against a
	// different frame count without going back to the .whe.
	Time float64
}

// fxClipEvents inverts the fires index: clip name to the events that clip fires
// and when, in the timeline's own seconds. fxTimelineFires is keyed by event
// and normalizes against a clip's length, which is the right way round for
// building the effects; the scrub bar wants the file's own number, because a
// frame is only computable once the clip on show says how long it is.
func fxClipEvents(timelines []whe.FxTimeline) map[string][]animScrubEvent {
	out := map[string][]animScrubEvent{}
	for _, tl := range timelines {
		seen := map[string]bool{}
		for _, anim := range append([]string{tl.Name}, tl.Animations...) {
			key := strings.ToLower(anim)
			if anim == "" || seen[key] {
				continue
			}
			seen[key] = true
			for _, ev := range tl.Events {
				out[key] = append(out[key], animScrubEvent{Name: ev.Name, Time: ev.Time})
			}
		}
	}
	return out
}

// frameAt is the frame a timeline time falls on. The playhead is read the other
// way round by currentFrame, which rounds, so this rounds too: the tick under a
// number and the frame that number seeks to are then the same one.
func frameAt(at float64, numFrames int) int {
	if numFrames < 1 {
		return 0
	}
	if at < 0 {
		return 0 // on entering the clip
	}
	if math.IsInf(at, 1) || at > 1.000001 {
		return numFrames - 1 // on leaving it
	}
	f := int(math.Round(at * float64(numFrames)))
	return minInt(maxInt(f, 0), numFrames-1)
}

// clipScrubEvents is the marked frames of one clip: every moment the .whe fires
// something, in order, each tagged with what fires there.
//
// An event the clip fires twice is kept twice: a clip that rings a footstep at
// a quarter and again at three quarters has two frames worth stopping at, and
// collapsing them to the first would hide the second.
func (v *modelViewer) clipScrubEvents(anim string) []animScrubEvent {
	if v == nil || v.anim == nil || v.anim.NumFrames < 1 || anim == "" {
		return nil
	}
	list := fxClipEvents(v.fxTimelines)[strings.ToLower(anim)]
	if len(list) == 0 {
		return nil
	}
	// Which of them are audio: the player's own event set is the only place
	// that knows, and it was built from the same .whe.
	out := make([]animScrubEvent, 0, len(list))
	for _, e := range list {
		at := whe.TimeNormalized(e.Time, animClipSeconds(v.anim))
		out = append(out, animScrubEvent{
			Frame: frameAt(at, v.anim.NumFrames),
			Name:  e.Name,
			Sound: v.fxSound != nil && v.fxSound.IsSoundEvent(e.Name),
			Time:  at,
		})
	}
	return out
}

// scrubHit reports whether a pointer position is inside the strip. It is
// written out rather than taken from image.Rectangle so the two callers — the
// press and the drag — cannot disagree about what counts as a hit.
func scrubHit(r image.Rectangle, p f32.Point) bool {
	if r.Empty() {
		return false
	}
	x, y := int(p.X), int(p.Y)
	return x >= r.Min.X && x < r.Max.X && y >= r.Min.Y && y < r.Max.Y
}

// scrubGeometry is the strip's left margin and drawable width for the canvas
// size the viewer last rendered at. scrubSeek, the key drag and the overlay
// all work from the same two numbers, so a press between the bar and the
// numbers lands on the frame the pointer is over rather than a frame beside
// it.
func (v *modelViewer) scrubGeometry() (left, width float32, ok bool) {
	if v == nil || v.anim == nil || v.anim.NumFrames < 1 {
		return 0, 0, false
	}
	w := float32(v.renderW)
	if w <= 0 {
		return 0, 0, false
	}
	left = float32(scrubBarMargin)
	width = w - 2*left
	if width <= 0 {
		return 0, 0, false
	}
	return left, width, true
}

// movePickedBoneKeyToX moves the picked bone key to the frame under x on the
// strip, over the same geometry scrubSeek seeks by, so the key lands on the
// frame the cursor is on while it is dragged.
func (v *modelViewer) movePickedBoneKeyToX(x float32) bool {
	if v == nil || v.anim == nil || v.anim.NumFrames < 1 {
		return false
	}
	left, width, ok := v.scrubGeometry()
	if !ok {
		return false
	}
	return v.movePickedBoneKeyToFrame(scrubBarAt(x, left, width, v.anim.NumFrames))
}

// scrubSeek is the press or drag on the strip: the x is turned into a frame of
// the clip and wound to. The bool reports whether the press landed on one of
// the picked bone's keymarks — the moment a drag stops being a scrub and
// starts carrying the key with it.
//
// snapKeys says whether the keyframe marks are on show: snapping to a mark
// that is not drawn would put the playhead on a key the reader cannot see, so
// the caller passes false whenever the strip is drawing without them. The
// marks themselves decide which keys are offered — a picked bone hides the
// clip's own marks, so only its keys snap while they are what is drawn.
func (v *modelViewer) scrubSeek(x float32, snapKeys bool) bool {
	if v == nil || v.anim == nil || v.anim.NumFrames < 1 {
		return false
	}
	left, width, ok := v.scrubGeometry()
	if !ok {
		return false
	}
	// The frame the click lands on, then the keyframe it may snap to: a
	// click that lands on a keyframe mark winds to the keyframe rather than
	// to the frame it happened to hit, because selecting a keyframe is the
	// whole point of marking them and a mark a pixel off would otherwise
	// leave the playhead beside the key instead of on it.
	frame := scrubBarAt(x, left, width, v.anim.NumFrames)
	if !snapKeys {
		v.seekFrame(frame)
		return false
	}
	// With a bone picked that bone's keys are what the strip marks, and a
	// press on one of them both selects it and says the drag that may follow
	// is a key drag.
	if v.boneTrack() != nil {
		if keys := v.boneKeyFrames(); len(keys) > 0 {
			if f, ok := snapKeyFrame(x, keys, left, width, v.anim.NumFrames); ok {
				frame = f
				if idx, mode, ok := v.boneKeyFrame(frame); ok {
					v.boneKeyIndex = idx
					v.boneKeyMode = mode
					v.invalidate()
					v.seekFrame(frame)
					return true
				}
			}
		}
		v.seekFrame(frame)
		return false
	}
	// No bone is picked, so the strip marks the clip's own keyframes — the
	// moments worth stopping at, wherever they come from.
	if keys := clipKeyFrames(v.anim); len(keys) > 0 {
		if f, ok := snapKeyFrame(x, keys, left, width, v.anim.NumFrames); ok {
			frame = f
		}
	}
	v.seekFrame(frame)
	return false
}

// seekFrame puts the playhead on a frame of the clip on show and holds it there,
// the way the step buttons do: a click lands on a frame to be looked at, not
// on a frame to be watched from, so playback stops until play is pressed again.
func (v *modelViewer) seekFrame(frame int) {
	if v == nil || v.anim == nil || v.anim.NumFrames < 1 {
		return
	}
	frame = minInt(maxInt(frame, 0), v.anim.NumFrames-1)
	v.playing = false
	// The clock is zeroed for the same reason the step buttons zero it: pressing
	// play afterwards should start from the frame that was clicked rather than
	// jump a whole second past it.
	v.lastFrame = time.Time{}
	// The playhead is the middle of the frame, which is where a step leaves it.
	v.time = float32((float64(frame) + 0.5) / float64(v.anim.NumFrames))
	if v.time >= 1 {
		v.time = 0
	}
	v.invalidate()
	log.Printf("3D preview: seek to frame %d of %s (%d frames)",
		frame+1, v.anim.Name, v.anim.NumFrames)
}

// scrubBarAt is the frame a click at x lands on. The bar is split into equal
// slices, one per frame, and the click takes the slice it falls in, so a click
// anywhere near a frame finds it.
func scrubBarAt(x, left, width float32, numFrames int) int {
	if numFrames < 1 || width <= 0 {
		return -1
	}
	rel := float64(x-left) / float64(width)
	if rel < 0 {
		rel = 0
	}
	if rel > 1 {
		rel = 1
	}
	// Rounded, not truncated: a frame's own place on the bar is f/n of the
	// width, and the division that gets a click back there lands a hair
	// under it often enough that truncating put every tick a frame early -
	// which reads as the strip seeking to the frame before the one that was
	// clicked.
	f := int(math.Round(rel * float64(numFrames)))
	return minInt(maxInt(f, 0), numFrames-1)
}

// frameX is where a frame sits on the bar: frames are spread evenly and the
// first is at the left edge, so the strip reads as the clip does.
func frameX(frame, numFrames int, width float32) float32 {
	if numFrames < 1 {
		return 0
	}
	return float32(frame) / float32(numFrames) * width
}

// clipKeyFrames is every frame of the clip that carries a key: bone position
// and rotation keys, mesh visibility keys and texture keys alike, since all
// three are keyframes of the same clip and a reader stepping the strip wants
// to stop at each of them. The answer is sorted and deduplicated — most keys
// land on frames another track also keys, and an unsorted strip would mark
// the same frame out of order.
func clipKeyFrames(anim *WhmAnimation) []int {
	if anim == nil || anim.NumFrames < 1 {
		return nil
	}
	seen := make(map[int]bool)
	add := func(t float32) {
		seen[frameAt(float64(t), anim.NumFrames)] = true
	}
	for k := range anim.Bones {
		for _, key := range anim.Bones[k].PosKey {
			add(key.Time)
		}
		for _, key := range anim.Bones[k].RotKey {
			add(key.Time)
		}
	}
	for k := range anim.Meshes {
		for _, key := range anim.Meshes[k].Keys {
			add(key.Time)
		}
	}
	for k := range anim.Textures {
		for _, key := range anim.Textures[k].Keys {
			add(key.Time)
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]int, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Ints(out)
	return out
}

// scrubKeySnapPx is how near a keyframe mark a click has to land to wind the
// playhead onto the keyframe rather than the frame under it. It is a few
// pixels: close enough that aiming at a mark finds it, far enough that a
// click between marks still lands on the frame it looked like.
const scrubKeySnapPx = 6

// snapKeyFrame is the keyframe a click at x should wind to, when one is close
// enough on the bar, and whether there is one at all.
func snapKeyFrame(x float32, keys []int, left, width float32, numFrames int) (int, bool) {
	best, bestDist := -1, float32(scrubKeySnapPx)
	for _, f := range keys {
		d := absF32(left + frameX(f, numFrames, width) - x)
		if d <= bestDist {
			best, bestDist = f, d
		}
	}
	return best, best >= 0
}

// absF32 is the absolute value of a float32, which the standard library only
// offers in float64.
func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// modelAnimScrubOverlay draws the frame strip over the bottom of the canvas and
// records where it is, so the canvas's own press handler can tell a seek from
// the start of a camera drag.
func (ui *uiState) modelAnimScrubOverlay(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	if v == nil || v.model == nil || v.anim == nil || v.anim.NumFrames < 1 {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	n := v.anim.NumFrames
	left := float32(scrubBarMargin)
	width := float32(w) - 2*left
	// Room for the bar and a number on each side of it.
	if width < 60 || h < int(scrubBarThickness)+2*int(scrubLabelHeight)+4*int(scrubBarMargin) {
		v.scrubRect = image.Rectangle{}
		return layout.Dimensions{}
	}
	barY := h - scrubBarMargin - scrubLabelHeight

	// The strip is remembered so a press inside it seeks. The hit area is taller
	// than the bar: a five pixel line is a poor target.
	hit := int(scrubLabelHeight)
	v.scrubRect = image.Rect(
		int(left)-hit, int(barY)-hit,
		int(left+width)+hit, int(barY)+hit,
	)

	// Everything is drawn relative to the left edge of the bar, so a frame's
	// place on it is the only number that has to be worked out.
	at := op.Offset(image.Point{X: int(left), Y: int(barY)}).Push(gtx.Ops)
	origin := gtx

	scrubFill(origin, 0, -scrubBarThickness/2, width, scrubBarThickness, cSaveBd)
	played := float32(v.time) * width
	if played > 0 {
		scrubFill(origin, 0, -scrubBarThickness/2, played, scrubBarThickness, cSelectBd)
	}

	// A tick every frame, thinned to what the width can carry.
	step := int(math.Max(1, math.Floor(float64(n)/float64(width/scrubLabelPx))))
	for f := 0; f < n; f += step {
		x := frameX(f, n, width)
		scrubFill(origin, x-0.5, -scrubBarThickness, 1, scrubBarThickness*2, cSaveBd)
	}

	// The frames something fires on, marked above the bar so they read as
	// belonging to a frame rather than to the bar itself.
	for _, e := range v.clipScrubEvents(v.anim.Name) {
		x := frameX(e.Frame, n, width)
		col := cEventFX
		if e.Sound {
			col = cEventSnd
		}
		scrubFill(origin, x-1.5, -scrubBarThickness-7, 3, 6, col)
	}

	// The keyframe marks are only on show with the bone list: on the
	// animation and marker lists they name keys that no list there selects,
	// so they are drawn off and the strip reads as frames alone.
	showKeys := t != ui.unitModel || ui.unitListMode == 2

	// The clip's own keyframes, drawn through the bar in amber: taller
	// than the frame ticks, so they read as the places worth stopping at
	// rather than as every frame counted out. A clip that keys nearly
	// every frame would draw a solid amber stripe, so marks closer than a
	// few pixels to the last one are thinned the way the numbers are.
	//
	// While a bone with a track in the clip is picked, this mark is off:
	// its amber covers every other bone's keys and the mesh and texture
	// tracks, and the rose marks below are the picked bone's alone. One
	// list of marks is what the panel below talks about, so the rest are
	// taken away rather than shown beside it.
	if showKeys && v.boneTrack() == nil {
		lastKeyX := float32(-1e9)
		for _, f := range clipKeyFrames(v.anim) {
			x := frameX(f, n, width)
			if x-lastKeyX < 4 {
				continue
			}
			lastKeyX = x
			scrubFill(origin, x-1, -scrubBarThickness-3, 2, scrubBarThickness*2+6, cKeyFrame)
		}
	}

	// The picked bone's own keyframes, drawn through the bar in rose. They
	// sit on top of the clip's amber marks wherever they share a frame, and
	// they are what the bone parameters panel's keyframe rows are talking about.
	if showKeys && v.boneIndex >= 0 && v.boneIndex < len(v.model.Bones) {
		lastBoneKeyX := float32(-1e9)
		for _, f := range v.boneKeyFrames() {
			x := frameX(f, n, width)
			if x-lastBoneKeyX < 4 {
				continue
			}
			lastBoneKeyX = x
			scrubFill(origin, x-1, -scrubBarThickness-4, 2, scrubBarThickness*2+8, cBoneKey)
		}
		// The picked bone key is drawn over its rose mark so it reads as the
		// one the panel's "Set ..." buttons refer to.
		if v.boneKeyIndex >= 0 {
			if f, ok := v.boneKeyFrameAt(); ok {
				x := frameX(f, n, width)
				scrubFill(origin, x-2, -scrubBarThickness-5, 4, scrubBarThickness*2+10, cText)
			}
		}
	}

	// The frame on screen.
	cur := int(math.Round(float64(v.time) * float64(n)))
	cur = minInt(maxInt(cur, 0), n-1)
	scrubFill(origin, frameX(cur, n, width)-1, -scrubBarThickness-2, 2,
		scrubBarThickness*2+4, rgb(0xFFFFFF))

	// The numbers, alternating above and below the bar: the first above, the
	// second below, the third above, and so on, which is what gives every
	// number twice the horizontal room its neighbour would otherwise take.
	labels := 0
	above := true
	for f := 0; f < n && labels < 96; f += step {
		x := frameX(f, n, width)
		if x < scrubLabelPx/2 || x > width-scrubLabelPx/2 {
			continue
		}
		y := -scrubBarThickness - 2 - scrubLabelHeight
		if !above {
			y = scrubBarThickness + 2
		}
		g := origin
		macro := op.Offset(image.Point{X: int(x) - int(scrubLabelPx)/2, Y: int(y)}).Push(g.Ops)
		g.Constraints.Min = image.Point{}
		g.Constraints.Max = image.Point{X: int(scrubLabelPx), Y: int(scrubLabelHeight)}
		l := material.Label(ui.th, unit.Sp(9), itoaPlain(f+1))
		l.Color = cTextDim
		l.MaxLines = 1
		l.Layout(g)
		macro.Pop()
		labels++
		above = !above
	}

	at.Pop()
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// scrubFill paints one rectangle at an offset from the bar's own origin.
func scrubFill(gtx layout.Context, x, y, w, h float32, col color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	macro := op.Offset(image.Point{X: int(x), Y: int(y)}).Push(gtx.Ops)
	stack := clip.Rect{Max: image.Pt(int(w), int(h))}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
	macro.Pop()
}

// ─── merged from viewer_canvas.go ───────────────────────────────────────────────

// modelCanvasWithFxList stacks readouts and diagnostics over the rendered frame.
func (ui *uiState) modelCanvasWithFxList(gtx layout.Context, t *fileTab) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelCanvas(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelFxAttributeDialog(gtx, t.model) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelAnimScrubOverlay(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelFxAxesOverlay(gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ui.modelGizmoOverlay(gtx, t) }),
	)
}

// modelCanvas paints the last rendered frame and handles orbiting.
func sgbCameraDragRequested(modifiers key.Modifiers) bool {
	return modifiers.Contain(key.ModAlt)
}

func (ui *uiState) modelCanvas(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	area := gtx.Constraints.Max
	if area.X < 8 || area.Y < 8 {
		v.redraw = true
		return layout.Dimensions{}
	}
	w, h := area.X, area.Y
	if w > 1280 {
		h = int(float64(h) * 1280 / float64(w))
		w = 1280
	}
	// A synthetic layout height — the scroll panel measures its tabs with
	// one — must never reach the renderer: it recreates the render target
	// at that height, the allocation fails, and the hardware path is then
	// treated as broken for the rest of the session.
	if h > 4096 {
		w = int(float64(w) * 4096 / float64(h))
		h = 4096
	}
	if h < 1 {
		h = 1
	}
	v.setRenderSize(w, h)
	if ui.fxStartSoundLoad(v) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	fxDrawn := false
	if v.showFX || v.fxAttrIndex >= 0 || (v.showFXAxes && len(v.fxEffects) == 0) {
		if ui.fxStartLoad(v) {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
		if v.showFX {
			// The hardware frame draws this frame's particles itself, so the
			// software picture is only built for the path that paints one.
			fxDrawn = v.fxAdvance(gtx.Now, w, h, v.options(w, h), ui.modelGpuUnavailable(v) != "")
		} else if len(v.fxEffects) > 0 {
			v.fxOrigins()
		}
		if fxDrawn {
			v.redraw = true
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
	}

	now := gtx.Now
	if t.kind == "sgb" && t.sgbView3D && len(v.animatedEntities) > 0 {
		if v.sgbAnimLast.IsZero() {
			v.sgbAnimLast = now
		} else {
			dt := now.Sub(v.sgbAnimLast).Seconds()
			v.sgbAnimLast = now
			if dt > 0.1 {
				dt = 0.1
			}
			for i := range v.animatedEntities {
				sgb.AdvanceEntityAnimation(&v.animatedEntities[i], dt)
			}
		}
		v.redraw = true
		gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
	} else {
		v.sgbAnimLast = time.Time{}
	}
	if v.playing && v.anim != nil {
		if v.lastFrame.IsZero() {
			v.lastFrame = now
		}
		dt := now.Sub(v.lastFrame).Seconds()
		v.lastFrame = now
		if v.animPlaybackDuration > 0 {
			v.time += float32(dt / v.animPlaybackDuration)
		} else {
			frames := maxInt(1, v.anim.NumFrames)
			v.time += float32(dt * animFramesPerSecond(v.anim) / float64(frames))
		}
		if v.time > 1 {
			v.time = float32(math.Mod(float64(v.time), 1))
		}
		v.poseValid = false
		v.redraw = true
	}
	if v.redraw {
		v.redraw = false
		if ui.renderModelOnGpu(gtx, t, w, h) {
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		} else {
			v.requestRender(w, h, modelQualityFinal)
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		}
	}
	// A frame finished on a worker goroutine after the last layout ran, so
	// schedule one more frame to actually put it on screen.
	if v.frame != nil && v.painted != v.serial {
		v.painted = v.serial
		gtx.Execute(op.InvalidateCmd{})
	}
	if v.rendering {
		// Keep polling until the background render lands.
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}

	if t.kind == "sgb" && t.sgbView3D {
		if t.sgbCameraTag == nil {
			t.sgbCameraTag = new(int)
		}
		event.Op(gtx.Ops, t.sgbCameraTag)
		for {
			ev, ok := gtx.Event(key.FocusFilter{Target: t.sgbCameraTag})
			if !ok {
				break
			}
			if focus, ok := ev.(key.FocusEvent); ok && !focus.Focus {
				t.sgbCameraKeys = [4]bool{}
				t.sgbCameraVelocity = [2]float64{}
				t.sgbCameraLastMove = time.Time{}
			}
		}
		arrowKeys := [...]key.Name{key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow}
		allModifiers := key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper
		moved := false
		for i, name := range arrowKeys {
			for {
				ev, ok := gtx.Event(key.Filter{Focus: t.sgbCameraTag, Name: name, Optional: allModifiers})
				if !ok {
					break
				}
				ke, ok := ev.(key.Event)
				if !ok {
					continue
				}
				t.sgbCameraKeys[i] = ke.State == key.Press && ke.Modifiers == 0
			}
		}
		forward, strafe := 0.0, 0.0
		if t.sgbCameraKeys[0] {
			forward++
		}
		if t.sgbCameraKeys[1] {
			forward--
		}
		if t.sgbCameraKeys[2] {
			strafe++ // left/right intentionally follow screen-facing convention
		}
		if t.sgbCameraKeys[3] {
			strafe--
		}
		dt := 1.0 / 60.0
		if !t.sgbCameraLastMove.IsZero() && !gtx.Now.IsZero() {
			dt = gtx.Now.Sub(t.sgbCameraLastMove).Seconds()
		}
		// Bound catch-up after a pause, then ease velocity toward the held
		// direction or back to rest after release for acceleration/braking.
		t.sgbCameraLastMove = gtx.Now
		dt = math.Max(0, math.Min(dt, 0.05))
		t.sgbCameraVelocity[0] = sgbApproachCameraVelocity(t.sgbCameraVelocity[0], forward, dt)
		t.sgbCameraVelocity[1] = sgbApproachCameraVelocity(t.sgbCameraVelocity[1], strafe, dt)
		moved = ui.sgbMoveCamera(t, t.sgbCameraVelocity[0], t.sgbCameraVelocity[1], dt)
		if moved || t.sgbCameraVelocity != [2]float64{} {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
	}

	rect := clip.Rect{Max: area}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &v.tag,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: math.MinInt, Max: math.MaxInt},
			ScrollY: pointer.ScrollRange{Min: math.MinInt, Max: math.MaxInt},
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonPrimary) && t.kind == "sgb" && t.sgbView3D {
				if scrubHit(v.scrubRect, e.Position) {
					t.sgbObjectDragging, t.sgbObjectRotating = false, false
					v.seeking = true
					v.dragging = false
					v.scrubSeek(e.Position.X, true)
					continue
				}
				gtx.Execute(key.FocusCmd{Tag: t.sgbCameraTag})
				v.dragging = sgbCameraDragRequested(e.Modifiers | heldModifiers())
				v.panning = false
				t.sgbSelecting = !v.dragging && t.sgbObjectSelected == ""
				t.sgbSelectionDrag = false
				t.sgbSelectionAnchor = sgbCanvasToFramePoint(area, w, h, e.Position)
				t.sgbSelectionCurrent = t.sgbSelectionAnchor
				if !v.dragging && len(t.sgbSelectedEntities) > 0 {
					camera := engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}
					if id, hit := sgbPickEntity(v.modelData(), t.sgbEntityMeshIDs, camera, float64(t.sgbSelectionAnchor.X), float64(t.sgbSelectionAnchor.Y)); hit && t.sgbSelectedEntities[id] {
						if (e.Modifiers | heldModifiers()).Contain(key.ModShift) {
							t.sgbObjectRotating = true
							t.sgbRotateLastX = e.Position.X
							t.sgbObjectMoved = false
							t.sgbSelecting = false
							continue
						}
						if point, onTerrain := sgbPickTerrainPoint(v.modelData(), camera, float64(t.sgbSelectionAnchor.X), float64(t.sgbSelectionAnchor.Y)); onTerrain {
							t.sgbObjectDragging = true
							t.sgbDragHasPoint = true
							t.sgbDragLast = point
							t.sgbObjectMoved = false
							t.sgbSelecting = false
							continue
						}
					}
				}
				if t.sgbObjectSelected != "" && !v.dragging {
					if point, hit := sgbPickTerrainPoint(v.modelData(), engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}, float64(t.sgbSelectionAnchor.X), float64(t.sgbSelectionAnchor.Y)); hit {
						ui.placeSgbObject(t, point)
					} else {
						ui.statusText = "Object placement — click on the terrain surface"
					}
					continue
				}
				if v.dragging {
					v.lastX, v.lastY = e.Position.X, e.Position.Y
				}
				continue
			}
			if e.Buttons.Contain(pointer.ButtonSecondary) {
				if t.kind == "sgb" && t.sgbView3D {
					t.sgbSelecting = false
				}
				v.panning = true
				v.dragging = false
				v.lastX, v.lastY = e.Position.X, e.Position.Y
				continue
			}
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			if scrubHit(v.scrubRect, e.Position) {
				v.seeking = true
				v.dragging = false
				picked := v.scrubSeek(e.Position.X, t != ui.unitModel || ui.unitListMode == 2)
				v.keyDrag = picked && t == ui.unitModel
				v.keyDragMoved = false
				LogInfo("3D preview: strip press pos=(%.1f, %.1f) keymark=%v", e.Position.X, e.Position.Y, v.keyDrag)
				continue
			}
			if t.kind == "sgb" && t.sgbView3D {
				continue
			}
			v.dragging = true
			v.lastX, v.lastY = e.Position.X, e.Position.Y
		case pointer.Drag:
			if t.kind == "sgb" && t.sgbView3D && t.sgbObjectRotating && e.Buttons.Contain(pointer.ButtonPrimary) {
				if !(e.Modifiers | heldModifiers()).Contain(key.ModShift) {
					t.sgbObjectRotating = false
					t.sgbObjectMoved = false
					continue
				}
				dx := float64(e.Position.X - t.sgbRotateLastX)
				t.sgbRotateLastX = e.Position.X
				if rotated := sgbRotateEntities(t.sgbMap, v.modelData(), t.sgbEntityMeshIDs, v.animatedEntities, t.sgbSelectedEntities, -dx*0.01); rotated > 0 {
					ui.geometryChangedModelGpu(v.model)
					t.sgbObjectMoved = true
					t.sgbDirty = true
					v.invalidate()
					gtx.Execute(op.InvalidateCmd{})
					ui.statusText = fmt.Sprintf("Rotated %d selected map object(s) around vertical axis", rotated)
				}
				continue
			}
			if t.kind == "sgb" && t.sgbView3D && t.sgbObjectDragging && e.Buttons.Contain(pointer.ButtonPrimary) {
				pixel := sgbCanvasToFramePoint(area, w, h, e.Position)
				camera := engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}
				if point, hit := sgbPickTerrainPoint(v.modelData(), camera, float64(pixel.X), float64(pixel.Y)); hit {
					if t.sgbDragHasPoint {
						dx, dy := point.X-t.sgbDragLast.X, point.Y-t.sgbDragLast.Y
						var moved int
						t.model.animatedEntities, moved = sgbMoveEntitiesOnSurface(t.sgbMap, v.modelData(), t.sgbEntityMeshIDs, v.animatedEntities, t.sgbSelectedEntities, dx, dy)
						if moved > 0 {
							ui.geometryChangedModelGpu(v.model)
							t.sgbObjectMoved = true
							t.sgbDirty = true
							v.invalidate()
							gtx.Execute(op.InvalidateCmd{})
							ui.statusText = fmt.Sprintf("Moved %d selected map object(s) along the terrain", moved)
						}
					}
					t.sgbDragLast, t.sgbDragHasPoint = point, true
				} else {
					t.sgbDragHasPoint = false
				}
				continue
			}
			if t.kind == "sgb" && t.sgbView3D && v.dragging && e.Buttons.Contain(pointer.ButtonPrimary) {
				dx := float64(e.Position.X - v.lastX)
				dy := float64(e.Position.Y - v.lastY)
				v.lastX, v.lastY = e.Position.X, e.Position.Y
				v.Yaw -= dx * 0.01
				v.Pitch = clampFloat64(v.Pitch+dy*0.01, -1.45, 1.45)
				v.redraw = true
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.kind == "sgb" && t.sgbView3D && t.sgbSelecting {
				t.sgbSelectionCurrent = sgbCanvasToFramePoint(area, w, h, e.Position)
				dx := t.sgbSelectionCurrent.X - t.sgbSelectionAnchor.X
				dy := t.sgbSelectionCurrent.Y - t.sgbSelectionAnchor.Y
				if dx*dx+dy*dy > 16 {
					t.sgbSelectionDrag = true
				}
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if v.panning {
				dx := float64(e.Position.X - v.lastX)
				dy := float64(e.Position.Y - v.lastY)
				v.lastX, v.lastY = e.Position.X, e.Position.Y
				v.panCamera(dx, dy, h)
				continue
			}
			if v.seeking {
				if v.keyDrag {
					if v.movePickedBoneKeyToX(e.Position.X) {
						ui.unitModelWhmDirty = true
						v.keyDragMoved = true
					}
					continue
				}
				v.scrubSeek(e.Position.X, t != ui.unitModel || ui.unitListMode == 2)
				continue
			}
			if !v.dragging {
				continue
			}
			dx := float64(e.Position.X - v.lastX)
			dy := float64(e.Position.Y - v.lastY)
			v.lastX, v.lastY = e.Position.X, e.Position.Y
			v.Yaw -= dx * 0.01
			v.Pitch = clampFloat64(v.Pitch+dy*0.01, -1.45, 1.45)
			v.redraw = true
		case pointer.Scroll:
			v.zoomBy(modelZoomByDelta(e.Scroll.Y))
			v.redraw = true
		case pointer.Release, pointer.Cancel:
			if t.kind == "sgb" && t.sgbView3D && t.sgbObjectRotating {
				t.sgbObjectRotating = false
				if e.Kind == pointer.Release && t.sgbObjectMoved {
					v.invalidate()
				}
				t.sgbObjectMoved = false
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.kind == "sgb" && t.sgbView3D && t.sgbObjectDragging {
				t.sgbObjectDragging = false
				t.sgbDragHasPoint = false
				t.sgbSelecting, t.sgbSelectionDrag = false, false
				if e.Kind == pointer.Release && t.sgbObjectMoved {
					v.invalidate()
				}
				t.sgbObjectMoved = false
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.kind == "sgb" && t.sgbView3D && v.dragging {
				v.dragging = false
				v.redraw = e.Kind == pointer.Release
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.kind == "sgb" && t.sgbView3D && t.sgbSelecting {
				t.sgbSelectionCurrent = sgbCanvasToFramePoint(area, w, h, e.Position)
				if t.sgbSelectedEntities == nil {
					t.sgbSelectedEntities = make(map[int]bool)
				}
				if e.Kind == pointer.Release {
					if t.sgbSelectionDrag {
						opts := v.options(w, h)
						selection := sgbRectangleEntities(v.modelData(), t.sgbEntityMeshIDs, buildViewMatrix(opts), w, h, sgbSelectionRect(t.sgbSelectionAnchor, t.sgbSelectionCurrent))
						t.sgbSelectedEntities = selection
					} else if id, hit := sgbPickEntity(v.modelData(), t.sgbEntityMeshIDs, engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}, float64(t.sgbSelectionCurrent.X), float64(t.sgbSelectionCurrent.Y)); hit {
						t.sgbSelectedEntities = map[int]bool{id: true}
					} else {
						clear(t.sgbSelectedEntities)
					}
					v.invalidate()
				}
				t.sgbSelecting, t.sgbSelectionDrag = false, false
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if v.keyDrag && v.keyDragMoved {
				if f, ok := v.boneKeyFrameAt(); ok {
					LogInfo("3D preview: bone key drag ended on frame %d", f+1)
					ui.statusText = fmt.Sprintf("Moved bone key to frame %d", f+1)
				}
			}
			v.keyDrag, v.keyDragMoved = false, false
			v.dragging, v.panning, v.seeking = false, false, false
		}
	}

	if v.gpuFrame != nil {
		if v.dragging || v.panning || (t.kind == "sgb" && (t.sgbCameraKeys[0] || t.sgbCameraKeys[1] || t.sgbCameraKeys[2] || t.sgbCameraKeys[3] || t.sgbCameraVelocity != [2]float64{})) || (v.playing && v.anim != nil) || (t.kind == "sgb" && t.sgbView3D && len(v.animatedEntities) > 0) {
			// Keep the frame refreshing while something is moving.
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		}
		if os.Getenv("DOW_NO_GPU_PAINT") != "" {
			// Diagnostic: keep the raw pass running but leave the frame out of
			// the op stream, which separates a bad render from a bad hand over.
			if modelGPUPaintSkipLogged.IsZero() {
				modelGPUPaintSkipLogged = time.Now()
				log.Printf("3D preview: GPU paint skipped (DOW_NO_GPU_PAINT)")
			}
			fillRect(gtx, rgb(0x1C1E24), area)
		} else {
			v.gpuFrame.Paint(gtx, image.Rectangle{Max: area})
		}
		// The picked marker is painted in the frame's own transform, so it
		// lands on the model exactly where the frame put it.
		v.gpuFrame.Overlay(gtx, image.Rectangle{Max: area}, func(gtx layout.Context) {
			v.paintMarkerOverlay(gtx, w, h)
		})
		if t.kind == "sgb" && t.sgbView3D {
			paintSgbSelectionMarquee(gtx, t, image.Rectangle{Max: area})
		}
		rect.Pop()
		return layout.Dimensions{Size: area}
	}
	img := v.frame
	if img == nil {
		if modelRenderingLogged.IsZero() || time.Since(modelRenderingLogged) >= time.Second {
			modelRenderingLogged = time.Now()
			log.Printf("3D preview: no frame on screen (skeleton=%v loading=%v)", v.showSkeleton, v.loading)
		}
		gtx.Execute(op.InvalidateCmd{At: now.Add(32 * time.Millisecond)})
		rect.Pop()
		fillRect(gtx, rgb(0x1C1E24), area)
		return layout.Stack{}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				fillRect(gtx, rgb(0x1C1E24), area)
				return layout.Dimensions{Size: area}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				msg := "Rendering…"
				if v.loading {
					msg = "Loading textures…"
				}
				l := material.Label(ui.th, unit.Sp(10), msg)
				l.Color = cTextDim
				return layout.Center.Layout(gtx, l.Layout)
			}),
		)
	}
	if v.dragging || v.panning || (t.kind == "sgb" && (t.sgbCameraKeys[0] || t.sgbCameraKeys[1] || t.sgbCameraKeys[2] || t.sgbCameraKeys[3] || t.sgbCameraVelocity != [2]float64{})) || (v.playing && v.anim != nil) || (t.kind == "sgb" && t.sgbView3D && len(v.animatedEntities) > 0) {
		gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
	}
	b := img.Bounds()
	scale := float32(area.X) / float32(b.Dx())
	if s := float32(area.Y) / float32(b.Dy()); s < scale {
		scale = s
	}
	dx := (float32(area.X) - float32(b.Dx())*scale) / 2
	dy := (float32(area.Y) - float32(b.Dy())*scale) / 2
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale))).Push(gtx.Ops)
	paint.NewImageOp(img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if fxDrawn && v.fxOverlay != nil {
		paint.NewImageOp(v.fxOverlay).Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	v.paintMarkerOverlay(gtx, w, h)
	v.paintFXAttributeMarkers(gtx, w, h)
	if t.kind == "sgb" && t.sgbView3D {
		paintSgbSelection(gtx, t, v, w, h, true)
	}
	tr.Pop()
	rect.Pop()
	return layout.Dimensions{Size: area}
}

// modelGizmoOverlay puts the camera orientation gizmo in the bottom right.
func (ui *uiState) modelGizmoOverlay(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	if v == nil || v.model == nil {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	opts := v.options(w, h)
	measure := cameraAxesLayout(ui.th, opts, false)
	gtx.Constraints.Min = image.Point{}
	d := measure(gtx)
	if d.Size.X == 0 || d.Size.Y == 0 {
		return d
	}
	at := image.Point{X: w - d.Size.X - gizmoMargin, Y: h - d.Size.Y - gizmoMargin}
	if at.X < 0 {
		at.X = 0
	}
	if at.Y < 0 {
		at.Y = 0
	}
	macro := op.Offset(at).Push(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = d.Size
	cameraAxesLayout(ui.th, opts, true)(gtx)
	macro.Pop()
	return d
}

const gizmoMargin = 10.0
