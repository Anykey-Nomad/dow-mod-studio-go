package main

import "dow-mod-studio/internal/whm"

// boneIndexByName keeps the UI call site on WhmModel while the model logic
// itself lives alongside the parsed model in internal/whm.
func (m *WhmModel) boneIndexByName(name string) int {
	return m.Model.BoneIndexByName(name)
}

// modelMarkerForwards remains for existing same-package diagnostics; the
// cached marker-frame analysis is implemented by internal/whm.Model.
func modelMarkerForwards(m *WhmModel) []int {
	if m == nil {
		return nil
	}
	return whm.ModelMarkerForwards(&m.Model)
}
