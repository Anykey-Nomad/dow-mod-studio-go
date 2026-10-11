package main

import (
	"dow-mod-studio/internal/modtree"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// rshNecronsDir is where the real game textures these tests read live; the
// raster package keeps its own copy for its own real-file tests.
const rshNecronsDir = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data\art\ebps\races\Necrons\texture_share`

// Stepping one frame at a time is how a single pose of an animation is looked
// at, so the play head has to land on the next frame exactly, stop the model
// while it is being wound, and stay inside the clip rather than running off
// either end of it.
func TestModelViewerStepsOneFrameAtATime(t *testing.T) {
	model := renderTestModelWithAnyAnimation(t)
	v := newModelViewer(model, "")
	v.selectAnim(0)
	anim := v.anim
	if anim == nil || anim.NumFrames < 4 {
		t.Skip("this model has no clip with enough frames to step through")
	}
	v.playing = true
	v.time = 0.5
	step := float32(1) / float32(anim.NumFrames)
	before, frames := v.currentFrame()

	v.stepFrame(1)
	if v.playing {
		t.Error("the model is still playing after a frame was wound to")
	}
	if got, want := v.time, float32(0.5)+step; abs32(got-want) > 1e-5 {
		t.Errorf("one frame forward landed on %v, want %v", got, want)
	}
	if frame, total := v.currentFrame(); total != frames || frame != before+1 {
		t.Errorf("the readout says frame %d of %d, want the one after frame %d of %d",
			frame+1, total, before+1, frames)
	}

	v.stepFrame(-1)
	if got := v.time; abs32(got-0.5) > 1e-5 {
		t.Errorf("stepping back landed on %v, want the frame it came from, 0.5", got)
	}
	// The first frame is a floor rather than a wrap: holding the button down
	// walks to the start of the clip and stays there.
	v.time = step
	v.stepFrame(-1)
	if got := v.time; abs32(got) > 1e-5 {
		t.Errorf("stepping back past the first frame landed on %v, want the first", got)
	}

	// The last frame of a clip is the first one again, so a step forward from
	// there wraps to the start rather than past the end of the pose.
	v.time = 1 - step
	v.stepFrame(1)
	if v.time != 0 {
		t.Errorf("stepping past the last frame landed on %v, want the start of the clip", v.time)
	}
}

// A model with no clip has no frames to step through, and the step is a no
// operation rather than a division by nothing.
func TestModelViewerStepsNothingWithoutAClip(t *testing.T) {
	v := newModelViewer(renderTestModel(t), "")
	v.stepFrame(1)
	if v.time != 0 || v.playing {
		t.Errorf("stepping a model with no clip left time at %v and playing at %v", v.time, v.playing)
	}
	if frame, frames := v.currentFrame(); frame != 0 || frames != 0 {
		t.Errorf("the readout says frame %d of %d for a model with no clip", frame, frames)
	}
}

// A notch of the wheel has to be a step the eye can follow. It used to be a
// two percent move raised to the whole scroll amount, and one notch is 120 of
// that, so a click multiplied the camera distance by ten: the model went from
// framed to inside out in one click and back in the next, and nothing in
// between was worth looking at. The step is now a single readable move, the
// two zoom buttons do exactly the same move, and the whole range the viewer
// allows is a dozen or so steps either way.
func TestOneNotchOfZoomIsAStepAndTheButtonsMatchIt(t *testing.T) {
	v := newModelViewer(renderTestModel(t), "")

	// A notch is worth one step's worth of distance, whichever way the wheel
	// is turned: a notch down moves the camera closer and so is a factor below
	// one.
	notch := modelZoomByDelta(wheelNotchDelta)
	if notch >= 1 || notch < 1/1.5 {
		t.Errorf("one notch multiplies the distance by %g, want a small step of its own", notch)
	}
	// A notch has to be one of several moves across the range, not all of it.
	lo, hi := v.FitDistance*0.15, v.FitDistance*6
	steps := math.Log(hi/lo) / math.Abs(math.Log(notch))
	if steps < 6 || steps > 40 {
		t.Errorf("the viewer's whole zoom range is %g notches, want a handful rather than one jump or a hundred", steps)
	}
	// A trackpad reports the same turn of the wheel in smaller pieces, and
	// those have to add up to the same move.
	if quarter := modelZoomByDelta(-wheelNotchDelta / 4); quarter != math.Pow(modelZoomStep, 0.25) {
		t.Errorf("a quarter notch is a factor of %g, which does not add up to the %g of a whole one", quarter, notch)
	}

	// The wheel and the buttons take the same path, so a press moves the
	// camera by exactly as much as a click of the wheel.
	v.Distance = v.FitDistance
	v.zoomBy(modelZoomByDelta(-wheelNotchDelta))
	byWheel := v.Distance
	v.Distance = v.FitDistance
	v.zoomBy(modelZoomStep) // the near button, in the modelControlClicks path
	if abs64(v.Distance-byWheel) > 1e-9 {
		t.Errorf("a press moved the distance to %v and a notch to %v: the two are not the same step", v.Distance, byWheel)
	}
	// And the far button is the same step the other way.
	v.zoomBy(1 / modelZoomStep)
	if abs64(v.Distance-v.FitDistance) > 1e-9 {
		t.Errorf("pressing the two zoom buttons returned the distance to %v, want the %v it started at", v.Distance, v.FitDistance)
	}
}

// The camera has to stay inside the range the viewer allows however hard the
// wheel is turned, or the model ends up inside the camera.
func TestZoomStepsAreClampedToWhatTheViewerAllows(t *testing.T) {
	v := newModelViewer(renderTestModel(t), "")
	lo, hi := v.FitDistance*0.15, v.FitDistance*6
	for i := 0; i < 200; i++ {
		v.zoomBy(modelZoomStep)
	}
	if v.Distance > hi+1e-9 {
		t.Errorf("zooming in stopped at %v, want no closer than %v", v.Distance, hi)
	}
	for i := 0; i < 400; i++ {
		v.zoomBy(1 / modelZoomStep)
	}
	if v.Distance < lo-1e-9 {
		t.Errorf("zooming out stopped at %v, want no further than %v", v.Distance, lo)
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestModelViewerProducesFirstFrame guards the wiring the GUI depends on: a
// freshly opened tab must ask for a render by itself. An earlier version gated
// the canvas behind "a frame already exists", so the first render was never
// requested and the tab stayed blank forever.
func TestModelViewerProducesFirstFrame(t *testing.T) {
	model := renderTestModel(t)
	v := newModelViewer(model, "")
	if !v.redraw {
		t.Fatal("a new viewer must start out needing a render")
	}
	// Drive it the way modelCanvas does: a layout clears the flag and starts a
	// background render, then the result arrives through the channel.
	v.redraw = false
	v.requestRender(64, 48, modelQualityFinal)
	if !v.rendering {
		t.Fatal("requestRender did not start a render")
	}
	frame := <-v.frameCh
	if frame.image == nil {
		t.Fatal("render returned no image")
	}
	if frame.image.Bounds().Dx() != 64 || frame.image.Bounds().Dy() != 48 {
		t.Fatalf("frame is %v, want 64x48", frame.image.Bounds())
	}
	// The frame has to be opaque enough to see: the model must have covered
	// some pixels rather than leaving the background.
	if !frameHasGeometry(frame.image) {
		t.Error("frame has no geometry; the canvas would look empty")
	}
}

// TestModelViewerCoalescesRenders checks that a burst of camera movement does
// not queue up a render per input event: while one render is in flight the
// viewer only remembers that another is needed.
func TestModelViewerCoalescesRenders(t *testing.T) {
	model := renderTestModel(t)
	v := newModelViewer(model, "")
	v.requestRender(32, 32, modelQualityFinal)
	first := v.frameCh
	for i := 0; i < 5; i++ {
		v.requestRender(32, 32, modelQualityFinal)
	}
	if v.frameCh != first {
		t.Fatal("a second render was started while one was still running")
	}
	if !v.pending {
		t.Error("the coalesced request was not recorded")
	}
	<-first
}

// TestModelViewerResizeInvalidates checks that a size change is noticed, since
// the projection depends on the aspect ratio.
func TestModelViewerResizeInvalidates(t *testing.T) {
	model := renderTestModel(t)
	v := newModelViewer(model, "")
	if v.renderW != 0 || v.renderH != 0 {
		t.Fatal("a new viewer has no render size yet")
	}
	v.redraw = false
	v.setRenderSize(800, 600)
	if !v.redraw {
		t.Error("the first size must schedule the initial render")
	}
	v.redraw = false
	v.setRenderSize(800, 600)
	if v.redraw {
		t.Error("an unchanged size must not schedule a render")
	}
	v.setRenderSize(640, 480)
	if !v.redraw {
		t.Error("a size change must schedule a render; the frame would not fit")
	}
	// A collapsed panel must not poison the stored size.
	v.redraw = false
	v.setRenderSize(0, 0)
	if v.renderW != 640 || v.renderH != 480 {
		t.Errorf("a zero size overwrote the render size: %dx%d", v.renderW, v.renderH)
	}
}

// TestModelViewerSelectAnim checks clip cycling, including the wrap around.
func TestModelViewerSelectAnim(t *testing.T) {
	model := renderTestModelWithAnyAnimation(t)
	v := newModelViewer(model, "")
	n := len(model.Animations)
	if n == 0 {
		t.Skip("model has no animations")
	}
	v.selectAnim(0)
	first := v.anim
	if first == nil || v.animIndex != 0 {
		t.Fatal("selectAnim(0) did not pick the first clip")
	}
	v.selectAnim(-1)
	if v.animIndex != n-1 {
		t.Errorf("selectAnim(-1) gave index %d, want %d", v.animIndex, n-1)
	}
	v.selectAnim(n)
	if v.animIndex != 0 {
		t.Errorf("selectAnim past the end gave index %d, want 0 (wrap)", v.animIndex)
	}
	if v.time != 0 {
		t.Errorf("time = %v after selecting a clip, want 0", v.time)
	}
}

// TestMaterialSearchRootsIncludesModFolders checks that the material lookup has
// somewhere to look beyond the merged tree: game art is shared between mods, so
// a .whm can reference a .rsh that lives in another data folder.
func TestMaterialSearchRootsIncludesModFolders(t *testing.T) {
	tree := &modtree.ModTree{
		AppPath:     `C:\Games\Dawn of War Definitive Edition`,
		ModFolder:   "AdvancedCampaign",
		ModFilePath: `C:\Games\Dawn of War Definitive Edition\AdvancedCampaign.module`,
		Sources: []*modtree.FileSource{
			{ModName: "AdvancedCampaign", DiskRoot: `C:\Games\Dawn of War Definitive Edition\AdvancedCampaign\Data`},
			{ModName: "DXP3", IsSGA: true, ArchivePath: `C:\Games\Dawn of War Definitive Edition\DXP3\Art.sga`},
		},
	}
	roots := materialSearchRoots(tree)
	if len(roots) == 0 {
		t.Fatal("no search roots")
	}
	wantMod := filepath.Clean(filepath.Join(tree.AppPath, tree.ModFolder))
	if roots[0] != wantMod {
		t.Errorf("first root = %q, want the current mod folder %q", roots[0], wantMod)
	}
	var found bool
	for _, r := range roots {
		if r == filepath.Clean(`C:\Games\Dawn of War Definitive Edition\AdvancedCampaign\Data`) {
			found = true
		}
	}
	if !found {
		t.Errorf("roots %v do not include the disk data folder", roots)
	}
	for _, r := range roots {
		if filepath.Ext(r) == ".sga" {
			t.Errorf("SGA archive %q should not be a disk root", r)
		}
	}
}

// TestReadModelMaterialFallsBackToDisk checks the fallback path: the merged
// tree misses the file, but another mod's data folder has it.
func TestReadModelMaterialFallsBackToDisk(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "Data", "art", "ebps", "races", "necrons", "texture_share")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rsh, err := os.ReadFile(filepath.Join(rshNecronsDir, "nec_warrior.rsh"))
	if err != nil {
		t.Skipf("game textures not available: %v", err)
	}
	rel := `art/ebps/races/necrons/texture_share/nec_warrior.rsh`
	if err := os.WriteFile(filepath.Join(dir, "Data", filepath.FromSlash(rel)), rsh, 0o644); err != nil {
		t.Fatal(err)
	}
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	got, ok := readModelMaterial(tree, []string{filepath.Join(dir, "Data")}, "",
		`art/ebps/races/necrons/texture_share/nec_warrior`)
	if !ok {
		t.Fatal("readModelMaterial did not find the file on disk")
	}
	if len(got) != len(rsh) {
		t.Errorf("read %d bytes, want %d", len(got), len(rsh))
	}
}

func frameHasGeometry(img *image.RGBA) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 0x3000 || g > 0x3000 || bl > 0x3000 {
				return true
			}
		}
	}
	return false
}
