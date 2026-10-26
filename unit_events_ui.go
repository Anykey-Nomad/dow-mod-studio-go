package main

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"image/color"
	"math"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dow-mod-studio/internal/modtree"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
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
	if name := strings.TrimSuffix(d.Entry.Name, path.Ext(d.Entry.Name)); name != "" {
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
	if ui.unitEvtTree == ui.modTree && ui.unitEvtTried && strings.EqualFold(ui.unitEvtFor, u.Path) {
		return
	}
	// Only another module invalidates the clipboard. Another unit does not:
	// every copy carries a snapshot of the definitions it needs, so the events
	// copied in one unit pastes into the next one. That is the whole point of
	// the snapshot, and dropping the entries here quietly emptied the buffer
	// exactly when it was about to be used.
	if ui.unitEvtTree != nil && ui.unitEvtTree != ui.modTree {
		ui.unitClips.DropKind(clipEvents)
		ui.unitClipCur = -1
	}
	ui.unitEvtTree = ui.modTree
	ui.unitEvtFor = u.Path
	ui.unitEvtTried = true
	ui.unitEvtDirty = false
	ui.unitEvtFormErr = ""
	ui.unitEvtSel = 0
	ui.unitEvtSelSet = map[int]bool{0: true}
	ui.unitEvtSelAnchor = 0
	ui.unitEvtBound = ""
	ui.unitEvtEntryBound = ""
	ui.unitActCondBound = ""
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

func (ui *uiState) ensureUnitEventAnimClicks(animations int) {
	if len(ui.unitEvtAnimClicks) != animations {
		ui.unitEvtAnimClicks = make([]widget.Clickable, animations)
	}
}

func (ui *uiState) ensureUnitMeshClicks(meshes int) {
	if len(ui.unitMeshClicks) != meshes {
		ui.unitMeshClicks = make([]widget.Clickable, meshes)
	}
}

// ensureUnitMeshNameClicks sizes the name half of each mesh row to match the
// list, so pressing a name always addresses the mesh that row shows.
func (ui *uiState) ensureUnitMeshNameClicks(meshes int) {
	if len(ui.unitMeshNameClicks) != meshes {
		ui.unitMeshNameClicks = make([]widget.Clickable, meshes)
	}
}

// handleUnitMeshToggles drains the eye buttons of the pinned mesh list and
// flips the shared hidden set, redrawing the viewer so the change is on screen
// at once. It runs whether or not a .whe is loaded, since the list acts on the
// model rather than the document, and the order of names is the one the panel
// lays out, so button i always belongs to mesh i.
func (ui *uiState) handleUnitMeshToggles(gtx layout.Context) {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		return
	}
	names := modelMeshNames(v.model)
	ui.ensureUnitMeshClicks(len(names))
	for i := range names {
		if i >= len(ui.unitMeshClicks) {
			break
		}
		if ui.unitMeshClicks[i].Clicked(gtx) {
			setMeshHidden(names[i], !meshHidden(names[i]))
			v.invalidate()
		}
	}
}

// unitMeshSectionMaxDp caps how tall the pinned mesh list may grow; past it
// the list scrolls rather than pushing the panel's own content off the top.
const unitMeshSectionMaxDp = 168

// eyeToggleSizeDp is the tap box of one eye button.
const eyeToggleSizeDp = 18

// unitMeshVisibilitySection is the list at the bottom of the Events panel:
// every mesh of the unit's model with an eye toggle, present whether a
// timeline or an action is on show. The eye is open while the mesh draws and
// closed while it is hidden, and the hidden set is the shared one every
// viewer reads.
// unitMeshRows is the eye-toggle list itself: one row per mesh of the
// current unit's model, each with its open or closed eye. It carries no
// header and no scroller of its own — the Events section wraps it in a
// bounded scroller, and the Classic OE panel column scrolls it with the rest
// — so both surfaces share exactly the same rows.
func (ui *uiState) unitMeshRows(gtx layout.Context) layout.Dimensions {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		return ui.meshRowsMessage(gtx, "No model is loaded for this unit.")
	}
	names := modelMeshNames(v.model)
	if len(names) == 0 {
		return ui.meshRowsMessage(gtx, "This model carries no meshes.")
	}
	ui.ensureUnitMeshClicks(len(names))
	ui.ensureUnitMeshNameClicks(len(names))
	var rc []layout.FlexChild
	for i := range names {
		i := i
		name := names[i]
		hidden := meshHidden(name)
		nameC := &ui.unitMeshNameClicks[i]
		// A second press on the name inside the window raises this mesh's
		// material editor. The eye beside it stays a plain visibility toggle.
		if nameC.Clicked(gtx) && ui.classicOEDoubleClick("mesh:"+strings.ToLower(name)) {
			ui.openUnitMeshMaterialDialog(name)
		}
		rc = append(rc, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.eyeToggle(gtx, &ui.unitMeshClicks[i], !hidden)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return nameC.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), name)
							if hidden {
								l.Color = cTextDim
							} else {
								l.Color = cText
							}
							if nameC.Hovered() {
								l.Color = rgb(0x53B9FF)
							}
							l.MaxLines = 1
							return l.Layout(gtx)
						})
					})
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rc...)
}

// meshRowsMessage is the one dim line the mesh list shows when there is
// nothing to list.
func (ui *uiState) meshRowsMessage(gtx layout.Context, msg string) layout.Dimensions {
	l := material.Label(ui.th, unit.Sp(9), msg)
	l.Color = cTextDim
	return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
}

// unitMeshVisibilitySection is the list at the bottom of the Events panel:
// every mesh of the unit's model with an eye toggle, present whether a
// timeline or an action is on show. The eye is open while the mesh draws and
// closed while it is hidden, and the hidden set is the shared one every
// viewer reads.
func (ui *uiState) unitMeshVisibilitySection(gtx layout.Context) layout.Dimensions {
	v := ui.unitModelViewer()
	count := 0
	if v != nil && v.model != nil {
		count = len(modelMeshNames(v.model))
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.th, fmt.Sprintf("Meshes (%d)", count))
		}),
	}
	if count == 0 {
		children = append(children, layout.Rigid(ui.unitMeshRows))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	// Measure the rows unbounded, then hand the scroller at most the cap: a
	// short list keeps its own height and a long one scrolls instead of
	// taking the whole panel. The measure's operations are discarded.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	natural := ui.unitMeshRows(measureGTX).Size.Y
	macro.Stop()
	viewport := natural
	if cap := gtx.Dp(unitMeshSectionMaxDp); viewport > cap {
		viewport = cap
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.Y = viewport
		return ui.panelScrollView(gtx, &ui.unitMeshScroll, ui.unitMeshRows)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// eyeToggle is one mesh's visibility button: an open eye while the mesh
// draws, a closed eye while it is hidden. The glyph is drawn as vector shapes
// rather than a font character, so it renders the same everywhere.
func (ui *uiState) eyeToggle(gtx layout.Context, btn *widget.Clickable, visible bool) layout.Dimensions {
	px := gtx.Dp(eyeToggleSizeDp)
	col := cText
	if !visible {
		col = cTextDim
	}
	if btn.Hovered() {
		col = rgb(0x53B9FF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(px, px)
		stack := clip.Rect{Max: sz}.Push(gtx.Ops)
		drawEyeGlyph(gtx.Ops, sz, visible, col)
		stack.Pop()
		return layout.Dimensions{Size: sz}
	})
}

// drawEyeGlyph paints an open or closed eye inside a sz box. Open is an
// almond outline with a pupil; closed is the lid drawn as a single downward
// curve — the pair reads as shown/hidden without needing a label.
func drawEyeGlyph(ops *op.Ops, sz image.Point, open bool, col color.NRGBA) {
	cx := float32(sz.X) / 2
	cy := float32(sz.Y) / 2
	w := float32(sz.X) * 0.40
	h := float32(sz.Y) * 0.30
	var path clip.Path
	path.Begin(ops)
	if open {
		path.MoveTo(f32.Pt(cx-w, cy))
		path.QuadTo(f32.Pt(cx, cy-h*2), f32.Pt(cx+w, cy))
		path.QuadTo(f32.Pt(cx, cy+h*2), f32.Pt(cx-w, cy))
		path.Close()
	} else {
		path.MoveTo(f32.Pt(cx-w, cy))
		path.QuadTo(f32.Pt(cx, cy+h*1.8), f32.Pt(cx+w, cy))
	}
	spec := path.End()
	paint.FillShape(ops, col, clip.Stroke{Path: spec, Width: 1.4}.Op())
	if open {
		pr := float32(sz.Y) * 0.17
		paint.FillShape(ops, col, clip.Ellipse(image.Rectangle{
			Min: image.Pt(int(cx-pr), int(cy-pr)),
			Max: image.Pt(int(cx+pr), int(cy+pr)),
		}).Op(ops))
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: input — presses, typing, focus (run before layout)
// ═══════════════════════════════════════════════════════════════════════════

// wheEventClip is the section's copy buffer: the events Copy took, each with
// the moment it fires at, and a snapshot of the DATAEVNT definitions they
// name. The snapshot is what lets a Paste into a timeline that lacks those
// definitions bring the marker and path settings along — while a name this
// .whe already defines differently is refused rather than overwritten. That is
// also why a copy outlives the unit it was taken from: it carries everything it
// needs rather than borrowing the .whe it was read from.
//
// timeline, animations and payload are set only when a whole timeline was
// copied. Paste then lands on the timeline of that name — merged into the one
// this .whe carries, or re-created from the payload — instead of on whichever
// timeline happens to be selected.

// wheActionClip is one copied action of a .whe's DATAACTS section, together
// with what it needs to run: the timelines its motions name — followed through
// the motions those timelines chain to — and the sub-actions it calls. An
// action pasted without them would point at motions the target file does not
// have, which is why the whole reachable set travels in one copy.

// clipEventKey normalizes an event name for the definition snapshot: the keys
// Events() carries are lower case and trimmed, and a timeline may store the
// name with padding bytes around it.
func clipEventKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
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
	if ui.unitActNameEd.Focused(gtx) {
		return true
	}
	for i := range ui.unitActCondEds {
		if ui.unitActCondEds[i].Focused(gtx) {
			return true
		}
	}
	for i := range ui.unitActNameEds {
		if ui.unitActNameEds[i].Focused(gtx) {
			return true
		}
	}
	for _, ed := range []*widget.Editor{&ui.unitEvtNameEd, &ui.unitEvtTimeEd, &ui.unitEvtFxEd, &ui.unitEvtSndEd, &ui.unitEvtMarkerEd, &ui.unitEvtShiftEd} {
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
	ui.unitActNameEd.Blur(gtx)
	for i := range ui.unitActCondEds {
		ui.unitActCondEds[i].Blur(gtx)
	}
	for i := range ui.unitActNameEds {
		ui.unitActNameEds[i].Blur(gtx)
	}
	for _, ed := range []*widget.Editor{&ui.unitEvtNameEd, &ui.unitEvtTimeEd, &ui.unitEvtFxEd, &ui.unitEvtSndEd, &ui.unitEvtMarkerEd, &ui.unitEvtShiftEd} {
		ed.Blur(gtx)
	}
}

// handleUnitEventsInput owns the presses and typing of the Events section. It
// runs on every frame while the section is on screen, ahead of the layout that
// draws what it decided.
func (ui *uiState) handleUnitEventsInput(gtx layout.Context) {
	if ui.currentUnitSection().label != unitSectionEvents && !ui.classicOEDialogOpen() {
		// Leaving the section silences whatever it was playing: a timeline
		// that keeps sounding behind another section is a leak, not a feature.
		// The Classic OE double-click dialog is the exception: while it is
		// up, the editors it embeds are the section, wherever the panel sits.
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
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtNameEd, evtPickName, false)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtTimeEd, evtPickNone, true)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtFxEd, evtPickFX, false)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtSndEd, evtPickSound, false)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtMarkerEd, evtPickMarker, false)
	ui.unitEvtUpdateShiftEditor(gtx)
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
			ui.unitEvtFocusAction = false
			ui.selectUnitEventTimeline(timelines[i].Name)
			tl = ui.selectedUnitEventTimeline()
			events = 0
			if tl != nil {
				events = len(tl.Events)
			}
			ui.ensureUnitEventClicks(len(timelines), events)
		}
	}
	// The action list under the timelines picks which action's conditions the
	// Events column draws beside it, and moves that column onto the actions —
	// the panel shows one or the other, never both.
	acts := ui.unitEvtDoc.Actions()
	ui.ensureUnitEvtActionClicks(len(acts))
	for i := range ui.unitEvtActionClicks {
		if i < len(acts) && ui.unitEvtActionClicks[i].Clicked(gtx) {
			ui.unitEvtBlurFields(gtx)
			// The panel leaves the timeline behind completely, and a sound
			// it was auditioning belongs to that timeline.
			if !ui.unitEvtFocusAction {
				ui.evtPlayer.stopAll()
			}
			ui.unitEvtFocusAction = true
			ui.unitEvtActionSel = i
			// Selecting an action also plays it: every clip its graph
			// resolves to, combined in the viewport beside the panel.
			ui.activateUnitAction(acts[i].Name)
		}
	}
	// While an action is the panel on screen, its conditions are the fields
	// being edited: their typing, the guard chips and the commit below.
	if ui.unitEvtFocusAction {
		ui.unitActConditionsInput(gtx)
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
	// The animation list's per-row Remove buttons each drop one blend entry
	// from the selected timeline. They are only laid out for a selected
	// timeline, so with none (or while an action is shown) no press can land.
	if tl != nil {
		ui.ensureUnitEventAnimClicks(len(tl.Animations))
		for i := range ui.unitEvtAnimClicks {
			if i < len(tl.Animations) && ui.unitEvtAnimClicks[i].Clicked(gtx) {
				ui.removeUnitTimelineAnimation(i)
				return
			}
		}
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
	if ui.unitEvtShift.Clicked(gtx) {
		ui.shiftSelectedUnitEventTimes()
		return
	}
	if ui.unitEvtCopyAct.Clicked(gtx) {
		ui.copySelectedUnitAction()
		return
	}
	if ui.unitEvtPasteAct.Clicked(gtx) {
		ui.pasteUnitAction()
		return
	}
	if ui.unitEvtCopy.Clicked(gtx) {
		ui.copySelectedUnitEvents()
		return
	}
	if ui.unitEvtCopyTl.Clicked(gtx) {
		ui.copySelectedUnitTimeline()
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
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtNameEd, evtPickName, false)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtTimeEd, evtPickNone, true)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtFxEd, evtPickFX, false)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtSndEd, evtPickSound, false)
	ui.unitEvtUpdateEditor(gtx, &ui.unitEvtMarkerEd, evtPickMarker, false)
	ui.unitEvtUpdateShiftEditor(gtx)
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
func (ui *uiState) unitEvtUpdateEditor(gtx layout.Context, ed *widget.Editor, pick evtPickTarget, batchTime bool) {
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
			if batchTime {
				ui.applySelectedEventForm()
			} else {
				ui.applySelectedEventFormSingle()
			}
			ed.Blur(gtx)
		}
	}
}

// unitEvtUpdateShiftEditor owns the batch shift field: a click takes the
// caret, Enter shifts the selection and consumes the number. It is kept out of
// unitEvtUpdateEditor because submitting it is not an apply of the form — the
// name, path and marker fields have nothing to do with a shift.
func (ui *uiState) unitEvtUpdateShiftEditor(gtx layout.Context) {
	ed := &ui.unitEvtShiftEd
	ed.SingleLine = true
	ed.Submit = true
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.shiftSelectedUnitEventTimes()
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

// removeUnitTimelineAnimation drops one entry from the selected timeline's
// animation blend list. It edits the in-memory document only — Save .whe
// writes it into the mod, matching how every other timeline edit behaves.
func (ui *uiState) removeUnitTimelineAnimation(index int) {
	doc := ui.unitEvtDoc
	tl := ui.selectedUnitEventTimeline()
	if doc == nil || tl == nil {
		ui.unitEvtFormErr = "Select a timeline's animation to remove."
		ui.lastFormAction = ""
		return
	}
	if index < 0 || index >= len(tl.Animations) {
		ui.unitEvtFormErr = "Animation selection is no longer valid."
		ui.lastFormAction = ""
		return
	}
	if err := doc.RemoveTimelineAnimation(ui.unitEvtTimeline, index); err != nil {
		ui.unitEvtFormErr = err.Error()
		ui.lastFormAction = ""
		return
	}
	ui.unitEvtFormErr = ""
	ui.unitEvtDirty = true
	ui.lastFormAction = "remove"
	ui.reloadUnitModelFx()
	ui.statusText = fmt.Sprintf("Removed an animation from %s — save to write it into the mod", ui.unitEvtTimeline)
}

// stepUnitClip moves the history position through the entries that fit the
// events field, from the entry Paste would take now.
func (ui *uiState) stepUnitClip(dir int) {
	_, cur, ok := ui.unitClips.Pick(clipEvents, ui.unitClipCur)
	if !ok {
		return
	}
	if next := ui.unitClips.Step(clipEvents, cur, dir); next >= 0 {
		ui.unitClipCur = next
	}
}

// copySelectedUnitEvents puts the selected rows and a snapshot of their
// definitions into the shared buffer, from where Paste carries them into any
// timeline — of this unit or of the next one, since the snapshot travels with
// the copy rather than pointing back at the .whe it was read from.
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
	clip := &wheEventClip{Defs: map[string]whe.FxEvent{}}
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		ev := tl.Events[i]
		clip.Events = append(clip.Events, ev)
		if d, ok := defs[clipEventKey(ev.Name)]; ok {
			clip.Defs[clipEventKey(ev.Name)] = d
		}
	}
	if len(clip.Events) == 0 {
		ui.statusText = "Nothing is selected to copy"
		return
	}
	ui.unitClips.Push(clipEntry{
		Kind:   clipEvents,
		Label:  fmt.Sprintf("%d event(s) from %s", len(clip.Events), ui.unitEvtTimeline),
		At:     time.Now(),
		Events: clip,
	})
	// The copy just made is the one Paste takes until the history is stepped.
	ui.unitClipCur = len(ui.unitClips.Entries) - 1
	ui.statusText = fmt.Sprintf("Copied %d event(s) — Paste into any timeline", len(clip.Events))
}

// copySelectedUnitTimeline puts the whole selected timeline into the shared
// buffer: its events, the clips it blends and the definitions those events
// name. Copying a few rows is a paste of those rows; this is a paste of the
// timeline itself, so it lands on the timeline of its own name — in this unit
// or in the next one, where it is merged in or created from the copied bytes.
func (ui *uiState) copySelectedUnitTimeline() {
	doc := ui.unitEvtDoc
	tl := ui.selectedUnitEventTimeline()
	if doc == nil || tl == nil {
		ui.statusText = "Select a timeline first"
		return
	}
	clip, ok := timelineClipFrom(doc, tl.Name)
	if !ok {
		ui.statusText = "This timeline could not be read back from the .whe"
		return
	}
	ui.unitClips.Push(clipEntry{
		Kind:   clipEvents,
		Label:  fmt.Sprintf("timeline %s (%d event(s))", clip.Timeline, len(clip.Events)),
		At:     time.Now(),
		Events: clip,
	})
	ui.unitClipCur = len(ui.unitClips.Entries) - 1
	ui.statusText = fmt.Sprintf("Copied timeline %s (%d event(s), %d animation(s)) — Paste in any unit",
		clip.Timeline, len(clip.Events), len(clip.Animations))
}

// timelineClipFrom reads one timeline of doc into a clipboard copy: its raw
// DATAMTON payload, the clips it blends, its events and the definitions behind
// them. Everything a paste needs travels inside the copy, which is what lets a
// timeline read here be written into another unit's file.
func timelineClipFrom(doc *whe.Document, name string) (*wheEventClip, bool) {
	tl, ok := timelineOf(doc, name)
	if !ok {
		return nil, false
	}
	payload, ok := doc.TimelinePayload(tl.Name)
	if !ok {
		return nil, false
	}
	clip := &wheEventClip{
		Timeline:   tl.Name,
		Animations: append([]string(nil), tl.Animations...),
		Payload:    payload,
		Defs:       map[string]whe.FxEvent{},
	}
	defs := doc.Events()
	for _, ev := range tl.Events {
		clip.Events = append(clip.Events, ev)
		if d, ok := defs[clipEventKey(ev.Name)]; ok {
			clip.Defs[clipEventKey(ev.Name)] = d
		}
	}
	return clip, true
}

// timelineOf finds one timeline of doc by name.
func timelineOf(doc *whe.Document, name string) (whe.FxTimeline, bool) {
	if doc == nil {
		return whe.FxTimeline{}, false
	}
	for _, tl := range doc.Timelines() {
		if strings.EqualFold(strings.TrimSpace(tl.Name), strings.TrimSpace(name)) {
			return tl, true
		}
	}
	return whe.FxTimeline{}, false
}

// timelinePaste is what writing one copied timeline into a .whe did: whether the
// file had to grow it, how many rows landed, and what was left where it was.
type timelinePaste struct {
	name      string
	created   bool
	events    int
	present   int
	anims     int
	skipped   int
	firstSkip string
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
	res.name = strings.TrimSpace(clip.Timeline)
	if _, exists := doc.TimelinePayload(res.name); !exists {
		if err := doc.AddTimeline(res.name, clip.Payload); err != nil {
			return res, err
		}
		res.created = true
	}
	for _, anim := range clip.Animations {
		wrote, err := doc.AddTimelineAnimation(res.name, anim)
		if err != nil {
			return res, err
		}
		if wrote {
			res.anims++
		}
	}
	// The definitions this .whe already carries, kept up to date as the paste
	// writes more of them; only the created path needs the set. An existing
	// timeline is read by slot, so a paste into one that half carries the copy
	// cannot double every row.
	known := map[string]bool{}
	if res.created {
		for name := range doc.Events() {
			known[name] = true
		}
	} else if tl, ok := timelineOf(doc, res.name); ok {
		for _, ev := range tl.Events {
			known[fxEventSlot(ev)] = true
		}
	}
	for _, ev := range clip.Events {
		if res.created {
			// The timeline was written from the copied payload, so its events
			// are already listed; only the definitions behind them still have
			// to come over. One this .whe already has is left exactly as it is.
			key := clipEventKey(ev.Name)
			if d, have := clip.Defs[key]; have && !known[key] {
				if err := doc.AddEventDefinition(d); err != nil {
					res.skipped++
					if res.firstSkip == "" {
						res.firstSkip = err.Error()
					}
					continue
				}
				known[key] = true
			}
			res.events++
			continue
		}
		if known[fxEventSlot(ev)] {
			res.present++
			continue
		}
		if err := doc.AddTimedEvent(res.name, ev, clipEventDefinition(clip, ev.Name), ""); err != nil {
			res.skipped++
			if res.firstSkip == "" {
				res.firstSkip = err.Error()
			}
			continue
		}
		known[fxEventSlot(ev)] = true
		res.events++
	}
	return res, nil
}

// timelinePasteMessage is how writing a timeline back reads in the status line.
func timelinePasteMessage(res timelinePaste) string {
	msg := fmt.Sprintf("Pasted %d event(s) into %s", res.events, res.name)
	if res.created {
		msg = fmt.Sprintf("Created timeline %s with %d event(s)", res.name, res.events)
	}
	if res.anims > 0 {
		msg += fmt.Sprintf(", %d animation(s)", res.anims)
	}
	if res.present > 0 {
		msg += fmt.Sprintf(", %d already there", res.present)
	}
	if res.skipped > 0 {
		msg += fmt.Sprintf(", %d skipped: %s", res.skipped, res.firstSkip)
	}
	return msg + " — Save .whe to write it into the mod"
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
func (ui *uiState) pasteUnitEvents() {
	entry, _, ok := ui.unitClips.Pick(clipEvents, ui.unitClipCur)
	if !ok || entry.Events == nil || len(entry.Events.Events) == 0 {
		ui.statusText = "The event clipboard is empty — Copy first"
		return
	}
	doc := ui.unitEvtDoc
	if doc == nil {
		ui.statusText = "No .whe is loaded — select a unit whose model has one first"
		return
	}
	clip := entry.Events
	if clip.Timeline != "" {
		res, err := pasteTimelineClip(doc, clip)
		if err != nil {
			ui.statusText = "Paste failed: " + err.Error()
			return
		}
		if res.events == 0 && res.present == 0 {
			if res.firstSkip != "" {
				ui.statusText = "Paste failed: " + res.firstSkip
			} else {
				ui.statusText = fmt.Sprintf("Nothing to paste — %s carries no events", res.name)
			}
			return
		}
		ui.unitEvtDirty = true
		ui.unitEvtBound = ""
		// A .whe read as carrying no timelines may have just gained one.
		ui.unitEvtErr = ""
		ui.unitEvtTimeline = res.name
		ui.unitEvtSel = 0
		ui.unitEvtSelSet = map[int]bool{0: true}
		ui.unitEvtSelAnchor = 0
		ui.reloadUnitModelFx()
		ui.statusText = timelinePasteMessage(res)
		return
	}
	target := ui.unitEvtTimeline
	if target == "" {
		ui.statusText = "Select a timeline first"
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
			ui.statusText = fmt.Sprintf("Nothing to paste — %d event(s) are already in %s", present, target)
			return
		}
		ui.statusText = "Paste failed: " + firstSkip
		return
	}
	ui.unitEvtDirty = true
	ui.unitEvtBound = ""
	// A .whe read as carrying no timelines may have just gained one.
	ui.unitEvtErr = ""
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
	msg := fmt.Sprintf("Pasted %d event(s) into %s", added, target)
	if present > 0 {
		msg += fmt.Sprintf(", %d already there", present)
	}
	if skipped > 0 {
		msg += fmt.Sprintf(", %d skipped: %s", skipped, firstSkip)
	}
	ui.statusText = msg
}

// fxEventSlot is what makes two rows of a timeline the same event: the name
// the game fires and the moment it fires at. The comparison is case
// insensitive for the name and exact for the time, the way the .whe stores
// them.
func fxEventSlot(ev whe.FxTimelineEvent) string {
	return clipEventKey(ev.Name) + "|" + strconv.FormatFloat(ev.Time, 'g', -1, 64)
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: apply — commit the form's edited fields to the selected event
// ═══════════════════════════════════════════════════════════════════════════

// applySelectedEventForm is the panel's Apply and Set: with a group selected
// it writes the typed moment into every selected row, and with a single row
// selected it writes the whole form — time, path and marker — into that row.
func (ui *uiState) applySelectedEventForm() {
	ui.applyEventForm(true)
}

// applySelectedEventFormSingle is the path, marker and name fields' own Enter:
// those describe the one row the form shows, so they never fan out over the
// selection.
func (ui *uiState) applySelectedEventFormSingle() {
	ui.applyEventForm(false)
}

// applyEventForm writes the form's typed fields into the selected event of the
// in-memory document: the moment it fires at on the timeline and the path and
// marker its definition carries. Nothing touches the file — Save .whe does
// that — and the viewport re-resolves from the document, so the edited event
// shows on the model before it is written out. The event's name is its identity
// across the file, so a changed name is refused rather than half-renamed.
func (ui *uiState) applyEventForm(allowBatchTime bool) {
	if ui.unitEvtDoc == nil {
		ui.unitEvtFormErr = "No .whe is loaded."
		return
	}
	tl := ui.selectedUnitEventTimeline()
	if tl == nil || ui.unitEvtSel < 0 || ui.unitEvtSel >= len(tl.Events) {
		ui.unitEvtFormErr = "Select an event to apply the fields to."
		return
	}
	// A group selection is the batch form of this button. The moment an event
	// fires at is the one field that means the same thing for every row — a
	// name is each row's own identity, and a path and marker belong to the
	// definition they came from — so a multi-row Apply writes the typed time
	// to all selected rows and leaves the rest of the form to the one row it
	// shows.
	if indices := ui.unitEvtSelectedIndices(); allowBatchTime && len(indices) > 1 {
		at, err := unitEventTime(ui.unitEvtTimeEd.Text())
		if err != nil {
			ui.unitEvtFormErr = err.Error()
			return
		}
		ui.applyUnitEventTimes(indices, at)
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
	ui.unitActCondBound = ""
	ui.reloadUnitModelFx()
	ui.statusText = "Applied to " + ev.Name + " — Save .whe to write it into the mod"
}

// unitEventShift parses the batch shift field: seconds added to every selected
// row, negative to move them earlier. A typed plus sign is accepted the way a
// person writes a delta.
func unitEventShift(text string) (float64, error) {
	s := strings.TrimSpace(text)
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, fmt.Errorf("type the seconds to add, e.g. 0.1 or -0.1")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
		return 0, fmt.Errorf("the shift must be a number of seconds, e.g. 0.1 or -0.1")
	}
	return v, nil
}

// unitEventShiftLabel reads a shift back the way it was typed, with its sign.
func unitEventShiftLabel(d float64) string {
	if d < 0 {
		return "-" + strconv.FormatFloat(-d, 'f', -1, 64) + "s"
	}
	return "+" + strconv.FormatFloat(d, 'f', -1, 64) + "s"
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
func (ui *uiState) shiftSelectedUnitEventTimes() {
	if ui.unitEvtDoc == nil {
		ui.unitEvtFormErr = "No .whe is loaded."
		return
	}
	delta, err := unitEventShift(ui.unitEvtShiftEd.Text())
	if err != nil {
		ui.unitEvtFormErr = err.Error()
		return
	}
	tl := ui.selectedUnitEventTimeline()
	indices := ui.unitEvtSelectedIndices()
	if len(indices) == 0 && ui.unitEvtSel >= 0 {
		// A single row is a selection of one: the shift works without the
		// Ctrl+click ritual the batch tools otherwise need.
		indices = []int{ui.unitEvtSel}
	}
	if tl == nil || len(indices) == 0 {
		ui.unitEvtFormErr = "Select the events to shift."
		return
	}
	if delta == 0 {
		ui.unitEvtFormErr = ""
		ui.statusText = "A shift of 0s moves nothing"
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
		if err := ui.unitEvtDoc.SetTimedEventTime(ui.unitEvtTimeline, i, at); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		moved++
	}
	if moved == 0 {
		if firstErr != "" {
			ui.unitEvtFormErr = firstErr
			return
		}
		ui.unitEvtFormErr = ""
		if sentinel > 0 {
			ui.statusText = fmt.Sprintf("Nothing shifted — %d event(s) fire on enter/leave, which have no moment to move", sentinel)
			return
		}
		ui.statusText = "Nothing shifted"
		return
	}
	ui.unitEvtFormErr = ""
	ui.unitEvtDirty = true
	// The form refills from the row it shows, whose moment has just moved.
	ui.unitEvtBound = ""
	ui.unitEvtShiftEd.SetText("")
	ui.reloadUnitModelFx()
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
	ui.statusText = msg + " — Save .whe to write it into the mod"
}

// applyUnitEventTimes writes one moment into every selected row of the
// timeline — the batch edit behind Apply while a group is selected. Events
// that already fire at that moment are counted rather than rewritten, and a
// row the document refuses is reported without stopping the rest: one damaged
// row must not leave the group half-moved with no word about it.
func (ui *uiState) applyUnitEventTimes(indices []int, at float64) {
	doc := ui.unitEvtDoc
	tl := ui.selectedUnitEventTimeline()
	if doc == nil || tl == nil {
		ui.unitEvtFormErr = "Select an event to apply the fields to."
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
		if err := doc.SetTimedEventTime(ui.unitEvtTimeline, i, at); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		moved++
	}
	if moved == 0 {
		if firstErr != "" {
			ui.unitEvtFormErr = firstErr
			return
		}
		ui.unitEvtFormErr = ""
		ui.statusText = fmt.Sprintf("All %d selected event(s) already start at %s", same, unitEventTimeLabel(at))
		return
	}
	ui.unitEvtFormErr = ""
	ui.unitEvtDirty = true
	// The time field refills from the row the form shows, which has just moved.
	ui.unitEvtBound = ""
	ui.reloadUnitModelFx()
	msg := fmt.Sprintf("Set %s on %d event(s)", unitEventTimeLabel(at), moved)
	if same > 0 {
		msg += fmt.Sprintf(", %d already there", same)
	}
	if firstErr != "" {
		msg += ", some refused: " + firstErr
	}
	ui.statusText = msg + " — Save .whe to write it into the mod"
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
	ui.unitActCondBound = ""
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
// the .whe, the selected one highlighted, and which clips each drives. The
// .whe's actions are drawn under that list — they are file-wide and their
// motion lines name these very timelines, so they belong beside the list of
// them rather than inside the events of whichever timeline is picked.
func (ui *uiState) layoutUnitEventTimelines(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitEvents()
	sec := ui.currentUnitSection()
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.middleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, ui.layoutUnitEventTimelineList),
		}
		if ui.unitEvtDoc != nil {
			acts := ui.unitEvtDoc.Actions()
			// The block also shows for a .whe that has no actions of its own
			// while one is on the clipboard: a copied action is the only way to
			// give such a unit its first one.
			_, _, hasClip := ui.unitClips.Pick(clipActions, ui.unitClipCur)
			if len(acts) > 0 || hasClip {
				children = append(children,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.layoutUnitEvtActions(gtx, acts)
					}),
				)
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// layoutUnitEvtActions is the .whe's action list under the timelines list: the
// header, then one row per DATAACTS action, the picked one highlighted. What
// each action guards its choices with is not here — those conditions belong to
// one action at a time and are drawn in the Events column beside this list.
//
// The list is laid out after the timelines, so it is a rigid child — it claims
// its height first and the list keeps what is left. A .whe with many actions is
// taller than that room, so the block is measured at its natural height and
// capped at a share of the column when it does not fit: long action lists scroll
// inside their own region rather than squeezing the animations off the column.
func (ui *uiState) layoutUnitEvtActions(gtx layout.Context, acts []whe.FxAction) layout.Dimensions {
	ui.ensureUnitEvtActionClicks(len(acts))
	ui.selectedUnitEvtAction(acts)
	content := func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionHeader(gtx, ui.th, "Actions")
				})
			}),
			// An action moves between units the way a timeline does: one is
			// copied whole here and pastes into another unit's list.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitEvtCopyAct, "Copy action", len(acts) == 0)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							_, _, has := ui.unitClips.Pick(clipActions, ui.unitClipCur)
							return ui.smallButtonDisabled(gtx, &ui.unitEvtPasteAct, "Paste action", !has)
						}),
					)
				})
			}),
		}
		for i := range acts {
			i := i
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitEvtActionRow(gtx, i, acts[i])
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	// Measure it the way panelScrollView measures its own content: against an
	// unbounded height with input off, so a block that fills the room it is
	// handed cannot pass the room off as its own height.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	natural := content(measureGTX).Size.Y
	if natural >= panelScrollMeasureHeight {
		measureGTX.Constraints.Max.Y = maxInt(1, gtx.Constraints.Max.Y)
		natural = content(measureGTX).Size.Y
	}
	macro.Stop()
	height := natural
	// Two fifths of the column is the most the block may claim, which leaves
	// the timelines list with the majority however many actions there are.
	if cap := gtx.Constraints.Max.Y * 2 / 5; cap > 0 && height > cap {
		height = cap
	}
	agtx := gtx
	agtx.Constraints.Min.Y = height
	agtx.Constraints.Max.Y = height
	return ui.panelScrollView(agtx, &ui.unitEvtActionsScroll, content)
}

// selectedUnitEvtAction is the action whose conditions the Events column draws:
// the one picked in the list under the timelines, kept inside that list when
// the file changes under it.
func (ui *uiState) selectedUnitEvtAction(acts []whe.FxAction) *whe.FxAction {
	if len(acts) == 0 {
		return nil
	}
	if ui.unitEvtActionSel < 0 || ui.unitEvtActionSel >= len(acts) {
		ui.unitEvtActionSel = 0
	}
	return &acts[ui.unitEvtActionSel]
}

func (ui *uiState) ensureUnitEvtActionClicks(n int) {
	if len(ui.unitEvtActionClicks) != n {
		ui.unitEvtActionClicks = make([]widget.Clickable, n)
	}
}

// layoutUnitEvtActionRow is one action of the list: its name and how many
// choices it carries, the picked one highlighted. Pressing it shows that
// action's conditions in the Events column.
func (ui *uiState) layoutUnitEvtActionRow(gtx layout.Context, i int, a whe.FxAction) layout.Dimensions {
	if i < 0 || i >= len(ui.unitEvtActionClicks) {
		return layout.Dimensions{}
	}
	c := &ui.unitEvtActionClicks[i]
	selected := i == ui.unitEvtActionSel
	bg := cPanelBg
	switch {
	case selected:
		bg = cSelectBg
	case c.Hovered():
		bg = cMenuHover
	}
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
							l := material.Label(ui.th, unit.Sp(10), unitViewportRowText(a.Name))
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							note := fmt.Sprintf("%d choices", len(a.Motions)+len(a.Subactions))
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

// copySelectedUnitAction puts the picked action and everything it needs to run
// into the shared buffer: the action itself, the timelines its motions name —
// followed through the motions those timelines chain to — and the sub-actions
// it calls. A unit's idle / build / death chain can then be set up once and
// pasted onto another unit in one go, instead of reassembling it from the
// timeline list by hand.
func (ui *uiState) copySelectedUnitAction() {
	doc := ui.unitEvtDoc
	if doc == nil {
		ui.statusText = "No .whe is loaded"
		return
	}
	acts := doc.Actions()
	a := ui.selectedUnitEvtAction(acts)
	if a == nil {
		ui.statusText = "Select an action to copy"
		return
	}
	clip := &wheActionClip{Action: *a}
	for _, name := range actionMotionNames(doc, *a) {
		if tl, ok := timelineClipFrom(doc, name); ok {
			clip.Timelines = append(clip.Timelines, *tl)
		}
	}
	clip.Subacts = actionSubactions(doc, *a)
	ui.unitClips.Push(clipEntry{
		Kind: clipActions,
		Label: fmt.Sprintf("action %s (%d choice(s), %d timeline(s), %d sub-action(s))",
			a.Name, len(unitActLinks(*a)), len(clip.Timelines), len(clip.Subacts)),
		At:      time.Now(),
		Actions: clip,
	})
	ui.unitClipCur = len(ui.unitClips.Entries) - 1
	ui.statusText = fmt.Sprintf("Copied action %s with %d timeline(s) and %d sub-action(s) — Paste in any unit",
		a.Name, len(clip.Timelines), len(clip.Subacts))
}

// actionMotionNames lists every timeline the action can reach: the motions its
// own choices name first, then the motions those timelines chain to, and so on
// through the chain — a motion timeline whose payload names another motion is
// only half copied without the one it points at. Names that this file has no
// timeline for are dropped, since there is nothing to carry.
func actionMotionNames(doc *whe.Document, a whe.FxAction) []string {
	seen := map[string]bool{}
	var out []string
	var visit func(name string)
	visit = func(name string) {
		key := clipEventKey(name)
		if key == "" || seen[key] {
			return
		}
		tl, ok := timelineOf(doc, key)
		if !ok {
			return
		}
		seen[key] = true
		out = append(out, tl.Name)
		for _, motion := range tl.Motions {
			visit(motion)
		}
		for _, motion := range tl.RandomMotions {
			visit(motion.Name)
		}
	}
	for _, link := range a.Motions {
		visit(link.Name)
	}
	return out
}

// actionSubactions lists the other actions of the file the action calls,
// followed through their own sub-action links. They travel with the copy for
// the same reason the timelines do: an action that calls a sub-action the
// target does not have is a broken chain.
func actionSubactions(doc *whe.Document, a whe.FxAction) []whe.FxAction {
	byName := map[string]whe.FxAction{}
	for _, have := range doc.Actions() {
		byName[clipEventKey(have.Name)] = have
	}
	seen := map[string]bool{clipEventKey(a.Name): true}
	var out []whe.FxAction
	var visit func(name string)
	visit = func(name string) {
		key := clipEventKey(name)
		if key == "" || seen[key] {
			return
		}
		sub, ok := byName[key]
		if !ok {
			return
		}
		seen[key] = true
		out = append(out, sub)
		for _, link := range sub.Subactions {
			visit(link.Name)
		}
	}
	for _, link := range a.Subactions {
		visit(link.Name)
	}
	return out
}

// pasteUnitAction writes the copied action, its timelines and its sub-actions
// into this unit's .whe. The timelines go in first — created when this file has
// never had them, merged into the ones it has — so the action's motions resolve
// the moment it lands, then the sub-actions, and the action itself last.
//
// An action of the same name here is refused before anything is written rather
// than merged: an action's choices are an if / else chain in file order, and
// splicing one into another would change what this unit plays.
func (ui *uiState) pasteUnitAction() {
	entry, _, ok := ui.unitClips.Pick(clipActions, ui.unitClipCur)
	if !ok || entry.Actions == nil {
		ui.statusText = "The action clipboard is empty — Copy an action first"
		return
	}
	doc := ui.unitEvtDoc
	if doc == nil {
		ui.statusText = "No .whe is loaded — select a unit whose model has one first"
		return
	}
	clip := entry.Actions
	if actionIndexByName(doc, clip.Action.Name) >= 0 {
		ui.statusText = fmt.Sprintf("action %s already exists in this .whe — reconcile it by hand instead of pasting over it",
			clip.Action.Name)
		return
	}
	createdTl, mergedTl, events, anims := 0, 0, 0, 0
	firstErr := ""
	for i := range clip.Timelines {
		res, err := pasteTimelineClip(doc, &clip.Timelines[i])
		if err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		if res.created {
			createdTl++
		} else {
			mergedTl++
		}
		events += res.events
		anims += res.anims
	}
	addedSubs, presentSubs := 0, 0
	for _, sub := range clip.Subacts {
		if actionIndexByName(doc, sub.Name) >= 0 {
			// The target has its own version of that action: keep it, and let
			// the reader settle the two by hand.
			presentSubs++
			continue
		}
		if err := doc.AddAction(sub); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		addedSubs++
	}
	if err := doc.AddAction(clip.Action); err != nil {
		// The timelines and sub-actions may already have landed, and Save .whe
		// has to know the document no longer matches the file.
		if createdTl+mergedTl+addedSubs > 0 {
			ui.unitEvtDirty = true
			ui.unitActCondBound = ""
		}
		ui.statusText = "Paste failed: " + err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.unitActCondBound = ""
	// The pasted action is what the list should show as picked, so the panel
	// below it describes the thing that just landed.
	if i := actionIndexByName(doc, clip.Action.Name); i >= 0 {
		ui.unitEvtActionSel = i
	}
	ui.reloadUnitModelFx()
	msg := fmt.Sprintf("Pasted action %s", clip.Action.Name)
	if createdTl+mergedTl > 0 {
		msg += fmt.Sprintf(" with %d timeline(s) (%d new, %d merged, %d event(s)", createdTl+mergedTl, createdTl, mergedTl, events)
		if anims > 0 {
			msg += fmt.Sprintf(", %d animation(s)", anims)
		}
		msg += ")"
	}
	if addedSubs > 0 {
		msg += fmt.Sprintf(" and %d sub-action(s)", addedSubs)
	}
	if presentSubs > 0 {
		msg += fmt.Sprintf(", %d sub-action(s) already here", presentSubs)
	}
	if firstErr != "" {
		msg += ", some skipped: " + firstErr
	}
	ui.statusText = msg + " — Save .whe to write it in"
}

// actionIndexByName is the position of an action in the file's DATAACTS list,
// or -1 when the file has no action of that name.
func actionIndexByName(doc *whe.Document, name string) int {
	if doc == nil {
		return -1
	}
	key := clipEventKey(name)
	for i, a := range doc.Actions() {
		if clipEventKey(a.Name) == key {
			return i
		}
	}
	return -1
}

// unitActLinks is one action's choices in file order: the motions it plays,
// then the actions it calls — the order the condition fields are sized and
// bound in.
func unitActLinks(a whe.FxAction) []whe.FxActionLink {
	links := make([]whe.FxActionLink, 0, len(a.Motions)+len(a.Subactions))
	links = append(links, a.Motions...)
	links = append(links, a.Subactions...)
	return links
}

// unitActLinkAt is the editable copy of one of those choices: an index into
// the motions, then into the sub-actions.
func unitActLinkAt(a *whe.FxAction, i int) *whe.FxActionLink {
	if i < 0 {
		return nil
	}
	if i < len(a.Motions) {
		return &a.Motions[i]
	}
	if i-len(a.Motions) < len(a.Subactions) {
		return &a.Subactions[i-len(a.Motions)]
	}
	return nil
}

// unitActGuardLabel is what a guard chip reads for a step of the chain.
func unitActGuardLabel(cmp int32) string {
	switch cmp {
	case whe.FxCompareIf:
		return "if"
	case whe.FxCompareElseIf:
		return "else if"
	case whe.FxCompareElse:
		return "else"
	default:
		return "always"
	}
}

// ensureUnitActConditions sizes the condition fields to the picked action's
// choices and refills them whenever the pick or the file changes under them,
// so what the fields show is what the document holds.
func (ui *uiState) ensureUnitActConditions() {
	if ui.unitEvtDoc == nil {
		ui.unitActCondEds = nil
		ui.unitActNameEds = nil
		ui.unitActCondStaged = nil
		ui.unitActGuardClicks = nil
		ui.unitActCondBound = ""
		return
	}
	a := ui.selectedUnitEvtAction(ui.unitEvtDoc.Actions())
	if a == nil {
		ui.unitActCondEds = nil
		ui.unitActNameEds = nil
		ui.unitActCondStaged = nil
		ui.unitActGuardClicks = nil
		ui.unitActCondBound = ""
		return
	}
	links := unitActLinks(*a)
	if len(ui.unitActCondEds) != len(links) || len(ui.unitActGuardClicks) != len(links) || len(ui.unitActNameEds) != len(links) {
		ui.unitActCondEds = make([]widget.Editor, len(links))
		ui.unitActNameEds = make([]widget.Editor, len(links))
		ui.unitActCondStaged = make([]int32, len(links))
		ui.unitActGuardClicks = make([]widget.Clickable, len(links))
		ui.unitActCondBound = ""
	}
	bound := strconv.Itoa(ui.unitEvtActionSel) + "|" + a.Name
	for _, ln := range links {
		bound += "|" + ln.Name
	}
	if ui.unitActCondBound == bound {
		if ui.unitActSel >= len(links) {
			ui.unitActSel = 0
		}
		return
	}
	ui.unitActCondBound = bound
	if ui.unitActSel >= len(links) {
		ui.unitActSel = 0
	}
	for i, ln := range links {
		ui.unitActCondStaged[i] = ln.Compare
		ui.unitActCondEds[i].SetText(ln.Condition)
		ui.unitActNameEds[i].SetText(ln.Name)
	}
}

// unitActConditionsInput runs the presses and typing of the action panel: the
// guard chips cycle a choice's if / else if / else / always step, the
// condition and name fields take the typing, Apply or Enter commits them,
// and the chain tools add, move and delete choices around the caret.
func (ui *uiState) unitActConditionsInput(gtx layout.Context) {
	ui.ensureUnitActConditions()
	ui.unitActUpdateNameEditor(gtx)
	if ui.unitActRename.Clicked(gtx) {
		ui.renameSelectedUnitAction()
	}
	for i := range ui.unitActGuardClicks {
		if i >= len(ui.unitActCondStaged) || !ui.unitActGuardClicks[i].Clicked(gtx) {
			continue
		}
		ui.unitActSel = i
		cmp := ui.unitActCondStaged[i]
		if cmp < whe.FxCompareIf || cmp >= whe.FxCompareNone {
			// A press starts the chain over from if — including for a compare
			// value outside the four the format defines.
			ui.unitActCondStaged[i] = whe.FxCompareIf
			continue
		}
		ui.unitActCondStaged[i] = cmp + 1
	}
	for i := range ui.unitActCondEds {
		ui.unitActUpdateEditor(gtx, &ui.unitActCondEds[i], i)
	}
	for i := range ui.unitActNameEds {
		ui.unitActUpdateEditor(gtx, &ui.unitActNameEds[i], i)
	}
	// Whichever field holds the caret is the selected choice: the move, add
	// and delete buttons of the footer act on it.
	for i := range ui.unitActCondEds {
		if ui.unitActCondEds[i].Focused(gtx) || (i < len(ui.unitActNameEds) && ui.unitActNameEds[i].Focused(gtx)) {
			ui.unitActSel = i
		}
	}
	if ui.unitActApply.Clicked(gtx) {
		ui.applyUnitActConditions()
	}
	if ui.unitActAddIf.Clicked(gtx) {
		ui.unitActAddChoice(actAddIf)
	}
	if ui.unitActAddElseIf.Clicked(gtx) {
		ui.unitActAddChoice(actAddElseIf)
	}
	if ui.unitActAddElse.Clicked(gtx) {
		ui.unitActAddChoice(actAddElse)
	}
	if ui.unitActMoveUp.Clicked(gtx) {
		ui.unitActMoveChoice(-1)
	}
	if ui.unitActMoveDown.Clicked(gtx) {
		ui.unitActMoveChoice(1)
	}
	if ui.unitActDelete.Clicked(gtx) {
		ui.unitActDeleteChoice()
	}
}

// unitActUpdateNameEditor owns the action's own name field: a click takes the
// caret, Enter renames, and the field refills from the picked action whenever
// the selection moves — but not while it holds the caret, so what is being
// typed is never overwritten.
func (ui *uiState) unitActUpdateNameEditor(gtx layout.Context) {
	ed := &ui.unitActNameEd
	ed.SingleLine = true
	ed.Submit = true
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	if !ed.Focused(gtx) {
		var act whe.FxAction
		have := false
		if ui.unitEvtDoc != nil {
			if a := ui.selectedUnitEvtAction(ui.unitEvtDoc.Actions()); a != nil {
				act = *a
				have = true
			}
		}
		bound := strconv.Itoa(ui.unitEvtActionSel) + "|" + act.Name
		if bound != ui.unitActNameBound {
			ui.unitActNameBound = bound
			if have {
				ed.SetText(act.Name)
			} else {
				ed.SetText("")
			}
		}
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.renameSelectedUnitAction()
			ed.Blur(gtx)
		}
	}
}

// renameSelectedUnitAction renames the picked action in the in-memory
// document. The rename also follows the name into every sub-action link of the
// file that called it, so a chain another action drives keeps working; a name
// another action already has is refused by the document rather than merged.
// Save .whe writes it into the mod.
func (ui *uiState) renameSelectedUnitAction() {
	doc := ui.unitEvtDoc
	if doc == nil {
		ui.unitActErr = "No .whe is loaded."
		return
	}
	acts := doc.Actions()
	if ui.unitEvtActionSel < 0 || ui.unitEvtActionSel >= len(acts) {
		ui.unitActErr = "Select an action to rename."
		return
	}
	old := acts[ui.unitEvtActionSel].Name
	next := strings.TrimSpace(ui.unitActNameEd.Text())
	if next == "" {
		ui.unitActErr = "An action needs a name."
		return
	}
	if strings.EqualFold(next, old) {
		ui.unitActErr = ""
		ui.statusText = "The action is already called " + old
		return
	}
	if err := doc.RenameAction(old, next); err != nil {
		ui.unitActErr = err.Error()
		return
	}
	ui.unitActErr = ""
	ui.unitEvtDirty = true
	// The conditions and the name field refill from what was just written.
	ui.unitActCondBound = ""
	ui.unitActNameBound = ""
	if i := actionIndexByName(doc, next); i >= 0 {
		ui.unitEvtActionSel = i
	}
	ui.reloadUnitModelFx()
	ui.statusText = fmt.Sprintf("Renamed action %s to %s — Save .whe to write it into the mod", old, next)
}

// unitActUpdateEditor processes one field of a choice line: a click takes
// the caret and selects that line, and Enter commits the conditions the way
// Apply does, never dropping a line break into the value.
func (ui *uiState) unitActUpdateEditor(gtx layout.Context, ed *widget.Editor, row int) {
	ed.SingleLine = true
	ed.Submit = true
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
		ui.unitActSel = row
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.unitActSel = row
			ui.applyUnitActConditions()
			ed.Blur(gtx)
		}
	}
}

// applyUnitActConditions writes the condition fields and their guards into
// the selected action of the in-memory document — the DATAACTS payload is
// rebuilt, the document is marked dirty, and the fields refill from what was
// written so what is shown is what the document holds. Nothing touches the
// file: Save .whe does that.
func (ui *uiState) applyUnitActConditions() {
	if ui.unitEvtDoc == nil {
		ui.unitActErr = "No .whe is loaded."
		return
	}
	acts := ui.unitEvtDoc.Actions()
	if ui.unitEvtActionSel < 0 || ui.unitEvtActionSel >= len(acts) {
		ui.unitActErr = "Select an action to apply the conditions to."
		return
	}
	a := acts[ui.unitEvtActionSel]
	links := unitActLinks(a)
	if len(links) == 0 {
		// Nothing to flush — an action with no choices yet only waits for
		// its first Add.
		ui.unitActErr = ""
		return
	}
	if len(ui.unitActCondEds) != len(links) || len(ui.unitActCondStaged) != len(links) {
		ui.unitActErr = "The condition fields are out of step with the file — pick the action again."
		return
	}
	changed := false
	for i, ln := range links {
		cmp := ui.unitActCondStaged[i]
		cond := ""
		if (whe.FxActionLink{Compare: cmp}).HasCondition() {
			cond = strings.TrimSpace(ui.unitActCondEds[i].Text())
		}
		name := ln.Name
		if i < len(ui.unitActNameEds) {
			name = strings.TrimSpace(ui.unitActNameEds[i].Text())
		}
		if name == "" {
			ui.unitActErr = "Every choice must name the motion or action it runs — fill in the name of line " + strconv.Itoa(i+1) + "."
			return
		}
		if ln.Compare == cmp && ln.Condition == cond && ln.Name == name {
			continue
		}
		if dst := unitActLinkAt(&a, i); dst != nil {
			dst.Compare = cmp
			dst.Condition = cond
			dst.Name = name
			changed = true
		}
	}
	if !changed {
		ui.unitActErr = ""
		ui.statusText = "no changes to apply"
		return
	}
	if err := ui.unitEvtDoc.SetAction(ui.unitEvtActionSel, a); err != nil {
		ui.unitActErr = err.Error()
		return
	}
	ui.unitActErr = ""
	ui.unitEvtDirty = true
	// The fields refill from what was just written.
	ui.unitActCondBound = ""
	ui.ensureUnitActConditions()
	ui.statusText = "Applied conditions to " + a.Name + " — Save .whe to write it into the mod"
}

// ═══════════════════════════════════════════════════════════════════════════
// TAB: chain tools — add, move and delete choices of the if / else if / else
// chain ═════════════════════════════════════════════════════════════════════
//
// The choices of an action live in two lists the format keeps apart — the
// motions, then the sub-actions — and each list reads as one if / else if /
// else chain: a head (an if, or an unguarded step) followed by its
// continuations. The tools below keep those shapes: a head moves with the
// whole chain it carries, a continuation only slides between the
// continuations of its own chain, a new continuation extends the chain it
// was added to, and deleting a head takes its chain while deleting a
// continuation removes only that step.

const (
	actAddIf = iota
	actAddElseIf
	actAddElse
)

// actIsHead reports whether a link starts a chain rather than continuing one.
func actIsHead(ln whe.FxActionLink) bool {
	return ln.Compare != whe.FxCompareElseIf && ln.Compare != whe.FxCompareElse
}

func actChainStart(list []whe.FxActionLink, i int) int {
	for i > 0 && !actIsHead(list[i]) {
		i--
	}
	return i
}

func actChainEnd(list []whe.FxActionLink, i int) int {
	e := actChainStart(list, i)
	for e+1 < len(list) && !actIsHead(list[e+1]) {
		e++
	}
	return e
}

// actLocate maps a combined row index onto the list it lives in and that
// list's own index — the motions come first, then the sub-actions.
func actLocate(a whe.FxAction, i int) (list []whe.FxActionLink, local int, motion, ok bool) {
	if i < 0 {
		return nil, i, false, false
	}
	if i < len(a.Motions) {
		return a.Motions, i, true, true
	}
	if i-len(a.Motions) < len(a.Subactions) {
		return a.Subactions, i - len(a.Motions), false, true
	}
	return nil, i, false, false
}

// actCombined maps a list index back onto the combined row index.
func actCombined(a whe.FxAction, motion bool, local int) int {
	if motion {
		return local
	}
	return len(a.Motions) + local
}

// actMoveLink slides one choice within its list — dir is -1 up, +1 down. A
// chain head swaps places with the neighbouring whole chain, so moving it
// never breaks the continuations it carries; an else if slides between the
// continuations of its own chain; the else that ends a chain stays pinned.
func actMoveLink(list []whe.FxActionLink, i, dir int) ([]whe.FxActionLink, int, bool) {
	if i < 0 || i >= len(list) || (dir != -1 && dir != 1) {
		return list, i, false
	}
	if actIsHead(list[i]) {
		s := actChainStart(list, i)
		e := actChainEnd(list, i)
		if dir < 0 {
			if s == 0 {
				return list, i, false
			}
			ps := actChainStart(list, s-1)
			out := make([]whe.FxActionLink, 0, len(list))
			out = append(out, list[:ps]...)
			out = append(out, list[s:e+1]...)
			out = append(out, list[ps:s]...)
			out = append(out, list[e+1:]...)
			return out, ps, true
		}
		if e == len(list)-1 {
			return list, i, false
		}
		ns := e + 1
		ne := actChainEnd(list, ns)
		out := make([]whe.FxActionLink, 0, len(list))
		out = append(out, list[:s]...)
		out = append(out, list[ns:ne+1]...)
		out = append(out, list[s:e+1]...)
		out = append(out, list[ne+1:]...)
		return out, s + (ne - ns + 1), true
	}
	if list[i].Compare == whe.FxCompareElse {
		// The else ends its chain; there is no position for it to slide to.
		return list, i, false
	}
	s := actChainStart(list, i)
	e := actChainEnd(list, i)
	j := i + dir
	if j < s || j > e || actIsHead(list[j]) || list[j].Compare == whe.FxCompareElse {
		return list, i, false
	}
	out := append([]whe.FxActionLink(nil), list...)
	out[i], out[j] = out[j], out[i]
	return out, j, true
}

// actAddLink inserts a new choice into the list the selected row lives in: a
// new if starts a fresh chain at the end of that list, while an else if /
// else continues the selected row's chain right after it and takes over the
// head's target name — the usual reason to guard the same motion twice.
func actAddLink(a *whe.FxAction, kind, at int) (int, bool) {
	// Which list the selected row lives in: rows are the motions first,
	// then the sub-actions. An action whose choices are all sub-actions has
	// no motions at all, and `at < len(a.Motions)` — not a length check on
	// the motions — is what tells the two apart; with no motions the old
	// check resolved every row to an empty motion list and every add was
	// refused.
	motion := at < len(a.Motions)
	if len(a.Motions) == 0 && len(a.Subactions) == 0 {
		// A brand-new action: its first choice starts as a motion.
		motion = true
	}
	local := at
	if !motion {
		local = at - len(a.Motions)
	}
	var list []whe.FxActionLink
	if motion {
		list = a.Motions
	} else {
		list = a.Subactions
	}
	ln := whe.FxActionLink{}
	switch kind {
	case actAddIf:
		ln.Compare = whe.FxCompareIf
		list = append(list, ln)
		newAt := actCombined(*a, motion, len(list)-1)
		if motion {
			a.Motions = list
		} else {
			a.Subactions = list
		}
		return newAt, true
	case actAddElseIf, actAddElse:
		if len(list) == 0 || local < 0 || local >= len(list) {
			return at, false
		}
		e := actChainEnd(list, local)
		insert := e + 1
		if kind == actAddElse {
			if list[e].Compare == whe.FxCompareElse {
				// A chain has one else, and it is its last step.
				return at, false
			}
			ln.Compare = whe.FxCompareElse
		} else {
			if list[e].Compare == whe.FxCompareElse {
				// The else ends the chain: a new else if goes in front of
				// it, so the chain keeps ending with the else and the move
				// buttons have somewhere to slide it from.
				insert = e
			}
			ln.Compare = whe.FxCompareElseIf
		}
		ln.Name = list[actChainStart(list, local)].Name
		out := make([]whe.FxActionLink, 0, len(list)+1)
		out = append(out, list[:insert]...)
		out = append(out, ln)
		out = append(out, list[insert:]...)
		newAt := actCombined(*a, motion, insert)
		if motion {
			a.Motions = out
		} else {
			a.Subactions = out
		}
		return newAt, true
	}
	return at, false
}

// actDeleteLink removes one choice: deleting a chain head takes its whole
// chain with it, deleting an else if / else removes only that step.
func actDeleteLink(a *whe.FxAction, at int) (int, bool) {
	list, j, motion, ok := actLocate(*a, at)
	if !ok {
		return at, false
	}
	end := j
	if actIsHead(list[j]) {
		end = actChainEnd(list, j)
	}
	out := make([]whe.FxActionLink, 0, len(list)-(end-j))
	out = append(out, list[:j]...)
	out = append(out, list[end+1:]...)
	if motion {
		a.Motions = out
	} else {
		a.Subactions = out
	}
	newAt := actCombined(*a, motion, j)
	if total := len(a.Motions) + len(a.Subactions); newAt >= total {
		newAt = total - 1
	}
	if newAt < 0 {
		newAt = 0
	}
	return newAt, true
}

// unitActMutate flushes the typed fields into the document and then runs one
// structural change on the selected action's choices. Every step goes
// through SetAction and marks the document dirty, so it lands in the file
// with the same Save .whe the text edits take.
func (ui *uiState) unitActMutate(what string, change func(a *whe.FxAction) (int, bool)) {
	if ui.unitEvtDoc == nil {
		ui.unitActErr = "No .whe is loaded."
		return
	}
	// The typed fields go in first: a structural change refills them from
	// the document, and unsaved typing would be dropped under it.
	ui.applyUnitActConditions()
	if ui.unitActErr != "" {
		return
	}
	acts := ui.unitEvtDoc.Actions()
	if ui.unitEvtActionSel < 0 || ui.unitEvtActionSel >= len(acts) {
		ui.unitActErr = "Select an action first."
		return
	}
	a := acts[ui.unitEvtActionSel]
	if rows := len(a.Motions) + len(a.Subactions); rows > 0 && (ui.unitActSel < 0 || ui.unitActSel >= rows) {
		ui.unitActSel = 0
	}
	newAt, ok := change(&a)
	if !ok {
		ui.unitActErr = what + " is not possible here."
		return
	}
	if err := ui.unitEvtDoc.SetAction(ui.unitEvtActionSel, a); err != nil {
		ui.unitActErr = err.Error()
		return
	}
	ui.unitActErr = ""
	ui.unitEvtDirty = true
	ui.unitActCondBound = ""
	ui.ensureUnitActConditions()
	ui.unitActSel = newAt
	ui.statusText = what + " — Save .whe to write it into the mod"
}

func (ui *uiState) unitActAddChoice(kind int) {
	what := "Added if"
	switch kind {
	case actAddElseIf:
		what = "Added else if"
	case actAddElse:
		what = "Added else"
	}
	ui.unitActMutate(what, func(a *whe.FxAction) (int, bool) {
		return actAddLink(a, kind, ui.unitActSel)
	})
}

func (ui *uiState) unitActMoveChoice(dir int) {
	what := "Moved down"
	if dir < 0 {
		what = "Moved up"
	}
	ui.unitActMutate(what, func(a *whe.FxAction) (int, bool) {
		list, j, motion, ok := actLocate(*a, ui.unitActSel)
		if !ok {
			return ui.unitActSel, false
		}
		moved, nj, ok := actMoveLink(list, j, dir)
		if !ok {
			return ui.unitActSel, false
		}
		if motion {
			a.Motions = moved
		} else {
			a.Subactions = moved
		}
		return actCombined(*a, motion, nj), true
	})
}

func (ui *uiState) unitActDeleteChoice() {
	ui.unitActMutate("Deleted", func(a *whe.FxAction) (int, bool) {
		return actDeleteLink(a, ui.unitActSel)
	})
}

// unitActMoveEnabled reports whether the move buttons act: the selected
// choice has a neighbour of the kind it may swap with.
func (ui *uiState) unitActMoveEnabled(dir int) bool {
	if ui.unitEvtDoc == nil {
		return false
	}
	a := ui.selectedUnitEvtAction(ui.unitEvtDoc.Actions())
	if a == nil {
		return false
	}
	list, j, _, ok := actLocate(*a, ui.unitActSel)
	if !ok {
		return false
	}
	_, _, ok = actMoveLink(list, j, dir)
	return ok
}

// unitActionConditions is one action's choices as a chain of one-line steps:
// each choice reads as "<guard> <condition> → <motion or action>", the
// guard chip is pressed to cycle if → else if → else → always, and the line
// is committed with Apply and written with Save .whe like every other edit
// of this section.
func (ui *uiState) unitActionConditions(gtx layout.Context, a whe.FxAction) layout.Dimensions {
	ui.ensureUnitActConditions()
	if len(a.Motions)+len(a.Subactions) == 0 {
		l := material.Label(ui.th, unit.Sp(9), "This action carries no motion and calls no action.")
		l.Color = cTextDim
		return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var rows []layout.FlexChild
	for i, m := range a.Motions {
		i, ln := i, m
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutUnitActConditionRow(gtx, i, ln)
		}))
	}
	for i, s := range a.Subactions {
		i, ln := len(a.Motions)+i, s
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutUnitActConditionRow(gtx, i, ln)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// layoutUnitActConditionRow is one choice of the picked action, drawn as a
// single line: the guard chip — press it to cycle if → else if → else →
// always — then the condition that guard carries, an arrow, and the motion
// or action the choice runs. An else / else if line continues the if above
// it, so it is indented the way a nested line of code is, and each line
// sits in its own card so the chain reads as steps rather than one wall of
// text.
func (ui *uiState) layoutUnitActConditionRow(gtx layout.Context, i int, ln whe.FxActionLink) layout.Dimensions {
	if i < 0 || i >= len(ui.unitActCondEds) || i >= len(ui.unitActCondStaged) || i >= len(ui.unitActGuardClicks) || i >= len(ui.unitActNameEds) {
		return layout.Dimensions{}
	}
	cmp := ui.unitActCondStaged[i]
	hasCond := (whe.FxActionLink{Compare: cmp}).HasCondition()
	indent := unit.Dp(0)
	switch cmp {
	case whe.FxCompareElseIf, whe.FxCompareElse:
		indent = unit.Dp(14)
	}
	line := func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitActGuardChip(gtx, i, unitActGuardLabel(cmp))
			}),
		}
		if hasCond {
			children = append(children,
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return ui.unitActFieldEditor(gtx, &ui.unitActCondEds[i], "condition")
				}),
			)
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "\u2192")
				l.Color = cTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Flexed(2, func(gtx layout.Context) layout.Dimensions {
				return ui.unitActFieldEditor(gtx, &ui.unitActNameEds[i], "motion or action")
			}),
		)
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	}
	// Measure the line before the card is drawn. The card's Expanded
	// background fills whatever height the row is handed, and in the
	// unbounded measure a scrolling panel makes that the whole panel —
	// every line after the first would be pushed off screen and no
	// scrollbar would appear. Clamping the row to its measured height is
	// what the fixed-height event rows do for the same reason.
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min.Y = 0
	measureGTX.Constraints.Max.Y = panelScrollMeasureHeight
	h := line(measureGTX).Size.Y
	macro.Stop()
	if h <= 0 || h >= panelScrollMeasureHeight {
		macro := op.Record(gtx.Ops)
		measureGTX := gtx.Disabled()
		measureGTX.Constraints.Min.Y = 0
		h = line(measureGTX).Size.Y
		macro.Stop()
	}
	if h <= 0 {
		h = gtx.Dp(unit.Dp(28))
	}
	// The card's inner inset adds its own top and bottom padding around the
	// line, so the row claims the line *plus* that padding — clamping to the
	// line alone drew a card shorter than its content, and the chip and the
	// fields hung over its bottom edge into the next row.
	rowGTX := gtx
	rowGTX.Constraints.Min.Y = h + gtx.Dp(unit.Dp(12))
	rowGTX.Constraints.Max.Y = h + gtx.Dp(unit.Dp(12))
	bg := cTabOff
	if i == ui.unitActSel {
		bg = cSelectBg
	}
	return layout.Inset{Left: indent, Bottom: unit.Dp(4)}.Layout(rowGTX, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				rrect(gtx, bg, sz, gtx.Dp(3))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				inset := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}
				return inset.Layout(gtx, line)
			}),
		)
	})
}

// unitActFieldEditor is an inline field of one choice line — the same
// bordered field the event definition rows use, clamped to one line's height
// so it sits on the line beside the arrow without stretching the row.
func (ui *uiState) unitActFieldEditor(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	ed.SingleLine = true
	ed.Submit = true
	gtx.Constraints.Min.Y = gtx.Dp(36)
	gtx.Constraints.Max.Y = gtx.Dp(40)
	b := widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			w := material.Editor(ui.th, ed, hint)
			w.TextSize = unit.Sp(10)
			w.HintColor = cTextDim
			return w.Layout(gtx)
		})
	})
}

// unitActGuardChip is the operator of one choice line, sized from its own
// label rather than from the room the row has left. smallButton reads the
// ambient constraints for its size, and inside a card row those numbers
// belong to the row, not to the chip — the chip must carry its own width so
// the arrow and the fields after it are not pushed off the line.
func (ui *uiState) unitActGuardChip(gtx layout.Context, i int, label string) layout.Dimensions {
	if i < 0 || i >= len(ui.unitActGuardClicks) {
		return layout.Dimensions{}
	}
	c := &ui.unitActGuardClicks[i]
	// Measure the label on its own; the discarded pass draws nothing.
	macro := op.Record(gtx.Ops)
	lgtx := gtx.Disabled()
	lgtx.Constraints.Min = image.Point{}
	l := material.Label(ui.th, unit.Sp(11), label)
	l.Color = cText
	ldims := l.Layout(lgtx)
	macro.Stop()
	padX, padY := unit.Dp(10), unit.Dp(5)
	inner := image.Pt(ldims.Size.X+2*gtx.Dp(padX), ldims.Size.Y+2*gtx.Dp(padY))
	if inner.Y < gtx.Dp(unit.Dp(30)) {
		inner.Y = gtx.Dp(unit.Dp(30))
	}
	bg := cSaveBg
	switch {
	case c.Pressed():
		bg = cMenuOpen
	case c.Hovered():
		bg = cSaveHover
	}
	return widget.Border{Color: cSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Exact constraints inside: nothing the row hands down can leak
		// into the chip's own size or into where its label is drawn.
		gtx.Constraints.Min = inner
		gtx.Constraints.Max = inner
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				rrect(gtx, bg, sz, gtx.Dp(3))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				// Centre the label: the chip is taller than the text, and
				// fixed padding would pin it to the top of the box. The
				// offset stays in pixels so scaling does not double it.
				padTop := (inner.Y - ldims.Size.Y) / 2
				if padTop < 0 {
					padTop = 0
				}
				off := op.Offset(image.Pt(gtx.Dp(padX), padTop)).Push(gtx.Ops)
				l := material.Label(ui.th, unit.Sp(11), label)
				l.Color = cText
				dims := l.Layout(gtx)
				off.Pop()
				return layout.Dimensions{Size: image.Pt(dims.Size.X+2*gtx.Dp(padX), dims.Size.Y)}
			}),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
			}),
		)
	})
}

// unitActPanelFooter is the bottom of the right column while an action is
// being worked on: the commit for the condition fields above, and the same
// Save .whe / Revert pair the timeline form carries — an action edit is
// written to the file exactly the way a timeline edit is.
func (ui *uiState) unitActPanelFooter(gtx layout.Context) layout.Dimensions {
	hasAction := false
	rows := 0
	selName := ""
	if ui.unitEvtDoc != nil {
		if a := ui.selectedUnitEvtAction(ui.unitEvtDoc.Actions()); a != nil {
			hasAction = true
			rows = len(a.Motions) + len(a.Subactions)
			if ui.unitActSel >= 0 && ui.unitActSel < rows {
				if list, j, _, ok := actLocate(*a, ui.unitActSel); ok {
					selName = list[j].Name
				}
			}
		}
	}
	var children []layout.FlexChild
	if ui.unitActErr != "" {
		err := ui.unitActErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), err)
			l.Color = rgb(0xC98A8A)
			l.MaxLines = 4
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	// The chain tools act on the selected choice — whichever line held the
	// caret last — and keep the if / else if / else shape intact: a head
	// moves with its whole chain, a continuation slides inside its own.
	selLabel := ""
	if selName != "" {
		selLabel = "selected:  " + unitViewportRowText(selName)
	} else if hasAction && rows > 0 {
		selLabel = "pick a line first"
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitActAddIf, "Add if", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitActAddElseIf, "+elif", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitActAddElse, "+else", !hasAction)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitActMoveUp, "\u25b2", !ui.unitActMoveEnabled(-1))
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitActMoveDown, "\u25bc", !ui.unitActMoveEnabled(1))
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitActDelete, "Delete", rows == 0)
				}),
				layout.Flexed(1, rowGap()),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(9), selLabel)
					l.Color = cTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
			)
		})
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &ui.unitActApply, "Apply", !hasAction)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.primaryButton(gtx, &ui.unitEvtSave, "Save .whe")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &ui.unitEvtRevert, "Revert", !ui.unitEvtDirty)
			}),
		)
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

// unitEvtActionPanelBody is the right column while an action is being worked
// on: that action's own properties — its name and the choices the game walks
// when it runs, each guarded by its step of the if / else if / else chain —
// and none of the timeline's events. The panel shows one or the other, so a
// timeline being edited gives the whole panel to its events instead.
func (ui *uiState) unitEvtActionPanelBody(gtx layout.Context) []layout.FlexChild {
	acts := ui.unitEvtDoc.Actions()
	a := ui.selectedUnitEvtAction(acts)
	if a == nil {
		return []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitEventsMessage(gtx, "This .whe carries no actions.")
			}),
		}
	}
	act := *a
	text := "Action  " + act.Name
	if ui.unitEvtDirty {
		text += "  • unsaved"
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), text)
			l.Color = cTextDim
			l.MaxLines = 2
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		// The action's name is its identity across the file, and it is what a
		// paste from another unit lands on — so it can be changed here, which
		// is how a name this .whe already uses is settled without editing the
		// file by hand.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(112)
					l := material.Label(ui.th, unit.Sp(9), "Name")
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
							w := material.Editor(ui.th, &ui.unitActNameEd, act.Name)
							w.TextSize = unit.Sp(10)
							w.HintColor = cTextDim
							return w.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &ui.unitActRename, "Rename")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Conditions")
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), "One line per choice — press a guard to cycle it, type the condition inline. Apply or Enter writes the document; Save .whe writes the file.")
			l.Color = cTextDim
			l.MaxLines = 3
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitActionConditions(gtx, act)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), "Pick a timeline above to edit its events instead.")
			l.Color = cTextDim
			l.MaxLines = 2
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
		}),
	}
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
		// A timeline is copied whole from here — its events, its clips and the
		// definitions they name — and pastes into the timeline of the same name
		// in any unit, which is what carries one unit's effects onto another.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &ui.unitEvtCopyTl, "Copy timeline", selected == nil)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), "copies it whole, for another unit")
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						})
					}),
				)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutListWithScrollbar(gtx, &ui.unitEvtScroll, &ui.unitEvtList, unitRowHeightDp-4, len(tls), func(gtx layout.Context, i int) layout.Dimensions {
				return ui.layoutUnitEventTimelineRow(gtx, i, tls[i])
			})
		}),
	)
}

// layoutUnitEventPanel is the right column. It carries the tools of whatever
// the reader is working on: the sound transport, the selected timeline's
// events and the form that edits one while a timeline is picked, and the
// picked action's conditions while an action is — never both tool sets at
// once, since an action carries no timeline and no event of its own.
func (ui *uiState) layoutUnitEventPanel(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitEvents()
	sec := ui.currentUnitSection()
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		}
		if !ui.unitEvtFocusAction {
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutUnitEvtTransport(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			)
		}
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.panelScrollView(gtx, &ui.unitEvtPanelScroll, func(gtx layout.Context) layout.Dimensions {
				return ui.unitEventPanelBody(gtx)
			})
		}))
		if !ui.unitEvtFocusAction {
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitEventPanelForm(gtx)
				}),
			)
		} else {
			// The action panel carries the same commit and file pair the
			// timeline form does, so an action's conditions are written the
			// way a timeline's events are.
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitActPanelFooter(gtx)
				}),
			)
		}
		// The mesh list is pinned below both editors, so it stays on screen
		// whether a timeline or an action is being worked on.
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitMeshVisibilitySection(gtx)
			}),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
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
	// The panel carries one editor at a time. While an action is being worked
	// on it shows that action's conditions and none of the timeline's events,
	// and the other way round: stacking the two put both editors in one panel
	// and left the reader hunting for the one they were in.
	if ui.unitEvtFocusAction {
		children = ui.unitEvtActionPanelBody(gtx)
	} else if tl == nil {
		msg := ui.unitEvtErr
		if msg == "" {
			msg = "Select a timeline in the middle column."
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitEventsMessage(gtx, msg)
		}))
		// A copied timeline needs no target picked: it carries its own name, so
		// a .whe whose timelines do not include it can still receive it whole.
		if entry, _, ok := ui.unitClips.Pick(clipEvents, ui.unitClipCur); ok && entry.Events != nil && entry.Events.Timeline != "" {
			name := entry.Events.Timeline
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &ui.unitEvtPaste, "Paste timeline "+name)
				})
			}))
		}
	} else {
		// The body below this point belongs to the selected timeline: its header,
		// the motion's own properties, its event rows and the selected event's
		// definition.
		// Which file and timeline this is, and whether it has unsaved changes.
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := "Timeline  " + tl.Name
			if ui.unitEvtDirty {
				text += "  • unsaved"
			}
			l := material.Label(ui.th, unit.Sp(9), text)
			l.Color = cTextDim
			l.MaxLines = 2
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
		}))
		// The clips this motion blends, one row each with its own Remove
		// button — the leading strings of the DATAMTON payload, and the thing
		// a reader most often wants to prune. Edited in memory only: Save .whe
		// writes the change into the mod.
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Animations")
			})
		}))
		ui.ensureUnitEventAnimClicks(len(tl.Animations))
		if len(tl.Animations) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "This motion blends no animations — it chains to other motions instead.")
				l.Color = cTextDim
				l.MaxLines = 2
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
			}))
		} else {
			for i := range tl.Animations {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(10), tl.Animations[i])
								l.Color = cText
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return ui.smallButton(gtx, &ui.unitEvtAnimClicks[i], "Remove")
							}),
						)
					})
				}))
			}
		}
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
	}
	dims := inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
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
		// The same moment, moved rather than set: a number of seconds added to
		// every selected row, with the button beside the field that reads it.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(112)
					l := material.Label(ui.th, unit.Sp(9), "Shift (s)")
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
							w := material.Editor(ui.th, &ui.unitEvtShiftEd, "e.g. 0.1 or -0.1")
							w.TextSize = unit.Sp(10)
							w.HintColor = cTextDim
							return w.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButtonDisabled(gtx, &ui.unitEvtShift, "Shift", len(ui.unitEvtSelSet) == 0)
				}),
			)
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
	// While several rows are selected the moment field is a batch edit, so the
	// panel says so where the button that does it sits.
	if n := len(ui.unitEvtSelectedIndices()); n > 1 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.th, fmt.Sprintf("%d rows selected — Apply sets the typed time on all of them, Shift moves it.", n))
			})
		}))
	}
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
					_, _, has := ui.unitClips.Pick(clipEvents, ui.unitClipCur)
					return ui.smallButtonDisabled(gtx, &ui.unitEvtPaste, "Paste", !has)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, cur, ok := ui.unitClips.Pick(clipEvents, ui.unitClipCur)
					first := !ok || ui.unitClips.Step(clipEvents, cur, -1) == cur
					return ui.smallButtonDisabled(gtx, &ui.unitClipPrev, "\u25c2", first)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					_, cur, ok := ui.unitClips.Pick(clipEvents, ui.unitClipCur)
					last := !ok || ui.unitClips.Step(clipEvents, cur, 1) == cur
					return ui.smallButtonDisabled(gtx, &ui.unitClipNext, "\u25b8", last)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), ui.unitClips.Label(clipEvents, ui.unitClipCur))
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
