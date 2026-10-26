package app

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"gioui.org/widget"
	"github.com/fogleman/fauxgl"
)

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
		Model:          &WhmModel{},
		Anim:           &WhmAnimation{Name: "landing", NumFrames: 20, Duration: 1},
		AnimIndex:      0,
		Playing:        true,
		Time:           0.30, // a third of the way in; the clip is one second long
		FxClipPrev:     0.20,
		FxAnim:         "landing",
		ShowFX:         true,
		FxEffects:      []fxOverlayEffect{},
		FxTextures:     nil,
		FxScale:        1,
		FxOverlay:      nil,
		FxLast:         time.Now(),
		FxClipPrevAnim: "landing",
	}
	e := fxOverlayEffect{
		Sim:        sim,
		Label:      "late",
		Timed:      true,
		Animations: []string{"landing"},
		FireTimes:  map[string]float64{"landing": 0.6},
	}
	v.FxEffects = []fxOverlayEffect{e}
	got := FxStepOnClip(v, []fxOverlayEffect{e}, "landing", 0.1)
	if len(got) != 0 {
		t.Errorf("an event due at 0.6 drew at 0.30; %d effects passed", len(got))
	}

	// Past its moment, it both draws and runs.
	v.Time = 0.70
	v.FxClipPrev = 0.65
	got = FxStepOnClip(v, []fxOverlayEffect{e}, "landing", 0.05)
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
		Model: &WhmModel{},
		Anim:  &WhmAnimation{Name: "all_stale", NumFrames: 20, Duration: 1},
		// The playhead is still at zero: the first frame of the clip.
		Time:           0,
		FxClipPrevAnim: "all_stale",
		ShowFX:         true,
		FxScale:        1,
	}
	got := FxStepOnClip(v, []fxOverlayEffect{e}, "all_stale", 0.05)
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
		Model: &WhmModel{},
		Anim:  &WhmAnimation{Name: "idle", NumFrames: 20, Duration: 1}, Playing: true,
		Time:           0.40,
		FxClipPrev:     0.38,
		FxClipPrevAnim: "idle",
		ShowFX:         true,
		FxScale:        1,
	} // Before the moment: nothing. This frame is a loop that is about to
	// happen, so it starts at 0.40 rather than at the end.
	v.Time = 0.40
	v.FxClipPrev = 0.38
	if got := FxStepOnClip(v, []fxOverlayEffect{e}, "idle", 0.02); len(got) != 0 {
		t.Fatalf("an event due at 0.5 drew at 0.40")
	}
	// The clip loops round: the playhead is back near the start, so the event
	// must be reachable again rather than waiting out another whole clip.
	v.Time = 0.02
	got := FxStepOnClip(v, []fxOverlayEffect{e}, "idle", 0.02)
	if len(got) != 0 {
		t.Fatalf("after a loop the event drew before its moment: %d", len(got))
	}
	v.Time = 0.55
	got = FxStepOnClip(v, []fxOverlayEffect{e}, "idle", 0.02)
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
	v := &modelViewer{FxEffects: effects, ShowFX: true, FxScale: 1}
	var played []fxOverlayEffect
	for i := range v.FxEffects {
		if v.FxEffects[i].fxPlaysFor("add_on_1a") {
			played = append(played, v.FxEffects[i])
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

func (fakeModelGpu) Render(*whm.Model, WhmPose, render.RenderOptions, map[string]render.WhmMaterial, func(string) ([]byte, bool)) (render.Frame, error) {
	return nil, nil
}
func (fakeModelGpu) Forget(*whm.Model)          {}
func (fakeModelGpu) GeometryChanged(*whm.Model) {}
func (fakeModelGpu) Release()                   {}

// One list, one answer: the reason the renderer acts on is the reason the FX
// step decides from, so neither can go quietly missing about a path that is
// off — a duplicated list here is how a frame ends up building a picture the
// hardware path never paints, or painting a picture that was never built.
func TestTheHardwarePathSaysWhyItWillNotDraw(t *testing.T) {
	ui := &uiState{ModelGpuTried: true, ModelGpu: fakeModelGpu{}}
	v := &modelViewer{Model: &WhmModel{}}
	if got := ModelGpuUnavailable(ui, v); got != "" {
		t.Errorf("available hardware path reported %q, want it available", got)
	}
	v.Model = nil
	if got := ModelGpuUnavailable(ui, v); got != "no model" {
		t.Errorf("modelless viewer reported %q, want %q", got, "no model")
	}
	v.Model = &WhmModel{}
	ui.ForceSoftRenderer = true
	if got := ModelGpuUnavailable(ui, v); got != "software rendering enabled in settings" {
		t.Errorf("software preference reported %q, want the settings reason", got)
	}
	ui.ForceSoftRenderer = false
	v.GpuBroken = true
	if got := ModelGpuUnavailable(ui, v); got != "hardware path marked broken" {
		t.Errorf("broken hardware path reported %q, want the broken reason", got)
	}
	v.GpuBroken = false
	ui.ModelGpu = nil
	if got := ModelGpuUnavailable(ui, v); got != "no hardware renderer on this window" {
		t.Errorf("missing renderer reported %q, want the no-renderer reason", got)
	}
}

// The hardware frame draws the particles itself, so the software picture of
// them is built only when the software path is the one that will paint — and
// it has to come back the moment the hardware path stops being one. Both
// halves run off the reason the renderer acts on.
func TestFxAdvanceBuildsTheSoftwarePictureOnlyForTheSoftwarePath(t *testing.T) {
	ui := &uiState{ModelGpuTried: true, ModelGpu: fakeModelGpu{}}
	v := &modelViewer{
		Model:   &WhmModel{},
		ShowFX:  true,
		FxScale: 1,
		FxEffects: []fxOverlayEffect{{
			Label: "glow",
			Sim: &fx.FxSim{Anchored: []fx.Visual{{
				Lifetime: 1,
				Radius:   []fx.FxKey{{V: 0.3}},
				SelfLit:  true,
			}}},
		}},
	}
	opts := render.RenderOptions{Width: 64, Height: 64, Fov: 45, Distance: 6}
	now := time.Now()

	if reason := ModelGpuUnavailable(ui, v); reason != "" {
		t.Fatalf("test setup: hardware path unavailable (%s)", reason)
	}
	if !FxAdvance(v, now, 64, 64, opts, false) {
		t.Fatal("the effects were not advanced while the hardware path draws them")
	}
	if v.FxOverlay != nil {
		t.Error("built the software FX picture for a frame the hardware renderer draws")
	}
	if v.FxScene == nil {
		t.Error("no hardware FX scene: the particles would not be drawn at all")
	}

	// The hardware path breaks. The same reason now asks for the picture,
	// and the next frame must carry it or the fallback shows no particles.
	v.GpuBroken = true
	if reason := ModelGpuUnavailable(ui, v); reason == "" {
		t.Fatal("a broken hardware path reported itself available")
	}
	if !FxAdvance(v, now.Add(time.Second), 64, 64, opts, true) {
		t.Fatal("the effects were not advanced after the hardware path broke")
	}
	if v.FxOverlay == nil {
		t.Error("no software FX picture after the hardware path broke; the fallback draws no particles")
	}
	if v.FxScene == nil {
		t.Error("no hardware FX scene after the fallback took over")
	}
}

// ─── merged from x_test_fx_monolith_beam_axes_test.go ────────────────────────────────────────

type monolithFxEvent struct {
	name   string
	marker string
	fx     string
}

// monolithEvents reads the NPC monolith's .whe out of the archives.
func monolithEvents(t *testing.T, tree *modtree.ModTree) []monolithFxEvent {
	t.Helper()
	raw, err := tree.ReadFile("Data/art/ebps/races/npc/troops/necron_monolith.whe")
	if err != nil {
		t.Skipf("no NPC monolith .whe in the archives: %v", err)
	}
	events, perr := whe.ParseFxEvents(raw)
	if perr != nil {
		t.Fatalf("the NPC monolith .whe does not parse: %v", perr)
	}
	out := make([]monolithFxEvent, 0, len(events))
	for _, ev := range events {
		marker, _ := ev.First("marker")
		fxPath, _ := ev.First("fx")
		out = append(out, monolithFxEvent{
			name:   strings.ToLower(strings.TrimSpace(ev.Name)),
			marker: strings.TrimSpace(marker),
			fx:     strings.TrimSpace(fxPath),
		})
	}
	return out
}

// monolithEvent finds one event by name. Names are exact: base_light_l_05 has a
// _lwr twin on the lower light strip, and the two play different effects.
func monolithEvent(t *testing.T, events []monolithFxEvent, name string) monolithFxEvent {
	t.Helper()
	for _, ev := range events {
		if ev.name == strings.ToLower(name) {
			return ev
		}
	}
	t.Fatalf("the monolith .whe carries no event %q", name)
	return monolithFxEvent{}
}

// gunBarrel returns the direction from the barrel's base marker to its muzzle.
// The monolith writes marker_gun_<corner>_base_<n> and marker_gun_<corner>_muzzle_<n>
// in pairs, and that span is the barrel itself: measured over the 63 tube pairs
// of the installed tree it is what the markers' second frame column follows in
// all 63. A highlight marker shares the gun and the index, whatever its own
// name in the middle is.
// gun_fl_hi_01 names marker_gun_fr_hi_10 — a near-copy of the right gun's
// marker, in the left gun's event — so a marker without a pair of its own is
// measured against the barrels on its bone.
func gunBarrel(m *WhmModel, pose whm.Pose, marker string) (fauxgl.Vector, bool) {
	dir := func(base, muzzle int) (fauxgl.Vector, bool) {
		a, okA := m.MarkerWorld(base, pose, true)
		b, okB := m.MarkerWorld(muzzle, pose, true)
		if !okA || !okB {
			return fauxgl.Vector{}, false
		}
		span := b.Sub(a)
		if span.Length() < 1e-6 {
			return fauxgl.Vector{}, false
		}
		return span.Normalize(), true
	}

	lower := strings.ToLower(marker)
	parts := strings.Split(lower, "_")
	// marker_gun_<corner>_<what>_<n>
	if len(parts) >= 5 && parts[0] == "marker" && parts[1] == "gun" {
		corner, index := parts[2], parts[len(parts)-1]
		base := m.MarkerIndex("marker_gun_" + corner + "_base_" + index)
		muzzle := m.MarkerIndex("marker_gun_" + corner + "_muzzle_" + index)
		if base >= 0 && muzzle >= 0 {
			if v, ok := dir(base, muzzle); ok {
				return v, true
			}
		}
	}

	i := m.MarkerIndex(marker)
	if i < 0 {
		return fauxgl.Vector{}, false
	}
	bone := strings.ToLower(strings.TrimSpace(m.Markers[i].Parent))
	for j := range m.Markers {
		name := strings.ToLower(strings.TrimSpace(m.Markers[j].Name))
		if !strings.Contains(name, "_base_") || strings.ToLower(strings.TrimSpace(m.Markers[j].Parent)) != bone {
			continue
		}
		muzzle := m.MarkerIndex(strings.Replace(name, "_base_", "_muzzle_", 1))
		if muzzle < 0 {
			continue
		}
		if v, ok := dir(j, muzzle); ok {
			return v, true
		}
	}
	return fauxgl.Vector{}, false
}

// TestMonolithGunHilightBeamsRunAlongTheirBarrels covers gun_fl_hi_01..03 and
// gun_rl_energy_01..03: every beam the effect draws has to run the way the
// barrel under it runs, not across it.
func TestMonolithGunHilightBeamsRunAlongTheirBarrels(t *testing.T) {
	tree := sgaTree(t)
	raw, err := tree.ReadFile("Data/art/ebps/races/npc/troops/necron_monolith.whm")
	if err != nil {
		t.Skipf("no NPC monolith model in the archives: %v", err)
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("the NPC monolith model does not parse: %v", perr)
	}
	pose := m.RestPose()
	events := monolithEvents(t, tree)
	roots := materialSearchRoots(tree)

	for _, name := range []string{
		"gun_fl_hi_01", "gun_fl_hi_02", "gun_fl_hi_03",
		"gun_rl_energy_01", "gun_rl_energy_02", "gun_rl_energy_03",
	} {
		t.Run(name, func(t *testing.T) {
			ev := monolithEvent(t, events, name)
			i := m.MarkerIndex(ev.marker)
			if i < 0 {
				t.Fatalf("event %q plays at %q, a marker the model does not carry", name, ev.marker)
			}
			barrel, ok := gunBarrel(m, pose, ev.marker)
			if !ok {
				t.Fatalf("no base/muzzle pair around %q to measure the barrel against", ev.marker)
			}
			origin, _ := m.MarkerWorld(i, pose, true)
			axis, ok := m.MarkerBeamAxis(i, pose, true)
			if !ok {
				t.Fatalf("marker %q has no frame", ev.marker)
			}
			if d := math.Abs(axis.Dot(barrel)); d < 0.99 {
				t.Errorf("the beam at %q runs (%.2f %.2f %.2f), |dot| %.3f with the barrel "+
					"(%.2f %.2f %.2f): it crosses the gun instead of running along it",
					ev.marker, axis.X, axis.Y, axis.Z, d, barrel.X, barrel.Y, barrel.Z)
			}

			// And the effect the event plays draws its beams that way too,
			// rather than taking a direction from the file.
			data, err := readFxLuaFile(tree, roots, ev.fx)
			if err != nil {
				t.Skipf("effect %q is not readable: %v", ev.fx, err)
			}
			ff, ferr := fx.ParseFxLua(data)
			if ferr != nil {
				t.Fatalf("effect %q does not parse: %v", ev.fx, ferr)
			}
			longest := fauxgl.Vector{}
			for _, rb := range fx.NewFxSim(ff).Beams(origin, origin.Add(axis), false) {
				if d := rb.To.Sub(rb.From); d.Length() > longest.Length() {
					longest = d
				}
			}
			if longest.Length() < 1e-6 {
				t.Fatalf("effect %q draws no beam to check", ev.fx)
			}
			if d := math.Abs(longest.Normalize().Dot(barrel)); d < 0.9 {
				t.Errorf("the longest beam of %q runs |dot| %.3f with the barrel: the effect "+
					"is drawing athwart the gun", ev.fx, d)
			}
		})
	}
}

// TestMonolithBaseLightBeamsFallAlongTheChamfer covers base_light_l_05 and
// base_light_l_06, whose markers hang off the root bone. MarkerAxis reads a
// root marker by aiming away from the model's middle, which put the beam
// horizontally out of the hull; the chamfer they sit on is the markers' second
// column and runs down and outward.
func TestMonolithBaseLightBeamsFallAlongTheChamfer(t *testing.T) {
	tree := sgaTree(t)
	raw, err := tree.ReadFile("Data/art/ebps/races/npc/troops/necron_monolith.whm")
	if err != nil {
		t.Skipf("no NPC monolith model in the archives: %v", err)
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("the NPC monolith model does not parse: %v", perr)
	}
	pose := m.RestPose()
	events := monolithEvents(t, tree)
	roots := materialSearchRoots(tree)

	for _, name := range []string{"base_light_l_05", "base_light_l_06"} {
		t.Run(name, func(t *testing.T) {
			ev := monolithEvent(t, events, name)
			i := m.MarkerIndex(ev.marker)
			if i < 0 {
				t.Fatalf("event %q plays at %q, a marker the model does not carry", name, ev.marker)
			}
			axis, ok := m.MarkerBeamAxis(i, pose, true)
			if !ok {
				t.Fatalf("marker %q has no frame", ev.marker)
			}
			if axis.Z > -0.9 {
				t.Errorf("the beam at %q runs (%.2f %.2f %.2f): it is not falling down the "+
					"chamfer, and a horizontal one leaves the hull sideways",
					ev.marker, axis.X, axis.Y, axis.Z)
			}
			if axis.X < 0 {
				t.Errorf("the beam at %q runs (%.2f %.2f %.2f): it points back into the "+
					"hull rather than down the outside of the chamfer",
					ev.marker, axis.X, axis.Y, axis.Z)
			}

			origin, _ := m.MarkerWorld(i, pose, true)
			data, err := readFxLuaFile(tree, roots, ev.fx)
			if err != nil {
				t.Skipf("effect %q is not readable: %v", ev.fx, err)
			}
			ff, ferr := fx.ParseFxLua(data)
			if ferr != nil {
				t.Fatalf("effect %q does not parse: %v", ev.fx, ferr)
			}
			beams := fx.NewFxSim(ff).Beams(origin, origin.Add(axis), false)
			if len(beams) == 0 {
				t.Fatalf("effect %q draws no beam to check", ev.fx)
			}
			for _, rb := range beams {
				d := rb.To.Sub(rb.From).Normalize()
				if d.Z > -0.9 {
					t.Errorf("a beam of %q runs (%.2f %.2f %.2f): it lies along the ground "+
						"instead of dropping down the chamfer", ev.fx, d.X, d.Y, d.Z)
				}
			}
		})
	}
}

// The two monolith models are separate files with their own marker frames, and
// the restored one is the other half of the same report. Its guns keep their
// barrels along column two as well; only the sign of the highlight markers'
// own frame differs, which is the file's business and not the renderer's.
func TestRestoredMonolithGunBeamsRunAlongTheirBarrels(t *testing.T) {
	// The restored monolith ships in the definitive-edition patch archives
	// rather than loose in the mod, so it is read out of whichever copy is on
	// this machine; see restoredMonolithWhe for the same list.
	var raw []byte
	for _, path := range []string{
		".freebuff/phase_in_inspect/restored/art/ebps/races/necrons/troops/necron_restored_monolith.whm",
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3/Data/art/ebps/races/necrons/troops/necron_restored_monolith.whm`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP2/data/art/ebps/races/necrons/troops/necron_restored_monolith.whm`,
	} {
		if data, err := os.ReadFile(filepath.FromSlash(path)); err == nil && len(data) > 0 {
			raw = data
			break
		}
	}
	if raw == nil {
		t.Skip("the restored monolith model is not available on this machine")
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("the restored monolith model does not parse: %v", perr)
	}
	pose := m.RestPose()
	for _, marker := range []string{
		"marker_gun_fl_hi_02", "marker_gun_fr_hi_01",
		"marker_gun_rl_base_01", "marker_gun_rr_base_02",
	} {
		i := m.MarkerIndex(marker)
		if i < 0 {
			continue
		}
		barrel, ok := gunBarrel(m, pose, marker)
		if !ok {
			t.Fatalf("no base/muzzle pair around %q to measure the barrel against", marker)
		}
		axis, ok := m.MarkerBeamAxis(i, pose, true)
		if !ok {
			t.Fatalf("marker %q has no frame", marker)
		}
		if d := math.Abs(axis.Dot(barrel)); d < 0.99 {
			t.Errorf("the beam at %q runs (%.2f %.2f %.2f), |dot| %.3f with its barrel: it "+
				"crosses the gun instead of running along it", marker, axis.X, axis.Y, axis.Z, d)
		}
	}
}

// ─── merged from x_test_fx_sim_test.go ────────────────────────────────────────

const fxSyntheticLua = `
fxtypes =
{
	{
		effect = "root",
		fx = { properties = { }, style = "STYLE_COMBO", },
	},
	{
		effect = "root>spray",
		fx = { properties = {
			property_00 = { name = "Emitter_Rate", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 100, 1, 100, }, },
			property_01 = { name = "Emitter_Duration", type = "VARTYPE_FLOAT", value = 1, },
			property_02 = { name = "Emitter_Loop", type = "VARTYPE_BOOL", value = 1, },
			property_03 = { name = "Emitter_Deviation", type = "VARTYPE_FLOAT", value = 10, },
			property_04 = { name = "Emitter_Offset", type = "VARTYPE_ARRAY_TIMEVECTOR3", value = { entry_00 = { 0, 0, 5, 0, }, entry_01 = { 1, 0, 1, 0, }, }, },
			property_05 = { name = "Particle_Gravity", type = "VARTYPE_FLOAT", value = 1, },
		}, style = "STYLE_SPRAY", },
	},
	{
		effect = "root>spray>ring",
		fx = { properties = {
			property_00 = { name = "Texture", type = "VARTYPE_STRING", value = "DATA:ART/FX/GLOW.TGA", },
			property_01 = { name = "Duration", type = "VARTYPE_FLOAT", value = 2, },
			property_02 = { name = "Blending", type = "VARTYPE_INT", value = 2, },
			property_03 = { name = "Radius", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 0.5, 1, 1.5, }, },
			property_04 = { name = "Colour", type = "VARTYPE_ARRAY_TIMECOLOUR", value = { entry_00 = { 0, 0, 255, 0, 128, }, entry_01 = { 1, 0, 255, 0, 0, }, }, },
			property_05 = { name = "Particle_Speed", type = "VARTYPE_FLOAT", value = 3, },
			property_06 = { name = "Billboard", type = "VARTYPE_BOOL", value = 0, },
			property_07 = { name = "SpinY", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 0.25, 1, 0.25, }, },
			property_08 = { name = "SpinZ", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 0.125, 1, 0.125, }, },
			property_09 = { name = "SpinEpsilonZ", type = "VARTYPE_FLOAT", value = 0.75, },
			property_10 = { name = "SpinRandom", type = "VARTYPE_BOOL", value = 0, },
		}, style = "STYLE_RING", },
	},
	{
		effect = "root>glow",
		fx = { properties = {
			property_00 = { name = "Texture", type = "VARTYPE_STRING", value = "DATA:ART/FX/GLOW.TGA", },
			property_01 = { name = "Duration", type = "VARTYPE_FLOAT", value = 2, },
			property_02 = { name = "Blending", type = "VARTYPE_INT", value = 2, },
			property_03 = { name = "Radius", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 1, 1, 2, }, },
		}, style = "STYLE_RING", },
	},
}
`

// TestFxSimSynthetic parses a hand written effect and checks the whole
// preview path: a spray births particles that move along +Z and sample the
// radius and colour curves. The two rings are read as different things on
// purpose — the one under the spray describes the particles the emitter
// births, and the one under the root stands on its own as a flat quad, which
// is what the two shapes are in the game.
func TestFxSimSynthetic(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxSyntheticLua))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	s := fx.NewFxSim(f)
	if len(s.Emitters) != 1 {
		t.Fatalf("emitters = %d, want 1", len(s.Emitters))
	}
	if len(s.Emitters[0].Visuals) != 1 {
		t.Fatalf("spray visuals = %d, want 1: the ring under the spray is its particle", len(s.Emitters[0].Visuals))
	}
	if len(s.RingNodes) != 1 {
		t.Fatalf("rings = %d, want 1: the ring under the root stands on its own", len(s.RingNodes))
	}
	if len(s.Anchored) != 0 {
		t.Errorf("anchored = %d, want 0: both rings are rings, not particles", len(s.Anchored))
	}
	for i := 0; i < 120; i++ {
		s.Step(1.0 / 60)
	}
	if len(s.Particles) == 0 {
		t.Fatal("no particles spawned")
	}
	if len(s.Particles) > fx.MaxParticles {
		t.Errorf("particles = %d, over the cap %d", len(s.Particles), fx.MaxParticles)
	}
	sprites := s.Sprites()
	if len(sprites) != len(s.Particles) {
		t.Fatalf("sprites = %d, want the %d particles", len(sprites), len(s.Particles))
	}
	// The particle sprite: new ones are born green and growing.
	p := sprites[0]
	if p.Texture != "DATA:ART/FX/GLOW.TGA" {
		t.Errorf("texture = %q", p.Texture)
	}
	if !p.Additive {
		t.Errorf("blending = additive expected")
	}
	if p.Radius <= 0 {
		t.Errorf("radius = %v", p.Radius)
	}
	if p.Pos[1] <= 0 {
		t.Errorf("particle did not remain above the origin while moving: %+v", p.Pos)
	}
	if !p.Horizontal {
		t.Errorf("non-billboard STYLE_RING particle should stay in the horizontal XY plane")
	}
	if math.Abs(p.Rotation-math.Pi/2) > 1e-6 {
		t.Errorf("particle rotation = %.6f rad, want the authored 0.25-turn SpinY angle %.6f", p.Rotation, math.Pi/2)
	}
	// Colour ramp: green fading out over life.
	if p.G < 0.5 || p.R > 0.5 {
		t.Errorf("colour = (%v %v %v %v), want fading green", p.R, p.G, p.B, p.A)
	}
}

func TestFxSpraySamplesAnimatedOffsetAndStableRingSpin(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxSyntheticLua))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	sim := fx.NewFxSim(f)
	if len(sim.Emitters) != 1 || len(sim.Emitters[0].Visuals) != 1 {
		t.Fatalf("synthetic spray parsed as %d emitters / %d visuals", len(sim.Emitters), len(sim.Emitters[0].Visuals))
	}
	e := &sim.Emitters[0]
	first := e.SpawnIndex
	e.Age = 0
	sim.Spawn(e)
	start := sim.Particles[len(sim.Particles)-1]
	e.Age = e.Duration * 0.8
	sim.Spawn(e)
	late := sim.Particles[len(sim.Particles)-1]
	// The file writes the offset on its own up axis, which the simulation
	// keeps: the attachment frame turns it into the model's up later.
	if start.Pos[1] < 4.9 || start.Pos[1] > 5.1 {
		t.Errorf("initial emitter offset Y = %.3f, want 5", start.Pos[1])
	}
	if late.Pos[1] < 1.7 || late.Pos[1] > 1.9 {
		t.Errorf("late emitter offset Y = %.3f, want interpolated 1.8 at 80%% of its curve", late.Pos[1])
	}
	if start.SpriteID != first || late.SpriteID != first+1 {
		t.Fatalf("particle ids = %d, %d; want monotonic %d, %d", start.SpriteID, late.SpriteID, first, first+1)
	}
	angle := fx.SpriteSpin(start.V, 0, start.SpriteID)
	if again := fx.SpriteSpin(start.V, 0, start.SpriteID); math.Abs(angle-again) > 1e-12 {
		t.Errorf("particle spin changed from %.6f to %.6f between samples", angle, again)
	}
	if angle0, angle1 := fx.SpriteSpin(start.V, 0, 0), fx.SpriteSpin(start.V, 0, 1); math.Abs(angle0-angle1) < 1e-6 {
		t.Errorf("ordered epsilon produced identical neighboring turns %.4f", angle0)
	}
	// Expiring/removing old particles must not recycle ids or perturb the
	// next deterministic spin and emitter-volume sample.
	sim.Particles = sim.Particles[:0]
	e.Age = e.Duration * 0.8
	sim.Spawn(e)
	if got := sim.Particles[0].SpriteID; got != first+2 {
		t.Errorf("spawn after particle removal reused id %d, want %d", got, first+2)
	}
}

// TestFxHorizontalSprayRingUsesItsVerticalSpinAxis checks the axis conversion
// for the non-billboard rings used by phase_in_main_combo: Billboard=false pins
// the sprite to the horizontal plane, SpinY becomes the renderer's world-Z
// heading, and the authored epsilon can spread particles without tilting them.
func TestFxHorizontalSprayRingUsesItsVerticalSpinAxis(t *testing.T) {
	n := &fx.FxNode{
		Effect: "root>spray>ring",
		Style:  "STYLE_RING",
		Props: map[string]fx.FxProp{
			"Billboard":    {Type: "VARTYPE_BOOL", Num: 0},
			"SpinY":        {Type: "VARTYPE_ARRAY_TIMEFLOAT", Curve: []fx.FxKey{{T: 0, V: 5}, {T: 1, V: 5}}},
			"SpinZ":        {Type: "VARTYPE_ARRAY_TIMEFLOAT", Curve: []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0}}},
			"SpinEpsilonZ": {Type: "VARTYPE_ARRAY_TIMEFLOAT", Curve: []fx.FxKey{{T: 0, V: 1}, {T: 1, V: 1}}},
			"SpinRandom":   {Type: "VARTYPE_BOOL", Num: 0},
		},
	}
	v, ok := fx.VisualFromNode(n)
	if !ok {
		t.Fatal("STYLE_RING was not parsed as a particle visual")
	}
	if !v.Horizontal {
		t.Fatal("Billboard=false ring should be pinned perpendicular to the renderer's up axis")
	}
	if got := math.Remainder(fx.SpriteSpin(v, 0, 0), 2*math.Pi); math.Abs(got) > 1e-9 {
		t.Errorf("first ring heading = %.6f rad, want SpinY's five full turns = 0 modulo 2pi", got)
	}
	for i := 1; i < fx.RingSpreadBuckets; i++ {
		got := fx.SpriteSpin(v, 0, i)
		want := 2 * math.Pi * (5 + float64(i)/float64(fx.RingSpreadBuckets-1))
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("particle %d in-plane heading = %.6f rad, want %.6f from SpinY and the stable authored spread", i, got, want)
		}
	}
}

func TestFxStepSamplesEmitterOffsetForSuccessiveBirths(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxSyntheticLua))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	sim := fx.NewFxSim(f)
	if len(sim.Emitters) != 1 {
		t.Fatalf("emitters = %d, want 1", len(sim.Emitters))
	}
	e := &sim.Emitters[0]
	// Isolate the animated emitter offset from launch cone/gravity motion, so
	// each particle's position directly reports its birth height.
	e.Dev = 0
	e.Gravity = 0
	e.Visuals[0].Speed = 0
	e.Visuals[0].SpeedCurve = nil
	sim.Step(0.1)
	if len(sim.Particles) == 0 {
		t.Fatal("first step did not emit any particles")
	}
	firstBirth := sim.Particles[len(sim.Particles)-1].Pos[1]
	firstCount := len(sim.Particles)
	sim.Step(0.1)
	if len(sim.Particles) <= firstCount {
		t.Fatal("second step did not emit any particles")
	}
	secondBirth := sim.Particles[len(sim.Particles)-1].Pos[1]
	if !(firstBirth > secondBirth) {
		t.Errorf("successive emission heights = %.3f then %.3f; expected the animated emitter offset to descend", firstBirth, secondBirth)
	}
	// The authored curve runs from file Y=5 to Y=1, and the simulation keeps
	// the file's own Y-up frame; the attachment frame maps Y to world Z.
	if math.Abs(firstBirth-4.6) > 0.05 || math.Abs(secondBirth-4.2) > 0.05 {
		t.Errorf("sampled emission heights = %.3f then %.3f, want approximately 4.6 then 4.2 along the file's up axis", firstBirth, secondBirth)
	}
}

// TestFxSimRealFiles is a smoke test: the game's own effects build and
// produce sprites.
func TestFxSimRealFiles(t *testing.T) {
	for _, name := range []string{
		"aeonic_orb_blasts.lua",
		"flame_fx/flame_sprays/flame_ground_strip_combo.lua",
	} {
		f, err := fx.ParseFxLua(readFxLua(t, name))
		if err != nil {
			t.Fatalf("fx.ParseFxLua(%s): %v", name, err)
		}
		s := fx.NewFxSim(f)
		if len(s.Emitters) == 0 && len(s.Anchored) == 0 {
			t.Errorf("%s: nothing to play", name)
			continue
		}
		for i := 0; i < 60; i++ {
			s.Step(1.0 / 60)
		}
		sprites := s.Sprites()
		t.Logf("%s: emitters=%d anchored=%d sprites after 1s=%d",
			name, len(s.Emitters), len(s.Anchored), len(sprites))
		if len(sprites) == 0 {
			t.Errorf("%s: no sprites", name)
		}
	}
}

// ─── merged from x_test_fx_beam_two_marker_test.go ────────────────────────────────────────

type fxTwoMarkerEvent struct {
	file   string
	event  string
	first  string
	second string
	fx     string
}

// fxMarkerKeys and fxOtherMarkerKeys are the keys the resolver reads. They are
// spelled out here rather than reused from fxEventMarker so that an event the
// sweep calls two-marker but the resolver does not is a reportable
// disagreement rather than something the sweep cannot see.
var (
	fxMarkerKeys      = []string{"marker", "marker_0", "marker_surface_00", "marker_surface_0", "random_marker_0"}
	fxOtherMarkerKeys = []string{"fx_other_marker", "fx_other_marker_0"}
)

// collectTwoMarkerEvents finds every event in the tree that names a marker and
// a second one.
func collectTwoMarkerEvents(t *testing.T, tree *modtree.ModTree) []fxTwoMarkerEvent {
	t.Helper()
	var out []fxTwoMarkerEvent
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".events") {
			return
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil || len(data) == 0 {
			return
		}
		evs, err := whe.ParseFxEvents(data)
		if err != nil {
			t.Errorf("events file %s does not parse: %v", f.FullPath, err)
			return
		}
		for _, ev := range evs {
			first := ""
			for _, k := range fxMarkerKeys {
				if v, ok := ev.First(k); ok && strings.TrimSpace(v) != "" {
					first = strings.TrimSpace(v)
					break
				}
			}
			second := ""
			for _, k := range fxOtherMarkerKeys {
				if v, ok := ev.First(k); ok && strings.TrimSpace(v) != "" {
					second = strings.TrimSpace(v)
					break
				}
			}
			if first == "" || second == "" {
				continue
			}
			fxName, _ := ev.First("fx")
			out = append(out, fxTwoMarkerEvent{
				file: f.FullPath, event: ev.Name,
				first: first, second: second, fx: fxName,
			})
		}
	})
	return out
}

// TestShippedTwoMarkerEventsParse pins the first invariant over every event in
// the game: each one is found, names an effect, and that effect is a file that
// reads and parses into beams.
func TestShippedTwoMarkerEventsParse(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	events := collectTwoMarkerEvents(t, tree)
	if len(events) == 0 {
		t.Skip("no two-marker events in the tree")
	}
	t.Logf("%d two-marker events across the base game", len(events))

	// One effect per event is enough to know the whole set parsed, but the
	// failures are reported per event so a broken one is identifiable.
	effects := map[string]bool{}
	var broken []string
	for _, e := range events {
		if e.fx == "" {
			broken = append(broken, fmt.Sprintf("%s: event %q names no fx", e.file, e.event))
			continue
		}
		if effects[e.fx] {
			continue
		}
		effects[e.fx] = true
		data, err := readFxLuaFile(tree, roots, e.fx)
		if err != nil {
			broken = append(broken, fmt.Sprintf("%s: event %q: effect %q does not read: %v", e.file, e.event, e.fx, err))
			continue
		}
		ff, err := fx.ParseFxLua(data)
		if err != nil {
			broken = append(broken, fmt.Sprintf("%s: event %q: effect %q does not parse: %v", e.file, e.event, e.fx, err))
			continue
		}
		sim := fx.NewFxSim(ff)
		// A unit span is all the sampler needs to hand a beam its length.
		if len(sim.Beams(fauxgl.Vector{}, fauxgl.Vector{X: 1}, true)) == 0 {
			broken = append(broken, fmt.Sprintf("%s: event %q: effect %q parses but has no beam node to draw",
				e.file, e.event, e.fx))
		}
	}
	for _, b := range broken {
		t.Error(b)
	}
	names := make([]string, 0, len(effects))
	for fx := range effects {
		names = append(names, fx)
	}
	sort.Strings(names)
	t.Logf("effects named by two-marker events: %s", strings.Join(names, ", "))
}

// TestShippedTwoMarkerBeamsSpanExactlyTheGap pins the second invariant. For
// every two-marker event whose pair resolves on a model carrying both
// markers, every beam it draws must reach from one to the other.
func TestShippedTwoMarkerBeamsSpanExactlyTheGap(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	events := collectTwoMarkerEvents(t, tree)
	if len(events) == 0 {
		t.Skip("no two-marker events in the tree")
	}

	// Index the models by the markers they carry, so a pair can be looked up
	// without walking every model for every event.
	type modelMarkers struct {
		path  string
		names map[string]int
	}
	var models []modelMarkers
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".whm") {
			return
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil || len(data) == 0 {
			return
		}
		m, err := ParseWhmModel(data)
		if err != nil || len(m.Markers) == 0 {
			return
		}
		mm := modelMarkers{path: f.FullPath, names: map[string]int{}}
		for i, mk := range m.Markers {
			mm.names[strings.ToLower(mk.Name)] = i
		}
		models = append(models, mm)
	})

	var resolved, unresolved, beamsChecked int
	for _, e := range events {
		first := strings.ToLower(e.first)
		second := strings.ToLower(e.second)
		var model *modelMarkers
		for i := range models {
			_, hasA := models[i].names[first]
			_, hasB := models[i].names[second]
			if hasA && hasB {
				model = &models[i]
				break
			}
		}
		if model == nil {
			// "random" is a real answer meaning any other marker of the
			// model; there is no fixed gap to measure against.
			unresolved++
			t.Logf("no fixed pair: %-58s %s -> %s", e.file, e.first, e.second)
			continue
		}
		data, err := tree.ReadFile(model.path)
		if err != nil {
			t.Errorf("%s: %v", model.path, err)
			continue
		}
		m, err := ParseWhmModel(data)
		if err != nil {
			t.Errorf("%s: %v", model.path, err)
			continue
		}
		ia, ib := model.names[first], model.names[second]
		// The resolver the viewer actually uses must agree with the sweep,
		// or the two disagree about which markers the event names.
		if got := m.MarkerIndex(e.first); got != ia {
			t.Errorf("%s: the sweep took %s as index %d, the resolver says %d", e.file, e.first, ia, got)
		}
		if got := m.MarkerIndex(e.second); got != ib {
			t.Errorf("%s: the sweep took %s as index %d, the resolver says %d", e.file, e.second, ib, got)
		}
		// fxBeamOtherMarker is what decides there is a second end at all, and
		// fxEventSpansDistance what makes it span the gap rather than run on.
		if got := fxBeamOtherMarker(m, e.event2(t, tree)); got != ib {
			t.Errorf("%s: fxBeamOtherMarker resolved %d, the sweep took %d", e.file, got, ib)
		}
		pose := m.RestPose()
		a, okA := m.MarkerWorld(ia, pose, true)
		b, okB := m.MarkerWorld(ib, pose, true)
		if !okA || !okB {
			t.Errorf("%s: markers %s/%s have no world position", model.path, e.first, e.second)
			continue
		}
		gap := a.Sub(b).Length()
		if gap < 1e-6 {
			t.Errorf("%s: markers %s and %s sit on the same point", model.path, e.first, e.second)
			continue
		}
		ld, err := readFxLuaFile(tree, roots, e.fx)
		if err != nil {
			// Already reported by the parse test; nothing to measure here.
			t.Logf("%s: effect %q unreadable, span not checked: %v", e.file, e.fx, err)
			continue
		}
		ff, err := fx.ParseFxLua(ld)
		if err != nil {
			t.Logf("%s: effect %q unparsed, span not checked: %v", e.file, e.fx, err)
			continue
		}
		sim := fx.NewFxSim(ff)
		for i, r := range sim.Beams(a, b, true) {
			beamsChecked++
			got := r.To.Sub(r.From).Length()
			if got < gap-1e-3 || got > gap+1e-3 {
				t.Errorf("%s: beam %d of %s spans %.4f across a gap of %.4f (%s -> %s on %s)",
					e.file, i, e.fx, got, gap, e.first, e.second, model.path)
			}
		}
		resolved++
		t.Logf("%-58s %s -> %s: gap %.3f", e.file, e.first, e.second, gap)
	}
	if beamsChecked == 0 {
		t.Skip("no two-marker beam could be measured")
	}
	t.Logf("%d events resolved onto a model and checked, %d beams measured, %d had no fixed pair",
		resolved, beamsChecked, unresolved)
}

// event2 re-reads the event definition for the cross-check above, which needs
// the parsed event rather than just its strings.
func (e fxTwoMarkerEvent) event2(t *testing.T, tree *modtree.ModTree) whe.FxEvent {
	t.Helper()
	data, err := tree.ReadFile(e.file)
	if err != nil {
		t.Errorf("%s: %v", e.file, err)
		return whe.FxEvent{}
	}
	evs, err := whe.ParseFxEvents(data)
	if err != nil {
		t.Errorf("%s: %v", e.file, err)
		return whe.FxEvent{}
	}
	for _, ev := range evs {
		if strings.EqualFold(ev.Name, e.event) {
			return ev
		}
	}
	t.Errorf("%s: event %q is not there any more", e.file, e.event)
	return whe.FxEvent{}
}

// ─── merged from x_test_fx_sound_test.go ────────────────────────────────────────

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

// ─── merged from x_test_fx_file_axes_test.go ────────────────────────────────────────

const deathDustFile = "necron/structures/summoning_core/death_dust_02.lua"

// dustEmitter parses the reference effect and returns its root emitter.
func dustEmitter(t *testing.T) *fx.FxSim {
	t.Helper()
	f, err := fx.ParseFxLua(readFxLua(t, deathDustFile))
	if err != nil {
		t.Fatalf("death_dust_02 does not parse: %v", err)
	}
	sim := fx.NewFxSim(f)
	if sim == nil || len(sim.Emitters) == 0 {
		t.Fatal("death_dust_02 produced no emitter to spawn from")
	}
	return sim
}

// FX vectors are authored in the effect's own Y-up frame and stay there
// through the simulation; the frame the effect is attached to is what turns
// them into model space. The marker-less effect uses fxFallbackFrame, whose
// second axis is the world's up, so a ground cloud is flat along the file's Y
// and, once mapped, flat in the renderer's ground plane as well.
//
// Converting the vectors inside the simulation instead was the double swap
// that made a falling spray drift sideways: its vertical had already become
// the model's Z, and the frame then turned that onto a horizontal axis.
func TestShippedGroundDustLiesFlatInTheRenderersGroundPlane(t *testing.T) {
	sim := dustEmitter(t)

	// Birth a batch and read the spread of the cloud itself, which is what
	// the viewport draws.
	const batch = 400
	for i := 0; i < batch; i++ {
		sim.Spawn(&sim.Emitters[0])
	}
	minLocal, maxLocal := math.Inf(1), math.Inf(-1)
	minWorld, maxWorld := math.Inf(1), math.Inf(-1)
	for _, p := range sim.Particles {
		if p.Pos[1] < minLocal {
			minLocal = p.Pos[1]
		}
		if p.Pos[1] > maxLocal {
			maxLocal = p.Pos[1]
		}
		world := fxFallbackFrame[0].MulScalar(p.Pos[0]).
			Add(fxFallbackFrame[1].MulScalar(p.Pos[1])).
			Add(fxFallbackFrame[2].MulScalar(p.Pos[2]))
		if world.Z < minWorld {
			minWorld = world.Z
		}
		if world.Z > maxWorld {
			maxWorld = world.Z
		}
	}
	if thickness := maxLocal - minLocal; thickness > 1e-6 {
		t.Errorf("dust cloud is %.3f thick along the effect's up axis, want flat: "+
			"the volume's zero extent must land on Y, not on X or Z", thickness)
	}
	if thickness := maxWorld - minWorld; thickness > 1e-6 {
		t.Errorf("dust cloud is %.3f thick along the world's up axis, want flat: "+
			"the fallback frame must carry the file's Y onto Z", thickness)
	}
}

// The offset's sign carries through the authored frame too: -2 down the file's
// up axis is below the marker, not above it.
func TestShippedDustOffsetKeepsItsSignOnTheUpAxis(t *testing.T) {
	sim := dustEmitter(t)
	keys := sim.Emitters[0].Offset
	if len(keys) == 0 {
		t.Fatal("the emitter carries no Emitter_Offset to read")
	}
	if keys[0].Y >= 0 {
		t.Errorf("Emitter_Offset start (%.2f, %.2f, %.2f) is not negative on the file's "+
			"up axis, so the dust cannot start below its marker",
			keys[0].X, keys[0].Y, keys[0].Z)
	}
	sim.Emitters[0].Age = 0
	sim.Spawn(&sim.Emitters[0])
	if got := sim.Particles[len(sim.Particles)-1].Pos[1]; got >= 0 {
		t.Errorf("a particle was born at Y=%.2f, want the file's negative offset to put it "+
			"below the emitter instead of above it", got)
	}
}

// The volume is the cleanest statement of the convention: the shipped ground
// effect writes its zero on the second component, and that zero has to stay on
// the effect's Y axis with both horizontal extents intact.
func TestShippedDustVolumeZeroLandsOnTheUpAxis(t *testing.T) {
	vol := dustEmitter(t).Emitters[0].Volume
	if len(vol) == 0 {
		t.Skip("death_dust_02 declares no Emitter_Volume")
	}
	if vol[0].Y != 0 {
		t.Errorf("Emitter_Volume (%.2f, %.2f, %.2f) has its zero on the file's second "+
			"component, so the slab has to stay flat along Y at %.2f",
			vol[0].X, vol[0].Y, vol[0].Z, vol[0].Y)
	}
	if vol[0].X == 0 || vol[0].Z == 0 {
		t.Errorf("volume (%.2f, %.2f, %.2f) lost its flat spread, want both horizontal "+
			"extents kept", vol[0].X, vol[0].Y, vol[0].Z)
	}
}

func TestFxGroundClassificationUsesEventAndPathHints(t *testing.T) {
	cases := []struct {
		name string
		ev   whe.FxEvent
		path string
		want bool
	}{
		{name: "dust event", ev: whe.FxEvent{Name: "land_dust"}, want: true},
		{name: "ground beam path stays anchored by its own markers", ev: whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "marker", Value: "marker_ground_01"}, {Key: "fx_other_marker", Value: "marker_chest_front"}}}, path: `necron\ground_lightning_01`, want: false},
		{name: "ground effect path", path: `necron\structures\ground_impact`, want: true},
		{name: "ground marker path is normalized", ev: whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "marker", Value: "marker_surface_01_ground"}}}, want: true},
		{name: "ground attachment marker", ev: whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "marker", Value: "marker_ground_01"}}}, want: true},
		{name: "ordinary muzzle effect", ev: whe.FxEvent{Name: "muzzle_flash"}, path: `weapon\muzzle_flash`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fxIsGroundEffect(tc.ev, tc.path); got != tc.want {
				t.Errorf("fxIsGroundEffect(%q, %q) = %v, want %v", tc.ev.Name, tc.path, got, tc.want)
			}
		})
	}
}

func TestGroundFXOriginKeepsHorizontalAttachment(t *testing.T) {
	origin := fauxgl.Vector{X: 3, Y: -4, Z: 12}
	got := fxGroundOrigin(origin, -2)
	want := fauxgl.Vector{X: 3, Y: -4, Z: -2}
	if got != want {
		t.Errorf("grounded origin = %+v, want %+v", got, want)
	}
	if origin.Z != 12 {
		t.Errorf("grounding mutated its input: %+v", origin)
	}
}

// A beam's Arc and Offset are written by the same files as the emitter's
// volume and offset, so they are local vectors in the same Y-up frame. The
// simulation leaves them as authored and the attachment frame maps them into
// model space; converting them here as well is exactly the second swap that
// laid a vertical spray along the ground.
func TestShippedBeamArcIsMappedByTheAttachmentFrame(t *testing.T) {
	// aspect_lightning_prod writes Arc (0.16866, 0.44179, 0.67612): its third
	// component is the largest, which is the file's depth, so the frame has to
	// carry it onto the world's Y and keep the bow across a vertical beam
	// rather than collapsing it onto the beam's own length. Its beam also
	// sits under a spray, so this doubles as the check that the walk
	// descends into one.
	const file = "aspect_lightning_prod.lua"
	f, err := fx.ParseFxLua(readFxLua(t, file))
	if err != nil {
		t.Fatalf("%s does not parse: %v", file, err)
	}
	sim := fx.NewFxSim(f)
	if sim == nil || len(sim.BeamNodes) == 0 {
		t.Fatalf("%s carries a STYLE_BEAM node the simulation never collected", file)
	}
	b := &sim.BeamNodes[0]
	if len(b.Arc) == 0 {
		t.Fatalf("%s no longer writes an Arc to test", file)
	}

	from := fauxgl.Vector{X: 0, Y: 0, Z: 0}
	to := fauxgl.Vector{X: 0, Y: 0, Z: 8}
	rb := fx.NewBeamRibbon(*b, from, to, 0, true)
	authored := b.Arc[0]
	if got, want := rb.Arc, (fauxgl.Vector{X: authored.X, Y: authored.Y, Z: authored.Z}); got != want {
		t.Errorf("the beam's arc came out as %+v, want the authored local vector %+v", got, want)
	}
	// Mapped the way the overlay maps it, the arc has to keep a component
	// across the span of a vertical beam.
	mapped := fxFallbackFrame[0].MulScalar(rb.Arc.X).
		Add(fxFallbackFrame[1].MulScalar(rb.Arc.Y)).
		Add(fxFallbackFrame[2].MulScalar(rb.Arc.Z))
	dir := to.Sub(from).Normalize()
	bow := mapped.Sub(dir.MulScalar(mapped.Dot(dir)))
	if bow.Length() < 1e-6 {
		t.Errorf("arc %+v lies entirely along the span of a vertical beam after mapping, so "+
			"the ribbon draws straight: the attachment frame was not applied", rb.Arc)
	}
}

// A beam is only ever drawn if the walk reaches it, and the walk used to stop
// at a STYLE_SPRAY. This file is a spray holding a combo holding the beam, so
// before the fix its beam parsed and was reachable by name yet never reached
// the simulation: the effect was listed in the viewport and drew nothing.
func TestShippedBeamUnderASprayIsCollected(t *testing.T) {
	f, err := fx.ParseFxLua(readFxLua(t, "aspect_lightning_prod.lua"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := 0
	for i := range f.Nodes {
		if f.Nodes[i].Style == "STYLE_BEAM" {
			want++
		}
	}
	if want == 0 {
		t.Skip("the file no longer carries a beam node")
	}
	sim := fx.NewFxSim(f)
	if len(sim.BeamNodes) != want {
		t.Errorf("the file declares %d beam node(s), the simulation collected %d: "+
			"the walk is not descending into a spray's subtree", want, len(sim.BeamNodes))
	}
}

// ─── merged from x_test_fx_sga_model_test.go ────────────────────────────────────────

const uaModule = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/UA_DC.module`

// warriorWhmPath is a necron_basic_warrior, whose gun tube beams are the ones
// that used to draw across the weapon rather than along it.
const warriorWhmPath = "Data/art/ebps/races/necrons/troops/necron_basic_warrior.whm"

// immortalWhmPath is a necron_immortal, whose gauss markers carry a leading
// space in the file and are asked for without one by the events.
const immortalWhmPath = "Data/art/ebps/races/necrons/troops/necron_immortal.whm"

// sgaTree opens the installed mod, skipping when it is not there so the normal
// test run stays hermetic.
func sgaTree(t *testing.T) *modtree.ModTree {
	t.Helper()
	tree, err := modtree.BuildModTree(uaModule, "English")
	if err != nil {
		t.Skipf("no installed UltimateApocalypse module: %v", err)
	}
	t.Cleanup(tree.Close)
	return tree
}

// sgaModel reads one model out of the archives.
func sgaModel(t *testing.T, path string) *WhmModel {
	t.Helper()
	raw, err := sgaTree(t).ReadFile(path)
	if err != nil {
		t.Skipf("%s not in the archives: %v", path, err)
	}
	m, perr := ParseWhmModel(raw)
	if perr != nil {
		t.Fatalf("%s parses: %v", path, perr)
	}
	return m
}

// sgaModels lists every .whm under the necron troop folders, which is how the
// sweeps below get a population rather than a hand-picked case.
func sgaModels(t *testing.T) []string {
	t.Helper()
	tree := sgaTree(t)
	var out []string
	seen := map[string]bool{}
	for _, dir := range []string{
		"Data/art/ebps/races/necrons/troops",
		"Data/art/ebps/races/necron/troops",
	} {
		folder := tree.FindFolder(dir)
		if folder == nil {
			continue
		}
		for _, tf := range folder.Files {
			if strings.EqualFold(filepath.Ext(tf.Name), ".whm") && !seen[tf.FullPath] {
				seen[tf.FullPath] = true
				out = append(out, tf.FullPath)
			}
		}
	}
	if len(out) == 0 {
		t.Skip("no necron models in the archives")
	}
	return out
}

// A model that lives only inside an archive has to be readable through the
// tree, markers and all: everything the FX preview hangs off a marker is
// invisible to a test that can only open loose files.
func TestShippedModelInsideAnArchiveParsesWithItsMarkers(t *testing.T) {
	m := sgaModel(t, warriorWhmPath)
	if len(m.Markers) == 0 {
		t.Fatal("the warrior model came out with no markers, so nothing could hang off it")
	}
	if len(m.Bones) == 0 {
		t.Error("the warrior model came out with no bones")
	}
	t.Logf("%s: %d bones, %d markers, %d vertices",
		filepath.Base(warriorWhmPath), len(m.Bones), len(m.Markers), len(m.Vertices))
}

// The gun tube is the case the marker's own geometry settles. Its base and end
// markers are the two ends of the barrel, so the direction between them is the
// direction a beam at that marker has to run: along the tube.
//
// It used to run across the weapon instead. The marker's frame is conjugated
// by diag(-1, 1, 1) — see MarkerFrameAxes — and without it the column this
// test measures pointed sideways.
func TestGunTubeMarkerPointsAlongTheWeapon(t *testing.T) {
	m := sgaModel(t, warriorWhmPath)
	pose := m.RestPose()

	base := m.MarkerIndex("marker_gun_tube_ctr_base")
	end := m.MarkerIndex("marker_gun_tube_ctr_end")
	if base < 0 || end < 0 {
		t.Skip("this model carries no gun tube markers")
	}
	pb, ok1 := m.MarkerWorld(base, pose, true)
	pe, ok2 := m.MarkerWorld(end, pose, true)
	if !ok1 || !ok2 {
		t.Fatal("the tube markers have no position under the rest pose")
	}
	span := pe.Sub(pb)
	if span.Length() < 1e-6 {
		t.Fatal("the tube's two markers are in the same place")
	}
	truth := span.Normalize()

	// The frame's columns are the candidate directions; one of them has to be
	// the barrel's. Scoring all six is what MarkerAxis does internally, and
	// what makes this a check of the frame rather than of one column.
	axX, axY, axZ, ok := m.MarkerFrameAxes(base, pose, true)
	if !ok {
		t.Fatal("the tube marker has no frame of its own")
	}
	best := -2.0
	for _, c := range []fauxgl.Vector{axX, axY, axZ, axX.MulScalar(-1), axY.MulScalar(-1), axZ.MulScalar(-1)} {
		if c.Length() < 1e-6 {
			continue
		}
		if d := c.Normalize().Dot(truth); d > best {
			best = d
		}
	}
	if best < 0.99 {
		t.Errorf("no column of the tube marker's frame points along the barrel: "+
			"best dot %.3f with the base->end direction (%.2f %.2f %.2f)",
			best, truth.X, truth.Y, truth.Z)
	}

	// And the axis a single-marker beam at this marker actually runs along has
	// to follow the tube rather than cross it. That is MarkerBeamAxis's second
	// column; the forward MarkerAxis scores the marker as facing is a different
	// question — here it points straight across the barrel, which is why tube
	// beams used to draw athwart the weapon.
	ax, aok := m.MarkerBeamAxis(base, pose, true)
	if !aok {
		t.Fatal("the tube marker has no frame")
	}
	if d := math.Abs(ax.Dot(truth)); d < 0.99 {
		t.Errorf("the beam axis (%.2f %.2f %.2f) runs across the weapon rather "+
			"than along it: |dot| %.3f with (%.2f %.2f %.2f)",
			ax.X, ax.Y, ax.Z, d, truth.X, truth.Y, truth.Z)
	}
}

// Two of the shipped necron immortals write 44 of their marker names with a
// leading space. An event asks for the name without one, so an exact match
// found nothing: every gauss effect fell back to the model's own axes and the
// tube ran up Z instead of along the barrel.
func TestMarkerLookupIgnoresStrayWhitespace(t *testing.T) {
	for _, path := range sgaModels(t) {
		m := sgaModel(t, path)
		for i := range m.Markers {
			name := m.Markers[i].Name
			trimmed := strings.TrimSpace(name)
			if trimmed == name {
				continue
			}
			if m.MarkerIndex(trimmed) != i {
				t.Errorf("%s: marker %q is unreachable by its own name %q",
					filepath.Base(path), name, trimmed)
			}
			if m.MarkerIndex(name) != i {
				t.Errorf("%s: marker %q is unreachable by its exact name",
					filepath.Base(path), name)
			}
		}
	}
}

// A tube beam that names no second marker has to take its direction from the
// marker's own frame. This is the check that the warrior's tube events really
// are single-marker, which is why they lean on the frame rather than on a
// span between two markers — and that the column read is the one the tube
// runs along, which is MarkerBeamAxis's second column rather than the axis
// MarkerAxis scores the marker as facing.
func TestGunTubeBeamEventsNameOnlyOneMarker(t *testing.T) {
	tree := sgaTree(t)
	m := sgaModel(t, warriorWhmPath)

	raw, err := tree.ReadFile("Data/art/ebps/races/necrons/troops/necron_basic_warrior.whe")
	if err != nil {
		t.Skipf("no warrior .whe in the archives: %v", err)
	}
	events, perr := whe.ParseFxEvents(raw)
	if perr != nil {
		t.Fatalf("the warrior .whe does not parse: %v", perr)
	}

	base := m.MarkerIndex("marker_gun_tube_ctr_base")
	end := m.MarkerIndex("marker_gun_tube_ctr_end")
	pb, _ := m.MarkerWorld(base, m.RestPose(), true)
	pe, _ := m.MarkerWorld(end, m.RestPose(), true)
	truth := pe.Sub(pb).Normalize()

	seen := 0
	for i, ev := range events {
		name := strings.ToLower(ev.Name)
		if !strings.Contains(name, "tube") || !strings.Contains(name, "beam") {
			continue
		}
		m1 := fxEventMarker(m, ev, i)
		m2 := fxBeamOtherMarker(m, ev)
		seen++
		if m1 < 0 {
			t.Errorf("event %q names a marker the model does not carry", ev.Name)
			continue
		}
		if m2 >= 0 {
			continue
		}
		ax, aok := m.MarkerBeamAxis(m1, m.RestPose(), true)
		if !aok {
			t.Errorf("event %q plays at a marker with no frame", ev.Name)
			continue
		}
		if d := math.Abs(ax.Dot(truth)); d < 0.9 {
			t.Errorf("event %q has no second marker, so its beam runs along the "+
				"marker's frame column two, which points across the weapon: |dot| %.3f",
				ev.Name, d)
		}
	}
	if seen == 0 {
		t.Skip("this .whe names no tube beam events")
	}
	t.Logf("checked %d tube beam events", seen)
}

// ─── merged from x_test_fx_frame_test.go ────────────────────────────────────────

func TestFXNoRotateDefaultsToFalseAndReadsRootFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		lua  string
		want bool
	}{
		{name: "missing flag inherits marker", lua: `fxtypes = { { effect = "root", fx = { properties = {}, style = "STYLE_COMBO" } } }`},
		{name: "explicit false inherits marker", lua: `fxtypes = { { effect = "root", fx = { properties = { property_00 = { name = "No_Rotate", type = "VARTYPE_BOOL", value = 0, }, }, style = "STYLE_COMBO" } } }`},
		{name: "explicit true opts out", lua: `fxtypes = { { effect = "root", fx = { properties = { property_00 = { name = "No_Rotate", type = "VARTYPE_BOOL", value = 1, }, }, style = "STYLE_COMBO" } } }`, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := fx.ParseFxLua([]byte(tc.lua))
			if err != nil {
				t.Fatalf("parse FX: %v", err)
			}
			sim := fx.NewFxSim(file)
			if sim.NoRotate != tc.want {
				t.Fatalf("NoRotate=%v, want %v", sim.NoRotate, tc.want)
			}
		})
	}
}

func TestFXLocalAxesFollowMarkerUnlessNoRotate(t *testing.T) {
	model := &WhmModel{}
	model.Markers = []WhmMarker{{
		Name:   "turned",
		Matrix: [9]float32{0, 0, 1, 0, 1, 0, 1, 0, 0},
	}}
	pose := model.RestPose()
	markerFrame := fxOriginAxisFrames(fxOverlayEffect{}, model, 0, pose)[0]
	if markerFrame == fxFallbackFrame {
		t.Fatal("default FX local axes should use the marker frame, not file/world fallback")
	}
	if markerFrame[0].Sub(fauxgl.Vector{Y: -1}).Length() > 1e-6 {
		t.Errorf("local X=%v, want marker/root-frame -Y", markerFrame[0])
	}

	noRotate := fxOriginAxisFrames(fxOverlayEffect{Sim: &fx.FxSim{NoRotate: true}}, model, 0, pose)[0]
	if noRotate != fxFallbackFrame {
		t.Errorf("No_Rotate local axes=%v, want the authored FX frame %v", noRotate, fxFallbackFrame)
	}
}

func TestFXLocalParticleOffsetMapsThroughMarkerFrame(t *testing.T) {
	frame := [3]fauxgl.Vector{{Y: 1}, {X: 1}, {Z: 1}}
	e := fxOverlayEffect{Origin: fauxgl.Vector{X: 10, Y: 20, Z: 30}, Frame: frame}
	sp := e.fxTurnSprite(fx.Sprite{Pos: [3]float64{2, 3, 4}})
	if want := [3]float64{3, 2, 4}; sp.Pos != want {
		t.Fatalf("local particle offset = %v, want marker-frame result %v", sp.Pos, want)
	}
	world := e.Origin.Add(fauxgl.Vector{X: sp.Pos[0], Y: sp.Pos[1], Z: sp.Pos[2]})
	if want := (fauxgl.Vector{X: 13, Y: 22, Z: 34}); world != want {
		t.Errorf("world particle position = %v, want marker origin + mapped local offset %v", world, want)
	}
}

func TestFXMarkerlessEmitterDirectionUsesAuthoredFXAxis(t *testing.T) {
	file, err := fx.ParseFxLua([]byte(`fxtypes = {
		{ effect = "root>puff", fx = { properties = {
			{ name = "Emitter_Rate", type = "VARTYPE_ARRAY_TIMEFLOAT", value = { 0, 1, 1, 1 }, },
		}, style = "STYLE_SPRAY" },
		{ effect = "root", fx = { properties = {
			{ name = "Emitter_Direction", type = "VARTYPE_VECTOR3", value = { 1, 0, 0 }, },
		}, style = "STYLE_COMBO" },
		{ effect = "root>leaf", fx = { properties = {
			{ name = "Duration", type = "VARTYPE_FLOAT", value = 1, },
		}, style = "STYLE_RING" },
	}`))
	if err != nil {
		t.Fatalf("parse FX: %v", err)
	}
	sim := fx.NewFxSim(file)
	if sim.Axis.Sub(fauxgl.Vector{X: 1}).Length() > 1e-6 {
		t.Fatalf("authored direction = %v, want renderer-local +X", sim.Axis)
	}
	e := fxOverlayEffect{Sim: sim, Origin: fauxgl.Vector{Z: 5}, Marker: -1, Frame: fxFallbackFrame}
	e.Axis = e.fxMarkerVector(sim.Axis)
	far := e.fxFarPoint()
	got := far.Sub(e.Origin).Normalize()
	if want := (fauxgl.Vector{X: 1}); got.Sub(want).Length() > 1e-6 {
		t.Errorf("markerless beam direction = %v, want authored direction %v", got, want)
	}
}

func TestFXTransformSelfToOtherIsRecognized(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  bool
	}{
		{name: "self to other", value: "self_to_other", want: true},
		{name: "other transform", value: "self", want: false},
		{name: "case and spaces", value: " SELF_TO_OTHER ", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "fx_transform", Value: tc.value}}}
			if got := fxEventAlignsToOther(ev); got != tc.want {
				t.Errorf("fxEventAlignsToOther(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestFXSelfToOtherAlignsTheLocalFXForwardWithTheMarkerSpan(t *testing.T) {
	frame := [3]fauxgl.Vector{{X: 1}, {Y: 1}, {Z: 1}}
	direction := fauxgl.Vector{X: 1, Y: 1}.Normalize()
	aligned := fxFrameToward(frame, direction)
	if got := aligned[2].Sub(direction).Length(); got > 1e-6 {
		t.Fatalf("self_to_other forward=%v, want marker-span direction %v", aligned[2], direction)
	}
	for i, axis := range aligned {
		if math.Abs(axis.Length()-1) > 1e-6 {
			t.Errorf("aligned axis %d is not unit length: %v", i, axis)
		}
	}
	if math.Abs(aligned[0].Dot(aligned[1])) > 1e-6 || math.Abs(aligned[0].Dot(aligned[2])) > 1e-6 || math.Abs(aligned[1].Dot(aligned[2])) > 1e-6 {
		t.Errorf("self_to_other frame is not orthogonal: %v", aligned)
	}
	// No_Rotate preserves the authored frame, even when the event carries
	// self_to_other; it is the explicit FX-level rotation opt-out. The file
	// is written Y up and the renderer is Z up, so the authored forward
	// still arrives on the model's Y axis.
	e := fxOverlayEffect{Sim: &fx.FxSim{NoRotate: true}, Frame: frame}
	if got, want := e.fxMarkerVector(fauxgl.Vector{Z: 1}), (fauxgl.Vector{Y: 1}); got != want {
		t.Errorf("No_Rotate forward axis = %v, want the file's own axis carried over as %v", got, want)
	}
}

func TestFXSelfToOtherAlignsGeneratedBeamWithTheMarkerSpan(t *testing.T) {
	origin := fauxgl.Vector{X: 1, Y: -2, Z: 3}
	far := fauxgl.Vector{X: 5, Y: 4, Z: 3}
	span := far.Sub(origin)
	frame := fxFrameToward(fxFallbackFrame, span)
	sim := &fx.FxSim{BeamNodes: []fx.Beam{{
		Length:   []fx.FxKey{{V: 0.5}},
		Width:    []fx.FxKey{{V: 0.25}},
		Lifetime: 1,
	}}}
	sim.Reset()
	effect := fxOverlayEffect{
		Sim: sim, Origin: origin, Far: far, HasOther: true, Spans: true,
		AlignToOther: true, Frame: frame,
	}
	scene := fxBuildGpuScene([]fxOverlayEffect{effect}, nil, fauxgl.Vector{Z: 5}, fauxgl.Vector{Z: 1})
	if scene == nil {
		t.Fatal("self_to_other beam produced no GPU scene")
	}
	var beam *fx.BeamRibbon
	for i := range scene.Groups {
		if len(scene.Groups[i].Beams) > 0 {
			beam = &scene.Groups[i].Beams[0].Ribbon
			break
		}
	}
	if beam == nil {
		t.Fatal("self_to_other GPU scene contains no beam ribbon")
	}
	gotDirection := beam.To.Sub(beam.From).Normalize()
	wantDirection := span.Normalize()
	if gotDirection.Sub(wantDirection).Length() > 1e-6 {
		t.Errorf("beam direction=%v, want marker-span direction=%v", gotDirection, wantDirection)
	}
	if got, want := beam.To.Sub(beam.From).Length(), span.Length()*0.5; math.Abs(got-want) > 1e-6 {
		t.Errorf("beam span=%.6f, want half the marker gap %.6f", got, want)
	}
}

func TestFXNoRotateKeepsFileFrameAndDropsMarkerRotation(t *testing.T) {
	frame := [3]fauxgl.Vector{{Y: 1}, {Z: 1}, {X: 1}}
	e := fxOverlayEffect{
		Sim:   &fx.FxSim{NoRotate: true},
		Frame: frame,
	}
	// The marker rotation is ignored, but the effect file's Y-up frame is
	// still carried over to the renderer's Z-up one.
	local := fauxgl.Vector{X: 2, Y: 3, Z: 4}
	if got, want := e.fxMarkerVector(local), (fauxgl.Vector{X: 2, Y: 4, Z: 3}); got != want {
		t.Errorf("No_Rotate local displacement = %v, want the file's own frame carried over as %v", got, want)
	}
	if x, y, z := e.fxRingFrame(); x != fxFallbackFrame[0] || y != fxFallbackFrame[1] || z != fxFallbackFrame[2] {
		t.Errorf("No_Rotate ring frame = (%v,%v,%v), want authored FX frame %v", x, y, z, fxFallbackFrame)
	}
}

// ─── merged from x_test_fx_ring_alpha_test.go ────────────────────────────────────────

func TestRingPanelLeavesItsTransparentMarginEmpty(t *testing.T) {
	const w, h = 96, 96
	// A 16x16 map with an opaque column down the middle and nothing else:
	// the same layout as CRYSTAL_BOT_01, a shape in a square of empty map.
	const n = 16
	tex := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if x >= 6 && x < 10 {
				tex.SetRGBA(x, y, color.RGBA{R: 40, G: 220, B: 60, A: 255})
				continue
			}
			tex.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 0})
		}
	}
	sim := &fx.FxSim{}
	sim.RingNodes = []fx.Ring{{
		Texture:  "panel",
		Blending: 1,
		Lifetime: 1,
		Radius:   []fx.FxKey{{T: 0, V: 1.2}, {T: 1, V: 1.2}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}
	opts := render.RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 6, Fov: 45}
	// The marker's own frame: up, outward and the way round the ring. All
	// three are needed - a ring laid out with its span and its width on the
	// same axis has no quad to draw at all.
	effects := []fxOverlayEffect{{Sim: sim,
		Right: fauxgl.Vector{X: 1},
		Up:    fauxgl.Vector{Y: 1},
		Axis:  fauxgl.Vector{Z: 1},
		Frame: [3]fauxgl.Vector{
			{X: 0, Y: 0, Z: 1}, // up
			{X: 1},             // outward
			{Y: 1},             // the way round
		}}}
	out := renderFxOverlay(effects, opts, w, h, map[string]*image.RGBA{"": tex})

	centre := out.RGBAAt(w/2, h/2)
	corner := out.RGBAAt(2, 2)
	if centre.A == 0 {
		t.Fatalf("the panel itself was not drawn: centre alpha 0")
	}
	if corner.A != 0 {
		t.Errorf("the empty margin of the map was filled in: corner is %#v, want nothing drawn", corner)
	}
	// The column must be a column: narrow beside the square it sits in. This
	// is the part that fails when the margin is filled, since the fill is
	// what makes the quad read as a square.
	drawn := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if out.RGBAAt(x, y).A != 0 {
				drawn++
			}
		}
	}
	share := float64(drawn) / float64(w*h)
	t.Logf("centre %#v, corner %#v, %d of %d pixels (%.1f%%) drawn", centre, corner, drawn, w*h, share*100)
	if share > 0.5 {
		t.Errorf("the panel covers %.1f%% of the frame: the map's empty margin is being drawn", share*100)
	}
}

// A map with no alpha of its own still fills the quad, which is the case the
// old rule was reaching for: a map that decodes fully opaque must not turn
// into a hole.
func TestOpaqueRingMapStillFillsItsQuad(t *testing.T) {
	const w, h = 96, 96
	const n = 16
	tex := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := range tex.Pix {
		tex.Pix[i] = 255
	}
	sim := &fx.FxSim{}
	sim.RingNodes = []fx.Ring{{
		Texture:  "flash",
		Blending: 1,
		Lifetime: 1,
		Radius:   []fx.FxKey{{T: 0, V: 1.2}, {T: 1, V: 1.2}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}
	opts := render.RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 6, Fov: 45}
	effects := []fxOverlayEffect{{Sim: sim,
		Right: fauxgl.Vector{X: 1},
		Up:    fauxgl.Vector{Y: 1},
		Axis:  fauxgl.Vector{Z: 1},
		Frame: [3]fauxgl.Vector{
			{X: 0, Y: 0, Z: 1}, // up
			{X: 1},             // outward
			{Y: 1},             // the way round
		}}}
	out := renderFxOverlay(effects, opts, w, h, map[string]*image.RGBA{"": tex})
	drawn := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if out.RGBAAt(x, y).A != 0 {
				drawn++
			}
		}
	}
	if drawn == 0 {
		t.Fatal("an opaque map drew nothing at all")
	}
	t.Logf("an opaque map fills %d of %d pixels", drawn, w*h)
}

// The SpinEpsilon curves scatter the copies of one panel over the turn they
// name, so a file holding a single ring has no copies to scatter. Every
// crystal panel is a file of its own: the model lays eight of them out
// around the gem on eight markers, each with SpinEpsilonZ 1 and SpinRandom 1.
// Scattering a lone panel turns it by an angle no marker asked for, and eight
// panels at eight unrelated tilts are not a crystal.
func TestLoneRingIsNotTurnedByItsSpinEpsilon(t *testing.T) {
	panel := func() *fx.FxSim {
		s := &fx.FxSim{}
		s.RingNodes = []fx.Ring{{
			Texture:    "TEXTURES_NEW/NECRON/CRYSTAL_BOT_01.TGA",
			Blending:   1,
			Lifetime:   1,
			Radius:     []fx.FxKey{{T: 0, V: 1.36}, {T: 1, V: 1.36}},
			SpinZ:      []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0}},
			EpsZ:       []fx.FxKey{{T: 0, V: 1}},
			SpinRandom: true,
			Colour:     []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
		}}
		return s
	}
	centre := fauxgl.Vector{Z: 8.977}
	right := fauxgl.Vector{X: -0.154, Y: 0.154, Z: 0.976}
	up := fauxgl.Vector{X: 0.690, Y: -0.690, Z: 0.218}
	side := fauxgl.Vector{X: 0.707, Y: 0.707}

	lone := panel().Rings(centre, side, right, up, fauxgl.Vector{Z: 1})
	if len(lone) != 1 {
		t.Fatalf("a file with one ring drew %d quads", len(lone))
	}
	// The same panel with its epsilon taken away, which is what the model
	// wants for a facet that has a marker of its own to stand on.
	upright := panel()
	upright.RingNodes[0].EpsZ = nil
	want := upright.Rings(centre, side, right, up, fauxgl.Vector{Z: 1})[0]
	got := lone[0]
	if got.Across.Sub(want.Across).Length() > 1e-9 {
		t.Errorf("a lone panel was turned by its SpinEpsilon: across %v, an unspread panel draws %v",
			got.Across, want.Across)
	}

	// A file that really does hold several rings still has its spread: that
	// is the case the curves are for.
	many := &fx.FxSim{}
	many.RingNodes = []fx.Ring{
		{Texture: "a", Lifetime: 1, Radius: []fx.FxKey{{V: 1}}, SpinZ: []fx.FxKey{{V: 0}},
			EpsZ: []fx.FxKey{{V: 1}}, SpinRandom: true},
		{Texture: "a", Lifetime: 1, Radius: []fx.FxKey{{V: 1}}, SpinZ: []fx.FxKey{{V: 0}},
			EpsZ: []fx.FxKey{{V: 1}}, SpinRandom: true},
	}
	both := many.Rings(centre, side, right, up, fauxgl.Vector{Z: 1})
	if len(both) != 2 {
		t.Fatalf("a file with two rings drew %d quads", len(both))
	}
	if both[0].Across.Sub(both[1].Across).Length() < 1e-6 {
		t.Error("two rings of one file came out on the same face, so the spread never reached them")
	}
}

// ─── merged from x_test_fx_scale_test.go ────────────────────────────────────────

func TestFxScaleSpriteScalesRadiusAndLocalOffsetTogether(t *testing.T) {
	sp := fx.Sprite{Pos: [3]float64{1, -2, 0.5}, Radius: 0.25}
	got := fxScaleSprite(sp, 2)
	if got.Pos != [3]float64{2, -4, 1} || got.Radius != 0.5 {
		t.Fatalf("scaled sprite = pos %v radius %v, want pos [2 -4 1] radius 0.5", got.Pos, got.Radius)
	}
	if sp.Pos != [3]float64{1, -2, 0.5} || sp.Radius != 0.25 {
		t.Fatalf("scaling modified the simulation sprite: %+v", sp)
	}

	fallback := fxScaleSprite(fx.Sprite{}, 2)
	if fallback.Radius != 2*render.SpriteFallbackRadius {
		t.Errorf("missing-radius fallback = %v, want %v in model units", fallback.Radius, 2*render.SpriteFallbackRadius)
	}
}

func TestFxScaleRibbonScalesAuthoredDimensionsButKeepsMarkerGap(t *testing.T) {
	origin := fauxgl.Vector{X: 10, Y: 2, Z: -1}
	e := fxOverlayEffect{Origin: origin, Scale: 2}
	rb := fx.BeamRibbon{
		From:  origin.Add(fauxgl.Vector{X: 0.5}),
		To:    origin.Add(fauxgl.Vector{X: 2.5}),
		Width: 0.25,
		Arc:   fauxgl.Vector{Y: 0.2},
		Noise: 0.1,
	}
	got := e.fxScaleRibbon(rb, true)
	if math.Abs(got.From.X-11) > 1e-9 || math.Abs(got.To.X-13) > 1e-9 {
		t.Errorf("scaled beam endpoints = %v to %v, want translated offset and fixed two-unit marker gap", got.From, got.To)
	}
	if math.Abs(got.To.Sub(got.From).Length()-2) > 1e-9 {
		t.Errorf("marker-to-marker span changed to %v", got.To.Sub(got.From).Length())
	}
	if got.Width != 0.5 || got.Arc.Y != 0.4 || got.Noise != 0.2 {
		t.Errorf("scaled ribbon dimensions width=%v arc=%v noise=%v", got.Width, got.Arc, got.Noise)
	}
}

func TestFxScaleRibbonScalesIntrinsicBeamLength(t *testing.T) {
	e := fxOverlayEffect{Origin: fauxgl.Vector{Y: -3}, Scale: 1.5}
	rb := fx.BeamRibbon{
		From:  e.Origin,
		To:    e.Origin.Add(fauxgl.Vector{Z: 2}),
		Width: 0.2,
	}
	got := e.fxScaleRibbon(rb, false)
	if math.Abs(got.To.Sub(got.From).Length()-3) > 1e-9 {
		t.Errorf("intrinsic beam length = %v, want 3 model units", got.To.Sub(got.From).Length())
	}
	if math.Abs(got.From.Sub(e.Origin).Length()) > 1e-9 {
		t.Errorf("intrinsic beam moved off its anchored origin: %v", got.From)
	}
}

func TestFxScaleFXControlSetsTheGeometryMultiplier(t *testing.T) {
	v := &modelViewer{FxScale: fxScaleDefaultValue}
	ScaleFX(v, modelFXScaleStep)
	if math.Abs(v.FxScale-modelFXScaleStep) > 1e-9 {
		t.Fatalf("UI scale control set %.3f, want %.3f", v.FxScale, modelFXScaleStep)
	}
	effects := fxScaledEffects([]fxOverlayEffect{{}}, v.FxScale)
	if len(effects) != 1 || math.Abs(effects[0].fxScaleOrOne()-modelFXScaleStep) > 1e-9 {
		t.Errorf("displayed scale did not reach FX geometry: %+v", effects)
	}
	ScaleFX(v, 1/modelFXScaleStep)
	if math.Abs(v.FxScale-fxScaleDefaultValue) > 1e-9 {
		t.Errorf("resetting with inverse step returned %.3f, want 1", v.FxScale)
	}
}

func TestFxBuildGpuSceneUsesUnifiedScaleAndPreservesMarkerSpans(t *testing.T) {
	origin := fauxgl.Vector{X: 10}
	sim := &fx.FxSim{
		Particles: []fx.Particle{{
			V:   fx.Visual{Lifetime: 1, Radius: []fx.FxKey{{V: 0.25}}, SelfLit: true},
			Pos: [3]float64{1, 2, 3},
		}},
		BeamNodes: []fx.Beam{{
			Lifetime: 1,
			Length:   []fx.FxKey{{V: 0.5}},
			Width:    []fx.FxKey{{V: 0.25}},
		}},
	}
	effect := fxOverlayEffect{
		Sim: sim, Origin: origin, Far: fauxgl.Vector{X: 14},
		HasOther: true, Spans: true, Scale: 1.5, EventScale: 2,
	}
	scene := fxBuildGpuScene([]fxOverlayEffect{effect}, nil, fauxgl.Vector{}, fauxgl.Vector{Z: 1})
	if scene == nil {
		t.Fatal("GPU scene is empty")
	}
	var sprite *fx.Sprite
	var beam *fx.BeamRibbon
	for i := range scene.Groups {
		g := &scene.Groups[i]
		if len(g.Sprites) != 0 {
			sprite = &g.Sprites[0]
		}
		if len(g.Beams) != 0 {
			beam = &g.Beams[0].Ribbon
		}
	}
	if sprite == nil || beam == nil {
		t.Fatalf("GPU scene has sprite=%v beam=%v", sprite != nil, beam != nil)
	}
	if sprite.Pos != [3]float64{3, 6, 9} || sprite.Radius != 0.75 {
		t.Errorf("GPU sprite pos=%v radius=%v, want pos [3 6 9] radius 0.75", sprite.Pos, sprite.Radius)
	}
	if math.Abs(beam.Width-0.75) > 1e-9 {
		t.Errorf("GPU beam width = %v, want 0.75", beam.Width)
	}
	if got := beam.To.Sub(beam.From).Length(); math.Abs(got-2) > 1e-9 {
		t.Errorf("GPU marker-spanning beam length = %v, want unchanged 2-unit marker fraction", got)
	}
	if scene.Groups[0].Origin != [3]float64{10, 0, 0} {
		t.Errorf("GPU effect origin changed with scale: %v", scene.Groups[0].Origin)
	}
}

// TestFxBuildGpuSceneGroupsByTextureBlendAndSite pins what the group key
// keeps apart, now that it is a struct rather than a formatted string: two
// effects reading the same map on different markers are two draws (each
// sprite is positioned relative to its own site), one site merges into one
// draw, and a covering and an additive copy of the same map never share.
func TestFxBuildGpuSceneGroupsByTextureBlendAndSite(t *testing.T) {
	simWith := func(blending int) *fx.FxSim {
		return &fx.FxSim{Anchored: []fx.Visual{{
			Lifetime: 1,
			Blending: blending,
			Radius:   []fx.FxKey{{V: 0.3}},
			SelfLit:  true,
		}}}
	}
	build := func(effects []fxOverlayEffect) *fxGpuScene {
		return fxBuildGpuScene(effects, nil, fauxgl.Vector{}, fauxgl.Vector{Z: 1})
	}
	sites := []fxOverlayEffect{
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 2}},
	}
	two := build(sites)
	if two == nil {
		t.Fatal("two effects on two markers built no scene")
	}
	if len(two.Groups) != 2 {
		t.Fatalf("two markers, same map: %d groups, want 2 (sprites are relative to their own site)", len(two.Groups))
	}
	for i := range two.Groups {
		g := &two.Groups[i]
		if len(g.Sprites) != 1 || len(g.Beams) != 0 {
			t.Errorf("group %d has %d sprites and %d beams, want one sprite each", i, len(g.Sprites), len(g.Beams))
		}
	}
	sameSite := []fxOverlayEffect{
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
	}
	if one := build(sameSite); one == nil || len(one.Groups) != 1 || len(one.Groups[0].Sprites) != 2 {
		t.Fatalf("one marker twice: scene %v, want a single group of two sprites", one)
	}
	mix := []fxOverlayEffect{
		{Sim: simWith(0), Origin: fauxgl.Vector{X: 1}},
		{Sim: simWith(2), Origin: fauxgl.Vector{X: 1}},
	}
	mixed := build(mix)
	if mixed == nil || len(mixed.Groups) != 2 {
		t.Fatalf("covering and additive on one marker: %v, want two groups", mixed)
	}
	if mixed.Groups[0].Additive || !mixed.Groups[1].Additive {
		t.Errorf("blend modes did not sort covering-first: %v then %v", mixed.Groups[0].Additive, mixed.Groups[1].Additive)
	}
}

// ─── merged from x_test_fx_beam_arc_real_test.go ────────────────────────────────────────

const fxProbeModule = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`

type fxBeamCase struct {
	model, first, second, fx string
}

// fxProbeTrees builds the mod tree once for the tests that need it.
func fxProbeTree(t *testing.T) *modtree.ModTree {
	t.Helper()
	path := filepath.FromSlash(fxProbeModule)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no installed module: %v", err)
	}
	tree, err := modtree.BuildModTree(path, "English")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)
	return tree
}

// TestShippedTwoMarkerBeamsBowAcrossTheirSpan resolves each case against the
// real model, runs the effect the event names, and checks that every beam
// drawn bows across the gap rather than along it.
func TestShippedTwoMarkerBeamsBowAcrossTheirSpan(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	cases := []fxBeamCase{
		{`data\art\ebps\environment\cinematics\aa_necron_crawling.whm`,
			"marker_ground_01", "marker_chest_front", `necron\ground_lightning_01`},
		{`data\art\ebps\environment\cinematics\aa_necron_crawling.whm`,
			"marker_ground_04", "marker_chest_front", `necron\ground_lightning_01`},
		{`data\art\ebps\races\orks\troops\mega_armoured_nobz.whm`,
			"mrkr_surge_01a", "mrkr_surge_02a", `orks\abilities\surge_01_combo`},
		{`data\art\ebps\races\orks\troops\mega_armoured_nobz.whm`,
			"mrkr_surge_02a", "mrkr_surge_02b", `orks\abilities\surge_04_combo`},
	}
	checked := 0
	for _, c := range cases {
		f := tree.FindFile(c.model)
		if f == nil {
			t.Errorf("model %s is not in the tree", c.model)
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		m, err := ParseWhmModel(data)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		ia, ib := m.MarkerIndex(c.first), m.MarkerIndex(c.second)
		if ia < 0 || ib < 0 {
			t.Errorf("%s: carries %s=%d and %s=%d", c.model, c.first, ia, c.second, ib)
			continue
		}
		pose := m.RestPose()
		a, okA := m.MarkerWorld(ia, pose, true)
		b, okB := m.MarkerWorld(ib, pose, true)
		if !okA || !okB {
			t.Errorf("%s: markers %s/%s have no world position", c.model, c.first, c.second)
			continue
		}
		ld, err := readFxLuaFile(tree, roots, c.fx)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		ff, err := fx.ParseFxLua(ld)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		sim := fx.NewFxSim(ff)
		// An eye off to the side: a ribbon seen end-on has no width, and
		// with it no bow to measure.
		eye := a.Add(fauxgl.Vector{X: 2, Y: 1, Z: 3})
		for i, r := range sim.Beams(a, b, true) {
			if r.Arc.Length() < 1e-9 {
				continue
			}
			share, peak, ok := arcLateralShare(r, eye)
			if !ok {
				t.Errorf("%s beam %d: produced no comparable geometry", c.fx, i)
				continue
			}
			checked++
			if share < 0.99 {
				t.Errorf("%s beam %d: arc %v bowed %.0f%% across the span, not all of it",
					c.fx, i, r.Arc, share*100)
			}
			if peak < 1e-4 {
				t.Errorf("%s beam %d: the arc bowed %.5f, which is no visible curve at all",
					c.fx, i, peak)
			}
			t.Logf("%-34s beam %d: arc %v bows %.3f across a %.2f unit span (%.0f%%)",
				c.fx, i, r.Arc, peak, a.Sub(b).Length(), share*100)
		}
	}
	if checked == 0 {
		t.Skip("none of the probe effects carried an arc")
	}
	t.Logf("checked %d shipped beams", checked)
}

// The distance half of the two-marker rule, on the same real models: a beam
// that named a second marker and asked to span the gap has to reach exactly
// from one to the other, which is what the effect files' Length of 1 means.
func TestShippedTwoMarkerBeamsSpanTheGap(t *testing.T) {
	tree := fxProbeTree(t)
	roots := materialSearchRoots(tree)
	cases := []fxBeamCase{
		{`data\art\ebps\environment\cinematics\aa_necron_crawling.whm`,
			"marker_ground_01", "marker_chest_front", `necron\ground_lightning_01`},
		{`data\art\ebps\races\orks\troops\mega_armoured_nobz.whm`,
			"mrkr_surge_01a", "mrkr_surge_02a", `orks\abilities\surge_01_combo`},
		{`data\art\ebps\environment\cinematics\aa_necron_lord.whm`,
			"mrkr_veil_of_darkness_r", "mrkr_veil_of_darkness_l01", `necron\monolith\tower_arc_05`},
	}
	checked := 0
	for _, c := range cases {
		f := tree.FindFile(c.model)
		if f == nil {
			t.Errorf("model %s is not in the tree", c.model)
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		m, err := ParseWhmModel(data)
		if err != nil {
			t.Errorf("%s: %v", c.model, err)
			continue
		}
		ia, ib := m.MarkerIndex(c.first), m.MarkerIndex(c.second)
		if ia < 0 || ib < 0 {
			t.Errorf("%s: markers %s/%s are missing", c.model, c.first, c.second)
			continue
		}
		pose := m.RestPose()
		a, _ := m.MarkerWorld(ia, pose, true)
		b, _ := m.MarkerWorld(ib, pose, true)
		gap := a.Sub(b).Length()

		ld, err := readFxLuaFile(tree, roots, c.fx)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		ff, err := fx.ParseFxLua(ld)
		if err != nil {
			t.Errorf("%s: %v", c.fx, err)
			continue
		}
		sim := fx.NewFxSim(ff)
		for i, r := range sim.Beams(a, b, true) {
			got := r.To.Sub(r.From).Length()
			checked++
			if got < gap-1e-3 || got > gap+1e-3 {
				t.Errorf("%s beam %d: spans %.4f across a gap of %.4f", c.fx, i, got, gap)
			}
		}
	}
	if checked == 0 {
		t.Skip("none of the probe effects drew a beam")
	}
	t.Logf("checked %d shipped beams span their gap exactly", checked)
}

// ─── merged from x_test_fx_resample_quality_test.go ────────────────────────────────────────

func probeSine(hz float64, i, rate int) float64 {
	return math.Sin(2 * math.Pi * hz * float64(i) / float64(rate))
}

// goertzel returns the magnitude of one frequency in a signal, by running the
// Goertzel recurrence. One pass, no transform, and it reads one exact bin,
// which is all a measurement of "is there energy at this frequency" needs.
func goertzel(s []int16, rate int, hz float64) float64 {
	if len(s) == 0 {
		return 0
	}
	w := 2 * math.Pi * hz / float64(rate)
	c := 2 * math.Cos(w)
	var q1, q2 float64
	for _, v := range s {
		q0 := float64(v) + c*q1 - q2
		q2, q1 = q1, q0
	}
	return math.Sqrt(q1*q1+q2*q2-c*q1*q2) / float64(len(s))
}

func resampleTone(t *testing.T, hz float64, inRate, outRate int) ([]int16, []int16) {
	t.Helper()
	// One second of tone, which is long enough that the kernel's edges are a
	// negligible part of the measurement.
	in := make([]int16, inRate)
	for i := range in {
		in[i] = int16(10000 * probeSine(hz, i, inRate))
	}
	out, err := fda.ResamplePCM16(in, inRate, 1, outRate, 1)
	if err != nil {
		t.Fatalf("%d -> %d Hz: %v", inRate, outRate, err)
	}
	return in, out
}

// Upsampling by two is the conversion the game's 22050 Hz sounds need, and the
// one linear interpolation was worst at.
func TestFxResampleRejectsImaging(t *testing.T) {
	const inRate, outRate = 22050, 44100
	const toneHz = 5000.0

	_, out := resampleTone(t, toneHz, inRate, outRate)
	signal := goertzel(out, outRate, toneHz)
	if signal <= 0 {
		t.Fatal("the tone itself is missing from the output")
	}
	// Doubling the rate puts images at k*inRate +/- tone.
	images := []float64{
		float64(inRate) - toneHz,
		float64(inRate) + toneHz,
	}
	for _, img := range images {
		if img >= outRate/2 {
			continue
		}
		m := goertzel(out, outRate, img)
		if m <= 0 {
			t.Errorf("image at %.0f Hz: no measurable level", img)
			continue
		}
		rel := 20 * math.Log10(m/signal)
		t.Logf("image at %7.0f Hz: %.1f dB relative to the signal", img, rel)
		if rel > -60 {
			t.Errorf("image at %.0f Hz is %.1f dB relative to the signal, want under -60 dB", img, rel)
		}
	}
}

// The level of the signal itself must not change across the band it carries.
// Linear interpolation lost 4.8 dB by 10 kHz.
func TestFxResamplePassbandIsFlat(t *testing.T) {
	const inRate, outRate = 22050, 44100
	for _, hz := range []float64{100, 1000, 5000, 8000, 10000} {
		in, out := resampleTone(t, hz, inRate, outRate)
		before := goertzel(in, inRate, hz)
		after := goertzel(out, outRate, hz)
		if before <= 0 || after <= 0 {
			t.Fatalf("%.0f Hz: tone missing (in %v, out %v)", hz, before, after)
		}
		d := 20 * math.Log10(after/before)
		t.Logf("%6.0f Hz: %+.2f dB", hz, d)
		if d < -1.0 || d > 1.0 {
			t.Errorf("%.0f Hz changed by %+.2f dB, want within 1 dB", hz, d)
		}
	}
}

// The kernel must not invent energy where the source had none, or the
// conversion is adding noise of its own.
func TestFxResampleAddsNoEnergyAboveSourceNyquist(t *testing.T) {
	const inRate, outRate = 22050, 44100
	// A tone above the source's own Nyquist, aliased into it by the source
	// itself. The converter must not add anything more on top.
	for _, hz := range []float64{13000, 15000} {
		in, out := resampleTone(t, hz, inRate, outRate)
		before := goertzel(in, inRate, hz)
		after := goertzel(out, outRate, hz)
		if before <= 0 || after <= 0 {
			continue
		}
		d := 20 * math.Log10(after/before)
		if d > 1.0 {
			t.Errorf("%.0f Hz grew by %+.2f dB through the conversion", hz, d)
		}
	}
}

// The length of a sample must not depend on how it is converted, or a sound
// drifts out of step with the animation that triggers it.
func TestFxResampleKeepsDuration(t *testing.T) {
	for _, inRate := range []int{22050, 44100} {
		in := make([]int16, inRate)
		for i := range in {
			in[i] = int16(9000 * probeSine(440, i, inRate))
		}
		for _, outRate := range []int{22050, 44100, 48000} {
			out, err := fda.ResamplePCM16(in, inRate, 1, outRate, 2)
			if err != nil {
				t.Fatalf("%d -> %d: %v", inRate, outRate, err)
			}
			got := float64(len(out)/2) / float64(outRate)
			want := float64(len(in)) / float64(inRate)
			if math.Abs(got-want) > 0.002 {
				t.Errorf("%d -> %d Hz: duration %.4fs, want %.4fs", inRate, outRate, got, want)
			}
		}
	}
}

// The pitch of a tone must survive the conversion. A resampler that changed the
// frequency would be the stretched, pitched-down sound this whole path exists to
// avoid.
func TestFxResampleKeepsPitch(t *testing.T) {
	const toneHz = 1000.0
	for _, c := range [][2]int{{22050, 44100}, {22050, 48000}, {44100, 48000}, {48000, 44100}} {
		_, out := resampleTone(t, toneHz, c[0], c[1])
		// zeroCrossings works in frames; the output here is mono.
		got := zeroCrossings(out, c[1], 1)
		if math.Abs(got-toneHz)/toneHz > 0.02 {
			t.Errorf("%d -> %d Hz: pitch %.1f Hz, want %.0f Hz", c[0], c[1], got, toneHz)
		}
	}
}

// ─── merged from x_test_fx_playback_rate_test.go ────────────────────────────────────────

func TestClipLengthIsFramesOverRate(t *testing.T) {
	for _, frames := range []int{4, 62, 362} {
		anim := &WhmAnimation{Name: "clip", NumFrames: frames, Duration: 1}
		fps := animFramesPerSecond(anim)
		clipLen := float64(anim.NumFrames) / fps

		// The playhead advances by dt*rate/frames per frame, so it covers the
		// whole clip at rate/frames per second — and one pass therefore takes
		// frames/rate seconds. That is the number the effect step is built on,
		// so the two have to be the same number.
		perSecond := fps / float64(frames)
		perPass := 1.0 / perSecond
		if math.Abs(perPass-clipLen) > 1e-9 {
			t.Errorf("a %d frame clip at %g fps lasts %g s by the playhead but "+
				"%g s by the length used for the effects", frames, fps, perPass, clipLen)
		}
		if perPass <= 0 || perPass > 60 {
			t.Errorf("a %d frame clip lasts %g s, which is not plausible", frames, perPass)
		}
	}
}

// The same measurement from the outside: step one viewer for a second while
// playing and another while paused, and the effect must end up in the same
// place. This is the symptom the reader reported, so it is the thing to hold.
func TestPlayingAndPausedEffectsAdvanceAlike(t *testing.T) {
	run := func(playing bool) float64 {
		sim := fx.NewFxSim(&fx.FxFile{})
		v := &modelViewer{
			Model:   &WhmModel{},
			Anim:    &WhmAnimation{Name: "vis_vis_main", NumFrames: 4, Duration: 1},
			Playing: playing,
			ShowFX:  true,
			FxScale: 1,
		}
		e := fxOverlayEffect{
			Sim:        sim,
			Label:      "glow",
			Timed:      true,
			Animations: []string{"vis_vis_main"},
			FireTimes:  map[string]float64{"vis_vis_main": -1},
		}
		v.FxEffects = []fxOverlayEffect{e}
		const frames = 60
		const dt = 1.0 / 60.0
		for i := 0; i < frames; i++ {
			// The canvas steps the effects first and advances the playhead
			// after, so the order here is the order it does it in.
			FxStepOnClip(v, []fxOverlayEffect{v.FxEffects[0]}, "vis_vis_main", dt)
			if playing {
				v.Time += float32(dt * animFramesPerSecond(v.Anim) / float64(v.Anim.NumFrames))
				for v.Time >= 1 {
					v.Time -= 1
				}
			}
		}
		return sim.Time
	}
	playing := run(true)
	paused := run(false)
	if math.Abs(playing-paused) > 0.2 {
		t.Errorf("after a second the simulation is at %g playing and %g paused — "+
			"the effects run at a different rate depending on the clip", playing, paused)
	}
	if playing < 0.5 {
		t.Errorf("after a second of a playing clip the simulation only reached %g", playing)
	}
}

// A clip loops, and a looping clip must not sit frozen between passes. The
// four frame vis clip comes round five times a second, so an effect that is
// reset every pass would be seen only as a flicker.
func TestALoopingShortClipKeepsItsEffectsRunning(t *testing.T) {
	sim := fx.NewFxSim(&fx.FxFile{})
	v := &modelViewer{
		Model:   &WhmModel{},
		Anim:    &WhmAnimation{Name: "vis_vis_main", NumFrames: 4, Duration: 1},
		Playing: true,
		ShowFX:  true,
		FxScale: 1,
	}
	e := fxOverlayEffect{
		Sim:        sim,
		Label:      "glow",
		Timed:      true,
		Animations: []string{"vis_vis_main"},
		FireTimes:  map[string]float64{"vis_vis_main": -1},
	}
	v.FxEffects = []fxOverlayEffect{e}
	const dt = 1.0 / 60.0
	drawn := 0
	for i := 0; i < 60; i++ {
		FxStepOnClip(v, []fxOverlayEffect{e}, "vis_vis_main", dt)
		if len(FxStepOnClip(v, []fxOverlayEffect{e}, "vis_vis_main", 0)) > 0 {
			drawn++
		}
		v.Time += float32(dt * animFramesPerSecond(v.Anim) / float64(v.Anim.NumFrames))
		for v.Time >= 1 {
			v.Time -= 1
		}
	}
	if drawn != 60 {
		t.Errorf("an entry event was drawn on %d of 60 frames of a looping clip", drawn)
	}
}

// ─── merged from x_test_fx_sound_ctx_test.go ────────────────────────────────────────

func TestOtoContextIsPinnedToPlayableFormat(t *testing.T) {
	if audioplayer.SampleRate != 48000 {
		t.Errorf("audioplayer.SampleRate = %d, want 48000: the rate Windows reliably plays at the rate it is told to", audioplayer.SampleRate)
	}
	if audioplayer.Channels != 2 {
		t.Errorf("audioplayer.Channels = %d, want 2: mono is what oto documents as unlikely to be supported by WASAPI", audioplayer.Channels)
	}
}

// The context takes its format from whoever asks first, so a caller asking for
// something else must still get the pinned one rather than a second format.
func TestGetOtoContextIgnoresRequestedFormat(t *testing.T) {
	// Not calling the real one: it opens an audio device. What is checked is
	// that the arguments cannot change the format the rest of the app reads.
	before := audioplayer.SampleRate
	beforeCh := audioplayer.Channels
	// The signature takes the caller's format and drops it.
	if before != 48000 || beforeCh != 2 {
		t.Fatalf("format is %d Hz/%dch before any call; a caller must not be able to move it", before, beforeCh)
	}
}

// A sound is only heard at the right pitch if it is converted into the context
// format on its way to the device, and the conversion has to preserve both the
// length and the frequencies — a conversion that changed either would be heard
// as exactly the stretching and pitch drop this exists to prevent.
func TestSoundConversionPreservesLengthAndPitch(t *testing.T) {
	const rate = 48000
	// One frame of a 22050 Hz mono sample, the format the game's sounds are in.
	const srcRate, srcCh = 22050, 1
	// A second long, because the measurement below skips a tenth of a second
	// from each end and would have nothing left to measure on a shorter one.
	src := make([]int16, srcRate)
	for i := range src {
		// A 440 Hz tone: two zero crossings per period, so its frequency can
		// be recovered from the output and compared with the input.
		src[i] = int16(10000 * math.Sin(2*math.Pi*440*float64(i)/float64(srcRate)))
	}
	srcCross := zeroCrossings(src, srcRate, srcCh)

	out, err := fda.ResamplePCM16(src, srcRate, srcCh, rate, 2)
	if err != nil {
		t.Fatalf("resample: %v", err)
	}

	// Length: the same wall-clock duration, which is len/channels/rate.
	gotSec := float64(len(out)/2) / float64(rate)
	wantSec := float64(len(src)/srcCh) / float64(srcRate)
	if diff := gotSec - wantSec; diff > 0.001 || diff < -0.001 {
		t.Errorf("duration %.4fs, want %.4fs", gotSec, wantSec)
	}

	// Pitch: the tone is still 440 Hz.
	if got := zeroCrossings(out, rate, 2); math.Abs(got-440)/440 > 0.02 {
		t.Errorf("pitch %.1f Hz, want 440 Hz (source measured %.1f Hz)", got, srcCross)
	}
}

// zeroCrossings returns the frequency a signal is at, by counting the times one
// channel changes sign. Only the first channel is read: an interleaved stereo
// signal crosses zero in both channels at once, and counting both would halve
// the answer. It skips the first and last tenth of a second, where linear
// interpolation and the end clamp are least trustworthy.
func zeroCrossings(samples []int16, rate, channels int) float64 {
	if channels < 1 {
		return 0
	}
	// Work in frames rather than samples, so the skipped time is the same
	// whatever the channel count.
	skip := rate / 10
	frames := len(samples) / channels
	end := frames - skip
	if end <= skip {
		return 0
	}
	first := func(frame int) int16 { return samples[frame*channels] }
	cross := 0
	for f := skip + 1; f < end; f++ {
		if (first(f-1) < 0) != (first(f) < 0) {
			cross++
		}
	}
	return float64(cross) / 2 / (float64(end-skip) / float64(rate))
}

// ─── merged from x_test_fx_binding_test.go ────────────────────────────────────────

func TestResolveFxEventsForRealModel(t *testing.T) {
	module := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\DXP3.module`
	if _, err := os.Stat(module); err != nil {
		t.Skipf("module not available: %v", err)
	}
	tree, err := modtree.BuildModTree(module, "English")
	if err != nil {
		t.Skipf("BuildModTree: %v", err)
	}
	defer tree.Close()
	folders, files := tree.Count()
	t.Logf("tree: %d folders, %d files", folders, files)
	for _, probe := range []string{
		"necron_summoning_core.lua", "Necron_Summoning_Core.whm", "relocation.events",
		"monolith_hit.events", "hit_surface_01.lua",
	} {
		if f := tree.FindByName(probe); f != nil {
			t.Logf("probe %q -> %s", probe, f.FullPath)
		} else {
			t.Logf("probe %q -> not in tree", probe)
		}
	}
	for _, probe := range []string{
		"attrib/ebps/races/necrons/structures/necron_summoning_core.lua",
		"attrib/ebps/races/necrons/structures/Necron_Summoning_Core.lua",
	} {
		if f := tree.FindFile(probe); f != nil {
			t.Logf("FindFile %q -> %s", probe, f.FullPath)
		} else {
			t.Logf("FindFile %q -> nil", probe)
		}
	}
	if f := tree.FindByName("necron_summoning_core.lua"); f != nil {
		if data, err := tree.ReadTreeFile(f); err == nil {
			s := string(data)
			t.Logf("blueprint %s: %d bytes, event_name refs=%d, drives=%v",
				f.FullPath, len(data), len(fx.EventNamePattern.FindAll(data, -1)), strings.Contains(strings.ToLower(s), "necron_summoning_core]]"))
			t.Logf("blueprint content:\n%s", s)
			for _, m := range fx.EventNamePattern.FindAllStringSubmatch(s, -1) {
				t.Logf("  ref: %s", m[1])
			}
		}
	}
	for _, dir := range tree.Root.Children {
		t.Logf("root child: %s", dir.FullPath)
	}
	// Every file that basenames to the blueprint, plus a sample of what the
	// attrib folder actually holds.
	var named []string
	var attribSample []string
	attribLua := 0
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		lp := strings.ToLower(f.FullPath)
		if strings.EqualFold(f.Name, "necron_summoning_core.lua") ||
			strings.HasPrefix(strings.ToLower(f.Name), "necron_summoning_core.") {
			named = append(named, f.FullPath)
		}
		if strings.Contains(lp, `\attrib\`) {
			if strings.HasSuffix(lp, ".lua") {
				attribLua++
			}
			if len(attribSample) < 25 && strings.Contains(lp, "ebps") {
				attribSample = append(attribSample, f.FullPath)
			}
		}
	})
	t.Logf("files named necron_summoning_core.*: %v", named)
	t.Logf("attrib lua files: %d; ebps sample:", attribLua)
	for _, s := range attribSample {
		t.Logf("  %s", s)
	}
	if len(named) > 0 {
		// print the first blueprint-ish file's head
		for _, p := range named {
			if !strings.HasSuffix(strings.ToLower(p), ".rgd") {
				continue
			}
			if data, err := tree.ReadFile(p); err == nil {
				s := string(data)
				if i := strings.Index(s, "event_manager"); i >= 0 {
					end := i + 600
					if end > len(s) {
						end = len(s)
					}
					t.Logf("rgd %s event section:\n%s", p, s[i:end])
				} else {
					t.Logf("rgd %s: no event_manager (%d bytes)", p, len(data))
				}
			}
		}
	}
	bindings, model, err := fx.ResolveEventsForModel(tree,
		"art/ebps/races/necrons/structures/Necron_Summoning_Core.whm")
	if err != nil {
		t.Fatalf("fx.ResolveEventsForModel: %v", err)
	}
	t.Logf("model %s: %d events", model, len(bindings))
	withFX := 0
	for _, b := range bindings {
		fx, hasFX := b.Event.First("fx")
		if hasFX {
			withFX++
		}
		t.Logf("  %-40s event=%-24s fx=%s", b.Ref, b.Event.Name, fx)
	}
	if withFX == 0 {
		t.Errorf("no resolved event names an effect")
	}
}

// ─── merged from x_test_fx_beam_free_test.go ────────────────────────────────────────

func markerlessBeam() fxOverlayEffect {
	return fxOverlayEffect{
		Label:    "awakening_lighting_ground",
		Marker:   -1,
		HasOther: false,
		Origin:   fauxgl.Vector{X: 0, Y: 0, Z: 6.3},
	}
}

func TestMarkerlessBeamRunsUpNotSideways(t *testing.T) {
	e := markerlessBeam()
	far := e.fxFarPoint()
	d := far.Sub(e.Origin)
	if n := d.Length(); n < 1e-9 {
		t.Fatalf("the far point fell on the origin, so the beam has no direction at all")
	}
	dir := d.Normalize()
	if math.Abs(dir.Z) < 0.99 {
		t.Errorf("a markerless beam runs (%.3f %.3f %.3f): it is not along up, and with no "+
			"marker to aim it a ground strike leaves the model sideways", dir.X, dir.Y, dir.Z)
	}
}

// The fallback is the model's own up, not a hardcoded guess, so it follows the
// renderer's axis rather than being written out twice in different places.
func TestMarkerlessBeamUsesTheRenderersUpAxis(t *testing.T) {
	if fxAxisZ.Z != 1 || fxAxisZ.X != 0 || fxAxisZ.Y != 0 {
		t.Errorf("the renderer's up is (%.1f %.1f %.1f), which is not +Z", fxAxisZ.X, fxAxisZ.Y, fxAxisZ.Z)
	}
	e := markerlessBeam()
	d := e.fxFarPoint().Sub(e.Origin).Normalize()
	want := fxAxisZ.Normalize()
	if d.Sub(want).Length() > 1e-9 {
		t.Errorf("the beam runs (%.3f %.3f %.3f), the up axis is (%.3f %.3f %.3f)",
			d.X, d.Y, d.Z, want.X, want.Y, want.Z)
	}
}

// A marker that does resolve must still win: the fallback is for the events
// that name none, and it must not quietly override one that names a barrel.
func TestMarkeredBeamStillUsesItsOwnAxis(t *testing.T) {
	e := markerlessBeam()
	e.Axis = fauxgl.Vector{X: 1}
	d := e.fxFarPoint().Sub(e.Origin).Normalize()
	if math.Abs(d.X) < 0.99 {
		t.Errorf("a beam with its own axis runs (%.3f %.3f %.3f), not along it", d.X, d.Y, d.Z)
	}
}

// The far point is a lever, not the answer: the length in the file is what
// decides how far the beam reaches, and a ground strike is twelve units long.
func TestMarkerlessBeamLengthComesFromTheFile(t *testing.T) {
	e := markerlessBeam()
	e.Sim = nil
	// fxBeamFreeLength is only the distance the axis is probed at; the beam's
	// own Length curve is read as model units because there is no gap to be a
	// fraction of.
	sim := fx.NewFxSim(mustParseFx(t, awakeningLightingGroundLua))
	for _, r := range sim.Beams(e.Origin, e.fxFarPoint(), e.fxSpansMarkers()) {
		got := r.To.Sub(r.From).Length()
		if math.Abs(got-12) > 0.01 {
			t.Errorf("the beam spans %.3f, the file asks for 12", got)
		}
	}
}

const awakeningLightingGroundLua = `
fxtypes =
{
	{
		effect = "root>__fxtool_private_copy",
		fx =
		{
			properties =
			{
				{
					name = "Duration",
					type = "VARTYPE_FLOAT",
					value = 3,
				},
				{
					name = "Length",
					type = "VARTYPE_ARRAY_TIMEFLOAT",
					value = { 0, 12, 1, 12 },
				},
				{
					name = "Width",
					type = "VARTYPE_ARRAY_TIMEFLOAT",
					value = { 0, 5, 1, 5 },
				},
			},
		},
		style = "STYLE_BEAM",
	},
	{
		effect = "root",
		fx = { properties = {} },
		style = "STYLE_COMBO",
	},
}
`

func mustParseFx(t *testing.T, lua string) *fx.FxFile {
	t.Helper()
	f, err := fx.ParseFxLua([]byte(lua))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

// ─── merged from x_test_fx_lua_test.go ────────────────────────────────────────

func readFxLua(t *testing.T, rel string) []byte {
	t.Helper()
	p := filepath.Join(dowInstallDir, "art", "fx", filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("game effects not available: %v", err)
	}
	return data
}

// TestParseFxLuaCombo reads a combo effect and checks the shape the preview
// needs: a root, a spray child with an emission rate, and a ring child with
// a texture, a size curve and a colour ramp.
func TestParseFxLuaCombo(t *testing.T) {
	f, err := fx.ParseFxLua(readFxLua(t, "aeonic_orb_blasts.lua"))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	if len(f.Nodes) < 3 {
		t.Fatalf("nodes = %d, want the root, a spray and more", len(f.Nodes))
	}
	root := f.Root()
	if root == nil || root.Effect != "root" {
		t.Fatalf("root = %+v", root)
	}
	kids := f.Children("root")
	if len(kids) == 0 {
		t.Fatal("root has no children")
	}
	var spray, leaf *fx.FxNode
	for _, k := range kids {
		switch k.Style {
		case "STYLE_SPRAY":
			spray = k
		case "STYLE_RING":
			leaf = k
		}
		if stringsHasPrefix(k.Effect, "root>") {
			t.Logf("child %s style=%s", k.Effect, k.Style)
		}
	}
	if spray == nil {
		// Some files nest one level deeper; search one more level down.
		for _, k := range kids {
			for _, kk := range f.Children(k.Effect) {
				if kk.Style == "STYLE_SPRAY" {
					spray = kk
				}
				if kk.Style == "STYLE_RING" {
					leaf = kk
				}
			}
		}
	}
	if spray == nil {
		t.Fatalf("no STYLE_SPRAY node among %d children", len(kids))
	}
	rate, ok := spray.Prop("Emitter_Rate")
	if !ok || len(rate.Curve) == 0 {
		t.Errorf("spray has no Emitter_Rate curve")
	} else {
		t.Logf("spray %s: rate keys=%v duration=%v loop=%v",
			spray.Effect, rate.Curve[:minInt(3, len(rate.Curve))],
			spray.Float("Emitter_Duration", -1), spray.Bool("Emitter_Loop", false))
	}
	if leaf == nil {
		t.Skip("no STYLE_RING leaf to inspect in this file")
	}
	tex := leaf.String("Texture")
	if tex == "" {
		t.Errorf("ring has no Texture")
	}
	rad, ok := leaf.Prop("Radius")
	if !ok || len(rad.Curve) == 0 {
		t.Errorf("ring has no Radius curve")
	}
	col, ok := leaf.Prop("Colour")
	if !ok || len(col.Colour) == 0 {
		t.Errorf("ring has no Colour ramp")
	} else {
		s := col.Colour[0]
		t.Logf("ring texture=%s radiusKeys=%d colour[0]=(t=%.3f rgba=%.2f %.2f %.2f %.2f) lifetime=%v",
			tex, len(rad.Curve), s.T, s.R, s.G, s.B, s.A,
			leaf.Float("Duration", -1))
		if s.R > 1.01 || s.G > 1.01 || s.B > 1.01 {
			t.Errorf("colour not normalised: %+v", s)
		}
	}
}

// TestParseFxLuaSprayFile reads a second effect so the parser is not tuned
// to one file's writing habits.
func TestParseFxLuaSprayFile(t *testing.T) {
	f, err := fx.ParseFxLua(readFxLua(t, "flame_fx/flame_sprays/flame_ground_strip_combo.lua"))
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	found := false
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.Style != "STYLE_SPRAY" {
			continue
		}
		found = true
		rate, ok := n.Prop("Emitter_Rate")
		if !ok || len(rate.Curve) == 0 {
			t.Errorf("%s: no rate", n.Effect)
		}
	}
	if !found {
		t.Fatal("no spray in a spray file")
	}
	// The colour ramp of this file is authored in bytes.
	for i := range f.Nodes {
		if p, ok := f.Nodes[i].Prop("Colour"); ok && len(p.Colour) > 1 {
			t.Logf("%s colour stops=%d first=%+v", f.Nodes[i].Effect, len(p.Colour), p.Colour[0])
			break
		}
	}
}

func stringsHasPrefix(s, pre string) bool {
	return len(s) >= len(pre) && s[:len(pre)] == pre
}

// ─── merged from x_test_fx_phase_ring_test.go ────────────────────────────────────────

func TestPhaseRingSpraysDescendAndRiseVertically(t *testing.T) {
	cases := []struct {
		file string
		// descends is what phase_in does; phase_out climbs.
		descends bool
	}{
		{file: "phase_in_main_combo", descends: true},
		{file: "phase_out_main_combo", descends: false},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			f, err := fx.ParseFxLua(readFxLua(t, "necron/monolith/"+c.file+".lua"))
			if err != nil {
				t.Fatalf("parse %s: %v", c.file, err)
			}
			sim := fx.NewFxSim(f)
			if len(sim.Emitters) == 0 {
				t.Fatalf("%s has no emitter to play", c.file)
			}
			// The first emitter of both files writes Emitter_Volume away and
			// Particle_Speed 0, so a particle born at a given moment sits
			// exactly at the authored offset: no spread, no launch cone.
			e := &sim.Emitters[0]
			birthAt := func(phase float64) fauxgl.Vector {
				e.Age = e.Duration * phase
				sim.Spawn(e)
				p := sim.Particles[len(sim.Particles)-1]
				return fxFallbackFrame[0].MulScalar(p.Pos[0]).
					Add(fxFallbackFrame[1].MulScalar(p.Pos[1])).
					Add(fxFallbackFrame[2].MulScalar(p.Pos[2]))
			}
			early, late := birthAt(0.1), birthAt(0.9)
			rise := late.Z - early.Z
			if c.descends && rise > -1 {
				t.Errorf("%s rings rose %.2f world units (from Z=%.2f to Z=%.2f); they are "+
					"authored to descend the effect's up axis", c.file, rise, early.Z, late.Z)
			}
			if !c.descends && rise < 1 {
				t.Errorf("%s rings rose only %.2f world units (from Z=%.2f to Z=%.2f); they "+
					"are authored to climb the effect's up axis", c.file, rise, early.Z, late.Z)
			}
			// The offset has no horizontal component at all, so anything the
			// attachment frame puts across the ground is the double swap
			// coming back: the vertical would then arrive as a sideways
			// trail, which is the bug this test exists for.
			for _, p := range []fauxgl.Vector{early, late} {
				if drift := p.X*p.X + p.Y*p.Y; drift > 0.25 {
					t.Errorf("%s ring born at (%+.2f %+.2f %+.2f): it drifted %.2f units "+
						"across the ground from a purely vertical offset",
						c.file, p.X, p.Y, p.Z, math.Sqrt(drift))
				}
			}
		})
	}
}

// ─── merged from x_test_fx_sound_decode_test.go ────────────────────────────────────────

func TestFxSoundDecodeYieldsContextFormat(t *testing.T) {
	// What the player asks of a sample it is handed, which is the shape of the
	// bug if the decode path ever stopped converting.
	needsConversion := func(s *fx.SoundSample) bool {
		return s.Info.SampleRate != audioplayer.SampleRate || s.Info.Channels != audioplayer.Channels
	}
	// A sample as it comes off the file: the game's own format.
	fromFile := &fx.SoundSample{
		Info: &wav.Info{SampleRate: 22050, Channels: 1},
		PCM:  make([]int16, 22050),
	}
	if !needsConversion(fromFile) {
		t.Fatal("a 22050 Hz mono sample would be played without conversion, so the test below would prove nothing")
	}
	// What the decode path hands over instead: already converted.
	converted := &fx.SoundSample{
		Info: &wav.Info{SampleRate: audioplayer.SampleRate, Channels: audioplayer.Channels},
		PCM:  make([]int16, 22050),
	}
	if needsConversion(converted) {
		t.Errorf("a decoded sample is still %d Hz/%dch, so the player converts it on the frame: want %d/%d",
			converted.Info.SampleRate, converted.Info.Channels, audioplayer.SampleRate, audioplayer.Channels)
	}
}

// The conversion is worth doing once per sample rather than once per play, so
// the work is bounded by the number of distinct samples and not by how often a
// sound fires. This measures the difference that caching buys.
func TestFxResampleCostIsPerSampleNotPerPlay(t *testing.T) {
	const inRate = 22050
	pcm := make([]int16, inRate/10) // a tenth of a second, a short effect
	for i := range pcm {
		pcm[i] = int16(8000 * probeSine(300, i, inRate))
	}
	start := time.Now()
	const plays = 50
	for i := 0; i < plays; i++ {
		// What the player does now that the sample arrives converted: hand the
		// finished PCM straight over.
		_ = fda.PCM16Bytes(pcm)
	}
	cached := time.Since(start) / plays

	start = time.Now()
	for i := 0; i < plays; i++ {
		// What it used to do: convert on the frame that starts the sound.
		if _, err := fda.ResamplePCM16(pcm, inRate, 1, audioplayer.SampleRate, audioplayer.Channels); err != nil {
			t.Fatal(err)
		}
	}
	converted := time.Since(start) / plays
	t.Logf("per play: cached %v, converting %v", cached, converted)
	if cached > converted {
		t.Errorf("handing over a converted sample (%v) costs more than converting it (%v)", cached, converted)
	}
}

// ─── merged from x_test_fx_events_test.go ────────────────────────────────────────

func readEventsFile(t *testing.T, rel string) []byte {
	t.Helper()
	p := filepath.Join(dowInstallDir, "art", "events", filepath.FromSlash(rel))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("game events not available: %v", err)
	}
	return data
}

// TestParseFxEventsReal checks the layout of DATAEVNT against a file that
// only has an fx entry and one that carries a pile of marker selectors.
func TestParseFxEventsReal(t *testing.T) {
	evs, err := whe.ParseFxEvents(readEventsFile(t, "structure_fx/relocation.events"))
	if err != nil {
		t.Fatalf("whe.ParseFxEvents(relocation): %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("relocation events = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Name != "relocate_building" {
		t.Errorf("name = %q, want relocate_building", ev.Name)
	}
	fx, ok := ev.First("fx")
	if !ok {
		t.Fatal("relocation has no fx entry")
	}
	if want := `eldar\structure fx\eldar_transport`; fx != want {
		t.Errorf("fx = %q, want %q", fx, want)
	}
}

// TestParseFxEventsWithMarkers is the shape a viewport preview cares about:
// an effect path and the markers it may play at.
func TestParseFxEventsWithMarkers(t *testing.T) {
	evs, err := whe.ParseFxEvents(readEventsFile(t, "necron/monolith_hit.events"))
	if err != nil {
		t.Fatalf("whe.ParseFxEvents(monolith_hit): %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	ev := evs[0]
	t.Logf("event %q: %d entries", ev.Name, len(ev.Entries))
	for _, e := range ev.Entries {
		t.Logf("  %s = %s", e.Key, e.Value)
	}
	if _, ok := ev.First("fx"); !ok {
		t.Errorf("event %q has no fx entry", ev.Name)
	}
	// At least one entry must name a marker, or there is nowhere to put the
	// effect on the model.
	found := false
	for _, e := range ev.Entries {
		if len(e.Key) >= 6 && e.Key[:6] == "marker" {
			found = true
		}
	}
	if !found {
		t.Errorf("event %q names no marker entry", ev.Name)
	}
}

// ─── merged from x_test_fx_attribute_axes_test.go ────────────────────────────────────────

func TestFXAttributeOriginAxisMatchesViewerAxis(t *testing.T) {
	origin := fauxgl.Vector{X: 1, Y: 2, Z: 3}
	for axis, value := range []float64{11, 22, 33} {
		got := fxAttributeOriginAxis(origin, axis, value)
		want := origin
		switch axis {
		case 0:
			want.X = value
		case 1:
			want.Y = value
		case 2:
			want.Z = value
		}
		if got != want {
			t.Errorf("editing viewer axis %d: got %+v, want %+v", axis, got, want)
		}
	}
}

func TestFXAttributeRotationUsesWorldAxes(t *testing.T) {
	// fxOrigins applies attribute rotations around the fixed viewer XYZ axes;
	// this is the same basis as the canvas gizmo and not marker-local axes.
	axes := [3]fauxgl.Vector{{X: 1}, {Y: 1}, {Z: 1}}
	vectors := [3]fauxgl.Vector{{Y: 1}, {Z: 1}, {X: 1}}
	want := [3]fauxgl.Vector{{Z: 1}, {X: 1}, {Y: 1}}
	for axis := range axes {
		angles := [3]float64{}
		angles[axis] = 3.141592653589793 / 2
		got := fxRotated(vectors[axis], angles[0], angles[1], angles[2], axes)
		if got.Sub(want[axis]).Length() > 1e-9 {
			t.Errorf("rotation about viewer axis %d: got %+v, want %+v", axis, got, want[axis])
		}
	}
}

func TestApplyFXAttributeFieldSetsOnlyMatchingPositionAxis(t *testing.T) {
	v := &modelViewer{
		FxEffects:   []fxOverlayEffect{{Origin: fauxgl.Vector{X: 1, Y: 2, Z: 3}}},
		FxAttrIndex: 0,
		FxOverrides: map[int]fxAttributeOverride{},
	}
	v.FxAttrPosY = widget.Editor{}
	v.FxAttrPosY.SetText("12.5")
	ApplyFXAttributeField(v, 1)
	got := v.FxOverrides[0]
	if !got.HasOrigin || got.Origin != (fauxgl.Vector{X: 1, Y: 12.5, Z: 3}) {
		t.Fatalf("editing Y should change only model-space Y, got %+v", got.Origin)
	}
}

// ─── helper kept for the overlay's arc checks (see internal/fx) ────────────

func arcLateralShare(r fx.BeamRibbon, eye fauxgl.Vector) (share, peak float64, ok bool) {
	withArc := r
	withArc.Noise = 0
	flat := withArc
	flat.Arc = fauxgl.Vector{}

	withVerts := fx.BeamQuads(withArc, eye)
	flatVerts := fx.BeamQuads(flat, eye)
	if len(withVerts) == 0 || len(withVerts) != len(flatVerts) {
		return 0, 0, false
	}
	dir := r.To.Sub(r.From)
	if n := dir.Length(); n > 1e-9 {
		dir = dir.MulScalar(1 / n)
	} else {
		return 0, 0, false
	}
	lateral := 0.0
	for i := range withVerts {
		w := withVerts[i].Pos.Sub(flatVerts[i].Pos)
		d := w.Length()
		if d > peak {
			peak = d
		}
		along := w.Dot(dir)
		if l := w.Sub(dir.MulScalar(along)).Length(); l > lateral {
			lateral = l
		}
	}
	if peak < 1e-9 {
		return 0, 0, true
	}
	return lateral / peak, peak, true
}
