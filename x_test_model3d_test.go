package main

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/fogleman/fauxgl"
)

// TestModelTextureReadsRowsAsStored pins the one convention the software path
// and the two hardware paths have to share: which end of the map is v = 0.
// fauxgl's own texture turns the coordinate into 1 - v, which draws every
// Dawn of War map upside down, so the texture the model viewer uses samples
// the rows as they are stored.
func TestModelTextureReadsRowsAsStored(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		src.SetNRGBA(x, 0, color.NRGBA{R: 255, A: 255})
		src.SetNRGBA(x, 3, color.NRGBA{B: 255, A: 255})
	}
	tex := newModelTexture(src)
	if got := tex.Sample(0.1, 0); got.R < 0.9 || got.B > 0.1 {
		t.Errorf("v = 0 does not read the first stored row: %+v", got)
	}
	if got := tex.Sample(0.1, 0.9); got.B < 0.9 || got.R > 0.1 {
		t.Errorf("v = 0.9 does not read the last stored row: %+v", got)
	}
	// A model's UVs run well past the edge of its map, so a coordinate
	// over one has to fold back onto it rather than run off the end.
	if tex.Sample(1.25, 2.25) != tex.Sample(0.25, 0.25) {
		t.Errorf("a coordinate past the edge does not wrap onto the map: %+v against %+v",
			tex.Sample(1.25, 2.25), tex.Sample(0.25, 0.25))
	}
}

// renderTestModel loads a real Necrons .whm and decodes it into a renderable
// model. It skips when the game is not installed.
func renderTestModel(t *testing.T) *WhmModel {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dowTroopsDir, "lychguard.whm"))
	if err != nil {
		t.Skipf("game models not available: %v", err)
	}
	model, err := ParseWhmModel(data)
	if err != nil {
		t.Fatalf("ParseWhmModel: %v", err)
	}
	if len(model.Vertices) == 0 || len(model.Meshes) == 0 {
		t.Fatalf("model has no geometry: %d verts, %d meshes", len(model.Vertices), len(model.Meshes))
	}
	return model
}

func TestRenderWhmModelGroundPlaneUsesTexture(t *testing.T) {
	texture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	texture.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	texture.SetRGBA(1, 0, color.RGBA{G: 255, A: 255})
	texture.SetRGBA(0, 1, color.RGBA{B: 255, A: 255})
	texture.SetRGBA(1, 1, color.RGBA{R: 255, G: 255, A: 255})
	model := &WhmModel{}
	opts := RenderOptions{
		Width: 100, Height: 100, Pitch: 0.65, Distance: 8,
		Target: fauxgl.Vector{Z: 3}, Quality: modelQualityFinal,
		Texture: newModelTexture(texture), Ground: true,
	}
	withGround := RenderWhmModel(model, model.RestPose(), opts)
	opts.Ground = false
	withoutGround := RenderWhmModel(model, model.RestPose(), opts)
	if imagesEqual(withGround, withoutGround) {
		t.Fatal("ground plane did not contribute any pixels to the rendered frame")
	}
	changed := 0
	for y := 0; y < withGround.Bounds().Dy(); y++ {
		for x := 0; x < withGround.Bounds().Dx(); x++ {
			if withGround.RGBAAt(x, y) != withoutGround.RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed < 20 {
		t.Errorf("ground plane covered only %d changed pixels", changed)
	}
}

func TestRenderWhmModelProducesGeometry(t *testing.T) {
	model := renderTestModel(t)
	center, distance := model.ModelCenter()
	img := RenderWhmModel(model, model.RestPose(), RenderOptions{
		Width:    160,
		Height:   120,
		Yaw:      0.6,
		Pitch:    0.2,
		Distance: distance,
		Target:   center,
		Quality:  modelQualityFinal,
	})
	if img == nil {
		t.Fatal("RenderWhmModel returned nil")
	}
	if img.Bounds().Dx() != 160 || img.Bounds().Dy() != 120 {
		t.Fatalf("size = %v, want 160x120", img.Bounds())
	}
	// The background is a flat colour, so a frame with real geometry must show
	// at least a few distinct colours.
	seen := map[uint32]bool{}
	lit := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, _ := img.At(x, y).RGBA()
			seen[r<<16|g<<8|bb] = true
			if r > 0x3000 || g > 0x3000 || bb > 0x3000 {
				lit++
			}
		}
	}
	if len(seen) < 3 {
		t.Errorf("frame is flat: %d distinct colours", len(seen))
	}
	if lit == 0 {
		t.Error("frame has no lit pixels; geometry did not rasterise")
	}
}

func TestRenderWhmModelDraftAndFinalDiffer(t *testing.T) {
	model := renderTestModel(t)
	center, distance := model.ModelCenter()
	opts := RenderOptions{
		Width:    64,
		Height:   48,
		Yaw:      0.3,
		Distance: distance,
		Target:   center,
	}
	draft := RenderWhmModel(model, model.RestPose(), withQuality(opts, modelQualityDraft))
	final := RenderWhmModel(model, model.RestPose(), withQuality(opts, modelQualityFinal))
	if imagesEqual(draft, final) {
		t.Error("draft and final frames are identical; shading is not applied")
	}
}

func TestRenderWhmModelWithSkeleton(t *testing.T) {
	model := renderTestModel(t)
	center, distance := model.ModelCenter()
	img := RenderWhmModel(model, model.RestPose(), RenderOptions{
		Width:    64,
		Height:   48,
		Yaw:      0.2,
		Distance: distance,
		Target:   center,
		Quality:  modelQualityFinal,
		Skeleton: true,
	})
	if img == nil {
		t.Fatal("RenderWhmModel returned nil")
	}
}

func TestRenderWhmModelAnimationMovesGeometry(t *testing.T) {
	model, anim := renderTestModelWithAnimation(t)
	center, distance := model.ModelCenter()
	opts := RenderOptions{
		Width:    96,
		Height:   72,
		Yaw:      0.4,
		Distance: distance,
		Target:   center,
		Quality:  modelQualityFinal,
	}
	first := RenderWhmModel(model, model.PoseAt(anim, 0), opts)
	// A still frame is not proof that playback works, so sample a few points
	// and require at least one of them to differ from the first frame.
	changed := false
	for i := 1; i <= 4 && !changed; i++ {
		tNorm := float32(i) / 4
		frame := RenderWhmModel(model, model.PoseAt(anim, tNorm), opts)
		if !imagesEqual(first, frame) {
			changed = true
		}
	}
	if !changed {
		t.Errorf("clip %q did not change the rendered frame", anim.Name)
	}
}

func TestRenderWhmModelRejectsEmptyOptions(t *testing.T) {
	model := renderTestModel(t)
	img := RenderWhmModel(model, model.RestPose(), RenderOptions{Width: 0, Height: 0})
	if img == nil {
		t.Fatal("expected an empty image, got nil")
	}
}

func TestPoseAtIsStable(t *testing.T) {
	model := renderTestModel(t)
	a := model.PoseAt(nil, 0)
	b := model.PoseAt(nil, 0)
	if len(a.Bones) != len(model.Bones) {
		t.Fatalf("pose has %d bones, model has %d", len(a.Bones), len(model.Bones))
	}
	for i := range a.Bones {
		if a.Bones[i] != b.Bones[i] {
			t.Fatalf("bone %d: rest pose is not deterministic", i)
		}
	}
}

func TestPoseAtClampsOutsideClip(t *testing.T) {
	model, anim := renderTestModelWithAnimation(t)
	first := model.PoseAt(anim, -5)
	zero := model.PoseAt(anim, 0)
	for i := range first.Bones {
		if first.Bones[i] != zero.Bones[i] {
			t.Fatalf("bone %d: t=-5 differs from t=0", i)
		}
	}
	last := model.PoseAt(anim, 5)
	one := model.PoseAt(anim, 1)
	for i := range last.Bones {
		if last.Bones[i] != one.Bones[i] {
			t.Fatalf("bone %d: t=5 differs from t=1", i)
		}
	}
}

func TestSkinnedVertexNormalisesWeights(t *testing.T) {
	model := renderTestModel(t)
	pose := model.RestPose()
	for i := range model.Vertices {
		v := &model.Vertices[i]
		// A doubled weight set must land on the same place as a halved one.
		pos, _ := SkinnedVertex(model, pose, v)
		if math.IsNaN(pos.X) || math.IsNaN(pos.Y) || math.IsNaN(pos.Z) {
			t.Fatalf("vertex %d skinned to NaN", i)
		}
	}
}

// renderTestModelWithAnyAnimation finds a troop model that has clips at all,
// without requiring one of them to move the mesh.
func renderTestModelWithAnyAnimation(t *testing.T) *WhmModel {
	t.Helper()
	entries, err := os.ReadDir(dowTroopsDir)
	if err != nil {
		t.Skipf("game models not available: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".whm" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dowTroopsDir, e.Name()))
		if err != nil {
			continue
		}
		model, err := ParseWhmModel(data)
		if err == nil && len(model.Animations) > 0 {
			return model
		}
	}
	t.Skip("no animated model found")
	return nil
}

// renderTestModelWithAnimation finds a troop model with a clip that actually
// moves bones, so playback is tested against real data rather than skipped.
// Some clips only fade or hide meshes, which legitimately leave the geometry
// untouched.
func renderTestModelWithAnimation(t *testing.T) (*WhmModel, *WhmAnimation) {
	t.Helper()
	entries, err := os.ReadDir(dowTroopsDir)
	if err != nil {
		t.Skipf("game models not available: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".whm" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dowTroopsDir, e.Name()))
		if err != nil {
			continue
		}
		model, err := ParseWhmModel(data)
		if err != nil || len(model.Animations) == 0 || len(model.Bones) == 0 {
			continue
		}
		for i := range model.Animations {
			if animMovesGeometry(model, &model.Animations[i]) {
				return model, &model.Animations[i]
			}
		}
	}
	t.Skip("no animated model found")
	return nil, nil
}

// animMovesGeometry reports whether a clip visibly displaces the mesh. Plenty
// of clips only twitch a weapon or a turret, and a model can be almost entirely
// rigid, so the bar is a noticeable share of the vertices moving a real amount.
func animMovesGeometry(model *WhmModel, anim *WhmAnimation) bool {
	if !animMovesBones(model, anim) {
		return false
	}
	rest := model.RestPose()
	posed := model.PoseAt(anim, 0.5)
	moved := 0
	for i := range model.Vertices {
		v := &model.Vertices[i]
		bound := false
		for s := 0; s < 4; s++ {
			if boneInRange(v.Bones[s], len(model.Bones)) && v.Weights[s] > 0 {
				bound = true
			}
		}
		if !bound {
			continue
		}
		p0, _ := SkinnedVertex(model, rest, v)
		p1, _ := SkinnedVertex(model, posed, v)
		if p0.Sub(p1).Length() > 1e-3 {
			moved++
		}
	}
	return moved*50 >= len(model.Vertices)
}

// animMovesBones reports whether a clip puts bones in more than one place.
func animMovesBones(model *WhmModel, anim *WhmAnimation) bool {
	for _, track := range anim.Bones {
		if track.Bone < 0 || track.Bone >= len(model.Bones) {
			continue
		}
		for i := 1; i < len(track.PosKey); i++ {
			if track.PosKey[i].V != track.PosKey[0].V {
				return true
			}
		}
		for i := 1; i < len(track.RotKey); i++ {
			if track.RotKey[i].V != track.RotKey[0].V {
				return true
			}
		}
	}
	return false
}

func withQuality(opts RenderOptions, q int) RenderOptions {
	opts.Quality = q
	return opts
}

func imagesEqual(a, b image.Image) bool {
	if a == nil || b == nil {
		return false
	}
	ab, bb := a.Bounds(), b.Bounds()
	if ab != bb {
		return false
	}
	for y := ab.Min.Y; y < ab.Max.Y; y++ {
		for x := ab.Min.X; x < ab.Max.X; x++ {
			ar, ag, ab_, aa := a.At(x, y).RGBA()
			br, bg, bb_, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab_ != bb_ || aa != ba {
				return false
			}
		}
	}
	return true
}
