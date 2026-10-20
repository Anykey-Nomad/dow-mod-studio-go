package main

import (
	viewer "dow-mod-studio/internal/3d_viewer"
	"fmt"
	"log"
	"math"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// modelControlsWithUV is the strip under a .whm canvas. The mapping controls
// are a dialog of their own, so there is nothing else to draw under the canvas:
// the name is kept because the canvas area is the one place that has to decide
// what goes below itself.
func (ui *uiState) modelControlsWithUV(gtx layout.Context, t *fileTab) layout.Dimensions {
	return ui.modelControls(gtx, t)
}

// modelControls is the strip under the canvas. A viewer whose controls live
// beside it rather than under it — the object window's viewport, where the
// canvas is a third of the window wide and the strip does not fit — is laid out
// without one.
func (ui *uiState) modelControls(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	ui.modelControlClicks(gtx, t)
	animCount := len(v.model.Animations)
	// The strip is two rows tall here, so the one-row clamp is applied to
	// each row below rather than to the whole block: applied outside it
	// would cap the pair at the height of one and clip the second.
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(modelRowHeight(gtx),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelPlay, modelPlayLabel(v), animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelPrevAnim, "◀", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), modelClipLabel(v))
							l.Color = cTextDim
							l.MaxLines = 1
							return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelNextAnim, "▶", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelStepBack, "|◀", animCount == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelStepFwd, "▶|", animCount == 0)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelTexture, "Textures", len(v.materials) == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelSkeleton, "Skeleton", len(v.model.Bones) == 0)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelFX, modelFXLabel(v))
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAxesButton(gtx, v) }),
						// The ground plane toggle, the same switch the Object
						// Viewport's side panel carries: disabled when the
						// mod carries no ground texture to show.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &t.modelGround, modelGroundLabel(v), v.groundImage == nil)
						}),
						// The zoom the wheel does, as two steps of it, so the
						// distance can be moved by a known amount without a
						// wheel to hand.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelZoomOut, modelZoomOutLabel)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelZoomIn, modelZoomInLabel)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.modelReset, "Reset view") }),
					)
				}),
			)
		})
}

// modelInfoTable reports what the 3D preview is showing, which makes it obvious
// whether a blank canvas is a missing texture or a parse problem.
func (ui *uiState) modelInfoTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.model == nil || t.model.model == nil {
		return layout.Dimensions{}
	}
	v := t.model
	tris := 0
	for _, mesh := range v.model.Meshes {
		tris += len(mesh.Indices) / 3
	}
	frame := "—"
	if v.frame != nil {
		b := v.frame.Bounds()
		frame = fmt.Sprintf("%d × %d", b.Dx(), b.Dy())
	}
	state := "idle"
	switch {
	case v.loading:
		state = "loading textures"
	case v.rendering:
		state = "rendering"
	case v.frame != nil:
		state = "ready"
	}
	// Several meshes usually share a map, so the count is over distinct
	// materials. Comparing against the mesh count made a model with four
	// textures look like it was missing twenty three of them.
	unique := map[string]bool{}
	for _, m := range v.model.Meshes {
		if m.Material != "" {
			unique[m.Material] = true
		}
	}
	textures := fmt.Sprintf("%d/%d", len(v.materials), len(unique))
	if v.missing > 0 {
		textures += fmt.Sprintf(" (%d missing)", v.missing)
	}
	anim := "—"
	if v.anim != nil {
		anim = v.anim.Name
	}
	rows := [][2]string{
		{"State", state},
		{"Frame", frame},
		{"Triangles", fmt.Sprintf("%d", tris)},
		{"Animation", anim},
		{"Textures", textures},
	}
	// The camera only gets a row when one of the model's own is in use, since
	// an orbit camera has no name to report.
	if v.ActiveCamera != "" {
		rows = append(rows, [2]string{"Camera", v.ActiveCamera})
	}
	children := make([]layout.FlexChild, 0, len(rows))
	for i, row := range rows {
		i, row := i, row
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, row[0], row[1], i%2 == 0)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// modelMaterialDiagnosticsTable lists each mesh's material reference, source,
// and decoded image encoding. Repeated materials remain visible per mesh so
// the reader can identify which geometry uses a missing or unexpected map.
func (ui *uiState) modelMaterialDiagnosticsTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.model == nil || t.model.model == nil {
		return layout.Dimensions{}
	}
	v := t.model
	if !v.materialsLoaded && !v.loading {
		return centeredPanelLabel(gtx, ui.th, "Material lookup has not run", unit.Sp(10), cTextDim)
	}
	if v.loading && len(v.materialDiagnostics) == 0 {
		return centeredPanelLabel(gtx, ui.th, "Resolving materials…", unit.Sp(10), cTextDim)
	}
	rows := make([]layout.FlexChild, 0, len(v.model.Meshes)*3)
	for meshIndex, mesh := range v.model.Meshes {
		if mesh.Material == "" {
			continue
		}
		diag, ok := v.materialDiagnostics[mesh.Material]
		if !ok {
			diag = modelMaterialDiagnostic{Format: "—", Status: "not resolved"}
		}
		meshLabel := mesh.Name
		if meshLabel == "" {
			meshLabel = fmt.Sprintf("Mesh %d", meshIndex+1)
		}
		rows = append(rows,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.tableRow2WithTooltip(gtx, meshLabel, mesh.Material, meshIndex%2 == 0, mesh.Material)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.tableRow2WithTooltip(gtx, "Format", diagnosticValue(diag.Format), meshIndex%2 != 0, diag.Status)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				path := diag.ResolvedPath
				if path == "" {
					path = diag.Status
				}
				return ui.tableRow2WithTooltip(gtx, "Source", path, meshIndex%2 == 0, diag.ResolvedPath)
			}),
		)
	}
	if len(rows) == 0 {
		return centeredPanelLabel(gtx, ui.th, "No meshes reference a material", unit.Sp(10), cTextDim)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func diagnosticValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// modelPlayLabel is what the play button says: the action it will do.
func modelPlayLabel(v *modelViewer) string {
	if v.playing {
		return "Pause"
	}
	return "Play"
}

// modelClipLabel is the name of the clip on show and where it is in the list.
func modelClipLabel(v *modelViewer) string {
	count := len(v.model.Animations)
	if count == 0 {
		return "no animations"
	}
	name := "—"
	if v.anim != nil {
		name = v.anim.Name
	}
	return fmt.Sprintf("%s  [%d/%d]", name, v.animIndex+1, count)
}

// onOff renders a boolean for a log line.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// toggleSound turns the model's own audio on and off. Turning it off stops what
// is playing at once, because a sound that keeps going after it was turned off
// is the one case of this that is actually annoying.
func (v *modelViewer) toggleSound() {
	if v == nil {
		return
	}
	if v.fxSoundOn {
		v.fxSoundOn = false
		if v.fxSound != nil {
			v.fxSound.StopAll()
		}
		log.Printf("3D preview: sound off")
		return
	}
	v.fxSoundOn = true
	log.Printf("3D preview: sound on")
}

// modelGroundLabel reports whether the unit viewport's ground plane is shown.
func modelGroundLabel(v *modelViewer) string {
	if v == nil || v.groundImage == nil {
		return "no ground"
	}
	if v.showGround {
		return "Ground on"
	}
	return "Ground off"
}

// modelSFXLabel is what the sound button reads: whether the model has sounds
// at all, and whether they are playing.
// modelZoomStep is what one step of the zoom changes the camera distance by:
// a notch of the wheel, or a press of one of the two zoom buttons.
//
// It used to be a two percent step raised to the whole scroll amount, and the
// scroll amount for one notch is wheelNotchDelta, so a click multiplied the
// distance by 1.02^120 — a factor of ten. The model went from framed to inside
// out in one click and back in the next, with nothing in between worth looking
// at. One step is now one readable move, and the whole range between the
// closest and furthest the viewer allows is about a dozen steps either way.
const modelZoomStep = 1.15

// zoomBy multiplies the camera distance, keeping it inside the range the viewer
// allows. A factor above one moves the camera away from the model.
func (v *modelViewer) zoomBy(factor float64) {
	if v == nil || factor <= 0 || factor == 1 {
		return
	}
	v.CameraState = viewer.ZoomBy(v.CameraState, factor)
	v.invalidate()
}

// modelZoomByDelta turns a scroll amount into the factor it costs the camera.
// The amount is in notches rather than pixels, so a Windows wheel and a
// trackpad — which reports the same gesture in smaller pieces that add up —
// move the camera by the same amount for the same turn of the wheel.
func modelZoomByDelta(delta float32) float64 {
	return math.Pow(modelZoomStep, -float64(delta)/wheelNotchDelta)
}
