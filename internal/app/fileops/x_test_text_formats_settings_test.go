package fileops_test

import (
	"dow-mod-studio/internal/app/fileops"
	"dow-mod-studio/internal/app/shell"
	"dow-mod-studio/internal/testutil"
	"encoding/json"
	"testing"
)

// The list a reader sets has to survive being written to the state file and
// read back, or the setting quietly forgets itself between sessions.
func TestTextFormatsSurviveTheStateFile(t *testing.T) {
	ui := &shell.UiState{TextEditorFormats: fileops.ParseTextFormats(".lua, .txt, .dsl")}
	state := shell.PersistedState{TextFormats: fileops.FormatTextFormats(ui.TextEditorFormats)}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back shell.PersistedState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := fileops.ParseTextFormats(back.TextFormats)
	want := []string{".lua", ".txt", ".dsl"}
	if len(got) != len(want) {
		t.Fatalf("the file held %q, which reads back as %v, want %v", back.TextFormats, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("the file held %q, which reads back as %v, want %v", back.TextFormats, got, want)
		}
	}
}

// A state file written before the setting existed has no field at all. It has
// to keep working and open the built-in formats rather than nothing.
func TestAStateFileWithoutTheFormatsFallsBackToTheDefault(t *testing.T) {
	data, err := json.Marshal(shell.PersistedState{Version: 1, DarkTheme: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back shell.PersistedState
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := fileops.ParseTextFormats(back.TextFormats)
	if len(got) != len(fileops.DefaultTextFormats) {
		t.Fatalf("no field read back as %v, want the %d defaults", got, len(fileops.DefaultTextFormats))
	}
	ui := &shell.UiState{}
	shell.ApplyPersistedSettings(ui, back)
	if !fileops.IsEditorFile(ui, "script.lua") {
		t.Error("a state file with no formats field stopped .lua from opening")
	}
}

// applyPersistedSettings is the path a session starts on, so the reader's
// list has to be the one in force afterwards.
func TestApplyingPersistedSettingsInstallsTheFormats(t *testing.T) {
	ui := &shell.UiState{}
	shell.ApplyPersistedSettings(ui, shell.PersistedState{TextFormats: ".lua, .mydsl"})
	if !fileops.IsEditorFile(ui, "x.mydsl") {
		t.Error("the reader's own format did not open after applying the settings")
	}
	if fileops.IsEditorFile(ui, "x.txt") {
		t.Error("a format left out of the reader's list still opened")
	}
}

// A uiState that never saw a state file still opens the built-in formats, so
// the editor works before any settings have been loaded.
func TestTheDefaultFormatsApplyToAFreshWindow(t *testing.T) {
	ui := shell.NewUI(testutil.UnitTestTheme())
	if !fileops.IsEditorFile(ui, "script.lua") {
		t.Error("a fresh window does not open .lua")
	}
	if fileops.IsEditorFile(ui, "thing.unknown") {
		t.Error("a fresh window opened a format that is not in the list")
	}
}
