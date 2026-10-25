// Package racebps reads the race blueprints of a Dawn of War module.
//
// Every race of the game has a blueprint in data/attrib/racebps: one .rgd per
// race, holding what the game needs to know about the race as a whole:
//
//	race_details       name_id (идентификатор локализации), playable
//	race_path          building_path, unit_path — где лежат модели расы
//	starting_buildings building_01 … building_NN — постройки, которые игрок
//	                   получает на старте
//
// The name of a race is not in the blueprint: name_id points into the
// localisation tables, and only those carry the text. The package therefore
// takes a locale table (package ucslocale) and fills the name from it.
package racebps

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/ucslocale"
)

// Folder — папка с чертежами рас внутри дерева модуля.
const Folder = `Data\attrib\racebps`

// StructuresRoot — папка, из которой берутся постройки рас.
const StructuresRoot = `Data\attrib\ebps\races`

// EbpsRoot — папка с чертежами игровых объектов.
const EbpsRoot = `Data\attrib\ebps`

// Race — раса модуля.
type Race struct {
	// ID — имя файла чертежа без расширения: "chaos_marine_race".
	ID string
	// Path — виртуальный путь чертежа в дереве модуля.
	Path string
	// NameID — идентификатор локализации имени: "$90020".
	NameID string
	// Name — название расы из таблицы локализации (пусто, если id не найден).
	Name string
	// Playable — можно ли за неё играть (у NPC-расы нельзя).
	Playable bool
	// BuildingPath — папка с постройками расы, как её пишет игра:
	// "Races/Chaos/Structures".
	BuildingPath string
	// UnitPath — папка с юнитами расы: "Races/Chaos/Troops".
	UnitPath string
	// StartingBuildings — постройки, которые игрок получает на старте:
	// ["chaos_hq"].
	StartingBuildings []string
}

// Label — название расы, а если его нет — имя чертежа.
func (r Race) Label() string {
	if r.Name != "" {
		return r.Name
	}
	return r.ID
}

// StructurePath — виртуальный путь чертежа постройки расы.
//
// Путь складывается из building_path чертежа расы и названия постройки:
// "Races/Chaos/Structures" и "chaos_hq" дают
// "Data/attrib/ebps/races/chaos/structures/chaos_hq.rgd".
func (r Race) StructurePath(building string) string {
	building = strings.TrimSpace(building)
	if building == "" {
		return ""
	}
	folder := r.buildingFolder()
	if folder == "" {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(building), ".rgd") {
		building += ".rgd"
	}
	return folder + "/" + building
}

// buildingFolder — папка с постройками расы в дереве модуля.
//
// Путь из чертежа расы отсчитывается от data/attrib/ebps: "Races/Chaos/
// Structures" значит data/attrib/ebps/races/chaos/structures.
func (r Race) buildingFolder() string {
	path := strings.ReplaceAll(strings.TrimSpace(r.BuildingPath), `\`, "/")
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}
	if len(parts) > 0 && parts[0] == "races" {
		return EbpsRoot + "/" + strings.Join(parts, "/")
	}
	return StructuresRoot + "/" + strings.Join(parts, "/")
}

// Load читает расы модуля: чертежи из data/attrib/racebps, названия — из
// таблицы локализации.
//
// Таблица может быть nil: тогда названия остаются пустыми, а Label() отдаёт
// имя файла. Расы, которые нельзя выбирать игроку (NPC), помечаются
// Playable = false, но остаются в списке: они нужны, чтобы увидеть, что
// модуль их вообще содержит.
func Load(tree *modtree.ModTree, loc *ucslocale.Table) ([]Race, error) {
	if tree == nil {
		return nil, fmt.Errorf("racebps: нет дерева модуля")
	}
	folder := tree.FindFolder(Folder)
	if folder == nil {
		return nil, fmt.Errorf("racebps: нет папки %s", Folder)
	}
	files := make([]*modtree.TreeFile, 0, len(folder.Files))
	files = append(files, folder.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	races := make([]Race, 0, len(files))
	for _, file := range files {
		if !strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
			continue
		}
		data, err := tree.ReadTreeFile(file)
		if err != nil {
			continue
		}
		rf, err := rgd.ParseRgd(data)
		if err != nil || rf == nil {
			continue
		}
		race, ok := raceFrom(rf, file.FullPath)
		if !ok {
			continue
		}
		if loc != nil {
			race.Name = loc.Text(stripDollar(race.NameID))
		}
		races = append(races, race)
	}
	return races, nil
}

// raceFrom читает расу из разобранного чертежа.
func raceFrom(rf *rgd.RgdFile, path string) (Race, bool) {
	if rf == nil || rf.Root == nil {
		return Race{}, false
	}
	race := Race{
		ID:     strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Path:   path,
		NameID: childValue(rf.Root, "race_details", "name_id"),
	}
	race.Playable = strings.EqualFold(childValue(rf.Root, "race_details", "playable"), "true")
	race.BuildingPath = childValue(rf.Root, "race_path", "building_path")
	race.UnitPath = childValue(rf.Root, "race_path", "unit_path")
	race.StartingBuildings = startingBuildings(rf.Root)
	if race.NameID == "" && race.BuildingPath == "" {
		return Race{}, false
	}
	return race, true
}

// startingBuildings читает список стартовых построек: поля building_01 и
// далее, в порядке их появления.
func startingBuildings(root *rgd.RgdEntry) []string {
	table := findChild(root, "starting_buildings")
	if table == nil {
		return nil
	}
	var out []string
	for _, c := range table.Children {
		value := strings.TrimSpace(c.DisplayValue())
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

// childValue читает значение вложенного поля: путь задаётся именами
// таблиц и поля, без учёта регистра.
func childValue(root *rgd.RgdEntry, path ...string) string {
	entry := root
	for _, name := range path {
		entry = findChild(entry, name)
		if entry == nil {
			return ""
		}
	}
	if entry == nil || entry.IsTable() {
		return ""
	}
	return strings.TrimSpace(entry.DisplayValue())
}

// findChild ищет дочернее поле по имени, без учёта регистра.
func findChild(entry *rgd.RgdEntry, name string) *rgd.RgdEntry {
	if entry == nil {
		return nil
	}
	for _, c := range entry.Children {
		if strings.EqualFold(c.Name, name) {
			return c
		}
	}
	return nil
}

// stripDollar убирает знак доллара у идентификатора локализации: "$90020"
// превращается в "90020".
func stripDollar(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "$")
}
