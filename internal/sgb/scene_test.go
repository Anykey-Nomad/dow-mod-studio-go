package sgb

import (
	"math"
	"testing"

	"dow-mod-studio/internal/whm"
)

func TestAppendEntityUsesReferenceEditorHeadingDirection(t *testing.T) {
	src := &whm.Model{
		Vertices: []whm.Vertex{{Pos: [3]float32{1, 0, 0}, Normal: [3]float32{1, 0, 0}}},
		Meshes:   []whm.Mesh{{Name: "body", Indices: []uint32{0, 0, 0}}},
	}
	for _, tc := range []struct {
		name    string
		heading float64
		wantX   float64
		wantY   float64
	}{
		{"zero heading", 0, 1, 0},
		{"quarter turn clockwise", math.Pi / 2, 0, 1},
		{"half turn", math.Pi, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := &whm.Model{}
			placement := (Entity{Heading: float32(tc.heading)}).PlacementOf()
			AppendEntity(dst, src, placement, "placed")
			if len(dst.Vertices) != 1 {
				t.Fatalf("got %d vertices, want 1", len(dst.Vertices))
			}
			got := dst.Vertices[0]
			if math.Abs(float64(got.Pos[0])-tc.wantX) > 1e-6 || math.Abs(float64(got.Pos[1])-tc.wantY) > 1e-6 {
				t.Errorf("heading %v produced XY=(%v,%v), want (%v,%v)", tc.heading, got.Pos[0], got.Pos[1], tc.wantX, tc.wantY)
			}
			if math.Abs(float64(got.Normal[0])-tc.wantX) > 1e-6 || math.Abs(float64(got.Normal[1])-tc.wantY) > 1e-6 {
				t.Errorf("heading %v produced normal XY=(%v,%v), want (%v,%v)", tc.heading, got.Normal[0], got.Normal[1], tc.wantX, tc.wantY)
			}
		})
	}
}
