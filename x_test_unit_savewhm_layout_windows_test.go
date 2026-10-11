package main

import (
	"testing"

	"gioui.org/widget"
)

// With the bone list on show and the model edited, Save WHM has to be drawn on
// screen: it is the only way the edited keys reach the .whm, and a panel that
// lays it out below its own bottom edge leaves it unreachable.
func TestSaveWHMIsOnScreenInTheBonePanel(t *testing.T) {
	ui, f := unitViewportUI(t, "necron_listening_post", 1200, 760)
	if !unitViewportLoad(t, ui, f, false) {
		t.Fatalf("the model of the listening post never loaded: %q", ui.unitModelErr)
	}
	ui.unitListMode = 2
	ui.unitModelWhmDirty = true
	unitViewportFrames(f, 3)
	// The walk fails the test when the button is never hovered on screen.
	unitViewportSideButton(t, f, []*widget.Clickable{&ui.unitBoneKeySaveClick}, &ui.unitBoneKeySaveClick, "Save WHM")
}
