package sgbview

import (
	"math"
	"strings"

	"dow-mod-studio/internal/modtree"
)

// ModelPath превращает entity_blueprint_ext/animator ("Races/Space_Marines/
// Structures/Barracks") в путь модели .whm так же, как это делает загрузчик
// сцены: модели лежат под art/ebps.
func ModelPath(animator string) string {
	p := strings.TrimSpace(strings.ReplaceAll(animator, `\`, "/"))
	if p == "" {
		return ""
	}
	p = "Data/art/ebps/" + strings.TrimPrefix(p, "/")
	if !strings.HasSuffix(strings.ToLower(p), ".whm") {
		p += ".whm"
	}
	return p
}

// BlueprintFootprint читает DATASEUI модели и возвращает полуоси её основания
// и высоту в метрах мира: по ним игра рисует «призрак» постройки при выборе
// места. Индикатор выделения лежит в плоскости основания, объём выделения —
// трёхмерный; их полуоси и есть размеры постройки (scale в DATASEUI хранит
// уже полуразмеры, а не масштаб).
func BlueprintFootprint(tree *modtree.ModTree, whmPath string) (half [2]float64, height float64, ok bool) {
	fx := selectedUI(tree, whmPath)
	if fx == nil {
		return half, 0, false
	}
	half = [2]float64{math.Abs(float64(fx.Scale[0])), math.Abs(float64(fx.Scale[1]))}
	if half[0] < 1e-4 || half[1] < 1e-4 {
		// Индикатора нет — основание берётся из объёма выделения.
		half = [2]float64{math.Abs(float64(fx.VolumeScale[0])), math.Abs(float64(fx.VolumeScale[2]))}
	}
	if half[0] < 1e-4 || half[1] < 1e-4 {
		return [2]float64{}, 0, false
	}
	height = math.Abs(float64(fx.VolumeScale[1])) * 2
	if height < 1e-4 {
		height = 4
	}
	return half, height, true
}
