// Command whmscan — слесарная диагностика разбора .whm: ищет в файле
// участники-строки и все смещения, где whm считает, что начинается секция
// материалов, и дампит байты вокруг указанного имени меша. Нужно, чтобы
// понять, почему разбор падает с "cannot locate the material section".
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/internal/whm"
	"dow-mod-studio/modapi"
)

func main() {
	path := os.Args[1]
	needle := os.Args[2]
	data, err := readBytes(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("файл %s, %d байт\n", path, len(data))

	// Все смещения, где начинается валидная секция материалов.
	var sections []int
	for off := 0; off+8 < len(data); off++ {
		if whm.MeshMaterialSection(data, off, true, false) > 0 {
			sections = append(sections, off)
		}
	}
	fmt.Printf("секций материалов (без strict): %d\n", len(sections))
	fmt.Println("первые:", sections[:min(20, len(sections))])

	// Где strict-проверка уже проходит.
	var strict []int
	for _, off := range sections {
		if whm.MeshMaterialSection(data, off, true, true) > 0 {
			strict = append(strict, off)
		}
	}
	fmt.Printf("проходят strict: %d\n", len(strict))
	fmt.Println("первые:", strict[:min(20, len(strict))])

	// Все вхождения искомого имени.
	var idxs []int
	for i := 0; ; {
		j := strings.Index(string(data[i:]), needle)
		if j < 0 {
			break
		}
		i += j
		idxs = append(idxs, i)
		i++
	}
	fmt.Printf("вхождений %q: %d %v\n", needle, len(idxs), idxs)
	for _, idx := range idxs {
		dump(data, idx, needle)
	}
	if len(sections) > 0 {
		fmt.Println("--- первая секция материалов ---")
		dump(data, sections[0], "")
	}
}

func dump(data []byte, idx int, label string) {
	from := max(0, idx-64)
	to := min(len(data), idx+128)
	fmt.Printf("--- дамп %d..%d (%s на %d) ---\n", from, to, label, idx)
	for off := from; off < to; off += 16 {
		line := strings.Builder{}
		hex := strings.Builder{}
		for i := 0; i < 16 && off+i < to; i++ {
			b := data[off+i]
			fmt.Fprintf(&hex, "%02x ", b)
			if b >= 0x20 && b < 0x7f {
				line.WriteByte(b)
			} else {
				line.WriteByte('.')
			}
		}
		fmt.Printf("%06d  %-48s %s\n", off, hex.String(), line.String())
	}
	// int32 по каждому смещению секции — подсказка для чтения дампа.
	_ = binary.LittleEndian
}

// readBytes читает файл или, для пути вида "Data/...", достаёт его из
// дерева модуля (файлы лежат внутри sga-архивов).
func readBytes(path string) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(path), "data/") {
		const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
		modules, err := modapi.DiscoverModules(dowRoot)
		if err != nil {
			return nil, err
		}
		target := modapi.FindModule(modules, "DoWDE")
		if target == nil {
			return nil, fmt.Errorf("модуль DoWDE не найден")
		}
		tree, err := modapi.BuildModTree(target.Path, "English")
		if err != nil {
			return nil, err
		}
		defer tree.Close()
		return tree.ReadFile(path)
	}
	return os.ReadFile(path)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
