// Command luasearch greps the scripts of a module for a string and prints the
// matching lines with the file they came from. It is how the order in which
// the game plays the visibility clips of a building can be found.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	root := flag.String("root", dowRoot, "путь к установленной Dawn of War")
	module := flag.String("module", "DoWDE", "модуль")
	folder := flag.String("folder", `Data/attrib/script`, "папка со скриптами")
	needle := flag.String("find", "", "что ищем")
	ext := flag.String("ext", ".lua", "расширение файлов")
	limit := flag.Int("limit", 40, "сколько совпадений показывать")
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
	shown := 0
	for _, f := range files {
		if !strings.EqualFold(filepath.Ext(f.Name), *ext) {
			continue
		}
		data, err := tree.ReadTreeFile(f)
		if err != nil {
			continue
		}
		text := string(data)
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			if !strings.Contains(strings.ToLower(line), strings.ToLower(*needle)) {
				continue
			}
			if shown >= *limit {
				fmt.Println("... больше не показано")
				return
			}
			fmt.Printf("%s:%d: %s\n", f.FullPath, i+1, strings.TrimSpace(line))
			shown++
		}
	}
	if shown == 0 {
		fmt.Println("совпадений нет")
	}
}

func collect(dir *modtree.TreeFolder, out *[]*modtree.TreeFile) {
	*out = append(*out, dir.Files...)
	for _, c := range dir.Children {
		collect(c, out)
	}
}
