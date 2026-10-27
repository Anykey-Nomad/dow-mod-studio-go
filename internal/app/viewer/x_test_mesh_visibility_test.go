package viewer_test

import (
	"testing"

	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/whm"
)

// TestMeshVisibilityTogglesAndMeshAlphas checks the eye toggle's core: the
// shared hidden set is matched case-insensitively, the mesh list collapses
// sub meshes that share a name while keeping model order, and meshAlphas —
// the one map both the software and the hardware renderer read — forces a
// hidden mesh to 0 so it is not drawn.
func TestMeshVisibilityTogglesAndMeshAlphas(t *testing.T) {
	// The hidden set is shared package state; start and end clean so no other
	// test sees what this one hides.
	viewer.HiddenMeshes = map[string]bool{}
	defer func() { viewer.HiddenMeshes = map[string]bool{} }()

	model := &uistate.WhmModel{Model: whm.Model{Meshes: []whm.Mesh{
		{Name: "body"},
		{Name: "gun"},
		{Name: "body"}, // the second sub mesh of the same name
	}}}

	names := viewer.ModelMeshNames(model)
	if len(names) != 2 || names[0] != "body" || names[1] != "gun" {
		t.Fatalf("modelMeshNames = %v, want [body gun] with the duplicate collapsed", names)
	}

	v := viewer.NewModelViewer(model, "")
	// Nothing hidden and no clip playing: nil means every mesh is drawn whole.
	if got := viewer.MeshAlphas(v); got != nil {
		t.Fatalf("meshAlphas with nothing hidden = %v, want nil", got)
	}

	viewer.SetMeshHidden("BODY", true) // stored lower case, matched case-insensitively
	if !viewer.MeshHidden("body") || !viewer.MeshHidden("Body") {
		t.Error("a mesh hidden under one casing is not hidden under another")
	}
	alphas := viewer.MeshAlphas(v)
	if a, ok := alphas["body"]; !ok || a != 0 {
		t.Errorf("hidden mesh alpha = %v (present %v), want 0 present", a, ok)
	}
	if _, ok := alphas["gun"]; ok {
		t.Error("a shown mesh was forced into the alpha map")
	}

	viewer.SetMeshHidden("body", false)
	if viewer.MeshHidden("body") {
		t.Error("showing a mesh left it hidden")
	}
	if got := viewer.MeshAlphas(v); got != nil {
		t.Fatalf("meshAlphas after showing = %v, want nil", got)
	}
}
