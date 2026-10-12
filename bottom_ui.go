package main

import (
	"dow-mod-studio/internal/modtree"
	"fmt"
	"image"
	"os"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (ui *uiState) layoutRightPanel(gtx layout.Context) layout.Dimensions {
	tab := ui.activeTab()
	// The object tab keeps this panel, but there is no open file behind it, so
	// it describes the selected unit instead of a file's properties.
	if isUnitTab(tab) {
		return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Object") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.unitInfoTable(gtx) }),
			}
			if v := ui.unitModelViewer(); v != nil && v.model != nil {
				children = append(children,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Model") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), ui.unitModelFileLabel())
						l.Color = cTextDim
						l.MaxLines = 2
						return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.unitModelFrameLabel(gtx, v) }),
				)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
	if tab != nil && tab.kind == "fda" {
		return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "FDA Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fdaPropertiesTable(gtx, tab) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "File Info") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fdaInfoTable(gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.kind == "wav" {
		return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "WAV Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.wavInfoTable(gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.kind == "whm" {
		return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Model Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.whmInfoTable(gtx, tab) }),
			}
			if tab.model != nil {
				children = append(children,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "3D Preview") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.modelInfoTable(gtx, tab) }),
				)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
	if tab != nil && tab.kind == "image" {
		return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Image Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.imageInfoTable(gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.kind == "rgd" {
		return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.propertiesTable(gtx, tab) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Table Children") }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.childrenTable(gtx, tab) }),
			)
		})
	}
	return ui.panelWithScroll(gtx, &ui.rightScroll, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Properties") }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.th, "Module tab — open a file to see its properties", unit.Sp(10), cTextDim)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Table Children") }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.th, "—", unit.Sp(10), cTextDim)
			}),
		)
	})
}

func sectionHeader(gtx layout.Context, th *material.Theme, title string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th, unit.Sp(11), title)
				l.Color = cText
				l.Font.Weight = font.Medium
				return l.Layout(gtx)
			})
		}),
	)
}

func (ui *uiState) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(36))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(12), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := ui.statusText
						if txt == "" {
							txt = "Ready"
						}
						l := material.Label(ui.th, unit.Sp(11), txt)
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
				)
			})
		}),
	)
}

func (ui *uiState) saveEligible() bool {
	t := ui.activeTab()
	if t == nil {
		return false
	}
	return (t.kind == "rgd" && t.rgdDirty) || (t.kind == "text" && t.textDirty)
}

func (ui *uiState) handleSaveActiveTab() {
	if t := ui.activeTab(); t != nil && t.kind == "rgd" {
		ui.handleRgdSave(t)
	} else if t := ui.activeTab(); t != nil && t.kind == "text" {
		ui.handleTextSave(t)
	} else if t := ui.activeTab(); t != nil && t.kind == "fda" {
		ui.fdaExportWav()
	} else if t := ui.activeTab(); t != nil && t.kind == "wav" {
		ui.convertWavToFDA(t)
	} else if t := ui.activeTab(); t != nil && t.kind == "whm" {
		ui.convertWhmToSgmFile(t)
	} else if ui.modTree == nil {
		ui.statusText = "Save — no mod loaded"
	} else if ui.activeTab() == nil {
		ui.statusText = "Save — open a .rgd, .txt, .lua, .ai, .fda, .wav, or .whm file first"
	} else {
		ui.statusText = "Save — the active file type is not editable"
	}
}

// exportEligible reports whether the active file can be copied from its
// resolved source into the current mod's writable overlay. An SGA source is
// eligible even when it belongs to a required mod or the engine, provided the
// current mod's ModFolder does not already contain the file on disk.
func (ui *uiState) exportEligible(tab *fileTab) (bool, string) {
	if ui.modTree == nil {
		return false, "no mod loaded"
	}
	if tab == nil {
		return false, "no active file"
	}
	file := ui.modTree.FindFile(tab.path)
	if file == nil {
		return false, "active file is not in the merged mod tree"
	}
	if file.Source == nil || (!file.Source.IsSGA && file.Source.Kind != modtree.KindReqMod) {
		return false, "active file is not from an SGA or a parent mod"
	}
	disk, err := ui.modTree.OverlayPath(tab.path)
	if err != nil {
		return false, err.Error()
	}
	if _, err := os.Stat(disk); err == nil {
		return false, "file already exists in the current mod folder"
	} else if !os.IsNotExist(err) {
		return false, err.Error()
	}
	return true, ""
}

func (ui *uiState) handleExportActiveTab() {
	tab := ui.activeTab()
	eligible, reason := ui.exportEligible(tab)
	if !eligible {
		ui.statusText = "Export — " + reason
		return
	}
	data, err := ui.modTree.ReadFile(tab.path)
	if err != nil {
		ui.statusText = fmt.Sprintf("Export failed: %v", err)
		return
	}
	disk, err := ui.modTree.WriteOverlayFile(tab.path, data)
	if err != nil {
		ui.statusText = fmt.Sprintf("Export failed: %v", err)
		return
	}
	ui.statusText = fmt.Sprintf("Exported %s to %s", tab.path, disk)
}
