package app

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"strings"
)

// ensureUnitEvents reads the .whe of the unit on screen, once per unit, and
// remembers why it could not when there is nothing to edit.
func EnsureUnitEvents(ui *uiState) {
	if ui.ModTree == nil {
		ui.UnitEvtTree, ui.UnitEvtFor, ui.UnitEvtDoc = nil, "", nil
		ui.UnitEvtPath, ui.UnitEvtErr = "", "No module is open."
		return
	}
	u := CurrentUnit(ui)
	if u == nil {
		ui.UnitEvtTree, ui.UnitEvtFor, ui.UnitEvtDoc = ui.ModTree, "", nil
		ui.UnitEvtPath, ui.UnitEvtErr = "", "No unit selected."
		return
	}
	if ui.UnitEvtTree == ui.ModTree && ui.UnitEvtTried && strings.EqualFold(ui.UnitEvtFor, u.Path) {
		return
	}
	// Only another module invalidates the clipboard. Another unit does not:
	// every copy carries a snapshot of the definitions it needs, so the events
	// copied in one unit pastes into the next one. That is the whole point of
	// the snapshot, and dropping the entries here quietly emptied the buffer
	// exactly when it was about to be used.
	if ui.UnitEvtTree != nil && ui.UnitEvtTree != ui.ModTree {
		ui.UnitClips.DropKind(clipEvents)
		ui.UnitClipCur = -1
	}
	ui.UnitEvtTree = ui.ModTree
	ui.UnitEvtFor = u.Path
	ui.UnitEvtTried = true
	ui.UnitEvtDirty = false
	ui.UnitEvtFormErr = ""
	ui.UnitEvtSel = 0
	ui.UnitEvtSelSet = map[int]bool{0: true}
	ui.UnitEvtSelAnchor = 0
	ui.UnitEvtBound = ""
	ui.UnitEvtEntryBound = ""
	ui.UnitTailBound = ""
	ui.UnitActCondBound = ""
	ui.UnitEvtPath = unitEventsWhePath(ui.ModTree, CurrentUnitDetails(ui))
	if ui.UnitEvtPath == "" {
		ui.UnitEvtDoc = nil
		ui.UnitEvtErr = "This unit's model has no .whe file, so it carries no event timelines."
		return
	}
	data, err := ui.ModTree.ReadFile(ui.UnitEvtPath)
	if err != nil {
		ui.UnitEvtDoc = nil
		ui.UnitEvtErr = "Could not read " + ui.UnitEvtPath + ": " + err.Error()
		return
	}
	doc, err := whe.ParseDocument(data)
	if err != nil {
		ui.UnitEvtDoc = nil
		ui.UnitEvtErr = err.Error()
		return
	}
	ui.UnitEvtDoc = doc
	ui.UnitEvtErr = ""
	if len(doc.Timelines()) == 0 {
		ui.UnitEvtErr = "This .whe carries no animation timelines to edit."
		return
	}
	// Start on the timeline of the clip the viewport is showing, so opening
	// the section after looking at phase_in lands on phase_in.
	want := ""
	if v := UnitModelViewer(ui); v != nil && v.Anim != nil {
		want = v.Anim.Name
	}
	for _, tl := range doc.Timelines() {
		if want != "" && timelineMatchesClip(tl, want) {
			ui.UnitEvtTimeline = tl.Name
			return
		}
	}
	ui.UnitEvtTimeline = doc.Timelines()[0].Name
}

// unitMeshVisibilitySection is the list at the bottom of the Events panel:
// every mesh of the unit's model with an eye toggle, present whether a
// timeline or an action is on show. The eye is open while the mesh draws and
// closed while it is hidden, and the hidden set is the shared one every
// viewer reads.
func UnitMeshVisibilitySection(ui *uiState, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	count := 0
	if v != nil && v.Model != nil {
		count = len(modelMeshNames(v.Model))
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, fmt.Sprintf("Meshes (%d)", count))
		}),
	}
	if count == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshRows(ui, gtx) }))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	// Measure the rows unbounded, then hand the scroller at most the cap: a
	// short list keeps its own height and a long one scrolls instead of
	// taking the whole panel. The measure's operations are discarded.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	natural := UnitMeshRows(ui, measureGTX).Size.Y
	macro.Stop()
	viewport := natural
	if cap := gtx.Dp(unitMeshSectionMaxDp); viewport > cap {
		viewport = cap
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.Y = viewport
		return PanelScrollView(ui, gtx, &ui.UnitMeshScroll, func(gtx layout.Context) layout.Dimensions { return UnitMeshRows(ui, gtx) })
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// clipEventDefinition is the definition a copied event names, for
// AddTimedEvent to write where the .whe has never had it. The snapshot goes
// over whole, sound entry and all: a definition that names both a sound and an
// FX path is one event, and handing AddTimedEvent its sound path alone made a
// mixed event fail the "sound names start with sound_" guard, which is how a
// copied timeline lost its main effect. A name the copy has no snapshot of
// returns nil, which leaves the .whe's own definition to be used.
func clipEventDefinition(clip *wheEventClip, name string) *whe.FxEvent {
	if clip == nil {
		return nil
	}
	d, ok := clip.Defs[clipEventKey(name)]
	if !ok || len(d.Entries) == 0 {
		return nil
	}
	snapshot := d
	return &snapshot
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
func UnitEvtTrackModifiers(ui *uiState, gtx layout.Context) {
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
		if s := fmt.Sprintf("%q %v mods=%v", e.Name, e.State, e.Modifiers); s != ui.UnitEvtModLog {
			LogInfo("evt: modifier key %s", s)
			ui.UnitEvtModLog = s
		}
		switch e.Name {
		case key.NameCtrl:
			ui.UnitEvtCtrlHeld = e.State != key.Release
		case key.NameShift:
			ui.UnitEvtShiftHeld = e.State != key.Release
		}
	}
}

// unitEvtFieldFocused says whether a field of this section holds the caret:
// while one does, Ctrl+C and Ctrl+V belong to the text being edited, not to
// the event clipboard.
func UnitEvtFieldFocused(ui *uiState, gtx layout.Context) bool {
	if TextActive(ui) {
		return true
	}
	for i := range ui.UnitEvtEntryEds {
		if ui.UnitEvtEntryEds[i].Focused(gtx) {
			return true
		}
	}
	if ui.UnitActNameEd.Focused(gtx) {
		return true
	}
	for i := range ui.UnitActCondEds {
		if ui.UnitActCondEds[i].Focused(gtx) {
			return true
		}
	}
	for i := range ui.UnitActNameEds {
		if ui.UnitActNameEds[i].Focused(gtx) {
			return true
		}
	}
	for _, ed := range []*widget.Editor{&ui.UnitEvtNameEd, &ui.UnitEvtTimeEd, &ui.UnitEvtFxEd, &ui.UnitEvtSndEd, &ui.UnitEvtMarkerEd, &ui.UnitEvtShiftEd, &ui.UnitEvtTlFilter, &ui.UnitActFilter} {
		if ed.Focused(gtx) {
			return true
		}
	}
	return false
}
