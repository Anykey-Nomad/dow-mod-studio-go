package shell

import (
	"dow-mod-studio/internal/app/editors"
	"dow-mod-studio/internal/app/fileops"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/modtree"
	"fmt"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"image/color"
	"strings"
)

func MenuButton(ui *UiState, gtx layout.Context, idx int, label string) layout.Dimensions {
	open := ui.MenuOpen == idx
	hover := ui.MenuClicks[idx].Hovered()
	bg := uikit.CMenuBg
	if open {
		bg = uikit.CMenuOpen
	} else if hover {
		bg = uikit.CMenuHover
	}
	border := color.NRGBA{}
	hasBorder := open || hover
	if open {
		border = uikit.CBorder
	} else if hover {
		border = Rgba(0x99, 0xCC, 0xFF, 0xFF)
	}
	return ui.MenuClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		lblDims := layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(13), label)
			l.Color = uikit.CText
			return l.Layout(gtx)
		})
		call := macro.Stop()
		sz := lblDims.Size
		if open || hover {
			FillRect(gtx, bg, sz)
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

func RecentModulesHeaderIndex() int {
	for i, item := range Menus[0].Items {
		if item == "Recent Modules" {
			return i
		}
	}
	return -1
}

func IsEmptyRecycleBinMenuLabel(label string) bool {
	const prefix = "Empty Recycle Bin"
	return label == prefix || (len(label) > len(prefix) && label[:len(prefix)] == prefix && label[len(prefix)] == ' ')
}

func EnsureMenuItemClicks(ui *UiState, menuIndex, n int) {
	if menuIndex < 0 || menuIndex >= len(ui.MenuItemClicks) {
		return
	}
	if len(ui.MenuItemClicks[menuIndex]) < n {
		old := ui.MenuItemClicks[menuIndex]
		clicks := make([]widget.Clickable, n)
		copy(clicks, old)
		ui.MenuItemClicks[menuIndex] = clicks
	} else if len(ui.MenuItemClicks[menuIndex]) > n {
		ui.MenuItemClicks[menuIndex] = ui.MenuItemClicks[menuIndex][:n]
	}
}

func MenuItem(ui *UiState, gtx layout.Context, idx int, label string) layout.Dimensions {
	click := &ui.MenuItemClicks[ui.MenuOpen][idx]
	hover := click.Hovered()
	bg := uikit.CPanelBg
	if hover {
		bg = uikit.CMenuHover
	}
	h := gtx.Dp(unit.Dp(22))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		macro := op.Record(gtx.Ops)
		inner := layout.Inset{Left: unit.Dp(28), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(12), label)
			l.Color = uikit.CText
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

func LayoutSettingsDialog(ui *UiState, gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: Rgba(0, 0, 0, 90)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(360))
				cardH := gtx.Dp(unit.Dp(410))
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = cardW, cardW
				gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = cardH, cardH
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							Rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(18), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(14), "Settings")
										l.Font.Weight = font.Medium
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(11), "Dark theme")
												l.Color = uikit.CText
												return l.Layout(gtx)
											}),
											layout.Flexed(1, RowGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												onBg := uikit.CMenuOpen
												return ToggleButton(ui, gtx, &ui.SettingsThemeClick, "Enabled", ui.DarkTheme, onBg)
											}),
										)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(11), "Game folder")
												l.Color = uikit.CText
												return l.Layout(gtx)
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												h := gtx.Dp(unit.Dp(28))
												gtx.Constraints.Min.Y = h
												gtx.Constraints.Max.Y = h
												return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														ed := material.Editor(ui.Th, &ui.SettingsGameEditor, "")
														ed.TextSize = unit.Sp(11)
														ed.HintColor = uikit.CTextDim
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
												l := material.Label(ui.Th, unit.Sp(11), "Text editor formats")
												l.Color = uikit.CText
												return l.Layout(gtx)
											}),
											layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												h := gtx.Dp(unit.Dp(28))
												gtx.Constraints.Min.Y = h
												gtx.Constraints.Max.Y = h
												return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														ed := material.Editor(ui.Th, &ui.SettingsFormatEditor, "")
														ed.TextSize = unit.Sp(11)
														ed.HintColor = uikit.CTextDim
														return ed.Layout(gtx)
													})
												})
											}),
											layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(10), "Opened by one click or Enter, separated by commas or spaces")
												l.Color = uikit.CTextDim
												return l.Layout(gtx)
											}),
										)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutAssocRows(ui, gtx) }),
									layout.Flexed(1, FlexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.SettingsCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions { return DialogButton(ui, gtx, &ui.SettingsOk, "OK", true) }),
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

// layoutAssocRows is the File associations section of the Settings dialog:
// one row per format, whose button registers or removes that format's
// double-click association for the current user. The rows come from the same
// table the registration code uses, so a format added there appears here.
func LayoutAssocRows(ui *UiState, gtx layout.Context) layout.Dimensions {
	if !fileops.AssociationsSupported() || len(ui.AssocClicks) != len(fileops.AssocTypes) {
		return layout.Dimensions{}
	}
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "File associations (this user)")
			l.Color = uikit.CText
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
	}
	for i := range fileops.AssocTypes {
		i, t := i, fileops.AssocTypes[i]
		on := i < len(ui.AssocOn) && ui.AssocOn[i]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), t.Ext+" — "+t.Label)
					l.Color = uikit.CText
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Flexed(1, RowGap()),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// The same pill the Dark theme row uses: lit when the
					// format is registered, and the click adds or removes.
					return ToggleButton(ui, gtx, &ui.AssocClicks[i], "Associated", on, uikit.CMenuOpen)
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func LayoutClearRecycleDialog(ui *UiState, gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: Rgba(0, 0, 0, 110)}.Add(gtx.Ops)
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
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							Rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(14), "Empty Recycle Bin")
										l.Font.Weight = font.Medium
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(11), "This action is irreversible. All files in the Recycle Bin will be permanently deleted.")
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, FlexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.ClearRecycleCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.ClearRecycleConfirm, "Delete", true)
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

func LayoutFileDeleteDialog(ui *UiState, gtx layout.Context) layout.Dimensions {
	_, name := modtree.VirtualDirAndBase(ui.FileDeletePath)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: Rgba(0, 0, 0, 110)}.Add(gtx.Ops)
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
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							Rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(18), Bottom: unit.Dp(16), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(14), "Delete File")
										l.Font.Weight = font.Medium
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(11), fmt.Sprintf("Move %q to the Recycle Bin?", name))
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Flexed(1, FlexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.FileDeleteCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.FileDeleteConfirm, "Delete", true)
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

func DialogButton(ui *UiState, gtx layout.Context, btn *widget.Clickable, label string, primary bool) layout.Dimensions {
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
	bg := uikit.CSaveBg
	bd := uikit.CSaveBd
	if primary {
		bg = Rgb(0x0078D4)
		if pressed {
			bg = Rgb(0x005A9E)
		} else if hover {
			bg = Rgb(0x106EBE)
		}
	} else if pressed {
		bg = uikit.CSaveBg
	} else if hover {
		bg = uikit.CSaveBg
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return widget.Border{Color: bd, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Max
					Rrect(gtx, bg, sz, gtx.Dp(3))
					return layout.Dimensions{Size: sz}
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), label)
						if primary {
							l.Color = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
						} else {
							l.Color = uikit.CText
						}
						return l.Layout(gtx)
					})
				}),
			)
		})
	})
}

// Geometry of the search field in the menu bar. The field and the results
// panel share the same right margin, so the panel lines up under the field it
// belongs to.
const (
	SearchFieldWidthDp     = 240
	SearchFieldHeightDp    = 22
	SearchPanelWidthDp     = 420
	SearchRowHeightDp      = 34
	SearchPanelMarginDp    = 8
	SearchPanelTopDp       = 26
	SearchResultLimit      = 200
	SearchPanelMaxHeightDp = 460
	SearchPanelMinHeightDp = 140
)

// SearchSourceColor tints a result the way the tree tints its source, so a hit
// from the engine reads differently from one in the mod being edited.
func SearchSourceColor(f *modtree.TreeFile) color.NRGBA {
	if f == nil || f.Source == nil {
		return uikit.CTextDim
	}
	switch f.Source.Kind {
	case modtree.KindThisMod:
		return uikit.CTreeThis
	case modtree.KindReqMod:
		return uikit.CTreeOther
	}
	return uikit.CTreeEngine
}

// SearchFolderOf is the folder part of a virtual path, shown under the name.
func SearchFolderOf(f *modtree.TreeFile) string {
	if f == nil {
		return ""
	}
	return strings.TrimSuffix(f.FullPath, f.Name)
}

// runSearch rebuilds the result list when the query, the filter, or the module
// has changed. Walking the merged tree is cheap but not free, and it must not
// run on frames where nothing was typed.
func RunSearch(ui *UiState, force bool) {
	q := strings.TrimSpace(ui.SearchEditor.Text())
	kind := ui.SearchKind
	if !force && ui.SearchQuerySet && q == ui.LastSearchQuery && kind == ui.LastSearchKind {
		return
	}
	ui.LastSearchQuery = q
	ui.LastSearchKind = kind
	ui.SearchQuerySet = true
	ui.SearchSel = 0
	ui.SearchScroll.Offset = 0
	ui.SearchHits = nil
	if q == "" || ui.ModTree == nil {
		EnsureSearchClicks(ui, 0)
		ui.SearchStatus = ""
		return
	}
	ui.SearchHits = ui.ModTree.Search(q, kind, SearchResultLimit)
	EnsureSearchClicks(ui, len(ui.SearchHits))
	if len(ui.SearchHits) == 0 {
		ui.SearchStatus = fmt.Sprintf("No files match %q", q)
		return
	}
	if kind == modtree.SearchAll {
		ui.SearchStatus = fmt.Sprintf("%d files", len(ui.SearchHits))
	} else {
		ui.SearchStatus = fmt.Sprintf("%d files (%s)", len(ui.SearchHits), kind.Label())
	}
}

func EnsureSearchClicks(ui *UiState, n int) {
	if len(ui.SearchHitClicks) < n {
		old := ui.SearchHitClicks
		grown := make([]widget.Clickable, n)
		copy(grown, old)
		ui.SearchHitClicks = grown
	}
	if len(ui.SearchHover) < n {
		old := ui.SearchHover
		grown := make([]bool, n)
		copy(grown, old)
		ui.SearchHover = grown
	}
}

// revealInTree expands every folder on the way to a file and selects it, so a
// hit does not vanish from the tree the moment it is opened.
func RevealInTree(ui *UiState, fullPath string) {
	if ui.ModTree == nil || fullPath == "" {
		return
	}
	ui.SelectedPath = fullPath
	ui.SelectedTreePath = fullPath
	if ui.Expanded == nil {
		ui.Expanded = map[string]bool{}
	}
	ui.Expanded[ui.ModTree.Root.FullPath] = true
	segs := strings.Split(strings.ReplaceAll(fullPath, "/", "\\"), "\\")
	prefix := ""
	// Stop one segment short: the last segment is the file itself.
	for _, seg := range segs[:editors.MaxInt(0, len(segs)-1)] {
		if prefix == "" {
			prefix = seg
		} else {
			prefix += "\\" + seg
		}
		ui.Expanded[prefix] = true
	}
	ui.VisibleNodes = GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(ui.VisibleNodes))
	EnsureRowHover(ui, len(ui.VisibleNodes))
}

// handleSearchArrows walks the result list with the arrow keys. Gio's editor
// consumes an arrow only when it can act on it, so whatever reaches us here is
// meant for the list.
func HandleSearchArrows(ui *UiState, gtx layout.Context) {
	if len(ui.SearchHits) == 0 {
		return
	}
	rowH := gtx.Dp(unit.Dp(SearchRowHeightDp))
	for _, name := range []key.Name{key.NameDownArrow, key.NameUpArrow} {
		for {
			ev, ok := gtx.Event(key.Filter{Name: name})
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			if name == key.NameDownArrow {
				ui.SearchSel++
			} else {
				ui.SearchSel--
			}
			ui.SearchSel = ClampInt(ui.SearchSel, 0, len(ui.SearchHits)-1)
			// Keep the selected row inside the viewport, mirroring what the
			// list's own scrolling would have done.
			top := ui.SearchSel * rowH
			if top < ui.SearchScroll.Offset {
				ui.SearchScroll.Offset = top
			} else if top+rowH > ui.SearchScroll.Offset+ui.SearchScroll.Viewport {
				ui.SearchScroll.Offset = top + rowH - ui.SearchScroll.Viewport
			}
			ui.SearchScroll.Offset = ClampInt(ui.SearchScroll.Offset, 0, editors.MaxInt(0, ui.SearchScroll.ContentHeight-ui.SearchScroll.Viewport))
		}
	}
}

// searchPanelWanted decides whether the results panel belongs on screen, from
// the field's text and the query the panel was last dismissed at. Typing
// brings it back; re-showing the same text does not.
//
// It is a function of the text rather than a flag an editor event can clear on
// the way past, and that is the whole point. While the panel is up it lays a
// dismiss catcher over the content, so a panel that springs back open by
// itself swallows every click on the file tree with no sign of why: files stop
// opening and folders stop expanding while the panel looks perfectly normal.
func SearchPanelWanted(ui *UiState, text string) bool {
	query := strings.TrimSpace(text)
	if query == "" {
		// Nothing to hold. An empty field has no query to spring back from,
		// and dropping the hold here is what lets the same text be typed
		// again after Escape emptied the field.
		ui.SearchHeldQuery = ""
		return false
	}
	return query != ui.SearchHeldQuery
}

// closeSearch puts the results panel away and holds it down for as long as the
// field keeps the query it was showing. Every path that hides the panel goes
// through here — the dismiss catcher, Escape, opening a result, a menu, the
// language picker — so none of them can leave it to spring back on the next
// frame and cover the content with its dismiss catcher.
func CloseSearch(ui *UiState) {
	ui.SearchVisible = false
	ui.SearchSel = 0
	ui.SearchScroll.Offset = 0
	ui.SearchHeldQuery = strings.TrimSpace(ui.SearchEditor.Text())
}

// layoutSearchSlot places the field at the right end of the menu bar.
func LayoutSearchSlot(ui *UiState, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unit.Dp(SearchPanelMarginDp), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutSearchField(ui, gtx) })
}

// layoutSearchField is the search field in the menu bar. The editor lives in
// the field itself rather than in the results panel, so it is laid out on
// every frame and can always take the keyboard focus — an editor that only
// exists while a panel is open cannot be focused the frame the panel opens.
func LayoutSearchField(ui *UiState, gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Dp(unit.Dp(SearchFieldWidthDp)), gtx.Dp(unit.Dp(SearchFieldHeightDp)))
	gtx.Constraints.Min = size
	gtx.Constraints.Max = size
	hover := ui.SearchFieldClick.Hovered() || ui.SearchEditor.Focused(gtx)
	border := uikit.CBorder
	if hover {
		border = uikit.CSelectBd
	}
	return ui.SearchFieldClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				Rrect(gtx, uikit.CContentBg, size, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: size}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(11), "Search")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							ed := material.Editor(ui.Th, &ui.SearchEditor, "file name or folder")
							return ed.Layout(gtx)
						}),
					)
				})
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: border, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: size}
				})
			}),
		)
	})
}

// layoutSearchPanel draws the editor, the filter chips and the result list in a
// panel anchored under the menu bar's search field.
func LayoutSearchPanel(ui *UiState, gtx layout.Context) layout.Dimensions {
	w := gtx.Dp(unit.Dp(SearchPanelWidthDp))
	margin := gtx.Dp(unit.Dp(SearchPanelMarginDp))
	top := gtx.Dp(unit.Dp(SearchPanelTopDp))
	maxH := gtx.Constraints.Max.Y - top - margin
	if maxH < gtx.Dp(unit.Dp(SearchPanelMinHeightDp)) {
		maxH = gtx.Dp(unit.Dp(SearchPanelMinHeightDp))
	}
	if maxH > gtx.Dp(unit.Dp(SearchPanelMaxHeightDp)) {
		maxH = gtx.Dp(unit.Dp(SearchPanelMaxHeightDp))
	}
	// Sit at the right edge of the window, in line with the field above.
	x := gtx.Constraints.Max.X - w - margin
	if x < 0 {
		x = 0
	}
	area := clip.Rect{Min: image.Pt(x, top), Max: image.Pt(x+w, top+maxH)}.Push(gtx.Ops)
	defer area.Pop()
	inner := gtx
	inner.Constraints.Min = image.Pt(w, maxH)
	inner.Constraints.Max = image.Pt(w, maxH)

	return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(inner, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				Rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutSearchFilters(ui, gtx) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return HLine(gtx, uikit.CHeaderLine, 1) }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutSearchStatus(ui, gtx) }),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutSearchResults(ui, gtx) }),
					)
				})
			}),
		)
	})
}

func LayoutSearchFilters(ui *UiState, gtx layout.Context) layout.Dimensions {
	var children []layout.FlexChild
	for i := range modtree.SearchKinds {
		i, k := i, modtree.SearchKinds[i]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ToggleButton(ui, gtx, &ui.SearchKindClicks[i], k.Label(), ui.SearchKind == k, uikit.CSelectBg)
			})
		}))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

func LayoutSearchStatus(ui *UiState, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(10), ui.SearchStatus)
		l.Color = uikit.CTextDim
		l.MaxLines = 1
		return l.Layout(gtx)
	})
}

func LayoutSearchResults(ui *UiState, gtx layout.Context) layout.Dimensions {
	if len(ui.SearchHits) == 0 {
		return layout.Dimensions{}
	}
	EnsureSearchClicks(ui, len(ui.SearchHits))
	rowH := gtx.Dp(unit.Dp(SearchRowHeightDp))
	ui.SearchList.Axis = layout.Vertical
	return LayoutListWithScrollbar(ui, gtx, &ui.SearchScroll, &ui.SearchList, rowH, len(ui.SearchHits), func(gtx layout.Context, index int) layout.Dimensions {
		return LayoutSearchRow(ui, gtx, index, ui.SearchHits[index])
	})
}

// layoutSearchRow draws one hit: the file name with the matched word picked out,
// and the folder it lives in underneath.
func LayoutSearchRow(ui *UiState, gtx layout.Context, index int, hit modtree.SearchHit) layout.Dimensions {
	f := hit.File
	if f == nil || index >= len(ui.SearchHitClicks) {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	selected := index == ui.SearchSel
	hover := index < len(ui.SearchHover) && ui.SearchHover[index]
	if selected || hover {
		bg := uikit.CMenuHover
		if selected {
			bg = uikit.CSelectBg
		}
		rect := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
		Rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
		rect.Pop()
	}
	return ui.SearchHitClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutSearchName(ui, gtx, f.Name, hit)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LayoutSearchPath(ui, gtx, f)
				}),
			)
		})
	})
}

// layoutSearchName splits the file name around the matched word so the word
// stands out, which is why a row's first line is a flex of three labels.
func LayoutSearchName(ui *UiState, gtx layout.Context, name string, hit modtree.SearchHit) layout.Dimensions {
	word := hit.MatchWord
	at := -1
	if word != "" {
		if i := strings.Index(strings.ToLower(name), word); i >= 0 {
			at = i
		}
	}
	if at < 0 {
		return SearchNameLabel(ui, gtx, name, uikit.CText)
	}
	before := name[:at]
	matched := name[at : at+len(word)]
	after := name[at+len(word):]
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SearchNameLabel(ui, gtx, before, uikit.CText)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SearchNameLabel(ui, gtx, matched, SearchSourceColor(hit.File))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SearchNameLabel(ui, gtx, after, uikit.CText)
		}),
	)
}

func SearchNameLabel(ui *UiState, gtx layout.Context, text string, col color.NRGBA) layout.Dimensions {
	l := material.Label(ui.Th, unit.Sp(11), text)
	l.Color = col
	l.MaxLines = 1
	return l.Layout(gtx)
}

func LayoutSearchPath(ui *UiState, gtx layout.Context, f *modtree.TreeFile) layout.Dimensions {
	modName := ""
	if f.Source != nil {
		modName = f.Source.ModName
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), SearchFolderOf(f))
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), modName)
			l.Color = SearchSourceColor(f)
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// layoutSearchOverlay is the top-level layer: a click catcher so a click
// outside dismisses the search, with the panel stacked on top of it. The
// catcher starts below the menu bar, otherwise it would swallow clicks on the
// search field itself and the field could never be used again after a dismiss.
func LayoutSearchOverlay(ui *UiState, gtx layout.Context) layout.Dimensions {
	if !ui.SearchVisible {
		return layout.Dimensions{}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			top := gtx.Dp(unit.Dp(SearchPanelTopDp))
			below := clip.Rect{Min: image.Pt(0, top), Max: gtx.Constraints.Max}.Push(gtx.Ops)
			dims := ui.SearchDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			below.Pop()
			return dims
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return LayoutSearchPanel(ui, gtx) }),
	)
}

// searchResultPaths lists the current hits as virtual paths, for tests.
func SearchResultPaths(ui *UiState) []string {
	out := make([]string, 0, len(ui.SearchHits))
	for _, hit := range ui.SearchHits {
		if hit.File != nil {
			out = append(out, hit.File.FullPath)
		}
	}
	return out
}
