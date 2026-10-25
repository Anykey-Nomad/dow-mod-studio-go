package whe

import (
	"dow-mod-studio/internal/chunky"
	"fmt"
	"math"
	"strings"
)

// A unit's effects are wired up in three steps. The entity blueprint
// (attrib/ebps/**.lua) maps gameplay triggers onto named events through
// event_manager_ext; the name resolves to a .events file under art/events;
// and that file says which .lua effect to play and where — usually the name
// of one of the markers the model's DATAMARK chunk declares.
//
// FxEventEntry is one key/value pair of an event definition. The keys seen in
// the game are "fx" (path of the .lua effect under art/fx) and several marker
// flavours ("marker", "random_marker_N", "marker_surface_NN",
// "marker_variance_x/z"), plus selectors such as "random_set".
type FxEventEntry struct {
	Key   string
	Value string
}

// FxEvent is one parsed event definition.
type FxEvent struct {
	Name    string
	Entries []FxEventEntry
}

// First returns the value of the first entry with the given key, compared
// case-insensitively, which is how callers look up "fx" or "marker".
func (e FxEvent) First(key string) (string, bool) {
	for _, en := range e.Entries {
		if strings.EqualFold(en.Key, key) {
			return en.Value, true
		}
	}
	return "", false
}

// ParseFxEvents reads the event definitions of one .events file. A file holds
// a FOLDEVCT folder with one or more DATAEVNT payloads; the event name is the
// chunk's own name and its payload is a count of key/value pairs, both sides
// length prefixed strings (the chunk name is NUL padded).
func ParseFxEvents(data []byte) ([]FxEvent, error) {
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	var out []FxEvent
	var firstErr error
	var walk func(cs []*chunky.Chunk)
	walk = func(cs []*chunky.Chunk) {
		for _, c := range cs {
			if c.TypeID == "DATAEVNT" {
				ev, err := parseDataEvnt(c.Name, c.Payload)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
				} else {
					out = append(out, ev)
				}
			}
			walk(c.Children)
		}
	}
	walk(chunks)
	if len(out) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("no DATAEVNT chunk in events file")
	}
	return out, nil
}

// A .whe also says which of those events play while each animation runs. That
// is a DATAMTON chunk per animation, held in the file's FOLDMTRE folder: a
// motion timeline naming the animations it blends and the events it fires.
// Reading it is what separates the effects that belong to the state on screen
// from the ones that belong to building or dying — a plasma generator's idle
// runs two green glows, and its build and death runs a cloud of smoke that
// would otherwise play on top of them.
//
// FxTimeline is one parsed DATAMTON chunk. Name is the chunk's own name, which
// is the animation it drives; Animations are the clips it may blend, and
// Events are the effects it fires, each with the moment it fires at.
type FxTimeline struct {
	Name          string
	Animations    []string
	Motions       []string
	RandomMotions []RandomMotion
	Reshuffle     bool
	Events        []FxTimelineEvent
	// Tail is the part of the payload after the event list: how the motion
	// blends and is delayed, and the modifier it may drive.
	Tail FxMotionTail
}

// FxTimelineEvent is one event of a timeline and when it fires. DATAMTON
// stores ordinary event times as seconds into the clip; -1 means "on entering"
// and the largest float means "on leaving".
type FxTimelineEvent struct {
	Name string
	Time float64
}

// FxMotionTail is the part of a DATAMTON payload after its event list: how
// the motion plays — its looping, its delays and transitions, and the
// modifier it may drive. Present says the payload carried a tail at all, so a
// file whose timeline stops at the events shows no motion block rather than
// one full of zeros.
//
// The layout follows the object_tool reference: motion type, start delay,
// loop delay, transition out, inset, exit delay, then the ignore-exit-delay
// and ignore-transitions flags, then has-modifier and the modifier name.
type FxMotionTail struct {
	Present           bool
	Type              int32
	StartDelay        [2]float32
	LoopDelay         [2]float32
	TransitionOut     float32
	Inset             [2]float32
	ExitDelay         [2]float32
	IgnoreExitDelay   bool
	IgnoreTransitions bool
	Modifier          string
}

// mtonTailMinSize is the fixed part of a DATAMTON tail: int32 + five float
// pairs... no — int32, 2f, 2f, f, 2f, 2f and three bytes, without the
// optional modifier name. A payload with fewer bytes left cannot hold one.
const mtonTailMinSize = 4 + 8 + 8 + 4 + 8 + 8 + 3

// parseMtonTail reads a DATAMTON payload from the motion type onwards. It is
// only called when the payload has room for the fixed part, so a short read
// means damage rather than an old file.
func parseMtonTail(r *chunky.Reader) FxMotionTail {
	var t FxMotionTail
	t.Type = r.I32()
	t.StartDelay = [2]float32{r.F32(), r.F32()}
	t.LoopDelay = [2]float32{r.F32(), r.F32()}
	t.TransitionOut = r.F32()
	t.Inset = [2]float32{r.F32(), r.F32()}
	t.ExitDelay = [2]float32{r.F32(), r.F32()}
	t.IgnoreExitDelay = r.I8() != 0
	t.IgnoreTransitions = r.I8() != 0
	if r.I8() != 0 {
		t.Modifier = strings.TrimRight(r.Str(), "\x00")
	}
	return t
}

// fxMotionTypeName is how the motion type reads in the Events panel. The
// numbers are the file's own: 0 non-looping, 1 looping, 2 hold end.
func fxMotionTypeName(t int32) string {
	switch t {
	case 0:
		return "non-looping"
	case 1:
		return "looping"
	case 2:
		return "hold end"
	default:
		return fmt.Sprintf("unknown (%d)", t)
	}
}

// fxDelayPairLabel is one of the tail's float pairs as typed numbers: the
// file keeps two for every delay and for the inset, and the Events panel
// shows both rather than guessing at what the second one means.
func fxDelayPairLabel(p [2]float32) string {
	return fmt.Sprintf("%.3f, %.3f", p[0], p[1])
}

func fxYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// MotionTailRows is the tail laid out as label/value pairs for the Events
// panel, in the order the file stores them.
func MotionTailRows(t FxMotionTail) [][2]string {
	modifier := t.Modifier
	if modifier == "" {
		modifier = "—"
	}
	return [][2]string{
		{"Type", fxMotionTypeName(t.Type)},
		{"Start delay", fxDelayPairLabel(t.StartDelay)},
		{"Loop delay", fxDelayPairLabel(t.LoopDelay)},
		{"Exit delay", fxDelayPairLabel(t.ExitDelay)},
		{"Transition out", fmt.Sprintf("%.3f", t.TransitionOut)},
		{"Inset", fxDelayPairLabel(t.Inset)},
		{"Ignore exit delay", fxYesNo(t.IgnoreExitDelay)},
		{"Ignore transitions", fxYesNo(t.IgnoreTransitions)},
		{"Modifier", modifier},
	}
}

// ParseFxTimelines reads the motion timelines of a .whe. A file without them
// is not an error: models that carry all their effects in their blueprint do
// not have any, and an empty result simply means "play them all".
func ParseFxTimelines(data []byte) []FxTimeline {
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	var out []FxTimeline
	var walk func(cs []*chunky.Chunk)
	walk = func(cs []*chunky.Chunk) {
		for _, c := range cs {
			if c.TypeID == "DATAMTON" {
				if tl, err := parseDataMton(c.Name, c.Payload); err == nil {
					out = append(out, tl)
				}
			}
			walk(c.Children)
		}
	}
	walk(chunks)
	return out
}

func parseDataMton(chunkName string, p []byte) (FxTimeline, error) {
	name := strings.TrimRight(chunkName, "\x00")
	r := &chunky.Reader{Data: p}
	tl := FxTimeline{Name: name}
	n := r.Count(4)
	for i := 0; i < n && r.Err == nil; i++ {
		tl.Animations = append(tl.Animations, r.Str())
	}
	// Motion links chain into another DATAMTON when this motion does not
	// carry an animation. Preserve names so idle resolution can follow them.
	n = r.Count(8)
	for i := 0; i < n && r.Err == nil; i++ {
		motion := RandomMotion{Name: strings.TrimSpace(r.Str()), Weight: r.F32()}
		tl.Motions = append(tl.Motions, motion.Name)
		tl.RandomMotions = append(tl.RandomMotions, motion)
	}
	// Whether the pick is reshuffled every loop.
	tl.Reshuffle = r.I8() != 0
	n = r.Count(8)
	for i := 0; i < n && r.Err == nil; i++ {
		ev := r.Str()
		t := float64(r.F32())
		if r.Err == nil {
			tl.Events = append(tl.Events, FxTimelineEvent{Name: ev, Time: t})
		}
	}
	// The tail after the events: how the motion blends. A payload that stops
	// at the events has no tail to read; one that runs out halfway through
	// it leaves its damage for the check below rather than showing half a
	// motion, so the read is rolled back and the timeline keeps its events.
	if r.Err == nil && len(p)-r.Off >= mtonTailMinSize {
		off := r.Off
		tl.Tail = parseMtonTail(r)
		if r.Err != nil {
			r.Err = nil
			r.Off = off
		} else {
			tl.Tail.Present = true
		}
	}
	if r.Err != nil {
		return FxTimeline{}, fmt.Errorf("timeline %q: %w", name, r.Err)
	}
	return tl, nil
}

// TimeNormalized converts the timeline's seconds to the normalized
// 0..1 playhead used by sound playback and the scrub bar. The two sentinel
// values stay special: negative times fire on entry, and the largest float
// fires on exit.
func TimeNormalized(at, duration float64) float64 {
	if at < 0 {
		return -1
	}
	if math.IsInf(at, 1) || at > 1e30 {
		return 1
	}
	if duration <= 0 {
		duration = 1
	}
	return math.Max(0, math.Min(1, at/duration))
}

// TimeSeconds converts a raw timeline event time to seconds into its
// clip, resolving the same entry/exit sentinels for effect simulation.
func TimeSeconds(at, duration float64) float64 {
	if at < 0 {
		return 0
	}
	if math.IsInf(at, 1) || at > 1e30 {
		return duration
	}
	if duration <= 0 {
		duration = 1
	}
	return math.Max(0, math.Min(1, at)) * duration
}

func parseDataEvnt(chunkName string, p []byte) (FxEvent, error) {
	name := strings.TrimRight(chunkName, "\x00")
	r := &chunky.Reader{Data: p}
	n := r.Count(8)
	ev := FxEvent{Name: name}
	for i := 0; i < n && r.Err == nil; i++ {
		k := strings.TrimRight(r.Str(), "\x00")
		v := strings.TrimRight(r.Str(), "\x00")
		ev.Entries = append(ev.Entries, FxEventEntry{Key: k, Value: v})
	}
	if r.Err != nil {
		return FxEvent{}, fmt.Errorf("event %q: %w", name, r.Err)
	}
	return ev, nil
}
