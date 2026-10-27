package unit

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"strings"
)

// UnitTabKind is the uistate.Tab kind of the object interface. It is a tab kind
// rather than a tab of its own type so that it rides the existing tab bar,
// selection, closing and click handling without a second mechanism.
const UnitTabKind = "unit"

// UnitTabLabel is what the tab calls itself in the bar.
const UnitTabLabel = "Object"

// UnitTabPath is the virtual path recorded for the tab. It is not a real file:
// the tab is not backed by one, and the empty path is what marks it as such,
// so session saving skips it and a mod reload leaves it alone.
const UnitTabPath = ""

// IsUnitTab reports whether a tab is the object interface.
func IsUnitTab(t *uistate.Tab) bool {
	return t != nil && t.Kind == UnitTabKind
}

// unitTabIndex is where the object tab sits in the tab list, or -1 when it is
// not open.
func UnitTabIndex(ui *uistate.State) int {
	for i, t := range ui.OpenTabs {
		if IsUnitTab(t) {
			return i
		}
	}
	return -1
}

// openUnitTab shows the object interface, opening its tab the first time. It is
// a singleton: asking for it again just brings the existing tab forward, so the
// user cannot end up with two copies browsing the same units.
func OpenUnitTab(ui *uistate.State) {
	if idx := UnitTabIndex(ui); idx >= 0 {
		ui.TabSel = idx + 1
		uikit.EnsureTabClicks(ui)
		return
	}
	ui.OpenTabs = append(ui.OpenTabs, &uistate.Tab{Kind: UnitTabKind, Path: UnitTabPath})
	ui.TabSel = len(ui.OpenTabs)
	uikit.EnsureTabClicks(ui)
}

// UnitTabLabelFor is the tab bar's name for a tab of the object kind, kept apart
// so the bar's label chain does not grow another kind case.
func UnitTabLabelFor(t *uistate.Tab) string {
	if IsUnitTab(t) {
		return UnitTabLabel
	}
	return ""
}

// UnitTabHasNoFile reports that the object tab is not backed by a file, which
// is what the mod reload check and the session writer need to know: a tab with
// no file must not be closed when a file disappears, and must not be persisted
// as a path.
func UnitTabHasNoFile(t *uistate.Tab) bool {
	return IsUnitTab(t) || strings.TrimSpace(t.Path) == ""
}

// UnitNameBatch is a slice of the scan's results. done marks the last one.
type UnitNameBatch = uistate.UnitNameBatch

// UnitNameBatchSize is how many units one batch carries. Small enough that the
// first names appear in tens of milliseconds rather than after a whole race,
// large enough that the channel traffic is not what costs.
const UnitNameBatchSize = 16

// startUnitNameScan kicks off a background read of every unit's screen name id
// for the catalog just built. A scan already running for the same tree is left
// alone, and a scan for a different tree is stopped rather than left to write
// into an index nobody reads any more.
func StartUnitNameScan(ui *uistate.State, tree *modtree.ModTree, cat *uistate.UnitCatalog) {
	if tree == nil || cat == nil {
		return
	}
	if ui.UnitNamesRunning && ui.UnitNamesTree == tree {
		return
	}
	StopUnitNameScan(ui)

	ui.UnitNamesTree = tree
	ui.UnitNames = make(map[string]string)
	ui.UnitNamesCh = make(chan UnitNameBatch, 8)
	stop := make(chan struct{})
	ui.UnitNamesStop = stop
	ui.UnitNamesRunning = true

	// The paths are copied so the goroutine does not walk a catalog the UI
	// thread is free to replace.
	paths := make([]string, 0, 64)
	for _, r := range cat.Races {
		for _, u := range append(append([]unitdef.UnitEntry{}, r.Troops...), r.Structures...) {
			paths = append(paths, u.Path)
		}
	}
	ch := ui.UnitNamesCh
	go func() {
		batch := make(map[string]string, UnitNameBatchSize)
		flush := func(done bool) bool {
			out := make(map[string]string, len(batch))
			for k, v := range batch {
				out[k] = v
			}
			select {
			case ch <- UnitNameBatch{Names: out, Done: done}:
				batch = make(map[string]string, UnitNameBatchSize)
				return true
			case <-stop:
				return false
			}
		}
		for _, path := range paths {
			if id, ok := ReadUnitScreenNameID(tree, path); ok {
				batch[strings.ToLower(path)] = id
			}
			if len(batch) >= UnitNameBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
}

// stopUnitNameScan ends a running scan. The goroutine notices through its stop
// channel and returns, so a module reload does not leave one reading a tree that
// is being closed underneath it.
func StopUnitNameScan(ui *uistate.State) {
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
func PollUnitNames(ui *uistate.State) bool {
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

// ReadUnitScreenNameID reads one unit's screen_name_id and returns it without
// the leading "$", which is the form the locale table is keyed by.
func ReadUnitScreenNameID(tree *modtree.ModTree, path string) (string, bool) {
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
	return UnitScreenNameIDOf(rf)
}

// UnitScreenNameIDOf is the id out of an already parsed unit. It is apart from
// the read because that is the part worth stating on its own: an unset field
// and a missing one both mean "this unit has no name", and neither may be
// cached as the id of something.
func UnitScreenNameIDOf(rf *rgd.RgdFile) (string, bool) {
	if rf == nil || rf.Root == nil {
		return "", false
	}
	e := unitdef.RgdFindPath(rf.Root, "ui_ext", "ui_info", "screen_name_id")
	if e == nil {
		return "", false
	}
	id, ok := rgd.LocID(e.DisplayValue())
	if !ok || id == "" || id == "0" {
		return "", false
	}
	return id, true
}

// UnitFileLabel is the row's file name, shown dimmed beside the display name.
// It is the name on disk, so a reader can tell which file a unit they are
// looking at actually is — which matters because the string table gives
// several units of one race the same text.
func UnitFileLabel(u unitdef.UnitEntry) string {
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

// UnitInfoFieldAt is the same row by index, which is what laying the list out
// needs: a row that cannot be edited still has to be drawn.
func UnitInfoFieldAt(d *uistate.UnitDetails, index int) *unitdef.UnitField {
	if d == nil || index < 0 || index >= len(d.Fields) {
		return nil
	}
	f := d.Fields[index]
	return &f
}

// revertUnitDetails throws the changes away and reads the unit from the file
// again, which is the only way back once a value has been typed wrongly: undo
// per keystroke would need the history the editor does not keep.
func RevertUnitDetails(ui *uistate.State) {
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	ui.UnitInfoDirty = false
	ui.UnitValError = ""
	ui.UnitValBound = ""
	ui.UnitValEditor.SetText("")
	ui.UnitInfoSel = 0
}

// RgdPathOf is where an entry sits in its file, as the dotted path the freeze
// record and the raw editor both speak. It is shown next to the editor because
// a modder who wants to check a value in the raw view needs to know where to
// look, and hunting for it again is the part they would rather not do.
func RgdPathOf(root *rgd.RgdEntry, target *rgd.RgdEntry) string {
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
		if p := RgdPathOf(ch, target); p != "" {
			return ch.Name + "." + p
		}
	}
	return ""
}

// UnitBoolText is the text a boolean is flipped with, which is the same
// spelling the parser accepts so that the toggle and a typed value go through
// one door.
func UnitBoolText(v bool) string {
	if v {
		return "false"
	}
	return "true"
}

func EnsureUnitInfoClicks(ui *uistate.State, n int) {
	if len(ui.UnitInfoClicks) != n {
		ui.UnitInfoClicks = make([]widget.Clickable, n)
	}
}

// UnitFieldNote is the small dimmed line the panel uses to explain itself. It
// is a function rather than a label so the two places that print one — the
// "select a row" case and the buttons — read the same.
func UnitFieldNote(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(9), text)
		l.Color = uikit.CTextDim
		l.MaxLines = 6
		return l.Layout(gtx)
	})
}

// UnitInfoRowSelectable reports whether a row is one the panel can change. A
// localised row keeps its own presses — they open the localisation dialog — and
// a counted row is a statement about a list rather than a field, so neither is
// given a selection click that would sit on top of the other one.
func UnitInfoRowSelectable(f *unitdef.UnitField) bool {
	return f != nil && f.Entry != nil && !f.IsLocID
}

// unitInfoRowFrame is the selection and hover of a row, under the row itself.
// It is the same shape the selectable rows of the weapon lists use, so a row
// looks selected or not in the same way wherever it is.
func UnitInfoRowFrame(ui *uistate.State, gtx layout.Context, index int, body layout.Widget) layout.Dimensions {
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
					uikit.Rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, body)
			}),
		)
	})
}
