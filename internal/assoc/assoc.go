package assoc

import ()

// File associations. The studio can be the default opener of the formats it
// works with: a .module picks the mod, and .fda, .whm, .whe and .sga open
// their viewers. Each format carries its own ProgID and is toggled on its own
// from the Settings dialog; registration is per user, never machine wide.

// assocType is one file format that can be associated with the studio. The
// registry names are Windows's, but the table itself is shared: the Settings
// dialog lists every row whatever platform it is drawn on.
type AssocType struct {
	Ext    string // ".fda"
	ProgID string // the Windows ProgID registered for the format
	Label  string // what the settings dialog calls it
	Desc   string // the registry's own type description
}

// moduleAssoc is the .module format. Its ProgID and description are the ones
// older builds registered, so they stay as they are.
var ModuleAssoc = AssocType{
	Ext:    ".module",
	ProgID: "DowModStudio.Module",
	Label:  "Module",
	Desc:   "Dawn of War Module",
}

// assocTypes are the formats the Settings dialog offers. .module is registered
// from the File menu, so it has a ProgID but no row here.
var AssocTypes = []AssocType{
	{Ext: ".fda", ProgID: "DowModStudio.Fda", Label: "FDA audio", Desc: "DoW Mod Studio FDA audio"},
	{Ext: ".whm", ProgID: "DowModStudio.Whm", Label: "WHM model", Desc: "DoW Mod Studio WHM model"},
	{Ext: ".whe", ProgID: "DowModStudio.Whe", Label: "WHE events", Desc: "DoW Mod Studio WHE events"},
	{Ext: ".sga", ProgID: "DowModStudio.Sga", Label: "SGA archive", Desc: "DoW Mod Studio SGA archive"},
}

// isModuleAssociated reports whether double-clicking a .module opens this exe.
// It is the File menu's own view of the shared registry machinery.
func IsModuleAssociated() (bool, string, error) { return IsExtAssociated(ModuleAssoc) }

// registerModuleAssociation registers the .module format for the current user.
func RegisterModuleAssociation() error { return RegisterExtAssociation(ModuleAssoc) }

// unregisterModuleAssociation removes the .module association when it is ours.
func UnregisterModuleAssociation() error { return UnregisterExtAssociation(ModuleAssoc) }
