package whe

import (
	"dow-mod-studio/internal/chunky"
	"strings"
)

// A .whe's DATAACTS section is the layer above the motion timelines: named
// actions that decide which motions play and which other actions run, each
// choice guarded by a condition. The guards form an if / else if / else chain
// in the order the file lists them, which is the same structure the
// object_tool reference reads out of the section.
//
// FxAction is one such action. Motions are the DATAMTON timelines it can
// play, Subactions are other actions of the same section it can call.
type FxAction struct {
	Name       string
	Motions    []FxActionLink
	Subactions []FxActionLink
}

// FxActionLink is one guarded choice inside an action: the motion (or
// sub-action) it names, which step of the if chain it sits at, and the
// condition guarding that step. Compare carries the file's own numbering —
// 0 if, 1 else if, 2 else, 3 none — and only the first two carry a
// condition name.
type FxActionLink struct {
	Name      string
	Compare   int32
	Condition string
}

// The CompareType values of DATAACTS, as object_tool defines them.
const (
	fxCompareIf     = 0
	fxCompareElseIf = 1
	fxCompareElse   = 2
	fxCompareNone   = 3
)

// Guard is how the link's condition reads in the panel: "if cond",
// "else if cond", "else", or "always" for a link no condition guards.
func (l FxActionLink) Guard() string {
	switch l.Compare {
	case fxCompareIf:
		return "if " + l.Condition
	case fxCompareElseIf:
		return "else if " + l.Condition
	case fxCompareElse:
		return "else"
	default:
		return "always"
	}
}

// Actions reads the .whe document's DATAACTS section. The chunk holds the
// whole section as one flat payload: an action count, then each action's
// name, motion list and sub-action list in turn.
func (d *Document) Actions() []FxAction {
	var out []FxAction
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID != "DATAACTS" {
			return
		}
		if acts, err := parseActs(c.Payload); err == nil {
			out = append(out, acts...)
		}
	})
	return out
}

// parseActs reads one DATAACTS payload. A link's condition name is present
// only when its compare type says the link is guarded, which is what keeps
// the reader and the writer of the section agreeing on where the next name
// begins.
func parseActs(p []byte) ([]FxAction, error) {
	r := &chunky.Reader{Data: p}
	readLinks := func() []FxActionLink {
		var links []FxActionLink
		n := r.Count(4)
		for i := 0; i < n && r.Err == nil; i++ {
			ln := FxActionLink{
				Name:    strings.TrimRight(r.Str(), "\x00"),
				Compare: r.I32(),
			}
			if ln.Compare != fxCompareElse && ln.Compare != fxCompareNone {
				ln.Condition = strings.TrimRight(r.Str(), "\x00")
			}
			links = append(links, ln)
		}
		return links
	}
	n := r.Count(4)
	var out []FxAction
	for i := 0; i < n && r.Err == nil; i++ {
		a := FxAction{Name: strings.TrimRight(r.Str(), "\x00")}
		a.Motions = readLinks()
		a.Subactions = readLinks()
		if r.Err == nil {
			out = append(out, a)
		}
	}
	if r.Err != nil {
		return nil, r.Err
	}
	return out, nil
}
