package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/whe"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var classicOESelectedUIFields = [20]string{
	"Display type (circle / square)",
	"Indicator scale X", "Indicator scale Z",
	"Indicator offset X", "Indicator offset Z",
	"Volume offset X", "Volume offset Y", "Volume offset Z",
	"Volume scale X", "Volume scale Y", "Volume scale Z",
	"Matrix 1,1", "Matrix 1,2", "Matrix 1,3",
	"Matrix 2,1", "Matrix 2,2", "Matrix 2,3",
	"Matrix 3,1", "Matrix 3,2", "Matrix 3,3",
}

var classicOESelectedUIMatrixFields = [9]string{
	"Matrix 1,1", "Matrix 1,2", "Matrix 1,3",
	"Matrix 2,1", "Matrix 2,2", "Matrix 2,3",
	"Matrix 3,1", "Matrix 3,2", "Matrix 3,3",
}

func selectedUIValues(v whe.FxSelectedUI) [20]string {
	var out [20]string
	out[0] = strconv.FormatInt(int64(v.DisplayType), 10)
	at := 1
	for _, pair := range [][2]float32{v.Scale, v.Offset} {
		for _, n := range pair {
			out[at] = strconv.FormatFloat(float64(n), 'g', -1, 32)
			at++
		}
	}
	for _, triple := range [][3]float32{v.VolumeOffset, v.VolumeScale} {
		for _, n := range triple {
			out[at] = strconv.FormatFloat(float64(n), 'g', -1, 32)
			at++
		}
	}
	for _, row := range v.Matrix {
		for _, n := range row {
			out[at] = strconv.FormatFloat(float64(n), 'g', -1, 32)
			at++
		}
	}
	return out
}

func selectedUIDisplayTypeCode(shape string) (int32, error) {
	switch shape {
	case "circle":
		return 0, nil
	case "square":
		return 1, nil
	default:
		return 0, fmt.Errorf("display type must be circle or square")
	}
}

func selectedUIFromValues(v whe.FxSelectedUI, values [20]string) (whe.FxSelectedUI, error) {
	kind, err := strconv.ParseInt(strings.TrimSpace(values[0]), 10, 32)
	if err != nil {
		return v, fmt.Errorf("display type must be circle or square")
	}
	if kind != 0 && kind != 1 {
		return v, fmt.Errorf("display type must be 0 (circle) or 1 (square)")
	}
	v.DisplayType = int32(kind)
	parse := func(index int) (float32, error) {
		n, err := strconv.ParseFloat(strings.TrimSpace(values[index]), 32)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("%s: enter a finite number", classicOESelectedUIFields[index])
		}
		return float32(n), nil
	}
	at := 1
	for pairIndex := range [][2]float32{v.Scale, v.Offset} {
		pair := [2]float32{}
		for i := range pair {
			pair[i], err = parse(at)
			if err != nil {
				return v, err
			}
			at++
		}
		if pairIndex == 0 {
			v.Scale = pair
		} else {
			v.Offset = pair
		}
	}
	for tripleIndex := range [][3]float32{v.VolumeOffset, v.VolumeScale} {
		triple := [3]float32{}
		for i := range triple {
			triple[i], err = parse(at)
			if err != nil {
				return v, err
			}
			at++
		}
		if tripleIndex == 0 {
			v.VolumeOffset = triple
		} else {
			v.VolumeScale = triple
		}
	}
	for row := range v.Matrix {
		for col := range v.Matrix[row] {
			v.Matrix[row][col], err = parse(at)
			if err != nil {
				return v, err
			}
			at++
		}
	}
	return v, nil
}

func (ui *uiState) ensureClassicOESelectedUI() (whe.FxSelectedUI, bool, error) {
	ui.syncUnitSelectionIndicator(ui.unitModelViewer())
	if ui.unitEvtDoc == nil {
		return whe.FxSelectedUI{}, false, fmt.Errorf("%s", ui.unitEvtErr)
	}
	v, present, err := ui.unitEvtDoc.SelectedUI()
	if err != nil {
		return v, present, err
	}
	if !present {
		return v, false, nil
	}
	values := selectedUIValues(v)
	if ui.classicOESelectedUIType == nil {
		ui.classicOESelectedUIType = &widget.Enum{}
	}
	bound := ui.unitEvtPath
	for i := range values {
		bound += "|" + values[i]
	}
	if bound != ui.classicOESelectedUIBound {
		ui.classicOESelectedUIBound = bound
		ui.classicOESelectedUIError = ""
		for i := range values {
			ui.classicOESelectedUIEditors[i].SetText(values[i])
			ui.classicOESelectedUIFill[i] = values[i]
		}
		if displayType, err := selectedUIDisplayTypeCodeFromValue(v.DisplayType); err == nil {
			ui.classicOESelectedUIType.Value = displayType
		} else {
			ui.classicOESelectedUIType.Value = "circle"
		}
	}
	if model := ui.unitModelViewer(); model != nil {
		indicator := &engine.SelectionIndicator{DisplayType: v.DisplayType, Scale: v.Scale, Offset: v.Offset}
		if model.selectionIndicator == nil || *model.selectionIndicator != *indicator {
			model.selectionIndicator = indicator
			model.invalidate()
		}
	}
	return v, true, nil
}

func (ui *uiState) layoutClassicOESelectedUI(gtx layout.Context) layout.Dimensions {
	_, present, err := ui.ensureClassicOESelectedUI()
	if err != nil {
		return classicOEHint(gtx, ui.th, "Could not read DATASEUI: "+err.Error())
	}
	if !present {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return classicOEHint(gtx, ui.th, "This .whe has no DATASEUI chunk.")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOESelectedUICreate, "Create Selected UI")
			}),
		)
	}
	var children []layout.FlexChild
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIVisibility(gtx)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIDisplayType(gtx)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIPair(gtx, "Indicator scale", "X", "Z", 1)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIPair(gtx, "Indicator offset", "X", "Z", 3)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUITriple(gtx, "Volume offset", "X", "Y", "Z", 5)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUITriple(gtx, "Volume scale", "X", "Y", "Z", 8)
	}))
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.layoutClassicOESelectedUIMatrix(gtx)
	}))
	if ui.classicOESelectedUIError != "" {
		msg := ui.classicOESelectedUIError
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.classicOEWheButtons(gtx, &ui.classicOESelectedUIApply)
	}))
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func selectedUIDisplayTypeCodeFromValue(displayType int32) (string, error) {
	switch displayType {
	case 0:
		return "circle", nil
	case 1:
		return "square", nil
	default:
		return "", fmt.Errorf("unknown display type %d", displayType)
	}
}

func (ui *uiState) layoutClassicOESelectedUIVisibility(gtx layout.Context) layout.Dimensions {
	v := ui.unitModelViewer()
	if v == nil {
		return layout.Dimensions{}
	}
	if ui.classicOESelectedUIVisible == nil {
		ui.classicOESelectedUIVisible = &widget.Bool{Value: v.showSelection}
	} else if ui.classicOESelectedUIVisible.Value != v.showSelection {
		ui.classicOESelectedUIVisible.Value = v.showSelection
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		check := material.CheckBox(ui.th, ui.classicOESelectedUIVisible, "Show selection outline")
		check.TextSize = unit.Sp(9)
		return check.Layout(gtx)
	})
}

func (ui *uiState) layoutClassicOESelectedUIDisplayType(gtx layout.Context) layout.Dimensions {
	if ui.classicOESelectedUIType == nil {
		ui.classicOESelectedUIType = &widget.Enum{}
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Label(ui.th, unit.Sp(9), "Display type")
				label.Color = cTextDim
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						radio := material.RadioButton(ui.th, ui.classicOESelectedUIType, "circle", "Circle")
						radio.TextSize = unit.Sp(9)
						return radio.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						radio := material.RadioButton(ui.th, ui.classicOESelectedUIType, "square", "Square")
						radio.TextSize = unit.Sp(9)
						return radio.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (ui *uiState) layoutClassicOESelectedUIPair(gtx layout.Context, title, axis1, axis2 string, first int) layout.Dimensions {
	return ui.layoutClassicOESelectedUIAxes(gtx, title, []string{axis1, axis2}, first)
}

func (ui *uiState) layoutClassicOESelectedUITriple(gtx layout.Context, title, axis1, axis2, axis3 string, first int) layout.Dimensions {
	return ui.layoutClassicOESelectedUIAxes(gtx, title, []string{axis1, axis2, axis3}, first)
}

func (ui *uiState) layoutClassicOESelectedUIAxes(gtx layout.Context, title string, axes []string, first int) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Label(ui.th, unit.Sp(9), title)
				label.Color = cTextDim
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, func() []layout.FlexChild {
					children := make([]layout.FlexChild, 0, len(axes)*3-1)
					for i, axis := range axes {
						i, axis := i, axis
						if i != 0 {
							children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
						}
						children = append(children,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								label := material.Label(ui.th, unit.Sp(8), axis)
								label.Color = cTextDim
								gtx.Constraints.Min.X = gtx.Dp(12)
								gtx.Constraints.Max.X = gtx.Dp(12)
								return layout.Center.Layout(gtx, label.Layout)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return ui.classicOEWheEditorField(gtx, &ui.classicOESelectedUIEditors[first+i])
							}),
						)
					}
					return children
				}()...)
			}),
		)
	})
}

func (ui *uiState) layoutClassicOESelectedUIMatrix(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Label(ui.th, unit.Sp(9), "Matrix")
				label.Color = cTextDim
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(layout.Spacer{Width: unit.Dp(22)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.layoutClassicOESelectedUIAxisHeadings(gtx, "X", "Y", "Z")
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					ui.layoutClassicOESelectedUIMatrixRow(gtx, 0, "X"),
					ui.layoutClassicOESelectedUIMatrixRow(gtx, 1, "Y"),
					ui.layoutClassicOESelectedUIMatrixRow(gtx, 2, "Z"),
				)
			}),
		)
	})
}

func (ui *uiState) layoutClassicOESelectedUIAxisHeadings(gtx layout.Context, axes ...string) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(axes)*2-1)
	for i, axis := range axes {
		axis := axis
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			label := material.Label(ui.th, unit.Sp(8), axis)
			label.Color = cTextDim
			return layout.Center.Layout(gtx, label.Layout)
		}))
		if i != len(axes)-1 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
		}
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

func (ui *uiState) layoutClassicOESelectedUIMatrixRow(gtx layout.Context, row int, axis string) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(22), gtx.Dp(22)
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						label := material.Label(ui.th, unit.Sp(8), axis)
						label.Color = cTextDim
						return label.Layout(gtx)
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, func() []layout.FlexChild {
						children := make([]layout.FlexChild, 0, 5)
						for col := 0; col < 3; col++ {
							index := 11 + row*3 + col
							children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return ui.classicOEWheEditorField(gtx, &ui.classicOESelectedUIEditors[index])
							}))
							if col != 2 {
								children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout))
							}
						}
						return children
					}()...)
				}),
			)
		})
	})
}

func (ui *uiState) classicOEWheEditorField(gtx layout.Context, ed *widget.Editor) layout.Dimensions {
	ed.SingleLine, ed.Submit = true, true
	gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = gtx.Dp(22), gtx.Dp(22)
	return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			w := material.Editor(ui.th, ed, "")
			w.TextSize = unit.Sp(10)
			return w.Layout(gtx)
		})
	})
}

func (ui *uiState) ensureClassicOEModifier() (whe.FxModifier, bool) {
	if ui.unitEvtDoc == nil {
		return whe.FxModifier{}, false
	}
	mods := ui.unitEvtDoc.Modifiers()
	for _, m := range mods {
		if strings.EqualFold(m.Name, ui.classicOEModifierName) {
			bound := ui.unitEvtPath + "|" + m.Name + "|" + m.Variable + "|" + strconv.Itoa(int(m.Type)) + "|" + strconv.FormatFloat(float64(m.Reference), 'g', -1, 32) + "|" + strconv.FormatFloat(float64(m.Default), 'g', -1, 32)
			if bound != ui.classicOEModifierBound {
				ui.classicOEModifierBound = bound
				ui.classicOEModifierError = ""
				texts := [4]string{m.Variable, strconv.FormatInt(int64(m.Type), 10), strconv.FormatFloat(float64(m.Reference), 'g', -1, 32), strconv.FormatFloat(float64(m.Default), 'g', -1, 32)}
				for i := range texts {
					ui.classicOEModifierEditors[i].SetText(texts[i])
					ui.classicOEModifierFill[i] = texts[i]
				}
			}
			return m, true
		}
	}
	return whe.FxModifier{}, false
}

func (ui *uiState) layoutClassicOEModifierEditor(gtx layout.Context) layout.Dimensions {
	m, ok := ui.ensureClassicOEModifier()
	if !ok {
		return classicOEHint(gtx, ui.th, "Select a modifier above to inspect and edit its definition.")
	}
	children := []layout.FlexChild{}
	labels := [4]string{"Variable", "Type (0 absolute time / 1 speed scale)", "Reference value", "Default value"}
	for i := range ui.classicOEModifierEditors {
		i := i
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEWheEditorRow(gtx, labels[i], &ui.classicOEModifierEditors[i])
		}))
	}
	_ = m
	if ui.classicOEModifierError != "" {
		msg := ui.classicOEModifierError
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.classicOEWheButtons(gtx, &ui.classicOEModifierApply)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (ui *uiState) classicOEWheEditorRow(gtx layout.Context, label string, ed *widget.Editor) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(9), label)
				l.Color = cTextDim
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.classicOEWheEditorField(gtx, ed)
			}),
		)
	})
}

func (ui *uiState) classicOEWheButtons(gtx layout.Context, apply *widget.Clickable) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, apply, "Apply") }),
			layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEWheSave, "Save .whe")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &ui.classicOEWheRevert, "Revert", !ui.unitEvtDirty)
			}),
		)
	})
}

func (ui *uiState) handleClassicOEWheInput(gtx layout.Context) {
	if ui.currentUnitSection().label != unitSectionClassicOE {
		return
	}
	if ui.unitEvtDoc == nil {
		return
	}
	for i := range ui.classicOESelectedUIEditors {
		if i == 0 {
			continue // Display type is selected with the Circle/Square radio buttons.
		}
		ui.updateClassicOEWheEditor(gtx, &ui.classicOESelectedUIEditors[i], func() { ui.applyClassicOESelectedUI() })
	}
	for i := range ui.classicOEModifierEditors {
		ui.updateClassicOEWheEditor(gtx, &ui.classicOEModifierEditors[i], func() { ui.applyClassicOEModifier() })
	}
	if ui.classicOESelectedUICreate.Clicked(gtx) {
		ui.createClassicOESelectedUI()
	}
	if ui.classicOESelectedUIVisible != nil && ui.classicOESelectedUIVisible.Update(gtx) {
		if v := ui.unitModelViewer(); v != nil {
			v.showSelection = ui.classicOESelectedUIVisible.Value
			v.invalidate()
		}
	}
	if ui.classicOESelectedUIApply.Clicked(gtx) {
		ui.applyClassicOESelectedUI()
	}
	if ui.classicOEModifierApply.Clicked(gtx) {
		ui.applyClassicOEModifier()
	}
	if ui.classicOEWheSave.Clicked(gtx) {
		ui.saveClassicOEWhe()
	}
	if ui.classicOEWheRevert.Clicked(gtx) {
		ui.revertUnitEvents()
		ui.classicOESelectedUIBound = ""
		ui.classicOEModifierBound = ""
	}
}

func (ui *uiState) updateClassicOEWheEditor(gtx layout.Context, ed *widget.Editor, submit func()) {
	if ui.rgdEnsureEditorClick(ed).click.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	for {
		ev, ok := ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			submit()
			ed.Blur(gtx)
		}
	}
}

func (ui *uiState) createClassicOESelectedUI() {
	if ui.unitEvtDoc == nil {
		ui.classicOESelectedUIError = "No .whe is loaded."
		return
	}
	value := whe.FxSelectedUI{DisplayType: 0}
	value.Scale = [2]float32{1, 1}
	value.VolumeScale = [3]float32{1, 1, 1}
	for i := range value.Matrix {
		value.Matrix[i][i] = 1
	}
	if err := ui.unitEvtDoc.SetSelectedUI(value); err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOESelectedUIBound = ""
	ui.classicOESelectedUIError = ""
	ui.updateUnitSelectionIndicator(value)
	ui.statusText = "Created DATASEUI — Save .whe to write it into the mod"
}

func (ui *uiState) updateUnitSelectionIndicator(value whe.FxSelectedUI) {
	if v := ui.unitModelViewer(); v != nil {
		indicator := &engine.SelectionIndicator{DisplayType: value.DisplayType, Scale: value.Scale, Offset: value.Offset}
		if v.selectionIndicator == nil || *v.selectionIndicator != *indicator {
			v.selectionIndicator = indicator
			v.invalidate()
		}
	}
}

// syncUnitSelectionIndicator loads the selected unit's DATASEUI values for
// either model panel and clears the previous unit's indicator when none exists.
func (ui *uiState) syncUnitSelectionIndicator(v *modelViewer) {
	if v == nil {
		return
	}
	ui.ensureUnitEvents()
	var indicator *engine.SelectionIndicator
	if ui.unitEvtDoc != nil {
		value, present, err := ui.unitEvtDoc.SelectedUI()
		if err == nil && present {
			indicator = &engine.SelectionIndicator{DisplayType: value.DisplayType, Scale: value.Scale, Offset: value.Offset}
		}
	}
	if v.selectionIndicator == nil && indicator == nil {
		return
	}
	if v.selectionIndicator != nil && indicator != nil && *v.selectionIndicator == *indicator {
		return
	}
	v.selectionIndicator = indicator
	v.invalidate()
}

func (ui *uiState) applyClassicOESelectedUI() {
	if ui.unitEvtDoc == nil {
		ui.classicOESelectedUIError = "No .whe is loaded."
		return
	}
	value, present, err := ui.unitEvtDoc.SelectedUI()
	if err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	if !present {
		ui.classicOESelectedUIError = "This .whe has no DATASEUI chunk."
		return
	}
	var values [20]string
	for i := range values {
		values[i] = ui.classicOESelectedUIEditors[i].Text()
	}
	if ui.classicOESelectedUIType != nil {
		displayType, err := selectedUIDisplayTypeCode(ui.classicOESelectedUIType.Value)
		if err != nil {
			ui.classicOESelectedUIError = err.Error()
			return
		}
		values[0] = strconv.FormatInt(int64(displayType), 10)
	}
	value, err = selectedUIFromValues(value, values)
	if err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	if err = ui.unitEvtDoc.SetSelectedUI(value); err != nil {
		ui.classicOESelectedUIError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOESelectedUIBound = ""
	ui.classicOESelectedUIError = ""
	ui.updateUnitSelectionIndicator(value)
	ui.statusText = "Applied selection volume settings — Save .whe to write them into the mod"
}

func (ui *uiState) applyClassicOEModifier() {
	if ui.unitEvtDoc == nil {
		ui.classicOEModifierError = "No .whe is loaded."
		return
	}
	var value whe.FxModifier
	for _, m := range ui.unitEvtDoc.Modifiers() {
		if strings.EqualFold(m.Name, ui.classicOEModifierName) {
			value = m
			break
		}
	}
	if value.Name == "" {
		ui.classicOEModifierError = "Select a modifier first."
		return
	}
	value.Variable = strings.TrimSpace(ui.classicOEModifierEditors[0].Text())
	kind, err := strconv.ParseInt(strings.TrimSpace(ui.classicOEModifierEditors[1].Text()), 10, 32)
	if err != nil || kind < 0 || kind > 1 {
		ui.classicOEModifierError = "Type must be 0 (absolute time) or 1 (speed scale)."
		return
	}
	value.Type = int32(kind)
	for i, dst := range []*float32{&value.Reference, &value.Default} {
		n, parseErr := strconv.ParseFloat(strings.TrimSpace(ui.classicOEModifierEditors[i+2].Text()), 32)
		if parseErr != nil {
			ui.classicOEModifierError = "Reference and default values must be finite numbers."
			return
		}
		*dst = float32(n)
	}
	if err = ui.unitEvtDoc.SetModifier(value.Name, value); err != nil {
		ui.classicOEModifierError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOEModifierBound = ""
	ui.classicOEModifierError = ""
	ui.statusText = "Applied modifier " + value.Name + " — Save .whe to write it into the mod"
}

func (ui *uiState) saveClassicOEWhe() {
	line, err := ui.saveUnitEvents()
	if err != nil {
		ui.statusText = "Save failed: " + err.Error()
		return
	}
	ui.statusText = line
	ui.markStateDirty()
}
