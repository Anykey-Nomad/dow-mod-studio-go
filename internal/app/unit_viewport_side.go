package app

import (
	"dow-mod-studio/internal/app/uikit"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// layoutUnitViewportArea is the whole section: the canvas on the left, the
// camera and animation panel on the right. It is what layoutUnitBody puts where
// the middle and right columns would be.
func LayoutUnitViewportArea(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitModel(ui, gtx)
	sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, CurrentUnitSection(ui).MiddleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							// The playback row belongs to the canvas rather than to
							// the section: it drives what is on the view, so it is
							// as wide as the view and stops where the list panel
							// begins. The list keeps the full height of the section.
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitCanvasColumn(ui, gtx) }),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(FixedPanel(ui, sideW, func(gtx layout.Context) layout.Dimensions { return LayoutUnitModelSide(ui, gtx) })),
						)
					})
			}),
		)
	})
}

// layoutUnitCanvasColumn is the canvas with its own playback row under it:
// the two belong together, and the row stops at the canvas's edges rather than
// running under the list panel beside it. The row keeps the height
// unitPlaybackStripDp promises, so the canvas above it is the same size in
// every window — the section's tests work that size out arithmetically.
func LayoutUnitCanvasColumn(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutUnitModelCanvas(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitModelTransport(ui, gtx, ui.UnitModel)
		}),
	)
}

// layoutUnitModelCanvas is the 3D view itself, or the reason there is not one
// yet: a unit whose model is still being read, one that has no model file, and
// one whose model would not decode all say so rather than showing an empty
// panel.
func LayoutUnitModelCanvas(ui *uiState, gtx layout.Context) layout.Dimensions {
	if t := ui.UnitModel; t != nil && t.Model != nil {
		return LayoutModelViewer(ui, gtx, t)
	}
	msg := "Loading the model…"
	switch {
	case ui.UnitModelErr == errNoUnitModel.Error():
		msg = "This unit names no model file, and none of the same name is in the mod."
	case ui.UnitModelErr != "":
		msg = "The model could not be read:\n" + ui.UnitModelErr
	case CurrentUnit(ui) == nil:
		msg = UnitEmptyStateText(ui)
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), msg)
			l.Color = uikit.CTextDim
			l.MaxLines = 4
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, l.Layout)
		})
	})
}

// unitEmptyStateText is what the viewport says when the mod has no unit to show
// a model of.
func UnitEmptyStateText(ui *uiState) string {
	if d := CurrentUnitDetails(ui); d != nil && d.Error != "" {
		return "Could not read this unit:\n" + d.Error
	}
	return "No unit selected."
}

// layoutUnitModelSide is the panel beside the canvas: the state of the preview,
// the file the geometry came from, the cameras the model declares and every
// clip it can be played with.
//
// The module-listing side panel already carries the Model header over in the
// right-hand panel of the window, so this side panel does not repeat it: the
// static playback area sits first, then the scrollable bone/animation lists.
func LayoutUnitModelSide(ui *uiState, gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// The playback controls stay at the top of this side panel, ahead of
			// the place where the Model header used to begin, so they stay visible
			// while the bone and keyframe lists are scrolled.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelSideControls(ui, gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return UnitModelSideScrollArea(ui, gtx)
			}),
		)
	})
}

// unitModelSideControls is the part of the side panel that does not scroll:
// the model-list tabs, then the preview toggles. The playback row is not part
// of this column — it sits under the canvas, where it belongs — so the tabs
// take the place the section's header used to, at the top of the column where
// they cannot scroll away either. The effects and ground switches moved into
// that row as well; what is left here is the view and the axes of what the
// preview draws.
func UnitModelSideControls(ui *uiState, gtx layout.Context) layout.Dimensions {
	t := ui.UnitModel
	v := UnitModelViewer(ui)
	if t == nil || v == nil {
		return UnitModelSideEmpty(ui, gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelListTabs(ui, gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelToggles(ui, gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelSelectionToggle(ui, gtx, v) }),
	)
}

func UnitModelSelectionToggle(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v == nil {
		return layout.Dimensions{}
	}
	SyncUnitSelectionIndicator(ui, v)
	if v.SelectionIndicator == nil {
		return layout.Dimensions{}
	}
	if ui.ClassicOESelectedUIVisible == nil {
		ui.ClassicOESelectedUIVisible = &widget.Bool{Value: v.ShowSelection}
	} else if ui.ClassicOESelectedUIVisible.Value != v.ShowSelection {
		ui.ClassicOESelectedUIVisible.Value = v.ShowSelection
	}
	check := material.CheckBox(ui.Th, ui.ClassicOESelectedUIVisible, "Show selection outline")
	check.TextSize = unit.Sp(9)
	if ui.ClassicOESelectedUIVisible.Update(gtx) {
		v.ShowSelection = ui.ClassicOESelectedUIVisible.Value
		Invalidate(v)
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, check.Layout)
}

// unitModelSideScrollArea is the scrollable body of the side panel. It holds
// the selected model list and any controls belonging to that list.
func UnitModelSideScrollArea(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBonePoseBar(ui, gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return PanelScrollView(ui, gtx, &ui.UnitViewportScroll, func(gtx layout.Context) layout.Dimensions {
				return UnitModelSideBody(ui, gtx)
			})
		}),
	)
}

// unitModelSideBody is the scrollable content of the side panel.
func UnitModelSideBody(ui *uiState, gtx layout.Context) layout.Dimensions {
	// The modifier keys first: the rows below read Ctrl and Shift in this
	// same pass, and a Ctrl held down for a click arrives as key events of
	// its own.
	UnitListTrackModifiers(ui, gtx)
	t := ui.UnitModel
	v := UnitModelViewer(ui)
	if t == nil || v == nil {
		l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
		l.Color = uikit.CTextDim
		return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
	}
	// The model's own facts — the file, the state, the clip on show, the
	// triangles and the frame the playhead stands on — report in the Object
	// panel on the right, not here: this column is three hundred pixels wide
	// and carries the controls, while the Object panel has the room and was
	// mostly empty. The column opens on the Playback strip directly.
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitClipOrMarkerSection(ui, gtx, v)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.UnitFXListMode || ui.UnitMeshListMode {
				return layout.Dimensions{}
			}
			return UnitImportSection(ui, gtx, v)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.UnitFXListMode {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return SmallButton(ui, gtx, &ui.UnitOpenModelClick, "Open model in a tab")
			})
		}),
	)
}

// unitModelTransport is the playback row: a single horizontal line under the
// canvas — Viewport and Classic OE both draw it there, and only there — with
// the transport, the frame readout, the clip's frame count and the switches
// for what the canvas shows. Everything on it is drained before it is laid
// out, in the order the app drains them everywhere else: the switches and the
// transport's presses, then the frames field's Enter.
func UnitModelTransport(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	// A unit with no model still gives the row its height: the canvas above is
	// measured against it, and a strip that came and went would move the view
	// every time a model finished loading.
	if t == nil || t.Model == nil {
		return unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	ModelControlClicks(ui, gtx, t)
	// The clip's frame count sits beside the transport: the buttons wind the
	// playhead through the clip, and this field says how many frames it is cut
	// into. Its clicks and its Enter are drained here, before the row is laid
	// out, the way every other button in the app is.
	framesSetClicked := ui.UnitAnimFramesSetClick.Clicked(gtx)
	// The frame count is the field typed for the frame/time clipboard's
	// frame half: a right-click pastes the frame number copied off the
	// timebar, ready for Set.
	uikit.MarkClipPaste(ui, &ui.UnitAnimFramesEd, uikit.ClipPasteFrame)
	uikit.PasteClipField(ui, gtx, &ui.UnitAnimFramesEd)
	framesSubmit := false
	for {
		ev, ok := ui.UnitAnimFramesEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			framesSubmit = true
			ui.UnitAnimFramesEd.Blur(gtx)
		}
	}
	SyncAnimFramesField(ui, t.Model)
	dims := unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelTransportButtons(ui, gtx, t)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelFrameLabel(ui, gtx, t.Model)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return UnitAnimFramesRow(ui, gtx, t.Model)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitModelPlaybackToggles(ui, gtx, t)
			}),
		)
	})
	if framesSetClicked || framesSubmit {
		LogInfo("bone: frames pressed: %q", ui.UnitAnimFramesEd.Text())
		ApplyAnimFrameCount(ui, t.Model)
	}
	return dims
}

// unitModelToggles is the row under the list tabs: how the view is framed,
// the axes the effects are drawn against, and the clip removal. The effects
// switch and the ground plane are playback switches and live in the playback
// row under the canvas — see unitModelPlaybackToggles.
func UnitModelToggles(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	ModelControlClicks(ui, gtx, t)
	v := t.Model
	return unitSideButtons(gtx, func(gtx layout.Context) layout.Dimensions {
		return unitSideButtonRow(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return SmallButton(ui, gtx, &t.ModelReset, "Reset view")
			},
			func(gtx layout.Context) layout.Dimensions {
				return FxAxesButton(ui, gtx, v)
			},
			// Remove takes the clip the viewer is on out of the .whm,
			// behind a confirmation — the one button on this panel that
			// changes the file rather than the view.
			func(gtx layout.Context) layout.Dimensions {
				noAnim := v == nil || v.Model == nil || len(v.Model.Animations) == 0
				return SmallButtonDisabled(ui, gtx, &t.ModelAnimRemove, "Remove", noAnim)
			},
		)
	})
}

// unitSideButtonCells is how many buttons fit across this panel. It is three
// hundred dp wide: at five to a row "Animations" wrapped over two lines, and
// three to a row leaves every cell wide enough for the widest label —
// "Reset view" — to sit on one line.
const unitSideButtonCells = 3

// unitSideButtonGapDp is the space between the cells of a row.
const unitSideButtonGapDp = 6

// unitSideRowGapDp is the space between one row of the grid and the next. It
// is the padding unitSideButtons puts around a whole block of them — 4dp
// above and 4dp below — so the two rows of the tabs stand as far apart as the
// tabs stand from the row of view buttons underneath, which is a block of its
// own rather than a third row of the same one.
const unitSideRowGapDp = 8

// unitSideButtons is the panel's padding around a row of its buttons, the same
// for every row of the grid.
func unitSideButtons(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, w)
}

// unitSideButtonRow is one row of the grid: the same number of cells however
// many buttons are on it, each taking an equal share of the panel and any cell
// a short row does not use left empty. That is what keeps a row of two
// buttons the same width as a row of three.
func unitSideButtonRow(gtx layout.Context, buttons ...layout.Widget) layout.Dimensions {
	children := make([]layout.FlexChild, 0, unitSideButtonCells*2-1)
	for i := 0; i < unitSideButtonCells; i++ {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(unitSideButtonGapDp)}.Layout))
		}
		var button layout.Widget = uikit.RowGap()
		if i < len(buttons) {
			button = buttons[i]
		}
		children = append(children, layout.Flexed(1, button))
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
}

// unitSideButtonGrid is several of those rows stacked: the tabs go over two
// rows of three, and the row of view buttons is one of them.
func unitSideButtonGrid(gtx layout.Context, rows ...[]layout.Widget) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(rows)*2-1)
	for i, row := range rows {
		if i > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(unitSideRowGapDp)}.Layout))
		}
		row := row
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return unitSideButtonRow(gtx, row...)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitModelPlaybackToggles are the two switches that change what the canvas is
// showing rather than how it is looked at — the effects and the ground plane
// — which is why they sit at the end of the playback row instead of in the
// list panel. Their presses are already drained by modelControlClicks, which
// the row's caller runs before laying anything out.
func UnitModelPlaybackToggles(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &t.ModelFX, modelFXLabel(v))
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelGround, modelGroundLabel(v), v == nil || v.GroundImage == nil)
		}),
	)
}

func UnitModelListTabs(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	HandleUnitModelListTabClicks(ui, gtx, t)
	labels := []string{"Animations", "Markers", "Bones", "FX", "Meshes"}
	selected := ui.UnitListMode
	if ui.UnitFXListMode {
		selected = 3
	}
	if ui.UnitMeshListMode {
		selected = 4
	}
	// Three tabs to a row, so the second row of two shares the cells of the
	// first and the row of buttons below them starts at the same places.
	var rows [][]layout.Widget
	for start := 0; start < len(labels); start += unitSideButtonCells {
		end := start + unitSideButtonCells
		if end > len(labels) {
			end = len(labels)
		}
		row := make([]layout.Widget, 0, end-start)
		for i := start; i < end; i++ {
			i, label := i, labels[i]
			row = append(row, func(gtx layout.Context) layout.Dimensions {
				return ToggleButton(ui, gtx, &ui.UnitListTabClicks[i], label, selected == i, uikit.CSelectBg)
			})
		}
		rows = append(rows, row)
	}
	return unitSideButtons(gtx, func(gtx layout.Context) layout.Dimensions {
		return unitSideButtonGrid(gtx, rows...)
	})
}

func HandleUnitModelListTabClicks(ui *uiState, gtx layout.Context, t *fileTab) {
	UnitListTrackModifiers(ui, gtx)
	for i := range ui.UnitListTabClicks {
		if ui.UnitListTabClicks[i].Clicked(gtx) {
			ui.UnitFXListMode = i == 3
			ui.UnitMeshListMode = i == 4
			if i < 3 {
				ui.UnitListMode = i
			}
			ui.UnitViewportScroll = panelScrollState{}
			if t != nil && t.Model != nil {
				Invalidate(t.Model)
			}
		}
	}
}

func ModelListTabButtons(ui *uiState, gtx layout.Context, labels []string, clicks []widget.Clickable, selected int) layout.Dimensions {
	if len(clicks) < len(labels) {
		return layout.Dimensions{}
	}
	if selected < 0 || selected >= len(labels) {
		selected = 0
	}
	children := make([]layout.FlexChild, 0, len(labels))
	for i, label := range labels {
		i, label := i, label
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ToggleButton(ui, gtx, &clicks[i], label, selected == i, uikit.CSelectBg)
		}))
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	})
}
