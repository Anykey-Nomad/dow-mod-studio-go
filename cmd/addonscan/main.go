// Command addonscan walks every structure blueprint of a module and prints
// the relation between the addons a building lists and the visibility clips
// its model carries. It is the tool that answers the question "which clip is
// the state of a building without addons".
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/whm"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	root := flag.String("root", dowRoot, "путь к установленной Dawn of War")
	module := flag.String("module", "DoWDE", "модуль")
	folder := flag.String("folder", `Data/attrib/ebps/races`, "папка с чертежами")
	only := flag.String("only", "", "показывать только чертежи, в имени которых есть подстрока")
	names := flag.Bool("names", false, "печатать имена видимых мешей в каждом клипе")
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
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	defer tree.Close()

	dir := tree.FindFolder(*folder)
	if dir == nil {
		fmt.Fprintln(os.Stderr, "папка не найдена:", *folder)
		os.Exit(1)
	}
	var files []*modtree.TreeFile
	collect(dir, &files)
	sort.Slice(files, func(i, j int) bool { return files[i].FullPath < files[j].FullPath })

	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".rgd") {
			continue
		}
		if *only != "" && !strings.Contains(strings.ToLower(f.FullPath), strings.ToLower(*only)) {
			continue
		}
		scan(tree, f, *names)
	}
}

// collect собирает все файлы папки и её подпапок.
func collect(dir *modtree.TreeFolder, out *[]*modtree.TreeFile) {
	*out = append(*out, dir.Files...)
	for _, c := range dir.Children {
		collect(c, out)
	}
}

// scan печатает сводку по одному чертежу постройки.
func scan(tree *modtree.ModTree, f *modtree.TreeFile, withNames bool) {
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf == nil {
		return
	}
	if findChild(rf.Root, "structure_ext") == nil {
		return // не постройка
	}
	animator := childValue(rf.Root, "entity_blueprint_ext", "animator")
	if animator == "" {
		return
	}
	model := loadModel(tree, animator)
	if model == nil {
		fmt.Printf("%-70s модель не найдена\n", rel(f.FullPath))
		return
	}

	// Аддоны постройки и имена их движений.
	var motions []string
	for _, name := range addonList(rf.Root) {
		motion := addonMotion(tree, name)
		motions = append(motions, name+"="+motion)
	}

	// Клипы модели: отдельно видимость, отдельно прочее.
	var vis, other []string
	for _, a := range model.Animations {
		lower := strings.ToLower(a.Name)
		if strings.HasPrefix(lower, "vis") || strings.HasPrefix(lower, "add_on") {
			vis = append(vis, a.Name)
			continue
		}
		other = append(other, a.Name)
	}
	// Какие меши видны в каком клипе. Считаем и по именам: меш может
	// состоять из нескольких кусков, а состояние интересует по именам.
	names := model.MeshNames()
	counts := map[string][3]int{} // клип -> (видно, скрыто, без трека)
	for _, a := range model.Animations {
		var shown, hidden int
		tracked := map[string]bool{}
		for _, tr := range a.Meshes {
			key := strings.ToLower(tr.Name)
			tracked[key] = true
			if tr.Force >= 0.5 {
				shown++
			} else {
				hidden++
			}
		}
		missing := 0
		for name := range names {
			if !tracked[strings.ToLower(name)] {
				missing++
			}
		}
		counts[a.Name] = [3]int{shown, hidden, missing}
	}
	fmt.Printf("=== %s\n", rel(f.FullPath))
	fmt.Printf("    модель: %s (мешей %d, имён %d, клипов %d)\n",
		filepath.Base(model.Name), len(model.Meshes), len(names), len(model.Animations))
	if len(motions) > 0 {
		fmt.Printf("    аддоны: %s\n", strings.Join(motions, ", "))
	} else {
		fmt.Printf("    аддоны: нет\n")
	}
	for _, c := range vis {
		s := counts[c]
		fmt.Printf("    клип %-26s видно %-4d скрыто %-4d без трека %-4d\n", c, s[0], s[1], s[2])
		if withNames {
			fmt.Printf("        %s\n", strings.Join(visibleIn(model, c), ", "))
		}
	}
	if withNames {
		for _, c := range other {
			s := counts[c]
			if s[0] == 0 {
				continue
			}
			fmt.Printf("    клип %-26s видно %-4d скрыто %-4d без трека %-4d\n", c, s[0], s[1], s[2])
			fmt.Printf("        %s\n", strings.Join(visibleIn(model, c), ", "))
		}
	}
	if len(other) > 0 {
		fmt.Printf("    прочие клипы: %s\n", strings.Join(other, ", "))
	}
	fmt.Println()
}

// visibleIn — имена мешей, видимых в клипе.
func visibleIn(m *whm.Model, clip string) []string {
	var out []string
	for _, a := range m.Animations {
		if a.Name != clip {
			continue
		}
		for _, tr := range a.Meshes {
			if tr.Force >= 0.5 {
				out = append(out, tr.Name)
			}
		}
	}
	return out
}

// loadModel читает модель по имени аниматора из чертежа.
func loadModel(tree *modtree.ModTree, animator string) *whm.Model {
	path := strings.ReplaceAll(strings.TrimSpace(animator), `\`, "/")
	path = strings.TrimPrefix(path, "./")
	if len(path) >= 5 && strings.EqualFold(path[:5], "data:") {
		path = path[5:]
	}
	if !strings.HasPrefix(strings.ToLower(path), "data/art/") {
		path = "Data/art/ebps/" + strings.TrimPrefix(path, "Data/")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".whm") {
		path += ".whm"
	}
	f := tree.FindFile(path)
	if f == nil {
		f = tree.FindByName(filepath.Base(path))
	}
	if f == nil {
		return nil
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return nil
	}
	m, err := whm.Parse(data)
	if err != nil {
		return nil
	}
	return m
}

// addonList собирает список аддонов постройки.
func addonList(root *rgd.RgdEntry) []string {
	ext := findChild(root, "addon_ext")
	if ext == nil {
		return nil
	}
	list := findChild(ext, "addons")
	if list == nil {
		return nil
	}
	var out []string
	for _, c := range list.Children {
		if v := strings.TrimSpace(c.DisplayValue()); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// addonMotion читает из чертежа аддона имя его движения.
func addonMotion(tree *modtree.ModTree, name string) string {
	rel := strings.ReplaceAll(strings.TrimSuffix(name, ".lua"), `\`, "/")
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.TrimPrefix(rel, "addons/")
	rel = strings.TrimPrefix(rel, "Addons/")
	paths := []string{
		"Data/attrib/addons/" + rel + ".rgd",
		"Data/attrib/" + rel + ".rgd",
		"Data/" + rel + ".rgd",
	}
	var f *modtree.TreeFile
	for _, p := range paths {
		if f = tree.FindFile(p); f != nil {
			break
		}
	}
	if f == nil {
		f = tree.FindByName(rel + ".rgd")
	}
	if f == nil {
		return "?"
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return "?"
	}
	arf, err := rgd.ParseRgd(data)
	if err != nil || arf == nil {
		return "?"
	}
	return childValue(arf.Root, "add_on_motion_name")
}

func rel(path string) string {
	p := strings.ReplaceAll(path, `\`, "/")
	if i := strings.Index(strings.ToLower(p), "data/"); i >= 0 {
		return p[i:]
	}
	return p
}

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

func childValue(e *rgd.RgdEntry, path ...string) string {
	cur := e
	for _, name := range path {
		cur = findChild(cur, name)
		if cur == nil {
			return ""
		}
	}
	if cur == nil || cur.IsTable() {
		return ""
	}
	return strings.TrimSpace(cur.DisplayValue())
}
