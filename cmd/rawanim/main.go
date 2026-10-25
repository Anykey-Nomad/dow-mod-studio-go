// Command rawanim dumps the raw bytes of an animation clip's track section,
// so the layout of the mesh visibility tracks can be read off the file
// instead of guessed from the parser.
//
//	go run ./cmd/rawanim -file "C:/.../Necron_Monolith.whm" -clip vis_main
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"dow-mod-studio/internal/chunky"
)

func main() {
	file := ""
	clip := ""
	for i := 1; i+1 < len(os.Args); i += 2 {
		switch os.Args[i] {
		case "-file":
			file = os.Args[i+1]
		case "-clip":
			clip = os.Args[i+1]
		}
	}
	if file == "" || clip == "" {
		fmt.Println("нужны -file и -clip")
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Println(err)
		return
	}
	chunks, _ := chunky.Parse(data[chunky.PreambleSize:], 0)
	for _, c := range chunks {
		if c.TypeID != "FOLDRSGM" {
			continue
		}
		for _, anim := range c.Children {
			if anim.TypeID != "FOLDANIM" || anim.Name != clip {
				continue
			}
			for _, sub := range anim.Children {
				if sub.TypeID != "DATADATA" {
					continue
				}
				dump(sub.Payload)
			}
		}
	}
}

// dump walks the clip payload the way the parser does and hex-dumps the
// bytes of every mesh track.
func dump(payload []byte) {
	r := &chunky.Reader{Data: payload}
	fmt.Printf("кадров %d, длительность %.3f\n", r.I32(), f32(r.Take(4)))
	bones := r.Count(4)
	fmt.Printf("костей %d\n", bones)
	for i := 0; i < bones && r.Err == nil; i++ {
		r.Str()
		if n := r.Count(16); n > 0 {
			r.Take(n * 16)
		}
		if n := r.Count(20); n > 0 {
			r.Take(n * 20)
		}
		r.I8()
	}
	if r.Err != nil {
		fmt.Println("ошибка:", r.Err)
		return
	}
	fmt.Printf("треки начинаются с %d\n", r.Off)
	count := r.Count(4)
	fmt.Printf("треков %d\n", count)
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		mode := r.I32()
		switch mode {
		case 2:
			start := r.Off
			r.Skip(8)
			n := r.Count(4)
			r.Skip(4)
			force := f32(r.Take(4))
			keys := r.Take(maxInt(n-1, 0) * 8)
			fmt.Printf("  меш %-28s режим 2: сырые байты % x\n", name, payload[start:r.Off])
			fmt.Printf("      счётчик %d, удерживается %.3f, ключей %d\n", n, force, maxInt(n-1, 0))
			for j := 0; j+8 <= len(keys); j += 8 {
				fmt.Printf("      ключ %d: t=%.3f v=%.3f\n", j/8,
					f32(keys[j:j+4]), f32(keys[j+4:j+8]))
			}
		case 0:
			start := r.Off
			r.Skip(4)
			r.I32()
			n := r.Count(4)
			r.Take(n * 8)
			fmt.Printf("  текстура %-25s сырые байты % x\n", name, payload[start:r.Off])
		default:
			fmt.Printf("  %s: неизвестный режим %d\n", name, mode)
			return
		}
	}
	if r.Err != nil {
		fmt.Println("ошибка:", r.Err)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func f32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}
