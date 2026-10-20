// Command sgbdump is a format probe: it prints the chunk tree of a Dawn of
// War .sgb scenario file with payload sizes, byte-map and dimension hints,
// and the printable strings each payload carries. It exists to reverse
// engineer the scenario format from real files.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"

	"dow-mod-studio/internal/chunky"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sgbdump <file.sgb>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		fmt.Fprintln(os.Stderr, "not a Relic Chunky file")
		os.Exit(1)
	}
	chunks, used := chunky.Parse(data[chunky.PreambleSize:], 0)
	fmt.Printf("file: %d bytes, %d chunks cover %d\n", len(data), len(chunks), used)
	dump(chunks, 0)
}

func dump(chunks []*chunky.Chunk, depth int) {
	for _, c := range chunks {
		indent := strings.Repeat("  ", depth)
		fmt.Printf("%s%s v%d name=%q payload=%d children=%d\n", indent, c.TypeID, c.Version, c.Name, len(c.Payload), len(c.Children))
		if len(c.Children) > 0 {
			dump(c.Children, depth+1)
			continue
		}
		dumpPayload(indent, c.Payload)
	}
}

func dumpPayload(indent string, p []byte) {
	if len(p) == 0 {
		return
	}
	head := len(p)
	if head > 64 {
		head = 64
	}
	fmt.Printf("%s  head: % x\n", indent, p[:head])
	// A u32 pair of plausible dimensions: byte maps satisfy 8+w*h == len,
	// grids of records satisfy (len-8) % (w*h) == 0.
	if len(p) >= 8 {
		w := int(binary.LittleEndian.Uint32(p[0:4]))
		h := int(binary.LittleEndian.Uint32(p[4:8]))
		if w > 0 && h > 0 && w <= 8192 && h <= 8192 {
			rest := len(p) - 8
			switch {
			case rest == w*h:
				lo, hi := p[8], p[8]
				for _, b := range p[8:] {
					if b < lo {
						lo = b
					}
					if b > hi {
						hi = b
					}
				}
				fmt.Printf("%s  bytemap %dx%d min=%d max=%d\n", indent, w, h, lo, hi)
			case w*h > 0 && rest%(w*h) == 0:
				stride := rest / (w * h)
				fmt.Printf("%s  grid %dx%d, %d bytes per cell\n", indent, w, h, stride)
				if stride == 4 {
					lo, hi := float32(math.Inf(1)), float32(math.Inf(-1))
					cells := w * h
					for i := 0; i < cells; i++ {
						off := 8 + i*4
						v := math.Float32frombits(binary.LittleEndian.Uint32(p[off : off+4]))
						if v < lo {
							lo = v
						}
						if v > hi {
							hi = v
						}
					}
					fmt.Printf("%s  float range [%v .. %v]\n", indent, lo, hi)
				}
			}
		}
	}
	if runs := stringsRuns(p); len(runs) > 0 {
		if len(runs) > 16 {
			runs = runs[:16]
		}
		fmt.Printf("%s  strings: %q\n", indent, runs)
	}
}

// stringsRuns collects printable ASCII and UTF-16LE runs of four characters or
// more, which is where file references and names hide.
func stringsRuns(p []byte) []string {
	var out []string
	for i := 0; i+4 <= len(p); {
		if isPrintASCII(p[i]) {
			j := i
			for j < len(p) && isPrintASCII(p[j]) {
				j++
			}
			if j-i >= 4 {
				run := string(p[i:j])
				if len(run) > 64 {
					run = run[:64] + fmt.Sprintf("…(%d)", j-i)
				}
				out = append(out, run)
			}
			i = j + 1
			continue
		}
		if i+10 <= len(p) && p[i+1] == 0 && isPrintASCII(p[i]) && isPrintASCII(p[i+2]) && p[i+3] == 0 {
			j := i
			var s strings.Builder
			for j+1 < len(p) && isPrintASCII(p[j]) && p[j+1] == 0 {
				s.WriteByte(p[j])
				j += 2
			}
			if j-i >= 8 {
				out = append(out, s.String()+" (utf16)")
			}
			i = j + 2
			continue
		}
		i++
	}
	return out
}

func isPrintASCII(b byte) bool { return b >= 0x20 && b < 0x7f }
