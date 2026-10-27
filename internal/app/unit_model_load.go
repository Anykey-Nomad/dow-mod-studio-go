package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/whm"
	"fmt"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"image"
	"time"
)

// ensureUnitModel starts loading the current unit's model if it is not already
// loaded or on its way. It is called from the Viewport section's layout, so
// browsing units never pays for a model that is not being looked at.
//
// While the read is on a worker the section has no model to animate, and the
// canvas below has nothing to ask a redraw for — the window repaints on input
// and on the invalidates a drawn model schedules, neither of which exist here.
// The poll that would pick the result up runs at the top of the frame loop, so
// without an invalidate of its own the finished answer waits in its channel
// until the mouse moves. That is what made a switch on this section look many
// seconds slower than the same switch done from another one and read afterwards:
// the read cost the same, but one of them was waiting for input to show it.
func EnsureUnitModel(ui *uiState, gtx layout.Context) {
	if ui.ModTree == nil {
		return
	}
	src, ok := newUnitModelSource(ui.ModTree, CurrentUnitDetails(ui))
	if !ok {
		ResetUnitModel(ui)
		return
	}
	if src.Unit != ui.UnitModelFor || (ui.UnitModel == nil && ui.UnitModelCh == nil && ui.UnitModelErr == "") {
		StartUnitModel(ui, src)
	}
	if ui.UnitModelCh != nil {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(30 * time.Millisecond)})
	}
}

// startUnitModel kicks the read and the parse off on a worker. Both are worth
// moving off the UI goroutine: a unit model is megabytes, and the necron warrior's
// alone is five of them.
func StartUnitModel(ui *uiState, src unitModelSource) {
	ResetUnitModel(ui)
	ui.UnitModelFor = src.Unit
	tree := ui.ModTree
	ch := make(chan unitModelResult, 1)
	ui.UnitModelCh = ch
	ui.UnitModelStart = time.Now()
	go func() {
		res := unitModelResult{Unit: src.Unit}
		workStart := time.Now()
		res.Path = unitModelPath(tree, src)
		if res.Path == "" {
			res.Err = errNoUnitModel
			ch <- res
			return
		}
		data, err := tree.ReadFile(res.Path)
		if err != nil {
			res.Err = err
			ch <- res
			return
		}
		model, err := ParseWhmModel(data)
		if err != nil {
			res.Err = err
			ch <- res
			return
		}
		res.Model = model
		res.GroundImage = loadWhmGroundImage(tree)
		res.Work = time.Since(workStart)
		ch <- res
	}()
}

func PollUnitModel(ui *uiState) {
	if ui.UnitModelCh == nil {
		return
	}
	select {
	case res := <-ui.UnitModelCh:
		ui.UnitModelCh = nil
		if res.Unit != ui.UnitModelFor {
			// The reader has moved on to another unit while this one loaded.
			return
		}
		if res.Err != nil {
			ui.UnitModelErr = res.Err.Error()
			return
		}
		// The gap between what the worker spent and what the reader waited is
		// the answer arriving without a frame to be picked up in, which is the
		// whole of the difference between a switch that looked slow and one
		// that did not.
		total := time.Since(ui.UnitModelStart)
		LogInfo("3D preview: model of %q on screen after %v (worker %v, waiting for a frame %v)",
			res.Path, total, res.Work, total-res.Work)
		tab := &fileTab{Path: res.Path, Kind: "whm"}
		tab.Model = newObjectModelViewer(res.Model, res.Path, res.GroundImage)
		selectObjectAction(tab.Model, ui.ModTree, res.Path, "idle")
		// The canvas is a third of the window wide here, so the playback strip
		// is drawn in the panel beside it rather than under it.
		tab.Model.SideControls = true
		ui.UnitModel = tab
		ui.UnitModelPath = res.Path
		// A freshly read model starts clean, and its keys rebind the typed
		// fields: the previous model's edits are not its edits.
		ui.UnitModelWhmDirty = false
		ui.UnitBoneKeyBound = ""
		ui.UnitBoneKeyErr = ""
	default:
	}
}

// saveUnitMeshMaterialDialog writes the fields into the .whm: the references
// that differ are spliced in, the whole file goes to the overlay, and the
// model on show is set to the same so the view agrees with what was written.
// The dialog stays up — a modder correcting a path usually tries another —
// and Cancel after a save reverts to nothing, because the file now says this.
func SaveUnitMeshMaterialDialog(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		ui.UnitMeshMatErr = "no model is loaded"
		return
	}
	if ui.ModTree == nil || ui.UnitModelPath == "" {
		ui.UnitMeshMatErr = "no module is loaded"
		return
	}
	refs := UnitMeshMatFieldText(ui)
	data, err := ui.ModTree.ReadFile(ui.UnitModelPath)
	if err != nil {
		ui.UnitMeshMatErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchMeshMaterialPaths(data, ui.UnitMeshMatName, refs)
	if err != nil {
		ui.UnitMeshMatErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitModelPath, patched)
	if err != nil {
		ui.UnitMeshMatErr = "save failed: " + err.Error()
		return
	}
	applyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, refs)
	ReloadUnitModelMaterials(ui, v)
	Invalidate(v)
	ui.UnitMeshMatErr = ""
	// The file holds these now: Cancel after this point reverts to them.
	ui.UnitMeshMatOrig = refs
	for i := range ui.UnitMeshMatEditors {
		if i < len(refs) {
			ui.UnitMeshMatEditors[i].SetText(refs[i])
		}
	}
	ui.StatusText = fmt.Sprintf("Saved materials → %s", disk)
	LogInfo("mesh material: Save WHM ok: %s (mesh=%q, %d path(s))",
		disk, ui.UnitMeshMatName, len(refs))
	MarkStateDirty(ui)
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
func LayoutUnitMeshMatDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	cancel := ui.UnitMeshMatCancel.Clicked(gtx)
	dim := ui.UnitMeshMatDim.Clicked(gtx)
	if cancel || dim {
		CloseUnitMeshMaterialDialog(ui, true)
	}
	if ui.UnitMeshMatPreview.Clicked(gtx) {
		PreviewUnitMeshMaterialDialog(ui)
	}
	if ui.UnitMeshMatSave.Clicked(gtx) {
		SaveUnitMeshMaterialDialog(ui)
	}
	if !ui.UnitMeshMatOpen {
		return layout.Dimensions{}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return ui.UnitMeshMatDim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
					UnitMeshMatNaturalHeight(ui, gtx, contentW)+gtx.Dp(inset.Top+inset.Bottom),
					gtx.Dp(unit.Dp(150)),
					gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)),
				)
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							// The card takes the press before the dim beneath
							// can have it: the fields rarely fill the card, and
							// a click on what is left used to shut the dialog.
							absorbDialogCardPress(gtx, &ui.UnitMeshMatCardTag)
							return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitMeshMatColumn(ui, gtx, func(gtx layout.Context) layout.Dimensions {
									return PanelScrollView(ui, gtx, &ui.UnitMeshMatScroll, func(gtx layout.Context) layout.Dimensions { return UnitMeshMatFields(ui, gtx) })
								})
							})
						}),
					)
				})
			})
		}),
	)
}
