package main

import (
	"dow-mod-studio/internal/modtree"
	"image"
	"path/filepath"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (ui *uiState) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			fillRect(gtx, cContentBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				total := gtx.Constraints.Max.X
				ui.ensurePanelWidths(total)
				// The object interface takes the space of the file tree and the
				// middle panel; the right panel stays where it is, so the
				// usual properties panel is still beside it.
				if ui.unitTabOwnsLeftPanel() {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return ui.layoutUnitTabBody(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutPanelSplitter(gtx, 1, total) }),
						layout.Rigid(ui.fixedPanel(ui.rightPanelWidth, ui.layoutRightPanel)),
					)
				}
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(ui.fixedPanel(ui.leftPanelWidth, ui.layoutLeftPanel)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutPanelSplitter(gtx, 0, total) }),
					layout.Flexed(1, ui.layoutMiddlePanel),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.layoutPanelSplitter(gtx, 1, total) }),
					layout.Rigid(ui.fixedPanel(ui.rightPanelWidth, ui.layoutRightPanel)),
				)
			})
		}),
	)
}

// flexGap is a flexible gap inside a Flex row or column, used to push the
// children after it to the far end of the line.
//
// It has to report the size it was given. Gio measures a Flexed child with
// exact constraints for its share of the free space, so a widget that returns
// an empty Dimensions collapses to zero and everything after it slides back
// against the preceding children instead of being pushed away.
func flexGap() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
}

// rowGap is the same spacer for a row of controls, with one difference: it
// takes the width left over on the line and none of the height. A row that
// uses flexGap is as tall as the space it was given, which for a row under a
// canvas means taking the canvas's height and leaving it nothing.
func rowGap() layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
	}
}

func panelFrame(gtx layout.Context, content layout.Widget) layout.Dimensions {
	b := widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				rrect(gtx, cPanelBg, sz, gtx.Dp(2))
				return layout.Dimensions{Size: sz}
			}),
			layout.Stacked(content),
		)
	})
}

func (ui *uiState) layoutLeftPanel(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		if !ui.contextMenuVisible {
			return ui.layoutLeftPanelContent(gtx)
		}
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(ui.layoutLeftPanelContent),
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				pass := pointer.PassOp{}.Push(gtx.Ops)
				dims := ui.contextDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
				pass.Pop()
				return dims
			}),
			layout.Stacked(ui.layoutFileContextMenu),
		)
	})
}

func (ui *uiState) layoutLeftPanelContent(gtx layout.Context) layout.Dimensions {
	if ui.modTree == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return centeredPanelLabel(gtx, ui.th, "No module loaded", unit.Sp(12), cTextDim)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return centeredPanelWidget(gtx, func(gtx layout.Context) layout.Dimensions {
								return material.Clickable(gtx, &widget.Clickable{}, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.th, unit.Sp(11), "File → Open Module…")
									l.Color = rgb(0x0066CC)
									return l.Layout(gtx)
								})
							})
						})
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						extra := ui.statusText
						if extra == "" {
							extra = "Pick a .module from Dawn of War Definitive Edition"
						}
						return centeredPanelLabel(gtx, ui.th, extra, unit.Sp(10), cTextDim)
					}),
				)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := image.Pt(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(22)))
					fillRect(gtx, cHeaderBg, sz)
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						modName := ui.modulePath
						if modName != "" {
							modName = filepath.Base(modName)
						} else {
							modName = "Data"
						}
						l := material.Label(ui.th, unit.Sp(10), modName)
						l.Color = cTextDim
						return l.Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						return ui.layoutListWithScrollbar(gtx, &ui.leftScroll, &ui.leftList, 22, len(ui.visibleNodes), func(gtx layout.Context, index int) layout.Dimensions {
							return ui.layoutTreeRow(gtx, index, ui.visibleNodes[index])
						})
					}),
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						if ui.leftTreePointerTag == nil {
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}
						// The one target of the tree that is not a row: it
						// carries the right press, which no click gesture sees.
						// Both wrappers are load bearing — the clip keeps the
						// area to the panel, and the PassOp keeps the rows
						// underneath it clickable.
						pass := pointer.PassOp{}.Push(gtx.Ops)
						area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
						event.Op(gtx.Ops, ui.leftTreePointerTag)
						area.Pop()
						pass.Pop()
						return layout.Dimensions{Size: gtx.Constraints.Max}
					}),
				)
			})
		}),
	)
}

func (ui *uiState) layoutFileContextMenu(gtx layout.Context) layout.Dimensions {
	if !ui.contextMenuVisible {
		return layout.Dimensions{}
	}
	menuW := gtx.Dp(unit.Dp(196))
	menuH := gtx.Dp(unit.Dp(86))
	y := 32 + ui.contextMenuAnchor*gtx.Dp(unit.Dp(19)) - ui.leftScroll.offset
	if y < 28 {
		y = 28
	}
	if maxY := gtx.Constraints.Max.Y - menuH - 4; y > maxY {
		y = maxY
	}
	if y < 0 {
		y = 0
	}
	gtx2 := gtx
	deleteDisabled := !ui.canDeleteCurrentModFile(ui.contextMenuPath)
	defer op.Offset(image.Pt(gtx.Dp(unit.Dp(18)), y)).Push(gtx2.Ops).Pop()
	gtx2.Constraints.Min = image.Pt(menuW, menuH)
	gtx2.Constraints.Max = image.Pt(menuW, menuH)
	return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(3))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							_, base := modtree.VirtualDirAndBase(ui.contextMenuPath)
							l := material.Label(ui.th, unit.Sp(11), base)
							l.Color = cText
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.contextDuplicate, "Duplicate")
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.contextDelete, "Delete", deleteDisabled)
						}),
					)
				})
			}),
		)
	})
}
