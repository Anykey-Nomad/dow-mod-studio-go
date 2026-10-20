package main

import (
	"math"

	"dow-mod-studio/internal/sgb"
)

const (
	// sgbCameraDistance is how far the 3D map view's camera opens from the
	// ground, in metres: the range the in-game camera works at, rather than
	// the whole-map fit a model viewer would choose.
	sgbCameraDistance = 70.0
	// sgbCameraMinDistance is the closest the wheel may bring the camera,
	// still an orbit a unit would fit in rather than a pinch of ground.
	sgbCameraMinDistance = 10.0
	// sgbCameraPitch is the opening elevation — what the game's camera looks
	// down from. The model viewer's 0.2 rad is nearly sideways at map scale.
	sgbCameraPitch = 49.481994013136 * math.Pi / 180
	// The opening view's camera-to-axis angles are the closest realizable
	// direction to X=120°, Y=65°, Z=140° for a single unit-length vector.
	sgbCameraYaw = -40.205715405573 * math.Pi / 180
)

// sgbCameraDefaultDistance picks the opening camera distance: the in-game
// range, but never past the fit of a map too small to need it.
func sgbCameraDefaultDistance(m *sgb.Map) float64 {
	if m == nil {
		return sgbCameraDistance
	}
	w := float64(m.Ground.XMax() - m.Ground.XMin())
	d := float64(m.Ground.ZMax() - m.Ground.ZMin())
	fit := 1.4 * math.Hypot(w, d)
	if fit > 0 && fit < sgbCameraDistance {
		return fit
	}
	return sgbCameraDistance
}

// 3D terrain for the .sgb viewer. The heightmap mesh itself is built by
// sgb.Map.TerrainModel in the scenario package, so a probe can render the
// same geometry the viewer shows; this file is the app-side glue — the
// material name, the model wrapper and the shaded relief as a diffuse map.

// sgbTerrainMaxGrid caps one axis of the terrain grid. The hardware
// renderer's index buffer is 16 bit and buildMesh refuses a model above
// 0xffff vertices, so a 250×250 grid is the densest one both paths draw.
const sgbTerrainMaxGrid = 250

// sgbTerrainMaterial names one map's terrain diffuse. It carries the tab's
// path so two open maps never share a texture slot in the renderer's cache.
func sgbTerrainMaterial(virtualPath string) string {
	return "sgb-terrain:" + virtualPath
}

// sgbTerrainModel wraps the scenario package's heightmap mesh for the
// viewer's model type.
func sgbTerrainModel(m *sgb.Map, material string) *WhmModel {
	model := m.TerrainModel(sgbTerrainMaxGrid)
	if model == nil {
		return nil
	}
	model.Meshes[0].Material = material
	m.AddWaterMesh(model)
	return &WhmModel{Model: *model}
}

// sgbTerrainHeightAt returns the heightmap's bilinearly interpolated surface
// height at world-space X/Y coordinates, clamped to the map edges.
func sgbTerrainHeightAt(m *sgb.Map, x, y float64) (float64, bool) {
	if m == nil || m.Relief.W < 2 || m.Relief.H < 2 || len(m.Relief.Data) < m.Relief.W*m.Relief.H ||
		m.Ground.CellSize <= 0 {
		return 0, false
	}
	cell := float64(m.Ground.CellSize)
	ix := math.Max(0, math.Min(float64(m.Relief.W-1), x/cell+float64(m.Ground.CenterX)-1))
	iy := math.Max(0, math.Min(float64(m.Relief.H-1), y/cell+float64(m.Ground.CenterZ)-1))
	x0, y0 := int(math.Floor(ix)), int(math.Floor(iy))
	x1, y1 := minInt(x0+1, m.Relief.W-1), minInt(y0+1, m.Relief.H-1)
	fx, fy := ix-float64(x0), iy-float64(y0)
	h00 := float64(m.Relief.At(x0, y0))
	h10 := float64(m.Relief.At(x1, y0))
	h01 := float64(m.Relief.At(x0, y1))
	h11 := float64(m.Relief.At(x1, y1))
	h0 := h00*(1-fx) + h10*fx
	h1 := h01*(1-fx) + h11*fx
	return (h0*(1-fy) + h1*fy) * float64(m.Ground.HeightScale), true
}

// sgbApproachCameraVelocity eases one movement axis toward its input target.
// A linear ramp gives predictable acceleration and a soft stop when released.
func sgbApproachCameraVelocity(value, target, seconds float64) float64 {
	const acceleration = 4.5 // reach full movement speed in about 220 ms
	step := acceleration * seconds
	if value < target {
		value = math.Min(value+step, target)
	} else {
		value = math.Max(value-step, target)
	}
	if target == 0 && math.Abs(value) < 1e-4 {
		return 0
	}
	return value
}

// sgbMoveCamera advances the orbit target over the map in camera-relative
// directions. forward and strafe are signed velocity axes; seconds makes
// motion independent of frame rate. The target's Z follows the terrain.
func (ui *uiState) sgbMoveCamera(t *fileTab, forward, strafe, seconds float64) bool {
	if t == nil || t.sgbMap == nil || t.model == nil || seconds <= 0 || (forward == 0 && strafe == 0) {
		return false
	}
	length := math.Hypot(forward, strafe)
	if length > 1 {
		forward, strafe = forward/length, strafe/length
	}
	speed := math.Max(float64(t.sgbMap.Ground.CellSize)*8, 20) // metres per second
	forwardX, forwardY := -math.Cos(t.model.Yaw), -math.Sin(t.model.Yaw)
	rightX, rightY := -forwardY, forwardX
	distance := speed * seconds
	dx := (forwardX*forward + rightX*strafe) * distance
	dy := (forwardY*forward + rightY*strafe) * distance
	g := t.sgbMap.Ground
	x := math.Max(float64(g.XMin()), math.Min(float64(g.XMax()), t.model.Center.X+dx))
	y := math.Max(float64(g.ZMin()), math.Min(float64(g.ZMax()), t.model.Center.Y+dy))
	if x == t.model.Center.X && y == t.model.Center.Y {
		return false
	}
	height, ok := sgbTerrainHeightAt(t.sgbMap, x, y)
	if !ok {
		return false
	}
	t.model.Center.X, t.model.Center.Y, t.model.Center.Z = x, y, height
	t.model.ActiveCamera = ""
	t.model.invalidate()
	return true
}
