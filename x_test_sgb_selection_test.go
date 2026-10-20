package main

import (
	"image"
	"math"
	"testing"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/f32"
	"gioui.org/io/key"
	"github.com/fogleman/fauxgl"
)

func TestSgbPlayerSelectionDefaultsToNeutralAndAssignsSelectedEntities(t *testing.T) {
	playerList := sgbNewPlayerList()
	if playerList.Value != "neutral" {
		t.Fatalf("default selected player = %q, want neutral", playerList.Value)
	}
	if got := sgbSelectedPlayerOwner(&fileTab{sgbPlayerList: playerList}); got != 0 {
		t.Fatalf("default owner = %d, want neutral slot 0", got)
	}
	if len(sgbPlayerOptions) != 9 {
		t.Fatalf("player options = %d, want Neutral plus players 1–8", len(sgbPlayerOptions))
	}
	for owner, option := range sgbPlayerOptions {
		got, ok := sgbPlayerOwner(option.key)
		if !ok || got != owner {
			t.Errorf("option %q maps to owner %d (valid=%v), want %d", option.key, got, ok, owner)
		}
	}

	playerList.Value = "player-8"
	if got := sgbSelectedPlayerOwner(&fileTab{sgbPlayerList: playerList}); got != 8 {
		t.Fatalf("selected Player 8 owner = %d, want 8", got)
	}

	tab := &fileTab{
		sgbMap:              &sgb.Map{Entities: []sgb.Entity{{ID: 1, Owner: 1}, {ID: 2, Owner: 2}, {ID: 3, Owner: 3}}},
		sgbSelectedEntities: map[int]bool{1: true, 3: true},
	}
	ui := &uiState{}
	if got := ui.setSelectedSgbOwner(tab, 8); got != 2 {
		t.Fatalf("changed entities = %d, want 2", got)
	}
	if !tab.sgbDirty || tab.sgbMap.Entities[0].Owner != 8 || tab.sgbMap.Entities[1].Owner != 2 || tab.sgbMap.Entities[2].Owner != 8 {
		t.Fatalf("owner assignment changed unexpected state: dirty=%v entities=%+v", tab.sgbDirty, tab.sgbMap.Entities)
	}
}

func TestSgbBlueprintIdleAnimationName(t *testing.T) {
	makeBlueprint := func(children ...*rgd.RgdEntry) *rgd.RgdFile {
		return &rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData", Type: rgd.RgdTypeTable, Children: children}}
	}
	strategicPoint := func(isRelic bool) *rgd.RgdEntry {
		return &rgd.RgdEntry{Name: "strategic_point_ext", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{{Name: "is_relic", Type: rgd.RgdTypeBool, BoolVal: isRelic}}}
	}
	cases := []struct {
		name string
		rgd  *rgd.RgdFile
		want string
	}{
		{"strategic_point", makeBlueprint(strategicPoint(false)), "uncaptured_idle"},
		{"strategic_objective", makeBlueprint(&rgd.RgdEntry{Name: "strategic_point_ext", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{{Name: "is_strategic_objective", Type: rgd.RgdTypeBool, BoolVal: true}}}), "uncaptured_idle"},
		{"relic", makeBlueprint(strategicPoint(true)), "ol_uncaptured_idle"},
		{"unrelated", makeBlueprint(), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sgbBlueprintIdleAnimationName(tc.rgd); got != tc.want {
				t.Fatalf("idle animation = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSgbCanvasToFramePointAccountsForLetterboxing(t *testing.T) {
	got := sgbCanvasToFramePoint(image.Pt(800, 600), 400, 400, f32.Pt(400, 300))
	if want := image.Pt(200, 200); got != want {
		t.Fatalf("center maps to %v, want %v", got, want)
	}
	got = sgbCanvasToFramePoint(image.Pt(800, 600), 400, 400, f32.Pt(0, 100))
	if want := image.Pt(0, 67); got != want {
		t.Fatalf("left edge of frame maps to %v, want %v", got, want)
	}
}

func TestSgbPickTerrainPointUsesActualReliefTriangle(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-10, -10, 0}}, {Pos: [3]float32{10, -10, 0}},
			{Pos: [3]float32{-10, 10, 0}}, {Pos: [3]float32{10, 10, 0}},
			{Pos: [3]float32{-10, -10, -1}}, {Pos: [3]float32{10, -10, -1}}, {Pos: [3]float32{-10, 10, -1}},
		},
		Meshes: []whm.Mesh{
			{Name: "terrain", Indices: []uint32{0, 1, 3, 0, 3, 2}},
			{Name: sgb.WaterMeshName, Indices: []uint32{4, 5, 6}},
			{Name: "entity-3", Indices: []uint32{4, 5, 6}},
		},
	}
	camera := engine.Camera{Width: 100, Height: 100, Yaw: 0, Pitch: math.Pi / 2, Distance: 20, Target: fauxgl.Vector{}, Fov: 60}
	point, hit := sgbPickTerrainPoint(model, camera, 50, 50)
	if !hit || math.Abs(point.X) > 1e-6 || math.Abs(point.Y) > 1e-6 || math.Abs(point.Z) > 1e-6 {
		t.Fatalf("center terrain hit = %v (hit=%v), want map origin on terrain z=0", point, hit)
	}
	if _, hit := sgbPickTerrainPoint(model, camera, 0, 0); hit {
		t.Fatal("ray outside the terrain bounds reported a hit")
	}
	if _, hit := sgbPickTerrainPoint(&whm.Model{Vertices: model.Vertices, Meshes: model.Meshes[1:]}, camera, 50, 50); hit {
		t.Fatal("water or entity geometry was accepted as terrain")
	}
}

func TestSgbScreenPointToWorldAccountsForCanvasLetterboxing(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-20, -20, 0}}, {Pos: [3]float32{20, -20, 0}},
			{Pos: [3]float32{-20, 20, 0}}, {Pos: [3]float32{20, 20, 0}},
		},
		Meshes: []whm.Mesh{{Name: "terrain", Indices: []uint32{0, 1, 3, 0, 3, 2}}},
	}
	camera := engine.Camera{Width: 100, Height: 100, Yaw: 0, Pitch: math.Pi / 2, Distance: 20, Target: fauxgl.Vector{}, Fov: 60}
	point, hit := sgbScreenPointToWorld(model, camera, image.Pt(200, 100), 100, 100, image.Pt(100, 50))
	if !hit || math.Abs(point.X) > 1e-6 || math.Abs(point.Y) > 1e-6 {
		t.Fatalf("letterboxed center maps to %v (hit=%v), want origin", point, hit)
	}
	if _, hit := sgbScreenPointToWorld(model, camera, image.Pt(200, 100), 100, 100, image.Pt(0, 0)); hit {
		t.Fatal("canvas point outside the displayed frame was pickable")
	}
}

func TestSgbPlacementBlueprintPathAndEntityID(t *testing.T) {
	if got, want := canonicalSgbBlueprintPath(`Data/attrib/ebps/environment/rocks/boulder.rgd`), `data:attrib\ebps\environment\rocks\boulder.rgd`; got != want {
		t.Fatalf("canonical blueprint = %q, want %q", got, want)
	}
	entities := []sgb.Entity{{ID: 0}, {ID: 2}, {ID: 4}}
	if got := sgbNextPlacementID(entities, 0); got != 1 {
		t.Fatalf("next placement ID = %d, want 1", got)
	}
	if got := sgbNextPlacementID(entities, 3); got != 3 {
		t.Fatalf("next placement ID after cached value = %d, want 3", got)
	}
}

func TestSgbSceneAppendEntityUpdatesSceneGeometryAndBounds(t *testing.T) {
	scene := &whm.Model{
		Vertices: []whm.Vertex{{Pos: [3]float32{-2, -2, 0}}, {Pos: [3]float32{2, -2, 0}}, {Pos: [3]float32{-2, 2, 0}}},
		Meshes:   []whm.Mesh{{Name: "terrain", Indices: []uint32{0, 1, 2}}},
		BoundMin: [3]float32{-2, -2, 0}, BoundMax: [3]float32{2, 2, 0},
	}
	source := &whm.Model{
		Vertices: []whm.Vertex{{Pos: [3]float32{-1, -1, 0}}, {Pos: [3]float32{1, -1, 0}}, {Pos: [3]float32{0, 1, 2}}},
		Meshes:   []whm.Mesh{{Name: "building", Material: "reused-label", MaterialPath: "art/shared/blue", Indices: []uint32{0, 1, 2}}},
	}
	placement := sgb.Placement{X: 10, Y: 20, Z: 3, ScaleX: 1, ScaleY: 1, ScaleZ: 1}
	if !sgbSceneAppendEntity(scene, source, placement, 7) {
		t.Fatal("renderable entity was rejected")
	}
	if len(scene.Meshes) != 2 || scene.Meshes[1].Name != "entity-7" {
		t.Fatalf("scene meshes = %+v, want appended entity-7", scene.Meshes)
	}
	if scene.Meshes[1].Material != "entity-7::reused-label" || scene.Meshes[1].MaterialPath != "art/shared/blue" {
		t.Fatalf("mesh material keys = %q / %q, want entity key and authored diffuse path", scene.Meshes[1].Material, scene.Meshes[1].MaterialPath)
	}
	if scene.BoundMin != [3]float32{-2, -2, 0} || scene.BoundMax != [3]float32{11, 21, 5} {
		t.Fatalf("scene bounds %v..%v, want [-2 -2 0]..[11 21 5]", scene.BoundMin, scene.BoundMax)
	}
	beforeVertices, beforeMeshes := len(scene.Vertices), len(scene.Meshes)
	if sgbSceneAppendEntity(scene, &whm.Model{Vertices: source.Vertices, Meshes: []whm.Mesh{{Name: "empty"}}}, placement, 8) {
		t.Fatal("model without triangles should be rejected")
	}
	if len(scene.Vertices) != beforeVertices || len(scene.Meshes) != beforeMeshes {
		t.Fatal("rejected object modified the scene")
	}
}

func TestClearSgbObjectSelectionCancelsPendingPlacement(t *testing.T) {
	tab := &fileTab{
		sgbObjectSelected:   `Data\\attrib\\ebps\\environment\\rocks\\boulder.rgd`,
		sgbObjectModelPath:  `Data\\attrib\\ebps\\environment\\rocks\\boulder.rgd`,
		sgbObjectModelFile:  `Data\\art\\ebps\\environment\\rocks\\boulder.whm`,
		sgbObjectModel:      &whm.Model{Name: "boulder"},
		sgbPlacementPending: &sgbPlacementRequest{blueprint: "boulder"},
	}
	clearSgbObjectSelection(tab)
	if tab.sgbObjectSelected != "" || tab.sgbObjectModelPath != "" || tab.sgbObjectModelFile != "" || tab.sgbObjectModel != nil || tab.sgbPlacementPending != nil {
		t.Fatalf("cleared selection retained placement state: %+v", tab)
	}
	clearSgbObjectSelection(nil)
}

func TestSgbEnvironmentEntriesRespectExpandableFolders(t *testing.T) {
	root := modtree.NewFolder("environment", sgbEnvironmentRoot, nil)
	rocks := modtree.NewFolder("rocks", sgbEnvironmentRoot+`\rocks`, root)
	boulder := &modtree.TreeFile{Name: "boulder.rgd", FullPath: rocks.FullPath + `\boulder.rgd`}
	root.Children = []*modtree.TreeFolder{rocks}
	rocks.Files = []*modtree.TreeFile{boulder}
	closed := sgbEnvironmentEntries(root, map[string]bool{})
	if len(closed) != 1 || closed[0].folder != rocks {
		t.Fatalf("closed-folder entries = %+v, want only rocks folder", closed)
	}
	open := sgbEnvironmentEntries(root, map[string]bool{rocks.FullPath: true})
	if len(open) != 2 || open[1].file != boulder || open[1].depth != 1 {
		t.Fatalf("expanded-folder entries = %+v, want nested boulder at depth 1", open)
	}
}

func TestSgbClickPicksNearestEntityTriangle(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-1, 0, -1}}, {Pos: [3]float32{1, 0, -1}}, {Pos: [3]float32{0, 0, 1}},
			{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, -1, -1}}, {Pos: [3]float32{0, -1, 1}},
		},
		Meshes: []whm.Mesh{
			{Name: "entity-10", Indices: []uint32{0, 1, 2}},
			{Name: "entity-20", Indices: []uint32{3, 4, 5}},
			{Name: sgb.WaterMeshName, Indices: []uint32{0, 1, 2}},
		},
	}
	got, ok := sgbPickEntity(model, map[string]int{"entity-10": 10, "entity-20": 20, sgb.WaterMeshName: 99}, engine.Camera{Width: 100, Height: 100, Yaw: math.Pi / 2, Distance: 2, Fov: 90}, 50, 50)
	if !ok || got != 10 {
		t.Fatalf("center ray picked entity %d (hit=%v), want nearest entity 10, not the water plane", got, ok)
	}
	waterOnly := &whm.Model{
		Vertices: model.Vertices[:3],
		Meshes:   []whm.Mesh{{Name: sgb.WaterMeshName, Indices: []uint32{0, 1, 2}}},
	}
	if _, ok := sgbPickEntity(waterOnly, map[string]int{sgb.WaterMeshName: 99}, engine.Camera{Width: 100, Height: 100, Yaw: math.Pi / 2, Distance: 2, Fov: 90}, 50, 50); ok {
		t.Fatal("water plane was pickable as an entity")
	}
	if _, ok := sgbPickEntity(model, map[string]int{"entity-10": 10}, engine.Camera{Width: 100, Height: 100, Yaw: math.Pi / 2, Distance: 2, Fov: 90}, 0, 0); ok {
		t.Fatal("corner ray picked a triangle it misses")
	}
}

func TestSgbRectangleSelectionIncludesProjectedBoundsOverlap(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-0.8, -0.8, 0}}, {Pos: [3]float32{-0.2, -0.8, 0}}, {Pos: [3]float32{-0.5, -0.2, 0}},
			{Pos: [3]float32{0.2, 0.2, 0}}, {Pos: [3]float32{0.8, 0.2, 0}}, {Pos: [3]float32{0.5, 0.8, 0}},
		},
		Meshes: []whm.Mesh{
			{Name: "entity-10", Indices: []uint32{0, 1, 2}},
			{Name: "entity-20", Indices: []uint32{3, 4, 5}},
			{Name: sgb.WaterMeshName, Indices: []uint32{0, 1, 2}},
		},
	}
	selected := sgbRectangleEntities(model, map[string]int{"entity-10": 10, "entity-20": 20, sgb.WaterMeshName: 99}, fauxgl.Identity(), 100, 100, image.Rect(8, 55, 45, 95))
	if len(selected) != 1 || !selected[10] || selected[20] {
		t.Fatalf("selection=%v, want only entity 10", selected)
	}
	if got := sgbRectangleEntities(model, map[string]int{"entity-10": 10}, fauxgl.Identity(), 100, 100, image.Rect(45, 95, 8, 55)); len(got) != 1 || !got[10] {
		t.Fatalf("reverse-direction rectangle selection=%v, want entity 10", got)
	}
}

func TestSgbRectangleSelectsTriangleCrossingBoxWithoutVertexInside(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-0.8, 0, 0}}, {Pos: [3]float32{0.8, 0, 0}}, {Pos: [3]float32{0, 0.8, 0}},
		},
		Meshes: []whm.Mesh{{Name: "entity-7", Indices: []uint32{0, 1, 2}}},
	}
	model.Meshes = append(model.Meshes, whm.Mesh{Name: sgb.WaterMeshName, Indices: []uint32{0, 1, 2}})
	selected := sgbRectangleEntities(model, map[string]int{"entity-7": 7}, fauxgl.Identity(), 100, 100, image.Rect(48, 48, 52, 52))
	if len(selected) != 1 || !selected[7] {
		t.Fatalf("selection=%v, want entity triangle crossing marquee selected", selected)
	}
}

func TestSgbSelectionOutlineBoundsMergeAllSubmeshesPerEntity(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-0.8, -0.5, 0}}, {Pos: [3]float32{-0.6, -0.5, 0}}, {Pos: [3]float32{-0.7, -0.3, 0}},
			{Pos: [3]float32{0.6, 0.3, 0}}, {Pos: [3]float32{0.8, 0.3, 0}}, {Pos: [3]float32{0.7, 0.5, 0}},
			{Pos: [3]float32{0.9, -0.9, 0}}, {Pos: [3]float32{1, -0.9, 0}}, {Pos: [3]float32{0.95, -0.8, 0}},
		},
		Meshes: []whm.Mesh{
			{Name: "entity-4", Indices: []uint32{0, 1, 2}},
			{Name: "entity-4", Indices: []uint32{3, 4, 5}},
			{Name: "entity-9", Indices: []uint32{6, 7, 8}},
		},
	}
	bounds := sgbEntityProjectedBounds(model, map[string]int{"entity-4": 4, "entity-9": 9}, fauxgl.Identity(), 100, 100, whm.Pose{}, false)
	if len(bounds) != 2 {
		t.Fatalf("got bounds for %d entities, want 2: %v", len(bounds), bounds)
	}
	combined := bounds[4]
	if combined.Min.X > 11 || combined.Min.Y > 26 || combined.Max.X < 89 || combined.Max.Y < 76 {
		t.Errorf("entity outline bounds %v do not enclose both distant submeshes", combined)
	}
	if _, ok := bounds[9]; !ok {
		t.Errorf("second entity bounds missing: %v", bounds)
	}
}

func TestSgbGpuSelectionBoundsAreOnlyBuiltForSelectedEntities(t *testing.T) {
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{-1, -1, 0}}, {Pos: [3]float32{1, -1, 0}}, {Pos: [3]float32{0, 1, 0}},
			{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, -1, -1}}, {Pos: [3]float32{0, 1, -1}},
		},
		Meshes: []whm.Mesh{
			{Name: "entity-4", Indices: []uint32{0, 1, 2}},
			{Name: "entity-9", Indices: []uint32{3, 4, 5}},
			{Name: sgb.WaterMeshName, Indices: []uint32{0, 1, 2}},
		},
	}
	camera := engine.Camera{Width: 120, Height: 120, Yaw: 0, Pitch: math.Pi / 2, Distance: 6, Target: fauxgl.Vector{}, Fov: 45}
	meshes := map[string]int{"entity-4": 4, "entity-9": 9, sgb.WaterMeshName: 99}
	lines := prepareSgbSelectionBounds(model, map[int]bool{4: true}, meshes, engine.ViewMatrix(camera), camera, whm.Pose{}, false)
	if len(lines) != 4 {
		t.Fatalf("selected entity generated %d GPU outline edges, want 4", len(lines))
	}
	for _, line := range lines {
		if line.R < 0.99 || line.G < 0.8 || line.B > 0.4 || line.HalfWidth <= 0 {
			t.Fatalf("unexpected GPU outline line: %+v", line)
		}
	}
	if lines := prepareSgbSelectionBounds(model, map[int]bool{}, meshes, engine.ViewMatrix(camera), camera, whm.Pose{}, false); len(lines) != 0 {
		t.Fatalf("no selection generated %d GPU outline edges", len(lines))
	}
}

func TestSgbRemoveEntitiesDeletesOnlySelectedSceneMeshes(t *testing.T) {
	m := &sgb.Map{Entities: []sgb.Entity{{ID: 1}, {ID: 2}, {ID: 3}}}
	scene := &whm.Model{
		Vertices: []whm.Vertex{{Pos: [3]float32{0, 0, 0}}, {Pos: [3]float32{1, 0, 0}}, {Pos: [3]float32{10, 0, 0}}},
		Meshes:   []whm.Mesh{{Name: "terrain", Indices: []uint32{0}}, {Name: "entity-1", Indices: []uint32{1}}, {Name: "entity-2", Indices: []uint32{2}}}, Bones: []whm.Bone{{Name: "root"}, {Name: "removed"}, {Name: "kept"}},
	}
	entityMeshes := map[string]int{"entity-1": 1, "entity-2": 2}
	animations := []sgb.AnimatedEntity{{ID: 1, BoneBase: 1, Model: &whm.Model{Bones: []whm.Bone{{Name: "removed"}}}}, {ID: 2, BoneBase: 2, Model: &whm.Model{Bones: []whm.Bone{{Name: "kept"}}}}}
	animations, removed := sgbRemoveEntities(m, scene, entityMeshes, animations, map[int]bool{1: true})
	if removed != 1 || len(m.Entities) != 2 || m.Entities[0].ID != 2 || m.Entities[1].ID != 3 {
		t.Fatalf("removed=%d entities=%v, want only ID 1 removed", removed, m.Entities)
	}
	if len(scene.Meshes) != 2 || scene.Meshes[1].Name != "entity-2" || entityMeshes["entity-1"] != 0 {
		t.Fatalf("scene meshes=%v mesh IDs=%v, selected entity mesh should be absent", scene.Meshes, entityMeshes)
	}
	if len(animations) != 1 || animations[0].ID != 2 || animations[0].BoneBase != 1 {
		t.Fatalf("remaining animations=%v, want ID 2 remapped to bone 1", animations)
	}
	if len(scene.Bones) != 2 || scene.Bones[1].Name != "kept" {
		t.Fatalf("scene bones=%v, want removed instance bone discarded", scene.Bones)
	}
	if scene.BoundMax[0] != 10 {
		t.Fatalf("bounds after removal end at %v, want remaining entity at x=10", scene.BoundMax)
	}
	if len(scene.Vertices) != 3 {
		t.Fatalf("removal discarded vertex storage (len=%d), want stable indices", len(scene.Vertices))
	}
}

func TestSgbMoveSelectedEntitiesFollowsTerrainAndClampsToMap(t *testing.T) {
	m := &sgb.Map{
		Ground:   sgb.Ground{CenterX: 1, CenterZ: 1, CellSize: 1, HeightScale: 2, CellsX: 2, CellsZ: 2},
		Relief:   sgb.Grid{W: 3, H: 3, Data: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8}},
		Entities: []sgb.Entity{{ID: 1, X: 0, Y: 8, Z: 0}, {ID: 2, X: 0, Y: 0, Z: 0}},
	}
	scene := &whm.Model{
		Vertices: []whm.Vertex{{Pos: [3]float32{-1, 0, 8}}, {Pos: [3]float32{1, 0, 8}}, {Pos: [3]float32{20, 0, 0}}},
		Meshes:   []whm.Mesh{{Name: "terrain", Indices: []uint32{2}}, {Name: "entity-1", Indices: []uint32{0, 1}}},
	}
	entityMeshes := map[string]int{"entity-1": 1}
	animated := []sgb.AnimatedEntity{{ID: 1, Placement: sgb.Placement{X: 0, Y: 0, Z: 8}, Model: &whm.Model{}}}
	animated, moved := sgbMoveEntitiesOnSurface(m, scene, entityMeshes, animated, map[int]bool{1: true}, 0.5, 0.5)
	if moved != 1 {
		t.Fatalf("moved=%d, want selected entity moved", moved)
	}
	entity := m.Entities[0]
	if math.Abs(float64(entity.X)-0.5) > 1e-6 || math.Abs(float64(entity.Z)-0.5) > 1e-6 || math.Abs(float64(entity.Y)-4) > 1e-6 {
		t.Fatalf("entity moved to (%v,%v,%v), want (0.5,4,0.5) on relief", entity.X, entity.Y, entity.Z)
	}
	if animated[0].Placement.X != 0.5 || animated[0].Placement.Y != 0.5 || animated[0].Placement.Z != 4 {
		t.Fatalf("animated placement=%+v, want terrain-snapped position", animated[0].Placement)
	}
	if scene.Vertices[0].Pos != [3]float32{-0.5, 0.5, 4} || scene.Vertices[1].Pos != [3]float32{1.5, 0.5, 4} {
		t.Fatalf("scene vertices after drag=%v and %v, want translated by the map-surface delta", scene.Vertices[0].Pos, scene.Vertices[1].Pos)
	}
	_, moved = sgbMoveEntitiesOnSurface(m, scene, entityMeshes, animated, map[int]bool{1: true}, 100, 100)
	if moved != 1 || m.Entities[0].X != m.Ground.XMax() || m.Entities[0].Z != m.Ground.ZMax() {
		t.Fatalf("edge drag moved=%d entity=(%v,%v), want clamped map bounds (%v,%v)", moved, m.Entities[0].X, m.Entities[0].Z, m.Ground.XMax(), m.Ground.ZMax())
	}
	if m.Entities[1].X != 0 || m.Entities[1].Z != 0 {
		t.Fatalf("unselected entity changed to (%v,%v)", m.Entities[1].X, m.Entities[1].Z)
	}
}

func TestSgbRotateSelectedEntitiesChangesHeadingAndWorldGeometryOnly(t *testing.T) {
	m := &sgb.Map{Entities: []sgb.Entity{
		{ID: 1, X: 10, Y: 2, Z: 20, Heading: 0.25},
		{ID: 2, X: -5, Y: 0, Z: -3, Heading: -0.5},
	}}
	scene := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{12, 20, 5}}, // selected object, two metres east of its pivot
			{Pos: [3]float32{10, 22, 7}}, // selected object's second mesh
			{Pos: [3]float32{-3, -3, 9}}, // unselected object
		},
		Meshes: []whm.Mesh{
			{Name: "entity-1", Indices: []uint32{0}},
			{Name: "entity-1", Indices: []uint32{0, 1}}, // shared vertex must rotate once
			{Name: "entity-2", Indices: []uint32{2}},
		},
	}
	entityMeshes := map[string]int{"entity-1": 1, "entity-2": 2}
	animated := []sgb.AnimatedEntity{{ID: 1, Placement: sgb.Placement{X: 10, Y: 20, Z: 2, Heading: 0.25}, Model: &whm.Model{}}}
	rotated := sgbRotateEntities(m, scene, entityMeshes, animated, map[int]bool{1: true}, math.Pi/2)
	if rotated != 1 {
		t.Fatalf("rotated=%d, want one selected object", rotated)
	}
	if math.Abs(float64(m.Entities[0].Heading)-(.25+math.Pi/2)) > 1e-6 {
		t.Fatalf("stored heading=%v, want +pi/2", m.Entities[0].Heading)
	}
	if math.Abs(float64(animated[0].Placement.Heading)-(.25+math.Pi/2)) > 1e-6 {
		t.Fatalf("animated heading=%v, want updated map heading", animated[0].Placement.Heading)
	}
	if math.Abs(float64(scene.Vertices[0].Pos[0])-10) > 1e-6 || math.Abs(float64(scene.Vertices[0].Pos[1])-22) > 1e-6 || scene.Vertices[0].Pos[2] != 5 {
		t.Fatalf("rotated point=%v, want pivot-relative vertical-axis rotation to (10,22,5)", scene.Vertices[0].Pos)
	}
	if scene.Vertices[1].Pos != [3]float32{8, 20, 7} {
		t.Fatalf("second mesh point=%v, want (8,20,7)", scene.Vertices[1].Pos)
	}
	if scene.Vertices[2].Pos != [3]float32{-3, -3, 9} || m.Entities[1].Heading != -0.5 {
		t.Fatalf("unselected entity changed: point=%v heading=%v", scene.Vertices[2].Pos, m.Entities[1].Heading)
	}
	if got := sgbRotateEntities(m, scene, entityMeshes, animated, map[int]bool{}, 0.1); got != 0 {
		t.Fatalf("empty selection rotated %d entities", got)
	}
}

func TestSgbCameraDragRequiresAlt(t *testing.T) {
	for _, tc := range []struct {
		name string
		mods key.Modifiers
		want bool
	}{
		{"plain drag", 0, false},
		{"alt drag", key.ModAlt, true},
		{"alt plus shift", key.ModAlt | key.ModShift, true},
		{"shift only", key.ModShift, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sgbCameraDragRequested(tc.mods); got != tc.want {
				t.Fatalf("sgbCameraDragRequested(%v)=%v, want %v", tc.mods, got, tc.want)
			}
		})
	}
}

func TestSgbSelectionUsesCameraProjection(t *testing.T) {
	view := engine.ViewMatrix(engine.Camera{Width: 200, Height: 100, Yaw: 0.6, Pitch: 0.4, Distance: 8, Target: fauxgl.Vector{}, Fov: 45})
	model := &whm.Model{
		Vertices: []whm.Vertex{
			{Pos: [3]float32{0, 0, 0}}, {Pos: [3]float32{0, 0, 1}}, {Pos: [3]float32{0, 1, 0}},
		},
		Meshes: []whm.Mesh{{Name: "entity-1", Indices: []uint32{0, 1, 2}}},
	}
	center := fauxgl.Vector{}
	projected := engine.Project(view, center, 200, 100)
	if projected == nil {
		t.Fatal("camera target did not project")
	}
	id, ok := sgbPickEntity(model, map[string]int{"entity-1": 1}, engine.Camera{Width: 200, Height: 100, Yaw: 0.6, Pitch: 0.4, Distance: 8, Target: fauxgl.Vector{}, Fov: 45}, projected.X, projected.Y)
	if !ok || id != 1 {
		t.Fatalf("camera-projected click picked %d (hit=%v), want entity 1", id, ok)
	}
}
