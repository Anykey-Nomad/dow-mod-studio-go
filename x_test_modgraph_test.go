package main

import (
	"dow-mod-studio/internal/modtree"
	"path/filepath"
	"strings"
	"testing"
)

// The graph is measured against the game's own data, the same way the
// templates are: the installed units and buildings are the only description of
// what a reference is, so a claim about them is checked against all of them.

// graphOf builds the graph of the installed game.
func graphOf(t *testing.T) *modGraph {
	t.Helper()
	return newModGraph(openTestDXP3(t))
}

func TestTheGraphKnowsEveryKindOfThingTheGameShips(t *testing.T) {
	tree := openTestDXP3(t)
	g := newModGraph(tree)
	g.ensureLinks()

	// The expectation is read off the tree rather than written down, because
	// a number written down is only as good as the day it was written: this
	// asks whether every .rgd of a kind was found and classified as that
	// kind, which is the property that matters.
	attrib := modtree.FindFolderByPath(tree.Root, "attrib")
	if attrib == nil {
		t.Fatal("no attrib folder")
	}
	onDisk := map[graphKind]int{}
	modtree.SearchWalk(attrib, func(f *modtree.TreeFile) {
		if !strings.EqualFold(filepath.Ext(f.Name), ".rgd") {
			return
		}
		kind, _ := classifyGraphEntity(f.FullPath)
		onDisk[kind]++
	})
	for kind, n := range onDisk {
		if n == 0 {
			continue
		}
		got := len(g.entitiesOfKind(kind))
		if got != n {
			t.Errorf("%s: indexed %d, the tree holds %d", kind.label(), got, n)
		}
	}
	// The kinds that carry the mod's content have to be there at all, or the
	// counts above would be comparing zeroes.
	for _, kind := range []graphKind{graphKindUnit, graphKindStructure, graphKindSquad,
		graphKindWeapon, graphKindAbility, graphKindAddon, graphKindResearch, graphKindModifier} {
		if len(g.entitiesOfKind(kind)) == 0 {
			t.Errorf("%s: nothing indexed", kind.label())
		}
	}
}

func TestEveryUnitAndBuildingIsFoundAndKnowsItsRace(t *testing.T) {
	tree := openTestDXP3(t)
	g := newModGraph(tree)
	cat := buildUnitCatalog(tree)

	for _, u := range cat.allUnits() {
		e := g.entity(u.path)
		if e == nil {
			t.Fatalf("%s is in the catalogue but not in the graph", u.path)
		}
		want := graphKindUnit
		if u.kind == unitKindStructure {
			want = graphKindStructure
		}
		if e.kind != want {
			t.Errorf("%s: graph says %s, the folder says %s", u.path, e.kind.label(), u.kind.label)
		}
		if !strings.EqualFold(e.race, u.race) {
			t.Errorf("%s: graph race %q, folder race %q", u.path, e.race, u.race)
		}
	}
}

func TestEveryReferenceRoleIsFoundInTheGamesOwnFiles(t *testing.T) {
	g := graphOf(t)
	g.ensureLinks()
	// Each role is pinned to the number of references the installed game
	// actually holds, counted by the extractor rather than written down from
	// memory. A rule that silently stopped matching would leave a freeze with
	// work it did not know about, so the count is the assertion.
	//
	// The numbers are for the whole referring set — the 604 units and
	// buildings, and the 2129 abilities, add-ons, weapons, squads and research
	// files besides. Most of the growth from the unit-only scan is
	// roleModifies: a research file names the units its upgrade applies to,
	// and a mod author who gets that name wrong gets an upgrade that does
	// nothing at all, with nothing in the game to say so.
	want := map[graphRole]int{
		roleWeapon:         1732,
		roleAbility:        1012,
		roleEffect:         747,
		roleSquad:          324,
		roleResearch:       293,
		roleBuiltBy:        692,
		roleAddon:          142,
		roleNeedsAddon:     494,
		roleNeedsResearch:  464,
		roleNeedsStructure: 426,
		roleModifies:       1407,
		roleProjectile:     165,
		roleBlueprint:      2,
		roleRelocation:     17,
		roleExcludesAddon:  2,
		roleSpawns:         14,
	}
	got := map[graphRole]int{}
	for _, l := range g.links {
		got[l.role]++
	}
	for role, n := range want {
		if got[role] != n {
			t.Errorf("role %q: found %d references, the files hold %d", role.label(), got[role], n)
		}
	}
	for role, n := range got {
		if _, ok := want[role]; !ok && n > 0 {
			t.Errorf("role %d: %d references matched a rule that is not in the table", role, n)
		}
	}
	if total := len(g.links); total != 7933 {
		t.Errorf("read %d references in total, the files hold 7933", total)
	}
}

func TestEveryReferenceInTheGamesOwnDataResolves(t *testing.T) {
	g := graphOf(t)
	g.ensureLinks()

	// Not "almost every": all of them. 7933 references across the 2733 files
	// that hold any, and every one names a file that is there. The harder half
	// of that is the bare names — "chaos_hq", "relocate_hq" — which the
	// requirement tables are full of and which a path-only lookup cannot
	// resolve; they are found by name within the referring entity's race.
	broken := g.BrokenLinks()
	for _, l := range broken {
		t.Errorf("unresolved reference %q (%s) in %s at %s", l.ref, l.role.label(), l.from, l.field)
	}
	if len(g.links) < 7000 {
		t.Errorf("only %d references were read out of the game's data", len(g.links))
	}
}

// A reference the game writes as a bare name is found by name, and the
// referring entity's own race is what disambiguates it.
func TestABareNameReferenceIsResolvedWithinTheReferringRace(t *testing.T) {
	tree := openTestDXP3(t)
	g := newModGraph(tree)

	// The eldar HQ leaves a dummy building behind when it relocates, and that
	// dummy is named in a bare name — no folder, no extension — which no
	// path-shaped lookup can find.
	links := g.LinksOfRole(`Data\attrib\ebps\races\eldar\structures\eldar_hq.rgd`, roleRelocation)
	if len(links) != 1 {
		t.Fatalf("eldar_hq has %d relocation references, want 1", len(links))
	}
	if !strings.EqualFold(links[0].ref, "relocate_hq") {
		t.Errorf("eldar_hq relocates as %q, want relocate_hq", links[0].ref)
	}
	if !links[0].resolved() {
		t.Error("the bare name relocate_hq did not resolve")
	}
}

func TestTheWeaponsOfAUnitResolveToTheWeaponLibrary(t *testing.T) {
	tree := openTestDXP3(t)
	g := newModGraph(tree)

	// necron_lord is a unit of the game with weapons, abilities and a
	// requirement, so it exercises every part of the extractor at once.
	const unit = `Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`
	weapons := g.LinksOfRole(unit, roleWeapon)
	if len(weapons) == 0 {
		t.Fatal("necron_lord has no weapons in the graph")
	}
	for _, w := range weapons {
		if !w.resolved() {
			t.Errorf("weapon %q does not resolve", w.ref)
			continue
		}
		e := g.entity(w.to)
		if e == nil || e.kind != graphKindWeapon {
			t.Errorf("weapon %q resolves to %s, which is not a weapon", w.ref, w.to)
		}
	}
}

func TestABuildingIsWhatBuildsTheUnitsThatRequireIt(t *testing.T) {
	tree := openTestDXP3(t)
	g := newModGraph(tree)

	// Every unit of the game says, in its requirement table, which structure
	// builds it. Reading that backwards has to give the army the game ships,
	// so the count is checked against the files rather than against a guess.
	const armoury = `Data\attrib\ebps\races\chaos\structures\chaos_armoury.rgd`
	built := g.Produces(armoury)
	if len(built) == 0 {
		t.Fatal("the chaos armoury builds nothing in the graph")
	}
	var units int
	for _, p := range built {
		e := g.entity(p)
		if e == nil {
			t.Errorf("%s is listed as built by the armoury but is not in the graph", p)
			continue
		}
		switch e.kind {
		case graphKindUnit:
			units++
		case graphKindStructure:
			// A building can require another building: the Chaos turrets
			// name the armoury, and that is a real dependency the freeze
			// has to clear.
		default:
			t.Errorf("%s is listed as built by the armoury but is a %s", p, e.kind.label())
		}
		// The link has to be a real one in that file, not something the
		// graph invented.
		found := false
		for _, l := range g.Outgoing(p) {
			if l.role == roleBuiltBy && strings.EqualFold(l.to, armoury) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is listed as built by the armoury but its own file says otherwise", p)
		}
	}
	if units == 0 {
		t.Error("the armoury builds no units at all, which cannot be right")
	}

	// And the two directions have to agree everywhere: every unit produced by
	// a building is one whose file names that building.
	for _, e := range g.entitiesOfKind(graphKindStructure) {
		for _, p := range g.Produces(e.path) {
			if e2 := g.entity(p); e2 != nil && !strings.EqualFold(e2.race, e.race) {
				t.Errorf("%s builds %s, which is a %s unit", e.name, e2.name, e2.race)
			}
		}
	}
}

func TestTheTwoDirectionsOfTheGraphAreInverses(t *testing.T) {
	g := graphOf(t)
	g.ensureLinks()

	for _, e := range g.entitiesOfKind(graphKindUnit, graphKindStructure) {
		for _, l := range g.Outgoing(e.path) {
			if !l.resolved() {
				continue
			}
			var back bool
			for _, r := range g.Incoming(l.to) {
				if strings.EqualFold(r.from, l.from) && r.field == l.field && r.role == l.role {
					back = true
				}
			}
			if !back {
				t.Errorf("%s -> %s at %s is in the forward index but not the reverse one", e.name, l.to, l.field)
			}
		}
	}
}

func TestRefStemIsTheGamesOwnNameForAThing(t *testing.T) {
	cases := map[string]string{
		`weapon\chaos_bolt_pistol_champion.lua`:    "chaos_bolt_pistol_champion",
		`ebps\races\chaos\structures\chaos_hq.lua`: "chaos_hq",
		"chaos_hq":                    "chaos_hq",
		`addons\chaos_hq_addon_1.lua`: "chaos_hq_addon_1",
		``:                            "",
	}
	for in, want := range cases {
		if got := refStem(in); got != want {
			t.Errorf("refStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyGraphEntityReadsTheGamesOwnFolders(t *testing.T) {
	cases := []struct {
		path string
		kind graphKind
		race string
	}{
		{`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`, graphKindUnit, "necrons"},
		{`Data\attrib\ebps\races\chaos\structures\chaos_hq.rgd`, graphKindStructure, "chaos"},
		{`Data\attrib\sbps\races\chaos\chaos_squad_cultist.rgd`, graphKindSquad, "chaos"},
		{`Data\attrib\weapon\guard_laspistol_psyker.rgd`, graphKindWeapon, ""},
		{`Data\attrib\abilities\chaos_furious_rage.rgd`, graphKindAbility, ""},
		{`Data\attrib\addons\chaos_hq_addon_1.rgd`, graphKindAddon, ""},
		{`Data\attrib\research\chaos_bloodthirster_research.rgd`, graphKindResearch, ""},
		{`Data\attrib\modifiers\no_modifier.rgd`, graphKindModifier, ""},
		{`Data\attrib\requirements\no_requirement.rgd`, graphKindRequirement, ""},
		{`Data\attrib\formations\chaos_formation.rgd`, graphKindFormation, ""},
		{`Data\attrib\ebps\environment\gameplay\blood_splat.rgd`, graphKindEnvironment, ""},
		{`Data\attrib\ebpextensions\combat_ext.rgd`, graphKindSchema, ""},
		{`Data\attrib\tables\weapon_table.rgd`, graphKindSchema, ""},
		{`Data\attrib\type_armour\tp_armour.rgd`, graphKindSchema, ""},
		{`Data\art\models\whatever.rgd`, graphKindOther, ""},
	}
	for _, c := range cases {
		kind, race := classifyGraphEntity(c.path)
		if kind != c.kind || race != c.race {
			t.Errorf("classify(%q) = %s/%q, want %s/%q", c.path, kind.label(), race, c.kind.label(), c.race)
		}
	}
}

func TestGraphRoleForFieldCoversTheShapesTheGameWrites(t *testing.T) {
	cases := []struct {
		kind graphKind
		path []string
		role graphRole
		ok   bool
	}{
		{graphKindUnit, []string{"combat_ext", "hardpoints", "hardpoint_01", "weapon_table", "weapon_01", "weapon"}, roleWeapon, true},
		{graphKindUnit, []string{"ability_ext", "abilities", "ability_01"}, roleAbility, true},
		{graphKindUnit, []string{"addon_ext", "addons", "addon_02"}, roleAddon, true},
		{graphKindUnit, []string{"research_ext", "research_table", "research_03"}, roleResearch, true},
		{graphKindUnit, []string{"spawner_ext", "squad_table", "squad_07"}, roleSquad, true},
		{graphKindUnit, []string{"requirement_ext", "requirements", "required_1", "structure_name"}, roleBuiltBy, true},
		{graphKindUnit, []string{"requirement_ext", "requirements", "required_11", "structure_name_or"}, roleBuiltBy, true},
		{graphKindUnit, []string{"requirement_ext", "requirements", "required_1", "global_addon_name"}, roleNeedsAddon, true},
		{graphKindUnit, []string{"possess_ext", "requirements", "required_1", "research_name"}, roleNeedsResearch, true},
		{graphKindUnit, []string{"entrench_ext", "entrenched_blueprint_name"}, roleBlueprint, true},
		{graphKindUnit, []string{"relocatee_ext", "dummy_structure_name"}, roleRelocation, true},
		{graphKindUnit, []string{"health_ext", "spawn_death_blossom"}, roleEffect, true},
		// A structure named in a unit's own requirement table is who builds it;
		// the same word in an add-on's or a weapon's is the building that has
		// to be finished first. Same field, opposite direction, and the graph
		// has to keep them apart or Produces() reads the wrong way round.
		{graphKindAddon, []string{"requirements", "required_1", "structure_name"}, roleNeedsStructure, true},
		{graphKindAddon, []string{"requirements", "required_10", "structure_name_either"}, roleNeedsStructure, true},
		{graphKindAbility, []string{"requirements", "required_1", "required_structure_name"}, roleNeedsStructure, true},
		{graphKindSquad, []string{"squad_requirement_ext", "requirements", "required_1", "structure_name"}, roleBuiltBy, true},
		// The requirement table hung off the top level, as abilities, add-ons,
		// weapons and research all write it.
		{graphKindAddon, []string{"requirements", "required_1", "addon_name"}, roleNeedsAddon, true},
		{graphKindAddon, []string{"requirements", "required_11", "addon"}, roleNeedsAddon, true},
		{graphKindAddon, []string{"requirements", "required_1", "mutually_exclusive_with"}, roleExcludesAddon, true},
		{graphKindResearch, []string{"requirements", "required_11", "research_name_or"}, roleNeedsResearch, true},
		{graphKindAbility, []string{"requirements", "required_1", "squad_name"}, roleSquad, true},
		// What a modifier applies to, in every file that carries one.
		{graphKindResearch, []string{"modifiers", "modifier_01", "target_type_name"}, roleModifies, true},
		{graphKindAddon, []string{"modifiers", "modifier_02", "target_type_name"}, roleModifies, true},
		{graphKindAbility, []string{"area_effect", "weapon_damage", "modifiers", "modifier_01", "modifier", "target_type_name"}, roleModifies, true},
		{graphKindUnit, []string{"modifier_apply_ext", "modifiers", "modifier_01", "target_type_name"}, roleModifies, true},
		{graphKindSquad, []string{"squad_modifier_apply_ext", "modifiers", "modifier_01", "modifier", "target_type_name"}, roleModifies, true},
		// The things an ability and a weapon put into play.
		{graphKindAbility, []string{"child_ability_name"}, roleAbility, true},
		{graphKindAbility, []string{"spawned_entity_name"}, roleSpawns, true},
		{graphKindWeapon, []string{"projectile_name"}, roleProjectile, true},
		{graphKindWeapon, []string{"fired_projectile"}, roleProjectile, true},
		{graphKindWeapon, []string{"impact_ability"}, roleAbility, true},
		{graphKindWeapon, []string{"group_weapon_table", "weapon_01", "referenced_weapon_name"}, roleWeapon, true},
		// Not references, and must not be mistaken for them. Every one of
		// these names something that looks like a file and is not one: an
		// animation, a sound cue, an icon, a hotkey string, a number.
		{graphKindUnit, []string{"health_ext", "hitpoints"}, 0, false},
		{graphKindUnit, []string{"cost_ext", "time_cost", "cost", "requisition"}, 0, false},
		{graphKindUnit, []string{"ui_ext", "ui_info", "icon_name"}, 0, false},
		{graphKindUnit, []string{"requirement_ext", "requirements", "required_1", "name"}, 0, false},
		{graphKindAbility, []string{"ui_hotkey_name"}, 0, false},
		{graphKindAbility, []string{"ability_motion_name"}, 0, false},
		{graphKindAbility, []string{"speech_directory"}, 0, false},
		{graphKindAbility, []string{"ability_ordered_event_name"}, 0, false},
		{graphKindAbility, []string{"ability_cursor_event"}, 0, false},
		{graphKindAddon, []string{"add_on_motion_name"}, 0, false},
		{graphKindWeapon, []string{"death_motion_value"}, 0, false},
		{graphKindWeapon, []string{"ui_info", "icon_name"}, 0, false},
		// target_type_name only means anything inside a modifier table.
		{graphKindUnit, []string{"ui_ext", "target_type_name"}, 0, false},
	}
	for _, c := range cases {
		role, _, ok := graphRoleForField(c.kind, c.path)
		if ok != c.ok || (ok && role != c.role) {
			t.Errorf("graphRoleForField(%v, %v) = %v/%v, want %v/%v", c.kind, c.path, role, ok, c.role, c.ok)
		}
	}
}

func TestOrphansAreUnitsNothingBuildsOrUnlocks(t *testing.T) {
	// The installed game is not the interesting case: most of its units are
	// built through a mechanism that is not a requirement link, so nearly all
	// of them come out as orphans. What matters is the mod's own units — a
	// test unit the author has just written is an orphan until something is
	// wired to it — so the behaviour is checked on a small tree of our own.
	tree := newGraphTestMod(t)
	g := newModGraph(tree)

	wired := `Data\attrib\ebps\races\necrons\troops\necron_gauntlet.rgd`
	orphan := `Data\attrib\ebps\races\necrons\troops\necron_loose.rgd`
	armoury := `Data\attrib\ebps\races\necrons\structures\necron_armoury.rgd`

	got := g.Orphans(graphKindUnit)
	var isWired, isOrphan bool
	for _, p := range got {
		if strings.EqualFold(p, wired) {
			isWired = true
		}
		if strings.EqualFold(p, orphan) {
			isOrphan = true
		}
	}
	if isWired {
		t.Error("a unit a building builds is not an orphan, so it must not be listed")
	}
	if !isOrphan {
		t.Error("a unit nothing builds and nothing unlocks is an orphan, so it must be listed")
	}
	// And the building that builds it knows so.
	if produced := g.Produces(armoury); len(produced) != 1 || !strings.EqualFold(produced[0], wired) {
		t.Errorf("the armoury builds %v, want just the gauntlet", produced)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
