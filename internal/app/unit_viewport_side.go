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

// unitModelToggles holds what is left of the preview's controls in this
// column: how the view is framed, and the axes the effects are drawn against.
// The effects switch and the ground plane are playback switches and live in
// the playback row under the canvas — see unitModelPlaybackToggles.
func UnitModelToggles(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	ModelControlClicks(ui, gtx, t)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitModelViewButtons(ui, gtx, t, false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitModelFXRow(ui, gtx, t)
		}),
	)
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

// unitModelFXRow is the axes switch: where in the world the effects being
// shown stand. The switch that turns the effects on is a playback switch and
// sits in the playback row under the canvas.
func UnitModelFXRow(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FxAxesButton(ui, gtx, v) }),
		layout.Flexed(1, rowGap()),
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
	return ModelListTabButtons(ui, gtx, labels, ui.UnitListTabClicks[:], selected)
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
