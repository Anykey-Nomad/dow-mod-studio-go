package main

import "dow-mod-studio/internal/modtree"
import "testing"

func TestFindRgdReferenceFile(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	folder := tree.GetOrCreateFolder(tree.Root, "ebpextensions", nil)
	file := &modtree.TreeFile{
		Name:     "health_ext.lua",
		FullPath: `Data\ebpextensions\health_ext.lua`,
		Source:   &modtree.FileSource{ModName: "TestMod", Kind: modtree.KindThisMod},
	}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[file.Name] = file
	ui := &uiState{modTree: tree}

	for _, ref := range []string{
		`ebpextensions\health_ext.lua`,
		`ebpextensions/health_ext.lua`,
		`Data\ebpextensions\health_ext.lua`,
	} {
		if got := ui.findRgdReferenceFile(ref); got != file.FullPath {
			t.Errorf("reference %q resolved to %q, want %q", ref, got, file.FullPath)
		}
	}
	for _, ref := range []string{"object://something", "missing.lua", ""} {
		if got := ui.findRgdReferenceFile(ref); got != "" {
			t.Errorf("reference %q unexpectedly resolved to %q", ref, got)
		}
	}
}
