package app

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"
	"math"
	"path/filepath"
	"strings"
)

// handleUnitEventsInput owns the presses and typing of the Events section. It
// runs on every frame while the section is on screen, ahead of the layout that
// draws what it decided.
func HandleUnitEventsInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).Label != unitSectionEvents && !ClassicOEDialogOpen(ui) {
		// Leaving the section silences whatever it was playing: a timeline
		// that keeps sounding behind another section is a leak, not a feature.
		// The Classic OE double-click dialog is the exception: while it is
		// up, the editors it embeds are the section, wherever the panel sits.
		if ui.EvtPlayer.Playing || ui.EvtPlayer.Audition != nil {
			ui.EvtPlayer.StopAll()
		}
		return
	}
	EnsureUnitEvents(ui)
	// The sound transport first: its clicks are its own, and the playhead it
	// drives must advance before the rows below are read.
	UnitEvtPlayerTick(ui, gtx)
	// The modifiers first: the clicks below are read in this same pass, and a
	// Ctrl held down for a click arrives as key events of its own.
	UnitEvtTrackModifiers(ui, gtx)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtNameEd, evtPickName, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtTimeEd, evtPickNone, true)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtFxEd, evtPickFX, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtSndEd, evtPickSound, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtMarkerEd, evtPickMarker, false)
	UnitEvtUpdateShiftEditor(ui, gtx)
	if ui.UnitEvtDoc == nil {
		return
	}
	// Fill the form from the selected row whenever the selection moves, so
	// re-firing an existing effect is a time edit rather than a retyping.
	SyncUnitEventForm(ui)
	timelines := ui.UnitEvtDoc.Timelines()
	tl := SelectedUnitEventTimeline(ui)
	events := 0
	if tl != nil {
		events = len(tl.Events)
	}
	EnsureUnitEventClicks(ui, len(timelines), events)
	for i := range ui.UnitEvtTlClicks {
		if i < len(timelines) && ui.UnitEvtTlClicks[i].Clicked(gtx) {
			ui.UnitEvtFocusAction = false
			SelectUnitEventTimeline(ui, timelines[i].Name)
			tl = SelectedUnitEventTimeline(ui)
			events = 0
			if tl != nil {
				events = len(tl.Events)
			}
			EnsureUnitEventClicks(ui, len(timelines), events)
		}
	}
	// The action list under the timelines picks which action's conditions the
	// Events column draws beside it, and moves that column onto the actions —
	// the panel shows one or the other, never both.
	acts := ui.UnitEvtDoc.Actions()
	EnsureUnitEvtActionClicks(ui, len(acts))
	for i := range ui.UnitEvtActionClicks {
		if i < len(acts) && ui.UnitEvtActionClicks[i].Clicked(gtx) {
			UnitEvtBlurFields(ui, gtx)
			// The panel leaves the timeline behind completely, and a sound
			// it was auditioning belongs to that timeline.
			if !ui.UnitEvtFocusAction {
				ui.EvtPlayer.StopAll()
			}
			ui.UnitEvtFocusAction = true
			ui.UnitEvtActionSel = i
			// Selecting an action also plays it: every clip its graph
			// resolves to, combined in the viewport beside the panel.
			ActivateUnitAction(ui, acts[i].Name)
		}
	}
	// While an action is the panel on screen, its conditions are the fields
	// being edited: their typing, the guard chips and the commit below.
	if ui.UnitEvtFocusAction {
		UnitActConditionsInput(ui, gtx)
	}
	for i := range ui.UnitEvtClicks {
		if i >= events || !ui.UnitEvtClicks[i].Clicked(gtx) {
			continue
		}
		// A row click is a selection, and it lets go of any field being typed
		// into so the caret does not hold the clipboard shortcuts hostage.
		// Plain picks the one row, Ctrl toggles it in the group, and Shift
		// takes the range from the anchor the last plain or Ctrl click left.
		UnitEvtBlurFields(ui, gtx)
		// The modifiers are read from the OS at the click itself. The
		// tracked flags depend on this section's key filters winning the
		// Ctrl/Shift events ahead of every other filter in the frame, and a
		// selection that appears only when that race is won is no selection
		// at all; the live key state cannot race. The tracked flags stay as
		// the fallback where there is no live reader.
		live := heldModifiers()
		ctrl := ui.UnitEvtCtrlHeld || live.Contain(key.ModCtrl)
		shift := ui.UnitEvtShiftHeld || live.Contain(key.ModShift)
		LogInfo("evt: row %d click ctrl=%v shift=%v (tracked %v/%v live=%v anchor=%d)",
			i, ctrl, shift, ui.UnitEvtCtrlHeld, ui.UnitEvtShiftHeld, live, ui.UnitEvtSelAnchor)
		switch {
		case ctrl:
			if ui.UnitEvtSelSet == nil {
				ui.UnitEvtSelSet = map[int]bool{}
			}
			if ui.UnitEvtSelSet[i] {
				delete(ui.UnitEvtSelSet, i)
			} else {
				ui.UnitEvtSelSet[i] = true
			}
			ui.UnitEvtSelAnchor = i
		case shift:
			lo, hi := i, ui.UnitEvtSelAnchor
			if lo > hi {
				lo, hi = hi, lo
			}
			ui.UnitEvtSelSet = map[int]bool{}
			for j := lo; j <= hi && j < events; j++ {
				ui.UnitEvtSelSet[j] = true
			}
		default:
			ui.UnitEvtSelSet = map[int]bool{i: true}
			ui.UnitEvtSelAnchor = i
		}
		ui.UnitEvtSel = i
		ui.UnitEvtBound = ""
		break
	}
	// Rows can disappear under a Remove, so the group loses what no longer
	// exists before anything reads it.
	for i := range ui.UnitEvtSelSet {
		if i >= events {
			delete(ui.UnitEvtSelSet, i)
		}
	}
	if ui.UnitEvtAddFX.Clicked(gtx) {
		AddUnitEventFromForm(ui, "fx")
		return
	}
	if ui.UnitEvtAddEvent.Clicked(gtx) {
		AddUnitEventFromForm(ui, "event")
		return
	}
	if ui.UnitEvtAddSound.Clicked(gtx) {
		AddUnitEventFromForm(ui, "sound")
		return
	}
	if ui.UnitEvtRemove.Clicked(gtx) {
		RemoveSelectedUnitEvent(ui)
		return
	}
	// The animation list's per-row Remove buttons each drop one blend entry
	// from the selected timeline. They are only laid out for a selected
	// timeline, so with none (or while an action is shown) no press can land.
	if tl != nil {
		EnsureUnitEventAnimClicks(ui, len(tl.Animations))
		for i := range ui.UnitEvtAnimClicks {
			if i < len(tl.Animations) && ui.UnitEvtAnimClicks[i].Clicked(gtx) {
				RemoveUnitTimelineAnimation(ui, i)
				return
			}
		}
	}
	if ui.UnitEvtSave.Clicked(gtx) {
		SaveUnitEventsFromPanel(ui)
		return
	}
	if ui.UnitEvtRevert.Clicked(gtx) {
		RevertUnitEvents(ui)
		return
	}
	if ui.UnitEvtSet.Clicked(gtx) || ui.UnitEvtApply.Clicked(gtx) {
		ApplySelectedEventForm(ui)
		return
	}
	if ui.UnitEvtShift.Clicked(gtx) {
		ShiftSelectedUnitEventTimes(ui)
		return
	}
	if ui.UnitEvtCopyAct.Clicked(gtx) {
		CopySelectedUnitAction(ui)
		return
	}
	if ui.UnitEvtPasteAct.Clicked(gtx) {
		PasteUnitAction(ui)
		return
	}
	if ui.UnitEvtCopy.Clicked(gtx) {
		CopySelectedUnitEvents(ui)
		return
	}
	if ui.UnitEvtCopyTl.Clicked(gtx) {
		CopySelectedUnitTimeline(ui)
		return
	}
	if ui.UnitEvtPaste.Clicked(gtx) {
		PasteUnitEvents(ui)
		return
	}
	if ui.UnitClipPrev.Clicked(gtx) {
		StepUnitClip(ui, -1)
	}
	if ui.UnitClipNext.Clicked(gtx) {
		StepUnitClip(ui, 1)
	}
	// Ctrl+C and Ctrl+V repeat those two buttons — unless a field holds the
	// caret, for while it does the text being edited keeps its own clipboard.
	if !UnitEvtFieldFocused(ui, gtx) {
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
				CopySelectedUnitEvents(ui)
			case "V":
				PasteUnitEvents(ui)
			}
		}
	}
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtNameEd, evtPickName, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtTimeEd, evtPickNone, true)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtFxEd, evtPickFX, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtSndEd, evtPickSound, false)
	UnitEvtUpdateEditor(ui, gtx, &ui.UnitEvtMarkerEd, evtPickMarker, false)
	UnitEvtUpdateShiftEditor(ui, gtx)
}

// unitEvtUpdateEditor processes one form field's typing. A press into the row
// focuses it, the same way the attribute editor's value field does. Enter
// submits: it applies the fields the way Set does and never drops a line
// break into the value.
//
// batchTime says the field belongs to the whole selection rather than to the
// one row the form shows. Only the moment does: Enter in the path or marker
// field while several rows are selected must not quietly move all of their
// times to whichever moment the primary row happened to have.
func UnitEvtUpdateEditor(ui *uiState, gtx layout.Context, ed *widget.Editor, pick evtPickTarget, batchTime bool) {
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		// A field with a list opens its picker instead of taking the caret: the
		// picker's own filter takes the typing.
		if pick != evtPickNone {
			OpenEvtPicker(ui, pick)
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
			if batchTime {
				ApplySelectedEventForm(ui)
			} else {
				ApplySelectedEventFormSingle(ui)
			}
			ed.Blur(gtx)
		}
	}
}

// unitEvtUpdateShiftEditor owns the batch shift field: a click takes the
// caret, Enter shifts the selection and consumes the number. It is kept out of
// unitEvtUpdateEditor because submitting it is not an apply of the form — the
// name, path and marker fields have nothing to do with a shift.
func UnitEvtUpdateShiftEditor(ui *uiState, gtx layout.Context) {
	ed := &ui.UnitEvtShiftEd
	ed.SingleLine = true
	ed.Submit = true
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ShiftSelectedUnitEventTimes(ui)
			ed.Blur(gtx)
		}
	}
}

// addUnitEventFromForm fires an event on the selected timeline. An FX keeps
// its marker binding from the definition it names; a sound names the folder
// under data/sound it resolves to. Filling in no path at all re-fires an
// event the .whe already defines, which is how an existing effect is put on a
// second clip.
func AddUnitEventFromForm(ui *uiState, kind string) {
	ApplyUnitEvent(ui, kind)
}

func ApplyUnitEvent(ui *uiState, kind string) {
	ui.LastFormAction = kind
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.UnitEvtFormErr = "No .whe is loaded."
		ui.LastFormAction = ""
		return
	}
	if ui.UnitEvtTimeline == "" {
		ui.UnitEvtFormErr = "Select a timeline first."
		ui.LastFormAction = ""
		return
	}
	name := strings.TrimSpace(ui.UnitEvtNameEd.Text())
	if name == "" {
		ui.UnitEvtFormErr = "Event name is required."
		ui.LastFormAction = ""
		return
	}
	at, err := unitEventTime(ui.UnitEvtTimeEd.Text())
	if err != nil {
		ui.UnitEvtFormErr = err.Error()
		ui.LastFormAction = ""
		return
	}
	// A sound takes its path from the sound field, an effect from the FX field;
	// an event that is only being placed takes neither.
	target := strings.TrimSpace(ui.UnitEvtFxEd.Text())
	if kind == "sound" {
		target = strings.TrimSpace(ui.UnitEvtSndEd.Text())
	}
	var definition *whe.FxEvent
	soundPath := ""
	switch kind {
	case "sound":
		if target == "" {
			ui.UnitEvtFormErr = "A sound path is required (relative to data\\sound)."
			ui.LastFormAction = ""
			return
		}
		if !strings.HasPrefix(strings.ToLower(name), "sound") {
			name = "sound_" + name
			ui.UnitEvtNameEd.SetText(name)
		}
		soundPath = target
	case "event":
		// Placing an event on the timeline needs no definition of its own: the
		// event must already be defined in this .whe, which AddTimedEvent checks.
	default:
		marker := strings.TrimSpace(ui.UnitEvtMarkerEd.Text())
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

	if err := doc.AddTimedEvent(ui.UnitEvtTimeline, whe.FxTimelineEvent{Name: name, Time: at}, definition, soundPath); err != nil {
		ui.UnitEvtFormErr = err.Error()
		ui.LastFormAction = ""
		return
	}

	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	ui.LastFormAction = kind
	// Land the selection on the row that was just added.
	if tl := SelectedUnitEventTimeline(ui); tl != nil {
		for i := range tl.Events {
			if strings.EqualFold(tl.Events[i].Name, name) && tl.Events[i].Time == at {
				ui.UnitEvtSel = i
				ui.UnitEvtSelSet = map[int]bool{i: true}
				ui.UnitEvtSelAnchor = i
			}
		}
		ui.UnitEvtBound = ""
	}
	ReloadUnitModelFx(ui)
	StatusEventAdded(ui, name, at)
}

// removeSelectedUnitEvent drops the selected rows from its timeline — every
// row of the group, not just the one showing below. The definitions themselves
// are kept: other clips and the blueprint may still fire them, and an orphan
// definition is harmless where a missing one is a broken effect.
func RemoveSelectedUnitEvent(ui *uiState) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.UnitEvtFormErr = "Select an event to remove."
		ui.LastFormAction = ""
		return
	}
	indices := UnitEvtSelectedIndices(ui)
	if len(indices) == 0 {
		ui.UnitEvtFormErr = "Select an event to remove."
		ui.LastFormAction = ""
		return
	}
	// Descending, so taking a row cannot shift the ones still to come.
	removed := 0
	for k := len(indices) - 1; k >= 0; k-- {
		i := indices[k]
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		if err := doc.RemoveTimedEvent(ui.UnitEvtTimeline, i); err != nil {
			ui.UnitEvtFormErr = err.Error()
			ui.LastFormAction = ""
			return
		}
		removed++
	}
	if removed == 0 {
		ui.UnitEvtFormErr = "Select an event to remove."
		ui.LastFormAction = ""
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	ui.LastFormAction = "remove"
	ui.UnitEvtSelSet = map[int]bool{}
	if tl := SelectedUnitEventTimeline(ui); tl != nil {
		if ui.UnitEvtSel >= len(tl.Events) {
			ui.UnitEvtSel = len(tl.Events) - 1
		}
		// The selection lands on whichever row took the removed one's place,
		// so the panel below and the buttons keep something to act on.
		if ui.UnitEvtSel >= 0 {
			ui.UnitEvtSelSet[ui.UnitEvtSel] = true
		}
	}
	ui.UnitEvtBound = ""
	ReloadUnitModelFx(ui)
	ui.StatusText = fmt.Sprintf("Removed %d event(s) from %s — save to write it into the mod", removed, ui.UnitEvtTimeline)
}

// removeUnitTimelineAnimation drops one entry from the selected timeline's
// animation blend list. It edits the in-memory document only — Save .whe
// writes it into the mod, matching how every other timeline edit behaves.
func RemoveUnitTimelineAnimation(ui *uiState, index int) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.UnitEvtFormErr = "Select a timeline's animation to remove."
		ui.LastFormAction = ""
		return
	}
	if index < 0 || index >= len(tl.Animations) {
		ui.UnitEvtFormErr = "Animation selection is no longer valid."
		ui.LastFormAction = ""
		return
	}
	if err := doc.RemoveTimelineAnimation(ui.UnitEvtTimeline, index); err != nil {
		ui.UnitEvtFormErr = err.Error()
		ui.LastFormAction = ""
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	ui.LastFormAction = "remove"
	ReloadUnitModelFx(ui)
	ui.StatusText = fmt.Sprintf("Removed an animation from %s — save to write it into the mod", ui.UnitEvtTimeline)
}

// pasteTimelineClip writes a copied timeline into doc under its own name: it is
// created from the copied payload when the file has never carried it, and
// merged into the one it has when it does. Rows the destination already fires
// at the same moment are left alone, and a definition the file already defines
// is used as it stands — reconciling two authored effects is the reader's call.
func pasteTimelineClip(doc *whe.Document, clip *wheEventClip) (timelinePaste, error) {
	var res timelinePaste
	if doc == nil || clip == nil || strings.TrimSpace(clip.Timeline) == "" {
		return res, fmt.Errorf("nothing to paste")
	}
	res.Name = strings.TrimSpace(clip.Timeline)
	if _, exists := doc.TimelinePayload(res.Name); !exists {
		if err := doc.AddTimeline(res.Name, clip.Payload); err != nil {
			return res, err
		}
		res.Created = true
	}
	for _, anim := range clip.Animations {
		wrote, err := doc.AddTimelineAnimation(res.Name, anim)
		if err != nil {
			return res, err
		}
		if wrote {
			res.Anims++
		}
	}
	// The definitions this .whe already carries, kept up to date as the paste
	// writes more of them; only the created path needs the set. An existing
	// timeline is read by slot, so a paste into one that half carries the copy
	// cannot double every row.
	known := map[string]bool{}
	if res.Created {
		for name := range doc.Events() {
			known[name] = true
		}
	} else if tl, ok := timelineOf(doc, res.Name); ok {
		for _, ev := range tl.Events {
			known[fxEventSlot(ev)] = true
		}
	}
	for _, ev := range clip.Events {
		if res.Created {
			// The timeline was written from the copied payload, so its events
			// are already listed; only the definitions behind them still have
			// to come over. One this .whe already has is left exactly as it is.
			key := clipEventKey(ev.Name)
			if d, have := clip.Defs[key]; have && !known[key] {
				if err := doc.AddEventDefinition(d); err != nil {
					res.Skipped++
					if res.FirstSkip == "" {
						res.FirstSkip = err.Error()
					}
					continue
				}
				known[key] = true
			}
			res.Events++
			continue
		}
		if known[fxEventSlot(ev)] {
			res.Present++
			continue
		}
		if err := doc.AddTimedEvent(res.Name, ev, clipEventDefinition(clip, ev.Name), ""); err != nil {
			res.Skipped++
			if res.FirstSkip == "" {
				res.FirstSkip = err.Error()
			}
			continue
		}
		known[fxEventSlot(ev)] = true
		res.Events++
	}
	return res, nil
}

// pasteUnitEvents writes the buffer's events into a timeline at their original
// moments, bringing the definitions Copy snapshotted along. A name the .whe
// already defines differently is not overwritten — AddTimedEvent says so — and
// that event is skipped and reported instead, so reconciling two authored
// markers stays the reader's call.
//
// A copy of a few rows goes into the timeline that is selected. A copy of a
// whole timeline goes to the timeline of its own name: merged into the one
// this .whe already carries, or created here from the copied payload when the
// file has never had one — its name is the clip it drives, so that is where it
// belongs. The copied name is never silently written under another one.
func PasteUnitEvents(ui *uiState) {
	entry, _, ok := ui.UnitClips.Pick(clipEvents, ui.UnitClipCur)
	if !ok || entry.Events == nil || len(entry.Events.Events) == 0 {
		ui.StatusText = "The event clipboard is empty — Copy first"
		return
	}
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.StatusText = "No .whe is loaded — select a unit whose model has one first"
		return
	}
	clip := entry.Events
	if clip.Timeline != "" {
		res, err := pasteTimelineClip(doc, clip)
		if err != nil {
			ui.StatusText = "Paste failed: " + err.Error()
			return
		}
		if res.Events == 0 && res.Present == 0 {
			if res.FirstSkip != "" {
				ui.StatusText = "Paste failed: " + res.FirstSkip
			} else {
				ui.StatusText = fmt.Sprintf("Nothing to paste — %s carries no events", res.Name)
			}
			return
		}
		ui.UnitEvtDirty = true
		ui.UnitEvtBound = ""
		// A .whe read as carrying no timelines may have just gained one.
		ui.UnitEvtErr = ""
		ui.UnitEvtTimeline = res.Name
		ui.UnitEvtSel = 0
		ui.UnitEvtSelSet = map[int]bool{0: true}
		ui.UnitEvtSelAnchor = 0
		ReloadUnitModelFx(ui)
		ui.StatusText = timelinePasteMessage(res)
		return
	}
	target := ui.UnitEvtTimeline
	if target == "" {
		ui.StatusText = "Select a timeline first"
		return
	}
	added, skipped, present := 0, 0, 0
	firstSkip := ""
	var pasted []whe.FxTimelineEvent
	// An event the destination already fires at the same moment is not a
	// conflict to report, it is already there: pasting the same rows twice
	// must not double every row.
	known := map[string]bool{}
	if tl, ok := timelineOf(doc, target); ok {
		for _, ev := range tl.Events {
			known[fxEventSlot(ev)] = true
		}
	}
	for _, ev := range clip.Events {
		if known[fxEventSlot(ev)] {
			present++
			continue
		}
		if err := doc.AddTimedEvent(target, ev, clipEventDefinition(clip, ev.Name), ""); err != nil {
			skipped++
			if firstSkip == "" {
				firstSkip = err.Error()
			}
			continue
		}
		known[fxEventSlot(ev)] = true
		added++
		pasted = append(pasted, ev)
	}
	if added == 0 {
		if present > 0 {
			ui.StatusText = fmt.Sprintf("Nothing to paste — %d event(s) are already in %s", present, target)
			return
		}
		ui.StatusText = "Paste failed: " + firstSkip
		return
	}
	ui.UnitEvtDirty = true
	ui.UnitEvtBound = ""
	// A .whe read as carrying no timelines may have just gained one.
	ui.UnitEvtErr = ""
	// Pasted rows land at the end of the timeline; the selection follows them
	// so what was just pasted is what the panels below show.
	ui.UnitEvtSelSet = map[int]bool{}
	if tl := SelectedUnitEventTimeline(ui); tl != nil {
		for _, ev := range pasted {
			for i := len(tl.Events) - 1; i >= 0; i-- {
				if tl.Events[i].Name == ev.Name && tl.Events[i].Time == ev.Time {
					ui.UnitEvtSelSet[i] = true
					ui.UnitEvtSel = i
					break
				}
			}
		}
	}
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Pasted %d event(s) into %s", added, target)
	if present > 0 {
		msg += fmt.Sprintf(", %d already there", present)
	}
	if skipped > 0 {
		msg += fmt.Sprintf(", %d skipped: %s", skipped, firstSkip)
	}
	ui.StatusText = msg
}

// applySelectedEventForm is the panel's Apply and Set: with a group selected
// it writes the typed moment into every selected row, and with a single row
// selected it writes the whole form — time, path and marker — into that row.
func ApplySelectedEventForm(ui *uiState) {
	ApplyEventForm(ui, true)
}

// applySelectedEventFormSingle is the path, marker and name fields' own Enter:
// those describe the one row the form shows, so they never fan out over the
// selection.
func ApplySelectedEventFormSingle(ui *uiState) {
	ApplyEventForm(ui, false)
}

// applyEventForm writes the form's typed fields into the selected event of the
// in-memory document: the moment it fires at on the timeline and the path and
// marker its definition carries. Nothing touches the file — Save .whe does
// that — and the viewport re-resolves from the document, so the edited event
// shows on the model before it is written out. The event's name is its identity
// across the file, so a changed name is refused rather than half-renamed.
func ApplyEventForm(ui *uiState, allowBatchTime bool) {
	if ui.UnitEvtDoc == nil {
		ui.UnitEvtFormErr = "No .whe is loaded."
		return
	}
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events) {
		ui.UnitEvtFormErr = "Select an event to apply the fields to."
		return
	}
	// A group selection is the batch form of this button. The moment an event
	// fires at is the one field that means the same thing for every row — a
	// name is each row's own identity, and a path and marker belong to the
	// definition they came from — so a multi-row Apply writes the typed time
	// to all selected rows and leaves the rest of the form to the one row it
	// shows.
	if indices := UnitEvtSelectedIndices(ui); allowBatchTime && len(indices) > 1 {
		at, err := unitEventTime(ui.UnitEvtTimeEd.Text())
		if err != nil {
			ui.UnitEvtFormErr = err.Error()
			return
		}
		ApplyUnitEventTimes(ui, indices, at)
		return
	}
	ev := tl.Events[ui.UnitEvtSel]
	if name := strings.TrimSpace(ui.UnitEvtNameEd.Text()); !strings.EqualFold(name, ev.Name) {
		ui.UnitEvtFormErr = "The event name is this event's identity — keep it as selected when applying."
		return
	}
	at, err := unitEventTime(ui.UnitEvtTimeEd.Text())
	if err != nil {
		ui.UnitEvtFormErr = err.Error()
		return
	}
	def0 := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	path := strings.TrimSpace(ui.UnitEvtFxEd.Text())
	if _, isSound := def0.First("sound"); isSound || strings.HasPrefix(strings.ToLower(ev.Name), "sound") {
		path = strings.TrimSpace(ui.UnitEvtSndEd.Text())
	}
	marker := strings.TrimSpace(ui.UnitEvtMarkerEd.Text())
	def, hasDef := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !hasDef && (path != "" || marker != "") {
		ui.UnitEvtFormErr = "This event has no definition in the .whe — path and marker cannot be applied."
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
		ui.UnitEvtFormErr = ""
		ui.StatusText = "no changes to apply"
		return
	}
	// The time is written first and the definition after it, so a definition
	// that cannot be written leaves the moment already correct in memory and
	// marked as an edit rather than silently dropped.
	wrote := false
	if timeChanged {
		if err := ui.UnitEvtDoc.SetTimedEventTime(ui.UnitEvtTimeline, ui.UnitEvtSel, at); err != nil {
			ui.UnitEvtFormErr = err.Error()
			if wrote {
				ReloadUnitModelFx(ui)
			}
			return
		}
		wrote = true
	}
	if pathChanged {
		if err := ui.UnitEvtDoc.SetEventEntryByKey(ev.Name, pathKey, path); err != nil {
			ui.UnitEvtFormErr = err.Error()
			if wrote {
				ReloadUnitModelFx(ui)
			}
			return
		}
		wrote = true
	}
	if markerChanged {
		if err := ui.UnitEvtDoc.SetEventEntryByKey(ev.Name, "marker", marker); err != nil {
			ui.UnitEvtFormErr = err.Error()
			if wrote {
				ReloadUnitModelFx(ui)
			}
			return
		}
		wrote = true
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	// The form and the definition's value fields refill from what was just
	// written, so what is shown is what the document holds.
	ui.UnitEvtBound = ""
	ui.UnitEvtEntryBound = ""
	ui.UnitActCondBound = ""
	ReloadUnitModelFx(ui)
	ui.StatusText = "Applied to " + ev.Name + " — Save .whe to write it into the mod"
}

// shiftSelectedUnitEventTimes moves every selected row of the timeline by the
// typed number of seconds. Everything the selection holds moves together — an
// FX, a sound, an FX bound to a marker — because the shift acts on when a row
// fires, not on what it fires.
//
// The entry and exit sentinels stay where they are: "on enter" and "on leave"
// are not moments a shift can move. A row that would land before the clip
// starts stops at 0s instead of firing at a negative time. The field is
// emptied by a shift that moved anything, so a second press cannot move the
// same rows again.
func ShiftSelectedUnitEventTimes(ui *uiState) {
	if ui.UnitEvtDoc == nil {
		ui.UnitEvtFormErr = "No .whe is loaded."
		return
	}
	delta, err := unitEventShift(ui.UnitEvtShiftEd.Text())
	if err != nil {
		ui.UnitEvtFormErr = err.Error()
		return
	}
	tl := SelectedUnitEventTimeline(ui)
	indices := UnitEvtSelectedIndices(ui)
	if len(indices) == 0 && ui.UnitEvtSel >= 0 {
		// A single row is a selection of one: the shift works without the
		// Ctrl+click ritual the batch tools otherwise need.
		indices = []int{ui.UnitEvtSel}
	}
	if tl == nil || len(indices) == 0 {
		ui.UnitEvtFormErr = "Select the events to shift."
		return
	}
	if delta == 0 {
		ui.UnitEvtFormErr = ""
		ui.StatusText = "A shift of 0s moves nothing"
		return
	}
	moved, sentinel, clamped := 0, 0, 0
	firstErr := ""
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		cur := tl.Events[i].Time
		if cur < 0 || math.IsInf(cur, 1) || cur > 1e30 {
			sentinel++
			continue
		}
		at := cur + delta
		if at < 0 {
			at = 0
			clamped++
		}
		if err := ui.UnitEvtDoc.SetTimedEventTime(ui.UnitEvtTimeline, i, at); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		moved++
	}
	if moved == 0 {
		if firstErr != "" {
			ui.UnitEvtFormErr = firstErr
			return
		}
		ui.UnitEvtFormErr = ""
		if sentinel > 0 {
			ui.StatusText = fmt.Sprintf("Nothing shifted — %d event(s) fire on enter/leave, which have no moment to move", sentinel)
			return
		}
		ui.StatusText = "Nothing shifted"
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	// The form refills from the row it shows, whose moment has just moved.
	ui.UnitEvtBound = ""
	ui.UnitEvtShiftEd.SetText("")
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Shifted %d event(s) by %s", moved, unitEventShiftLabel(delta))
	if sentinel > 0 {
		msg += fmt.Sprintf(", %d on enter/leave left as they were", sentinel)
	}
	if clamped > 0 {
		msg += fmt.Sprintf(", %d stopped at 0s", clamped)
	}
	if firstErr != "" {
		msg += ", some refused: " + firstErr
	}
	ui.StatusText = msg + " — Save .whe to write it into the mod"
}

// applyUnitEventTimes writes one moment into every selected row of the
// timeline — the batch edit behind Apply while a group is selected. Events
// that already fire at that moment are counted rather than rewritten, and a
// row the document refuses is reported without stopping the rest: one damaged
// row must not leave the group half-moved with no word about it.
func ApplyUnitEventTimes(ui *uiState, indices []int, at float64) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.UnitEvtFormErr = "Select an event to apply the fields to."
		return
	}
	moved, same := 0, 0
	firstErr := ""
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		if tl.Events[i].Time == at {
			same++
			continue
		}
		if err := doc.SetTimedEventTime(ui.UnitEvtTimeline, i, at); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		moved++
	}
	if moved == 0 {
		if firstErr != "" {
			ui.UnitEvtFormErr = firstErr
			return
		}
		ui.UnitEvtFormErr = ""
		ui.StatusText = fmt.Sprintf("All %d selected event(s) already start at %s", same, unitEventTimeLabel(at))
		return
	}
	ui.UnitEvtFormErr = ""
	ui.UnitEvtDirty = true
	// The time field refills from the row the form shows, which has just moved.
	ui.UnitEvtBound = ""
	ReloadUnitModelFx(ui)
	msg := fmt.Sprintf("Set %s on %d event(s)", unitEventTimeLabel(at), moved)
	if same > 0 {
		msg += fmt.Sprintf(", %d already there", same)
	}
	if firstErr != "" {
		msg += ", some refused: " + firstErr
	}
	ui.StatusText = msg + " — Save .whe to write it into the mod"
}

// saveUnitEvents writes the edited .whe into the mod's own overlay, leaving
// the installed file and the SGA archive untouched.
func SaveUnitEvents(ui *uiState) (string, error) {
	if ui.UnitEvtDoc == nil {
		return "", fmt.Errorf("no .whe is loaded")
	}
	if ui.UnitEvtPath == "" {
		return "", fmt.Errorf("no .whe path to save")
	}
	if !ui.UnitEvtDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.ModTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	data, err := ui.UnitEvtDoc.MarshalBinary()
	if err != nil {
		return "", err
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitEvtPath, data)
	if err != nil {
		return "", err
	}
	ui.UnitEvtDirty = false
	ReloadUnitModelFx(ui)
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(ui.UnitEvtPath), disk, len(data)), nil
}

func SaveUnitEventsFromPanel(ui *uiState) {
	line, err := SaveUnitEvents(ui)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = line
	MarkStateDirty(ui)
}

// revertUnitEvents throws the in-memory edits away and re-reads the file from
// the tree, which is the way back from an event added by mistake.
func RevertUnitEvents(ui *uiState) {
	forced := ui.UnitEvtFor
	ui.UnitEvtDoc = nil
	ui.UnitEvtFor = ""
	ui.UnitEvtTried = false
	EnsureUnitEvents(ui)
	if ui.UnitEvtFor == "" {
		ui.UnitEvtFor = forced
	}
	ui.UnitEvtDirty = false
	ui.UnitEvtFormErr = ""
	ui.LastFormAction = ""
	ui.UnitEvtSel = 0
	ui.UnitEvtSelSet = map[int]bool{0: true}
	ui.UnitEvtSelAnchor = 0
	ui.UnitEvtBound = ""
	ui.UnitEvtEntryBound = ""
	ui.UnitActCondBound = ""
	ui.StatusText = "Changes to " + filepath.Base(ui.UnitEvtPath) + " thrown away"
	// The document is the file's own copy again, so the viewport resolves it
	// rather than the edits that were thrown away.
	ReloadUnitModelFx(ui)
}
