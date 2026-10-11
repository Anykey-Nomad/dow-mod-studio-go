package main

import (
	"dow-mod-studio/internal/rgd"
	"fmt"
	"strings"
)

// A new unit, written from a template.
//
// An RGD is a table of hashed names, and the game's own unit files are the only
// description of what one has to contain: 432 units of the installed game, every
// one of them carrying the same twelve *_ext sections, each of which points at a
// shared lua that fills in the rest. A new unit therefore does not need a schema
// written out by hand — it needs those twelve sections, each with its $REF, and
// the attributes every unit has inside them.
//
// What is here is that template. The defaults are the values all 432 units agree
// on where there is one, and a plain, obvious value where there is not; the
// attributes a new unit has to be given — the model, the icon, the name id — are
// filled from the name it was created under, or left empty where the game itself
// cannot be guessed (a model is not named after its file).
//
// What is deliberately not here: weapons and abilities. They live in
// combat_ext, ability_ext and friends, which only some units have, and a
// generated unit with an empty weapon slot is a unit that loads; a generated
// unit with somebody's gun is not.

// unitTemplateName is the file name stem a new unit is written under.
type unitTemplateName struct {
	// Race is the folder under "races", e.g. "necrons".
	Race string
	// Name is the file name stem, e.g. "necron_wraith".
	Name string
}

// unitTemplateVirtualPath is where a new unit of that race lives.
func (n unitTemplateName) unitTemplateVirtualPath() string {
	return fmt.Sprintf(`Data\attrib\ebps\races\%s\troops\%s.rgd`, n.Race, n.Name)
}

// The race folders of the game do not all spell their display name the way the
// folder does, and the models, icons and sound directories are filed under the
// display one. These are the spellings the installed game uses, read off its own
// unit files; a race that is not listed here gets the folder name, which is
// right often enough to start from and wrong in a way the RGD editor shows.
var unitTemplateRaceDisplay = map[string]string{
	"chaos":         "Chaos",
	"dark_eldar":    "Dark_Eldar",
	"eldar":         "Eldar",
	"guard":         "Imperial_Guard",
	"necrons":       "Necrons",
	"npc":           "Single_Player",
	"orks":          "Orks",
	"sisters":       "Sisters",
	"space_marines": "Space_Marines",
	"tau":           "Tau",
}

// unitTemplateRaceDisplayName is the folder name as the game spells it in model
// and sound paths.
func unitTemplateRaceDisplayName(race string) string {
	if display, ok := unitTemplateRaceDisplay[race]; ok {
		return display
	}
	return unitTemplateTitleCase(race)
}

// unitTemplateIconBase is the icon folder of a race: the race folder without its
// plural, as the game files it — necrons/necron_icons, orks/ork_icons,
// space_marines/space_marine_icons.
func unitTemplateIconBase(race string) string {
	base := strings.TrimSuffix(race, "s")
	if base == "" {
		base = race
	}
	return base + "_icons"
}

// unitTemplateTitleCase spells a folder or file name the way the game spells a
// model: each underscore separated word capitalised, e.g. "necron_basic_warrior"
// becomes "Necron_Basic_Warrior".
func unitTemplateTitleCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "_")
}

// ── entry constructors ──────────────────────────────────────────────────────

// The references the sections and their sub-tables are built on. They are the
// game's own files, and a new unit that points at one inherits everything that
// file defines, which is why a generated unit is small.
const (
	refCostExt         = `ebpextensions\cost_ext.lua`
	refCoverExt        = `ebpextensions\cover_ext.lua`
	refBlueprintExt    = `ebpextensions\entity_blueprint_ext.lua`
	refEventManager    = `ebpextensions\event_manager_ext.lua`
	refHealthExt       = `ebpextensions\health_ext.lua`
	refModifierExt     = `ebpextensions\modifier_ext.lua`
	refMovingExt       = `ebpextensions\moving_ext.lua`
	refSightExt        = `ebpextensions\sight_ext.lua`
	refSimEntityExt    = `ebpextensions\sim_entity_ext.lua`
	refSpecialAttack   = `ebpextensions\special_attack_physics_ext.lua`
	refTypeExt         = `ebpextensions\type_ext.lua`
	refUIExt           = `ebpextensions\ui_ext.lua`
	refTimeCostTable   = `tables\time_cost_table.lua`
	refCostTable       = `tables\cost_table.lua`
	refCoverTable      = `tables\cover_table.lua`
	refModifierTable   = `tables\modifier_table.lua`
	refNoModifier      = `modifiers\no_modifier.lua`
	refEventTable      = `tables\modifier_event_table.lua`
	refEventEntry      = `tables\modifier_event_entry.lua`
	refResourceTable   = `tables\resource_table.lua`
	refUIInfoTable     = `tables\ui_info_table.lua`
	refTextTable       = `tables\text_table.lua`
	refIndicatorTable  = `tables\terrain_footfall_based_event_table.lua`
	refTypeEnvironment = `type_environment\tp_environment.lua`
	refTypeSurface     = `type_surface\tp_light_metal_armour.lua`
	refTypeSpeech      = `type_speech\speech_normal.lua`
	refTypeArmour      = `type_armour\tp_armour.lua`
	refTypeArmourMain  = `type_armour\tp_infantry_heavy_med.lua`
) // The numbered slots the game gives a unit. They are empty in a new unit and
// filled by the abilities and events the unit is given later, but the game reads
// the whole set, so they are written out.
const (
	unitTemplateEventSlots   = 36
	unitTemplateEventEntries = 4
	unitTemplateTextSlots    = 8
	// rgdTemplateMovementDisabledHash names an attribute of moving_ext that the
	// game stores under a hash its dictionary does not know. Every unit has it.
	rgdTemplateMovementDisabledHash = 0x6F9C32B9
)

// rgdTemplateEntry is one entry of a generated unit.
func rgdTemplateEntry(name string, typ uint32) *rgd.RgdEntry {
	return &rgd.RgdEntry{Hash: rgd.Hash(name), Name: name, Type: typ}
}

// rgdTemplateRef is the $REF entry a table points at.
func rgdTemplateRef(ref string) *rgd.RgdEntry {
	return &rgd.RgdEntry{Hash: rgd.RefHash, Name: "$REF", Type: rgd.RgdTypeString, StrVal: ref}
}

// rgdTemplateTable is a table with the reference it extends and the children the
// game expects inside it.
func rgdTemplateTable(name, ref string, children ...*rgd.RgdEntry) *rgd.RgdEntry {
	e := rgdTemplateEntry(name, rgd.RgdTypeTable)
	e.Children = children
	if ref != "" {
		e.Ref = rgdTemplateRef(ref)
	}
	return e
}

func rgdTemplateFloat(name string, v float32) *rgd.RgdEntry {
	e := rgdTemplateEntry(name, rgd.RgdTypeFloat)
	e.FloatVal = v
	return e
}

func rgdTemplateBool(name string, v bool) *rgd.RgdEntry {
	e := rgdTemplateEntry(name, rgd.RgdTypeBool)
	e.BoolVal = v
	return e
}

func rgdTemplateString(name, v string) *rgd.RgdEntry {
	e := rgdTemplateEntry(name, rgd.RgdTypeString)
	e.StrVal = v
	return e
}

// rgdTemplateHashedString is an attribute the game's dictionary cannot name: the
// file stores a hash that no word in RGD_DIC.TXT matches, and every unit of the
// game carries it. The game reads it back by hash, so the template writes the
// hash itself rather than a name that would hash to something else.
func rgdTemplateHashedString(hash uint32, v string) *rgd.RgdEntry {
	e := rgdTemplateEntry(fmt.Sprintf("0x%X", hash), rgd.RgdTypeString)
	e.Hash = hash
	e.StrVal = v
	return e
}

func rgdTemplateWString(name, v string) *rgd.RgdEntry {
	e := rgdTemplateEntry(name, rgd.RgdTypeWString)
	e.WStrVal = v
	return e
}

// rgdTemplateSlots writes a numbered set of children — event_01 … event_36 and
// the like — the way the game numbers them: two digits, from one.
func rgdTemplateSlots(prefix string, n int, build func(i int) *rgd.RgdEntry) []*rgd.RgdEntry {
	out := make([]*rgd.RgdEntry, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, build(i))
	}
	return out
}

func rgdTemplateSlotName(prefix string, i int) string {
	return fmt.Sprintf("%s_%02d", prefix, i)
}

// rgdTemplateTextSlots is a text table of empty slots, which is what the help
// text of a unit and its override both are. The slots hold localised text, so
// they are wide strings — the game reads them as one.
func rgdTemplateTextSlots() []*rgd.RgdEntry {
	return rgdTemplateTextSlotsOf("")
}

// rgdTemplateTextSlotsOf is a text table whose slots all hold the same thing. A
// building writes $0 into its help text, which is how the game spells an unset
// localised string; a unit writes an empty one, which is what its own files
// carry. Both are honest, and neither is the other's, so the value is a
// parameter rather than a guess.
func rgdTemplateTextSlotsOf(value string) []*rgd.RgdEntry {
	return rgdTemplateSlots("text", unitTemplateTextSlots, func(i int) *rgd.RgdEntry {
		return rgdTemplateWString(rgdTemplateSlotName("text", i), value)
	})
}

// newUnitRgd builds the file of a new unit: the twelve sections every unit of
// the game has, with the values the unit's own name gives and plain defaults for
// the rest.
func newUnitRgd(n unitTemplateName) *rgd.RgdFile {
	model := unitTemplateTitleCase(n.Name)
	raceDisplay := unitTemplateRaceDisplayName(n.Race)
	// Thirty six event slots, each with four entries, all empty. The game reads
	// the whole set, so a new unit is written with all of it.
	eventSlots := rgdTemplateSlots("event", unitTemplateEventSlots, func(i int) *rgd.RgdEntry {
		entries := make([]*rgd.RgdEntry, 0, unitTemplateEventEntries+2)
		for e := 1; e <= unitTemplateEventEntries; e++ {
			entries = append(entries, rgdTemplateTable(
				rgdTemplateSlotName("event_entry", e), refEventEntry,
				rgdTemplateFloat("active_range_max", 0),
				rgdTemplateFloat("active_range_min", 0),
				rgdTemplateString("event_end_name", ""),
				rgdTemplateString("event_name", ""),
			))
		}
		entries = append(entries,
			rgdTemplateBool("has_active_range", false),
			rgdTemplateString("modifier_name", ""),
		)
		return rgdTemplateTable(rgdTemplateSlotName("event", i), refEventTable, entries...)
	})
	root := &rgd.RgdEntry{
		Hash: rgd.Hash("GameData"),
		Name: "GameData",
		Type: rgd.RgdTypeTable,
		Children: []*rgd.RgdEntry{
			// What it costs and how long it takes: the two numbers a new unit
			// is always asked for first.
			rgdTemplateTable("cost_ext", refCostExt,
				rgdTemplateTable("time_cost", refTimeCostTable,
					rgdTemplateTable("cost", refCostTable,
						rgdTemplateFloat("faith", 0),
						rgdTemplateFloat("population", 0),
						rgdTemplateFloat("power", 0),
						rgdTemplateFloat("requisition", 250),
						rgdTemplateFloat("souls", 0),
					),
					rgdTemplateFloat("time_seconds", 60),
				),
			),
			// The five cover types, each a cover table with an empty modifier
			// list. The priority of 0 is the value all 432 units agree on.
			rgdTemplateTable("cover_ext", refCoverExt,
				rgdTemplateCover("cover_blocking"),
				rgdTemplateCover("cover_heavy"),
				rgdTemplateCover("cover_light"),
				rgdTemplateCover("cover_negative"),
				rgdTemplateCover("cover_stealth"),
			),
			// The model, and how big it is. The path is a starting point: a
			// model is not always named after the unit that uses it.
			rgdTemplateTable("entity_blueprint_ext", refBlueprintExt,
				rgdTemplateString("animator", fmt.Sprintf("Races/%s/Troops/%s", raceDisplay, model)),
				rgdTemplateFloat("minimum_update_radius", 5),
				rgdTemplateFloat("scale_x", 1),
				rgdTemplateFloat("scale_y", 1),
				rgdTemplateFloat("scale_z", 1),
			),
			// Thirty six event slots, each with four entries, all empty.
			rgdTemplateTable("event_manager_ext", refEventManager, eventSlots...),
			rgdTemplateTable("health_ext", refHealthExt,
				rgdTemplateFloat("armour", 100),
				rgdTemplateFloat("armour_minimum", 0),
				rgdTemplateBool("can_be_repaired", true),
				rgdTemplateFloat("damage_dealt_regeneration_factor", 0),
				rgdTemplateString("death_event", ""),
				rgdTemplateTable("death_resource", refResourceTable,
					rgdTemplateFloat("faith", 0),
					rgdTemplateFloat("pop", 0),
					rgdTemplateFloat("power", 0),
					rgdTemplateFloat("requisition", 0),
					rgdTemplateFloat("souls", 0),
				),
				rgdTemplateBool("death_resource_permanent", false),
				rgdTemplateBool("display_health_bar", true),
				rgdTemplateFloat("get_back_up_chance", 0),
				rgdTemplateFloat("get_back_up_get_up_time", 0),
				rgdTemplateFloat("get_back_up_health_percent", 0),
				rgdTemplateFloat("get_back_up_squad_proximity", 0),
				rgdTemplateFloat("hitpoints", 1000),
				rgdTemplateBool("invulnerable", false),
				rgdTemplateBool("keep_persistent_body", true),
				rgdTemplateFloat("max_repairers", 1),
				rgdTemplateFloat("morale_death", 0),
				rgdTemplateFloat("poison_damage", 0),
				rgdTemplateFloat("poison_damage_duration", 0),
				rgdTemplateString("poison_event", ""),
				rgdTemplateFloat("poison_immunity_duration", 0),
				rgdTemplateFloat("poison_spread_delay", 2),
				rgdTemplateFloat("post_death_event_delay", 0),
				rgdTemplateFloat("pre_death_event_delay", 0),
				rgdTemplateFloat("regeneration_decrease_in_combat", 0),
				rgdTemplateFloat("regeneration_rate", 0),
				rgdTemplateFloat("return_from_dead_duration", 0),
				rgdTemplateString("spawn_blood_splat_on_death", ""),
				rgdTemplateString("spawn_death_blossom", ""),
				rgdTemplateString("spawn_on_death", ""),
				rgdTemplateString("spawn_usable_body_on_death", ""),
				rgdTemplateFloat("stay_in_pathfinding_after_dead_time", 0),
				rgdTemplateIndicatorEvent("usable_body_indicator_event"),
				rgdTemplateIndicatorEvent("usable_body_indicator_pre_event"),
			),
			// The game leaves this one empty in every unit it ships.
			rgdTemplateTable("modifier_ext", refModifierExt),
			rgdTemplateTable("moving_ext", refMovingExt,
				rgdTemplateHashedString(rgdTemplateMovementDisabledHash, "movement_disabled"),
				rgdTemplateFloat("acceleration_time", 2),
				rgdTemplateBool("air_unit", false),
				rgdTemplateFloat("air_unit_float_height", 9),
				rgdTemplateFloat("corner_anticipation_time", 0.1),
				rgdTemplateFloat("deceleration_time", 1),
				rgdTemplateBool("move_to_min_combat_range", false),
				rgdTemplateFloat("rotation_rate", 90),
				rgdTemplateFloat("speed_max", 4),
				rgdTemplateString("turning_behavior_template", "default"),
			),
			rgdTemplateTable("sight_ext", refSightExt,
				rgdTemplateFloat("keen_sight_radius", 0),
				rgdTemplateFloat("sight_radius", 25),
			),
			rgdTemplateTable("sim_entity_ext", refSimEntityExt,
				rgdTemplateBool("is_collide", true),
				rgdTemplateBool("is_in_spatial_bucket", true),
				rgdTemplateBool("no_rotate", false),
			),
			rgdTemplateTable("special_attack_physics_ext", refSpecialAttack,
				rgdTemplateBool("disable_special_attack", true),
				rgdTemplateBool("face_backwards_when_flying", true),
				rgdTemplateFloat("get_up_time", 0),
				rgdTemplateFloat("mass", 50),
			),
			// What the unit is: the surface it stands on, the armour type that
			// decides what can hurt it, and how it speaks.
			rgdTemplateTable("type_ext", refTypeExt,
				rgdTemplateBool("can_be_possessed_by_enemy", false),
				rgdTemplateBool("reanimate", false),
				rgdTemplateBool("single_player_only", false),
				rgdTemplateBool("summon_teleport_building_check_override", false),
				rgdTemplateBool("summon_teleport_destination", true),
				rgdTemplateTable("type_armour", refTypeArmourMain,
					rgdTemplateWString("screen_name_id", "$90105"),
				),
				rgdTemplateTable("type_armour_2", refTypeArmour,
					rgdTemplateWString("screen_name_id", "$90100"),
				),
				rgdTemplateTable("type_environment", refTypeEnvironment),
				rgdTemplateTable("type_speech", refTypeSpeech),
				rgdTemplateTable("type_surface", refTypeSurface),
			),
			// What the player sees: the name, the icon, the build button.
			rgdTemplateTable("ui_ext", refUIExt,
				rgdTemplateBool("ghost_auto_enable", false),
				rgdTemplateBool("ghost_enable", false),
				rgdTemplateBool("ghost_hidden_until_seen", false),
				rgdTemplateFloat("minimap_color_b", 255),
				rgdTemplateFloat("minimap_color_g", 255),
				rgdTemplateFloat("minimap_color_r", 255),
				rgdTemplateBool("minimap_enable", true),
				rgdTemplateBool("minimap_teamcolor", true),
				rgdTemplateString("selection_event", ""),
				rgdTemplateBool("selection_use_events", false),
				rgdTemplateString("speech_directory", ""),
				rgdTemplateBool("ui_hide_button_when_unable_to_build", false),
				rgdTemplateString("ui_hotkey_name", n.Name),
				rgdTemplateFloat("ui_index_hint", 1),
				rgdTemplateTable("ui_info", refUIInfoTable,
					rgdTemplateWString("help_text_id", "$0"),
					rgdTemplateTable("help_text_list", refTextTable, rgdTemplateTextSlots()...),
					rgdTemplateString("icon_name", fmt.Sprintf("%s/%s_icon", unitTemplateIconBase(n.Race), n.Name)),
					rgdTemplateBool("no_button", false),
					rgdTemplateTable("override_help_text_list", refTextTable, rgdTemplateTextSlots()...),
					rgdTemplateBool("pseudo_leader", false),
					// The name the game shows. A real unit points at an entry of
					// the string table, which a mod has to add; $0 is what an
					// unset one reads as.
					rgdTemplateWString("screen_name_id", "$0"),
					rgdTemplateBool("use_override_table_for_non_requisition_races", false),
				),
				rgdTemplateBool("use_hero_ui", false),
			),
		},
	}
	// The header of a unit file, which is the same in every one of them.
	return &rgd.RgdFile{HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1, Root: root}
}

// rgdTemplateCover is one of the five cover types: a cover table with a priority
// and a modifier list.
//
// The list is empty, and that is the whole of the default: what a cover type
// carries in it is a different set of modifier slots in every unit of the game,
// so there is no honest one to write down. A unit that needs a cover modifier
// gets it in the RGD editor.
func rgdTemplateCover(name string) *rgd.RgdEntry {
	return rgdTemplateTable(name, refCoverTable,
		rgdTemplateTable("modifiers", refModifierTable),
		rgdTemplateFloat("priority", 0),
	)
}

// rgdTemplateIndicatorEvent is the table that says which sound a corpse makes on
// each kind of ground.
func rgdTemplateIndicatorEvent(name string) *rgd.RgdEntry {
	return rgdTemplateTable(name, refIndicatorTable,
		rgdTemplateString("dirtsand", ""),
		rgdTemplateString("grass", ""),
		rgdTemplateString("rock", ""),
		rgdTemplateString("water", ""),
	)
}

// The twelve sections a generated unit always has, in the order the game writes
// them. A test reads this, so that a section cannot be dropped from the template
// without the test saying so.
var unitTemplateSections = []string{
	"cost_ext",
	"cover_ext",
	"entity_blueprint_ext",
	"event_manager_ext",
	"health_ext",
	"modifier_ext",
	"moving_ext",
	"sight_ext",
	"sim_entity_ext",
	"special_attack_physics_ext",
	"type_ext",
	"ui_ext",
}

// The requirement section and its tables, which are the references a unit's
// build requirements are written on. Read off the game's own units: the
// section points at the extension, the list of slots at one table, and every
// slot at the same one whatever it requires.
const (
	refRequirementExt   = `ebpextensions\requirement_ext.lua`
	refRequirementsList = `tables\requirements.lua`
	refRequirementSlot  = `requirements\required_cap.lua`
)

// The combat section and the four tables a weapon slot is written on, read off
// the game's own units. The chain is fixed: a unit's combat_ext holds a table of
// hardpoints, a hardpoint holds a table of weapon slots, and a slot holds a
// position, a choice name, a position and the weapon itself.
const (
	refCombatExt        = `ebpextensions\combat_ext.lua`
	refHardpointTable   = `tables\hardpoint_table.lua`
	refHardpointDetails = `tables\hardpoint_details.lua`
	refWeaponTable      = `tables\weapon_table.lua`
	refWeaponDetails    = `tables\weapon_details.lua`
	refPosition         = `tables\position.lua`
)

// rgdTemplatePosition is a position table at the origin, which is where a
// weapon's origin and muzzle sit unless a weapon says otherwise. Every
// position in the game has exactly these three.
func rgdTemplatePosition(name string) *rgd.RgdEntry {
	return rgdTemplateTable(name, refPosition,
		rgdTemplateFloat("x", 0),
		rgdTemplateFloat("y", 0),
		rgdTemplateFloat("z", 0),
	)
}
