package unitdef_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"dow-mod-studio/internal/app/editors"
	"dow-mod-studio/internal/app/shell"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/unitdef"
)

type rgdAttrStat struct {
	files  int            // how many files carry the path at all
	types  map[uint32]int // type -> how many files carry it that way
	values map[string]int // value -> how many files carry that value
}

func newRgdAttrStat() *rgdAttrStat {
	return &rgdAttrStat{types: map[uint32]int{}, values: map[string]int{}}
}

func (a *rgdAttrStat) typeList() string {
	types := make([]uint32, 0, len(a.types))
	for typ := range a.types {
		types = append(types, typ)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	parts := make([]string, 0, len(types))
	for _, typ := range types {
		parts = append(parts, fmt.Sprintf("%s(%d)", rgdTypeName(typ), a.types[typ]))
	}
	return strings.Join(parts, " ")
}

// uniform reports the value every file agrees on, and whether there is one.
func (a *rgdAttrStat) uniform() (string, bool) {
	best, n := "", 0
	for v, c := range a.values {
		if c > n || c == n && v < best {
			best, n = v, c
		}
	}
	return best, n == a.files
}

func rgdTypeName(typ uint32) string {
	switch typ {
	case rgd.RgdTypeFloat:
		return "float"
	case rgd.RgdTypeInt:
		return "int"
	case rgd.RgdTypeBool:
		return "bool"
	case rgd.RgdTypeString:
		return "string"
	case rgd.RgdTypeWString:
		return "wstring"
	case rgd.RgdTypeTable:
		return "table"
	case rgd.RgdTypeTableInt:
		return "tableInt"
	case rgd.RgdTypeNoData:
		return "nodata"
	}
	return fmt.Sprintf("type%d", typ)
}

// rgdAttrValue is what makes two occurrences of the same attribute comparable.
// A table is its reference, because that is what the unit inherits from; a
// scalar is its printed value.
func rgdAttrValue(e *rgd.RgdEntry) string {
	if e.IsTable() {
		if ref := e.RefString(); ref != "" {
			return "table->" + ref
		}
		return fmt.Sprintf("table(%d children)", len(e.Children))
	}
	return rgdTypeName(e.Type) + ":" + e.DisplayValue()
}

func collectRgdAttrs(prefix string, children []*rgd.RgdEntry, into map[string]*rgdAttrStat) {
	for _, ch := range children {
		path := prefix + "/" + ch.Name
		a := into[path]
		if a == nil {
			a = newRgdAttrStat()
			into[path] = a
		}
		a.files++
		a.types[ch.Type]++
		a.values[rgdAttrValue(ch)]++
		collectRgdAttrs(path, ch.Children, into)
	}
}

func TestRealCommonRgdAttributes(t *testing.T) {
	tree := testutil.OpenDXP3(t)
	cat := unitdef.BuildUnitCatalog(tree)
	var troops, structures []unitdef.UnitEntry
	for _, r := range cat.Races {
		troops = append(troops, r.Troops...)
		structures = append(structures, r.Structures...)
	}
	var full strings.Builder
	fmt.Fprintf(&full, "races: %d\n", len(cat.Races))
	for _, r := range cat.Races {
		fmt.Fprintf(&full, "  %-14s %d troops, %d structures\n", r.Name, len(r.Troops), len(r.Structures))
	}
	for _, set := range []struct {
		title   string
		entries []unitdef.UnitEntry
	}{
		{"TROOPS", troops},
		{"STRUCTURES", structures},
	} {
		stats, unreadable := rgdCommonStats(tree, set.entries)
		report := rgdCommonReport(set.title, stats, unreadable)
		skeleton := rgdCommonSkeleton(stats, 3)
		full.WriteString(report)
		full.WriteString(skeleton)
		t.Logf("\n%s\n%s", report, skeleton)
	}
	out := filepath.Join(t.TempDir(), "rgd_common_attributes.txt")
	if err := os.WriteFile(out, []byte(full.String()), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("full report: %s", out)
}

// rgdCommonStats reads every file of the set and folds its attributes together.
func rgdCommonStats(tree *modtree.ModTree, entries []unitdef.UnitEntry) (map[string]*rgdAttrStat, []string) {
	stats := map[string]*rgdAttrStat{}
	var unreadable []string
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.Path), ".rgd") {
			continue
		}
		data, err := tree.ReadFile(e.Path)
		if err != nil {
			unreadable = append(unreadable, e.Name)
			continue
		}
		rf, err := rgd.ParseRgd(data)
		if err != nil {
			unreadable = append(unreadable, e.Name)
			continue
		}
		one := map[string]*rgdAttrStat{}
		collectRgdAttrs(rf.Root.Name, rf.Root.Children, one)
		for path, a := range one {
			s := stats[path]
			if s == nil {
				s = newRgdAttrStat()
				stats[path] = s
			}
			s.files++
			for typ, c := range a.types {
				s.types[typ] += c
			}
			for v, c := range a.values {
				s.values[v] += c
			}
		}
	}
	return stats, unreadable
}

func rgdSortedPaths(stats map[string]*rgdAttrStat) []string {
	paths := make([]string, 0, len(stats))
	for p := range stats {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		if di != dj {
			return di < dj
		}
		return paths[i] < paths[j]
	})
	return paths
}

func rgdCommonReport(title string, stats map[string]*rgdAttrStat, unreadable []string) string {
	paths := rgdSortedPaths(stats)
	files := 0
	for _, a := range stats {
		if a.files > files {
			files = a.files
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n########## %s: %d files, %d unreadable ##########\n", title, files, len(unreadable))
	if len(unreadable) > 0 {
		fmt.Fprintf(&out, "unreadable: %s\n", strings.Join(unreadable, ", "))
	}
	// Top level sections, and how many files carry each. This is the shape of
	// the object: what a unit always has, and what only some of them have.
	inAll := map[string]int{}
	partial := map[string]int{}
	for path, a := range stats {
		head := strings.SplitN(strings.TrimPrefix(path, "GameData/"), "/", 2)[0]
		if a.files == files {
			inAll[head] = a.files
		} else {
			partial[head] = a.files
		}
	}
	names := make([]string, 0, len(inAll)+len(partial))
	for k := range inAll {
		names = append(names, k)
	}
	for k := range partial {
		if _, ok := inAll[k]; !ok {
			names = append(names, k)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		ni, nj := inAll[names[i]]+partial[names[i]], inAll[names[j]]+partial[names[j]]
		if ni != nj {
			return ni > nj
		}
		return names[i] < names[j]
	})
	out.WriteString("\ntop level sections:\n")
	for _, k := range names {
		n := inAll[k] + partial[k]
		mark := fmt.Sprintf("in all %d", files)
		if n < files {
			mark = fmt.Sprintf("in %d of %d", n, files)
		}
		fmt.Fprintf(&out, "  %-32s %s\n", k, mark)
	}
	common := 0
	for _, p := range paths {
		if stats[p].files == files {
			common++
		}
	}
	fmt.Fprintf(&out, "\nattributes in every one of the %d files: %d\n", files, common)
	for _, p := range paths {
		a := stats[p]
		if a.files != files {
			continue
		}
		v, uniform := a.uniform()
		value := "varies: " + v
		if uniform {
			value = v
		}
		fmt.Fprintf(&out, "  %-84s %-20s %s\n", p, a.typeList(), clipText(value, 70))
	}
	out.WriteString("\nattributes in 90% or more of the files, but not all:\n")
	shown := 0
	for _, p := range paths {
		a := stats[p]
		if a.files == files || a.files*10 < files*9 {
			continue
		}
		if shown++; shown > 80 {
			fmt.Fprintf(&out, "  ... and more\n")
			break
		}
		fmt.Fprintf(&out, "  %-84s in %d of %d\n", p, a.files, files)
	}
	return out.String()
}

func clipText(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

var rgdIndexSuffix = regexp.MustCompile(`_\d+$`)

// collapseRgdIndices folds a unit's numbered slots together: event_01 …
// event_36 become event_NN, modifier_01 … modifier_45 become modifier_NN, and so
// on down the path, because forty copies of one line say nothing about what a
// unit needs.
func collapseRgdIndices(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = rgdIndexSuffix.ReplaceAllString(p, "_NN")
	}
	return strings.Join(parts, "/")
}

// rgdSkeletonGroup is one line of the skeleton: an attribute every file has,
// with the numbered slots it stands for counted rather than listed.
type rgdSkeletonGroup struct {
	slots   int
	types   map[string]int
	values  map[string]int
	uniform bool
}

// rgdCommonSkeleton draws the always present attributes as an indented tree.
func rgdCommonSkeleton(stats map[string]*rgdAttrStat, maxDepth int) string {
	files := 0
	for _, a := range stats {
		if a.files > files {
			files = a.files
		}
	}
	groups := map[string]*rgdSkeletonGroup{}
	for path, a := range stats {
		if a.files != files {
			continue
		}
		key := collapseRgdIndices(path)
		if strings.Count(key, "/") > maxDepth {
			continue
		}
		g := groups[key]
		if g == nil {
			g = &rgdSkeletonGroup{types: map[string]int{}, values: map[string]int{}, uniform: true}
			groups[key] = g
		}
		g.slots++
		for t, c := range a.typeListTypes() {
			g.types[t] += c
		}
		v, uniform := a.uniform()
		if !uniform {
			g.uniform = false
		}
		g.values[v]++
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		di, dj := strings.Count(keys[i], "/"), strings.Count(keys[j], "/")
		if di != dj {
			return di < dj
		}
		return keys[i] < keys[j]
	})
	type node struct {
		name  string
		group *rgdSkeletonGroup
		kids  []*node
	}
	var root []*node
	index := map[string]*node{}
	for _, key := range keys {
		parts := strings.Split(strings.TrimPrefix(key, "GameData/"), "/")
		kids := &root
		for i, part := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			n := index[prefix]
			if n == nil {
				n = &node{name: part}
				index[prefix] = n
				*kids = append(*kids, n)
			}
			kids = &n.kids
			if i == len(parts)-1 {
				n.group = groups[key]
			}
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\nskeleton of the attributes every file has, to depth %d '=' every file agrees on the value, '~' it varies:\n", maxDepth)
	var draw func(n *node, prefix string, last bool)
	draw = func(n *node, prefix string, last bool) {
		branch := "|-- "
		if last {
			branch = "`-- "
		}
		if n.group != nil {
			g := n.group
			types := make([]string, 0, len(g.types))
			for t := range g.types {
				types = append(types, t)
			}
			sort.Strings(types)
			mark := "~"
			value := ""
			if g.uniform && len(g.values) == 1 {
				mark = "="
				for v := range g.values {
					value = v
				}
			}
			name := n.name
			if g.slots > 1 {
				name = fmt.Sprintf("%s (%d slots)", name, g.slots)
			}
			fmt.Fprintf(&out, "%s%s  [%s] %s  %s\n", prefix+branch, name, strings.Join(types, " "), mark, clipText(value, 60))
		} else {
			fmt.Fprintf(&out, "%s%s\n", prefix+branch, n.name)
		}
		next := prefix + "|   "
		if last {
			next = prefix + "    "
		}
		for i, k := range n.kids {
			draw(k, next, i == len(n.kids)-1)
		}
	}
	for i, n := range root {
		draw(n, "", i == len(root)-1)
	}
	return out.String()
}

// typeListTypes is typeList's parts, for a caller that wants to add them up.
func (a *rgdAttrStat) typeListTypes() map[string]int {
	types := make([]uint32, 0, len(a.types))
	for typ := range a.types {
		types = append(types, typ)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	out := make(map[string]int, len(types))
	for _, typ := range types {
		out[rgdTypeName(typ)] = a.types[typ]
	}
	return out
}

// ─── merged from x_test_rgd_test.go ────────────────────────────────────────

func TestRgdParseMonolith(t *testing.T) {
	tree, err := modtree.BuildModTree(`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`, "English")
	if err != nil {
		t.Fatalf("modtree.BuildModTree: %v", err)
	}
	defer tree.Close()
	// find any .rgd
	var rgdPath string
	var search func(folder *modtree.TreeFolder)
	search = func(folder *modtree.TreeFolder) {
		if rgdPath != "" {
			return
		}
		for _, f := range folder.Files {
			if filepath.Ext(f.Name) == ".rgd" && f.Name == "monolith.rgd" {
				rgdPath = f.FullPath
				return
			}
		}
		for _, c := range folder.Children {
			search(c)
			if rgdPath != "" {
				return
			}
		}
	}
	search(tree.Root)
	if rgdPath == "" {
		// try any rgd
		search = func(folder *modtree.TreeFolder) {
			if rgdPath != "" {
				return
			}
			for _, f := range folder.Files {
				if filepath.Ext(f.Name) == ".rgd" {
					rgdPath = f.FullPath
					return
				}
			}
			for _, c := range folder.Children {
				search(c)
			}
		}
		search(tree.Root)
	}
	if rgdPath == "" {
		t.Fatalf("no rgd found")
	}
	t.Logf("found rgd at %q", rgdPath)
	// read bytes
	data, err := tree.ReadFile(rgdPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	t.Logf("size %d", len(data))
	// hex dump first 64
	for i := 0; i < 64 && i < len(data); i++ {
		if i%16 == 0 {
			t.Logf("\n%04x:", i)
		}
		t.Logf(" %02x", data[i])
	}
	t.Logf(" header %q", string(data[0:16]))
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("rgd.ParseRgd: %v", err)
	}
	t.Logf("Parsed %q descriptor %q chunkVer %d root children %d", rf.Root.Name, rf.Descriptor, rf.ChunkVersion, len(rf.Root.Children))
	for i, ch := range rf.Root.Children {
		if i < 30 {
			ref := ""
			if ch.Ref != nil {
				ref = ch.Ref.DisplayValue()
			}
			t.Logf(" child[%d] %q type=%s hash=%08x ref=%q children=%d val=%q", i, ch.Name, ch.TypeLabel(), ch.Hash, ref, len(ch.Children), ch.DisplayValue())
		}
	}
	// check expected children as in screenshot
	wantNames := []string{"ability_ext", "health_ext", "ui_ext"}
	found := map[string]bool{}
	for _, ch := range rf.Root.Children {
		found[ch.Name] = true
	}
	for _, n := range wantNames {
		if !found[n] {
			t.Logf("WARNING expected child %q missing", n)
		}
	}
	// also test nested: health_ext children?
	for _, ch := range rf.Root.Children {
		if ch.Name == "health_ext" {
			t.Logf(" health_ext children %d", len(ch.Children))
			for _, sub := range ch.Children {
				t.Logf("  health sub %q type %s val %q children %d", sub.Name, sub.TypeLabel(), sub.DisplayValue(), len(sub.Children))
			}
			break
		}
	}
	// try to verify Reference parsing: health_ext ref?
	for _, ch := range rf.Root.Children {
		if ch.Name == "health_ext" {
			if ch.Ref != nil {
				t.Logf(" health_ext ref type %s val %q", ch.Ref.TypeLabel(), ch.Ref.DisplayValue())
			} else {
				t.Logf(" health_ext no ref")
			}
		}
	}
}

// ─── merged from x_test_rgd_edit_test.go ────────────────────────────────────────

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
		if _, err := editors.RgdApplyScalarText(entry, changes[entry.Hash]); err != nil {
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
	if _, err := editors.RgdApplyScalarText(entry, "not-a-number"); err == nil {
		t.Fatal("invalid integer was accepted")
	}
	if entry.IntVal != 7 {
		t.Fatalf("invalid edit changed value to %d", entry.IntVal)
	}
}

// ─── merged from x_test_rgd_reference_test.go ────────────────────────────────────────

func TestFindRgdReferenceFile(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	folder := tree.GetOrCreateFolder(tree.Root, "ebpextensions", nil)
	file := &modtree.TreeFile{
		Name:     "health_ext.lua",
		FullPath: `Data\ebpextensions\health_ext.lua`,
		Source:   &modtree.FileSource{ModName: "TestMod", Kind: modtree.KindThisMod},
	}
	folder.Files = append(folder.Files, file)
	folder.FileByLower[file.Name] = file
	ui := &shell.UiState{ModTree: tree}

	for _, ref := range []string{
		`ebpextensions\health_ext.lua`,
		`ebpextensions/health_ext.lua`,
		`Data\ebpextensions\health_ext.lua`,
	} {
		if got := shell.FindRgdReferenceFile(ui, ref); got != file.FullPath {
			t.Errorf("reference %q resolved to %q, want %q", ref, got, file.FullPath)
		}
	}
	for _, ref := range []string{"object://something", "missing.lua", ""} {
		if got := shell.FindRgdReferenceFile(ui, ref); got != "" {
			t.Errorf("reference %q unexpectedly resolved to %q", ref, got)
		}
	}
}
