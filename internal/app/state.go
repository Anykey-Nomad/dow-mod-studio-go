package app

import (
	"dow-mod-studio/internal/app/render"
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
		DarkTheme:        ui.DarkTheme,
		GPUAPI:           render.NormalizeGPUAPI(ui.GpuAPI),
		TextFormats:      formatTextFormats(ui.TextEditorFormats),
		GamePath:         ui.GamePath,
		ModulePath:       ui.ModulePath,
		RecentFolders:    append([]string(nil), ui.RecentFolders...),
		RecentModules:    append([]string(nil), ui.RecentModules...),
		Expanded:         make(map[string]bool, len(ui.Expanded)),
		SelectedTreePath: ui.SelectedTreePath,
		SelectedPath:     ui.SelectedPath,
		Locale:           ui.Locale,
		ActiveTab:        ui.TabSel,
		ObjectSection:    ui.UnitSection,
	}
	// The Object window's own pickers, again by name. The catalog is read
	// directly rather than through the accessor, because asking for the
	// selected unit would build the whole catalog on a background save, and a
	// save is not the moment to start reading six hundred files.
	if ui.UnitCatalog != nil {
		if r := ui.UnitCatalog.SelectedRace(ui.UnitRaceSel); r != nil {
			state.ObjectRace = r.Name
		}
		if u := ui.UnitCatalog.SelectedUnit(ui.UnitRaceSel, ui.UnitSel); u != nil {
			state.ObjectUnit = u.Path
		}
	}
	for path, expanded := range ui.Expanded {
		state.Expanded[path] = expanded
	}
	for _, tab := range ui.OpenTabs {
		// The object tab has no file behind it, so there is no path to
		// remember; it is reopened from the menu bar instead.
		if tab == nil || unitTabHasNoFile(tab) {
			continue
		}
		// A .ucs belongs to the game and sits outside the mod tree, so the
		// session loader would skip it on the way back in. Saving the path
		// would only put a dead entry in the settings file.
		if tab.TextLocale {
			continue
		}
		state.Tabs = append(state.Tabs, persistedTab{Path: tab.Path, Kind: tab.Kind, RGDSelected: tab.RgdSelected})
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	ui.LastStateSave = time.Now()
	ui.StateDirty = false
	return nil
}

func MarkStateDirty(ui *uiState) {
	ui.StateDirty = true
}

func MaybeSaveState(ui *uiState) {
	if !ui.StateDirty || time.Since(ui.LastStateSave) < 2*time.Second {
		return
	}
	if err := savePersistedState(ui); err != nil {
		log.Printf("save state: %v", err)
	}
}

func ApplyPersistedSettings(ui *uiState, state persistedState) {
	ui.DarkTheme = state.DarkTheme
	ui.GpuAPI = render.NormalizeGPUAPI(state.GPUAPI)
	// A state file from before the setting existed has no list, and so does
	// one whose field was cleared: both mean the built-in default, which is
	// what parseTextFormats answers for an empty string.
	ui.TextEditorFormats = parseTextFormats(state.TextFormats)
	ui.GamePath = state.GamePath
	ui.RecentFolders = append([]string(nil), state.RecentFolders...)
	ui.RecentModules = append([]string(nil), state.RecentModules...)
	// The language is restored before the table is built; refreshLocales is
	// what rediscovers the installed languages and would otherwise fall back
	// to English on an empty setting, so the default is filled in here.
	ui.Locale = strings.TrimSpace(state.Locale)
	if ui.Locale == "" {
		ui.Locale = localeDefault
	}
	applyMaterialTheme(ui.Th, ui.DarkTheme)
	RefreshLocales(ui)
	MarkStateDirty(ui)
}

func RememberRecentModule(ui *uiState, modulePath string) {
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" {
		return
	}
	if abs, err := filepath.Abs(modulePath); err == nil {
		modulePath = abs
	}
	key := strings.ToLower(filepath.Clean(modulePath))
	out := ui.RecentModules[:0]
	for _, old := range ui.RecentModules {
		if strings.ToLower(filepath.Clean(old)) != key {
			out = append(out, old)
		}
	}
	ui.RecentModules = append([]string{modulePath}, out...)
	if len(ui.RecentModules) > 8 {
		ui.RecentModules = ui.RecentModules[:8]
	}
	MarkStateDirty(ui)
}

func RememberRecentFolder(ui *uiState, folder string) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return
	}
	if abs, err := filepath.Abs(folder); err == nil {
		folder = abs
	}
	key := strings.ToLower(filepath.Clean(folder))
	out := ui.RecentFolders[:0]
	for _, old := range ui.RecentFolders {
		if strings.ToLower(filepath.Clean(old)) != key {
			out = append(out, old)
		}
	}
	ui.RecentFolders = append([]string{folder}, out...)
	if len(ui.RecentFolders) > 10 {
		ui.RecentFolders = ui.RecentFolders[:10]
	}
	MarkStateDirty(ui)
}

func RestorePersistedState(ui *uiState, state persistedState) bool {
	if strings.TrimSpace(state.ModulePath) == "" {
		return false
	}
	if _, err := os.Stat(state.ModulePath); err != nil {
		return false
	}
	LoadModule(ui, state.ModulePath)
	if ui.ModTree == nil || !samePath(ui.ModulePath, state.ModulePath) {
		return false
	}
	if len(state.Expanded) > 0 {
		ui.Expanded = make(map[string]bool, len(state.Expanded))
		for path, expanded := range state.Expanded {
			ui.Expanded[path] = expanded
		}
	}
	ui.SelectedTreePath = state.SelectedTreePath
	if ui.SelectedTreePath == "" {
		ui.SelectedTreePath = "Data"
	}
	ui.SelectedPath = state.SelectedPath
	// The Object window's pickers are restored by name once the catalog
	// exists, which is the first frame something asks it for. The section is
	// an index into a fixed list, so it can be put back straight away.
	ui.UnitPickRace = state.ObjectRace
	ui.UnitPickPath = state.ObjectUnit
	if state.ObjectSection > 0 && state.ObjectSection < len(unitSections) {
		ui.UnitSection = state.ObjectSection
	}
	for _, savedTab := range state.Tabs {
		if ui.ModTree.FindFile(savedTab.Path) == nil {
			continue
		}
		before := len(ui.OpenTabs)
		RestoreSavedTab(ui, savedTab)
		if len(ui.OpenTabs) > before && savedTab.RGDSelected != "" {
			ui.OpenTabs[len(ui.OpenTabs)-1].RgdSelected = savedTab.RGDSelected
			RebuildRgdMidNodesFor(ui, ui.OpenTabs[len(ui.OpenTabs)-1])
		}
	}
	ui.TabSel = state.ActiveTab
	EnsureTabClicks(ui)
	MarkStateDirty(ui)
	return true
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func RestoreSavedTab(ui *uiState, saved persistedTab) {
	switch strings.ToLower(saved.Kind) {
	case "rgd":
		OpenRgd(ui, saved.Path)
	case "fda":
		OpenFda(ui, saved.Path)
	case "wav":
		OpenWav(ui, saved.Path)
	case "image":
		OpenImage(ui, saved.Path)
	case "whm":
		// A model tab has to go back through the model viewer, otherwise the
		// 3D preview is lost on every restart and the file opens as text.
		OpenWhm(ui, saved.Path)
	case "sgb":
		OpenSgb(ui, saved.Path)
	default:
		OpenTextFile(ui, saved.Path)
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
