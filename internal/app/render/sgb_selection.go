package render

import (
	"image"
	"math"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"

	"github.com/fogleman/fauxgl"
)

// The SGB selection overlay: projecting selected entities into the world-space
// strips the Vulkan pipeline draws. It lives with the renderer that consumes
// it, so the view and the GPU path share one implementation rather than the
// renderer reaching back into the application package.

// PrepareSgbSelectionBounds converts projected bounds for selected entities
// into four camera-facing world-space strips for the Vulkan overlay pipeline.
func PrepareSgbSelectionBounds(model *whm.Model, selected map[int]bool, entityMeshes map[string]int, view fauxgl.Matrix, camera engine.Camera, pose whm.Pose, animated bool) []ModelOverlayLine {
	if model == nil || len(selected) == 0 || camera.Width <= 0 || camera.Height <= 0 || math.Abs(view.Determinant()) < 1e-12 {
		return nil
	}
	bounds := SgbEntityProjectedBounds(model, entityMeshes, view, camera.Width, camera.Height, pose, animated)
	inverse := view.Inverse()
	halfWidth := engine.OverlayHalfWidth(engine.OverlayOptions{Camera: camera}, 1.1)
	var lines []ModelOverlayLine
	for id := range selected {
		box := bounds[id]
		if box.Empty() {
			continue
		}
		// Mirror the software outline's small margin, then reconstruct the
		// four corners at the entity's nearest projected depth. The overlay
		// pipeline draws these strips into the Vulkan color target without a
		// depth test, keeping the selection visible over the selected mesh.
		box = box.Inset(-3)
		depth := sgbEntityNearestDepth(model, entityMeshes, id, view, pose, animated)
		corners := [...]image.Point{box.Min, {X: box.Max.X, Y: box.Min.Y}, box.Max, {X: box.Min.X, Y: box.Max.Y}}
		var points [4]fauxgl.Vector
		valid := true
		for i, point := range corners {
			clipPoint := fauxgl.Vector{
				X: 2*float64(point.X)/float64(camera.Width) - 1,
				Y: 1 - 2*float64(point.Y)/float64(camera.Height),
				Z: math.Max(0, depth-0.001),
			}
			world := inverse.MulPositionW(clipPoint)
			if math.Abs(world.W) < 1e-12 {
				valid = false
				break
			}
			points[i] = fauxgl.Vector{X: world.X / world.W, Y: world.Y / world.W, Z: world.Z / world.W}
		}
		if !valid {
			continue
		}
		for edge := range 4 {
			lines = append(lines, ModelOverlayLine{From: points[edge], To: points[(edge+1)%4], HalfWidth: halfWidth, R: 1, G: 0.86, B: 0.31, A: 0.92})
		}
	}
	return lines
}

func sgbEntityNearestDepth(model *whm.Model, entityMeshes map[string]int, entityID int, view fauxgl.Matrix, pose whm.Pose, animated bool) float64 {
	nearest := math.Inf(1)
	for _, mesh := range model.Meshes {
		id, ok := entityMeshes[mesh.Name]
		if !ok || id != entityID || mesh.Name == sgb.WaterMeshName {
			continue
		}
		for _, index := range mesh.Indices {
			if int(index) >= len(model.Vertices) {
				continue
			}
			vertex := &model.Vertices[index]
			position := VecFromWhm(vertex.Pos)
			if animated {
				position, _ = whm.SkinnedVertex(model, pose, vertex)
			}
			projected := view.MulPositionW(position)
			if projected.W <= 1e-6 {
				continue
			}
			depth := projected.Z / projected.W
			if depth >= 0 && depth < nearest {
				nearest = depth
			}
		}
	}
	if math.IsInf(nearest, 1) {
		return 1
	}
	return nearest
}

// SgbEntityProjectedBounds projects all referenced vertices and combines every
// submesh into one screen-space rectangle per entity. Animated meshes use the
// same skinning pose as the renderer.
func SgbEntityProjectedBounds(model *whm.Model, entityMeshes map[string]int, view fauxgl.Matrix, width, height int, pose whm.Pose, animated bool) map[int]image.Rectangle {
	bounds := make(map[int]image.Rectangle)
	if model == nil || width <= 0 || height <= 0 {
		return bounds
	}
	for _, mesh := range model.Meshes {
		if mesh.Name == sgb.WaterMeshName {
			continue
		}
		id, ok := entityMeshes[mesh.Name]
		if !ok {
			continue
		}
		for _, index := range mesh.Indices {
			if int(index) >= len(model.Vertices) {
				continue
			}
			vertex := &model.Vertices[index]
			p := VecFromWhm(vertex.Pos)
			if animated {
				p, _ = whm.SkinnedVertex(model, pose, vertex)
			}
			projected := view.MulPositionW(p)
			if projected.W <= 1e-6 {
				continue
			}
			x := (projected.X/projected.W*0.5 + 0.5) * float64(width)
			y := (1 - (projected.Y/projected.W*0.5 + 0.5)) * float64(height)
			point := image.Rect(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(x))+1, int(math.Floor(y))+1)
			if current := bounds[id]; current.Empty() {
				bounds[id] = point
			} else {
				bounds[id] = current.Union(point)
			}
		}
	}
	return bounds
}

// VecFromWhm converts the format's float array to the renderer's vector type.
func VecFromWhm(position [3]float32) fauxgl.Vector {
	return fauxgl.Vector{X: float64(position[0]), Y: float64(position[1]), Z: float64(position[2])}
}
