package main

import (
	"dow-mod-studio/internal/rgd"
	"strings"
	"testing"
)

// The template a new unit is written from, checked against the game's own units.
//
// The claim the template rests on is that it needs nothing the game does not
// already ask for: every attribute it writes is one that all 432 units of the
// installed game carry. The last test here is what holds that claim — it reads
// the real units and compares — and it is the one that would fail if a section,
// a slot or an attribute were dropped or invented.

// templateEntries flattens a generated unit into path -> entry, the same shape
// the common attribute report works in.
func templateEntries(rf *rgd.RgdFile) map[string]*rgd.RgdEntry {
	out := map[string]*rgd.RgdEntry{}
	var walk func(prefix string, children []*rgd.RgdEntry)
	walk = func(prefix string, children []*rgd.RgdEntry) {
		for _, ch := range children {
			path := prefix + "/" + ch.Name
			out[path] = ch
			walk(path, ch.Children)
		}
	}
	walk(rf.Root.Name, rf.Root.Children)
	return out
}

func TestNewUnitRgdHasTheTwelveSections(t *testing.T) {
	rf := newUnitRgd(unitTemplateName{Race: "necrons", Name: "necron_wraith"})
	var names []string
	for _, c := range rf.Root.Children {
		names = append(names, c.Name)
	}
	if len(names) != len(unitTemplateSections) {
		t.Fatalf("a new unit has %d top level entries, want the %d sections every unit has: %v",
			len(names), len(unitTemplateSections), names)
	}
	for i, want := range unitTemplateSections {
		if names[i] != want {
			t.Errorf("entry %d is %q, want %q", i, names[i], want)
		}
	}
	// Every section points at the shared lua it extends: that reference is what
	// fills in the thousands of attributes the file does not spell out.
	for _, c := range rf.Root.Children {
		if !strings.HasPrefix(c.RefString(), `ebpextensions\`) {
			t.Errorf("section %q extends %q, want one of the game's ebpextensions", c.Name, c.RefString())
		}
	}
}

// TestNewUnitRgdRoundTrips is the plain question of whether the file it writes is
// a file the app can read back: an RGD written by the marshaller and parsed
// again has to be the file that went in.
func TestNewUnitRgdRoundTrips(t *testing.T) {
	rf := newUnitRgd(unitTemplateName{Race: "chaos", Name: "chaos_hound"})
	data, err := rf.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("marshal produced no bytes")
	}
	back, err := rgd.ParseRgd(data)
	if err != nil {
		t.Fatalf("parse of the marshalled unit: %v", err)
	}
	if back.Descriptor != rf.Descriptor {
		t.Errorf("descriptor is %q, want %q", back.Descriptor, rf.Descriptor)
	}
	want := templateEntries(rf)
	got := templateEntries(back)
	if len(got) != len(want) {
		t.Fatalf("the file read back has %d entries, want %d", len(got), len(want))
	}
	for path, e := range want {
		g := got[path]
		if g == nil {
			t.Errorf("%s is missing after the round trip", path)
			continue
		}
		if g.Type != e.Type {
			t.Errorf("%s is a %s after the round trip, want %s", path, rgdTypeName(g.Type), rgdTypeName(e.Type))
		}
		if g.RefString() != e.RefString() {
			t.Errorf("%s extends %q after the round trip, want %q", path, g.RefString(), e.RefString())
		}
		if !g.IsTable() && g.DisplayValue() != e.DisplayValue() {
			t.Errorf("%s is %q after the round trip, want %q", path, g.DisplayValue(), e.DisplayValue())
		}
	}
}

// TestNewUnitRgdCarriesTheGivenName is what makes the dialog worth having: the
// values a user would otherwise have to type are filled from the name and the
// race the unit was created under.
func TestNewUnitRgdCarriesTheGivenName(t *testing.T) {
	rf := newUnitRgd(unitTemplateName{Race: "space_marines", Name: "marine_veteran"})
	entries := templateEntries(rf)
	want := map[string]string{
		"GameData/entity_blueprint_ext/animator":                     "Races/Space_Marines/Troops/Marine_Veteran",
		"GameData/ui_ext/ui_hotkey_name":                             "marine_veteran",
		"GameData/ui_ext/ui_info/icon_name":                          "space_marine_icons/marine_veteran_icon",
		"GameData/health_ext/hitpoints":                              "1000",
		"GameData/health_ext/armour":                                 "100",
		"GameData/sight_ext/sight_radius":                            "25",
		"GameData/moving_ext/speed_max":                              "4",
		"GameData/cost_ext/time_cost/cost/requisition":               "250",
		"GameData/cost_ext/time_cost/time_seconds":                   "60",
		"GameData/ui_ext/ui_info/screen_name_id":                     "$0",
		"GameData/type_ext/type_surface":                             refTypeSurface,
		"GameData/special_attack_physics_ext/disable_special_attack": "True",
	}
	for path, expect := range want {
		e := entries[path]
		if e == nil {
			t.Errorf("a new unit has no %s", path)
			continue
		}
		got := e.RefString()
		if got == "" {
			got = e.DisplayValue()
		}
		if got != expect {
			t.Errorf("%s is %q, want %q", path, got, expect)
		}
	}
	if path := `Data\attrib\ebps\races\space_marines\troops\marine_veteran.rgd`; (unitTemplateName{Race: "space_marines", Name: "marine_veteran"}).unitTemplateVirtualPath() != path {
		t.Errorf("the unit would be written to the wrong place")
	}
}

// TestNewUnitRgdInventsNothingTheGameDoesNotAskFor is the test the template has
// to pass to be worth having. It reads every unit of the installed game, takes
// the attributes they all have, and requires the generated file to use nothing
// else: a name the game does not use, a type it does not use, or a section that
// is not one of the twelve would all be caught here.
func TestNewUnitRgdInventsNothingTheGameDoesNotAskFor(t *testing.T) {
	tree := openTestDXP3(t)
	cat := buildUnitCatalog(tree)
	var troops []unitEntry
	for _, r := range cat.races {
		troops = append(troops, r.troops...)
	}
	if len(troops) < 100 {
		t.Skipf("only %d units in the installed game, too few to compare against", len(troops))
	}
	stats, unreadable := rgdCommonStats(tree, troops)
	if len(unreadable) > 0 {
		t.Fatalf("%d of %d units could not be read, so the comparison would be against a part of the game", len(unreadable), len(troops))
	}
	files := 0
	for _, a := range stats {
		if a.files > files {
			files = a.files
		}
	}
	common := map[string]*rgdAttrStat{}
	for path, a := range stats {
		if a.files == files {
			common[path] = a
		}
	}
	rf := newUnitRgd(unitTemplateName{Race: "necrons", Name: "necron_wraith"})
	entries := templateEntries(rf)
	missing := 0
	for path, e := range entries {
		c, ok := common[path]
		if !ok {
			missing++
			if missing <= 25 {
				t.Errorf("a new unit has %s (%s %s), which is not an attribute every one of the %d units has",
					path, rgdTypeName(e.Type), e.RefString(), files)
			}
			continue
		}
		if c.types[e.Type]*10 < files*9 {
			// Not "in every unit": the game's own files are not perfectly
			// uniform — help_text_list/text_02 is a string in one unit of 432 and
			// a wide string in the rest. A template has to pick the common one.
			best, bestN := uint32(0), 0
			for typ, n := range c.types {
				if n > bestN {
					best, bestN = typ, n
				}
			}
			t.Errorf("%s is a %s in the template; the %d units use %s for it %d times",
				path, rgdTypeName(e.Type), files, rgdTypeName(best), bestN)
		}
	}
	if len(entries) == 0 {
		t.Fatal("the template has no entries at all")
	}
	t.Logf("template: %d attributes, all of them shared by %d real units", len(entries), files)
}
