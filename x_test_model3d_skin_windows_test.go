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

// changedPixels counts the pixels that differ between two readbacks, ignoring
// the background so a camera that has not moved does not count as a change.
func changedPixels(a, b []byte, w, h int) int {
	n := 0
	for i := 0; i+3 < len(a) && i+3 < len(b); i += 4 {
		if a[i] == bgR && a[i+1] == bgG && a[i+2] == bgB {
			continue
		}
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			n++
		}
	}
	return n
}

// TestGpuSkeletonMovesTheAttachedMeshes checks on the hardware path that a
// skeleton with moving bones actually moves the geometry bound to them, which
// is the part a still frame cannot show.
func TestGpuSkeletonMovesTheAttachedMeshes(t *testing.T) {
	model := requireGpuTestModel(t, "lychguard.whm")
	if len(model.Animations) == 0 {
		t.Skip("no clips")
	}
	const w, h = 200, 200
	r, readback, release := requireGpuRenderer(t)
	defer release()
	opts := gpuTestOptions(model, w, h)

	// A frame in which nothing moves must come out identical, which is the
	// baseline the moving frame is measured against.
	rest := model.RestPose()
	if _, err := r.Render(model, rest, opts, nil, nil); err != nil {
		t.Fatalf("Render rest: %v", err)
	}
	baseline := readback()

	best, bestName, bestAt := 0, "", float32(0)
	for i := range model.Animations {
		clip := &model.Animations[i]
		for _, at := range []float32{0.25, 0.5, 0.75} {
			if _, err := r.Render(model, model.PoseAt(clip, at), opts, nil, nil); err != nil {
				t.Fatalf("Render %s: %v", clip.Name, err)
			}
			if n := changedPixels(baseline, readback(), w, h); n > best {
				best, bestName, bestAt = n, clip.Name, at
			}
		}
	}
	t.Logf("busiest clip %q at t=%.2f moves %d pixels of %d", bestName, bestAt, best, w*h)
	if best == 0 {
		t.Errorf("no clip in %s moved a single pixel: the skeleton is not deforming the mesh", model.Name)
	}
}
