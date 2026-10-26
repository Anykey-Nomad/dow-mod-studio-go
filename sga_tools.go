package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sga"
	"dow-mod-studio/internal/sgapack"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type archiveOption struct {
	source *modtree.FileSource
	label  string
}

func (ui *uiState) openArchiveDialog() {
	if ui.modTree == nil {
		ui.statusText = "Extract SGA — no mod loaded"
		return
	}
	ui.archiveOptions = nil
	seen := make(map[string]bool)
	for _, source := range ui.modTree.Sources {
		if source == nil || !source.IsSGA || source.Kind == modtree.KindEngine || source.SGAHandle == nil {
			continue
		}
		key := strings.ToLower(source.ArchivePath)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		label := fmt.Sprintf("%s — %s", source.ModName, source.SourceName)
		ui.archiveOptions = append(ui.archiveOptions, archiveOption{source: source, label: label})
	}
	sort.Slice(ui.archiveOptions, func(i, j int) bool {
		return strings.ToLower(ui.archiveOptions[i].label) < strings.ToLower(ui.archiveOptions[j].label)
	})
	ui.archiveSelected = make([]bool, len(ui.archiveOptions))
	ui.archiveSelectClicks = make([]widget.Clickable, len(ui.archiveOptions))
	for i := range ui.archiveSelected {
		ui.archiveSelected[i] = true
	}
	ui.archiveList = layout.List{}
	ui.archiveStatus = fmt.Sprintf("%d SGA archive(s) available", len(ui.archiveOptions))
	ui.archiveDialog = len(ui.archiveOptions) > 0
	if !ui.archiveDialog {
		ui.statusText = "Extract SGA — no current/parent SGA archives found"
	}
}

func (ui *uiState) currentModFiles() (map[string]string, error) {
	if ui.modTree == nil {
		return nil, fmt.Errorf("no mod loaded")
	}
	roots := make(map[string]bool)
	modRoot := currentModFolderRoot(ui.modTree)
	for _, source := range ui.modTree.Sources {
		if source != nil && !source.IsSGA && source.Kind == modtree.KindThisMod && source.DiskRoot != "" && pathWithinRoot(source.DiskRoot, modRoot) {
			roots[filepath.Clean(source.DiskRoot)] = true
		}
	}
	if root, err := ui.modTree.ResolveWritableRoot(); err == nil && pathWithinRoot(root, modRoot) {
		roots[filepath.Clean(root)] = true
	}
	files := make(map[string]string)
	for root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() || strings.EqualFold(filepath.Ext(entry.Name()), ".sga") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			virtual := "Data\\" + filepath.ToSlash(rel)
			if _, exists := files[virtual]; !exists {
				files[virtual] = path
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func (ui *uiState) packCurrentModSGA() {
	if ui.modTree == nil || ui.modTree.ModFilePath == "" {
		ui.statusText = "Pack SGA — no mod loaded"
		return
	}
	mf, err := modtree.ParseModule(ui.modTree.ModFilePath)
	if err != nil {
		ui.statusText = "Pack SGA failed: " + err.Error()
		return
	}
	modFolder := mf.ModFolder
	if modFolder == "" {
		modFolder = mf.FileMapName
	}
	output := filepath.Join(ui.modTree.AppPath, modFolder, ui.modTree.FileMapName+".sga")
	if _, err := os.Stat(output); err == nil {
		output = filepath.Join(ui.modTree.AppPath, modFolder, ui.modTree.FileMapName+"_packed.sga")
	}
	files, err := ui.currentModFiles()
	if err != nil || len(files) == 0 {
		if err != nil {
			ui.statusText = "Pack SGA failed: " + err.Error()
		} else {
			ui.statusText = "Pack SGA — no current mod files found"
		}
		return
	}
	if err := writeSGA2(output, files, "Data"); err != nil {
		ui.statusText = "Pack SGA failed: " + err.Error()
		return
	}
	archiveBase := strings.TrimSuffix(filepath.Base(output), filepath.Ext(output))
	if err := addModuleArchive(ui.modTree.ModFilePath, archiveBase); err != nil {
		ui.statusText = "SGA created, but .module update failed: " + err.Error()
		return
	}
	ui.modSnapshot = ""
	ui.lastModPoll = time.Time{}
	ui.statusText = fmt.Sprintf("Packed %d files to %s", len(files), output)
}

func addModuleArchive(modulePath, archiveBase string) error {
	m, err := modtree.ParseModule(modulePath)
	if err != nil {
		return err
	}
	for _, existing := range m.Archives {
		base := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(existing, "/", "\\")), filepath.Ext(existing))
		if strings.EqualFold(base, archiveBase) {
			return nil
		}
	}
	data, err := os.ReadFile(modulePath)
	if err != nil {
		return err
	}
	text := string(data)
	next := 1
	for key := range m.Archives {
		if key >= next {
			next = key + 1
		}
	}
	line := fmt.Sprintf("ArchiveFile.%d=%s", next, archiveBase)
	lines := strings.Split(text, "\n")
	insertAt := len(lines)
	globalSeen := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.EqualFold(trim, "[global]") {
			globalSeen = true
			continue
		}
		if globalSeen && strings.HasPrefix(trim, "[") {
			insertAt = i
			break
		}
	}
	if !globalSeen {
		return fmt.Errorf("module has no [global] section")
	}
	updated := append([]string{}, lines[:insertAt]...)
	updated = append(updated, line)
	updated = append(updated, lines[insertAt:]...)
	return os.WriteFile(modulePath, []byte(strings.Join(updated, "\n")), 0644)
}

func (ui *uiState) extractSelectedArchives() {
	if ui.modTree == nil {
		return
	}
	selected := 0
	filesWritten := 0
	filesSkipped := 0
	for i, option := range ui.archiveOptions {
		if i >= len(ui.archiveSelected) || !ui.archiveSelected[i] || option.source == nil || option.source.SGAHandle == nil {
			continue
		}
		selected++
		for toc := 0; toc < option.source.SGAHandle.ToCCount(); toc++ {
			root := option.source.SGAHandle.ToCName(toc)
			if root == "" {
				root = "Data"
			}
			dir, err := option.source.SGAHandle.RootDir(toc)
			if err != nil {
				continue
			}
			filesWritten, filesSkipped = ui.extractSGADir(option.source.SGAHandle, dir, root, filesWritten, filesSkipped)
		}
	}
	ui.archiveStatus = fmt.Sprintf("Extracted %d file(s), skipped %d existing", filesWritten, filesSkipped)
	ui.statusText = fmt.Sprintf("Extracted %d SGA archive(s): %d written, %d skipped", selected, filesWritten, filesSkipped)
	ui.modSnapshot = ""
	ui.lastModPoll = time.Time{}
}

func (ui *uiState) extractSGADir(sga *sga.SGAFile, dirID int, virtualDir string, written, skipped int) (int, int) {
	entries, err := sga.List(dirID)
	if err != nil {
		return written, skipped
	}
	for _, entry := range entries {
		virtual := virtualDir + "\\" + entry.Name
		if entry.IsDir {
			written, skipped = ui.extractSGADir(sga, entry.DirID, virtual, written, skipped)
			continue
		}
		if _, err := os.Stat(func() string {
			p, err := ui.modTree.OverlayPath(virtual)
			if err != nil {
				return ""
			}
			return p
		}()); err == nil {
			skipped++
			continue
		}
		data, err := sga.ReadFile(entry.FileID)
		if err != nil {
			skipped++
			continue
		}
		if _, err := ui.modTree.WriteOverlayFile(virtual, data); err != nil {
			skipped++
			continue
		}
		written++
	}
	return written, skipped
}

func (ui *uiState) handleArchiveDialogInput(gtx layout.Context) {
	if !ui.archiveDialog {
		return
	}
	for i := range ui.archiveSelectClicks {
		if ui.archiveSelectClicks[i].Clicked(gtx) && i < len(ui.archiveSelected) {
			ui.archiveSelected[i] = !ui.archiveSelected[i]
		}
	}
	if ui.archiveCloseClick.Clicked(gtx) {
		ui.archiveDialog = false
		return
	}
	if ui.archiveExtractClick.Clicked(gtx) {
		ui.extractSelectedArchives()
	}
}

func (ui *uiState) layoutArchiveDialog(gtx layout.Context) layout.Dimensions {
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
		return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
					return layout.Dimensions{Size: gtx.Constraints.Max}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(14), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						var children []layout.FlexChild
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(14), "Extract SGA archives")
							l.Font.Weight = font.Medium
							return l.Layout(gtx)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), "Select one or more current/parent SGA archives. Existing current-mod files are preserved.")
							l.Color = cTextDim
							return l.Layout(gtx)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout))
						children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							ui.archiveList.Axis = layout.Vertical
							return ui.archiveList.Layout(gtx, len(ui.archiveOptions), func(gtx layout.Context, index int) layout.Dimensions {
								opt := ui.archiveOptions[index]
								click := &ui.archiveSelectClicks[index]
								bg := cPanelBg
								if click.Hovered() {
									bg = cMenuHover
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
												col := cBorder
												if ui.archiveSelected[index] {
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
												l := material.Label(ui.th, unit.Sp(11), opt.label)
												l.Color = cText
												return l.Layout(gtx)
											}),
										)
									})
								})
							})
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), ui.archiveStatus)
							l.Color = cTextDim
							return l.Layout(gtx)
						}))
						children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout))
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &ui.archiveCloseClick, "Close") }),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return ui.smallButton(gtx, &ui.archiveExtractClick, "Extract selected")
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

func colorWhite() color.NRGBA { return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} }

// writeSGA2 packs a folder of files into an SGA v2 archive; the format work
// lives in internal/sgapack, this is the name the packer UI calls.
func writeSGA2(output string, files map[string]string, tocName string) error {
	return sgapack.WriteSGA2(output, files, tocName)
}
