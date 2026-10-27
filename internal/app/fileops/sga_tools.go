package fileops

import (
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sga"
	"dow-mod-studio/internal/sgapack"
	"fmt"
	"gioui.org/layout"
	"gioui.org/widget"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ArchiveOption = uistate.ArchiveOption

func OpenArchiveDialog(ui *uistate.State) {
	if ui.ModTree == nil {
		ui.StatusText = "Extract SGA — no mod loaded"
		return
	}
	ui.ArchiveOptions = nil
	seen := make(map[string]bool)
	for _, source := range ui.ModTree.Sources {
		if source == nil || !source.IsSGA || source.Kind == modtree.KindEngine || source.SGAHandle == nil {
			continue
		}
		key := strings.ToLower(source.ArchivePath)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		label := fmt.Sprintf("%s — %s", source.ModName, source.SourceName)
		ui.ArchiveOptions = append(ui.ArchiveOptions, ArchiveOption{Source: source, Label: label})
	}
	sort.Slice(ui.ArchiveOptions, func(i, j int) bool {
		return strings.ToLower(ui.ArchiveOptions[i].Label) < strings.ToLower(ui.ArchiveOptions[j].Label)
	})
	ui.ArchiveSelected = make([]bool, len(ui.ArchiveOptions))
	ui.ArchiveSelectClicks = make([]widget.Clickable, len(ui.ArchiveOptions))
	for i := range ui.ArchiveSelected {
		ui.ArchiveSelected[i] = true
	}
	ui.ArchiveList = layout.List{}
	ui.ArchiveStatus = fmt.Sprintf("%d SGA archive(s) available", len(ui.ArchiveOptions))
	ui.ArchiveDialog = len(ui.ArchiveOptions) > 0
	if !ui.ArchiveDialog {
		ui.StatusText = "Extract SGA — no current/parent SGA archives found"
	}
}

func CurrentModFiles(ui *uistate.State) (map[string]string, error) {
	if ui.ModTree == nil {
		return nil, fmt.Errorf("no mod loaded")
	}
	roots := make(map[string]bool)
	modRoot := CurrentModFolderRoot(ui.ModTree)
	for _, source := range ui.ModTree.Sources {
		if source != nil && !source.IsSGA && source.Kind == modtree.KindThisMod && source.DiskRoot != "" && PathWithinRoot(source.DiskRoot, modRoot) {
			roots[filepath.Clean(source.DiskRoot)] = true
		}
	}
	if root, err := ui.ModTree.ResolveWritableRoot(); err == nil && PathWithinRoot(root, modRoot) {
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

func PackCurrentModSGA(ui *uistate.State) {
	if ui.ModTree == nil || ui.ModTree.ModFilePath == "" {
		ui.StatusText = "Pack SGA — no mod loaded"
		return
	}
	mf, err := modtree.ParseModule(ui.ModTree.ModFilePath)
	if err != nil {
		ui.StatusText = "Pack SGA failed: " + err.Error()
		return
	}
	modFolder := mf.ModFolder
	if modFolder == "" {
		modFolder = mf.FileMapName
	}
	output := filepath.Join(ui.ModTree.AppPath, modFolder, ui.ModTree.FileMapName+".sga")
	if _, err := os.Stat(output); err == nil {
		output = filepath.Join(ui.ModTree.AppPath, modFolder, ui.ModTree.FileMapName+"_packed.sga")
	}
	files, err := CurrentModFiles(ui)
	if err != nil || len(files) == 0 {
		if err != nil {
			ui.StatusText = "Pack SGA failed: " + err.Error()
		} else {
			ui.StatusText = "Pack SGA — no current mod files found"
		}
		return
	}
	if err := WriteSGA2(output, files, "Data"); err != nil {
		ui.StatusText = "Pack SGA failed: " + err.Error()
		return
	}
	archiveBase := strings.TrimSuffix(filepath.Base(output), filepath.Ext(output))
	if err := AddModuleArchive(ui.ModTree.ModFilePath, archiveBase); err != nil {
		ui.StatusText = "SGA created, but .module update failed: " + err.Error()
		return
	}
	ui.ModSnapshot = ""
	ui.LastModPoll = time.Time{}
	ui.StatusText = fmt.Sprintf("Packed %d files to %s", len(files), output)
}

func AddModuleArchive(modulePath, archiveBase string) error {
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

func ExtractSelectedArchives(ui *uistate.State) {
	if ui.ModTree == nil {
		return
	}
	selected := 0
	filesWritten := 0
	filesSkipped := 0
	for i, option := range ui.ArchiveOptions {
		if i >= len(ui.ArchiveSelected) || !ui.ArchiveSelected[i] || option.Source == nil || option.Source.SGAHandle == nil {
			continue
		}
		selected++
		for toc := 0; toc < option.Source.SGAHandle.ToCCount(); toc++ {
			root := option.Source.SGAHandle.ToCName(toc)
			if root == "" {
				root = "Data"
			}
			dir, err := option.Source.SGAHandle.RootDir(toc)
			if err != nil {
				continue
			}
			filesWritten, filesSkipped = ExtractSGADir(ui, option.Source.SGAHandle, dir, root, filesWritten, filesSkipped)
		}
	}
	ui.ArchiveStatus = fmt.Sprintf("Extracted %d file(s), skipped %d existing", filesWritten, filesSkipped)
	ui.StatusText = fmt.Sprintf("Extracted %d SGA archive(s): %d written, %d skipped", selected, filesWritten, filesSkipped)
	ui.ModSnapshot = ""
	ui.LastModPoll = time.Time{}
}

func ExtractSGADir(ui *uistate.State, sga *sga.SGAFile, dirID int, virtualDir string, written, skipped int) (int, int) {
	entries, err := sga.List(dirID)
	if err != nil {
		return written, skipped
	}
	for _, entry := range entries {
		virtual := virtualDir + "\\" + entry.Name
		if entry.IsDir {
			written, skipped = ExtractSGADir(ui, sga, entry.DirID, virtual, written, skipped)
			continue
		}
		if _, err := os.Stat(func() string {
			p, err := ui.ModTree.OverlayPath(virtual)
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
		if _, err := ui.ModTree.WriteOverlayFile(virtual, data); err != nil {
			skipped++
			continue
		}
		written++
	}
	return written, skipped
}

func HandleArchiveDialogInput(ui *uistate.State, gtx layout.Context) {
	if !ui.ArchiveDialog {
		return
	}
	for i := range ui.ArchiveSelectClicks {
		if ui.ArchiveSelectClicks[i].Clicked(gtx) && i < len(ui.ArchiveSelected) {
			ui.ArchiveSelected[i] = !ui.ArchiveSelected[i]
		}
	}
	if ui.ArchiveCloseClick.Clicked(gtx) {
		ui.ArchiveDialog = false
		return
	}
	if ui.ArchiveExtractClick.Clicked(gtx) {
		ExtractSelectedArchives(ui)
	}
}

func ColorWhite() color.NRGBA { return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF} }

// WriteSGA2 packs a folder of files into an SGA v2 archive; the format work
// lives in internal/sgapack, this is the name the packer UI calls.
func WriteSGA2(output string, files map[string]string, tocName string) error {
	return sgapack.WriteSGA2(output, files, tocName)
}
