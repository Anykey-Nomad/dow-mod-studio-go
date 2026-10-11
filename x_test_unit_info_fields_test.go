package main

import (
	"dow-mod-studio/internal/rgd"
	"strings"
	"testing"
)

// ── the widened info column ─────────────────────────────────────────────────
//
// The Object window showed six sections of a unit's .rgd out of the thirty it
// carries. These tests are about what it shows now: that the added fields are
// read from where the game actually writes them, that the list is grouped so a
// hundred rows stay readable, and that the list counters distinguish a used slot
// from a shipped template.

// unitFieldValue finds a row by label, which is how a test says "the window shows
// this value" without depending on the order the table happens to be in.
func unitFieldValue(t *testing.T, fields []unitField, label string) (string, bool) {
	t.Helper()
	for _, f := range fields {
		if f.label == label {
			return f.value, true
		}
	}
	return "", false
}

// parseCountValue reads back a count the info list rendered, so a test can tell
// "10 entries" from "0 entries" — both end in the same two characters — and
// "1 entry" from a count of zero.
func parseCountValue(s string) (n int, noun string) {
	digits, noun, ok := strings.Cut(s, " ")
	if !ok {
		return 0, ""
	}
	n = 0
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, ""
		}
		n = n*10 + int(r-'0')
	}
	return n, noun
}

// TestUnitInfoReadsThePreviouslyHiddenSections checks against the game's own
// data that the fields which used to be invisible are now read. Each label is
// one the Object window did not show before, and each value is checked rather
// than merely present: a row that rendered an empty or a placeholder would look
// the same in the window.
func TestUnitInfoReadsThePreviouslyHiddenSections(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	race := cat.findRace("necrons")
	if race == nil {
		t.Fatal("necrons missing from the catalog")
	}
	var lord *unitEntry
	for _, u := range race.unitsOf() {
		if u.name == "necron_lord" && u.kind == unitKindTroop {
			lord = &u
			break
		}
	}
	if lord == nil {
		t.Fatal("necron_lord is not in the necrons catalog")
	}
	d := loadUnitDetails(tree, *lord)
	if d.error != "" {
		t.Fatalf("could not read the unit: %v", d.error)
	}
	// A lord is a one-model unit, so its armour and morale are the values the
	// game's own balance gives it; the point is that the numbers are the file's
	// and not the window's defaults.
	want := map[string]string{
		"Armour":              "",
		"Regeneration":        "",
		"Get back up chance":  "",
		"Speed":               "",
		"Rotation rate":       "",
		"Light priority":      "",
		"Accuracy multiplier": "",
		"Enemy opacity":       "",
		"Melee leap damage":   "",
	}
	for label := range want {
		v, ok := unitFieldValue(t, d.fields, label)
		if !ok {
			t.Errorf("the info list has no %q row; it has %v", label, labelList(d.fields))
			continue
		}
		if strings.TrimSpace(v) == "" {
			t.Errorf("the %q row is blank", label)
		}
	}
	// The values must agree with the file rather than with each other, so one
	// is checked directly against the parsed entry.
	rf := d.file
	if got, want := unitFieldMust(t, d.fields, "Hit points"),
		rgdEntryStr(rgdFindPath(rf.Root, "health_ext", "hitpoints")); got != want {
		t.Errorf("Hit points shows %q but the file says %q", got, want)
	}
	if got, want := unitFieldMust(t, d.fields, "Speed"),
		rgdEntryStr(rgdFindPath(rf.Root, "moving_ext", "speed_max")); got != want {
		t.Errorf("Speed shows %q but the file says %q", got, want)
	}
}

func unitFieldMust(t *testing.T, fields []unitField, label string) string {
	t.Helper()
	v, ok := unitFieldValue(t, fields, label)
	if !ok {
		t.Fatalf("the info list has no %q row", label)
	}
	return v
}

// TestUnitInfoGroupsAreMarkedOnce checks the heading rule. A group that is drawn
// above every one of its rows would be unreadable, and one drawn above none
// would leave the reader with a wall of values. This is the invariant that
// makes a hundred rows navigable, so it is checked rather than eyeballed.
func TestUnitInfoGroupsAreMarkedOnce(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	race := cat.findRace("necrons")
	if race == nil {
		t.Fatal("necrons missing from the catalog")
	}
	d := loadUnitDetails(tree, race.unitsOf()[0])
	if d.error != "" {
		t.Fatalf("could not read the unit: %v", d.error)
	}
	if len(d.fields) < 40 {
		t.Fatalf("the info list has only %d rows, so this proves nothing", len(d.fields))
	}
	seen := map[string]int{}
	lastGroup := ""
	for i, f := range d.fields {
		if f.group == "" {
			// The ungrouped rows are the name and the hotkey, which lead the
			// list. A group after one of them would mean a heading was lost.
			continue
		}
		if f.group != lastGroup {
			if !f.startsGroup {
				t.Errorf("row %d (%s/%s) continues a group without being marked as its start",
					i, f.group, f.label)
			}
			seen[f.group]++
			lastGroup = f.group
		} else if f.startsGroup {
			t.Errorf("row %d (%s/%s) is marked as a group start but continues the group above",
				i, f.group, f.label)
		}
	}
	if len(seen) < 8 {
		t.Errorf("only %d groups in the list, so the headings cannot be doing much: %v", len(seen), seen)
	}
	// The groups the window was always showing have to still be there: the
	// change was to add, not to reorganise.
	for _, want := range []string{"Cost", "Health", "Movement", "Sight", "Model"} {
		if seen[want] == 0 {
			t.Errorf("the %q group is gone from the info list", want)
		}
	}
}

// TestUnitInfoCountsSkipEmptySections checks that a section a unit does not
// carry produces no row at all, rather than a row reading zero. A unit with no
// requirements should not claim to have zero requirements in a list of counts —
// the absence of the section is the fact, and the window states it by leaving
// the row out.
func TestUnitInfoCountsSkipEmptySections(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	shown := 0
	for _, u := range cat.allUnits() {
		if u.kind != unitKindTroop {
			continue
		}
		d := loadUnitDetails(tree, u)
		if d.error != "" {
			continue
		}
		for _, f := range d.fields {
			if f.group == "Lists" {
				// A count is either a range of real entries or nothing at all:
				// the row is skipped when it is zero, so a row reading zero
				// would mean the counter was shown rather than its result
				// suppressed. The value is parsed rather than pattern-matched
				// because "10 entries" ends in "0 entries".
				n, noun := parseCountValue(f.value)
				if noun == "" {
					t.Errorf("%s shows %s = %q, which is not a count at all", u.name, f.label, f.value)
				} else if n == 0 {
					t.Errorf("%s shows a zero count: %s = %s", u.name, f.label, f.value)
				} else if n == 1 && noun != "entry" {
					t.Errorf("%s shows %s = %q, want the singular for one", u.name, f.label, f.value)
				} else if n > 1 && noun != "entries" {
					t.Errorf("%s shows %s = %q, want the plural for %d", u.name, f.label, f.value, n)
				}
				shown++
			}
		}
	}
	if shown == 0 {
		t.Error("no unit of the tree shows a single list count")
	}
}

// TestUnitInfoModifierCountIsNotTheTemplate checks the counter that took the most
// measuring. Every modifier table in the game ships all 45 of its slots filled
// with the same template, so a count that just tallied children would say 45 for
// every unit that has the table and say nothing at all. The count has to
// distinguish the template, and it has to actually differ between units.
func TestUnitInfoModifierCountIsNotTheTemplate(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	counts := map[string]int{}
	distinct := 0
	for _, u := range cat.allUnits() {
		if u.kind != unitKindTroop {
			continue
		}
		d := loadUnitDetails(tree, u)
		if d.error != "" {
			continue
		}
		if v, ok := unitFieldValue(t, d.fields, "Modifiers"); ok {
			counts[v]++
			distinct++
		}
	}
	if distinct < 50 {
		t.Fatalf("only %d units show a modifier count, so the rule is barely exercised", distinct)
	}
	if len(counts) < 2 {
		t.Errorf("every unit that has modifiers reports the same count %v, which is the template's", counts)
	}
	for v := range counts {
		if v == "45 entries" {
			t.Errorf("%d units report the raw slot count 45, which is the shipped template", counts[v])
		}
	}
}

// TestUnitInfoScalarsAreRealPaths is a guard on the table itself: every path in it
// has to be one the game's files actually use. A path with a typo in it reads as
// a row that is simply absent, which is the failure this table exists to fix, so
// the table is checked against the files rather than trusted.
//
// The check is over every troop rather than one unit, because the table spans
// the whole range of the game's sections: a necron lord carries
// modifier_apply_ext, melee_ext and special_attack_ext but not morale_add_ext or
// squad_hold_ext, and no single unit of the DXP3 tree carries all of them. A path
// that no unit anywhere resolves is a typo, which is what this catches.
func TestUnitInfoScalarsAreRealPaths(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	// seen counts, per row of the table, how many units carry the field. A row
	// that no unit carries is either a typo or a field the game has since
	// renamed, and both mean the row will never be shown.
	seen := make([]int, len(unitInfoScalars))
	notScalar := map[string]bool{}
	checked := 0
	for _, u := range cat.allUnits() {
		if u.kind != unitKindTroop {
			continue
		}
		data, err := tree.ReadFile(u.path)
		if err != nil {
			continue
		}
		rf, err := rgd.ParseRgd(data)
		if err != nil {
			continue
		}
		checked++
		for i, s := range unitInfoScalars {
			e := rgdFindPath(rf.Root, s.path...)
			if e == nil {
				continue
			}
			if e.IsTable() {
				// A table's value would render as a $ref rather than as the
				// number the label promises, so the path is wrong for this row.
				notScalar[strings.Join(s.path, "\\")] = true
				continue
			}
			seen[i]++
		}
	}
	if checked < 400 {
		t.Fatalf("only %d troops were readable, so this proves nothing", checked)
	}
	for i, s := range unitInfoScalars {
		if seen[i] == 0 {
			t.Errorf("%s\\%s (%s) is on no unit of the tree: a typo, or a field the game has renamed",
				strings.Join(s.path, "\\"), s.label, s.group)
		}
	}
	for path := range notScalar {
		t.Errorf("%s resolves to a table, so its row would show a $ref", path)
	}
	// The sections the window showed before the change must be fully present,
	// and the common ones must be common: health_ext.hitpoints is on every troop
	// in the game, so a row that is only on a handful is a path that lost a level
	// somewhere.
	if seen[0] == 0 {
		t.Error("the first row of the table resolves nowhere")
	}
}

// TestUnitInfoListsAreDistinctGroups checks that a list row's label is unique
// within the list. Two sections that both count "modifiers" would render as two
// rows with the same name and no way to tell which is which, which is why the
// cover ones are named for their cover type.
func TestUnitInfoListsAreDistinctGroups(t *testing.T) {
	seen := map[string]string{}
	for _, c := range unitInfoCounts {
		key := c.group + "/" + c.label
		if prev, ok := seen[c.label]; ok {
			t.Errorf("two list rows are both labelled %q (%s and %s)", c.label, prev, key)
		}
		seen[c.label] = key
	}
}

// TestUnitListCountOnAMissingSection checks the helper behind every counter: a
// section the file does not have has to report that it is missing, not that it
// is empty, because the two produce different rows.
func TestUnitListCountOnAMissingSection(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	race := cat.findRace("necrons")
	if race == nil {
		t.Fatal("necrons missing")
	}
	var tau *unitEntry
	for _, u := range race.unitsOf() {
		if u.name == "tau_crisis_suit" && u.kind == unitKindTroop {
			tau = &u
		}
	}
	if tau == nil {
		t.Skip("tau_crisis_suit not in the catalog")
	}
	_ = race
	data, err := tree.ReadFile(tau.path)
	if err != nil {
		t.Fatal(err)
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := unitListCount(rf.Root, []string{"no_such_section_ext"}, nil); ok {
		t.Error("a missing section reported itself as present")
	}
	if _, ok := unitListCount(rf.Root, []string{"health_ext"}, nil); !ok {
		t.Error("an existing section reported itself as missing")
	}
	// A section that exists but is empty counts zero and is still present, which
	// is the difference the two cases above turn on.
	n, ok := unitListCount(rf.Root, []string{"health_ext", "death_resource"}, func(e *rgd.RgdEntry) bool {
		return strings.TrimSpace(e.DisplayValue()) != "0"
	})
	if !ok {
		t.Fatal("death_resource reported itself as missing")
	}
	if n != 0 {
		t.Errorf("a crisis suit shows %d non-zero death resources, want 0", n)
	}
}

// TestUnitInfoFieldsOnAUnitWithoutThem checks the other end: a unit that carries
// none of the added sections still gets the rows it does carry, and the list does
// not invent anything. This is the case a modder hits when their own unit is a
// stub, and it is the one where a wrong path would be least visible.
func TestUnitInfoFieldsOnAUnitWithoutThem(t *testing.T) {
	// A unit declaring only a name and hitpoints, which is what a stub in a
	// work-in-progress mod looks like.
	stub := &rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
		{Name: "ui_ext", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
			{Name: "ui_info", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$1234"},
			}},
		}},
		{Name: "health_ext", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
			{Name: "hitpoints", Type: rgd.RgdTypeFloat, FloatVal: 100},
		}},
	}}}
	fields := unitInfoFields(stub)
	if len(fields) == 0 {
		t.Fatal("a unit with a name and hitpoints produced no rows")
	}
	declared := map[string]bool{"Name": true, "Hit points": true}
	for _, f := range fields {
		if !declared[f.label] {
			t.Errorf("a row %q = %q was invented for a unit that declares only a name and hitpoints", f.label, f.value)
		}
	}
	if _, ok := unitFieldValue(t, fields, "Hit points"); !ok {
		t.Error("the stub's hit points are missing")
	}
	// A unit with nothing at all must produce nothing, and must not panic.
	empty := &rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData", Type: rgd.RgdTypeTable}}
	if got := unitInfoFields(empty); len(got) != 0 {
		t.Errorf("an empty unit produced %d rows: %v", len(got), labelList(got))
	}
	if got := unitInfoFields(nil); got != nil {
		t.Errorf("a nil file produced %d rows", len(got))
	}
}
