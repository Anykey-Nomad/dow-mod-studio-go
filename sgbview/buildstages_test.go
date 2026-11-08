package sgbview

import (
	"testing"

	"dow-mod-studio/internal/whe"
)

// TestProgressSpanOf — подписи _min/_max задают стороны диапазона, а без них
// сторону берёт сравнение.
func TestProgressSpanOf(t *testing.T) {
	conditions := map[string]whe.FxCondition{
		"stage_1": {Name: "stage_1", ClauseNames: []string{"stage_1_max", "stage_1_min"}},
		"stage_2": {Name: "stage_2", ClauseNames: []string{"stage_2_min"}},
		"broken":  {Name: "broken", ClauseNames: []string{"damage"}},
	}
	clauses := map[string]whe.FxClause{
		"stage_1_max": {Name: "stage_1_max", Variable: "build_progress", Comparison: 3, FloatValue: 66},
		"stage_1_min": {Name: "stage_1_min", Variable: "build_progress", Comparison: 5, FloatValue: 0},
		"stage_2_min": {Name: "stage_2_min", Variable: "build_progress", Comparison: 4, FloatValue: 66},
		"damage":      {Name: "damage", Variable: "damage", Comparison: 4, FloatValue: 50},
	}
	first := progressSpanOf("stage_1", conditions, clauses)
	if !first.minSet || first.min != 0 || !first.maxSet || first.max != 66 {
		t.Fatalf("stage_1: %+v", first)
	}
	second := progressSpanOf("stage_2", conditions, clauses)
	if !second.minSet || second.min != 66 || !second.maxSet || second.max != 100 {
		t.Fatalf("stage_2: %+v", second)
	}
	if span := progressSpanOf("broken", conditions, clauses); span.has() {
		t.Fatalf("чужое условие дало диапазон: %+v", span)
	}
	if span := progressSpanOf("missing", conditions, clauses); span.has() {
		t.Fatalf("нет условия, а диапазон есть: %+v", span)
	}
}

// TestProgressSpanMerge — дочерний этап уточняет только свою границу.
func TestProgressSpanMerge(t *testing.T) {
	parent := progressSpan{min: 0, minSet: true, max: 100, maxSet: true}
	child := parent.merge(progressSpan{max: 66, maxSet: true})
	if child.min != 0 || child.max != 66 {
		t.Fatalf("merge: %+v", child)
	}
	keep := parent.merge(progressSpan{})
	if keep.min != 0 || keep.max != 100 {
		t.Fatalf("пустое уточнение испортило диапазон: %+v", keep)
	}
}

// TestProgressSpanRootIsNotStage — корневой диапазон 0..100 существует только
// как граница наследования: audio_build и vis_master, висящие на действии без
// условий, не должны становиться этапами 0..100.
func TestProgressSpanRootIsNotStage(t *testing.T) {
	root := progressSpan{max: 100, maxSet: true}
	if root.has() {
		t.Fatalf("корневой span стал этапом: %+v", root)
	}
	child := root.merge(progressSpan{min: 12, max: 66, minSet: true, maxSet: true, set: true})
	if !child.has() || child.min != 12 || child.max != 66 {
		t.Fatalf("наследование сломало этап: %+v", child)
	}
}

// TestLooksLikeBuild — этапами считаются только стройка и фаза появления.
func TestLooksLikeBuild(t *testing.T) {
	if !looksLikeBuild(`races\necron\structures\building_start\build_start`) {
		t.Fatal("build_start не признан стройкой")
	}
	if !looksLikeBuild(`races\npc\necron_monolith\phase_in`) {
		t.Fatal("phase_in не признан появлением")
	}
	if looksLikeBuild(`races\necron\structures\monolith\idle_whole\idle`) {
		t.Fatal("idle признан стройкой")
	}
}
