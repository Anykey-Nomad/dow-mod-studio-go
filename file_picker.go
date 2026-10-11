package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dow-mod-studio/internal/modtree"
)

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
