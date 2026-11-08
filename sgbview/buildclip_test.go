package sgbview

import (
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
)

// TestBuildStageAt — прогресс выбирает этап по его диапазону: шаблон даёт
// build_1, стройка до 80 % — build_2, финиш — build_4. Этап без диапазона
// (phase_in) играет только пока стройка не началась.
func TestBuildStageAt(t *testing.T) {
	generator := &Anim{build: []buildClip{
		{min: 0, max: 2, ranged: true},
		{min: 2, max: 80, ranged: true},
		{min: 80, max: 100, ranged: true},
	}}
	cases := []struct {
		percent float64
		want    int
	}{
		{0, 0}, {1.5, 0}, {2, 1}, {2.1, 1}, {79, 1}, {80, 2}, {100, 2}, {130, 2},
	}
	for _, tc := range cases {
		if got := generator.buildStageAt(tc.percent); got != tc.want {
			t.Errorf("%v%% — этап %d, хотели %d", tc.percent, got, tc.want)
		}
	}

	placed := &Anim{build: []buildClip{
		{ranged: false},
		{min: 0, max: 100, ranged: true},
	}}
	if got := placed.buildStageAt(0); got != 0 {
		t.Errorf("до начала стройки этап %d, хотели появление 0", got)
	}
	if got := placed.buildStageAt(10); got != 1 {
		t.Errorf("на 10%% этап %d, хотели 1", got)
	}
}

// TestNecronPlasmaGeneratorBuildStages — у генератора некронов три этапа:
// build_1 на 0–2 %, build_2 на 2–80 % и build_4 на 80–100 %, все с hold end,
// поэтому клип этапа стоит на последнем кадре, пока прогресс не сменит этап.
func TestNecronPlasmaGeneratorBuildStages(t *testing.T) {
	root := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
	tree, err := modtree.BuildModTree(root+`\DoWDE.module`, "English")
	if err != nil {
		t.Skipf("игра не установлена: %v", err)
	}
	defer tree.Close()

	const blueprint = `data:attrib\ebps\races\necrons\structures\necron_plasma_generator.rgd`
	model, whmPath, _, reason := loadEntityModel(tree, sgb.Entity{Name: blueprint})
	if model == nil {
		t.Fatalf("модель не загрузилась: %q (путь %q)", reason, whmPath)
	}
	doc, err := modelWHE(tree, whmPath)
	if err != nil || doc == nil {
		t.Fatalf(".whe не разобран: %v", err)
	}

	stages := (&Anim{doc: doc}).BuildStages()
	want := []struct {
		motion       string
		min, max     float64
		hasRange     bool
		timelineName string
	}{
		{"build_1", 0, 2, true, "build_1"},
		{"build_2", 2, 80, true, "build_2"},
		{"build_4", 80, 100, true, "build_4"},
	}
	for _, tc := range want {
		var found *BuildStage
		for i := range stages {
			if stages[i].Motion == tc.motion {
				found = &stages[i]
				break
			}
		}
		if found == nil {
			t.Errorf("этап %q не найден", tc.motion)
			continue
		}
		if !found.HasRange || found.Min != tc.min || found.Max != tc.max {
			t.Errorf("этап %q: диапазон %v..%v (hasRange=%v), хотели %v..%v",
				tc.motion, found.Min, found.Max, found.HasRange, tc.min, tc.max)
		}
		if len(found.Anims) == 0 || found.Anims[0] != tc.timelineName {
			t.Errorf("этап %q: анимации %v, хотели %q", tc.motion, found.Anims, tc.timelineName)
		}
		if found.Loop {
			t.Errorf("этап %q зациклен, а в .whe у него hold end", tc.motion)
		}
	}

	clips := buildStageClips(tree, whmPath, model, doc)
	for _, clip := range clips {
		if clip.clip == nil {
			t.Fatal("этап без клипа")
		}
		if clip.loop {
			t.Errorf("клип %q зациклен, хотели hold end", clip.clip.Name)
		}
	}
}
