package whm

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"dow-mod-studio/internal/chunky"
)

// Renderable representation of a Dawn of War .whm model.
//
// The converter in whm_sgm.go throws the .whm away after rewriting it, but the
// very same chunks already describe everything a viewer needs: skinned
// vertices, a bone hierarchy with bind poses and per-bone animation keyframes.
// This file decodes them into plain slices so the renderer can consume them
// without touching the chunky container again.
//
// Coordinate systems. Dawn of War stores each vector as (x, z, y) of the
// authoring tool with the X axis mirrored, which the exporter undoes as
// (-x, z, -y). Doing the same here puts the model in the orientation Blender
// would show, with Y up, so a viewer never has to guess an extra rotation.

type Vertex struct {
	Pos     [3]float32
	Normal  [3]float32
	UV      [2]float32
	Bones   [4]int32 // -1 when unused
	Weights [4]float32
}

// Mesh is one FOLDMSLC sub-mesh with its texture reference.
type Mesh struct {
	Name     string
	Material string
	// MaterialPath is the full shared-texture reference from the model's
	// DATASSHR chunks, when Material is only a display/local alias.
	MaterialPath string
	Indices      []uint32
	// SingleBone is the skeleton bone a rigid mesh follows, from the
	// FOLDMSGR mesh list. -1 when the mesh carries its own skin weights or is
	// an external reference.
	SingleBone int
}

// Bone is one skeleton's rest pose, stored relative to the parent bone.
type Bone struct {
	Name   string
	Parent int
	Pos    [3]float32
	Quat   [4]float32 // x, y, z, w
}

// Marker is a named attachment point; Markers holds its parent bone name.
type Marker struct {
	Name   string
	Parent string
	Matrix [9]float32 // 3x3 rotation
	Pos    [3]float32
}

// Camera is a named camera with its local transform.
type Camera struct {
	Name       string
	Pos        [3]float32
	Quat       [4]float32
	Fov        float32
	ClipStart  float32
	ClipEnd    float32
	FocusPoint [3]float32
}

type Key3 struct {
	Time float32 // normalised 0..1
	V    [3]float32
}

type Key4 struct {
	Time float32 // normalised 0..1
	V    [4]float32
}

// Track holds the keyframes of one animated bone. Key times are stored
// normalised to the clip length, so a clip can be resampled without knowing its
// frame rate.
type Track struct {
	Bone   int // index into Model.Bones, -1 for mesh/material tracks
	Name   string
	PosKey []Key3
	RotKey []Key4
	Stale  bool
}

// TrackKind distinguishes the per-mesh and per-material tracks that follow
// the bone tracks inside an animation.
type TrackKind int

const (
	TrackMeshVisibility TrackKind = iota
	TrackTextureUV
)

// MeshTrack is a mesh visibility animation: keys hold frame and alpha.
type MeshTrack struct {
	Name           string
	ForceInvisible bool
	// Force — значение, на котором меш удерживается, пока кривая не
	// сказала иное: ноль — меш не виден, единица — виден целиком.
	Force float32
	Keys  []Key3 // Time, value, 0
}

// TextureTrack is an animated material UV offset, tiling or colour channel.
type TextureTrack struct {
	Material string
	Channel  int // 1 offsetU, 2 offsetV, 3 tileU, 4 tileV
	Keys     []Key3
}

// Animation is one FOLDANIM clip.
type Animation struct {
	Name      string
	NumFrames int
	Duration  float32
	Bones     []Track
	Meshes    []MeshTrack
	Textures  []TextureTrack
}

// Model is a decoded model ready for the renderer.
type Model struct {
	Name       string
	Vertices   []Vertex
	Meshes     []Mesh
	Bones      []Bone
	Markers    []Marker
	Cameras    []Camera
	Animations []Animation
	BoundMin   [3]float32
	BoundMax   [3]float32
	// SharedTextures preserves the canonical references from FOLDSTXT.
	SharedTextures []string
	// MaterialPaths maps a shared shader's display label to its diffuse map.
	MaterialPaths map[string]string
	boneParents   []int
	// MarkerForwards is which of each marker's six frame directions its
	// effects leave in, worked out once from the mesh. It lives here rather
	// than in a table keyed by the model so that a model is freed when the
	// tab holding it closes.
	MarkerForwards []int
}

// ParseModel decodes a .whm byte stream into a renderable model.
func Parse(data []byte) (*Model, error) {
	if len(data) < chunky.PreambleSize || string(data[:len(chunky.Magic)]) != string(chunky.Magic) {
		return nil, fmt.Errorf("not a Relic chunky model file")
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
		return nil, fmt.Errorf("FOLDRSGM chunk not found")
	}

	m := &Model{Name: root.Name}
	m.BoundMin = [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	m.BoundMax = [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}

	for _, c := range root.Children {
		var err error
		switch c.TypeID {
		case "DATASKEL":
			err = m.parseSkeleton(c)
		case "DATAMARK":
			err = m.parseMarkers(c)
		case "DATACAMS":
			err = m.parseCameras(c)
		case "FOLDSTXT":
			m.parseSharedTexture(c)
		case "FOLDSHDR":
			m.parseSharedShader(c)
		case "FOLDMSGR":
			err = m.parseMeshes(c, false)
		case "FOLDANIM":
			err = m.parseAnimation(c)
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

// parseSharedTexture records each FOLDSTXT canonical texture path, indexed by
// its basename so equivalent shared-shader labels can use it as an alias.
func (m *Model) parseSharedTexture(c *chunky.Chunk) {
	path := strings.TrimSpace(c.Name)
	if path == "" {
		return
	}
	for _, existing := range m.SharedTextures {
		if strings.EqualFold(existing, path) {
			return
		}
	}
	m.SharedTextures = append(m.SharedTextures, path)
}

// parseSharedShader reads the diffuse channel's texture path and indexes it
// by the shader label used in mesh material lists.
func (m *Model) parseSharedShader(c *chunky.Chunk) {
	if c == nil || c.Name == "" {
		return
	}
	for _, channel := range c.Children {
		if channel.TypeID != "DATACHAN" || len(channel.Payload) < 12 || binary.LittleEndian.Uint32(channel.Payload[:4]) != 0 {
			continue
		}
		r := &chunky.Reader{Data: channel.Payload[12:]}
		path := strings.TrimSpace(r.Str())
		if r.Err != nil || path == "" {
			continue
		}
		if m.MaterialPaths == nil {
			m.MaterialPaths = make(map[string]string)
		}
		m.MaterialPaths[strings.ToLower(strings.TrimSpace(c.Name))] = path
		return
	}
}

// --- skeleton ---------------------------------------------------------------

func (m *Model) parseSkeleton(c *chunky.Chunk) error {
	r := &chunky.Reader{Data: c.Payload}
	count := r.Count(4)
	m.Bones = make([]Bone, 0, count)
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		parent := int(r.I32())
		body := r.Take(28) // 3 floats translation + 4 floats rotation
		if r.Err != nil {
			break
		}
		m.Bones = append(m.Bones, Bone{
			Name:   name,
			Parent: parent,
			Pos:    decodeBoneVec3(body[0:12]),
			Quat:   decodeBoneQuat(body[12:28]),
		})
	}
	if r.Err != nil {
		return fmt.Errorf("skeleton: %w", r.Err)
	}
	return nil
}

func (m *Model) parseMarkers(c *chunky.Chunk) error {
	r := &chunky.Reader{Data: c.Payload}
	count := r.Count(4)
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		parent := r.Str()
		// A marker is a 3x3 rotation (9 floats) followed by a position (3).
		body := r.Take(48)
		if r.Err != nil {
			break
		}
		mark := Marker{Name: name, Parent: parent, Pos: decodeBoneVec3(body[36:48])}
		for k := 0; k < 9; k++ {
			mark.Matrix[k] = decodeFloat(body[k*4:])
		}
		m.Markers = append(m.Markers, mark)
	}
	if r.Err != nil {
		return fmt.Errorf("markers: %w", r.Err)
	}
	return nil
}

func (m *Model) parseCameras(c *chunky.Chunk) error {
	r := &chunky.Reader{Data: c.Payload}
	count := r.Count(4)
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		body := r.Take(12 + 16 + 12 + 12)
		if r.Err != nil {
			break
		}
		cam := Camera{
			Name:       name,
			Pos:        decodeBoneVec3(body[0:12]),
			Quat:       decodeBoneQuat(body[12:28]),
			Fov:        decodeFloat(body[28:32]),
			ClipStart:  decodeFloat(body[32:36]),
			ClipEnd:    decodeFloat(body[36:40]),
			FocusPoint: decodeBoneVec3(body[40:52]),
		}
		m.Cameras = append(m.Cameras, cam)
	}
	if r.Err != nil {
		return fmt.Errorf("cameras: %w", r.Err)
	}
	return nil
}

// --- meshes -----------------------------------------------------------------

// parseMeshes reads one FOLDMSGR group. sgm says which container flavour the
// mesh payloads are in: the .sgm record drops the reserved dword that follows
// each material's face block, and that is the only difference that reaches
// this far.
func (m *Model) parseMeshes(c *chunky.Chunk, sgm bool) error {
	var ranges []whmMeshRange
	for _, sub := range c.Children {
		if sub.TypeID != "FOLDMSLC" {
			continue
		}
		data := sub.Child("DATADATA")
		if data == nil {
			continue
		}
		beg := len(m.Vertices)
		mesh, err := m.parseMeshData(data.Payload, beg, sgm)
		if err != nil {
			return fmt.Errorf("mesh %q: %w", sub.Name, err)
		}
		m.Vertices = append(m.Vertices, mesh.vertices...)
		if len(mesh.materials) == 0 {
			m.Meshes = append(m.Meshes, Mesh{
				Name:       sub.Name,
				SingleBone: -1,
			})
			ranges = append(ranges, whmMeshRange{mesh: len(m.Meshes) - 1, beg: beg, n: len(mesh.vertices)})
			continue
		}
		// One sub mesh per material: a section's materials each carry their
		// own face range over the same shared vertices, and collapsing them
		// onto the first material painted the whole sub mesh with one of its
		// maps. Models ship with a different texture per range, so the split
		// is what keeps the second map from being drawn with the first.
		for i, material := range mesh.materials {
			materialPath := m.MaterialPaths[strings.ToLower(strings.TrimSpace(material))]
			if materialPath == "" && len(m.SharedTextures) == len(mesh.materials) {
				materialPath = m.SharedTextures[i]
			}
			m.Meshes = append(m.Meshes, Mesh{
				Name:         sub.Name,
				Material:     material,
				MaterialPath: materialPath,
				Indices:      mesh.indices[i],
				SingleBone:   -1,
			})
			ranges = append(ranges, whmMeshRange{mesh: len(m.Meshes) - 1, beg: beg, n: len(mesh.vertices)})
		}
	}
	m.applyMeshBones(c, ranges)
	return nil
}

// whmMeshRange records where one parsed mesh's vertices landed in the shared
// vertex array, so the mesh list can bind them afterwards.
type whmMeshRange struct {
	mesh   int
	beg, n int
}

// applyMeshBones reads the FOLDMSGR mesh list, which names every mesh in the
// model together with the single bone a rigid mesh follows, and binds those
// meshes' vertices to that bone at full weight. Without it a building's
// individual pieces carry no skin weights at all and never move, because there
// is nothing in the vertex data to move them.
func (m *Model) applyMeshBones(c *chunky.Chunk, ranges []whmMeshRange) {
	data := c.Child("DATADATA")
	if data == nil {
		return
	}
	r := &chunky.Reader{Data: data.Payload}
	count := r.Count(4)
	for i := 0; i < count && r.Err == nil; i++ {
		name := r.Str()
		r.Str() // external reference path, unused here
		bone := int32(binary.LittleEndian.Uint32(r.Take(4)))
		if bone < 0 || int(bone) >= len(m.Bones) {
			continue
		}
		for _, rg := range ranges {
			if !strings.EqualFold(m.Meshes[rg.mesh].Name, name) {
				continue
			}
			m.Meshes[rg.mesh].SingleBone = int(bone)
			for v := rg.beg; v < rg.beg+rg.n && v < len(m.Vertices); v++ {
				if m.Vertices[v].Weights[0] > 0 {
					continue // already skinned
				}
				m.Vertices[v].Bones = [4]int32{bone, -1, -1, -1}
				m.Vertices[v].Weights = [4]float32{1, 0, 0, 0}
			}
			// No break: a section split into one sub mesh per material shares
			// this name, and all of them follow the same bone.
		}
	}
}

// whmMeshGeometry is the decoded form of one FOLDMSLC DATADATA payload.
type whmMeshGeometry struct {
	vertices  []Vertex
	indices   [][]uint32
	materials []string
}

func (m *Model) parseMeshData(data []byte, vertexBase int, sgm bool) (*whmMeshGeometry, error) {
	r := &chunky.Reader{Data: data}
	r.Take(13) // reserved, flag, triangle count, reserved
	skinBones := r.Count(4)
	// The bone ids inside the vertex data are local to this mesh: the skin bone
	// list maps a local index to a bone name, which in turn indexes the model
	// wide skeleton. Relic also writes meshes whose ids already address the
	// global array, so unmapped ids are passed through.
	localToGlobal := make(map[int32]int32, skinBones)
	for i := 0; i < skinBones && r.Err == nil; i++ {
		name := r.Str()
		local := int32(binary.LittleEndian.Uint32(r.Take(4)))
		if global := int32(m.boneIndex(name)); global >= 0 {
			localToGlobal[local] = global
		}
	}

	verts := r.Count(4)
	sizeID := r.I32()
	if r.Err != nil {
		return nil, r.Err
	}
	skinned := sizeID > 37
	stride := 32
	if skinned {
		stride += 16
	}
	vertexStart := r.Off
	block := verts * stride
	if skinned {
		block += 4 // the .whm reserved dword after the UV array
	}
	if vertexStart+block > len(data) {
		return nil, fmt.Errorf("vertex block of %d vertices does not fit", verts)
	}
	pos := r.Take(verts * 12)
	// Skinned vertices interleave their influences: three weights and then the
	// four bone ids, sixteen bytes per vertex. Reading the weights and the ids
	// as two separate flat arrays walks the block with the wrong stride, which
	// is what used to leave a fifth of the vertices unweighted and shifted the
	// rest onto the wrong bones.
	skin := make([]byte, 0, verts*16)
	if skinned {
		skin = r.Take(verts * 16)
	}
	normals := r.Take(verts * 12)
	uvs := r.Take(verts * 8)
	if r.Err != nil {
		return nil, r.Err
	}

	geo := &whmMeshGeometry{vertices: make([]Vertex, verts)}
	for i := 0; i < verts; i++ {
		v := &geo.vertices[i]
		v.Pos = decodeVec3(pos[i*12:])
		v.Normal = decodeVec3(normals[i*12:])
		v.UV = [2]float32{decodeFloat(uvs[i*8:]), decodeFloat(uvs[i*8+4:])}
		if !skinned {
			continue
		}
		rec := skin[i*16:]
		w := [3]float32{
			sanitizeWeight(decodeFloat(rec)),
			sanitizeWeight(decodeFloat(rec[4:])),
			sanitizeWeight(decodeFloat(rec[8:])),
		}
		// The fourth influence is whatever the first three leave over. Game
		// data is not always tidy here, so it is floored at zero and the
		// skinner renormalises whatever is left once unbound bones are
		// dropped.
		rest := 1 - w[0] - w[1] - w[2]
		if rest < 0 {
			rest = 0
		}
		v.Weights = [4]float32{w[0], w[1], w[2], rest}
		for s := 0; s < 4; s++ {
			id := int32(rec[12+s])
			v.Bones[s] = -1
			if id == 255 {
				continue
			}
			if global, ok := localToGlobal[id]; ok {
				v.Bones[s] = global
			} else if int(id) < len(m.Bones) {
				v.Bones[s] = id
			} else {
				// The file references a bone this model does not have. Relic
				// ships such vertices; the engine drops the extra influence and
				// so do we.
				v.Bones[s] = -1
			}
		}
	}

	// Locate the material list; the vertex block is followed by an optional
	// reserved dword. See convertMeshData for why the search is needed.
	materialStart := -1
	if MeshMaterialSection(data, vertexStart+verts*stride+4, true, false) > 0 {
		materialStart = vertexStart + verts*stride + 4
	} else if MeshMaterialSection(data, vertexStart+verts*stride, true, false) > 0 {
		materialStart = vertexStart + verts*stride
	} else {
		for off := vertexStart + 4; off+8 < len(data); off += 2 {
			if MeshMaterialSection(data, off, true, true) > 0 {
				materialStart = off
				break
			}
		}
	}
	if materialStart < 0 {
		return nil, fmt.Errorf("cannot locate the material section after %d vertices (size id %d)", verts, sizeID)
	}

	r.Off = materialStart
	materialCount := r.Count(4)
	for i := 0; i < materialCount && r.Err == nil; i++ {
		path := r.Str()
		indices := r.I32()
		if r.Err != nil || indices < 0 || int(indices)*2 > len(data)-r.Off {
			return nil, fmt.Errorf("face block of %d indices does not fit", indices)
		}
		raw := r.Take(int(indices) * 2)
		r.Skip(4) // min vertex + vertex count
		if !sgm {
			r.Skip(4) // reserved, .whm only; .sgm drops it
		}
		var faces []uint32
		for j := 0; j+2 < len(raw); j += 6 {
			// Each triangle is stored as three uint16 in the order (x, z, y).
			a := uint32(binary.LittleEndian.Uint16(raw[j:]))
			b := uint32(binary.LittleEndian.Uint16(raw[j+2:]))
			c := uint32(binary.LittleEndian.Uint16(raw[j+4:]))
			faces = append(faces, a+uint32(vertexBase), c+uint32(vertexBase), b+uint32(vertexBase))
		}
		// The face ranges are kept per material rather than concatenated:
		// they index different vertices of the same shared array and each
		// names the map those triangles belong to.
		geo.materials = append(geo.materials, path)
		geo.indices = append(geo.indices, faces)
	}
	if r.Err != nil {
		return nil, r.Err
	}
	return geo, nil
}

// --- animations -------------------------------------------------------------

func (m *Model) parseAnimation(c *chunky.Chunk) error {
	for _, sub := range c.Children {
		if sub.TypeID != "DATADATA" {
			continue
		}
		anim, err := m.parseAnimationData(sub)
		if err != nil {
			return fmt.Errorf("animation %q: %w", c.Name, err)
		}
		anim.Name = c.Name
		m.Animations = append(m.Animations, anim)
	}
	return nil
}

func (m *Model) parseAnimationData(c *chunky.Chunk) (Animation, error) {
	data := c.Payload
	r := &chunky.Reader{Data: data}
	anim := Animation{
		NumFrames: int(r.I32()),
		Duration:  decodeFloat(r.Take(4)),
	}
	boneCount := r.Count(4)
	for i := 0; i < boneCount && r.Err == nil; i++ {
		name := r.Str()
		track := Track{Bone: m.boneIndex(name), Name: name}
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
		anim.Bones = append(anim.Bones, track)
	}

	trackCount := r.Count(4)
	for i := 0; i < trackCount && r.Err == nil; i++ {
		name := r.Str()
		mode := r.I32()
		switch mode {
		case 2:
			r.Skip(8)
			n := r.Count(4) - 1 // the count includes one forced key
			r.Skip(4)
			// The value a mesh is held at until its curve says otherwise,
			// as a float32 and not a byte: zero holds it back, one lets it
			// be drawn.
			//
			// The restored monolith's phase_in holds all twenty-three of
			// its meshes back this way and lets them out at its first
			// key, which is what a model that phases in does, while its
			// phase_out and every ordinary clip of it hold nothing back.
			// The Obelisk is the same pair of readings the other way
			// round: build_phasing fades its body in from its curve, and
			// the vis_level clips that show the finished obelisk hold it
			// visible.
			force := decodeFloat(r.Take(4))
			track := MeshTrack{Name: name, ForceInvisible: force == 0, Force: force}
			if n > 0 {
				raw := r.Take(n * 8)
				for j := 0; j < n; j++ {
					track.Keys = append(track.Keys, Key3{
						Time: decodeFloat(raw[j*8:]),
						V:    [3]float32{decodeFloat(raw[j*8+4:]), 0, 0},
					})
				}
			}
			anim.Meshes = append(anim.Meshes, track)
		case 0:
			r.Skip(4)
			channel := int(r.I32())
			n := r.Count(4)
			track := TextureTrack{Material: name, Channel: channel}
			if n > 0 {
				raw := r.Take(n * 8)
				for j := 0; j < n; j++ {
					track.Keys = append(track.Keys, Key3{
						Time: decodeFloat(raw[j*8:]),
						V:    [3]float32{decodeFloat(raw[j*8+4:]), 0, 0},
					})
				}
			}
			anim.Textures = append(anim.Textures, track)
		default:
			r.Skip(len(data) - r.Off)
		}
	}
	if r.Err != nil {
		return anim, r.Err
	}
	return anim, nil
}

// boneIndex resolves a bone name to its index, or -1 when the clip references
// a bone this model does not have.
func (m *Model) boneIndex(name string) int {
	for i := range m.Bones {
		if m.Bones[i].Name == name {
			return i
		}
	}
	return -1
}

func (m *Model) recomputeBounds() {
	if len(m.Vertices) == 0 {
		m.BoundMin = [3]float32{}
		m.BoundMax = [3]float32{}
		return
	}
	min, max := m.BoundMin, m.BoundMax
	for _, v := range m.Vertices {
		for i := 0; i < 3; i++ {
			if v.Pos[i] < min[i] {
				min[i] = v.Pos[i]
			}
			if v.Pos[i] > max[i] {
				max[i] = v.Pos[i]
			}
		}
	}
	m.BoundMin, m.BoundMax = min, max
}

// --- scalar helpers ---------------------------------------------------------

func decodeFloat(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

// sanitizeWeight drops non-finite and negative skin weights. A few shipped
// meshes store garbage in the second weight slot; clamping keeps that from
// poisoning a whole vertex instead of simply ignoring the bad influence.
func sanitizeWeight(w float32) float32 {
	if math.IsNaN(float64(w)) || math.IsInf(float64(w), 0) || w < 0 {
		return 0
	}
	return w
}

// decodeVec3 turns a stored mesh position into renderer coordinates.
//
// A mesh vertex is stored as (x, z, y) and the reference importer emits
// (-x, -y, z) into its Z-up authoring tool, so the game space maps to that as
// (f0, f4, f8) -> (-f0, -f8, f4). The renderer uses that same Z-up space.
func decodeVec3(b []byte) [3]float32 {
	return [3]float32{-decodeFloat(b), -decodeFloat(b[8:]), decodeFloat(b[4:])}
}

// decodeBoneVec3 turns a stored bone translation into renderer coordinates.
// Unlike a mesh vertex a bone is stored in natural (x, y, z) order, and the
// reference importer emits (-x, y, z) with no axis swap at all.
func decodeBoneVec3(b []byte) [3]float32 {
	return [3]float32{-decodeFloat(b), decodeFloat(b[4:]), decodeFloat(b[8:])}
}

// decodeBoneQuat turns a stored bone rotation into renderer coordinates. The
// reference importer builds Quaternion(w, x, -y, -z) from the stored
// (x, y, z, w), so the vector part is (x, -y, -z) in natural axis order.
func decodeBoneQuat(b []byte) [4]float32 {
	return [4]float32{decodeFloat(b), -decodeFloat(b[4:]), -decodeFloat(b[8:]), decodeFloat(b[12:])}
}
