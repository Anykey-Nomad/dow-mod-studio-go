package whe

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"dow-mod-studio/internal/chunky"
)

func selectionTestWhe(children ...*chunky.Chunk) []byte {
	var out bytes.Buffer
	out.Write(chunky.Preamble())
	out.Write(chunky.Serialize(&chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: children}))
	return out.Bytes()
}

func TestSelectedUIParseEditRoundTrip(t *testing.T) {
	want := FxSelectedUI{
		DisplayType: 1,
		Scale:       [2]float32{2.5, 3.5}, Offset: [2]float32{4.5, 5.5},
		VolumeOffset: [3]float32{6.5, 7.5, 8.5}, VolumeScale: [3]float32{9.5, 10.5, 11.5},
		Matrix:   [3][3]float32{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
		trailing: []byte{0xA1, 0xB2},
	}
	payload := marshalSelectedUIForTest(t, want)
	doc, err := ParseDocument(selectionTestWhe(&chunky.Chunk{TypeID: "DATASEUI", Version: 3, Payload: payload}))
	if err != nil {
		t.Fatal(err)
	}
	got, present, err := doc.SelectedUI()
	if err != nil || !present {
		t.Fatalf("SelectedUI() = (%+v, %v, %v)", got, present, err)
	}
	if got.DisplayType != want.DisplayType || got.Scale != want.Scale || got.Offset != want.Offset || got.VolumeOffset != want.VolumeOffset || got.VolumeScale != want.VolumeScale || got.Matrix != want.Matrix || !bytes.Equal(got.trailing, want.trailing) {
		t.Fatalf("SelectedUI() = %+v, want %+v", got, want)
	}
	got.VolumeScale[1] = 12.5
	if err := doc.SetSelectedUI(got); err != nil {
		t.Fatal(err)
	}
	out, err := doc.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := ParseDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	after, present, err := reparsed.SelectedUI()
	if err != nil || !present {
		t.Fatalf("reparse SelectedUI: present=%v err=%v", present, err)
	}
	if after.VolumeScale[1] != 12.5 || !bytes.Equal(after.trailing, want.trailing) {
		t.Fatalf("edited UI = %+v", after)
	}
}

func TestSelectedUIFindsOnlySupportedContainerChunk(t *testing.T) {
	payload := marshalSelectedUIForTest(t, FxSelectedUI{DisplayType: 1})
	doc, err := ParseDocument(selectionTestWhe(
		&chunky.Chunk{TypeID: "FOLDMODL", Version: 1, Children: []*chunky.Chunk{{TypeID: "DATASEUI", Version: 3, Payload: payload}}},
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, present, err := doc.SelectedUI(); err != nil || present {
		t.Fatalf("DATASEUI under FOLDMODL should not be treated as Selected UI: present=%v err=%v", present, err)
	}
}

func TestSetSelectedUICreatesChunk(t *testing.T) {
	doc, err := ParseDocument(selectionTestWhe())
	if err != nil {
		t.Fatal(err)
	}
	value := FxSelectedUI{DisplayType: 0, Scale: [2]float32{1, 1}, VolumeScale: [3]float32{1, 1, 1}}
	for i := range value.Matrix {
		value.Matrix[i][i] = 1
	}
	if err := doc.SetSelectedUI(value); err != nil {
		t.Fatal(err)
	}
	got, present, err := doc.SelectedUI()
	if err != nil || !present || got.DisplayType != value.DisplayType || got.Scale != value.Scale || got.Offset != value.Offset || got.VolumeOffset != value.VolumeOffset || got.VolumeScale != value.VolumeScale || got.Matrix != value.Matrix {
		t.Fatalf("created DATASEUI = (%+v, %v, %v), want (%+v, true, nil)", got, present, err, value)
	}
}

func TestModifierParseEditRoundTrip(t *testing.T) {
	payload := modifierPayloadForTest(t, "health", "Health", 1, 0.25, 0.5, []byte{0xCC})
	doc, err := ParseDocument(selectionTestWhe(&chunky.Chunk{TypeID: "FOLDMODL", Version: 1, Children: []*chunky.Chunk{{TypeID: "DATAMODF", Version: 1, Name: "speed", Payload: payload}}}))
	if err != nil {
		t.Fatal(err)
	}
	// A similarly named chunk outside FOLDMODL must not masquerade as a
	// modifier definition.
	doc, err = ParseDocument(selectionTestWhe(
		&chunky.Chunk{TypeID: "FOLDMODL", Version: 1, Children: []*chunky.Chunk{{TypeID: "DATAMODF", Version: 1, Name: "speed", Payload: payload}}},
		&chunky.Chunk{TypeID: "DATAMODF", Version: 1, Name: "unrelated", Payload: payload},
	))
	if err != nil {
		t.Fatal(err)
	}
	mods := doc.Modifiers()
	if len(mods) != 1 {
		t.Fatalf("Modifiers() has %d items", len(mods))
	}
	m := mods[0]
	if m.Variable != "Health" || m.Type != 1 || m.Reference != 25 || m.Default != 50 || !bytes.Equal(m.trailing, []byte{0xCC}) {
		t.Fatalf("modifier = %+v", m)
	}
	m.Variable, m.Type, m.Reference, m.Default = "MoveSpeed", 0, 125, 75
	if err := doc.SetModifier("speed", m); err != nil {
		t.Fatal(err)
	}
	out, err := doc.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := ParseDocument(out)
	if err != nil {
		t.Fatal(err)
	}
	got := reparsed.Modifiers()[0]
	if got.Variable != "MoveSpeed" || got.Type != 0 || got.Reference != 125 || got.Default != 75 || !bytes.Equal(got.trailing, []byte{0xCC}) {
		t.Fatalf("edited modifier = %+v", got)
	}
}

func TestSetSelectedUIRejectsNonFiniteValues(t *testing.T) {
	doc, err := ParseDocument(selectionTestWhe(&chunky.Chunk{TypeID: "DATASEUI", Version: 3, Payload: marshalSelectedUIForTest(t, FxSelectedUI{})}))
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := doc.SelectedUI()
	if err != nil {
		t.Fatal(err)
	}
	value.Scale[0] = float32(math.Inf(1))
	if err := doc.SetSelectedUI(value); err == nil {
		t.Fatal("accepted infinite selection UI value")
	}
}

func marshalSelectedUIForTest(t *testing.T, v FxSelectedUI) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, v.DisplayType)
	for _, pair := range [][2]float32{v.Scale, v.Offset} {
		for _, n := range pair {
			_ = binary.Write(&b, binary.LittleEndian, n)
		}
	}
	for _, triple := range [][3]float32{v.VolumeOffset, v.VolumeScale} {
		for _, n := range triple {
			_ = binary.Write(&b, binary.LittleEndian, n)
		}
	}
	for _, row := range v.Matrix {
		for _, n := range row {
			_ = binary.Write(&b, binary.LittleEndian, n)
		}
	}
	b.Write(v.trailing)
	return b.Bytes()
}

func modifierPayloadForTest(t *testing.T, _ string, variable string, kind int32, reference, def float32, trailing []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(len(variable)))
	b.WriteString(variable)
	_ = binary.Write(&b, binary.LittleEndian, kind)
	_ = binary.Write(&b, binary.LittleEndian, reference)
	_ = binary.Write(&b, binary.LittleEndian, def)
	b.Write(trailing)
	return b.Bytes()
}
