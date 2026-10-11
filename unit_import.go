package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// This file is the "Import frame's timeline" keyframe clipboard: a second
// .whm is read for the keys its clips hold, the panel lists those clips and
// the frames the picked bone has keys on, and a multi-selection of those
// frames is copied or written over the picked bone's track in the clip on
// show. The two files are assumed to carry the same skeleton — the match is
// by bone name — so a frame's keys are the same pose in both.

// unitImpRef names one frame of one animation of the imported file: which
// clip it belongs to and which frame it stands on. The selection is keyed
// this way because several clips can be open at once and a frame number on
// its own says which of them it came from.
type unitImpRef struct {
	anim  int
	frame int
}

// openUnitImportDialog shows the file dialog over the module's .whm files; a
// path typed in full or a file from the list is read when the dialog is
// confirmed.
func (ui *uiState) openUnitImportDialog() {
	ui.openFilePicker(pickConfig{
		title:    "Import frame's timeline",
		subtitle: "Choose the .whm to read keyframes from — a path, or a file from the module",
		hint:     `Data\art\ebps\...\model.whm`,
		exts:     []string{".whm"},
		modTree:  true,
		onPick: func(path string) (bool, string) {
			before := ui.unitImpModel
			ui.unitImportRead(path)
			if ui.unitImpModel == before {
				return false, ui.unitImpStatus
			}
			return true, ""
		},
	})
}

// unitImportRead loads the picked .whm from disk or from the module and
// keeps its parsed model for the panel to browse. Nothing of the source is
// written back: it is a clipboard, read once per dialog confirmation.
func (ui *uiState) unitImportRead(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		ui.unitImpStatus = "No file was chosen"
		return
	}
	var data []byte
	var err error
	if filepath.IsAbs(path) {
		data, err = os.ReadFile(path)
	} else if ui.modTree != nil {
		data, err = ui.modTree.ReadFile(path)
		if err != nil {
			// A bare file name may still be on disk beside the module.
			if data2, err2 := os.ReadFile(path); err2 == nil {
				data, err = data2, nil
			}
		}
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		ui.unitImpStatus = "Read failed: " + err.Error()
		LogInfo("bone: import: read %q failed: %v", path, err)
		return
	}
	m, perr := ParseWhmModel(data)
	if perr != nil {
		ui.unitImpStatus = "Not a .whm: " + perr.Error()
		LogInfo("bone: import: parse %q failed: %v", path, perr)
		return
	}
	ui.unitImpModel = m
	ui.unitImpPath = path
	ui.unitImpOpen = make([]bool, len(m.Animations))
	ui.unitImpAnimClicks = make([]widget.Clickable, len(m.Animations))
	ui.unitImpFrameClicks = make(map[unitImpRef]*widget.Clickable)
	ui.unitImpSel = make(map[unitImpRef]bool)
	ui.unitImpAnchor = unitImpRef{anim: -1}
	ui.unitImpStatus = fmt.Sprintf("Imported %s — %d clip(s)", filepath.Base(path), len(m.Animations))
	LogInfo("bone: import: loaded %q (%d animations, %d bones)", path, len(m.Animations), len(m.Bones))
}

// unitImportBoneIndex is the source file's bone matching the picked bone of
// the edited model, by name — the two files are assumed to carry the same
// skeleton, which is the premise of the whole feature. -1 means no match.
func (ui *uiState) unitImportBoneIndex(v *modelViewer) int {
	if ui.unitImpModel == nil || v == nil || v.model == nil || v.boneIndex < 0 || v.boneIndex >= len(v.model.Bones) {
		return -1
	}
	name := v.model.Bones[v.boneIndex].Name
	if name == "" {
		return -1
	}
	return ui.unitImpModel.boneIndexByName(name)
}

// unitImportFrames lists the frames the matched bone has keys on in one
// source clip: position and rotation keys together, in frame order, which
// is the same walk the panel's own keyframe list takes over a track. A
// frame carrying only one of the two kinds is still a frame worth copying —
// the note on its row says which kind it holds.
func unitImportFrames(a *WhmAnimation, bone int) []int {
	if a == nil || bone < 0 {
		return nil
	}
	var track *WhmTrack
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			track = &a.Bones[i]
			break
		}
	}
	if track == nil {
		return nil
	}
	seen := make(map[int]bool)
	for i := range track.PosKey {
		seen[frameAt(float64(track.PosKey[i].Time), a.NumFrames)] = true
	}
	for i := range track.RotKey {
		seen[frameAt(float64(track.RotKey[i].Time), a.NumFrames)] = true
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]int, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Ints(out)
	return out
}

// unitImportFrameKinds says which key kinds a source frame carries for the
// matched bone, as the row's note: "pos+rot", "pos" or "rot".
func unitImportFrameKinds(a *WhmAnimation, bone, frame int) string {
	if a == nil || bone < 0 {
		return ""
	}
	var track *WhmTrack
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			track = &a.Bones[i]
			break
		}
	}
	if track == nil {
		return ""
	}
	pos, rot := false, false
	for i := range track.PosKey {
		if frameAt(float64(track.PosKey[i].Time), a.NumFrames) == frame {
			pos = true
			break
		}
	}
	for i := range track.RotKey {
		if frameAt(float64(track.RotKey[i].Time), a.NumFrames) == frame {
			rot = true
			break
		}
	}
	switch {
	case pos && rot:
		return "pos+rot"
	case pos:
		return "pos"
	case rot:
		return "rot"
	}
	return ""
}

// unitImportRowClick returns the row's click target, allocating it the
// first time a frame is shown. The map rather than a slice: frames appear
// and vanish as clips are opened, and a click target has to stay the same
// widget between frames for a press to complete.
func (ui *uiState) unitImportRowClick(ref unitImpRef) *widget.Clickable {
	if ui.unitImpFrameClicks == nil {
		ui.unitImpFrameClicks = make(map[unitImpRef]*widget.Clickable)
	}
	c := ui.unitImpFrameClicks[ref]
	if c == nil {
		c = &widget.Clickable{}
		ui.unitImpFrameClicks[ref] = c
	}
	return c
}

// unitImportRowClicked is a press on a frame row: a plain click picks that
// frame alone, Ctrl adds or takes it away, Shift picks every frame of the
// same clip between the anchor and this one. Shift only spans one clip —
// a range across two clips has no timeline to be a range on.
func (ui *uiState) unitImportRowClicked(ref unitImpRef, frames []int, mods key.Modifiers) {
	if ui.unitImpSel == nil {
		ui.unitImpSel = make(map[unitImpRef]bool)
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	switch {
	case shift && ui.unitImpAnchor.anim == ref.anim && ui.unitImpAnchor.anim >= 0:
		lo, hi := -1, -1
		for i, f := range frames {
			if f == ui.unitImpAnchor.frame {
				lo = i
			}
			if f == ref.frame {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			ui.unitImpSel = map[unitImpRef]bool{ref: true}
			ui.unitImpAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.unitImpSel = make(map[unitImpRef]bool, hi-lo+1)
		for _, f := range frames[lo : hi+1] {
			ui.unitImpSel[unitImpRef{anim: ref.anim, frame: f}] = true
		}
		LogInfo("bone: import: Shift+click spans %d frame(s) of clip %d", hi-lo+1, ref.anim)
	case ctrl:
		if ui.unitImpSel[ref] {
			delete(ui.unitImpSel, ref)
		} else {
			ui.unitImpSel[ref] = true
			ui.unitImpAnchor = ref
		}
	default:
		ui.unitImpSel = map[unitImpRef]bool{ref: true}
		ui.unitImpAnchor = ref
	}
}

// unitImportApply writes the selected source frames into the picked bone's
// track of the clip on show. With replace the key on the frame is
// overwritten and a missing one is created; without it only the missing
// ones are created, so a first pass fills gaps and a second overwrites.
// Frame numbers are mapped by frame number, clamped to the clip's length —
// the two files are assumed to carry clips of the same length, which is
// what "analogous set of bones" means in practice.
func (ui *uiState) unitImportApply(v *modelViewer, replace bool) (added, replaced, skipped int) {
	what := "copy"
	if replace {
		what = "replace"
	}
	if ui.unitImpModel == nil {
		ui.unitBoneKeyErr = "no file imported"
		return 0, 0, 0
	}
	if v == nil || v.anim == nil {
		ui.unitBoneKeyErr = "no clip is on show"
		return 0, 0, 0
	}
	if v.boneIndex < 0 || v.boneIndex >= len(v.model.Bones) {
		ui.unitBoneKeyErr = "no bone is picked"
		return 0, 0, 0
	}
	src := ui.unitImportBoneIndex(v)
	if src < 0 {
		ui.unitBoneKeyErr = "the imported file has no bone named " + v.model.Bones[v.boneIndex].Name
		return 0, 0, 0
	}
	// The refs are walked in a fixed order — clip then frame — so the
	// result does not depend on map iteration.
	refs := make([]unitImpRef, 0, len(ui.unitImpSel))
	for ref := range ui.unitImpSel {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].anim != refs[j].anim {
			return refs[i].anim < refs[j].anim
		}
		return refs[i].frame < refs[j].frame
	})
	if len(refs) == 0 {
		ui.unitBoneKeyErr = "no frame is picked"
		return 0, 0, 0
	}
	t := v.boneTrack()
	if t == nil {
		ui.unitBoneKeyErr = "the bone has no track in this clip"
		return 0, 0, 0
	}
	// The keys written below are also what the copy history keeps: a Copy or
	// Replace records the source keys it took, so they can be found again.
	var keys boneKeyClip
	for _, ref := range refs {
		if ref.anim < 0 || ref.anim >= len(ui.unitImpModel.Animations) {
			skipped++
			continue
		}
		sa := &ui.unitImpModel.Animations[ref.anim]
		var st *WhmTrack
		for i := range sa.Bones {
			if sa.Bones[i].Bone == src {
				st = &sa.Bones[i]
				break
			}
		}
		if st == nil {
			skipped++
			continue
		}
		frame := ref.frame
		if frame < 0 {
			skipped++
			continue
		}
		if frame >= v.anim.NumFrames {
			// The source clip runs longer than the one on show:
			// the frame has nowhere to land.
			skipped++
			continue
		}
		// Position keys the source frame carries.
		for i := range st.PosKey {
			if frameAt(float64(st.PosKey[i].Time), sa.NumFrames) != frame {
				continue
			}
			keys.pos = append(keys.pos, st.PosKey[i])
			a, r := writeImportPos(t, v, frame, st.PosKey[i].V, replace)
			added += a
			replaced += r
		}
		// Rotation keys, the same way.
		for i := range st.RotKey {
			if frameAt(float64(st.RotKey[i].Time), sa.NumFrames) != frame {
				continue
			}
			keys.rot = append(keys.rot, st.RotKey[i])
			a, r := writeImportRot(t, v, frame, st.RotKey[i].V, replace)
			added += a
			replaced += r
		}
	}
	v.poseValid = false
	v.invalidate()
	if added+replaced > 0 {
		ui.unitModelWhmDirty = true
		ui.unitBoneKeyErr = ""
		ui.unitClips.push(clipEntry{
			kind:  clipImportKeys,
			label: fmt.Sprintf("%d key(s) from %s", len(keys.pos)+len(keys.rot), filepath.Base(ui.unitImpPath)),
			at:    time.Now(),
			keys:  &keys,
		})
		ui.fillBoneKeyFields(v, 0)
		ui.fillBoneKeyFields(v, 1)
	} else if ui.unitBoneKeyErr == "" {
		ui.unitBoneKeyErr = "nothing to write"
	}
	LogInfo("bone: import %s: %d key(s) added, %d replaced, %d skipped (bone=%q clip=%q)",
		what, added, replaced, skipped, t.Name, v.anim.Name)
	return added, replaced, skipped
}

// writeImportPos puts one source position value on the frame of the picked
// bone's track. It reports whether a key was created and whether an
// existing one was written over.
func writeImportPos(t *WhmTrack, v *modelViewer, frame int, val [3]float32, replace bool) (added, replaced int) {
	if idx := v.boneKeyIndexAtFrame(frame, 0); idx >= 0 {
		if !replace {
			return 0, 0
		}
		t.PosKey[idx].V = val
		return 0, 1
	}
	now := timeForFrame(frame, v.anim.NumFrames)
	at := 0
	for at < len(t.PosKey) && t.PosKey[at].Time <= now {
		at++
	}
	t.PosKey = append(t.PosKey, WhmKey3{})
	copy(t.PosKey[at+1:], t.PosKey[at:])
	t.PosKey[at] = WhmKey3{Time: now, V: val}
	return 1, 0
}

// writeImportRot is writeImportPos for rotation keys.
func writeImportRot(t *WhmTrack, v *modelViewer, frame int, val [4]float32, replace bool) (added, replaced int) {
	if idx := v.boneKeyIndexAtFrame(frame, 1); idx >= 0 {
		if !replace {
			return 0, 0
		}
		t.RotKey[idx].V = val
		return 0, 1
	}
	now := timeForFrame(frame, v.anim.NumFrames)
	at := 0
	for at < len(t.RotKey) && t.RotKey[at].Time <= now {
		at++
	}
	t.RotKey = append(t.RotKey, WhmKey4{})
	copy(t.RotKey[at+1:], t.RotKey[at:])
	t.RotKey[at] = WhmKey4{Time: now, V: val}
	return 1, 0
}

// unitImportSection is the panel under the keyframe list: the imported
// file's clips, each opening onto the frames the picked bone has keys on,
// and the two buttons that write a multi-selection into the clip on show.
// With nothing imported it is not drawn at all, so the panel looks as it
// did before the feature existed.
func (ui *uiState) unitImportSection(gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.unitImpModel == nil {
		return layout.Dimensions{}
	}
	// Clicks are drained before the buttons are laid out, the order the
	// pose bar uses: an update read after Layout has already been spent.
	copyClicked := ui.unitImpCopyClick.Clicked(gtx)
	replaceClicked := ui.unitImpReplaceClick.Clicked(gtx)
	if copyClicked {
		ui.unitImportApply(v, false)
	}
	if replaceClicked {
		ui.unitImportApply(v, true)
	}
	src := ui.unitImportBoneIndex(v)
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.th, "Imported · "+filepath.Base(ui.unitImpPath))
		}),
	}
	// The buttons sit directly under the header rather than after every clip
	// and frame: a model's imported file lists dozens of clips and hundreds of
	// frames, and at the far end of that the buttons were effectively
	// unreachable. With nothing picked they are drawn disabled rather than
	// hidden, so the row does not jump under the mouse.
	n := len(ui.unitImpSel)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &ui.unitImpCopyClick, "Copy", n == 0)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &ui.unitImpReplaceClick, "Replace", n == 0)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := fmt.Sprintf("%d picked", n)
						if n == 0 {
							txt = "Ctrl+click picks more frames"
						}
						l := material.Label(ui.th, unit.Sp(9), txt)
						l.Color = cTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
	}))
	if src < 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			msg := "Select a bone to see its frames from the imported file."
			if v == nil || v.boneIndex < 0 || v.model == nil || v.boneIndex >= len(v.model.Bones) {
				// The message already says it; keep it short.
			} else {
				msg = "No bone named " + v.model.Bones[v.boneIndex].Name + " in the imported file."
			}
			l := material.Label(ui.th, unit.Sp(9), msg)
			l.Color = cTextDim
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	} else {
		for i := range ui.unitImpModel.Animations {
			i := i
			if i >= len(ui.unitImpOpen) {
				break
			}
			c := &ui.unitImpAnimClicks[i]
			if c.Clicked(gtx) {
				ui.unitImpOpen[i] = !ui.unitImpOpen[i]
			}
			a := &ui.unitImpModel.Animations[i]
			frames := unitImportFrames(a, src)
			note := fmt.Sprintf("%d frame(s)", len(frames))
			if len(frames) == 0 {
				note = "no keys for this bone"
			}
			open := ui.unitImpOpen[i]
			mark := "▸ "
			if open {
				mark = "▾ "
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.unitViewportRow(gtx, c, open, mark+a.Name, note)
			}))
			if !open {
				continue
			}
			if len(frames) == 0 {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(9), "This clip gives "+v.model.Bones[v.boneIndex].Name+" no keys.")
					l.Color = cTextDim
					return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
				}))
				continue
			}
			for _, f := range frames {
				f := f
				ref := unitImpRef{anim: i, frame: f}
				rc := ui.unitImportRowClick(ref)
				if rc.Clicked(gtx) {
					ui.unitImportRowClicked(ref, frames, ui.unitListModifiers())
				}
				selected := ui.unitImpSel[ref]
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.unitViewportRow(gtx, rc, selected,
						fmt.Sprintf("frame %d", f+1), unitImportFrameKinds(a, src, f))
				}))
			}
		}
	}
	if ui.unitBoneKeyErr != "" {
		msg := ui.unitBoneKeyErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), msg)
			l.Color = rgb(0xB00020)
			l.MaxLines = 1
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
