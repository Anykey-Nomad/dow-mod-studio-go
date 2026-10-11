package main

import (
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whe"
	"math"
	"testing"
)

// The sound events of a .whe are read the same way the effect events are, out
// of the DATAEVNT chunks, so what is worth pinning down here is the part that
// is only about sound: the payload shape, what a path means, and when a sound
// starts.

// whe.ParseSoundEvent reads the payload of one sound chunk: an int32, the key
// "sound", and the path. These are the three shapes necron_pariah.whe carries.
func TestParseSoundEventPayload(t *testing.T) {
	payload := func(parts ...any) []byte {
		return buildSoundPayload(parts...)
	}
	tests := []struct {
		name    string
		payload []byte
		want    string
		ok      bool
	}{
		{"full", payload(int32(1), "sound", `races\necron\shared\melee_damage`), `races\necron\shared\melee_damage`, true},
		{"case in key", payload(int32(1), "Sound", `races\necrons\shared\roar`), `races\necrons\shared\roar`, true},
		// Four zero bytes and nothing else: the model declares the event and
		// leaves it silent, which is a real answer rather than a bad parse.
		{"silent", []byte{0, 0, 0, 0}, "", true},
		{"empty", nil, "", true},
		// The name said sound but the payload says otherwise. Guessing here
		// would play the wrong sample, so it is left unclaimed.
		{"wrong key", payload(int32(1), "effect", `races\necrons\shared\roar`), "", false},
		{"truncated", []byte{1, 0, 0, 0, 5, 0}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := whe.ParseSoundEvent("sound_fire", tc.payload)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (path %q)", ok, tc.ok, ev.Path)
			}
			if ok && ev.Path != tc.want {
				t.Fatalf("path = %q, want %q", ev.Path, tc.want)
			}
		})
	}
}

// A chunk that is not a sound belongs to the effects, which are read by their
// own name elsewhere. This must not claim it.
func TestParseSoundEventIgnoresEffectChunks(t *testing.T) {
	p := buildSoundPayload(int32(1), "sound", `races\necrons\shared\roar`)
	if _, ok := whe.ParseSoundEvent("effect_fire", p); ok {
		t.Fatal("an effect event was claimed as a sound")
	}
}

// Paths in a .whe are relative to data/sound, so the prefix has to be added
// before a lookup can succeed, and a path that already names it must not be
// given it twice.
func TestFxSoundPathCleans(t *testing.T) {
	tests := []struct{ in, want string }{
		{`races\necron\shared\melee_damage`, `sound\races\necron\shared\melee_damage`},
		{`races/necrons/shared/roar`, `sound\races\necrons\shared\roar`},
		{`sound\races\necrons\shared\roar`, `sound\races\necrons\shared\roar`},
		{`Data:\sound\races\necrons\shared\roar`, `sound\races\necrons\shared\roar`},
		{`data\sound\races\necrons\shared\roar`, `sound\races\necrons\shared\roar`},
		{`  sound\races\necrons\shared\roar  `, `sound\races\necrons\shared\roar`},
		{"", ""},
	}
	for _, tc := range tests {
		if got := whe.SoundPathCleans(tc.in); got != tc.want {
			t.Errorf("whe.SoundPathCleans(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// %terrain% has no ground under a model in a preview, so it is resolved away
// into the folders it can mean rather than left in a path that names nothing.
func TestFxSoundPathResolvedTerrain(t *testing.T) {
	got := whe.SoundPathResolved(`sound\races\necron\footstep\%terrain%`)
	if len(got) != len(whe.TerrainTokens) {
		t.Fatalf("got %d candidates, want %d: %q", len(got), len(whe.TerrainTokens), got)
	}
	for _, c := range got {
		if want := `sound\races\necron\footstep\` + whe.TerrainTokens[0]; c == want {
			return
		}
	}
	t.Fatalf("no candidate for the first ground in %q", got)
}

// A path with no extension is a folder of variants; one that names a file is
// that file alone.
func TestFxSoundVariantCandidates(t *testing.T) {
	folder := whe.SoundVariantCandidates(`sound\races\necrons\shared\melee_damage`)
	if len(folder) != 2 {
		t.Fatalf("a folder gave %d candidates: %q", len(folder), folder)
	}
	file := whe.SoundVariantCandidates(`sound\races\necrons\shared\roar\loud.fda`)
	if len(file) != 1 || file[0] != `sound\races\necrons\shared\roar\loud.fda` {
		t.Fatalf("a file gave %q", file)
	}
}

// The game writes -1 for on entering and the largest float for on leaving, and
// everything between as seconds into the clip.
func TestFxSoundEventTime(t *testing.T) {
	tests := []struct {
		in     float64
		length float64
		want   float64
	}{
		{-1, 10, -1},
		{math.MaxFloat32, 10, 10},         // on leaving, with the clip's length known
		{math.MaxFloat32, 0, math.Inf(1)}, // on leaving, with no length yet
		{0, 10, 0},
		{0.35, 10, 0.35},
		{1, 10, 1},
		// A .whe whose moments are written as seconds, which is how
		// necron_monolith's add_on_3 writes its crystals at 43 to 51 of a
		// 58.5 second clip: read as a fraction of one, every one of them
		// would fall off the end of the clip.
		{43.15, 58.566666, 43.15},
		{51.18, 58.566666, 51.18},
	}
	for _, tc := range tests {
		if got := fx.EventTime(tc.in, tc.length); got != tc.want {
			t.Errorf("fx.EventTime(%v, %v) = %v, want %v", tc.in, tc.length, got, tc.want)
		}
	}
}

// A sound starts when the playhead crosses the moment the timeline gave it, and
// the same name may sound as many times as the clip asks for: a walk cycle
// names sound_footfall twice, once per step, and a flag per event name would
// leave the second step silent.
func TestFxSoundAdvanceFiresOnCrossing(t *testing.T) {
	p := fx.NewSoundPlayer(
		map[string]fx.SoundBundle{
			"sound_footfall": {Path: "footstep", Files: []string{"a.fda", "b.fda"}},
			"sound_die":      {Path: "die", Files: []string{"die.fda"}},
		},
		[]whe.FxTimeline{{
			Name:       "move",
			Animations: []string{"walk_cycle"},
			Events: []whe.FxTimelineEvent{
				{Name: "effect_dust", Time: 0.25}, // not a sound: must not count
				{Name: "sound_footfall", Time: 0.2},
				{Name: "SOUND_FOOTFALL", Time: 0.6}, // the same event, a second time
				{Name: "sound_die", Time: -1},
			},
		}},
		map[string]*fx.SoundSample{}, // nothing decodable, so nothing reaches the device
	)
	if !p.HasSounds() {
		t.Fatal("HasSounds is false for a player with sounds and a clip")
	}

	// The clip is entered at its start, so the event written at 0.2 has not
	// been reached yet and only the on-entry one has sounded.
	p.Advance("walk_cycle", 0)
	if got := p.Sounded(); got != 1 {
		t.Fatalf("after entering: %d sounds, want 1 (the on-entry event)", got)
	}

	p.Advance("walk_cycle", 0.5)
	if got := p.Sounded(); got != 2 {
		t.Fatalf("after the first step: %d sounds, want 2", got)
	}

	// Standing still must not re-fire anything.
	p.Advance("walk_cycle", 0.5)
	if got := p.Sounded(); got != 2 {
		t.Fatalf("a still playhead fired again: %d sounds, want 2", got)
	}

	// Crossing the second step sounds the same name again.
	p.Advance("walk_cycle", 0.9)
	if got := p.Sounded(); got != 3 {
		t.Fatalf("after the second step: %d sounds, want 3", got)
	}

	// A different clip starts a fresh pass, on-entry events included.
	p.Advance("move", 0)
	if got := p.Sounded(); got != 4 {
		t.Fatalf("after switching clip: %d sounds, want 4", got)
	}

	// Playing the clip out crosses both footsteps: the same name twice, which
	// is the case a flag per name would get wrong.
	p.Advance("move", 0.9)
	if got := p.Sounded(); got != 6 {
		t.Fatalf("after playing the clip out: %d sounds, want 6", got)
	}

	// A rewind is a fresh pass: the on-entry event sounds again, and a moment
	// the playhead has not reached yet stays silent.
	p.Advance("move", 0.1)
	if got := p.Sounded(); got != 7 {
		t.Fatalf("after a rewind: %d sounds, want 7", got)
	}
}

// Turning the sounds off stops what is playing and stops new sounds from
// starting, and turning them back on must not need a reload.
func TestFxSoundSetEnabled(t *testing.T) {
	p := fx.NewSoundPlayer(
		map[string]fx.SoundBundle{"sound_footfall": {Files: []string{"a.fda"}}},
		[]whe.FxTimeline{{Name: "walk", Events: []whe.FxTimelineEvent{{Name: "sound_footfall", Time: 0.5}}}},
		map[string]*fx.SoundSample{},
	)
	p.Advance("walk", 0.5)
	if got := p.Sounded(); got != 1 {
		t.Fatalf("before muting: %d sounds, want 1", got)
	}
	p.SetEnabled(false)
	p.Advance("walk", 0.9)
	if got := p.Sounded(); got != 1 {
		t.Fatalf("while off: %d sounds, want 1", got)
	}
	p.SetEnabled(true)
	p.Advance("walk", 0.95)
	if got := p.Sounded(); got != 1 {
		t.Fatalf("a re-enabled player lost its bundles: %d sounds, want 1", got)
	}
}

// A model that declares no sounds is silent, not broken.
func TestFxSoundPlayerEmpty(t *testing.T) {
	p := fx.NewSoundPlayer(nil, nil, nil)
	if p.HasSounds() {
		t.Fatal("an empty player claims sounds")
	}
	p.Advance("walk", 0.5) // must not panic
	p.StopAll()
	p.Close()
}

// buildSoundPayload writes the payload of a DATAEVNT chunk in the shape the
// sound events use: an int32, then the key, then the path.
func buildSoundPayload(parts ...any) []byte {
	var b []byte
	for _, p := range parts {
		switch v := p.(type) {
		case int32:
			b = append(b, chunky.I32(v)...)
		case string:
			b = append(b, chunky.I32(int32(len(v)))...)
			b = append(b, v...)
		default:
			panic("buildSoundPayload: unsupported part")
		}
	}
	return b
}
