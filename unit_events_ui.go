package main

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"math"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dow-mod-studio/internal/modtree"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// The Events section edits the selected model's .whe, which is where both
// halves of an event live: DATAEVNT chunks hold what an event does (the .lua
// effect to play and the marker to play it at, or the sound folder), and
// DATAMTON timelines say which of those events fire while each animation runs
// and at what second. An art/events file carries definitions only for the
// blueprint references, so the timed add/remove this window offers is a .whe
// edit — and only the .whe needs writing for these tasks.
//
// Everything is written through the mod overlay, exactly like the unit
// attribute editor: the source .whe often lives in an SGA archive or Program
// Files, and saving makes the mod's own copy rather than touching the game.

// unitSectionEvents names the section, so the layout does not repeat the
// string of the menu entry it is keyed on.
const unitSectionEvents = "Events"

// unitEventsWhePath is the .whe that carries the selected unit's event
// timelines: the file beside the model its blueprint names, then the animator
// path on its own, then a search by name for a model that was renamed.
func unitEventsWhePath(tree *modtree.ModTree, d *unitDetails) string {
	if tree == nil || d == nil {
		return ""
	}
	var candidates []string
	if src, ok := newUnitModelSource(tree, d); ok {
		if model := unitModelPath(tree, src); model != "" {
			candidates = append(candidates, strings.TrimSuffix(model, path.Ext(model))+".whe")
		}
		rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(src.animator), `\`, "/"), "./")
		if rel != "" {
			if strings.Contains(rel, ".") {
				rel = strings.TrimSuffix(rel, path.Ext(rel)) + ".whe"
			} else {
				rel += ".whe"
			}
			candidates = append(candidates, filepath.ToSlash(filepath.Join("Data", "art", "ebps", filepath.FromSlash(rel))))
		}
	}
	for _, cand := range candidates {
		if tree.FindFile(cand) != nil {
			return cand
		}
	}
	if name := strings.TrimSuffix(d.entry.name, path.Ext(d.entry.name)); name != "" {
		if f := tree.FindByName(strings.ToLower(name + ".whe")); f != nil {
			return f.FullPath
		}
		// FindByName answers from an index built on first use, which may
		// predate a file added afterwards, so the tree itself is walked when
		// the index has no answer.
		want := strings.ToLower(name + ".whe")
		var found string
		modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
			if found == "" && strings.EqualFold(f.Name, want) {
				found = f.FullPath
			}
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// ensureUnitEvents reads the .whe of the unit on screen, once per unit, and
// remembers why it could not when there is nothing to edit.
func (ui *uiState) ensureUnitEvents() {
	if ui.modTree == nil {
		ui.unitEvtTree, ui.unitEvtFor, ui.unitEvtDoc = nil, "", nil
		ui.unitEvtPath, ui.unitEvtErr = "", "No module is open."
		return
	}
	u := ui.currentUnit()
	if u == nil {
		ui.unitEvtTree, ui.unitEvtFor, ui.unitEvtDoc = ui.modTree, "", nil
		ui.unitEvtPath, ui.unitEvtErr = "", "No unit selected."
		return
	}
	if ui.unitEvtTree == ui.modTree && ui.unitEvtTried && strings.EqualFold(ui.unitEvtFor, u.path) {
		return
	}
	ui.unitEvtTree = ui.modTree
	ui.unitEvtFor = u.path
	ui.unitEvtTried = true
	ui.unitEvtDirty = false
	ui.unitEvtFormErr = ""
	ui.unitEvtSel = 0
	ui.unitEvtSelSet = map[int]bool{0: true}
	ui.unitEvtSelAnchor = 0
	// Another unit means another .whe almost always, and the definitions in
	// the buffer are only good for the file they were snapshotted from.
	ui.unitClips.dropKind(clipEvents)
	ui.unitClipCur = -1
	ui.unitEvtBound = ""
	ui.unitEvtEntryBound = ""
	ui.unitEvtPath = unitEventsWhePath(ui.modTree, ui.currentUnitDetails())
	if ui.unitEvtPath == "" {
		ui.unitEvtDoc = nil
		ui.unitEvtErr = "This unit's model has no .whe file, so it carries no event timelines."
		return
	}
	data, err := ui.modTree.ReadFile(ui.unitEvtPath)
	if err != nil {
		ui.unitEvtDoc = nil
		ui.unitEvtErr = "Could not read " + ui.unitEvtPath + ": " + err.Error()
		return
	}
	doc, err := whe.ParseDocument(data)
	if err != nil {
		ui.unitEvtDoc = nil
		ui.unitEvtErr = err.Error()
		return
	}
	ui.unitEvtDoc = doc
	ui.unitEvtErr = ""
	if len(doc.Timelines()) == 0 {
		ui.unitEvtErr = "This .whe carries no animation timelines to edit."
		return
	}
	// Start on the timeline of the clip the viewport is showing, so opening
	// the section after looking at phase_in lands on phase_in.
	want := ""
	if v := ui.unitModelViewer(); v != nil && v.anim != nil {
		want = v.anim.Name
	}
	for _, tl := range doc.Timelines() {
		if want != "" && timelineMatchesClip(tl, want) {
			ui.unitEvtTimeline = tl.Name
			return
		}
	}
	ui.unitEvtTimeline = doc.Timelines()[0].Name
}

// timelineMatchesClip reports whether a timeline drives the named clip, by its
// own name or by the clips it blends.
func timelineMatchesClip(tl whe.FxTimeline, clip string) bool {
	if strings.EqualFold(tl.Name, clip) {
		return true
	}
	for _, anim := range tl.Animations {
		if strings.EqualFold(anim, clip) {
			return true
		}
	}
	return false
}

// selectedUnitEventTimeline is the timeline the right column is showing, or
// nil when there is none.
func (ui *uiState) selectedUnitEventTimeline() *whe.FxTimeline {
	if ui.unitEvtDoc == nil || ui.unitEvtTimeline == "" {
		return nil
	}
	for _, tl := range ui.unitEvtDoc.Timelines() {
		tl := tl
		if strings.EqualFold(tl.Name, ui.unitEvtTimeline) {
			return &tl
		}
	}
	return nil
}

// selectUnitEventTimeline picks the timeline whose events are listed, which is
// what pressing a row of the middle column does.
func (ui *uiState) selectUnitEventTimeline(name string) {
	if strings.EqualFold(ui.unitEvtTimeline, name) {
		return
	}
	ui.unitEvtTimeline = name
	ui.unitEvtSel = 0
	ui.unitEvtSelSet = map[int]bool{0: true}
	ui.unitEvtSelAnchor = 0
	ui.unitEvtBound = ""
}

func (ui *uiState) ensureUnitEventClicks(timelines, events int) {
	if len(ui.unitEvtTlClicks) != timelines {
		ui.unitEvtTlClicks = make([]widget.Clickable, timelines)
	}
	if len(ui.unitEvtClicks) != events {
		ui.unitEvtClicks = make([]widget.Clickable, events)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: input — presses, typing, focus (run before layout)
// ═══════════════════════════════════════════════════════════════════════════

// wheEventClip is the section's copy buffer: the events Copy took, each with
// the moment it fires at, and a snapshot of the DATAEVNT definitions they
// name. The snapshot is what lets a Paste into a timeline that lacks those
// definitions bring the marker and path settings along — while a name this
// .whe already defines differently is refused rather than overwritten.
type wheEventClip struct {
	events []whe.FxTimelineEvent
	defs   map[string]whe.FxEvent
}

// unitEvtSelectedIndices is the selected rows of the current timeline in
// ascending order: what Copy, Remove and the row highlight act on.
func (ui *uiState) unitEvtSelectedIndices() []int {
	out := make([]int, 0, len(ui.unitEvtSelSet))
	for i := range ui.unitEvtSelSet {
		if i >= 0 {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

// unitEvtRowSelected says whether a row wears the selection colour.
func (ui *uiState) unitEvtRowSelected(i int) bool {
	return ui.unitEvtSelSet[i]
}

// unitEvtTrackModifiers follows Ctrl and Shift while the section is on
// screen: a pointer press does not carry usable modifiers here, so
// Ctrl+click and Shift+click read the state these key events keep up to date.
//
// Both filters allow any modifier state: a filter's match requires the
// event's modifiers to be exactly the ones it names as Required plus the ones
// it allows as Optional, so a name-only filter would reject the Ctrl press
// itself the moment that event starts carrying ModCtrl — the event never
// reaches this section and the held flag stays false forever. Every modifier
// event seen is logged on change, so a multi-select that never fires shows
// here whether the key events arrived at all and what they carried.
func (ui *uiState) unitEvtTrackModifiers(gtx layout.Context) {
	const anyMod = key.ModCtrl | key.ModShift | key.ModAlt | key.ModSuper
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameCtrl, Optional: anyMod},
			key.Filter{Name: key.NameShift, Optional: anyMod},
		)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if s := fmt.Sprintf("%q %v mods=%v", e.Name, e.State, e.Modifiers); s != ui.unitEvtModLog {
			LogInfo("evt: modifier key %s", s)
			ui.unitEvtModLog = s
		}
		switch e.Name {
		case key.NameCtrl:
			ui.unitEvtCtrlHeld = e.State != key.Release
		case key.NameShift:
			ui.unitEvtShiftHeld = e.State != key.Release
		}
	}
}

// unitEvtFieldFocused says whether a field of this section holds the caret:
// while one does, Ctrl+C and Ctrl+V belong to the text being edited, not to
// the event clipboard.
func (ui *uiState) unitEvtFieldFocused(gtx layout.Context) bool {
	if ui.textActive() {
		return true
	}
	for i := range ui.unitEvtEntryEds {
		if ui.unitEvtEntryEds[i].Focused(gtx) {
			return true
		}
	}
	for _, ed := range []*widget.Editor{&ui.unitEvtNameEd, &ui.unitEvtTimeEd, &ui.unitEvtFxEd, &ui.unitEvtSndEd, &ui.unitEvtMarkerEd} {
		if ed.Focused(gtx) {
			return true
		}
	}
	return false
}

// unitEvtBlurFields lets go of any caret this section holds: a row click
// ends editing, so the selection change is not stuck behind a field that is
// still focused and the shortcuts below become available again.
func (ui *uiState) unitEvtBlurFields(gtx layout.Context) {
	for i := range ui.unitEvtEntryEds {
		ui.unitEvtEntryEds[i].Blur(gtx)
	}
	for _, ed := range []*widget.Editor{&ui.unitEvtNameEd, &ui.unitEvtTimeEd, &ui.unitEvtFxEd, &ui.unitEvtSndEd, &ui.unitEvtMarkerEd} {
		ed.Blur(gtx)
	}
}

// handleUnitEventsInput owns the presses and typing of the Events section. It
// runs on every frame while the section is on screen, ahead of the layout that
// draws what it decided.
func (ui *uiState) handleUnitEventsInput(gtx layout.Context) {
	if ui.currentUnitSection().label != unitSectionEvents {
		// Leaving the section silences whatever it was playing: a timeline
		// that keeps sounding behind another section is a leak, not a feature.
		if ui.evtPlayer.playing || ui.evtPlayer.audition != nil {
			ui.evtPlayer.stopAll()
		}
		return
	}
	ui.ensureUnitEvents()
	// The sound transport first: its clicks are its own, and the playhead it
	// drives must advance before the rows below are read.
	ui.unitEvtPlayerTick(gtx)
	// The modifiers first: the clicks below are read in this same pass, and a
	// Ctrl held down for a click arrives as key events of its own.
	ui.unitEvtTrackModifiers(gtx)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtNameEd, evtPickName)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtTimeEd, evtPickNone)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtFxEd, evtPickFX)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtSndEd, evtPickSound)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtMarkerEd, evtPickMarker)
	if ui.unitEvtDoc == nil {
		return
	}
	// Fill the form from the selected row whenever the selection moves, so
	// re-firing an existing effect is a time edit rather than a retyping.
	ui.syncUnitEventForm()
	timelines := ui.unitEvtDoc.Timelines()
	tl := ui.selectedUnitEventTimeline()
	events := 0
	if tl != nil {
		events = len(tl.Events)
	}
	ui.ensureUnitEventClicks(len(timelines), events)
	for i := range ui.unitEvtTlClicks {
		if i < len(timelines) && ui.unitEvtTlClicks[i].Clicked(gtx) {
			ui.selectUnitEventTimeline(timelines[i].Name)
			tl = ui.selectedUnitEventTimeline()
			events = 0
			if tl != nil {
				events = len(tl.Events)
			}
			ui.ensureUnitEventClicks(len(timelines), events)
		}
	}
	for i := range ui.unitEvtClicks {
		if i >= events || !ui.unitEvtClicks[i].Clicked(gtx) {
			continue
		}
		// A row click is a selection, and it lets go of any field being typed
		// into so the caret does not hold the clipboard shortcuts hostage.
		// Plain picks the one row, Ctrl toggles it in the group, and Shift
		// takes the range from the anchor the last plain or Ctrl click left.
		ui.unitEvtBlurFields(gtx)
		// The modifiers are read from the OS at the click itself. The
		// tracked flags depend on this section's key filters winning the
		// Ctrl/Shift events ahead of every other filter in the frame, and a
		// selection that appears only when that race is won is no selection
		// at all; the live key state cannot race. The tracked flags stay as
		// the fallback where there is no live reader.
		live := heldModifiers()
		ctrl := ui.unitEvtCtrlHeld || live.Contain(key.ModCtrl)
		shift := ui.unitEvtShiftHeld || live.Contain(key.ModShift)
		LogInfo("evt: row %d click ctrl=%v shift=%v (tracked %v/%v live=%v anchor=%d)",
			i, ctrl, shift, ui.unitEvtCtrlHeld, ui.unitEvtShiftHeld, live, ui.unitEvtSelAnchor)
		switch {
		case ctrl:
			if ui.unitEvtSelSet == nil {
				ui.unitEvtSelSet = map[int]bool{}
			}
			if ui.unitEvtSelSet[i] {
				delete(ui.unitEvtSelSet, i)
			} else {
				ui.unitEvtSelSet[i] = true
			}
			ui.unitEvtSelAnchor = i
		case shift:
			lo, hi := i, ui.unitEvtSelAnchor
			if lo > hi {
				lo, hi = hi, lo
			}
			ui.unitEvtSelSet = map[int]bool{}
			for j := lo; j <= hi && j < events; j++ {
				ui.unitEvtSelSet[j] = true
			}
		default:
			ui.unitEvtSelSet = map[int]bool{i: true}
			ui.unitEvtSelAnchor = i
		}
		ui.unitEvtSel = i
		ui.unitEvtBound = ""
		break
	}
	// Rows can disappear under a Remove, so the group loses what no longer
	// exists before anything reads it.
	for i := range ui.unitEvtSelSet {
		if i >= events {
			delete(ui.unitEvtSelSet, i)
		}
	}
	if ui.unitEvtAddFX.Clicked(gtx) {
		ui.addUnitEventFromForm("fx")
		return
	}
	if ui.unitEvtAddEvent.Clicked(gtx) {
		ui.addUnitEventFromForm("event")
		return
	}
	if ui.unitEvtAddSound.Clicked(gtx) {
		ui.addUnitEventFromForm("sound")
		return
	}
	if ui.unitEvtRemove.Clicked(gtx) {
		ui.removeSelectedUnitEvent()
		return
	}
	if ui.unitEvtSave.Clicked(gtx) {
		ui.saveUnitEventsFromPanel()
		return
	}
	if ui.unitEvtRevert.Clicked(gtx) {
		ui.revertUnitEvents()
		return
	}
	if ui.unitEvtSet.Clicked(gtx) || ui.unitEvtApply.Clicked(gtx) {
		ui.applySelectedEventForm()
		return
	}
	if ui.unitEvtCopy.Clicked(gtx) {
		ui.copySelectedUnitEvents()
		return
	}
	if ui.unitEvtPaste.Clicked(gtx) {
		ui.pasteUnitEvents()
		return
	}
	if ui.unitClipPrev.Clicked(gtx) {
		ui.stepUnitClip(-1)
	}
	if ui.unitClipNext.Clicked(gtx) {
		ui.stepUnitClip(1)
	}
	// Ctrl+C and Ctrl+V repeat those two buttons — unless a field holds the
	// caret, for while it does the text being edited keeps its own clipboard.
	if !ui.unitEvtFieldFocused(gtx) {
		for {
			ev, ok := gtx.Event(
				key.Filter{Name: "C", Required: key.ModCtrl, Optional: key.ModShift},
				key.Filter{Name: "V", Required: key.ModCtrl},
			)
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			switch e.Name {
			case "C":
				ui.copySelectedUnitEvents()
			case "V":
				ui.pasteUnitEvents()
			}
		}
	}
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtNameEd, evtPickName)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtTimeEd, evtPickNone)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtFxEd, evtPickFX)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtSndEd, evtPickSound)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtMarkerEd, evtPickMarker)
}

// unitEvtUpdateEditor processes one form field's typing. A press into the row
// focuses it, the same way the attribute editor's value field does. Enter
// submits: it applies the fields the way Set does and never drops a line
// break into the value.
func (ui *uiState) unitEvtUpdateEditor(gtx layout.Context, ed *widget.Editor, pick evtPickTarget) {
	ed.SingleLine = true
	ed.Submit = true
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
		// A field with a list opens its picker instead of taking the caret: the
		// picker's own filter takes the typing.
		if pick != evtPickNone {
			ui.openEvtPicker(pick)
		} else {
			gtx.Execute(key.FocusCmd{Tag: ed})
		}
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.applySelectedEventForm()
			ed.Blur(gtx)
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: form sync — refill fields from the selected event row
// ═══════════════════════════════════════════════════════════════════════════

// syncUnitEventForm refills the form fields when the selection changes.
func (ui *uiState) syncUnitEventForm() {
	tl := ui.selectedUnitEventTimeline()
	if tl == nil || ui.unitEvtSel < 0 || ui.unitEvtSel >= len(tl.Events) {
		ui.unitEvtBound = ""
		return
	}
	bound := ui.unitEvtTimeline + "|" + strconv.Itoa(ui.unitEvtSel)
	if ui.unitEvtBound == bound {
		return
	}
	ev := tl.Events[ui.unitEvtSel]
	ui.unitEvtBound = bound
	ui.unitEvtNameEd.SetText(ev.Name)
	ui.unitEvtTimeEd.SetText(unitEventTimeEdit(ev.Time))
	ui.unitEvtFxEd.SetText("")
	ui.unitEvtSndEd.SetText("")
	ui.unitEvtMarkerEd.SetText("")
	def, ok := ui.unitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !ok {
		return
	}
	if p, has := def.First("sound"); has {
		ui.unitEvtSndEd.SetText(p)
		return
	}
	if p, has := def.First("fx"); has {
		ui.unitEvtFxEd.SetText(p)
	}
	if m, has := def.First("marker"); has {
		ui.unitEvtMarkerEd.SetText(m)
	}
}

// unitEventTimeEdit is the plain text a moment is edited as, without the
// units the rows show.
func unitEventTimeEdit(t float64) string {
	if t < 0 {
		return "-1"
	}
	if math.IsInf(t, 1) || t > 1e30 {
		return "leave"
	}
	return strconv.FormatFloat(t, 'f', -1, 64)
}

// unitEventTime parses the time field: empty means the start of the clip,
// "enter" the DATAMTON sentinel for on entering, "leave" the one for on
// leaving, and anything else seconds into the clip.
func unitEventTime(text string) (float64, error) {
	s := strings.ToLower(strings.TrimSpace(text))
	switch s {
	case "":
		return 0, nil
	case "enter", "-1":
		return -1, nil
	case "leave", "exit":
		return math.MaxFloat32, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("time must be seconds into the clip, -1 (enter) or leave")
	}
	if math.IsNaN(v) || v < -1 || v > math.MaxFloat32 {
		return 0, fmt.Errorf("time must be seconds into the clip, -1 (enter) or leave")
	}
	return v, nil
}

// addUnitEventFromForm fires an event on the selected timeline. An FX keeps
// its marker binding from the definition it names; a sound names the folder
// under data/sound it resolves to. Filling in no path at all re-fires an
// event the .whe already defines, which is how an existing effect is put on a
// second clip.
func (ui *uiState) addUnitEventFromForm(kind string) {
	ui.applyUnitEvent(kind)
}

func (ui *uiState) applyUnitEvent(kind string) {
	ui.lastFormAction = kind
	doc := ui.unitEvtDoc
	if doc == nil {
		ui.unitEvtFormErr = "No .whe is loaded."
		ui.lastFormAction = ""
		return
	}
	if ui.unitEvtTimeline == "" {
		ui.unitEvtFormErr = "Select a timeline first."
		ui.lastFormAction = ""
		return
	}
	name := strings.TrimSpace(ui.unitEvtNameEd.Text())
	if name == "" {
		ui.unitEvtFormErr = "Event name is required."
		ui.lastFormAction = ""
		return
	}
	at, err := unitEventTime(ui.unitEvtTimeEd.Text())
	if err != nil {
		ui.unitEvtFormErr = err.Error()
		ui.lastFormAction = ""
		return
	}
	// A sound takes its path from the sound field, an effect from the FX field;
	// an event that is only being placed takes neither.
	target := strings.TrimSpace(ui.unitEvtFxEd.Text())
	if kind == "sound" {
		target = strings.TrimSpace(ui.unitEvtSndEd.Text())
	}
	var definition *whe.FxEvent
	soundPath := ""
	switch kind {
	case "sound":
		if target == "" {
			ui.unitEvtFormErr = "A sound path is required (relative to data\\sound)."
			ui.lastFormAction = ""
			return
		}
		if !strings.HasPrefix(strings.ToLower(name), "sound") {
			name = "sound_" + name
			ui.unitEvtNameEd.SetText(name)
		}
		soundPath = target
	case "event":
		// Placing an event on the timeline needs no definition of its own: the
		// event must already be defined in this .whe, which AddTimedEvent checks.
	default:
		marker := strings.TrimSpace(ui.unitEvtMarkerEd.Text())
		var entries []whe.FxEventEntry
		if marker != "" {
			entries = append(entries, whe.FxEventEntry{Key: "marker", Value: marker})
		}
		if target != "" {
			entries = append(entries, whe.FxEventEntry{Key: "fx", Value: target})
		}
		if len(entries) > 0 {
			definition = &whe.FxEvent{Name: name, Entries: entries}
		}
	}

	if err := doc.AddTimedEvent(ui.unitEvtTimeline, whe.FxTimelineEvent{Name: name, Time: at}, definition, soundPath); err != nil {
		ui.unitEvtFormErr = err.Error()
		ui.lastFormAction = ""
		return
	}

	ui.unitEvtFormErr = ""
	ui.unitEvtDirty = true
	ui.lastFormAction = kind
	// Land the selection on the row that was just added.
	if tl := ui.selectedUnitEventTimeline(); tl != nil {
		for i := range tl.Events {
			if strings.EqualFold(tl.Events[i].Name, name) && tl.Events[i].Time == at {
				ui.unitEvtSel = i
				ui.unitEvtSelSet = map[int]bool{i: true}
				ui.unitEvtSelAnchor = i
			}
		}
		ui.unitEvtBound = ""
	}
	ui.reloadUnitModelFx()
	ui.statusEventAdded(name, at)
}

func (ui *uiState) statusEventAdded(name string, at float64) {
	ui.statusText = fmt.Sprintf("Added %s at %s on %s — save to write it into the mod",
		name, unitEventTimeLabel(at), ui.unitEvtTimeline)
}

// removeSelectedUnitEvent drops the selected rows from its timeline — every
// row of the group, not just the one showing below. The definitions themselves
// are kept: other clips and the blueprint may still fire them, and an orphan
// definition is harmless where a missing one is a broken effect.
func (ui *uiState) removeSelectedUnitEvent() {
	doc := ui.unitEvtDoc
	tl := ui.selectedUnitEventTimeline()
	if doc == nil || tl == nil {
		ui.unitEvtFormErr = "Select an event to remove."
		ui.lastFormAction = ""
		return
	}
	indices := ui.unitEvtSelectedIndices()
	if len(indices) == 0 {
		ui.unitEvtFormErr = "Select an event to remove."
		ui.lastFormAction = ""
		return
	}
	// Descending, so taking a row cannot shift the ones still to come.
	removed := 0
	for k := len(indices) - 1; k >= 0; k-- {
		i := indices[k]
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		if err := doc.RemoveTimedEvent(ui.unitEvtTimeline, i); err != nil {
			ui.unitEvtFormErr = err.Error()
			ui.lastFormAction = ""
			return
		}
		removed++
	}
	if removed == 0 {
		ui.unitEvtFormErr = "Select an event to remove."
		ui.lastFormAction = ""
		return
	}
	ui.unitEvtFormErr = ""
	ui.unitEvtDirty = true
	ui.lastFormAction = "remove"
	ui.unitEvtSelSet = map[int]bool{}
	if tl := ui.selectedUnitEventTimeline(); tl != nil {
		if ui.unitEvtSel >= len(tl.Events) {
			ui.unitEvtSel = len(tl.Events) - 1
		}
		// The selection lands on whichever row took the removed one's place,
		// so the panel below and the buttons keep something to act on.
		if ui.unitEvtSel >= 0 {
			ui.unitEvtSelSet[ui.unitEvtSel] = true
		}
	}
	ui.unitEvtBound = ""
	ui.reloadUnitModelFx()
	ui.statusText = fmt.Sprintf("Removed %d event(s) from %s — save to write it into the mod", removed, ui.unitEvtTimeline)
}

// stepUnitClip moves the history position through the entries that fit the
// events field, from the entry Paste would take now.
func (ui *uiState) stepUnitClip(dir int) {
	_, cur, ok := ui.unitClips.pick(clipEvents, ui.unitClipCur)
	if !ok {
		return
	}
	if next := ui.unitClips.step(clipEvents, cur, dir); next >= 0 {
		ui.unitClipCur = next
	}
}

// copySelectedUnitEvents puts the selected rows and a snapshot of their
// definitions into the section's buffer, from where Paste carries them into
// any timeline of this .whe. The buffer dies with the section's .whe — the
// definitions snapshotted here are only good for the file they came from.
func (ui *uiState) copySelectedUnitEvents() {
	tl := ui.selectedUnitEventTimeline()
	if tl == nil {
		ui.statusText = "Select a timeline first"
		return
	}
	indices := ui.unitEvtSelectedIndices()
	if len(indices) == 0 {
		ui.statusText = "Nothing is selected to copy"
		return
	}
	defs := ui.unitEvtDoc.Events()
	clip := &wheEventClip{defs: map[string]whe.FxEvent{}}
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		ev := tl.Events[i]
		clip.events = append(clip.events, ev)
		if d, ok := defs[strings.ToLower(ev.Name)]; ok {
			clip.defs[strings.ToLower(ev.Name)] = d
		}
	}
	if len(clip.events) == 0 {
		ui.statusText = "Nothing is selected to copy"
		return
	}
	ui.unitClips.push(clipEntry{
		kind:   clipEvents,
		label:  fmt.Sprintf("%d event(s) from %s", len(clip.events), ui.unitEvtTimeline),
		at:     time.Now(),
		events: clip,
	})
	// The copy just made is the one Paste takes until the history is stepped.
	ui.unitClipCur = len(ui.unitClips.entries) - 1
	ui.statusText = fmt.Sprintf("Copied %d event(s) — Paste into any timeline", len(clip.events))
}

// pasteUnitEvents writes the buffer's events into the selected timeline at
// their original moments, bringing the definitions Copy snapshotted along. A
// name this .whe already defines differently is not overwritten —
// AddTimedEvent says so — and that event is skipped and reported instead.
func (ui *uiState) pasteUnitEvents() {
	entry, _, ok := ui.unitClips.pick(clipEvents, ui.unitClipCur)
	if !ok || entry.events == nil || len(entry.events.events) == 0 {
		ui.statusText = "The event clipboard is empty — Copy first"
		return
	}
	clip := entry.events
	if ui.unitEvtDoc == nil || ui.unitEvtTimeline == "" {
		ui.statusText = "Select a timeline first"
		return
	}
	added, skipped := 0, 0
	firstSkip := ""
	var pasted []whe.FxTimelineEvent
	for _, ev := range clip.events {
		var definition *whe.FxEvent
		soundPath := ""
		if d, ok := clip.defs[strings.ToLower(ev.Name)]; ok {
			if p, isSound := d.First("sound"); isSound {
				soundPath = p
			} else {
				snapshot := d
				definition = &snapshot
			}
		}
		if err := ui.unitEvtDoc.AddTimedEvent(ui.unitEvtTimeline, ev, definition, soundPath); err != nil {
			skipped++
			if firstSkip == "" {
				firstSkip = err.Error()
			}
			continue
		}
		added++
		pasted = append(pasted, ev)
	}
	if added == 0 {
		ui.statusText = "Paste failed: " + firstSkip
		return
	}
	ui.unitEvtDirty = true
	ui.unitEvtBound = ""
	// Pasted rows land at the end of the timeline; the selection follows them
	// so what was just pasted is what the panels below show.
	ui.unitEvtSelSet = map[int]bool{}
	if tl := ui.selectedUnitEventTimeline(); tl != nil {
		for _, ev := range pasted {
			for i := len(tl.Events) - 1; i >= 0; i-- {
				if tl.Events[i].Name == ev.Name && tl.Events[i].Time == ev.Time {
					ui.unitEvtSelSet[i] = true
					ui.unitEvtSel = i
					break
				}
			}
		}
	}
	ui.reloadUnitModelFx()
	msg := fmt.Sprintf("Pasted %d event(s) into %s", added, ui.unitEvtTimeline)
	if skipped > 0 {
		msg += fmt.Sprintf(", %d skipped: %s", skipped, firstSkip)
	}
	ui.statusText = msg
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: apply — commit the form's edited fields to the selected event
// ═══════════════════════════════════════════════════════════════════════════

// applySelectedEventForm writes the form's typed fields into the selected
// event of the in-memory document: the moment it fires at on the timeline and
// the path and marker its definition carries. Nothing touches the file —
// Save .whe does that — and the viewport re-resolves from the document, so
// the edited event shows on the model before it is written out. The event's
// name is its identity across the file, so a changed name is refused rather
// than half-renamed.
func (ui *uiState) applySelectedEventForm() {
	if ui.unitEvtDoc == nil {
		ui.unitEvtFormErr = "No .whe is loaded."
		return
	}
	tl := ui.selectedUnitEventTimeline()
	if tl == nil || ui.unitEvtSel < 0 || ui.unitEvtSel >= len(tl.Events) {
		ui.unitEvtFormErr = "Select an event to apply the fields to."
		return
	}
	ev := tl.Events[ui.unitEvtSel]
	if name := strings.TrimSpace(ui.unitEvtNameEd.Text()); !strings.EqualFold(name, ev.Name) {
		ui.unitEvtFormErr = "The event name is this event's identity — keep it as selected when applying."
		return
	}
	at, err := unitEventTime(ui.unitEvtTimeEd.Text())
	if err != nil {
		ui.unitEvtFormErr = err.Error()
		return
	}
	def0 := ui.unitEvtDoc.Events()[strings.ToLower(ev.Name)]
	path := strings.TrimSpace(ui.unitEvtFxEd.Text())
	if _, isSound := def0.First("sound"); isSound || strings.HasPrefix(strings.ToLower(ev.Name), "sound") {
		path = strings.TrimSpace(ui.unitEvtSndEd.Text())
	}
	marker := strings.TrimSpace(ui.unitEvtMarkerEd.Text())
	def, hasDef := ui.unitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !hasDef && (path != "" || marker != "") {
		ui.unitEvtFormErr = "This event has no definition in the .whe — path and marker cannot be applied."
		return
	}
	pathKey := "fx"
	if _, isSound := def.First("sound"); isSound || strings.HasPrefix(strings.ToLower(ev.Name), "sound") {
		pathKey = "sound"
	}
	pathCur, _ := def.First(pathKey)
	markerCur, _ := def.First("marker")
	timeChanged := at != ev.Time
	pathChanged := path != strings.TrimSpace(pathCur)
	markerChanged := marker != strings.TrimSpace(markerCur)
	if !timeChanged && !pathChanged && !markerChanged {
		ui.unitEvtFormErr = ""
		ui.statusText = "no changes to apply"
		return
	}
	// The time is written first and the definition after it, so a definition
	// that cannot be written leaves the moment already correct in memory and
	// marked as an edit rather than silently dropped.
	wrote := false
	if timeChanged {
		if err := ui.unitEvtDoc.SetTimedEventTime(ui.unitEvtTimeline, ui.unitEvtSel, at); err != nil {
			ui.unitEvtFormErr = err.Error()
			if wrote {
				ui.reloadUnitModelFx()
			}
			return
		}
		wrote = true
	}
	if pathChanged {
		if err := ui.unitEvtDoc.SetEventEntryByKey(ev.Name, pathKey, path); err != nil {
			ui.unitEvtFormErr = err.Error()
			if wrote {
				ui.reloadUnitModelFx()
			}
			return
		}
		wrote = true
	}
	if markerChanged {
		if err := ui.unitEvtDoc.SetEventEntryByKey(ev.Name, "marker", marker); err != nil {
			ui.unitEvtFormErr = err.Error()
			if wrote {
				ui.reloadUnitModelFx()
			}
			return
		}
		wrote = true
	}
	ui.unitEvtFormErr = ""
	ui.unitEvtDirty = true
	// The form and the definition's value fields refill from what was just
	// written, so what is shown is what the document holds.
	ui.unitEvtBound = ""
	ui.unitEvtEntryBound = ""
	ui.reloadUnitModelFx()
	ui.statusText = "Applied to " + ev.Name + " — Save .whe to write it into the mod"
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: save / revert — write overlay and throw edits away
// ═══════════════════════════════════════════════════════════════════════════

// saveUnitEvents writes the edited .whe into the mod's own overlay, leaving
// the installed file and the SGA archive untouched.
func (ui *uiState) saveUnitEvents() (string, error) {
	if ui.unitEvtDoc == nil {
		return "", fmt.Errorf("no .whe is loaded")
	}
	if ui.unitEvtPath == "" {
		return "", fmt.Errorf("no .whe path to save")
	}
	if !ui.unitEvtDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.modTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	data, err := ui.unitEvtDoc.MarshalBinary()
	if err != nil {
		return "", err
	}
	disk, err := ui.modTree.WriteOverlayFile(ui.unitEvtPath, data)
	if err != nil {
		return "", err
	}
	ui.unitEvtDirty = false
	ui.reloadUnitModelFx()
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(ui.unitEvtPath), disk, len(data)), nil
}

func (ui *uiState) saveUnitEventsFromPanel() {
	line, err := ui.saveUnitEvents()
	if err != nil {
		ui.statusText = "Save failed: " + err.Error()
		return
	}
	ui.statusText = line
	ui.markStateDirty()
}

// revertUnitEvents throws the in-memory edits away and re-reads the file from
// the tree, which is the way back from an event added by mistake.
func (ui *uiState) revertUnitEvents() {
	forced := ui.unitEvtFor
	ui.unitEvtDoc = nil
	ui.unitEvtFor = ""
	ui.unitEvtTried = false
	ui.ensureUnitEvents()
	if ui.unitEvtFor == "" {
		ui.unitEvtFor = forced
	}
	ui.unitEvtDirty = false
	ui.unitEvtFormErr = ""
	ui.lastFormAction = ""
	ui.unitEvtSel = 0
	ui.unitEvtSelSet = map[int]bool{0: true}
	ui.unitEvtSelAnchor = 0
	ui.unitEvtBound = ""
	ui.unitEvtEntryBound = ""
	ui.statusText = "Changes to " + filepath.Base(ui.unitEvtPath) + " thrown away"
	// The document is the file's own copy again, so the viewport resolves it
	// rather than the edits that were thrown away.
	ui.reloadUnitModelFx()
}

// unitEventTimeLabel is how a moment reads in the event rows.
func unitEventTimeLabel(t float64) string {
	if t < 0 {
		return "enter"
	}
	if math.IsInf(t, 1) || t > 1e30 {
		return "leave"
	}
	return strconv.FormatFloat(t, 'f', 3, 64) + "s"
}

// ── the columns ────────────────────────────────────────────────────────────

// unitEventsMessage is the empty state of either column: why there is nothing
// to edit, in the same dimmed line the other sections use.
func (ui *uiState) unitEventsMessage(gtx layout.Context, msg string) layout.Dimensions {
	if msg == "" {
		msg = "There is nothing to edit here."
	}
	return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.th, unit.Sp(10), msg)
		l.Color = cTextDim
		l.MaxLines = 6
		return l.Layout(gtx)
	})
}

// layoutUnitEventTimelines is the middle column: every DATAMTON timeline of
// the .whe, the selected one highlighted, and which clips each drives.
func (ui *uiState) layoutUnitEventTimelines(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitEvents()
	sec := ui.currentUnitSection()
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.middleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, ui.layoutUnitEventTimelineList),
		)
	})
}

func (ui *uiState) layoutUnitEventTimelineList(gtx layout.Context) layout.Dimensions {
	if ui.unitEvtDoc == nil {
		return ui.unitEventsMessage(gtx, ui.unitEvtErr)
	}
	tls := ui.unitEvtDoc.Timelines()
	if len(tls) == 0 {
		return ui.unitEventsMessage(gtx, "This .whe carries no animation timelines.")
	}
	selected := ui.selectedUnitEventTimeline()
	selEvents := 0
	if selected != nil {
		selEvents = len(selected.Events)
	}
	ui.ensureUnitEventClicks(len(tls), selEvents)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := ui.unitEvtPath
			if ui.unitEvtDirty {
				text += "  \u2022 unsaved"
			}
			l := material.Label(ui.th, unit.Sp(9), text)
			l.Color = cTextDim
			l.MaxLines = 2
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutListWithScrollbar(gtx, &ui.unitEvtScroll, &ui.unitEvtList, unitRowHeightDp-4, len(tls), func(gtx layout.Context, i int) layout.Dimensions {
				return ui.layoutUnitEventTimelineRow(gtx, i, tls[i])
			})
		}),
	)
}

// layoutUnitEventPanel is the right column: the events of the selected
// timeline, the form that adds one, and the save/revert buttons.
func (ui *uiState) layoutUnitEventPanel(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitEvents()
	sec := ui.currentUnitSection()
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitEvtTransport(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.panelScrollView(gtx, &ui.unitEvtPanelScroll, func(gtx layout.Context) layout.Dimensions {
					return ui.unitEventPanelBody(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitEventPanelForm(gtx)
			}),
		)
	})
}

// unitEventPanelBody is the scrollable content of the right column.
func (ui *uiState) unitEventPanelBody(gtx layout.Context) layout.Dimensions {
	if ui.unitEvtDoc == nil {
		return ui.unitEventsMessage(gtx, ui.unitEvtErr)
	}
	inset := layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}
	tl := ui.selectedUnitEventTimeline()
	var children []layout.FlexChild
	if tl == nil {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitEventsMessage(gtx, "Select a timeline in the middle column.")
		}))
	} else {
		// The body below this point belongs to the selected timeline: its header,
		// the motion's own properties, its event rows and the selected event's
		// definition. The actions block after it is file-wide and shows even with
		// no timeline picked.
		// Which file and timeline this is, and whether it has unsaved changes.
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := "Timeline  " + tl.Name
			if len(tl.Animations) > 0 {
				text += "  (" + strings.Join(tl.Animations, ", ") + ")"
			}
			if ui.unitEvtDirty {
				text += "  • unsaved"
			}
			l := material.Label(ui.th, unit.Sp(9), text)
			l.Color = cTextDim
			l.MaxLines = 2
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}))
		// How the motion itself plays: the tail of the DATAMTON payload, which
		// the game reads for looping, delays and transitions even though the
		// events are what a reader edits most.
		if tl.Tail.Present {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.th, "Motion")
				})
			}))
			for _, row := range whe.MotionTailRows(tl.Tail) {
				row := row
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.unitKeyValueRow(gtx, row[0], row[1], false)
					})
				}))
			}
		}
		ui.ensureUnitEventClicks(len(ui.unitEvtDoc.Timelines()), len(tl.Events))
		defText := "No events on this timeline yet. Add one below."
		if len(tl.Events) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), defText)
				l.Color = cTextDim
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
			}))
		} else {
			defs := ui.unitEvtDoc.Events()
			for i := range tl.Events {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutUnitEventRow(gtx, i, tl.Events[i], defs)
				}))
			}
		}
		// What the selected event actually does, read from its definition.
		if ui.unitEvtSel >= 0 && ui.unitEvtSel < len(tl.Events) {
			ev := tl.Events[ui.unitEvtSel]
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.th, "Selected event")
				})
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), "Values are editable — Set or Enter applies, Save .whe writes the file.")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.unitEvtEntrySetClick, "Set")
						}),
					)
				})
			}))
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.unitKeyValueRow(gtx, "Name", ev.Name, false)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.unitKeyValueRow(gtx, "At", unitEventTimeLabel(ev.Time), false)
					})
				}),
			)
			def, ok := ui.unitEvtDoc.Events()[strings.ToLower(ev.Name)]
			if !ok {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(9), "No definition in this .whe — the game has nothing to play for it.")
					l.Color = rgb(0xC98A8A)
					return l.Layout(gtx)
				}))
			} else {
				// The values are typed fields: Enter writes the value into the
				// in-memory document and the viewport re-resolves from it, and
				// Save .whe writes the file. The fields refill for the selection
				// first, so a value typed for the event that was picked a moment
				// ago cannot land on this one.
				ui.ensureUnitEvtEntryEditors(len(def.Entries))
				ui.syncUnitEvtEntryEditors(ev.Name, def)
				ui.updateUnitEvtEntryEditors(gtx, ev.Name)
				for i := range def.Entries {
					i := i
					key := def.Entries[i].Key
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return ui.unitEvtEntryRow(gtx, i, key)
						})
					}))
				}
			}
		}
	}
	// The .whe's actions: which timelines each named action plays and which
	// other actions it calls, every choice guarded by an if / else if / else
	// condition — the chain the game walks when the action runs.
	if acts := ui.unitEvtDoc.Actions(); len(acts) > 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Actions")
			})
		}))
		for _, a := range acts {
			a := a
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitActionBlock(gtx, a)
			}))
		}
	}

	dims := inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	// Set applies what was typed in the value fields: the button answers even
	// when no field has focus, so it works where Enter in the field may not.
	if ui.unitEvtEntrySetClick.Clicked(gtx) {
		if tl := ui.selectedUnitEventTimeline(); tl != nil && ui.unitEvtSel >= 0 && ui.unitEvtSel < len(tl.Events) {
			ui.setUnitEvtEntryValues(tl.Events[ui.unitEvtSel].Name)
		}
	}
	return dims
}

// ensureUnitEvtEntryEditors sizes the value fields to the selected event's
// definition. Fields are only ever added: the panel draws one row per entry,
// and the ones past the end sit unused rather than the values being cut off.
func (ui *uiState) ensureUnitEvtEntryEditors(n int) {
	for len(ui.unitEvtEntryEds) < n {
		ui.unitEvtEntryEds = append(ui.unitEvtEntryEds, widget.Editor{SingleLine: true, Submit: true})
		ui.unitEvtEntryFillText = append(ui.unitEvtEntryFillText, "")
	}
}

// syncUnitEvtEntryEditors fills the value fields when another definition is
// selected — whatever the cursor is doing, the fields must show the event
// that is now picked. A value changed by Enter is refilled by the apply
// itself, so this only has to notice the selection.
func (ui *uiState) syncUnitEvtEntryEditors(name string, def whe.FxEvent) {
	bound := strings.ToLower(name) + "#" + strconv.Itoa(len(def.Entries))
	if bound == ui.unitEvtEntryBound {
		return
	}
	ui.unitEvtEntryBound = bound
	for i := range def.Entries {
		if i < len(ui.unitEvtEntryEds) {
			ui.unitEvtEntryEds[i].SetText(def.Entries[i].Value)
			if i < len(ui.unitEvtEntryFillText) {
				ui.unitEvtEntryFillText[i] = strings.TrimSpace(def.Entries[i].Value)
			}
		}
	}
}

// updateUnitEvtEntryEditors drains the value fields: Enter writes the typed
// value into the document and lets go of the field, the same way the bone
// key fields do.
func (ui *uiState) updateUnitEvtEntryEditors(gtx layout.Context, name string) {
	for i := range ui.unitEvtEntryEds {
		for {
			ev, ok := ui.unitEvtEntryEds[i].Update(gtx)
			if !ok {
				break
			}
			if _, submit := ev.(widget.SubmitEvent); submit {
				ui.applyUnitEvtEntry(name, i, ui.unitEvtEntryEds[i].Text())
				ui.unitEvtEntryEds[i].Blur(gtx)
			}
		}
	}
}

// applyUnitEvtEntry writes one typed value into the definition in memory —
// the DATAEVNT payload is rebuilt, the document is marked dirty, and the
// viewport re-resolves from the document so the change is on screen before
// the file is saved.
func (ui *uiState) applyUnitEvtEntry(name string, index int, value string) {
	if ui.unitEvtDoc == nil {
		ui.statusText = "No .whe is loaded"
		return
	}
	if err := ui.unitEvtDoc.SetEventEntry(name, index, strings.TrimSpace(value)); err != nil {
		ui.statusText = "Edit failed: " + err.Error()
		return
	}
	ui.unitEvtDirty = true
	// The field takes back what was stored, so what is shown is what the chunk
	// now holds rather than what was typed — and the fill text moves with it,
	// so the axis counts as untouched until it is changed again.
	if def, ok := ui.unitEvtDoc.Events()[strings.ToLower(name)]; ok && index < len(def.Entries) && index < len(ui.unitEvtEntryEds) {
		ui.unitEvtEntryEds[index].SetText(def.Entries[index].Value)
		if index < len(ui.unitEvtEntryFillText) {
			ui.unitEvtEntryFillText[index] = strings.TrimSpace(def.Entries[index].Value)
		}
	}
	ui.statusText = "Edited " + name + " — Save .whe to write it into the mod"
	ui.reloadUnitModelFx()
}

// setUnitEvtEntryValues applies every value field the user typed over, the
// way the bone panel's Set does: Enter in a field is meant to do the same,
// but a button does not depend on the field having focus. Fields left as they
// were filled are skipped, and the viewport re-resolves once at the end.
func (ui *uiState) setUnitEvtEntryValues(name string) {
	if ui.unitEvtDoc == nil {
		ui.statusText = "No .whe is loaded"
		return
	}
	def, ok := ui.unitEvtDoc.Events()[strings.ToLower(name)]
	if !ok {
		ui.statusText = "No definition of " + name + " in this .whe"
		return
	}
	changed := 0
	for i := range def.Entries {
		if i >= len(ui.unitEvtEntryEds) {
			break
		}
		text := strings.TrimSpace(ui.unitEvtEntryEds[i].Text())
		if i < len(ui.unitEvtEntryFillText) && text == ui.unitEvtEntryFillText[i] {
			continue
		}
		if err := ui.unitEvtDoc.SetEventEntry(name, i, text); err != nil {
			ui.statusText = "Edit failed: " + err.Error()
			if changed > 0 {
				ui.reloadUnitModelFx()
			}
			return
		}
		ui.unitEvtDirty = true
		if i < len(ui.unitEvtEntryFillText) {
			ui.unitEvtEntryFillText[i] = text
		}
		ui.unitEvtEntryEds[i].SetText(text)
		changed++
	}
	if changed == 0 {
		ui.statusText = "no values were changed"
		return
	}
	ui.statusText = "Edited " + name + " — Save .whe to write it into the mod"
	ui.reloadUnitModelFx()
}

// unitEvtEntryRow is one row of the selected event's definition: the entry's
// key, then its value in a field that submits on Enter. The key is clipped so
// a long name cannot push the value off the panel.
func (ui *uiState) unitEvtEntryRow(gtx layout.Context, i int, key string) layout.Dimensions {
	if i < 0 || i >= len(ui.unitEvtEntryEds) {
		return layout.Dimensions{}
	}
	ed := &ui.unitEvtEntryEds[i]
	ed.SingleLine = true
	ed.Submit = true
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(112)
			l := material.Label(ui.th, unit.Sp(9), key)
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(20)
			gtx.Constraints.Max.Y = gtx.Dp(22)
			b := widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
			return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := material.Editor(ui.th, ed, "")
					w.TextSize = unit.Sp(10)
					w.HintColor = cTextDim
					return w.Layout(gtx)
				})
			})
		}),
	)
}

// reloadUnitModelFx re-resolves the unit viewport's effects and sounds from
// the edited .whe document, so a change made in the Events section shows on
// the model before it is saved. The document is only handed over when it is
// the same file the viewport's model sits beside — a .whe read for another
// reason would put its events on this model.
func (ui *uiState) reloadUnitModelFx() {
	v := ui.unitModelViewer()
	if v == nil {
		return
	}
	var doc *whe.Document
	if ui.unitEvtDoc != nil && ui.unitEvtPath != "" && v.modelFile != "" {
		base := func(p string) string {
			if j := strings.LastIndexAny(p, "/\\"); j >= 0 {
				return p[j+1:]
			}
			return p
		}
		name := base(v.modelFile)
		if k := strings.LastIndexByte(name, '.'); k >= 0 {
			name = name[:k]
		}
		if strings.EqualFold(base(ui.unitEvtPath), name+".whe") {
			doc = ui.unitEvtDoc
		}
	}
	ui.fxReload(v, doc)
}

// unitActionBlock renders one DATAACTS action: its name, then each motion it
// plays and each sub-action it calls with the condition guarding that line.
func (ui *uiState) unitActionBlock(gtx layout.Context, a whe.FxAction) layout.Dimensions {
	var rows []layout.FlexChild
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), a.Name)
			l.Color = cText
			l.MaxLines = 1
			return l.Layout(gtx)
		})
	}))
	add := func(kind string, ln whe.FxActionLink) {
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := kind + "  " + ln.Name + "   · " + ln.Guard()
			return layout.Inset{Left: unit.Dp(12), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(9), text)
				l.Color = cTextDim
				l.MaxLines = 2
				return l.Layout(gtx)
			})
		}))
	}
	for _, m := range a.Motions {
		add("motion", m)
	}
	for _, s := range a.Subactions {
		add("action", s)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// unitEventPanelForm draws the static input form and action buttons at the bottom of the right column.
func (ui *uiState) unitEventPanelForm(gtx layout.Context) layout.Dimensions {
	tl := ui.selectedUnitEventTimeline()
	if tl == nil {
		return layout.Dimensions{}
	}
	var children []layout.FlexChild

	// The add form.
	children = append(children,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowEditor(gtx, "Event name", &ui.unitEvtNameEd, false, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowEditor(gtx, "Time (s / enter / leave)", &ui.unitEvtTimeEd, true, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowEditor(gtx, "FX path", &ui.unitEvtFxEd, false, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowEditor(gtx, "Sound path", &ui.unitEvtSndEd, false, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowEditor(gtx, "Marker (FX only)", &ui.unitEvtMarkerEd, true, "")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			noSel := ui.unitEvtSel < 0 || ui.unitEvtSel >= len(tl.Events)
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitEvtSet, "Set", noSel)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitEvtApply, "Apply", noSel)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.evtPlayer.playEv, "▶ Sound", !ui.unitEvtSelectedIsSound())
				}),
			)
		}),
	)
	if ui.unitEvtFormErr != "" {
		err := ui.unitEvtFormErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(9), err)
				l.Color = rgb(0xC98A8A)
				l.MaxLines = 4
				return l.Layout(gtx)
			})
		}))
	}
	// Add and remove, then the one-press crystal copy, then save/revert.
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &ui.unitEvtAddEvent, "Add event")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &ui.unitEvtAddFX, "Add FX")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &ui.unitEvtAddSound, "Add sound")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitEvtRemove, "Remove", len(tl.Events) == 0 || len(ui.unitEvtSelSet) == 0)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitEvtCopy, "Copy", len(ui.unitEvtSelSet) == 0)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, _, has := ui.unitClips.pick(clipEvents, ui.unitClipCur)
					return ui.smallButtonDisabled(gtx, &ui.unitEvtPaste, "Paste", !has)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, cur, ok := ui.unitClips.pick(clipEvents, ui.unitClipCur)
					first := !ok || ui.unitClips.step(clipEvents, cur, -1) == cur
					return ui.smallButtonDisabled(gtx, &ui.unitClipPrev, "\u25c2", first)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, cur, ok := ui.unitClips.pick(clipEvents, ui.unitClipCur)
					last := !ok || ui.unitClips.step(clipEvents, cur, 1) == cur
					return ui.smallButtonDisabled(gtx, &ui.unitClipNext, "\u25b8", last)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), ui.unitClips.label(clipEvents, ui.unitClipCur))
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					})
				}),
			)
		})
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.primaryButton(gtx, &ui.unitEvtSave, "Save .whe")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitEvtRevert, "Revert", !ui.unitEvtDirty)
				}),
			)
		})
	}))
	note := "Saved into this mod's own .whe; the game's files are never written."
	if ui.unitEvtDirty {
		note = "Unsaved changes. " + note
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return unitFieldNote(gtx, ui.th, note)
		})
	}))

	inset := layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutUnitEventRow is one event of the timeline: when it fires, what it
// fires, and what kind of event it is.
func (ui *uiState) layoutUnitEventRow(gtx layout.Context, i int, ev whe.FxTimelineEvent, defs map[string]whe.FxEvent) layout.Dimensions {
	if i < 0 || i >= len(ui.unitEvtClicks) {
		return layout.Dimensions{}
	}
	c := &ui.unitEvtClicks[i]
	selected := ui.unitEvtRowSelected(i)
	bg := cPanelBg
	switch {
	case selected:
		bg = cSelectBg
	case c.Hovered():
		bg = cMenuHover
	}
	kind := unitEventKindTag(ev.Name, defs)
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), unitEventTimeLabel(ev.Time))
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), unitViewportRowText(ev.Name))
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), kind)
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

// unitEventKindTag says what an event does, from its definition: a sound, an
// effect at a marker, an effect with no marker of its own, or nothing at all.
func unitEventKindTag(name string, defs map[string]whe.FxEvent) string {
	def, ok := defs[strings.ToLower(name)]
	if !ok {
		return "undefined"
	}
	if _, has := def.First("sound"); has {
		return "sound"
	}
	if _, has := def.First("fx"); has {
		if _, has := def.First("marker"); has {
			return "FX @ marker"
		}
		return "FX"
	}
	return "event"
}

// layoutUnitEventTimelineRow is one selectable timeline: its name, and the
// clips it drives.
func (ui *uiState) layoutUnitEventTimelineRow(gtx layout.Context, i int, tl whe.FxTimeline) layout.Dimensions {
	if i < 0 || i >= len(ui.unitEvtTlClicks) {
		return layout.Dimensions{}
	}
	c := &ui.unitEvtTlClicks[i]
	selected := strings.EqualFold(ui.unitEvtTimeline, tl.Name)
	bg := cPanelBg
	switch {
	case selected:
		bg = cSelectBg
	case c.Hovered():
		bg = cMenuHover
	}
	clips := strings.Join(tl.Animations, ", ")
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), unitViewportRowText(tl.Name))
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							note := fmt.Sprintf("%d events", len(tl.Events))
							if clips != "" {
								note += "  \u00b7  " + clips
							}
							l := material.Label(ui.th, unit.Sp(9), note)
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}
