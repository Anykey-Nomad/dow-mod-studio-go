//go:build windows || linux

package main

import "testing"

func TestModelGpuVkGeometryChangesAreDeferredUntilRender(t *testing.T) {
	model := &WhmModel{}
	mesh := &modelGpuVkMesh{draws: []modelGpuVkDraw{{count: 3}}}
	r := &modelGpuVkRenderer{
		meshes:      map[*WhmModel]*modelGpuVkMesh{model: mesh},
		dirtyMeshes: make(map[*WhmModel]bool),
	}

	r.geometryChanged(model)
	if r.meshes[model] != mesh || !r.dirtyMeshes[model] {
		t.Fatal("geometryChanged must retain the in-use mesh and mark it dirty for the next render")
	}
}

func TestModelGpuVkForgetRetiresTheCachedMesh(t *testing.T) {
	model := &WhmModel{}
	mesh := &modelGpuVkMesh{}
	r := &modelGpuVkRenderer{meshes: map[*WhmModel]*modelGpuVkMesh{model: mesh}}

	r.forget(model)
	if _, ok := r.meshes[model]; ok {
		t.Fatal("forgotten model remains in the GPU mesh cache")
	}
}
