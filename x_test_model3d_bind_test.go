package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/fogleman/fauxgl"
)

func boneIndexByName(m *WhmModel, name string) int {
	for i := range m.Bones {
		if m.Bones[i].Name == name {
			return i
		}
	}
	return -1
}

func bonePos(pose WhmPose, m *WhmModel, name string) (fauxgl.Vector, bool) {
	i := boneIndexByName(m, name)
	if i < 0 || i >= len(pose.Bones) {
		return fauxgl.Vector{}, false
	}
	return pose.Bones[i].MulPosition(fauxgl.Vector{}), true
}

// TestBipedStandsUpright pins the skeleton convention against anatomy, which no
// amount of plausible looking wrongness can fake. A biped authored for the game
// has its toes on the ground plane, its head above its pelvis and its spine
// climbing between them. Getting the root frame sign, the order of a bone's
// translation and rotation, or the sense of its stored quaternion wrong folds
// the chain over: the legs climb towards the head and the spine dives.
func TestBipedStandsUpright(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dowTroopsDir, "lychguard.whm"))
	if err != nil {
		t.Skip(err)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	pose := model.RestPose()
	need := []string{"bip01", "bip01 l toe0", "bip01 r toe0", "bip01 head", "bip01 spine", "bip01 spine3", "bip01 l calf"}
	got := make(map[string]fauxgl.Vector, len(need))
	for _, n := range need {
		p, ok := bonePos(pose, model, n)
		if !ok {
			t.Fatalf("no bone %q", n)
		}
		got[n] = p
	}
	t.Logf("pelvis z=%.2f  toes z=%.2f/%.2f  head z=%.2f  spine z=%.2f spine3 z=%.2f",
		got["bip01"].Z, got["bip01 l toe0"].Z, got["bip01 r toe0"].Z, got["bip01 head"].Z,
		got["bip01 spine"].Z, got["bip01 spine3"].Z)

	height := float64(model.BoundMax[2] - model.BoundMin[2])
	floor := float64(model.BoundMin[2])

	// The toes have to be on the mesh floor, not sunk into it or floating.
	for _, n := range []string{"bip01 l toe0", "bip01 r toe0"} {
		if d := math.Abs(got[n].Z - floor); d > 0.12*height {
			t.Errorf("%s is %.2f from the ground plane, want under %.2f", n, d, 0.12*height)
		}
	}
	if got["bip01 head"].Z <= got["bip01"].Z {
		t.Errorf("head z=%.2f is not above the pelvis z=%.2f", got["bip01 head"].Z, got["bip01"].Z)
	}
	if got["bip01 spine3"].Z <= got["bip01 spine"].Z {
		t.Errorf("spine3 z=%.2f does not climb above spine z=%.2f", got["bip01 spine3"].Z, got["bip01 spine"].Z)
	}
	if got["bip01 l calf"].Z >= got["bip01"].Z {
		t.Errorf("calf z=%.2f is not below the hip z=%.2f", got["bip01 l calf"].Z, got["bip01"].Z)
	}
	if got["bip01 head"].Z > floor+height {
		t.Errorf("head z=%.2f is above the top of the model", got["bip01 head"].Z)
	}
}

// TestBuildingBonesSitInsideTheirPiece covers a building, where every piece is
// rigid and hangs off its own bone. A piece has to move with the bone named
// for it in the mesh list, which means that bone has to be inside the piece it
// belongs to. It is the same convention as the biped, checked on geometry
// whose correct answer is not in doubt.
func TestBuildingBonesSitInsideTheirPiece(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dowRacesDir, "Necrons", "structures", "necron_energy_core.whm"))
	if err != nil {
		t.Skip(err)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	bound := 0
	for i := range model.Vertices {
		if model.Vertices[i].Weights[0] > 0 {
			bound++
		}
	}
	if bound != len(model.Vertices) {
		t.Fatalf("only %d of %d vertices are bound to a bone; a rigid piece has no weights of its own", bound, len(model.Vertices))
	}

	pose := model.RestPose()
	for i := range model.Meshes {
		m := &model.Meshes[i]
		if m.SingleBone < 0 || m.SingleBone >= len(model.Bones) {
			t.Errorf("mesh %q has no valid single bone (got %d)", m.Name, m.SingleBone)
			continue
		}
		lo := [3]float64{1e9, 1e9, 1e9}
		hi := [3]float64{-1e9, -1e9, -1e9}
		for _, idx := range m.Indices {
			if int(idx) >= len(model.Vertices) {
				continue
			}
			v := &model.Vertices[idx]
			p := [3]float64{float64(v.Pos[0]), float64(v.Pos[1]), float64(v.Pos[2])}
			for a := 0; a < 3; a++ {
				if p[a] < lo[a] {
					lo[a] = p[a]
				}
				if p[a] > hi[a] {
					hi[a] = p[a]
				}
			}
		}
		origin := pose.Bones[m.SingleBone].MulPosition(fauxgl.Vector{})
		p := [3]float64{origin.X, origin.Y, origin.Z}
		outside := ""
		for a := 0; a < 3; a++ {
			if p[a] < lo[a]-0.05 || p[a] > hi[a]+0.05 {
				outside += string("xyz"[a])
			}
		}
		if outside != "" {
			t.Errorf("mesh %q spans X[%.2f %.2f] Y[%.2f %.2f] Z[%.2f %.2f] but its bone %q is at (%.2f %.2f %.2f), outside on %s",
				m.Name, lo[0], hi[0], lo[1], hi[1], lo[2], hi[2],
				model.Bones[m.SingleBone].Name, p[0], p[1], p[2], outside)
		}
	}
}

// TestPoseAtRestKeyIsNeutral checks the property that pins down how a key is
// interpreted. The first key of an idle clip repeats the bone's own rest
// transform, so a clip evaluated there must leave the mesh exactly where the
// bind pose had it. Reading a key as a delta from the rest transform, or
// converting it through another axis convention, both break this.
func TestPoseAtRestKeyIsNeutral(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(dowTroopsDir, "lychguard.whm"))
	if err != nil {
		t.Skip(err)
	}
	model, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatal(err)
	}
	rest := model.RestPose()
	checked := 0
	for i := range model.Animations {
		anim := &model.Animations[i]
		// Bones whose first key repeats their own rest transform have to come
		// out of PoseAt exactly where the bind pose had them. Capes and
		// weapons are keyed away from the bind pose even in an idle clip, so
		// they simply do not qualify as a probe.
		neutral := map[int]bool{}
		for _, tr := range anim.Bones {
			if tr.Bone < 0 {
				continue
			}
			pos, rot := model.Bones[tr.Bone].Pos, model.Bones[tr.Bone].Quat
			if len(tr.PosKey) > 0 && vecDistance(tr.PosKey[0].V, pos) < 1e-3 {
				if len(tr.RotKey) == 0 || quatDistance(tr.RotKey[0].V, rot) < 1e-3 {
					neutral[tr.Bone] = true
				}
			}
		}
		if len(neutral) == 0 {
			continue
		}
		checked += len(neutral)
		pose := model.PoseAt(anim, anim.Bones[0].PosKey[0].Time)
		for b := range neutral {
			// The keys are float32 in the file, so an exact match is not on;
			// a centimetre on a model metres tall is rounding.
			if d := matDelta(rest.Bones[b], pose.Bones[b]); d > 0.01 {
				t.Errorf("clip %q bone %q drifted %.4f at its rest key",
					anim.Name, model.Bones[b].Name, d)
			}
		}
		if checked > 200 {
			break
		}
	}
	if checked == 0 {
		t.Skip("no clip repeats the rest pose at its first key")
	}
	t.Logf("checked %d neutral rest keys", checked)
}

func vecDistance(a, b [3]float32) float64 {
	s := 0.0
	for i := 0; i < 3; i++ {
		d := float64(a[i]) - float64(b[i])
		s += d * d
	}
	return math.Sqrt(s)
}

func quatDistance(a, b [4]float32) float64 {
	dot := 0.0
	for i := 0; i < 4; i++ {
		dot += float64(a[i]) * float64(b[i])
	}
	if dot < 0 {
		dot = -dot
	}
	if dot > 1 {
		dot = 1
	}
	return 2 * math.Acos(dot)
}

func matDelta(a, b fauxgl.Matrix) float64 {
	worst := 0.0
	for _, p := range [][2]float64{
		{a.X00, b.X00}, {a.X01, b.X01}, {a.X02, b.X02}, {a.X03, b.X03},
		{a.X10, b.X10}, {a.X11, b.X11}, {a.X12, b.X12}, {a.X13, b.X13},
		{a.X20, b.X20}, {a.X21, b.X21}, {a.X22, b.X22}, {a.X23, b.X23},
	} {
		if d := math.Abs(p[0] - p[1]); d > worst {
			worst = d
		}
	}
	return worst
}

// TestBonesStayNearTheirGeometry covers what the anatomical checks cannot: a
// skeleton that is reflected rather than folded still stands on its feet, but
// every bone is then on the wrong side of the geometry it deforms. Averaging the
// distance from a vertex to its own bone over six models separates the two by
// a wide margin, where the anatomy of a single model does not.
func TestBonesStayNearTheirGeometry(t *testing.T) {
	models := []struct {
		dir, file string
	}{
		{dowTroopsDir, "lychguard.whm"},
		{filepath.Join(dowRacesDir, "Necrons", "structures"), "necron_energy_core.whm"},
		{dowTroopsDir, "Necron_pylon.whm"},
		{dowTroopsDir, "eliminator.whm"},
		{filepath.Join(dowRacesDir, "Necrons", "structures"), "necron_great_pyramid.whm"},
		{dowTroopsDir, "Builder_scarab.whm"},
	}
	total, n, checked := 0.0, 0, 0
	for _, m := range models {
		raw, err := os.ReadFile(filepath.Join(m.dir, m.file))
		if err != nil {
			t.Skip(err)
		}
		model, err := ParseWhmModel(raw)
		if err != nil {
			t.Errorf("%s: %v", m.file, err)
			continue
		}
		size := float64(model.BoundMax[2] - model.BoundMin[2])
		if size <= 0.01 || len(model.Bones) == 0 {
			continue
		}
		pose := model.RestPose()
		for i := range model.Vertices {
			v := &model.Vertices[i]
			if v.Weights[0] <= 0 {
				continue
			}
			b := v.Bones[0]
			if b < 0 || int(b) >= len(pose.Bones) {
				continue
			}
			pos, _ := SkinnedVertex(model, pose, v)
			origin := pose.Bones[b].MulPosition(fauxgl.Vector{})
			total += origin.Sub(pos).Length() / size
			n++
		}
		checked++
	}
	if checked == 0 || n == 0 {
		t.Skip("no models available")
	}
	mean := total / float64(n)
	t.Logf("bones sit %.1f%% of the model height from the geometry they deform (%d vertices, %d models)",
		100*mean, n, checked)
	// A reflected skeleton scores above 0.33, the correct one 0.23.
	if mean > 0.28 {
		t.Errorf("mean bone to geometry distance is %.0f%% of the model height, want under 28%%", 100*mean)
	}
}
