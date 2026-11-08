package sgb

import (
	"math"
	"testing"

	"dow-mod-studio/internal/chunky"
)

func saveFixture(t *testing.T) *Map {
	t.Helper()
	root := &chunky.Chunk{TypeID: "FOLDSCEN", Version: 1, Children: []*chunky.Chunk{
		{TypeID: "DATAKEEP", Version: 7, Payload: []byte("preserve this")},
		{TypeID: "FOLDSMEG", Version: 1, Children: []*chunky.Chunk{
			{TypeID: "DATAEBPT", Version: 2000},
			{TypeID: "FOLDENTL", Version: 1},
		}},
	}}
	original := append(chunky.Preamble(), chunky.Serialize(root)...)
	m, err := Parse(original)
	if err != nil {
		t.Fatalf("Parse fixture: %v", err)
	}
	return m
}

func TestMarshalBinaryNewEntityUsesGameCompatibleChunkRecords(t *testing.T) {
	m := saveFixture(t)
	m.Blueprints = []string{"object.rgd"}
	m.Entities = []Entity{{ID: 12, BP: 0, Owner: 1, X: 12.5, Y: 3.25, Z: -9.75, Heading: 0.73}}

	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse marshaled data: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].ID != 12 || got.Entities[0].Owner != 1 {
		t.Fatalf("entity after round trip = %+v", got.Entities)
	}
	entityChunk := got.entityChunks[12]
	if entityChunk == nil {
		t.Fatal("new entity chunk was not retained after parsing")
	}
	for _, chunkID := range []string{"DATAENTI", "DATAEBED", "DATASEBD"} {
		if find(entityChunk, chunkID) == nil {
			t.Errorf("new entity is missing %s", chunkID)
		}
	}
	if got.sourceRoot.Child("FOLDSMEG").Version != 2002 {
		t.Errorf("FOLDSMEG version = %d, want 2002", got.sourceRoot.Child("FOLDSMEG").Version)
	}
	if gotEntity := find(entityChunk, "DATASEBD"); gotEntity == nil || len(gotEntity.Payload) != 1 {
		t.Errorf("new entity DATASEBD = %v, want one-byte record matching existing SGB entities", gotEntity)
	}

	// A second save must retain the newly-created entity record, not append
	// another FOLDENTY with the same ID.
	data, err = got.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary second save: %v", err)
	}
	got, err = Parse(data)
	if err != nil {
		t.Fatalf("Parse second save: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].ID != 12 {
		t.Fatalf("entities after second save = %+v", got.Entities)
	}
}

func TestMarshalBinaryRoundTripsEditedEntityBlueprintAndPreservesChunks(t *testing.T) {
	m := saveFixture(t)
	m.Blueprints = []string{`data:attrib\ebps\environment\rocks\boulder.rgd`}
	m.Entities = []Entity{{ID: 12, BP: 0, Name: m.Blueprints[0], X: 12.5, Y: 3.25, Z: -9.75, Heading: 0.73}}

	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	if !IsSgb(data) {
		t.Fatal("marshaled data is not an SGB")
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse marshaled data: %v", err)
	}
	if len(got.Blueprints) != 1 || got.Blueprints[0] != m.Blueprints[0] {
		t.Fatalf("blueprints after round trip = %q", got.Blueprints)
	}
	if len(got.Entities) != 1 {
		t.Fatalf("got %d entities, want 1", len(got.Entities))
	}
	entity := got.Entities[0]
	if entity.ID != 12 || entity.BP != 0 || entity.Name != m.Blueprints[0] {
		t.Fatalf("entity identity after round trip = %+v", entity)
	}
	if math.Abs(float64(entity.X-m.Entities[0].X)) > 1e-6 || math.Abs(float64(entity.Y-m.Entities[0].Y)) > 1e-6 || math.Abs(float64(entity.Z-m.Entities[0].Z)) > 1e-6 {
		t.Fatalf("entity position after round trip = (%v,%v,%v)", entity.X, entity.Y, entity.Z)
	}
	if math.Abs(float64(entity.Heading-m.Entities[0].Heading)) > 1e-6 {
		t.Fatalf("entity heading after round trip = %v, want %v", entity.Heading, m.Entities[0].Heading)
	}
	if find(got.sourceRoot, "DATAKEEP") == nil || string(find(got.sourceRoot, "DATAKEEP").Payload) != "preserve this" {
		t.Fatal("unrelated scenario chunk was not preserved")
	}
}

func TestMarshalBinaryRetainsOriginalEntityExtensionsAndRemovesDeletedEntity(t *testing.T) {
	originalEntity := Entity{ID: 1, BP: 0, X: 1, Y: 2, Z: 3, Heading: 0.2}
	chunk := newEntityChunk()
	find(chunk, "DATAENTI").Payload = marshalEntity(originalEntity, nil)
	chunk.Children = append(chunk.Children, &chunky.Chunk{TypeID: "DATAEXTR", Version: 9, Payload: []byte{4, 5, 6}})
	root := &chunky.Chunk{TypeID: "FOLDSCEN", Version: 1, Children: []*chunky.Chunk{
		{TypeID: "FOLDSMEG", Version: 1, Children: []*chunky.Chunk{
			{TypeID: "DATAEBPT", Version: 2000, Payload: mustMarshalBlueprints(t, []string{"first.rgd"})},
			{TypeID: "FOLDENTL", Version: 1, Children: []*chunky.Chunk{
				{TypeID: "FOLDMETA", Version: 3, Payload: []byte("list metadata")},
				chunk,
			}},
		}},
	}}
	seed := append(chunky.Preamble(), chunky.Serialize(root)...)
	m, err := Parse(seed)
	if err != nil {
		t.Fatalf("Parse fixture: %v", err)
	}
	m.Blueprints = []string{"first.rgd", "second.rgd"}
	m.Entities = append(m.Entities, Entity{ID: 2, BP: 1, X: -4, Y: 5, Z: 6, Heading: -0.4})

	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse marshaled data: %v", err)
	}
	if len(got.Entities) != 2 || got.Entities[0].ID != 1 || got.Entities[1].ID != 2 {
		t.Fatalf("entities after save = %+v", got.Entities)
	}
	if len(got.entityLayout) == 0 || got.entityLayout[0].TypeID != "FOLDMETA" || string(got.entityLayout[0].Payload) != "list metadata" {
		t.Fatal("unknown FOLDENTL child was not preserved")
	}
	if extra := find(got.entityChunks[1], "DATAEXTR"); extra == nil || len(extra.Payload) != 3 || extra.Payload[0] != 4 || extra.Payload[1] != 5 || extra.Payload[2] != 6 {
		t.Fatal("original entity extension chunk was not preserved")
	}

	got.Entities = got.Entities[1:]
	data, err = got.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary after delete: %v", err)
	}
	got, err = Parse(data)
	if err != nil {
		t.Fatalf("Parse after delete: %v", err)
	}
	if len(got.Entities) != 1 || got.Entities[0].ID != 2 {
		t.Fatalf("entities after delete = %+v, want only ID 2", got.Entities)
	}
}

func TestMarshalBinaryRoundTripsEntityOwners(t *testing.T) {
	m := saveFixture(t)
	m.Blueprints = []string{"object.rgd"}
	m.Entities = []Entity{
		{ID: 10, BP: 0, Owner: 0},
		{ID: 11, BP: 0, Owner: 1},
		{ID: 18, BP: 0, Owner: 8},
	}

	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse marshaled data: %v", err)
	}
	owners := make(map[int]int, len(got.Entities))
	for _, entity := range got.Entities {
		owners[entity.ID] = entity.Owner
	}
	for id, want := range map[int]int{10: 0, 11: 1, 18: 8} {
		if owners[id] != want {
			t.Errorf("entity %d owner = %d, want %d", id, owners[id], want)
		}
	}
}

// A squad still naming a deleted entity makes the engine reject the whole map
// ("Invalid squad entity id"), so saving has to drop it from both the squad
// list and the player group that owns it.
func TestMarshalBinaryDropsSquadsWhoseEntitiesAreGone(t *testing.T) {
	squadDef := func(id, entity int) *chunky.Chunk {
		return &chunky.Chunk{TypeID: "FOLDSQDD", Version: 2000, Children: []*chunky.Chunk{
			{TypeID: "DATASQDI", Version: 2000, Payload: append(chunky.I32(int32(id)), chunky.I32(0)...)},
			{TypeID: "DATASQDE", Version: 2000, Payload: marshalIDList([]int{entity})},
		}}
	}
	entityChunk := func(id int) *chunky.Chunk {
		chunk := newEntityChunk()
		find(chunk, "DATAENTI").Payload = marshalEntity(Entity{ID: id, BP: 0}, nil)
		return chunk
	}
	root := &chunky.Chunk{TypeID: "FOLDSCEN", Version: 1, Children: []*chunky.Chunk{
		{TypeID: "FOLDSMEG", Version: 2002, Children: []*chunky.Chunk{
			{TypeID: "DATAEBPT", Version: 2000, Payload: mustMarshalBlueprints(t, []string{"object.rgd"})},
			{TypeID: "FOLDENTL", Version: 2000, Children: []*chunky.Chunk{entityChunk(1), entityChunk(2)}},
			{TypeID: "FOLDSQDL", Version: 2000, Children: []*chunky.Chunk{squadDef(50000, 1), squadDef(50001, 2)}},
			{TypeID: "FOLDPLLS", Version: 2000, Children: []*chunky.Chunk{
				{TypeID: "FOLDPLYR", Version: 2001, Children: []*chunky.Chunk{
					{TypeID: "DATAINFO", Version: 2000, Payload: append(chunky.I32(1000), chunky.I32(0)...)},
					{TypeID: "DATAEGRP", Version: 2000, Name: "Entity Group\x00", Payload: marshalIDList([]int{1, 2})},
					{TypeID: "DATASGRP", Version: 2000, Name: "Squad Group\x00", Payload: marshalIDList([]int{50000, 50001})},
				}},
			}},
		}},
	}}
	m, err := Parse(append(chunky.Preamble(), chunky.Serialize(root)...))
	if err != nil {
		t.Fatalf("Parse fixture: %v", err)
	}
	m.Entities = m.Entities[:1]
	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse marshaled data: %v", err)
	}
	squads := find(got.sourceRoot, "FOLDSQDL")
	if squads == nil || len(squads.Children) != 1 {
		t.Fatalf("squad list after deleting entity 2 = %+v, want only squad 50000", squads)
	}
	if info := find(squads.Children[0], "DATASQDI"); info == nil || le32(info.Payload, 0) != 50000 {
		t.Fatalf("surviving squad id = %v, want 50000", info)
	}
	player := find(got.sourceRoot, "FOLDPLYR")
	group := find(player, "DATASGRP")
	if want := marshalIDList([]int{50000}); group == nil || string(group.Payload) != string(want) {
		t.Fatalf("player squad group = %v, want %v", group, want)
	}
}

// A race object placed in a scenario is a squad of one entity in the reference
// editor, not a bare entity. A save has to write the squad as well — the squad
// blueprint table names it, the squad definition names the entity, and the
// player's squad group names the squad — or the game sees a unit blueprint with
// nothing fielding it.
func TestMarshalBinaryCreatesSquadForPlacedObject(t *testing.T) {
	squadTable := func(names ...string) []byte {
		payload := chunky.I32(int32(len(names)))
		for _, name := range names {
			payload = append(payload, chunky.I32(int32(len(name)))...)
			payload = append(payload, name...)
		}
		return payload
	}
	entityChunk := func(id int) *chunky.Chunk {
		chunk := newEntityChunk()
		find(chunk, "DATAENTI").Payload = marshalEntity(Entity{ID: id, BP: 0}, nil)
		return chunk
	}
	root := &chunky.Chunk{TypeID: "FOLDSCEN", Version: 1, Children: []*chunky.Chunk{
		{TypeID: "FOLDSMEG", Version: 2002, Children: []*chunky.Chunk{
			{TypeID: "DATASBPT", Version: 2000, Payload: squadTable("existing_squad")},
			{TypeID: "DATAEBPT", Version: 2000, Payload: mustMarshalBlueprints(t, []string{"object.rgd"})},
			{TypeID: "FOLDENTL", Version: 2000, Children: []*chunky.Chunk{entityChunk(1), entityChunk(2)}},
			{TypeID: "FOLDSQDL", Version: 2000, Children: []*chunky.Chunk{
				{TypeID: "FOLDSQDD", Version: 2000, Children: []*chunky.Chunk{
					{TypeID: "DATASQDI", Version: 2000, Payload: append(chunky.I32(50000), chunky.I32(0)...)},
					{TypeID: "DATASQDE", Version: 2000, Payload: marshalIDList([]int{1})},
				}},
			}},
			{TypeID: "FOLDPLLS", Version: 2000, Children: []*chunky.Chunk{
				{TypeID: "FOLDPLYR", Version: 2001, Children: []*chunky.Chunk{
					{TypeID: "DATAINFO", Version: 2000, Payload: append(chunky.I32(1000), chunky.I32(0)...)},
					{TypeID: "DATAEGRP", Version: 2000, Name: "Entity Group\x00", Payload: marshalIDList([]int{1})},
					{TypeID: "DATASGRP", Version: 2000, Name: "Squad Group\x00", Payload: marshalIDList([]int{50000})},
				}},
			}},
		}},
	}}
	m, err := Parse(append(chunky.Preamble(), chunky.Serialize(root)...))
	if err != nil {
		t.Fatalf("Parse fixture: %v", err)
	}
	// The editor's edit: the object is placed for player 1, with the squad
	// blueprint the Objects window read from the sbps files.
	m.Entities = append(m.Entities[:1], Entity{ID: 3, BP: 0, Owner: 1, X: 4, Y: 5, Z: 6, Squad: "object_squad"})
	data, err := m.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse marshaled data: %v", err)
	}
	squads := find(got.sourceRoot, "FOLDSQDL")
	if squads == nil || len(squads.Children) != 2 {
		t.Fatalf("squad list after placing the object = %+v, want the existing squad and a new one", squads)
	}
	var placed *chunky.Chunk
	for _, def := range squads.Children {
		if info := find(def, "DATASQDI"); info != nil && le32(info.Payload, 0) == 50001 {
			placed = def
		}
	}
	if placed == nil {
		t.Fatal("no squad was created for the placed object")
	}
	if info := find(placed, "DATASQDI"); le32(info.Payload, 4) != 1 {
		t.Fatalf("new squad blueprint index = %d, want 1 (the added DATASBPT entry)", le32(info.Payload, 4))
	}
	list := find(placed, "DATASQDE")
	if want := marshalIDList([]int{3}); list == nil || string(list.Payload) != string(want) {
		t.Fatalf("new squad entities = %v, want %v (the placed object)", list, want)
	}
	names := decodeStringTable(find(got.sourceRoot, "DATASBPT").Payload)
	if len(names) != 2 || names[0] != "existing_squad" || names[1] != "object_squad" {
		t.Fatalf("squad blueprint table after placing = %v, want the existing entry and object_squad", names)
	}
	group := find(find(got.sourceRoot, "FOLDPLYR"), "DATASGRP")
	if want := marshalIDList([]int{50000, 50001}); group == nil || string(group.Payload) != string(want) {
		t.Fatalf("player squad group = %v, want %v", group, want)
	}
}

func mustMarshalBlueprints(t *testing.T, blueprints []string) []byte {
	t.Helper()
	payload, err := marshalBlueprints(blueprints)
	if err != nil {
		t.Fatalf("marshal blueprints: %v", err)
	}
	return payload
}

func TestMarshalBinaryRejectsInvalidEntityData(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Map)
	}{
		{"duplicate id", func(m *Map) { m.Entities = []Entity{{ID: 1, BP: 0}, {ID: 1, BP: 0}} }},
		{"invalid blueprint", func(m *Map) { m.Entities = []Entity{{ID: 1, BP: 1}} }},
		{"negative id", func(m *Map) { m.Entities = []Entity{{ID: -1, BP: 0}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := saveFixture(t)
			m.Blueprints = []string{"object.rgd"}
			tc.edit(m)
			if _, err := m.MarshalBinary(); err == nil {
				t.Fatal("MarshalBinary succeeded for invalid entity state")
			}
		})
	}
}
