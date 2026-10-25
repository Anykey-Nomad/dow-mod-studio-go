// Command raceprobe is a format probe for the data the player and race setup
// screen needs: the race blueprints (data/attrib/racebps), the race folders
// (data/attrib/ebps/races) and the starting positions of a scenario with
// their owners.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	root := flag.String("root", dowRoot, "путь к установленной Dawn of War")
	module := flag.String("module", "DoWDE", "модуль")
	folder := flag.String("folder", `Data\attrib\racebps`, "папка для обхода")
	dump := flag.String("dump", "", "файл .rgd для разбора")
	scenario := flag.String("scenario", "", "сценарий для разбора стартовых позиций")
	races := flag.Bool("races", false, "сводка по расам папки racebps")
	chunks := flag.String("chunks", "", "файл сценария: печать дерева чанков")
	flag.Parse()

	modules, err := modapi.DiscoverModules(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
		os.Exit(1)
	}
	target := modapi.FindModule(modules, *module)
	if target == nil {
		fmt.Fprintln(os.Stderr, "модуль не найден:", *module)
		os.Exit(1)
	}
	tree, err := modtree.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	defer tree.Close()

	switch {
	case *dump != "":
		dumpRgd(tree, *dump)
	case *scenario != "":
		dumpScenario(tree, *scenario)
	case *races:
		dumpRaces(tree, *folder)
	case *chunks != "":
		dumpChunks(tree, *chunks)
	default:
		dumpFolders(tree, *folder)
	}
}

// dumpFolders печатает содержимое папки дерева и папку с расами.
func dumpFolders(tree *modtree.ModTree, folder string) {
	fmt.Printf("=== %s ===\n", folder)
	f := tree.FindFolder(folder)
	if f == nil {
		fmt.Println("  папка не найдена")
	} else {
		for _, file := range f.Files {
			fmt.Printf("  %-40s %s\n", file.Name, file.FullPath)
		}
	}
	fmt.Println("=== data/attrib/ebps/races ===")
	rdir := tree.FindFolder(`Data\attrib\ebps\races`)
	if rdir == nil {
		fmt.Println("  папка не найдена")
		return
	}
	for _, ch := range rdir.Children {
		fmt.Printf("  %-30s файлов: %d, подпапок: %d\n", ch.Name, len(ch.Files), len(ch.Children))
	}
}

// dumpRaces печатает сводку по всем расам папки racebps: имя, локализованный
// идентификатор, играбельность и стартовые постройки.
func dumpRaces(tree *modtree.ModTree, folder string) {
	f := tree.FindFolder(folder)
	if f == nil {
		fmt.Println("папка не найдена:", folder)
		return
	}
	for _, file := range f.Files {
		data, err := tree.ReadTreeFile(file)
		if err != nil {
			fmt.Println(file.Name, "— читать не удалось:", err)
			continue
		}
		rf, err := rgd.ParseRgd(data)
		if err != nil {
			fmt.Println(file.Name, "— разбор не удался:", err)
			continue
		}
		details := findChild(rf.Root, "race_details")
		path := findChild(rf.Root, "race_path")
		starting := findChild(rf.Root, "starting_buildings")
		nameID := ""
		playable := ""
		if details != nil {
			nameID = childValue(details, "name_id")
			playable = childValue(details, "playable")
		}
		buildings := ""
		if starting != nil {
			var list []string
			for _, c := range starting.Children {
				list = append(list, c.Name+"="+c.DisplayValue())
			}
			buildings = strings.Join(list, ", ")
		}
		fmt.Printf("%-28s name_id=%-10s playable=%-6s buildings=[%s]\n", file.Name, nameID, playable, buildings)
		if path != nil {
			fmt.Printf("    building_path=%q unit_path=%q\n",
				childValue(path, "building_path"), childValue(path, "unit_path"))
		}
	}
}

// dumpChunks печатает дерево чанков файла из дерева модуля.
func dumpChunks(tree *modtree.ModTree, path string) {
	data, err := tree.ReadFile(path)
	if err != nil {
		fmt.Println("читать не удалось:", err)
		return
	}
	list, _ := chunky.Parse(data[min(chunky.PreambleSize, len(data)):], 0)
	dumpChunkList(list, 0)
}

// min — меньшее из двух чисел.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// dumpChunkList печатает список чанков с отступом.
func dumpChunkList(list []*chunky.Chunk, depth int) {
	pad := strings.Repeat("  ", depth)
	for _, c := range list {
		fmt.Printf("%s%s v%d name=%q payload=%d детей=%d\n",
			pad, c.TypeID, c.Version, c.Name, len(c.Payload), len(c.Children))
		if len(c.Children) > 0 {
			dumpChunkList(c.Children, depth+1)
		}
	}
}

// findChild ищет поле по имени, без учёта регистра.
func findChild(e *rgd.RgdEntry, name string) *rgd.RgdEntry {
	if e == nil {
		return nil
	}
	for _, c := range e.Children {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

// childValue читает значение поля по имени.
func childValue(e *rgd.RgdEntry, name string) string {
	c := findChild(e, name)
	if c == nil {
		return ""
	}
	return c.DisplayValue()
}

// dumpRgd печатает содержимое .rgd: дерево полей с значениями.
func dumpRgd(tree *modtree.ModTree, path string) {
	f := tree.FindFile(path)
	if f == nil {
		fmt.Println("файл не найден:", path)
		return
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Println("читать не удалось:", err)
		return
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		fmt.Println("разбор не удался:", err)
		return
	}
	fmt.Printf("%s: %d байт, дескриптор %q\n", f.FullPath, len(data), rf.Descriptor)
	dumpEntry(rf.Root, 0, 3)
}

// dumpEntry печатает дерево .rgd с отступом.
func dumpEntry(e *rgd.RgdEntry, depth, maxDepth int) {
	if e == nil || depth > maxDepth {
		return
	}
	pad := strings.Repeat("  ", depth)
	if e.IsTable() {
		fmt.Printf("%s%s [table] %d детей\n", pad, e.Name, len(e.Children))
		for _, c := range e.Children {
			dumpEntry(c, depth+1, maxDepth)
		}
		return
	}
	fmt.Printf("%s%s = %q (type %d)\n", pad, e.Name, e.DisplayValue(), e.Type)
}

// dumpScenario печатает стартовые позиции сценария и их владельцев.
func dumpScenario(tree *modtree.ModTree, path string) {
	data, err := tree.ReadFile(path)
	if err != nil {
		fmt.Println("читать не удалось:", err)
		return
	}
	m, err := sgb.Parse(data)
	if err != nil {
		fmt.Println("разбор не удался:", err)
		return
	}
	fmt.Printf("карта %q, объектов: %d\n", m.Name, len(m.Entities))
	counts := map[string]int{}
	for _, e := range m.Entities {
		counts[strings.ToLower(filepath.Base(e.Name))]++
	}
	fmt.Println("--- объекты по имени (топ-30) ---")
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range counts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
	for i, it := range list {
		if i >= 30 {
			break
		}
		fmt.Printf("  %-40s %d\n", it.k, it.v)
	}
	fmt.Println("--- starting_position ---")
	for _, e := range m.Entities {
		if !strings.Contains(strings.ToLower(e.Name), "starting_position") {
			continue
		}
		fmt.Printf("  id=%-6d owner=%-3d %-60s x=%.1f y=%.1f z=%.1f\n",
			e.ID, e.Owner, e.Name, e.X, e.Y, e.Z)
	}
}
