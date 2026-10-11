//go:build windows

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

// renderTo lays a widget out in a headless window and reads the pixels back.
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
