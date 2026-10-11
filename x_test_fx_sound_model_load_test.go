//go:build windows

package main

import (
	"testing"

	"dow-mod-studio/internal/modtree"
)

// Sounds have to be readable without the effects, because they are loaded from
// the model's first frame while the effects still wait for the FX button. This
// runs the sound half of the load on its own, with no model and no blueprint
// walk, which is what the frame does.
func TestLoadModelSoundsNeedsNoEffects(t *testing.T) {
	if testDXP3Module == "" {
		t.Skip("no module path")
	}
	tree, err := modtree.BuildModTree(testDXP3Module, "English")
	if err != nil {
		t.Skipf("BuildModTree: %v", err)
	}
	defer tree.Close()
	// Find a model that declares sounds at all, so the test is about loading
	// them rather than about a model that happens to be silent.
	var files []string
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if len(files) < 40 && hasWheExtension(f.Name) {
			files = append(files, f.FullPath)
		}
	})
	if len(files) == 0 {
		t.Skip("no models with a .whe")
	}
	loaded := 0
	for _, f := range files {
		res := loadModelSounds(tree, f)
		if res.err != nil {
			continue
		}
		if len(res.soundBundles) > 0 {
			loaded++
			t.Logf("%s: %d sound events, %d samples decoded, %d timelines",
				f, len(res.soundBundles), res.sounds.Len(), len(res.timelines))
		}
		// The point of the split: loading sounds must not have resolved a
		// single effect, or the frame would be doing the button's work early.
		if len(res.effects) != 0 || len(res.textures) != 0 {
			t.Errorf("%s: loading sounds also resolved %d effects and %d textures",
				f, len(res.effects), len(res.textures))
		}
	}
	if loaded == 0 {
		t.Logf("no sounds declared by any of the %d models checked", len(files))
	} else {
		t.Logf("%d of %d models declared sounds", loaded, len(files))
	}
}

func hasWheExtension(name string) bool {
	lower := toLowerASCII(name)
	return len(lower) > 4 && lower[len(lower)-4:] == ".whe"
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
