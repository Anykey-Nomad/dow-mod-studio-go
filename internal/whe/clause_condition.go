package whe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"dow-mod-studio/internal/chunky"
)

const (
	ClauseFloat   int32 = 0
	ClauseBoolean int32 = 1
	ClauseString  int32 = 2
	clauseRawBool int32 = 3 // internal-only discriminator for unknown future clause types
)

const (
	ClauseEqual int32 = iota
	ClauseNotEqual
	ClauseLess
	ClauseLessOrEqual
	ClauseGreater
	ClauseGreaterOrEqual
)

// FxClause is one named DATACLAS definition. Numeric values are exposed in
// the editor's units (the on-disk floats multiplied by 100). Exactly the
// value fields selected by Type are meaningful.
type FxClause struct {
	Name          string
	Type          int32
	RawType       uint32 // preserves an unknown future type while editing common fields
	Variable      string
	Comparison    int32
	FloatValue    float32
	FloatDefault  float32
	BoolValue     bool
	BoolDefault   bool
	StringValue   string
	StringDefault string
	trailing      []byte
}

func ClauseTypeName(kind int32) string {
	if kind == clauseRawBool {
		return "unknown type"
	}
	switch kind {
	case ClauseFloat:
		return "float"
	case ClauseBoolean:
		return "boolean"
	case ClauseString:
		return "string"
	default:
		return fmt.Sprintf("unknown (%d)", kind)
	}
}

func ClauseComparisonName(comparison int32) string {
	switch comparison {
	case ClauseEqual:
		return "equal"
	case ClauseNotEqual:
		return "not equal"
	case ClauseLess:
		return "less"
	case ClauseLessOrEqual:
		return "less or equal"
	case ClauseGreater:
		return "greater"
	case ClauseGreaterOrEqual:
		return "greater or equal"
	default:
		return fmt.Sprintf("unknown (%d)", comparison)
	}
}

// Clauses returns DATACLAS definitions in FOLDCLST folder order. Malformed
// chunks are skipped; they remain untouched in the document and on save.
func (d *Document) Clauses() []FxClause {
	var out []FxClause
	if d == nil {
		return nil
	}
	walkRebpFolders(d.chunks, "FOLDCLST", func(folder *chunky.Chunk) {
		for _, c := range folder.Children {
			if c.TypeID != "DATACLAS" {
				continue
			}
			if clause, err := parseClause(c.Name, c.Payload); err == nil {
				out = append(out, clause)
			}
		}
	})
	return out
}

// SetClause updates every same-named DATACLAS under FOLDCLST, preserving
// trailing bytes from the parsed definition.
func (d *Document) SetClause(name string, value FxClause) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	payload, err := marshalClause(value)
	if err != nil {
		return err
	}
	written := false
	walkRebpFolders(d.chunks, "FOLDCLST", func(folder *chunky.Chunk) {
		for _, c := range folder.Children {
			if c.TypeID == "DATACLAS" && strings.EqualFold(c.Name, name) {
				c.Payload = append([]byte(nil), payload...)
				written = true
			}
		}
	})
	if !written {
		return fmt.Errorf("clause %q was not found", name)
	}
	return nil
}

func parseClause(name string, payload []byte) (FxClause, error) {
	r := &chunky.Reader{Data: payload}
	c := FxClause{Name: strings.TrimRight(name, "\x00"), RawType: uint32(r.I32())}
	c.Type = int32(c.RawType)
	c.Variable = r.Str()
	c.Comparison = r.I32()
	switch c.Type {
	case ClauseFloat:
		c.FloatValue = r.F32() * 100
		c.FloatDefault = r.F32() * 100
	case ClauseBoolean:
		c.BoolValue = r.I8() != 0
		c.BoolDefault = r.I8() != 0
	case ClauseString:
		c.StringValue = r.Str()
		c.StringDefault = r.Str()
	default:
		// Unknown types are preserved byte-for-byte as an opaque type-specific
		// tail; the editor can still show and edit the shared metadata.
		c.trailing = append([]byte(nil), payload[r.Off:]...)
		c.Type = clauseRawBool
		return c, nil
	}
	if r.Err != nil {
		return FxClause{}, fmt.Errorf("clause %q: %w", name, r.Err)
	}
	c.trailing = append([]byte(nil), payload[r.Off:]...)
	return c, nil
}

func marshalClause(c FxClause) ([]byte, error) {
	if c.Type < ClauseFloat || c.Type > ClauseString {
		if c.Type != clauseRawBool || c.RawType <= uint32(ClauseString) {
			return nil, fmt.Errorf("clause type must be 0 (float), 1 (boolean), or 2 (string)")
		}
	}
	var b bytes.Buffer
	putI32 := func(v int32) { _ = binary.Write(&b, binary.LittleEndian, v) }
	putString := func(s string) error {
		if len(s) > math.MaxInt32 {
			return fmt.Errorf("clause string is too long")
		}
		putI32(int32(len(s)))
		_, _ = b.WriteString(s)
		return nil
	}
	if c.Type == clauseRawBool {
		putI32(int32(c.RawType))
	} else {
		putI32(c.Type)
	}
	if c.Type == clauseRawBool {
		// The internal sentinel is not a real type; RawType is the original
		// code. Preserve its payload opaquely after the shared header.
		if err := putString(c.Variable); err != nil {
			return nil, err
		}
		putI32(c.Comparison)
		b.Write(c.trailing)
		return b.Bytes(), nil
	}
	if err := putString(c.Variable); err != nil {
		return nil, err
	}
	putI32(c.Comparison)
	switch c.Type {
	case ClauseFloat:
		if !finiteFloat32(c.FloatValue) || !finiteFloat32(c.FloatDefault) {
			return nil, fmt.Errorf("clause float values must be finite")
		}
		_ = binary.Write(&b, binary.LittleEndian, c.FloatValue/100)
		_ = binary.Write(&b, binary.LittleEndian, c.FloatDefault/100)
	case ClauseBoolean:
		if c.BoolValue {
			b.WriteByte(1)
		} else {
			b.WriteByte(0)
		}
		if c.BoolDefault {
			b.WriteByte(1)
		} else {
			b.WriteByte(0)
		}
	case ClauseString:
		if err := putString(c.StringValue); err != nil {
			return nil, err
		}
		if err := putString(c.StringDefault); err != nil {
			return nil, err
		}
	case clauseRawBool:
		b.Write(c.trailing)
		return b.Bytes(), nil
	}
	if c.RawType == uint32(c.Type) {
		b.Write(c.trailing)
	}
	return b.Bytes(), nil
}

// FxCondition is a named DATACOND whose ordered ClauseNames refer to named
// DATACLAS chunks. Unknown names are retained because the source may be
// incomplete or refer to engine-provided definitions.
type FxCondition struct {
	Name        string
	ClauseNames []string
	trailing    []byte
}

// Conditions returns named conditions in FOLDCONL folder order.
func (d *Document) Conditions() []FxCondition {
	var out []FxCondition
	if d == nil {
		return nil
	}
	walkRebpFolders(d.chunks, "FOLDCONL", func(folder *chunky.Chunk) {
		for _, c := range folder.Children {
			if c.TypeID != "DATACOND" {
				continue
			}
			if condition, err := parseCondition(c.Name, c.Payload); err == nil {
				out = append(out, condition)
			}
		}
	})
	return out
}

// SetCondition replaces the clause-name references of every same-named
// DATACOND under FOLDCONL, keeping the chunk's unrecognized payload tail.
func (d *Document) SetCondition(name string, value FxCondition) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	payload, err := marshalCondition(value)
	if err != nil {
		return err
	}
	written := false
	walkRebpFolders(d.chunks, "FOLDCONL", func(folder *chunky.Chunk) {
		for _, c := range folder.Children {
			if c.TypeID == "DATACOND" && strings.EqualFold(c.Name, name) {
				c.Payload = append([]byte(nil), payload...)
				written = true
			}
		}
	})
	if !written {
		return fmt.Errorf("condition %q was not found", name)
	}
	return nil
}

func parseCondition(name string, payload []byte) (FxCondition, error) {
	r := &chunky.Reader{Data: payload}
	condition := FxCondition{Name: strings.TrimRight(name, "\x00")}
	n := r.Count(4)
	for i := 0; i < n && r.Err == nil; i++ {
		condition.ClauseNames = append(condition.ClauseNames, strings.TrimRight(r.Str(), "\x00"))
	}
	if r.Err != nil {
		return FxCondition{}, fmt.Errorf("condition %q: %w", name, r.Err)
	}
	condition.trailing = append([]byte(nil), payload[r.Off:]...)
	return condition, nil
}

func marshalCondition(condition FxCondition) ([]byte, error) {
	if len(condition.ClauseNames) > math.MaxInt32 {
		return nil, fmt.Errorf("condition has too many clause references")
	}
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(len(condition.ClauseNames)))
	for _, name := range condition.ClauseNames {
		if len(name) > math.MaxInt32 {
			return nil, fmt.Errorf("clause name is too long")
		}
		_ = binary.Write(&b, binary.LittleEndian, int32(len(name)))
		_, _ = b.WriteString(name)
	}
	b.Write(condition.trailing)
	return b.Bytes(), nil
}

func walkRebpFolders(chunks []*chunky.Chunk, typeID string, fn func(*chunky.Chunk)) {
	for _, root := range chunks {
		if root.TypeID != "FOLDREBP" {
			continue
		}
		for _, child := range root.Children {
			if child.TypeID == typeID {
				fn(child)
			}
		}
	}
}
