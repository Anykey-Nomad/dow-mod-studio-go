package main

import "dow-mod-studio/internal/rgd"
import "testing"

func TestRgdScalarEditingAndRoundTrip(t *testing.T) {
	root := &rgd.RgdEntry{Hash: 0, Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
		{Hash: 1, Name: "float_value", Type: rgd.RgdTypeFloat, FloatVal: 1},
		{Hash: 2, Name: "int_value", Type: rgd.RgdTypeInt, IntVal: 2},
		{Hash: 3, Name: "bool_value", Type: rgd.RgdTypeBool, BoolVal: false},
		{Hash: 4, Name: "string_value", Type: rgd.RgdTypeString, StrVal: "old"},
		{Hash: 5, Name: "wstring_value", Type: rgd.RgdTypeWString, WStrVal: "old"},
	}}
	changes := map[uint32]string{
		1: "3.25",
		2: "0x2a",
		3: "true",
		4: "new string",
		5: "Новая строка",
	}
	for _, entry := range root.Children {
		if _, err := rgdApplyScalarText(entry, changes[entry.Hash]); err != nil {
			t.Fatalf("apply %s: %v", entry.Name, err)
		}
	}

	data, err := (&rgd.RgdFile{Root: root, HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1}).MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("parse round-trip: %v", err)
	}
	got := make(map[uint32]*rgd.RgdEntry)
	for _, entry := range parsed.Root.Children {
		got[entry.Hash] = entry
	}
	if got[1].FloatVal != 3.25 || got[2].IntVal != 42 || !got[3].BoolVal || got[4].StrVal != "new string" || got[5].WStrVal != "Новая строка" {
		t.Fatalf("round-trip values = %#v", got)
	}
}

func TestRgdAliasDisplayName(t *testing.T) {
	dict := map[uint32]string{rgd.Hash("70506"): "hit_entity_event_name"}
	if got := rgd.AliasDisplayName("$70506", dict); got != "hit_entity_event_name" {
		t.Fatalf("alias display = %q, want %q", got, "hit_entity_event_name")
	}
	if got := rgd.AliasDisplayName("$missing", dict); got != "" {
		t.Fatalf("unknown alias display = %q, want empty", got)
	}
}

func TestRgdDisplayFieldValue(t *testing.T) {
	alias := &rgd.RgdEntry{Name: "hit_entity_event_name", Type: rgd.RgdTypeString, StrVal: "$70506"}
	if got := alias.DisplayFieldValue(); got != alias.Name {
		t.Fatalf("alias display = %q, want field name %q", got, alias.Name)
	}
	if alias.StrVal != "$70506" {
		t.Fatalf("display changed stored alias to %q", alias.StrVal)
	}
	plain := &rgd.RgdEntry{Name: "description", Type: rgd.RgdTypeString, StrVal: "hello"}
	if got := plain.DisplayFieldValue(); got != "hello" {
		t.Fatalf("plain display = %q, want %q", got, "hello")
	}
}

func TestRgdScalarEditingRejectsInvalidValues(t *testing.T) {
	entry := &rgd.RgdEntry{Type: rgd.RgdTypeInt, IntVal: 7}
	if _, err := rgdApplyScalarText(entry, "not-a-number"); err == nil {
		t.Fatal("invalid integer was accepted")
	}
	if entry.IntVal != 7 {
		t.Fatalf("invalid edit changed value to %d", entry.IntVal)
	}
}
