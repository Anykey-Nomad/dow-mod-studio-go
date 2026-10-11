package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"path/filepath"
	"sort"
	"strings"
)

// The mod's data read as a graph.
//
// Everything a mod can be wrong about in a way that never shows up as a parse
// error lives in a reference: a unit pointing at a weapon that is not there, a
// building whose weapon table names a file the game never heard of. The files
// themselves say nothing wrong, so nothing complains — until the game loads the
// unit and quietly drops the weapon. The tools that build units need to know
// this, because a unit that is about to be moved out of the mod has to leave
// nothing behind that points at it.
//
// So the whole of Data\attrib is indexed once: every .rgd becomes an entity, and
// every field of a unit or building that names another file becomes a link
// between two entities. Reading a unit then means reading its links, and
// freezing one means following them backwards.
//
// The field vocabulary is not guessed. It was measured over the 582 units and
// structures the game ships: 154 field paths carry a reference, in eleven
// sections, and graphRoleForField below is the table that covers them.

const (
	// graphRoot is where the indexed data starts.
	graphRoot = "attrib"
)

// graphKind is what an entity is, decided by where it sits in the tree. The
// game's own folders are the only description of this there is.
type graphKind int

const (
	graphKindUnit graphKind = iota
	graphKindStructure
	graphKindSquad
	graphKindWeapon
	graphKindAbility
	graphKindAddon
	graphKindResearch
	graphKindModifier
	graphKindRequirement
	graphKindFormation
	graphKindEnvironment
	graphKindSchema
	graphKindOther
)

// label is what the UI calls the kind.
func (k graphKind) label() string {
	switch k {
	case graphKindUnit:
		return "Unit"
	case graphKindStructure:
		return "Structure"
	case graphKindSquad:
		return "Squad"
	case graphKindWeapon:
		return "Weapon"
	case graphKindAbility:
		return "Ability"
	case graphKindAddon:
		return "Addon"
	case graphKindResearch:
		return "Research"
	case graphKindModifier:
		return "Modifier"
	case graphKindRequirement:
		return "Requirement"
	case graphKindFormation:
		return "Formation"
	case graphKindEnvironment:
		return "Environment"
	case graphKindSchema:
		return "Schema"
	}
	return "Other"
}

// graphEntity is one .rgd of the mod, as the graph knows it.
type graphEntity struct {
	// path is the virtual tree path, e.g.
	// "Data\attrib\ebps\races\necrons\troops\necron_wraith.rgd".
	path string
	// name is the file name without its extension.
	name string
	// kind is what the entity is.
	kind graphKind
	// race is the folder name under "races" for the kinds that have one.
	race string
	// file is the tree entry it came from, which knows whether the mod owns
	// it and where it physically lives.
	file *modtree.TreeFile
}

// graphRole is what a link means to the entity that holds it, said from that
// entity's point of view.
type graphRole int

const (
	roleWeapon graphRole = iota
	roleAbility
	roleAddon
	roleResearch
	roleSquad
	roleBuiltBy
	roleNeedsAddon
	roleNeedsResearch
	roleBlueprint
	roleRelocation
	roleEffect
	roleModifies
	roleSpawns
	roleProjectile
	roleNeedsStructure
	roleExcludesAddon
)

// label is what the UI calls the role, and the wording matters: a link is read
// from the entity that holds it, so "built by" is right and "builds" would be
// backwards.
func (r graphRole) label() string {
	switch r {
	case roleWeapon:
		return "weapon"
	case roleAbility:
		return "ability"
	case roleAddon:
		return "addon"
	case roleResearch:
		return "research"
	case roleSquad:
		return "squad"
	case roleBuiltBy:
		return "built by"
	case roleNeedsAddon:
		return "needs addon"
	case roleNeedsResearch:
		return "needs research"
	case roleBlueprint:
		return "entrenched as"
	case roleRelocation:
		return "relocates as"
	case roleEffect:
		return "death effect"
	case roleModifies:
		return "modifies"
	case roleSpawns:
		return "spawns"
	case roleProjectile:
		return "fires"
	case roleNeedsStructure:
		return "needs structure"
	case roleExcludesAddon:
		return "excludes addon"
	}
	return "refers to"
}

// isRequirement is true of the roles that come out of a requirements table,
// which is where the game's own "only buildable if" rules live.
func (r graphRole) isRequirement() bool {
	return r == roleBuiltBy || r == roleNeedsAddon || r == roleNeedsResearch
}

// graphLink is one reference from one entity to another.
type graphLink struct {
	// from is the virtual path of the entity holding the reference.
	from string
	// to is the virtual path it resolves to, or "" when it resolves to
	// nothing at all — a dangling reference, which is the whole point of
	// looking.
	to string
	// ref is the reference exactly as the file writes it, e.g.
	// "weapon\chaos_bolt_pistol_champion.lua" or the bare "chaos_hq".
	ref string
	// field is the dotted path of the field holding it, e.g.
	// "ability_ext.abilities.ability_01", which is what a freeze has to
	// find again to clear it.
	field string
	// slot is the numbered slot the reference sits in, e.g. "ability_01",
	// or the hardpoint and weapon pair for a weapon. It is the part of
	// field that says which of several similar references this is.
	slot string
	// role is what the reference means.
	role graphRole
}

// resolved reports whether the link found a file.
func (l graphLink) resolved() bool { return l.to != "" }

// targetName is what the link points at, as a name, whether or not it resolved.
func (l graphLink) targetName() string { return refStem(l.ref) }

// modGraph is the whole of Data\attrib, indexed.
type modGraph struct {
	tree *modtree.ModTree

	// entities is every .rgd under Data\attrib, in tree order.
	entities []*graphEntity
	// byPath indexes the entities by their lower cased virtual path.
	byPath map[string]*graphEntity
	// byName indexes them by lower cased file stem, because references are
	// often written as a bare name and a name is all there is to go on.
	byName map[string][]*graphEntity

	// links is every reference found, once ensureLinks has run.
	links []graphLink
	// out and in index the links by their source and their target, so both
	// directions of a relationship are one lookup.
	out map[string][]int
	in  map[string][]int
	// linked is set once the unit and building files have been read.
	linked bool
	// scanning is set while a background scan is still writing batches in, so
	// that the indexes are built once at the end rather than by two threads at
	// once.
	scanning bool
	// parsed holds the files already read, so the freeze that rewrites a
	// reference and the report that found it agree on the same parse.
	parsed map[string]*rgd.RgdFile
}

// newModGraph indexes the tree without reading a single file's contents: the
// folder layout alone says what every .rgd is. Reading the ~580 unit and
// building files to find their references is left to ensureLinks, because that
// is the expensive half and not every caller needs it.
func newModGraph(tree *modtree.ModTree) *modGraph {
	g := &modGraph{
		tree:   tree,
		byPath: make(map[string]*graphEntity),
		byName: make(map[string][]*graphEntity),
		out:    make(map[string][]int),
		in:     make(map[string][]int),
	}
	if tree == nil || tree.Root == nil {
		return g
	}
	root := modtree.FindFolderByPath(tree.Root, graphRoot)
	if root == nil {
		return g
	}
	modtree.SearchWalk(root, func(f *modtree.TreeFile) {
		if f == nil || !strings.EqualFold(filepath.Ext(f.Name), ".rgd") {
			return
		}
		e := &graphEntity{
			path: f.FullPath,
			name: strings.TrimSuffix(f.Name, filepath.Ext(f.Name)),
			file: f,
		}
		e.kind, e.race = classifyGraphEntity(f.FullPath)
		g.entities = append(g.entities, e)
		lower := strings.ToLower(f.FullPath)
		g.byPath[lower] = e
		lowerName := strings.ToLower(e.name)
		g.byName[lowerName] = append(g.byName[lowerName], e)
	})
	return g
}

// entity looks an entity up by virtual path.
func (g *modGraph) entity(path string) *graphEntity {
	if g == nil {
		return nil
	}
	return g.byPath[strings.ToLower(path)]
}

// entitiesOfKind returns every entity of one kind, in tree order.
func (g *modGraph) entitiesOfKind(kinds ...graphKind) []*graphEntity {
	want := make(map[graphKind]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	var out []*graphEntity
	for _, e := range g.entities {
		if want[e.kind] {
			out = append(out, e)
		}
	}
	return out
}

// ── classification ──────────────────────────────────────────────────────────

// classifyGraphEntity decides what a .rgd is from its path, and which race it
// belongs to. The folder names are the game's own; there is no marker inside
// the files that would say.
func classifyGraphEntity(path string) (graphKind, string) {
	parts := strings.Split(strings.ReplaceAll(path, "/", "\\"), "\\")
	// parts[0] is "Data", parts[1] is "attrib"; anything else is not data
	// we index.
	if len(parts) < 3 || !strings.EqualFold(parts[1], graphRoot) {
		return graphKindOther, ""
	}
	folder := strings.ToLower(parts[2])
	switch folder {
	case "weapon":
		return graphKindWeapon, ""
	case "abilities":
		return graphKindAbility, ""
	case "addons":
		return graphKindAddon, ""
	case "research":
		return graphKindResearch, ""
	case "modifiers":
		return graphKindModifier, ""
	case "requirements":
		return graphKindRequirement, ""
	case "formations":
		return graphKindFormation, ""
	case "sbps":
		// sbps\races\<race>\<squad>.rgd
		if len(parts) >= 5 && strings.EqualFold(parts[3], "races") {
			return graphKindSquad, parts[4]
		}
		return graphKindOther, ""
	case "ebps":
		// ebps\races\<race>\{troops,structures}\<unit>.rgd
		if len(parts) >= 6 && strings.EqualFold(parts[3], "races") {
			switch strings.ToLower(parts[5]) {
			case "troops":
				return graphKindUnit, parts[4]
			case "structures":
				return graphKindStructure, parts[4]
			}
		}
		// environment and game hold the props, projectiles and relocation
		// blueprints; everything else under ebps is one of those two.
		if len(parts) >= 4 {
			switch strings.ToLower(parts[3]) {
			case "environment", "game":
				return graphKindEnvironment, ""
			}
		}
		return graphKindOther, ""
	}
	// ebpextensions, sbpextensions, tables, tuning, racebps and every
	// type_* folder hold the definitions the rest of the data is written
	// against. They are the schema, not content.
	return graphKindSchema, ""
}

// ── link extraction ─────────────────────────────────────────────────────────

// graphRoleForField decides what a field's value means, from the field's own
// dotted path and from what kind of file holds it. It returns false for a
// field that carries no reference.
//
// Both arguments are needed, and the second one is not decoration. A field
// called structure_name means "this building makes me" when a unit or a squad
// says it, and "this add-on is unlocked by that building" when an add-on says
// it. The same three words, opposite directions, and the graph's job is to say
// which one it is — Produces() reads the link backwards and would otherwise
// claim that an add-on builds the building it is gated behind.
//
// The shapes below are the ones the game's own files use, read off all of its
// abilities, add-ons, weapons, squads, research, units and buildings rather
// than guessed. Hardpoints are numbered, and so are the weapon, ability,
// addon, research, squad, modifier and requirement slots, so a number in the
// path is expected rather than matched literally.
func graphRoleForField(kind graphKind, path []string) (graphRole, string, bool) {
	// combat_ext.hardpoints.hardpoint_NN.weapon_table.weapon_NN.weapon
	if len(path) == 6 && eq(path[0], "combat_ext") && eq(path[1], "hardpoints") &&
		eq(path[3], "weapon_table") && eq(path[5], "weapon") {
		return roleWeapon, path[2] + "/" + path[4], true
	}
	// The four numbered slot tables. Each is one section, one table, and
	// the slot itself.
	if len(path) == 3 {
		switch {
		case eq(path[0], "ability_ext") && eq(path[1], "abilities"):
			return roleAbility, path[2], true
		case eq(path[0], "addon_ext") && eq(path[1], "addons"):
			return roleAddon, path[2], true
		case eq(path[0], "research_ext") && eq(path[1], "research_table"):
			return roleResearch, path[2], true
		case eq(path[0], "spawner_ext") && eq(path[1], "squad_table"):
			return roleSquad, path[2], true
		}
	}
	// A requirement table. The game writes it two ways and both are in use:
	// units and squads nest it inside a section ("requirement_ext.requirements
	// .required_1.structure_name", "squad_requirement_ext.requirements
	// .required_1.addon_name"), while abilities, add-ons, weapons and research
	// hang it off the top level ("requirements.required_1.addon_name"). Both
	// are read here, because an ability that cannot be unlocked is exactly as
	// broken as a unit that cannot be built.
	//
	// The leaf says what kind of thing is named, and the referrer's own kind
	// says what it means. A unit or a squad naming a structure is being built
	// by it; an add-on, an ability, a weapon or a piece of research naming one
	// is asking for it to be finished first.
	if i := requirementLeafIndex(path); i > 0 {
		leaf := strings.ToLower(path[i])
		slot := path[i-1]
		switch {
		case strings.Contains(leaf, "structure_name"):
			if kind == graphKindUnit || kind == graphKindStructure || kind == graphKindSquad {
				return roleBuiltBy, slot, true
			}
			return roleNeedsStructure, slot, true
		case leaf == "global_addon_name" || leaf == "addon_name" || leaf == "addon":
			return roleNeedsAddon, slot, true
		case leaf == "mutually_exclusive_with":
			return roleExcludesAddon, slot, true
		case strings.Contains(leaf, "research_name"):
			return roleNeedsResearch, slot, true
		case leaf == "squad_name":
			return roleSquad, slot, true
		}
		return 0, "", false
	}
	// Which unit a modifier applies to. This is the largest single source of
	// references in the game outside the units themselves: 1290 of them in
	// research alone, and every one of those is an upgrade that silently does
	// nothing if the unit it names is wrong or missing. The table is written
	// the same way in abilities (nested under area_effect and backfire), in
	// add-ons and research (modifiers.N) and in units and squads (which carry
	// several sections of their own), so the path is matched by shape rather
	// than by section name.
	if n := len(path); n >= 2 && eq(path[n-1], "target_type_name") {
		for _, part := range path[:n-1] {
			if eq(part, "modifiers") {
				return roleModifies, path[n-2], true
			}
		}
		return 0, "", false
	}
	// An ability that fires another ability, and the entity it puts in the
	// world. Both are top-level fields of an ability file.
	if len(path) == 1 {
		switch {
		case eq(path[0], "child_ability_name"):
			return roleAbility, path[0], true
		case eq(path[0], "spawned_entity_name"):
			return roleSpawns, path[0], true
		}
	}
	// The projectile a weapon or an ability sends, and the ability a weapon
	// triggers on impact. The projectile is a file under
	// ebps\environment\gameplay, so it is an entity like any other.
	if len(path) == 1 {
		switch {
		case eq(path[0], "fired_projectile"), eq(path[0], "projectile_name"):
			return roleProjectile, path[0], true
		case eq(path[0], "impact_ability"):
			return roleAbility, path[0], true
		}
	}
	// A weapon that is really a group of other weapons. Chaos' advance
	// variants put three real weapons behind one slot, and swapping one out
	// has to know the other two.
	if len(path) == 3 && eq(path[0], "group_weapon_table") && eq(path[2], "referenced_weapon_name") {
		return roleWeapon, path[1], true
	}
	// A unit that has a second, entrenched version of itself.
	if len(path) == 2 && eq(path[0], "entrench_ext") && eq(path[1], "entrenched_blueprint_name") {
		return roleBlueprint, path[1], true
	}
	// The building a relocation leaves behind: when a structure moves, the
	// game stands a dummy in its place, and that dummy is another unit. It is
	// named in a bare name, and it is a reference like any other — a building
	// that is moved out of the mod must not leave a dummy pointing at it.
	if len(path) == 2 && eq(path[0], "relocatee_ext") && eq(path[1], "dummy_structure_name") {
		return roleRelocation, path[1], true
	}
	// What a corpse leaves behind.
	if len(path) == 2 && eq(path[0], "health_ext") {
		switch strings.ToLower(path[1]) {
		case "spawn_death_blossom", "spawn_blood_splat_on_death", "spawn_usable_body_on_death":
			return roleEffect, path[1], true
		}
	}
	return 0, "", false
}

func eq(a, b string) bool { return strings.EqualFold(a, b) }

// requirementLeafIndex finds the field of a requirement slot in a dotted path,
// or returns -1 when the path is not one.
//
// The two shapes the game writes are "requirements.required_1.structure_name"
// and "requirement_ext.requirements.required_1.structure_name", and the
// difference is one element. Rather than test for both, this walks for the
// "requirements" table and takes the field after the slot that follows it.
func requirementLeafIndex(path []string) int {
	for i := 0; i+2 < len(path); i++ {
		if eq(path[i], "requirements") && strings.HasPrefix(strings.ToLower(path[i+1]), "required") {
			return i + 2
		}
	}
	return -1
}

// graphLinkKinds are the kinds whose files are read for the references they
// hold. It started as units and buildings, because those are the only things
// the mod author writes by hand, and every other kind was merely a target.
// That turned out to be half the picture: an ability names the entity it
// spawns and the unit its modifier applies to, an add-on names the building
// that unlocks it and the squad it upgrades, and a research file carries 1290
// target_type_name references of its own. A mod author who breaks one of
// those breaks the mod just as surely, and neither freeze nor the broken-link
// report could see it.
//
// The kinds deliberately left out are the ones that only ever hold tuning
// numbers: modifiers, requirements and formations. The schema and environment
// kinds are out for a different reason — they are large and mostly props.
func graphLinkKinds() []graphKind {
	return []graphKind{
		graphKindUnit,
		graphKindStructure,
		graphKindAbility,
		graphKindAddon,
		graphKindWeapon,
		graphKindSquad,
		graphKindResearch,
	}
}

// ensureLinks reads every referring file once and records the references it
// finds. It is safe to call as often as you like: the work happens once.
//
// It is the blocking form, used by the tests and by anything that is not on a
// frame. A window that wants the links should start a scan instead, because
// reading three thousand files takes seconds and a frame cannot wait that long.
func (g *modGraph) ensureLinks() {
	if g == nil || g.linked {
		return
	}
	g.linked = true
	if g.tree == nil {
		return
	}
	for _, e := range g.entitiesOfKind(graphLinkKinds()...) {
		for _, l := range g.scanEntityLinks(e) {
			g.addLink(l)
		}
	}
}

// beginLinkScan puts the graph into the state a background scan writes into:
// links arrive over time rather than all at once, and the forward and reverse
// indexes are built at the end from what arrived, so that no two goroutines
// are ever writing to the same map.
func (g *modGraph) beginLinkScan() {
	if g == nil {
		return
	}
	g.scanning = true
	g.links = nil
	g.out = make(map[string][]int)
	g.in = make(map[string][]int)
}

// finishLinkScan ends a scan and builds the indexes.
func (g *modGraph) finishLinkScan() {
	if g == nil {
		return
	}
	g.scanning = false
	g.linked = true
	g.reindex()
}

// addScannedLinks takes a batch from a scan. The links are resolved here,
// because the resolution is the part that reads the tree and has to happen
// before anything can be asked about them.
func (g *modGraph) addScannedLinks(links []graphLink) {
	if g == nil {
		return
	}
	for _, l := range links {
		g.addLink(l)
	}
}

// addLink records one reference. During a scan the link is only kept, because
// the indexes are built from the whole list once every batch has arrived; the
// rest of the time it is indexed as it goes.
func (g *modGraph) addLink(l graphLink) {
	if g.scanning {
		g.links = append(g.links, l)
		return
	}
	g.appendLink(l)
}

// appendLink puts a resolved link into the indexes and returns its index.
func (g *modGraph) appendLink(l graphLink) int {
	g.links = append(g.links, l)
	idx := len(g.links) - 1
	g.out[strings.ToLower(l.from)] = append(g.out[strings.ToLower(l.from)], idx)
	if l.to != "" {
		g.in[strings.ToLower(l.to)] = append(g.in[strings.ToLower(l.to)], idx)
	}
	return idx
}

// reindex rebuilds the forward and reverse indexes from the link list, which is
// what a scan needs once every batch has arrived.
func (g *modGraph) reindex() {
	g.out = make(map[string][]int, len(g.out))
	g.in = make(map[string][]int, len(g.in))
	for i := range g.links {
		l := &g.links[i]
		g.out[strings.ToLower(l.from)] = append(g.out[strings.ToLower(l.from)], i)
		if l.to != "" {
			g.in[strings.ToLower(l.to)] = append(g.in[strings.ToLower(l.to)], i)
		}
	}
}

// scanEntityLinks reads one entity's references without recording them, which
// is what a background scan does. The references are resolved here rather than
// in the file walk because resolution reads the tree, and doing it in the
// goroutine keeps the tree off the frame.
//
// The file is read without going through the cache: this runs on the scan's own
// goroutine, and the cache belongs to the frame, and a map written by two
// threads is a crash waiting for the second one.
func (g *modGraph) scanEntityLinks(e *graphEntity) []graphLink {
	rf := g.readRgd(e.path)
	if rf == nil {
		return nil
	}
	var out []graphLink
	collectGraphLinks(e, rf, func(l graphLink) {
		l.to = g.resolveIn(e, l.ref)
		out = append(out, l)
	})
	return out
}

// readRgd reads and parses a file without keeping it, which is safe to call
// from any thread.
func (g *modGraph) readRgd(virtualPath string) *rgd.RgdFile {
	if g == nil || g.tree == nil || virtualPath == "" {
		return nil
	}
	data, err := g.tree.ReadFile(virtualPath)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	return rf
}

// parse reads an entity's file. Results are kept, so the second caller of the
// same entity does not pay for it again.
func (g *modGraph) parse(e *graphEntity) *rgd.RgdFile {
	if e == nil || g.tree == nil {
		return nil
	}
	if rf, ok := g.parsed[strings.ToLower(e.path)]; ok {
		return rf
	}
	var rf *rgd.RgdFile
	if data, err := g.tree.ReadFile(e.path); err == nil {
		rf, _ = rgd.ParseRgd(data)
	}
	if g.parsed == nil {
		g.parsed = make(map[string]*rgd.RgdFile)
	}
	g.parsed[strings.ToLower(e.path)] = rf
	return rf
}

// Parsed returns the file of an entity, reading it on first use. It is how a
// caller that needs the fields of a unit — a freeze clearing a reference, an
// editor showing a table — gets them without reading the file twice.
func (g *modGraph) Parsed(path string) *rgd.RgdFile {
	if g == nil {
		return nil
	}
	return g.parse(g.entity(path))
}

// collectGraphLinks walks one parsed file and hands every field that
// graphRoleForField recognises to the caller, which is given the link because
// a background scan and a blocking read want them in different ways.
func collectGraphLinks(e *graphEntity, rf *rgd.RgdFile, emit func(graphLink)) {
	if rf == nil || rf.Root == nil || emit == nil {
		return
	}
	var walk func(entry *rgd.RgdEntry, path []string)
	walk = func(entry *rgd.RgdEntry, path []string) {
		if entry == nil {
			return
		}
		if !entry.IsTable() {
			role, slot, ok := graphRoleForField(e.kind, path)
			if !ok {
				return
			}
			ref := strings.TrimSpace(entry.DisplayValue())
			if ref == "" {
				// An empty slot is the normal state of almost every
				// slot in the game: a unit has ten ability slots and
				// usually two of them. It is not a broken reference.
				return
			}
			emit(graphLink{from: e.path, ref: ref, field: strings.Join(path, "."), slot: slot, role: role})
			return
		}
		for _, c := range entry.Children {
			walk(c, append(path, c.Name))
		}
	}
	for _, section := range rf.Root.Children {
		walk(section, []string{section.Name})
	}
}

// resolve turns a reference as written into a tree path, or "" when it names
// nothing that is there.
//
// The game writes references three ways, all of which occur in its own files:
// a path relative to Data\attrib ("research\chaos_furious_rage.lua"), a path
// relative to Data\attrib\ebps ("ebps\races\chaos\structures\chaos_hq.lua"),
// and a bare file name with neither folder nor extension ("chaos_hq"), which
// is what a good part of the requirement tables use. Rather than guess which
// one a given field means, every shape is tried and the first that exists wins.
func (g *modGraph) resolveIn(from *graphEntity, ref string) string {
	if g == nil || g.tree == nil {
		return ""
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	rel := strings.ReplaceAll(ref, "/", "\\")
	rel = strings.TrimPrefix(rel, "Data\\")
	stem := refStem(ref)
	// The .rgd form comes before the literal one everywhere: a reference
	// naming a .lua almost never has a .lua on disk, so the literal name
	// would only ever cost a wasted lookup.
	for _, c := range resolveCandidates(rel, stem) {
		if p := g.findDataPath(c); p != "" {
			return p
		}
	}
	// A bare name. A unit, a structure or a squad of the referring entity's
	// own race answers it before anything of another race, and before content
	// that has no race at all: "chaos_hq" in a Chaos building's requirement
	// table means the building, not a weapon that happens to share a name.
	list := g.byName[strings.ToLower(stem)]
	if from != nil {
		for _, cand := range list {
			if cand.race != "" && strings.EqualFold(cand.race, from.race) {
				return cand.path
			}
		}
	}
	if len(list) > 0 {
		return list[0].path
	}
	return ""
}

// resolveCandidates lists the relative paths a reference might be written as,
// most specific first.
func resolveCandidates(rel, stem string) []string {
	asRgd := stem + ".rgd"
	out := []string{rel, asRgd}
	for _, base := range []string{`ebps\`, `sbps\`} {
		out = append(out, base+rel, base+asRgd)
	}
	return out
}

// findDataPath resolves a path relative to Data\attrib, then as a full tree
// path, and returns the first that exists.
func (g *modGraph) findDataPath(rel string) string {
	if rel == "" {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(rel), ".rgd") {
		rel += ".rgd"
	}
	for _, candidate := range []string{filepath.Join("Data", graphRoot, rel), rel} {
		if g.tree.FindFile(candidate) != nil {
			return candidate
		}
	}
	return ""
}

// refStem is a reference's file name with neither its folder nor its
// extension, which is how the game names the thing it points at.
func refStem(ref string) string {
	base := strings.TrimSpace(ref)
	base = strings.ReplaceAll(base, "/", "\\")
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// ── queries ─────────────────────────────────────────────────────────────────

// Outgoing returns the references the entity at path holds, in file order.
//
// While a background scan is running it returns what has arrived so far, which
// is what the window wants: a frame must not wait for six hundred files. When
// no scan is running it reads the mod itself, so that a caller that just wants
// the answer — a test, a freeze — is never quietly handed an empty list
// because nobody had started a scan.
func (g *modGraph) Outgoing(path string) []graphLink {
	if g == nil {
		return nil
	}
	if !g.scanning {
		g.ensureLinks()
	}
	return g.collect(g.out[strings.ToLower(path)])
}

// Incoming returns the references other entities hold to the entity at path.
//
// This is the direction a freeze needs: what has to be cleared before the
// entity can be moved out of the mod.
func (g *modGraph) Incoming(path string) []graphLink {
	if g == nil {
		return nil
	}
	if !g.scanning {
		g.ensureLinks()
	}
	return g.collect(g.in[strings.ToLower(path)])
}

// LinksOfRole returns the outgoing links of one role, which is how a caller
// asks "what does this unit build" without walking everything.
func (g *modGraph) LinksOfRole(path string, role graphRole) []graphLink {
	var out []graphLink
	for _, l := range g.Outgoing(path) {
		if l.role == role {
			out = append(out, l)
		}
	}
	return out
}

func (g *modGraph) collect(idxs []int) []graphLink {
	if len(idxs) == 0 {
		return nil
	}
	out := make([]graphLink, 0, len(idxs))
	for _, i := range idxs {
		if i >= 0 && i < len(g.links) {
			out = append(out, g.links[i])
		}
	}
	return out
}

// Produces returns the units and buildings the structure at path is what
// builds.
//
// This is not read from the building: no building says what it builds. Each
// unit names the structure that makes it in its requirement table, so the
// answer is the requirement links pointing at this building, read backwards.
func (g *modGraph) Produces(path string) []string {
	var out []string
	for _, l := range g.Incoming(path) {
		if l.role == roleBuiltBy && l.from != path {
			out = append(out, l.from)
		}
	}
	return dedupeStrings(out)
}

// ReferencesTo returns the entities that point at the entity at path for one
// role, which is how "what breaks if I delete this ability" is answered.
func (g *modGraph) ReferencesTo(path string, role graphRole) []graphLink {
	var out []graphLink
	for _, l := range g.Incoming(path) {
		if l.role == role {
			out = append(out, l)
		}
	}
	return out
}

// BrokenLinks returns every reference that resolves to nothing, in the order
// they were found.
func (g *modGraph) BrokenLinks() []graphLink {
	if g == nil {
		return nil
	}
	if !g.scanning {
		g.ensureLinks()
	}
	var out []graphLink
	for _, l := range g.links {
		if !l.resolved() {
			out = append(out, l)
		}
	}
	return out
}

// BrokenLinksOf returns the broken references held by one entity, which is what
// the object window shows under a single unit.
func (g *modGraph) BrokenLinksOf(path string) []graphLink {
	var out []graphLink
	for _, l := range g.Outgoing(path) {
		if !l.resolved() {
			out = append(out, l)
		}
	}
	return out
}

// Orphans returns the entities of a kind that nothing points at, which for a
// unit means it is in the mod and in no build order at all.
//
// A requirement link only counts when it is the one that names a structure:
// research and addon requirements say what unlocks a unit, not what makes it,
// and a unit that is only unlocked and never built is normal.
func (g *modGraph) Orphans(kinds ...graphKind) []string {
	if g == nil {
		return nil
	}
	if !g.scanning {
		g.ensureLinks()
	}
	referenced := make(map[string]bool)
	for _, l := range g.links {
		if l.resolved() {
			referenced[strings.ToLower(l.to)] = true
		}
	}
	// A unit is produced by a structure, so a unit that is required by
	// nothing but is pointed at by some other unit's build requirement is not
	// an orphan.
	produced := make(map[string]bool)
	for _, l := range g.links {
		if l.role == roleBuiltBy && l.resolved() {
			produced[strings.ToLower(l.from)] = true
		}
	}
	var out []string
	for _, e := range g.entitiesOfKind(kinds...) {
		key := strings.ToLower(e.path)
		if referenced[key] || produced[key] {
			continue
		}
		out = append(out, e.path)
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}
