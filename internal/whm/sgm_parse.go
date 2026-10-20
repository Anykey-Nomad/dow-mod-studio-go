package whm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"dow-mod-studio/internal/chunky"
)

// The Object Editor's .sgm model carries the same data as a .whm model, but
// names move out of the record payloads into the chunk names and a few
// .whm-only blocks are dropped. sgm.go is the writer (whm → sgm); this file is
// the reader for the other side, so an .sgm a modder saved from the Object
// Editor opens in the same viewer, with the same animations, as the .whm the
// game ships.
//
//	.whm                                  .sgm
//	DATASKEL v5 flat bone list            FOLDSKEL v3 -> DATAINFO + DATABONE(name)
//	DATAMARK count + records              N named DATAMARK v1 (parent + rot + pos)
//	DATACAMS count + records              N named DATACMRA v1
//	FOLDANIM -> DATADATA v2 (named)       FOLDANIM -> FOLDDATA v3 -> DATAINFO
//	                                       + DATABANM(name) + DATACANM(name)
//	mesh DATADATA with a reserved dword   same, minus that dword and the
//	after the UVs and per material        shadow volume (see parseMeshes)
//
// Camera animation tracks exist only in .whm and are absent from every .sgm.

// IsSgm reports whether a Relic Chunky model stream is the Object Editor
// (.sgm) flavour rather than the game's (.whm). The two formats spell the
// skeleton differently — a FOLDSKEL folder in .sgm, a flat DATASKEL chunk in
// .whm — which is the reliable difference between them.
func IsSgm(data []byte) bool {
	root := chunkyRoot(data)
	return root != nil && root.Child("FOLDSKEL") != nil
}

// chunkyRoot parses the preamble and returns the FOLDRSGM root, or nil when
// the stream is not a Relic Chunky model file.
func chunkyRoot(data []byte) *chunky.Chunk {
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil
	}
	chunks, _ := chunky.Parse(data[chunky.PreambleSize:], 0)
	for _, c := range chunks {
		if c.TypeID == "FOLDRSGM" {
			return c
		}
	}
	return nil
}

// ParseSgm decodes an Object Editor .sgm model into the same Model a .whm
// parse produces, so the viewer, the clip lists and the pose code need no
// knowledge of which container the model came in.
func ParseSgm(data []byte) (*Model, error) {
	root := chunkyRoot(data)
	if root == nil {
		return nil, fmt.Errorf("not a Relic Chunky model file")
	}
	m := &Model{Name: root.Name}
	m.BoundMin = [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	m.BoundMax = [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for _, c := range root.Children {
		var err error
		switch c.TypeID {
		case "FOLDSKEL":
			err = m.parseSgmSkeleton(c)
		case "DATAMARK":
			err = m.parseSgmMarker(c)
		case "DATACMRA":
			err = m.parseSgmCamera(c)
		case "FOLDMSGR":
			err = m.parseMeshes(c, true)
		case "FOLDANIM":
			err = m.parseSgmAnimation(c)
		}
		if err != nil {
			return nil, err
		}
	}
	m.boneParents = make([]int, len(m.Bones))
	for i, b := range m.Bones {
		m.boneParents[i] = b.Parent
	}
	m.recomputeBounds()
	return m, nil
}

// InspectSgm summarises an .sgm model the way Inspect summarises a .whm one:
// the counts the properties panel shows, read without decoding every vertex.
func InspectSgm(data []byte) (*ModelInfo, error) {
	root := chunkyRoot(data)
	if root == nil {
		return nil, fmt.Errorf("not a Relic Chunky model file")
	}
	info := &ModelInfo{Name: root.Name}
	for _, c := range root.Children {
		switch c.TypeID {
		case "FOLDSKEL":
			for _, b := range c.Children {
				if b.TypeID == "DATABONE" {
					info.Bones++
				}
			}
		case "DATAMARK":
			info.Markers++
		case "DATACMRA":
			info.Cameras++
		case "DATASSHR":
			info.Materials++
		case "FOLDTXTR":
			info.Textures++
		case "FOLDANIM":
			if c.Child("FOLDDATA") != nil {
				info.Animations++
			}
		case "FOLDMSGR":
			for _, sub := range c.Children {
				if sub.TypeID != "FOLDMSLC" {
					continue
				}
				data := sub.Child("DATADATA")
				if data == nil {
					continue
				}
				meshes, verts, mats := sgmMeshCounts(data.Payload)
				info.Meshes += meshes
				info.Vertices += verts
				info.MeshMaterials += mats
			}
		}
	}
	return info, nil
}

// sgmMeshCounts reads the counters of one .sgm mesh payload — the walk the
// full mesh reader does, stopping where the geometry would begin. The material
// list is located with the same probes the .whm reader uses, so a payload
// written by either side of the conversion is counted the same way.
func sgmMeshCounts(data []byte) (meshes, verts, mats int) {
	r := &chunky.Reader{Data: data}
	r.Take(13) // reserved, flag, triangle count, reserved
	skinBones := r.Count(4)
	for i := 0; i < skinBones && r.Err == nil; i++ {
		r.Str()   // bone name
		r.Skip(4) // local bone index
	}
	verts = r.Count(4)
	sizeID := r.I32()
	if r.Err != nil || verts < 0 {
		return 1, 0, 0
	}
	stride := 32
	if sizeID > 37 {
		stride = 48
	}
	vertexStart := r.Off
	base := vertexStart + verts*stride
	if base > len(data) {
		return 1, verts, 0
	}
	matStart := -1
	if base+4 <= len(data) && MeshMaterialSection(data, base+4, true, false) > 0 {
		matStart = base + 4
	} else if MeshMaterialSection(data, base, true, false) > 0 {
		matStart = base
	} else {
		for off := vertexStart + 4; off+8 < len(data); off += 2 {
			if MeshMaterialSection(data, off, true, true) > 0 {
				matStart = off
				break
			}
		}
	}
	if matStart < 0 || matStart+4 > len(data) {
		return 1, verts, 0
	}
	n := int(binary.LittleEndian.Uint32(data[matStart : matStart+4]))
	if n < 0 || n > (len(data)-matStart)/4 {
		n = 0
	}
	return 1, verts, n
}

// parseSgmSkeleton reads FOLDSKEL: a DATAINFO count followed by one DATABONE
// chunk per bone, named after it, each carrying the parent index and the rest
// pose exactly as the flat .whm record stores them.
func (m *Model) parseSgmSkeleton(c *chunky.Chunk) error {
	for _, b := range c.Children {
		if b.TypeID != "DATABONE" {
			continue
		}
		r := &chunky.Reader{Data: b.Payload}
		parent := int(r.I32())
		body := r.Take(28) // translation 3 x f32 + rotation 4 x f32
		if r.Err != nil {
			return fmt.Errorf("bone %q: %w", b.Name, r.Err)
		}
		m.Bones = append(m.Bones, Bone{
			Name:   b.Name,
			Parent: parent,
			Pos:    decodeBoneVec3(body[0:12]),
			Quat:   decodeBoneQuat(body[12:28]),
		})
	}
	return nil
}

// parseSgmMarker reads one named DATAMARK chunk: the parent's name, then the
// same rotation and position the .whm record carries after its own name.
func (m *Model) parseSgmMarker(c *chunky.Chunk) error {
	r := &chunky.Reader{Data: c.Payload}
	parent := strings.TrimRight(r.Str(), "\x00")
	body := r.Take(48) // 3 x 3 rotation floats + 3 position floats
	if r.Err != nil {
		return fmt.Errorf("marker %q: %w", c.Name, r.Err)
	}
	mark := Marker{Name: c.Name, Parent: parent, Pos: decodeBoneVec3(body[36:48])}
	for k := 0; k < 9; k++ {
		mark.Matrix[k] = decodeFloat(body[k*4:])
	}
	m.Markers = append(m.Markers, mark)
	return nil
}

// parseSgmCamera reads one named DATACMRA chunk: the name is the chunk's, the
// payload is the .whm camera record without it.
func (m *Model) parseSgmCamera(c *chunky.Chunk) error {
	r := &chunky.Reader{Data: c.Payload}
	body := r.Take(12 + 16 + 12 + 12)
	if r.Err != nil {
		return fmt.Errorf("camera %q: %w", c.Name, r.Err)
	}
	m.Cameras = append(m.Cameras, Camera{
		Name:       c.Name,
		Pos:        decodeBoneVec3(body[0:12]),
		Quat:       decodeBoneQuat(body[12:28]),
		Fov:        decodeFloat(body[28:32]),
		ClipStart:  decodeFloat(body[32:36]),
		ClipEnd:    decodeFloat(body[36:40]),
		FocusPoint: decodeBoneVec3(body[40:52]),
	})
	return nil
}

// parseSgmAnimation reads one FOLDANIM: its FOLDDATA folder holds the frame
// header and one chunk per animated track, named after the bone or material
// the track drives.
func (m *Model) parseSgmAnimation(c *chunky.Chunk) error {
	for _, f := range c.Children {
		if f.TypeID != "FOLDDATA" {
			continue
		}
		anim, err := m.parseSgmAnimData(f)
		if err != nil {
			return fmt.Errorf("animation %q: %w", c.Name, err)
		}
		anim.Name = c.Name
		m.Animations = append(m.Animations, anim)
	}
	return nil
}

// parseSgmAnimData reads one FOLDDATA folder: DATAINFO carries the frame count
// and duration, DATABANM one bone's keys, DATACANM a mesh visibility or
// material UV track. The payloads start after the name the .whm record stores
// inside them, which is the chunk name here.
func (m *Model) parseSgmAnimData(f *chunky.Chunk) (Animation, error) {
	var anim Animation
	if info := f.Child("DATAINFO"); info != nil && len(info.Payload) >= 8 {
		anim.NumFrames = int(binary.LittleEndian.Uint32(info.Payload[0:4]))
		anim.Duration = decodeFloat(info.Payload[4:8])
	}
	for _, tr := range f.Children {
		switch tr.TypeID {
		case "DATABANM":
			r := &chunky.Reader{Data: tr.Payload}
			track := Track{Bone: m.boneIndex(tr.Name), Name: tr.Name}
			if n := r.Count(16); n > 0 {
				raw := r.Take(n * 16)
				for j := 0; j < n; j++ {
					track.PosKey = append(track.PosKey, Key3{
						Time: decodeFloat(raw[j*16:]),
						V:    decodeBoneVec3(raw[j*16+4:]),
					})
				}
			}
			if n := r.Count(20); n > 0 {
				raw := r.Take(n * 20)
				for j := 0; j < n; j++ {
					track.RotKey = append(track.RotKey, Key4{
						Time: decodeFloat(raw[j*20:]),
						V:    decodeBoneQuat(raw[j*20+4:]),
					})
				}
			}
			track.Stale = r.I8() == 0
			if r.Err != nil {
				return anim, fmt.Errorf("bone track %q: %w", tr.Name, r.Err)
			}
			anim.Bones = append(anim.Bones, track)
		case "DATACANM":
			r := &chunky.Reader{Data: tr.Payload}
			mode := r.I32()
			switch mode {
			case 2: // mesh visibility
				r.Skip(8)
				n := r.Count(4) - 1 // the count includes one forced key
				r.Skip(4)
				force := decodeFloat(r.Take(4))
				track := MeshTrack{Name: tr.Name, ForceInvisible: force == 0}
				if n > 0 {
					raw := r.Take(n * 8)
					for j := 0; j < n; j++ {
						track.Keys = append(track.Keys, Key3{
							Time: decodeFloat(raw[j*8:]),
							V:    [3]float32{decodeFloat(raw[j*8+4:]), 0, 0},
						})
					}
				}
				if r.Err != nil {
					return anim, fmt.Errorf("mesh track %q: %w", tr.Name, r.Err)
				}
				anim.Meshes = append(anim.Meshes, track)
			case 0: // material UV
				r.Skip(4)
				channel := int(r.I32())
				n := r.Count(4)
				track := TextureTrack{Material: tr.Name, Channel: channel}
				if n > 0 {
					raw := r.Take(n * 8)
					for j := 0; j < n; j++ {
						track.Keys = append(track.Keys, Key3{
							Time: decodeFloat(raw[j*8:]),
							V:    [3]float32{decodeFloat(raw[j*8+4:]), 0, 0},
						})
					}
				}
				if r.Err != nil {
					return anim, fmt.Errorf("texture track %q: %w", tr.Name, r.Err)
				}
				anim.Textures = append(anim.Textures, track)
			default:
				// Unknown track kind: nothing after it can be trusted.
				return anim, fmt.Errorf("track %q has unknown mode %d", tr.Name, mode)
			}
		}
	}
	return anim, nil
}
