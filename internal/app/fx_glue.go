package app

import (
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"fmt"
	"github.com/fogleman/fauxgl"
	"image"
	"log"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fxStartLoad begins resolving the model's effects, once, reporting whether it
// started anything. The path is the one the model itself was read from, which
// is how its blueprint is found.
func FxStartLoad(ui *uiState, v *modelViewer) bool {
	if v == nil || v.Model == nil || ui.ModTree == nil || v.ModelFile == "" {
		return false
	}
	if v.FxCh != nil || len(v.FxEffects) > 0 || v.FxErr != "" {
		return false
	}
	ch := make(chan fxLoadResult, 1)
	v.FxCh = ch
	model := v.Model
	file := v.ModelFile
	tree := ui.ModTree
	// Whatever the sound worker decoded already is handed over, because both
	// read the same .whe and the samples would otherwise be decoded twice.
	// The maps of the last load ride along for the same reason, and the sound
	// half is left to the worker that is usually decoding these very samples
	// while the FX button is being pressed.
	opts := fxLoadOpts{
		Decoded: v.FxSound.Decoded(),
		Prev:    v.FxTextures,
		Audio:   v.FxSound == nil && v.FxSoundCh == nil,
	}
	go func() {
		ch <- loadModelEffects(tree, model, file, opts)
	}()
	return true
}

// fxStartSoundLoad begins reading and decoding the model's declared sounds,
// once, reporting whether it started anything.
//
// Sounds are loaded on their own rather than riding in with the effects
// because of what they cost and when it is paid. Decoding an .fda and
// converting it into the audio context's format takes milliseconds per second
// of audio, and a walk cycle names several samples, so a sound that is decoded
// when it first plays is a sound that arrives late. Nothing about that work
// needs the FX toggle to have been pressed, and a reader may never press it, so
// the sounds are read as soon as the model is on screen and the effects stay
// behind their button.
func FxStartSoundLoad(ui *uiState, v *modelViewer) bool {
	if v == nil || ui.ModTree == nil || v.ModelFile == "" {
		return false
	}
	if v.FxSoundCh != nil || v.FxSound != nil {
		return false
	}
	ch := make(chan fxLoadResult, 1)
	v.FxSoundCh = ch
	file := v.ModelFile
	tree := ui.ModTree
	go func() {
		ch <- loadModelSounds(tree, file)
	}()
	return true
}

// fxReload restarts both workers for a viewer with the live .whe document in
// hand: a value edited in the Events section re-resolves into the viewport
// without saving the file and without reopening the model. What is on screen
// keeps showing until the workers answer, so a reload swaps rather than
// blanks. The sound player is closed and dropped because its bundles came
// from the old definitions — fxSoundTick and the labels nil-check it, so the
// gap is silence rather than a crash — and its decoded samples are handed to
// the new loads, so only paths that changed pay for a decode.
func FxReload(ui *uiState, v *modelViewer, doc *whe.Document) {
	if v == nil || v.Model == nil || ui.ModTree == nil || v.ModelFile == "" {
		return
	}
	model, file, tree := v.Model, v.ModelFile, ui.ModTree
	decoded := v.FxSound.Decoded()
	// The sound worker is started first, so the effects worker below can see
	// that somebody is already decoding this document's samples and leave the
	// sounds to it. Starting the effects worker first would hide the worker
	// that is about to start and have both of them pay for the same decode.
	if v.FxSoundCh == nil {
		if v.FxSound != nil {
			v.FxSound.Close()
			v.FxSound = nil
		}
		ch := make(chan fxLoadResult, 1)
		v.FxSoundCh = ch
		go func() {
			ch <- loadModelSoundsWhe(tree, file, doc, decoded)
		}()
	}
	if v.FxCh == nil {
		ch := make(chan fxLoadResult, 1)
		v.FxCh = ch
		opts := fxLoadOpts{
			Decoded: decoded,
			Prev:    v.FxTextures,
			Audio:   v.FxSound == nil && v.FxSoundCh == nil,
		}
		go func() {
			ch <- loadModelEffectsWhe(tree, model, file, opts, doc)
		}()
	}
	log.Printf("3D preview: fx reload for an edited .whe (live document=%v)", doc != nil)
}

// loadModelEffects resolves a model's events, parses every effect they name
// and decodes the sprite maps they use. It is meant to run on a worker
// goroutine: resolving a blueprint can mean a walk of the whole mod, which is
// far too slow to do while the window is waiting for a frame.
//
// A model with no events is not an error worth reporting — most of them have
// none — but an event that names a file which cannot be read is logged and
// skipped, so one broken effect does not cost the reader the other eleven.
// loadModelEffects is loadModelEffectsWhe with no live document: the .whe on
// disk is the whole story, which is every caller except an Events edit.
func loadModelEffects(tree *modtree.ModTree, model *WhmModel, modelFile string, opts fxLoadOpts) fxLoadResult {
	return loadModelEffectsWhe(tree, model, modelFile, opts, nil)
}

// loadModelEffectsWhe resolves the model's effects with the live .whe
// document in hand when there is one: its definitions and its timelines are
// what the effects are built from, so values edited but not yet saved are the
// ones the preview shows.
func loadModelEffectsWhe(tree *modtree.ModTree, model *WhmModel, modelFile string, opts fxLoadOpts, liveWhe *whe.Document) fxLoadResult {
	res := fxLoadResult{
		Textures:     map[string]*image.RGBA{},
		Sounds:       fx.NewSoundCache(),
		SoundBundles: map[string]fx.SoundBundle{},
	}
	// Samples the sound-only worker already decoded are handed over rather
	// than decoded again. Both workers read the same .whe, so without this the
	// effect of the FX button would be to pay for every sample of the model a
	// second time.
	if len(opts.Decoded) > 0 {
		for k, s := range opts.Decoded {
			res.Sounds.Put(k, s)
		}
	}
	if tree == nil || model == nil {
		return res
	}
	loadStart := time.Now()
	bindings, _, err := fx.ResolveEventsForModel(tree, modelFile)
	if err != nil {
		res.Err = err
		return res
	}
	tEvents := time.Since(loadStart)
	if liveWhe != nil {
		bindings = fx.WheBindingsOverlay(bindings, liveWhe)
	}
	roots := materialSearchRoots(tree)
	seen := map[string]*image.RGBA{}
	// The maps the last load already decoded are the starting point, so a
	// reload after an edit does not decode the same seventy sprites again.
	for path, img := range opts.Prev {
		if img != nil {
			seen[path] = img
		}
	}
	// The .whe's timelines say which events belong to which animation, so a
	// model that has them only plays the ones its clip on screen fires. A
	// model whose effects all live in its blueprint has none, and then every
	// event it named plays at once, which is how this worked before.
	// Read and parse the .whe once and index it twice: which clip fires each
	// event, and when in that clip it fires. Reading it twice would parse a
	// 40 KB file of chunks a second time for an answer already in hand.
	var timelines []whe.FxTimeline
	if liveWhe != nil {
		timelines = liveWhe.Timelines()
	}
	if len(timelines) == 0 {
		// No document, or one whose parse produced no timelines: the file's
		// own copy is the fallback rather than an empty set, because empty
		// timelines mean "every event plays at once".
		timelines = fx.ReadWheTimelines(tree, modelFile)
	}
	durations := make(map[string]float64, len(model.Animations))
	for i := range model.Animations {
		a := &model.Animations[i]
		duration := float64(a.Duration)
		if duration <= 0 && a.NumFrames > 0 {
			duration = float64(a.NumFrames) / animFramesPerSecond(a)
		}
		if duration > 0 {
			durations[strings.ToLower(a.Name)] = duration
		}
	}
	tTimelines := time.Since(loadStart)
	var tLua, tTex time.Duration
	var tRead, tParse, tSim time.Duration
	fires, _ := fxTimelineFires(timelines, durations)
	tlAnims := map[string][]string{}
	for ev, list := range fires {
		anims := make([]string, 0, len(list))
		for _, f := range list {
			anims = append(anims, f.Anim)
		}
		tlAnims[ev] = anims
	}
	tlTimes := fxTimelineFireTimes(timelines, durations)
	timed := len(tlAnims) > 0
	// One effect file serves several events — the monolith's 189 events name
	// just 75 files — so each is read and parsed once and every event builds
	// its own simulation from that shared parse. An FxFile is plain data and
	// NewFxSim reads it without writing to it, so the parsed copy is safe to
	// hand to as many simulations as name it.
	parsedFx := map[string]*fx.FxFile{}
	for _, b := range bindings {
		fxPath, ok := b.Event.First("fx")
		if !ok || fxPath == "" {
			continue
		}
		luaStart := time.Now()
		key := strings.ToLower(fxLuaPath(fxPath))
		file := parsedFx[key]
		if file == nil {
			readStart := time.Now()
			data, err := readFxLuaFile(tree, roots, fxPath)
			tRead += time.Since(readStart)
			if err != nil {
				log.Printf("3D preview: effect %q: %v", fxPath, err)
				continue
			}
			parseStart := time.Now()
			file, err = fx.ParseFxLua(data)
			tParse += time.Since(parseStart)
			if err != nil {
				log.Printf("3D preview: effect %q: %v", fxPath, err)
				continue
			}
			parsedFx[key] = file
		}
		simStart := time.Now()
		sim := fx.NewFxSim(file)
		tSim += time.Since(simStart)
		tLua += time.Since(luaStart)
		marker := fxEventMarker(model, b.Event, len(res.Effects))
		other := fxBeamOtherMarker(model, b.Event)
		e := fxOverlayEffect{
			Sim:          sim,
			Label:        b.Event.Name,
			FxName:       fxPath,
			Marker:       marker,
			Ground:       fxIsGroundEffect(b.Event, fxPath),
			HasOther:     other >= 0,
			OtherMarker:  other,
			Spans:        fxEventSpansDistance(b.Event),
			AlignToOther: fxEventAlignsToOther(b.Event),
			// An event no clip fires is not one the preview plays on its
			// own: it waits for whatever triggers it in the game, which is
			// a hit landing or a unit dying rather than the model standing
			// there. Only a model with no timelines plays everything.
			Timed:      timed,
			Animations: tlAnims[strings.ToLower(b.Event.Name)],
			FireTimes:  tlTimes[strings.ToLower(b.Event.Name)],
		}
		// fx_scale applies uniformly to this event's local effect geometry
		// on top of the viewer's own scale; the marker origin itself stays put.
		if s, ok := b.Event.First("fx_scale"); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
				e.EventScale = f
			}
		}
		rest := model.RestPose()
		e.Frame = fxFallbackFrame
		if marker >= 0 {
			if world, ok := model.MarkerWorld(marker, rest, true); ok {
				e.Origin = world
			}
		} else {
			// No marker, or one this model does not carry: the middle
			// of the model is where an effect plays by default.
			e.Origin, _ = model.ModelCenter()
		}
		if e.HasOther {
			if world, ok := model.MarkerWorld(other, rest, true); ok {
				e.Far = world
			}
		}
		if axis, ok := model.MarkerBeamAxis(marker, rest, true); ok {
			e.Axis = axis
		} else if sim != nil && sim.Axis.Length() > 1e-6 {
			e.Axis = e.FxMarkerVector(sim.Axis)
		}
		if sim != nil && sim.NoRotate {
			// No_Rotate overrides marker orientation, not the FX file's
			// required Y-up to model Z-up axis conversion.
			e.Frame = fxFallbackFrame
		} else if x, y, z, ok := model.MarkerFrameAxes(marker, rest, true); ok {
			e.Frame = [3]fauxgl.Vector{x, y, z}
		}
		if right, up, ok := model.MarkerBasis(marker, rest, true); ok {
			e.Right = right
			e.Up = up
		}
		if e.AlignToOther && sim != nil && !sim.NoRotate {
			e.Frame = fxFrameToward(e.Frame, e.Far.Sub(e.Origin))
		}
		res.Effects = append(res.Effects, e)
		// Every map the effect names, decoded once however many nodes
		// point at it. The sprites are asked for rather than the file
		// scanned, so only the maps the running effect actually uses
		// are paid for.
		for _, sprite := range sim.Sprites() {
			path := fxTexturePath(sprite.Texture)
			if path == "" {
				continue
			}
			if _, done := seen[path]; done {
				continue
			}
			texStart := time.Now()
			img, err := decodeFxTexture(tree, roots, path)
			tTex += time.Since(texStart)
			if err != nil {
				log.Printf("3D preview: sprite texture %q: %v", path, err)
				seen[path] = nil
				continue
			}
			seen[path] = img
		}
		// A beam's map is named by the node, not by anything on show, so it
		// is read straight off the parsed file.
		// A beam's and a ring's map are named by the node, not by anything
		// on show, so they are read straight off the parsed file. Both are
		// asked with a unit span, which is all the sampler needs to name
		// the file they will draw from.
		want := map[string]bool{}
		for _, beam := range sim.Beams(fauxgl.Vector{}, fauxgl.Vector{X: 1}, false) {
			want[fxTexturePath(beam.Texture)] = true
		}
		for _, ring := range sim.Rings(fauxgl.Vector{}, fauxgl.Vector{X: 1},
			fauxgl.Vector{Y: 1}, fauxgl.Vector{}, fauxgl.Vector{Z: 1}) {
			want[fxTexturePath(ring.Texture)] = true
		}
		for path := range want {
			if path == "" {
				continue
			}
			if _, done := seen[path]; done {
				continue
			}
			texStart := time.Now()
			img, err := decodeFxTexture(tree, roots, path)
			tTex += time.Since(texStart)
			if err != nil {
				log.Printf("3D preview: effect texture %q: %v", path, err)
				seen[path] = nil
				continue
			}
			seen[path] = img
		}
	}
	for path, img := range seen {
		if img != nil {
			res.Textures[path] = img
		}
	}
	tEffects := time.Since(loadStart)
	// The timelines are this loader's own answer either way: the scrub bar
	// reads them whatever becomes of the sounds, and the sound worker's own
	// result carries the same ones out of the same file.
	res.Timelines = timelines
	if opts.Audio {
		fxLoadWheAudio(tree, modelFile, liveWhe, &res)
	}
	log.Printf("3D preview: fx load in %v: events %v, timelines %v, effects %v (read %v, parse %v, sim %v, textures %v), sounds %v; %d bindings, %d effects, %d textures from %d fx files",
		time.Since(loadStart), tEvents, tTimelines-tEvents,
		tEffects-tTimelines, tRead, tParse, tSim, tTex, time.Since(loadStart)-tEffects,
		len(bindings), len(res.Effects), len(res.Textures), len(parsedFx))
	return res
}

// fxLoadWheAudio fills res with the .whe's declared sounds: from the live
// document when one is being edited, from the tree's copy of the file
// otherwise.
func fxLoadWheAudio(tree *modtree.ModTree, modelFile string, liveWhe *whe.Document, res *fxLoadResult) {
	if liveWhe != nil {
		if data, err := liveWhe.MarshalBinary(); err == nil && len(data) > 0 {
			loadWheSoundsFrom(tree, data, liveWhe.Timelines(), res)
			return
		}
	}
	loadWheSounds(tree, modelFile, res)
}

// loadModelSounds resolves and decodes a model's declared sounds on their own,
// without touching its effects. It is the sound half of loadModelEffects,
// split out so a model on screen has its audio ready without the FX toggle
// having been pressed and without resolving a blueprint walk it may never
// need.
// loadModelSounds is loadModelSoundsWhe with no live document, which is
// every caller except an Events edit.
func loadModelSounds(tree *modtree.ModTree, modelFile string) fxLoadResult {
	return loadModelSoundsWhe(tree, modelFile, nil, nil)
}

// loadModelSoundsWhe resolves and decodes the declared sounds, from the live
// .whe document when one is being edited so an unsaved sound path is what
// gets decoded. decoded is what the outgoing player already holds: those
// samples are seeded into the new cache rather than decoded again, so a
// reload only pays for the paths that changed.
func loadModelSoundsWhe(tree *modtree.ModTree, modelFile string, liveWhe *whe.Document, decoded map[string]*fx.SoundSample) fxLoadResult {
	res := fxLoadResult{
		Sounds:       fx.NewSoundCache(),
		SoundBundles: map[string]fx.SoundBundle{},
	}
	if tree == nil {
		return res
	}
	if len(decoded) > 0 {
		for k, s := range decoded {
			res.Sounds.Put(k, s)
		}
	}
	fxLoadWheAudio(tree, modelFile, liveWhe, &res)
	return res
}

// loadWheSounds reads the sound events out of a .whe and decodes every sample
// they name, so the frame that plays one only has to hand the samples over.
//
// Only the variants of the sounds the model's own timelines actually fire are
// decoded. A .whe declares sounds for states the preview never plays — the ones
// a shot impact or a death would trigger — and decoding an .fda costs far more
// than drawing it, so the rest are resolved and left on disk.
func loadWheSounds(tree *modtree.ModTree, modelFile string, res *fxLoadResult) {
	data := readFxWheFile(tree, modelFile)
	if len(data) == 0 {
		return
	}
	// The bytes are already in hand, so the timelines come out of them: reading
	// the .whe a second time for this would parse a file of chunks again for
	// an answer the line above just produced.
	loadWheSoundsFrom(tree, data, whe.ParseFxTimelines(data), res)
}

// loadWheSoundsFrom is loadWheSounds with the .whe bytes and the timelines
// handed in: the live document being edited marshals its own, so an unsaved
// sound edit is what gets decoded and wired.
func loadWheSoundsFrom(tree *modtree.ModTree, data []byte, timelines []whe.FxTimeline, res *fxLoadResult) {
	// The timelines come first: they are the scrub bar's own answer and do not
	// depend on there being any sound to play. Returning below on an empty
	// sound list without them would hand the caller a set that clears what the
	// other worker has already put there.
	res.Timelines = timelines
	// One worker per sound that names a folder of variants, all of them
	// finished before this returns: the samples are handed to the player as one
	// set once it does.
	var fills sync.WaitGroup
	declared := whe.ParseWheSounds(data)
	if len(declared) == 0 {
		return
	}
	soundStart := time.Now()
	// Which events the timelines on screen can fire at all, so the samples for
	// the rest are never decoded.
	played := map[string]bool{}
	for _, tl := range timelines {
		for _, ev := range tl.Events {
			played[strings.ToLower(ev.Name)] = true
		}
	}
	roots := materialSearchRoots(tree)
	// Declared but never fired is normal rather than a failure: the sound
	// belongs to a state this model is not in.
	type soundJob struct {
		name string
		ev   whe.FxSoundEvent
		b    fx.SoundBundle
		err  error
	}
	var jobs []*soundJob
	for name, ev := range declared {
		if ev.Path == "" || len(played) == 0 && !fxSoundAlwaysPlays(name) {
			continue
		}
		if len(played) > 0 && !played[name] {
			continue
		}
		jobs = append(jobs, &soundJob{name: name, ev: ev})
	}
	// Resolving a bundle and decoding its first sample were paid one sound
	// after another — sixteen of them in a row is seconds of a worker waiting
	// on itself, and it is the whole of what this function costs. No two
	// sounds depend on each other, so they are resolved and decoded at once,
	// a few at a time per core, and only the wiring below runs serially,
	// because that is what writes the maps the caller is about to read.
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxInt(2, runtime.GOMAXPROCS(0)))
	for _, j := range jobs {
		wg.Add(1)
		go func(j *soundJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			b, ok := fx.ResolveSound(tree, roots, j.ev.Path)
			if !ok || len(b.Files) == 0 {
				j.err = whe.SoundMissing(j.ev.Path)
				return
			}
			j.b = b
			first := b.Files[0]
			if _, done := res.Sounds.Get(first); done {
				return
			}
			s, err := fxSoundDecodeForPlayback(tree, roots, first)
			if err != nil {
				j.err = err
				return
			}
			res.Sounds.Put(first, s)
		}(j)
	}
	wg.Wait()
	for _, j := range jobs {
		if j.err != nil {
			log.Printf("3D preview: sound %q: %v", j.ev.Path, j.err)
			continue
		}
		res.SoundBundles[j.name] = j.b
		if len(j.b.Files) > 1 {
			fills.Add(1)
			go func(b fx.SoundBundle) {
				defer fills.Done()
				fxSoundFillVariants(tree, roots, b, res.Sounds)
			}(j.b)
		}
	}
	// The fills have to be finished before the result is handed on. They write
	// into the same map the caller is about to read and then keep, and a map
	// written by one goroutine while another reads it is not a map with a
	// missing key in it but a crash. The first sample of each sound is already
	// decoded by now, so this waits for the variants of a footfall folder and
	// not for the model's audio as a whole.
	waitStart := time.Now()
	fills.Wait()
	variantWait := time.Since(waitStart)
	log.Printf("3D preview: sounds in %v (resolve + first %v, variants %v): %d bundles",
		time.Since(soundStart), time.Since(soundStart)-variantWait, variantWait,
		len(res.SoundBundles))
}

// fxSoundFillVariants decodes the rest of a sound's samples alongside the
// others, so a footfall folder of seven files is ready by the time the second
// step lands rather than one step at a time.
//
// A variant the outgoing player already holds is left alone: a reload hands
// the old samples over for exactly this reason, and decoding them again was
// seconds of work repeated on every edit to the .whe.
//
// Every fill for a model runs at once rather than one after another: the files
// are independent, they are decoded off the frame that asked for them, and a
// model with three sound folders would otherwise decode them in sequence for
// no gain. They write into one cache, so it is a fx.SoundCache rather than a
// bare map and the caller's Wait is what makes handing it over safe.
func fxSoundFillVariants(tree *modtree.ModTree, roots []string, b fx.SoundBundle, into *fx.SoundCache) {
	for _, f := range b.Files[1:] {
		if _, done := into.Get(f); done {
			continue
		}
		s, err := fxSoundDecodeForPlayback(tree, roots, f)
		if err != nil {
			continue
		}
		into.Put(f, s)
	}
}

// fxSoundDecodeForPlayback decodes one sample and converts it into the audio
// context's format straight away.
//
// The conversion belongs here rather than at the moment a sound plays, and the
// reason is measured rather than stylistic: a windowed sinc conversion of a
// second of audio costs about 28 ms, against 2.4 ms for the linear
// interpolation it replaced. On the frame that starts a sound that is a visible
// hitch, and a walk cycle starts a sound on every step, so it would be paid
// over and over for a conversion whose answer never changes.
//
// Doing it on the worker instead means the frame only ever hands finished PCM to
// the player, and the same sample converted once serves every step that uses it.
func fxSoundDecodeForPlayback(tree *modtree.ModTree, roots []string, p string) (*fx.SoundSample, error) {
	info, err := fx.SoundRead(tree, roots, p)
	if err != nil {
		return nil, err
	}
	pcm := wavPCM16(info.PCMData)
	if real := fxSoundDeclaredRate(info.SampleRate); real != info.SampleRate {
		// Reading the sample at its real rate rather than the declared one is
		// what makes it the right length and the right pitch; the samples
		// themselves are untouched.
		log.Printf("3D preview: sound %s declares %d Hz, played at %d Hz",
			filepath.Base(p), info.SampleRate, real)
		info = &wav.Info{SampleRate: real, Channels: info.Channels, PCMData: info.PCMData}
	}
	// The context is created once for the process and its format is fixed, so
	// the target is known here without asking it.
	if sr, ch := info.SampleRate, info.Channels; sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		converted, err := fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			return nil, fmt.Errorf("convert %s: %w", p, err)
		}
		pcm = converted
		info = &wav.Info{SampleRate: audioplayer.SampleRate, Channels: audioplayer.Channels, PCMData: fda.PCM16Bytes(pcm)}
	}
	return &fx.SoundSample{Info: info, PCM: pcm}, nil
}

// decodeFxTexture decodes a sprite map. Effects keep theirs as .tga or .dds
// under art/fx, which the image decoder already reads; what is missing is a
// plain RGBA copy, since that is what the overlay samples.
func decodeFxTexture(tree *modtree.ModTree, roots []string, name string) (*image.RGBA, error) {
	candidates := fxTextureCandidates(name)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no texture named")
	}
	var lastErr error
	for _, p := range candidates {
		data, format, err := readFxTextureBytes(tree, roots, p)
		if err != nil {
			lastErr = err
			continue
		}
		img, _, err := decodeImageData(data, format)
		if err != nil {
			return nil, err
		}
		return toRGBA(img), nil
	}
	return nil, lastErr
}
