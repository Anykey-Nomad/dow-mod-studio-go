package main

// End-to-end check of the 3D map view's terrain texture against a real
// scenario: DSHD tile and SMAP decal texture lookups through the merged mod
// tree, the baked composite and map camera defaults. Skipped when the game is
// not installed at the standard path.

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	engine "dow-mod-studio/internal/3d_engine"
	modelviewer "dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"

	"github.com/fogleman/fauxgl"
)

// The same install reachable from Windows and from this workspace's mount.
var sgbTextureTestModules = []string{
	`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/AdvancedCampaign.module`,
	`/mnt/windows/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/AdvancedCampaign.module`,
}

const sgbTextureTestMap = `Data\scenarios\sp\4p_antiga_bay.sgb`

// The real mod tree and map, built once for the tests in this file.
var (
	sgbTestTree *modtree.ModTree
	sgbTestMap  *sgb.Map
)

func sgbTestSetup(t *testing.T) (*modtree.ModTree, *sgb.Map) {
	t.Helper()
	if sgbTestTree != nil {
		return sgbTestTree, sgbTestMap
	}
	var module string
	for _, want := range sgbTextureTestModules {
		if _, err := os.Stat(want); err == nil {
			module = want
			break
		}
	}
	if module == "" {
		t.Skipf("no Dawn of War install at %s", sgbTextureTestModules[0])
	}
	tree, err := modtree.BuildModTree(module, "English")
	if err != nil {
		t.Fatalf("modtree: %v", err)
	}
	data, err := tree.ReadFile(sgbTextureTestMap)
	if err != nil {
		t.Fatalf("read map: %v", err)
	}
	m, err := sgb.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sgbTestTree, sgbTestMap = tree, m
	return tree, m
}

func TestSgbSamplePreservesTileColor(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	r, g, b, a := sgbSample(img, 0.5, 0.5, true)
	for _, tc := range []struct {
		name string
		got  float64
		want uint8
	}{
		{"red", r, 200}, {"green", g, 100}, {"blue", b, 50}, {"alpha", a, 128},
	} {
		if diff := math.Abs(tc.got*255 - float64(tc.want)); diff > 1 {
			t.Errorf("%s sample = %.4f (byte %.1f), want byte %d", tc.name, tc.got, tc.got*255, tc.want)
		}
	}
}

func TestSgbOpeningCameraAxisAnglesMatchNearestRequestedView(t *testing.T) {
	got := engine.AxisAngles(engine.Camera{Yaw: sgbCameraYaw, Pitch: sgbCameraPitch})
	want := [3]float64{119.748020302740, 65.203605531972, 139.481994013136}
	for axis := range got {
		if math.Abs(got[axis]-want[axis]) > 1e-6 {
			t.Errorf("opening camera axis angle %d = %.9f°, want %.9f°", axis, got[axis], want[axis])
		}
	}
}

func TestSgbTerrainTextureRealMap(t *testing.T) {
	tree, m := sgbTestSetup(t)
	if len(m.Decals) == 0 {
		t.Fatal("map carries no decals")
	}

	// Tile art (DSHD) supplies the ground detail; the adjacent .tga is
	// only the map-selection preview and must not decide the surface size.
	tileImg := sgbBuildTileTexture(tree, m)
	if tileImg == nil {
		t.Fatal("no DSHD terrain textures resolved")
	}
	if tileImg.Bounds().Dx() <= m.Relief.W || tileImg.Bounds().Dy() <= m.Relief.H {
		t.Errorf("terrain texture %v did not exceed heightmap resolution %dx%d", tileImg.Bounds().Size(), m.Relief.W, m.Relief.H)
	}

	// A decal texture from the SMAP table must resolve through the merged
	// tree (it lives in a base-game .sga), and the bake must land pixels.
	seen := false
	for _, d := range m.Decals {
		if tex := sgbTreeTexture(tree, d.Name); tex != nil {
			seen = true
			break
		}
	}
	if !seen {
		t.Error("no decal texture resolved through the mod tree")
	}
	base := tileImg
	if n := sgbBakeDecals(base, tree, m); n == 0 {
		t.Error("no decals baked")
	}

	// The output follows the tile art's repeat density, not the heightmap's
	// resolution; the source argument is intentionally irrelevant.
	img := sgbBuildTerrainTexture(tree, sgbTextureTestMap, m)
	if img == nil {
		t.Fatal("composite is nil")
	}
	b := img.Bounds()
	if b.Dx() != tileImg.Bounds().Dx() || b.Dy() != tileImg.Bounds().Dy() {
		t.Errorf("composite %dx%d, want tile-driven %dx%d", b.Dx(), b.Dy(), tileImg.Bounds().Dx(), tileImg.Bounds().Dy())
	}
	changed := 0
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 != g>>8 || g>>8 != bl>>8 {
				changed++
			}
		}
	}
	if changed == 0 {
		t.Error("composite is grey everywhere: no colour from the map art")
	}
	var maxChannel uint8
	for i := 0; i < len(img.Pix); i += 4 {
		maxChannel = max(maxChannel, img.Pix[i], img.Pix[i+1], img.Pix[i+2])
	}
	if maxChannel <= 2 {
		t.Errorf("terrain composite is black (max RGB channel %d)", maxChannel)
	}

	// The camera opens in the in-game range and zooms from a close orbit up
	// to the whole-map fit.
	d := sgbCameraDefaultDistance(m)
	if d != sgbCameraDistance {
		t.Errorf("default camera distance %v, want %v", d, sgbCameraDistance)
	}
	v := newModelViewer(sgbTerrainModel(m, "t"), sgbTextureTestMap)
	v.Yaw, v.Pitch = sgbCameraYaw, sgbCameraPitch
	if v.FitDistance <= d {
		t.Fatalf("fit distance %v should exceed the default %v", v.FitDistance, d)
	}
	v.MinDistance = sgbCameraMinDistance
	v.MaxDistance = v.FitDistance
	v.Distance = d
	for range 100 {
		v.zoomBy(0.5)
	}
	if v.Distance < sgbCameraMinDistance-1e-9 {
		t.Errorf("zoomed past the minimum: %v", v.Distance)
	}
	for range 100 {
		v.zoomBy(2)
	}
	if v.Distance > v.FitDistance+1e-9 {
		t.Errorf("zoomed past the fit: %v", v.Distance)
	}

	// A PNG of the composite, for eyeballing the bake when wanted.
	if os.Getenv("DOW_SGB_DUMP") != "" {
		out, err := os.Create("/tmp/sgb_composite.png")
		if err == nil {
			_ = png.Encode(out, img)
			out.Close()
		}
	}
}

// TestSgbSceneDeliveryTwoPhases locks the async contract the map view is
// built on: the first result (the terrain diffuse, more=true) leaves the
// channel armed so the second one still arrives, and the second result
// swaps the viewer's model. Needs no game install.
func TestSgbTerrainHeightAtBilinearlyInterpolatesAndClamps(t *testing.T) {
	m := &sgb.Map{
		Ground: sgb.Ground{CenterX: 1, CenterZ: 1, CellSize: 1, HeightScale: 0.5},
		Relief: sgb.Grid{W: 2, H: 2, Data: []byte{0, 10, 20, 30}},
	}
	for _, tc := range []struct {
		name string
		x, y float64
		want float64
	}{
		{"first vertex", 0, 0, 0},
		{"bilinear center", 0.5, 0.5, 7.5},
		{"last vertex", 1, 1, 15},
		{"clamped below", -10, -10, 0},
		{"clamped above", 10, 10, 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sgbTerrainHeightAt(m, tc.x, tc.y)
			if !ok || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("height(%v,%v) = %v, ok=%v; want %v", tc.x, tc.y, got, ok, tc.want)
			}
		})
	}
}

func TestSgbCameraTargetMarkerProjectsOntoSurfaceTarget(t *testing.T) {
	viewer := &modelViewer{CameraState: modelviewer.CameraState{
		Yaw: 0.7, Pitch: 0.5, Distance: 40, FOV: 45,
		Center: fauxgl.Vector{X: 2, Y: -3, Z: 5},
	}}
	at, ok := sgbCameraTargetScreen(viewer, 640, 480)
	if !ok {
		t.Fatal("camera target did not project")
	}
	opts := viewer.options(640, 480)
	projected := fxProject(buildViewMatrix(opts), viewer.Center, 640, 480)
	if projected == nil || math.Hypot(at.X-projected.X, at.Y-projected.Y) > 1e-9 {
		t.Fatalf("camera target marker at (%v,%v), projected orbit target %v", at.X, at.Y, projected)
	}
	if math.Abs(at.X-320) > 1e-6 || math.Abs(at.Y-240) > 1e-6 {
		t.Errorf("orbit target marker should sit at view center, got (%v,%v)", at.X, at.Y)
	}
	if _, ok := sgbCameraTargetScreen(viewer, 0, 480); ok {
		t.Error("marker projected for an invalid viewport")
	}
}

func TestSgbArrowMovesOrbitTargetAlongCameraHeadingAndTerrain(t *testing.T) {
	m := &sgb.Map{
		Ground: sgb.Ground{CenterX: 2, CenterZ: 2, CellSize: 1, HeightScale: 1, CellsX: 3, CellsZ: 3},
		Relief: sgb.Grid{W: 4, H: 4, Data: []byte{
			0, 0, 0, 0,
			0, 1, 2, 0,
			0, 3, 4, 0,
			0, 0, 0, 0,
		}},
	}
	viewer := &modelViewer{CameraState: modelviewer.CameraState{Yaw: 0, Pitch: 0.4, Distance: 30, Center: fauxgl.Vector{X: 0, Y: 1, Z: 3}}}
	tab := &fileTab{sgbMap: m, model: viewer}
	ui := &uiState{}
	if !ui.sgbMoveCamera(tab, 1, 0, 0.2) {
		t.Fatal("up arrow did not move camera target")
	}
	if viewer.Center.X >= 0 || math.Abs(viewer.Center.Y-1) > 1e-9 {
		t.Fatalf("up arrow at yaw 0 moved target to (%v,%v), want toward negative X", viewer.Center.X, viewer.Center.Y)
	}
	height, ok := sgbTerrainHeightAt(m, viewer.Center.X, viewer.Center.Y)
	if !ok || math.Abs(viewer.Center.Z-height) > 1e-9 {
		t.Fatalf("camera target Z=%v, sampled surface=%v (ok=%v)", viewer.Center.Z, height, ok)
	}
	if !viewer.redraw {
		t.Fatal("camera move did not invalidate rendered frame")
	}
	if viewer.Center.Z >= 3 {
		t.Fatalf("target altitude did not follow the lower terrain: started at 3, now %v", viewer.Center.Z)
	}
}

func TestSgbCameraVelocityEasesIntoMovementAndBrakesOnRelease(t *testing.T) {
	v := sgbApproachCameraVelocity(0, 1, 1.0/60)
	if v <= 0 || v >= 1 {
		t.Fatalf("first held frame velocity=%v; want a gradual ramp between 0 and 1", v)
	}
	for range 30 {
		v = sgbApproachCameraVelocity(v, 1, 1.0/60)
	}
	if v != 1 {
		t.Fatalf("held arrow velocity=%v, want full speed", v)
	}
	firstBrake := sgbApproachCameraVelocity(v, 0, 1.0/60)
	if firstBrake <= 0 || firstBrake >= v {
		t.Fatalf("release velocity=%v; want smooth deceleration from %v", firstBrake, v)
	}
	for range 30 {
		firstBrake = sgbApproachCameraVelocity(firstBrake, 0, 1.0/60)
	}
	if firstBrake != 0 {
		t.Fatalf("velocity after braking=%v, want rest", firstBrake)
	}
}

func TestSgbCameraMovementIsSmoothAndHorizontalControlsAreReversed(t *testing.T) {
	newViewer := func() *modelViewer {
		return &modelViewer{CameraState: modelviewer.CameraState{Yaw: 0, Center: fauxgl.Vector{}}}
	}
	m := &sgb.Map{
		Ground: sgb.Ground{CenterX: 20, CenterZ: 20, CellSize: 1, HeightScale: 1, CellsX: 39, CellsZ: 39},
		Relief: sgb.Grid{W: 40, H: 40, Data: make([]byte, 40*40)},
	}
	ui := &uiState{}
	short, long := &fileTab{sgbMap: m, model: newViewer()}, &fileTab{sgbMap: m, model: newViewer()}
	if !ui.sgbMoveCamera(short, 1, 0, 0.1) || !ui.sgbMoveCamera(long, 1, 0, 0.2) {
		t.Fatal("forward movement did not advance")
	}
	if got, want := long.model.Center.X, short.model.Center.X*2; math.Abs(got-want) > 1e-9 {
		t.Errorf("movement should scale smoothly with elapsed time: 0.1s X=%v, 0.2s X=%v; want %v", short.model.Center.X, got, want)
	}
	left, right := &fileTab{sgbMap: m, model: newViewer()}, &fileTab{sgbMap: m, model: newViewer()}
	if !ui.sgbMoveCamera(left, 0, 1, 0.1) || !ui.sgbMoveCamera(right, 0, -1, 0.1) {
		t.Fatal("horizontal movement did not advance")
	}
	if left.model.Center.Y >= 0 || right.model.Center.Y <= 0 {
		t.Errorf("left/right should be reversed relative to the previous mapping: left Y=%v right Y=%v", left.model.Center.Y, right.model.Center.Y)
	}
}

func TestSgbArrowStopsAtMapEdge(t *testing.T) {
	m := &sgb.Map{
		Ground: sgb.Ground{CenterX: 1, CenterZ: 1, CellSize: 1, HeightScale: 1, CellsX: 2, CellsZ: 2},
		Relief: sgb.Grid{W: 3, H: 3, Data: make([]byte, 9)},
	}
	viewer := &modelViewer{CameraState: modelviewer.CameraState{Yaw: 0, Pitch: 0.4, Distance: 20, Center: fauxgl.Vector{X: float64(m.Ground.XMin()), Y: 0}}}
	tab := &fileTab{sgbMap: m, model: viewer}
	ui := &uiState{}
	if ui.sgbMoveCamera(tab, 1, 0, 1) {
		t.Fatal("up arrow moved beyond the west edge")
	}
	if viewer.Center.X != float64(m.Ground.XMin()) {
		t.Fatalf("target X=%v, want edge %v", viewer.Center.X, m.Ground.XMin())
	}
}

func TestSgbEntityMaterialsExposeTextureUnderMeshMaterialKey(t *testing.T) {
	const material = "necron warrior"
	const diffusePath = "art/ebps/races/necrons/texture_share/nec_warrior"
	dir := t.TempDir()
	textureData := syntheticRSHDXT1(t)
	texturePath := filepath.Join(dir, filepath.FromSlash(diffusePath)+".rsh")
	if err := os.MkdirAll(filepath.Dir(texturePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(texturePath, textureData, 0644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{
		Root:    modtree.NewFolder("Data", "Data", nil),
		Sources: []*modtree.FileSource{{DiskRoot: dir}},
	}
	scene := &whm.Model{Meshes: []whm.Mesh{{
		Name: "entity-7", Material: material, MaterialPath: diffusePath,
	}}}
	rsh, materials, missing, diagnostics := sgbEntityMaterials(tree, "scene.sgb", scene, "ground", nil)
	if missing != 0 {
		t.Fatalf("SGB entity textures missing=%d, want 0", missing)
	}
	if len(rsh[material]) == 0 || string(rsh[material]) != string(textureData) {
		t.Fatalf("raw texture is not keyed by mesh material %q", material)
	}
	if materials[material].Texture == nil {
		t.Fatalf("decoded material is not keyed by mesh material %q: %#v", material, materials)
	}
	if got := diagnostics[material].ResolvedPath; got != texturePath {
		t.Fatalf("mesh material diagnostic path=%q, want %q", got, texturePath)
	}
}

func TestSgbObjectMaterialResultsMergeIntoMapViewer(t *testing.T) {
	viewer := &modelViewer{
		materials: map[string]WhmMaterial{"ground": {Color: rgbaWhite}},
		rshData:   map[string][]byte{"ground": []byte("ground-rsh")},
	}
	request := sgbObjectMaterialRequest{modelPath: "Data/art/ebps/environment/rock.whm"}
	tab := &fileTab{
		model:                    viewer,
		sgbObjectMaterialsCh:     make(chan sgbObjectMaterialsResult, 1),
		sgbObjectMaterialsActive: request,
	}
	tab.sgbObjectMaterialsCh <- sgbObjectMaterialsResult{
		request: request,
		materials: map[string]WhmMaterial{
			"rock_diffuse": {Color: rgbaWhite},
		},
		rsh:         map[string][]byte{"rock_diffuse": []byte("rock-rsh")},
		diagnostics: map[string]modelMaterialDiagnostic{"rock_diffuse": {Status: "decoded"}},
	}
	ui := &uiState{}
	if !ui.pollSgbObjectMaterials(tab) {
		t.Fatal("completed object material result was not polled")
	}
	if tab.sgbObjectMaterialsCh != nil {
		t.Fatal("material channel was not cleared")
	}
	if _, ok := viewer.materials["ground"]; !ok {
		t.Fatal("existing terrain material was discarded")
	}
	if _, ok := viewer.materials["rock_diffuse"]; !ok {
		t.Fatal("placed object's decoded material was not merged")
	}
	if string(viewer.rshData["rock_diffuse"]) != "rock-rsh" {
		t.Fatalf("placed object's raw texture = %q, want rock-rsh", viewer.rshData["rock_diffuse"])
	}
	if viewer.materialDiagnostics["rock_diffuse"].Status != "decoded" {
		t.Fatalf("placed object's diagnostics not merged: %+v", viewer.materialDiagnostics)
	}
	if !tab.sgbObjectMaterialsLoaded[strings.ToLower(request.modelPath)] {
		t.Fatal("loaded WHM was not recorded to prevent duplicate lookups")
	}
}

func TestSgbSceneDeliveryTwoPhases(t *testing.T) {
	v := &modelViewer{materials: map[string]WhmMaterial{}}
	tab := &fileTab{model: v, modelMaterialCh: make(chan modelMaterialResult, 2)}
	ui := &uiState{}
	terrainMat := map[string]WhmMaterial{"ground": {Color: rgbaWhite}}
	tab.modelMaterialCh <- modelMaterialResult{materials: terrainMat, more: true}
	ui.pollViewerJobs(tab)
	if tab.modelMaterialCh == nil {
		t.Fatal("channel closed after phase one although more=true")
	}
	if len(v.materials) != 1 || !v.textured || v.loading {
		t.Fatalf("phase one state: materials=%d textured=%v loading=%v", len(v.materials), v.textured, v.loading)
	}
	scene := &WhmModel{}
	both := map[string]WhmMaterial{"ground": {Color: rgbaWhite}, "wall": {Color: rgbaWhite}}
	tab.modelMaterialCh <- modelMaterialResult{materials: both, model: scene}
	ui.pollViewerJobs(tab)
	if tab.modelMaterialCh != nil {
		t.Fatal("channel still armed after the final phase")
	}
	if v.model != scene {
		t.Fatal("scene model not applied")
	}
	if len(v.materials) != 2 {
		t.Errorf("phase two materials %d, want 2", len(v.materials))
	}
}

// TestSgbSceneRealMap builds the full 3D scene — terrain plus every
// resolvable placed object — and checks that the objects actually landed:
// more meshes, more vertices and bounds that still fit the map.
func TestSgbSceneRealMap(t *testing.T) {
	tree, m := sgbTestSetup(t)
	terrain := m.TerrainModel(sgbTerrainMaxGrid)
	if terrain == nil {
		t.Fatal("terrain model is nil")
	}
	scene := sgbBuildSceneModel(tree, m, "t")
	if scene == nil {
		t.Fatal("scene model is nil")
	}
	added := len(scene.Meshes) - len(terrain.Meshes)
	t.Logf("scene: %d meshes (+%d entity groups), %d verts (terrain %d), bounds %v..%v",
		len(scene.Meshes), added, len(scene.Vertices), len(terrain.Vertices), scene.BoundMin, scene.BoundMax)
	if added < len(m.Entities)/2 {
		t.Errorf("only %d of %d entities produced meshes", added, len(m.Entities))
	}
	if len(scene.Vertices) <= len(terrain.Vertices) {
		t.Errorf("scene has %d vertices, terrain alone %d", len(scene.Vertices), len(terrain.Vertices))
	}
	// Objects live on the map: nothing may sit a kilometre off it.
	g := m.Ground
	margin := float32(50)
	if scene.BoundMin[0] < g.XMin()-margin || scene.BoundMax[0] > g.XMax()+margin ||
		scene.BoundMin[1] < g.ZMin()-margin || scene.BoundMax[1] > g.ZMax()+margin {
		t.Errorf("scene bounds %v..%v escape the map %v..%v", scene.BoundMin, scene.BoundMax, g.XMin(), g.ZMin())
	}
	// The terrain's own box must not have moved: the objects sit on top of
	// it, they do not displace it.
	if scene.BoundMax[2] < terrain.BoundMax[2] {
		t.Errorf("scene top %v below terrain top %v", scene.BoundMax[2], terrain.BoundMax[2])
	}
}
