package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"path/filepath"
	"strings"
)

// ── which squads a unit belongs to ──────────────────────────────────────────
//
// A unit is not a complete object on its own. The .rgd under
// Data\attrib\ebps\races\<race>\troops describes the individual trooper: its
// hit points, its weapons, its model. What the player actually gets is a squad
// of three to eight of them, and that squad is a separate object living under
// Data\attrib\sbps\races\<race>\ named <something>_squad.rgd.
//
// There are no .sbps files in Dawn of War: Definitive Edition, which is worth
// stating because the folder is called sbps and the older game's unit data really
// was .sbps. The "s" side of a unit lives in .rgd files of exactly the same
// shape as the ebps ones, under that folder. Measured across the DXP3 tree: 431
// squad files over 10 races, naming 369 distinct units.
//
// The link runs from the squad to the unit, not the other way round. There is no
// field in a unit's .rgd that names its squad — the only near miss is
// possess_ext\squad_replacement3_name, which names one squad in a bare form and
// only on the 26 units that can be possessed. So the index has to be built by
// walking the squads, which is what this file does.
//
// In squad_loadout_ext:
//
//	trooper_base
//	{
//	    type = "ebps\\races\\necrons\\troops\\necron_lord.lua"
//	}
//
// The reference is relative to Data\attrib and names the .lua the game builds the
// unit into; the file on disk is the .rgd of the same name, which is the same
// trick the ability references use and the same resolveRgdRef handles.
//
// A unit is in at most three squads in the DXP3 tree, and 431 files take about
// 880ms to walk, so the index is built in the background and delivered in small
// batches rather than on the frame the dialog opens.

// squadRacesFolder is where the squad side of a unit lives, relative to the
// tree's "Data" root.
const squadRacesFolder = "attrib\\sbps\\races"

// unitSquadRef is one squad a unit takes part in.
type unitSquadRef struct {
	// path is the squad's own .rgd in the tree.
	path string
	// name is its file name without extension, which is how the game names it.
	name string
	// race is the folder it sits under, e.g. "necrons".
	race string
	// unitMin and unitMax are squad_loadout_ext's unit_min and unit_max: how
	// many of this unit the squad is built from. They are kept as the strings
	// the file holds rather than as numbers, because the column shows them
	// verbatim and a .rgd can carry a float where an int is expected.
	unitMin string
	unitMax string
	// birthResource is the squad's birth_resource table, the cost of raising
	// the squad as a whole. It is a separate cost from the unit's own
	// cost_ext, and it is the one a player pays.
	birthResource map[string]string
	// path fields the unit the squad is made of, for the row's own reference.
	unitRef string
}

// unitSquadBatch is a slice of the scan's results. done marks the last one.
type unitSquadBatch struct {
	// byUnit maps a lower cased unit path to the squads naming it. The unit
	// path is the key rather than the reference written in the squad, because
	// the dialog already knows a unit by its path and resolving the reference
	// back would be work done twice.
	byUnit map[string][]unitSquadRef
	done   bool
}

// unitSquadBatchSize is how many squads one batch carries.
const unitSquadBatchSize = 24

// startUnitSquadScan walks every squad .rgd in the background and builds the
// index of which squads use which unit. A scan already running for the same tree
// is left alone; one for a different tree is stopped, so a module reload does not
// leave a goroutine reading a tree that is being closed.
func (ui *uiState) startUnitSquadScan(tree *modtree.ModTree) {
	if tree == nil {
		return
	}
	if ui.unitSquadsRunning && ui.unitSquadsTree == tree {
		return
	}
	ui.stopUnitSquadScan()

	ui.unitSquadsTree = tree
	ui.unitSquads = make(map[string][]unitSquadRef)
	ui.unitSquadsCh = make(chan unitSquadBatch, 8)
	stop := make(chan struct{})
	ui.unitSquadsStop = stop
	ui.unitSquadsRunning = true

	files := collectSquadRgds(tree)
	ch := ui.unitSquadsCh
	go func() {
		batch := make(map[string][]unitSquadRef)
		flush := func(done bool) bool {
			out := make(map[string][]unitSquadRef, len(batch))
			for k, v := range batch {
				out[k] = v
			}
			select {
			case ch <- unitSquadBatch{byUnit: out, done: done}:
				batch = make(map[string][]unitSquadRef)
				return true
			case <-stop:
				return false
			}
		}
		for _, f := range files {
			for _, ref := range squadsOfUnit(tree, f) {
				key := strings.ToLower(ref.unitRef)
				batch[key] = append(batch[key], ref)
			}
			if len(batch) >= unitSquadBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
}

// stopUnitSquadScan ends a running scan. The goroutine notices through its stop
// channel and returns, so a module reload does not leave one reading a tree that
// is being closed underneath it.
func (ui *uiState) stopUnitSquadScan() {
	if ui.unitSquadsStop != nil {
		close(ui.unitSquadsStop)
		ui.unitSquadsStop = nil
	}
	ui.unitSquadsRunning = false
	ui.unitSquadsCh = nil
	ui.unitSquadsTree = nil
}

// pollUnitSquads takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw. Nothing else
// animates while a module is idle, so without this the squad column would sit
// empty until the mouse happened to move.
func (ui *uiState) pollUnitSquads() bool {
	if ui.unitSquadsCh == nil {
		return false
	}
	got := false
	for {
		select {
		case b := <-ui.unitSquadsCh:
			for path, refs := range b.byUnit {
				// A batch can carry a unit the previous batch also carried, so
				// the lists are concatenated rather than replaced. Deduplication
				// is the reader's job: a squad appears once per trooper_base it
				// declares, and no squad in the DXP3 tree declares more than one.
				ui.unitSquads[path] = append(ui.unitSquads[path], refs...)
			}
			if b.done {
				ui.unitSquadsDone = true
				ui.stopUnitSquadScan()
			}
			got = true
		default:
			if got {
				ui.sortUnitSquads()
			}
			return got
		}
	}
}

// sortUnitSquads puts every unit's squads in name order, so the list does not
// arrive in whatever order the scan happened to finish in. The scan walks
// races in tree order already, but the batches interleave units, and a list that
// reordered itself between two scans of the same mod would be a bug a reader
// could not explain.
func (ui *uiState) sortUnitSquads() {
	for _, refs := range ui.unitSquads {
		sortSquadRefs(refs)
	}
}

// sortSquadRefs orders one unit's squads by name, with the path as a tiebreak so
// two squads of one name in different races still have a fixed order.
func sortSquadRefs(refs []unitSquadRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && squadRefLess(refs[j], refs[j-1]); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// squadRefLess orders two squads of one unit. The stripped name is the primary
// key, because that is what the row shows; the full name and then the path
// settle the ties, so two squads whose stripped names collide still have a fixed
// order rather than whichever the scan reached first.
func squadRefLess(a, b unitSquadRef) bool {
	if an, bn := squadNameOf(a), squadNameOf(b); an != bn {
		return an < bn
	}
	if a.name != b.name {
		return a.name < b.name
	}
	return a.path < b.path
}

// squadNameOf is a squad's name with the trailing _squad dropped, which is the
// suffix the game's own files end in: necron_lord_squad.rgd is the lord's squad.
//
// Only a suffix is dropped, and only at the end. An earlier version cut at the
// last _squad anywhere, which turned chaos_squad_sorcerer_sp_dxp3 into "chaos" —
// the prefix of the name rather than the squad in it. Leaving a name like
// chaos_squad_sorcerer whole is the lesser evil: it is what the file is called,
// and the row's own file is shown in the right-hand column.
func squadNameOf(s unitSquadRef) string {
	if n, stripped := strings.CutSuffix(s.name, "_squad"); stripped && n != "" {
		return n
	}
	return s.name
}

// squadsOfUnit reads one squad file and returns one entry per trooper_base it
// declares, each naming the unit that trooper is. The unit path is carried in
// unitRef rather than returned separately, because the caller keys its batch by
// it and the entry it stores is the enriched copy: a squad that is in the list
// also has a size and a birth cost, and reading the file a second time to get
// those would be a wasted read per squad.
func squadsOfUnit(tree *modtree.ModTree, f unitSquadRef) []unitSquadRef {
	if tree == nil || f.path == "" {
		return nil
	}
	data, err := tree.ReadFile(f.path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	loadout := rgdFindPath(rf.Root, "squad_loadout_ext")
	if loadout == nil {
		return nil
	}
	// The size and the birth cost belong to the squad rather than to any one
	// trooper, so they are read once here and copied onto every entry.
	base := f
	base.unitMin = strings.TrimSpace(rgdEntryStr(rgdFindChild(loadout, "unit_min")))
	base.unitMax = strings.TrimSpace(rgdEntryStr(rgdFindChild(loadout, "unit_max")))
	base.birthResource = readBirthResource(loadout)

	var out []unitSquadRef
	for _, c := range loadout.Children {
		if c == nil || !c.IsTable() || !strings.EqualFold(c.Name, "trooper_base") {
			continue
		}
		ref := strings.TrimSpace(rgdEntryStr(rgdFindChild(c, "type")))
		if ref == "" {
			continue
		}
		p := resolveRgdRef(tree, ref)
		if p == "" {
			continue
		}
		entry := base
		entry.unitRef = p
		out = append(out, entry)
	}
	return out
}

// readBirthResource is the squad's birth_resource table: what it costs to raise
// the squad, which is a different cost from the unit's own cost_ext. Only the
// currencies a squad can actually charge are read, and a zero is kept because a
// zero is a real answer.
func readBirthResource(loadout *rgd.RgdEntry) map[string]string {
	br := rgdFindChild(loadout, "birth_resource")
	if br == nil {
		return nil
	}
	out := map[string]string{}
	for _, cur := range []string{"requisition", "power", "pop", "faith", "souls"} {
		if v := strings.TrimSpace(rgdEntryStr(rgdFindChild(br, cur))); v != "" {
			out[cur] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectSquadRgds lists every squad file in the tree, sorted by path so the scan
// is reproducible. A race folder that cannot be read is skipped rather than
// failing the whole index, for the same reason the unit catalog does.
func collectSquadRgds(tree *modtree.ModTree) []unitSquadRef {
	var out []unitSquadRef
	if tree == nil || tree.Root == nil {
		return out
	}
	races := modtree.FindFolderByPath(tree.Root, squadRacesFolder)
	if races == nil {
		return out
	}
	for _, race := range races.Children {
		if race == nil {
			continue
		}
		for _, file := range race.Files {
			if file == nil || !strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
				continue
			}
			name := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
			if name == "" {
				continue
			}
			out = append(out, unitSquadRef{
				path: file.FullPath,
				name: name,
				race: race.Name,
			})
		}
	}
	sortSquadRefs(out)
	return out
}

// squadsOfUnitOf is the dialog's lookup: the squads a given unit is part of, or
// nil when the unit is in none. It is nil rather than an empty slice so a caller
// can tell "not scanned yet" from "this unit is in no squad" — the scan starts
// when the catalog is built and finishes seconds later, and during that window
// every unit would otherwise look like it is in no squad at all.
func (ui *uiState) squadsOfUnitOf(u unitEntry) []unitSquadRef {
	if u.path == "" || ui.unitSquads == nil {
		return nil
	}
	return ui.unitSquads[strings.ToLower(u.path)]
}
