package main

import (
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	arg := os.Args[1]
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
	if strings.Contains(arg, "/") || strings.Contains(arg, "\\") {
		if folder := tree.FindFolder(arg); folder != nil && len(folder.Children) > 0 {
			fmt.Printf("папка %q: подпапки %d\n", arg, len(folder.Children))
			for _, child := range folder.Children {
				fmt.Printf("  [dir] %s\n", child.FullPath)
			}
		}
		files := modapi.FilesInFolder(tree, arg)
		fmt.Printf("папка %q: %d файлов\n", arg, len(files))
		for _, f := range files {
			fmt.Printf("  %-50s %s\n", f.Name, f.FullPath)
		}
		return
	}
	hits := tree.Search(arg, modtree.SearchAll, 200)
	fmt.Printf("поиск %q: %d совпадений\n", arg, len(hits))
	for _, h := range hits {
		fmt.Printf("  %-60s %s\n", h.File.Name, h.File.FullPath)
	}
}
