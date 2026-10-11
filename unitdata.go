package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"path/filepath"
	"sort"
	"strings"
)

// ── unit catalogue ──────────────────────────────────────────────────────────
//
// The Object dialog browses the mod's own units rather than the currently open
// file. Every unit of every race lives at a fixed place in the tree:
//
//	Data\attrib\ebps\races\<race>\troops\*.rgd
//	Data\attrib\ebps\races\<race>\structures\*.rgd
//
// The .whe/.whm/.lua siblings of an .rgd are the same unit in other
// representations, so only the .rgd is listed: it is the one carrying the
// stats, the weapon table and the ability references.

// unitKind separates the two folders a unit can come from, so the dialog can
// label it and the "Unit info" column can say which kind it is showing.
type unitKind struct {
	folder string // subfolder under the race, as it appears in the tree
	label  string // shown in the dialog
}

var (
	unitKindTroop     = unitKind{folder: "troops", label: "Troop"}
	unitKindStructure = unitKind{folder: "structures", label: "Structure"}
)

// unitEntry is one browsable unit.
type unitEntry struct {
	// path is the virtual tree path of the .rgd, e.g.
	// "Data\attrib\ebps\races\necrons\troops\necron_lord.rgd".
	path string
	// name is the file name without its extension.
	name string
	// race is the folder name under "races", e.g. "necrons".
	race string
	kind unitKind
}

// displayName is what the dialog lists. The file name is the only thing known
// before the file is parsed, and it is what the game itself uses for hotkeys,
// so it is the label the list shows.
func (u unitEntry) displayName() string { return u.name }

// raceEntry is one race and the units found under it.
type raceEntry struct {
	name       string
	troops     []unitEntry
	structures []unitEntry
}

// unitCatalog is the whole browsable set, built once per mod.
type unitCatalog struct {
	races []raceEntry
	// byPath indexes every unit for lookup by its tree path.
	byPath map[string]unitEntry
}

// allUnits returns every unit of the catalog, races in name order and within a
// race troops before structures, so the order is stable between builds.
func (c *unitCatalog) allUnits() []unitEntry {
	if c == nil {
		return nil
	}
	var out []unitEntry
	for _, r := range c.races {
		out = append(out, r.troops...)
		out = append(out, r.structures...)
	}
	return out
}

// findRace returns the race with the given name.
func (c *unitCatalog) findRace(name string) *raceEntry {
	if c == nil {
		return nil
	}
	for i := range c.races {
		if strings.EqualFold(c.races[i].name, name) {
			return &c.races[i]
		}
	}
	return nil
}

// unitCount is how many units the race has in total, used to label the race
// picker.
func (r *raceEntry) unitCount() int { return len(r.troops) + len(r.structures) }

// racesFolder is the fixed location every race lives under, relative to the
// tree's "Data" root: Data\attrib\ebps\races\<race>\...
const racesFolder = "attrib\\ebps\\races"

// buildUnitCatalog walks the races folder and collects every .rgd unit. A race
// folder that fails to read is skipped rather than failing the whole catalog,
// so one unreadable mod cannot leave the dialog empty.
func buildUnitCatalog(tree *modtree.ModTree) *unitCatalog {
	cat := &unitCatalog{byPath: make(map[string]unitEntry)}
	if tree == nil || tree.Root == nil {
		return cat
	}
	races := modtree.FindFolderByPath(tree.Root, racesFolder)
	if races == nil {
		return cat
	}
	for _, rf := range races.Children {
		if rf == nil {
			continue
		}
		entry := raceEntry{name: rf.Name}
		for _, kind := range []unitKind{unitKindTroop, unitKindStructure} {
			sub := modtree.FindFolderByLower(rf, kind.folder)
			if sub == nil {
				continue
			}
			units := collectUnitRgds(sub, rf.Name, kind)
			if kind.folder == unitKindTroop.folder {
				entry.troops = units
			} else {
				entry.structures = units
			}
		}
		if entry.unitCount() == 0 {
			continue
		}
		cat.races = append(cat.races, entry)
	}
	sort.Slice(cat.races, func(i, j int) bool {
		return cat.races[i].name < cat.races[j].name
	})
	for _, r := range cat.races {
		for _, u := range append(append([]unitEntry{}, r.troops...), r.structures...) {
			cat.byPath[strings.ToLower(u.path)] = u
		}
	}
	return cat
}

// collectUnitRgds lists the .rgd units of one folder, sorted by name.
func collectUnitRgds(folder *modtree.TreeFolder, race string, kind unitKind) []unitEntry {
	var out []unitEntry
	for _, f := range folder.Files {
		if f == nil || !strings.EqualFold(filepath.Ext(f.Name), ".rgd") {
			continue
		}
		name := strings.TrimSuffix(f.Name, filepath.Ext(f.Name))
		if name == "" {
			continue
		}
		out = append(out, unitEntry{path: f.FullPath, name: name, race: race, kind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// ── ability references ──────────────────────────────────────────────────────
//
// A unit's ability_ext\abilities table holds up to ten slots. Each slot is a
// DT_String with a path that is relative to Data\attrib and written with a .lua
// extension:
//
//	ability_02 = "abilities\necron_chronometron.lua"
//
// The .lua is what the game uses at runtime after it has converted the .rgd in
// memory; on disk the asset is the .rgd of the same name, which lives at
// Data\attrib\abilities\<name>.rgd. Measured on necron_lord.rgd, all ten slots
// resolve that way.

// abilityRef is one resolved ability slot of a unit.
type abilityRef struct {
	// slot is the field name, e.g. "ability_02".
	slot string
	// ref is the path exactly as written in the rgd, e.g.
	// "abilities\necron_chronometron.lua".
	ref string
	// path is the .rgd that ref points at on disk, or "" when it does not
	// resolve.
	path string
}

// resolved reports whether the reference found a file.
func (a abilityRef) resolved() bool { return a.path != "" }

// name is the ability's file name without extension, which is the game's own
// name for it.
func (a abilityRef) name() string {
	base := a.ref
	if i := strings.LastIndexAny(base, "\\/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// abilitySlots is the field name pattern of the ability table, in the order the
// game numbers them.
var abilitySlots = []string{
	"ability_01", "ability_02", "ability_03", "ability_04", "ability_05",
	"ability_06", "ability_07", "ability_08", "ability_09", "ability_10",
}

// unitAbilities reads ability_ext\abilities out of a parsed unit and resolves
// every slot against the tree. Slots whose value is empty are kept, because
// their emptiness is information: 115 of the race unit .rgds ship an empty
// ability_01, and the trailing slots are usually empty too.
func unitAbilities(tree *modtree.ModTree, rf *rgd.RgdFile) []abilityRef {
	if tree == nil || rf == nil || rf.Root == nil {
		return nil
	}
	ext := rgdFindChild(rf.Root, "ability_ext")
	if ext == nil {
		return nil
	}
	table := rgdFindChild(ext, "abilities")
	if table == nil {
		return nil
	}
	byName := make(map[string]*rgd.RgdEntry, len(table.Children))
	for _, c := range table.Children {
		byName[strings.ToLower(c.Name)] = c
	}
	var out []abilityRef
	for _, slot := range abilitySlots {
		e := byName[strings.ToLower(slot)]
		if e == nil {
			continue
		}
		ref := e.DisplayValue()
		if strings.TrimSpace(ref) == "" {
			out = append(out, abilityRef{slot: slot})
			continue
		}
		out = append(out, abilityRef{slot: slot, ref: ref, path: resolveRgdRef(tree, ref)})
	}
	return out
}

// resolveRgdRef turns a reference written inside a .rgd into a tree path.
//
// A reference is relative to Data\attrib, uses backslashes, and normally names
// a .lua — the game converts the .rgd in memory and refers to the result by
// its .lua name. The asset on disk is the .rgd of the same name, so the .rgd
// form is tried as well as the literal one. Both Data\attrib and Data\attrib\ebps
// are tried as bases, because references are written both ways in the game's
// own data.
func resolveRgdRef(tree *modtree.ModTree, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	rel := strings.ReplaceAll(ref, "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	// The .rgd form of the reference, which is normally the real asset.
	asRgd := rel
	if i := strings.LastIndex(asRgd, "."); i >= 0 {
		asRgd = asRgd[:i] + ".rgd"
	} else {
		asRgd += ".rgd"
	}
	for _, base := range []string{"", "ebps/"} {
		// The .rgd form first: a reference naming a .lua almost never has a
		// .lua on disk, so trying the literal name first would only cost a
		// wasted lookup.
		for _, candidate := range []string{asRgd, base + rel} {
			if candidate == "" {
				continue
			}
			path := filepath.Join("Data", "attrib", filepath.FromSlash(candidate))
			if tree.FindFile(path) != nil {
				return path
			}
		}
	}
	// A reference already written as a full tree path, e.g. from an edited file.
	if tree.FindFile(filepath.FromSlash(rel)) != nil {
		return filepath.FromSlash(rel)
	}
	return ""
}

// rgdFindChild looks a child up by name, case-insensitively.
func rgdFindChild(e *rgd.RgdEntry, name string) *rgd.RgdEntry {
	if e == nil {
		return nil
	}
	for _, c := range e.Children {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

// ── selection helpers ───────────────────────────────────────────────────────

// unitsOf returns the race's units as one list, troops before structures, which
// is the order the unit picker lists them in.
func (r *raceEntry) unitsOf() []unitEntry {
	if r == nil {
		return nil
	}
	out := make([]unitEntry, 0, r.unitCount())
	out = append(out, r.troops...)
	out = append(out, r.structures...)
	return out
}

// clampRaceSel keeps the race index inside the catalog. An empty catalog, or an
// index left over from a different mod, yields 0 rather than a panic, and the
// caller checks for a nil race.
func (c *unitCatalog) clampRaceSel(i int) int {
	if c == nil || len(c.races) == 0 {
		return 0
	}
	if i < 0 || i >= len(c.races) {
		return 0
	}
	return i
}

// clampUnitSel keeps the unit index inside a race's unit list.
func (r *raceEntry) clampUnitSel(i int) int {
	n := r.unitCount()
	if n == 0 {
		return 0
	}
	if i < 0 || i >= n {
		return 0
	}
	return i
}

// selectedRace is the race the dialog is showing, or nil when the catalog is
// empty.
func (c *unitCatalog) selectedRace(i int) *raceEntry {
	if c == nil || len(c.races) == 0 {
		return nil
	}
	return &c.races[c.clampRaceSel(i)]
}

// selectedUnit is the unit the dialog is showing, or nil when there is none.
func (c *unitCatalog) selectedUnit(raceIdx, unitIdx int) *unitEntry {
	r := c.selectedRace(raceIdx)
	if r == nil {
		return nil
	}
	units := r.unitsOf()
	i := r.clampUnitSel(unitIdx)
	if i >= len(units) {
		return nil
	}
	return &units[i]
}
