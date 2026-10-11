package main

import (
	"dow-mod-studio/internal/rgd"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real installation is the only place the parser meets a 3.4 MB table with
// 26439 entries, embedded tabs and a byte order mark on the first id. The
// fixture tests use hand written tables, so these check the real thing: the ids
// the units actually carry have to come back with their text and a line to
// jump to.
const testGamePath = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`

func testGameFolder(t *testing.T) string {
	t.Helper()
	if info, err := os.Stat(testGamePath); err != nil || !info.IsDir() {
		t.Skipf("game not installed: %v", err)
	}
	return testGamePath
}

// TestRealEnglishTableResolvesTheUnitIDs indexes the real 3.4 MB English
// Engine.ucs and asks for ids taken from the unit definitions, so a parser that
// only copes with a small fixture cannot pass.
func TestRealEnglishTableResolvesTheUnitIDs(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)

	table := setLocale(gamePath, "", "English")
	if table.Files == 0 {
		t.Fatal("no .ucs files found in the real installation")
	}
	if table.Lines < 20000 {
		t.Errorf("only %d entries indexed, the English table has 26439", table.Lines)
	}

	// Spot checked against the file: a screen name, a unit name, an ability
	// and a campaign string, across the id ranges the units use.
	for id, want := range map[string]string{
		"90100":   "Armour",
		"90101":   "Builder",
		"705754":  "Necron Lord",
		"666550":  "Mass Resurrection",
		"671551":  "Essence of the Nightbringer",
		"4300000": "Act of Faith: Ascension",
	} {
		got := localisedString(id)
		if got != want {
			t.Errorf("$%s = %q, want %q", id, got, want)
		}
		// The jump needs a line, and a line inside the real file rather than
		// a made up one.
		entry, ok := localeLookup(id)
		if !ok {
			t.Errorf("$%s did not resolve", id)
			continue
		}
		if entry.Line < 1 {
			t.Errorf("$%s reports line %d", id, entry.Line)
		}
		if filepath.Base(entry.File) != "Engine.ucs" {
			t.Errorf("$%s came from %q", id, entry.File)
		}
	}
}

// A unit's screen_name_id is exactly the value the RGD views show, so the whole
// chain is checked on real files: read the unit, take its id out of the RGD,
// and ask the table for the text.
func TestRealUnitsScreenNamesResolve(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)
	setLocale(gamePath, "", "English")

	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	necrons := cat.findRace("necrons")
	if necrons == nil {
		t.Fatalf("no necrons in %d races", len(cat.races))
	}

	examined, missing := 0, 0
	for _, entry := range necrons.unitsOf() {
		d := loadUnitDetails(tree, entry)
		if d.error != "" || d.name == "" {
			continue
		}
		// unitScreenName shows the display value, which is the id with the
		// resolved text appended.
		id, ok := rgd.LocID(strings.Fields(d.name)[0])
		if !ok {
			t.Errorf("unit %s: %q is not a localisation id", entry.name, d.name)
			continue
		}
		examined++
		if localisedString(id) == "" {
			missing++
			if missing <= 5 {
				t.Errorf("unit %s has screen_name_id $%s, which the English table does not carry",
					entry.name, id)
			}
		}
	}
	if examined == 0 {
		t.Fatal("no unit screen names were checked, the fixture changed shape")
	}
	t.Logf("%d necron screen names checked, %d missing from the table", examined, missing)
	if missing > examined/10 {
		t.Errorf("%d of %d necron screen names are missing, that is too many to be a translation gap", missing, examined)
	}
}

// A second language has to be a real translation rather than the English text
// repeated, and it comes from a different file.
func TestRealRussianTableDiffersFromEnglish(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)

	setLocale(gamePath, "", "English")
	english := localisedString("90100")
	englishEntry, _ := localeLookup("90100")
	setLocale(gamePath, "", "Russian")
	russian := localisedString("90100")
	russianEntry, _ := localeLookup("90100")

	if english == "" || russian == "" {
		t.Fatalf("English=%q Russian=%q", english, russian)
	}
	if english == russian {
		t.Errorf("both languages returned %q", english)
	}
	if !isCyrillic(russian) {
		t.Errorf("the Russian text is not Cyrillic: %q", russian)
	}
	if englishEntry.File == russianEntry.File {
		t.Errorf("both languages read %q", englishEntry.File)
	}
}

func isCyrillic(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}

// The picker has to offer what the installation actually ships, which is the
// engine's thirteen languages.
func TestRealInstallationListsItsLanguages(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)

	langs := availableLocales(gamePath, "")
	for _, want := range []string{"English", "Russian", "German", "French", "SChinese"} {
		if !containsFold(langs, want) {
			t.Errorf("%s missing from %d languages: %s", want, len(langs), strings.Join(langs, ", "))
		}
	}
	if len(langs) < 13 {
		t.Errorf("only %d languages found, the engine ships 13: %s", len(langs), strings.Join(langs, ", "))
	}
}

// The jump has to land on the id's line, so the line the index reports is read
// back out of the real file and compared.
func TestRealJumpLandsOnTheRightLine(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)
	setLocale(gamePath, "", "English")

	entry, ok := localeLookup("705754")
	if !ok {
		t.Fatal("705754 did not resolve")
	}
	data, err := os.ReadFile(entry.File)
	if err != nil {
		t.Fatalf("read %s: %v", entry.File, err)
	}
	lines := strings.Split(trimBOM(decodeUCS(data)), "\n")
	if entry.Line-1 >= len(lines) {
		t.Fatalf("line %d is past the end of a %d line file", entry.Line, len(lines))
	}
	line := strings.TrimSuffix(lines[entry.Line-1], "\r")
	if !strings.HasPrefix(line, "705754\tNecron Lord") {
		t.Errorf("line %d is %q, want it to start with 705754", entry.Line, line)
	}
}

// Building a table is the one call in the feature that reads 3.4 MB off the
// disk, and it happens while a language is being picked, so it has to finish in
// well under a frame budget's worth of waiting. The bound is deliberately loose
// — it is there to catch a build that reads the file repeatedly, not to police
// a few milliseconds on a busy machine.
func TestRealTableBuildsWithoutStalling(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)

	start := time.Now()
	table := buildLocaleTable(gamePath, "", "English")
	took := time.Since(start)
	t.Logf("indexed %d entries from %d files in %v", table.Lines, table.Files, took)

	if table.Lines < 20000 {
		t.Fatalf("only %d entries, the build did not read the whole table", table.Lines)
	}
	if took > 5*time.Second {
		t.Errorf("building the English table took %v, which would hang the language pick", took)
	}
}
