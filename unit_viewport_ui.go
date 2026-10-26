package main

import (
	"dow-mod-studio/internal/whm"
	"fmt"
	"image"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"image/color"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
)

// The Viewport section's two halves: the canvas, and the panel beside it that
// lists what the model offers — its statistics, its own cameras and every clip
// it can play.
//
// The section takes the middle and right columns together rather than sitting in
// a third of the window, because a 3D view in a 400 pixel column is a picture
// of a model, not a viewport. The camera list is what fills the space the
// extra width buys.

// unitViewportSideDp is the width of the panel beside the canvas.
const unitViewportSideDp = 300

// layoutUnitViewportArea is the whole section: the canvas on the left, the
// camera and animation panel on the right. It is what layoutUnitBody puts where
// the middle and right columns would be.
func (ui *uiState) layoutUnitViewportArea(gtx layout.Context) layout.Dimensions {
	ui.ensureUnitModel(gtx)
	sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutUnitColumnHeader(gtx, ui.currentUnitSection().middleTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							// The playback row belongs to the canvas rather than to
							// the section: it drives what is on the view, so it is
							// as wide as the view and stops where the list panel
							// begins. The list keeps the full height of the section.
							layout.Flexed(1, ui.layoutUnitCanvasColumn),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(ui.fixedPanel(sideW, ui.layoutUnitModelSide)),
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
func (ui *uiState) layoutUnitCanvasColumn(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, ui.layoutUnitModelCanvas),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitModelTransport(gtx, ui.unitModel)
		}),
	)
}

// layoutUnitModelCanvas is the 3D view itself, or the reason there is not one
// yet: a unit whose model is still being read, one that has no model file, and
// one whose model would not decode all say so rather than showing an empty
// panel.
func (ui *uiState) layoutUnitModelCanvas(gtx layout.Context) layout.Dimensions {
	if t := ui.unitModel; t != nil && t.model != nil {
		return ui.layoutModelViewer(gtx, t)
	}
	msg := "Loading the model…"
	switch {
	case ui.unitModelErr == errNoUnitModel.Error():
		msg = "This unit names no model file, and none of the same name is in the mod."
	case ui.unitModelErr != "":
		msg = "The model could not be read:\n" + ui.unitModelErr
	case ui.currentUnit() == nil:
		msg = ui.unitEmptyStateText()
	}
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), msg)
			l.Color = cTextDim
			l.MaxLines = 4
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, l.Layout)
		})
	})
}

// unitEmptyStateText is what the viewport says when the mod has no unit to show
// a model of.
func (ui *uiState) unitEmptyStateText() string {
	if d := ui.currentUnitDetails(); d != nil && d.Error != "" {
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
func (ui *uiState) layoutUnitModelSide(gtx layout.Context) layout.Dimensions {
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// The playback controls stay at the top of this side panel, ahead of
			// the place where the Model header used to begin, so they stay visible
			// while the bone and keyframe lists are scrolled.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitModelSideControls(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.unitModelSideScrollArea(gtx)
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
func (ui *uiState) unitModelSideControls(gtx layout.Context) layout.Dimensions {
	t := ui.unitModel
	v := ui.unitModelViewer()
	if t == nil || v == nil {
		return ui.unitModelSideEmpty(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.unitModelListTabs(gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.unitModelToggles(gtx, t) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.unitModelSelectionToggle(gtx, v) }),
	)
}

func (ui *uiState) unitModelSelectionToggle(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if v == nil {
		return layout.Dimensions{}
	}
	ui.syncUnitSelectionIndicator(v)
	if v.selectionIndicator == nil {
		return layout.Dimensions{}
	}
	if ui.classicOESelectedUIVisible == nil {
		ui.classicOESelectedUIVisible = &widget.Bool{Value: v.showSelection}
	} else if ui.classicOESelectedUIVisible.Value != v.showSelection {
		ui.classicOESelectedUIVisible.Value = v.showSelection
	}
	check := material.CheckBox(ui.th, ui.classicOESelectedUIVisible, "Show selection outline")
	check.TextSize = unit.Sp(9)
	if ui.classicOESelectedUIVisible.Update(gtx) {
		v.showSelection = ui.classicOESelectedUIVisible.Value
		v.invalidate()
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, check.Layout)
}

// unitModelSideEmpty is what the controls area shows when no model is loaded.
func (ui *uiState) unitModelSideEmpty(gtx layout.Context) layout.Dimensions {
	l := material.Label(ui.th, unit.Sp(9), "No model is loaded.")
	l.Color = cTextDim
	return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
}

// unitModelSideScrollArea is the scrollable body of the side panel. It holds
// the selected model list and any controls belonging to that list.
func (ui *uiState) unitModelSideScrollArea(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitBonePoseBar(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.panelScrollView(gtx, &ui.unitViewportScroll, func(gtx layout.Context) layout.Dimensions {
				return ui.unitModelSideBody(gtx)
			})
		}),
	)
}

// unitModelSideBody is the scrollable content of the side panel.
func (ui *uiState) unitModelSideBody(gtx layout.Context) layout.Dimensions {
	// The modifier keys first: the rows below read Ctrl and Shift in this
	// same pass, and a Ctrl held down for a click arrives as key events of
	// its own.
	ui.unitListTrackModifiers(gtx)
	t := ui.unitModel
	v := ui.unitModelViewer()
	if t == nil || v == nil {
		l := material.Label(ui.th, unit.Sp(9), "No model is loaded.")
		l.Color = cTextDim
		return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
	}
	// The model's own facts — the file, the state, the clip on show, the
	// triangles and the frame the playhead stands on — report in the Object
	// panel on the right, not here: this column is three hundred pixels wide
	// and carries the controls, while the Object panel has the room and was
	// mostly empty. The column opens on the Playback strip directly.
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitClipOrMarkerSection(gtx, v)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.unitFXListMode || ui.unitMeshListMode {
				return layout.Dimensions{}
			}
			return ui.unitImportSection(gtx, v)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.unitFXListMode {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &ui.unitOpenModelClick, "Open model in a tab")
			})
		}),
	)
}

// unitPlaybackStripDp is the height of the playback row: one line of transport
// buttons, the frame readout and the frames field, plus the row's own padding.
// It is fixed so the canvas above it can be worked out from the window's size —
// the viewport's tests do exactly that arithmetic — and so the strip does not
// grow a line taller when a hint wraps.
const unitPlaybackStripDp = 38

// unitPlaybackRow gives the strip its height and its padding, whatever is
// drawn inside it.
func unitPlaybackRow(gtx layout.Context, content layout.Widget) layout.Dimensions {
	d := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, content)
	if h := gtx.Dp(unit.Dp(unitPlaybackStripDp)); d.Size.Y < h {
		d.Size.Y = h
	}
	return d
}

// unitModelTransport is the playback row: a single horizontal line under the
// canvas — Viewport and Classic OE both draw it there, and only there — with
// the transport, the frame readout, the clip's frame count and the switches
// for what the canvas shows. Everything on it is drained before it is laid
// out, in the order the app drains them everywhere else: the switches and the
// transport's presses, then the frames field's Enter.
func (ui *uiState) unitModelTransport(gtx layout.Context, t *fileTab) layout.Dimensions {
	// A unit with no model still gives the row its height: the canvas above is
	// measured against it, and a strip that came and went would move the view
	// every time a model finished loading.
	if t == nil || t.model == nil {
		return unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), "No model is loaded.")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	ui.modelControlClicks(gtx, t)
	// The clip's frame count sits beside the transport: the buttons wind the
	// playhead through the clip, and this field says how many frames it is cut
	// into. Its clicks and its Enter are drained here, before the row is laid
	// out, the way every other button in the app is.
	framesSetClicked := ui.unitAnimFramesSetClick.Clicked(gtx)
	framesSubmit := false
	for {
		ev, ok := ui.unitAnimFramesEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			framesSubmit = true
			ui.unitAnimFramesEd.Blur(gtx)
		}
	}
	ui.syncAnimFramesField(t.model)
	dims := unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitModelTransportButtons(gtx, t)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitModelFrameLabel(gtx, t.model)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return ui.unitAnimFramesRow(gtx, t.model)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitModelPlaybackToggles(gtx, t)
			}),
		)
	})
	if framesSetClicked || framesSubmit {
		LogInfo("bone: frames pressed: %q", ui.unitAnimFramesEd.Text())
		ui.applyAnimFrameCount(t.model)
	}
	return dims
}

// unitModelTransportButtons draws the viewport's shared playback row after its
// caller has handled modelControlClicks for this frame. It is content only —
// the strip it sits in gives the row its padding and its height.
func (ui *uiState) unitModelTransportButtons(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	none := len(v.model.Animations) == 0
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &t.modelPlay, modelPlayLabel(v), none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &t.modelPrevAnim, "◀", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &t.modelNextAnim, "▶", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &t.modelStepBack, "|◀", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &t.modelStepFwd, "▶|", none)
		}),
	)
}

// unitFrameLabelDp is the width the frame readout is given. The digits of the
// frame number and the key suffix beside it come and go as the playhead moves,
// and a label that grew with them shoved the frames field, the Set button and
// the duration readout sideways every few frames. The box is a fixed width
// instead, and the text wraps inside it rather than widening it.
const unitFrameLabelDp = 132

// unitModelFrameLabel is where the play head is: the frame on show out of the
// frames there are, which is what a single step of the buttons moves. When the
// playhead stands on a keyframe of the clip, the readout says which key it is
// out of how many — that is the number a reader stepping between keys needs,
// and it cannot be told from the picture.
func (ui *uiState) unitModelFrameLabel(gtx layout.Context, v *modelViewer) layout.Dimensions {
	text := "no frames to step through"
	if frame, frames := v.currentFrame(); frames > 0 {
		text = fmt.Sprintf("frame %d of %d", frame+1, frames)
		if keys := clipKeyFrames(v.anim); len(keys) > 0 {
			for i, k := range keys {
				if k == frame {
					text += fmt.Sprintf(" · key %d/%d", i+1, len(keys))
					break
				}
			}
		}
	}
	l := material.Label(ui.th, unit.Sp(9), text)
	l.Color = cTextDim
	l.MaxLines = 2
	w := gtx.Dp(unit.Dp(unitFrameLabelDp))
	gtx.Constraints.Max.X = w
	d := layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	d.Size.X = w
	return d
}

// unitModelToggles holds what is left of the preview's controls in this
// column: how the view is framed, and the axes the effects are drawn against.
// The effects switch and the ground plane are playback switches and live in
// the playback row under the canvas — see unitModelPlaybackToggles.
func (ui *uiState) unitModelToggles(gtx layout.Context, t *fileTab) layout.Dimensions {
	ui.modelControlClicks(gtx, t)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitModelViewButtons(gtx, t, false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitModelFXRow(gtx, t)
		}),
	)
}

// unitModelPlaybackToggles are the two switches that change what the canvas is
// showing rather than how it is looked at — the effects and the ground plane
// — which is why they sit at the end of the playback row instead of in the
// list panel. Their presses are already drained by modelControlClicks, which
// the row's caller runs before laying anything out.
func (ui *uiState) unitModelPlaybackToggles(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButton(gtx, &t.modelFX, modelFXLabel(v))
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &t.modelGround, modelGroundLabel(v), v == nil || v.groundImage == nil)
		}),
	)
}

// unitModelViewButtons is the viewport's reset row. WHM tabs reuse it as well.
func (ui *uiState) unitModelViewButtons(gtx layout.Context, t *fileTab, showListToggle bool) layout.Dimensions {
	gtx = modelRowHeight(gtx)
	return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.smallButton(gtx, &t.modelReset, "Reset view")
				}),
				layout.Flexed(1, rowGap()),
			)
		})
}

// unitModelFXRow is the axes switch: where in the world the effects being
// shown stand. The switch that turns the effects on is a playback switch and
// sits in the playback row under the canvas.
func (ui *uiState) unitModelFXRow(gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.model
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.fxAxesButton(gtx, v) }),
		layout.Flexed(1, rowGap()),
	)
}

func (ui *uiState) unitModelListTabs(gtx layout.Context, t *fileTab) layout.Dimensions {
	ui.handleUnitModelListTabClicks(gtx, t)
	labels := []string{"Animations", "Markers", "Bones", "FX", "Meshes"}
	selected := ui.unitListMode
	if ui.unitFXListMode {
		selected = 3
	}
	if ui.unitMeshListMode {
		selected = 4
	}
	return ui.modelListTabButtons(gtx, labels, ui.unitListTabClicks[:], selected)
}

func (ui *uiState) handleUnitModelListTabClicks(gtx layout.Context, t *fileTab) {
	ui.unitListTrackModifiers(gtx)
	for i := range ui.unitListTabClicks {
		if ui.unitListTabClicks[i].Clicked(gtx) {
			ui.unitFXListMode = i == 3
			ui.unitMeshListMode = i == 4
			if i < 3 {
				ui.unitListMode = i
			}
			ui.unitViewportScroll = panelScrollState{}
			if t != nil && t.model != nil {
				t.model.invalidate()
			}
		}
	}
}

func (ui *uiState) modelListTabButtons(gtx layout.Context, labels []string, clicks []widget.Clickable, selected int) layout.Dimensions {
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
			return ui.toggleButton(gtx, &clicks[i], label, selected == i, cSelectBg)
		}))
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// unitModelFileLabel is the model file the viewport is showing.
func (ui *uiState) unitModelFileLabel() string {
	return filepath.Base(ui.unitModelPath)
}

// unitAnimList is every clip the model carries, the one being played marked, so
// the list is also the answer to "what can this thing do".
func (ui *uiState) unitAnimList(gtx layout.Context, v *modelViewer) layout.Dimensions {
	return ui.unitAnimListWithClicks(gtx, v, &ui.unitAnimClicks)
}

func (ui *uiState) unitAnimListWithClicks(gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	anims := v.model.Animations
	if len(*clicks) != len(anims) {
		*clicks = make([]widget.Clickable, len(anims))
	}
	if len(anims) == 0 {
		l := material.Label(ui.th, unit.Sp(9), "This model carries no animations.")
		l.Color = cTextDim
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range anims {
		i := i
		a := anims[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			v.selectAnim(i)
			v.playing = true
			v.lastFrame = time.Time{}
		}
		right := fmt.Sprintf("%d frames", a.NumFrames)
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitViewportRow(gtx, c, v.animIndex == i, a.Name, right)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitClipOrMarkerSection is the one section of the panel that holds the
// clip list, marker list, bone list, FX list, or mesh list. All five are
// lists of the same model's contents, and all have to be reachable while
// looking at the others: which clip plays an effect is a marker question,
// which marker a beam leaves is a clip question, and a bone sits under both.
//
// The header says which list is on show; the button that swaps it lives in
// the Playback strip above, which never scrolls away.
func (ui *uiState) unitClipOrMarkerSection(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.unitMeshListMode {
		return ui.unitMeshListSection(gtx, v)
	}
	return ui.unitModelListSectionOrFX(gtx, v, ui.unitListMode, ui.unitFXListMode,
		&ui.unitAnimClicks, &ui.unitMarkerClicks, &ui.unitBoneClicks, &ui.unitFXClicks)
}

// unitMeshListSection is the Meshes tab of the side panel: the count header
// over the same eye-toggle rows the Events and Classic OE panels draw, so a
// mesh hidden here is hidden in every viewer of this model. The rows carry no
// scroller of their own — the side panel's body, which holds this section, is
// already scrolled.
func (ui *uiState) unitMeshListSection(gtx layout.Context, v *modelViewer) layout.Dimensions {
	count := 0
	if v != nil && v.model != nil {
		count = len(modelMeshNames(v.model))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.th, fmt.Sprintf("Meshes (%d)", count))
		}),
		layout.Rigid(ui.unitMeshRows),
	)
}

func (ui *uiState) unitModelListContent(gtx layout.Context, t *fileTab, mode int) layout.Dimensions {
	ui.unitListTrackModifiers(gtx)
	return ui.unitModelListSectionOrFX(gtx, t.model, mode, mode == 3, &t.modelAnimClicks, &t.modelMarkerClicks, &t.modelBoneClicks, &t.modelFXClicks)
}

func (ui *uiState) unitModelListSectionOrFX(gtx layout.Context, v *modelViewer, mode int, fxMode bool, animClicks, markerClicks, boneClicks, fxClicks *[]widget.Clickable) layout.Dimensions {
	if fxMode {
		return ui.unitFXListWithHeader(gtx, v, fxClicks)
	}
	return ui.unitModelListSection(gtx, v, mode, animClicks, markerClicks, boneClicks)
}

func (ui *uiState) unitFXListWithHeader(gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	if v == nil || v.model == nil {
		return sectionHeader(gtx, ui.th, "FX")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.th, fmt.Sprintf("FX (%d)", len(v.fxEffects)))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.modelFxChecklist(gtx, v, clicks) }),
	)
}

func (ui *uiState) unitModelListSection(gtx layout.Context, v *modelViewer, mode int, animClicks, markerClicks, boneClicks *[]widget.Clickable) layout.Dimensions {
	labels := [3]string{"Animations", "Markers", "Bones"}
	if mode < 0 || mode >= len(labels) {
		mode = 0
	}
	list := func(gtx layout.Context) layout.Dimensions { return ui.unitAnimListWithClicks(gtx, v, animClicks) }
	switch mode {
	case 1:
		list = func(gtx layout.Context) layout.Dimensions { return ui.unitMarkerListWithClicks(gtx, v, markerClicks) }
	case 2:
		list = func(gtx layout.Context) layout.Dimensions { return ui.unitBoneListWithClicks(gtx, v, boneClicks) }
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.th, labels[mode]) }),
		layout.Rigid(list),
	)
}

// unitMarkerList is every attachment point the model carries, its name and the
// bone it hangs off. Picking one draws a red ring on the model at that marker,
// which is the only way to tell a dozen identically named muzzle points apart
// on a mesh that shows none of them.
func (ui *uiState) unitMarkerList(gtx layout.Context, v *modelViewer) layout.Dimensions {
	return ui.unitMarkerListWithClicks(gtx, v, &ui.unitMarkerClicks)
}

func (ui *uiState) unitMarkerListWithClicks(gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	markers := v.model.Markers
	if len(*clicks) != len(markers) {
		*clicks = make([]widget.Clickable, len(markers))
	}
	if len(markers) == 0 {
		l := material.Label(ui.th, unit.Sp(9), "This model carries no markers.")
		l.Color = cTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range markers {
		i := i
		mk := markers[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			if v.markerIndex == i {
				v.markerIndex = -1
			} else {
				v.markerIndex = i
			}
			v.redraw = true
		}
		note := mk.Parent
		if note == "" {
			note = "no bone"
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitViewportRow(gtx, c, v.markerIndex == i, mk.Name, note)
		}))
	}
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
}

// unitBoneList is every bone the model's skeleton carries, its name and the
// bone it hangs off. Picking one outlines that bone on the model in the pick
// colour and shows its parameters under the list: which joint is picked is a
// question a picture of a skeleton answers badly, since every bone looks like
// every other bone once the mesh is skinned over it.
func (ui *uiState) unitBoneList(gtx layout.Context, v *modelViewer) layout.Dimensions {
	return ui.unitBoneListWithClicks(gtx, v, &ui.unitBoneClicks)
}

func (ui *uiState) unitBoneListWithClicks(gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	bones := v.model.Bones
	if len(*clicks) != len(bones) {
		*clicks = make([]widget.Clickable, len(bones))
	}
	if len(bones) == 0 {
		l := material.Label(ui.th, unit.Sp(9), "This model carries no skeleton.")
		l.Color = cTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range bones {
		i := i
		bone := bones[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			if v.boneIndex == i {
				v.boneIndex = -1
			} else {
				v.boneIndex = i
			}
			v.redraw = true
		}
		note := "root"
		if bone.Parent >= 0 && bone.Parent < len(bones) && bones[bone.Parent].Name != "" {
			note = bones[bone.Parent].Name
		} else if bone.Parent >= 0 && bone.Parent < len(bones) {
			note = fmt.Sprintf("bone %d", bone.Parent)
		}
		name := bone.Name
		if name == "" {
			name = fmt.Sprintf("bone %d", i)
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitViewportRow(gtx, c, v.boneIndex == i, name, note)
		}))
	}
	if v.boneIndex >= 0 && v.boneIndex < len(bones) {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitBoneParams(gtx, v, v.boneIndex)
		}))
	}
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
}

// unitBoneParams is the picked bone's own numbers: where its joint sits under
// the pose on show, the rest position as the file stores it, and how many
// keys the current clip holds for it.
func (ui *uiState) unitBoneParams(gtx layout.Context, v *modelViewer, i int) layout.Dimensions {
	bone := v.model.Bones[i]
	pose := v.pose()
	pos := "—"
	if i < len(pose.Bones) {
		p := pose.Bones[i].MulPosition(fauxgl.Vector{})
		pos = fmt.Sprintf("%.3f, %.3f, %.3f", p.X, p.Y, p.Z)
	}
	rest := fmt.Sprintf("%.3f, %.3f, %.3f", bone.Pos[0], bone.Pos[1], bone.Pos[2])
	rows := [][2]string{
		{"Position", pos},
		{"Rest pose", rest},
		{"Keys here", fmt.Sprintf("%d", boneTrackKeys(v.anim, i))},
	}
	children := make([]layout.FlexChild, 0, len(rows)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) })
	}))
	for idx, row := range rows {
		row, idx := row, idx
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, row[0], row[1], idx%2 == 0)
		}))
	}

	if v.anim != nil {
		t := v.boneTrack()
		if t != nil && v.boneIndex == i && (len(t.PosKey) > 0 || len(t.RotKey) > 0) {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) })
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitBoneKeyListHeader(gtx, v)
			}))
			refs := v.boneKeyRefs()
			ui.ensureUnitBoneKeyClicks(len(refs))
			ui.bindBoneKeySelection(v)
			keyIdx := 0
			for idx, k := range t.PosKey {
				ref := boneKeyRef{mode: 0, frame: frameAt(float64(k.Time), v.anim.NumFrames)}
				c := &ui.unitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					ui.boneKeyRowClicked(v, ref, refs, ui.unitListModifiers())
				}
				selected := ui.boneKeyRowPicked(v, ref)
				text := fmt.Sprintf("pos %d  frame %d  %.3f, %.3f, %.3f", idx+1, ref.frame+1, k.V[0], k.V[1], k.V[2])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitBoneKeyRow(gtx, c, selected, text)
				}))
				keyIdx++
			}
			for idx, k := range t.RotKey {
				ref := boneKeyRef{mode: 1, frame: frameAt(float64(k.Time), v.anim.NumFrames)}
				c := &ui.unitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					ui.boneKeyRowClicked(v, ref, refs, ui.unitListModifiers())
				}
				selected := ui.boneKeyRowPicked(v, ref)
				text := fmt.Sprintf("rot %d  frame %d  %.3f, %.3f, %.3f, %.3f", idx+1, ref.frame+1, k.V[0], k.V[1], k.V[2], k.V[3])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitBoneKeyRow(gtx, c, selected, text)
				}))
				keyIdx++
			}

		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// unitBoneKeyListHeader is the "Keyframes" line over the picked bone's rows,
// with how many of them are picked on the right. The rows are the selection's
// only other showing, and a highlight spread over a scrolled list does not say
// how far it reaches.
//
// The spacer is rowGap and not flexGap: this line lives inside the panel's
// scroll content, which is measured with an unbounded height, and a spacer
// that reports back the height it was offered measures the whole panel as
// full — no scrollbar, no wheel — the moment the picked bone has keys. The
// picked count still reaches the right edge because rowGap takes all the
// width and none of the height.
func (ui *uiState) unitBoneKeyListHeader(gtx layout.Context, v *modelViewer) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), "Keyframes")
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Flexed(1, rowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if n := len(ui.pickedBoneKeyRefs(v)); n > 1 {
				l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("%d picked", n))
				l.Color = cTextDim
				return l.Layout(gtx)
			}
			return layout.Dimensions{}
		}),
	)
}

// unitListTrackModifiers follows Ctrl and Shift while the side panel is on
// screen: a pointer press does not carry usable modifiers here, so Ctrl+click
// and Shift+click on the keyframe rows read the state these key events keep up
// to date. Both filters allow any modifier state — a filter's match requires
// the event's modifiers to be exactly the ones it names as Required plus the
// ones it allows as Optional, so a name-only filter would reject the Ctrl
// press itself the moment that event starts carrying ModCtrl, and the held
// flag would stay false for ever. Every modifier event seen is logged on
// change, so a multi-select that never fires shows here whether the key events
// arrived at all and what they carried.
func (ui *uiState) unitListTrackModifiers(gtx layout.Context) {
	const anyMod = key.ModCtrl | key.ModShift | key.ModAlt | key.ModSuper
	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameCtrl, Optional: anyMod},
			key.Filter{Name: key.NameShift, Optional: anyMod},
		)
		if !ok {
			return
		}
		e, ok := ev.(key.Event)
		if !ok {
			continue
		}
		if s := fmt.Sprintf("%q %v mods=%v", e.Name, e.State, e.Modifiers); s != ui.unitBoneKeyModLog {
			LogInfo("bone: modifier key %s", s)
			ui.unitBoneKeyModLog = s
		}
		switch e.Name {
		case key.NameCtrl:
			ui.unitBoneKeyCtrlHeld = e.State != key.Release
		case key.NameShift:
			ui.unitBoneKeyShiftHeld = e.State != key.Release
		}
	}
}

// unitListModifiers is the state of Ctrl and Shift at the moment a row is
// read: the tracked key events, ORed with the live OS state, since either one
// alone can miss a press on some backends.
func (ui *uiState) unitListModifiers() key.Modifiers {
	m := heldModifiers()
	if ui.unitBoneKeyCtrlHeld {
		m |= key.ModCtrl
	}
	if ui.unitBoneKeyShiftHeld {
		m |= key.ModShift
	}
	return m
}

// bindBoneKeySelection keeps the picked rows with the bone they were picked
// on: another bone or another clip starts an empty selection rather than
// carrying a set of frames over to keys that are not there. It runs where the
// rows are built, which is the only place both the selection and the bone it
// belongs to are on show.
func (ui *uiState) bindBoneKeySelection(v *modelViewer) {
	bound := ""
	if v != nil {
		bound = fmt.Sprintf("%p|%d", v.anim, v.boneIndex)
	}
	if bound == ui.unitBoneKeySelBound {
		return
	}
	LogInfo("bone: keyframe selection bound to %q -> %q: cleared", ui.unitBoneKeySelBound, bound)
	ui.unitBoneKeySelBound = bound
	ui.unitBoneKeySel = nil
	ui.unitBoneKeySelAnchor = boneKeyRef{mode: -1}
}

// pickedBoneKeyRefs is the rows of the list that are picked and whose keys
// are still on the track, in list order. Rows whose key has gone — a deletion
// elsewhere, a drag that carried it off its frame — leave the selection rather
// than pointing at whatever frame now carries that number.
func (ui *uiState) pickedBoneKeyRefs(v *modelViewer) []boneKeyRef {
	if len(ui.unitBoneKeySel) == 0 {
		return nil
	}
	if v == nil || v.anim == nil {
		ui.unitBoneKeySel = nil
		ui.unitBoneKeySelAnchor = boneKeyRef{mode: -1}
		return nil
	}
	var out []boneKeyRef
	for _, ref := range v.boneKeyRefs() {
		if ui.unitBoneKeySel[ref] {
			out = append(out, ref)
		}
	}
	if len(out) == len(ui.unitBoneKeySel) {
		return out
	}
	fresh := make(map[boneKeyRef]bool, len(out))
	for _, ref := range out {
		fresh[ref] = true
	}
	LogInfo("bone: keyframe selection dropped %d row(s) whose keys are gone", len(ui.unitBoneKeySel)-len(out))
	ui.unitBoneKeySel = fresh
	if len(fresh) == 0 {
		ui.unitBoneKeySelAnchor = boneKeyRef{mode: -1}
	}
	return out
}

// boneKeyActionRefs is what the pose bar's key buttons act on: the rows still
// picked on the list, and — when nothing on the list is picked — the single
// key the strip or the playhead had picked, which is all the buttons had
// before the list could hold a selection.
func (ui *uiState) boneKeyActionRefs(v *modelViewer) []boneKeyRef {
	if v == nil || v.anim == nil {
		return nil
	}
	if refs := ui.pickedBoneKeyRefs(v); len(refs) > 0 {
		return refs
	}
	if ref, ok := v.pickedBoneKeyRef(); ok {
		return []boneKeyRef{ref}
	}
	return nil
}

// boneKeyRowPicked says whether a row wears the selection colour. While the
// list holds a selection that is what decides; with nothing picked on the list
// the key the strip or the playhead picked is the one on show, which is what
// the list highlighted before it could hold a selection at all.
func (ui *uiState) boneKeyRowPicked(v *modelViewer, ref boneKeyRef) bool {
	if len(ui.unitBoneKeySel) > 0 {
		return ui.unitBoneKeySel[ref]
	}
	got, ok := v.pickedBoneKeyRef()
	return ok && got == ref
}

// boneKeyRowClicked is what a press on a row of the keyframe list does. A plain
// click picks that row alone; Ctrl+click adds or takes it away, which is how a
// set of rows to delete or to add keys around is gathered; Shift+click picks
// every row between the row the anchor stands on and this one. Whatever the
// modifiers did, the key the typed fields and the strip work on ends up on the
// row that was clicked while that row is among the picked ones — and on the
// last row still picked when it is not, or on none when nothing is left.
func (ui *uiState) boneKeyRowClicked(v *modelViewer, ref boneKeyRef, refs []boneKeyRef, mods key.Modifiers) {
	if v == nil || v.anim == nil {
		return
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	if ui.unitBoneKeySel == nil {
		ui.unitBoneKeySel = make(map[boneKeyRef]bool)
	}
	switch {
	case shift && ui.unitBoneKeySelAnchor.mode >= 0:
		lo, hi := -1, -1
		for i, r := range refs {
			if r == ui.unitBoneKeySelAnchor {
				lo = i
			}
			if r == ref {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			// The anchor is not on the list any more: a Shift+click with
			// nothing to extend from picks the row it lands on.
			ui.unitBoneKeySel = map[boneKeyRef]bool{ref: true}
			ui.unitBoneKeySelAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.unitBoneKeySel = make(map[boneKeyRef]bool, hi-lo+1)
		for _, r := range refs[lo : hi+1] {
			ui.unitBoneKeySel[r] = true
		}
		LogInfo("bone: Shift+click spans %d row(s), frames %d..%d", hi-lo+1, refs[lo].frame, refs[hi].frame)
	case ctrl:
		if ui.unitBoneKeySel[ref] {
			delete(ui.unitBoneKeySel, ref)
			LogInfo("bone: Ctrl+click takes frame %d (%s) out of the selection", ref.frame, boneGroupLog(ref.mode))
		} else {
			ui.unitBoneKeySel[ref] = true
			ui.unitBoneKeySelAnchor = ref
			LogInfo("bone: Ctrl+click adds frame %d (%s) to the selection", ref.frame, boneGroupLog(ref.mode))
		}
	default:
		ui.unitBoneKeySel = map[boneKeyRef]bool{ref: true}
		ui.unitBoneKeySelAnchor = ref
	}
	// The picked key follows the click: the clicked row while it is still
	// among the picked ones, otherwise the last row still picked — in list
	// order, so the choice does not depend on map iteration — and none when
	// the selection is empty.
	picked := boneKeyRef{mode: -1}
	if ui.unitBoneKeySel[ref] {
		picked = ref
	} else {
		for _, r := range refs {
			if ui.unitBoneKeySel[r] {
				picked = r
			}
		}
	}
	if at := v.boneKeyIndexAtFrame(picked.frame, picked.mode); picked.mode >= 0 && at >= 0 {
		v.boneKeyIndex, v.boneKeyMode = at, picked.mode
	} else {
		v.boneKeyIndex, v.boneKeyMode = -1, -1
	}
	v.redraw = true
}

// pickFrameKeyOnTimeline is what the step buttons do after winding the
// playhead: with a bone picked, every key that bone holds on the frame they
// landed on joins the selection and one of them becomes the key the strip
// shows picked, so stepping through the clip walks the selection along the
// keys instead of leaving it behind where it started. A frame carrying no
// key of this bone leaves the selection as it was.
func (ui *uiState) pickFrameKeyOnTimeline(v *modelViewer) {
	if v == nil || v.anim == nil || v.boneIndex < 0 || v.boneTrack() == nil {
		return
	}
	frame, _ := v.currentFrame()
	posAt := v.boneKeyIndexAtFrame(frame, 0)
	rotAt := v.boneKeyIndexAtFrame(frame, 1)
	if posAt < 0 && rotAt < 0 {
		return
	}
	// The key the strip picks follows the kind that was picked before
	// when it has a key on this frame, otherwise the other kind.
	mode := 1
	if rotAt < 0 || (v.boneKeyMode == 0 && posAt >= 0) {
		mode = 0
	}
	if mode == 0 {
		v.boneKeyIndex, v.boneKeyMode = posAt, 0
	} else {
		v.boneKeyIndex, v.boneKeyMode = rotAt, 1
	}
	sel := map[boneKeyRef]bool{}
	if posAt >= 0 {
		sel[boneKeyRef{mode: 0, frame: frame}] = true
	}
	if rotAt >= 0 {
		sel[boneKeyRef{mode: 1, frame: frame}] = true
	}
	ui.unitBoneKeySel = sel
	ui.unitBoneKeySelAnchor = boneKeyRef{mode: mode, frame: frame}
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	LogInfo("bone: step picked frame %d: posKey=%v rotKey=%v picked=%s",
		frame+1, posAt >= 0, rotAt >= 0, boneGroupLog(mode))
}

// unitBonePoseBar is the pose-editing menu shown above the list while the bone
// list is on show. The keyframe list scrolls with the rest of the panel, so
// the buttons appended to it slid out of reach exactly when a key picked near
// the top of the list needed them; this bar sits outside the scroll area, and
// showing it shortens the list, so the menu stays visible at any scroll
// position.
//
// The picked key's position and rotation are also typed fields: Enter in a
// field applies its triple to the key, and Save WHM writes the edited keys
// back into the .whm. Both groups show the key a typed apply would write —
// the picked key for its own kind, the nearest key of the other kind to the
// frame on show for the other.
func (ui *uiState) unitBonePoseBar(gtx layout.Context) layout.Dimensions {
	v := ui.unitModelViewer()
	if ui.unitListMode != 2 || v == nil {
		// If the bar is not laid out, its buttons cannot see a press — while
		// the last drawn frame still shows them. The log says when this
		// becomes the case, which turns a dead click into a visible cause.
		if state := fmt.Sprintf("mode=%d viewer=%v", ui.unitListMode, v != nil); state != ui.unitBonePoseBarLog {
			LogInfo("bone: pose bar is not laid out (%s)", state)
			ui.unitBonePoseBarLog = state
		}
		return layout.Dimensions{}
	}
	ui.unitBonePoseBarLog = ""
	// The buttons' clicks are drained before they are laid out
	// below, never after. Clickable.Layout also updates the gesture, and an
	// update that completes a click inside Layout hands the click to Layout —
	// which discards it, so a Clicked call later in the same frame finds
	// nothing. The log showed Set and Set all pressed (press=true→false with
	// hover held) without ever reaching their handlers, while Save — whose
	// release happened to land between its Layout and its Clicked — fired;
	// draining first inverts that race: a release waiting from the previous
	// frame is read here, before Layout can swallow it. This is the order the
	// rest of the app uses, with handleInput running ahead of Layout.
	setClicked := ui.unitBoneKeySetClick.Clicked(gtx)
	setAllClicked := ui.unitBoneKeySetAllClick.Clicked(gtx)
	saveClicked := ui.unitBoneKeySaveClick.Clicked(gtx)
	importClicked := ui.unitImpClick.Clicked(gtx)
	addPosClicked := ui.unitBoneKeyAddPosClick.Clicked(gtx)
	addRotClicked := ui.unitBoneKeyAddRotClick.Clicked(gtx)
	beforeClicked := ui.unitBoneKeyBeforeClick.Clicked(gtx)
	afterClicked := ui.unitBoneKeyAfterClick.Clicked(gtx)
	delClicked := ui.unitBoneKeyDelClick.Clicked(gtx)
	mirClicked := [3]bool{
		ui.unitBoneMirClick[0].Clicked(gtx),
		ui.unitBoneMirClick[1].Clicked(gtx),
		ui.unitBoneMirClick[2].Clicked(gtx),
	}
	shiftBackClicked := ui.unitBoneKeyShiftBack.Clicked(gtx)
	shiftFwdClicked := ui.unitBoneKeyShiftFwd.Clicked(gtx)
	undoClicked := ui.unitBoneKeyUndoClick.Clicked(gtx)
	// Events first, before the fields are laid out below: an Enter pressed
	// since the last frame applies its triple, and the group it belongs to
	// is refilled with the values now in the key.
	ui.updateBoneKeyEditors(gtx, v)
	ui.syncBoneKeyFields(gtx, v)
	// The frames field keeps its events drained like the typed groups:
	// a press inside it takes the keyboard, Enter lets go of it again.
	for {
		ev, ok := ui.unitBoneKeyShiftEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.unitBoneKeyShiftEd.Blur(gtx)
		}
	}
	// What the keyframe buttons act on — the picked bone's track to add a key
	// to, the rows of the list (or, with none picked, the key the strip picked)
	// to take away or to add keys beside — is read here, before the layout below
	// draws them disabled.
	canAddKey := v != nil && v.anim != nil && v.boneIndex >= 0 && v.boneTrack() != nil
	actionRefs := ui.boneKeyActionRefs(v)
	canDelKey := len(actionRefs) > 0
	canAddAround := canAddKey && len(actionRefs) > 0
	canShift := canDelKey
	canMir := false
	if v.anim != nil {
		frame, _ := v.currentFrame()
		canMir = v.boneKeyTarget(frame, 1) >= 0
	}
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) })
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return ui.unitBoneKeyFieldsRow(gtx, "Pos", &ui.unitBonePosEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return ui.unitBoneKeyFieldsRow(gtx, "Rot", &ui.unitBoneRotEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The angle sliders dial the rotation key the Rot fields
			// show about one axis each, across the full -180..180
			// turn the fields work in. They follow the fields, and
			// dragging one writes the key the same way Enter in a
			// field does.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					children := []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), "Ang")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
					}
					for i := range ui.unitBoneAngleSlid {
						i := i
						children = append(children,
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.th, unit.Sp(9), boneFieldAxes[i])
								l.Color = cTextDim
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(56))
								gtx.Constraints.Max.X = gtx.Dp(unit.Dp(64))
								sl := material.Slider(ui.th, &ui.unitBoneAngleSlid[i])
								sl.Color = rgb(0x6B6B6B)
								// The slider's own Layout folds the drag into
								// its Value; the write to the key happens once,
								// after the whole row has laid out.
								return sl.Layout(gtx)
							}),
						)
					}
					dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
					// All three sliders have laid out, so their drags are in
					// Value now. While any is under the hand, store the trio
					// in the rotation key — the bone and the mesh skinned to
					// it turn to the new angle on this very frame.
					ui.applyBoneAngleSliders(v)
					return dims
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The mirror buttons flip the angle of the rotation key
			// the Rot fields show about their own axis: that axis's
			// angle comes back negated, the other two are left as
			// they were.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), "Mirror")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneMirClick[0], "mirror X", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneMirClick[1], "mirror Y", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneMirClick[2], "mirror Z", !canMir)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The shift row moves the picked keys along the strip:
			// the field holds how many frames, and back/forward move
			// them by that many without reselecting.
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), "Shift")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.unitBoneKeyField(gtx, "f", &ui.unitBoneKeyShiftEd)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyShiftBack, "back", !canShift)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyShiftFwd, "fwd", !canShift)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(8), "frames each press moves")
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The keyframe buttons: give the picked bone a key on the frame
			// the playhead is on, or take the picked key away. The hint on
			// the right is how the keys already on show are moved — the
			// strip's marks are the handles their drag carries along.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyAddPosClick, "+ pos", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyAddRotClick, "+ rot", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyDelClick, "Delete key", !canDelKey)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(8), "drag a mark on the strip to move its key")
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// The buttons that add a key either side of the picked rows:
			// one of the row's own kind on the nearest frame that carries
			// none yet, holding what the row holds. They take their own row
			// because the one above is full, and the hint on the right is
			// how a set of rows is gathered in the first place.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyBeforeClick, "+ before", !canAddAround)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyAfterClick, "+ after", !canAddAround)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(8), "Ctrl+click picks more rows")
							l.Color = cTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeyUndoClick, "Undo", len(ui.unitBoneUndo) == 0)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.unitBoneKeySetClick, "Set")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.unitBoneKeySetAllClick, "Set all")
						}),
						layout.Flexed(1, layout.Spacer{Width: unit.Dp(0)}.Layout),
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// Import and Save share one row: they are the two things that
			// write out of this bar, and one of them opens a dialog rather
			// than editing. The note beside them is whichever is worth
			// reading now — a failure first, since it says why nothing was
			// written, and the imported file otherwise.
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButton(gtx, &ui.unitImpClick, "Import frame's timeline")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.smallButtonDisabled(gtx, &ui.unitBoneKeySaveClick, "Save WHM", !ui.unitModelWhmDirty)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text, noteColor := ui.unitImpPath, cTextDim
							if ui.unitBoneKeyErr != "" {
								text, noteColor = ui.unitBoneKeyErr, rgb(0xB00020)
							}
							if text == "" {
								return layout.Dimensions{}
							}
							if text == ui.unitImpPath {
								text = filepath.Base(text)
							}
							l := material.Label(ui.th, unit.Sp(9), text)
							l.Color = noteColor
							l.MaxLines = 2
							return l.Layout(gtx)
						}),
					)
				})
		}),
	)
	// The hover and press states of the buttons are logged on every
	// change: a button the mouse crosses without ever hovering has its hit
	// area somewhere else, a press that never becomes a click broke between
	// the frames, and seeing them settles where a dead click is not.
	btnState := [26]bool{
		ui.unitBoneKeySetClick.Hovered(), ui.unitBoneKeySetClick.Pressed(),
		ui.unitBoneKeySetAllClick.Hovered(), ui.unitBoneKeySetAllClick.Pressed(),
		ui.unitBoneKeySaveClick.Hovered(), ui.unitBoneKeySaveClick.Pressed(),
		ui.unitBoneKeyAddPosClick.Hovered(), ui.unitBoneKeyAddPosClick.Pressed(),
		ui.unitBoneKeyAddRotClick.Hovered(), ui.unitBoneKeyAddRotClick.Pressed(),
		ui.unitBoneKeyDelClick.Hovered(), ui.unitBoneKeyDelClick.Pressed(),
		ui.unitBoneKeyBeforeClick.Hovered(), ui.unitBoneKeyBeforeClick.Pressed(),
		ui.unitBoneKeyAfterClick.Hovered(), ui.unitBoneKeyAfterClick.Pressed(),
		ui.unitBoneMirClick[0].Hovered(), ui.unitBoneMirClick[0].Pressed(),
		ui.unitBoneMirClick[1].Hovered(), ui.unitBoneMirClick[1].Pressed(),
		ui.unitBoneMirClick[2].Hovered(), ui.unitBoneMirClick[2].Pressed(),
		ui.unitBoneKeyShiftBack.Hovered(), ui.unitBoneKeyShiftBack.Pressed(),
		ui.unitBoneKeyShiftFwd.Hovered(), ui.unitBoneKeyShiftFwd.Pressed(),
	}
	if btnState != ui.unitBoneKeyBtnLog {
		LogInfo("bone: buttons [Set hover press | SetAll hover press | Save hover press | +pos hover press | +rot hover press | Del hover press | Before hover press | After hover press | MirX | MirY | MirZ | Back | Fwd] = %v", btnState)
		ui.unitBoneKeyBtnLog = btnState
	}
	if importClicked {
		LogInfo("bone: Import frame's timeline pressed")
		ui.openUnitImportDialog()
	}
	if setClicked {
		LogInfo("bone: Set pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.unitBonePosEd), boneFieldTexts(&ui.unitBoneRotEd))
		// Set reads what was typed over: only a group with a changed axis is
		// applied, so a bone with keys of just one kind does not error on the
		// other, and an untouched group is a no-op rather than a surprise.
		any := false
		var firstErr string
		for mode := 0; mode < 2; mode++ {
			changed, _ := ui.modifiedBoneAxes(mode)
			if len(changed) == 0 {
				continue
			}
			any = true
			LogInfo("bone: Set applies the %s group, changed axes %v", boneGroupLog(mode), changed)
			ui.applyBoneKeyFields(v, mode)
			if ui.unitBoneKeyErr != "" && firstErr == "" {
				firstErr = ui.unitBoneKeyErr
			}
		}
		switch {
		case !any:
			ui.unitBoneKeyErr = "no values were changed"
		case firstErr != "":
			ui.unitBoneKeyErr = firstErr
		}
	}
	if setAllClicked {
		LogInfo("bone: Set all pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.unitBonePosEd), boneFieldTexts(&ui.unitBoneRotEd))
		ui.setAllBoneKeyFields(v)
	}
	if saveClicked {
		LogInfo("bone: Save pressed (dirty=%v)", ui.unitModelWhmDirty)
		ui.saveUnitModelWhm()
	}
	if addPosClicked {
		ui.addBoneKeyAtPlayhead(v, 0)
	}
	if addRotClicked {
		ui.addBoneKeyAtPlayhead(v, 1)
	}
	if beforeClicked {
		ui.addBoneKeysAround(v, -1)
	}
	if afterClicked {
		ui.addBoneKeysAround(v, 1)
	}
	if delClicked {
		ui.deleteBoneKeysAction(v)
	}
	for axis := 0; axis < 3; axis++ {
		if mirClicked[axis] {
			ui.mirrorBoneRotationAction(v, axis)
		}
	}
	if shiftBackClicked {
		ui.shiftBoneKeysAction(v, -1)
	}
	if shiftFwdClicked {
		ui.shiftBoneKeysAction(v, 1)
	}
	if undoClicked {
		LogInfo("bone: Undo pressed (%d step(s) on the stack)", len(ui.unitBoneUndo))
		ui.undoBoneKeyEdit(v)
	}
	return dims
}

// boneFieldAxes names the three fields of a group, in the order they are laid
// out, so an error can point at the axis that would not parse.
var boneFieldAxes = [3]string{"X", "Y", "Z"}

// boneGroupLog names a group for the log: 0 is the position row, 1 the
// rotation row.
func boneGroupLog(mode int) string {
	if mode == 1 {
		return "Rot"
	}
	return "Pos"
}

// boneFieldTexts is one group's three fields as a single loggable line.
func boneFieldTexts(eds *[3]widget.Editor) string {
	return strings.TrimSpace(eds[0].Text()) + " " +
		strings.TrimSpace(eds[1].Text()) + " " +
		strings.TrimSpace(eds[2].Text())
}

// unitBoneKeyFieldsRow is one row of the pose bar's typed group: the group's
// name, then X, Y and Z. Each field submits on Enter, which is what applies
// the triple to the key it belongs to.
func (ui *uiState) unitBoneKeyFieldsRow(gtx layout.Context, title string, eds *[3]widget.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), title)
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitBoneKeyField(gtx, boneFieldAxes[0], &eds[0])
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitBoneKeyField(gtx, boneFieldAxes[1], &eds[1])
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitBoneKeyField(gtx, boneFieldAxes[2], &eds[2])
		}),
	)
}

// unitBoneKeyField is one typed axis: its letter, and a small bordered editor
// that submits on Enter. The fixed width keeps the three of a row aligned and
// stops a long value from pushing its neighbours out of the panel.
func (ui *uiState) unitBoneKeyField(gtx layout.Context, axis string, ed *widget.Editor) layout.Dimensions {
	ed.SingleLine = true
	ed.Submit = true
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), axis)
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(2)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Pt(gtx.Dp(56), gtx.Dp(20))
			gtx.Constraints.Max = image.Pt(gtx.Dp(56), gtx.Dp(22))
			b := widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
			return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := material.Editor(ui.th, ed, "")
					w.TextSize = unit.Sp(10)
					w.HintColor = cTextDim
					return w.Layout(gtx)
				})
			})
		}),
	)
}

// updateBoneKeyEditors drains the typed fields' events, applying an Enter:
// the position group writes its XYZ triple into the target position key, the
// rotation group turns its XYZ degrees into the quaternion that key holds.
func (ui *uiState) updateBoneKeyEditors(gtx layout.Context, v *modelViewer) {
	// Which field holds the caret is logged on change: typing needs it and
	// Enter submits through it, so a caret that never lands on a field
	// explains both of them going quiet.
	var focus [6]bool
	for m, eds := range [2]*[3]widget.Editor{&ui.unitBonePosEd, &ui.unitBoneRotEd} {
		for i := range eds {
			focus[m*3+i] = eds[i].Focused(gtx)
		}
	}
	if focus != ui.unitBoneKeyFocusLog {
		LogInfo("bone: field focus — Pos X=%v Y=%v Z=%v, Rot X=%v Y=%v Z=%v",
			focus[0], focus[1], focus[2], focus[3], focus[4], focus[5])
		ui.unitBoneKeyFocusLog = focus
	}
	// Tab moves the caret to the next of the six fields and Shift+Tab to the
	// previous one, wrapping round. The editor does not take Tab itself, so it
	// is read here, before the editors drain their own events.
	fields := [2]*[3]widget.Editor{&ui.unitBonePosEd, &ui.unitBoneRotEd}
	for flat := 0; flat < 6; flat++ {
		tag := &fields[flat/3][flat%3]
		for {
			ev, ok := gtx.Event(key.Filter{Focus: tag, Name: key.NameTab, Optional: key.ModShift})
			if !ok {
				break
			}
			ke, isKey := ev.(key.Event)
			if !isKey || ke.State != key.Press {
				continue
			}
			next := (flat + 1) % 6
			if ke.Modifiers.Contain(key.ModShift) {
				next = (flat + 5) % 6
			}
			gtx.Execute(key.FocusCmd{Tag: &fields[next/3][next%3]})
		}
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.unitBonePosEd, &ui.unitBoneRotEd} {
		for i := range eds {
			for {
				ev, ok := eds[i].Update(gtx)
				if !ok {
					break
				}
				if _, submit := ev.(widget.SubmitEvent); submit {
					LogInfo("bone: Enter in the %s group: fields=[%s]", boneGroupLog(mode), boneFieldTexts(eds))
					ui.applyBoneKeyFields(v, mode)
					// Enter applies, then moves the caret to the next field: the
					// six fields run Pos X..Z then Rot X..Z and wrap round, so a
					// row of values is typed with Enter alone.
					flat := (mode*3 + i + 1) % 6
					fields := [2]*[3]widget.Editor{&ui.unitBonePosEd, &ui.unitBoneRotEd}
					gtx.Execute(key.FocusCmd{Tag: &fields[flat/3][flat%3]})
				}
			}
		}
	}
}

// applyBoneKeyFields reads one triple out of the typed fields and writes it
// into the key it belongs to — the picked key when it is of that kind, the
// nearest key of that kind to the frame on show when it is not. A value that
// is not a number, or a bone with no key of that kind, is reported in the
// note beside Save rather than thrown away silently.
func (ui *uiState) applyBoneKeyFields(v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	val, err := ui.parseBoneGroup(mode)
	if err != nil {
		ui.unitBoneKeyErr = err.Error()
		return
	}
	frame, _ := v.currentFrame()
	target := v.boneKeyTarget(frame, mode)
	LogInfo("bone: apply %s: parsed=[%.3f %.3f %.3f] frame=%d target=%d picked(idx=%d mode=%d)",
		boneGroupLog(mode), val[0], val[1], val[2], frame, target, v.boneKeyIndex, v.boneKeyMode)
	if target < 0 {
		LogInfo("bone: apply %s: no target key in this clip", boneGroupLog(mode))
		ui.unitBoneKeyErr = fmt.Sprintf("this bone has no %s key in the clip", kind)
		return
	}
	v.boneKeyIndex, v.boneKeyMode = target, mode
	ok := v.setBoneKeyPosition([3]float32{float32(val[0]), float32(val[1]), float32(val[2])})
	if mode == 1 {
		ok = v.setBoneKeyRotation(quatFromEulerDeg(val[0], val[1], val[2]))
	}
	if !ok {
		LogWarn("bone: apply %s: the write was refused (bone=%d mode=%d idx=%d)", boneGroupLog(mode), v.boneIndex, v.boneKeyMode, v.boneKeyIndex)
		ui.unitBoneKeyErr = fmt.Sprintf("the %s key could not be written", kind)
		return
	}
	LogInfo("bone: apply %s: written ok, dirty=%v", boneGroupLog(mode), ui.unitModelWhmDirty)
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	// The playhead is wound to the key that was just written so the edit is
	// on show, then the group shows what the key now holds.
	v.windToPickedBoneKey()
	// The group is refilled from the key, so what is shown is what was
	// stored: a rotation comes back as the degrees its quaternion means.
	ui.fillBoneKeyFields(v, mode)
}

// parseBoneGroup reads one group of the typed fields as numbers, naming the
// axis that would not parse. An Enter and both buttons go through it, so a
// value is read the same way however it is applied.
func (ui *uiState) parseBoneGroup(mode int) ([3]float64, error) {
	var val [3]float64
	eds := &ui.unitBonePosEd
	kind := "position"
	if mode == 1 {
		eds = &ui.unitBoneRotEd
		kind = "rotation"
	}
	for i := range eds {
		spec := bonePositionField
		if mode == 1 {
			spec = boneRotationField
		}
		f, _, err := spec.Parse(eds[i].Text())
		if err != nil {
			return val, fmt.Errorf("%s %s: %v", kind, boneFieldAxes[i], err)
		}
		val[i] = f
	}
	return val, nil
}

// modifiedBoneAxes returns the axes of a group whose typed text differs from
// what the field was last filled with — the axes Set and Set all have to
// write — and whether anything changed at all. A field that does not parse
// still counts as changed, so applying it reports the problem rather than
// skipping what was typed.
func (ui *uiState) modifiedBoneAxes(mode int) ([]int, bool) {
	eds := &ui.unitBonePosEd
	if mode == 1 {
		eds = &ui.unitBoneRotEd
	}
	var axes []int
	for i := range eds {
		if strings.TrimSpace(eds[i].Text()) != ui.unitBoneKeyFillText[mode][i] {
			axes = append(axes, i)
		}
	}
	return axes, len(axes) > 0
}

// setAllBoneKeyFields writes only the axes the user changed into every key of
// that kind the selected bone has in the clip: a Rot X of 180 typed over one
// frame becomes a Rot X of 180 on every rotation key, while the axes left
// alone keep their own per-key values. Everything is read before anything is
// written, so a field that does not parse leaves the keys untouched.
func (ui *uiState) setAllBoneKeyFields(v *modelViewer) {
	if v == nil || v.anim == nil || v.boneIndex < 0 {
		ui.unitBoneKeyErr = "no bone is picked"
		return
	}
	t := v.boneTrack()
	if t == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	axes0, ch0 := ui.modifiedBoneAxes(0)
	axes1, ch1 := ui.modifiedBoneAxes(1)
	LogInfo("bone: Set all: changed pos-axes=%v rot-axes=%v", axes0, axes1)
	if !ch0 && !ch1 {
		ui.unitBoneKeyErr = "no values were changed"
		return
	}
	var val0, val1 [3]float64
	var err error
	if ch0 {
		if val0, err = ui.parseBoneGroup(0); err != nil {
			ui.unitBoneKeyErr = err.Error()
			return
		}
	}
	if ch1 {
		if val1, err = ui.parseBoneGroup(1); err != nil {
			ui.unitBoneKeyErr = err.Error()
			return
		}
	}
	snap, haveSnap := v.snapshotBoneTrack()
	written := 0
	if ch0 {
		for i := range t.PosKey {
			p := t.PosKey[i].V
			for _, ax := range axes0 {
				p[ax] = float32(val0[ax])
			}
			t.PosKey[i].V = p
		}
		written += len(t.PosKey)
	}
	if ch1 {
		for i := range t.RotKey {
			e := eulerDegFromQuat(t.RotKey[i].V)
			for _, ax := range axes1 {
				e[ax] = val1[ax]
			}
			t.RotKey[i].V = quatFromEulerDeg(e[0], e[1], e[2])
		}
		written += len(t.RotKey)
	}
	if written == 0 {
		LogInfo("bone: Set all: no keys of the changed kind to write")
		ui.unitBoneKeyErr = "the bone has no keys of the changed kind in this clip"
		return
	}
	// The pose cache predates the writes, so the viewport evaluates it again.
	v.poseValid = false
	v.invalidate()
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	ui.pushBoneUndo(snap, haveSnap)
	name := "?"
	if v.boneIndex < len(v.model.Bones) {
		name = v.model.Bones[v.boneIndex].Name
	}
	LogInfo("bone: Set all: %d key(s) written for bone %s, dirty=%v", written, name, ui.unitModelWhmDirty)
	ui.statusText = fmt.Sprintf("Set changed values on %d key(s) of bone %s", written, name)
}

// syncBoneKeyFields keeps the typed fields on the keys a typed apply would
// write: the picked key for its own group, the nearest key of the other kind
// to the frame on show for the other.
//
// A change of selection — another clip, another bone, another picked key —
// refills both groups whatever the cursor is doing: the fields must show the
// bone that is now picked, not the one that was typed into before the switch,
// or Enter would write the old bone's values into the new one. A change of
// frame alone only refills the groups the cursor is not in, so typing
// survives the playhead being moved.
func (ui *uiState) syncBoneKeyFields(gtx layout.Context, v *modelViewer) {
	if v == nil {
		return
	}
	frame, _ := v.currentFrame()
	sel := "no clip"
	if v.anim != nil {
		sel = fmt.Sprintf("%s|%d|%d|%d", v.anim.Name, v.boneIndex, v.boneKeyMode, v.boneKeyIndex)
	}
	selChanged := sel != ui.unitBoneKeyBound
	if !selChanged && frame == ui.unitBoneKeyFrame {
		return
	}
	prevSel := ui.unitBoneKeyBound
	ui.unitBoneKeyBound = sel
	ui.unitBoneKeyFrame = frame
	if selChanged {
		LogInfo("bone: selection changed %q -> %q: refilling both groups", prevSel, sel)
		ui.unitBoneKeyErr = ""
		ui.fillBoneKeyFields(v, 0)
		ui.fillBoneKeyFields(v, 1)
		return
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.unitBonePosEd, &ui.unitBoneRotEd} {
		if eds[0].Focused(gtx) || eds[1].Focused(gtx) || eds[2].Focused(gtx) {
			continue
		}
		ui.fillBoneKeyFields(v, mode)
	}
}

// fillBoneKeyFields puts one group of fields on the values of the key a typed
// apply of that kind would write, or empties the group when there is no such
// key: an apply into nothing is not offered, and the fields say so by being
// blank.
func (ui *uiState) fillBoneKeyFields(v *modelViewer, mode int) {
	eds := &ui.unitBonePosEd
	if mode == 1 {
		eds = &ui.unitBoneRotEd
	}
	var vals [3]float64
	ok := false
	if v != nil {
		frame, _ := v.currentFrame()
		if idx := v.boneKeyTarget(frame, mode); idx >= 0 {
			if t := v.boneTrack(); t != nil {
				if mode == 0 && idx < len(t.PosKey) {
					p := t.PosKey[idx].V
					vals = [3]float64{float64(p[0]), float64(p[1]), float64(p[2])}
					ok = true
				} else if mode == 1 && idx < len(t.RotKey) {
					vals = boneAngleDegs(eulerDegFromQuat(t.RotKey[idx].V))
					ok = true
				}
			}
		}
	}
	prev := ui.unitBoneKeyFillText[mode]
	if mode == 1 {
		// While a slider is under the hand the three sliders hold the
		// trio being written and the fields follow them. Reading the
		// key back mid-drag would show its canonical decomposition —
		// a different triple for the same rotation once an axis passes
		// ±90° — and refilling the two idle sliders from that fed the
		// flipped triple into the next frame's write, so all three
		// jumped back and forth every frame. With no hand on a slider,
		// everything follows the key again.
		held := false
		for i := range ui.unitBoneAngleSlid {
			if ui.unitBoneAngleSlid[i].Dragging() {
				held = true
				break
			}
		}
		if held && ok {
			vals = [3]float64{
				boneAngleSliderDeg(ui.unitBoneAngleSlid[0].Value),
				boneAngleSliderDeg(ui.unitBoneAngleSlid[1].Value),
				boneAngleSliderDeg(ui.unitBoneAngleSlid[2].Value),
			}
		} else if !held {
			for i := range ui.unitBoneAngleSlid {
				frac := float32(0.5)
				if ok {
					frac = boneAngleSliderFrac(vals[i])
				}
				ui.unitBoneAngleSlid[i].Value = frac
			}
		}
	}
	for i := range eds {
		text := ""
		if ok {
			if vals[i] == 0 {
				vals[i] = 0 // keep -0 from printing as "-0.000"
			}
			text = fmt.Sprintf("%.3f", vals[i])
		}
		// What the field showed is what a later change is measured against:
		// Set and Set all write only the axes typed over.
		ui.unitBoneKeyFillText[mode][i] = text
		eds[i].SetText(text)
	}
	if now := ui.unitBoneKeyFillText[mode]; now != prev {
		frame := -1
		if v != nil {
			frame, _ = v.currentFrame()
		}
		LogInfo("bone: fill %s frame=%d: [%s] (was [%s])", boneGroupLog(mode), frame,
			strings.Join(now[:], " "), strings.Join(prev[:], " "))
	}
}

// maxAnimFrames is the largest frame count the frames field takes. A clip is a
// few hundred frames long in the shipped models, and a number in the millions
// only makes every key land on the same frame.
const maxAnimFrames = 100000

// unitAnimFramesRow is the clip's frame count: the number typed in, the Set
// button beside it, and on the right what that number is in the file — the
// clip's length in seconds and the rate the two of them work out to.
func (ui *uiState) unitAnimFramesRow(gtx layout.Context, v *modelViewer) layout.Dimensions {
	hint := "no clip on show"
	canSet := v != nil && v.anim != nil
	if canSet {
		hint = fmt.Sprintf("%.2f s · %.1f fps", animClipSeconds(v.anim), animFramesPerSecond(v.anim))
	}
	// A count the field would not take says why where the length is shown: it
	// is the one place the frames row has to report in, and it is on screen in
	// every list mode.
	hintColor := cTextDim
	if ui.unitAnimFramesErr != "" {
		hint = ui.unitAnimFramesErr
		hintColor = rgb(0xB00020)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), "Frames")
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.unitBoneKeyField(gtx, "", &ui.unitAnimFramesEd)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.smallButtonDisabled(gtx, &ui.unitAnimFramesSetClick, "Set", !canSet)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(8), hint)
			l.Color = hintColor
			l.MaxLines = 2
			return l.Layout(gtx)
		}),
	)
}

// syncAnimFramesField keeps the frames field on the clip on show. The field is
// refilled when the clip or its count changes — another clip, another model, a
// count just applied — and left alone otherwise, so a number being typed over
// is not taken away mid-word.
func (ui *uiState) syncAnimFramesField(v *modelViewer) {
	key, text := "no clip", ""
	if v != nil && v.anim != nil {
		key = fmt.Sprintf("%s|%d", v.anim.Name, v.anim.NumFrames)
		text = strconv.Itoa(v.anim.NumFrames)
	}
	if key == ui.unitAnimFramesSync {
		return
	}
	ui.unitAnimFramesSync = key
	ui.unitAnimFramesEd.SetText(text)
}

// applyAnimFrameCount is what Set beside the frames field and Enter in it do:
// the clip is counted out in the number typed in, and the file is marked dirty
// for Save WHM.
//
// Key times are stored normalised over the clip, so the count is a grid rather
// than a hand on a clock: the same clip counted in twenty frames and in forty
// holds the same poses at the same fractions, and the strip, the frame readout
// and the keyframe rows simply count them out in the new number. The rate is
// what is held fixed: fps is frames over seconds, so a new count keeps the
// rate it had only by moving the duration by the same ratio — the clip is
// counted out at the same rate over a longer or shorter length — and the
// header keeps showing the rate the clip had before the change.
func (ui *uiState) applyAnimFrameCount(v *modelViewer) {
	if v == nil || v.anim == nil {
		ui.unitAnimFramesErr = "no clip is on show"
		return
	}
	text := strings.TrimSpace(ui.unitAnimFramesEd.Text())
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 {
		ui.unitAnimFramesErr = fmt.Sprintf("%q is not a frame count — a whole number of at least 1", text)
		return
	}
	if n > maxAnimFrames {
		ui.unitAnimFramesErr = fmt.Sprintf("%d is past the %d frames a clip is given here", n, maxAnimFrames)
		return
	}
	old := v.anim.NumFrames
	if n == old {
		ui.unitAnimFramesErr = ""
		ui.statusText = fmt.Sprintf("Clip %q is already %d frames", v.anim.Name, n)
		return
	}
	// The rate is taken before the count moves and the duration is put back
	// to match the new count: fps is frames over seconds, so holding the rate
	// means the length follows the count by the same ratio. A clip with no
	// duration of its own rates at the 20 fps fallback and is given the
	// length that goes with it.
	fps := animFramesPerSecond(v.anim)
	v.anim.NumFrames = n
	if fps > 0 {
		v.anim.Duration = float32(float64(n) / fps)
	}
	v.poseValid = false
	v.invalidate()
	ui.unitModelWhmDirty = true
	ui.unitAnimFramesErr = ""
	ui.statusText = fmt.Sprintf("Clip %q: %d frames (was %d) at %.1f fps — Save WHM writes it",
		v.anim.Name, n, old, animFramesPerSecond(v.anim))
	LogInfo("bone: clip %q frames %d -> %d (%.2f s at %.1f fps, dirty=%v)",
		v.anim.Name, old, n, animClipSeconds(v.anim), animFramesPerSecond(v.anim), ui.unitModelWhmDirty)
}

// saveUnitModelWhm writes the edited bone keys back into the .whm: the file
// is read from the mod (overlay first, so an earlier save is included), the
// current animation's bone keys are rebuilt into that copy — values, times
// and counts alike, since keys can be added and removed on the strip — and
// the whole file goes back through the overlay. Everything this editor does
// not read survives intact: only the animation's bone section is rewritten,
// with the chunk sizes above it moved to match.
func (ui *uiState) saveUnitModelWhm() {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil || v.animIndex < 0 || v.animIndex >= len(v.model.Animations) {
		ui.unitBoneKeyErr = "no animation to save"
		return
	}
	// A press can reach the handler even while the button is drawn disabled,
	// and a save of untouched keys must not be a reason to rewrite the file.
	if !ui.unitModelWhmDirty {
		LogInfo("bone: Save skipped — no key was changed")
		ui.unitBoneKeyErr = "nothing to save — no key was changed"
		return
	}
	if ui.modTree == nil || ui.unitModelPath == "" {
		ui.unitBoneKeyErr = "no module is loaded"
		return
	}
	data, err := ui.modTree.ReadFile(ui.unitModelPath)
	if err != nil {
		ui.unitBoneKeyErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchAnimClip(data, &v.model.Model, v.animIndex, v.anim.NumFrames)
	if err != nil {
		ui.unitBoneKeyErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.modTree.WriteOverlayFile(ui.unitModelPath, patched)
	if err != nil {
		ui.unitBoneKeyErr = "save failed: " + err.Error()
		return
	}
	ui.unitModelWhmDirty = false
	ui.unitBoneKeyErr = ""
	LogInfo("bone: Save WHM ok: %s (%d bytes patched)", filepath.Base(ui.unitModelPath), len(patched))
	ui.statusText = fmt.Sprintf("Saved bone keys → %s (%d bytes)", disk, len(patched))
	ui.markStateDirty()
	log.Printf("saved whm bone keys %q -> %q (%d bytes)", ui.unitModelPath, disk, len(patched))
}

// addBoneKeyAtPlayhead is what the "+ pos" and "+ rot" buttons do: the picked
// bone gets a key of that kind on the frame the playhead is on, holding the
// pose the viewport is showing — the same value "Set ... from pose" would
// write — and the file is marked dirty for Save WHM. A key of that kind
// already on the frame is picked instead of a second one being created, and
// the note beside Save says which of the two happened.
func (ui *uiState) addBoneKeyAtPlayhead(v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return
	}
	if v.boneIndex < 0 {
		ui.unitBoneKeyErr = "no bone is picked"
		return
	}
	if v.boneTrack() == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	snap, haveSnap := v.snapshotBoneTrack()
	frame, _ := v.currentFrame()
	if !v.addBoneKeyAtFrame(frame, mode) {
		// The key at the frame was picked all the same, so the fields show
		// what it holds and the note says why nothing was created.
		ui.unitBoneKeyErr = fmt.Sprintf("frame %d already has a %s key", frame+1, kind)
		ui.fillBoneKeyFields(v, 0)
		ui.fillBoneKeyFields(v, 1)
		return
	}
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	ui.pushBoneUndo(snap, haveSnap)
	ui.statusText = fmt.Sprintf("Added a %s key on frame %d", kind, frame+1)
	LogInfo("bone: %s key added on frame %d for bone %q (dirty=%v)",
		kind, frame+1, v.boneTrack().Name, ui.unitModelWhmDirty)
}

// deleteBoneKeysAction is what the "Delete key" button does: every row the
// list has picked leaves the picked bone's track — or, with nothing picked on
// the list, the single key the strip or the playhead had picked — and the file
// is marked dirty for Save WHM. The note and the status line say how many keys
// went and whose they were, so removing several is something the reader can
// count back from the log.
func (ui *uiState) deleteBoneKeysAction(v *modelViewer) {
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return
	}
	if v.boneTrack() == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := ui.boneKeyActionRefs(v)
	if len(refs) == 0 {
		ui.unitBoneKeyErr = "no key is picked"
		return
	}
	name := v.boneTrack().Name
	snap, haveSnap := v.snapshotBoneTrack()
	removed := 0
	for _, ref := range refs {
		if v.deleteBoneKeyAt(ref.frame, ref.mode) {
			removed++
		}
	}
	if removed == 0 {
		ui.unitBoneKeyErr = "no key could be removed"
		return
	}
	// Nothing on the list is picked any more: the keys the selection named
	// are gone, and leaving the selection behind would aim the next button at
	// frames that now hold other keys or nothing at all.
	ui.unitBoneKeySel = nil
	ui.unitBoneKeySelAnchor = boneKeyRef{mode: -1}
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	ui.pushBoneUndo(snap, haveSnap)
	msg := fmt.Sprintf("Removed %d key(s) of bone %s", removed, name)
	ui.statusText = msg
	LogInfo("bone: %s (dirty=%v)", msg, ui.unitModelWhmDirty)
}

// addBoneKeysAround is what "+ before" and "+ after" do (dir −1 and +1): every
// picked row gets a key of its own kind on the nearest frame in that direction
// that carries no key of that kind yet, holding what the row holds — the clip's
// motion is extended rather than pulled towards the pose the viewport happens
// to be showing.
//
// The rows are walked outermost first, so two picked rows standing next to
// each other each get a frame of their own instead of the second one finding
// the first's taken. What the list shows picked afterwards is the keys that
// were just added, since editing or taking them away is what comes next.
func (ui *uiState) addBoneKeysAround(v *modelViewer, dir int) {
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return
	}
	if v.boneTrack() == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := ui.boneKeyActionRefs(v)
	if len(refs) == 0 {
		ui.unitBoneKeyErr = "no key is picked"
		return
	}
	side := "before"
	if dir > 0 {
		side = "after"
	}
	sort.Slice(refs, func(i, j int) bool {
		if dir < 0 {
			return refs[i].frame > refs[j].frame
		}
		return refs[i].frame < refs[j].frame
	})
	added := make([]boneKeyRef, 0, len(refs))
	blocked := 0
	for _, ref := range refs {
		f := v.freeBoneKeyFrame(ref.frame, ref.mode, dir)
		if f < 0 || !v.insertBoneKeyCopy(f, ref.mode, ref.frame) {
			blocked++
			continue
		}
		added = append(added, boneKeyRef{mode: ref.mode, frame: f})
	}
	if len(added) == 0 {
		ui.unitBoneKeyErr = fmt.Sprintf("no free frame %s those keys", side)
		LogInfo("bone: + %s: nothing was added, %d row(s) had no free frame", side, blocked)
		return
	}
	ui.unitBoneKeySel = make(map[boneKeyRef]bool, len(added))
	for _, ref := range added {
		ui.unitBoneKeySel[ref] = true
	}
	ui.unitBoneKeySelAnchor = added[0]
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	if blocked > 0 {
		ui.unitBoneKeyErr = fmt.Sprintf("%d row(s) had no free frame", blocked)
	}
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	msg := fmt.Sprintf("Added %d key(s) %s the picked rows", len(added), side)
	ui.statusText = msg
	LogInfo("bone: %s (dirty=%v)", msg, ui.unitModelWhmDirty)
}

// boneAngleDegs applies boneAngleDeg to the three axes of a rotation.
func boneAngleDegs(v [3]float64) [3]float64 {
	for i := range v {
		v[i] = boneAngleDeg(v[i])
	}
	return v
}

// boneAngleSliderFrac is an axis angle as the slider's 0..1 position across
// the field range 1..360 degrees.
func boneAngleSliderFrac(angle float64) float32 {
	f := (boneAngleDeg(angle) - 1) / 359
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return float32(f)
}

// boneAngleSliderDeg is the inverse: the angle, in 1..360 degrees, that a
// slider position means.
func boneAngleSliderDeg(frac float32) float64 {
	d := 1 + float64(frac)*359
	if d < 1 {
		d = 1
	}
	if d > 360 {
		d = 360
	}
	return d
}

// applyBoneAngleSliders writes the three angle sliders into the rotation key
// the Rot fields show, while any of them is under the hand. The sliders' own
// Layout has already folded each drag into its Value, so this only reads the
// Value and stores it — the same key Enter in the Rot fields would write, with
// all three axes at once so the axes no slider touched keep what they hold.
// setBoneKeyRotation drops the pose cache, so the viewport turns the bone (and
// the mesh skinned to it) to the new angle on the very frame it is written.
func (ui *uiState) applyBoneAngleSliders(v *modelViewer) {
	dragging := false
	for i := range ui.unitBoneAngleSlid {
		if ui.unitBoneAngleSlid[i].Dragging() {
			dragging = true
			break
		}
	}
	if !dragging {
		ui.unitBoneSliderUndoArmed = false
		return
	}
	if v == nil || v.anim == nil {
		return
	}
	frame, _ := v.currentFrame()
	target := v.boneKeyTarget(frame, 1)
	if target < 0 {
		ui.unitBoneKeyErr = "this bone has no rotation key in the clip"
		return
	}
	t := v.boneTrack()
	if t == nil || target >= len(t.RotKey) {
		ui.unitBoneKeyErr = "the rotation key could not be written"
		return
	}
	v.boneKeyIndex, v.boneKeyMode = target, 1
	ang := [3]float64{
		boneAngleSliderDeg(ui.unitBoneAngleSlid[0].Value),
		boneAngleSliderDeg(ui.unitBoneAngleSlid[1].Value),
		boneAngleSliderDeg(ui.unitBoneAngleSlid[2].Value),
	}
	// Only a real move writes, so holding still mid-drag does not redraw
	// the viewport or add a log line every frame. The key is compared as
	// a rotation rather than as angles: the same turn has two euler
	// triples — (x, y, z) and (x±180, 180−y, z±180) — and an angle
	// comparison saw those as a change and rewrote the key every frame.
	if quatSame(t.RotKey[target].V, quatFromEulerDeg(ang[0], ang[1], ang[2])) {
		return
	}
	// One undo step for the whole drag: the first write of a gesture records
	// the angle as it was before the hand touched the slider, and the rest of
	// the drag reuses it until the button is let go.
	if !ui.unitBoneSliderUndoArmed {
		if snap, ok := v.snapshotBoneTrack(); ok {
			ui.pushBoneUndo(snap, true)
		}
		ui.unitBoneSliderUndoArmed = true
	}
	if !v.setBoneKeyRotation(quatFromEulerDeg(ang[0], ang[1], ang[2])) {
		ui.unitBoneKeyErr = "the rotation key could not be written"
		return
	}
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	// The typed Rot fields follow the key; the sliders are left alone
	// (fillBoneKeyFields skips the one being dragged).
	ui.fillBoneKeyFields(v, 1)
	LogInfo("bone: angle sliders wrote key %d: rot %.3f %.3f %.3f (dirty=%v)",
		target, ang[0], ang[1], ang[2], ui.unitModelWhmDirty)
}

// mirrorBoneRotationAction is what a "mirror X/Y/Z" button does: the rotation
// key the Rot fields show comes back with its angle about the chosen axis
// negated and the two other axes left as they were — the angle about that
// axis mirrored, which is then what the fields show.
func (ui *uiState) mirrorBoneRotationAction(v *modelViewer, axis int) {
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return
	}
	if v.boneTrack() == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	frame, _ := v.currentFrame()
	target := v.boneKeyTarget(frame, 1)
	if target < 0 {
		ui.unitBoneKeyErr = "this bone has no rotation key in the clip"
		return
	}
	v.boneKeyIndex, v.boneKeyMode = target, 1
	t := v.boneTrack()
	before := eulerDegFromQuat(t.RotKey[target].V)
	after := before
	after[axis] = -after[axis]
	snap, haveSnap := v.snapshotBoneTrack()
	if !v.setBoneKeyRotation(quatFromEulerDeg(after[0], after[1], after[2])) {
		ui.unitBoneKeyErr = "the rotation key could not be written"
		return
	}
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	v.windToPickedBoneKey()
	ui.fillBoneKeyFields(v, 1)
	ui.pushBoneUndo(snap, haveSnap)
	msg := fmt.Sprintf("Mirrored %s about %s", t.Name, boneFieldAxes[axis])
	ui.statusText = msg
	LogInfo("bone: %s (key=%d, rot %.3f %.3f %.3f -> %.3f %.3f %.3f, dirty=%v)",
		msg, target, before[0], before[1], before[2], after[0], after[1], after[2], ui.unitModelWhmDirty)
}

// shiftBoneKeysAction is what the back/forward buttons beside the frames field
// do: every picked row of the keyframe list — or, with none picked, the single
// key the strip or the playhead had picked — moves that many frames along the
// clip. A key whose new frame is already held by a key that is not shifting
// stays where it is rather than landing on top of it, and the selection
// follows the keys to the frames they ended on, so a run of presses carries a
// block along the strip without reselecting.
func (ui *uiState) shiftBoneKeysAction(v *modelViewer, dir int) {
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return
	}
	if v.boneTrack() == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := ui.boneKeyActionRefs(v)
	if len(refs) == 0 {
		ui.unitBoneKeyErr = "no key is picked"
		return
	}
	n := ui.boneKeyShiftFrames()
	pickedOld, hasPicked := v.pickedBoneKeyRef()
	snap, haveSnap := v.snapshotBoneTrack()
	newRefs, moved, blocked := v.shiftBoneKeys(refs, dir*n)
	if moved == 0 && blocked == 0 {
		ui.unitBoneKeyErr = "no key could be shifted"
		LogInfo("bone: shift %+d of %d key(s): nothing moved", dir*n, len(refs))
		return
	}
	// The selection follows the keys: it now names the frames they landed
	// on, so pressing again shifts them further without reselecting.
	ui.unitBoneKeySel = make(map[boneKeyRef]bool, len(newRefs))
	for _, ref := range newRefs {
		ui.unitBoneKeySel[ref] = true
	}
	ui.unitBoneKeySelAnchor = boneKeyRef{mode: -1}
	if len(newRefs) > 0 {
		ui.unitBoneKeySelAnchor = newRefs[0]
	}
	// The picked key follows its row to the frame it landed on, so the
	// strip marks and the typed fields point at it again after the move.
	for i, r := range refs {
		if hasPicked && r == pickedOld && i < len(newRefs) {
			if at := v.boneKeyIndexAtFrame(newRefs[i].frame, newRefs[i].mode); at >= 0 {
				v.boneKeyIndex, v.boneKeyMode = at, newRefs[i].mode
			}
			break
		}
	}
	if moved > 0 {
		ui.unitModelWhmDirty = true
		ui.pushBoneUndo(snap, haveSnap)
	}
	ui.unitBoneKeyErr = ""
	if blocked > 0 {
		ui.unitBoneKeyErr = fmt.Sprintf("%d key(s) stayed put (no room)", blocked)
	}
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	side := "forward"
	if dir < 0 {
		side = "back"
	}
	msg := fmt.Sprintf("Shifted %d key(s) %d frame(s) %s", moved, n, side)
	ui.statusText = msg
	LogInfo("bone: %s (%d row(s) shifted, %d held back, dirty=%v)", msg, len(newRefs), blocked, ui.unitModelWhmDirty)
}

// boneKeyShiftFrames is the shift the field holds, in frames: a whole number,
// with 1 standing in for an empty or unreadable field so the buttons still
// move something.
func (ui *uiState) boneKeyShiftFrames() int {
	s := strings.TrimSpace(ui.unitBoneKeyShiftEd.Text())
	n, err := strconv.Atoi(s)
	if err != nil {
		return 1
	}
	if n < 0 {
		n = -n
	}
	if n < 1 {
		n = 1
	}
	if n > 999 {
		n = 999
	}
	return n
}

// boneUndoLimit is how many key edits are kept for Undo. The stack holds one
// small copied track per step, so a dozen is plenty for a session and bounds
// what an extended editing binge can hold in memory.
const boneUndoLimit = 16

// pushBoneUndo records a snapshot taken before an edit as the next undo step.
// A snapshot with nothing in it (no clip or bone at the time) is dropped, and
// the stack is capped so it cannot grow without bound.
func (ui *uiState) pushBoneUndo(s boneTrackSnapshot, ok bool) {
	if !ok || (len(s.posKey) == 0 && len(s.rotKey) == 0 && s.anim == nil) {
		return
	}
	ui.unitBoneUndo = append(ui.unitBoneUndo, s)
	if len(ui.unitBoneUndo) > boneUndoLimit {
		ui.unitBoneUndo = ui.unitBoneUndo[len(ui.unitBoneUndo)-boneUndoLimit:]
	}
}

// undoBoneKeyEdit rolls the last key edit back: the most recent snapshot is
// restored into the clip and bone it was taken from, the selection is dropped
// (the frames it named may not exist in the restored track) and the typed
// fields are refilled from the restored keys. It reports whether anything was
// undone, which is what the note beside Save says.
func (ui *uiState) undoBoneKeyEdit(v *modelViewer) bool {
	if len(ui.unitBoneUndo) == 0 {
		ui.unitBoneKeyErr = "nothing to undo"
		return false
	}
	s := ui.unitBoneUndo[len(ui.unitBoneUndo)-1]
	ui.unitBoneUndo = ui.unitBoneUndo[:len(ui.unitBoneUndo)-1]
	if v == nil || !v.restoreBoneTrack(s) {
		ui.unitBoneKeyErr = "could not restore the previous keys"
		return false
	}
	// The restored track may not carry the keys the selection and picked key
	// pointed at, so both are cleared and the fields refill from what is
	// actually there now.
	ui.unitBoneKeySel = nil
	ui.unitBoneKeySelAnchor = boneKeyRef{mode: -1}
	ui.unitModelWhmDirty = true
	ui.unitBoneKeyErr = ""
	ui.fillBoneKeyFields(v, 0)
	ui.fillBoneKeyFields(v, 1)
	ui.statusText = "Restored the keys before the last edit"
	LogInfo("bone: undo restored track of bone %d: %d pos, %d rot keys (dirty=%v)",
		s.boneIndex, len(s.posKey), len(s.rotKey), ui.unitModelWhmDirty)
	return true
}

// boneTrackKeys is how many keys of the clip on show belong to bone i,
// counting position and rotation keys together. Zero means the bone is held
// still for the whole clip, which is the common case and worth saying out
// loud rather than leaving a reader to infer it from a still picture.
func boneTrackKeys(anim *WhmAnimation, i int) int {
	if anim == nil {
		return 0
	}
	n := 0
	for k := range anim.Bones {
		t := &anim.Bones[k]
		if t.Bone == i {
			n += len(t.PosKey) + len(t.RotKey)
		}
	}
	return n
}

// unitBoneKeyRow is one row of the picked bone's keyframe list. It shows the
// key's number, the frame it sits on, and its current value, and clicking it
// picks that key for editing in the buttons below the list.
func (ui *uiState) unitBoneKeyRow(gtx layout.Context, c *widget.Clickable, selected bool, text string) layout.Dimensions {
	bg := cPanelBg
	if selected {
		bg = cSelectBg
	}
	h := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
	sz := image.Pt(gtx.Constraints.Max.X, h)
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					ui.rrect(gtx, bg, gtx.Constraints.Max, unit.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(10), text)
					l.Color = cText
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
		)
	})
}

// unitViewportRow is one selectable line of the camera panel: the name, and a
// dimmer note on the right that says what it is.
func (ui *uiState) unitViewportRow(gtx layout.Context, c *widget.Clickable, selected bool, name, note string) layout.Dimensions {
	return ui.unitViewportRowIcon(gtx, c, selected, name, note, nil, false)
}

// unitViewportRowIcon is unitViewportRow with an optional icon column before
// the name. A list that shows an icon for every row — the Classic OE's
// Actions folder — reserves the column whether or not a row's icon was found
// (iconCol), so its names stay in one line; a list whose icons come and go
// passes iconCol false and shows them as they come.
func (ui *uiState) unitViewportRowIcon(gtx layout.Context, c *widget.Clickable, selected bool, name, note string, icon image.Image, iconCol bool) layout.Dimensions {
	bg := cPanelBg
	switch {
	case selected:
		bg = cSelectBg
	case c.Hovered():
		bg = cMenuHover
	}
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != cPanelBg {
					ui.rrect(gtx, bg, gtx.Constraints.Max, unit.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if icon == nil && !iconCol {
								return layout.Dimensions{}
							}
							return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return unitIconLayout(gtx, icon, 24, 18)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(10), unitViewportRowText(name))
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if note == "" {
								return layout.Dimensions{}
							}
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

// unitViewportRowText keeps a long clip name inside the panel, since the names
// are the game's and run to forty characters.
func unitViewportRowText(name string) string {
	if name == "" {
		return "(unnamed)"
	}
	if len(name) > 34 {
		return name[:31] + "…"
	}
	return name
}

// ensureUnitAnimClicks sizes the clip list's click targets. They are rebuilt
// per model, so a slice of the wrong length is replaced rather than grown
// into.
func (ui *uiState) ensureUnitAnimClicks(n int) {
	if len(ui.unitAnimClicks) != n {
		ui.unitAnimClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitMarkerClicks sizes the markers list's click targets, the same way
// as the clip list's: rebuilt per model rather than grown into.
func (ui *uiState) ensureUnitMarkerClicks(n int) {
	if len(ui.unitMarkerClicks) != n {
		ui.unitMarkerClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitBoneClicks sizes the bones list's click targets, the same way as
// the other two lists': rebuilt per model rather than grown into.
func (ui *uiState) ensureUnitBoneClicks(n int) {
	if len(ui.unitBoneClicks) != n {
		ui.unitBoneClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitBoneKeyClicks sizes the bone keyframe row click targets for the
// currently picked bone, rebuilt whenever the bone selection changes.
func (ui *uiState) ensureUnitBoneKeyClicks(n int) {
	if len(ui.unitBoneKeyClicks) != n {
		ui.unitBoneKeyClicks = make([]widget.Clickable, n)
	}
}

// unitModelLoaded reports whether the viewport has a model on screen, which is
// what a test checks rather than what it can see.
func (ui *uiState) unitModelLoaded() bool {
	return ui.unitModelViewer() != nil
}

// unitModelAnimNames are the clips on show, which the tests compare against.
func (ui *uiState) unitModelAnimNames() []string {
	v := ui.unitModelViewer()
	if v == nil || v.model == nil {
		return nil
	}
	out := make([]string, len(v.model.Animations))
	for i := range v.model.Animations {
		out[i] = v.model.Animations[i].Name
	}
	return out
}

// unitModelClipDuration is how long one pass of the clip on show takes, in
// seconds. A clip with no frame count is reported as zero rather than as the
// fallback rate, since a zero here means "nothing to wait for".
func unitModelClipDuration(v *modelViewer) float64 {
	if v == nil || v.anim == nil || v.anim.NumFrames <= 0 {
		return 0
	}
	return float64(v.anim.NumFrames) / animFramesPerSecond(v.anim)
}

func (ui *uiState) rrect(gtx layout.Context, bg color.NRGBA, sz image.Point, r unit.Dp) {
	paint.Fill(gtx.Ops, bg)
}
