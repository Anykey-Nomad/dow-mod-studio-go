package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"strings"
)

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
type unitNameBatch struct {
	names map[string]string
	done  bool
}

// unitNameBatchSize is how many units one batch carries. Small enough that the
// first names appear in tens of milliseconds rather than after a whole race,
// large enough that the channel traffic is not what costs.
const unitNameBatchSize = 16

// startUnitNameScan kicks off a background read of every unit's screen name id
// for the catalog just built. A scan already running for the same tree is left
// alone, and a scan for a different tree is stopped rather than left to write
// into an index nobody reads any more.
func (ui *uiState) startUnitNameScan(tree *modtree.ModTree, cat *unitCatalog) {
	if tree == nil || cat == nil {
		return
	}
	if ui.unitNamesRunning && ui.unitNamesTree == tree {
		return
	}
	ui.stopUnitNameScan()

	ui.unitNamesTree = tree
	ui.unitNames = make(map[string]string)
	ui.unitNamesCh = make(chan unitNameBatch, 8)
	stop := make(chan struct{})
	ui.unitNamesStop = stop
	ui.unitNamesRunning = true

	// The paths are copied so the goroutine does not walk a catalog the UI
	// thread is free to replace.
	paths := make([]string, 0, 64)
	for _, r := range cat.Races {
		for _, u := range append(append([]unitEntry{}, r.Troops...), r.Structures...) {
			paths = append(paths, u.Path)
		}
	}
	ch := ui.unitNamesCh
	go func() {
		batch := make(map[string]string, unitNameBatchSize)
		flush := func(done bool) bool {
			out := make(map[string]string, len(batch))
			for k, v := range batch {
				out[k] = v
			}
			select {
			case ch <- unitNameBatch{names: out, done: done}:
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
func (ui *uiState) stopUnitNameScan() {
	if ui.unitNamesStop != nil {
		close(ui.unitNamesStop)
		ui.unitNamesStop = nil
	}
	ui.unitNamesRunning = false
	ui.unitNamesCh = nil
	ui.unitNamesTree = nil
}

// pollUnitNames takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw. Nothing else is
// animating while a module is idle, so without that a list whose names arrived
// quietly would sit on file names until the mouse happened to move.
func (ui *uiState) pollUnitNames() bool {
	if ui.unitNamesCh == nil {
		return false
	}
	got := false
	for {
		select {
		case batch := <-ui.unitNamesCh:
			for path, id := range batch.names {
				ui.unitNames[path] = id
			}
			if batch.done {
				ui.unitNamesDone = true
				ui.stopUnitNameScan()
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
func (ui *uiState) unitLocalisedName(u unitEntry) string {
	if len(ui.unitNames) == 0 || u.Path == "" {
		return ""
	}
	id, ok := ui.unitNames[strings.ToLower(u.Path)]
	if !ok {
		return ""
	}
	return localisedString(id)
}

// unitLabel is what the unit lists call a unit: its localised screen name where
// the string table has one, and its .rgd file name where it does not. The file
// name is a real fallback rather than a placeholder — it is what the game uses
// for the unit's hotkey.
func (ui *uiState) unitLabel(u unitEntry) string {
	if text := ui.unitLocalisedName(u); text != "" {
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
