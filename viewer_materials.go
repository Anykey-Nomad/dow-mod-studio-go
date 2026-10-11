package main

import (
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// loadModelMaterials resolves every mesh material of a model to its .rsh
// diffuse map. A .whm names its maps by a full path, by a bare file name, or
// with a folder the game does not keep them in, and the texture is often in a
// different mod than the model — game art is shared between factions. The order
// is the one an author would expect: the mod being worked on first, then the
// data folders the model itself sits in, and the parent mod last. Anything
// still missing simply keeps the flat colour.
//
// Both forms of the file are kept: the bytes as they are, which is what the
// hardware renderer uploads, and the decoded picture, which is what the
// software renderer samples. Both are needed at once — showing the skeleton
// falls back to the software renderer on a window that has a device — and
// decoding a .rsh is a fraction of the cost of the model it belongs to.
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
	// The hardware renderer wants the file as it is, so whether the bytes are
	// kept is asked for here, on the UI goroutine, because it reads the UI.
	raw := ui.modelGpuRenderer() != nil
	go func() {
		result <- decodeModelMaterials(resolveModelMaterials(tree, roots, model, names), raw)
	}()
}

// resolveModelMaterials finds the .rsh of every material a model names. The
// names nothing else answered are looked for in a single walk of the data
// folders, which is what makes the last resort affordable: a walk each left
// the loader crawling the game folder once per texture.
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
	for name, path := range findMaterialsOnDisk(roots, missing) {
		if data, err := os.ReadFile(path); err == nil {
			files[name] = data
		}
	}
	return files
}

// decodeModelMaterials turns resolved .rsh files into the two forms a renderer
// wants: the bytes as they are for the hardware path, which uploads the block
// compressed mip chain itself, and the picture for the software path. A file
// that decodes to nothing is counted as missing, since that is what it is.
func decodeModelMaterials(files map[string][]byte, raw bool) modelMaterialResult {
	res := modelMaterialResult{
		materials: map[string]WhmMaterial{},
		rsh:       map[string][]byte{},
	}
	for name, data := range files {
		img, _, err := decodeRshImage(data, 0)
		if err != nil {
			continue
		}
		res.materials[name] = WhmMaterial{Texture: newModelTexture(img), Color: rgbaWhite}
		if raw {
			res.rsh[name] = data
		}
	}
	res.missing = len(files) - len(res.materials)
	return res
}

// materialSearchRoots lists the on disk data folders that can hold a material,
// most specific first.
func materialSearchRoots(tree *modtree.ModTree) []string {
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

// readModelMaterial resolves one material reference to its raw bytes.
//
// Most references are a full path such as
// "art/ebps/races/necrons/texture_share/necron_lord01", but plenty of models
// name the map with no folder at all. A bare name is looked for beside the
// model that wants it first — the game keeps a model's maps in its own folder
// more often than not — and in the mod's own folder before the loaded data
// folders after it. A name that is still missing is left to the caller, which
// searches every root in one pass rather than one pass per texture.
func readModelMaterial(tree *modtree.ModTree, roots []string, modelFile, name string) ([]byte, bool) {
	if data, err := tree.ReadFile(name + ".rsh"); err == nil {
		return data, true
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
	// Beside the model itself, in the mod folder and in the tree alike.
	if dir := filepath.Dir(modelFile); dir != "" && dir != "." {
		if data, err := tree.ReadFile(filepath.Join(dir, base+".rsh")); err == nil {
			return data, true
		}
		for _, root := range roots {
			if data, err := os.ReadFile(filepath.Join(root, dir, base+".rsh")); err == nil {
				return data, true
			}
		}
	}
	// The folder the reference names is not the one the game keeps it in: look
	// the file up by name alone.
	if f := tree.FindByName(base + ".rsh"); f != nil && f.Source != nil {
		if data, err := tree.ReadTreeFile(f); err == nil {
			return data, true
		}
	}
	return nil, false
}

// findMaterialsOnDisk is the last resort for the references nothing else
// resolved: one walk of every data folder answers all of them at once. The
// roots are already in order, so the first root that has a file wins, and a
// name is only looked for until it is found.
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
				return nil //nolint:nilerr // an unreadable folder just means no match
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
