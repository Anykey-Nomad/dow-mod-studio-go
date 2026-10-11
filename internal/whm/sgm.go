package whm

import (
	"bytes"
	"fmt"

	"dow-mod-studio/internal/chunky"
)

// Relic Chunky container -> Object Editor (.sgm) conversion.
//
// Both .whm (game model) and .sgm (Object Editor model) are "Relic Chunky"
// streams: a 24 byte preamble followed by a tree of chunks. A chunk header is
//
//	typeid   8 bytes ASCII
//	version  int32
//	size     int32  (bytes of body that follow the header, children included)
//	nameLen  int32
//	name     nameLen bytes, NUL terminated when non-empty
//
// The two container formats carry the same data but split it differently:
//
//	.whm                                 .sgm
//	DATAFBIF  (exporter meta)            -- dropped --
//	DATASSHR  v2                         DATASSHR v1
//	FOLDTXTR / FOLDSHDR                  FOLDTXTR / FOLDSHDR (identical)
//	DATASKEL  v5  flat bone list         FOLDSKEL v3 -> DATAINFO v1 + DATABONE v5
//	FOLDMSGR  mesh data + shadow volume  FOLDMSGR  same, minus the shadow volume
//	DATAMARK  v1  count + N records      N named DATAMARK v1 chunks
//	DATACAMS  v1  count + N records      N named DATACMRA v1 chunks
//	FOLDANIM  v3 -> DATADATA v2          FOLDANIM v2 -> FOLDDATA v3
//	                   + DATAANBV v1        -> DATAINFO v5 + DATABANM v2 + DATACANM v2
//
// In .sgm the object/bone/marker/camera/animation name moves from the record
// payload into the chunk name, and camera animation tracks are dropped because
// the Object Editor format has nowhere to put them.

// ModelInfo summarises what a conversion produced, for the properties panel.
type ModelInfo struct {
	Name          string
	Bones         int
	Meshes        int
	Vertices      int
	MeshMaterials int
	Materials     int
	Textures      int
	Markers       int
	Cameras       int
	Animations    int
}

// ConvertToSgm rewrites a .whm model as an Object Editor .sgm model. It is a
// pure byte transformation: no Blender, no texture work, no re-triangulation.
func ConvertToSgm(data []byte) ([]byte, *ModelInfo, error) {
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil, nil, fmt.Errorf("not a Relic chunky model file")
	}
	chunks, _ := chunky.Parse(data[chunky.PreambleSize:], 0)
	var root *chunky.Chunk
	for _, c := range chunks {
		if c.TypeID == "FOLDRSGM" {
			root = c
			break
		}
	}
	if root == nil {
		return nil, nil, fmt.Errorf("FOLDRSGM chunk not found")
	}

	info := &ModelInfo{Name: root.Name}
	out := &chunky.Chunk{TypeID: "FOLDRSGM", Version: 1, Name: root.Name}
	for _, c := range root.Children {
		switch c.TypeID {
		case "DATAFBIF", "DATAANBV":
			// .whm only: exporter metadata and an animation bounding volume
			// placeholder. The .sgm container has no room for either.
		case "DATASKEL":
			skel, err := convertWhmSkel(c, info)
			if err != nil {
				return nil, nil, err
			}
			out.Children = append(out.Children, skel)
		case "DATAMARK":
			marks, err := convertWhmMarks(c, info)
			if err != nil {
				return nil, nil, err
			}
			out.Children = append(out.Children, marks...)
		case "DATACAMS":
			cams, err := convertWhmCams(c, info)
			if err != nil {
				return nil, nil, err
			}
			out.Children = append(out.Children, cams...)
		case "FOLDANIM":
			anims, err := convertWhmAnims(c, info)
			if err != nil {
				return nil, nil, err
			}
			out.Children = append(out.Children, anims...)
		case "DATASSHR":
			info.Materials++
			clone := chunky.Clone(c)
			clone.Version = 1
			out.Children = append(out.Children, clone)
		case "FOLDTXTR":
			info.Textures++
			out.Children = append(out.Children, chunky.Clone(c))
		case "FOLDMSGR":
			meshes, err := convertWhmMeshGroup(c, info)
			if err != nil {
				return nil, nil, err
			}
			out.Children = append(out.Children, meshes)
		default:
			// FOLDSHDR and anything the game ships that we do not model here
			// keep their exact bytes.
			out.Children = append(out.Children, chunky.Clone(c))
		}
	}

	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(out))
	return buf.Bytes(), info, nil
}

func Inspect(data []byte) (*ModelInfo, error) {
	_, info, err := ConvertToSgm(data)
	return info, err
}

// --- per chunk conversions --------------------------------------------------

// DATASKEL: int32 bone count, then per bone
//
//	name string, parentIndex int32, translation 3 x f32, rotation 4 x f32
//
// becomes FOLDSKEL { DATAINFO{count}, DATABONE(name){parentIndex, tr, rot} }.
func convertWhmSkel(c *chunky.Chunk, info *ModelInfo) (*chunky.Chunk, error) {
	r := &chunky.Reader{Data: c.Payload}
	bones := r.Count(4)
	info.Bones = bones
	out := &chunky.Chunk{TypeID: "FOLDSKEL", Version: 3}
	out.Children = append(out.Children, &chunky.Chunk{TypeID: "DATAINFO", Version: 1, Payload: chunky.I32(int32(bones))})
	for i := 0; i < bones && r.Err == nil; i++ {
		name := r.Str()
		body := r.Take(4 + 12 + 16) // parent index + translation + rotation
		out.Children = append(out.Children, &chunky.Chunk{TypeID: "DATABONE", Version: 5, Name: name, Payload: body})
	}
	if r.Err != nil {
		return nil, fmt.Errorf("skeleton: %w", r.Err)
	}
	return out, nil
}

// DATAMARK: int32 marker count, then per marker
//
//	name string, parentName string, rotation 3 x 3 f32, translation 3 x f32
//
// becomes one DATAMARK chunk per marker, named after the marker.
func convertWhmMarks(c *chunky.Chunk, info *ModelInfo) ([]*chunky.Chunk, error) {
	r := &chunky.Reader{Data: c.Payload}
	count := r.Count(4)
	var out []*chunky.Chunk
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		parent := r.RawStr()
		rest := r.Take(36 + 12)
		body := make([]byte, 0, len(parent)+len(rest))
		body = append(body, parent...)
		body = append(body, rest...)
		out = append(out, &chunky.Chunk{TypeID: "DATAMARK", Version: 1, Name: name, Payload: body})
	}
	if r.Err != nil {
		return nil, fmt.Errorf("markers: %w", r.Err)
	}
	info.Markers = len(out)
	return out, nil
}

// DATACAMS: int32 camera count, then per camera
//
//	name string, translation 3 x f32, rotation 4 x f32,
//	fov/clipStart/clipEnd 3 x f32, focus point 3 x f32
//
// becomes one DATACMRA chunk per camera, named after the camera.
func convertWhmCams(c *chunky.Chunk, info *ModelInfo) ([]*chunky.Chunk, error) {
	r := &chunky.Reader{Data: c.Payload}
	count := r.Count(4)
	var out []*chunky.Chunk
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		rest := r.Take(12 + 16 + 12 + 12)
		out = append(out, &chunky.Chunk{TypeID: "DATACMRA", Version: 1, Name: name, Payload: rest})
	}
	if r.Err != nil {
		return nil, fmt.Errorf("cameras: %w", r.Err)
	}
	info.Cameras = len(out)
	return out, nil
}

// FOLDMSGR holds FOLDMSLC children (one per mesh) plus a mesh list and a group
// bounding volume. The .sgm mesh record drops the trailing shadow volume and
// two reserved fields, so DATADATA is rebuilt rather than copied.
func convertWhmMeshGroup(c *chunky.Chunk, info *ModelInfo) (*chunky.Chunk, error) {
	out := &chunky.Chunk{TypeID: "FOLDMSGR", Version: 1, Name: c.Name}
	for _, ch := range c.Children {
		if ch.TypeID != "FOLDMSLC" {
			out.Children = append(out.Children, chunky.Clone(ch))
			continue
		}
		mesh := &chunky.Chunk{TypeID: "FOLDMSLC", Version: ch.Version, Name: ch.Name}
		for _, sub := range ch.Children {
			if sub.TypeID != "DATADATA" {
				mesh.Children = append(mesh.Children, chunky.Clone(sub))
				continue
			}
			payload, verts, materials, err := convertWhmMeshData(sub.Payload)
			if err != nil {
				return nil, fmt.Errorf("mesh %q: %w", ch.Name, err)
			}
			mesh.Children = append(mesh.Children, &chunky.Chunk{
				TypeID:  "DATADATA",
				Version: sub.Version,
				Name:    sub.Name,
				Payload: payload,
			})
			info.Meshes++
			info.Vertices += verts
			info.MeshMaterials += materials
		}
		out.Children = append(out.Children, mesh)
	}
	return out, nil
}

// convertWhmMeshData rewrites one mesh DATADATA payload for the .sgm layout.
//
//	rsv int32, flag int8, polygonCount int32, rsv int32
//	skinBoneCount int32, per bone: name string + boneIndex uint32
//	vertexCount int32, vertexSizeId int32   (37 = 32 bytes/vertex, 39 = 48)
//	per vertex: position 3 x f32, [weights 3 x f32 + 4 x uint8],
//	            normal 3 x f32, uv 2 x f32
//	optional 4 reserved bytes                <- .whm only
//	materialCount int32, per material:
//	    texture path string, indexCount int32, indexCount x uint16,
//	    minVertex uint16, vertexCount uint16, reserved 4 bytes  <- .whm only
//	shadow volume vertices/faces/edges                     <- .whm only
//
// indexCount is the number of vertex indices, not the number of triangles,
// so a material with 126 triangles stores 378.
//
// The vertex array is byte identical in both formats, so it is copied through
// untouched. Only the reserved dword in front of the material list and the
// shadow volume are removed, which means the exact start of the material
// section matters. Relic shipped a few mesh variants whose per-vertex block
// does not match the nominal stride, so the start is confirmed against the rest
// of the payload rather than trusted blindly; see MeshMaterialSection.
func convertWhmMeshData(data []byte) ([]byte, int, int, error) {
	r := &chunky.Reader{Data: data}
	var out bytes.Buffer
	out.Write(r.Take(13)) // rsv, flag, polygon count, rsv
	if r.Err != nil {
		return nil, 0, 0, r.Err
	}

	skinBones := r.Count(4)
	skinBlock := chunky.I32(int32(skinBones))
	for i := 0; i < skinBones && r.Err == nil; i++ {
		skinBlock = append(skinBlock, r.RawStr()...)
		skinBlock = append(skinBlock, r.Take(4)...)
	}
	out.Write(skinBlock)

	verts := r.Count(4)
	vertexSizeID := r.I32()
	out.Write(chunky.I32(int32(verts)))
	out.Write(chunky.I32(vertexSizeID))
	if r.Err != nil {
		return nil, 0, 0, r.Err
	}
	vertexStart := r.Off

	// The nominal stride comes from vertexSizeId, not from the bone count:
	// some shipped meshes report no skin bones yet still store 48 bytes.
	stride := 32
	if vertexSizeID > 37 {
		stride = 48
	}
	materialStart, strip := -1, 0
	if MeshMaterialSection(data, vertexStart+verts*stride+4, true, false) > 0 {
		// Nominal layout: a reserved dword separates the UV array from the
		// material list, and the .sgm format does not carry it.
		materialStart, strip = vertexStart+verts*stride+4, 4
	} else if MeshMaterialSection(data, vertexStart+verts*stride, true, false) > 0 {
		materialStart, strip = vertexStart+verts*stride, 0
	} else {
		// Unusual variant: look for the one offset from which the rest of the
		// payload decodes as a material list.
		for off := vertexStart + 4; off+8 < len(data); off += 2 {
			if MeshMaterialSection(data, off, true, true) <= 0 {
				continue
			}
			materialStart = off
			if off-4 >= vertexStart && chunky.AllZero(data[off-4:off]) {
				strip = 4
			}
			break
		}
	}
	if materialStart < 0 || materialStart-strip < vertexStart {
		return nil, 0, 0, fmt.Errorf("cannot locate the material section after %d vertices (size id %d)", verts, vertexSizeID)
	}
	out.Write(data[vertexStart : materialStart-strip])

	r.Off = materialStart
	materialCount := r.Count(4)
	matBlock := chunky.I32(int32(materialCount))
	for i := 0; i < materialCount && r.Err == nil; i++ {
		matBlock = append(matBlock, r.RawStr()...) // texture path
		indexCount := r.I32()
		matBlock = append(matBlock, chunky.I32(indexCount)...)
		if r.Err != nil || indexCount < 0 || int(indexCount)*2 > len(data)-r.Off {
			return nil, 0, 0, fmt.Errorf("face block of %d indices does not fit", indexCount)
		}
		matBlock = append(matBlock, r.Take(int(indexCount)*2)...) // one uint16 each
		matBlock = append(matBlock, r.Take(4)...)                 // min vertex + vertex count
		r.Skip(4)                                                 // reserved, .whm only
	}
	out.Write(matBlock)
	if r.Err != nil {
		return nil, 0, 0, r.Err
	}
	// Everything left is the .whm-only shadow volume: dropped.
	return out.Bytes(), verts, materialCount, nil
}

// MeshMaterialSection reports whether a material list starts at off, returning
// the offset just past the list (and 0 when off is not one). The scan lives
// with the model in internal/whm, which reads the same layout.
// FOLDANIM wraps a single DATADATA record. In .sgm the record becomes a
// FOLDDATA folder holding DATAINFO plus one chunk per animated bone, mesh or
// material track. Names move from the record into the chunk name; camera
// tracks are dropped because .sgm cannot store them.
func convertWhmAnims(c *chunky.Chunk, info *ModelInfo) ([]*chunky.Chunk, error) {
	out := &chunky.Chunk{TypeID: "FOLDANIM", Version: 2, Name: c.Name}
	found := false
	for _, sub := range c.Children {
		if sub.TypeID != "DATADATA" {
			continue // DATAANBV: .whm only animation bounding volume
		}
		found = true
		fold, err := convertWhmAnimData(sub)
		if err != nil {
			return nil, fmt.Errorf("animation %q: %w", c.Name, err)
		}
		out.Children = append(out.Children, fold)
	}
	if found {
		info.Animations++
	}
	if len(out.Children) == 0 {
		return nil, nil
	}
	return []*chunky.Chunk{out}, nil
}

func convertWhmAnimData(c *chunky.Chunk) (*chunky.Chunk, error) {
	data := c.Payload
	r := &chunky.Reader{Data: data}
	// DATAINFO v5 is byte identical to the .whm frame count + duration header.
	infoHdr := r.Take(8)
	if r.Err != nil {
		return nil, r.Err
	}
	out := &chunky.Chunk{TypeID: "FOLDDATA", Version: 3, Name: c.Name}
	out.Children = append(out.Children, &chunky.Chunk{TypeID: "DATAINFO", Version: 5, Payload: infoHdr})

	tracks := r.Count(4)
	for i := 0; i < tracks && r.Err == nil; i++ {
		name := r.Str()
		start := r.Off
		// Position keys, rotation keys, then the stale flag: the .sgm DATABANM
		// payload is exactly these bytes.
		n := r.Count(16)
		r.Skip(n * 16)
		n = r.Count(20)
		r.Skip(n * 20)
		r.Skip(1)
		if r.Err != nil {
			break
		}
		out.Children = append(out.Children, &chunky.Chunk{
			TypeID:  "DATABANM",
			Version: 2,
			Name:    name,
			Payload: data[start:r.Off],
		})
	}

	canims := r.Count(4)
	for i := 0; i < canims && r.Err == nil; i++ {
		name := r.Str()
		start := r.Off
		mode := r.I32()
		switch mode {
		case 2: // mesh visibility: reserved x2, key count, reserved, force flag.
			// The key count includes a forced first key, so the number of
			// (frame, value) pairs that follow is count-1.
			r.Skip(8)
			n := r.Count(4) - 1
			r.Skip(4) // reserved
			r.Skip(4) // force invisible flag
			if n > 0 {
				r.Skip(n * 8)
			}
		case 0: // material UV: reserved, channel, key count, key pairs
			r.Skip(4)
			r.Skip(4) // texture channel
			n := r.Count(4)
			r.Skip(n * 8)
		default:
			// Unknown track kind: keep the remainder verbatim so nothing is lost.
			r.Skip(len(data) - r.Off)
		}
		if r.Err != nil {
			break
		}
		out.Children = append(out.Children, &chunky.Chunk{
			TypeID:  "DATACANM",
			Version: 2,
			Name:    name,
			Payload: data[start:r.Off],
		})
	}
	if r.Err != nil {
		return nil, r.Err
	}
	// Remaining bytes are the .whm-only animated camera tracks.
	return out, nil
}

func boneKeyPayload(anim *Animation, boneIndex int) ([]byte, error) {
	var track *Track
	for i := range anim.Bones {
		if anim.Bones[i].Bone == boneIndex {
			track = &anim.Bones[i]
			break
		}
	}
	if track == nil {
		return nil, fmt.Errorf("bone %d has no track in animation %q", boneIndex, anim.Name)
	}
	pos, rot := track.PosKey, track.RotKey
	if len(pos) == 0 && len(rot) == 0 {
		return nil, fmt.Errorf("bone %d has no track in animation %q", boneIndex, anim.Name)
	}
	var b bytes.Buffer
	b.Write(chunky.I32(int32(len(pos))))
	for _, k := range pos {
		b.Write(chunky.F32(k.V[0]))
		b.Write(chunky.F32(k.V[1]))
		b.Write(chunky.F32(k.V[2]))
	}
	b.Write(chunky.I32(int32(len(rot))))
	for _, k := range rot {
		b.Write(chunky.F32(k.V[0]))
		b.Write(chunky.F32(k.V[1]))
		b.Write(chunky.F32(k.V[2]))
		b.Write(chunky.F32(k.V[3]))
	}
	b.WriteByte(0)
	return b.Bytes(), nil
}
