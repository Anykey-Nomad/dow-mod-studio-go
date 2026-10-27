package unit

import (
	viewer3d "dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/app/viewer"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/unitdef"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"errors"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
	"image"
	"image/color"
	"math"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
)

// UnitViewportSideDp is the width of the panel beside the canvas.
const UnitViewportSideDp = 300

// unitModelSideEmpty is what the controls area shows when no model is loaded.
func UnitModelSideEmpty(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	l := material.Label(ui.Th, unit.Sp(9), "No model is loaded.")
	l.Color = uikit.CTextDim
	return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
}

// UnitPlaybackStripDp is the height of the playback row: one line of transport
// buttons, the frame readout and the frames field, plus the row's own padding.
// It is fixed so the canvas above it can be worked out from the window's size —
// the viewport's tests do exactly that arithmetic — and so the strip does not
// grow a line taller when a hint wraps.
const UnitPlaybackStripDp = 38

// UnitPlaybackRow gives the strip its height and its padding, whatever is
// drawn inside it.
func UnitPlaybackRow(gtx layout.Context, content layout.Widget) layout.Dimensions {
	d := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, content)
	if h := gtx.Dp(unit.Dp(UnitPlaybackStripDp)); d.Size.Y < h {
		d.Size.Y = h
	}
	return d
}

// unitModelTransportButtons draws the viewport's shared playback row after its
// caller has handled modelControlClicks for this frame. It is content only —
// the strip it sits in gives the row its padding and its height.
func UnitModelTransportButtons(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
	v := t.Model
	none := len(v.Model.Animations) == 0
	gtx = viewer.ModelRowHeight(gtx)
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &t.ModelPlay, viewer.ModelPlayLabel(v), none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &t.ModelPrevAnim, "◀", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &t.ModelNextAnim, "▶", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &t.ModelStepBack, "|◀", none)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &t.ModelStepFwd, "▶|", none)
		}),
	)
}

// UnitFrameLabelDp is the width the frame readout is given. The digits of the
// frame number and the key suffix beside it come and go as the playhead moves,
// and a label that grew with them shoved the frames field, the Set button and
// the duration readout sideways every few frames. The box is a fixed width
// instead, and the text wraps inside it rather than widening it.
const UnitFrameLabelDp = 132

// unitModelFrameLabel is where the play head is: the frame on show out of the
// frames there are, which is what a single step of the buttons moves. When the
// playhead stands on a keyframe of the clip, the readout says which key it is
// out of how many — that is the number a reader stepping between keys needs,
// and it cannot be told from the picture.
func UnitModelFrameLabel(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer) layout.Dimensions {
	text := "no frames to step through"
	if frame, frames := viewer.CurrentFrame(v); frames > 0 {
		text = fmt.Sprintf("frame %d of %d", frame+1, frames)
		if keys := viewer.ClipKeyFrames(v.Anim); len(keys) > 0 {
			for i, k := range keys {
				if k == frame {
					text += fmt.Sprintf(" · key %d/%d", i+1, len(keys))
					break
				}
			}
		}
	}
	l := material.Label(ui.Th, unit.Sp(9), text)
	l.Color = uikit.CTextDim
	l.MaxLines = 2
	w := gtx.Dp(unit.Dp(UnitFrameLabelDp))
	gtx.Constraints.Max.X = w
	d := layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	d.Size.X = w
	return d
}

// unitModelViewButtons is the viewport's reset row. WHM tabs reuse it as well.
func UnitModelViewButtons(ui *uistate.State, gtx layout.Context, t *uistate.Tab, showListToggle bool) layout.Dimensions {
	gtx = viewer.ModelRowHeight(gtx)
	return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return uikit.SmallButton(ui, gtx, &t.ModelReset, "Reset view")
				}),
				layout.Flexed(1, uikit.RowGap()),
			)
		})
}

// unitModelFileLabel is the model file the viewport is showing.
func UnitModelFileLabel(ui *uistate.State) string {
	return filepath.Base(ui.UnitModelPath)
}

// unitListModifiers is the state of Ctrl and Shift at the moment a row is
// read: the tracked key events, ORed with the live OS state, since either one
// alone can miss a press on some backends.
func UnitListModifiers(ui *uistate.State) key.Modifiers {
	m := uikit.HeldModifiers()
	if ui.UnitBoneKeyCtrlHeld {
		m |= key.ModCtrl
	}
	if ui.UnitBoneKeyShiftHeld {
		m |= key.ModShift
	}
	return m
}

// boneKeyRowPicked says whether a row wears the selection colour. While the
// list holds a selection that is what decides; with nothing picked on the list
// the key the strip or the playhead picked is the one on show, which is what
// the list highlighted before it could hold a selection at all.
func BoneKeyRowPicked(ui *uistate.State, v *viewer.ModelViewer, ref viewer.BoneKeyRef) bool {
	if len(ui.UnitBoneKeySel) > 0 {
		return ui.UnitBoneKeySel[ref]
	}
	got, ok := viewer.PickedBoneKeyRef(v)
	return ok && got == ref
}

// BoneFieldAxes names the three fields of a group, in the order they are laid
// out, so an error can point at the axis that would not parse.
var BoneFieldAxes = [3]string{"X", "Y", "Z"}

// BoneFieldTexts is one group's three fields as a single loggable line.
func BoneFieldTexts(eds *[3]widget.Editor) string {
	return strings.TrimSpace(eds[0].Text()) + " " +
		strings.TrimSpace(eds[1].Text()) + " " +
		strings.TrimSpace(eds[2].Text())
}

// unitBoneKeyFieldsRow is one row of the pose bar's typed group: the group's
// name, then X, Y and Z. Each field submits on Enter, which is what applies
// the triple to the key it belongs to.
func UnitBoneKeyFieldsRow(ui *uistate.State, gtx layout.Context, title string, eds *[3]widget.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), title)
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, BoneFieldAxes[0], &eds[0])
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, BoneFieldAxes[1], &eds[1])
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, BoneFieldAxes[2], &eds[2])
		}),
	)
}

// unitBoneKeyField is one typed axis: its letter, and a small bordered editor
// that submits on Enter. The fixed width keeps the three of a row aligned and
// stops a long value from pushing its neighbours out of the panel.
func UnitBoneKeyField(ui *uistate.State, gtx layout.Context, axis string, ed *widget.Editor) layout.Dimensions {
	ed.SingleLine = true
	ed.Submit = true
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), axis)
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(2)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Pt(gtx.Dp(56), gtx.Dp(20))
			gtx.Constraints.Max = image.Pt(gtx.Dp(56), gtx.Dp(22))
			b := widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
			return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := material.Editor(ui.Th, ed, "")
					w.TextSize = unit.Sp(10)
					w.HintColor = uikit.CTextDim
					return w.Layout(gtx)
				})
			})
		}),
	)
}

// parseBoneGroup reads one group of the typed fields as numbers, naming the
// axis that would not parse. An Enter and both buttons go through it, so a
// value is read the same way however it is applied.
func ParseBoneGroup(ui *uistate.State, mode int) ([3]float64, error) {
	var val [3]float64
	eds := &ui.UnitBonePosEd
	kind := "position"
	if mode == 1 {
		eds = &ui.UnitBoneRotEd
		kind = "rotation"
	}
	for i := range eds {
		spec := unitdef.BonePositionField
		if mode == 1 {
			spec = unitdef.BoneRotationField
		}
		f, _, err := spec.Parse(eds[i].Text())
		if err != nil {
			return val, fmt.Errorf("%s %s: %v", kind, BoneFieldAxes[i], err)
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
func ModifiedBoneAxes(ui *uistate.State, mode int) ([]int, bool) {
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

// MaxAnimFrames is the largest frame count the frames field takes. A clip is a
// few hundred frames long in the shipped models, and a number in the millions
// only makes every key land on the same frame.
const MaxAnimFrames = 100000

// unitAnimFramesRow is the clip's frame count: the number typed in, the Set
// button beside it, and on the right what that number is in the file — the
// clip's length in seconds and the rate the two of them work out to.
func UnitAnimFramesRow(ui *uistate.State, gtx layout.Context, v *viewer.ModelViewer) layout.Dimensions {
	hint := "no clip on show"
	canSet := v != nil && v.Anim != nil
	if canSet {
		hint = fmt.Sprintf("%.2f s · %.1f fps", viewer.AnimClipSeconds(v.Anim), viewer.AnimFramesPerSecond(v.Anim))
	}
	// A count the field would not take says why where the length is shown: it
	// is the one place the frames row has to report in, and it is on screen in
	// every list mode.
	hintColor := uikit.CTextDim
	if ui.UnitAnimFramesErr != "" {
		hint = ui.UnitAnimFramesErr
		hintColor = uikit.Rgb(0xB00020)
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), "Frames")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return UnitBoneKeyField(ui, gtx, "", &ui.UnitAnimFramesEd)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitAnimFramesSetClick, "Set", !canSet)
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
func SyncAnimFramesField(ui *uistate.State, v *viewer.ModelViewer) {
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

// BoneAngleSliderDeg is the inverse: the angle, in 1..360 degrees, that a
// slider position means.
func BoneAngleSliderDeg(frac float32) float64 {
	d := 1 + float64(frac)*359
	if d < 1 {
		d = 1
	}
	if d > 360 {
		d = 360
	}
	return d
}

// boneKeyShiftFrames is the shift the field holds, in frames: a whole number,
// with 1 standing in for an empty or unreadable field so the buttons still
// move something.
func BoneKeyShiftFrames(ui *uistate.State) int {
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

// BoneUndoLimit is how many key edits are kept for Undo. The stack holds one
// small copied track per step, so a dozen is plenty for a session and bounds
// what an extended editing binge can hold in memory.
const BoneUndoLimit = 16

// pushBoneUndo records a snapshot taken before an edit as the next undo step.
// A snapshot with nothing in it (no clip or bone at the time) is dropped, and
// the stack is capped so it cannot grow without bound.
func PushBoneUndo(ui *uistate.State, s viewer.BoneTrackSnapshot, ok bool) {
	if !ok || (len(s.PosKey) == 0 && len(s.RotKey) == 0 && s.Anim == nil) {
		return
	}
	ui.UnitBoneUndo = append(ui.UnitBoneUndo, s)
	if len(ui.UnitBoneUndo) > BoneUndoLimit {
		ui.UnitBoneUndo = ui.UnitBoneUndo[len(ui.UnitBoneUndo)-BoneUndoLimit:]
	}
}

// BoneTrackKeys is how many keys of the clip on show belong to bone i,
// counting position and rotation keys together. Zero means the bone is held
// still for the whole clip, which is the common case and worth saying out
// loud rather than leaving a reader to infer it from a still picture.
func BoneTrackKeys(anim *uistate.WhmAnimation, i int) int {
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
func UnitBoneKeyRow(ui *uistate.State, gtx layout.Context, c *widget.Clickable, selected bool, text string) layout.Dimensions {
	bg := uikit.CPanelBg
	if selected {
		bg = uikit.CSelectBg
	}
	h := gtx.Dp(unit.Dp(uikit.UnitRowHeightDp - 4))
	sz := image.Pt(gtx.Constraints.Max.X, h)
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
					Rrect(ui, gtx, bg, gtx.Constraints.Max, unit.Dp(2))
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(10), text)
					l.Color = uikit.CText
					l.MaxLines = 1
					return l.Layout(gtx)
				})
			}),
		)
	})
}

// UnitViewportRowText keeps a long clip name inside the panel, since the names
// are the game's and run to forty characters.
func UnitViewportRowText(name string) string {
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
func EnsureUnitAnimClicks(ui *uistate.State, n int) {
	if len(ui.UnitAnimClicks) != n {
		ui.UnitAnimClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitMarkerClicks sizes the markers list's click targets, the same way
// as the clip list's: rebuilt per model rather than grown into.
func EnsureUnitMarkerClicks(ui *uistate.State, n int) {
	if len(ui.UnitMarkerClicks) != n {
		ui.UnitMarkerClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitBoneClicks sizes the bones list's click targets, the same way as
// the other two lists': rebuilt per model rather than grown into.
func EnsureUnitBoneClicks(ui *uistate.State, n int) {
	if len(ui.UnitBoneClicks) != n {
		ui.UnitBoneClicks = make([]widget.Clickable, n)
	}
}

// ensureUnitBoneKeyClicks sizes the bone keyframe row click targets for the
// currently picked bone, rebuilt whenever the bone selection changes.
func EnsureUnitBoneKeyClicks(ui *uistate.State, n int) {
	if len(ui.UnitBoneKeyClicks) != n {
		ui.UnitBoneKeyClicks = make([]widget.Clickable, n)
	}
}

// unitModelLoaded reports whether the viewport has a model on screen, which is
// what a test checks rather than what it can see.
func UnitModelLoaded(ui *uistate.State) bool {
	return UnitModelViewer(ui) != nil
}

// unitModelAnimNames are the clips on show, which the tests compare against.
func UnitModelAnimNames(ui *uistate.State) []string {
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

// UnitModelClipDuration is how long one pass of the clip on show takes, in
// seconds. A clip with no frame count is reported as zero rather than as the
// fallback rate, since a zero here means "nothing to wait for".
func UnitModelClipDuration(v *viewer.ModelViewer) float64 {
	if v == nil || v.Anim == nil || v.Anim.NumFrames <= 0 {
		return 0
	}
	return float64(v.Anim.NumFrames) / viewer.AnimFramesPerSecond(v.Anim)
}

func Rrect(ui *uistate.State, gtx layout.Context, bg color.NRGBA, sz image.Point, r unit.Dp) {
	paint.Fill(gtx.Ops, bg)
}

// UnitSectionViewport names the section, so the layout does not repeat the
// string of the menu entry it is keyed on.
const UnitSectionViewport = "Viewport"

// UnitModelArtRoot is the folder animator paths are relative to.
const UnitModelArtRoot = "art/ebps"

// UnitGroundTexture is the default terrain texture shown beneath unit models.
const UnitGroundTexture = "Data/art/scenarios/textures/detail/jungle_ground_02.tga"

// ErrNoUnitModel is what a unit whose model cannot be found anywhere reports,
// as distinct from a model that was found and would not decode.
var ErrNoUnitModel = errors.New("no model file for this unit")

// UnitModelResult carries a decoded model back to the UI goroutine. The unit it
// was read for travels with it, since a load started for one unit can finish
// after the reader has moved on to another.
type UnitModelResult = uistate.UnitModelResult

// UnitModelSource is everything about a unit that the loader needs, copied off
// the UI goroutine before the load starts so the worker touches no UI state.
type UnitModelSource struct {
	Unit     string // the unit .rgd's tree path
	Animator string // entity_blueprint_ext.animator, may be empty
	Name     string // the unit's file name without extension
	Race     string // the folder name under races
	Kind     string // "troops" or "structures"
}

// NewUnitModelSource reads what a unit says about its model.
func NewUnitModelSource(tree *modtree.ModTree, d *uistate.UnitDetails) (UnitModelSource, bool) {
	if d == nil {
		return UnitModelSource{}, false
	}
	src := UnitModelSource{
		Unit: d.Entry.Path,
		Name: d.Entry.Name,
		Race: d.Entry.Race,
		Kind: d.Entry.Kind.Folder,
	}
	if src.Name == "" {
		if base := d.Entry.Path; base != "" {
			src.Name = strings.TrimSuffix(filepath.Base(base), filepath.Ext(base))
		}
	}
	if d.File != nil {
		src.Animator = UnitAnimatorName(d.File)
	}
	if src.Name == "" && src.Animator == "" {
		return UnitModelSource{}, false
	}
	return src, true
}

// UnitAnimatorName reads entity_blueprint_ext.animator out of a unit .rgd, or
// "" when the file does not carry one. The field is a plain string, not a
// reference: it names a model folder path rather than an asset the .rgd
// resolves, which is why it is read here rather than through resolveRgdRef.
func UnitAnimatorName(rf *rgd.RgdFile) string {
	if rf == nil || rf.Root == nil {
		return ""
	}
	ext := unitdef.RgdFindChild(rf.Root, "entity_blueprint_ext")
	if ext == nil {
		return ""
	}
	e := unitdef.RgdFindChild(ext, "animator")
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.DisplayValue())
}

// UnitModelPath finds the .whm of a unit, or "" when there is none. The order
// is the one that works on the shipped data first and the one that rescues a
// mod's own naming last.
func UnitModelPath(tree *modtree.ModTree, src UnitModelSource) string {
	if tree == nil {
		return ""
	}
	rel := strings.ReplaceAll(strings.TrimSpace(src.Animator), "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	if rel != "" {
		if !strings.Contains(rel, ".") {
			rel += ".whm"
		}
		p := filepath.Join("Data", filepath.FromSlash(UnitModelArtRoot), filepath.FromSlash(rel))
		if tree.FindFile(p) != nil {
			return p
		}
	}
	// The folder the unit's .rgd itself lives in is where the game keeps the
	// model of a unit it renamed, so the sibling name is worth a try before
	// searching the whole tree.
	if src.Race != "" && src.Name != "" {
		dirs := []string{}
		if src.Kind != "" {
			dirs = append(dirs, src.Kind)
		}
		dirs = append(dirs, "", "troops", "structures")
		for _, d := range dirs {
			parts := []string{"Data", UnitModelArtRoot, "races", src.Race}
			if d != "" {
				parts = append(parts, d)
			}
			parts = append(parts, src.Name+".whm")
			p := filepath.Join(parts...)
			if tree.FindFile(p) != nil {
				return p
			}
		}
	}
	if src.Name != "" {
		if f := tree.FindByName(src.Name + ".whm"); f != nil {
			return f.FullPath
		}
	}
	return ""
}

// resetUnitModel drops the viewer and forgets which unit it was for, so the
// next request loads again. The worker is not stopped: it writes into a
// buffered channel nobody reads, which costs one model parse and no more.
func ResetUnitModel(ui *uistate.State) {
	if ui.UnitModel != nil && ui.UnitModel.Model != nil {
		// The hardware renderer caches the model's buffers, and the goroutine
		// that may be mid render holds the same pointer, so both go back.
		if ui.ModelGpu != nil && ui.UnitModel.Model.Model != nil {
			ui.ModelGpu.Forget(&ui.UnitModel.Model.Model.Model)
		}
		viewer.Close(ui.UnitModel.Model)
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
	ui.UnitViewportScroll = uistate.PanelScrollState{}
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
// SelectObjectAction reads the model's WHE graph and plays all clips
// selected by the named action together. Weighted motion links choose one
// variant independently for each motion; absent or unresolved WHE data leaves
// the viewport's ordinary single-clip default in place. The idle action also
// falls back to the pristine state's idle/stand clips when the graph has no
// idle action of its own.
func SelectObjectAction(v *viewer.ModelViewer, tree *modtree.ModTree, modelPath, actionName string) bool {
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
	var clips []*uistate.WhmAnimation
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
	viewer.SelectAnimations(v, clips, true)
	v.Playing = true
	return true
}

// activateUnitAction plays every clip the named action of the loaded unit
// model's .whe resolves to, combined in the viewport, and says so in the
// status bar. It answers false when no model is on screen or the action
// resolves to nothing, leaving the caller with its own fallback.
func ActivateUnitAction(ui *uistate.State, actionName string) bool {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return false
	}
	if !SelectObjectAction(v, ui.ModTree, ui.UnitModelPath, actionName) {
		return false
	}
	ui.StatusText = fmt.Sprintf("Action %q — playing %d clip(s) together",
		strings.TrimSpace(actionName), len(v.Anims))
	return true
}

// unitModelViewer is the viewport's viewer, or nil when no model is up.
func UnitModelViewer(ui *uistate.State) *viewer.ModelViewer {
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
func LookThroughCamera(v *viewer.ModelViewer, cam whm.Camera) {
	if v == nil || v.Model == nil {
		return
	}
	eye := whm.RootFrame.MulPosition(render.Vec3From(cam.Pos))
	focus := whm.RootFrame.MulPosition(render.Vec3From(cam.FocusPoint))
	v.CameraState = viewer3d.LookThroughCamera(v.CameraState, eye, focus, cam.Fov, cam.Name)
	viewer.Invalidate(v)
}

// resetCamera puts the orbit camera back to the framing viewer.NewModelViewer chose,
// and forgets any camera of the model's that was being looked through.
func ResetCamera(v *viewer.ModelViewer) {
	if v == nil {
		return
	}
	camera := viewer3d.ResetCamera(v.CameraState)
	v.CameraState = camera
	viewer.Invalidate(v)
}

// panCamera shifts the orbit target in the camera's screen plane. Pixel motion
// is scaled by the world-space width visible at the camera distance, so the
// drag has consistent sensitivity across zoom levels and viewport sizes.
func PanCamera(v *viewer.ModelViewer, dx, dy float64, height int) {
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
	viewer.Invalidate(v)
}

// MeshMaterialRefs returns the named mesh's material references in order. A
// mesh with no materials under that name yields nil, which is what stops the
// dialog opening on a mesh there is nothing to edit.
func MeshMaterialRefs(m *uistate.WhmModel, name string) []string {
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

// ApplyMeshMaterialRefs writes refs back onto the named mesh's materials, in
// the order MeshMaterialRefs collected them. An empty entry leaves that
// material as it was, and the shared-shader path is cleared alongside so the
// renderer keys the texture off the reference it was just given — the shape a
// model whose material label carries the path already has, and the field the
// game reads.
func ApplyMeshMaterialRefs(m *uistate.WhmModel, name string, refs []string) {
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

// EqualRefs reports whether two reference lists hold the same strings, so a
// Cancel that would change nothing skips the texture reload it would cost.
func EqualRefs(a, b []string) bool {
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
func UnitMeshMatFieldText(ui *uistate.State) []string {
	out := make([]string, len(ui.UnitMeshMatEditors))
	for i := range ui.UnitMeshMatEditors {
		out[i] = strings.TrimSpace(ui.UnitMeshMatEditors[i].Text())
	}
	return out
}

// reloadUnitModelMaterials restarts the texture lookup for the unit's viewer
// from the material references it now carries — the same step a fresh load
// takes, so a changed reference is resolved and drawn again.
func ReloadUnitModelMaterials(ui *uistate.State, v *viewer.ModelViewer) {
	t := ui.UnitModel
	if t == nil || v == nil || v.Model == nil || ui.ModTree == nil {
		return
	}
	t.ModelMaterialCh = make(chan viewer.ModelMaterialResult, 1)
	viewer.LoadModelMaterials(ui, v, t.ModelMaterialCh)
}

// openUnitMeshMaterialDialog fills one field per material of the named mesh
// with the reference it carries and puts the dialog up. A mesh with no
// materials leaves it down: there is nothing to edit and nothing to save.
func OpenUnitMeshMaterialDialog(ui *uistate.State, name string) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		return
	}
	refs := MeshMaterialRefs(v.Model, name)
	if len(refs) == 0 {
		return
	}
	ui.UnitMeshMatOpen = true
	ui.UnitMeshMatName = name
	ui.UnitMeshMatOrig = refs
	ui.UnitMeshMatErr = ""
	ui.UnitMeshMatScroll = uistate.PanelScrollState{}
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
func CloseUnitMeshMaterialDialog(ui *uistate.State, revert bool) {
	if revert {
		v := UnitModelViewer(ui)
		if v != nil && v.Model != nil && ui.UnitMeshMatName != "" {
			cur := MeshMaterialRefs(v.Model, ui.UnitMeshMatName)
			if !EqualRefs(cur, ui.UnitMeshMatOrig) {
				ApplyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, ui.UnitMeshMatOrig)
				ReloadUnitModelMaterials(ui, v)
				viewer.Invalidate(v)
			}
		}
	}
	ui.UnitMeshMatOpen = false
	ui.UnitMeshMatName = ""
	ui.UnitMeshMatOrig = nil
	ui.UnitMeshMatEditors = nil
	ui.UnitMeshMatErr = ""
	ui.UnitMeshMatScroll = uistate.PanelScrollState{}
}

// previewUnitMeshMaterialDialog applies the fields to the model on show and
// re-resolves its textures, so the map the path names appears behind the dim
// without touching the file.
func PreviewUnitMeshMaterialDialog(ui *uistate.State) {
	v := UnitModelViewer(ui)
	if v == nil || v.Model == nil {
		ui.UnitMeshMatErr = "no model is loaded"
		return
	}
	ApplyMeshMaterialRefs(v.Model, ui.UnitMeshMatName, UnitMeshMatFieldText(ui))
	ReloadUnitModelMaterials(ui, v)
	viewer.Invalidate(v)
	ui.UnitMeshMatErr = ""
}

// unitMeshMatColumn is the card's whole content — title, the rules, the
// fields, the note and the buttons — with the fields handed in, so the same
// body can be laid out for real and laid out again to measure the height the
// card should be.
func UnitMeshMatColumn(ui *uistate.State, gtx layout.Context, fields layout.Widget) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitMeshMatTitle(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.HLine(gtx, uikit.CHeaderLine, 1)
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
func UnitMeshMatNaturalHeight(ui *uistate.State, gtx layout.Context, contentW int) int {
	if contentW <= 0 {
		return 0
	}
	macro := op.Record(gtx.Ops)
	measureGTX := gtx.Disabled()
	measureGTX.Constraints.Min = image.Pt(0, 0)
	measureGTX.Constraints.Max = image.Pt(contentW, uikit.PanelScrollMeasureHeight)
	height := UnitMeshMatColumn(ui, measureGTX, func(gtx layout.Context) layout.Dimensions { return UnitMeshMatFields(ui, gtx) }).Size.Y
	macro.Stop()
	return height
}

// unitMeshMatTitle names the dialog and the mesh it edits.
func UnitMeshMatTitle(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(14), "Mesh materials")
			l.Color = uikit.CText
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), ui.UnitMeshMatName)
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// unitMeshMatFields is the scrollable body: one field per material, with the
// reference the file holds shown dimmed beneath a field that has moved away
// from it.
func UnitMeshMatFields(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	if len(ui.UnitMeshMatEditors) == 0 {
		l := material.Label(ui.Th, unit.Sp(9), "This mesh carries no materials.")
		l.Color = uikit.CTextDim
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
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						}),
						layout.Flexed(1, uikit.RowGap()),
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
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return uikit.HLine(gtx, uikit.CBorder, 1)
					})
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rc...)
}

// unitMeshMatNote is the one line under the fields: the reason the dialog
// exists while there is no error, and the save's complaint when there is one.
func UnitMeshMatNote(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	msg := "Preview draws the map behind this card; Save WHM writes the path into the file."
	col := uikit.CTextDim
	if ui.UnitMeshMatErr != "" {
		msg = ui.UnitMeshMatErr
		col = uikit.CProblem
	}
	l := material.Label(ui.Th, unit.Sp(9), msg)
	l.Color = col
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
}

// unitMeshMatFooter is Preview, Save WHM and Cancel. Save is disabled while
// there is no file to write to.
func UnitMeshMatFooter(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	noFile := ui.ModTree == nil || ui.UnitModelPath == ""
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButton(ui, gtx, &ui.UnitMeshMatPreview, "Preview")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitMeshMatSave, "Save WHM", noFile)
		}),
		layout.Flexed(1, uikit.RowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.SmallButton(ui, gtx, &ui.UnitMeshMatCancel, "Cancel")
		}),
	)
}
