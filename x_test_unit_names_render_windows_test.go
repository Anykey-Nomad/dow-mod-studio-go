//go:build windows

package main

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
)

// renderUnitDropdown lays the object tab out with the unit list open and reads
// the pixels back. It returns the image and the box the list landed in, which
// the layout records itself, so a probe never has to guess where the rows are.
func renderUnitDropdown(t *testing.T, ui *uiState, w, h int) (*image.RGBA, image.Rectangle) {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := newLayoutContextForTest(&ops, w, h)
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
