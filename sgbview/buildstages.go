package sgbview

import (
	"fmt"
	"math"
	"strings"

	"dow-mod-studio/internal/whe"
)

// BuildStage — этап стройки из действия build: клип/таймлайн, диапазон
// прогресса в процентах и то, что этот этап играет.
//
// Игра описывает стройку не одним клипом: действие build выбирает клип по
// условию build_progress_stage_N, а самые первые секунды стройки — это
// отдельный клип с phase_in. Здесь это разбирается в плоский список этапов,
// чтобы сцена играла их на своих порогах, не зная про граф действий.
type BuildStage struct {
	// Motion — имя моушена действия (оно же обычно имя таймлайна).
	Motion string
	// Min и Max — проценты прогресса 0..100; HasRange=false — этап играет
	// при появлении постройки, без порога (audio_build, build_placement).
	Min, Max float64
	HasRange bool
	Sounds   []SoundEvent
	Effects  []FXEvent
}

// progressSpan — диапазон прогресса из условий .whe.
type progressSpan struct {
	min, max       float64
	minSet, maxSet bool
}

func (s progressSpan) has() bool { return s.minSet || s.maxSet }

// merge наследует незаданные границы: дочерний этап уточняет диапазон
// родительского, а не заменяет его целиком.
func (s progressSpan) merge(other progressSpan) progressSpan {
	out := s
	if other.minSet {
		out.min, out.minSet = other.min, true
	}
	if other.maxSet {
		out.max, out.maxSet = other.max, true
	}
	return out
}

// BuildStages разбирает действие build модели в этапы стройки: моушены
// верхнего действия и вложенных поддействий, с диапазонами прогресса из
// условий и звуками/эффектами их таймлайнов.
func (a *Anim) BuildStages() []BuildStage {
	if a == nil || a.doc == nil {
		return nil
	}
	build, ok := findWHAction(a.doc.Actions(), "build")
	if !ok {
		return nil
	}
	conditions := map[string]whe.FxCondition{}
	for _, condition := range a.doc.Conditions() {
		conditions[strings.ToLower(strings.TrimSpace(condition.Name))] = condition
	}
	clauses := map[string]whe.FxClause{}
	for _, clause := range a.doc.Clauses() {
		clauses[strings.ToLower(strings.TrimSpace(clause.Name))] = clause
	}

	var out []BuildStage
	seen := map[string]bool{}
	var walk func(action whe.FxAction, span progressSpan, depth int)
	walk = func(action whe.FxAction, span progressSpan, depth int) {
		if depth > 5 {
			return
		}
		for _, link := range action.Motions {
			child := span
			if link.HasCondition() {
				child = child.merge(progressSpanOf(link.Condition, conditions, clauses))
			}
			stage, ok := a.buildStageForMotion(link.Name, child)
			if !ok {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(stage.Motion)) + "|" + fmt.Sprint(stage.HasRange, stage.Min, stage.Max)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, stage)
		}
		for _, link := range action.Subactions {
			child := span
			if link.HasCondition() {
				child = child.merge(progressSpanOf(link.Condition, conditions, clauses))
			}
			sub, ok := findWHAction(a.doc.Actions(), link.Name)
			if !ok {
				continue
			}
			walk(sub, child, depth+1)
		}
	}
	walk(build, progressSpan{min: 0, max: 100, maxSet: true}, 0)
	return out
}

// buildStageForMotion собирает этап по моушену: его таймлайн, звуки и
// эффекты. Этапы без отношения к стройке (idle и подсветка) отбрасываются:
// действие build тянет их за собой, но играть их при закладке не нужно.
func (a *Anim) buildStageForMotion(motion string, span progressSpan) (BuildStage, bool) {
	stage := BuildStage{Motion: strings.TrimSpace(motion), Max: 100}
	if span.has() {
		stage.HasRange = true
		stage.Min = span.min
		if span.maxSet {
			stage.Max = span.max
		}
	}
	timeline := timelineForMotion(a.doc, motion)
	if timeline == nil {
		return BuildStage{}, false
	}
	stage.Sounds = fxSoundsOfTimelines(a.doc, func(tl whe.FxTimeline) bool {
		return strings.EqualFold(tl.Name, timeline.Name)
	}, true)
	stage.Effects = fxEventsOfTimeline(a.doc, *timeline)
	if !buildStageRelevant(stage.Motion, timeline, stage.Sounds, stage.Effects) {
		return BuildStage{}, false
	}
	return stage, true
}

// buildStageRelevant — этап относится к появлению и стройке. Действие build
// включает в себя и обычные клипы (idle, vis_master): их звуки — фон, а не
// стройка, и на порогах они не нужны.
func buildStageRelevant(motion string, timeline *whe.FxTimeline, sounds []SoundEvent, effects []FXEvent) bool {
	values := []string{motion, timeline.Name, strings.Join(timeline.Animations, " ")}
	for _, sound := range sounds {
		values = append(values, sound.Name+" "+sound.Sound)
	}
	for _, effect := range effects {
		values = append(values, effect.Name+" "+effect.Effect)
	}
	for _, value := range values {
		if looksLikeBuild(value) {
			return true
		}
	}
	return false
}

// looksLikeBuild — имя, звук или путь эффекта говорит о постройке/фазе.
func looksLikeBuild(value string) bool {
	lower := strings.ToLower(value)
	for _, word := range []string{"build", "phase", "placement", "prebuild", "warp_in"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// progressSpanOf переводит условие .whe в диапазон build_progress.
// Ограничения подписаны _min/_max; без подписи сторону задаёт сравнение
// (2 и 3 — верхняя граница, 4 и 5 — нижняя), как их пишет редактор игры.
func progressSpanOf(name string, conditions map[string]whe.FxCondition, clauses map[string]whe.FxClause) progressSpan {
	condition, ok := conditions[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return progressSpan{}
	}
	var span progressSpan
	matched := false
	for _, clauseName := range condition.ClauseNames {
		clause, ok := clauses[strings.ToLower(strings.TrimSpace(clauseName))]
		if !ok || !strings.EqualFold(strings.TrimSpace(clause.Variable), "build_progress") {
			continue
		}
		matched = true
		value := float64(clause.FloatValue)
		lower := strings.Contains(strings.ToLower(clause.Name), "min")
		upper := strings.Contains(strings.ToLower(clause.Name), "max")
		switch {
		case lower:
			if !span.minSet || value > span.min {
				span.min = value
			}
			span.minSet = true
		case upper:
			if !span.maxSet || value < span.max {
				span.max = value
			}
			span.maxSet = true
		case clause.Comparison == 2 || clause.Comparison == 3:
			if !span.maxSet || value < span.max {
				span.max = value
			}
			span.maxSet = true
		default:
			if !span.minSet || value > span.min {
				span.min = value
			}
			span.minSet = true
		}
	}
	if matched && !span.maxSet {
		// Верх не задан в conditions (stage_N_min без max) — стройка упирается
		// в 100 %, дальше этап не меняется.
		span.max = 100
		span.maxSet = true
	}
	return span
}

// findWHAction ищет действие по имени без регистра.
func findWHAction(actions []whe.FxAction, name string) (whe.FxAction, bool) {
	for _, action := range actions {
		if strings.EqualFold(strings.TrimSpace(action.Name), strings.TrimSpace(name)) {
			return action, true
		}
	}
	return whe.FxAction{}, false
}

// timelineForMotion находит таймлайн моушена: по имени, затем по списку
// моушенов и анимаций таймлайна.
func timelineForMotion(doc *whe.Document, motion string) *whe.FxTimeline {
	want := strings.ToLower(strings.TrimSpace(motion))
	if doc == nil || want == "" {
		return nil
	}
	timelines := doc.Timelines()
	for i := range timelines {
		if strings.EqualFold(strings.TrimSpace(timelines[i].Name), want) {
			return &timelines[i]
		}
	}
	for i := range timelines {
		for _, name := range timelines[i].Motions {
			if strings.EqualFold(strings.TrimSpace(name), want) {
				return &timelines[i]
			}
		}
	}
	for i := range timelines {
		for _, name := range timelines[i].Animations {
			if strings.EqualFold(strings.TrimSpace(name), want) {
				return &timelines[i]
			}
		}
	}
	return nil
}

// BuildStageMax — предел прогресса этапа (100, если верх не задан).
func (s BuildStage) BuildStageMax() float64 {
	if s.Max > 0 && !math.IsNaN(s.Max) {
		return s.Max
	}
	return 100
}
