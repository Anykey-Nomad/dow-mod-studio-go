package main

import (
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// The file tree and the keyboard.
//
// A click on a row is meant to leave the tree on the keyboard: the arrow keys
// walk the list from the row that was clicked, Enter opens what is selected, and
// Delete asks before it takes anything away. These tests drive a whole window
// through Gio's router, because the claim is about the two together — a click
// and then a key — and neither half says anything on its own.

// clickTreeRow puts the pointer on a row of the tree, clicks it, and settles a
// frame, which is what a click on a row amounts to in a window.
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
	f.ui.keyboardArea = "rgd"
	clickTreeRow(t, f, row)
	if !f.ui.treeHasKeyboard() {
		t.Fatalf("a click on row %d did not give the file tree the keyboard", row)
	}
	if got, want := f.ui.selectedTreePath, f.ui.visibleNodes[row].FullPath; got != want {
		t.Errorf("the keyboard is on %q, want the row that was clicked, %q", got, want)
	}
}

// TestARightPressGivesTheTreeTheKeyboardToo covers the other button: the pointer
// is in the tree, so the keys are as well, whichever button it came with.
func TestARightPressGivesTheTreeTheKeyboardToo(t *testing.T) {
	f, row := treeWindow(t)
	f.ui.keyboardArea = "rgd"
	at := treeRowAt(t, f, treeOrigin(t, f), row)
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonSecondary},
	)
	f.frame()
	if !f.ui.contextMenuVisible {
		t.Skip("the right press opened no file menu, so the tree was not under the pointer")
	}
	if !f.ui.treeHasKeyboard() {
		t.Error("a right press in the file tree left the keyboard to the middle tree")
	}
}

// TestAnArrowKeyAfterAClickMovesTheSelection is the first key of the three, and
// the one that has to start from the row that was clicked rather than from
// wherever the list used to be.
func TestAnArrowKeyAfterAClickMovesTheSelection(t *testing.T) {
	f, row := treeWindow(t)
	clickTreeRow(t, f, row)
	want := f.ui.visibleNodes[row+1].FullPath
	keyFrame(f, key.NameDownArrow)
	if f.ui.selectedTreePath != want {
		t.Errorf("Down after a click on row %d selected %q, want the next row, %q", row, f.ui.selectedTreePath, want)
	}
	if f.ui.selectedPath != want {
		t.Errorf("Down selected %q as the current file, want %q", f.ui.selectedPath, want)
	}
	keyFrame(f, key.NameUpArrow)
	if got := f.ui.selectedTreePath; got != f.ui.visibleNodes[row].FullPath {
		t.Errorf("Up after Down selected %q, want the row the click was on, %q", got, f.ui.visibleNodes[row].FullPath)
	}
}

// TestEnterAfterAClickOpensTheSelectedRow is Enter, which has two names and one
// meaning: whichever one the platform delivers, it opens the row the arrows are
// on and not the row the click was on.
func TestEnterAfterAClickOpensTheSelectedRow(t *testing.T) {
	f, row := treeWindow(t)
	clickTreeRow(t, f, row)
	keyFrame(f, key.NameDownArrow)
	want := f.ui.visibleNodes[row+1]
	if !want.IsFile {
		t.Skipf("the row below the one under test is %q, which is not a file", want.Name)
	}
	keyFrame(f, key.NameReturn)
	tab := f.ui.activeTab()
	if tab == nil {
		t.Fatalf("Enter after a click opened no tab; the status line reads %q", f.ui.statusText)
	}
	if !strings.HasSuffix(tab.path, want.Name) {
		t.Errorf("Enter opened %q, want %q, the row the arrows were on", tab.path, want.Name)
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
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.expanded = map[string]bool{"Data": true, `Data\files`: true}
	ui.visibleNodes = ui.getVisibleNodes()
	ui.ensureRowClicks(len(ui.visibleNodes))
	ui.ensureRowHover(len(ui.visibleNodes))
	row, foreignRow := -1, -1
	for i, n := range ui.visibleNodes {
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
	if !f.ui.canDeleteCurrentModFile(f.ui.selectedPath) {
		t.Fatalf("%q is not a file of the mod, so the key has nothing it may delete", f.ui.selectedPath)
	}
	keyFrame(f, key.NameDeleteForward)
	if !f.ui.fileDeleteDialog {
		t.Fatalf("Delete asked nothing; the status line reads %q", f.ui.statusText)
	}
	if f.ui.fileDeletePath != f.ui.visibleNodes[row].FullPath {
		t.Errorf("the confirmation is for %q, want the selected row, %q", f.ui.fileDeletePath, f.ui.visibleNodes[row].FullPath)
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
	for i, n := range f.ui.visibleNodes {
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
	if f.ui.fileDeleteDialog {
		t.Errorf("Delete on the folder %q asked to delete a file", f.ui.visibleNodes[dir].FullPath)
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
	if f.ui.fileDeleteDialog {
		t.Errorf("Delete on the required mod's file %q asked to delete it", f.ui.selectedPath)
	}
	if f.ui.statusText == "" {
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
	if !f.ui.fileDeleteDialog {
		t.Fatal("Delete asked nothing, so there is no confirmation to close")
	}
	keyFrame(f, key.NameEscape)
	if f.ui.fileDeleteDialog {
		t.Error("Escape left the delete confirmation open")
	}
	if f.ui.fileDeletePath != "" {
		t.Errorf("the closed confirmation still names %q", f.ui.fileDeletePath)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Errorf("cancelling the confirmation deleted the file anyway: %v", err)
	}
}
