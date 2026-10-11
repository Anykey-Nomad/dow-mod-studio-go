package fx

import (
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/whe"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// The sounds a unit makes are wired up in two steps, both in the .whe beside
// the model: the motion timeline fires an event at a point in a clip, and a
// DATAEVNT chunk of that name says which sound to play. The preview plays them
// the same way, which means two things it has to get right:
//
//   - a sound starts when the clip reaches the moment the timeline names, not
//     when the clip starts and not every frame;
//   - several of them may overlap. A unit's death timeline fires sound_die_normal_1,
//     sound_melee_damage and sound_power_lose all at 0.0 and sound_electrical_dysfunction
//     at 0.1, which in the game are four samples on top of each other. They are
//     separate players, not one that cuts the last off.
//
// The times are DATAMTON seconds into the clip, with -1 meaning on entering
// and the largest float meaning on leaving it.

// SoundPlayer holds the samples a model's sound events resolve to, decoded
// once, and starts them as the clip reaches the moments that name them.
type SoundPlayer struct {
	// events maps a timeline event name in lower case to the bundle of files
	// that may serve it.
	events map[string]SoundBundle
	// clips is the timeline of each animation, which is what says when an event
	// fires and what it is named.
	clips map[string]whe.FxTimeline

	// preloaded holds what the worker decoded, and decoded is what has been
	// played at least once. A sound that fires on every footstep must not be
	// decoded again each time, so the two are kept apart: preloaded is filled
	// from outside and read here, decoded is filled here.
	preloaded map[string]*SoundSample
	decoded   map[string]*SoundSample

	// playing are the sounds currently audible. They are kept rather than
	// dropped so that a sound that has finished is not stopped twice and so
	// that closing a tab can silence everything at once.
	playing []*SoundVoice

	// entered records the events that sound on entering the clip, so the frames
	// after the first do not sound them again. The timed events need no such
	// record: what is played for them is whether the playhead has crossed their
	// moment since the last frame, which is not the same question.
	entered map[string]bool

	// clip is the animation being played, so a change of clip is a change of
	// what fires and of what has already sounded.
	clip string
	// clipSeconds is how long the clip on screen takes to play, which is what
	// turns the timeline's "on leaving" marker into a moment it can happen at.
	clipSeconds float64
	lastTime    float64
	enabled     bool
	muted       bool
	closed      bool
	gain        float64
	// seed picks the variant, and is advanced by a fixed sequence rather than
	// a random source so that the same clip plays the same samples in the same
	// order every run, which is what makes two previews comparable.
	seed uint64
	// sounded counts how many times any event has started a sound, which is
	// what says whether a clip's footsteps all played or only the first step.
	sounded int
	mu      sync.Mutex
}

// SoundVoice is one sample playing. It is kept as a player plus the moment it
// runs out, because the player reports neither: an oto player has no length and
// no "finished" to ask about, so the length is taken from the samples and the
// voice dropped when it has passed.
type SoundVoice struct {
	player *oto.Player
	EndsAt time.Time
}

// NewSoundVoice wraps a playing player with the moment it runs out. The end
// is taken from the samples because an oto player reports neither length nor
// "finished" to ask about.
func NewSoundVoice(player *oto.Player, s *SoundSample) *SoundVoice {
	return &SoundVoice{player: player, EndsAt: time.Now().Add(SoundDuration(s))}
}

func (v *SoundVoice) Stop() {
	if v == nil || v.player == nil {
		return
	}
	v.player.PauseAndStopReading()
	v.player.Close()
	v.player = nil
}

// NewSoundPlayer builds a player for a model from the bundles the worker
// resolved and the timelines that fire them. Bundles is one entry per sound the
// model declares, and samples is what the worker decoded: a sound whose samples
// are all missing is left out here rather than complained about at play time,
// so the log says once.
func NewSoundPlayer(bundles map[string]SoundBundle, timelines []whe.FxTimeline, samples map[string]*SoundSample) *SoundPlayer {
	p := &SoundPlayer{
		events:    map[string]SoundBundle{},
		clips:     map[string]whe.FxTimeline{},
		preloaded: map[string]*SoundSample{},
		decoded:   map[string]*SoundSample{},
		entered:   map[string]bool{},
		gain:      1,
		// A player that exists is one that plays. Leaving this to be turned on
		// from outside means anything that built one and forgot to say so got
		// silence, which is the harder of the two mistakes to notice.
		enabled: true,
	}
	for name, b := range bundles {
		if len(b.Files) == 0 {
			continue
		}
		p.events[strings.ToLower(name)] = b
	}
	for path, s := range samples {
		p.preloaded[path] = s
	}
	for _, tl := range timelines {
		// A timeline names the clips it blends, and its own chunk is usually
		// one of them, so both are registered under both.
		p.clips[strings.ToLower(tl.Name)] = tl
		for _, anim := range tl.Animations {
			if _, seen := p.clips[strings.ToLower(anim)]; !seen {
				p.clips[strings.ToLower(anim)] = tl
			}
		}
	}
	return p
}

// HasSounds reports whether anything at all was found, which is what the panel
// uses to decide whether to show the sound row at all.
func (p *SoundPlayer) HasSounds() bool {
	return p != nil && len(p.events) > 0 && len(p.clips) > 0
}

// Decoded returns every sample that has been decoded, keyed by virtual path,
// so the effects worker can be handed them instead of decoding the same .fda
// files a second time.
//
// preloaded is the set to report, not decoded: preloaded is what the worker
// decoded up front, while decoded only fills as sounds are first played, so
// reporting the latter would leave every sample nobody has heard yet to be
// decoded again.
func (p *SoundPlayer) Decoded() map[string]*SoundSample {
	if p == nil || len(p.preloaded) == 0 {
		return nil
	}
	out := make(map[string]*SoundSample, len(p.preloaded))
	for k, v := range p.preloaded {
		out[k] = v
	}
	return out
}

// Sounded reports how many sounds have been started, which is what says whether
// a clip played everything its timeline asks for.
func (p *SoundPlayer) Sounded() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sounded
}

// IsSoundEvent reports whether the player declares a sound for the named
// timeline event. Names are matched in lower case, which is how the event set
// is keyed, because the timelines name clips as the artist typed them.
func (p *SoundPlayer) IsSoundEvent(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.events[strings.ToLower(name)]
	return ok
}

// SoundNames lists the events the model declares a sound for, sorted, for the
// panel that shows what is wired up.
func (p *SoundPlayer) SoundNames() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.events))
	for k := range p.events {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// SetEnabled turns the sounds on or off without losing what has been decoded,
// so switching them back on is instant.
func (p *SoundPlayer) SetEnabled(on bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.enabled == on {
		return
	}
	p.enabled = on
	if !on {
		p.stopAllLocked()
		p.entered = map[string]bool{}
	}
}

// SetMuted silences what is playing and what will start, without stopping the
// simulation: a unit that is still walking still gets to a silent sound at the
// moment it would have played one.
func (p *SoundPlayer) SetMuted(muted bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.muted == muted {
		return
	}
	p.muted = muted
	if muted {
		p.stopAllLocked()
	}
}

// SetGain sets the volume every sound plays at, as a fraction of full.
func (p *SoundPlayer) SetGain(g float64) {
	if p == nil {
		return
	}
	if g < 0 {
		g = 0
	}
	if g > 1 {
		g = 1
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gain = g
}

// Advance is called once per frame with the clip on screen and how many
// seconds into it the playhead is. It starts whatever the clip's timeline
// says should start between lastTime and now, which is what makes a sound fire
// at its moment rather than when the frame happens to land on it.
//
// The test is a crossing rather than a flag per event name, and it has to be: a
// walk cycle names sound_footfall twice, once for each step, and remembering
// that it had played would leave the second step silent. A sound fires as the
// playhead moves past the point the timeline gave it, which lets the same name
// sound as many times as the clip asks for.
func (p *SoundPlayer) Advance(anim string, t float64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	clip := strings.ToLower(anim)
	prev := p.lastTime
	// A different clip, or the playhead moved backwards — a rewind or a loop —
	// is a fresh pass over the clip, so what has already sounded does not apply.
	// The entry events sound again with it, which is what the game's does.
	if clip != p.clip || t+1e-6 < prev {
		p.clip = clip
		p.entered = map[string]bool{}
		// Just short of where the playhead is, so an event written at the
		// very start of the clip — which is where a footfall or a shot is
		// usually written — counts as crossed on the first frame. Starting
		// the previous position exactly at t would step over it.
		prev = t - 1e-9
	}
	p.lastTime = t
	tl, ok := p.clips[clip]
	if !ok {
		return
	}
	p.expireLocked()
	for _, ev := range tl.Events {
		name := strings.ToLower(ev.Name)
		at := EventTime(ev.Time, p.clipSeconds)
		if at < 0 {
			// "On entering the clip": once per pass, and not repeated by the
			// frames that follow.
			if p.entered[name] {
				continue
			}
			p.entered[name] = true
			if p.enabled && !p.muted {
				p.startLocked(name)
			}
			continue
		}
		if at > t || prev >= at {
			continue
		}
		if p.enabled && !p.muted {
			p.startLocked(name)
		}
	}
}

// EventTime resolves the timeline's entry and exit sentinels while
// leaving ordinary event times as seconds into the clip.
//
// DATAMTON writes -1 for on entering and the largest float for on leaving;
// neither is a moment, so the first stays a marker for the caller and the
// second becomes the end of the clip once its length is known. A clip whose
// length was never given has no moment to put it at, so it never plays.
func EventTime(t, clipSeconds float64) float64 {
	if t < 0 {
		return -1
	}
	if t > 1e30 {
		if clipSeconds <= 0 {
			return math.Inf(1)
		}
		return clipSeconds
	}
	return t
}

// SetClipSeconds tells the player how long the clip on screen takes to play,
// which is what the "on leaving" events are resolved against. It is set before
// Advance, so it is stored outright rather than matched against the clip the
// last Advance saw. Zero, the default, leaves them unheard rather than firing
// them at some invented point.
func (p *SoundPlayer) SetClipSeconds(seconds float64) {
	if p == nil || seconds < 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clipSeconds = seconds
}

// startLocked plays one sound event. It picks a variant from the folder the path
// named, the way the game does, so a footstep is not the same sample twice in a
// row: the game varies it and a preview that did not would sound wrong in a way
// that is hard to see but easy to hear.
func (p *SoundPlayer) startLocked(event string) {
	b, ok := p.events[event]
	if !ok || len(b.Files) == 0 {
		// Not a sound, or a sound with nothing behind it: a timeline fires
		// effects and terrain events on the same moments it fires audio, and
		// only the audio is counted as played.
		return
	}
	p.sounded++
	pick := b.Files[0]
	if len(b.Files) > 1 {
		pick = b.Files[p.nextIndex(len(b.Files))]
	}
	sample := p.sampleLocked(pick)
	if sample == nil {
		return
	}
	dur := SoundDuration(sample)
	if dur <= 0 {
		return
	}
	if v := p.play(sample, dur); v != nil {
		p.playing = append(p.playing, v)
	}
}

// nextIndex is the preview's own sequence of choices. A fixed sequence rather
// than a random one, so that a footstep cycle a preview can be compared against
// another run of the preview is repeatable: the same clip plays the same
// samples in the same order.
func (p *SoundPlayer) nextIndex(n int) int {
	if n <= 0 {
		return 0
	}
	p.seed = p.seed*6364136223846793005 + 1442695040888963407
	// The high bits of a linear congruential step are the better mixed, which
	// is what keeps a run of footsteps from picking the same index twice.
	idx := int((p.seed >> 33) % uint64(n))
	return idx
}

// play hands one sample to a fresh player. Each sound gets its own, which is
// what lets two overlap rather than the second cutting off the first.
//
// The sample arrives already in the context's format, converted on the worker
// that decoded it: see fxSoundDecodeForPlayback. Nothing is converted here,
// because a conversion costs tens of milliseconds and this runs on the frame
// that starts the sound. The format is checked anyway, because a sample that
// reached the player in the wrong one would play at the wrong speed, and
// silently is the worst way for that to happen.
func (p *SoundPlayer) play(sample *SoundSample, dur time.Duration) *SoundVoice {
	ctx, err := audioplayer.Context()
	if err != nil {
		return nil
	}
	pcm := sample.PCM
	sr, ch := sample.Info.SampleRate, sample.Info.Channels
	if sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		// Only reachable for a sample built outside the decode path, which is
		// the tests and nothing else. Converting here rather than playing it
		// wrong is the lesser evil, and it costs nothing in normal use.
		var err error
		pcm, err = fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			log.Printf("fx sound: convert %d Hz/%dch: %v", sr, ch, err)
			return nil
		}
		sr, ch = audioplayer.SampleRate, audioplayer.Channels
	}
	cb := audioplayer.NewChunkBuffer(fda.PCM16Bytes(pcm), sr, ch)
	pl := ctx.NewPlayer(audioplayer.NewChunkSource(cb))
	pl.SetVolume(p.gain)
	pl.Play()
	// How long it runs for is taken from the samples rather than from the
	// player, which has no way to say: the voice is dropped once that long has
	// passed and the player released.
	return &SoundVoice{player: pl, EndsAt: time.Now().Add(dur)}
}

// sampleLocked decodes a sample the first time it is asked for.
func (p *SoundPlayer) sampleLocked(path string) *SoundSample {
	if s, ok := p.decoded[path]; ok {
		return s
	}
	// Decoding reads the tree and the disk, which is not something to do while
	// the frame is waiting; the load happens on the worker with the effects
	// instead and this only finds what is already there.
	s := p.preloaded[path]
	if s == nil {
		return nil
	}
	p.decoded[path] = s
	return s
}

func SoundDuration(s *SoundSample) time.Duration {
	if s == nil || s.Info == nil || s.Info.SampleRate <= 0 {
		return 0
	}
	frames := len(s.PCM) / s.Info.Channels
	if frames <= 0 {
		return 0
	}
	return time.Duration(float64(frames) / float64(s.Info.SampleRate) * float64(time.Second))
}

// expireLocked drops the voices that have finished, which is what keeps a long
// clip from accumulating a thousand players.
func (p *SoundPlayer) expireLocked() {
	now := time.Now()
	kept := p.playing[:0]
	for _, v := range p.playing {
		if now.Before(v.EndsAt) {
			kept = append(kept, v)
			continue
		}
		v.Stop()
	}
	p.playing = kept
}

func (p *SoundPlayer) stopAllLocked() {
	for _, v := range p.playing {
		v.Stop()
	}
	p.playing = nil
}

// StopAll silences everything without turning the sounds off, which is what
// closing a tab and starting a different animation both need.
func (p *SoundPlayer) StopAll() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopAllLocked()
	p.entered = map[string]bool{}
}

// Close releases the player and forgets everything it decoded, so a closed tab
// leaves nothing playing behind it.
func (p *SoundPlayer) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	p.stopAllLocked()
	p.decoded = nil
	p.preloaded = nil
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
