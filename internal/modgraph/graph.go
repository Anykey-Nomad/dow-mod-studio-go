package modgraph

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
	GraphRoot = "attrib"
)

// graphKind is what an entity is, decided by where it sits in the tree. The
// game's own folders are the only description of this there is.
type GraphKind int

const (
	GraphKindUnit GraphKind = iota
	GraphKindStructure
	GraphKindSquad
	GraphKindWeapon
	GraphKindAbility
	GraphKindAddon
	GraphKindResearch
	GraphKindModifier
	GraphKindRequirement
	GraphKindFormation
	GraphKindEnvironment
	GraphKindSchema
	GraphKindOther
)

// label is what the UI calls the kind.
func (k GraphKind) Label() string {
	switch k {
	case GraphKindUnit:
		return "Unit"
	case GraphKindStructure:
		return "Structure"
	case GraphKindSquad:
		return "Squad"
	case GraphKindWeapon:
		return "Weapon"
	case GraphKindAbility:
		return "Ability"
	case GraphKindAddon:
		return "Addon"
	case GraphKindResearch:
		return "Research"
	case GraphKindModifier:
		return "Modifier"
	case GraphKindRequirement:
		return "Requirement"
	case GraphKindFormation:
		return "Formation"
	case GraphKindEnvironment:
		return "Environment"
	case GraphKindSchema:
		return "Schema"
	}
	return "Other"
}

// graphEntity is one .rgd of the mod, as the graph knows it.
type GraphEntity struct {
	// path is the virtual tree path, e.g.
	// "Data\attrib\ebps\races\necrons\troops\necron_wraith.rgd".
	Path string
	// name is the file name without its extension.
	Name string
	// kind is what the entity is.
	Kind GraphKind
	// race is the folder name under "races" for the kinds that have one.
	Race string
	// file is the tree entry it came from, which knows whether the mod owns
	// it and where it physically lives.
	File *modtree.TreeFile
}

// graphRole is what a link means to the entity that holds it, said from that
// entity's point of view.
type GraphRole int

const (
	RoleWeapon GraphRole = iota
	RoleAbility
	RoleAddon
	RoleResearch
	RoleSquad
	RoleBuiltBy
	RoleNeedsAddon
	RoleNeedsResearch
	RoleBlueprint
	RoleRelocation
	RoleEffect
	RoleModifies
	RoleSpawns
	RoleProjectile
	RoleNeedsStructure
	RoleExcludesAddon
)

// label is what the UI calls the role, and the wording matters: a link is read
// from the entity that holds it, so "built by" is right and "builds" would be
// backwards.
func (r GraphRole) Label() string {
	switch r {
	case RoleWeapon:
		return "weapon"
	case RoleAbility:
		return "ability"
	case RoleAddon:
		return "addon"
	case RoleResearch:
		return "research"
	case RoleSquad:
		return "squad"
	case RoleBuiltBy:
		return "built by"
	case RoleNeedsAddon:
		return "needs addon"
	case RoleNeedsResearch:
		return "needs research"
	case RoleBlueprint:
		return "entrenched as"
	case RoleRelocation:
		return "relocates as"
	case RoleEffect:
		return "death effect"
	case RoleModifies:
		return "modifies"
	case RoleSpawns:
		return "spawns"
	case RoleProjectile:
		return "fires"
	case RoleNeedsStructure:
		return "needs structure"
	case RoleExcludesAddon:
		return "excludes addon"
	}
	return "refers to"
}

// isRequirement is true of the roles that come out of a requirements table,
// which is where the game's own "only buildable if" rules live.
func (r GraphRole) IsRequirement() bool {
	return r == RoleBuiltBy || r == RoleNeedsAddon || r == RoleNeedsResearch
}

// graphLink is one reference from one entity to another.
type GraphLink struct {
	// from is the virtual path of the entity holding the reference.
	From string
	// to is the virtual path it resolves to, or "" when it resolves to
	// nothing at all — a dangling reference, which is the whole point of
	// looking.
	To string
	// ref is the reference exactly as the file writes it, e.g.
	// "weapon\chaos_bolt_pistol_champion.lua" or the bare "chaos_hq".
	Ref string
	// field is the dotted path of the field holding it, e.g.
	// "ability_ext.abilities.ability_01", which is what a freeze has to
	// find again to clear it.
	Field string
	// slot is the numbered slot the reference sits in, e.g. "ability_01",
	// or the hardpoint and weapon pair for a weapon. It is the part of
	// field that says which of several similar references this is.
	Slot string
	// role is what the reference means.
	Role GraphRole
}

// resolved reports whether the link found a file.
func (l GraphLink) Resolved() bool { return l.To != "" }

// targetName is what the link points at, as a name, whether or not it resolved.
func (l GraphLink) TargetName() string { return RefStem(l.Ref) }

// modGraph is the whole of Data\attrib, indexed.
type ModGraph struct {
	Tree *modtree.ModTree

	// entities is every .rgd under Data\attrib, in tree order.
	Entities []*GraphEntity
	// byPath indexes the entities by their lower cased virtual path.
	ByPath map[string]*GraphEntity
	// byName indexes them by lower cased file stem, because references are
	// often written as a bare name and a name is all there is to go on.
	ByName map[string][]*GraphEntity

	// links is every reference found, once ensureLinks has run.
	Links []GraphLink
	// out and in index the links by their source and their target, so both
	// directions of a relationship are one lookup.
	Out map[string][]int
	In  map[string][]int
	// linked is set once the unit and building files have been read.
	Linked bool
	// scanning is set while a background scan is still writing batches in, so
	// that the indexes are built once at the end rather than by two threads at
	// once.
	Scanning bool
	// parsed holds the files already read, so the freeze that rewrites a
	// reference and the report that found it agree on the same parse.
	ParsedFiles map[string]*rgd.RgdFile
}

// newModGraph indexes the tree without reading a single file's contents: the
// folder layout alone says what every .rgd is. Reading the ~580 unit and
// building files to find their references is left to ensureLinks, because that
// is the expensive half and not every caller needs it.
func NewModGraph(Tree *modtree.ModTree) *ModGraph {
	g := &ModGraph{
		Tree:   Tree,
		ByPath: make(map[string]*GraphEntity),
		ByName: make(map[string][]*GraphEntity),
		Out:    make(map[string][]int),
		In:     make(map[string][]int),
	}
	if Tree == nil || Tree.Root == nil {
		return g
	}
	root := modtree.FindFolderByPath(Tree.Root, GraphRoot)
	if root == nil {
		return g
	}
	modtree.SearchWalk(root, func(f *modtree.TreeFile) {
		if f == nil || !strings.EqualFold(filepath.Ext(f.Name), ".rgd") {
			return
		}
		e := &GraphEntity{
			Path: f.FullPath,
			Name: strings.TrimSuffix(f.Name, filepath.Ext(f.Name)),
			File: f,
		}
		e.Kind, e.Race = ClassifyGraphEntity(f.FullPath)
		g.Entities = append(g.Entities, e)
		lower := strings.ToLower(f.FullPath)
		g.ByPath[lower] = e
		lowerName := strings.ToLower(e.Name)
		g.ByName[lowerName] = append(g.ByName[lowerName], e)
	})
	return g
}

// entity looks an entity up by virtual path.
func (g *ModGraph) Entity(Path string) *GraphEntity {
	if g == nil {
		return nil
	}
	return g.ByPath[strings.ToLower(Path)]
}

// entitiesOfKind returns every entity of one kind, in tree order.
func (g *ModGraph) EntitiesOfKind(kinds ...GraphKind) []*GraphEntity {
	want := make(map[GraphKind]bool, len(kinds))
	for _, k := range kinds {
		want[k] = true
	}
	var Out []*GraphEntity
	for _, e := range g.Entities {
		if want[e.Kind] {
			Out = append(Out, e)
		}
	}
	return Out
}

// ── classification ──────────────────────────────────────────────────────────

// classifyGraphEntity decides what a .rgd is from its path, and which race it
// belongs to. The folder names are the game's own; there is no marker inside
// the files that would say.
func ClassifyGraphEntity(Path string) (GraphKind, string) {
	parts := strings.Split(strings.ReplaceAll(Path, "/", "\\"), "\\")
	// parts[0] is "Data", parts[1] is "attrib"; anything else is not data
	// we index.
	if len(parts) < 3 || !strings.EqualFold(parts[1], GraphRoot) {
		return GraphKindOther, ""
	}
	folder := strings.ToLower(parts[2])
	switch folder {
	case "weapon":
		return GraphKindWeapon, ""
	case "abilities":
		return GraphKindAbility, ""
	case "addons":
		return GraphKindAddon, ""
	case "research":
		return GraphKindResearch, ""
	case "modifiers":
		return GraphKindModifier, ""
	case "requirements":
		return GraphKindRequirement, ""
	case "formations":
		return GraphKindFormation, ""
	case "sbps":
		// sbps\races\<race>\<squad>.rgd
		if len(parts) >= 5 && strings.EqualFold(parts[3], "races") {
			return GraphKindSquad, parts[4]
		}
		return GraphKindOther, ""
	case "ebps":
		// ebps\races\<race>\{troops,structures}\<unit>.rgd
		if len(parts) >= 6 && strings.EqualFold(parts[3], "races") {
			switch strings.ToLower(parts[5]) {
			case "troops":
				return GraphKindUnit, parts[4]
			case "structures":
				return GraphKindStructure, parts[4]
			}
		}
		// environment and game hold the props, projectiles and relocation
		// blueprints; everything else under ebps is one of those two.
		if len(parts) >= 4 {
			switch strings.ToLower(parts[3]) {
			case "environment", "game":
				return GraphKindEnvironment, ""
			}
		}
		return GraphKindOther, ""
	}
	// ebpextensions, sbpextensions, tables, tuning, racebps and every
	// type_* folder hold the definitions the rest of the data is written
	// against. They are the schema, not content.
	return GraphKindSchema, ""
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
func GraphRoleForField(Kind GraphKind, Path []string) (GraphRole, string, bool) {
	// combat_ext.hardpoints.hardpoint_NN.weapon_table.weapon_NN.weapon
	if len(Path) == 6 && Eq(Path[0], "combat_ext") && Eq(Path[1], "hardpoints") &&
		Eq(Path[3], "weapon_table") && Eq(Path[5], "weapon") {
		return RoleWeapon, Path[2] + "/" + Path[4], true
	}
	// The four numbered slot tables. Each is one section, one table, and
	// the slot itself.
	if len(Path) == 3 {
		switch {
		case Eq(Path[0], "ability_ext") && Eq(Path[1], "abilities"):
			return RoleAbility, Path[2], true
		case Eq(Path[0], "addon_ext") && Eq(Path[1], "addons"):
			return RoleAddon, Path[2], true
		case Eq(Path[0], "research_ext") && Eq(Path[1], "research_table"):
			return RoleResearch, Path[2], true
		case Eq(Path[0], "spawner_ext") && Eq(Path[1], "squad_table"):
			return RoleSquad, Path[2], true
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
	if i := RequirementLeafIndex(Path); i > 0 {
		leaf := strings.ToLower(Path[i])
		Slot := Path[i-1]
		switch {
		case strings.Contains(leaf, "structure_name"):
			if Kind == GraphKindUnit || Kind == GraphKindStructure || Kind == GraphKindSquad {
				return RoleBuiltBy, Slot, true
			}
			return RoleNeedsStructure, Slot, true
		case leaf == "global_addon_name" || leaf == "addon_name" || leaf == "addon":
			return RoleNeedsAddon, Slot, true
		case leaf == "mutually_exclusive_with":
			return RoleExcludesAddon, Slot, true
		case strings.Contains(leaf, "research_name"):
			return RoleNeedsResearch, Slot, true
		case leaf == "squad_name":
			return RoleSquad, Slot, true
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
	if n := len(Path); n >= 2 && Eq(Path[n-1], "target_type_name") {
		for _, part := range Path[:n-1] {
			if Eq(part, "modifiers") {
				return RoleModifies, Path[n-2], true
			}
		}
		return 0, "", false
	}
	// An ability that fires another ability, and the entity it puts in the
	// world. Both are top-level fields of an ability file.
	if len(Path) == 1 {
		switch {
		case Eq(Path[0], "child_ability_name"):
			return RoleAbility, Path[0], true
		case Eq(Path[0], "spawned_entity_name"):
			return RoleSpawns, Path[0], true
		}
	}
	// The projectile a weapon or an ability sends, and the ability a weapon
	// triggers on impact. The projectile is a file under
	// ebps\environment\gameplay, so it is an entity like any other.
	if len(Path) == 1 {
		switch {
		case Eq(Path[0], "fired_projectile"), Eq(Path[0], "projectile_name"):
			return RoleProjectile, Path[0], true
		case Eq(Path[0], "impact_ability"):
			return RoleAbility, Path[0], true
		}
	}
	// A weapon that is really a group of other weapons. Chaos' advance
	// variants put three real weapons behind one slot, and swapping one out
	// has to know the other two.
	if len(Path) == 3 && Eq(Path[0], "group_weapon_table") && Eq(Path[2], "referenced_weapon_name") {
		return RoleWeapon, Path[1], true
	}
	// A unit that has a second, entrenched version of itself.
	if len(Path) == 2 && Eq(Path[0], "entrench_ext") && Eq(Path[1], "entrenched_blueprint_name") {
		return RoleBlueprint, Path[1], true
	}
	// The building a relocation leaves behind: when a structure moves, the
	// game stands a dummy in its place, and that dummy is another unit. It is
	// named in a bare name, and it is a reference like any other — a building
	// that is moved out of the mod must not leave a dummy pointing at it.
	if len(Path) == 2 && Eq(Path[0], "relocatee_ext") && Eq(Path[1], "dummy_structure_name") {
		return RoleRelocation, Path[1], true
	}
	// What a corpse leaves behind.
	if len(Path) == 2 && Eq(Path[0], "health_ext") {
		switch strings.ToLower(Path[1]) {
		case "spawn_death_blossom", "spawn_blood_splat_on_death", "spawn_usable_body_on_death":
			return RoleEffect, Path[1], true
		}
	}
	return 0, "", false
}

func Eq(a, b string) bool { return strings.EqualFold(a, b) }

// requirementLeafIndex finds the field of a requirement slot in a dotted path,
// or returns -1 when the path is not one.
//
// The two shapes the game writes are "requirements.required_1.structure_name"
// and "requirement_ext.requirements.required_1.structure_name", and the
// difference is one element. Rather than test for both, this walks for the
// "requirements" table and takes the field after the slot that follows it.
func RequirementLeafIndex(Path []string) int {
	for i := 0; i+2 < len(Path); i++ {
		if Eq(Path[i], "requirements") && strings.HasPrefix(strings.ToLower(Path[i+1]), "required") {
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
func GraphLinkKinds() []GraphKind {
	return []GraphKind{
		GraphKindUnit,
		GraphKindStructure,
		GraphKindAbility,
		GraphKindAddon,
		GraphKindWeapon,
		GraphKindSquad,
		GraphKindResearch,
	}
}

// ensureLinks reads every referring file once and records the references it
// finds. It is safe to call as often as you like: the work happens once.
//
// It is the blocking form, used by the tests and by anything that is not on a
// frame. A window that wants the links should start a scan instead, because
// reading three thousand files takes seconds and a frame cannot wait that long.
func (g *ModGraph) EnsureLinks() {
	if g == nil || g.Linked {
		return
	}
	g.Linked = true
	if g.Tree == nil {
		return
	}
	for _, e := range g.EntitiesOfKind(GraphLinkKinds()...) {
		for _, l := range g.ScanEntityLinks(e) {
			g.AddLink(l)
		}
	}
}

// beginLinkScan puts the graph into the state a background scan writes into:
// links arrive over time rather than all at once, and the forward and reverse
// indexes are built at the end from what arrived, so that no two goroutines
// are ever writing to the same map.
func (g *ModGraph) BeginLinkScan() {
	if g == nil {
		return
	}
	g.Scanning = true
	g.Links = nil
	g.Out = make(map[string][]int)
	g.In = make(map[string][]int)
}

// finishLinkScan ends a scan and builds the indexes.
func (g *ModGraph) FinishLinkScan() {
	if g == nil {
		return
	}
	g.Scanning = false
	g.Linked = true
	g.Reindex()
}

// addScannedLinks takes a batch from a scan. The links are resolved here,
// because the resolution is the part that reads the tree and has to happen
// before anything can be asked about them.
func (g *ModGraph) AddScannedLinks(Links []GraphLink) {
	if g == nil {
		return
	}
	for _, l := range Links {
		g.AddLink(l)
	}
}

// addLink records one reference. During a scan the link is only kept, because
// the indexes are built from the whole list once every batch has arrived; the
// rest of the time it is indexed as it goes.
func (g *ModGraph) AddLink(l GraphLink) {
	if g.Scanning {
		g.Links = append(g.Links, l)
		return
	}
	g.AppendLink(l)
}

// appendLink puts a resolved link into the indexes and returns its index.
func (g *ModGraph) AppendLink(l GraphLink) int {
	g.Links = append(g.Links, l)
	idx := len(g.Links) - 1
	g.Out[strings.ToLower(l.From)] = append(g.Out[strings.ToLower(l.From)], idx)
	if l.To != "" {
		g.In[strings.ToLower(l.To)] = append(g.In[strings.ToLower(l.To)], idx)
	}
	return idx
}

// reindex rebuilds the forward and reverse indexes from the link list, which is
// what a scan needs once every batch has arrived.
func (g *ModGraph) Reindex() {
	g.Out = make(map[string][]int, len(g.Out))
	g.In = make(map[string][]int, len(g.In))
	for i := range g.Links {
		l := &g.Links[i]
		g.Out[strings.ToLower(l.From)] = append(g.Out[strings.ToLower(l.From)], i)
		if l.To != "" {
			g.In[strings.ToLower(l.To)] = append(g.In[strings.ToLower(l.To)], i)
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
func (g *ModGraph) ScanEntityLinks(e *GraphEntity) []GraphLink {
	rf := g.ReadRgd(e.Path)
	if rf == nil {
		return nil
	}
	var Out []GraphLink
	CollectGraphLinks(e, rf, func(l GraphLink) {
		l.To = g.ResolveIn(e, l.Ref)
		Out = append(Out, l)
	})
	return Out
}

// readRgd reads and parses a file without keeping it, which is safe to call
// from any thread.
func (g *ModGraph) ReadRgd(virtualPath string) *rgd.RgdFile {
	if g == nil || g.Tree == nil || virtualPath == "" {
		return nil
	}
	data, err := g.Tree.ReadFile(virtualPath)
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
func (g *ModGraph) Parse(e *GraphEntity) *rgd.RgdFile {
	if e == nil || g.Tree == nil {
		return nil
	}
	if rf, ok := g.ParsedFiles[strings.ToLower(e.Path)]; ok {
		return rf
	}
	var rf *rgd.RgdFile
	if data, err := g.Tree.ReadFile(e.Path); err == nil {
		rf, _ = rgd.ParseRgd(data)
	}
	if g.ParsedFiles == nil {
		g.ParsedFiles = make(map[string]*rgd.RgdFile)
	}
	g.ParsedFiles[strings.ToLower(e.Path)] = rf
	return rf
}

// Parsed returns the file of an entity, reading it on first use. It is how a
// caller that needs the fields of a unit — a freeze clearing a reference, an
// editor showing a table — gets them without reading the file twice.
func (g *ModGraph) Parsed(Path string) *rgd.RgdFile {
	if g == nil {
		return nil
	}
	return g.Parse(g.Entity(Path))
}

// collectGraphLinks walks one parsed file and hands every field that
// graphRoleForField recognises to the caller, which is given the link because
// a background scan and a blocking read want them in different ways.
func CollectGraphLinks(e *GraphEntity, rf *rgd.RgdFile, emit func(GraphLink)) {
	if rf == nil || rf.Root == nil || emit == nil {
		return
	}
	var walk func(entry *rgd.RgdEntry, Path []string)
	walk = func(entry *rgd.RgdEntry, Path []string) {
		if entry == nil {
			return
		}
		if !entry.IsTable() {
			Role, Slot, ok := GraphRoleForField(e.Kind, Path)
			if !ok {
				return
			}
			Ref := strings.TrimSpace(entry.DisplayValue())
			if Ref == "" {
				// An empty slot is the normal state of almost every
				// slot in the game: a unit has ten ability slots and
				// usually two of them. It is not a broken reference.
				return
			}
			emit(GraphLink{From: e.Path, Ref: Ref, Field: strings.Join(Path, "."), Slot: Slot, Role: Role})
			return
		}
		for _, c := range entry.Children {
			walk(c, append(Path, c.Name))
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
func (g *ModGraph) ResolveIn(From *GraphEntity, Ref string) string {
	if g == nil || g.Tree == nil {
		return ""
	}
	Ref = strings.TrimSpace(Ref)
	if Ref == "" {
		return ""
	}
	rel := strings.ReplaceAll(Ref, "/", "\\")
	rel = strings.TrimPrefix(rel, "Data\\")
	stem := RefStem(Ref)
	// The .rgd form comes before the literal one everywhere: a reference
	// naming a .lua almost never has a .lua on disk, so the literal name
	// would only ever cost a wasted lookup.
	for _, c := range ResolveCandidates(rel, stem) {
		if p := g.FindDataPath(c); p != "" {
			return p
		}
	}
	// A bare name. A unit, a structure or a squad of the referring entity's
	// own race answers it before anything of another race, and before content
	// that has no race at all: "chaos_hq" in a Chaos building's requirement
	// table means the building, not a weapon that happens to share a name.
	list := g.ByName[strings.ToLower(stem)]
	if From != nil {
		for _, cand := range list {
			if cand.Race != "" && strings.EqualFold(cand.Race, From.Race) {
				return cand.Path
			}
		}
	}
	if len(list) > 0 {
		return list[0].Path
	}
	return ""
}

// resolveCandidates lists the relative paths a reference might be written as,
// most specific first.
func ResolveCandidates(rel, stem string) []string {
	asRgd := stem + ".rgd"
	Out := []string{rel, asRgd}
	for _, base := range []string{`ebps\`, `sbps\`} {
		Out = append(Out, base+rel, base+asRgd)
	}
	return Out
}

// findDataPath resolves a path relative to Data\attrib, then as a full tree
// path, and returns the first that exists.
func (g *ModGraph) FindDataPath(rel string) string {
	if rel == "" {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(rel), ".rgd") {
		rel += ".rgd"
	}
	for _, candidate := range []string{filepath.Join("Data", GraphRoot, rel), rel} {
		if g.Tree.FindFile(candidate) != nil {
			return candidate
		}
	}
	return ""
}

// refStem is a reference's file name with neither its folder nor its
// extension, which is how the game names the thing it points at.
func RefStem(Ref string) string {
	base := strings.TrimSpace(Ref)
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
func (g *ModGraph) Outgoing(Path string) []GraphLink {
	if g == nil {
		return nil
	}
	if !g.Scanning {
		g.EnsureLinks()
	}
	return g.Collect(g.Out[strings.ToLower(Path)])
}

// Incoming returns the references other entities hold to the entity at path.
//
// This is the direction a freeze needs: what has to be cleared before the
// entity can be moved out of the mod.
func (g *ModGraph) Incoming(Path string) []GraphLink {
	if g == nil {
		return nil
	}
	if !g.Scanning {
		g.EnsureLinks()
	}
	return g.Collect(g.In[strings.ToLower(Path)])
}

// LinksOfRole returns the outgoing links of one role, which is how a caller
// asks "what does this unit build" without walking everything.
func (g *ModGraph) LinksOfRole(Path string, Role GraphRole) []GraphLink {
	var Out []GraphLink
	for _, l := range g.Outgoing(Path) {
		if l.Role == Role {
			Out = append(Out, l)
		}
	}
	return Out
}

func (g *ModGraph) Collect(idxs []int) []GraphLink {
	if len(idxs) == 0 {
		return nil
	}
	Out := make([]GraphLink, 0, len(idxs))
	for _, i := range idxs {
		if i >= 0 && i < len(g.Links) {
			Out = append(Out, g.Links[i])
		}
	}
	return Out
}

// Produces returns the units and buildings the structure at path is what
// builds.
//
// This is not read from the building: no building says what it builds. Each
// unit names the structure that makes it in its requirement table, so the
// answer is the requirement links pointing at this building, read backwards.
func (g *ModGraph) Produces(Path string) []string {
	var Out []string
	for _, l := range g.Incoming(Path) {
		if l.Role == RoleBuiltBy && l.From != Path {
			Out = append(Out, l.From)
		}
	}
	return DedupeStrings(Out)
}

// ReferencesTo returns the entities that point at the entity at path for one
// role, which is how "what breaks if I delete this ability" is answered.
func (g *ModGraph) ReferencesTo(Path string, Role GraphRole) []GraphLink {
	var Out []GraphLink
	for _, l := range g.Incoming(Path) {
		if l.Role == Role {
			Out = append(Out, l)
		}
	}
	return Out
}

// BrokenLinks returns every reference that resolves to nothing, in the order
// they were found.
func (g *ModGraph) BrokenLinks() []GraphLink {
	if g == nil {
		return nil
	}
	if !g.Scanning {
		g.EnsureLinks()
	}
	var Out []GraphLink
	for _, l := range g.Links {
		if !l.Resolved() {
			Out = append(Out, l)
		}
	}
	return Out
}

// BrokenLinksOf returns the broken references held by one entity, which is what
// the object window shows under a single unit.
func (g *ModGraph) BrokenLinksOf(Path string) []GraphLink {
	var Out []GraphLink
	for _, l := range g.Outgoing(Path) {
		if !l.Resolved() {
			Out = append(Out, l)
		}
	}
	return Out
}

// Orphans returns the entities of a kind that nothing points at, which for a
// unit means it is in the mod and in no build order at all.
//
// A requirement link only counts when it is the one that names a structure:
// research and addon requirements say what unlocks a unit, not what makes it,
// and a unit that is only unlocked and never built is normal.
func (g *ModGraph) Orphans(kinds ...GraphKind) []string {
	if g == nil {
		return nil
	}
	if !g.Scanning {
		g.EnsureLinks()
	}
	referenced := make(map[string]bool)
	for _, l := range g.Links {
		if l.Resolved() {
			referenced[strings.ToLower(l.To)] = true
		}
	}
	// A unit is produced by a structure, so a unit that is required by
	// nothing but is pointed at by some other unit's build requirement is not
	// an orphan.
	produced := make(map[string]bool)
	for _, l := range g.Links {
		if l.Role == RoleBuiltBy && l.Resolved() {
			produced[strings.ToLower(l.From)] = true
		}
	}
	var Out []string
	for _, e := range g.EntitiesOfKind(kinds...) {
		key := strings.ToLower(e.Path)
		if referenced[key] || produced[key] {
			continue
		}
		Out = append(Out, e.Path)
	}
	sort.Strings(Out)
	return Out
}

func DedupeStrings(In []string) []string {
	seen := make(map[string]bool, len(In))
	Out := make([]string, 0, len(In))
	for _, s := range In {
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		Out = append(Out, s)
	}
	return Out
}
