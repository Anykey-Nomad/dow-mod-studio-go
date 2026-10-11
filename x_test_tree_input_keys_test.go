package main

import (
	"strings"
	"testing"

	"gioui.org/io/key"
)

// Enter has two names in Gio and a platform delivers only one of them. The tree
// was filtering on the wrong one, so Enter did nothing on Windows while the
// arrow keys — which have a single name each — kept working.
//
// These tests drive the decision functions rather than a key event, because
// this project's headless tests cannot get an event through Gio's router at
// all. That is the project's own convention for input: a test that cannot press
// a key asserts the function the key would have called.

// treeKeyboardUI is a state with a real mod behind it and one file selected, so
// a navigation has something to act on.
func treeKeyboardUI(t *testing.T) *uiState {
	t.Helper()
	tree := openTestDXP3(t)
	ui := &uiState{modTree: tree, keyboardArea: "files", expanded: map[string]bool{"Data": true}}
	target := `Data\attrib\ebps\races\necrons\troops\necron_basic_warrior.rgd`
	segs := strings.Split(target, `\`)
	for i := 2; i < len(segs); i++ {
		ui.expanded[strings.Join(segs[:i], `\`)] = true
	}
	ui.visibleNodes = ui.getVisibleNodes()
	for _, n := range ui.visibleNodes {
		if n.FullPath == target {
			ui.selectedTreePath = target
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
			ui.navigateFileTree(name)
			tab := ui.activeTab()
			if tab == nil {
				t.Fatalf("Enter under the name %q opened no tab", name)
			}
			if !strings.HasSuffix(tab.path, "necron_basic_warrior.rgd") {
				t.Errorf("Enter under %q opened %q, want the selected file", name, tab.path)
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
	before := ui.selectedTreePath
	ui.navigateFileTree(key.NameDownArrow)
	if ui.selectedTreePath == before {
		t.Error("Down did not move the selection, so the arrow keys are no longer reaching the tree")
	}
	if len(ui.openTabs) != 0 {
		t.Error("an arrow key opened a file, which is not what an arrow means")
	}
}

// TestEnterOnAFolderOpensNothing pins the other half of the report: Enter on a
// folder is not an open, and must not fall through to opening something else.
func TestEnterOnAFolderOpensNothing(t *testing.T) {
	tree := openTestDXP3(t)
	ui := &uiState{modTree: tree, keyboardArea: "files", expanded: map[string]bool{"Data": true}}
	ui.visibleNodes = ui.getVisibleNodes()
	ui.selectedTreePath = `Data`
	ui.navigateFileTree(key.NameReturn)
	if len(ui.openTabs) != 0 {
		t.Errorf("Enter on a folder opened %d tab(s)", len(ui.openTabs))
	}
	// And the folder it was on is still there, untouched.
	if len(ui.visibleNodes) == 0 {
		t.Error("the tree lost its nodes")
	}
}
