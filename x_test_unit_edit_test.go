package main

import (
	"bytes"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// Editing a unit from the object window, checked from both ends: that a change
// reaches the file the mod ships, and that nothing else in the file moved. The
// second half matters more than the first — a unit .rgd is a few hundred
// fields, a save that rewrote it wrongly would still parse, and the mod would
// be broken in a way that only shows up in the game.

// The unit these tests edit. It carries the sections a modder's first changes
// land in and a mix of types, because a panel that only works on floats is not
// an editor.
const (
	unitEditTroop  = `Data\attrib\ebps\races\necrons\troops\necron_lancer.rgd`
	unitEditEngine = `Data\attrib\ebps\races\necrons\troops\necron_reaper.rgd`
)

// unitEditFixture is the unit's file: a float, an integer, a boolean, a string
// and a localisation id, each under the section the game keeps it in.
func unitEditFixture(t *testing.T) []byte {
	t.Helper()
	return mustMarshalGraph(t, &rgd.RgdFile{
		HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
		Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
			rgdTemplateTable("ui_ext", refUIExt,
				rgdTemplateTable("ui_info", "",
					rgdTemplateWString("screen_name_id", "$1000"),
				),
				rgdTemplateString("ui_hotkey_name", "necron_lancer"),
			),
			rgdTemplateTable("health_ext", refHealthExt,
				rgdTemplateFloat("hitpoints", 100),
				rgdTemplateFloat("armour", 5),
				rgdTemplateBool("can_be_repaired", false),
				rgdTemplateFloat("max_repairers", 2),
			),
			rgdTemplateTable("moving_ext", refMovingExt,
				rgdTemplateFloat("speed_max", 3.5),
			),
			rgdTemplateTable("sight_ext", refSightExt,
				rgdTemplateFloat("sight_radius", 20),
			),
			rgdTemplateTable("cost_ext", refCostExt,
				rgdTemplateTable("time_cost", "",
					rgdTemplateTable("cost", "",
						rgdTemplateFloat("requisition", 150),
						rgdTemplateFloat("population", 2),
					),
					rgdTemplateFloat("time_seconds", 25),
				),
			),
			// One modifier in use, which is what puts a counted row in the list:
			// it is a statement about a list rather than a value, and has to be
			// left out of the editor rather than offered as one.
			rgdTemplateTable("modifier_apply_ext", refModifierExt,
				rgdTemplateTable("modifiers", refModifierTable,
					rgdTemplateTable("modifier_01", "",
						rgdTemplateString("target_type_name", "necron_gauntlet"),
					),
				),
			),
		}},
	})
}

// unitEditUI is a mod holding the fixture unit, with the object window pointed
// at it. The unit is selected through the catalog rather than by hand, so the
// test goes through the same path the window does.
func unitEditUI(t *testing.T) (*uiState, *modtree.ModTree) {
	t.Helper()
	tree := newGraphTestMod(t)
	freezeTestMod(t, tree)
	writeTreeFile(t, tree, unitEditTroop, unitEditFixture(t))
	// A second unit that the mod does not own: it comes from the engine's own
	// data, which is where most units a modder edits actually live.
	engineRoot := t.TempDir()
	engineDir := filepath.Join(engineRoot, "Data", "attrib", "ebps", "races", "necrons", "troops")
	if err := os.MkdirAll(engineDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeSourcedFile(t, tree, unitEditEngine, unitEditFixture(t), &modtree.FileSource{
		ModName: "(Engine)", SourceName: "Data", Kind: modtree.KindEngine, DiskRoot: filepath.Join(engineRoot, "Data"),
	})
	tree.SortAll(tree.Root)
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.modulePath = filepath.Join(tree.AppPath, "GraphTestMod")
	selectUnit(t, ui, unitEditTroop)
	return ui, tree
}

// writeSourcedFile is writeTreeFile for a file that belongs to another source
// than the mod, which is what makes it a base game file as far as the app is
// concerned.
func writeSourcedFile(t *testing.T, tree *modtree.ModTree, virtualPath string, data []byte, src *modtree.FileSource) {
	t.Helper()
	rel := strings.TrimPrefix(virtualPath, `Data\`)
	disk := filepath.Join(src.DiskRoot, filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/")))
	if err := os.MkdirAll(filepath.Dir(disk), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, data, 0644); err != nil {
		t.Fatal(err)
	}
	segs := strings.Split(filepath.ToSlash(rel), "/")
	folder := tree.Root
	for _, seg := range segs[:len(segs)-1] {
		folder = tree.GetOrCreateFolder(folder, seg, src)
	}
	name := segs[len(segs)-1]
	f := &modtree.TreeFile{Name: name, FullPath: virtualPath, Source: src, DiskPath: disk, SGAFileID: -1}
	folder.Files = append(folder.Files, f)
	folder.FileByLower[name] = f
}

// selectUnit points the object window at one unit of the catalog, the way the
// pickers do when a row is chosen.
func selectUnit(t *testing.T, ui *uiState, virtualPath string) {
	t.Helper()
	cat := ui.unitCatalogFor()
	for i := range cat.races {
		r := &cat.races[i]
		for j, u := range r.unitsOf() {
			if strings.EqualFold(u.path, virtualPath) {
				ui.unitRaceSel = i
				ui.unitSel = j
				ui.unitDetails = nil
				ui.unitDetailsPath = ""
				return
			}
		}
	}
}

// selectUnitField puts the panel on the row with the given label, which is what
// pressing that row of the info list does.
func selectUnitField(t *testing.T, ui *uiState, label string) *unitField {
	t.Helper()
	d := ui.currentUnitDetails()
	if d == nil {
		t.Fatal("no unit is open in the object window")
	}
	for i := range d.fields {
		if d.fields[i].label != label {
			continue
		}
		ui.unitInfoSel = i
		f := ui.unitInfoFieldSelected()
		if f == nil {
			t.Fatalf("the row %q cannot be edited", label)
		}
		return f
	}
	t.Fatalf("the unit has no row labelled %q", label)
	return nil
}

// TestTheInfoRowsCarryTheFieldTheyCameFrom is the whole basis of editing: a row
// that is only a string cannot be written back, so each one has to point at the
// field it was read from. A counted row is a statement about a list and is
// expected to have none.
func TestTheInfoRowsCarryTheFieldTheyCameFrom(t *testing.T) {
	ui, _ := unitEditUI(t)
	d := ui.currentUnitDetails()
	if d == nil {
		t.Fatal("no unit is open in the object window")
	}
	checked := 0
	for _, row := range d.fields {
		if strings.HasPrefix(row.group, "Lists") {
			if row.entry != nil {
				t.Errorf("the counted row %q claims a field of the file", row.label)
			}
			continue
		}
		if row.isLocID {
			continue
		}
		if row.entry == nil {
			t.Errorf("the row %q has no field behind it, so it cannot be written back", row.label)
			continue
		}
		checked++
		if got := rgdPathOf(d.file.Root, row.entry); got == "" {
			t.Errorf("the field of the row %q is not reachable from the file's root", row.label)
		}
	}
	if checked < 8 {
		t.Errorf("only %d rows were checked, want the whole of the unit's own fields", checked)
	}
}

// TestTypingAValueChangesTheUnitAndSavingWritesItIntoTheMod is the round trip:
// the value the panel shows is the one that was typed, the file on disk says
// the same, and no other field of the unit moved.
func TestTypingAValueChangesTheUnitAndSavingWritesItIntoTheMod(t *testing.T) {
	ui, tree := unitEditUI(t)
	before := rgdLeaves(readRgdAt(t, tree, unitEditTroop).Root)
	f := selectUnitField(t, ui, "Hit points")
	if err := ui.applyUnitFieldText(f, "250"); err != nil {
		t.Fatalf("apply 250: %v", err)
	}
	if !ui.unitInfoDirty {
		t.Fatal("a changed value did not mark the unit as unsaved")
	}
	// The list is rebuilt from the file, so the row has to show what was typed
	// and the selection has to have followed the field rather than the index.
	row := ui.currentUnitDetails().fields[ui.unitInfoSel]
	if row.label != "Hit points" || row.value != "250" {
		t.Errorf("the info list now shows %q = %q, want Hit points = 250", row.label, row.value)
	}
	if _, err := ui.saveUnitDetails(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if ui.unitInfoDirty {
		t.Error("the unit is still marked unsaved after it was written")
	}
	after := rgdLeaves(readRgdAt(t, tree, unitEditTroop).Root)
	if after["health_ext.hitpoints"] != "250" {
		t.Errorf("the file says hitpoints = %q, want 250", after["health_ext.hitpoints"])
	}
	if len(after) != len(before) {
		t.Errorf("the written file has %d fields, the original had %d", len(after), len(before))
	}
	for path, want := range before {
		if path == "health_ext.hitpoints" {
			continue
		}
		got, ok := after[path]
		if !ok {
			t.Errorf("the written file has lost %s", path)
			continue
		}
		if got != want {
			t.Errorf("%s is now %q, was %q", path, got, want)
		}
	}
}

// TestAValueThatWouldNotParseIsRefusedAndNothingIsWritten is the other half of
// the round trip: a number field is a number, and text typed into it has to be
// refused rather than stored as something the game will choke on.
func TestAValueThatWouldNotParseIsRefusedAndNothingIsWritten(t *testing.T) {
	ui, tree := unitEditUI(t)
	disk := tree.FindFile(unitEditTroop).DiskPath
	original, err := os.ReadFile(disk)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"lots", "12hp", ""} {
		f := selectUnitField(t, ui, "Sight range")
		if err := ui.applyUnitFieldText(f, bad); err == nil {
			t.Errorf("%q was accepted for a float field", bad)
		}
	}
	if ui.unitInfoDirty {
		t.Error("a refused value marked the unit as changed")
	}
	if _, err := ui.saveUnitDetails(); err == nil {
		t.Error("a unit with nothing valid to save was written anyway")
	}
	now, err := os.ReadFile(disk)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, now) {
		t.Error("the unit's file changed although every value was refused")
	}
}

// TestABooleanIsChangedThroughTheSameParserAsATypedValue keeps the toggle and
// the text box from disagreeing about what a boolean is: the toggle writes the
// word a person would have typed.
func TestABooleanIsChangedThroughTheSameParserAsATypedValue(t *testing.T) {
	ui, tree := unitEditUI(t)
	f := selectUnitField(t, ui, "Can be repaired")
	if f.entry.Type != rgd.RgdTypeBool {
		t.Fatalf("the row %q is a %s, not a boolean", f.label, f.entry.TypeLabel())
	}
	if err := ui.applyUnitFieldText(f, unitBoolText(f.entry.BoolVal)); err != nil {
		t.Fatalf("flip: %v", err)
	}
	if got := f.entry.DisplayValue(); got != "True" {
		t.Errorf("the field is now %q, want True", got)
	}
	if _, err := ui.saveUnitDetails(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := fieldAt(t, tree, unitEditTroop, "health_ext.can_be_repaired"); got != "True" {
		t.Errorf("the file says can_be_repaired = %q, want True", got)
	}
}

// TestAValueThatIsAlreadyThereIsNotAChange keeps a save from being offered for
// a unit nobody touched: every keystroke that retypes what the field already
// holds would otherwise make the panel claim a change that is not one.
func TestAValueThatIsAlreadyThereIsNotAChange(t *testing.T) {
	ui, _ := unitEditUI(t)
	f := selectUnitField(t, ui, "Speed")
	if err := ui.applyUnitFieldText(f, f.entry.DisplayValue()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if ui.unitInfoDirty {
		t.Error("typing what the field already held marked the unit as changed")
	}
	if _, err := ui.saveUnitDetails(); err == nil {
		t.Error("an unchanged unit was written out anyway")
	}
}

// TestEditingABaseGameUnitWritesTheModsOwnCopyAndLeavesTheOriginalAlone is the
// safety property the whole overlay exists for. The reaper is the engine's file,
// not the mod's, and its installed copy must come out of this untouched.
func TestEditingABaseGameUnitWritesTheModsOwnCopyAndLeavesTheOriginalAlone(t *testing.T) {
	ui, tree := unitEditUI(t)
	engineFile := tree.FindFile(unitEditEngine)
	if engineFile == nil {
		t.Fatal("the engine's unit is not in the tree")
	}
	if engineFile.Source.Kind != modtree.KindEngine {
		t.Fatalf("the fixture's unit comes from %v, so this test is not testing the base game", engineFile.Source.Kind)
	}
	// The path is taken before the save, because saving replaces this entry in
	// the tree with the mod's own copy — which is the point of the test, and
	// would otherwise mean reading the mod's file back and calling it the
	// engine's.
	engineDisk := engineFile.DiskPath
	original, err := os.ReadFile(engineDisk)
	if err != nil {
		t.Fatal(err)
	}
	selectUnit(t, ui, unitEditEngine)
	f := selectUnitField(t, ui, "Hit points")
	if err := ui.applyUnitFieldText(f, "3100"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := ui.saveUnitDetails(); err != nil {
		t.Fatalf("save: %v", err)
	}
	now, err := os.ReadFile(engineDisk)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, now) {
		t.Error("the engine's own file was rewritten")
	}
	// The mod's copy is what the tree now serves, and it is inside the mod.
	after := tree.FindFile(unitEditEngine)
	if after == nil {
		t.Fatal("the unit is no longer in the tree after being edited")
	}
	if after.Source.Kind != modtree.KindThisMod {
		t.Errorf("the edited unit still comes from %v rather than from the mod", after.Source.Kind)
	}
	if !strings.HasPrefix(after.DiskPath, filepath.Join(tree.AppPath, "GraphTestMod")) {
		t.Errorf("the edited unit was written to %q, which is outside the mod", after.DiskPath)
	}
	if got := fieldAt(t, tree, unitEditEngine, "health_ext.hitpoints"); got != "3100" {
		t.Errorf("the mod's copy says hitpoints = %q, want 3100", got)
	}
}

// TestRevertThrowsTheChangeAway is the way back from a value typed wrongly.
func TestRevertThrowsTheChangeAway(t *testing.T) {
	ui, tree := unitEditUI(t)
	f := selectUnitField(t, ui, "Hit points")
	if err := ui.applyUnitFieldText(f, "1"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	ui.revertUnitDetails()
	if ui.unitInfoDirty {
		t.Error("the unit is still marked as changed after being reverted")
	}
	f = selectUnitField(t, ui, "Hit points")
	if f.value != "100" {
		t.Errorf("the row shows %q after the change was thrown away, want the file's own 100", f.value)
	}
	// And the file was never written, so the revert had nothing to undo there.
	if got := fieldAt(t, tree, unitEditTroop, "health_ext.hitpoints"); got != "100" {
		t.Errorf("the file says hitpoints = %q, want 100", got)
	}
}

// TestARowThatIsNotAFieldCannotBeSelectedForEditing keeps the counted rows out
// of the editor: a row that says "4 entries" is a statement, not a value, and
// an editor that accepted it would write a number over a list.
func TestARowThatIsNotAFieldCannotBeSelectedForEditing(t *testing.T) {
	ui, _ := unitEditUI(t)
	d := ui.currentUnitDetails()
	counts := 0
	for i := range d.fields {
		if d.fields[i].entry != nil {
			continue
		}
		counts++
		ui.unitInfoSel = i
		if f := ui.unitInfoFieldSelected(); f != nil {
			t.Errorf("the row %q was offered for editing", d.fields[i].label)
		}
	}
	if counts == 0 {
		t.Fatal("the fixture unit has no counted rows, so this test is not testing anything")
	}
}

// TestThePanelDrawsOverEachKindOfField is the smoke test for the new column: a
// panel that lays out a float, a boolean, a string and a counted row without
// panicking is a view, and one that never draws is not.
func TestThePanelDrawsOverEachKindOfField(t *testing.T) {
	ui, _ := unitEditUI(t)
	d := ui.currentUnitDetails()
	seen := map[uint32]bool{}
	for i := range d.fields {
		f := unitInfoFieldAt(d, i)
		if f == nil {
			continue
		}
		if f.entry != nil {
			seen[f.entry.Type] = true
		}
		ui.unitInfoSel = i
		ui.layoutUnitFieldPanel(unitTestContext(), ui.currentUnitSection())
	}
	for _, typ := range []uint32{rgd.RgdTypeFloat, rgd.RgdTypeBool, rgd.RgdTypeString} {
		if !seen[typ] {
			t.Errorf("no %s row was laid out", (&rgd.RgdEntry{Type: typ}).TypeLabel())
		}
	}
}

// TestAFieldPointedAtANewStringIsWrittenOutWithTheUnit closes the gap between
// the two editors. Giving a field a new localisation id writes the string to
// the .ucs and, until now, left the field that names it in memory only — the
// unit would have shown a name in the app and none at all in the game.
func TestAFieldPointedAtANewStringIsWrittenOutWithTheUnit(t *testing.T) {
	ui, tree := unitEditUI(t)
	d := ui.currentUnitDetails()
	nameField := rgdFindPath(d.file.Root, "ui_ext", "ui_info", "screen_name_id")
	if nameField == nil {
		t.Fatal("the fixture unit has no screen name field")
	}
	nameField.WStrVal = "$1000000123"
	ui.markUnitFieldDirty(nameField)
	if !ui.unitInfoDirty {
		t.Fatal("a field of the unit on screen was changed and the unit was not marked unsaved")
	}
	if _, err := ui.saveUnitDetails(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := fieldAt(t, tree, unitEditTroop, "ui_ext.ui_info.screen_name_id"); got != "$1000000123" {
		t.Errorf("the file says the screen name is %q, want the new id", got)
	}
	// A field of some other file is not the unit's business.
	ui.unitInfoDirty = false
	ui.markUnitFieldDirty(rgdTemplateFloat("somewhere_else", 1))
	if ui.unitInfoDirty {
		t.Error("a field of another file marked this unit as changed")
	}
}

// TestTheAttributeEditorDrawsInAWholeWindow is the smoke test with the whole
// app on screen: the panel is inside a flex column with the pickers, the three
// columns and the properties table, and a layout mistake there would not show
// up in a panel laid out on its own. It also checks that the rows have click
// targets at all — a list that draws and cannot be pressed is a picture of an
// editor.
func TestTheAttributeEditorDrawsInAWholeWindow(t *testing.T) {
	ui, _ := unitEditUI(t)
	ui.openUnitTab()
	ui.unitSection = unitSectionNamed("Unit info")
	d := ui.currentUnitDetails()
	f := newWindowFrame(ui, 1000, 700)
	if got := len(ui.unitInfoClicks); got != len(d.fields) {
		t.Errorf("the info list has %d click targets for %d rows", got, len(d.fields))
	}
	f.frame()
}

// unitTestContext is a context a panel can be laid out into without a window,
// which is all a smoke test needs: the panel has to build its layout, not draw
// it.
func unitTestContext() layout.Context {
	return layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(400, 600)),
		Ops:         new(op.Ops),
	}
}

// rgdLeaves is every field of a file as its path and its value, which is how a
// test says that one value changed and the rest of the file did not.
func rgdLeaves(root *rgd.RgdEntry) map[string]string {
	out := map[string]string{}
	var walk func(prefix string, e *rgd.RgdEntry)
	walk = func(prefix string, e *rgd.RgdEntry) {
		if e == nil {
			return
		}
		if e.IsTable() {
			for _, ch := range e.Children {
				walk(prefix+"."+ch.Name, ch)
			}
			return
		}
		out[strings.TrimPrefix(prefix, ".")] = e.DisplayValue()
	}
	walk("", root)
	return out
}
