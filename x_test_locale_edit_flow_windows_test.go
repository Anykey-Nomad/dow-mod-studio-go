//go:build windows

package main

import (
	"dow-mod-studio/internal/rgd"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveLocaleEditCreatesAStringAndPointsTheFieldAtIt is the whole of what a
// click on a field with no localisation is for: a string is written under a free
// id, the field is given that id, and the text is on screen afterwards.
//
// The last part is the one that catches a silent failure. The table is cached
// per language, so a write that does not invalidate it leaves the app showing
// the old state while the file on disk has changed — the edit would look like it
// had worked and then be gone the moment anything else reloaded the table.
func TestSaveLocaleEditCreatesAStringAndPointsTheFieldAtIt(t *testing.T) {
	resetLocales(t)
	project := t.TempDir()
	ui := newUI(unitTestTheme())
	ui.projectDir = project
	ui.locale = "English"
	// An engine table with the id the field already points at, so the "before"
	// state is a real one rather than an empty table.
	gamePath := t.TempDir()
	writeUCSFixture(t, filepath.Join(gamePath, "Engine", "Locale", "English"), "Engine.ucs",
		map[string]string{"705754": "Necron Lord"})
	setLocaleOverrideDir(project)
	t.Cleanup(func() { setLocaleOverrideDir("") })
	ui.gamePath = gamePath
	ui.refreshLocales()

	// A field that carries the game's "no string" marker, which is how a unit
	// says it has no name of its own.
	entry := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$0"}
	if got, _ := rgd.LocID(entry.DisplayValue()); got != "0" {
		t.Fatalf("the fixture does not look unset, it reads %q", got)
	}

	ui.openLocaleEdit("0", localeRowCreate, entry, "unit.rgd\u00a0screen_name_id")
	if !ui.localeEditDialog {
		t.Fatal("the dialog did not open")
	}
	if !ui.localeEditCreate {
		t.Error("the dialog is not in create mode, so it would overwrite id $0")
	}
	if got := ui.localeEditEditor.Text(); got != "" {
		t.Errorf("the editor started with %q, want empty for a new string", got)
	}

	ui.localeEditEditor.SetText("My Lord")
	ui.saveLocaleEditAndClose()
	if ui.localeEditError != "" {
		t.Fatalf("saving failed: %s", ui.localeEditError)
	}
	msg := ui.statusText

	// The field now points at a real id rather than at the unset marker.
	if !strings.HasPrefix(entry.WStrVal, "$") || entry.WStrVal == "$0" {
		t.Errorf("the field still reads %q", entry.WStrVal)
	}
	id, ok := rgd.LocID(entry.WStrVal)
	if !ok || id == "0" {
		t.Fatalf("the field does not hold a usable id: %q", entry.WStrVal)
	}
	t.Logf("allocated $%s — %s", id, msg)

	// The string is on disk, in the mod's own folder and nowhere else.
	path := filepath.Join(project, "Locale", "English", "ModStudio.ucs")
	lines := decodeUCSDocument(mustReadFile(t, path))
	if len(lines) != 1 {
		t.Fatalf("the mod's file has %d lines: %q", len(lines), lines)
	}
	if want := id + "\tMy Lord"; lines[0] != want {
		t.Errorf("the file reads %q, want %q", lines[0], want)
	}
	// The engine's own file must be untouched: it is the game's data.
	engine := filepath.Join(gamePath, "Engine", "Locale", "English", "Engine.ucs")
	if strings.Contains(string(mustReadFile(t, engine)), "My Lord") {
		t.Error("the string was written into the game's file")
	}

	// And it is visible: the table was rebuilt, so the new id resolves.
	if got := localisedString(id); got != "My Lord" {
		t.Errorf("after saving, the id resolves to %q, want %q; the table was not rebuilt", got, "My Lord")
	}
	if ui.localeEditDialog {
		t.Error("the dialog is still open after a successful save")
	}
}

// TestSaveLocaleEditOverridesAnEngineString is the other mode: a string the game
// defines, given a different text. The id stays the same — the field already
// points at it, and a new id would leave the field reading the old text — and the
// mod's file gains a line the engine's table has no entry for.
func TestSaveLocaleEditOverridesAnEngineString(t *testing.T) {
	resetLocales(t)
	project := t.TempDir()
	gamePath := t.TempDir()
	writeUCSFixture(t, filepath.Join(gamePath, "Engine", "Locale", "English"), "Engine.ucs",
		map[string]string{"705754": "Necron Lord"})

	ui := newUI(unitTestTheme())
	ui.projectDir = project
	ui.locale = "English"
	ui.gamePath = gamePath
	setLocaleOverrideDir(project)
	t.Cleanup(func() { setLocaleOverrideDir("") })
	ui.refreshLocales()
	if got := localisedString("705754"); got != "Necron Lord" {
		t.Fatalf("the engine string does not resolve, got %q", got)
	}

	entry := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$705754"}
	ui.openLocaleEdit("705754", localeRowEdit, entry, "unit.rgd\u00a0screen_name_id")
	if ui.localeEditCreate {
		t.Error("editing an existing string was treated as creating a new one")
	}
	if got := ui.localeEditEditor.Text(); got != "Necron Lord" {
		t.Errorf("the editor opened with %q, want the current text", got)
	}

	ui.localeEditEditor.SetText("Overlord")
	ui.saveLocaleEditAndClose()
	if ui.localeEditError != "" {
		t.Fatalf("saving failed: %s", ui.localeEditError)
	}

	if entry.WStrVal != "$705754" {
		t.Errorf("the field was changed to %q; an override keeps the id", entry.WStrVal)
	}
	if got := localisedString("705754"); got != "Overlord" {
		t.Errorf("the id resolves to %q after the override, want %q", got, "Overlord")
	}
	// The game's file still has its own text.
	engine := filepath.Join(gamePath, "Engine", "Locale", "English", "Engine.ucs")
	if !strings.Contains(string(decodeUCS(mustReadFile(t, engine))), "Necron Lord") {
		t.Error("the engine's own entry was changed")
	}
}

// TestSaveLocaleEditRefusesAnEmptyString: a localisation with no text is a blank
// in game, and it is much easier to write by accident than to notice afterwards.
func TestSaveLocaleEditRefusesAnEmptyString(t *testing.T) {
	resetLocales(t)
	project := t.TempDir()
	ui := newUI(unitTestTheme())
	ui.projectDir = project
	ui.locale = "English"
	setLocaleOverrideDir(project)
	t.Cleanup(func() { setLocaleOverrideDir("") })
	ui.gamePath = t.TempDir()
	ui.refreshLocales()

	ui.openLocaleEdit("1000000000", localeRowCreate, nil, "")
	ui.localeEditEditor.SetText("   ")
	ui.saveLocaleEditAndClose()
	if ui.localeEditError == "" {
		t.Error("an empty string was accepted")
	}
	if _, err := os.Stat(filepath.Join(project, "Locale", "English", "ModStudio.ucs")); err == nil {
		t.Error("a file was written for a string that was never saved")
	}
}

// TestSaveLocaleEditSaysWhereThereIsNowhereToWrite: with no mod open there is no
// project folder, and the dialog has to say so rather than fail silently or
// write somewhere arbitrary.
func TestSaveLocaleEditSaysWhereThereIsNowhereToWrite(t *testing.T) {
	resetLocales(t)
	ui := newUI(unitTestTheme())
	ui.projectDir = ""
	ui.locale = "English"

	ui.openLocaleEdit("1000000000", localeRowCreate, nil, "")
	ui.localeEditEditor.SetText("Something")
	ui.saveLocaleEditAndClose()
	if ui.localeEditError == "" {
		t.Error("a string was accepted with no mod open")
	} else if !strings.Contains(ui.localeEditError, "mod is open") {
		t.Errorf("the message does not say what is wrong: %s", ui.localeEditError)
	}
}
