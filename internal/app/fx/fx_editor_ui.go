package fx

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	appunit "dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/app/viewer"
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
	"image"
	"math"
	"strconv"
	"strings"
)

// modelFxChecklist lists every resolved effect. Timeline membership supplies
// the default checks; explicit selections are kept on the viewer across clips.
func ModelFxChecklist(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	v = FxChecklistViewer(ui, v)
	if v == nil || v.Model == nil {
		return layout.Dimensions{}
	}
	anim := ""
	if v.Anim != nil {
		anim = v.Anim.Name
	}
	FxSelectionAnimName(v, anim)
	if len(*clicks) != len(v.FxEffects) {
		*clicks = make([]widget.Clickable, len(v.FxEffects))
	}
	if len(v.FxAttrClicks) != len(v.FxEffects) {
		v.FxAttrClicks = make([]widget.Clickable, len(v.FxEffects))
	}
	if v.FxAttrClick.Clicked(gtx) {
		if v.FxAttrIndex < 0 && len(v.FxEffects) > 0 {
			v.FxAttrIndex = 0
			SyncFXAttributeEditors(v, 0)
		}
		if v.FxAttrIndex >= 0 {
			v.FxAttrOpen = true
			viewer.Invalidate(v)
		}
	}
	children := []layout.FlexChild{}
	if len(v.FxEffects) == 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "No FX events were resolved for this model.")
			l.Color = uikit.CTextDim
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6), Top: unit.Dp(6)}.Layout(gtx, l.Layout)
		}))
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return FxAttributeDialogLauncher(ui, gtx, v)
		}))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	}
	for i := range v.FxEffects {
		i := i
		e := v.FxEffects[i]
		click := &(*clicks)[i]
		if click.Clicked(gtx) {
			FxSetEffectSelected(v, i, anim, !FxEffectSelected(v, i, anim))
			viewer.Invalidate(v)
		}
		checked := FxEffectSelected(v, i, anim)
		inTimeline := FxEffectVisible(v, i)
		marker := "no marker"
		if e.Marker >= 0 && e.Marker < len(v.Model.Markers) {
			marker = v.Model.Markers[e.Marker].Name
		}
		if e.HasOther && e.OtherMarker >= 0 && e.OtherMarker < len(v.Model.Markers) {
			marker += " → " + v.Model.Markers[e.OtherMarker].Name
		}
		name := e.Label
		if e.FxName != "" {
			name += " · " + e.FxName
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return FxChecklistRow(ui, gtx, click, checked, name, marker, v.FxAttrIndex == i, inTimeline)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					attrClick := &v.FxAttrClicks[i]
					if attrClick.Clicked(gtx) {
						if v.FxAttrIndex != i {
							v.FxAttrIndex = i
							SyncFXAttributeEditors(v, i)
						}
						v.FxAttrOpen = true
						viewer.Invalidate(v)
					}
					return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, attrClick, "Attributes")
					})
				}),
			)
		}))
	}
	children = append([]layout.FlexChild{layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return FxAttributeDialogLauncher(ui, gtx, v)
	})}, children...)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func FxAttributeDialogLauncher(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer) layout.Dimensions {
	if v.FxAttrClick.Clicked(gtx) {
		if v.FxAttrIndex < 0 && len(v.FxEffects) > 0 {
			v.FxAttrIndex = 0
			SyncFXAttributeEditors(v, 0)
		}
		if v.FxAttrIndex >= 0 {
			v.FxAttrOpen = true
			viewer.Invalidate(v)
		}
	}
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		label := "FX attributes"
		if v.FxAttrIndex >= 0 && v.FxAttrIndex < len(v.FxEffects) {
			label += ": " + v.FxEffects[v.FxAttrIndex].Label
		}
		return uikit.SmallButton(ui, gtx, &v.FxAttrClick, label)
	})
}

// fxChecklistViewer chooses the viewer whose effect list is currently shown.
// The unit/Object viewport owns a separate viewer from the selected WHM tab.
func FxChecklistViewer(ui *uistate.State, v *viewer.ModelViewer) *viewer.ModelViewer {
	if ui.UnitModel != nil && ui.UnitModel.Model != nil && ui.UnitModel.Model == v {
		return ui.UnitModel.Model
	}
	for _, t := range ui.OpenTabs {
		if t != nil && t.Kind == "whm" && t.Model == v {
			return t.Model
		}
	}
	return v
}

func FxChecklistRow(ui *uistate.State, gtx layout.Context, click *widget.Clickable, checked bool, title, marker string, selected, inTimeline bool) layout.Dimensions {
	bg := uikit.CPanelBg
	if selected {
		bg = uikit.CSelectBg
	} else if click.Hovered() {
		bg = uikit.CMenuHover
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(uikit.UnitRowHeightDp + 8))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
					appunit.Rrect(ui, gtx, bg, sz, unit.Dp(2))
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
							l := material.Label(ui.Th, unit.Sp(10), mark+"  "+title)
							l.Color, l.MaxLines = uikit.CText, 1
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(9), marker)
							l.Color, l.MaxLines = uikit.CTextDim, 1
							return layout.Inset{Left: unit.Dp(20)}.Layout(gtx, l.Layout)
						}),
					)
				})
			}),
		)
	})
}

func EnsureFXOverride(v *viewer.ModelViewer, index int) {
	if v.FxOverrides == nil {
		v.FxOverrides = make(map[int]FxAttributeOverride)
	}
	if _, ok := v.FxOverrides[index]; !ok {
		v.FxOverrides[index] = FxAttributeOverride{}
	}
}

func SyncFXAttributeEditors(v *viewer.ModelViewer, index int) {
	if v == nil || index < 0 || index >= len(v.FxEffects) {
		return
	}
	origin := v.FxEffects[index].Origin
	rotation := [3]float64{}
	distance := origin.Length()
	if override, ok := v.FxOverrides[index]; ok {
		if override.HasOrigin {
			origin = override.Origin
		}
		rotation = override.Rotation
		if override.HasScaleDistance {
			distance = override.ScaleDistance
		}
	}
	editors := []*widget.Editor{&v.FxAttrPosX, &v.FxAttrPosY, &v.FxAttrPosZ, &v.FxAttrRotX, &v.FxAttrRotY, &v.FxAttrRotZ, &v.FxAttrDistance}
	values := []float64{origin.X, origin.Y, origin.Z, rotation[0], rotation[1], rotation[2], distance}
	for i := range editors {
		editors[i].SetText(strconv.FormatFloat(values[i], 'f', 3, 64))
	}
}

func ApplyFXAttributeField(v *viewer.ModelViewer, field int) {
	if v == nil || v.FxAttrIndex < 0 || v.FxAttrIndex >= len(v.FxEffects) || field < 0 || field >= 7 {
		return
	}
	editors := []*widget.Editor{&v.FxAttrPosX, &v.FxAttrPosY, &v.FxAttrPosZ, &v.FxAttrRotX, &v.FxAttrRotY, &v.FxAttrRotZ, &v.FxAttrDistance}
	value, ok := FxEditorValue(editors[field])
	if !ok {
		v.FxAttrError = "Enter a valid number"
		return
	}
	if field >= 3 && field <= 5 && (value < 1 || value > 360) {
		v.FxAttrError = "Angle must be between 1° and 360°"
		return
	}
	if field == 6 && value < 0 {
		v.FxAttrError = "Distance must not be negative"
		return
	}
	v.FxAttrError = ""
	EnsureFXOverride(v, v.FxAttrIndex)
	o := v.FxOverrides[v.FxAttrIndex]
	switch field {
	case 0, 1, 2:
		origin := v.FxEffects[v.FxAttrIndex].Origin
		if o.HasOrigin {
			origin = o.Origin
		}
		origin = FxAttributeOriginAxis(origin, field, value)
		o.Origin, o.HasOrigin = origin, true
	case 3, 4, 5:
		o.Rotation[field-3] = value
	case 6:
		o.ScaleDistance, o.HasScaleDistance = value, true
	}
	v.FxOverrides[v.FxAttrIndex] = o
	viewer.Invalidate(v)
}

// FX attribute origin inputs are viewer/model space, where +Z is up. Setters
// use precisely the matching fauxgl component without an FX file-axis swap.
func FxAttributeOriginAxis(origin fauxgl.Vector, axis int, value float64) fauxgl.Vector {
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

func FxEditorValue(ed *widget.Editor) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(ed.Text()), 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func ModelFxAttributeDialog(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer) layout.Dimensions {
	if v == nil || v.Model == nil || !v.FxAttrOpen || v.FxAttrIndex < 0 || v.FxAttrIndex >= len(v.FxEffects) {
		return layout.Dimensions{}
	}
	editors := []*widget.Editor{&v.FxAttrPosX, &v.FxAttrPosY, &v.FxAttrPosZ, &v.FxAttrRotX, &v.FxAttrRotY, &v.FxAttrRotZ, &v.FxAttrDistance}
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
			ApplyFXAttributeField(v, i)
		}
		for {
			ev, ok := gtx.Event(key.Filter{Focus: ed, Name: key.NameEscape})
			if !ok {
				break
			}
			if event, ok := ev.(key.Event); ok && event.State == key.Press {
				v.FxAttrOpen = false
			}
		}
	}
	if v.FxAttrSetPos.Clicked(gtx) {
		var vals [3]float64
		valid := true
		for i := range vals {
			vals[i], valid = FxEditorValue(editors[i])
			if !valid {
				break
			}
		}
		if valid {
			EnsureFXOverride(v, v.FxAttrIndex)
			ov := v.FxOverrides[v.FxAttrIndex]
			ov.Origin, ov.HasOrigin = fauxgl.Vector{X: vals[0], Y: vals[1], Z: vals[2]}, true
			v.FxOverrides[v.FxAttrIndex] = ov
			v.FxAttrError = ""
			viewer.Invalidate(v)
		} else {
			v.FxAttrError = "Enter valid coordinates"
		}
	}
	if v.FxAttrSetAngle.Clicked(gtx) {
		var angles [3]float64
		valid := true
		for i := range angles {
			angles[i], valid = FxEditorValue(editors[i+3])
			if !valid || angles[i] < 1 || angles[i] > 360 {
				valid = false
				break
			}
		}
		if valid {
			EnsureFXOverride(v, v.FxAttrIndex)
			ov := v.FxOverrides[v.FxAttrIndex]
			ov.Rotation = angles
			v.FxOverrides[v.FxAttrIndex] = ov
			v.FxAttrError = ""
			viewer.Invalidate(v)
		} else {
			v.FxAttrError = "Each angle must be 1°..360°"
		}
	}
	if v.FxAttrResetPos.Clicked(gtx) {
		if ov, ok := v.FxOverrides[v.FxAttrIndex]; ok {
			ov.HasOrigin = false
			v.FxOverrides[v.FxAttrIndex] = ov
		}
		SyncFXAttributeEditors(v, v.FxAttrIndex)
		viewer.Invalidate(v)
	}
	if v.FxAttrResetRot.Clicked(gtx) {
		if ov, ok := v.FxOverrides[v.FxAttrIndex]; ok {
			ov.Rotation = [3]float64{}
			v.FxOverrides[v.FxAttrIndex] = ov
		}
		SyncFXAttributeEditors(v, v.FxAttrIndex)
		viewer.Invalidate(v)
	}
	if v.FxAttrSetDistance.Clicked(gtx) {
		if d, ok := FxEditorValue(&v.FxAttrDistance); ok && d >= 0 {
			EnsureFXOverride(v, v.FxAttrIndex)
			ov := v.FxOverrides[v.FxAttrIndex]
			ov.ScaleDistance, ov.HasScaleDistance = d, true
			v.FxOverrides[v.FxAttrIndex] = ov
			viewer.Invalidate(v)
		}
	}
	if v.FxAttrDir.Clicked(gtx) {
		EnsureFXOverride(v, v.FxAttrIndex)
		ov := v.FxOverrides[v.FxAttrIndex]
		ov.ShowDirection = !ov.ShowDirection
		v.FxOverrides[v.FxAttrIndex] = ov
		viewer.Invalidate(v)
	}
	if v.FxAttrMarker.Clicked(gtx) {
		EnsureFXOverride(v, v.FxAttrIndex)
		ov := v.FxOverrides[v.FxAttrIndex]
		ov.ShowMarker = !ov.ShowMarker
		v.FxOverrides[v.FxAttrIndex] = ov
		viewer.Invalidate(v)
	}
	UpdateFXAttributeDrag(v, gtx)
	if !v.FxAttrOpen {
		return layout.Dimensions{}
	}

	w := uikit.MinInt(gtx.Dp(unit.Dp(360)), gtx.Constraints.Max.X-16)
	h := uikit.MinInt(gtx.Dp(unit.Dp(420)), gtx.Constraints.Max.Y-16)
	w, h = uikit.MaxInt(240, w), uikit.MaxInt(300, h)
	if v.FxAttrWindowX == 0 && v.FxAttrWindowY == 0 {
		v.FxAttrWindowX = uikit.MaxInt(8, (gtx.Constraints.Max.X-w)/2)
		v.FxAttrWindowY = uikit.MaxInt(8, (gtx.Constraints.Max.Y-h)/2)
	}
	v.FxAttrWindowX = uikit.ClampInt(v.FxAttrWindowX, 0, uikit.MaxInt(0, gtx.Constraints.Max.X-w))
	v.FxAttrWindowY = uikit.ClampInt(v.FxAttrWindowY, 0, uikit.MaxInt(0, gtx.Constraints.Max.Y-h))
	e := v.FxEffects[v.FxAttrIndex]
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			at := image.Pt(v.FxAttrWindowX, v.FxAttrWindowY)
			child := gtx
			child.Constraints.Min, child.Constraints.Max = image.Pt(w, h), image.Pt(w, h)
			off := op.Offset(at).Push(gtx.Ops)
			defer off.Pop()
			return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(child, func(gtx layout.Context) layout.Dimensions {
				uikit.FillRect(gtx, uikit.CPanelBg, image.Pt(w, h))
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FxAttributeTitle(ui, gtx, v, e) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Origin X", &v.FxAttrPosX)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Origin Y", &v.FxAttrPosY)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Origin Z", &v.FxAttrPosZ)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Rotate X° (1..360)", &v.FxAttrRotX)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Rotate Y° (1..360)", &v.FxAttrRotY)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Rotate Z° (1..360)", &v.FxAttrRotZ)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return FxAttributeField(ui, gtx, "Distance", &v.FxAttrDistance)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FxAttributeButtonRows(ui, gtx, v) }), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if v.FxAttrError == "" {
							return layout.Dimensions{}
						}
						l := material.Label(ui.Th, unit.Sp(9), v.FxAttrError)
						l.Color = uikit.CProblem
						return layout.Inset{Left: unit.Dp(10), Top: unit.Dp(4)}.Layout(gtx, l.Layout)
					}),
				)
			})
		}),
	)
}

func FxAttributeTitle(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer, e FxOverlayEffect) layout.Dimensions {
	h := gtx.Dp(unit.Dp(32))
	sz := image.Pt(gtx.Constraints.Max.X, h)
	titleW := gtx.Constraints.Max.X - gtx.Dp(unit.Dp(36))
	if titleW < 1 {
		titleW = gtx.Constraints.Max.X
	}
	if v.FxAttrClose.Clicked(gtx) {
		v.FxAttrOpen = false
	}
	titleRect := clip.Rect{Max: image.Pt(titleW, h)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &v.FxAttrDragTag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.FxAttrDragTag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		p, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch p.Kind {
		case pointer.Press:
			v.FxAttrDragging = true
			v.FxAttrDragOrigin = image.Pt(v.FxAttrWindowX, v.FxAttrWindowY)
			v.FxAttrDragX, v.FxAttrDragY = p.Position.X, p.Position.Y
		case pointer.Drag:
			if v.FxAttrDragging {
				v.FxAttrWindowX = v.FxAttrDragOrigin.X + int(p.Position.X-v.FxAttrDragX)
				v.FxAttrWindowY = v.FxAttrDragOrigin.Y + int(p.Position.Y-v.FxAttrDragY)
			}
		case pointer.Release, pointer.Cancel:
			v.FxAttrDragging = false
		}
	}
	titleRect.Pop()
	uikit.FillRect(gtx, uikit.CHeaderBg, sz)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = titleW
			gtx.Constraints.Max.X = titleW
			l := material.Label(ui.Th, unit.Sp(11), e.Label+" attributes  ·  drag to move")
			l.Color, l.MaxLines = uikit.CText, 1
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(7)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButton(ui, gtx, &v.FxAttrClose, "×")
		}),
	)
}

func FxAttributeField(ui *uistate.State, gtx layout.Context, label string, ed *widget.Editor) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), label)
				l.Color = uikit.CTextDim
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				editor := material.Editor(ui.Th, ed, "")
				editor.TextSize = unit.Sp(10)
				editor.HintColor = uikit.CTextDim
				return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx, editor.Layout)
			}),
		)
	})
}

func FxAttributeButtonRows(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrSetPos, "Set origin")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrSetAngle, "Set angle")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrResetPos, "Use marker")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrSetDistance, "Set distance")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrResetRot, "Reset rotation")
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrDir, FxAttrToggleLabel(v.FxOverrides, v.FxAttrIndex, "Direction"))
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &v.FxAttrMarker, FxAttrToggleLabel(v.FxOverrides, v.FxAttrIndex, "Marker"))
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(9), "clip: "+v.FxSelectionAnim)
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			}),
		)
	})
}

func FxAttrToggleLabel(overrides map[int]FxAttributeOverride, index int, label string) string {
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

func UpdateFXAttributeDrag(v *viewer.ModelViewer, gtx layout.Context) {
	if !v.FxAttrDragging {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &v.FxAttrDragTag, Kinds: pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		p, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch p.Kind {
		case pointer.Drag:
			v.FxAttrWindowX = v.FxAttrDragOrigin.X + int(p.Position.X-v.FxAttrDragX)
			v.FxAttrWindowY = v.FxAttrDragOrigin.Y + int(p.Position.Y-v.FxAttrDragY)
		case pointer.Release, pointer.Cancel:
			v.FxAttrDragging = false
		}
	}
}
