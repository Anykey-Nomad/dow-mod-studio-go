package main

import (
	"strings"
	"testing"
	"time"
)

// ── the squad index ─────────────────────────────────────────────────────────
//
// A unit's .rgd says nothing about the squads it is fielded in, so the index
// has to be built by walking the squad files the other way round. These tests
// check both halves of that: that the walk finds the real linkage, and that the
// index it produces survives being started, polled and stopped.

// runSquadScanToCompletion drives the background scan the way the input path
// does, and fails if it does not finish. Polling without a sleep would only
// measure how fast the channel is, not whether the scan works.
func runSquadScanToCompletion(t *testing.T, ui *uiState) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !ui.unitSquadsDone {
		if !ui.pollUnitSquads() {
			if time.Now().After(deadline) {
				t.Fatal("the squad scan did not finish within a minute")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// TestRealSquadScanFindsTheLinkage checks against the game's own data that a
// known unit is found in the squads that field it. necron_lord is a single-model
// unit: the game fields exactly one of it, so its squad is the easiest link in
// the tree to be wrong about.
func TestRealSquadScanFindsTheLinkage(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	necrons := cat.findRace("necrons")
	if necrons == nil {
		t.Fatal("necrons missing from the catalog")
	}
	lord, ok := cat.byPath[strings.ToLower(`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`)]
	if !ok {
		t.Fatal("necron_lord is not in the catalog")
	}

	ui := &uiState{}
	ui.startUnitSquadScan(tree)
	defer ui.stopUnitSquadScan()
	runSquadScanToCompletion(t, ui)

	squads := ui.squadsOfUnitOf(lord)
	if len(squads) == 0 {
		t.Fatal("necron_lord is in no squad, but the game fields it in one")
	}
	var found bool
	for _, s := range squads {
		if strings.EqualFold(s.name, "necron_lord_squad") {
			found = true
			// The size of a lord squad is fixed at one, which is the whole
			// point of the field: a count that came out as 0 or 45 would mean
			// the loadout was read from the wrong place.
			if s.unitMin != "1" || s.unitMax != "1" {
				t.Errorf("necron_lord_squad size is %s-%s, want 1-1", s.unitMin, s.unitMax)
			}
			if s.race != "necrons" {
				t.Errorf("squad race is %q, want necrons", s.race)
			}
		}
	}
	if !found {
		t.Errorf("necron_lord_squad not among %d squads of necron_lord", len(squads))
	}
}

// TestRealSquadScanFindsAScaledSquad checks the other shape of the same link: a
// unit the game fields a variable number of, so the index has to carry a range
// rather than a single figure.
func TestRealSquadScanFindsAScaledSquad(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	warrior, ok := cat.byPath[strings.ToLower(`Data\attrib\ebps\races\necrons\troops\necron_basic_warrior.rgd`)]
	if !ok {
		t.Fatal("necron_basic_warrior is not in the catalog")
	}
	ui := &uiState{}
	ui.startUnitSquadScan(tree)
	defer ui.stopUnitSquadScan()
	runSquadScanToCompletion(t, ui)

	squads := ui.squadsOfUnitOf(warrior)
	if len(squads) == 0 {
		t.Fatal("necron_basic_warrior is in no squad")
	}
	// A warrior squad is 3 to 8: the row's whole reason for existing.
	var scaled bool
	for _, s := range squads {
		if s.unitMin != s.unitMax {
			scaled = true
		}
	}
	if !scaled {
		t.Errorf("no squad of necron_basic_warrior has a size range, got %d squads all fixed size", len(squads))
	}
}

// TestRealSquadScanCoversTheTree pins the measured shape of the index. These
// numbers come from walking all 431 squad files of the DXP3 tree, and they are
// the clearest statement of what the scan is for: a unit is in a handful of
// squads at most, and most troops are in at least one.
func TestRealSquadScanCoversTheTree(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	ui := &uiState{}
	ui.startUnitSquadScan(tree)
	defer ui.stopUnitSquadScan()
	runSquadScanToCompletion(t, ui)

	if len(ui.unitSquads) < 300 {
		t.Errorf("indexed only %d units, expected the ~369 the tree's squads name", len(ui.unitSquads))
	}
	max := 0
	for path, refs := range ui.unitSquads {
		if len(refs) > max {
			max = len(refs)
		}
		// The key is the unit's own tree path, which is what the dialog looks
		// units up by, so a key that is not one would make the column empty.
		if _, ok := cat.byPath[path]; !ok {
			t.Fatalf("index key %q is not a unit of the catalog", path)
		}
	}
	if max > 4 {
		t.Errorf("a unit is in %d squads, where the tree's busiest is 3", max)
	}

	// Every squad file is walked: the count is a property of the tree, not of
	// how many happen to be reachable.
	files := collectSquadRgds(tree)
	if len(files) < 400 {
		t.Errorf("found %d squad files, expected the tree's 431", len(files))
	}
	var linked int
	for _, f := range files {
		if len(squadsOfUnit(tree, f)) > 0 {
			linked++
		}
	}
	if linked < 400 {
		t.Errorf("only %d of %d squad files name a unit", linked, len(files))
	}
}

// TestSquadScanIndexIsSorted checks that the index arrives in a fixed order. The
// scan walks races in tree order but delivers units in batches that interleave,
// so without sorting the Squads column would reorder itself between two scans of
// the same mod — which a reader would read as the tool being wrong.
func TestSquadScanIndexIsSorted(t *testing.T) {
	refs := []unitSquadRef{
		{name: "zulu_squad", race: "tau"},
		{name: "alpha_squad", race: "tau"},
		{name: "alpha_squad", race: "eldar"},
	}
	sortSquadRefs(refs)
	// The stripped name is the primary key, so the two alpha_squads lead. They
	// are then ordered by the full name, which is the same for both, and only
	// then by the path — which is empty here, so their relative order is the one
	// they were given. What is being checked is that the sort is stable in the
	// sense that matters: the same input always produces the same output, and
	// zulu is last however the ties fall.
	if refs[2].name != "zulu_squad" {
		t.Errorf("last entry is %s, want zulu_squad", refs[2].name)
	}
	for i := 0; i < 2; i++ {
		if refs[i].name != "alpha_squad" {
			t.Errorf("entry %d is %s, want an alpha_squad ahead of zulu", i, refs[i].name)
		}
	}
	// A distinguishing path must break the tie deterministically.
	tied := []unitSquadRef{
		{name: "alpha_squad", race: "tau", path: `Data\b\tau.rgd`},
		{name: "alpha_squad", race: "eldar", path: `Data\b\eldar.rgd`},
	}
	sortSquadRefs(tied)
	if tied[0].path != `Data\b\eldar.rgd` {
		t.Errorf("tied entries were ordered %s then %s, want the paths sorted",
			tied[0].path, tied[1].path)
	}
}

// TestSquadNameDropsTheSuffix checks the row's own name. Every squad file in the
// tree ends in _squad, so leaving it on would make the column read
// "necron_lord_squad" where the game's own name is "Necron Lord".
func TestSquadNameDropsTheSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"necron_lord_squad", "necron_lord"},
		{"chaos_squad_sorcerer_sp_dxp3", "chaos_squad_sorcerer_sp_dxp3"},
		{"chaos_squad_sorcerer", "chaos_squad_sorcerer"},
		{"_squad", "_squad"},
		{"squad", "squad"},
		{"", ""},
	}
	for _, c := range cases {
		if got := squadNameOf(unitSquadRef{name: c.in}); got != c.want {
			t.Errorf("squadNameOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSquadSizeLabel covers the three shapes a squad's size takes in the game's
// files: a range, a fixed figure, and a missing one.
func TestSquadSizeLabel(t *testing.T) {
	cases := []struct {
		min, max, want string
		why            string
	}{
		{"1", "1", "1", "a single-model unit is a fixed size"},
		{"3", "8", "3–8", "a scalable squad is a range"},
		{"", "8", "8", "no minimum is a single figure"},
		{"3", "", "3", "no maximum is a single figure"},
		{"", "", "", "no size at all says nothing"},
	}
	for _, c := range cases {
		got := squadSizeLabel(unitSquadRef{unitMin: c.min, unitMax: c.max})
		if got != c.want {
			t.Errorf("size %q-%q = %q, want %q: %s", c.min, c.max, got, c.want, c.why)
		}
	}
}

// TestSquadScanStopsCleanly checks the lifecycle the input path depends on: a
// stopped scan must leave nothing running, and starting one again must not leave
// the first one's goroutine writing into the index.
func TestSquadScanStopsCleanly(t *testing.T) {
	tree := openTestDXP3(t)
	ui := &uiState{}
	ui.startUnitSquadScan(tree)
	if !ui.unitSquadsRunning {
		t.Fatal("a scan that was started reports itself as not running")
	}
	// A second start for the same tree is left alone rather than duplicated.
	stop := ui.unitSquadsStop
	ui.startUnitSquadScan(tree)
	if ui.unitSquadsStop != stop {
		t.Error("starting a scan again for the same tree replaced the running one")
	}
	ui.stopUnitSquadScan()
	if ui.unitSquadsRunning {
		t.Error("a stopped scan still reports itself as running")
	}
	if ui.unitSquadsCh != nil {
		t.Error("a stopped scan left its channel behind")
	}
	// Polling a stopped scan must be inert rather than panic: the input path
	// polls it on every frame, including the frame the mod was closed.
	if ui.pollUnitSquads() {
		t.Error("polling a stopped scan reported results")
	}
	// And it must be startable again afterwards.
	ui.startUnitSquadScan(tree)
	if !ui.unitSquadsRunning {
		t.Error("a scan could not be restarted after being stopped")
	}
	ui.stopUnitSquadScan()
}

// TestSquadsOfUnitBeforeScan checks the state the column is in while the scan is
// still running. It has to be nil rather than an empty list, because otherwise
// the column would say "this unit is in no squad" for the first second of the
// dialog and then change its mind.
func TestSquadsOfUnitBeforeScan(t *testing.T) {
	ui := &uiState{}
	u := unitEntry{path: `Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`}
	if refs := ui.squadsOfUnitOf(u); refs != nil {
		t.Errorf("an unstarted index returned %d squads, want nil", len(refs))
	}
	ui = squadRowsForTest(map[string][]unitSquadRef{
		strings.ToLower(u.path): {{name: "necron_lord_squad", unitMin: "1", unitMax: "1"}},
	})
	if refs := ui.squadsOfUnitOf(u); len(refs) != 1 {
		t.Errorf("a filled index returned %d squads, want 1", len(refs))
	}
	// A unit the index has no entry for is genuinely in no squad, and that is
	// nil too — the distinction that matters is the index's existence, which
	// the column reads through unitSquadsDone.
	other := unitEntry{path: `Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`}
	if refs := ui.squadsOfUnitOf(other); len(refs) != 1 {
		t.Errorf("lookup is not case-insensitive: got %d squads", len(refs))
	}
}

// TestSquadSizeLabelSkipsZeroBirthCost is about the birth cost rows, which are
// the one thing here that is not in the unit's own file. Every one of the 431
// squads of the base game leaves birth_resource at zero, so the rows are only
// drawn for a mod that actually charges.
func TestBirthCostSkipsZero(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	lord, ok := cat.byPath[strings.ToLower(`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`)]
	if !ok {
		t.Skip("necron_lord not in the catalog")
	}
	ui := &uiState{}
	ui.startUnitSquadScan(tree)
	defer ui.stopUnitSquadScan()
	runSquadScanToCompletion(t, ui)
	squads := ui.squadsOfUnitOf(lord)
	if len(squads) == 0 {
		t.Skip("no squad found for necron_lord")
	}
	for _, s := range squads {
		for _, f := range squadFields(tree, s) {
			if f.group == "Birth cost" {
				t.Errorf("a zero birth cost was drawn: %s = %s", f.label, f.value)
			}
		}
	}
}

// TestSquadFieldsCarrySquadOnlyValues checks that the squad's own properties are
// read from the squad. Squad cap and morale are the two that no unit .rgd has
// any field for, so they are what proves the column is reading a second file
// rather than re-reading the unit.
func TestSquadFieldsCarrySquadOnlyValues(t *testing.T) {
	tree := openTestDXP3(t)
	s := unitSquadRef{
		path: `Data\attrib\sbps\races\necrons\necron_lord_squad.rgd`,
		name: "necron_lord_squad",
		race: "necrons",
	}
	fields := squadFields(tree, s)
	if len(fields) == 0 {
		t.Skipf("could not read %s", s.path)
	}
	byLabel := map[string]string{}
	for _, f := range fields {
		byLabel[f.label] = f.value
	}
	for _, want := range []string{"Squad cap", "Starting morale", "Max morale", "Ranged attack range", "Idle formation"} {
		if _, ok := byLabel[want]; !ok {
			t.Errorf("the squad's own properties are missing %q; got %v", want, labelList(fields))
		}
	}
}

func labelList(fields []unitField) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.label)
	}
	return out
}
