package sgbview

import (
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
)

// TestMonolithSettlesIntoGroundWithoutAddons — базовое состояние монолита
// некронов (races/necrons/structures): на карте он должен стоять после
// подъёма из земли, а не в покое скелета. Покой скелета — это состояние
// «все аддоны достроены», и без запекания позы монолит выглядит уже
// достроенным (как addon 2), в отличие от гвардии и орков, у которых покой
// скелета и есть база.
//
// Числа сверены с игрой по данным Necron_Monolith.whm:
//   - buildSettleClip выбирает build_stage_2 — финальную стадию подъёма
//     (отклонение позы от покоя 381079 против 1115227 у build_stage_1);
//   - после запекания корпус (monolith_body) уходит вниз в среднем на ~6.87
//     единиц — верхушка остаётся чуть над землёй;
//   - посты (nec_listening_post*) стройка не трогает: они остаются на земле.
func TestMonolithSettlesIntoGroundWithoutAddons(t *testing.T) {
	root := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
	tree, err := modtree.BuildModTree(root+`\DoWDE.module`, "English")
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	const blueprint = `data:attrib\ebps\races\necrons\structures\monolith.rgd`
	model, path, visible, reason := loadEntityModel(tree, sgb.Entity{Name: blueprint})
	if model == nil {
		t.Fatalf("модель не загрузилась: %q (путь %q)", reason, path)
	}
	if len(visible) != 19 {
		t.Errorf("видно мешей %d, хотели 19 — база без аддонов", len(visible))
	}

	// Сырая модель — база сравнения до запекания.
	data, err := tree.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := whm.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if clip := buildSettleClip(raw); clip == nil {
		t.Fatal("клип успокоения не найден: build_stage_* отсутствуют")
	} else if clip.Name != "build_stage_2" {
		t.Errorf("клип успокоения %q, хотели build_stage_2", clip.Name)
	}

	// Средний сдвиг каждой модели по вершинам: корпус уходит вниз, посты нет.
	avgShift := func(m *whm.Model, meshName string) float32 {
		var sum, n float32
		for _, wm := range m.Meshes {
			if !strings.EqualFold(wm.Name, meshName) {
				continue
			}
			seen := map[uint32]bool{}
			for _, idx := range wm.Indices {
				if seen[idx] || int(idx) >= len(raw.Vertices) {
					continue
				}
				seen[idx] = true
				sum += m.Vertices[idx].Pos[2] - raw.Vertices[idx].Pos[2]
				n++
			}
		}
		if n == 0 {
			t.Fatalf("меш %q не найден", meshName)
		}
		return sum / n
	}

	if body := avgShift(model, "monolith_body"); body > -6 {
		t.Errorf("корпус сдвинулся на %.2f, хотели ниже -6 (погружение в землю)", body)
	}
	for _, post := range []string{"nec_listening_post01", "nec_listening_post05"} {
		if s := avgShift(model, post); s > 0.01 || s < -0.01 {
			t.Errorf("пост %s сдвинулся на %.2f, а должен остаться на земле", post, s)
		}
	}
}
