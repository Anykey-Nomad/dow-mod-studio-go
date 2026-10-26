package unitdef

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

// StructureTemplateName is the file name stem a new building is written under.
type StructureTemplateName struct {
	// Race is the folder under "races", e.g. "necrons".
	Race string
	// Name is the file name stem, e.g. "necron_energy_core".
	Name string
}

// StructureTemplateVirtualPath is where a new building of that race lives.
func (n StructureTemplateName) StructureTemplateVirtualPath() string {
	return fmt.Sprintf(`Data\attrib\ebps\races\%s\structures\%s.rgd`, n.Race, n.Name)
}

// The references a building's sections are built on, over and above the ones a
// unit needs. They are the game's own files: a section that points at one
// inherits everything that file defines, which is why a generated building is
// small.
const (
	RefStructureExt       = `ebpextensions\structure_ext.lua`
	RefStructureBuild     = `ebpextensions\structure_buildable_ext.lua`
	RefInfiltrationExt    = `ebpextensions\infiltration_ext.lua`
	RefModifierApplyExt   = `ebpextensions\modifier_apply_ext.lua`
	RefAttachToTable      = `tables\attachto_table.lua`
	RefModApplyToEntity   = `type_modifierapplicationtype\tp_mod_apply_to_entity.lua`
	RefModClassName       = `type_modifier\tp_modifier.lua`
	RefModUsageEnable     = `type_modifierusagetype\tp_mod_usage_enable.lua`
	RefModUsageMultiply   = `type_modifierusagetype\tp_mod_usage_multiplication.lua`
	RefEnableInfiltration = `modifiers\enable_infiltration.lua`
	RefBuildingArmour     = `type_armour\tp_building_med.lua`
	RefHeavyMetalSurface  = `type_surface\tp_heavy_metal_armour.lua`
	RefNoSpeech           = `type_speech\speech_none.lua`
)

// The numbered slots the game gives a building. They are empty in a new
// building and filled by the weapons and upgrades it is given later, but the
// game reads the whole set, so they are written out.
const (
	// structureTemplateEventSlots is the event manager's slots, the same
	// thirty six a unit has.
	StructureTemplateEventSlots = 36
	// structureTemplateEventEntries is the entries inside one event slot.
	StructureTemplateEventEntries = 4
	// structureTemplateModifierSlots is the length of each of the three
	// modifier tables under modifier_apply_ext.
	StructureTemplateModifierSlots = 45
)

// NewStructureRgd builds the file of a new building: the thirteen sections the
// game's own structures carry, with the values they agree on and plain ones
// where they do not.
func NewStructureRgd(n StructureTemplateName) *rgd.RgdFile {
	model := UnitTemplateTitleCase(n.Name)
	raceDisplay := UnitTemplateRaceDisplayName(n.Race)
	// Thirty six event slots, each with four entries, all empty.
	eventSlots := RgdTemplateSlots("event", StructureTemplateEventSlots, func(i int) *rgd.RgdEntry {
		entries := make([]*rgd.RgdEntry, 0, StructureTemplateEventEntries+2)
		for e := 1; e <= StructureTemplateEventEntries; e++ {
			entries = append(entries, RgdTemplateTable(
				RgdTemplateSlotName("event_entry", e), RefEventEntry,
				RgdTemplateFloat("active_range_max", 0),
				RgdTemplateFloat("active_range_min", 0),
				RgdTemplateString("event_end_name", ""),
				RgdTemplateString("event_name", ""),
			))
		}
		entries = append(entries,
			RgdTemplateBool("has_active_range", false),
			RgdTemplateString("modifier_name", ""),
		)
		return RgdTemplateTable(RgdTemplateSlotName("event", i), RefEventTable, entries...)
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
			RgdTemplateTable("cost_ext", RefCostExt,
				RgdTemplateTable("time_cost", RefTimeCostTable,
					RgdTemplateTable("cost", RefCostTable,
						RgdTemplateFloat("faith", 0),
						RgdTemplateFloat("population", 0),
						RgdTemplateFloat("power", 0),
						RgdTemplateFloat("requisition", 250),
						RgdTemplateFloat("souls", 0),
					),
					RgdTemplateFloat("time_seconds", 30),
				),
			),
			// The model, and how big it is. A building is wider than it is
			// deep, which is what the 2/1/2 scale of the game's own
			// structures says.
			RgdTemplateTable("entity_blueprint_ext", RefBlueprintExt,
				RgdTemplateString("animator", fmt.Sprintf("Races/%s/Structures/%s", raceDisplay, model)),
				RgdTemplateFloat("minimum_update_radius", 5),
				RgdTemplateFloat("scale_x", 2),
				RgdTemplateFloat("scale_y", 1),
				RgdTemplateFloat("scale_z", 2),
			),
			RgdTemplateTable("event_manager_ext", RefEventManager, eventSlots...),
			// A building is tougher than a man and does not get back up.
			RgdTemplateTable("health_ext", RefHealthExt,
				RgdTemplateFloat("armour", 100),
				RgdTemplateFloat("armour_minimum", 0),
				RgdTemplateBool("can_be_repaired", true),
				RgdTemplateFloat("damage_dealt_regeneration_factor", 0),
				RgdTemplateString("death_event", "unit_death_events/building_death"),
				RgdTemplateTable("death_resource", RefResourceTable,
					RgdTemplateFloat("faith", 0),
					RgdTemplateFloat("pop", 0),
					RgdTemplateFloat("power", 0),
					RgdTemplateFloat("requisition", 0),
					RgdTemplateFloat("souls", 0),
				),
				RgdTemplateBool("death_resource_permanent", false),
				RgdTemplateBool("display_health_bar", true),
				RgdTemplateFloat("get_back_up_chance", 0),
				RgdTemplateFloat("get_back_up_get_up_time", 0),
				RgdTemplateFloat("get_back_up_health_percent", 0),
				RgdTemplateFloat("get_back_up_squad_proximity", 0),
				RgdTemplateFloat("hitpoints", 2000),
				RgdTemplateBool("invulnerable", false),
				// Unlike a unit, a building leaves no body to walk away
				// from: all 146 structures that carry the attribute set it
				// false.
				RgdTemplateBool("keep_persistent_body", false),
				RgdTemplateFloat("max_repairers", 3),
				RgdTemplateFloat("morale_death", 0),
				RgdTemplateFloat("poison_damage", 0),
				RgdTemplateFloat("poison_damage_duration", 0),
				RgdTemplateString("poison_event", ""),
				RgdTemplateFloat("poison_immunity_duration", 0),
				RgdTemplateFloat("poison_spread_delay", 5),
				RgdTemplateFloat("post_death_event_delay", 0),
				RgdTemplateFloat("pre_death_event_delay", 1),
				RgdTemplateFloat("regeneration_decrease_in_combat", 0),
				RgdTemplateFloat("regeneration_rate", 0),
				RgdTemplateFloat("return_from_dead_duration", 6),
				RgdTemplateString("spawn_blood_splat_on_death", ""),
				RgdTemplateString("spawn_death_blossom", ""),
				RgdTemplateString("spawn_on_death", ""),
				RgdTemplateString("spawn_usable_body_on_death", ""),
				RgdTemplateFloat("stay_in_pathfinding_after_dead_time", 0),
				RgdTemplateIndicatorEvent("usable_body_indicator_event"),
				RgdTemplateIndicatorEvent("usable_body_indicator_pre_event"),
			),
			// Whether troops can walk into it, and how visible they are while
			// they do. The three numbers are the same in all 145 structures
			// that have this section.
			RgdTemplateTable("infiltration_ext", RefInfiltrationExt,
				RgdTemplateFloat("enemy_infiltrate_opacity", 0.1),
				RgdTemplateFloat("friendly_infiltrate_opacity", 0.6),
				RgdTemplateFloat("initial_delay_time", 0),
			),
			// Three modifier tables of forty five slots each. The game reads
			// all of them, so all of them are written: the first slot of the
			// first table is the infiltration the section above describes, and
			// the rest are no_modifier, which is what they are in 120 of the
			// 150 structures.
			RgdTemplateTable("modifier_apply_ext", RefModifierApplyExt,
				RgdTemplateModifierTable("modifiers", true),
				RgdTemplateModifierTable("modifiers_idle", false),
				RgdTemplateModifierTable("modifiers_immediate", false),
			),
			// The game leaves this one empty in every structure it ships.
			RgdTemplateTable("modifier_ext", RefModifierExt),
			RgdTemplateTable("sim_entity_ext", RefSimEntityExt,
				RgdTemplateBool("is_collide", true),
				RgdTemplateBool("is_in_spatial_bucket", true),
				RgdTemplateBool("no_rotate", false),
			),
			RgdTemplateTable("sight_ext", RefSightExt,
				RgdTemplateFloat("keen_sight_radius", 0),
				RgdTemplateFloat("sight_radius", 15),
			),
			// What makes it a building rather than a prop: the section the
			// game reads to put it in the build menu, to decide what a player
			// gets back when it is destroyed, and what a refund is worth.
			RgdTemplateTable("structure_buildable_ext", RefStructureBuild,
				RgdTemplateBool("advanced_build_option", false),
				RgdTemplateFloat("build_menu_priority", 1),
				RgdTemplateFloat("power_gift", 0),
				RgdTemplateFloat("requisition_gift", 0),
				RgdTemplateFloat("return_power_percent", 0.5),
				RgdTemplateFloat("return_power_unbuilt_percent", 1),
				RgdTemplateFloat("return_requisition_percent", 0.5),
				RgdTemplateFloat("return_requisition_unbuilt_percent", 1),
			),
			// What the structure itself is: how it sits on the ground, and
			// what it turns into when a player captures it. The orientation of
			// 0 and the radius of 0 are the values every structure has.
			RgdTemplateTable("structure_ext", RefStructureExt,
				RgdTemplateTable("attach_to", RefAttachToTable,
					RgdTemplateBool("active", false),
					RgdTemplateTable("attach_to_tp", RefTypeEnvironment),
				),
				RgdTemplateBool("control_structure_is", false),
				RgdTemplateFloat("control_structure_radius", 0),
				RgdTemplateBool("control_structure_use", true),
				RgdTemplateBool("control_structure_use_allied", false),
				RgdTemplateFloat("extra_no_build_buffer", 1),
				RgdTemplateFloat("orientation", 0),
			),
			// A building is a mid armour structure on heavy metal, and it
			// does not speak: all 150 structures agree on the armour type and
			// the surface, and 145 of them on there being no voice.
			RgdTemplateTable("type_ext", RefTypeExt,
				RgdTemplateBool("can_be_possessed_by_enemy", true),
				RgdTemplateBool("reanimate", false),
				RgdTemplateBool("single_player_only", false),
				RgdTemplateBool("summon_teleport_building_check_override", false),
				RgdTemplateBool("summon_teleport_destination", true),
				RgdTemplateTable("type_armour", RefBuildingArmour,
					RgdTemplateWString("screen_name_id", "$90102"),
				),
				RgdTemplateTable("type_armour_2", RefTypeArmour,
					RgdTemplateWString("screen_name_id", "$90100"),
				),
				RgdTemplateTable("type_environment", RefTypeEnvironment),
				RgdTemplateTable("type_speech", RefNoSpeech),
				RgdTemplateTable("type_surface", RefHeavyMetalSurface),
			),
			// What the player sees: the name, the icon, the build button.
			// The ghost of a building is on by default, which is what lets a
			// player place one before they can pay for it.
			RgdTemplateTable("ui_ext", RefUIExt,
				RgdTemplateBool("ghost_auto_enable", false),
				RgdTemplateBool("ghost_enable", true),
				RgdTemplateBool("ghost_hidden_until_seen", true),
				RgdTemplateFloat("minimap_color_b", 255),
				RgdTemplateFloat("minimap_color_g", 255),
				RgdTemplateFloat("minimap_color_r", 255),
				RgdTemplateBool("minimap_enable", true),
				RgdTemplateBool("minimap_teamcolor", true),
				RgdTemplateString("selection_event", ""),
				RgdTemplateBool("selection_use_events", false),
				RgdTemplateString("speech_directory", ""),
				RgdTemplateBool("ui_hide_button_when_unable_to_build", false),
				RgdTemplateString("ui_hotkey_name", n.Name),
				RgdTemplateFloat("ui_index_hint", 1),
				RgdTemplateTable("ui_info", RefUIInfoTable,
					RgdTemplateWString("help_text_id", "$0"),
					RgdTemplateTable("help_text_list", RefTextTable, RgdTemplateTextSlotsOf("$0")...),
					RgdTemplateString("icon_name", fmt.Sprintf("%s/%s_icon", UnitTemplateIconBase(n.Race), n.Name)),
					RgdTemplateBool("no_button", false),
					RgdTemplateTable("override_help_text_list", RefTextTable, RgdTemplateTextSlotsOf("$0")...),
					RgdTemplateBool("pseudo_leader", false),
					// The name the game shows. A real building points at an
					// entry of the string table, which a mod has to add; $0 is
					// what an unset one reads as.
					RgdTemplateWString("screen_name_id", "$0"),
					RgdTemplateBool("use_override_table_for_non_requisition_races", false),
				),
				RgdTemplateBool("use_hero_ui", false),
			),
		},
	}
	// The header of a structure file, which is the same in every one of them.
	return &rgd.RgdFile{HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1, Root: root}
}

// RgdTemplateModifierTable is one of the three modifier tables of a building:
// forty five slots, each a modifier the game applies. All of them are
// no_modifier except, where infiltration is wanted, the first.
//
// The two modifiers are not the same shape: an enabled modifier is applied by
// switching it on, with a value of -1, and a no_modifier is a multiplier of 1.
// That is the difference the game reads, so it is written out rather than
// left to the lua.
func RgdTemplateModifierTable(name string, firstEnablesInfiltration bool) *rgd.RgdEntry {
	children := make([]*rgd.RgdEntry, 0, StructureTemplateModifierSlots)
	for i := 1; i <= StructureTemplateModifierSlots; i++ {
		enable := firstEnablesInfiltration && i == 1
		ref := RefNoModifier
		usage := RefModUsageMultiply
		value := float32(1)
		if enable {
			ref = RefEnableInfiltration
			usage = RefModUsageEnable
			value = -1
		}
		children = append(children, RgdTemplateTable(
			RgdTemplateSlotName("modifier", i), ref,
			RgdTemplateTable("application_type", RefModApplyToEntity),
			RgdTemplateBool("exclusive", false),
			RgdTemplateTable("modifier_class_name", RefModClassName),
			RgdTemplateFloat("probability_of_applying", 1),
			RgdTemplateBool("shield_of_faith", false),
			RgdTemplateString("target_type_name", ""),
			RgdTemplateTable("usage_type", usage),
			RgdTemplateFloat("value", value),
		))
	}
	return RgdTemplateTable(name, RefModifierTable, children...)
}

// The thirteen sections a generated building always has, in the order the game
// writes them. A test reads this, so that a section cannot be dropped from the
// template without the test saying so.
var StructureTemplateSections = []string{
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
