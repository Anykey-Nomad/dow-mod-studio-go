package whm

import "strings"

// Базовое состояние постройки: видно только то, что постройка показывает
// до постройки аддонов.
//
// Как это устроено в игре. Модель постройки хранит все её части сразу:
// и корпус, и надстройки аддонов, и леса строительства. Какая часть видна —
// решают клипы видимости (vis_*): в каждом таком клипе для КАЖДОГО меша
// модели записано, держится он скрытым или показан. Игра при появлении
// постройки проигрывает клип базового состояния, а когда аддон достроен —
// клип этого аддона. Поэтому постройка без аддонов — это в точности то, что
// показывает базовый клип.
//
// Имена базовых клипов у рас разные: у гвардии и сестёр — vis_base, у хаоса
// и космодесантников — vis_default, у тау — vis_hq. У эльдаров, орков и
// тёмных эльдаров отдельного базового клипа нет: там состояние готовой
// постройки задаёт клип покоя (idle_*).

// baseClipNames — клипы базового состояния, от более точных к более
// общим.
var baseClipNames = []string{
	"vis_base",
	"vis_default",
	"vis_hq",
}

// idleClipPrefixes — префиксы клипов покоя. Ими задаётся состояние готовой
// постройки, когда отдельного базового клипа нет.
var idleClipPrefixes = []string{"idle"}

// MeshNames — имена мешей модели. Часть мешей приходит несколькими
// кусками (по одному на материал), и в карте видимости имя повторяется:
// для состояния важно имя, а не кусок.
func (m *Model) MeshNames() map[string]bool {
	if m == nil {
		return nil
	}
	names := make(map[string]bool, len(m.Meshes))
	for _, mesh := range m.Meshes {
		names[mesh.Name] = true
	}
	return names
}

// BaseState — состояние модели без аддонов: имя клипа, который его задаёт,
// и множество имён видимых мешей.
//
// Второе значение равно nil, когда у модели нет клипа, описывающего
// состояние целиком: у такого клипа нет треков для части мешей, и по нему
// нельзя понять, что должно быть видно. Тогда видно всё, как и в модели,
// из которой ничего не убрано.
func (m *Model) BaseState() (string, map[string]bool) {
	if m == nil {
		return "", nil
	}
	names := m.MeshNames()
	if len(names) == 0 {
		return "", nil
	}
	for _, wanted := range baseClipNames {
		anim := m.Animation(wanted)
		if anim == nil {
			continue
		}
		if visible, ok := anim.VisibleMeshes(names); ok {
			return anim.Name, visible
		}
	}
	for _, anim := range m.Animations {
		lower := strings.ToLower(anim.Name)
		matched := false
		for _, prefix := range idleClipPrefixes {
			if strings.HasPrefix(lower, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if visible, ok := anim.VisibleMeshes(names); ok {
			return anim.Name, visible
		}
	}
	return "", nil
}

// VisibleMeshes — меши, которые клип оставляет показанными.
//
// Имена в возвращённой карте записаны строчными буквами: регистр имён в
// данных встречается разный, а искать по ним придётся из разных мест.
//
// Второе значение — true, когда клип описывает состояние целиком, то есть
// имеет трек для каждого из переданных мешей. Только такой клип годится
// базовым: по неполному нельзя отличить «скрыт» от «не упомянут».
func (a *Animation) VisibleMeshes(names map[string]bool) (map[string]bool, bool) {
	if a == nil || len(names) == 0 {
		return nil, false
	}
	// Имена мешей в модели и в треках пишутся одинаково, но регистр
	// в данных встречается разный.
	lower := make(map[string]bool, len(names))
	for name := range names {
		lower[strings.ToLower(name)] = true
	}
	visible := make(map[string]bool, len(names))
	seen := make(map[string]bool, len(names))
	for _, tr := range a.Meshes {
		key := strings.ToLower(tr.Name)
		if !lower[key] {
			continue
		}
		seen[key] = true
		if meshVisible(tr) {
			visible[key] = true
		}
	}
	if len(seen) < len(lower) {
		return nil, false
	}
	return visible, true
}

// meshVisible — виден ли меш по своей кривой видимости.
//
// Меш держится на значении force, пока кривая не сказала иное. Ключи кривой
// приводят меш к новому значению, и состояние покоя — это последнее
// значение кривой.
func meshVisible(tr MeshTrack) bool {
	value := tr.Force
	if len(tr.Keys) > 0 {
		value = tr.Keys[len(tr.Keys)-1].V[0]
	}
	return value >= 0.5
}

// VisibleIn — меши, которые показывают вместе перечисленные клипы.
//
// Клип добавляет к общему множеству свои видимые меши, а те, которых нет в
// модели, пропускаются: так набирается состояние постройки, когда граф
// действий складывает его из нескольких клипов — база, флаги, покой.
//
// Второе значение — false, когда ни один из клипов в модели не нашёлся:
// по такому списку видимости не сказать ничего.
func (m *Model) VisibleIn(clipNames []string) (map[string]bool, bool) {
	if m == nil || len(clipNames) == 0 {
		return nil, false
	}
	names := m.MeshNames()
	if len(names) == 0 {
		return nil, false
	}
	lower := make(map[string]bool, len(names))
	for name := range names {
		lower[strings.ToLower(name)] = true
	}
	visible := make(map[string]bool, len(names))
	found := false
	for _, want := range clipNames {
		anim := m.Animation(want)
		if anim == nil {
			continue
		}
		found = true
		for _, tr := range anim.Meshes {
			key := strings.ToLower(tr.Name)
			if lower[key] && meshVisible(tr) {
				visible[key] = true
			}
		}
	}
	if !found {
		return nil, false
	}
	return visible, true
}
