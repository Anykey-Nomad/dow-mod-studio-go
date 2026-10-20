package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A small mod of our own, for the things the installed game cannot show.
//
// The game's own data is the right yardstick for "does this read the game
// correctly", but it is the wrong place to test what a mod author does: the
// installed army has its units wired up in ways that a new mod's test units are
// not, and its files live in archives that cannot be written to. So the tests
// that are about the mod's own work build a tree of their own — two units, one
// building that builds one of them, and a weapon the other one uses.

// The fixture's files, by virtual path. A unit is given the sections the graph
// reads, and nothing else: a test of the graph does not need a whole unit, and
// a fixture that carried everything would be a second copy of the template to
// keep true.
var (
	graphTestArmoury  = `Data\attrib\ebps\races\necrons\structures\necron_armoury.rgd`
	graphTestGauntlet = `Data\attrib\ebps\races\necrons\troops\necron_gauntlet.rgd`
	graphTestLoose    = `Data\attrib\ebps\races\necrons\troops\necron_loose.rgd`
	graphTestGauss    = `Data\attrib\weapon\necron_gauss.rgd`
	// An ability and an add-on that both name the gauntlet, in the shapes the
	// game writes them: the ability through a modifier table nested under its
	// area effect, the add-on through a modifier table of its own. They are
	// here because a freeze that only ever reads units misses these, and
	// moving the gauntlet out of the mod would leave both files pointing at a
	// file that is no longer there.
	graphTestWarp   = `Data\attrib\abilities\necron_warp_ability.rgd`
	graphTestHubris = `Data\attrib\addons\necron_hubris.rgd`
)

// graphTestRgdFiles is the content of the fixture, built once.
func graphTestRgdFiles(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		graphTestArmoury: mustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
		}),
		// The gauntlet says which structure builds it, in the shape the game
		// writes it, and carries a weapon.
		graphTestGauntlet: mustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				rgdTemplateTable("requirement_ext", refRequirementExt,
					rgdTemplateTable("requirements", refRequirementsList,
						rgdTemplateTable("required_1", refRequirementSlot,
							rgdTemplateString("structure_name", `ebps\races\necrons\structures\necron_armoury.lua`),
						),
					),
				),
			}},
		}),
		graphTestLoose: mustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
		}),
		graphTestGauss: mustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
		}),
		// The ability's modifier table is written three tables deep, under
		// area_effect.weapon_damage, and its leaf is target_type_name. A rule
		// that matched only the add-on's shape would miss this one.
		graphTestWarp: mustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				rgdTemplateTable("area_effect", "",
					rgdTemplateTable("weapon_damage", "",
						rgdTemplateTable("modifiers", "",
							rgdTemplateTable("modifier_01", "",
								rgdTemplateTable("modifier", "",
									rgdTemplateString("target_type_name", `necron_gauntlet`),
								),
							),
						),
					),
				),
				rgdTemplateString("ui_hotkey_name", "necron_warp_ability"),
			}},
		}),
		// The add-on names the same unit, one table shallower, and unlocks
		// itself from a building. Both halves are references, and they mean
		// different things, which is why the referrer's kind is part of the
		// rule.
		graphTestHubris: mustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				rgdTemplateTable("modifiers", "",
					rgdTemplateTable("modifier_01", "",
						rgdTemplateString("target_type_name", `necron_gauntlet`),
					),
				),
				rgdTemplateTable("requirements", "",
					rgdTemplateTable("required_1", "",
						rgdTemplateString("structure_name", `necron_armoury`),
					),
				),
			}},
		}),
	}
}

func mustMarshalGraph(t *testing.T, f *rgd.RgdFile) []byte {
	t.Helper()
	data, err := f.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

// appendBrokenUnit adds a unit that names a weapon with the given reference,
// in the shape the game writes a weapon in: a hardpoint, its weapon table, and
// the weapon slot inside it. It is how a fixture gets a reference that points
// at nothing.
func appendBrokenUnit(t *testing.T, tree *modtree.ModTree, virtualPath, weaponRef string) {
	t.Helper()
	body := mustMarshalGraph(t, &rgd.RgdFile{
		HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
		Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
			rgdTemplateTable("combat_ext", refCombatExt,
				rgdTemplateTable("hardpoints", refHardpointTable,
					rgdTemplateTable("hardpoint_01", refHardpointDetails,
						rgdTemplateTable("weapon_table", refWeaponTable,
							rgdTemplateTable("weapon_01", refWeaponDetails,
								rgdTemplatePosition("muzzle"),
								rgdTemplateString("name_for_this_weapon_choice", ""),
								rgdTemplatePosition("origin"),
								rgdTemplateString("weapon", weaponRef),
							),
						),
					),
				),
			),
		}},
	})
	writeTreeFile(t, tree, virtualPath, body)
}

// sleepMillis is the pause a background scan is given between checks.
func sleepMillis() { time.Sleep(5 * time.Millisecond) }

// newGraphTestMod builds a mod holding the fixture, with every file a real file
// on disk so that a freeze has something it can move.
func newGraphTestMod(t *testing.T) *modtree.ModTree {
	t.Helper()
	appPath := t.TempDir()
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "GraphTestMod"}
	t.Cleanup(tree.Close)
	for virtual, data := range graphTestRgdFiles(t) {
		writeTreeFile(t, tree, virtual, data)
	}
	tree.SortAll(tree.Root)
	return tree
}

// writeTreeFile puts one file into the fixture mod, on disk and in the tree at
// once, so that the two never disagree about what the mod holds.
func writeTreeFile(t *testing.T, tree *modtree.ModTree, virtualPath string, data []byte) {
	t.Helper()
	rel := strings.TrimPrefix(virtualPath, `Data\`)
	// The tree path is virtual and uses backslashes on every OS; on Linux a
	// backslash is an ordinary file name character, so both the disk path and
	// the segment walk have to split on it explicitly.
	slashRel := strings.ReplaceAll(rel, `\`, "/")
	disk := filepath.Join(tree.AppPath, "GraphTestMod", "Data", filepath.FromSlash(slashRel))
	if err := os.MkdirAll(filepath.Dir(disk), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, data, 0644); err != nil {
		t.Fatal(err)
	}
	// The tree looks a file up by walking path segments, so the folders are
	// created one segment at a time.
	src := &modtree.FileSource{Kind: modtree.KindThisMod, DiskRoot: filepath.Join(tree.AppPath, "GraphTestMod", "Data")}
	folder := tree.Root
	segs := strings.Split(slashRel, "/")
	for _, seg := range segs[:len(segs)-1] {
		folder = tree.GetOrCreateFolder(folder, seg, src)
	}
	name := segs[len(segs)-1]
	f := &modtree.TreeFile{Name: name, FullPath: virtualPath, Source: src, DiskPath: disk, SGAFileID: -1}
	folder.Files = append(folder.Files, f)
	folder.FileByLower[name] = f
}
