package main

import (
	"strings"
	"testing"
)

// The relationships view and the report are the two things the graph is for, so
// they are tested where the graph is small enough to know the answer: what each
// one says about a unit that is wired up, and about a reference that is not.

func TestLinkRowsSayBothDirectionsAndWhichIsWhich(t *testing.T) {
	tree := newGraphTestMod(t)
	g := newModGraph(tree)
	g.ensureLinks()

	// The gauntlet refers out once, to the armoury, and is referred to twice:
	// by the ability and by the add-on, both of which upgrade it. The two
	// directions have to be told apart, because a freeze works from the second.
	rows := linkRows(g, graphTestGauntlet)
	if len(rows) != 3 {
		t.Fatalf("the gauntlet has %d rows, want one out and two in: %+v", len(rows), rows)
	}
	var r linkRow
	for _, one := range rows {
		if one.outgoing {
			r = one
		}
	}
	if r.role != "built by" {
		t.Errorf("the gauntlet's own reference is a %q, want built by", r.role)
	}
	if r.target != "necron_armoury" {
		t.Errorf("the gauntlet is built by %q, want necron_armoury", r.target)
	}
	if !r.found {
		t.Error("the gauntlet's reference did not resolve")
	}

	// The other end of the same link is the other direction of the same
	// relationship, so the armoury lists the gauntlet as something referring to
	// it, and lists the add-on as something waiting on it. This is the
	// direction a freeze works from.
	back := linkRows(g, graphTestArmoury)
	if len(back) != 2 {
		t.Fatalf("the armoury has %d rows, want the two references pointing at it", len(back))
	}
	// One of the two is the gauntlet saying who builds it. The other is the
	// add-on saying it needs the armoury finished — the same field name, the
	// opposite meaning, and the only thing telling them apart is which kind of
	// file is holding it. Read as "built by" the add-on would appear to build
	// the building it is gated behind, and the armoury would list itself as
	// its own product.
	roles := map[string]string{}
	for _, one := range back {
		if one.outgoing {
			t.Error("the armoury's row is marked as its own reference, but it is the one being referred to")
		}
		if !one.found {
			t.Errorf("the reference from %s did not resolve", one.target)
		}
		roles[one.role] = one.target
	}
	if roles["built by"] != "necron_gauntlet" {
		t.Errorf("the armoury is built-by %q, want necron_gauntlet", roles["built by"])
	}
	if roles["needs structure"] != "necron_hubris" {
		t.Errorf("the armoury is needed by %q, want necron_hubris", roles["needs structure"])
	}
}

func TestLinkRowsSplitIntoTheTwoDirections(t *testing.T) {
	g := newModGraph(newGraphTestMod(t))
	g.ensureLinks()
	// The gauntlet refers out once and is referred to twice, so both sides of
	// the split are populated for it — which is the normal case, and the one
	// that a list showing only outgoing links would get wrong.
	outgoing, incoming := groupLinkRows(linkRows(g, graphTestGauntlet))
	if len(outgoing) != 1 || len(incoming) != 2 {
		t.Fatalf("the gauntlet's split gave %d out and %d in, want 1 and 2", len(outgoing), len(incoming))
	}
	if !outgoing[0].outgoing {
		t.Error("a row is on the wrong side of the split")
	}
	// The loose unit is wired to nothing at all, so it can put a list
	// entirely on one side.
	outgoing, incoming = groupLinkRows(linkRows(g, graphTestLoose))
	if len(outgoing) != 0 || len(incoming) != 0 {
		t.Fatalf("the loose unit's split gave %d out and %d in, want none of either", len(outgoing), len(incoming))
	}
	// And the add-on refers out twice, once at the unit it upgrades and once
	// at the building that unlocks it, and is referred to by nobody.
	outgoing, incoming = groupLinkRows(linkRows(g, graphTestHubris))
	if len(outgoing) != 2 || len(incoming) != 0 {
		t.Fatalf("the add-on's split gave %d out and %d in, want 2 and 0", len(outgoing), len(incoming))
	}
	for _, one := range outgoing {
		if !one.outgoing {
			t.Error("a row is on the wrong side of the split")
		}
	}
}

func TestAUnitThatRefersToNothingHasNoRows(t *testing.T) {
	g := newModGraph(newGraphTestMod(t))
	g.ensureLinks()
	if rows := linkRows(g, graphTestLoose); len(rows) != 0 {
		t.Errorf("a unit nothing refers to and which refers to nothing has %d rows: %v", len(rows), rows)
	}
}

func TestAUnitDoesNotListItselfTwice(t *testing.T) {
	// A building whose requirement names itself is normal in the game, and
	// saying so once is enough.
	tree := newGraphTestMod(t)
	g := newModGraph(tree)
	g.ensureLinks()
	for _, r := range linkRows(g, graphTestArmoury) {
		if !r.outgoing && strings.EqualFold(r.target, "necron_armoury") {
			t.Error("the armoury lists itself as something that refers to it")
		}
	}
}

func TestGraphEntityNameFallsBackToTheFileName(t *testing.T) {
	g := newModGraph(newGraphTestMod(t))
	if got := graphEntityName(g, graphTestGauntlet); got != "necron_gauntlet" {
		t.Errorf("a known entity is called %q", got)
	}
	if got := graphEntityName(g, `Data\attrib\weapon\never_seen.rgd`); got != "never_seen" {
		t.Errorf("an unknown path is called %q, want its file name", got)
	}
}

func TestBrokenLinkGroupsCollectOneRowPerMissingFile(t *testing.T) {
	tree := newGraphTestMod(t)
	// A unit naming a weapon that is not in the mod: the one thing this whole
	// feature exists to find.
	broken := `Data\attrib\ebps\races\necrons\troops\necron_broken.rgd`
	appendBrokenUnit(t, tree, broken, `weapon\necron_missing_gun.lua`)

	g := newModGraph(tree)
	g.ensureLinks()
	if g.BrokenLinksOf(broken) == nil {
		t.Fatal("the broken unit has no broken links, so the fixture did not take")
	}
	groups := brokenLinkGroups(g)
	if len(groups) != 1 {
		t.Fatalf("the report has %d rows, want one for the single missing file", len(groups))
	}
	grp := groups[0]
	if grp.ref != `weapon\necron_missing_gun.lua` {
		t.Errorf("the row names %q", grp.ref)
	}
	if grp.role != roleWeapon {
		t.Errorf("the row says the reference was a %q, want weapon", grp.role.label())
	}
	if len(grp.holders) != 1 || !strings.EqualFold(grp.holders[0], broken) {
		t.Errorf("the row says the files that still point at it are %v", grp.holders)
	}
	// A mod with nothing broken reports nothing, rather than a row saying so.
	if rows := brokenLinkGroups(newModGraph(newGraphTestMod(t))); len(rows) != 0 {
		t.Errorf("a mod with no broken references has %d rows", len(rows))
	}
}

func TestBrokenLinkGroupsPutTheMostUsedMissingFileFirst(t *testing.T) {
	tree := newGraphTestMod(t)
	appendBrokenUnit(t, tree, `Data\attrib\ebps\races\necrons\troops\necron_one.rgd`, `weapon\rare.lua`)
	appendBrokenUnit(t, tree, `Data\attrib\ebps\races\necrons\troops\necron_two.rgd`, `weapon\common.lua`)
	appendBrokenUnit(t, tree, `Data\attrib\ebps\races\necrons\troops\necron_three.rgd`, `weapon\common.lua`)

	g := newModGraph(tree)
	g.ensureLinks()
	groups := brokenLinkGroups(g)
	if len(groups) != 2 {
		t.Fatalf("the report has %d rows, want two", len(groups))
	}
	// The file three units lost comes first, because that is the one the
	// author most needs to see.
	if len(groups[0].holders) != 2 || groups[0].ref != `weapon\common.lua` {
		t.Errorf("the first row is %q held by %d files, want common.lua held by 2", groups[0].ref, len(groups[0].holders))
	}
}

func TestTheBackgroundScanFindsWhatTheBlockingReadFinds(t *testing.T) {
	// The window reads the links off the UI thread, so the two paths have to
	// agree: a list that filled in differently from the report would be worse
	// than either.
	blocking := newModGraph(openTestDXP3(t))
	blocking.ensureLinks()

	ui := newUI(unitTestTheme())
	ui.modTree = openTestDXP3(t)
	ui.modulePath = ""
	t.Setenv("AppData", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	ui.startGraphLinkScan()
	waitForLinkScan(t, ui)
	if ui.graphLinksRunning {
		t.Fatal("the scan says it is still running after it finished")
	}
	scanned := ui.graphFor()

	if len(scanned.links) != len(blocking.links) {
		t.Errorf("the scan read %d references, the blocking read found %d", len(scanned.links), len(blocking.links))
	}
	if len(scanned.BrokenLinks()) != len(blocking.BrokenLinks()) {
		t.Errorf("the scan found %d broken references, the blocking read found %d",
			len(scanned.BrokenLinks()), len(blocking.BrokenLinks()))
	}
	// And the indexes were built, so the views have something to read.
	const unit = `Data\attrib\ebps\races\chaos\structures\chaos_armoury.rgd`
	if len(scanned.Incoming(unit)) != len(blocking.Incoming(unit)) {
		t.Errorf("the scan found %d things referring to the armoury, the blocking read found %d",
			len(scanned.Incoming(unit)), len(blocking.Incoming(unit)))
	}
	if len(scanned.Outgoing(unit)) != len(blocking.Outgoing(unit)) {
		t.Error("the scan and the blocking read disagree about what the armoury refers to")
	}
}

func TestStartingAScanTwiceDoesNotReadTheModTwice(t *testing.T) {
	ui := newUI(unitTestTheme())
	ui.modTree = newGraphTestMod(t)
	g := ui.startGraphLinkScan()
	if again := ui.startGraphLinkScan(); again != g {
		t.Error("a second scan replaced the graph the first one is writing into")
	}
	waitForLinkScan(t, ui)
	if len(g.links) == 0 {
		t.Error("the scan of the fixture found nothing")
	}
}

// waitForLinkScan gives the background scan time to finish, which it needs
// because reading the files is the point of it.
// linkScanWaitLimit is how many polls waitForLinkScan makes before giving up.
//
// The bound is generous on purpose: it is a guard against a scan that never
// finishes, not a measurement, and a full scan of the installed game reads
// 2733 files, which takes about half a minute on a cold cache. Twenty times
// that in wall clock is a hung goroutine rather than a slow disk.
const linkScanWaitLimit = 20000

func waitForLinkScan(t *testing.T, ui *uiState) {
	t.Helper()
	for i := 0; i < linkScanWaitLimit; i++ {
		if !ui.graphLinksRunning && ui.graphLinksCh == nil {
			return
		}
		ui.pollGraphLinks()
		sleepMillis()
	}
	t.Fatal("the background scan did not finish")
}
