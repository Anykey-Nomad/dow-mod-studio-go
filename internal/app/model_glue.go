package app

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/sgb"
	"fmt"
	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"image"
	"log"
	"math"
	"os"
	"time"
)

// pollModelJobs runs on the UI goroutine and applies texture/FX results.
func PollModelJobs(ui *uiState) {
	for _, t := range ui.OpenTabs {
		PollViewerJobs(ui, t)
		PollSgbObjectModel(ui, t)
		PollSgbObjectMaterials(ui, t)
		if t.SgbPlacementPending != nil && t.Model != nil && !t.Model.Loading && !t.Model.Rendering && t.Model.FrameCh == nil && t.ModelMaterialCh == nil && t.SgbObjectModelCh == nil && t.SgbObjectMaterialsCh == nil && len(t.SgbObjectMaterialsPending) == 0 && t.SgbObjectModel != nil {
			request := *t.SgbPlacementPending
			t.SgbPlacementPending = nil
			AppendSgbPlacement(ui, t, request, t.SgbObjectModel)
		}
	}
	// The object window's viewport has a viewer that is not a file tab, and it
	// renders and loads textures exactly the same way.
	PollViewerJobs(ui, ui.UnitModel)
}

// pollViewerJobs takes whatever one viewer has finished: its textures and FX.
// A nil tab, or one with no model on it, is the normal case for most of them.
func PollViewerJobs(ui *uiState, t *fileTab) {
	if t == nil || t.Model == nil {
		return
	}
	v := t.Model
	if t.ModelMaterialCh != nil {
		select {
		case res := <-t.ModelMaterialCh:
			if !res.More {
				t.ModelMaterialCh = nil
			}
			v.Loading = false
			v.MaterialsLoaded = !res.More
			v.Materials = res.Materials
			v.MaterialAliases = res.Aliases
			if len(res.Materials) > 0 {
				v.Textured = true
			}
			log.Printf("3D preview: textures ready: %d Materials resolved, %d Missing", len(res.Materials)+len(res.Rsh), res.Missing)
			if len(res.Rsh) > 0 {
				v.RshData = res.Rsh
			}
			if res.Diagnostics != nil {
				v.MaterialDiagnostics = res.Diagnostics
			}
			if res.Missing > 0 {
				v.Missing = res.Missing
			}
			// A map scene swaps in the model with its objects; a .whm
			// tab never sends one and keeps its model unchanged.
			if res.Model != nil {
				v.Model = res.Model
			}
			if res.Model != nil {
				t.Model.AnimatedEntities = res.AnimatedEntities
			}
			if res.Model != nil && res.EntityMeshes != nil {
				t.SgbEntityMeshIDs = res.EntityMeshes
				if t.SgbSelectedEntities == nil {
					t.SgbSelectedEntities = make(map[int]bool)
				}
			}
			Invalidate(v)
		default:
		}
	}
	if v.FxCh != nil {
		select {
		case res := <-v.FxCh:
			FxApply(v, res)
		default:
		}
	}
	if v.FxSoundCh != nil {
		select {
		case res := <-v.FxSoundCh:
			FxApplySoundsOnly(v, res)
		default:
		}
	}
	if v.FrameCh != nil {
		select {
		case frame := <-v.FrameCh:
			v.FrameCh = nil
			v.Rendering = false
			if frame.Serial >= v.Serial {
				v.Serial = frame.Serial
				v.Frame = frame.Image
			} else {
				// A frame the camera already moved past.
				v.Redraw = true
			}
			// A camera move during the render means the frame is already
			// stale; start the next one right away.
			if v.Pending {
				v.Pending = false
				v.Redraw = true
			}
		default:
		}
	}
}

// modelControlsWithUV is the strip under a .whm canvas. The mapping controls
// are a dialog of their own, so there is nothing else to draw under the canvas:
// the name is kept because the canvas area is the one place that has to decide
// what goes below itself.
func ModelControlsWithUV(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	return ModelControls(ui, gtx, t)
}

// modelControls is the strip under the canvas. A viewer whose controls live
// beside it rather than under it — the object window's viewport, where the
// canvas is a third of the window wide and the strip does not fit — is laid out
// without one.
func ModelControls(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	ModelControlClicks(ui, gtx, t)
	animCount := len(v.Model.Animations)
	// The strip is two rows tall here, so the one-row clamp is applied to
	// each row below rather than to the whole block: applied outside it
	// would cap the pair at the height of one and clip the second.
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(modelRowHeight(gtx),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelPlay, modelPlayLabel(v), animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelPrevAnim, "◀", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), modelClipLabel(v))
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelNextAnim, "▶", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelStepBack, "|◀", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelStepFwd, "▶|", animCount == 0)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelTexture, "Textures", len(v.Materials) == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelSkeleton, "Skeleton", len(v.Model.Bones) == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &t.ModelFX, modelFXLabel(v))
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FxAxesButton(ui, gtx, v) }),
						// The ground plane toggle, the same switch the Object
						// Viewport's side panel carries: disabled when the
						// mod carries no ground texture to show.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &t.ModelGround, modelGroundLabel(v), v.GroundImage == nil)
						}),
						// The zoom the wheel does, as two steps of it, so the
						// distance can be moved by a known amount without a
						// wheel to hand.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &t.ModelZoomOut, modelZoomOutLabel)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &t.ModelZoomIn, modelZoomInLabel)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.ModelReset, "Reset view") }),
					)
				}),
			)
		})
}

// modelControlClicks applies whichever button of the strip was pressed. It is
// the one place the presses are read, so a strip laid out in a different shape —
// the viewport's two narrow rows — does exactly the same as the one under a
// canvas.
func ModelControlClicks(ui *uiState, gtx layout.Context, t *fileTab) {
	v := t.Model
	if t.ModelPlay.Clicked(gtx) {
		if v.Anim == nil {
			SelectAnim(v, 0)
		}
		v.Playing = !v.Playing
		v.LastFrame = time.Time{}
		Invalidate(v)
	}
	if t.ModelPrevAnim.Clicked(gtx) {
		SelectAnim(v, v.AnimIndex-1)
	}
	if t.ModelNextAnim.Clicked(gtx) {
		SelectAnim(v, v.AnimIndex+1)
	}
	if t.ModelStepBack.Clicked(gtx) {
		StepFrame(v, -1)
		PickFrameKeyOnTimeline(ui, v)
	}
	if t.ModelStepFwd.Clicked(gtx) {
		StepFrame(v, 1)
		PickFrameKeyOnTimeline(ui, v)
	}
	if t.ModelReset.Clicked(gtx) {
		ResetCamera(v)
	}
	if t.ModelZoomIn.Clicked(gtx) {
		ZoomBy(v, modelZoomStep)
	}
	if t.ModelZoomOut.Clicked(gtx) {
		ZoomBy(v, 1/modelZoomStep)
	}
	if t.ModelSkeleton.Clicked(gtx) {
		v.ShowSkeleton = !v.ShowSkeleton
		log.Printf("3D preview: skeleton overlay %s", onOff(v.ShowSkeleton))
		Invalidate(v)
	}
	if t.ModelFX.Clicked(gtx) {
		ToggleFX(v)
	}
	if t.ModelGround.Clicked(gtx) {
		v.ShowGround = !v.ShowGround
		log.Printf("3D preview: ground %s", onOff(v.ShowGround))
		Invalidate(v)
	}
	if t.ModelTexture.Clicked(gtx) {
		v.Textured = !v.Textured
		log.Printf("3D preview: textures %s", onOff(v.Textured))
		Invalidate(v)
	}
}

// layoutModelViewer draws the canvas plus the playback controls.
func LayoutModelViewer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	if v == nil || v.Model == nil {
		return centeredPanelLabel(gtx, ui.Th, "No model", unit.Sp(10), uikit.CTextDim)
	}
	if modelMaterialsNeedLoad(t) {
		t.ModelMaterialCh = make(chan modelMaterialResult, 1)
		LoadModelMaterials(ui, v, t.ModelMaterialCh)
	}
	// The canvas is always laid out, even before the first frame arrives: it is
	// the canvas that starts the render, so skipping it here would leave the
	// tab permanently blank.
	if v.SideControls {
		return ModelCanvasWithFxList(ui, gtx, t)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ModelCanvasWithFxList(ui, gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ModelControlsWithUV(ui, gtx, t) }),
	)
}

// modelCanvasWithFxList stacks readouts and diagnostics over the rendered frame.
func ModelCanvasWithFxList(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ModelCanvas(ui, gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ModelFxAttributeDialog(ui, gtx, t.Model) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ModelAnimScrubOverlay(ui, gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ModelFxAxesOverlay(ui, gtx, t) }),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return ModelGizmoOverlay(ui, gtx, t) }),
	)
}

func ModelCanvas(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	area := gtx.Constraints.Max
	if area.X < 8 || area.Y < 8 {
		v.Redraw = true
		return layout.Dimensions{}
	}
	w, h := area.X, area.Y
	if w > 1280 {
		h = int(float64(h) * 1280 / float64(w))
		w = 1280
	}
	// A synthetic layout height — the scroll panel measures its tabs with
	// one — must never reach the renderer: it recreates the render target
	// at that height, the allocation fails, and the hardware path is then
	// treated as broken for the rest of the session.
	if h > 4096 {
		w = int(float64(w) * 4096 / float64(h))
		h = 4096
	}
	if h < 1 {
		h = 1
	}
	SetRenderSize(v, w, h)
	if FxStartSoundLoad(ui, v) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	fxDrawn := false
	if v.ShowFX || v.FxAttrIndex >= 0 || (v.ShowFXAxes && len(v.FxEffects) == 0) {
		if FxStartLoad(ui, v) {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
		if v.ShowFX {
			// The hardware frame draws this frame's particles itself, so the
			// software picture is only built for the path that paints one.
			fxDrawn = FxAdvance(v, gtx.Now, w, h, Options(v, w, h), ModelGpuUnavailable(ui, v) != "")
		} else if len(v.FxEffects) > 0 {
			FxOrigins(v)
		}
		if fxDrawn {
			v.Redraw = true
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
	}

	now := gtx.Now
	if t.Kind == "sgb" && t.SgbView3D && len(v.AnimatedEntities) > 0 {
		if v.SgbAnimLast.IsZero() {
			v.SgbAnimLast = now
		} else {
			dt := now.Sub(v.SgbAnimLast).Seconds()
			v.SgbAnimLast = now
			if dt > 0.1 {
				dt = 0.1
			}
			for i := range v.AnimatedEntities {
				sgb.AdvanceEntityAnimation(&v.AnimatedEntities[i], dt)
			}
		}
		v.Redraw = true
		gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
	} else {
		v.SgbAnimLast = time.Time{}
	}
	if v.Playing && v.Anim != nil {
		if v.LastFrame.IsZero() {
			v.LastFrame = now
		}
		dt := now.Sub(v.LastFrame).Seconds()
		v.LastFrame = now
		if v.AnimPlaybackDuration > 0 {
			v.Time += float32(dt / v.AnimPlaybackDuration)
		} else {
			frames := maxInt(1, v.Anim.NumFrames)
			v.Time += float32(dt * animFramesPerSecond(v.Anim) / float64(frames))
		}
		if v.Time > 1 {
			v.Time = float32(math.Mod(float64(v.Time), 1))
		}
		v.PoseValid = false
		v.Redraw = true
	}
	if v.Redraw {
		v.Redraw = false
		if RenderModelOnGpu(ui, gtx, t, w, h) {
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		} else {
			RequestRender(v, w, h, render.ModelQualityFinal)
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		}
	}
	// A frame finished on a worker goroutine after the last layout ran, so
	// schedule one more frame to actually put it on screen.
	if v.Frame != nil && v.Painted != v.Serial {
		v.Painted = v.Serial
		gtx.Execute(op.InvalidateCmd{})
	}
	if v.Rendering {
		// Keep polling until the background render lands.
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}

	if t.Kind == "sgb" && t.SgbView3D {
		if t.SgbCameraTag == nil {
			t.SgbCameraTag = new(int)
		}
		event.Op(gtx.Ops, t.SgbCameraTag)
		for {
			ev, ok := gtx.Event(key.FocusFilter{Target: t.SgbCameraTag})
			if !ok {
				break
			}
			if focus, ok := ev.(key.FocusEvent); ok && !focus.Focus {
				t.SgbCameraKeys = [4]bool{}
				t.SgbCameraVelocity = [2]float64{}
				t.SgbCameraLastMove = time.Time{}
			}
		}
		arrowKeys := [...]key.Name{key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow}
		allModifiers := key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt | key.ModSuper
		moved := false
		for i, name := range arrowKeys {
			for {
				ev, ok := gtx.Event(key.Filter{Focus: t.SgbCameraTag, Name: name, Optional: allModifiers})
				if !ok {
					break
				}
				ke, ok := ev.(key.Event)
				if !ok {
					continue
				}
				t.SgbCameraKeys[i] = ke.State == key.Press && ke.Modifiers == 0
			}
		}
		forward, strafe := 0.0, 0.0
		if t.SgbCameraKeys[0] {
			forward++
		}
		if t.SgbCameraKeys[1] {
			forward--
		}
		if t.SgbCameraKeys[2] {
			strafe++ // left/right intentionally follow screen-facing convention
		}
		if t.SgbCameraKeys[3] {
			strafe--
		}
		dt := 1.0 / 60.0
		if !t.SgbCameraLastMove.IsZero() && !gtx.Now.IsZero() {
			dt = gtx.Now.Sub(t.SgbCameraLastMove).Seconds()
		}
		// Bound catch-up after a pause, then ease velocity toward the held
		// direction or back to rest after release for acceleration/braking.
		t.SgbCameraLastMove = gtx.Now
		dt = math.Max(0, math.Min(dt, 0.05))
		t.SgbCameraVelocity[0] = sgbApproachCameraVelocity(t.SgbCameraVelocity[0], forward, dt)
		t.SgbCameraVelocity[1] = sgbApproachCameraVelocity(t.SgbCameraVelocity[1], strafe, dt)
		moved = SgbMoveCamera(ui, t, t.SgbCameraVelocity[0], t.SgbCameraVelocity[1], dt)
		if moved || t.SgbCameraVelocity != [2]float64{} {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
		}
	}

	rect := clip.Rect{Max: area}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.Tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  &v.Tag,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: math.MinInt, Max: math.MaxInt},
			ScrollY: pointer.ScrollRange{Min: math.MinInt, Max: math.MaxInt},
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons.Contain(pointer.ButtonPrimary) && t.Kind == "sgb" && t.SgbView3D {
				if scrubHit(v.ScrubRect, e.Position) {
					t.SgbObjectDragging, t.SgbObjectRotating = false, false
					v.Seeking = true
					v.Dragging = false
					ScrubSeek(v, e.Position.X, true)
					continue
				}
				gtx.Execute(key.FocusCmd{Tag: t.SgbCameraTag})
				v.Dragging = sgbCameraDragRequested(e.Modifiers | heldModifiers())
				v.Panning = false
				t.SgbSelecting = !v.Dragging && t.SgbObjectSelected == ""
				t.SgbSelectionDrag = false
				t.SgbSelectionAnchor = sgbCanvasToFramePoint(area, w, h, e.Position)
				t.SgbSelectionCurrent = t.SgbSelectionAnchor
				if !v.Dragging && len(t.SgbSelectedEntities) > 0 {
					camera := engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}
					if id, hit := sgbPickEntity(ModelData(v), t.SgbEntityMeshIDs, camera, float64(t.SgbSelectionAnchor.X), float64(t.SgbSelectionAnchor.Y)); hit && t.SgbSelectedEntities[id] {
						if (e.Modifiers | heldModifiers()).Contain(key.ModShift) {
							t.SgbObjectRotating = true
							t.SgbRotateLastX = e.Position.X
							t.SgbObjectMoved = false
							t.SgbSelecting = false
							continue
						}
						if point, onTerrain := sgbPickTerrainPoint(ModelData(v), camera, float64(t.SgbSelectionAnchor.X), float64(t.SgbSelectionAnchor.Y)); onTerrain {
							t.SgbObjectDragging = true
							t.SgbDragHasPoint = true
							t.SgbDragLast = point
							t.SgbObjectMoved = false
							t.SgbSelecting = false
							continue
						}
					}
				}
				if t.SgbObjectSelected != "" && !v.Dragging {
					if point, hit := sgbPickTerrainPoint(ModelData(v), engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}, float64(t.SgbSelectionAnchor.X), float64(t.SgbSelectionAnchor.Y)); hit {
						PlaceSgbObject(ui, t, point)
					} else {
						ui.StatusText = "Object placement — click on the terrain surface"
					}
					continue
				}
				if v.Dragging {
					v.LastX, v.LastY = e.Position.X, e.Position.Y
				}
				continue
			}
			if e.Buttons.Contain(pointer.ButtonSecondary) {
				if t.Kind == "sgb" && t.SgbView3D {
					t.SgbSelecting = false
				}
				v.Panning = true
				v.Dragging = false
				v.LastX, v.LastY = e.Position.X, e.Position.Y
				continue
			}
			if !e.Buttons.Contain(pointer.ButtonPrimary) {
				continue
			}
			if scrubHit(v.ScrubRect, e.Position) {
				v.Seeking = true
				v.Dragging = false
				picked := ScrubSeek(v, e.Position.X, t != ui.UnitModel || ui.UnitListMode == 2)
				v.KeyDrag = picked && t == ui.UnitModel
				v.KeyDragMoved = false
				LogInfo("3D preview: strip press pos=(%.1f, %.1f) keymark=%v", e.Position.X, e.Position.Y, v.KeyDrag)
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D {
				continue
			}
			v.Dragging = true
			v.LastX, v.LastY = e.Position.X, e.Position.Y
		case pointer.Drag:
			if t.Kind == "sgb" && t.SgbView3D && t.SgbObjectRotating && e.Buttons.Contain(pointer.ButtonPrimary) {
				if !(e.Modifiers | heldModifiers()).Contain(key.ModShift) {
					t.SgbObjectRotating = false
					t.SgbObjectMoved = false
					continue
				}
				dx := float64(e.Position.X - t.SgbRotateLastX)
				t.SgbRotateLastX = e.Position.X
				if rotated := sgbRotateEntities(t.SgbMap, ModelData(v), t.SgbEntityMeshIDs, v.AnimatedEntities, t.SgbSelectedEntities, -dx*0.01); rotated > 0 {
					GeometryChangedModelGpu(ui, v.Model)
					t.SgbObjectMoved = true
					t.SgbDirty = true
					Invalidate(v)
					gtx.Execute(op.InvalidateCmd{})
					ui.StatusText = fmt.Sprintf("Rotated %d selected map object(s) around vertical axis", rotated)
				}
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D && t.SgbObjectDragging && e.Buttons.Contain(pointer.ButtonPrimary) {
				pixel := sgbCanvasToFramePoint(area, w, h, e.Position)
				camera := engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}
				if point, hit := sgbPickTerrainPoint(ModelData(v), camera, float64(pixel.X), float64(pixel.Y)); hit {
					if t.SgbDragHasPoint {
						dx, dy := point.X-t.SgbDragLast.X, point.Y-t.SgbDragLast.Y
						var moved int
						t.Model.AnimatedEntities, moved = sgbMoveEntitiesOnSurface(t.SgbMap, ModelData(v), t.SgbEntityMeshIDs, v.AnimatedEntities, t.SgbSelectedEntities, dx, dy)
						if moved > 0 {
							GeometryChangedModelGpu(ui, v.Model)
							t.SgbObjectMoved = true
							t.SgbDirty = true
							Invalidate(v)
							gtx.Execute(op.InvalidateCmd{})
							ui.StatusText = fmt.Sprintf("Moved %d selected map object(s) along the terrain", moved)
						}
					}
					t.SgbDragLast, t.SgbDragHasPoint = point, true
				} else {
					t.SgbDragHasPoint = false
				}
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D && v.Dragging && e.Buttons.Contain(pointer.ButtonPrimary) {
				dx := float64(e.Position.X - v.LastX)
				dy := float64(e.Position.Y - v.LastY)
				v.LastX, v.LastY = e.Position.X, e.Position.Y
				v.Yaw -= dx * 0.01
				v.Pitch = engine.ClampFloat64(v.Pitch+dy*0.01, -1.45, 1.45)
				v.Redraw = true
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D && t.SgbSelecting {
				t.SgbSelectionCurrent = sgbCanvasToFramePoint(area, w, h, e.Position)
				dx := t.SgbSelectionCurrent.X - t.SgbSelectionAnchor.X
				dy := t.SgbSelectionCurrent.Y - t.SgbSelectionAnchor.Y
				if dx*dx+dy*dy > 16 {
					t.SgbSelectionDrag = true
				}
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if v.Panning {
				dx := float64(e.Position.X - v.LastX)
				dy := float64(e.Position.Y - v.LastY)
				v.LastX, v.LastY = e.Position.X, e.Position.Y
				PanCamera(v, dx, dy, h)
				continue
			}
			if v.Seeking {
				if v.KeyDrag {
					if MovePickedBoneKeyToX(v, e.Position.X) {
						ui.UnitModelWhmDirty = true
						v.KeyDragMoved = true
					}
					continue
				}
				ScrubSeek(v, e.Position.X, t != ui.UnitModel || ui.UnitListMode == 2)
				continue
			}
			if !v.Dragging {
				continue
			}
			dx := float64(e.Position.X - v.LastX)
			dy := float64(e.Position.Y - v.LastY)
			v.LastX, v.LastY = e.Position.X, e.Position.Y
			v.Yaw -= dx * 0.01
			v.Pitch = engine.ClampFloat64(v.Pitch+dy*0.01, -1.45, 1.45)
			v.Redraw = true
		case pointer.Scroll:
			ZoomBy(v, modelZoomByDelta(e.Scroll.Y))
			v.Redraw = true
		case pointer.Release, pointer.Cancel:
			if t.Kind == "sgb" && t.SgbView3D && t.SgbObjectRotating {
				t.SgbObjectRotating = false
				if e.Kind == pointer.Release && t.SgbObjectMoved {
					Invalidate(v)
				}
				t.SgbObjectMoved = false
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D && t.SgbObjectDragging {
				t.SgbObjectDragging = false
				t.SgbDragHasPoint = false
				t.SgbSelecting, t.SgbSelectionDrag = false, false
				if e.Kind == pointer.Release && t.SgbObjectMoved {
					Invalidate(v)
				}
				t.SgbObjectMoved = false
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D && v.Dragging {
				v.Dragging = false
				v.Redraw = e.Kind == pointer.Release
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if t.Kind == "sgb" && t.SgbView3D && t.SgbSelecting {
				t.SgbSelectionCurrent = sgbCanvasToFramePoint(area, w, h, e.Position)
				if t.SgbSelectedEntities == nil {
					t.SgbSelectedEntities = make(map[int]bool)
				}
				if e.Kind == pointer.Release {
					if t.SgbSelectionDrag {
						opts := Options(v, w, h)
						selection := sgbRectangleEntities(ModelData(v), t.SgbEntityMeshIDs, render.BuildViewMatrix(opts), w, h, sgbSelectionRect(t.SgbSelectionAnchor, t.SgbSelectionCurrent))
						t.SgbSelectedEntities = selection
					} else if id, hit := sgbPickEntity(ModelData(v), t.SgbEntityMeshIDs, engine.Camera{Width: w, Height: h, Yaw: v.Yaw, Pitch: v.Pitch, Distance: v.Distance, Target: v.Center, Fov: v.FOV}, float64(t.SgbSelectionCurrent.X), float64(t.SgbSelectionCurrent.Y)); hit {
						t.SgbSelectedEntities = map[int]bool{id: true}
					} else {
						clear(t.SgbSelectedEntities)
					}
					Invalidate(v)
				}
				t.SgbSelecting, t.SgbSelectionDrag = false, false
				gtx.Execute(op.InvalidateCmd{})
				continue
			}
			if v.KeyDrag && v.KeyDragMoved {
				if f, ok := BoneKeyFrameAt(v); ok {
					LogInfo("3D preview: bone key drag ended on frame %d", f+1)
					ui.StatusText = fmt.Sprintf("Moved bone key to frame %d", f+1)
				}
			}
			v.KeyDrag, v.KeyDragMoved = false, false
			v.Dragging, v.Panning, v.Seeking = false, false, false
		}
	}

	if v.GpuFrame != nil {
		if v.Dragging || v.Panning || (t.Kind == "sgb" && (t.SgbCameraKeys[0] || t.SgbCameraKeys[1] || t.SgbCameraKeys[2] || t.SgbCameraKeys[3] || t.SgbCameraVelocity != [2]float64{})) || (v.Playing && v.Anim != nil) || (t.Kind == "sgb" && t.SgbView3D && len(v.AnimatedEntities) > 0) {
			// Keep the frame refreshing while something is moving.
			gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
		}
		if os.Getenv("DOW_NO_GPU_PAINT") != "" {
			// Diagnostic: keep the raw pass running but leave the frame out of
			// the op stream, which separates a bad render from a bad hand over.
			if modelGPUPaintSkipLogged.IsZero() {
				modelGPUPaintSkipLogged = time.Now()
				log.Printf("3D preview: GPU paint skipped (DOW_NO_GPU_PAINT)")
			}
			fillRect(gtx, rgb(0x1C1E24), area)
		} else {
			v.GpuFrame.Paint(gtx, image.Rectangle{Max: area})
		}
		// The picked marker is painted in the frame's own transform, so it
		// lands on the model exactly where the frame put it.
		v.GpuFrame.Overlay(gtx, image.Rectangle{Max: area}, func(gtx layout.Context) {
			PaintMarkerOverlay(v, gtx, w, h)
		})
		if t.Kind == "sgb" && t.SgbView3D {
			paintSgbSelectionMarquee(gtx, t, image.Rectangle{Max: area})
		}
		rect.Pop()
		return layout.Dimensions{Size: area}
	}
	img := v.Frame
	if img == nil {
		if modelRenderingLogged.IsZero() || time.Since(modelRenderingLogged) >= time.Second {
			modelRenderingLogged = time.Now()
			log.Printf("3D preview: no frame on screen (skeleton=%v loading=%v)", v.ShowSkeleton, v.Loading)
		}
		gtx.Execute(op.InvalidateCmd{At: now.Add(32 * time.Millisecond)})
		rect.Pop()
		fillRect(gtx, rgb(0x1C1E24), area)
		return layout.Stack{}.Layout(gtx,
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				fillRect(gtx, rgb(0x1C1E24), area)
				return layout.Dimensions{Size: area}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				msg := "Rendering…"
				if v.Loading {
					msg = "Loading textures…"
				}
				l := material.Label(ui.Th, unit.Sp(10), msg)
				l.Color = uikit.CTextDim
				return layout.Center.Layout(gtx, l.Layout)
			}),
		)
	}
	if v.Dragging || v.Panning || (t.Kind == "sgb" && (t.SgbCameraKeys[0] || t.SgbCameraKeys[1] || t.SgbCameraKeys[2] || t.SgbCameraKeys[3] || t.SgbCameraVelocity != [2]float64{})) || (v.Playing && v.Anim != nil) || (t.Kind == "sgb" && t.SgbView3D && len(v.AnimatedEntities) > 0) {
		gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
	}
	b := img.Bounds()
	scale := float32(area.X) / float32(b.Dx())
	if s := float32(area.Y) / float32(b.Dy()); s < scale {
		scale = s
	}
	dx := (float32(area.X) - float32(b.Dx())*scale) / 2
	dy := (float32(area.Y) - float32(b.Dy())*scale) / 2
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Pt(dx, dy), f32.Pt(scale, scale))).Push(gtx.Ops)
	paint.NewImageOp(img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	if fxDrawn && v.FxOverlay != nil {
		paint.NewImageOp(v.FxOverlay).Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
	}
	PaintMarkerOverlay(v, gtx, w, h)
	PaintFXAttributeMarkers(v, gtx, w, h)
	if t.Kind == "sgb" && t.SgbView3D {
		paintSgbSelection(gtx, t, v, w, h, true)
	}
	tr.Pop()
	rect.Pop()
	return layout.Dimensions{Size: area}
}
