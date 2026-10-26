package app

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dow-mod-studio/internal/assoc"
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
		if _, ok := assocTypeFor(pickExt(trim)); ok && !strings.EqualFold(pickExt(trim), moduleAssoc.Ext) {
			return trim
		}
	}
	return ""
}

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

// readTabFile reads the bytes a tab is opened from. A virtual path belongs to
// the open module's tree; an absolute one is a file the OS handed the studio
// through an association, read straight off disk.
func ReadTabFile(ui *uiState, path string) ([]byte, error) {
	if ui.ModTree != nil && !filepath.IsAbs(path) {
		return ui.ModTree.ReadFile(path)
	}
	return os.ReadFile(path)
}

// ── .whe → the unit that carries its events ─────────────────────────────────

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
func OpenSgaArchive(ui *uiState, path string) {
	if idx := FindTabIndex(ui, path); idx >= 0 {
		ui.TabSel = idx + 1
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s", filepath.Base(path))
		return
	}
	t := &fileTab{Path: path, Kind: "sga"}
	arch, err := sga.OpenSGA(path)
	if err != nil {
		t.SgaError = err.Error()
	} else {
		t.SgaRows = sgaArchiveRows(arch)
		arch.Close()
	}
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	EnsureTabClicks(ui)
	if t.SgaError != "" {
		ui.StatusText = "Open " + filepath.Base(path) + " failed: " + t.SgaError
	} else {
		ui.StatusText = fmt.Sprintf("Opened %s — %d entries", filepath.Base(path), len(t.SgaRows))
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
func LayoutSgaListing(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.SgaError != "" {
		return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.SgaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if len(t.SgaRows) == 0 {
		return centeredPanelLabel(gtx, ui.Th, "Empty archive", unit.Sp(10), cTextDim)
	}
	t.SgaList.Axis = layout.Vertical
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return LayoutListWithScrollbar(ui, gtx, &t.SgaScroll, &t.SgaList, 20, len(t.SgaRows), func(gtx layout.Context, index int) layout.Dimensions {
			r := t.SgaRows[index]
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
						l := material.Label(ui.Th, unit.Sp(10), name)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Flexed(1, flexGap()),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), size)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
		})
	})
}

// refreshAssocStates re-reads which formats the studio is associated with, so
// the Settings buttons can say what a click would do. A query that fails is
// shown as "not associated": the button then offers the safe action.
func RefreshAssocStates(ui *uiState) {
	ui.AssocOn = make([]bool, len(assocTypes))
	if !associationsSupported() {
		return
	}
	for i, t := range assocTypes {
		on, _, _ := isExtAssociated(t)
		ui.AssocOn[i] = on
	}
}

// toggleAssociation adds or removes one format's association and re-reads the
// row's state, so the button reflects the registry at once rather than the
// next time the dialog is opened.
func ToggleAssociation(ui *uiState, i int) {
	if i < 0 || i >= len(assocTypes) {
		return
	}
	t := assocTypes[i]
	on, _, _ := isExtAssociated(t)
	var err error
	if on {
		err = unregisterExtAssociation(t)
	} else {
		err = registerExtAssociation(t)
	}
	if err != nil {
		log.Printf("association %s: %v", t.Ext, err)
		ui.StatusText = fmt.Sprintf("%s association failed: %v", t.Ext, err)
		RefreshAssocStates(ui)
		return
	}
	if on {
		log.Printf("association %s: removed", t.Ext)
		ui.StatusText = fmt.Sprintf("Removed %s association", t.Ext)
	} else {
		log.Printf("association %s: registered for %s", t.Ext, getExePath())
		ui.StatusText = fmt.Sprintf("Associated %s with %s", t.Ext, getExePath())
	}
	RefreshAssocStates(ui)
}

// assocTypeFor returns the table row for an extension, ".module" included.
func assocTypeFor(ext string) (assocType, bool) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == moduleAssoc.Ext {
		return moduleAssoc, true
	}
	for _, t := range assocTypes {
		if t.Ext == ext {
			return t, true
		}
	}
	return assocType{}, false
}

// ── file associations (internal/assoc) ───────────────────────────────────────
//
// The registry half of the associations lives in internal/assoc; these are
// the names the settings dialog and the openers keep using.

type assocType = assoc.AssocType

var (
	moduleAssoc = assoc.ModuleAssoc
	assocTypes  = assoc.AssocTypes
)

func associationsSupported() bool { return assoc.AssociationsSupported() }

func isExtAssociated(t assocType) (bool, string, error) { return assoc.IsExtAssociated(t) }
func registerExtAssociation(t assocType) error          { return assoc.RegisterExtAssociation(t) }
func unregisterExtAssociation(t assocType) error        { return assoc.UnregisterExtAssociation(t) }

func isModuleAssociated() (bool, string, error) { return assoc.IsModuleAssociated() }
func registerModuleAssociation() error          { return assoc.RegisterModuleAssociation() }
func unregisterModuleAssociation() error        { return assoc.UnregisterModuleAssociation() }

func getExePath() string { return assoc.GetExePath() }

// ─── merged from module_discovery.go ───────────────────────────────────────────────

type moduleCandidate struct {
	path   string
	folder string
}

const steamDowFolder = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`

// discoverModuleFiles lists the .module files directly inside the folders.
func discoverModuleFiles(dirs []string) []moduleCandidate {
	var modules []moduleCandidate
	for _, r := range listPickFiles(dirs, func(name string) bool { return pickExt(name) == ".module" }) {
		modules = append(modules, moduleCandidate{path: r.path, folder: r.folder})
	}
	return modules
}

// moduleRoots are the folders the Open Module dialog starts from.
func ModuleRoots(ui *uiState) []string {
	dirs := make([]string, 0, 6)
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	dirs = append(dirs,
		`C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`,
		`C:\Program Files (x86)\Steam\steamapps\common\Dawn of War Definitive Edition`,
	)
	if ui.GamePath != "" {
		dirs = append(dirs, ui.GamePath)
	}
	dirs = append(dirs, ui.RecentFolders...)
	if ui.ModulePath != "" {
		dirs = append(dirs, filepath.Dir(ui.ModulePath))
	}
	return dirs
}

// openModulePicker shows the Open Module dialog over the .module files.
func OpenModulePicker(ui *uiState) {
	OpenFilePicker(ui, pickConfig{
		title:    "Open Module",
		subtitle: "Enter a .module file or the Dawn of War folder to scan",
		hint:     `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`,
		exts:     []string{".module"},
		roots:    func() []string { return ModuleRoots(ui) },
		start:    ui.ModulePath,
		onPick: func(path string) (bool, string) {
			path = ResolveModulePath(ui, path)
			if !pathExists(path) {
				return false, "No such module file: " + path
			}
			LoadModule(ui, path)
			return true, ""
		},
	})
}

// resolveModulePath finds a module named relative to the app or to the game
// folder, which is where a bare name typed in the dialog is looked for.
func ResolveModulePath(ui *uiState, path string) string {
	if !filepath.IsAbs(path) && pickExt(path) == ".module" {
		if ui.ModTree != nil && ui.ModTree.AppPath != "" {
			if cand := filepath.Join(ui.ModTree.AppPath, path); pathExists(cand) {
				return cand
			}
		}
		if !pathExists(path) {
			if cand := filepath.Join(steamDowFolder, path); pathExists(cand) {
				return cand
			}
		}
	}
	if !pathExists(path) {
		if cand := filepath.Join(steamDowFolder, path+".module"); pathExists(cand) {
			return cand
		}
	}
	return path
}
