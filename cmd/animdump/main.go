// Command animdump prints the raw bytes of one animation clip of a model, so
// the layout of the mesh visibility tracks can be checked against the data.
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 4 {
		fmt.Println("usage: animdump <model path> <clip name> [bytes]")
		os.Exit(1)
	}
	modelPath, clipName := os.Args[1], os.Args[2]
	limit := 512
	if len(os.Args) > 3 {
		fmt.Sscanf(os.Args[3], "%d", &limit)
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

	data := read(tree, modelPath)
	if data == nil {
		fmt.Fprintln(os.Stderr, "модель не найдена:", modelPath)
		os.Exit(1)
	}
	chunks, _ := chunky.Parse(data[chunky.PreambleSize:], 0)
	for _, c := range chunks {
		if c.TypeID != "FOLDRSGM" {
			continue
		}
		for _, anim := range c.Children {
			if anim.TypeID != "FOLDANIM" || !strings.EqualFold(anim.Name, clipName) {
				continue
			}
			for _, sub := range anim.Children {
				if sub.TypeID != "DATADATA" {
					continue
				}
				dump(sub.Payload, limit)
			}
		}
	}
}

// read читает файл модели из дерева модуля.
func read(tree *modtree.ModTree, path string) []byte {
	f := tree.FindFile(path)
	if f == nil {
		f = tree.FindByName(filepath.Base(path))
	}
	if f == nil {
		return nil
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return nil
	}
	return data
}

// dump печатает содержимое клипа: заголовок, треки костей и первые треки
// видимости — с сырыми байтами каждого.
func dump(payload []byte, limit int) {
	r := &chunky.Reader{Data: payload}
	frames := int(r.I32())
	duration := f32(r.Take(4))
	fmt.Printf("кадров %d, длительность %.3f\n", frames, duration)

	boneCount := r.Count(4)
	fmt.Printf("треков костей: %d\n", boneCount)
	for i := 0; i < boneCount && r.Err == nil; i++ {
		name := r.Str()
		pos := r.Count(16)
		r.Take(pos * 16)
		rot := r.Count(20)
		r.Take(rot * 20)
		r.I8()
		_ = name
	}
	trackCount := r.Count(4)
	fmt.Printf("треков видимости: %d\n", trackCount)
	for i := 0; i < trackCount && r.Err == nil; i++ {
		start := r.Off
		name := r.Str()
		mode := r.I32()
		if mode != 2 {
			fmt.Printf("трек %d %q: режим %d (не видимость)\n", i, name, mode)
			break
		}
		skip := r.Take(8)
		count := int(binary.LittleEndian.Uint32(r.Take(4)))
		skip2 := r.Take(4)
		force := f32(r.Take(4))
		fmt.Printf("трек %d %q: count=%d force=%.3f skip=% x skip2=% x\n",
			i, name, count, force, skip, skip2)
		if i >= 4 {
			break
		}
		// Ключи кривой: по count-1 штук, по два float32.
		for j := 0; j < count-1; j++ {
			t := f32(r.Take(4))
			v := f32(r.Take(4))
			fmt.Printf("    ключ %.3f = %.3f\n", t, v)
		}
		fmt.Printf("    байты: % x\n", payload[start:min(r.Off, start+64)])
	}
	if len(payload) > limit {
		fmt.Printf("--- первые %d байт ---\n", limit)
		for off := 0; off < limit && off < len(payload); off += 16 {
			end := off + 16
			if end > limit {
				end = limit
			}
			if end > len(payload) {
				end = len(payload)
			}
			fmt.Printf("%06x  % x\n", off, payload[off:end])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// f32 читает float32 из четырёх байт.
func f32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}
