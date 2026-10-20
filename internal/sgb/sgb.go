// Package sgb reads Dawn of War's .sgb scenario (map) files.
//
// A scenario is a Relic Chunky container rooted at FOLDSCEN. The layouts here
// follow the DowAdvMapEditor reference parser (fr.dow.gamedata.sgbmap), so the
// field meanings are known rather than guessed:
//
//		FOLDSCEN
//		  DATAWMHD            u32 tag, u32 worldDim, len-str engine,
//		                      u32 nameBytes + UTF-16 name,
//		                      u32 descBytes + UTF-16 description, u32 tail
//		  FOLDWSTC            world static data
//		    FOLDTERR          terrain
//		      DATAEFFC        water parameters (wave name, RGBA, texture repeat)
//		      DATAHRZN        water horizon settings
//		      DATAWCLR        water color
//		      DATAOCLR        water overlay color
//		      FOLDTFAC        terrain facets
//		        DATAVDAT      GroundData header: centerX, centerZ (i32),
//		                      cellsize, heightscale (f32), cellnbrX, cellnbrZ,
//		                      texTileSize, texTileRepeat (i32) — no heights here
//		        FOLDCHAN      terrain channels
//		          DATAHMAP    u32 w, u32 h, then w*h height bytes, column-major
//		                      (hmap[8 + j*w + i]), scaled by heightscale
//		          DATATTYP    footfall + cover byte grids
//		          DATAIMAP    u32 w, u32 h, w*h passability bytes
//		        DATADSHD      u32 n, n × ([len-str][path][u32 texPtr])
//		        DATADETT      u32 count, count × u32 texPtr (one per tile)
//		      FOLDDECL        decals//        DATASMAP      u32 n, n × ([len-str][path][u32 decalPtr])
//	       DATAENTY      u32 n, n × 24B: u32 id, u32 decalPtr,
//	                     f32 x, f32 z, f32 size, f32 angle
//	 FOLDSMEG            placed entities (objects)
//	   DATAEBPT          u32 n, n × [len-str] blueprint path, indexed by
//	                     the entity records
//	   FOLDENTL          entity list
//	     FOLDENTY        one entity
//	       DATAENTI      u32 id, u32 blueprintIndex, 9×f32 transform
//	                     columns, f32 x, f32 y, f32 z
//	 FOLDMSTC            static world data
//		    FOLDPFDR          pathfinding
//		      DATAIMPA        u32 w, u32 h, u32 tag, w*h bytes   passability
//
// The world grid is measured in cells of cellsize metres; a vertex sits at
// (i - centerX + 1) * cellsize on X and the same on Z. The relief therefore
// spans [cellnbrX+1] × [cellnbrZ+1] vertices and cellnbrX × cellnbrZ cells.
package sgb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"

	"dow-mod-studio/internal/chunky"
)

// Grid is one byte-per-cell map with its stored dimensions. The bytes are the
// file's own row-major order; a caller that needs metres uses Map's ground
// header instead.
type Grid struct {
	W, H int
	Data []byte
}

// At returns the cell at (x, y), or 0 outside the grid.
func (g Grid) At(x, y int) byte {
	if x < 0 || y < 0 || x >= g.W || y >= g.H || len(g.Data) < g.W*g.H {
		return 0
	}
	return g.Data[y*g.W+x]
}

// Ground is the world header a scenario's terrain is measured against. Every
// distance below is in cells of CellSize metres.
type Ground struct {
	CenterX, CenterZ int     // centre vertex index, so the origin sits here
	CellSize         float32 // metres per cell
	HeightScale      float32 // metres per height byte
	CellsX, CellsZ   int     // cell counts; vertex counts are these + 1
	TileSize         int     // cells per ground-texture tile
	TileRepeat       int     // texture repeats inside one tile
}

// WaterLevel returns the reference viewer's sea plane height in metres.
func (g Ground) WaterLevel() float32 { return 4.5 }

// WaterBounds is the water plane extent in the renderer's map-plane X/Y axes.
type WaterBounds struct {
	MinX, MaxX float32
	MinY, MaxY float32
}

// Bounds returns the ground's extents in the renderer's X/Y-up map plane.
func (g Ground) Bounds() WaterBounds {
	return WaterBounds{MinX: g.XMin(), MaxX: g.XMax(), MinY: g.ZMin(), MaxY: g.ZMax()}
}

// XMin/XMax and ZMin/ZMax are the world-space bounds of the relief, matching
// the reference editor's get_XMin and friends.
func (g Ground) XMin() float32 { return (1 - float32(g.CenterX)) * g.CellSize }
func (g Ground) XMax() float32 { return (1 + float32(g.CellsX-g.CenterX)) * g.CellSize }
func (g Ground) ZMin() float32 { return (1 - float32(g.CenterZ)) * g.CellSize }
func (g Ground) ZMax() float32 { return (1 + float32(g.CellsZ-g.CenterZ)) * g.CellSize }

// Decal is one placed decal. X and Z are world coordinates in metres, Size the
// tile's world size and Angle its rotation in radians.
type Decal struct {
	ID    int
	Name  string // resolved from DecalNames through Ptr
	Ptr   int    // key into Map.DecalNames
	X, Z  float32
	Size  float32
	Angle float32
}

// Entity is one placed object: a building, a strategic point, a wargear
// crate. X/Y/Z is its world position in metres (the same frame the decals
// use), BP its index into Map.Blueprints, and Heading the yaw recovered from
// the stored transform matrix, in radians.
type Entity struct {
	ID      int
	BP      int
	Owner   int // 0 is neutral/world, 1–8 are player slots
	Name    string // resolved from Blueprints through BP
	X, Y, Z float32
	Heading float32
}

// Map is the part of a scenario the viewer draws.
type Map struct {
	// sourceRoot and entityChunks retain the original chunky records so a save
	// can replace only the edited entity payloads while preserving unrelated
	// scenario data and per-entity extension chunks.
	sourceRoot     *chunky.Chunk
	sourceChunks   []*chunky.Chunk
	sourcePreamble []byte
	sourceTrailing []byte
	entityChunks   map[int]*chunky.Chunk
	entityOrder    []int
	entityLayout   []*chunky.Chunk
	playerLists    map[int]*chunky.Chunk

	Name    string // map name from the world header
	Engine  string // "dxp2", "DoWDE", ...
	Dim     int    // world dimension the header declares
	Ground  Ground // world header from DATAVDAT
	Relief  Grid   // DATAHMAP, row-major copy of the column-major file bytes
	Terrain Grid   // DATATTYP footfall bytes
	Cover   Grid   // DATATTYP cover bytes
	Pass    Grid   // DATAIMAP passability
	Tiles   []int  // DATADETT: ground-texture pointer per tile
	TileNX  int    // tile grid width
	TileNZ  int    // tile grid height
	// TexNames maps a DSHD key to its ground-texture blueprint path.
	TexNames map[int]string
	// DecalNames maps an SMAP key to its decal blueprint path.
	DecalNames map[int]string
	Decals     []Decal
	// Blueprints lists the entity blueprint paths from DATAEBPT; entity
	// records point into it by index, so it is a slice, not a map.
	Blueprints []string
	Entities   []Entity
}

// IsSgb reports whether the stream is a scenario file rather than another
// chunky flavour: a FOLDSCEN root is the difference that counts.
func IsSgb(data []byte) bool {
	return root(data) != nil
}

// Parse decodes a scenario's terrain core. A file without terrain parses into
// a map with empty grids — the viewer says so — but a file that is not a
// FOLDSCEN container at all is an error.
func Parse(data []byte) (*Map, error) {
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil, fmt.Errorf("not a Relic Chunky scenario file")
	}
	body := data[chunky.PreambleSize:]
	chunks, used := chunky.Parse(body, 0)
	var r *chunky.Chunk
	for _, chunk := range chunks {
		if chunk.TypeID == "FOLDSCEN" {
			r = chunk
			break
		}
	}
	if r == nil {
		return nil, fmt.Errorf("not a Relic Chunky scenario file")
	}
	m := &Map{
		TexNames:       map[int]string{},
		DecalNames:     map[int]string{},
		sourceRoot:     chunky.Clone(r),
		sourcePreamble: append([]byte(nil), data[:chunky.PreambleSize]...),
		entityChunks:   make(map[int]*chunky.Chunk),
		playerLists:    make(map[int]*chunky.Chunk),
	}
	for _, chunk := range chunks {
		m.sourceChunks = append(m.sourceChunks, chunky.Clone(chunk))
	}
	if used >= 0 && used <= len(body) {
		m.sourceTrailing = append([]byte(nil), body[used:]...)
	}
	m.parseHeader(find(r, "DATAWMHD"))
	m.parseGround(find(r, "DATAVDAT"))
	m.parseHeightMap(find(r, "DATAHMAP"))
	m.parseTerrainType(find(r, "DATATTYP"))
	m.parseImap(find(r, "DATAIMAP"))
	m.parseTexTiles(find(r, "DATADETT"))
	m.parseFileLinks(find(r, "DATADSHD"), m.TexNames)

	// The decal table and the placed decals are siblings under FOLDDECL; the
	// table has to be decoded first so the records can resolve names.
	if ch := find(r, "FOLDDECL"); ch != nil {
		m.parseFileLinks(find(ch, "DATASMAP"), m.DecalNames)
		if en := find(ch, "DATAENTY"); en != nil {
			m.parseDecals(en.Payload)
		}
	}

	// The placed objects sit in FOLDSMEG: the blueprint table first, then
	// the entity list, because the records resolve names through the table.
	if meg := find(r, "FOLDSMEG"); meg != nil {
		m.parseBlueprints(find(meg, "DATAEBPT"))
		if entl := find(meg, "FOLDENTL"); entl != nil {
			m.parseEntities(entl)
		}
		m.parsePlayerLists(meg)
	}
	return m, nil
}

// root parses the preamble and returns the FOLDSCEN root, or nil.
func root(data []byte) *chunky.Chunk {
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil
	}
	chunks, _ := chunky.Parse(data[chunky.PreambleSize:], 0)
	for _, c := range chunks {
		if c.TypeID == "FOLDSCEN" {
			return c
		}
	}
	return nil
}

// find walks the subtree for the first chunk of a type.
func find(c *chunky.Chunk, typeID string) *chunky.Chunk {
	if c == nil {
		return nil
	}
	for _, ch := range c.Children {
		if ch.TypeID == typeID {
			return ch
		}
		if sub := find(ch, typeID); sub != nil {
			return sub
		}
	}
	return nil
}

// parseHeader reads the world header: declared dimension, engine tag, map
// name and description. Strings here are byte-counted, not len-str: a u32
// byte count followed by that many bytes, UTF-16 for the name and
// description, plain ASCII for the short engine tag.
func (m *Map) parseHeader(c *chunky.Chunk) {
	if c == nil {
		return
	}
	r := &chunky.Reader{Data: c.Payload}
	r.I32() // tag: 0 or 2; differs between maps and unused
	m.Dim = int(r.I32())
	m.Engine = strings.TrimRight(r.Str(), "\x00")
	m.Name = strings.TrimRight(readCountedUTF16(r), "\x00")
	if desc := strings.TrimRight(readCountedUTF16(r), "\x00"); desc != "" {
		// The description is long; keep only its first line for the
		// viewer's info row.
		if i := strings.IndexAny(desc, "\r\n"); i > 0 {
			desc = desc[:i]
		}
	}
}

// readCountedUTF16 reads a u32 byte count followed by that many bytes,
// decoded as UTF-16LE. A short or implausible count yields "".
func readCountedUTF16(r *chunky.Reader) string {
	n := int(r.I32())
	if r.Err != nil {
		return ""
	}
	if n < 0 || n%2 != 0 || n > len(r.Data)-r.Off {
		return ""
	}
	raw := r.Take(n)
	if r.Err != nil {
		return ""
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(raw[i:]))
	}
	return string(utf16.Decode(units))
}

// parseGround reads DATAVDAT, the world header. The reference editor reads
// this from offset 0: centerX, centerZ, cellsize, heightscale, cellnbrX,
// cellnbrZ, texTileSize, texTileRepeat.
func (m *Map) parseGround(c *chunky.Chunk) {
	if c == nil || len(c.Payload) < 48 {
		return
	}
	p := c.Payload
	g := Ground{
		CenterX:     int(le32(p, 0)),
		CenterZ:     int(le32(p, 4)),
		CellSize:    f32(p[8:]),
		HeightScale: f32(p[16:]),
		CellsX:      int(le32(p, 24)),
		CellsZ:      int(le32(p, 28)),
		TileSize:    int(le32(p, 40)),
		TileRepeat:  int(le32(p, 44)),
	}
	// A map without a usable header still has a relief; fall back to the
	// declared world dimension so the viewer has something to scale by.
	if g.CellsX <= 0 || g.CellsZ <= 0 {
		g.CellsX, g.CellsZ = m.Dim, m.Dim
	}
	if g.CellSize <= 0 {
		g.CellSize = 2
	}
	if g.TileSize <= 0 {
		g.TileSize = 16
	}
	m.Ground = g
}

// parseHeightMap reads DATAHMAP. The file stores heights column-major
// (hmap[8 + j*W + i]); the copy here is row-major so Grid.At indexes with
// y*W+x, which is what the rest of the app expects.
func (m *Map) parseHeightMap(c *chunky.Chunk) {
	if c == nil {
		return
	}
	p := c.Payload
	if len(p) < 8 {
		return
	}
	w := int(le32(p, 0))
	h := int(le32(p, 4))
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 || 8+w*h > len(p) {
		return
	}
	data := make([]byte, w*h)
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			data[j*w+i] = p[8+j*w+i]
		}
	}
	m.Relief = Grid{W: w, H: h, Data: data}
}

// parseTerrainType reads DATATTYP, which holds two byte grids: a footfall map
// then a cover map, each preceded by its own size pair.
func (m *Map) parseTerrainType(c *chunky.Chunk) {
	if c == nil {
		return
	}
	p := c.Payload
	if len(p) < 12 {
		return
	}
	m.Terrain = readGridAt(p, 4)
	end := 12 + m.Terrain.W*m.Terrain.H
	if end+8 <= len(p) {
		m.Cover = readGridAt(p, end)
	}
}

// parseImap reads DATAIMAP, the passability grid: a size pair then bytes.
func (m *Map) parseImap(c *chunky.Chunk) {
	if c == nil {
		return
	}
	m.Pass = readGridAt(c.Payload, 0)
}

// readGridAt reads a [w][h][bytes] grid starting at off.
func readGridAt(p []byte, off int) Grid {
	if off < 0 || off+8 > len(p) {
		return Grid{}
	}
	w := int(le32(p, off))
	h := int(le32(p, off+4))
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 || off+8+w*h > len(p) {
		return Grid{}
	}
	data := make([]byte, w*h)
	copy(data, p[off+8:off+8+w*h])
	return Grid{W: w, H: h, Data: data}
}

// parseTexTiles reads DATADETT: a count then that many u32 texture pointers,
// one per tile, laid out row-major over the tile grid.
func (m *Map) parseTexTiles(c *chunky.Chunk) {
	if c == nil {
		return
	}
	p := c.Payload
	if len(p) < 4 {
		return
	}
	n := int(le32(p, 0))
	if n <= 0 || n > len(p)/4 || n > 1<<20 {
		return
	}
	tiles := make([]int, n)
	for i := 0; i < n; i++ {
		tiles[i] = int(le32(p, 4+4*i))
	}
	m.Tiles = tiles
	g := m.Ground
	if g.TileSize > 0 {
		m.TileNX = g.CellsX / g.TileSize
		m.TileNZ = g.CellsZ / g.TileSize
	}
	if m.TileNX*m.TileNZ != n {
		// The declared tile grid does not match the record count; keep the
		// flat list and let the viewer fall back to one row per record.
		m.TileNX, m.TileNZ = n, 1
	}
}

// parseFileLinks reads a DSHD or SMAP chunk: a count then records of
// [len-str][path][u32 key]. The key is what the tile and decal records point
// at, so the result is a map rather than a slice.
func (m *Map) parseFileLinks(c *chunky.Chunk, out map[int]string) {
	if c == nil {
		return
	}
	r := &chunky.Reader{Data: c.Payload}
	n := r.Count(4)
	for i := 0; i < n && r.Err == nil; i++ {
		name := strings.TrimRight(r.Str(), "\x00")
		key := int(r.I32())
		if r.Err != nil {
			break
		}
		out[key] = name
	}
}

// parseDecals reads the placed-decal records: a count then 24 bytes each —
// u32 id, u32 decal pointer into the SMAP table, then x, z, size and angle as
// little-endian float32.
func (m *Map) parseDecals(p []byte) {
	r := &chunky.Reader{Data: p}
	n := r.Count(24)
	out := make([]Decal, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		rec := r.Take(24)
		if r.Err != nil {
			break
		}
		d := Decal{
			ID:    int(le32(rec, 0)),
			Ptr:   int(le32(rec, 4)),
			X:     f32(rec[8:]),
			Z:     f32(rec[12:]),
			Size:  f32(rec[16:]),
			Angle: f32(rec[20:]),
		}
		d.Name = m.DecalNames[d.Ptr]
		out = append(out, d)
	}
	m.Decals = out
}

// parseBlueprints reads DATAEBPT: a count then count length-prefixed
// blueprint paths (data:attrib\ebps\...). The records carry no key — entities
// point at them by index.
func (m *Map) parseBlueprints(c *chunky.Chunk) {
	if c == nil {
		return
	}
	r := &chunky.Reader{Data: c.Payload}
	n := r.Count(4) // a record is at least its 4-byte length prefix
	out := make([]string, 0, n)
	for i := 0; i < n && r.Err == nil; i++ {
		out = append(out, strings.TrimRight(r.Str(), "\x00"))
	}
	m.Blueprints = out
}

// parseEntities walks FOLDENTL's FOLDENTY records; each carries a DATAENTI
// payload of id, blueprint index, nine transform floats and the position.
func (m *Map) parseEntities(c *chunky.Chunk) {
	for _, ch := range c.Children {
		if ch.TypeID == "FOLDENTY" {
			if e := m.parseEntity(find(ch, "DATAENTI")); e != nil {
				m.Entities = append(m.Entities, *e)
				m.entityChunks[e.ID] = chunky.Clone(ch)
				m.entityOrder = append(m.entityOrder, e.ID)
			}
		}
		m.entityLayout = append(m.entityLayout, chunky.Clone(ch))
	}
}

// MarshalBinary replaces the entity blueprint and placed-entity data in a
// preserved source tree, leaving other scenario chunks and entity extensions
// intact.
func (m *Map) MarshalBinary() ([]byte, error) {
	if m == nil || m.sourceRoot == nil || len(m.sourcePreamble) != chunky.PreambleSize {
		return nil, fmt.Errorf("scenario has no original chunky tree")
	}
	root := chunky.Clone(m.sourceRoot)
	meg := root.Child("FOLDSMEG")
	if meg == nil {
		meg = &chunky.Chunk{TypeID: "FOLDSMEG", Version: 1}
		root.Children = append(root.Children, meg)
	}
	blueprints := meg.Child("DATAEBPT")
	if blueprints == nil {
		blueprints = &chunky.Chunk{TypeID: "DATAEBPT", Version: 2000}
		meg.Children = append(meg.Children, blueprints)
	}
	entl := meg.Child("FOLDENTL")
	if entl == nil {
		entl = &chunky.Chunk{TypeID: "FOLDENTL", Version: 1}
		meg.Children = append(meg.Children, entl)
	}
	blueprintsPayload, err := marshalBlueprints(m.Blueprints)
	if err != nil {
		return nil, err
	}
	blueprints.Payload = blueprintsPayload

	entityByID := make(map[int]Entity, len(m.Entities))
	for _, entity := range m.Entities {
		if entity.ID < 0 || uint64(entity.ID) > math.MaxUint32 {
			return nil, fmt.Errorf("entity ID %d is outside the SGB range", entity.ID)
		}
		if entity.BP < 0 || entity.BP >= len(m.Blueprints) || uint64(entity.BP) > math.MaxUint32 {
			return nil, fmt.Errorf("entity %d has invalid blueprint index %d", entity.ID, entity.BP)
		}
		if _, exists := entityByID[entity.ID]; exists {
			return nil, fmt.Errorf("duplicate SGB entity ID %d", entity.ID)
		}
		if entity.Owner < 0 || entity.Owner > 8 {
			return nil, fmt.Errorf("entity %d has invalid owner slot %d", entity.ID, entity.Owner)
		}
		entityByID[entity.ID] = entity
	}
	entityChildren := make([]*chunky.Chunk, 0, len(m.entityLayout)+len(m.Entities))
	saved := make(map[int]bool, len(m.Entities))
	for _, original := range m.entityLayout {
		if original == nil || original.TypeID != "FOLDENTY" {
			entityChildren = append(entityChildren, chunky.Clone(original))
			continue
		}
		originalData := find(original, "DATAENTI")
		if originalData == nil || len(originalData.Payload) < 8 {
			entityChildren = append(entityChildren, chunky.Clone(original))
			continue
		}
		id := int(binary.LittleEndian.Uint32(originalData.Payload[:4]))
		entity, exists := entityByID[id]
		if !exists || saved[id] {
			continue
		}
		chunk := chunky.Clone(m.entityChunks[id])
		if chunk == nil {
			chunk = chunky.Clone(original)
		}
		data := find(chunk, "DATAENTI")
		if data == nil || len(data.Payload) < 56 {
			return nil, fmt.Errorf("entity %d has an invalid DATAENTI chunk", entity.ID)
		}
		data.Payload = marshalEntity(entity, data.Payload)
		entityChildren = append(entityChildren, chunk)
		saved[id] = true
	}
	for _, entity := range m.Entities {
		if saved[entity.ID] {
			continue
		}
		chunk := newEntityChunk()
		find(chunk, "DATAENTI").Payload = marshalEntity(entity, nil)
		entityChildren = append(entityChildren, chunk)
	}
	entl.Children = entityChildren
	if err := updatePlayerLists(meg, m.Entities); err != nil {
		return nil, err
	}

	var output bytes.Buffer
	output.Write(m.sourcePreamble)
	rootWritten := false
	for _, source := range m.sourceChunks {
		if source != nil && source.TypeID == "FOLDSCEN" && !rootWritten {
			output.Write(chunky.Serialize(root))
			rootWritten = true
		} else {
			output.Write(chunky.Serialize(source))
		}
	}
	if !rootWritten {
		return nil, fmt.Errorf("original scenario root is missing")
	}
	output.Write(m.sourceTrailing)
	return output.Bytes(), nil
}

func marshalBlueprints(blueprints []string) ([]byte, error) {
	if uint64(len(blueprints)) > math.MaxInt32 {
		return nil, fmt.Errorf("too many entity blueprints")
	}
	payload := chunky.I32(int32(len(blueprints)))
	for _, blueprint := range blueprints {
		if uint64(len(blueprint)) > math.MaxInt32 {
			return nil, fmt.Errorf("entity blueprint path is too long")
		}
		payload = append(payload, chunky.I32(int32(len(blueprint)))...)
		payload = append(payload, blueprint...)
	}
	return payload, nil
}

func newEntityChunk() *chunky.Chunk {
	return &chunky.Chunk{TypeID: "FOLDENTY", Version: 2000, Children: []*chunky.Chunk{
		{TypeID: "DATAENTI", Version: 2001, Payload: make([]byte, 56)},
		{TypeID: "DATAEBED", Version: 2000, Payload: []byte{0, 0, 0, 0}},
		{TypeID: "DATASEBD", Version: 2000, Payload: []byte{0}},
	}}
}

func marshalEntity(entity Entity, original []byte) []byte {
	payload := make([]byte, 56)
	copy(payload, original)
	binary.LittleEndian.PutUint32(payload[0:], uint32(entity.ID))
	binary.LittleEndian.PutUint32(payload[4:], uint32(entity.BP))
	cosHeading, sinHeading := float32(math.Cos(float64(entity.Heading))), float32(math.Sin(float64(entity.Heading)))
	for off, value := range map[int]float32{
		8: cosHeading, 16: -sinHeading, 24: 1,
		32: sinHeading, 40: cosHeading,
	} {
		binary.LittleEndian.PutUint32(payload[off:], math.Float32bits(value))
	}
	binary.LittleEndian.PutUint32(payload[44:], math.Float32bits(entity.X))
	binary.LittleEndian.PutUint32(payload[48:], math.Float32bits(entity.Y))
	binary.LittleEndian.PutUint32(payload[52:], math.Float32bits(entity.Z))
	return payload
}

func (m *Map) parsePlayerLists(meg *chunky.Chunk) {
	m.playerLists = make(map[int]*chunky.Chunk, 8)
	plls := meg.Child("FOLDPLLS")
	if plls == nil {
		return
	}
	for _, player := range plls.Children {
		if player == nil || player.TypeID != "FOLDPLYR" {
			continue
		}
		info := player.Child("DATAINFO")
		if info == nil || len(info.Payload) < 4 {
			continue
		}
		playerID := int(le32(info.Payload, 0))
		if owner, ok := ownerSlot(playerID); ok {
			m.playerLists[owner] = chunky.Clone(player)
		}
	}
	for i := range m.Entities {
		m.Entities[i].Owner = m.ownerFromLists(m.Entities[i].ID)
	}
}

func ownerSlot(playerID int) (int, bool) {
	if playerID == 0 {
		return 0, true
	}
	if playerID >= 1000 && playerID <= 1007 {
		return playerID - 999, true
	}
	return 0, false
}

func playerIDForOwner(owner int) (int, bool) {
	if owner == 0 {
		return 0, true
	}
	if owner >= 1 && owner <= 8 {
		return owner + 999, true
	}
	return 0, false
}

func (m *Map) ownerFromLists(entityID int) int {
	for owner, player := range m.playerLists {
		if player == nil {
			continue
		}
		entities := player.Child("DATAEGRP")
		if entities == nil || len(entities.Payload) < 4 {
			continue
		}
		count := int(le32(entities.Payload, 0))
		if count < 0 || count > (len(entities.Payload)-4)/4 {
			continue
		}
		for i := 0; i < count; i++ {
			if int(le32(entities.Payload[4+i*4:], 0)) == entityID {
				return owner
			}
		}
	}
	return 0
}

func updatePlayerLists(meg *chunky.Chunk, entities []Entity) error {
	if meg == nil {
		return fmt.Errorf("scenario has no FOLDSMEG chunk")
	}
	plls := meg.Child("FOLDPLLS")
	if plls == nil {
		plls = &chunky.Chunk{TypeID: "FOLDPLLS", Version: 2000}
		meg.Children = append(meg.Children, plls)
	}
	byOwner := make(map[int][]int, 9)
	for _, entity := range entities {
		if _, ok := playerIDForOwner(entity.Owner); !ok {
			return fmt.Errorf("entity %d has invalid owner slot %d", entity.ID, entity.Owner)
		}
		byOwner[entity.Owner] = append(byOwner[entity.Owner], entity.ID)
	}
	players := make([]*chunky.Chunk, 0, len(plls.Children)+8)
	used := make(map[int]bool, 8)
	originalLists := make(map[int]*chunky.Chunk, 8)
	for _, original := range plls.Children {
		if original == nil || original.TypeID != "FOLDPLYR" {
			players = append(players, chunky.Clone(original))
			continue
		}
		info := original.Child("DATAINFO")
		if info == nil || len(info.Payload) < 4 {
			players = append(players, chunky.Clone(original))
			continue
		}
		owner, ok := ownerSlot(int(le32(info.Payload, 0)))
		if !ok || used[owner] {
			continue
		}
		originalLists[owner] = original
		players = append(players, marshalPlayerList(original, owner, byOwner[owner]))
		used[owner] = true
	}
	for owner := 1; owner <= 8; owner++ {
		if used[owner] {
			continue
		}
		if original := originalLists[owner]; original != nil {
			players = append(players, marshalPlayerList(original, owner, byOwner[owner]))
			continue
		}
		playerID, _ := playerIDForOwner(owner)
		players = append(players, newPlayerList(playerID, byOwner[owner]))
	}
	plls.Children = players
	return nil
}

func marshalPlayerList(original *chunky.Chunk, owner int, ids []int) *chunky.Chunk {
	player := chunky.Clone(original)
	info := player.Child("DATAINFO")
	playerID, _ := playerIDForOwner(owner)
	binary.LittleEndian.PutUint32(info.Payload, uint32(playerID))
	entities := player.Child("DATAEGRP")
	if entities == nil {
		entities = &chunky.Chunk{TypeID: "DATAEGRP", Version: 2000, Name: "Entity Group\x00"}
		player.Children = append(player.Children, entities)
	}
	entities.Payload = marshalIDList(ids)
	if squads := player.Child("DATASGRP"); squads == nil {
		player.Children = append(player.Children, &chunky.Chunk{TypeID: "DATASGRP", Version: 2000, Name: "Squad Group\x00", Payload: chunky.I32(0)})
	}
	return player
}

func newPlayerList(playerID int, ids []int) *chunky.Chunk {
	return &chunky.Chunk{TypeID: "FOLDPLYR", Version: 2001, Children: []*chunky.Chunk{
		{TypeID: "DATAINFO", Version: 2000, Payload: append(chunky.I32(int32(playerID)), chunky.I32(0)...)},
		{TypeID: "DATAEGRP", Version: 2000, Name: "Entity Group\x00", Payload: marshalIDList(ids)},
		{TypeID: "DATASGRP", Version: 2000, Name: "Squad Group\x00", Payload: chunky.I32(0)},
	}}
}

func marshalIDList(ids []int) []byte {
	payload := chunky.I32(int32(len(ids)))
	for _, id := range ids {
		payload = append(payload, chunky.I32(int32(id))...)
	}
	return payload
}

// parseEntity decodes one DATAENTI payload: u32 id, u32 blueprint index,
// nine float32 transform values in column order and the x/y/z position.
// The heading follows the reference editor: atan2(-m01, m21) away from the
// poles, atan2(-m20, m00) at them.
func (m *Map) parseEntity(c *chunky.Chunk) *Entity {
	if c == nil || len(c.Payload) < 56 {
		return nil
	}
	p := c.Payload
	e := &Entity{
		ID: int(le32(p, 0)),
		BP: int(le32(p, 4)),
		X:  f32(p[44:]),
		Y:  f32(p[48:]),
		Z:  f32(p[52:]),
	}
	e.Owner = m.ownerFromLists(e.ID)
	if e.BP >= 0 && e.BP < len(m.Blueprints) {
		e.Name = m.Blueprints[e.BP]
	}
	m11 := f32(p[24:])
	if m11 > 0.998 || m11 < -0.998 {
		e.Heading = float32(math.Atan2(-float64(f32(p[16:])), float64(f32(p[8:]))))
	} else {
		e.Heading = float32(math.Atan2(-float64(f32(p[20:])), float64(f32(p[28:]))))
	}
	return e
}

// le32 reads one little-endian uint32 at off, or 0 when the slice is short.
func le32(p []byte, off int) uint32 {
	if off < 0 || off+4 > len(p) {
		return 0
	}
	return binary.LittleEndian.Uint32(p[off:])
}

// f32 decodes one little-endian float32; a short slice reads as zero.
func f32(b []byte) float32 {
	if len(b) < 4 {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}
