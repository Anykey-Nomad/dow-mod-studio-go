package sgb

import (
	"testing"

	"dow-mod-studio/internal/whm"
)

func TestAddWaterMeshUsesMapBoundsAndLevel(t *testing.T) {
	m := &Map{Ground: Ground{CenterX: 3, CenterZ: 2, CellSize: 4, CellsX: 8, CellsZ: 6}}
	model := &whm.Model{
		Name:     "terrain",
		Vertices: []whm.Vertex{{Pos: [3]float32{0, 0, 0}}},
		Meshes:   []whm.Mesh{{Name: "terrain", Indices: []uint32{0, 0, 0}}},
		BoundMin: [3]float32{0, 0, 0},
		BoundMax: [3]float32{1, 1, 1},
	}
	m.AddWaterMesh(model)
	if len(model.Vertices) != 5 || len(model.Meshes) != 2 {
		t.Fatalf("water adds vertices/meshes: got %d/%d, want 5/2", len(model.Vertices), len(model.Meshes))
	}
	water := model.Meshes[1]
	if water.Name != WaterMeshName || water.Material != WaterMaterialName {
		t.Fatalf("water mesh identity = %q/%q", water.Name, water.Material)
	}
	wantPositions := [][3]float32{
		{-8, -4, 4.5}, {24, -4, 4.5}, {24, 20, 4.5}, {-8, 20, 4.5},
	}
	for i, want := range wantPositions {
		if got := model.Vertices[i+1].Pos; got != want {
			t.Errorf("water vertex %d = %v, want %v", i, got, want)
		}
	}
	wantIndices := []uint32{1, 2, 3, 1, 3, 4}
	for i, want := range wantIndices {
		if got := water.Indices[i]; got != want {
			t.Errorf("water index %d = %d, want %d", i, got, want)
		}
	}
	if model.BoundMin != [3]float32{-8, -4, 0} || model.BoundMax != [3]float32{24, 20, 4.5} {
		t.Errorf("bounds = %v..%v, want [-8 -4 0]..[24 20 4.5]", model.BoundMin, model.BoundMax)
	}
	if got := m.Ground.WaterLevel(); got != 4.5 {
		t.Fatalf("reference water level is %v, want 4.5 m", got)
	}
	for _, vertex := range model.Vertices[1:] {
		if vertex.Bones != [4]int32{-1, -1, -1, -1} || vertex.Weights != [4]float32{} {
			t.Errorf("water vertex should be unskinned: bones=%v weights=%v", vertex.Bones, vertex.Weights)
			break
		}
	}
	beforeVertices, beforeMeshes := len(model.Vertices), len(model.Meshes)
	m.AddWaterMesh(model)
	if len(model.Vertices) != beforeVertices || len(model.Meshes) != beforeMeshes {
		t.Fatal("adding water twice duplicated the plane")
	}
}
