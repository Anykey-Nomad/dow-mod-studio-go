package fx

import (
	"dow-mod-studio/internal/whe"
	"fmt"
	"log"
	"path"
	"regexp"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
)

// An effect reaches a model in three steps. The entity blueprint that spawns
// the unit (attrib/ebps/**.lua) lists event references in event_manager_ext;
// each reference names a file under art/events; and that file names the .lua
// effect plus the markers of the model it plays at. FxEventBinding is one
// resolved step of that chain, ready to be matched against a model's markers.
type FxEventBinding struct {
	Ref    string // as the blueprint wrote it: "structure_fx/relocate"
	Event  whe.FxEvent
	Source string // the blueprint the reference came from
}

// EventNamePattern matches the event references of an entity blueprint
// written as text: the DE style GameData["event_manager_ext"]…event_name =
// [[...]] and the classic rgd style ["event_name"] = "...".
var EventNamePattern = regexp.MustCompile(`event_name"\]\s*=\s*(?:\[\[([^\]]+)\]|"([^"]+)")`)

// ResolveEventsForModel finds every event the entity blueprints of a model
// reference and parses the definitions. The blueprint is found by its
// animator field naming the model — the path of the .whm under art/ebps with
// the extension off — so a model shared by several units picks up the events
// of all of them.
func ResolveEventsForModel(tree *modtree.ModTree, modelFile string) ([]FxEventBinding, string, error) {
	if tree == nil {
		return nil, "", fmt.Errorf("no module is open")
	}
	// A model's own events live beside it in a .whe of the same name, and for
	// the models that carry one that file is the whole story: it is written
	// per model rather than per unit, and it is the only place a beam's second
	// marker appears. The blueprint walk below finds events for a model
	// without one, so both are read.
	var out []FxEventBinding
	readWhe := func(virtualPath string) []FxEventBinding {
		data, err := tree.ReadFile(virtualPath)
		if err != nil || len(data) == 0 {
			return nil
		}
		events, err := whe.ParseFxEvents(data)
		if err != nil {
			log.Printf("3D preview: events %s: %v", virtualPath, err)
			return nil
		}
		var b []FxEventBinding
		for _, ev := range events {
			b = append(b, FxEventBinding{Ref: virtualPath, Event: ev, Source: virtualPath})
		}
		return b
	}
	// The tree is case insensitive, so the upper case guess below resolves to
	// the same file; one read of it is one copy of each event.
	wheRead := map[string]bool{}
	for _, cand := range []string{
		strings.TrimSuffix(modelFile, path.Ext(modelFile)) + ".whe",
		strings.TrimSuffix(modelFile, path.Ext(modelFile)) + ".WHE",
	} {
		key := strings.ToLower(cand)
		if wheRead[key] {
			continue
		}
		wheRead[key] = true
		out = append(out, readWhe(cand)...)
	}
	if len(out) == 0 {
		// Not in the tree at the model's own path: it may sit in another
		// archive of the same mod under the same name.
		whe := strings.ToLower(strings.TrimSuffix(path.Base(modelFile), path.Ext(modelFile)) + ".whe")
		modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
			if strings.EqualFold(f.Name, whe) {
				out = append(out, readWhe(f.FullPath)...)
			}
		})
	}
	// A blueprint and a .whe can name the same event; the .whe wins, since it
	// is the copy written against this model. Only the blueprint's own events
	// are filtered — the .whe's are the ones being kept.
	wheNames := map[string]bool{}
	for _, b := range out {
		wheNames[strings.ToLower(b.Event.Name)] = true
	}
	wheEvents := out
	var blueprint []FxEventBinding
	base := MaterialBaseName(modelFile)
	base = strings.TrimSuffix(base, path.Ext(base))
	if base == "" {
		return nil, "", fmt.Errorf("model file %q has no name", modelFile)
	}
	lower := strings.ToLower(base)
	// The blueprint normally sits at the model's own path under attrib/ebps,
	// so the direct guesses come first and only a miss pays for a walk.
	var candidates []string
	if i := strings.Index(strings.ToLower(modelFile), "art/ebps/"); i >= 0 {
		rel := modelFile[i+len("art/ebps/"):]
		rel = strings.TrimSuffix(rel, path.Ext(rel))
		for _, cand := range []string{
			"attrib/ebps/" + rel + ".lua",
			"attrib/ebps/" + rel + ".rgd",
			"attrib/ebps/" + strings.ToLower(rel) + ".lua",
			"attrib/ebps/" + strings.ToLower(rel) + ".rgd",
		} {
			candidates = append(candidates, cand, strings.ReplaceAll(cand, "/", `\`))
		}
	}
	read := func(virtualPath string) ([]byte, bool) {
		data, err := tree.ReadFile(virtualPath)
		if err != nil || len(data) == 0 {
			return nil, false
		}
		return data, true
	}
	var sources []string
	for _, cand := range candidates {
		if data, ok := read(cand); ok {
			_ = data
			sources = append(sources, cand)
			break
		}
	}
	if len(sources) == 0 {
		// The model is driven from somewhere else: one walk of the blueprints
		// settles it. A blueprint keeps the model's own file name, so that is
		// what it is matched on.
		wantLua := lower + ".lua"
		wantRgd := lower + ".rgd"
		modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
			lp := strings.ToLower(f.FullPath)
			if !strings.Contains(lp, `\attrib\`) {
				return
			}
			ln := strings.ToLower(f.Name)
			if ln == wantLua || ln == wantRgd {
				sources = append(sources, f.FullPath)
			}
		})
	}
	if len(sources) == 0 {
		// No blueprint drives it, but the model's own .whe may still have
		// been found above, and those events stand on their own.
		if len(out) > 0 {
			return out, base, nil
		}
		return nil, base, fmt.Errorf("no entity blueprint drives %s", base)
	}
	seenRef := map[string]bool{}
	var firstErr error
	for _, src := range sources {
		data, _ := read(src)
		for _, ref := range fxEventRefs(data, strings.HasSuffix(strings.ToLower(src), ".rgd")) {
			key := strings.ToLower(ref)
			if seenRef[key] {
				continue
			}
			seenRef[key] = true
			events, _, err := readFxEvents(tree, ref)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", ref, err)
				}
				continue
			}
			for _, ev := range events {
				blueprint = append(blueprint, FxEventBinding{Ref: ref, Event: ev, Source: src})
			}
		}
	}
	if len(blueprint) == 0 && len(wheEvents) == 0 && firstErr != nil {
		return nil, base, firstErr
	}
	// The blueprint's events come last: a model with its own .whe has already
	// been given the definitions that were written for it, and a reference
	// shared with another unit should not add a second copy of each.
	out = wheEvents
	for _, b := range blueprint {
		if wheNames[strings.ToLower(b.Event.Name)] {
			continue
		}
		out = append(out, b)
	}
	return out, base, nil
}

// WheBindingsOverlay puts a live .whe document's definitions over the ones
// resolved from the tree. The workers read the file, but the Events section
// edits the document in memory — an edited entry (another fx, another marker,
// a different scale) is what the preview must resolve while the file on disk
// still says the old one. A definition the document carries that the tree's
// copy does not — one added this session — joins as its own binding.
func WheBindingsOverlay(bindings []FxEventBinding, doc *whe.Document) []FxEventBinding {
	live := doc.Events()
	if len(live) == 0 {
		return bindings
	}
	out := make([]FxEventBinding, 0, len(bindings)+len(live))
	present := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		key := strings.ToLower(b.Event.Name)
		if ev, ok := live[key]; ok {
			b.Event = ev
			present[key] = true
		}
		out = append(out, b)
	}
	for name, ev := range live {
		if !present[name] {
			out = append(out, FxEventBinding{Ref: ev.Name, Event: ev, Source: "edited .whe"})
		}
	}
	return out
}

// ReadWheTimelines reads the motion timelines of the .whe beside a model,
// which say which events play while each animation runs. A model whose
// effects all live in its blueprint has no .whe, and then no timelines, which
// is not a failure: everything it named plays at once.
func ReadWheTimelines(tree *modtree.ModTree, modelFile string) []whe.FxTimeline {
	data := ReadWheData(tree, modelFile)
	if len(data) == 0 {
		return nil
	}
	return whe.ParseFxTimelines(data)
}

// ReadWheData reads the .whe beside a model, by whatever route finds it.
//
// The file normally sits at the model's own path with the extension swapped,
// but that is not always true: a model opened from inside an archive, or
// through the unit viewport, arrives with the virtual path it was reached by
// rather than the one the game keeps it at, and the sibling read misses. When
// it does, the file is looked up by name across the tree, which is what
// ResolveEventsForModel above already does for the same reason.
//
// This matters more than it looks. The .whe is the only file that says when an
// event fires inside a clip, so a model whose .whe is not found plays every
// effect and every sound the instant its animation starts — the timelines are
// read as empty rather than as missing, and an empty set means "play them all".
func ReadWheData(tree *modtree.ModTree, modelFile string) []byte {
	if tree == nil {
		return nil
	}
	base := strings.TrimSuffix(modelFile, path.Ext(modelFile))
	for _, cand := range []string{base + ".whe", base + ".WHE"} {
		data, err := tree.ReadFile(cand)
		if err == nil && len(data) > 0 {
			return data
		}
	}
	whe := strings.ToLower(path.Base(base) + ".whe")
	var found []byte
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if found != nil || !strings.EqualFold(f.Name, whe) {
			return
		}
		if data, err := tree.ReadTreeFile(f); err == nil {
			found = data
		}
	})
	return found
}

// fxEventRefs pulls every event reference out of a blueprint: a binary .rgd
// through the parser (the reference lives under event_manager_ext), a text
// one through the two styles the game has written over the years.
func fxEventRefs(data []byte, binary bool) []string {
	if binary {
		file, err := rgd.ParseRgd(data)
		if err == nil && file.Root != nil {
			var refs []string
			var walk func(e *rgd.RgdEntry)
			walk = func(e *rgd.RgdEntry) {
				for _, c := range e.Children {
					if strings.EqualFold(c.Name, "event_name") {
						if s := c.StrVal; s != "" {
							refs = append(refs, s)
						}
						if s := c.WStrVal; s != "" {
							refs = append(refs, s)
						}
					}
					walk(c)
				}
			}
			walk(file.Root)
			if len(refs) > 0 {
				return refs
			}
		}
		// A parse failure falls through to the text scan: some files are
		// chunky containers with text tables inside.
	}
	var refs []string
	for _, m := range EventNamePattern.FindAllSubmatch(data, -1) {
		ref := string(m[1])
		if ref == "" {
			ref = string(m[2])
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

// readFxEvents resolves an event reference such as "structure_fx/relocate" or
// "Unit_Ability_FX\wraith_tomb" to its file under art/events and parses it.
// A reference that names no file directly is looked up by file name, which is
// how a reference into another folder still resolves.
func readFxEvents(tree *modtree.ModTree, ref string) ([]whe.FxEvent, string, error) {
	norm := strings.ReplaceAll(ref, `\`, "/")
	norm = strings.TrimSuffix(norm, ".events")
	rel := "art/events/" + norm + ".events"
	for _, cand := range []string{rel, strings.ReplaceAll(rel, "/", `\`)} {
		if data, err := tree.ReadFile(cand); err == nil {
			evs, err := whe.ParseFxEvents(data)
			return evs, cand, err
		}
	}
	want := strings.ToLower(path.Base(norm) + ".events")
	if f := tree.FindByName(want); f != nil {
		if data, err := tree.ReadTreeFile(f); err == nil {
			evs, err := whe.ParseFxEvents(data)
			return evs, f.FullPath, err
		}
	}
	return nil, rel, fmt.Errorf("events file not found")
}

// MaterialBaseName is the file name of a material reference, with any folder
// in front of it dropped. Both separators are recognised because references
// come from game data written on Windows as well as from mod folders.
func MaterialBaseName(name string) string {
	base := name
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	return base
}
