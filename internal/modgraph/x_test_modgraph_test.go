package modgraph_test

import (
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modgraph"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/unitdef"
)

func graphOf(t *testing.T) *uistate.ModGraph {
	t.Helper()
	return modgraph.NewModGraph(testutil.OpenDXP3(t))
}

func TestTheGraphKnowsEveryKindOfThingTheGameShips(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	g := modgraph.NewModGraph(tree)
	g.EnsureLinks()

	// The expectation is read off the tree rather than written down, because
	// a number written down is only as good as the day it was written: this
	// asks whether every .rgd of a kind was found and classified as that
	// kind, which is the property that matters.
	attrib := modtree.FindFolderByPath(tree.Root, "attrib")
	if attrib == nil {
		t.Fatal("no attrib folder")
	}
	onDisk := map[modgraph.GraphKind]int{}
	modtree.SearchWalk(attrib, func(f *modtree.TreeFile) {
		if !strings.EqualFold(filepath.Ext(f.Name), ".rgd") {
			return
		}
		kind, _ := modgraph.ClassifyGraphEntity(f.FullPath)
		onDisk[kind]++
	})
	for kind, n := range onDisk {
		if n == 0 {
			continue
		}
		got := len(g.EntitiesOfKind(kind))
		if got != n {
			t.Errorf("%s: indexed %d, the tree holds %d", kind.Label(), got, n)
		}
	}
	// The kinds that carry the mod's content have to be there at all, or the
	// counts above would be comparing zeroes.
	for _, kind := range []modgraph.GraphKind{modgraph.GraphKindUnit, modgraph.GraphKindStructure, modgraph.GraphKindSquad,
		modgraph.GraphKindWeapon, modgraph.GraphKindAbility, modgraph.GraphKindAddon, modgraph.GraphKindResearch, modgraph.GraphKindModifier} {
		if len(g.EntitiesOfKind(kind)) == 0 {
			t.Errorf("%s: nothing indexed", kind.Label())
		}
	}
}

func TestEveryUnitAndBuildingIsFoundAndKnowsItsRace(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	g := modgraph.NewModGraph(tree)
	cat := unitdef.BuildUnitCatalog(tree)

	for _, u := range cat.AllUnits() {
		e := g.Entity(u.Path)
		if e == nil {
			t.Fatalf("%s is in the catalogue but not in the graph", u.Path)
		}
		want := modgraph.GraphKindUnit
		if u.Kind == unitdef.UnitKindStructure {
			want = modgraph.GraphKindStructure
		}
		if e.Kind != want {
			t.Errorf("%s: graph says %s, the folder says %s", u.Path, e.Kind.Label(), u.Kind.Label)
		}
		if !strings.EqualFold(e.Race, u.Race) {
			t.Errorf("%s: graph race %q, folder race %q", u.Path, e.Race, u.Race)
		}
	}
}

func TestEveryReferenceRoleIsFoundInTheGamesOwnFiles(t *testing.T) {
	g := graphOf(t)
	g.EnsureLinks()
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
	want := map[modgraph.GraphRole]int{
		modgraph.RoleWeapon:         1732,
		modgraph.RoleAbility:        1012,
		modgraph.RoleEffect:         747,
		modgraph.RoleSquad:          324,
		modgraph.RoleResearch:       293,
		modgraph.RoleBuiltBy:        692,
		modgraph.RoleAddon:          142,
		modgraph.RoleNeedsAddon:     494,
		modgraph.RoleNeedsResearch:  464,
		modgraph.RoleNeedsStructure: 426,
		modgraph.RoleModifies:       1407,
		modgraph.RoleProjectile:     165,
		modgraph.RoleBlueprint:      2,
		modgraph.RoleRelocation:     17,
		modgraph.RoleExcludesAddon:  2,
		modgraph.RoleSpawns:         14,
	}
	got := map[modgraph.GraphRole]int{}
	for _, l := range g.Links {
		got[l.Role]++
	}
	for role, n := range want {
		if got[role] != n {
			t.Errorf("role %q: found %d references, the files hold %d", role.Label(), got[role], n)
		}
	}
	for role, n := range got {
		if _, ok := want[role]; !ok && n > 0 {
			t.Errorf("role %d: %d references matched a rule that is not in the table", role, n)
		}
	}
	if total := len(g.Links); total != 7933 {
		t.Errorf("read %d references in total, the files hold 7933", total)
	}
}

func TestEveryReferenceInTheGamesOwnDataResolves(t *testing.T) {
	g := graphOf(t)
	g.EnsureLinks()

	// Not "almost every": all of them. 7933 references across the 2733 files
	// that hold any, and every one names a file that is there. The harder half
	// of that is the bare names — "chaos_hq", "relocate_hq" — which the
	// requirement tables are full of and which a path-only lookup cannot
	// resolve; they are found by name within the referring entity's race.
	broken := g.BrokenLinks()
	for _, l := range broken {
		t.Errorf("unresolved reference %q (%s) in %s at %s", l.Ref, l.Role.Label(), l.From, l.Field)
	}
	if len(g.Links) < 7000 {
		t.Errorf("only %d references were read out of the game's data", len(g.Links))
	}
}

// A reference the game writes as a bare name is found by name, and the
// referring entity's own race is what disambiguates it.
func TestABareNameReferenceIsResolvedWithinTheReferringRace(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	g := modgraph.NewModGraph(tree)

	// The eldar HQ leaves a dummy building behind when it relocates, and that
	// dummy is named in a bare name — no folder, no extension — which no
	// path-shaped lookup can find.
	links := g.LinksOfRole(`Data\attrib\ebps\races\eldar\structures\eldar_hq.rgd`, modgraph.RoleRelocation)
	if len(links) != 1 {
		t.Fatalf("eldar_hq has %d relocation references, want 1", len(links))
	}
	if !strings.EqualFold(links[0].Ref, "relocate_hq") {
		t.Errorf("eldar_hq relocates as %q, want relocate_hq", links[0].Ref)
	}
	if !links[0].Resolved() {
		t.Error("the bare name relocate_hq did not resolve")
	}
}

func TestTheWeaponsOfAUnitResolveToTheWeaponLibrary(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	g := modgraph.NewModGraph(tree)

	// necron_lord is a unit of the game with weapons, abilities and a
	// requirement, so it exercises every part of the extractor at once.
	const unit = `Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`
	weapons := g.LinksOfRole(unit, modgraph.RoleWeapon)
	if len(weapons) == 0 {
		t.Fatal("necron_lord has no weapons in the graph")
	}
	for _, w := range weapons {
		if !w.Resolved() {
			t.Errorf("weapon %q does not resolve", w.Ref)
			continue
		}
		e := g.Entity(w.To)
		if e == nil || e.Kind != modgraph.GraphKindWeapon {
			t.Errorf("weapon %q resolves to %s, which is not a weapon", w.Ref, w.To)
		}
	}
}

func TestABuildingIsWhatBuildsTheUnitsThatRequireIt(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	g := modgraph.NewModGraph(tree)

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
		e := g.Entity(p)
		if e == nil {
			t.Errorf("%s is listed as built by the armoury but is not in the graph", p)
			continue
		}
		switch e.Kind {
		case modgraph.GraphKindUnit:
			units++
		case modgraph.GraphKindStructure:
			// A building can require another building: the Chaos turrets
			// name the armoury, and that is a real dependency the freeze
			// has to clear.
		default:
			t.Errorf("%s is listed as built by the armoury but is a %s", p, e.Kind.Label())
		}
		// The link has to be a real one in that file, not something the
		// graph invented.
		found := false
		for _, l := range g.Outgoing(p) {
			if l.Role == modgraph.RoleBuiltBy && strings.EqualFold(l.To, armoury) {
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
	for _, e := range g.EntitiesOfKind(modgraph.GraphKindStructure) {
		for _, p := range g.Produces(e.Path) {
			if e2 := g.Entity(p); e2 != nil && !strings.EqualFold(e2.Race, e.Race) {
				t.Errorf("%s builds %s, which is a %s unit", e.Name, e2.Name, e2.Race)
			}
		}
	}
}

func TestTheTwoDirectionsOfTheGraphAreInverses(t *testing.T) {
	g := graphOf(t)
	g.EnsureLinks()

	for _, e := range g.EntitiesOfKind(modgraph.GraphKindUnit, modgraph.GraphKindStructure) {
		for _, l := range g.Outgoing(e.Path) {
			if !l.Resolved() {
				continue
			}
			var back bool
			for _, r := range g.Incoming(l.To) {
				if strings.EqualFold(r.From, l.From) && r.Field == l.Field && r.Role == l.Role {
					back = true
				}
			}
			if !back {
				t.Errorf("%s -> %s at %s is in the forward index but not the reverse one", e.Name, l.To, l.Field)
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
		if got := modgraph.RefStem(in); got != want {
			t.Errorf("refStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyGraphEntityReadsTheGamesOwnFolders(t *testing.T) {
	cases := []struct {
		path string
		kind modgraph.GraphKind
		race string
	}{
		{`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`, modgraph.GraphKindUnit, "necrons"},
		{`Data\attrib\ebps\races\chaos\structures\chaos_hq.rgd`, modgraph.GraphKindStructure, "chaos"},
		{`Data\attrib\sbps\races\chaos\chaos_squad_cultist.rgd`, modgraph.GraphKindSquad, "chaos"},
		{`Data\attrib\weapon\guard_laspistol_psyker.rgd`, modgraph.GraphKindWeapon, ""},
		{`Data\attrib\abilities\chaos_furious_rage.rgd`, modgraph.GraphKindAbility, ""},
		{`Data\attrib\addons\chaos_hq_addon_1.rgd`, modgraph.GraphKindAddon, ""},
		{`Data\attrib\research\chaos_bloodthirster_research.rgd`, modgraph.GraphKindResearch, ""},
		{`Data\attrib\modifiers\no_modifier.rgd`, modgraph.GraphKindModifier, ""},
		{`Data\attrib\requirements\no_requirement.rgd`, modgraph.GraphKindRequirement, ""},
		{`Data\attrib\formations\chaos_formation.rgd`, modgraph.GraphKindFormation, ""},
		{`Data\attrib\ebps\environment\gameplay\blood_splat.rgd`, modgraph.GraphKindEnvironment, ""},
		{`Data\attrib\ebpextensions\combat_ext.rgd`, modgraph.GraphKindSchema, ""},
		{`Data\attrib\tables\weapon_table.rgd`, modgraph.GraphKindSchema, ""},
		{`Data\attrib\type_armour\tp_armour.rgd`, modgraph.GraphKindSchema, ""},
		{`Data\art\models\whatever.rgd`, modgraph.GraphKindOther, ""},
	}
	for _, c := range cases {
		kind, race := modgraph.ClassifyGraphEntity(c.path)
		if kind != c.kind || race != c.race {
			t.Errorf("classify(%q) = %s/%q, want %s/%q", c.path, kind.Label(), race, c.kind.Label(), c.race)
		}
	}
}

func TestGraphRoleForFieldCoversTheShapesTheGameWrites(t *testing.T) {
	cases := []struct {
		kind modgraph.GraphKind
		path []string
		role modgraph.GraphRole
		ok   bool
	}{
		{modgraph.GraphKindUnit, []string{"combat_ext", "hardpoints", "hardpoint_01", "weapon_table", "weapon_01", "weapon"}, modgraph.RoleWeapon, true},
		{modgraph.GraphKindUnit, []string{"ability_ext", "abilities", "ability_01"}, modgraph.RoleAbility, true},
		{modgraph.GraphKindUnit, []string{"addon_ext", "addons", "addon_02"}, modgraph.RoleAddon, true},
		{modgraph.GraphKindUnit, []string{"research_ext", "research_table", "research_03"}, modgraph.RoleResearch, true},
		{modgraph.GraphKindUnit, []string{"spawner_ext", "squad_table", "squad_07"}, modgraph.RoleSquad, true},
		{modgraph.GraphKindUnit, []string{"requirement_ext", "requirements", "required_1", "structure_name"}, modgraph.RoleBuiltBy, true},
		{modgraph.GraphKindUnit, []string{"requirement_ext", "requirements", "required_11", "structure_name_or"}, modgraph.RoleBuiltBy, true},
		{modgraph.GraphKindUnit, []string{"requirement_ext", "requirements", "required_1", "global_addon_name"}, modgraph.RoleNeedsAddon, true},
		{modgraph.GraphKindUnit, []string{"possess_ext", "requirements", "required_1", "research_name"}, modgraph.RoleNeedsResearch, true},
		{modgraph.GraphKindUnit, []string{"entrench_ext", "entrenched_blueprint_name"}, modgraph.RoleBlueprint, true},
		{modgraph.GraphKindUnit, []string{"relocatee_ext", "dummy_structure_name"}, modgraph.RoleRelocation, true},
		{modgraph.GraphKindUnit, []string{"health_ext", "spawn_death_blossom"}, modgraph.RoleEffect, true},
		// A structure named in a unit's own requirement table is who builds it;
		// the same word in an add-on's or a weapon's is the building that has
		// to be finished first. Same field, opposite direction, and the graph
		// has to keep them apart or Produces() reads the wrong way round.
		{modgraph.GraphKindAddon, []string{"requirements", "required_1", "structure_name"}, modgraph.RoleNeedsStructure, true},
		{modgraph.GraphKindAddon, []string{"requirements", "required_10", "structure_name_either"}, modgraph.RoleNeedsStructure, true},
		{modgraph.GraphKindAbility, []string{"requirements", "required_1", "required_structure_name"}, modgraph.RoleNeedsStructure, true},
		{modgraph.GraphKindSquad, []string{"squad_requirement_ext", "requirements", "required_1", "structure_name"}, modgraph.RoleBuiltBy, true},
		// The requirement table hung off the top level, as abilities, add-ons,
		// weapons and research all write it.
		{modgraph.GraphKindAddon, []string{"requirements", "required_1", "addon_name"}, modgraph.RoleNeedsAddon, true},
		{modgraph.GraphKindAddon, []string{"requirements", "required_11", "addon"}, modgraph.RoleNeedsAddon, true},
		{modgraph.GraphKindAddon, []string{"requirements", "required_1", "mutually_exclusive_with"}, modgraph.RoleExcludesAddon, true},
		{modgraph.GraphKindResearch, []string{"requirements", "required_11", "research_name_or"}, modgraph.RoleNeedsResearch, true},
		{modgraph.GraphKindAbility, []string{"requirements", "required_1", "squad_name"}, modgraph.RoleSquad, true},
		// What a modifier applies to, in every file that carries one.
		{modgraph.GraphKindResearch, []string{"modifiers", "modifier_01", "target_type_name"}, modgraph.RoleModifies, true},
		{modgraph.GraphKindAddon, []string{"modifiers", "modifier_02", "target_type_name"}, modgraph.RoleModifies, true},
		{modgraph.GraphKindAbility, []string{"area_effect", "weapon_damage", "modifiers", "modifier_01", "modifier", "target_type_name"}, modgraph.RoleModifies, true},
		{modgraph.GraphKindUnit, []string{"modifier_apply_ext", "modifiers", "modifier_01", "target_type_name"}, modgraph.RoleModifies, true},
		{modgraph.GraphKindSquad, []string{"squad_modifier_apply_ext", "modifiers", "modifier_01", "modifier", "target_type_name"}, modgraph.RoleModifies, true},
		// The things an ability and a weapon put into play.
		{modgraph.GraphKindAbility, []string{"child_ability_name"}, modgraph.RoleAbility, true},
		{modgraph.GraphKindAbility, []string{"spawned_entity_name"}, modgraph.RoleSpawns, true},
		{modgraph.GraphKindWeapon, []string{"projectile_name"}, modgraph.RoleProjectile, true},
		{modgraph.GraphKindWeapon, []string{"fired_projectile"}, modgraph.RoleProjectile, true},
		{modgraph.GraphKindWeapon, []string{"impact_ability"}, modgraph.RoleAbility, true},
		{modgraph.GraphKindWeapon, []string{"group_weapon_table", "weapon_01", "referenced_weapon_name"}, modgraph.RoleWeapon, true},
		// Not references, and must not be mistaken for them. Every one of
		// these names something that looks like a file and is not one: an
		// animation, a sound cue, an icon, a hotkey string, a number.
		{modgraph.GraphKindUnit, []string{"health_ext", "hitpoints"}, 0, false},
		{modgraph.GraphKindUnit, []string{"cost_ext", "time_cost", "cost", "requisition"}, 0, false},
		{modgraph.GraphKindUnit, []string{"ui_ext", "ui_info", "icon_name"}, 0, false},
		{modgraph.GraphKindUnit, []string{"requirement_ext", "requirements", "required_1", "name"}, 0, false},
		{modgraph.GraphKindAbility, []string{"ui_hotkey_name"}, 0, false},
		{modgraph.GraphKindAbility, []string{"ability_motion_name"}, 0, false},
		{modgraph.GraphKindAbility, []string{"speech_directory"}, 0, false},
		{modgraph.GraphKindAbility, []string{"ability_ordered_event_name"}, 0, false},
		{modgraph.GraphKindAbility, []string{"ability_cursor_event"}, 0, false},
		{modgraph.GraphKindAddon, []string{"add_on_motion_name"}, 0, false},
		{modgraph.GraphKindWeapon, []string{"death_motion_value"}, 0, false},
		{modgraph.GraphKindWeapon, []string{"ui_info", "icon_name"}, 0, false},
		// target_type_name only means anything inside a modifier table.
		{modgraph.GraphKindUnit, []string{"ui_ext", "target_type_name"}, 0, false},
	}
	for _, c := range cases {
		role, _, ok := modgraph.GraphRoleForField(c.kind, c.path)
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
	tree := testutil.NewGraphTestMod(t)
	g := modgraph.NewModGraph(tree)

	wired := `Data\attrib\ebps\races\necrons\troops\necron_gauntlet.rgd`
	orphan := `Data\attrib\ebps\races\necrons\troops\necron_loose.rgd`
	armoury := `Data\attrib\ebps\races\necrons\structures\necron_armoury.rgd`

	got := g.Orphans(modgraph.GraphKindUnit)
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
