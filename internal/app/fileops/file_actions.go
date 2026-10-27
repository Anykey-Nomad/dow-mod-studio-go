package fileops

import (
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modtree"
	"fmt"
	"gioui.org/widget"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The file formats a module opens in the built-in text editor. It is a
// starting point rather than a rule: Settings lets a reader add their own
// through a comma and space separated list, which is what isRead, isEditorFile
// and openTreeFile all go by.
var DefaultTextFormats = []string{".lua", ".txt", ".ai", ".scar", ".rat", ".con"}

// DefaultTextFormatList is DefaultTextFormats as the Settings field shows them.
const DefaultTextFormatList = ".lua, .txt, .ai, .scar, .rat, .con"

// ParseTextFormats reads a list of file extensions the way a person types it:
// separated by commas, spaces or both, in any order, written with or without
// the leading dot. Entries are compared case-insensitively and returned in
// lower case with a dot, which is the form filepath.Ext produces.
//
// An empty list is not an error and not "nothing opens": it falls back to the
// defaults, so clearing the field cannot lock the editor out of every file.
func ParseTextFormats(s string) []string {
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
		return append([]string(nil), DefaultTextFormats...)
	}
	return out
}

// FormatTextFormats is the list as the Settings field shows it, so what the
// reader typed is what they see again when they reopen the dialog.
func FormatTextFormats(formats []string) string {
	if len(formats) == 0 {
		return DefaultTextFormatList
	}
	parts := make([]string, 0, len(formats))
	for _, f := range formats {
		f = strings.TrimSpace(f)
		if f != "" {
			parts = append(parts, f)
		}
	}
	if len(parts) == 0 {
		return DefaultTextFormatList
	}
	return strings.Join(parts, ", ")
}

// HasTextFormat reports whether one path's extension is in the list. The
// comparison is case-insensitive because a file's extension comes from
// whatever the game's archive used, and Windows itself does not care.
func HasTextFormat(formats []string, virtualPath string) bool {
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
func IsExternalTextFile(ui *uistate.State, virtualPath string) bool {
	return HasTextFormat(TextFormats(ui), virtualPath)
}

// isEditorFile reports whether Enter or a single click should open the path in
// the built-in text editor.
func IsEditorFile(ui *uistate.State, virtualPath string) bool {
	return HasTextFormat(TextFormats(ui), virtualPath)
}

// textFormats is the list in force, which is the reader's when they set one
// and the built-in default otherwise.
func TextFormats(ui *uistate.State) []string {
	if ui == nil || len(ui.TextEditorFormats) == 0 {
		return DefaultTextFormats
	}
	return ui.TextEditorFormats
}

func MoveFileToRecycle(source, recycleDir string) (string, error) {
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
	if err := CopyFileThenRemove(source, destination, info.Mode().Perm()); err != nil {
		return "", err
	}
	return destination, nil
}

// MoveFileKeeping moves a file to a destination whose name the caller has
// chosen, which is what freezing needs: the project's folder holds the file
// under a name that says what it is, not under a timestamp.
//
// It is MoveFileToRecycle's other half, and the reason it exists separately is
// that a freeze is undone by a name, not by whatever the recycle bin happened
// to call the file this minute.
func MoveFileKeeping(source, destination string) error {
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
	return CopyFileThenRemove(source, destination, info.Mode().Perm())
}

// CopyFileThenRemove copies a file and then removes the original, which is how
// a move is done when the two ends are on different volumes. The order is not
// negotiable: a failed copy must leave the original where it was.
func CopyFileThenRemove(source, destination string, perm os.FileMode) error {
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

func CountFilesInDir(dir string) int {
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

func ClearRecycleDir(recycleDir string) error {
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

func CanDeleteCurrentModFile(ui *uistate.State, virtualPath string) bool {
	_, err := ManagedFileDiskPath(ui, virtualPath)
	return err == nil
}

func ManagedFileDiskPath(ui *uistate.State, virtualPath string) (string, error) {
	if ui.ModTree == nil {
		return "", fmt.Errorf("no mod loaded")
	}
	file := ui.ModTree.FindFile(virtualPath)
	if file == nil {
		return "", fmt.Errorf("file not found: %s", virtualPath)
	}
	if file.Source == nil || file.Source.IsSGA {
		return "", fmt.Errorf("file is not a regular file in the mod folder: %s", virtualPath)
	}
	if file.DiskPath == "" {
		return "", fmt.Errorf("physical file path is unknown: %s", virtualPath)
	}
	root := CurrentModFolderRoot(ui.ModTree)
	if root == "" || !PathWithinRoot(file.DiskPath, root) {
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

func DuplicateVirtualPath(tree *modtree.ModTree, virtualPath string) string {
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

func VirtualParentPath(virtualPath string) string {
	normalized := strings.ReplaceAll(virtualPath, "/", `\\`)
	pos := strings.LastIndex(normalized, `\\`)
	if pos <= 0 {
		return "Data"
	}
	return normalized[:pos]
}

// PickMaxRows caps the rows of a start list over the module tree, which can
// hold thousands of files.
const PickMaxRows = 300

type PickConfig = uistate.PickConfig

type PickRow = uistate.PickRow

type FilePicker = uistate.FilePicker

// PickExt forwards to uistate, which needs it for the shared state.
func PickExt(name string) string {
	return uistate.PickExt(name)
}

// PickFilterOf is the part of a typed path the list is filtered by: the last
// segment, so a folder path typed in full shows its own contents.
func PickFilterOf(text string) string {
	if i := strings.LastIndexAny(text, `\/`); i >= 0 {
		return text[i+1:]
	}
	return text
}

// CleanDialogPath trims spaces and one pair of surrounding quotes, so a path
// copied with Explorer's "Copy as path" can be pasted as it is.
func CleanDialogPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func PathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// fpStartList shows the start list: the module's files for a tree caller, or
// the folders and matching files of the caller's roots.
func FpStartList(ui *uistate.State) {
	ui.FilePick.Dir = ""
	FpRebuild(ui)
}

// fpStartRows builds the start list for the filter text.
func FpStartRows(ui *uistate.State, filter string) []PickRow {
	fp := &ui.FilePick
	q := strings.ToLower(filter)
	if fp.Cfg.ModTree {
		var out []PickRow
		if ui.ModTree == nil {
			ui.FilePick.Status = "No mod is loaded: the mod file tree is empty"
			return nil
		}
		modtree.SearchWalk(ui.ModTree.Root, func(f *modtree.TreeFile) {
			if f == nil || len(out) >= PickMaxRows || !fp.Allows(f.Name) {
				return
			}
			if q != "" && !strings.Contains(strings.ToLower(f.FullPath), q) {
				return
			}
			out = append(out, PickRow{Path: f.FullPath, Label: f.FullPath, Folder: filepath.Dir(f.FullPath)})
		})
		return out
	}
	var dirs []string
	if fp.Cfg.Roots != nil {
		dirs = fp.Cfg.Roots()
	}
	out := DiscoverFolderRows(dirs)
	out = append(out, ListPickFiles(dirs, fp.Allows)...)
	return out
}

// fpRebuild recomputes the rows for the current filter and folder.
func FpRebuild(ui *uistate.State) {
	fp := &ui.FilePick
	if fp.Dir == "" {
		fp.All = FpStartRows(ui, fp.Filter)
	}
	q := strings.ToLower(fp.Filter)
	fp.Rows = fp.Rows[:0]
	for _, r := range fp.All {
		if r.Label == ".." || q == "" || strings.Contains(strings.ToLower(r.Label), q) {
			fp.Rows = append(fp.Rows, r)
		}
	}
	fp.Clicks = make([]widget.Clickable, len(fp.Rows))
}

// DiscoverFolderRows lists the existing roots as folders to open.
func DiscoverFolderRows(dirs []string) []PickRow {
	seen := make(map[string]bool)
	var rows []PickRow
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(dir))
		if seen[key] {
			continue
		}
		seen[key] = true
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			rows = append(rows, PickRow{Path: dir, Label: dir, Folder: filepath.Dir(dir), IsDir: true})
		}
	}
	return rows
}

// ListPickFiles lists the files of the roots that allows accepts, without
// repeats, sorted by path.
func ListPickFiles(dirs []string, allows func(string) bool) []PickRow {
	seen := make(map[string]bool)
	var out []PickRow
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !allows(e.Name()) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			key := strings.ToLower(filepath.Clean(p))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, PickRow{Path: p, Label: p, Folder: dir})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out
}
