package app

import (
	"dow-mod-studio/internal/app/uikit"
	"fmt"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// openUnitImportDialog shows the file dialog over the module's .whm files; a
// path typed in full or a file from the list is read when the dialog is
// confirmed.
func OpenUnitImportDialog(ui *uiState) {
	OpenFilePicker(ui, pickConfig{
		Title:    "Import frame's timeline",
		Subtitle: "Choose the .whm to read keyframes from — a path, or a file from the module",
		Hint:     `Data\art\ebps\...\model.whm`,
		Exts:     []string{".whm"},
		ModTree:  true,
		OnPick: func(path string) (bool, string) {
			before := ui.UnitImpModel
			UnitImportRead(ui, path)
			if ui.UnitImpModel == before {
				return false, ui.UnitImpStatus
			}
			return true, ""
		},
	})
}

// unitImportRead loads the picked .whm from disk or from the module and
// keeps its parsed model for the panel to browse. Nothing of the source is
// written back: it is a clipboard, read once per dialog confirmation.
func UnitImportRead(ui *uiState, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		ui.UnitImpStatus = "No file was chosen"
		return
	}
	var data []byte
	var err error
	if filepath.IsAbs(path) {
		data, err = os.ReadFile(path)
	} else if ui.ModTree != nil {
		data, err = ui.ModTree.ReadFile(path)
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
		ui.UnitImpStatus = "Read failed: " + err.Error()
		LogInfo("bone: import: read %q failed: %v", path, err)
		return
	}
	m, perr := ParseWhmModel(data)
	if perr != nil {
		ui.UnitImpStatus = "Not a .whm: " + perr.Error()
		LogInfo("bone: import: parse %q failed: %v", path, perr)
		return
	}
	ui.UnitImpModel = m
	ui.UnitImpPath = path
	ui.UnitImpOpen = make([]bool, len(m.Animations))
	ui.UnitImpAnimClicks = make([]widget.Clickable, len(m.Animations))
	ui.UnitImpFrameClicks = make(map[unitImpRef]*widget.Clickable)
	ui.UnitImpSel = make(map[unitImpRef]bool)
	ui.UnitImpAnchor = unitImpRef{Anim: -1}
	ui.UnitImpStatus = fmt.Sprintf("Imported %s — %d clip(s)", filepath.Base(path), len(m.Animations))
	LogInfo("bone: import: loaded %q (%d animations, %d bones)", path, len(m.Animations), len(m.Bones))
}

// unitImportRowClicked is a press on a frame row: a plain click picks that
// frame alone, Ctrl adds or takes it away, Shift picks every frame of the
// same clip between the anchor and this one. Shift only spans one clip —
// a range across two clips has no timeline to be a range on.
func UnitImportRowClicked(ui *uiState, ref unitImpRef, frames []int, mods key.Modifiers) {
	if ui.UnitImpSel == nil {
		ui.UnitImpSel = make(map[unitImpRef]bool)
	}
	ctrl, shift := mods.Contain(key.ModCtrl), mods.Contain(key.ModShift)
	switch {
	case shift && ui.UnitImpAnchor.Anim == ref.Anim && ui.UnitImpAnchor.Anim >= 0:
		lo, hi := -1, -1
		for i, f := range frames {
			if f == ui.UnitImpAnchor.Frame {
				lo = i
			}
			if f == ref.Frame {
				hi = i
			}
		}
		if lo < 0 || hi < 0 {
			ui.UnitImpSel = map[unitImpRef]bool{ref: true}
			ui.UnitImpAnchor = ref
			break
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		ui.UnitImpSel = make(map[unitImpRef]bool, hi-lo+1)
		for _, f := range frames[lo : hi+1] {
			ui.UnitImpSel[unitImpRef{Anim: ref.Anim, Frame: f}] = true
		}
		LogInfo("bone: import: Shift+click spans %d frame(s) of clip %d", hi-lo+1, ref.Anim)
	case ctrl:
		if ui.UnitImpSel[ref] {
			delete(ui.UnitImpSel, ref)
		} else {
			ui.UnitImpSel[ref] = true
			ui.UnitImpAnchor = ref
		}
	default:
		ui.UnitImpSel = map[unitImpRef]bool{ref: true}
		ui.UnitImpAnchor = ref
	}
}

// unitImportApply writes the selected source frames into the picked bone's
// track of the clip on show. With replace the key on the frame is
// overwritten and a missing one is created; without it only the missing
// ones are created, so a first pass fills gaps and a second overwrites.
// Frame numbers are mapped by frame number, clamped to the clip's length —
// the two files are assumed to carry clips of the same length, which is
// what "analogous set of bones" means in practice.
func UnitImportApply(ui *uiState, v *modelViewer, replace bool) (added, replaced, skipped int) {
	what := "copy"
	if replace {
		what = "replace"
	}
	if ui.UnitImpModel == nil {
		ui.UnitBoneKeyErr = "no file imported"
		return 0, 0, 0
	}
	if v == nil || v.Anim == nil {
		ui.UnitBoneKeyErr = "no clip is on show"
		return 0, 0, 0
	}
	if v.BoneIndex < 0 || v.BoneIndex >= len(v.Model.Bones) {
		ui.UnitBoneKeyErr = "no bone is picked"
		return 0, 0, 0
	}
	src := UnitImportBoneIndex(ui, v)
	if src < 0 {
		ui.UnitBoneKeyErr = "the imported file has no bone named " + v.Model.Bones[v.BoneIndex].Name
		return 0, 0, 0
	}
	// The refs are walked in a fixed order — clip then frame — so the
	// result does not depend on map iteration.
	refs := make([]unitImpRef, 0, len(ui.UnitImpSel))
	for ref := range ui.UnitImpSel {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Anim != refs[j].Anim {
			return refs[i].Anim < refs[j].Anim
		}
		return refs[i].Frame < refs[j].Frame
	})
	if len(refs) == 0 {
		ui.UnitBoneKeyErr = "no frame is picked"
		return 0, 0, 0
	}
	t := BoneTrack(v)
	if t == nil {
		ui.UnitBoneKeyErr = "the bone has no track in this clip"
		return 0, 0, 0
	}
	// The keys written below are also what the copy history keeps: a Copy or
	// Replace records the source keys it took, so they can be found again.
	var keys boneKeyClip
	for _, ref := range refs {
		if ref.Anim < 0 || ref.Anim >= len(ui.UnitImpModel.Animations) {
			skipped++
			continue
		}
		sa := &ui.UnitImpModel.Animations[ref.Anim]
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
		Frame := ref.Frame
		if Frame < 0 {
			skipped++
			continue
		}
		if Frame >= v.Anim.NumFrames {
			// The source clip runs longer than the one on show:
			// the frame has nowhere to land.
			skipped++
			continue
		}
		// Position keys the source frame carries.
		for i := range st.PosKey {
			if frameAt(float64(st.PosKey[i].Time), sa.NumFrames) != Frame {
				continue
			}
			keys.Pos = append(keys.Pos, st.PosKey[i])
			a, r := writeImportPos(t, v, Frame, st.PosKey[i].V, replace)
			added += a
			replaced += r
		}
		// Rotation keys, the same way.
		for i := range st.RotKey {
			if frameAt(float64(st.RotKey[i].Time), sa.NumFrames) != Frame {
				continue
			}
			keys.Rot = append(keys.Rot, st.RotKey[i])
			a, r := writeImportRot(t, v, Frame, st.RotKey[i].V, replace)
			added += a
			replaced += r
		}
	}
	v.PoseValid = false
	Invalidate(v)
	if added+replaced > 0 {
		ui.UnitModelWhmDirty = true
		ui.UnitBoneKeyErr = ""
		ui.UnitClips.Push(clipEntry{
			Kind:  clipImportKeys,
			Label: fmt.Sprintf("%d key(s) from %s", len(keys.Pos)+len(keys.Rot), filepath.Base(ui.UnitImpPath)),
			At:    time.Now(),
			Keys:  &keys,
		})
		FillBoneKeyFields(ui, v, 0)
		FillBoneKeyFields(ui, v, 1)
	} else if ui.UnitBoneKeyErr == "" {
		ui.UnitBoneKeyErr = "nothing to write"
	}
	LogInfo("bone: import %s: %d key(s) added, %d replaced, %d skipped (bone=%q clip=%q)",
		what, added, replaced, skipped, t.Name, v.Anim.Name)
	return added, replaced, skipped
}

// unitImportSection is the panel under the keyframe list: the imported
// file's clips, each opening onto the frames the picked bone has keys on,
// and the two buttons that write a multi-selection into the clip on show.
// With nothing imported it is not drawn at all, so the panel looks as it
// did before the feature existed.
func UnitImportSection(ui *uiState, gtx layout.Context, v *modelViewer) layout.Dimensions {
	if ui.UnitImpModel == nil {
		return layout.Dimensions{}
	}
	// Clicks are drained before the buttons are laid out, the order the
	// pose bar uses: an update read after Layout has already been spent.
	copyClicked := ui.UnitImpCopyClick.Clicked(gtx)
	replaceClicked := ui.UnitImpReplaceClick.Clicked(gtx)
	if copyClicked {
		UnitImportApply(ui, v, false)
	}
	if replaceClicked {
		UnitImportApply(ui, v, true)
	}
	src := UnitImportBoneIndex(ui, v)
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionHeader(gtx, ui.Th, "Imported · "+filepath.Base(ui.UnitImpPath))
		}),
	}
	// The buttons sit directly under the header rather than after every clip
	// and frame: a model's imported file lists dozens of clips and hundreds of
	// frames, and at the far end of that the buttons were effectively
	// unreachable. With nothing picked they are drawn disabled rather than
	// hidden, so the row does not jump under the mouse.
	n := len(ui.UnitImpSel)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitImpCopyClick, "Copy", n == 0)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &ui.UnitImpReplaceClick, "Replace", n == 0)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := fmt.Sprintf("%d picked", n)
						if n == 0 {
							txt = "Ctrl+click picks more frames"
						}
						l := material.Label(ui.Th, unit.Sp(9), txt)
						l.Color = uikit.CTextDim
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
				)
			})
	}))
	if src < 0 {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			msg := "Select a bone to see its frames from the imported file."
			if v == nil || v.BoneIndex < 0 || v.Model == nil || v.BoneIndex >= len(v.Model.Bones) {
				// The message already says it; keep it short.
			} else {
				msg = "No bone named " + v.Model.Bones[v.BoneIndex].Name + " in the imported file."
			}
			l := material.Label(ui.Th, unit.Sp(9), msg)
			l.Color = uikit.CTextDim
			return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	} else {
		for i := range ui.UnitImpModel.Animations {
			i := i
			if i >= len(ui.UnitImpOpen) {
				break
			}
			c := &ui.UnitImpAnimClicks[i]
			if c.Clicked(gtx) {
				ui.UnitImpOpen[i] = !ui.UnitImpOpen[i]
			}
			a := &ui.UnitImpModel.Animations[i]
			frames := unitImportFrames(a, src)
			note := fmt.Sprintf("%d frame(s)", len(frames))
			if len(frames) == 0 {
				note = "no keys for this bone"
			}
			open := ui.UnitImpOpen[i]
			mark := "▸ "
			if open {
				mark = "▾ "
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return UnitViewportRow(ui, gtx, c, open, mark+a.Name, note)
			}))
			if !open {
				continue
			}
			if len(frames) == 0 {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), "This clip gives "+v.Model.Bones[v.BoneIndex].Name+" no keys.")
					l.Color = uikit.CTextDim
					return layout.Inset{Left: unit.Dp(16), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
				}))
				continue
			}
			for _, f := range frames {
				f := f
				ref := unitImpRef{Anim: i, Frame: f}
				rc := UnitImportRowClick(ui, ref)
				if rc.Clicked(gtx) {
					UnitImportRowClicked(ui, ref, frames, UnitListModifiers(ui))
				}
				selected := ui.UnitImpSel[ref]
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return UnitViewportRow(ui, gtx, rc, selected,
						fmt.Sprintf("frame %d", f+1), unitImportFrameKinds(a, src, f))
				}))
			}
		}
	}
	if ui.UnitBoneKeyErr != "" {
		msg := ui.UnitBoneKeyErr
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(9), msg)
			l.Color = rgb(0xB00020)
			l.MaxLines = 1
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
