package sgbview

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
)

// TestDiagScene — временная диагностика: собирает сцену сценария и
// печатает, что получилось у объектов strategic_point и slag_deposit:
// текстуры, альфу, размеры.
func TestDiagScene(t *testing.T) {
	root := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
	tree, err := modtree.BuildModTree(root+`\DoWDE.module`, "English")
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	scen := os.Getenv("SCEN")
	if scen == "" {
		scen = "2p_abandon_all_hope.sgb"
	}
	data, err := tree.ReadFile(`Data/Scenarios/MP/` + scen)
	if err != nil {
		t.Fatal(err)
	}
	m, err := sgb.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := Build(tree, m, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("сценарий %s: объектов: %d, пропущено: %d, отброшено по лимитам: %d, сущностей: %d\n",
		scen, len(sc.Objects), len(sc.Skipped), sc.Dropped, sc.Entities)
	fmt.Printf("  сущностей в карте: %d, лимит объектов: %d, лимит вершин: %d\n",
		len(m.Entities), DefaultLimits().MaxObjects, DefaultLimits().MaxVertices)
	byName := map[string]int{}
	for _, e := range m.Entities {
		byName[e.Name]++
	}
	type kv struct {
		n string
		c int
	}
	list := make([]kv, 0, len(byName))
	for n, c := range byName {
		list = append(list, kv{n, c})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].c > list[j].c })
	for i, e := range list {
		if i >= 15 {
			break
		}
		fmt.Printf("    %-60s %d\n", e.n, e.c)
	}
	for _, s := range sc.Skipped {
		fmt.Println("  пропущен:", s.Name, "—", s.Reason)
	}
	limit := 20
	uniq := map[string]int{}
	for _, s := range sc.Skipped {
		uniq[s.Reason]++
	}
	for reason, n := range uniq {
		fmt.Printf("   причина x%-4d %s\n", n, reason)
	}
	_ = limit

	// Считаем, какие текстуры у каких материалов и какая в них альфа.
	alpha := map[string][2]int{}
	for _, obj := range sc.Objects {
		for _, p := range obj.Parts {
			if p.Texture == nil {
				continue
			}
			lo, hi := alpha[p.Texture.Key][0], alpha[p.Texture.Key][1]
			for i := 3; i < len(p.Texture.Pixels); i += 4 {
				a := int(p.Texture.Pixels[i])
				if a < lo {
					lo = a
				}
				if a > hi {
					hi = a
				}
			}
			alpha[p.Texture.Key] = [2]int{lo, hi}
		}
	}
	for key, v := range alpha {
		fmt.Printf("  текстура %-70s альфа %d..%d\n", key, v[0], v[1])
	}

	// Что получилось у нужных объектов.
	for _, obj := range sc.Objects {
		lower := strings.ToLower(obj.Name)
		if !strings.Contains(lower, "strategic_point") && !strings.Contains(lower, "slag_deposit") {
			continue
		}
		fmt.Println("объект:", obj.Name)
		for _, p := range obj.Parts {
			if p.Mesh == nil {
				continue
			}
			tex := "нет"
			if p.Texture != nil {
				tex = fmt.Sprintf("%dx%d", p.Texture.Width, p.Texture.Height)
			}
			var minZ, maxZ float32 = 1e30, -1e30
			for _, v := range p.Mesh.Vertices {
				if v.Pos[2] < minZ {
					minZ = v.Pos[2]
				}
				if v.Pos[2] > maxZ {
					maxZ = v.Pos[2]
				}
			}
			fmt.Printf("   группа %-10s материал %-60s текстура %-10s вершин %5d z %.1f..%.1f\n",
				p.Mesh.Name, p.Mesh.Material, tex, len(p.Mesh.Vertices), minZ, maxZ)
		}
	}
}
