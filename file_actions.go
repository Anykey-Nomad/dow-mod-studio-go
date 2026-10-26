package main

import (
	"dow-mod-studio/internal/modtree"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The file formats a module opens in the built-in text editor. It is a
// starting point rather than a rule: Settings lets a reader add their own
// through a comma and space separated list, which is what isRead, isEditorFile
// and openTreeFile all go by.
var defaultTextFormats = []string{".lua", ".txt", ".ai", ".scar", ".rat", ".con"}

// defaultTextFormatList is defaultTextFormats as the Settings field shows them.
const defaultTextFormatList = ".lua, .txt, .ai, .scar, .rat, .con"

// parseTextFormats reads a list of file extensions the way a person types it:
// separated by commas, spaces or both, in any order, written with or without
// the leading dot. Entries are compared case-insensitively and returned in
// lower case with a dot, which is the form filepath.Ext produces.
//
// An empty list is not an error and not "nothing opens": it falls back to the
// defaults, so clearing the field cannot lock the editor out of every file.
func parseTextFormats(s string) []string {
	seen := make(map[string]bool, 8)
	out := make([]string, 0, 8)
	for _, field := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		ext := strings.ToLower(field)
		if ext == "" {
			continue
		}
		// ".TXT" and "txt" are the same extension, and both are how
		// people write them.
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if seen[ext] {
			continue
		}
		seen[ext] = true
		out = append(out, ext)
	}
	if len(out) == 0 {
		return append([]string(nil), defaultTextFormats...)
	}
	return out
}

// formatTextFormats is the list as the Settings field shows it, so what the
// reader typed is what they see again when they reopen the dialog.
func formatTextFormats(formats []string) string {
	if len(formats) == 0 {
		return defaultTextFormatList
	}
	parts := make([]string, 0, len(formats))
	for _, f := range formats {
		f = strings.TrimSpace(f)
		if f != "" {
			parts = append(parts, f)
		}
	}
	if len(parts) == 0 {
		return defaultTextFormatList
	}
	return strings.Join(parts, ", ")
}

// hasTextFormat reports whether one path's extension is in the list. The
// comparison is case-insensitive because a file's extension comes from
// whatever the game's archive used, and Windows itself does not care.
func hasTextFormat(formats []string, virtualPath string) bool {
	ext := strings.ToLower(filepath.Ext(virtualPath))
	if ext == "" {
		return false
	}
	for _, f := range formats {
		if strings.EqualFold(f, ext) {
			return true
		}
	}
	return false
}

// isExternalTextFile reports whether one click should open the path in the
// built-in text editor. The list is the reader's own where they set one.
func (ui *uiState) isExternalTextFile(virtualPath string) bool {
	return hasTextFormat(ui.textFormats(), virtualPath)
}

// isEditorFile reports whether Enter or a single click should open the path in
// the built-in text editor.
func (ui *uiState) isEditorFile(virtualPath string) bool {
	return hasTextFormat(ui.textFormats(), virtualPath)
}

// textFormats is the list in force, which is the reader's when they set one
// and the built-in default otherwise.
func (ui *uiState) textFormats() []string {
	if ui == nil || len(ui.textEditorFormats) == 0 {
		return defaultTextFormats
	}
	return ui.textEditorFormats
}

func moveFileToRecycle(source, recycleDir string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("only regular files can be deleted: %s", source)
	}
	if err := os.MkdirAll(recycleDir, 0755); err != nil {
		return "", err
	}
	base := filepath.Base(source)
	stamp := time.Now().Format("20060102-150405.000000000")
	destination := filepath.Join(recycleDir, stamp+"_"+base)
	for i := 2; ; i++ {
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			break
		}
		destination = filepath.Join(recycleDir, fmt.Sprintf("%s_%d_%s", stamp, i, base))
	}
	if err := os.Rename(source, destination); err == nil {
		return destination, nil
	}

	// Rename can fail when the source and AppData are on different volumes.
	if err := copyFileThenRemove(source, destination, info.Mode().Perm()); err != nil {
		return "", err
	}
	return destination, nil
}

// moveFileKeeping moves a file to a destination whose name the caller has
// chosen, which is what freezing needs: the project's folder holds the file
// under a name that says what it is, not under a timestamp.
//
// It is moveFileToRecycle's other half, and the reason it exists separately is
// that a freeze is undone by a name, not by whatever the recycle bin happened
// to call the file this minute.
func moveFileKeeping(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("only regular files can be moved: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	return copyFileThenRemove(source, destination, info.Mode().Perm())
}

// copyFileThenRemove copies a file and then removes the original, which is how
// a move is done when the two ends are on different volumes. The order is not
// negotiable: a failed copy must leave the original where it was.
func copyFileThenRemove(source, destination string, perm os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	copyErr := func() error {
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
		return out.Sync()
	}()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	return os.Remove(source)
}

func countFilesInDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count
}

func recycleFileCount() int {
	dir, err := recycleBinDir()
	if err != nil {
		return 0
	}
	return countFilesInDir(dir)
}

func clearRecycleDir(recycleDir string) error {
	entries, err := os.ReadDir(recycleDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(recycleDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (ui *uiState) canDeleteCurrentModFile(virtualPath string) bool {
	_, err := ui.managedFileDiskPath(virtualPath)
	return err == nil
}

func (ui *uiState) managedFileDiskPath(virtualPath string) (string, error) {
	if ui.modTree == nil {
		return "", fmt.Errorf("no mod loaded")
	}
	file := ui.modTree.FindFile(virtualPath)
	if file == nil {
		return "", fmt.Errorf("file not found: %s", virtualPath)
	}
	if file.Source == nil || file.Source.IsSGA {
		return "", fmt.Errorf("file is not a regular file in the mod folder: %s", virtualPath)
	}
	if file.DiskPath == "" {
		return "", fmt.Errorf("physical file path is unknown: %s", virtualPath)
	}
	root := currentModFolderRoot(ui.modTree)
	if root == "" || !pathWithinRoot(file.DiskPath, root) {
		return "", fmt.Errorf("file is outside the current mod folder: %s", virtualPath)
	}
	info, err := os.Stat(file.DiskPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("only regular files can be deleted: %s", virtualPath)
	}
	return filepath.Clean(file.DiskPath), nil
}

func duplicateVirtualPath(tree *modtree.ModTree, virtualPath string) string {
	dir, base := modtree.VirtualDirAndBase(virtualPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		candidate := name
		if dir != "" {
			candidate = dir + `\` + name
		}
		if tree == nil || tree.FindFile(candidate) == nil {
			return candidate
		}
	}
}

func (ui *uiState) openTreeFileExternally(virtualPath string) error {
	if ui.modTree == nil {
		return fmt.Errorf("no mod loaded")
	}
	diskPath, managedErr := ui.managedFileDiskPath(virtualPath)
	copied := false
	if managedErr != nil {
		data, err := ui.modTree.ReadFile(virtualPath)
		if err != nil {
			return err
		}
		diskPath, err = ui.modTree.WriteOverlayFile(virtualPath, data)
		if err != nil {
			return err
		}
		copied = true
	}
	if err := openFileWithDefaultApp(diskPath); err != nil {
		return err
	}
	if tabIndex := ui.findTabIndex(virtualPath); tabIndex >= 0 {
		ui.closeTabAt(tabIndex)
	}
	if copied {
		ui.statusText = fmt.Sprintf("Copied %s to the current mod and opened it", filepath.Base(virtualPath))
	} else {
		ui.statusText = fmt.Sprintf("Opened %s with the system default application", filepath.Base(virtualPath))
	}
	ui.markStateDirty()
	return nil
}

func virtualParentPath(virtualPath string) string {
	normalized := strings.ReplaceAll(virtualPath, "/", `\\`)
	pos := strings.LastIndex(normalized, `\\`)
	if pos <= 0 {
		return "Data"
	}
	return normalized[:pos]
}

func (ui *uiState) refreshModTreePreservingUI() error {
	if ui.modTree == nil || ui.modulePath == "" {
		return fmt.Errorf("no mod loaded")
	}
	oldTree := ui.modTree
	ui.stopModFileWatcher()
	newTree, err := modtree.BuildModTree(ui.modulePath, "English")
	if err != nil {
		// Keep the old tree usable if a refresh fails.
		ui.startModFileWatcher(oldTree)
		return err
	}
	oldTree.Close()
	ui.modTree = newTree
	ui.modSnapshot = currentModSnapshot(newTree)
	ui.lastModPoll = time.Now()
	ui.startModFileWatcher(newTree)

	// Keep expanded folders, selection, scroll position, and unrelated tabs.
	// Only tabs whose virtual file disappeared need to be closed. The object
	// tab is not backed by a file, so it survives a reload like a Module tab
	// does.
	for i := len(ui.openTabs) - 1; i >= 0; i-- {
		if ui.openTabs[i] == nil || unitTabHasNoFile(ui.openTabs[i]) {
			continue
		}
		if newTree.FindFile(ui.openTabs[i].path) == nil {
			ui.closeTabAt(i)
		}
	}
	ui.visibleNodes = newTree.Flatten(ui.expanded, ui.selectedPath)
	ui.ensureRowClicks(len(ui.visibleNodes))
	ui.markStateDirty()
	return nil
}

func (ui *uiState) deleteCurrentModFile(virtualPath string) error {
	diskPath, err := ui.managedFileDiskPath(virtualPath)
	if err != nil {
		return err
	}
	recycleDir, err := ensureRecycleBinDir()
	if err != nil {
		return err
	}
	destination, err := moveFileToRecycle(diskPath, recycleDir)
	if err != nil {
		return err
	}
	if ui.selectedPath == virtualPath {
		ui.selectedPath = ""
		ui.selectedTreePath = virtualParentPath(virtualPath)
	}
	if err := ui.refreshModTreePreservingUI(); err != nil {
		return fmt.Errorf("file moved, but mod tree refresh failed: %w", err)
	}
	ui.statusText = fmt.Sprintf("Moved %s to Recycle Bin: %s", filepath.Base(diskPath), destination)
	ui.markStateDirty()
	return nil
}

func (ui *uiState) duplicateCurrentModFile(virtualPath string) error {
	if ui.modTree == nil {
		return fmt.Errorf("no mod loaded")
	}
	data, err := ui.modTree.ReadFile(virtualPath)
	if err != nil {
		return err
	}
	duplicate := duplicateVirtualPath(ui.modTree, virtualPath)
	if _, err := ui.modTree.WriteOverlayFile(duplicate, data); err != nil {
		return err
	}
	if err := ui.refreshModTreePreservingUI(); err != nil {
		return fmt.Errorf("duplicate created, but mod tree refresh failed: %w", err)
	}
	ui.statusText = "Duplicated " + filepath.Base(virtualPath) + " as " + filepath.Base(duplicate)
	ui.markStateDirty()
	return nil
}
