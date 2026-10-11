package whm

import (
	"testing"
)

// TestMeshAlphaReadsTheForceFlag checks what the flag decides. It is the value
// a mesh is held at while a curve has not reached it, so a track with no curve
// leaves the mesh alone: the restored monolith's idle, aim, fire and death
// clips carry a flag of zero on every one of their meshes and every one of
// them is on screen the whole clip.
func TestMeshAlphaReadsTheForceFlag(t *testing.T) {
	held := &MeshTrack{Name: "body", ForceInvisible: true}
	if a := meshTrackAlpha(held, 0.5); a != 1 {
		t.Errorf("a mesh held back with no curve drew at %.2f, not whole", a)
	}
	shown := &MeshTrack{Name: "body"}
	if a := meshTrackAlpha(shown, 0.5); a != 1 {
		t.Errorf("a mesh with no curve and no flag drew at %.2f, not whole", a)
	}
	// With a curve it is the value the mesh starts at: a phase_in holds the
	// model back until its first key lets it out.
	phaseIn := &MeshTrack{
		Name:           "monolith_body",
		ForceInvisible: true,
		Keys:           []Key3{{Time: 0.01, V: [3]float32{1}}},
	}
	if a := meshTrackAlpha(phaseIn, 0); a != 0 {
		t.Errorf("before its first key a held mesh drew at %.2f, not nothing", a)
	}
	if a := meshTrackAlpha(phaseIn, 0.02); a != 1 {
		t.Errorf("after its first key it drew at %.2f, not whole", a)
	}
}

// TestMeshAlphaReadsTheCurve checks the fade itself: the values between two
// keys, the values either side of them, and what the flag means before the
// first key.
func TestMeshAlphaReadsTheCurve(t *testing.T) {
	tr := &MeshTrack{
		Name:           "nec_listening_post",
		ForceInvisible: true,
		Keys: []Key3{
			{Time: 0.25, V: [3]float32{0}},
			{Time: 0.5, V: [3]float32{1}},
		},
	}
	cases := []struct {
		at   float32
		want float32
		why  string
	}{
		{0, 0, "before the first key the flag still holds it back"},
		{0.25, 0, "on the first key"},
		{0.375, 0.5, "half way between one and none"},
		{0.5, 1, "on the last key"},
		{1, 1, "past the last key it holds the last value"},
	}
	for _, c := range cases {
		if got := meshTrackAlpha(tr, c.at); got != c.want {
			t.Errorf("t=%.3f: got %.3f, want %.3f - %s", c.at, got, c.want, c.why)
		}
	}
	// A value outside the range is not something to draw: it is zero, the
	// mesh is not there.
	over := &MeshTrack{Keys: []Key3{{Time: 0, V: [3]float32{1.4}}, {Time: 1, V: [3]float32{-2}}}}
	if got := meshTrackAlpha(over, 0.5); got != 0 {
		t.Errorf("a curve that leaves 0..1 drew at %.2f", got)
	}
}

// TestMeshAlphasLeavesTheCommonCaseEmpty checks that a clip which draws
// everything hands back nothing at all: the map is what a renderer looks a
// mesh up in, and a model with no hidden or fading meshes in the clip on show
// must not pay for one every frame.
func TestMeshAlphasLeavesTheCommonCaseEmpty(t *testing.T) {
	m := &Model{
		Meshes: []Mesh{{Name: "body"}, {Name: "gun"}},
		Animations: []Animation{
			{Name: "idle", Meshes: []MeshTrack{
				{Name: "body", ForceInvisible: true},
				{Name: "gun"},
			}},
			{Name: "phase_in", Meshes: []MeshTrack{
				{Name: "body", ForceInvisible: true, Keys: []Key3{{Time: 0.01, V: [3]float32{1}}}},
				{Name: "gun", ForceInvisible: true, Keys: []Key3{{Time: 1, V: [3]float32{0}}}},
			}},
		},
	}
	if got := m.MeshAlphas("idle", 0.5); got != nil {
		t.Errorf("a clip that draws everything produced %v", got)
	}
	if got := m.MeshAlphas("phase_in", 0); len(got) != 2 || got["body"] != 0 || got["gun"] != 0 {
		t.Errorf("phase_in at its first frame reported %v, want both meshes still held back", got)
	}
	got := m.MeshAlphas("phase_in", 0.5)
	if len(got) != 1 || got["gun"] != 0 {
		t.Errorf("phase_in half way reported %v, want only the gun held back until its key", got)
	}
	if got := m.MeshAlphas("no such clip", 0.5); got != nil {
		t.Errorf("a clip the model does not have produced %v", got)
	}
	// The name is matched the way a timeline spells it, in lower case.
	if got := m.MeshAlphas("PHASE_IN", 0.5); len(got) != 1 {
		t.Errorf("a clip named in another case reported %v", got)
	}
}

// TestMeshAlphaByteKeepsTheEnds checks the byte an alpha travels in. Both ends
// have to come out exact: a mesh the clip has let go of has to be the one the
// shader multiplies by one, and a mesh held back has to be a zero rather than a
// shade of one.
func TestMeshAlphaByteKeepsTheEnds(t *testing.T) {
	for _, tc := range []struct {
		alpha float32
		want  uint8
	}{
		{alpha: 0, want: 0},
		{alpha: 1, want: 255},
		{alpha: -0.4, want: 0},
		{alpha: 3, want: 255},
		{alpha: 0.5, want: 128},
		{alpha: 0.002, want: 1},
	} {
		if got := AlphaByte(tc.alpha); got != tc.want {
			t.Errorf("an alpha of %.3f became the byte %d, want %d", tc.alpha, got, tc.want)
		}
	}
	// A quarter turn through a fade, which is what a build animation does to
	// its body: 0.25 truncates to 63 and the mesh would be a shade lighter
	// than the timeline asked for on every one of those frames.
	for _, alpha := range []float32{0.25, 0.75} {
		if got, want := AlphaByte(alpha), uint8(alpha*255+0.5); got != want {
			t.Errorf("an alpha of %.2f became the byte %d, want %d", alpha, got, want)
		}
	}
}

// TestFadeTintToBackgroundKeepsTheEnds checks the two ends the fade is judged
