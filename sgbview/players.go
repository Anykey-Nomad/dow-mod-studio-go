package sgbview

import (
	"fmt"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
)

// StartingPositionBlueprint — часть пути к чертежу стартовой позиции.
// Сценарий ставит на карту объекты этого чертежа, и владелец такого объекта
// говорит, чья это стартовая позиция.
const StartingPositionBlueprint = "starting_position"

// MaxPlayers — сколько игроков в сценарии может быть: игра держит слоты
// с первого по восьмой.
const MaxPlayers = 8

// StartingPosition — стартовая позиция игрока на карте.
type StartingPosition struct {
	// Name — путь к чертежу объекта.
	Name string
	// X, Y, Z — мировые координаты объекта: X и Y лежат в плоскости карты,
	// Z — высота.
	X, Y, Z float32
	// Heading — поворот объекта вокруг вертикали, радианы.
	Heading float32
}

// SlotBuild — постройка, которая ставится на стартовую позицию игрока.
//
// Слот игрока определяет, куда именно ставить: у каждого игрока своя
// стартовая позиция, и постройка его расы появляется на ней.
type SlotBuild struct {
	// Slot — номер игрока (1..8).
	Slot int
	// Blueprint — путь к чертежу постройки:
	// "Data/attrib/ebps/races/chaos/structures/chaos_hq.rgd".
	Blueprint string
}

// PlayerSlot — слот игрока сценария.
type PlayerSlot struct {
	// Owner — номер слота: от 1 до 8. Ноль — нейтральный объект.
	Owner int
	// Positions — стартовые позиции игрока. Их может быть несколько:
	// на некоторых картах у игрока две базы.
	Positions []StartingPosition
}

// LoadPlayers читает из сценария слоты игроков и их стартовые позиции.
//
// Слоты считаются по стартовым позициям: у каждой стоит объект-маркер, и
// его владелец (1..8) — это номер игрока. Слоты без стартовых позиций не
// показываются: играть за них всё равно не с чего.
func LoadPlayers(tree *modtree.ModTree, sgbPath string) ([]PlayerSlot, error) {
	m, err := parseMap(tree, sgbPath)
	if err != nil {
		return nil, err
	}
	return playersOf(m), nil
}

// parseMap читает и разбирает сценарий из дерева модуля.
func parseMap(tree *modtree.ModTree, sgbPath string) (*sgb.Map, error) {
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
	return m, nil
}

// playersOf собирает слоты игроков по объектам сценария.
func playersOf(m *sgb.Map) []PlayerSlot {
	if m == nil {
		return nil
	}
	var order []int
	positions := map[int][]StartingPosition{}
	for _, e := range m.Entities {
		if !isStartingPosition(e.Name) {
			continue
		}
		if e.Owner < 1 || e.Owner > MaxPlayers {
			continue
		}
		if _, seen := positions[e.Owner]; !seen {
			order = append(order, e.Owner)
		}
		positions[e.Owner] = append(positions[e.Owner], StartingPosition{
			Name:    e.Name,
			X:       e.X,
			Y:       e.Y,
			Z:       e.Z,
			Heading: e.Heading,
		})
	}
	// Слоты идут по номеру игрока: так список читается как в игре.
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if order[j] < order[i] {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	slots := make([]PlayerSlot, 0, len(order))
	for _, owner := range order {
		slots = append(slots, PlayerSlot{Owner: owner, Positions: positions[owner]})
	}
	return slots
}

// isStartingPosition — является ли объект стартовой позицией.
func isStartingPosition(name string) bool {
	name = strings.ToLower(strings.ReplaceAll(name, `\`, "/"))
	return strings.Contains(name, StartingPositionBlueprint)
}
