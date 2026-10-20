package main

import (
	"image"
	"image/color"
	"math"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"github.com/fogleman/fauxgl"
)

// paintSgbSelection draws the software fallback's selection bounds and keeps
// the drag marquee in the UI. Vulkan draws selection bounds into its color target.
func paintSgbSelection(gtx layout.Context, tab *fileTab, viewer *modelViewer, width, height int, drawBounds bool) {
	area := image.Rectangle{Max: image.Pt(width, height)}
	canvasClip := clip.Rect{Max: area.Max}.Push(gtx.Ops)
	defer canvasClip.Pop()
	if tab == nil || viewer == nil || tab.sgbMap == nil || width <= 0 || height <= 0 {
		return
	}
	if drawBounds && viewer.model != nil && len(tab.sgbSelectedEntities) > 0 {
		pose := viewer.pose()
		if len(viewer.animatedEntities) > 0 {
			pose = sgb.CombineEntityPoses(len(pose.Bones), viewer.animatedEntities)
		}
		bounds := sgbEntityProjectedBounds(viewer.modelData(), tab.sgbEntityMeshIDs, buildViewMatrix(viewer.options(width, height)), width, height, pose, len(pose.Bones) > 0)
		for id := range tab.sgbSelectedEntities {
			box := bounds[id].Inset(-3).Intersect(area)
			if !box.Empty() {
				paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 220, B: 80, A: 220}, clip.Stroke{Path: sgbRectPath(gtx, box), Width: 2}.Op())
			}
		}
	}
	paintSgbSelectionMarquee(gtx, tab, area)
}

// prepareSgbSelectionBounds converts projected bounds for selected entities
// into four camera-facing world-space strips for the Vulkan overlay pipeline.
func prepareSgbSelectionBounds(model *whm.Model, selected map[int]bool, entityMeshes map[string]int, view fauxgl.Matrix, camera engine.Camera, pose whm.Pose, animated bool) []modelOverlayLine {
	if model == nil || len(selected) == 0 || camera.Width <= 0 || camera.Height <= 0 || math.Abs(view.Determinant()) < 1e-12 {
		return nil
	}
	bounds := sgbEntityProjectedBounds(model, entityMeshes, view, camera.Width, camera.Height, pose, animated)
	inverse := view.Inverse()
	halfWidth := engine.OverlayHalfWidth(engine.OverlayOptions{Camera: camera}, 1.1)
	var lines []modelOverlayLine
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
			lines = append(lines, modelOverlayLine{From: points[edge], To: points[(edge+1)%4], HalfWidth: halfWidth, R: 1, G: 0.86, B: 0.31, A: 0.92})
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
			position := vectorFromWhm(vertex.Pos)
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

func paintSgbSelectionMarquee(gtx layout.Context, tab *fileTab, area image.Rectangle) {
	if tab == nil || !tab.sgbSelecting || !tab.sgbSelectionDrag {
		return
	}
	box := sgbSelectionRect(tab.sgbSelectionAnchor, tab.sgbSelectionCurrent).Intersect(area)
	if box.Empty() {
		return
	}
	paint.FillShape(gtx.Ops, color.NRGBA{R: 90, G: 170, B: 255, A: 30}, clip.Rect{Min: box.Min, Max: box.Max}.Op())
	paint.FillShape(gtx.Ops, color.NRGBA{R: 110, G: 190, B: 255, A: 230}, clip.Stroke{Path: sgbRectPath(gtx, box), Width: 1}.Op())
}

// sgbEntityProjectedBounds projects all referenced vertices and combines every
// submesh into one screen-space rectangle per entity. Animated meshes use the
// same skinning pose as the renderer.
func sgbEntityProjectedBounds(model *whm.Model, entityMeshes map[string]int, view fauxgl.Matrix, width, height int, pose whm.Pose, animated bool) map[int]image.Rectangle {
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
			p := vectorFromWhm(vertex.Pos)
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

// sgbCanvasToFramePoint undoes the frame's scale and letterboxing so pointer
// coordinates and projected scene pixels share one coordinate system.
func sgbCanvasToFramePoint(area image.Point, frameWidth, frameHeight int, point f32.Point) image.Point {
	if area.X <= 0 || area.Y <= 0 || frameWidth <= 0 || frameHeight <= 0 {
		return image.Point{}
	}
	scale := math.Min(float64(area.X)/float64(frameWidth), float64(area.Y)/float64(frameHeight))
	offsetX := (float64(area.X) - float64(frameWidth)*scale) / 2
	offsetY := (float64(area.Y) - float64(frameHeight)*scale) / 2
	x := int(math.Round((float64(point.X) - offsetX) / scale))
	y := int(math.Round((float64(point.Y) - offsetY) / scale))
	return image.Pt(maxInt(x, 0), maxInt(y, 0))
}

func sgbPickEntity(model *whm.Model, entityMeshes map[string]int, camera engine.Camera, pixelX, pixelY float64) (int, bool) {
	if model == nil {
		return 0, false
	}
	view := engine.ViewMatrix(camera)
	if math.Abs(view.Determinant()) < 1e-12 {
		return 0, false
	}
	inverse := view.Inverse()
	nx, ny := 2*pixelX/float64(camera.Width)-1, 1-2*pixelY/float64(camera.Height)
	near := inverse.MulPositionW(fauxgl.Vector{X: nx, Y: ny, Z: -1})
	far := inverse.MulPositionW(fauxgl.Vector{X: nx, Y: ny, Z: 1})
	if math.Abs(near.W) < 1e-12 || math.Abs(far.W) < 1e-12 {
		return 0, false
	}
	origin := fauxgl.Vector{X: near.X / near.W, Y: near.Y / near.W, Z: near.Z / near.W}
	direction := fauxgl.Vector{X: far.X/far.W - origin.X, Y: far.Y/far.W - origin.Y, Z: far.Z/far.W - origin.Z}
	closest := math.Inf(1)
	picked := 0
	for _, mesh := range model.Meshes {
		id, ok := entityMeshes[mesh.Name]
		if !ok || mesh.Name == sgb.WaterMeshName {
			continue
		}
		for i := 0; i+2 < len(mesh.Indices); i += 3 {
			indices := mesh.Indices[i : i+3]
			if int(indices[0]) >= len(model.Vertices) || int(indices[1]) >= len(model.Vertices) || int(indices[2]) >= len(model.Vertices) {
				continue
			}
			vertices := [3]fauxgl.Vector{}
			for j, index := range indices {
				vertices[j] = vectorFromWhm(model.Vertices[index].Pos)
			}
			if distance, hit := sgbRayTriangle(origin, direction, vertices[0], vertices[1], vertices[2]); hit && distance < closest {
				closest, picked = distance, id
			}
		}
	}
	return picked, !math.IsInf(closest, 1)
}

func sgbRectangleEntities(model *whm.Model, entityMeshes map[string]int, view fauxgl.Matrix, width, height int, rect image.Rectangle) map[int]bool {
	selected := make(map[int]bool)
	if model == nil || width <= 0 || height <= 0 {
		return selected
	}
	rect = rect.Canon()
	for _, mesh := range model.Meshes {
		id, ok := entityMeshes[mesh.Name]
		if !ok || mesh.Name == sgb.WaterMeshName {
			continue
		}
		for i := 0; i+2 < len(mesh.Indices); i += 3 {
			idx := mesh.Indices[i : i+3]
			if int(idx[0]) >= len(model.Vertices) || int(idx[1]) >= len(model.Vertices) || int(idx[2]) >= len(model.Vertices) {
				continue
			}
			var points [3]image.Point
			valid := true
			for j, index := range idx {
				projected := view.MulPositionW(vectorFromWhm(model.Vertices[index].Pos))
				if projected.W <= 1e-6 {
					valid = false
					break
				}
				x := (projected.X/projected.W*0.5 + 0.5) * float64(width)
				y := (1 - (projected.Y/projected.W*0.5 + 0.5)) * float64(height)
				points[j] = image.Pt(int(math.Round(x)), int(math.Round(y)))
			}
			if valid && sgbTriangleIntersectsRect(points, rect) {
				selected[id] = true
				break
			}
		}
	}
	return selected
}

func sgbTriangleIntersectsRect(triangle [3]image.Point, rect image.Rectangle) bool {
	if rect.Empty() {
		return false
	}
	for _, point := range triangle {
		if point.In(rect) {
			return true
		}
	}
	corners := [...]image.Point{rect.Min, {X: rect.Max.X - 1, Y: rect.Min.Y}, {X: rect.Min.X, Y: rect.Max.Y - 1}, {X: rect.Max.X - 1, Y: rect.Max.Y - 1}}
	for _, corner := range corners {
		if sgbPointInTriangle(corner, triangle) {
			return true
		}
	}
	for edge := 0; edge < 3; edge++ {
		a, b := triangle[edge], triangle[(edge+1)%3]
		for side := 0; side < 4; side++ {
			c, d := corners[side], corners[(side+1)%4]
			if sgbSegmentsIntersect(a, b, c, d) {
				return true
			}
		}
	}
	return false
}

func sgbPointInTriangle(point image.Point, triangle [3]image.Point) bool {
	orientation := func(a, b, p image.Point) int64 {
		return int64(b.X-a.X)*int64(p.Y-a.Y) - int64(b.Y-a.Y)*int64(p.X-a.X)
	}
	d0 := orientation(triangle[0], triangle[1], point)
	d1 := orientation(triangle[1], triangle[2], point)
	d2 := orientation(triangle[2], triangle[0], point)
	return !(d0 < 0 || d1 < 0 || d2 < 0) || !(d0 > 0 || d1 > 0 || d2 > 0)
}

func sgbSegmentsIntersect(a, b, c, d image.Point) bool {
	orientation := func(p, q, r image.Point) int64 {
		return int64(q.X-p.X)*int64(r.Y-p.Y) - int64(q.Y-p.Y)*int64(r.X-p.X)
	}
	onSegment := func(p, q, r image.Point) bool {
		return q.X >= minInt(p.X, r.X) && q.X <= maxInt(p.X, r.X) && q.Y >= minInt(p.Y, r.Y) && q.Y <= maxInt(p.Y, r.Y)
	}
	o1, o2, o3, o4 := orientation(a, b, c), orientation(a, b, d), orientation(c, d, a), orientation(c, d, b)
	if (o1 < 0 && o2 > 0 || o1 > 0 && o2 < 0) && (o3 < 0 && o4 > 0 || o3 > 0 && o4 < 0) {
		return true
	}
	return o1 == 0 && onSegment(a, c, b) || o2 == 0 && onSegment(a, d, b) || o3 == 0 && onSegment(c, a, d) || o4 == 0 && onSegment(c, b, d)
}

func sgbSelectionRect(a, b image.Point) image.Rectangle {
	return image.Rect(minInt(a.X, b.X), minInt(a.Y, b.Y), maxInt(a.X, b.X), maxInt(a.Y, b.Y))
}

func sgbRectPath(gtx layout.Context, rect image.Rectangle) clip.PathSpec {
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(f32.Pt(float32(rect.Min.X), float32(rect.Min.Y)))
	path.LineTo(f32.Pt(float32(rect.Max.X), float32(rect.Min.Y)))
	path.LineTo(f32.Pt(float32(rect.Max.X), float32(rect.Max.Y)))
	path.LineTo(f32.Pt(float32(rect.Min.X), float32(rect.Max.Y)))
	path.Close()
	return path.End()
}
