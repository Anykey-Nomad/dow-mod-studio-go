package main

import (
	"fmt"
	"image"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
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
	const reservedPlacementIDs = 2 // IDs 0 and 1 are reserved by the game.
	if next < reservedPlacementIDs {
		next = reservedPlacementIDs
	}
	for _, entity := range entities {
		if entity.ID >= next {
			next = entity.ID + 1
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

// ─── merged from sgb_objects.go ───────────────────────────────────────────────

const sgbEnvironmentRoot = `Data\attrib\ebps\environment`

type sgbEnvironmentEntry struct {
	folder   *modtree.TreeFolder
	file     *modtree.TreeFile
	depth    int
	expanded bool
}

type sgbPlacementRequest struct {
	blueprint string
	point     fauxgl.Vector
	owner     int
}

func sgbNewPlayerList() *widget.Enum {
	return &widget.Enum{Value: "neutral"}
}

type sgbObjectModelResult struct {
	blueprint string
	modelPath string
	model     *whm.Model
}

type sgbObjectMaterialRequest struct {
	blueprint string
	modelPath string
	model     *whm.Model
	entityID  int
}

type sgbObjectMaterialsResult struct {
	request     sgbObjectMaterialRequest
	materials   map[string]WhmMaterial
	rsh         map[string][]byte
	diagnostics map[string]modelMaterialDiagnostic
	missing     int
}

func sgbEnvironmentEntries(root *modtree.TreeFolder, expanded map[string]bool) []sgbEnvironmentEntry {
	if root == nil {
		return nil
	}
	var entries []sgbEnvironmentEntry
	var walk func(*modtree.TreeFolder, int)
	walk = func(folder *modtree.TreeFolder, depth int) {
		children := append([]*modtree.TreeFolder(nil), folder.Children...)
		sort.Slice(children, func(i, j int) bool { return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name) })
		for _, child := range children {
			if child == nil {
				continue
			}
			open := expanded[child.FullPath]
			entries = append(entries, sgbEnvironmentEntry{folder: child, depth: depth, expanded: open})
			if open {
				walk(child, depth+1)
			}
		}
		files := append([]*modtree.TreeFile(nil), folder.Files...)
		sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
		for _, file := range files {
			if file != nil && strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
				entries = append(entries, sgbEnvironmentEntry{file: file, depth: depth})
			}
		}
	}
	walk(root, 0)
	return entries
}

func (ui *uiState) ensureSgbObjectTree(tab *fileTab) {
	if tab == nil {
		return
	}
	if tab.sgbObjectExpanded == nil {
		tab.sgbObjectExpanded = make(map[string]bool)
	}
	if tab.sgbObjectClicks == nil {
		tab.sgbObjectClicks = make(map[string]*widget.Clickable)
	}
	if tab.sgbObjectRoot == nil && ui.modTree != nil {
		tab.sgbObjectRoot = ui.modTree.FindFolder(sgbEnvironmentRoot)
	}
}

func (ui *uiState) ensureSgbObjectModel(tab *fileTab) {
	if tab == nil || tab.sgbObjectSelected == "" || ui.modTree == nil || tab.sgbObjectModelCh != nil {
		return
	}
	blueprint, tree := tab.sgbObjectSelected, ui.modTree
	ch := make(chan sgbObjectModelResult, 1)
	tab.sgbObjectModelPath = blueprint
	tab.sgbObjectModelCh = ch
	go func() {
		model, modelPath := sgbBlueprintModelWithPath(tree, blueprint)
		ch <- sgbObjectModelResult{blueprint: blueprint, modelPath: modelPath, model: model}
	}()
}

func (ui *uiState) pollSgbObjectModel(tab *fileTab) bool {
	if tab == nil || tab.sgbObjectModelCh == nil {
		return false
	}
	select {
	case result := <-tab.sgbObjectModelCh:
		tab.sgbObjectModelCh = nil
		if strings.EqualFold(tab.sgbObjectSelected, result.blueprint) {
			tab.sgbObjectModel = result.model
			tab.sgbObjectModelFile = result.modelPath
			if result.model == nil {
				tab.sgbPlacementPending = nil
				ui.statusText = "Could not load selected environment object " + filepath.Base(result.blueprint)
			} else if len(result.model.Meshes) == 0 {
				ui.statusText = "Selected object has no renderable meshes: " + filepath.Base(result.blueprint)
			} else {
				ui.startSgbObjectMaterials(tab, result.blueprint, result.model)
			}
		}
		return true
	default:
		return false
	}
}

func clearSgbObjectSelection(tab *fileTab) {
	if tab == nil {
		return
	}
	tab.sgbObjectSelected = ""
	tab.sgbObjectModel = nil
	tab.sgbObjectModelPath = ""
	tab.sgbObjectModelFile = ""
	tab.sgbPlacementPending = nil
}

func (ui *uiState) layoutSgbObjectPanel(gtx layout.Context, tab *fileTab) layout.Dimensions {
	ui.ensureSgbObjectTree(tab)
	if tab.sgbObjectSelected != "" && tab.sgbObjectModel == nil {
		ui.ensureSgbObjectModel(tab)
	}
	if tab.sgbObjectModelCh != nil || tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) > 0 || tab.sgbPlacementPending != nil {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	if tab.sgbObjectClear.Clicked(gtx) {
		clearSgbObjectSelection(tab)
		ui.statusText = "Object placement cancelled"
	}
	if tab.sgbPlayerList == nil {
		tab.sgbPlayerList = sgbNewPlayerList()
	}
	if tab.sgbPlayerSet.Clicked(gtx) {
		if owner, ok := sgbPlayerOwner(tab.sgbPlayerList.Value); ok {
			ui.setSelectedSgbOwner(tab, owner)
		}
	}
	entries := sgbEnvironmentEntries(tab.sgbObjectRoot, tab.sgbObjectExpanded)
	return ui.panelWithScroll(gtx, &tab.sgbObjectScroll, func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Object Owner")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSgbPlayerSelector(gtx, tab)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hLine(gtx, cHeaderLine, 1)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Objects — EBPS Environment")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				msg := "Select an object, then click the map surface to place it."
				if tab.sgbObjectSelected != "" {
					msg = "Selected: " + filepath.Base(tab.sgbObjectSelected)
					if tab.sgbObjectModelCh != nil {
						msg += " (loading…)"
					}
				}
				l := material.Label(ui.th, unit.Sp(9), msg)
				l.Color = cTextDim
				l.MaxLines = 2
				return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
		)
		if tab.sgbObjectSelected != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.Button(ui.th, &tab.sgbObjectClear, "Clear selection")
				button.TextSize = unit.Sp(9)
				button.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(5)}.Layout(gtx, button.Layout)
			}))
		}
		if tab.sgbObjectRoot == nil {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutClassicOEHint(gtx, "", "Data/attrib/ebps/environment was not found in this module.")
			}))
		} else if len(entries) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutClassicOEHint(gtx, "", "Expand a folder to browse environment objects.")
			}))
		} else {
			for _, entry := range entries {
				entry := entry
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutSgbEnvironmentRow(gtx, tab, entry)
				}))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

type sgbPlayerOption struct {
	key, label string
	owner      int
}

var sgbPlayerOptions = func() []sgbPlayerOption {
	options := []sgbPlayerOption{{key: "neutral", label: "Neutral", owner: 0}}
	for player := 1; player <= 8; player++ {
		options = append(options, sgbPlayerOption{key: fmt.Sprintf("player-%d", player), label: fmt.Sprintf("Player %d", player), owner: player})
	}
	return options
}()

func sgbPlayerOwner(key string) (int, bool) {
	for _, option := range sgbPlayerOptions {
		if option.key == key {
			return option.owner, true
		}
	}
	return 0, false
}

func (ui *uiState) layoutSgbPlayerSelector(gtx layout.Context, tab *fileTab) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, sgbPlayerRadioChildren(ui, tab)...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			button := material.Button(ui.th, &tab.sgbPlayerSet, "Set")
			button.TextSize = unit.Sp(9)
			button.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(8), Bottom: unit.Dp(5)}.Layout(gtx, button.Layout)
		}),
	)
}

func sgbPlayerRadioChildren(ui *uiState, tab *fileTab) []layout.FlexChild {
	children := make([]layout.FlexChild, 0, len(sgbPlayerOptions))
	for _, option := range sgbPlayerOptions {
		option := option
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			radio := material.RadioButton(ui.th, tab.sgbPlayerList, option.key, option.label)
			radio.TextSize = unit.Sp(9)
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(4), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, radio.Layout)
		}))
	}
	return children
}

func (ui *uiState) setSelectedSgbOwner(tab *fileTab, owner int) int {
	if tab == nil || tab.sgbMap == nil || len(tab.sgbSelectedEntities) == 0 || owner < 0 || owner > 8 {
		return 0
	}
	changed := 0
	for i := range tab.sgbMap.Entities {
		entity := &tab.sgbMap.Entities[i]
		if !tab.sgbSelectedEntities[entity.ID] {
			continue
		}
		if entity.Owner == owner {
			continue
		}
		entity.Owner = owner
		changed++
	}
	if changed > 0 {
		tab.sgbDirty = true
		ui.statusText = fmt.Sprintf("Assigned %s to %d selected map object(s)", sgbPlayerOptions[owner].label, changed)
	} else {
		found := false
		for _, entity := range tab.sgbMap.Entities {
			if tab.sgbSelectedEntities[entity.ID] {
				found = true
				break
			}
		}
		if !found {
			ui.statusText = "Select one or more map objects first"
		} else {
			ui.statusText = fmt.Sprintf("Selected map objects already belong to %s", sgbPlayerOptions[owner].label)
		}
	}
	return changed
}

func (ui *uiState) layoutSgbEnvironmentRow(gtx layout.Context, tab *fileTab, entry sgbEnvironmentEntry) layout.Dimensions {
	folder := entry.folder != nil
	key, label := "", ""
	if folder {
		key, label = "folder:"+entry.folder.FullPath, entry.folder.Name
	} else if entry.file != nil {
		key, label = "file:"+entry.file.FullPath, strings.TrimSuffix(entry.file.Name, filepath.Ext(entry.file.Name))
	} else {
		return layout.Dimensions{}
	}
	click := tab.sgbObjectClicks[key]
	if click == nil {
		click = &widget.Clickable{}
		tab.sgbObjectClicks[key] = click
	}
	if click.Clicked(gtx) {
		if folder {
			tab.sgbObjectExpanded[entry.folder.FullPath] = !entry.expanded
		} else {
			if strings.EqualFold(tab.sgbObjectSelected, entry.file.FullPath) {
				clearSgbObjectSelection(tab)
				ui.statusText = "Object placement cancelled"
			} else {
				tab.sgbObjectSelected = entry.file.FullPath
				tab.sgbObjectModel = nil
				tab.sgbObjectModelPath = ""
				tab.sgbObjectModelFile = ""
				tab.sgbPlacementPending = nil
				ui.ensureSgbObjectModel(tab)
				ui.statusText = "Loading map object " + filepath.Base(entry.file.FullPath)
			}
		}
	}
	selected := !folder && strings.EqualFold(tab.sgbObjectSelected, entry.file.FullPath)
	bg := cPanelBg
	if selected {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	h := gtx.Dp(unit.Dp(22))
	gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
	gtx.Constraints.Max = image.Pt(gtx.Constraints.Max.X, h)
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		defer func() {
			if folder {
				return
			}
			rect := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			rect.Pop()
		}()
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				marker := ""
				if folder {
					marker = "▸"
					if entry.expanded {
						marker = "▾"
					}
				}
				return layout.Inset{Left: unit.Dp(6 + entry.depth*12), Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), marker)
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), label)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) placeSgbObject(tab *fileTab, point fauxgl.Vector) {
	if tab == nil || tab.model == nil || tab.sgbMap == nil || tab.sgbObjectSelected == "" {
		return
	}
	if tab.model.loading || tab.modelMaterialCh != nil || tab.model.rendering || tab.model.frameCh != nil || tab.model.gpuRetries > 0 || tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) > 0 {
		tab.sgbPlacementPending = &sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}
		ui.statusText = "Object placement — waiting for the map scene to finish loading"
		return
	}
	if !strings.EqualFold(tab.sgbObjectModelPath, tab.sgbObjectSelected) || tab.sgbObjectModel == nil {
		tab.sgbPlacementPending = &sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}
		ui.ensureSgbObjectModel(tab)
		ui.statusText = "Loading selected environment object…"
		return
	}
	if tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) > 0 {
		tab.sgbPlacementPending = &sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}
		ui.statusText = "Loading selected object materials…"
		return
	}
	ui.appendSgbPlacement(tab, sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}, tab.sgbObjectModel)
}

func (ui *uiState) startSgbObjectMaterials(tab *fileTab, blueprint string, model *whm.Model) {
	if tab == nil || model == nil || ui.modTree == nil {
		return
	}
	ui.queueSgbObjectMaterials(tab, sgbObjectMaterialRequest{
		blueprint: blueprint,
		modelPath: tab.sgbObjectModelFile,
		model:     model,
	})
}

func (ui *uiState) queueSgbObjectMaterials(tab *fileTab, request sgbObjectMaterialRequest) {
	if tab == nil || request.model == nil {
		return
	}
	key := strings.ToLower(request.modelPath)
	if result, ok := tab.sgbObjectMaterialsCache[key]; ok {
		ui.applySgbObjectMaterials(tab, result)
		return
	}
	if tab.sgbObjectMaterialsLoaded == nil {
		tab.sgbObjectMaterialsLoaded = make(map[string]bool)
	}
	if request.modelPath != "" && tab.sgbObjectMaterialsLoaded[key] {
		return
	}
	if tab.sgbObjectMaterialsCh != nil && strings.EqualFold(tab.sgbObjectMaterialsActive.modelPath, request.modelPath) {
		if request.entityID != 0 && tab.sgbObjectMaterialsActive.entityID == 0 {
			tab.sgbObjectMaterialsActive.entityID = request.entityID
		}
		return
	}
	for _, queued := range tab.sgbObjectMaterialsPending {
		if strings.EqualFold(queued.modelPath, request.modelPath) {
			return
		}
	}
	tab.sgbObjectMaterialsPending = append(tab.sgbObjectMaterialsPending, request)
	ui.launchNextSgbObjectMaterials(tab)
}

func (ui *uiState) launchNextSgbObjectMaterials(tab *fileTab) {
	if tab == nil || tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) == 0 || ui.modTree == nil {
		return
	}
	request := tab.sgbObjectMaterialsPending[0]
	tab.sgbObjectMaterialsPending = tab.sgbObjectMaterialsPending[1:]
	tree := ui.modTree
	ch := make(chan sgbObjectMaterialsResult, 1)
	tab.sgbObjectMaterialsCh = ch
	tab.sgbObjectMaterialsActive = request
	go func() {
		rsh, materials, missing, diagnostics := sgbEntityMaterials(tree, request.modelPath, request.model, "", nil)
		ch <- sgbObjectMaterialsResult{
			request: request, materials: materials, rsh: rsh,
			diagnostics: diagnostics, missing: missing,
		}
	}()
}

func (ui *uiState) pollSgbObjectMaterials(tab *fileTab) bool {
	if tab == nil || tab.sgbObjectMaterialsCh == nil {
		return false
	}
	select {
	case result := <-tab.sgbObjectMaterialsCh:
		tab.sgbObjectMaterialsCh = nil
		tab.sgbObjectMaterialsActive = sgbObjectMaterialRequest{}
		key := strings.ToLower(result.request.modelPath)
		active := tab.sgbObjectMaterialsActive
		if tab.sgbObjectMaterialsCache == nil {
			tab.sgbObjectMaterialsCache = make(map[string]sgbObjectMaterialsResult)
		}
		cached := result
		cached.request.entityID = 0
		tab.sgbObjectMaterialsCache[key] = cached
		if tab.sgbObjectMaterialsLoaded == nil {
			tab.sgbObjectMaterialsLoaded = make(map[string]bool)
		}
		tab.sgbObjectMaterialsLoaded[key] = true
		result.request = active
		ui.applySgbObjectMaterials(tab, result)
		if result.missing > 0 {
			ui.statusText = fmt.Sprintf("Placed object materials: %d texture(s) could not be resolved", result.missing)
		}
		ui.launchNextSgbObjectMaterials(tab)
		return true
	default:
		return false
	}
}

func (ui *uiState) applySgbObjectMaterials(tab *fileTab, result sgbObjectMaterialsResult) {
	if tab == nil || tab.model == nil {
		return
	}
	if tab.model.materials == nil {
		tab.model.materials = make(map[string]WhmMaterial)
	}
	if tab.model.rshData == nil {
		tab.model.rshData = make(map[string][]byte)
	}
	if tab.model.materialDiagnostics == nil {
		tab.model.materialDiagnostics = make(map[string]modelMaterialDiagnostic)
	}
	if result.request.modelPath != "" && tab.sgbEntityModelFiles != nil {
		for id, modelPath := range tab.sgbEntityModelFiles {
			if !strings.EqualFold(modelPath, result.request.modelPath) {
				continue
			}
			prefix := fmt.Sprintf("entity-%d::", id)
			for _, mesh := range result.request.model.Meshes {
				if mesh.Material == "" {
					continue
				}
				unique := prefix + mesh.Material
				lookup := mesh.Material
				if _, ok := result.materials[lookup]; !ok && mesh.MaterialPath != "" {
					lookup = mesh.MaterialPath
				}
				if material, ok := result.materials[lookup]; ok {
					tab.model.materials[unique] = material
				}
				if data, ok := result.rsh[lookup]; ok {
					tab.model.rshData[unique] = data
				}
				if diagnostic, ok := result.diagnostics[lookup]; ok {
					tab.model.materialDiagnostics[unique] = diagnostic
				}
			}
		}
	}
	if len(result.materials) > 0 {
		tab.model.textured = true
	}
	tab.model.invalidate()
}

func (ui *uiState) deleteSelectedSgbEntities(tab *fileTab) {
	if tab == nil || tab.sgbMap == nil || tab.model == nil || len(tab.sgbSelectedEntities) == 0 {
		return
	}
	animated, removed := sgbRemoveEntities(tab.sgbMap, tab.model.modelData(), tab.sgbEntityMeshIDs, tab.model.animatedEntities, tab.sgbSelectedEntities)
	if removed == 0 {
		return
	}
	tab.model.animatedEntities = animated
	for id := range tab.sgbSelectedEntities {
		delete(tab.sgbEntityModelFiles, id)
	}
	clear(tab.sgbSelectedEntities)
	ui.geometryChangedModelGpu(tab.model.model)
	tab.model.invalidate()
	tab.sgbDirty = true
	ui.statusText = fmt.Sprintf("Deleted %d selected map object(s)", removed)
}

func sgbSelectedPlayerOwner(tab *fileTab) int {
	if tab == nil || tab.sgbPlayerList == nil {
		return 0
	}
	owner, ok := sgbPlayerOwner(tab.sgbPlayerList.Value)
	if !ok {
		return 0
	}
	return owner
}

func (ui *uiState) appendSgbPlacement(tab *fileTab, request sgbPlacementRequest, src *whm.Model) {
	if tab == nil || tab.sgbMap == nil || tab.model == nil || tab.model.model == nil || src == nil ||
		!strings.EqualFold(tab.sgbObjectSelected, request.blueprint) {
		return
	}
	m := tab.sgbMap
	blueprint := canonicalSgbBlueprintPath(request.blueprint)
	bp := -1
	for i, existing := range m.Blueprints {
		if strings.EqualFold(canonicalSgbBlueprintPath(existing), blueprint) {
			bp = i
			break
		}
	}
	if bp < 0 {
		bp = len(m.Blueprints)
		m.Blueprints = append(m.Blueprints, blueprint)
	}
	id := sgbNextPlacementID(m.Entities, tab.sgbNextEntityID)
	x, y, z := sgbPlacementWorldToEntity(request.point)
	if request.owner < 0 || request.owner > 8 {
		ui.statusText = fmt.Sprintf("Object placement failed: invalid player slot %d", request.owner)
		return
	}
	entity := sgb.Entity{ID: id, BP: bp, Owner: request.owner, Name: blueprint, X: x, Y: y, Z: z}
	if !sgbSceneAppendEntity(tab.model.modelData(), src, entity.PlacementOf(), entity.ID) {
		ui.statusText = "Object placement failed: model has no renderable meshes"
		return
	}
	// The viewer keeps the same WhmModel pointer as the GPU mesh-cache key.
	// Appending vertices does not change that pointer, so discard the cached
	// buffers before the next frame or the newly placed object stays invisible.
	ui.geometryChangedModelGpu(tab.model.model)
	tab.sgbNextEntityID = id + 1
	m.Entities = append(m.Entities, entity)
	tab.sgbDirty = true
	if tab.sgbEntityMeshIDs == nil {
		tab.sgbEntityMeshIDs = make(map[string]int)
	}
	tab.sgbEntityMeshIDs[fmt.Sprintf("entity-%d", entity.ID)] = entity.ID
	tab.sgbSelectedEntities = map[int]bool{entity.ID: true}
	if tab.sgbEntityModelFiles == nil {
		tab.sgbEntityModelFiles = make(map[int]string)
	}
	tab.sgbEntityModelFiles[entity.ID] = tab.sgbObjectModelFile
	ui.queueSgbObjectMaterials(tab, sgbObjectMaterialRequest{
		blueprint: request.blueprint,
		modelPath: tab.sgbObjectModelFile,
		model:     src,
		entityID:  entity.ID,
	})
	tab.model.invalidate()
	ui.statusText = fmt.Sprintf("Placed %s at %.1f, %.1f, %.1f", filepath.Base(request.blueprint), entity.X, entity.Z, entity.Y)
}

func sgbBlueprintModel(tree *modtree.ModTree, blueprintPath string) *whm.Model {
	model, _ := sgbBlueprintModelWithPath(tree, blueprintPath)
	return model
}

func sgbBlueprintModelWithPath(tree *modtree.ModTree, blueprintPath string) (*whm.Model, string) {
	if tree == nil || blueprintPath == "" {
		return nil, ""
	}
	path := strings.ReplaceAll(strings.TrimSpace(blueprintPath), "\\", "/")
	path = strings.TrimPrefix(path, "data:")
	path = strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(strings.ToLower(path), "data/") {
		path = "Data/" + path
	}
	if !strings.HasSuffix(strings.ToLower(path), ".rgd") {
		path += ".rgd"
	}
	data, err := tree.ReadFile(path)
	if err != nil {
		return nil, ""
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf == nil || rf.Root == nil {
		return nil, ""
	}
	animator := strings.ReplaceAll(strings.TrimSpace(unitAnimatorName(rf)), "\\", "/")
	animator = strings.TrimPrefix(animator, "./")
	if animator == "" {
		return nil, ""
	}
	modelPath := "Data/art/ebps/" + strings.TrimPrefix(animator, "/")
	if !strings.HasSuffix(strings.ToLower(modelPath), ".whm") {
		modelPath += ".whm"
	}
	modelData, err := tree.ReadFile(modelPath)
	if err != nil {
		file := tree.FindByName(filepath.Base(modelPath))
		if file == nil {
			return nil, ""
		}
		modelData, err = tree.ReadTreeFile(file)
		if err != nil {
			return nil, ""
		}
		modelPath = file.FullPath
	}
	parsed, err := ParseWhmModel(modelData)
	if err != nil || parsed == nil || len(parsed.Model.Vertices) == 0 {
		return nil, ""
	}
	return &parsed.Model, modelPath
}
