package app

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/testutil"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
)

func countColoursIn(img *image.RGBA, box image.Rectangle) int {
	box = box.Intersect(img.Bounds())
	if box.Empty() {
		return 0
	}
	seen := map[color.RGBA]bool{}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			seen[img.RGBAAt(x, y)] = true
		}
	}
	return len(seen)
}

// renderLocaleOverlay draws the open language list on its own and hands back
// the screenshot, so a test can look at what was actually painted.
func renderLocaleOverlay(t *testing.T, ui *uiState, w, h int) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	ui.layoutLocaleOverlay(gtx)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

func localeTestUI(t *testing.T) *uiState {
	t.Helper()
	resetLocales(t)
	gamePath, moduleDir := writeUCS(t, map[string][]byte{
		"Engine/Locale/English/Engine.ucs": encodeUCS(t, "90100\tArmour"),
		"Engine/Locale/Russian/Engine.ucs": encodeUCS(t, "90100\tБроня"),
		"Engine/Locale/German/Engine.ucs":  encodeUCS(t, "90100\tRüstung"),
	})
	ui := newUI(unitTestTheme())
	ui.gamePath = gamePath
	ui.modulePath = moduleDir
	ui.refreshLocales()
	return ui
}

// The open list has to actually write the language names. Counting colours
// inside the box is not enough to tell: the border, the drop shadow and the
// rows' own tints add up to a convincing number of shades around a list that
// says nothing at all, which is exactly how this shipped.
//
// So the rows are compared against the same list with the names taken away. A
// name that reaches the screen changes the row; a name painted over by the
// row's own fill changes nothing, and the two shots come out identical.
func TestLocaleDropdownWritesTheLanguageNames(t *testing.T) {
	const w, h = 900, 700
	ui := localeTestUI(t)
	ui.localeOpen = true

	img := renderLocaleOverlay(t, ui, w, h)

	box := ui.localeDropBox
	if box.Empty() {
		t.Fatal("the layout reported no dropdown box at all")
	}
	if box.Max.X > w || box.Max.Y > h {
		t.Errorf("the dropdown box %v runs past the %dx%d window", box, w, h)
	}
	// Three languages in the fixture, so three rows plus the panel's own frame.
	// The frame is part of the height: a panel sized for the rows alone clips
	// the last one by however much the border and the inset take.
	wantH := 3*localeRowHeightDp + 2*(localeListPadDp+localeListBorderDp)
	if box.Dy() != wantH {
		t.Errorf("the dropdown is %d px tall, want %d for 3 rows and its frame", box.Dy(), wantH)
	}
	// The list hangs below the menu bar, not over it.
	if box.Min.Y < 24 {
		t.Errorf("the dropdown starts at y=%d, on top of the menu bar", box.Min.Y)
	}

	// The same list with every name blanked out: only the writing can differ.
	names := append([]string(nil), ui.localeList...)
	for i := range ui.localeList {
		ui.localeList[i] = ""
	}
	blank := renderLocaleOverlay(t, ui, w, h)
	for i := range ui.localeList {
		ui.localeList[i] = names[i]
	}

	if diff := imagesDifferIn(img, blank, box); diff == 0 {
		t.Fatal("the list is pixel for pixel the same with the language names removed: " +
			"nothing is being written, the row fill is painted over the text")
	} else {
		t.Logf("%d pixels of the %d px box change when the names are removed", diff, box.Dx()*box.Dy())
	}

	// And the same question asked one name at a time, so one working row cannot
	// carry the test on its own. Each is compared over the whole box rather than
	// over a guessed band: a name that is not drawn at all cannot change
	// anything anywhere in it, and no row geometry has to be worked out here.
	for i := range names {
		langs := append([]string(nil), names...)
		langs[i] = ""
		ui.localeList = langs
		oneBlank := renderLocaleOverlay(t, ui, w, h)
		ui.localeList = names

		if imagesDifferIn(img, oneBlank, box) == 0 {
			t.Errorf("nothing changes when %q is removed: row %d is not being drawn", names[i], i)
		}
	}
}

// containsColor reports whether a box holds a pixel close enough to want. It is
// how a piece of text is looked for directly, rather than by how much a region
// differs from another region — comparing two regions cannot tell a label that
// is drawn from a fill that merely came out a different width.
func containsColor(img *image.RGBA, box image.Rectangle, want color.NRGBA, tol int) bool {
	box = box.Intersect(img.Bounds())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if absDiff(p.R, want.R) <= tol && absDiff(p.G, want.G) <= tol && absDiff(p.B, want.B) <= tol {
				return true
			}
		}
	}
	return false
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// The field says which language is in force, and it was drawn the same way the
// rows were: the fill went down after the labels and hid them, so the field came
// out as a solid block. The name is written in cText over a dark background, so
// the field either has pixels of that colour in it or it has nothing on it.
func TestLocaleFieldWritesTheChosenLanguage(t *testing.T) {
	const w, h = 900, 700
	ui := localeTestUI(t)
	ui.localeOpen = false

	img := renderFrame(t, w, h, func(gtx layout.Context) {
		gtx.Constraints.Min = image.Point{}
		ui.layoutLocaleField(gtx)
	})

	// The field is wider than it looks, so the box is generous; what matters is
	// that the glyphs are somewhere inside it.
	box := image.Rectangle{Max: image.Pt(300, localeFieldHeightDp)}
	if !containsColor(img, box, cText, 12) {
		t.Errorf("no text-coloured pixels in the field: %q is not being drawn", ui.localeLabel())
	} else {
		t.Logf("the field carries text-coloured pixels, so %q is written in it", ui.localeLabel())
	}
}

// The list is capped to the window: a language folder with dozens of entries
// must not run off the bottom and cut the last rows in half.
func TestLocaleDropdownStaysInsideTheWindow(t *testing.T) {
	const w, h = 500, 300
	ui := localeTestUI(t)
	// Far more languages than any installation has.
	ui.localeList = make([]string, 60)
	for i := range ui.localeList {
		ui.localeList[i] = "Language" + string(rune('A'+i%26))
	}
	ui.localeOpen = true

	ui.ensureLocaleClicks(len(ui.localeList))
	renderLocaleOverlay(t, ui, w, h)

	box := ui.localeDropBox
	if box.Empty() {
		t.Fatal("the layout reported no dropdown box at all")
	}
	if box.Max.Y > h {
		t.Errorf("the dropdown box %v runs %d px past the bottom of a %d px window", box, box.Max.Y-h, h)
	}
	if box.Dy() >= h {
		t.Errorf("the dropdown is %d px tall in a %d px window, so it was not capped", box.Dy(), h)
	}
}

// The whole point of the picker is to change what the RGD values show, so the
// chosen language has to reach the resolved text, not just the label.
func TestPickingALanguageChangesTheResolvedText(t *testing.T) {
	ui := localeTestUI(t)

	if got := rgd.LocIDDisplay("$90100"); got != "$90100  Armour" {
		t.Fatalf("English shows %q", got)
	}
	ui.setLocaleLanguage("Russian")
	if got := rgd.LocIDDisplay("$90100"); got != "$90100  Броня" {
		t.Errorf("Russian shows %q, want the Russian text", got)
	}
	ui.setLocaleLanguage("German")
	if got := rgd.LocIDDisplay("$90100"); got != "$90100  Rüstung" {
		t.Errorf("German shows %q, want Rüstung", got)
	}
}

// The field is the only clue which language is in force when a value shows no
// text, so it has to name the chosen one and stay inside the menu bar.
func TestLocaleFieldShowsTheChosenLanguage(t *testing.T) {
	ui := localeTestUI(t)
	ui.setLocaleLanguage("Russian")
	if got := ui.localeLabel(); got != "Russian" {
		t.Errorf("label = %q, want Russian", got)
	}
}

// The resolved text lives on a row under the value, and Alt+click on that row
// is the jump to the .ucs. Both halves matter: without the row there is
// nothing to click, and without the tag on it the press cannot be told apart
// from an ordinary one.
func TestLocaleRowShowsTheTextAndRegistersTheAltClick(t *testing.T) {
	const w, h = 600, 80
	ui := localeTestUI(t)
	tab := &fileTab{path: "unit.rgd", kind: "text"}
	e := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$90100"}

	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	dims := ui.tableRowLocaleText(gtx, tab, e, "GameData/screen_name_id", false)

	if dims.Size.Y == 0 {
		t.Fatal("the locale row took no height, so nothing is drawn or clickable")
	}
	tag := ui.rgdEnsureLocTag(tab, "GameData/screen_name_id")
	if tag == nil {
		t.Fatal("the row registered no pointer target, so Alt+click cannot reach it")
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	box := image.Rectangle{Max: image.Pt(w, dims.Size.Y)}
	if colours := countColoursIn(img, box); colours < 5 {
		t.Errorf("only %d distinct colours in the row, so no text was drawn", colours)
	}
}

// An id that the chosen language does not carry must say which language is
// missing it, rather than showing a blank row that reads like a bug.
func TestLocaleRowSaysWhenTheTableHasNoEntry(t *testing.T) {
	ui := localeTestUI(t)
	e := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "$404"}

	// The message a reader gets is the one that has to name the language.
	text, tooltip := ui.localeRowDisplay(e.DisplayValue())
	if text == "" {
		t.Fatal("an unknown id produced no label at all")
	}
	if !strings.Contains(text, "English") {
		t.Errorf("label = %q, want it to name the language that is missing the id", text)
	}
	if !strings.Contains(tooltip, "404") {
		t.Errorf("tooltip = %q, want it to name the id", tooltip)
	}
	// There is no line to jump to, so the row must not offer one. It does still
	// take a pointer target: a click on a field with no text is how that field
	// gets a string written for it, which is the whole point of the row being
	// there at all.
	if _, ok := localeLookup("404"); ok {
		t.Fatal("404 should not be in the table")
	}
	tab := &fileTab{path: "unit.rgd", kind: "text"}
	gtx := testutil.NewLayoutContext(new(op.Ops), 400, 60)
	ui.tableRowLocaleText(gtx, tab, e, "GameData/screen_name_id", false)
	if len(tab.rgdLocTags) == 0 {
		t.Error("a field with no text registered no pointer target, so a click on it could not add one")
	}
	// Alt on it asks for a jump, and there is nowhere to jump to, so the rule
	// the row applies must not answer "jump". The press itself is not simulated
	// here — driving Gio's router needs a window, which a headless render does
	// not have — so what is checked is the decision the row makes for this id,
	// which is the part that can be wrong.
	hasText := localisedString("404") != ""
	if got := localeRowActionFor(hasText, true, false); got == localeRowJump {
		t.Errorf("Alt on a field with no text asks to jump to $404, which is not in any table")
	}
	if got := localeRowActionFor(hasText, false, false); got != localeRowCreate {
		t.Errorf("a plain click on a field with no text asks for %v, want a create", got)
	}
}

// An unset slot is not a missing translation, and saying so stops a reader going
// looking for a table entry that was never meant to exist. It is also the one
// case a click can fix outright, so the label says that too.
func TestLocaleRowSaysWhenTheSlotIsUnset(t *testing.T) {
	ui := localeTestUI(t)
	text, tooltip := ui.localeRowDisplay("$0")
	if !strings.Contains(text, "no localisation") {
		t.Errorf("label = %q, want it to say the field has no localisation", text)
	}
	if !strings.Contains(text, "click") {
		t.Errorf("label = %q, want it to say a click adds one", text)
	}
	if !strings.Contains(tooltip, "$0") {
		t.Errorf("tooltip = %q, want it to explain the marker", tooltip)
	}
}

// A value that is not a localisation id has no row to show at all.
func TestLocaleRowIsSkippedForANonIDValue(t *testing.T) {
	ui := localeTestUI(t)
	tab := &fileTab{path: "unit.rgd", kind: "text"}
	e := &rgd.RgdEntry{Name: "screen_name_id", Type: rgd.RgdTypeWString, WStrVal: "not an id"}

	gtx := testutil.NewLayoutContext(new(op.Ops), 400, 60)
	dims := ui.tableRowLocaleText(gtx, tab, e, "GameData/screen_name_id", false)
	if dims.Size.Y != 0 {
		t.Errorf("row took %d px, want nothing for a value that is not an id", dims.Size.Y)
	}
}

// ─── merged from x_test_locale_real_windows_test.go ────────────────────────────────────────

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
	necrons := cat.FindRace("necrons")
	if necrons == nil {
		t.Fatalf("no necrons in %d races", len(cat.Races))
	}

	examined, missing := 0, 0
	for _, entry := range necrons.UnitsOf() {
		d := loadUnitDetails(tree, entry)
		if d.Error != "" || d.Name == "" {
			continue
		}
		// unitScreenName shows the display value, which is the id with the
		// resolved text appended.
		id, ok := rgd.LocID(strings.Fields(d.Name)[0])
		if !ok {
			t.Errorf("unit %s: %q is not a localisation id", entry.Name, d.Name)
			continue
		}
		examined++
		if localisedString(id) == "" {
			missing++
			if missing <= 5 {
				t.Errorf("unit %s has screen_name_id $%s, which the English table does not carry",
					entry.Name, id)
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
