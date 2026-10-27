package app

import (
	"dow-mod-studio/internal/app/uikit"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/fogleman/fauxgl"
	"image"
	"time"
)

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
		l.Color = uikit.CTextDim
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
		l.Color = uikit.CTextDim
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
		l.Color = uikit.CTextDim
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
			func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) })
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
					func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) })
			}))
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitBoneKeyListHeader(ui, gtx, v)
			}))
			refs := BoneKeyRefs(v)
			EnsureUnitBoneKeyClicks(ui, len(refs))
			BindBoneKeySelection(ui, v)
			keyIdx := 0
			for idx, k := range t.PosKey {
				ref := boneKeyRef{Mode: 0, Frame: frameAt(float64(k.Time), v.Anim.NumFrames)}
				c := &ui.UnitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					BoneKeyRowClicked(ui, v, ref, refs, UnitListModifiers(ui))
				}
				selected := BoneKeyRowPicked(ui, v, ref)
				text := fmt.Sprintf("pos %d  Frame %d  %.3f, %.3f, %.3f", idx+1, ref.Frame+1, k.V[0], k.V[1], k.V[2])
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitBoneKeyRow(ui, gtx, c, selected, text)
				}))
				keyIdx++
			}
			for idx, k := range t.RotKey {
				ref := boneKeyRef{Mode: 1, Frame: frameAt(float64(k.Time), v.Anim.NumFrames)}
				c := &ui.UnitBoneKeyClicks[keyIdx]
				if c.Clicked(gtx) {
					BoneKeyRowClicked(ui, v, ref, refs, UnitListModifiers(ui))
				}
				selected := BoneKeyRowPicked(ui, v, ref)
				text := fmt.Sprintf("rot %d  Frame %d  %.3f, %.3f, %.3f, %.3f", idx+1, ref.Frame+1, k.V[0], k.V[1], k.V[2], k.V[3])
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
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		}),
		layout.Flexed(1, rowGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if n := len(PickedBoneKeyRefs(ui, v)); n > 1 {
				l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d picked", n))
				l.Color = uikit.CTextDim
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
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
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
		ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
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
		ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
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
	case shift && ui.UnitBoneKeySelAnchor.Mode >= 0:
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
		LogInfo("bone: Shift+click spans %d row(s), frames %d..%d", hi-lo+1, refs[lo].Frame, refs[hi].Frame)
	case ctrl:
		if ui.UnitBoneKeySel[ref] {
			delete(ui.UnitBoneKeySel, ref)
			LogInfo("bone: Ctrl+click takes Frame %d (%s) out of the selection", ref.Frame, boneGroupLog(ref.Mode))
		} else {
			ui.UnitBoneKeySel[ref] = true
			ui.UnitBoneKeySelAnchor = ref
			LogInfo("bone: Ctrl+click adds Frame %d (%s) to the selection", ref.Frame, boneGroupLog(ref.Mode))
		}
	default:
		ui.UnitBoneKeySel = map[boneKeyRef]bool{ref: true}
		ui.UnitBoneKeySelAnchor = ref
	}
	// The picked key follows the click: the clicked row while it is still
	// among the picked ones, otherwise the last row still picked — in list
	// order, so the choice does not depend on map iteration — and none when
	// the selection is empty.
	picked := boneKeyRef{Mode: -1}
	if ui.UnitBoneKeySel[ref] {
		picked = ref
	} else {
		for _, r := range refs {
			if ui.UnitBoneKeySel[r] {
				picked = r
			}
		}
	}
	if at := BoneKeyIndexAtFrame(v, picked.Frame, picked.Mode); picked.Mode >= 0 && at >= 0 {
		v.BoneKeyIndex, v.BoneKeyMode = at, picked.Mode
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
		sel[boneKeyRef{Mode: 0, Frame: frame}] = true
	}
	if rotAt >= 0 {
		sel[boneKeyRef{Mode: 1, Frame: frame}] = true
	}
	ui.UnitBoneKeySel = sel
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: mode, Frame: frame}
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	LogInfo("bone: step picked frame %d: posKey=%v rotKey=%v picked=%s",
		frame+1, posAt >= 0, rotAt >= 0, boneGroupLog(mode))
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
	bg := uikit.CPanelBg
	switch {
	case selected:
		bg = uikit.CSelectBg
	case c.Hovered():
		bg = uikit.CMenuHover
	}
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
		sz := image.Pt(gtx.Constraints.Max.X, h)
		gtx.Constraints.Min = sz
		gtx.Constraints.Max = sz
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				if bg != uikit.CPanelBg {
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
							l.Color = uikit.CText
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						layout.Flexed(1, flexGap()),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if note == "" {
								return layout.Dimensions{}
							}
							l := material.Label(ui.Th, unit.Sp(9), note)
							l.Color = uikit.CTextDim
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}
