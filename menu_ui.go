package main

import (
	"dow-mod-studio/internal/modtree"
	"fmt"
	"image"
	"image/color"
	"path/filepath"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (ui *uiState) layoutMenuBar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(24))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, cMenuBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					var ch []layout.FlexChild
					for i, m := range menus {
						i, m := i, m
						ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.menuButton(gtx, i, m.name) }))
					}
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, ch...)
				}),
			)
		}),
		// The object tools and the search field are flush with the right edge
		// of the window. They are placed by measuring the group and offsetting
		// it by whatever is left, instead of by a flexible gap inside the same
		// Flex: Gio sizes a Flexible child from the space left after all the
		// Rigid ones, so a gap written before them does not push them along,
		// and the field ends up past the window edge.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			w := ui.layoutMenuRightGroup(gtx).Size.X
			call := macro.Stop()
			if w > 0 && w < gtx.Constraints.Max.X {
				defer op.Offset(image.Pt(gtx.Constraints.Max.X-w, 0)).Push(gtx.Ops).Pop()
			}
			call.Add(gtx.Ops)
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
	)
}

// layoutMenuRightGroup is the right hand end of the menu bar: the language
// picker, the object interface tools and the search field, side by side.
func (ui *uiState) layoutMenuRightGroup(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(ui.layoutLocaleSlot),
		layout.Rigid(ui.layoutUnitPanelSlot),
		layout.Rigid(ui.layoutSearchSlot),
	)
}

func (ui *uiState) menuButton(gtx layout.Context, idx int, label string) layout.Dimensions {
	open := ui.menuOpen == idx
	hover := ui.menuClicks[idx].Hovered()
	bg := cMenuBg
	if open {
		bg = cMenuOpen
	} else if hover {
		bg = cMenuHover
	}
	border := color.NRGBA{}
	hasBorder := open || hover
	if open {
		border = cBorder
	} else if hover {
		border = rgba(0x99, 0xCC, 0xFF, 0xFF)
	}
	return ui.menuClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		lblDims := layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(13), label)
			l.Color = cText
			return l.Layout(gtx)
		})
		call := macro.Stop()
		sz := lblDims.Size
		if open || hover {
			fillRect(gtx, bg, sz)
			if hasBorder {
				b := widget.Border{Color: border, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
			}
		}
		call.Add(gtx.Ops)
		return lblDims
	})
}

func recentModulesHeaderIndex() int {
	for i, item := range menus[0].items {
		if item == "Recent Modules" {
			return i
		}
	}
	return -1
}

func emptyRecycleBinMenuLabel() string {
	return fmt.Sprintf("Empty Recycle Bin (%d)", recycleFileCount())
}

func isEmptyRecycleBinMenuLabel(label string) bool {
	const prefix = "Empty Recycle Bin"
	return label == prefix || (len(label) > len(prefix) && label[:len(prefix)] == prefix && label[len(prefix)] == ' ')
}

func (ui *uiState) menuItemsForOpen() []string {
	if ui.menuOpen != 0 {
		return menus[ui.menuOpen].items
	}
	items := append([]string(nil), menus[0].items...)
	for i, item := range items {
		if item == "Empty Recycle Bin" {
			items[i] = emptyRecycleBinMenuLabel()
		}
	}
	if !ui.recentMenuExpanded || len(ui.recentModules) == 0 {
		return items
	}
	header := recentModulesHeaderIndex()
	if header < 0 {
		return items
	}
	tail := append([]string(nil), items[header+1:]...)
	items = append(items[:header+1], "—")
	for _, modulePath := range ui.recentModules {
		items = append(items, filepath.Base(modulePath))
	}
	return append(items, tail...)
}

func (ui *uiState) ensureMenuItemClicks(menuIndex, n int) {
	if menuIndex < 0 || menuIndex >= len(ui.menuItemClicks) {
		return
	}
	if len(ui.menuItemClicks[menuIndex]) < n {
		old := ui.menuItemClicks[menuIndex]
		clicks := make([]widget.Clickable, n)
		copy(clicks, old)
		ui.menuItemClicks[menuIndex] = clicks
	} else if len(ui.menuItemClicks[menuIndex]) > n {
		ui.menuItemClicks[menuIndex] = ui.menuItemClicks[menuIndex][:n]
	}
}

func (ui *uiState) layoutMenuDropdown(gtx layout.Context) layout.Dimensions {
	menuBarH := gtx.Dp(unit.Dp(24))
	xOff := gtx.Dp(unit.Dp(6))
	for i := 0; i < ui.menuOpen; i++ {
		w := len(menus[i].name)*7 + 20
		xOff += gtx.Dp(unit.Dp(w))
		xOff += gtx.Dp(unit.Dp(2))
	}
	items := ui.menuItemsForOpen()
	ui.ensureMenuItemClicks(ui.menuOpen, len(items))
	itemH := gtx.Dp(unit.Dp(22))
	ddW := gtx.Dp(unit.Dp(220))
	ddH := len(items)*itemH + gtx.Dp(unit.Dp(6))
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return ui.dismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} })
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx2 := gtx
			defer op.Offset(image.Pt(xOff, menuBarH)).Push(gtx.Ops).Pop()
			sz := image.Pt(ddW, ddH)
			shadowOff := gtx.Dp(unit.Dp(2))
			shadowSz := image.Pt(ddW+shadowOff, ddH+shadowOff)
			st := clip.Rect{Max: shadowSz}.Push(gtx2.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 28)}.Add(gtx2.Ops)
			paint.PaintOp{}.Add(gtx2.Ops)
			st.Pop()
			st2 := clip.Rect{Max: sz}.Push(gtx2.Ops)
			paint.ColorOp{Color: cPanelBg}.Add(gtx2.Ops)
			paint.PaintOp{}.Add(gtx2.Ops)
			st2.Pop()
			b := widget.Border{Color: cBorder, Width: unit.Dp(1)}
			gtx2.Constraints.Min = sz
			gtx2.Constraints.Max = sz
			return b.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var ch []layout.FlexChild
					for idx, it := range items {
						idx, it := idx, it
						if it == "—" {
							ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									sz2 := image.Pt(gtx.Constraints.Max.X, 1)
									gtx.Constraints.Min.Y = 1
									gtx.Constraints.Max.Y = 1
									st := clip.Rect{Max: sz2}.Push(gtx.Ops)
									paint.ColorOp{Color: cHeaderLine}.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									st.Pop()
									return layout.Dimensions{Size: sz2}
								})
							}))
						} else {
							ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.menuItem(gtx, idx, it) }))
						}
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
				})
			})
		}),
	)
}

func (ui *uiState) menuItem(gtx layout.Context, idx int, label string) layout.Dimensions {
	click := &ui.menuItemClicks[ui.menuOpen][idx]
	hover := click.Hovered()
	bg := cPanelBg
	if hover {
		bg = cMenuHover
	}
	h := gtx.Dp(unit.Dp(22))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		inner := layout.Inset{Left: unit.Dp(28), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(12), label)
			l.Color = cText
			return l.Layout(gtx)
		})
		call := macro.Stop()
		sz := image.Pt(gtx.Constraints.Max.X, h)
		st := clip.Rect{Max: sz}.Push(gtx.Ops)
		paint.ColorOp{Color: bg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		call.Add(gtx.Ops)
		inner.Size.Y = h
		inner.Size.X = sz.X
		return inner
	})
}

func (ui *uiState) layoutSettingsDialog(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 90)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(360))
				cardH := gtx.Dp(unit.Dp(300))
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = cardW, cardW
				gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = cardH, cardH
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(18), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(14), "Settings")
										l.Font.Weight = font.Medium
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(11), "Dark theme")
												l.Color = cText
												return l.Layout(gtx)
											}),
											layout.Flexed(1, rowGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												onBg := cMenuOpen
												return ui.toggleButton(gtx, &ui.settingsThemeClick, "Enabled", ui.darkTheme, onBg)
											}),
										)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(11), "Game folder")
												l.Color = cText
												return l.Layout(gtx)
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												h := gtx.Dp(unit.Dp(28))
												gtx.Constraints.Min.Y = h
												gtx.Constraints.Max.Y = h
												return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														ed := material.Editor(ui.th, &ui.settingsGameEditor, "")
														ed.TextSize = unit.Sp(11)
														ed.HintColor = cTextDim
														return ed.Layout(gtx)
													})
												})
											}),
										)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(11), "Text editor formats")
												l.Color = cText
												return l.Layout(gtx)
											}),
											layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												h := gtx.Dp(unit.Dp(28))
												gtx.Constraints.Min.Y = h
												gtx.Constraints.Max.Y = h
												return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														ed := material.Editor(ui.th, &ui.settingsFormatEditor, "")
														ed.TextSize = unit.Sp(11)
														ed.HintColor = cTextDim
														return ed.Layout(gtx)
													})
												})
											}),
											layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(10), "Opened by one click or Enter, separated by commas or spaces")
												l.Color = cTextDim
												return l.Layout(gtx)
											}),
										)
									}),
									layout.Flexed(1, flexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &ui.settingsCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.dialogButton(gtx, &ui.settingsOk, "OK", true) }),
										)
									}),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

func (ui *uiState) layoutClearRecycleDialog(gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 110)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(380))
				cardH := gtx.Dp(unit.Dp(164))
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(14), "Empty Recycle Bin")
										l.Font.Weight = font.Medium
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(11), "This action is irreversible. All files in the Recycle Bin will be permanently deleted.")
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, flexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &ui.clearRecycleCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &ui.clearRecycleConfirm, "Delete", true)
											}),
										)
									}),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

func (ui *uiState) layoutFileDeleteDialog(gtx layout.Context) layout.Dimensions {
	_, name := modtree.VirtualDirAndBase(ui.fileDeletePath)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 110)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(400))
				cardH := gtx.Dp(unit.Dp(164))
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(14), "Delete File")
										l.Font.Weight = font.Medium
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(11), fmt.Sprintf("Move %q to the Recycle Bin?", name))
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, flexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &ui.fileDeleteCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.dialogButton(gtx, &ui.fileDeleteConfirm, "Delete", true)
											}),
										)
									}),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

func (ui *uiState) dialogButton(gtx layout.Context, btn *widget.Clickable, label string, primary bool) layout.Dimensions {
	// Keep action buttons compact. Without explicit constraints, the Stack
	// background inherits the dialog's available width and height.
	w := gtx.Dp(unit.Dp(88))
	h := gtx.Dp(unit.Dp(32))
	gtx.Constraints.Min.X = w
	gtx.Constraints.Max.X = w
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h

	hover := btn.Hovered()
	pressed := btn.Pressed()
	bg := cSaveBg
	bd := cSaveBd
	if primary {
		bg = rgb(0x0078D4)
		if pressed {
			bg = rgb(0x005A9E)
		} else if hover {
			bg = rgb(0x106EBE)
		}
	} else if pressed {
		bg = cSaveBg
	} else if hover {
		bg = cSaveBg
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: bd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Max
					rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(11), label)
						if primary {
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
