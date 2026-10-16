//go:build windows

package main

import (
	"testing"

	"github.com/fogleman/fauxgl"
)

// TestMarkerSpaceSettledByBounds loads real models, prints their markers and
// counts how many land inside the model bounds (with a small margin) under
// each reading of the marker position. The reading that puts markers on the
// model is the one the game uses, and everything downstream — where an effect
// plays — depends on getting it right.
func TestMarkerSpaceSettledByBounds(t *testing.T) {
	checked := 0
	for _, name := range []string{"Necron_Summoning_Core.whm", "necron_monolith.whm", "necron_tomb_sviva.whm"} {
		raw := findDowFile(name)
		if raw == nil {
			continue
		}
		model, err := ParseWhmModel(raw)
		if err != nil {
			t.Fatalf("ParseWhmModel(%s): %v", name, err)
		}
		if len(model.Markers) == 0 {
			t.Logf("%s: no markers", name)
			continue
		}
		checked++
		pose := model.RestPose()
		min := vec3From(model.BoundMin)
		max := vec3From(model.BoundMax)
		// Markers such as muzzles legitimately stick out a little.
		grow := max.Sub(min).MulScalar(0.2)
		min = min.Sub(grow)
		max = max.Add(grow)
		inside := func(p fauxgl.Vector) bool {
			return p.X >= min.X && p.X <= max.X &&
				p.Y >= min.Y && p.Y <= max.Y &&
				p.Z >= min.Z && p.Z <= max.Z
		}
		var inBoneLocal, inBindSpace int
		t.Logf("%s: %d markers, bounds (%.1f %.1f %.1f)..(%.1f %.1f %.1f)",
			name, len(model.Markers), min.X, min.Y, min.Z, max.X, max.Y, max.Z)
		for i, mk := range model.Markers {
			pBone, _ := model.MarkerWorld(i, pose, true)
			pBind, _ := model.MarkerWorld(i, pose, false)
			if inside(pBone) {
				inBoneLocal++
			}
			if inside(pBind) {
				inBindSpace++
			}
			if i < 8 {
				t.Logf("  %-24s parent=%-16s pos=(%.2f %.2f %.2f) boneLocal=(%.2f %.2f %.2f)%v bind=(%.2f %.2f %.2f)%v",
					mk.Name, mk.Parent, mk.Pos[0], mk.Pos[1], mk.Pos[2],
					pBone.X, pBone.Y, pBone.Z, inside(pBone),
					pBind.X, pBind.Y, pBind.Z, inside(pBind))
			}
		}
		t.Logf("%s: inside bounds: bone-local %d/%d, bind-space %d/%d",
			name, inBoneLocal, len(model.Markers), inBindSpace, len(model.Markers))
		// The game reads a marker in its parent bone's frame; a bind space
		// reading lands almost every marker outside the model it belongs to.
		if inBindSpace > inBoneLocal {
			t.Errorf("%s: bind-space reading fits better (%d vs %d) — marker space needs rethinking",
				name, inBindSpace, inBoneLocal)
		}
	}
	if checked == 0 {
		t.Skip("no installed model with markers found")
	}
}
