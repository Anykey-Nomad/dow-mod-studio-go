package sgbview

import (
	"strings"

	"dow-mod-studio/internal/whe"
)

// resolveShotFX собирает эффекты выстрела из таймлайнов, которые ведут
// клипы стрельбы. Событие таймлайна — это имя, а свойства (fx, marker,
// fx_other, fx_length, fx_transform) лежат в определениях .whe, поэтому
// событие без файла эффекта (звук, вспышка без картинки) отбрасывается.
func resolveShotFX(doc *whe.Document, clips []string) []ShotFX {
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
	defs := doc.Events()
	var out []ShotFX
	seen := map[string]bool{}
	for _, timeline := range doc.Timelines() {
		// Таймлайн может назвать клип сам или вести к нему через
		// Animations — срабатывает любой из вариантов.
		matched := false
		for _, name := range append([]string{timeline.Name}, timeline.Animations...) {
			if wanted[strings.ToLower(strings.TrimSpace(name))] {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, timed := range timeline.Events {
			key := strings.ToLower(strings.TrimSpace(timed.Name))
			if key == "" || seen[key] {
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
			seen[key] = true
			shot := ShotFX{Name: event.Name, Effect: effect}
			for _, prop := range []string{"marker", "marker_0", "marker_surface_00", "marker_surface_0", "random_marker_0"} {
				if value, ok := event.First(prop); ok && value != "" && !strings.EqualFold(value, "random") {
					shot.Marker = value
					break
				}
			}
			shot.Other, _ = event.First("fx_other")
			shot.OtherMarker, _ = event.First("fx_other_marker")
			if value, ok := event.First("fx_length"); ok && strings.EqualFold(strings.TrimSpace(value), "distance") {
				shot.SpansDistance = true
			}
			if value, ok := event.First("fx_transform"); ok && strings.EqualFold(strings.TrimSpace(value), "self_to_other") {
				shot.AlignToOther = true
			}
			out = append(out, shot)
		}
	}
	return out
}
