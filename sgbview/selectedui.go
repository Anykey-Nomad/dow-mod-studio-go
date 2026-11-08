package sgbview

import (
	"math"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
)

// SelectedUI — данные DATASEUI из .whe (страница Selected UI окна Object,
// Classic OE), пересчитанные в мировые координаты сцены: Z вверх, как у всей
// остальной геометрии sgbview.
//
// Источник — чанк DATASEUI файла .whe, лежащего рядом с моделью .whm.
// Значения на диске хранятся в осях модели игры (Y вверх): индикатор лежит в
// плоскости X/Z, объём — трёхмерный X/Y/Z. Пересчёт повторяет то, что делает
// парсер .whm с вершинами (см. whm.decodeVec3): (x, y, z) -> (-x, -z, y),
// а затем объект расставляется по placement'у сценария.
type SelectedUI struct {
	// DisplayType: 0 — круг, 1 — прямоугольник. Как на диске.
	DisplayType int32
	// Scale, Offset — исходные значения DATASEUI (оси модели, Y вверх).
	Scale  [2]float32
	Offset [2]float32
	// Indicator — индикатор выделения в мировых координатах.
	Indicator SelectionQuad
	// Volume — объём выделения в мировых координатах; nil, если в DATASEUI
	// он не задан (нулевые полуоси).
	Volume *SelectionBox
}

// SelectionQuad — рамка индикатора: центр, полуоси в плоскости XY и поворот
// вокруг вертикали.
type SelectionQuad struct {
	Center [3]float64
	Half   [2]float64
	Yaw    float64
}

// SelectionBox — ориентированный объём выделения (ООБ): центр, полуоси в
// собственных осях и поворот вокруг вертикали.
type SelectionBox struct {
	Center [3]float64
	Half   [3]float64
	Yaw    float64
}

// selectedUI parses DATASEUI from the .whe next to the model. The result is
// raw (on-disk axes); call selectedUIWorld to place it in the scene.
func selectedUI(tree *modtree.ModTree, whmPath string) *whe.FxSelectedUI {
	if tree == nil || whmPath == "" {
		return nil
	}
	doc, err := modelWHE(tree, whmPath)
	if err != nil {
		return nil
	}
	value, present, err := doc.SelectedUI()
	if err != nil || !present {
		return nil
	}
	return &value
}

// selectedUIWorld places raw DATASEUI values into the scene using the same
// entity placement the mesh itself was baked with, so the indicator and the
// volume stay glued to the rendered model.
func selectedUIWorld(fx *whe.FxSelectedUI, placement sgb.Placement) *SelectedUI {
	if fx == nil {
		return nil
	}
	if !finiteF32(fx.Scale[0]) || !finiteF32(fx.Scale[1]) ||
		!finiteF32(fx.Offset[0]) || !finiteF32(fx.Offset[1]) {
		return nil
	}
	out := &SelectedUI{
		DisplayType: fx.DisplayType,
		Scale:       fx.Scale,
		Offset:      fx.Offset,
	}

	ch, sh := math.Cos(placement.Heading), math.Sin(placement.Heading)
	rotate := func(x, y float64) (float64, float64) {
		return ch*x - sh*y, sh*x + ch*y
	}
	// Мировая точка из координат модели (Z вверх).
	place := func(mx, my, mz float64) [3]float64 {
		x, y := rotate(mx, my)
		return [3]float64{x + placement.X, y + placement.Y, mz + placement.Z}
	}

	// Индикатор: на диске (offset.X, offset.Z) в плоскости X/Z с полуосями
	// (scale.X, scale.Z); в координаты модели это (-x, -z, 0).
	rx, ry := math.Abs(float64(fx.Scale[0])), math.Abs(float64(fx.Scale[1]))
	if rx >= 1e-4 && ry >= 1e-4 {
		out.Indicator = SelectionQuad{
			Center: place(-float64(fx.Offset[0]), -float64(fx.Offset[1]), 0),
			Half:   [2]float64{rx, ry},
			Yaw:    placement.Heading,
		}
	}

	// Объём: на диске (VolumeOffset.X/Y/Z) и полуоси (VolumeScale.X/Y/Z);
	// Y вверх, поэтому в координатах модели это (−x, −z, y).
	vsx, vsy, vsz := float64(fx.VolumeScale[0]), float64(fx.VolumeScale[1]), float64(fx.VolumeScale[2])
	if finiteF64(vsx) && finiteF64(vsy) && finiteF64(vsz) &&
		math.Abs(vsx) >= 1e-4 && math.Abs(vsy) >= 1e-4 && math.Abs(vsz) >= 1e-4 &&
		finiteF32(fx.VolumeOffset[0]) && finiteF32(fx.VolumeOffset[1]) && finiteF32(fx.VolumeOffset[2]) {
		out.Volume = &SelectionBox{
			Center: place(-float64(fx.VolumeOffset[0]), -float64(fx.VolumeOffset[2]), float64(fx.VolumeOffset[1])),
			Half:   [3]float64{math.Abs(vsx), math.Abs(vsz), math.Abs(vsy)},
			Yaw:    placement.Heading,
		}
	}
	return out
}

func finiteF32(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

func finiteF64(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
