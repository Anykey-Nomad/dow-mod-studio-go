// Command passprobe печатает метрику карты проходимости (.sgb): размеры
// рельефа и DATAIMAP, заголовок земли и гистограмму значений — чтобы
// понять, в каких ячейках какой код проходимости лежит.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"dow-mod-studio/internal/sgb"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: passprobe file.sgb")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	m, err := sgb.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	g := m.Ground
	fmt.Printf("relief: %dx%d  pass: %dx%d\n", m.Relief.W, m.Relief.H, m.Pass.W, m.Pass.H)
	fmt.Printf("ground: center=(%d,%d) cell=%g cells=%dx%d heightScale=%g\n",
		g.CenterX, g.CenterZ, g.CellSize, g.CellsX, g.CellsZ, g.HeightScale)
	hist := map[byte]int{}
	for _, v := range m.Pass.Data {
		hist[v]++
	}
	for v, n := range hist {
		fmt.Printf("  pass[%d] = %d ячеек\n", v, n)
	}
	// Проверка метрики как в sgbview.PassMap: ячейка = (мир−origin)/cell.
	if m.Pass.W > 0 && g.CellSize > 0 {
		cellX := float64(g.CellsX) * float64(g.CellSize) / float64(m.Pass.W)
		cellY := float64(g.CellsZ) * float64(g.CellSize) / float64(m.Pass.H)
		ox := -float64(g.CenterX-1) * float64(g.CellSize)
		oy := -float64(g.CenterZ-1) * float64(g.CellSize)
		at := func(x, y float64) byte {
			cx := int((x - ox) / cellX)
			cy := int((y - oy) / cellY)
			return m.Pass.At(cx, cy)
		}
		fmt.Printf("метрика: cell=(%g,%g) origin=(%g,%g)\n", cellX, cellY, ox, oy)
		fmt.Printf("  центр (0,0) = %d (ожидается 0 — открыто)\n", at(0, 0))
		fmt.Printf("  угол (-250,-250) = %d (ожидается 1 — граница)\n", at(-250, -250))
		fmt.Printf("  угол (250,250) = %d (ожидается 1 — граница)\n", at(250, 250))
	}

	// Ориентация сетки: сценарий ставит объекты только на проходимую
	// землю — считаем, сколько из них попадает на «1» при ориентации
	// как есть и при транспонированной (если DATAIMAP хранится
	// столбцами, как DATAHMAP, до копирования в строковый порядок).
	if m.Pass.W > 0 && g.CellSize > 0 {
		cellX := float64(g.CellsX) * float64(g.CellSize) / float64(m.Pass.W)
		cellY := float64(g.CellsZ) * float64(g.CellSize) / float64(m.Pass.H)
		ox := -float64(g.CenterX-1) * float64(g.CellSize)
		oy := -float64(g.CenterZ-1) * float64(g.CellSize)
		asIs, transposed, total, skipped := 0, 0, 0, 0
		for _, e := range m.Entities {
			cx := int((float64(e.X) - ox) / cellX)
			cy := int((float64(e.Z) - oy) / cellY)
			if cx < 0 || cy < 0 || cx >= m.Pass.W || cy >= m.Pass.H {
				skipped++
				continue
			}
			total++
			if m.Pass.At(cx, cy) == 1 {
				asIs++
			}
			if m.Pass.At(cy, cx) == 1 {
				transposed++
			}
		}
		fmt.Printf("объектов сценария=%d (вне сетки=%d); на непроходимых: как есть=%d, транспонированно=%d\n",
			total, skipped, asIs, transposed)
		// Первые заблокированные — координаты и соседство ячейки.
		shown := 0
		for _, e := range m.Entities {
			cx := int((float64(e.X) - ox) / cellX)
			cy := int((float64(e.Z) - oy) / cellY)
			if cx < 0 || cy < 0 || cx >= m.Pass.W || cy >= m.Pass.H || m.Pass.At(cx, cy) != 1 {
				continue
			}
			fmt.Printf("  %s: мир=(%.0f,%.0f) ячейка=(%d,%d) вокруг:", e.Name, e.X, e.Z, cx, cy)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					fmt.Printf(" %d", m.Pass.At(cx+dx, cy+dy))
				}
			}
			fmt.Println()
			shown++
			if shown >= 15 {
				break
			}
		}
	}

	// Подбор метрики: перебор размера ячейки и смещения против объектов,
	// которые гарантированно стоят на проходимой земле (стартовые позиции,
	// стратегические точки, реликты, слаг-депозиты). Строения.environment
	// в перебор не берём — здания сами блокируют проходимость.
	if m.Pass.W > 0 && g.CellSize > 0 {
		type combo struct {
			cell      float64
			ox, oy    float64
			transpose bool
			blocked   int
			total     int
		}
		var reliable []struct {
			name string
			x, z float64
		}
		for _, e := range m.Entities {
			n := strings.ToLower(e.Name)
			if strings.Contains(n, "starting_position") || strings.Contains(n, "strategic_point") ||
				strings.Contains(n, "relic") || strings.Contains(n, "slag") {
				reliable = append(reliable, struct {
					name string
					x, z float64
				}{n, float64(e.X), float64(e.Z)})
			}
		}
		var combos []combo
		for _, transpose := range []bool{false, true} {
			for _, cell := range []float64{1, 2, 3, 4, 5, 6, 8, 10, 12, 16} {
				for ox := -384.0; ox <= 384; ox += 64 {
					for oy := -384.0; oy <= 384; oy += 64 {
						c := combo{cell: cell, ox: ox, oy: oy, transpose: transpose}
						for _, r := range reliable {
							cx := int((r.x - ox) / cell)
							cy := int((r.z - oy) / cell)
							if transpose {
								cx, cy = cy, cx
							}
							if cx < 0 || cy < 0 || cx >= m.Pass.W || cy >= m.Pass.H {
								continue // вне сетки — комбинация не в счёт
							}
							c.total++
							if m.Pass.At(cx, cy) == 1 {
								c.blocked++
							}
						}
						if c.total < len(reliable) {
							continue // объекты вне сетки — метрика не та
						}
						combos = append(combos, c)
					}
				}
			}
		}
		sort.Slice(combos, func(i, j int) bool { return combos[i].blocked < combos[j].blocked })
		fmt.Printf("надёжных объектов: %d; лучшие метрики:\n", len(reliable))
		for _, c := range combos[:5] {
			fmt.Printf("  cell=%g origin=(%g,%g) transpose=%v → blocked %d/%d\n",
				c.cell, c.ox, c.oy, c.transpose, c.blocked, c.total)
		}
	}

	// Две популяции для окончательного выбора метрики: «гарантированно
	// открытые» (стартовые позиции, точки захвата — юниты там обязаны
	// стоять) и «гарантированно занятые» (здания environment — сквозь
	// них ходить нельзя, редактор помечает их пятна непроходимыми).
	// Правильная метрика закрывает вторые и не закрывает первых.
	var openPop, blockPop [][2]float64
	for _, e := range m.Entities {
		n := strings.ToLower(e.Name)
		switch {
		case strings.Contains(n, "starting_position") || strings.Contains(n, "strategic_point") ||
			strings.Contains(n, "relic") || strings.Contains(n, "slag"):
			openPop = append(openPop, [2]float64{float64(e.X), float64(e.Z)})
		case strings.Contains(n, "habblock") || (strings.Contains(n, "necron_bldg") && !strings.Contains(n, "gameplay")):
			blockPop = append(blockPop, [2]float64{float64(e.X), float64(e.Z)})
		}
	}
	fmt.Printf("популяции: открытых=%d, зданий=%d\n", len(openPop), len(blockPop))
	for _, cand := range []struct {
		cell      float64
		ox, oy    float64
		transpose bool
		note      string
	}{{4, -256, -256, false, "4м, край ±256"}, {2, -128, -128, false, "2м, кольцо ±128"},
		{4, -256, -256, true, "4м, транспонировано"}, {2, -128, -128, true, "2м/±128, транспонировано"},
		{8, -512, -512, false, "8м, край ±512(?)"}} {
		count := func(pop [][2]float64) int {
			blocked := 0
			for _, p := range pop {
				cx := int((p[0] - cand.ox) / cand.cell)
				cy := int((p[1] - cand.oy) / cand.cell)
				if cand.transpose {
					cx, cy = cy, cx
				}
				if cx < 0 || cy < 0 || cx >= m.Pass.W || cy >= m.Pass.H {
					continue
				}
				if m.Pass.At(cx, cy) == 1 {
					blocked++
				}
			}
			return blocked
		}
		fmt.Printf("  %-24s открытых закрыто=%d/%d, зданий закрыто=%d/%d\n",
			cand.note, count(openPop), len(openPop), count(blockPop), len(blockPop))
	}

	// Профиль высот рельефа поперёк карты: если играбельная граница
	// на ±128 — там будет стенка (резкий перепад высот).
	if m.Relief.W > 0 && g.CellSize > 0 {
		height := func(x, z float64) float64 {
			i := int(x/float64(g.CellSize) + float64(g.CenterX-1))
			j := int(z/float64(g.CellSize) + float64(g.CenterZ-1))
			return float64(m.Relief.At(i, j)) * float64(g.HeightScale)
		}
		fmt.Print("профиль y=0, x=-176..-96: ")
		for x := -176.0; x <= -96; x += 8 {
			fmt.Printf("%.0f:%.1f ", x, height(x, 0))
		}
		fmt.Println()
		fmt.Print("профиль x=0, y=-176..-96: ")
		for y := -176.0; y <= -96; y += 8 {
			fmt.Printf("%.0f:%.1f ", y, height(0, y))
		}
		fmt.Println()
	}

	// Проверка итоговой метрики (как в sgbview.BuildWith): ячейка =
	// размеру ячейки рельефа, сетка отцентрована по миру.
	if m.Pass.W > 0 && g.CellSize > 0 {
		cell := float64(g.CellSize)
		cx0 := (float64(g.XMin()) + float64(g.XMax())) * 0.5
		cy0 := (float64(g.ZMin()) + float64(g.ZMax())) * 0.5
		ox := cx0 - float64(m.Pass.W)*cell*0.5
		oy := cy0 - float64(m.Pass.H)*cell*0.5
		at := func(x, y float64) byte {
			cx := int((x - ox) / cell)
			cy := int((y - oy) / cell)
			if cx < 0 || cy < 0 || cx >= m.Pass.W || cy >= m.Pass.H {
				return 255 // вне сетки
			}
			return m.Pass.At(cx, cy)
		}
		fmt.Printf("итоговая метрика: cell=%g origin=(%g,%g)\n", cell, ox, oy)
		fmt.Printf("  центр (0,0) = %d (ожидается 0 — открыто)\n", at(0, 0))
		fmt.Printf("  стартовая (24,100) = %d (ожидается 0 — открыто)\n", at(24, 100))
		fmt.Printf("  стартовая (-102,-26) = %d (ожидается 0 — открыто)\n", at(-102, -26))
		fmt.Printf("  за границей (250,0) = %d (ожидается 255 — вне сетки)\n", at(250, 0))
	}

	// ASCII-карта проходимости: 1 = '#', 2 = '+', 0 = '.'.
	for y := 0; y < m.Pass.H; y += 2 {
		line := make([]byte, 0, m.Pass.W/2)
		for x := 0; x < m.Pass.W; x += 2 {
			switch m.Pass.At(x, y) {
			case 1:
				line = append(line, '#')
			case 2:
				line = append(line, '+')
			default:
				line = append(line, '.')
			}
		}
		fmt.Println(string(line))
	}
}
