package app

import (
	"dow-mod-studio/internal/rgd"
	"gioui.org/layout"
	"image"
	"strings"
)

// actionIcon returns the decoded command icon of an action or motion, or nil
// when the game's vocabulary has none for it.
func ActionIcon(ui *uiState, name string) image.Image {
	file := actionIconFile(name)
	if file == "" || ui.ModTree == nil {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.ActionIcons == nil || ui.ActionIconsMod != mod {
		ui.ActionIcons = make(map[string]iconCacheEntry)
		ui.ActionIconsMod = mod
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if e, ok := ui.ActionIcons[key]; ok {
		return e.Img
	}
	img := ReadIconAt(ui, actionIconCommandsDir+`\`+file)
	ui.ActionIcons[key] = iconCacheEntry{Img: img, Tried: true}
	return img
}

// unitIcon returns the decoded icon of a unit or building, or nil when the mod
// has none.
func UnitIcon(ui *uiState, u unitEntry) image.Image {
	if ui.ModTree == nil || u.Path == "" {
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.UnitIcons == nil || ui.UnitIconsMod != mod {
		ui.UnitIcons = make(map[string]iconCacheEntry)
		ui.UnitIconsMod = mod
	}
	if e, ok := ui.UnitIcons[u.Path]; ok {
		return e.Img
	}
	img := LoadUnitIcon(ui, u)
	ui.UnitIcons[u.Path] = iconCacheEntry{Img: img, Tried: true}
	return img
}

// loadUnitIcon resolves and decodes the icon of one unit, cheapest lookup
// first: the game's naming convention, then the file anywhere in the tree,
// then the name the unit's own .rgd records — reading the unit is the
// expensive part, so it is last.
func LoadUnitIcon(ui *uiState, u unitEntry) image.Image {
	if ui.ModTree == nil {
		return nil
	}
	stem := unitTemplateIconBase(u.Race) + `\` + u.Name + "_icon"
	if img := ReadIconAt(ui, `art\ui\ingame\`+stem); img != nil {
		return img
	}
	if img := ReadIconByName(ui, u.Name+"_icon"); img != nil {
		return img
	}
	data, err := ui.ModTree.ReadFile(u.Path)
	if err != nil {
		return nil
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil
	}
	icon := rgdEntryStr(rgdFindPath(rf.Root, "ui_ext", "ui_info", "icon_name"))
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return nil
	}
	icon = strings.ReplaceAll(icon, "/", `\`)
	if img := ReadIconAt(ui, `art\ui\ingame\`+icon); img != nil {
		return img
	}
	// icon_name carries the race folder in front ("necrons/necron_icons/...")
	// while the file sits directly under the icons folder.
	if i := strings.Index(icon, `\`); i >= 0 {
		if img := ReadIconAt(ui, `art\ui\ingame\`+icon[i+1:]); img != nil {
			return img
		}
	}
	if i := strings.LastIndex(icon, `\`); i >= 0 {
		icon = icon[i+1:]
	}
	return ReadIconByName(ui, icon)
}

// readIconAt looks for the given tree path without an extension, trying the
// two forms an icon ships in.
func ReadIconAt(ui *uiState, virtualPathNoExt string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		data, err := ui.ModTree.ReadFile(virtualPathNoExt + ext)
		if err != nil {
			continue
		}
		if img := decodeIconData(ext, data); img != nil {
			return img
		}
	}
	return nil
}

// readIconByName finds an icon file by its base name anywhere in the tree,
// which covers a mod that filed its icons somewhere of its own.
func ReadIconByName(ui *uiState, stem string) image.Image {
	for _, ext := range []string{".tga", ".rsh"} {
		f := ui.ModTree.FindByName(stem + ext)
		if f == nil {
			continue
		}
		data, err := ui.ModTree.ReadFile(f.FullPath)
		if err != nil {
			continue
		}
		if img := decodeIconData(ext, data); img != nil {
			return img
		}
	}
	return nil
}

// decodeIconData decodes one icon file. Both forms are ordinary images; a file
// that will not decode simply yields no icon rather than an error line in a
// list that is about names.
func decodeIconData(ext string, data []byte) image.Image {
	switch ext {
	case ".tga":
		if img, err := decodeTGA(data); err == nil {
			return img
		}
	case ".rsh":
		if img, _, err := decodeRshImage(data, 0); err == nil {
			return img
		}
	}
	return nil
}

// unitCatalogFor returns the catalog for the open mod, building it the first
// time it is asked for and again when the mod changes. Building walks every
// race folder, so it is kept off the per-frame path and keyed by the mod's name.
//
// It resets the *selection* when the mod changes, because the old indexes and
// the cached unit then mean nothing. It deliberately does not touch whether a
// dropdown is open: the first call happens lazily, on whatever frame first asks
// for the catalog — which is the frame the user clicked the picker — so
// clearing the open state here would shut the list the instant it opened.
func UnitCatalogFor(ui *uiState) *unitCatalog {
	if ui.ModTree == nil {
		ui.UnitCatalog = nil
		ui.UnitCatalogMod = ""
		ui.ModGraph = nil
		ui.ModGraphMod = ""
		// With no mod there is nothing for a running scan to be reading, and
		// a tree that is being closed is the last place to find that out.
		StopUnitNameScan(ui)
		ui.UnitNames = nil
		ui.UnitNamesDone = false
		StopUnitSquadScan(ui)
		ui.UnitSquads = nil
		ui.UnitSquadsDone = false
		ForgetFreezeBook(ui)
		return nil
	}
	mod := ui.ModTree.FileMapName + "|" + ui.ModTree.ModFilePath
	if ui.UnitCatalog != nil && ui.UnitCatalogMod == mod {
		return ui.UnitCatalog
	}
	// A different mod means a different set of files, so the graph of the old
	// one is meaningless as well.
	ui.ModGraph = nil
	ui.ModGraphMod = ""
	ForgetFreezeBook(ui)
	ui.UnitCatalog = buildUnitCatalog(ui.ModTree)
	ui.UnitCatalogMod = mod
	// The catalog is only the file names; the names the lists show come from
	// each unit's own localisation id, which is a .rgd read per unit. That
	// starts here, in the background, so the dropdown is never the thing that
	// waits for it.
	ui.UnitNamesDone = false
	StartUnitNameScan(ui, ui.ModTree, ui.UnitCatalog)
	// The squad index has the same shape of problem and the same answer: the
	// link from a unit to its squads exists only in the squad files, so those
	// are walked in the background too. Measured at 880ms for the 431 squads of
	// the DXP3 tree, which is far too long to spend on the frame the dialog
	// opens.
	ui.UnitSquadsDone = false
	StartUnitSquadScan(ui, ui.ModTree)
	// A different mod means a different set of races and units, so the old
	// indexes and the cached unit are meaningless.
	ui.UnitRaceSel = 0
	ui.UnitSel = 0
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	ui.UnitSelRow = 0
	ApplyPendingUnitPick(ui)
	return ui.UnitCatalog
}

// graphFor is the mod read as a graph: every .rgd of Data\attrib as an entity,
// and every reference a unit or building holds as a link between two of them.
//
// It is built on first use rather than when the module opens, because indexing
// is cheap but reading the six hundred unit and building files to find their
// references is not, and a session that never opens the relationships view
// should never pay for it. The links themselves are read even later, by
// ensureLinks, for the same reason.
func GraphFor(ui *uiState) *modGraph {
	if ui.ModTree == nil {
		ui.ModGraph = nil
		ui.ModGraphMod = ""
		return nil
	}
	mod := ui.ModTree.FileMapName
	if ui.ModGraph == nil || ui.ModGraphMod != mod {
		ui.ModGraph = newModGraph(ui.ModTree)
		ui.ModGraphMod = mod
	}
	return ui.ModGraph
}

// currentUnit is the unit the dialog is showing, or nil when the mod has none.
func CurrentUnit(ui *uiState) *unitEntry {
	return UnitCatalogFor(ui).SelectedUnit(ui.UnitRaceSel, ui.UnitSel)
}

// currentRace is the race the dialog is showing, or nil when there is none.
func CurrentRace(ui *uiState) *raceEntry {
	return UnitCatalogFor(ui).SelectedRace(ui.UnitRaceSel)
}

// currentUnitDetails parses the selected unit once and caches it, so switching
// between the sections does not re-read and re-parse the file each time.
func CurrentUnitDetails(ui *uiState) *unitDetails {
	u := CurrentUnit(ui)
	if u == nil {
		return nil
	}
	if ui.UnitDetails != nil && strings.EqualFold(ui.UnitDetailsPath, u.Path) {
		return ui.UnitDetails
	}
	d := loadUnitDetails(ui.ModTree, *u)
	ui.UnitDetails = d
	ui.UnitDetailsPath = u.Path
	// A different unit means different fields, so the pointer targets of the
	// previous unit's localised rows are dead weight. Dropping them here keeps
	// the map the size of one unit's rows rather than one per unit ever opened.
	ui.LocaleEditTags = nil
	// A different unit means the editor's field is not in this file any more,
	// and any change that had not been written is the previous unit's. The
	// unsaved state is deliberately not carried over: unitInfoDirty says the
	// unit on screen has been changed, and it is asking about a different one.
	ui.UnitInfoDirty = false
	ui.UnitValError = ""
	ui.UnitValBound = ""
	ui.UnitValEditor.SetText("")
	ui.UnitInfoSel = 0
	// A different unit means the selected row of the populated columns points
	// at nothing, so the selection starts over.
	ui.UnitSelRow = 0
	// The columns are scrolled to the top for a new unit: the previous unit's
	// offset is meaningless against a different list, and keeping it would drop
	// the reader into the middle of the fields with no way to know they had
	// scrolled at all.
	ui.UnitInfoList = layout.List{}
	ui.UnitSquadList = layout.List{}
	// A different unit is a different model, so the viewport's viewer and the
	// textures it loaded go with the previous one. The next load only happens
	// if the Viewport section is looked at again, which is what keeps browsing
	// units from parsing five megabytes of geometry per click.
	ResetUnitModel(ui)
	return d
}
