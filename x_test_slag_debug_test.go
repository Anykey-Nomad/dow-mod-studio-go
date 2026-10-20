package main

import (
	"fmt"
	"testing"

	"dow-mod-studio/internal/modtree"
)

func TestDebugSlagDepositMaterials(t *testing.T) {
	module := `/mnt/windows/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/AdvancedCampaign.module`
	tree, err := modtree.BuildModTree(module, "English")
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	modelPath := `Data\art\ebps\environment\gameplay\slag_deposit.whm`
	data, err := tree.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	model, err := ParseWhmModel(data)
	if err != nil {
		t.Fatalf("parse model: %v", err)
	}
	seen := map[string]bool{}
	for _, mesh := range model.Meshes {
		if mesh.Material == "" || seen[mesh.Material] {
			continue
		}
		seen[mesh.Material] = true
		fmt.Printf("MATERIAL %q\n", mesh.Material)
		for _, ext := range []string{".rsh", ".rtx", ".dds", ".tga"} {
			for _, candidate := range []string{mesh.Material + ext, `Data\\` + mesh.Material + ext} {
				if b, e := tree.ReadFile(candidate); e == nil {
					fmt.Printf("  exact %s: %d bytes\n", candidate, len(b))
				}
			}
			base := fxMaterialBaseForDebug(mesh.Material) + ext
			if f := tree.FindMaterialByName(base); f != nil {
				fmt.Printf("  basename %s: %s (%d bytes)\n", base, f.FullPath, func() int { b, _ := tree.ReadTreeFile(f); return len(b) }())
			}
		}
	}
	resolved := resolveModelMaterials(tree, materialSearchRoots(tree), modelPath, func() []string { var n []string; for name := range seen { n = append(n, name) }; return n }())
	fmt.Printf("RSH resolver returned %d of %d\n", len(resolved), len(seen))
}

func fxMaterialBaseForDebug(name string) string {
	for i := len(name)-1; i >= 0; i-- {
		if name[i] == '/' || name[i] == '\\' { return name[i+1:] }
	}
	return name
}
