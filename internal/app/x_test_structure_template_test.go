package app

import (
	"dow-mod-studio/internal/rgd"
	"strings"
	"testing"
)

// A new building, from the template.
//
// The tests here are the three things that can be wrong with a template: a
// section the game needs is missing, a value that the game agrees on is not the
// one the template wrote, or the template has invented an attribute the game
// does not ask for. The last is the one worth being strict about: an RGD the
// game has never seen an example of is a file that loads, if it loads.

// TestANewBuildingHasTheSectionsTheGameAgreesOn is the shape of the file. Every
// one of the 150 structures of the installed game has these thirteen sections
// except for the npc_* markers and test_building, which are not things a player
// builds, so a new building is written with all of them.
func TestANewBuildingHasTheSectionsTheGameAgreesOn(t *testing.T) {
	rf := newStructureRgd(structureTemplateName{Race: "necrons", Name: "necron_relay"})
	if len(rf.Root.Children) != len(structureTemplateSections) {
		t.Fatalf("the building has %d sections, want the %d a structure has", len(rf.Root.Children), len(structureTemplateSections))
	}
	have := map[string]bool{}
	for _, ch := range rf.Root.Children {
		have[ch.Name] = true
		if ch.Type != rgd.RgdTypeTable {
			t.Errorf("%s is a %s, but a section is a table", ch.Name, rgdTypeName(ch.Type))
			continue
		}
		if ch.Ref == nil || !strings.HasPrefix(ch.Ref.StrVal, `ebpextensions\`) {
			t.Errorf("%s does not point at an ebpextensions lua, so the game has nothing to extend", ch.Name)
		}
	}
	for _, want := range structureTemplateSections {
		if !have[want] {
			t.Errorf("the building has no %s, which every structure of the game has", want)
		}
	}
}

// TestTheSectionsTheUserNamedAreThere spells out the four the request was about,
// one by one, because they are the ones a building is recognised by: without
// structure_ext it is not a structure, without structure_buildable_ext the
// player cannot build it, and health and cost are what it is judged on.
func TestTheSectionsTheUserNamedAreThere(t *testing.T) {
	rf := newStructureRgd(structureTemplateName{Race: "necrons", Name: "necron_relay"})
	byName := map[string]*rgd.RgdEntry{}
	for _, ch := range rf.Root.Children {
		byName[ch.Name] = ch
	}
	for _, c := range []struct {
		section string
		ref     string
		want    []string
	}{
		{"structure_ext", refStructureExt, []string{
			"attach_to", "control_structure_is", "control_structure_radius",
			"control_structure_use", "control_structure_use_allied",
			"extra_no_build_buffer", "orientation",
		}},
		{"structure_buildable_ext", refStructureBuild, []string{
			"advanced_build_option", "build_menu_priority", "power_gift",
			"requisition_gift", "return_power_percent", "return_power_unbuilt_percent",
			"return_requisition_percent", "return_requisition_unbuilt_percent",
		}},
		{"health_ext", refHealthExt, []string{
			"armour", "armour_minimum", "can_be_repaired", "hitpoints",
			"invulnerable", "keep_persistent_body", "max_repairers",
		}},
		{"cost_ext", refCostExt, []string{"time_cost"}},
	} {
		sec := byName[c.section]
		if sec == nil {
			t.Errorf("there is no %s at all", c.section)
			continue
		}
		if sec.Ref == nil || sec.Ref.StrVal != c.ref {
			t.Errorf("%s points at %v, want %s", c.section, sec.Ref, c.ref)
		}
		names := map[string]bool{}
		for _, ch := range sec.Children {
			names[ch.Name] = true
		}
		for _, want := range c.want {
			if !names[want] {
				t.Errorf("%s has no %s, which all the game's structures have", c.section, want)
			}
		}
	}
	// The cost is a time cost table over a cost table, and a building that
	// cannot be paid for is not one anybody asked for.
	timeCost := rgdFindChild(byName["cost_ext"], "time_cost")
	if timeCost == nil {
		t.Fatal("cost_ext has no time_cost, so the building cannot be priced")
	}
	if timeCost.Ref == nil || timeCost.Ref.StrVal != refTimeCostTable {
		t.Errorf("time_cost points at %v, want %s", timeCost.Ref, refTimeCostTable)
	}
	if rgdFindChild(timeCost, "time_seconds") == nil {
		t.Error("time_cost has no time_seconds, so the building would build instantly")
	}
	cost := rgdFindChild(timeCost, "cost")
	if cost == nil {
		t.Fatal("time_cost has no cost table, so the building would be free")
	}
	for _, want := range []string{"faith", "population", "power", "requisition", "souls"} {
		if rgdFindChild(cost, want) == nil {
			t.Errorf("the cost table has no %s, which the game reads for every cost", want)
		}
	}
	if r := rgdFindChild(cost, "requisition"); r != nil && r.FloatVal <= 0 {
		t.Errorf("the building costs %v requisition, so it is free", r.FloatVal)
	}
}

// TestTheValuesTheStructuresAgreeOnAreTheValuesWritten checks the template
// against the installed game rather than against the author's memory of it: for
// every attribute of a building that all 150 structures carry with one value,
// the template has to have written that value.
func TestTheValuesTheStructuresAgreeOnAreTheValuesWritten(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	var structures []unitEntry
	for _, r := range cat.Races {
		structures = append(structures, r.Structures...)
	}
	stats, _ := rgdCommonStats(tree, structures)
	rf := newStructureRgd(structureTemplateName{Race: "necrons", Name: "necron_relay"})
	checked := 0
	for _, p := range rgdSortedPaths(stats) {
		a := stats[p]
		// Only the attributes every structure has, and only those where the
		// structures agree on one value: where they differ there is no single
		// right answer, and the template is allowed to pick.
		want, uniform := a.uniform()
		if !uniform || len(a.values) != 1 {
			continue
		}
		rel := strings.TrimPrefix(p, "GameData/")
		// The event manager is the one section a new object is written with
		// empty on purpose. All 150 structures carry the same nine event
		// names — a flamer hit event, a wraith tomb event — and they are the
		// game's shared definitions of those events rather than anything to
		// do with the building, so a new one starts with none of them.
		if strings.HasPrefix(rel, "event_manager_ext/") {
			continue
		}
		// Every structure of the game names a Necron sound in its usable
		// body indicator, on all four kinds of ground, whatever race it
		// belongs to. That is a path copied through the whole file set
		// rather than something the building has, so a new one is written
		// empty, the way a new unit's is.
		if strings.HasPrefix(rel, "health_ext/usable_body_indicator") {
			continue
		}
		got := rgdFindPath(rf.Root, strings.Split(rel, "/")...)
		if got == nil {
			// The template leaves an attribute out on purpose in some cases,
			// and says so where it does.
			continue
		}
		if rgdAttrValue(got) != want {
			t.Errorf("%s is %s in the template, %s in every structure of the game", rel, rgdAttrValue(got), want)
		}
		checked++
	}
	if checked < 40 {
		t.Errorf("only %d attributes were compared with the game's own structures, which is too few to be a check", checked)
	}
	t.Logf("compared %d attributes with the 150 installed structures", checked)
}

// TestANewBuildingInventsNothingTheGameDoesNotAskFor is the other direction: an
// attribute the template writes that no structure of the game has is a name the
// game has never heard of, and one that cannot be right.
func TestANewBuildingInventsNothingTheGameDoesNotAskFor(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	var structures []unitEntry
	for _, r := range cat.Races {
		structures = append(structures, r.Structures...)
	}
	stats, _ := rgdCommonStats(tree, structures)
	rf := newStructureRgd(structureTemplateName{Race: "necrons", Name: "necron_relay"})
	// The game's structures number their slots event_01 … event_36 and
	// modifier_01 … modifier_45, and the path of a generated file says so too,
	// so a numbered slot is compared under its collapsed name.
	known := map[string]bool{}
	for p := range stats {
		known[collapseRgdIndices(p)] = true
	}
	wrong := 0
	for path := range templateEntries(rf) {
		key := collapseRgdIndices(path)
		if known[key] {
			continue
		}
		wrong++
		if wrong <= 25 {
			t.Errorf("the template writes %s, which no structure of the game has", key)
		}
	}
	if wrong > 25 {
		t.Errorf("and %d more", wrong-25)
	}
}

// TestTheBuildingIsNamedAfterItself is the part a name can decide: the folder it
// goes in, the model it points at, the icon, the hotkey.
func TestTheBuildingIsNamedAfterItself(t *testing.T) {
	n := structureTemplateName{Race: "necrons", Name: "necron_relay"}
	if got, want := n.StructureTemplateVirtualPath(), `Data\attrib\ebps\races\necrons\structures\necron_relay.rgd`; got != want {
		t.Errorf("a necron building would be written to %q, want %q", got, want)
	}
	rf := newStructureRgd(n)
	blueprint := rgdFindChild(rf.Root, "entity_blueprint_ext")
	if blueprint == nil {
		t.Fatal("no entity_blueprint_ext, so the building has no model")
	}
	anim := rgdFindChild(blueprint, "animator")
	if anim == nil || anim.StrVal != "Races/Necrons/Structures/Necron_Relay" {
		t.Errorf("the model is %v, want the one named after the building", anim)
	}
	ui := rgdFindChild(rgdFindChild(rf.Root, "ui_ext"), "ui_info")
	if ui == nil {
		t.Fatal("no ui_info, so the building has no name or icon")
	}
	if icon := rgdFindChild(ui, "icon_name"); icon == nil || icon.StrVal != "necron_icons/necron_relay_icon" {
		t.Errorf("the icon is %v, want the one named after the building", icon)
	}
	if name := rgdFindChild(ui, "screen_name_id"); name == nil || name.Type != rgd.RgdTypeWString {
		t.Errorf("the screen name is %v, want a wide string, since the game reads it as localised text", name)
	}
	if hot := rgdFindChild(rgdFindChild(rf.Root, "ui_ext"), "ui_hotkey_name"); hot == nil || hot.StrVal != "necron_relay" {
		t.Errorf("the hotkey is %v, want the building's own name", hot)
	}
}

// TestTheBuildingRoundTripsThroughTheParser is the file as the app will read it
// back: written, parsed, and the sections still all there.
func TestTheBuildingRoundTripsThroughTheParser(t *testing.T) {
	data, err := newStructureRgd(structureTemplateName{Race: "necrons", Name: "necron_relay"}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("the building the app wrote does not parse: %v", err)
	}
	if len(rf.Root.Children) != len(structureTemplateSections) {
		t.Errorf("the parsed building has %d sections, want %d", len(rf.Root.Children), len(structureTemplateSections))
	}
	// The root of a parsed file is the GameData table by name; its hash is not
	// stored, in the game's files or in this one's.
	if rf.Root.Name != "GameData" {
		t.Errorf("the parsed building's root is %q, want the GameData table every object file has", rf.Root.Name)
	}
}

// TestTheModifierTablesAreTheShapeTheGameReads: forty five slots in each of the
// three tables, all but the first empty, and the two modifiers the game applies
// written the way it applies them.
func TestTheModifierTablesAreTheShapeTheGameReads(t *testing.T) {
	rf := newStructureRgd(structureTemplateName{Race: "necrons", Name: "necron_relay"})
	apply := rgdFindChild(rf.Root, "modifier_apply_ext")
	if apply == nil {
		t.Fatal("no modifier_apply_ext, which all 150 structures have")
	}
	for _, name := range []string{"modifiers", "modifiers_idle", "modifiers_immediate"} {
		table := rgdFindChild(apply, name)
		if table == nil {
			t.Errorf("modifier_apply_ext has no %s, which all 150 structures have", name)
			continue
		}
		if len(table.Children) != structureTemplateModifierSlots {
			t.Errorf("%s has %d slots, want the %d the game reads", name, len(table.Children), structureTemplateModifierSlots)
		}
		for i, slot := range table.Children {
			if want := rgdTemplateSlotName("modifier", i+1); slot.Name != want {
				t.Errorf("%s slot %d is named %q, want %q", name, i+1, slot.Name, want)
			}
		}
	}
	// The first slot of the first table is the infiltration; the game reads it
	// as a switch that is on, not as a multiplier.
	first := rgdFindChild(rgdFindChild(apply, "modifiers"), "modifier_01")
	if first == nil || first.Ref == nil || first.Ref.StrVal != refEnableInfiltration {
		t.Fatalf("the first modifier is %v, want the infiltration the game gives 146 of its 150 structures", first)
	}
	if usage := rgdFindChild(first, "usage_type"); usage == nil || usage.Ref == nil || usage.Ref.StrVal != refModUsageEnable {
		t.Errorf("the infiltration's usage is %v, want the enable one", usage)
	}
	if v := rgdFindChild(first, "value"); v == nil || v.FloatVal != -1 {
		t.Errorf("the infiltration's value is %v, want -1", v)
	}
	// Everything else is no_modifier: a multiplier of 1, which is how the game
	// spells a slot that does nothing.
	second := rgdFindChild(rgdFindChild(apply, "modifiers"), "modifier_02")
	if second == nil || second.Ref == nil || second.Ref.StrVal != refNoModifier {
		t.Fatalf("the second modifier is %v, want no_modifier", second)
	}
	if usage := rgdFindChild(second, "usage_type"); usage == nil || usage.Ref == nil || usage.Ref.StrVal != refModUsageMultiply {
		t.Errorf("no_modifier's usage is %v, want the multiplication one", usage)
	}
	if v := rgdFindChild(second, "value"); v == nil || v.FloatVal != 1 {
		t.Errorf("no_modifier's value is %v, want 1", v)
	}
}

// TestABuildingIsNotAUnit: the two templates are different files, and a building
// that had a unit's sections would not be a building. This is the test that
// would fail if the two were ever merged by accident.
func TestABuildingIsNotAUnit(t *testing.T) {
	building := map[string]bool{}
	for _, ch := range newStructureRgd(structureTemplateName{Race: "necrons", Name: "x"}).Root.Children {
		building[ch.Name] = true
	}
	for _, unitOnly := range []string{"cover_ext", "moving_ext", "special_attack_physics_ext"} {
		if building[unitOnly] {
			t.Errorf("the building has %s, which is a unit's section and not a structure's", unitOnly)
		}
	}
	for _, structureOnly := range []string{"structure_ext", "structure_buildable_ext", "infiltration_ext"} {
		if !building[structureOnly] {
			t.Errorf("the building has no %s, which a structure has and a unit does not", structureOnly)
		}
	}
	unit := map[string]bool{}
	for _, ch := range newUnitRgd(unitTemplateName{Race: "necrons", Name: "x"}).Root.Children {
		unit[ch.Name] = true
	}
	if unit["structure_ext"] {
		t.Error("the unit has structure_ext, so the two templates have been confused")
	}
}

// TestABuildingRefusesTheSameNamesAUnitDoes: the rules are about writing a file,
// not about what goes in it, so both kinds are held to them.
func TestABuildingRefusesTheSameNamesAUnitDoes(t *testing.T) {
	for _, name := range []string{"", "  ", "necrons/other", `sub\necron`, "necron_hq.rgd", "necron:hq"} {
		if err := newObjTemplateNameError(newObjBuilding, name); err == nil {
			t.Errorf("the building name %q was accepted, so a file nobody asked for could be written", name)
		}
	}
	if err := newObjTemplateNameError(newObjBuilding, "necron_hq"); err != nil {
		t.Errorf("a plain building name was refused: %v", err)
	}
	// And the message names the thing, so the dialog does not say "unit" while
	// writing a building.
	err := newObjTemplateNameError(newObjBuilding, "")
	if err == nil || !strings.Contains(err.Error(), "building") {
		t.Errorf("an empty building name says %v, want a message that says building", err)
	}
}
