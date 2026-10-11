package main

import (
	"dow-mod-studio/internal/modtree"
	"path/filepath"
	"strings"
	"testing"
)

// searchTestTree builds a small merged tree by hand: three sources, folders
// and files that exercise name matches, folder-only matches and the kind
// filters, without touching the game's files.
func searchTestTree() *modtree.ModTree {
	tree := &modtree.ModTree{
		Root:        modtree.NewFolder("Data", "Data", nil),
		FileMapName: "TestMod",
	}
	this := &modtree.FileSource{ModName: "ThisMod", Kind: modtree.KindThisMod, Priority: 0}
	req := &modtree.FileSource{ModName: "RequiredMod", Kind: modtree.KindReqMod, Priority: 15000}
	eng := &modtree.FileSource{ModName: "(Engine)", Kind: modtree.KindEngine, Priority: 30001}

	add := func(folderPath, name string, src *modtree.FileSource) {
		cur := tree.Root
		for _, seg := range strings.Split(folderPath, "\\") {
			if seg == "" {
				continue
			}
			cur = tree.GetOrCreateFolder(cur, seg, nil)
		}
		f := &modtree.TreeFile{Name: name, FullPath: cur.FullPath + "\\" + name, Source: src}
		cur.Files = append(cur.Files, f)
		cur.FileByLower[strings.ToLower(name)] = f
	}

	structures := "attrib\\necrons\\structures"
	add(structures, "necron_monolith.rgd", this)
	add(structures, "monolith_door.rgd", this)
	add(structures, "monolith_door.fda", this)
	add(structures, "monolith_door.art", this)
	add("attrib\\necrons", "necron_lord_tgd.whm", req)
	add(structures, "necron_lord_tgd.rgd", eng)
	add("script\\ebps", "necron_death.lua", req)
	add("script\\ebps", "necrons.lua", this)
	add("art\\necrons\\texture_share", "necron_lord01_src_b", eng)
	add("art\\necrons\\texture_share", "necron_lord01_src_b.rsh", eng)
	return tree
}

func hitNames(hits []modtree.SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.File != nil {
			out = append(out, h.File.Name)
		}
	}
	return out
}

func hitPaths(hits []modtree.SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.File != nil {
			out = append(out, h.File.FullPath)
		}
	}
	return out
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestSearchFindsByNameAndFolder(t *testing.T) {
	tree := searchTestTree()

	byName := tree.Search("monolith", modtree.SearchAll, 0)
	names := hitNames(byName)
	if len(names) == 0 {
		t.Fatal("expected matches for \"monolith\"")
	}
	// A name that starts with the query outranks one that merely contains it,
	// so the three monolith_door files lead and necron_monolith follows.
	wantLead := []string{"monolith_door.art", "monolith_door.fda", "monolith_door.rgd"}
	for i, want := range wantLead {
		if names[i] != want {
			t.Errorf("hit %d for \"monolith\" = %q, want %q (names=%v)", i, names[i], want, names)
		}
	}
	if !containsString(names, "necron_monolith.rgd") {
		t.Errorf("necron_monolith.rgd missing from %v", names)
	}

	// "tgd" only appears in file names, never in a folder, so every hit must
	// be a name match and carry the word for the UI to highlight. It must also
	// stay a short list: the engine tree holds tens of thousands of files.
	tgd := tree.Search("tgd", modtree.SearchAll, 0)
	if len(tgd) == 0 {
		t.Fatal("expected matches for \"tgd\"")
	}
	if len(tgd) > 10 {
		t.Errorf("\"tgd\" matched %d files, which is far more than the four in the tree", len(tgd))
	}
	for _, h := range tgd {
		if h.MatchIn != "name" || h.MatchWord != "tgd" {
			t.Errorf("hit %q reported MatchIn=%q MatchWord=%q, want name/tgd", h.File.Name, h.MatchIn, h.MatchWord)
		}
	}

	// "texture_share" is a folder, so it matches by folder only.
	folderHits := tree.Search("texture_share", modtree.SearchAll, 0)
	if len(folderHits) == 0 {
		t.Fatal("expected matches for \"texture_share\"")
	}
	for _, h := range folderHits {
		if h.MatchIn != "folder" {
			t.Errorf("hit %q reported MatchIn=%q, want folder", h.File.Name, h.MatchIn)
		}
	}
}

func TestSearchIsCaseInsensitiveAndAcceptsWildcard(t *testing.T) {
	tree := searchTestTree()
	lowerCase := tree.Search("necron_monolith", modtree.SearchAll, 0)
	upperCase := tree.Search("NECRON_MONOLITH", modtree.SearchAll, 0)
	star := tree.Search("*necron_monolith", modtree.SearchAll, 0)
	if len(lowerCase) == 0 {
		t.Fatal("no hits for lower case query")
	}
	if len(lowerCase) != len(upperCase) || len(lowerCase) != len(star) {
		t.Fatalf("case and leading star changed the result count: %d, %d, %d", len(lowerCase), len(upperCase), len(star))
	}
}

func TestSearchMultipleWordsMustAllMatch(t *testing.T) {
	tree := searchTestTree()
	// "lord" is in the name, "necrons" is only in the folder, so the file has
	// to match across both places to be found.
	hits := tree.Search("lord necrons", modtree.SearchAll, 0)
	if len(hits) == 0 {
		t.Fatal("expected necron_lord_tgd files to match \"lord necrons\"")
	}
	// A word that is nowhere rules the file out.
	if got := tree.Search("lord zzzznotpresent", modtree.SearchAll, 0); len(got) != 0 {
		t.Errorf("expected no hits for an absent word, got %v", hitNames(got))
	}
}

func TestSearchFiltersByKind(t *testing.T) {
	tree := searchTestTree()
	// The tree holds one file of every filtered type, each with "necrons" in
	// its folder, so the query matches all of them and the filter is the only
	// thing that separates them.
	cases := []modtree.SearchKind{modtree.SearchRgd, modtree.SearchLua, modtree.SearchFda, modtree.SearchArt, modtree.SearchWhm}
	for _, kind := range cases {
		hits := tree.Search("necrons", kind, 0)
		if len(hits) == 0 {
			t.Errorf("filter %s found nothing for \"necrons\"", kind.Label())
			continue
		}
		for _, h := range hits {
			ext := strings.ToLower(filepath.Ext(h.File.Name))
			if !containsString(kind.Extensions(), ext) {
				t.Errorf("filter %s returned %q with extension %q", kind.Label(), h.File.Name, ext)
			}
		}
	}
	// A filter must not invent hits: no lua file in the tree mentions the
	// monolith, and the RGD filter must exclude the .fda and .art twins.
	if hits := tree.Search("monolith", modtree.SearchLua, 0); len(hits) != 0 {
		t.Errorf("lua filter returned monolith results: %v", hitNames(hits))
	}
	if hits := tree.Search("monolith_door", modtree.SearchRgd, 0); len(hits) != 1 || hits[0].File.Name != "monolith_door.rgd" {
		t.Errorf("rgd filter for monolith_door = %v, want only monolith_door.rgd", hitNames(hits))
	}
}

func TestSearchPutsThisModFirst(t *testing.T) {
	tree := searchTestTree()
	hits := tree.Search("necron_lord_tgd", modtree.SearchAll, 0)
	if len(hits) < 2 {
		t.Skipf("expected the same file name in several sources, got %v", hitPaths(hits))
	}
	paths := hitPaths(hits)
	engineAt := -1
	for i, h := range hits {
		if h.File.Source != nil && h.File.Source.Kind == modtree.KindEngine {
			engineAt = i
			break
		}
	}
	if engineAt == 0 {
		t.Errorf("engine file ranked first for %v", paths)
	}
}

func TestSearchRespectsLimitAndEmptyQuery(t *testing.T) {
	tree := searchTestTree()
	if hits := tree.Search("   ", modtree.SearchAll, 0); len(hits) != 0 {
		t.Errorf("blank query returned %d hits", len(hits))
	}
	hits := tree.Search("necrons", modtree.SearchAll, 2)
	if len(hits) != 2 {
		t.Fatalf("limit 2 returned %d hits", len(hits))
	}
	// A capped list must still be a prefix of the full one, or the "best
	// first" promise is broken.
	full := tree.Search("necrons", modtree.SearchAll, 0)
	for i := range hits {
		if hits[i].File.FullPath != full[i].File.FullPath {
			t.Errorf("hit %d differs between capped and full search: %q vs %q",
				i, hits[i].File.FullPath, full[i].File.FullPath)
		}
	}
}

// TestSearchRealModule runs the same ranking over the actual game data the
// user searches in, and checks that each filter finds its own kind of file.
func TestSearchRealModule(t *testing.T) {
	path := `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`
	if _, err := filepath.Abs(path); err != nil {
		t.Skipf("game data not available: %v", err)
	}
	tree, err := modtree.BuildModTree(path, "English")
	if err != nil {
		t.Fatalf("modtree.BuildModTree: %v", err)
	}
	defer tree.Close()

	hits := tree.Search("necron_lord_tgd", modtree.SearchAll, 0)
	if len(hits) == 0 {
		t.Fatal("expected necron_lord_tgd files in the merged tree")
	}
	t.Logf("necron_lord_tgd: %d hits, first 5:", len(hits))
	for i := 0; i < len(hits) && i < 5; i++ {
		t.Logf("  %s (%s, score=%d, in=%s)", hits[i].File.FullPath, hits[i].File.Source.ModName, hits[i].Score, hits[i].MatchIn)
	}
	if hits[0].MatchIn != "name" {
		t.Errorf("best hit %q matched in %q, want the name", hits[0].File.FullPath, hits[0].MatchIn)
	}

	// Every filter must find something in a real module, otherwise the chip is
	// dead weight in the UI.
	for _, kind := range []modtree.SearchKind{modtree.SearchRgd, modtree.SearchLua, modtree.SearchFda, modtree.SearchArt, modtree.SearchWhm} {
		all := tree.Search("necron", kind, 5)
		if len(all) == 0 {
			t.Errorf("filter %s found no necron files in the real module", kind.Label())
			continue
		}
		for _, h := range all {
			if !containsString(kind.Extensions(), strings.ToLower(filepath.Ext(h.File.Name))) {
				t.Errorf("filter %s returned %q", kind.Label(), h.File.Name)
			}
		}
		t.Logf("filter %s: %d sample hits, first %q", kind.Label(), len(all), all[0].File.Name)
	}
}

// ── the results panel staying down ──────────────────────────────────────────

// TestSearchPanelStaysDownUntilTheQueryChanges is the regression behind "no
// file opens and folders stop expanding". While the results panel is up it
// lays a dismiss catcher over the content below the field, so a panel that
// reopens by itself takes every click on the file tree with it. What decides
// the panel is the field's text, so a dismissed panel has to stay down while
// the text is the same — and must come back the moment the text differs.
func TestSearchPanelStaysDownUntilTheQueryChanges(t *testing.T) {
	ui := newUI(unitTestTheme())

	if ui.searchPanelWanted("") {
		t.Fatal("an empty field should not want the panel")
	}
	if !ui.searchPanelWanted("necron") {
		t.Fatal("typing a query should bring the panel up")
	}
	ui.searchVisible = true

	// The user clicks outside, which dismisses it. The query is kept in the
	// field so it can be refined, and the hold is recorded from that field.
	ui.searchEditor.SetText("necron")
	ui.closeSearch()
	if ui.searchVisible {
		t.Fatal("closeSearch left the panel on screen")
	}
	for i := 0; i < 5; i++ {
		if ui.searchPanelWanted("necron") {
			t.Fatalf("the panel sprang back open on frame %d with an unchanged query; "+
				"its dismiss catcher is now over the file tree", i)
		}
	}

	// Refining the query is how the user asks for the panel again.
	if !ui.searchPanelWanted("necron lord") {
		t.Error("changing the query should bring the panel back")
	}
}

// TestEmptyingTheSearchReleasesTheHold covers Escape: clearing the field has to
// drop the hold, or retyping the very same text would leave the panel down and
// look like the search had stopped working.
func TestEmptyingTheSearchReleasesTheHold(t *testing.T) {
	ui := newUI(unitTestTheme())
	ui.searchEditor.SetText("necron")
	ui.searchPanelWanted("necron")
	ui.closeSearch()

	if ui.searchPanelWanted("") {
		t.Error("an empty field should not want the panel")
	}
	if !ui.searchPanelWanted("necron") {
		t.Error("the same query typed again after clearing should bring the panel back")
	}
}

// TestEveryPathThatHidesThePanelHoldsItDown is what the menu and the language
// picker rely on. They call closeSearch without a dismiss click of their own,
// so if that did not hold the panel down it would spring back on the very next
// frame and quietly cover the content again.
func TestEveryPathThatHidesThePanelHoldsItDown(t *testing.T) {
	ui := newUI(unitTestTheme())
	ui.searchEditor.SetText("necron")
	ui.searchPanelWanted("necron")
	ui.searchVisible = true

	ui.closeSearch() // what a menu click and the language picker both do

	if ui.searchVisible {
		t.Error("closeSearch did not put the panel away")
	}
	if ui.searchPanelWanted(ui.searchEditor.Text()) {
		t.Error("a panel hidden from the menu bar sprang straight back up")
	}
}
