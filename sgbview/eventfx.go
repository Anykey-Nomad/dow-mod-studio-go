package sgbview

import (
	"math"
	"sort"
	"strings"

	"dow-mod-studio/internal/whe"
)

// FXEvent — эффект, который .whe привязывает к моменту клипа.
//
// Файл .whe описывает не только стрельбу: таймлайны смерти, ударов и
// падений несут события с путём эффекта и маркером модели (кровь на шее,
// искры попадания). В отличие от ShotFX у такого события есть время —
// секунды от начала клипа, — поэтому сцена запускает его не в кадре
// события, а тогда, когда клип до этого времени дошёл.
type FXEvent struct {
	Name          string
	Effect        string
	Marker        string
	Other         string
	OtherMarker   string
	SpansDistance bool
	AlignToOther  bool
	// Clip — таймлайн, в котором событие записано: по нему видно, к какому
	// действию оно относится.
	Clip string
	// Time — секунды от начала клипа.
	Time float64
}

// TimelineFX отдаёт эффекты, которые .whe играет во время клипа. Клип
// ищется по имени таймлайна и по его списку Animations — так же, как это
// делает разрешение эффектов стрельбы.
func (a *Anim) TimelineFX(clip string) []FXEvent {
	if a == nil || a.doc == nil || strings.TrimSpace(clip) == "" {
		return nil
	}
	return fxEventsForClips(a.doc, []string{clip})
}

// DeathFX отдаёт эффекты гибели модели: события её таймлайнов смерти.
//
// Клип смерти и таймлайн в .whe не всегда называются одинаково (клип
// die_1 против таймлайна syncdeath_1_shared), поэтому сначала ищется
// точное совпадение по клипу смерти, а если его нет — все таймлайны, в
// имени или анимациях которых есть смерть. Одинаковые эффекты из разных
// вариантов смерти схлопываются: юнит умирает один раз, и кровь должна
// брызнуть один раз.
func (a *Anim) DeathFX() []FXEvent {
	if a == nil || a.doc == nil {
		return nil
	}
	var clips []string
	if a.death != nil {
		clips = append(clips, a.death.Name)
	}
	if out := fxEventsForClips(a.doc, clips); len(out) > 0 {
		return out
	}
	return fxDeathEvents(a.doc)
}

// fxEventsForClips собирает эффекты таймлайнов, которые ведут перечисленные
// клипы. События без пути эффекта (звук, декаль, след) пропускаются: их
// играет не эта подсистема.
func fxEventsForClips(doc *whe.Document, clips []string) []FXEvent {
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
	var out []FXEvent
	for _, timeline := range doc.Timelines() {
		if !fxTimelineRunsClip(timeline, wanted) {
			continue
		}
		out = append(out, fxEventsOfTimeline(doc, timeline)...)
	}
	return out
}

// fxTimelineRunsClip — ведёт ли таймлайн один из искомых клипов: сам или
// через список Animations.
func fxTimelineRunsClip(timeline whe.FxTimeline, wanted map[string]bool) bool {
	if wanted[strings.ToLower(strings.TrimSpace(timeline.Name))] {
		return true
	}
	for _, name := range timeline.Animations {
		if wanted[strings.ToLower(strings.TrimSpace(name))] {
			return true
		}
	}
	return false
}

// fxEventsOfTimeline читает эффекты одного таймлайна вместе с их временем.
func fxEventsOfTimeline(doc *whe.Document, timeline whe.FxTimeline) []FXEvent {
	defs := doc.Events()
	var out []FXEvent
	for _, timed := range timeline.Events {
		key := strings.ToLower(strings.TrimSpace(timed.Name))
		if key == "" {
			continue
		}
		event, ok := defs[key]
		if !ok {
			continue
		}
		effect, ok := event.First("fx")
		if !ok || strings.TrimSpace(effect) == "" {
			continue
		}
		out = append(out, FXEvent{
			Name:          eventName(event, timed.Name),
			Effect:        effect,
			Marker:        fxMarkerOf(event),
			Other:         fxEntry(event, "fx_other"),
			OtherMarker:   fxEntry(event, "fx_other_marker"),
			SpansDistance: strings.EqualFold(fxEntry(event, "fx_length"), "distance"),
			AlignToOther:  strings.EqualFold(fxEntry(event, "fx_transform"), "self_to_other"),
			Clip:          timeline.Name,
			Time:          fxEventTime(timed.Time),
		})
	}
	return out
}

// fxEventTime — время события в секундах от начала клипа. Файлы пишут -1
// для «на входе в клип» и наибольшее float для «на выходе»: и то и другое
// для сцены значит «сразу», а не ожидание в 3e38 секунд. Верхняя граница
// отсекает такие значения и мусор из испорченного файла.
func fxEventTime(t float64) float64 {
	if math.IsNaN(t) || t <= 0 || t > 600 {
		return 0
	}
	return t
}

// fxDeathEvents собирает эффекты всех таймлайнов смерти файла, схлопывая
// повторы по эффекту и маркеру и оставляя самое раннее время.
func fxDeathEvents(doc *whe.Document) []FXEvent {
	var out []FXEvent
	seen := map[string]int{}
	for _, timeline := range doc.Timelines() {
		if !fxDeathTimeline(timeline) {
			continue
		}
		for _, event := range fxEventsOfTimeline(doc, timeline) {
			key := strings.ToLower(event.Effect) + "|" + strings.ToLower(event.Marker)
			if at, ok := seen[key]; ok {
				if event.Time < out[at].Time {
					out[at].Time = event.Time
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, event)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}

// fxDeathTimeline — таймлайн описывает гибель: в его имени или анимациях
// есть death/die/corpse.
func fxDeathTimeline(timeline whe.FxTimeline) bool {
	if fxDeathName(timeline.Name) {
		return true
	}
	for _, name := range timeline.Animations {
		if fxDeathName(name) {
			return true
		}
	}
	return false
}

// fxDeathName — имя клипа или таймлайна означает смерть. Слова режутся по
// разделителям: иначе «soldier» содержал бы «die» и любой пехотинец
// считался бы мёртвым.
func fxDeathName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "death") || strings.Contains(lower, "corpse") {
		return true
	}
	for _, part := range strings.FieldsFunc(lower, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.'
	}) {
		if part == "die" || part == "dies" || part == "dying" {
			return true
		}
	}
	return false
}

// fxMarkerOf — маркер модели, у которого играет эффект: файлы пишут его
// разными ключами.
func fxMarkerOf(event whe.FxEvent) string {
	for _, prop := range []string{"marker", "marker_0", "marker_surface_00", "marker_surface_0", "random_marker_0"} {
		if value, ok := event.First(prop); ok && value != "" && !strings.EqualFold(value, "random") {
			return value
		}
	}
	return ""
}

// fxEntry — первое значение ключа события или пустая строка.
func fxEntry(event whe.FxEvent, key string) string {
	value, _ := event.First(key)
	return value
}

// eventName — имя события: у определений оно своё, у таймлайна своё. В
// логах и данных полезнее имя определения, а без него — записанное в
// таймлайне.
func eventName(event whe.FxEvent, fallback string) string {
	if strings.TrimSpace(event.Name) != "" {
		return event.Name
	}
	return fallback
}
