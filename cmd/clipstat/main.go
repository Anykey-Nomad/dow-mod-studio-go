// Command clipstat prints the visibility of every clip of a model.
//
// Two readings of the data are printed side by side:
//
//	по флагу  — a track with the force flag set shows its mesh, a track
//	            without it says nothing. This is how the "vis_*" state clips
//	            of a building split its meshes into groups.
//	по кривой — a mesh is shown unless the clip's curve hides it.
//
// The state of a building without addons is the union of the groups its
// state clips show, minus the groups the addon clips show.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whm"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	root := flag.String("root", dowRoot, "путь к установленной Dawn of War")
	module := flag.String("module", "DoWDE", "модуль")
	model := flag.String("model", "", "путь к модели .whm")
	names := flag.Bool("names", false, "печатать имена видимых мешей")
	only := flag.String("only", "", "показывать только клипы, в имени которых есть подстрока")
	flag.Parse()

	modules, err := modapi.DiscoverModules(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
		os.Exit(1)
	}
	target := modapi.FindModule(modules, *module)
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	defer tree.Close()

	m := loadModel(tree, *model)
	if m == nil {
		fmt.Fprintln(os.Stderr, "модель не найдена:", *model)
		os.Exit(1)
	}
	all := m.MeshNames()
	fmt.Printf("модель %s: мешей %d (имен %d), клипов %d\n",
		filepath.Base(m.Name), len(m.Meshes), len(all), len(m.Animations))

	type row struct {
		name   string
		tracks int
		curved int
		flag   int // показано по флагу
		curve  int // показано по кривой
	}
	var rows []row
	for i := range m.Animations {
		a := &m.Animations[i]
		r := row{name: a.Name, tracks: len(a.Meshes)}
		for _, tr := range a.Meshes {
			if len(tr.Keys) > 0 {
				r.curved++
			}
			if tr.Force >= 0.5 {
				r.flag++
			}
			if a.MeshAlpha(tr.Name, 1) >= 0.5 {
				r.curve++
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, r := range rows {
		if *only != "" && !strings.Contains(strings.ToLower(r.name), strings.ToLower(*only)) {
			continue
		}
		fmt.Printf("  %-28s треков %-4d кривых %-4d по флагу %-4d по кривой %-4d\n",
			r.name, r.tracks, r.curved, r.flag, r.curve)
		if *names {
			a := m.Animation(r.name)
			var vis []string
			for _, tr := range a.Meshes {
				if tr.Force >= 0.5 {
					vis = append(vis, tr.Name)
				}
			}
			sort.Strings(vis)
			fmt.Printf("        %s\n", strings.Join(vis, ", "))
		}
	}
}

func loadModel(tree *modtree.ModTree, path string) *whm.Model {
	p := strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	if !strings.HasPrefix(strings.ToLower(p), "data/") {
		p = "Data/art/ebps/" + strings.TrimPrefix(p, "/")
	}
	if !strings.HasSuffix(strings.ToLower(p), ".whm") {
		p += ".whm"
	}
	f := tree.FindFile(p)
	if f == nil {
		f = tree.FindByName(filepath.Base(p))
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
