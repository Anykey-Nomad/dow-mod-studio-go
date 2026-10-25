package main

import (
	"fmt"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	modules, _ := modapi.DiscoverModules(dowRoot)
	target := modapi.FindModule(modules, "DoWDE")
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Println("build:", err)
		return
	}
	defer tree.Close()
	var walk func(d *modtree.TreeFolder, depth int)
	walk = func(d *modtree.TreeFolder, depth int) {
		if depth > 4 {
			return
		}
		for _, sub := range d.Children {
			p := strings.ReplaceAll(sub.FullPath, "\\", "/")
			if strings.Contains(strings.ToLower(p), "script") {
				fmt.Println("FOLDER", p)
			}
			walk(sub, depth+1)
		}
	}
	walk(tree.Root, 0)
}
