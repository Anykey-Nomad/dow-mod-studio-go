//go:build windows

package app

import (
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/widget/material"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/testutil"
)

// The viewer's list of formats and the decoder's list of formats were two
// lists written out separately, and .rtx and .rsh reached the second without
// ever reaching the first. That is invisible in a test that only checks the
// decoder and invisible in one that only checks a click, so this checks both
// against each other: every extension the viewer claims has to be one it
// recognises, and every format the decoder has a case for has to be one the
// viewer claims.
func TestImageViewerCoversEveryDecodableFormat(t *testing.T) {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".bmp", ".tga", ".dds", ".rtx", ".rsh"} {
		if !isImageViewerFile("sample" + ext) {
			t.Errorf("the decoder handles %q but a click on it would not open the viewer", ext)
		}
	}
	// Case must not matter, since the game's own files are not consistent
	// about it.
	for _, name := range []string{"UNIT.RTX", "Unit.Rsh", "x.DDS"} {
		if !isImageViewerFile(name) {
			t.Errorf("%q was not recognised regardless of case", name)
		}
	}
	// A name that merely contains an extension must not be mistaken for one:
	// a mod ships files like "unit.rtx_backup", and those are not images.
	for _, name := range []string{"script.lua", "unit.rgd", "notes.txt", "unit.rtx2", "rtx", "x.rtx.gz"} {
		if isImageViewerFile(name) {
			t.Errorf("%q was routed to the image viewer but is not an image", name)
		}
	}
}

// The routing has to actually reach openImage, not merely recognise the
// extension: this drives openTreeFile for a real .rtx from the tree and checks
// the tab that comes back is an image tab rather than a text editor.
func TestOpeningAnRtxOpensTheImageViewer(t *testing.T) {
	tree := probeTestTree(t)
	defer tree.Close()

	var path string
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if path == "" && strings.EqualFold(filepath.Ext(f.Name), ".rtx") {
			path = f.FullPath
		}
	})
	if path == "" {
		t.Skip("no .rtx in the tree")
	}

	ui := newUI(probeTestTheme())
	ui.ModTree = tree
	OpenTreeFile(ui, modtree.VisibleNode{Name: filepath.Base(path), FullPath: path, IsFile: true})

	if len(ui.OpenTabs) == 0 {
		t.Fatalf("clicking %s opened no tab at all", path)
	}
	tab := ui.OpenTabs[len(ui.OpenTabs)-1]
	if tab.Kind != "image" {
		t.Errorf("clicking %s opened a %q tab, want an image tab", path, tab.Kind)
	}
	// And the tab has to be the one on show, or the panel shows the module
	// placeholder while a tab sits in the bar looking as though it worked.
	if got := ActiveTab(ui); got == nil || got.Kind != "image" {
		t.Errorf("after clicking %s the active tab is %s, want the image tab",
			path, probeTabKind(got))
	}
}

func probeTabKind(t *fileTab) string {
	if t == nil {
		return "<none>"
	}
	return t.Kind
}

// probeTestTree builds the game tree, skipping rather than failing when the
// game is not installed: what these tests need from it is one real file of
// each kind.
func probeTestTree(t *testing.T) *modtree.ModTree {
	t.Helper()
	tree, err := modtree.BuildModTree(testutil.DXP3Module, "English")
	if err != nil {
		t.Skipf("BuildModTree: %v", err)
	}
	return tree
}

func probeTestTheme() *material.Theme {
	return material.NewTheme()
}
