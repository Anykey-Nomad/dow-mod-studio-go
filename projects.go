package main

import (
	"dow-mod-studio/internal/modtree"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── projects folder ────────────────────────────────────────────────────────
//
// Sits next to the recycle bin, under the program's own data folder:
//
//	%AppData%\DoW Mod Studio\
//	    recycle\
//	    Projects\
//	        DXP3\
//	            Units\
//
// Opening a module makes sure its own folder and its Units folder exist. A
// later step writes the unit interfaces there, which is why the tree is laid
// out up front rather than on first use.

const (
	projectsDirName = "Projects"
	// unitsDirName is the subfolder every project's unit data goes into.
	unitsDirName = "Units"
)

// programDataDir is the program's own folder, the parent of both the recycle
// bin and the projects tree.
func programDataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "DoW Mod Studio"), nil
}

// projectsDir is the root of the per-mod project tree.
func projectsDir() (string, error) {
	data, err := programDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, projectsDirName), nil
}

// sanitizeFolderName turns a mod name into something safe to use as a
// directory name. A mod's name comes from its .module file and is not
// guaranteed to be a valid path component: it can hold a slash, a colon or a
// reserved character, and two different names must not collapse onto the same
// folder.
func sanitizeFolderName(name string) string {
	name = strings.TrimSpace(name)
	// Drop any directory part: only the last component is the mod's name.
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".module")
	name = strings.TrimSuffix(name, ".MODULE")
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20, r == 0x7f:
			// Control characters have no place in a path.
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	// Windows refuses these names regardless of extension, and a folder called
	// "." or ".." would walk back up the tree.
	switch strings.ToUpper(strings.Trim(out, ". ")) {
	case "", ".", "..", "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "mod"
	}
	if len(out) > 96 {
		out = out[:96]
	}
	return out
}

// modProjectName is the folder name a module gets. The tree's own name is used
// because it is what the rest of the app already treats as the mod's identity;
// the module file's path is the fallback for a tree that never got one.
func modProjectName(tree *modtree.ModTree, modulePath string) string {
	if tree != nil && tree.FileMapName != "" {
		return tree.FileMapName
	}
	if modulePath != "" {
		return filepath.Base(modulePath)
	}
	return ""
}

// projectDir is one mod's folder, without creating anything.
func projectDir(modName string) (string, error) {
	root, err := projectsDir()
	if err != nil {
		return "", err
	}
	name := sanitizeFolderName(modName)
	if name == "" {
		return "", fmt.Errorf("empty project name")
	}
	return filepath.Join(root, name), nil
}

// ensureProjectDirs creates Projects\<mod>\Units and returns the mod's folder.
// It is safe to call on every module open: an existing tree is left alone.
func ensureProjectDirs(tree *modtree.ModTree, modulePath string) (string, error) {
	modName := modProjectName(tree, modulePath)
	dir, err := projectDir(modName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, unitsDirName), 0755); err != nil {
		return "", err
	}
	return dir, nil
}
