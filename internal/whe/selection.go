package whe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"dow-mod-studio/internal/chunky"
)

// FxModifier is a named FOLDMODL/DATAMODF definition. Reference and Default
// are exposed in the editor's units (the serialized values multiplied by 100).
type FxModifier struct {
	Name      string
	Variable  string
	Type      int32
	Reference float32
	Default   float32
	trailing  []byte
}

// ModifierTypeName gives the known DATAMODF type a readable label while
// retaining unknown numeric values for forward-compatible inspection.
func ModifierTypeName(kind int32) string {
	switch kind {
	case 0:
		return "absolute time"
	case 1:
		return "speed scale"
	default:
		return fmt.Sprintf("unknown (%d)", kind)
	}
}

// Modifiers reads every named modifier definition in file order.
func (d *Document) Modifiers() []FxModifier {
	var out []FxModifier
	if d == nil {
		return nil
	}
	var visit func([]*chunky.Chunk)
	visit = func(chunks []*chunky.Chunk) {
		for _, c := range chunks {
			if c.TypeID == "FOLDMODL" {
				for _, child := range c.Children {
					if child.TypeID != "DATAMODF" {
						continue
					}
					if m, err := parseModifier(child.Name, child.Payload); err == nil {
						out = append(out, m)
					}
				}
			}
			visit(c.Children)
		}
	}
	visit(d.chunks)
	return out
}

// SetModifier updates every DATAMODF with the named definition, preserving
// unknown bytes after the fields understood by this format version.
func (d *Document) SetModifier(name string, value FxModifier) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	if !finiteFloat32(value.Reference) || !finiteFloat32(value.Default) {
		return fmt.Errorf("modifier values must be finite")
	}
	payload, err := marshalModifier(value)
	if err != nil {
		return err
	}
	written := false
	var visit func([]*chunky.Chunk)
	visit = func(chunks []*chunky.Chunk) {
		for _, c := range chunks {
			if c.TypeID == "FOLDMODL" {
				for _, child := range c.Children {
					if child.TypeID == "DATAMODF" && strings.EqualFold(strings.TrimRight(child.Name, "\x00"), name) {
						child.Payload = append([]byte(nil), payload...)
						written = true
					}
				}
			}
			visit(c.Children)
		}
	}
	visit(d.chunks)
	if !written {
		return fmt.Errorf("modifier %q was not found", name)
	}
	return nil
}

func marshalModifier(m FxModifier) ([]byte, error) {
	if !finiteFloat32(m.Reference) || !finiteFloat32(m.Default) {
		return nil, fmt.Errorf("modifier values must be finite")
	}
	var b bytes.Buffer
	if err := binary.Write(&b, binary.LittleEndian, int32(len(m.Variable))); err != nil {
		return nil, err
	}
	b.WriteString(m.Variable)
	for _, value := range []any{m.Type, m.Reference / 100, m.Default / 100} {
		if err := binary.Write(&b, binary.LittleEndian, value); err != nil {
			return nil, err
		}
	}
	b.Write(m.trailing)
	return b.Bytes(), nil
}

func finiteFloat32(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func parseModifier(name string, payload []byte) (FxModifier, error) {
	r := &chunky.Reader{Data: payload}
	m := FxModifier{Name: strings.TrimRight(name, "\x00"), Variable: r.Str()}
	m.Type = r.I32()
	m.Reference = r.F32() * 100
	m.Default = r.F32() * 100
	if r.Err != nil {
		return FxModifier{}, fmt.Errorf("modifier %q: %w", name, r.Err)
	}
	m.trailing = append([]byte(nil), payload[r.Off:]...)
	return m, nil
}

// FxSelectedUI is the DATASEUI payload: indicator settings, selection-volume
// settings and the 3x3 transform matrix. The fields retain the on-disk axes.
type FxSelectedUI struct {
	DisplayType  int32
	Scale        [2]float32
	Offset       [2]float32
	VolumeOffset [3]float32
	VolumeScale  [3]float32
	Matrix       [3][3]float32
	trailing     []byte
}

const selectedUIPayloadSize = 80

// SelectedUI reads the DATASEUI chunk. present is false when the document has
// no such chunk; malformed payloads are returned as errors instead of being
// silently displayed as default values.
func (d *Document) SelectedUI() (value FxSelectedUI, present bool, err error) {
	if d == nil {
		return value, false, nil
	}
	var chunk *chunky.Chunk
	for _, root := range d.chunks {
		if root.TypeID == "FOLDREBP" {
			chunk = root.Child("DATASEUI")
			break
		}
	}
	if chunk == nil {
		return value, false, nil
	}
	value, err = parseSelectedUI(chunk.Payload)
	return value, true, err
}

func parseSelectedUI(payload []byte) (FxSelectedUI, error) {
	if len(payload) < selectedUIPayloadSize {
		return FxSelectedUI{}, fmt.Errorf("DATASEUI payload is %d bytes; need at least %d", len(payload), selectedUIPayloadSize)
	}
	r := &chunky.Reader{Data: payload}
	v := FxSelectedUI{DisplayType: r.I32()}
	for i := range v.Scale {
		v.Scale[i] = r.F32()
	}
	for i := range v.Offset {
		v.Offset[i] = r.F32()
	}
	for i := range v.VolumeOffset {
		v.VolumeOffset[i] = r.F32()
	}
	for i := range v.VolumeScale {
		v.VolumeScale[i] = r.F32()
	}
	for row := range v.Matrix {
		for col := range v.Matrix[row] {
			v.Matrix[row][col] = r.F32()
		}
	}
	if r.Err != nil {
		return FxSelectedUI{}, r.Err
	}
	v.trailing = append([]byte(nil), payload[r.Off:]...)
	return v, nil
}

// SetSelectedUI replaces the DATASEUI payload without disturbing the rest of
// the WHE. If absent, it creates the chunk under FOLDREBP.
func (d *Document) SetSelectedUI(value FxSelectedUI) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	payload, err := marshalSelectedUI(value)
	if err != nil {
		return err
	}
	var target *chunky.Chunk
	var root *chunky.Chunk
	for _, c := range d.chunks {
		if c.TypeID == "FOLDREBP" {
			root = c
			target = c.Child("DATASEUI")
			break
		}
	}
	if target == nil {
		if root == nil {
			return fmt.Errorf(".whe has no FOLDREBP folder")
		}
		target = &chunky.Chunk{TypeID: "DATASEUI", Version: 3}
		root.Children = append(root.Children, target)
	}
	target.Payload = payload
	return nil
}

func marshalSelectedUI(value FxSelectedUI) ([]byte, error) {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, value.DisplayType)
	putF32 := func(n float32) error {
		if !finiteFloat32(n) {
			return fmt.Errorf("DATASEUI values must be finite")
		}
		return binary.Write(&b, binary.LittleEndian, n)
	}
	for _, pair := range [][2]float32{value.Scale, value.Offset} {
		for _, n := range pair {
			if err := putF32(n); err != nil {
				return nil, err
			}
		}
	}
	for _, triple := range [][3]float32{value.VolumeOffset, value.VolumeScale} {
		for _, n := range triple {
			if err := putF32(n); err != nil {
				return nil, err
			}
		}
	}
	for _, row := range value.Matrix {
		for _, n := range row {
			if err := putF32(n); err != nil {
				return nil, err
			}
		}
	}
	b.Write(value.trailing)
	return b.Bytes(), nil
}
