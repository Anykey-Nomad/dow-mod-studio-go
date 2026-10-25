package sgbview

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
)

// TestDiagModels — временная диагностика: разбирает .whm и .rgd игровых
// объектов и печатает, что в них есть.
func TestDiagModels(t *testing.T) {
	root := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
	tree, err := modtree.BuildModTree(root+`\DoWDE.module`, "English")
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	// То, как сценарий зовёт объекты: путь к blueprint'у в data:.
	for _, obj := range []string{
		`data:attrib\ebps\races\necrons\structures\monolith.rgd`,
		`data:attrib\ebps\environment\gameplay\slag_deposit.rgd`,
		`data:attrib\ebps\environment\gameplay\strategic_point.rgd`,
		`data:attrib\ebps\environment\gameplay\strategic_point_flag.rgd`,
		`data:attrib\ebps\environment\gameplay\relic_struct.rgd`,
	} {
		model, path, visible, reason := loadEntityModel(tree, sgb.Entity{Name: obj})
		meshes := 0
		if model != nil {
			meshes = len(model.Meshes)
		}
		fmt.Printf("объект %s: модель=%v путь=%q мешей=%d видно=%q причина=%q\n", obj, model != nil, path, meshes, visCount(visible), reason)
		if model != nil {
			fmt.Printf("   анимаций: %d, треков мешей: %d\n", len(model.Animations), countMeshTracks(model))
			for _, anim := range model.Animations {
				fmt.Printf("   анимация %q длительность %.2f\n", anim.Name, anim.Duration)
				for _, mt := range anim.Meshes {
					fmt.Printf("      меш %-30s ключей %d скрыт=%v\n", mt.Name, len(mt.Keys), mt.ForceInvisible)
				}
			}
			if doc, err := modelWHE(tree, path); err == nil {
				clips := doc.RestClipNames()
				fmt.Printf("   .whe: клипов покоя=%d %q\n", len(clips), clips)
				for _, c := range clips {
					if r, ok := model.VisibleIn([]string{c}); ok {
						fmt.Printf("      клип %-12q видно=%q\n", c, visCount(r))
					} else {
						fmt.Printf("      клип %-12q в модели нет\n", c)
					}
				}
				if r, ok := model.VisibleIn(clips); ok {
					fmt.Printf("   VisibleIn: видно=%q\n", visCount(r))
				} else {
					fmt.Printf("   VisibleIn: не подошёл\n")
				}
			} else {
				fmt.Printf("   .whe: %v\n", err)
			}
			if clip, bs := model.BaseState(); clip != "" {
				fmt.Printf("   BaseState: клип=%q видно=%q\n", clip, visCount(bs))
			} else {
				fmt.Printf("   BaseState: пусто\n")
			}
			all := make([]string, 0)
			for name := range model.MeshNames() {
				if visible != nil && !visible[strings.ToLower(name)] {
					all = append(all, name)
				}
			}
			sort.Strings(all)
			fmt.Printf("   скрыто по итогу (%d): %q\n", len(all), all)
			for _, want := range []string{"vis_empty", "vis_main", "vis_pillars", "add_on_1", "add_on_2", "add_on_3"} {
				an := model.Animation(want)
				if an == nil {
					continue
				}
				fmt.Printf("   клип %q: костей-треков=%d меш-треков=%d\n", an.Name, len(an.Bones), len(an.Meshes))
				for _, tr := range an.Bones {
					if len(tr.PosKey) == 0 {
						continue
					}
					last := tr.PosKey[len(tr.PosKey)-1]
					first := tr.PosKey[0]
					fmt.Printf("      кость %-20s ключей=%d первый=(%.2f %.2f %.2f) последний=(%.2f %.2f %.2f)\n",
						tr.Name, len(tr.PosKey), first.V[0], first.V[1], first.V[2], last.V[0], last.V[1], last.V[2])
				}
			}
			clipNames := make([]string, 0, len(model.Animations))
			for _, an := range model.Animations {
				clipNames = append(clipNames, an.Name)
			}
			sort.Strings(clipNames)
			fmt.Printf("   все клипы (%d): %q\n", len(clipNames), clipNames)
			for _, b := range model.Bones {
				fmt.Printf("   кость скелета %-24s поза=(%.2f %.2f %.2f) родитель=%d\n", b.Name, b.Pos[0], b.Pos[1], b.Pos[2], b.Parent)
			}
			for _, an := range model.Animations {
				lower := strings.ToLower(an.Name)
				if !strings.Contains(lower, "build") && !strings.HasPrefix(lower, "idle") {
					continue
				}
				for _, tr := range an.Bones {
					if tr.Name != "bone_master" || len(tr.PosKey) == 0 {
						continue
					}
					first := tr.PosKey[0]
					last := tr.PosKey[len(tr.PosKey)-1]
					fmt.Printf("   клип %-20q bone_master ключей=%d первый=(%.2f %.2f %.2f) последний=(%.2f %.2f %.2f)\n",
						an.Name, len(tr.PosKey), first.V[0], first.V[1], first.V[2], last.V[0], last.V[1], last.V[2])
				}
			}
			pillarBones := map[string]bool{
				"bone_master": true, "bone_right_pillar": true, "bone_left_pillar": true,
				"bone_rear_pillar": true, "bone_front_pillar": true,
			}
			for _, an := range model.Animations {
				var lines []string
				for _, tr := range an.Bones {
					if !pillarBones[tr.Name] || len(tr.PosKey) == 0 {
						continue
					}
					first := tr.PosKey[0]
					last := tr.PosKey[len(tr.PosKey)-1]
					lines = append(lines, fmt.Sprintf("%s [%d ключей] (%.2f %.2f %.2f)->(%.2f %.2f %.2f)",
						tr.Name, len(tr.PosKey), first.V[0], first.V[1], first.V[2], last.V[0], last.V[1], last.V[2]))
				}
				if len(lines) > 0 {
					fmt.Printf("   клип %q:\n", an.Name)
					for _, l := range lines {
						fmt.Printf("      %s\n", l)
					}
				}
			}
		}
	}
}

// visCount — сколько мешей видно и каких: nil означает состояние, в котором
// видно всё, и печатается это как «всё».
func visCount(visible map[string]bool) string {
	if visible == nil {
		return "всё"
	}
	names := make([]string, 0, len(visible))
	for name := range visible {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf("%d [%s]", len(visible), strings.Join(names, ", "))
}

// countMeshTracks считает треки видимости мешей во всех анимациях.
func countMeshTracks(m *whm.Model) int {
	n := 0
	for _, a := range m.Animations {
		n += len(a.Meshes)
	}
	return n
}

// TestDiagScenarios — временная диагностика: во всех сценариях ищет
// объекты strategic_point и slag_deposit и печатает, как они названы.
func TestDiagScenarios(t *testing.T) {
	root := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`
	tree, err := modtree.BuildModTree(root+`\DoWDE.module`, "English")
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	folder := tree.FindFolder(`Data\scenarios\mp`)
	if folder == nil {
		t.Fatal("нет папки сценариев")
	}
	counts := map[string]int{}
	scen := map[string]int{}
	slag := map[string]int{}
	maxEntities := 0
	maxFile := ""
	entitiesTotal := 0
	for _, f := range folder.Files {
		if !strings.EqualFold(filepath.Ext(f.Name), ".sgb") {
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			continue
		}
		m, err := sgb.Parse(data)
		if err != nil || m == nil {
			continue
		}
		if len(m.Entities) > maxEntities {
			maxEntities = len(m.Entities)
			maxFile = f.Name
		}
		for _, e := range m.Entities {
			entitiesTotal++
			lower := strings.ToLower(e.Name)
			if !strings.Contains(lower, "strategic_point") && !strings.Contains(lower, "slag_deposit") {
				continue
			}
			counts[e.Name]++
			scen[f.Name]++
			if strings.Contains(lower, "slag_deposit") {
				slag[f.Name]++
			}
		}
	}
	fmt.Printf("всего сущностей: %d, максимум в сценарии: %d (%s)\n", entitiesTotal, maxEntities, maxFile)
	fmt.Println("=== сценарии со slag_deposit")
	slagFiles := make([]string, 0, len(slag))
	for n := range slag {
		slagFiles = append(slagFiles, n)
	}
	sort.Strings(slagFiles)
	for _, n := range slagFiles {
		fmt.Printf("  x%-5d %s\n", slag[n], n)
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println("=== имена объектов")
	for _, n := range names {
		fmt.Printf("  x%-5d %s\n", counts[n], n)
	}
	files := make([]string, 0, len(scen))
	for n := range scen {
		files = append(files, n)
	}
	sort.Strings(files)
	fmt.Println("=== сценарии")
	for _, n := range files {
		fmt.Printf("  x%-5d %s\n", scen[n], n)
	}
	_ = rgd.ParseRgd
	_ = whm.Parse
}
