package whe

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"fmt"
	"strings"
)

// A .ebp is the same object as a .whe: the model's FX, events, clauses,
// conditions, modifiers, motions, actions and selection UI. The difference is
// the encoding the official Object Editor works in — the .ebp drops the burner
// block, numbers three chunk types differently and keeps the action layer as a
// FOLDACTR folder of named DATAACTN chunks instead of one flat DATAACTS
// payload. The game itself reads the .whe; the .ebp is what a modder hands to
// the Object Editor.

// ebpVersions is the chunk version numbering the Object Editor's files carry,
// for every chunk type this package writes. Types outside the table keep the
// version the loaded file had, which is what the editor does too: it only
// knows the chunks it was built for.
var ebpVersions = map[string]int32{
	"FOLDREBP": 4,
	"FOLDANIM": 2,
	"DATAXREF": 3,
	"DATAANBV": 1,
	"FOLDEVCT": 1,
	"DATAEVNT": 3,
	"FOLDCLST": 1,
	"DATACLAS": 2,
	"FOLDCONL": 1,
	"DATACOND": 1,
	"FOLDMODL": 1,
	"DATAMODF": 2,
	"FOLDMTRE": 3,
	"DATAMTON": 4,
	"FOLDACTR": 1,
	"DATAACTN": 1,
	"DATASEUI": 3,
}

// MarshalEBP writes the loaded document in the Object Editor's format, so the
// model can be taken into that editor without a separate conversion. The
// loaded document is left as it was: only the copy that goes to the file has
// the .ebp numbering and the action folder.
func (d *Document) MarshalEBP() ([]byte, error) {
	if d == nil || len(d.header) != chunky.PreambleSize {
		return nil, fmt.Errorf("no .whe document is loaded")
	}
	var out bytes.Buffer
	out.Write(d.header)
	for _, c := range d.chunks {
		if converted := ebpChunk(c); converted != nil {
			out.Write(chunky.Serialize(converted))
		}
	}
	return out.Bytes(), nil
}

// ebpChunk converts one chunk of the tree for the .ebp copy: the burner block
// is dropped, the action payload becomes the editor's folder, and the known
// types take the editor's version numbers.
func ebpChunk(c *chunky.Chunk) *chunky.Chunk {
	if c == nil {
		return nil
	}
	typeID := strings.TrimRight(c.TypeID, "\x00")
	if typeID == "DATAFBIF" {
		// Burn info records which tool last wrote the .whe; an .ebp the editor
		// is about to open has no use for it, and the reference exporter omits
		// it in this format.
		return nil
	}
	if typeID == "DATAACTS" {
		return ebpActionFolder(c)
	}
	out := &chunky.Chunk{TypeID: c.TypeID, Version: c.Version, Name: c.Name, Payload: c.Payload}
	if version, ok := ebpVersions[typeID]; ok {
		out.Version = version
	}
	for _, child := range c.Children {
		if converted := ebpChunk(child); converted != nil {
			out.Children = append(out.Children, converted)
		}
	}
	return out
}

// ebpActionFolder rewrites the flat DATAACTS payload as FOLDACTR: one named
// DATAACTN chunk per action, each holding the action's motion links and then
// its sub-action links. An unreadable payload yields an empty folder rather
// than a file the editor would open with a broken action tree.
func ebpActionFolder(c *chunky.Chunk) *chunky.Chunk {
	folder := &chunky.Chunk{TypeID: "FOLDACTR", Version: ebpVersions["FOLDACTR"]}
	actions, err := parseActs(c.Payload)
	if err != nil {
		return folder
	}
	for _, action := range actions {
		name := strings.TrimRight(action.Name, "\x00")
		if name == "" {
			continue
		}
		payload := marshalActLinks(action.Motions)
		payload = append(payload, marshalActLinks(action.Subactions)...)
		folder.Children = append(folder.Children, &chunky.Chunk{
			TypeID:  "DATAACTN",
			Version: ebpVersions["DATAACTN"],
			Name:    name,
			Payload: payload,
		})
	}
	return folder
}
