package app

import (
	"dow-mod-studio/internal/modgraph"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
)

// The reference graph between a mod's .rgd files lives in internal/modgraph:
// the entity/link model, the field vocabulary that says which field of a unit
// refers to which kind of file, and the index that answers "what points at
// this". The relationship view, the freezer and the unit picker read it under
// the names they always had, through the aliases and one-line helpers below.

// ── types ────────────────────────────────────────────────────────────────────

type (
	modGraph    = modgraph.ModGraph
	graphKind   = modgraph.GraphKind
	graphRole   = modgraph.GraphRole
	graphEntity = modgraph.GraphEntity
	graphLink   = modgraph.GraphLink
)

// ── kinds and roles ──────────────────────────────────────────────────────────

var (
	graphKindUnit        = modgraph.GraphKindUnit
	graphKindStructure   = modgraph.GraphKindStructure
	graphKindSquad       = modgraph.GraphKindSquad
	graphKindWeapon      = modgraph.GraphKindWeapon
	graphKindAbility     = modgraph.GraphKindAbility
	graphKindAddon       = modgraph.GraphKindAddon
	graphKindResearch    = modgraph.GraphKindResearch
	graphKindModifier    = modgraph.GraphKindModifier
	graphKindRequirement = modgraph.GraphKindRequirement
	graphKindFormation   = modgraph.GraphKindFormation
	graphKindEnvironment = modgraph.GraphKindEnvironment
	graphKindSchema      = modgraph.GraphKindSchema
	graphKindOther       = modgraph.GraphKindOther
)

// ── construction and scanning ────────────────────────────────────────────────

func newModGraph(tree *modtree.ModTree) *modGraph { return modgraph.NewModGraph(tree) }

func graphLinkKinds() []graphKind { return modgraph.GraphLinkKinds() }

func classifyGraphEntity(path string) (graphKind, string) {
	return modgraph.ClassifyGraphEntity(path)
}

func graphRoleForField(kind graphKind, path []string) (graphRole, string, bool) {
	return modgraph.GraphRoleForField(kind, path)
}

func collectGraphLinks(e *graphEntity, rf *rgd.RgdFile, emit func(graphLink)) {
	modgraph.CollectGraphLinks(e, rf, emit)
}

func requirementLeafIndex(path []string) int { return modgraph.RequirementLeafIndex(path) }

func resolveCandidates(rel, stem string) []string { return modgraph.ResolveCandidates(rel, stem) }

func refStem(ref string) string { return modgraph.RefStem(ref) }

func dedupeStrings(in []string) []string { return modgraph.DedupeStrings(in) }

// ── roles ───────────────────────────────────────────────────────────────────

const (
	roleWeapon         = modgraph.RoleWeapon
	roleAbility        = modgraph.RoleAbility
	roleAddon          = modgraph.RoleAddon
	roleResearch       = modgraph.RoleResearch
	roleSquad          = modgraph.RoleSquad
	roleBuiltBy        = modgraph.RoleBuiltBy
	roleNeedsAddon     = modgraph.RoleNeedsAddon
	roleNeedsResearch  = modgraph.RoleNeedsResearch
	roleBlueprint      = modgraph.RoleBlueprint
	roleRelocation     = modgraph.RoleRelocation
	roleEffect         = modgraph.RoleEffect
	roleModifies       = modgraph.RoleModifies
	roleSpawns         = modgraph.RoleSpawns
	roleProjectile     = modgraph.RoleProjectile
	roleNeedsStructure = modgraph.RoleNeedsStructure
	roleExcludesAddon  = modgraph.RoleExcludesAddon
)
