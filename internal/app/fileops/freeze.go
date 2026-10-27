package fileops

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	appunit "dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/modgraph"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/projects"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
	"encoding/json"
	"fmt"
	"gioui.org/layout"
	"os"
	"path/filepath"
	"strings"
)

// FreezeFile is one file moved out of the mod.
type FreezeFile = uistate.FreezeFile

// FreezeMention is one reference to the frozen entity that was cleared.
type FreezeMention = uistate.FreezeMention

type FreezeRecord = uistate.FreezeRecord

type FreezeBook = uistate.FreezeBook

// FreezeBookVersion is written into the file so that a later version of the
// program can tell what it is reading.
const FreezeBookVersion = 1

// DependenciesFileName is the record inside the project's Units folder. The
// name is the user's: the folder is already Projects\<mod>\Units, and this is
// the one file in it that says what the freeze did.
const DependenciesFileName = "dependencies.json"

// ProjectUnitsDir is the project's Units folder, computed without creating
// anything. Reading the record must not make folders: the object window asks
// what is frozen on every frame it draws, and a program that creates its own
// directories just because a window was painted is a program nobody can run
// read-only.
func ProjectUnitsDir(tree *modtree.ModTree, modulePath string) (string, error) {
	dir, err := ProjectDir(ModProjectName(tree, modulePath))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, UnitsDirName), nil
}

// FreezeBookPath is where the record of a mod's frozen units and buildings
// lives, without creating anything.
func FreezeBookPath(tree *modtree.ModTree, modulePath string) (string, error) {
	dir, err := ProjectUnitsDir(tree, modulePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DependenciesFileName), nil
}

// ReadFreezeBook reads the mod's record, treating a missing file as an empty
// one: a mod that has never frozen anything has nothing to undo.
func ReadFreezeBook(tree *modtree.ModTree, modulePath string) (*FreezeBook, error) {
	book := &FreezeBook{Version: FreezeBookVersion, Mod: ModProjectName(tree, modulePath)}
	path, err := FreezeBookPath(tree, modulePath)
	if err != nil {
		return book, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return book, nil
	}
	if err != nil {
		return book, err
	}
	var read FreezeBook
	if err := json.Unmarshal(data, &read); err != nil {
		return book, fmt.Errorf("%s is not readable: %w", path, err)
	}
	read.Mod = book.Mod
	return &read, nil
}

// WriteFreezeBook writes the record back, creating the folder if it is not
// there. The write goes through a temporary file and a rename, because a
// record half written is a mod that cannot be unfrozen.
func WriteFreezeBook(tree *modtree.ModTree, modulePath string, book *FreezeBook) error {
	path, err := FreezeBookPath(tree, modulePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	book.Version = FreezeBookVersion
	data, err := json.MarshalIndent(book, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// FreezeUnitsDir is the project's Units folder, created if it is not there.
func FreezeUnitsDir(tree *modtree.ModTree, modulePath string) (string, error) {
	dir, err := EnsureProjectDirs(tree, modulePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, UnitsDirName), nil
}

// freezeBookFor is the mod's record, read once and kept.
//
// The strip asks on every frame whether the selected object is frozen, and the
// Frozen list asks on every frame what is frozen, so reading the file there
// would be a disk read per frame for a question whose answer changes only when
// a freeze happens. The cache is dropped when the mod changes and after every
// freeze and unfreeze, which are the only things that change it.
func FreezeBookFor(ui *uistate.State) *FreezeBook {
	if ui == nil || ui.ModTree == nil {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.FreezeBook != nil && ui.FreezeBookMod == mod {
		return ui.FreezeBook
	}
	book, err := ReadFreezeBook(ui.ModTree, ui.ModulePath)
	if err != nil {
		// A record that cannot be read is treated as an empty one for
		// drawing, and the error is said when something is actually
		// attempted, which is where the user can act on it.
		book = &FreezeBook{Version: FreezeBookVersion, Mod: mod}
	}
	ui.FreezeBook = book
	ui.FreezeBookMod = mod
	return book
}

// forgetFreezeBook drops the cached record, which is what a freeze, an
// unfreeze and a module change all have to do.
func ForgetFreezeBook(ui *uistate.State) {
	ui.FreezeBook = nil
	ui.FreezeBookMod = ""
}

// EbpsSquadPathFor is the squad file that goes with a unit: the same name in
// the same race, but under sbps rather than ebps, because a squad is its own
// kind of object. Only units have one — a building is never a squad — so a
// building's answer is empty, and so is anything that is not in a race.
func EbpsSquadPathFor(e *modgraph.GraphEntity) string {
	if e == nil || e.Race == "" || e.Kind != modgraph.GraphKindUnit {
		return ""
	}
	return fmt.Sprintf(`Data\attrib\sbps\races\%s\%s.rgd`, e.Race, e.Name)
}

// clearRgdField sets one field of one file to a value and writes the file
// back through the mod's own overlay.
//
// Going through the overlay matters: the file being changed is often one the
// mod inherits from the base game, and writing a changed copy of it into the
// mod's own folder is how the mod's version takes the place of the base one.
// A file the mod owns is simply rewritten.
func ClearRgdField(ui *uistate.State, virtualPath, field, value string) error {
	if ui == nil || ui.ModTree == nil {
		return fmt.Errorf("no mod is loaded")
	}
	data, err := ui.ModTree.ReadFile(virtualPath)
	if err != nil {
		return err
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return err
	}
	entry := RgdEntryAtPath(rf.Root, field)
	if entry == nil {
		return fmt.Errorf("there is no %s in it", field)
	}
	if err := SetRgdString(entry, value); err != nil {
		return err
	}
	out, err := rf.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = ui.ModTree.WriteOverlayFile(virtualPath, out)
	return err
}

// RgdEntryAtPath finds the field a dotted path names, so that a reference can
// be found again from the record that says where it was.
func RgdEntryAtPath(root *rgd.RgdEntry, path string) *rgd.RgdEntry {
	if root == nil || path == "" {
		return nil
	}
	cur := root
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		cur = unitdef.RgdFindChild(cur, seg)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// SetRgdString gives a string field a value. Only the two string types can
// hold a reference, so anything else is refused rather than silently dropped:
// a field that is not a string was never the field the record meant.
func SetRgdString(e *rgd.RgdEntry, value string) error {
	if e == nil {
		return fmt.Errorf("no entry")
	}
	switch e.Type {
	case rgd.RgdTypeString:
		e.StrVal = value
	case rgd.RgdTypeWString:
		e.WStrVal = value
	default:
		return fmt.Errorf("%s is not a text field, so it cannot hold a reference", e.Name)
	}
	return nil
}

const (
	ProjectsDirName = projects.ProjectsDirName
	UnitsDirName    = projects.UnitsDirName
)

func ProgramDataDir() (string, error) { return projects.ProgramDataDir() }

func ProjectsDir() (string, error) { return projects.ProjectsDir() }

func SanitizeFolderName(name string) string { return projects.SanitizeFolderName(name) }

func ProjectDir(modName string) (string, error) { return projects.ProjectDir(modName) }

func ModProjectName(tree *modtree.ModTree, modulePath string) string {
	return projects.ModProjectName(tree, modulePath)
}

func EnsureProjectDirs(tree *modtree.ModTree, modulePath string) (string, error) {
	return projects.EnsureProjectDirs(tree, modulePath)
}

// layoutFreezeButtons draws the freeze or unfreeze button for the selected
// object. A nil selection draws nothing, because there is nothing to act on.
func LayoutFreezeButtons(ui *uistate.State, gtx layout.Context, sel *unitdef.UnitEntry) layout.Dimensions {
	if sel == nil || ui.ModTree == nil {
		return layout.Dimensions{}
	}
	if IsFrozen(ui, sel.Path) {
		return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitUnfreezeTrigger, "Unfreeze…", false)
	}
	// Only a file the mod itself owns can be moved out of it, so a unit that
	// comes from the base game gets no button rather than one that fails.
	return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitFreezeTrigger, "Freeze…", !CanFreeze(ui, sel.Path))
}

// isFrozen reports whether an object has been moved out of the mod.
func IsFrozen(ui *uistate.State, path string) bool {
	if ui == nil || ui.ModTree == nil {
		return false
	}
	return FreezeBookFor(ui).IsFrozen(path)
}

// canFreeze reports whether an object is one this mod owns, which is the only
// kind that can be moved into the project's folder. It is asked on the frame
// the strip is drawn, so it reads the record rather than caching it: the record
// changes when a freeze happens, and a stale answer would offer a button that
// does the wrong thing.
func CanFreeze(ui *uistate.State, path string) bool {
	if ui == nil || ui.ModTree == nil {
		return false
	}
	_, err := ManagedFileDiskPath(ui, path)
	return err == nil
}

// frozenRows is the list of what has been moved out of the mod, read from the
// record rather than from the mod: a frozen unit is not in the mod, so this is
// the only place it is.
func FrozenRows(ui *uistate.State) []appunit.UnitWeaponRow {
	if ui == nil || ui.ModTree == nil {
		return nil
	}
	book := FreezeBookFor(ui)
	out := make([]appunit.UnitWeaponRow, 0, len(book.Frozen))
	for _, r := range book.Frozen {
		out = append(out, appunit.UnitWeaponRow{
			Hardpoint: r.Kind,
			Name:      r.Name,
			Ref:       r.Path,
		})
	}
	return out
}
