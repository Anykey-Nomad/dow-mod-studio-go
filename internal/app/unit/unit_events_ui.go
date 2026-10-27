package unit

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/cliphist"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"image/color"
	"math"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// UnitSectionEvents names the section, so the layout does not repeat the
// string of the menu entry it is keyed on.
const UnitSectionEvents = "Events"

// UnitEventsWhePath is the .whe that carries the selected unit's event
// timelines: the file beside the model its blueprint names, then the animator
// path on its own, then a search by name for a model that was renamed.
func UnitEventsWhePath(tree *modtree.ModTree, d *uistate.UnitDetails) string {
	if tree == nil || d == nil {
		return ""
	}
	var candidates []string
	if src, ok := NewUnitModelSource(tree, d); ok {
		if model := UnitModelPath(tree, src); model != "" {
			candidates = append(candidates, strings.TrimSuffix(model, path.Ext(model))+".whe")
		}
		rel := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(src.Animator), `\`, "/"), "./")
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

// TimelineMatchesClip reports whether a timeline drives the named clip, by its
// own name or by the clips it blends.
func TimelineMatchesClip(tl whe.FxTimeline, clip string) bool {
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
func SelectedUnitEventTimeline(ui *uistate.State) *whe.FxTimeline {
	if ui.UnitEvtDoc == nil || ui.UnitEvtTimeline == "" {
		return nil
	}
	for _, tl := range ui.UnitEvtDoc.Timelines() {
		tl := tl
		if strings.EqualFold(tl.Name, ui.UnitEvtTimeline) {
			return &tl
		}
	}
	return nil
}

// selectUnitEventTimeline picks the timeline whose events are listed, which is
// what pressing a row of the middle column does.
func SelectUnitEventTimeline(ui *uistate.State, name string) {
	if strings.EqualFold(ui.UnitEvtTimeline, name) {
		return
	}
	ui.UnitEvtTimeline = name
	ui.UnitEvtSel = 0
	ui.UnitEvtSelSet = map[int]bool{0: true}
	ui.UnitEvtSelAnchor = 0
	ui.UnitEvtBound = ""
}

func EnsureUnitEventClicks(ui *uistate.State, timelines, events int) {
	if len(ui.UnitEvtTlClicks) != timelines {
		ui.UnitEvtTlClicks = make([]widget.Clickable, timelines)
	}
	if len(ui.UnitEvtClicks) != events {
		ui.UnitEvtClicks = make([]widget.Clickable, events)
	}
}

func EnsureUnitEventAnimClicks(ui *uistate.State, animations int) {
	if len(ui.UnitEvtAnimClicks) != animations {
		ui.UnitEvtAnimClicks = make([]widget.Clickable, animations)
	}
}

func EnsureUnitMeshClicks(ui *uistate.State, meshes int) {
	if len(ui.UnitMeshClicks) != meshes {
		ui.UnitMeshClicks = make([]widget.Clickable, meshes)
	}
}

// ensureUnitMeshNameClicks sizes the name half of each mesh row to match the
// list, so pressing a name always addresses the mesh that row shows.
func EnsureUnitMeshNameClicks(ui *uistate.State, meshes int) {
	if len(ui.UnitMeshNameClicks) != meshes {
		ui.UnitMeshNameClicks = make([]widget.Clickable, meshes)
	}
}

// handleUnitMeshToggles drains the eye buttons of the pinned mesh list and
// flips the shared hidden set, redrawing the viewer so the change is on screen
// at once. It runs whether or not a .whe is loaded, since the list acts on the
// model rather than the document, and the order of names is the one the panel
// lays out, so button i always belongs to mesh i.
func HandleUnitMeshToggles(ui *uistate.State, gtx layout.Context) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return
	}
	names := viewer.ModelMeshNames(v.Model)
	EnsureUnitMeshClicks(ui, len(names))
	for i := range names {
		if i >= len(ui.UnitMeshClicks) {
			break
		}
		if ui.UnitMeshClicks[i].Clicked(gtx) {
			viewer.SetMeshHidden(names[i], !viewer.MeshHidden(names[i]))
			viewer.Invalidate(v)
		}
	}
}

// UnitMeshSectionMaxDp caps how tall the pinned mesh list may grow; past it
// the list scrolls rather than pushing the panel's own content off the top.
const UnitMeshSectionMaxDp = 168

// EyeToggleSizeDp is the tap box of one eye button.
const EyeToggleSizeDp = 18

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
func UnitMeshRows(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return MeshRowsMessage(ui, gtx, "No model is loaded for this unit.")
	}
	names := viewer.ModelMeshNames(v.Model)
	if len(names) == 0 {
		return MeshRowsMessage(ui, gtx, "This model carries no meshes.")
	}
	EnsureUnitMeshClicks(ui, len(names))
	EnsureUnitMeshNameClicks(ui, len(names))
	var rc []layout.FlexChild
	for i := range names {
		i := i
		name := names[i]
		hidden := viewer.MeshHidden(name)
		nameC := &ui.UnitMeshNameClicks[i]
		// A second press on the name inside the window raises this mesh's
		// material editor. The eye beside it stays a plain visibility toggle.
		if nameC.Clicked(gtx) && ClassicOEDoubleClick(ui, "mesh:"+strings.ToLower(name)) {
			OpenUnitMeshMaterialDialog(ui, name)
		}
		rc = append(rc, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return EyeToggle(ui, gtx, &ui.UnitMeshClicks[i], !hidden)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return nameC.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), name)
							if hidden {
								l.Color = uikit.CTextDim
							} else {
								l.Color = uikit.CText
							}
							if nameC.Hovered() {
								l.Color = uikit.Rgb(0x53B9FF)
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
func MeshRowsMessage(ui *uistate.State, gtx layout.Context, msg string) layout.Dimensions {
	l := material.Label(ui.Th, unit.Sp(9), msg)
	l.Color = uikit.CTextDim
	return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
}

// eyeToggle is one mesh's visibility button: an open eye while the mesh
// draws, a closed eye while it is hidden. The glyph is drawn as vector shapes
// rather than a font character, so it renders the same everywhere.
func EyeToggle(ui *uistate.State, gtx layout.Context, btn *widget.Clickable, visible bool) layout.Dimensions {
	px := gtx.Dp(EyeToggleSizeDp)
	col := uikit.CText
	if !visible {
		col = uikit.CTextDim
	}
	if btn.Hovered() {
		col = uikit.Rgb(0x53B9FF)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(px, px)
		stack := clip.Rect{Max: sz}.Push(gtx.Ops)
		DrawEyeGlyph(gtx.Ops, sz, visible, col)
		stack.Pop()
		return layout.Dimensions{Size: sz}
	})
}

// DrawEyeGlyph paints an open or closed eye inside a sz box. Open is an
// almond outline with a pupil; closed is the lid drawn as a single downward
// curve — the pair reads as shown/hidden without needing a label.
func DrawEyeGlyph(ops *op.Ops, sz image.Point, open bool, col color.NRGBA) {
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

// ClipEventKey normalizes an event name for the definition snapshot: the keys
// Events() carries are lower case and trimmed, and a timeline may store the
// name with padding bytes around it.
func ClipEventKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// unitEvtSelectedIndices is the selected rows of the current timeline in
// ascending order: what Copy, Remove and the row highlight act on.
func UnitEvtSelectedIndices(ui *uistate.State) []int {
	out := make([]int, 0, len(ui.UnitEvtSelSet))
	for i := range ui.UnitEvtSelSet {
		if i >= 0 {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

// unitEvtRowSelected says whether a row wears the selection colour.
func UnitEvtRowSelected(ui *uistate.State, i int) bool {
	return ui.UnitEvtSelSet[i]
}

// unitEvtBlurFields lets go of any caret this section holds: a row click
// ends editing, so the selection change is not stuck behind a field that is
// still focused and the shortcuts below become available again.
func UnitEvtBlurFields(ui *uistate.State, gtx layout.Context) {
	for i := range ui.UnitEvtEntryEds {
		ui.UnitEvtEntryEds[i].Blur(gtx)
	}
	ui.UnitActNameEd.Blur(gtx)
	for i := range ui.UnitActCondEds {
		ui.UnitActCondEds[i].Blur(gtx)
	}
	for i := range ui.UnitActNameEds {
		ui.UnitActNameEds[i].Blur(gtx)
	}
	for _, ed := range []*widget.Editor{&ui.UnitEvtNameEd, &ui.UnitEvtTimeEd, &ui.UnitEvtFxEd, &ui.UnitEvtSndEd, &ui.UnitEvtMarkerEd, &ui.UnitEvtShiftEd} {
		ed.Blur(gtx)
	}
}

// syncUnitEventForm refills the form fields when the selection changes.
func SyncUnitEventForm(ui *uistate.State) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events) {
		ui.UnitEvtBound = ""
		return
	}
	bound := ui.UnitEvtTimeline + "|" + strconv.Itoa(ui.UnitEvtSel)
	if ui.UnitEvtBound == bound {
		return
	}
	ev := tl.Events[ui.UnitEvtSel]
	ui.UnitEvtBound = bound
	ui.UnitEvtNameEd.SetText(ev.Name)
	ui.UnitEvtTimeEd.SetText(UnitEventTimeEdit(ev.Time))
	ui.UnitEvtFxEd.SetText("")
	ui.UnitEvtSndEd.SetText("")
	ui.UnitEvtMarkerEd.SetText("")
	def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !ok {
		return
	}
	if p, has := def.First("sound"); has {
		ui.UnitEvtSndEd.SetText(p)
		return
	}
	if p, has := def.First("fx"); has {
		ui.UnitEvtFxEd.SetText(p)
	}
	if m, has := def.First("marker"); has {
		ui.UnitEvtMarkerEd.SetText(m)
	}
}

// UnitEventTimeEdit is the plain text a moment is edited as, without the
// units the rows show.
func UnitEventTimeEdit(t float64) string {
	if t < 0 {
		return "-1"
	}
	if math.IsInf(t, 1) || t > 1e30 {
		return "leave"
	}
	return strconv.FormatFloat(t, 'f', -1, 64)
}

// UnitEventTime parses the time field: empty means the start of the clip,
// "enter" the DATAMTON sentinel for on entering, "leave" the one for on
// leaving, and anything else seconds into the clip.
func UnitEventTime(text string) (float64, error) {
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

func StatusEventAdded(ui *uistate.State, name string, at float64) {
	ui.StatusText = fmt.Sprintf("Added %s at %s on %s — save to write it into the mod",
		name, UnitEventTimeLabel(at), ui.UnitEvtTimeline)
}

// stepUnitClip moves the history position through the entries that fit the
// events field, from the entry Paste would take now.
func StepUnitClip(ui *uistate.State, dir int) {
	_, cur, ok := ui.UnitClips.Pick(cliphist.ClipEvents, ui.UnitClipCur)
	if !ok {
		return
	}
	if next := ui.UnitClips.Step(cliphist.ClipEvents, cur, dir); next >= 0 {
		ui.UnitClipCur = next
	}
}

// copySelectedUnitEvents puts the selected rows and a snapshot of their
// definitions into the shared buffer, from where Paste carries them into any
// timeline — of this unit or of the next one, since the snapshot travels with
// the copy rather than pointing back at the .whe it was read from.
func CopySelectedUnitEvents(ui *uistate.State) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil {
		ui.StatusText = "Select a timeline first"
		return
	}
	indices := UnitEvtSelectedIndices(ui)
	if len(indices) == 0 {
		ui.StatusText = "Nothing is selected to copy"
		return
	}
	defs := ui.UnitEvtDoc.Events()
	clip := &cliphist.WheEventClip{Defs: map[string]whe.FxEvent{}}
	for _, i := range indices {
		if i < 0 || i >= len(tl.Events) {
			continue
		}
		ev := tl.Events[i]
		clip.Events = append(clip.Events, ev)
		if d, ok := defs[ClipEventKey(ev.Name)]; ok {
			clip.Defs[ClipEventKey(ev.Name)] = d
		}
	}
	if len(clip.Events) == 0 {
		ui.StatusText = "Nothing is selected to copy"
		return
	}
	ui.UnitClips.Push(cliphist.ClipEntry{
		Kind:   cliphist.ClipEvents,
		Label:  fmt.Sprintf("%d event(s) from %s", len(clip.Events), ui.UnitEvtTimeline),
		At:     time.Now(),
		Events: clip,
	})
	// The copy just made is the one Paste takes until the history is stepped.
	ui.UnitClipCur = len(ui.UnitClips.Entries) - 1
	ui.StatusText = fmt.Sprintf("Copied %d event(s) — Paste into any timeline", len(clip.Events))
}

// copySelectedUnitTimeline puts the whole selected timeline into the shared
// buffer: its events, the clips it blends and the definitions those events
// name. Copying a few rows is a paste of those rows; this is a paste of the
// timeline itself, so it lands on the timeline of its own name — in this unit
// or in the next one, where it is merged in or created from the copied bytes.
func CopySelectedUnitTimeline(ui *uistate.State) {
	doc := ui.UnitEvtDoc
	tl := SelectedUnitEventTimeline(ui)
	if doc == nil || tl == nil {
		ui.StatusText = "Select a timeline first"
		return
	}
	clip, ok := TimelineClipFrom(doc, tl.Name)
	if !ok {
		ui.StatusText = "This timeline could not be read back from the .whe"
		return
	}
	ui.UnitClips.Push(cliphist.ClipEntry{
		Kind:   cliphist.ClipEvents,
		Label:  fmt.Sprintf("timeline %s (%d event(s))", clip.Timeline, len(clip.Events)),
		At:     time.Now(),
		Events: clip,
	})
	ui.UnitClipCur = len(ui.UnitClips.Entries) - 1
	ui.StatusText = fmt.Sprintf("Copied timeline %s (%d event(s), %d animation(s)) — Paste in any unit",
		clip.Timeline, len(clip.Events), len(clip.Animations))
}

// TimelineClipFrom reads one timeline of doc into a clipboard copy: its raw
// DATAMTON payload, the clips it blends, its events and the definitions behind
// them. Everything a paste needs travels inside the copy, which is what lets a
// timeline read here be written into another unit's file.
func TimelineClipFrom(doc *whe.Document, name string) (*cliphist.WheEventClip, bool) {
	tl, ok := TimelineOf(doc, name)
	if !ok {
		return nil, false
	}
	payload, ok := doc.TimelinePayload(tl.Name)
	if !ok {
		return nil, false
	}
	clip := &cliphist.WheEventClip{
		Timeline:   tl.Name,
		Animations: append([]string(nil), tl.Animations...),
		Payload:    payload,
		Defs:       map[string]whe.FxEvent{},
	}
	defs := doc.Events()
	for _, ev := range tl.Events {
		clip.Events = append(clip.Events, ev)
		if d, ok := defs[ClipEventKey(ev.Name)]; ok {
			clip.Defs[ClipEventKey(ev.Name)] = d
		}
	}
	return clip, true
}

// TimelineOf finds one timeline of doc by name.
func TimelineOf(doc *whe.Document, name string) (whe.FxTimeline, bool) {
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

// TimelinePaste is what writing one copied timeline into a .whe did: whether the
// file had to grow it, how many rows landed, and what was left where it was.
type TimelinePaste struct {
	Name      string
	Created   bool
	Events    int
	Present   int
	Anims     int
	Skipped   int
	FirstSkip string
}

// TimelinePasteMessage is how writing a timeline back reads in the status line.
func TimelinePasteMessage(res TimelinePaste) string {
	msg := fmt.Sprintf("Pasted %d event(s) into %s", res.Events, res.Name)
	if res.Created {
		msg = fmt.Sprintf("Created timeline %s with %d event(s)", res.Name, res.Events)
	}
	if res.Anims > 0 {
		msg += fmt.Sprintf(", %d animation(s)", res.Anims)
	}
	if res.Present > 0 {
		msg += fmt.Sprintf(", %d already there", res.Present)
	}
	if res.Skipped > 0 {
		msg += fmt.Sprintf(", %d skipped: %s", res.Skipped, res.FirstSkip)
	}
	return msg + " — Save .whe to write it into the mod"
}

// FxEventSlot is what makes two rows of a timeline the same event: the name
// the game fires and the moment it fires at. The comparison is case
// insensitive for the name and exact for the time, the way the .whe stores
// them.
func FxEventSlot(ev whe.FxTimelineEvent) string {
	return ClipEventKey(ev.Name) + "|" + strconv.FormatFloat(ev.Time, 'g', -1, 64)
}

// UnitEventShift parses the batch shift field: seconds added to every selected
// row, negative to move them earlier. A typed plus sign is accepted the way a
// person writes a delta.
func UnitEventShift(text string) (float64, error) {
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

// UnitEventShiftLabel reads a shift back the way it was typed, with its sign.
func UnitEventShiftLabel(d float64) string {
	if d < 0 {
		return "-" + strconv.FormatFloat(-d, 'f', -1, 64) + "s"
	}
	return "+" + strconv.FormatFloat(d, 'f', -1, 64) + "s"
}

// UnitEventTimeLabel is how a moment reads in the event rows.
func UnitEventTimeLabel(t float64) string {
	if t < 0 {
		return "enter"
	}
	if math.IsInf(t, 1) || t > 1e30 {
		return "leave"
	}
	return strconv.FormatFloat(t, 'f', 3, 64) + "s"
}

// unitEventsMessage is the empty state of either column: why there is nothing
// to edit, in the same dimmed line the other sections use.
func UnitEventsMessage(ui *uistate.State, gtx layout.Context, msg string) layout.Dimensions {
	if msg == "" {
		msg = "There is nothing to edit here."
	}
	return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(10), msg)
		l.Color = uikit.CTextDim
		l.MaxLines = 6
		return l.Layout(gtx)
	})
}

// selectedUnitEvtAction is the action whose conditions the Events column draws:
// the one picked in the list under the timelines, kept inside that list when
// the file changes under it.
func SelectedUnitEvtAction(ui *uistate.State, acts []whe.FxAction) *whe.FxAction {
	if len(acts) == 0 {
		return nil
	}
	if ui.UnitEvtActionSel < 0 || ui.UnitEvtActionSel >= len(acts) {
		ui.UnitEvtActionSel = 0
	}
	return &acts[ui.UnitEvtActionSel]
}

func EnsureUnitEvtActionClicks(ui *uistate.State, n int) {
	if len(ui.UnitEvtActionClicks) != n {
		ui.UnitEvtActionClicks = make([]widget.Clickable, n)
	}
}

// copySelectedUnitAction puts the picked action and everything it needs to run
// into the shared buffer: the action itself, the timelines its motions name —
// followed through the motions those timelines chain to — and the sub-actions
// it calls. A unit's idle / build / death chain can then be set up once and
// pasted onto another unit in one go, instead of reassembling it from the
// timeline list by hand.
func CopySelectedUnitAction(ui *uistate.State) {
	doc := ui.UnitEvtDoc
	if doc == nil {
		ui.StatusText = "No .whe is loaded"
		return
	}
	acts := doc.Actions()
	a := SelectedUnitEvtAction(ui, acts)
	if a == nil {
		ui.StatusText = "Select an action to copy"
		return
	}
	clip := &cliphist.WheActionClip{Action: *a}
	for _, name := range ActionMotionNames(doc, *a) {
		if tl, ok := TimelineClipFrom(doc, name); ok {
			clip.Timelines = append(clip.Timelines, *tl)
		}
	}
	clip.Subacts = ActionSubactions(doc, *a)
	ui.UnitClips.Push(cliphist.ClipEntry{
		Kind: cliphist.ClipActions,
		Label: fmt.Sprintf("action %s (%d choice(s), %d timeline(s), %d sub-action(s))",
			a.Name, len(UnitActLinks(*a)), len(clip.Timelines), len(clip.Subacts)),
		At:      time.Now(),
		Actions: clip,
	})
	ui.UnitClipCur = len(ui.UnitClips.Entries) - 1
	ui.StatusText = fmt.Sprintf("Copied action %s with %d timeline(s) and %d sub-action(s) — Paste in any unit",
		a.Name, len(clip.Timelines), len(clip.Subacts))
}

// ActionMotionNames lists every timeline the action can reach: the motions its
// own choices name first, then the motions those timelines chain to, and so on
// through the chain — a motion timeline whose payload names another motion is
// only half copied without the one it points at. Names that this file has no
// timeline for are dropped, since there is nothing to carry.
func ActionMotionNames(doc *whe.Document, a whe.FxAction) []string {
	seen := map[string]bool{}
	var out []string
	var visit func(name string)
	visit = func(name string) {
		key := ClipEventKey(name)
		if key == "" || seen[key] {
			return
		}
		tl, ok := TimelineOf(doc, key)
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

// ActionSubactions lists the other actions of the file the action calls,
// followed through their own sub-action links. They travel with the copy for
// the same reason the timelines do: an action that calls a sub-action the
// target does not have is a broken chain.
func ActionSubactions(doc *whe.Document, a whe.FxAction) []whe.FxAction {
	byName := map[string]whe.FxAction{}
	for _, have := range doc.Actions() {
		byName[ClipEventKey(have.Name)] = have
	}
	seen := map[string]bool{ClipEventKey(a.Name): true}
	var out []whe.FxAction
	var visit func(name string)
	visit = func(name string) {
		key := ClipEventKey(name)
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

// ActionIndexByName is the position of an action in the file's DATAACTS list,
// or -1 when the file has no action of that name.
func ActionIndexByName(doc *whe.Document, name string) int {
	if doc == nil {
		return -1
	}
	key := ClipEventKey(name)
	for i, a := range doc.Actions() {
		if ClipEventKey(a.Name) == key {
			return i
		}
	}
	return -1
}

// UnitActLinks is one action's choices in file order: the motions it plays,
// then the actions it calls — the order the condition fields are sized and
// bound in.
func UnitActLinks(a whe.FxAction) []whe.FxActionLink {
	links := make([]whe.FxActionLink, 0, len(a.Motions)+len(a.Subactions))
	links = append(links, a.Motions...)
	links = append(links, a.Subactions...)
	return links
}

// UnitActLinkAt is the editable copy of one of those choices: an index into
// the motions, then into the sub-actions.
func UnitActLinkAt(a *whe.FxAction, i int) *whe.FxActionLink {
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

// UnitActGuardLabel is what a guard chip reads for a step of the chain.
func UnitActGuardLabel(cmp int32) string {
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
func EnsureUnitActConditions(ui *uistate.State) {
	if ui.UnitEvtDoc == nil {
		ui.UnitActCondEds = nil
		ui.UnitActNameEds = nil
		ui.UnitActCondStaged = nil
		ui.UnitActGuardClicks = nil
		ui.UnitActCondBound = ""
		return
	}
	a := SelectedUnitEvtAction(ui, ui.UnitEvtDoc.Actions())
	if a == nil {
		ui.UnitActCondEds = nil
		ui.UnitActNameEds = nil
		ui.UnitActCondStaged = nil
		ui.UnitActGuardClicks = nil
		ui.UnitActCondBound = ""
		return
	}
	links := UnitActLinks(*a)
	if len(ui.UnitActCondEds) != len(links) || len(ui.UnitActGuardClicks) != len(links) || len(ui.UnitActNameEds) != len(links) {
		ui.UnitActCondEds = make([]widget.Editor, len(links))
		ui.UnitActNameEds = make([]widget.Editor, len(links))
		ui.UnitActCondStaged = make([]int32, len(links))
		ui.UnitActGuardClicks = make([]widget.Clickable, len(links))
		ui.UnitActCondBound = ""
	}
	bound := strconv.Itoa(ui.UnitEvtActionSel) + "|" + a.Name
	for _, ln := range links {
		bound += "|" + ln.Name
	}
	if ui.UnitActCondBound == bound {
		if ui.UnitActSel >= len(links) {
			ui.UnitActSel = 0
		}
		return
	}
	ui.UnitActCondBound = bound
	if ui.UnitActSel >= len(links) {
		ui.UnitActSel = 0
	}
	for i, ln := range links {
		ui.UnitActCondStaged[i] = ln.Compare
		ui.UnitActCondEds[i].SetText(ln.Condition)
		ui.UnitActNameEds[i].SetText(ln.Name)
	}
}

// applyUnitActConditions writes the condition fields and their guards into
// the selected action of the in-memory document — the DATAACTS payload is
// rebuilt, the document is marked dirty, and the fields refill from what was
// written so what is shown is what the document holds. Nothing touches the
// file: Save .whe does that.
func ApplyUnitActConditions(ui *uistate.State) {
	if ui.UnitEvtDoc == nil {
		ui.UnitActErr = "No .whe is loaded."
		return
	}
	acts := ui.UnitEvtDoc.Actions()
	if ui.UnitEvtActionSel < 0 || ui.UnitEvtActionSel >= len(acts) {
		ui.UnitActErr = "Select an action to apply the conditions to."
		return
	}
	a := acts[ui.UnitEvtActionSel]
	links := UnitActLinks(a)
	if len(links) == 0 {
		// Nothing to flush — an action with no choices yet only waits for
		// its first Add.
		ui.UnitActErr = ""
		return
	}
	if len(ui.UnitActCondEds) != len(links) || len(ui.UnitActCondStaged) != len(links) {
		ui.UnitActErr = "The condition fields are out of step with the file — pick the action again."
		return
	}
	changed := false
	for i, ln := range links {
		cmp := ui.UnitActCondStaged[i]
		cond := ""
		if (whe.FxActionLink{Compare: cmp}).HasCondition() {
			cond = strings.TrimSpace(ui.UnitActCondEds[i].Text())
		}
		name := ln.Name
		if i < len(ui.UnitActNameEds) {
			name = strings.TrimSpace(ui.UnitActNameEds[i].Text())
		}
		if name == "" {
			ui.UnitActErr = "Every choice must name the motion or action it runs — fill in the name of line " + strconv.Itoa(i+1) + "."
			return
		}
		if ln.Compare == cmp && ln.Condition == cond && ln.Name == name {
			continue
		}
		if dst := UnitActLinkAt(&a, i); dst != nil {
			dst.Compare = cmp
			dst.Condition = cond
			dst.Name = name
			changed = true
		}
	}
	if !changed {
		ui.UnitActErr = ""
		ui.StatusText = "no changes to apply"
		return
	}
	if err := ui.UnitEvtDoc.SetAction(ui.UnitEvtActionSel, a); err != nil {
		ui.UnitActErr = err.Error()
		return
	}
	ui.UnitActErr = ""
	ui.UnitEvtDirty = true
	// The fields refill from what was just written.
	ui.UnitActCondBound = ""
	EnsureUnitActConditions(ui)
	ui.StatusText = "Applied conditions to " + a.Name + " — Save .whe to write it into the mod"
}

const (
	ActAddIf = iota
	ActAddElseIf
	ActAddElse
)

// ActIsHead reports whether a link starts a chain rather than continuing one.
func ActIsHead(ln whe.FxActionLink) bool {
	return ln.Compare != whe.FxCompareElseIf && ln.Compare != whe.FxCompareElse
}

func ActChainStart(list []whe.FxActionLink, i int) int {
	for i > 0 && !ActIsHead(list[i]) {
		i--
	}
	return i
}

func ActChainEnd(list []whe.FxActionLink, i int) int {
	e := ActChainStart(list, i)
	for e+1 < len(list) && !ActIsHead(list[e+1]) {
		e++
	}
	return e
}

// ActLocate maps a combined row index onto the list it lives in and that
// list's own index — the motions come first, then the sub-actions.
func ActLocate(a whe.FxAction, i int) (list []whe.FxActionLink, local int, motion, ok bool) {
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

// ActCombined maps a list index back onto the combined row index.
func ActCombined(a whe.FxAction, motion bool, local int) int {
	if motion {
		return local
	}
	return len(a.Motions) + local
}

// ActMoveLink slides one choice within its list — dir is -1 up, +1 down. A
// chain head swaps places with the neighbouring whole chain, so moving it
// never breaks the continuations it carries; an else if slides between the
// continuations of its own chain; the else that ends a chain stays pinned.
func ActMoveLink(list []whe.FxActionLink, i, dir int) ([]whe.FxActionLink, int, bool) {
	if i < 0 || i >= len(list) || (dir != -1 && dir != 1) {
		return list, i, false
	}
	if ActIsHead(list[i]) {
		s := ActChainStart(list, i)
		e := ActChainEnd(list, i)
		if dir < 0 {
			if s == 0 {
				return list, i, false
			}
			ps := ActChainStart(list, s-1)
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
		ne := ActChainEnd(list, ns)
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
	s := ActChainStart(list, i)
	e := ActChainEnd(list, i)
	j := i + dir
	if j < s || j > e || ActIsHead(list[j]) || list[j].Compare == whe.FxCompareElse {
		return list, i, false
	}
	out := append([]whe.FxActionLink(nil), list...)
	out[i], out[j] = out[j], out[i]
	return out, j, true
}

// ActAddLink inserts a new choice into the list the selected row lives in: a
// new if starts a fresh chain at the end of that list, while an else if /
// else continues the selected row's chain right after it and takes over the
// head's target name — the usual reason to guard the same motion twice.
func ActAddLink(a *whe.FxAction, kind, at int) (int, bool) {
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
	case ActAddIf:
		ln.Compare = whe.FxCompareIf
		list = append(list, ln)
		newAt := ActCombined(*a, motion, len(list)-1)
		if motion {
			a.Motions = list
		} else {
			a.Subactions = list
		}
		return newAt, true
	case ActAddElseIf, ActAddElse:
		if len(list) == 0 || local < 0 || local >= len(list) {
			return at, false
		}
		e := ActChainEnd(list, local)
		insert := e + 1
		if kind == ActAddElse {
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
		ln.Name = list[ActChainStart(list, local)].Name
		out := make([]whe.FxActionLink, 0, len(list)+1)
		out = append(out, list[:insert]...)
		out = append(out, ln)
		out = append(out, list[insert:]...)
		newAt := ActCombined(*a, motion, insert)
		if motion {
			a.Motions = out
		} else {
			a.Subactions = out
		}
		return newAt, true
	}
	return at, false
}

// ActDeleteLink removes one choice: deleting a chain head takes its whole
// chain with it, deleting an else if / else removes only that step.
func ActDeleteLink(a *whe.FxAction, at int) (int, bool) {
	list, j, motion, ok := ActLocate(*a, at)
	if !ok {
		return at, false
	}
	end := j
	if ActIsHead(list[j]) {
		end = ActChainEnd(list, j)
	}
	out := make([]whe.FxActionLink, 0, len(list)-(end-j))
	out = append(out, list[:j]...)
	out = append(out, list[end+1:]...)
	if motion {
		a.Motions = out
	} else {
		a.Subactions = out
	}
	newAt := ActCombined(*a, motion, j)
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
func UnitActMutate(ui *uistate.State, what string, change func(a *whe.FxAction) (int, bool)) {
	if ui.UnitEvtDoc == nil {
		ui.UnitActErr = "No .whe is loaded."
		return
	}
	// The typed fields go in first: a structural change refills them from
	// the document, and unsaved typing would be dropped under it.
	ApplyUnitActConditions(ui)
	if ui.UnitActErr != "" {
		return
	}
	acts := ui.UnitEvtDoc.Actions()
	if ui.UnitEvtActionSel < 0 || ui.UnitEvtActionSel >= len(acts) {
		ui.UnitActErr = "Select an action first."
		return
	}
	a := acts[ui.UnitEvtActionSel]
	if rows := len(a.Motions) + len(a.Subactions); rows > 0 && (ui.UnitActSel < 0 || ui.UnitActSel >= rows) {
		ui.UnitActSel = 0
	}
	newAt, ok := change(&a)
	if !ok {
		ui.UnitActErr = what + " is not possible here."
		return
	}
	if err := ui.UnitEvtDoc.SetAction(ui.UnitEvtActionSel, a); err != nil {
		ui.UnitActErr = err.Error()
		return
	}
	ui.UnitActErr = ""
	ui.UnitEvtDirty = true
	ui.UnitActCondBound = ""
	EnsureUnitActConditions(ui)
	ui.UnitActSel = newAt
	ui.StatusText = what + " — Save .whe to write it into the mod"
}

func UnitActAddChoice(ui *uistate.State, kind int) {
	what := "Added if"
	switch kind {
	case ActAddElseIf:
		what = "Added else if"
	case ActAddElse:
		what = "Added else"
	}
	UnitActMutate(ui, what, func(a *whe.FxAction) (int, bool) {
		return ActAddLink(a, kind, ui.UnitActSel)
	})
}

func UnitActMoveChoice(ui *uistate.State, dir int) {
	what := "Moved down"
	if dir < 0 {
		what = "Moved up"
	}
	UnitActMutate(ui, what, func(a *whe.FxAction) (int, bool) {
		list, j, motion, ok := ActLocate(*a, ui.UnitActSel)
		if !ok {
			return ui.UnitActSel, false
		}
		moved, nj, ok := ActMoveLink(list, j, dir)
		if !ok {
			return ui.UnitActSel, false
		}
		if motion {
			a.Motions = moved
		} else {
			a.Subactions = moved
		}
		return ActCombined(*a, motion, nj), true
	})
}

func UnitActDeleteChoice(ui *uistate.State) {
	UnitActMutate(ui, "Deleted", func(a *whe.FxAction) (int, bool) {
		return ActDeleteLink(a, ui.UnitActSel)
	})
}

// unitActMoveEnabled reports whether the move buttons act: the selected
// choice has a neighbour of the kind it may swap with.
func UnitActMoveEnabled(ui *uistate.State, dir int) bool {
	if ui.UnitEvtDoc == nil {
		return false
	}
	a := SelectedUnitEvtAction(ui, ui.UnitEvtDoc.Actions())
	if a == nil {
		return false
	}
	list, j, _, ok := ActLocate(*a, ui.UnitActSel)
	if !ok {
		return false
	}
	_, _, ok = ActMoveLink(list, j, dir)
	return ok
}

// unitActionConditions is one action's choices as a chain of one-line steps:
// each choice reads as "<guard> <condition> → <motion or action>", the
// guard chip is pressed to cycle if → else if → else → always, and the line
// is committed with Apply and written with Save .whe like every other edit
// of this section.
func UnitActionConditions(ui *uistate.State, gtx layout.Context, a whe.FxAction) layout.Dimensions {
	EnsureUnitActConditions(ui)
	if len(a.Motions)+len(a.Subactions) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This action carries no motion and calls no action.")
		l.Color = uikit.CTextDim
		return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var rows []layout.FlexChild
	for i, m := range a.Motions {
		i, ln := i, m
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutUnitActConditionRow(ui, gtx, i, ln)
		}))
	}
	for i, s := range a.Subactions {
		i, ln := len(a.Motions)+i, s
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutUnitActConditionRow(ui, gtx, i, ln)
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
func LayoutUnitActConditionRow(ui *uistate.State, gtx layout.Context, i int, ln whe.FxActionLink) layout.Dimensions {
	if i < 0 || i >= len(ui.UnitActCondEds) || i >= len(ui.UnitActCondStaged) || i >= len(ui.UnitActGuardClicks) || i >= len(ui.UnitActNameEds) {
		return layout.Dimensions{}
	}
	cmp := ui.UnitActCondStaged[i]
	hasCond := (whe.FxActionLink{Compare: cmp}).HasCondition()
	indent := unit.Dp(0)
	switch cmp {
	case whe.FxCompareElseIf, whe.FxCompareElse:
		indent = unit.Dp(14)
	}
	line := func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitActGuardChip(ui, gtx, i, UnitActGuardLabel(cmp))
			}),
		}
		if hasCond {
			children = append(children,
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return UnitActFieldEditor(ui, gtx, &ui.UnitActCondEds[i], "condition")
				}),
			)
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "\u2192")
				l.Color = uikit.CTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Flexed(2, func(gtx layout.Context) layout.Dimensions {
				return UnitActFieldEditor(ui, gtx, &ui.UnitActNameEds[i], "motion or action")
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
	measureGTX.Constraints.Max.Y = uikit.PanelScrollMeasureHeight
	h := line(measureGTX).Size.Y
	macro.Stop()
	if h <= 0 || h >= uikit.PanelScrollMeasureHeight {
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
	bg := uikit.CTabOff
	if i == ui.UnitActSel {
		bg = uikit.CSelectBg
	}
	return layout.Inset{Left: indent, Bottom: unit.Dp(4)}.Layout(rowGTX, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				uikit.Rrect(gtx, bg, sz, gtx.Dp(3))
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
func UnitActFieldEditor(ui *uistate.State, gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	ed.SingleLine = true
	ed.Submit = true
	gtx.Constraints.Min.Y = gtx.Dp(36)
	gtx.Constraints.Max.Y = gtx.Dp(40)
	b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			w := material.Editor(ui.Th, ed, hint)
			w.TextSize = unit.Sp(10)
			w.HintColor = uikit.CTextDim
			return w.Layout(gtx)
		})
	})
}

// unitActGuardChip is the operator of one choice line, sized from its own
// label rather than from the room the row has left. smallButton reads the
// ambient constraints for its size, and inside a card row those numbers
// belong to the row, not to the chip — the chip must carry its own width so
// the arrow and the fields after it are not pushed off the line.
func UnitActGuardChip(ui *uistate.State, gtx layout.Context, i int, label string) layout.Dimensions {
	if i < 0 || i >= len(ui.UnitActGuardClicks) {
		return layout.Dimensions{}
	}
	c := &ui.UnitActGuardClicks[i]
	// Measure the label on its own; the discarded pass draws nothing.
	macro := op.Record(gtx.Ops)
	lgtx := gtx.Disabled()
	lgtx.Constraints.Min = image.Point{}
	l := material.Label(ui.Th, unit.Sp(11), label)
	l.Color = uikit.CText
	ldims := l.Layout(lgtx)
	macro.Stop()
	padX, padY := unit.Dp(10), unit.Dp(5)
	inner := image.Pt(ldims.Size.X+2*gtx.Dp(padX), ldims.Size.Y+2*gtx.Dp(padY))
	if inner.Y < gtx.Dp(unit.Dp(30)) {
		inner.Y = gtx.Dp(unit.Dp(30))
	}
	bg := uikit.CSaveBg
	switch {
	case c.Pressed():
		bg = uikit.CMenuOpen
	case c.Hovered():
		bg = uikit.CSaveHover
	}
	return widget.Border{Color: uikit.CSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Exact constraints inside: nothing the row hands down can leak
		// into the chip's own size or into where its label is drawn.
		gtx.Constraints.Min = inner
		gtx.Constraints.Max = inner
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				uikit.Rrect(gtx, bg, sz, gtx.Dp(3))
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
				l := material.Label(ui.Th, unit.Sp(11), label)
				l.Color = uikit.CText
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

// ensureUnitEvtEntryEditors sizes the value fields to the selected event's
// definition. Fields are only ever added: the panel draws one row per entry,
// and the ones past the end sit unused rather than the values being cut off.
func EnsureUnitEvtEntryEditors(ui *uistate.State, n int) {
	for len(ui.UnitEvtEntryEds) < n {
		ui.UnitEvtEntryEds = append(ui.UnitEvtEntryEds, widget.Editor{SingleLine: true, Submit: true})
		ui.UnitEvtEntryFillText = append(ui.UnitEvtEntryFillText, "")
	}
}

// syncUnitEvtEntryEditors fills the value fields when another definition is
// selected — whatever the cursor is doing, the fields must show the event
// that is now picked. A value changed by Enter is refilled by the apply
// itself, so this only has to notice the selection.
func SyncUnitEvtEntryEditors(ui *uistate.State, name string, def whe.FxEvent) {
	bound := strings.ToLower(name) + "#" + strconv.Itoa(len(def.Entries))
	if bound == ui.UnitEvtEntryBound {
		return
	}
	ui.UnitEvtEntryBound = bound
	for i := range def.Entries {
		if i < len(ui.UnitEvtEntryEds) {
			ui.UnitEvtEntryEds[i].SetText(def.Entries[i].Value)
			if i < len(ui.UnitEvtEntryFillText) {
				ui.UnitEvtEntryFillText[i] = strings.TrimSpace(def.Entries[i].Value)
			}
		}
	}
}

// unitEvtEntryRow is one row of the selected event's definition: the entry's
// key, then its value in a field that submits on Enter. The key is clipped so
// a long name cannot push the value off the panel.
func UnitEvtEntryRow(ui *uistate.State, gtx layout.Context, i int, key string) layout.Dimensions {
	if i < 0 || i >= len(ui.UnitEvtEntryEds) {
		return layout.Dimensions{}
	}
	ed := &ui.UnitEvtEntryEds[i]
	ed.SingleLine = true
	ed.Submit = true
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = gtx.Dp(112)
			l := material.Label(ui.Th, unit.Sp(9), key)
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(20)
			gtx.Constraints.Max.Y = gtx.Dp(22)
			b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
			return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := material.Editor(ui.Th, ed, "")
					w.TextSize = unit.Sp(10)
					w.HintColor = uikit.CTextDim
					return w.Layout(gtx)
				})
			})
		}),
	)
}

// UnitEventKindTag says what an event does, from its definition: a sound, an
// effect at a marker, an effect with no marker of its own, or nothing at all.
func UnitEventKindTag(name string, defs map[string]whe.FxEvent) string {
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

type EvtPickTarget = uistate.EvtPickTarget

const (
	EvtPickNone EvtPickTarget = iota
	EvtPickName
	EvtPickFX
	EvtPickSound
	EvtPickMarker
)

// EvtPickerMaxHits caps the rows the picker draws, so a folder of hundreds of
// effects does not lay out hundreds of rows a frame.
const EvtPickerMaxHits = 300

type EvtPicker = uistate.EvtPicker

// openEvtPicker shows the list for one field, with every value of it ready.
func OpenEvtPicker(ui *uistate.State, t EvtPickTarget) {
	p := &ui.EvtPick
	p.Open = true
	p.Target = t
	p.All = EvtPickerItems(ui, t)
	p.Ed.SingleLine = true
	p.Ed.Submit = true
	p.Ed.SetText("")
	p.Filter = ""
	RefreshEvtPickerHits(ui)
}

// evtPickerItems lists the values a target can take, sorted and without
// repeats.
func EvtPickerItems(ui *uistate.State, t EvtPickTarget) []string {
	var out []string
	switch t {
	case EvtPickName:
		if ui.UnitEvtDoc != nil {
			for _, def := range ui.UnitEvtDoc.Events() {
				out = append(out, def.Name)
			}
		}
	case EvtPickMarker:
		if v := UnitModelViewer(ui); v != nil && v.Model != nil {
			for _, m := range v.Model.Markers {
				out = append(out, m.Name)
			}
		}
	case EvtPickFX:
		out = EvtTreeFolderFiles(ui.ModTree, `\art\fx\`, []string{".lua"}, true)
	case EvtPickSound:
		out = EvtTreeFolderFiles(ui.ModTree, `\sound\`, []string{".fda", ".wav"}, false)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	uniq := out[:0]
	for i, s := range out {
		if i == 0 || !strings.EqualFold(s, uniq[len(uniq)-1]) {
			uniq = append(uniq, s)
		}
	}
	return uniq
}

// EvtTreeFolderFiles lists the files of the mod tree that sit under one folder
// of the game's data and carry one of the extensions. The names are relative
// to that folder, backslash separated the way a .whe writes them. keepExt says
// whether the extension stays on the name: an effect's does, a sound's does
// not, because a .whe names a sound without one.
func EvtTreeFolderFiles(tree *modtree.ModTree, folder string, exts []string, keepExt bool) []string {
	if tree == nil {
		return nil
	}
	var out []string
	lowerFolder := strings.ToLower(folder)
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if f == nil {
			return
		}
		full := strings.ReplaceAll(f.FullPath, "/", `\`)
		i := strings.Index(strings.ToLower(full), lowerFolder)
		if i < 0 {
			return
		}
		rel := full[i+len(folder):]
		ext := ""
		if k := strings.LastIndexByte(rel, '.'); k >= 0 && !strings.ContainsRune(rel[k:], '\\') {
			ext = strings.ToLower(rel[k:])
		}
		matched := false
		for _, e := range exts {
			if ext == e {
				matched = true
				break
			}
		}
		if !matched {
			return
		}
		if !keepExt {
			rel = rel[:len(rel)-len(ext)]
		}
		out = append(out, rel)
	})
	return out
}

// refreshEvtPickerHits filters the list by what is in the filter box.
func RefreshEvtPickerHits(ui *uistate.State) {
	p := &ui.EvtPick
	q := strings.ToLower(strings.TrimSpace(p.Filter))
	p.Hits = p.Hits[:0]
	for _, s := range p.All {
		if q == "" || strings.Contains(strings.ToLower(s), q) {
			p.Hits = append(p.Hits, s)
			if len(p.Hits) >= EvtPickerMaxHits {
				break
			}
		}
	}
	p.Clicks = make([]widget.Clickable, len(p.Hits))
}

// applyEvtPick writes the chosen value into the field it was opened from and
// closes the picker. A chosen event name also fills in the FX, sound and marker
// its definition holds, so the form shows what picking it will fire.
func ApplyEvtPick(ui *uistate.State, t EvtPickTarget, value string) {
	value = strings.TrimSpace(value)
	ui.EvtPick.Open = false
	if value == "" {
		return
	}
	switch t {
	case EvtPickName:
		ui.UnitEvtNameEd.SetText(value)
		ui.UnitEvtFxEd.SetText("")
		ui.UnitEvtSndEd.SetText("")
		ui.UnitEvtMarkerEd.SetText("")
		if ui.UnitEvtDoc != nil {
			if def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(value)]; ok {
				if p, has := def.First("sound"); has {
					ui.UnitEvtSndEd.SetText(p)
				} else if p, has := def.First("fx"); has {
					ui.UnitEvtFxEd.SetText(p)
				}
				if m, has := def.First("marker"); has {
					ui.UnitEvtMarkerEd.SetText(m)
				}
			}
		}
	case EvtPickFX:
		ui.UnitEvtFxEd.SetText(value)
	case EvtPickSound:
		ui.UnitEvtSndEd.SetText(value)
	case EvtPickMarker:
		ui.UnitEvtMarkerEd.SetText(value)
	}
}

// EvtPickerTitle is what the picker's card says it is choosing.
func EvtPickerTitle(t EvtPickTarget) string {
	switch t {
	case EvtPickName:
		return "Choose an event"
	case EvtPickFX:
		return "Choose an FX"
	case EvtPickSound:
		return "Choose a sound"
	case EvtPickMarker:
		return "Choose a model marker"
	}
	return "Choose"
}

type UnitEvtFire = uistate.UnitEvtFire

type UnitEvtPlayer = uistate.UnitEvtPlayer

// evtPlayerStopAll is the section's exit hook: it only touches anything when
// something is actually sounding, so the per-frame call is free.
func EvtPlayerStopAll(ui *uistate.State) {
	ui.EvtPlayer.StopAll()
}

// UnitEvtStartVoice hands one decoded sample to a fresh oto player, the same
// way the 3D preview's player does: one player per sound so two overlap
// instead of the second cutting the first off.
func UnitEvtStartVoice(s *fx.SoundSample) (*fx.SoundVoice, error) {
	if s == nil || s.Info == nil || len(s.PCM) == 0 {
		return nil, fmt.Errorf("empty sample")
	}
	ctx, err := audioplayer.Context()
	if err != nil {
		return nil, err
	}
	cb := audioplayer.NewChunkBuffer(fda.PCM16Bytes(s.PCM), s.Info.SampleRate, s.Info.Channels)
	pl := ctx.NewPlayer(audioplayer.NewChunkSource(cb))
	pl.SetVolume(1)
	pl.Play()
	return fx.NewSoundVoice(pl, s), nil
}

// unitEvtSelectedIsSound says whether the selected event plays audio, which is
// what the Play sound button acts on and lights for.
func UnitEvtSelectedIsSound(ui *uistate.State) bool {
	if ui.UnitEvtDoc == nil {
		return false
	}
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events) {
		return false
	}
	def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(tl.Events[ui.UnitEvtSel].Name)]
	if !ok {
		return false
	}
	_, isSound := def.First("sound")
	return isSound
}

// unitEvtTimelinePlay starts the selected timeline from the beginning: every
// sound its events name, sorted by moment, the entry sentinel at zero and the
// exit sentinel at the last timed event, which is as close to "on leaving" as
// a timeline with no declared length can get.
func UnitEvtTimelinePlay(ui *uistate.State, gtx layout.Context) {
	tl := SelectedUnitEventTimeline(ui)
	p := &ui.EvtPlayer
	if tl == nil || ui.UnitEvtDoc == nil {
		ui.StatusText = "Select a timeline first"
		return
	}
	defs := ui.UnitEvtDoc.Events()
	var order []UnitEvtFire
	var duration float64
	for _, ev := range tl.Events {
		def, ok := defs[strings.ToLower(ev.Name)]
		if !ok {
			continue
		}
		path, isSound := def.First("sound")
		if !isSound || strings.TrimSpace(path) == "" {
			continue
		}
		at := ev.Time
		if at >= 0 && at <= 1e30 && at > duration {
			duration = at
		}
		order = append(order, UnitEvtFire{At: at, Name: ev.Name, Path: strings.TrimSpace(path)})
	}
	if len(order) == 0 {
		ui.StatusText = "No sounds fire on " + tl.Name
		return
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].At < order[j].At })
	// After the sort, so entry events stay ahead of everything timed at zero
	// and exit events behind everything else.
	for i := range order {
		switch {
		case order[i].At < 0:
			order[i].At = 0
		case order[i].At > 1e30:
			order[i].At = duration
		}
	}
	p.StopVoices()
	p.Order = order
	p.Duration = duration
	p.Pos = 0
	p.Next = 0
	p.Playing = true
	p.Paused = false
	p.ForTimeline = tl.Name
	p.ForDoc = ui.UnitEvtDoc
	p.Last = gtx.Now
	if p.Last.IsZero() {
		p.Last = time.Now()
	}
	ui.StatusText = fmt.Sprintf("Playing %s: %d sounds over %.2fs", tl.Name, len(order), duration)
}

func UnitEvtFillRect(gtx layout.Context, r image.Rectangle, col color.NRGBA) {
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
}
