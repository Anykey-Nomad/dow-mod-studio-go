package whm

import "testing"

// TestVisibleInUnionsTheGivenClips — состояние постройки собирается из
// нескольких клипов: каждый добавляет свои видимые меши, чужие имена и
// отсутствующие клипы не мешают, а список без единого клипа модели — не
// состояние вовсе.
func TestVisibleInUnionsTheGivenClips(t *testing.T) {
	m := &Model{
		Meshes: []Mesh{{Name: "body"}, {Name: "flag"}, {Name: "mast"}},
		Animations: []Animation{
			{Name: "vis_base", Meshes: []MeshTrack{
				{Name: "body", Force: 1},
				{Name: "flag"},
				{Name: "mast"},
			}},
			{Name: "vis_banner", Meshes: []MeshTrack{
				{Name: "body", Force: 1},
				{Name: "flag", Force: 1},
			}},
		},
	}
	visible, ok := m.VisibleIn([]string{"vis_base", "vis_banner", "no_such_clip"})
	if !ok {
		t.Fatal("VisibleIn не нашёл ни одного клипа из списка")
	}
	if len(visible) != 2 || !visible["body"] || !visible["flag"] {
		t.Fatalf("видно %v, want body и flag", visible)
	}
	if got, ok := m.VisibleIn([]string{"no_such_clip"}); ok || got != nil {
		t.Fatalf("неизвестный клип = (%v, %v), want (nil, false)", got, ok)
	}
	if got, ok := m.VisibleIn(nil); ok || got != nil {
		t.Fatalf("пустой список = (%v, %v), want (nil, false)", got, ok)
	}
}
