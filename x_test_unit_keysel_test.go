package main

import (
	"testing"

	"gioui.org/io/key"
)

// keySelViewer is a one-bone clip that already carries the keys the selection
// tests need: position keys on frames 2 and 5, rotation keys on frames 3 and
// 7, out of 20. Two kinds, four rows, and frames far enough apart that
// "before" and "after" land somewhere the test can name.
func keySelViewer() *modelViewer {
	const frames = 20
	model := &WhmModel{
		Name:  "t",
		Bones: []WhmBone{{Name: "root", Parent: -1, Pos: [3]float32{1, 2, 3}}},
	}
	return &modelViewer{
		model: model,
		anim: &WhmAnimation{
			Name:      "clip",
			NumFrames: frames,
			Bones: []WhmTrack{{
				Bone: 0,
				Name: "root",
				PosKey: []WhmKey3{
					{Time: timeForFrame(2, frames), V: [3]float32{1, 2, 3}},
					{Time: timeForFrame(5, frames), V: [3]float32{4, 5, 6}},
				},
				RotKey: []WhmKey4{
					{Time: timeForFrame(3, frames), V: [4]float32{0, 0, 0, 1}},
					{Time: timeForFrame(7, frames), V: [4]float32{1, 0, 0, 0}},
				},
			}},
		},
		boneIndex:    0,
		boneKeyIndex: -1,
		boneKeyMode:  -1,
	}
}

// keySelUI is a fresh window with that viewer on show and its keyframe
// selection bound to the bone it belongs to, which is where every row click
// starts from.
func keySelUI(v *modelViewer) *uiState {
	ui := newUI(unitTestTheme())
	ui.bindBoneKeySelection(v)
	return ui
}

// keySelPosFrames and keySelRotFrames read the track back as frames, which is
// what both the list and the strip call the place a key stands.
func keySelPosFrames(v *modelViewer) []int {
	out := make([]int, 0, len(v.boneTrack().PosKey))
	for _, k := range v.boneTrack().PosKey {
		out = append(out, frameAt(float64(k.Time), v.anim.NumFrames))
	}
	return out
}

func keySelRotFrames(v *modelViewer) []int {
	out := make([]int, 0, len(v.boneTrack().RotKey))
	for _, k := range v.boneTrack().RotKey {
		out = append(out, frameAt(float64(k.Time), v.anim.NumFrames))
	}
	return out
}

func keySelFramesEqual(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestKeyFrameRowsAreListedPositionKeysThenRotationKeys(t *testing.T) {
	v := keySelViewer()
	want := []boneKeyRef{{mode: 0, frame: 2}, {mode: 0, frame: 5}, {mode: 1, frame: 3}, {mode: 1, frame: 7}}
	got := v.boneKeyRefs()
	if len(got) != len(want) {
		t.Fatalf("the list has %d rows, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestInsertBoneKeyCopyTakesTheValueOfTheKeyItCopies(t *testing.T) {
	v := keySelViewer()
	if !v.insertBoneKeyCopy(4, 0, 2) {
		t.Fatal("frame 4 carries no key, so a copy of frame 2 was refused")
	}
	if got, want := keySelPosFrames(v), []int{2, 4, 5}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v — the track has to stay sorted", got, want)
	}
	if got := v.boneTrack().PosKey[1].V; got != [3]float32{1, 2, 3} {
		t.Errorf("the new key holds %v, want the copied [1 2 3]", got)
	}
	if ref, ok := v.pickedBoneKeyRef(); !ok || ref != (boneKeyRef{mode: 0, frame: 4}) {
		t.Errorf("the picked key is %+v (ok=%v), want the new one on frame 4", ref, ok)
	}
}

func TestInsertBoneKeyCopyRefusesAFrameThatAlreadyHasAKey(t *testing.T) {
	v := keySelViewer()
	if v.insertBoneKeyCopy(2, 0, 5) {
		t.Error("a second position key landed on frame 2, which already holds one")
	}
	if got := keySelPosFrames(v); len(got) != 2 {
		t.Errorf("the track now holds %v, want the two keys it started with", got)
	}
}

func TestFreeBoneKeyFrameWalksOutToAFreeFrame(t *testing.T) {
	v := keySelViewer()
	if got := v.freeBoneKeyFrame(5, 0, -1); got != 4 {
		t.Errorf("the frame before position key 5 is %d, want 4", got)
	}
	// A run of taken frames is walked past rather than refused: insert a
	// position key on 4 and the free frame before 5 becomes 3.
	if !v.insertBoneKeyCopy(4, 0, 2) {
		t.Fatal("the key on frame 4 could not be inserted for the walk")
	}
	if got := v.freeBoneKeyFrame(5, 0, -1); got != 3 {
		t.Errorf("with frame 4 taken the free frame before 5 is %d, want 3", got)
	}
	// The other kind has its own keys: frame 4 is free for a rotation.
	if got := v.freeBoneKeyFrame(5, 1, -1); got != 4 {
		t.Errorf("rotation keys do not stand on frame 4, so the free frame is %d, want 4", got)
	}
	if got := v.freeBoneKeyFrame(0, 0, -1); got != -1 {
		t.Errorf("there is no frame before 0, got %d", got)
	}
	if got := v.freeBoneKeyFrame(v.anim.NumFrames-1, 0, 1); got != -1 {
		t.Errorf("there is no frame after the last one, got %d", got)
	}
}

func TestDeleteBoneKeyAtKeepsThePickedKeyAimedAtItself(t *testing.T) {
	v := keySelViewer()
	// The position key on frame 5 is picked: it is the second of its kind,
	// so taking the key on frame 2 out slides its index down.
	v.boneKeyIndex, v.boneKeyMode = 1, 0
	if !v.deleteBoneKeyAt(2, 0) {
		t.Fatal("the key on frame 2 was not removed")
	}
	if got, want := keySelPosFrames(v), []int{5}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v", got, want)
	}
	if v.boneKeyIndex != 0 || v.boneKeyMode != 0 {
		t.Fatalf("the picked key is now (idx %d, mode %d), want (0, 0)", v.boneKeyIndex, v.boneKeyMode)
	}
	if ref, ok := v.pickedBoneKeyRef(); !ok || ref != (boneKeyRef{mode: 0, frame: 5}) {
		t.Errorf("the picked key is %+v (ok=%v), want the one still standing on frame 5", ref, ok)
	}
}

func TestDeleteBoneKeyAtClearsThePickedKeyWhenItIsTheOne(t *testing.T) {
	v := keySelViewer()
	v.boneKeyIndex, v.boneKeyMode = 0, 0
	if !v.deleteBoneKeyAt(2, 0) {
		t.Fatal("the picked key was not removed")
	}
	if v.boneKeyIndex != -1 || v.boneKeyMode != -1 {
		t.Errorf("the picked key is now (idx %d, mode %d), want nothing picked", v.boneKeyIndex, v.boneKeyMode)
	}
	if got, want := keySelPosFrames(v), []int{5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v", got, want)
	}
}

func TestRowClickPicksOneRowAndCtrlClickGathersMore(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[1], refs, 0)
	if len(ui.unitBoneKeySel) != 1 || !ui.unitBoneKeySel[refs[1]] {
		t.Fatalf("a plain click picked %v, want just row %+v", ui.unitBoneKeySel, refs[1])
	}
	if v.boneKeyIndex != 1 || v.boneKeyMode != 0 {
		t.Errorf("the picked key is (idx %d, mode %d), want the clicked row (1, 0)", v.boneKeyIndex, v.boneKeyMode)
	}

	ui.boneKeyRowClicked(v, refs[0], refs, key.ModCtrl)
	if len(ui.unitBoneKeySel) != 2 || !ui.unitBoneKeySel[refs[0]] {
		t.Fatalf("Ctrl+click gathered %v, want both rows", ui.unitBoneKeySel)
	}

	// Ctrl+clicking a picked row takes it away, and the key the fields work
	// on moves to the row still picked rather than one that no longer says
	// so on the list.
	ui.boneKeyRowClicked(v, refs[1], refs, key.ModCtrl)
	if len(ui.unitBoneKeySel) != 1 || !ui.unitBoneKeySel[refs[0]] {
		t.Fatalf("the second Ctrl+click left %v, want the first row alone", ui.unitBoneKeySel)
	}
	if v.boneKeyIndex != 0 || v.boneKeyMode != 0 {
		t.Errorf("the picked key is (idx %d, mode %d), want the row still picked (0, 0)", v.boneKeyIndex, v.boneKeyMode)
	}

	// And taking the last one away leaves nothing picked at all.
	ui.boneKeyRowClicked(v, refs[0], refs, key.ModCtrl)
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the selection still holds %v after its last row was taken away", ui.unitBoneKeySel)
	}
	if v.boneKeyIndex != -1 || v.boneKeyMode != -1 {
		t.Errorf("the picked key is (idx %d, mode %d), want nothing picked", v.boneKeyIndex, v.boneKeyMode)
	}
	if got := ui.boneKeyActionRefs(v); len(got) != 0 {
		t.Errorf("the buttons would act on %v while the list has nothing picked", got)
	}
}

func TestShiftClickSpansFromTheAnchor(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[1], refs, 0)            // the anchor: position key on frame 5
	ui.boneKeyRowClicked(v, refs[3], refs, key.ModShift) // rotation key on frame 7
	want := map[boneKeyRef]bool{refs[1]: true, refs[2]: true, refs[3]: true}
	if len(ui.unitBoneKeySel) != len(want) {
		t.Fatalf("Shift+click picked %v, want the %d rows between the anchor and the click", ui.unitBoneKeySel, len(want))
	}
	for ref := range want {
		if !ui.unitBoneKeySel[ref] {
			t.Errorf("row %+v is missing from %v", ref, ui.unitBoneKeySel)
		}
	}
	if ui.unitBoneKeySel[refs[0]] {
		t.Errorf("row %+v lies before the anchor and was picked all the same: %v", refs[0], ui.unitBoneKeySel)
	}
}

func TestDeleteTakesEveryPickedRowAway(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)           // position key on frame 2
	ui.boneKeyRowClicked(v, refs[3], refs, key.ModCtrl) // rotation key on frame 7
	ui.deleteBoneKeysAction(v)

	if got, want := keySelPosFrames(v), []int{5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v", got, want)
	}
	if got, want := keySelRotFrames(v), []int{3}; !keySelFramesEqual(got, want) {
		t.Errorf("the rotation keys sit on frames %v, want %v", got, want)
	}
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the selection kept %v after the keys it named were removed", ui.unitBoneKeySel)
	}
	if !ui.unitModelWhmDirty {
		t.Error("removing keys did not mark the file for Save WHM")
	}
	if ui.unitBoneKeyErr != "" {
		t.Errorf("the note after a successful delete says %q", ui.unitBoneKeyErr)
	}
}

func TestKeyButtonsFallBackToTheStripPickedKey(t *testing.T) {
	v := keySelViewer()
	ui := keySelUI(v)
	// No row has been clicked: the strip picked the position key on frame 5.
	v.boneKeyIndex, v.boneKeyMode = 1, 0

	got := ui.boneKeyActionRefs(v)
	if len(got) != 1 || got[0] != (boneKeyRef{mode: 0, frame: 5}) {
		t.Fatalf("the buttons would act on %v, want the single key the strip picked", got)
	}
	ui.deleteBoneKeysAction(v)
	if frames := keySelPosFrames(v); !keySelFramesEqual(frames, []int{2}) {
		t.Errorf("the position keys sit on frames %v, want [2]", frames)
	}
}

func TestPickedRowsLeaveTheSelectionWhenTheirKeysGo(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)
	// The key is carried off its frame some other way — a drag on the strip
	// dropped it onto another — and the row must not go on naming that frame.
	if !v.deleteBoneKeyAt(2, 0) {
		t.Fatal("the key on frame 2 was not removed")
	}
	if got := ui.pickedBoneKeyRefs(v); len(got) != 0 {
		t.Errorf("the selection still holds %v for keys that are gone", got)
	}
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the selection map kept %v", ui.unitBoneKeySel)
	}
}

func TestAnotherBoneStartsAFreshSelection(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)
	ui.boneKeyRowClicked(v, refs[0], refs, 0)

	v.model.Bones = append(v.model.Bones, WhmBone{Name: "child", Parent: 0})
	v.boneIndex = 1
	ui.bindBoneKeySelection(v)
	if len(ui.unitBoneKeySel) != 0 {
		t.Errorf("the rows picked on the last bone were carried over: %v", ui.unitBoneKeySel)
	}
	if ui.unitBoneKeySelAnchor != (boneKeyRef{mode: -1}) {
		t.Errorf("the anchor still stands on %+v after the bone changed", ui.unitBoneKeySelAnchor)
	}
}

func TestAddBeforeGivesEveryPickedRowItsOwnFrame(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)           // position key on frame 2
	ui.boneKeyRowClicked(v, refs[1], refs, key.ModCtrl) // position key on frame 5
	ui.addBoneKeysAround(v, -1)

	if got, want := keySelPosFrames(v), []int{1, 2, 4, 5}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v — each picked row gets the free frame before it", got, want)
	}
	if !ui.unitModelWhmDirty {
		t.Error("adding keys did not mark the file for Save WHM")
	}
	// The keys just added are what the list shows picked, since editing or
	// taking them away is what comes next.
	want := map[boneKeyRef]bool{{mode: 0, frame: 1}: true, {mode: 0, frame: 4}: true}
	if len(ui.unitBoneKeySel) != len(want) {
		t.Fatalf("the selection holds %v, want the two keys just added", ui.unitBoneKeySel)
	}
	for ref := range want {
		if !ui.unitBoneKeySel[ref] {
			t.Errorf("row %+v is missing from %v", ref, ui.unitBoneKeySel)
		}
	}
}

func TestAddAfterGivesEveryPickedRowItsOwnFrame(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)

	ui.boneKeyRowClicked(v, refs[0], refs, 0)
	ui.boneKeyRowClicked(v, refs[1], refs, key.ModCtrl)
	ui.addBoneKeysAround(v, 1)

	if got, want := keySelPosFrames(v), []int{2, 3, 5, 6}; !keySelFramesEqual(got, want) {
		t.Fatalf("the position keys sit on frames %v, want %v — each picked row gets the free frame after it", got, want)
	}
}

func TestAddBeforeWalksFurtherWhenTheNeighbourIsTaken(t *testing.T) {
	v := keySelViewer()
	refs := v.boneKeyRefs()
	ui := keySelUI(v)
	// A position key on frame 3 makes frames 2 and 3 neighbours of the run.
	if !v.insertBoneKeyCopy(3, 0, 2) {
		t.Fatal("the key on frame 3 could not be inserted")
	}
	refs = v.boneKeyRefs()
	row := -1
	for i, r := range refs {
		if r == (boneKeyRef{mode: 0, frame: 3}) {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("the list does not show the new key: %v", refs)
	}
	ui.boneKeyRowClicked(v, refs[row], refs, 0)
	ui.addBoneKeysAround(v, -1)

	if got, want := keySelPosFrames(v), []int{1, 2, 3, 5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v — frame 2 was taken, so the key went to 1", got, want)
	}
}

func TestAddAroundSaysSoWhenNothingCanBeAdded(t *testing.T) {
	v := keySelViewer()
	ui := keySelUI(v)
	// Frames 0 and 1 are filled in front of the position key on frame 2, so
	// the walk backwards runs out of clip before it finds a free frame.
	for _, frame := range []int{1, 0} {
		if !v.insertBoneKeyCopy(frame, 0, 2) {
			t.Fatalf("the key on frame %d could not be inserted", frame)
		}
	}
	refs := v.boneKeyRefs()
	for _, r := range refs {
		if r == (boneKeyRef{mode: 0, frame: 2}) {
			ui.boneKeyRowClicked(v, r, refs, 0)
		}
	}
	ui.addBoneKeysAround(v, -1)

	if ui.unitBoneKeyErr == "" {
		t.Error("adding before a key with no free frame said nothing")
	}
	if ui.unitModelWhmDirty {
		t.Error("a refusal still marked the file for Save WHM")
	}
	if got, want := keySelPosFrames(v), []int{0, 1, 2, 5}; !keySelFramesEqual(got, want) {
		t.Errorf("the position keys sit on frames %v, want %v — nothing may be added", got, want)
	}
}
