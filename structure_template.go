package main

import "dow-mod-studio/internal/rgd"
import "fmt"

// A new building, written from a template.
//
// The same argument as a new unit applies, and it is the game's own files that
// make it: 150 structures in the installed game, and between them they say what
// a building has to contain. Eight sections are in every one of them —
// structure_ext, entity_blueprint_ext, event_manager_ext, modifier_apply_ext,
// modifier_ext, sim_entity_ext, type_ext and ui_ext — and five more are in all
// but a handful of props that are not really buildings: cost_ext,
// health_ext, infiltration_ext, sight_ext and structure_buildable_ext. The
// nine structures that lack cost_ext and structure_buildable_ext are the npc_*
// markers and test_building, none of which a player builds, so a new building
// is written with all thirteen.
//
// Where the structures agree, the template takes their value. Where they do not,
// it takes the value that makes a plain building: a mid armour building type, a
// heavy metal surface, the infiltration a building has, and the cost and
// hitpoints of an ordinary one.

// structureTemplateName is the file name stem a new building is written under.
type structureTemplateName struct {
	// Race is the folder under "races", e.g. "necrons".
	Race string
	// Name is the file name stem, e.g. "necron_energy_core".
	Name string
}

// structureTemplateVirtualPath is where a new building of that race lives.
func (n structureTemplateName) structureTemplateVirtualPath() string {
	return fmt.Sprintf(`Data\attrib\ebps\races\%s\structures\%s.rgd`, n.Race, n.Name)
}

// The references a building's sections are built on, over and above the ones a
// unit needs. They are the game's own files: a section that points at one
// inherits everything that file defines, which is why a generated building is
// small.
const (
	refStructureExt       = `ebpextensions\structure_ext.lua`
	refStructureBuild     = `ebpextensions\structure_buildable_ext.lua`
	refInfiltrationExt    = `ebpextensions\infiltration_ext.lua`
	refModifierApplyExt   = `ebpextensions\modifier_apply_ext.lua`
	refAttachToTable      = `tables\attachto_table.lua`
	refModApplyToEntity   = `type_modifierapplicationtype\tp_mod_apply_to_entity.lua`
	refModClassName       = `type_modifier\tp_modifier.lua`
	refModUsageEnable     = `type_modifierusagetype\tp_mod_usage_enable.lua`
	refModUsageMultiply   = `type_modifierusagetype\tp_mod_usage_multiplication.lua`
	refEnableInfiltration = `modifiers\enable_infiltration.lua`
	refBuildingArmour     = `type_armour\tp_building_med.lua`
	refHeavyMetalSurface  = `type_surface\tp_heavy_metal_armour.lua`
	refNoSpeech           = `type_speech\speech_none.lua`
)

// The numbered slots the game gives a building. They are empty in a new
// building and filled by the weapons and upgrades it is given later, but the
// game reads the whole set, so they are written out.
const (
	// structureTemplateEventSlots is the event manager's slots, the same
	// thirty six a unit has.
	structureTemplateEventSlots = 36
	// structureTemplateEventEntries is the entries inside one event slot.
	structureTemplateEventEntries = 4
	// structureTemplateModifierSlots is the length of each of the three
	// modifier tables under modifier_apply_ext.
	structureTemplateModifierSlots = 45
)

// newStructureRgd builds the file of a new building: the thirteen sections the
// game's own structures carry, with the values they agree on and plain ones
// where they do not.
func newStructureRgd(n structureTemplateName) *rgd.RgdFile {
	model := unitTemplateTitleCase(n.Name)
	raceDisplay := unitTemplateRaceDisplayName(n.Race)
	// Thirty six event slots, each with four entries, all empty.
	eventSlots := rgdTemplateSlots("event", structureTemplateEventSlots, func(i int) *rgd.RgdEntry {
		entries := make([]*rgd.RgdEntry, 0, structureTemplateEventEntries+2)
		for e := 1; e <= structureTemplateEventEntries; e++ {
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
			// What it costs and how long it takes to raise. The requisition
			// is the price of an ordinary building rather than the zero most
			// of the game's cost tables settle at, because a free building
			// is not one anybody asked for.
			rgdTemplateTable("cost_ext", refCostExt,
				rgdTemplateTable("time_cost", refTimeCostTable,
					rgdTemplateTable("cost", refCostTable,
						rgdTemplateFloat("faith", 0),
						rgdTemplateFloat("population", 0),
						rgdTemplateFloat("power", 0),
						rgdTemplateFloat("requisition", 250),
						rgdTemplateFloat("souls", 0),
					),
					rgdTemplateFloat("time_seconds", 30),
				),
			),
			// The model, and how big it is. A building is wider than it is
			// deep, which is what the 2/1/2 scale of the game's own
			// structures says.
			rgdTemplateTable("entity_blueprint_ext", refBlueprintExt,
				rgdTemplateString("animator", fmt.Sprintf("Races/%s/Structures/%s", raceDisplay, model)),
				rgdTemplateFloat("minimum_update_radius", 5),
				rgdTemplateFloat("scale_x", 2),
				rgdTemplateFloat("scale_y", 1),
				rgdTemplateFloat("scale_z", 2),
			),
			rgdTemplateTable("event_manager_ext", refEventManager, eventSlots...),
			// A building is tougher than a man and does not get back up.
			rgdTemplateTable("health_ext", refHealthExt,
				rgdTemplateFloat("armour", 100),
				rgdTemplateFloat("armour_minimum", 0),
				rgdTemplateBool("can_be_repaired", true),
				rgdTemplateFloat("damage_dealt_regeneration_factor", 0),
				rgdTemplateString("death_event", "unit_death_events/building_death"),
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
				rgdTemplateFloat("hitpoints", 2000),
				rgdTemplateBool("invulnerable", false),
				// Unlike a unit, a building leaves no body to walk away
				// from: all 146 structures that carry the attribute set it
				// false.
				rgdTemplateBool("keep_persistent_body", false),
				rgdTemplateFloat("max_repairers", 3),
				rgdTemplateFloat("morale_death", 0),
				rgdTemplateFloat("poison_damage", 0),
				rgdTemplateFloat("poison_damage_duration", 0),
				rgdTemplateString("poison_event", ""),
				rgdTemplateFloat("poison_immunity_duration", 0),
				rgdTemplateFloat("poison_spread_delay", 5),
				rgdTemplateFloat("post_death_event_delay", 0),
				rgdTemplateFloat("pre_death_event_delay", 1),
				rgdTemplateFloat("regeneration_decrease_in_combat", 0),
				rgdTemplateFloat("regeneration_rate", 0),
				rgdTemplateFloat("return_from_dead_duration", 6),
				rgdTemplateString("spawn_blood_splat_on_death", ""),
				rgdTemplateString("spawn_death_blossom", ""),
				rgdTemplateString("spawn_on_death", ""),
				rgdTemplateString("spawn_usable_body_on_death", ""),
				rgdTemplateFloat("stay_in_pathfinding_after_dead_time", 0),
				rgdTemplateIndicatorEvent("usable_body_indicator_event"),
				rgdTemplateIndicatorEvent("usable_body_indicator_pre_event"),
			),
			// Whether troops can walk into it, and how visible they are while
			// they do. The three numbers are the same in all 145 structures
			// that have this section.
			rgdTemplateTable("infiltration_ext", refInfiltrationExt,
				rgdTemplateFloat("enemy_infiltrate_opacity", 0.1),
				rgdTemplateFloat("friendly_infiltrate_opacity", 0.6),
				rgdTemplateFloat("initial_delay_time", 0),
			),
			// Three modifier tables of forty five slots each. The game reads
			// all of them, so all of them are written: the first slot of the
			// first table is the infiltration the section above describes, and
			// the rest are no_modifier, which is what they are in 120 of the
			// 150 structures.
			rgdTemplateTable("modifier_apply_ext", refModifierApplyExt,
				rgdTemplateModifierTable("modifiers", true),
				rgdTemplateModifierTable("modifiers_idle", false),
				rgdTemplateModifierTable("modifiers_immediate", false),
			),
			// The game leaves this one empty in every structure it ships.
			rgdTemplateTable("modifier_ext", refModifierExt),
			rgdTemplateTable("sim_entity_ext", refSimEntityExt,
				rgdTemplateBool("is_collide", true),
				rgdTemplateBool("is_in_spatial_bucket", true),
				rgdTemplateBool("no_rotate", false),
			),
			rgdTemplateTable("sight_ext", refSightExt,
				rgdTemplateFloat("keen_sight_radius", 0),
				rgdTemplateFloat("sight_radius", 15),
			),
			// What makes it a building rather than a prop: the section the
			// game reads to put it in the build menu, to decide what a player
			// gets back when it is destroyed, and what a refund is worth.
			rgdTemplateTable("structure_buildable_ext", refStructureBuild,
				rgdTemplateBool("advanced_build_option", false),
				rgdTemplateFloat("build_menu_priority", 1),
				rgdTemplateFloat("power_gift", 0),
				rgdTemplateFloat("requisition_gift", 0),
				rgdTemplateFloat("return_power_percent", 0.5),
				rgdTemplateFloat("return_power_unbuilt_percent", 1),
				rgdTemplateFloat("return_requisition_percent", 0.5),
				rgdTemplateFloat("return_requisition_unbuilt_percent", 1),
			),
			// What the structure itself is: how it sits on the ground, and
			// what it turns into when a player captures it. The orientation of
			// 0 and the radius of 0 are the values every structure has.
			rgdTemplateTable("structure_ext", refStructureExt,
				rgdTemplateTable("attach_to", refAttachToTable,
					rgdTemplateBool("active", false),
					rgdTemplateTable("attach_to_tp", refTypeEnvironment),
				),
				rgdTemplateBool("control_structure_is", false),
				rgdTemplateFloat("control_structure_radius", 0),
				rgdTemplateBool("control_structure_use", true),
				rgdTemplateBool("control_structure_use_allied", false),
				rgdTemplateFloat("extra_no_build_buffer", 1),
				rgdTemplateFloat("orientation", 0),
			),
			// A building is a mid armour structure on heavy metal, and it
			// does not speak: all 150 structures agree on the armour type and
			// the surface, and 145 of them on there being no voice.
			rgdTemplateTable("type_ext", refTypeExt,
				rgdTemplateBool("can_be_possessed_by_enemy", true),
				rgdTemplateBool("reanimate", false),
				rgdTemplateBool("single_player_only", false),
				rgdTemplateBool("summon_teleport_building_check_override", false),
				rgdTemplateBool("summon_teleport_destination", true),
				rgdTemplateTable("type_armour", refBuildingArmour,
					rgdTemplateWString("screen_name_id", "$90102"),
				),
				rgdTemplateTable("type_armour_2", refTypeArmour,
					rgdTemplateWString("screen_name_id", "$90100"),
				),
				rgdTemplateTable("type_environment", refTypeEnvironment),
				rgdTemplateTable("type_speech", refNoSpeech),
				rgdTemplateTable("type_surface", refHeavyMetalSurface),
			),
			// What the player sees: the name, the icon, the build button.
			// The ghost of a building is on by default, which is what lets a
			// player place one before they can pay for it.
			rgdTemplateTable("ui_ext", refUIExt,
				rgdTemplateBool("ghost_auto_enable", false),
				rgdTemplateBool("ghost_enable", true),
				rgdTemplateBool("ghost_hidden_until_seen", true),
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
					rgdTemplateTable("help_text_list", refTextTable, rgdTemplateTextSlotsOf("$0")...),
					rgdTemplateString("icon_name", fmt.Sprintf("%s/%s_icon", unitTemplateIconBase(n.Race), n.Name)),
					rgdTemplateBool("no_button", false),
					rgdTemplateTable("override_help_text_list", refTextTable, rgdTemplateTextSlotsOf("$0")...),
					rgdTemplateBool("pseudo_leader", false),
					// The name the game shows. A real building points at an
					// entry of the string table, which a mod has to add; $0 is
					// what an unset one reads as.
					rgdTemplateWString("screen_name_id", "$0"),
					rgdTemplateBool("use_override_table_for_non_requisition_races", false),
				),
				rgdTemplateBool("use_hero_ui", false),
			),
		},
	}
	// The header of a structure file, which is the same in every one of them.
	return &rgd.RgdFile{HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1, Root: root}
}

// rgdTemplateModifierTable is one of the three modifier tables of a building:
// forty five slots, each a modifier the game applies. All of them are
// no_modifier except, where infiltration is wanted, the first.
//
// The two modifiers are not the same shape: an enabled modifier is applied by
// switching it on, with a value of -1, and a no_modifier is a multiplier of 1.
// That is the difference the game reads, so it is written out rather than
// left to the lua.
func rgdTemplateModifierTable(name string, firstEnablesInfiltration bool) *rgd.RgdEntry {
	children := make([]*rgd.RgdEntry, 0, structureTemplateModifierSlots)
	for i := 1; i <= structureTemplateModifierSlots; i++ {
		enable := firstEnablesInfiltration && i == 1
		ref := refNoModifier
		usage := refModUsageMultiply
		value := float32(1)
		if enable {
			ref = refEnableInfiltration
			usage = refModUsageEnable
			value = -1
		}
		children = append(children, rgdTemplateTable(
			rgdTemplateSlotName("modifier", i), ref,
			rgdTemplateTable("application_type", refModApplyToEntity),
			rgdTemplateBool("exclusive", false),
			rgdTemplateTable("modifier_class_name", refModClassName),
			rgdTemplateFloat("probability_of_applying", 1),
			rgdTemplateBool("shield_of_faith", false),
			rgdTemplateString("target_type_name", ""),
			rgdTemplateTable("usage_type", usage),
			rgdTemplateFloat("value", value),
		))
	}
	return rgdTemplateTable(name, refModifierTable, children...)
}

// The thirteen sections a generated building always has, in the order the game
// writes them. A test reads this, so that a section cannot be dropped from the
// template without the test saying so.
var structureTemplateSections = []string{
	"cost_ext",
	"entity_blueprint_ext",
	"event_manager_ext",
	"health_ext",
	"infiltration_ext",
	"modifier_apply_ext",
	"modifier_ext",
	"sim_entity_ext",
	"sight_ext",
	"structure_buildable_ext",
	"structure_ext",
	"type_ext",
	"ui_ext",
}
