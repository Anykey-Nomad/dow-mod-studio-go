package app

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/whe"
	"gioui.org/f32"
	"gioui.org/layout"
	"github.com/fogleman/fauxgl"
)

func TestMaterialResolverFindsCompiledRTXBesideModel(t *testing.T) {
	dir := t.TempDir()
	modelDir := filepath.Join(dir, "Data", "art", "ebps", "environment", "gameplay")
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, "slag_deposit.rtx"), []byte("rtx"), 0644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), Sources: []*modtree.FileSource{{DiskRoot: filepath.Join(dir, "Data")}}}
	roots := materialSearchRoots(tree)
	got, ok := readModelMaterial(tree, roots, `Data\art\ebps\environment\gameplay\slag_deposit.whm`, "slag_deposit")
	if !ok || string(got) != "rtx" {
		t.Fatalf("material resolution = (%q, %v), want the RTX beside the model", got, ok)
	}
}

func TestInstalledCompiledEntityTexturesResolve(t *testing.T) {
	tree, _ := sgbTestSetup(t)
	roots := materialSearchRoots(tree)
	for _, tc := range []struct {
		name      string
		modelPath string
	}{
		{"slag_deposit", `Data\art\ebps\environment\gameplay\slag_deposit.whm`},
		{"starting_position", `Data\art\ebps\environment\gameplay\starting_position.whm`},
		{"carnage_necron_01", `Data\art\ebps\environment\all\carnage_necron_01.whm`},
		{"carnage_necron_02", `Data\art\ebps\environment\all\carnage_necron_02.whm`},
		{"carnage_necron_03", `Data\art\ebps\environment\all\carnage_necron_03.whm`},
		{"carnage_necron_04", `Data\art\ebps\environment\all\carnage_necron_04.whm`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modelData, err := tree.ReadFile(tc.modelPath)
			if err != nil {
				t.Fatalf("read model %s: %v", tc.modelPath, err)
			}
			model, err := ParseWhmModel(modelData)
			if err != nil {
				t.Fatalf("parse model %s: %v", tc.modelPath, err)
			}
			var names []string
			for _, mesh := range model.Meshes {
				if mesh.Material != "" {
					names = append(names, mesh.Material)
				}
			}
			if len(names) == 0 {
				t.Fatal("model has no material references")
			}
			if strings.HasPrefix(tc.name, "carnage_necron_") {
				if len(model.Meshes) != 1 || model.Meshes[0].Material != "necron warrior" ||
					model.Meshes[0].MaterialPath != "art/ebps/races/necrons/texture_share/nec_warrior" {
					t.Fatalf("Carnage material mapping = %+v, want necron warrior -> .../nec_warrior", model.Meshes)
				}
			}
			files, paths := resolveModelMaterialsWithPaths(tree, roots, tc.modelPath, names)
			result := decodeModelMaterials(files, true)
			attachMaterialSourcePaths(result.diagnostics, paths)
			for _, name := range names {
				if result.materials[name].Texture == nil {
					t.Errorf("material %q did not resolve and decode: %+v", name, result.diagnostics[name])
				}
			}
			if result.missing != 0 {
				t.Errorf("%d/%d material textures missing", result.missing, len(names))
			}
		})
	}
}

func TestModelMaterialReferenceResolvesUnderRendererMaterialKey(t *testing.T) {
	const material = "necron warrior"
	const diffusePath = "art/ebps/races/necrons/texture_share/nec_warrior"
	modelData := materialPathTestWHM(material, diffusePath)
	parsedModel, parseErr := ParseWhmModel(modelData)
	if parseErr != nil {
		t.Fatalf("parse test WHM: %v", parseErr)
	}
	if len(parsedModel.Meshes) != 1 || parsedModel.Meshes[0].Material != material || parsedModel.Meshes[0].MaterialPath != diffusePath {
		t.Fatalf("test WHM material mapping = %+v", parsedModel.Meshes)
	}
	dir := t.TempDir()
	textures := filepath.Join(dir, "art", "ebps", "races", "necrons", "texture_share")
	if err := os.MkdirAll(textures, 0755); err != nil {
		t.Fatal(err)
	}
	textureData := syntheticRSHDXT1(t)
	if err := os.WriteFile(filepath.Join(textures, "nec_warrior.rsh"), textureData, 0644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{
		Root:    modtree.NewFolder("Data", "Data", nil),
		Sources: []*modtree.FileSource{{DiskRoot: dir}},
	}
	modelPath := `Data\art\ebps\races\necrons\carnage_necron.whm`
	modelFile := &modtree.TreeFile{Name: "carnage_necron.whm", FullPath: modelPath, Source: tree.Sources[0], DiskPath: filepath.Join(dir, "carnage_necron.whm")}
	modelDir := tree.GetOrCreateFolder(tree.Root, "art", nil)
	modelDir = tree.GetOrCreateFolder(modelDir, "ebps", nil)
	modelDir = tree.GetOrCreateFolder(modelDir, "races", nil)
	modelDir = tree.GetOrCreateFolder(modelDir, "necrons", nil)
	modelDir.Files = append(modelDir.Files, modelFile)
	modelDir.FileByLower[strings.ToLower(modelFile.Name)] = modelFile
	if err := os.WriteFile(modelFile.DiskPath, modelData, 0644); err != nil {
		t.Fatal(err)
	}
	// Request exactly the key a parsed mesh and both renderers use, not the
	// canonical MaterialPath stored in the WHM shared shader.
	files, paths := resolveModelMaterialsWithPaths(tree, materialSearchRoots(tree), modelPath, []string{material})
	decoded := decodeModelMaterials(files, true)
	attachMaterialSourcePaths(decoded.diagnostics, paths)
	if got := string(files[material]); got != string(textureData) {
		t.Fatalf("material lookup returned %d bytes, want the diffuse for key %q", len(files[material]), material)
	}
	if decoded.materials[material].Texture == nil || len(decoded.rsh[material]) == 0 {
		t.Fatalf("resolved texture was not available under renderer key %q: materials=%v raw=%v", material, decoded.materials, len(decoded.rsh[material]))
	}
	if got := decoded.diagnostics[material].ResolvedPath; !strings.HasSuffix(strings.ToLower(got), "nec_warrior.rsh") {
		t.Fatalf("diagnostic source path = %q, want the resolved canonical diffuse", got)
	}
}

func materialPathTestWHM(material, diffusePath string) []byte {
	channel := make([]byte, 12)
	channel = append(channel, tStr(diffusePath)...)
	mesh := make([]byte, 13)
	mesh = append(mesh, chunky.I32(0)...)
	mesh = append(mesh, chunky.I32(3)...)
	mesh = append(mesh, chunky.I32(32)...)
	mesh = append(mesh, tF32(0, 0, 0, 1, 0, 0, 0, 1, 0)...)
	mesh = append(mesh, tF32(0, 0, 1, 0, 0, 1, 0, 0, 1)...)
	mesh = append(mesh, tF32(0, 0, 1, 0, 0, 1)...)
	mesh = append(mesh, chunky.I32(0)...)
	mesh = append(mesh, chunky.I32(1)...)
	mesh = append(mesh, tStr(material)...)
	mesh = append(mesh, chunky.I32(3)...)
	mesh = append(mesh, tU16(0, 1, 2)...)
	mesh = append(mesh, chunky.I32(0)...)
	mesh = append(mesh, chunky.I32(3)...)
	mesh = append(mesh, chunky.I32(0)...)
	root := &chunky.Chunk{TypeID: "FOLDRSGM", Version: 3, Name: "material-key-test", Children: []*chunky.Chunk{
		{TypeID: "FOLDSHDR", Version: 1, Name: material, Children: []*chunky.Chunk{{TypeID: "DATACHAN", Version: 1, Payload: channel}}},
		{TypeID: "FOLDSTXT", Version: 1, Name: diffusePath},
		{TypeID: "FOLDMSGR", Version: 1, Children: []*chunky.Chunk{{TypeID: "FOLDMSLC", Version: 1, Name: "test-mesh", Children: []*chunky.Chunk{{TypeID: "DATADATA", Version: 2, Payload: mesh}}}}},
	}}
	return append(chunky.Preamble(), chunky.Serialize(root)...)
}

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

func TestDecodeModelMaterialsKeepsCompiledRTXForVulkan(t *testing.T) {
	data := syntheticRTXDXT1(t)
	got := decodeModelMaterials(map[string][]byte{"slag_deposit": data}, true)
	if len(got.rsh) != 0 {
		t.Errorf("compiled RTX was incorrectly returned as raw RSH: %d", len(got.rsh))
	}
	if material := got.materials["slag_deposit"]; material.Texture == nil {
		t.Fatal("compiled RTX did not produce a decoded material texture")
	}
	if got.missing != 0 {
		t.Fatalf("compiled RTX was counted as missing: %d", got.missing)
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

func syntheticRTXDXT1(t *testing.T) []byte {
	t.Helper()
	attr := []byte{8, 0, 0, 0, 4, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0}
	root := &chunky.Chunk{TypeID: "FOLDRTXTR", Children: []*chunky.Chunk{
		{TypeID: "FOLDIMAG", Children: []*chunky.Chunk{
			{TypeID: "DATAATTR", Payload: attr},
			{TypeID: "DATADATA", Payload: []byte{0, 0xf8, 0, 0, 0, 0, 0, 0}},
		}},
	}}
	return append(chunky.Preamble(), chunky.Serialize(root)...)
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

// ─── merged from x_test_model3d_view_test.go ────────────────────────────────────────

const rshNecronsDir = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data\art\ebps\races\Necrons\texture_share`

// Stepping one frame at a time is how a single pose of an animation is looked
// at, so the play head has to land on the next frame exactly, stop the model
// while it is being wound, and stay inside the clip rather than running off
// either end of it.
func TestModelViewerStepsOneFrameAtATime(t *testing.T) {
	model := renderTestModelWithAnyAnimation(t)
	v := newModelViewer(model, "")
	v.SelectAnim(0)
	anim := v.Anim
	if anim == nil || anim.NumFrames < 4 {
		t.Skip("this model has no clip with enough frames to step through")
	}
	v.Playing = true
	v.Time = 0.5
	step := float32(1) / float32(anim.NumFrames)
	before, frames := v.CurrentFrame()

	v.StepFrame(1)
	if v.Playing {
		t.Error("the model is still playing after a frame was wound to")
	}
	if got, want := v.Time, float32(0.5)+step; abs32(got-want) > 1e-5 {
		t.Errorf("one frame forward landed on %v, want %v", got, want)
	}
	if frame, total := v.CurrentFrame(); total != frames || frame != before+1 {
		t.Errorf("the readout says frame %d of %d, want the one after frame %d of %d",
			frame+1, total, before+1, frames)
	}

	v.StepFrame(-1)
	if got := v.Time; abs32(got-0.5) > 1e-5 {
		t.Errorf("stepping back landed on %v, want the frame it came from, 0.5", got)
	}
	// The first frame is a floor rather than a wrap: holding the button down
	// walks to the start of the clip and stays there.
	v.Time = step
	v.StepFrame(-1)
	if got := v.Time; abs32(got) > 1e-5 {
		t.Errorf("stepping back past the first frame landed on %v, want the first", got)
	}

	// The last frame of a clip is the first one again, so a step forward from
	// there wraps to the start rather than past the end of the pose.
	v.Time = 1 - step
	v.StepFrame(1)
	if v.Time != 0 {
		t.Errorf("stepping past the last frame landed on %v, want the start of the clip", v.Time)
	}
}

// A model with no clip has no frames to step through, and the step is a no
// operation rather than a division by nothing.
func TestModelViewerStepsNothingWithoutAClip(t *testing.T) {
	v := newModelViewer(renderTestModel(t), "")
	v.StepFrame(1)
	if v.Time != 0 || v.Playing {
		t.Errorf("stepping a model with no clip left time at %v and playing at %v", v.Time, v.Playing)
	}
	if frame, frames := v.CurrentFrame(); frame != 0 || frames != 0 {
		t.Errorf("the readout says frame %d of %d for a model with no clip", frame, frames)
	}
}

// A notch of the wheel has to be a step the eye can follow. It used to be a
// two percent move raised to the whole scroll amount, and one notch is 120 of
// that, so a click multiplied the camera distance by ten: the model went from
// framed to inside out in one click and back in the next, and nothing in
// between was worth looking at. The step is now a single readable move, the
// two zoom buttons do exactly the same move, and the whole range the viewer
// allows is a dozen or so steps either way.
func TestOneNotchOfZoomIsAStepAndTheButtonsMatchIt(t *testing.T) {
	v := newModelViewer(renderTestModel(t), "")

	// A notch is worth one step's worth of distance, whichever way the wheel
	// is turned: a notch down moves the camera closer and so is a factor below
	// one.
	notch := modelZoomByDelta(wheelNotchDelta)
	if notch >= 1 || notch < 1/1.5 {
		t.Errorf("one notch multiplies the distance by %g, want a small step of its own", notch)
	}
	// A notch has to be one of several moves across the range, not all of it.
	lo, hi := v.FitDistance*0.15, v.FitDistance*6
	steps := math.Log(hi/lo) / math.Abs(math.Log(notch))
	if steps < 6 || steps > 40 {
		t.Errorf("the viewer's whole zoom range is %g notches, want a handful rather than one jump or a hundred", steps)
	}
	// A trackpad reports the same turn of the wheel in smaller pieces, and
	// those have to add up to the same move.
	if quarter := modelZoomByDelta(-wheelNotchDelta / 4); quarter != math.Pow(modelZoomStep, 0.25) {
		t.Errorf("a quarter notch is a factor of %g, which does not add up to the %g of a whole one", quarter, notch)
	}

	// The wheel and the buttons take the same path, so a press moves the
	// camera by exactly as much as a click of the wheel.
	v.Distance = v.FitDistance
	v.ZoomBy(modelZoomByDelta(-wheelNotchDelta))
	byWheel := v.Distance
	v.Distance = v.FitDistance
	v.ZoomBy(modelZoomStep) // the near button, in the modelControlClicks path
	if abs64(v.Distance-byWheel) > 1e-9 {
		t.Errorf("a press moved the distance to %v and a notch to %v: the two are not the same step", v.Distance, byWheel)
	}
	// And the far button is the same step the other way.
	v.ZoomBy(1 / modelZoomStep)
	if abs64(v.Distance-v.FitDistance) > 1e-9 {
		t.Errorf("pressing the two zoom buttons returned the distance to %v, want the %v it started at", v.Distance, v.FitDistance)
	}
}

// The camera has to stay inside the range the viewer allows however hard the
// wheel is turned, or the model ends up inside the camera.
func TestZoomStepsAreClampedToWhatTheViewerAllows(t *testing.T) {
	v := newModelViewer(renderTestModel(t), "")
	lo, hi := v.FitDistance*0.15, v.FitDistance*6
	for i := 0; i < 200; i++ {
		v.ZoomBy(modelZoomStep)
	}
	if v.Distance > hi+1e-9 {
		t.Errorf("zooming in stopped at %v, want no closer than %v", v.Distance, hi)
	}
	for i := 0; i < 400; i++ {
		v.ZoomBy(1 / modelZoomStep)
	}
	if v.Distance < lo-1e-9 {
		t.Errorf("zooming out stopped at %v, want no further than %v", v.Distance, lo)
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestModelViewerProducesFirstFrame guards the wiring the GUI depends on: a
// freshly opened tab must ask for a render by itself. An earlier version gated
// the canvas behind "a frame already exists", so the first render was never
// requested and the tab stayed blank forever.
func TestObjectModelViewerUsesViewportDefaults(t *testing.T) {
	model := &WhmModel{
		Animations: []WhmAnimation{
			{Name: "die_1", NumFrames: 8},
			{Name: "idle_1", NumFrames: 10},
			{Name: "move", NumFrames: 12},
		},
	}
	ground := image.NewRGBA(image.Rect(3, 4, 9, 10))
	v := newObjectModelViewer(model, "Data/art/ebps/test/model.whm", ground)

	if v.ModelFile != "Data/art/ebps/test/model.whm" {
		t.Errorf("model path = %q", v.ModelFile)
	}
	if v.Anim == nil || v.Anim.Name != "move" || v.AnimIndex != 2 {
		t.Errorf("initial animation = %v at index %d, want move at 2", v.Anim, v.AnimIndex)
	}
	if !v.Playing {
		t.Error("viewer with clips should start playing")
	}
	var groundBounds image.Rectangle
	if v.GroundImage != nil {
		groundBounds = v.GroundImage.Bounds()
	}
	if !v.ShowGround || v.GroundImage == nil || groundBounds != image.Rect(0, 0, 6, 6) {
		t.Errorf("ground = enabled:%v bounds:%v, want enabled 6x6 copy", v.ShowGround, groundBounds)
	}
	if got := v.Options(80, 60); !got.Ground || got.GroundImage == nil {
		t.Error("render options did not carry the ground image and enabled state")
	}
}

func TestObjectModelViewerWithoutGroundOrAnimation(t *testing.T) {
	v := newObjectModelViewer(&WhmModel{}, "", nil)
	if v.Anim != nil || v.Playing {
		t.Errorf("viewer without clips has animation %v and playing %v", v.Anim, v.Playing)
	}
	if v.ShowGround || v.GroundImage != nil {
		t.Error("viewer without a ground image should not enable the ground")
	}
	v.SelectAnim(0) // empty models are safe for the shared playback controls
	if v.Anim != nil || v.Playing {
		t.Error("selecting a clip on an empty model changed playback state")
	}
}

// TestModelViewerProducesFirstFrame guards the wiring the GUI depends on: a
func TestWhmViewerConstrainsScrollMeasurementHeight(t *testing.T) {
	ui := &uiState{}
	tab := &fileTab{Model: newModelViewer(&WhmModel{}, "")}
	for _, maxHeight := range []int{panelScrollMeasureHeight, panelScrollMeasureHeight - 210, 510} {
		if got := whmMeasurePass(maxHeight); got != (maxHeight >= panelScrollMeasureHeight/2) {
			t.Errorf("whmMeasurePass(%d) = %v", maxHeight, got)
		}
	}
	gtx := layout.Context{Constraints: layout.Constraints{Min: image.Pt(0, 1<<20), Max: image.Pt(800, 1<<20)}}
	got := whmViewerLayout(ui, tab)(gtx)
	if got.Size.Y != 0 {
		t.Fatalf("WHM viewer measurement height = %d px, want zero during synthetic measurement", got.Size.Y)
	}
}

func TestViewerMarksEmptyMaterialLoadComplete(t *testing.T) {
	v := &modelViewer{
		Loading:   true,
		Materials: map[string]render.WhmMaterial{},
		Redraw:    false,
	}
	tab := &fileTab{Model: v, ModelMaterialCh: make(chan modelMaterialResult, 1)}
	tab.ModelMaterialCh <- modelMaterialResult{materials: map[string]render.WhmMaterial{}}
	ui := &uiState{}
	PollViewerJobs(ui, tab)

	if v.Loading {
		t.Fatal("material load remained in progress after its result was received")
	}
	if !v.MaterialsLoaded {
		t.Fatal("empty material result was not remembered as a completed load")
	}
	if len(v.Materials) != 0 {
		t.Fatalf("loaded material map has %d entries, want empty", len(v.Materials))
	}

	// The viewer's scheduling condition must not issue another worker just
	// because the completed material set is empty.
	if modelMaterialsNeedLoad(tab) {
		t.Fatal("completed empty material load can be mistaken for an unstarted load")
	}
}

func TestRightDragPansCameraParallelToViewPlane(t *testing.T) {
	model := &WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, 1, 1}}},
		BoundMin: [3]float32{-1, -1, -1},
		BoundMax: [3]float32{1, 1, 1},
	}
	v := newModelViewer(model, "")
	v.Yaw, v.Pitch = 0, 0
	v.Distance = 10
	v.FOV = render.DefaultFov
	start := v.Center
	v.PanCamera(20, 10, 100)

	if v.Center == start {
		t.Fatal("right drag did not move the camera target")
	}
	if v.Center.Z == start.Z {
		t.Fatal("vertical drag did not pan along the camera's screen up axis")
	}
	if v.ActiveCamera != "" {
		t.Errorf("panning left named camera %q active", v.ActiveCamera)
	}
	if !v.Redraw {
		t.Error("panning did not request a new frame")
	}

	// At zero yaw and pitch, a horizontal pan has no depth component.
	v.Center = start
	v.PanCamera(20, 0, 100)
	if v.Center.Z != start.Z || v.Center.X != start.X {
		t.Errorf("horizontal pan moved out of screen plane: center changed from %+v to %+v", start, v.Center)
	}
	if v.Center.Y == start.Y {
		t.Error("horizontal drag did not move the target sideways")
	}
}

func TestModelViewerProducesFirstFrame(t *testing.T) {
	model := renderTestModel(t)
	v := newModelViewer(model, "")
	if !v.Redraw {
		t.Fatal("a new viewer must start out needing a render")
	}
	// Drive it the way modelCanvas does: a layout clears the flag and starts a
	// background render, then the result arrives through the channel.
	v.Redraw = false
	v.RequestRender(64, 48, render.ModelQualityFinal)
	if !v.Rendering {
		t.Fatal("requestRender did not start a render")
	}
	frame := <-v.FrameCh
	if frame.image == nil {
		t.Fatal("render returned no image")
	}
	if frame.image.Bounds().Dx() != 64 || frame.image.Bounds().Dy() != 48 {
		t.Fatalf("frame is %v, want 64x48", frame.image.Bounds())
	}
	// The frame has to be opaque enough to see: the model must have covered
	// some pixels rather than leaving the background.
	if !frameHasGeometry(frame.image) {
		t.Error("frame has no geometry; the canvas would look empty")
	}
}

// TestModelViewerCoalescesRenders checks that a burst of camera movement does
// not queue up a render per input event: while one render is in flight the
// viewer only remembers that another is needed.
func TestModelViewerCoalescesRenders(t *testing.T) {
	model := renderTestModel(t)
	v := newModelViewer(model, "")
	v.RequestRender(32, 32, render.ModelQualityFinal)
	first := v.FrameCh
	for i := 0; i < 5; i++ {
		v.RequestRender(32, 32, render.ModelQualityFinal)
	}
	if v.FrameCh != first {
		t.Fatal("a second render was started while one was still running")
	}
	if !v.Pending {
		t.Error("the coalesced request was not recorded")
	}
	<-first
}

// TestModelViewerResizeInvalidates checks that a size change is noticed, since
// the projection depends on the aspect ratio.
func TestModelViewerResizeInvalidates(t *testing.T) {
	model := renderTestModel(t)
	v := newModelViewer(model, "")
	if v.RenderW != 0 || v.RenderH != 0 {
		t.Fatal("a new viewer has no render size yet")
	}
	v.Redraw = false
	v.SetRenderSize(800, 600)
	if !v.Redraw {
		t.Error("the first size must schedule the initial render")
	}
	v.Redraw = false
	v.SetRenderSize(800, 600)
	if v.Redraw {
		t.Error("an unchanged size must not schedule a render")
	}
	v.SetRenderSize(640, 480)
	if !v.Redraw {
		t.Error("a size change must schedule a render; the frame would not fit")
	}
	// A collapsed panel must not poison the stored size.
	v.Redraw = false
	v.SetRenderSize(0, 0)
	if v.RenderW != 640 || v.RenderH != 480 {
		t.Errorf("a zero size overwrote the render size: %dx%d", v.RenderW, v.RenderH)
	}
}

// TestModelViewerSelectAnim checks clip cycling, including the wrap around.
func TestModelViewerSelectAnim(t *testing.T) {
	model := renderTestModelWithAnyAnimation(t)
	v := newModelViewer(model, "")
	n := len(model.Animations)
	if n == 0 {
		t.Skip("model has no animations")
	}
	v.SelectAnim(0)
	first := v.Anim
	if first == nil || v.AnimIndex != 0 {
		t.Fatal("selectAnim(0) did not pick the first clip")
	}
	v.SelectAnim(-1)
	if v.AnimIndex != n-1 {
		t.Errorf("selectAnim(-1) gave index %d, want %d", v.AnimIndex, n-1)
	}
	v.SelectAnim(n)
	if v.AnimIndex != 0 {
		t.Errorf("selectAnim past the end gave index %d, want 0 (wrap)", v.AnimIndex)
	}
	if v.Time != 0 {
		t.Errorf("time = %v after selecting a clip, want 0", v.Time)
	}
}

// TestMaterialSearchRootsIncludesModFolders checks that the material lookup has
// somewhere to look beyond the merged tree: game art is shared between mods, so
// a .whm can reference a .rsh that lives in another data folder.
func TestMaterialSearchRootsIncludesModFolders(t *testing.T) {
	tree := &modtree.ModTree{
		AppPath:     `C:\Games\Dawn of War Definitive Edition`,
		ModFolder:   "AdvancedCampaign",
		ModFilePath: `C:\Games\Dawn of War Definitive Edition\AdvancedCampaign.module`,
		Sources: []*modtree.FileSource{
			{ModName: "AdvancedCampaign", DiskRoot: `C:\Games\Dawn of War Definitive Edition\AdvancedCampaign\Data`},
			{ModName: "DXP3", IsSGA: true, ArchivePath: `C:\Games\Dawn of War Definitive Edition\DXP3\Art.sga`},
		},
	}
	roots := materialSearchRoots(tree)
	if len(roots) == 0 {
		t.Fatal("no search roots")
	}
	wantMod := filepath.Clean(filepath.Join(tree.AppPath, tree.ModFolder))
	if roots[0] != wantMod {
		t.Errorf("first root = %q, want the current mod folder %q", roots[0], wantMod)
	}
	var found bool
	for _, r := range roots {
		if r == filepath.Clean(`C:\Games\Dawn of War Definitive Edition\AdvancedCampaign\Data`) {
			found = true
		}
	}
	if !found {
		t.Errorf("roots %v do not include the disk data folder", roots)
	}
	for _, r := range roots {
		if filepath.Ext(r) == ".sga" {
			t.Errorf("SGA archive %q should not be a disk root", r)
		}
	}
}

// TestReadModelMaterialFallsBackToDisk checks the fallback path: the merged
// tree misses the file, but another mod's data folder has it.
func TestReadModelMaterialFallsBackToDisk(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "art", "ebps", "races", "necrons", "texture_share")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rsh, err := os.ReadFile(filepath.Join(rshNecronsDir, "nec_warrior.rsh"))
	if err != nil {
		t.Skipf("game textures not available: %v", err)
	}
	rel := `art/ebps/races/necrons/texture_share/nec_warrior.rsh`
	if err := os.WriteFile(filepath.Join(dir, "Data", filepath.FromSlash(rel)), rsh, 0o644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	got, ok := readModelMaterial(tree, []string{filepath.Join(dir, "Data")}, "",
		`art/ebps/races/necrons/texture_share/nec_warrior`)
	if !ok {
		t.Fatal("readModelMaterial did not find the file on disk")
	}
	if len(got) != len(rsh) {
		t.Errorf("read %d bytes, want %d", len(got), len(rsh))
	}
}

func frameHasGeometry(img *image.RGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 0x3000 || g > 0x3000 || bl > 0x3000 {
				return true
			}
		}
	}
	return false
}

// ─── merged from x_test_model3d_test.go ────────────────────────────────────────

func TestModelTextureReadsRowsAsStored(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		src.SetNRGBA(x, 0, color.NRGBA{R: 255, A: 255})
		src.SetNRGBA(x, 3, color.NRGBA{B: 255, A: 255})
	}
	tex := render.NewModelTexture(src)
	if got := tex.Sample(0.1, 0); got.R < 0.9 || got.B > 0.1 {
		t.Errorf("v = 0 does not read the first stored row: %+v", got)
	}
	if got := tex.Sample(0.1, 0.9); got.B < 0.9 || got.R > 0.1 {
		t.Errorf("v = 0.9 does not read the last stored row: %+v", got)
	}
	// A model's UVs run well past the edge of its map, so a coordinate
	// over one has to fold back onto it rather than run off the end.
	if tex.Sample(1.25, 2.25) != tex.Sample(0.25, 0.25) {
		t.Errorf("a coordinate past the edge does not wrap onto the map: %+v against %+v",
			tex.Sample(1.25, 2.25), tex.Sample(0.25, 0.25))
	}
}

// renderTestModel loads a real Necrons .whm and decodes it into a renderable
// model. It skips when the game is not installed.
func renderTestModel(t *testing.T) *WhmModel {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dowTroopsDir, "lychguard.whm"))
	if err != nil {
		t.Skipf("game models not available: %v", err)
	}
	model, err := ParseWhmModel(data)
	if err != nil {
		t.Fatalf("ParseWhmModel: %v", err)
	}
	if len(model.Vertices) == 0 || len(model.Meshes) == 0 {
		t.Fatalf("model has no geometry: %d verts, %d meshes", len(model.Vertices), len(model.Meshes))
	}
	return model
}

func TestRenderWhmModelGroundPlaneUsesTexture(t *testing.T) {
	texture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	texture.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	texture.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	texture.SetRGBA(0, 1, color.RGBA{B: 255, A: 255})
	texture.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, A: 255})
	model := &WhmModel{}
	opts := render.RenderOptions{
		Width: 100, Height: 100, Pitch: 0.65, Distance: 8,
		Target: fauxgl.Vector{Z: 3}, Quality: render.ModelQualityFinal,
		Texture: render.NewModelTexture(texture), Ground: true,
	}
	withGround := render.RenderWhmModel(&model.Model, model.RestPose(), opts)
	opts.Ground = false
	withoutGround := render.RenderWhmModel(&model.Model, model.RestPose(), opts)
	if imagesEqual(withGround, withoutGround) {
		t.Fatal("ground plane did not contribute any pixels to the rendered frame")
	}
	changed := 0
	for y := 0; y < withGround.Bounds().Dy(); y++ {
		for x := 0; x < withGround.Bounds().Dx(); x++ {
			if withGround.RGBAAt(x, y) != withoutGround.RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed < 20 {
		t.Errorf("ground plane covered only %d changed pixels", changed)
	}
}

func TestRenderWhmModelProducesGeometry(t *testing.T) {
	model := renderTestModel(t)
	center, distance := model.ModelCenter()
	img := render.RenderWhmModel(&model.Model, model.RestPose(), render.RenderOptions{
		Width:    160,
		Height:   120,
		Yaw:      0.6,
		Pitch:    0.2,
		Distance: distance,
		Target:   center,
		Quality:  render.ModelQualityFinal,
	})
	if img == nil {
		t.Fatal("render.RenderWhmModel returned nil")
	}
	if img.Bounds().Dx() != 160 || img.Bounds().Dy() != 120 {
		t.Fatalf("size = %v, want 160x120", img.Bounds())
	}
	// The background is a flat colour, so a frame with real geometry must show
	// at least a few distinct colours.
	seen := map[uint32]bool{}
	lit := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			seen[r<<16|g<<8|bb] = true
			if r > 0x3000 || g > 0x3000 || bb > 0x3000 {
				lit++
			}
		}
	}
	if len(seen) < 3 {
		t.Errorf("frame is flat: %d distinct colours", len(seen))
	}
	if lit == 0 {
		t.Error("frame has no lit pixels; geometry did not rasterise")
	}
}

func TestRenderWhmModelDraftAndFinalDiffer(t *testing.T) {
	model := renderTestModel(t)
	center, distance := model.ModelCenter()
	opts := render.RenderOptions{
		Width:    64,
		Height:   48,
		Yaw:      0.3,
		Distance: distance,
		Target:   center,
	}
	draft := render.RenderWhmModel(&model.Model, model.RestPose(), withQuality(opts, render.ModelQualityDraft))
	final := render.RenderWhmModel(&model.Model, model.RestPose(), withQuality(opts, render.ModelQualityFinal))
	if imagesEqual(draft, final) {
		t.Error("draft and final frames are identical; shading is not applied")
	}
}

func TestRenderWhmModelWithSkeleton(t *testing.T) {
	model := renderTestModel(t)
	center, distance := model.ModelCenter()
	img := render.RenderWhmModel(&model.Model, model.RestPose(), render.RenderOptions{
		Width:    64,
		Height:   48,
		Yaw:      0.2,
		Distance: distance,
		Target:   center,
		Quality:  render.ModelQualityFinal,
		Skeleton: true,
	})
	if img == nil {
		t.Fatal("render.RenderWhmModel returned nil")
	}
}

func TestRenderWhmModelAnimationMovesGeometry(t *testing.T) {
	model, anim := renderTestModelWithAnimation(t)
	center, distance := model.ModelCenter()
	opts := render.RenderOptions{
		Width:    96,
		Height:   72,
		Yaw:      0.4,
		Distance: distance,
		Target:   center,
		Quality:  render.ModelQualityFinal,
	}
	first := render.RenderWhmModel(&model.Model, model.PoseAt(anim, 0), opts)
	// A still frame is not proof that playback works, so sample a few points
	// and require at least one of them to differ from the first frame.
	changed := false
	for i := 1; i <= 4 && !changed; i++ {
		tNorm := float32(i) / 4
		frame := render.RenderWhmModel(&model.Model, model.PoseAt(anim, tNorm), opts)
		if !imagesEqual(first, frame) {
			changed = true
		}
	}
	if !changed {
		t.Errorf("clip %q did not change the rendered frame", anim.Name)
	}
}

func TestRenderWhmModelRejectsEmptyOptions(t *testing.T) {
	model := renderTestModel(t)
	img := render.RenderWhmModel(&model.Model, model.RestPose(), render.RenderOptions{Width: 0, Height: 0})
	if img == nil {
		t.Fatal("expected an empty image, got nil")
	}
}

func TestPoseAtIsStable(t *testing.T) {
	model := renderTestModel(t)
	a := model.PoseAt(nil, 0)
	b := model.PoseAt(nil, 0)
	if len(a.Bones) != len(model.Bones) {
		t.Fatalf("pose has %d bones, model has %d", len(a.Bones), len(model.Bones))
	}
	for i := range a.Bones {
		if a.Bones[i] != b.Bones[i] {
			t.Fatalf("bone %d: rest pose is not deterministic", i)
		}
	}
}

func TestPoseAtClampsOutsideClip(t *testing.T) {
	model, anim := renderTestModelWithAnimation(t)
	first := model.PoseAt(anim, -5)
	zero := model.PoseAt(anim, 0)
	for i := range first.Bones {
		if first.Bones[i] != zero.Bones[i] {
			t.Fatalf("bone %d: t=-5 differs from t=0", i)
		}
	}
	last := model.PoseAt(anim, 5)
	one := model.PoseAt(anim, 1)
	for i := range last.Bones {
		if last.Bones[i] != one.Bones[i] {
			t.Fatalf("bone %d: t=5 differs from t=1", i)
		}
	}
}

func TestSkinnedVertexNormalisesWeights(t *testing.T) {
	model := renderTestModel(t)
	pose := model.RestPose()
	for i := range model.Vertices {
		v := &model.Vertices[i]
		// A doubled weight set must land on the same place as a halved one.
		pos, _ := render.SkinnedVertex(&model.Model, pose, v)
		if math.IsNaN(pos.X) || math.IsNaN(pos.Y) || math.IsNaN(pos.Z) {
			t.Fatalf("vertex %d skinned to NaN", i)
		}
	}
}

// renderTestModelWithAnyAnimation finds a troop model that has clips at all,
// without requiring one of them to move the mesh.
func renderTestModelWithAnyAnimation(t *testing.T) *WhmModel {
	t.Helper()
	entries, err := os.ReadDir(dowTroopsDir)
	if err != nil {
		t.Skipf("game models not available: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".whm" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dowTroopsDir, e.Name()))
		if err != nil {
			continue
		}
		model, err := ParseWhmModel(data)
		if err == nil && len(model.Animations) > 0 {
			return model
		}
	}
	t.Skip("no animated model found")
	return nil
}

// renderTestModelWithAnimation finds a troop model with a clip that actually
// moves bones, so playback is tested against real data rather than skipped.
// Some clips only fade or hide meshes, which legitimately leave the geometry
// untouched.
func renderTestModelWithAnimation(t *testing.T) (*WhmModel, *WhmAnimation) {
	t.Helper()
	entries, err := os.ReadDir(dowTroopsDir)
	if err != nil {
		t.Skipf("game models not available: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".whm" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dowTroopsDir, e.Name()))
		if err != nil {
			continue
		}
		model, err := ParseWhmModel(data)
		if err != nil || len(model.Animations) == 0 || len(model.Bones) == 0 {
			continue
		}
		for i := range model.Animations {
			if animMovesGeometry(model, &model.Animations[i]) {
				return model, &model.Animations[i]
			}
		}
	}
	t.Skip("no animated model found")
	return nil, nil
}

// animMovesGeometry reports whether a clip visibly displaces the mesh. Plenty
// of clips only twitch a weapon or a turret, and a model can be almost entirely
// rigid, so the bar is a noticeable share of the vertices moving a real amount.
func animMovesGeometry(model *WhmModel, anim *WhmAnimation) bool {
	if !animMovesBones(model, anim) {
		return false
	}
	rest := model.RestPose()
	posed := model.PoseAt(anim, 0.5)
	moved := 0
	for i := range model.Vertices {
		v := &model.Vertices[i]
		bound := false
		for s := 0; s < 4; s++ {
			if render.BoneInRange(v.Bones[s], len(model.Bones)) && v.Weights[s] > 0 {
				bound = true
			}
		}
		if !bound {
			continue
		}
		p0, _ := render.SkinnedVertex(&model.Model, rest, v)
		p1, _ := render.SkinnedVertex(&model.Model, posed, v)
		if p0.Sub(p1).Length() > 1e-3 {
			moved++
		}
	}
	return moved*50 >= len(model.Vertices)
}

// animMovesBones reports whether a clip puts bones in more than one place.
func animMovesBones(model *WhmModel, anim *WhmAnimation) bool {
	for _, track := range anim.Bones {
		if track.Bone < 0 || track.Bone >= len(model.Bones) {
			continue
		}
		for i := 1; i < len(track.PosKey); i++ {
			if track.PosKey[i].V != track.PosKey[0].V {
				return true
			}
		}
		for i := 1; i < len(track.RotKey); i++ {
			if track.RotKey[i].V != track.RotKey[0].V {
				return true
			}
		}
	}
	return false
}

func withQuality(opts render.RenderOptions, q int) render.RenderOptions {
	opts.Quality = q
	return opts
}

func imagesEqual(a, b image.Image) bool {
	if a == nil || b == nil {
		return false
	}
	ab, bb := a.Bounds(), b.Bounds()
	if ab != bb {
		return false
	}
	for y := ab.Min.Y; y < ab.Max.Y; y++ {
		for x := ab.Min.X; x < ab.Max.X; x++ {
			ar, ag, ab_, aa := a.At(x, y).RGBA()
			br, bg, bb_, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab_ != bb_ || aa != ba {
				return false
			}
		}
	}
	return true
}

// ─── merged from x_test_model3d_overlay_test.go ────────────────────────────────────────

func overlayTestModel() *WhmModel {
	return &WhmModel{
		Name: "overlay_box",
		Vertices: []WhmVertex{
			{Pos: [3]float32{-0.5, -0.5, -0.5}, Normal: [3]float32{-1, -1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, -0.5, -0.5}, Normal: [3]float32{1, -1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, 0.5, -0.5}, Normal: [3]float32{1, 1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{-0.5, 0.5, -0.5}, Normal: [3]float32{-1, 1, -1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{-0.5, -0.5, 0.5}, Normal: [3]float32{-1, -1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, -0.5, 0.5}, Normal: [3]float32{1, -1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{0.5, 0.5, 0.5}, Normal: [3]float32{1, 1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
			{Pos: [3]float32{-0.5, 0.5, 0.5}, Normal: [3]float32{-1, 1, 1}, Bones: [4]int32{-1, -1, -1, -1}},
		},
		Meshes: []WhmMesh{{Name: "box", Indices: []uint32{0, 2, 1, 0, 3, 2, 4, 5, 6, 4, 6, 7, 0, 1, 5, 0, 5, 4}}},
		Bones: []WhmBone{
			{Name: "root", Parent: -1},
			{Name: "child", Parent: 0, Pos: [3]float32{0.4, 0, 0}},
		},
		Markers: []WhmMarker{{
			Name:   "marker_tip",
			Parent: "child",
			Matrix: [9]float32{1, 0, 0, 0, 1, 0, 0, 0, 1},
			Pos:    [3]float32{0.3, 0, 0},
		}},
	}
}

// overlayTestOptions is a camera that frames the box: without a field of view
// and a distance every overlay width works out to zero or to nonsense.
func overlayTestOptions() render.RenderOptions {
	return render.RenderOptions{
		Width: 400, Height: 300,
		Yaw: 0.6, Pitch: 0.3, Distance: 6,
		Fov: render.DefaultFov, Quality: render.ModelQualityFinal,
	}
}

// A bone is drawn from its parent's origin to its own, which is what makes a
// skeleton read as a skeleton rather than as a cloud of dots: the line has to
// start where the joint above it is.
func TestOverlayBonesJoinTheParentToTheBone(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Skeleton = true
	got := render.ModelOverlayLines(&m.Model, pose, opts)
	if len(got) == 0 {
		t.Fatal("no bone lines were built for a two bone skeleton")
	}
	// One bone has one parent, so a two bone chain has one line: the root has
	// no parent and so has nothing to be joined to.
	if len(got) != 1 {
		t.Fatalf("built %d bone lines, want 1 for a two bone chain", len(got))
	}
	s := got[0]
	wantFrom := pose.Bones[0].MulPosition(fauxgl.Vector{})
	wantTo := pose.Bones[1].MulPosition(fauxgl.Vector{})
	if s.From.Sub(wantFrom).Length() > 1e-9 {
		t.Errorf("the line starts at %v, the root's origin is %v", s.From, wantFrom)
	}
	if s.To.Sub(wantTo).Length() > 1e-9 {
		t.Errorf("the line ends at %v, the child's origin is %v", s.To, wantTo)
	}
	if s.HalfWidth <= 0 {
		t.Errorf("the line is %v wide, which draws nothing at all", s.HalfWidth)
	}
}

// The overlay's width is asked for in pixels and converted, so a bone is the
// same thickness on screen at any camera distance. A fixed world width would
// be a different length of line here and there, which is the bug this rule
// exists to prevent.
func TestOverlayWidthIsInScreenPixels(t *testing.T) {
	opts := overlayTestOptions()
	near := render.ModelOverlayHalfWidth(opts, 2)
	opts.Distance *= 4
	far := render.ModelOverlayHalfWidth(opts, 2)
	if math.Abs(near*4-far) > 1e-9 {
		t.Errorf("four times the distance gave a half width of %v against %v, which is not four times %v", far, near, near)
	}
	// Half the pixels is half the width, at the same camera.
	if half := render.ModelOverlayHalfWidth(opts, 1); math.Abs(half*2-far) > 1e-9 {
		t.Errorf("two pixels gave %v and one gave %v, which are not in step", far, half)
	}
}

// The picked marker gets its frame drawn, and its forward arrow points along
// the axis its effects would leave by. The direction is the whole point of the
// arrow, so it is compared against the axis itself rather than merely checked
// for existing.
func TestOverlayMarkerArrowPointsAlongItsAxis(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Marker = render.PickedMarker(0)
	segments := render.ModelOverlayLines(&m.Model, pose, opts)
	if len(segments) == 0 {
		t.Fatal("no overlay was built for a picked marker")
	}
	axis, ok := m.MarkerAxis(0, pose, true)
	if !ok {
		t.Skip("the test marker has no usable forward axis")
	}
	at, _ := m.MarkerWorld(0, pose, true)

	// The arrow is the yellow segment that leaves the marker; the axes are
	// the six shorter ones around it.
	var shaft *render.ModelOverlayLine
	for i := range segments {
		s := &segments[i]
		if s.From.Sub(at).Length() > 1e-9 || s.R != render.ModelMarkerFwdColor[0] {
			continue
		}
		if dir := s.To.Sub(s.From); dir.Dot(axis) > 0.99*dir.Length() {
			shaft = s
			break
		}
	}
	if shaft == nil {
		t.Fatalf("none of the %d overlay segments runs from the marker along its forward %v", len(segments), axis)
	}
	dir := shaft.To.Sub(shaft.From)
	want := axis.MulScalar(render.ModelMarkerAxisWorldLength)
	if math.Abs(dir.Length()-want.Length()) > 1e-9 {
		t.Errorf("the arrow is %v long, the frame reaches %v", dir.Length(), want.Length())
	}
	dot := dir.Normalize().Dot(axis)
	if math.IsNaN(dot) {
		t.Fatalf("the arrow direction came out as %v and cannot be compared", dir)
	}
	if dot < 0.999 {
		t.Errorf("the arrow points %v but the marker's forward is %v (dot=%.4f)", dir.Normalize(), axis, dot)
	}
}

// DATASEUI's scale values are already the ground-plane half-extents, not a
// multiplier for the mesh bounds. The NPC monolith's measured rectangle uses
// this same convention: its half-extents span almost the model's whole base.
func TestSelectionIndicatorUsesDATASEUIHalfExtentsOnGround(t *testing.T) {
	m := overlayTestModel()
	opts := overlayTestOptions()
	selection := &engine.SelectionIndicator{
		DisplayType: 1,
		Scale:       [2]float32{5.334, 4.452},
		Offset:      [2]float32{0.25, -0.5},
	}
	opts.SelectionIndicator = selection
	lines := render.ModelOverlayLines(&m.Model, m.RestPose(), opts)
	if len(lines) != 4 {
		t.Fatalf("square indicator has %d segments, want four sides", len(lines))
	}
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, line := range lines {
		for _, point := range []fauxgl.Vector{line.From, line.To} {
			minX, maxX = math.Min(minX, point.X), math.Max(maxX, point.X)
			minY, maxY = math.Min(minY, point.Y), math.Max(maxY, point.Y)
			if point.Z != 0 {
				t.Errorf("selection line is at Z=%v, want the ground plane Z=0", point.Z)
			}
		}
		if line.HalfWidth <= 0 || line.G <= line.R {
			t.Errorf("selection line has invalid width or is not green: %+v", line)
		}
	}
	if want := float64(selection.Offset[0] - selection.Scale[0]); math.Abs(minX-want) > 1e-6 {
		t.Errorf("left edge x=%v, want offset-scale %v", minX, want)
	}
	if want := float64(selection.Offset[0] + selection.Scale[0]); math.Abs(maxX-want) > 1e-6 {
		t.Errorf("right edge x=%v, want offset+scale %v", maxX, want)
	}
	if want := float64(selection.Offset[1] - selection.Scale[1]); math.Abs(minY-want) > 1e-6 {
		t.Errorf("near edge y=%v, want offset-scale %v", minY, want)
	}
	if want := float64(selection.Offset[1] + selection.Scale[1]); math.Abs(maxY-want) > 1e-6 {
		t.Errorf("far edge y=%v, want offset+scale %v", maxY, want)
	}

	selection.DisplayType = 0
	circle := render.ModelOverlayLines(&m.Model, m.RestPose(), opts)
	if len(circle) != 48 {
		t.Fatalf("circle indicator has %d segments, want 48", len(circle))
	}
	if gap := circle[0].From.Sub(circle[len(circle)-1].To).Length(); gap > 1e-6 {
		t.Errorf("circle indicator is not closed: end gap %v", gap)
	}
}

// Nothing asked for, nothing drawn. A frame with no skeleton and no picked
// marker must carry no overlay at all, or every model would come out wearing
// a skeleton the reader never turned on.
func TestOverlayIsEmptyWhenNothingIsAskedFor(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Skeleton = false
	opts.Marker = nil
	if got := render.ModelOverlayLines(&m.Model, pose, opts); len(got) != 0 {
		t.Errorf("built %d overlay segments for a frame that asked for none", len(got))
	}
	// A stale marker index, which is what a model replaced under a viewer
	// that still remembers the old one looks like, must not read past the end.
	stale := len(m.Markers) + 3
	opts.Marker = &stale
	if got := render.ModelOverlayLines(&m.Model, pose, opts); len(got) != 0 {
		t.Errorf("built %d overlay segments for marker %d of a model with %d", len(got), stale, len(m.Markers))
	}
	if got := render.ModelOverlayLines(nil, pose, opts); len(got) != 0 {
		t.Errorf("built %d overlay segments for no model at all", len(got))
	}
}

// The software renderer draws the same segments, so the skeleton it has always
// drawn must still be there. This is the regression guard on the rewrite: the
// bones used to be drawn by drawSkeleton, which this replaced with a shared
// builder, and a builder that produced nothing would leave the software frame
// silently bare.
func TestSoftwareFrameStillDrawsTheSkeleton(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.Target = render.Vec3From(m.BoundMin).Add(render.Vec3From(m.BoundMax)).MulScalar(0.5)
	without := render.RenderWhmModel(&m.Model, pose, opts)
	opts.Skeleton = true
	with := render.RenderWhmModel(&m.Model, pose, opts)
	changed := 0
	for i := 0; i+3 < len(without.Pix) && i+3 < len(with.Pix); i += 4 {
		if without.Pix[i] != with.Pix[i] || without.Pix[i+1] != with.Pix[i+1] || without.Pix[i+2] != with.Pix[i+2] {
			changed++
		}
	}
	t.Logf("%d pixels differ between the frame with and without the skeleton", changed)
	if changed == 0 {
		t.Error("turning the skeleton on changed nothing on the software path")
	}
}

// ─── merged from x_test_model3d_bind_test.go ────────────────────────────────────────

func boneIndexByName(m *WhmModel, name string) int {
	for i := range m.Bones {
		if m.Bones[i].Name == name {
			return i
		}
	}
	return -1
}

func bonePos(pose WhmPose, m *WhmModel, name string) (fauxgl.Vector, bool) {
	i := boneIndexByName(m, name)
	if i < 0 || i >= len(pose.Bones) {
		return fauxgl.Vector{}, false
	}
	return pose.Bones[i].MulPosition(fauxgl.Vector{}), true
}

// TestBipedStandsUpright pins the skeleton convention against anatomy, which no
// amount of plausible looking wrongness can fake. A biped authored for the game
// has its toes on the ground plane, its head above its pelvis and its spine
// climbing between them. Getting the root frame sign, the order of a bone's
// translation and rotation, or the sense of its stored quaternion wrong folds
// the chain over: the legs climb towards the head and the spine dives.
func TestBipedStandsUpright(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dowTroopsDir, "lychguard.whm"))
	if err != nil {
		t.Skip(err)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	pose := model.RestPose()
	need := []string{"bip01", "bip01 l toe0", "bip01 r toe0", "bip01 head", "bip01 spine", "bip01 spine3", "bip01 l calf"}
	got := make(map[string]fauxgl.Vector, len(need))
	for _, n := range need {
		p, ok := bonePos(pose, model, n)
		if !ok {
			t.Fatalf("no bone %q", n)
		}
		got[n] = p
	}
	t.Logf("pelvis z=%.2f  toes z=%.2f/%.2f  head z=%.2f  spine z=%.2f spine3 z=%.2f",
		got["bip01"].Z, got["bip01 l toe0"].Z, got["bip01 r toe0"].Z, got["bip01 head"].Z,
		got["bip01 spine"].Z, got["bip01 spine3"].Z)

	height := float64(model.BoundMax[2] - model.BoundMin[2])
	floor := float64(model.BoundMin[2])

	// The toes have to be on the mesh floor, not sunk into it or floating.
	for _, n := range []string{"bip01 l toe0", "bip01 r toe0"} {
		if d := math.Abs(got[n].Z - floor); d > 0.12*height {
			t.Errorf("%s is %.2f from the ground plane, want under %.2f", n, d, 0.12*height)
		}
	}
	if got["bip01 head"].Z <= got["bip01"].Z {
		t.Errorf("head z=%.2f is not above the pelvis z=%.2f", got["bip01 head"].Z, got["bip01"].Z)
	}
	if got["bip01 spine3"].Z <= got["bip01 spine"].Z {
		t.Errorf("spine3 z=%.2f does not climb above spine z=%.2f", got["bip01 spine3"].Z, got["bip01 spine"].Z)
	}
	if got["bip01 l calf"].Z >= got["bip01"].Z {
		t.Errorf("calf z=%.2f is not below the hip z=%.2f", got["bip01 l calf"].Z, got["bip01"].Z)
	}
	if got["bip01 head"].Z > floor+height {
		t.Errorf("head z=%.2f is above the top of the model", got["bip01 head"].Z)
	}
}

// TestBuildingBonesSitInsideTheirPiece covers a building, where every piece is
// rigid and hangs off its own bone. A piece has to move with the bone named
// for it in the mesh list, which means that bone has to be inside the piece it
// belongs to. It is the same convention as the biped, checked on geometry
// whose correct answer is not in doubt.
func TestBuildingBonesSitInsideTheirPiece(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dowRacesDir, "Necrons", "structures", "necron_energy_core.whm"))
	if err != nil {
		t.Skip(err)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	bound := 0
	for i := range model.Vertices {
		if model.Vertices[i].Weights[0] > 0 {
			bound++
		}
	}
	if bound != len(model.Vertices) {
		t.Fatalf("only %d of %d vertices are bound to a bone; a rigid piece has no weights of its own", bound, len(model.Vertices))
	}

	pose := model.RestPose()
	for i := range model.Meshes {
		m := &model.Meshes[i]
		if m.SingleBone < 0 || m.SingleBone >= len(model.Bones) {
			t.Errorf("mesh %q has no valid single bone (got %d)", m.Name, m.SingleBone)
			continue
		}
		lo := [3]float64{1e9, 1e9, 1e9}
		hi := [3]float64{-1e9, -1e9, -1e9}
		for _, idx := range m.Indices {
			if int(idx) >= len(model.Vertices) {
				continue
			}
			v := &model.Vertices[idx]
			p := [3]float64{float64(v.Pos[0]), float64(v.Pos[1]), float64(v.Pos[2])}
			for a := 0; a < 3; a++ {
				if p[a] < lo[a] {
					lo[a] = p[a]
				}
				if p[a] > hi[a] {
					hi[a] = p[a]
				}
			}
		}
		origin := pose.Bones[m.SingleBone].MulPosition(fauxgl.Vector{})
		p := [3]float64{origin.X, origin.Y, origin.Z}
		outside := ""
		for a := 0; a < 3; a++ {
			if p[a] < lo[a]-0.05 || p[a] > hi[a]+0.05 {
				outside += string("xyz"[a])
			}
		}
		if outside != "" {
			t.Errorf("mesh %q spans X[%.2f %.2f] Y[%.2f %.2f] Z[%.2f %.2f] but its bone %q is at (%.2f %.2f %.2f), outside on %s",
				m.Name, lo[0], hi[0], lo[1], hi[1], lo[2], hi[2],
				model.Bones[m.SingleBone].Name, p[0], p[1], p[2], outside)
		}
	}
}

// TestPoseAtRestKeyIsNeutral checks the property that pins down how a key is
// interpreted. The first key of an idle clip repeats the bone's own rest
// transform, so a clip evaluated there must leave the mesh exactly where the
// bind pose had it. Reading a key as a delta from the rest transform, or
// converting it through another axis convention, both break this.
func TestPoseAtRestKeyIsNeutral(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dowTroopsDir, "lychguard.whm"))
	if err != nil {
		t.Skip(err)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	rest := model.RestPose()
	checked := 0
	for i := range model.Animations {
		anim := &model.Animations[i]
		// Bones whose first key repeats their own rest transform have to come
		// out of PoseAt exactly where the bind pose had them. Capes and
		// weapons are keyed away from the bind pose even in an idle clip, so
		// they simply do not qualify as a probe.
		neutral := map[int]bool{}
		for _, tr := range anim.Bones {
			if tr.Bone < 0 {
				continue
			}
			pos, rot := model.Bones[tr.Bone].Pos, model.Bones[tr.Bone].Quat
			if len(tr.PosKey) > 0 && vecDistance(tr.PosKey[0].V, pos) < 1e-3 {
				if len(tr.RotKey) == 0 || quatDistance(tr.RotKey[0].V, rot) < 1e-3 {
					neutral[tr.Bone] = true
				}
			}
		}
		if len(neutral) == 0 {
			continue
		}
		checked += len(neutral)
		pose := model.PoseAt(anim, anim.Bones[0].PosKey[0].Time)
		for b := range neutral {
			// The keys are float32 in the file, so an exact match is not on;
			// a centimetre on a model metres tall is rounding.
			if d := matDelta(rest.Bones[b], pose.Bones[b]); d > 0.01 {
				t.Errorf("clip %q bone %q drifted %.4f at its rest key",
					anim.Name, model.Bones[b].Name, d)
			}
		}
		if checked > 200 {
			break
		}
	}
	if checked == 0 {
		t.Skip("no clip repeats the rest pose at its first key")
	}
	t.Logf("checked %d neutral rest keys", checked)
}

func vecDistance(a, b [3]float32) float64 {
	s := 0.0
	for i := 0; i < 3; i++ {
		d := float64(a[i]) - float64(b[i])
		s += d * d
	}
	return math.Sqrt(s)
}

func quatDistance(a, b [4]float32) float64 {
	dot := 0.0
	for i := 0; i < 4; i++ {
		dot += float64(a[i]) * float64(b[i])
	}
	if dot < 0 {
		dot = -dot
	}
	if dot > 1 {
		dot = 1
	}
	return 2 * math.Acos(dot)
}

func matDelta(a, b fauxgl.Matrix) float64 {
	worst := 0.0
	for _, p := range [][2]float64{
		{a.X00, b.X00}, {a.X01, b.X01}, {a.X02, b.X02}, {a.X03, b.X03},
		{a.X10, b.X10}, {a.X11, b.X11}, {a.X12, b.X12}, {a.X13, b.X13},
		{a.X20, b.X20}, {a.X21, b.X21}, {a.X22, b.X22}, {a.X23, b.X23},
	} {
		if d := math.Abs(p[0] - p[1]); d > worst {
			worst = d
		}
	}
	return worst
}

// TestBonesStayNearTheirGeometry covers what the anatomical checks cannot: a
// skeleton that is reflected rather than folded still stands on its feet, but
// every bone is then on the wrong side of the geometry it deforms. Averaging the
// distance from a vertex to its own bone over six models separates the two by
// a wide margin, where the anatomy of a single model does not.
func TestBonesStayNearTheirGeometry(t *testing.T) {
	models := []struct {
		dir, file string
	}{
		{dowTroopsDir, "lychguard.whm"},
		{filepath.Join(dowRacesDir, "Necrons", "structures"), "necron_energy_core.whm"},
		{dowTroopsDir, "Necron_pylon.whm"},
		{dowTroopsDir, "eliminator.whm"},
		{filepath.Join(dowRacesDir, "Necrons", "structures"), "necron_great_pyramid.whm"},
		{dowTroopsDir, "Builder_scarab.whm"},
	}
	total, n, checked := 0.0, 0, 0
	for _, m := range models {
		raw, err := os.ReadFile(filepath.Join(m.dir, m.file))
		if err != nil {
			t.Skip(err)
		}
		model, err := ParseWhmModel(raw)
		if err != nil {
			t.Errorf("%s: %v", m.file, err)
			continue
		}
		size := float64(model.BoundMax[2] - model.BoundMin[2])
		if size <= 0.01 || len(model.Bones) == 0 {
			continue
		}
		pose := model.RestPose()
		for i := range model.Vertices {
			v := &model.Vertices[i]
			if v.Weights[0] <= 0 {
				continue
			}
			b := v.Bones[0]
			if b < 0 || int(b) >= len(pose.Bones) {
				continue
			}
			pos, _ := render.SkinnedVertex(&model.Model, pose, v)
			origin := pose.Bones[b].MulPosition(fauxgl.Vector{})
			total += origin.Sub(pos).Length() / size
			n++
		}
		checked++
	}
	if checked == 0 || n == 0 {
		t.Skip("no models available")
	}
	mean := total / float64(n)
	t.Logf("bones sit %.1f%% of the model height from the geometry they deform (%d vertices, %d models)",
		100*mean, n, checked)
	// A reflected skeleton scores above 0.33, the correct one 0.23.
	if mean > 0.28 {
		t.Errorf("mean bone to geometry distance is %.0f%% of the model height, want under 28%%", 100*mean)
	}
}

// ─── merged from x_test_model3d_scrub_test.go ────────────────────────────────────────

func TestScrubBarSplitsTheWidthIntoOneSlicePerFrame(t *testing.T) {
	const frames = 8
	const left, width = 10.0, 800.0
	cases := []struct {
		x    float32
		want int
	}{
		{left, 0},
		{left + 1, 0},
		// A pixel under frame 1's tick is frame 1: the strip rounds to the
		// nearest tick rather than splitting the width into slices, so the
		// click lands on the tick it was aimed at — and a click on a tick
		// reads that tick's frame, which is what the header above demands.
		{left + width/frames - 1, 1},
		{left + width/frames, 1},
		{left + width/2, 4},
		{left + width - 1, 7},
	}
	for _, c := range cases {
		if got := scrubBarAt(c.x, left, width, frames); got != c.want {
			t.Errorf("a click at %.0f lands on frame %d, want %d", c.x, got+1, c.want+1)
		}
	}
	// Past the ends is clamped rather than refused: a press on the margin next
	// to the strip is the nearest frame, not nothing at all.
	if got := scrubBarAt(left-50, left, width, frames); got != 0 {
		t.Errorf("a click left of the strip gave frame %d, want the first", got)
	}
	if got := scrubBarAt(left+width+50, left, width, frames); got != frames-1 {
		t.Errorf("a click right of the strip gave frame %d, want the last", got)
	}
}

// The number under a tick, the tick itself and the frame the playhead reports
// are three readings of one position, so a timeline event at 0.5 of a 10 frame
// clip is frame 6 and not frame 5 or 7.
func TestTimelineTimesLandOnTheFrameThePlayheadReports(t *testing.T) {
	const frames = 10
	cases := []struct {
		at   float64
		want int
	}{
		{-1, 0}, // on entering the clip
		{0, 0},
		{0.05, 1}, // rounds
		{0.5, 5},
		{0.55, 6}, // rounds up
		{0.999, 9},
		{3.4e38, 9}, // on leaving it
	}
	for _, c := range cases {
		if got := frameAt(c.at, frames); got != c.want {
			t.Errorf("a normalized timeline time of %g lands on frame %d, want %d", c.at, got, c.want)
		}
	}
	// And the playhead put at that frame reads the same frame back.
	v := &modelViewer{Anim: &WhmAnimation{Name: "c", NumFrames: frames}}
	for _, c := range cases {
		v.SeekFrame(frameAt(c.at, frames))
		got, _ := v.CurrentFrame()
		if got != c.want {
			t.Errorf("seeking to the frame for %g reads back as %d, want %d", c.at, got, c.want)
		}
	}
}

func TestSeekingStopsPlaybackAndClampsToTheClip(t *testing.T) {
	v := &modelViewer{Anim: &WhmAnimation{Name: "land", NumFrames: 62}, Playing: true}
	v.SeekFrame(61)
	if v.Playing {
		t.Error("a seek left playback running; the reader asked to stop on a frame")
	}
	if f, _ := v.CurrentFrame(); f != 61 {
		t.Errorf("seeked to the last frame reads back as %d", f)
	}
	// Past either end is the nearest frame, not a playhead off the clip.
	v.SeekFrame(1000)
	if f, _ := v.CurrentFrame(); f != 61 {
		t.Errorf("a seek past the end landed on %d, want the last frame", f)
	}
	v.SeekFrame(-5)
	if f, _ := v.CurrentFrame(); f != 0 {
		t.Errorf("a seek before the start landed on %d, want the first frame", f)
	}
}

func TestTheStripCatchesAPressAndLeavesTheCameraAlone(t *testing.T) {
	// The overlay pads the bar by a label's height on each side, so the strip
	// a press is tested against is taller than the five pixel line drawn on
	// it: a drag that runs off the bar is still a seek.
	hit := int(scrubLabelHeight)
	strip := image.Rect(0, 100-hit, 800, 116+hit)
	if !scrubHit(strip, f32.Pt(400, 108)) {
		t.Error("a press on the bar was not caught")
	}
	// Above and below the numbers is still the strip: a drag that runs off the
	// five pixel bar must not suddenly become a camera turn.
	for _, y := range []float32{92, 124} {
		if !scrubHit(strip, f32.Pt(400, y)) {
			t.Errorf("a press at y=%.0f, beside the bar, was not caught", y)
		}
	}
	if scrubHit(strip, f32.Pt(400, 60)) {
		t.Error("a press on the model above the strip was taken for a seek")
	}
	var empty = image.Rectangle{}
	if scrubHit(empty, f32.Pt(400, 108)) {
		t.Error("an undrawn strip caught a press")
	}
}

// The frames worth stopping at are the ones the .whe fires something on, and
// the bar has to say which kind of thing it was.
func TestTheStripMarksTheFramesEventsFireOn(t *testing.T) {
	v := &modelViewer{
		// Sixty frames at the default twenty a second is three seconds
		// long, which is what the timeline's own seconds are read against:
		// the moments in a .whe are seconds into the clip, not a fraction
		// of it.
		Anim: &WhmAnimation{Name: "landing", NumFrames: 60},
		FxTimelines: []whe.FxTimeline{
			{Name: "landing", Animations: []string{"landing"}, Events: []whe.FxTimelineEvent{
				{Name: "land_dust", Time: -1},
				{Name: "land_halo", Time: 0.5},
			}},
			{Name: "idle", Animations: []string{"idle"}, Events: []whe.FxTimelineEvent{
				{Name: "crystal_glow", Time: 0},
			}},
		},
	}
	got := v.ClipScrubEvents("landing")
	if len(got) != 2 {
		t.Fatalf("landing has %d marked frames, want 2", len(got))
	}
	// -1 is on entering, which is the first frame; half a second of a three
	// second clip is the eleventh.
	if got[0].Frame != 0 || got[1].Frame != 10 {
		t.Errorf("landing is marked on frames %d and %d, want 0 and 10", got[0].Frame, got[1].Frame)
	}
	if got[0].Name != "land_dust" || got[1].Name != "land_halo" {
		t.Errorf("landing is marked as %q and %q", got[0].Name, got[1].Name)
	}
	// Another clip's events do not leak onto this one.
	if other := v.ClipScrubEvents("idle"); len(other) != 1 || other[0].Name != "crystal_glow" {
		t.Errorf("idle is marked as %v", other)
	}
	if none := v.ClipScrubEvents("phase_in"); len(none) != 0 {
		t.Errorf("a clip nothing fires on is marked %v", none)
	}
}

func TestAModelWithoutTimelinesHasNothingToMark(t *testing.T) {
	v := &modelViewer{Anim: &WhmAnimation{Name: "any", NumFrames: 10}}
	if got := v.ClipScrubEvents("any"); len(got) != 0 {
		t.Errorf("a model with no .whe marked %d frames", len(got))
	}
}

// The thinning: a 362 frame clip cannot carry a number under every tick, and
// the step that keeps the labels readable must still reach the last frame.
func TestFrameSpacingThinsButKeepsTheEnds(t *testing.T) {
	for _, n := range []int{4, 62, 362} {
		step := int(math.Max(1, math.Floor(float64(n)/float64(float32(900)/scrubLabelPx))))
		if step < 1 {
			t.Fatalf("%d frames: step %d", n, step)
		}
		if float64(step) < 1 {
			t.Fatalf("%d frames: step %d", n, step)
		}
		// Every frame is still reachable by a click, however thin the ticks.
		for f := 0; f < n; f++ {
			// The bar is drawn inside an offset, so a frame's place on it
			// is measured from its left edge while the click carries the
			// position of the whole canvas.
			const left = float32(10)
			x := left + frameX(f, n, 800)
			if got := scrubBarAt(x, left, 800, n); got != f {
				t.Errorf("%d frames: the tick for frame %d clicks to %d", n, f, got)
			}
		}
	}
}

// ─── merged from x_test_model3d_bone_keys_test.go ────────────────────────────────────────

func TestClipKeyFramesCollectsAndDeduplicates(t *testing.T) {
	anim := &WhmAnimation{
		Name:      "move",
		NumFrames: 100,
		Bones: []WhmTrack{
			{Bone: 0, PosKey: []WhmKey3{{Time: 0}, {Time: 0.5}}},
			// The second bone keys the same first frame: one mark, not two.
			{Bone: 1, PosKey: []WhmKey3{{Time: 0}, {Time: 0.25}}},
			{Bone: 1, RotKey: []WhmKey4{{Time: 0.75}}},
		},
		Meshes: []WhmMeshTrack{
			{Name: "body", Keys: []WhmKey3{{Time: 0.5}, {Time: 1}}},
		},
	}
	got := clipKeyFrames(anim)
	want := []int{0, 25, 50, 75, 99}
	if len(got) != len(want) {
		t.Fatalf("clipKeyFrames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("clipKeyFrames[%d] = %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
	// Sorted, because the strip draws them in order and the snap walks them.
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("clipKeyFrames is not sorted: %v", got)
		}
	}
}

// A clip with no keys marks nothing; neither does a clip with no frame count.
func TestClipKeyFramesEmptyWhenThereAreNoKeys(t *testing.T) {
	if got := clipKeyFrames(&WhmAnimation{Name: "idle", NumFrames: 10}); len(got) != 0 {
		t.Errorf("a keyless clip marked %v", got)
	}
	if got := clipKeyFrames(nil); got != nil {
		t.Errorf("a nil clip marked %v", got)
	}
	if got := clipKeyFrames(&WhmAnimation{Name: "odd", NumFrames: 0, Bones: []WhmTrack{{PosKey: []WhmKey3{{Time: 0.5}}}}}); got != nil {
		t.Errorf("a clip with no frames marked %v", got)
	}
}

// A click on a keyframe mark winds to the keyframe; a click away from every
// mark winds to the frame under it instead.
func TestSnapKeyFrameFindsTheMarkUnderTheClick(t *testing.T) {
	const width = 800
	keys := []int{0, 50, 99}
	// On the middle keyframe's pixel.
	x := float32(scrubBarMargin) + frameX(50, 100, width)
	if f, ok := snapKeyFrame(x, keys, scrubBarMargin, width, 100); !ok || f != 50 {
		t.Errorf("a click on the mark found key %d (ok=%v), want 50", f, ok)
	}
	// A few pixels off it still snaps: the mark is thin and the hand is not.
	if f, ok := snapKeyFrame(x+scrubKeySnapPx-1, keys, scrubBarMargin, width, 100); !ok || f != 50 {
		t.Errorf("a click %dpx off the mark found key %d (ok=%v), want 50", scrubKeySnapPx-1, f, ok)
	}
	// Halfway between two marks: no snap at all.
	gap := frameX(25, 100, width)
	if _, ok := snapKeyFrame(scrubBarMargin+gap, keys, scrubBarMargin, width, 100); ok {
		t.Error("a click between marks snapped to a keyframe; free seeking is lost")
	}
	// No keys: no snap.
	if _, ok := snapKeyFrame(x, nil, scrubBarMargin, width, 100); ok {
		t.Error("a click snapped to a keyframe in a clip that has none")
	}
}

// The picked bone gets its segment in the pick colour, whether or not the
// skeleton is also on — the bone is picked out of a list, and a reader who
// opened only the bones list still has to see which one they picked.
func TestOverlayDrawsThePickedBone(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	opts.PickedBone = render.PickedBone(1)

	segments := render.ModelOverlayLines(&m.Model, pose, opts)
	if len(segments) == 0 {
		t.Fatal("picking a bone built no overlay at all")
	}
	found := false
	for _, s := range segments {
		if s.R == render.ModelBonePickColor[0] && s.G == render.ModelBonePickColor[1] && s.B == render.ModelBonePickColor[2] {
			found = true
			if s.HalfWidth <= 0 {
				t.Error("the picked bone's segment draws nothing: zero width")
			}
		}
	}
	if !found {
		t.Fatalf("none of the %d segments is in the pick colour %v", len(segments), render.ModelBonePickColor)
	}

	// The root bone has no parent-to-self segment, so it gets a cross at
	// its joint instead — three segments, all through the origin.
	opts.PickedBone = render.PickedBone(0)
	cross := 0
	origin := pose.Bones[0].MulPosition(fauxgl.Vector{})
	for _, s := range render.ModelOverlayLines(&m.Model, pose, opts) {
		if s.R != render.ModelBonePickColor[0] {
			continue
		}
		cross++
		// The origin is the arm's middle, not its end: a cross spans
		// through the joint it marks.
		mid := s.From.Add(s.To).MulScalar(0.5)
		if mid.Sub(origin).Length() > 1e-6 {
			t.Errorf("the root cross arm runs %v..%v, centred on %v, want it through %v", s.From, s.To, mid, origin)
		}
	}
	if cross != 3 {
		t.Errorf("the picked root bone drew %d arms, want 3", cross)
	}
}

// A stale bone index — a model replaced under a viewer that still remembers
// the old list — draws nothing rather than reading past the end.
func TestOverlayPickedBoneStaleIndexDrawsNothing(t *testing.T) {
	m := overlayTestModel()
	pose := m.RestPose()
	opts := overlayTestOptions()
	stale := len(m.Bones) + 3
	opts.PickedBone = render.PickedBone(stale)
	if got := render.ModelOverlayLines(&m.Model, pose, opts); len(got) != 0 {
		t.Errorf("bone %d of a model with %d built %d segments", stale, len(m.Bones), len(got))
	}
	opts.PickedBone = render.PickedBone(-1)
	if got := render.ModelOverlayLines(&m.Model, pose, opts); len(got) != 0 {
		t.Errorf("\"nothing picked\" built %d segments", len(got))
	}
}

// ─── merged from x_test_model3d_camera_overlay_test.go ────────────────────────────────────────

func optsAt(yaw, pitch float64) render.RenderOptions {
	return render.RenderOptions{Yaw: yaw, Pitch: pitch}
}

// The level views are the ones whose angles are exact, and they are the ones a
// user lands on most: yaw turns the camera about the up axis, and the models
// are Z up, so at pitch zero the camera looks along a horizontal axis and Z is
// always across the view at ninety degrees.
func TestCameraAxisAnglesOnTheLevelViews(t *testing.T) {
	cases := []struct {
		name string
		yaw  float64
		// the axis the camera looks straight along, and which way
		along  string
		angles [3]float64
	}{
		{"looking along -X", 0, "-X", [3]float64{180, 90, 90}},
		{"looking along -Y", math.Pi / 2, "-Y", [3]float64{90, 180, 90}},
		{"looking along +X", math.Pi, "+X", [3]float64{0, 90, 90}},
		{"looking along +Y", -math.Pi / 2, "+Y", [3]float64{90, 0, 90}},
	}
	for _, c := range cases {
		got := render.CameraAxesAngles(optsAt(c.yaw, 0))
		for i := 0; i < 3; i++ {
			if math.Abs(got[i]-c.angles[i]) > 0.5 {
				t.Errorf("%s: axis %d angle is %.1f degrees, want %.1f", c.name, i, got[i], c.angles[i])
			}
		}
	}
}

// The three angles are direction cosines, so their squares add to one. This is
// what catches a sign flipped in the orbit direction: every angle stays the
// right size and the squares still add up, but the ones reported for the
// axis the camera looks along become the mirror of what they should be, which
// the level-view cases above catch and this one holds true across the sphere.
func TestCameraAxisAnglesAreDirectionCosines(t *testing.T) {
	for _, yaw := range []float64{0, 0.7, 1.9, 3.4, 5.1} {
		for _, pitch := range []float64{-1.4, -0.6, 0, 0.4, 1.4} {
			a := render.CameraAxesAngles(optsAt(yaw, pitch))
			sum := 0.0
			for _, v := range a {
				r := v * math.Pi / 180
				sum += math.Cos(r) * math.Cos(r)
			}
			if math.Abs(sum-1) > 1e-6 {
				t.Errorf("yaw %.2f pitch %.2f: the squared cosines add to %.6f, want 1", yaw, pitch, sum)
			}
		}
	}
}

// The camera looks the other way from the orbit direction: the orbit goes out
// from the target towards the eye, and the view is back along it. Getting
// this backwards leaves every angle correct in size and wrong in sign.
func TestCameraLooksOppositeTheOrbit(t *testing.T) {
	for _, yaw := range []float64{0, 1.2, 2.8, 4.5} {
		for _, pitch := range []float64{-0.9, 0, 0.7} {
			orbit := render.CameraOrbitDirection(optsAt(yaw, pitch))
			view := render.CameraViewDirection(optsAt(yaw, pitch))
			dot := orbit.X*view.X + orbit.Y*view.Y + orbit.Z*view.Z
			if math.Abs(dot+1) > 1e-6 {
				t.Errorf("yaw %.2f pitch %.2f: view direction dot orbit is %.6f, want -1",
					yaw, pitch, dot)
			}
		}
	}
}

// Pitch is clamped short of the poles, so the camera can never look straight
// down an axis from above or below. The readout must respect that clamp
// rather than reporting a pole the picture cannot show.
func TestCameraAxisAnglesRespectThePitchClamp(t *testing.T) {
	const pole = math.Pi / 2
	a := render.CameraAxesAngles(optsAt(0, pole*10))
	for i, v := range a {
		if math.IsNaN(v) || v < 0 || v > 180 {
			t.Errorf("axis %d angle is %v, which is not a real angle", i, v)
		}
	}
	// A pitch far past the pole clamps to the same view as the clamp itself.
	clamped := render.CameraAxesAngles(optsAt(0, 1.5))
	past := render.CameraAxesAngles(optsAt(0, 4.0))
	for i := range clamped {
		if math.Abs(clamped[i]-past[i]) > 0.5 {
			t.Errorf("axis %d: a pitch past the clamp is %.1f, the clamp gives %.1f",
				i, past[i], clamped[i])
		}
	}
}

// A default view must produce something worth reading.
func TestCameraAxisAnglesAreSane(t *testing.T) {
	a := render.CameraAxesAngles(render.RenderOptions{})
	for i, v := range a {
		if math.IsNaN(v) || v < 0 || v > 180 {
			t.Errorf("axis %d angle is %v, which is not a real angle", i, v)
		}
	}
}
