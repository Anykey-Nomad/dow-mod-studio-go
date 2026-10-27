package app

import (
	"dow-mod-studio/internal/app/uikit"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"path/filepath"
	"strings"
)

// unitBonePoseBar is the pose-editing menu shown above the list while the bone
// list is on show. The keyframe list scrolls with the rest of the panel, so
// the buttons appended to it slid out of reach exactly when a key picked near
// the top of the list needed them; this bar sits outside the scroll area, and
// showing it shortens the list, so the menu stays visible at any scroll
// position.
//
// The picked key's position and rotation are also typed fields: Enter in a
// field applies its triple to the key, and Save WHM writes the edited keys
// back into the .whm. Both groups show the key a typed apply would write —
// the picked key for its own kind, the nearest key of the other kind to the
// frame on show for the other.
func UnitBonePoseBar(ui *uiState, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	if ui.UnitListMode != 2 || v == nil {
		// If the bar is not laid out, its buttons cannot see a press — while
		// the last drawn frame still shows them. The log says when this
		// becomes the case, which turns a dead click into a visible cause.
		if state := fmt.Sprintf("mode=%d viewer=%v", ui.UnitListMode, v != nil); state != ui.UnitBonePoseBarLog {
			LogInfo("bone: pose bar is not laid out (%s)", state)
			ui.UnitBonePoseBarLog = state
		}
		return layout.Dimensions{}
	}
	ui.UnitBonePoseBarLog = ""
	// The buttons' clicks are drained before they are laid out
	// below, never after. Clickable.Layout also updates the gesture, and an
	// update that completes a click inside Layout hands the click to Layout —
	// which discards it, so a Clicked call later in the same frame finds
	// nothing. The log showed Set and Set all pressed (press=true→false with
	// hover held) without ever reaching their handlers, while Save — whose
	// release happened to land between its Layout and its Clicked — fired;
	// draining first inverts that race: a release waiting from the previous
	// frame is read here, before Layout can swallow it. This is the order the
	// rest of the app uses, with handleInput running ahead of Layout.
	setClicked := ui.UnitBoneKeySetClick.Clicked(gtx)
	setAllClicked := ui.UnitBoneKeySetAllClick.Clicked(gtx)
	saveClicked := ui.UnitBoneKeySaveClick.Clicked(gtx)
	importClicked := ui.UnitImpClick.Clicked(gtx)
	addPosClicked := ui.UnitBoneKeyAddPosClick.Clicked(gtx)
	addRotClicked := ui.UnitBoneKeyAddRotClick.Clicked(gtx)
	beforeClicked := ui.UnitBoneKeyBeforeClick.Clicked(gtx)
	afterClicked := ui.UnitBoneKeyAfterClick.Clicked(gtx)
	delClicked := ui.UnitBoneKeyDelClick.Clicked(gtx)
	mirClicked := [3]bool{
		ui.UnitBoneMirClick[0].Clicked(gtx),
		ui.UnitBoneMirClick[1].Clicked(gtx),
		ui.UnitBoneMirClick[2].Clicked(gtx),
	}
	shiftBackClicked := ui.UnitBoneKeyShiftBack.Clicked(gtx)
	shiftFwdClicked := ui.UnitBoneKeyShiftFwd.Clicked(gtx)
	undoClicked := ui.UnitBoneKeyUndoClick.Clicked(gtx)
	// Events first, before the fields are laid out below: an Enter pressed
	// since the last frame applies its triple, and the group it belongs to
	// is refilled with the values now in the key.
	UpdateBoneKeyEditors(ui, gtx, v)
	SyncBoneKeyFields(ui, gtx, v)
	// The frames field keeps its events drained like the typed groups:
	// a press inside it takes the keyboard, Enter lets go of it again.
	for {
		ev, ok := ui.UnitBoneKeyShiftEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.UnitBoneKeyShiftEd.Blur(gtx)
		}
	}
	// What the keyframe buttons act on — the picked bone's track to add a key
	// to, the rows of the list (or, with none picked, the key the strip picked)
	// to take away or to add keys beside — is read here, before the layout below
	// draws them disabled.
	canAddKey := v != nil && v.Anim != nil && v.BoneIndex >= 0 && BoneTrack(v) != nil
	actionRefs := BoneKeyActionRefs(ui, v)
	canDelKey := len(actionRefs) > 0
	canAddAround := canAddKey && len(actionRefs) > 0
	canShift := canDelKey
	canMir := false
	if v.Anim != nil {
		frame, _ := CurrentFrame(v)
		canMir = BoneKeyTarget(v, frame, 1) >= 0
	}
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) })
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyFieldsRow(ui, gtx, "Pos", &ui.UnitBonePosEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyFieldsRow(ui, gtx, "Rot", &ui.UnitBoneRotEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The angle sliders dial the rotation key the Rot fields
			// show about one axis each, across the full -180..180
			// turn the fields work in. They follow the fields, and
			// dragging one writes the key the same way Enter in a
			// field does.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Ang")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
					}
					for i := range ui.UnitBoneAngleSlid {
						i := i
						children = append(children,
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(9), boneFieldAxes[i])
								l.Color = uikit.CTextDim
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(56))
								gtx.Constraints.Max.X = gtx.Dp(unit.Dp(64))
								sl := material.Slider(ui.Th, &ui.UnitBoneAngleSlid[i])
								sl.Color = rgb(0x6B6B6B)
								// The slider's own Layout folds the drag into
								// its Value; the write to the key happens once,
								// after the whole row has laid out.
								return sl.Layout(gtx)
							}),
						)
					}
					dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
					// All three sliders have laid out, so their drags are in
					// Value now. While any is under the hand, store the trio
					// in the rotation key — the bone and the mesh skinned to
					// it turn to the new angle on this very frame.
					ApplyBoneAngleSliders(ui, v)
					return dims
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The mirror buttons flip the angle of the rotation key
			// the Rot fields show about their own axis: that axis's
			// angle comes back negated, the other two are left as
			// they were.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Mirror")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[0], "mirror X", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[1], "mirror Y", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[2], "mirror Z", !canMir)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The shift row moves the picked keys along the strip:
			// the field holds how many frames, and back/forward move
			// them by that many without reselecting.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), "Shift")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return UnitBoneKeyField(ui, gtx, "f", &ui.UnitBoneKeyShiftEd)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyShiftBack, "back", !canShift)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyShiftFwd, "fwd", !canShift)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "frames each press moves")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The keyframe buttons: give the picked bone a key on the frame
			// the playhead is on, or take the picked key away. The hint on
			// the right is how the keys already on show are moved — the
			// strip's marks are the handles their drag carries along.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAddPosClick, "+ pos", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAddRotClick, "+ rot", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyDelClick, "Delete key", !canDelKey)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "drag a mark on the strip to move its key")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The buttons that add a key either side of the picked rows:
			// one of the row's own kind on the nearest frame that carries
			// none yet, holding what the row holds. They take their own row
			// because the one above is full, and the hint on the right is
			// how a set of rows is gathered in the first place.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyBeforeClick, "+ before", !canAddAround)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAfterClick, "+ after", !canAddAround)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "Ctrl+click picks more rows")
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyUndoClick, "Undo", len(ui.UnitBoneUndo) == 0)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitBoneKeySetClick, "Set")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitBoneKeySetAllClick, "Set all")
						}),
						layout.Flexed(1, layout.Spacer{Width: unit.Dp(0)}.Layout),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// Import and Save share one row: they are the two things that
			// write out of this bar, and one of them opens a dialog rather
			// than editing. The note beside them is whichever is worth
			// reading now — a failure first, since it says why nothing was
			// written, and the imported file otherwise.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitImpClick, "Import frame's timeline")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeySaveClick, "Save WHM", !ui.UnitModelWhmDirty)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text, noteColor := ui.UnitImpPath, uikit.CTextDim
							if ui.UnitBoneKeyErr != "" {
								text, noteColor = ui.UnitBoneKeyErr, rgb(0xB00020)
							}
							if text == "" {
								return layout.Dimensions{}
							}
							if text == ui.UnitImpPath {
								text = filepath.Base(text)
							}
							l := material.Label(ui.Th, unit.Sp(9), text)
							l.Color = noteColor
							l.MaxLines = 2
							return l.Layout(gtx)
						}),
					)
				})
		}),
	)
	// The hover and press states of the buttons are logged on every
	// change: a button the mouse crosses without ever hovering has its hit
	// area somewhere else, a press that never becomes a click broke between
	// the frames, and seeing them settles where a dead click is not.
	btnState := [26]bool{
		ui.UnitBoneKeySetClick.Hovered(), ui.UnitBoneKeySetClick.Pressed(),
		ui.UnitBoneKeySetAllClick.Hovered(), ui.UnitBoneKeySetAllClick.Pressed(),
		ui.UnitBoneKeySaveClick.Hovered(), ui.UnitBoneKeySaveClick.Pressed(),
		ui.UnitBoneKeyAddPosClick.Hovered(), ui.UnitBoneKeyAddPosClick.Pressed(),
		ui.UnitBoneKeyAddRotClick.Hovered(), ui.UnitBoneKeyAddRotClick.Pressed(),
		ui.UnitBoneKeyDelClick.Hovered(), ui.UnitBoneKeyDelClick.Pressed(),
		ui.UnitBoneKeyBeforeClick.Hovered(), ui.UnitBoneKeyBeforeClick.Pressed(),
		ui.UnitBoneKeyAfterClick.Hovered(), ui.UnitBoneKeyAfterClick.Pressed(),
		ui.UnitBoneMirClick[0].Hovered(), ui.UnitBoneMirClick[0].Pressed(),
		ui.UnitBoneMirClick[1].Hovered(), ui.UnitBoneMirClick[1].Pressed(),
		ui.UnitBoneMirClick[2].Hovered(), ui.UnitBoneMirClick[2].Pressed(),
		ui.UnitBoneKeyShiftBack.Hovered(), ui.UnitBoneKeyShiftBack.Pressed(),
		ui.UnitBoneKeyShiftFwd.Hovered(), ui.UnitBoneKeyShiftFwd.Pressed(),
	}
	if btnState != ui.UnitBoneKeyBtnLog {
		LogInfo("bone: buttons [Set hover press | SetAll hover press | Save hover press | +pos hover press | +rot hover press | Del hover press | Before hover press | After hover press | MirX | MirY | MirZ | Back | Fwd] = %v", btnState)
		ui.UnitBoneKeyBtnLog = btnState
	}
	if importClicked {
		LogInfo("bone: Import frame's timeline pressed")
		OpenUnitImportDialog(ui)
	}
	if setClicked {
		LogInfo("bone: Set pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.UnitBonePosEd), boneFieldTexts(&ui.UnitBoneRotEd))
		// Set reads what was typed over: only a group with a changed axis is
		// applied, so a bone with keys of just one kind does not error on the
		// other, and an untouched group is a no-op rather than a surprise.
		any := false
		var firstErr string
		for mode := 0; mode < 2; mode++ {
			changed, _ := ModifiedBoneAxes(ui, mode)
			if len(changed) == 0 {
				continue
			}
			any = true
			LogInfo("bone: Set applies the %s group, changed axes %v", boneGroupLog(mode), changed)
			ApplyBoneKeyFields(ui, v, mode)
			if ui.UnitBoneKeyErr != "" && firstErr == "" {
				firstErr = ui.UnitBoneKeyErr
			}
		}
		switch {
		case !any:
			ui.UnitBoneKeyErr = "no values were changed"
		case firstErr != "":
			ui.UnitBoneKeyErr = firstErr
		}
	}
	if setAllClicked {
		LogInfo("bone: Set all pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.UnitBonePosEd), boneFieldTexts(&ui.UnitBoneRotEd))
		SetAllBoneKeyFields(ui, v)
	}
	if saveClicked {
		LogInfo("bone: Save pressed (dirty=%v)", ui.UnitModelWhmDirty)
		SaveUnitModelWhm(ui)
	}
	if addPosClicked {
		AddBoneKeyAtPlayhead(ui, v, 0)
	}
	if addRotClicked {
		AddBoneKeyAtPlayhead(ui, v, 1)
	}
	if beforeClicked {
		AddBoneKeysAround(ui, v, -1)
	}
	if afterClicked {
		AddBoneKeysAround(ui, v, 1)
	}
	if delClicked {
		DeleteBoneKeysAction(ui, v)
	}
	for axis := 0; axis < 3; axis++ {
		if mirClicked[axis] {
			MirrorBoneRotationAction(ui, v, axis)
		}
	}
	if shiftBackClicked {
		ShiftBoneKeysAction(ui, v, -1)
	}
	if shiftFwdClicked {
		ShiftBoneKeysAction(ui, v, 1)
	}
	if undoClicked {
		LogInfo("bone: Undo pressed (%d step(s) on the stack)", len(ui.UnitBoneUndo))
		UndoBoneKeyEdit(ui, v)
	}
	return dims
}

// updateBoneKeyEditors drains the typed fields' events, applying an Enter:
// the position group writes its XYZ triple into the target position key, the
// rotation group turns its XYZ degrees into the quaternion that key holds.
func UpdateBoneKeyEditors(ui *uiState, gtx layout.Context, v *modelViewer) {
	// Which field holds the caret is logged on change: typing needs it and
	// Enter submits through it, so a caret that never lands on a field
	// explains both of them going quiet.
	var focus [6]bool
	for m, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		for i := range eds {
			focus[m*3+i] = eds[i].Focused(gtx)
		}
	}
	if focus != ui.UnitBoneKeyFocusLog {
		LogInfo("bone: field focus — Pos X=%v Y=%v Z=%v, Rot X=%v Y=%v Z=%v",
			focus[0], focus[1], focus[2], focus[3], focus[4], focus[5])
		ui.UnitBoneKeyFocusLog = focus
	}
	// Tab moves the caret to the next of the six fields and Shift+Tab to the
	// previous one, wrapping round. The editor does not take Tab itself, so it
	// is read here, before the editors drain their own events.
	fields := [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd}
	for flat := 0; flat < 6; flat++ {
		tag := &fields[flat/3][flat%3]
		for {
			ev, ok := gtx.Event(key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift})
			if !ok {
				break
			}
			ke, isKey := ev.(key.Event)
			if !isKey || ke.State != key.Press {
				continue
			}
			next := (flat + 1) % 6
			if ke.Modifiers.Contain(key.ModShift) {
				next = (flat + 5) % 6
			}
			gtx.Execute(key.FocusCmd{Tag: &fields[next/3][next%3]})
		}
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		for i := range eds {
			for {
				ev, ok := eds[i].Update(gtx)
				if !ok {
					break
				}
				if _, submit := ev.(widget.SubmitEvent); submit {
					LogInfo("bone: Enter in the %s group: fields=[%s]", boneGroupLog(mode), boneFieldTexts(eds))
					ApplyBoneKeyFields(ui, v, mode)
					// Enter applies, then moves the caret to the next field: the
					// six fields run Pos X..Z then Rot X..Z and wrap round, so a
					// row of values is typed with Enter alone.
					flat := (mode*3 + i + 1) % 6
					fields := [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd}
					gtx.Execute(key.FocusCmd{Tag: &fields[flat/3][flat%3]})
				}
			}
		}
	}
}

// applyBoneKeyFields reads one triple out of the typed fields and writes it
// into the key it belongs to — the picked key when it is of that kind, the
// nearest key of that kind to the frame on show when it is not. A value that
// is not a number, or a bone with no key of that kind, is reported in the
// note beside Save rather than thrown away silently.
func ApplyBoneKeyFields(ui *uiState, v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	val, err := ParseBoneGroup(ui, mode)
	if err != nil {
		ui.UnitBoneKeyErr = err.Error()
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, mode)
	LogInfo("bone: apply %s: parsed=[%.3f %.3f %.3f] frame=%d target=%d picked(idx=%d mode=%d)",
		boneGroupLog(mode), val[0], val[1], val[2], frame, target, v.BoneKeyIndex, v.BoneKeyMode)
	if target < 0 {
		LogInfo("bone: apply %s: no target key in this clip", boneGroupLog(mode))
		ui.UnitBoneKeyErr = fmt.Sprintf("this bone has no %s key in the clip", kind)
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, mode
	ok := SetBoneKeyPosition(v, [3]float32{float32(val[0]), float32(val[1]), float32(val[2])})
	if mode == 1 {
		ok = SetBoneKeyRotation(v, quatFromEulerDeg(val[0], val[1], val[2]))
	}
	if !ok {
		LogWarn("bone: apply %s: the write was refused (bone=%d mode=%d idx=%d)", boneGroupLog(mode), v.BoneIndex, v.BoneKeyMode, v.BoneKeyIndex)
		ui.UnitBoneKeyErr = fmt.Sprintf("the %s key could not be written", kind)
		return
	}
	LogInfo("bone: apply %s: written ok, dirty=%v", boneGroupLog(mode), ui.UnitModelWhmDirty)
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	// The playhead is wound to the key that was just written so the edit is
	// on show, then the group shows what the key now holds.
	WindToPickedBoneKey(v)
	// The group is refilled from the key, so what is shown is what was
	// stored: a rotation comes back as the degrees its quaternion means.
	FillBoneKeyFields(ui, v, mode)
}

// setAllBoneKeyFields writes only the axes the user changed into every key of
// that kind the selected bone has in the clip: a Rot X of 180 typed over one
// frame becomes a Rot X of 180 on every rotation key, while the axes left
// alone keep their own per-key values. Everything is read before anything is
// written, so a field that does not parse leaves the keys untouched.
func SetAllBoneKeyFields(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil || v.BoneIndex < 0 {
		ui.UnitBoneKeyErr = "no bone is picked"
		return
	}
	t := BoneTrack(v)
	if t == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	axes0, ch0 := ModifiedBoneAxes(ui, 0)
	axes1, ch1 := ModifiedBoneAxes(ui, 1)
	LogInfo("bone: Set all: changed pos-axes=%v rot-axes=%v", axes0, axes1)
	if !ch0 && !ch1 {
		ui.UnitBoneKeyErr = "no values were changed"
		return
	}
	var val0, val1 [3]float64
	var err error
	if ch0 {
		if val0, err = ParseBoneGroup(ui, 0); err != nil {
			ui.UnitBoneKeyErr = err.Error()
			return
		}
	}
	if ch1 {
		if val1, err = ParseBoneGroup(ui, 1); err != nil {
			ui.UnitBoneKeyErr = err.Error()
			return
		}
	}
	snap, haveSnap := SnapshotBoneTrack(v)
	written := 0
	if ch0 {
		for i := range t.PosKey {
			p := t.PosKey[i].V
			for _, ax := range axes0 {
				p[ax] = float32(val0[ax])
			}
			t.PosKey[i].V = p
		}
		written += len(t.PosKey)
	}
	if ch1 {
		for i := range t.RotKey {
			e := eulerDegFromQuat(t.RotKey[i].V)
			for _, ax := range axes1 {
				e[ax] = val1[ax]
			}
			t.RotKey[i].V = quatFromEulerDeg(e[0], e[1], e[2])
		}
		written += len(t.RotKey)
	}
	if written == 0 {
		LogInfo("bone: Set all: no keys of the changed kind to write")
		ui.UnitBoneKeyErr = "the bone has no keys of the changed kind in this clip"
		return
	}
	// The pose cache predates the writes, so the viewport evaluates it again.
	v.PoseValid = false
	Invalidate(v)
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	name := "?"
	if v.BoneIndex < len(v.Model.Bones) {
		name = v.Model.Bones[v.BoneIndex].Name
	}
	LogInfo("bone: Set all: %d key(s) written for bone %s, dirty=%v", written, name, ui.UnitModelWhmDirty)
	ui.StatusText = fmt.Sprintf("Set changed values on %d key(s) of bone %s", written, name)
}

// syncBoneKeyFields keeps the typed fields on the keys a typed apply would
// write: the picked key for its own group, the nearest key of the other kind
// to the frame on show for the other.
//
// A change of selection — another clip, another bone, another picked key —
// refills both groups whatever the cursor is doing: the fields must show the
// bone that is now picked, not the one that was typed into before the switch,
// or Enter would write the old bone's values into the new one. A change of
// frame alone only refills the groups the cursor is not in, so typing
// survives the playhead being moved.
func SyncBoneKeyFields(ui *uiState, gtx layout.Context, v *modelViewer) {
	if v == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	sel := "no clip"
	if v.Anim != nil {
		sel = fmt.Sprintf("%s|%d|%d|%d", v.Anim.Name, v.BoneIndex, v.BoneKeyMode, v.BoneKeyIndex)
	}
	selChanged := sel != ui.UnitBoneKeyBound
	if !selChanged && frame == ui.UnitBoneKeyFrame {
		return
	}
	prevSel := ui.UnitBoneKeyBound
	ui.UnitBoneKeyBound = sel
	ui.UnitBoneKeyFrame = frame
	if selChanged {
		LogInfo("bone: selection changed %q -> %q: refilling both groups", prevSel, sel)
		ui.UnitBoneKeyErr = ""
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
		return
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		if eds[0].Focused(gtx) || eds[1].Focused(gtx) || eds[2].Focused(gtx) {
			continue
		}
		FillBoneKeyFields(ui, v, mode)
	}
}

// fillBoneKeyFields puts one group of fields on the values of the key a typed
// apply of that kind would write, or empties the group when there is no such
// key: an apply into nothing is not offered, and the fields say so by being
// blank.
func FillBoneKeyFields(ui *uiState, v *modelViewer, mode int) {
	eds := &ui.UnitBonePosEd
	if mode == 1 {
		eds = &ui.UnitBoneRotEd
	}
	var vals [3]float64
	ok := false
	if v != nil {
		frame, _ := CurrentFrame(v)
		if idx := BoneKeyTarget(v, frame, mode); idx >= 0 {
			if t := BoneTrack(v); t != nil {
				if mode == 0 && idx < len(t.PosKey) {
					p := t.PosKey[idx].V
					vals = [3]float64{float64(p[0]), float64(p[1]), float64(p[2])}
					ok = true
				} else if mode == 1 && idx < len(t.RotKey) {
					vals = boneAngleDegs(eulerDegFromQuat(t.RotKey[idx].V))
					ok = true
				}
			}
		}
	}
	prev := ui.UnitBoneKeyFillText[mode]
	if mode == 1 {
		// While a slider is under the hand the three sliders hold the
		// trio being written and the fields follow them. Reading the
		// key back mid-drag would show its canonical decomposition —
		// a different triple for the same rotation once an axis passes
		// ±90° — and refilling the two idle sliders from that fed the
		// flipped triple into the next frame's write, so all three
		// jumped back and forth every frame. With no hand on a slider,
		// everything follows the key again.
		held := false
		for i := range ui.UnitBoneAngleSlid {
			if ui.UnitBoneAngleSlid[i].Dragging() {
				held = true
				break
			}
		}
		if held && ok {
			vals = [3]float64{
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[0].Value),
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[1].Value),
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[2].Value),
			}
		} else if !held {
			for i := range ui.UnitBoneAngleSlid {
				frac := float32(0.5)
				if ok {
					frac = boneAngleSliderFrac(vals[i])
				}
				ui.UnitBoneAngleSlid[i].Value = frac
			}
		}
	}
	for i := range eds {
		text := ""
		if ok {
			if vals[i] == 0 {
				vals[i] = 0 // keep -0 from printing as "-0.000"
			}
			text = fmt.Sprintf("%.3f", vals[i])
		}
		// What the field showed is what a later change is measured against:
		// Set and Set all write only the axes typed over.
		ui.UnitBoneKeyFillText[mode][i] = text
		eds[i].SetText(text)
	}
	if now := ui.UnitBoneKeyFillText[mode]; now != prev {
		frame := -1
		if v != nil {
			frame, _ = CurrentFrame(v)
		}
		LogInfo("bone: fill %s frame=%d: [%s] (was [%s])", boneGroupLog(mode), frame,
			strings.Join(now[:], " "), strings.Join(prev[:], " "))
	}
}
