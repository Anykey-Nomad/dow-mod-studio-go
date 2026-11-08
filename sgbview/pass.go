package sgbview

import "math"

// PassMap — карта проходимости сценария (DATAIMAP) с метрикой в мировых
// метрах. Сетка вдвое реже рельефа (128×128 при 256×256 ячеек земли), и
// индекс связан с координатами так же, как у рельефа, только в своей
// шкале: индекс = (мир − origin) / размерЯчейки.
//
// Значения байтов: 1 — непроходимо (границы карты, скалы), 0 и 2 —
// проходимо (0 — обычная земля, 2 — редкие_SPECIAL-ячейки, тоже открытые).
type PassMap struct {
	W, H int
	Data []byte // построчно: Data[y*W+x]
	// CellX, CellY — метров на ячейку; OriginX, OriginY — мировые
	// координаты индекса 0.
	CellX, CellY     float64
	OriginX, OriginY float64
}

// PassableCell — блокирует ли ячейка (cx, cy) путь. За границами сетки
// прохода нет: сетка покрывает играбельную область, за её пределами
// (декоративная периферия рельефа) юниты не ходят. Пустая карта (nil)
// ничего не ограничивает.
func (p *PassMap) PassableCell(cx, cy int) bool {
	if p == nil {
		return true
	}
	if cx < 0 || cy < 0 || cx >= p.W || cy >= p.H {
		return false
	}
	return p.Data[cy*p.W+cx] != 1
}

// Passable — можно ли идти в мировой точке (x, y).
func (p *PassMap) Passable(x, y float64) bool {
	if p == nil || p.CellX <= 0 || p.CellY <= 0 {
		return true
	}
	cx := int((x - p.OriginX) / p.CellX)
	cy := int((y - p.OriginY) / p.CellY)
	return p.PassableCell(cx, cy)
}

// At — значение ячейки в мировой точке и признак попадания в сетку.
func (p *PassMap) At(x, y float64) (byte, bool) {
	if p == nil || p.CellX <= 0 || p.CellY <= 0 {
		return 0, false
	}
	cx, cy := p.Cell(x, y)
	if cx < 0 || cy < 0 || cx >= p.W || cy >= p.H {
		return 0, false
	}
	return p.Data[cy*p.W+cx], true
}

// Cell — мировая точка → индекс ячейки (может выйти за сетку).
func (p *PassMap) Cell(x, y float64) (int, int) {
	return int(math.Floor((x - p.OriginX) / p.CellX)),
		int(math.Floor((y - p.OriginY) / p.CellY))
}

// CellCenter — мировые координаты центра ячейки: точка, куда ведёт
// путь поиска пути.
func (p *PassMap) CellCenter(cx, cy int) (float64, float64) {
	return p.OriginX + (float64(cx)+0.5)*p.CellX,
		p.OriginY + (float64(cy)+0.5)*p.CellY
}
