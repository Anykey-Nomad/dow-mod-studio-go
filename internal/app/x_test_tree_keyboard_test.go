package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/testutil"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

func clickTreeRow(t *testing.T, f *windowFrame, row int) {
	t.Helper()
	at := treeRowAt(t, f, treeOrigin(t, f), row)
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
}

// keyFrame presses a key and lets a frame run, the way a window does with one
// key event in it.
func keyFrame(f *windowFrame, name key.Name) {
	f.sendKey(key.Event{Name: name, State: key.Press})
	f.frame()
}

// TestAClickLeavesTheTreeOnTheKeyboard is the base of the rest: a click on a row
// is what puts the tree on the keyboard, and it puts the row that was clicked
// under the keys. The middle tree is given the keyboard first, which is where it
// goes when the user clicks in there.
func TestAClickLeavesTheTreeOnTheKeyboard(t *testing.T) {
	f, row := treeWindow(t)
	f.ui.KeyboardArea = "rgd"
	clickTreeRow(t, f, row)
	if !TreeHasKeyboard(f.ui) {
		t.Fatalf("a click on row %d did not give the file tree the keyboard", row)
	}
	if got, want := f.ui.SelectedTreePath, f.ui.VisibleNodes[row].FullPath; got != want {
		t.Errorf("the keyboard is on %q, want the row that was clicked, %q", got, want)
	}
}

// TestARightPressGivesTheTreeTheKeyboardToo covers the other button: the pointer
// is in the tree, so the keys are as well, whichever button it came with.
func TestARightPressGivesTheTreeTheKeyboardToo(t *testing.T) {
	f, row := treeWindow(t)
	f.ui.KeyboardArea = "rgd"
	at := treeRowAt(t, f, treeOrigin(t, f), row)
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonSecondary},
	)
	f.frame()
	if !f.ui.ContextMenuVisible {
		t.Skip("the right press opened no file menu, so the tree was not under the pointer")
	}
	if !TreeHasKeyboard(f.ui) {
		t.Error("a right press in the file tree left the keyboard to the middle tree")
	}
}

// TestAnArrowKeyAfterAClickMovesTheSelection is the first key of the three, and
// the one that has to start from the row that was clicked rather than from
// wherever the list used to be.
func TestAnArrowKeyAfterAClickMovesTheSelection(t *testing.T) {
	f, row := treeWindow(t)
	clickTreeRow(t, f, row)
	want := f.ui.VisibleNodes[row+1].FullPath
	keyFrame(f, key.NameDownArrow)
	if f.ui.SelectedTreePath != want {
		t.Errorf("Down after a click on row %d selected %q, want the next row, %q", row, f.ui.SelectedTreePath, want)
	}
	if f.ui.SelectedPath != want {
		t.Errorf("Down selected %q as the current file, want %q", f.ui.SelectedPath, want)
	}
	keyFrame(f, key.NameUpArrow)
	if got := f.ui.SelectedTreePath; got != f.ui.VisibleNodes[row].FullPath {
		t.Errorf("Up after Down selected %q, want the row the click was on, %q", got, f.ui.VisibleNodes[row].FullPath)
	}
}

// TestEnterAfterAClickOpensTheSelectedRow is Enter, which has two names and one
// meaning: whichever one the platform delivers, it opens the row the arrows are
// on and not the row the click was on.
func TestEnterAfterAClickOpensTheSelectedRow(t *testing.T) {
	f, row := treeWindow(t)
	clickTreeRow(t, f, row)
	keyFrame(f, key.NameDownArrow)
	want := f.ui.VisibleNodes[row+1]
	if !want.IsFile {
		t.Skipf("the row below the one under test is %q, which is not a file", want.Name)
	}
	keyFrame(f, key.NameReturn)
	tab := ActiveTab(f.ui)
	if tab == nil {
		t.Fatalf("Enter after a click opened no tab; the status line reads %q", f.ui.StatusText)
	}
	if !strings.HasSuffix(tab.Path, want.Name) {
		t.Errorf("Enter opened %q, want %q, the row the arrows were on", tab.Path, want.Name)
	}
}

// managedModWindow is a whole window over a mod that exists on disk, which is
// what Delete needs: a file of the mod is a file the app is allowed to take
// away, and the DXP3 fixture is made of files that it is not. A file of a
// required mod is in the list as well, because it is the one Delete must refuse.
func managedModWindow(t *testing.T) (*windowFrame, int, int, string) {
	t.Helper()
	appPath := t.TempDir()
	modRoot := filepath.Join(appPath, "KeyboardMod", "Data")
	reqRoot := filepath.Join(appPath, "RequiredMod", "Data")
	for _, dir := range []string{modRoot, reqRoot} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	diskPath := filepath.Join(modRoot, "target.tga")
	reqPath := filepath.Join(reqRoot, "borrowed.tga")
	for path, data := range map[string]string{diskPath: "not really an image", reqPath: "nor this"} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "KeyboardMod"}
	folder := tree.GetOrCreateFolder(tree.Root, "files", &modtree.FileSource{Kind: modtree.KindThisMod})
	target := &modtree.TreeFile{Name: "target.tga", FullPath: `Data\files\target.tga`, Source: &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: modRoot}, DiskPath: diskPath}
	foreign := &modtree.TreeFile{Name: "borrowed.tga", FullPath: `Data\files\borrowed.tga`, Source: &modtree.FileSource{Kind: modtree.KindReqMod, DiskRoot: reqRoot}, DiskPath: reqPath}
	folder.Files = append(folder.Files, target, foreign)
	for _, file := range folder.Files {
		folder.FileByLower[file.Name] = file
	}
	ui := newUI(testutil.UnitTestTheme())
	ui.ModTree = tree
	ui.Expanded = map[string]bool{"Data": true, `Data\files`: true}
	ui.VisibleNodes = GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(ui.VisibleNodes))
	EnsureRowHover(ui, len(ui.VisibleNodes))
	row, foreignRow := -1, -1
	for i, n := range ui.VisibleNodes {
		switch n.FullPath {
		case target.FullPath:
			row = i
		case foreign.FullPath:
			foreignRow = i
		}
	}
	if row < 0 || foreignRow < 0 {
		t.Skip("the fixture's files are not in the visible tree")
	}
	return newWindowFrame(ui, 900, 700), row, foreignRow, diskPath
}

// TestDeleteAfterAClickAsksBeforeTakingAnything is Delete, and the whole point
// of it is the asking: the key may not move a file, it may only put the file up
// for the confirmation the file menu has always used.
func TestDeleteAfterAClickAsksBeforeTakingAnything(t *testing.T) {
	f, row, _, diskPath := managedModWindow(t)
	clickTreeRow(t, f, row)
	if !CanDeleteCurrentModFile(f.ui, f.ui.SelectedPath) {
		t.Fatalf("%q is not a file of the mod, so the key has nothing it may delete", f.ui.SelectedPath)
	}
	keyFrame(f, key.NameDeleteForward)
	if !f.ui.FileDeleteDialog {
		t.Fatalf("Delete asked nothing; the status line reads %q", f.ui.StatusText)
	}
	if f.ui.FileDeletePath != f.ui.VisibleNodes[row].FullPath {
		t.Errorf("the confirmation is for %q, want the selected row, %q", f.ui.FileDeletePath, f.ui.VisibleNodes[row].FullPath)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Errorf("the key took the file away before anything confirmed it: %v", err)
	}
}

// TestDeleteOnAFolderAsksNothing is the other half: a folder is not a file the
// mod can lose, and the menu's Delete is disabled for the same reason, so the
// key must not open a dialog that has nothing to confirm.
func TestDeleteOnAFolderAsksNothing(t *testing.T) {
	f, _, _, _ := managedModWindow(t)
	dir := -1
	for i, n := range f.ui.VisibleNodes {
		if n.IsDir {
			dir = i
			break
		}
	}
	if dir < 0 {
		t.Skip("the fixture has no folder in the visible tree")
	}
	clickTreeRow(t, f, dir)
	keyFrame(f, key.NameDeleteForward)
	if f.ui.FileDeleteDialog {
		t.Errorf("Delete on the folder %q asked to delete a file", f.ui.VisibleNodes[dir].FullPath)
	}
}

// TestDeleteOnAFileOfAnotherModAsksNothing is the case the menu's greyed out
// Delete stands for. The file is right there in the list and the key works, so
// the app has to say why it will not — not open a dialog for a file that is not
// the mod's to lose.
func TestDeleteOnAFileOfAnotherModAsksNothing(t *testing.T) {
	f, _, foreignRow, _ := managedModWindow(t)
	clickTreeRow(t, f, foreignRow)
	keyFrame(f, key.NameDeleteForward)
	if f.ui.FileDeleteDialog {
		t.Errorf("Delete on the required mod's file %q asked to delete it", f.ui.SelectedPath)
	}
	if f.ui.StatusText == "" {
		t.Error("Delete on a file of another mod said nothing about why it will not")
	}
}

// TestEscapeClosesTheDeleteConfirmation closes the loop the keyboard opens: the
// confirmation is now reachable without the mouse, so it has to be leaveable
// without it too — and leaving it must not delete anything.
func TestEscapeClosesTheDeleteConfirmation(t *testing.T) {
	f, row, _, diskPath := managedModWindow(t)
	clickTreeRow(t, f, row)
	keyFrame(f, key.NameDeleteForward)
	if !f.ui.FileDeleteDialog {
		t.Fatal("Delete asked nothing, so there is no confirmation to close")
	}
	keyFrame(f, key.NameEscape)
	if f.ui.FileDeleteDialog {
		t.Error("Escape left the delete confirmation open")
	}
	if f.ui.FileDeletePath != "" {
		t.Errorf("the closed confirmation still names %q", f.ui.FileDeletePath)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Errorf("cancelling the confirmation deleted the file anyway: %v", err)
	}
}

// ─── merged from x_test_tree_input_keys_test.go ────────────────────────────────────────

func treeKeyboardUI(t *testing.T) *uiState {
	t.Helper()
	tree := testutil.OpenDXP3(t)
	ui := &uiState{ModTree: tree, KeyboardArea: "files", Expanded: map[string]bool{"Data": true}}
	target := `Data\attrib\ebps\races\necrons\troops\necron_basic_warrior.rgd`
	segs := strings.Split(target, `\`)
	for i := 2; i < len(segs); i++ {
		ui.Expanded[strings.Join(segs[:i], `\`)] = true
	}
	ui.VisibleNodes = GetVisibleNodes(ui)
	for _, n := range ui.VisibleNodes {
		if n.FullPath == target {
			ui.SelectedTreePath = target
			return ui
		}
	}
	t.Skip("the target unit is not in the visible tree")
	return nil
}

// TestEnterOpensTheSelectedFileUnderEitherName is the regression test for the
// reported bug. Both names are checked, because a fix that only handled the one
// the developer happened to test against would leave the other platform broken —
// and on Windows it is NameReturn, not the NameEnter the code used to ask for.
func TestEnterOpensTheSelectedFileUnderEitherName(t *testing.T) {
	for _, name := range []key.Name{key.NameReturn, key.NameEnter} {
		t.Run(string(name), func(t *testing.T) {
			ui := treeKeyboardUI(t)
			NavigateFileTree(ui, name)
			tab := ActiveTab(ui)
			if tab == nil {
				t.Fatalf("Enter under the name %q opened no tab", name)
			}
			if !strings.HasSuffix(tab.Path, "necron_basic_warrior.rgd") {
				t.Errorf("Enter under %q opened %q, want the selected file", name, tab.Path)
			}
		})
	}
}

// TestEnterNamesAreDistinct pins the fact the fix rests on, so that a future Gio
// which folds the two names together says so here rather than leaving a filter
// that silently matches nothing.
func TestEnterNamesAreDistinct(t *testing.T) {
	if key.NameReturn == key.NameEnter {
		t.Skip("this Gio maps both names to one key, so a single filter would do")
	}
	if !isEnterKey(key.NameReturn) {
		t.Error("isEnterKey rejects NameReturn, which is the name Windows delivers")
	}
	if !isEnterKey(key.NameEnter) {
		t.Error("isEnterKey rejects NameEnter, which is the name the code used to ask for")
	}
	for _, notEnter := range []key.Name{key.NameUpArrow, key.NameDownArrow, key.NameSpace, key.NameEscape} {
		if isEnterKey(notEnter) {
			t.Errorf("isEnterKey accepts %q, which is not Enter", notEnter)
		}
	}
	// A key filter takes one name, so both have to be in the list the handler
	// loops over or one of them is never asked for.
	var hasReturn, hasEnter bool
	for _, n := range treeNavKeyNames {
		hasReturn = hasReturn || n == key.NameReturn
		hasEnter = hasEnter || n == key.NameEnter
	}
	if !hasReturn || !hasEnter {
		t.Errorf("the tree's key list is %v, which does not carry both Enter names", treeNavKeyNames)
	}
}

// TestArrowKeysStillMoveTheSelection checks the fix did not disturb the keys that
// already worked, and that an arrow does not open anything.
func TestArrowKeysStillMoveTheSelection(t *testing.T) {
	ui := treeKeyboardUI(t)
	before := ui.SelectedTreePath
	NavigateFileTree(ui, key.NameDownArrow)
	if ui.SelectedTreePath == before {
		t.Error("Down did not move the selection, so the arrow keys are no longer reaching the tree")
	}
	if len(ui.OpenTabs) != 0 {
		t.Error("an arrow key opened a file, which is not what an arrow means")
	}
}

// TestEnterOnAFolderOpensNothing pins the other half of the report: Enter on a
// folder is not an open, and must not fall through to opening something else.
func TestEnterOnAFolderOpensNothing(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	ui := &uiState{ModTree: tree, KeyboardArea: "files", Expanded: map[string]bool{"Data": true}}
	ui.VisibleNodes = GetVisibleNodes(ui)
	ui.SelectedTreePath = `Data`
	NavigateFileTree(ui, key.NameReturn)
	if len(ui.OpenTabs) != 0 {
		t.Errorf("Enter on a folder opened %d tab(s)", len(ui.OpenTabs))
	}
	// And the folder it was on is still there, untouched.
	if len(ui.VisibleNodes) == 0 {
		t.Error("the tree lost its nodes")
	}
}
