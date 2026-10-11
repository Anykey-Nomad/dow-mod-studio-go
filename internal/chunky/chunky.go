// Package chunky reads and writes the "Relic Chunky" container that Relic's
// file formats share: .whm, .sgm, .whe, .rsh and the FX files all wrap their
// payloads in the same tree of FOLD*/DATA* chunks. It knows the container and
// nothing about what any particular format keeps inside a chunk.
package chunky

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// Magic is the file signature every chunky file starts with.
var Magic = []byte("Relic Chunky")

const (
	// PreambleSize is "Relic Chunky" plus the three int32 header fields
	// (1706509, 1, 1).
	PreambleSize = 24
	HeaderSize   = 20
	MaxDepth     = 16
)

// Chunk is one node of the chunky tree. FOLD* chunks hold children, DATA*
// chunks hold an opaque payload.
type Chunk struct {
	TypeID   string
	Version  int32
	Name     string
	Payload  []byte
	Children []*Chunk
}

func (c *Chunk) IsFolder() bool { return strings.HasPrefix(c.TypeID, "FOLD") }

func (c *Chunk) Child(typeID string) *Chunk {
	for _, ch := range c.Children {
		if ch.TypeID == typeID {
			return ch
		}
	}
	return nil
}

// --- little endian cursor ---------------------------------------------------

type Reader struct {
	Data []byte
	Off  int
	Err  error
}

func (r *Reader) Fail(format string, args ...interface{}) {
	if r.Err == nil {
		r.Err = fmt.Errorf(format, args...)
	}
}

func (r *Reader) Take(n int) []byte {
	if r.Err != nil {
		return nil
	}
	if n < 0 || n > len(r.Data)-r.Off {
		r.Fail("truncated chunk payload: want %d bytes at offset %d of %d", n, r.Off, len(r.Data))
		return nil
	}
	b := r.Data[r.Off : r.Off+n]
	r.Off += n
	return b
}

func (r *Reader) Skip(n int) { r.Take(n) }

func (r *Reader) I8() int8 {
	b := r.Take(1)
	if b == nil {
		return 0
	}
	return int8(b[0])
}

func (r *Reader) F32() float32 {
	b := r.Take(4)
	if b == nil {
		return 0
	}
	return readF32(b)
}

func (r *Reader) I32() int32 {
	b := r.Take(4)
	if b == nil {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b))
}

// Count reads a count and rejects values that cannot fit in what is left, so a
// corrupt file fails fast instead of allocating gigabytes.
func (r *Reader) Count(stride int) int {
	n := int(r.I32())
	if r.Err != nil {
		return 0
	}
	if n < 0 || (stride > 0 && n > (len(r.Data)-r.Off)/stride) {
		r.Fail("implausible element count %d at offset %d", n, r.Off)
		return 0
	}
	return n
}

// Str reads a length prefixed UTF-8 string.
func (r *Reader) Str() string {
	n := int(r.I32())
	if r.Err != nil {
		return ""
	}
	if n < 0 {
		r.Fail("negative string length %d", n)
		return ""
	}
	return string(r.Take(n))
}

// RawStr returns the length prefixed string exactly as stored, so it can be
// copied into another payload without a decode/encode round trip.
func (r *Reader) RawStr() []byte {
	start := r.Off
	n := int(r.I32())
	if r.Err != nil {
		return nil
	}
	if n < 0 {
		r.Fail("negative string length %d", n)
		return nil
	}
	r.Take(n)
	if r.Err != nil {
		return nil
	}
	return r.Data[start:r.Off]
}

// readF32 decodes one little endian float32, the same guard the model parser
// keeps: a short slice reads as zero rather than panicking.
func readF32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

// --- chunk tree -------------------------------------------------------------

func AllZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// Parse reads a flat sequence of chunks. Folders recurse into their body.
// Trailing padding or unknown trailing bytes are kept on the chunk instead of
// failing the whole file. The second result is how many bytes of data the
// returned chunks cover, which is the rest of a folder body.
func Parse(data []byte, depth int) ([]*Chunk, int) {
	var out []*Chunk
	off := 0
	for off+HeaderSize <= len(data) {
		if AllZero(data[off:]) {
			break
		}
		typeID := string(data[off : off+8])
		version := int32(binary.LittleEndian.Uint32(data[off+8 : off+12]))
		size := int(int32(binary.LittleEndian.Uint32(data[off+12 : off+16])))
		nameLen := int(int32(binary.LittleEndian.Uint32(data[off+16 : off+20])))
		off += HeaderSize
		if size < 0 || nameLen < 0 || off+nameLen+size > len(data) {
			break
		}
		name := strings.TrimRight(string(data[off:off+nameLen]), "\x00")
		off += nameLen
		body := data[off : off+size]
		off += size
		c := &Chunk{TypeID: typeID, Version: version, Name: name}
		if strings.HasPrefix(typeID, "FOLD") && depth < MaxDepth {
			children, used := Parse(body, depth+1)
			c.Children = children
			if used > len(body) {
				used = len(body)
			}
			c.Payload = body[used:]
		} else {
			c.Payload = body
		}
		out = append(out, c)
	}
	return out, off
}

func Serialize(c *Chunk) []byte {
	var body bytes.Buffer
	for _, ch := range c.Children {
		body.Write(Serialize(ch))
	}
	body.Write(c.Payload)
	name := []byte(c.Name)
	if len(name) > 0 && name[len(name)-1] != 0 {
		name = append(name, 0)
	}
	var out bytes.Buffer
	var hdr [HeaderSize]byte
	copy(hdr[0:8], c.TypeID)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(c.Version))
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(body.Len()))
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(len(name)))
	out.Write(hdr[:])
	out.Write(name)
	out.Write(body.Bytes())
	return out.Bytes()
}

func Clone(c *Chunk) *Chunk {
	if c == nil {
		return nil
	}
	clone := &Chunk{TypeID: c.TypeID, Version: c.Version, Name: c.Name, Payload: c.Payload}
	for _, ch := range c.Children {
		clone.Children = append(clone.Children, Clone(ch))
	}
	return clone
}

// --- spans: chunks as they sit in a file ------------------------------------

// Span is one chunk as it sits in the file: where its header is, and where the
// body after the name runs from and to.
type Span struct {
	TypeID    string
	HeaderAt  int
	BodyStart int
	BodyEnd   int
}

// SpansIn reads a flat sequence of chunks out of data[from:to] the way Parse
// does — the same header layout, the same refusal to read past the end, the
// same stop at zero padding — but keeps the file offsets, which is what a
// splice works with and what parsed payloads leave behind.
func SpansIn(data []byte, from, to int) []Span {
	if to > len(data) {
		to = len(data)
	}
	var out []Span
	off := from
	for off+HeaderSize <= to {
		if AllZero(data[off:to]) {
			break
		}
		typeID := string(data[off : off+8])
		size := int(int32(binary.LittleEndian.Uint32(data[off+12 : off+16])))
		nameLen := int(int32(binary.LittleEndian.Uint32(data[off+16 : off+20])))
		headerAt := off
		off += HeaderSize
		if size < 0 || nameLen < 0 || off+nameLen+size > to {
			break
		}
		bodyStart := off + nameLen
		bodyEnd := bodyStart + size
		out = append(out, Span{
			TypeID:    typeID,
			HeaderAt:  headerAt,
			BodyStart: bodyStart,
			BodyEnd:   bodyEnd,
		})
		off = bodyEnd
	}
	return out
}

// --- preamble and payload builders -------------------------------------------

// Preamble returns the fixed file header every chunky file starts with.
func Preamble() []byte {
	out := make([]byte, 0, PreambleSize)
	out = append(out, Magic...)
	for _, v := range []int32{1706509, 1, 1} {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(v))
		out = append(out, b[:]...)
	}
	return out
}

// I32 encodes one little endian int32.
func I32(v int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return b
}

// F32 encodes one little endian float32.
func F32(v float32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, math.Float32bits(v))
	return b
}
