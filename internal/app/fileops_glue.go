package app

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/whm"
	"encoding/json"
	"flag"
	"fmt"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// openAssociatedFile opens one OS file argument by its format. It runs once,
// at startup, after the module is in place.
func OpenAssociatedFile(ui *uiState, path string) {
	log.Printf("openAssociatedFile: %q", path)
	switch strings.ToLower(pickExt(path)) {
	case ".fda":
		OpenFda(ui, path)
	case ".whm":
		OpenWhm(ui, path)
	case ".whe":
		OpenWheViaUnit(ui, path)
	case ".sga":
		OpenSgaArchive(ui, path)
	default:
		ui.StatusText = fmt.Sprintf("Cannot open %s — unknown format", filepath.Base(path))
		return
	}
	MarkStateDirty(ui)
}

// openWheViaUnit opens a .whe the way the game itself refers to it: through
// the unit whose model carries those events. A model's .whm and .whe always
// share their name, so the first unit of the open mod whose resolved model has
// the file's own name is shown in the Object tab. With no such unit the file
// is simply reported, since a .whe alone has no viewer of its own.
func OpenWheViaUnit(ui *uiState, path string) {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), pickExt(path)))
	if ui.ModTree == nil {
		ui.StatusText = fmt.Sprintf("Open %s — no module loaded", filepath.Base(path))
		return
	}
	cat := UnitCatalogFor(ui)
	start := time.Now()
	units := cat.AllUnits()
	for _, u := range units {
		d := loadUnitDetails(ui.ModTree, u)
		src, ok := newUnitModelSource(ui.ModTree, d)
		if !ok {
			continue
		}
		model := unitModelPath(ui.ModTree, src)
		if model == "" {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(model), pickExt(model)))
		if base != name {
			continue
		}
		ui.UnitPickRace = u.Race
		ui.UnitPickPath = u.Path
		ApplyPendingUnitPick(ui)
		OpenUnitTab(ui)
		ui.StatusText = fmt.Sprintf("Opened %s as unit %s", filepath.Base(path), u.DisplayName())
		log.Printf("openWheViaUnit %q: unit %s after scanning %d units in %v", path, u.Path, len(units), time.Since(start))
		return
	}
	ui.StatusText = fmt.Sprintf("Open %s — no unit references that model", filepath.Base(path))
	log.Printf("openWheViaUnit %q: no match among %d units in %v", path, len(units), time.Since(start))
}

// layoutSgaListing is the archive tab's body: every folder and file the .sga
// carries, one per row. Folders run on in the same list rather than opening,
// because the whole point of the tab is to see the archive at a glance.
func LayoutSgaListing(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.SgaError != "" {
		return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.SgaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if len(t.SgaRows) == 0 {
		return centeredPanelLabel(gtx, ui.Th, "Empty archive", unit.Sp(10), uikit.CTextDim)
	}
	t.SgaList.Axis = layout.Vertical
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return LayoutListWithScrollbar(ui, gtx, &t.SgaScroll, &t.SgaList, 20, len(t.SgaRows), func(gtx layout.Context, index int) layout.Dimensions {
			r := t.SgaRows[index]
			gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(20))
			gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(20))
			name := r.Path
			if r.IsDir {
				name += "\\"
			}
			size := ""
			if !r.IsDir {
				size = sgaSizeLabel(r.Size)
			}
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), name)
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Flexed(1, flexGap()),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), size)
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
		})
	})
}

// openModulePicker shows the Open Module dialog over the .module files.
func OpenModulePicker(ui *uiState) {
	OpenFilePicker(ui, pickConfig{
		Title:    "Open Module",
		Subtitle: "Enter a .module file or the Dawn of War folder to scan",
		Hint:     `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`,
		Exts:     []string{".module"},
		Roots:    func() []string { return ModuleRoots(ui) },
		Start:    ui.ModulePath,
		OnPick: func(path string) (bool, string) {
			path = ResolveModulePath(ui, path)
			if !pathExists(path) {
				return false, "No such module file: " + path
			}
			LoadModule(ui, path)
			return true, ""
		},
	})
}

func recycleFileCount() int {
	dir, err := recycleBinDir()
	if err != nil {
		return 0
	}
	return countFilesInDir(dir)
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

// freezeEntity moves a unit or a building out of the mod.
//
// The order is fixed and each step depends on the one before: the references
// are found and written down, the record is saved, and only then is anything
// changed. A failure before the save leaves the mod untouched, which is the
// only safe way for this to fail.
func FreezeEntity(ui *uiState, virtualPath string) (*freezeRecord, error) {
	if ui == nil || ui.ModTree == nil {
		return nil, fmt.Errorf("no mod is loaded")
	}
	graph := GraphFor(ui)
	e := graph.Entity(virtualPath)
	if e == nil {
		return nil, fmt.Errorf("%s is not in the mod", virtualPath)
	}
	if e.Kind != graphKindUnit && e.Kind != graphKindStructure {
		return nil, fmt.Errorf("only a unit or a building can be frozen, and %s is a %s", e.Name, e.Kind.Label())
	}
	book, err := readFreezeBook(ui.ModTree, ui.ModulePath)
	if err != nil {
		return nil, err
	}
	if book.IsFrozen(e.Path) {
		return nil, fmt.Errorf("%s is already frozen", e.Name)
	}
	// The references are read here, in full, rather than being taken from
	// whatever a background scan happens to have reached. This is the one
	// operation that rewrites other people's files, and a freeze that cleared
	// only some of what pointed at the unit would leave a mod broken, so it
	// waits for the whole mod rather than showing a progress bar.
	graph.EnsureLinks()
	// The files have to be the mod's own: a file that lives in a required
	// mod or an archive cannot be moved, and pretending otherwise would
	// leave the game still loading a unit the author believes is gone. The
	// path itself is not needed here — it is asked for again when the file
	// actually moves — so what matters is that this refuses.
	if _, err := ManagedFileDiskPath(ui, e.Path); err != nil {
		return nil, fmt.Errorf("%s cannot be frozen: %v", e.Name, err)
	}

	record := freezeRecord{
		Path:     e.Path,
		Name:     e.Name,
		Kind:     e.Kind.Label(),
		Race:     e.Race,
		FrozenAt: time.Now().Format(time.RFC3339),
	}

	// Every reference to this entity, from every other file, found by walking
	// the graph backwards from it.
	mentions := graph.Incoming(e.Path)
	for _, l := range mentions {
		if strings.EqualFold(l.From, e.Path) {
			continue
		}
		record.Mentions = append(record.Mentions, freezeMention{
			File:  l.From,
			Field: l.Field,
			Slot:  l.Slot,
			Role:  l.Role.Label(),
			Value: l.Ref,
		})
	}

	// The squad file of a unit has the same name as the unit and lives under
	// sbps rather than ebps. It is part of the same unit to the game, so it
	// moves with it.
	squadPath := ebpsSquadPathFor(e)
	if squadPath != "" && ui.ModTree.FindFile(squadPath) != nil {
		record.Files = append(record.Files, freezeFile{From: e.Path, To: e.Name + ".rgd"})
		record.Files = append(record.Files, freezeFile{From: squadPath, To: e.Name + "_squad.rgd"})
	} else {
		record.Files = append(record.Files, freezeFile{From: e.Path, To: e.Name + ".rgd"})
	}

	// Saved before anything is touched: a record that does not exist cannot
	// undo anything.
	book.Frozen = append(book.Frozen, record)
	if err := writeFreezeBook(ui.ModTree, ui.ModulePath, book); err != nil {
		return nil, fmt.Errorf("the record of what freezing changes could not be written, so nothing was changed: %w", err)
	}

	// Now the mentions. Each is one field of one file, cleared one file at a
	// time; a file that cannot be rewritten stops the freeze there, with the
	// record already written, so the state is still recoverable.
	for _, m := range record.Mentions {
		if err := ClearRgdField(ui, m.File, m.Field, ""); err != nil {
			return nil, fmt.Errorf("%s could not be cleared from %s: %w", m.Value, m.File, err)
		}
	}

	// And the files themselves, moved into the project's Units folder.
	unitsDir, err := freezeUnitsDir(ui.ModTree, ui.ModulePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(unitsDir, 0755); err != nil {
		return nil, err
	}
	for _, f := range record.Files {
		disk, err := ManagedFileDiskPath(ui, f.From)
		if err != nil {
			return nil, fmt.Errorf("%s could not be moved: %v", f.From, err)
		}
		if err := moveFileKeeping(disk, filepath.Join(unitsDir, f.To)); err != nil {
			return nil, fmt.Errorf("%s could not be moved into the project folder: %w", f.From, err)
		}
		ui.ModTree.RemoveFile(f.From)
	}

	// The indexes are snapshots: a frozen unit is not in the mod any more, so
	// the catalogue, the graph and the cached detail all have to be read
	// again.
	ui.UnitCatalog = nil
	ui.ModGraph = nil
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	ForgetFreezeBook(ui)
	MarkStateDirty(ui)
	return &record, nil
}

// unfreezeEntity puts a frozen unit or building back the way it was: every
// reference it had is given its value again, and its files are moved home.
//
// The references go back before the files do, so that there is no moment in
// which the files are visible to the game but nothing names them. If a file
// cannot be rewritten the freeze is left in place, because a half-restored
// unit is worse than a frozen one.
func UnfreezeEntity(ui *uiState, virtualPath string) (*freezeRecord, error) {
	if ui == nil || ui.ModTree == nil {
		return nil, fmt.Errorf("no mod is loaded")
	}
	book, err := readFreezeBook(ui.ModTree, ui.ModulePath)
	if err != nil {
		return nil, err
	}
	Record := book.Record(virtualPath)
	if Record == nil {
		return nil, fmt.Errorf("%s is not frozen", filepath.Base(virtualPath))
	}

	for _, m := range Record.Mentions {
		if err := ClearRgdField(ui, m.File, m.Field, m.Value); err != nil {
			return nil, fmt.Errorf("the reference to %s could not be put back into %s: %w", Record.Name, m.File, err)
		}
	}

	unitsDir, err := freezeUnitsDir(ui.ModTree, ui.ModulePath)
	if err != nil {
		return nil, err
	}
	for _, f := range Record.Files {
		saved := filepath.Join(unitsDir, f.To)
		data, err := os.ReadFile(saved)
		if err != nil {
			return nil, fmt.Errorf("the saved %s could not be read, so nothing was moved back: %w", f.To, err)
		}
		if _, err := ui.ModTree.WriteOverlayFile(f.From, data); err != nil {
			return nil, fmt.Errorf("%s could not be put back: %w", f.From, err)
		}
		if err := os.Remove(saved); err != nil {
			return nil, fmt.Errorf("%s was put back but its saved copy could not be removed: %w", f.To, err)
		}
	}

	kept := make([]freezeRecord, 0, len(book.Frozen))
	for _, r := range book.Frozen {
		if !strings.EqualFold(r.Path, Record.Path) {
			kept = append(kept, r)
		}
	}
	book.Frozen = kept
	if err := writeFreezeBook(ui.ModTree, ui.ModulePath, book); err != nil {
		return nil, err
	}

	ui.UnitCatalog = nil
	ui.ModGraph = nil
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	ForgetFreezeBook(ui)
	MarkStateDirty(ui)
	return Record, nil
}

// layoutFrozenList is the "Frozen" column. Each row is one object that has been
// moved out of the mod, and pressing it puts it back — the list is the way back,
// because the object itself is no longer anywhere else.
func LayoutFrozenList(ui *uiState, gtx layout.Context) layout.Dimensions {
	rows := FrozenRows(ui)
	if len(rows) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "Nothing has been frozen. Freezing a unit moves it out of the mod and clears what pointed at it; unfreezing puts both back.")
				l.Color = uikit.CTextDim
				l.MaxLines = 6
				return l.Layout(gtx)
			})
		})
	}
	EnsureUnitRowClicks(ui, len(rows))
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range rows {
				i := i
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitSelectableRow(ui, gtx, i, len(rows), rows[i].Hardpoint, rows[i].Name, "put back", false)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// handleFrozenRowInput puts a frozen object back when its row is pressed.
//
// It is a single click rather than a button on each row because the rows are
// the only thing in the column, and because the operation is the same for all
// of them: the row says which object, and pressing it undoes that one freeze.
func HandleFrozenRowInput(ui *uiState, gtx layout.Context) {
	rows := FrozenRows(ui)
	EnsureUnitRowClicks(ui, len(rows))
	for i := range rows {
		if i < len(ui.UnitRowClicks) && ui.UnitRowClicks[i].Clicked(gtx) {
			ui.UnitSelRow = i
			record, err := UnfreezeEntity(ui, rows[i].Ref)
			if err != nil {
				ui.StatusText = "Could not put " + rows[i].Name + " back: " + err.Error()
			} else {
				ui.StatusText = freezeStatusLine("Put back into the mod", record)
			}
			MarkStateDirty(ui)
			return
		}
	}
}

// handleFreezeInput acts on the freeze buttons. Both are one-way operations
// that rewrite other files, so neither of them is a toggle: the button says
// which one it is, and the status line says what happened.
func HandleFreezeInput(ui *uiState, gtx layout.Context) {
	sel := CurrentUnit(ui)
	if sel == nil {
		return
	}
	if ui.UnitFreezeTrigger.Clicked(gtx) {
		record, err := FreezeEntity(ui, sel.Path)
		if err != nil {
			ui.StatusText = "Could not freeze " + sel.Name + ": " + err.Error()
			MarkStateDirty(ui)
			return
		}
		ui.StatusText = freezeStatusLine("Moved out of the mod", record)
		// The object is no longer in the mod, so the section that lists what
		// was frozen is where the user is sent to undo it.
		ui.UnitSection = unitSectionNamed("Frozen")
		MarkStateDirty(ui)
		return
	}
	if ui.UnitUnfreezeTrigger.Clicked(gtx) {
		record, err := UnfreezeEntity(ui, sel.Path)
		if err != nil {
			ui.StatusText = "Could not unfreeze " + sel.Name + ": " + err.Error()
			MarkStateDirty(ui)
			return
		}
		ui.StatusText = freezeStatusLine("Put back into the mod", record)
		MarkStateDirty(ui)
	}
}

// freezeStatusLine says what a freeze or an unfreeze did, in the terms the
// operation uses: the files that moved, and the references that were cleared
// or put back. It is the only account of the change that survives the object
// leaving the mod, so it says what happened rather than that something did.
func freezeStatusLine(what string, record *freezeRecord) string {
	if record == nil {
		return what
	}
	files := itoa(len(record.Files))
	if len(record.Files) == 1 {
		files = "1 file"
	} else {
		files += " files"
	}
	mentions := itoa(len(record.Mentions))
	switch {
	case len(record.Mentions) == 0:
		return record.Name + ": " + what + " (" + files + ", nothing referred to it)"
	case len(record.Mentions) == 1:
		return record.Name + ": " + what + " (" + files + ", 1 reference cleared)"
	}
	return record.Name + ": " + what + " (" + files + ", " + mentions + " references cleared)"
}

func runMarkerDebugCLI(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("--debug-markers", flag.ContinueOnError)
	flags.SetOutput(stderr)
	module := flags.String("module", "", "path to a .module file")
	unit := flags.String("unit", "", "unit name, unit .rgd virtual path, or model .whm virtual path")
	locale := flags.String("locale", "English", "module locale")
	step := flags.Float64("step", 1, "forward endpoint distance in model units")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *module == "" || *unit == "" {
		fmt.Fprintln(stderr, "usage: dow-mod-studio.exe --debug-markers --module <file.module> --unit <name|path> [--locale English] [--step 1]")
		return 2
	}
	if math.IsNaN(*step) || math.IsInf(*step, 0) || *step <= 0 {
		fmt.Fprintln(stderr, "--step must be a positive finite number")
		return 2
	}

	tree, err := modtree.BuildModTree(*module, *locale)
	if err != nil {
		fmt.Fprintf(stderr, "load module: %v\n", err)
		return 1
	}
	defer tree.Close()

	modelPath, unitPath, unitName, err := markerDebugResolve(tree, *unit)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	raw, err := tree.ReadFile(modelPath)
	if err != nil {
		fmt.Fprintf(stderr, "read model %q: %v\n", modelPath, err)
		return 1
	}
	model, err := whm.Parse(raw)
	if err != nil {
		fmt.Fprintf(stderr, "parse model %q: %v\n", modelPath, err)
		return 1
	}

	pose := model.RestPose()
	report := markerDebugReport{
		Module:    tree.ModFilePath,
		Unit:      unitName,
		Model:     modelPath,
		Bounds:    markerDebugBounds{Min: model.BoundMin, Max: model.BoundMax},
		Materials: make([]markerDebugMaterial, 0),
		Markers:   make([]markerDebugItem, 0, len(model.Markers)),
	}
	center, _ := engine.ModelCenter(model)
	report.Center = vectorArray(center)
	report.Materials = markerDebugMaterials(tree, modelPath, model)
	for i := range model.Markers {
		marker := &model.Markers[i]
		pos, posOK := model.MarkerWorld(i, pose, true)
		if !posOK {
			continue
		}
		item := markerDebugItem{
			Index:    i,
			Name:     marker.Name,
			Parent:   marker.Parent,
			Position: vectorArray(pos),
		}
		if axis, ok := model.MarkerAxis(i, pose, true); ok {
			item.HasForward = true
			item.Forward = vectorArray(axis)
			item.Next = vectorArray(pos.Add(axis.MulScalar(*step)))
		}
		report.Markers = append(report.Markers, item)
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintf(stderr, "write marker report: %v\n", err)
		return 1
	}
	_ = unitPath // retained by resolve for diagnostics and future path output
	return 0
}

func markerDebugMaterials(tree *modtree.ModTree, modelPath string, model *whm.Model) []markerDebugMaterial {
	if model == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []markerDebugMaterial
	for _, mesh := range model.Meshes {
		name := strings.TrimSpace(mesh.Material)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		status := markerDebugMaterial{Name: name, TexturePath: name + ".rsh"}
		roots := materialSearchRoots(tree)
		data, found := readModelMaterial(tree, roots, modelPath, name)
		status.Found = found
		if exact, path, exactOK := readModelMaterialAtAuthoredPath(tree, roots, status.TexturePath); exactOK {
			data, status.FoundAtMaterialPath, status.ResolvedPath = exact, true, path
		}
		if !found {
			status.Error = "texture file was not found"
			out = append(out, status)
			continue
		}
		if status.ResolvedPath == "" {
			status.ResolvedPath = "resolved by fallback search"
		}
		if _, _, err := raster.RshRawImage(data, 0); err != nil {
			status.Error = "texture decode failed: " + err.Error()
			out = append(out, status)
			continue
		}
		if _, _, err := decodeRshImage(data, 0); err != nil {
			status.Error = "texture decode failed: " + err.Error()
			out = append(out, status)
			continue
		}
		status.Applied = true
		out = append(out, status)
	}
	return out
}

func LayoutArchiveDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: rgba(0, 0, 0, 70)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		cardW := minInt(gtx.Dp(unit.Dp(640)), gtx.Constraints.Max.X-gtx.Dp(unit.Dp(30)))
		cardH := minInt(gtx.Dp(unit.Dp(520)), gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)))
		gtx.Constraints.Min = image.Pt(cardW, cardH)
		gtx.Constraints.Max = image.Pt(cardW, cardH)
		return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
					return layout.Dimensions{Size: gtx.Constraints.Max}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(14), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						var children []layout.FlexChild
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(14), "Extract SGA archives")
							l.Font.Weight = font.Medium
							return l.Layout(gtx)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Select one or more current/parent SGA archives. Existing current-mod files are preserved.")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout))
						children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							ui.ArchiveList.Axis = layout.Vertical
							return ui.ArchiveList.Layout(gtx, len(ui.ArchiveOptions), func(gtx layout.Context, index int) layout.Dimensions {
								opt := ui.ArchiveOptions[index]
								click := &ui.ArchiveSelectClicks[index]
								bg := uikit.CPanelBg
								if click.Hovered() {
									bg = uikit.CMenuHover
								}
								return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(28))
									st := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(28)))}.Push(gtx.Ops)
									paint.ColorOp{Color: bg}.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									st.Pop()
									return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												box := image.Pt(gtx.Dp(unit.Dp(14)), gtx.Dp(unit.Dp(14)))
												col := uikit.CBorder
												if ui.ArchiveSelected[index] {
													col = rgb(0x0078D4)
												}
												st := clip.Rect{Max: box}.Push(gtx.Ops)
												paint.ColorOp{Color: col}.Add(gtx.Ops)
												paint.PaintOp{}.Add(gtx.Ops)
												st.Pop()
												return layout.Dimensions{Size: box}
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(11), opt.Label)
												l.Color = uikit.CText
												return l.Layout(gtx)
											}),
										)
									})
								})
							})
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), ui.ArchiveStatus)
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return SmallButton(ui, gtx, &ui.ArchiveCloseClick, "Close")
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return SmallButton(ui, gtx, &ui.ArchiveExtractClick, "Extract selected")
								}),
							)
						}))
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
					})
				}),
			)
		})
	})
}
