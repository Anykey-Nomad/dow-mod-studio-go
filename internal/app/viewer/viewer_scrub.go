package viewer

import (
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/whe"
	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"image"
	"image/color"
	"log"
	"math"
	"sort"
	"strings"
	"time"
)

// ScrubBarThickness is the bar's own height. It is thin on purpose — it is a
// ruler laid over the model, not a control the reader has to aim at, since a
// click anywhere in the strip is a seek.
const ScrubBarThickness = 5.0

// ScrubBarMargin keeps the strip off the frame's edge, matching the gizmo's.
const ScrubBarMargin = 10.0

// ScrubLabelPx is roughly the width of a two or three digit frame number. It
// decides how many numbers fit, and so how often one is drawn.
const ScrubLabelPx = 26.0

// ScrubLabelHeight is the box a number is laid out in, above or below.
const ScrubLabelHeight = 13.0

// The two event marks, kept apart because they mean different things: one is
// something seen, the other something heard.
var (
	CEventFX  = uikit.Rgb(0x7BD88F)
	CEventSnd = uikit.Rgb(0x6FB7E8)
	// The clip's own keyframes — where any track of the animation holds a
	// key — are amber, the same warm colour as a picked bone, because both
	// answer "which spot is this" about the same skeleton.
	CKeyFrame = uikit.Rgb(0xE8A33D)
	// The picked bone's own keyframes are rose, so they read as the key
	// frames the bone parameters panel is editing rather than as the clip's.
	CBoneKey = uikit.Rgb(0xE27B9D)
)

// AnimScrubEvent is one moment in a clip that something fires on.
type AnimScrubEvent struct {
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

// FxClipEvents inverts the fires index: clip name to the events that clip fires
// and when, in the timeline's own seconds. fxTimelineFires is keyed by event
// and normalizes against a clip's length, which is the right way round for
// building the effects; the scrub bar wants the file's own number, because a
// frame is only computable once the clip on show says how long it is.
func FxClipEvents(timelines []whe.FxTimeline) map[string][]AnimScrubEvent {
	out := map[string][]AnimScrubEvent{}
	for _, tl := range timelines {
		seen := map[string]bool{}
		for _, anim := range append([]string{tl.Name}, tl.Animations...) {
			key := strings.ToLower(anim)
			if anim == "" || seen[key] {
				continue
			}
			seen[key] = true
			for _, ev := range tl.Events {
				out[key] = append(out[key], AnimScrubEvent{Name: ev.Name, Time: ev.Time})
			}
		}
	}
	return out
}

// FrameAt is the frame a timeline time falls on. The playhead is read the other
// way round by currentFrame, which rounds, so this rounds too: the tick under a
// number and the frame that number seeks to are then the same one.
func FrameAt(at float64, numFrames int) int {
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
	return uikit.MinInt(uikit.MaxInt(f, 0), numFrames-1)
}

// clipScrubEvents is the marked frames of one clip: every moment the .whe fires
// something, in order, each tagged with what fires there.
//
// An event the clip fires twice is kept twice: a clip that rings a footstep at
// a quarter and again at three quarters has two frames worth stopping at, and
// collapsing them to the first would hide the second.
func ClipScrubEvents(v *ModelViewer, anim string) []AnimScrubEvent {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 || anim == "" {
		return nil
	}
	list := FxClipEvents(v.FxTimelines)[strings.ToLower(anim)]
	if len(list) == 0 {
		return nil
	}
	// Which of them are audio: the player's own event set is the only place
	// that knows, and it was built from the same .whe.
	out := make([]AnimScrubEvent, 0, len(list))
	for _, e := range list {
		at := whe.TimeNormalized(e.Time, AnimClipSeconds(v.Anim))
		out = append(out, AnimScrubEvent{
			Frame: FrameAt(at, v.Anim.NumFrames),
			Name:  e.Name,
			Sound: v.FxSound != nil && v.FxSound.IsSoundEvent(e.Name),
			Time:  at,
		})
	}
	return out
}

// ScrubHit reports whether a pointer position is inside the strip. It is
// written out rather than taken from image.Rectangle so the two callers — the
// press and the drag — cannot disagree about what counts as a hit.
func ScrubHit(r image.Rectangle, p f32.Point) bool {
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
func ScrubGeometry(v *ModelViewer) (left, width float32, ok bool) {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 {
		return 0, 0, false
	}
	w := float32(v.RenderW)
	if w <= 0 {
		return 0, 0, false
	}
	left = float32(ScrubBarMargin)
	width = w - 2*left
	if width <= 0 {
		return 0, 0, false
	}
	return left, width, true
}

// movePickedBoneKeyToX moves the picked bone key to the frame under x on the
// strip, over the same geometry scrubSeek seeks by, so the key lands on the
// frame the cursor is on while it is dragged.
func MovePickedBoneKeyToX(v *ModelViewer, x float32) bool {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 {
		return false
	}
	left, width, ok := ScrubGeometry(v)
	if !ok {
		return false
	}
	return MovePickedBoneKeyToFrame(v, ScrubBarAt(x, left, width, v.Anim.NumFrames))
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
func ScrubSeek(v *ModelViewer, x float32, snapKeys bool) bool {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 {
		return false
	}
	left, width, ok := ScrubGeometry(v)
	if !ok {
		return false
	}
	// The frame the click lands on, then the keyframe it may snap to: a
	// click that lands on a keyframe mark winds to the keyframe rather than
	// to the frame it happened to hit, because selecting a keyframe is the
	// whole point of marking them and a mark a pixel off would otherwise
	// leave the playhead beside the key instead of on it.
	frame := ScrubBarAt(x, left, width, v.Anim.NumFrames)
	if !snapKeys {
		SeekFrame(v, frame)
		return false
	}
	// With a bone picked that bone's keys are what the strip marks, and a
	// press on one of them both selects it and says the drag that may follow
	// is a key drag.
	if BoneTrack(v) != nil {
		if keys := BoneKeyFrames(v); len(keys) > 0 {
			if f, ok := SnapKeyFrame(x, keys, left, width, v.Anim.NumFrames); ok {
				frame = f
				if idx, mode, ok := BoneKeyFrame(v, frame); ok {
					v.BoneKeyIndex = idx
					v.BoneKeyMode = mode
					Invalidate(v)
					SeekFrame(v, frame)
					return true
				}
			}
		}
		SeekFrame(v, frame)
		return false
	}
	// No bone is picked, so the strip marks the clip's own keyframes — the
	// moments worth stopping at, wherever they come from.
	if keys := ClipKeyFrames(v.Anim); len(keys) > 0 {
		if f, ok := SnapKeyFrame(x, keys, left, width, v.Anim.NumFrames); ok {
			frame = f
		}
	}
	SeekFrame(v, frame)
	return false
}

// seekFrame puts the playhead on a frame of the clip on show and holds it there,
// the way the step buttons do: a click lands on a frame to be looked at, not
// on a frame to be watched from, so playback stops until play is pressed again.
func SeekFrame(v *ModelViewer, frame int) {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 {
		return
	}
	frame = uikit.MinInt(uikit.MaxInt(frame, 0), v.Anim.NumFrames-1)
	v.Playing = false
	// The clock is zeroed for the same reason the step buttons zero it: pressing
	// play afterwards should start from the frame that was clicked rather than
	// jump a whole second past it.
	v.LastFrame = time.Time{}
	// The playhead is the middle of the frame, which is where a step leaves it.
	v.Time = float32((float64(frame) + 0.5) / float64(v.Anim.NumFrames))
	if v.Time >= 1 {
		v.Time = 0
	}
	Invalidate(v)
	log.Printf("3D preview: seek to frame %d of %s (%d frames)",
		frame+1, v.Anim.Name, v.Anim.NumFrames)
}

// ScrubBarAt is the frame a click at x lands on. The bar is split into equal
// slices, one per frame, and the click takes the slice it falls in, so a click
// anywhere near a frame finds it.
func ScrubBarAt(x, left, width float32, numFrames int) int {
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
	return uikit.MinInt(uikit.MaxInt(f, 0), numFrames-1)
}

// FrameX is where a frame sits on the bar: frames are spread evenly and the
// first is at the left edge, so the strip reads as the clip does.
func FrameX(frame, numFrames int, width float32) float32 {
	if numFrames < 1 {
		return 0
	}
	return float32(frame) / float32(numFrames) * width
}

// ClipKeyFrames is every frame of the clip that carries a key: bone position
// and rotation keys, mesh visibility keys and texture keys alike, since all
// three are keyframes of the same clip and a reader stepping the strip wants
// to stop at each of them. The answer is sorted and deduplicated — most keys
// land on frames another track also keys, and an unsorted strip would mark
// the same frame out of order.
func ClipKeyFrames(anim *uistate.WhmAnimation) []int {
	if anim == nil || anim.NumFrames < 1 {
		return nil
	}
	seen := make(map[int]bool)
	add := func(t float32) {
		seen[FrameAt(float64(t), anim.NumFrames)] = true
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

// ScrubKeySnapPx is how near a keyframe mark a click has to land to wind the
// playhead onto the keyframe rather than the frame under it. It is a few
// pixels: close enough that aiming at a mark finds it, far enough that a
// click between marks still lands on the frame it looked like.
const ScrubKeySnapPx = 6

// SnapKeyFrame is the keyframe a click at x should wind to, when one is close
// enough on the bar, and whether there is one at all.
func SnapKeyFrame(x float32, keys []int, left, width float32, numFrames int) (int, bool) {
	best, bestDist := -1, float32(ScrubKeySnapPx)
	for _, f := range keys {
		d := AbsF32(left + FrameX(f, numFrames, width) - x)
		if d <= bestDist {
			best, bestDist = f, d
		}
	}
	return best, best >= 0
}

// AbsF32 is the absolute value of a float32, which the standard library only
// offers in float64.
func AbsF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// modelAnimScrubOverlay draws the frame strip over the bottom of the canvas and
// records where it is, so the canvas's own press handler can tell a seek from
// the start of a camera drag.
func ModelAnimScrubOverlay(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
	v := t.Model
	if v == nil || v.Model == nil || v.Anim == nil || v.Anim.NumFrames < 1 {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	n := v.Anim.NumFrames
	left := float32(ScrubBarMargin)
	width := float32(w) - 2*left
	// Room for the bar and a number on each side of it.
	if width < 60 || h < int(ScrubBarThickness)+2*int(ScrubLabelHeight)+4*int(ScrubBarMargin) {
		v.ScrubRect = image.Rectangle{}
		return layout.Dimensions{}
	}
	barY := h - ScrubBarMargin - ScrubLabelHeight

	// The strip is remembered so a press inside it seeks. The hit area is taller
	// than the bar: a five pixel line is a poor target.
	hit := int(ScrubLabelHeight)
	v.ScrubRect = image.Rect(
		int(left)-hit, int(barY)-hit,
		int(left+width)+hit, int(barY)+hit,
	)

	// Everything is drawn relative to the left edge of the bar, so a frame's
	// place on it is the only number that has to be worked out.
	at := op.Offset(image.Point{X: int(left), Y: int(barY)}).Push(gtx.Ops)
	origin := gtx

	ScrubFill(origin, 0, -ScrubBarThickness/2, width, ScrubBarThickness, uikit.CSaveBd)
	played := float32(v.Time) * width
	if played > 0 {
		ScrubFill(origin, 0, -ScrubBarThickness/2, played, ScrubBarThickness, uikit.CSelectBd)
	}

	// A tick every frame, thinned to what the width can carry.
	step := int(math.Max(1, math.Floor(float64(n)/float64(width/ScrubLabelPx))))
	for f := 0; f < n; f += step {
		x := FrameX(f, n, width)
		ScrubFill(origin, x-0.5, -ScrubBarThickness, 1, ScrubBarThickness*2, uikit.CSaveBd)
	}

	// The frames something fires on, marked above the bar so they read as
	// belonging to a frame rather than to the bar itself.
	for _, e := range ClipScrubEvents(v, v.Anim.Name) {
		x := FrameX(e.Frame, n, width)
		col := CEventFX
		if e.Sound {
			col = CEventSnd
		}
		ScrubFill(origin, x-1.5, -ScrubBarThickness-7, 3, 6, col)
	}

	// The keyframe marks are only on show with the bone list: on the
	// animation and marker lists they name keys that no list there selects,
	// so they are drawn off and the strip reads as frames alone.
	showKeys := t != ui.UnitModel || ui.UnitListMode == 2

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
	if showKeys && BoneTrack(v) == nil {
		lastKeyX := float32(-1e9)
		for _, f := range ClipKeyFrames(v.Anim) {
			x := FrameX(f, n, width)
			if x-lastKeyX < 4 {
				continue
			}
			lastKeyX = x
			ScrubFill(origin, x-1, -ScrubBarThickness-3, 2, ScrubBarThickness*2+6, CKeyFrame)
		}
	}

	// The picked bone's own keyframes, drawn through the bar in rose. They
	// sit on top of the clip's amber marks wherever they share a frame, and
	// they are what the bone parameters panel's keyframe rows are talking about.
	if showKeys && v.BoneIndex >= 0 && v.BoneIndex < len(v.Model.Bones) {
		lastBoneKeyX := float32(-1e9)
		for _, f := range BoneKeyFrames(v) {
			x := FrameX(f, n, width)
			if x-lastBoneKeyX < 4 {
				continue
			}
			lastBoneKeyX = x
			ScrubFill(origin, x-1, -ScrubBarThickness-4, 2, ScrubBarThickness*2+8, CBoneKey)
		}
		// The picked bone key is drawn over its rose mark so it reads as the
		// one the panel's "Set ..." buttons refer to.
		if v.BoneKeyIndex >= 0 {
			if f, ok := BoneKeyFrameAt(v); ok {
				x := FrameX(f, n, width)
				ScrubFill(origin, x-2, -ScrubBarThickness-5, 4, ScrubBarThickness*2+10, uikit.CText)
			}
		}
	}

	// The frame on screen.
	cur := int(math.Round(float64(v.Time) * float64(n)))
	cur = uikit.MinInt(uikit.MaxInt(cur, 0), n-1)
	ScrubFill(origin, FrameX(cur, n, width)-1, -ScrubBarThickness-2, 2,
		ScrubBarThickness*2+4, uikit.Rgb(0xFFFFFF))

	// The numbers, alternating above and below the bar: the first above, the
	// second below, the third above, and so on, which is what gives every
	// number twice the horizontal room its neighbour would otherwise take.
	labels := 0
	above := true
	for f := 0; f < n && labels < 96; f += step {
		x := FrameX(f, n, width)
		if x < ScrubLabelPx/2 || x > width-ScrubLabelPx/2 {
			continue
		}
		y := -ScrubBarThickness - 2 - ScrubLabelHeight
		if !above {
			y = ScrubBarThickness + 2
		}
		g := origin
		macro := op.Offset(image.Point{X: int(x) - int(ScrubLabelPx)/2, Y: int(y)}).Push(g.Ops)
		g.Constraints.Min = image.Point{}
		g.Constraints.Max = image.Point{X: int(ScrubLabelPx), Y: int(ScrubLabelHeight)}
		l := material.Label(ui.Th, unit.Sp(9), uikit.ItoaPlain(f+1))
		l.Color = uikit.CTextDim
		l.MaxLines = 1
		l.Layout(g)
		macro.Pop()
		labels++
		above = !above
	}

	at.Pop()
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// ScrubFill paints one rectangle at an offset from the bar's own origin.
func ScrubFill(gtx layout.Context, x, y, w, h float32, col color.NRGBA) {
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

// modelCanvas paints the last rendered frame and handles orbiting.
func SgbCameraDragRequested(modifiers key.Modifiers) bool {
	return modifiers.Contain(key.ModAlt)
}

// modelGizmoOverlay puts the camera orientation gizmo in the bottom right.
func ModelGizmoOverlay(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
	v := t.Model
	if v == nil || v.Model == nil {
		return layout.Dimensions{}
	}
	w, h := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	if w <= 0 || h <= 0 {
		return layout.Dimensions{}
	}
	opts := Options(v, w, h)
	measure := render.CameraAxesLayout(ui.Th, opts, false)
	gtx.Constraints.Min = image.Point{}
	d := measure(gtx)
	if d.Size.X == 0 || d.Size.Y == 0 {
		return d
	}
	at := image.Point{X: w - d.Size.X - GizmoMargin, Y: h - d.Size.Y - GizmoMargin}
	if at.X < 0 {
		at.X = 0
	}
	if at.Y < 0 {
		at.Y = 0
	}
	macro := op.Offset(at).Push(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = d.Size
	render.CameraAxesLayout(ui.Th, opts, true)(gtx)
	macro.Pop()
	return d
}

const GizmoMargin = 10.0
