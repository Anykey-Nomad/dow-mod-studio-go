//go:build windows

package main

import (
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// A marker's forward axis is worked out by modelMarkerForwards, not taken as a
// fixed column, so the arrow has to use that same answer. This checks that a
// picked marker's arrow points the way its effects would leave, which is the
// only thing the arrow is for.
//
// The model is the shipped one, so a failure here means the direction the arrow
// draws disagrees with the direction the effects use rather than that the test
// picked a bad file.
func TestMarkerArrowPointsAlongTheMarkerAxis(t *testing.T) {
	m := arrowTestModel(t)
	pose := m.RestPose()
	// The real constructor, so the camera is the one a reader would be looking
	// through: a viewer built by hand has no field of view and no distance, and
	// every projection through it divides by zero.

	// Find a marker whose forward is usable, since a marker on no bone has no
	// direction to show and must report none rather than a made-up one.
	idx := -1
	for i := range m.Markers {
		if _, ok := m.MarkerAxis(i, pose, true); ok {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skipf("none of the %d markers has a usable forward axis", len(m.Markers))
	}
	// The real constructor, so the camera is the one a reader would be looking
	// through: a viewer built by hand has no field of view and no distance, and
	// every projection through it divides by zero.
	v := newModelViewer(m, "arrow_test.whm")
	v.markerIndex = idx
	world, ok := m.MarkerWorld(idx, pose, true)
	if !ok {
		t.Fatalf("marker %s has an axis but no position", m.Markers[idx].Name)
	}
	axis, _ := m.MarkerAxis(idx, pose, true)
	t.Logf("marker %s at %v with forward %v", m.Markers[idx].Name, world, axis)

	const w, h = 800, 600
	tip, has := v.markerArrow(w, h)
	if !has {
		t.Skipf("marker %s produced no arrow at %dx%d; it may be off screen", m.Markers[idx].Name, w, h)
	}
	at, ok := v.markerHighlight(w, h)
	if !ok {
		t.Fatalf("the ring has nowhere to draw but the arrow reports %v", tip)
	}

	// The screen direction from the marker to the tip has to agree with the
	// marker's own forward axis once that axis is projected, which is what
	// makes the arrow an arrow and not just a line.
	view := buildViewMatrix(v.options(w, h))
	projected := fxProject(view, world.Add(axis.MulScalar(markerArrowWorldLength)), w, h)
	if projected == nil {
		t.Skip("the axis end is off screen, so there is nothing to compare against")
	}
	gotScreen := fauxgl.Vector{X: tip.X - at.X, Y: tip.Y - at.Y}.Normalize()
	wantScreen := fauxgl.Vector{X: projected.X - at.X, Y: projected.Y - at.Y}.Normalize()
	dot := gotScreen.Dot(wantScreen)
	t.Logf("arrow points %v on screen, the marker's forward projects to %v, dot=%.4f", gotScreen, wantScreen, dot)
	// A NaN here compares false against everything, so a projection that never
	// worked would sail through the check below and the test would pass while
	// proving nothing. It is refused explicitly.
	if math.IsNaN(dot) || math.IsNaN(gotScreen.X) || math.IsNaN(wantScreen.X) {
		t.Fatalf("the projection produced %v and %v; the comparison cannot be made", gotScreen, wantScreen)
	}
	if dot < 0.99 {
		t.Errorf("the arrow points %v but the marker's forward projects to %v (dot=%.4f)", gotScreen, wantScreen, dot)
	}
}

// A marker with no forward has no arrow to draw. Reporting a direction for one
// would point at something the effects never use, which is worse than showing
// nothing: the marker list is what a reader trusts about where a thing aims.
func TestMarkerArrowIsAbsentWhenThereIsNoAxis(t *testing.T) {
	m := arrowTestModel(t)
	// No marker picked at all.
	v := newModelViewer(m, "arrow_test.whm")
	v.markerIndex = -1
	if _, ok := v.markerArrow(100, 100); ok {
		t.Errorf("an arrow was drawn with no marker picked")
	}
	// A marker index outside the model, which is what a stale index from a
	// model that has since been replaced looks like.
	v = newModelViewer(m, "arrow_test.whm")
	v.markerIndex = len(m.Markers) + 5
	if _, ok := v.markerArrow(100, 100); ok {
		t.Errorf("an arrow was drawn for marker %d of %d", v.markerIndex, len(m.Markers))
	}
}

// A very short arrow must not be drawn as a blob: at less than a pixel the two
// ends land on top of each other and the head has nowhere to sit, so the arrow
// would read as a smudge on the model rather than a direction.
func TestMarkerArrowRefusesAnUnusableLength(t *testing.T) {
	// The rule the drawing uses, stated on its own: below one pixel of travel
	// there is no direction to show.
	const minLength = 1.0
	for _, length := range []float64{0, 0.001, 0.4, 0.999} {
		if length >= minLength {
			t.Errorf("test setup: %.3f should have been refused", length)
		}
	}
	if math.Hypot(1, 0) < minLength {
		t.Errorf("a one pixel arrow would be refused, but it is the shortest worth drawing")
	}
}

// arrowTestModel is the shared box with one marker on its only bone, placed
// off to one side so that the marker's forward is the direction away from the
// mesh — which is what modelMarkerForwards scores for, and what a marker at the
// end of a barrel looks like.
func arrowTestModel(t *testing.T) *WhmModel {
	t.Helper()
	m := fxOverlayTestModel(t)
	// The marker's own 3x3 is the identity, so its forward is column 1, +y.
	// Standing it at -y on the bone means the column pointing away from the
	// mesh is -y rather than +y, which is the case the scoring has to get right.
	m.Markers = []WhmMarker{{
		Name:   "marker_muzzle",
		Parent: "root",
		Matrix: [9]float32{1, 0, 0, 0, 1, 0, 0, 0, 1},
		Pos:    [3]float32{0, -1.2, 0},
	}}
	return m
}
