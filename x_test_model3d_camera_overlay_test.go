package main

import (
	"math"
	"testing"
)

// The corner gizmo exists so a screenshot says how it was taken. These pin the
// numbers it reports: they are read off the same yaw and pitch the view is
// drawn from, so an angle that disagrees with the picture makes the readout
// worse than no readout at all.

func optsAt(yaw, pitch float64) RenderOptions {
	return RenderOptions{Yaw: yaw, Pitch: pitch}
}

// The level views are the ones whose angles are exact, and they are the ones a
// user lands on most: yaw turns the camera about the up axis, and the models
// are Z up, so at pitch zero the camera looks along a horizontal axis and Z is
// always across the view at ninety degrees.
func TestCameraAxisAnglesOnTheLevelViews(t *testing.T) {
	cases := []struct {
		name string
		yaw  float64
		// the axis the camera looks straight along, and which way
		along  string
		angles [3]float64
	}{
		{"looking along -X", 0, "-X", [3]float64{180, 90, 90}},
		{"looking along -Y", math.Pi / 2, "-Y", [3]float64{90, 180, 90}},
		{"looking along +X", math.Pi, "+X", [3]float64{0, 90, 90}},
		{"looking along +Y", -math.Pi / 2, "+Y", [3]float64{90, 0, 90}},
	}
	for _, c := range cases {
		got := cameraAxesAngles(optsAt(c.yaw, 0))
		for i := 0; i < 3; i++ {
			if math.Abs(got[i]-c.angles[i]) > 0.5 {
				t.Errorf("%s: axis %d angle is %.1f degrees, want %.1f", c.name, i, got[i], c.angles[i])
			}
		}
	}
}

// The three angles are direction cosines, so their squares add to one. This is
// what catches a sign flipped in the orbit direction: every angle stays the
// right size and the squares still add up, but the ones reported for the
// axis the camera looks along become the mirror of what they should be, which
// the level-view cases above catch and this one holds true across the sphere.
func TestCameraAxisAnglesAreDirectionCosines(t *testing.T) {
	for _, yaw := range []float64{0, 0.7, 1.9, 3.4, 5.1} {
		for _, pitch := range []float64{-1.4, -0.6, 0, 0.4, 1.4} {
			a := cameraAxesAngles(optsAt(yaw, pitch))
			sum := 0.0
			for _, v := range a {
				r := v * math.Pi / 180
				sum += math.Cos(r) * math.Cos(r)
			}
			if math.Abs(sum-1) > 1e-6 {
				t.Errorf("yaw %.2f pitch %.2f: the squared cosines add to %.6f, want 1", yaw, pitch, sum)
			}
		}
	}
}

// The camera looks the other way from the orbit direction: the orbit goes out
// from the target towards the eye, and the view is back along it. Getting
// this backwards leaves every angle correct in size and wrong in sign.
func TestCameraLooksOppositeTheOrbit(t *testing.T) {
	for _, yaw := range []float64{0, 1.2, 2.8, 4.5} {
		for _, pitch := range []float64{-0.9, 0, 0.7} {
			orbit := cameraOrbitDirection(optsAt(yaw, pitch))
			view := cameraViewDirection(optsAt(yaw, pitch))
			dot := orbit.X*view.X + orbit.Y*view.Y + orbit.Z*view.Z
			if math.Abs(dot+1) > 1e-6 {
				t.Errorf("yaw %.2f pitch %.2f: view direction dot orbit is %.6f, want -1",
					yaw, pitch, dot)
			}
		}
	}
}

// Pitch is clamped short of the poles, so the camera can never look straight
// down an axis from above or below. The readout must respect that clamp
// rather than reporting a pole the picture cannot show.
func TestCameraAxisAnglesRespectThePitchClamp(t *testing.T) {
	const pole = math.Pi / 2
	a := cameraAxesAngles(optsAt(0, pole*10))
	for i, v := range a {
		if math.IsNaN(v) || v < 0 || v > 180 {
			t.Errorf("axis %d angle is %v, which is not a real angle", i, v)
		}
	}
	// A pitch far past the pole clamps to the same view as the clamp itself.
	clamped := cameraAxesAngles(optsAt(0, 1.5))
	past := cameraAxesAngles(optsAt(0, 4.0))
	for i := range clamped {
		if math.Abs(clamped[i]-past[i]) > 0.5 {
			t.Errorf("axis %d: a pitch past the clamp is %.1f, the clamp gives %.1f",
				i, past[i], clamped[i])
		}
	}
}

// A default view must produce something worth reading.
func TestCameraAxisAnglesAreSane(t *testing.T) {
	a := cameraAxesAngles(RenderOptions{})
	for i, v := range a {
		if math.IsNaN(v) || v < 0 || v > 180 {
			t.Errorf("axis %d angle is %v, which is not a real angle", i, v)
		}
	}
}
