package app

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/widget"
)

// Creating a new unit, from the button in the object window to the file on disk.
//
// The three tests here are the three things that can go wrong: a name that would
// write a file nobody wants, a file that is written but the mod does not know
// about, and a dialog that does not open when the button is pressed. All of them
// are driven through the real code — the tree, the catalogue, the window.

const newUnitTestRace = "necrons"

// newUnitTestMod is a mod on disk with one race and one unit in it, so the object
// window has a race to write into and the catalogue has something to list.
func newUnitTestMod(t *testing.T) (*uiState, string) {
	t.Helper()
	appPath := t.TempDir()
	modRoot := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "troops")
	if err := os.MkdirAll(modRoot, 0755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(modRoot, "necron_basic_warrior.rgd")
	if err := os.WriteFile(existing, mustMarshalUnit(t, unitTemplateName{Race: newUnitTestRace, Name: "necron_basic_warrior"}), 0644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "NewUnitMod"}
	// The folder has to be built a segment at a time: the tree looks a file up
	// by walking path segments, and one folder whose name holds backslashes is
	// a folder the catalogue would never find.
	folder := tree.Root
	for _, seg := range []string{"attrib", "ebps", "races", "necrons", "troops"} {
		folder = tree.GetOrCreateFolder(folder, seg, &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: modRoot})
	}
	file := &modtree.TreeFile{Name: "necron_basic_warrior.rgd", FullPath: `Data\attrib\ebps\races\necrons\troops\necron_basic_warrior.rgd`,
		Source: &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: modRoot}, DiskPath: existing}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[file.Name] = file
	ui := newUI(unitTestTheme())
	ui.ModTree = tree
	ui.ModulePath = filepath.Join(appPath, "NewUnitMod")
	ui.Expanded = map[string]bool{"Data": true}
	return ui, appPath
}

func mustMarshalUnit(t *testing.T, n unitTemplateName) []byte {
	t.Helper()
	data, err := newUnitRgd(n).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal %s: %v", n.Name, err)
	}
	return data
}

// TestCreatingAUnitWritesItIntoTheMod is the whole of the feature below the
// window: the file lands in the mod's own folder, it is a file the app can read
// back, and the catalogue and the pickers know about it without a reload.
func TestCreatingAUnitWritesItIntoTheMod(t *testing.T) {
	ui, appPath := newUnitTestMod(t)
	t.Cleanup(func() { ui.ModTree.Close() })
	virtual, err := CreateNewObject(ui, newObjUnit, newUnitTestRace, "necron_wraith")
	if err != nil {
		t.Fatalf("createNewObject: %v", err)
	}
	want := `Data\attrib\ebps\races\necrons\troops\necron_wraith.rgd`
	if virtual != want {
		t.Fatalf("the unit was written to %q, want %q", virtual, want)
	}
	disk := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "troops", "necron_wraith.rgd")
	data, err := os.ReadFile(disk)
	if err != nil {
		t.Fatalf("the file is not in the mod folder: %v", err)
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("the file the app wrote does not parse: %v", err)
	}
	if len(rf.Root.Children) != len(unitTemplateSections) {
		t.Errorf("the written unit has %d sections, want the %d every unit has", len(rf.Root.Children), len(unitTemplateSections))
	}
	if ui.ModTree.FindFile(virtual) == nil {
		t.Error("the mod tree does not know the new unit, so it would not show up in the list")
	}
	cat := UnitCatalogFor(ui)
	if cat == nil {
		t.Fatal("no catalogue after creating a unit")
	}
	race := cat.FindRace(newUnitTestRace)
	if race == nil {
		t.Fatalf("the race %q is not in the catalogue any more", newUnitTestRace)
	}
	found := false
	for _, u := range race.UnitsOf() {
		if strings.EqualFold(u.Path, virtual) {
			found = true
		}
	}
	if !found {
		t.Error("the new unit is not in the race's list, so the object window cannot show it")
	}
	if u := CurrentUnit(ui); u == nil || !strings.EqualFold(u.Path, virtual) {
		t.Errorf("the object window is on %v, want the unit that was just created", u)
	}
}

// TestANameThatCannotBeAFileIsRefused covers what the dialog says instead of
// writing: an empty name, a name with a path in it, a name that already names a
// unit. A unit that overwrote somebody's would be worse than one not created.
func TestANameThatCannotBeAFileIsRefused(t *testing.T) {
	ui, _ := newUnitTestMod(t)
	t.Cleanup(func() { ui.ModTree.Close() })
	for _, name := range []string{"", "  ", "necrons/other", `sub\necron`, "necron_wraith.rgd", "necron:lord", "necron_basic_warrior"} {
		if _, err := CreateNewObject(ui, newObjUnit, newUnitTestRace, name); err == nil {
			t.Errorf("the name %q was accepted, so a file nobody asked for could be written", name)
		}
	}
	// And a name that is fine is accepted, so the rules above are not simply
	// refusing everything.
	if _, err := CreateNewObject(ui, newObjUnit, newUnitTestRace, "necron_wraith"); err != nil {
		t.Errorf("a plain name was refused: %v", err)
	}
	if _, err := CreateNewObject(ui, newObjUnit, "", "necron_shade"); err == nil {
		t.Error("a unit was created with no race, so there was nowhere to write it")
	}
}

// TestTheNewUnitButtonOpensTheDialogAndTheDialogWritesTheUnit is the path a
// user takes: the button in the object window's picker strip, the name, Create.
// The button is found by hovering along the strip rather than by being told
// where it is, so the test fails if the button moves somewhere unreachable.
func TestTheNewUnitButtonOpensTheDialogAndTheDialogWritesTheUnit(t *testing.T) {
	ui, appPath := newUnitTestMod(t)
	t.Cleanup(func() { ui.ModTree.Close() })
	OpenUnitTab(ui)
	f := newWindowFrame(ui, 1000, 700)
	if !UnitTabActive(ui) {
		t.Fatal("the object window is not the tab on screen, so its strip was never laid out")
	}
	at, ok := findNewUnitButton(t, f)
	if !ok {
		t.Fatal("the New unit button was not found in the picker strip")
	}
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
	if !ui.NewObjDialog {
		t.Fatalf("a click on the New unit button at %v opened no dialog", f32.Point(at))
	}
	// A name the file system will not take is said and nothing is written.
	ui.NewObjNameEditor.SetText("necron/bad")
	f.frame()
	create, ok := findDialogButton(t, f, &ui.NewObjOk)
	if !ok {
		t.Fatal("the dialog's Create button was not found")
	}
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: create},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: create, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: create},
	)
	f.frame()
	if !ui.NewObjDialog {
		t.Fatal("the dialog closed on a name it should have refused")
	}
	if ui.NewObjStatus == "" {
		t.Error("the dialog said nothing about why the name was refused")
	}
	disk := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "troops", "necron_bad.rgd")
	if _, err := os.Stat(disk); err == nil {
		t.Fatal("a refused name was written to disk anyway")
	}
	// A good name creates the unit, closes the dialog and opens the file.
	ui.NewObjNameEditor.SetText("necron_shade")
	f.frame()
	create, ok = findDialogButton(t, f, &ui.NewObjOk)
	if !ok {
		t.Fatal("the dialog's Create button was not found the second time")
	}
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: create},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: create, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: create},
	)
	f.frame()
	if ui.NewObjDialog {
		t.Fatalf("the dialog stayed up after a unit was created; it says %q", ui.NewObjStatus)
	}
	disk = filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "troops", "necron_shade.rgd")
	data, err := os.ReadFile(disk)
	if err != nil {
		t.Fatalf("the unit was not written: %v", err)
	}
	if _, err := rgd.ParseRgd(data); err != nil {
		t.Fatalf("the unit the dialog wrote does not parse: %v", err)
	}
	tab := ActiveTab(ui)
	if tab == nil || tab.Kind != "rgd" || !strings.HasSuffix(tab.Path, "necron_shade.rgd") {
		t.Errorf("the created unit is not open for editing: %+v", tab)
	}
}

// TestTheNewObjDialogCanBeLeft finds the button, opens the dialog and closes it
// again, because a dialog that writes a file has to be leaveable without writing
// one.
func TestTheNewObjDialogCanBeLeft(t *testing.T) {
	ui, appPath := newUnitTestMod(t)
	t.Cleanup(func() { ui.ModTree.Close() })
	OpenUnitTab(ui)
	f := newWindowFrame(ui, 1000, 700)
	at, ok := findNewUnitButton(t, f)
	if !ok {
		t.Fatal("the New unit button was not found in the picker strip")
	}
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
	if !ui.NewObjDialog {
		t.Fatal("the button opened no dialog")
	}
	ui.NewObjNameEditor.SetText("necron_veil")
	f.frame()
	// Escape leaves the dialog, and nothing is written.
	f.sendKey(key.Event{Name: key.NameEscape, State: key.Press})
	f.frame()
	if ui.NewObjDialog {
		t.Error("Escape left the new unit dialog open")
	}
	disk := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "troops", "necron_veil.rgd")
	if _, err := os.Stat(disk); err == nil {
		t.Error("leaving the dialog wrote the unit anyway")
	}
}

// findNewUnitButton is where the button is, found by asking: the pointer walks
// along the strip and the button is the place the pointer makes it hovered. The
// strip sits under the window's menu bar and tab bar, which are 24 and 28
// pixels tall at the test's scale, and each probe is a whole frame, so the walk
// is a few rows rather than a grid.
func findNewUnitButton(t *testing.T, f *windowFrame) (f32.Point, bool) {
	t.Helper()
	return findStripButton(t, f, &f.ui.UnitNewTrigger)
}

// findNewBuildingButton is the same walk for the button beside it. Both are
// found by hovering rather than by being told where they are, so a test fails if
// either one moves somewhere a user cannot reach.
func findNewBuildingButton(t *testing.T, f *windowFrame) (f32.Point, bool) {
	t.Helper()
	return findStripButton(t, f, &f.ui.UnitBuildingTrigger)
}

// findStripButton walks the pointer along the picker strip until the given
// button says it is hovered.
func findStripButton(t *testing.T, f *windowFrame, c *widget.Clickable) (f32.Point, bool) {
	t.Helper()
	top := unitMenuBarHeightPx + unitTabBarHeightPx
	for _, y := range []int{top + 8, top + 16, top + 24} {
		if at, ok := findHoverPoint(t, f, c.Hovered, image.Rect(20, y, 900, y), 4); ok {
			return at, true
		}
	}
	return f32.Point{}, false
}

// The window's own chrome, at the scale the tests lay out at: the menu bar and
// the tab bar are the fixed rows above the content area.
const (
	unitMenuBarHeightPx = 24
	unitTabBarHeightPx  = 28
)

// findDialogButton is where one of the dialog's buttons is, the same way. The
// card is centred in the window and its buttons are along its bottom edge, so
// the region is that corner of the card rather than the whole window.
func findDialogButton(t *testing.T, f *windowFrame, c *widget.Clickable) (f32.Point, bool) {
	t.Helper()
	card := f.ui.NewObjCardBox
	if card.Empty() {
		return f32.Point{}, false
	}
	// The buttons sit along the card's bottom right corner.
	row := image.Rect(card.Max.X-260, card.Max.Y-20, card.Max.X-8, card.Max.Y-20)
	for _, y := range []int{row.Min.Y - 6, row.Min.Y, row.Min.Y + 6} {
		if at, ok := findHoverPoint(t, f, c.Hovered, image.Rect(row.Min.X, y, row.Max.X, y), 4); ok {
			return at, true
		}
	}
	return f32.Point{}, false
}

// findHoverPoint walks the pointer over a row of the window and returns where
// the widget under it says it is hovered. A test that is told where a button is
// proves nothing about the layout; one that finds it proves the button is
// somewhere a user can reach.
func findHoverPoint(t *testing.T, f *windowFrame, hovered func() bool, row image.Rectangle, step int) (f32.Point, bool) {
	t.Helper()
	for x := row.Min.X; x <= row.Max.X; x += step {
		at := f32.Pt(float32(x), float32(row.Min.Y))
		f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
		f.frame()
		if hovered() {
			return at, true
		}
	}
	return f32.Point{}, false
}

// TestCreatingABuildingWritesItIntoTheMod is the unit's first test for a
// building: the file lands under the race's structures folder rather than its
// troops, and it is a building's file rather than a unit's.
func TestCreatingABuildingWritesItIntoTheMod(t *testing.T) {
	ui, appPath := newUnitTestMod(t)
	t.Cleanup(func() { ui.ModTree.Close() })
	virtual, err := CreateNewObject(ui, newObjBuilding, newUnitTestRace, "necron_relay")
	if err != nil {
		t.Fatalf("createNewObject: %v", err)
	}
	want := `Data\attrib\ebps\races\necrons\structures\necron_relay.rgd`
	if virtual != want {
		t.Fatalf("the building was written to %q, want %q", virtual, want)
	}
	disk := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "structures", "necron_relay.rgd")
	data, err := os.ReadFile(disk)
	if err != nil {
		t.Fatalf("the building is not in the mod folder: %v", err)
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("the file the app wrote does not parse: %v", err)
	}
	names := map[string]bool{}
	for _, ch := range rf.Root.Children {
		names[ch.Name] = true
	}
	for _, want := range []string{"structure_ext", "structure_buildable_ext", "health_ext", "cost_ext"} {
		if !names[want] {
			t.Errorf("the written building has no %s", want)
		}
	}
	if names["moving_ext"] || names["cover_ext"] {
		t.Error("the written building has a unit's sections, so the wrong template was used")
	}
	// And the mod tree and the catalogue both know about it, without a reload.
	if ui.ModTree.FindFile(virtual) == nil {
		t.Error("the mod tree does not know the new building, so it would not show up in the list")
	}
	cat := UnitCatalogFor(ui)
	if cat == nil {
		t.Fatal("no catalogue after creating a building")
	}
	race := cat.FindRace(newUnitTestRace)
	if race == nil {
		t.Fatalf("the race %q is not in the catalogue any more", newUnitTestRace)
	}
	found := false
	for _, u := range race.UnitsOf() {
		if strings.EqualFold(u.Path, virtual) {
			found = true
		}
	}
	if !found {
		t.Error("the new building is not in the race's list, so the object window cannot show it")
	}
	if u := CurrentUnit(ui); u == nil || !strings.EqualFold(u.Path, virtual) {
		t.Errorf("the object window is on %v, want the building that was just created", u)
	}
}

// TestTheNewBuildingButtonWritesTheBuilding is the path a user takes for a
// building: the second button in the strip, the name, Create. It is the unit's
// window test again, and it is here because the two buttons sit side by side and
// either could be the one wired to the wrong dialog.
func TestTheNewBuildingButtonWritesTheBuilding(t *testing.T) {
	ui, appPath := newUnitTestMod(t)
	t.Cleanup(func() { ui.ModTree.Close() })
	OpenUnitTab(ui)
	f := newWindowFrame(ui, 1000, 700)
	at, ok := findNewBuildingButton(t, f)
	if !ok {
		t.Fatal("the New building button was not found in the picker strip")
	}
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
	if !ui.NewObjDialog {
		t.Fatalf("a click on the New building button at %v opened no dialog", f32.Point(at))
	}
	if ui.NewObjKind != newObjBuilding {
		t.Error("the New building button opened the dialog for something else")
	}
	// The dialog says where the file will go, and for a building that is the
	// structures folder.
	if label := NewObjFolderLabel(ui); !strings.Contains(label, `\structures\`) {
		t.Errorf("the dialog says it will write to %q, want the structures folder", label)
	}
	ui.NewObjNameEditor.SetText("necron_relay")
	f.frame()
	create, ok := findDialogButton(t, f, &ui.NewObjOk)
	if !ok {
		t.Fatal("the dialog's Create button was not found")
	}
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: create},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: create, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: create},
	)
	f.frame()
	if ui.NewObjDialog {
		t.Fatalf("the dialog stayed up after a building was created; it says %q", ui.NewObjStatus)
	}
	disk := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "structures", "necron_relay.rgd")
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("the building was not written: %v", err)
	}
	tab := ActiveTab(ui)
	if tab == nil || tab.Kind != "rgd" || !strings.HasSuffix(tab.Path, "necron_relay.rgd") {
		t.Errorf("the created building is not open for editing: %+v", tab)
	}
	// And it did not go into the troops folder as well, which is the mistake a
	// shared writer could make.
	unitDisk := filepath.Join(appPath, "NewUnitMod", "Data", "attrib", "ebps", "races", newUnitTestRace, "troops", "necron_relay.rgd")
	if _, err := os.Stat(unitDisk); err == nil {
		t.Error("the building was also written as a unit")
	}
}
