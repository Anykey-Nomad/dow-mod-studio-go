package sgbview

import (
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
)

// TestStructureHqRestState — постройки на картах должны показываться без
// аддонов: чертежи штаб-квартир рас разбираются через тот же путь, что и
// любые объекты сценария (loadEntityModel), и видимость берётся из графа
// действий .whe рядом с моделью. Числа — состояние нетронутой постройки,
// сверенное с графом: база плюс флаги, без аддон-меши, строительные леса и
// краны в покое не показываются.
func TestStructureHqRestState(t *testing.T) {
	root := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
	tree, err := modtree.BuildModTree(root+`\DoWDE.module`, "English")
	if err != nil {
		t.Skipf("игра не установлена: %v", err)
	}
	defer tree.Close()

	cases := []struct {
		blueprint string
		want      int
	}{
		{`data:attrib\ebps\races\chaos\structures\chaos_hq.rgd`, 6},
		{`data:attrib\ebps\races\dark_eldar\structures\dark_eldar_hq.rgd`, 1},
		{`data:attrib\ebps\races\eldar\structures\eldar_hq.rgd`, 2},
		{`data:attrib\ebps\races\guard\structures\guard_hq.rgd`, 3},
		{`data:attrib\ebps\races\necrons\structures\monolith.rgd`, 19},
		{`data:attrib\ebps\races\orks\structures\ork_hq.rgd`, 5},
		{`data:attrib\ebps\races\sisters\structures\sisters_hq.rgd`, 9},
		{`data:attrib\ebps\races\space_marines\structures\space_marine_hq.rgd`, 24},
		{`data:attrib\ebps\races\tau\structures\tau_hq.rgd`, 7},
	}
	for _, tc := range cases {
		model, path, visible, reason := loadEntityModel(tree, sgb.Entity{Name: tc.blueprint})
		if reason != "" {
			t.Errorf("%s: %s", tc.blueprint, reason)
			continue
		}
		if model == nil {
			t.Errorf("%s: модель не разобрана (%s)", tc.blueprint, path)
			continue
		}
		if visible == nil {
			t.Errorf("%s: видимость не найдена, видно всё", tc.blueprint)
			continue
		}
		if len(visible) != tc.want {
			t.Errorf("%s: видно %d мешей, want %d", tc.blueprint, len(visible), tc.want)
		}
	}
}
