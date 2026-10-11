package main

import (
	"dow-mod-studio/internal/rgd"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"gioui.org/widget/material"
)

// useTempAppData points the settings file at a throwaway folder for the rest of
// the test. stateFilePath resolves through os.UserConfigDir, which on Windows
// is the AppData environment variable, so that is what has to be redirected —
// and it has to be a temp dir, because writing the user's real settings from a
// test is not a thing a test may do.
func useTempAppData(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("LOCALAPPDATA", dir)
	// A no-op Setenv would leave the tests writing to the real file, so the
	// redirect is proved rather than assumed.
	got, err := stateFilePath()
	if err != nil {
		t.Fatalf("stateFilePath after redirect: %v", err)
	}
	if !strings.HasPrefix(got, dir) {
		t.Fatalf("stateFilePath is %q, which is outside the temp folder %q: the tests would write to the real settings file", got, dir)
	}
}

// newLocaleTestUI builds a uiState for the tests that only read state and
// never lay a frame out, so it does not need a font collection: a shaper is
// only touched while painting. The tests that do lay a frame out are in
// locale_ui_windows_test.go and use unitTestTheme.
func newLocaleTestUI() *uiState {
	return newUI(material.NewTheme())
}

// ── fixtures ───────────────────────────────────────────────────────────────

// encodeUCS writes text the way the game does: UTF-16LE, byte order mark, CRLF
// line endings, one "id<TAB>text" entry per line. Every parser test goes
// through it so no test can accidentally be fed a file the game would not
// write.
func encodeUCS(t *testing.T, lines ...string) []byte {
	t.Helper()
	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteString("\r\n")
	}
	return encodeUCSRaw(t, sb.String())
}

// encodeUCSRaw is encodeUCS for text that already carries its own endings.
func encodeUCSRaw(t *testing.T, text string) []byte {
	t.Helper()
	out := []byte{0xFF, 0xFE}
	for _, r := range utf16.Encode([]rune(text)) {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}

// writeUCS lays out a fake game folder: a game root with an Engine module and
// another module, each with a Locale folder. It returns the game root and the
// module folder.
func writeUCS(t *testing.T, layout map[string][]byte) (gamePath string, moduleDir string) {
	t.Helper()
	gamePath = t.TempDir()
	moduleDir = filepath.Join(gamePath, "MyMod")
	for rel, data := range layout {
		path := filepath.Join(gamePath, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return gamePath, moduleDir
}

func resetLocales(t *testing.T) {
	t.Helper()
	resetLocaleTables()
	t.Cleanup(resetLocaleTables)
}

// ── decoding ───────────────────────────────────────────────────────────────

// The first id in a real table is glued to the byte order mark, because the
// BOM lives at the head of the file and not on every line. Getting this wrong
// drops the first entry of every table in the game.
func TestParseUCSStripsTheBOMFromTheFirstID(t *testing.T) {
	resetLocales(t)
	data := encodeUCS(t, "300\tGamma", "301\tResolution")

	index := map[string]localeEntry{}
	added := parseUCS(data, "Engine.ucs", index)

	if added != 2 {
		t.Fatalf("added %d entries, want 2: %v", added, index)
	}
	if _, ok := index["300"]; !ok {
		t.Errorf("first id not indexed, the BOM was not stripped: %v", index)
	}
	if got := index["301"].Text; got != "Resolution" {
		t.Errorf("text = %q, want %q", got, "Resolution")
	}
}

func TestParseUCSReadsRealIDs(t *testing.T) {
	resetLocales(t)
	// The three shapes the units actually use, including a screen name, a unit
	// name and a multi word ability.
	data := encodeUCS(t,
		"90100\tArmour",
		"705754\tNecron Lord",
		"4300000\tAct of Faith: Ascension",
	)
	index := map[string]localeEntry{}
	parseUCS(data, "Engine.ucs", index)

	for id, want := range map[string]string{
		"90100":   "Armour",
		"705754":  "Necron Lord",
		"4300000": "Act of Faith: Ascension",
	} {
		if got := index[id].Text; got != want {
			t.Errorf("$%s = %q, want %q", id, got, want)
		}
	}
}

func TestParseUCSLinesAreOneBased(t *testing.T) {
	resetLocales(t)
	data := encodeUCS(t, "1\tfirst", "2\tsecond", "3\tthird")
	index := map[string]localeEntry{}
	parseUCS(data, "Engine.ucs", index)

	if got := index["1"].Line; got != 1 {
		t.Errorf("first entry on line %d, want 1", got)
	}
	if got := index["3"].Line; got != 3 {
		t.Errorf("third entry on line %d, want 3", got)
	}
}

func TestParseUCSNonASCII(t *testing.T) {
	resetLocales(t)
	data := encodeUCS(t, "90100\tНевозможно уничтожить", "90200\t下城区")
	index := map[string]localeEntry{}
	parseUCS(data, "Russian.ucs", index)

	if got := index["90100"].Text; got != "Невозможно уничтожить" {
		t.Errorf("Cyrillic = %q", got)
	}
	if got := index["90200"].Text; got != "下城区" {
		t.Errorf("CJK = %q", got)
	}
}

// Three English lines embed a literal tab in the text, so splitting on every
// tab loses the tail of the text. Only the first tab is a separator.
func TestParseUCSKeepsTabsInsideTheText(t *testing.T) {
	resetLocales(t)
	data := encodeUCS(t, "34728\tPurchase includes:\tSpace Marine\tOrks")
	index := map[string]localeEntry{}
	parseUCS(data, "Engine.ucs", index)

	want := "Purchase includes:\tSpace Marine\tOrks"
	if got := index["34728"].Text; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestParseUCSSkipsJunkLines(t *testing.T) {
	resetLocales(t)
	data := encodeUCSRaw(t, "300\tGamma\r\n\r\n; a comment\r\nnotanid\ttext\r\n301\tok\r\n")
	index := map[string]localeEntry{}
	added := parseUCS(data, "Engine.ucs", index)

	if added != 2 {
		t.Fatalf("added %d entries, want 2: %v", added, index)
	}
	if _, bad := index["notanid"]; bad {
		t.Errorf("a non numeric id was indexed: %v", index)
	}
}

func TestParseUCSFallsBackToUTF8(t *testing.T) {
	resetLocales(t)
	// A table saved by a modding tool comes out as UTF-8 with no mark.
	index := map[string]localeEntry{}
	parseUCS([]byte("300\tGamma\r\n"), "plain.ucs", index)
	if got := index["300"].Text; got != "Gamma" {
		t.Errorf("text = %q, want Gamma", got)
	}
}

func TestDecodeUCSBigEndian(t *testing.T) {
	resetLocales(t)
	var out []byte
	out = append(out, 0xFE, 0xFF)
	for _, u := range utf16.Encode([]rune("300\tGamma")) {
		out = append(out, byte(u>>8), byte(u))
	}
	if got := trimBOM(decodeUCS(out)); got != "300\tGamma" {
		t.Errorf("decoded %q", got)
	}
}

// ── the table ──────────────────────────────────────────────────────────────

// A mod that ships its own table for an id the engine also defines has to win,
// because that is what the game shows.
func TestLocaleTablePrefersTheOpenModule(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
		"MyMod/Locale/English/MyMod.ucs":   encodeUCS(t, "90100\tMod Armour"),
	})

	// A .module file rather than the folder, since that is the other way a
	// module is opened.
	moduleFile := filepath.Join(moduleDir, "MyMod.module")
	if err := os.WriteFile(moduleFile, []byte("[global]\nModFolder=MyMod\n"), 0644); err != nil {
		t.Fatalf("write module file: %v", err)
	}
	setLocale(gamePath, moduleFile, "English")
	if got := localisedString("90100"); got != "Mod Armour" {
		t.Errorf("text = %q, want the mod's own", got)
	}
}

func TestLocaleTableFallsBackToTheEngine(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour", "705754\tNecron Lord"),
		"MyMod/Locale/English/MyMod.ucs":   encodeUCS(t, "90100\tMod Armour"),
	})

	setLocale(gamePath, moduleDir, "English")
	// 705754 is only in the engine's table, so it still has to resolve.
	if got := localisedString("705754"); got != "Necron Lord" {
		t.Errorf("text = %q, want Necron Lord", got)
	}
}

func TestLocaleTableSwitchesLanguage(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
		"Engine/Locale/Russian/Engine.ucs": encodeUCS(t, "90100\tБроня"),
	})

	setLocale(gamePath, moduleDir, "English")
	if got := localisedString("90100"); got != "Armour" {
		t.Fatalf("English = %q", got)
	}
	setLocale(gamePath, moduleDir, "Russian")
	if got := localisedString("90100"); got != "Броня" {
		t.Errorf("Russian = %q, want Броня", got)
	}
	// Going back is served from the cache and has to give the same answer.
	setLocale(gamePath, moduleDir, "English")
	if got := localisedString("90100"); got != "Armour" {
		t.Errorf("back to English = %q", got)
	}
}

func TestLocaleTableReportsTheLineAndFile(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "1\tfirst", "2\tsecond", "90100\tArmour"),
	})
	setLocale(gamePath, moduleDir, "English")

	entry, ok := localeLookup("90100")
	if !ok {
		t.Fatal("90100 did not resolve")
	}
	if entry.Line != 3 {
		t.Errorf("line = %d, want 3", entry.Line)
	}
	if filepath.Base(entry.File) != "Engine.ucs" {
		t.Errorf("file = %q, want Engine.ucs", entry.File)
	}
	if got := localeLineNumber("90100"); got != 3 {
		t.Errorf("localeLineNumber = %d, want 3", got)
	}
	if got := localeLineNumber("404"); got != 0 {
		t.Errorf("an unknown id reported line %d, want 0", got)
	}
}

func TestLocaleMissIsEmptyNotTheID(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
	})
	setLocale(gamePath, moduleDir, "English")

	// The id is the value and stays on screen; a miss adds nothing to it.
	if got := rgd.LocIDDisplay("$90100"); got != "$90100  Armour" {
		t.Errorf("known id = %q", got)
	}
	if got := rgd.LocIDDisplay("$123456"); got != "$123456" {
		t.Errorf("unknown id = %q, want the id alone", got)
	}
}

func TestLocaleTableMissingFilesIsNotAnError(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, nil)
	table := setLocale(gamePath, moduleDir, "Klingon")

	if table.Files != 0 {
		t.Errorf("Files = %d, want 0", table.Files)
	}
	if got := localisedString("90100"); got != "" {
		t.Errorf("text = %q, want empty", got)
	}
}

// ── discovering languages ──────────────────────────────────────────────────

func TestAvailableLocalesUnionsTheEngineAndTheModule(t *testing.T) {
	resetLocales(t)
	// The engine ships thirteen; the THB test module adds Slovak and
	// English_Chinese on top, so the picker has to show those too.
	gamePath, _ := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs":     encodeUCS(t),
		"Engine/Locale/Russian/Engine.ucs":     encodeUCS(t),
		"MyMod/Locale/Slovak/MyMod.ucs":        encodeUCS(t),
		"MyMod/Locale/English_Chinese/Mod.ucs": encodeUCS(t),
	})

	langs := availableLocales(gamePath, "")
	joined := strings.Join(langs, ",")
	for _, want := range []string{"English", "Russian", "Slovak", "English_Chinese"} {
		if !containsFold(langs, want) {
			t.Errorf("%s missing from %s", want, joined)
		}
	}
}

func TestAvailableLocalesAlwaysOffersEnglish(t *testing.T) {
	resetLocales(t)
	// No game folder at all: the mod tree is built with English, so English
	// has to be on offer rather than leaving an empty picker.
	langs := availableLocales("", "")
	if len(langs) != 1 || langs[0] != localeDefault {
		t.Errorf("langs = %v, want [%s]", langs, localeDefault)
	}
}

func TestAvailableLocalesAreSortedAndUnique(t *testing.T) {
	resetLocales(t)
	gamePath, _ := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t),
		"Engine/Locale/russian/Engine.ucs": encodeUCS(t),
		"MyMod/Locale/ENGLISH/MyMod.ucs":   encodeUCS(t),
	})
	langs := availableLocales(gamePath, "")
	// english and ENGLISH are the same language and must not both appear.
	seen := map[string]bool{}
	for _, l := range langs {
		lower := strings.ToLower(l)
		if seen[lower] {
			t.Errorf("duplicate language %q in %v", l, langs)
		}
		seen[lower] = true
	}
}

// A language remembered from a previous session may not be installed any more;
// the field must not go on naming a table that was never built.
func TestRefreshLocalesFallsBackWhenTheLanguageIsGone(t *testing.T) {
	resetLocales(t)
	ui := newLocaleTestUI()
	ui.gamePath, ui.modulePath = writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
	})
	ui.locale = "Ultralanguage"

	ui.refreshLocales()

	if ui.locale != localeDefault {
		t.Errorf("locale = %q, want %q", ui.locale, localeDefault)
	}
	if got := localisedString("90100"); got != "Armour" {
		t.Errorf("table not rebuilt for the fallback: %q", got)
	}
}

func TestRefreshLocalesKeepsAModOnlyLanguage(t *testing.T) {
	resetLocales(t)
	ui := newLocaleTestUI()
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
		"MyMod/Locale/Slovak/MyMod.ucs":    encodeUCS(t, "90100\tBrnena"),
	})
	ui.gamePath = gamePath
	ui.modulePath = moduleDir
	ui.locale = "Slovak"

	ui.refreshLocales()

	if ui.locale != "Slovak" {
		t.Fatalf("locale = %q, want Slovak", ui.locale)
	}
	if got := localisedString("90100"); got != "Brnena" {
		t.Errorf("text = %q, want Brnena", got)
	}
}

func TestSetLocaleLanguagePersistsTheChoice(t *testing.T) {
	resetLocales(t)
	ui := newLocaleTestUI()
	ui.gamePath, ui.modulePath = writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
		"Engine/Locale/Russian/Engine.ucs": encodeUCS(t, "90100\tБроня"),
	})
	ui.refreshLocales()
	ui.stateDirty = false

	ui.setLocaleLanguage("Russian")

	if ui.locale != "Russian" {
		t.Errorf("locale = %q, want Russian", ui.locale)
	}
	if !ui.stateDirty {
		t.Error("picking a language did not mark the settings for saving")
	}
	if got := localisedString("90100"); got != "Броня" {
		t.Errorf("text = %q, want Броня", got)
	}
}

// ── the saved setting ──────────────────────────────────────────────────────

func TestLocaleSurvivesASaveAndLoad(t *testing.T) {
	resetLocaleTables()
	t.Cleanup(resetLocaleTables)

	ui := newLocaleTestUI()
	ui.gamePath, ui.modulePath = writeUCS(t, map[string][]byte{
		"Engine/Locale/Russian/Engine.ucs": encodeUCS(t, "90100\tБроня"),
	})
	ui.locale = "Russian"
	ui.stateDirty = true

	// The settings file sits in the user's real AppData next to the theme
	// choice, so the round trip goes through the real path — but the folder is
	// redirected first, because writing the user's actual settings from a test
	// is not a thing a test may do.
	useTempAppData(t)
	if err := savePersistedState(ui); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, _, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Locale != "Russian" {
		t.Errorf("Locale = %q, want Russian", loaded.Locale)
	}

	restored := newLocaleTestUI()
	restored.applyPersistedSettings(loaded)
	if restored.locale != "Russian" {
		t.Errorf("restored locale = %q, want Russian", restored.locale)
	}
}

func TestEmptyLocaleSettingFallsBackToEnglish(t *testing.T) {
	resetLocales(t)
	ui := newLocaleTestUI()
	ui.applyPersistedSettings(persistedState{DarkTheme: true})
	if ui.locale != localeDefault {
		t.Errorf("locale = %q, want %q", ui.locale, localeDefault)
	}
}

// ── the jump ───────────────────────────────────────────────────────────────

// Gio counts runes and a .ucs is mostly non-ASCII, so a byte offset would land
// in the wrong place. This is the Cyrillic table the game really ships.
func TestLocaleLineRangeCountsRunes(t *testing.T) {
	resetLocales(t)
	text := "90100\tНевозможно уничтожить\r\n90101\tСтроитель\r\n90102\tБроня\r\n"

	// Line 1, and the id is at the very start: offset 0, five runes long.
	start, end := localeLineRange(text, 1, "90100")
	if start != 0 || end != 5 {
		t.Errorf("line 1 = %d..%d, want 0..5", start, end)
	}

	// Line 2 sits after 90100 + tab + 20 Cyrillic runes + CRLF.
	first := "90100\tНевозможно уничтожить\r\n"
	firstRunes := len([]rune(first))
	start, end = localeLineRange(text, 2, "90101")
	if start != firstRunes || end != firstRunes+5 {
		t.Errorf("line 2 = %d..%d, want %d..%d", start, end, firstRunes, firstRunes+5)
	}
	if got := []rune(text)[start:end]; string(got) != "90101" {
		t.Errorf("selection = %q, want 90101", got)
	}
}

func TestLocaleLineRangeFallsBackToTheWholeLine(t *testing.T) {
	resetLocales(t)
	// The id is not where the index says it is, which happens when the file
	// changed under the table. The line itself is still the right answer.
	text := "90100\tGamma\r\n"
	start, end := localeLineRange(text, 1, "99999")
	if got := strings.TrimSuffix(string([]rune(text)[start:end]), "\r"); got != "90100\tGamma" {
		t.Errorf("selection = %q, want the whole line", got)
	}
}

func TestLocaleLineRangeOutOfRange(t *testing.T) {
	resetLocales(t)
	text := "90100\tGamma\r\n"
	if start, end := localeLineRange(text, 0, "90100"); start != 0 || end != 0 {
		t.Errorf("line 0 = %d..%d, want 0..0", start, end)
	}
	if start, end := localeLineRange(text, 99, "90100"); start != 0 || end != 0 {
		t.Errorf("line 99 = %d..%d, want 0..0", start, end)
	}
}

// ── only one popup owns the space under the menu bar ────────────────────────

// A click on the menu bar lands above the list's dismiss area, so opening a
// menu or the search has to take the list away; nothing else would.
func TestOpeningAMenuOrSearchClosesTheLanguageList(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(ui *uiState)
	}{
		{"a menu", func(ui *uiState) { ui.menuOpen = 0 }},
		{"the search", func(ui *uiState) { ui.searchVisible = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ui := newLocaleTestUI()
			ui.localeOpen = true
			tc.setup(ui)
			ui.closeLocaleUnderOtherMenuBarPopups()
			if ui.localeOpen {
				t.Error("the language list stayed open under " + tc.name)
			}
		})
	}
}

// With nothing else open the list is left alone, or a frame with no input
// anywhere would keep shutting it.
func TestTheLanguageListSurvivesAFrameWithNoInput(t *testing.T) {
	ui := newLocaleTestUI()
	ui.menuOpen = -1
	ui.localeOpen = true
	ui.closeLocaleUnderOtherMenuBarPopups()
	if !ui.localeOpen {
		t.Error("the list was closed by a frame where nothing was clicked")
	}
}

func TestOpenLocaleFileSelectsTheIDAndIsReadOnly(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "1\tone", "90100\tArmour", "3\tthree"),
	})
	setLocale(gamePath, moduleDir, "English")
	ui := newLocaleTestUI()

	ui.jumpToLocaleEntry("90100")

	if len(ui.openTabs) != 1 {
		t.Fatalf("opened %d tabs, want 1", len(ui.openTabs))
	}
	tab := ui.openTabs[0]
	if !tab.textLocale {
		t.Error("the tab is not marked as a locale file")
	}
	if !tab.textEditor.ReadOnly {
		t.Error("a game locale file was opened editable")
	}
	if tab.textDirty {
		t.Error("a freshly opened locale file is already dirty")
	}
	// The file is UTF-16, so the text in the editor is UTF-8.
	if got := tab.textEditor.Text(); !strings.Contains(got, "90100\tArmour") {
		t.Errorf("decoded text = %q", got)
	}
	start, end := tab.textEditor.Selection()
	if sel := string([]rune(tab.textEditor.Text())[start:end]); sel != "90100" {
		t.Errorf("selection = %q, want 90100", sel)
	}
}

func TestOpenLocaleFileReusesAnOpenTab(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "1\tone", "90100\tArmour"),
	})
	setLocale(gamePath, moduleDir, "English")
	ui := newLocaleTestUI()

	ui.jumpToLocaleEntry("90100")
	ui.jumpToLocaleEntry("90100")

	if len(ui.openTabs) != 1 {
		t.Errorf("opened %d tabs for one file, want 1", len(ui.openTabs))
	}
}

func TestJumpToAnUnknownIDSaysSo(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
	})
	setLocale(gamePath, moduleDir, "English")
	ui := newLocaleTestUI()

	ui.jumpToLocaleEntry("404")

	if len(ui.openTabs) != 0 {
		t.Errorf("opened a tab for an id that is not in the table")
	}
	if !strings.Contains(ui.statusText, "404") {
		t.Errorf("status = %q, want it to name the id", ui.statusText)
	}
}

func TestLocaleTabsAreNotSavedAsModFiles(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
	})
	setLocale(gamePath, moduleDir, "English")
	ui := newLocaleTestUI()
	ui.gamePath = gamePath
	ui.modulePath = moduleDir
	ui.jumpToLocaleEntry("90100")

	if _, err := stateFilePath(); err != nil {
		t.Fatalf("stateFilePath: %v", err)
	}
	useTempAppData(t)
	if err := savePersistedState(ui); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, _, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// A .ucs is not in the mod tree, so reopening it from the saved session
	// must not try.
	for _, tab := range loaded.Tabs {
		if strings.EqualFold(filepath.Ext(tab.Path), ".ucs") {
			t.Errorf("a locale file was persisted as a tab: %q", tab.Path)
		}
	}
}

func TestSavingALocaleTabIsRefused(t *testing.T) {
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
	})
	setLocale(gamePath, moduleDir, "English")
	ui := newLocaleTestUI()
	ui.jumpToLocaleEntry("90100")
	before, err := os.ReadFile(ui.openTabs[0].path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	ui.handleTextSave(ui.openTabs[0])

	if !strings.Contains(ui.statusText, "read only") {
		t.Errorf("status = %q, want it to say read only", ui.statusText)
	}
	after, err := os.ReadFile(ui.openTabs[0].path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// The file is UTF-16 on disk; writing the editor's UTF-8 back would
	// corrupt every line of it.
	if string(before) != string(after) {
		t.Error("the .ucs on disk changed")
	}
}
