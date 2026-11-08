// Command rtxpng сохраняет текстуру .rtx в PNG: для безсжатых форматов (raw
// 32-битные пиксели) он пишет оба варианта порядка каналов, чтобы по картинке
// выбрать правильный — сравнение с тем же скином в DXT5 показывает, где
// перепутаны красный и синий.
//
//	go run ./cmd/rtxpng "Data/art/ui/textures/taskbar/necron/map_separated.rtx" out.png
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: rtxpng <virtual path> <out.png>")
		os.Exit(1)
	}
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

	data, err := tree.ReadFile(os.Args[1])
	if err != nil {
		fatal("read: %v", err)
	}

	img, _, err := raster.DecodeImageData(data, "rtx")
	if err == nil {
		write(os.Args[2], img)
		fmt.Printf("%s -> %s (%s)\n", os.Args[1], os.Args[2], bounds(img))
		return
	}
	fmt.Printf("декодер отказал: %v; пробуем raw-порядки каналов\n", err)

	w, h, pixels, err := rawAttrs(data)
	if err != nil {
		fatal("%v", err)
	}
	for _, order := range []string{"rgba", "bgra"} {
		img := rawImage(pixels, w, h, order)
		out := os.Args[2] + "." + order + ".png"
		write(out, img)
		fmt.Printf("raw %s -> %s (%s)\n", order, out, bounds(img))
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

func rawImage(p []byte, w, h int, order string) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		s := p[i*4:]
		if order == "bgra" {
			img.Pix[i*4+0] = s[2]
			img.Pix[i*4+1] = s[1]
			img.Pix[i*4+2] = s[0]
		} else {
			img.Pix[i*4+0] = s[0]
			img.Pix[i*4+1] = s[1]
			img.Pix[i*4+2] = s[2]
		}
		img.Pix[i*4+3] = s[3]
	}
	return img
}

func bounds(img image.Image) string {
	b := img.Bounds()
	return fmt.Sprintf("%dx%d", b.Dx(), b.Dy())
}

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		fatal("%v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fatal("%v", err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
