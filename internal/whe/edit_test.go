package whe

import (
	"bytes"
	"strings"
	"testing"

	"dow-mod-studio/internal/chunky"
)

// editWhe собирает маленький .whe для тестов правок: два таймлайна (у первого
// есть хвост воспроизведения) и два действия, одно из которых вызывает другое.
func editWhe() []byte {
	mton := func(name string, tail []byte) *chunky.Chunk {
		payload := (&Mton{Animations: []string{name + "_clip"}, trailing: tail}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 4, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	tail := []byte{1, 0, 0, 0, 2, 0, 0, 0, 3}
	mt.Children = append(mt.Children,
		mton("walk", tail),
		mton("idle", nil),
	)
	acts := []FxAction{
		{Name: "default", Subactions: []FxActionLink{{Name: "idle_act", Compare: FxCompareNone}}},
		{Name: "idle_act", Motions: []FxActionLink{{Name: "idle", Compare: FxCompareNone}}},
	}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Version: 1, Payload: MarshalActs(acts)},
		mt,
	}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	return buf.Bytes()
}

func editTimelineNames(t *testing.T, doc *Document) []string {
	t.Helper()
	var out []string
	for _, tl := range doc.Timelines() {
		out = append(out, tl.Name)
	}
	return out
}

func editActionNames(doc *Document) []string {
	var out []string
	for _, a := range doc.Actions() {
		out = append(out, a.Name)
	}
	return out
}

// Таймлайн уходит из motion-папки один: остальные остаются на месте, файл
// после обратной сериализации читается снова, а имя, которого нет, беречь не
// за что — оно отклоняется, а не считается пустой операцией.
func TestRemoveTimelineTakesOnlyItsOwnChunkOut(t *testing.T) {
	doc, err := ParseDocument(editWhe())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	// Имя таймлайна сравнивается без учёта регистра, как в остальных
	// методах документа.
	if err := doc.RemoveTimeline("WALK"); err != nil {
		t.Fatalf("RemoveTimeline: %v", err)
	}
	got := editTimelineNames(t, doc)
	if len(got) != 1 || got[0] != "idle" {
		t.Fatalf("после удаления таймлайнов = %v, want [idle]", got)
	}

	back, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("обратная сериализация: %v", err)
	}
	again, err := ParseDocument(back)
	if err != nil {
		t.Fatalf("перечитывание после удаления: %v", err)
	}
	if got := editTimelineNames(t, again); len(got) != 1 || got[0] != "idle" {
		t.Errorf("после перечитывания таймлайнов = %v, want [idle]", got)
	}
	if _, ok := again.TimelinePayload("walk"); ok {
		t.Error("удалённый таймлайн остался в файле после перечитывания")
	}

	if err := doc.RemoveTimeline("walk"); err == nil {
		t.Error("повторное удаление того же таймлайна прошло молча")
	}
	if err := doc.RemoveTimeline("   "); err == nil {
		t.Error("пустое имя таймлайна принято")
	}
}

// Действие уходит из DATAACTS одно: остальные записи и их связи остаются как
// были, и файл после обратной сериализации читается снова. Ссылку на него
// само действие не чинит — отказ за ней остаётся за редактором.
func TestRemoveActionDropsOnlyThatRecord(t *testing.T) {
	doc, err := ParseDocument(editWhe())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	if err := doc.RemoveAction("idle_act"); err != nil {
		t.Fatalf("RemoveAction: %v", err)
	}
	got := editActionNames(doc)
	if len(got) != 1 || got[0] != "default" {
		t.Fatalf("после удаления действий = %v, want [default]", got)
	}
	if acts := doc.Actions(); len(acts) != 1 || len(acts[0].Subactions) != 1 {
		t.Errorf("связи оставшегося действия изменились: %+v", acts)
	}

	back, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("обратная сериализация: %v", err)
	}
	again, err := ParseDocument(back)
	if err != nil {
		t.Fatalf("перечитывание после удаления: %v", err)
	}
	if got := editActionNames(again); len(got) != 1 || got[0] != "default" {
		t.Errorf("после перечитывания действий = %v, want [default]", got)
	}

	if err := doc.RemoveAction("idle_act"); err == nil {
		t.Error("повторное удаление того же действия прошло молча")
	}
	if err := doc.RemoveAction(""); err == nil {
		t.Error("пустое имя действия принято")
	}
}

// Новый таймлайн — не пустой заголовок: он несёт хвост воспроизведения,
// взятый у таймлайна этого же файла, иначе у него нет ни зацикливания, ни
// переходов, которые игра читает из конца DATAMTON.
func TestNewTimelineCarriesTheFilePlaybackTail(t *testing.T) {
	doc, err := ParseDocument(editWhe())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	wantTail := []byte{1, 0, 0, 0, 2, 0, 0, 0, 3}
	payload := doc.NewTimelinePayload([]string{"new_clip"})
	m, err := ParseMton("new_timeline", payload)
	if err != nil {
		t.Fatalf("payload нового таймлайна не разбирается: %v", err)
	}
	if len(m.Animations) != 1 || m.Animations[0] != "new_clip" {
		t.Errorf("клипы нового таймлайна = %v, want [new_clip]", m.Animations)
	}
	if !bytes.Equal(m.trailing, wantTail) {
		t.Errorf("хвост воспроизведения = %v, want %v — он берётся у таймлайна файла", m.trailing, wantTail)
	}

	if err := doc.AddTimeline("new_timeline", payload); err != nil {
		t.Fatalf("AddTimeline: %v", err)
	}
	if err := doc.AddTimeline("new_timeline", payload); err == nil {
		t.Error("таймлайн с уже существующим именем добавлен повторно")
	}
	if len(doc.NewTimelinePayload(nil)) == 0 {
		t.Error("таймлайн без клипов получает пустой payload")
	}

	back, err := doc.MarshalBinary()
	if err != nil {
		t.Fatalf("обратная сериализация: %v", err)
	}
	again, err := ParseDocument(back)
	if err != nil {
		t.Fatalf("перечитывание после добавления: %v", err)
	}
	saved, ok := again.TimelinePayload("new_timeline")
	if !ok {
		t.Fatal("нового таймлайна нет в файле после перечитывания")
	}
	m2, err := ParseMton("new_timeline", saved)
	if err != nil {
		t.Fatalf("сохранённый таймлайн не разбирается: %v", err)
	}
	if len(m2.Animations) != 1 || m2.Animations[0] != "new_clip" {
		t.Errorf("после перечитывания клипы = %v, want [new_clip]", m2.Animations)
	}
	if !bytes.Equal(m2.trailing, wantTail) {
		t.Errorf("после перечитывания хвост = %v, want %v", m2.trailing, wantTail)
	}
	if names := editTimelineNames(t, again); len(names) != 3 || !containsFold(names, "new_timeline") {
		t.Errorf("таймлайнов = %v, want три с новым", names)
	}
}

func containsFold(names []string, want string) bool {
	for _, n := range names {
		if strings.EqualFold(n, want) {
			return true
		}
	}
	return false
}
