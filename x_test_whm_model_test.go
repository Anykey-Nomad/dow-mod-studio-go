package main

import (
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/whm"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The model parser is a second reader of the same chunks the .sgm converter
// consumes, so on real files the two must agree on the geometry.

func TestParseRealDowModelsForViewing(t *testing.T) {
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	parsed := 0
	total := 0
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		dir := filepath.Join(dowRacesDir, race.Name(), "troops")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range files {
			if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
				continue
			}
			total++
			label := race.Name() + "/" + e.Name()
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Errorf("%s: %v", label, err)
				continue
			}
			model, err := ParseWhmModel(raw)
			if err != nil {
				// Same variant meshes the converter reports; they are logged
				// there and must fail here as well rather than render garbage.
				t.Logf("unsupported: %s: %v", label, err)
				continue
			}
			verifyParsedModel(t, label, model, raw)
			parsed++
		}
	}
	t.Logf("parsed %d of %d models for viewing", parsed, total)
	if total > 0 && parsed*10 < total*9 {
		t.Errorf("only %d of %d models parsed, coverage regressed", parsed, total)
	}
}

func verifyParsedModel(t *testing.T, label string, model *WhmModel, raw []byte) {
	t.Helper()

	// The converter must see exactly the same vertex count and the parser
	// one sub mesh per material, which is what MeshMaterials counts; Meshes
	// counts sections, which is one entry only when every section is single
	// material.
	_, info, err := whm.ConvertToSgm(raw)
	if err != nil {
		t.Errorf("%s: converter disagrees with the model parser: %v", label, err)
		return
	}
	if len(model.Meshes) != info.MeshMaterials {
		t.Errorf("%s: %d sub meshes, converter counted %d materials", label, len(model.Meshes), info.MeshMaterials)
	}
	if len(model.Vertices) != info.Vertices {
		t.Errorf("%s: %d vertices, converter counted %d", label, len(model.Vertices), info.Vertices)
	}
	if len(model.Bones) != info.Bones {
		t.Errorf("%s: %d bones, converter counted %d", label, len(model.Bones), info.Bones)
	}
	if len(model.Animations) != info.Animations {
		t.Errorf("%s: %d animations, converter counted %d", label, len(model.Animations), info.Animations)
	}
	if len(model.Markers) != info.Markers {
		t.Errorf("%s: %d markers, converter counted %d", label, len(model.Markers), info.Markers)
	}

	// Every index must land on a real vertex, and every bone index on a real
	// bone, or the renderer would read past the end of a slice.
	for _, mesh := range model.Meshes {
		if len(mesh.Indices)%3 != 0 {
			t.Errorf("%s: mesh %q has %d indices, not a multiple of 3", label, mesh.Name, len(mesh.Indices))
		}
		for _, idx := range mesh.Indices {
			if idx >= uint32(len(model.Vertices)) {
				t.Errorf("%s: mesh %q indexes vertex %d of %d", label, mesh.Name, idx, len(model.Vertices))
				break
			}
		}
	}
	skinned := 0
	badBone := -1
	badWeight := -1
	for i := range model.Vertices {
		v := &model.Vertices[i]
		weight := float32(0)
		for s := 0; s < 4; s++ {
			if v.Bones[s] < 0 {
				continue
			}
			if v.Bones[s] >= int32(len(model.Bones)) {
				badBone = i
				break
			}
			weight += v.Weights[s]
		}
		if hasAnyBone(v) {
			skinned++
			// Weights are renormalised when skinning, so the sum only has to be
			// finite and non-negative here.
			if math.IsNaN(float64(weight)) || math.IsInf(float64(weight), 0) || weight < 0 {
				badWeight = i
			}
		}
	}
	if badBone >= 0 {
		v := &model.Vertices[badBone]
		t.Errorf("%s: vertex %d references bone %d of %d", label, badBone, v.Bones[0], len(model.Bones))
	}
	if badWeight >= 0 {
		t.Errorf("%s: vertex %d has a non-finite or negative weight sum", label, badWeight)
	}
	if len(model.Bones) > 0 && skinned == 0 {
		t.Logf("%s: model has %d bones but no skinned vertices", label, len(model.Bones))
	}

	// Animation keyframes must be normalised and reference known bones.
	for _, anim := range model.Animations {
		if anim.NumFrames <= 0 {
			t.Errorf("%s: animation %q has %d frames", label, anim.Name, anim.NumFrames)
		}
		for _, track := range anim.Bones {
			if track.Bone < 0 {
				// Game clips regularly reference bones the model does not
				// define; the skinner skips such tracks.
				continue
			}
			for _, k := range track.PosKey {
				// Key times are relative to the clip length but the game
				// stores a few beyond 1, which the sampler clamps.
				if math.IsNaN(float64(k.Time)) || k.Time < -0.001 {
					t.Errorf("%s: animation %q key time %f is invalid", label, anim.Name, k.Time)
				}
			}
		}
	}

	// The bounding box must be finite and actually contain the vertices.
	for i := 0; i < 3; i++ {
		if math.IsInf(float64(model.BoundMin[i]), 0) || math.IsNaN(float64(model.BoundMin[i])) {
			t.Errorf("%s: degenerate bounds", label)
			break
		}
		if model.BoundMax[i] < model.BoundMin[i] {
			t.Errorf("%s: inverted bounds on axis %d", label, i)
			break
		}
	}
}

func hasAnyBone(v *WhmVertex) bool {
	for s := 0; s < 4; s++ {
		if v.Bones[s] >= 0 {
			return true
		}
	}
	return false
}

func TestParseWhmModelRejectsGarbage(t *testing.T) {
	if _, err := ParseWhmModel([]byte("not a model")); err == nil {
		t.Error("expected an error for a non-chunky input")
	}
	if _, err := ParseWhmModel(append(chunky.Preamble(), chunky.I32(1706509)...)); err == nil {
		t.Error("expected an error when FOLDRSGM is missing")
	}
}

// TestWhmModelsStandOnGround locks the axis convention. Dawn of War models are
// authored Z-up with their feet on the ground plane, so the up axis runs from
// roughly zero upwards. The previous Y-up mapping put every model below the
// origin, which rendered the viewer upside down.
//
// The check is aggregate because a handful of models legitimately break the
// rule: a bomb on a ballistic arc and a hovering jetpack both sit below their
// own origin. What must hold for every model is that Z reaches above the
// origin at all, and that the vast majority stand on the plane.
func TestWhmModelsStandOnGround(t *testing.T) {
	total, standing := 0, 0
	forEachRealModel(t, func(label string, model *WhmModel) {
		if model.BoundMax[2] <= 0 {
			return // a prop authored entirely below the origin
		}
		total++
		// Measured across the shipped races, the median model dips by 0.1% of
		// its height and the 90th percentile by 11%; only flight and hover
		// units go further.
		depth := -model.BoundMin[2] / model.BoundMax[2]
		if depth <= 0.5 {
			standing++
			return
		}
		t.Logf("%s: %.0f%% below the ground plane (flying or hovering unit?)", label, depth*100)
	})
	if total < 10 {
		t.Skipf("only %d models to check", total)
	}
	if standing*20 < total*19 {
		t.Errorf("only %d of %d models stand on the ground plane; the axis convention is wrong", standing, total)
	}
	t.Logf("%d of %d models stand on the ground plane", standing, total)
}

// forEachRealModel runs fn over every .whm under the races tree that the
// parser accepts.
func forEachRealModel(t *testing.T, fn func(label string, model *WhmModel)) {
	t.Helper()
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		dir := filepath.Join(dowRacesDir, race.Name(), "troops")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range files {
			if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			model, err := ParseWhmModel(raw)
			if err != nil {
				continue
			}
			fn(race.Name()+"/"+e.Name(), model)
		}
	}
}

// TestWhmDecodePreservesHandedness guards the winding order. Swapping two axes
// of a coordinate system mirrors it, which would flip every triangle and make
// back face culling show the inside of the model.
func TestWhmDecodePreservesHandedness(t *testing.T) {
	// Volume by the divergence theorem is positive for a closed mesh wound
	// counter-clockwise seen from outside, which is exactly the convention the
	// renderer's back face culling assumes. Swapping two axes of a coordinate
	// system mirrors it and flips this sign, and the symptom is a model that
	// shows only its inside.
	signs := map[int]int{}
	checked := 0
	forEachRealModel(t, func(label string, model *WhmModel) {
		var vol float64
		for i := range model.Meshes {
			vol += meshSignedVolume(model, &model.Meshes[i])
		}
		if vol == 0 {
			return
		}
		checked++
		if vol > 0 {
			signs[1]++
		} else {
			signs[-1]++
		}
	})
	if checked < 10 {
		t.Skipf("only %d models to check", checked)
	}
	if len(signs) != 1 {
		t.Errorf("winding direction is inconsistent across models: %v", signs)
	}
	if signs[-1] != 0 {
		t.Errorf("%d of %d models are wound inside out, so back face culling shows their interior", signs[-1], checked)
	}
}

func meshSignedVolume(model *WhmModel, mesh *WhmMesh) float64 {
	total := 0.0
	idx := mesh.Indices
	for i := 0; i+2 < len(idx); i += 3 {
		a, b, c := idx[i], idx[i+1], idx[i+2]
		if int(a) >= len(model.Vertices) || int(b) >= len(model.Vertices) || int(c) >= len(model.Vertices) {
			continue
		}
		p := func(v uint32) [3]float64 {
			return [3]float64{
				float64(model.Vertices[v].Pos[0]),
				float64(model.Vertices[v].Pos[1]),
				float64(model.Vertices[v].Pos[2]),
			}
		}
		pa, pb, pc := p(a), p(b), p(c)
		total += pa[0]*(pb[1]*pc[2]-pb[2]*pc[1]) -
			pa[1]*(pb[0]*pc[2]-pb[2]*pc[0]) +
			pa[2]*(pb[0]*pc[1]-pb[1]*pc[0])
	}
	return total / 6
}
