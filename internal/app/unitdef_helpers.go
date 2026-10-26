package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
)

// The unit definitions — the catalog of races and units a mod declares, the
// details of the one on show, and the templates a new object is written from —
// live in internal/unitdef. Everything the rest of the program (and the tests)
// reach for keeps the name it had when they were all one package: the types are
// aliases, so their fields and methods read unchanged, and the functions are
// one-line helpers over the exported ones.
//
// Grouping them here rather than sprinkling unitdef. across the window code is
// what lets a definition be moved again without touching the files that draw it.

// ── types ───────────────────────────────────────────────────────────────────

type (
	unitKind              = unitdef.UnitKind
	unitEntry             = unitdef.UnitEntry
	raceEntry             = unitdef.RaceEntry
	unitCatalog           = uistate.UnitCatalog
	abilityRef            = unitdef.AbilityRef
	unitField             = unitdef.UnitField
	unitWeapon            = unitdef.UnitWeapon
	weaponSlot            = unitdef.WeaponSlot
	unitDetails           = uistate.UnitDetails
	unitTemplateName      = uistate.UnitTemplateName
	structureTemplateName = uistate.StructureTemplateName
	fieldKind             = unitdef.FieldKind
	fieldSpec             = unitdef.FieldSpec
)

// ── formatting helpers ──────────────────────────────────────────────────────

// boneAngleDeg maps any angle onto the rotation fields' 1..360 degree range.
func boneAngleDeg(d float64) float64 { return unitdef.BoneAngleDeg(d) }

// ItoaPlain lives in uikit; the alias keeps every call site as it was.
var itoaPlain = uikit.ItoaPlain

// itoaCount is a count with the noun it counts: "1 unit", "52 units".
func itoaCount(n int, one, many string) string { return unitdef.ItoaCount(n, one, many) }

// rgdLocIDOrEmpty is rgd.LocID with the failure folded into an empty id.
func rgdLocIDOrEmpty(value string) string { return unitdef.RgdLocIDOrEmpty(value) }

// ── catalog and details ──────────────────────────────────────────────────────

// buildUnitCatalog reads a mod's races and units into the picker's catalog.
func buildUnitCatalog(tree *modtree.ModTree) *unitCatalog {
	return unitdef.BuildUnitCatalog(tree)
}

// loadUnitDetails reads one unit's fields, weapons and abilities.
func loadUnitDetails(tree *modtree.ModTree, entry unitEntry) *unitDetails {
	return unitdef.LoadUnitDetails(tree, entry)
}

// localeRowKey is the loc-file key of one details row.
func localeRowKey(entry *rgd.RgdEntry, label string) string {
	return unitdef.LocaleRowKey(entry, label)
}

// unitFieldIndex is the row of details an rgd entry belongs to, or -1.
func unitFieldIndex(fields []unitField, e *rgd.RgdEntry) int {
	return unitdef.UnitFieldIndex(fields, e)
}

// unitListCount counts the filled entries of a table under path.
func unitListCount(root *rgd.RgdEntry, path []string, used func(*rgd.RgdEntry) bool) (int, bool) {
	return unitdef.UnitListCount(root, path, used)
}

// resolveRgdRef turns a efs\... reference into the path that loads it.
func resolveRgdRef(tree *modtree.ModTree, ref string) string {
	return unitdef.ResolveRgdRef(tree, ref)
}

// ── rgd helpers ──────────────────────────────────────────────────────────────

var (
	rgdFindChild = unitdef.RgdFindChild
	rgdFindPath  = unitdef.RgdFindPath
	rgdEntryStr  = unitdef.RgdEntryStr
)

// ── slot checks ──────────────────────────────────────────────────────────────

func modifierSlotUsed(e *rgd.RgdEntry) bool    { return unitdef.ModifierSlotUsed(e) }
func refSlotUsed(e *rgd.RgdEntry) bool         { return unitdef.RefSlotUsed(e) }
func requirementSlotUsed(e *rgd.RgdEntry) bool { return unitdef.RequirementSlotUsed(e) }

// ── templates and kinds ──────────────────────────────────────────────────────

func unitTemplateIconBase(race string) string              { return unitdef.UnitTemplateIconBase(race) }
func newUnitRgd(n unitTemplateName) *rgd.RgdFile           { return unitdef.NewUnitRgd(n) }
func newStructureRgd(n structureTemplateName) *rgd.RgdFile { return unitdef.NewStructureRgd(n) }

var (
	bonePositionField = unitdef.BonePositionField
	boneRotationField = unitdef.BoneRotationField
	unitKindTroop     = unitdef.UnitKindTroop
	unitKindStructure = unitdef.UnitKindStructure
)
