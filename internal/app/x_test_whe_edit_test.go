package app

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/whe"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"

	"gioui.org/op"
)

// restoredMonolithWhe loads the installed restored monolith .whe, or the copy
// extracted into the workspace. The file is where the request started: its
// phase_out fires the crystal set, its phase_in does not, and the editor has
// to be able to move that set across.
func restoredMonolithWhe(t *testing.T) []byte {
	t.Helper()
	candidates := []string{
		".freebuff/phase_in_inspect/restored/art/ebps/races/necrons/troops/necron_restored_monolith.whe",
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3/Data/art/ebps/races/necrons/troops/necron_restored_monolith.whe`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP2/data/art/ebps/races/necrons/troops/necron_restored_monolith.whe`,
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(filepath.FromSlash(p)); err == nil {
			return data
		}
	}
	t.Skip("restored monolith .whe is not available on this machine")
	return nil
}

// syntheticEventWhe is a small .whe with the same shape as a shipped one: a
// FOLDEVCT of event definitions and a FOLDMTRE of timelines, built so the
// editor's tests run without the game installed.
func syntheticEventWhe() []byte {
	fx := &chunky.Chunk{TypeID: "FOLDEVCT", Version: 1}
	fx.Children = append(fx.Children,
		&chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: "phase_in_main", Payload: whe.FxPayload([]whe.FxEventEntry{
			{Key: "fx_motion_orphan", Value: "yes"},
			{Key: "fx", Value: `necron\monolith\phase_in_main_combo`},
		})},
		&chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: "sound_phase_in", Payload: whe.SoundPayload(`races\npc\necron_monolith\phase_in`)},
		&chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: "crystal_top_01", Payload: whe.FxPayload([]whe.FxEventEntry{
			{Key: "marker", Value: "marker_gem_top_01"},
			{Key: "fx", Value: `necron\monolith\crystal_top_panel_01`},
		})},
		&chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: "crystal_interior", Payload: whe.FxPayload([]whe.FxEventEntry{
			{Key: "marker", Value: "mrkr_crystal_ctr"},
			{Key: "fx", Value: `necron\monolith\crystal_interior_3`},
		})},
	)
	mton := func(name string, events []whe.FxTimelineEvent) *chunky.Chunk {
		payload := (&whe.Mton{Animations: []string{name}, Events: events}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 1, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	mt.Children = append(mt.Children,
		mton("phase_in", []whe.FxTimelineEvent{
			{Name: "phase_in_main", Time: 0},
			{Name: "sound_phase_in", Time: 0},
		}),
		mton("phase_out", []whe.FxTimelineEvent{
			{Name: "crystal_top_01", Time: -1},
			{Name: "crystal_interior", Time: -1},
		}),
	)
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{fx, mt}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	return buf.Bytes()
}

// timelineByName finds one timeline of a parsed .whe, failing the test when it
// is missing.
func timelineByName(t *testing.T, tls []whe.FxTimeline, name string) whe.FxTimeline {
	t.Helper()
	for _, tl := range tls {
		if strings.EqualFold(tl.Name, name) {
			return tl
		}
	}
	t.Fatalf("no timeline named %q in the file (have %v)", name, timelineNames(tls))
	return whe.FxTimeline{}
}

func timelineNames(tls []whe.FxTimeline) []string {
	out := make([]string, 0, len(tls))
	for _, tl := range tls {
		out = append(out, tl.Name)
	}
	return out
}

func findEvent(t whe.FxTimeline, name string) (whe.FxTimelineEvent, bool) {
	for _, e := range t.Events {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return whe.FxTimelineEvent{}, false
}

// TestWheEditRoundTripKeepsEveryByte is the safety property of the whole
// editor: before anything is changed, reading the real file and writing it
// back must produce identical bytes, or a save would rewrite every chunk of a
// model's events file on trust.
func TestWheEditRoundTripKeepsEveryByte(t *testing.T) {
	data := restoredMonolithWhe(t)
	doc, err := whe.ParseDocument(data)
	if err != nil {
		t.Fatalf("whe.ParseDocument: %v", err)
	}
	out, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !bytes.Equal(data, out) {
		t.Fatalf("an untouched .whe did not round-trip: %d bytes in, %d bytes out", len(data), len(out))
	}
	// The parse has to see the same content the readers of the file do.
	if len(doc.Timelines()) != len(whe.ParseFxTimelines(data)) {
		t.Errorf("document sees %d timelines, the file parses %d", len(doc.Timelines()), len(whe.ParseFxTimelines(data)))
	}
	if len(doc.Events()) != len(mustParseFxEvents(t, data)) {
		t.Errorf("document sees %d definitions, the file parses %d", len(doc.Events()), len(mustParseFxEvents(t, data)))
	}
}

func mustParseFxEvents(t *testing.T, data []byte) map[string]whe.FxEvent {
	t.Helper()
	evs, err := whe.ParseFxEvents(data)
	if err != nil {
		t.Fatalf("whe.ParseFxEvents: %v", err)
	}
	out := map[string]whe.FxEvent{}
	for _, ev := range evs {
		out[strings.ToLower(ev.Name)] = ev
	}
	return out
}

// TestAddAndRemoveTimedEventsRoundTrip is the editor's core: a new effect with
// a marker, a new sound, and a re-fired existing definition all survive a
// marshal/reparse, the untouched events of the other timeline stay exactly as
// they were, and the refusals refuse.
func TestAddAndRemoveTimedEventsRoundTrip(t *testing.T) {
	doc, err := whe.ParseDocument(syntheticEventWhe())
	if err != nil {
		t.Fatalf("whe.ParseDocument: %v", err)
	}
	if err := doc.AddTimedEvent("phase_in", whe.FxTimelineEvent{Name: "editor_glow", Time: 1.25},
		&whe.FxEvent{Entries: []whe.FxEventEntry{
			{Key: "marker", Value: "mrkr_gem_top"},
			{Key: "fx", Value: `necron\monolith\editor_glow`},
		}}, ""); err != nil {
		t.Fatalf("add FX: %v", err)
	}
	if err := doc.AddTimedEvent("phase_in", whe.FxTimelineEvent{Name: "sound_editor", Time: 2.5}, nil,
		`races\npc\editor`); err != nil {
		t.Fatalf("add sound: %v", err)
	}
	// Re-firing an event the file already defines needs no new definition:
	// this is how the crystal set is put on a second clip.
	if err := doc.AddTimedEvent("phase_out", whe.FxTimelineEvent{Name: "crystal_top_01", Time: 0.5}, nil, ""); err != nil {
		t.Fatalf("re-fire existing definition: %v", err)
	}
	// The refusals.
	if err := doc.AddTimedEvent("phase_in", whe.FxTimelineEvent{Name: "never_defined", Time: 0}, nil, ""); err == nil {
		t.Error("an event with no definition anywhere was accepted")
	}
	if err := doc.AddTimedEvent("phase_in", whe.FxTimelineEvent{Name: "editor_glow", Time: 0},
		&whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "fx", Value: `other\fx`}}}, ""); err == nil {
		t.Error("a conflicting definition for an existing event was accepted")
	}
	if err := doc.AddTimedEvent("phase_in", whe.FxTimelineEvent{Name: "editor_glow", Time: -2},
		&whe.FxEvent{Entries: []whe.FxEventEntry{{Key: "fx", Value: `necron\monolith\editor_glow`}}}, ""); err == nil {
		t.Error("a time before on-enter was accepted")
	}
	if err := doc.AddTimedEvent("phase_in", whe.FxTimelineEvent{Name: "footstep", Time: 0}, nil,
		`races\npc\editor`); err == nil {
		t.Error("a sound event named without the sound_ prefix was accepted")
	}

	out, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	tls := whe.ParseFxTimelines(out)
	phaseIn := timelineByName(t, tls, "phase_in")
	if len(phaseIn.Events) != 4 {
		t.Fatalf("phase_in has %d events (%v), want the two originals plus the two added", len(phaseIn.Events), phaseIn.Events)
	}
	ev, ok := findEvent(phaseIn, "editor_glow")
	if !ok || ev.Time != 1.25 {
		t.Errorf("editor_glow = %+v, want it at 1.25s", ev)
	}
	if ev, ok = findEvent(phaseIn, "sound_editor"); !ok || ev.Time != 2.5 {
		t.Errorf("sound_editor = %+v, want it at 2.5s", ev)
	}
	// The originals of the same timeline are still there.
	if _, ok = findEvent(phaseIn, "phase_in_main"); !ok {
		t.Error("phase_in lost its original phase_in_main event")
	}
	if _, ok = findEvent(phaseIn, "sound_phase_in"); !ok {
		t.Error("phase_in lost its original sound_phase_in event")
	}
	// The other timeline gained the re-fired crystal and lost nothing. A
	// second firing of the same name is how a clip plays a sound more than
	// once, so both moments must be there.
	phaseOut := timelineByName(t, tls, "phase_out")
	var crystalTimes []float64
	for _, e := range phaseOut.Events {
		if strings.EqualFold(e.Name, "crystal_top_01") {
			crystalTimes = append(crystalTimes, e.Time)
		}
	}
	if len(crystalTimes) != 2 || crystalTimes[0] != -1 || crystalTimes[1] != 0.5 {
		t.Errorf("phase_out crystal_top_01 moments = %v, want the original -1 plus the new 0.5", crystalTimes)
	}
	if _, ok = findEvent(phaseOut, "crystal_interior"); !ok {
		t.Error("phase_out lost its original crystal_interior event")
	}
	// The definitions are in the file where the game reads them.
	defs := mustParseFxEvents(t, out)
	glow, ok := defs["editor_glow"]
	if !ok {
		t.Fatal("the written file has no definition for editor_glow")
	}
	if v, _ := glow.First("marker"); v != "mrkr_gem_top" {
		t.Errorf("editor_glow marker = %q, want mrkr_gem_top", v)
	}
	if v, _ := glow.First("fx"); v != `necron\monolith\editor_glow` {
		t.Errorf("editor_glow fx = %q", v)
	}
	sounds := whe.ParseWheSounds(out)
	if s, ok := sounds["sound_editor"]; !ok || s.Path != `races\npc\editor` {
		t.Errorf("sound_editor resolves to %+v, want the path it was written with", s)
	}
	// Removing drops the firing, not the definition.
	if err := doc.RemoveTimedEvent("phase_in", indexOfEvent(timelineByName(t, doc.Timelines(), "phase_in"), "editor_glow")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	out, err = doc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary after remove: %v", err)
	}
	if _, ok := findEvent(timelineByName(t, whe.ParseFxTimelines(out), "phase_in"), "editor_glow"); ok {
		t.Error("the removed event still fires")
	}
	if _, ok := mustParseFxEvents(t, out)["editor_glow"]; !ok {
		t.Error("removing the firing also dropped the definition")
	}
}

func indexOfEvent(tl whe.FxTimeline, name string) int {
	for i := range tl.Events {
		if strings.EqualFold(tl.Events[i].Name, name) {
			return i
		}
	}
	return -1
}

// TestRemoveTimelineAnimationRoundTrip drops one clip from a motion's blend
// list: the rest of the list survives a marshal/reparse in order, the events
// on the same timeline are untouched, the other timeline keeps its own
// animation and event, and the out-of-range removals refuse without writing.
// Animation removal is a document edit like an event removal — one Save .whe
// away from the mod.
func TestRemoveTimelineAnimationRoundTrip(t *testing.T) {
	fx := &chunky.Chunk{TypeID: "FOLDEVCT", Version: 1}
	fx.Children = append(fx.Children,
		&chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: "step", Payload: whe.FxPayload([]whe.FxEventEntry{
			{Key: "fx", Value: `test\\step`},
		})},
		&chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: "hum", Payload: whe.FxPayload([]whe.FxEventEntry{
			{Key: "fx", Value: `test\\hum`},
		})},
	)
	mton := func(name string, anims []string, events []whe.FxTimelineEvent) *chunky.Chunk {
		payload := (&whe.Mton{Animations: anims, Events: events}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 1, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	mt.Children = append(mt.Children,
		mton("walk", []string{"walk_01", "walk_02", "walk_03"}, []whe.FxTimelineEvent{
			{Name: "step", Time: 0},
		}),
		mton("idle", []string{"idle_01"}, []whe.FxTimelineEvent{
			{Name: "hum", Time: -1},
		}),
	)
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{fx, mt}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	doc, err := whe.ParseDocument(buf.Bytes())
	if err != nil {
		t.Fatalf("whe.ParseDocument: %v", err)
	}

	// The refusals: past the end of the list and a timeline not in the file.
	if err := doc.RemoveTimelineAnimation("walk", 3); err == nil {
		t.Error("removing past the end of the animation list was accepted")
	}
	if err := doc.RemoveTimelineAnimation("walk", -1); err == nil {
		t.Error("removing a negative animation index was accepted")
	}
	if err := doc.RemoveTimelineAnimation("gone", 0); err == nil {
		t.Error("removing from a timeline that is not in the file was accepted")
	}

	// Drop the middle clip of walk.
	if err := doc.RemoveTimelineAnimation("walk", 1); err != nil {
		t.Fatalf("RemoveTimelineAnimation(walk, 1): %v", err)
	}
	out, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	walk := timelineByName(t, whe.ParseFxTimelines(out), "walk")
	if got := walk.Animations; len(got) != 2 || got[0] != "walk_01" || got[1] != "walk_03" {
		t.Errorf("walk animations = %v, want [walk_01 walk_03] with walk_02 removed and order kept", got)
	}
	if len(walk.Events) != 1 || walk.Events[0].Name != "step" {
		t.Errorf("walk events = %+v, want the original step event untouched", walk.Events)
	}
	// The other timeline is left exactly as it was.
	idle := timelineByName(t, whe.ParseFxTimelines(out), "idle")
	if got := idle.Animations; len(got) != 1 || got[0] != "idle_01" {
		t.Errorf("idle animations = %v, want [idle_01] untouched", got)
	}
	if len(idle.Events) != 1 || idle.Events[0].Name != "hum" {
		t.Errorf("idle events = %+v, want the original hum event untouched", idle.Events)
	}
}

// TestCopyWheCrystalEventsPutsTheCrystalOnPhaseIn is the acceptance case: the
// crystal set that phase_out plays becomes part of phase_in, on its markers,
// and everything else in the file is left exactly as it was.
func TestCopyWheCrystalEventsPutsTheCrystalOnPhaseIn(t *testing.T) {
	data := restoredMonolithWhe(t)
	doc, err := whe.ParseDocument(data)
	if err != nil {
		t.Fatalf("whe.ParseDocument: %v", err)
	}
	before := whe.ParseFxTimelines(data)
	phaseOut := timelineByName(t, before, "phase_out")
	var sourceCrystals []whe.FxTimelineEvent
	for _, e := range phaseOut.Events {
		if strings.HasPrefix(strings.ToLower(e.Name), "crystal") {
			sourceCrystals = append(sourceCrystals, e)
		}
	}
	if len(sourceCrystals) == 0 {
		t.Fatal("phase_out of the restored monolith fires no crystal events, so there is nothing to copy")
	}
	phaseInBefore := timelineByName(t, before, "phase_in")

	n, err := doc.CopyWheCrystalEvents()
	if err != nil {
		t.Fatalf("CopyWheCrystalEvents: %v", err)
	}
	if n != len(sourceCrystals) {
		t.Errorf("copied %d events, phase_out has %d crystal events", n, len(sourceCrystals))
	}
	out, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	after := whe.ParseFxTimelines(out)
	phaseIn := timelineByName(t, after, "phase_in")
	defs := mustParseFxEvents(t, out)
	for _, want := range sourceCrystals {
		ev, ok := findEvent(phaseIn, want.Name)
		if !ok {
			t.Errorf("phase_in does not fire %s", want.Name)
			continue
		}
		if ev.Time != want.Time {
			t.Errorf("%s fires at %v on phase_in, want the phase_out moment %v", want.Name, ev.Time, want.Time)
		}
		def, ok := defs[strings.ToLower(want.Name)]
		if !ok {
			t.Errorf("%s has no definition in the written file", want.Name)
			continue
		}
		if _, has := def.First("marker"); !has {
			t.Errorf("%s was copied without its marker binding", want.Name)
		}
		if _, has := def.First("fx"); !has {
			t.Errorf("%s was copied without its fx path", want.Name)
		}
	}
	// Everything that was on phase_in before is still on it.
	for _, e := range phaseInBefore.Events {
		if _, ok := findEvent(phaseIn, e.Name); !ok {
			t.Errorf("phase_in lost its original event %s", e.Name)
		}
	}
	// No other timeline moved: only phase_in may differ from the source file.
	for _, want := range before {
		if strings.EqualFold(want.Name, "phase_in") {
			continue
		}
		got := timelineByName(t, after, want.Name)
		if len(got.Events) != len(want.Events) {
			t.Errorf("timeline %s changed from %d events to %d", want.Name, len(want.Events), len(got.Events))
			continue
		}
		for i := range want.Events {
			if got.Events[i] != want.Events[i] {
				t.Errorf("timeline %s event %d = %+v, want %+v", want.Name, i, got.Events[i], want.Events[i])
			}
		}
	}
}

// unitEventsTestWhePath is where the Object window's fixture unit looks for
// its .whe when its blueprint names no model.
const unitEventsTestWhePath = `Data\art\ebps\races\necrons\troops\necron_lancer.whe`

// TestEventsSectionSavesEditedWheIntoModOverlay is the whole Object flow: the
// section finds the unit's .whe, a sound is added at a moment, Save writes the
// mod's own copy, and the installed file that the .whe came from comes out of
// it untouched.
func TestEventsSectionSavesEditedWheIntoModOverlay(t *testing.T) {
	ui, tree := unitEditUI(t)
	original := syntheticEventWhe()
	engineRoot := t.TempDir()
	writeSourcedFile(t, tree, unitEventsTestWhePath, original, &modtree.FileSource{
		ModName: "(Engine)", SourceName: "Data", Kind: modtree.KindEngine,
		DiskRoot: filepath.Join(engineRoot, "Data"),
	})
	tree.SortAll(tree.Root)
	engineDisk := filepath.Join(engineRoot, "Data", "art", "ebps", "races", "necrons", "troops", "necron_lancer.whe")

	ui.UnitSection = unitSectionNamed("Events")
	EnsureUnitEvents(ui)
	if ui.UnitEvtDoc == nil {
		t.Fatalf("the Events section did not load a .whe: %s", ui.UnitEvtErr)
	}
	if !strings.EqualFold(ui.UnitEvtPath, unitEventsTestWhePath) {
		t.Fatalf("the section opened %q, want %q", ui.UnitEvtPath, unitEventsTestWhePath)
	}

	// A sound added through the form at a chosen moment.
	ui.UnitEvtNameEd.SetText("sound_editor_test")
	ui.UnitEvtTimeEd.SetText("1.5")
	ui.UnitEvtSndEd.SetText(`races\npc\editor`)
	AddUnitEventFromForm(ui, "sound")
	if ui.UnitEvtFormErr != "" {
		t.Fatalf("add sound: %s", ui.UnitEvtFormErr)
	}
	ev, ok := findEvent(timelineByName(t, ui.UnitEvtDoc.Timelines(), "phase_in"), "sound_editor_test")
	if !ok || ev.Time != 1.5 {
		t.Fatalf("the sound event is %+v, want it on phase_in at 1.5s", ev)
	}

	if _, err := SaveUnitEvents(ui); err != nil {
		t.Fatalf("save: %v", err)
	}
	if ui.UnitEvtDirty {
		t.Error("the file is still marked unsaved after it was written")
	}

	// The engine's own file is untouched...
	now, err := os.ReadFile(engineDisk)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, now) {
		t.Error("the engine's .whe was rewritten instead of a mod copy being made")
	}
	// ...and the mod's copy carries the edits.
	f := tree.FindFile(unitEventsTestWhePath)
	if f == nil {
		t.Fatal("the .whe is no longer in the tree after being saved")
	}
	if f.Source.Kind != modtree.KindThisMod {
		t.Errorf("the saved .whe still comes from %v rather than from the mod", f.Source.Kind)
	}
	if !strings.HasPrefix(f.DiskPath, filepath.Join(tree.AppPath, "GraphTestMod")) {
		t.Errorf("the .whe was written to %q, which is outside the mod", f.DiskPath)
	}
	saved, err := tree.ReadFile(unitEventsTestWhePath)
	if err != nil {
		t.Fatal(err)
	}
	phaseIn := timelineByName(t, whe.ParseFxTimelines(saved), "phase_in")
	for _, name := range []string{"sound_editor_test", "phase_in_main"} {
		if _, ok := findEvent(phaseIn, name); !ok {
			t.Errorf("the saved file's phase_in does not fire %s", name)
		}
	}
	sounds := whe.ParseWheSounds(saved)
	if s, ok := sounds["sound_editor_test"]; !ok || s.Path != `races\npc\editor` {
		t.Errorf("the saved file's sound_editor_test resolves to %+v", s)
	}
	if ui.UnitEvtErr != "" {
		t.Errorf("the section reports an error after a successful save: %s", ui.UnitEvtErr)
	}
}

// TestANewActionLandsInTheLoadedWhe covers the New action button of the
// Events section: it gives the file an action under a name no other action
// answers to, marks the document as changed, and picks the new record so the
// conditions panel beside the list is about it. A second press has to find
// another name — two actions of one name are refused by the document.
//
// The action lands with no choices: an empty record is what the condition
// editor's add tools are built to start from, and a button that had to carry
// a first choice with it could not answer a .whe that has no timeline to name.
func TestANewActionLandsInTheLoadedWhe(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)
	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	doc := ui.UnitEvtDoc
	before := len(doc.Actions())
	ui.UnitEvtDirty = false

	ui.UnitEvtNewAct.Click()
	HandleUnitEventsInput(ui, gtx)
	acts := doc.Actions()
	if len(acts) != before+1 {
		t.Fatalf("the file carries %d actions after New action, want %d", len(acts), before+1)
	}
	sel := ui.UnitEvtActionSel
	if sel != len(acts)-1 {
		t.Errorf("the new action is not the picked one: selection %d of %d", sel, len(acts))
	}
	first := acts[sel].Name
	if !ui.UnitEvtDirty {
		t.Error("a new action does not mark the document as changed")
	}
	if !ui.UnitEvtFocusAction {
		t.Error("the section stayed on the timeline instead of moving onto the action it just created")
	}
	if n := len(acts[sel].Motions) + len(acts[sel].Subactions); n != 0 {
		t.Errorf("a brand-new action carries %d choices, want none", n)
	}

	// The second one has to land under another name: the first press used
	// the placeholder, and a file that already carries it takes the next.
	ui.UnitEvtNewAct.Click()
	HandleUnitEventsInput(ui, gtx)
	acts = doc.Actions()
	if len(acts) != before+2 {
		t.Fatalf("the file carries %d actions after the second New action, want %d", len(acts), before+2)
	}
	if second := acts[len(acts)-1].Name; strings.EqualFold(second, first) {
		t.Errorf("the second new action is called %q again, want another name", second)
	}
}

// TestANewTimelineLandsAndIsPicked covers the new-timeline row of the middle
// column: a name the file does not carry becomes a timeline the panel picks
// straight away — its animation list is the next thing to fill — the field
// empties for the next one, and the document is marked as changed. A name
// that is empty or already in the file is refused with the reason, and the
// typed name stays in the field to be corrected rather than being thrown
// away with the attempt.
func TestANewTimelineLandsAndIsPicked(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)
	doc := ui.UnitEvtDoc
	before := len(doc.Timelines())
	ui.UnitEvtDirty = false

	ui.UnitEvtNewTlEd.SetText("  brand_new_timeline  ")
	NewUnitTimeline(ui)
	if ui.UnitEvtFormErr != "" {
		t.Fatalf("NewUnitTimeline: %s", ui.UnitEvtFormErr)
	}
	if got := len(doc.Timelines()); got != before+1 {
		t.Fatalf("the file carries %d timelines after New timeline, want %d", got, before+1)
	}
	if ui.UnitEvtTimeline != "brand_new_timeline" {
		t.Errorf("the picked timeline is %q, want the one just created", ui.UnitEvtTimeline)
	}
	if sel := SelectedUnitEventTimeline(ui); sel == nil {
		t.Fatal("the new timeline is not on show in the panel")
	} else if len(sel.Animations) != 0 {
		t.Errorf("a brand-new timeline arrives with %d animations, want none to fill", len(sel.Animations))
	}
	if got := ui.UnitEvtNewTlEd.Text(); got != "" {
		t.Errorf("the name field still holds %q after the timeline was created", got)
	}
	if !ui.UnitEvtDirty {
		t.Error("a new timeline does not mark the document as changed")
	}

	// An empty name has no timeline to become, and the refusal keeps what
	// was typed so it can be corrected.
	ui.UnitEvtNewTlEd.SetText("   ")
	NewUnitTimeline(ui)
	if ui.UnitEvtFormErr == "" {
		t.Error("a timeline was created without a name")
	}
	if got := len(doc.Timelines()); got != before+1 {
		t.Errorf("the file carries %d timelines after the refused attempt, want %d", got, before+1)
	}

	// A name the file already carries is refused rather than replaced: two
	// timelines of one name are two motions that cannot be told apart.
	ui.UnitEvtNewTlEd.SetText("filter_tl_01")
	NewUnitTimeline(ui)
	if ui.UnitEvtFormErr == "" {
		t.Error("a timeline was created under a name the file already carries")
	}
	if got := len(doc.Timelines()); got != before+1 {
		t.Errorf("the file carries %d timelines after the refused attempt, want %d", got, before+1)
	}
	if got := ui.UnitEvtNewTlEd.Text(); got != "filter_tl_01" {
		t.Errorf("the refused name was cleared from the field: %q", got)
	}
}

// TestARemovedTimelineIsRefusedWhileAnActionNamesIt covers the Remove timeline
// button both ways: a timeline one of the file's actions names in a motion is
// refused before anything is dropped, with the actions that hold it named, and
// one nothing points at comes out with the panel moving on to a neighbour
// rather than being left on a name that is gone.
func TestARemovedTimelineIsRefusedWhileAnActionNamesIt(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)
	doc := ui.UnitEvtDoc
	before := len(doc.Timelines())

	// Every action of this fixture drives filter_tl_00 with its motion.
	SelectUnitEventTimeline(ui, "filter_tl_00")
	RemoveSelectedUnitTimeline(ui)
	if ui.UnitEvtFormErr == "" {
		t.Fatal("a timeline the actions name in their motions was removed")
	}
	if !strings.Contains(ui.UnitEvtFormErr, "filter_act_") {
		t.Errorf("the refusal does not name the actions holding the timeline: %s", ui.UnitEvtFormErr)
	}
	if got := len(doc.Timelines()); got != before {
		t.Errorf("the file changed under a refusal: %d timelines, want %d", got, before)
	}

	// Nothing names the last of them: it comes out, and the pick moves on.
	SelectUnitEventTimeline(ui, "filter_tl_59")
	RemoveSelectedUnitTimeline(ui)
	if ui.UnitEvtFormErr != "" {
		t.Fatalf("RemoveSelectedUnitTimeline: %s", ui.UnitEvtFormErr)
	}
	if got := len(doc.Timelines()); got != before-1 {
		t.Errorf("the file carries %d timelines after the removal, want %d", got, before-1)
	}
	if ui.UnitEvtTimeline == "filter_tl_59" {
		t.Error("the panel is still pointed at the timeline that was removed")
	}
	if SelectedUnitEventTimeline(ui) == nil {
		t.Error("no timeline is picked after the removal, though the file still has them")
	}
	if !ui.UnitEvtDirty {
		t.Error("a removed timeline does not mark the document as changed")
	}
	if _, ok := doc.TimelinePayload("filter_tl_59"); ok {
		t.Error("the removed timeline is still in the document")
	}
}

// TestARemovedActionIsRefusedWhileAnotherCallsIt covers the Remove action
// button both ways: an action another action calls in a sub-action link is
// refused with the caller named — a link left pointing at nothing is a break
// the file cannot report — and the caller itself, being called by nobody,
// comes out with the pick staying inside the shorter list.
func TestARemovedActionIsRefusedWhileAnotherCallsIt(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)
	doc := ui.UnitEvtDoc
	if err := doc.AddAction(whe.FxAction{
		Name:       "caller_act",
		Subactions: []whe.FxActionLink{{Name: "filter_act_03", Compare: whe.FxCompareNone}},
	}); err != nil {
		t.Fatalf("AddAction: %v", err)
	}
	before := len(doc.Actions())

	ui.UnitEvtActionSel = actionIndexByName(doc, "filter_act_03")
	RemoveSelectedUnitAction(ui)
	if ui.UnitActErr == "" {
		t.Fatal("an action another action calls was removed")
	}
	if !strings.Contains(ui.UnitActErr, "caller_act") {
		t.Errorf("the refusal does not name the caller: %s", ui.UnitActErr)
	}
	if got := len(doc.Actions()); got != before {
		t.Errorf("the file changed under a refusal: %d actions, want %d", got, before)
	}

	ui.UnitEvtActionSel = actionIndexByName(doc, "caller_act")
	RemoveSelectedUnitAction(ui)
	if ui.UnitActErr != "" {
		t.Fatalf("RemoveSelectedUnitAction: %s", ui.UnitActErr)
	}
	if got := len(doc.Actions()); got != before-1 {
		t.Errorf("the file carries %d actions after the removal, want %d", got, before-1)
	}
	if left := len(doc.Actions()); ui.UnitEvtActionSel < 0 || ui.UnitEvtActionSel >= left {
		t.Errorf("the pick sits at %d of %d actions after the removal", ui.UnitEvtActionSel, left)
	}
	if !ui.UnitEvtDirty {
		t.Error("a removed action does not mark the document as changed")
	}
}

// TestTheEventsSectionLayoutsItsColumns lays the section out headlessly with a
// .whe behind it, the way a reader meets it: the timeline rows have to be
// clickable, the event rows have to be drawn, and a frame must not panic over
// an empty or half-loaded state.
func TestTheEventsSectionLayoutsItsColumns(t *testing.T) {
	ui, tree := unitEditUI(t)
	testutil.WriteTreeFile(t, tree, unitEventsTestWhePath, syntheticEventWhe())
	tree.SortAll(tree.Root)
	ui.UnitSection = unitSectionNamed("Events")
	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, 1400, 800)
	LayoutUnitBody(ui, gtx)
	if ui.UnitEvtDoc == nil {
		t.Fatalf("the section did not load a .whe while drawing: %s", ui.UnitEvtErr)
	}
	if len(ui.UnitEvtTlClicks) != 2 {
		t.Errorf("%d timeline rows were laid out, want the two the file carries", len(ui.UnitEvtTlClicks))
	}
	if len(ui.UnitEvtClicks) == 0 {
		t.Error("no event rows were laid out for a timeline that fires events")
	}
	// The second frame has to work as well: input ran in between, and a stale
	// selection or click list is what panics then.
	HandleUnitEventsInput(ui, gtx)
	LayoutUnitBody(ui, gtx)
	// Switching timelines refills the form from the other timeline's rows.
	SelectUnitEventTimeline(ui, "phase_out")
	HandleUnitEventsInput(ui, gtx)
	LayoutUnitBody(ui, gtx)
	if ui.UnitEvtTimeline != "phase_out" {
		t.Errorf("the selected timeline is %q, want phase_out", ui.UnitEvtTimeline)
	}
}
