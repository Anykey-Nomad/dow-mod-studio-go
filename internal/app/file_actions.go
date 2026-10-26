package app

import (
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dow-mod-studio/internal/modtree"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
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

// ─── merged from file_picker.go ───────────────────────────────────────────────

// One file dialog serves every place the app asks for a file: Open Module, the
// Import frame's timeline and the event editor's sound field. A caller gives
// its title, the extensions it takes, where the list starts and what to do
// with the chosen path; the dialog does the browsing and the filtering.

// pickMaxRows caps the rows of a start list over the module tree, which can
// hold thousands of files.
const pickMaxRows = 300

// pickConfig is what a caller hands the dialog when it opens.
type pickConfig struct {
	title, subtitle, hint string
	exts                  []string // lower case with the dot; a bare name gets the first
	// modTree makes the picker use the mod file tree: its list is the tree's
	// files, folders are not browsed on disk, and only tree paths are accepted.
	// Sounds, FX and the timeline import ask for it; Open Module does not.
	modTree bool
	roots   func() []string
	start   string
	// onPick receives the path that was chosen. It returns whether the dialog
	// may close, and a status line for the dialog when it may not.
	onPick func(path string) (bool, string)
}

type pickRow struct {
	path   string
	label  string
	folder string
	isDir  bool
}

// filePicker is the dialog's state. It lives once on the window, since one
// file dialog is open at a time.
type filePicker struct {
	cfg    pickConfig
	ed     widget.Editor
	status string
	dir    string // folder being browsed; "" while the start list is shown
	last   string // the field's text as the dialog last saw it
	filter string
	all    []pickRow
	rows   []pickRow
	clicks []widget.Clickable
	list   layout.List
	ok     widget.Clickable
	cancel widget.Clickable
}

// pickExt is the lower case extension of a file name or path, dot included,
// or "" when the last part has none. A backslash or slash after the dot means
// the dot belongs to a folder.
func pickExt(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 || strings.ContainsAny(name[i:], `\/`) {
		return ""
	}
	return strings.ToLower(name[i:])
}

// pickFilterOf is the part of a typed path the list is filtered by: the last
// segment, so a folder path typed in full shows its own contents.
func pickFilterOf(text string) string {
	if i := strings.LastIndexAny(text, `\/`); i >= 0 {
		return text[i+1:]
	}
	return text
}

// cleanDialogPath trims spaces and one pair of surrounding quotes, so a path
// copied with Explorer's "Copy as path" can be pasted as it is.
func cleanDialogPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (fp *filePicker) allows(name string) bool {
	if len(fp.cfg.exts) == 0 {
		return true
	}
	ext := pickExt(name)
	for _, e := range fp.cfg.exts {
		if ext == e {
			return true
		}
	}
	return false
}

func (fp *filePicker) setText(s string) {
	fp.ed.SetText(s)
	fp.last = s
}

// openFilePicker shows the dialog for one caller.
func (ui *uiState) openFilePicker(cfg pickConfig) {
	fp := &ui.filePick
	fp.cfg = cfg
	fp.ed.SingleLine = true
	fp.ed.Submit = true
	fp.status = ""
	fp.filter = ""
	fp.dir = ""
	fp.setText(cfg.start)
	ui.filePickOpen = true
	if cfg.start != "" && !cfg.modTree {
		if info, err := os.Stat(cfg.start); err == nil && info.IsDir() {
			ui.fpBrowse(cfg.start)
			return
		}
	}
	ui.fpStartList()
}

// fpStartList shows the start list: the module's files for a tree caller, or
// the folders and matching files of the caller's roots.
func (ui *uiState) fpStartList() {
	ui.filePick.dir = ""
	ui.fpRebuild()
}

// fpBrowse lists the folders and matching files of one folder, with a ".."
// row up. A top folder's ".." goes back to the start list.
func (ui *uiState) fpBrowse(dir string) {
	fp := &ui.filePick
	if dir == "" {
		ui.fpStartList()
		return
	}
	dir = filepath.Clean(dir)
	ui.rememberRecentFolder(dir)
	fp.dir = dir
	fp.setText(dir)
	fp.filter = ""
	parent := filepath.Dir(dir)
	up := pickRow{path: parent, label: "..", folder: dir, isDir: true}
	if parent == dir {
		up.path = ""
	}
	fp.all = []pickRow{up}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fp.status = "Cannot read " + dir + ": " + err.Error()
	}
	var subs, files []pickRow
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			subs = append(subs, pickRow{path: full, label: e.Name() + `\`, folder: dir, isDir: true})
			continue
		}
		if fp.allows(e.Name()) {
			files = append(files, pickRow{path: full, label: e.Name(), folder: dir})
		}
	}
	sort.Slice(subs, func(i, j int) bool {
		return strings.ToLower(subs[i].label) < strings.ToLower(subs[j].label)
	})
	fp.all = append(fp.all, subs...)
	fp.all = append(fp.all, files...)
	ui.fpRebuild()
	if err == nil {
		fp.status = fmt.Sprintf("%d folder(s), %d file(s) in %s", len(subs), len(files), dir)
	}
}

// fpStartRows builds the start list for the filter text.
func (ui *uiState) fpStartRows(filter string) []pickRow {
	fp := &ui.filePick
	q := strings.ToLower(filter)
	if fp.cfg.modTree {
		var out []pickRow
		if ui.modTree == nil {
			ui.filePick.status = "No mod is loaded: the mod file tree is empty"
			return nil
		}
		modtree.SearchWalk(ui.modTree.Root, func(f *modtree.TreeFile) {
			if f == nil || len(out) >= pickMaxRows || !fp.allows(f.Name) {
				return
			}
			if q != "" && !strings.Contains(strings.ToLower(f.FullPath), q) {
				return
			}
			out = append(out, pickRow{path: f.FullPath, label: f.FullPath, folder: filepath.Dir(f.FullPath)})
		})
		return out
	}
	var dirs []string
	if fp.cfg.roots != nil {
		dirs = fp.cfg.roots()
	}
	out := discoverFolderRows(dirs)
	out = append(out, listPickFiles(dirs, fp.allows)...)
	return out
}

// fpRebuild recomputes the rows for the current filter and folder.
func (ui *uiState) fpRebuild() {
	fp := &ui.filePick
	if fp.dir == "" {
		fp.all = ui.fpStartRows(fp.filter)
	}
	q := strings.ToLower(fp.filter)
	fp.rows = fp.rows[:0]
	for _, r := range fp.all {
		if r.label == ".." || q == "" || strings.Contains(strings.ToLower(r.label), q) {
			fp.rows = append(fp.rows, r)
		}
	}
	fp.clicks = make([]widget.Clickable, len(fp.rows))
}

// discoverFolderRows lists the existing roots as folders to open.
func discoverFolderRows(dirs []string) []pickRow {
	seen := make(map[string]bool)
	var rows []pickRow
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
			rows = append(rows, pickRow{path: dir, label: dir, folder: filepath.Dir(dir), isDir: true})
		}
	}
	return rows
}

// listPickFiles lists the files of the roots that allows accepts, without
// repeats, sorted by path.
func listPickFiles(dirs []string, allows func(string) bool) []pickRow {
	seen := make(map[string]bool)
	var out []pickRow
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
			out = append(out, pickRow{path: p, label: p, folder: dir})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].path) < strings.ToLower(out[j].path)
	})
	return out
}

// handleFilePicker runs the dialog's input: typing narrows the list or opens a
// typed folder, a folder row opens that folder, a file row fills the field,
// Enter or Open hands the path to the caller, and Cancel closes it.
func (ui *uiState) handleFilePicker(gtx layout.Context) {
	fp := &ui.filePick
	submitted := false
	for {
		ev, ok := fp.ed.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if text := fp.ed.Text(); text != fp.last {
		fp.last = text
		t := cleanDialogPath(text)
		if t != "" && !fp.cfg.modTree {
			if info, err := os.Stat(t); err == nil && info.IsDir() {
				ui.fpBrowse(t)
				return
			}
		}
		fp.filter = pickFilterOf(t)
		ui.fpRebuild()
	}
	if fp.cancel.Clicked(gtx) {
		ui.filePickOpen = false
		return
	}
	for i := range fp.clicks {
		if !fp.clicks[i].Clicked(gtx) || i >= len(fp.rows) {
			continue
		}
		r := fp.rows[i]
		if r.isDir {
			ui.fpBrowse(r.path)
			return
		}
		fp.setText(r.path)
		fp.status = "Selected " + r.path
		return
	}
	if !fp.ok.Clicked(gtx) && !submitted {
		return
	}
	path := cleanDialogPath(fp.ed.Text())
	if path == "" {
		fp.status = "Choose a file"
		return
	}
	if fp.cfg.modTree && filepath.IsAbs(path) {
		fp.status = "This picker uses the mod file tree: choose a file from the list"
		return
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		ui.fpBrowse(path)
		return
	}
	if pickExt(path) == "" && !pathExists(path) && len(fp.cfg.exts) > 0 {
		path += fp.cfg.exts[0]
	}
	if !fp.allows(path) {
		fp.status = "Choose a " + strings.Join(fp.cfg.exts, " or ") + " file"
		return
	}
	accepted, status := fp.cfg.onPick(path)
	if accepted {
		ui.filePickOpen = false
		return
	}
	if status != "" {
		fp.status = status
	}
}

// layoutFilePicker draws the dialog over the window: a path field, the list,
// the status line and the two buttons.
func (ui *uiState) layoutFilePicker(gtx layout.Context) layout.Dimensions {
	fp := &ui.filePick
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			st := clip.Rect{Max: sz}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 60)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(540))
				cardH := (gtx.Constraints.Max.Y * 9) / 10
				if minH := gtx.Dp(unit.Dp(360)); cardH < minH {
					cardH = minH
				}
				gtx.Constraints.Max.X = cardW
				gtx.Constraints.Min.X = cardW
				gtx.Constraints.Max.Y = cardH
				gtx.Constraints.Min.Y = cardH
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Constraints.Max
							rrect(gtx, cPanelBg, sz, gtx.Dp(4))
							return layout.Dimensions{Size: sz}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(14), fp.cfg.title)
										l.Font.Weight = font.Medium
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(11), fp.cfg.subtitle)
										l.Color = cTextDim
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										h := gtx.Dp(unit.Dp(28))
										gtx.Constraints.Min.Y = h
										gtx.Constraints.Max.Y = h
										return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												ed := material.Editor(ui.th, &fp.ed, "")
												ed.TextSize = unit.Sp(11)
												ed.HintColor = cTextDim
												ed.Hint = fp.cfg.hint
												return ed.Layout(gtx)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(11), "Folders and files")
										l.Font.Weight = font.Medium
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											rrect(gtx, cContentBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
											fp.list.Axis = layout.Vertical
											return fp.list.Layout(gtx, len(fp.rows), func(gtx layout.Context, index int) layout.Dimensions {
												r := fp.rows[index]
												click := &fp.clicks[index]
												bg := cPanelBg
												if click.Hovered() {
													bg = cMenuHover
												}
												return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(26))
													st := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(26)))}.Push(gtx.Ops)
													paint.ColorOp{Color: bg}.Add(gtx.Ops)
													paint.PaintOp{}.Add(gtx.Ops)
													st.Pop()
													return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														l := material.Label(ui.th, unit.Sp(11), r.label)
														l.Color = cText
														return l.Layout(gtx)
													})
												})
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(10), fp.status)
										l.Color = cTextDim
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Flexed(1, rowGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &fp.cancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &fp.ok, "Open", true)
											}),
										)
									}),
								)
							})
						}),
					)
				})
			})
		}),
	)
}
