package whe

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"encoding/binary"
	"fmt"
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
	FxCompareIf     = 0
	FxCompareElseIf = 1
	FxCompareElse   = 2
	FxCompareNone   = 3
)

// HasCondition reports whether the link's compare type carries a condition
// name in the file: if and else if do, else and always do not — which is also
// where the writer of the section puts the next link's name.
func (l FxActionLink) HasCondition() bool {
	return l.Compare != FxCompareElse && l.Compare != FxCompareNone
}

// Guard is how the link's condition reads in the panel: "if cond",
// "else if cond", "else", or "always" for a link no condition guards.
func (l FxActionLink) Guard() string {
	switch l.Compare {
	case FxCompareIf:
		return "if " + l.Condition
	case FxCompareElseIf:
		return "else if " + l.Condition
	case FxCompareElse:
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
			if ln.HasCondition() {
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
	if r.Err == nil && r.Off < len(p) {
		// Older action payloads end at the links. Newer ones append base-action
		// and sub-data-type metadata for every action; the editor uses the
		// explicit type when deciding whether it follows an action or a motion.
		for i := range out {
			if len(p)-r.Off < 8 {
				break
			}
			base := r.I32()
			typ := r.I32()
			if base != 0 && base != 1 {
				r.Off -= 8
				break
			}
			_ = typ // reserved metadata; link counts already specify the type.
			if base != 0 {
				out[i].Motions = nil
				out[i].Subactions = nil
			}
		}
	}
	if r.Err != nil {
		return nil, r.Err
	}
	return out, nil
}

// MarshalActs writes actions back into a DATAACTS payload: an action count,
// then each action's name, motion list and sub-action list in turn — the
// exact shape parseActs reads, strings length prefixed the way the rest of
// the file stores them, and a condition written only for the link types
// that carry one.
func MarshalActs(acts []FxAction) []byte {
	var b bytes.Buffer
	putI32 := func(v int) {
		var t [4]byte
		binary.LittleEndian.PutUint32(t[:], uint32(v))
		b.Write(t[:])
	}
	putStr := func(s string) {
		putI32(len(s))
		b.WriteString(s)
	}
	putLinks := func(links []FxActionLink) {
		putI32(len(links))
		for _, ln := range links {
			putStr(ln.Name)
			putI32(int(ln.Compare))
			if ln.HasCondition() {
				putStr(ln.Condition)
			}
		}
	}
	putI32(len(acts))
	for _, a := range acts {
		putStr(a.Name)
		putLinks(a.Motions)
		putLinks(a.Subactions)
	}
	return b.Bytes()
}

// AddAction appends an action to the file's DATAACTS section: an action copied
// out of another unit's .whe, put on this model's own. A name the file already
// carries is refused rather than merged or replaced — an action's choices are
// an if / else if / else chain in file order, so splicing one into another
// would change what the target plays.
func (d *Document) AddAction(a FxAction) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return fmt.Errorf("an action needs a name")
	}
	chunk := d.actionsChunk()
	if chunk == nil {
		// A model whose file never carried the section gets one: the action
		// has nowhere else to live, and the game reads the chunk by type.
		chunk = d.newActionsChunk()
		if chunk == nil {
			return fmt.Errorf(".whe has no root folder to add the action to")
		}
	}
	acts, err := parseActs(chunk.Payload)
	if err != nil {
		return err
	}
	for _, have := range acts {
		if strings.EqualFold(strings.TrimSpace(have.Name), name) {
			return fmt.Errorf("action %q already exists in this .whe", name)
		}
	}
	copied := a
	copied.Name = name
	chunk.Payload = MarshalActs(append(acts, copied))
	return nil
}

// RenameAction renames one action of the DATAACTS section and follows the name
// through every sub-action link that called it: an action other actions invoke
// is renamed where it is defined and where it is called, or the chain that
// names it breaks. The motions are left alone — they name timelines, not
// actions — and a name another action already answers to is refused before
// anything is written.
func (d *Document) RenameAction(oldName, newName string) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	old := strings.TrimSpace(oldName)
	next := strings.TrimSpace(newName)
	if old == "" || next == "" {
		return fmt.Errorf("an action needs a name")
	}
	if strings.EqualFold(old, next) {
		return fmt.Errorf("the action is already called %q", next)
	}
	chunks := d.actionsChunks()
	found := false
	for _, c := range chunks {
		acts, err := parseActs(c.Payload)
		if err != nil {
			return err
		}
		for _, a := range acts {
			name := strings.TrimSpace(a.Name)
			if strings.EqualFold(name, next) && !strings.EqualFold(name, old) {
				return fmt.Errorf("action %q already exists in this .whe", next)
			}
			if strings.EqualFold(name, old) {
				found = true
			}
		}
	}
	if !found {
		return fmt.Errorf("no action named %q in this .whe", old)
	}
	for _, c := range chunks {
		acts, err := parseActs(c.Payload)
		if err != nil {
			return err
		}
		changed := false
		for i := range acts {
			if strings.EqualFold(strings.TrimSpace(acts[i].Name), old) {
				acts[i].Name = next
				changed = true
			}
			for j := range acts[i].Subactions {
				if strings.EqualFold(strings.TrimSpace(acts[i].Subactions[j].Name), old) {
					acts[i].Subactions[j].Name = next
					changed = true
				}
			}
		}
		if changed {
			c.Payload = MarshalActs(acts)
		}
	}
	return nil
}

// actionsChunks is every DATAACTS chunk of the file, in walk order.
func (d *Document) actionsChunks() []*chunky.Chunk {
	var out []*chunky.Chunk
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID == "DATAACTS" {
			out = append(out, c)
		}
	})
	return out
}

// actionsChunk is the DATAACTS chunk the section is read from and written to.
func (d *Document) actionsChunk() *chunky.Chunk {
	var found *chunky.Chunk
	d.walk(func(c *chunky.Chunk) {
		if found == nil && c.TypeID == "DATAACTS" {
			found = c
		}
	})
	return found
}

// newActionsChunk adds an empty DATAACTS chunk beside the model's other folders
// and returns it.
func (d *Document) newActionsChunk() *chunky.Chunk {
	chunk := &chunky.Chunk{TypeID: "DATAACTS", Version: 1, Payload: MarshalActs(nil)}
	var root *chunky.Chunk
	d.walk(func(c *chunky.Chunk) {
		if root == nil && c.TypeID == "FOLDREBP" {
			root = c
		}
	})
	switch {
	case root != nil:
		root.Children = append(root.Children, chunk)
	case len(d.chunks) > 0:
		d.chunks = append(d.chunks, chunk)
	default:
		return nil
	}
	return chunk
}

// SetAction writes one action back into the DATAACTS chunk it was read from,
// rebuilding that chunk's payload the way SetEventEntry rebuilds a
// definition. Actions are numbered across every DATAACTS chunk of the file,
// in the order Actions() reports them.
func (d *Document) SetAction(action int, a FxAction) error {
	if d == nil {
		return fmt.Errorf("no .whe document is loaded")
	}
	at := 0
	found := false
	var failure error
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID != "DATAACTS" || found {
			return
		}
		acts, err := parseActs(c.Payload)
		if err != nil {
			failure = err
			return
		}
		if action < at || action >= at+len(acts) {
			at += len(acts)
			return
		}
		acts[action-at] = a
		c.Payload = MarshalActs(acts)
		found = true
	})
	if !found {
		if failure != nil {
			return failure
		}
		return fmt.Errorf("action %d is no longer in this .whe", action+1)
	}
	return nil
}
