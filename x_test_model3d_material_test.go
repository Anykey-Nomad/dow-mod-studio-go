package main

import (
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"image/color"
	"os"
	"path/filepath"
	"strings"
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

func TestFindMaterialByNameUsesPriorityAcrossModuleSources(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	parent := tree.GetOrCreateFolder(tree.Root, "parent", nil)
	mod := tree.GetOrCreateFolder(tree.Root, "mod", nil)
	parentTexture := tree.GetOrCreateFolder(parent, "texture_share", nil)
	modTexture := tree.GetOrCreateFolder(mod, "texture_share", nil)
	parentFile := &modtree.TreeFile{Name: "wire.rsh", Source: &modtree.FileSource{Priority: 15000}}
	modFile := &modtree.TreeFile{Name: "wire.rsh", Source: &modtree.FileSource{Priority: 0}}
	parentTexture.Files = append(parentTexture.Files, parentFile)
	modTexture.Files = append(modTexture.Files, modFile)

	if got := tree.FindMaterialByName("WIRE.RSH"); got != modFile {
		t.Fatalf("material lookup returned %#v, want current-mod override (parent was %#v)", got, parentFile)
	}
}

func TestFindMaterialsInTreeChoosesTextureShareAndHighestPriority(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	plain := tree.GetOrCreateFolder(tree.Root, "models", nil)
	parentTexture := tree.GetOrCreateFolder(tree.Root, "parent", nil)
	parentTexture = tree.GetOrCreateFolder(parentTexture, "texture_share", nil)
	modTexture := tree.GetOrCreateFolder(tree.Root, "mod", nil)
	modTexture = tree.GetOrCreateFolder(modTexture, "texture_share", nil)
	add := func(folder *modtree.TreeFolder, source *modtree.FileSource, path string) *modtree.TreeFile {
		file := &modtree.TreeFile{Name: "wire.rsh", FullPath: path, Source: source}
		folder.Files = append(folder.Files, file)
		return file
	}
	add(plain, &modtree.FileSource{Priority: 1}, `Data\models\wire.rsh`)
	add(parentTexture, &modtree.FileSource{Priority: 15000}, `Data\parent\texture_share\wire.rsh`)
	modFile := add(modTexture, &modtree.FileSource{Priority: 0}, `Data\mod\texture_share\wire.rsh`)

	got := findMaterialsInTree(tree, []string{"art/units/texture_share/wire", "WIRE"})
	if got["WIRE"] != modFile || got[`art/units/texture_share/wire`] != modFile {
		t.Fatalf("tree lookup chose wrong RSH files: %#v", got)
	}
}

func TestDecodeModelMaterialsKeepsCompressedRSHForVulkan(t *testing.T) {
	data := syntheticRSHDXT1(t)
	got := decodeModelMaterials(map[string][]byte{"map": data}, true)
	if len(got.rsh) != 1 || len(got.materials) != 1 {
		t.Fatalf("Vulkan material result = %d raw, %d decoded, want both upload and preview data", len(got.rsh), len(got.materials))
	}
	if string(got.rsh["map"]) != string(data) {
		t.Fatal("raw RSH bytes were changed before upload")
	}
	if got.materials["map"].Texture == nil {
		t.Fatal("Vulkan material has no decoded view for the texture inspector")
	}
	if got.missing != 0 {
		t.Fatalf("Vulkan material marked %d files missing", got.missing)
	}
}

func TestVulkanFragmentAlphaTestsRSHAlpha(t *testing.T) {
	data := makeRSHWithFormat(11, solidDXT5AlphaBlock())
	got := decodeModelMaterials(map[string][]byte{"wire": data}, true)
	if len(got.rsh) != 1 || got.materials["wire"].Texture == nil {
		t.Fatal("test texture did not load for Vulkan")
	}
	shader, err := os.ReadFile(filepath.Join("shaders", "model3d_vk.frag"))
	if err != nil {
		t.Fatal(err)
	}
	// Cutout transparency is an alpha test against the Shade.z reference:
	// models stay on the opaque pipeline with depth writes, so a mesh never
	// shows its own interior, and the wire's transparent texels are gone
	// before either pipeline's blend state matters.
	if !strings.Contains(string(shader), "if (sampleColor.a < d.Shade.z) discard") ||
		!strings.Contains(string(shader), "fragColor = vec4(col * alpha, alpha)") {
		t.Fatal("Vulkan fragment shader must alpha-test texture cutouts against the Shade.z reference")
	}
	decoded, _, err := raster.DecodeRshImage(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if gotPixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA); gotPixel.A != 0 {
				t.Fatalf("texture alpha at (%d,%d) decoded as %d, want 0", x, y, gotPixel.A)
			}
		}
	}
}

func solidDXT5AlphaBlock() []byte {
	block := make([]byte, 16)
	block[0] = 0
	block[1] = 255
	// Every pixel selects alpha endpoint a0=0; set all sixteen 3-bit fields.
	// The index bits are one contiguous little-endian 48-bit sequence.
	block[2], block[3], block[4], block[5], block[6], block[7] = 0, 0, 0, 0, 0, 0
	// White color endpoints, every pixel selects the first.
	block[8], block[9] = 0xff, 0xff
	block[10], block[11] = 0, 0
	return block
}

func makeRSHWithFormat(format int32, pixels []byte) []byte {
	attr := []byte{byte(format), 0, 0, 0, 4, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0}
	imageChunk := &chunky.Chunk{TypeID: "FOLDIMAG", Children: []*chunky.Chunk{
		{TypeID: "DATAATTR", Payload: attr},
		{TypeID: "DATADATA", Payload: pixels},
	}}
	texture := &chunky.Chunk{TypeID: "FOLDTXTR", Children: []*chunky.Chunk{imageChunk}}
	root := &chunky.Chunk{TypeID: "FOLDSHRF", Children: []*chunky.Chunk{texture}}
	return append(chunky.Preamble(), chunky.Serialize(root)...)
}

func TestLoadWhmGroundImageFindsTreeBasename(t *testing.T) {
	data := make([]byte, 18+3)
	data[2] = 2
	data[16] = 24
	data[17] = 0x20
	data[12], data[14] = 1, 1
	data[18], data[19], data[20] = 40, 30, 20
	root := modtree.NewFolder("Data", "Data", nil)
	tree := &modtree.ModTree{Root: root}
	folder := tree.GetOrCreateFolder(root, "scenarios", nil)
	source := &modtree.FileSource{ModName: "parent", Priority: 15000}
	file := &modtree.TreeFile{Name: filepath.Base(unitGroundTexture), FullPath: `Data\\scenarios\\jungle_ground_02.tga`, Source: source, DiskPath: filepath.Join(t.TempDir(), filepath.Base(unitGroundTexture))}
	if err := os.WriteFile(file.DiskPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[strings.ToLower(file.Name)] = file

	img := loadWhmGroundImage(tree)
	if img == nil {
		t.Fatal("ground image was not found by basename in the parent source tree")
	}
	if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got != (color.NRGBA{R: 20, G: 30, B: 40, A: 255}) {
		t.Fatalf("loaded ground pixel = %#v", got)
	}
}

func TestDecodeModelMaterialsRejectsMalformedRSH(t *testing.T) {
	got := decodeModelMaterials(map[string][]byte{"map": []byte("invalid")}, true)
	if len(got.rsh) != 0 || got.missing != 1 {
		t.Fatalf("malformed RSH result = raw:%d missing:%d", len(got.rsh), got.missing)
	}
}

func syntheticRSHDXT1(t *testing.T) []byte {
	t.Helper()
	imageChunk := &chunky.Chunk{TypeID: "FOLDIMAG", Children: []*chunky.Chunk{
		{TypeID: "DATAATTR", Payload: []byte{
			8, 0, 0, 0, // DXT1
			4, 0, 0, 0, // width
			4, 0, 0, 0, // height
			1, 0, 0, 0, // one mip
		}},
		{TypeID: "DATADATA", Payload: []byte{0, 0xf8, 0, 0, 0, 0, 0, 0}},
	}}
	texture := &chunky.Chunk{TypeID: "FOLDTXTR", Children: []*chunky.Chunk{imageChunk}}
	root := &chunky.Chunk{TypeID: "FOLDSHRF", Children: []*chunky.Chunk{texture}}
	data := chunky.Preamble()
	return append(data, chunky.Serialize(root)...)
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
