package main

import (
	"bytes"
	"testing"

	"dow-mod-studio/internal/whm"
)

// The material path a save rewrites, and the mesh it belongs to in the fixture
// buildTestWhm builds: one FOLDMSLC named body_mesh carrying a single
// reference. These are the exact bytes testWhmMesh writes into the DATADATA.
const (
	testMeshName   = "body_mesh"
	testMeshOldMat = "art/ebps/races/marines/texture_share/marine_body"
)

// A save with nothing changed must leave the file byte for byte as it was:
// the rebuild copies the reference it is told to leave alone and the splice
// moves nothing.
func TestPatchWhmMeshMaterialPathsLeavesTheFileAloneWhenNothingChanges(t *testing.T) {
	file := buildTestWhm()
	patched, err := whm.PatchMeshMaterialPaths(file, testMeshName, nil)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !bytes.Equal(file, patched) {
		t.Fatalf("a save with no changed paths rewrote the file: %d bytes -> %d bytes", len(file), len(patched))
	}
}

// A changed reference writes, the file grows by exactly the difference
// between the two strings, the mesh reads back with the new path, and — the
// strong claim — patching straight back restores the original file byte for
// byte, which is what says no other part of the container was disturbed.
func TestPatchWhmMeshMaterialPathsWritesANewReference(t *testing.T) {
	file := buildTestWhm()
	model, err := whm.Parse(file)
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if len(model.Meshes) != 1 || model.Meshes[0].Material != testMeshOldMat {
		t.Fatalf("the fixture mesh came back as %+v, want one mesh with material %q", model.Meshes, testMeshOldMat)
	}
	const newPath = "art/ebps/races/marines/texture_share/replacement_map"
	patched, err := whm.PatchMeshMaterialPaths(file, testMeshName, []string{newPath})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if want := len(file) + len(newPath) - len(testMeshOldMat); len(patched) != want {
		t.Errorf("the patched file is %d bytes, want %d (original %d)", len(patched), want, len(file))
	}
	again, err := whm.Parse(patched)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	if len(again.Meshes) != 1 || again.Meshes[0].Material != newPath {
		t.Errorf("the mesh came back as %+v, want one mesh with material %q", again.Meshes, newPath)
	}
	// Everything but the one reference must round trip byte for byte.
	back, err := whm.PatchMeshMaterialPaths(patched, testMeshName, []string{testMeshOldMat})
	if err != nil {
		t.Fatalf("patch back: %v", err)
	}
	if !bytes.Equal(file, back) {
		for i := 0; i < len(file) && i < len(back); i++ {
			if file[i] != back[i] {
				t.Fatalf("patching back diverged at byte %d of %d (back is %d bytes, original %d)",
					i, len(file), len(back), len(file))
			}
		}
		t.Fatalf("patching back changed the file length: %d -> %d", len(file), len(back))
	}
}

// The refusals: a mesh the file does not carry, more paths than the mesh has
// materials, and a reference with a byte the material section could not read
// back — each would leave a broken or wrong file, so none may be written.
func TestPatchWhmMeshMaterialPathsRefusesBadInput(t *testing.T) {
	file := buildTestWhm()
	if _, err := whm.PatchMeshMaterialPaths(file, "no_such_mesh", []string{"x"}); err == nil {
		t.Error("a mesh the file does not carry saved without complaint")
	}
	if _, err := whm.PatchMeshMaterialPaths(file, testMeshName, []string{"a", "b"}); err == nil {
		t.Error("more paths than the mesh has materials saved without complaint")
	}
	if _, err := whm.PatchMeshMaterialPaths(file, testMeshName, []string{"bad\x01path"}); err == nil {
		t.Error("a reference with a non-printable byte saved without complaint")
	}
}
