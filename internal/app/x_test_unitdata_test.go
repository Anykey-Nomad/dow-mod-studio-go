package app

import (
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// formatUint renders a hash as the decimal a "$<number>" reference carries.
func formatUint(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// testModTreeWithFiles builds a merged tree by hand from virtual path to
// content, so the reference and catalog tests do not depend on the game files.
func testModTreeWithFiles(t *testing.T, files map[string]string) *modtree.ModTree {
	t.Helper()
	tree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		FileMapName: "TestMod",
	}
	src := &modtree.FileSource{ModName: "ThisMod", Kind: modtree.KindThisMod, Priority: 0}
	for path := range files {
		segs := strings.Split(path, `\`)
		// The tree's root already is "Data", so the leading segment of a
		// virtual path is dropped rather than becoming a child folder.
		if len(segs) > 1 && strings.EqualFold(segs[0], "Data") {
			segs = segs[1:]
		}
		if len(segs) < 2 {
			t.Fatalf("test path %q has no folder", path)
		}
		name := segs[len(segs)-1]
		cur := tree.Root
		for _, seg := range segs[:len(segs)-1] {
			cur = tree.GetOrCreateFolder(cur, seg, nil)
		}
		f := &modtree.TreeFile{Name: name, FullPath: path, Source: src}
		cur.Files = append(cur.Files, f)
		cur.FileByLower[strings.ToLower(name)] = f
	}
	return tree
}

// ── WString localisation ids ────────────────────────────────────────────────

func TestRgdLocID(t *testing.T) {
	cases := []struct {
		in     string
		id     string
		isID   bool
		unset  bool
		reason string
	}{
		{"$90100", "90100", true, false, "a screen_name_id is a localisation id"},
		{"$0", "0", true, true, "$0 is the game's empty marker"},
		{"$705750", "705750", true, false, "text_NN slots are localisation ids"},
		{"", "", false, false, "an empty value is not an id"},
		{"necron_lord", "", false, false, "a plain name is not an id"},
		{"$abc", "", false, false, "non-digits after $ are not an id"},
		{"$", "", false, false, "a bare $ is not an id"},
	}
	for _, c := range cases {
		id, ok := rgd.LocID(c.in)
		if ok != c.isID || id != c.id {
			t.Errorf("rgd.LocID(%q) = (%q,%v), want (%q,%v): %s", c.in, id, ok, c.id, c.isID, c.reason)
		}
		if got := rgd.LocIDIsUnset(c.in); got != c.unset {
			t.Errorf("rgd.LocIDIsUnset(%q) = %v, want %v: %s", c.in, got, c.unset, c.reason)
		}
	}
}

// A WString id is the value, so it must survive to the display. The old code
// looked it up in the RGD dictionary and, failing that, replaced it with the
// field name — which showed "screen_name_id" in place of "$705754".
func TestWStringIDIsNotReplacedByFieldName(t *testing.T) {
	e := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$705754"}
	if got := e.DisplayFieldValue(); got != "$705754" {
		t.Fatalf("WString display = %q, want the id %q unchanged", got, "$705754")
	}
	if got := e.DisplayValue(); got != "$705754" {
		t.Fatalf("WString raw value = %q, want %q", got, "$705754")
	}
}

// A DT_String is the other case: "$<number>" there really is a dictionary
// alias, and an unresolvable one still falls back to the field name.
func TestStringAliasStillResolvesThroughDictionary(t *testing.T) {
	e := &rgd.RgdEntry{Name: "some_field", Type: rgd.RgdTypeString, StrVal: "$999999"}
	if got := e.DisplayFieldValue(); got != "some_field" {
		t.Fatalf("String alias display = %q, want the field-name fallback %q", got, "some_field")
	}
	// A resolvable alias shows the name, not the number. The dictionary maps a
	// hash back to a name, so the reference is written as the hash of the name.
	name := "weapon_01"
	alias := &rgd.RgdEntry{Name: "f", Type: rgd.RgdTypeString, StrVal: "$" + formatUint(uint32(rgd.Hash(name)))}
	if got := alias.DisplayFieldValue(); got == "$"+formatUint(uint32(rgd.Hash(name))) {
		t.Fatalf("resolvable String alias %q still shows as a number; dictionary lookup failed", got)
	}
}

func TestWStringTooltipSaysLocalisation(t *testing.T) {
	e := &rgd.RgdEntry{Name: "text_01", Type: rgd.RgdTypeWString, WStrVal: "$705750"}
	got := rgdTooltipForValue(e)
	if !strings.Contains(got, "localisation") {
		t.Fatalf("WString tooltip = %q, want it to name the localisation table", got)
	}
	if !strings.Contains(got, "$705750") {
		t.Fatalf("WString tooltip = %q, want it to carry the id", got)
	}
}

// ── ability reference resolution ────────────────────────────────────────────

func TestResolveRgdRefSwapsLuaForRgd(t *testing.T) {
	// A ref is relative to Data\attrib and names a .lua; the asset on disk is
	// the .rgd beside it. The base is what differs per tree, so these cases use
	// a tree built by the test helper.
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\abilities\necron_solar_pulse.rgd`: "",
		`Data\attrib\ebpextensions\ability_ext.rgd`:    "",
	})
	cases := []struct {
		ref  string
		want string
	}{
		{`abilities\necron_solar_pulse.lua`, `Data\attrib\abilities\necron_solar_pulse.rgd`},
		{`ebpextensions\ability_ext.lua`, `Data\attrib\ebpextensions\ability_ext.rgd`},
		{`abilities/necron_solar_pulse.lua`, `Data\attrib\abilities\necron_solar_pulse.rgd`},
		{``, ``},
		{`abilities\nothing_here.lua`, ``},
	}
	for _, c := range cases {
		if got := resolveRgdRef(tree, c.ref); got != c.want {
			t.Errorf("resolveRgdRef(%q) = %q, want %q", c.ref, got, c.want)
		}
	}
}

func TestAbilityRefName(t *testing.T) {
	a := abilityRef{Slot: "ability_02", Ref: `abilities\necron_chronometron.lua`, Path: "x"}
	if got := a.Name(); got != "necron_chronometron" {
		t.Fatalf("abilityRef.name = %q, want %q", got, "necron_chronometron")
	}
	if !a.Resolved() {
		t.Fatal("an abilityRef with a path should report resolved")
	}
	var empty abilityRef
	if empty.Resolved() {
		t.Fatal("an abilityRef with no path must not report resolved")
	}
}

// ── catalog guards ──────────────────────────────────────────────────────────

// The dialog clamps its selection rather than trusting it: an index left over
// from a different mod must land on a real entry, never panic.
func TestCatalogClampsStaleIndexes(t *testing.T) {
	cat := &unitCatalog{Races: []raceEntry{{
		Name:       "necrons",
		Troops:     []unitEntry{{Path: "a", Name: "a"}},
		Structures: []unitEntry{{Path: "b", Name: "b"}},
	}}}
	for _, i := range []int{-5, 0, 1, 99} {
		if got := cat.ClampRaceSel(i); got != 0 {
			t.Errorf("clampRaceSel(%d) = %d, want 0", i, got)
		}
	}
	r := cat.SelectedRace(7)
	if r == nil || r.Name != "necrons" {
		t.Fatalf("selectedRace(7) = %+v, want the only race", r)
	}
	if u := cat.SelectedUnit(0, 99); u == nil || u.Name != "a" {
		t.Fatalf("selectedUnit(0,99) = %+v, want the first unit", u)
	}
	if got := r.UnitsOf(); len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("unitsOf = %+v, want troops before structures", got)
	}
}

func TestEmptyCatalogIsSafe(t *testing.T) {
	var nilCat *unitCatalog
	if nilCat.ClampRaceSel(3) != 0 {
		t.Error("a nil catalog must clamp to 0")
	}
	if nilCat.SelectedRace(0) != nil {
		t.Error("a nil catalog must have no race")
	}
	if nilCat.SelectedUnit(0, 0) != nil {
		t.Error("a nil catalog must have no unit")
	}
	if got := nilCat.AllUnits(); got != nil {
		t.Error("a nil catalog must have no units")
	}
	empty := &unitCatalog{}
	if empty.FindRace("necrons") != nil {
		t.Error("an empty catalog must find no race")
	}
	if u := empty.SelectedUnit(0, 0); u != nil {
		t.Error("an empty catalog must have no unit")
	}
	// A race with no units must report zero rather than panicking.
	var r raceEntry
	if r.UnitCount() != 0 {
		t.Error("a race with no units must count zero")
	}
	if r.ClampUnitSel(4) != 0 {
		t.Error("clampUnitSel on an empty race must be 0")
	}
}

// The tree in the dialog is the merged one, so a mod's own unit shadows an
// engine one of the same name rather than appearing twice.
func TestCatalogDeduplicatesByPath(t *testing.T) {
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`:  "",
		`Data\attrib\ebps\races\necrons\troops\necron_lord.whe`:  "",
		`Data\attrib\ebps\races\necrons\structures\monolith.rgd`: "",
	})
	cat := buildUnitCatalog(tree)
	if cat == nil {
		t.Fatal("buildUnitCatalog returned nil")
	}
	r := cat.FindRace("necrons")
	if r == nil {
		t.Fatalf("necrons not found in %+v", cat.Races)
	}
	// Only the .rgd is listed: the .whe is the same unit in another form.
	if len(r.Troops) != 1 || r.Troops[0].Name != "necron_lord" {
		t.Fatalf("troops = %+v, want just necron_lord", r.Troops)
	}
	if len(r.Structures) != 1 || r.Structures[0].Name != "monolith" {
		t.Fatalf("structures = %+v, want just monolith", r.Structures)
	}
	if r.Troops[0].Kind != unitKindTroop {
		t.Errorf("troop kind = %+v, want the troops kind", r.Troops[0].Kind)
	}
	if u, ok := cat.ByPath[`data\attrib\ebps\races\necrons\troops\necron_lord.rgd`]; !ok || u.Name != "necron_lord" {
		t.Errorf("byPath lookup failed: %+v ok=%v", u, ok)
	}
}

func TestEmptyWeaponRef(t *testing.T) {
	for _, ref := range []string{"", "   ", `weapons\no_weapon.lua`, "NO_WEAPON"} {
		if !unitdef.IsEmptyWeaponRef(ref) {
			t.Errorf("isEmptyWeaponRef(%q) = false, want true", ref)
		}
	}
	for _, ref := range []string{`weapons\necron_dirac_gun.lua`, `weapons\guard_lasgun.lua`} {
		if unitdef.IsEmptyWeaponRef(ref) {
			t.Errorf("isEmptyWeaponRef(%q) = true, want false", ref)
		}
	}
}

func TestWeaponNameFromRef(t *testing.T) {
	cases := map[string]string{
		`weapons\necron_dirac_gun.lua`: "necron_dirac_gun",
		`weapons/guard_lasgun.lua`:     "guard_lasgun",
		"":                             "",
	}
	for ref, want := range cases {
		if got := unitdef.WeaponNameFromRef(ref); got != want {
			t.Errorf("weaponNameFromRef(%q) = %q, want %q", ref, got, want)
		}
	}
}

// ── against the real mod ────────────────────────────────────────────────────

// Every unit of every race is listed, in the two folders the game keeps them in.
func TestRealCatalogListsRacesAndUnits(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	cat := buildUnitCatalog(tree)
	if len(cat.Races) < 5 {
		t.Fatalf("only %d races found, expected the full set", len(cat.Races))
	}
	necrons := cat.FindRace("necrons")
	if necrons == nil {
		t.Fatalf("necrons missing from %d races", len(cat.Races))
	}
	if necrons.UnitCount() == 0 {
		t.Fatal("necrons has no units")
	}
	// Troops and structures are both present and both non-empty.
	if len(necrons.Troops) == 0 {
		t.Error("necrons has no troops")
	}
	if len(necrons.Structures) == 0 {
		t.Error("necrons has no structures")
	}
	// Every listed unit must actually exist and parse.
	bad := 0
	for _, u := range necrons.UnitsOf() {
		if tree.FindFile(u.Path) == nil {
			t.Errorf("listed unit does not resolve: %s", u.Path)
			bad++
		}
	}
	if bad > 0 {
		t.Fatalf("%d listed units do not resolve", bad)
	}
}

// The real check of the .lua -> .rgd mapping: every ability slot of a real unit
// that names a file must resolve to that file.
func TestRealUnitAbilitiesResolve(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	cat := buildUnitCatalog(tree)

	named, resolved := 0, 0
	for _, r := range cat.Races {
		for _, u := range r.UnitsOf() {
			data, err := tree.ReadFile(u.Path)
			if err != nil {
				continue
			}
			rf, err := rgd.ParseRgd(data)
			if err != nil {
				continue
			}
			for _, a := range unitdef.UnitAbilities(tree, rf) {
				if a.Ref == "" {
					continue
				}
				named++
				if !a.Resolved() {
					t.Errorf("%s: ability slot %s ref %q did not resolve", u.Name, a.Slot, a.Ref)
					continue
				}
				resolved++
			}
		}
	}
	if named == 0 {
		t.Fatal("no ability references found at all")
	}
	// The mapping must hold everywhere, not just usually.
	if resolved != named {
		t.Fatalf("only %d of %d ability references resolved", resolved, named)
	}
	t.Logf("all %d ability references across %d races resolved", named, len(cat.Races))
}

// The "Unit info" list must only state things the file actually says, so every
// field it shows has to be present on the unit it was read from.
func TestRealUnitDetailsArePopulated(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	cat := buildUnitCatalog(tree)
	necrons := cat.FindRace("necrons")
	if necrons == nil {
		t.Skip("necrons not found")
	}

	populated, withAbilities, withWeapons, readErrors := 0, 0, 0, 0
	for _, u := range necrons.UnitsOf() {
		d := loadUnitDetails(tree, u)
		if d.Error != "" {
			readErrors++
			continue
		}
		if len(d.Fields) > 0 {
			populated++
		}
		if len(unitAbilityRows(d)) > 0 {
			withAbilities++
		}
		if len(d.Weapons) > 0 {
			withWeapons++
		}
	}
	total := necrons.UnitCount()
	if readErrors > 0 {
		t.Errorf("%d of %d necrons units failed to read", readErrors, total)
	}
	if populated != total-readErrors {
		t.Errorf("only %d of %d readable units produced info fields", populated, total-readErrors)
	}
	if withAbilities == 0 {
		t.Error("no necrons unit produced ability rows")
	}
	if withWeapons == 0 {
		t.Error("no necrons unit produced weapons")
	}
	t.Logf("necrons: %d units, %d with abilities, %d with weapons", total, withAbilities, withWeapons)
}

// A unit's details must survive the selection dance the dialog performs, and a
// repeated request must not re-read the file.
func TestUnitDetailsCacheFollowsSelection(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	cat := buildUnitCatalog(tree)
	ui := &uiState{ModTree: tree}
	if got := UnitCatalogFor(ui); got == nil {
		t.Fatal("unitCatalogFor returned nil for a real mod")
	}
	first := CurrentUnit(ui)
	if first == nil {
		t.Fatal("no unit selected for a real mod")
	}
	d1 := CurrentUnitDetails(ui)
	if d1 == nil {
		t.Fatal("no details for the first unit")
	}
	// A second call must return the same cached object.
	if d2 := CurrentUnitDetails(ui); d2 != d1 {
		t.Error("details were re-read instead of served from the cache")
	}
	// Moving to another unit must invalidate the cache.
	race := CurrentRace(ui)
	if race == nil || len(race.UnitsOf()) < 2 {
		t.Skip("race has fewer than two units")
	}
	ui.UnitSel = 1
	d3 := CurrentUnitDetails(ui)
	if d3 == d1 {
		t.Error("details were not invalidated when the unit changed")
	}
	if filepath.Base(d3.Entry.Path) != CurrentUnit(ui).DisplayName()+".rgd" {
		t.Errorf("details describe %s but the selection is %s", d3.Entry.Path, CurrentUnit(ui).Path)
	}
	_ = cat
}
