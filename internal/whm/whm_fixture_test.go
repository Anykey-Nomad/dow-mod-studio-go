package whm_test

import (
	"bytes"
	"encoding/binary"
	"math"

	"dow-mod-studio/internal/chunky"
)

// The synthetic .whm the parser tests parse: a small FOLDRSGM tree built from
// the same chunk types the game writes, so a test can parse a model without
// the game's files.

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

func tU16(vs ...uint16) []byte {
	b := make([]byte, 0, len(vs)*2)
	for _, v := range vs {
		b = append(b, byte(v), byte(v>>8))
	}
	return b
}
