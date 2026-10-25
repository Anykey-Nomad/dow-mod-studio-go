// Command building is a scratch tool for analysing a building blueprint:
// the model it points at, the addons it lists and what the model itself
// contains (meshes, materials, visibility clips).
//
//	go run ./cmd/building -blueprint "Data/attrib/ebps/races/chaos/structures/chaos_hq.rgd"
package main

import (
	"flag"
	"fmt"
	"os"
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
	blueprint := flag.String("blueprint", "", "чертёж постройки .rgd")
	animator := flag.String("animator", "", "путь к модели .whm (без data:art/ebps)")
	lua := flag.String("lua", "", "имя файла скрипта, который надо найти и напечатать")
	search := flag.String("search", "", "найти файлы по подстроке имени и напечатать их пути")
	clip := flag.String("clip", "", "список клипов через запятую: показать треки видимости подробно")
	model := flag.String("model", "", "путь к модели .whm для -clip")
	clips := flag.String("clips", "", "путь к модели .whm: напечатать список клипов")
	vis := flag.String("vis", "", "путь к модели .whm: напечатать таблицу видимости по интересным мешам")
	all := flag.Bool("all", false, "с -vis показывать все меши, а не только интересные")
	summary := flag.String("summary", "", "чертёж постройки: короткая сводка по модели и аддонам")
	basestate := flag.String("basestate", "", "чертёж постройки: какое состояние без аддонов выбирает whm.BaseState")
	depth := flag.Int("depth", 3, "-blueprint: на сколько уровней вглубь печатать дерево")
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

	switch {
	case *blueprint != "":
		dumpBlueprint(tree, *blueprint, *depth)
	case *animator != "":
		dumpModel(tree, *animator)
	case *lua != "":
		dumpLua(tree, *lua)
	case *search != "":
		searchFiles(tree, *search)
	case *clip != "":
		dumpClips(tree, *model, *clip)
	case *clips != "":
		dumpClipNames(tree, *clips)
	case *vis != "":
		dumpVisibility(tree, *vis, *all)
	case *summary != "":
		dumpSummary(tree, *summary)
	case *basestate != "":
		dumpBaseState(tree, *basestate)
	default:
		fmt.Println("нужен -blueprint, -animator, -lua или -clip")
	}
}

// dumpBlueprint печатает чертеж постройки: модель, аддоны и прочее.
func dumpBlueprint(tree *modtree.ModTree, path string, depth int) {
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
	if err != nil || rf == nil {
		fmt.Println("разбор не удался:", err)
		return
	}
	fmt.Printf("=== %s ===\n", f.FullPath)
	dumpEntry(rf.Root, 1, depth)

	// Модель постройки и её аддоны.
	animator := childValue(rf.Root, "entity_blueprint_ext", "animator")
	fmt.Println("--- модель:", animator)
	if animator != "" {
		dumpModel(tree, animator)
	}
	fmt.Println("--- аддоны:")
	for _, name := range addons(rf.Root) {
		fmt.Println("   ", name)
	}
}

// addons собирает список аддонов постройки: addon_ext.addons.
func addons(root *rgd.RgdEntry) []string {
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

// dumpModel печатает содержимое модели: меши, материалы и клипы видимости.
func dumpModel(tree *modtree.ModTree, animator string) {
	path := virtualPath(animator)
	// Имя аниматора в чертеже — путь от data/art/ebps.
	if !strings.HasPrefix(strings.ToLower(path), "data/art/") {
		path = "Data/art/ebps/" + strings.TrimPrefix(path, "Data/")
	}
	if !strings.HasSuffix(strings.ToLower(path), ".whm") {
		path += ".whm"
	}
	f := tree.FindFile(path)
	if f == nil {
		f = tree.FindByName(baseName(path))
	}
	if f == nil {
		fmt.Println("  модель не найдена:", path)
		return
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Println("  читать не удалось:", err)
		return
	}
	m, err := whm.Parse(data)
	if err != nil {
		fmt.Println("  разбор не удался:", err)
		return
	}
	fmt.Printf("  модель %s: вершин %d, мешей %d, костей %d, клипов %d\n",
		f.FullPath, len(m.Vertices), len(m.Meshes), len(m.Bones), len(m.Animations))
	seen := map[string]bool{}
	for _, mesh := range m.Meshes {
		key := mesh.Name + "|" + mesh.Material
		if seen[key] {
			continue
		}
		seen[key] = true
		fmt.Printf("    меш %-40s материал %-30s треугольников %d\n",
			mesh.Name, mesh.Material, len(mesh.Indices)/3)
	}
	fmt.Println("  --- клипы:")
	// Таблица видимости: строки — меши, столбцы — клипы.
	// Знак: «+» — меш виден, «-» — скрыт принудительно, «?» — нет трека.
	names := []string{}
	for _, mesh := range m.Meshes {
		if !seen[mesh.Name] {
			seen[mesh.Name] = true
			names = append(names, mesh.Name)
		}
	}
	fmt.Printf("    %-24s", "меш \\ клип")
	for _, anim := range m.Animations {
		fmt.Printf(" %-10s", anim.Name)
	}
	fmt.Println()
	for _, name := range names {
		fmt.Printf("    %-24s", name)
		for _, anim := range m.Animations {
			mark := "?"
			for _, mt := range anim.Meshes {
				if mt.Name != name {
					continue
				}
				mark = "+"
				if mt.ForceInvisible {
					mark = "-"
				}
				if len(mt.Keys) > 0 {
					mark = "k"
				}
			}
			fmt.Printf(" %-10s", mark)
		}
		fmt.Println()
	}
	fmt.Println("  --- общие текстуры:")
	for _, t := range m.SharedTextures {
		fmt.Println("    ", t)
	}
}

// loadModel читает модель по пути в дереве модуля.
func loadModel(tree *modtree.ModTree, modelPath string) *whm.Model {
	f := tree.FindFile(modelPath)
	if f == nil {
		f = tree.FindByName(baseName(modelPath))
	}
	if f == nil {
		fmt.Println("модель не найдена:", modelPath)
		return nil
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Println("читать не удалось:", err)
		return nil
	}
	m, err := whm.Parse(data)
	if err != nil {
		fmt.Println("разбор не удался:", err)
		return nil
	}
	return m
}

// dumpClipNames печатает список клипов модели: по именам видно, какие
// клипы отвечают за аддоны и за базовое состояние.
func dumpClipNames(tree *modtree.ModTree, modelPath string) {
	m := loadModel(tree, modelPath)
	if m == nil {
		return
	}
	fmt.Printf("модель %s: клипов %d\n", modelPath, len(m.Animations))
	for _, a := range m.Animations {
		fmt.Printf("  %-34s кадров %-4d длительность %.2f\n", a.Name, a.NumFrames, a.Duration)
	}
}

// dumpVisibility печатает, какие «интересные» меши (аддоны, надстройки,
// стройка) видны в каждом клипе: значение 1 — виден, 0 — скрыт.
//
// Печатаются оба конца кривой: в начале клипа и в конце.
func dumpVisibility(tree *modtree.ModTree, modelPath string, all bool) {
	m := loadModel(tree, modelPath)
	if m == nil {
		return
	}
	interesting := map[string]bool{}
	for _, mesh := range m.Meshes {
		lower := strings.ToLower(mesh.Name)
		if all {
			interesting[mesh.Name] = true
			continue
		}
		for _, part := range []string{"addon", "upgrade", "scaffold", "build", "construct", "extension", "turret", "dish", "radar", "tower", "defence"} {
			if strings.Contains(lower, part) {
				interesting[mesh.Name] = true
				break
			}
		}
	}
	names := make([]string, 0, len(interesting))
	for name := range interesting {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("модель %s: мешей %d, клипов %d, интересных %d\n",
		modelPath, len(m.Meshes), len(m.Animations), len(names))
	// По каждому клипу — список видимых интересных мешей.
	for _, a := range m.Animations {
		var visible []string
		for _, name := range names {
			if visibilityOf(a, name) == "1" {
				visible = append(visible, name)
			}
		}
		if len(visible) == 0 {
			continue
		}
		fmt.Printf("  %-24s %v\n", a.Name, visible)
	}
	fmt.Println("  --- по мешам:")
	for _, name := range names {
		var clips []string
		for _, a := range m.Animations {
			if v := visibilityOf(a, name); v == "1" {
				clips = append(clips, a.Name)
			}
		}
		fmt.Printf("  %-24s %v\n", name, clips)
	}
}

// visibilityOf — строка видимости меша в клипе: «1» — виден, «0» — скрыт,
// «0>1» — в начале скрыт и появляется, «1>0» — исчезает.
func visibilityOf(a whm.Animation, name string) string {
	for _, tr := range a.Meshes {
		if !strings.EqualFold(tr.Name, name) {
			continue
		}
		first := tr.Force
		last := tr.Force
		if len(tr.Keys) > 0 {
			first = tr.Keys[0].V[0]
			last = tr.Keys[len(tr.Keys)-1].V[0]
		}
		switch {
		case first == last:
			if first >= 0.5 {
				return "1"
			}
			return "0"
		case first >= 0.5 && last < 0.5:
			return "1>0"
		case first < 0.5 && last >= 0.5:
			return "0>1"
		default:
			return "?"
		}
	}
	return "-"
}

// dumpSummary печатает короткую сводку по чертежу постройки: модель, список
// клипов видимости с числом видимых и скрытых мешей, аддоны и их клипы.
//
// Это и есть анализ «как здание выглядит без аддонов»: клип, который
// прячет все аддоны и оставляет только корпус, — это базовое состояние.
func dumpSummary(tree *modtree.ModTree, path string) {
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
	if err != nil || rf == nil {
		fmt.Println("разбор не удался:", err)
		return
	}
	fmt.Printf("=== %s ===\n", f.FullPath)
	animator := childValue(rf.Root, "entity_blueprint_ext", "animator")
	names := addons(rf.Root)
	fmt.Printf("модель: %s\n", animator)
	fmt.Printf("аддоны: %v\n", names)
	if animator == "" {
		return
	}
	m := loadModel(tree, modelPath(animator))
	if m == nil {
		return
	}
	meshNames := map[string]bool{}
	for _, mesh := range m.Meshes {
		meshNames[mesh.Name] = true
	}
	fmt.Printf("мешей (уникальных имён): %d, клипов: %d\n", len(meshNames), len(m.Animations))
	for _, a := range m.Animations {
		if !strings.HasPrefix(strings.ToLower(a.Name), "vis") {
			continue
		}
		visible, hidden := 0, 0
		for _, tr := range a.Meshes {
			if tr.Force >= 0.5 {
				visible++
			} else {
				hidden++
			}
		}
		marker := ""
		if len(a.Meshes) == len(meshNames) {
			marker = "  <- покрывает все меши"
		}
		fmt.Printf("  %-24s треков %-4d видимых %-4d скрытых %-4d%s\n",
			a.Name, len(a.Meshes), visible, hidden, marker)
	}
	// Аддоны по чертежам: у каждого своё имя движения.
	for _, name := range names {
		addonPath := virtualPath(strings.TrimSuffix(name, ".lua")) + ".rgd"
		af := tree.FindFile(addonPath)
		if af == nil {
			// Путь в чертеже задан от корня attrib.
			rel := strings.TrimPrefix(strings.ToLower(addonPath), "data/")
			af = tree.FindFile("Data/attrib/" + rel)
		}
		if af == nil {
			af = tree.FindByName(baseName(addonPath))
		}
		if af == nil {
			fmt.Printf("  аддон %s: чертежа нет (%s)\n", name, addonPath)
			continue
		}
		adata, err := tree.ReadTreeFile(af)
		if err != nil {
			fmt.Printf("  аддон %s: читать не удалось\n", name)
			continue
		}
		arf, err := rgd.ParseRgd(adata)
		if err != nil || arf == nil {
			fmt.Printf("  аддон %s: разбор не удался\n", name)
			continue
		}
		motion := childValue(arf.Root, "add_on_motion_name")
		fmt.Printf("  аддон %s: движение %q\n", name, motion)
	}
}

// dumpBaseState печатает, какое состояние постройки без аддонов выбирает
// пакет whm: имя клипа и список видимых мешей.
func dumpBaseState(tree *modtree.ModTree, path string) {
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
	if err != nil || rf == nil {
		fmt.Println("разбор не удался:", err)
		return
	}
	animator := childValue(rf.Root, "entity_blueprint_ext", "animator")
	fmt.Printf("=== %s ===\n", f.FullPath)
	fmt.Printf("аддоны: %v\n", addons(rf.Root))
	m := loadModel(tree, modelPath(animator))
	if m == nil {
		return
	}
	names := m.MeshNames()
	fmt.Printf("уникальных имён мешей: %d\n", len(names))
	clip, visible := m.BaseState()
	if visible == nil {
		fmt.Println("базовое состояние: НЕ НАЙДЕНО (видно всё)")
		return
	}
	fmt.Printf("базовое состояние: клип %q, видно %d из %d\n", clip, len(visible), len(names))
	list := make([]string, 0, len(visible))
	for name := range visible {
		list = append(list, name)
	}
	sort.Strings(list)
	fmt.Printf("  %s\n", strings.Join(list, ", "))
}

// modelPath превращает имя аниматора из чертежа в путь к модели в дереве.
func modelPath(animator string) string {
	p := virtualPath(animator)
	if !strings.HasPrefix(strings.ToLower(p), "data/art/") {
		p = "Data/art/ebps/" + strings.TrimPrefix(p, "Data/")
	}
	if !strings.HasSuffix(strings.ToLower(p), ".whm") {
		p += ".whm"
	}
	return p
}

// dumpClips печатает треки видимости указанных клипов: значение, на котором
// меш удерживается, и ключи кривой.
func dumpClips(tree *modtree.ModTree, modelPath, clips string) {
	if modelPath == "" {
		fmt.Println("нужен -model")
		return
	}
	f := tree.FindFile(modelPath)
	if f == nil {
		f = tree.FindByName(baseName(modelPath))
	}
	if f == nil {
		fmt.Println("модель не найдена:", modelPath)
		return
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Println("читать не удалось:", err)
		return
	}
	m, err := whm.Parse(data)
	if err != nil {
		fmt.Println("разбор не удался:", err)
		return
	}
	fmt.Printf("модель %s: мешей %d, клипов %d\n", f.FullPath, len(m.Meshes), len(m.Animations))
	for _, name := range strings.Split(clips, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		anim := m.Animation(name)
		if anim == nil {
			fmt.Printf("клип %q не найден\n", name)
			continue
		}
		fmt.Printf("--- клип %q: кадров %d, длительность %.2f, треков %d\n",
			anim.Name, anim.NumFrames, anim.Duration, len(anim.Meshes))
		for _, tr := range anim.Meshes {
			fmt.Printf("    %-28s удерживается=%.3f ключей=%d", tr.Name, tr.Force, len(tr.Keys))
			for _, k := range tr.Keys {
				fmt.Printf(" [%.3f=%.3f]", k.Time, k.V[0])
			}
			fmt.Println()
		}
	}
}

// searchFiles печатает пути файлов, чьё имя содержит подстроку.
func searchFiles(tree *modtree.ModTree, query string) {
	hits := tree.Search(query, modtree.SearchAll, 0)
	fmt.Printf("найдено файлов: %d\n", len(hits))
	for i, h := range hits {
		if i >= 60 {
			fmt.Println("...")
			break
		}
		fmt.Println("  ", h.File.FullPath)
	}
}

// dumpLua ищет в дереве файл скрипта по имени и печатает его.
func dumpLua(tree *modtree.ModTree, name string) {
	f := tree.FindByName(baseName(name))
	if f == nil {
		fmt.Println("скрипт не найден:", name)
		return
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Println("читать не удалось:", err)
		return
	}
	fmt.Printf("=== %s (%d байт) ===\n", f.FullPath, len(data))
	fmt.Println(string(data))
}

// --- helpers ----------------------------------------------------------------

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
	fmt.Printf("%s%s = %q\n", pad, e.Name, e.DisplayValue())
}

func virtualPath(path string) string {
	p := strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	p = strings.TrimPrefix(p, "./")
	if len(p) >= 5 && strings.EqualFold(p[:5], "data:") {
		p = p[5:]
	}
	p = strings.TrimLeft(p, "/")
	if !strings.HasPrefix(strings.ToLower(p), "data/") {
		p = "Data/" + p
	}
	return p
}

func baseName(path string) string {
	p := strings.ReplaceAll(path, `\`, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
