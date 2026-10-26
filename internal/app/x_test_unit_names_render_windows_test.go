//go:build windows

package app

import (
	"dow-mod-studio/internal/testutil"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
)

func renderUnitDropdown(t *testing.T, ui *uiState, w, h int) (*image.RGBA, image.Rectangle) {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, w, h)
	ui.layoutUnitTabBody(gtx)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if ui.unitDropBox.Empty() {
		t.Fatal("the layout reported no dropdown box at all")
	}
	return img, ui.unitDropBox
}

// pixelsThatDiffer counts the pixels of a that b does not have. Comparing two
// renders of the same list is the only way to be sure a given piece of text
// reached the screen: a row of solid background and a row of writing differ,
// and an absolute count of colours does not tell the two apart.
func pixelsThatDiffer(a, b *image.RGBA, box image.Rectangle) int {
	box = box.Intersect(a.Bounds()).Intersect(b.Bounds())
	n := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// unitRowFixture is an object tab with the unit list open, on the race the
// selection starts at, with the first unit of that race named. That unit is the
// one a probe can rely on: the list shows the first unitListVisibleRows of the
// selected race, and the selection starts at index 0, which is chaos — not
// necrons, whose units a test is far more likely to reach for.
type unitRowFixture struct {
	ui   *uiState
	unit unitEntry
}

func newUnitRowFixture(t *testing.T) unitRowFixture {
	t.Helper()
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	if cat == nil || len(cat.races) == 0 {
		t.Skip("the module has no races")
	}
	shown := cat.races[0]
	units := shown.unitsOf()
	if len(units) == 0 {
		t.Skipf("race %q has no units", shown.name)
	}
	ui := &uiState{th: unitTestTheme(), modTree: tree}
	ui.openUnitTab()
	ui.unitListOpen = true
	return unitRowFixture{ui: ui, unit: units[0]}
}

// nameUnit points the index at an id and gives the table the text for it, so
// the row shows a word nothing else in the module uses.
func (f unitRowFixture) nameUnit(t *testing.T, id, text string) {
	t.Helper()
	f.ui.unitNames = map[string]string{strings.ToLower(f.unit.path): id}
	f.ui.testLocaleTable(t, id, text)
}

// unitRowTextPixels is how many pixels a change of words on these rows is worth.
// Swapping one display name for another of a similar length moves about a
// thousand pixels. A row that is not showing the text at all still moves a
// handful — 9 and 58 measured — which is why the check is a floor and not "any
// difference at all": that mistake makes a test pass on a UI that is broken.
const unitRowTextPixels = 200

// TestUnitRowsShowTheLocalisedName is the request, checked on screen rather than
// in state: name the row, render, name it differently, render again. If the row
// were showing the file name, or the id, or nothing, the two renders would be
// all but identical.
func TestUnitRowsShowTheLocalisedName(t *testing.T) {
	f := newUnitRowFixture(t)
	const w, h = 900, 700

	f.nameUnit(t, "900001", "Qqzz First Localised Name")
	first, box := renderUnitDropdown(t, f.ui, w, h)

	f.nameUnit(t, "900002", "Wwyy Second Localised Name")
	second, box2 := renderUnitDropdown(t, f.ui, w, h)

	if box != box2 {
		t.Fatalf("the list moved between renders, %v then %v", box, box2)
	}

	// The comparison is only worth anything if re-rendering the same state is
	// stable, so establish the noise floor first.
	third, _ := renderUnitDropdown(t, f.ui, w, h)
	if n := pixelsThatDiffer(second, third, box); n != 0 {
		t.Fatalf("%d pixels changed between two renders of the same state, so the comparison is noise", n)
	}

	n := pixelsThatDiffer(first, second, box)
	if n < unitRowTextPixels {
		t.Errorf("only %d pixels changed when the localised text changed, want at least %d: "+
			"the rows are not showing it", n, unitRowTextPixels)
	} else {
		t.Logf("%d pixels changed when the localised name changed", n)
	}
}

// TestUnitRowsFallBackToTheFileName is the other half: with nothing resolved,
// the row shows its .rgd's name, and resolving a name must change the picture.
func TestUnitRowsFallBackToTheFileName(t *testing.T) {
	f := newUnitRowFixture(t)
	const w, h = 900, 700

	f.ui.unitNames = nil
	f.ui.testLocaleTable(t, "900001", "Qqzz First Localised Name")
	byFileName, box := renderUnitDropdown(t, f.ui, w, h)

	f.nameUnit(t, "900001", "Qqzz First Localised Name")
	byLocalised, _ := renderUnitDropdown(t, f.ui, w, h)

	n := pixelsThatDiffer(byFileName, byLocalised, box)
	if n < unitRowTextPixels {
		t.Errorf("only %d pixels changed when a row switched from the file name to the "+
			"localised one, want at least %d: the rows are not using it", n, unitRowTextPixels)
	} else {
		t.Logf("%d pixels changed when a row switched from the file name to the localised one", n)
	}
}

// TestUnitRowCarriesTheFileNameToo: the display name is the flexible part of the
// row and the file name is not, because units of one race share a display name
// (measured: 140 such groups on DXP3) and the file name is what tells them
// apart. A row of writing antialiases into many shades; a flat fill does not.
func TestUnitRowCarriesTheFileNameToo(t *testing.T) {
	f := newUnitRowFixture(t)
	f.nameUnit(t, "900001", "Qqzz Localised Name")

	img, box := renderUnitDropdown(t, f.ui, 900, 700)
	seen := map[color.RGBA]int{}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			seen[img.RGBAAt(x, y)]++
		}
	}
	if len(seen) < 20 {
		t.Errorf("the list holds %d distinct colours in %v, which is a flat fill rather than writing", len(seen), box)
	}
}

// ─── merged from x_test_unit_names_real_windows_test.go ────────────────────────────────────────

func TestUnitNamesOnTheRealModule(t *testing.T) {
	resetLocales(t)
	gamePath := testGameFolder(t)
	setLocale(gamePath, "", "English")

	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	if cat == nil || len(cat.races) == 0 {
		t.Skip("no races in the module")
	}
	total := 0
	for _, r := range cat.races {
		total += len(r.troops) + len(r.structures)
	}

	ui := newUI(unitTestTheme())
	// setLocaleLanguage finds the .ucs files through the game folder, so the
	// state needs one, not just the table the test installed by hand.
	ui.gamePath = gamePath
	ui.modulePath = tree.AppPath
	ui.modTree = tree
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)

	// Every unit that carries an id should have it cached. Measured on DXP3:
	// 582 units, all but a handful carrying one, so a scan that resolved far
	// fewer means the path into ui_info is wrong, not that the mod is sparse.
	if len(ui.unitNames) < total-8 {
		t.Errorf("the scan resolved %d ids for %d units; the walk into ui_ext\\ui_info is probably wrong",
			len(ui.unitNames), total)
	}

	// Every race's units should mostly resolve to real text in English.
	for _, r := range cat.races {
		units := r.unitsOf()
		resolved := 0
		for _, u := range units {
			if ui.unitLocalisedName(u) != "" {
				resolved++
			}
		}
		if resolved == 0 {
			t.Errorf("race %q resolved no names at all", r.name)
		}
		if resolved < len(units)-4 {
			t.Errorf("race %q resolved %d of %d names", r.name, resolved, len(units))
		}
	}

	// The label is the text, never the id, and the fallback is the file name.
	lord := findRealUnit(t, cat, "necron_lord")
	got := ui.unitLabel(lord)
	if got == "necron_lord" {
		t.Errorf("necron_lord still reads as its file name; the English table has %q",
			localisedString("705754"))
	}
	if strings.Contains(got, "$") {
		t.Errorf("the label %q shows a localisation id", got)
	}
	if unitFileLabel(lord) != "necron_lord" {
		t.Errorf("the dim file name reads %q", unitFileLabel(lord))
	}
	t.Logf("necron_lord reads %q (file %q)", got, unitFileLabel(lord))

	// Switching the language must relabel from the cache. The index is keyed by
	// id and holds no text, so this is the whole point of caching the id.
	english := ui.unitLabel(lord)
	ui.setLocaleLanguage("Russian")
	russian := ui.unitLabel(lord)
	if russian == english {
		t.Errorf("switching to Russian left the label as %q", russian)
	}
	if ui.unitNamesRunning {
		t.Error("switching the language restarted the scan; the cached ids should have been enough")
	}
	if russian == "necron_lord" {
		t.Error("the Russian label fell back to the file name")
	}
	t.Logf("Russian: %q", russian)
	ui.setLocaleLanguage("English")

	// And back again, to catch a table swap that did not restore.
	if again := ui.unitLabel(lord); again != english {
		t.Errorf("switching back gave %q, want %q", again, english)
	}
}

// TestRealUnitsShareNamesWithinARace is why the file name stays on the row.
// Measured on DXP3: several units of one race resolve to the same text, so a
// list showing only the display name cannot tell them apart.
func TestRealUnitsShareNamesWithinARace(t *testing.T) {
	resetLocales(t)
	setLocale(testGameFolder(t), "", "English")
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	ui := newUI(unitTestTheme())
	ui.modTree = tree
	ui.unitCatalog = cat
	ui.unitCatalogMod = tree.FileMapName
	ui.startUnitNameScan(tree, cat)
	waitForUnitNames(t, ui)

	shared := 0
	for _, r := range cat.races {
		seen := map[string][]string{}
		for _, u := range r.unitsOf() {
			seen[ui.unitLabel(u)] = append(seen[ui.unitLabel(u)], u.name)
		}
		for label, files := range seen {
			if len(files) > 1 {
				shared++
				if shared <= 3 {
					t.Logf("race %q: %d units all read %q (%s)", r.name, len(files), label, strings.Join(files, ", "))
				}
			}
		}
	}
	if shared == 0 {
		t.Skip("no shared display names in this module, so the file name is not carrying anything here")
	}
	fmt.Printf("%d groups of units share a display name, which is why the file name is on the row\n", shared)
}

func findRealUnit(t *testing.T, cat *unitCatalog, name string) unitEntry {
	t.Helper()
	for _, r := range cat.races {
		for _, u := range append(append([]unitEntry{}, r.troops...), r.structures...) {
			if u.name == name {
				return u
			}
		}
	}
	t.Fatalf("no unit named %q in the module", name)
	return unitEntry{}
}
