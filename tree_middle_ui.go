package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"image"
	"image/color"
	"path/filepath"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (ui *uiState) layoutTreeRow(gtx layout.Context, idx int, n modtree.VisibleNode) layout.Dimensions {
	selected := n.FullPath == ui.selectedTreePath
	contextSelected := ui.contextMenuVisible && n.IsFile && n.FullPath == ui.contextMenuPath
	if n.IsFile && n.FullPath == ui.selectedPath {
		selected = true
	}
	expandable := n.IsDir && n.HasKids
	open := ui.expanded[n.FullPath]
	h := gtx.Dp(unit.Dp(19))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	ui.leftRowHeightPx = h
	ui.registerTreeRowClick(gtx, idx, h)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if contextSelected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, cMenuOpen, sz)
				b := widget.Border{Color: cMenuHover, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				return layout.Dimensions{Size: sz}
			}
			if selected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, cSelectBg, sz)
				bd := cSelectBd
				if ui.treeHasKeyboard() {
					// The row the keys act on, which is worth telling apart
					// from a row that is merely selected: the middle tree can
					// hold the keyboard while this row stays selected.
					bd = cFocusBd
				}
				b := widget.Border{Color: bd, Width: unit.Dp(1)}
				gtx.Constraints.Min = sz
				gtx.Constraints.Max = sz
				_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				return layout.Dimensions{Size: sz}
			}
			if ui.rowHover[idx] && !selected {
				sz := image.Pt(gtx.Constraints.Max.X, h)
				fillRect(gtx, cMenuHover, sz)
				return layout.Dimensions{Size: sz}
			}
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			indentW := gtx.Dp(unit.Dp(float32(n.Depth)*14 + 4))
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: unit.Dp(float32(indentW))}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !expandable {
						return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
					}
					sz := gtx.Dp(unit.Dp(9))
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					b := widget.Border{Color: rgb(0x808080), Width: unit.Dp(1)}
					gtx.Constraints.Min = box
					gtx.Constraints.Max = box
					return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							s := "-"
							if !open {
								s = "+"
							}
							l := material.Label(ui.th, unit.Sp(9), s)
							l.Color = cText
							return l.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(unit.Dp(12))
					var col color.NRGBA
					if n.IsDir {
						col = rgb(0xFFD54F)
					} else {
						if hasSuffix(n.Name, ".rgd") {
							col = rgb(0x9DB7D0)
						} else if hasSuffix(n.Name, ".fda") {
							col = rgb(0xE57373)
						} else if hasSuffix(n.Name, ".lua") {
							col = rgb(0x6BB5A0)
						} else if hasSuffix(n.Name, ".tga") || hasSuffix(n.Name, ".dds") {
							col = rgb(0xD07DBB)
						} else {
							col = rgb(0xC8C8C8)
						}
					}
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: col}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					return layout.Dimensions{Size: box}
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := n.Name
					if len(lbl) > 38 && n.IsFile {
						lbl = lbl[:35] + "…"
					}
					l := material.Label(ui.th, unit.Sp(11), lbl)
					if selected {
						l.Color = cText
						l.Font.Weight = font.Medium
					} else if n.IsFile {
						switch n.Kind {
						case modtree.KindThisMod:
							l.Color = cTreeThis
						case modtree.KindEngine:
							l.Color = cTreeEngine
						default:
							l.Color = cTreeOther
						}
					} else {
						l.Color = cText
					}
					if n.IsDir {
						l.Font.Weight = font.Medium
					}
					return l.Layout(gtx)
				}),
			)
		}))
}

// registerTreeRowClick gives the row its own pointer area and its own click
// gesture over it. One area per row, rather than one area over the whole list
// with the row worked out from the position afterwards, is what makes a click
// a property of a row: the release has to land on the row the press did, and
// Gio says so without the app keeping any of it.
//
// The clip is what makes the area the row and not the window, and it is the
// part that matters: without it every row is hit by every position and the last
// one laid out answers for all of them. The PassOp says the row is stacked over
// the targets below it rather than in place of them — the list's own wheel
// target and the tree's file menu are both registered underneath the rows.
func (ui *uiState) registerTreeRowClick(gtx layout.Context, idx, h int) {
	if idx < 0 || idx >= len(ui.rowClicks) {
		return
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, h)}.Push(gtx.Ops)
	ui.rowClicks[idx].Add(gtx.Ops)
	area.Pop()
	pass.Pop()
}

// treeHasKeyboard reports whether the arrow keys, Enter and Delete act on the
// file tree. It is a click away and stays there: a click in the middle tree
// takes the keyboard over, and nothing else does, so the tree keeps answering
// to the keys the way a file list is expected to.
func (ui *uiState) treeHasKeyboard() bool {
	return ui.keyboardArea == "files"
}

func hasSuffix(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	return s[len(s)-len(suf):] == suf
}

func (ui *uiState) middleTreeRow(gtx layout.Context, indent int, txt string, expandable, open bool) layout.Dimensions {
	h := gtx.Dp(unit.Dp(18))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			indentW := gtx.Dp(unit.Dp(float32(indent)*14 + 4))
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: unit.Dp(float32(indentW))}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if !expandable {
						return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
					}
					sz := gtx.Dp(unit.Dp(9))
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					b := widget.Border{Color: rgb(0x808080), Width: unit.Dp(1)}
					gtx.Constraints.Min = box
					gtx.Constraints.Max = box
					return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							s := "-"
							if !open {
								s = "+"
							}
							l := material.Label(ui.th, unit.Sp(9), s)
							l.Color = cText
							return l.Layout(gtx)
						})
					})
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Dp(unit.Dp(12))
					col := rgb(0xFFD54F)
					if hasSuffix(txt, ".lua") {
						col = rgb(0x6BB5A0)
					}
					box := image.Pt(sz, sz)
					st := clip.Rect{Max: box}.Push(gtx.Ops)
					paint.ColorOp{Color: col}.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					st.Pop()
					return layout.Dimensions{Size: box}
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(11), txt)
					l.Color = cText
					return l.Layout(gtx)
				}),
			)
		}),
	)
}

func (ui *uiState) layoutRgdMidRow(gtx layout.Context, t *fileTab, idx int, v rgdVisible) layout.Dimensions {
	selected := v.Selected
	expandable := v.IsTable && v.HasKids
	open := v.Expanded
	h := gtx.Dp(unit.Dp(19))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return t.rgdMidClicks[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if selected {
					sz := image.Pt(gtx.Constraints.Max.X, h)
					fillRect(gtx, cSelectBg, sz)
					b := widget.Border{Color: cSelectBd, Width: unit.Dp(1)}
					gtx.Constraints.Min = sz
					gtx.Constraints.Max = sz
					_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
					return layout.Dimensions{Size: sz}
				}
				if t.rgdMidClicks[idx].Hovered() && !selected {
					sz := image.Pt(gtx.Constraints.Max.X, h)
					fillRect(gtx, cMenuHover, sz)
					return layout.Dimensions{Size: sz}
				}
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				indentW := gtx.Dp(unit.Dp(float32(v.Depth)*14 + 4))
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(layout.Spacer{Width: unit.Dp(float32(indentW))}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !expandable {
							return layout.Spacer{Width: unit.Dp(12)}.Layout(gtx)
						}
						sz := gtx.Dp(unit.Dp(9))
						box := image.Pt(sz, sz)
						st := clip.Rect{Max: box}.Push(gtx.Ops)
						paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						st.Pop()
						b := widget.Border{Color: rgb(0x808080), Width: unit.Dp(1)}
						gtx.Constraints.Min = box
						gtx.Constraints.Max = box
						return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								s := "-"
								if !open {
									s = "+"
								}
								l := material.Label(ui.th, unit.Sp(9), s)
								l.Color = cText
								return l.Layout(gtx)
							})
						})
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(unit.Dp(12))
						var col color.NRGBA
						if v.IsTable {
							col = rgb(0xFFD54F)
						} else {
							switch v.Type {
							case rgd.RgdTypeFloat, rgd.RgdTypeInt:
								col = rgb(0x6BB5A0)
							case rgd.RgdTypeBool:
								col = rgb(0x9DB7D0)
							case rgd.RgdTypeString, rgd.RgdTypeWString:
								col = rgb(0xD07DBB)
							default:
								col = rgb(0xC8C8C8)
							}
						}
						box := image.Pt(sz, sz)
						st := clip.Rect{Max: box}.Push(gtx.Ops)
						paint.ColorOp{Color: col}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						st.Pop()
						return layout.Dimensions{Size: box}
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := v.Name
						if len(lbl) > 48 {
							lbl = lbl[:45] + "…"
						}
						l := material.Label(ui.th, unit.Sp(11), lbl)
						if selected {
							l.Color = cText
							l.Font.Weight = font.Medium
						} else {
							l.Color = cText
							if v.Depth == 0 {
								l.Font.Weight = font.Medium
							}
						}
						return l.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (ui *uiState) middleHeader(gtx layout.Context) layout.Dimensions {
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
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(unit.Dp(14))
						box := image.Pt(sz, sz)
						st := clip.Rect{Max: box}.Push(gtx.Ops)
						paint.ColorOp{Color: rgb(0x6B6B6B)}.Add(gtx.Ops)
						paint.PaintOp{}.Add(gtx.Ops)
						st.Pop()
						return layout.Dimensions{Size: box}
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						title := "GameData"
						if tab := ui.activeTab(); tab != nil {
							if tab.kind == "fda" {
								title = filepath.Base(tab.path)
								if title == "" {
									title = "FDA"
								}
							} else if tab.kind == "text" {
								title = filepath.Base(tab.path)
							} else if tab.kind == "rgd" {
								if tab.rgdFile != nil {
									title = filepath.Base(tab.path)
									if e := ui.rgdSelectedEntryFor(tab); e != nil && e != tab.rgdFile.Root {
										title = e.Name
										if idx := strings.LastIndex(tab.rgdSelected, "/"); idx >= 0 {
											title = tab.rgdSelected[idx+1:]
										}
									}
								} else {
									title = filepath.Base(tab.path)
								}
							}
						} else if ui.selectedPath != "" {
							title = ui.selectedPath
							if strings.Contains(title, "\\") {
								title = title[strings.LastIndex(title, "\\")+1:]
							}
						}
						l := material.Label(ui.th, unit.Sp(11), title)
						l.Color = cText
						l.Font.Weight = font.Medium
						return l.Layout(gtx)
					}),
				)
			})
		}),
	)
}
