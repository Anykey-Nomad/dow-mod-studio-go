package unitdef

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ── unit details ────────────────────────────────────────────────────────────
//
// What the Object dialog shows for one unit. Everything is read out of the
// unit's own .rgd; nothing is invented, and a field the file does not carry is
// left out rather than shown as empty.
//
// The field names below were measured across the 582 unit .rgds of the DXP3 tree
// rather than guessed, because most of the obvious candidates do not exist:
// there is no health_ext.max_health (it is health_ext.hitpoints), no
// moving_ext.speed (it is moving_ext.speed_max) and no type_ext.template.

// UnitField is one label/value line of a unit's info or a weapon's settings.
type UnitField struct {
	Label string
	Value string
	// group is the section of the .rgd the row came from. It is what keeps a
	// list of a hundred scalars readable: without it the rows run together and
	// a reader cannot tell which part of the file a value belongs to.
	Group string
	// startsGroup marks the first row of a group, so the column can draw a
	// heading above it. It is decided where the list is built rather than in
	// the layout, because deciding it from the row's position in the list would
	// need the previous row and so would put list-building knowledge in the
	// drawing code.
	StartsGroup bool
	// isLocID marks a value that is a DT_WString localisation id, so the column
	// can present it as an id rather than as literal text.
	IsLocID bool
	// locID is that id without the "$", which is what the locale table is keyed
	// by. It is kept beside the display value because the value has the text
	// glued to the front of it, and a row that can be clicked has to know which
	// string it is about rather than re-deriving it from a label.
	LocID string
	// entry is the rgd.RgdEntry the value came from. It is what makes the row
	// editable: the value is only a string cut out of the parsed file, and
	// without the field behind it there is nowhere to write a change. A
	// localisation row needs it for the same reason, so every row that was
	// read from a field has one and the rows that count a list rather than
	// describe a field do not.
	Entry *rgd.RgdEntry
}

// UnitFieldIndex is where a field sits in a unit's rows, or -1. It is how a
// selection survives a rebuild of the list: a change to a value rebuilds the
// rows from the file, and the field is the thing that has to be found again
// rather than the index, which has moved if the change emptied a row.
func UnitFieldIndex(fields []UnitField, e *rgd.RgdEntry) int {
	if e == nil {
		return -1
	}
	for i := range fields {
		if fields[i].Entry == e {
			return i
		}
	}
	return -1
}

// LocaleRowKey names a localisation row for the pointer target that goes with
// it. The entry's own address is used rather than its label, because two fields
// of one unit can share a label — a weapon's "description" and another's — and
// two rows answering to one target would send both presses to the same field.
// The addresses are dropped with the parsed unit, which is what the map that
// holds the targets is cleared alongside.
func LocaleRowKey(entry *rgd.RgdEntry, label string) string {
	if entry == nil {
		return label
	}
	return fmt.Sprintf("%p\x00%s", entry, label)
}

// UnitWeapon is one weapon of the unit, which the game declares as a hardpoint
// holding a weapon table rather than as a flat weapon list.
type UnitWeapon struct {
	// hardpoint is the field name, e.g. "hardpoint_01".
	Hardpoint string
	// weapons are the entries of that hardpoint's weapon table, each a slot
	// with its own reference.
	Weapons []WeaponSlot
}

// WeaponSlot is one entry of a hardpoint's weapon table.
type WeaponSlot struct {
	// slot is the field name, e.g. "weapon_01".
	Slot string
	// ref is the table it points at, e.g. "weapons\necron_dirac_gun.lua".
	Ref string
	// name is ref's file name, which is how the game names the weapon.
	Name string
	// path is the weapon's own .rgd in the tree, Resolved from Ref: the file
	// writes a .lua because the game converts the .rgd in memory and refers to
	// the result by that name, so the .rgd of the same name is what is really
	// on disk. "" means the reference does not resolve.
	Path string
	// fields are the slot's scalar settings as the unit's own file carries
	// them — what the hardpoint says about the weapon rather than what the
	// weapon says about itself.
	Fields []UnitField
	// rgdFields are the settings read out of the weapon's own .rgd, grouped by
	// the section they sit in. This is the detailed information: damage, range,
	// cooldown and the rest live in the weapon file, not in the unit.
	RgdFields []UnitField
	// rgdError says why the weapon file could not be read, when it was found
	// but would not parse.
	RgdError string
}

// UnitDetails is everything the dialog shows for one unit.
type UnitDetails struct {
	Entry UnitEntry
	// fields is the "Unit info" list.
	Fields []UnitField
	// weapons feed the Weapons list; abilities carry their Resolved path so a
	// row can say whether the file was found.
	Weapons   []UnitWeapon
	Abilities []AbilityRef
	// name is the unit's screen_name_id, kept apart because the dialog puts it
	// in its title. It is a localisation id, so it is usually the id itself.
	Name string
	// file is the parsed .rgd the rows were read from, kept so a change to one
	// of its fields can be reflected without reading the file again. Giving a
	// unit a new localisation id changes a field in here, and the rows are
	// derived from the same tree.
	File *rgd.RgdFile
	// tree is the mod tree the details were read from, kept so a rebuild after
	// an edit can resolve the weapon references again rather than losing the
	// .rgd paths the first pass found.
	Tree *modtree.ModTree
	// error is set when the .rgd could not be read or parsed, in which case
	// everything else is empty.
	Error string
}

// HasContent reports whether anything could be read, so a column can say the
// unit could not be read instead of showing an empty list.
func (d *UnitDetails) HasContent() bool {
	if d == nil {
		return false
	}
	return len(d.Fields) > 0 || len(d.Weapons) > 0 || len(d.Abilities) > 0
}

// WeaponCount is how many weapon slots the unit has in total, used to label the
// Weapons section.
func (d *UnitDetails) WeaponCount() int {
	n := 0
	for _, w := range d.Weapons {
		n += len(w.Weapons)
	}
	return n
}

// LoadUnitDetails parses a unit's .rgd and pulls out the parts the dialog
// shows. A read or parse failure is reported inside the result rather than as an
// error return, because the unit is still listed and worth naming.
func LoadUnitDetails(tree *modtree.ModTree, entry UnitEntry) *UnitDetails {
	d := &UnitDetails{Entry: entry}
	if tree == nil {
		d.Error = "no mod loaded"
		return d
	}
	data, err := tree.ReadFile(entry.Path)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.File = rf
	d.Tree = tree
	d.Name = UnitScreenName(rf)
	d.Fields = UnitInfoFields(rf)
	d.Weapons = UnitHardpointWeapons(tree, rf)
	d.Abilities = UnitAbilities(tree, rf)
	return d
}

// RebuildFromFile re-reads the rows that are derived from the parsed .rgd, for
// when one of its fields has just been changed in memory — which is what giving
// an untranslated field a new localisation id does.
func (d *UnitDetails) RebuildFromFile() {
	if d == nil || d.File == nil {
		return
	}
	d.Name = UnitScreenName(d.File)
	d.Fields = UnitInfoFields(d.File)
	// The weapons go with them: a weapon slot's settings are rows of the same
	// kind, so an edit to one has to show in the right column as well as in the
	// info list, and both are derived from this one tree.
	d.Weapons = UnitHardpointWeapons(d.Tree, d.File)
}

// ScreenNameID is the id of the unit's own display name, or "" when the file
// carries none. It is what the object panel's editable row acts on.
func (d *UnitDetails) ScreenNameID() string {
	if d == nil || d.File == nil {
		return ""
	}
	e := RgdFindPath(d.File.Root, "ui_ext", "ui_info", "screen_name_id")
	if e == nil {
		return ""
	}
	id, ok := rgd.LocID(e.DisplayValue())
	if !ok {
		return ""
	}
	return id
}

// UnitScreenName is the unit's own display name. It is a localisation id in
// ui_ext\ui_info\screen_name_id, so the id is all the file carries; it is
// present on all 582 unit .rgds of the DXP3 tree.
func UnitScreenName(rf *rgd.RgdFile) string {
	e := RgdFindPath(rf.Root, "ui_ext", "ui_info", "screen_name_id")
	if e == nil {
		return ""
	}
	if rgd.LocIDIsUnset(e.DisplayValue()) {
		return ""
	}
	return e.DisplayFieldValue()
}

// UnitInfoFields builds the "Unit info" list in a fixed order, so it does not
// jump around as the selection changes. Each entry names the place it was read
// from, and a value the file does not carry is skipped rather than shown blank.
//
// The rows come from a table rather than from straight-line code, because the
// list is over a hundred scalars long and a reader comparing two units needs them
// in the same order. The table is measured data: every path in it was read out of
// the 432 troop .rgds of the DXP3 tree, and a path that no file carries is
// simply skipped, so a mod that adds a section gets a row without a code change.
//
// The sections shown before this one were ui_ext, cost_ext, health_ext,
// moving_ext, sight_ext and entity_blueprint_ext. The rest — combat, cover,
// melee, morale, requirements, modifiers and the like — were invisible in the
// Object window even though a unit carries them, which is what this table adds.
func UnitInfoFields(rf *rgd.RgdFile) []UnitField {
	if rf == nil || rf.Root == nil {
		return nil
	}
	var out []UnitField
	// add and addGrouped take the field the value was read from, so that the row
	// is editable from the object window rather than only readable. A row that
	// was counted rather than read has no field and passes nil.
	add := func(label, value string, e *rgd.RgdEntry) {
		if strings.TrimSpace(value) == "" {
			return
		}
		out = append(out, UnitField{Label: label, Value: value, Entry: e})
	}
	// addGrouped appends a row of a named section, marking the first row of each
	// section so the column knows where to put a heading. The heading is drawn
	// only for a section that has at least one row, which is why it is decided
	// here rather than after the fact.
	addGrouped := func(group, label, value string, e *rgd.RgdEntry) {
		if strings.TrimSpace(value) == "" {
			return
		}
		f := UnitField{Group: group, Label: label, Value: value, Entry: e}
		f.StartsGroup = group != "" && (len(out) == 0 || out[len(out)-1].Group != group)
		out = append(out, f)
	}
	addLoc := func(label string, e *rgd.RgdEntry) {
		if e == nil || rgd.LocIDIsUnset(e.DisplayValue()) {
			return
		}
		out = append(out, UnitField{
			Label:   label,
			Value:   e.DisplayFieldValue(),
			IsLocID: true,
			LocID:   RgdLocIDOrEmpty(e.DisplayValue()),
			Entry:   e,
		})
	}

	// The name is first and ungrouped: it is the unit's identity, not one
	// property among many, and it is the row the object panel also edits.
	addLoc("Name", RgdFindPath(rf.Root, "ui_ext", "ui_info", "screen_name_id"))
	hotkey := RgdFindPath(rf.Root, "ui_ext", "ui_hotkey_name")
	add("Hotkey", RgdEntryStr(hotkey), hotkey)

	for _, s := range UnitInfoScalars {
		e := RgdFindPath(rf.Root, s.Path...)
		addGrouped(s.Group, s.Label, RgdEntryStr(e), e)
	}
	for _, c := range UnitInfoCounts {
		n, ok := UnitListCount(rf.Root, c.Path, c.Filled)
		if !ok || n == 0 {
			continue
		}
		addGrouped(c.Group, c.Label, ItoaCount(n, "entry", "entries"), nil)
	}
	// The weapon and ability counts are stated here as well as being listed in
	// their own columns, because "this unit has no weapons" is a fact a reader
	// of the info column should not have to go and check for.
	return out
}

// unitInfoScalars is the plain scalar rows of the info list, in the order they
// are shown. Every path is a chain of child names below the unit's root table.
//
// The names come from the game's own files, and several of them are not what one
// would guess: there is no health_ext.max_health (it is health_ext.hitpoints),
// no moving_ext.speed (it is moving_ext.speed_max) and no ui_ext.Name (it is
// ui_ext.ui_info.screen_name_id). The labels are the readable forms.
var UnitInfoScalars = []struct {
	Group string
	Label string
	Path  []string
}{
	// ── cost ────────────────────────────────────────────────────────────────
	{"Cost", "Requisition", []string{"cost_ext", "time_cost", "cost", "requisition"}},
	{"Cost", "Power", []string{"cost_ext", "time_cost", "cost", "power"}},
	{"Cost", "Population", []string{"cost_ext", "time_cost", "cost", "population"}},
	{"Cost", "Faith", []string{"cost_ext", "time_cost", "cost", "faith"}},
	{"Cost", "Souls", []string{"cost_ext", "time_cost", "cost", "souls"}},
	{"Cost", "Build time", []string{"cost_ext", "time_cost", "time_seconds"}},

	// ── health ──────────────────────────────────────────────────────────────
	{"Health", "Hit points", []string{"health_ext", "hitpoints"}},
	{"Health", "Armour", []string{"health_ext", "armour"}},
	{"Health", "Minimum armour", []string{"health_ext", "armour_minimum"}},
	{"Health", "Invulnerable", []string{"health_ext", "invulnerable"}},
	{"Health", "Can be repaired", []string{"health_ext", "can_be_repaired"}},
	{"Health", "Max repairers", []string{"health_ext", "max_repairers"}},
	{"Health", "Regeneration", []string{"health_ext", "regeneration_rate"}},
	{"Health", "Regeneration decay in combat", []string{"health_ext", "regeneration_decrease_in_combat"}},
	{"Health", "Damage dealt regen factor", []string{"health_ext", "damage_dealt_regeneration_factor"}},
	{"Health", "Get back up chance", []string{"health_ext", "get_back_up_chance"}},
	{"Health", "Get back up time", []string{"health_ext", "get_back_up_get_up_time"}},
	{"Health", "Get back up health", []string{"health_ext", "get_back_up_health_percent"}},
	{"Health", "Get back up proximity", []string{"health_ext", "get_back_up_squad_proximity"}},
	{"Health", "Return from dead", []string{"health_ext", "return_from_dead_duration"}},
	{"Health", "Stays in pathfinding when dead", []string{"health_ext", "stay_in_pathfinding_after_dead_time"}},
	{"Health", "Keeps body", []string{"health_ext", "keep_persistent_body"}},
	{"Health", "Spawns on death", []string{"health_ext", "spawn_on_death"}},
	{"Health", "Spawns usable body", []string{"health_ext", "spawn_usable_body_on_death"}},
	{"Health", "Spawns death blossom", []string{"health_ext", "spawn_death_blossom"}},
	{"Health", "Spawns blood splat", []string{"health_ext", "spawn_blood_splat_on_death"}},
	{"Health", "Death event", []string{"health_ext", "death_event"}},
	{"Health", "Pre-death delay", []string{"health_ext", "pre_death_event_delay"}},
	{"Health", "Post-death delay", []string{"health_ext", "post_death_event_delay"}},
	{"Health", "Permanent death resource", []string{"health_ext", "death_resource_permanent"}},
	{"Health", "Death requisition", []string{"health_ext", "death_resource", "requisition"}},
	{"Health", "Death power", []string{"health_ext", "death_resource", "power"}},
	{"Health", "Death population", []string{"health_ext", "death_resource", "pop"}},
	{"Health", "Death faith", []string{"health_ext", "death_resource", "faith"}},
	{"Health", "Death souls", []string{"health_ext", "death_resource", "souls"}},
	{"Health", "Morale on death", []string{"health_ext", "morale_death"}},
	{"Health", "Poison damage", []string{"health_ext", "poison_damage"}},
	{"Health", "Poison duration", []string{"health_ext", "poison_damage_duration"}},
	{"Health", "Shows health bar", []string{"health_ext", "display_health_bar"}},

	// ── movement ────────────────────────────────────────────────────────────
	{"Movement", "Speed", []string{"moving_ext", "speed_max"}},
	{"Movement", "Acceleration time", []string{"moving_ext", "acceleration_time"}},
	{"Movement", "Deceleration time", []string{"moving_ext", "deceleration_time"}},
	{"Movement", "Rotation rate", []string{"moving_ext", "rotation_rate"}},
	{"Movement", "Corner anticipation", []string{"moving_ext", "corner_anticipation_time"}},
	{"Movement", "Min combat range", []string{"moving_ext", "move_to_min_combat_range"}},
	{"Movement", "Air unit", []string{"moving_ext", "air_unit"}},
	{"Movement", "Air float height", []string{"moving_ext", "air_unit_float_height"}},
	{"Movement", "Turning behaviour", []string{"moving_ext", "turning_behavior_template"}},

	// ── sight ───────────────────────────────────────────────────────────────
	{"Sight", "Sight range", []string{"sight_ext", "sight_radius"}},
	{"Sight", "Keen sight", []string{"sight_ext", "keen_sight_radius"}},

	// ── model ───────────────────────────────────────────────────────────────
	{"Model", "Model scale", []string{"entity_blueprint_ext", "scale_x"}},
	{"Model", "Model scale Y", []string{"entity_blueprint_ext", "scale_y"}},
	{"Model", "Model scale Z", []string{"entity_blueprint_ext", "scale_z"}},
	{"Model", "Animator", []string{"entity_blueprint_ext", "animator"}},
	{"Model", "Minimum update radius", []string{"entity_blueprint_ext", "minimum_update_radius"}},
	{"Model", "Collides", []string{"sim_entity_ext", "is_collide"}},
	{"Model", "In spatial bucket", []string{"sim_entity_ext", "is_in_spatial_bucket"}},
	{"Model", "No rotation", []string{"sim_entity_ext", "no_rotate"}},

	// ── combat ──────────────────────────────────────────────────────────────
	{"Combat", "Accuracy multiplier", []string{"combat_ext", "accuracy_multiplier"}},
	{"Combat", "Complex upgrades", []string{"combat_ext", "complex_upgrades"}},
	{"Combat", "Mass", []string{"special_attack_physics_ext", "mass"}},
	{"Combat", "Get up time", []string{"special_attack_physics_ext", "get_up_time"}},
	{"Combat", "Special attack disabled", []string{"special_attack_physics_ext", "disable_special_attack"}},
	{"Combat", "Face backwards when flying", []string{"special_attack_physics_ext", "face_backwards_when_flying"}},
	{"Combat", "Time between special attacks", []string{"special_attack_ext", "time_between_special_attacks"}},
	{"Combat", "Random extra time", []string{"special_attack_ext", "time_between_special_attacks_random"}},
	{"Combat", "Sync kill chance", []string{"synckill_ext", "chance"}},
	{"Combat", "Melee charge range", []string{"melee_ext", "charge_range"}},
	{"Combat", "Melee charge toggle", []string{"melee_ext", "charge_modifier_toggle"}},
	{"Combat", "Melee leap damage", []string{"melee_ext", "melee_leap_damage"}},
	{"Combat", "Melee leap speed", []string{"melee_ext", "melee_leap_speed"}},
	{"Combat", "Melee leap min distance", []string{"melee_ext", "melee_leap_min_distance"}},
	{"Combat", "Melee leap max target mass", []string{"melee_ext", "melee_leap_max_target_mass"}},
	{"Combat", "Melee leap curve ratio", []string{"melee_ext", "melee_leap_curve_ratio"}},
	{"Combat", "Melee leap tendency", []string{"melee_ext", "melee_leap_tendency"}},
	{"Combat", "Melee leap action", []string{"melee_ext", "melee_leap_action_name"}},

	// ── cover ───────────────────────────────────────────────────────────────
	{"Cover", "Light priority", []string{"cover_ext", "cover_light", "priority"}},
	{"Cover", "Heavy priority", []string{"cover_ext", "cover_heavy", "priority"}},
	{"Cover", "Blocking priority", []string{"cover_ext", "cover_blocking", "priority"}},
	{"Cover", "Negative priority", []string{"cover_ext", "cover_negative", "priority"}},
	{"Cover", "Stealth priority", []string{"cover_ext", "cover_stealth", "priority"}},
	{"Cover", "Light damage modifier", []string{"cover_ext", "light_damage_modifier"}},
	{"Cover", "Light speed modifier", []string{"cover_ext", "light_speed_modifier"}},
	{"Cover", "Heavy damage modifier", []string{"cover_ext", "heavy_damage_modifier"}},
	{"Cover", "Heavy speed modifier", []string{"cover_ext", "heavy_speed_modifier"}},
	{"Cover", "Negative damage modifier", []string{"cover_ext", "negative_damage_modifier"}},
	{"Cover", "Negative speed modifier", []string{"cover_ext", "negative_speed_modifier"}},

	// ── morale and infiltration ─────────────────────────────────────────────
	{"Morale", "Morale added", []string{"morale_add_ext", "inc_morale_max"}},
	{"Morale", "Morale rate", []string{"morale_add_ext", "inc_morale_rate"}},
	{"Infiltration", "Enemy opacity", []string{"infiltration_ext", "enemy_infiltrate_opacity"}},
	{"Infiltration", "Friendly opacity", []string{"infiltration_ext", "friendly_infiltrate_opacity"}},
	{"Infiltration", "Initial delay", []string{"infiltration_ext", "initial_delay_time"}},

	// ── abilities of the unit itself ────────────────────────────────────────
	{"Special", "Possession UI", []string{"possess_ext", "possession_ui_type"}},
	{"Special", "Possession recharge", []string{"possess_ext", "recharge_time"}},
	{"Special", "Possession health transfer", []string{"possess_ext", "transfer_health_percentage"}},
	{"Special", "Possessed squad", []string{"possess_ext", "squad_replacement_name"}},
	{"Special", "Possessed squad 2", []string{"possess_ext", "squad_replacement2_name"}},
	{"Special", "Possessed squad 3", []string{"possess_ext", "squad_replacement3_name"}},
	{"Special", "On create motion", []string{"spawn_ext", "oncreate_motion_name"}},
	{"Special", "On create motion duration", []string{"spawn_ext", "oncreate_motion_duration"}},
	{"Special", "On create event", []string{"spawn_ext", "oncreate_event_name"}},
	{"Special", "Mob value", []string{"mob_ext", "mob_value"}},
	{"Special", "Reanimate", []string{"type_ext", "reanimate"}},
	{"Special", "Can be possessed by enemy", []string{"type_ext", "can_be_possessed_by_enemy"}},
	{"Special", "Single player only", []string{"type_ext", "single_player_only"}},
	{"Special", "Repair per second", []string{"repair_ext", "health_repaired_per_second"}},
	{"Special", "Repair cost fraction", []string{"repair_ext", "percent_of_target_cost_for_full_repair"}},
	{"Special", "Autobuild", []string{"building_engineer_ext", "autobuild"}},

	// ── resource income and decay ───────────────────────────────────────────
	{"Resource", "Requisition per second", []string{"resource_ext", "requisition_per_second"}},
	{"Resource", "Power per second", []string{"resource_ext", "power_per_second"}},
	{"Resource", "Faith per second", []string{"resource_ext", "faith_per_second"}},
	{"Resource", "Decay enabled", []string{"resource_ext", "decay_enabled"}},
	{"Resource", "Decay delay", []string{"resource_ext", "decay_delay_time"}},
	{"Resource", "Decay lower limit", []string{"resource_ext", "decay_lower_limit_percentage"}},
	{"Resource", "Decay time to limit", []string{"resource_ext", "decay_to_lower_limit_time"}},

	// ── holding other squads ───────────────────────────────────────────────
	{"Squad hold", "Holds produced squads", []string{"squad_hold_ext", "holds_produced_squads"}},
	{"Squad hold", "Available spots", []string{"squad_hold_ext", "nr_available_spots"}},
	{"Squad hold", "Underground time", []string{"squad_hold_ext", "underground_time"}},
	{"Squad hold", "Unload delay", []string{"squad_hold_ext", "unload_delay"}},
	{"Squad hold", "Dark eldar raider", []string{"squad_hold_ext", "dark_eldar_raider"}},
	{"Squad hold", "Shared with same type", []string{"squad_hold_ext", "shared_with_other_same_type_units"}},
}

// unitInfoCounts are the list rows of the info column: sections whose value is
// how many entries they hold rather than a number of their own.
//
// A slot of 45 is shipped whether or not the unit uses it, so a raw child count
// would say the same thing about every unit and say nothing. Each of these
// therefore states a rule for what counts as a used entry, and the count is only
// shown when at least one entry passes it.
var UnitInfoCounts = []struct {
	Group string
	Label string
	Path  []string
	// filled reports whether one entry of the list is really in use. A nil
	// filled means every child counts.
	Filled func(*rgd.RgdEntry) bool
}{
	{"Lists", "Modifiers", []string{"modifier_apply_ext", "modifiers"}, ModifierSlotUsed},
	{"Lists", "In-combat modifiers", []string{"combat_ext", "in_combat_modifiers"}, ModifierSlotUsed},
	{"Lists", "Melee charge modifiers", []string{"melee_ext", "charge_modifiers"}, RefSlotUsed},
	{"Lists", "Melee modifiers", []string{"melee_ext", "in_melee_modifiers"}, ModifierSlotUsed},
	{"Lists", "Light cover modifiers", []string{"cover_ext", "cover_light", "modifiers"}, ModifierSlotUsed},
	{"Lists", "Heavy cover modifiers", []string{"cover_ext", "cover_heavy", "modifiers"}, ModifierSlotUsed},
	{"Lists", "Requirements", []string{"requirement_ext", "requirements"}, RequirementSlotUsed},
	{"Lists", "Abilities", []string{"ability_ext", "abilities"}, RefSlotUsed},
	{"Lists", "Event modifiers", []string{"event_manager_ext"}, EventSlotUsed},
	{"Lists", "Death explosions", []string{"death_explosion_ext", "death_explosions"}, DeathExplosionUsed},
	{"Lists", "Sync death motions", []string{"syncdeath_ext", "syncdeathinfos"}, MotionSlotUsed},
	{"Lists", "Special attacks", []string{"special_attack_ext", "special_attacks"}, SpecialAttackUsed},
}

// UnitListCount counts the children of a list that are really in use, and reports
// whether the list is there at all. A section the unit does not carry reports
// false rather than zero, so no row is shown for it.
func UnitListCount(root *rgd.RgdEntry, path []string, used func(*rgd.RgdEntry) bool) (int, bool) {
	table := RgdFindPath(root, path...)
	if table == nil {
		return 0, false
	}
	n := 0
	for _, c := range table.Children {
		if c == nil {
			continue
		}
		if used == nil || used(c) {
			n++
		}
	}
	return n, true
}

// ModifierSlotUsed reports whether a modifier entry says anything.
//
// This one took measuring. Every modifier table ships all 45 of its slots
// populated, and modifier_class_name is tp_modifier.lua in all 45 of every unit
// of the DXP3 tree — 45855 slots, one distinct value — so neither the child
// count nor the class name tells a real modifier from the template. What does is
// usage_type: the template is always tp_mod_usage_multiplication with an empty
// target_type_name, while a real one either multiplies something by name
// (target_type_name set, measured on 35 slots) or uses a different usage type
// (add, enable; measured on 91 slots).
//
// Measured on the 432 troops, this leaves 119 units with no modifier at all,
// 60 with one, 9 with two and 2 with twelve, against a raw child count that
// would have said "45" for every one of the 190 units that has the table.
func ModifierSlotUsed(e *rgd.RgdEntry) bool {
	if e == nil {
		return false
	}
	if strings.TrimSpace(RgdEntryStr(RgdFindChild(e, "target_type_name"))) != "" {
		return true
	}
	usage := RgdEntryStr(RgdFindChild(e, "usage_type"))
	return usage != "" && !strings.Contains(strings.ToLower(usage), "multiplication")
}

// RefSlotUsed reports whether a slot's reference names something. The game's
// "nothing here" placeholder is modifiers\no_modifier.lua, which is named in the
// list but is not an entry the unit has.
func RefSlotUsed(e *rgd.RgdEntry) bool {
	if e == nil {
		return false
	}
	ref := strings.TrimSpace(e.RefString())
	if ref == "" {
		// A plain string field rather than a reference: it counts when it says
		// anything, which is what an ability slot's path does.
		return strings.TrimSpace(e.DisplayValue()) != ""
	}
	return !strings.Contains(strings.ToLower(ref), "no_modifier")
}

// RequirementSlotUsed reports whether a requirement entry states anything. The
// requirements table ships every one of its slots with is_display_requirement
// set, so that field alone would make all eleven count on every unit.
func RequirementSlotUsed(e *rgd.RgdEntry) bool {
	for _, c := range e.Children {
		if c == nil {
			continue
		}
		if c.Name == "is_display_requirement" {
			continue
		}
		v := strings.TrimSpace(c.DisplayValue())
		if v == "" || v == "False" || v == "0" {
			continue
		}
		return true
	}
	return false
}

// EventSlotUsed reports whether an event_manager_ext slot names a modifier.
//
// This is the weakest of the counters and is kept because it is true rather
// than because it is sharp. The section ships 36 slots on every unit, and 22 to
// 29 of them name something on every unit of the DXP3 tree, out of 43 distinct
// names overall — so the number mostly says "this unit has the standard event
// hooks" and only occasionally distinguishes one unit from another. It is worth
// a row because a unit with 22 events and one with 29 really are different, and
// because a mod that strips them shows a different figure again.
func EventSlotUsed(e *rgd.RgdEntry) bool {
	name := strings.TrimSpace(RgdEntryStr(RgdFindChild(e, "modifier_name")))
	return name != "" && name != "$0"
}

// DeathExplosionUsed reports whether a death explosion entry can happen. The
// chance is the whole of it: an entry whose chance is zero never fires, and the
// table ships eight of them on a unit that has any at all.
func DeathExplosionUsed(e *rgd.RgdEntry) bool {
	chance := strings.TrimSpace(RgdEntryStr(RgdFindChild(e, "chance")))
	switch chance {
	case "", "0", "0.0":
		return false
	}
	return true
}

// MotionSlotUsed reports whether a syncdeath slot names a death motion. The table
// ships 18 slots and only a handful carry a motion.
func MotionSlotUsed(e *rgd.RgdEntry) bool {
	return strings.TrimSpace(RgdEntryStr(RgdFindChild(e, "dead_motion_name"))) != ""
}

// SpecialAttackUsed reports whether a special attack is really declared. A slot
// with no motion and no chance is an empty template, and there are eight of them
// on a unit that declares six.
func SpecialAttackUsed(e *rgd.RgdEntry) bool {
	if strings.TrimSpace(RgdEntryStr(RgdFindChild(e, "motion_name"))) != "" {
		return true
	}
	chance := strings.TrimSpace(RgdEntryStr(RgdFindChild(e, "chance")))
	return chance != "" && chance != "0"
}

// UnitHardpointWeapons reads the weapons a unit carries. Weapons are not a flat
// list on the unit: combat_ext\hardpoints holds up to sixteen hardpoints, each
// with its own weapon_table of variant slots. Hardpoints whose slots are all
// empty are left out, so the list holds the weapons that are really there —
// measured on DXP3, 441 of 582 units carry at least one.
//
// Slots that name the game's "no weapon" placeholder are dropped too, since
// naming the placeholder is not information about the unit.
func UnitHardpointWeapons(tree *modtree.ModTree, rf *rgd.RgdFile) []UnitWeapon {
	hp := RgdFindPath(rf.Root, "combat_ext", "hardpoints")
	if hp == nil {
		return nil
	}
	var out []UnitWeapon
	for _, h := range hp.Children {
		if h == nil {
			continue
		}
		table := RgdFindChild(h, "weapon_table")
		if table == nil {
			continue
		}
		var slots []WeaponSlot
		for _, w := range table.Children {
			if w == nil {
				continue
			}
			ref := w.RefString()
			if IsEmptyWeaponRef(ref) {
				continue
			}
			slots = append(slots, WeaponSlot{
				Slot:   w.Name,
				Ref:    ref,
				Name:   WeaponNameFromRef(ref),
				Path:   ResolveRgdRef(tree, ref),
				Fields: WeaponFields(w),
			})
		}
		if len(slots) == 0 {
			continue
		}
		out = append(out, UnitWeapon{Hardpoint: h.Name, Weapons: slots})
	}
	// The weapon's own file is where its details are: the unit's hardpoint
	// only says which weapon is carried and how it is mounted. Each Resolved
	// slot is read here, once per unit load, and a file that will not read is
	// reported on the slot rather than failing the whole unit.
	if tree != nil {
		for hi := range out {
			for si := range out[hi].Weapons {
				s := &out[hi].Weapons[si]
				if s.Path == "" {
					continue
				}
				data, err := tree.ReadFile(s.Path)
				if err != nil {
					s.RgdError = err.Error()
					continue
				}
				wrf, err := rgd.ParseRgd(data)
				if err != nil {
					s.RgdError = err.Error()
					continue
				}
				s.RgdFields = WeaponRgdFields(wrf)
			}
		}
	}
	return out
}

// WeaponRgdFields reads a weapon's own .rgd into rows: every scalar in the
// file, grouped by the top-level section it sits in. A weapon file is a stack
// of sections — damage_ext, range_ext, accuracy_ext and the like — and the
// properties column reads as sections rather than as one undifferentiated
// stream of names, the same way the unit's own info list is grouped.
//
// Localised strings are shown as the text they resolve to rather than as ids:
// the rows belong to a file that is not the one the object editor writes, so
// they are shown, not offered for editing.
func WeaponRgdFields(rf *rgd.RgdFile) []UnitField {
	if rf == nil || rf.Root == nil {
		return nil
	}
	var out []UnitField
	add := func(group, label string, e *rgd.RgdEntry) {
		if e == nil || e.IsTable() {
			return
		}
		value := e.DisplayValue()
		if e.Type == rgd.RgdTypeWString {
			if rgd.LocIDIsUnset(value) {
				return
			}
			value = e.DisplayFieldValue()
		}
		if strings.TrimSpace(value) == "" {
			return
		}
		f := UnitField{Label: label, Value: value, Group: group, Entry: e}
		f.StartsGroup = group != "" && (len(out) == 0 || out[len(out)-1].Group != group)
		out = append(out, f)
	}
	var walk func(group, prefix string, e *rgd.RgdEntry)
	walk = func(group, prefix string, e *rgd.RgdEntry) {
		for _, c := range e.Children {
			if c == nil {
				continue
			}
			label := c.Name
			if prefix != "" {
				label = prefix + "." + c.Name
			}
			if c.IsTable() {
				walk(group, label, c)
				continue
			}
			add(group, label, c)
		}
	}
	for _, sec := range rf.Root.Children {
		if sec == nil {
			continue
		}
		if sec.IsTable() {
			walk(sec.Name, "", sec)
		} else {
			add("", sec.Name, sec)
		}
	}
	return out
}

// emptyWeaponRefs are the placeholders the game puts in an unused weapon slot.
// An empty reference is the same thing without a name.
var EmptyWeaponRefs = []string{"no_weapon", "weapon_no", "no_weapon_", "default_no_weapon"}

// IsEmptyWeaponRef reports whether a weapon slot reference means "no weapon".
func IsEmptyWeaponRef(ref string) bool {
	r := strings.ToLower(strings.TrimSpace(ref))
	if r == "" {
		return true
	}
	for _, e := range EmptyWeaponRefs {
		if strings.Contains(r, e) {
			return true
		}
	}
	return false
}

// WeaponNameFromRef is the weapon's file name, which is how the game names it.
func WeaponNameFromRef(ref string) string {
	base := ref
	if i := strings.LastIndexAny(base, "\\/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// WeaponFields lists a weapon's scalar settings. Only scalars are taken: a
// nested table needs a column of its own, and these are the values one line can
// state.
func WeaponFields(w *rgd.RgdEntry) []UnitField {
	var out []UnitField
	for _, f := range w.Children {
		if f == nil || f.IsTable() {
			continue
		}
		v := f.DisplayValue()
		if strings.TrimSpace(v) == "" {
			continue
		}
		field := UnitField{Label: f.Name, Value: v, IsLocID: f.Type == rgd.RgdTypeWString, Entry: f}
		if field.IsLocID {
			field.LocID = RgdLocIDOrEmpty(v)
		}
		out = append(out, field)
	}
	return out
}

// RgdFindPath walks a chain of child names, returning nil if any step is
// missing, so a field the file does not have is skipped instead of panicking.
func RgdFindPath(e *rgd.RgdEntry, path ...string) *rgd.RgdEntry {
	cur := e
	for _, seg := range path {
		cur = RgdFindChild(cur, seg)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// RgdEntryStr is a nil-safe accessor for an entry's display value.
func RgdEntryStr(e *rgd.RgdEntry) string {
	if e == nil {
		return ""
	}
	return e.DisplayValue()
}

// FormatUnitFloat renders a float the way the game writes it: no trailing zeros,
// so 3100 stays "3100" rather than becoming "3100.0000".
func FormatUnitFloat(f float32) string {
	return strconv.FormatFloat(float64(f), 'f', -1, 32)
}
