package main

import (
	"fmt"
	"image"
	"log"
	"math"
	"path/filepath"
	"strings"
	"time"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"github.com/fogleman/fauxgl"
)

// The 3D map view's scene: terrain with placed entities merged into the same
// model-viewer pipeline, with renderer mesh names mapped back to SGB entity IDs.
func sgbBuildSceneModel(tree *modtree.ModTree, m *sgb.Map, terrainMaterial string) *whm.Model {
	scene, _, _ := sgbBuildSceneModelWithEntityAnimations(tree, m, terrainMaterial)
	return scene
}

func sgbBuildSceneModelWithEntityIDs(tree *modtree.ModTree, m *sgb.Map, terrainMaterial string) (*whm.Model, map[string]int) {
	scene, entityMeshes, _ := sgbBuildSceneModelWithEntityAnimations(tree, m, terrainMaterial)
	return scene, entityMeshes
}

func sgbBuildSceneModelWithEntityAnimations(tree *modtree.ModTree, m *sgb.Map, terrainMaterial string) (*whm.Model, map[string]int, []sgb.AnimatedEntity) {
	entityMeshes := make(map[string]int)
	var animatedEntities []sgb.AnimatedEntity
	if m == nil {
		return nil, entityMeshes, animatedEntities
	}
	start := time.Now()
	scene := m.TerrainModel(sgbTerrainMaxGrid)
	if scene == nil && len(m.Entities) == 0 {
		return nil, entityMeshes, animatedEntities
	}
	if scene == nil {
		scene = &whm.Model{Name: "sgb-scene"}
	}
	if len(scene.Meshes) > 0 {
		scene.Meshes[0].Material = terrainMaterial
	}
	if !sceneHasMesh(scene, sgb.WaterMeshName) {
		m.AddWaterMesh(scene)
	}
	scene.Name = "sgb-scene"
	type blueprintModel struct {
		model     *whm.Model
		modelPath string
		idleAnim  string
		animation *whm.Animation
	}
	cache := map[int]blueprintModel{}
	placed, skipped, distinct := 0, 0, 0
	for _, e := range m.Entities {
		cached, seen := cache[e.BP]
		if !seen {
			src, modelPath := sgbLoadEntityModelWithPath(tree, e)
			cached = blueprintModel{model: src, modelPath: modelPath, idleAnim: sgbEntityIdleAnimationName(tree, m, e)}
			if cached.idleAnim == "" {
				cached.animation, _ = sgb.ResolveLinkedIdleAnimation(tree, modelPath, src)
			} else {
				cached.animation, _ = sgb.ResolveLinkedAnimation(tree, modelPath, src, cached.idleAnim)
			}
			cached.animation = sgb.BindAnimationToModel(src, cached.animation)
			cache[e.BP] = cached
		}
		src := cached.model
		if src == nil {
			if !seen {
				skipped++
			}
			continue
		}
		if !seen {
			distinct++
		}
		meshName := fmt.Sprintf("entity-%d", e.ID)
		boneBase := -1
		if cached.animation != nil && len(src.Bones) > 0 {
			boneBase = sgb.AppendEntityWithPose(scene, src, e.PlacementOf(), meshName, src.RestPose(), true)
			animatedEntities = append(animatedEntities, sgb.AnimatedEntity{ID: e.ID, BoneBase: boneBase, Placement: e.PlacementOf(), Model: src, Animation: cached.animation})
		} else {
			sgb.AppendEntity(scene, src, e.PlacementOf(), meshName)
		}
		for i := len(scene.Meshes) - 1; i >= 0 && scene.Meshes[i].Name == meshName; i-- {
			entityMeshes[meshName] = e.ID
		}
		placed++
	}
	sgbSceneBounds(scene)
	log.Printf("sgb: scene %s: %d/%d objects placed from %d distinct models (%d blueprints unresolved), %d verts, in %v",
		filepath.Base(m.Name), placed, len(m.Entities), distinct, skipped, len(scene.Vertices), time.Since(start))
	if placed == 0 {
		return nil, entityMeshes, animatedEntities
	}
	return scene, entityMeshes, animatedEntities
}

func sceneHasMesh(scene *whm.Model, name string) bool {
	if scene == nil {
		return false
	}
	for _, mesh := range scene.Meshes {
		if mesh.Name == name {
			return true
		}
	}
	return false
}

// sgbSceneAppendEntity appends renderable static geometry to the live map
// scene, then refreshes its bounds for the viewer and GPU renderer.
func sgbSceneAppendEntity(scene, src *whm.Model, placement sgb.Placement, entityID int) bool {
	if scene == nil || src == nil || len(src.Vertices) == 0 || len(src.Meshes) == 0 {
		return false
	}
	hasTriangles := false
	for _, mesh := range src.Meshes {
		if len(mesh.Indices) >= 3 {
			hasTriangles = true
			break
		}
	}
	if !hasTriangles {
		return false
	}
	meshName := fmt.Sprintf("entity-%d", entityID)
	firstMesh := len(scene.Meshes)
	sgb.AppendEntity(scene, src, placement, meshName)
	// Different WHMs can reuse a display material name for different diffuse
	// maps. Give the live placement its own lookup key, retaining the authored
	// diffuse reference in MaterialPath for both software and Vulkan renderers.
	for i := firstMesh; i < len(scene.Meshes); i++ {
		mesh := &scene.Meshes[i]
		if mesh.Material == "" {
			continue
		}
		mesh.Material = fmt.Sprintf("%s::%s", meshName, mesh.Material)
	}
	sgbSceneBounds(scene)
	return true
}

// sgbSceneBounds recomputes the merged model's bounds after geometry changes.
func sgbSceneBounds(scene *whm.Model) {
	if scene == nil {
		return
	}
	found := false
	var lo, hi [3]float32
	for _, mesh := range scene.Meshes {
		for _, index := range mesh.Indices {
			if int(index) >= len(scene.Vertices) {
				continue
			}
			position := scene.Vertices[index].Pos
			if !found {
				lo, hi, found = position, position, true
				continue
			}
			for axis := 0; axis < 3; axis++ {
				if position[axis] < lo[axis] {
					lo[axis] = position[axis]
				}
				if position[axis] > hi[axis] {
					hi[axis] = position[axis]
				}
			}
		}
	}
	scene.BoundMin, scene.BoundMax = lo, hi
}

// sgbRemoveEntities drops placed entity records and their scene meshes. Vertex
// storage is left intact because the remaining mesh indices are stable; bounds
// are recomputed from referenced vertices so removed geometry no longer affects
// framing or culling.
func sgbRemoveEntities(m *sgb.Map, scene *whm.Model, entityMeshes map[string]int, animated []sgb.AnimatedEntity, selected map[int]bool) ([]sgb.AnimatedEntity, int) {
	if m == nil || len(selected) == 0 {
		return animated, 0
	}
	removedNames := make(map[string]bool)
	for name, id := range entityMeshes {
		if selected[id] {
			removedNames[name] = true
		}
	}
	removedAnimated := make(map[int]bool, len(animated))
	for _, entity := range animated {
		if selected[entity.ID] && entity.Model != nil && entity.BoneBase >= 0 {
			removedAnimated[entity.ID] = true
		}
	}
	originalAnimated := append([]sgb.AnimatedEntity(nil), animated...)
	keptEntities := m.Entities[:0]
	removed := 0
	for _, entity := range m.Entities {
		if selected[entity.ID] {
			removed++
			continue
		}
		keptEntities = append(keptEntities, entity)
	}
	m.Entities = keptEntities
	for name := range removedNames {
		delete(entityMeshes, name)
	}
	keptAnimations := animated[:0]
	for _, entity := range animated {
		if !selected[entity.ID] {
			keptAnimations = append(keptAnimations, entity)
		}
	}
	if scene != nil && len(removedNames) > 0 {
		keptMeshes := scene.Meshes[:0]
		for _, mesh := range scene.Meshes {
			if !removedNames[mesh.Name] {
				keptMeshes = append(keptMeshes, mesh)
			}
		}
		scene.Meshes = keptMeshes
		if len(removedAnimated) > 0 {
			keepBones := make([]bool, len(scene.Bones))
			for i := range keepBones {
				keepBones[i] = true
			}
			for _, entity := range originalAnimated {
				if !removedAnimated[entity.ID] {
					continue
				}
				end := entity.BoneBase + len(entity.Model.Bones)
				for i := entity.BoneBase; i < end && i < len(keepBones); i++ {
					if i >= 0 {
						keepBones[i] = false
					}
				}
			}
			boneRemap := make([]int32, len(keepBones))
			bones := make([]whm.Bone, 0, len(scene.Bones))
			for i, keep := range keepBones {
				if !keep {
					boneRemap[i] = -1
					continue
				}
				boneRemap[i] = int32(len(bones))
				bones = append(bones, scene.Bones[i])
			}
			for i := range bones {
				parent := bones[i].Parent
				if parent >= 0 && parent < len(boneRemap) {
					bones[i].Parent = int(boneRemap[parent])
				}
			}
			scene.Bones = bones
			for i := range scene.Vertices {
				for slot, bone := range scene.Vertices[i].Bones {
					if bone >= 0 && int(bone) < len(boneRemap) && boneRemap[bone] >= 0 {
						scene.Vertices[i].Bones[slot] = boneRemap[bone]
					} else if bone >= 0 && int(bone) < len(boneRemap) {
						scene.Vertices[i].Bones[slot] = -1
						scene.Vertices[i].Weights[slot] = 0
					}
				}
			}
			for i := range keptAnimations {
				oldBase := keptAnimations[i].BoneBase
				if oldBase < 0 || oldBase >= len(boneRemap) || boneRemap[oldBase] < 0 {
					keptAnimations[i].BoneBase = -1
					continue
				}
				keptAnimations[i].BoneBase = int(boneRemap[oldBase])
			}
		}
		sgbSceneBounds(scene)
	}
	return keptAnimations, removed
}

// sgbRotateEntities rotates selected placements around map vertical (world Z).
// Static vertices are transformed around each entity's origin; animated models
// use the same heading on their placement matrix.
func sgbRotateEntities(m *sgb.Map, scene *whm.Model, entityMeshes map[string]int, animated []sgb.AnimatedEntity, selected map[int]bool, delta float64) int {
	if m == nil || scene == nil || len(selected) == 0 || delta == 0 {
		return 0
	}
	animatedIDs := make(map[int]int, len(animated))
	for i := range animated {
		animatedIDs[animated[i].ID] = i
	}
	rotated := 0
	for i := range m.Entities {
		entity := &m.Entities[i]
		if !selected[entity.ID] {
			continue
		}
		x, y := float64(entity.X), float64(entity.Z)
		c, s := math.Cos(delta), math.Sin(delta)
		rotatedVertices := make(map[uint32]bool)
		for _, mesh := range scene.Meshes {
			if id, ok := entityMeshes[mesh.Name]; !ok || id != entity.ID {
				continue
			}
			for _, index := range mesh.Indices {
				if int(index) >= len(scene.Vertices) || rotatedVertices[index] {
					continue
				}
				rotatedVertices[index] = true
				vertex := &scene.Vertices[index]
				dx, dy := float64(vertex.Pos[0])-x, float64(vertex.Pos[1])-y
				vertex.Pos[0] = float32(x + dx*c - dy*s)
				vertex.Pos[1] = float32(y + dx*s + dy*c)
			}
		}
		entity.Heading = float32(math.Remainder(float64(entity.Heading)+delta, 2*math.Pi))
		if animatedIndex, ok := animatedIDs[entity.ID]; ok {
			animated[animatedIndex].Placement.Heading = float64(entity.Heading)
		}
		rotated++
	}
	if rotated > 0 {
		sgbSceneBounds(scene)
	}
	return rotated
}

// sgbMoveEntitiesOnSurface applies a map-plane drag to the selected entities,
// sampling each destination's relief height. Static scene vertices are moved
// in-place; animated instances use their separate placement transforms.
func sgbMoveEntitiesOnSurface(m *sgb.Map, scene *whm.Model, entityMeshes map[string]int, animated []sgb.AnimatedEntity, selected map[int]bool, dx, dy float64) ([]sgb.AnimatedEntity, int) {
	if m == nil || scene == nil || len(selected) == 0 || (dx == 0 && dy == 0) {
		return animated, 0
	}
	animatedIDs := make(map[int]int, len(animated))
	for i := range animated {
		animatedIDs[animated[i].ID] = i
	}
	minDX, maxDX := math.Inf(-1), math.Inf(1)
	minDY, maxDY := math.Inf(-1), math.Inf(1)
	for _, entity := range m.Entities {
		if !selected[entity.ID] {
			continue
		}
		minDX = math.Max(minDX, float64(m.Ground.XMin())-float64(entity.X))
		maxDX = math.Min(maxDX, float64(m.Ground.XMax())-float64(entity.X))
		minDY = math.Max(minDY, float64(m.Ground.ZMin())-float64(entity.Z))
		maxDY = math.Min(maxDY, float64(m.Ground.ZMax())-float64(entity.Z))
	}
	if math.IsInf(minDX, 0) || minDX > maxDX || minDY > maxDY {
		return animated, 0
	}
	dx = clampFloat64(dx, minDX, maxDX)
	dy = clampFloat64(dy, minDY, maxDY)
	if dx == 0 && dy == 0 {
		return animated, 0
	}
	moved := 0
	for i := range m.Entities {
		entity := &m.Entities[i]
		if !selected[entity.ID] {
			continue
		}
		newX := float64(entity.X) + dx
		newY := float64(entity.Z) + dy
		if newX == float64(entity.X) && newY == float64(entity.Z) {
			continue
		}
		height, ok := sgbTerrainHeightAt(m, newX, newY)
		if !ok {
			continue
		}
		moveX, moveY, moveZ := newX-float64(entity.X), newY-float64(entity.Z), height-float64(entity.Y)
		entity.X, entity.Z, entity.Y = float32(newX), float32(newY), float32(height)
		if animatedIndex, ok := animatedIDs[entity.ID]; ok {
			animated[animatedIndex].Placement.X = newX
			animated[animatedIndex].Placement.Y = newY
			animated[animatedIndex].Placement.Z = height
		}
		// Scene vertices are stored in their placement's world frame. Move
		// them together with the animation placement so its conjugated skin
		// matrices continue to see the same model-local coordinates.
		movedVertices := make(map[uint32]bool)
		for _, mesh := range scene.Meshes {
			if id, ok := entityMeshes[mesh.Name]; !ok || id != entity.ID {
				continue
			}
			for _, index := range mesh.Indices {
				if int(index) >= len(scene.Vertices) || movedVertices[index] {
					continue
				}
				movedVertices[index] = true
				vertex := &scene.Vertices[index]
				vertex.Pos[0] += float32(moveX)
				vertex.Pos[1] += float32(moveY)
				vertex.Pos[2] += float32(moveZ)
			}
		}
		moved++
	}
	if moved > 0 {
		sgbSceneBounds(scene)
	}
	return animated, moved
}

func sgbNextPlacementID(entities []sgb.Entity, next int) int {
	if next == 0 {
		maxID := -1
		for _, entity := range entities {
			if entity.ID > maxID {
				maxID = entity.ID
			}
		}
		next = maxID + 1
		if len(entities) > 0 {
			for candidate := 0; candidate <= maxID; candidate++ {
				used := false
				for _, entity := range entities {
					if entity.ID == candidate {
						used = true
						break
					}
				}
				if !used {
					return candidate
				}
			}
		}
	}
	for {
		used := false
		for _, entity := range entities {
			if entity.ID == next {
				used = true
				break
			}
		}
		if !used {
			return next
		}
		next++
	}
}

func canonicalSgbBlueprintPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "/", `\`))
	path = strings.TrimPrefix(path, `Data\`)
	path = strings.TrimPrefix(path, `data:`)
	path = strings.TrimLeft(path, `\`)
	if !strings.HasSuffix(strings.ToLower(path), ".rgd") {
		path += ".rgd"
	}
	return `data:` + path
}

func sgbPlacementWorldToEntity(point fauxgl.Vector) (x, y, z float32) {
	return float32(point.X), float32(point.Z), float32(point.Y)
}

func sgbScreenPointToWorld(model *whm.Model, camera engine.Camera, canvas image.Point, frameWidth, frameHeight int, point image.Point) (fauxgl.Vector, bool) {
	if frameWidth <= 0 || frameHeight <= 0 || point.X < 0 || point.Y < 0 || point.X >= canvas.X || point.Y >= canvas.Y {
		return fauxgl.Vector{}, false
	}
	scale := math.Min(float64(canvas.X)/float64(frameWidth), float64(canvas.Y)/float64(frameHeight))
	offsetX := (float64(canvas.X) - float64(frameWidth)*scale) / 2
	offsetY := (float64(canvas.Y) - float64(frameHeight)*scale) / 2
	if float64(point.X) < offsetX || float64(point.Y) < offsetY || float64(point.X) >= offsetX+float64(frameWidth)*scale || float64(point.Y) >= offsetY+float64(frameHeight)*scale {
		return fauxgl.Vector{}, false
	}
	framePoint := sgbCanvasToFramePoint(canvas, frameWidth, frameHeight, f32.Pt(float32(point.X), float32(point.Y)))
	return sgbPickTerrainPoint(model, camera, float64(framePoint.X), float64(framePoint.Y))
}

// sgbPickTerrainPoint unprojects a pixel into a world ray and intersects it
// against the terrain triangles only; water and placed objects are excluded.
func sgbPickTerrainPoint(model *whm.Model, camera engine.Camera, pixelX, pixelY float64) (fauxgl.Vector, bool) {
	if model == nil || camera.Width <= 0 || camera.Height <= 0 || len(model.Meshes) == 0 {
		return fauxgl.Vector{}, false
	}
	view := engine.ViewMatrix(camera)
	if math.Abs(view.Determinant()) < 1e-12 {
		return fauxgl.Vector{}, false
	}
	inverse := view.Inverse()
	near := inverse.MulPositionW(fauxgl.Vector{X: 2*pixelX/float64(camera.Width) - 1, Y: 1 - 2*pixelY/float64(camera.Height), Z: -1})
	far := inverse.MulPositionW(fauxgl.Vector{X: 2*pixelX/float64(camera.Width) - 1, Y: 1 - 2*pixelY/float64(camera.Height), Z: 1})
	if math.Abs(near.W) < 1e-12 || math.Abs(far.W) < 1e-12 {
		return fauxgl.Vector{}, false
	}
	origin := fauxgl.Vector{X: near.X / near.W, Y: near.Y / near.W, Z: near.Z / near.W}
	end := fauxgl.Vector{X: far.X / far.W, Y: far.Y / far.W, Z: far.Z / far.W}
	direction := end.Sub(origin)
	closest := math.Inf(1)
	var hitPoint fauxgl.Vector
	for _, mesh := range model.Meshes {
		if mesh.Name != "terrain" {
			continue
		}
		for i := 0; i+2 < len(mesh.Indices); i += 3 {
			a, b, c := mesh.Indices[i], mesh.Indices[i+1], mesh.Indices[i+2]
			if int(a) >= len(model.Vertices) || int(b) >= len(model.Vertices) || int(c) >= len(model.Vertices) {
				continue
			}
			v0 := vectorFromWhm(model.Vertices[a].Pos)
			v1 := vectorFromWhm(model.Vertices[b].Pos)
			v2 := vectorFromWhm(model.Vertices[c].Pos)
			distance, ok := sgbRayTriangle(origin, direction, v0, v1, v2)
			if ok && distance < closest {
				closest = distance
				hitPoint = origin.Add(direction.MulScalar(distance))
			}
		}
	}
	return hitPoint, !math.IsInf(closest, 1)
}

func vectorFromWhm(position [3]float32) fauxgl.Vector {
	return fauxgl.Vector{X: float64(position[0]), Y: float64(position[1]), Z: float64(position[2])}
}

func sgbRayTriangle(origin, direction, a, b, c fauxgl.Vector) (float64, bool) {
	edge1, edge2 := b.Sub(a), c.Sub(a)
	p := direction.Cross(edge2)
	determinant := edge1.Dot(p)
	if determinant > -1e-10 && determinant < 1e-10 {
		return 0, false
	}
	inverse := 1 / determinant
	t := origin.Sub(a)
	u := t.Dot(p) * inverse
	if u < 0 || u > 1 {
		return 0, false
	}
	q := t.Cross(edge1)
	v := direction.Dot(q) * inverse
	if v < 0 || u+v > 1 {
		return 0, false
	}
	distance := edge2.Dot(q) * inverse
	return distance, distance >= 0
}

func sgbEntityIdleAnimationName(tree *modtree.ModTree, m *sgb.Map, e sgb.Entity) string {
	if tree == nil || m == nil || e.BP < 0 || e.BP >= len(m.Blueprints) || e.Name == "" {
		return ""
	}
	rel := strings.ReplaceAll(strings.TrimSpace(e.Name), `\`, "/")
	rel = strings.TrimPrefix(rel, "data:")
	rgdPath := "Data/" + strings.TrimPrefix(rel, "/")
	if !strings.HasSuffix(strings.ToLower(rgdPath), ".rgd") {
		rgdPath += ".rgd"
	}
	data, err := tree.ReadFile(rgdPath)
	if err != nil {
		return ""
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf == nil || rf.Root == nil {
		return ""
	}
	return sgbBlueprintIdleAnimationName(rf)
}

func sgbBlueprintIdleAnimationName(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	sp := rgdFindChild(rf.Root, "strategic_point_ext")
	if sp != nil {
		if relic := rgdFindChild(sp, "is_relic"); relic != nil && relic.Type == rgd.RgdTypeBool && relic.BoolVal {
			return "ol_uncaptured_idle"
		}
		return "uncaptured_idle"
	}
	if objective := rgdFindChild(rf.Root, "is_strategic_objective"); objective != nil && objective.Type == rgd.RgdTypeBool && objective.BoolVal {
		return "uncaptured_idle"
	}
	if objective := rgdFindChild(rgdFindChild(rf.Root, "entity_blueprint_ext"), "is_strategic_objective"); objective != nil && objective.Type == rgd.RgdTypeBool && objective.BoolVal {
		return "uncaptured_idle"
	}
	return ""
}

func sgbLoadEntityModelWithPath(tree *modtree.ModTree, e sgb.Entity) (*whm.Model, string) {
	if tree == nil || e.Name == "" {
		return nil, ""
	}
	rel := strings.ReplaceAll(strings.TrimSpace(e.Name), `\`, "/")
	rel = strings.TrimPrefix(rel, "data:")
	rgdPath := "Data/" + strings.TrimPrefix(rel, "/")
	if !strings.HasSuffix(strings.ToLower(rgdPath), ".rgd") {
		rgdPath += ".rgd"
	}
	data, err := tree.ReadFile(rgdPath)
	if err != nil {
		return nil, ""
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil, ""
	}
	animator := strings.ReplaceAll(strings.TrimSpace(unitAnimatorName(rf)), `\`, "/")
	animator = strings.TrimPrefix(animator, "./")
	if animator == "" {
		return nil, ""
	}
	whmPath := "Data/art/ebps/" + strings.TrimPrefix(animator, "/")
	if !strings.HasSuffix(strings.ToLower(whmPath), ".whm") {
		whmPath += ".whm"
	}
	wdata, err := tree.ReadFile(whmPath)
	if err != nil {
		file := tree.FindByName(filepath.Base(whmPath))
		if file == nil {
			return nil, ""
		}
		wdata, err = tree.ReadTreeFile(file)
		if err != nil {
			return nil, ""
		}
		whmPath = file.FullPath
	}
	parsed, err := ParseWhmModel(wdata)
	if err != nil || parsed == nil || len(parsed.Model.Vertices) == 0 {
		return nil, ""
	}
	return &parsed.Model, whmPath
}
