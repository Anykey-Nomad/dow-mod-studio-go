package main

import (
	"fmt"
	"image"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

const sgbEnvironmentRoot = `Data\attrib\ebps\environment`

type sgbEnvironmentEntry struct {
	folder   *modtree.TreeFolder
	file     *modtree.TreeFile
	depth    int
	expanded bool
}

type sgbPlacementRequest struct {
	blueprint string
	point     fauxgl.Vector
	owner     int
}

func sgbNewPlayerList() *widget.Enum {
	return &widget.Enum{Value: "neutral"}
}

type sgbObjectModelResult struct {
	blueprint string
	modelPath string
	model     *whm.Model
}

type sgbObjectMaterialRequest struct {
	blueprint string
	modelPath string
	model     *whm.Model
	entityID  int
}

type sgbObjectMaterialsResult struct {
	request     sgbObjectMaterialRequest
	materials   map[string]WhmMaterial
	rsh         map[string][]byte
	diagnostics map[string]modelMaterialDiagnostic
	missing     int
}

func sgbEnvironmentEntries(root *modtree.TreeFolder, expanded map[string]bool) []sgbEnvironmentEntry {
	if root == nil {
		return nil
	}
	var entries []sgbEnvironmentEntry
	var walk func(*modtree.TreeFolder, int)
	walk = func(folder *modtree.TreeFolder, depth int) {
		children := append([]*modtree.TreeFolder(nil), folder.Children...)
		sort.Slice(children, func(i, j int) bool { return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name) })
		for _, child := range children {
			if child == nil {
				continue
			}
			open := expanded[child.FullPath]
			entries = append(entries, sgbEnvironmentEntry{folder: child, depth: depth, expanded: open})
			if open {
				walk(child, depth+1)
			}
		}
		files := append([]*modtree.TreeFile(nil), folder.Files...)
		sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
		for _, file := range files {
			if file != nil && strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
				entries = append(entries, sgbEnvironmentEntry{file: file, depth: depth})
			}
		}
	}
	walk(root, 0)
	return entries
}

func (ui *uiState) ensureSgbObjectTree(tab *fileTab) {
	if tab == nil {
		return
	}
	if tab.sgbObjectExpanded == nil {
		tab.sgbObjectExpanded = make(map[string]bool)
	}
	if tab.sgbObjectClicks == nil {
		tab.sgbObjectClicks = make(map[string]*widget.Clickable)
	}
	if tab.sgbObjectRoot == nil && ui.modTree != nil {
		tab.sgbObjectRoot = ui.modTree.FindFolder(sgbEnvironmentRoot)
	}
}

func (ui *uiState) ensureSgbObjectModel(tab *fileTab) {
	if tab == nil || tab.sgbObjectSelected == "" || ui.modTree == nil || tab.sgbObjectModelCh != nil {
		return
	}
	blueprint, tree := tab.sgbObjectSelected, ui.modTree
	ch := make(chan sgbObjectModelResult, 1)
	tab.sgbObjectModelPath = blueprint
	tab.sgbObjectModelCh = ch
	go func() {
		model, modelPath := sgbBlueprintModelWithPath(tree, blueprint)
		ch <- sgbObjectModelResult{blueprint: blueprint, modelPath: modelPath, model: model}
	}()
}

func (ui *uiState) pollSgbObjectModel(tab *fileTab) bool {
	if tab == nil || tab.sgbObjectModelCh == nil {
		return false
	}
	select {
	case result := <-tab.sgbObjectModelCh:
		tab.sgbObjectModelCh = nil
		if strings.EqualFold(tab.sgbObjectSelected, result.blueprint) {
			tab.sgbObjectModel = result.model
			tab.sgbObjectModelFile = result.modelPath
			if result.model == nil {
				tab.sgbPlacementPending = nil
				ui.statusText = "Could not load selected environment object " + filepath.Base(result.blueprint)
			} else if len(result.model.Meshes) == 0 {
				ui.statusText = "Selected object has no renderable meshes: " + filepath.Base(result.blueprint)
			} else {
				ui.startSgbObjectMaterials(tab, result.blueprint, result.model)
			}
		}
		return true
	default:
		return false
	}
}

func clearSgbObjectSelection(tab *fileTab) {
	if tab == nil {
		return
	}
	tab.sgbObjectSelected = ""
	tab.sgbObjectModel = nil
	tab.sgbObjectModelPath = ""
	tab.sgbObjectModelFile = ""
	tab.sgbPlacementPending = nil
}

func (ui *uiState) layoutSgbObjectPanel(gtx layout.Context, tab *fileTab) layout.Dimensions {
	ui.ensureSgbObjectTree(tab)
	if tab.sgbObjectSelected != "" && tab.sgbObjectModel == nil {
		ui.ensureSgbObjectModel(tab)
	}
	if tab.sgbObjectModelCh != nil || tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) > 0 || tab.sgbPlacementPending != nil {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	if tab.sgbObjectClear.Clicked(gtx) {
		clearSgbObjectSelection(tab)
		ui.statusText = "Object placement cancelled"
	}
	if tab.sgbPlayerList == nil {
		tab.sgbPlayerList = sgbNewPlayerList()
	}
	if tab.sgbPlayerSet.Clicked(gtx) {
		if owner, ok := sgbPlayerOwner(tab.sgbPlayerList.Value); ok {
			ui.setSelectedSgbOwner(tab, owner)
		}
	}
	entries := sgbEnvironmentEntries(tab.sgbObjectRoot, tab.sgbObjectExpanded)
	return ui.panelWithScroll(gtx, &tab.sgbObjectScroll, func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Object Owner")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSgbPlayerSelector(gtx, tab)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hLine(gtx, cHeaderLine, 1)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.th, "Objects — EBPS Environment")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				msg := "Select an object, then click the map surface to place it."
				if tab.sgbObjectSelected != "" {
					msg = "Selected: " + filepath.Base(tab.sgbObjectSelected)
					if tab.sgbObjectModelCh != nil {
						msg += " (loading…)"
					}
				}
				l := material.Label(ui.th, unit.Sp(9), msg)
				l.Color = cTextDim
				l.MaxLines = 2
				return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
		)
		if tab.sgbObjectSelected != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.Button(ui.th, &tab.sgbObjectClear, "Clear selection")
				button.TextSize = unit.Sp(9)
				button.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(5)}.Layout(gtx, button.Layout)
			}))
		}
		if tab.sgbObjectRoot == nil {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutClassicOEHint(gtx, "", "Data/attrib/ebps/environment was not found in this module.")
			}))
		} else if len(entries) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutClassicOEHint(gtx, "", "Expand a folder to browse environment objects.")
			}))
		} else {
			for _, entry := range entries {
				entry := entry
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutSgbEnvironmentRow(gtx, tab, entry)
				}))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

type sgbPlayerOption struct {
	key, label string
	owner      int
}

var sgbPlayerOptions = func() []sgbPlayerOption {
	options := []sgbPlayerOption{{key: "neutral", label: "Neutral", owner: 0}}
	for player := 1; player <= 8; player++ {
		options = append(options, sgbPlayerOption{key: fmt.Sprintf("player-%d", player), label: fmt.Sprintf("Player %d", player), owner: player})
	}
	return options
}()

func sgbPlayerOwner(key string) (int, bool) {
	for _, option := range sgbPlayerOptions {
		if option.key == key {
			return option.owner, true
		}
	}
	return 0, false
}

func (ui *uiState) layoutSgbPlayerSelector(gtx layout.Context, tab *fileTab) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, sgbPlayerRadioChildren(ui, tab)...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			button := material.Button(ui.th, &tab.sgbPlayerSet, "Set")
			button.TextSize = unit.Sp(9)
			button.Inset = layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(8), Bottom: unit.Dp(5)}.Layout(gtx, button.Layout)
		}),
	)
}

func sgbPlayerRadioChildren(ui *uiState, tab *fileTab) []layout.FlexChild {
	children := make([]layout.FlexChild, 0, len(sgbPlayerOptions))
	for _, option := range sgbPlayerOptions {
		option := option
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			radio := material.RadioButton(ui.th, tab.sgbPlayerList, option.key, option.label)
			radio.TextSize = unit.Sp(9)
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(4), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, radio.Layout)
		}))
	}
	return children
}

func (ui *uiState) setSelectedSgbOwner(tab *fileTab, owner int) int {
	if tab == nil || tab.sgbMap == nil || len(tab.sgbSelectedEntities) == 0 || owner < 0 || owner > 8 {
		return 0
	}
	changed := 0
	for i := range tab.sgbMap.Entities {
		entity := &tab.sgbMap.Entities[i]
		if !tab.sgbSelectedEntities[entity.ID] {
			continue
		}
		if entity.Owner == owner {
			continue
		}
		entity.Owner = owner
		changed++
	}
	if changed > 0 {
		tab.sgbDirty = true
		ui.statusText = fmt.Sprintf("Assigned %s to %d selected map object(s)", sgbPlayerOptions[owner].label, changed)
	} else {
		found := false
		for _, entity := range tab.sgbMap.Entities {
			if tab.sgbSelectedEntities[entity.ID] {
				found = true
				break
			}
		}
		if !found {
			ui.statusText = "Select one or more map objects first"
		} else {
			ui.statusText = fmt.Sprintf("Selected map objects already belong to %s", sgbPlayerOptions[owner].label)
		}
	}
	return changed
}

func (ui *uiState) layoutSgbEnvironmentRow(gtx layout.Context, tab *fileTab, entry sgbEnvironmentEntry) layout.Dimensions {
	folder := entry.folder != nil
	key, label := "", ""
	if folder {
		key, label = "folder:"+entry.folder.FullPath, entry.folder.Name
	} else if entry.file != nil {
		key, label = "file:"+entry.file.FullPath, strings.TrimSuffix(entry.file.Name, filepath.Ext(entry.file.Name))
	} else {
		return layout.Dimensions{}
	}
	click := tab.sgbObjectClicks[key]
	if click == nil {
		click = &widget.Clickable{}
		tab.sgbObjectClicks[key] = click
	}
	if click.Clicked(gtx) {
		if folder {
			tab.sgbObjectExpanded[entry.folder.FullPath] = !entry.expanded
		} else {
			if strings.EqualFold(tab.sgbObjectSelected, entry.file.FullPath) {
				clearSgbObjectSelection(tab)
				ui.statusText = "Object placement cancelled"
			} else {
				tab.sgbObjectSelected = entry.file.FullPath
				tab.sgbObjectModel = nil
				tab.sgbObjectModelPath = ""
				tab.sgbObjectModelFile = ""
				tab.sgbPlacementPending = nil
				ui.ensureSgbObjectModel(tab)
				ui.statusText = "Loading map object " + filepath.Base(entry.file.FullPath)
			}
		}
	}
	selected := !folder && strings.EqualFold(tab.sgbObjectSelected, entry.file.FullPath)
	bg := cPanelBg
	if selected {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	h := gtx.Dp(unit.Dp(22))
	gtx.Constraints.Min = image.Pt(gtx.Constraints.Max.X, h)
	gtx.Constraints.Max = image.Pt(gtx.Constraints.Max.X, h)
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		defer func() {
			if folder {
				return
			}
			rect := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			pointer.CursorPointer.Add(gtx.Ops)
			rect.Pop()
		}()
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				marker := ""
				if folder {
					marker = "▸"
					if entry.expanded {
						marker = "▾"
					}
				}
				return layout.Inset{Left: unit.Dp(6 + entry.depth*12), Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), marker)
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), label)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) placeSgbObject(tab *fileTab, point fauxgl.Vector) {
	if tab == nil || tab.model == nil || tab.sgbMap == nil || tab.sgbObjectSelected == "" {
		return
	}
	if tab.model.loading || tab.modelMaterialCh != nil || tab.model.rendering || tab.model.frameCh != nil || tab.model.gpuRetries > 0 || tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) > 0 {
		tab.sgbPlacementPending = &sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}
		ui.statusText = "Object placement — waiting for the map scene to finish loading"
		return
	}
	if !strings.EqualFold(tab.sgbObjectModelPath, tab.sgbObjectSelected) || tab.sgbObjectModel == nil {
		tab.sgbPlacementPending = &sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}
		ui.ensureSgbObjectModel(tab)
		ui.statusText = "Loading selected environment object…"
		return
	}
	if tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) > 0 {
		tab.sgbPlacementPending = &sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}
		ui.statusText = "Loading selected object materials…"
		return
	}
	ui.appendSgbPlacement(tab, sgbPlacementRequest{blueprint: tab.sgbObjectSelected, point: point, owner: sgbSelectedPlayerOwner(tab)}, tab.sgbObjectModel)
}

func (ui *uiState) startSgbObjectMaterials(tab *fileTab, blueprint string, model *whm.Model) {
	if tab == nil || model == nil || ui.modTree == nil {
		return
	}
	ui.queueSgbObjectMaterials(tab, sgbObjectMaterialRequest{
		blueprint: blueprint,
		modelPath: tab.sgbObjectModelFile,
		model:     model,
	})
}

func (ui *uiState) queueSgbObjectMaterials(tab *fileTab, request sgbObjectMaterialRequest) {
	if tab == nil || request.model == nil {
		return
	}
	key := strings.ToLower(request.modelPath)
	if result, ok := tab.sgbObjectMaterialsCache[key]; ok {
		ui.applySgbObjectMaterials(tab, result)
		return
	}
	if tab.sgbObjectMaterialsLoaded == nil {
		tab.sgbObjectMaterialsLoaded = make(map[string]bool)
	}
	if request.modelPath != "" && tab.sgbObjectMaterialsLoaded[key] {
		return
	}
	if tab.sgbObjectMaterialsCh != nil && strings.EqualFold(tab.sgbObjectMaterialsActive.modelPath, request.modelPath) {
		if request.entityID != 0 && tab.sgbObjectMaterialsActive.entityID == 0 {
			tab.sgbObjectMaterialsActive.entityID = request.entityID
		}
		return
	}
	for _, queued := range tab.sgbObjectMaterialsPending {
		if strings.EqualFold(queued.modelPath, request.modelPath) {
			return
		}
	}
	tab.sgbObjectMaterialsPending = append(tab.sgbObjectMaterialsPending, request)
	ui.launchNextSgbObjectMaterials(tab)
}

func (ui *uiState) launchNextSgbObjectMaterials(tab *fileTab) {
	if tab == nil || tab.sgbObjectMaterialsCh != nil || len(tab.sgbObjectMaterialsPending) == 0 || ui.modTree == nil {
		return
	}
	request := tab.sgbObjectMaterialsPending[0]
	tab.sgbObjectMaterialsPending = tab.sgbObjectMaterialsPending[1:]
	tree := ui.modTree
	ch := make(chan sgbObjectMaterialsResult, 1)
	tab.sgbObjectMaterialsCh = ch
	tab.sgbObjectMaterialsActive = request
	go func() {
		rsh, materials, missing, diagnostics := sgbEntityMaterials(tree, request.modelPath, request.model, "", nil)
		ch <- sgbObjectMaterialsResult{
			request: request, materials: materials, rsh: rsh,
			diagnostics: diagnostics, missing: missing,
		}
	}()
}

func (ui *uiState) pollSgbObjectMaterials(tab *fileTab) bool {
	if tab == nil || tab.sgbObjectMaterialsCh == nil {
		return false
	}
	select {
	case result := <-tab.sgbObjectMaterialsCh:
		tab.sgbObjectMaterialsCh = nil
		tab.sgbObjectMaterialsActive = sgbObjectMaterialRequest{}
		key := strings.ToLower(result.request.modelPath)
		active := tab.sgbObjectMaterialsActive
		if tab.sgbObjectMaterialsCache == nil {
			tab.sgbObjectMaterialsCache = make(map[string]sgbObjectMaterialsResult)
		}
		cached := result
		cached.request.entityID = 0
		tab.sgbObjectMaterialsCache[key] = cached
		if tab.sgbObjectMaterialsLoaded == nil {
			tab.sgbObjectMaterialsLoaded = make(map[string]bool)
		}
		tab.sgbObjectMaterialsLoaded[key] = true
		result.request = active
		ui.applySgbObjectMaterials(tab, result)
		if result.missing > 0 {
			ui.statusText = fmt.Sprintf("Placed object materials: %d texture(s) could not be resolved", result.missing)
		}
		ui.launchNextSgbObjectMaterials(tab)
		return true
	default:
		return false
	}
}

func (ui *uiState) applySgbObjectMaterials(tab *fileTab, result sgbObjectMaterialsResult) {
	if tab == nil || tab.model == nil {
		return
	}
	if tab.model.materials == nil {
		tab.model.materials = make(map[string]WhmMaterial)
	}
	if tab.model.rshData == nil {
		tab.model.rshData = make(map[string][]byte)
	}
	if tab.model.materialDiagnostics == nil {
		tab.model.materialDiagnostics = make(map[string]modelMaterialDiagnostic)
	}
	if result.request.modelPath != "" && tab.sgbEntityModelFiles != nil {
		for id, modelPath := range tab.sgbEntityModelFiles {
			if !strings.EqualFold(modelPath, result.request.modelPath) {
				continue
			}
			prefix := fmt.Sprintf("entity-%d::", id)
			for _, mesh := range result.request.model.Meshes {
				if mesh.Material == "" {
					continue
				}
				unique := prefix + mesh.Material
				lookup := mesh.Material
				if _, ok := result.materials[lookup]; !ok && mesh.MaterialPath != "" {
					lookup = mesh.MaterialPath
				}
				if material, ok := result.materials[lookup]; ok {
					tab.model.materials[unique] = material
				}
				if data, ok := result.rsh[lookup]; ok {
					tab.model.rshData[unique] = data
				}
				if diagnostic, ok := result.diagnostics[lookup]; ok {
					tab.model.materialDiagnostics[unique] = diagnostic
				}
			}
		}
	}
	if len(result.materials) > 0 {
		tab.model.textured = true
	}
	tab.model.invalidate()
}

func (ui *uiState) deleteSelectedSgbEntities(tab *fileTab) {
	if tab == nil || tab.sgbMap == nil || tab.model == nil || len(tab.sgbSelectedEntities) == 0 {
		return
	}
	animated, removed := sgbRemoveEntities(tab.sgbMap, tab.model.modelData(), tab.sgbEntityMeshIDs, tab.model.animatedEntities, tab.sgbSelectedEntities)
	if removed == 0 {
		return
	}
	tab.model.animatedEntities = animated
	for id := range tab.sgbSelectedEntities {
		delete(tab.sgbEntityModelFiles, id)
	}
	clear(tab.sgbSelectedEntities)
	ui.geometryChangedModelGpu(tab.model.model)
	tab.model.invalidate()
	tab.sgbDirty = true
	ui.statusText = fmt.Sprintf("Deleted %d selected map object(s)", removed)
}

func sgbSelectedPlayerOwner(tab *fileTab) int {
	if tab == nil || tab.sgbPlayerList == nil {
		return 0
	}
	owner, ok := sgbPlayerOwner(tab.sgbPlayerList.Value)
	if !ok {
		return 0
	}
	return owner
}

func (ui *uiState) appendSgbPlacement(tab *fileTab, request sgbPlacementRequest, src *whm.Model) {
	if tab == nil || tab.sgbMap == nil || tab.model == nil || tab.model.model == nil || src == nil ||
		!strings.EqualFold(tab.sgbObjectSelected, request.blueprint) {
		return
	}
	m := tab.sgbMap
	blueprint := canonicalSgbBlueprintPath(request.blueprint)
	bp := -1
	for i, existing := range m.Blueprints {
		if strings.EqualFold(canonicalSgbBlueprintPath(existing), blueprint) {
			bp = i
			break
		}
	}
	if bp < 0 {
		bp = len(m.Blueprints)
		m.Blueprints = append(m.Blueprints, blueprint)
	}
	id := sgbNextPlacementID(m.Entities, tab.sgbNextEntityID)
	x, y, z := sgbPlacementWorldToEntity(request.point)
	if request.owner < 0 || request.owner > 8 {
		ui.statusText = fmt.Sprintf("Object placement failed: invalid player slot %d", request.owner)
		return
	}
	entity := sgb.Entity{ID: id, BP: bp, Owner: request.owner, Name: blueprint, X: x, Y: y, Z: z}
	if !sgbSceneAppendEntity(tab.model.modelData(), src, entity.PlacementOf(), entity.ID) {
		ui.statusText = "Object placement failed: model has no renderable meshes"
		return
	}
	// The viewer keeps the same WhmModel pointer as the GPU mesh-cache key.
	// Appending vertices does not change that pointer, so discard the cached
	// buffers before the next frame or the newly placed object stays invisible.
	ui.geometryChangedModelGpu(tab.model.model)
	tab.sgbNextEntityID = id + 1
	m.Entities = append(m.Entities, entity)
	tab.sgbDirty = true
	if tab.sgbEntityMeshIDs == nil {
		tab.sgbEntityMeshIDs = make(map[string]int)
	}
	tab.sgbEntityMeshIDs[fmt.Sprintf("entity-%d", entity.ID)] = entity.ID
	tab.sgbSelectedEntities = map[int]bool{entity.ID: true}
	if tab.sgbEntityModelFiles == nil {
		tab.sgbEntityModelFiles = make(map[int]string)
	}
	tab.sgbEntityModelFiles[entity.ID] = tab.sgbObjectModelFile
	ui.queueSgbObjectMaterials(tab, sgbObjectMaterialRequest{
		blueprint: request.blueprint,
		modelPath: tab.sgbObjectModelFile,
		model:     src,
		entityID:  entity.ID,
	})
	tab.model.invalidate()
	ui.statusText = fmt.Sprintf("Placed %s at %.1f, %.1f, %.1f", filepath.Base(request.blueprint), entity.X, entity.Z, entity.Y)
}

func sgbBlueprintModel(tree *modtree.ModTree, blueprintPath string) *whm.Model {
	model, _ := sgbBlueprintModelWithPath(tree, blueprintPath)
	return model
}

func sgbBlueprintModelWithPath(tree *modtree.ModTree, blueprintPath string) (*whm.Model, string) {
	if tree == nil || blueprintPath == "" {
		return nil, ""
	}
	path := strings.ReplaceAll(strings.TrimSpace(blueprintPath), "\\", "/")
	path = strings.TrimPrefix(path, "data:")
	path = strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(strings.ToLower(path), "data/") {
		path = "Data/" + path
	}
	if !strings.HasSuffix(strings.ToLower(path), ".rgd") {
		path += ".rgd"
	}
	data, err := tree.ReadFile(path)
	if err != nil {
		return nil, ""
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil || rf == nil || rf.Root == nil {
		return nil, ""
	}
	animator := strings.ReplaceAll(strings.TrimSpace(unitAnimatorName(rf)), "\\", "/")
	animator = strings.TrimPrefix(animator, "./")
	if animator == "" {
		return nil, ""
	}
	modelPath := "Data/art/ebps/" + strings.TrimPrefix(animator, "/")
	if !strings.HasSuffix(strings.ToLower(modelPath), ".whm") {
		modelPath += ".whm"
	}
	modelData, err := tree.ReadFile(modelPath)
	if err != nil {
		file := tree.FindByName(filepath.Base(modelPath))
		if file == nil {
			return nil, ""
		}
		modelData, err = tree.ReadTreeFile(file)
		if err != nil {
			return nil, ""
		}
		modelPath = file.FullPath
	}
	parsed, err := ParseWhmModel(modelData)
	if err != nil || parsed == nil || len(parsed.Model.Vertices) == 0 {
		return nil, ""
	}
	return &parsed.Model, modelPath
}
