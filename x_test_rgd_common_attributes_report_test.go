package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// What every unit and every building of the real game data has in common.
//
// Creating a unit or a building means writing an RGD the game will load, and
// the only reliable answer to "what has to be in it" is the data itself: this
// reads every unit and every structure of the installed game and reports the
// sections and the attributes they all carry, the ones nearly all of them
// carry, and the values that are the same everywhere — the boilerplate a new
// object starts from, and the handful of values it has to be given.
//
// It is a report rather than an assertion on purpose: the game data is not the
// project's to fix, and a test that failed because a patch changed a building
// would say nothing about the code. It skips when the game is not installed.
//
//	go test -run TestRealCommonRgdAttributes -v .
//
// The full report goes to the temp directory and its path is logged; the test
// output is the condensed skeleton of it.

// rgdAttrStat is one attribute path, seen across every file of a set.
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
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	var troops, structures []unitEntry
	for _, r := range cat.races {
		troops = append(troops, r.troops...)
		structures = append(structures, r.structures...)
	}
	var full strings.Builder
	fmt.Fprintf(&full, "races: %d\n", len(cat.races))
	for _, r := range cat.races {
		fmt.Fprintf(&full, "  %-14s %d troops, %d structures\n", r.name, len(r.troops), len(r.structures))
	}
	for _, set := range []struct {
		title   string
		entries []unitEntry
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
func rgdCommonStats(tree *modtree.ModTree, entries []unitEntry) (map[string]*rgdAttrStat, []string) {
	stats := map[string]*rgdAttrStat{}
	var unreadable []string
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.path), ".rgd") {
			continue
		}
		data, err := tree.ReadFile(e.path)
		if err != nil {
			unreadable = append(unreadable, e.name)
			continue
		}
		rf, err := rgd.ParseRgd(data)
		if err != nil {
			unreadable = append(unreadable, e.name)
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
