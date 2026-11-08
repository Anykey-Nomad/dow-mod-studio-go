// Command rtxattr печатает DATAATTR текстуры .rtx в шестнадцатеричном виде
// и первые байты DATADATA: нужно, чтобы понять, в каком порядке в атрибутах
// лежат формат, ширина и высота, когда декодер отказывается их читать.
//
//	go run ./cmd/rtxattr "Data/art/ui/textures/taskbar/necron/map_separated.rtx"
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: rtxattr <virtual path>")
		os.Exit(1)
	}
	modules, err := modapi.DiscoverModules(dowRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "discover:", err)
		os.Exit(1)
	}
	mod := modapi.FindModule(modules, "DoWDE")
	if mod == nil {
		fmt.Fprintln(os.Stderr, "модуль не найден")
		os.Exit(1)
	}
	tree, err := modapi.BuildModTree(mod.Path, "English")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tree:", err)
		os.Exit(1)
	}
	defer tree.Close()

	data, err := tree.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: %d байт\n", os.Args[1], len(data))

	chunks, _ := chunky.Parse(data[min(chunky.PreambleSize, len(data)):], 0)
	var walk func(cs []*chunky.Chunk, depth int)
	walk = func(cs []*chunky.Chunk, depth int) {
		for _, c := range cs {
			switch c.TypeID {
			case "DATAATTR", "DATAHEAD":
				fmt.Printf("%s%s v%d payload=%d: % x\n",
					strings.Repeat("  ", depth), c.TypeID, c.Version, len(c.Payload), c.Payload)
				if c.TypeID == "DATAATTR" && len(c.Payload) >= 16 {
					fmt.Printf("  как u32: format=%d w=%d h=%d mips=%d\n",
						binary.LittleEndian.Uint32(c.Payload[0:4]),
						binary.LittleEndian.Uint32(c.Payload[4:8]),
						binary.LittleEndian.Uint32(c.Payload[8:12]),
						binary.LittleEndian.Uint32(c.Payload[12:16]))
					fmt.Printf("  как u16: %d %d %d %d %d %d %d %d\n",
						binary.LittleEndian.Uint16(c.Payload[0:2]),
						binary.LittleEndian.Uint16(c.Payload[2:4]),
						binary.LittleEndian.Uint16(c.Payload[4:6]),
						binary.LittleEndian.Uint16(c.Payload[6:8]),
						binary.LittleEndian.Uint16(c.Payload[8:10]),
						binary.LittleEndian.Uint16(c.Payload[10:12]),
						binary.LittleEndian.Uint16(c.Payload[12:14]),
						binary.LittleEndian.Uint16(c.Payload[14:16]))
				}
			case "DATADATA":
				fmt.Printf("%s%s v%d payload=%d; первые 32 байта: % x\n",
					strings.Repeat("  ", depth), c.TypeID, c.Version, len(c.Payload),
					c.Payload[:min(32, len(c.Payload))])
			default:
				fmt.Printf("%s%s v%d %d байт\n", strings.Repeat("  ", depth), c.TypeID, c.Version, len(c.Payload))
			}
			walk(c.Children, depth+1)
		}
	}
	walk(chunks, 0)
}
