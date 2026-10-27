package app

import (
	"dow-mod-studio/internal/testutil"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
)

// The file dialog lists the files it found. Its rows have to be on screen and
// take the pointer: a dialog that counts files but draws none leaves nothing
// to click.
func TestOpenModuleListRowsAreOnScreen(t *testing.T) {
	const w, h = 1076, 1070
	dir := t.TempDir()
	for _, name := range []string{"DoWDE.module", "Alt.module", "Third.module"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ui := newUI(testutil.UnitTestTheme())
	OpenModulePicker(ui)
	FpBrowse(ui, dir)
	f := newWindowFrame(ui, w, h)

	found := false
	// The rows sit in the left part of the card, under the path field. A
	// column of positions there finds them without a frame per pixel.
	for y := 150; y < 800 && !found; y += 5 {
		f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(60, float32(y))})
		f.frame()
		for i := range ui.FilePick.Clicks {
			if ui.FilePick.Clicks[i].Hovered() {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no row of the %d-row list takes the pointer: the list is not drawn", len(ui.FilePick.Rows))
	}
}
