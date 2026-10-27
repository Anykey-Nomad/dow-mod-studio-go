package shell

import (
	"dow-mod-studio/internal/app/editors"
	"dow-mod-studio/internal/app/fileops"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uistate"
	appunit "dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/cliphist"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type PersistedTab struct {
	Path        string `json:"path"`
	Kind        string `json:"kind,omitempty"`
	RGDSelected string `json:"rgdSelected,omitempty"`
}

type PersistedState struct {
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
	Tabs      []PersistedTab `json:"tabs,omitempty"`
	// ObjectRace, ObjectUnit and ObjectSection are the Object window's own
	// pickers as they were when the session ended. The race and the unit are
	// named rather than numbered, because the catalog is rebuilt from whatever
	// the mod holds on every open: an index saved in one session would point
	// at a different unit in the next.
	ObjectRace    string `json:"objectRace,omitempty"`
	ObjectUnit    string `json:"objectUnit,omitempty"`
	ObjectSection int    `json:"objectSection,omitempty"`
}

func StateFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "DoW Mod Studio", "state.conf"), nil
}

func LoadPersistedState() (PersistedState, bool, error) {
	path, err := StateFilePath()
	if err != nil {
		return PersistedState{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return PersistedState{}, false, nil
	}
	if err != nil {
		return PersistedState{}, false, err
	}
	var state PersistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return PersistedState{}, true, fmt.Errorf("parse %s: %w", path, err)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	return state, true, nil
}

func SavePersistedState(ui *UiState) error {
	path, err := StateFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	state := PersistedState{
		Version:          1,
		DarkTheme:        ui.DarkTheme,
		GPUAPI:           render.NormalizeGPUAPI(ui.GpuAPI),
		TextFormats:      fileops.FormatTextFormats(ui.TextEditorFormats),
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
		if tab == nil || appunit.UnitTabHasNoFile(tab) {
			continue
		}
		// A .ucs belongs to the game and sits outside the mod tree, so the
		// session loader would skip it on the way back in. Saving the path
		// would only put a dead entry in the settings file.
		if tab.TextLocale {
			continue
		}
		state.Tabs = append(state.Tabs, PersistedTab{Path: tab.Path, Kind: tab.Kind, RGDSelected: tab.RgdSelected})
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

func MarkStateDirty(ui *UiState) {
	ui.StateDirty = true
}

func MaybeSaveState(ui *UiState) {
	if !ui.StateDirty || time.Since(ui.LastStateSave) < 2*time.Second {
		return
	}
	if err := SavePersistedState(ui); err != nil {
		log.Printf("save state: %v", err)
	}
}

func ApplyPersistedSettings(ui *UiState, state PersistedState) {
	ui.DarkTheme = state.DarkTheme
	ui.GpuAPI = render.NormalizeGPUAPI(state.GPUAPI)
	// A state file from before the setting existed has no list, and so does
	// one whose field was cleared: both mean the built-in default, which is
	// what fileops.ParseTextFormats answers for an empty string.
	ui.TextEditorFormats = fileops.ParseTextFormats(state.TextFormats)
	ui.GamePath = state.GamePath
	ui.RecentFolders = append([]string(nil), state.RecentFolders...)
	ui.RecentModules = append([]string(nil), state.RecentModules...)
	// The language is restored before the table is built; refreshLocales is
	// what rediscovers the installed languages and would otherwise fall back
	// to English on an empty setting, so the default is filled in here.
	ui.Locale = strings.TrimSpace(state.Locale)
	if ui.Locale == "" {
		ui.Locale = editors.LocaleDefault
	}
	ApplyMaterialTheme(ui.Th, ui.DarkTheme)
	editors.RefreshLocales(ui)
	MarkStateDirty(ui)
}

func RememberRecentModule(ui *UiState, modulePath string) {
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

func RememberRecentFolder(ui *UiState, folder string) {
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

func SamePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

type (
	ClipKind      = cliphist.ClipKind
	ClipEntry     = cliphist.ClipEntry
	ClipHistory   = uistate.ClipHistory
	BoneKeyClip   = cliphist.BoneKeyClip
	WheEventClip  = cliphist.WheEventClip
	WheActionClip = cliphist.WheActionClip
)

const (
	ClipEvents       = cliphist.ClipEvents
	ClipBoneKeys     = cliphist.ClipBoneKeys
	ClipImportKeys   = cliphist.ClipImportKeys
	ClipActions      = cliphist.ClipActions
	ClipHistoryLimit = cliphist.ClipHistoryLimit
)
