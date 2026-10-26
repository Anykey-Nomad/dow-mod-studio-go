package app

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
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
func LayoutUnitViewportArea(ui *uiState, gtx layout.Context) layout.Dimensions {
	EnsureUnitModel(ui, gtx)
	sideW := gtx.Dp(unit.Dp(unitViewportSideDp))
	return panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, CurrentUnitSection(ui).middleTitle)
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
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
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
			l.Color = cTextDim
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

// unitModelSideEmpty is what the controls area shows when no model is loaded.
func UnitModelSideEmpty(ui *uiState, gtx layout.Context) layout.Dimensions {
	l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
	l.Color = cTextDim
	return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
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
func UnitModelTransport(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	// A unit with no model still gives the row its height: the canvas above is
	// measured against it, and a strip that came and went would move the view
	// every time a model finished loading.
	if t == nil || t.Model == nil {
		return unitPlaybackRow(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
			l.Color = cTextDim
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

// unitModelTransportButtons draws the viewport's shared playback row after its
// caller has handled modelControlClicks for this frame. It is content only —
// the strip it sits in gives the row its padding and its height.
func UnitModelTransportButtons(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	v := t.Model
	none := len(v.Model.Animations) == 0
	gtx = modelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelPlay, modelPlayLabel(v), none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelPrevAnim, "◀", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelNextAnim, "▶", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelStepBack, "|◀", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &t.ModelStepFwd, "▶|", none)
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
func UnitModelFrameLabel(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	text := "no frames to step through"
	if frame, frames := CurrentFrame(v); frames > 0 {
		text = fmt.Sprintf("frame %d of %d", frame+1, frames)
		if keys := clipKeyFrames(v.Anim); len(keys) > 0 {
			for i, k := range keys {
				if k == frame {
					text += fmt.Sprintf(" · key %d/%d", i+1, len(keys))
					break
				}
			}
		}
	}
	l := material.Label(ui.Th, unit.Sp(9), text)
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

// unitModelViewButtons is the viewport's reset row. WHM tabs reuse it as well.
func UnitModelViewButtons(ui *uiState, gtx layout.Context, t *fileTab, showListToggle bool) layout.Dimensions {
	gtx = modelRowHeight(gtx)
	return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return SmallButton(ui, gtx, &t.ModelReset, "Reset view")
				}),
				layout.Flexed(1, rowGap()),
			)
		})
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
			return ToggleButton(ui, gtx, &clicks[i], label, selected == i, cSelectBg)
		}))
	}
	return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// unitModelFileLabel is the model file the viewport is showing.
func UnitModelFileLabel(ui *uiState) string {
	return filepath.Base(ui.UnitModelPath)
}

// unitAnimList is every clip the model carries, the one being played marked, so
// the list is also the answer to "what can this thing do".
func UnitAnimList(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return UnitAnimListWithClicks(ui, gtx, v, &ui.UnitAnimClicks)
}

func UnitAnimListWithClicks(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	anims := v.Model.Animations
	if len(*clicks) != len(anims) {
		*clicks = make([]widget.Clickable, len(anims))
	}
	if len(anims) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This model carries no animations.")
		l.Color = cTextDim
		return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range anims {
		i := i
		a := anims[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			SelectAnim(v, i)
			v.Playing = true
			v.LastFrame = time.Time{}
		}
		right := fmt.Sprintf("%d frames", a.NumFrames)
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitViewportRow(ui, gtx, c, v.AnimIndex == i, a.Name, right)
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
func UnitClipOrMarkerSection(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.UnitMeshListMode {
		return UnitMeshListSection(ui, gtx, v)
	}
	return UnitModelListSectionOrFX(ui, gtx, v, ui.UnitListMode, ui.UnitFXListMode,
		&ui.UnitAnimClicks, &ui.UnitMarkerClicks, &ui.UnitBoneClicks, &ui.UnitFXClicks)
}

// unitMeshListSection is the Meshes tab of the side panel: the count header
// over the same eye-toggle rows the Events and Classic OE panels draw, so a
// mesh hidden here is hidden in every viewer of this model. The rows carry no
// scroller of their own — the side panel's body, which holds this section, is
// already scrolled.
func UnitMeshListSection(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	count := 0
	if v != nil && v.Model != nil {
		count = len(modelMeshNames(v.Model))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, fmt.Sprintf("Meshes (%d)", count))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshRows(ui, gtx) }),
	)
}

func UnitModelListContent(ui *uiState, gtx layout.Context, t *fileTab, mode int) layout.Dimensions {
	UnitListTrackModifiers(ui, gtx)
	return UnitModelListSectionOrFX(ui, gtx, t.Model, mode, mode == 3, &t.ModelAnimClicks, &t.ModelMarkerClicks, &t.ModelBoneClicks, &t.ModelFXClicks)
}

func UnitModelListSectionOrFX(ui *uiState, gtx layout.Context, v *modelViewer, mode int, fxMode bool, animClicks, markerClicks, boneClicks, fxClicks *[]widget.Clickable) layout.Dimensions {
	if fxMode {
		return UnitFXListWithHeader(ui, gtx, v, fxClicks)
	}
	return UnitModelListSection(ui, gtx, v, mode, animClicks, markerClicks, boneClicks)
}

func UnitFXListWithHeader(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	if v == nil || v.Model == nil {
		return sectionHeader(gtx, ui.Th, "FX")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, fmt.Sprintf("FX (%d)", len(v.FxEffects)))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ModelFxChecklist(ui, gtx, v, clicks) }),
	)
}

func UnitModelListSection(ui *uiState, gtx layout.Context, v *modelViewer, mode int, animClicks, markerClicks, boneClicks *[]widget.Clickable) layout.Dimensions {
	labels := [3]string{"Animations", "Markers", "Bones"}
	if mode < 0 || mode >= len(labels) {
		mode = 0
	}
	list := func(gtx layout.Context) layout.Dimensions { return UnitAnimListWithClicks(ui, gtx, v, animClicks) }
	switch mode {
	case 1:
		list = func(gtx layout.Context) layout.Dimensions { return UnitMarkerListWithClicks(ui, gtx, v, markerClicks) }
	case 2:
		list = func(gtx layout.Context) layout.Dimensions { return UnitBoneListWithClicks(ui, gtx, v, boneClicks) }
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, labels[mode]) }),
		layout.Rigid(list),
	)
}

// unitMarkerList is every attachment point the model carries, its name and the
// bone it hangs off. Picking one draws a red ring on the model at that marker,
// which is the only way to tell a dozen identically named muzzle points apart
// on a mesh that shows none of them.
func UnitMarkerList(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return UnitMarkerListWithClicks(ui, gtx, v, &ui.UnitMarkerClicks)
}

func UnitMarkerListWithClicks(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	markers := v.Model.Markers
	if len(*clicks) != len(markers) {
		*clicks = make([]widget.Clickable, len(markers))
	}
	if len(markers) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This model carries no markers.")
		l.Color = cTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range markers {
		i := i
		mk := markers[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			if v.MarkerIndex == i {
				v.MarkerIndex = -1
			} else {
				v.MarkerIndex = i
			}
			v.Redraw = true
		}
		note := mk.Parent
		if note == "" {
			note = "no bone"
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitViewportRow(ui, gtx, c, v.MarkerIndex == i, mk.Name, note)
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
func UnitBoneList(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return UnitBoneListWithClicks(ui, gtx, v, &ui.UnitBoneClicks)
}

func UnitBoneListWithClicks(ui *uiState, gtx layout.Context, v *modelViewer, clicks *[]widget.Clickable) layout.Dimensions {
	bones := v.Model.Bones
	if len(*clicks) != len(bones) {
		*clicks = make([]widget.Clickable, len(bones))
	}
	if len(bones) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This model carries no skeleton.")
		l.Color = cTextDim
		return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
	var children []layout.FlexChild
	for i := range bones {
		i := i
		bone := bones[i]
		c := &(*clicks)[i]
		if c.Clicked(gtx) {
			if v.BoneIndex == i {
				v.BoneIndex = -1
			} else {
				v.BoneIndex = i
			}
			v.Redraw = true
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
			return UnitViewportRow(ui, gtx, c, v.BoneIndex == i, name, note)
		}))
	}
	if v.BoneIndex >= 0 && v.BoneIndex < len(bones) {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneParams(ui, gtx, v, v.BoneIndex)
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
func UnitBoneParams(ui *uiState, gtx layout.Context, v *modelViewer, i int) layout.Dimensions {
	bone := v.Model.Bones[i]
	pose := Pose(v)
	pos := "—"
	if i < len(pose.Bones) {
		p := pose.Bones[i].MulPosition(fauxgl.Vector{})
		pos = fmt.Sprintf("%.3f, %.3f, %.3f", p.X, p.Y, p.Z)
	}
	rest := fmt.Sprintf("%.3f, %.3f, %.3f", bone.Pos[0], bone.Pos[1], bone.Pos[2])
	rows := [][2]string{
		{"Position", pos},
		{"Rest pose", rest},
		{"Keys here", fmt.Sprintf("%d", boneTrackKeys(v.Anim, i))},
	}
	children := make([]layout.FlexChild, 0, len(rows)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) })
	}))
	for idx, row := range rows {
		row, idx := row, idx
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, row[0], row[1], idx%2 == 0)
		}))
	}

	if v.Anim != nil {
		t := BoneTrack(v)
		if t != nil && v.BoneIndex == i && (len(t.PosKey) > 0 || len(t.RotKey) > 0) {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) })
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitBoneKeyListHeader(ui, gtx, v)
			}))
			refs := BoneKeyRefs(v)
			EnsureUnitBoneKeyClicks(ui, len(refs))
			BindBoneKeySelection(ui, v)
			keyIdx := 0
			for idx, k := range t.PosKey {
				ref := boneKeyRef{mode: 0, frame: frameAt(float64(k.Time), v.Anim.NumFrames)}
				c := &ui.UnitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					BoneKeyRowClicked(ui, v, ref, refs, UnitListModifiers(ui))
				}
				selected := BoneKeyRowPicked(ui, v, ref)
				text := fmt.Sprintf("pos %d  frame %d  %.3f, %.3f, %.3f", idx+1, ref.frame+1, k.V[0], k.V[1], k.V[2])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyRow(ui, gtx, c, selected, text)
				}))
				keyIdx++
			}
			for idx, k := range t.RotKey {
				ref := boneKeyRef{mode: 1, frame: frameAt(float64(k.Time), v.Anim.NumFrames)}
				c := &ui.UnitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					BoneKeyRowClicked(ui, v, ref, refs, UnitListModifiers(ui))
				}
				selected := BoneKeyRowPicked(ui, v, ref)
				text := fmt.Sprintf("rot %d  frame %d  %.3f, %.3f, %.3f, %.3f", idx+1, ref.frame+1, k.V[0], k.V[1], k.V[2], k.V[3])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyRow(ui, gtx, c, selected, text)
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
func UnitBoneKeyListHeader(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), "Keyframes")
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Flexed(1, rowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if n := len(PickedBoneKeyRefs(ui, v)); n > 1 {
				l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d picked", n))
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
func UnitListTrackModifiers(ui *uiState, gtx layout.Context) {
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
		if s := fmt.Sprintf("%q %v mods=%v", e.Name, e.State, e.Modifiers); s != ui.UnitBoneKeyModLog {
			LogInfo("bone: modifier key %s", s)
			ui.UnitBoneKeyModLog = s
		}
		switch e.Name {
		case key.NameCtrl:
			ui.UnitBoneKeyCtrlHeld = e.State != key.Release
		case key.NameShift:
			ui.UnitBoneKeyShiftHeld = e.State != key.Release
		}
	}
}

// unitListModifiers is the state of Ctrl and Shift at the moment a row is
// read: the tracked key events, ORed with the live OS state, since either one
// alone can miss a press on some backends.
func UnitListModifiers(ui *uiState) key.Modifiers {
	m := heldModifiers()
	if ui.UnitBoneKeyCtrlHeld {
		m |= key.ModCtrl
	}
	if ui.UnitBoneKeyShiftHeld {
		m |= key.ModShift
	}
	return m
}

// bindBoneKeySelection keeps the picked rows with the bone they were picked
// on: another bone or another clip starts an empty selection rather than
// carrying a set of frames over to keys that are not there. It runs where the
// rows are built, which is the only place both the selection and the bone it
// belongs to are on show.
func BindBoneKeySelection(ui *uiState, v *modelViewer) {
	bound := ""
	if v != nil {
		bound = fmt.Sprintf("%p|%d", v.Anim, v.BoneIndex)
	}
	if bound == ui.UnitBoneKeySelBound {
		return
	}
	LogInfo("bone: keyframe selection bound to %q -> %q: cleared", ui.UnitBoneKeySelBound, bound)
	ui.UnitBoneKeySelBound = bound
	ui.UnitBoneKeySel = nil
	ui.UnitBoneKeySelAnchor = boneKeyRef{mode: -1}
}

// pickedBoneKeyRefs is the rows of the list that are picked and whose keys
// are still on the track, in list order. Rows whose key has gone — a deletion
// elsewhere, a drag that carried it off its frame — leave the selection rather
// than pointing at whatever frame now carries that number.
func PickedBoneKeyRefs(ui *uiState, v *modelViewer) []boneKeyRef {
	if len(ui.UnitBoneKeySel) == 0 {
		return nil
	}
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeySel = nil
		ui.UnitBoneKeySelAnchor = boneKeyRef{mode: -1}
		return nil
	}
	var out []boneKeyRef
	for _, ref := range BoneKeyRefs(v) {
		if ui.UnitBoneKeySel[ref] {
			out = append(out, ref)
		}
	}
	if len(out) == len(ui.UnitBoneKeySel) {
		return out
	}
	fresh := make(map[boneKeyRef]bool, len(out))
	for _, ref := range out {
		fresh[ref] = true
	}
	LogInfo("bone: keyframe selection dropped %d row(s) whose keys are gone", len(ui.UnitBoneKeySel)-len(out))
	ui.UnitBoneKeySel = fresh
	if len(fresh) == 0 {
		ui.UnitBoneKeySelAnchor = boneKeyRef{mode: -1}
	}
	return out
}

// boneKeyActionRefs is what the pose bar's key buttons act on: the rows still
// picked on the list, and — when nothing on the list is picked — the single
// key the strip or the playhead had picked, which is all the buttons had
// before the list could hold a selection.
func BoneKeyActionRefs(ui *uiState, v *modelViewer) []boneKeyRef {
	if v == nil || v.Anim == nil {
		return nil
	}
	if refs := PickedBoneKeyRefs(ui, v); len(refs) > 0 {
		return refs
	}
	if ref, ok := PickedBoneKeyRef(v); ok {
		return []boneKeyRef{ref}
	}
	return nil
}

// boneKeyRowPicked says whether a row wears the selection colour. While the
// list holds a selection that is what decides; with nothing picked on the list
// the key the strip or the playhead picked is the one on show, which is what
// the list highlighted before it could hold a selection at all.
func BoneKeyRowPicked(ui *uiState, v *modelViewer, ref boneKeyRef) bool {
	if len(ui.UnitBoneKeySel) > 0 {
		return ui.UnitBoneKeySel[ref]
	}
	got, ok := PickedBoneKeyRef(v)
	return ok && got == ref
}

// boneKeyRowClicked is what a press on a row of the keyframe list does. A plain
// click picks that row alone; Ctrl+click adds or takes it away, which is how a
// set of rows to delete or to add keys around is gathered; Shift+click picks
// every row between the row the anchor stands on and this one. Whatever the
// modifiers did, the key the typed fields and the strip work on ends up on the
// row that was clicked while that row is among the picked ones — and on the
// last row still picked when it is not, or on none when nothing is left.
func BoneKeyRowClicked(ui *uiState, v *modelViewer, ref boneKeyRef, refs []boneKeyRef, mods key.Modifiers) {
	if v == nil || v.Anim == nil {
		return
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	if ui.UnitBoneKeySel == nil {
		ui.UnitBoneKeySel = make(map[boneKeyRef]bool)
	}
	switch {
	case shift && ui.UnitBoneKeySelAnchor.mode >= 0:
		lo, hi := -1, -1
		for i, r := range refs {
			if r == ui.UnitBoneKeySelAnchor {
				lo = i
			}
			if r == ref {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			// The anchor is not on the list any more: a Shift+click with
			// nothing to extend from picks the row it lands on.
			ui.UnitBoneKeySel = map[boneKeyRef]bool{ref: true}
			ui.UnitBoneKeySelAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.UnitBoneKeySel = make(map[boneKeyRef]bool, hi-lo+1)
		for _, r := range refs[lo : hi+1] {
			ui.UnitBoneKeySel[r] = true
		}
		LogInfo("bone: Shift+click spans %d row(s), frames %d..%d", hi-lo+1, refs[lo].frame, refs[hi].frame)
	case ctrl:
		if ui.UnitBoneKeySel[ref] {
			delete(ui.UnitBoneKeySel, ref)
			LogInfo("bone: Ctrl+click takes frame %d (%s) out of the selection", ref.frame, boneGroupLog(ref.mode))
		} else {
			ui.UnitBoneKeySel[ref] = true
			ui.UnitBoneKeySelAnchor = ref
			LogInfo("bone: Ctrl+click adds frame %d (%s) to the selection", ref.frame, boneGroupLog(ref.mode))
		}
	default:
		ui.UnitBoneKeySel = map[boneKeyRef]bool{ref: true}
		ui.UnitBoneKeySelAnchor = ref
	}
	// The picked key follows the click: the clicked row while it is still
	// among the picked ones, otherwise the last row still picked — in list
	// order, so the choice does not depend on map iteration — and none when
	// the selection is empty.
	picked := boneKeyRef{mode: -1}
	if ui.UnitBoneKeySel[ref] {
		picked = ref
	} else {
		for _, r := range refs {
			if ui.UnitBoneKeySel[r] {
				picked = r
			}
		}
	}
	if at := BoneKeyIndexAtFrame(v, picked.frame, picked.mode); picked.mode >= 0 && at >= 0 {
		v.BoneKeyIndex, v.BoneKeyMode = at, picked.mode
	} else {
		v.BoneKeyIndex, v.BoneKeyMode = -1, -1
	}
	v.Redraw = true
}

// pickFrameKeyOnTimeline is what the step buttons do after winding the
// playhead: with a bone picked, every key that bone holds on the frame they
// landed on joins the selection and one of them becomes the key the strip
// shows picked, so stepping through the clip walks the selection along the
// keys instead of leaving it behind where it started. A frame carrying no
// key of this bone leaves the selection as it was.
func PickFrameKeyOnTimeline(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil || v.BoneIndex < 0 || BoneTrack(v) == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	posAt := BoneKeyIndexAtFrame(v, frame, 0)
	rotAt := BoneKeyIndexAtFrame(v, frame, 1)
	if posAt < 0 && rotAt < 0 {
		return
	}
	// The key the strip picks follows the kind that was picked before
	// when it has a key on this frame, otherwise the other kind.
	mode := 1
	if rotAt < 0 || (v.BoneKeyMode == 0 && posAt >= 0) {
		mode = 0
	}
	if mode == 0 {
		v.BoneKeyIndex, v.BoneKeyMode = posAt, 0
	} else {
		v.BoneKeyIndex, v.BoneKeyMode = rotAt, 1
	}
	sel := map[boneKeyRef]bool{}
	if posAt >= 0 {
		sel[boneKeyRef{mode: 0, frame: frame}] = true
	}
	if rotAt >= 0 {
		sel[boneKeyRef{mode: 1, frame: frame}] = true
	}
	ui.UnitBoneKeySel = sel
	ui.UnitBoneKeySelAnchor = boneKeyRef{mode: mode, frame: frame}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
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
func UnitBonePoseBar(ui *uiState, gtx layout.Context) layout.Dimensions {
	v := UnitModelViewer(ui)
	if ui.UnitListMode != 2 || v == nil {
		// If the bar is not laid out, its buttons cannot see a press — while
		// the last drawn frame still shows them. The log says when this
		// becomes the case, which turns a dead click into a visible cause.
		if state := fmt.Sprintf("mode=%d viewer=%v", ui.UnitListMode, v != nil); state != ui.UnitBonePoseBarLog {
			LogInfo("bone: pose bar is not laid out (%s)", state)
			ui.UnitBonePoseBarLog = state
		}
		return layout.Dimensions{}
	}
	ui.UnitBonePoseBarLog = ""
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
	setClicked := ui.UnitBoneKeySetClick.Clicked(gtx)
	setAllClicked := ui.UnitBoneKeySetAllClick.Clicked(gtx)
	saveClicked := ui.UnitBoneKeySaveClick.Clicked(gtx)
	importClicked := ui.UnitImpClick.Clicked(gtx)
	addPosClicked := ui.UnitBoneKeyAddPosClick.Clicked(gtx)
	addRotClicked := ui.UnitBoneKeyAddRotClick.Clicked(gtx)
	beforeClicked := ui.UnitBoneKeyBeforeClick.Clicked(gtx)
	afterClicked := ui.UnitBoneKeyAfterClick.Clicked(gtx)
	delClicked := ui.UnitBoneKeyDelClick.Clicked(gtx)
	mirClicked := [3]bool{
		ui.UnitBoneMirClick[0].Clicked(gtx),
		ui.UnitBoneMirClick[1].Clicked(gtx),
		ui.UnitBoneMirClick[2].Clicked(gtx),
	}
	shiftBackClicked := ui.UnitBoneKeyShiftBack.Clicked(gtx)
	shiftFwdClicked := ui.UnitBoneKeyShiftFwd.Clicked(gtx)
	undoClicked := ui.UnitBoneKeyUndoClick.Clicked(gtx)
	// Events first, before the fields are laid out below: an Enter pressed
	// since the last frame applies its triple, and the group it belongs to
	// is refilled with the values now in the key.
	UpdateBoneKeyEditors(ui, gtx, v)
	SyncBoneKeyFields(ui, gtx, v)
	// The frames field keeps its events drained like the typed groups:
	// a press inside it takes the keyboard, Enter lets go of it again.
	for {
		ev, ok := ui.UnitBoneKeyShiftEd.Update(gtx)
		if !ok {
			break
		}
		if _, submit := ev.(widget.SubmitEvent); submit {
			ui.UnitBoneKeyShiftEd.Blur(gtx)
		}
	}
	// What the keyframe buttons act on — the picked bone's track to add a key
	// to, the rows of the list (or, with none picked, the key the strip picked)
	// to take away or to add keys beside — is read here, before the layout below
	// draws them disabled.
	canAddKey := v != nil && v.Anim != nil && v.BoneIndex >= 0 && BoneTrack(v) != nil
	actionRefs := BoneKeyActionRefs(ui, v)
	canDelKey := len(actionRefs) > 0
	canAddAround := canAddKey && len(actionRefs) > 0
	canShift := canDelKey
	canMir := false
	if v.Anim != nil {
		frame, _ := CurrentFrame(v)
		canMir = BoneKeyTarget(v, frame, 1) >= 0
	}
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) })
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyFieldsRow(ui, gtx, "Pos", &ui.UnitBonePosEd)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyFieldsRow(ui, gtx, "Rot", &ui.UnitBoneRotEd)
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
							l := material.Label(ui.Th, unit.Sp(10), "Ang")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
					}
					for i := range ui.UnitBoneAngleSlid {
						i := i
						children = append(children,
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(ui.Th, unit.Sp(9), boneFieldAxes[i])
								l.Color = cTextDim
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(56))
								gtx.Constraints.Max.X = gtx.Dp(unit.Dp(64))
								sl := material.Slider(ui.Th, &ui.UnitBoneAngleSlid[i])
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
					ApplyBoneAngleSliders(ui, v)
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
							l := material.Label(ui.Th, unit.Sp(10), "Mirror")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[0], "mirror X", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[1], "mirror Y", !canMir)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneMirClick[2], "mirror Z", !canMir)
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
							l := material.Label(ui.Th, unit.Sp(10), "Shift")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return UnitBoneKeyField(ui, gtx, "f", &ui.UnitBoneKeyShiftEd)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyShiftBack, "back", !canShift)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyShiftFwd, "fwd", !canShift)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "frames each press moves")
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
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAddPosClick, "+ pos", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAddRotClick, "+ rot", !canAddKey)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyDelClick, "Delete key", !canDelKey)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "drag a mark on the strip to move its key")
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
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyBeforeClick, "+ before", !canAddAround)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyAfterClick, "+ after", !canAddAround)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(8), "Ctrl+click picks more rows")
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
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeyUndoClick, "Undo", len(ui.UnitBoneUndo) == 0)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitBoneKeySetClick, "Set")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButton(ui, gtx, &ui.UnitBoneKeySetAllClick, "Set all")
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
							return SmallButton(ui, gtx, &ui.UnitImpClick, "Import frame's timeline")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return SmallButtonDisabled(ui, gtx, &ui.UnitBoneKeySaveClick, "Save WHM", !ui.UnitModelWhmDirty)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							text, noteColor := ui.UnitImpPath, cTextDim
							if ui.UnitBoneKeyErr != "" {
								text, noteColor = ui.UnitBoneKeyErr, rgb(0xB00020)
							}
							if text == "" {
								return layout.Dimensions{}
							}
							if text == ui.UnitImpPath {
								text = filepath.Base(text)
							}
							l := material.Label(ui.Th, unit.Sp(9), text)
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
		ui.UnitBoneKeySetClick.Hovered(), ui.UnitBoneKeySetClick.Pressed(),
		ui.UnitBoneKeySetAllClick.Hovered(), ui.UnitBoneKeySetAllClick.Pressed(),
		ui.UnitBoneKeySaveClick.Hovered(), ui.UnitBoneKeySaveClick.Pressed(),
		ui.UnitBoneKeyAddPosClick.Hovered(), ui.UnitBoneKeyAddPosClick.Pressed(),
		ui.UnitBoneKeyAddRotClick.Hovered(), ui.UnitBoneKeyAddRotClick.Pressed(),
		ui.UnitBoneKeyDelClick.Hovered(), ui.UnitBoneKeyDelClick.Pressed(),
		ui.UnitBoneKeyBeforeClick.Hovered(), ui.UnitBoneKeyBeforeClick.Pressed(),
		ui.UnitBoneKeyAfterClick.Hovered(), ui.UnitBoneKeyAfterClick.Pressed(),
		ui.UnitBoneMirClick[0].Hovered(), ui.UnitBoneMirClick[0].Pressed(),
		ui.UnitBoneMirClick[1].Hovered(), ui.UnitBoneMirClick[1].Pressed(),
		ui.UnitBoneMirClick[2].Hovered(), ui.UnitBoneMirClick[2].Pressed(),
		ui.UnitBoneKeyShiftBack.Hovered(), ui.UnitBoneKeyShiftBack.Pressed(),
		ui.UnitBoneKeyShiftFwd.Hovered(), ui.UnitBoneKeyShiftFwd.Pressed(),
	}
	if btnState != ui.UnitBoneKeyBtnLog {
		LogInfo("bone: buttons [Set hover press | SetAll hover press | Save hover press | +pos hover press | +rot hover press | Del hover press | Before hover press | After hover press | MirX | MirY | MirZ | Back | Fwd] = %v", btnState)
		ui.UnitBoneKeyBtnLog = btnState
	}
	if importClicked {
		LogInfo("bone: Import frame's timeline pressed")
		OpenUnitImportDialog(ui)
	}
	if setClicked {
		LogInfo("bone: Set pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.UnitBonePosEd), boneFieldTexts(&ui.UnitBoneRotEd))
		// Set reads what was typed over: only a group with a changed axis is
		// applied, so a bone with keys of just one kind does not error on the
		// other, and an untouched group is a no-op rather than a surprise.
		any := false
		var firstErr string
		for mode := 0; mode < 2; mode++ {
			changed, _ := ModifiedBoneAxes(ui, mode)
			if len(changed) == 0 {
				continue
			}
			any = true
			LogInfo("bone: Set applies the %s group, changed axes %v", boneGroupLog(mode), changed)
			ApplyBoneKeyFields(ui, v, mode)
			if ui.UnitBoneKeyErr != "" && firstErr == "" {
				firstErr = ui.UnitBoneKeyErr
			}
		}
		switch {
		case !any:
			ui.UnitBoneKeyErr = "no values were changed"
		case firstErr != "":
			ui.UnitBoneKeyErr = firstErr
		}
	}
	if setAllClicked {
		LogInfo("bone: Set all pressed: pos=[%s] rot=[%s]", boneFieldTexts(&ui.UnitBonePosEd), boneFieldTexts(&ui.UnitBoneRotEd))
		SetAllBoneKeyFields(ui, v)
	}
	if saveClicked {
		LogInfo("bone: Save pressed (dirty=%v)", ui.UnitModelWhmDirty)
		SaveUnitModelWhm(ui)
	}
	if addPosClicked {
		AddBoneKeyAtPlayhead(ui, v, 0)
	}
	if addRotClicked {
		AddBoneKeyAtPlayhead(ui, v, 1)
	}
	if beforeClicked {
		AddBoneKeysAround(ui, v, -1)
	}
	if afterClicked {
		AddBoneKeysAround(ui, v, 1)
	}
	if delClicked {
		DeleteBoneKeysAction(ui, v)
	}
	for axis := 0; axis < 3; axis++ {
		if mirClicked[axis] {
			MirrorBoneRotationAction(ui, v, axis)
		}
	}
	if shiftBackClicked {
		ShiftBoneKeysAction(ui, v, -1)
	}
	if shiftFwdClicked {
		ShiftBoneKeysAction(ui, v, 1)
	}
	if undoClicked {
		LogInfo("bone: Undo pressed (%d step(s) on the stack)", len(ui.UnitBoneUndo))
		UndoBoneKeyEdit(ui, v)
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
func UnitBoneKeyFieldsRow(ui *uiState, gtx layout.Context, title string, eds *[3]widget.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), title)
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, boneFieldAxes[0], &eds[0])
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, boneFieldAxes[1], &eds[1])
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, boneFieldAxes[2], &eds[2])
		}),
	)
}

// unitBoneKeyField is one typed axis: its letter, and a small bordered editor
// that submits on Enter. The fixed width keeps the three of a row aligned and
// stops a long value from pushing its neighbours out of the panel.
func UnitBoneKeyField(ui *uiState, gtx layout.Context, axis string, ed *widget.Editor) layout.Dimensions {
	ed.SingleLine = true
	ed.Submit = true
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), axis)
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
					w := material.Editor(ui.Th, ed, "")
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
func UpdateBoneKeyEditors(ui *uiState, gtx layout.Context, v *modelViewer) {
	// Which field holds the caret is logged on change: typing needs it and
	// Enter submits through it, so a caret that never lands on a field
	// explains both of them going quiet.
	var focus [6]bool
	for m, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		for i := range eds {
			focus[m*3+i] = eds[i].Focused(gtx)
		}
	}
	if focus != ui.UnitBoneKeyFocusLog {
		LogInfo("bone: field focus — Pos X=%v Y=%v Z=%v, Rot X=%v Y=%v Z=%v",
			focus[0], focus[1], focus[2], focus[3], focus[4], focus[5])
		ui.UnitBoneKeyFocusLog = focus
	}
	// Tab moves the caret to the next of the six fields and Shift+Tab to the
	// previous one, wrapping round. The editor does not take Tab itself, so it
	// is read here, before the editors drain their own events.
	fields := [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd}
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
	for mode, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		for i := range eds {
			for {
				ev, ok := eds[i].Update(gtx)
				if !ok {
					break
				}
				if _, submit := ev.(widget.SubmitEvent); submit {
					LogInfo("bone: Enter in the %s group: fields=[%s]", boneGroupLog(mode), boneFieldTexts(eds))
					ApplyBoneKeyFields(ui, v, mode)
					// Enter applies, then moves the caret to the next field: the
					// six fields run Pos X..Z then Rot X..Z and wrap round, so a
					// row of values is typed with Enter alone.
					flat := (mode*3 + i + 1) % 6
					fields := [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd}
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
func ApplyBoneKeyFields(ui *uiState, v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	val, err := ParseBoneGroup(ui, mode)
	if err != nil {
		ui.UnitBoneKeyErr = err.Error()
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, mode)
	LogInfo("bone: apply %s: parsed=[%.3f %.3f %.3f] frame=%d target=%d picked(idx=%d mode=%d)",
		boneGroupLog(mode), val[0], val[1], val[2], frame, target, v.BoneKeyIndex, v.BoneKeyMode)
	if target < 0 {
		LogInfo("bone: apply %s: no target key in this clip", boneGroupLog(mode))
		ui.UnitBoneKeyErr = fmt.Sprintf("this bone has no %s key in the clip", kind)
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, mode
	ok := SetBoneKeyPosition(v, [3]float32{float32(val[0]), float32(val[1]), float32(val[2])})
	if mode == 1 {
		ok = SetBoneKeyRotation(v, quatFromEulerDeg(val[0], val[1], val[2]))
	}
	if !ok {
		LogWarn("bone: apply %s: the write was refused (bone=%d mode=%d idx=%d)", boneGroupLog(mode), v.BoneIndex, v.BoneKeyMode, v.BoneKeyIndex)
		ui.UnitBoneKeyErr = fmt.Sprintf("the %s key could not be written", kind)
		return
	}
	LogInfo("bone: apply %s: written ok, dirty=%v", boneGroupLog(mode), ui.UnitModelWhmDirty)
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	// The playhead is wound to the key that was just written so the edit is
	// on show, then the group shows what the key now holds.
	WindToPickedBoneKey(v)
	// The group is refilled from the key, so what is shown is what was
	// stored: a rotation comes back as the degrees its quaternion means.
	FillBoneKeyFields(ui, v, mode)
}

// parseBoneGroup reads one group of the typed fields as numbers, naming the
// axis that would not parse. An Enter and both buttons go through it, so a
// value is read the same way however it is applied.
func ParseBoneGroup(ui *uiState, mode int) ([3]float64, error) {
	var val [3]float64
	eds := &ui.UnitBonePosEd
	kind := "position"
	if mode == 1 {
		eds = &ui.UnitBoneRotEd
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
func ModifiedBoneAxes(ui *uiState, mode int) ([]int, bool) {
	eds := &ui.UnitBonePosEd
	if mode == 1 {
		eds = &ui.UnitBoneRotEd
	}
	var axes []int
	for i := range eds {
		if strings.TrimSpace(eds[i].Text()) != ui.UnitBoneKeyFillText[mode][i] {
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
func SetAllBoneKeyFields(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil || v.BoneIndex < 0 {
		ui.UnitBoneKeyErr = "no bone is picked"
		return
	}
	t := BoneTrack(v)
	if t == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	axes0, ch0 := ModifiedBoneAxes(ui, 0)
	axes1, ch1 := ModifiedBoneAxes(ui, 1)
	LogInfo("bone: Set all: changed pos-axes=%v rot-axes=%v", axes0, axes1)
	if !ch0 && !ch1 {
		ui.UnitBoneKeyErr = "no values were changed"
		return
	}
	var val0, val1 [3]float64
	var err error
	if ch0 {
		if val0, err = ParseBoneGroup(ui, 0); err != nil {
			ui.UnitBoneKeyErr = err.Error()
			return
		}
	}
	if ch1 {
		if val1, err = ParseBoneGroup(ui, 1); err != nil {
			ui.UnitBoneKeyErr = err.Error()
			return
		}
	}
	snap, haveSnap := SnapshotBoneTrack(v)
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
		ui.UnitBoneKeyErr = "the bone has no keys of the changed kind in this clip"
		return
	}
	// The pose cache predates the writes, so the viewport evaluates it again.
	v.PoseValid = false
	Invalidate(v)
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	name := "?"
	if v.BoneIndex < len(v.Model.Bones) {
		name = v.Model.Bones[v.BoneIndex].Name
	}
	LogInfo("bone: Set all: %d key(s) written for bone %s, dirty=%v", written, name, ui.UnitModelWhmDirty)
	ui.StatusText = fmt.Sprintf("Set changed values on %d key(s) of bone %s", written, name)
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
func SyncBoneKeyFields(ui *uiState, gtx layout.Context, v *modelViewer) {
	if v == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	sel := "no clip"
	if v.Anim != nil {
		sel = fmt.Sprintf("%s|%d|%d|%d", v.Anim.Name, v.BoneIndex, v.BoneKeyMode, v.BoneKeyIndex)
	}
	selChanged := sel != ui.UnitBoneKeyBound
	if !selChanged && frame == ui.UnitBoneKeyFrame {
		return
	}
	prevSel := ui.UnitBoneKeyBound
	ui.UnitBoneKeyBound = sel
	ui.UnitBoneKeyFrame = frame
	if selChanged {
		LogInfo("bone: selection changed %q -> %q: refilling both groups", prevSel, sel)
		ui.UnitBoneKeyErr = ""
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
		return
	}
	for mode, eds := range [2]*[3]widget.Editor{&ui.UnitBonePosEd, &ui.UnitBoneRotEd} {
		if eds[0].Focused(gtx) || eds[1].Focused(gtx) || eds[2].Focused(gtx) {
			continue
		}
		FillBoneKeyFields(ui, v, mode)
	}
}

// fillBoneKeyFields puts one group of fields on the values of the key a typed
// apply of that kind would write, or empties the group when there is no such
// key: an apply into nothing is not offered, and the fields say so by being
// blank.
func FillBoneKeyFields(ui *uiState, v *modelViewer, mode int) {
	eds := &ui.UnitBonePosEd
	if mode == 1 {
		eds = &ui.UnitBoneRotEd
	}
	var vals [3]float64
	ok := false
	if v != nil {
		frame, _ := CurrentFrame(v)
		if idx := BoneKeyTarget(v, frame, mode); idx >= 0 {
			if t := BoneTrack(v); t != nil {
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
	prev := ui.UnitBoneKeyFillText[mode]
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
		for i := range ui.UnitBoneAngleSlid {
			if ui.UnitBoneAngleSlid[i].Dragging() {
				held = true
				break
			}
		}
		if held && ok {
			vals = [3]float64{
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[0].Value),
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[1].Value),
				boneAngleSliderDeg(ui.UnitBoneAngleSlid[2].Value),
			}
		} else if !held {
			for i := range ui.UnitBoneAngleSlid {
				frac := float32(0.5)
				if ok {
					frac = boneAngleSliderFrac(vals[i])
				}
				ui.UnitBoneAngleSlid[i].Value = frac
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
		ui.UnitBoneKeyFillText[mode][i] = text
		eds[i].SetText(text)
	}
	if now := ui.UnitBoneKeyFillText[mode]; now != prev {
		frame := -1
		if v != nil {
			frame, _ = CurrentFrame(v)
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
func UnitAnimFramesRow(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	hint := "no clip on show"
	canSet := v != nil && v.Anim != nil
	if canSet {
		hint = fmt.Sprintf("%.2f s · %.1f fps", animClipSeconds(v.Anim), animFramesPerSecond(v.Anim))
	}
	// A count the field would not take says why where the length is shown: it
	// is the one place the frames row has to report in, and it is on screen in
	// every list mode.
	hintColor := cTextDim
	if ui.UnitAnimFramesErr != "" {
		hint = ui.UnitAnimFramesErr
		hintColor = rgb(0xB00020)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), "Frames")
			l.Color = cTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, "", &ui.UnitAnimFramesEd)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &ui.UnitAnimFramesSetClick, "Set", !canSet)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(8), hint)
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
func SyncAnimFramesField(ui *uiState, v *modelViewer) {
	key, text := "no clip", ""
	if v != nil && v.Anim != nil {
		key = fmt.Sprintf("%s|%d", v.Anim.Name, v.Anim.NumFrames)
		text = strconv.Itoa(v.Anim.NumFrames)
	}
	if key == ui.UnitAnimFramesSync {
		return
	}
	ui.UnitAnimFramesSync = key
	ui.UnitAnimFramesEd.SetText(text)
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
func ApplyAnimFrameCount(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil {
		ui.UnitAnimFramesErr = "no clip is on show"
		return
	}
	text := strings.TrimSpace(ui.UnitAnimFramesEd.Text())
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 {
		ui.UnitAnimFramesErr = fmt.Sprintf("%q is not a frame count — a whole number of at least 1", text)
		return
	}
	if n > maxAnimFrames {
		ui.UnitAnimFramesErr = fmt.Sprintf("%d is past the %d frames a clip is given here", n, maxAnimFrames)
		return
	}
	old := v.Anim.NumFrames
	if n == old {
		ui.UnitAnimFramesErr = ""
		ui.StatusText = fmt.Sprintf("Clip %q is already %d frames", v.Anim.Name, n)
		return
	}
	// The rate is taken before the count moves and the duration is put back
	// to match the new count: fps is frames over seconds, so holding the rate
	// means the length follows the count by the same ratio. A clip with no
	// duration of its own rates at the 20 fps fallback and is given the
	// length that goes with it.
	fps := animFramesPerSecond(v.Anim)
	v.Anim.NumFrames = n
	if fps > 0 {
		v.Anim.Duration = float32(float64(n) / fps)
	}
	v.PoseValid = false
	Invalidate(v)
	ui.UnitModelWhmDirty = true
	ui.UnitAnimFramesErr = ""
	ui.StatusText = fmt.Sprintf("Clip %q: %d frames (was %d) at %.1f fps — Save WHM writes it",
		v.Anim.Name, n, old, animFramesPerSecond(v.Anim))
	LogInfo("bone: clip %q frames %d -> %d (%.2f s at %.1f fps, dirty=%v)",
		v.Anim.Name, old, n, animClipSeconds(v.Anim), animFramesPerSecond(v.Anim), ui.UnitModelWhmDirty)
}

// saveUnitModelWhm writes the edited bone keys back into the .whm: the file
// is read from the mod (overlay first, so an earlier save is included), the
// current animation's bone keys are rebuilt into that copy — values, times
// and counts alike, since keys can be added and removed on the strip — and
// the whole file goes back through the overlay. Everything this editor does
// not read survives intact: only the animation's bone section is rewritten,
// with the chunk sizes above it moved to match.
func SaveUnitModelWhm(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil || v.AnimIndex < 0 || v.AnimIndex >= len(v.Model.Animations) {
		ui.UnitBoneKeyErr = "no animation to save"
		return
	}
	// A press can reach the handler even while the button is drawn disabled,
	// and a save of untouched keys must not be a reason to rewrite the file.
	if !ui.UnitModelWhmDirty {
		LogInfo("bone: Save skipped — no key was changed")
		ui.UnitBoneKeyErr = "nothing to save — no key was changed"
		return
	}
	if ui.ModTree == nil || ui.UnitModelPath == "" {
		ui.UnitBoneKeyErr = "no module is loaded"
		return
	}
	data, err := ui.ModTree.ReadFile(ui.UnitModelPath)
	if err != nil {
		ui.UnitBoneKeyErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchAnimClip(data, &v.Model.Model, v.AnimIndex, v.Anim.NumFrames)
	if err != nil {
		ui.UnitBoneKeyErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitModelPath, patched)
	if err != nil {
		ui.UnitBoneKeyErr = "save failed: " + err.Error()
		return
	}
	ui.UnitModelWhmDirty = false
	ui.UnitBoneKeyErr = ""
	LogInfo("bone: Save WHM ok: %s (%d bytes patched)", filepath.Base(ui.UnitModelPath), len(patched))
	ui.StatusText = fmt.Sprintf("Saved bone keys → %s (%d bytes)", disk, len(patched))
	MarkStateDirty(ui)
	log.Printf("saved whm bone keys %q -> %q (%d bytes)", ui.UnitModelPath, disk, len(patched))
}

// addBoneKeyAtPlayhead is what the "+ pos" and "+ rot" buttons do: the picked
// bone gets a key of that kind on the frame the playhead is on, holding the
// pose the viewport is showing — the same value "Set ... from pose" would
// write — and the file is marked dirty for Save WHM. A key of that kind
// already on the frame is picked instead of a second one being created, and
// the note beside Save says which of the two happened.
func AddBoneKeyAtPlayhead(ui *uiState, v *modelViewer, mode int) {
	kind := "position"
	if mode == 1 {
		kind = "rotation"
	}
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if v.BoneIndex < 0 {
		ui.UnitBoneKeyErr = "no bone is picked"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	snap, haveSnap := SnapshotBoneTrack(v)
	frame, _ := CurrentFrame(v)
	if !AddBoneKeyAtFrame(v, frame, mode) {
		// The key at the frame was picked all the same, so the fields show
		// what it holds and the note says why nothing was created.
		ui.UnitBoneKeyErr = fmt.Sprintf("frame %d already has a %s key", frame+1, kind)
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
		return
	}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	ui.StatusText = fmt.Sprintf("Added a %s key on frame %d", kind, frame+1)
	LogInfo("bone: %s key added on frame %d for bone %q (dirty=%v)",
		kind, frame+1, BoneTrack(v).Name, ui.UnitModelWhmDirty)
}

// deleteBoneKeysAction is what the "Delete key" button does: every row the
// list has picked leaves the picked bone's track — or, with nothing picked on
// the list, the single key the strip or the playhead had picked — and the file
// is marked dirty for Save WHM. The note and the status line say how many keys
// went and whose they were, so removing several is something the reader can
// count back from the log.
func DeleteBoneKeysAction(ui *uiState, v *modelViewer) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := BoneKeyActionRefs(ui, v)
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no key is picked"
		return
	}
	name := BoneTrack(v).Name
	snap, haveSnap := SnapshotBoneTrack(v)
	removed := 0
	for _, ref := range refs {
		if DeleteBoneKeyAt(v, ref.frame, ref.mode) {
			removed++
		}
	}
	if removed == 0 {
		ui.UnitBoneKeyErr = "no key could be removed"
		return
	}
	// Nothing on the list is picked any more: the keys the selection named
	// are gone, and leaving the selection behind would aim the next button at
	// frames that now hold other keys or nothing at all.
	ui.UnitBoneKeySel = nil
	ui.UnitBoneKeySelAnchor = boneKeyRef{mode: -1}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	msg := fmt.Sprintf("Removed %d key(s) of bone %s", removed, name)
	ui.StatusText = msg
	LogInfo("bone: %s (dirty=%v)", msg, ui.UnitModelWhmDirty)
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
func AddBoneKeysAround(ui *uiState, v *modelViewer, dir int) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := BoneKeyActionRefs(ui, v)
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no key is picked"
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
		f := FreeBoneKeyFrame(v, ref.frame, ref.mode, dir)
		if f < 0 || !InsertBoneKeyCopy(v, f, ref.mode, ref.frame) {
			blocked++
			continue
		}
		added = append(added, boneKeyRef{mode: ref.mode, frame: f})
	}
	if len(added) == 0 {
		ui.UnitBoneKeyErr = fmt.Sprintf("no free frame %s those keys", side)
		LogInfo("bone: + %s: nothing was added, %d row(s) had no free frame", side, blocked)
		return
	}
	ui.UnitBoneKeySel = make(map[boneKeyRef]bool, len(added))
	for _, ref := range added {
		ui.UnitBoneKeySel[ref] = true
	}
	ui.UnitBoneKeySelAnchor = added[0]
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	if blocked > 0 {
		ui.UnitBoneKeyErr = fmt.Sprintf("%d row(s) had no free frame", blocked)
	}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	msg := fmt.Sprintf("Added %d key(s) %s the picked rows", len(added), side)
	ui.StatusText = msg
	LogInfo("bone: %s (dirty=%v)", msg, ui.UnitModelWhmDirty)
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
func ApplyBoneAngleSliders(ui *uiState, v *modelViewer) {
	dragging := false
	for i := range ui.UnitBoneAngleSlid {
		if ui.UnitBoneAngleSlid[i].Dragging() {
			dragging = true
			break
		}
	}
	if !dragging {
		ui.UnitBoneSliderUndoArmed = false
		return
	}
	if v == nil || v.Anim == nil {
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, 1)
	if target < 0 {
		ui.UnitBoneKeyErr = "this bone has no rotation key in the clip"
		return
	}
	t := BoneTrack(v)
	if t == nil || target >= len(t.RotKey) {
		ui.UnitBoneKeyErr = "the rotation key could not be written"
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, 1
	ang := [3]float64{
		boneAngleSliderDeg(ui.UnitBoneAngleSlid[0].Value),
		boneAngleSliderDeg(ui.UnitBoneAngleSlid[1].Value),
		boneAngleSliderDeg(ui.UnitBoneAngleSlid[2].Value),
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
	if !ui.UnitBoneSliderUndoArmed {
		if snap, ok := SnapshotBoneTrack(v); ok {
			PushBoneUndo(ui, snap, true)
		}
		ui.UnitBoneSliderUndoArmed = true
	}
	if !SetBoneKeyRotation(v, quatFromEulerDeg(ang[0], ang[1], ang[2])) {
		ui.UnitBoneKeyErr = "the rotation key could not be written"
		return
	}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	// The typed Rot fields follow the key; the sliders are left alone
	// (fillBoneKeyFields skips the one being dragged).
	FillBoneKeyFields(ui, v, 1)
	LogInfo("bone: angle sliders wrote key %d: rot %.3f %.3f %.3f (dirty=%v)",
		target, ang[0], ang[1], ang[2], ui.UnitModelWhmDirty)
}

// mirrorBoneRotationAction is what a "mirror X/Y/Z" button does: the rotation
// key the Rot fields show comes back with its angle about the chosen axis
// negated and the two other axes left as they were — the angle about that
// axis mirrored, which is then what the fields show.
func MirrorBoneRotationAction(ui *uiState, v *modelViewer, axis int) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	frame, _ := CurrentFrame(v)
	target := BoneKeyTarget(v, frame, 1)
	if target < 0 {
		ui.UnitBoneKeyErr = "this bone has no rotation key in the clip"
		return
	}
	v.BoneKeyIndex, v.BoneKeyMode = target, 1
	t := BoneTrack(v)
	before := eulerDegFromQuat(t.RotKey[target].V)
	after := before
	after[axis] = -after[axis]
	snap, haveSnap := SnapshotBoneTrack(v)
	if !SetBoneKeyRotation(v, quatFromEulerDeg(after[0], after[1], after[2])) {
		ui.UnitBoneKeyErr = "the rotation key could not be written"
		return
	}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	WindToPickedBoneKey(v)
	FillBoneKeyFields(ui, v, 1)
	PushBoneUndo(ui, snap, haveSnap)
	msg := fmt.Sprintf("Mirrored %s about %s", t.Name, boneFieldAxes[axis])
	ui.StatusText = msg
	LogInfo("bone: %s (key=%d, rot %.3f %.3f %.3f -> %.3f %.3f %.3f, dirty=%v)",
		msg, target, before[0], before[1], before[2], after[0], after[1], after[2], ui.UnitModelWhmDirty)
}

// shiftBoneKeysAction is what the back/forward buttons beside the frames field
// do: every picked row of the keyframe list — or, with none picked, the single
// key the strip or the playhead had picked — moves that many frames along the
// clip. A key whose new frame is already held by a key that is not shifting
// stays where it is rather than landing on top of it, and the selection
// follows the keys to the frames they ended on, so a run of presses carries a
// block along the strip without reselecting.
func ShiftBoneKeysAction(ui *uiState, v *modelViewer, dir int) {
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return
	}
	if BoneTrack(v) == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return
	}
	refs := BoneKeyActionRefs(ui, v)
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no key is picked"
		return
	}
	n := BoneKeyShiftFrames(ui)
	pickedOld, hasPicked := PickedBoneKeyRef(v)
	snap, haveSnap := SnapshotBoneTrack(v)
	newRefs, moved, blocked := ShiftBoneKeys(v, refs, dir*n)
	if moved == 0 && blocked == 0 {
		ui.UnitBoneKeyErr = "no key could be shifted"
		LogInfo("bone: shift %+d of %d key(s): nothing moved", dir*n, len(refs))
		return
	}
	// The selection follows the keys: it now names the frames they landed
	// on, so pressing again shifts them further without reselecting.
	ui.UnitBoneKeySel = make(map[boneKeyRef]bool, len(newRefs))
	for _, ref := range newRefs {
		ui.UnitBoneKeySel[ref] = true
	}
	ui.UnitBoneKeySelAnchor = boneKeyRef{mode: -1}
	if len(newRefs) > 0 {
		ui.UnitBoneKeySelAnchor = newRefs[0]
	}
	// The picked key follows its row to the frame it landed on, so the
	// strip marks and the typed fields point at it again after the move.
	for i, r := range refs {
		if hasPicked && r == pickedOld && i < len(newRefs) {
			if at := BoneKeyIndexAtFrame(v, newRefs[i].frame, newRefs[i].mode); at >= 0 {
				v.BoneKeyIndex, v.BoneKeyMode = at, newRefs[i].mode
			}
			break
		}
	}
	if moved > 0 {
		ui.UnitModelWhmDirty = true
		PushBoneUndo(ui, snap, haveSnap)
	}
	ui.UnitBoneKeyErr = ""
	if blocked > 0 {
		ui.UnitBoneKeyErr = fmt.Sprintf("%d key(s) stayed put (no room)", blocked)
	}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	side := "forward"
	if dir < 0 {
		side = "back"
	}
	msg := fmt.Sprintf("Shifted %d key(s) %d frame(s) %s", moved, n, side)
	ui.StatusText = msg
	LogInfo("bone: %s (%d row(s) shifted, %d held back, dirty=%v)", msg, len(newRefs), blocked, ui.UnitModelWhmDirty)
}

// boneKeyShiftFrames is the shift the field holds, in frames: a whole number,
// with 1 standing in for an empty or unreadable field so the buttons still
// move something.
func BoneKeyShiftFrames(ui *uiState) int {
	s := strings.TrimSpace(ui.UnitBoneKeyShiftEd.Text())
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
func PushBoneUndo(ui *uiState, s boneTrackSnapshot, ok bool) {
	if !ok || (len(s.posKey) == 0 && len(s.rotKey) == 0 && s.anim == nil) {
		return
	}
	ui.UnitBoneUndo = append(ui.UnitBoneUndo, s)
	if len(ui.UnitBoneUndo) > boneUndoLimit {
		ui.UnitBoneUndo = ui.UnitBoneUndo[len(ui.UnitBoneUndo)-boneUndoLimit:]
	}
}

// undoBoneKeyEdit rolls the last key edit back: the most recent snapshot is
// restored into the clip and bone it was taken from, the selection is dropped
// (the frames it named may not exist in the restored track) and the typed
// fields are refilled from the restored keys. It reports whether anything was
// undone, which is what the note beside Save says.
func UndoBoneKeyEdit(ui *uiState, v *modelViewer) bool {
	if len(ui.UnitBoneUndo) == 0 {
		ui.UnitBoneKeyErr = "nothing to undo"
		return false
	}
	s := ui.UnitBoneUndo[len(ui.UnitBoneUndo)-1]
	ui.UnitBoneUndo = ui.UnitBoneUndo[:len(ui.UnitBoneUndo)-1]
	if v == nil || !RestoreBoneTrack(v, s) {
		ui.UnitBoneKeyErr = "could not restore the previous keys"
		return false
	}
	// The restored track may not carry the keys the selection and picked key
	// pointed at, so both are cleared and the fields refill from what is
	// actually there now.
	ui.UnitBoneKeySel = nil
	ui.UnitBoneKeySelAnchor = boneKeyRef{mode: -1}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	ui.StatusText = "Restored the keys before the last edit"
	LogInfo("bone: undo restored track of bone %d: %d pos, %d rot keys (dirty=%v)",
		s.boneIndex, len(s.posKey), len(s.rotKey), ui.UnitModelWhmDirty)
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
func UnitBoneKeyRow(ui *uiState, gtx layout.Context, c *widget.Clickable, selected bool, text string) layout.Dimensions {
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
					Rrect(ui, gtx, bg, gtx.Constraints.Max, unit.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(10), text)
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
func UnitViewportRow(ui *uiState, gtx layout.Context, c *widget.Clickable, selected bool, name, note string) layout.Dimensions {
	return UnitViewportRowIcon(ui, gtx, c, selected, name, note, nil, false)
}

// unitViewportRowIcon is unitViewportRow with an optional icon column before
// the name. A list that shows an icon for every row — the Classic OE's
// Actions folder — reserves the column whether or not a row's icon was found
// (iconCol), so its names stay in one line; a list whose icons come and go
// passes iconCol false and shows them as they come.
func UnitViewportRowIcon(ui *uiState, gtx layout.Context, c *widget.Clickable, selected bool, name, note string, icon image.Image, iconCol bool) layout.Dimensions {
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
					Rrect(ui, gtx, bg, gtx.Constraints.Max, unit.Dp(2))
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
							l := material.Label(ui.Th, unit.Sp(10), unitViewportRowText(name))
							l.Color = cText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if note == "" {
								return layout.Dimensions{}
							}
							l := material.Label(ui.Th, unit.Sp(9), note)
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
func EnsureUnitAnimClicks(ui *uiState, n int) {
	if len(ui.UnitAnimClicks) != n {
		ui.UnitAnimClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitMarkerClicks sizes the markers list's click targets, the same way
// as the clip list's: rebuilt per model rather than grown into.
func EnsureUnitMarkerClicks(ui *uiState, n int) {
	if len(ui.UnitMarkerClicks) != n {
		ui.UnitMarkerClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitBoneClicks sizes the bones list's click targets, the same way as
// the other two lists': rebuilt per model rather than grown into.
func EnsureUnitBoneClicks(ui *uiState, n int) {
	if len(ui.UnitBoneClicks) != n {
		ui.UnitBoneClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitBoneKeyClicks sizes the bone keyframe row click targets for the
// currently picked bone, rebuilt whenever the bone selection changes.
func EnsureUnitBoneKeyClicks(ui *uiState, n int) {
	if len(ui.UnitBoneKeyClicks) != n {
		ui.UnitBoneKeyClicks = make([]widget.Clickable, n)
	}
}

// unitModelLoaded reports whether the viewport has a model on screen, which is
// what a test checks rather than what it can see.
func UnitModelLoaded(ui *uiState) bool {
	return UnitModelViewer(ui) != nil
}

// unitModelAnimNames are the clips on show, which the tests compare against.
func UnitModelAnimNames(ui *uiState) []string {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return nil
	}
	out := make([]string, len(v.Model.Animations))
	for i := range v.Model.Animations {
		out[i] = v.Model.Animations[i].Name
	}
	return out
}

// unitModelClipDuration is how long one pass of the clip on show takes, in
// seconds. A clip with no frame count is reported as zero rather than as the
// fallback rate, since a zero here means "nothing to wait for".
func unitModelClipDuration(v *modelViewer) float64 {
	if v == nil || v.Anim == nil || v.Anim.NumFrames <= 0 {
		return 0
	}
	return float64(v.Anim.NumFrames) / animFramesPerSecond(v.Anim)
}

func Rrect(ui *uiState, gtx layout.Context, bg color.NRGBA, sz image.Point, r unit.Dp) {
	paint.Fill(gtx.Ops, bg)
}

// ─── merged from unit_viewport.go ───────────────────────────────────────────────

// The object window's Viewport section: the selected unit's own 3D model, with
// its animations.
//
// Dawn of War keeps a unit's geometry out of the .rgd. The .rgd names it in
// entity_blueprint_ext.animator, a path relative to Data\art\ebps that ends in
// the model's own name, e.g. "Races/Necrons/Troops/Necron_Basic_Warrior", and
// the model is the .whm of that name in that folder. Measured over the 582
// unit .rgds of the DXP3 tree that resolves for 581 of them, so it is the
// lookup; the rest fall back to a search of the art tree for a .whm of the
// unit's own name, which is how a mod that renames the model still gets one.
//
// The viewer itself is not new. It is the same one a .whm tab uses, driven
// from a fileTab held here rather than in openTabs, so a unit's model can be
// orbited, played and inspected without opening it first.

// unitSectionViewport names the section, so the layout does not repeat the
// string of the menu entry it is keyed on.
const unitSectionViewport = "Viewport"

// unitModelArtRoot is the folder animator paths are relative to.
const unitModelArtRoot = "art/ebps"

// unitGroundTexture is the default terrain texture shown beneath unit models.
const unitGroundTexture = "Data/art/scenarios/textures/detail/jungle_ground_02.tga"

// errNoUnitModel is what a unit whose model cannot be found anywhere reports,
// as distinct from a model that was found and would not decode.
var errNoUnitModel = errors.New("no model file for this unit")

// unitModelResult carries a decoded model back to the UI goroutine. The unit it
// was read for travels with it, since a load started for one unit can finish
// after the reader has moved on to another.
type unitModelResult struct {
	unit        string
	path        string
	model       *WhmModel
	groundImage image.Image
	work        time.Duration // reading, parsing and the ground image
	err         error
}

// unitModelSource is everything about a unit that the loader needs, copied off
// the UI goroutine before the load starts so the worker touches no UI state.
type unitModelSource struct {
	unit     string // the unit .rgd's tree path
	animator string // entity_blueprint_ext.animator, may be empty
	name     string // the unit's file name without extension
	race     string // the folder name under races
	kind     string // "troops" or "structures"
}

// newUnitModelSource reads what a unit says about its model.
func newUnitModelSource(tree *modtree.ModTree, d *unitDetails) (unitModelSource, bool) {
	if d == nil {
		return unitModelSource{}, false
	}
	src := unitModelSource{
		unit: d.Entry.Path,
		name: d.Entry.Name,
		race: d.Entry.Race,
		kind: d.Entry.Kind.Folder,
	}
	if src.name == "" {
		if base := d.Entry.Path; base != "" {
			src.name = strings.TrimSuffix(filepath.Base(base), filepath.Ext(base))
		}
	}
	if d.File != nil {
		src.animator = unitAnimatorName(d.File)
	}
	if src.name == "" && src.animator == "" {
		return unitModelSource{}, false
	}
	return src, true
}

// unitAnimatorName reads entity_blueprint_ext.animator out of a unit .rgd, or
// "" when the file does not carry one. The field is a plain string, not a
// reference: it names a model folder path rather than an asset the .rgd
// resolves, which is why it is read here rather than through resolveRgdRef.
func unitAnimatorName(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	ext := rgdFindChild(rf.Root, "entity_blueprint_ext")
	if ext == nil {
		return ""
	}
	e := rgdFindChild(ext, "animator")
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.DisplayValue())
}

// unitModelPath finds the .whm of a unit, or "" when there is none. The order
// is the one that works on the shipped data first and the one that rescues a
// mod's own naming last.
func unitModelPath(tree *modtree.ModTree, src unitModelSource) string {
	if tree == nil {
		return ""
	}
	rel := strings.ReplaceAll(strings.TrimSpace(src.animator), "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	if rel != "" {
		if !strings.Contains(rel, ".") {
			rel += ".whm"
		}
		p := filepath.Join("Data", filepath.FromSlash(unitModelArtRoot), filepath.FromSlash(rel))
		if tree.FindFile(p) != nil {
			return p
		}
	}
	// The folder the unit's .rgd itself lives in is where the game keeps the
	// model of a unit it renamed, so the sibling name is worth a try before
	// searching the whole tree.
	if src.race != "" && src.name != "" {
		dirs := []string{}
		if src.kind != "" {
			dirs = append(dirs, src.kind)
		}
		dirs = append(dirs, "", "troops", "structures")
		for _, d := range dirs {
			parts := []string{"Data", unitModelArtRoot, "races", src.race}
			if d != "" {
				parts = append(parts, d)
			}
			parts = append(parts, src.name+".whm")
			p := filepath.Join(parts...)
			if tree.FindFile(p) != nil {
				return p
			}
		}
	}
	if src.name != "" {
		if f := tree.FindByName(src.name + ".whm"); f != nil {
			return f.FullPath
		}
	}
	return ""
}

// ensureUnitModel starts loading the current unit's model if it is not already
// loaded or on its way. It is called from the Viewport section's layout, so
// browsing units never pays for a model that is not being looked at.
//
// While the read is on a worker the section has no model to animate, and the
// canvas below has nothing to ask a redraw for — the window repaints on input
// and on the invalidates a drawn model schedules, neither of which exist here.
// The poll that would pick the result up runs at the top of the frame loop, so
// without an invalidate of its own the finished answer waits in its channel
// until the mouse moves. That is what made a switch on this section look many
// seconds slower than the same switch done from another one and read afterwards:
// the read cost the same, but one of them was waiting for input to show it.
func EnsureUnitModel(ui *uiState, gtx layout.Context) {
	if ui.ModTree == nil {
		return
	}
	src, ok := newUnitModelSource(ui.ModTree, CurrentUnitDetails(ui))
	if !ok {
		ResetUnitModel(ui)
		return
	}
	if src.unit != ui.UnitModelFor || (ui.UnitModel == nil && ui.UnitModelCh == nil && ui.UnitModelErr == "") {
		StartUnitModel(ui, src)
	}
	if ui.UnitModelCh != nil {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(30 * time.Millisecond)})
	}
}

// startUnitModel kicks the read and the parse off on a worker. Both are worth
// moving off the UI goroutine: a unit model is megabytes, and the necron warrior's
// alone is five of them.
func StartUnitModel(ui *uiState, src unitModelSource) {
	ResetUnitModel(ui)
	ui.UnitModelFor = src.unit
	tree := ui.ModTree
	ch := make(chan unitModelResult, 1)
	ui.UnitModelCh = ch
	ui.UnitModelStart = time.Now()
	go func() {
		res := unitModelResult{unit: src.unit}
		workStart := time.Now()
		res.path = unitModelPath(tree, src)
		if res.path == "" {
			res.err = errNoUnitModel
			ch <- res
			return
		}
		data, err := tree.ReadFile(res.path)
		if err != nil {
			res.err = err
			ch <- res
			return
		}
		model, err := ParseWhmModel(data)
		if err != nil {
			res.err = err
			ch <- res
			return
		}
		res.model = model
		res.groundImage = loadWhmGroundImage(tree)
		res.work = time.Since(workStart)
		ch <- res
	}()
}

// resetUnitModel drops the viewer and forgets which unit it was for, so the
// next request loads again. The worker is not stopped: it writes into a
// buffered channel nobody reads, which costs one model parse and no more.
func ResetUnitModel(ui *uiState) {
	if ui.UnitModel != nil && ui.UnitModel.Model != nil {
		// The hardware renderer caches the model's buffers, and the goroutine
		// that may be mid render holds the same pointer, so both go back.
		if ui.ModelGpu != nil && ui.UnitModel.Model.Model != nil {
			ui.ModelGpu.Forget(&ui.UnitModel.Model.Model.Model)
		}
		Close(ui.UnitModel.Model)
	}
	ui.UnitModel = nil
	ui.UnitModelCh = nil
	ui.UnitModelFor = ""
	ui.UnitModelPath = ""
	ui.UnitModelErr = ""
	ui.UnitAnimClicks = nil

	ui.UnitMarkerClicks = nil
	ui.UnitBoneClicks = nil
	ui.UnitFXListMode = false
	ui.UnitMeshListMode = false
	ui.UnitViewportScroll = panelScrollState{}
	// The typed bone key fields belong to the model that has gone with it:
	// their bound key and any pending note are dropped so a later model
	// cannot be shown with, or written from, this one's values.
	ui.UnitModelWhmDirty = false
	ui.UnitBoneKeyBound = ""
	ui.UnitBoneKeyErr = ""
	ui.UnitAnimFramesSync = ""
	ui.UnitAnimFramesErr = ""
}

// pollUnitModel takes a finished decode and builds the viewer from it. The
// frames and the textures it goes on to produce are polled by pollModelJobs,
// like any other viewer's.
// selectObjectAction reads the model's WHE graph and plays all clips
// selected by the named action together. Weighted motion links choose one
// variant independently for each motion; absent or unresolved WHE data leaves
// the viewport's ordinary single-clip default in place. The idle action also
// falls back to the pristine state's idle/stand clips when the graph has no
// idle action of its own.
func selectObjectAction(v *modelViewer, tree *modtree.ModTree, modelPath, actionName string) bool {
	if v == nil || v.Model == nil || tree == nil || modelPath == "" || strings.TrimSpace(actionName) == "" {
		return false
	}
	base := strings.TrimSuffix(modelPath, filepath.Ext(modelPath))
	data, err := tree.ReadFile(base + ".whe")
	if err != nil {
		if f := tree.FindByName(strings.ToLower(filepath.Base(base) + ".whe")); f != nil {
			data, err = tree.ReadTreeFile(f)
		}
	}
	if err != nil || len(data) == 0 {
		return false
	}
	doc, err := whe.ParseDocument(data)
	if err != nil {
		return false
	}
	seed := rand.Int63()
	clipNames := doc.ActionAnimationNamesWithSeed(actionName, seed)
	if len(clipNames) == 0 && strings.EqualFold(strings.TrimSpace(actionName), "idle") {
		for _, name := range doc.RestClipNamesWithSeed(seed) {
			lower := strings.ToLower(strings.TrimSpace(name))
			if strings.HasPrefix(lower, "idle") || strings.HasPrefix(lower, "stand") {
				clipNames = append(clipNames, name)
			}
		}
	}
	var clips []*WhmAnimation
	seen := make(map[string]bool)
	for _, name := range clipNames {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		animation, source := sgb.ResolveLinkedAnimation(tree, modelPath, &v.Model.Model, name)
		if animation == nil || source == nil {
			continue
		}
		if source != &v.Model.Model {
			animation = sgb.BindAnimationToModel(&v.Model.Model, animation)
		}
		if animation != nil {
			clips = append(clips, animation)
		}
	}
	if len(clips) == 0 {
		return false
	}
	SelectAnimations(v, clips, true)
	v.Playing = true
	return true
}

// activateUnitAction plays every clip the named action of the loaded unit
// model's .whe resolves to, combined in the viewport, and says so in the
// status bar. It answers false when no model is on screen or the action
// resolves to nothing, leaving the caller with its own fallback.
func ActivateUnitAction(ui *uiState, actionName string) bool {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return false
	}
	if !selectObjectAction(v, ui.ModTree, ui.UnitModelPath, actionName) {
		return false
	}
	ui.StatusText = fmt.Sprintf("Action %q — playing %d clip(s) together",
		strings.TrimSpace(actionName), len(v.Anims))
	return true
}

func PollUnitModel(ui *uiState) {
	if ui.UnitModelCh == nil {
		return
	}
	select {
	case res := <-ui.UnitModelCh:
		ui.UnitModelCh = nil
		if res.unit != ui.UnitModelFor {
			// The reader has moved on to another unit while this one loaded.
			return
		}
		if res.err != nil {
			ui.UnitModelErr = res.err.Error()
			return
		}
		// The gap between what the worker spent and what the reader waited is
		// the answer arriving without a frame to be picked up in, which is the
		// whole of the difference between a switch that looked slow and one
		// that did not.
		total := time.Since(ui.UnitModelStart)
		LogInfo("3D preview: model of %q on screen after %v (worker %v, waiting for a frame %v)",
			res.path, total, res.work, total-res.work)
		tab := &fileTab{Path: res.path, Kind: "whm"}
		tab.Model = newObjectModelViewer(res.model, res.path, res.groundImage)
		selectObjectAction(tab.Model, ui.ModTree, res.path, "idle")
		// The canvas is a third of the window wide here, so the playback strip
		// is drawn in the panel beside it rather than under it.
		tab.Model.SideControls = true
		ui.UnitModel = tab
		ui.UnitModelPath = res.path
		// A freshly read model starts clean, and its keys rebind the typed
		// fields: the previous model's edits are not its edits.
		ui.UnitModelWhmDirty = false
		ui.UnitBoneKeyBound = ""
		ui.UnitBoneKeyErr = ""
	default:
	}
}

// unitDefaultAnim picks the clip the viewport starts on, or -1 when the model
// has none. The names are the game's own: a unit moves with "move" and "run",
// stands with "idle_1", and everything else in the list is a one off.
func unitDefaultAnim(model *WhmModel) int {
	if model == nil || len(model.Animations) == 0 {
		return -1
	}
	want := []string{"move", "run", "walk", "idle_1", "idle_2", "idle", "idle_firing"}
	names := make([]string, len(model.Animations))
	for i := range model.Animations {
		names[i] = strings.ToLower(model.Animations[i].Name)
	}
	for _, w := range want {
		for i, n := range names {
			if n == w {
				return i
			}
		}
	}
	// A clip whose name starts "vis_" only fades a weapon or a banner in and
	// out; it moves nothing, so it is a poor thing to open on.
	for i, n := range names {
		if !strings.HasPrefix(n, "vis_") {
			return i
		}
	}
	return 0
}

// unitModelViewer is the viewport's viewer, or nil when no model is up.
func UnitModelViewer(ui *uiState) *modelViewer {
	if ui.UnitModel == nil {
		return nil
	}
	return ui.UnitModel.Model
}

// lookThroughCamera moves the orbit camera to where one of the model's own
// cameras is, which is what makes the list of them worth having. A .whm camera
// is a local transform in the exporter's frame, so it is put into model space
// by the same rotation the skeleton's root bones get, and it looks down its own
// -Z at its focus point. The direction is turned into the orbit camera's pair
// the way render.BuildViewMatrix takes it apart: the yaw around the up axis, which is
// Z, and the height off the horizon.
func LookThroughCamera(v *modelViewer, cam WhmCamera) {
	if v == nil || v.Model == nil {
		return
	}
	eye := boneRootFrame.MulPosition(render.Vec3From(cam.Pos))
	focus := boneRootFrame.MulPosition(render.Vec3From(cam.FocusPoint))
	v.CameraState = viewer.LookThroughCamera(v.CameraState, eye, focus, cam.Fov, cam.Name)
	Invalidate(v)
}

// resetCamera puts the orbit camera back to the framing newModelViewer chose,
// and forgets any camera of the model's that was being looked through.
func ResetCamera(v *modelViewer) {
	if v == nil {
		return
	}
	camera := viewer.ResetCamera(v.CameraState)
	v.CameraState = camera
	Invalidate(v)
}

// panCamera shifts the orbit target in the camera's screen plane. Pixel motion
// is scaled by the world-space width visible at the camera distance, so the
// drag has consistent sensitivity across zoom levels and viewport sizes.
func PanCamera(v *modelViewer, dx, dy float64, height int) {
	if v == nil || height <= 0 || v.Distance <= 0 {
		return
	}
	forward := fauxgl.Vector{
		X: -math.Cos(v.Yaw) * math.Cos(v.Pitch),
		Y: -math.Sin(v.Yaw) * math.Cos(v.Pitch),
		Z: -math.Sin(v.Pitch),
	}
	worldUp := fauxgl.Vector{Z: 1}
	right := forward.Cross(worldUp)
	if length := right.Length(); length > 1e-9 {
		right = right.MulScalar(1 / length)
	} else {
		right = fauxgl.Vector{X: 1}
	}
	up := right.Cross(forward)
	if length := up.Length(); length > 1e-9 {
		up = up.MulScalar(1 / length)
	}
	worldPerPixel := 2 * v.Distance * math.Tan(v.FOV*math.Pi/360) / float64(height)
	v.Center = v.Center.Sub(right.MulScalar(dx * worldPerPixel)).Add(up.MulScalar(dy * worldPerPixel))
	v.ActiveCamera = ""
	Invalidate(v)
}

// ─── merged from unit_mesh_material_ui.go ───────────────────────────────────────────────

// The material editor a double press on a mesh row raises: every reference the
// named mesh carries, in an editable field, with the file's own value kept
// beside it. Preview applies the fields to the model on show — the map
// appears behind the dim, which is the fast way to try a corrected path —
// and Save WHM writes them into the file through the .whm material patcher.
//
// The references are collected in the order the mesh list holds them, which is
// the order the file's FOLDMSLC material list holds them, so field i and the
// i-th material the patcher rewrites are the same one.

// meshMaterialRefs returns the named mesh's material references in order. A
// mesh with no materials under that name yields nil, which is what stops the
// dialog opening on a mesh there is nothing to edit.
func meshMaterialRefs(m *WhmModel, name string) []string {
	if m == nil {
		return nil
	}
	var out []string
	for _, mesh := range m.Meshes {
		if strings.EqualFold(mesh.Name, name) {
			out = append(out, mesh.Material)
		}
	}
	return out
}

// applyMeshMaterialRefs writes refs back onto the named mesh's materials, in
// the order meshMaterialRefs collected them. An empty entry leaves that
// material as it was, and the shared-shader path is cleared alongside so the
// renderer keys the texture off the reference it was just given — the shape a
// model whose material label carries the path already has, and the field the
// game reads.
func applyMeshMaterialRefs(m *WhmModel, name string, refs []string) {
	if m == nil {
		return
	}
	i := 0
	for j := range m.Meshes {
		if !strings.EqualFold(m.Meshes[j].Name, name) {
			continue
		}
		if i < len(refs) && refs[i] != "" {
			m.Meshes[j].Material = refs[i]
			m.Meshes[j].MaterialPath = ""
		}
		i++
	}
}

// equalRefs reports whether two reference lists hold the same strings, so a
// Cancel that would change nothing skips the texture reload it would cost.
func equalRefs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unitMeshMatFieldText reads the editors as they stand, trimmed, in material
// order. An empty field means "leave this one as it is", the same the patcher
// takes it to mean.
func UnitMeshMatFieldText(ui *uiState) []string {
	out := make([]string, len(ui.UnitMeshMatEditors))
	for i := range ui.UnitMeshMatEditors {
		out[i] = strings.TrimSpace(ui.UnitMeshMatEditors[i].Text())
	}
	return out
}

// reloadUnitModelMaterials restarts the texture lookup for the unit's viewer
// from the material references it now carries — the same step a fresh load
// takes, so a changed reference is resolved and drawn again.
func ReloadUnitModelMaterials(ui *uiState, v *modelViewer) {
	t := ui.UnitModel
	if t == nil || v == nil || v.Model == nil || ui.ModTree == nil {
		return
	}
	t.ModelMaterialCh = make(chan modelMaterialResult, 1)
	LoadModelMaterials(ui, v, t.ModelMaterialCh)
}

// openUnitMeshMaterialDialog fills one field per material of the named mesh
// with the reference it carries and puts the dialog up. A mesh with no
// materials leaves it down: there is nothing to edit and nothing to save.
func OpenUnitMeshMaterialDialog(ui *uiState, name string) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return
	}
	refs := meshMaterialRefs(v.Model, name)
	if len(refs) == 0 {
		return
	}
	ui.UnitMeshMatOpen = true
	ui.UnitMeshMatName = name
	ui.UnitMeshMatOrig = refs
	ui.UnitMeshMatErr = ""
	ui.UnitMeshMatScroll = panelScrollState{}
	ui.UnitMeshMatEditors = make([]widget.Editor, len(refs))
	for i, ref := range refs {
		ed := widget.Editor{SingleLine: true}
		ed.SetText(ref)
		ui.UnitMeshMatEditors[i] = ed
	}
}

// closeUnitMeshMaterialDialog puts the dialog down. With revert it also puts
// the model back on the references the file holds: a preview may have moved it,
// and leaving without saving must not keep the texture of an edit that was
// never written. A close after a save reverts nothing, because the file says
// what the model now says.
func CloseUnitMeshMaterialDialog(ui *uiState, revert bool) {
	if revert {
		v := UnitModelViewer(ui)
		if v != nil && v.Model != nil && ui.UnitMeshMatName != "" {
			cur := meshMaterialRefs(v.Model, ui.UnitMeshMatName)
			if !equalRefs(cur, ui.UnitMeshMatOrig) {
				applyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, ui.UnitMeshMatOrig)
				ReloadUnitModelMaterials(ui, v)
				Invalidate(v)
			}
		}
	}
	ui.UnitMeshMatOpen = false
	ui.UnitMeshMatName = ""
	ui.UnitMeshMatOrig = nil
	ui.UnitMeshMatEditors = nil
	ui.UnitMeshMatErr = ""
	ui.UnitMeshMatScroll = panelScrollState{}
}

// previewUnitMeshMaterialDialog applies the fields to the model on show and
// re-resolves its textures, so the map the path names appears behind the dim
// without touching the file.
func PreviewUnitMeshMaterialDialog(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		ui.UnitMeshMatErr = "no model is loaded"
		return
	}
	applyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, UnitMeshMatFieldText(ui))
	ReloadUnitModelMaterials(ui, v)
	Invalidate(v)
	ui.UnitMeshMatErr = ""
}

// saveUnitMeshMaterialDialog writes the fields into the .whm: the references
// that differ are spliced in, the whole file goes to the overlay, and the
// model on show is set to the same so the view agrees with what was written.
// The dialog stays up — a modder correcting a path usually tries another —
// and Cancel after a save reverts to nothing, because the file now says this.
func SaveUnitMeshMaterialDialog(ui *uiState) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		ui.UnitMeshMatErr = "no model is loaded"
		return
	}
	if ui.ModTree == nil || ui.UnitModelPath == "" {
		ui.UnitMeshMatErr = "no module is loaded"
		return
	}
	refs := UnitMeshMatFieldText(ui)
	data, err := ui.ModTree.ReadFile(ui.UnitModelPath)
	if err != nil {
		ui.UnitMeshMatErr = "read failed: " + err.Error()
		return
	}
	patched, err := whm.PatchMeshMaterialPaths(data, ui.UnitMeshMatName, refs)
	if err != nil {
		ui.UnitMeshMatErr = "save failed: " + err.Error()
		return
	}
	disk, err := ui.ModTree.WriteOverlayFile(ui.UnitModelPath, patched)
	if err != nil {
		ui.UnitMeshMatErr = "save failed: " + err.Error()
		return
	}
	applyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, refs)
	ReloadUnitModelMaterials(ui, v)
	Invalidate(v)
	ui.UnitMeshMatErr = ""
	// The file holds these now: Cancel after this point reverts to them.
	ui.UnitMeshMatOrig = refs
	for i := range ui.UnitMeshMatEditors {
		if i < len(refs) {
			ui.UnitMeshMatEditors[i].SetText(refs[i])
		}
	}
	ui.StatusText = fmt.Sprintf("Saved materials → %s", disk)
	LogInfo("mesh material: Save WHM ok: %s (mesh=%q, %d path(s))",
		disk, ui.UnitMeshMatName, len(refs))
	MarkStateDirty(ui)
}

// layoutUnitMeshMatDialog is the dialog itself: a dimmed card over the unit
// body with the mesh's references in editable fields and Preview / Save / Cancel
// beneath. The buttons and the dim were laid out last frame, so a press on any
// of them is read here, ahead of redrawing what they decided — the order the
// Classic OE event dialog takes.
//
// The card is sized to its content rather than to the window, and it takes
// every press on its own area, so clicking the card's whitespace neither
// leaves a dialog that fills the screen nor closes it under the user's hand.
func LayoutUnitMeshMatDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	cancel := ui.UnitMeshMatCancel.Clicked(gtx)
	dim := ui.UnitMeshMatDim.Clicked(gtx)
	if cancel || dim {
		CloseUnitMeshMaterialDialog(ui, true)
	}
	if ui.UnitMeshMatPreview.Clicked(gtx) {
		PreviewUnitMeshMaterialDialog(ui)
	}
	if ui.UnitMeshMatSave.Clicked(gtx) {
		SaveUnitMeshMaterialDialog(ui)
	}
	if !ui.UnitMeshMatOpen {
		return layout.Dimensions{}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return ui.UnitMeshMatDim.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				sz := gtx.Constraints.Max
				st := clip.Rect{Max: sz}.Push(gtx.Ops)
				paint.ColorOp{Color: rgba(0, 0, 0, 70)}.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				st.Pop()
				return layout.Dimensions{Size: sz}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				inset := layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}
				cardW := minInt(gtx.Dp(unit.Dp(720)), gtx.Constraints.Max.X-gtx.Dp(unit.Dp(30)))
				contentW := cardW - gtx.Dp(inset.Left+inset.Right)
				// The card is as tall as what it holds, not as tall as the
				// window. One material was drawing a 540dp card that was nine
				// tenths empty; only when the fields run past the room the
				// window has does the card fill it, and then they scroll.
				cardH := clampInt(
					UnitMeshMatNaturalHeight(ui, gtx, contentW)+gtx.Dp(inset.Top+inset.Bottom),
					gtx.Dp(unit.Dp(150)),
					gtx.Constraints.Max.Y-gtx.Dp(unit.Dp(30)),
				)
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							// The card takes the press before the dim beneath
							// can have it: the fields rarely fill the card, and
							// a click on what is left used to shut the dialog.
							absorbDialogCardPress(gtx, &ui.UnitMeshMatCardTag)
							return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return UnitMeshMatColumn(ui, gtx, func(gtx layout.Context) layout.Dimensions {
									return PanelScrollView(ui, gtx, &ui.UnitMeshMatScroll, func(gtx layout.Context) layout.Dimensions { return UnitMeshMatFields(ui, gtx) })
								})
							})
						}),
					)
				})
			})
		}),
	)
}

// unitMeshMatColumn is the card's whole content — title, the rules, the
// fields, the note and the buttons — with the fields handed in, so the same
// body can be laid out for real and laid out again to measure the height the
// card should be.
func UnitMeshMatColumn(ui *uiState, gtx layout.Context, fields layout.Widget) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshMatTitle(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return hLine(gtx, cHeaderLine, 1)
		}),
		layout.Flexed(1, fields),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshMatNote(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshMatFooter(ui, gtx) }),
	)
}

// unitMeshMatNaturalHeight measures that body at the width the card will give
// it, with input off and the recorded operations thrown away — the height a
// card sized to its content needs, taken the way the scroll panel takes the
// height of its own contents.
func UnitMeshMatNaturalHeight(ui *uiState, gtx layout.Context, contentW int) int {
	if contentW <= 0 {
		return 0
	}
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min = image.Pt(0, 0)
	measureGTX.Constraints.Max = image.Pt(contentW, panelScrollMeasureHeight)
	height := UnitMeshMatColumn(ui, measureGTX, func(gtx layout.Context) layout.Dimensions { return UnitMeshMatFields(ui, gtx) }).Size.Y
	macro.Stop()
	return height
}

// unitMeshMatTitle names the dialog and the mesh it edits.
func UnitMeshMatTitle(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(14), "Mesh materials")
			l.Color = cText
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), ui.UnitMeshMatName)
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// unitMeshMatFields is the scrollable body: one field per material, with the
// reference the file holds shown dimmed beneath a field that has moved away
// from it.
func UnitMeshMatFields(ui *uiState, gtx layout.Context) layout.Dimensions {
	if len(ui.UnitMeshMatEditors) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This mesh carries no materials.")
		l.Color = cTextDim
		return l.Layout(gtx)
	}
	var rc []layout.FlexChild
	for i := range ui.UnitMeshMatEditors {
		i := i
		ed := &ui.UnitMeshMatEditors[i]
		rc = append(rc, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(10), fmt.Sprintf("Material %d", i+1))
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Flexed(1, rowGap()),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return material.Editor(ui.Th, ed, "texture path").Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// The file's own value, shown only once the field has
					// drifted from it: that is when a reader needs to see
					// what is being replaced.
					if i >= len(ui.UnitMeshMatOrig) || strings.TrimSpace(ed.Text()) == ui.UnitMeshMatOrig[i] {
						return layout.Dimensions{}
					}
					l := material.Label(ui.Th, unit.Sp(9), "file: "+ui.UnitMeshMatOrig[i])
					l.Color = cTextDim
					l.MaxLines = 1
					return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return hLine(gtx, cBorder, 1)
					})
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rc...)
}

// unitMeshMatNote is the one line under the fields: the reason the dialog
// exists while there is no error, and the save's complaint when there is one.
func UnitMeshMatNote(ui *uiState, gtx layout.Context) layout.Dimensions {
	msg := "Preview draws the map behind this card; Save WHM writes the path into the file."
	col := cTextDim
	if ui.UnitMeshMatErr != "" {
		msg = ui.UnitMeshMatErr
		col = cProblem
	}
	l := material.Label(ui.Th, unit.Sp(9), msg)
	l.Color = col
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
}

// unitMeshMatFooter is Preview, Save WHM and Cancel. Save is disabled while
// there is no file to write to.
func UnitMeshMatFooter(ui *uiState, gtx layout.Context) layout.Dimensions {
	noFile := ui.ModTree == nil || ui.UnitModelPath == ""
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &ui.UnitMeshMatPreview, "Preview")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButtonDisabled(ui, gtx, &ui.UnitMeshMatSave, "Save WHM", noFile)
		}),
		layout.Flexed(1, rowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &ui.UnitMeshMatCancel, "Cancel")
		}),
	)
}
