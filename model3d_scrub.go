package main

import (
	"dow-mod-studio/internal/whe"
	"image"
	"image/color"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"gioui.org/f32"
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
