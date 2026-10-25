// Command wheacts prints the action layer of a model's .whe: every named
// action, its guarded motion choices and the conditions they name. The chain
// is how the engine decides which vis_* clips a building shows: the condition
// names (addon_1, damage_50, build_progress) are the game states, and each
// motion shares its name with a clip of the model.
//
//	go run ./cmd/wheacts "Data/art/ebps/races/imperial_guard/structures/guard_hq.whe"
package main

import (
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: wheacts <path to .whe> ...")
		os.Exit(1)
	}
	modules, err := modapi.DiscoverModules(dowRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
		os.Exit(1)
	}
	target := modapi.FindModule(modules, "DoWDE")
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	defer tree.Close()

	for _, arg := range os.Args[1:] {
		report(tree, arg)
	}
}

func report(tree *modtree.ModTree, path string) {
	f := tree.FindFile(path)
	if f == nil {
		fmt.Printf("=== %s: файл не найден\n", path)
		return
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Printf("=== %s: читать не удалось: %v\n", path, err)
		return
	}
	doc, err := whe.ParseDocument(data)
	if err != nil {
		fmt.Printf("=== %s: разбор не удался: %v\n", path, err)
		return
	}
	fmt.Printf("\n=== %s\n", f.FullPath)

	// Имена клипов модели лежат рядом: тот же путь с .whm.
	clips := loadClipNames(tree, strings.TrimSuffix(path, filepathExt(path))+".whm")

	actions := doc.Actions()
	timelines := make(map[string]whe.FxTimeline)
	for _, tl := range doc.Timelines() {
		timelines[strings.ToLower(strings.TrimSpace(tl.Name))] = tl
	}
	fmt.Printf("    действий: %d, таймлайнов: %d\n", len(actions), len(doc.Timelines()))
	for _, a := range actions {
		fmt.Printf("  действие %q\n", a.Name)
		for _, l := range a.Motions {
			fmt.Printf("      %-22s %s\n", l.Guard(), resolveMotion(timelines, clips, l.Name, 0))
		}
		for _, l := range a.Subactions {
			fmt.Printf("      %-22s → действие %q\n", l.Guard(), l.Name)
		}
	}
}

// resolveMotion описывает один мотион: какие клипы он ведёт и в какие
// мотионы цепляется дальше. Цепочки разрываются по повторам и по глубине.
func resolveMotion(timelines map[string]whe.FxTimeline, clips map[string]bool, name string, depth int) string {
	if depth > 4 {
		return name + " (…)"
	}
	tl, ok := timelines[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return fmt.Sprintf("%s  [нет таймлайна; клип в модели: %s]", name, hasClip(clips, name))
	}
	var parts []string
	for _, an := range tl.Animations {
		parts = append(parts, "клип "+an+" ("+hasClip(clips, an)+")")
	}
	for _, mo := range tl.Motions {
		if strings.EqualFold(strings.TrimSpace(mo), strings.TrimSpace(name)) {
			continue
		}
		parts = append(parts, "→ "+resolveMotion(timelines, clips, mo, depth+1))
	}
	if len(parts) == 0 {
		return name + "  [без клипов и связей]"
	}
	return name + ": " + strings.Join(parts, "; ")
}

// hasClip — есть ли клип с таким именем среди имён модели (без учёта регистра).
func hasClip(clips map[string]bool, name string) string {
	if len(clips) == 0 {
		return "?"
	}
	if clips[strings.ToLower(strings.TrimSpace(name))] {
		return "да"
	}
	return "НЕТ"
}

// loadClipNames имена клипов (.whm рядом с .whe); nil, когда модель не найдена.
func loadClipNames(tree *modtree.ModTree, whmPath string) map[string]bool {
	f := tree.FindFile(whmPath)
	if f == nil {
		return nil
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return nil
	}
	m, err := whm.Parse(data)
	if err != nil || m == nil {
		return nil
	}
	out := make(map[string]bool, len(m.Animations))
	for i := range m.Animations {
		out[strings.ToLower(m.Animations[i].Name)] = true
	}
	return out
}

// filepathExt — расширение с точкой из пути виртуального файла.
func filepathExt(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i:]
	}
	return ""
}
