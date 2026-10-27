package app

import (
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whm"
	"fmt"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
	"image"
	"image/color"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// The 3D map view's scene: terrain with placed entities merged into the same
// model-viewer pipeline, with renderer mesh names mapped back to SGB entity IDs.
func sgbBuildSceneModel(tree *modtree.ModTree, m *sgb.Map, terrainMaterial string) *whm.Model {
	scene, _, _ := sgbBuildSceneModelWithEntityAnimations(tree, m, terrainMaterial)
	return scene
}

func sgbBuildSceneModelWithEntityIDs(tree *modtree.ModTree, m *sgb.Map, terrainMaterial string) (*whm.Model, map[string]int) {
	scene, entityMeshes, _ := sgbBuildSceneModelWithEntityAnimations(tree, m, terrainMaterial)
	return scene, entityMeshes
}

func sgbBuildSceneModelWithEntityAnimations(tree *modtree.ModTree, m *sgb.Map, terrainMaterial string) (*whm.Model, map[string]int, []sgb.AnimatedEntity) {
	entityMeshes := make(map[string]int)
	var animatedEntities []sgb.AnimatedEntity
	if m == nil {
		return nil, entityMeshes, animatedEntities
	}
	start := time.Now()
	scene := m.TerrainModel(sgbTerrainMaxGrid)
	if scene == nil && len(m.Entities) == 0 {
		return nil, entityMeshes, animatedEntities
	}
	if scene == nil {
		scene = &whm.Model{Name: "sgb-scene"}
	}
	if len(scene.Meshes) > 0 {
		scene.Meshes[0].Material = terrainMaterial
	}
	if !sceneHasMesh(scene, sgb.WaterMeshName) {
		m.AddWaterMesh(scene)
	}
	scene.Name = "sgb-scene"
	type blueprintModel struct {
		model     *whm.Model
		modelPath string
		idleAnim  string
		animation *whm.Animation
	}
	cache := map[int]blueprintModel{}
	placed, skipped, distinct := 0, 0, 0
	for _, e := range m.Entities {
		cached, seen := cache[e.BP]
		if !seen {
			src, modelPath := sgbLoadEntityModelWithPath(tree, e)
			cached = blueprintModel{model: src, modelPath: modelPath, idleAnim: sgbEntityIdleAnimationName(tree, m, e)}
			if cached.idleAnim == "" {
				cached.animation, _ = sgb.ResolveLinkedIdleAnimation(tree, modelPath, src)
			} else {
				cached.animation, _ = sgb.ResolveLinkedAnimation(tree, modelPath, src, cached.idleAnim)
			}
			cached.animation = sgb.BindAnimationToModel(src, cached.animation)
			cache[e.BP] = cached
		}
		src := cached.model
		if src == nil {
			if !seen {
				skipped++
			}
			continue
		}
		if !seen {
			distinct++
		}
		meshName := fmt.Sprintf("entity-%d", e.ID)
		boneBase := -1
		if cached.animation != nil && len(src.Bones) > 0 {
			boneBase = sgb.AppendEntityWithPose(scene, src, e.PlacementOf(), meshName, src.RestPose(), true)
			animatedEntities = append(animatedEntities, sgb.AnimatedEntity{ID: e.ID, BoneBase: boneBase, Placement: e.PlacementOf(), Model: src, Animation: cached.animation})
		} else {
			sgb.AppendEntity(scene, src, e.PlacementOf(), meshName)
		}
		for i := len(scene.Meshes) - 1; i >= 0 && scene.Meshes[i].Name == meshName; i-- {
			entityMeshes[meshName] = e.ID
		}
		placed++
	}
	sgbSceneBounds(scene)
	log.Printf("sgb: scene %s: %d/%d objects placed from %d distinct models (%d blueprints unresolved), %d verts, in %v",
		filepath.Base(m.Name), placed, len(m.Entities), distinct, skipped, len(scene.Vertices), time.Since(start))
	if placed == 0 {
		return nil, entityMeshes, animatedEntities
	}
	return scene, entityMeshes, animatedEntities
}

func sgbLoadEntityModelWithPath(tree *modtree.ModTree, e sgb.Entity) (*whm.Model, string) {
	if tree == nil || e.Name == "" {
		return nil, ""
	}
	rel := strings.ReplaceAll(strings.TrimSpace(e.Name), `\`, "/")
	rel = strings.TrimPrefix(rel, "data:")
	rgdPath := "Data/" + strings.TrimPrefix(rel, "/")
	if !strings.HasSuffix(strings.ToLower(rgdPath), ".rgd") {
		rgdPath += ".rgd"
	}
	data, err := tree.ReadFile(rgdPath)
	if err != nil {
		return nil, ""
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		return nil, ""
	}
	animator := strings.ReplaceAll(strings.TrimSpace(unitAnimatorName(rf)), `\`, "/")
	animator = strings.TrimPrefix(animator, "./")
	if animator == "" {
		return nil, ""
	}
	whmPath := "Data/art/ebps/" + strings.TrimPrefix(animator, "/")
	if !strings.HasSuffix(strings.ToLower(whmPath), ".whm") {
		whmPath += ".whm"
	}
	wdata, err := tree.ReadFile(whmPath)
	if err != nil {
		file := tree.FindByName(filepath.Base(whmPath))
		if file == nil {
			return nil, ""
		}
		wdata, err = tree.ReadTreeFile(file)
		if err != nil {
			return nil, ""
		}
		whmPath = file.FullPath
	}
	parsed, err := ParseWhmModel(wdata)
	if err != nil || parsed == nil || len(parsed.Model.Vertices) == 0 {
		return nil, ""
	}
	return &parsed.Model, whmPath
}

func EnsureSgbObjectModel(ui *uiState, tab *fileTab) {
	if tab == nil || tab.SgbObjectSelected == "" || ui.ModTree == nil || tab.SgbObjectModelCh != nil {
		return
	}
	blueprint, tree := tab.SgbObjectSelected, ui.ModTree
	ch := make(chan sgbObjectModelResult, 1)
	tab.SgbObjectModelPath = blueprint
	tab.SgbObjectModelCh = ch
	go func() {
		model, modelPath := sgbBlueprintModelWithPath(tree, blueprint)
		ch <- sgbObjectModelResult{Blueprint: blueprint, ModelPath: modelPath, Model: model}
	}()
}

func LayoutSgbObjectPanel(ui *uiState, gtx layout.Context, tab *fileTab) layout.Dimensions {
	EnsureSgbObjectTree(ui, tab)
	if tab.SgbObjectSelected != "" && tab.SgbObjectModel == nil {
		EnsureSgbObjectModel(ui, tab)
	}
	if tab.SgbObjectModelCh != nil || tab.SgbObjectMaterialsCh != nil || len(tab.SgbObjectMaterialsPending) > 0 || tab.SgbPlacementPending != nil {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	if tab.SgbObjectClear.Clicked(gtx) {
		clearSgbObjectSelection(tab)
		ui.StatusText = "Object placement cancelled"
	}
	if tab.SgbPlayerList == nil {
		tab.SgbPlayerList = sgbNewPlayerList()
	}
	if tab.SgbPlayerSet.Clicked(gtx) {
		if owner, ok := sgbPlayerOwner(tab.SgbPlayerList.Value); ok {
			SetSelectedSgbOwner(ui, tab, owner)
		}
	}
	entries := sgbEnvironmentEntries(tab.SgbObjectRoot, tab.SgbObjectExpanded)
	return PanelWithScroll(ui, gtx, &tab.SgbObjectScroll, func(gtx layout.Context) layout.Dimensions {
		var children []layout.FlexChild
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.Th, "Object Owner")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutSgbPlayerSelector(ui, gtx, tab)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hLine(gtx, uikit.CHeaderLine, 1)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionHeader(gtx, ui.Th, "Objects — EBPS Environment")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				msg := "Select an object, then click the map surface to place it."
				if tab.SgbObjectSelected != "" {
					msg = "Selected: " + filepath.Base(tab.SgbObjectSelected)
					if tab.SgbObjectModelCh != nil {
						msg += " (loading…)"
					}
				}
				l := material.Label(ui.Th, unit.Sp(9), msg)
				l.Color = uikit.CTextDim
				l.MaxLines = 2
				return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
		)
		if tab.SgbObjectSelected != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.Button(ui.Th, &tab.SgbObjectClear, "Clear selection")
				button.TextSize = unit.Sp(9)
				button.Inset = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(5)}.Layout(gtx, button.Layout)
			}))
		}
		if tab.SgbObjectRoot == nil {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutClassicOEHint(ui, gtx, "", "Data/attrib/ebps/environment was not found in this module.")
			}))
		} else if len(entries) == 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutClassicOEHint(ui, gtx, "", "Expand a folder to browse environment objects.")
			}))
		} else {
			for _, entry := range entries {
				entry := entry
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutSgbEnvironmentRow(ui, gtx, tab, entry)
				}))
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func LayoutSgbEnvironmentRow(ui *uiState, gtx layout.Context, tab *fileTab, entry sgbEnvironmentEntry) layout.Dimensions {
	folder := entry.Folder != nil
	key, label := "", ""
	if folder {
		key, label = "folder:"+entry.Folder.FullPath, entry.Folder.Name
	} else if entry.File != nil {
		key, label = "file:"+entry.File.FullPath, strings.TrimSuffix(entry.File.Name, filepath.Ext(entry.File.Name))
	} else {
		return layout.Dimensions{}
	}
	click := tab.SgbObjectClicks[key]
	if click == nil {
		click = &widget.Clickable{}
		tab.SgbObjectClicks[key] = click
	}
	if click.Clicked(gtx) {
		if folder {
			tab.SgbObjectExpanded[entry.Folder.FullPath] = !entry.Expanded
		} else {
			if strings.EqualFold(tab.SgbObjectSelected, entry.File.FullPath) {
				clearSgbObjectSelection(tab)
				ui.StatusText = "Object placement cancelled"
			} else {
				tab.SgbObjectSelected = entry.File.FullPath
				tab.SgbObjectModel = nil
				tab.SgbObjectModelPath = ""
				tab.SgbObjectModelFile = ""
				tab.SgbPlacementPending = nil
				EnsureSgbObjectModel(ui, tab)
				ui.StatusText = "Loading map object " + filepath.Base(entry.File.FullPath)
			}
		}
	}
	selected := !folder && strings.EqualFold(tab.SgbObjectSelected, entry.File.FullPath)
	bg := uikit.CPanelBg
	if selected {
		bg = uikit.CSelectBg
	} else if click.Hovered() {
		bg = uikit.CMenuHover
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
				if bg != uikit.CPanelBg {
					rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				marker := ""
				if folder {
					marker = "▸"
					if entry.Expanded {
						marker = "▾"
					}
				}
				return layout.Inset{Left: unit.Dp(6 + entry.Depth*12), Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), marker)
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), label)
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func PlaceSgbObject(ui *uiState, tab *fileTab, point fauxgl.Vector) {
	if tab == nil || tab.Model == nil || tab.SgbMap == nil || tab.SgbObjectSelected == "" {
		return
	}
	if tab.Model.Loading || tab.ModelMaterialCh != nil || tab.Model.Rendering || tab.Model.FrameCh != nil || tab.Model.GpuRetries > 0 || tab.SgbObjectMaterialsCh != nil || len(tab.SgbObjectMaterialsPending) > 0 {
		tab.SgbPlacementPending = &sgbPlacementRequest{Blueprint: tab.SgbObjectSelected, Point: point, Owner: sgbSelectedPlayerOwner(tab)}
		ui.StatusText = "Object placement — waiting for the map scene to finish loading"
		return
	}
	if !strings.EqualFold(tab.SgbObjectModelPath, tab.SgbObjectSelected) || tab.SgbObjectModel == nil {
		tab.SgbPlacementPending = &sgbPlacementRequest{Blueprint: tab.SgbObjectSelected, Point: point, Owner: sgbSelectedPlayerOwner(tab)}
		EnsureSgbObjectModel(ui, tab)
		ui.StatusText = "Loading selected environment object…"
		return
	}
	if tab.SgbObjectMaterialsCh != nil || len(tab.SgbObjectMaterialsPending) > 0 {
		tab.SgbPlacementPending = &sgbPlacementRequest{Blueprint: tab.SgbObjectSelected, Point: point, Owner: sgbSelectedPlayerOwner(tab)}
		ui.StatusText = "Loading selected object materials…"
		return
	}
	AppendSgbPlacement(ui, tab, sgbPlacementRequest{Blueprint: tab.SgbObjectSelected, Point: point, Owner: sgbSelectedPlayerOwner(tab)}, tab.SgbObjectModel)
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

// startSgbSceneLoad builds the map's diffuse and its object scene off the UI
// goroutine, in two results over the material channel every viewer already
// polls: the terrain diffuse first, so the ground appears as soon as it is
// baked, then the model with the placed objects plus their materials,
// resolved through the same .rsh lookup a .whm tab uses. The whole build
// reads files and bakes pixels for seconds on a real map, so none of it may
// run inside a layout pass; v.Loading keeps layoutModelViewer from starting
// its own .rsh lookup over the terrain material while this is in flight, and
// both paths answer even on failure so the viewer settles instead of waiting
// forever.
func StartSgbSceneLoad(ui *uiState, t *fileTab, material string) {
	if t == nil || t.Model == nil {
		return
	}
	t.Model.Loading = true
	tree := ui.ModTree
	path := t.Path
	m := t.SgbMap
	ch := make(chan modelMaterialResult, 2)
	t.ModelMaterialCh = ch
	go func() {
		// Phase one: the ground. Its diffuse is the only material the
		// terrain-only model the tab opened with can show.
		ground := modelMaterialResult{Materials: map[string]render.WhmMaterial{}, More: true}
		if img := sgbBuildTerrainTexture(tree, path, m); img != nil {
			ground.Materials[material] = render.WhmMaterial{Texture: render.NewModelTexture(img), Color: render.RgbaWhite}
		} else {
			ground.Missing = 1
		}
		ch <- ground
		// Phase two: the scene — terrain plus every resolvable object —
		// with the entity .rsh materials alongside the ground's.
		res := modelMaterialResult{Materials: map[string]render.WhmMaterial{}, Missing: ground.Missing,
			Diagnostics: map[string]modelMaterialDiagnostic{},
		}
		for name, mat := range ground.Materials {
			res.Materials[name] = mat
			res.Diagnostics[name] = modelMaterialDiagnostic{Format: "SGB terrain diffuse", Status: "decoded in memory"}
		}
		if scene, entityMeshes, animatedEntities := sgbBuildSceneModelWithEntityAnimations(tree, m, material); scene != nil {
			res.Model = &WhmModel{Model: *scene}
			res.EntityMeshes = entityMeshes
			res.AnimatedEntities = animatedEntities
			var entityDiagnostics map[string]modelMaterialDiagnostic
			res.Rsh, res.Materials, res.Missing, entityDiagnostics = sgbEntityMaterials(tree, path, scene, material, res.Materials)
			for name, diag := range entityDiagnostics {
				res.Diagnostics[name] = diag
			}
		}
		ch <- res
	}()
}

func LayoutSgbViewer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "No scenario selected")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	if t.SgbError != "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.SgbError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	m := t.SgbMap
	if m == nil || t.SgbImage == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "No map data")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	// The switch trades the flat map for the 3D terrain and back; both
	// views read the same parsed map, so nothing is reparsed on the way.
	if t.SgbViewSwitch.Clicked(gtx) {
		t.SgbView3D = !t.SgbView3D
	}
	if t.SgbView3D {
		SgbEnsureTerrain(ui, t)
	} else {
		// The overlays are part of the image, so a toggle rebuilds it once —
		// the maps are a million cells at most and the rebuild is milliseconds.
		for _, tg := range sgbToggles(t) {
			if tg.Click.Clicked(gtx) {
				t.SgbMask ^= tg.Bit
				t.SgbImage = buildSgbImage(m, t.SgbMask)
			}
		}
	}
	info := fmt.Sprintf("%s  ·  %s  ·  %d × %d cells  ·  %.0f × %.0f m  ·  %d decals  ·  %d entities  ·  %d textures",
		m.Name, m.Engine, m.Ground.CellsX, m.Ground.CellsZ,
		float64(m.Ground.CellsX)*float64(m.Ground.CellSize),
		float64(m.Ground.CellsZ)*float64(m.Ground.CellSize),
		len(m.Decals), len(m.Entities), len(m.TexNames))
	viewLabel := "3D"
	if t.SgbView3D {
		viewLabel = "2D"
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), info)
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					layout.Flexed(1, rowGap()),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if t.SgbView3D {
							return layout.Dimensions{}
						}
						return SgbOverlayBar(ui, gtx, t)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButton(ui, gtx, &t.SgbViewSwitch, viewLabel)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if t.SgbView3D && t.Model != nil {
				// The scroll panel measures its tabs with a synthetic
				// enormous height; rendering at that size recreates the
				// render target absurdly tall and fails the device. The
				// .whm viewer guards the same way (whmViewerLayout).
				if whmMeasurePass(gtx.Constraints.Max.Y) {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}
				gtx.Constraints.Min.Y = 0
				return LayoutModelViewer(ui, gtx, t)
			}
			return DrawSgbCanvas(ui, gtx, t)
		}),
	)
}

// sgbEnsureTerrain builds the tab's 3D terrain once, on the first switch to
// the 3D view: a heightmap mesh and the shaded relief as its diffuse map. The
// viewer is a plain model viewer, so the canvas, camera and both renderers
// need no sgb-specific path — only the material handed in here.
func SgbEnsureTerrain(ui *uiState, t *fileTab) {
	if t == nil || t.SgbMap == nil || t.Model != nil {
		return
	}
	start := time.Now()
	material := sgbTerrainMaterial(t.Path)
	model := sgbTerrainModel(t.SgbMap, material)
	if model == nil {
		return
	}
	v := newModelViewer(model, t.Path)
	// The framing a model viewer opens on is the whole model at 1.4× its
	// bounds — for a map that is hundreds of metres, the game's most
	// zoomed-out camera at its closest. A map opens on an in-game-like
	// distance instead, and its zoom range runs from a close orbit up to
	// the whole-map fit rather than a fraction of it.
	v.DefaultDistance = sgbCameraDefaultDistance(t.SgbMap)
	v.Distance = v.DefaultDistance
	if height, ok := sgbTerrainHeightAt(t.SgbMap, v.Center.X, v.Center.Y); ok {
		v.Center.Z = height
	}
	v.MinDistance = sgbCameraMinDistance
	v.MaxDistance = v.FitDistance
	v.Yaw, v.Pitch = sgbCameraYaw, sgbCameraPitch
	t.Model = v
	// The diffuse — companion .tga, tile blend, every decal baked — and
	// the object scene on top of it cost seconds on a real map, so both
	// are built on a worker and arrive through the material channel every
	// viewer already polls: the ground first, the placed objects with
	// their materials after. Until then the terrain draws untextured
	// rather than the layout stalling.
	StartSgbSceneLoad(ui, t, material)
	log.Printf("sgb: terrain %s ready in %v: %d verts, %d tris, material %q",
		filepath.Base(t.Path), time.Since(start), len(model.Vertices), len(model.Meshes[0].Indices)/3, material)
}

// sgbCameraTargetScreen projects the orbit target into the renderer's pixel
// space, shared by the software and GPU overlay paths.
func sgbCameraTargetScreen(v *modelViewer, width, height int) (fauxgl.Vector, bool) {
	if v == nil || width < 1 || height < 1 {
		return fauxgl.Vector{}, false
	}
	at := fxProject(render.BuildViewMatrix(Options(v, width, height)), v.Center, width, height)
	if at == nil {
		return fauxgl.Vector{}, false
	}
	return *at, true
}

// paintSgbCameraTargetOverlay draws nested contrasting circles so the current
// surface point remains easy to spot against both terrain and placed objects.
func PaintSgbCameraTargetOverlay(v *modelViewer, gtx layout.Context, width, height int) {
	at, ok := sgbCameraTargetScreen(v, width, height)
	if !ok {
		return
	}
	x, y := int(at.X+0.5), int(at.Y+0.5)
	for _, mark := range []struct {
		radius int
		color  color.NRGBA
	}{
		{15, color.NRGBA{R: 12, G: 14, B: 18, A: 245}},
		{11, color.NRGBA{R: 255, G: 194, B: 40, A: 255}},
		{7, color.NRGBA{R: 16, G: 20, B: 25, A: 255}},
		{3, color.NRGBA{R: 255, G: 255, B: 240, A: 255}},
	} {
		bounds := image.Rect(x-mark.radius, y-mark.radius, x+mark.radius+1, y+mark.radius+1)
		paint.FillShape(gtx.Ops, mark.color, clip.Ellipse(bounds).Op(gtx.Ops))
	}
}
