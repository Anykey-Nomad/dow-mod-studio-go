package main

import (
	"fmt"
	"log"
	"strings"
)

// File associations. The studio can be the default opener of the formats it
// works with: a .module picks the mod, and .fda, .whm, .whe and .sga open
// their viewers. Each format carries its own ProgID and is toggled on its own
// from the Settings dialog; registration is per user, never machine wide.

// assocType is one file format that can be associated with the studio. The
// registry names are Windows's, but the table itself is shared: the Settings
// dialog lists every row whatever platform it is drawn on.
type assocType struct {
	ext    string // ".fda"
	progID string // the Windows ProgID registered for the format
	label  string // what the settings dialog calls it
	desc   string // the registry's own type description
}

// moduleAssoc is the .module format. Its ProgID and description are the ones
// older builds registered, so they stay as they are.
var moduleAssoc = assocType{
	ext:    ".module",
	progID: "DowModStudio.Module",
	label:  "Module",
	desc:   "Dawn of War Module",
}

// assocTypes are the formats the Settings dialog offers. .module is registered
// from the File menu, so it has a ProgID but no row here.
var assocTypes = []assocType{
	{ext: ".fda", progID: "DowModStudio.Fda", label: "FDA audio", desc: "DoW Mod Studio FDA audio"},
	{ext: ".whm", progID: "DowModStudio.Whm", label: "WHM model", desc: "DoW Mod Studio WHM model"},
	{ext: ".whe", progID: "DowModStudio.Whe", label: "WHE events", desc: "DoW Mod Studio WHE events"},
	{ext: ".sga", progID: "DowModStudio.Sga", label: "SGA archive", desc: "DoW Mod Studio SGA archive"},
}

// isModuleAssociated reports whether double-clicking a .module opens this exe.
// It is the File menu's own view of the shared registry machinery.
func isModuleAssociated() (bool, string, error) { return isExtAssociated(moduleAssoc) }

// registerModuleAssociation registers the .module format for the current user.
func registerModuleAssociation() error { return registerExtAssociation(moduleAssoc) }

// unregisterModuleAssociation removes the .module association when it is ours.
func unregisterModuleAssociation() error { return unregisterExtAssociation(moduleAssoc) }

// refreshAssocStates re-reads which formats the studio is associated with, so
// the Settings buttons can say what a click would do. A query that fails is
// shown as "not associated": the button then offers the safe action.
func (ui *uiState) refreshAssocStates() {
	ui.assocOn = make([]bool, len(assocTypes))
	if !associationsSupported() {
		return
	}
	for i, t := range assocTypes {
		on, _, _ := isExtAssociated(t)
		ui.assocOn[i] = on
	}
}

// toggleAssociation adds or removes one format's association and re-reads the
// row's state, so the button reflects the registry at once rather than the
// next time the dialog is opened.
func (ui *uiState) toggleAssociation(i int) {
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
		log.Printf("association %s: %v", t.ext, err)
		ui.statusText = fmt.Sprintf("%s association failed: %v", t.ext, err)
		ui.refreshAssocStates()
		return
	}
	if on {
		log.Printf("association %s: removed", t.ext)
		ui.statusText = fmt.Sprintf("Removed %s association", t.ext)
	} else {
		log.Printf("association %s: registered for %s", t.ext, getExePath())
		ui.statusText = fmt.Sprintf("Associated %s with %s", t.ext, getExePath())
	}
	ui.refreshAssocStates()
}

// assocTypeFor returns the table row for an extension, ".module" included.
func assocTypeFor(ext string) (assocType, bool) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == moduleAssoc.ext {
		return moduleAssoc, true
	}
	for _, t := range assocTypes {
		if t.ext == ext {
			return t, true
		}
	}
	return assocType{}, false
}
