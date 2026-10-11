package whm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"dow-mod-studio/internal/chunky"
)

// PatchAnimBoneKeys rewrites the bone keys of one animation inside a .whm
// and returns the whole file with them written. Key values, key times and key
// counts all follow the editor: a key added on the strip lands in the file, a
// key deleted from it leaves, and a dragged key is stored on the frame it was
// dropped on. Everything else — names, the clip's length, the mesh and texture
// tracks and the animated camera block — keeps the bytes it came with: only
// the animation's DATADATA payload is rebuilt, only its bone section within
// that, and the size fields of the chunks holding the payload are moved by
// the change in length, so the rest of the file is byte for byte what it was.
//
// animIndex indexes model.Animations, which is the order the FOLDANIM chunks'
// DATADATA children are read in — the same walk ParseWhmModel takes, so the
// index the panel shows is the index the file is found by. The clip's frame
// count and the set of bone names have to be the editor's and the file's
// alike before anything is written: they are what says the animation on show
// belongs to the file on disk, and a save that would write one clip over
// another is refused rather than guessed at.
//
// The keys in memory are held in renderer coordinates, where decodeBoneVec3
// and decodeBoneQuat put them and where the pose panel writes; the file stores
// the sign-flipped form, so every value is flipped back on the way out.
func PatchAnimBoneKeys(data []byte, model *Model, animIndex int) ([]byte, error) {
	if model == nil || animIndex < 0 || animIndex >= len(model.Animations) {
		return nil, fmt.Errorf("animation %d of %d does not exist", animIndex, len(model.Animations))
	}
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil, fmt.Errorf("not a Relic Chunky model file")
	}
	loc, err := whmAnimDataSplice(data, animIndex)
	if err != nil {
		return nil, err
	}
	payload, err := rebuildAnimBoneKeyPayload(data[loc.start:loc.end], &model.Animations[animIndex])
	if err != nil {
		return nil, fmt.Errorf("animation %q: %w", model.Animations[animIndex].Name, err)
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

// animDataLocate is where one animation's DATADATA payload sits in a .whm.
type animDataLocate struct {
	// start and end are the payload's byte range within the file.
	start, end int
	// sizeAts are the file offsets of the size field of the DATADATA chunk
	// itself and of each FOLD folder holding it — the fields that have to
	// move when the payload changes length.
	sizeAts []int
}

// whmAnimDataSplice finds the DATADATA payload of animation animIndex by the
// walk this function has always taken to it — the root FOLDRSGM's FOLDANIM
// children, each one's DATADATA children, counted in order — but keeping the
// file offsets rather than only the payloads: a splice needs to know where the
// bytes are, and a tree of slices throws that away.
func whmAnimDataSplice(data []byte, animIndex int) (animDataLocate, error) {
	var loc animDataLocate
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
	n := 0
	for _, fold := range chunky.SpansIn(data, spans[root].BodyStart, spans[root].BodyEnd) {
		if fold.TypeID != "FOLDANIM" {
			continue
		}
		for _, sub := range chunky.SpansIn(data, fold.BodyStart, fold.BodyEnd) {
			if sub.TypeID != "DATADATA" {
				continue
			}
			if n == animIndex {
				loc = animDataLocate{
					start:   sub.BodyStart,
					end:     sub.BodyEnd,
					sizeAts: []int{sub.HeaderAt, fold.HeaderAt, spans[root].HeaderAt},
				}
				return loc, nil
			}
			n++
		}
	}
	return loc, fmt.Errorf("animation %d not found in the file", animIndex)
}

// rebuildAnimBoneKeyPayload re-emits the bone section of one DATADATA payload
// from the editor's animation and keeps the rest of the payload as it was.
// The walk mirrors parseAnimationData, so the clip's header, the mesh and
// texture tracks and the camera block after the bones go back out as the same
// bytes they came in as, while the keys themselves are whatever the editor now
// holds — counts, times and values alike. Built up sequentially like this
// there are no offsets to compute: a key added is a key appended, and the
// payload ends up as long as the keys make it.
//
// A payload whose bones or frame count disagree with the editor is an error
// rather than a write: that disagreement means the animation on show was not
// read from the file being written.
func rebuildAnimBoneKeyPayload(payload []byte, anim *Animation) ([]byte, error) {
	if anim == nil {
		return nil, fmt.Errorf("no animation to write")
	}
	if len(payload) < 12 {
		return nil, fmt.Errorf("data chunk is %d bytes, too short for a header", len(payload))
	}
	fileFrames := int(int32(binary.LittleEndian.Uint32(payload[:4])))
	if fileFrames != anim.NumFrames {
		return nil, fmt.Errorf("the editor's clip has %d frames and the file's has %d — refusing to write one clip over another",
			anim.NumFrames, fileFrames)
	}
	r := &chunky.Reader{Data: payload}
	var out bytes.Buffer
	// The frame count and the duration are the file's own: adding a key does
	// not make the clip longer.
	out.Write(payload[:8])
	r.Skip(8)
	boneCount := r.Count(4)
	if r.Err != nil {
		return nil, r.Err
	}
	if boneCount != len(anim.Bones) {
		return nil, fmt.Errorf("the file has %d bone tracks and the editor has %d — refusing to write one skeleton over another",
			boneCount, len(anim.Bones))
	}
	out.Write(payload[8:12])
	var scratch [4]byte
	putCount := func(n int) {
		binary.LittleEndian.PutUint32(scratch[:], uint32(int32(n)))
		out.Write(scratch[:])
	}
	putFloat := func(v float32) {
		binary.LittleEndian.PutUint32(scratch[:], math.Float32bits(v))
		out.Write(scratch[:])
	}
	for i := 0; i < boneCount; i++ {
		nameRaw := r.RawStr()
		if r.Err != nil {
			return nil, r.Err
		}
		nameLen := int(binary.LittleEndian.Uint32(nameRaw))
		name := string(nameRaw[4 : 4+nameLen])
		// Reading the file's section ahead of writing the editor's: the
		// walk has to reach the stale flag either way, and it is what
		// says where this bone ends and the next begins.
		posCount := r.Count(16)
		r.Skip(posCount * 16)
		rotCount := r.Count(20)
		r.Skip(rotCount * 20)
		staleAt := r.Off
		r.Skip(1)
		if r.Err != nil {
			return nil, r.Err
		}
		track := -1
		for j := range anim.Bones {
			if anim.Bones[j].Name == name {
				track = j
				break
			}
		}
		if track < 0 {
			return nil, fmt.Errorf("bone %q is in the file but not in the editor's clip", name)
		}
		// The name goes back out as it arrived: it is length prefixed
		// bytes in the file, and re-encoding it could change its length.
		out.Write(nameRaw)
		// Position keys: the count, then each key as its time and its
		// value in the file's sign convention.
		putCount(len(anim.Bones[track].PosKey))
		for _, key := range anim.Bones[track].PosKey {
			putFloat(key.Time)
			v := encodeBoneVec3(key.V)
			for c := range v {
				putFloat(v[c])
			}
		}
		// Rotation keys, the same way round.
		putCount(len(anim.Bones[track].RotKey))
		for _, key := range anim.Bones[track].RotKey {
			putFloat(key.Time)
			q := encodeBoneQuat(key.V)
			for c := range q {
				putFloat(q[c])
			}
		}
		// The stale flag is not something this editor edits, so the byte
		// the file holds goes back out with it.
		out.WriteByte(payload[staleAt])
	}
	// Everything after the bone tracks — the mesh and texture tracks and the
	// animated camera block — is not read and not rewritten: the bytes from
	// here to the end of the payload go back out untouched.
	out.Write(payload[r.Off:])
	return out.Bytes(), nil
}

// encodeBoneVec3 is decodeBoneVec3 read backwards: the renderer's x is the
// stored negation and the other two axes pass through.
func encodeBoneVec3(v [3]float32) [3]float32 {
	return [3]float32{-v[0], v[1], v[2]}
}

// encodeBoneQuat is decodeBoneQuat read backwards: the stored vector part is
// the renderer's with y and z negated, and w passes through.
func encodeBoneQuat(q [4]float32) [4]float32 {
	return [4]float32{q[0], -q[1], -q[2], q[3]}
}

// putF32 writes one little-endian float32. The offset comes from walking the
// same buffer with a checked reader, so it is inside the payload by
// construction; the guard keeps a future caller from panicking over it.
func putF32(b []byte, off int, v float32) {
	if off < 0 || off+4 > len(b) {
		return
	}
	binary.LittleEndian.PutUint32(b[off:], math.Float32bits(v))
}
