package main

import (
	"dow-mod-studio/internal/rgd"
	"fmt"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (ui *uiState) propertiesTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	e := ui.rgdSelectedEntryFor(t)
	var name, typeLabel string
	if e == nil {
		name = "—"
		typeLabel = "—"
	} else {
		if t.rgdSelected == "GameData" || t.rgdSelected == "" {
			name = "GameData"
		} else if idx := strings.LastIndex(t.rgdSelected, "/"); idx >= 0 {
			name = t.rgdSelected[idx+1:]
		} else {
			name = e.Name
		}
		typeLabel = e.TypeLabel()
	}
	isTable := e != nil && e.IsTable()
	editableScalar := e != nil && rgdIsEditableType(e.Type)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Name", name, false) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Data Type", typeLabel, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if isTable {
				return ui.tableRowEditor(gtx, "Reference", &t.rgdRefEditor, false, "")
			}
			return ui.tableRow2(gtx, "Reference", e.RefString(), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if e != nil && e.Type == rgd.RgdTypeBool {
				return ui.tableRowBool(gtx, "Value", t, e, t.rgdSelected, true)
			}
			if e != nil && (e.Type == rgd.RgdTypeString || e.Type == rgd.RgdTypeWString) {
				return ui.tableRowStringEditor(gtx, "Value", t, e, t.rgdSelected, true)
			}
			if editableScalar {
				ed := ui.rgdEnsureValEditor(t, t.rgdSelected, e.DisplayValue())
				return ui.tableRowEditor(gtx, "Value", ed, true, t.rgdValErrors[t.rgdSelected])
			}
			if e != nil && !isTable {
				return ui.tableRow2(gtx, "Value", e.DisplayFieldValue(), true)
			}
			return layout.Dimensions{}
		}),
	)
}

var childrenRows = [][]string{
	{"ability_ext", "ebpextensions\\ability_ext.lua"},
	{"addon_ext", "ebpextensions\\addon_ext.lua"},
	{"cost_ext", "ebpextensions\\cost_ext.lua"},
	{"deep_strike_ext", "ebpextensions\\deep_strike_ext.lua"},
	{"entity_blueprint_ext", "ebpextensions\\entity_blueprint_ext.lua"},
	{"event_manager_ext", "ebpextensions\\event_manager_ext.lua"},
	{"health_ext", "ebpextensions\\health_ext.lua"},
	{"hq_ext", "ebpextensions\\hq_ext.lua"},
	{"infiltration_ext", "ebpextensions\\infiltration_ext.lua"},
	{"modifier_apply_ext", "ebpextensions\\modifier_apply_ext.lua"},
	{"modifier_ext", "ebpextensions\\modifier_ext.lua"},
	{"offline_ext", "ebpextensions\\offline_ext.lua"},
	{"requirement_ext", "ebpextensions\\requirement_ext.lua"},
	{"research_ext", "ebpextensions\\research_ext.lua"},
	{"resource_ext", "ebpextensions\\resource_ext.lua"},
	{"sight_ext", "ebpextensions\\sight_ext.lua"},
	{"sim_entity_ext", "ebpextensions\\sim_entity_ext.lua"},
	{"spawner_ext", "ebpextensions\\spawner_ext.lua"},
	{"squad_hold_ext", "ebpextensions\\squad_hold_ext.lua"},
	{"structure_buildable_ext", "ebpextensions\\structure_buildable_ext.lua"},
	{"structure_ext", "ebpextensions\\structure_ext.lua"},
	{"type_ext", "ebpextensions\\type_ext.lua"},
	{"ui_ext", "ebpextensions\\ui_ext.lua"},
}

func (ui *uiState) childrenTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.rgdFile == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "Select a .rgd file, then a node in the middle tree")
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		})
	}
	e := ui.rgdSelectedEntryFor(t)
	if e == nil {
		e = t.rgdFile.Root
	}
	if !e.IsTable() {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			val := e.DisplayFieldValue()
			if val == "" {
				val = "—"
			}
			msg := fmt.Sprintf("%s (%s) = %s — no children", e.Name, e.TypeLabel(), val)
			l := material.Label(ui.th, unit.Sp(11), msg)
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	children := e.Children
	if len(children) == 0 {
		if ref := e.RefString(); ref != "" {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), fmt.Sprintf("Empty table — Reference: %s", ref))
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), "No children")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutListWithScrollbar(gtx, &t.rgdChildScroll, &t.rgdChildList, 28, len(children), func(gtx layout.Context, index int) layout.Dimensions {
				ch := children[index]
				alt := index%2 == 1
				key := t.rgdSelected + "/" + ch.Name
				if ch.IsTable() {
					refEditor := ui.rgdEnsureValEditor(t, key+"/$ref", ch.RefString())
					return ui.tableRowEditor(gtx, ch.Name, refEditor, alt, "")
				}
				if ch.Type == rgd.RgdTypeBool {
					return ui.tableRowBool(gtx, ch.Name, t, ch, key, alt)
				}
				if ch.Type == rgd.RgdTypeString || ch.Type == rgd.RgdTypeWString {
					return ui.tableRowStringEditor(gtx, ch.Name, t, ch, key, alt)
				}
				if rgdIsEditableType(ch.Type) {
					ed := ui.rgdEnsureValEditor(t, key, ch.DisplayValue())
					return ui.tableRowEditor(gtx, ch.Name, ed, alt, t.rgdValErrors[key])
				}
				val := ch.DisplayFieldValue()
				if len(val) > 64 {
					val = val[:61] + "…"
				}
				return ui.tableRow2WithTooltip(gtx, ch.Name, val, alt, rgdTooltipForValue(ch))
			})
		}),
	)
}
