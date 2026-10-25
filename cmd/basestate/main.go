// Command basestate prints, for every race HQ of the module, what state the
// model shows without addons: which clips the model holds, how many meshes
// each of them keeps visible, and what whm.BaseState() picks today.
//
//	go run ./cmd/basestate
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
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

// hqBlueprints — чертежи штаб-квартир рас: путь в дереве и название.
var hqBlueprints = []struct {
	race string
	path string
}{
	{"chaos", "Data/attrib/ebps/races/chaos/structures/chaos_hq.rgd"},
	{"dark_eldar", "Data/attrib/ebps/races/dark_eldar/structures/dark_eldar_hq.rgd"},
	{"eldar", "Data/attrib/ebps/races/eldar/structures/eldar_hq.rgd"},
	{"guard", "Data/attrib/ebps/races/guard/structures/guard_hq.rgd"},
	{"necron", "Data/attrib/ebps/races/necrons/structures/monolith.rgd"},
	{"ork", "Data/attrib/ebps/races/orks/structures/ork_hq.rgd"},
	{"sisters", "Data/attrib/ebps/races/sisters/structures/sisters_hq.rgd"},
	{"space_marines", "Data/attrib/ebps/races/space_marines/structures/space_marine_hq.rgd"},
	{"tau", "Data/attrib/ebps/races/tau/structures/tau_hq.rgd"},
}

func main() {
	root := flag.String("root", dowRoot, "путь к установленной Dawn of War")
	module := flag.String("module", "DoWDE", "модуль")
	names := flag.Bool("names", false, "печатать имена видимых мешей по клипам")
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

	for _, hq := range hqBlueprints {
		report(tree, hq.race, hq.path, *names)
	}
}

// report печатает сводку по одному чертежу: клипы модели и то, что выбрал
// BaseState.
func report(tree *modtree.ModTree, race, path string, names bool) {
	f := tree.FindFile(path)
	if f == nil {
		fmt.Printf("=== %s: чертёж не найден (%s)\n", race, path)
		return
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Printf("=== %s: читать не удалось: %v\n", race, err)
		return
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf == nil {
		fmt.Printf("=== %s: разбор не удался: %v\n", race, err)
		return
	}
	animator := childValue(rf.Root, "entity_blueprint_ext", "animator")
	m := loadModel(tree, modelPath(animator))
	if m == nil {
		fmt.Printf("=== %s: модель не найдена (%s)\n", race, animator)
		return
	}
	meshNames := m.MeshNames()
	fmt.Printf("\n=== %s: %s\n", race, f.FullPath)
	fmt.Printf("    модель %s: мешей %d (имён %d), клипов %d\n",
		shortPath(modelPath(animator)), len(m.Meshes), len(meshNames), len(m.Animations))
	fmt.Printf("    аддоны: %v\n", addonMotions(tree, rf.Root))

	clip, visible := m.BaseState()
	if visible == nil {
		fmt.Printf("    BaseState: НЕ НАЙДЕНО (видно всё)\n")
	} else {
		fmt.Printf("    BaseState: клип %q, видно %d из %d\n", clip, len(visible), len(meshNames))
	}

	// Что говорит граф действий .whe рядом с моделью: клипы нетронутой
	// постройки и объединение их флагов.
	var wheVisible map[string]bool
	if doc, err := wheDocument(tree, modelPath(animator)); err == nil {
		if clips := doc.RestClipNames(); len(clips) > 0 {
			if v, ok := m.VisibleIn(clips); ok {
				wheVisible = v
				fmt.Printf("    whe: клипы %s\n", strings.Join(clips, ", "))
				fmt.Printf("    whe-rest: видно %d из %d\n", len(v), len(meshNames))
			}
		}
	}
	if wheVisible == nil {
		fmt.Printf("    whe: граф не нашёлся\n")
	}

	// Все клипы модели одной строкой.
	var clipNames []string
	for i := range m.Animations {
		clipNames = append(clipNames, m.Animations[i].Name)
	}
	fmt.Printf("    клипы: %s\n", strings.Join(clipNames, ", "))

	// Все клипы с их числом видимых мешей.
	type row struct {
		name    string
		tracks  int
		visible int
		names   []string
	}
	var rows []row
	for i := range m.Animations {
		a := &m.Animations[i]
		r := row{name: a.Name, tracks: len(a.Meshes)}
		for _, tr := range a.Meshes {
			if visibleByCurve(tr) {
				r.visible++
				r.names = append(r.names, strings.ToLower(tr.Name))
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, r := range rows {
		if !strings.HasPrefix(strings.ToLower(r.name), "vis") {
			continue
		}
		fmt.Printf("      %-26s треков %-4d видно %-4d\n", r.name, r.tracks, r.visible)
		if names {
			sort.Strings(r.names)
			fmt.Printf("          %s\n", strings.Join(r.names, ", "))
		}
	}

	// Объединение видимых мешей по всем клипам vis_*: сколько всего частей
	// модели вообще может быть видно.
	union := map[string]bool{}
	for i := range m.Animations {
		a := &m.Animations[i]
		if !strings.HasPrefix(strings.ToLower(a.Name), "vis") {
			continue
		}
		for _, tr := range a.Meshes {
			if visibleByCurve(tr) {
				union[strings.ToLower(tr.Name)] = true
			}
		}
	}
	fmt.Printf("    объединение клипов vis_*: %d из %d\n", len(union), len(meshNames))
	if visible != nil {
		missing := []string{}
		for name := range meshNames {
			if !union[strings.ToLower(name)] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			fmt.Printf("    вне vis_*: %s\n", strings.Join(missing, ", "))
		}
	}

	// Базовое состояние по аддонам: всё, кроме мешей, которые показывают
	// клипы аддонов (vis_addon_N / add_on_N).
	addonMeshes := map[string]bool{}
	motions := addonMotions(tree, rf.Root)
	for _, motion := range motions {
		idx := strings.Index(motion, "=")
		if idx < 0 {
			continue
		}
		name := motion[idx+1:]
		for _, prefix := range []string{"vis_", "add_on_", ""} {
			clip := m.Animation(prefix + name)
			if clip == nil {
				continue
			}
			for _, tr := range clip.Meshes {
				if visibleByCurve(tr) {
					addonMeshes[strings.ToLower(tr.Name)] = true
				}
			}
		}
	}
	base := map[string]bool{}
	for name := range meshNames {
		if !addonMeshes[strings.ToLower(name)] {
			base[strings.ToLower(name)] = true
		}
	}
	sort.Strings(motions)
	fmt.Printf("    база по аддонам: %d из %d (аддон-мешей %d: %s)\n",
		len(base), len(meshNames), len(addonMeshes), joinKeys(addonMeshes))

	// Сравнение с BaseState.
	if visible != nil {
		onlyBase, onlyState := diff(visible, base)
		if len(onlyBase) == 0 && len(onlyState) == 0 {
			fmt.Printf("    BaseState СОВПАДАЕТ с базой по аддонам\n")
		} else {
			fmt.Printf("    расхождение: лишнее в BaseState [%s], не хватает [%s]\n",
				strings.Join(onlyState, ", "), strings.Join(onlyBase, ", "))
		}
	}

	// Сравнение с тем, что говорит граф .whe.
	if visible != nil && wheVisible != nil {
		extraInBase, missingInBase := diff(visible, wheVisible)
		if len(extraInBase) == 0 && len(missingInBase) == 0 {
			fmt.Printf("    whe СОВПАДАЕТ с BaseState\n")
		} else {
			fmt.Printf("    whe против BaseState: в BaseState лишнее [%s], не хватает [%s]\n",
				strings.Join(extraInBase, ", "), strings.Join(missingInBase, ", "))
		}
	}
}

// visibleByCurve — виден ли меш по своей кривой видимости.
func visibleByCurve(tr whm.MeshTrack) bool {
	value := tr.Force
	if len(tr.Keys) > 0 {
		value = tr.Keys[len(tr.Keys)-1].V[0]
	}
	return value >= 0.5
}

// joinKeys — имена из карты через запятую, по возрастанию.
func joinKeys(set map[string]bool) string {
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

// diff — что есть в одном множестве и нет в другом.
func diff(a, b map[string]bool) (onlyA, onlyB []string) {
	for k := range a {
		if !b[k] {
			onlyA = append(onlyA, k)
		}
	}
	for k := range b {
		if !a[k] {
			onlyB = append(onlyB, k)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	return onlyA, onlyB
}

// loadModel читает модель из дерева по виртуальному пути.
func loadModel(tree *modtree.ModTree, path string) *whm.Model {
	f := tree.FindFile(path)
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

// wheDocument читает файл .whe, лежащий рядом с моделью: в нём — граф
// действий, который говорит, какие клипы играют у нетронутой постройки.
func wheDocument(tree *modtree.ModTree, whmPath string) (*whe.Document, error) {
	whePath := strings.TrimSuffix(whmPath, filepath.Ext(whmPath)) + ".whe"
	f := tree.FindFile(whePath)
	if f == nil {
		return nil, fmt.Errorf("файл не найден: %s", whePath)
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return nil, err
	}
	return whe.ParseDocument(data)
}

// modelPath превращает имя аниматора из чертежа в путь к модели в дереве.
func modelPath(animator string) string {
	p := strings.ReplaceAll(strings.TrimSpace(animator), `\`, "/")
	if !strings.HasPrefix(strings.ToLower(p), "data/art/") {
		p = "Data/art/ebps/" + strings.TrimPrefix(p, "Data/")
	}
	if !strings.HasSuffix(strings.ToLower(p), ".whm") {
		p += ".whm"
	}
	return p
}

// shortPath — короткая запись пути модели: имя файла.
func shortPath(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// addonMotions — имена аддонов чертежа и их движения видимости: каким
// клипом модели показывается достроенный аддон.
func addonMotions(tree *modtree.ModTree, root *rgd.RgdEntry) []string {
	var out []string
	list := findChild(root, "addon_ext")
	if list != nil {
		list = findChild(list, "addons")
	}
	if list == nil {
		return out
	}
	for _, c := range list.Children {
		name := c.DisplayValue()
		if strings.TrimSpace(name) == "" {
			continue
		}
		out = append(out, filepath.Base(name)+"="+motionName(tree, name))
	}
	return out
}

// motionName читает add_on_motion_name чертежа аддона: по нему модель
// находит клип, показывающий аддон.
func motionName(tree *modtree.ModTree, addon string) string {
	name := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(addon, `\`, "/")), filepath.Ext(addon))
	for _, path := range []string{
		"Data/attrib/addons/" + name + ".rgd",
		"Data/attrib/addons/" + name + ".lua",
	} {
		f := tree.FindFile(path)
		if f == nil {
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			return ""
		}
		rf, err := rgd.ParseRgd(data)
		if err != nil || rf == nil {
			return ""
		}
		if v := childValue(rf.Root, "add_on_motion_name"); v != "" {
			return v
		}
	}
	return "?"
}

// childValue читает значение по пути в дереве записи.
func childValue(e *rgd.RgdEntry, path ...string) string {
	cur := e
	for _, name := range path {
		cur = findChild(cur, name)
		if cur == nil {
			return ""
		}
	}
	return cur.DisplayValue()
}

// findChild ищет дочернюю запись по имени.
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
