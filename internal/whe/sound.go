package whe

import (
	"dow-mod-studio/internal/chunky"
	"fmt"
	"strings"
)

// A unit's sounds sit in the same .whe its effects do, and are wired up the
// same way: the motion timeline names an event at a moment in the clip, and
// that event is a DATAEVNT chunk named sound_<something>. What differs is what
// the chunk holds — not an effect but a path.
//
// The measured payload of necron_pariah.whe, one sound chunk per event:
//
//	name  "sound_fire"
//	payload 01 00 00 00 | 05 00 00 00 "sound" | 03 00 00 00 "races\necron\troop_weapons\fire_small_extra_voltage"
//
// so an int32, then a string, then the path. The chunks that hold nothing at
// all are four bytes of zero, which is an event with no sound on this unit —
// sound_die_thrown, sound_sync_death_3 and a good many others — so an empty
// payload is a real answer and not a parse that went wrong.
//
// The path has no extension and does not name a file: it names a folder of
// variants under data/sound, the way the Relic Audio Tool files a sound that
// should pick a different sample each time it plays. races\necrons\shared\
// melee_hit_blade_electric is such a folder, holding _default.rat beside
// blade_elec_01.fda through blade_elec_07.fda.

// FxSoundEvent is one sound event of a model: the name a timeline fires and the
// path under data/sound it resolves to. An empty Path is an event the model
// declares but leaves silent.
type FxSoundEvent struct {
	Name string
	Path string
}

// ParseWheSounds reads every sound event a .whe carries, keyed by the event
// name in lower case. A file that carries none is not an error: plenty of
// models play all their sound from the game, and an empty result means "there
// is nothing to play".
//
// The name is returned as written, because the timeline that fires it compares
// case-insensitively already and matching on the lower case here is what makes
// "sound_footfall" from one file and "Sound_Footfall" from another the same
// event.
func ParseWheSounds(data []byte) map[string]FxSoundEvent {
	out := map[string]FxSoundEvent{}
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	var walk func(cs []*chunky.Chunk)
	walk = func(cs []*chunky.Chunk) {
		for _, c := range cs {
			if c.TypeID == "DATAEVNT" {
				name := strings.TrimRight(c.Name, "\x00")
				if ev, ok := ParseSoundEvent(name, c.Payload); ok {
					out[strings.ToLower(name)] = ev
				}
			}
			walk(c.Children)
		}
	}
	walk(chunks)
	return out
}

// ParseSoundEvent reads one DATAEVNT payload. It reports false for a chunk that
// is not a sound, which is every effect event in the same folder: the effects
// are read by their own name elsewhere and this must not claim them.
func ParseSoundEvent(name string, p []byte) (FxSoundEvent, bool) {
	if !strings.HasPrefix(strings.ToLower(name), "sound") {
		return FxSoundEvent{}, false
	}
	ev := FxSoundEvent{Name: name}
	// Four zero bytes and nothing else: the event exists, and is silent.
	if len(p) <= 4 {
		return ev, true
	}
	r := &chunky.Reader{Data: p}
	r.I32()
	key := r.Str()
	path := r.Str()
	if r.Err != nil || key == "" || path == "" {
		// A chunk that does not read as the shape above is not one to guess
		// at: the name said sound, but the payload said otherwise, and a wrong
		// path would play the wrong sample rather than none.
		return FxSoundEvent{}, false
	}
	if !strings.EqualFold(key, "sound") {
		return FxSoundEvent{}, false
	}
	ev.Path = path
	return ev, true
}

// SoundEventPath returns the sound a model plays for an event name, if it plays
// one at all. The event name is matched case-insensitively, which is how a
// timeline refers to it.
func SoundEventPath(sounds map[string]FxSoundEvent, event string) (string, bool) {
	ev, ok := sounds[strings.ToLower(strings.TrimSpace(event))]
	if !ok || ev.Path == "" {
		return "", false
	}
	return ev.Path, true
}

// SoundPathCleans turns a path a .whe writes into the path a sample is looked
// for under.
//
// The paths are relative to data/sound, not to data: necron_pariah.whe writes
// races\necron\shared\melee_damage, which is data/sound/races/necron/shared/
// melee_damage and not anything under data/races. The prefix is added here so
// that every lookup below speaks in the same terms, and a path that already
// names the folder is not given it twice.
func SoundPathCleans(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "/", `\`)
	for _, prefix := range []string{`data:\`, `Data:\`, `data\`, `Data\`} {
		if len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
			p = p[len(prefix):]
			break
		}
	}
	p = strings.TrimLeft(p, `\`)
	if p == "" {
		return ""
	}
	for _, prefix := range []string{`sound\`, `Sound\`} {
		if len(p) >= len(prefix) && strings.EqualFold(p[:len(prefix)], prefix) {
			return `sound\` + strings.TrimRight(p[len(prefix):], `\`)
		}
	}
	return `sound\` + strings.TrimRight(p, `\`)
}

// TerrainTokens are the ground names %terrain% stands for. The preview
// has no ground under a model, so the first is what a preview can honestly
// offer: the token is resolved away rather than left in a path that names no
// folder.
var TerrainTokens = []string{"dirt", "sand", "grass", "rock", "water"}

// SoundPathResolved turns a sound path into the folders and files it can mean,
// best first.
//
// The three cases are what the shipped files show. A path that names a file
// directly is taken as it is. A path that ends in %terrain% has one folder per
// ground, and each of those is tried. Anything else is a folder of variants,
// which is the common case: the tools file a repeating sound as a directory of
// numbered samples with a _default.rat beside them, and pick one at random.
func SoundPathResolved(p string) []string {
	p = SoundPathCleans(p)
	if p == "" {
		return nil
	}
	if i := strings.Index(strings.ToLower(p), "%terrain%"); i >= 0 {
		out := make([]string, 0, len(TerrainTokens))
		for _, t := range TerrainTokens {
			out = append(out, p[:i]+t+p[i+len("%terrain%"):])
		}
		return out
	}
	return []string{p}
}

// SoundVariantCandidates lists the files a resolved sound path may be, in the
// order they are worth trying. A folder of variants yields its _default.rat
// marker plus the samples themselves, which is what lets the caller choose a
// different one per play; a path that names one file yields that file alone.
func SoundVariantCandidates(p string) []string {
	p = SoundPathCleans(p)
	if p == "" {
		return nil
	}
	if ext := strings.ToLower(LastExt(p)); ext != "" {
		return []string{p}
	}
	out := []string{p}
	// The game reads a sound path with no extension as the folder of variants
	// beside it, but a build that shipped the sample flat puts it under the
	// name itself.
	out = append(out, p+".fda")
	return out
}

// LastExt returns the extension of the last element of a path, without the dot,
// or "" when it has none. The separator is what tells a dot inside a folder
// name from a dot that ends a file name, so the search starts after it: on a
// Windows path the separators themselves are backslashes, and taking the text
// after the last one would report a folder called melee_damage as carrying the
// extension "melee_damage".
func LastExt(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i+1:]
	}
	return ""
}

// SoundMissing says why a sound could not be found, for the log line the
// preview writes rather than for anything shown on screen: a missing sample is
// worth saying once and then leaving alone.
func SoundMissing(p string) error {
	return fmt.Errorf("sound %s not found under data/sound", p)
}
