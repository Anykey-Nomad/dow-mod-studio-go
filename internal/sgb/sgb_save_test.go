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
