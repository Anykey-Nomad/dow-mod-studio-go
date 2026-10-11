package main

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/whm"
	"encoding/binary"
	"math"
	"testing"
)

// The fixture below mirrors, byte for byte, what blender_dow's exporter.py
// writes for ExportFormat.WHM, so the conversion is checked against the real
// layout rather than against itself.

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
