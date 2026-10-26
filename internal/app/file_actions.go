package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
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
func IsExternalTextFile(ui *uiState, virtualPath string) bool {
	return hasTextFormat(TextFormats(ui), virtualPath)
}

// isEditorFile reports whether Enter or a single click should open the path in
// the built-in text editor.
func IsEditorFile(ui *uiState, virtualPath string) bool {
	return hasTextFormat(TextFormats(ui), virtualPath)
}

// textFormats is the list in force, which is the reader's when they set one
// and the built-in default otherwise.
func TextFormats(ui *uiState) []string {
	if ui == nil || len(ui.TextEditorFormats) == 0 {
		return defaultTextFormats
	}
	return ui.TextEditorFormats
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

func CanDeleteCurrentModFile(ui *uiState, virtualPath string) bool {
	_, err := ManagedFileDiskPath(ui, virtualPath)
	return err == nil
}

func ManagedFileDiskPath(ui *uiState, virtualPath string) (string, error) {
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
	root := currentModFolderRoot(ui.ModTree)
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

func OpenTreeFileExternally(ui *uiState, virtualPath string) error {
	if ui.ModTree == nil {
		return fmt.Errorf("no mod loaded")
	}
	diskPath, managedErr := ManagedFileDiskPath(ui, virtualPath)
	copied := false
	if managedErr != nil {
		data, err := ui.ModTree.ReadFile(virtualPath)
		if err != nil {
			return err
		}
		diskPath, err = ui.ModTree.WriteOverlayFile(virtualPath, data)
		if err != nil {
			return err
		}
		copied = true
	}
	if err := openFileWithDefaultApp(diskPath); err != nil {
		return err
	}
	if tabIndex := FindTabIndex(ui, virtualPath); tabIndex >= 0 {
		CloseTabAt(ui, tabIndex)
	}
	if copied {
		ui.StatusText = fmt.Sprintf("Copied %s to the current mod and opened it", filepath.Base(virtualPath))
	} else {
		ui.StatusText = fmt.Sprintf("Opened %s with the system default application", filepath.Base(virtualPath))
	}
	MarkStateDirty(ui)
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

func RefreshModTreePreservingUI(ui *uiState) error {
	if ui.ModTree == nil || ui.ModulePath == "" {
		return fmt.Errorf("no mod loaded")
	}
	oldTree := ui.ModTree
	StopModFileWatcher(ui)
	newTree, err := modtree.BuildModTree(ui.ModulePath, "English")
	if err != nil {
		// Keep the old tree usable if a refresh fails.
		StartModFileWatcher(ui, oldTree)
		return err
	}
	oldTree.Close()
	ui.ModTree = newTree
	ui.ModSnapshot = currentModSnapshot(newTree)
	ui.LastModPoll = time.Now()
	StartModFileWatcher(ui, newTree)

	// Keep expanded folders, selection, scroll position, and unrelated tabs.
	// Only tabs whose virtual file disappeared need to be closed. The object
	// tab is not backed by a file, so it survives a reload like a Module tab
	// does.
	for i := len(ui.OpenTabs) - 1; i >= 0; i-- {
		if ui.OpenTabs[i] == nil || unitTabHasNoFile(ui.OpenTabs[i]) {
			continue
		}
		if newTree.FindFile(ui.OpenTabs[i].Path) == nil {
			CloseTabAt(ui, i)
		}
	}
	ui.VisibleNodes = newTree.Flatten(ui.Expanded, ui.SelectedPath)
	EnsureRowClicks(ui, len(ui.VisibleNodes))
	MarkStateDirty(ui)
	return nil
}

func DeleteCurrentModFile(ui *uiState, virtualPath string) error {
	diskPath, err := ManagedFileDiskPath(ui, virtualPath)
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
	if ui.SelectedPath == virtualPath {
		ui.SelectedPath = ""
		ui.SelectedTreePath = virtualParentPath(virtualPath)
	}
	if err := RefreshModTreePreservingUI(ui); err != nil {
		return fmt.Errorf("file moved, but mod tree refresh failed: %w", err)
	}
	ui.StatusText = fmt.Sprintf("Moved %s to Recycle Bin: %s", filepath.Base(diskPath), destination)
	MarkStateDirty(ui)
	return nil
}

func DuplicateCurrentModFile(ui *uiState, virtualPath string) error {
	if ui.ModTree == nil {
		return fmt.Errorf("no mod loaded")
	}
	data, err := ui.ModTree.ReadFile(virtualPath)
	if err != nil {
		return err
	}
	duplicate := duplicateVirtualPath(ui.ModTree, virtualPath)
	if _, err := ui.ModTree.WriteOverlayFile(duplicate, data); err != nil {
		return err
	}
	if err := RefreshModTreePreservingUI(ui); err != nil {
		return fmt.Errorf("duplicate created, but mod tree refresh failed: %w", err)
	}
	ui.StatusText = "Duplicated " + filepath.Base(virtualPath) + " as " + filepath.Base(duplicate)
	MarkStateDirty(ui)
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

type pickConfig = uistate.PickConfig

type pickRow = uistate.PickRow

type filePicker = uistate.FilePicker

// pickExt forwards to uistate, which needs it for the shared state.
func pickExt(name string) string {
	return uistate.PickExt(name)
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

// openFilePicker shows the dialog for one caller.
func OpenFilePicker(ui *uiState, cfg pickConfig) {
	fp := &ui.FilePick
	fp.Cfg = cfg
	fp.Ed.SingleLine = true
	fp.Ed.Submit = true
	fp.Status = ""
	fp.Filter = ""
	fp.Dir = ""
	fp.SetText(cfg.Start)
	ui.FilePickOpen = true
	if cfg.Start != "" && !cfg.ModTree {
		if info, err := os.Stat(cfg.Start); err == nil && info.IsDir() {
			FpBrowse(ui, cfg.Start)
			return
		}
	}
	FpStartList(ui)
}

// fpStartList shows the start list: the module's files for a tree caller, or
// the folders and matching files of the caller's roots.
func FpStartList(ui *uiState) {
	ui.FilePick.Dir = ""
	FpRebuild(ui)
}

// fpBrowse lists the folders and matching files of one folder, with a ".."
// row up. A top folder's ".." goes back to the start list.
func FpBrowse(ui *uiState, dir string) {
	fp := &ui.FilePick
	if dir == "" {
		FpStartList(ui)
		return
	}
	dir = filepath.Clean(dir)
	RememberRecentFolder(ui, dir)
	fp.Dir = dir
	fp.SetText(dir)
	fp.Filter = ""
	parent := filepath.Dir(dir)
	up := pickRow{Path: parent, Label: "..", Folder: dir, IsDir: true}
	if parent == dir {
		up.Path = ""
	}
	fp.All = []pickRow{up}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fp.Status = "Cannot read " + dir + ": " + err.Error()
	}
	var subs, files []pickRow
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			subs = append(subs, pickRow{Path: full, Label: e.Name() + `\`, Folder: dir, IsDir: true})
			continue
		}
		if fp.Allows(e.Name()) {
			files = append(files, pickRow{Path: full, Label: e.Name(), Folder: dir})
		}
	}
	sort.Slice(subs, func(i, j int) bool {
		return strings.ToLower(subs[i].Label) < strings.ToLower(subs[j].Label)
	})
	fp.All = append(fp.All, subs...)
	fp.All = append(fp.All, files...)
	FpRebuild(ui)
	if err == nil {
		fp.Status = fmt.Sprintf("%d folder(s), %d file(s) in %s", len(subs), len(files), dir)
	}
}

// fpStartRows builds the start list for the filter text.
func FpStartRows(ui *uiState, filter string) []pickRow {
	fp := &ui.FilePick
	q := strings.ToLower(filter)
	if fp.Cfg.ModTree {
		var out []pickRow
		if ui.ModTree == nil {
			ui.FilePick.Status = "No mod is loaded: the mod file tree is empty"
			return nil
		}
		modtree.SearchWalk(ui.ModTree.Root, func(f *modtree.TreeFile) {
			if f == nil || len(out) >= pickMaxRows || !fp.Allows(f.Name) {
				return
			}
			if q != "" && !strings.Contains(strings.ToLower(f.FullPath), q) {
				return
			}
			out = append(out, pickRow{Path: f.FullPath, Label: f.FullPath, Folder: filepath.Dir(f.FullPath)})
		})
		return out
	}
	var dirs []string
	if fp.Cfg.Roots != nil {
		dirs = fp.Cfg.Roots()
	}
	out := discoverFolderRows(dirs)
	out = append(out, listPickFiles(dirs, fp.Allows)...)
	return out
}

// fpRebuild recomputes the rows for the current filter and folder.
func FpRebuild(ui *uiState) {
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
			rows = append(rows, pickRow{Path: dir, Label: dir, Folder: filepath.Dir(dir), IsDir: true})
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
			out = append(out, pickRow{Path: p, Label: p, Folder: dir})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out
}

// handleFilePicker runs the dialog's input: typing narrows the list or opens a
// typed folder, a folder row opens that folder, a file row fills the field,
// Enter or Open hands the path to the caller, and Cancel closes it.
func HandleFilePicker(ui *uiState, gtx layout.Context) {
	fp := &ui.FilePick
	submitted := false
	for {
		ev, ok := fp.Ed.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if text := fp.Ed.Text(); text != fp.Last {
		fp.Last = text
		t := cleanDialogPath(text)
		if t != "" && !fp.Cfg.ModTree {
			if info, err := os.Stat(t); err == nil && info.IsDir() {
				FpBrowse(ui, t)
				return
			}
		}
		fp.Filter = pickFilterOf(t)
		FpRebuild(ui)
	}
	if fp.Cancel.Clicked(gtx) {
		ui.FilePickOpen = false
		return
	}
	for i := range fp.Clicks {
		if !fp.Clicks[i].Clicked(gtx) || i >= len(fp.Rows) {
			continue
		}
		r := fp.Rows[i]
		if r.IsDir {
			FpBrowse(ui, r.Path)
			return
		}
		fp.SetText(r.Path)
		fp.Status = "Selected " + r.Path
		return
	}
	if !fp.Ok.Clicked(gtx) && !submitted {
		return
	}
	path := cleanDialogPath(fp.Ed.Text())
	if path == "" {
		fp.Status = "Choose a file"
		return
	}
	if fp.Cfg.ModTree && filepath.IsAbs(path) {
		fp.Status = "This picker uses the mod file tree: choose a file from the list"
		return
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		FpBrowse(ui, path)
		return
	}
	if pickExt(path) == "" && !pathExists(path) && len(fp.Cfg.Exts) > 0 {
		path += fp.Cfg.Exts[0]
	}
	if !fp.Allows(path) {
		fp.Status = "Choose a " + strings.Join(fp.Cfg.Exts, " or ") + " file"
		return
	}
	accepted, status := fp.Cfg.OnPick(path)
	if accepted {
		ui.FilePickOpen = false
		return
	}
	if status != "" {
		fp.Status = status
	}
}

// layoutFilePicker draws the dialog over the window: a path field, the list,
// the status line and the two buttons.
func LayoutFilePicker(ui *uiState, gtx layout.Context) layout.Dimensions {
	fp := &ui.FilePick
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
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Constraints.Max
							rrect(gtx, uikit.CPanelBg, sz, gtx.Dp(4))
							return layout.Dimensions{Size: sz}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(14), fp.Cfg.Title)
										l.Font.Weight = font.Medium
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(11), fp.Cfg.Subtitle)
										l.Color = uikit.CTextDim
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										h := gtx.Dp(unit.Dp(28))
										gtx.Constraints.Min.Y = h
										gtx.Constraints.Max.Y = h
										return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												Ed := material.Editor(ui.Th, &fp.Ed, "")
												Ed.TextSize = unit.Sp(11)
												Ed.HintColor = uikit.CTextDim
												Ed.Hint = fp.Cfg.Hint
												return Ed.Layout(gtx)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(11), "Folders and files")
										l.Font.Weight = font.Medium
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											rrect(gtx, uikit.CContentBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
											fp.List.Axis = layout.Vertical
											return fp.List.Layout(gtx, len(fp.Rows), func(gtx layout.Context, index int) layout.Dimensions {
												r := fp.Rows[index]
												click := &fp.Clicks[index]
												bg := uikit.CPanelBg
												if click.Hovered() {
													bg = uikit.CMenuHover
												}
												return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(26))
													st := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(26)))}.Push(gtx.Ops)
													paint.ColorOp{Color: bg}.Add(gtx.Ops)
													paint.PaintOp{}.Add(gtx.Ops)
													st.Pop()
													return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														l := material.Label(ui.Th, unit.Sp(11), r.Label)
														l.Color = uikit.CText
														return l.Layout(gtx)
													})
												})
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(10), fp.Status)
										l.Color = uikit.CTextDim
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Flexed(1, rowGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &fp.Cancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &fp.Ok, "Open", true)
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
