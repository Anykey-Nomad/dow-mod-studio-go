package sgb

import (
	"math"

	"dow-mod-studio/internal/whm"
)

// TerrainModel builds the scenario's heightmap as a static mesh in the
// renderer's Z-up model space — X and Y the map plane, Z the height. The
// mesh carries no animation and weights every vertex to a single root bone,
// so the model-viewer pipeline draws it unchanged.
//
// maxGrid caps one axis of the grid: hardware renderers with a 16 bit index
// buffer refuse a model above 0xffff vertices, so a caller passes 250 or
// less and a 1024-cell map is sampled every fourth vertex while a 128-cell
// one comes out at full resolution.
func (m *Map) TerrainModel(maxGrid int) *whm.Model {
	if m == nil {
		return nil
	}
	g := m.Relief
	if g.W < 2 || g.H < 2 || len(g.Data) < g.W*g.H || maxGrid < 2 {
		return nil
	}
	// Sample indices along one axis: a regular step, then the final vertex
	// appended so the mesh reaches the map border exactly.
	axis := func(n int) []int {
		step := (n - 1 + maxGrid - 2) / (maxGrid - 1)
		if step < 1 {
			step = 1
		}
		out := make([]int, 0, n/step+2)
		for i := 0; i < n-1; i += step {
			out = append(out, i)
		}
		if len(out) == 0 || out[len(out)-1] != n-1 {
			out = append(out, n-1)
		}
		return out
	}
	xs := axis(g.W)
	zs := axis(g.H)
	gw, gh := len(xs), len(zs)

	cell := m.Ground.CellSize
	if cell <= 0 {
		cell = 1
	}
	height := func(i, j int) float32 { return float32(g.At(i, j)) * m.Ground.HeightScale }
	// Model space is Z up (the ground plane is Z=0), so the map plane
	// becomes X/Y and the relief Z. A vertex sits at
	// (i - centerX + 1) * cellsize on the map, matching the 2D view.
	worldX := func(i int) float32 { return (float32(i) - float32(m.Ground.CenterX) + 1) * cell }
	worldZ := func(j int) float32 { return (float32(j) - float32(m.Ground.CenterZ) + 1) * cell }

	clampI := func(v, hi int) int {
		if v < 0 {
			return 0
		}
		if v > hi {
			return hi
		}
		return v
	}
	verts := make([]whm.Vertex, 0, gw*gh)
	for _, j := range zs {
		for _, i := range xs {
			// The normal is the surface's own slope in metres, a central
			// difference over the stored cells, so it agrees with the
			// relief shading the diffuse image bakes in.
			dhdx := (height(clampI(i+1, g.W-1), j) - height(clampI(i-1, g.W-1), j)) / (2 * cell)
			dhdy := (height(i, clampI(j+1, g.H-1)) - height(i, clampI(j-1, g.H-1))) / (2 * cell)
			nx, ny := -dhdx, -dhdy
			n := math.Sqrt(float64(nx)*float64(nx) + float64(ny)*float64(ny) + 1)
			verts = append(verts, whm.Vertex{
				Pos:     [3]float32{worldX(i), worldZ(j), height(i, j)},
				Normal:  [3]float32{float32(float64(nx) / n), float32(float64(ny) / n), float32(1 / n)},
				UV:      [2]float32{(float32(i) + 0.5) / float32(g.W), (float32(j) + 0.5) / float32(g.H)},
				Bones:   [4]int32{0, -1, -1, -1},
				Weights: [4]float32{1, 0, 0, 0},
			})
		}
	}
	// Two triangles per cell, counter clockwise seen from above (+Z), which
	// is the front face the software renderer culls against and the
	// winding the hardware path mirrors into.
	idx := make([]uint32, 0, (gw-1)*(gh-1)*6)
	at := func(gi, gj int) uint32 { return uint32(gj*gw + gi) }
	for j := 0; j+1 < gh; j++ {
		for i := 0; i+1 < gw; i++ {
			a, b := at(i, j), at(i+1, j)
			c, d := at(i, j+1), at(i+1, j+1)
			idx = append(idx, a, b, d, a, d, c)
		}
	}

	model := &whm.Model{
		Name:     "terrain",
		Vertices: verts,
		// A single root bone keeps every vertex on the identity skin
		// matrix on both renderers: weight one to bone zero rather than
		// the all-slots-empty encoding, whose zero total the hardware
		// shader does not promise to fall back on.
		Bones: []whm.Bone{{Name: "terrain_root", Parent: -1, Quat: [4]float32{0, 0, 0, 1}}},
		Meshes: []whm.Mesh{
			{Name: "terrain", Indices: idx, SingleBone: -1},
		},
	}
	lo, hi := verts[0].Pos, verts[0].Pos
	for _, v := range verts {
		for a := 0; a < 3; a++ {
			if v.Pos[a] < lo[a] {
				lo[a] = v.Pos[a]
			}
			if v.Pos[a] > hi[a] {
				hi[a] = v.Pos[a]
			}
		}
	}
	model.BoundMin, model.BoundMax = lo, hi
	return model
}

const (
	WaterMeshName     = "sgb-water"
	WaterMaterialName = "sgb-water"
)

var WaterColor = [4]uint8{8, 32, 92, 112}

// AddWaterMesh appends the map's simple translucent sea surface to a scene.
// The reference viewer uses a fixed water elevation of 4.5 world units.
func (m *Map) AddWaterMesh(model *whm.Model) {
	if m == nil || model == nil || m.Ground.CellsX <= 0 || m.Ground.CellsZ <= 0 {
		return
	}
	bounds := m.Ground.Bounds()
	if bounds.MaxX <= bounds.MinX || bounds.MaxY <= bounds.MinY {
		return
	}
	for _, mesh := range model.Meshes {
		if mesh.Name == WaterMeshName {
			return
		}
	}
	base := uint32(len(model.Vertices))
	level := m.Ground.WaterLevel()
	model.Vertices = append(model.Vertices,
		whm.Vertex{Pos: [3]float32{bounds.MinX, bounds.MinY, level}, Normal: [3]float32{0, 0, 1}, Bones: [4]int32{-1, -1, -1, -1}, Weights: [4]float32{}},
		whm.Vertex{Pos: [3]float32{bounds.MaxX, bounds.MinY, level}, Normal: [3]float32{0, 0, 1}, Bones: [4]int32{-1, -1, -1, -1}, Weights: [4]float32{}},
		whm.Vertex{Pos: [3]float32{bounds.MaxX, bounds.MaxY, level}, Normal: [3]float32{0, 0, 1}, Bones: [4]int32{-1, -1, -1, -1}, Weights: [4]float32{}},
		whm.Vertex{Pos: [3]float32{bounds.MinX, bounds.MaxY, level}, Normal: [3]float32{0, 0, 1}, Bones: [4]int32{-1, -1, -1, -1}, Weights: [4]float32{}},
	)
	model.Meshes = append(model.Meshes, whm.Mesh{
		Name: WaterMeshName, Material: WaterMaterialName,
		Indices:    []uint32{base, base + 1, base + 2, base, base + 2, base + 3},
		SingleBone: -1,
	})
	for axis := 0; axis < 3; axis++ {
		lo := [...]float32{bounds.MinX, bounds.MinY, level}[axis]
		hi := [...]float32{bounds.MaxX, bounds.MaxY, level}[axis]
		if model.BoundMin[axis] > lo {
			model.BoundMin[axis] = lo
		}
		if model.BoundMax[axis] < hi {
			model.BoundMax[axis] = hi
		}
	}
}
