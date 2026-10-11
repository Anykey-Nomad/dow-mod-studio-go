package main

import (
	"os"
	"path/filepath"
)

type moduleCandidate struct {
	path   string
	folder string
}

const steamDowFolder = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`

// discoverModuleFiles lists the .module files directly inside the folders.
func discoverModuleFiles(dirs []string) []moduleCandidate {
	var modules []moduleCandidate
	for _, r := range listPickFiles(dirs, func(name string) bool { return pickExt(name) == ".module" }) {
		modules = append(modules, moduleCandidate{path: r.path, folder: r.folder})
	}
	return modules
}

// moduleRoots are the folders the Open Module dialog starts from.
func (ui *uiState) moduleRoots() []string {
	dirs := make([]string, 0, 6)
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs,
		`C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`,
		`C:\Program Files (x86)\Steam\steamapps\common\Dawn of War Definitive Edition`,
	)
	if ui.gamePath != "" {
		dirs = append(dirs, ui.gamePath)
	}
	dirs = append(dirs, ui.recentFolders...)
	if ui.modulePath != "" {
		dirs = append(dirs, filepath.Dir(ui.modulePath))
	}
	return dirs
}

// openModulePicker shows the Open Module dialog over the .module files.
func (ui *uiState) openModulePicker() {
	ui.openFilePicker(pickConfig{
		title:    "Open Module",
		subtitle: "Enter a .module file or the Dawn of War folder to scan",
		hint:     `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`,
		exts:     []string{".module"},
		roots:    ui.moduleRoots,
		start:    ui.modulePath,
		onPick: func(path string) (bool, string) {
			path = ui.resolveModulePath(path)
			if !pathExists(path) {
				return false, "No such module file: " + path
			}
			ui.loadModule(path)
			return true, ""
		},
	})
}

// resolveModulePath finds a module named relative to the app or to the game
// folder, which is where a bare name typed in the dialog is looked for.
func (ui *uiState) resolveModulePath(path string) string {
	if !filepath.IsAbs(path) && pickExt(path) == ".module" {
		if ui.modTree != nil && ui.modTree.AppPath != "" {
			if cand := filepath.Join(ui.modTree.AppPath, path); pathExists(cand) {
				return cand
			}
		}
		if !pathExists(path) {
			if cand := filepath.Join(steamDowFolder, path); pathExists(cand) {
				return cand
			}
		}
	}
	if !pathExists(path) {
		if cand := filepath.Join(steamDowFolder, path+".module"); pathExists(cand) {
			return cand
		}
	}
	return path
}
