package whm

import (
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

func TestRootMarkerForwardDoesNotDoubleRotateBoneAxis(t *testing.T) {
	// These are the relevant transforms from the shipped necron_monolith:
	// bone_master is a quarter-turn root, while the portal's chosen frame axis
	// is horizontal in model space. Reapplying the accumulated root matrix to
	// its own second column squares the rotation and makes the effect point -Z.
	m := &Model{
		Bones: []Bone{{
			Name:   "bone_master",
			Parent: -1,
			Pos:    [3]float32{0, 3.5008492, 0},
			Quat:   [4]float32{0, 0, 0.70710665, 0.7071069},
		}},
		Markers: []Marker{{
			Name:   "mrkr_portal_01",
			Parent: "bone_master",
			Matrix: [9]float32{
				1.6852224e-5, -1, -1.636346e-5,
				-0.002672204, -1.6408434e-5, 0.9999964,
				-0.9999964, -1.6808435e-5, -0.0026722043,
			},
			Pos: [3]float32{-0.13464424, -0.08467927, 3.0711234},
		}},
	}
	pose := m.RestPose()
	world, ok := m.MarkerWorld(0, pose, true)
	if !ok {
		t.Fatal("portal marker has no position")
	}
	// The portal is on the outside/below the structure; its effect is aimed
	// toward the body, approximately +Y in the model's coordinates. This
	// gives the root-marker fallback a real mesh cue without needing a game
	// installation in the test.
	centroid := world.Add(fauxgl.Vector{Y: -3, Z: -2})
	for range 4 {
		m.Vertices = append(m.Vertices, Vertex{
			Pos:     [3]float32{float32(centroid.X), float32(centroid.Y), float32(centroid.Z)},
			Bones:   [4]int32{0, -1, -1, -1},
			Weights: [4]float32{1},
		})
	}

	axis, ok := m.MarkerAxis(0, pose, true)
	if !ok {
		t.Fatal("portal marker has no forward axis")
	}
	want := fauxgl.Vector{Y: 1}
	if dot := axis.Dot(want); dot < 0.99 {
		t.Errorf("portal marker points %v (dot %.3f), want inward along %v", axis, dot, want)
	}
	if math.Abs(axis.Z) > 0.01 {
		t.Errorf("portal marker points vertically (Z=%.3f), want its horizontal inward direction", axis.Z)
	}
}
