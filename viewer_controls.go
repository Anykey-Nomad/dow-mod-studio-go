package main

import (
	viewer "dow-mod-studio/internal/3d_viewer"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
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
						}), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelFX, modelFXLabel(v))
						}),
						// Uniform FX geometry scale; one is model-space size.
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelFXScaleOut, "FX v")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), modelFXScaleLabel(v))
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelFXScaleIn, "FX ^")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &t.modelFXScaleRst, "FX 1")
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
				// The headings the effects point along, dialled in
				// against the picture. See fxAngleRow.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.fxAngleRow(gtx, t)
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
func modelSFXLabel(v *modelViewer) string {
	if v == nil || v.fxSound == nil {
		return "no sound"
	}
	n := len(v.fxSound.SoundNames())
	if v.fxSoundOn {
		return fmt.Sprintf("SFX %d", n)
	}
	return "SFX muted"
}

// fxAngleEditors is the three dial-in fields in the order the rest of this
// file names them — pitch, yaw, roll — so the sync below can walk all three
// without saying it three times.
func fxAngleEditors(t *fileTab) [3]*widget.Editor {
	return [3]*widget.Editor{&t.modelFXAngleX, &t.modelFXAngleY, &t.modelFXAngleZ}
}

// fxAnglesPrepare gives the three editors their starting text once. A widget
// editor has no text until it is given some, and setting it every frame would
// move the caret to the end of whatever was being typed.
func fxAnglesPrepare(t *fileTab) {
	if t == nil || t.fxAnglesReady {
		return
	}
	t.fxAnglesReady = true
	for _, ed := range fxAngleEditors(t) {
		ed.SingleLine = true
		ed.Submit = true
		ed.SetText("0")
	}
}

// fxAnglesRead pulls whatever has been typed into the three fields into the
// viewer, in degrees. A field holding something that is not a number is left
// alone rather than read as zero, so a heading that was just dialled in does
// not jump back to the model's own the instant the value is selected for
// editing, or while a "-" is halfway through being typed.
func (ui *uiState) fxAnglesRead(gtx layout.Context, t *fileTab, v *modelViewer) {
	fxAnglesPrepare(t)
	if t == nil || v == nil {
		return
	}
	eds := fxAngleEditors(t)
	for _, ed := range eds {
		for {
			if _, ok := ed.Update(gtx); !ok {
				break
			}
		}
	}
	// The viewer's own values are the starting point, so a field that
	// cannot be read leaves its angle where it was. Starting from zero
	// instead would throw a heading away the moment the field is selected
	// for editing, or while a single "-" is halfway through being typed.
	got := [3]float64{v.fxRotX, v.fxRotY, v.fxRotZ}
	for i, ed := range eds {
		n, err := strconv.ParseFloat(strings.TrimSpace(ed.Text()), 64)
		if err != nil {
			continue
		}
		got[i] = n
	}
	if got[0] == v.fxRotX && got[1] == v.fxRotY && got[2] == v.fxRotZ {
		return
	}
	v.fxRotX, v.fxRotY, v.fxRotZ = got[0], got[1], got[2]
	v.invalidate()
	log.Printf("3D preview: FX rotation X %.1f, Y %.1f, Z %.1f", v.fxRotX, v.fxRotY, v.fxRotZ)
}

// fxAnglesReset puts every angle back to zero, in the viewer and in the text of
// all three fields. Only the viewer half would do: a field still reading the
// value it was reset away from would write it straight back on the next frame.
func (ui *uiState) fxAnglesReset(t *fileTab, v *modelViewer) {
	if t == nil || v == nil {
		return
	}
	v.fxRotX, v.fxRotY, v.fxRotZ = 0, 0, 0
	for _, ed := range fxAngleEditors(t) {
		ed.SetText("0")
	}
	v.invalidate()
	log.Printf("3D preview: FX rotation reset to zero")
}

// fxAngleRow is the strip of three fields the effects' headings are dialled in
// with, plus the way back to zero. It is a row of its own rather than more
// controls on the FX row: three editors with a name beside each would not fit
// across the unit panel beside the toggles already there.
func (ui *uiState) fxAngleRow(gtx layout.Context, t *fileTab) layout.Dimensions {
	fxAnglesPrepare(t)
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), "FX rot")
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		ui.fxAngleField("X", &t.modelFXAngleX),
		ui.fxAngleField("Y", &t.modelFXAngleY),
		ui.fxAngleField("Z", &t.modelFXAngleZ),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &t.modelFXAngleRst, "0°")
			})
		}),
		// The model's own sound, which the clip plays at the moments its
		// timeline names.
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			v := t.model
			disabled := v == nil || v.fxSound == nil
			return ui.smallButtonDisabled(gtx, &t.modelSFX, modelSFXLabel(v), disabled)
		}),
		layout.Flexed(1, rowGap()),
	)
}

// fxAngleField is a name and the editor that takes the number beside it. The
// box is a fixed width rather than the width of whatever is in it: an editor
// sized by its own text is both too small a target to click into when it holds
// one digit and a control that shifts sideways under the pointer the moment a
// second digit is typed.
func (ui *uiState) fxAngleField(name string, ed *widget.Editor) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), name)
				l.Color = cTextDim
				l.MaxLines = 1
				return layout.Inset{Right: unit.Dp(3)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// A heading is dialled in and then left alone for a
				// while, so the box has to say where to click: bare
				// editor text on the panel background reads as a
				// label, not something to click.
				g := gtx
				w := gtx.Dp(unit.Dp(46))
				if gtx.Constraints.Max.X > 0 && w > gtx.Constraints.Max.X {
					w = gtx.Constraints.Max.X
				}
				g.Constraints.Min.X = w
				g.Constraints.Max.X = w
				return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(3), Right: unit.Dp(3)}.Layout(g,
					func(gtx layout.Context) layout.Dimensions {
						e := material.Editor(ui.th, ed, "")
						e.TextSize = unit.Sp(10)
						e.HintColor = cTextDim
						return e.Layout(gtx)
					})
			}),
		)
	})
}

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
