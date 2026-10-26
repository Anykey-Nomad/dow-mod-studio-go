//go:build windows

package app

import (
	"image/color"
	"testing"
)

// TestFadeTintToBackgroundKeepsTheEnds checks the two ends the fade is judged
// by: a mesh drawn whole is untouched, and one drawn at nothing is the panel.
func TestFadeTintToBackgroundKeepsTheEnds(t *testing.T) {
	tint := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	if got := fadeTintToBackground(tint, 1); got != tint {
		t.Errorf("a mesh drawn whole came out as %v, want %v", got, tint)
	}
	got := fadeTintToBackground(tint, 0)
	if got.R != panelBackground.R || got.G != panelBackground.G || got.B != panelBackground.B {
		t.Errorf("a mesh drawn at nothing came out as %v, want the panel %v", got, panelBackground)
	}
	// Halfway sits between the two, in the linear space the target is written
	// in - which is closer to the panel's byte value than a straight average
	// of the bytes would be, and further from black.
	half := fadeTintToBackground(tint, 0.5)
	if half.R <= panelBackground.R || half.R >= tint.R {
		t.Errorf("halfway came out as %v, between the panel %v and %v", half, panelBackground, tint)
	}
	if half.B >= half.G {
		t.Errorf("halfway came out as %v, which does not keep the tint's order", half)
	}
}
