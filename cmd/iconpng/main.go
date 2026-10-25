// Command iconpng decodes an image out of the module tree (a building icon,
// for example) and writes it as a PNG, so the icons can be looked at: they are
// renders of the building as the game shows it.
//
//	go run ./cmd/iconpng "Data/art/ui/ingame/guard_icons/guard_hq.tga" guard_hq.png
package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: iconpng <virtual path> [out.png] ...")
		os.Exit(1)
	}
	modules, err := modapi.DiscoverModules(dowRoot)
	if err != nil {
		fmt.Println("discover:", err)
		os.Exit(1)
	}
	target := modapi.FindModule(modules, "DoWDE")
	tree, err := modapi.BuildModTree(target.Path, "English")
	if err != nil {
		fmt.Println("build:", err)
		os.Exit(1)
	}
	defer tree.Close()

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		path := args[i]
		out := ""
		if i+1 < len(args) && !strings.HasSuffix(strings.ToLower(args[i+1]), ".tga") &&
			!strings.HasSuffix(strings.ToLower(args[i+1]), ".rtx") {
			out = args[i+1]
			i++
		} else {
			out = filepath.Base(path) + ".png"
		}
		if err := convert(tree, path, out); err != nil {
			fmt.Println(path, "—", err)
			continue
		}
		fmt.Println(out)
	}
}

// convert читает картинку из дерева и пишет её в PNG.
func convert(tree *modtree.ModTree, path, out string) error {
	f := tree.FindFile(path)
	if f == nil {
		return fmt.Errorf("файл не найден")
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return err
	}
	img, _, err := raster.DecodeImageData(data, strings.TrimPrefix(filepath.Ext(f.Name), "."))
	if err != nil {
		return err
	}
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	defer w.Close()
	return png.Encode(w, img)
}
