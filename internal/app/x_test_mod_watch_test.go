package app

import (
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentModSnapshotDetectsFileChanges(t *testing.T) {
	root := t.TempDir()
	modRoot := filepath.Join(root, "TestMod")
	if err := os.MkdirAll(modRoot, 0755); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		FileMapName: "TestMod",
		ModFolder:   "TestMod",
		AppPath:     root,
		Sources: []*modtree.FileSource{{
			ModName:    "TestMod",
			SourceName: "Data",
			Kind:       modtree.KindThisMod,
			DiskRoot:   modRoot,
		}},
	}
	before := currentModSnapshot(tree)
	if before == "" {
		t.Fatal("expected non-empty initial snapshot")
	}
	if err := os.WriteFile(filepath.Join(modRoot, "new.lua"), []byte("return true"), 0644); err != nil {
		t.Fatal(err)
	}
	after := currentModSnapshot(tree)
	if before == after {
		t.Fatal("expected snapshot to change after adding a mod file")
	}

	external := filepath.Join(root, "ModTools", "DataGeneric", "TestMod")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatal(err)
	}
	tree.Sources = append(tree.Sources, &modtree.FileSource{
		ModName: "TestMod", SourceName: "DataGeneric", Kind: modtree.KindThisMod, DiskRoot: external,
	})
	beforeExternal := currentModSnapshot(tree)
	if err := os.WriteFile(filepath.Join(external, "ignored.lua"), []byte("return true"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := currentModSnapshot(tree); got != beforeExternal {
		t.Fatal("external DataGeneric changes must not trigger current-mod catalog polling")
	}
}
