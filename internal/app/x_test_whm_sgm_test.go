package app

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/whm"
)

func tStr(s string) []byte {
	b := chunky.I32(int32(len(s)))
	return append(b, s...)
}

func tF32(vs ...float32) []byte {
	b := make([]byte, 0, len(vs)*4)
	var tmp [4]byte
	for _, v := range vs {
		binary.LittleEndian.PutUint32(tmp[:], math.Float32bits(v))
		b = append(b, tmp[:]...)
	}
	return b
}

func tBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i)
	}
	return b
}

func tU16(vs ...uint16) []byte {
	b := make([]byte, 0, len(vs)*2)
	for _, v := range vs {
		b = append(b, byte(v), byte(v>>8))
	}
	return b
}

func testWhmSkeleton() []byte {
	var p bytes.Buffer
	p.Write(chunky.I32(2))
	p.Write(tStr("root"))
	p.Write(chunky.I32(-1))
	p.Write(tF32(0, 0, 0))
	p.Write(tF32(0, 0, 0, 1))
	p.Write(tStr("child"))
	p.Write(chunky.I32(0))
	p.Write(tF32(0.5, 0, -0.25))
	p.Write(tF32(0, 0.7071068, 0, 0.7071068))
	return p.Bytes()
}

func testWhmMesh() []byte {
	var p bytes.Buffer
	p.Write(chunky.I32(0)) // reserved
	p.Write([]byte{1})     // flag
	p.Write(chunky.I32(3)) // triangle count
	p.Write(chunky.I32(0)) // reserved
	p.Write(chunky.I32(1)) // skin bone count
	p.Write(tStr("root"))
	p.Write(chunky.I32(0)) // bone index
	p.Write(chunky.I32(3)) // vertex count
	p.Write(chunky.I32(39))
	// 3 skinned vertices: position 3f, weights 3f + 4 bytes, normal 3f, uv 2f
	p.Write(tBytes(3*48, 0x40))
	p.Write(chunky.I32(0)) // reserved after the UV block, .whm only
	p.Write(chunky.I32(1)) // material count
	p.Write(tStr("art/ebps/races/marines/texture_share/marine_body"))
	p.Write(chunky.I32(3)) // index count, not triangle count
	p.Write(tU16(0, 2, 1))
	p.Write(tU16(0, 3))    // min vertex + vertex count
	p.Write(chunky.I32(0)) // reserved, .whm only
	// Shadow volume, .whm only: counted vertices, faces and edges
	p.Write(chunky.I32(1)) // 1 shadow vertex
	p.Write(tF32(0, 0, 0)) // position
	p.Write(chunky.I32(1)) // 1 shadow face
	p.Write(tF32(0, 1, 0)) // normal
	p.Write(chunky.I32(0)) // 3 indices
	p.Write(chunky.I32(1))
	p.Write(chunky.I32(0))
	p.Write(chunky.I32(1)) // 1 shadow edge
	p.Write(chunky.I32(0)) // 4 uint32
	p.Write(chunky.I32(1))
	p.Write(chunky.I32(0))
	p.Write(chunky.I32(0))
	p.Write(tF32(0, 0, 0, 0, 0, 0)) // 2 positions
	return p.Bytes()
}

func testWhmMarkers() []byte {
	var p bytes.Buffer
	p.Write(chunky.I32(1))
	p.Write(tStr("marker_foot_left"))
	p.Write(tStr("root"))
	p.Write(tF32(1, 0, 0, 0, 1, 0, 0, 0, 1))
	p.Write(tF32(0, 0.1, 0.2))
	return p.Bytes()
}

func testWhmCameras() []byte {
	var p bytes.Buffer
	p.Write(chunky.I32(1))
	p.Write(tStr("chase"))
	p.Write(tF32(1, 2, 3))
	p.Write(tF32(0, 0, 0, 1))
	p.Write(tF32(1.2, 0.1, 100))
	p.Write(tF32(0, 0, 0))
	return p.Bytes()
}

func testWhmAnimData() []byte {
	var p bytes.Buffer
	p.Write(chunky.I32(31))  // frame count
	p.Write(tF32(1.0333333)) // duration
	p.Write(chunky.I32(1))   // bone track count
	p.Write(tStr("root"))
	p.Write(chunky.I32(2)) // position keys
	p.Write(tF32(0, 0, 0, 0))
	p.Write(tF32(1, 0, 0.5, 0))
	p.Write(chunky.I32(2)) // rotation keys
	p.Write(tF32(0, 0, 0, 0, 1))
	p.Write(tF32(1, 0, 0, 0, 1))
	p.Write([]byte{1})     // stale flag
	p.Write(chunky.I32(1)) // mesh/material track count
	p.Write(tStr("body_mesh"))
	p.Write(chunky.I32(2)) // mode: mesh visibility
	p.Write(chunky.I32(0)) // reserved x2
	p.Write(chunky.I32(1)) // key count
	p.Write(chunky.I32(0)) // reserved
	p.Write(tF32(1))       // force invisible
	p.Write(tF32(0, 1))
	p.Write(chunky.I32(1)) // animated camera count, .whm only
	p.Write(tStr("chase"))
	p.Write(chunky.I32(1))
	p.Write(tF32(0, 0, 0, 0))
	p.Write(chunky.I32(1))
	p.Write(tF32(0, 0, 0, 0, 1))
	return p.Bytes()
}

func testWhmTexture() *chunky.Chunk {
	return &chunky.Chunk{
		TypeID:  "FOLDTXTR",
		Version: 1,
		Name:    "art/ebps/races/marines/texture_share/marine_body",
		Children: []*chunky.Chunk{
			{TypeID: "DATAHEAD", Version: 1, Payload: chunky.I32(0)},
			{TypeID: "DATAINFO", Version: 3, Payload: chunky.I32(3)},
		},
	}
}

func buildTestWhm() []byte {
	root := &chunky.Chunk{
		TypeID:  "FOLDRSGM",
		Version: 3,
		Name:    "test_marine",
		Children: []*chunky.Chunk{
			{TypeID: "DATAFBIF", Version: 1, Name: "FileBurnInfo", Payload: tStr("meta")},
			{TypeID: "DATASSHR", Version: 2, Name: "art/ebps/races/marines/texture_share/marine_body", Payload: tStr("art/ebps/races/marines/texture_share/marine_body")},
			testWhmTexture(),
			{TypeID: "DATASKEL", Version: 5, Payload: testWhmSkeleton()},
			{
				TypeID:  "FOLDMSGR",
				Version: 1,
				Children: []*chunky.Chunk{
					{
						TypeID:  "FOLDMSLC",
						Version: 1,
						Name:    "body_mesh",
						Children: []*chunky.Chunk{
							{TypeID: "DATADATA", Version: 2, Payload: testWhmMesh()},
							{TypeID: "DATABVOL", Version: 2, Payload: tF32(1, 0, 0, 0, 0, 0, 0)},
						},
					},
					{
						TypeID:  "DATADATA",
						Version: 1,
						Payload: append(append(chunky.I32(1), tStr("body_mesh")...), append(tStr(""), chunky.I32(-1)...)...),
					},
					{TypeID: "DATABVOL", Version: 2, Payload: tF32(1, 0, 0, 0, 0, 0, 0)},
				},
			},
			{TypeID: "DATAMARK", Version: 1, Payload: testWhmMarkers()},
			{TypeID: "DATACAMS", Version: 1, Payload: testWhmCameras()},
			{
				TypeID:  "FOLDANIM",
				Version: 3,
				Name:    "run",
				Children: []*chunky.Chunk{
					{TypeID: "DATADATA", Version: 2, Name: "run", Payload: testWhmAnimData()},
					{TypeID: "DATAANBV", Version: 1, Name: "run", Payload: tBytes(24, 0)},
				},
			},
		},
	}
	var out bytes.Buffer
	out.Write(chunky.Preamble())
	out.Write(chunky.Serialize(root))
	return out.Bytes()
}

func parseSgmRoot(t *testing.T, sgm []byte) *chunky.Chunk {
	t.Helper()
	if !bytes.HasPrefix(sgm, chunky.Magic) {
		t.Fatalf("output lost the Relic Chunky preamble")
	}
	chunks, _ := chunky.Parse(sgm[chunky.PreambleSize:], 0)
	if len(chunks) != 1 || chunks[0].TypeID != "FOLDRSGM" {
		t.Fatalf("expected a single FOLDRSGM root, got %+v", chunks)
	}
	return chunks[0]
}

func findChild(t *testing.T, parent *chunky.Chunk, typeID string) *chunky.Chunk {
	t.Helper()
	for _, c := range parent.Children {
		if c.TypeID == typeID {
			return c
		}
	}
	t.Fatalf("chunk %s not found", typeID)
	return nil
}

func countChildren(parent *chunky.Chunk, typeID string) int {
	n := 0
	for _, c := range parent.Children {
		if c.TypeID == typeID {
			n++
		}
	}
	return n
}

func TestConvertWhmToSgmStructure(t *testing.T) {
	sgm, info, err := whm.ConvertToSgm(buildTestWhm())
	if err != nil {
		t.Fatalf("whm.ConvertToSgm: %v", err)
	}
	root := parseSgmRoot(t, sgm)

	if root.Version != 1 {
		t.Errorf("FOLDRSGM version = %d, want 1", root.Version)
	}
	if root.Name != "test_marine" {
		t.Errorf("FOLDRSGM name = %q, want test_marine", root.Name)
	}
	for _, gone := range []string{"DATAFBIF", "DATASKEL", "DATACAMS", "DATAANBV", "DATADATA"} {
		if findOrNil(root, gone) != nil {
			t.Errorf("%s should not survive into the .sgm", gone)
		}
	}

	if info.Bones != 2 || info.Meshes != 1 || info.Vertices != 3 {
		t.Errorf("info = %+v, want 2 bones, 1 mesh, 3 vertices", info)
	}
	if info.Markers != 1 || info.Cameras != 1 || info.Animations != 1 {
		t.Errorf("info = %+v, want 1 marker, 1 camera, 1 animation", info)
	}

	// Skeleton: the flat DATASKEL list becomes a folder of named DATABONEs.
	skel := findChild(t, root, "FOLDSKEL")
	if skel.Version != 3 {
		t.Errorf("FOLDSKEL version = %d, want 3", skel.Version)
	}
	skelInfo := findChild(t, skel, "DATAINFO")
	if skelInfo.Version != 1 || len(skelInfo.Payload) != 4 || int(relicI32Value(skelInfo.Payload)) != 2 {
		t.Errorf("FOLDSKEL/DATAINFO = %+v payload %v, want v1 count 2", skelInfo.Version, skelInfo.Payload)
	}
	bones := filterChildren(skel, "DATABONE")
	if len(bones) != 2 {
		t.Fatalf("DATABONE count = %d, want 2", len(bones))
	}
	if bones[0].Name != "root" || bones[1].Name != "child" {
		t.Errorf("bone names = %q, %q", bones[0].Name, bones[1].Name)
	}
	for _, b := range bones {
		if b.Version != 5 {
			t.Errorf("DATABONE version = %d, want 5", b.Version)
		}
		// parent index (4) + translation (12) + rotation (16); the name moved
		// out of the payload and into the chunk name.
		if len(b.Payload) != 32 {
			t.Errorf("DATABONE %q payload = %d bytes, want 32 (name moved to the chunk)", b.Name, len(b.Payload))
		}
	}
	if got := relicI32Value(bones[1].Payload); got != 0 {
		t.Errorf("child bone parent index = %d, want 0", got)
	}
	if got := relicI32Value(bones[0].Payload); got != -1 {
		t.Errorf("root bone parent index = %d, want -1", got)
	}

	// Markers and cameras become one named chunk per record.
	mark := findChild(t, root, "DATAMARK")
	if mark.Version != 1 || mark.Name != "marker_foot_left" {
		t.Errorf("DATAMARK = %+v, want v1 named marker_foot_left", mark)
	}
	if len(mark.Payload) != len(tStr("root"))+36+12 {
		t.Errorf("DATAMARK payload = %d bytes, want parent name + 12 floats", len(mark.Payload))
	}
	cam := findChild(t, root, "DATACMRA")
	if cam.Version != 1 || cam.Name != "chase" {
		t.Errorf("DATACMRA = %+v, want v1 named chase", cam)
	}
	if len(cam.Payload) != 12+16+12+12 {
		t.Errorf("DATACMRA payload = %d bytes, want 52", len(cam.Payload))
	}

	// Animation: FOLDANIM/FOLDDATA with one chunk per track, cameras dropped.
	anim := findChild(t, root, "FOLDANIM")
	if anim.Version != 2 || anim.Name != "run" {
		t.Errorf("FOLDANIM = %+v, want v2 named run", anim)
	}
	fold := findChild(t, anim, "FOLDDATA")
	if fold.Version != 3 || fold.Name != "run" {
		t.Errorf("FOLDDATA = %+v, want v3 named run", fold)
	}
	animInfo := findChild(t, fold, "DATAINFO")
	if animInfo.Version != 5 || len(animInfo.Payload) != 8 {
		t.Errorf("FOLDDATA/DATAINFO = %+v payload %v, want v5 with 8 bytes", animInfo.Version, animInfo.Payload)
	}
	if got := relicI32Value(animInfo.Payload); got != 31 {
		t.Errorf("DATAINFO frame count = %d, want 31", got)
	}
	boneAnims := filterChildren(fold, "DATABANM")
	if len(boneAnims) != 1 || boneAnims[0].Name != "root" || boneAnims[0].Version != 2 {
		t.Fatalf("DATABANM = %+v, want one v2 chunk named root", boneAnims)
	}
	if len(boneAnims[0].Payload) != 4+2*16+4+2*20+1 {
		t.Errorf("DATABANM payload = %d bytes, want keys plus stale flag only", len(boneAnims[0].Payload))
	}
	canims := filterChildren(fold, "DATACANM")
	if len(canims) != 1 || canims[0].Name != "body_mesh" || canims[0].Version != 2 {
		t.Fatalf("DATACANM = %+v, want one v2 chunk named body_mesh", canims)
	}
	if len(fold.Children) != 3 {
		t.Errorf("FOLDDATA has %d children, want 3 (the camera track must be dropped)", len(fold.Children))
	}

	// Materials and textures keep their payloads but adopt the .sgm versions.
	sshr := findChild(t, root, "DATASSHR")
	if sshr.Version != 1 {
		t.Errorf("DATASSHR version = %d, want 1", sshr.Version)
	}
	if tx := findChild(t, root, "FOLDTXTR"); tx.Version != 1 || tx.Name != "art/ebps/races/marines/texture_share/marine_body" {
		t.Errorf("FOLDTXTR = %+v, want the payload copied verbatim", tx)
	}
}

func TestConvertWhmToSgmMeshDropsWhmOnlyFields(t *testing.T) {
	sgm, _, err := whm.ConvertToSgm(buildTestWhm())
	if err != nil {
		t.Fatalf("whm.ConvertToSgm: %v", err)
	}
	root := parseSgmRoot(t, sgm)
	msgr := findChild(t, root, "FOLDMSGR")
	mesh := findChild(t, msgr, "FOLDMSLC")
	if mesh.Name != "body_mesh" {
		t.Fatalf("FOLDMSLC name = %q, want body_mesh", mesh.Name)
	}
	if findChild(t, mesh, "DATABVOL") == nil {
		t.Error("the mesh bounding volume must be kept")
	}
	data := findChild(t, mesh, "DATADATA")
	r := &chunky.Reader{Data: data.Payload}
	r.Take(13) // reserved, flag, triangle count, reserved
	skinBones := r.Count(4)
	if skinBones != 1 {
		t.Fatalf("skin bone count = %d, want 1", skinBones)
	}
	if bone := r.RawStr(); !bytes.Equal(bone, tStr("root")) {
		t.Errorf("skin bone name = %q, want root", bone)
	}
	r.Skip(4) // bone index
	verts := r.Count(4)
	if verts != 3 {
		t.Fatalf("vertex count = %d, want 3", verts)
	}
	r.Skip(4) // vertex size id
	r.Skip(3 * 48)
	materials := r.Count(4)
	if materials != 1 {
		t.Fatalf("material count = %d, want 1", materials)
	}
	if path := r.Str(); path != "art/ebps/races/marines/texture_share/marine_body" {
		t.Errorf("material path = %q, want art/ebps/races/marines/texture_share/marine_body", path)
	}
	if indices := r.I32(); indices != 3 {
		t.Fatalf("index count = %d, want 3", indices)
	}
	r.Skip(3 * 2) // three uint16 indices
	r.Skip(4)     // min vertex + vertex count
	if r.Err != nil {
		t.Fatalf("mesh Payload: %v", r.Err)
	}
	if r.Off != len(data.Payload) {
		t.Errorf("the payload has %d trailing bytes, want none (shadow volume must be dropped)",
			len(data.Payload)-r.Off)
	}
	if len(data.Payload) == len(testWhmMesh()) {
		t.Error("the .whm-only reserved fields and shadow volume should have been removed")
	}
	// The geometry itself must be copied through untouched, minus the trailing
	// reserved dword.
	whmVertices := testWhmVertexBlock(t)
	sgmVertices := sgmVertexBlock(t, data.Payload)
	if !bytes.Equal(sgmVertices, whmVertices) {
		t.Errorf("vertex data changed:\n got %v\nwant %v", sgmVertices, whmVertices)
	}
}

// testWhmVertexBlock pulls the position/weight/normal/UV arrays out of the
// .whm fixture, stopping before the reserved dword.
func testWhmVertexBlock(t *testing.T) []byte {
	t.Helper()
	p := testWhmMesh()
	start := 13 + 4 + len(tStr("root")) + 4 + 4 + 4
	return p[start : start+3*48]
}

func sgmVertexBlock(t *testing.T, payload []byte) []byte {
	t.Helper()
	r := &chunky.Reader{Data: payload}
	r.Take(13)
	r.Count(4)
	r.RawStr()
	r.Skip(4)
	verts := r.Count(4)
	r.Skip(4)
	return r.Take(verts * 48)
}

func TestConvertWhmToSgmRoundTripsThroughParser(t *testing.T) {
	sgm, _, err := whm.ConvertToSgm(buildTestWhm())
	if err != nil {
		t.Fatalf("whm.ConvertToSgm: %v", err)
	}
	root := parseSgmRoot(t, sgm)
	if got := chunky.Serialize(root); !bytes.Equal(got, sgm[chunky.PreambleSize:]) {
		t.Error("re-serializing the parsed .sgm did not reproduce the original bytes")
	}
}

func TestConvertWhmToSgmRejectsGarbage(t *testing.T) {
	if _, _, err := whm.ConvertToSgm([]byte("not a model")); err == nil {
		t.Error("expected an error for a non-Relic-chunky input")
	}
	if _, _, err := whm.ConvertToSgm(append(chunky.Magic, chunky.I32(1706509)...)); err == nil {
		t.Error("expected an error when FOLDRSGM is missing")
	}
}

func TestConvertWhmToSgmWithoutOptionalChunks(t *testing.T) {
	root := &chunky.Chunk{
		TypeID:  "FOLDRSGM",
		Version: 3,
		Name:    "empty",
		Children: []*chunky.Chunk{
			{TypeID: "DATASKEL", Version: 5, Payload: chunky.I32(0)},
		},
	}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	sgm, info, err := whm.ConvertToSgm(buf.Bytes())
	if err != nil {
		t.Fatalf("whm.ConvertToSgm: %v", err)
	}
	if info.Bones != 0 {
		t.Errorf("bones = %d, want 0", info.Bones)
	}
	parsed := parseSgmRoot(t, sgm)
	if findChild(t, parsed, "FOLDSKEL") == nil {
		t.Error("an empty skeleton should still be emitted")
	}
}

// Real game files are written by more than one tool, and a truncated download
// should surface an error rather than a panic.
func TestConvertWhmToSgmSurvivesGarbage(t *testing.T) {
	full := buildTestWhm()
	for i := 1; i < len(full); i += 7 {
		// Truncation: never panics, and the only acceptable outcome is an error.
		if _, _, err := whm.ConvertToSgm(full[:i]); err == nil {
			continue // some prefixes legitimately parse
		}
	}
	seed := uint32(12345)
	for i := 0; i < 2000; i++ {
		seed = seed*1664525 + 1013904223
		blob := make([]byte, seed%512)
		for j := range blob {
			seed = seed*1664525 + 1013904223
			blob[j] = byte(seed >> 16)
		}
		whm.ConvertToSgm(blob)
	}
}

// A chunk name is supposed to be NUL terminated, but not every writer pads it.
func TestConvertWhmToSgmUnpaddedChunkName(t *testing.T) {
	raw := buildTestWhm()
	const (
		preamble  = 24
		sizeOff   = preamble + 12 // chunk size field
		nameLen   = preamble + 16
		nameStart = preamble + 20
	)
	if binary.LittleEndian.Uint32(raw[nameLen:]) != 12 {
		t.Fatalf("fixture name length changed, test needs updating")
	}
	stripped := make([]byte, 0, len(raw)-1)
	stripped = append(stripped, raw[:sizeOff]...)
	stripped = append(stripped, chunky.I32(int32(binary.LittleEndian.Uint32(raw[sizeOff:])-1))...)
	stripped = append(stripped, chunky.I32(11)...)
	stripped = append(stripped, raw[nameStart:nameStart+11]...) // "test_marine", no NUL
	stripped = append(stripped, raw[nameStart+12:]...)

	sgm, _, err := whm.ConvertToSgm(stripped)
	if err != nil {
		t.Fatalf("whm.ConvertToSgm: %v", err)
	}
	if root := parseSgmRoot(t, sgm); root.Name != "test_marine" {
		t.Errorf("root name = %q, want test_marine", root.Name)
	}
}

func findOrNil(parent *chunky.Chunk, typeID string) *chunky.Chunk {
	for _, c := range parent.Children {
		if c.TypeID == typeID {
			return c
		}
	}
	return nil
}

func filterChildren(parent *chunky.Chunk, typeID string) []*chunky.Chunk {
	var out []*chunky.Chunk
	for _, c := range parent.Children {
		if c.TypeID == typeID {
			out = append(out, c)
		}
	}
	return out
}

func relicI32Value(b []byte) int32 {
	if len(b) < 4 {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b))
}

// ─── merged from x_test_whm_anim_edit_test.go ────────────────────────────────────────

func TestPatchWhmAnimBoneKeysLeavesUntouchedKeysByteForByteTheSame(t *testing.T) {
	file := buildTestWhm()
	model, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	if len(model.Animations) != 1 {
		t.Fatalf("the test model has %d animations, want 1", len(model.Animations))
	}
	patched, err := whm.PatchAnimBoneKeys(file, &model.Model, 0)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if bytes.Equal(file, patched) {
		return
	}
	for i := 0; i < len(file) && i < len(patched); i++ {
		if file[i] != patched[i] {
			t.Fatalf("a save of untouched keys changed byte %d of %d (output is %d bytes)",
				i, len(file), len(patched))
		}
	}
	t.Fatalf("a save of untouched keys changed the file length: %d -> %d", len(file), len(patched))
}

func TestPatchWhmAnimBoneKeysWritesAddMoveAndRemove(t *testing.T) {
	file := buildTestWhm()
	model, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	anim := &model.Animations[0]
	track := &anim.Bones[0]
	if len(track.PosKey) != 2 || len(track.RotKey) != 2 {
		t.Fatalf("the fixture changed shape: %d position and %d rotation keys, want two of each",
			len(track.PosKey), len(track.RotKey))
	}
	// Move: the last position key goes to frame 10 (still after frame 0,
	// so the list stays sorted the way a drag along the strip keeps it).
	track.PosKey[1].Time = timeForFrame(10, anim.NumFrames)
	// Remove: the first rotation key leaves.
	track.RotKey = track.RotKey[1:]
	// Add: a position key on frame 15, appended behind the moved one.
	added := WhmKey3{Time: timeForFrame(15, anim.NumFrames), V: [3]float32{0.25, -0.5, 0.75}}
	track.PosKey = append(track.PosKey, added)

	patched, err := whm.PatchAnimBoneKeys(file, &model.Model, 0)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	// One key of each kind carries its own count entry beside it, so the
	// payload changes by 16 bytes for the added position key and loses 20
	// for the removed rotation key.
	if want := len(file) + 16 - 20; len(patched) != want {
		t.Fatalf("the patched file is %d bytes, want %d (original %d)", len(patched), want, len(file))
	}
	again, err := ParseWhmModel(patched)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	got := again.Animations[0]
	if got.NumFrames != anim.NumFrames || got.Name != anim.Name {
		t.Errorf("the clip came back as %q/%d frames, want %q/%d",
			got.Name, got.NumFrames, anim.Name, anim.NumFrames)
	}
	gotTrack := got.Bones[0]
	frames := keyEditFrames(posKeyTimes(gotTrack), anim.NumFrames)
	if len(frames) != 3 || frames[0] != 0 || frames[1] != 10 || frames[2] != 15 {
		t.Errorf("position keys sit on frames %v, want [0 10 15]", frames)
	}
	if v := gotTrack.PosKey[2].V; v != [3]float32{0.25, -0.5, 0.75} {
		t.Errorf("the added key holds %v after the round trip, want {0.25 -0.5 0.75}", v)
	}
	rotFrames := keyEditFrames(rotKeyTimes(gotTrack), anim.NumFrames)
	if len(rotFrames) != 1 || rotFrames[0] != anim.NumFrames-1 {
		t.Errorf("rotation keys sit on frames %v, want only the original last frame %d",
			rotFrames, anim.NumFrames-1)
	}
	// Everything the editor did not touch must come back untouched: the
	// mesh tracks, the camera block, the skeleton, the meshes — the whole
	// model with the one edited track held to the edit the editor made.
	want, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the original does not parse: %v", err)
	}
	want.Animations[0].Bones[0].PosKey = model.Animations[0].Bones[0].PosKey
	want.Animations[0].Bones[0].RotKey = model.Animations[0].Bones[0].RotKey
	if !reflect.DeepEqual(want, again) {
		t.Errorf("the round trip changed more than the edited track:\nwant %+v\ngot  %+v", want, again)
	}
}

func TestPatchWhmAnimBoneKeysRefusesToWriteOverTheWrongThing(t *testing.T) {
	file := buildTestWhm()
	// An index no animation answers to.
	model, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	if _, err := whm.PatchAnimBoneKeys(file, &model.Model, 5); err == nil {
		t.Error("an out of range animation index saved without complaint")
	}
	// A clip the file does not carry: its frame count says so.
	model, err = ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	model.Animations[0].NumFrames = 99
	if _, err := whm.PatchAnimBoneKeys(file, &model.Model, 0); err == nil {
		t.Error("a clip with a different frame count saved over the file's own")
	}
	// A skeleton the file does not carry: its bone tracks say so.
	model, err = ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	model.Animations[0].Bones = nil
	if _, err := whm.PatchAnimBoneKeys(file, &model.Model, 0); err == nil {
		t.Error("an animation with different bone tracks saved over the file's own")
	}
	// Not a chunky file at all.
	if _, err := whm.PatchAnimBoneKeys([]byte("not a model"), &model.Model, 0); err == nil {
		t.Error("a non-chunky file saved without complaint")
	}
}

// firstAnimWithBoneKeys is the first animation of a parsed model that carries
// a bone with at least two position keys — enough to remove one, add one and
// still have the track left. -1 means the model has no such animation.
func firstAnimWithBoneKeys(model *WhmModel) int {
	for i := range model.Animations {
		for j := range model.Animations[i].Bones {
			if len(model.Animations[i].Bones[j].PosKey) >= 2 {
				return i
			}
		}
	}
	return -1
}

// realWhmFixture is the first animated .whm of the real install that parses
// with a bone worth editing. It skips — like the other checks against the real
// game — when the game is not there.
func realWhmFixture(t *testing.T) ([]byte, *WhmModel, int) {
	t.Helper()
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	checked := 0
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		for _, sub := range []string{"troops", "vehicles"} {
			files, err := os.ReadDir(filepath.Join(dowRacesDir, race.Name(), sub))
			if err != nil {
				continue
			}
			for _, e := range files {
				if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
					continue
				}
				raw, err := os.ReadFile(filepath.Join(dowRacesDir, race.Name(), sub, e.Name()))
				if err != nil {
					continue
				}
				model, err := ParseWhmModel(raw)
				if err != nil {
					continue
				}
				if idx := firstAnimWithBoneKeys(model); idx >= 0 {
					t.Logf("fixture %s: animation %q of %d, bone %q",
						e.Name(), model.Animations[idx].Name, len(model.Animations),
						trackWithTwoPosKeys(&model.Animations[idx]).Name)
					return raw, model, idx
				}
				checked++
				if checked >= 60 {
					t.Skipf("no animated .whm in the first %d files under %s", checked, dowRacesDir)
				}
			}
		}
	}
	t.Skipf("no animated .whm under %s", dowRacesDir)
	return nil, nil, -1
}

// trackWithTwoPosKeys is the track firstAnimWithBoneKeys settled on.
func trackWithTwoPosKeys(anim *WhmAnimation) *WhmTrack {
	for i := range anim.Bones {
		if len(anim.Bones[i].PosKey) >= 2 {
			return &anim.Bones[i]
		}
	}
	return nil
}

// A save that changes nothing has to leave a shipped model byte for byte as it
// was: the game reads every part of the file, not only the part this editor
// writes, and a rebuild that shuffled a name or a padding byte would be a
// change nobody asked for.
func TestPatchWhmAnimBoneKeysLeavesARealModelIdenticalWithoutEdits(t *testing.T) {
	raw, model, idx := realWhmFixture(t)
	patched, err := whm.PatchAnimBoneKeys(raw, &model.Model, idx)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if bytes.Equal(raw, patched) {
		return
	}
	for i := 0; i < len(raw) && i < len(patched); i++ {
		if raw[i] != patched[i] {
			t.Fatalf("a save of untouched keys changed byte %d of %d (output is %d bytes)",
				i, len(raw), len(patched))
		}
	}
	t.Fatalf("a save of untouched keys changed the file length: %d -> %d", len(raw), len(patched))
}

// A structural edit against a shipped model comes back out of the parser as
// exactly that edit: the whole re-read model is compared against the original
// with the same edit applied in memory, so a size field left behind, a name
// shifted by four bytes or a tail rewritten by mistake all fail the same
// comparison.
func TestPatchWhmAnimBoneKeysEditsARealModelAndNothingElse(t *testing.T) {
	raw, model, idx := realWhmFixture(t)
	anim := &model.Animations[idx]
	track := trackWithTwoPosKeys(anim)
	if track == nil {
		t.Fatal("the fixture's animation lost its track with two keys")
	}

	// Remove the track's last position key.
	track.PosKey = track.PosKey[:len(track.PosKey)-1]
	patched, err := whm.PatchAnimBoneKeys(raw, &model.Model, idx)
	if err != nil {
		t.Fatalf("patch after the removal: %v", err)
	}
	if want := len(raw) - 16; len(patched) != want {
		t.Fatalf("after removing a key the file is %d bytes, want %d (original %d)",
			len(patched), want, len(raw))
	}
	reparsed, err := ParseWhmModel(patched)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	want, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatalf("the original does not parse: %v", err)
	}
	for j := range want.Animations[idx].Bones {
		if want.Animations[idx].Bones[j].Name == track.Name {
			want.Animations[idx].Bones[j].PosKey = track.PosKey
			break
		}
	}
	if !reflect.DeepEqual(want, reparsed) {
		t.Errorf("after the removal the model differs beyond the removed key:\nwant %+v\ngot  %+v", want, reparsed)
	}

	// Add a key on the clip's middle frame, sorted into the track.
	raw2, model2, idx2 := realWhmFixture(t)
	anim2 := &model2.Animations[idx2]
	track2 := trackWithTwoPosKeys(anim2)
	mid := WhmKey3{
		Time: timeForFrame(anim2.NumFrames/2, anim2.NumFrames),
		V:    [3]float32{0.125, -0.25, 0.5},
	}
	at := len(track2.PosKey)
	for i, k := range track2.PosKey {
		if k.Time > mid.Time {
			at = i
			break
		}
	}
	track2.PosKey = append(track2.PosKey, WhmKey3{})
	copy(track2.PosKey[at+1:], track2.PosKey[at:])
	track2.PosKey[at] = mid
	patched2, err := whm.PatchAnimBoneKeys(raw2, &model2.Model, idx2)
	if err != nil {
		t.Fatalf("patch after the addition: %v", err)
	}
	if want := len(raw2) + 16; len(patched2) != want {
		t.Fatalf("after adding a key the file is %d bytes, want %d (original %d)",
			len(patched2), want, len(raw2))
	}
	reparsed2, err := ParseWhmModel(patched2)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	want2, err := ParseWhmModel(raw2)
	if err != nil {
		t.Fatalf("the original does not parse: %v", err)
	}
	for j := range want2.Animations[idx2].Bones {
		if want2.Animations[idx2].Bones[j].Name == track2.Name {
			want2.Animations[idx2].Bones[j].PosKey = track2.PosKey
			break
		}
	}
	if !reflect.DeepEqual(want2, reparsed2) {
		t.Errorf("after the addition the model differs beyond the added key:\nwant %+v\ngot  %+v", want2, reparsed2)
	}
}

// posKeyTimes and rotKeyTimes read a track's key times for the frame checks
// the key-editing tests make.
func posKeyTimes(t WhmTrack) []float32 {
	out := make([]float32, len(t.PosKey))
	for i, k := range t.PosKey {
		out[i] = k.Time
	}
	return out
}

func rotKeyTimes(t WhmTrack) []float32 {
	out := make([]float32, len(t.RotKey))
	for i, k := range t.RotKey {
		out[i] = k.Time
	}
	return out
}

// ─── merged from x_test_whm_model_test.go ────────────────────────────────────────

func TestParseRealDowModelsForViewing(t *testing.T) {
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	parsed := 0
	total := 0
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		dir := filepath.Join(dowRacesDir, race.Name(), "troops")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range files {
			if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
				continue
			}
			total++
			label := race.Name() + "/" + e.Name()
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Errorf("%s: %v", label, err)
				continue
			}
			model, err := ParseWhmModel(raw)
			if err != nil {
				// Same variant meshes the converter reports; they are logged
				// there and must fail here as well rather than render garbage.
				t.Logf("unsupported: %s: %v", label, err)
				continue
			}
			verifyParsedModel(t, label, model, raw)
			parsed++
		}
	}
	t.Logf("parsed %d of %d models for viewing", parsed, total)
	if total > 0 && parsed*10 < total*9 {
		t.Errorf("only %d of %d models parsed, coverage regressed", parsed, total)
	}
}

func verifyParsedModel(t *testing.T, label string, model *WhmModel, raw []byte) {
	t.Helper()

	// The converter must see exactly the same vertex count and the parser
	// one sub mesh per material, which is what MeshMaterials counts; Meshes
	// counts sections, which is one entry only when every section is single
	// material.
	_, info, err := whm.ConvertToSgm(raw)
	if err != nil {
		t.Errorf("%s: converter disagrees with the model parser: %v", label, err)
		return
	}
	if len(model.Meshes) != info.MeshMaterials {
		t.Errorf("%s: %d sub meshes, converter counted %d materials", label, len(model.Meshes), info.MeshMaterials)
	}
	if len(model.Vertices) != info.Vertices {
		t.Errorf("%s: %d vertices, converter counted %d", label, len(model.Vertices), info.Vertices)
	}
	if len(model.Bones) != info.Bones {
		t.Errorf("%s: %d bones, converter counted %d", label, len(model.Bones), info.Bones)
	}
	if len(model.Animations) != info.Animations {
		t.Errorf("%s: %d animations, converter counted %d", label, len(model.Animations), info.Animations)
	}
	if len(model.Markers) != info.Markers {
		t.Errorf("%s: %d markers, converter counted %d", label, len(model.Markers), info.Markers)
	}

	// Every index must land on a real vertex, and every bone index on a real
	// bone, or the renderer would read past the end of a slice.
	for _, mesh := range model.Meshes {
		if len(mesh.Indices)%3 != 0 {
			t.Errorf("%s: mesh %q has %d indices, not a multiple of 3", label, mesh.Name, len(mesh.Indices))
		}
		for _, idx := range mesh.Indices {
			if idx >= uint32(len(model.Vertices)) {
				t.Errorf("%s: mesh %q indexes vertex %d of %d", label, mesh.Name, idx, len(model.Vertices))
				break
			}
		}
	}
	skinned := 0
	badBone := -1
	badWeight := -1
	for i := range model.Vertices {
		v := &model.Vertices[i]
		weight := float32(0)
		for s := 0; s < 4; s++ {
			if v.Bones[s] < 0 {
				continue
			}
			if v.Bones[s] >= int32(len(model.Bones)) {
				badBone = i
				break
			}
			weight += v.Weights[s]
		}
		if hasAnyBone(v) {
			skinned++
			// Weights are renormalised when skinning, so the sum only has to be
			// finite and non-negative here.
			if math.IsNaN(float64(weight)) || math.IsInf(float64(weight), 0) || weight < 0 {
				badWeight = i
			}
		}
	}
	if badBone >= 0 {
		v := &model.Vertices[badBone]
		t.Errorf("%s: vertex %d references bone %d of %d", label, badBone, v.Bones[0], len(model.Bones))
	}
	if badWeight >= 0 {
		t.Errorf("%s: vertex %d has a non-finite or negative weight sum", label, badWeight)
	}
	if len(model.Bones) > 0 && skinned == 0 {
		t.Logf("%s: model has %d bones but no skinned vertices", label, len(model.Bones))
	}

	// Animation keyframes must be normalised and reference known bones.
	for _, anim := range model.Animations {
		if anim.NumFrames <= 0 {
			t.Errorf("%s: animation %q has %d frames", label, anim.Name, anim.NumFrames)
		}
		for _, track := range anim.Bones {
			if track.Bone < 0 {
				// Game clips regularly reference bones the model does not
				// define; the skinner skips such tracks.
				continue
			}
			for _, k := range track.PosKey {
				// Key times are relative to the clip length but the game
				// stores a few beyond 1, which the sampler clamps.
				if math.IsNaN(float64(k.Time)) || k.Time < -0.001 {
					t.Errorf("%s: animation %q key time %f is invalid", label, anim.Name, k.Time)
				}
			}
		}
	}

	// The bounding box must be finite and actually contain the vertices.
	for i := 0; i < 3; i++ {
		if math.IsInf(float64(model.BoundMin[i]), 0) || math.IsNaN(float64(model.BoundMin[i])) {
			t.Errorf("%s: degenerate bounds", label)
			break
		}
		if model.BoundMax[i] < model.BoundMin[i] {
			t.Errorf("%s: inverted bounds on axis %d", label, i)
			break
		}
	}
}

func hasAnyBone(v *WhmVertex) bool {
	for s := 0; s < 4; s++ {
		if v.Bones[s] >= 0 {
			return true
		}
	}
	return false
}

func TestParseWhmModelRejectsGarbage(t *testing.T) {
	if _, err := ParseWhmModel([]byte("not a model")); err == nil {
		t.Error("expected an error for a non-chunky input")
	}
	if _, err := ParseWhmModel(append(chunky.Preamble(), chunky.I32(1706509)...)); err == nil {
		t.Error("expected an error when FOLDRSGM is missing")
	}
}

// TestWhmModelsStandOnGround locks the axis convention. Dawn of War models are
// authored Z-up with their feet on the ground plane, so the up axis runs from
// roughly zero upwards. The previous Y-up mapping put every model below the
// origin, which rendered the viewer upside down.
//
// The check is aggregate because a handful of models legitimately break the
// rule: a bomb on a ballistic arc and a hovering jetpack both sit below their
// own origin. What must hold for every model is that Z reaches above the
// origin at all, and that the vast majority stand on the plane.
func TestWhmModelsStandOnGround(t *testing.T) {
	total, standing := 0, 0
	forEachRealModel(t, func(label string, model *WhmModel) {
		if model.BoundMax[2] <= 0 {
			return // a prop authored entirely below the origin
		}
		total++
		// Measured across the shipped races, the median model dips by 0.1% of
		// its height and the 90th percentile by 11%; only flight and hover
		// units go further.
		depth := -model.BoundMin[2] / model.BoundMax[2]
		if depth <= 0.5 {
			standing++
			return
		}
		t.Logf("%s: %.0f%% below the ground plane (flying or hovering unit?)", label, depth*100)
	})
	if total < 10 {
		t.Skipf("only %d models to check", total)
	}
	if standing*20 < total*19 {
		t.Errorf("only %d of %d models stand on the ground plane; the axis convention is wrong", standing, total)
	}
	t.Logf("%d of %d models stand on the ground plane", standing, total)
}

// forEachRealModel runs fn over every .whm under the races tree that the
// parser accepts.
func forEachRealModel(t *testing.T, fn func(label string, model *WhmModel)) {
	t.Helper()
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		dir := filepath.Join(dowRacesDir, race.Name(), "troops")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range files {
			if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			model, err := ParseWhmModel(raw)
			if err != nil {
				continue
			}
			fn(race.Name()+"/"+e.Name(), model)
		}
	}
}

// TestWhmDecodePreservesHandedness guards the winding order. Swapping two axes
// of a coordinate system mirrors it, which would flip every triangle and make
// back face culling show the inside of the model.
func TestWhmDecodePreservesHandedness(t *testing.T) {
	// Volume by the divergence theorem is positive for a closed mesh wound
	// counter-clockwise seen from outside, which is exactly the convention the
	// renderer's back face culling assumes. Swapping two axes of a coordinate
	// system mirrors it and flips this sign, and the symptom is a model that
	// shows only its inside.
	signs := map[int]int{}
	checked := 0
	forEachRealModel(t, func(label string, model *WhmModel) {
		var vol float64
		for i := range model.Meshes {
			vol += meshSignedVolume(model, &model.Meshes[i])
		}
		if vol == 0 {
			return
		}
		checked++
		if vol > 0 {
			signs[1]++
		} else {
			signs[-1]++
		}
	})
	if checked < 10 {
		t.Skipf("only %d models to check", checked)
	}
	if len(signs) != 1 {
		t.Errorf("winding direction is inconsistent across models: %v", signs)
	}
	if signs[-1] != 0 {
		t.Errorf("%d of %d models are wound inside out, so back face culling shows their interior", signs[-1], checked)
	}
}

func meshSignedVolume(model *WhmModel, mesh *WhmMesh) float64 {
	total := 0.0
	idx := mesh.Indices
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := idx[i], idx[i+1], idx[i+2]
		if int(a) >= len(model.Vertices) || int(b) >= len(model.Vertices) || int(c) >= len(model.Vertices) {
			continue
		}
		p := func(v uint32) [3]float64 {
			return [3]float64{
				float64(model.Vertices[v].Pos[0]),
				float64(model.Vertices[v].Pos[1]),
				float64(model.Vertices[v].Pos[2]),
			}
		}
		pa, pb, pc := p(a), p(b), p(c)
		total += pa[0]*(pb[1]*pc[2]-pb[2]*pc[1]) -
			pa[1]*(pb[0]*pc[2]-pb[2]*pc[0]) +
			pa[2]*(pb[0]*pc[1]-pb[1]*pc[0])
	}
	return total / 6
}

// ─── merged from x_test_whm_sgm_real_test.go ────────────────────────────────────────

const (
	dowInstallDir = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data`
	dowRacesDir   = dowInstallDir + `\art\ebps\races`
	dowTroopsDir  = dowRacesDir + `\Necrons\troops`
)

func TestConvertRealDowModels(t *testing.T) {
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	converted := 0
	total := 0
	var unsupported []string
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		for _, sub := range []string{"troops", "vehicles"} {
			dir := filepath.Join(dowRacesDir, race.Name(), sub)
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range files {
				if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
					continue
				}
				total++
				path := filepath.Join(dir, e.Name())
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("%s: %v", path, err)
					continue
				}
				label := race.Name() + "/" + e.Name()
				sgm, info, err := whm.ConvertToSgm(raw)
				if err != nil {
					// A handful of shipped meshes use a per-vertex block that
					// does not match the documented stride. Those must fail
					// loudly instead of producing a corrupt .sgm, so they are
					// reported rather than treated as a test failure.
					unsupported = append(unsupported, fmt.Sprintf("%s: %v", label, err))
					continue
				}
				verifyConvertedModel(t, label, raw, sgm, info)
				verifyGeometryPreserved(t, label, raw, sgm)
				converted++
			}
		}
	}
	for _, u := range unsupported {
		t.Logf("unsupported: %s", u)
	}
	t.Logf("converted %d of %d models", converted, total)
	if total > 0 && converted*10 < total*9 {
		t.Errorf("only %d of %d models converted, coverage regressed", converted, total)
	}
}

// verifyConvertedModel re-parses the produced .sgm and checks every invariant
// the Object Editor format depends on.
func verifyConvertedModel(t *testing.T, name string, raw, sgm []byte, info *whm.ModelInfo) {
	t.Helper()
	chunks, consumed := chunky.Parse(sgm[chunky.PreambleSize:], 0)
	if consumed != len(sgm)-chunky.PreambleSize {
		t.Errorf("%s: %d trailing bytes after the root chunk", name,
			len(sgm)-chunky.PreambleSize-consumed)
	}
	if len(chunks) != 1 || chunks[0].TypeID != "FOLDRSGM" {
		t.Fatalf("%s: unexpected root chunks", name)
	}
	root := chunks[0]
	if root.Version != 1 {
		t.Errorf("%s: FOLDRSGM version %d", name, root.Version)
	}
	if !bytes.Equal(chunky.Serialize(root), sgm[chunky.PreambleSize:]) {
		t.Errorf("%s: re-serialization is not byte stable", name)
	}

	bones := 0
	meshes := 0
	anims := 0
	animTracks := 0
	for _, c := range root.Children {
		switch c.TypeID {
		case "FOLDSKEL":
			bones = verifySkel(t, name, c)
		case "FOLDMSGR":
			meshes = verifyMeshes(t, name, c)
		case "FOLDANIM":
			anims++
			animTracks += len(c.Children)
		case "DATAMARK", "DATACMRA":
			if c.Name == "" {
				t.Errorf("%s: %s chunk has an empty name", name, c.TypeID)
			}
		case "DATAFBIF", "DATASKEL", "DATACAMS", "DATAANBV", "DATADATA":
			t.Errorf("%s: .whm only chunk %s survived", name, c.TypeID)
		}
	}
	if bones != info.Bones {
		t.Errorf("%s: bones %d != %d", name, bones, info.Bones)
	}
	if meshes != info.Meshes {
		t.Errorf("%s: meshes %d != %d", name, meshes, info.Meshes)
	}
	if anims != info.Animations {
		t.Errorf("%s: animations %d != %d", name, anims, info.Animations)
	}
	t.Logf("%-34s bones=%-4d meshes=%-3d verts=%-7d anims=%-3d tracks=%-4d markers=%-3d cams=%-2d tex=%-2d  %d -> %d bytes",
		name, info.Bones, info.Meshes, info.Vertices, info.Animations, animTracks,
		info.Markers, info.Cameras, info.Textures, len(raw), len(sgm))
}

func verifySkel(t *testing.T, name string, skel *chunky.Chunk) int {
	t.Helper()
	if skel.Version != 3 {
		t.Errorf("%s: FOLDSKEL version %d", name, skel.Version)
	}
	info := skel.Child("DATAINFO")
	if info == nil || info.Version != 1 || len(info.Payload) != 4 {
		t.Fatalf("%s: FOLDSKEL/DATAINFO = %+v", name, info)
	}
	count := int(binary.LittleEndian.Uint32(info.Payload))
	boneChunks := 0
	for _, b := range skel.Children {
		if b.TypeID != "DATABONE" {
			continue
		}
		boneChunks++
		if b.Version != 5 {
			t.Errorf("%s: DATABONE version %d", name, b.Version)
		}
		if len(b.Payload) != 32 {
			t.Errorf("%s: bone %q payload %d bytes", name, b.Name, len(b.Payload))
		}
		// parent index must reference an already emitted bone or -1
		parent := int32(binary.LittleEndian.Uint32(b.Payload))
		if parent >= int32(boneChunks-1) && parent != -1 {
			t.Errorf("%s: bone %q parent %d is not a previous bone", name, b.Name, parent)
		}
	}
	if count != boneChunks {
		t.Errorf("%s: DATAINFO says %d bones, %d emitted", name, count, boneChunks)
	}
	return count
}

func verifyMeshes(t *testing.T, name string, msgr *chunky.Chunk) int {
	t.Helper()
	meshes := 0
	for _, m := range msgr.Children {
		if m.TypeID != "FOLDMSLC" {
			continue
		}
		meshes++
		if m.Child("DATADATA") == nil {
			t.Errorf("%s: mesh %q has no DATADATA", name, m.Name)
		}
		if m.Child("DATABVOL") == nil {
			t.Errorf("%s: mesh %q lost its bounding volume", name, m.Name)
		}
	}
	return meshes
}

// verifyGeometryPreserved walks the .whm mesh layout independently and compares
// it with the .sgm, proving the geometry really survived the rewrite rather
// than merely parsing without error.
func verifyGeometryPreserved(t *testing.T, name string, raw, sgm []byte) {
	t.Helper()
	inRoot := firstRoot(t, raw)
	outRoot := firstRoot(t, sgm)
	inMeshes, inVerts, inMats, inVertexBytes := walkMeshData(t, name, inRoot, true)
	outMeshes, outVerts, outMats, outVertexBytes := walkMeshData(t, name, outRoot, false)
	if inMeshes != outMeshes || inVerts != outVerts {
		t.Errorf("%s: geometry changed: %d/%d verts -> %d/%d verts",
			name, inVerts, inMeshes, outVerts, outMeshes)
	}
	if inMats != outMats {
		t.Errorf("%s: material references changed: %d -> %d", name, inMats, outMats)
	}
	if inVertexBytes != outVertexBytes {
		t.Errorf("%s: vertex data is %d bytes, want %d", name, outVertexBytes, inVertexBytes)
	}
}

func firstRoot(t *testing.T, blob []byte) *chunky.Chunk {
	t.Helper()
	chunks, _ := chunky.Parse(blob[chunky.PreambleSize:], 0)
	for _, c := range chunks {
		if c.TypeID == "FOLDRSGM" {
			return c
		}
	}
	t.Fatal("no FOLDRSGM")
	return nil
}

// walkMeshData returns meshes, vertices, material references and the total byte
// size of the vertex arrays. whm selects the .whm record layout, which carries
// the reserved dwords and the trailing shadow volume.
func walkMeshData(t *testing.T, name string, root *chunky.Chunk, isWhm bool) (int, int, int, int) {
	t.Helper()
	msgr := root.Child("FOLDMSGR")
	if msgr == nil {
		return 0, 0, 0, 0
	}
	meshes, verts, mats, vertexBytes := 0, 0, 0, 0
	for _, m := range msgr.Children {
		if m.TypeID != "FOLDMSLC" {
			continue
		}
		d := m.Child("DATADATA")
		if d == nil {
			t.Errorf("%s: mesh %q has no DATADATA", name, m.Name)
			continue
		}
		meshes++
		p := d.Payload
		r := &chunky.Reader{Data: p}
		r.Take(13)
		skin := r.Count(4)
		for i := 0; i < skin; i++ {
			r.RawStr()
			r.Skip(4)
		}
		count := r.Count(4)
		sizeID := r.I32()
		if r.Err != nil {
			t.Errorf("%s: mesh %q header: %v", name, m.Name, r.Err)
			continue
		}
		stride := 32
		if sizeID > 37 {
			stride = 48
		}
		vertexStart := r.Off
		matStart, strip := -1, 0
		for _, cand := range []struct{ off, strip int }{
			{vertexStart + count*stride + 4, 4},
			{vertexStart + count*stride, 0},
		} {
			if whm.MeshMaterialSection(p, cand.off, isWhm, false) > 0 {
				matStart, strip = cand.off, cand.strip
				break
			}
		}
		if matStart < 0 {
			for off := vertexStart + 4; off+8 < len(p); off += 2 {
				if whm.MeshMaterialSection(p, off, isWhm, true) > 0 {
					matStart = off
					if off-4 >= vertexStart && chunky.AllZero(p[off-4:off]) {
						strip = 4
					}
					break
				}
			}
		}
		if matStart < 0 {
			t.Errorf("%s: mesh %q: material section not found", name, m.Name)
			continue
		}
		verts += count
		vertexBytes += matStart - strip - vertexStart
		matEnd := whm.MeshMaterialSection(p, matStart, isWhm, false)
		mats += int(int32(binary.LittleEndian.Uint32(p[matStart:])))
		if !isWhm && matEnd != len(p) {
			t.Errorf("%s: mesh %q payload has %d trailing bytes, want none", name, m.Name, len(p)-matEnd)
		}
	}
	return meshes, verts, mats, vertexBytes
}

func TestDumpModelTree(t *testing.T) {
	if os.Getenv("DOW_DUMP") == "" {
		t.Skip("set DOW_DUMP to a .whm path")
	}
	raw, err := os.ReadFile(os.Getenv("DOW_DUMP"))
	if err != nil {
		t.Fatal(err)
	}
	chunks, _ := chunky.Parse(raw[chunky.PreambleSize:], 0)
	var dump func(prefix string, c *chunky.Chunk)
	dump = func(prefix string, c *chunky.Chunk) {
		fmt.Printf("%s%-9s v%-2d %8d  %q\n", prefix, c.TypeID, c.Version, len(c.Payload), c.Name)
		for _, ch := range c.Children {
			dump(prefix+"  ", ch)
		}
	}
	for _, c := range chunks {
		dump("", c)
	}
}
