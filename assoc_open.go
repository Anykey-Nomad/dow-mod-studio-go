package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dow-mod-studio/internal/sga"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// ── opening files handed over by the OS ─────────────────────────────────────
//
// A registered association launches the studio with a real path on the command
// line. A .module is picked up by tryAutoLoad; these formats open their viewer
// instead. The path is a real one, not a tree path, so the viewers read it
// straight off disk — the same bytes the tree would hand them when the file
// happens to live inside the open mod.

// findFirstAssocArg returns the first CLI arg naming a format the studio opens
// through a file association (a .module has its own entry point).
func findFirstAssocArg() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		trim := strings.TrimSpace(strings.Trim(a, "\"'"))
		if trim == "" {
			continue
		}
		if _, ok := assocTypeFor(pickExt(trim)); ok && !strings.EqualFold(pickExt(trim), moduleAssoc.ext) {
			return trim
		}
	}
	return ""
}

// openAssociatedFile opens one OS file argument by its format. It runs once,
// at startup, after the module is in place.
func (ui *uiState) openAssociatedFile(path string) {
	log.Printf("openAssociatedFile: %q", path)
	switch strings.ToLower(pickExt(path)) {
	case ".fda":
		ui.openFda(path)
	case ".whm":
		ui.openWhm(path)
	case ".whe":
		ui.openWheViaUnit(path)
	case ".sga":
		ui.openSgaArchive(path)
	default:
		ui.statusText = fmt.Sprintf("Cannot open %s — unknown format", filepath.Base(path))
		return
	}
	ui.markStateDirty()
}

// readTabFile reads the bytes a tab is opened from. A virtual path belongs to
// the open module's tree; an absolute one is a file the OS handed the studio
// through an association, read straight off disk.
func (ui *uiState) readTabFile(path string) ([]byte, error) {
	if ui.modTree != nil && !filepath.IsAbs(path) {
		return ui.modTree.ReadFile(path)
	}
	return os.ReadFile(path)
}

// ── .whe → the unit that carries its events ─────────────────────────────────

// openWheViaUnit opens a .whe the way the game itself refers to it: through
// the unit whose model carries those events. A model's .whm and .whe always
// share their name, so the first unit of the open mod whose resolved model has
// the file's own name is shown in the Object tab. With no such unit the file
// is simply reported, since a .whe alone has no viewer of its own.
func (ui *uiState) openWheViaUnit(path string) {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), pickExt(path)))
	if ui.modTree == nil {
		ui.statusText = fmt.Sprintf("Open %s — no module loaded", filepath.Base(path))
		return
	}
	cat := ui.unitCatalogFor()
	start := time.Now()
	units := cat.AllUnits()
	for _, u := range units {
		d := loadUnitDetails(ui.modTree, u)
		src, ok := newUnitModelSource(ui.modTree, d)
		if !ok {
			continue
		}
		model := unitModelPath(ui.modTree, src)
		if model == "" {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(model), pickExt(model)))
		if base != name {
			continue
		}
		ui.unitPickRace = u.Race
		ui.unitPickPath = u.Path
		ui.applyPendingUnitPick()
		ui.openUnitTab()
		ui.statusText = fmt.Sprintf("Opened %s as unit %s", filepath.Base(path), u.DisplayName())
		log.Printf("openWheViaUnit %q: unit %s after scanning %d units in %v", path, u.Path, len(units), time.Since(start))
		return
	}
	ui.statusText = fmt.Sprintf("Open %s — no unit references that model", filepath.Base(path))
	log.Printf("openWheViaUnit %q: no match among %d units in %v", path, len(units), time.Since(start))
}

// ── .sga → a listing of the archive ─────────────────────────────────────────

// sgaRow is one line of the archive listing tab.
type sgaRow struct {
	path  string // the entry's path inside the archive, e.g. "Data\art\..."
	isDir bool
	size  int64
}

// openSgaArchive opens an .sga as a read-only listing of what it carries.
// Nothing is extracted: the archive is the game's own packaging, and a listing
// answers what is inside without writing a byte of it back out.
func (ui *uiState) openSgaArchive(path string) {
	if idx := ui.findTabIndex(path); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s", filepath.Base(path))
		return
	}
	t := &fileTab{path: path, kind: "sga"}
	arch, err := sga.OpenSGA(path)
	if err != nil {
		t.sgaError = err.Error()
	} else {
		t.sgaRows = sgaArchiveRows(arch)
		arch.Close()
	}
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	if t.sgaError != "" {
		ui.statusText = "Open " + filepath.Base(path) + " failed: " + t.sgaError
	} else {
		ui.statusText = fmt.Sprintf("Opened %s — %d entries", filepath.Base(path), len(t.sgaRows))
	}
}

// sgaArchiveRows flattens an archive's directory tree into listing rows, one
// ToC after another in the order the archive carries them.
func sgaArchiveRows(arch *sga.SGAFile) []sgaRow {
	var rows []sgaRow
	for toc := 0; toc < arch.ToCCount(); toc++ {
		root := arch.ToCName(toc)
		if root == "" {
			root = "Data"
		}
		dir, err := arch.RootDir(toc)
		if err != nil {
			continue
		}
		rows = sgaWalkRows(arch, dir, root, rows)
	}
	return rows
}

// sgaWalkRows appends one directory's entries and everything under them.
func sgaWalkRows(arch *sga.SGAFile, dirID int, prefix string, rows []sgaRow) []sgaRow {
	entries, err := arch.List(dirID)
	if err != nil {
		return rows
	}
	for _, e := range entries {
		p := prefix + "\\" + e.Name
		if e.IsDir {
			rows = append(rows, sgaRow{path: p, isDir: true})
			rows = sgaWalkRows(arch, e.DirID, p, rows)
			continue
		}
		rows = append(rows, sgaRow{path: p, size: e.Size})
	}
	return rows
}

// sgaSizeLabel is a file's size as the listing shows it.
func sgaSizeLabel(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%d KB", (size+1023)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

// layoutSgaListing is the archive tab's body: every folder and file the .sga
// carries, one per row. Folders run on in the same list rather than opening,
// because the whole point of the tab is to see the archive at a glance.
func (ui *uiState) layoutSgaListing(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.sgaError != "" {
		return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "Failed: "+t.sgaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if len(t.sgaRows) == 0 {
		return centeredPanelLabel(gtx, ui.th, "Empty archive", unit.Sp(10), cTextDim)
	}
	t.sgaList.Axis = layout.Vertical
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return ui.layoutListWithScrollbar(gtx, &t.sgaScroll, &t.sgaList, 20, len(t.sgaRows), func(gtx layout.Context, index int) layout.Dimensions {
			r := t.sgaRows[index]
			gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(20))
			gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(20))
			name := r.path
			if r.isDir {
				name += "\\"
			}
			size := ""
			if !r.isDir {
				size = sgaSizeLabel(r.size)
			}
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(10), name)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Flexed(1, flexGap()),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(10), size)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
		})
	})
}
