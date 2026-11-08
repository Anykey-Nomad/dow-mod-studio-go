// Command rawcheck сравнивает безсжатый .rtx с его же .tga-близнецом:
// в архивах лежат одинаковые картинки в двух форматах, поэтому по разнице
// пикселей видно, в каком порядке каналов лежат raw-пиксели .rtx.
//
//	go run ./cmd/rawcheck "Data/art/ui/textures/taskbar/necron/btn_multipage_back_up"
package main

import (
	"fmt"
	"image"
	"os"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: rawcheck <путь без расширения>")
		os.Exit(1)
	}
	stem := os.Args[1]
	modules, err := modapi.DiscoverModules(dowRoot)
	if err != nil {
		fatal("discover: %v", err)
	}
	mod := modapi.FindModule(modules, "DoWDE")
	if mod == nil {
		fatal("модуль не найден")
	}
	tree, err := modapi.BuildModTree(mod.Path, "English")
	if err != nil {
		fatal("tree: %v", err)
	}
	defer tree.Close()

	etalonData, err := tree.ReadFile(stem + ".tga")
	if err != nil {
		fatal("эталон .tga: %v", err)
	}
	etalon, _, err := raster.DecodeImageData(etalonData, "tga")
	if err != nil {
		fatal("эталон .tga не декодируется: %v", err)
	}
	raw, err := tree.ReadFile(stem + ".rtx")
	if err != nil {
		fatal(".rtx: %v", err)
	}
	w, h, pixels, err := rawAttrs(raw)
	if err != nil {
		fatal("%v", err)
	}
	fmt.Printf("%s: %dx%d, эталон %s\n", stem, w, h, size(etalon))
	for _, order := range []string{"rgba", "bgra", "argb", "abgr"} {
		img := rawImage(pixels, w, h, order)
		diff, n := meanDiff(img, etalon)
		fmt.Printf("  %-4s средняя разница с .tga: %.2f (n=%d)\n", order, diff, n)
	}
	bgra := rawImage(pixels, w, h, "bgra")
	for i := 0; i < 4 && i < w*h; i++ {
		x, y := i%w, i/w
		r1, g1, b1, a1 := bgra.At(x, y).RGBA()
		r2, g2, b2, a2 := etalon.At(etalon.Bounds().Min.X+x, etalon.Bounds().Min.Y+y).RGBA()
		fmt.Printf("  px(%d,%d) rtx-bgra=(%d,%d,%d,%d) tga=(%d,%d,%d,%d)\n",
			x, y, r1>>8, g1>>8, b1>>8, a1>>8, r2>>8, g2>>8, b2>>8, a2>>8)
	}
}

func rawAttrs(data []byte) (int, int, []byte, error) {
	chunks, _ := chunky.Parse(data[min(chunky.PreambleSize, len(data)):], 0)
	var attr, pixels []byte
	var walk func(cs []*chunky.Chunk)
	walk = func(cs []*chunky.Chunk) {
		for _, c := range cs {
			if c.TypeID == "FOLDIMAG" {
				if a := c.Child("DATAATTR"); a != nil {
					if d := c.Child("DATADATA"); d != nil {
						attr, pixels = a.Payload, d.Payload
						return
					}
				}
			}
			walk(c.Children)
			if attr != nil {
				return
			}
		}
	}
	walk(chunks)
	if len(attr) < 16 {
		return 0, 0, nil, fmt.Errorf("нет DATAATTR")
	}
	w := int(uint32(attr[4]) | uint32(attr[5])<<8 | uint32(attr[6])<<16 | uint32(attr[7])<<24)
	h := int(uint32(attr[8]) | uint32(attr[9])<<8 | uint32(attr[10])<<16 | uint32(attr[11])<<24)
	if w <= 0 || h <= 0 || len(pixels) < w*h*4 {
		return 0, 0, nil, fmt.Errorf("raw не подходит: %dx%d, payload %d", w, h, len(pixels))
	}
	return w, h, pixels[:w*h*4], nil
}

func rawImage(p []byte, w, h int, order string) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		s := p[i*4:]
		var r, g, b, a uint8
		switch order {
		case "rgba":
			r, g, b, a = s[0], s[1], s[2], s[3]
		case "bgra":
			b, g, r, a = s[0], s[1], s[2], s[3]
		case "argb":
			a, r, g, b = s[0], s[1], s[2], s[3]
		case "abgr":
			a, b, g, r = s[0], s[1], s[2], s[3]
		}
		img.Pix[i*4+0] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = a
	}
	return img
}

func meanDiff(a, b image.Image) (float64, int) {
	ab, bb := a.Bounds(), b.Bounds()
	w, h := ab.Dx(), ab.Dy()
	if bb.Dx() < w {
		w = bb.Dx()
	}
	if bb.Dy() < h {
		h = bb.Dy()
	}
	var sum float64
	n := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ar, ag, abt, aa := a.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			br, bg, bb2, ba := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			sum += float64(absDiff(ar, br)) +
				float64(absDiff(ag, bg)) +
				float64(absDiff(abt, bb2)) +
				float64(absDiff(aa, ba))
			n += 4
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

func absDiff(a, b uint32) int {
	a >>= 8
	b >>= 8
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func size(img image.Image) string {
	b := img.Bounds()
	return fmt.Sprintf("%dx%d", b.Dx(), b.Dy())
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
