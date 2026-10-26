package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestPersistedStateJSONRoundTrip(t *testing.T) {
	want := persistedState{
		Version:          1,
		DarkTheme:        false,
		GamePath:         `C:\Games\Dawn of War Definitive Edition`,
		ModulePath:       `C:\Mods\Test\Test.module`,
		RecentFolders:    []string{`C:\Mods\Test`},
		Expanded:         map[string]bool{"Data": true, `Data\attrib`: true},
		SelectedTreePath: `Data\attrib\example.rgd`,
		SelectedPath:     `Data\attrib\example.rgd`,
		ActiveTab:        2,
		Tabs:             []persistedTab{{Path: `Data\attrib\example.rgd`, Kind: "rgd", RGDSelected: "GameData"}},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got persistedState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ModulePath != want.ModulePath || got.ActiveTab != want.ActiveTab || len(got.Tabs) != 1 || got.Tabs[0].RGDSelected != "GameData" {
		t.Fatalf("round trip lost session state: %#v", got)
	}
}

func TestRecentModules(t *testing.T) {
	ui := &uiState{}
	first, err := filepath.Abs(filepath.Join("mods", "B", "B.module"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := filepath.Abs(filepath.Join("mods", "A", "A.module"))
	if err != nil {
		t.Fatal(err)
	}
	RememberRecentModule(ui, first)
	RememberRecentModule(ui, second)
	RememberRecentModule(ui, first)
	if len(ui.RecentModules) != 2 || ui.RecentModules[0] != first || ui.RecentModules[1] != second {
		t.Fatalf("recent modules = %#v", ui.RecentModules)
	}
}

func TestSamePath(t *testing.T) {
	a := filepath.Join("C:", "Games", "DoW", "..", "DoW")
	b := filepath.Join("C:", "Games", "DoW")
	if !samePath(a, b) {
		t.Fatalf("samePath(%q, %q) = false", a, b)
	}
}
