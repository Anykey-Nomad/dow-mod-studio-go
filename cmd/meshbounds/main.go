// Command meshbounds prints the bounding box of every mesh of a model: it
// shows whether an addon part is added next to the building body or replaces
// a part of it.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/whm"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: meshbounds <model path> [mesh name filter]")
		os.Exit(1)
	}
	filter := ""
	if len(os.Args) > 2 {
		filter = os.Args[2]
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

	f := tree.FindFile(os.Args[1])
	if f == nil {
		f = tree.FindByName(filepath.Base(os.Args[1]))
	}
	if f == nil {
		fmt.Fprintln(os.Stderr, "модель не найдена")
		os.Exit(1)
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m, err := whm.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	seen := map[string]bool{}
	for _, mesh := range m.Meshes {
		if seen[mesh.Name] {
			continue
		}
		seen[mesh.Name] = true
		if filter != "" && !strings.Contains(strings.ToLower(mesh.Name), strings.ToLower(filter)) {
			continue
		}
		min := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
		max := [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
		count := 0
		for _, idx := range mesh.Indices {
			if int(idx) >= len(m.Vertices) {
				continue
			}
			v := m.Vertices[idx]
			count++
			for a := 0; a < 3; a++ {
				if v.Pos[a] < min[a] {
					min[a] = v.Pos[a]
				}
				if v.Pos[a] > max[a] {
					max[a] = v.Pos[a]
				}
			}
		}
		if count == 0 {
			continue
		}
		fmt.Printf("%-32s x[%7.2f %7.2f] y[%7.2f %7.2f] z[%7.2f %7.2f]  вершин %d\n",
			mesh.Name, min[0], max[0], min[1], max[1], min[2], max[2], count)
	}
}
