package whm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"dow-mod-studio/internal/chunky"
)

// PatchMeshMaterialPaths rewrites the material reference strings of one named
// mesh inside a .whm and returns the whole file with them written.
//
// newPaths[i] replaces the i-th material reference of that mesh; an empty
// entry leaves that material as it is, so a caller can change one of three
// references without re-stating the other two. Only the mesh's material
// section is rebuilt — the vertex block before it, each material's face block
// and the shadow volume after the list keep the bytes they came with — and the
// size fields of the chunks holding the payload move by the change in length,
// so the rest of the file is byte for byte what it was.
//
// The mesh is the FOLDMSLC of that name under the model's FOLDMSGR, and the
// material order is the order parseMeshData reads them, which is the order the
// mesh list shows. Models ship one FOLDMSLC per mesh name; the first match is
// the one written. The reference is the string the game reads straight out of
// the mesh, so a full texture path written here is what both the engine and
// the viewer will resolve — this is the field that decides whether a mesh
// draws its map or the magenta placeholder.
//
// A reference that would not parse back is refused rather than written: the
// material section demands printable ASCII, and a path with a byte outside
// that would leave a file the reader rejects on the next open.
func PatchMeshMaterialPaths(data []byte, meshName string, newPaths []string) ([]byte, error) {
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil, fmt.Errorf("not a Relic Chunky model file")
	}
	for i, p := range newPaths {
		if p == "" {
			continue
		}
		if err := checkMaterialRef(p); err != nil {
			return nil, fmt.Errorf("material %d: %w", i, err)
		}
	}
	loc, err := whmMeshDataSplice(data, meshName)
	if err != nil {
		return nil, err
	}
	payload, err := rebuildMeshMaterialPayload(data[loc.start:loc.end], newPaths)
	if err != nil {
		return nil, fmt.Errorf("mesh %q: %w", meshName, err)
	}
	delta := len(payload) - (loc.end - loc.start)
	out := make([]byte, 0, len(data)+delta)
	out = append(out, data[:loc.start]...)
	out = append(out, payload...)
	out = append(out, data[loc.end:]...)
	// The payload changed length, so the size field of the chunk it sits in
	// and of every folder above it move by the same amount. Each recorded
	// offset lies before the payload, so they all still name their header in
	// the grown or shrunken file.
	for _, at := range loc.sizeAts {
		if at+chunky.HeaderSize > loc.start {
			return nil, fmt.Errorf("internal: a chunk header at %d overlaps the payload it holds", at)
		}
		size := int32(binary.LittleEndian.Uint32(out[at+12 : at+16]))
		binary.LittleEndian.PutUint32(out[at+12:at+16], uint32(size+int32(delta)))
	}
	return out, nil
}

// checkMaterialRef rejects a texture reference that the material section would
// not read back: every byte has to be printable ASCII, the same rule
// MeshMaterialSection holds a stored reference to.
func checkMaterialRef(ref string) error {
	if ref == "" {
		return nil
	}
	for i := 0; i < len(ref); i++ {
		if ref[i] < 0x20 || ref[i] >= 0x7f {
			return fmt.Errorf("reference has a byte (0x%02x) the material section cannot store", ref[i])
		}
	}
	return nil
}

// meshDataLocate is where one mesh's DATADATA payload sits in a .whm.
type meshDataLocate struct {
	// start and end are the payload's byte range within the file.
	start, end int
	// sizeAts are the file offsets of the size field of the DATADATA chunk
	// itself and of each FOLD folder holding it — the fields that have to
	// move when the payload changes length.
	sizeAts []int
}

// whmMeshDataSplice finds the DATADATA payload of the first FOLDMSLC named
// meshName, keeping the file offsets rather than only the payloads: a splice
// needs to know where the bytes are. The walk is the one Parse takes — the
// root FOLDRSGM's FOLDMSGR children, each one's FOLDMSLC children matched by
// their chunk name, and the DATADATA inside — but over spans.
func whmMeshDataSplice(data []byte, meshName string) (meshDataLocate, error) {
	var loc meshDataLocate
	spans := chunky.SpansIn(data, chunky.PreambleSize, len(data))
	root := -1
	for i := range spans {
		if spans[i].TypeID == "FOLDRSGM" {
			root = i
			break
		}
	}
	if root < 0 {
		return loc, fmt.Errorf("FOLDRSGM chunk not found")
	}
	for _, fold := range chunky.SpansIn(data, spans[root].BodyStart, spans[root].BodyEnd) {
		if fold.TypeID != "FOLDMSGR" {
			continue
		}
		for _, sub := range chunky.SpansIn(data, fold.BodyStart, fold.BodyEnd) {
			if sub.TypeID != "FOLDMSLC" {
				continue
			}
			if !strings.EqualFold(chunkSpanName(data, sub), meshName) {
				continue
			}
			for _, d := range chunky.SpansIn(data, sub.BodyStart, sub.BodyEnd) {
				if d.TypeID != "DATADATA" {
					continue
				}
				loc = meshDataLocate{
					start:   d.BodyStart,
					end:     d.BodyEnd,
					sizeAts: []int{d.HeaderAt, sub.HeaderAt, fold.HeaderAt, spans[root].HeaderAt},
				}
				return loc, nil
			}
		}
	}
	return loc, fmt.Errorf("mesh %q not found in the file", meshName)
}

// chunkSpanName reads a span's chunk instance name straight from the file: it
// sits between the fixed header and the body, which is what SpansIn does not
// keep. That name is the mesh's own name for a FOLDMSLC.
func chunkSpanName(data []byte, s chunky.Span) string {
	if s.BodyStart <= s.HeaderAt+chunky.HeaderSize {
		return ""
	}
	return strings.TrimRight(string(data[s.HeaderAt+chunky.HeaderSize:s.BodyStart]), "\x00")
}

// locateMeshMaterialStart finds where a mesh DATADATA payload's material list
// begins. It walks the vertex header exactly as parseMeshData does — the same
// skin bone list, the same stride and the same two nominal offsets with a scan
// behind them — because the section sits after the vertex block and its start
// cannot be found without it.
func locateMeshMaterialStart(data []byte) (int, error) {
	r := &chunky.Reader{Data: data}
	r.Take(13) // reserved, flag, triangle count, reserved
	skinBones := r.Count(4)
	for i := 0; i < skinBones && r.Err == nil; i++ {
		r.Str()   // skin bone name
		r.Take(4) // local bone index
	}
	verts := r.Count(4)
	sizeID := r.I32()
	if r.Err != nil {
		return 0, r.Err
	}
	skinned := sizeID > 37
	stride := 32
	if skinned {
		stride += 16
	}
	vertexStart := r.Off
	// The material list follows the vertex block, past an optional reserved
	// dword — the two nominal offsets first, then a scan for a stray layout.
	if MeshMaterialSection(data, vertexStart+verts*stride+4, true, false) > 0 {
		return vertexStart + verts*stride + 4, nil
	}
	if MeshMaterialSection(data, vertexStart+verts*stride, true, false) > 0 {
		return vertexStart + verts*stride, nil
	}
	for off := vertexStart + 4; off+8 < len(data); off += 2 {
		if MeshMaterialSection(data, off, true, true) > 0 {
			return off, nil
		}
	}
	return 0, fmt.Errorf("cannot locate the material section after %d vertices (size id %d)", verts, sizeID)
}

// rebuildMeshMaterialPayload re-emits a mesh DATADATA payload with newPaths
// applied to the material references it names, keeping the vertex block before
// the section, every material's face block and the shadow volume after it as
// the bytes they were. Built sequentially there are no offsets to compute: only
// the reference strings can change length, and everything else is copied.
func rebuildMeshMaterialPayload(payload []byte, newPaths []string) ([]byte, error) {
	materialStart, err := locateMeshMaterialStart(payload)
	if err != nil {
		return nil, err
	}
	if materialStart+4 > len(payload) {
		return nil, fmt.Errorf("material count does not fit at offset %d", materialStart)
	}
	// MeshMaterialSection both finds where the list ends and rejects a payload
	// whose material section does not parse, so the walk below is known to
	// agree with the reader before a byte is rewritten.
	sectionEnd := MeshMaterialSection(payload, materialStart, true, false)
	if sectionEnd <= 0 || sectionEnd > len(payload) {
		return nil, fmt.Errorf("material section does not parse at offset %d", materialStart)
	}
	count := int(int32(binary.LittleEndian.Uint32(payload[materialStart:])))
	if len(newPaths) > count {
		return nil, fmt.Errorf("the mesh has %d materials but %d paths were given", count, len(newPaths))
	}
	// Walk the list, recording each material's reference bytes and the block
	// after it, so only the reference has to be rebuilt.
	r := &chunky.Reader{Data: payload}
	r.Off = materialStart + 4
	type matSpan struct {
		pathAt, pathEnd, matEnd int
	}
	spans := make([]matSpan, 0, count)
	for i := 0; i < count; i++ {
		pathAt := r.Off
		r.Str()
		if r.Err != nil {
			return nil, r.Err
		}
		pathEnd := r.Off
		indices := r.I32()
		if r.Err != nil {
			return nil, r.Err
		}
		if indices < 0 || r.Off+int(indices)*2+8 > len(payload) {
			return nil, fmt.Errorf("material %d: face block of %d indices does not fit", i, indices)
		}
		r.Skip(int(indices) * 2)
		r.Skip(4) // min vertex + vertex count
		r.Skip(4) // reserved, .whm only
		if r.Err != nil {
			return nil, r.Err
		}
		spans = append(spans, matSpan{pathAt: pathAt, pathEnd: pathEnd, matEnd: r.Off})
	}
	if r.Off != sectionEnd {
		return nil, fmt.Errorf("internal: the material walk ended at %d, the section at %d", r.Off, sectionEnd)
	}
	var out bytes.Buffer
	out.Write(payload[:materialStart])                  // vertex block
	out.Write(payload[materialStart : materialStart+4]) // material count, unchanged
	for i, sp := range spans {
		if i < len(newPaths) && newPaths[i] != "" {
			p := []byte(newPaths[i])
			out.Write(chunky.I32(int32(len(p))))
			out.Write(p)
		} else {
			out.Write(payload[sp.pathAt:sp.pathEnd]) // the reference as it was
		}
		out.Write(payload[sp.pathEnd:sp.matEnd]) // face block, verbatim
	}
	out.Write(payload[sectionEnd:]) // shadow volume, verbatim
	return out.Bytes(), nil
}
