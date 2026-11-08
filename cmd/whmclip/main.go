// Command whmclip is a throwaway probe: it checks every animation clip's track
// names against the model's meshes and bones, and dumps the volkite clips.
package main

import (
	"fmt"
	"os"
	"strings"

	"dow-mod-studio/internal/whm"
)

const modelPath = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data\art\ebps\races\chaos\troops\chaos_predator_codex.whm`

func main() {
	data, err := os.ReadFile(modelPath)
	if err != nil {
		fail("read", err)
	}
	m, err := whm.Parse(data)
	if err != nil {
		fail("parse", err)
	}
	meshes := make(map[string]bool, len(m.Meshes))
	for _, mesh := range m.Meshes {
		meshes[strings.ToLower(strings.TrimSpace(mesh.Name))] = true
	}
	bones := make(map[string]bool, len(m.Bones))
	for _, b := range m.Bones {
		bones[strings.ToLower(strings.TrimSpace(b.Name))] = true
	}

	badMesh, badBone := 0, 0
	for _, anim := range m.Animations {
		for _, t := range anim.Meshes {
			if !meshes[strings.ToLower(strings.TrimSpace(t.Name))] {
				badMesh++
				fmt.Printf("clip %-38s unknown mesh track %q\n", anim.Name, t.Name)
			}
		}
		for _, t := range anim.Bones {
			if !bones[strings.ToLower(strings.TrimSpace(t.Name))] {
				badBone++
				fmt.Printf("clip %-38s unknown bone track %q\n", anim.Name, t.Name)
			}
		}
	}
	fmt.Printf("clips=%d unknownMeshTracks=%d unknownBoneTracks=%d\n", len(m.Animations), badMesh, badBone)

	for _, anim := range m.Animations {
		name := strings.ToLower(anim.Name)
		if !strings.Contains(name, "volkite") && !strings.Contains(name, "vis_weapon_main") {
			continue
		}
		fmt.Printf("\n=== clip %q frames=%d duration=%v bones=%d meshes=%d textures=%d\n",
			anim.Name, anim.NumFrames, anim.Duration, len(anim.Bones), len(anim.Meshes), len(anim.Textures))
		for _, t := range anim.Meshes {
			fmt.Printf("  %+v\n", t)
		}
	}
}

func fail(what string, err error) {
	fmt.Fprintln(os.Stderr, what+":", err)
	os.Exit(1)
}
