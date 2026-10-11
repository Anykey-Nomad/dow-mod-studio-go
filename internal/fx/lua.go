package fx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The .lua files under art/fx are the output of Relic's FX editor: a flat
// fxtypes list of nodes, each carrying an effect path ("root", "root>spray",
// "root>spray>ring"), a style (STYLE_COMBO, STYLE_SPRAY, STYLE_RING, …) and a
// property table of typed values. The path is the tree: a node's parent is
// the path with its last segment cut off.
//
// The properties the viewport preview understands are the ones that say what
// a particle looks like and when (Texture, Blending, Duration, Loop,
// Billboard, Radius, Colour) and the ones that say how a spray emits them
// (Emitter_Rate, Emitter_Duration, Emitter_Loop, Emitter_Direction,
// Emitter_Deviation, Emitter_Drag).

// FxProp is one parsed property: a scalar, a time/value curve or a colour
// ramp, depending on Type.
type FxProp struct {
	Type   string // VARTYPE_… as written in the file
	Str    string // VARTYPE_STRING
	Num    float64
	Curve  []FxKey        // VARTYPE_ARRAY_TIMEFLOAT: (time, value) pairs
	Colour []FxColourStop // VARTYPE_ARRAY_TIMECOLOUR
	Vec3   []FxVec3Key    // VARTYPE_ARRAY_TIMEVECTOR3: (time, x, y, z)
}

// FxKey is one point of a time/value curve.
type FxKey struct {
	T, V float64
}

// FxVec3Key is one point of a time/vector curve, which is what a beam's Arc
// and Offset are written as.
type FxVec3Key struct {
	T       float64
	X, Y, Z float64
}

// FxColourStop is one point of a colour ramp, all channels 0..1.
type FxColourStop struct {
	T       float64
	R, G, B float64
	A       float64
}

// FxNode is one effect node of a file.
type FxNode struct {
	Effect string // "root", "root>spray", …
	Style  string // STYLE_… without the quotes, may be empty
	Props  map[string]FxProp
}

// FxFile is a parsed effect file.
type FxFile struct {
	Nodes []FxNode
	// Single is set for the files that hold one node and give it no path of
	// its own. 1122 of the 4592 shipped effect files are written this way:
	// a bare `fx = { properties = { … } }` with a style beside it and no
	// effect = "root>…" to key the node on. There is no tree to walk in one
	// of these — the single node is the whole effect — so the simulator has
	// to be told to read it rather than look for children of it.
	Single bool
}

// Prop looks a property up, reporting whether it is there at all.
func (n FxNode) Prop(name string) (FxProp, bool) {
	p, ok := n.Props[name]
	return p, ok
}

// Float returns a scalar property or a fallback.
func (n FxNode) Float(name string, fallback float64) float64 {
	if p, ok := n.Props[name]; ok {
		return p.Num
	}
	return fallback
}

// Bool returns a boolean property or a fallback.
func (n FxNode) Bool(name string, fallback bool) bool {
	if p, ok := n.Props[name]; ok {
		return p.Num != 0
	}
	return fallback
}

// String returns a string property or an empty string.
func (n FxNode) String(name string) string {
	if p, ok := n.Props[name]; ok {
		return p.Str
	}
	return ""
}

// Root returns the node whose path has no parent.
func (f *FxFile) Root() *FxNode {
	for i := range f.Nodes {
		if !strings.Contains(f.Nodes[i].Effect, ">") {
			return &f.Nodes[i]
		}
	}
	if len(f.Nodes) > 0 {
		return &f.Nodes[0]
	}
	return nil
}

// Children returns the direct children of a node path.
func (f *FxFile) Children(effect string) []*FxNode {
	var out []*FxNode
	for i := range f.Nodes {
		p := f.Nodes[i].Effect
		if !strings.HasPrefix(p, effect+">") {
			continue
		}
		if !strings.Contains(strings.TrimPrefix(p, effect+">"), ">") {
			out = append(out, &f.Nodes[i])
		}
	}
	return out
}

var (
	fxEffectRe = regexp.MustCompile(`effect\s*=\s*"([^"]*)"`)
	fxStyleRe  = regexp.MustCompile(`style\s*=\s*"([^"]*)"`)
	fxNameRe   = regexp.MustCompile(`name\s*=\s*"([^"]+)"`)
	fxTypeRe   = regexp.MustCompile(`type\s*=\s*"(VARTYPE_[A-Z0-9_]+)"`)
	fxValueRe  = regexp.MustCompile(`value\s*=`)
	fxEntryRe  = regexp.MustCompile(`entry_\d+\s*=\s*\{`)
	// fxPropertyRe opens one property's own block, which is what a property's
	// type and value are read out of whichever order the file writes them in.
	fxPropertyRe = regexp.MustCompile(`property_\d+\s*=\s*\{`)
	fxNumRe      = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?`)
	// fxBareNodeRe recognises the single-node form: an fx = holding a
	// property table, with no effect = to name it by. fxBareRootName is the
	// path that single node is filed under, chosen so it reads like any other
	// file's root and has no children of its own.
	fxBareNodeRe   = regexp.MustCompile(`(?m)^\s*fx\s*=\s*$`)
	fxBareRootName = "root"
)

// ParseFxLua reads an effect file. The parser is a scanner over the text
// rather than a lua interpreter: the files are machine written, every node
// has the same shape, and a property whose value cannot be read is skipped
// rather than failing the file — a preview of most of an effect beats none.
func ParseFxLua(data []byte) (*FxFile, error) {
	s := string(data)
	locs := fxEffectRe.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		// The other way these files are written. A quarter of the shipped
		// effects are a single node with no effect = of its own, and they
		// carry everything a beam or a ring needs — the tree, the length,
		// the texture. Rejecting them loses the effect outright: four of the
		// base game's lightning beams are written this way and drew nothing
		// at all. The node stands for the file, so it is read as the root.
		if fxBareNodeRe.MatchString(s) {
			node := FxNode{Effect: fxBareRootName, Props: map[string]FxProp{}}
			if m := fxStyleRe.FindStringSubmatchIndex(s); m != nil {
				node.Style = s[m[2]:m[3]]
			}
			parseFxProps(s, &node)
			return &FxFile{Nodes: []FxNode{node}, Single: true}, nil
		}
		return nil, fmt.Errorf("no fxtypes nodes in effect file")
	}
	f := &FxFile{}
	for i, loc := range locs {
		end := len(s)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		span := s[loc[2]:end]
		node := FxNode{
			Effect: s[loc[2]:loc[3]],
			Props:  map[string]FxProp{},
		}
		if m := fxStyleRe.FindStringSubmatchIndex(span); m != nil {
			node.Style = span[m[2]:m[3]]
		}
		parseFxProps(span, &node)
		f.Nodes = append(f.Nodes, node)
	}
	return f, nil
}

// parseFxProps fills one node's property table from its text span.
//
// The two ways these files are written put the three fields of a property in
// either order: one style writes name, then type, then value, and the other
// writes value, then type, then name. Searching forward from the name only
// works for the first — in the second the search walks past the end of the
// property and picks up the next one's type and value, so every property in
// such a file gets its neighbour's setting. Reading each property's own block
// handles both, since the block is written the same way either way.
func parseFxProps(span string, node *FxNode) {
	names := fxNameRe.FindAllStringSubmatchIndex(span, -1)
	for i, nm := range names {
		name := span[nm[2]:nm[3]]
		searchFrom := nm[1]
		spanEnd := len(span)
		if i+1 < len(names) {
			spanEnd = names[i+1][0]
		}
		// The block this name belongs to, which is where its type and value
		// are written whichever order they come in.
		blocks := fxPropertyRe.FindAllStringIndex(span[:searchFrom], -1)
		if n := len(blocks); n > 0 {
			searchFrom = blocks[n-1][1]
		}
		region := span[searchFrom:spanEnd]
		tm := fxTypeRe.FindStringSubmatchIndex(region)
		if tm == nil {
			continue
		}
		typ := region[tm[2]:tm[3]]
		// The value is looked for on its own rather than after the type,
		// because in the value-first style it comes before it. A block holds
		// one type and one value, so the first of each in it is its own.
		vm := fxValueRe.FindStringIndex(region)
		if vm == nil {
			continue
		}
		value := region[vm[1]:]
		node.Props[name] = fxParseValue(typ, value)
	}
}

// fxParseValue reads one property value from the text that starts right
// after "value =".
func fxParseValue(typ, value string) FxProp {
	// The scanner stops right after "value =", so a space between the sign
	// and the literal is still in front: files write `value = "map.tga"`
	// and a leading space would miss the opening quote and drop the string.
	value = strings.TrimLeft(value, " \t\r\n")
	p := FxProp{Type: typ}
	switch typ {
	case "VARTYPE_STRING":
		if len(value) > 0 && value[0] == '"' {
			if j := strings.IndexByte(value[1:], '"'); j >= 0 {
				p.Str = value[1 : 1+j]
			}
		}
	case "VARTYPE_ARRAY_TIMEFLOAT":
		p.Curve = fxCurve(value)
	case "VARTYPE_ARRAY_TIMECOLOUR":
		p.Colour = fxColourRamp(value)
	case "VARTYPE_ARRAY_TIMEVECTOR3":
		p.Vec3 = fxVec3Curve(value)
	default:
		// Numbers, booleans and small enums all start the same way.
		if m := fxNumRe.FindString(value); m != "" {
			p.Num, _ = strconv.ParseFloat(m, 64)
		}
	}
	return p
}

// fxCurve reads a flat {t, v, t, v, …} table into pairs.
func fxCurve(value string) []FxKey {
	body, ok := fxBraceBody(value)
	if !ok {
		return nil
	}
	var nums []float64
	for _, m := range fxNumRe.FindAllString(body, -1) {
		v, err := strconv.ParseFloat(m, 64)
		if err == nil {
			nums = append(nums, v)
		}
	}
	if len(nums)%2 == 1 {
		nums = nums[:len(nums)-1]
	}
	keys := make([]FxKey, 0, len(nums)/2)
	for i := 0; i+1 < len(nums); i += 2 {
		keys = append(keys, FxKey{T: nums[i], V: nums[i+1]})
	}
	return keys
}

// fxVec3Curve reads { entry_NN = {t, x, y, z}, … } into a time/vector curve.
// A beam's Arc and Offset are written this way; the first three numbers of an
// entry are the vector and the fourth is the time, which is why the reader
// takes the numbers in fours from the end.
func fxVec3Curve(value string) []FxVec3Key {
	body, ok := fxBraceBody(value)
	if !ok {
		return nil
	}
	locs := fxEntryRe.FindAllStringIndex(body, -1)
	var keys []FxVec3Key
	for i, loc := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		chunk := body[loc[1]:end]
		var nums []float64
		for _, m := range fxNumRe.FindAllString(chunk, -1) {
			v, err := strconv.ParseFloat(m, 64)
			if err == nil {
				nums = append(nums, v)
			}
		}
		if len(nums) < 4 {
			continue
		}
		keys = append(keys, FxVec3Key{
			T: nums[0], X: nums[1], Y: nums[2], Z: nums[3],
		})
	}
	return keys
}

// fxColourRamp reads { entry_NN = {t, r, g, b, a}, … } into stops with the
// channels scaled to 0..1. A ramp whose numbers already sit in 0..1 is taken
// as authored that way.
//
// Every entry in these files carries all five numbers: across the 209 979
// colour stops of the shipped effects not one omits the alpha, so a stop that
// somehow has only four is a malformed file and is read as opaque rather than
// as a fade that would quietly hide the effect.
func fxColourRamp(value string) []FxColourStop {
	body, ok := fxBraceBody(value)
	if !ok {
		return nil
	}
	locs := fxEntryRe.FindAllStringIndex(body, -1)
	var stops []FxColourStop
	normalized := !strings.Contains(body, "255") && !fxHasLargeColor(body)
	for i, loc := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		chunk := body[loc[1]:end]
		var nums []float64
		for _, m := range fxNumRe.FindAllString(chunk, -1) {
			v, err := strconv.ParseFloat(m, 64)
			if err == nil {
				nums = append(nums, v)
			}
		}
		if len(nums) < 4 {
			continue
		}
		st := FxColourStop{T: nums[0]}
		rgb := nums[1:4]
		scale := 1.0 / 255
		if normalized {
			scale = 1
		}
		st.R, st.G, st.B = rgb[0]*scale, rgb[1]*scale, rgb[2]*scale
		if len(nums) >= 5 {
			st.A = nums[4] * scale
		} else {
			st.A = 1
		}
		stops = append(stops, st)
	}
	return stops
}

// fxHasLargeColor reports whether any of the first colour numbers is past
// what an authored 0..1 ramp would hold.
func fxHasLargeColor(body string) bool {
	locs := fxEntryRe.FindAllStringIndex(body, -1)
	for i, loc := range locs {
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		chunk := body[loc[1]:end]
		nums := fxNumRe.FindAllString(chunk, -1)
		if len(nums) >= 2 {
			if v, err := strconv.ParseFloat(nums[1], 64); err == nil && v > 1.5 {
				return true
			}
		}
	}
	return false
}

// fxBraceBody returns the inside of the first {…} of value.
func fxBraceBody(value string) (string, bool) {
	open := strings.IndexByte(value, '{')
	if open < 0 {
		return "", false
	}
	depth := 0
	for i := open; i < len(value); i++ {
		switch value[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return value[open+1 : i], true
			}
		}
	}
	return "", false
}
