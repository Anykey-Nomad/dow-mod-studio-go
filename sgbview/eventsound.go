package sgbview

import (
	"sort"
	"strings"

	"dow-mod-studio/internal/whe"
)

// SoundEvent — звук, который .whe привязывает к моменту клипа.
//
// События таймлайнов несут не только эффекты: смерть юнита — это ещё крик,
// удар тела и лязг, выстрел — свой звук оружия. Всё это записано в тех же
// DATAEVNT-определениях ключом sound, а время — секунды от начала клипа.
type SoundEvent struct {
	Name  string
	Sound string
	Clip  string
	Time  float64
}

// TimelineSounds отдаёт звуки, которые .whe играет во время клипа.
func (a *Anim) TimelineSounds(clip string) []SoundEvent {
	if a == nil || a.doc == nil || strings.TrimSpace(clip) == "" {
		return nil
	}
	return fxSoundsForClips(a.doc, []string{clip})
}

// DeathSounds отдаёт звуки гибели модели: крик, удар тела, падение. Один и
// тот же звук из разных вариантов смерти схлопывается: юнит умирает один
// раз, и кричать он должен один раз.
func (a *Anim) DeathSounds() []SoundEvent {
	if a == nil || a.doc == nil {
		return nil
	}
	if a.death != nil {
		if out := fxSoundsForClips(a.doc, []string{a.death.Name}); len(out) > 0 {
			return out
		}
	}
	return fxSoundsOfTimelines(a.doc, fxDeathTimeline)
}

// ShotSounds отдаёт звуки стрельбы: то, что .whe играет в таймлайнах,
// ведущих клипы огня. Пусто — у модели выстрел без звука.
func (a *Anim) ShotSounds() []SoundEvent {
	if a == nil || a.doc == nil || len(a.shotClips) == 0 {
		return nil
	}
	return fxSoundsForClips(a.doc, a.shotClips)
}

// fxSoundsForClips собирает звуки таймлайнов, которые ведут перечисленные
// клипы.
func fxSoundsForClips(doc *whe.Document, clips []string) []SoundEvent {
	if doc == nil || len(clips) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(clips))
	for _, clip := range clips {
		clip = strings.ToLower(strings.TrimSpace(clip))
		if clip != "" {
			wanted[clip] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	return fxSoundsOfTimelines(doc, func(timeline whe.FxTimeline) bool {
		return fxTimelineRunsClip(timeline, wanted)
	})
}

// fxSoundsOfTimelines собирает звуки подходящих таймлайнов. Одинаковые
// пути звука не повторяются: файл описывает категорию, а не конкретный
// sample, и повтор того же пути — это тот же крик, а не второй.
func fxSoundsOfTimelines(doc *whe.Document, keep func(whe.FxTimeline) bool) []SoundEvent {
	if doc == nil {
		return nil
	}
	defs := doc.Events()
	var out []SoundEvent
	seen := map[string]int{}
	for _, timeline := range doc.Timelines() {
		if keep != nil && !keep(timeline) {
			continue
		}
		for _, timed := range timeline.Events {
			key := strings.ToLower(strings.TrimSpace(timed.Name))
			if key == "" {
				continue
			}
			event, ok := defs[key]
			if !ok {
				continue
			}
			sound, ok := event.First("sound")
			if !ok || strings.TrimSpace(sound) == "" {
				continue
			}
			ev := SoundEvent{
				Name:  eventName(event, timed.Name),
				Sound: sound,
				Clip:  timeline.Name,
				Time:  fxEventTime(timed.Time),
			}
			lower := strings.ToLower(strings.TrimSpace(sound))
			if at, ok := seen[lower]; ok {
				if ev.Time < out[at].Time {
					out[at].Time = ev.Time
				}
				continue
			}
			seen[lower] = len(out)
			out = append(out, ev)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}
