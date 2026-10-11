package main

import (
	"dow-mod-studio/internal/fx"
	"os"
	"strings"
	"testing"

	"dow-mod-studio/internal/modtree"
)

// TestResolveFxEventsForRealModel walks the chain the game walks: entity
// blueprint → event reference → .events definition, for a model that has
// markers to play effects at. It skips when the install is not there.
func TestResolveFxEventsForRealModel(t *testing.T) {
	module := `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\DXP3.module`
	if _, err := os.Stat(module); err != nil {
		t.Skipf("module not available: %v", err)
	}
	tree, err := modtree.BuildModTree(module, "English")
	if err != nil {
		t.Skipf("BuildModTree: %v", err)
	}
	defer tree.Close()
	folders, files := tree.Count()
	t.Logf("tree: %d folders, %d files", folders, files)
	for _, probe := range []string{
		"necron_summoning_core.lua", "Necron_Summoning_Core.whm", "relocation.events",
		"monolith_hit.events", "hit_surface_01.lua",
	} {
		if f := tree.FindByName(probe); f != nil {
			t.Logf("probe %q -> %s", probe, f.FullPath)
		} else {
			t.Logf("probe %q -> not in tree", probe)
		}
	}
	for _, probe := range []string{
		"attrib/ebps/races/necrons/structures/necron_summoning_core.lua",
		"attrib/ebps/races/necrons/structures/Necron_Summoning_Core.lua",
	} {
		if f := tree.FindFile(probe); f != nil {
			t.Logf("FindFile %q -> %s", probe, f.FullPath)
		} else {
			t.Logf("FindFile %q -> nil", probe)
		}
	}
	if f := tree.FindByName("necron_summoning_core.lua"); f != nil {
		if data, err := tree.ReadTreeFile(f); err == nil {
			s := string(data)
			t.Logf("blueprint %s: %d bytes, event_name refs=%d, drives=%v",
				f.FullPath, len(data), len(fx.EventNamePattern.FindAll(data, -1)), strings.Contains(strings.ToLower(s), "necron_summoning_core]]"))
			t.Logf("blueprint content:\n%s", s)
			for _, m := range fx.EventNamePattern.FindAllStringSubmatch(s, -1) {
				t.Logf("  ref: %s", m[1])
			}
		}
	}
	for _, dir := range tree.Root.Children {
		t.Logf("root child: %s", dir.FullPath)
	}
	// Every file that basenames to the blueprint, plus a sample of what the
	// attrib folder actually holds.
	var named []string
	var attribSample []string
	attribLua := 0
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		lp := strings.ToLower(f.FullPath)
		if strings.EqualFold(f.Name, "necron_summoning_core.lua") ||
			strings.HasPrefix(strings.ToLower(f.Name), "necron_summoning_core.") {
			named = append(named, f.FullPath)
		}
		if strings.Contains(lp, `\attrib\`) {
			if strings.HasSuffix(lp, ".lua") {
				attribLua++
			}
			if len(attribSample) < 25 && strings.Contains(lp, "ebps") {
				attribSample = append(attribSample, f.FullPath)
			}
		}
	})
	t.Logf("files named necron_summoning_core.*: %v", named)
	t.Logf("attrib lua files: %d; ebps sample:", attribLua)
	for _, s := range attribSample {
		t.Logf("  %s", s)
	}
	if len(named) > 0 {
		// print the first blueprint-ish file's head
		for _, p := range named {
			if !strings.HasSuffix(strings.ToLower(p), ".rgd") {
				continue
			}
			if data, err := tree.ReadFile(p); err == nil {
				s := string(data)
				if i := strings.Index(s, "event_manager"); i >= 0 {
					end := i + 600
					if end > len(s) {
						end = len(s)
					}
					t.Logf("rgd %s event section:\n%s", p, s[i:end])
				} else {
					t.Logf("rgd %s: no event_manager (%d bytes)", p, len(data))
				}
			}
		}
	}
	bindings, model, err := fx.ResolveEventsForModel(tree,
		"art/ebps/races/necrons/structures/Necron_Summoning_Core.whm")
	if err != nil {
		t.Fatalf("fx.ResolveEventsForModel: %v", err)
	}
	t.Logf("model %s: %d events", model, len(bindings))
	withFX := 0
	for _, b := range bindings {
		fx, hasFX := b.Event.First("fx")
		if hasFX {
			withFX++
		}
		t.Logf("  %-40s event=%-24s fx=%s", b.Ref, b.Event.Name, fx)
	}
	if withFX == 0 {
		t.Errorf("no resolved event names an effect")
	}
}
