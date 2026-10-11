package main

import (
	"dow-mod-studio/internal/rgd"
	"image"
	"image/color"
	"strings"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
)

// countColoursIn counts the distinct colours inside a box. It is how a frame
// that drew text is told from a frame that drew a flat background: the border
// and the fill alone look the same either way, which is how an empty list once
// shipped looking like a correctly framed one.
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
	gtx := newLayoutContextForTest(&ops, w, h)
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
	gtx := newLayoutContextForTest(&ops, w, h)
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
	gtx := newLayoutContextForTest(new(op.Ops), 400, 60)
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

	gtx := newLayoutContextForTest(new(op.Ops), 400, 60)
	dims := ui.tableRowLocaleText(gtx, tab, e, "GameData/screen_name_id", false)
	if dims.Size.Y != 0 {
		t.Errorf("row took %d px, want nothing for a value that is not an id", dims.Size.Y)
	}
}
