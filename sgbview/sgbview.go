// Package sgbview — 3D-сцена сценария Dawn of War: рельеф, текстура земли
// и модели объектов с их текстурами.
//
// Пакет ничего не рисует: он читает .sgb из дерева модуля и отдаёт обычные
// Go-структуры, которые рендерер (Vulkan, OpenGL, ...) загружает в свои
// буферы. Логика чтения сценариев и текстур перенесена из редактора
// dow-mod-studio (sgb_scene.go, sgb_textures.go, viewer_materials.go).
//
// Мир сцены — Z вверх: X и Y лежат в плоскости карты, Z — высота. Это та же
// система координат, которую используют sgb.Map.TerrainModel и sgb.AppendEntity.
package sgbview

import (
	"fmt"
	"image"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
)

// TerrainMaterial — имя материала рельефа: у него своя, общая для всей
// карты, текстура земли.
const TerrainMaterial = "sgb-terrain"

// WaterMaterial — имя материала плоскости воды.
const WaterMaterial = sgb.WaterMaterialName

// WaterColor — цвет воды RGBA8 из редактора сценариев.
var WaterColor = sgb.WaterColor

// Vertex — вершина меша: позиция в метрах, нормаль и текстурные координаты.
type Vertex struct {
	Pos    [3]float32
	Normal [3]float32
	UV     [2]float32
}

// Mesh — геометрия: вершины и индексы треугольников.
type Mesh struct {
	// Name — имя группы (у объектов — «object-N»).
	Name string
	// Material — имя материала, по которому ищется текстура.
	Material string
	// Vertices, Indices — вершины и индексы треугольников.
	Vertices []Vertex
	Indices  []uint32
}

// Texture — текстура RGBA8. Одинаковый Key означает одну и ту же картинку:
// рендерер может грузить такую текстуру один раз.
type Texture struct {
	Key    string
	Width  int
	Height int
	// Pixels — RGBA8, по 4 байта на пиксель, построчно сверху вниз.
	Pixels []byte
}

// Part — одна группа геометрии объекта со своей текстурой.
type Part struct {
	Mesh    *Mesh
	Texture *Texture
}

// Object — объект на карте: геометрия в мировых координатах и текстуры.
type Object struct {
	// Name — имя объекта (путь к blueprint'у сценария).
	Name string
	// Parts — группы геометрии: у каждой своя текстура.
	Parts []Part
}

// Scene — собранная сцена сценария.
type Scene struct {
	// Name — имя карты из заголовка сценария.
	Name string
	// CellSize — размер ячейки карты в метрах.
	CellSize float32
	// GroundMin, GroundMax — границы карты в плоскости XY (метры).
	GroundMin, GroundMax [2]float32
	// CenterHeight — высота рельефа в центре карты (в метрах).
	CenterHeight float32
	// Terrain — рельеф (меш в мировых координатах, Z вверх).
	Terrain *Mesh
	// Water — плоскость воды (может быть nil).
	Water *Mesh
	// TerrainTexture — диффузная карта земли (тайлы + декали).
	TerrainTexture *Texture
	// Objects — объекты, которые удалось загрузить.
	Objects []Object
	// Entities — сколько объектов стоит на карте. Это больше, чем
	// len(Objects): часть объектов может не загрузиться (нет модели) или
	// не поместиться в лимиты.
	Entities int
	// Players — слоты игроков сценария и их стартовые позиции. Слот
	// появляется там, где на карте стоит объект-маркер стартовой позиции
	// с этим владельцем.
	Players []PlayerSlot
	// Dropped — сколько объектов не попало в сцену из-за лимитов.
	Dropped int
	// Skipped — объекты, которые загрузить не вышло, с причиной.
	// Сценарий может звать модель, которой в модуле нет, или модель может
	// не разбираться: без списка пропущенных непонятно, почему на карте
	// дыры.
	Skipped []Skipped
	// Min, Max — границы сцены в метрах (X, Y, Z).
	Min, Max [3]float32
}

// Skipped — объект сценария, который не попал в сцену, и причина,
// по которой он не попал.
type Skipped struct {
	// Name — имя объекта: путь к blueprint'у сценария.
	Name string
	// Reason — что именно не сработало.
	Reason string
}

// Limits — ограничения сборки сцены: большая карта и большое число
// объектов иначе не помещаются в память.
type Limits struct {
	// MaxGrid — сторона сетки рельефа (в вершинах).
	MaxGrid int
	// MaxObjects — сколько объектов брать.
	MaxObjects int
	// MaxVertices — сколько вершин суммарно во всей сцене.
	MaxVertices int
}

// DefaultLimits — разумные пределы по умолчанию.
//
// Предел объектов взят с запасом: в самом большом сценарии игры (на
// семь игроков) стоит около 3400 объектов, и лимит в 600 молча выкидывал
// всё, что не поместилось, — вместе с флагами и залежами руды.
func DefaultLimits() Limits {
	return Limits{MaxGrid: 250, MaxObjects: 4000, MaxVertices: 8_000_000}
}

// Load читает сценарий из дерева модуля и собирает сцену.
func Load(tree *modtree.ModTree, sgbPath string) (*Scene, error) {
	return LoadLimited(tree, sgbPath, DefaultLimits())
}

// LoadWith читает сценарий и собирает сцену, ставя на стартовые позиции
// игроков их стартовые постройки.
//
// Слот игрока указывает builds: по номеру слота сценарий знает, где стоит
// его начальная позиция, а постройка — что там должно появиться.
func LoadWith(tree *modtree.ModTree, sgbPath string, limits Limits, builds []SlotBuild) (*Scene, error) {
	if tree == nil {
		return nil, fmt.Errorf("sgbview: нет дерева модуля")
	}
	data, err := tree.ReadFile(sgbPath)
	if err != nil {
		return nil, fmt.Errorf("sgbview: чтение %s: %w", sgbPath, err)
	}
	m, err := sgb.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("sgbview: разбор %s: %w", sgbPath, err)
	}
	return BuildWith(tree, m, limits, builds)
}

// LoadLimited читает сценарий из дерева модуля и собирает сцену с
// заданными пределами.
func LoadLimited(tree *modtree.ModTree, sgbPath string, limits Limits) (*Scene, error) {
	return LoadWith(tree, sgbPath, limits, nil)
}

// Build собирает сцену по уже разобранной карте: рельеф, текстуру земли и
// объекты, расставленные по сценарию.
func Build(tree *modtree.ModTree, m *sgb.Map, limits Limits) (*Scene, error) {
	return BuildWith(tree, m, limits, nil)
}

// BuildWith собирает сцену и ставит на стартовые позиции игроков их
// стартовые постройки.
func BuildWith(tree *modtree.ModTree, m *sgb.Map, limits Limits, builds []SlotBuild) (*Scene, error) {
	if m == nil {
		return nil, fmt.Errorf("sgbview: карта пуста")
	}
	if limits.MaxGrid < 2 {
		limits.MaxGrid = DefaultLimits().MaxGrid
	}
	if limits.MaxObjects <= 0 {
		limits.MaxObjects = DefaultLimits().MaxObjects
	}
	if limits.MaxVertices <= 0 {
		limits.MaxVertices = DefaultLimits().MaxVertices
	}

	g := m.Ground
	scene := &Scene{
		Name:      m.Name,
		CellSize:  g.CellSize,
		GroundMin: [2]float32{g.XMin(), g.ZMin()},
		GroundMax: [2]float32{g.XMax(), g.ZMax()},
	}
	cx := (g.XMin() + g.XMax()) * 0.5
	cy := (g.ZMin() + g.ZMax()) * 0.5
	if h, ok := terrainHeightAt(m, float64(cx), float64(cy)); ok {
		scene.CenterHeight = float32(h)
	}

	// --- Рельеф и вода ---
	terrain := m.TerrainModel(limits.MaxGrid)
	if terrain != nil && len(terrain.Meshes) > 0 {
		scene.Terrain = meshFrom("terrain", TerrainMaterial, terrain, terrain.Meshes[0], nil)
		m.AddWaterMesh(terrain)
		for _, wm := range terrain.Meshes {
			if wm.Name == sgb.WaterMeshName {
				scene.Water = meshFrom(sgb.WaterMeshName, WaterMaterial, terrain, wm, nil)
				break
			}
		}
	}
	if img := buildTerrainTexture(tree, m); img != nil {
		scene.TerrainTexture = &Texture{
			Key:    TerrainMaterial,
			Width:  img.Bounds().Dx(),
			Height: img.Bounds().Dy(),
			Pixels: rgbaPixels(img),
		}
	}

	// --- Объекты ---
	objects, skipped, dropped, err := buildObjects(tree, m, limits, builds)
	if err != nil {
		return nil, err
	}
	scene.Objects = objects
	scene.Skipped = skipped
	scene.Entities = len(m.Entities)
	scene.Dropped = dropped
	scene.Players = playersOf(m)

	// --- Границы ---
	scene.Min, scene.Max = [3]float32{1e30, 1e30, 1e30}, [3]float32{-1e30, -1e30, -1e30}
	addBounds := func(mesh *Mesh) {
		for _, v := range mesh.Vertices {
			for a := 0; a < 3; a++ {
				if v.Pos[a] < scene.Min[a] {
					scene.Min[a] = v.Pos[a]
				}
				if v.Pos[a] > scene.Max[a] {
					scene.Max[a] = v.Pos[a]
				}
			}
		}
	}
	if scene.Terrain != nil {
		addBounds(scene.Terrain)
	}
	for _, o := range scene.Objects {
		for _, p := range o.Parts {
			if p.Mesh != nil {
				addBounds(p.Mesh)
			}
		}
	}
	if scene.Min[0] > scene.Max[0] {
		scene.Min, scene.Max = [3]float32{}, [3]float32{}
	}
	return scene, nil
}

// cachedModel — кэшированная модель blueprint'а вместе с её состоянием.
type cachedModel struct {
	model *whm.Model
	path  string
	// visible — меши, видимые в состоянии постройки без аддонов.
	// nil — видно всё: у модели нет клипа, который описывает состояние.
	visible map[string]bool
}

// buildObjects загружает модели объектов, расставленных по сценарию, и
// сажает их в мир сцены.
//
// Третьим значением возвращается число объектов, до которых сцена не
// дошла: лимиты на число объектов и вершин. Их не выкидывают молча —
// вызывающий код должен сказать пользователю, что часть карты не показана.
func buildObjects(tree *modtree.ModTree, m *sgb.Map, limits Limits, builds []SlotBuild) ([]Object, []Skipped, int, error) {
	if tree == nil || m == nil {
		return nil, nil, 0, nil
	}
	cache := map[int]cachedModel{}
	// reason кэша: почему модель не загрузилась, если не загрузилась.
	reasons := map[int]string{}
	// Пути моделей стартовых построек: по ним ищутся их текстуры.
	extraPaths := map[string]string{}

	var objects []Object
	var skipped []Skipped
	vertices := 0
	dropped := 0
	for _, e := range m.Entities {
		if len(objects) >= limits.MaxObjects || vertices >= limits.MaxVertices {
			dropped++
			continue
		}
		entry, seen := cache[e.BP]
		if !seen {
			model, path, visible, reason := loadEntityModel(tree, e)
			entry = cachedModel{model: model, path: path, visible: visible}
			cache[e.BP] = entry
			reasons[e.BP] = reason
		}
		if entry.model == nil {
			reason := reasons[e.BP]
			if reason == "" {
				reason = "модель не загрузилась"
			}
			skipped = append(skipped, Skipped{Name: e.Name, Reason: reason})
			continue
		}
		// Раскладываем модель в мир по месту объекта. Видимые меши отбираются
		// заранее, по исходным именам: AppendEntity переименовывает меши в имя
		// объекта, и фильтр visible по ним после копирования не сработал бы.
		src := visibleModel(entry.model, entry.visible)
		world := &whm.Model{Name: fmt.Sprintf("object-%d", e.ID)}
		sgb.AppendEntity(world, src, e.PlacementOf(), world.Name)
		parts := make([]Part, 0, len(world.Meshes))
		for _, wm := range world.Meshes {
			mesh := meshFrom(world.Name, materialName(wm), world, wm, nil)
			if mesh == nil {
				continue
			}
			parts = append(parts, Part{Mesh: mesh})
			vertices += len(mesh.Vertices)
		}
		if len(parts) == 0 {
			skipped = append(skipped, Skipped{Name: e.Name, Reason: "у модели нет геометрии"})
			continue
		}
		objects = append(objects, Object{Name: e.Name, Parts: parts})
	}

	// --- Стартовые постройки игроков ---
	buildObjects2, buildSkipped := placeSlotBuildings(tree, m, limits, builds, extraPaths)
	objects = append(objects, buildObjects2...)
	skipped = append(skipped, buildSkipped...)

	if len(objects) == 0 {
		return nil, skipped, dropped, nil
	}
	// Текстуры ищем пачкой: один обход дерева на все материалы.
	paths := cachePaths(cache)
	for mat, path := range extraPaths {
		paths[mat] = path
	}
	resolveObjectTextures(tree, objects, paths)
	return objects, skipped, dropped, nil
}

// placeSlotBuildings ставит стартовые постройки игроков на их стартовые
// позиции: постройка расы игрока появляется там, где у него начало.
//
// Пути моделей кладутся в extraPaths: по ним ищутся текстуры постройки.
func placeSlotBuildings(tree *modtree.ModTree, m *sgb.Map, limits Limits,
	builds []SlotBuild, extraPaths map[string]string) ([]Object, []Skipped) {
	if len(builds) == 0 {
		return nil, nil
	}
	slots := playersOf(m)
	var objects []Object
	var skipped []Skipped
	for _, b := range builds {
		if b.Slot < 1 || b.Slot > MaxPlayers || b.Blueprint == "" {
			continue
		}
		var positions []StartingPosition
		for _, s := range slots {
			if s.Owner == b.Slot {
				positions = s.Positions
				break
			}
		}
		if len(positions) == 0 {
			skipped = append(skipped, Skipped{
				Name:   b.Blueprint,
				Reason: fmt.Sprintf("у игрока %d нет стартовой позиции", b.Slot),
			})
			continue
		}
		model, path, visible, reason := loadBlueprintModel(tree, b.Blueprint)
		if model == nil {
			if reason == "" {
				reason = "модель не загрузилась"
			}
			skipped = append(skipped, Skipped{Name: b.Blueprint, Reason: reason})
			continue
		}
		for _, wm := range model.Meshes {
			if mat := materialName(wm); mat != "" {
				extraPaths[mat] = path
			}
		}
		for i, pos := range positions {
			world := &whm.Model{Name: fmt.Sprintf("building-%d-%d", b.Slot, i)}
			placement := sgb.Placement{
				X:       float64(pos.X),
				Y:       float64(pos.Z),
				Z:       float64(pos.Y),
				Heading: float64(pos.Heading),
				ScaleX:  1, ScaleY: 1, ScaleZ: 1,
			}
			src := visibleModel(model, visible)
			sgb.AppendEntity(world, src, placement, world.Name)
			parts := make([]Part, 0, len(world.Meshes))
			for _, wm := range world.Meshes {
				mesh := meshFrom(world.Name, materialName(wm), world, wm, nil)
				if mesh == nil {
					continue
				}
				parts = append(parts, Part{Mesh: mesh})
			}
			if len(parts) == 0 {
				skipped = append(skipped, Skipped{Name: b.Blueprint, Reason: "у модели нет геометрии"})
				continue
			}
			objects = append(objects, Object{Name: b.Blueprint, Parts: parts})
		}
	}
	return objects, skipped
}

// cachePaths собирает пути моделей кэша: по ним ищутся текстуры, лежащие
// рядом с моделью.
func cachePaths(cache map[int]cachedModel) map[string]string {
	paths := map[string]string{}
	for _, entry := range cache {
		if entry.path != "" && entry.model != nil {
			for _, wm := range entry.model.Meshes {
				mat := materialName(wm)
				if mat != "" {
					paths[mat] = entry.path
				}
			}
		}
	}
	return paths
}

// materialName — ключ поиска текстуры группы: путь из shared-шейдера, если
// он есть, иначе имя материала.
func materialName(wm whm.Mesh) string {
	if wm.MaterialPath != "" {
		return wm.MaterialPath
	}
	return wm.Material
}

// meshFrom переносит одну группу .whm в меш сцены: использованные вершины
// копируются в компактный список, индексы перенумеровываются.
//
// visible — состояние постройки без аддонов: меши, которых в нём нет, в
// сцену не попадают. nil означает, что видно всё.
func meshFrom(name, material string, model *whm.Model, wm whm.Mesh, visible map[string]bool) *Mesh {
	if model == nil || len(wm.Indices) == 0 {
		return nil
	}
	if visible != nil && !visible[strings.ToLower(wm.Name)] {
		return nil
	}
	mesh := &Mesh{Name: name, Material: material}
	remap := make(map[uint32]int, len(wm.Indices))
	mesh.Vertices = make([]Vertex, 0, len(wm.Indices))
	mesh.Indices = make([]uint32, 0, len(wm.Indices))
	for _, idx := range wm.Indices {
		if int(idx) >= len(model.Vertices) {
			continue
		}
		pos, ok := remap[idx]
		if !ok {
			pos = len(mesh.Vertices)
			remap[idx] = pos
			v := model.Vertices[idx]
			mesh.Vertices = append(mesh.Vertices, Vertex{
				Pos:    v.Pos,
				Normal: v.Normal,
				UV:     v.UV,
			})
		}
		mesh.Indices = append(mesh.Indices, uint32(pos))
	}
	if len(mesh.Indices) < 3 {
		return nil
	}
	return mesh
}

// visibleModel возвращает копию модели, оставляющую только меши, видимые в
// состоянии без аддонов (по нижнему регистру имени). Копия обязательна: кэш
// моделей общий между объектами одного blueprint, менять список мешей на
// месте нельзя. nil visible означает «ничего не отфильтровывать» и возвращает
// модель как есть.
//
// Отбор нужен до sgb.AppendEntity: он переименовывает каждый вложенный меш в
// имя объекта ("object-N"), и фильтр meshFrom по исходным именам мешей после
// такого копирования пропускал бы всё — объект уходил в Skipped как объект
// «без геометрии».
func visibleModel(model *whm.Model, visible map[string]bool) *whm.Model {
	if model == nil || visible == nil {
		return model
	}
	kept := make([]whm.Mesh, 0, len(model.Meshes))
	for _, wm := range model.Meshes {
		if visible[strings.ToLower(wm.Name)] {
			kept = append(kept, wm)
		}
	}
	if len(kept) == len(model.Meshes) {
		return model
	}
	filtered := *model
	filtered.Meshes = kept
	return &filtered
}

// loadEntityModel находит модель объекта: blueprint сценария (.rgd) хранит
// имя аниматора, а оно указывает на .whm.
//
// Третьим значением возвращается причина, по которой модель не нашлась:
// пустая строка означает, что всё сошлось.
func loadEntityModel(tree *modtree.ModTree, e sgb.Entity) (*whm.Model, string, map[string]bool, string) {
	return loadBlueprintModel(tree, e.Name)
}

// loadBlueprintModel находит модель по чертежу: путь к чертежу ведёт в
// entity_blueprint_ext/animator, а тот — к файлу .whm.
//
// Третьим значением возвращается состояние модели без аддонов: какие меши
// видны, когда аддоны ещё не построены. Для построек это важно: модель
// хранит и корпус, и надстройки аддонов сразу, и без этого состояния
// постройка выглядит уже достроенной.
//
// Последним значением возвращается причина, по которой модель не нашлась:
// пустая строка означает, что всё сошлось.
func loadBlueprintModel(tree *modtree.ModTree, blueprint string) (*whm.Model, string, map[string]bool, string) {
	if tree == nil || strings.TrimSpace(blueprint) == "" {
		return nil, "", nil, "объект без имени"
	}
	rgdPath := virtualPath(blueprint)
	if !strings.HasSuffix(strings.ToLower(rgdPath), ".rgd") {
		rgdPath += ".rgd"
	}
	data, err := tree.ReadFile(rgdPath)
	if err != nil {
		return nil, "", nil, "blueprint не найден: " + rgdPath
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf == nil {
		return nil, "", nil, "blueprint не разобран: " + rgdPath
	}
	animator := strings.ReplaceAll(strings.TrimSpace(animatorName(rf)), `\`, "/")
	animator = strings.TrimPrefix(animator, "./")
	if animator == "" {
		return nil, "", nil, "в blueprint'е нет модели: " + rgdPath
	}
	whmPath := "Data/art/ebps/" + strings.TrimPrefix(animator, "/")
	if !strings.HasSuffix(strings.ToLower(whmPath), ".whm") {
		whmPath += ".whm"
	}
	wdata, err := tree.ReadFile(whmPath)
	if err != nil {
		file := tree.FindByName(filepath.Base(whmPath))
		if file == nil {
			return nil, "", nil, "модель не найдена: " + whmPath
		}
		wdata, err = tree.ReadTreeFile(file)
		if err != nil {
			return nil, "", nil, "модель не читается: " + file.FullPath
		}
		whmPath = file.FullPath
	}
	parsed, err := whm.Parse(wdata)
	if err != nil {
		return nil, "", nil, "модель не разобрана: " + whmPath + ": " + err.Error()
	}
	if parsed == nil || len(parsed.Vertices) == 0 {
		return nil, "", nil, "модель пустая: " + whmPath
	}
	var visible map[string]bool
	if isStructure(rf) {
		// Граф действий .whe рядом с моделью говорит, какие клипы играют у
		// нетронутой постройки: аддоны не построены, повреждений нет,
		// ничего не строится. Видимость — объединение флагов этих клипов.
		if doc, err := modelWHE(tree, whmPath); err == nil {
			if clips := doc.RestClipNames(); len(clips) > 0 {
				if rest, ok := parsed.VisibleIn(clips); ok && len(rest) > 0 {
					visible = rest
				}
			}
		}
		if visible == nil {
			_, visible = parsed.BaseState()
		}
	}
	// Монолит некронов (races/necrons/structures) после стройки остаётся на
	// последнем кадре подъёма: кость bone_master держится на нём, пока
	// аддонов нет, а покой скелета — это уже состояние «все аддоны
	// достроены». Показать постройку как есть — значит запечь в вершины позу
	// успокоения, иначе монолит выглядит готовым до аддонов. Остальные
	// постройки не трогаем: их покой скелета и есть база.
	if strings.EqualFold(filepath.Base(whmPath), "Necron_Monolith.whm") {
		if clip := buildSettleClip(parsed); clip != nil {
			parsed = bakePose(parsed, parsed.PoseAt(clip, 1))
		}
	}
	return parsed, whmPath, visible, ""
}

// buildSettleClip — клип последней стадии стройки: среди build_stage_* тот,
// чья финальная поза ближе всех к покою скелета. Покой — это состояние
// «все аддоны достроены», а стройка поднимает постройку из земли стадия за
// стадией, поэтому ближайший к покою кадр — это и есть последняя стадия,
// на которой постройка успокаивается.
func buildSettleClip(m *whm.Model) *whm.Animation {
	var best *whm.Animation
	bestDev := float64(0)
	for i := range m.Animations {
		an := &m.Animations[i]
		if !strings.HasPrefix(strings.ToLower(an.Name), "build_stage") {
			continue
		}
		dev, ok := poseDeviation(m, an)
		if !ok {
			continue
		}
		if best == nil || dev < bestDev {
			best, bestDev = an, dev
		}
	}
	return best
}

// poseDeviation — суммарный квадрат смещения вершин относительно покоя
// скелета в финальном кадре клипа: насколько поза дальше от «как есть».
func poseDeviation(m *whm.Model, clip *whm.Animation) (float64, bool) {
	pose := m.PoseAt(clip, 1)
	var dev float64
	moved := false
	for i := range m.Vertices {
		p, _ := whm.SkinnedVertex(m, pose, &m.Vertices[i])
		dx := p.X - float64(m.Vertices[i].Pos[0])
		dy := p.Y - float64(m.Vertices[i].Pos[1])
		dz := p.Z - float64(m.Vertices[i].Pos[2])
		dev += dx*dx + dy*dy + dz*dz
		if dx != 0 || dy != 0 || dz != 0 {
			moved = true
		}
	}
	return dev, moved
}

// bakePose — копия модели с вершинами, перенесёнными в позу. Кости не
// трогаются: поза покоя даёт единичную матрицу, поэтому позже построенное
// AppendEntity в покое скелета вернёт эти координаты без изменений.
func bakePose(m *whm.Model, pose whm.Pose) *whm.Model {
	out := *m
	out.Vertices = make([]whm.Vertex, len(m.Vertices))
	for i := range m.Vertices {
		p, n := whm.SkinnedVertex(m, pose, &m.Vertices[i])
		v := m.Vertices[i]
		v.Pos = [3]float32{float32(p.X), float32(p.Y), float32(p.Z)}
		v.Normal = [3]float32{float32(n.X), float32(n.Y), float32(n.Z)}
		out.Vertices[i] = v
	}
	return &out
}

// modelWHE читает файл .whe, лежащий рядом с моделью: он хранит граф
// действий — состояния игры, мотионы и клипы, которые те ведут.
//
// Файла рядом с моделью может не быть, и это не ошибка: у части моделей
// граф живёт в другом месте или его вовсе нет.
func modelWHE(tree *modtree.ModTree, whmPath string) (*whe.Document, error) {
	whePath := strings.TrimSuffix(whmPath, filepath.Ext(whmPath)) + ".whe"
	f := tree.FindFile(whePath)
	if f == nil {
		return nil, fmt.Errorf("файл не найден: %s", whePath)
	}
	data, err := tree.ReadTreeFile(f)
	if err != nil {
		return nil, err
	}
	return whe.ParseDocument(data)
}

// isStructure — постройка ли это. У построек есть extension structure_ext,
// у юнитов его нет: у них вместо него combat_ext, cover_ext и moving_ext.
//
// Различие нужно, чтобы правило «показывать постройку без аддонов» не
// трогало модели юнитов: у них в клипе покоя тоже часть мешей скрыта, но
// это оружие в руках, а не аддон.
func isStructure(rf *rgd.RgdFile) bool {
	if rf == nil || rf.Root == nil {
		return false
	}
	return rgdFindChild(rf.Root, "structure_ext") != nil ||
		rgdFindChild(rf.Root, "structure_buildable_ext") != nil
}

// virtualPath приводит путь к чертежу к виду дерева модуля: "Data\...".
//
// Пути приходят откуда угодно: из сценария ("data:attrib\ebps\..."), из
// чертежа расы ("Data/attrib/ebps/races/...") или из дерева ("Data\...").
// Префикс "data:" игра ставит перед путём внутри архива, а папка Data в
// дереве — это корень, и добавлять её второй раз нельзя: получится
// "Data/Data/attrib/..." и чертеж не найдётся.
func virtualPath(path string) string {
	p := strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	if len(p) >= 5 && strings.EqualFold(p[:5], "data:") {
		p = p[5:]
	}
	p = strings.TrimLeft(p, "/")
	if len(p) < 5 || !strings.EqualFold(p[:5], "data/") {
		p = "Data/" + p
	}
	return p
}

// animatorName читает entity_blueprint_ext.animator из .rgd объекта: это
// строка с именем модели, а не ссылка на ассет.
func animatorName(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	ext := rgdFindChild(rf.Root, "entity_blueprint_ext")
	if ext == nil {
		return ""
	}
	e := rgdFindChild(ext, "animator")
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.DisplayValue())
}

// rgdFindChild ищет поле по имени, без учёта регистра.
func rgdFindChild(e *rgd.RgdEntry, name string) *rgd.RgdEntry {
	if e == nil {
		return nil
	}
	for _, c := range e.Children {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

// rgbaPixels переводит картинку в плотный массив RGBA8.
func rgbaPixels(img image.Image) []byte {
	if img == nil {
		return nil
	}
	if rgba, ok := img.(*image.RGBA); ok && rgba.Stride == rgba.Bounds().Dx()*4 {
		out := make([]byte, len(rgba.Pix))
		copy(out, rgba.Pix)
		return out
	}
	if nrgba, ok := img.(*image.NRGBA); ok && nrgba.Stride == nrgba.Bounds().Dx()*4 {
		out := make([]byte, len(nrgba.Pix))
		copy(out, nrgba.Pix)
		return out
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	out := make([]byte, w*h*4)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			out[i+0] = byte(r >> 8)
			out[i+1] = byte(g >> 8)
			out[i+2] = byte(bl >> 8)
			out[i+3] = byte(a >> 8)
			i += 4
		}
	}
	return out
}
