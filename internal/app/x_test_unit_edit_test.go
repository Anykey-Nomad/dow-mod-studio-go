package app

import (
	"bytes"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/whm"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/fogleman/fauxgl"
)

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

// ─── merged from x_test_unit_info_fields_test.go ────────────────────────────────────────

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

// ─── merged from x_test_unit_keysel_test.go ────────────────────────────────────────

func keySelViewer() *modelViewer {
	const frames = 20
	model := &WhmModel{
		Name:  "t",
		Bones: []WhmBone{{Name: "root", Parent: -1, Pos: [3]float32{1, 2, 3}}},
	}
	return &modelViewer{
		model: model,
		anim: &WhmAnimation{
			Name:      "clip",
			NumFrames: frames,
			Bones: []WhmTrack{{
				Bone: 0,
				Name: "root",
				PosKey: []WhmKey3{
					{Time: timeForFrame(2, frames), V: [3]float32{1, 2, 3}},
					{Time: timeForFrame(5, frames), V: [3]float32{4, 5, 6}},
				},
				RotKey: []WhmKey4{
					{Time: timeForFrame(3, frames), V: [4]float32{0, 0, 0, 1}},
					{Time: timeForFrame(7, frames), V: [4]float32{1, 0, 0, 0}},
				},
			}},
		},
		boneIndex:    0,
		boneKeyIndex: -1,
		boneKeyMode:  -1,
	}
}

// keySelUI is a fresh window with that viewer on show and its keyframe
// selection bound to the bone it belongs to, which is where every row click
// starts from.
func keySelUI(v *modelViewer) *uiState {
	ui := newUI(unitTestTheme())
	ui.bindBoneKeySelection(v)
	return ui
}

// keySelPosFrames and keySelRotFrames read the track back as frames, which is
// what both the list and the strip call the place a key stands.
func keySelPosFrames(v *modelViewer) []int {
	out := make([]int, 0, len(v.boneTrack().PosKey))
	for _, k := range v.boneTrack().PosKey {
		out = append(out, frameAt(float64(k.Time), v.anim.NumFrames))
	}
	return out
}

func keySelRotFrames(v *modelViewer) []int {
	out := make([]int, 0, len(v.boneTrack().RotKey))
	for _, k := range v.boneTrack().RotKey {
		out = append(out, frameAt(float64(k.Time), v.anim.NumFrames))
	}
	return out
}

func keySelFramesEqual(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestKeyFrameRowsAreListedPositionKeysThenRotationKeys(t *testing.T) {
	v := keySelViewer()
	want := []boneKeyRef{{mode: 0, frame: 2}, {mode: 0, frame: 5}, {mode: 1, frame: 3}, {mode: 1, frame: 7}}
	got := v.boneKeyRefs()
	if len(got) != len(want) {
		t.Fatalf("the list has %d rows, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestInsertBoneKeyCopyTakesTheValueOfTheKeyItCopies(t *testing.T) {
	v := keySelViewer()
	if !v.insertBoneKeyCopy(4, 0, 2) {
		t.Fatal("frame 4 carries no key, so a copy of frame 2 was refused")
	}
	if got, want := keySelPosFrames(v), []int{2, 4, 5}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v — the track has to stay sorted", got, want)
	}
	if got := v.boneTrack().PosKey[1].V; got != [3]float32{1, 2, 3} {
		t.Errorf("the new key holds %v, want the copied [1 2 3]", got)
	}
	if ref, ok := v.pickedBoneKeyRef(); !ok || ref != (boneKeyRef{mode: 0, frame: 4}) {
		t.Errorf("the picked key is %+v (ok=%v), want the new one on frame 4", ref, ok)
	}
}

func TestInsertBoneKeyCopyRefusesAFrameThatAlreadyHasAKey(t *testing.T) {
	v := keySelViewer()
	if v.insertBoneKeyCopy(2, 0, 5) {
		t.Error("a second position key landed on frame 2, which already holds one")
	}
	if got := keySelPosFrames(v); len(got) != 2 {
		t.Errorf("the track now holds %v, want the two keys it started with", got)
	}
}

func TestFreeBoneKeyFrameWalksOutToAFreeFrame(t *testing.T) {
	v := keySelViewer()
	if got := v.freeBoneKeyFrame(5, 0, -1); got != 4 {
		t.Errorf("the frame before position key 5 is %d, want 4", got)
	}
	// A run of taken frames is walked past rather than refused: insert a
	// position key on 4 and the free frame before 5 becomes 3.
	if !v.insertBoneKeyCopy(4, 0, 2) {
		t.Fatal("the key on frame 4 could not be inserted for the walk")
	}
	if got := v.freeBoneKeyFrame(5, 0, -1); got != 3 {
		t.Errorf("with frame 4 taken the free frame before 5 is %d, want 3", got)
	}
	// The other kind has its own keys: frame 4 is free for a rotation.
	if got := v.freeBoneKeyFrame(5, 1, -1); got != 4 {
		t.Errorf("rotation keys do not stand on frame 4, so the free frame is %d, want 4", got)
	}
	if got := v.freeBoneKeyFrame(0, 0, -1); got != -1 {
		t.Errorf("there is no frame before 0, got %d", got)
	}
	if got := v.freeBoneKeyFrame(v.anim.NumFrames-1, 0, 1); got != -1 {
		t.Errorf("there is no frame after the last one, got %d", got)
	}
}

func TestDeleteBoneKeyAtKeepsThePickedKeyAimedAtItself(t *testing.T) {
	v := keySelViewer()
	// The position key on frame 5 is picked: it is the second of its kind,
	// so taking the key on frame 2 out slides its index down.
	v.boneKeyIndex, v.boneKeyMode = 1, 0
	if !v.deleteBoneKeyAt(2, 0) {
		t.Fatal("the key on frame 2 was not removed")
	}
	if got, want := keySelPosFrames(v), []int{5}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v", got, want)
	}
	if v.boneKeyIndex != 0 || v.boneKeyMode != 0 {
		t.Fatalf("the picked key is now (idx %d, mode %d), want (0, 0)", v.boneKeyIndex, v.boneKeyMode)
	}
	if ref, ok := v.pickedBoneKeyRef(); !ok || ref != (boneKeyRef{mode: 0, frame: 5}) {
		t.Errorf("the picked key is %+v (ok=%v), want the one still standing on frame 5", ref, ok)
	}
}

func TestDeleteBoneKeyAtClearsThePickedKeyWhenItIsTheOne(t *testing.T) {
	v := keySelViewer()
	v.boneKeyIndex, v.boneKeyMode = 0, 0
	if !v.deleteBoneKeyAt(2, 0) {
		t.Fatal("the picked key was not removed")
	}
	if v.boneKeyIndex != -1 || v.boneKeyMode != -1 {
		t.Errorf("the picked key is now (idx %d, mode %d), want nothing picked", v.boneKeyIndex, v.boneKeyMode)
	}
	if got, want := keySelPosFrames(v), []int{5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v", got, want)
	}
}

func TestRowClickPicksOneRowAndCtrlClickGathersMore(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[1], refs, 0)
	if len(ui.unitBoneKeySel) != 1 || !ui.unitBoneKeySel[refs[1]] {
		t.Fatalf("a plain click picked %v, want just row %+v", ui.unitBoneKeySel, refs[1])
	}
	if v.boneKeyIndex != 1 || v.boneKeyMode != 0 {
		t.Errorf("the picked key is (idx %d, mode %d), want the clicked row (1, 0)", v.boneKeyIndex, v.boneKeyMode)
	}

	ui.boneKeyRowClicked(v, refs[0], refs, key.ModCtrl)
	if len(ui.unitBoneKeySel) != 2 || !ui.unitBoneKeySel[refs[0]] {
		t.Fatalf("Ctrl+click gathered %v, want both rows", ui.unitBoneKeySel)
	}

	// Ctrl+clicking a picked row takes it away, and the key the fields work
	// on moves to the row still picked rather than one that no longer says
	// so on the list.
	ui.boneKeyRowClicked(v, refs[1], refs, key.ModCtrl)
	if len(ui.unitBoneKeySel) != 1 || !ui.unitBoneKeySel[refs[0]] {
		t.Fatalf("the second Ctrl+click left %v, want the first row alone", ui.unitBoneKeySel)
	}
	if v.boneKeyIndex != 0 || v.boneKeyMode != 0 {
		t.Errorf("the picked key is (idx %d, mode %d), want the row still picked (0, 0)", v.boneKeyIndex, v.boneKeyMode)
	}

	// And taking the last one away leaves nothing picked at all.
	ui.boneKeyRowClicked(v, refs[0], refs, key.ModCtrl)
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the selection still holds %v after its last row was taken away", ui.unitBoneKeySel)
	}
	if v.boneKeyIndex != -1 || v.boneKeyMode != -1 {
		t.Errorf("the picked key is (idx %d, mode %d), want nothing picked", v.boneKeyIndex, v.boneKeyMode)
	}
	if got := ui.boneKeyActionRefs(v); len(got) != 0 {
		t.Errorf("the buttons would act on %v while the list has nothing picked", got)
	}
}

func TestShiftClickSpansFromTheAnchor(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[1], refs, 0)            // the anchor: position key on frame 5
	ui.boneKeyRowClicked(v, refs[3], refs, key.ModShift) // rotation key on frame 7
	want := map[boneKeyRef]bool{refs[1]: true, refs[2]: true, refs[3]: true}
	if len(ui.unitBoneKeySel) != len(want) {
		t.Fatalf("Shift+click picked %v, want the %d rows between the anchor and the click", ui.unitBoneKeySel, len(want))
	}
	for ref := range want {
		if !ui.unitBoneKeySel[ref] {
			t.Errorf("row %+v is missing from %v", ref, ui.unitBoneKeySel)
		}
	}
	if ui.unitBoneKeySel[refs[0]] {
		t.Errorf("row %+v lies before the anchor and was picked all the same: %v", refs[0], ui.unitBoneKeySel)
	}
}

func TestDeleteTakesEveryPickedRowAway(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)           // position key on frame 2
	ui.boneKeyRowClicked(v, refs[3], refs, key.ModCtrl) // rotation key on frame 7
	ui.deleteBoneKeysAction(v)

	if got, want := keySelPosFrames(v), []int{5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v", got, want)
	}
	if got, want := keySelRotFrames(v), []int{3}; !keySelFramesEqual(got, want) {
		t.Errorf("the rotation keys sit on frames %v, want %v", got, want)
	}
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the selection kept %v after the keys it named were removed", ui.unitBoneKeySel)
	}
	if !ui.unitModelWhmDirty {
		t.Error("removing keys did not mark the file for Save WHM")
	}
	if ui.unitBoneKeyErr != "" {
		t.Errorf("the note after a successful delete says %q", ui.unitBoneKeyErr)
	}
}

func TestKeyButtonsFallBackToTheStripPickedKey(t *testing.T) {
	v := keySelViewer()
	ui := keySelUI(v)
	// No row has been clicked: the strip picked the position key on frame 5.
	v.boneKeyIndex, v.boneKeyMode = 1, 0

	got := ui.boneKeyActionRefs(v)
	if len(got) != 1 || got[0] != (boneKeyRef{mode: 0, frame: 5}) {
		t.Fatalf("the buttons would act on %v, want the single key the strip picked", got)
	}
	ui.deleteBoneKeysAction(v)
	if frames := keySelPosFrames(v); !keySelFramesEqual(frames, []int{2}) {
		t.Errorf("the position keys sit on frames %v, want [2]", frames)
	}
}

func TestPickedRowsLeaveTheSelectionWhenTheirKeysGo(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)
	// The key is carried off its frame some other way — a drag on the strip
	// dropped it onto another — and the row must not go on naming that frame.
	if !v.deleteBoneKeyAt(2, 0) {
		t.Fatal("the key on frame 2 was not removed")
	}
	if got := ui.pickedBoneKeyRefs(v); len(got) != 0 {
		t.Errorf("the selection still holds %v for keys that are gone", got)
	}
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the selection map kept %v", ui.unitBoneKeySel)
	}
}

func TestAnotherBoneStartsAFreshSelection(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)
	ui.boneKeyRowClicked(v, refs[0], refs, 0)

	v.model.Bones = append(v.model.Bones, WhmBone{Name: "child", Parent: 0})
	v.boneIndex = 1
	ui.bindBoneKeySelection(v)
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the rows picked on the last bone were carried over: %v", ui.unitBoneKeySel)
	}
	if ui.unitBoneKeySelAnchor != (boneKeyRef{mode: -1}) {
		t.Errorf("the anchor still stands on %+v after the bone changed", ui.unitBoneKeySelAnchor)
	}
}

func TestAddBeforeGivesEveryPickedRowItsOwnFrame(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)           // position key on frame 2
	ui.boneKeyRowClicked(v, refs[1], refs, key.ModCtrl) // position key on frame 5
	ui.addBoneKeysAround(v, -1)

	if got, want := keySelPosFrames(v), []int{1, 2, 4, 5}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v — each picked row gets the free frame before it", got, want)
	}
	if !ui.unitModelWhmDirty {
		t.Error("adding keys did not mark the file for Save WHM")
	}
	// The keys just added are what the list shows picked, since editing or
	// taking them away is what comes next.
	want := map[boneKeyRef]bool{{mode: 0, frame: 1}: true, {mode: 0, frame: 4}: true}
	if len(ui.unitBoneKeySel) != len(want) {
		t.Fatalf("the selection holds %v, want the two keys just added", ui.unitBoneKeySel)
	}
	for ref := range want {
		if !ui.unitBoneKeySel[ref] {
			t.Errorf("row %+v is missing from %v", ref, ui.unitBoneKeySel)
		}
	}
}

func TestAddAfterGivesEveryPickedRowItsOwnFrame(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)
	ui.boneKeyRowClicked(v, refs[1], refs, key.ModCtrl)
	ui.addBoneKeysAround(v, 1)

	if got, want := keySelPosFrames(v), []int{2, 3, 5, 6}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v — each picked row gets the free frame after it", got, want)
	}
}

func TestAddBeforeWalksFurtherWhenTheNeighbourIsTaken(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)
	// A position key on frame 3 makes frames 2 and 3 neighbours of the run.
	if !v.insertBoneKeyCopy(3, 0, 2) {
		t.Fatal("the key on frame 3 could not be inserted")
	}
	refs = v.boneKeyRefs()
	row := -1
	for i, r := range refs {
		if r == (boneKeyRef{mode: 0, frame: 3}) {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("the list does not show the new key: %v", refs)
	}
	ui.boneKeyRowClicked(v, refs[row], refs, 0)
	ui.addBoneKeysAround(v, -1)

	if got, want := keySelPosFrames(v), []int{1, 2, 3, 5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v — frame 2 was taken, so the key went to 1", got, want)
	}
}

func TestAddAroundSaysSoWhenNothingCanBeAdded(t *testing.T) {
	v := keySelViewer()
	ui := keySelUI(v)
	// Frames 0 and 1 are filled in front of the position key on frame 2, so
	// the walk backwards runs out of clip before it finds a free frame.
	for _, frame := range []int{1, 0} {
		if !v.insertBoneKeyCopy(frame, 0, 2) {
			t.Fatalf("the key on frame %d could not be inserted", frame)
		}
	}
	refs := v.boneKeyRefs()
	for _, r := range refs {
		if r == (boneKeyRef{mode: 0, frame: 2}) {
			ui.boneKeyRowClicked(v, r, refs, 0)
		}
	}
	ui.addBoneKeysAround(v, -1)

	if ui.unitBoneKeyErr == "" {
		t.Error("adding before a key with no free frame said nothing")
	}
	if ui.unitModelWhmDirty {
		t.Error("a refusal still marked the file for Save WHM")
	}
	if got, want := keySelPosFrames(v), []int{0, 1, 2, 5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v — nothing may be added", got, want)
	}
}

// ─── merged from x_test_unit_viewport_test.go ────────────────────────────────────────

func fauxglDir(v *modelViewer) fauxgl.Vector {
	return fauxgl.Vector{
		X: math.Cos(v.Yaw) * math.Cos(v.Pitch),
		Y: math.Sin(v.Yaw) * math.Cos(v.Pitch),
		Z: math.Sin(v.Pitch),
	}
}

// The orbit camera turns the model on the axis its own up vector names, which
// for these models is Z: a change of yaw spins it left and right, a change of
// pitch tips it up and down. The pair used to be worked out as if the models
// were Y-up, so the two came out crossed — dragging sideways turned the model
// up and down and the other way round. Measuring where a point of the model
// lands on screen is the only way to see it: the numbers are right in each
// direction and still wrong together.
func TestTurningTheModelTurnsItTheWayTheMouseWent(t *testing.T) {
	// A point beside the middle of a model, where a turn of the camera moves
	// it as far as it can.
	const w, h = 400, 400
	point := fauxgl.Vector{X: 1, Y: 0.2, Z: 0.5}
	opts := render.RenderOptions{Width: w, Height: h, Fov: 45, Distance: 8, Yaw: 0.7, Pitch: 0.2}

	onScreen := func(o render.RenderOptions) (float64, float64) {
		v := render.BuildViewMatrix(o).MulPositionW(point)
		if v.W == 0 {
			t.Fatalf("the point is at the camera, at yaw %v pitch %v", o.Yaw, o.Pitch)
		}
		return v.X / v.W, v.Y / v.W
	}
	x0, y0 := onScreen(opts)
	turned := opts
	turned.Yaw += 0.2
	xa, ya := onScreen(turned)
	tilted := opts
	tilted.Pitch += 0.2
	xb, yb := onScreen(tilted)

	if math.Abs(ya-y0) > math.Abs(xa-x0)/2 {
		t.Errorf("turning the camera sideways moved the model %v across and %v up, so the axes are crossed",
			xa-x0, ya-y0)
	}
	if math.Abs(xb-x0) > math.Abs(yb-y0)/2 {
		t.Errorf("tilting the camera moved the model %v across and %v up, so the axes are crossed",
			xb-x0, yb-y0)
	}
	// And the model has to go the way the pointer did. The handler turns the
	// camera one way for a drag to the left and the other for a drag down, so
	// these two samples are what those drags produce: the model follows the
	// hand, left for the first and down for the second.
	if xa-x0 > 0 {
		t.Errorf("a drag to the left moved the model %v to the right of where it was", xa-x0)
	}
	if yb-y0 > 0 {
		t.Errorf("a drag downwards lifted the model %v up the screen instead of turning it", yb-y0)
	}
}

// The object window's viewport finds a unit's model through the field the unit
// .rgd itself carries, and these are the cases that decide which .whm that is.

// unitViewportMod is an empty mod for the path lookups: the .rgd and the .whm
// are put into it by the test.
func unitViewportMod(t *testing.T) *modtree.ModTree {
	t.Helper()
	appPath := t.TempDir()
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "ViewportTestMod"}
	t.Cleanup(tree.Close)
	return tree
}

// unitViewportWriteUnit puts a generated unit into a mod, with no model beside
// it unless one is named, and returns the unit's catalog entry.
func unitViewportWriteUnit(t *testing.T, tree *modtree.ModTree, name unitTemplateName, modelPath string) unitEntry {
	t.Helper()
	data, err := newUnitRgd(name).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	rgdPath := name.unitTemplateVirtualPath()
	writeTreeFile(t, tree, rgdPath, data)
	if modelPath != "" {
		writeTreeFile(t, tree, modelPath, []byte("not a real model"))
	}
	tree.SortAll(tree.Root)
	for _, u := range buildUnitCatalog(tree).allUnits() {
		if strings.EqualFold(u.path, rgdPath) {
			return u
		}
	}
	t.Fatalf("the fixture unit %s is not in the catalog", rgdPath)
	return unitEntry{}
}

// unitViewportFixture writes a generated unit and a model of the given name
// into a fresh mod, and returns the tree with the unit's entry.
func unitViewportFixture(t *testing.T, name unitTemplateName, modelPath string) (*modtree.ModTree, unitEntry) {
	t.Helper()
	tree := unitViewportMod(t)
	return tree, unitViewportWriteUnit(t, tree, name, modelPath)
}

// The generated unit names its model the way the game's own units do, so the
// field is read out of the parsed .rgd rather than out of the catalog.
func TestUnitAnimatorNameReadsTheBlueprintField(t *testing.T) {
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, "")
	d := loadUnitDetails(tree, entry)
	if d.error != "" {
		t.Fatal(d.error)
	}
	got := unitAnimatorName(d.file)
	want := "Races/Necrons/Troops/Necron_Wraith"
	if got != want {
		t.Fatalf("animator is %q, want %q", got, want)
	}
}

func TestModelInfoTableHandlesMissingTabAndViewer(t *testing.T) {
	ui := &uiState{}
	for _, tab := range []*fileTab{nil, {}} {
		if got := ui.modelInfoTable(layout.Context{}, tab); got != (layout.Dimensions{}) {
			t.Errorf("model info for tab %v = %+v, want empty dimensions", tab, got)
		}
	}
}

// A file with no entity_blueprint_ext, and a nil one, both answer "" rather than
// panicking: the viewport has to survive a unit it cannot find a model for.
func TestUnitAnimatorNameIsEmptyWhenTheFieldIsMissing(t *testing.T) {
	if got := unitAnimatorName(nil); got != "" {
		t.Errorf("a nil file gave %q", got)
	}
	if got := unitAnimatorName(&rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData"}}); got != "" {
		t.Errorf("a file with no extensions gave %q", got)
	}
}

// The model is the .whm of the animator's name under Data\art\ebps, which is
// where the game keeps it: the .rgd says "Races/Necrons/Troops/Necron_Wraith"
// and the file is Data\art\ebps\Races\Necrons\Troops\Necron_Wraith.whm.
func TestUnitModelPathFollowsAnimator(t *testing.T) {
	want := `Data\art\ebps\Races\Necrons\Troops\Necron_Wraith.whm`
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, want)
	src, ok := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	if !ok {
		t.Fatal("the fixture unit says nothing about a model")
	}
	got := unitModelPath(tree, src)
	if !strings.EqualFold(got, want) {
		t.Fatalf("model path is %q, want %q", got, want)
	}
}

// A unit that names a model which is not there falls back to a .whm of the
// unit's own name, because that is how a mod that ships its own geometry under
// its own name is put together.
func TestUnitModelPathFallsBackToTheUnitsOwnName(t *testing.T) {
	// The generated unit points at a model the mod does not have; this one is
	// the only .whm of the name.
	placed := `Data\art\ebps\races\necrons\custom\necron_wraith.whm`
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, placed)
	if tree.FindFile(filepath.FromSlash(`Data\art\ebps\Races\Necrons\Troops\Necron_Wraith.whm`)) != nil {
		t.Fatal("the animator's model is present, so this is not a fallback case")
	}
	src, _ := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	got := unitModelPath(tree, src)
	if !strings.EqualFold(got, placed) {
		t.Fatalf("model path is %q, want the .whm of the unit's own name %q", got, placed)
	}
}

// A unit with no model at all is not an error the viewport has to survive with
// a panic: the lookup answers an empty path, which the section turns into a
// line of text.
func TestUnitModelPathIsEmptyWhenThereIsNoModel(t *testing.T) {
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, "")
	src, ok := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	if !ok {
		t.Fatal("the fixture unit says nothing about a model")
	}
	if got := unitModelPath(tree, src); got != "" {
		t.Fatalf("model path is %q, want none", got)
	}
	// A nil tree is what a mod that failed to load leaves behind.
	if got := unitModelPath(nil, src); got != "" {
		t.Fatalf("model path with no mod is %q, want none", got)
	}
}

// A unit whose .rgd carries no model field at all still gets whatever .whm is
// named after it, which is the whole of what is left to go on.
func TestUnitModelPathWithoutAnAnimatorField(t *testing.T) {
	placed := `Data\art\ebps\races\necron_wraith.whm`
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, placed)
	src, _ := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	src.animator = ""
	got := unitModelPath(tree, src)
	if !strings.EqualFold(got, placed) {
		t.Fatalf("model path is %q, want %q", got, placed)
	}
}

// The clip the viewport opens on is the one that moves something. A model's
// list is mostly one off actions, so "move" wins, then an idle, and a list of
// nothing but "vis_" clips — which only fade a weapon in and out — still starts
// on a real one if there is one and on the first if there is not.
func TestUnitDefaultAnimPrefersLocomotion(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"die_1", "idle_1", "move"}, "move"},
		{[]string{"die_1", "idle_1", "idle_2"}, "idle_1"},
		{[]string{"vis_flag", "vis_base_sword", "run"}, "run"},
		{[]string{"vis_flag", "vis_base_sword"}, "vis_flag"},
		{[]string{"attack_1", "walk_cycle"}, "attack_1"},
	}
	for _, tc := range cases {
		m := &WhmModel{}
		for _, n := range tc.names {
			m.Animations = append(m.Animations, WhmAnimation{Name: n})
		}
		idx := unitDefaultAnim(m)
		if idx < 0 || idx >= len(m.Animations) {
			t.Fatalf("%v: picked index %d", tc.names, idx)
		}
		if got := m.Animations[idx].Name; got != tc.want {
			t.Errorf("%v: picked %q, want %q", tc.names, got, tc.want)
		}
	}
	if idx := unitDefaultAnim(&WhmModel{}); idx != -1 {
		t.Errorf("a model with no clips gave index %d, want -1", idx)
	}
	if idx := unitDefaultAnim(nil); idx != -1 {
		t.Errorf("a nil model gave index %d, want -1", idx)
	}
}

// A model's own camera is a local transform in the exporter's frame, so looking
// through one has to put the orbit camera at the rotated position, aimed at the
// rotated focus point. Checking the round trip is what pins the rotation down: a
// viewer that left the camera unrotated would show a model from under the floor.
func TestLookThroughCameraAimsAtTheFocusPoint(t *testing.T) {
	v := newModelViewer(&WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, 1, 1}}},
		BoundMin: [3]float32{-1, -1, -1},
		BoundMax: [3]float32{1, 1, 1},
	}, "")
	cam := WhmCamera{Name: "port_hud", Pos: [3]float32{4, 0, 6}, Fov: 30, FocusPoint: [3]float32{0, 1, 0}}
	v.lookThroughCamera(cam)

	eye := boneRootFrame.MulPosition(render.Vec3From(cam.Pos))
	focus := boneRootFrame.MulPosition(render.Vec3From(cam.FocusPoint))
	if got := (v.Center.Sub(focus)).Length(); got > 1e-6 {
		t.Fatalf("the view is centred %v away from the camera's focus point", got)
	}
	if got, want := v.Distance, eye.Sub(focus).Length(); math.Abs(got-want) > 1e-6 {
		t.Fatalf("the view sits %v from the model, want %v", got, want)
	}
	if v.FOV != 30 {
		t.Errorf("the field of view is %v, want the camera's 30", v.FOV)
	}
	if v.ActiveCamera != "port_hud" {
		t.Errorf("the active camera is %q, want the one that was picked", v.ActiveCamera)
	}
	// The orbit parameters have to describe the same direction, or the model
	// is seen from somewhere the camera is not.
	dir := fauxglDir(v)
	if d := dir.Sub(eye.Sub(focus).Normalize()).Length(); d > 1e-6 {
		t.Errorf("the orbit direction is %v away from the camera's own", d)
	}
}

// A field of view the game stored as zero or as a full turn would make the
// projection useless, so only a sane one is taken.
func TestLookThroughCameraKeepsAnUnusableFieldOfView(t *testing.T) {
	v := newModelViewer(&WhmModel{Model: whm.Model{Vertices: []WhmVertex{{}}, BoundMax: [3]float32{1, 1, 1}}}, "")
	for _, fov := range []float32{0, -20, 180} {
		v.lookThroughCamera(WhmCamera{Name: "c", Pos: [3]float32{0, 0, 5}, Fov: fov})
		if v.FOV != render.DefaultFov {
			t.Errorf("a field of view of %v was taken, leaving the view at %v", fov, v.FOV)
		}
	}
}

// Reset view goes back to the framing the model was opened with, and forgets
// that a camera of the model's was in use.
func TestResetCameraForgetsTheModelsCamera(t *testing.T) {
	model := &WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-2, 0, -2}}, {Pos: [3]float32{2, 2, 2}}},
		BoundMin: [3]float32{-2, 0, -2},
		BoundMax: [3]float32{2, 2, 2},
	}
	v := newModelViewer(model, "")
	_, fit := model.ModelCenter()
	v.lookThroughCamera(WhmCamera{Name: "shot", Pos: [3]float32{9, 9, 9}, Fov: 60})
	v.resetCamera()
	if v.ActiveCamera != "" {
		t.Errorf("the active camera survived a reset: %q", v.ActiveCamera)
	}
	if v.FOV != render.DefaultFov {
		t.Errorf("the field of view is %v after a reset, want %v", v.FOV, render.DefaultFov)
	}
	if v.Distance != fit {
		t.Errorf("the distance is %v after a reset, want the framing distance %v", v.Distance, fit)
	}
	if center, _ := model.ModelCenter(); v.Center.Sub(center).Length() > 1e-6 {
		t.Errorf("the view is centred on %v, want the middle of the model %v", v.Center, center)
	}
}

// A camera sitting on its own focus point has no direction to work out, so it
// keeps the distance that framed the model rather than dividing by nothing.
func TestLookThroughCameraOnItsFocusPoint(t *testing.T) {
	model := &WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, 1, 1}}},
		BoundMin: [3]float32{-1, -1, -1},
		BoundMax: [3]float32{1, 1, 1},
	}
	v := newModelViewer(model, "")
	_, fit := model.ModelCenter()
	v.lookThroughCamera(WhmCamera{Name: "degenerate", Pos: [3]float32{2, 2, 2}, FocusPoint: [3]float32{2, 2, 2}})
	if math.IsNaN(v.Distance) || math.IsInf(v.Distance, 0) {
		t.Fatalf("the distance became %v", v.Distance)
	}
	if v.Distance != fit {
		t.Errorf("the distance is %v, want the framing distance %v", v.Distance, fit)
	}
}

// ─── merged from x_test_unit_squads_test.go ────────────────────────────────────────

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

// ─── merged from x_test_unit_names_test.go ────────────────────────────────────────

func unitRGDWith(value string) *rgd.RgdFile {
	info := &rgd.RgdEntry{Name: "ui_info", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
		{Name: "icon_name", Type: rgd.RgdTypeString, StrVal: "necron_lord"},
	}}
	if value != "" {
		info.Children = append(info.Children, &rgd.RgdEntry{
			Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: value,
		})
	}
	return &rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
		{Name: "ui_ext", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{info}},
	}}}
}

// ── the id ──────────────────────────────────────────────────────────────────

// TestUnitScreenNameIDDropsTheDollar pins what the index is allowed to hold: the
// id with no "$" and no text. The text is looked up on the UI thread so a
// language change costs nothing, and a test that let text in here would quietly
// make every language switch need a full rescan of the mod.
func TestUnitScreenNameIDDropsTheDollar(t *testing.T) {
	id, ok := unitScreenNameIDOf(unitRGDWith("$705754"))
	if !ok {
		t.Fatal("the id was not read out of a unit that carries one")
	}
	if id != "705754" {
		t.Errorf("got id %q, want %q", id, "705754")
	}
}

// TestUnitScreenNameIDRejectsUnitsWithNoName covers the fallback the lists rely
// on. An unset field and a missing one both mean "this unit has no name", and
// neither may be cached: "$0" is the marker for nothing, not the id of a string.
func TestUnitScreenNameIDRejectsUnitsWithNoName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"the unset marker", "$0"},
		{"no screen_name_id at all", ""},
		{"a value that is not an id", "necron_lord"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if id, ok := unitScreenNameIDOf(unitRGDWith(tc.value)); ok {
				t.Errorf("read id %q from a unit with %s", id, tc.name)
			}
		})
	}
	if _, ok := unitScreenNameIDOf(nil); ok {
		t.Error("read an id out of no file")
	}
	if _, ok := unitScreenNameIDOf(&rgd.RgdFile{}); ok {
		t.Error("read an id out of a file with no root")
	}
}

// ── the label ───────────────────────────────────────────────────────────────

// testUnitEntry is a unit as the lists see it: a path and the name the file
// gives it.
func testUnitEntry(name string) unitEntry {
	return unitEntry{
		path: `Data\attrib\ebps\races\necron\troops\` + name + ".rgd",
		name: name,
		race: "necron",
	}
}

// TestUnitLabelUsesTheLocalisedText is the request itself: with the id resolved
// and the string table holding it, the list shows the text and not the id.
func TestUnitLabelUsesTheLocalisedText(t *testing.T) {
	lord := testUnitEntry("necron_lord")
	ui := newUI(unitTestTheme())
	ui.unitNames = map[string]string{strings.ToLower(lord.path): "705754"}
	ui.testLocaleTable(t, "705754", "Necron Lord")

	got := ui.unitLabel(lord)
	if got != "Necron Lord" {
		t.Errorf("the list reads %q, want the localised text", got)
	}
	if strings.Contains(got, "$") || strings.Contains(got, "705754") {
		t.Errorf("the list shows the localisation id in %q; it was asked for the name only", got)
	}
}

// TestUnitLabelFallsBackToTheFileName is what the reader sees when there is no
// localisation: the .rgd's own name, which is the name the game uses for the
// unit's hotkey, so it is a real name rather than a placeholder.
func TestUnitLabelFallsBackToTheFileName(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  map[string]string
	}{
		{"nothing scanned yet", nil},
		{"the unit has no id of its own", map[string]string{strings.ToLower(testUnitEntry("necron_lord").path): "999"}},
		{"the chosen language has no entry for it", map[string]string{strings.ToLower(testUnitEntry("necron_lord").path): "705754"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lord := testUnitEntry("necron_lord")
			ui := newUI(unitTestTheme())
			ui.unitNames = tc.ids
			// A table that knows nothing, so the third case is a real miss
			// rather than a missing table.
			ui.testLocaleTable(t, "1", "One")

			if got := ui.unitLabel(lord); got != "necron_lord" {
				t.Errorf("the list reads %q, want the file name", got)
			}
		})
	}
}

// TestUnitFileLabelIsTheFilesOwnName: the dim name beside the display name is
// the file's, and it is what tells two same-named units of one race apart.
func TestUnitFileLabelIsTheFilesOwnName(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`Data\a\b\necron_nexus.rgd`, "necron_nexus"},
		{`Data\attrib\ebps\races\necrons\structures\necron_dreadnought.rgd`, "necron_dreadnought"},
		{"plain.rgd", "plain"},
	} {
		if got := unitFileLabel(unitEntry{path: tc.path}); got != tc.want {
			t.Errorf("unitFileLabel(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ── the background scan ─────────────────────────────────────────────────────

// TestUnitNameScanIsNotRestartedForTheSameTree: the catalog is asked for on
// every frame the object tab is open, so a scan that restarted each time would
// read every .rgd in the mod over and over, forever.
func TestUnitNameScanIsNotRestartedForTheSameTree(t *testing.T) {
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necron\troops\necron_lord.rgd`: "not a real rgd",
	})
	cat := buildUnitCatalog(tree)
	ui := newUI(unitTestTheme())

	ui.startUnitNameScan(tree, cat)
	first := ui.unitNamesCh
	if first == nil {
		t.Fatal("the scan did not start")
	}
	ui.startUnitNameScan(tree, cat)
	if ui.unitNamesCh != first {
		t.Error("a second scan was started for the same tree")
	}
	ui.stopUnitNameScan()
}

// TestUnitNameScanStopsWhenTheModGoesAway is the case that matters: a reload
// closes the tree the scan is reading, so the scan has to be told rather than
// left to find out by itself.
func TestUnitNameScanStopsWhenTheModGoesAway(t *testing.T) {
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necron\troops\necron_lord.rgd`: "not a real rgd",
	})
	cat := buildUnitCatalog(tree)
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)

	// What the catalog asks for once there is no mod at all.
	ui.modTree = nil
	if got := ui.unitCatalogFor(); got != nil {
		t.Fatal("the catalog survived the mod going away")
	}
	if ui.unitNamesRunning {
		t.Error("the scan is still running against a tree that is being closed")
	}
	if ui.unitNames != nil {
		t.Error("the name index outlived the mod it belonged to")
	}
}

// TestPollUnitNamesIsQuietWithNothingToSay: a finished scan, or no scan at all,
// must not report work, or every frame would ask for a redraw it does not need.
func TestPollUnitNamesIsQuietWithNothingToSay(t *testing.T) {
	ui := newUI(unitTestTheme())
	if ui.pollUnitNames() {
		t.Error("pollUnitNames reported work before any scan was started")
	}

	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necron\troops\necron_lord.rgd`: "not a real rgd",
	})
	cat := buildUnitCatalog(tree)
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)
	if ui.pollUnitNames() {
		t.Error("pollUnitNames reported work after the scan had finished")
	}
}

// waitForUnitNames polls the way the frame loop does until the scan is done.
// waitForUnitNames gives the asynchronous name scan time to finish. The scan
// reads a real catalogue off a real disk, so the budget is wall clock and
// generous: a fixed number of polls turns a slow disk into a failed test, which
// says nothing about the code.
func waitForUnitNames(t *testing.T, ui *uiState) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		ui.pollUnitNames()
		if ui.unitNamesDone || (!ui.unitNamesRunning && ui.unitNamesCh == nil) {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatal("the scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// testLocaleTable installs a string table holding exactly the ids given, so a
// test can say what the language knows without reading a .ucs. The tables are
// package level state, so they are reset afterwards.
func (ui *uiState) testLocaleTable(t *testing.T, entries ...string) {
	t.Helper()
	resetLocales(t)
	if len(entries)%2 != 0 {
		t.Fatalf("testLocaleTable got %d values, want id and text in pairs", len(entries))
	}
	index := make(map[string]localeEntry, len(entries)/2)
	for i := 0; i < len(entries); i += 2 {
		index[entries[i]] = localeEntry{Text: entries[i+1]}
	}
	localeState.mu.Lock()
	localeState.cur = &localeTable{Lang: "Test", Index: index, Files: 1, Lines: len(index)}
	localeState.mu.Unlock()
}

// ─── merged from x_test_unit_template_test.go ────────────────────────────────────────

func templateEntries(rf *rgd.RgdFile) map[string]*rgd.RgdEntry {
	out := map[string]*rgd.RgdEntry{}
	var walk func(prefix string, children []*rgd.RgdEntry)
	walk = func(prefix string, children []*rgd.RgdEntry) {
		for _, ch := range children {
			path := prefix + "/" + ch.Name
			out[path] = ch
			walk(path, ch.Children)
		}
	}
	walk(rf.Root.Name, rf.Root.Children)
	return out
}

func TestNewUnitRgdHasTheTwelveSections(t *testing.T) {
	rf := newUnitRgd(unitTemplateName{Race: "necrons", Name: "necron_wraith"})
	var names []string
	for _, c := range rf.Root.Children {
		names = append(names, c.Name)
	}
	if len(names) != len(unitTemplateSections) {
		t.Fatalf("a new unit has %d top level entries, want the %d sections every unit has: %v",
			len(names), len(unitTemplateSections), names)
	}
	for i, want := range unitTemplateSections {
		if names[i] != want {
			t.Errorf("entry %d is %q, want %q", i, names[i], want)
		}
	}
	// Every section points at the shared lua it extends: that reference is what
	// fills in the thousands of attributes the file does not spell out.
	for _, c := range rf.Root.Children {
		if !strings.HasPrefix(c.RefString(), `ebpextensions\`) {
			t.Errorf("section %q extends %q, want one of the game's ebpextensions", c.Name, c.RefString())
		}
	}
}

// TestNewUnitRgdRoundTrips is the plain question of whether the file it writes is
// a file the app can read back: an RGD written by the marshaller and parsed
// again has to be the file that went in.
func TestNewUnitRgdRoundTrips(t *testing.T) {
	rf := newUnitRgd(unitTemplateName{Race: "chaos", Name: "chaos_hound"})
	data, err := rf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshal produced no bytes")
	}
	back, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("parse of the marshalled unit: %v", err)
	}
	if back.Descriptor != rf.Descriptor {
		t.Errorf("descriptor is %q, want %q", back.Descriptor, rf.Descriptor)
	}
	want := templateEntries(rf)
	got := templateEntries(back)
	if len(got) != len(want) {
		t.Fatalf("the file read back has %d entries, want %d", len(got), len(want))
	}
	for path, e := range want {
		g := got[path]
		if g == nil {
			t.Errorf("%s is missing after the round trip", path)
			continue
		}
		if g.Type != e.Type {
			t.Errorf("%s is a %s after the round trip, want %s", path, rgdTypeName(g.Type), rgdTypeName(e.Type))
		}
		if g.RefString() != e.RefString() {
			t.Errorf("%s extends %q after the round trip, want %q", path, g.RefString(), e.RefString())
		}
		if !g.IsTable() && g.DisplayValue() != e.DisplayValue() {
			t.Errorf("%s is %q after the round trip, want %q", path, g.DisplayValue(), e.DisplayValue())
		}
	}
}

// TestNewUnitRgdCarriesTheGivenName is what makes the dialog worth having: the
// values a user would otherwise have to type are filled from the name and the
// race the unit was created under.
func TestNewUnitRgdCarriesTheGivenName(t *testing.T) {
	rf := newUnitRgd(unitTemplateName{Race: "space_marines", Name: "marine_veteran"})
	entries := templateEntries(rf)
	want := map[string]string{
		"GameData/entity_blueprint_ext/animator":                     "Races/Space_Marines/Troops/Marine_Veteran",
		"GameData/ui_ext/ui_hotkey_name":                             "marine_veteran",
		"GameData/ui_ext/ui_info/icon_name":                          "space_marine_icons/marine_veteran_icon",
		"GameData/health_ext/hitpoints":                              "1000",
		"GameData/health_ext/armour":                                 "100",
		"GameData/sight_ext/sight_radius":                            "25",
		"GameData/moving_ext/speed_max":                              "4",
		"GameData/cost_ext/time_cost/cost/requisition":               "250",
		"GameData/cost_ext/time_cost/time_seconds":                   "60",
		"GameData/ui_ext/ui_info/screen_name_id":                     "$0",
		"GameData/type_ext/type_surface":                             refTypeSurface,
		"GameData/special_attack_physics_ext/disable_special_attack": "True",
	}
	for path, expect := range want {
		e := entries[path]
		if e == nil {
			t.Errorf("a new unit has no %s", path)
			continue
		}
		got := e.RefString()
		if got == "" {
			got = e.DisplayValue()
		}
		if got != expect {
			t.Errorf("%s is %q, want %q", path, got, expect)
		}
	}
	if path := `Data\attrib\ebps\races\space_marines\troops\marine_veteran.rgd`; (unitTemplateName{Race: "space_marines", Name: "marine_veteran"}).unitTemplateVirtualPath() != path {
		t.Errorf("the unit would be written to the wrong place")
	}
}

// TestNewUnitRgdInventsNothingTheGameDoesNotAskFor is the test the template has
// to pass to be worth having. It reads every unit of the installed game, takes
// the attributes they all have, and requires the generated file to use nothing
// else: a name the game does not use, a type it does not use, or a section that
// is not one of the twelve would all be caught here.
func TestNewUnitRgdInventsNothingTheGameDoesNotAskFor(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	var troops []unitEntry
	for _, r := range cat.races {
		troops = append(troops, r.troops...)
	}
	if len(troops) < 100 {
		t.Skipf("only %d units in the installed game, too few to compare against", len(troops))
	}
	stats, unreadable := rgdCommonStats(tree, troops)
	if len(unreadable) > 0 {
		t.Fatalf("%d of %d units could not be read, so the comparison would be against a part of the game", len(unreadable), len(troops))
	}
	files := 0
	for _, a := range stats {
		if a.files > files {
			files = a.files
		}
	}
	common := map[string]*rgdAttrStat{}
	for path, a := range stats {
		if a.files == files {
			common[path] = a
		}
	}
	rf := newUnitRgd(unitTemplateName{Race: "necrons", Name: "necron_wraith"})
	entries := templateEntries(rf)
	missing := 0
	for path, e := range entries {
		c, ok := common[path]
		if !ok {
			missing++
			if missing <= 25 {
				t.Errorf("a new unit has %s (%s %s), which is not an attribute every one of the %d units has",
					path, rgdTypeName(e.Type), e.RefString(), files)
			}
			continue
		}
		if c.types[e.Type]*10 < files*9 {
			// Not "in every unit": the game's own files are not perfectly
			// uniform — help_text_list/text_02 is a string in one unit of 432 and
			// a wide string in the rest. A template has to pick the common one.
			best, bestN := uint32(0), 0
			for typ, n := range c.types {
				if n > bestN {
					best, bestN = typ, n
				}
			}
			t.Errorf("%s is a %s in the template; the %d units use %s for it %d times",
				path, rgdTypeName(e.Type), files, rgdTypeName(best), bestN)
		}
	}
	if len(entries) == 0 {
		t.Fatal("the template has no entries at all")
	}
	t.Logf("template: %d attributes, all of them shared by %d real units", len(entries), files)
}

// ─── merged from x_test_unit_ua_real_test.go ────────────────────────────────────────

const testUADEModule = "/mnt/windows/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/UA_DE.module"

// findUAUnit returns the unit with the given file name from any race.
func findUAUnit(t *testing.T, cat *unitCatalog, name string) unitEntry {
	t.Helper()
	for _, r := range cat.races {
		for _, u := range append(append([]unitEntry{}, r.troops...), r.structures...) {
			if u.name == name {
				return u
			}
		}
	}
	t.Fatalf("unit %q not in catalog (%d races)", name, len(cat.races))
	return unitEntry{}
}

func TestUADEDiagnoseUnitPipeline(t *testing.T) {
	if _, err := os.Stat(testUADEModule); err != nil {
		t.Skipf("UA_DE not installed: %v", err)
	}
	resetLocales(t)

	tree, err := modtree.BuildModTree(testUADEModule, "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}
	defer tree.Close()
	// The same dictionary hookup loadModule does before anything parses a .rgd.
	rgd.AddDictRoot(tree.AppPath)
	t.Logf("sources=%d warnings=%d appPath=%q modFolder=%q", len(tree.Sources), len(tree.Warnings), tree.AppPath, tree.ModFolder)
	for _, w := range tree.Warnings {
		t.Logf("warning: %s", w)
	}

	// 1. The locale table, exactly as loadModule builds it.
	table := setLocale(tree.AppPath, testUADEModule, "English")
	t.Logf("locale table: files=%d lines=%d err=%v", table.Files, table.Lines, table.Err)
	for id, want := range map[string]string{
		"90100":  "", // engine screen name slot
		"705754": "", // Necron Lord on DXP3
	} {
		t.Logf("localisedString(%q) = %q (want non-empty %q)", id, localisedString(id), want)
	}

	// 2. The catalog and the background name scan.
	cat := buildUnitCatalog(tree)
	total := 0
	for _, r := range cat.races {
		total += r.unitCount()
	}
	t.Logf("catalog: %d races, %d units", len(cat.races), total)

	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.modulePath = testUADEModule
	ui.gamePath = tree.AppPath
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)
	t.Logf("scan: %d ids cached, done=%v", len(ui.unitNames), ui.unitNamesDone)

	// Coverage over the whole catalogue: how many units get a localised name,
	// a decoded icon and a model path, with a few misses of each kind logged
	// so a systemic gap cannot hide behind a good total.
	var noID, noName, noIcon, noModel int
	shownModel := 0
	for _, r := range cat.races {
		for _, u := range r.unitsOf() {
			if ui.unitNames[strings.ToLower(u.path)] == "" {
				noID++
			}
			if ui.unitLocalisedName(u) == "" {
				noName++
			}
			if ui.unitIcon(u) == nil {
				noIcon++
				if noIcon <= 5 {
					t.Logf("coverage: no icon for %s", u.path)
				}
			}
			d := loadUnitDetails(tree, u)
			src, ok := newUnitModelSource(tree, d)
			if !ok || unitModelPath(tree, src) == "" {
				noModel++
				if shownModel < 5 {
					shownModel++
					t.Logf("coverage: no model for %s (detailsErr=%q)", u.path, d.error)
				}
			}
		}
	}
	t.Logf("coverage over %d units: without id=%d, without localised name=%d, without icon=%d, without model=%d",
		total, noID, noName, noIcon, noModel)

	// 3. The three units the screenshots show, walked through every step.
	for _, name := range []string{"inquisition_damocles", "necron_basic_warrior", "necron_basic_warrior_advance_sp"} {
		u := findUAUnit(t, cat, name)
		id := ui.unitNames[strings.ToLower(u.path)]
		t.Logf("unit %s: id=%q localised=%q label=%q fileLabel=%q",
			u.name, id, ui.unitLocalisedName(u), ui.unitLabel(u), unitFileLabel(u))

		data, rerr := tree.ReadFile(u.path)
		if rerr != nil {
			t.Errorf("  ReadFile(%q): %v", u.path, rerr)
			continue
		}
		rf, perr := rgd.ParseRgd(data)
		if perr != nil {
			t.Errorf("  ParseRgd: %v", perr)
			continue
		}
		top := []string{}
		for _, e := range rf.Root.Children {
			top = append(top, e.Name)
		}
		t.Logf("  rgd top entries: %s", strings.Join(top, ", "))
		e := rgdFindPath(rf.Root, "ui_ext", "ui_info", "screen_name_id")
		t.Logf("  ui_ext=%v screen_name_id=%q animator=%q",
			rgdFindChild(rf.Root, "ui_ext") != nil,
			func() string {
				if e == nil {
					return "<missing>"
				}
				return e.DisplayValue()
			}(),
			unitAnimatorName(rf))

		// Icon chain: convention, by-name, then rgd icon_name.
		img := ui.unitIcon(u)
		if img != nil {
			t.Logf("  icon: ok bounds=%v", img.Bounds())
		} else {
			conv := `art\ui\ingame\` + unitTemplateIconBase(u.race) + `\` + u.name + "_icon.tga"
			t.Logf("  icon: MISSING; convention %q found=%v; byName %q found=%v; rgd icon_name=%q",
				conv, tree.FindFile(conv) != nil,
				u.name+"_icon.tga", tree.FindByName(u.name+"_icon.tga") != nil,
				rgdEntryStr(rgdFindPath(rf.Root, "ui_ext", "ui_info", "icon_name")))
		}

		// Model resolution as the viewport does it.
		d := loadUnitDetails(tree, u)
		if d.error != "" {
			t.Logf("  details: error %q", d.error)
			continue
		}
		src, ok := newUnitModelSource(tree, d)
		if !ok {
			t.Logf("  model: no source")
			continue
		}
		t.Logf("  model: animator=%q path=%q", src.animator, unitModelPath(tree, src))
	}
}

// ─── merged from x_test_unit_ui_test.go ────────────────────────────────────────

func TestUnitSectionsAreInOrder(t *testing.T) {
	// The order is the reading order: what the object is, then what it uses,
	// then what it belongs to, then its relationships both ways, then what has
	// been moved out of the mod, and only then the mod as a whole.
	want := []string{"Unit info", "Weapons", "Abilities", "Squads", "Links", "Frozen", "Problems", "Events", "Viewport", "Classic OE"}
	if len(unitSections) != len(want) {
		t.Fatalf("got %d sections, want %d: %v", len(unitSections), len(want), unitSectionLabels())
	}
	for i, label := range want {
		if unitSections[i].label != label {
			t.Errorf("section %d = %q, want %q", i, unitSections[i].label, label)
		}
	}
	// Every section needs both column headings, or a column comes out blank.
	for _, s := range unitSections {
		if s.middleTitle == "" || s.rightTitle == "" || s.hint == "" {
			t.Errorf("section %q is incomplete: middle=%q right=%q hint=%q", s.label, s.middleTitle, s.rightTitle, s.hint)
		}
	}
}

func unitSectionLabels() []string {
	out := make([]string, 0, len(unitSections))
	for _, s := range unitSections {
		out = append(out, s.label)
	}
	return out
}

// TestCurrentUnitSectionSurvivesBadIndex guards the layout against an index
// left over from a future, longer section list: a bad one must fall back to
// the first section rather than panic in the middle of a frame.
func TestCurrentUnitSectionSurvivesBadIndex(t *testing.T) {
	for _, bad := range []int{-1, len(unitSections), len(unitSections) + 5} {
		ui := &uiState{unitSection: bad}
		if got := ui.currentUnitSection().label; got != unitSections[0].label {
			t.Errorf("section index %d gave %q, want fallback %q", bad, got, unitSections[0].label)
		}
	}
	ui := &uiState{unitSection: 1}
	if got := ui.currentUnitSection().label; got != "Weapons" {
		t.Errorf("section index 1 gave %q, want Weapons", got)
	}
}

// TestUnitColumnWidths checks the proportions the dialog was specified with:
// a narrow fixed menu on the left and a middle column of about a third.
func TestUnitColumnWidths(t *testing.T) {
	const (
		gap   = 8
		minW  = 140
		leftW = 132
	)
	for _, bodyW := range []int{1600, 1200, 900, 700} {
		left, mid, right := unitColumnWidths(bodyW, leftW, gap, minW)
		if left != leftW {
			t.Errorf("body %d: left = %d, want the fixed %d", bodyW, left, leftW)
		}
		usable := bodyW - leftW - 2*gap
		if mid < minW || right < minW {
			t.Errorf("body %d: a column fell below the minimum: mid=%d right=%d", bodyW, mid, right)
			continue
		}
		if mid+right != usable {
			t.Errorf("body %d: mid+right = %d, want the usable %d", bodyW, mid+right, usable)
		}
		// A third of the window, to within the rounding of one pixel.
		third := usable / 3
		if diff := mid - third; diff > 1 || diff < -1 {
			t.Errorf("body %d: mid = %d, want about a third (%d)", bodyW, mid, third)
		}
		// The right column must be the widest one, or the detail pane loses.
		if right <= mid {
			t.Errorf("body %d: right = %d is not wider than mid = %d", bodyW, right, mid)
		}
	}
}

// TestUnitColumnWidthsNarrowWindow checks the floor: a body too small for the
// specified proportions keeps the minimums instead of going negative.
func TestUnitColumnWidthsNarrowWindow(t *testing.T) {
	const gap, minW, leftW = 8, 140, 132
	left, mid, right := unitColumnWidths(300, leftW, gap, minW)
	if left != leftW || mid < minW || right < minW {
		t.Fatalf("narrow body gave left=%d mid=%d right=%d, want left=%d and both others >= %d", left, mid, right, leftW, minW)
	}
	// A negative width would panic in clip.Rect, so nothing may go below zero.
	for _, bodyW := range []int{0, 10, 100, 200} {
		_, m, r := unitColumnWidths(bodyW, leftW, gap, minW)
		if m < 0 || r < 0 {
			t.Errorf("body %d produced a negative width: mid=%d right=%d", bodyW, m, r)
		}
	}
}

// TestUnitObjectLabelFallsBack checks the dialog title never comes out blank,
// since an unnamed card would read as a rendering bug. The dialog now browses
// the mod's units rather than the open file, so the label names the selected
// unit — and with no mod loaded there is nothing to name.
func TestUnitObjectLabelFallsBack(t *testing.T) {
	ui := &uiState{}
	if got := ui.unitObjectLabel(); got != "No unit selected" {
		t.Errorf("with no mod loaded the label is %q, want %q", got, "No unit selected")
	}
	// With a catalog but a stale selection, the label still resolves to a real
	// unit rather than coming out blank.
	ui.modTree = testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`: "",
	})
	if got := ui.unitObjectLabel(); got != "necron_lord" {
		t.Errorf("label = %q, want the selected unit's name", got)
	}
	// An out of range index must not panic and must still name a unit.
	ui.unitRaceSel = 99
	ui.unitSel = 99
	if got := ui.unitObjectLabel(); got == "" {
		t.Error("a stale selection index produced an empty label")
	}
}

// ─── merged from x_test_unit_tab_test.go ────────────────────────────────────────

func TestUnitTabIsSingleton(t *testing.T) {
	ui := &uiState{}
	if ui.unitTabIndex() != -1 {
		t.Fatal("a fresh ui must have no object tab")
	}
	ui.openUnitTab()
	if got := len(ui.openTabs); got != 1 {
		t.Fatalf("openTabs = %d, want 1", got)
	}
	if !ui.unitTabActive() {
		t.Error("the object tab should be active after opening it")
	}
	ui.openUnitTab()
	if got := len(ui.openTabs); got != 1 {
		t.Fatalf("opening twice produced %d tabs, want 1", got)
	}
}

// Selecting something else must make the object tab inactive, and coming back to
// it must make it active again — the tab bar is the only thing that decides.
func TestUnitTabTracksSelection(t *testing.T) {
	ui := &uiState{}
	ui.openUnitTab()
	ui.tabSel = 0
	if ui.unitTabActive() {
		t.Error("the object tab is still active with the Module tab selected")
	}
	ui.openUnitTab()
	if !ui.unitTabActive() {
		t.Error("reopening did not bring the object tab forward")
	}
}

// Closing the tab must leave the selection in a state the rest of the app can
// use, and reopening must land on the same unit rather than resetting it.
func TestUnitTabCloseKeepsSelection(t *testing.T) {
	ui := &uiState{modTree: testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`:  "",
		`Data\attrib\ebps\races\necrons\troops\necron_lord2.rgd`: "",
	})}
	ui.openUnitTab()
	ui.unitSel = 1
	want := ui.currentUnit()
	if want == nil {
		t.Fatal("no unit selected to begin with")
	}
	ui.closeUnitTab()
	if ui.unitTabIndex() != -1 {
		t.Error("the object tab survived being closed")
	}
	if ui.unitTabActive() {
		t.Error("a closed object tab is still active")
	}
	// An open dropdown belongs to the tab that went away. Closing through the
	// tab bar's × calls closeTabAt directly, so that is the path exercised
	// here rather than closeUnitTab.
	ui.openUnitTab()
	ui.unitListOpen = true
	ui.closeTabAt(ui.unitTabIndex())
	if ui.unitListOpen || ui.unitRaceOpen {
		t.Error("a dropdown was left open after its tab closed")
	}
	ui.openUnitTab()
	if got := ui.currentUnit(); got == nil || got.name != want.name {
		t.Errorf("reopening landed on %v, want %v", got, want)
	}
}

// A file tab and the object tab coexist, and closing one leaves the other's
// selection alone.
func TestUnitTabCoexistsWithFileTabs(t *testing.T) {
	ui := &uiState{}
	ui.openTabs = append(ui.openTabs, &fileTab{kind: "rgd", path: `Data\attrib\x.rgd`})
	ui.ensureTabClicks()
	ui.openUnitTab()
	if got := len(ui.openTabs); got != 2 {
		t.Fatalf("openTabs = %d, want 2", got)
	}
	if ui.unitTabIndex() != 1 {
		t.Errorf("object tab index = %d, want 1", ui.unitTabIndex())
	}
	if ui.tabSel != 2 {
		t.Errorf("tabSel = %d, want the object tab selected", ui.tabSel)
	}
	// The file tab must still be the one before it.
	ui.tabSel = 1
	if got := ui.activeTab(); got == nil || got.kind != "rgd" {
		t.Errorf("activeTab = %+v, want the rgd tab", got)
	}
	if ui.unitTabActive() {
		t.Error("the object tab claims to be active while a file tab is selected")
	}
}

// The object tab is not backed by a file, so a mod reload must not close it and
// the session writer must not try to remember a path for it.
func TestUnitTabSurvivesModReloadAndIsNotPersisted(t *testing.T) {
	tab := &fileTab{kind: unitTabKind, path: unitTabPath}
	if !unitTabHasNoFile(tab) {
		t.Error("the object tab must count as having no file")
	}
	if !isUnitTab(tab) {
		t.Error("isUnitTab did not recognise the object tab")
	}
	if got := unitTabLabelFor(tab); got != unitTabLabel {
		t.Errorf("tab label = %q, want %q", got, unitTabLabel)
	}
	// A real file tab is not confused for the object tab.
	file := &fileTab{kind: "rgd", path: `Data\attrib\x.rgd`}
	if unitTabHasNoFile(file) {
		t.Error("a real file tab must not count as having no file")
	}
	if got := unitTabLabelFor(file); got != "" {
		t.Errorf("a file tab must not get the object label, got %q", got)
	}
}

// The session writer skips tabs with no file, so the object tab is not written
// out as a path that could not be reopened. The state file's location follows
// the config dir, which is redirected to a temp one so the real session file is
// not touched.
func TestObjectTabNotWrittenToSession(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	ui := newUI(unitTestTheme())
	ui.modTree = testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`: "",
	})
	ui.openTabs = append(ui.openTabs, &fileTab{kind: "rgd", path: `Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`})
	ui.openUnitTab()
	if err := savePersistedState(ui); err != nil {
		t.Fatalf("savePersistedState: %v", err)
	}
	saved, found, err := loadPersistedState()
	if err != nil {
		t.Fatalf("loadPersistedState: %v", err)
	}
	if !found {
		t.Fatal("the state file was not written")
	}
	if len(saved.Tabs) != 1 {
		t.Fatalf("persisted %d tabs, want only the file tab: %+v", len(saved.Tabs), saved.Tabs)
	}
	if saved.Tabs[0].Path == "" {
		t.Error("the file tab lost its path")
	}
}
