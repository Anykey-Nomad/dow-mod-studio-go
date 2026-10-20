package main

import (
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/whm"
	"encoding/binary"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// modelMaterialDiagnostic is what the material inspector can tell about a
// material reference, whether resolution and decoding succeeded or not.
type modelMaterialDiagnostic struct {
	ResolvedPath string
	Format       string
	Status       string
}

// loadModelMaterials resolves every mesh material of a model to its diffuse
// map. A .whm may name a .rsh, a compiled .rtx, or a bare base name; the
// texture is often in a different mod than the model, so shared game art and
// files beside the model are both part of the lookup.
func (ui *uiState) loadModelMaterials(v *modelViewer, result chan<- modelMaterialResult) {
	if v == nil || v.model == nil || ui.modTree == nil {
		return
	}
	v.loading = true
	needed := map[string]bool{}
	for _, mesh := range v.model.Meshes {
		if mesh.Material != "" {
			// Keep results under the exact key the renderer uses. The resolver
			// follows MaterialPath via the model's shared-shader channel, but
			// returns the bytes keyed by this mesh material label.
			needed[mesh.Material] = true
		}
	}
	names := make([]string, 0, len(needed))
	for name := range needed {
		names = append(names, name)
	}
	tree := ui.modTree
	model := v.modelFile
	roots := materialSearchRoots(tree)
	// Vulkan uploads block-compressed RSH mip chains directly. RTX is
	// decoded as a compiled image instead, preserving the GPU preview by
	// uploading it as an in-memory diffuse texture.
	raw := true
	go func() {
		files, paths := resolveModelMaterialsWithPaths(tree, roots, model, names)
		decoded := decodeModelMaterials(files, raw)
		attachMaterialSourcePaths(decoded.diagnostics, paths)
		for _, name := range names {
			if _, ok := decoded.diagnostics[name]; !ok {
				decoded.diagnostics[name] = modelMaterialDiagnostic{Format: "—", Status: "not found"}
			}
		}
		result <- decoded
	}()
}

// resolveModelMaterials first checks authored paths, then searches the merged
// virtual tree (including parent-mod and game SGA archives), and finally disk.
func resolveModelMaterials(tree *modtree.ModTree, roots []string, modelFile string, names []string) map[string][]byte {
	files, _ := resolveModelMaterialsWithPaths(tree, roots, modelFile, names)
	return files
}

// resolveModelMaterialsWithPaths resolves each material once and keeps the
// actual source path alongside the bytes, including the archive/container
// boundary for assets read from SGA files.
func resolveModelMaterialsWithPaths(tree *modtree.ModTree, roots []string, modelFile string, names []string) (map[string][]byte, map[string]string) {
	files := map[string][]byte{}
	paths := map[string]string{}
	var missing []string
	for _, name := range names {
		reference := modelMaterialReference(tree, modelFile, name)
		data, path, ok := readModelMaterialWithPath(tree, roots, modelFile, name)
		if reference != "" && !strings.EqualFold(reference, name) {
			// The display/material label is not necessarily a real path or
			// filename (for example, "necron warrior"). Try the shared
			// shader's diffuse reference first, then retain the label as the
			// returned key so the draw can find it.
			data, path, ok = readModelMaterialWithPath(tree, roots, modelFile, reference)
		}
		if !ok {
			missing = append(missing, name)
			continue
		}
		files[name], paths[name] = data, path
	}
	for name, file := range findMaterialsInTree(tree, missing) {
		if data, err := tree.ReadTreeFile(file); err == nil {
			files[name], paths[name] = data, materialTreePath(file)
		}
	}
	for name, path := range findMaterialsOnDisk(roots, missing) {
		if _, alreadyFound := files[name]; alreadyFound {
			continue
		}
		if data, err := os.ReadFile(path); err == nil {
			files[name], paths[name] = data, path
		}
	}
	return files, paths
}

func modelMaterialReference(tree *modtree.ModTree, modelFile, material string) string {
	if tree == nil || modelFile == "" {
		return ""
	}
	data, err := tree.ReadFile(modelFile)
	if err != nil {
		return ""
	}
	model, err := whm.Parse(data)
	if err != nil {
		return ""
	}
	key := strings.ToLower(strings.TrimSpace(material))
	if reference := model.MaterialPaths[key]; reference != "" {
		return reference
	}
	// Some WHMs only give parseMeshes a positional FOLDSTXT fallback when
	// there is no shader-name mapping. Preserve that material-to-path link
	// too, so display labels still resolve to the canonical diffuse.
	for _, mesh := range model.Meshes {
		if strings.EqualFold(strings.TrimSpace(mesh.Material), strings.TrimSpace(material)) && mesh.MaterialPath != "" {
			return mesh.MaterialPath
		}
	}
	return ""
}

// attachMaterialSourcePaths pairs decoder results with resolved source paths
// without decoding large files a second time.
func attachMaterialSourcePaths(diagnostics map[string]modelMaterialDiagnostic, paths map[string]string) {
	for name, diag := range diagnostics {
		diag.ResolvedPath = paths[name]
		diagnostics[name] = diag
	}
}

func rshMaterialFormat(format int32) string {
	switch format {
	case 8:
		return "RSH / DXT1 (BC1)"
	case 10:
		return "RSH / DXT3 (BC2)"
	case 11:
		return "RSH / DXT5 (BC3)"
	default:
		return fmt.Sprintf("RSH / format %d", format)
	}
}

func decodedMaterialFormat(format string, data []byte) string {
	format = strings.ToLower(format)
	if format != "rtx" {
		return strings.ToUpper(format)
	}
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	var find func([]*chunky.Chunk) string
	find = func(chunks []*chunky.Chunk) string {
		for _, chunk := range chunks {
			if chunk.TypeID == "FOLDIMAG" {
				attr := chunk.Child("DATAATTR")
				if attr != nil && len(attr.Payload) >= 4 {
					switch int(binary.LittleEndian.Uint32(attr.Payload[:4])) {
					case 8:
						return "RTX / DXT1 (BC1)"
					case 10:
						return "RTX / DXT3 (BC2)"
					case 11:
						return "RTX / DXT5 (BC3)"
					case 0, 2:
						return "RTX / TGA"
					}
				}
			}
			if nested := find(chunk.Children); nested != "" {
				return nested
			}
		}
		return "RTX"
	}
	return find(chunks)
}

// decodeModelMaterials retains compressed bytes for Vulkan and decoded images
// for the software renderer used by diagnostic/fallback paths.
func decodeModelMaterials(files map[string][]byte, raw bool) modelMaterialResult {
	res := modelMaterialResult{
		materials:   map[string]WhmMaterial{},
		rsh:         map[string][]byte{},
		diagnostics: map[string]modelMaterialDiagnostic{},
	}
	for name, data := range files {
		diag := modelMaterialDiagnostic{Status: "unrecognized image data"}
		rshInfo, _, rshErr := raster.RshRawImage(data, 0)
		var img image.Image
		var err error
		if rshErr == nil {
			diag.Format = rshMaterialFormat(rshInfo.Format)
			if raw {
				res.rsh[name] = data
			}
			img, _, err = decodeRshImage(data, 0)
		} else {
			// Texture references in WHM files often omit an extension. The
			// resolved file may therefore be RTX even though the reference key
			// itself has none; detect supported compiled formats by decoding.
			for _, ext := range []string{"rtx", "dds", "tga"} {
				img, _, err = raster.DecodeImageData(data, ext)
				if err == nil && img != nil {
					diag.Format = decodedMaterialFormat(ext, data)
					break
				}
			}
		}
		if err == nil && img != nil {
			diag.Status = "decoded"
			res.materials[name] = WhmMaterial{Texture: newModelTexture(img), Color: rgbaWhite}
		} else if rshErr != nil {
			diag.Status = "decode failed"
		}
		res.diagnostics[name] = diag
	}
	for name := range files {
		if _, ok := res.materials[name]; !ok {
			res.missing++
		}
	}
	return res
}

// materialTextureExtensions are tried in the game's effective order: the
// shared material format first, then compiled textures (which some models name
// implicitly) and the source-friendly interchange formats.
var materialTextureExtensions = []string{".rsh", ".rtx", ".dds", ".tga"}

func materialCandidatePaths(name string) []string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" {
		return []string{name}
	}
	out := make([]string, 0, len(materialTextureExtensions))
	for _, suffix := range materialTextureExtensions {
		out = append(out, name+suffix)
	}
	return out
}

func materialCandidateNames(name string) []string {
	paths := materialCandidatePaths(name)
	out := make([]string, 0, len(paths))
	for _, candidate := range paths {
		out = append(out, fx.MaterialBaseName(candidate))
	}
	return out
}

// materialDiskPaths returns both useful interpretations of a virtual path:
// roots may be the mod root (which needs the Data prefix) or the Data folder
// itself (which does not).
func materialDiskPaths(root, candidate string) []string {
	rel := filepath.FromSlash(strings.ReplaceAll(candidate, "\\", "/"))
	paths := []string{filepath.Join(root, rel)}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > 1 && strings.EqualFold(parts[0], "Data") {
		paths = append(paths, filepath.Join(root, filepath.Join(parts[1:]...)))
	}
	return paths
}

// findMaterialsInTree returns the best merged-tree file for each requested
// material base name. Current-mod files win within texture_share; texture_share
// files win over same-named files elsewhere.
func findMaterialsInTree(tree *modtree.ModTree, names []string) map[string]*modtree.TreeFile {
	if tree == nil || tree.Root == nil || len(names) == 0 {
		return nil
	}
	want := make(map[string]bool, len(names))
	for _, name := range names {
		for _, fileName := range materialCandidateNames(name) {
			want[strings.ToLower(fileName)] = true
		}
	}
	found := findMaterialFilesInTree(tree, want)
	out := make(map[string]*modtree.TreeFile, len(found))
	for _, name := range names {
		for _, candidate := range materialCandidateNames(name) {
			if file := found[strings.ToLower(candidate)]; file != nil {
				out[name] = file
				break
			}
		}
	}
	return out
}

func findMaterialInTree(tree *modtree.ModTree, fileName string) *modtree.TreeFile {
	if tree == nil || tree.Root == nil || fileName == "" {
		return nil
	}
	return findMaterialFilesInTree(tree, map[string]bool{strings.ToLower(fileName): true})[strings.ToLower(fileName)]
}

func findMaterialFilesInTree(tree *modtree.ModTree, want map[string]bool) map[string]*modtree.TreeFile {
	found := make(map[string]*modtree.TreeFile, len(want))
	var walk func(*modtree.TreeFolder, bool)
	walk = func(folder *modtree.TreeFolder, textureShare bool) {
		textureShare = textureShare || strings.EqualFold(folder.Name, "texture_share")
		for _, file := range folder.Files {
			key := strings.ToLower(file.Name)
			if !want[key] || file.Source == nil {
				continue
			}
			old, exists := found[key]
			oldTexture := exists && treePathInFolder(old.FullPath, "texture_share")
			if !exists || (textureShare && !oldTexture) || (textureShare == oldTexture && sourceMoreSpecific(file, old)) {
				found[key] = file
			}
		}
		for _, child := range folder.Children {
			walk(child, textureShare)
		}
	}
	walk(tree.Root, false)
	return found
}

func sourceMoreSpecific(candidate, current *modtree.TreeFile) bool {
	if candidate == nil || candidate.Source == nil {
		return false
	}
	if current == nil || current.Source == nil {
		return true
	}
	return candidate.Source.Priority < current.Source.Priority
}

func treePathInFolder(path, folder string) bool {
	for _, part := range strings.Split(strings.ToLower(strings.ReplaceAll(path, "/", "\\")), `\`) {
		if part == folder {
			return true
		}
	}
	return false
}

// materialSearchRoots lists on-disk data folders, most specific first.
func materialSearchRoots(tree *modtree.ModTree) []string {
	if tree == nil {
		return nil
	}
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		seen[clean] = true
		roots = append(roots, clean)
	}
	if tree.ModFolder != "" && tree.AppPath != "" {
		add(filepath.Join(tree.AppPath, tree.ModFolder))
	}
	for _, src := range tree.Sources {
		if !src.IsSGA {
			add(src.DiskRoot)
		}
	}
	return roots
}

// readModelMaterial resolves one .whm material reference to image bytes.
func readModelMaterial(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, bool) {
	data, _, ok := readModelMaterialWithPath(tree, roots, modelFile, name)
	return data, ok
}

func readModelMaterialWithPath(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, string, bool) {
	if tree != nil {
		for _, candidate := range materialCandidatePaths(name) {
			if file := tree.FindFile(candidate); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, materialTreePath(file), true
				}
			}
		}
	}
	for _, root := range roots {
		for _, candidate := range materialCandidatePaths(name) {
			for _, path := range materialDiskPaths(root, candidate) {
				if data, err := os.ReadFile(path); err == nil {
					return data, path, true
				}
			}
		}
	}
	base := fx.MaterialBaseName(name)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		return nil, "", false
	}
	modelPath := strings.ReplaceAll(modelFile, "\\", "/")
	if dir := filepath.Dir(modelPath); dir != "" && dir != "." {
		for _, candidate := range materialCandidatePaths(filepath.ToSlash(filepath.Join(dir, base))) {
			if tree != nil {
				if file := tree.FindFile(candidate); file != nil {
					if data, err := tree.ReadTreeFile(file); err == nil {
						return data, materialTreePath(file), true
					}
				}
			}
			for _, root := range roots {
				for _, path := range materialDiskPaths(root, candidate) {
					if data, err := os.ReadFile(path); err == nil {
						return data, path, true
					}
				}
			}
		}
	}
	if tree != nil {
		for _, ext := range materialTextureExtensions {
			fileName := base + ext
			if file := tree.FindMaterialByName(fileName); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, materialTreePath(file), true
				}
			}
			if file := findMaterialInTree(tree, fileName); file != nil {
				if data, err := tree.ReadTreeFile(file); err == nil {
					return data, materialTreePath(file), true
				}
			}
		}
	}
	return nil, "", false
}

func materialTreePath(file *modtree.TreeFile) string {
	if file == nil {
		return ""
	}
	if file.Source != nil && file.Source.IsSGA {
		return file.Source.ArchivePath + " :: " + file.FullPath
	}
	if file.DiskPath != "" {
		return file.DiskPath
	}
	return file.FullPath
}

// findMaterialsOnDisk answers remaining references with one walk per root.
func findMaterialsOnDisk(roots []string, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	want := map[string][]string{}
	for _, name := range names {
		for _, fileName := range materialCandidateNames(name) {
			key := strings.ToLower(fileName)
			want[key] = append(want[key], name)
		}
	}
	found := map[string]string{}
	for _, root := range roots {
		if len(want) == 0 {
			break
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || len(want) == 0 {
				return nil //nolint:nilerr // unreadable folders contain no result
			}
			if d.IsDir() {
				return nil
			}
			if names, ok := want[strings.ToLower(d.Name())]; ok {
				for _, name := range names {
					found[name] = path
				}
				delete(want, strings.ToLower(d.Name()))
			}
			return nil
		})
	}
	return found
}
