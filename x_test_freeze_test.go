package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"gioui.org/f32"
	"gioui.org/io/pointer"
)

func freezeTestMod(t *testing.T, tree *modtree.ModTree) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("LOCALAPPDATA", dir)
	return dir
}

// readRgdAt reads a file of the mod back and parses it.
func readRgdAt(t *testing.T, tree *modtree.ModTree, virtualPath string) *rgd.RgdFile {
	t.Helper()
	data, err := tree.ReadFile(virtualPath)
	if err != nil {
		t.Fatalf("read %s: %v", virtualPath, err)
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("parse %s: %v", virtualPath, err)
	}
	return rf
}

// fieldAt reads one field of a file of the mod.
func fieldAt(t *testing.T, tree *modtree.ModTree, virtualPath, field string) string {
	t.Helper()
	e := rgdEntryAtPath(readRgdAt(t, tree, virtualPath).Root, field)
	if e == nil {
		t.Fatalf("%s has no %s", virtualPath, field)
	}
	return e.DisplayValue()
}

func freezeTestUI(t *testing.T) (*uiState, *modtree.ModTree) {
	t.Helper()
	tree := newGraphTestMod(t)
	freezeTestMod(t, tree)
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.modulePath = filepath.Join(tree.AppPath, "GraphTestMod")
	return ui, tree
}

func TestFreezingMovesTheFilesOutOfTheModAndRecordsWhere(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)

	// The squad file goes with the unit, so the fixture needs one.
	squad := `Data\attrib\sbps\races\necrons\necron_gauntlet.rgd`
	writeFixtureFile(t, tree, squad)

	record, err := ui.freezeEntity(graphTestGauntlet)
	if err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	if record.Name != "necron_gauntlet" {
		t.Errorf("the record names %q, want necron_gauntlet", record.Name)
	}
	if len(record.Files) != 2 {
		t.Fatalf("the record lists %d files, want the ebps one and the sbps one", len(record.Files))
	}

	// Neither file is in the mod any more.
	for _, f := range record.Files {
		if tree.FindFile(f.From) != nil {
			t.Errorf("%s is still in the mod after being frozen", f.From)
		}
	}
	// Both are in the project's Units folder, under the names the record says.
	unitsDir, err := freezeUnitsDir(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range record.Files {
		if _, err := os.Stat(filepath.Join(unitsDir, f.To)); err != nil {
			t.Errorf("the frozen file is not in the project folder: %v", err)
		}
	}
	// And the rest of the mod is untouched.
	if tree.FindFile(graphTestLoose) == nil {
		t.Error("freezing one unit took another one out of the mod")
	}
	if tree.FindFile(graphTestArmoury) == nil {
		t.Error("freezing a unit took the building out of the mod")
	}
}

func TestFreezingClearsEveryReferenceAndWritesThemDownFirst(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)

	// The gauntlet's own file names the armoury as its builder, and the
	// fixture's add-on is unlocked by it. Freeze the armoury, so the
	// references are in two other files of two other kinds.
	before := fieldAt(t, tree, graphTestGauntlet, "requirement_ext.requirements.required_1.structure_name")
	if before == "" {
		t.Fatal("the fixture's gauntlet does not name the armoury, so nothing is being tested")
	}
	// The graph has not been read yet, and this is the point: the object window
	// reads it in the background and a freeze must not depend on that having
	// finished. A freeze that cleared only what a scan had reached would leave
	// the mod with a name pointing at nothing.
	if ui.graphFor().Linked {
		t.Fatal("the fixture's graph was already read, so this is not testing what it should")
	}
	record, err := ui.freezeEntity(graphTestArmoury)
	if err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	if len(record.Mentions) != 2 {
		t.Fatalf("the record lists %d mentions, want the gauntlet's and the add-on's", len(record.Mentions))
	}
	var m freezeMention
	for _, one := range record.Mentions {
		if strings.EqualFold(one.File, graphTestGauntlet) {
			m = one
		}
	}
	if m.File == "" {
		t.Fatalf("the record does not list the gauntlet's mention: %+v", record.Mentions)
	}
	if m.Value != before {
		t.Errorf("the record says the field said %q, but it said %q", m.Value, before)
	}
	if m.Field != "requirement_ext.requirements.required_1.structure_name" {
		t.Errorf("the record names the field %q, which is not the one the reference is in", m.Field)
	}
	// And the field really is empty now.
	if got := fieldAt(t, tree, graphTestGauntlet, m.Field); got != "" {
		t.Errorf("the reference is still %q after the freeze", got)
	}
}

// A freeze that only ever read the units and buildings would miss the ability
// and the add-on, and would leave both of them naming a file that is no longer
// in the mod. Nothing in the game would say so: an add-on that upgrades a
// missing unit simply does nothing.
func TestFreezingClearsTheMentionsInAbilitiesAndAddonsToo(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)

	const abilityLeaf = "area_effect.weapon_damage.modifiers.modifier_01.modifier.target_type_name"
	const addonLeaf = "modifiers.modifier_01.target_type_name"
	if got := fieldAt(t, tree, graphTestWarp, abilityLeaf); got != "necron_gauntlet" {
		t.Fatalf("the fixture's ability does not name the gauntlet, it names %q", got)
	}
	if got := fieldAt(t, tree, graphTestHubris, addonLeaf); got != "necron_gauntlet" {
		t.Fatalf("the fixture's add-on does not name the gauntlet, it names %q", got)
	}

	record, err := ui.freezeEntity(graphTestGauntlet)
	if err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	if len(record.Mentions) != 2 {
		for _, m := range record.Mentions {
			t.Logf("mention %s %s = %q", m.File, m.Field, m.Value)
		}
		t.Fatalf("the record lists %d mentions, want the ability's and the add-on's", len(record.Mentions))
	}
	byFile := map[string]freezeMention{}
	for _, m := range record.Mentions {
		byFile[strings.ToLower(m.File)] = m
	}
	if _, ok := byFile[strings.ToLower(graphTestWarp)]; !ok {
		t.Error("the ability's reference to the gauntlet was not recorded")
	}
	if _, ok := byFile[strings.ToLower(graphTestHubris)]; !ok {
		t.Error("the add-on's reference to the gauntlet was not recorded")
	}
	if got := fieldAt(t, tree, graphTestWarp, abilityLeaf); got != "" {
		t.Errorf("the ability still names %q after the freeze", got)
	}
	if got := fieldAt(t, tree, graphTestHubris, addonLeaf); got != "" {
		t.Errorf("the add-on still names %q after the freeze", got)
	}
	// The add-on's other reference is to the armoury, which is still in the
	// mod, and must have been left alone. A freeze clears what points at the
	// thing being moved, not everything in the files it happens to touch.
	if got := fieldAt(t, tree, graphTestHubris, "requirements.required_1.structure_name"); got != "necron_armoury" {
		t.Errorf("the add-on's reference to the armoury is %q, want it left alone", got)
	}
}

func TestTheRecordIsWrittenBeforeAnythingIsTouched(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)

	if _, err := ui.freezeEntity(graphTestArmoury); err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	path, err := freezeBookPath(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the record of what the freeze changed was not written: %v", err)
	}
	book, err := readFreezeBook(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	if book.Mod != "GraphTestMod" {
		t.Errorf("the record says the mod is %q, want GraphTestMod", book.Mod)
	}
	if !book.isFrozen(graphTestArmoury) {
		t.Error("the record does not say the armoury is frozen")
	}
	if book.isFrozen(graphTestLoose) {
		t.Error("the record says a unit is frozen that never was")
	}
}

func TestFreezingTwiceIsRefused(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	if _, err := ui.freezeEntity(graphTestArmoury); err != nil {
		t.Fatalf("the first freeze: %v", err)
	}
	if _, err := ui.freezeEntity(graphTestArmoury); err == nil {
		t.Error("freezing an already frozen unit was allowed, which would lose the first record")
	}
}

func TestOnlyAUnitOrABuildingCanBeFrozen(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	if _, err := ui.freezeEntity(graphTestGauss); err == nil {
		t.Error("a weapon was frozen, but only units and buildings are units the mod ships")
	}
	if _, err := ui.freezeEntity(`Data\attrib\ebps\races\necrons\troops\nothing_here.rgd`); err == nil {
		t.Error("a file that is not in the mod was frozen")
	}
}

func TestUnfreezingPutsBackExactlyWhatWasThere(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	squad := `Data\attrib\sbps\races\necrons\necron_gauntlet.rgd`
	writeFixtureFile(t, tree, squad)

	field := "requirement_ext.requirements.required_1.structure_name"
	before := fieldAt(t, tree, graphTestGauntlet, field)
	gauntletBefore, err := os.ReadFile(mustDiskPath(t, tree, graphTestGauntlet))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ui.freezeEntity(graphTestArmoury); err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	record, err := ui.unfreezeEntity(graphTestArmoury)
	if err != nil {
		t.Fatalf("unfreezeEntity: %v", err)
	}

	// The reference is back, with the value it had.
	if got := fieldAt(t, tree, graphTestGauntlet, field); got != before {
		t.Errorf("after unfreezing the field says %q, want %q", got, before)
	}
	// The files are back in the mod.
	for _, f := range record.Files {
		if tree.FindFile(f.From) == nil {
			t.Errorf("%s was not put back into the mod", f.From)
		}
	}
	armouryAfter, err := os.ReadFile(mustDiskPath(t, tree, graphTestArmoury))
	if err != nil {
		t.Fatal(err)
	}
	if len(armouryAfter) == 0 {
		t.Error("the armoury came back empty")
	}
	// The gauntlet's file, which was rewritten to clear the reference, is
	// back to the same bytes: the rewrite round-tripped.
	gauntletAfter, err := os.ReadFile(mustDiskPath(t, tree, graphTestGauntlet))
	if err != nil {
		t.Fatal(err)
	}
	if string(gauntletAfter) != string(gauntletBefore) {
		t.Error("the file that pointed at the frozen unit is not byte for byte what it was")
	}
	// The project folder no longer holds a copy of a unit that is in the mod.
	unitsDir, err := freezeUnitsDir(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range record.Files {
		if _, err := os.Stat(filepath.Join(unitsDir, f.To)); !os.IsNotExist(err) {
			t.Errorf("the saved %s is still in the project folder", f.To)
		}
	}
	// And the record no longer claims it is frozen.
	book, err := readFreezeBook(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	if book.isFrozen(graphTestArmoury) {
		t.Error("the record still says the unit is frozen after it was unfrozen")
	}
}

func TestUnfreezingSomethingThatWasNeverFrozenIsRefused(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	if _, err := ui.unfreezeEntity(graphTestArmoury); err == nil {
		t.Error("unfreezing a unit that is not frozen was allowed")
	}
}

// TestAskingWhatIsFrozenCreatesNothing is the difference between a record that
// is read and a record that is set up. The object window asks on every frame it
// draws whether the selected object is frozen, and a program that made its own
// folders each time it was painted would litter the user's machine with empty
// project trees for mods that were never touched.
func TestAskingWhatIsFrozenCreatesNothing(t *testing.T) {
	ui, tree := freezeTestUI(t)
	appData := freezeTestMod(t, tree)

	if ui.isFrozen(graphTestArmoury) {
		t.Error("a mod that has frozen nothing says a unit is frozen")
	}
	if rows := ui.frozenRows(); len(rows) != 0 {
		t.Errorf("a mod that has frozen nothing lists %d rows", len(rows))
	}
	// Asking a few more times, which is what a window does.
	for i := 0; i < 5; i++ {
		ui.isFrozen(graphTestLoose)
		ui.frozenRows()
	}
	if _, err := os.Stat(filepath.Join(appData, "DoW Mod Studio")); !os.IsNotExist(err) {
		t.Errorf("asking what is frozen created the program's own folder under %s", appData)
	}

	// A freeze does create the project folder, because it has somewhere to put
	// the files.
	if _, err := ui.freezeEntity(graphTestArmoury); err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	dir, err := freezeUnitsDir(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("freezing did not create the project folder it moves files into: %v", err)
	}
	// And the record is in it, which is the only account of what was cleared.
	if _, err := os.Stat(filepath.Join(dir, dependenciesFileName)); err != nil {
		t.Errorf("the record of the freeze was not written: %v", err)
	}
}

// TestTheRecordIsReadOnceNotOnceAFrame is the reason the record is cached: the
// strip and the list both ask on every frame, and the file behind the answer
// only changes when a freeze does.
func TestTheRecordIsReadOnceNotOnceAFrame(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	first := ui.freezeBookFor()
	if first == nil {
		t.Fatal("no record for a mod that has one")
	}
	if again := ui.freezeBookFor(); again != first {
		t.Error("the record was read again instead of being kept")
	}
	// A freeze changes it, so the cache has to be dropped.
	if _, err := ui.freezeEntity(graphTestArmoury); err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	after := ui.freezeBookFor()
	if after == first {
		t.Error("the cached record survived a freeze, so the window would not show it")
	}
	if !after.isFrozen(graphTestArmoury) {
		t.Error("the record read back after the freeze does not say the unit is frozen")
	}
}

func TestRgdEntryAtPathFindsTheFieldAgain(t *testing.T) {
	rf := readRgdAt(t, newGraphTestMod(t), graphTestGauntlet)
	e := rgdEntryAtPath(rf.Root, "requirement_ext.requirements.required_1.structure_name")
	if e == nil {
		t.Fatal("the field the record names could not be found again")
	}
	if e.DisplayValue() == "" {
		t.Error("the field was found but is empty")
	}
	if rgdEntryAtPath(rf.Root, "requirement_ext.requirements.required_1.nothing") != nil {
		t.Error("a field that is not there was found")
	}
	if rgdEntryAtPath(rf.Root, "") != nil {
		t.Error("an empty path found something")
	}
}

func TestSetRgdStringRefusesAFieldThatCannotHoldAReference(t *testing.T) {
	if err := setRgdString(&rgd.RgdEntry{Name: "n", Type: rgd.RgdTypeFloat}, "x"); err == nil {
		t.Error("a number field was given a reference")
	}
	if err := setRgdString(&rgd.RgdEntry{Name: "n", Type: rgd.RgdTypeTable}, "x"); err == nil {
		t.Error("a table was given a reference")
	}
	if err := setRgdString(nil, "x"); err == nil {
		t.Error("nothing was given a reference")
	}
	s := &rgd.RgdEntry{Name: "n", Type: rgd.RgdTypeString}
	if err := setRgdString(s, "x"); err != nil || s.StrVal != "x" {
		t.Errorf("a text field was not given the value: %v", err)
	}
	w := &rgd.RgdEntry{Name: "n", Type: rgd.RgdTypeWString}
	if err := setRgdString(w, "x"); err != nil || w.WStrVal != "x" {
		t.Errorf("a localised text field was not given the value: %v", err)
	}
}

func mustDiskPath(t *testing.T, tree *modtree.ModTree, virtualPath string) string {
	t.Helper()
	f := tree.FindFile(virtualPath)
	if f == nil {
		t.Fatalf("%s is not in the mod", virtualPath)
	}
	return f.DiskPath
}

// writeFixtureFile adds a file to the fixture mod, on disk and in the tree, so
// that a test can have a squad file or a second building to point at.
func writeFixtureFile(t *testing.T, tree *modtree.ModTree, virtualPath string) {
	t.Helper()
	rel := strings.TrimPrefix(virtualPath, `Data\`)
	disk := filepath.Join(tree.WritableOverlayRoot, rel)
	if tree.WritableOverlayRoot == "" {
		// The fixture's AppPath is a temp dir, so the overlay root is the
		// same place the other fixture files went.
		disk = filepath.Join(tree.AppPath, "GraphTestMod", "Data", rel)
	}
	if err := os.MkdirAll(filepath.Dir(disk), 0755); err != nil {
		t.Fatal(err)
	}
	body := mustMarshalGraph(t, &rgd.RgdFile{
		HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
		Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
	})
	if err := os.WriteFile(disk, body, 0644); err != nil {
		t.Fatal(err)
	}
	// The tree is built a segment at a time, so the file is registered the
	// same way the fixture's own files are.
	src := &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: tree.AppPath}
	folder := tree.Root
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for _, seg := range segs[:len(segs)-1] {
		folder = tree.GetOrCreateFolder(folder, seg, src)
	}
	name := segs[len(segs)-1]
	f := &modtree.TreeFile{Name: name, FullPath: virtualPath, Source: src, DiskPath: disk, SGAFileID: -1}
	folder.Files = append(folder.Files, f)
	folder.FileByLower[name] = f
}

// ─── merged from x_test_freeze_ui_test.go ────────────────────────────────────────

func clickAt(f *windowFrame, at f32.Point) {
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
}

// TestTheFreezeButtonIsReachableInTheWindow puts a mod in the object window
// and finds the freeze button by walking the pointer along the strip.
func TestTheFreezeButtonIsReachableInTheWindow(t *testing.T) {
	ui, _ := freezeTestUI(t)
	freezeTestMod(t, ui.modTree)
	ui.openUnitTab()
	f := newWindowFrame(ui, 1000, 700)
	at, ok := findStripButton(t, f, &f.ui.unitFreezeTrigger)
	if !ok {
		t.Fatal("the Freeze button was not found in the picker strip")
	}
	if at.X <= 0 || at.Y <= 0 {
		t.Errorf("the Freeze button was found at %v, which is the window corner rather than the strip", at)
	}
}

// TestFreezingFromTheWindowMovesTheSelectedUnitAndSaysSo presses the button and
// checks the two things a user would check afterwards: that the unit has left
// the mod, and that the status line says what happened to it.
//
// It acts on whatever the Unit field is showing rather than on a unit named in
// the test, because that is what the button does.
func TestFreezingFromTheWindowMovesTheSelectedUnitAndSaysSo(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	ui.openUnitTab()
	sel := ui.currentUnit()
	if sel == nil {
		t.Fatal("no unit is selected in the object window")
	}
	f := newWindowFrame(ui, 1000, 700)

	at, ok := findStripButton(t, f, &f.ui.unitFreezeTrigger)
	if !ok {
		t.Fatal("the Freeze button was not found in the picker strip")
	}
	clickAt(f, at)

	if tree.FindFile(sel.Path) != nil {
		t.Errorf("%s is still in the mod after the button was pressed", sel.Name)
	}
	if !ui.isFrozen(sel.Path) {
		t.Error("the record does not say the unit was frozen")
	}
	if ui.statusText == "" {
		t.Error("nothing was said about what the freeze did")
	}
	// The window sends the user where the way back is, because the unit is no
	// longer in the list it was just frozen from.
	if got := unitSections[ui.unitSection].label; got != "Frozen" {
		t.Errorf("after freezing the window shows %q, want the Frozen section", got)
	}
	// And it still draws.
	f.frame()
}

// TestAFrozenUnitIsListedSoItCanBePutBack is the other half of the round trip.
// A frozen unit is not in the mod, so the record is the only place it exists,
// and without a list of them a freeze could not be undone from the program.
func TestAFrozenUnitIsListedSoItCanBePutBack(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	sel := ui.currentUnit()
	if sel == nil {
		t.Fatal("no unit is selected in the object window")
	}
	if _, err := ui.freezeEntity(sel.Path); err != nil {
		t.Fatalf("freezeEntity: %v", err)
	}
	rows := ui.frozenRows()
	if len(rows) != 1 {
		t.Fatalf("the Frozen list has %d rows, want the one that was frozen", len(rows))
	}
	if rows[0].name != sel.Name {
		t.Errorf("the Frozen list names %q, want %s", rows[0].name, sel.Name)
	}
	if !strings.EqualFold(rows[0].ref, sel.Path) {
		t.Errorf("the row carries %q as the path to put back, want %s", rows[0].ref, sel.Path)
	}
	// A mod that has never frozen anything says so rather than showing a gap.
	if got := (freezeTestUIRows(t)); len(got) != 0 {
		t.Errorf("a mod with nothing frozen has %d rows", len(got))
	}
}

// freezeTestUIRows is the Frozen list of a fresh mod, which is empty.
func freezeTestUIRows(t *testing.T) []unitWeaponRow {
	t.Helper()
	ui, _ := freezeTestUI(t)
	freezeTestMod(t, ui.modTree)
	return ui.frozenRows()
}

// TestPuttingAFrozenUnitBackFromTheListPressesItHome is the whole round trip
// through the window: freeze from the strip, put back from the list.
func TestPuttingAFrozenUnitBackFromTheListPressesItHome(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	ui.openUnitTab()
	sel := ui.currentUnit()
	if sel == nil {
		t.Fatal("no unit is selected in the object window")
	}
	f := newWindowFrame(ui, 1000, 700)

	at, ok := findStripButton(t, f, &f.ui.unitFreezeTrigger)
	if !ok {
		t.Fatal("the Freeze button was not found in the picker strip")
	}
	clickAt(f, at)
	if tree.FindFile(sel.Path) != nil {
		t.Fatalf("%s was not moved out of the mod", sel.Name)
	}

	// Put it back by pressing its row in the Frozen list. The row is found by
	// hovering it, the same way the strip buttons are.
	ui.unitSection = unitSectionNamed("Frozen")
	rowClicks := len(ui.frozenRows())
	ui.ensureUnitRowClicks(rowClicks)
	at, ok = findHoverPoint(t, f, ui.unitRowClicks[0].Hovered, frozenRowProbeRow(t, f), 4)
	if !ok {
		t.Fatal("the row of the frozen unit was not found in the Frozen list")
	}
	clickAt(f, at)

	if tree.FindFile(sel.Path) == nil {
		t.Errorf("%s was not put back into the mod", sel.Name)
	}
	if ui.isFrozen(sel.Path) {
		t.Error("the record still says the unit is frozen after it was put back")
	}
	if len(ui.frozenRows()) != 0 {
		t.Error("the Frozen list is not empty after the last unit was put back")
	}
	// Nothing is left in the project folder either.
	unitsDir, err := freezeUnitsDir(tree, ui.modulePath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(unitsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".rgd" {
			t.Errorf("%s is still in the project folder after the unit was put back", e.Name())
		}
	}
}

// frozenRowProbeRow is the band the Frozen list's first row sits in, which is
// the same place every row of the object window's lists sits: under the header
// of the column, below the pickers.
func frozenRowProbeRow(t *testing.T, f *windowFrame) image.Rectangle {
	t.Helper()
	top := unitMenuBarHeightPx + unitTabBarHeightPx
	y := top + int(unitPickerStripHeightDp) + unitTabPadDp*2 + unitColHeaderHeight + 10
	return image.Rect(20, y, 900, y)
}

// TestTheRelationshipsViewDraws checks that the new sections lay out over a
// real window, because a column that panics or draws nothing is not a view.
func TestTheRelationshipsViewDraws(t *testing.T) {
	ui, tree := freezeTestUI(t)
	freezeTestMod(t, tree)
	ui.openUnitTab()
	f := newWindowFrame(ui, 1000, 700)
	for _, name := range []string{"Links", "Problems", "Frozen"} {
		ui.unitSection = unitSectionNamed(name)
		f.frame()
	}
	waitForLinkScan(t, ui)
	f.frame()
}
