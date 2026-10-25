package whm

import (
	"testing"

	"github.com/fogleman/fauxgl"
)

// combineTestModel — скелет из двух костей и три клипа: один двигает только
// руку, второй только корень, третий конфликтует с первым за руку.
func combineTestModel() *Model {
	return &Model{
		Bones: []Bone{
			{Name: "root", Parent: -1, Quat: [4]float32{0, 0, 0, 1}},
			{Name: "arm", Parent: 0, Quat: [4]float32{0, 0, 0, 1}},
		},
		Animations: []Animation{
			{Name: "arm_idle", Bones: []Track{{
				Bone:   1,
				Name:   "arm",
				PosKey: []Key3{{Time: 0, V: [3]float32{0, 0, 0}}, {Time: 1, V: [3]float32{2, 0, 0}}},
			}}},
			{Name: "root_idle", Bones: []Track{{
				Bone:   0,
				Name:   "root",
				PosKey: []Key3{{Time: 0, V: [3]float32{0, 0, 0}}, {Time: 1, V: [3]float32{0, 3, 0}}},
			}}},
			{Name: "arm_other", Bones: []Track{{
				Bone:   1,
				Name:   "arm",
				PosKey: []Key3{{Time: 0, V: [3]float32{0, 0, 0}}, {Time: 1, V: [3]float32{0, 0, 5}}},
			}}},
		},
	}
}

func matrixClose(a, b fauxgl.Matrix) bool {
	for _, pair := range [][2]float64{
		{a.X00, b.X00}, {a.X01, b.X01}, {a.X02, b.X02}, {a.X03, b.X03},
		{a.X10, b.X10}, {a.X11, b.X11}, {a.X12, b.X12}, {a.X13, b.X13},
		{a.X20, b.X20}, {a.X21, b.X21}, {a.X22, b.X22}, {a.X23, b.X23},
		{a.X30, b.X30}, {a.X31, b.X31}, {a.X32, b.X32}, {a.X33, b.X33},
	} {
		if d := pair[0] - pair[1]; d > 1e-5 || d < -1e-5 {
			return false
		}
	}
	return true
}

// boneOrigin — точка-начало кости в позе.
func boneOrigin(p Pose, bone int) fauxgl.Vector {
	return boneOriginAt(p.Bones[bone])
}

func boneOriginAt(m fauxgl.Matrix) fauxgl.Vector {
	return m.MulPosition(fauxgl.Vector{})
}

// TestPoseAtAnimationTimesCombinesDifferentBones — два клипа, движущие разные
// кости, складываются в одну позу: корень идёт за своим клипом, а рука
// наследует сдвинутый корень. Кости — чистые сдвиги, поэтому начало руки в
// комбинированной позе равно сумме её сдвига и сдвига корня.
func TestPoseAtAnimationTimesCombinesDifferentBones(t *testing.T) {
	m := combineTestModel()
	armClip, rootClip := &m.Animations[0], &m.Animations[1]
	combined := m.PoseAtAnimationTimes([]*Animation{armClip, rootClip}, []float32{1, 1})
	armOnly := m.PoseAt(armClip, 1)
	rootOnly := m.PoseAt(rootClip, 1)
	rest := m.RestPose()

	// Корень не двигается клипом руки, поэтому его поза равна позе из
	// корневого клипа.
	if !matrixClose(combined.Bones[0], rootOnly.Bones[0]) {
		t.Errorf("корень в комбинированной позе не совпадает с его собственным клипом")
	}
	// Рука сдвинута суммой: её собственный сдвиг плюс сдвиг корня.
	want := boneOrigin(armOnly, 1).
		Add(boneOrigin(rootOnly, 0)).
		Sub(boneOrigin(rest, 0))
	got := boneOrigin(combined, 1)
	if d := got.Sub(want).Length(); d > 1e-4 {
		t.Errorf("начало руки в комбинированной позе = %v, want %v (сдвиг руки + сдвиг корня)", got, want)
	}
}

// TestPoseAtAnimationTimesLaterClipWins — когда два клипа двигают одну кость,
// побеждает поздний клип.
func TestPoseAtAnimationTimesLaterClipWins(t *testing.T) {
	m := combineTestModel()
	armA, armB := &m.Animations[0], &m.Animations[2]
	combined := m.PoseAtAnimationTimes([]*Animation{armA, armB}, []float32{1, 1})
	bOnly := m.PoseAt(armB, 1)
	if !matrixClose(combined.Bones[1], bOnly.Bones[1]) {
		t.Errorf("при конфликте за кость победил не поздний клип")
	}
}

// TestPoseAtAnimationTimesUsesOwnTimes — каждый клип оценивается по своему
// нормализованному времени.
func TestPoseAtAnimationTimesUsesOwnTimes(t *testing.T) {
	m := combineTestModel()
	armClip, rootClip := &m.Animations[0], &m.Animations[1]
	combined := m.PoseAtAnimationTimes([]*Animation{armClip, rootClip}, []float32{0.5, 0})
	armHalf := m.PoseAt(armClip, 0.5)
	rootRest := m.PoseAt(rootClip, 0)
	if !matrixClose(combined.Bones[0], rootRest.Bones[0]) {
		t.Errorf("корень при t=0 не в позе своего клипа на нуле")
	}
	if !matrixClose(combined.Bones[1], armHalf.Bones[1]) {
		t.Errorf("рука при t=0.5 не совпадает с половиной своего клипа")
	}
}

// TestPoseAtAnimationsIsEmptyFallsBackToRest — без клипов возвращается
// rest-поза, а не пустая структура.
func TestPoseAtAnimationsIsEmptyFallsBackToRest(t *testing.T) {
	m := combineTestModel()
	got := m.PoseAtAnimations(nil, 0.5)
	rest := m.RestPose()
	for i := range rest.Bones {
		if !matrixClose(got.Bones[i], rest.Bones[i]) {
			t.Fatalf("пустой список клипов не вернул rest-позу: кость %d другая", i)
		}
	}
}
