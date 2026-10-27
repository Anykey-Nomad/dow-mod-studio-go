package testutil

import (
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"gioui.org/font/gofont"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// UnitTestTheme builds a material theme with a real font collection, without
// which every label in the tab would panic on a nil shaper.
func UnitTestTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return th
}

// DXP3Module is the real DXP3 module the integration tests read.
const DXP3Module = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`

// OpenDXP3 opens the real DXP3 module for a test, skipping the test when the
// game is not installed.
func OpenDXP3(t *testing.T) *modtree.ModTree {
	t.Helper()
	tree, err := modtree.BuildModTree(DXP3Module, "English")
	if err != nil {
		t.Skipf("DXP3 module not available: %v", err)
	}
	t.Cleanup(tree.Close)
	return tree
}

// TemplateEntries flattens a parsed rgd file's tree into a map keyed by the
// slash-joined path of every entry, which is how template tests address the
// entries they check.
func TemplateEntries(rf *rgd.RgdFile) map[string]*rgd.RgdEntry {
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
