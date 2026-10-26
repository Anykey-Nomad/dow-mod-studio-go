//go:build windows

package app

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/widget"
)

// settingsWindow is a window with the Settings dialog already open, the way
// the File menu opens it, so the tests press the real field rather than a copy
// of it.
func settingsWindow(t *testing.T) *windowFrame {
	t.Helper()
	ui := newUI(unitTestTheme())
	OpenSettingsDialog(ui)
	f := newWindowFrame(ui, 1000, 700)
	if !ui.SettingsDialog {
		t.Fatal("the Settings dialog did not open")
	}
	return f
}

// findInDialog walks the pointer over the card and returns where the widget
// under it says it is hovered. The Settings card does not record its own box,
// so the whole window is searched: that is the stronger claim anyway, since
// what matters is that the button is somewhere a reader can click.
func findInDialog(t *testing.T, f *windowFrame, c *widget.Clickable) (f32.Point, bool) {
	t.Helper()
	for y := 200; y < 520; y += 4 {
		if at, ok := findHoverPoint(t, f, c.Hovered, image.Rect(300, y, 700, y), 4); ok {
			return at, true
		}
	}
	return f32.Point{}, false
}

// findFormatField returns a point where clicking puts the caret in the text
// formats field. Editors report no hover, so the field is found the only way
// that says anything about the layout: by clicking until the caret is in it.
// The scan stops short of the card's bottom edge so it cannot press Cancel.
func findFormatField(t *testing.T, f *windowFrame) (f32.Point, bool) {
	t.Helper()
	ui := f.ui
	for y := 210; y < 450; y += 4 {
		for x := 330; x < 670; x += 8 {
			at := f32.Pt(float32(x), float32(y))
			f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
			f.frame()
			f.send(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at})
			f.frame()
			if ui.SettingsFormatEditor.Focused(f.gtx) {
				return at, true
			}
		}
	}
	return f32.Point{}, false
}

// TestTheSettingsDialogOffersItsTextFormatsField checks the thing that makes
// the setting usable at all: the row is really in the dialog and a reader can
// click into it. An editor laid out at zero size cannot be clicked and Gio does
// not complain, so the caret is the check.
func TestTheSettingsDialogOffersItsTextFormatsField(t *testing.T) {
	f := settingsWindow(t)
	at, ok := findFormatField(t, f)
	if !ok {
		t.Fatal("the text formats field was not reachable in the Settings dialog")
	}
	t.Logf("the formats field takes the caret at %v", at)
}

// TestTypingFormatsInSettingsAndPressingOkChangesWhatOpens is the whole
// feature end to end through the real dialog: the list goes in the field, OK is
// pressed, and a format the program has never heard of opens in the built-in
// editor by one click.
func TestTypingFormatsInSettingsAndPressingOkChangesWhatOpens(t *testing.T) {
	f := settingsWindow(t)
	ui := f.ui
	if _, ok := findFormatField(t, f); !ok {
		t.Fatal("the text formats field was not reachable")
	}
	ui.SettingsFormatEditor.SetText(".lua, .txt, .dsl, card")
	f.frame()
	at, ok := findInDialog(t, f, &ui.SettingsOk)
	if !ok {
		t.Fatal("the OK button was not found in the Settings dialog")
	}
	clickAt(f, at)
	if ui.SettingsDialog {
		t.Fatal("the dialog stayed open after OK")
	}
	for _, path := range []string{"a.lua", "a.txt", "a.dsl", "a.card"} {
		if !IsEditorFile(ui, path) {
			t.Errorf("%q does not open in the editor after the reader listed it", path)
		}
	}
	if IsEditorFile(ui, "a.ai") {
		t.Error("a format the reader left out still opens")
	}
}

// Cancel has to put the list back: a dialog the reader backs out of changes
// nothing, which is what the other fields in it already do.
func TestCancellingSettingsLeavesTheFormatsAlone(t *testing.T) {
	f := settingsWindow(t)
	ui := f.ui
	before := formatTextFormats(ui.TextEditorFormats)
	ui.SettingsFormatEditor.SetText(".only, .mine")
	f.frame()
	at, ok := findInDialog(t, f, &ui.SettingsCancel)
	if !ok {
		t.Fatal("the Cancel button was not found in the Settings dialog")
	}
	clickAt(f, at)
	if ui.SettingsDialog {
		t.Fatal("the dialog stayed open after Cancel")
	}
	if got := formatTextFormats(ui.TextEditorFormats); got != before {
		t.Errorf("Cancel changed the formats to %q, want %q", got, before)
	}
	if IsEditorFile(ui, "a.only") {
		t.Error("a format typed into a cancelled dialog took effect")
	}
}

// Reopening the dialog shows what is in force, not what was typed last time
// into a dialog that was cancelled.
func TestReopeningSettingsShowsTheFormatsInForce(t *testing.T) {
	f := settingsWindow(t)
	ui := f.ui
	ui.TextEditorFormats = parseTextFormats(".lua, .mine")
	OpenSettingsDialog(ui)
	f.frame()
	if got := ui.SettingsFormatEditor.Text(); got != ".lua, .mine" {
		t.Errorf("the field showed %q, want %q", got, ".lua, .mine")
	}
}

// formatTestMod is a writable module holding one file of a format the program
// ships no support for, so a click on it can be watched all the way through to
// the tab it opens.
func formatTestMod(t *testing.T, name string) (*uiState, string) {
	t.Helper()
	appPath := t.TempDir()
	dir := filepath.Join(appPath, "FormatMod", "Data", "art")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, name)
	if err := os.WriteFile(disk, []byte("return true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	full := `Data\art\` + name
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "FormatMod"}
	folder := tree.GetOrCreateFolder(tree.Root, "art", &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: dir})
	file := &modtree.TreeFile{Name: name, FullPath: full,
		Source: &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: dir}, DiskPath: disk}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[file.Name] = file
	ui := newUI(unitTestTheme())
	ui.ModTree = tree
	t.Cleanup(tree.Close)
	ui.ModulePath = filepath.Join(appPath, "FormatMod")
	ui.Expanded = map[string]bool{"Data": true, `Data\art`: true}
	ui.TextEditorFormats = parseTextFormats(".lua, .dsl")
	return ui, full
}

// The point of the feature: a format the program ships no support for is
// opened by a single click in the tree, and one that is not listed is not
// opened as text at all.
func TestACustomFormatOpensOnOneClickInTheTree(t *testing.T) {
	ui, full := formatTestMod(t, "my_effect.dsl")
	OpenTreeFile(ui, modtree.VisibleNode{FullPath: full, IsFile: true})
	tab := ActiveTab(ui)
	if tab == nil {
		t.Fatal("one click on a listed format opened no tab")
	}
	if tab.Path != full {
		t.Errorf("one click opened %q, want %q", tab.Path, full)
	}
	if tab.Kind != "text" {
		t.Errorf("the tab is a %q, want the text editor", tab.Kind)
	}
	if tab.TextError != "" {
		t.Errorf("the editor could not read the file: %s", tab.TextError)
	}
	if !strings.Contains(tab.TextOriginal, "return true") {
		t.Errorf("the editor shows %q, want the file's own text", tab.TextOriginal)
	}
}

// Leaving a format out of the list closes it again, which is what makes the
// list a setting rather than a way of adding to it.
func TestAFormatLeftOutOfTheListDoesNotOpenAsText(t *testing.T) {
	ui, full := formatTestMod(t, "other_effect.bin")
	OpenTreeFile(ui, modtree.VisibleNode{FullPath: full, IsFile: true})
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "text" {
		t.Errorf("an unlisted format opened in the text editor: %q", tab.Path)
	}
}

// The formats the game ships with are in the default list, and one click still
// opens them in the editor.
func TestTheDefaultFormatsStillOpenInTheEditor(t *testing.T) {
	ui, full := formatTestMod(t, "unit.ai")
	ui.TextEditorFormats = append([]string(nil), defaultTextFormats...)
	OpenTreeFile(ui, modtree.VisibleNode{FullPath: full, IsFile: true})
	tab := ActiveTab(ui)
	if tab == nil || tab.Kind != "text" {
		t.Fatalf("a .ai file did not open in the editor: %+v", tab)
	}
}
