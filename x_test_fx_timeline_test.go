package main

import (
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/whe"
	"strings"
	"testing"
	"time"
)

// A timeline's time is the whole point of reading one: an event written at 0.6
// of a clip starts there, not when the clip begins. These cover the reading of
// that time and the gating of the simulation on it.

func TestTimelineMapKeepsTheMomentEachEventFires(t *testing.T) {
	tls := []whe.FxTimeline{{
		Name:       "land",
		Animations: []string{"landing"},
		Events: []whe.FxTimelineEvent{
			{Name: "land_dust", Time: 0.25},
			{Name: "land_flash", Time: 0.75},
		},
	}}
	times := fxTimelineFireTimes(tls)
	if got := times["land_dust"]["landing"]; got != 0.25 {
		t.Errorf("land_dust fires at %g, want 0.25", got)
	}
	if got := times["land_flash"]["landing"]; got != 0.75 {
		t.Errorf("land_flash fires at %g, want 0.75", got)
	}
	// The timeline's own chunk name is a clip it drives as well.
	if got := times["land_dust"]["land"]; got != 0.25 {
		t.Errorf("under the chunk name land_dust fires at %g, want 0.25", got)
	}
	// The old view of the same data must not lose the clip it belongs to.
	anims := fxTimelineMap(tls)
	if got := anims["land_dust"]; len(got) != 2 {
		t.Errorf("land_dust -> %v, want both the chunk name and the clip", got)
	}
}

func TestAnEventBeforeThePlayheadHasNotFiredYet(t *testing.T) {
	sim := fx.NewFxSim(&fx.FxFile{})
	v := &modelViewer{
		model:          &WhmModel{},
		anim:           &WhmAnimation{Name: "landing", NumFrames: 20, Duration: 1},
		animIndex:      0,
		playing:        true,
		time:           0.30, // a third of the way in; the clip is one second long
		fxClipPrev:     0.20,
		fxAnim:         "landing",
		showFX:         true,
		fxEffects:      []fxOverlayEffect{},
		fxTextures:     nil,
		fxScale:        1,
		fxOverlay:      nil,
		fxLast:         time.Now(),
		fxClipPrevAnim: "landing",
	}
	e := fxOverlayEffect{
		Sim:        sim,
		Label:      "late",
		Timed:      true,
		Animations: []string{"landing"},
		FireTimes:  map[string]float64{"landing": 0.6},
	}
	v.fxEffects = []fxOverlayEffect{e}
	got := v.fxStepOnClip([]fxOverlayEffect{e}, "landing", 0.1)
	if len(got) != 0 {
		t.Errorf("an event due at 0.6 drew at 0.30; %d effects passed", len(got))
	}

	// Past its moment, it both draws and runs.
	v.time = 0.70
	v.fxClipPrev = 0.65
	got = v.fxStepOnClip([]fxOverlayEffect{e}, "landing", 0.05)
	if len(got) != 1 {
		t.Fatalf("an event due at 0.6 did not draw at 0.70: %d effects", len(got))
	}
}

func TestAnEntryEventFiresAsTheClipBegins(t *testing.T) {
	sim := fx.NewFxSim(&fx.FxFile{})
	e := fxOverlayEffect{
		Sim:        sim,
		Label:      "gates",
		Timed:      true,
		Animations: []string{"all_stale"},
		// -1 is what the game writes for "on entering the clip".
		FireTimes: map[string]float64{"all_stale": -1},
	}
	v := &modelViewer{
		model: &WhmModel{},
		anim:  &WhmAnimation{Name: "all_stale", NumFrames: 20, Duration: 1},
		// The playhead is still at zero: the first frame of the clip.
		time:           0,
		fxClipPrevAnim: "all_stale",
		showFX:         true,
		fxScale:        1,
	}
	got := v.fxStepOnClip([]fxOverlayEffect{e}, "all_stale", 0.05)
	if len(got) != 1 {
		t.Fatalf("an entry event did not fire on the first frame: %d effects", len(got))
	}
}

func TestALoopingClipStartsItsEventsAgain(t *testing.T) {
	sim := fx.NewFxSim(&fx.FxFile{})
	e := fxOverlayEffect{
		Sim:        sim,
		Timed:      true,
		Animations: []string{"idle"},
		FireTimes:  map[string]float64{"idle": 0.5},
	}
	v := &modelViewer{
		model: &WhmModel{},
		anim:  &WhmAnimation{Name: "idle", NumFrames: 20, Duration: 1}, playing: true,
		time:           0.40,
		fxClipPrev:     0.38,
		fxClipPrevAnim: "idle",
		showFX:         true,
		fxScale:        1,
	} // Before the moment: nothing. This frame is a loop that is about to
	// happen, so it starts at 0.40 rather than at the end.
	v.time = 0.40
	v.fxClipPrev = 0.38
	if got := v.fxStepOnClip([]fxOverlayEffect{e}, "idle", 0.02); len(got) != 0 {
		t.Fatalf("an event due at 0.5 drew at 0.40")
	}
	// The clip loops round: the playhead is back near the start, so the event
	// must be reachable again rather than waiting out another whole clip.
	v.time = 0.02
	got := v.fxStepOnClip([]fxOverlayEffect{e}, "idle", 0.02)
	if len(got) != 0 {
		t.Fatalf("after a loop the event drew before its moment: %d", len(got))
	}
	v.time = 0.55
	got = v.fxStepOnClip([]fxOverlayEffect{e}, "idle", 0.02)
	if len(got) != 1 {
		t.Fatalf("after a loop the event did not fire again at 0.55: %d", len(got))
	}
}

// A clip no timeline names draws nothing. It once fell back to playing every
// effect the model has instead, on the theory that a blank viewport is worse
// than a busy one; a monolith carries 189 effects and drawing all of them at
// once hung the GPU until it dropped the device. Cheap and survivable beats
// complete and fatal.
func TestAClipNoTimelineNamesDrawsNothingRatherThanEverything(t *testing.T) {
	effects := []fxOverlayEffect{
		{Label: "a", Timed: true, Animations: []string{"all_stale"}},
		{Label: "b", Timed: true, Animations: []string{"land"}},
	}
	v := &modelViewer{fxEffects: effects, showFX: true, fxScale: 1}
	var played []fxOverlayEffect
	for i := range v.fxEffects {
		if v.fxEffects[i].fxPlaysFor("add_on_1a") {
			played = append(played, v.fxEffects[i])
		}
	}
	if len(played) != 0 {
		t.Fatalf("%d effects play on a clip no timeline drives; each one is "+
			"submitted to the GPU every frame", len(played))
	}
	// A model with no timelines at all still plays everything, which is what an
	// empty set has always meant and is a different case: there is nothing to
	// be faithful to.
	for i := range effects {
		untimed := effects[i]
		untimed.Timed = false
		if !untimed.fxPlaysFor("add_on_1a") {
			t.Errorf("an untimed model must play %s on any clip", untimed.Label)
		}
	}
}

// The .whe is the only file that says when anything fires. Losing it turns
// "at 0.6 of the clip" into "immediately", silently, so the lookup has to
// survive a model whose virtual path is not where the game keeps the file.

func TestWheIsFoundByNameWhenTheModelPathMisses(t *testing.T) {
	tree := sgaTree(t)
	const real = "Data/art/ebps/races/npc/troops/necron_monolith.whm"
	if len(fx.ReadWheData(tree, real)) == 0 {
		t.Skip("no .whe beside this model")
	}
	// The same model reached by a path that does not exist in the tree: the
	// name is right, the folder is not.
	wrong := "Data/art/ebps/races/npc/troops/../../../somewhere/necron_monolith.whm"
	got := fx.ReadWheData(tree, wrong)
	if len(got) == 0 {
		t.Fatal("a .whe reachable by name was not found; every event loses its timing")
	}
	if len(fx.ReadWheTimelines(tree, wrong)) == 0 {
		t.Error("timelines were not read through the name fallback")
	}
}

func TestTheMonolithPortalVortexFiresOnItsOwnClip(t *testing.T) {
	tree := sgaTree(t)
	const model = "Data/art/ebps/races/npc/troops/necron_monolith.whm"
	tls := fx.ReadWheTimelines(tree, model)
	if len(tls) == 0 {
		t.Skip("no timelines for this model")
	}
	times := fxTimelineFireTimes(tls)
	anims := fxTimelineMap(tls)
	var on []string
	for anim := range times["portal_vortex"] {
		on = append(on, anim)
	}
	if len(on) == 0 {
		t.Fatal("portal_vortex fires on no clip at all")
	}
	t.Logf("portal_vortex fires on %v (clips %v)", on, anims["portal_vortex"])
	// It is an entry event, so it must be at the start of the clip: the portal
	// is already spinning when the monolith is idle.
	if got := times["portal_vortex"][on[0]]; got > 0.5 {
		t.Errorf("portal_vortex fires at %g of the clip, want it at the start", got)
	}
}

// The event a reader is most likely to look for must be reachable from the
// clip the game's own idle plays, or the portal vortex is simply missing.
func TestPortalVortexIsNotLostToATimelineNamingGap(t *testing.T) {
	tree := sgaTree(t)
	const model = "Data/art/ebps/races/npc/troops/necron_monolith.whm"
	bindings, _, err := fx.ResolveEventsForModel(tree, model)
	if err != nil {
		t.Skipf("no bindings: %v", err)
	}
	tls := fx.ReadWheTimelines(tree, model)
	times := fxTimelineFireTimes(tls)
	anims := fxTimelineMap(tls)
	var fxPath string
	for _, b := range bindings {
		if strings.EqualFold(b.Event.Name, "portal_vortex") {
			fxPath, _ = b.Event.First("fx")
		}
	}
	if fxPath == "" {
		t.Skip("this build has no portal_vortex event")
	}
	e := fxOverlayEffect{
		Timed:      true,
		Animations: anims["portal_vortex"],
		FireTimes:  times["portal_vortex"],
	}
	if len(e.Animations) == 0 {
		t.Fatalf("portal_vortex (%s) is wired to no clip, so it can never draw", fxPath)
	}
	// Whatever clip it is wired to, it must be drawable: a clip with no time is
	// an entry event, not a missing one.
	if at, ok := e.FireTimes[strings.ToLower(e.Animations[0])]; !ok {
		t.Errorf("portal_vortex has no fire time on %q", e.Animations[0])
	} else if at > 0.5 {
		t.Errorf("portal_vortex fires at %g of %q, too late for an idle glow", at, e.Animations[0])
	}
}

// fakeModelGpu is a hardware renderer that exists, which is all the "will the
// hardware path draw this frame" question asks of it.
type fakeModelGpu struct{}

func (fakeModelGpu) Render(*WhmModel, WhmPose, RenderOptions, map[string]WhmMaterial, func(string) ([]byte, bool)) (modelGpuFrame, error) {
	return nil, nil
}
func (fakeModelGpu) forget(*WhmModel)          {}
func (fakeModelGpu) geometryChanged(*WhmModel) {}
func (fakeModelGpu) release()                  {}

// One list, one answer: the reason the renderer acts on is the reason the FX
// step decides from, so neither can go quietly missing about a path that is
// off — a duplicated list here is how a frame ends up building a picture the
// hardware path never paints, or painting a picture that was never built.
func TestTheHardwarePathSaysWhyItWillNotDraw(t *testing.T) {
	ui := &uiState{modelGpuTried: true, modelGpu: fakeModelGpu{}}
	v := &modelViewer{model: &WhmModel{}}
	if got := ui.modelGpuUnavailable(v); got != "" {
		t.Errorf("available hardware path reported %q, want it available", got)
	}
	v.model = nil
	if got := ui.modelGpuUnavailable(v); got != "no model" {
		t.Errorf("modelless viewer reported %q, want %q", got, "no model")
	}
	v.model = &WhmModel{}
	ui.forceSoftRenderer = true
	if got := ui.modelGpuUnavailable(v); got != "software rendering enabled in settings" {
		t.Errorf("software preference reported %q, want the settings reason", got)
	}
	ui.forceSoftRenderer = false
	v.gpuBroken = true
	if got := ui.modelGpuUnavailable(v); got != "hardware path marked broken" {
		t.Errorf("broken hardware path reported %q, want the broken reason", got)
	}
	v.gpuBroken = false
	ui.modelGpu = nil
	if got := ui.modelGpuUnavailable(v); got != "no hardware renderer on this window" {
		t.Errorf("missing renderer reported %q, want the no-renderer reason", got)
	}
}

// The hardware frame draws the particles itself, so the software picture of
// them is built only when the software path is the one that will paint — and
// it has to come back the moment the hardware path stops being one. Both
// halves run off the reason the renderer acts on.
func TestFxAdvanceBuildsTheSoftwarePictureOnlyForTheSoftwarePath(t *testing.T) {
	ui := &uiState{modelGpuTried: true, modelGpu: fakeModelGpu{}}
	v := &modelViewer{
		model:   &WhmModel{},
		showFX:  true,
		fxScale: 1,
		fxEffects: []fxOverlayEffect{{
			Label: "glow",
			Sim: &fx.FxSim{Anchored: []fx.Visual{{
				Lifetime: 1,
				Radius:   []fx.FxKey{{V: 0.3}},
				SelfLit:  true,
			}}},
		}},
	}
	opts := RenderOptions{Width: 64, Height: 64, Fov: 45, Distance: 6}
	now := time.Now()

	if reason := ui.modelGpuUnavailable(v); reason != "" {
		t.Fatalf("test setup: hardware path unavailable (%s)", reason)
	}
	if !v.fxAdvance(now, 64, 64, opts, false) {
		t.Fatal("the effects were not advanced while the hardware path draws them")
	}
	if v.fxOverlay != nil {
		t.Error("built the software FX picture for a frame the hardware renderer draws")
	}
	if v.fxScene == nil {
		t.Error("no hardware FX scene: the particles would not be drawn at all")
	}

	// The hardware path breaks. The same reason now asks for the picture,
	// and the next frame must carry it or the fallback shows no particles.
	v.gpuBroken = true
	if reason := ui.modelGpuUnavailable(v); reason == "" {
		t.Fatal("a broken hardware path reported itself available")
	}
	if !v.fxAdvance(now.Add(time.Second), 64, 64, opts, true) {
		t.Fatal("the effects were not advanced after the hardware path broke")
	}
	if v.fxOverlay == nil {
		t.Error("no software FX picture after the hardware path broke; the fallback draws no particles")
	}
	if v.fxScene == nil {
		t.Error("no hardware FX scene after the fallback took over")
	}
}
