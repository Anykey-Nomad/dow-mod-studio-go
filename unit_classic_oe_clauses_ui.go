package main

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"dow-mod-studio/internal/whe"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (ui *uiState) ensureClassicOEClause() (whe.FxClause, bool) {
	if ui.unitEvtDoc == nil {
		return whe.FxClause{}, false
	}
	for _, clause := range ui.unitEvtDoc.Clauses() {
		if strings.EqualFold(clause.Name, ui.classicOEClauseName) {
			texts := clauseEditorValues(clause)
			bound := ui.unitEvtPath + "|" + clause.Name + "|" + strings.Join(texts[:], "|")
			if bound != ui.classicOEClauseBound {
				ui.classicOEClauseBound = bound
				ui.classicOEClauseError = ""
				for i, text := range texts {
					ui.classicOEClauseEditors[i].SetText(text)
					ui.classicOEClauseFill[i] = text
				}
			}
			return clause, true
		}
	}
	return whe.FxClause{}, false
}

func clauseEditorValues(c whe.FxClause) [5]string {
	var values [5]string
	kind := c.Type
	if kind == 3 {
		kind = int32(c.RawType)
	}
	values[0] = strconv.FormatInt(int64(kind), 10)
	values[1] = c.Variable
	values[2] = strconv.FormatInt(int64(c.Comparison), 10)
	switch c.Type {
	case whe.ClauseFloat:
		values[3] = strconv.FormatFloat(float64(c.FloatValue), 'g', -1, 32)
		values[4] = strconv.FormatFloat(float64(c.FloatDefault), 'g', -1, 32)
	case whe.ClauseBoolean:
		values[3] = strconv.FormatBool(c.BoolValue)
		values[4] = strconv.FormatBool(c.BoolDefault)
	case whe.ClauseString:
		values[3] = c.StringValue
		values[4] = c.StringDefault
	default:
		values[0] = strconv.FormatUint(uint64(c.RawType), 10)
	}
	return values
}

func (ui *uiState) layoutClassicOEClauseEditor(gtx layout.Context) layout.Dimensions {
	clause, ok := ui.ensureClassicOEClause()
	if !ok {
		return classicOEHint(gtx, ui.th, "Select a clause above to inspect and edit its definition.")
	}
	labels := [5]string{"Type (0 float / 1 boolean / 2 string)", "Variable", "Comparison (0..5)", "Value", "Default"}
	rows := make([]layout.FlexChild, 0, 7)
	for i := range ui.classicOEClauseEditors {
		i := i
		label := labels[i]
		if i == 3 || i == 4 {
			switch clause.Type {
			case whe.ClauseFloat:
				if i == 3 {
					label = "Value (×100 editor units)"
				} else {
					label = "Default (×100 editor units)"
				}
			case whe.ClauseBoolean:
				label += " (true / false)"
			case whe.ClauseString:
				label += " (string)"
			}
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEWheEditorRow(gtx, label, &ui.classicOEClauseEditors[i])
		}))
	}
	if ui.classicOEClauseError != "" {
		msg := ui.classicOEClauseError
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return ui.classicOEWheButtons(gtx, &ui.classicOEClauseApply)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) layoutClassicOEClauseList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	clauses := doc.Clauses()
	if len(clauses) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe defines no clauses.")
	}
	if len(ui.classicOEClauseClicks) != len(clauses) {
		ui.classicOEClauseClicks = make([]widget.Clickable, len(clauses))
	}
	rows := make([]layout.FlexChild, 0, len(clauses)+1)
	for i := range clauses {
		i, clause := i, clauses[i]
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEClauseRow(gtx, i, clause)
		}))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutClassicOEClauseEditor(gtx) }))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) classicOEClauseRow(gtx layout.Context, index int, clause whe.FxClause) layout.Dimensions {
	if index < 0 || index >= len(ui.classicOEClauseClicks) {
		return layout.Dimensions{}
	}
	click := &ui.classicOEClauseClicks[index]
	if click.Clicked(gtx) {
		ui.classicOEClauseName = clause.Name
		ui.classicOEClauseBound = ""
		ui.classicOEClauseError = ""
	}
	bg := cPanelBg
	if ui.classicOEClauseName == clause.Name {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, bg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), clause.Name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), whe.ClauseTypeName(clause.Type))
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) layoutClassicOEConditionEditorList(gtx layout.Context) layout.Dimensions {
	doc, msg := ui.classicOEListDoc()
	if doc == nil {
		return classicOEHint(gtx, ui.th, msg)
	}
	conditions := doc.Conditions()
	if len(conditions) == 0 {
		return classicOEHint(gtx, ui.th, "This .whe defines no conditions.")
	}
	if len(ui.classicOEConditionClicks) != len(conditions) {
		ui.classicOEConditionClicks = make([]widget.Clickable, len(conditions))
	}
	rows := make([]layout.FlexChild, 0, len(conditions)+1)
	for i := range conditions {
		i, condition := i, conditions[i]
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEConditionRow(gtx, i, condition)
		}))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutClassicOEConditionEditor(gtx) }))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) classicOEConditionRow(gtx layout.Context, index int, condition whe.FxCondition) layout.Dimensions {
	if index < 0 || index >= len(ui.classicOEConditionClicks) {
		return layout.Dimensions{}
	}
	click := &ui.classicOEConditionClicks[index]
	if click.Clicked(gtx) {
		ui.classicOEConditionName = condition.Name
		ui.classicOEConditionBound = ""
		ui.classicOEConditionError = ""
	}
	bg := cPanelBg
	if ui.classicOEConditionName == condition.Name {
		bg = cSelectBg
	} else if click.Hovered() {
		bg = cMenuHover
	}
	note := fmt.Sprintf("%d clauses", len(condition.ClauseNames))
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(22))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min, gtx.Constraints.Max = sz, sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, bg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), condition.Name)
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(9), note)
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}

func (ui *uiState) ensureClassicOECondition() (whe.FxCondition, bool) {
	if ui.unitEvtDoc == nil {
		return whe.FxCondition{}, false
	}
	for _, condition := range ui.unitEvtDoc.Conditions() {
		if strings.EqualFold(condition.Name, ui.classicOEConditionName) {
			bound := ui.unitEvtPath + "|" + condition.Name + "|" + strings.Join(condition.ClauseNames, "\x00")
			if bound != ui.classicOEConditionBound {
				ui.classicOEConditionBound = bound
				ui.classicOEConditionError = ""
				ui.classicOEConditionEditors = make([]widget.Editor, len(condition.ClauseNames))
				ui.classicOEConditionFill = append([]string(nil), condition.ClauseNames...)
				for i, name := range condition.ClauseNames {
					ui.classicOEConditionEditors[i].SetText(name)
				}
			}
			return condition, true
		}
	}
	return whe.FxCondition{}, false
}

func (ui *uiState) layoutClassicOEConditionEditor(gtx layout.Context) layout.Dimensions {
	condition, ok := ui.ensureClassicOECondition()
	if !ok {
		return classicOEHint(gtx, ui.th, "Select a condition to inspect and edit its clause references.")
	}
	rows := make([]layout.FlexChild, 0, len(condition.ClauseNames)+2)
	for i := range ui.classicOEConditionEditors {
		i := i
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.classicOEWheEditorRow(gtx, fmt.Sprintf("Clause reference %d", i+1), &ui.classicOEConditionEditors[i])
		}))
	}
	if ui.classicOEConditionError != "" {
		msg := ui.classicOEConditionError
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return classicOEHint(gtx, ui.th, msg) }))
	}
	rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEConditionAdd, "Add clause")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButtonDisabled(gtx, &ui.classicOEConditionRemove, "Remove last", len(ui.classicOEConditionEditors) == 0)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEConditionApply, "Apply")
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.classicOEWheSave, "Save .whe")
			}),
		)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

func (ui *uiState) applyClassicOEClause() {
	if ui.unitEvtDoc == nil {
		ui.classicOEClauseError = "No .whe is loaded."
		return
	}
	var clause whe.FxClause
	found := false
	for _, item := range ui.unitEvtDoc.Clauses() {
		if strings.EqualFold(item.Name, ui.classicOEClauseName) {
			clause, found = item, true
			break
		}
	}
	if !found {
		ui.classicOEClauseError = "Select a clause first."
		return
	}
	kind, err := strconv.ParseInt(strings.TrimSpace(ui.classicOEClauseEditors[0].Text()), 10, 32)
	if err != nil || kind < 0 || kind > 2 {
		ui.classicOEClauseError = "Type must be 0 (float), 1 (boolean), or 2 (string)."
		return
	}
	previousType := clause.Type
	clause.Type = int32(kind)
	clause.RawType = uint32(kind)
	clause.Variable = strings.TrimSpace(ui.classicOEClauseEditors[1].Text())
	comparison, err := strconv.ParseInt(strings.TrimSpace(ui.classicOEClauseEditors[2].Text()), 10, 32)
	if err != nil || comparison < 0 || comparison > 5 {
		ui.classicOEClauseError = "Comparison must be an integer from 0 to 5."
		return
	}
	clause.Comparison = int32(comparison)
	a, b := ui.classicOEClauseEditors[3].Text(), ui.classicOEClauseEditors[4].Text()
	if clause.Type == whe.ClauseFloat || clause.Type == whe.ClauseBoolean {
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	}
	switch clause.Type {
	case whe.ClauseFloat:
		if previousType != whe.ClauseFloat {
			a, b = "0", "0"
		}
		value, err1 := strconv.ParseFloat(a, 32)
		def, err2 := strconv.ParseFloat(b, 32)
		if err1 != nil || err2 != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.IsInf(def, 0) || math.IsNaN(def) {
			ui.classicOEClauseError = "Value and default must be finite numbers."
			return
		}
		clause.FloatValue, clause.FloatDefault = float32(value), float32(def)
		clause.BoolValue, clause.BoolDefault = false, false
		clause.StringValue, clause.StringDefault = "", ""
	case whe.ClauseBoolean:
		if previousType != whe.ClauseBoolean {
			a, b = "false", "false"
		}
		value, err1 := strconv.ParseBool(a)
		def, err2 := strconv.ParseBool(b)
		if err1 != nil || err2 != nil {
			ui.classicOEClauseError = "Boolean values must be true or false."
			return
		}
		clause.BoolValue, clause.BoolDefault = value, def
		clause.FloatValue, clause.FloatDefault = 0, 0
		clause.StringValue, clause.StringDefault = "", ""
	case whe.ClauseString:
		if previousType != whe.ClauseString {
			a, b = "", ""
		}
		clause.StringValue, clause.StringDefault = a, b
		clause.FloatValue, clause.FloatDefault = 0, 0
		clause.BoolValue, clause.BoolDefault = false, false
	}
	if err := ui.unitEvtDoc.SetClause(clause.Name, clause); err != nil {
		ui.classicOEClauseError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOEClauseBound = ""
	ui.classicOEClauseError = ""
	ui.statusText = "Applied clause " + clause.Name + " — Save .whe to write it into the mod"
}

func (ui *uiState) applyClassicOECondition() {
	if ui.unitEvtDoc == nil {
		ui.classicOEConditionError = "No .whe is loaded."
		return
	}
	condition, ok := ui.ensureClassicOECondition()
	if !ok {
		ui.classicOEConditionError = "Select a condition first."
		return
	}
	refs := make([]string, len(ui.classicOEConditionEditors))
	for i := range refs {
		refs[i] = strings.TrimSpace(ui.classicOEConditionEditors[i].Text())
		if refs[i] == "" {
			ui.classicOEConditionError = fmt.Sprintf("Clause reference %d cannot be empty.", i+1)
			return
		}
	}
	condition.ClauseNames = refs
	if err := ui.unitEvtDoc.SetCondition(condition.Name, condition); err != nil {
		ui.classicOEConditionError = err.Error()
		return
	}
	ui.unitEvtDirty = true
	ui.classicOEConditionBound = ""
	ui.classicOEConditionError = ""
	ui.statusText = "Applied condition " + condition.Name + " — Save .whe to write it into the mod"
}

func (ui *uiState) handleClassicOEClauseConditionInput(gtx layout.Context) {
	if ui.currentUnitSection().label != unitSectionClassicOE || ui.unitEvtDoc == nil {
		return
	}
	for i := range ui.classicOEClauseEditors {
		ui.updateClassicOEWheEditor(gtx, &ui.classicOEClauseEditors[i], ui.applyClassicOEClause)
	}
	for i := range ui.classicOEConditionEditors {
		ui.updateClassicOEWheEditor(gtx, &ui.classicOEConditionEditors[i], ui.applyClassicOECondition)
	}
	if ui.classicOEClauseApply.Clicked(gtx) {
		ui.applyClassicOEClause()
	}
	if ui.classicOEConditionApply.Clicked(gtx) {
		ui.applyClassicOECondition()
	}
	if ui.classicOEConditionAdd.Clicked(gtx) {
		if _, ok := ui.ensureClassicOECondition(); ok {
			ui.classicOEConditionEditors = append(ui.classicOEConditionEditors, widget.Editor{SingleLine: true, Submit: true})
			ui.classicOEConditionFill = append(ui.classicOEConditionFill, "")
		}
	}
	if ui.classicOEConditionRemove.Clicked(gtx) && len(ui.classicOEConditionEditors) > 0 {
		ui.classicOEConditionEditors = ui.classicOEConditionEditors[:len(ui.classicOEConditionEditors)-1]
		ui.classicOEConditionFill = ui.classicOEConditionFill[:len(ui.classicOEConditionFill)-1]
	}
}
