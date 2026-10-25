// Command chunkydump prints the chunk tree of a file from the module tree:
// which Relic Chunky sections it holds and how big each is. A file that is
// not a chunky stream is printed as printable strings instead, which is how
// a .whe next to a model can be looked at.
//
//	go run ./cmd/chunkydump "Data/art/ebps/races/imperial_guard/structures/guard_hq.whm"
package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/modapi"
)

const dowRoot = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition`

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: chunkydump <virtual path>")
		os.Exit(1)
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

	f := tree.FindFile(os.Args[1])
	if f == nil {
		fmt.Fprintln(os.Stderr, "файл не найден:", os.Args[1])
		os.Exit(1)
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "читать не удалось:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: %d байт\n", f.FullPath, len(data))
	if len(data) < chunky.PreambleSize || string(data[:len(chunky.Magic)]) != string(chunky.Magic) {
		printStrings(data)
		return
	}
	chunks, _ := chunky.Parse(data[chunky.PreambleSize:], 0)
	var walk func(c *chunky.Chunk, depth int)
	walk = func(c *chunky.Chunk, depth int) {
		pad := strings.Repeat("  ", depth)
		name := c.Name
		if len(name) > 40 {
			name = name[:40] + "…"
		}
		fmt.Printf("%s%-12s v%d  %8d байт  %q\n", pad, c.TypeID, c.Version, subtree(c), name)
		for _, ch := range c.Children {
			walk(ch, depth+1)
		}
	}
	for _, c := range chunks {
		walk(c, 0)
	}
}

// subtree — размер чанка со всеми детьми.
func subtree(c *chunky.Chunk) int {
	n := len(c.Payload)
	for _, ch := range c.Children {
		n += subtree(ch)
	}
	return n
}

// printStrings печатает печатные последовательности файла — для файлов,
// которые не являются потоком Relic Chunky.
func printStrings(data []byte) {
	var b strings.Builder
	flush := func() {
		if b.Len() >= 4 {
			fmt.Println("   ", b.String())
		}
		b.Reset()
	}
	for _, c := range data {
		if unicode.IsPrint(rune(c)) && c < 0x80 {
			b.WriteByte(c)
			continue
		}
		flush()
	}
	flush()
}
