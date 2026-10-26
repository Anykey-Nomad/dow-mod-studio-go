package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ── the Object tab ──────────────────────────────────────────────────────────
//
// The object interface used to be a modal window over the whole app. It is a
// tab now: the Object button in the menu bar opens it, it sits in the tab bar
// beside the file tabs, and the standard right panel stays where it was.
//
// While it is active the content area gives up its file tree and its middle
// panel: the Object's three columns take the space of the left and middle
// panels, which is where they are widest and where nothing else is wanted. The
// right panel is untouched, so the usual properties panel is still on screen.

// unitTabKind is the fileTab kind of the object interface. It is a tab kind
// rather than a tab of its own type so that it rides the existing tab bar,
// selection, closing and click handling without a second mechanism.
const unitTabKind = "unit"

// unitTabLabel is what the tab calls itself in the bar.
const unitTabLabel = "Object"

// unitTabPath is the virtual path recorded for the tab. It is not a real file:
// the tab is not backed by one, and the empty path is what marks it as such,
// so session saving skips it and a mod reload leaves it alone.
const unitTabPath = ""

// isUnitTab reports whether a tab is the object interface.
func isUnitTab(t *fileTab) bool {
	return t != nil && t.Kind == unitTabKind
}

// unitTabIndex is where the object tab sits in the tab list, or -1 when it is
// not open.
func UnitTabIndex(ui *uiState) int {
	for i, t := range ui.OpenTabs {
		if isUnitTab(t) {
			return i
		}
	}
	return -1
}

// unitTabActive reports whether the object interface is the tab on screen.
func UnitTabActive(ui *uiState) bool {
	return isUnitTab(ActiveTab(ui))
}

// openUnitTab shows the object interface, opening its tab the first time. It is
// a singleton: asking for it again just brings the existing tab forward, so the
// user cannot end up with two copies browsing the same units.
func OpenUnitTab(ui *uiState) {
	if idx := UnitTabIndex(ui); idx >= 0 {
		ui.TabSel = idx + 1
		EnsureTabClicks(ui)
		return
	}
	ui.OpenTabs = append(ui.OpenTabs, &fileTab{Kind: unitTabKind, Path: unitTabPath})
	ui.TabSel = len(ui.OpenTabs)
	EnsureTabClicks(ui)
}

// closeUnitTab removes the object tab if it is open. Closing it leaves the
// selection alone, so reopening it lands on the same unit.
func CloseUnitTab(ui *uiState) {
	idx := UnitTabIndex(ui)
	if idx < 0 {
		// The tab is already gone, but a dropdown it left open would still be
		// drawn over whatever replaced it, so the state is cleared either way.
		ui.UnitRaceOpen = false
		ui.UnitListOpen = false
		return
	}
	CloseTabAt(ui, idx)
	// An open dropdown belongs to the tab that went away.
	ui.UnitRaceOpen = false
	ui.UnitListOpen = false
}

// ── the body ────────────────────────────────────────────────────────────────

// layoutUnitTabBody is the object interface as the content area's middle: the
// picker strip above the three columns.
//
// The order of the layers is what makes an open list usable. The columns come
// first and the strip and its lists last, so a list taller than the strip paints
// over the columns instead of being painted over by them — a strip laid out as
// a row above the columns would be drawn first, and the columns would then
// cover everything the list hangs below it.
func LayoutUnitTabBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	stripH := gtx.Dp(unit.Dp(unitPickerStripHeightDp))
	pad := gtx.Dp(unit.Dp(unitTabPadDp))
	return layout.Stack{}.Layout(gtx,
		// The columns, with the strip's height kept clear at the top.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(stripH + pad), Left: unit.Dp(pad), Right: unit.Dp(pad), Bottom: unit.Dp(pad)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutUnitBody(ui, gtx) })
		}),
		// While a list is open, a transparent catcher over the columns: a click
		// that misses the strip and the list lands here and puts the list away.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !UnitPickerOpen(ui) {
				return layout.Dimensions{}
			}
			pass := pointer.PassOp{}.Push(gtx.Ops)
			dims := ui.UnitDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			pass.Pop()
			return dims
		}),
		// The strip, and any open list hanging below it, on top of both.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			LayoutUnitPickerLayer(ui, gtx, stripH, pad)
			// The layer reports the whole area, so the columns are laid out at
			// full size and the strip is free to overhang them.
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
	)
}

// layoutUnitPickerLayer draws the picker strip at the top of its layer and then
// draws whichever list is open underneath it, offset to sit under the field the
// list belongs to.
//
// The two trigger fields are measured first, against a throwaway op stream, so
// the list can be lined up with its own field. The fields carry no clickable of
// their own — the clickable is wrapped around them at the call site — so
// measuring them here neither draws anything twice nor counts a click twice.
func LayoutUnitPickerLayer(ui *uiState, gtx layout.Context, stripH, pad int) {
	measure := func(label, value string) int {
		var scratch op.Ops
		mg := gtx
		mg.Ops = &scratch
		mg.Constraints.Min = image.Pt(0, stripH)
		mg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
		return UnitPickerField(ui, mg, label, value, true, false).Size.X
	}
	// How far the unit field sits from the strip's own left edge. The list is
	// drawn at the same offset, so it hangs under the field that opened it.
	unitX := measure("Race", PickerRaceLabel(ui)) + gtx.Dp(unit.Dp(unitColGapDp))

	// The strip itself, at the top of the layer with the usual padding.
	sg := gtx
	sg.Constraints.Min = image.Pt(0, stripH)
	sg.Constraints.Max = image.Pt(gtx.Constraints.Max.X, stripH)
	padOff := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
	LayoutUnitPickerRow(ui, sg)
	padOff.Pop()

	if !UnitPickerOpen(ui) {
		ui.UnitDropBox = image.Rectangle{}
		return
	}
	// The list hangs below the strip, over the columns. Its height is not taken
	// from the layer, so it can be as tall as it needs.
	lg := gtx
	lg.Constraints.Min = image.Pt(0, 0)
	lg.Constraints.Max = image.Pt(gtx.Constraints.Max.X-pad, gtx.Constraints.Max.Y-stripH)
	listOff := op.Offset(image.Pt(pad, stripH)).Push(gtx.Ops)
	offset := unitX
	if ui.UnitRaceOpen {
		offset = 0
	}
	dims := LayoutUnitDropdown(ui, lg, offset)
	listOff.Pop()
	// Recorded for tests, which otherwise have no way to know where the list
	// landed and end up probing the columns beside it.
	ui.UnitDropBox = image.Rect(pad+offset, stripH, pad+offset+dims.Size.X, stripH+dims.Size.Y)
}

// ── the content area ────────────────────────────────────────────────────────

// unitTabOwnsLeftPanel reports whether the object tab is drawn over the area
// the file tree normally occupies. It does, since the tree has nothing to say
// about browsing units and the section menu is the left column instead.
func UnitTabOwnsLeftPanel(ui *uiState) bool { return UnitTabActive(ui) }

// unitTabOwnsMiddlePanel is the same question for the middle panel, and the
// answer matches: the object columns take that space too.
func UnitTabOwnsMiddlePanel(ui *uiState) bool { return UnitTabActive(ui) }

// unitInfoTable is the object tab's right hand panel: what the selected unit
// is and where it came from. It deliberately repeats only the identifying rows
// — the race, the kind and the file — because the columns already carry the
// unit's properties, and a panel that listed them again would say nothing new.
func UnitInfoTable(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.Th, "No unit selected", unit.Sp(10), uikit.CTextDim)
			}),
		)
	}
	rows := []struct{ label, value string }{
		{"Name", d.Entry.Name},
		{"Race", d.Entry.Race},
		{"Kind", d.Entry.Kind.Label},
		{"File", d.Entry.Path},
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
	}
	for i, r := range rows {
		r := r
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, r.label, r.value, i%2 == 1)
		}))
	}
	// The screen name is the one row here that is a localisation rather than a
	// fact about the file, so it is drawn as an editable one: the text, the id
	// on hover, and the presses that change it. A unit with no screen name still
	// gets the row, because "this unit has no name yet" is the thing a reader
	// most wants to be able to fix from here.
	screenNameID := d.ScreenNameID()
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return LocalePanelRow(ui, gtx, "Screen name", screenNameID, localeFieldForID(d, screenNameID))
	}))
	if d.Error != "" {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "Could not read this unit:\n"+d.Error)
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitTabLabelFor is the tab bar's name for a tab of the object kind, kept apart
// so the bar's label chain does not grow another kind case.
func unitTabLabelFor(t *fileTab) string {
	if isUnitTab(t) {
		return unitTabLabel
	}
	return ""
}

// unitTabHasNoFile reports that the object tab is not backed by a file, which
// is what the mod reload check and the session writer need to know: a tab with
// no file must not be closed when a file disappears, and must not be persisted
// as a path.
func unitTabHasNoFile(t *fileTab) bool {
	return isUnitTab(t) || strings.TrimSpace(t.Path) == ""
}

// ─── merged from unit_names.go ───────────────────────────────────────────────

// The unit lists name a unit the way the game does on screen, which is the
// text behind ui_ext\ui_info\screen_name_id, and fall back to the .rgd file
// name when there is no localisation for it.
//
// Getting that text means parsing every unit's .rgd, and the DXP3 tree has 582
// of them: measured at roughly 300ms for one race's 60 units, which is a very
// visible stall to pay while a dropdown is opening. So the ids are read in the
// background as soon as the catalog exists and arrive in small batches, and a
// row shows the file name until its own text lands.
//
// Only the id is cached, never the text. The text is read on the UI thread
// through the locale table, so changing the language relabels every unit that
// has already been scanned without reading a single .rgd again.

// unitNameBatch is a slice of the scan's results. done marks the last one.
type unitNameBatch = uistate.UnitNameBatch

// unitNameBatchSize is how many units one batch carries. Small enough that the
// first names appear in tens of milliseconds rather than after a whole race,
// large enough that the channel traffic is not what costs.
const unitNameBatchSize = 16

// startUnitNameScan kicks off a background read of every unit's screen name id
// for the catalog just built. A scan already running for the same tree is left
// alone, and a scan for a different tree is stopped rather than left to write
// into an index nobody reads any more.
func StartUnitNameScan(ui *uiState, tree *modtree.ModTree, cat *unitCatalog) {
	if tree == nil || cat == nil {
		return
	}
	if ui.UnitNamesRunning && ui.UnitNamesTree == tree {
		return
	}
	StopUnitNameScan(ui)

	ui.UnitNamesTree = tree
	ui.UnitNames = make(map[string]string)
	ui.UnitNamesCh = make(chan unitNameBatch, 8)
	stop := make(chan struct{})
	ui.UnitNamesStop = stop
	ui.UnitNamesRunning = true

	// The paths are copied so the goroutine does not walk a catalog the UI
	// thread is free to replace.
	paths := make([]string, 0, 64)
	for _, r := range cat.Races {
		for _, u := range append(append([]unitEntry{}, r.Troops...), r.Structures...) {
			paths = append(paths, u.Path)
		}
	}
	ch := ui.UnitNamesCh
	go func() {
		batch := make(map[string]string, unitNameBatchSize)
		flush := func(done bool) bool {
			out := make(map[string]string, len(batch))
			for k, v := range batch {
				out[k] = v
			}
			select {
			case ch <- unitNameBatch{Names: out, Done: done}:
				batch = make(map[string]string, unitNameBatchSize)
				return true
			case <-stop:
				return false
			}
		}
		for _, path := range paths {
			if id, ok := readUnitScreenNameID(tree, path); ok {
				batch[strings.ToLower(path)] = id
			}
			if len(batch) >= unitNameBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
}

// stopUnitNameScan ends a running scan. The goroutine notices through its stop
// channel and returns, so a module reload does not leave one reading a tree that
// is being closed underneath it.
func StopUnitNameScan(ui *uiState) {
	if ui.UnitNamesStop != nil {
		close(ui.UnitNamesStop)
		ui.UnitNamesStop = nil
	}
	ui.UnitNamesRunning = false
	ui.UnitNamesCh = nil
	ui.UnitNamesTree = nil
}

// pollUnitNames takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw. Nothing else is
// animating while a module is idle, so without that a list whose names arrived
// quietly would sit on file names until the mouse happened to move.
func PollUnitNames(ui *uiState) bool {
	if ui.UnitNamesCh == nil {
		return false
	}
	got := false
	for {
		select {
		case batch := <-ui.UnitNamesCh:
			for path, id := range batch.Names {
				ui.UnitNames[path] = id
			}
			if batch.Done {
				ui.UnitNamesDone = true
				StopUnitNameScan(ui)
			}
			got = true
		default:
			return got
		}
	}
}

// readUnitScreenNameID reads one unit's screen_name_id and returns it without
// the leading "$", which is the form the locale table is keyed by.
func readUnitScreenNameID(tree *modtree.ModTree, path string) (string, bool) {
	if tree == nil || path == "" {
		return "", false
	}
	data, err := tree.ReadFile(path)
	if err != nil {
		return "", false
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return "", false
	}
	return unitScreenNameIDOf(rf)
}

// unitScreenNameIDOf is the id out of an already parsed unit. It is apart from
// the read because that is the part worth stating on its own: an unset field
// and a missing one both mean "this unit has no name", and neither may be
// cached as the id of something.
func unitScreenNameIDOf(rf *rgd.RgdFile) (string, bool) {
	if rf == nil || rf.Root == nil {
		return "", false
	}
	e := rgdFindPath(rf.Root, "ui_ext", "ui_info", "screen_name_id")
	if e == nil {
		return "", false
	}
	id, ok := rgd.LocID(e.DisplayValue())
	if !ok || id == "" || id == "0" {
		return "", false
	}
	return id, true
}

// unitLocalisedName is the text the chosen language gives a unit, or "" when
// the unit has no name yet, carries none, or the string table has no entry for
// it. The id is only a cache key here; the text is read on the UI thread, so a
// language change needs no rescan.
func UnitLocalisedName(ui *uiState, u unitEntry) string {
	if len(ui.UnitNames) == 0 || u.Path == "" {
		return ""
	}
	id, ok := ui.UnitNames[strings.ToLower(u.Path)]
	if !ok {
		return ""
	}
	return localisedString(id)
}

// unitLabel is what the unit lists call a unit: its localised screen name where
// the string table has one, and its .rgd file name where it does not. The file
// name is a real fallback rather than a placeholder — it is what the game uses
// for the unit's hotkey.
func UnitLabel(ui *uiState, u unitEntry) string {
	if text := UnitLocalisedName(ui, u); text != "" {
		return text
	}
	return u.Name
}

// unitFileLabel is the row's file name, shown dimmed beside the display name.
// It is the name on disk, so a reader can tell which file a unit they are
// looking at actually is — which matters because the string table gives
// several units of one race the same text.
func unitFileLabel(u unitEntry) string {
	// The tree path is virtual and always uses backslashes, whichever OS the
	// app runs on. filepath.Base only knows the separator of the host OS, so
	// on Linux it returned the whole path — the row then showed
	// "Data\attrib\ebps\…" instead of the file name.
	name := u.Path
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	return name
}

// ─── merged from unit_edit.go ───────────────────────────────────────────────

// Changing a unit's attributes from the object window.
//
// The object window is where a modder looks at a unit, so it is where they
// should be able to change it. The raw RGD editor can already do the job, but
// it asks the reader to find health_ext inside a tree of tables and click
// through it, which is exactly the work the info column has already done: it
// knows that "Hit points" is health_ext.hitpoints. What it could not do was
// write anything back, because its rows were strings cut out of the parsed
// file with nothing pointing at the field they came from. So a row now carries
// that field, the right column binds one editor to the selected row, and
// saving marshals the parsed unit and writes it into the mod's own overlay.
//
// The overlay is the whole safety story. The unit being edited is often one the
// mod inherits from the base game, and its installed file lives under Program
// Files: writing it would change it for every mod on the machine. Writing the
// mod's own copy instead makes the change the mod's, which is the way a modder
// overrides a unit anyway, and leaves the installation alone.

// unitFieldKey names the field the right column's editor is bound to. The
// entry's own address is used rather than the label, because two fields of one
// unit can carry the same label, and an editor bound to the wrong one of them
// would write a value into the wrong place.
func unitFieldKey(f unitField) string {
	return localeRowKey(f.Entry, f.Label)
}

// unitInfoFieldSelected is the row of the info list the right column is
// editing, or nil when there is none — no unit, an empty info list, or a row
// that is a count rather than a field.
func UnitInfoFieldSelected(ui *uiState) *unitField {
	d := CurrentUnitDetails(ui)
	if d == nil || len(d.Fields) == 0 {
		return nil
	}
	idx := clampInt(ui.UnitInfoSel, 0, len(d.Fields)-1)
	f := d.Fields[idx]
	if !unitInfoRowSelectable(&f) {
		return nil
	}
	return &f
}

// unitInfoFieldAt is the same row by index, which is what laying the list out
// needs: a row that cannot be edited still has to be drawn.
func unitInfoFieldAt(d *unitDetails, index int) *unitField {
	if d == nil || index < 0 || index >= len(d.Fields) {
		return nil
	}
	f := d.Fields[index]
	return &f
}

// applyUnitFieldText writes typed text into the field a row came from and
// rebuilds the rows so the list shows what was just changed.
//
// The text is turned into a value by the same code the raw editor uses, so a
// float typed here is refused on exactly the terms it would be refused there,
// and a value that parses to what the field already holds is not a change. The
// selection is put back on the same field rather than the same index: a change
// that empties a row drops it from the list, and the row after it would
// otherwise slide into the selection.
func ApplyUnitFieldText(ui *uiState, f *unitField, text string) error {
	if f == nil || f.Entry == nil {
		return fmt.Errorf("this row is a count, not a field of the file")
	}
	changed, err := rgdApplyScalarText(f.Entry, text)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	ui.UnitInfoDirty = true
	d := CurrentUnitDetails(ui)
	if d == nil {
		return nil
	}
	d.RebuildFromFile()
	if i := unitFieldIndex(d.Fields, f.Entry); i >= 0 {
		ui.UnitInfoSel = i
	}
	return nil
}

// saveUnitDetails writes the parsed unit back into the mod's own overlay, and
// says where it went. A unit that is not dirty is left alone rather than
// written out again: rewriting an unchanged file is what makes a tool's idea of
// "modified" and a modder's drift apart for no reason.
func SaveUnitDetails(ui *uiState) (string, error) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		return "", fmt.Errorf("no unit is open")
	}
	if !ui.UnitInfoDirty {
		return "", fmt.Errorf("there is nothing to save")
	}
	if ui.UnitValError != "" {
		return "", fmt.Errorf("correct %s first", ui.UnitValError)
	}
	data, err := d.File.MarshalBinary()
	if err != nil {
		return "", err
	}
	if ui.ModTree == nil {
		return "", fmt.Errorf("no mod is loaded")
	}
	disk, err := ui.ModTree.WriteOverlayFile(d.Entry.Path, data)
	if err != nil {
		return "", err
	}
	ui.UnitInfoDirty = false
	return fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(d.Entry.Path), disk, len(data)), nil
}

// revertUnitDetails throws the changes away and reads the unit from the file
// again, which is the only way back once a value has been typed wrongly: undo
// per keystroke would need the history the editor does not keep.
func RevertUnitDetails(ui *uiState) {
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	ui.UnitInfoDirty = false
	ui.UnitValError = ""
	ui.UnitValBound = ""
	ui.UnitValEditor.SetText("")
	ui.UnitInfoSel = 0
}

// markUnitFieldDirty marks the unit on screen as changed when a field of it has
// been changed by something other than this panel. Giving an untranslated field
// a new localisation id is the case that matters: the string itself is written
// to the .ucs, but the field that has to name it only exists in memory until
// the unit is saved, and without this the unit keeps showing no name in game
// while the app shows it translated.
//
// A field of another file is ignored, because a change to a file that is not on
// screen is that file's own business.
func MarkUnitFieldDirty(ui *uiState, f *rgd.RgdEntry) {
	if f == nil {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil || rgdPathOf(d.File.Root, f) == "" {
		return
	}
	ui.UnitInfoDirty = true
}

// rgdPathOf is where an entry sits in its file, as the dotted path the freeze
// record and the raw editor both speak. It is shown next to the editor because
// a modder who wants to check a value in the raw view needs to know where to
// look, and hunting for it again is the part they would rather not do.
func rgdPathOf(root *rgd.RgdEntry, target *rgd.RgdEntry) string {
	if root == nil || target == nil || target == root {
		return ""
	}
	for _, ch := range root.Children {
		if ch == nil {
			continue
		}
		if ch == target {
			return ch.Name
		}
		if p := rgdPathOf(ch, target); p != "" {
			return ch.Name + "." + p
		}
	}
	return ""
}

// ── the editor ──────────────────────────────────────────────────────────────

// unitBoolText is the text a boolean is flipped with, which is the same
// spelling the parser accepts so that the toggle and a typed value go through
// one door.
func unitBoolText(v bool) string {
	if v {
		return "false"
	}
	return "true"
}

// unitSyncValEditor reads the editor's events and applies what it says to the
// selected field. Enter saves rather than inserting a line break, so a value can
// be typed and committed without reaching for the button.
//
// The editor is bound to a field rather than to a row index, because the row a
// field sits at moves when the list is rebuilt and a bound-to-index editor
// would then write into whatever took its place.
func UnitSyncValEditor(ui *uiState, gtx layout.Context, f *unitField) {
	if f == nil || f.Entry == nil || !rgdIsEditableType(f.Entry.Type) || f.Entry.Type == rgd.RgdTypeBool {
		return
	}
	ed := &ui.UnitValEditor
	fieldKey := unitFieldKey(*f)
	if ui.UnitValBound != fieldKey {
		ed.SetText(f.Entry.DisplayValue())
		ui.UnitValBound = fieldKey
		ui.UnitValError = ""
	}
	ed.SingleLine = true
	ed.Submit = true
	// The editor is a one line field, so a press into it has to be given focus
	// or the reader would type into the window and wonder where it went.
	if RgdEnsureEditorClick(ui, ed).Click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	submit := false
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEnter})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if ok && e.State == key.Press {
			submit = true
		}
	}
	for {
		if _, ok := ed.Update(gtx); !ok {
			break
		}
	}
	if err := ApplyUnitFieldText(ui, f, ed.Text()); err != nil {
		ui.UnitValError = err.Error()
	} else {
		ui.UnitValError = ""
	}
	if submit {
		SaveUnitFromPanel(ui)
	}
}

// saveUnitFromPanel is the Save button's action: it writes the unit and puts
// what happened in the status line, which is where every other write in the app
// reports itself.
func SaveUnitFromPanel(ui *uiState) {
	if !ui.UnitInfoDirty {
		ui.StatusText = "This unit has no unsaved changes"
		return
	}
	line, err := SaveUnitDetails(ui)
	if err != nil {
		ui.StatusText = "Save failed: " + err.Error()
		return
	}
	ui.StatusText = line
	MarkStateDirty(ui)
}

// handleUnitFieldInput owns the presses of the attribute editor: the rows of
// the info list that select a field, the boolean toggle, the editor's typing
// and the three buttons. It runs after the section list, so a press that
// changed section is already seen here.
func HandleUnitFieldInput(ui *uiState, gtx layout.Context) {
	if CurrentUnitSection(ui).label != "Unit info" {
		return
	}
	d := CurrentUnitDetails(ui)
	if d == nil {
		return
	}
	EnsureUnitInfoClicks(ui, len(d.Fields))
	for i := range ui.UnitInfoClicks {
		if i < len(ui.UnitInfoClicks) && ui.UnitInfoClicks[i].Clicked(gtx) {
			ui.UnitInfoSel = i
		}
	}
	if ui.UnitOpenRgdClick.Clicked(gtx) {
		OpenRgd(ui, d.Entry.Path)
		return
	}
	if ui.UnitRevertClick.Clicked(gtx) {
		RevertUnitDetails(ui)
		ui.StatusText = "Changes to this unit thrown away"
		return
	}
	if ui.UnitSaveClick.Clicked(gtx) {
		SaveUnitFromPanel(ui)
		return
	}
	f := UnitInfoFieldSelected(ui)
	if f == nil {
		return
	}
	if f.Entry.Type == rgd.RgdTypeBool {
		if ui.UnitBoolClick.Clicked(gtx) {
			if err := ApplyUnitFieldText(ui, f, unitBoolText(f.Entry.BoolVal)); err != nil {
				ui.UnitValError = err.Error()
			} else {
				ui.UnitValError = ""
			}
		}
		return
	}
	UnitSyncValEditor(ui, gtx, f)
}

func EnsureUnitInfoClicks(ui *uiState, n int) {
	if len(ui.UnitInfoClicks) != n {
		ui.UnitInfoClicks = make([]widget.Clickable, n)
	}
}

// ── the right column ────────────────────────────────────────────────────────

// layoutUnitFieldPanel is the right column of the info section: which field the
// list has selected, where it sits in the file, what type it is, and the editor
// that changes it.
func LayoutUnitFieldPanel(ui *uiState, gtx layout.Context, sec unitSection) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	body := func(gtx layout.Context) layout.Dimensions {
		if d == nil || d.Error != "" {
			return UnitEmptyState(ui, gtx)
		}
		f := UnitInfoFieldSelected(ui)
		if f == nil {
			return unitFieldNote(gtx, ui.Th, "Select a row in the info list to change it.")
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "Field", f.Label, false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "In file", rgdPathOf(d.File.Root, f.Entry), false)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return UnitKeyValueRow(ui, gtx, "Type", f.Entry.TypeLabel(), false)
				})
			}),
		}
		children = append(children, UnitFieldValueWidget(ui, gtx, f)...)
		children = append(children, UnitFieldButtons(ui, gtx)...)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.rightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, body)
			}),
		)
	})
}

// unitFieldValueWidget is the value control for the selected field: a toggle for
// a boolean, since a boolean has exactly two values and typing one of them is
// both slower and easier to get wrong, and a text editor for everything else.
// The error line is under either of them, because a value that would not parse
// has to be shown as refused rather than simply not applied.
func UnitFieldValueWidget(ui *uiState, gtx layout.Context, f *unitField) []layout.FlexChild {
	out := make([]layout.FlexChild, 0, 3)
	if f.Entry.Type == rgd.RgdTypeBool {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			value := "False"
			if f.Entry.BoolVal {
				value = "True"
			}
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ToggleButton(ui, gtx, &ui.UnitBoolClick, value, f.Entry.BoolVal, uikit.CSelectBg)
			})
		}))
	} else {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowEditor(ui, gtx, "Value", &ui.UnitValEditor, false, ui.UnitValError)
		}))
	}
	if ui.UnitValError != "" {
		out = append(out, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, ui.UnitValError)
			})
		}))
	}
	return out
}

// unitFieldButtons is the row of buttons under the value: save, throw away, and
// the way out to the raw view for the fields this panel does not show.
func UnitFieldButtons(ui *uiState, gtx layout.Context) []layout.FlexChild {
	note := "Enter saves. The change is written into this mod, not the game."
	if ui.UnitInfoDirty {
		note = "Unsaved changes. " + note
	}
	return []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return PrimaryButton(ui, gtx, &ui.UnitSaveClick, "Save unit")
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButtonDisabled(ui, gtx, &ui.UnitRevertClick, "Revert", !ui.UnitInfoDirty)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &ui.UnitOpenRgdClick, "Open .rgd")
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return unitFieldNote(gtx, ui.Th, note)
			})
		}),
	}
}

// unitFieldNote is the small dimmed line the panel uses to explain itself. It
// is a function rather than a label so the two places that print one — the
// "select a row" case and the buttons — read the same.
func unitFieldNote(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(9), text)
		l.Color = uikit.CTextDim
		l.MaxLines = 6
		return l.Layout(gtx)
	})
}

// ── the rows of the info list ───────────────────────────────────────────────

// unitInfoRowSelectable reports whether a row is one the panel can change. A
// localised row keeps its own presses — they open the localisation dialog — and
// a counted row is a statement about a list rather than a field, so neither is
// given a selection click that would sit on top of the other one.
func unitInfoRowSelectable(f *unitField) bool {
	return f != nil && f.Entry != nil && !f.IsLocID
}

// layoutUnitInfoRow is one row of the info list: the section's heading where a
// section starts, and the value line itself.
func LayoutUnitInfoRow(ui *uiState, gtx layout.Context, index int, f *unitField) layout.Dimensions {
	body := func(gtx layout.Context) layout.Dimensions {
		if f == nil {
			return layout.Dimensions{}
		}
		value := func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return UnitKeyValueRowFor(ui, gtx, f.Label, f.Value, f.IsLocID, f.LocID, f.Entry)
			})
		}
		if !f.StartsGroup {
			return value(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), f.Group)
					l.Color = uikit.CTextDim
					l.Font.Weight = font.SemiBold
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(value),
		)
	}
	if !unitInfoRowSelectable(f) || index < 0 || index >= len(ui.UnitInfoClicks) {
		return body(gtx)
	}
	return UnitInfoRowFrame(ui, gtx, index, body)
}

// unitInfoRowFrame is the selection and hover of a row, under the row itself.
// It is the same shape the selectable rows of the weapon lists use, so a row
// looks selected or not in the same way wherever it is.
func UnitInfoRowFrame(ui *uiState, gtx layout.Context, index int, body layout.Widget) layout.Dimensions {
	bg := uikit.CPanelBg
	switch {
	case index == ui.UnitInfoSel:
		bg = uikit.CSelectBg
	case ui.UnitInfoClicks[index].Hovered():
		bg = uikit.CMenuHover
	}
	return ui.UnitInfoClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, body)
			}),
		)
	})
}
