package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"dow-mod-studio/internal/rgd"
	"gioui.org/widget/material"
)

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

// ─── merged from x_test_locale_edit_test.go ────────────────────────────────────────

func TestLocaleRowActionFor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hasText   bool
		alt, ctrl bool
		want      localeRowAction
	}{
		{"text, plain click", true, false, false, localeRowNothing},
		{"text, Alt", true, true, false, localeRowJump},
		{"text, Alt+Ctrl", true, true, true, localeRowEdit},
		{"text, Ctrl alone", true, false, true, localeRowNothing},
		{"no text, plain click", false, false, false, localeRowCreate},
		{"no text, Alt+Ctrl", false, true, true, localeRowCreate},
		{"no text, Alt", false, true, false, localeRowCreate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := localeRowActionFor(tc.hasText, tc.alt, tc.ctrl); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ── the .ucs file ───────────────────────────────────────────────────────────

// TestWriteUCSDocumentRoundTrips is the format the engine reads, checked by
// reading it back with the decoder the rest of the app uses. A .ucs is UTF-16LE
// with a byte order mark and CRLF endings; getting the mark wrong glues an
// unprintable character to the first id of the file, which silently loses that
// entry.
func TestWriteUCSDocumentRoundTrips(t *testing.T) {
	lines := []string{"705754\tNecron Lord", "90100\tArmour", "666550\tMass Resurrection"}
	data := writeUCSDocument(lines)

	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xFE {
		t.Fatalf("the file does not start with a UTF-16LE byte order mark: % x", data[:4])
	}
	// CRLF, and a final one so the last entry is a line and not a fragment.
	if !strings.Contains(string(decodeUCS(data)), "705754\tNecron Lord\r\n") {
		t.Errorf("entries are not CRLF separated: %q", string(decodeUCS(data))[:40])
	}
	if !strings.HasSuffix(string(decodeUCS(data)), "\r\n") {
		t.Error("the file does not end with a line ending")
	}
	// And the decoder the app itself uses gets the same lines back.
	got := decodeUCSDocument(data)
	if len(got) != len(lines) {
		t.Fatalf("read back %d lines, wrote %d: %q", len(got), len(lines), got)
	}
	for i := range lines {
		if got[i] != lines[i] {
			t.Errorf("line %d read back as %q, wrote %q", i, got[i], lines[i])
		}
	}
}

// TestUCSSetEntryReplacesInPlace: writing a string the file already has must
// change that line, not add a second one. Two lines with the same id is a table
// whose meaning depends on read order, which is not a thing to leave behind in
// a file a person will edit by hand.
func TestUCSSetEntryReplacesInPlace(t *testing.T) {
	lines := []string{"705754\tNecron Lord", "90100\tArmour", "666550\tMass Resurrection"}
	out, line := ucsSetEntry(lines, "90100", "Броня")
	if line != 2 {
		t.Errorf("the entry ended up on line %d, want 2", line)
	}
	if len(out) != len(lines) {
		t.Errorf("the file grew from %d lines to %d", len(lines), len(out))
	}
	if out[1] != "90100\tБроня" {
		t.Errorf("line 2 reads %q", out[1])
	}
	if out[0] != lines[0] || out[2] != lines[2] {
		t.Errorf("the other lines changed: %q", out)
	}
}

// TestUCSSetEntryAppendsWhenAbsent is the override case, which is the common
// one: a string the game already defines, given a different text by the mod.
func TestUCSSetEntryAppendsWhenAbsent(t *testing.T) {
	lines := []string{"705754\tNecron Lord"}
	out, line := ucsSetEntry(lines, "705754", "Некрон Лорд")
	_ = out
	if line != 1 {
		t.Errorf("unexpected line %d", line)
	}
	out, line = ucsSetEntry(lines, "705754", "Некрон Лорд")
	_ = out
	if line != 1 {
		t.Errorf("an existing id should be replaced in place, not appended: line %d", line)
	}
	out, line = ucsSetEntry(lines, "1000000000", "Something new")
	if line != 2 {
		t.Errorf("a new id should be appended on line 2, got %d", line)
	}
	if out[1] != "1000000000\tSomething new" {
		t.Errorf("the appended line reads %q", out[1])
	}
}

// TestUCSSanitiseTextKeepsEntriesOnePerLine: an entry is one line, so a newline
// in the text would become a second entry with no id at all, and a tab would
// split the entry the wrong way round. Both are the kind of thing a paste from a
// word processor brings in, so they are handled rather than trusted.
func TestUCSSanitiseTextKeepsEntriesOnePerLine(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"one line", "one line"},
		{"two\nlines", "two lines"},
		{"two\r\nlines", "two lines"},
		{"a\tb", "a b"},
		{"trailing\n", "trailing "},
	} {
		if got := ucsSanitiseText(tc.in); got != tc.want {
			t.Errorf("ucsSanitiseText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	out, _ := ucsSetEntry(nil, "1", "two\nlines")
	if len(out) != 1 {
		t.Errorf("a newline in the text produced %d lines: %q", len(out), out)
	}
}

// TestUCSDeleteEntry covers removing a string the mod added, which is the same
// operation as adding one.
func TestUCSDeleteEntry(t *testing.T) {
	lines := []string{"1\tone", "2\ttwo", "3\tthree"}
	out, ok := ucsDeleteEntry(lines, "2")
	if !ok {
		t.Fatal("the entry was not found")
	}
	if len(out) != 2 || out[0] != "1\tone" || out[1] != "3\tthree" {
		t.Errorf("the file reads %q", out)
	}
	if _, ok := ucsDeleteEntry(lines, "9"); ok {
		t.Error("deleting an id that is not there reported success")
	}
	if len(lines) != 3 {
		t.Error("delete changed the slice it was given")
	}
}

// ── where a string goes ─────────────────────────────────────────────────────

// TestLocaleEditPathIsInsideTheProject: a change must land in the mod's own
// folder, never in the game's installed files. The path is checked part by part
// because the whole point is that it cannot end up somewhere else.
func TestLocaleEditPathIsInsideTheProject(t *testing.T) {
	root := t.TempDir()
	path, err := localeEditPath(root, "Russian")
	if err != nil {
		t.Fatalf("localeEditPath: %v", err)
	}
	want := filepath.Join(root, "Locale", "Russian", "ModStudio.ucs")
	if path != want {
		t.Errorf("got %q, want %q", path, want)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("the language folder was not created: %v", err)
	}
	// An empty project means no mod is open, and there is nowhere to write.
	if _, err := localeEditPath("  ", "English"); err == nil {
		t.Error("a path was produced with no mod open")
	}
}

// TestWriteLocaleEntryCreatesAndThenEdits: the first write has no file yet, and
// the second must change the line the first made rather than adding another.
func TestWriteLocaleEntryCreatesAndThenEdits(t *testing.T) {
	root := t.TempDir()
	path, line, err := writeLocaleEntry(root, "English", "1000000000", "First")
	if err != nil {
		t.Fatalf("the first write failed: %v", err)
	}
	if line != 1 {
		t.Errorf("the first entry is on line %d, want 1", line)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file was not created: %v", err)
	}
	if _, line, err = writeLocaleEntry(root, "English", "1000000000", "Second"); err != nil {
		t.Fatalf("the second write failed: %v", err)
	}
	if line != 1 {
		t.Errorf("the edit landed on line %d, want the entry to stay on 1", line)
	}
	lines := decodeUCSDocument(mustReadFile(t, path))
	if len(lines) != 1 {
		t.Fatalf("the file has %d lines after an edit: %q", len(lines), lines)
	}
	if lines[0] != "1000000000\tSecond" {
		t.Errorf("the file reads %q", lines[0])
	}
}

// TestTheModsOwnStringsWinOverTheEngine checks the whole reason the override
// folder is searched first: a string the game defines, given a different text
// by the mod, has to resolve to the mod's text. Reading the engine's file first
// would leave every edit invisible.
func TestTheModsOwnStringsWinOverTheEngine(t *testing.T) {
	resetLocales(t)
	gamePath := t.TempDir()
	moduleDir := filepath.Join(gamePath, "Engine")
	modDir := t.TempDir()

	// The engine's own table, in the layout the game uses.
	writeUCSFixture(t, filepath.Join(moduleDir, "Locale", "English"), "Engine.ucs",
		map[string]string{"705754": "Necron Lord", "90100": "Armour"})

	// Nothing of the mod's yet.
	if got := localisedStringOf(t, gamePath, modDir, "English", "705754"); got != "Necron Lord" {
		t.Fatalf("the engine's own string did not resolve, got %q", got)
	}

	// The mod overrides one of them and adds another.
	if _, _, err := writeLocaleEntry(modDir, "English", "705754", "Overridden"); err != nil {
		t.Fatalf("writing the override: %v", err)
	}
	if _, _, err := writeLocaleEntry(modDir, "English", "1000000000", "Brand new"); err != nil {
		t.Fatalf("writing the new string: %v", err)
	}
	setLocaleOverrideDir(modDir)
	t.Cleanup(func() { setLocaleOverrideDir("") })

	if got := localisedStringOf(t, gamePath, modDir, "English", "705754"); got != "Overridden" {
		t.Errorf("the mod's override did not win: got %q", got)
	}
	if got := localisedStringOf(t, gamePath, modDir, "English", "90100"); got != "Armour" {
		t.Errorf("a string the mod did not touch changed: got %q", got)
	}
	if got := localisedStringOf(t, gamePath, modDir, "English", "1000000000"); got != "Brand new" {
		t.Errorf("the mod's own new string did not resolve: got %q", got)
	}
}

// TestAllocatedIDsDoNotCollide is the property that matters about a free id: two
// allocations in a row must be different, and neither may be an id the loaded
// table already holds.
func TestAllocatedIDsDoNotCollide(t *testing.T) {
	resetLocales(t)
	localeState.Mu.Lock()
	localeState.Cur = &localeTable{Lang: "Test", Index: map[string]localeEntry{
		"705754":     {Text: "Necron Lord"},
		"1000000000": {Text: "Taken"},
		"1000000001": {Text: "Also taken"},
		// A huge id from somewhere else must not drag this mod's numbering
		// up with it: the ids a mod gets are meant to be predictable.
		"999999999999": {Text: "Far away"},
	}}
	localeState.Mu.Unlock()

	first := allocateLocaleID()
	second := allocateLocaleID()
	if first == "" || second == "" {
		t.Fatal("no id could be allocated")
	}
	if first == second {
		t.Errorf("two allocations gave the same id %q", first)
	}
	for _, id := range []string{first, second} {
		if _, taken := localeLookup(id); taken {
			t.Errorf("allocated %q, which the table already holds", id)
		}
	}
	// The two ids in use are skipped rather than handed out, and the huge id
	// from elsewhere does not move this mod's numbering.
	if first != "1000000002" {
		t.Errorf("the first free id is %q, want 1000000002", first)
	}
	if second != "1000000003" {
		t.Errorf("the next free id is %q, want 1000000003", second)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

// writeUCSFixture writes a .ucs from id to text pairs.
func writeUCSFixture(t *testing.T, dir string, name string, entries map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	lines := make([]string, 0, len(entries))
	for id, text := range entries {
		lines = append(lines, id+"\t"+text)
	}
	if err := os.WriteFile(filepath.Join(dir, name), writeUCSDocument(lines), 0644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
}

// localisedStringOf builds a table and reads one id out of it.
func localisedStringOf(t *testing.T, gamePath, modDir, lang, id string) string {
	t.Helper()
	setLocale(gamePath, modDir, lang)
	return localisedString(id)
}
