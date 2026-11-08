package sgbview

import (
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"

	"github.com/fogleman/fauxgl"
)

// AnimState — состояние анимации объекта: какой клип сейчас играет.
type AnimState int

const (
	// AnimIdle — без приказов: юнит, постройка и окружение стоят на idle.
	AnimIdle AnimState = iota
	// AnimMove — выполняется приказ движения: боец идёт (run/walk).
	AnimMove
	// AnimCapture — боец стоит на стратегической точке и захватывает её.
	AnimCapture
	// AnimConstruct — постройка возводится: клип ведёт прогресс работы
	// (SetProgress), а не время, поэтому остановка стройки держит кадр.
	AnimConstruct
	// AnimDeath — юнит уничтожен: тело лежит в позе смерти, последний
	// кадр клипа держится, пока труп не уберут или не поднимут.
	AnimDeath
	// AnimAttack — боец в дистанции и ведёт огонь (или бьёт в ближнем бою):
	// клипы действия атаки играют вместе, а клипы прицела стоят на кадре,
	// который смотрит на цель (SetAim).
	AnimAttack
)

// ModelFacingOffset — поправка между осью сцены и «лицом» модели: у бойцов
// DoW передняя сторона смотрит вдоль −Y модели, а heading отсчитывается от
// +X, поэтому к направлению движения добавляется четверть оборота.
const ModelFacingOffset = math.Pi / 2

// Anim — анимация одного объекта сцены: клипы состояний и время их игры.
//
// Объект попадает сюда, только если у модели есть скелет и есть костяной
// idle-клип: для юнитов он берётся из .whe по цепочке действия default —
// так же, как в просмотрщике sgb. Состояние (Idle/Move/Capture) переключается
// снаружи — по приказу сущности, — а поза каждый кадр превращается в палитру
// костей для GPU-скиннинга: P × SkinMatrix × P⁻¹, где P — размещение
// объекта в мире (вершины лежат в мировых координатах, поза считается в
// координатах модели).
type Anim struct {
	model *whm.Model
	idle  *whm.Animation
	// move и capture — клипы приказов; nil, если у модели таких состояний
	// нет (окружение), тогда остаётся idle.
	move    *whm.Animation
	capture *whm.Animation
	// attackRanged и attackMelee — клипы боя: то, что .whe ведёт из действий
	// idle_firing/move_firing (стрельба) и melee_attack (ближний бой).
	// Клипы прицела в них не входят: их время задаёт угол на цель, а не
	// часы анимации, поэтому они лежат отдельно.
	attackRanged []*whm.Animation
	attackMelee  []*whm.Animation
	attackTimes  []float32
	melee        bool
	// aimH, aimV — клипы прицела по горизонтали и вертикали, aimSpanH и
	// aimSpanV — их размах в радианах, измеренный по позе (у бойцов игры
	// горизонталь делает полный оборот, вертикаль — половину). Время этих
	// клипов считается из направления на цель: atan2 к кости прицела даёт
	// тот кадр, где ствол смотрит на противника.	// aimOffsetH и aimOffsetV — где на этом размахе лежит «прямо вперёд»
	// и «ровно»: клип начинается не с прицела вперёд, и без поправки
	// боец смотрел бы мимо цели на постоянный угол.
	aimH, aimV *whm.Animation
	aimSpanH   float64
	aimOffsetH float64
	// aimVTable — угол наклона кости по кадрам клипа (33 отсчёта), а
	// aimVLevel — угол на кадре «ровно». Зависимость нелинейная: у
	// некронов на середине клипа кость уже задрана на 66°, поэтому кадр
	// подъёма ищется по таблице, а не по размаху.
	aimVTable  []float64
	aimVLevel  float64
	aimBearing float64
	aimPitch   float64
	// death — клип позы смерти для AnimDeath; nil — у модели нет смерти
	// (тогда уничтоженный остаётся на idle).
	death *whm.Animation
	// shotFX — эффекты выстрела из таймлайнов стрельбы: игра запускает их
	// на каждом выстреле. Пусто — у модели стрельба без эффектов.
	shotFX []ShotFX
	// shotClips — клипы стрельбы: по ним сцена берёт и эффекты, и звуки
	// выстрела из таймлайнов .whe.
	shotClips []string
	// doc — разобранный .whe модели: по нему сцена берёт эффекты её
	// событий (смерть, удары) вместе со временем срабатывания.
	doc   *whe.Document
	state AnimState
	time  float32
	// frames — клипы, кадр которых задан извне (точка захвата): их время
	// не идёт по часам, а стоит на доле прогресса.
	frames  []clipFrame
	P, Pinv fauxgl.Matrix
	// boneBase — база костей объекта в общей палитре кадра. Геометрия
	// объекта лежит в своём мире, поэтому индексы костей в вершинах
	// локальны (с нуля); базу назначает рендерер при загрузке сцены —
	// по ней палитра пишется в общий буфер и читается в шейдере.
	boneBase int
	// build — этапы стройки из действия build .whe: клип, диапазон прогресса в
	// процентах и как клип играет. Этап без диапазона — появление постройки
	// (phase_in), он звучит и играет сразу после установки шаблона.
	build []buildClip
	// stage — какой этап стройки сейчас играет.
	stage int
	// pos — точка постановки объекта в мире: вокруг её вертикальной оси
	// модель поворачивается, когда боец меняет направление.
	pos [3]float64
	// heading — поворот, запечённый в геометрию при загрузке; yaw — куда
	// объект смотрит сейчас. Оба — в радианах, как и sgb.Placement.Heading.
	heading float64
	yaw     float64
}

// clipFrame — клип с заданным извне кадром.
type clipFrame struct {
	clip *whm.Animation
	at   float32
}

// buildClip — этап стройки: клип, диапазон прогресса в процентах (ranged) и
// зацикленность таймлайна. Этап без диапазона — появление постройки.
type buildClip struct {
	clip   *whm.Animation
	min    float64
	max    float64
	ranged bool
	loop   bool
}

// SetFacing задаёт направление взгляда в мировых радианах (0 — вдоль +X).
// Модель поворачивается матрицей палитры костей, без перезапекания вершин:
// маршрут может петлять, и повороты идут по нему, а не по конечной цели.
func (a *Anim) SetFacing(yaw float64) {
	if a == nil {
		return
	}
	a.yaw = yaw
}

// rotateAboutVertical — поворот вокруг вертикальной оси через точку (px, py):
// боец крутится вокруг своих ступней, а не вокруг центра карты.
func rotateAboutVertical(px, py, angle float64) fauxgl.Matrix {
	ch, sh := math.Cos(angle), math.Sin(angle)
	return fauxgl.Matrix{
		X00: ch, X01: -sh, X03: px - ch*px + sh*py,
		X10: sh, X11: ch, X13: py - sh*px - ch*py,
		X22: 1, X23: 0,
		X33: 1,
	}
}

// SetBase задаёт базу костей объекта в палитре кадра (вызывается
// рендерером один раз при загрузке сцены).
func (a *Anim) SetBase(base int) {
	if a == nil {
		return
	}
	a.boneBase = base
}

// Base — база костей объекта в палитре кадра.
func (a *Anim) Base() int {
	if a == nil {
		return 0
	}
	return a.boneBase
}

// newAnim собирает анимацию объекта по модели и её .whe. nil, если анимировать
// нечего: нет костей, нет idle-клипа, а модель с запечённой позой (монолит
// показывается до достройки аддонов) остаётся статичной — её покой скелета
// уже не исходный.
func newAnim(tree *modtree.ModTree, whmPath string, model *whm.Model, placement sgb.Placement) *Anim {
	if model == nil || len(model.Bones) == 0 || poseBakedModel(whmPath) {
		return nil
	}
	// Idle — цепочка действия default в .whe, затем типовые имена; клип
	// привязывается к скелету этой модели по именам костей, даже если он
	// пришёл из модели-ссылки (XREF).
	idle, _ := sgb.ResolveLinkedIdleAnimation(tree, whmPath, model)
	idle = sgb.BindAnimationToModel(model, idle)
	if idle == nil || len(idle.Bones) == 0 {
		return nil
	}
	a := &Anim{model: model, idle: idle, P: sgb.EntityModelMatrix(placement)}
	a.Pinv = a.P.Inverse()
	a.pos = [3]float64{placement.X, placement.Y, placement.Z}
	a.heading, a.yaw = placement.Heading, placement.Heading
	if doc, err := modelWHE(tree, whmPath); err == nil && doc != nil {
		a.doc = doc
		a.move = resolveActionClip(tree, whmPath, model, doc, "move")
		a.capture = resolveActionClip(tree, whmPath, model, doc, "capture_strategic_point")
		a.build = buildStageClips(tree, whmPath, model, doc)
		a.resolveAttackClips(tree, whmPath, model, doc)
	}
	return a
}

// buildStageClips — клипы этапов стройки: у каждого свой диапазон прогресса.
// Этап выбирает прогресс (SetProgress), а клип идёт по своим часам (Step):
// события таймлайна размечены в секундах от начала клипа, поэтому скрабить
// его прогрессом нельзя — звук на тридцатой секунде разошёлся бы с кадром.
// Пустой список означает, что стройку анимировать нечем.
func buildStageClips(tree *modtree.ModTree, whmPath string, model *whm.Model, doc *whe.Document) []buildClip {
	stages := (&Anim{doc: doc}).BuildStages()
	clips := make([]buildClip, 0, len(stages))
	for _, stage := range stages {
		// audio_build — слой звука, а не поза: его «анимация» vis_empty
		// только прячет меши.
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(stage.Motion)), "audio") {
			continue
		}
		var clip *whm.Animation
		for _, name := range append(append([]string{}, stage.Anims...), stage.Motion) {
			resolved, _ := sgb.ResolveLinkedAnimation(tree, whmPath, model, name)
			if resolved == nil {
				continue
			}
			if bound := sgb.BindAnimationToModel(model, resolved); bound != nil && len(bound.Bones) > 0 {
				clip = bound
				break
			}
		}
		if clip == nil || strings.HasPrefix(strings.ToLower(clip.Name), "vis_") {
			continue
		}
		clips = append(clips, buildClip{
			clip:   clip,
			min:    stage.Min,
			max:    stage.Max,
			ranged: stage.HasRange,
			loop:   stage.Loop,
		})
	}
	if len(clips) > 0 {
		return clips
	}
	// Действие build не разобралось: остаются стадии build_stage_* модели,
	// которые делят прогресс на равные отрезки.
	numbered := buildStageAnimations(model)
	for i, clip := range numbered {
		n := float64(len(numbered))
		clips = append(clips, buildClip{
			clip:   clip,
			min:    100 * float64(i) / n,
			max:    100 * float64(i+1) / n,
			ranged: true,
		})
	}
	return clips
}

// buildStageAnimations — клипы build_stage_* модели по номеру стадии.
func buildStageAnimations(model *whm.Model) []*whm.Animation {
	type stage struct {
		number int
		clip   *whm.Animation
	}
	var stages []stage
	for i := range model.Animations {
		animation := &model.Animations[i]
		name := strings.ToLower(strings.TrimSpace(animation.Name))
		if !strings.HasPrefix(name, "build_stage") {
			continue
		}
		number, err := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(name, "build_stage"), "_"))
		if err != nil {
			number = len(stages) + 1
		}
		stages = append(stages, stage{number: number, clip: animation})
	}
	sort.SliceStable(stages, func(i, j int) bool { return stages[i].number < stages[j].number })
	clips := make([]*whm.Animation, 0, len(stages))
	for _, entry := range stages {
		clips = append(clips, entry.clip)
	}
	return clips
}

// ShotFX — эффект, который .whe запускает при выстреле. Effect — путь к
// файлу эффекта (DATA:ART/FX/...); Marker — маркер ствола на этой модели;
// Other — второй конец эффекта (у стрельбы обычно target — тот, в кого
// стреляли): по нему ленты и снаряды тянутся до цели.
// SpansDistance — fx_length=distance: Length ленты — доля зазора между
// концами, а не длина в единицах модели. AlignToOther — fx_transform=
// self_to_other: эффект разворачивается от ствола к цели.
type ShotFX struct {
	Name          string
	Effect        string
	Marker        string
	Other         string
	OtherMarker   string
	SpansDistance bool
	AlignToOther  bool
}

// ShotFX отдаёт эффекты выстрела этой модели. Срез общий: менять его нельзя.
func (a *Anim) ShotFX() []ShotFX {
	if a == nil {
		return nil
	}
	return a.shotFX
}

// MarkerPoint — точка маркера в мире для текущей позы и поворота модели:
// к ней сцена добавляет сдвиг объекта по карте. ok=false — такого маркера у
// модели нет (или у неё нет скелета).
func (a *Anim) MarkerPoint(name string) ([3]float64, bool) {
	if a == nil || a.model == nil || strings.TrimSpace(name) == "" {
		return [3]float64{}, false
	}
	index := a.model.MarkerIndex(name)
	if index < 0 {
		return [3]float64{}, false
	}
	anims, times := a.pose()
	pose := a.model.PoseAtAnimationTimes(anims, times)
	local, ok := a.model.MarkerWorld(index, pose, true)
	if !ok {
		return [3]float64{}, false
	}
	point := a.P.MulPosition(local)
	if delta := a.yaw - a.heading; math.Abs(delta) > 1e-4 {
		point = rotateAboutVertical(a.pos[0], a.pos[1], delta).MulPosition(point)
	}
	return [3]float64{point.X, point.Y, point.Z}, true
}

// SetAttack включает боевую стойку: melee — ближний бой, иначе стрельба.
// Повторный вызов с тем же боем клип не сбрасывает, поэтому вызывать его
// можно каждый кадр; смена боя начинает действие с начала.
func (a *Anim) SetAttack(melee bool) {
	if a == nil {
		return
	}
	if a.state == AnimAttack && a.melee == melee {
		return
	}
	a.state = AnimAttack
	a.melee = melee
	a.time = 0
	for i := range a.attackTimes {
		a.attackTimes[i] = 0
	}
}

// HasAttack сообщает, есть ли у модели чем сыграть атаку — нужно игре,
// чтобы не включать боевую стойку бойцу без клипов боя.
func (a *Anim) HasAttack() bool {
	return a != nil && (len(a.attackRanged) > 0 || len(a.attackMelee) > 0)
}

// HasDeath сообщает, есть ли у модели клип смерти: по нему игра решает,
// играть ли die перед исчезновением обломков.
func (a *Anim) HasDeath() bool {
	return a != nil && a.death != nil
}

// DeathDuration — длина клипа смерти в секундах (0 — клипа нет).
func (a *Anim) DeathDuration() float64 {
	if a == nil || a.death == nil {
		return 0
	}
	return float64(a.death.Duration)
}

// SetAim задаёт направление на цель в радианах относительно «вперёд» бойца
// (0 — прямо перед ним, плюс — в сторону обхода клипа) и угол подъёма
// (плюс — вверх).
//
// Клипы aim_horiz и aim_vert — не анимация выстрела, а развёрнутая поза:
// у бойцов игры горизонтальный клип поворачивает прицел на полный оборот,
// вертикальный — на половину. Поэтому нужный кадр — это доля угла от
// измеренного размаха клипа: горизонталь идёт по кругу, вертикаль — от
// середины к краям. Кадр выбирается заново каждый кадр, так что идущая цель
// не уходит из-под прицела.
func (a *Anim) SetAim(bearing, pitch float64) {
	if a == nil {
		return
	}
	a.aimBearing = bearing
	a.aimPitch = pitch
}

// attackClips — клипы текущего вида боя.
func (a *Anim) attackClips() []*whm.Animation {
	if a == nil {
		return nil
	}
	if a.melee {
		return a.attackMelee
	}
	return a.attackRanged
}

// pose — клипы и их времена для текущего состояния. У атаки клипов
// несколько, и поза собирается по костям (PoseAtAnimationTimes): клип
// прицела ведёт только свои кости, клипы выстрела — свои, а тело, которого
// они не касаются, держит idle.
// aimTimeV — кадр клипа, на котором ствол смотрит на нужный подъём. Кадр
// ищется по измеренной таблице углов: зависимость нелинейная, и линейная
// доля размаха уводила бы ствол выше цели.
func (a *Anim) aimTimeV() float32 {
	if len(a.aimVTable) < 2 {
		return 0
	}
	target := a.aimVLevel + a.aimPitch
	best, bestT := math.Inf(1), 0.0
	for i, elev := range a.aimVTable {
		if delta := math.Abs(elev - target); delta < best {
			best, bestT = delta, float64(i)/float64(len(a.aimVTable)-1)
		}
	}
	return float32(clamp01(bestT))
}

func (a *Anim) pose() ([]*whm.Animation, []float32) {
	if a == nil {
		return nil, nil
	}
	if len(a.frames) > 0 {
		anims := make([]*whm.Animation, 0, len(a.frames))
		times := make([]float32, 0, len(a.frames))
		for _, frame := range a.frames {
			anims = append(anims, frame.clip)
			times = append(times, frame.at)
		}
		return anims, times
	}
	if a.state == AnimDeath {
		if a.death == nil {
			return nil, nil
		}
		// Последний кадр клипа смерти: тело лежит, а не крутит клип
		// заново.
		return []*whm.Animation{a.death}, []float32{float32(a.death.Duration)}
	}
	if a.state == AnimAttack {
		clips := a.attackClips()
		anims := make([]*whm.Animation, 0, len(clips)+2)
		times := make([]float32, 0, len(clips)+2)
		for i, clip := range clips {
			if clip == nil {
				continue
			}
			t := float32(0)
			if i < len(a.attackTimes) {
				t = a.attackTimes[i]
			}
			anims = append(anims, clip)
			times = append(times, t)
		}
		if a.aimH != nil && math.Abs(a.aimSpanH) > 1e-3 {
			anims = append(anims, a.aimH)
			times = append(times, float32(wrap01(a.aimOffsetH+a.aimBearing/a.aimSpanH)))
		}
		if a.aimV != nil && len(a.aimVTable) > 1 {
			anims = append(anims, a.aimV)
			times = append(times, a.aimTimeV())
		}
		return anims, times
	}
	clip := a.clip()
	if clip == nil {
		return nil, nil
	}
	return []*whm.Animation{clip}, []float32{a.time}
}

// resolveAttackClips собирает наборы клипов боя из .whe и находит клипы
// прицела: они не играют по часам, а выбираются по углу на цель.
func (a *Anim) resolveAttackClips(tree *modtree.ModTree, whmPath string, model *whm.Model, doc *whe.Document) {
	// Стрельба стоит: игра ведёт огонь с места. Поэтому берётся набор
	// idle_firing/idle_tracking, а не move_firing: последний несёт цикл
	// шага, и в бою боец «шёл» бы на месте. Набор с движением остаётся
	// запасным — у модели может не быть стоячей стрельбы вовсе.
	standing := wheActionClips(doc, nil, "idle_firing", "idle_tracking", "attack", "fire")
	moving := wheActionClips(doc, nil, "move_firing", "move_tracking", "attack", "fire")
	// Эффекты выстрела — из тех же таймлайнов, что ведут клипы стрельбы.
	a.shotClips = standing
	a.shotFX = resolveShotFX(doc, standing)
	if len(a.shotFX) == 0 {
		a.shotClips = moving
		a.shotFX = resolveShotFX(doc, moving)
	}
	melee := wheActionClips(doc, nil, "melee_attack", "melee_move", "attack")
	a.aimH = a.resolveAimClip(tree, whmPath, model, standing, "aim_horiz")
	a.aimV = a.resolveAimClip(tree, whmPath, model, standing, "aim_vert")
	if a.aimH == nil && a.aimV == nil {
		a.aimH = a.resolveAimClip(tree, whmPath, model, moving, "aim_horiz")
		a.aimV = a.resolveAimClip(tree, whmPath, model, moving, "aim_vert")
	}
	a.aimSpanH, a.aimOffsetH = measureAim(model, a.aimH)
	a.aimVLevel, a.aimVTable = measureAimVertical(model, a.aimV)
	a.attackRanged = a.resolveNamedClips(tree, whmPath, model, standing, true)
	if len(a.attackRanged) == 0 {
		a.attackRanged = a.resolveNamedClips(tree, whmPath, model, moving, true)
	}
	a.attackMelee = a.resolveNamedClips(tree, whmPath, model, melee, false)
	// Кости, которых бой не двигает, держит idle: иначе тело застыло бы
	// в позе прицела.
	if a.idle != nil {
		if len(a.attackRanged) > 0 {
			a.attackRanged = append(a.attackRanged, a.idle)
		}
		if len(a.attackMelee) > 0 {
			a.attackMelee = append(a.attackMelee, a.idle)
		}
	}
	a.attackTimes = make([]float32, len(a.attackRanged))
	// Поза трупа — клип смерти (die/straight_to_death_pose): он же
	// держится последним кадром, когда юнит уничтожен.
	a.death = corpseClip(tree, whmPath, model)
}

// resolveNamedClips превращает имена клипов действия в клипы модели,
// привязанные к её скелету. Клипы прицела пропускаются, если skipAim: они
// не играют по времени, их ставит SetAim. В конец набора идёт idle: кости,
// которые бой не двигает, должны жить, а не застывать.
func (a *Anim) resolveNamedClips(tree *modtree.ModTree, whmPath string, model *whm.Model, names []string, skipAim bool) []*whm.Animation {
	var clips []*whm.Animation
	seen := map[string]bool{}
	for _, name := range names {
		lower := strings.ToLower(strings.TrimSpace(name))
		if skipAim && strings.Contains(lower, "aim_") {
			continue
		}
		clip, _ := sgb.ResolveLinkedAnimation(tree, whmPath, model, name)
		if clip == nil {
			continue
		}
		bound := sgb.BindAnimationToModel(model, clip)
		if bound == nil || len(bound.Bones) == 0 {
			continue
		}
		key := strings.ToLower(bound.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		clips = append(clips, bound)
	}
	return clips
}

// resolveAimClip находит клип прицела по части имени: сначала среди клипов
// действия, потом среди анимаций самой модели.
func (a *Anim) resolveAimClip(tree *modtree.ModTree, whmPath string, model *whm.Model, names []string, want string) *whm.Animation {
	for _, name := range names {
		if !strings.Contains(strings.ToLower(name), want) {
			continue
		}
		clip, _ := sgb.ResolveLinkedAnimation(tree, whmPath, model, name)
		if clip == nil {
			continue
		}
		if bound := sgb.BindAnimationToModel(model, clip); bound != nil && len(bound.Bones) > 0 {
			return bound
		}
	}
	for i := range model.Animations {
		if strings.Contains(strings.ToLower(model.Animations[i].Name), want) {
			return &model.Animations[i]
		}
	}
	return nil
}

// modelForwardYaw — куда смотрит «лицо» модели в её собственной системе:
// передняя сторона бойцов DoW направлена вдоль −Y, а углы считаются от +X.
const modelForwardYaw = -math.Pi / 2

// measureAim измеряет клип прицела: его размах в радианах и где на этом
// размахе лежит нейтральное положение.
//
// Кость, которую ведёт клип, поворачивается на весь размах, а вот начинает
// клип не с прицела вперёд: у некрона горизонталь стартует на 90° в сторону.
// Поэтому для горизонтали смещение — это доля, на которой кость смотрит в
// ту же сторону, что и лицо модели; для вертикали — доля, на которой ось
// кости лежит горизонтально. Ноль размаха — клип ничего не крутит.
func measureAim(model *whm.Model, clip *whm.Animation) (span, offset float64) {
	if model == nil || clip == nil || clip.Duration <= 0 {
		return 0, 0
	}
	moved := model.MovedBones(clip)
	bone := -1
	for i, moves := range moved {
		if moves {
			bone = i
			break
		}
	}
	if bone < 0 {
		return 0, 0
	}
	// Горизонталь — поворот вокруг вертикали. Суммируются шаги со знаком,
	// поэтому у модели, которая крутит в другую сторону, пеленг сам ляжет
	// в обратном направлении.
	prev := aimYaw(aimSkin(model, clip, bone, 0))
	for step := 1; step <= 32; step++ {
		yaw := aimYaw(aimSkin(model, clip, bone, float32(step)/32))
		delta := yaw - prev
		for delta > math.Pi {
			delta -= 2 * math.Pi
		}
		for delta < -math.Pi {
			delta += 2 * math.Pi
		}
		span += delta
		prev = yaw
	}
	if math.Abs(span) <= 1e-3 {
		return 0, 0
	}
	start := aimYaw(aimSkin(model, clip, bone, 0))
	offset = wrap01((modelForwardYaw - start) / span)
	return span, offset
}

// measureAimVertical снимает таблицу «кадр клипа → угол наклона кости» и
// угол на кадре «ровно». Клип наклоняет кость неравномерно (у некронов на
// середине уже 66°), поэтому подъём ищется по таблице, а не по линейному
// размаху: иначе прицел «+30°» уводил бы ствол вдвое выше.
func measureAimVertical(model *whm.Model, clip *whm.Animation) (level float64, table []float64) {
	if model == nil || clip == nil || clip.Duration <= 0 {
		return 0, nil
	}
	moved := model.MovedBones(clip)
	bone := -1
	for i, moves := range moved {
		if moves {
			bone = i
			break
		}
	}
	if bone < 0 {
		return 0, nil
	}
	const steps = 32
	// Подъём меряется по той оси кости, которая от наклона ходит сильнее
	// всего — у одних моделей это X, у других Y или Z.
	axis, bestRange := 0, -1.0
	for candidate := 0; candidate < 3; candidate++ {
		low, high := math.Inf(1), math.Inf(-1)
		for step := 0; step <= steps; step++ {
			elev := axisElevation(aimSkin(model, clip, bone, float32(step)/steps), candidate)
			if elev < low {
				low = elev
			}
			if elev > high {
				high = elev
			}
		}
		if high-low > bestRange {
			axis, bestRange = candidate, high-low
		}
	}
	table = make([]float64, steps+1)
	best := math.Inf(1)
	for step := 0; step <= steps; step++ {
		elev := axisElevation(aimSkin(model, clip, bone, float32(step)/steps), axis)
		table[step] = elev
		if math.Abs(elev) < best {
			best, level = math.Abs(elev), elev
		}
	}
	return level, table
}

// aimYaw — поворот оси кости вокруг вертикали в системе модели.
func aimYaw(m fauxgl.Matrix) float64 {
	return math.Atan2(m.X10, m.X00)
}

// axisElevation — насколько высоко над горизонтом модели поднята ось кости:
// 0 — X, 1 — Y, 2 — Z.
func axisElevation(m fauxgl.Matrix, axis int) float64 {
	z := m.X20
	switch axis {
	case 1:
		z = m.X21
	case 2:
		z = m.X22
	}
	if z > 1 {
		z = 1
	}
	if z < -1 {
		z = -1
	}
	return math.Asin(z)
}

// aimSkin — матрица кожи одной кости в позе клипа на доле времени t.
func aimSkin(model *whm.Model, clip *whm.Animation, bone int, t float32) fauxgl.Matrix {
	return model.PoseAt(clip, t).SkinMatrix(int32(bone))
}

// rotationAngle — угол поворота матрицы без переноса, в радианах.
func rotationAngle(m fauxgl.Matrix) float64 {
	trace := m.X00 + m.X11 + m.X22
	if trace > 3 {
		trace = 3
	}
	if trace < -1 {
		trace = -1
	}
	return math.Acos((trace - 1) / 2)
}

// wrap01 приводит долю к 0..1 по кругу: полный оборот прицела замкнут.
func wrap01(v float64) float64 {
	v = math.Mod(v, 1)
	if v < 0 {
		v++
	}
	return v
}

// clamp01 держит долю в 0..1: вертикальный прицел упирается в край клипа.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// HasBuildAnimation сообщает, есть ли чем показать возведение постройки.
func (a *Anim) HasBuildAnimation() bool {
	return a != nil && len(a.build) > 0
}

// SetProgress выбирает этап стройки по доле готовности и запускает его клип с
// начала: этап играет по своим часам (Step), а прогресс только решает, какой
// из них сейчас на сцене. Сам клип не скрабится — у таймлайнов события
// размечены в секундах, и пропорциональное время развело бы звук с кадром
// (у листенинг поста phase_in идёт пять секунд при двух процентах стройки).
func (a *Anim) SetProgress(progress float64) {
	if a == nil || len(a.build) == 0 {
		return
	}
	if !(progress > 0) {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	wasConstruct := a.state == AnimConstruct
	a.state = AnimConstruct
	index := a.buildStageAt(progress * 100)
	if wasConstruct && index == a.stage {
		return
	}
	a.stage = index
	a.time = 0
}

// buildStageAt — этап, в диапазон которого попал процент готовности. Этап
// без диапазона (phase_in) играет в самом начале, пока стройка ещё не дала
// прогресса; если процент вышел за все диапазоны, остаётся последний
// пройденный этап.
func (a *Anim) buildStageAt(percent float64) int {
	placed := -1
	for i, stage := range a.build {
		if !stage.ranged && placed < 0 {
			placed = i
		}
	}
	// Шаблон только поставили: играет появление (phase_in), даже если
	// диапазон первого этапа начинается с нуля.
	if percent <= 0 && placed >= 0 {
		return placed
	}
	best := -1
	for i, stage := range a.build {
		if !stage.ranged {
			continue
		}
		if percent >= stage.min && (percent < stage.max || stage.max >= 100) {
			return i
		}
		if stage.min <= percent && (best < 0 || stage.min > a.build[best].min) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	if placed >= 0 {
		return placed
	}
	return 0
}

// appendObjectModel укладывает модель в мир: анимируемый объект идёт с
// сохранением данных скелета (AppendEntityWithPose), статический — обычным
// способом. Разницы в геометрии нет: покой скелета даёт те же вершины.
func appendObjectModel(world *whm.Model, src *whm.Model, placement sgb.Placement, anim *Anim) {
	if anim != nil {
		sgb.AppendEntityWithPose(world, src, placement, world.Name, src.RestPose(), true)
		return
	}
	sgb.AppendEntity(world, src, placement, world.Name)
}

// poseBakedModel — модель, у которой при загрузке запекается поза в вершины
// (монолит некронов показывается в состоянии «ещё не достроен»). Её анимировать
// нельзя: палитра костей ссылалась бы на позу, которой в вершинах уже нет.
func poseBakedModel(whmPath string) bool {
	return strings.EqualFold(filepath.Base(strings.ReplaceAll(whmPath, `\`, "/")), "Necron_Monolith.whm")
}

// resolveActionClip — клип, к которому ведёт действие .whe (move → run_1,
// capture_strategic_point → одноимённый клип): первый клип цепочки, который
// есть у модели. nil, если ни действия, ни подходящего клипа нет.
func resolveActionClip(tree *modtree.ModTree, whmPath string, model *whm.Model, doc *whe.Document, action string) *whm.Animation {
	for _, name := range wheActionClips(doc, nil, action) {
		clip, _ := sgb.ResolveLinkedAnimation(tree, whmPath, model, name)
		if clip == nil {
			continue
		}
		if bound := sgb.BindAnimationToModel(model, clip); bound != nil && len(bound.Bones) > 0 {
			return bound
		}
	}
	return nil
}

// BoneCount — сколько строк костей занимает объект в общей палитре.
func (a *Anim) BoneCount() int {
	if a == nil {
		return 0
	}
	return len(a.model.Bones)
}

// SetState переключает состояние: следующий Step начинает клип нового
// состояния с начала — перехода без смешивания, как при смене приказа.
func (a *Anim) SetState(state AnimState) {
	if a == nil || a.state == state {
		return
	}
	// Состояние по приказу возвращает обычную анимацию: клипы, заданные
	// извне точкой захвата, больше не играют.
	a.frames = nil
	a.state = state
	a.time = 0
	a.stage = 0
	for i := range a.attackTimes {
		a.attackTimes[i] = 0
	}
}

// ClipFrame — клип и доля его проигрывания: 0 — первый кадр, 1 — последний.
// Так точка захвата связывает подъём флага с процентом захвата.
type ClipFrame struct {
	Name     string
	Progress float64
}

// SetClipFrames ставит объект на кадры клипов, заданных извне: клипы не
// идут по часам, а стоят на доле прогресса. Клипов может быть несколько —
// у некронов вместе с captured_raise играет necron_raise.
// false — ни одного из этих клипов у модели нет.
func (a *Anim) SetClipFrames(frames []ClipFrame) bool {
	if a == nil || a.model == nil {
		return false
	}
	driven := make([]clipFrame, 0, len(frames))
	for _, frame := range frames {
		clip := a.model.Animation(frame.Name)
		if clip == nil {
			continue
		}
		progress := frame.Progress
		if progress < 0 {
			progress = 0
		} else if progress > 1 {
			progress = 1
		}
		driven = append(driven, clipFrame{clip: clip, at: float32(progress) * clip.Duration})
	}
	if len(driven) == 0 {
		return false
	}
	a.frames = driven
	return true
}

// HasClip — у модели есть клип с таким именем.
func (a *Anim) HasClip(name string) bool {
	return a != nil && a.model != nil && a.model.Animation(name) != nil
}

// ClipDuration — длительность клипа в секундах; 0 — клипа нет.
func (a *Anim) ClipDuration(name string) float64 {
	if a == nil || a.model == nil {
		return 0
	}
	clip := a.model.Animation(name)
	if clip == nil {
		return 0
	}
	return float64(clip.Duration)
}

// VisibleWith — какие меши видны при наборе клипов (объединение треков
// видимости): так флаг расы показывается вместе с клипом подъёма.
// nil — модель такого состояния не описывает.
func (a *Anim) VisibleWith(names ...string) map[string]bool {
	if a == nil || a.model == nil {
		return nil
	}
	visible, ok := a.model.VisibleIn(names)
	if !ok {
		return nil
	}
	return visible
}

// Step продвигает время текущего клипа на dt секунд; клип зациклен.
func (a *Anim) Step(dt float64) {
	if a == nil || !(dt > 0) {
		return
	}
	// Кадры, заданные извне (точка захвата), временем не управляются.
	if len(a.frames) > 0 {
		return
	}
	// Стройка: этап выбрал прогресс, а клип этапа идёт по своим часам и
	// остаётся на последнем кадре (hold end), пока прогресс не сменит этап.
	if a.state == AnimConstruct {
		clip := a.clip()
		if clip == nil || clip.Duration <= 0 {
			return
		}
		if a.stage < 0 || a.stage >= len(a.build) {
			a.stage = 0
		}
		a.time += float32(dt / float64(clip.Duration))
		if a.time >= 1 {
			if a.build[a.stage].loop {
				a.time -= float32(math.Floor(float64(a.time)))
			} else {
				a.time = 1
			}
		}
		return
	}
	// Атака — набор клипов, и у каждого своё время: очередь пишется по
	// длительности самого клипа, а не общего действия.
	if a.state == AnimAttack {
		for i, clip := range a.attackClips() {
			if i >= len(a.attackTimes) || clip == nil || clip.Duration <= 0 {
				continue
			}
			t := a.attackTimes[i] + float32(dt/float64(clip.Duration))
			for t >= 1 {
				t--
			}
			a.attackTimes[i] = t
		}
		return
	}
	clip := a.clip()
	if clip == nil || clip.Duration <= 0 {
		return
	}
	a.time += float32(dt / float64(clip.Duration))
	for a.time >= 1 {
		a.time--
	}
}

// clip — клип текущего состояния; для capture берётся idle, если у модели
// нет клипа захвата.
func (a *Anim) clip() *whm.Animation {
	switch a.state {
	case AnimConstruct:
		if len(a.build) > 0 {
			if a.stage < 0 || a.stage >= len(a.build) {
				a.stage = 0
			}
			return a.build[a.stage].clip
		}
	case AnimMove:
		if a.move != nil {
			return a.move
		}
	case AnimCapture:
		if a.capture != nil {
			return a.capture
		}
	}
	return a.idle
}

// WritePalette пишет в dst палитру костей текущей позы: BoneCount()*16
// float32 в столбиковом порядке (как ждёт GLSL) по смещению базы объекта —// dst общий на кадр, по нему ходят все анимируемые объекты сцены.
// move — насколько объект сдвинут с места появления: перенос идёт уже
// поверх позы, поэтому добавляется отдельной матрицей впереди.
func (a *Anim) WritePalette(dst []float32, move [3]float64) {
	if a == nil || len(dst) < (a.boneBase+len(a.model.Bones))*16 {
		return
	}
	anims, times := a.pose()
	pose := a.model.PoseAtAnimationTimes(anims, times)
	var shift fauxgl.Matrix
	if move != ([3]float64{}) {
		shift = fauxgl.Translate(fauxgl.Vector{X: move[0], Y: move[1], Z: move[2]})
	} else {
		shift = fauxgl.Identity()
	}
	// Поворот — до сдвига: модель крутится у своих ступней, а потом
	// переносится туда, куда боец уже ушёл от точки появления.
	rotate := fauxgl.Identity()
	if delta := a.yaw - a.heading; math.Abs(delta) > 1e-4 {
		rotate = rotateAboutVertical(a.pos[0], a.pos[1], delta)
	}
	for b := range a.model.Bones {
		// Мировая палитра: (сдвиг × поворот × размещение) × поза × обратное
		// размещение.
		m := shift.Mul(rotate).Mul(a.P).Mul(pose.SkinMatrix(int32(b))).Mul(a.Pinv)
		base := (a.boneBase + b) * 16
		// fauxgl хранит матрицу построчно (Xrc — строка r, столбец c),
		// GLSL ждёт столбцовую раскладку: dst[c*4+r] = Xrc.
		dst[base+0] = float32(m.X00)
		dst[base+1] = float32(m.X10)
		dst[base+2] = float32(m.X20)
		dst[base+3] = float32(m.X30)
		dst[base+4] = float32(m.X01)
		dst[base+5] = float32(m.X11)
		dst[base+6] = float32(m.X21)
		dst[base+7] = float32(m.X31)
		dst[base+8] = float32(m.X02)
		dst[base+9] = float32(m.X12)
		dst[base+10] = float32(m.X22)
		dst[base+11] = float32(m.X32)
		dst[base+12] = float32(m.X03)
		dst[base+13] = float32(m.X13)
		dst[base+14] = float32(m.X23)
		dst[base+15] = float32(m.X33)
	}
}
