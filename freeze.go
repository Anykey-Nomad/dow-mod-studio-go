package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Freezing a unit or a building.
//
// A frozen entity is not deleted and it is not disabled: its .rgd files are
// moved out of the mod and into the project's own folder, so the game no
// longer sees them and the mod no longer ships them, while the files
// themselves are still there to look at. That is a different thing from
// deleting, and it is why freezing is reversible.
//
// The hard part is not the files but everything that pointed at them. A
// Necron Gauss Blaster is named by the weapons of four hundred units, and a
// unit is named by the requirement table of whichever building makes it. Move
// the file and every one of those references is a name that resolves to
// nothing. So freezing first finds every reference to the entity, then clears
// them, and writes down exactly what each one said before it did — into
// dependencies.json, next to the mod's other project data. Unfreezing reads
// that file, puts each value back, and moves the .rgd files home.
//
// The order matters and is the whole of the design: nothing is moved and
// nothing is cleared until the record of what to undo has been written.

// freezeFile is one file moved out of the mod.
type freezeFile struct {
	// From is the virtual path the file had in the mod.
	From string `json:"from"`
	// To is its name inside the project's Units folder.
	To string `json:"to"`
}

// freezeMention is one reference to the frozen entity that was cleared.
type freezeMention struct {
	// File is the virtual path of the file that held the reference.
	File string `json:"file"`
	// Field is the dotted path of the field, which is how it is found again.
	Field string `json:"field"`
	// Slot and Role are kept so the record reads as something a person can
	// check rather than as a list of offsets.
	Slot  string `json:"slot"`
	Role  string `json:"role"`
	Value string `json:"value"`
}

// freezeRecord is one frozen entity and everything that was done to it.
type freezeRecord struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Race string `json:"race"`
	// FrozenAt is when, in the project's own terms: the record is read by a
	// person trying to work out what happened to their mod.
	FrozenAt string `json:"frozenAt"`
	// Files are the .rgd files that were moved out, in the order they were
	// moved: the ebps one first, then the sbps one.
	Files []freezeFile `json:"files"`
	// Mentions are the references that were cleared, so they can be put back.
	Mentions []freezeMention `json:"mentions"`
}

// freezeBook is the whole of dependencies.json.
type freezeBook struct {
	Version int            `json:"version"`
	Mod     string         `json:"mod"`
	Frozen  []freezeRecord `json:"frozen"`
}

// freezeBookVersion is written into the file so that a later version of the
// program can tell what it is reading.
const freezeBookVersion = 1

// dependenciesFileName is the record inside the project's Units folder. The
// name is the user's: the folder is already Projects\<mod>\Units, and this is
// the one file in it that says what the freeze did.
const dependenciesFileName = "dependencies.json"

// projectUnitsDir is the project's Units folder, computed without creating
// anything. Reading the record must not make folders: the object window asks
// what is frozen on every frame it draws, and a program that creates its own
// directories just because a window was painted is a program nobody can run
// read-only.
func projectUnitsDir(tree *modtree.ModTree, modulePath string) (string, error) {
	dir, err := projectDir(modProjectName(tree, modulePath))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, unitsDirName), nil
}

// freezeBookPath is where the record of a mod's frozen units and buildings
// lives, without creating anything.
func freezeBookPath(tree *modtree.ModTree, modulePath string) (string, error) {
	dir, err := projectUnitsDir(tree, modulePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dependenciesFileName), nil
}

// readFreezeBook reads the mod's record, treating a missing file as an empty
// one: a mod that has never frozen anything has nothing to undo.
func readFreezeBook(tree *modtree.ModTree, modulePath string) (*freezeBook, error) {
	book := &freezeBook{Version: freezeBookVersion, Mod: modProjectName(tree, modulePath)}
	path, err := freezeBookPath(tree, modulePath)
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
	var read freezeBook
	if err := json.Unmarshal(data, &read); err != nil {
		return book, fmt.Errorf("%s is not readable: %w", path, err)
	}
	read.Mod = book.Mod
	return &read, nil
}

// writeFreezeBook writes the record back, creating the folder if it is not
// there. The write goes through a temporary file and a rename, because a
// record half written is a mod that cannot be unfrozen.
func writeFreezeBook(tree *modtree.ModTree, modulePath string, book *freezeBook) error {
	path, err := freezeBookPath(tree, modulePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	book.Version = freezeBookVersion
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

// record finds the record of a frozen entity.
func (b *freezeBook) record(path string) *freezeRecord {
	if b == nil {
		return nil
	}
	for i := range b.Frozen {
		if strings.EqualFold(b.Frozen[i].Path, path) {
			return &b.Frozen[i]
		}
	}
	return nil
}

// isFrozen reports whether an entity has been frozen.
func (b *freezeBook) isFrozen(path string) bool { return b.record(path) != nil }

// frozenPaths lists every entity that has been frozen.
func (b *freezeBook) frozenPaths() []string {
	if b == nil {
		return nil
	}
	out := make([]string, 0, len(b.Frozen))
	for _, r := range b.Frozen {
		out = append(out, r.Path)
	}
	return out
}

// ── freezing ────────────────────────────────────────────────────────────────

// freezeEntity moves a unit or a building out of the mod.
//
// The order is fixed and each step depends on the one before: the references
// are found and written down, the record is saved, and only then is anything
// changed. A failure before the save leaves the mod untouched, which is the
// only safe way for this to fail.
func (ui *uiState) freezeEntity(virtualPath string) (*freezeRecord, error) {
	if ui == nil || ui.modTree == nil {
		return nil, fmt.Errorf("no mod is loaded")
	}
	graph := ui.graphFor()
	e := graph.Entity(virtualPath)
	if e == nil {
		return nil, fmt.Errorf("%s is not in the mod", virtualPath)
	}
	if e.Kind != graphKindUnit && e.Kind != graphKindStructure {
		return nil, fmt.Errorf("only a unit or a building can be frozen, and %s is a %s", e.Name, e.Kind.Label())
	}
	book, err := readFreezeBook(ui.modTree, ui.modulePath)
	if err != nil {
		return nil, err
	}
	if book.isFrozen(e.Path) {
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
	if _, err := ui.managedFileDiskPath(e.Path); err != nil {
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
	if squadPath != "" && ui.modTree.FindFile(squadPath) != nil {
		record.Files = append(record.Files, freezeFile{From: e.Path, To: e.Name + ".rgd"})
		record.Files = append(record.Files, freezeFile{From: squadPath, To: e.Name + "_squad.rgd"})
	} else {
		record.Files = append(record.Files, freezeFile{From: e.Path, To: e.Name + ".rgd"})
	}

	// Saved before anything is touched: a record that does not exist cannot
	// undo anything.
	book.Frozen = append(book.Frozen, record)
	if err := writeFreezeBook(ui.modTree, ui.modulePath, book); err != nil {
		return nil, fmt.Errorf("the record of what freezing changes could not be written, so nothing was changed: %w", err)
	}

	// Now the mentions. Each is one field of one file, cleared one file at a
	// time; a file that cannot be rewritten stops the freeze there, with the
	// record already written, so the state is still recoverable.
	for _, m := range record.Mentions {
		if err := ui.clearRgdField(m.File, m.Field, ""); err != nil {
			return nil, fmt.Errorf("%s could not be cleared from %s: %w", m.Value, m.File, err)
		}
	}

	// And the files themselves, moved into the project's Units folder.
	unitsDir, err := freezeUnitsDir(ui.modTree, ui.modulePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(unitsDir, 0755); err != nil {
		return nil, err
	}
	for _, f := range record.Files {
		disk, err := ui.managedFileDiskPath(f.From)
		if err != nil {
			return nil, fmt.Errorf("%s could not be moved: %v", f.From, err)
		}
		if err := moveFileKeeping(disk, filepath.Join(unitsDir, f.To)); err != nil {
			return nil, fmt.Errorf("%s could not be moved into the project folder: %w", f.From, err)
		}
		ui.modTree.RemoveFile(f.From)
	}

	// The indexes are snapshots: a frozen unit is not in the mod any more, so
	// the catalogue, the graph and the cached detail all have to be read
	// again.
	ui.unitCatalog = nil
	ui.modGraph = nil
	ui.unitDetails = nil
	ui.unitDetailsPath = ""
	ui.forgetFreezeBook()
	ui.markStateDirty()
	return &record, nil
}

// unfreezeEntity puts a frozen unit or building back the way it was: every
// reference it had is given its value again, and its files are moved home.
//
// The references go back before the files do, so that there is no moment in
// which the files are visible to the game but nothing names them. If a file
// cannot be rewritten the freeze is left in place, because a half-restored
// unit is worse than a frozen one.
func (ui *uiState) unfreezeEntity(virtualPath string) (*freezeRecord, error) {
	if ui == nil || ui.modTree == nil {
		return nil, fmt.Errorf("no mod is loaded")
	}
	book, err := readFreezeBook(ui.modTree, ui.modulePath)
	if err != nil {
		return nil, err
	}
	record := book.record(virtualPath)
	if record == nil {
		return nil, fmt.Errorf("%s is not frozen", filepath.Base(virtualPath))
	}

	for _, m := range record.Mentions {
		if err := ui.clearRgdField(m.File, m.Field, m.Value); err != nil {
			return nil, fmt.Errorf("the reference to %s could not be put back into %s: %w", record.Name, m.File, err)
		}
	}

	unitsDir, err := freezeUnitsDir(ui.modTree, ui.modulePath)
	if err != nil {
		return nil, err
	}
	for _, f := range record.Files {
		saved := filepath.Join(unitsDir, f.To)
		data, err := os.ReadFile(saved)
		if err != nil {
			return nil, fmt.Errorf("the saved %s could not be read, so nothing was moved back: %w", f.To, err)
		}
		if _, err := ui.modTree.WriteOverlayFile(f.From, data); err != nil {
			return nil, fmt.Errorf("%s could not be put back: %w", f.From, err)
		}
		if err := os.Remove(saved); err != nil {
			return nil, fmt.Errorf("%s was put back but its saved copy could not be removed: %w", f.To, err)
		}
	}

	kept := make([]freezeRecord, 0, len(book.Frozen))
	for _, r := range book.Frozen {
		if !strings.EqualFold(r.Path, record.Path) {
			kept = append(kept, r)
		}
	}
	book.Frozen = kept
	if err := writeFreezeBook(ui.modTree, ui.modulePath, book); err != nil {
		return nil, err
	}

	ui.unitCatalog = nil
	ui.modGraph = nil
	ui.unitDetails = nil
	ui.unitDetailsPath = ""
	ui.forgetFreezeBook()
	ui.markStateDirty()
	return record, nil
}

// freezeUnitsDir is the project's Units folder, created if it is not there.
func freezeUnitsDir(tree *modtree.ModTree, modulePath string) (string, error) {
	dir, err := ensureProjectDirs(tree, modulePath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, unitsDirName), nil
}

// freezeBookFor is the mod's record, read once and kept.
//
// The strip asks on every frame whether the selected object is frozen, and the
// Frozen list asks on every frame what is frozen, so reading the file there
// would be a disk read per frame for a question whose answer changes only when
// a freeze happens. The cache is dropped when the mod changes and after every
// freeze and unfreeze, which are the only things that change it.
func (ui *uiState) freezeBookFor() *freezeBook {
	if ui == nil || ui.modTree == nil {
		return nil
	}
	mod := ui.modTree.FileMapName
	if ui.freezeBook != nil && ui.freezeBookMod == mod {
		return ui.freezeBook
	}
	book, err := readFreezeBook(ui.modTree, ui.modulePath)
	if err != nil {
		// A record that cannot be read is treated as an empty one for
		// drawing, and the error is said when something is actually
		// attempted, which is where the user can act on it.
		book = &freezeBook{Version: freezeBookVersion, Mod: mod}
	}
	ui.freezeBook = book
	ui.freezeBookMod = mod
	return book
}

// forgetFreezeBook drops the cached record, which is what a freeze, an
// unfreeze and a module change all have to do.
func (ui *uiState) forgetFreezeBook() {
	ui.freezeBook = nil
	ui.freezeBookMod = ""
}

// ebpsSquadPathFor is the squad file that goes with a unit: the same name in
// the same race, but under sbps rather than ebps, because a squad is its own
// kind of object. Only units have one — a building is never a squad — so a
// building's answer is empty, and so is anything that is not in a race.
func ebpsSquadPathFor(e *graphEntity) string {
	if e == nil || e.Race == "" || e.Kind != graphKindUnit {
		return ""
	}
	return fmt.Sprintf(`Data\attrib\sbps\races\%s\%s.rgd`, e.Race, e.Name)
}

// ── clearing and restoring a field ──────────────────────────────────────────

// clearRgdField sets one field of one file to a value and writes the file
// back through the mod's own overlay.
//
// Going through the overlay matters: the file being changed is often one the
// mod inherits from the base game, and writing a changed copy of it into the
// mod's own folder is how the mod's version takes the place of the base one.
// A file the mod owns is simply rewritten.
func (ui *uiState) clearRgdField(virtualPath, field, value string) error {
	if ui == nil || ui.modTree == nil {
		return fmt.Errorf("no mod is loaded")
	}
	data, err := ui.modTree.ReadFile(virtualPath)
	if err != nil {
		return err
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return err
	}
	entry := rgdEntryAtPath(rf.Root, field)
	if entry == nil {
		return fmt.Errorf("there is no %s in it", field)
	}
	if err := setRgdString(entry, value); err != nil {
		return err
	}
	out, err := rf.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = ui.modTree.WriteOverlayFile(virtualPath, out)
	return err
}

// rgdEntryAtPath finds the field a dotted path names, so that a reference can
// be found again from the record that says where it was.
func rgdEntryAtPath(root *rgd.RgdEntry, path string) *rgd.RgdEntry {
	if root == nil || path == "" {
		return nil
	}
	cur := root
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		cur = rgdFindChild(cur, seg)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// setRgdString gives a string field a value. Only the two string types can
// hold a reference, so anything else is refused rather than silently dropped:
// a field that is not a string was never the field the record meant.
func setRgdString(e *rgd.RgdEntry, value string) error {
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
