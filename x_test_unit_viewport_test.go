package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/whm"
	"gioui.org/layout"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fogleman/fauxgl"
)

// fauxglDir is where the orbit camera sits, rebuilt from the yaw and pitch the
// same way buildViewMatrix works it out, so a test can check that the pair
// describes the direction a model's own camera was pointing. The up axis is Z,
// since that is how the game's models are authored.
func fauxglDir(v *modelViewer) fauxgl.Vector {
	return fauxgl.Vector{
		X: math.Cos(v.Yaw) * math.Cos(v.Pitch),
		Y: math.Sin(v.Yaw) * math.Cos(v.Pitch),
		Z: math.Sin(v.Pitch),
	}
}

// The orbit camera turns the model on the axis its own up vector names, which
// for these models is Z: a change of yaw spins it left and right, a change of
// pitch tips it up and down. The pair used to be worked out as if the models
// were Y-up, so the two came out crossed — dragging sideways turned the model
// up and down and the other way round. Measuring where a point of the model
// lands on screen is the only way to see it: the numbers are right in each
// direction and still wrong together.
func TestTurningTheModelTurnsItTheWayTheMouseWent(t *testing.T) {
	// A point beside the middle of a model, where a turn of the camera moves
	// it as far as it can.
	const w, h = 400, 400
	point := fauxgl.Vector{X: 1, Y: 0.2, Z: 0.5}
	opts := RenderOptions{Width: w, Height: h, Fov: 45, Distance: 8, Yaw: 0.7, Pitch: 0.2}

	onScreen := func(o RenderOptions) (float64, float64) {
		v := buildViewMatrix(o).MulPositionW(point)
		if v.W == 0 {
			t.Fatalf("the point is at the camera, at yaw %v pitch %v", o.Yaw, o.Pitch)
		}
		return v.X / v.W, v.Y / v.W
	}
	x0, y0 := onScreen(opts)
	turned := opts
	turned.Yaw += 0.2
	xa, ya := onScreen(turned)
	tilted := opts
	tilted.Pitch += 0.2
	xb, yb := onScreen(tilted)

	if math.Abs(ya-y0) > math.Abs(xa-x0)/2 {
		t.Errorf("turning the camera sideways moved the model %v across and %v up, so the axes are crossed",
			xa-x0, ya-y0)
	}
	if math.Abs(xb-x0) > math.Abs(yb-y0)/2 {
		t.Errorf("tilting the camera moved the model %v across and %v up, so the axes are crossed",
			xb-x0, yb-y0)
	}
	// And the model has to go the way the pointer did. The handler turns the
	// camera one way for a drag to the left and the other for a drag down, so
	// these two samples are what those drags produce: the model follows the
	// hand, left for the first and down for the second.
	if xa-x0 > 0 {
		t.Errorf("a drag to the left moved the model %v to the right of where it was", xa-x0)
	}
	if yb-y0 > 0 {
		t.Errorf("a drag downwards lifted the model %v up the screen instead of turning it", yb-y0)
	}
}

// The object window's viewport finds a unit's model through the field the unit
// .rgd itself carries, and these are the cases that decide which .whm that is.

// unitViewportMod is an empty mod for the path lookups: the .rgd and the .whm
// are put into it by the test.
func unitViewportMod(t *testing.T) *modtree.ModTree {
	t.Helper()
	appPath := t.TempDir()
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "ViewportTestMod"}
	t.Cleanup(tree.Close)
	return tree
}

// unitViewportWriteUnit puts a generated unit into a mod, with no model beside
// it unless one is named, and returns the unit's catalog entry.
func unitViewportWriteUnit(t *testing.T, tree *modtree.ModTree, name unitTemplateName, modelPath string) unitEntry {
	t.Helper()
	data, err := newUnitRgd(name).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	rgdPath := name.unitTemplateVirtualPath()
	writeTreeFile(t, tree, rgdPath, data)
	if modelPath != "" {
		writeTreeFile(t, tree, modelPath, []byte("not a real model"))
	}
	tree.SortAll(tree.Root)
	for _, u := range buildUnitCatalog(tree).allUnits() {
		if strings.EqualFold(u.path, rgdPath) {
			return u
		}
	}
	t.Fatalf("the fixture unit %s is not in the catalog", rgdPath)
	return unitEntry{}
}

// unitViewportFixture writes a generated unit and a model of the given name
// into a fresh mod, and returns the tree with the unit's entry.
func unitViewportFixture(t *testing.T, name unitTemplateName, modelPath string) (*modtree.ModTree, unitEntry) {
	t.Helper()
	tree := unitViewportMod(t)
	return tree, unitViewportWriteUnit(t, tree, name, modelPath)
}

// The generated unit names its model the way the game's own units do, so the
// field is read out of the parsed .rgd rather than out of the catalog.
func TestUnitAnimatorNameReadsTheBlueprintField(t *testing.T) {
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, "")
	d := loadUnitDetails(tree, entry)
	if d.error != "" {
		t.Fatal(d.error)
	}
	got := unitAnimatorName(d.file)
	want := "Races/Necrons/Troops/Necron_Wraith"
	if got != want {
		t.Fatalf("animator is %q, want %q", got, want)
	}
}

func TestModelInfoTableHandlesMissingTabAndViewer(t *testing.T) {
	ui := &uiState{}
	for _, tab := range []*fileTab{nil, {}} {
		if got := ui.modelInfoTable(layout.Context{}, tab); got != (layout.Dimensions{}) {
			t.Errorf("model info for tab %v = %+v, want empty dimensions", tab, got)
		}
	}
}

// A file with no entity_blueprint_ext, and a nil one, both answer "" rather than
// panicking: the viewport has to survive a unit it cannot find a model for.
func TestUnitAnimatorNameIsEmptyWhenTheFieldIsMissing(t *testing.T) {
	if got := unitAnimatorName(nil); got != "" {
		t.Errorf("a nil file gave %q", got)
	}
	if got := unitAnimatorName(&rgd.RgdFile{Root: &rgd.RgdEntry{Name: "GameData"}}); got != "" {
		t.Errorf("a file with no extensions gave %q", got)
	}
}

// The model is the .whm of the animator's name under Data\art\ebps, which is
// where the game keeps it: the .rgd says "Races/Necrons/Troops/Necron_Wraith"
// and the file is Data\art\ebps\Races\Necrons\Troops\Necron_Wraith.whm.
func TestUnitModelPathFollowsAnimator(t *testing.T) {
	want := `Data\art\ebps\Races\Necrons\Troops\Necron_Wraith.whm`
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, want)
	src, ok := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	if !ok {
		t.Fatal("the fixture unit says nothing about a model")
	}
	got := unitModelPath(tree, src)
	if !strings.EqualFold(got, want) {
		t.Fatalf("model path is %q, want %q", got, want)
	}
}

// A unit that names a model which is not there falls back to a .whm of the
// unit's own name, because that is how a mod that ships its own geometry under
// its own name is put together.
func TestUnitModelPathFallsBackToTheUnitsOwnName(t *testing.T) {
	// The generated unit points at a model the mod does not have; this one is
	// the only .whm of the name.
	placed := `Data\art\ebps\races\necrons\custom\necron_wraith.whm`
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, placed)
	if tree.FindFile(filepath.FromSlash(`Data\art\ebps\Races\Necrons\Troops\Necron_Wraith.whm`)) != nil {
		t.Fatal("the animator's model is present, so this is not a fallback case")
	}
	src, _ := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	got := unitModelPath(tree, src)
	if !strings.EqualFold(got, placed) {
		t.Fatalf("model path is %q, want the .whm of the unit's own name %q", got, placed)
	}
}

// A unit with no model at all is not an error the viewport has to survive with
// a panic: the lookup answers an empty path, which the section turns into a
// line of text.
func TestUnitModelPathIsEmptyWhenThereIsNoModel(t *testing.T) {
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, "")
	src, ok := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	if !ok {
		t.Fatal("the fixture unit says nothing about a model")
	}
	if got := unitModelPath(tree, src); got != "" {
		t.Fatalf("model path is %q, want none", got)
	}
	// A nil tree is what a mod that failed to load leaves behind.
	if got := unitModelPath(nil, src); got != "" {
		t.Fatalf("model path with no mod is %q, want none", got)
	}
}

// A unit whose .rgd carries no model field at all still gets whatever .whm is
// named after it, which is the whole of what is left to go on.
func TestUnitModelPathWithoutAnAnimatorField(t *testing.T) {
	placed := `Data\art\ebps\races\necron_wraith.whm`
	tree, entry := unitViewportFixture(t, unitTemplateName{Race: "necrons", Name: "necron_wraith"}, placed)
	src, _ := newUnitModelSource(tree, loadUnitDetails(tree, entry))
	src.animator = ""
	got := unitModelPath(tree, src)
	if !strings.EqualFold(got, placed) {
		t.Fatalf("model path is %q, want %q", got, placed)
	}
}

// The clip the viewport opens on is the one that moves something. A model's
// list is mostly one off actions, so "move" wins, then an idle, and a list of
// nothing but "vis_" clips — which only fade a weapon in and out — still starts
// on a real one if there is one and on the first if there is not.
func TestUnitDefaultAnimPrefersLocomotion(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"die_1", "idle_1", "move"}, "move"},
		{[]string{"die_1", "idle_1", "idle_2"}, "idle_1"},
		{[]string{"vis_flag", "vis_base_sword", "run"}, "run"},
		{[]string{"vis_flag", "vis_base_sword"}, "vis_flag"},
		{[]string{"attack_1", "walk_cycle"}, "attack_1"},
	}
	for _, tc := range cases {
		m := &WhmModel{}
		for _, n := range tc.names {
			m.Animations = append(m.Animations, WhmAnimation{Name: n})
		}
		idx := unitDefaultAnim(m)
		if idx < 0 || idx >= len(m.Animations) {
			t.Fatalf("%v: picked index %d", tc.names, idx)
		}
		if got := m.Animations[idx].Name; got != tc.want {
			t.Errorf("%v: picked %q, want %q", tc.names, got, tc.want)
		}
	}
	if idx := unitDefaultAnim(&WhmModel{}); idx != -1 {
		t.Errorf("a model with no clips gave index %d, want -1", idx)
	}
	if idx := unitDefaultAnim(nil); idx != -1 {
		t.Errorf("a nil model gave index %d, want -1", idx)
	}
}

// A model's own camera is a local transform in the exporter's frame, so looking
// through one has to put the orbit camera at the rotated position, aimed at the
// rotated focus point. Checking the round trip is what pins the rotation down: a
// viewer that left the camera unrotated would show a model from under the floor.
func TestLookThroughCameraAimsAtTheFocusPoint(t *testing.T) {
	v := newModelViewer(&WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, 1, 1}}},
		BoundMin: [3]float32{-1, -1, -1},
		BoundMax: [3]float32{1, 1, 1},
	}, "")
	cam := WhmCamera{Name: "port_hud", Pos: [3]float32{4, 0, 6}, Fov: 30, FocusPoint: [3]float32{0, 1, 0}}
	v.lookThroughCamera(cam)

	eye := boneRootFrame.MulPosition(vec3From(cam.Pos))
	focus := boneRootFrame.MulPosition(vec3From(cam.FocusPoint))
	if got := (v.Center.Sub(focus)).Length(); got > 1e-6 {
		t.Fatalf("the view is centred %v away from the camera's focus point", got)
	}
	if got, want := v.Distance, eye.Sub(focus).Length(); math.Abs(got-want) > 1e-6 {
		t.Fatalf("the view sits %v from the model, want %v", got, want)
	}
	if v.FOV != 30 {
		t.Errorf("the field of view is %v, want the camera's 30", v.FOV)
	}
	if v.ActiveCamera != "port_hud" {
		t.Errorf("the active camera is %q, want the one that was picked", v.ActiveCamera)
	}
	// The orbit parameters have to describe the same direction, or the model
	// is seen from somewhere the camera is not.
	dir := fauxglDir(v)
	if d := dir.Sub(eye.Sub(focus).Normalize()).Length(); d > 1e-6 {
		t.Errorf("the orbit direction is %v away from the camera's own", d)
	}
}

// A field of view the game stored as zero or as a full turn would make the
// projection useless, so only a sane one is taken.
func TestLookThroughCameraKeepsAnUnusableFieldOfView(t *testing.T) {
	v := newModelViewer(&WhmModel{Model: whm.Model{Vertices: []WhmVertex{{}}, BoundMax: [3]float32{1, 1, 1}}}, "")
	for _, fov := range []float32{0, -20, 180} {
		v.lookThroughCamera(WhmCamera{Name: "c", Pos: [3]float32{0, 0, 5}, Fov: fov})
		if v.FOV != modelDefaultFov {
			t.Errorf("a field of view of %v was taken, leaving the view at %v", fov, v.FOV)
		}
	}
}

// Reset view goes back to the framing the model was opened with, and forgets
// that a camera of the model's was in use.
func TestResetCameraForgetsTheModelsCamera(t *testing.T) {
	model := &WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-2, 0, -2}}, {Pos: [3]float32{2, 2, 2}}},
		BoundMin: [3]float32{-2, 0, -2},
		BoundMax: [3]float32{2, 2, 2},
	}
	v := newModelViewer(model, "")
	_, fit := model.ModelCenter()
	v.lookThroughCamera(WhmCamera{Name: "shot", Pos: [3]float32{9, 9, 9}, Fov: 60})
	v.resetCamera()
	if v.ActiveCamera != "" {
		t.Errorf("the active camera survived a reset: %q", v.ActiveCamera)
	}
	if v.FOV != modelDefaultFov {
		t.Errorf("the field of view is %v after a reset, want %v", v.FOV, modelDefaultFov)
	}
	if v.Distance != fit {
		t.Errorf("the distance is %v after a reset, want the framing distance %v", v.Distance, fit)
	}
	if center, _ := model.ModelCenter(); v.Center.Sub(center).Length() > 1e-6 {
		t.Errorf("the view is centred on %v, want the middle of the model %v", v.Center, center)
	}
}

// A camera sitting on its own focus point has no direction to work out, so it
// keeps the distance that framed the model rather than dividing by nothing.
func TestLookThroughCameraOnItsFocusPoint(t *testing.T) {
	model := &WhmModel{
		Vertices: []WhmVertex{{Pos: [3]float32{-1, -1, -1}}, {Pos: [3]float32{1, 1, 1}}},
		BoundMin: [3]float32{-1, -1, -1},
		BoundMax: [3]float32{1, 1, 1},
	}
	v := newModelViewer(model, "")
	_, fit := model.ModelCenter()
	v.lookThroughCamera(WhmCamera{Name: "degenerate", Pos: [3]float32{2, 2, 2}, FocusPoint: [3]float32{2, 2, 2}})
	if math.IsNaN(v.Distance) || math.IsInf(v.Distance, 0) {
		t.Fatalf("the distance became %v", v.Distance)
	}
	if v.Distance != fit {
		t.Errorf("the distance is %v, want the framing distance %v", v.Distance, fit)
	}
}
