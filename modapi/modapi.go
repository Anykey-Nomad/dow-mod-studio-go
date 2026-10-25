// Package modapi is the public face of the module tooling.
//
// The module code itself lives in internal/modtree, and Go forbids importing
// an internal package from another module. This package re-exports the parts
// of it that other programs need: finding the .module files of a Dawn of War
// installation, parsing them, and building the merged file tree of a module.
package modapi

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
)

// ModFile — parsed .module descriptor.
type ModFile = modtree.ModFile

// ModTree — merged file tree of a module: its own files, the files of the
// mods it requires, and the engine data, in one tree.
type ModTree = modtree.ModTree

// TreeFile — one file of the tree, with the source it came from.
type TreeFile = modtree.TreeFile

// TreeFolder — one folder of the tree.
type TreeFolder = modtree.TreeFolder

// Module — a .module file found on disk together with what it declares.
type Module struct {
	// Path — absolute path of the .module file.
	Path string
	// Name — base name of the file without the .module extension.
	Name string
	// UIName — name the game shows in the module list.
	UIName string
	// Description — one line description of the module.
	Description string
	// Version — version string of the module.
	Version string
	// ModFolder — folder of the module the game maps its data from.
	ModFolder string
}

// ParseModule reads a .module file.
func ParseModule(path string) (*ModFile, error) {
	return modtree.ParseModule(path)
}

// BuildModTree builds the merged file tree of the module at modulePath.
// The locale is the language folder the module asks for, "English" by default.
func BuildModTree(modulePath, locale string) (*ModTree, error) {
	return modtree.BuildModTree(modulePath, locale)
}

// DiscoverModules lists the .module files that sit directly in root.
//
// A module that cannot be parsed is skipped: the file may belong to a mod
// from another game or be half written, and one broken file must not hide
// the modules around it.
func DiscoverModules(root string) ([]Module, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var modules []Module
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".module") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		mf, err := ParseModule(path)
		if err != nil {
			continue
		}
		modules = append(modules, Module{
			Path:        path,
			Name:        mf.FileMapName,
			UIName:      mf.UIName,
			Description: mf.Description,
			Version:     mf.ModVersion,
			ModFolder:   mf.ModFolder,
		})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	return modules, nil
}

// FindModule returns the module of the list with the given name, matched
// without regard to case, or nil when there is none.
func FindModule(modules []Module, name string) *Module {
	for i := range modules {
		if strings.EqualFold(modules[i].Name, name) {
			return &modules[i]
		}
	}
	return nil
}

// FilesInFolder returns the files of a virtual folder of the tree, sorted by
// name. The path is matched without regard to case, as the game does.
func FilesInFolder(tree *ModTree, virtualPath string) []*TreeFile {
	folder := tree.FindFolder(virtualPath)
	if folder == nil {
		return nil
	}
	files := make([]*TreeFile, 0, len(folder.Files))
	files = append(files, folder.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}
