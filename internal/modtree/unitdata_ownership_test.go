package modtree_test

import (
	"dow-mod-studio/internal/unitdef"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
)

func TestUnitCatalogListsInheritedAndOverriddenUnits(t *testing.T) {
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	add := func(path string, kind modtree.SourceKind) {
		t.Helper()
		parts := strings.Split(path, `\`)
		parts = parts[1:] // The tree root is already the virtual Data folder.
		folder := tree.Root
		source := &modtree.FileSource{Kind: kind}
		for _, part := range parts[:len(parts)-1] {
			folder = tree.GetOrCreateFolder(folder, part, source)
		}
		file := &modtree.TreeFile{Name: parts[len(parts)-1], FullPath: path, Source: source}
		folder.Files = append(folder.Files, file)
		folder.FileByLower[strings.ToLower(file.Name)] = file
	}

	const own = `Data\attrib\ebps\races\necrons\troops\my_new_unit.rgd`
	const required = `Data\attrib\ebps\races\necrons\troops\vanilla_unit.rgd`
	add(own, modtree.KindThisMod)
	add(required, modtree.KindReqMod)
	add(`Data\attrib\ebps\races\space_marines\structures\engine_unit.rgd`, modtree.KindEngine)

	cat := unitdef.BuildUnitCatalog(tree)
	units := cat.AllUnits()
	if len(units) != 3 {
		t.Fatalf("unit catalog has %d entries, want current-mod, required-mod and engine units: %+v", len(units), units)
	}
	got := map[string]modtree.SourceKind{}
	for _, u := range units {
		f := tree.FindFile(u.Path)
		if f == nil || f.Source == nil {
			t.Fatalf("catalog Path %q has no source in tree", u.Path)
		}
		got[u.Path] = f.Source.Kind
	}
	engine := `Data\attrib\ebps\races\space_marines\structures\engine_unit.rgd`
	if got[own] != modtree.KindThisMod || got[required] != modtree.KindReqMod || got[engine] != modtree.KindEngine {
		t.Fatalf("catalog ownership = %+v, want current mod %q, required mod %q and engine %q", got, own, required, engine)
	}
	necrons := cat.FindRace("necrons")
	if necrons == nil || necrons.UnitCount() != 2 {
		t.Fatalf("necrons race = %+v, want own and inherited units", necrons)
	}
}

func TestUADECatalogFindsInheritedAndModOwnedUnitsInLowercaseDataFolder(t *testing.T) {
	app := t.TempDir()
	writeModule := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(app, name+".module"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeModule("UA_DE", "[global]\nModFolder=UltimateApocalypse_THB_test\nDataFolder.1=Data\nRequiredMod.1=W40k\n")
	writeModule("W40k", "[global]\nModFolder=W40k\nDataFolder.1=Data\n")

	putUnit := func(modFolder, dataFolder, name string) {
		t.Helper()
		dir := filepath.Join(app, modFolder, dataFolder, "attrib", "ebps", "races", "necrons", "troops")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".rgd"), []byte("fixture"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// The UA_DE .module spells this "Data", while its files are actually in
	// lowercase "data" on a case-sensitive filesystem. Windows treats them as
	// the same directory; the module tree must do so too.
	putUnit("UltimateApocalypse_THB_test", "data", "ua_custom_unit")
	putUnit("W40k", "Data", "vanilla_unit")

	tree, err := modtree.BuildModTree(filepath.Join(app, "UA_DE.module"), "English")
	if err != nil {
		t.Fatalf("BuildModTree: %v", err)
	}
	defer tree.Close()

	const own = `Data\attrib\ebps\races\necrons\troops\ua_custom_unit.rgd`
	f := tree.FindFile(own)
	if f == nil || f.Source == nil || f.Source.Kind != modtree.KindThisMod {
		t.Fatalf("own unit lookup = %#v, want file from UA_DE", f)
	}
	cat := unitdef.BuildUnitCatalog(tree)
	units := cat.AllUnits()
	if len(units) != 2 {
		t.Fatalf("UA_DE catalog = %+v, want the mod-owned unit and inherited vanilla unit", units)
	}
	foundOwn, foundVanilla := false, false
	for _, u := range units {
		f := tree.FindFile(u.Path)
		if f == nil || f.Source == nil {
			t.Fatalf("catalog Path %q has no source", u.Path)
		}
		switch f.Source.Kind {
		case modtree.KindThisMod:
			foundOwn = foundOwn || u.Path == own
		case modtree.KindReqMod:
			foundVanilla = foundVanilla || u.Name == "vanilla_unit"
		}
	}
	if !foundOwn || !foundVanilla {
		t.Fatalf("UA_DE catalog ownership: own=%v inherited=%v; entries=%+v", foundOwn, foundVanilla, units)
	}
}
