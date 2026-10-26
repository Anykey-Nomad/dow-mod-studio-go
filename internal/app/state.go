package app

import (
	"dow-mod-studio/internal/cliphist"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type persistedTab struct {
	Path        string `json:"path"`
	Kind        string `json:"kind,omitempty"`
	RGDSelected string `json:"rgdSelected,omitempty"`
}

type persistedState struct {
	Version   int    `json:"version"`
	DarkTheme bool   `json:"darkTheme"`
	GPUAPI    string `json:"gpuAPI"`
	// TextFormats is the list of file formats the built-in text editor
	// opens, as the reader typed it: ".lua, .txt, .mydsl". An empty value
	// means the built-in default, which is why it is written only when it
	// has actually been changed.
	TextFormats      string          `json:"textFormats,omitempty"`
	GamePath         string          `json:"gamePath,omitempty"`
	ModulePath       string          `json:"modulePath,omitempty"`
	RecentFolders    []string        `json:"recentFolders,omitempty"`
	RecentModules    []string        `json:"recentModules,omitempty"`
	Expanded         map[string]bool `json:"expanded,omitempty"`
	SelectedTreePath string          `json:"selectedTreePath,omitempty"`
	SelectedPath     string          `json:"selectedPath,omitempty"`
	// Locale is the language the "$id" values are resolved in. It is a
	// setting rather than per-module state, so it is kept with the theme: the
	// same reader always shows the same text, whichever module is open.
	Locale    string         `json:"locale,omitempty"`
	ActiveTab int            `json:"activeTab,omitempty"`
	Tabs      []persistedTab `json:"tabs,omitempty"`
	// ObjectRace, ObjectUnit and ObjectSection are the Object window's own
	// pickers as they were when the session ended. The race and the unit are
	// named rather than numbered, because the catalog is rebuilt from whatever
	// the mod holds on every open: an index saved in one session would point
	// at a different unit in the next.
	ObjectRace    string `json:"objectRace,omitempty"`
	ObjectUnit    string `json:"objectUnit,omitempty"`
	ObjectSection int    `json:"objectSection,omitempty"`
}

func stateFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "DoW Mod Studio", "state.conf"), nil
}

func loadPersistedState() (persistedState, bool, error) {
	path, err := stateFilePath()
	if err != nil {
		return persistedState{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return persistedState{}, false, nil
	}
	if err != nil {
		return persistedState{}, false, err
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, true, fmt.Errorf("parse %s: %w", path, err)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	return state, true, nil
}

func savePersistedState(ui *uiState) error {
	path, err := stateFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	state := persistedState{
		Version:          1,
		DarkTheme:        ui.darkTheme,
		GPUAPI:           normalizeGPUAPI(ui.gpuAPI),
		TextFormats:      formatTextFormats(ui.textEditorFormats),
		GamePath:         ui.gamePath,
		ModulePath:       ui.modulePath,
		RecentFolders:    append([]string(nil), ui.recentFolders...),
		RecentModules:    append([]string(nil), ui.recentModules...),
		Expanded:         make(map[string]bool, len(ui.expanded)),
		SelectedTreePath: ui.selectedTreePath,
		SelectedPath:     ui.selectedPath,
		Locale:           ui.locale,
		ActiveTab:        ui.tabSel,
		ObjectSection:    ui.unitSection,
	}
	// The Object window's own pickers, again by name. The catalog is read
	// directly rather than through the accessor, because asking for the
	// selected unit would build the whole catalog on a background save, and a
	// save is not the moment to start reading six hundred files.
	if ui.unitCatalog != nil {
		if r := ui.unitCatalog.SelectedRace(ui.unitRaceSel); r != nil {
			state.ObjectRace = r.Name
		}
		if u := ui.unitCatalog.SelectedUnit(ui.unitRaceSel, ui.unitSel); u != nil {
			state.ObjectUnit = u.Path
		}
	}
	for path, expanded := range ui.expanded {
		state.Expanded[path] = expanded
	}
	for _, tab := range ui.openTabs {
		// The object tab has no file behind it, so there is no path to
		// remember; it is reopened from the menu bar instead.
		if tab == nil || unitTabHasNoFile(tab) {
			continue
		}
		// A .ucs belongs to the game and sits outside the mod tree, so the
		// session loader would skip it on the way back in. Saving the path
		// would only put a dead entry in the settings file.
		if tab.textLocale {
			continue
		}
		state.Tabs = append(state.Tabs, persistedTab{Path: tab.path, Kind: tab.kind, RGDSelected: tab.rgdSelected})
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	ui.lastStateSave = time.Now()
	ui.stateDirty = false
	return nil
}

func (ui *uiState) markStateDirty() {
	ui.stateDirty = true
}

func (ui *uiState) maybeSaveState() {
	if !ui.stateDirty || time.Since(ui.lastStateSave) < 2*time.Second {
		return
	}
	if err := savePersistedState(ui); err != nil {
		log.Printf("save state: %v", err)
	}
}

func (ui *uiState) applyPersistedSettings(state persistedState) {
	ui.darkTheme = state.DarkTheme
	ui.gpuAPI = normalizeGPUAPI(state.GPUAPI)
	// A state file from before the setting existed has no list, and so does
	// one whose field was cleared: both mean the built-in default, which is
	// what parseTextFormats answers for an empty string.
	ui.textEditorFormats = parseTextFormats(state.TextFormats)
	ui.gamePath = state.GamePath
	ui.recentFolders = append([]string(nil), state.RecentFolders...)
	ui.recentModules = append([]string(nil), state.RecentModules...)
	// The language is restored before the table is built; refreshLocales is
	// what rediscovers the installed languages and would otherwise fall back
	// to English on an empty setting, so the default is filled in here.
	ui.locale = strings.TrimSpace(state.Locale)
	if ui.locale == "" {
		ui.locale = localeDefault
	}
	applyMaterialTheme(ui.th, ui.darkTheme)
	ui.refreshLocales()
	ui.markStateDirty()
}

func (ui *uiState) rememberRecentModule(modulePath string) {
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" {
		return
	}
	if abs, err := filepath.Abs(modulePath); err == nil {
		modulePath = abs
	}
	key := strings.ToLower(filepath.Clean(modulePath))
	out := ui.recentModules[:0]
	for _, old := range ui.recentModules {
		if strings.ToLower(filepath.Clean(old)) != key {
			out = append(out, old)
		}
	}
	ui.recentModules = append([]string{modulePath}, out...)
	if len(ui.recentModules) > 8 {
		ui.recentModules = ui.recentModules[:8]
	}
	ui.markStateDirty()
}

func (ui *uiState) rememberRecentFolder(folder string) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return
	}
	if abs, err := filepath.Abs(folder); err == nil {
		folder = abs
	}
	key := strings.ToLower(filepath.Clean(folder))
	out := ui.recentFolders[:0]
	for _, old := range ui.recentFolders {
		if strings.ToLower(filepath.Clean(old)) != key {
			out = append(out, old)
		}
	}
	ui.recentFolders = append([]string{folder}, out...)
	if len(ui.recentFolders) > 10 {
		ui.recentFolders = ui.recentFolders[:10]
	}
	ui.markStateDirty()
}

func (ui *uiState) restorePersistedState(state persistedState) bool {
	if strings.TrimSpace(state.ModulePath) == "" {
		return false
	}
	if _, err := os.Stat(state.ModulePath); err != nil {
		return false
	}
	ui.loadModule(state.ModulePath)
	if ui.modTree == nil || !samePath(ui.modulePath, state.ModulePath) {
		return false
	}
	if len(state.Expanded) > 0 {
		ui.expanded = make(map[string]bool, len(state.Expanded))
		for path, expanded := range state.Expanded {
			ui.expanded[path] = expanded
		}
	}
	ui.selectedTreePath = state.SelectedTreePath
	if ui.selectedTreePath == "" {
		ui.selectedTreePath = "Data"
	}
	ui.selectedPath = state.SelectedPath
	// The Object window's pickers are restored by name once the catalog
	// exists, which is the first frame something asks it for. The section is
	// an index into a fixed list, so it can be put back straight away.
	ui.unitPickRace = state.ObjectRace
	ui.unitPickPath = state.ObjectUnit
	if state.ObjectSection > 0 && state.ObjectSection < len(unitSections) {
		ui.unitSection = state.ObjectSection
	}
	for _, savedTab := range state.Tabs {
		if ui.modTree.FindFile(savedTab.Path) == nil {
			continue
		}
		before := len(ui.openTabs)
		ui.restoreSavedTab(savedTab)
		if len(ui.openTabs) > before && savedTab.RGDSelected != "" {
			ui.openTabs[len(ui.openTabs)-1].rgdSelected = savedTab.RGDSelected
			ui.rebuildRgdMidNodesFor(ui.openTabs[len(ui.openTabs)-1])
		}
	}
	ui.tabSel = state.ActiveTab
	ui.ensureTabClicks()
	ui.markStateDirty()
	return true
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func (ui *uiState) restoreSavedTab(saved persistedTab) {
	switch strings.ToLower(saved.Kind) {
	case "rgd":
		ui.openRgd(saved.Path)
	case "fda":
		ui.openFda(saved.Path)
	case "wav":
		ui.openWav(saved.Path)
	case "image":
		ui.openImage(saved.Path)
	case "whm":
		// A model tab has to go back through the model viewer, otherwise the
		// 3D preview is lost on every restart and the file opens as text.
		ui.openWhm(saved.Path)
	case "sgb":
		ui.openSgb(saved.Path)
	default:
		ui.openTextFile(saved.Path)
	}
}

// ── clipboard history (internal/cliphist) ────────────────────────────────────
//
// What the copy buffers of the timeline, the keyframe tracks and the action
// list remember lives in internal/cliphist; the editors keep the names they
// paste and pick with.

type (
	clipKind      = cliphist.ClipKind
	clipEntry     = cliphist.ClipEntry
	clipHistory   = cliphist.ClipHistory
	boneKeyClip   = cliphist.BoneKeyClip
	wheEventClip  = cliphist.WheEventClip
	wheActionClip = cliphist.WheActionClip
)

const (
	clipEvents       = cliphist.ClipEvents
	clipBoneKeys     = cliphist.ClipBoneKeys
	clipImportKeys   = cliphist.ClipImportKeys
	clipActions      = cliphist.ClipActions
	clipHistoryLimit = cliphist.ClipHistoryLimit
)
