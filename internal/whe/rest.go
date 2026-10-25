package whe

import (
	"math"
	"math/rand"
	"strings"
)

// RestClipNames — клипы, которые играют у нетронутой постройки: что находит
// граф действий .whe, когда не построен ни один аддон, ничего не повреждено
// и ничего не строится.
//
// Условия графа — состояния игры (addon_1, damage_50, build_progress…).
// Для постройки без аддонов они все ложны: ветки if и else if отпадают, а
// else и безусловные ссылки срабатывают. Так «default» доходит до своих
// поддействий, а каждый мотион разрешается по таймлайну: Animations — клипы,
// которые мотион ведёт, Motions — другие мотионы, к которым он цепляется.
//
// Мотион без таймлайна проходит дальше своим именем — кандидатом: подлинным
// клипом он может быть только в глазах модели, которая отфильтрует чужие
// имена. Пустой результат означает, что в файле нет действия default: тогда
// состояние нужно находить другим способом.
func (d *Document) RestClipNames() []string {
	return d.restClipNames(rand.Float64)
}

// RestClipNamesWithSeed picks random motions deterministically for previews.
func (d *Document) RestClipNamesWithSeed(seed int64) []string {
	rng := rand.New(rand.NewSource(seed))
	return d.restClipNames(rng.Float64)
}

func (d *Document) restClipNames(randomFloat func() float64) []string {
	actions := d.Actions()
	timelines := make(map[string]FxTimeline, len(actions))
	for _, tl := range d.Timelines() {
		timelines[restKey(tl.Name)] = tl
	}
	byName := make(map[string]int, len(actions))
	for i, a := range actions {
		byName[restKey(a.Name)] = i
	}

	var clips []string
	seenClips := make(map[string]bool)
	addClip := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seenClips[restKey(name)] {
			return
		}
		seenClips[restKey(name)] = true
		clips = append(clips, name)
	}

	seenMotions := make(map[string]bool)
	var walkMotion func(name string, depth int)
	walkMotion = func(name string, depth int) {
		key := restKey(name)
		if seenMotions[key] || depth > 16 {
			return
		}
		seenMotions[key] = true
		tl, ok := timelines[key]
		if !ok {
			addClip(name)
			return
		}
		for _, anim := range tl.Animations {
			addClip(anim)
		}
		motions := tl.RandomMotions
		if len(motions) == 0 && len(tl.Animations) == 0 && len(tl.Motions) > 0 {
			motions = make([]RandomMotion, len(tl.Motions))
			for i, next := range tl.Motions {
				motions[i] = RandomMotion{Name: next, Weight: 1}
			}
		}
		if len(motions) > 0 {
			pick := 0
			if len(motions) > 1 {
				pick = weightedMotionIndex(motions, randomFloat)
			}
			walkMotion(motions[pick].Name, depth+1)
		}
	}

	seenActions := make(map[string]bool)
	var walkAction func(name string, depth int)
	walkAction = func(name string, depth int) {
		key := restKey(name)
		if seenActions[key] || depth > 16 {
			return
		}
		index, ok := byName[key]
		if !ok {
			return
		}
		seenActions[key] = true
		action := actions[index]
		for _, link := range action.Subactions {
			if restRuns(link) {
				walkAction(link.Name, depth+1)
			}
		}
		for _, link := range action.Motions {
			if restRuns(link) {
				walkMotion(link.Name, 0)
			}
		}
	}
	walkAction("default", 0)
	return clips
}

// ActionAnimationNamesWithSeed resolves one named action to the clips its
// motions play, deterministically for a given seed. Every animation in a
// timeline is included (they may drive different parts of one model);
// weighted motion links are alternatives and one is picked per timeline —
// so a mesh with several idle variants gets a randomly chosen one.
func (d *Document) ActionAnimationNamesWithSeed(actionName string, seed int64) []string {
	rng := rand.New(rand.NewSource(seed))
	return d.actionAnimationNames(actionName, rng.Float64)
}

func (d *Document) actionAnimationNames(actionName string, randomFloat func() float64) []string {
	if d == nil || strings.TrimSpace(actionName) == "" {
		return nil
	}
	if randomFloat == nil {
		randomFloat = rand.Float64
	}
	actions := d.Actions()
	timelines := make(map[string]FxTimeline, len(d.Timelines()))
	for _, timeline := range d.Timelines() {
		timelines[restKey(timeline.Name)] = timeline
	}
	actionMap := make(map[string]FxAction, len(actions))
	for _, action := range actions {
		actionMap[restKey(action.Name)] = action
	}
	var clips []string
	seenClips := make(map[string]bool)
	addClip := func(name string) {
		name = strings.TrimSpace(name)
		key := restKey(name)
		if key == "" || seenClips[key] {
			return
		}
		seenClips[key] = true
		clips = append(clips, name)
	}
	seenActions := make(map[string]bool)
	var walkAction func(string, int)
	var walkMotion func(string, map[string]bool, int)
	walkMotion = func(name string, stack map[string]bool, depth int) {
		key := restKey(name)
		if key == "" || depth > 16 || stack[key] {
			return
		}
		timeline, ok := timelines[key]
		if !ok {
			addClip(name)
			return
		}
		stack[key] = true
		for _, animation := range timeline.Animations {
			addClip(animation)
		}
		motions := timeline.RandomMotions
		if len(motions) == 0 && len(timeline.Animations) == 0 && len(timeline.Motions) > 0 {
			for _, next := range timeline.Motions {
				motions = append(motions, RandomMotion{Name: next, Weight: 1})
			}
		}
		if len(motions) > 0 {
			walkMotion(motions[weightedMotionIndex(motions, randomFloat)].Name, stack, depth+1)
		}
		delete(stack, key)
	}
	walkAction = func(name string, depth int) {
		key := restKey(name)
		if key == "" || depth > 16 || seenActions[key] {
			return
		}
		action, ok := actionMap[key]
		if !ok {
			return
		}
		seenActions[key] = true
		for _, child := range action.Subactions {
			if restRuns(child) {
				walkAction(child.Name, depth+1)
			}
		}
		for _, motion := range action.Motions {
			if restRuns(motion) {
				walkMotion(motion.Name, make(map[string]bool), 0)
			}
		}
	}
	walkAction(actionName, 0)
	return clips
}

func weightedMotionIndex(motions []RandomMotion, randomFloat func() float64) int {
	if len(motions) < 2 {
		return 0
	}
	var total float64
	for _, motion := range motions {
		weight := float64(motion.Weight)
		if weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0) {
			total += weight
		}
	}
	if total <= 0 {
		return int(randomFloat()*float64(len(motions))) % len(motions)
	}
	pick := randomFloat()
	if pick < 0 {
		pick = 0
	} else if pick >= 1 || math.IsNaN(pick) {
		pick = math.Nextafter(1, 0)
	}
	needle := pick * total
	for i, motion := range motions {
		weight := float64(motion.Weight)
		if weight <= 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
			continue
		}
		if needle < weight {
			return i
		}
		needle -= weight
	}
	return len(motions) - 1
}

// restRuns — идёт ли ссылка графа в нетронутом состоянии. Условия все ложны,
// поэтому ветки if и else if пропускаются, а else (он срабатывает, когда ни
// одна ветка цепочки не истинна) и безусловные ссылки — срабатывают.
func restRuns(link FxActionLink) bool {
	return link.Compare != FxCompareIf && link.Compare != FxCompareElseIf
}

// restKey — имя узла графа для поиска: регистр в данных встречается разный.
func restKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
