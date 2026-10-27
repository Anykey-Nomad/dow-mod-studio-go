package sgb

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/unitdef"
	"dow-mod-studio/internal/whm"
	"fmt"
	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
	"image"
	"math"
	"path/filepath"
	"sort"
	"strings"
)

func SceneHasMesh(scene *whm.Model, name string) bool {
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

// SgbSceneAppendEntity appends renderable static geometry to the live map
// scene, then refreshes its bounds for the viewer and GPU renderer.
func SgbSceneAppendEntity(scene, src *whm.Model, placement sgb.Placement, entityID int) bool {
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
	SgbSceneBounds(scene)
	return true
}

// SgbSceneBounds recomputes the merged model's bounds after geometry changes.
func SgbSceneBounds(scene *whm.Model) {
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

// SgbRemoveEntities drops placed entity records and their scene meshes. Vertex
// storage is left intact because the remaining mesh indices are stable; bounds
// are recomputed from referenced vertices so removed geometry no longer affects
// framing or culling.
func SgbRemoveEntities(m *sgb.Map, scene *whm.Model, entityMeshes map[string]int, animated []sgb.AnimatedEntity, selected map[int]bool) ([]sgb.AnimatedEntity, int) {
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
		SgbSceneBounds(scene)
	}
	return keptAnimations, removed
}

// SgbRotateEntities rotates selected placements around map vertical (world Z).
// Static vertices are transformed around each entity's origin; animated models
// use the same heading on their placement matrix.
func SgbRotateEntities(m *sgb.Map, scene *whm.Model, entityMeshes map[string]int, animated []sgb.AnimatedEntity, selected map[int]bool, delta float64) int {
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
		SgbSceneBounds(scene)
	}
	return rotated
}

// SgbMoveEntitiesOnSurface applies a map-plane drag to the selected entities,
// sampling each destination's relief height. Static scene vertices are moved
// in-place; animated instances use their separate placement transforms.
func SgbMoveEntitiesOnSurface(m *sgb.Map, scene *whm.Model, entityMeshes map[string]int, animated []sgb.AnimatedEntity, selected map[int]bool, dx, dy float64) ([]sgb.AnimatedEntity, int) {
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
	dx = engine.ClampFloat64(dx, minDX, maxDX)
	dy = engine.ClampFloat64(dy, minDY, maxDY)
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
		height, ok := SgbTerrainHeightAt(m, newX, newY)
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
		SgbSceneBounds(scene)
	}
	return animated, moved
}

func SgbNextPlacementID(entities []sgb.Entity, next int) int {
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

func CanonicalSgbBlueprintPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "/", `\`))
	path = strings.TrimPrefix(path, `Data\`)
	path = strings.TrimPrefix(path, `data:`)
	path = strings.TrimLeft(path, `\`)
	if !strings.HasSuffix(strings.ToLower(path), ".rgd") {
		path += ".rgd"
	}
	return `data:` + path
}

func SgbPlacementWorldToEntity(point fauxgl.Vector) (x, y, z float32) {
	return float32(point.X), float32(point.Z), float32(point.Y)
}

func SgbScreenPointToWorld(model *whm.Model, camera engine.Camera, canvas image.Point, frameWidth, frameHeight int, point image.Point) (fauxgl.Vector, bool) {
	if frameWidth <= 0 || frameHeight <= 0 || point.X < 0 || point.Y < 0 || point.X >= canvas.X || point.Y >= canvas.Y {
		return fauxgl.Vector{}, false
	}
	scale := math.Min(float64(canvas.X)/float64(frameWidth), float64(canvas.Y)/float64(frameHeight))
	offsetX := (float64(canvas.X) - float64(frameWidth)*scale) / 2
	offsetY := (float64(canvas.Y) - float64(frameHeight)*scale) / 2
	if float64(point.X) < offsetX || float64(point.Y) < offsetY || float64(point.X) >= offsetX+float64(frameWidth)*scale || float64(point.Y) >= offsetY+float64(frameHeight)*scale {
		return fauxgl.Vector{}, false
	}
	framePoint := SgbCanvasToFramePoint(canvas, frameWidth, frameHeight, f32.Pt(float32(point.X), float32(point.Y)))
	return SgbPickTerrainPoint(model, camera, float64(framePoint.X), float64(framePoint.Y))
}

// SgbPickTerrainPoint unprojects a pixel into a world ray and intersects it
// against the terrain triangles only; water and placed objects are excluded.
func SgbPickTerrainPoint(model *whm.Model, camera engine.Camera, pixelX, pixelY float64) (fauxgl.Vector, bool) {
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
			v0 := render.VecFromWhm(model.Vertices[a].Pos)
			v1 := render.VecFromWhm(model.Vertices[b].Pos)
			v2 := render.VecFromWhm(model.Vertices[c].Pos)
			distance, ok := SgbRayTriangle(origin, direction, v0, v1, v2)
			if ok && distance < closest {
				closest = distance
				hitPoint = origin.Add(direction.MulScalar(distance))
			}
		}
	}
	return hitPoint, !math.IsInf(closest, 1)
}

func SgbRayTriangle(origin, direction, a, b, c fauxgl.Vector) (float64, bool) {
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

func SgbEntityIdleAnimationName(tree *modtree.ModTree, m *sgb.Map, e sgb.Entity) string {
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
	return SgbBlueprintIdleAnimationName(rf)
}

func SgbBlueprintIdleAnimationName(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	sp := unitdef.RgdFindChild(rf.Root, "strategic_point_ext")
	if sp != nil {
		if relic := unitdef.RgdFindChild(sp, "is_relic"); relic != nil && relic.Type == rgd.RgdTypeBool && relic.BoolVal {
			return "ol_uncaptured_idle"
		}
		return "uncaptured_idle"
	}
	if objective := unitdef.RgdFindChild(rf.Root, "is_strategic_objective"); objective != nil && objective.Type == rgd.RgdTypeBool && objective.BoolVal {
		return "uncaptured_idle"
	}
	if objective := unitdef.RgdFindChild(unitdef.RgdFindChild(rf.Root, "entity_blueprint_ext"), "is_strategic_objective"); objective != nil && objective.Type == rgd.RgdTypeBool && objective.BoolVal {
		return "uncaptured_idle"
	}
	return ""
}

const SgbEnvironmentRoot = `Data\attrib\ebps\environment`

type SgbEnvironmentEntry struct {
	Folder   *modtree.TreeFolder
	File     *modtree.TreeFile
	Depth    int
	Expanded bool
}

type SgbPlacementRequest = uistate.SgbPlacementRequest

func SgbNewPlayerList() *widget.Enum {
	return &widget.Enum{Value: "neutral"}
}

type SgbObjectModelResult = uistate.SgbObjectModelResult

type SgbObjectMaterialRequest = uistate.SgbObjectMaterialRequest

type SgbObjectMaterialsResult = uistate.SgbObjectMaterialsResult

func SgbEnvironmentEntries(root *modtree.TreeFolder, expanded map[string]bool) []SgbEnvironmentEntry {
	if root == nil {
		return nil
	}
	var entries []SgbEnvironmentEntry
	var walk func(*modtree.TreeFolder, int)
	walk = func(folder *modtree.TreeFolder, depth int) {
		children := append([]*modtree.TreeFolder(nil), folder.Children...)
		sort.Slice(children, func(i, j int) bool { return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name) })
		for _, child := range children {
			if child == nil {
				continue
			}
			open := expanded[child.FullPath]
			entries = append(entries, SgbEnvironmentEntry{Folder: child, Depth: depth, Expanded: open})
			if open {
				walk(child, depth+1)
			}
		}
		files := append([]*modtree.TreeFile(nil), folder.Files...)
		sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
		for _, file := range files {
			if file != nil && strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
				entries = append(entries, SgbEnvironmentEntry{File: file, Depth: depth})
			}
		}
	}
	walk(root, 0)
	return entries
}

func EnsureSgbObjectTree(ui *uistate.State, tab *uistate.Tab) {
	if tab == nil {
		return
	}
	if tab.SgbObjectExpanded == nil {
		tab.SgbObjectExpanded = make(map[string]bool)
	}
	if tab.SgbObjectClicks == nil {
		tab.SgbObjectClicks = make(map[string]*widget.Clickable)
	}
	if tab.SgbObjectRoot == nil && ui.ModTree != nil {
		tab.SgbObjectRoot = ui.ModTree.FindFolder(SgbEnvironmentRoot)
	}
}

func PollSgbObjectModel(ui *uistate.State, tab *uistate.Tab) bool {
	if tab == nil || tab.SgbObjectModelCh == nil {
		return false
	}
	select {
	case result := <-tab.SgbObjectModelCh:
		tab.SgbObjectModelCh = nil
		if strings.EqualFold(tab.SgbObjectSelected, result.Blueprint) {
			tab.SgbObjectModel = result.Model
			tab.SgbObjectModelFile = result.ModelPath
			if result.Model == nil {
				tab.SgbPlacementPending = nil
				ui.StatusText = "Could not load selected environment object " + filepath.Base(result.Blueprint)
			} else if len(result.Model.Meshes) == 0 {
				ui.StatusText = "Selected object has no renderable meshes: " + filepath.Base(result.Blueprint)
			} else {
				StartSgbObjectMaterials(ui, tab, result.Blueprint, result.Model)
			}
		}
		return true
	default:
		return false
	}
}

func ClearSgbObjectSelection(tab *uistate.Tab) {
	if tab == nil {
		return
	}
	tab.SgbObjectSelected = ""
	tab.SgbObjectModel = nil
	tab.SgbObjectModelPath = ""
	tab.SgbObjectModelFile = ""
	tab.SgbPlacementPending = nil
}

type SgbPlayerOption struct {
	Key, Label string
	owner      int
}

var SgbPlayerOptions = func() []SgbPlayerOption {
	options := []SgbPlayerOption{{Key: "neutral", Label: "Neutral", owner: 0}}
	for player := 1; player <= 8; player++ {
		options = append(options, SgbPlayerOption{Key: fmt.Sprintf("player-%d", player), Label: fmt.Sprintf("Player %d", player), owner: player})
	}
	return options
}()

func SgbPlayerOwner(key string) (int, bool) {
	for _, option := range SgbPlayerOptions {
		if option.Key == key {
			return option.owner, true
		}
	}
	return 0, false
}

func LayoutSgbPlayerSelector(ui *uistate.State, gtx layout.Context, tab *uistate.Tab) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, SgbPlayerRadioChildren(ui, tab)...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			button := material.Button(ui.Th, &tab.SgbPlayerSet, "Set")
			button.TextSize = unit.Sp(9)
			button.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(8), Bottom: unit.Dp(5)}.Layout(gtx, button.Layout)
		}),
	)
}

func SgbPlayerRadioChildren(ui *uistate.State, tab *uistate.Tab) []layout.FlexChild {
	children := make([]layout.FlexChild, 0, len(SgbPlayerOptions))
	for _, option := range SgbPlayerOptions {
		option := option
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			radio := material.RadioButton(ui.Th, tab.SgbPlayerList, option.Key, option.Label)
			radio.TextSize = unit.Sp(9)
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(4), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, radio.Layout)
		}))
	}
	return children
}

func SetSelectedSgbOwner(ui *uistate.State, tab *uistate.Tab, owner int) int {
	if tab == nil || tab.SgbMap == nil || len(tab.SgbSelectedEntities) == 0 || owner < 0 || owner > 8 {
		return 0
	}
	changed := 0
	for i := range tab.SgbMap.Entities {
		entity := &tab.SgbMap.Entities[i]
		if !tab.SgbSelectedEntities[entity.ID] {
			continue
		}
		if entity.Owner == owner {
			continue
		}
		entity.Owner = owner
		changed++
	}
	if changed > 0 {
		tab.SgbDirty = true
		ui.StatusText = fmt.Sprintf("Assigned %s to %d selected map object(s)", SgbPlayerOptions[owner].Label, changed)
	} else {
		found := false
		for _, entity := range tab.SgbMap.Entities {
			if tab.SgbSelectedEntities[entity.ID] {
				found = true
				break
			}
		}
		if !found {
			ui.StatusText = "Select one or more map objects first"
		} else {
			ui.StatusText = fmt.Sprintf("Selected map objects already belong to %s", SgbPlayerOptions[owner].Label)
		}
	}
	return changed
}

func StartSgbObjectMaterials(ui *uistate.State, tab *uistate.Tab, blueprint string, model *whm.Model) {
	if tab == nil || model == nil || ui.ModTree == nil {
		return
	}
	QueueSgbObjectMaterials(ui, tab, SgbObjectMaterialRequest{
		Blueprint: blueprint,
		ModelPath: tab.SgbObjectModelFile,
		Model:     model,
	})
}

func QueueSgbObjectMaterials(ui *uistate.State, tab *uistate.Tab, request SgbObjectMaterialRequest) {
	if tab == nil || request.Model == nil {
		return
	}
	key := strings.ToLower(request.ModelPath)
	if result, ok := tab.SgbObjectMaterialsCache[key]; ok {
		ApplySgbObjectMaterials(ui, tab, result)
		return
	}
	if tab.SgbObjectMaterialsLoaded == nil {
		tab.SgbObjectMaterialsLoaded = make(map[string]bool)
	}
	if request.ModelPath != "" && tab.SgbObjectMaterialsLoaded[key] {
		return
	}
	if tab.SgbObjectMaterialsCh != nil && strings.EqualFold(tab.SgbObjectMaterialsActive.ModelPath, request.ModelPath) {
		if request.EntityID != 0 && tab.SgbObjectMaterialsActive.EntityID == 0 {
			tab.SgbObjectMaterialsActive.EntityID = request.EntityID
		}
		return
	}
	for _, queued := range tab.SgbObjectMaterialsPending {
		if strings.EqualFold(queued.ModelPath, request.ModelPath) {
			return
		}
	}
	tab.SgbObjectMaterialsPending = append(tab.SgbObjectMaterialsPending, request)
	LaunchNextSgbObjectMaterials(ui, tab)
}

func LaunchNextSgbObjectMaterials(ui *uistate.State, tab *uistate.Tab) {
	if tab == nil || tab.SgbObjectMaterialsCh != nil || len(tab.SgbObjectMaterialsPending) == 0 || ui.ModTree == nil {
		return
	}
	request := tab.SgbObjectMaterialsPending[0]
	tab.SgbObjectMaterialsPending = tab.SgbObjectMaterialsPending[1:]
	tree := ui.ModTree
	ch := make(chan SgbObjectMaterialsResult, 1)
	tab.SgbObjectMaterialsCh = ch
	tab.SgbObjectMaterialsActive = request
	go func() {
		rsh, materials, missing, diagnostics := SgbEntityMaterials(tree, request.ModelPath, request.Model, "", nil)
		ch <- SgbObjectMaterialsResult{
			Request: request, Materials: materials, Rsh: rsh,
			Diagnostics: diagnostics, Missing: missing,
		}
	}()
}

func PollSgbObjectMaterials(ui *uistate.State, tab *uistate.Tab) bool {
	if tab == nil || tab.SgbObjectMaterialsCh == nil {
		return false
	}
	select {
	case result := <-tab.SgbObjectMaterialsCh:
		tab.SgbObjectMaterialsCh = nil
		tab.SgbObjectMaterialsActive = SgbObjectMaterialRequest{}
		key := strings.ToLower(result.Request.ModelPath)
		active := tab.SgbObjectMaterialsActive
		if tab.SgbObjectMaterialsCache == nil {
			tab.SgbObjectMaterialsCache = make(map[string]SgbObjectMaterialsResult)
		}
		cached := result
		cached.Request.EntityID = 0
		tab.SgbObjectMaterialsCache[key] = cached
		if tab.SgbObjectMaterialsLoaded == nil {
			tab.SgbObjectMaterialsLoaded = make(map[string]bool)
		}
		tab.SgbObjectMaterialsLoaded[key] = true
		result.Request = active
		ApplySgbObjectMaterials(ui, tab, result)
		if result.Missing > 0 {
			ui.StatusText = fmt.Sprintf("Placed object materials: %d texture(s) could not be resolved", result.Missing)
		}
		LaunchNextSgbObjectMaterials(ui, tab)
		return true
	default:
		return false
	}
}

func ApplySgbObjectMaterials(ui *uistate.State, tab *uistate.Tab, result SgbObjectMaterialsResult) {
	if tab == nil || tab.Model == nil {
		return
	}
	if tab.Model.Materials == nil {
		tab.Model.Materials = make(map[string]render.WhmMaterial)
	}
	if tab.Model.RshData == nil {
		tab.Model.RshData = make(map[string][]byte)
	}
	if tab.Model.MaterialDiagnostics == nil {
		tab.Model.MaterialDiagnostics = make(map[string]viewer.ModelMaterialDiagnostic)
	}
	if result.Request.ModelPath != "" && tab.SgbEntityModelFiles != nil {
		for id, modelPath := range tab.SgbEntityModelFiles {
			if !strings.EqualFold(modelPath, result.Request.ModelPath) {
				continue
			}
			prefix := fmt.Sprintf("entity-%d::", id)
			for _, mesh := range result.Request.Model.Meshes {
				if mesh.Material == "" {
					continue
				}
				unique := prefix + mesh.Material
				lookup := mesh.Material
				if _, ok := result.Materials[lookup]; !ok && mesh.MaterialPath != "" {
					lookup = mesh.MaterialPath
				}
				if material, ok := result.Materials[lookup]; ok {
					tab.Model.Materials[unique] = material
				}
				if data, ok := result.Rsh[lookup]; ok {
					tab.Model.RshData[unique] = data
				}
				if diagnostic, ok := result.Diagnostics[lookup]; ok {
					tab.Model.MaterialDiagnostics[unique] = diagnostic
				}
			}
		}
	}
	if len(result.Materials) > 0 {
		tab.Model.Textured = true
	}
	viewer.Invalidate(tab.Model)
}

func DeleteSelectedSgbEntities(ui *uistate.State, tab *uistate.Tab) {
	if tab == nil || tab.SgbMap == nil || tab.Model == nil || len(tab.SgbSelectedEntities) == 0 {
		return
	}
	animated, removed := SgbRemoveEntities(tab.SgbMap, viewer.ModelData(tab.Model), tab.SgbEntityMeshIDs, tab.Model.AnimatedEntities, tab.SgbSelectedEntities)
	if removed == 0 {
		return
	}
	tab.Model.AnimatedEntities = animated
	for id := range tab.SgbSelectedEntities {
		delete(tab.SgbEntityModelFiles, id)
	}
	clear(tab.SgbSelectedEntities)
	viewer.GeometryChangedModelGpu(ui, tab.Model.Model)
	viewer.Invalidate(tab.Model)
	tab.SgbDirty = true
	ui.StatusText = fmt.Sprintf("Deleted %d selected map object(s)", removed)
}

func SgbSelectedPlayerOwner(tab *uistate.Tab) int {
	if tab == nil || tab.SgbPlayerList == nil {
		return 0
	}
	owner, ok := SgbPlayerOwner(tab.SgbPlayerList.Value)
	if !ok {
		return 0
	}
	return owner
}

func AppendSgbPlacement(ui *uistate.State, tab *uistate.Tab, request SgbPlacementRequest, src *whm.Model) {
	if tab == nil || tab.SgbMap == nil || tab.Model == nil || tab.Model.Model == nil || src == nil ||
		!strings.EqualFold(tab.SgbObjectSelected, request.Blueprint) {
		return
	}
	m := tab.SgbMap
	Blueprint := CanonicalSgbBlueprintPath(request.Blueprint)
	bp := -1
	for i, existing := range m.Blueprints {
		if strings.EqualFold(CanonicalSgbBlueprintPath(existing), Blueprint) {
			bp = i
			break
		}
	}
	if bp < 0 {
		bp = len(m.Blueprints)
		m.Blueprints = append(m.Blueprints, Blueprint)
	}
	id := SgbNextPlacementID(m.Entities, tab.SgbNextEntityID)
	x, y, z := SgbPlacementWorldToEntity(request.Point)
	if request.Owner < 0 || request.Owner > 8 {
		ui.StatusText = fmt.Sprintf("Object placement failed: invalid player slot %d", request.Owner)
		return
	}
	entity := sgb.Entity{ID: id, BP: bp, Owner: request.Owner, Name: Blueprint, X: x, Y: y, Z: z}
	if !SgbSceneAppendEntity(viewer.ModelData(tab.Model), src, entity.PlacementOf(), entity.ID) {
		ui.StatusText = "Object placement failed: model has no renderable meshes"
		return
	}
	// The viewer keeps the same uistate.WhmModel pointer as the GPU mesh-cache key.
	// Appending vertices does not change that pointer, so discard the cached
	// buffers before the next frame or the newly placed object stays invisible.
	viewer.GeometryChangedModelGpu(ui, tab.Model.Model)
	tab.SgbNextEntityID = id + 1
	m.Entities = append(m.Entities, entity)
	tab.SgbDirty = true
	if tab.SgbEntityMeshIDs == nil {
		tab.SgbEntityMeshIDs = make(map[string]int)
	}
	tab.SgbEntityMeshIDs[fmt.Sprintf("entity-%d", entity.ID)] = entity.ID
	tab.SgbSelectedEntities = map[int]bool{entity.ID: true}
	if tab.SgbEntityModelFiles == nil {
		tab.SgbEntityModelFiles = make(map[int]string)
	}
	tab.SgbEntityModelFiles[entity.ID] = tab.SgbObjectModelFile
	QueueSgbObjectMaterials(ui, tab, SgbObjectMaterialRequest{
		Blueprint: request.Blueprint,
		ModelPath: tab.SgbObjectModelFile,
		Model:     src,
		EntityID:  entity.ID,
	})
	viewer.Invalidate(tab.Model)
	ui.StatusText = fmt.Sprintf("Placed %s at %.1f, %.1f, %.1f", filepath.Base(request.Blueprint), entity.X, entity.Z, entity.Y)
}
