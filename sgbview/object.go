package sgbview

import (
	"fmt"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
)

// ObjectForBlueprint загружает один объект по его чертежу и ставит его в мир
// по заданной точке: геометрию с текстурами, DATASEUI и анимацию (idle плюс
// стадии стройки). Нужно игре: постройка появляется посреди сессии, а не
// только из сценария, поэтому тот же путь, что и у объектов карты, вызывается
// по ходу игры.
//
// x и y — горизонтальные координаты точки постановки, z — её высота (в сцене
// вверх смотрит ось Z, как и в мире игры), heading — поворот вокруг
// вертикали в радианах, name — имя меша объекта (все части одного объекта
// носят одно имя: по нему рендерер находит его для сдвига и анимации).
// lift прижимает низ модели к земле: у бойцов пивот выше ступней, и без
// поправки они стоят по пояс в земле (у построек своё основание, их не
// трогаем).
func ObjectForBlueprint(tree *modtree.ModTree, blueprint string, x, y, z, heading float64, name string, lift bool) (*Object, error) {
	if tree == nil {
		return nil, fmt.Errorf("sgbview: нет дерева модуля")
	}
	if strings.TrimSpace(blueprint) == "" {
		return nil, fmt.Errorf("sgbview: пустой чертёж объекта")
	}
	if strings.TrimSpace(name) == "" {
		name = "object"
	}
	model, whmPath, visible, reason := loadBlueprintModel(tree, blueprint)
	if model == nil {
		if reason == "" {
			reason = "модель не загрузилась"
		}
		return nil, fmt.Errorf("sgbview: %s", reason)
	}
	if lift {
		z -= modelMinZ(model)
	}
	placement := sgb.Placement{X: x, Y: y, Z: z, Heading: heading, ScaleX: 1, ScaleY: 1, ScaleZ: 1}
	world := &whm.Model{Name: name}
	anim := newAnim(tree, whmPath, model, placement)
	// Точка захвата приходит с флагами всех рас (скрытыми): флаг
	// захватчика включается уже по ходу игры.
	src, hidden := captureSource(model, visible)
	appendObjectModel(world, src, placement, anim)
	parts, _ := partsFromWorld(src, world, hidden)
	if len(parts) == 0 {
		return nil, fmt.Errorf("sgbview: у модели нет геометрии: %s", whmPath)
	}
	object := &Object{
		Name:     blueprint,
		Parts:    parts,
		Selected: selectedUIWorld(selectedUI(tree, whmPath), placement),
		Anim:     anim,
	}
	// Действия нужны и без скелета: телепорт монолита — это jump_setup и
	// jump_breakdown с их клипами и таймлайнами, хотя кости модели запечены.
	if anim == nil {
		if doc, err := modelWHE(tree, whmPath); err == nil {
			object.Actions = NewActionSet(doc)
		}
	}
	// Текстуры ищутся по материалам модели: путь к .whm ведёт к её картинкам.
	paths := map[string]string{}
	for _, wm := range model.Meshes {
		if material := materialName(wm); material != "" {
			paths[material] = whmPath
		}
	}
	resolveObjectTextures(tree, []Object{*object}, paths)
	return object, nil
}

// MeshName — имя меша объекта в рендерере: у всех частей одного объекта оно
// одно, по нему объект находят для сдвига и анимации.
func (o *Object) MeshName() string {
	if o == nil {
		return ""
	}
	for _, part := range o.Parts {
		if part.Mesh != nil && part.Mesh.Name != "" {
			return part.Mesh.Name
		}
	}
	return ""
}
