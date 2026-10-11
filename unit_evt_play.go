package main

import (
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// The Events window can now be heard, not only read. Two ways to listen: one
// event's sound on its own while its fields are being edited, and the whole
// selected timeline — every sound it fires, in timeline order, at the moments
// the DATAMTON gives them, with the usual transport: pause, resume, and a
// scrub strip to wind back or forward.
//
// The clock is the frame clock. A playhead advanced by wall time between
// frames, which is the same way the 3D preview's player is driven, and sounds
// fire by crossing their moment rather than by landing on it, so a dropped
// frame skips nothing.

// unitEvtFire is one sound of a timeline playback: when it fires (in the
// timeline's own seconds, sentinels already resolved) and the sound path its
// definition names.
type unitEvtFire struct {
	at   float64
	name string
	path string
}

// unitEvtPlayer holds everything the Events section's audio needs. It is a
// value on uiState rather than a pointer built on demand, because the click
// widgets inside it must keep their identity across frames.
type unitEvtPlayer struct {
	playEv widget.Clickable // play the selected event's sound
	tlPlay widget.Clickable // play / pause / resume the selected timeline
	tlStop widget.Clickable // stop the timeline playback
	scrub  struct{}         // pointer tag of the scrub strip

	// audition is the single event's sound while it sounds.
	audition *fx.SoundVoice

	// Timeline playback state.
	playing  bool
	paused   bool
	dragging bool
	// forTimeline and forDoc pin the playback to the selection it was started
	// from: picking another timeline or another .whe under a playing one
	// stops it rather than leaving sounds firing for a list not on screen.
	forTimeline string
	forDoc      *whe.Document
	pos         float64 // playhead, seconds into the timeline
	duration    float64 // moment of the last timed event
	last        time.Time
	order       []unitEvtFire
	next        int
	voices      []*fx.SoundVoice
	// cache keys decoded samples by the file picked to play them, so a
	// footstep fired at five moments is decoded once.
	cache map[string]*fx.SoundSample
}

func (p *unitEvtPlayer) stopVoices() {
	for _, v := range p.voices {
		v.Stop()
	}
	p.voices = nil
}

func (p *unitEvtPlayer) expire() {
	now := time.Now()
	kept := p.voices[:0]
	for _, v := range p.voices {
		if now.Before(v.EndsAt) {
			kept = append(kept, v)
			continue
		}
		v.Stop()
	}
	p.voices = kept
}

func (p *unitEvtPlayer) stopAll() {
	p.stopVoices()
	if p.audition != nil {
		p.audition.Stop()
		p.audition = nil
	}
	p.playing = false
	p.paused = false
	p.dragging = false
	p.pos = 0
	p.next = 0
	p.order = nil
}

// evtPlayerStopAll is the section's exit hook: it only touches anything when
// something is actually sounding, so the per-frame call is free.
func (ui *uiState) evtPlayerStopAll() {
	ui.evtPlayer.stopAll()
}

// unitEvtStartVoice hands one decoded sample to a fresh oto player, the same
// way the 3D preview's player does: one player per sound so two overlap
// instead of the second cutting the first off.
func unitEvtStartVoice(s *fx.SoundSample) (*fx.SoundVoice, error) {
	if s == nil || s.Info == nil || len(s.PCM) == 0 {
		return nil, fmt.Errorf("empty sample")
	}
	ctx, err := audioplayer.Context()
	if err != nil {
		return nil, err
	}
	cb := audioplayer.NewChunkBuffer(fda.PCM16Bytes(s.PCM), s.Info.SampleRate, s.Info.Channels)
	pl := ctx.NewPlayer(audioplayer.NewChunkSource(cb))
	pl.SetVolume(1)
	pl.Play()
	return fx.NewSoundVoice(pl, s), nil
}

// unitEvtLoadSound resolves one sound path against the mod tree, through the
// same rules the game uses, and returns the decoded sample for it. The first
// file of the bundle plays: a folder of variants sounds different each time in
// the game, but a preview the user is checking wants the same answer twice.
func (ui *uiState) unitEvtLoadSound(p string) (*fx.SoundSample, string, error) {
	pl := &ui.evtPlayer
	if pl.cache == nil {
		pl.cache = map[string]*fx.SoundSample{}
	}
	if ui.modTree == nil {
		return nil, "", fmt.Errorf("no mod is loaded")
	}
	roots := materialSearchRoots(ui.modTree)
	b, ok := fx.ResolveSound(ui.modTree, roots, p)
	if !ok || len(b.Files) == 0 {
		return nil, "", whe.SoundMissing(p)
	}
	file := b.Files[0]
	if s, done := pl.cache[file]; done {
		return s, file, nil
	}
	s, err := fxSoundDecodeForPlayback(ui.modTree, roots, file)
	if err != nil {
		return nil, file, err
	}
	pl.cache[file] = s
	return s, file, nil
}

// unitEvtSelectedIsSound says whether the selected event plays audio, which is
// what the Play sound button acts on and lights for.
func (ui *uiState) unitEvtSelectedIsSound() bool {
	if ui.unitEvtDoc == nil {
		return false
	}
	tl := ui.selectedUnitEventTimeline()
	if tl == nil || ui.unitEvtSel < 0 || ui.unitEvtSel >= len(tl.Events) {
		return false
	}
	def, ok := ui.unitEvtDoc.Events()[strings.ToLower(tl.Events[ui.unitEvtSel].Name)]
	if !ok {
		return false
	}
	_, isSound := def.First("sound")
	return isSound
}

// unitEvtPlaySelectedSound plays the selected event's sound. The path typed
// into the form wins over the saved one, so a retyped path can be auditioned
// before Set writes it into the document.
func (ui *uiState) unitEvtPlaySelectedSound() {
	tl := ui.selectedUnitEventTimeline()
	if tl == nil || ui.unitEvtDoc == nil || ui.unitEvtSel < 0 || ui.unitEvtSel >= len(tl.Events) {
		ui.statusText = "Select a sound event first"
		return
	}
	ev := tl.Events[ui.unitEvtSel]
	def, ok := ui.unitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !ok {
		ui.statusText = ev.Name + " has no definition in this .whe"
		return
	}
	path, _ := def.First("sound")
	if typed := strings.TrimSpace(ui.unitEvtSndEd.Text()); typed != "" {
		path = typed
	}
	path = strings.TrimSpace(path)
	if path == "" {
		ui.statusText = ev.Name + " names no sound"
		return
	}
	p := &ui.evtPlayer
	if p.audition != nil {
		p.audition.Stop()
		p.audition = nil
	}
	s, file, err := ui.unitEvtLoadSound(path)
	if err != nil {
		ui.statusText = "Play failed: " + err.Error()
		return
	}
	v, err := unitEvtStartVoice(s)
	if err != nil {
		ui.statusText = "Play failed: " + err.Error()
		return
	}
	p.audition = v
	ui.statusText = "Playing " + filepath.Base(file)
}

// unitEvtTimelinePlay starts the selected timeline from the beginning: every
// sound its events name, sorted by moment, the entry sentinel at zero and the
// exit sentinel at the last timed event, which is as close to "on leaving" as
// a timeline with no declared length can get.
func (ui *uiState) unitEvtTimelinePlay(gtx layout.Context) {
	tl := ui.selectedUnitEventTimeline()
	p := &ui.evtPlayer
	if tl == nil || ui.unitEvtDoc == nil {
		ui.statusText = "Select a timeline first"
		return
	}
	defs := ui.unitEvtDoc.Events()
	var order []unitEvtFire
	var duration float64
	for _, ev := range tl.Events {
		def, ok := defs[strings.ToLower(ev.Name)]
		if !ok {
			continue
		}
		path, isSound := def.First("sound")
		if !isSound || strings.TrimSpace(path) == "" {
			continue
		}
		at := ev.Time
		if at >= 0 && at <= 1e30 && at > duration {
			duration = at
		}
		order = append(order, unitEvtFire{at: at, name: ev.Name, path: strings.TrimSpace(path)})
	}
	if len(order) == 0 {
		ui.statusText = "No sounds fire on " + tl.Name
		return
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].at < order[j].at })
	// After the sort, so entry events stay ahead of everything timed at zero
	// and exit events behind everything else.
	for i := range order {
		switch {
		case order[i].at < 0:
			order[i].at = 0
		case order[i].at > 1e30:
			order[i].at = duration
		}
	}
	p.stopVoices()
	p.order = order
	p.duration = duration
	p.pos = 0
	p.next = 0
	p.playing = true
	p.paused = false
	p.forTimeline = tl.Name
	p.forDoc = ui.unitEvtDoc
	p.last = gtx.Now
	if p.last.IsZero() {
		p.last = time.Now()
	}
	ui.statusText = fmt.Sprintf("Playing %s: %d sounds over %.2fs", tl.Name, len(order), duration)
}

// unitEvtPlayerSeek winds the playhead to a fraction of the timeline's length.
// Everything sounding stops, and the fire cursor moves with the playhead, so a
// sound already passed stays quiet until the playhead is wound back before it.
func (p *unitEvtPlayer) seek(frac float64) {
	if !p.playing {
		return
	}
	p.pos = frac * p.duration
	p.stopVoices()
	for p.next > 0 && p.order[p.next-1].at > p.pos {
		p.next--
	}
	for p.next < len(p.order) && p.order[p.next].at <= p.pos {
		p.next++
	}
}

// unitEvtPlayerTick is one frame of the transport. It reads the three buttons,
// advances the playhead, and starts whatever the playhead has crossed. While a
// timeline plays it asks for the next frame, which is what keeps the clock
// running when the user touches nothing.
func (ui *uiState) unitEvtPlayerTick(gtx layout.Context) {
	p := &ui.evtPlayer
	if p.playing && (p.forDoc != ui.unitEvtDoc || p.forTimeline != ui.unitEvtTimeline) {
		p.stopAll()
	}
	if p.tlStop.Clicked(gtx) {
		p.stopAll()
		ui.statusText = "Playback stopped"
	}
	if p.tlPlay.Clicked(gtx) {
		if p.playing {
			p.paused = !p.paused
		} else {
			ui.unitEvtTimelinePlay(gtx)
		}
	}
	if p.playEv.Clicked(gtx) {
		ui.unitEvtPlaySelectedSound()
	}
	if p.audition != nil && !time.Now().Before(p.audition.EndsAt) {
		p.audition.Stop()
		p.audition = nil
	}
	if !p.playing {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	dt := now.Sub(p.last).Seconds()
	p.last = now
	if dt < 0 {
		dt = 0
	}
	// A stall — a modal dialog, a slow decode on the first play — must not
	// skip the sounds it put the playhead past; it counts as the frame it is.
	if dt > 0.25 {
		dt = 0.25
	}
	if !p.paused && !p.dragging {
		p.pos += dt
	}
	for p.next < len(p.order) && p.order[p.next].at <= p.pos {
		f := p.order[p.next]
		p.next++
		s, _, err := ui.unitEvtLoadSound(f.path)
		if err != nil {
			log.Printf("evt play: %s: %v", f.name, err)
			continue
		}
		v, err := unitEvtStartVoice(s)
		if err != nil {
			log.Printf("evt play: %s: %v", f.name, err)
			continue
		}
		p.voices = append(p.voices, v)
	}
	p.expire()
	if p.pos >= p.duration && len(p.voices) == 0 {
		p.playing = false
		p.paused = false
	}
	gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
}

// ── the transport row ──────────────────────────────────────────────────────

// layoutUnitEvtTransport is the row above the events: play/pause, stop, the
// playhead's time, and the scrub strip.
func (ui *uiState) layoutUnitEvtTransport(gtx layout.Context) layout.Dimensions {
	p := &ui.evtPlayer
	noTl := ui.unitEvtDoc == nil || ui.selectedUnitEventTimeline() == nil
	btn := "Play"
	if p.playing {
		if p.paused {
			btn = "Resume"
		} else {
			btn = "Pause"
		}
	}
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &p.tlPlay, btn, noTl)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &p.tlStop, "Stop", !p.playing)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := ""
				if p.playing || p.pos > 0 {
					text = fmt.Sprintf("%.2f / %.2f s", p.pos, p.duration)
				}
				l := material.Label(ui.th, unit.Sp(9), text)
				l.Color = cTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutUnitEvtScrub(gtx, 10)
				})
			}),
		)
	})
}

// layoutUnitEvtScrub is the strip itself: a played portion, a tick per sound
// in timeline order, and the playhead. Press or drag winds to the pointer's
// position — the whole strip is the target, so it needs no handle to aim at.
func (ui *uiState) layoutUnitEvtScrub(gtx layout.Context, hDp int) layout.Dimensions {
	p := &ui.evtPlayer
	w := gtx.Constraints.Max.X
	if w < 1 {
		w = 1
	}
	h := gtx.Dp(unit.Dp(hDp))
	gtx.Constraints.Min = image.Pt(w, h)
	gtx.Constraints.Max = image.Pt(w, h)
	frac := 0.0
	if p.duration > 0 {
		frac = math.Max(0, math.Min(1, p.pos/p.duration))
	}
	posX := int(float64(w) * frac)
	// Input first: the strip registers itself, reads whatever the pointer did
	// to it, and only then draws at the position those events settled on.
	stack := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.scrub)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.scrub, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press, pointer.Drag:
			if !p.playing {
				break
			}
			f := float64(e.Position.X) / float64(w)
			p.dragging = true
			p.seek(math.Max(0, math.Min(1, f)))
		case pointer.Release, pointer.Cancel:
			p.dragging = false
		}
	}
	stack.Pop()
	unitEvtFillRect(gtx, image.Rect(0, 0, w, h), cContentBg)
	if posX > 0 {
		unitEvtFillRect(gtx, image.Rect(0, 0, posX, h), cSelectBg)
	}
	if p.duration > 0 {
		for _, f := range p.order {
			x := int(float64(w) * (f.at / p.duration))
			if x < 0 {
				x = 0
			}
			if x >= w {
				x = w - 1
			}
			unitEvtFillRect(gtx, image.Rect(x, 0, x+1, h), cEventSnd)
		}
	}
	unitEvtFillRect(gtx, image.Rect(posX, 0, minInt(posX+2, w), h), rgba(255, 255, 255, 200))
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func unitEvtFillRect(gtx layout.Context, r image.Rectangle, col color.NRGBA) {
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}
