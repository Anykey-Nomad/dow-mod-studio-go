package app

import (
	"dow-mod-studio/internal/whm"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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
		if DeleteBoneKeyAt(v, ref.Frame, ref.Mode) {
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
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
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
			return refs[i].Frame > refs[j].Frame
		}
		return refs[i].Frame < refs[j].Frame
	})
	added := make([]boneKeyRef, 0, len(refs))
	blocked := 0
	for _, ref := range refs {
		f := FreeBoneKeyFrame(v, ref.Frame, ref.Mode, dir)
		if f < 0 || !InsertBoneKeyCopy(v, f, ref.Mode, ref.Frame) {
			blocked++
			continue
		}
		added = append(added, boneKeyRef{Mode: ref.Mode, Frame: f})
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
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
	if len(newRefs) > 0 {
		ui.UnitBoneKeySelAnchor = newRefs[0]
	}
	// The picked key follows its row to the frame it landed on, so the
	// strip marks and the typed fields point at it again after the move.
	for i, r := range refs {
		if hasPicked && r == pickedOld && i < len(newRefs) {
			if at := BoneKeyIndexAtFrame(v, newRefs[i].Frame, newRefs[i].Mode); at >= 0 {
				v.BoneKeyIndex, v.BoneKeyMode = at, newRefs[i].Mode
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
	ui.UnitBoneKeySelAnchor = boneKeyRef{Mode: -1}
	ui.UnitModelWhmDirty = true
	ui.UnitBoneKeyErr = ""
	FillBoneKeyFields(ui, v, 0)
	FillBoneKeyFields(ui, v, 1)
	ui.StatusText = "Restored the keys before the last edit"
	LogInfo("bone: undo restored track of bone %d: %d pos, %d rot keys (dirty=%v)",
		s.BoneIndex, len(s.PosKey), len(s.RotKey), ui.UnitModelWhmDirty)
	return true
}
