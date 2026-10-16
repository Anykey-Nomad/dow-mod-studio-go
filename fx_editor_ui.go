package main

import (
	"image"
	"math"
	"strconv"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

// modelFxChecklist lists every resolved effect. Timeline membership supplies
// the default checks; explicit selections are kept on the viewer across clips.
func (ui *uiState) modelFxChecklist(gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	v = ui.fxChecklistViewer(v)
	if v == nil || v.model == nil {
		return layout.Dimensions{}
	}
	anim := ""
	if v.anim != nil {
		anim = v.anim.Name
	}
	v.fxSelectionAnimName(anim)
	if len(*clicks) != len(v.fxEffects) {
		*clicks = make([]widget.Clickable, len(v.fxEffects))
	}
	if len(v.fxAttrClicks) != len(v.fxEffects) {
		v.fxAttrClicks = make([]widget.Clickable, len(v.fxEffects))
	}
	if v.fxAttrClick.Clicked(gtx) {
		if v.fxAttrIndex < 0 && len(v.fxEffects) > 0 {
			v.fxAttrIndex = 0
			v.syncFXAttributeEditors(0)
		}
		if v.fxAttrIndex >= 0 {
			v.fxAttrOpen = true
			v.invalidate()
		}
	}
	children := []layout.FlexChild{}
	if len(v.fxEffects) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), "No FX events were resolved for this model.")
			l.Color = cTextDim
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6), Top: unit.Dp(6)}.Layout(gtx, l.Layout)
		}))
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.fxAttributeDialogLauncher(gtx, v)
		}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	for i := range v.fxEffects {
		i := i
		e := v.fxEffects[i]
		click := &(*clicks)[i]
		if click.Clicked(gtx) {
			v.fxSetEffectSelected(i, anim, !v.fxEffectSelected(i, anim))
			v.invalidate()
		}
		checked := v.fxEffectSelected(i, anim)
		inTimeline := v.fxEffectVisible(i)
		marker := "no marker"
		if e.Marker >= 0 && e.Marker < len(v.model.Markers) {
			marker = v.model.Markers[e.Marker].Name
		}
		if e.HasOther && e.OtherMarker >= 0 && e.OtherMarker < len(v.model.Markers) {
			marker += " → " + v.model.Markers[e.OtherMarker].Name
		}
		name := e.Label
		if e.FxName != "" {
			name += " · " + e.FxName
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return ui.fxChecklistRow(gtx, click, checked, name, marker, v.fxAttrIndex == i, inTimeline)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					attrClick := &v.fxAttrClicks[i]
					if attrClick.Clicked(gtx) {
						if v.fxAttrIndex != i {
							v.fxAttrIndex = i
							v.syncFXAttributeEditors(i)
						}
						v.fxAttrOpen = true
						v.invalidate()
					}
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, attrClick, "Attributes")
					})
				}),
			)
		}))
	}
	children = append([]layout.FlexChild{layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.fxAttributeDialogLauncher(gtx, v)
	})}, children...)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (ui *uiState) fxAttributeDialogLauncher(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v.fxAttrClick.Clicked(gtx) {
		if v.fxAttrIndex < 0 && len(v.fxEffects) > 0 {
			v.fxAttrIndex = 0
			v.syncFXAttributeEditors(0)
		}
		if v.fxAttrIndex >= 0 {
			v.fxAttrOpen = true
			v.invalidate()
		}
	}
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := "FX attributes"
		if v.fxAttrIndex >= 0 && v.fxAttrIndex < len(v.fxEffects) {
			label += ": " + v.fxEffects[v.fxAttrIndex].Label
		}
		return ui.smallButton(gtx, &v.fxAttrClick, label)
	})
}

// fxChecklistViewer chooses the viewer whose effect list is currently shown.
// The unit/Object viewport owns a separate viewer from the selected WHM tab.
func (ui *uiState) fxChecklistViewer(v *modelViewer) *modelViewer {
	if ui.unitModel != nil && ui.unitModel.model != nil && ui.unitModel.model == v {
		return ui.unitModel.model
	}
	for _, t := range ui.openTabs {
		if t != nil && t.kind == "whm" && t.model == v {
			return t.model
		}
	}
	return v
}

func (ui *uiState) fxChecklistRow(gtx layout.Context, click *widget.Clickable, checked bool, title, marker string, selected, inTimeline bool) layout.Dimensions {
	bg := cPanelBg
	if selected {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp + 8))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					ui.rrect(gtx, bg, sz, unit.Dp(2))
				}
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							mark := "☐"
							if checked {
								mark = "☑"
							}
							if !inTimeline {
								mark += "·"
							}
							l := material.Label(ui.th, unit.Sp(10), mark+"  "+title)
							l.Color, l.MaxLines = cText, 1
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), marker)
							l.Color, l.MaxLines = cTextDim, 1
							return layout.Inset{Left: unit.Dp(20)}.Layout(gtx, l.Layout)
						}),
					)
				})
			}),
		)
	})
}

func (v *modelViewer) ensureFXOverride(index int) {
	if v.fxOverrides == nil {
		v.fxOverrides = make(map[int]fxAttributeOverride)
	}
	if _, ok := v.fxOverrides[index]; !ok {
		v.fxOverrides[index] = fxAttributeOverride{}
	}
}

func (v *modelViewer) syncFXAttributeEditors(index int) {
	if v == nil || index < 0 || index >= len(v.fxEffects) {
		return
	}
	origin := v.fxEffects[index].Origin
	rotation := [3]float64{}
	distance := origin.Length()
	if override, ok := v.fxOverrides[index]; ok {
		if override.HasOrigin {
			origin = override.Origin
		}
		rotation = override.Rotation
		if override.HasScaleDistance {
			distance = override.ScaleDistance
		}
	}
	editors := []*widget.Editor{&v.fxAttrPosX, &v.fxAttrPosY, &v.fxAttrPosZ, &v.fxAttrRotX, &v.fxAttrRotY, &v.fxAttrRotZ, &v.fxAttrDistance}
	values := []float64{origin.X, origin.Y, origin.Z, rotation[0], rotation[1], rotation[2], distance}
	for i := range editors {
		editors[i].SetText(strconv.FormatFloat(values[i], 'f', 3, 64))
	}
}

func (v *modelViewer) applyFXAttributeField(field int) {
	if v == nil || v.fxAttrIndex < 0 || v.fxAttrIndex >= len(v.fxEffects) || field < 0 || field >= 7 {
		return
	}
	editors := []*widget.Editor{&v.fxAttrPosX, &v.fxAttrPosY, &v.fxAttrPosZ, &v.fxAttrRotX, &v.fxAttrRotY, &v.fxAttrRotZ, &v.fxAttrDistance}
	value, ok := fxEditorValue(editors[field])
	if !ok {
		v.fxAttrError = "Enter a valid number"
		return
	}
	if field >= 3 && field <= 5 && (value < 1 || value > 360) {
		v.fxAttrError = "Angle must be between 1° and 360°"
		return
	}
	if field == 6 && value < 0 {
		v.fxAttrError = "Distance must not be negative"
		return
	}
	v.fxAttrError = ""
	v.ensureFXOverride(v.fxAttrIndex)
	o := v.fxOverrides[v.fxAttrIndex]
	switch field {
	case 0, 1, 2:
		origin := v.fxEffects[v.fxAttrIndex].Origin
		if o.HasOrigin {
			origin = o.Origin
		}
		origin = fxAttributeOriginAxis(origin, field, value)
		o.Origin, o.HasOrigin = origin, true
	case 3, 4, 5:
		o.Rotation[field-3] = value
	case 6:
		o.ScaleDistance, o.HasScaleDistance = value, true
	}
	v.fxOverrides[v.fxAttrIndex] = o
	v.invalidate()
}

// FX attribute origin inputs are viewer/model space, where +Z is up. Setters
// use precisely the matching fauxgl component without an FX file-axis swap.
func fxAttributeOriginAxis(origin fauxgl.Vector, axis int, value float64) fauxgl.Vector {
	switch axis {
	case 0:
		origin.X = value
	case 1:
		origin.Y = value
	case 2:
		origin.Z = value
	}
	return origin
}

func fxEditorValue(ed *widget.Editor) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(ed.Text()), 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func (ui *uiState) modelFxAttributeDialog(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v == nil || v.model == nil || !v.fxAttrOpen || v.fxAttrIndex < 0 || v.fxAttrIndex >= len(v.fxEffects) {
		return layout.Dimensions{}
	}
	editors := []*widget.Editor{&v.fxAttrPosX, &v.fxAttrPosY, &v.fxAttrPosZ, &v.fxAttrRotX, &v.fxAttrRotY, &v.fxAttrRotZ, &v.fxAttrDistance}
	for _, ed := range editors {
		ed.SingleLine = true
	}
	for i, ed := range editors {
		ed.Submit = true
		submit := false
		for {
			ev, ok := ed.Update(gtx)
			if !ok {
				break
			}
			if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
				submit = true
			}
		}
		// widget.Editor submits on NameEnter, but some Windows backends
		// deliver Enter as NameReturn. Check the currently focused editor for
		// both names after Update has drained the editor events.
		if gtx.Focused(ed) {
			for _, name := range []key.Name{key.NameReturn, key.NameEnter} {
				for {
					ev, ok := gtx.Event(key.Filter{Focus: ed, Name: name})
					if !ok {
						break
					}
					if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
						submit = true
					}
				}
			}
		}
		if submit {
			v.applyFXAttributeField(i)
		}
		for {
			ev, ok := gtx.Event(key.Filter{Focus: ed, Name: key.NameEscape})
			if !ok {
				break
			}
			if event, ok := ev.(key.Event); ok && event.State == key.Press {
				v.fxAttrOpen = false
			}
		}
	}
	if v.fxAttrSetPos.Clicked(gtx) {
		var vals [3]float64
		valid := true
		for i := range vals {
			vals[i], valid = fxEditorValue(editors[i])
			if !valid {
				break
			}
		}
		if valid {
			v.ensureFXOverride(v.fxAttrIndex)
			ov := v.fxOverrides[v.fxAttrIndex]
			ov.Origin, ov.HasOrigin = fauxgl.Vector{X: vals[0], Y: vals[1], Z: vals[2]}, true
			v.fxOverrides[v.fxAttrIndex] = ov
			v.fxAttrError = ""
			v.invalidate()
		} else {
			v.fxAttrError = "Enter valid coordinates"
		}
	}
	if v.fxAttrSetAngle.Clicked(gtx) {
		var angles [3]float64
		valid := true
		for i := range angles {
			angles[i], valid = fxEditorValue(editors[i+3])
			if !valid || angles[i] < 1 || angles[i] > 360 {
				valid = false
				break
			}
		}
		if valid {
			v.ensureFXOverride(v.fxAttrIndex)
			ov := v.fxOverrides[v.fxAttrIndex]
			ov.Rotation = angles
			v.fxOverrides[v.fxAttrIndex] = ov
			v.fxAttrError = ""
			v.invalidate()
		} else {
			v.fxAttrError = "Each angle must be 1°..360°"
		}
	}
	if v.fxAttrResetPos.Clicked(gtx) {
		if ov, ok := v.fxOverrides[v.fxAttrIndex]; ok {
			ov.HasOrigin = false
			v.fxOverrides[v.fxAttrIndex] = ov
		}
		v.syncFXAttributeEditors(v.fxAttrIndex)
		v.invalidate()
	}
	if v.fxAttrResetRot.Clicked(gtx) {
		if ov, ok := v.fxOverrides[v.fxAttrIndex]; ok {
			ov.Rotation = [3]float64{}
			v.fxOverrides[v.fxAttrIndex] = ov
		}
		v.syncFXAttributeEditors(v.fxAttrIndex)
		v.invalidate()
	}
	if v.fxAttrSetDistance.Clicked(gtx) {
		if d, ok := fxEditorValue(&v.fxAttrDistance); ok && d >= 0 {
			v.ensureFXOverride(v.fxAttrIndex)
			ov := v.fxOverrides[v.fxAttrIndex]
			ov.ScaleDistance, ov.HasScaleDistance = d, true
			v.fxOverrides[v.fxAttrIndex] = ov
			v.invalidate()
		}
	}
	if v.fxAttrDir.Clicked(gtx) {
		v.ensureFXOverride(v.fxAttrIndex)
		ov := v.fxOverrides[v.fxAttrIndex]
		ov.ShowDirection = !ov.ShowDirection
		v.fxOverrides[v.fxAttrIndex] = ov
		v.invalidate()
	}
	if v.fxAttrMarker.Clicked(gtx) {
		v.ensureFXOverride(v.fxAttrIndex)
		ov := v.fxOverrides[v.fxAttrIndex]
		ov.ShowMarker = !ov.ShowMarker
		v.fxOverrides[v.fxAttrIndex] = ov
		v.invalidate()
	}
	v.updateFXAttributeDrag(gtx)
	if !v.fxAttrOpen {
		return layout.Dimensions{}
	}

	w := minInt(gtx.Dp(unit.Dp(360)), gtx.Constraints.Max.X-16)
	h := minInt(gtx.Dp(unit.Dp(420)), gtx.Constraints.Max.Y-16)
	w, h = maxInt(240, w), maxInt(300, h)
	if v.fxAttrWindowX == 0 && v.fxAttrWindowY == 0 {
		v.fxAttrWindowX = maxInt(8, (gtx.Constraints.Max.X-w)/2)
		v.fxAttrWindowY = maxInt(8, (gtx.Constraints.Max.Y-h)/2)
	}
	v.fxAttrWindowX = clampInt(v.fxAttrWindowX, 0, maxInt(0, gtx.Constraints.Max.X-w))
	v.fxAttrWindowY = clampInt(v.fxAttrWindowY, 0, maxInt(0, gtx.Constraints.Max.Y-h))
	e := v.fxEffects[v.fxAttrIndex]
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			at := image.Pt(v.fxAttrWindowX, v.fxAttrWindowY)
			child := gtx
			child.Constraints.Min, child.Constraints.Max = image.Pt(w, h), image.Pt(w, h)
			off := op.Offset(at).Push(gtx.Ops)
			defer off.Pop()
			return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(child, func(gtx layout.Context) layout.Dimensions {
				fillRect(gtx, cPanelBg, image.Pt(w, h))
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAttributeTitle(gtx, v, e) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAttributeField(gtx, "Origin X", &v.fxAttrPosX) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAttributeField(gtx, "Origin Y", &v.fxAttrPosY) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAttributeField(gtx, "Origin Z", &v.fxAttrPosZ) }),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.fxAttributeField(gtx, "Rotate X° (1..360)", &v.fxAttrRotX)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.fxAttributeField(gtx, "Rotate Y° (1..360)", &v.fxAttrRotY)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.fxAttributeField(gtx, "Rotate Z° (1..360)", &v.fxAttrRotZ)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.fxAttributeField(gtx, "Distance", &v.fxAttrDistance)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAttributeButtonRows(gtx, v) }), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.fxAttrError == "" {
							return layout.Dimensions{}
						}
						l := material.Label(ui.th, unit.Sp(9), v.fxAttrError)
						l.Color = cProblem
						return layout.Inset{Left: unit.Dp(10), Top: unit.Dp(4)}.Layout(gtx, l.Layout)
					}),
				)
			})
		}),
	)
}

func (ui *uiState) fxAttributeTitle(gtx layout.Context, v *modelViewer, e fxOverlayEffect) layout.Dimensions {
	h := gtx.Dp(unit.Dp(32))
	sz := image.Pt(gtx.Constraints.Max.X, h)
	titleW := gtx.Constraints.Max.X - gtx.Dp(unit.Dp(36))
	if titleW < 1 {
		titleW = gtx.Constraints.Max.X
	}
	if v.fxAttrClose.Clicked(gtx) {
		v.fxAttrOpen = false
	}
	titleRect := clip.Rect{Max: image.Pt(titleW, h)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.fxAttrDragTag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.fxAttrDragTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		p, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch p.Kind {
		case pointer.Press:
			v.fxAttrDragging = true
			v.fxAttrDragOrigin = image.Pt(v.fxAttrWindowX, v.fxAttrWindowY)
			v.fxAttrDragX, v.fxAttrDragY = p.Position.X, p.Position.Y
		case pointer.Drag:
			if v.fxAttrDragging {
				v.fxAttrWindowX = v.fxAttrDragOrigin.X + int(p.Position.X-v.fxAttrDragX)
				v.fxAttrWindowY = v.fxAttrDragOrigin.Y + int(p.Position.Y-v.fxAttrDragY)
			}
		case pointer.Release, pointer.Cancel:
			v.fxAttrDragging = false
		}
	}
	titleRect.Pop()
	fillRect(gtx, cHeaderBg, sz)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = titleW
			gtx.Constraints.Max.X = titleW
			l := material.Label(ui.th, unit.Sp(11), e.Label+" attributes  ·  drag to move")
			l.Color, l.MaxLines = cText, 1
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(7)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButton(gtx, &v.fxAttrClose, "×")
		}),
	)
}

func (ui *uiState) fxAttributeField(gtx layout.Context, label string, ed *widget.Editor) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), label)
				l.Color = cTextDim
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				editor := material.Editor(ui.th, ed, "")
				editor.TextSize = unit.Sp(10)
				editor.HintColor = cTextDim
				return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx, editor.Layout)
			}),
		)
	})
}

func (ui *uiState) fxAttributeButtonRows(gtx layout.Context, v *modelViewer) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &v.fxAttrSetPos, "Set origin") }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &v.fxAttrSetAngle, "Set angle") }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, &v.fxAttrResetPos, "Use marker")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, &v.fxAttrSetDistance, "Set distance")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, &v.fxAttrResetRot, "Reset rotation")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, &v.fxAttrDir, fxAttrToggleLabel(v.fxOverrides, v.fxAttrIndex, "Direction"))
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.smallButton(gtx, &v.fxAttrMarker, fxAttrToggleLabel(v.fxOverrides, v.fxAttrIndex, "Marker"))
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(9), "clip: "+v.fxSelectionAnim)
				l.Color = cTextDim
				return l.Layout(gtx)
			}),
		)
	})
}

func fxAttrToggleLabel(overrides map[int]fxAttributeOverride, index int, label string) string {
	if o, ok := overrides[index]; ok {
		switch label {
		case "Direction":
			if o.ShowDirection {
				return "Arrow on"
			}
		case "Marker":
			if o.ShowMarker {
				return "Marker on"
			}
		}
	}
	if label == "Direction" {
		return "Arrow off"
	}
	return "Marker off"
}

func (v *modelViewer) updateFXAttributeDrag(gtx layout.Context) {
	if !v.fxAttrDragging {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.fxAttrDragTag, Kinds: pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		p, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch p.Kind {
		case pointer.Drag:
			v.fxAttrWindowX = v.fxAttrDragOrigin.X + int(p.Position.X-v.fxAttrDragX)
			v.fxAttrWindowY = v.fxAttrDragOrigin.Y + int(p.Position.Y-v.fxAttrDragY)
		case pointer.Release, pointer.Cancel:
			v.fxAttrDragging = false
		}
	}
}
