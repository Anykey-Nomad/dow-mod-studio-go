package app

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"

	"dow-mod-studio/internal/modtree"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func centeredPanelLabel(gtx layout.Context, th *material.Theme, text string, size unit.Sp, col color.NRGBA) layout.Dimensions {
	width := gtx.Constraints.Max.X
	if width <= 0 {
		width = gtx.Constraints.Min.X
	}
	if width <= 0 {
		return layout.Dimensions{}
	}
	label := func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, size, text)
		l.Color = col
		return l.Layout(gtx)
	}
	macro := op.Record(gtx.Ops)
	measure := gtx
	measure.Constraints.Min = image.Point{}
	dims := label(measure)
	macro.Stop()
	x := (width - dims.Size.X) / 2
	if x < 0 {
		x = 0
	}
	defer op.Offset(image.Pt(x, 0)).Push(gtx.Ops).Pop()
	draw := gtx
	draw.Constraints.Min = image.Point{}
	label(draw)
	return layout.Dimensions{Size: image.Pt(width, dims.Size.Y)}
}

func centeredPanelWidget(gtx layout.Context, widget layout.Widget) layout.Dimensions {
	width := gtx.Constraints.Max.X
	height := gtx.Constraints.Max.Y
	if width <= 0 {
		width = gtx.Constraints.Min.X
	}
	if height <= 0 {
		height = gtx.Constraints.Min.Y
	}
	if width <= 0 || height <= 0 {
		return widget(gtx)
	}
	macro := op.Record(gtx.Ops)
	measure := gtx
	measure.Constraints.Min = image.Point{}
	dims := widget(measure)
	macro.Stop()
	x := (width - dims.Size.X) / 2
	y := (height - dims.Size.Y) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	draw := gtx
	draw.Constraints.Min = image.Point{}
	widget(draw)
	return layout.Dimensions{Size: image.Pt(width, height)}
}

func (ui *uiState) iconButton(gtx layout.Context, btn *widget.Clickable, glyph string, active bool) layout.Dimensions {
	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := cSaveBg
	bd := cSaveBd
	if active {
		bg = rgb(0x0078D4)
		bd = rgb(0x005A9E)
		if pressed {
			bg = rgb(0x005A9E)
		} else if hover {
			bg = rgb(0x106EBE)
		}
	} else if pressed {
		bg = cSaveBg
	} else if hover {
		bg = cSaveHover
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: bd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(13), glyph)
						if active {
							l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
						} else {
							l.Color = cText
						}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}

func (ui *uiState) miniButton(gtx layout.Context, btn *widget.Clickable, label string, on bool, bgOn color.NRGBA) layout.Dimensions {
	return ui.toggleButton(gtx, btn, label, on, bgOn)
}
func (ui *uiState) miniButtonDisabled(gtx layout.Context, btn *widget.Clickable, label string, disabled bool) layout.Dimensions {
	return ui.smallButtonDisabled(gtx, btn, label, disabled)
}

func (ui *uiState) smallButton(gtx layout.Context, btn *widget.Clickable, label string) layout.Dimensions {
	return ui.smallButtonDisabled(gtx, btn, label, false)
}
func (ui *uiState) smallButtonDisabled(gtx layout.Context, btn *widget.Clickable, label string, disabled bool) layout.Dimensions {
	hover := btn.Hovered() && !disabled
	pressed := btn.Pressed() && !disabled
	bg := cSaveBg
	if disabled {
		bg = cSaveBg
	} else if pressed {
		bg = cMenuOpen
	} else if hover {
		bg = cSaveHover
	}
	if disabled {
		gtx = gtx.Disabled()
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: cSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						if disabled {
							l.Color = cTextDim
						} else {
							l.Color = cText
						}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}
func (ui *uiState) primaryButton(gtx layout.Context, btn *widget.Clickable, label string) layout.Dimensions {
	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := rgb(0x0078D4)
	if pressed {
		bg = rgb(0x005A9E)
	} else if hover {
		bg = rgb(0x106EBE)
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: rgb(0x005A9E), Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}
func (ui *uiState) toggleButton(gtx layout.Context, btn *widget.Clickable, label string, on bool, bgOn color.NRGBA) layout.Dimensions {
	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := cSaveBg
	if on {
		bg = bgOn
	}
	if pressed {
		bg = cMenuOpen
	} else if hover {
		bg = cSaveHover
		if on {
			bg = cSelectBg
		}
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: cSaveBd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Min
					if sz.X == 0 || sz.Y == 0 {
						sz = gtx.Constraints.Max
					}
					if sz.X > gtx.Constraints.Max.X {
						sz.X = gtx.Constraints.Max.X
					}
					if sz.Y > gtx.Constraints.Max.Y {
						sz.Y = gtx.Constraints.Max.Y
					}
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}

// ─── merged from bottom_ui.go ───────────────────────────────────────────────

func (ui *uiState) layoutRightPanel(gtx layout.Context) layout.Dimensions {
	tab := ui.activeTab()
	if tab != nil && tab.kind == "sgb" {
		return ui.layoutSgbObjectPanel(gtx, tab)
	}
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
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, "Materials by Mesh") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.modelMaterialDiagnosticsTable(gtx, tab) }),
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
	if t.kind == "sgb" {
		return t.sgbDirty
	}
	return (t.kind == "rgd" && t.rgdDirty) || (t.kind == "text" && t.textDirty)
}

func (ui *uiState) handleSaveActiveTab() {
	if t := ui.activeTab(); t != nil && t.kind == "sgb" {
		ui.handleSgbSave(t)
	} else if t := ui.activeTab(); t != nil && t.kind == "rgd" {
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
		ui.statusText = "Save — open an editable file first"
	} else {
		ui.statusText = "Save — the active file type is not editable"
	}
}

func (ui *uiState) handleSgbSave(t *fileTab) {
	if t == nil || t.kind != "sgb" || t.sgbMap == nil {
		ui.statusText = "Save failed: no editable scenario is open"
		return
	}
	if ui.modTree == nil {
		ui.statusText = "Save failed: no mod loaded"
		return
	}
	if !t.sgbDirty {
		ui.statusText = filepath.Base(t.path) + " — no map changes"
		return
	}
	data, err := t.sgbMap.MarshalBinary()
	if err != nil {
		ui.statusText = fmt.Sprintf("Save failed: %v", err)
		return
	}
	diskPath, err := ui.modTree.WriteOverlayFile(t.path, data)
	if err != nil {
		ui.statusText = fmt.Sprintf("Save failed: %v", err)
		return
	}
	t.sgbBytes = len(data)
	t.sgbDirty = false
	ui.statusText = fmt.Sprintf("Saved map %s → %s (%d bytes)", filepath.Base(t.path), diskPath, len(data))
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

// ─── merged from table_ui.go ───────────────────────────────────────────────

func (ui *uiState) tableHead(gtx layout.Context, a, b string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(20))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cHeaderBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), a)
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, cHeaderLine) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), b)
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

func vLine(gtx layout.Context, col color.NRGBA) layout.Dimensions {
	w := gtx.Dp(unit.Dp(1))
	h := gtx.Dp(unit.Dp(20))
	sz := image.Pt(w, h)
	gtx.Constraints.Min.X = w
	gtx.Constraints.Max.X = w
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

func (ui *uiState) tableRow2(gtx layout.Context, a, b string, alt bool) layout.Dimensions {
	return ui.tableRow2WithTooltip(gtx, a, b, alt, "")
}

func (ui *uiState) tableRow2WithTooltip(gtx layout.Context, a, b string, alt bool, tooltip string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(24))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := cPanelBg
	if alt {
		bg = cSaveBg
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, bg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if tooltip == "" {
				return layout.Dimensions{}
			}
			if ui.rgdTooltipTags == nil {
				ui.rgdTooltipTags = make(map[string]event.Tag)
			}
			tag, ok := ui.rgdTooltipTags[tooltip]
			if !ok {
				tag = newPointerTag(tooltip)
				ui.rgdTooltipTags[tooltip] = tag
			}
			event.Op(gtx.Ops, tag)
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Enter | pointer.Leave})
				if !ok {
					break
				}
				e, ok := ev.(pointer.Event)
				if !ok {
					continue
				}
				switch e.Kind {
				case pointer.Enter:
					ui.rgdTooltipText = tooltip
				case pointer.Leave:
					ui.rgdTooltipText = ""
				}
			}
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), a)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, cBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), b)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
			)
		}),
	)
}

// ─── merged from tabs_ui.go ───────────────────────────────────────────────

// tabBarHeightDp and menuBarHeightDp are the two fixed bands above the tab bar.
// The tab context menu is drawn in the window's own coordinates, so it has to
// know where the bar starts; keeping the two heights named here is what stops
// that from drifting when either band changes.
const (
	tabBarHeightDp   = 28
	menuBarHeightDp  = 24
	tabContextWDP    = 170
	tabContextRowDp  = 26
	tabContextPadDp  = 6
	tabContextInsetP = 8
)

// tabWheelStepPx is how many pixels of the row one unit of the wheel's delta
// scrolls it by. A notch is wheelNotchDelta, so this is a little under two tabs
// a click: a row of tabs is easy to lose track of, and a wheel that jumped a
// dozen of them at once could not be aimed at one.
const tabWheelStepPx = 0.9

// tabBarTop is where the tab bar starts in the window: the menu bar and the one
// pixel line under it.
func tabBarTop(gtx layout.Context) int {
	return gtx.Dp(unit.Dp(menuBarHeightDp)) + 1
}

func (ui *uiState) layoutTabBar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(tabBarHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	ui.ensureTabClicks()

	// The offset is clamped against the width measured on the previous frame.
	// The row cannot be measured earlier in this frame instead: it holds the
	// tab clickables, and both ways of looking at it twice go wrong — laying it
	// out again registers their areas twice, and replaying a recorded copy of
	// the ops moves the frame's input handlers around after they were placed.
	// One frame of lag on the clamp is invisible; a misplaced handler is not.
	ui.tabBarWidth = gtx.Constraints.Max.X
	ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cTabBarBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			// The row is emitted once, with the scroll already in force, so the
			// tab clickables' handlers are registered once and in the right
			// place. When the bar is scrolled the row is wider than the bar, so
			// it is clipped to the bar and slid left; the bar's own background
			// covers whatever is past the end.
			if ui.tabScrollX != 0 {
				st := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Push(gtx.Ops)
				off := op.Offset(image.Pt(-ui.tabScrollX, 0)).Push(gtx.Ops)
				ui.layoutTabRow(gtx)
				off.Pop()
				st.Pop()
			} else {
				ui.layoutTabRow(gtx)
			}
			// The row's real width is known now, for the next frame's clamp.
			ui.tabRowWidth = ui.tabRowWidthPx
			// The bar's own pointer target goes in after the row, not before
			// it. Gio's hit test walks a frame's input stack from the top and
			// stops descending at the first target it meets, so a target
			// registered before the tabs is never reached with the pointer
			// over one of them: the wheel and the right press would do nothing
			// on a tab. Being last, and inside a PassOp, it sees the press
			// first and the tabs underneath still see it after it.
			ui.registerTabBarPointer(gtx)
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
	)
}

// registerTabBarPointer covers the bar with one pointer target. It is pushed
// inside a PassOp, so the wheel and the right press reach it while the tabs
// underneath keep their own clicks — the same arrangement the file tree uses
// for its scroll area. It must be called after the tabs are laid out; see
// layoutTabBar for why the order matters.
func (ui *uiState) registerTabBarPointer(gtx layout.Context) {
	if ui.tabBarPointerTag == nil {
		return
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(tabBarHeightDp)))}.Push(gtx.Ops)
	event.Op(gtx.Ops, ui.tabBarPointerTag)
	area.Pop()
	pass.Pop()
}

// layoutTabRow is the tabs themselves, at their natural width, which is wider
// than the bar as soon as there are enough of them. It records each tab's
// rectangle and leaves the row's total width in tabRowWidthPx, because the bar
// needs both after the row has been drawn and the bar's own size is not
// something the row can be measured against beforehand.
//
// Each tab is laid out under its own offset, which is what puts it where the
// recorded rectangle says it is. Without it every tab is drawn and hit-tested
// at the row's left edge: they cover one another, only the last one is under
// the pointer, and the row looks like a single tab with pieces of the others
// sticking out from behind it.
func (ui *uiState) layoutTabRow(gtx layout.Context) layout.Dimensions {
	x := gtx.Dp(unit.Dp(6))
	for idx, t := range ui.openTabs {
		idx, t := idx, t
		x += gtx.Dp(unit.Dp(2))
		off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		tabW := ui.tab(gtx, idx+1, tabLabelFor(t), idx).Size.X
		off.Pop()
		if len(ui.tabRects) <= idx {
			ui.tabRects = append(ui.tabRects, image.Rectangle{})
		}
		// The rects are recorded in the bar's own coordinates, which is the
		// space a pointer event on the bar is reported in, so a press can be
		// matched against them without translating anything.
		ui.tabRects[idx] = image.Rectangle{
			Min: image.Pt(x, 0),
			Max: image.Pt(x+tabW, gtx.Dp(unit.Dp(tabBarHeightDp))),
		}
		x += tabW
	}
	ui.tabRects = ui.tabRects[:len(ui.openTabs)]
	ui.tabRowWidthPx = x
	return layout.Dimensions{Size: image.Pt(x, gtx.Dp(unit.Dp(tabBarHeightDp)))}
}

// tabLabelFor is what a tab says. It is apart from the layout because both the
// bar and the tab's own tooltip want it.
func tabLabelFor(t *fileTab) string {
	if t == nil {
		return ""
	}
	if isUnitTab(t) {
		return unitTabLabel
	}
	switch t.kind {
	case "fda":
		return fmt.Sprintf("FDA [%s]", filepath.Base(t.path))
	case "wav":
		return fmt.Sprintf("WAV [%s]", filepath.Base(t.path))
	case "whm":
		return fmt.Sprintf("Model [%s]", filepath.Base(t.path))
	case "image":
		return fmt.Sprintf("Image [%s]", filepath.Base(t.path))
	case "sga":
		return fmt.Sprintf("SGA [%s]", filepath.Base(t.path))
	case "sgb":
		return fmt.Sprintf("Map [%s]", filepath.Base(t.path))
	case "rgd":
		return fmt.Sprintf("RGD [%s]", filepath.Base(t.path))
	}
	return filepath.Base(t.path)
}

// tabMaxScroll is how far the row can be slid: the part of it that does not
// fit. It is zero while the tabs do fit, which is what makes the bar scroll
// only when it has to.
func (ui *uiState) tabMaxScroll(barWidth, rowWidth int) int {
	if d := rowWidth - barWidth; d > 0 {
		return d
	}
	return 0
}

func (ui *uiState) clampTabScroll(barWidth, rowWidth int) {
	ui.tabScrollX = clampInt(ui.tabScrollX, 0, ui.tabMaxScroll(barWidth, rowWidth))
}

// tabAt is which tab a position in the bar lands on, or -1 for the gap beside
// the last one. The comparison is in floats because a pointer event reports
// fractional pixels, and rounding first would put the rightmost pixel column of
// a tab on the tab beside it.
func (ui *uiState) tabAt(p f32.Point) int {
	for i, r := range ui.tabRects {
		if p.X >= float32(r.Min.X) && p.X < float32(r.Max.X) &&
			p.Y >= float32(r.Min.Y) && p.Y < float32(r.Max.Y) {
			return i
		}
	}
	return -1
}

func (ui *uiState) tab(gtx layout.Context, idx int, label string, closeIdx int) layout.Dimensions {
	sel := ui.tabSel == idx
	bg := cTabOff
	closable := closeIdx >= 0
	if sel {
		bg = cTabOn
	} else if ui.tabClicks[idx].Hovered() {
		bg = cMenuHover
	}
	return ui.tabClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		inner := func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(2), Top: unit.Dp(5), Bottom: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(12), label)
						if sel {
							l.Color = cText
							l.Font.Weight = font.Medium
						} else {
							l.Color = cTextDim
						}
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !closable {
						return layout.Dimensions{}
					}
					return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.tabCloses[closeIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							hover := ui.tabCloses[closeIdx].Hovered()
							sz := gtx.Dp(unit.Dp(16))
							if hover {
								rrect(gtx, rgb(0xE81123), image.Pt(sz, sz), gtx.Dp(2))
							}
							l := material.Label(ui.th, unit.Sp(12), "×")
							if hover {
								l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
							} else {
								l.Color = cTextDim
							}
							return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, l.Layout)
						})
					})
				}),
			)
		}
		macro := op.Record(gtx.Ops)
		dims := inner(gtx)
		call := macro.Stop()
		sz := dims.Size
		rr := clip.RRect{Rect: image.Rectangle{Max: sz}, NE: gtx.Dp(4), NW: gtx.Dp(4)}.Push(gtx.Ops)
		paint.ColorOp{Color: bg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rr.Pop()
		st := clip.Rect(image.Rect(0, 0, sz.X, 1)).Push(gtx.Ops)
		paint.ColorOp{Color: cBorder}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		st = clip.Rect(image.Rect(0, 0, 1, sz.Y)).Push(gtx.Ops)
		paint.ColorOp{Color: cBorder}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		st = clip.Rect(image.Rect(sz.X-1, 0, sz.X, sz.Y)).Push(gtx.Ops)
		paint.ColorOp{Color: cBorder}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		if sel {
			st = clip.Rect(image.Rect(0, sz.Y-1, sz.X, sz.Y)).Push(gtx.Ops)
			paint.ColorOp{Color: bg}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
		}
		call.Add(gtx.Ops)
		return dims
	})
}

// ── tab context menu ───────────────────────────────────────────────────────

// openTabContextMenu puts the menu on a tab, at the point it was clicked. The
// tab is selected as well, so the menu belongs to the tab under the pointer
// even when it was not the active one.
func (ui *uiState) openTabContextMenu(idx int, at image.Point) {
	if idx < 0 || idx >= len(ui.openTabs) {
		return
	}
	ui.tabContextIndex = idx
	ui.tabContextPos = at
	ui.tabContextVisible = true
	// Selecting is not what the menu acts on yet, but a context menu on an
	// unselected tab is a trap: the reader has no way to tell which one it is
	// for, and the next item to be added will act on the selection.
	ui.tabSel = idx + 1
	ui.markStateDirty()
}

// closeTabContextMenu puts the menu away.
func (ui *uiState) closeTabContextMenu() {
	ui.tabContextVisible = false
	ui.tabContextIndex = -1
}

// tabContextItems are the menu's rows, in order. One for now; the list is here
// so the next one is a line rather than a second layout pass.
var tabContextItems = []string{"Close all tabs"}

// tabContextMenuOrigin is where the menu goes for a click at p in a window of
// winW by winH. It hangs below the click at the click's own x, then is turned
// back inside the window: to the left when there is no room to the right, and
// up when there is no room below. A menu that opened off screen would take its
// only item with it, which is the one thing it must never do.
//
// It is apart from the layout so the placement can be pinned for every corner —
// the layout only ever sees one of them at a time.
func tabContextMenuOrigin(p image.Point, winW, winH, menuW, menuH, inset int) image.Point {
	x, y := p.X, p.Y
	if x+menuW > winW-inset {
		// Flip to the left of the click rather than off the right edge.
		x = p.X - menuW
	}
	if x < inset {
		// Not enough room on either side, so sit against the left edge.
		x = inset
	}
	if y+menuH > winH-inset {
		y = winH - menuH - inset
	}
	if y < 0 {
		y = 0
	}
	return image.Pt(x, y)
}

// layoutTabContextMenu is the menu itself, drawn over the window. It is
// positioned in window coordinates — the bar's own top plus where the click
// landed — and nudged back inside the window, because a click near the right or
// bottom edge would otherwise open it off screen.
func (ui *uiState) layoutTabContextMenu(gtx layout.Context) layout.Dimensions {
	if !ui.tabContextVisible {
		return layout.Dimensions{}
	}
	menuW := gtx.Dp(unit.Dp(tabContextWDP))
	pad := gtx.Dp(unit.Dp(tabContextPadDp))
	rowH := gtx.Dp(unit.Dp(tabContextRowDp))
	menuH := pad*2 + rowH*len(tabContextItems)
	inset := gtx.Dp(unit.Dp(tabContextInsetP))
	origin := tabContextMenuOrigin(ui.tabContextPos, gtx.Constraints.Max.X, gtx.Constraints.Max.Y, menuW, menuH, inset)

	gtx2 := gtx
	defer op.Offset(origin).Push(gtx2.Ops).Pop()
	gtx2.Constraints.Min = image.Pt(menuW, menuH)
	gtx2.Constraints.Max = image.Pt(menuW, menuH)
	// A shadow, so the menu reads as sitting above the tab bar rather than
	// being printed on it.
	shadow := gtx.Dp(unit.Dp(2))
	st := clip.Rect{Max: image.Pt(menuW+shadow, menuH+shadow)}.Push(gtx2.Ops)
	paint.ColorOp{Color: rgba(0, 0, 0, 28)}.Add(gtx2.Ops)
	paint.PaintOp{}.Add(gtx2.Ops)
	st.Pop()
	return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(3))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{
					Top: unit.Dp(tabContextPadDp), Bottom: unit.Dp(tabContextPadDp),
					Left: unit.Dp(tabContextInsetP), Right: unit.Dp(tabContextInsetP),
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.tabContextCloseAll, tabContextItems[0])
						}),
					)
				})
			}),
		)
	})
}
