package fileops

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/assoc"
	"dow-mod-studio/internal/sga"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// FindFirstAssocArg returns the first CLI arg naming a format the studio opens
// through a file association (a .module has its own entry point).
func FindFirstAssocArg() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		trim := strings.TrimSpace(strings.Trim(a, "\"'"))
		if trim == "" {
			continue
		}
		if _, ok := AssocTypeFor(PickExt(trim)); ok && !strings.EqualFold(PickExt(trim), ModuleAssoc.Ext) {
			return trim
		}
	}
	return ""
}

// readTabFile reads the bytes a tab is opened from. A virtual path belongs to
// the open module's tree; an absolute one is a file the OS handed the studio
// through an association, read straight off disk.
func ReadTabFile(ui *uistate.State, path string) ([]byte, error) {
	if ui.ModTree != nil && !filepath.IsAbs(path) {
		return ui.ModTree.ReadFile(path)
	}
	return os.ReadFile(path)
}

// SgaRow is one line of the archive listing tab.
type SgaRow = uistate.SgaRow

// openSgaArchive opens an .sga as a read-only listing of what it carries.
// Nothing is extracted: the archive is the game's own packaging, and a listing
// answers what is inside without writing a byte of it back out.
func OpenSgaArchive(ui *uistate.State, path string) {
	if idx := uikit.FindTabIndex(ui, path); idx >= 0 {
		ui.TabSel = idx + 1
		uikit.EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s", filepath.Base(path))
		return
	}
	t := &uistate.Tab{Path: path, Kind: "sga"}
	arch, err := sga.OpenSGA(path)
	if err != nil {
		t.SgaError = err.Error()
	} else {
		t.SgaRows = SgaArchiveRows(arch)
		arch.Close()
	}
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	uikit.EnsureTabClicks(ui)
	if t.SgaError != "" {
		ui.StatusText = "Open " + filepath.Base(path) + " failed: " + t.SgaError
	} else {
		ui.StatusText = fmt.Sprintf("Opened %s — %d entries", filepath.Base(path), len(t.SgaRows))
	}
}

// SgaArchiveRows flattens an archive's directory tree into listing rows, one
// ToC after another in the order the archive carries them.
func SgaArchiveRows(arch *sga.SGAFile) []SgaRow {
	var rows []SgaRow
	for toc := 0; toc < arch.ToCCount(); toc++ {
		root := arch.ToCName(toc)
		if root == "" {
			root = "Data"
		}
		dir, err := arch.RootDir(toc)
		if err != nil {
			continue
		}
		rows = SgaWalkRows(arch, dir, root, rows)
	}
	return rows
}

// SgaWalkRows appends one directory's entries and everything under them.
func SgaWalkRows(arch *sga.SGAFile, dirID int, prefix string, rows []SgaRow) []SgaRow {
	entries, err := arch.List(dirID)
	if err != nil {
		return rows
	}
	for _, e := range entries {
		p := prefix + "\\" + e.Name
		if e.IsDir {
			rows = append(rows, SgaRow{Path: p, IsDir: true})
			rows = SgaWalkRows(arch, e.DirID, p, rows)
			continue
		}
		rows = append(rows, SgaRow{Path: p, Size: e.Size})
	}
	return rows
}

// SgaSizeLabel is a file's size as the listing shows it.
func SgaSizeLabel(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%d KB", (size+1023)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

// refreshAssocStates re-reads which formats the studio is associated with, so
// the Settings buttons can say what a click would do. A query that fails is
// shown as "not associated": the button then offers the safe action.
func RefreshAssocStates(ui *uistate.State) {
	ui.AssocOn = make([]bool, len(AssocTypes))
	if !AssociationsSupported() {
		return
	}
	for i, t := range AssocTypes {
		on, _, _ := IsExtAssociated(t)
		ui.AssocOn[i] = on
	}
}

// toggleAssociation adds or removes one format's association and re-reads the
// row's state, so the button reflects the registry at once rather than the
// next time the dialog is opened.
func ToggleAssociation(ui *uistate.State, i int) {
	if i < 0 || i >= len(AssocTypes) {
		return
	}
	t := AssocTypes[i]
	on, _, _ := IsExtAssociated(t)
	var err error
	if on {
		err = UnregisterExtAssociation(t)
	} else {
		err = RegisterExtAssociation(t)
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
		log.Printf("association %s: registered for %s", t.Ext, GetExePath())
		ui.StatusText = fmt.Sprintf("Associated %s with %s", t.Ext, GetExePath())
	}
	RefreshAssocStates(ui)
}

// AssocTypeFor returns the table row for an extension, ".module" included.
func AssocTypeFor(ext string) (AssocType, bool) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == ModuleAssoc.Ext {
		return ModuleAssoc, true
	}
	for _, t := range AssocTypes {
		if t.Ext == ext {
			return t, true
		}
	}
	return AssocType{}, false
}

type AssocType = assoc.AssocType

var (
	ModuleAssoc = assoc.ModuleAssoc
	AssocTypes  = assoc.AssocTypes
)

func AssociationsSupported() bool { return assoc.AssociationsSupported() }

func IsExtAssociated(t AssocType) (bool, string, error) { return assoc.IsExtAssociated(t) }

func RegisterExtAssociation(t AssocType) error { return assoc.RegisterExtAssociation(t) }

func UnregisterExtAssociation(t AssocType) error { return assoc.UnregisterExtAssociation(t) }

func IsModuleAssociated() (bool, string, error) { return assoc.IsModuleAssociated() }

func RegisterModuleAssociation() error { return assoc.RegisterModuleAssociation() }

func UnregisterModuleAssociation() error { return assoc.UnregisterModuleAssociation() }

func GetExePath() string { return assoc.GetExePath() }

type ModuleCandidate struct {
	path   string
	folder string
}

const SteamDowFolder = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`

// DiscoverModuleFiles lists the .module files directly inside the folders.
func DiscoverModuleFiles(dirs []string) []ModuleCandidate {
	var modules []ModuleCandidate
	for _, r := range ListPickFiles(dirs, func(name string) bool { return PickExt(name) == ".module" }) {
		modules = append(modules, ModuleCandidate{path: r.Path, folder: r.Folder})
	}
	return modules
}

// moduleRoots are the folders the Open Module dialog starts from.
func ModuleRoots(ui *uistate.State) []string {
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

// resolveModulePath finds a module named relative to the app or to the game
// folder, which is where a bare name typed in the dialog is looked for.
func ResolveModulePath(ui *uistate.State, path string) string {
	if !filepath.IsAbs(path) && PickExt(path) == ".module" {
		if ui.ModTree != nil && ui.ModTree.AppPath != "" {
			if cand := filepath.Join(ui.ModTree.AppPath, path); PathExists(cand) {
				return cand
			}
		}
		if !PathExists(path) {
			if cand := filepath.Join(SteamDowFolder, path); PathExists(cand) {
				return cand
			}
		}
	}
	if !PathExists(path) {
		if cand := filepath.Join(SteamDowFolder, path+".module"); PathExists(cand) {
			return cand
		}
	}
	return path
}
