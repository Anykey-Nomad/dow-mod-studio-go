package editors

import (
	"dow-mod-studio/internal/app/uistate"
	"fmt"
	"strings"
)

type NewObjKind = uistate.NewObjKind

const (
	// NewObjUnit is a unit: a file under the race's troops folder.
	NewObjUnit NewObjKind = iota
	// NewObjBuilding is a building: a file under the race's structures
	// folder, with the sections a building has rather than a unit's.
	NewObjBuilding
)

// NewObjTemplateNameError is why a name cannot become an object file, said in
// the dialog's own terms rather than as a file system error the user cannot act
// on.
func NewObjTemplateNameError(kind NewObjKind, name string) error {
	noun := kind.NewObjNoun()
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

// closeNewObjDialog puts the dialog away, and the name with it: a name left in
// the field would be offered again as though it were a fresh one.
func CloseNewObjDialog(ui *uistate.State) {
	ui.NewObjDialog = false
	ui.NewObjStatus = ""
	ui.NewObjNameEditor.SetText("")
}

// newObjHint is the example name the empty field shows. It is a real name from
// the game, so it reads as a name rather than as a placeholder.
func NewObjHint(ui *uistate.State) string {
	if ui.NewObjKind == NewObjBuilding {
		return "chaos_hq"
	}
	return "chaos_hound"
}
