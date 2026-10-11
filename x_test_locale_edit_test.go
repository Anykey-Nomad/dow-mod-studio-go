package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── what a press means ──────────────────────────────────────────────────────

// TestLocaleRowActionFor is the whole gesture rule, which is a thing worth
// stating on its own because the object window and the file view both use it and
// they must not disagree about what a click does.
//
// The shape of it: a row that has text needs a deliberate gesture to change,
// because a plain click on it is a selection; a row that has none takes a plain
// click, because there is nothing to select and a string is exactly what is
// missing.
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
	localeState.mu.Lock()
	localeState.cur = &localeTable{Lang: "Test", Index: map[string]localeEntry{
		"705754":     {Text: "Necron Lord"},
		"1000000000": {Text: "Taken"},
		"1000000001": {Text: "Also taken"},
		// A huge id from somewhere else must not drag this mod's numbering
		// up with it: the ids a mod gets are meant to be predictable.
		"999999999999": {Text: "Far away"},
	}}
	localeState.mu.Unlock()

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
