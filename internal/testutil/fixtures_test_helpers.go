package testutil

// Shared mod-graph test fixtures: the fixture mod the graph, freeze and unit
// tests all build their scenarios on, and the small helpers that write it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"image"
)

// ─── merged from x_test_modgraph_fixture_test.go ────────────────────────────────────────

var (
	GraphTestArmoury  = `Data\attrib\ebps\races\necrons\structures\necron_armoury.rgd`
	GraphTestGauntlet = `Data\attrib\ebps\races\necrons\troops\necron_gauntlet.rgd`
	GraphTestLoose    = `Data\attrib\ebps\races\necrons\troops\necron_loose.rgd`
	GraphTestGauss    = `Data\attrib\weapon\necron_gauss.rgd`
	// An ability and an add-on that both name the gauntlet, in the shapes the
	// game writes them: the ability through a modifier table nested under its
	// area effect, the add-on through a modifier table of its own. They are
	// here because a freeze that only ever reads units misses these, and
	// moving the gauntlet out of the mod would leave both files pointing at a
	// file that is no longer there.
	GraphTestWarp   = `Data\attrib\abilities\necron_warp_ability.rgd`
	GraphTestHubris = `Data\attrib\addons\necron_hubris.rgd`
)

// graphTestRgdFiles is the content of the fixture, built once.
func graphTestRgdFiles(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		GraphTestArmoury: MustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
		}),
		// The gauntlet says which structure builds it, in the shape the game
		// writes it, and carries a weapon.
		GraphTestGauntlet: MustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				unitdef.RgdTemplateTable("requirement_ext", unitdef.RefRequirementExt,
					unitdef.RgdTemplateTable("requirements", unitdef.RefRequirementsList,
						unitdef.RgdTemplateTable("required_1", unitdef.RefRequirementSlot,
							unitdef.RgdTemplateString("structure_name", `ebps\races\necrons\structures\necron_armoury.lua`),
						),
					),
				),
			}},
		}),
		GraphTestLoose: MustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
		}),
		GraphTestGauss: MustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable},
		}),
		// The ability's modifier table is written three tables deep, under
		// area_effect.weapon_damage, and its leaf is target_type_name. A rule
		// that matched only the add-on's shape would miss this one.
		GraphTestWarp: MustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				unitdef.RgdTemplateTable("area_effect", "",
					unitdef.RgdTemplateTable("weapon_damage", "",
						unitdef.RgdTemplateTable("modifiers", "",
							unitdef.RgdTemplateTable("modifier_01", "",
								unitdef.RgdTemplateTable("modifier", "",
									unitdef.RgdTemplateString("target_type_name", `necron_gauntlet`),
								),
							),
						),
					),
				),
				unitdef.RgdTemplateString("ui_hotkey_name", "necron_warp_ability"),
			}},
		}),
		// The add-on names the same unit, one table shallower, and unlocks
		// itself from a building. Both halves are references, and they mean
		// different things, which is why the referrer's kind is part of the
		// rule.
		GraphTestHubris: MustMarshalGraph(t, &rgd.RgdFile{
			HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
			Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
				unitdef.RgdTemplateTable("modifiers", "",
					unitdef.RgdTemplateTable("modifier_01", "",
						unitdef.RgdTemplateString("target_type_name", `necron_gauntlet`),
					),
				),
				unitdef.RgdTemplateTable("requirements", "",
					unitdef.RgdTemplateTable("required_1", "",
						unitdef.RgdTemplateString("structure_name", `necron_armoury`),
					),
				),
			}},
		}),
	}
}

func MustMarshalGraph(t *testing.T, f *rgd.RgdFile) []byte {
	t.Helper()
	data, err := f.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

// AppendBrokenUnit adds a unit that names a weapon with the given reference,
// in the shape the game writes a weapon in: a hardpoint, its weapon table, and
// the weapon slot inside it. It is how a fixture gets a reference that points
// at nothing.
func AppendBrokenUnit(t *testing.T, tree *modtree.ModTree, virtualPath, weaponRef string) {
	t.Helper()
	body := MustMarshalGraph(t, &rgd.RgdFile{
		HeaderVersion: 1, HeaderUnknown3: 1, ChunkVersion: 1,
		Root: &rgd.RgdEntry{Hash: rgd.Hash("GameData"), Name: "GameData", Type: rgd.RgdTypeTable, Children: []*rgd.RgdEntry{
			unitdef.RgdTemplateTable("combat_ext", unitdef.RefCombatExt,
				unitdef.RgdTemplateTable("hardpoints", unitdef.RefHardpointTable,
					unitdef.RgdTemplateTable("hardpoint_01", unitdef.RefHardpointDetails,
						unitdef.RgdTemplateTable("weapon_table", unitdef.RefWeaponTable,
							unitdef.RgdTemplateTable("weapon_01", unitdef.RefWeaponDetails,
								unitdef.RgdTemplatePosition("muzzle"),
								unitdef.RgdTemplateString("name_for_this_weapon_choice", ""),
								unitdef.RgdTemplatePosition("origin"),
								unitdef.RgdTemplateString("weapon", weaponRef),
							),
						),
					),
				),
			),
		}},
	})
	WriteTreeFile(t, tree, virtualPath, body)
}

// SleepMillis is the pause a background scan is given between checks.
func SleepMillis() { time.Sleep(5 * time.Millisecond) }

// NewGraphTestMod builds a mod holding the fixture, with every file a real file
// on disk so that a freeze has something it can move.
func NewGraphTestMod(t *testing.T) *modtree.ModTree {
	t.Helper()
	appPath := t.TempDir()
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil), AppPath: appPath, FileMapName: "GraphTestMod"}
	t.Cleanup(tree.Close)
	for virtual, data := range graphTestRgdFiles(t) {
		WriteTreeFile(t, tree, virtual, data)
	}
	tree.SortAll(tree.Root)
	return tree
}

// WriteTreeFile puts one file into the fixture mod, on disk and in the tree at
// once, so that the two never disagree about what the mod holds.
func WriteTreeFile(t *testing.T, tree *modtree.ModTree, virtualPath string, data []byte) {
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

// RenderFrame lays the function out over a fresh window and returns the
// screenshot, so a test can look at what was painted rather than at what the
// code says it painted. It skips the test when no GPU backend is available.
func RenderFrame(t *testing.T, w, h int, draw func(gtx layout.Context)) *image.RGBA {
	t.Helper()
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	var ops op.Ops
	gtx := NewLayoutContext(&ops, w, h)
	draw(gtx)

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

// ImagesDifferIn counts the pixels that are not the same in two shots of the
// same box.
// imagesDifferIn counts the pixels that are not the same in two shots of the
// same box. Scrolling has to change what is drawn; a bar that reported an offset
// but drew the same tabs would be indistinguishable from one that worked.
func ImagesDifferIn(a, b *image.RGBA, box image.Rectangle) int {
	box = box.Intersect(a.Bounds()).Intersect(b.Bounds())
	diff := 0
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				diff++
			}
		}
	}
	return diff
}
