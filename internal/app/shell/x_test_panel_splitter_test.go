package shell_test

import (
	"dow-mod-studio/internal/app/shell"
	"testing"
)

func TestPanelSplitterSlowDragKeepsWindowPointerCoordinate(t *testing.T) {
	const total, splitterWidth = 1200, 6
	for _, tc := range []struct {
		name  string
		index int
	}{
		{name: "left", index: 0},
		{name: "right", index: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left, right := 300, 400
			origin := shell.PanelSplitterLayoutOrigin(tc.index, total, splitterWidth, left, right)
			pressX := float32(origin + 3)
			startLeft, startRight := left, right

			// Advance in subpixel increments over separate layout frames, then
			// reverse direction. Each event position is local to the splitter's
			// most recently rendered origin, while the drag remains anchored to
			// its original press position in window coordinates.
			for _, delta := range []float32{0, .25, .5, .75, 1, 1.25, 1.5, 1.75, 2, 1.75, 1.5, 1.25, 1, .75, .5, .25, 0, -.25, -.5} {
				pointerX := pressX + delta
				localX := pointerX - float32(origin)
				left, right = shell.PanelSplitterWidthFromDrag(
					tc.index, float32(origin)+localX, pressX, startLeft, startRight,
				)
				if tc.index == 0 {
					want := startLeft + int(delta)
					if left != want || right != startRight {
						t.Fatalf("delta %.2f: widths=(%d,%d), want=(%d,%d)", delta, left, right, want, startRight)
					}
				} else {
					want := startRight - int(delta)
					if left != startLeft || right != want {
						t.Fatalf("delta %.2f: widths=(%d,%d), want=(%d,%d)", delta, left, right, startLeft, want)
					}
				}
				// Layout emits the new splitter position for the next frame.
				origin = shell.PanelSplitterLayoutOrigin(tc.index, total, splitterWidth, left, right)
			}
		})
	}
}

func TestPanelSplitterDragCalculatesFromPressState(t *testing.T) {
	const total, splitterWidth = 1200, 6
	const startLeft, startRight = 300, 400
	for _, tc := range []struct {
		name      string
		index     int
		localX    float32
		wantLeft  int
		wantRight int
	}{
		{"left divider rightward", 0, 13, 310, startRight},
		{"right divider rightward", 1, 13, startLeft, 390},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origin := shell.PanelSplitterLayoutOrigin(tc.index, total, splitterWidth, startLeft, startRight)
			pressX := float32(origin + 3)
			left, right := shell.PanelSplitterWidthFromDrag(tc.index, float32(origin)+tc.localX, pressX, startLeft, startRight)
			if left != tc.wantLeft || right != tc.wantRight {
				t.Fatalf("drag result left=%d right=%d; want left=%d right=%d", left, right, tc.wantLeft, tc.wantRight)
			}
		})
	}
}
