package main

import (
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	path := os.Args[1]
	max := 99
	if len(os.Args) > 2 && os.Args[2] == "1" {
		max = 1
	}
	modules, err := modapi.DiscoverModules(dowRoot)
	if err != nil {
		fmt.Println("discover:", err)
		return
	}
	target := modapi.FindModule(modules, "DoWDE")
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Println("build:", err)
		return
	}
	defer tree.Close()
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
	var walk func(node *rgd.RgdEntry, depth int)
	walk = func(node *rgd.RgdEntry, depth int) {
		if depth > max {
			return
		}
		for _, ch := range node.Children {
			if ch.IsTable() {
				fmt.Printf("%s%s [table] %d детей %s\n", strings.Repeat("  ", depth), ch.Name, len(ch.Children), ch.RefString())
				walk(ch, depth+1)
			} else {
				fmt.Printf("%s%s = %q\n", strings.Repeat("  ", depth), ch.Name, ch.DisplayValue())
			}
		}
	}
	walk(rf.Root, 0)
}
