//go:build windows

package main

import (
	"math"
	"testing"
)

// TestSkinWeightsAreComplete pins the skin block layout. The weights and the
// bone ids are interleaved per vertex, three floats then four bytes; reading
// them as two flat arrays instead walked the block with the wrong stride and
// left a fifth of every mesh unweighted.
func TestSkinWeightsAreComplete(t *testing.T) {
	for _, name := range []string{"lychguard.whm", "necron_lord_tgd.whm", "builder_scarab.whm", "necron_energy_core.whm"} {
		model := requireGpuTestModel(t, name)
		var unweighted, partial, outOfRange int
		for i := range model.Vertices {
			v := &model.Vertices[i]
			total := 0.0
			for k := 0; k < 4; k++ {
				if v.Weights[k] > 0 {
					total += float64(v.Weights[k])
				}
				if v.Bones[k] >= int32(len(model.Bones)) {
					outOfRange++
				}
			}
			switch {
			case total <= 0:
				unweighted++
			case math.Abs(total-1) > 0.01:
				partial++
			}
		}
		if unweighted > 0 {
			t.Errorf("%s: %d of %d vertices carry no weight at all, so they never move",
				name, unweighted, len(model.Vertices))
		}
		if partial > 0 {
			t.Errorf("%s: %d vertices have weights summing to %.3f instead of 1", name, partial, 0.0)
		}
		if outOfRange > 0 {
			t.Errorf("%s: %d weights point past the %d bone skeleton", name, outOfRange, len(model.Bones))
		}
	}
}

// TestEveryMeshHasGeometry catches the other half of the complaint: a mesh that
// parses but carries no triangles is invisible in the preview.
func TestEveryMeshHasGeometry(t *testing.T) {
	for _, name := range []string{"lychguard.whm", "necron_lord_tgd.whm", "builder_scarab.whm", "necron_energy_core.whm"} {
		model := requireGpuTestModel(t, name)
		for _, m := range model.Meshes {
			if len(m.Indices) < 3 {
				t.Errorf("%s: mesh %q has %d indices, it cannot draw anything", name, m.Name, len(m.Indices))
			}
			for _, idx := range m.Indices {
				if int(idx) >= len(model.Vertices) {
					t.Errorf("%s: mesh %q indexes vertex %d of %d", name, m.Name, idx, len(model.Vertices))
					break
				}
			}
		}
	}
}
