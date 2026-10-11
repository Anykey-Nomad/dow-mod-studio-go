package main

import (
	"dow-mod-studio/internal/modtree"
	"os"
	"path/filepath"
	"testing"
)

// TestMaterialResolvesByNameAlone covers the case where a .whm names its maps
// with no folder, as the DXP3 Necron Lord does: "necron_lord01_src_b" rather
// than "art/ebps/races/necrons/texture_share/necron_lord01_src_b". A path
// lookup misses those, the model comes out untextured and the preview shows a
// flat white silhouette.
func TestMaterialResolvesByNameAlone(t *testing.T) {
	appPath := t.TempDir()
	modFolder := "TestMod"
	dataRoot := filepath.Join(appPath, modFolder, "Data")
	textures := filepath.Join(dataRoot, "art", "ebps", "races", "necrons", "texture_share")
	if err := os.MkdirAll(textures, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(textures, "necron_lord01_src_b.rsh"), []byte("rsh"), 0644); err != nil {
		t.Fatal(err)
	}

	// The same file also sits in the tree, the way an archived texture does.
	tree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		AppPath:     appPath,
		ModFolder:   modFolder,
		FileMapName: modFolder,
		Sources:     []*modtree.FileSource{{ModName: modFolder, Kind: modtree.KindThisMod, DiskRoot: dataRoot}},
	}
	folder := tree.GetOrCreateFolder(tree.GetOrCreateFolder(tree.Root, "art", nil), "texture_share", nil)
	archived := &modtree.TreeFile{Name: "necron_lord01_src_b.rsh", FullPath: `Data\art\texture_share\necron_lord01_src_b.rsh`}
	folder.Files = append(folder.Files, archived)
	folder.FileByLower[archived.Name] = archived

	roots := materialSearchRoots(tree)

	// The batch loader is what the preview actually calls, and it is the one
	// that falls back to searching the data folders, so that is what is
	// asked about a name the reference gives with no folder in it.
	names := []string{
		`art/ebps/races/necrons/texture_share/necron_lord01_src_b`, // full path
		`necron_lord01_src_b`, // bare name
		`art/ebps/races/necrons/texture_share/NECRON_LORD01_SRC_B`, // wrong case
	}
	files := resolveModelMaterials(tree, roots, "", names)
	if len(files) != len(names) {
		t.Errorf("%d of %d materials were found, they are all in the mod", len(files), len(names))
	}
	for _, name := range names {
		data, ok := files[name]
		if !ok {
			t.Errorf("no .rsh was resolved for %q", name)
			continue
		}
		if string(data) != "rsh" {
			t.Errorf("the file resolved for %q is %q, want %q", name, data, "rsh")
		}
	}

	if _, ok := readModelMaterial(tree, roots, "", "no_such_texture"); ok {
		t.Error("readModelMaterial invented a texture that is not there")
	}
}

// A model and the maps it uses are usually in the same folder, and the mod
// being worked on wins over the game it is built on. Both of those are the
// order a texture has to be looked up in, and a preview that skips them shows
// a model in flat white.
func TestAMaterialIsFoundWhereTheModelLives(t *testing.T) {
	dir := t.TempDir()
	dataRoot := filepath.Join(dir, "Data")
	beside := filepath.Join(dataRoot, "art", "ebps", "races", "necrons", "structures")
	elsewhere := filepath.Join(dataRoot, "art", "ebps", "races", "necrons", "texture_share")
	if err := os.MkdirAll(beside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(elsewhere, 0755); err != nil {
		t.Fatal(err)
	}
	modelFile := `art\ebps\races\necrons\structures\necron_thing`
	if err := os.WriteFile(filepath.Join(beside, "necron_thing.rsh"), []byte("beside"), 0644); err != nil {
		t.Fatal(err)
	}
	// The same name elsewhere, so a lookup that stops at the first folder it
	// finds the file in rather than the model's own would take this one.
	if err := os.WriteFile(filepath.Join(elsewhere, "necron_thing.rsh"), []byte("elsewhere"), 0644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		AppPath:     dir,
		ModFolder:   "TestMod",
		FileMapName: "TestMod",
		Sources:     []*modtree.FileSource{{ModName: "TestMod", Kind: modtree.KindThisMod, DiskRoot: dataRoot}},
	}
	roots := materialSearchRoots(tree)

	files := resolveModelMaterials(tree, roots, modelFile, []string{"necron_thing"})
	if got := string(files["necron_thing"]); got != "beside" {
		t.Errorf("the bare name resolved to %q, want the copy beside the model (%q)", got, "beside")
	}

	// And the mod's own folder is ahead of the data folder the game keeps its
	// art in, so a mod can replace a texture by putting its own next to it.
	modFolder := filepath.Join(dir, "TestMod", "Data", "art", "ebps", "races", "necrons", "structures")
	if err := os.MkdirAll(modFolder, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modFolder, "necron_thing.rsh"), []byte("mod"), 0644); err != nil {
		t.Fatal(err)
	}
	modTree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		AppPath:     dir,
		ModFolder:   "TestMod",
		FileMapName: "TestMod",
		Sources: []*modtree.FileSource{
			{ModName: "TestMod", Kind: modtree.KindThisMod, DiskRoot: filepath.Join(dir, "TestMod", "Data")},
			{ModName: "DXP3", Kind: modtree.KindReqMod, DiskRoot: dataRoot},
		},
	}
	roots = materialSearchRoots(modTree)
	if roots[0] != filepath.Clean(filepath.Join(dir, "TestMod")) {
		t.Errorf("the mod folder is not searched first: %v", roots)
	}
	files = resolveModelMaterials(modTree, roots, modelFile, []string{"necron_thing"})
	if got := string(files["necron_thing"]); got != "mod" {
		t.Errorf("the bare name resolved to %q, want the mod's own copy", got)
	}
}

// TestFindByNamePrefersTextureFolders pins the tie break. Several folders hold
// a file of the same name, and the one a model's material refers to is the copy
// in a texture_share folder.
func TestFindByNamePrefersTextureFolders(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	models := tree.GetOrCreateFolder(tree.Root, "models", nil)
	textures := tree.GetOrCreateFolder(tree.Root, "texture_share", nil)

	add := func(folder *modtree.TreeFolder, name, full string) {
		f := &modtree.TreeFile{Name: name, FullPath: full}
		folder.Files = append(folder.Files, f)
		folder.FileByLower[name] = f
	}
	add(models, "map.rsh", `Data\models\map.rsh`)
	add(textures, "map.rsh", `Data\texture_share\map.rsh`)

	got := tree.FindByName("map.rsh")
	if got == nil {
		t.Fatal("FindByName found nothing")
	}
	if got.FullPath != `Data\texture_share\map.rsh` {
		t.Errorf("FindByName returned %q, want the texture_share copy", got.FullPath)
	}
	if tree.FindByName("MAP.RSH") == nil {
		t.Error("FindByName is case sensitive, the game is not")
	}
}
