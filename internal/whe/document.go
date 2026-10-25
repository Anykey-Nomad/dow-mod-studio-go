package whe

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"encoding/binary"
	"fmt"
	"math"
	"path"
	"strings"
)

// Document retains the original header and all unknown chunk data while
// allowing DATAEVNT and DATAMTON payloads to be changed in memory.
type Document struct {
	header   []byte
	chunks   []*chunky.Chunk
	trailing []byte
}

func ParseDocument(data []byte) (*Document, error) {
	if len(data) < chunky.PreambleSize || !bytes.HasPrefix(data, chunky.Magic) {
		return nil, fmt.Errorf("not a Relic Chunky .whe file")
	}
	chunks, used := chunky.Parse(data[chunky.PreambleSize:], 0)
	if len(chunks) == 0 || used == 0 {
		return nil, fmt.Errorf(".whe contains no readable chunks")
	}
	return &Document{
		header:   append([]byte(nil), data[:chunky.PreambleSize]...),
		chunks:   chunks,
		trailing: append([]byte(nil), data[chunky.PreambleSize+used:]...),
	}, nil
}

func (d *Document) MarshalBinary() ([]byte, error) {
	if d == nil || len(d.header) != chunky.PreambleSize {
		return nil, fmt.Errorf("no .whe document is loaded")
	}
	var out bytes.Buffer
	out.Write(d.header)
	for _, c := range d.chunks {
		out.Write(chunky.Serialize(c))
	}
	out.Write(d.trailing)
	return out.Bytes(), nil
}

func (d *Document) walk(fn func(*chunky.Chunk)) {
	if d == nil {
		return
	}
	var visit func([]*chunky.Chunk)
	visit = func(chunks []*chunky.Chunk) {
		for _, c := range chunks {
			fn(c)
			visit(c.Children)
		}
	}
	visit(d.chunks)
}

func (d *Document) timelineChunks() []*chunky.Chunk {
	var out []*chunky.Chunk
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID == "DATAMTON" {
			out = append(out, c)
		}
	})
	return out
}

func (d *Document) Timelines() []FxTimeline {
	var out []FxTimeline
	for _, c := range d.timelineChunks() {
		if tl, err := parseDataMton(c.Name, c.Payload); err == nil {
			out = append(out, tl)
		}
	}
	return out
}

// LinkedAnimations maps an animation name declared by this model's .whe to
// the source model and animation it imports from. Keys are normalized for
// case-insensitive lookup, like the reference editor's linkedAnims map.
func (d *Document) LinkedAnimations() map[string]LinkedAnimation {
	out := make(map[string]LinkedAnimation)
	d.walk(func(c *chunky.Chunk) {
		typeID := strings.TrimSpace(strings.TrimRight(c.TypeID, "\x00"))
		if !strings.EqualFold(typeID, "ANIM") && !strings.EqualFold(typeID, "FOLDANIM") {
			return
		}
		var xref *chunky.Chunk
		var visit func([]*chunky.Chunk)
		visit = func(chunks []*chunky.Chunk) {
			for _, child := range chunks {
				if strings.EqualFold(strings.TrimSpace(strings.TrimRight(child.TypeID, "\x00")), "DATA") && strings.EqualFold(child.Name, "XREF") {
					xref = child
					return
				}
				visit(child.Children)
				if xref != nil {
					return
				}
			}
		}
		visit(c.Children)
		if xref == nil {
			return
		}
		r := &chunky.Reader{Data: xref.Payload}
		source, name := strings.TrimSpace(r.Str()), strings.TrimSpace(r.Str())
		if r.Err == nil && name != "" {
			out[strings.ToLower(strings.TrimSpace(c.Name))] = LinkedAnimation{SourceModel: source, Animation: name}
		}
	})
	return out
}

// LinkedAnimation names an animation imported from a different model.
type LinkedAnimation struct {
	SourceModel string
	Animation   string
}

// IdleAnimation follows the same default action -> last motion/action link ->
// last motion animation chain as the reference editor. Conditions are ignored,
// matching its editor preview which treats the final listed branch as default.
func (d *Document) IdleAnimation() string {
	if d == nil {
		return ""
	}
	actions := d.Actions()
	motions := make(map[string]FxTimeline, len(d.Timelines()))
	for _, timeline := range d.Timelines() {
		motions[strings.ToLower(strings.TrimSpace(timeline.Name))] = timeline
	}
	actionByName := make(map[string]FxAction, len(actions))
	for _, action := range actions {
		actionByName[strings.ToLower(strings.TrimSpace(action.Name))] = action
	}
	action, ok := actionByName["default"]
	if !ok {
		return ""
	}
	seenActions := make(map[string]bool)
	for {
		name := strings.ToLower(strings.TrimSpace(action.Name))
		if seenActions[name] {
			return ""
		}
		seenActions[name] = true
		if len(action.Motions) > 0 {
			break
		}
		if len(action.Subactions) == 0 {
			return ""
		}
		next := strings.ToLower(strings.TrimSpace(action.Subactions[len(action.Subactions)-1].Name))
		var found bool
		action, found = actionByName[next]
		if !found {
			return ""
		}
	}
	motionName := strings.ToLower(strings.TrimSpace(action.Motions[len(action.Motions)-1].Name))
	seenMotions := make(map[string]bool)
	for {
		if seenMotions[motionName] {
			return ""
		}
		seenMotions[motionName] = true
		timeline, found := motions[motionName]
		if !found {
			return ""
		}
		if len(timeline.Animations) > 0 {
			return strings.TrimSpace(timeline.Animations[len(timeline.Animations)-1])
		}
		if len(timeline.Motions) == 0 {
			return ""
		}
		motionName = strings.ToLower(strings.TrimSpace(timeline.Motions[len(timeline.Motions)-1]))
	}
}

// RGDLinkedAnimationPath resolves a .whe XREF source against the game's art tree.
func RGDLinkedAnimationPath(source string) string {
	source = strings.ReplaceAll(strings.TrimSpace(source), "\\", "/")
	source = strings.TrimPrefix(source, "./")
	if strings.HasPrefix(strings.ToLower(source), "data:") {
		source = source[len("data:"):]
	}
	source = strings.TrimPrefix(source, "/")
	if !strings.HasPrefix(strings.ToLower(source), "art/") {
		source = "art/ebps/" + source
	}
	return "Data/" + strings.TrimSuffix(source, path.Ext(source)) + ".whm"
}

func (d *Document) findTimeline(name string) *chunky.Chunk {
	for _, c := range d.timelineChunks() {
		tl, err := parseDataMton(c.Name, c.Payload)
		if err != nil {
			continue
		}
		if strings.EqualFold(tl.Name, name) {
			return c
		}
		for _, anim := range tl.Animations {
			if strings.EqualFold(anim, name) {
				return c
			}
		}
	}
	return nil
}

func (d *Document) Events() map[string]FxEvent {
	out := map[string]FxEvent{}
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID != "DATAEVNT" {
			return
		}
		if ev, err := parseDataEvnt(c.Name, c.Payload); err == nil {
			out[strings.ToLower(ev.Name)] = ev
		}
	})
	return out
}

// SetEventEntry writes one value into the definition of an event, rebuilding
// the DATAEVNT payload it lives in. Every chunk carrying the name is written:
// Events() keeps the last of several same-named definitions, so editing only
// one of them would either show the wrong value or lose the edit.
func (d *Document) SetEventEntry(event string, index int, value string) error {
	written := false
	var failure error
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID != "DATAEVNT" || !strings.EqualFold(strings.TrimRight(c.Name, "\x00"), event) {
			return
		}
		ev, err := parseDataEvnt(c.Name, c.Payload)
		if err != nil {
			failure = err
			return
		}
		if index < 0 || index >= len(ev.Entries) {
			failure = fmt.Errorf("%s has no entry %d", event, index+1)
			return
		}
		ev.Entries[index].Value = value
		c.Payload = FxPayload(ev.Entries)
		written = true
	})
	if failure != nil {
		return failure
	}
	if !written {
		return fmt.Errorf("no definition of %s in this .whe", event)
	}
	return nil
}

// SetTimedEventTime moves one event's moment on a timeline in place — the
// edit the Events form's time field makes — without re-adding the event.
func (d *Document) SetTimedEventTime(timeline string, index int, at float64) error {
	if math.IsNaN(at) || math.IsInf(at, 0) || at > math.MaxFloat32 || at < -1 {
		return fmt.Errorf("event time must be -1 (on enter) or a finite non-negative number")
	}
	chunk := d.findTimeline(timeline)
	if chunk == nil {
		return fmt.Errorf("timeline %q was not found", timeline)
	}
	tl, err := ParseMton(chunk.Name, chunk.Payload)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(tl.Events) {
		return fmt.Errorf("event selection is no longer valid")
	}
	tl.Events[index].Time = at
	chunk.Payload = tl.MarshalBinary()
	return nil
}

// SetEventEntryByKey writes a value into an event definition by key rather
// than position: the entry is updated where it exists and appended where it
// does not (unless the value is empty). Every chunk carrying the name is
// rewritten the way SetEventEntry does, so several same-named definitions
// stay identical.
func (d *Document) SetEventEntryByKey(event, key, value string) error {
	written := false
	var failure error
	d.walk(func(c *chunky.Chunk) {
		if c.TypeID != "DATAEVNT" || !strings.EqualFold(strings.TrimRight(c.Name, "\x00"), event) {
			return
		}
		ev, err := parseDataEvnt(c.Name, c.Payload)
		if err != nil {
			failure = err
			return
		}
		found := false
		for i := range ev.Entries {
			if strings.EqualFold(ev.Entries[i].Key, key) {
				ev.Entries[i].Value = value
				found = true
				break
			}
		}
		if !found && value != "" {
			ev.Entries = append(ev.Entries, FxEventEntry{Key: key, Value: value})
		}
		c.Payload = FxPayload(ev.Entries)
		written = true
	})
	if failure != nil {
		return failure
	}
	if !written {
		return fmt.Errorf("no definition of %s in this .whe", event)
	}
	return nil
}

func (d *Document) AddTimedEvent(timeline string, ev FxTimelineEvent, definition *FxEvent, soundPath string) error {
	name := strings.TrimSpace(ev.Name)
	if name == "" {
		return fmt.Errorf("event name is required")
	}
	if math.IsNaN(ev.Time) || math.IsInf(ev.Time, 0) || ev.Time > math.MaxFloat32 || ev.Time < -1 {
		return fmt.Errorf("event time must be -1 (on enter) or a finite non-negative number")
	}
	chunk := d.findTimeline(timeline)
	if chunk == nil {
		return fmt.Errorf("timeline %q was not found", timeline)
	}
	tl, err := ParseMton(chunk.Name, chunk.Payload)
	if err != nil {
		return err
	}
	// The definition behind the name is created when the caller supplies one,
	// reused when the .whe already carries it, and must already exist when the
	// caller supplies none — which is how the crystal set is re-fired on another
	// clip without re-authoring its marker bindings.
	existing, exists := d.Events()[strings.ToLower(name)]
	switch {
	case soundPath != "":
		if !strings.HasPrefix(strings.ToLower(name), "sound") {
			return fmt.Errorf("sound event names should start with sound_")
		}
		if !exists {
			if err := d.addDefinition(FxEvent{Name: name}, soundPath); err != nil {
				return err
			}
		}
	case definition != nil && len(definition.Entries) > 0:
		if exists {
			if !fxEventEntriesEqual(existing, definition.Entries) {
				return fmt.Errorf("event %q is already defined here with different settings", name)
			}
		} else if err := d.addDefinition(FxEvent{Name: name, Entries: definition.Entries}, ""); err != nil {
			return err
		}
	default:
		if !exists {
			return fmt.Errorf("event %q is not defined in this .whe; fill in an FX path or a sound path to define it", name)
		}
	}
	tl.Events = append(tl.Events, FxTimelineEvent{Name: name, Time: ev.Time})
	chunk.Payload = tl.MarshalBinary()
	return nil
}

// fxEventEntriesEqual compares two definition entry lists on the terms the
// game reads them: keys case-insensitively, values exactly.
func fxEventEntriesEqual(a FxEvent, b []FxEventEntry) bool {
	if len(a.Entries) != len(b) {
		return false
	}
	for i := range a.Entries {
		if !strings.EqualFold(a.Entries[i].Key, b[i].Key) || a.Entries[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

func (d *Document) addDefinition(ev FxEvent, soundPath string) error {
	if ev.Name == "" {
		return fmt.Errorf("event name is required")
	}
	if _, exists := d.Events()[strings.ToLower(ev.Name)]; exists {
		return fmt.Errorf("event %q already exists in this .whe", ev.Name)
	}
	folder := (*chunky.Chunk)(nil)
	var find func([]*chunky.Chunk)
	find = func(cs []*chunky.Chunk) {
		for _, c := range cs {
			if c.TypeID == "FOLDEVCT" {
				folder = c
				return
			}
			find(c.Children)
			if folder != nil {
				return
			}
		}
	}
	find(d.chunks)
	if folder == nil {
		return fmt.Errorf(".whe has no FOLDEVCT event folder")
	}
	var payload []byte
	if soundPath != "" {
		payload = SoundPayload(soundPath)
	} else {
		payload = FxPayload(ev.Entries)
	}
	folder.Children = append(folder.Children, &chunky.Chunk{TypeID: "DATAEVNT", Version: 3, Name: ev.Name, Payload: payload})
	return nil
}

func (d *Document) RemoveTimedEvent(timeline string, index int) error {
	chunk := d.findTimeline(timeline)
	if chunk == nil {
		return fmt.Errorf("timeline %q was not found", timeline)
	}
	tl, err := ParseMton(chunk.Name, chunk.Payload)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(tl.Events) {
		return fmt.Errorf("event selection is no longer valid")
	}
	tl.Events = append(tl.Events[:index], tl.Events[index+1:]...)
	chunk.Payload = tl.MarshalBinary()
	return nil
}

// RemoveTimelineAnimation drops one animation name from a motion timeline's
// blend list. It uses the same ParseMton/MarshalBinary round trip as the
// event edits, so only this timeline's DATAMTON payload is rewritten and the
// save path stays byte-exact for the rest of the file.
func (d *Document) RemoveTimelineAnimation(timeline string, index int) error {
	chunk := d.findTimeline(timeline)
	if chunk == nil {
		return fmt.Errorf("timeline %q was not found", timeline)
	}
	tl, err := ParseMton(chunk.Name, chunk.Payload)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(tl.Animations) {
		return fmt.Errorf("animation selection is no longer valid")
	}
	tl.Animations = append(tl.Animations[:index], tl.Animations[index+1:]...)
	chunk.Payload = tl.MarshalBinary()
	return nil
}

// CopyWheCrystalEvents copies the marker-bound crystal set from phase_out to
// phase_in, preserving its authored event times and definitions.
func (d *Document) CopyWheCrystalEvents() (int, error) {
	source := d.findTimeline("phase_out")
	target := d.findTimeline("phase_in")
	if source == nil || target == nil {
		return 0, fmt.Errorf("phase_in and phase_out timelines are both required")
	}
	src, err := ParseMton(source.Name, source.Payload)
	if err != nil {
		return 0, err
	}
	dst, err := ParseMton(target.Name, target.Payload)
	if err != nil {
		return 0, err
	}
	defs := d.Events()
	present := map[string]bool{}
	for _, e := range dst.Events {
		present[strings.ToLower(e.Name)] = true
	}
	added := 0
	for _, e := range src.Events {
		if !strings.HasPrefix(strings.ToLower(e.Name), "crystal") || present[strings.ToLower(e.Name)] {
			continue
		}
		if _, ok := defs[strings.ToLower(e.Name)]; !ok {
			continue
		}
		dst.Events = append(dst.Events, e)
		present[strings.ToLower(e.Name)] = true
		added++
	}
	if added == 0 {
		return 0, fmt.Errorf("phase_out has no crystal events missing from phase_in")
	}
	target.Payload = dst.MarshalBinary()
	return added, nil
}

type RandomMotion struct {
	Name   string
	Weight float32
}
type Mton struct {
	Animations []string
	random     []RandomMotion
	loop       int8
	Events     []FxTimelineEvent
	trailing   []byte
}

func ParseMton(name string, data []byte) (*Mton, error) {
	r := &chunky.Reader{Data: data}
	m := &Mton{}
	n := r.Count(4)
	for i := 0; i < n && r.Err == nil; i++ {
		m.Animations = append(m.Animations, r.Str())
	}
	n = r.Count(4)
	for i := 0; i < n && r.Err == nil; i++ {
		m.random = append(m.random, RandomMotion{Name: r.Str(), Weight: r.F32()})
	}
	m.loop = r.I8()
	n = r.Count(8)
	for i := 0; i < n && r.Err == nil; i++ {
		m.Events = append(m.Events, FxTimelineEvent{Name: r.Str(), Time: float64(r.F32())})
	}
	if r.Err != nil {
		return nil, fmt.Errorf("timeline %q: %w", name, r.Err)
	}
	m.trailing = append([]byte(nil), data[r.Off:]...)
	return m, nil
}

func (m *Mton) MarshalBinary() []byte {
	var b bytes.Buffer
	writeI32 := func(v int32) { _ = binary.Write(&b, binary.LittleEndian, v) }
	writeString := func(s string) { writeI32(int32(len(s))); b.WriteString(s) }
	writeI32(int32(len(m.Animations)))
	for _, s := range m.Animations {
		writeString(s)
	}
	writeI32(int32(len(m.random)))
	for _, r := range m.random {
		writeString(r.Name)
		_ = binary.Write(&b, binary.LittleEndian, r.Weight)
	}
	b.WriteByte(byte(m.loop))
	writeI32(int32(len(m.Events)))
	for _, e := range m.Events {
		writeString(e.Name)
		_ = binary.Write(&b, binary.LittleEndian, float32(e.Time))
	}
	b.Write(m.trailing)
	return b.Bytes()
}

func FxPayload(entries []FxEventEntry) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(len(entries)))
	for _, e := range entries {
		for _, s := range []string{e.Key, e.Value} {
			_ = binary.Write(&b, binary.LittleEndian, int32(len(s)))
			b.WriteString(s)
		}
	}
	return b.Bytes()
}

func SoundPayload(soundPath string) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(1))
	for _, s := range []string{"sound", strings.TrimSpace(soundPath)} {
		_ = binary.Write(&b, binary.LittleEndian, int32(len(s)))
		b.WriteString(s)
	}
	return b.Bytes()
}
