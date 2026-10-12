package main

import (
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// loadModelMaterials resolves every mesh material of a model to its .rsh
// diffuse map. A .whm names its maps by a full path, by a bare file name, or
// with a folder the game does not keep them in, and the texture is often in a
// different mod than the model — game art is shared between factions.
func (ui *uiState) loadModelMaterials(v *modelViewer, result chan<- modelMaterialResult) {
	if v == nil || v.model == nil || ui.modTree == nil {
		return
	}
	v.loading = true
	needed := map[string]bool{}
	for _, mesh := range v.model.Meshes {
		if mesh.Material != "" {
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
	// Vulkan uploads the block-compressed mip chain directly, preserving alpha.
	raw := true
	go func() {
		result <- decodeModelMaterials(resolveModelMaterials(tree, roots, model, names), raw)
	}()
}

// resolveModelMaterials first checks authored paths, then searches the merged
// virtual tree (including parent-mod and game SGA archives), and finally disk.
func resolveModelMaterials(tree *modtree.ModTree, roots []string, modelFile string, names []string) map[string][]byte {
	files := map[string][]byte{}
	var missing []string
	for _, name := range names {
		data, ok := readModelMaterial(tree, roots, modelFile, name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		files[name] = data
	}
	for name, file := range findMaterialsInTree(tree, missing) {
		if data, err := tree.ReadTreeFile(file); err == nil {
			files[name] = data
		}
	}
	for name, path := range findMaterialsOnDisk(roots, missing) {
		if _, alreadyFound := files[name]; alreadyFound {
			continue
		}
		if data, err := os.ReadFile(path); err == nil {
			files[name] = data
		}
	}
	return files
}

// decodeModelMaterials retains compressed bytes for Vulkan and decoded images
// for the software renderer used by diagnostic/fallback paths.
func decodeModelMaterials(files map[string][]byte, raw bool) modelMaterialResult {
	res := modelMaterialResult{
		materials: map[string]WhmMaterial{},
		rsh:       map[string][]byte{},
	}
	for name, data := range files {
		if raw {
			if _, _, err := raster.RshRawImage(data, 0); err != nil {
				continue
			}
			res.rsh[name] = data
		}
		img, _, err := decodeRshImage(data, 0)
		if err != nil {
			continue
		}
		res.materials[name] = WhmMaterial{Texture: newModelTexture(img), Color: rgbaWhite}
	}
	if raw {
		// A Vulkan texture result also keeps a decoded view for the inspector.
		res.missing = len(files) - len(res.rsh)
	} else {
		res.missing = len(files) - len(res.materials)
	}
	return res
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
		base := strings.ToLower(fx.MaterialBaseName(name) + ".rsh")
		if base != ".rsh" {
			want[base] = true
		}
	}
	found := findMaterialFilesInTree(tree, want)
	out := make(map[string]*modtree.TreeFile, len(found))
	for _, name := range names {
		if file := found[strings.ToLower(fx.MaterialBaseName(name)+".rsh")]; file != nil {
			out[name] = file
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

// readModelMaterial resolves one .whm material reference to raw RSH bytes.
func readModelMaterial(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, bool) {
	if tree != nil {
		if data, err := tree.ReadFile(name + ".rsh"); err == nil {
			return data, true
		}
	}
	rel := filepath.FromSlash(name)
	for _, root := range roots {
		if data, err := os.ReadFile(filepath.Join(root, rel+".rsh")); err == nil {
			return data, true
		}
	}
	base := fx.MaterialBaseName(name)
	if base == "" {
		return nil, false
	}
	if dir := filepath.Dir(modelFile); dir != "" && dir != "." {
		if tree != nil {
			if data, err := tree.ReadFile(filepath.Join(dir, base+".rsh")); err == nil {
				return data, true
			}
		}
		for _, root := range roots {
			if data, err := os.ReadFile(filepath.Join(root, dir, base+".rsh")); err == nil {
				return data, true
			}
		}
	}
	if tree != nil {
		if file := tree.FindMaterialByName(base + ".rsh"); file != nil {
			if data, err := tree.ReadTreeFile(file); err == nil {
				return data, true
			}
		}
		if file := findMaterialInTree(tree, base+".rsh"); file != nil {
			if data, err := tree.ReadTreeFile(file); err == nil {
				return data, true
			}
		}
	}
	return nil, false
}

// findMaterialsOnDisk answers remaining references with one walk per root.
func findMaterialsOnDisk(roots []string, names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	want := map[string]string{}
	for _, name := range names {
		base := fx.MaterialBaseName(name)
		if base != "" {
			want[strings.ToLower(base+".rsh")] = name
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
			if name, ok := want[strings.ToLower(d.Name())]; ok {
				found[name] = path
				delete(want, strings.ToLower(d.Name()))
			}
			return nil
		})
	}
	return found
}
