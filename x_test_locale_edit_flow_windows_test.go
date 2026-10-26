//go:build windows

package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/rgd"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
)

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

// ─── merged from x_test_locale_edit_ui_windows_test.go ────────────────────────────────────────

func renderTo(t *testing.T, w, h int, draw func(gtx layout.Context)) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()
	var ops op.Ops
	draw(newLayoutContextForTest(&ops, w, h))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

// distinctColours counts the distinct colours in a box, which is how writing is
// told from a flat fill: text antialiases into many shades and a background is
// one or two.
func localeDistinctColours(img *image.RGBA, box image.Rectangle) int {
	box = box.Intersect(img.Bounds())
	seen := map[color.RGBA]bool{}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			seen[img.RGBAAt(x, y)] = true
		}
	}
	return len(seen)
}

// differingPixels counts the pixels of a that b does not have.
func localeDifferingPixels(a, b *image.RGBA) int {
	bd := a.Bounds().Intersect(b.Bounds())
	n := 0
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		for x := bd.Min.X; x < bd.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

const localeRowTextPixels = 200

// TestLocaleRowShowsTheTextNotTheId is the third of the three asks: the row
// leads with the text, and the id is not in it. A row that printed "$705754" and
// the text together would be unreadable, and the id has the tooltip to go in.
func TestLocaleRowShowsTheTextNotTheId(t *testing.T) {
	resetLocales(t)
	ui := newUI(unitTestTheme())
	ui.locale = "English"
	ui.testLocaleTable(t, "705754", "Necron Lord")

	entry := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$705754"}
	const w, h = 420, 60

	withText := renderTo(t, w, h, func(gtx layout.Context) {
		ui.localeEditableRow(gtx, "Screen name", "705754", entry, "k")
	})
	// The same row with the string table knowing nothing: it falls back to
	// naming what is missing, which is a different set of words.
	ui.testLocaleTable(t, "1", "One")
	withoutText := renderTo(t, w, h, func(gtx layout.Context) {
		ui.localeEditableRow(gtx, "Screen name", "705754", entry, "k")
	})

	if n := localeDifferingPixels(withText, withoutText); n < localeRowTextPixels {
		t.Errorf("only %d pixels changed between a row with its text and one without, "+
			"want at least %d: the row is not showing the text", n, localeRowTextPixels)
	}
	if c := localeDistinctColours(withText, withText.Bounds()); c < 20 {
		t.Errorf("the row holds %d distinct colours, which is a flat fill rather than writing", c)
	}
}

// TestLocaleRowTooltipCarriesTheId: hovering says which string the row is
// about, because the row itself deliberately does not.
func TestLocaleRowTooltipCarriesTheId(t *testing.T) {
	resetLocales(t)
	ui := newUI(unitTestTheme())
	ui.locale = "English"
	ui.testLocaleTable(t, "705754", "Necron Lord")

	tip := ui.localeRowTooltip("705754")
	if !strings.Contains(tip, "$705754") {
		t.Errorf("the tooltip does not carry the id: %q", tip)
	}
	// It says where the text came from, so a reader can tell the engine's string
	// from the mod's override of it.
	if !strings.Contains(tip, "Alt+Ctrl") {
		t.Errorf("the tooltip does not say how to change the text: %q", tip)
	}

	// A field with no id says what a click will do, not "$0".
	none := ui.localeRowTooltip("0")
	if strings.Contains(none, "$0 ") {
		t.Errorf("the tooltip for an unset field leads with the marker: %q", none)
	}
	if !strings.Contains(none, "click") {
		t.Errorf("the tooltip for an unset field does not say a click adds one: %q", none)
	}
}

// TestLocaleEditDialogDrawsItsText: the dialog is the whole of the editing
// gesture, so a frame that draws it has to contain its own words — the language
// and the id it is about.
func TestLocaleEditDialogDrawsItsText(t *testing.T) {
	resetLocales(t)
	ui := newUI(unitTestTheme())
	ui.locale = "English"
	ui.testLocaleTable(t, "705754", "Necron Lord")
	ui.projectDir = t.TempDir()

	closed := renderTo(t, 900, 700, func(gtx layout.Context) {
		ui.layoutLocaleEditDialog(gtx)
	})
	ui.openLocaleEdit("705754", localeRowEdit, nil, "k")
	if !ui.localeEditDialog {
		t.Fatal("the dialog did not open")
	}
	open := renderTo(t, 900, 700, func(gtx layout.Context) {
		ui.layoutLocaleEditDialog(gtx)
	})

	n := localeDifferingPixels(closed, open)
	if n < 2000 {
		t.Errorf("opening the dialog changed only %d pixels, so very little of it was drawn", n)
	}
	if c := localeDistinctColours(open, open.Bounds()); c < 20 {
		t.Errorf("the dialog holds %d distinct colours, which is a flat fill", c)
	}
}
