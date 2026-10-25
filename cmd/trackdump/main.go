// Command trackdump prints every mesh track of one clip of a model: the mesh
// name, the force flag the clip holds it at and the curve of the track.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	clip := flag.String("clip", "", "имя клипа")
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
	a := m.Animation(*clip)
	if a == nil {
		fmt.Fprintln(os.Stderr, "клип не найден:", *clip)
		os.Exit(1)
	}
	fmt.Printf("клип %s: кадров %d, длительность %.3f, треков %d\n", a.Name, a.NumFrames, a.Duration, len(a.Meshes))
	for _, tr := range a.Meshes {
		keys := ""
		for _, k := range tr.Keys {
			keys += fmt.Sprintf(" %.3f=%.3f", k.Time, k.V[0])
		}
		fmt.Printf("    %-44s force=%.3f%s\n", tr.Name, tr.Force, keys)
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
