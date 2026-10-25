package main

import (
	"fmt"
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dow-mod-studio/internal/whm"
)

// The material editor a double press on a mesh row raises: every reference the
// named mesh carries, in an editable field, with the file's own value kept
// beside it. Preview applies the fields to the model on show — the map
// appears behind the dim, which is the fast way to try a corrected path —
// and Save WHM writes them into the file through the .whm material patcher.
//
// The references are collected in the order the mesh list holds them, which is
// the order the file's FOLDMSLC material list holds them, so field i and the
// i-th material the patcher rewrites are the same one.

// meshMaterialRefs returns the named mesh's material references in order. A
// mesh with no materials under that name yields nil, which is what stops the
// dialog opening on a mesh there is nothing to edit.
func meshMaterialRefs(m *WhmModel, name string) []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, mesh := range m.Meshes {
		if strings.EqualFold(mesh.Name, name) {
			out = append(out, mesh.Material)
		}
	}
	return out
}

// applyMeshMaterialRefs writes refs back onto the named mesh's materials, in
// the order meshMaterialRefs collected them. An empty entry leaves that
// material as it was, and the shared-shader path is cleared alongside so the
// renderer keys the texture off the reference it was just given — the shape a
// model whose material label carries the path already has, and the field the
// game reads.
func applyMeshMaterialRefs(m *WhmModel, name string, refs []string) {
	if m == nil {
		return
	}
	i := 0
	for j := range m.Meshes {
		if !strings.EqualFold(m.Meshes[j].Name, name) {
			continue
		}
		if i < len(refs) && refs[i] != "" {
			m.Meshes[j].Material = refs[i]
			m.Meshes[j].MaterialPath = ""
		}
		i++
	}
}

// equalRefs reports whether two reference lists hold the same strings, so a
// Cancel that would change nothing skips the texture reload it would cost.
func equalRefs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unitMeshMatFieldText reads the editors as they stand, trimmed, in material
// order. An empty field means "leave this one as it is", the same the patcher
// takes it to mean.
func (ui *uiState) unitMeshMatFieldText() []string {
	out := make([]string, len(ui.unitMeshMatEditors))
	for i := range ui.unitMeshMatEditors {
		out[i] = strings.TrimSpace(ui.unitMeshMatEditors[i].Text())
	}
	return out
}

// reloadUnitModelMaterials restarts the texture lookup for the unit's viewer
// from the material references it now carries — the same step a fresh load
// takes, so a changed reference is resolved and drawn again.
func (ui *uiState) reloadUnitModelMaterials(v *modelViewer) {
	t := ui.unitModel
	if t == nil || v == nil || v.model == nil || ui.modTree == nil {
		return
	}
	t.modelMaterialCh = make(chan modelMaterialResult, 1)
	ui.loadModelMaterials(v, t.modelMaterialCh)
}

// openUnitMeshMaterialDialog fills one field per material of the named mesh
// with the reference it carries and puts the dialog up. A mesh with no
// materials leaves it down: there is nothing to edit and nothing to save.
func (ui *uiState) openUnitMeshMaterialDialog(name string) {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		return
	}
	refs := meshMaterialRefs(v.model, name)
	if len(refs) == 0 {
		return
	}
	ui.unitMeshMatOpen = true
	ui.unitMeshMatName = name
	ui.unitMeshMatOrig = refs
	ui.unitMeshMatErr = ""
	ui.unitMeshMatScroll = panelScrollState{}
	ui.unitMeshMatEditors = make([]widget.Editor, len(refs))
	for i, ref := range refs {
		ed := widget.Editor{SingleLine: true}
		ed.SetText(ref)
		ui.unitMeshMatEditors[i] = ed
	}
}

// closeUnitMeshMaterialDialog puts the dialog down. With revert it also puts
// the model back on the references the file holds: a preview may have moved it,
// and leaving without saving must not keep the texture of an edit that was
// never written. A close after a save reverts nothing, because the file says
// what the model now says.
func (ui *uiState) closeUnitMeshMaterialDialog(revert bool) {
	if revert {
		v := ui.unitModelViewer()
		if v != nil && v.model != nil && ui.unitMeshMatName != "" {
			cur := meshMaterialRefs(v.model, ui.unitMeshMatName)
			if !equalRefs(cur, ui.unitMeshMatOrig) {
				applyMeshMaterialRefs(v.model, ui.unitMeshMatName, ui.unitMeshMatOrig)
				ui.reloadUnitModelMaterials(v)
				v.invalidate()
			}
		}
	}
	ui.unitMeshMatOpen = false
	ui.unitMeshMatName = ""
	ui.unitMeshMatOrig = nil
	ui.unitMeshMatEditors = nil
	ui.unitMeshMatErr = ""
	ui.unitMeshMatScroll = panelScrollState{}
}

// previewUnitMeshMaterialDialog applies the fields to the model on show and
// re-resolves its textures, so the map the path names appears behind the dim
// without touching the file.
func (ui *uiState) previewUnitMeshMaterialDialog() {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		ui.unitMeshMatErr = "no model is loaded"
		return
	}
	applyMeshMaterialRefs(v.model, ui.unitMeshMatName, ui.unitMeshMatFieldText())
	ui.reloadUnitModelMaterials(v)
	v.invalidate()
	ui.unitMeshMatErr = ""
}

// saveUnitMeshMaterialDialog writes the fields into the .whm: the references
// that differ are spliced in, the whole file goes to the overlay, and the
// model on show is set to the same so the view agrees with what was written.
// The dialog stays up — a modder correcting a path usually tries another —
// and Cancel after a save reverts to nothing, because the file now says this.
func (ui *uiState) saveUnitMeshMaterialDialog() {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		ui.unitMeshMatErr = "no model is loaded"
		return
	}
	if ui.modTree == nil || ui.unitModelPath == "" {
		ui.unitMeshMatErr = "no module is loaded"
		return
	}
	refs := ui.unitMeshMatFieldText()
	data, err := ui.modTree.ReadFile(ui.unitModelPath)
	if err != nil {
		ui.unitMeshMatErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchMeshMaterialPaths(data, ui.unitMeshMatName, refs)
	if err != nil {
		ui.unitMeshMatErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.modTree.WriteOverlayFile(ui.unitModelPath, patched)
	if err != nil {
		ui.unitMeshMatErr = "save failed: " + err.Error()
		return
	}
	applyMeshMaterialRefs(v.model, ui.unitMeshMatName, refs)
	ui.reloadUnitModelMaterials(v)
	v.invalidate()
	ui.unitMeshMatErr = ""
	// The file holds these now: Cancel after this point reverts to them.
	ui.unitMeshMatOrig = refs
	for i := range ui.unitMeshMatEditors {
		if i < len(refs) {
			ui.unitMeshMatEditors[i].SetText(refs[i])
		}
	}
	ui.statusText = fmt.Sprintf("Saved materials → %s", disk)
	LogInfo("mesh material: Save WHM ok: %s (mesh=%q, %d path(s))",
		disk, ui.unitMeshMatName, len(refs))
	ui.markStateDirty()
}

// layoutUnitMeshMatDialog is the dialog itself: a dimmed card over the unit
// body with the mesh's references in editable fields and Preview / Save / Cancel
// beneath. The buttons and the dim were laid out last frame, so a press on any
// of them is read here, ahead of redrawing what they decided — the order the
// Classic OE event dialog takes.
//
// The card is sized to its content rather than to the window, and it takes
// every press on its own area, so clicking the card's whitespace neither
// leaves a dialog that fills the screen nor closes it under the user's hand.
func (ui *uiState) layoutUnitMeshMatDialog(gtx layout.Context) layout.Dimensions {
	cancel := ui.unitMeshMatCancel.Clicked(gtx)
	dim := ui.unitMeshMatDim.Clicked(gtx)
	if cancel || dim {
		ui.closeUnitMeshMaterialDialog(true)
	}
	if ui.unitMeshMatPreview.Clicked(gtx) {
		ui.previewUnitMeshMaterialDialog()
	}
	if ui.unitMeshMatSave.Clicked(gtx) {
		ui.saveUnitMeshMaterialDialog()
	}
	if !ui.unitMeshMatOpen {
		return layout.Dimensions{}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return ui.unitMeshMatDim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				st := clip.Rect{Max: sz}.Push(gtx.Ops)
				paint.ColorOp{Color: rgba(0, 0, 0, 70)}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				st.Pop()
				return layout.Dimensions{Size: sz}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				inset := layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}
				cardW := minInt(gtx.Dp(unit.Dp(720)), gtx.Constraints.Max.X-gtx.Dp(unit.Dp(30)))
				contentW := cardW - gtx.Dp(inset.Left+inset.Right)
				// The card is as tall as what it holds, not as tall as the
				// window. One material was drawing a 540dp card that was nine
				// tenths empty; only when the fields run past the room the
				// window has does the card fill it, and then they scroll.
				cardH := clampInt(
					ui.unitMeshMatNaturalHeight(gtx, contentW)+gtx.Dp(inset.Top+inset.Bottom),
					gtx.Dp(unit.Dp(150)),
					gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)),
				)
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							// The card takes the press before the dim beneath
							// can have it: the fields rarely fill the card, and
							// a click on what is left used to shut the dialog.
							absorbDialogCardPress(gtx, &ui.unitMeshMatCardTag)
							return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return ui.unitMeshMatColumn(gtx, func(gtx layout.Context) layout.Dimensions {
									return ui.panelScrollView(gtx, &ui.unitMeshMatScroll, ui.unitMeshMatFields)
								})
							})
						}),
					)
				})
			})
		}),
	)
}

// unitMeshMatColumn is the card's whole content — title, the rules, the
// fields, the note and the buttons — with the fields handed in, so the same
// body can be laid out for real and laid out again to measure the height the
// card should be.
func (ui *uiState) unitMeshMatColumn(gtx layout.Context, fields layout.Widget) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(ui.unitMeshMatTitle),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return hLine(gtx, cHeaderLine, 1)
		}),
		layout.Flexed(1, fields),
		layout.Rigid(ui.unitMeshMatNote),
		layout.Rigid(ui.unitMeshMatFooter),
	)
}

// unitMeshMatNaturalHeight measures that body at the width the card will give
// it, with input off and the recorded operations thrown away — the height a
// card sized to its content needs, taken the way the scroll panel takes the
// height of its own contents.
func (ui *uiState) unitMeshMatNaturalHeight(gtx layout.Context, contentW int) int {
	if contentW <= 0 {
		return 0
	}
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min = image.Pt(0, 0)
	measureGTX.Constraints.Max = image.Pt(contentW, panelScrollMeasureHeight)
	height := ui.unitMeshMatColumn(measureGTX, ui.unitMeshMatFields).Size.Y
	macro.Stop()
	return height
}

// unitMeshMatTitle names the dialog and the mesh it edits.
func (ui *uiState) unitMeshMatTitle(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(14), "Mesh materials")
			l.Color = cText
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), ui.unitMeshMatName)
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// unitMeshMatFields is the scrollable body: one field per material, with the
// reference the file holds shown dimmed beneath a field that has moved away
// from it.
func (ui *uiState) unitMeshMatFields(gtx layout.Context) layout.Dimensions {
	if len(ui.unitMeshMatEditors) == 0 {
		l := material.Label(ui.th, unit.Sp(9), "This mesh carries no materials.")
		l.Color = cTextDim
		return l.Layout(gtx)
	}
	var rc []layout.FlexChild
	for i := range ui.unitMeshMatEditors {
		i := i
		ed := &ui.unitMeshMatEditors[i]
		rc = append(rc, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), fmt.Sprintf("Material %d", i+1))
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.Editor(ui.th, ed, "texture path").Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// The file's own value, shown only once the field has
					// drifted from it: that is when a reader needs to see
					// what is being replaced.
					if i >= len(ui.unitMeshMatOrig) || strings.TrimSpace(ed.Text()) == ui.unitMeshMatOrig[i] {
						return layout.Dimensions{}
					}
					l := material.Label(ui.th, unit.Sp(9), "file: "+ui.unitMeshMatOrig[i])
					l.Color = cTextDim
					l.MaxLines = 1
					return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hLine(gtx, cBorder, 1)
					})
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rc...)
}

// unitMeshMatNote is the one line under the fields: the reason the dialog
// exists while there is no error, and the save's complaint when there is one.
func (ui *uiState) unitMeshMatNote(gtx layout.Context) layout.Dimensions {
	msg := "Preview draws the map behind this card; Save WHM writes the path into the file."
	col := cTextDim
	if ui.unitMeshMatErr != "" {
		msg = ui.unitMeshMatErr
		col = cProblem
	}
	l := material.Label(ui.th, unit.Sp(9), msg)
	l.Color = col
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
}

// unitMeshMatFooter is Preview, Save WHM and Cancel. Save is disabled while
// there is no file to write to.
func (ui *uiState) unitMeshMatFooter(gtx layout.Context) layout.Dimensions {
	noFile := ui.modTree == nil || ui.unitModelPath == ""
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButton(gtx, &ui.unitMeshMatPreview, "Preview")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &ui.unitMeshMatSave, "Save WHM", noFile)
		}),
		layout.Flexed(1, rowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButton(gtx, &ui.unitMeshMatCancel, "Cancel")
		}),
	)
}
