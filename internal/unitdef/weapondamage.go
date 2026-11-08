package unitdef

import (
	"strings"

	"dow-mod-studio/internal/rgd"
)

// WeaponArmour is one line of a weapon's damage: the armour type it is measured
// against, and the value the weapon applies to it. A weapon file names the
// armour by the localisation id its own file carries — attrib\type_armour holds
// little more than that id — so the id is kept beside the text it resolves to,
// and the entry that holds the number is kept because that is what an edit
// writes back.
type WeaponArmour struct {
	// Armour is the armour type as the file names it, or the id when the id
	// does not resolve.
	Armour string
	// ID is the armour's screen_name_id, e.g. "$90104".
	ID string
	// Value is the number the weapon applies to that armour.
	Value string
	// Entry is the armour_piercing_value entry, the one an edit changes.
	Entry *rgd.RgdEntry
}

// WeaponDamages reads the damage a weapon does against every armour type.
//
// The numbers live under weapon_damage\armour_damage\armour_piercing_types, one
// entry per armour type, and a weapon that damages only through an area effect
// keeps the same table under area_effect\weapon_damage instead. Both are read,
// the direct table first, because that is the one a melee or a gun uses.
func WeaponDamages(rf *rgd.RgdFile) []WeaponArmour {
	ad := weaponArmourDamage(rf)
	if ad == nil {
		return nil
	}
	types := RgdFindChild(ad, "armour_piercing_types")
	if types == nil {
		return nil
	}
	var out []WeaponArmour
	for _, e := range types.Children {
		if e == nil {
			continue
		}
		val := RgdFindChild(e, "armour_piercing_value")
		arm := RgdFindPath(e, "armour_type", "screen_name_id")
		if val == nil || arm == nil {
			continue
		}
		name := strings.TrimSpace(arm.DisplayFieldValue())
		id := strings.TrimSpace(arm.DisplayValue())
		if name == "" {
			name = id
		}
		out = append(out, WeaponArmour{
			Armour: name,
			ID:     id,
			Value:  strings.TrimSpace(val.DisplayValue()),
			Entry:  val,
		})
	}
	return out
}

// weaponArmourDamage is the damage section of a weapon: its own, or the area
// effect's when the weapon damages only through that. A weapon file states its
// amounts in exactly one of the two.
func weaponArmourDamage(rf *rgd.RgdFile) *rgd.RgdEntry {
	if rf == nil || rf.Root == nil {
		return nil
	}
	if ad := RgdFindPath(rf.Root, "weapon_damage", "armour_damage"); ad != nil {
		return ad
	}
	return RgdFindPath(rf.Root, "area_effect", "weapon_damage", "armour_damage")
}

// WeaponDamageField is one scalar of the damage section: the amount itself, the
// type it is dealt as, and whatever else the file states beside the per-armour
// table. The tree lists them above the armour rows, and an edit writes back to
// the entry the row came from.
type WeaponDamageField struct {
	Label string
	Value string
	Entry *rgd.RgdEntry
}

// WeaponDamageFields reads those scalars in the order the file holds them.
// Tables are left out: the per-armour values are rows of their own, and a nested
// table is not a value one line can state.
func WeaponDamageFields(rf *rgd.RgdFile) []WeaponDamageField {
	ad := weaponArmourDamage(rf)
	if ad == nil {
		return nil
	}
	var out []WeaponDamageField
	for _, e := range ad.Children {
		if e == nil || e.IsTable() {
			continue
		}
		out = append(out, WeaponDamageField{Label: e.Name, Value: strings.TrimSpace(e.DisplayValue()), Entry: e})
	}
	return out
}

// WeaponArmourPiercing is the weapon's flat armour piercing, which is added to
// the per-armour values. It is "" for a weapon that declares none.
func WeaponArmourPiercing(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	if ad := RgdFindPath(rf.Root, "weapon_damage", "armour_damage"); ad != nil {
		if e := RgdFindChild(ad, "armour_piercing"); e != nil {
			return strings.TrimSpace(e.DisplayValue())
		}
	}
	return ""
}

// WeaponTableSlots is every slot of one hardpoint's weapon table, including the
// empty ones. The Weapons list shows only the slots that carry a weapon, but
// adding one means finding a slot that carries none, and those are the majority:
// a hardpoint ships with 32 of them.
func WeaponTableSlots(rf *rgd.RgdFile, hardpoint string) []*rgd.RgdEntry {
	if rf == nil || rf.Root == nil {
		return nil
	}
	table := RgdFindPath(rf.Root, "combat_ext", "hardpoints", hardpoint, "weapon_table")
	if table == nil {
		return nil
	}
	var out []*rgd.RgdEntry
	for _, w := range table.Children {
		if w == nil || !w.IsTable() {
			continue
		}
		out = append(out, w)
	}
	return out
}

// WeaponSlotRefEntry is the entry that names the weapon of a slot, which is the
// one an add or a remove writes.
func WeaponSlotRefEntry(slot *rgd.RgdEntry) *rgd.RgdEntry {
	if slot == nil {
		return nil
	}
	return RgdFindChild(slot, "weapon")
}
