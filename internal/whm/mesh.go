package whm

import (
	"encoding/binary"
	"strings"
)

// meshMaterialSection reports whether a material list starts at off, returning
// the offset just past the list (and 0 when off is not one). reserved selects
// the .whm record layout, which carries four extra bytes after each material's
// vertex range.
//
// The loose form is used to confirm the two nominal offsets, where the offset
// is already known from the vertex count. The strict form is the predicate for
// scanning, where it has to stand on its own against raw float data: it demands
// a long name built only from characters texture references actually use. The
// shadow volume behind the list is never validated - it is discarded, and a few
// shipped meshes put something else in its place.
func MeshMaterialSection(data []byte, off int, reserved, strict bool) int {
	if off < 0 || off+8 > len(data) {
		return 0
	}
	materials := int(int32(binary.LittleEndian.Uint32(data[off:])))
	if materials < 1 || materials > 64 {
		return 0
	}
	off += 4
	for i := 0; i < materials; i++ {
		if off+4 > len(data) {
			return 0
		}
		nameLen := int(int32(binary.LittleEndian.Uint32(data[off:])))
		off += 4
		if nameLen < 1 || nameLen > 512 || off+nameLen > len(data) {
			return 0
		}
		if strict && !looksLikeTextureRef(data[off:off+nameLen]) {
			return 0
		}
		for _, c := range data[off : off+nameLen] {
			if c < 0x20 || c >= 0x7f {
				return 0
			}
		}
		off += nameLen
		if off+4 > len(data) {
			return 0
		}
		indices := int(int32(binary.LittleEndian.Uint32(data[off:])))
		off += 4
		tail := 4
		if reserved {
			tail += 4
		}
		// A material with no faces is legal and stores a zero index count.
		if indices < 0 || indices > 0 && indices%3 != 0 || off+indices*2+tail > len(data) {
			return 0
		}
		off += indices*2 + tail // indices, min vertex + count, optional reserved
	}
	return off
}

// looksLikeTextureRef reports whether name could be a texture reference. Real
// names are either a path ("art/ebps/races/<race>/texture_share/<name>") or a
// generated one ("merged material-[a,b]"), and are always long.
func looksLikeTextureRef(name []byte) bool {
	if len(name) < 16 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("_./- [],:()", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// FOLDANIM wraps a single DATADATA record. In .sgm the record becomes a
// FOLDDATA folder holding DATAINFO plus one chunk per animated bone, mesh or
