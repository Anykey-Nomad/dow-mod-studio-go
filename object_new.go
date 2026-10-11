package main

import (
	"dow-mod-studio/internal/rgd"
	"fmt"
	"strings"
)

// Writing a new object into the mod.
//
// The templates say what a unit or a building file contains; this says where it
// goes and what happens afterwards. The file is written through the mod's own
// overlay, the same way saving an edited RGD is, so it lands in the current
// module folder and the mod tree knows about it straight away — which is what
// makes the new object appear in the object window without a reload.

// newObjKind is what the dialog is being opened to create.
type newObjKind int

const (
	// newObjUnit is a unit: a file under the race's troops folder.
	newObjUnit newObjKind = iota
	// newObjBuilding is a building: a file under the race's structures
	// folder, with the sections a building has rather than a unit's.
	newObjBuilding
)

// newObjNoun is what the dialog calls the thing it is about to write.
func (k newObjKind) newObjNoun() string {
	if k == newObjBuilding {
		return "building"
	}
	return "unit"
}

// newObjPath is where an object of this kind and name would be written.
func (k newObjKind) newObjPath(race, name string) string {
	if k == newObjBuilding {
		return structureTemplateName{Race: race, Name: name}.structureTemplateVirtualPath()
	}
	return unitTemplateName{Race: race, Name: name}.unitTemplateVirtualPath()
}

// newObjTemplateNameError is why a name cannot become an object file, said in
// the dialog's own terms rather than as a file system error the user cannot act
// on.
func newObjTemplateNameError(kind newObjKind, name string) error {
	noun := kind.newObjNoun()
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return fmt.Errorf("the %s needs a name", noun)
	case trimmed != name:
		return fmt.Errorf("the name has a space at either end of it")
	case strings.ContainsAny(name, `\/:*?"<>|`):
		return fmt.Errorf("a %s name cannot hold \\ / : * ? \" < > or |", noun)
	case strings.Contains(name, "."):
		return fmt.Errorf("write the name without .rgd — the extension is added for you")
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("a %s name cannot hold control characters", noun)
		}
	}
	return nil
}

// createNewObject writes a new unit or building of the given race from the
// template and returns the virtual path it wrote.
//
// It refuses rather than guesses: a name the file system would not take, and a
// name that already names a file of the mod or of the game, are both errors the
// dialog shows, because an object that overwrote somebody's would be worse than
// one that was not created.
func (ui *uiState) createNewObject(kind newObjKind, race, name string) (string, error) {
	if ui.modTree == nil {
		return "", fmt.Errorf("no module is loaded")
	}
	if err := newObjTemplateNameError(kind, name); err != nil {
		return "", err
	}
	if race == "" {
		return "", fmt.Errorf("no race is selected, so there is no folder to write the %s into", kind.newObjNoun())
	}
	virtual := kind.newObjPath(race, name)
	if ui.modTree.FindFile(virtual) != nil {
		return "", fmt.Errorf("%s already exists", name)
	}
	var file *rgd.RgdFile
	if kind == newObjBuilding {
		file = newStructureRgd(structureTemplateName{Race: race, Name: name})
	} else {
		file = newUnitRgd(unitTemplateName{Race: race, Name: name})
	}
	data, err := file.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("the %s could not be written: %v", kind.newObjNoun(), err)
	}
	if _, err := ui.modTree.WriteOverlayFile(virtual, data); err != nil {
		return "", fmt.Errorf("the %s could not be saved: %v", kind.newObjNoun(), err)
	}
	// The catalogue is a snapshot of the tree, so it is thrown away and read
	// again: that is what puts the new object in the list and in the pickers.
	ui.unitCatalog = nil
	ui.unitDetails = nil
	ui.unitDetailsPath = ""
	ui.selectNewObject(race, virtual)
	ui.statusText = fmt.Sprintf("Created %s", name)
	ui.markStateDirty()
	return virtual, nil
}

// selectNewObject points the object window at the file that was just written,
// so the list opens on it rather than where it was before.
func (ui *uiState) selectNewObject(race, virtual string) {
	cat := ui.unitCatalogFor()
	if cat == nil {
		return
	}
	for ri, r := range cat.races {
		if !strings.EqualFold(r.name, race) {
			continue
		}
		ui.unitRaceSel = ri
		ui.unitSel = 0
		// unitsOf is the list's own order: the race's troops, then its
		// structures. A new building is picked out of the second half, and a
		// new unit out of the first, without either case being written twice.
		for i, e := range r.unitsOf() {
			if strings.EqualFold(e.path, virtual) {
				ui.unitSel = i
				return
			}
		}
		return
	}
}
