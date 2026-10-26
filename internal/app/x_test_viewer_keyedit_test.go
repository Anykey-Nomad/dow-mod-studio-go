package app

import (
	"encoding/binary"
	"testing"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"gioui.org/layout"
)

func testKeyEditViewer(numFrames int) *modelViewer {
	model := &WhmModel{
		Name:  "t",
		Bones: []WhmBone{{Name: "root", Parent: -1, Pos: [3]float32{1, 2, 3}}},
	}
	return &modelViewer{
		Model: model,
		Anim: &WhmAnimation{
			Name:      "clip",
			NumFrames: numFrames,
			Bones:     []WhmTrack{{Bone: 0, Name: "root"}},
		},
		BoneIndex:    0,
		BoneKeyIndex: -1,
		BoneKeyMode:  -1,
	}
}

// keyEditFrames reads a track's key times back as frames, which is how both
// the strip and the panel rows show them.
func keyEditFrames(times []float32, numFrames int) []int {
	out := make([]int, len(times))
	for i, t := range times {
		out[i] = frameAt(float64(t), numFrames)
	}
	return out
}

func TestTimeForFrameRoundTripsThroughFrameAt(t *testing.T) {
	for _, n := range []int{1, 10, 60, 100, 240} {
		for f := 0; f < n; f++ {
			got := frameAt(float64(timeForFrame(f, n)), n)
			if got != f {
				t.Fatalf("frame %d of %d wrote time %g, which reads back as frame %d",
					f, n, timeForFrame(f, n), got)
			}
		}
	}
}

func TestAddBoneKeyAtFrameWritesThePoseOnShow(t *testing.T) {
	v := testKeyEditViewer(100)
	v.SeekFrame(50)
	if !v.AddBoneKeyAtFrame(50, 0) {
		t.Fatal("adding a position key on an empty clip reported no key was created")
	}
	track := v.BoneTrack()
	if len(track.PosKey) != 1 {
		t.Fatalf("the clip holds %d position keys, want 1", len(track.PosKey))
	}
	k := track.PosKey[0]
	if got := frameAt(float64(k.Time), 100); got != 50 {
		t.Errorf("the key wrote time %g, which reads back as frame %d, want 50", k.Time, got)
	}
	// The bone rests at (1, 2, 3) and the clip keys nothing, so the pose on
	// show is the rest pose and the new key holds it.
	if k.V != [3]float32{1, 2, 3} {
		t.Errorf("the new key holds %v, want the pose on show (1 2 3)", k.V)
	}
	if v.BoneKeyIndex != 0 || v.BoneKeyMode != 0 {
		t.Errorf("selection after the add is index %d mode %d, want the new key (0, 0)",
			v.BoneKeyIndex, v.BoneKeyMode)
	}
}

func TestAddBoneKeyOnAnOccupiedFramePicksTheKeyAlreadyThere(t *testing.T) {
	v := testKeyEditViewer(100)
	if !v.AddBoneKeyAtFrame(40, 0) {
		t.Fatal("the first key was not created")
	}
	if v.AddBoneKeyAtFrame(40, 0) {
		t.Error("a second key was created on a frame that already carries one")
	}
	track := v.BoneTrack()
	if len(track.PosKey) != 1 {
		t.Fatalf("the clip holds %d position keys, want 1", len(track.PosKey))
	}
	if v.BoneKeyIndex != 0 || v.BoneKeyMode != 0 {
		t.Errorf("selection is index %d mode %d, want the key at the frame (0, 0)",
			v.BoneKeyIndex, v.BoneKeyMode)
	}
}

func TestAddBoneKeyKeepsTheListSortedByTime(t *testing.T) {
	v := testKeyEditViewer(100)
	for _, f := range []int{60, 20, 40} {
		if !v.AddBoneKeyAtFrame(f, 0) {
			t.Fatalf("key for frame %d was not created", f)
		}
	}
	track := v.BoneTrack()
	got := keyEditFrames([]float32{track.PosKey[0].Time, track.PosKey[1].Time, track.PosKey[2].Time}, 100)
	want := []int{20, 40, 60}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys sit on frames %v, want %v — the list must stay sorted", got, want)
		}
	}
}

func TestPositionAndRotationKeysLiveSideBySideOnOneFrame(t *testing.T) {
	v := testKeyEditViewer(100)
	if !v.AddBoneKeyAtFrame(30, 0) {
		t.Fatal("the position key was not created")
	}
	if !v.AddBoneKeyAtFrame(30, 1) {
		t.Fatal("a rotation key was refused on a frame that only carries a position key")
	}
	track := v.BoneTrack()
	if len(track.PosKey) != 1 || len(track.RotKey) != 1 {
		t.Fatalf("the clip holds %d position and %d rotation keys, want one of each",
			len(track.PosKey), len(track.RotKey))
	}
	// Adding the position key again now finds it instead of duplicating it.
	if v.AddBoneKeyAtFrame(30, 0) {
		t.Error("a duplicate position key was created beside the rotation key")
	}
	if v.BoneKeyMode != 0 {
		t.Errorf("the re-add picked mode %d, want the position key (0)", v.BoneKeyMode)
	}
}

func TestDeletePickedBoneKeyRemovesOnlyThatKey(t *testing.T) {
	v := testKeyEditViewer(100)
	v.AddBoneKeyAtFrame(20, 0)
	v.AddBoneKeyAtFrame(40, 0)
	v.AddBoneKeyAtFrame(30, 1)
	// Pick the position key on frame 20 — the list's first key.
	v.BoneKeyIndex, v.BoneKeyMode = 0, 0
	if !v.PickedBoneKeyValid() {
		t.Fatal("the picked key reports itself gone before it is deleted")
	}
	if !v.DeletePickedBoneKey() {
		t.Fatal("the picked key was not removed")
	}
	track := v.BoneTrack()
	if len(track.PosKey) != 1 || frameAt(float64(track.PosKey[0].Time), 100) != 40 {
		t.Fatalf("position keys after the delete are %v, want only frame 40",
			keyEditFrames([]float32{track.PosKey[0].Time}, 100))
	}
	if len(track.RotKey) != 1 {
		t.Errorf("the rotation key was removed with the position key: %d remain, want 1", len(track.RotKey))
	}
	if v.BoneKeyIndex != -1 || v.BoneKeyMode != -1 {
		t.Errorf("selection after the delete is index %d mode %d, want nothing picked (-1, -1)",
			v.BoneKeyIndex, v.BoneKeyMode)
	}
	if v.PickedBoneKeyValid() {
		t.Error("a deleted key still reports itself as picked")
	}
	// A delete with nothing picked changes nothing rather than removing a
	// key the reader never chose.
	if v.DeletePickedBoneKey() {
		t.Error("a delete with nothing picked reported it removed something")
	}
}

func TestMovePickedBoneKeyKeepsOrderAndMergesOntoAnOccupiedFrame(t *testing.T) {
	v := testKeyEditViewer(100)
	v.AddBoneKeyAtFrame(20, 0)
	v.AddBoneKeyAtFrame(40, 0)
	v.AddBoneKeyAtFrame(60, 0)
	track := v.BoneTrack()
	track.PosKey[1].V = [3]float32{7, 7, 7} // the key to drag
	track.PosKey[2].V = [3]float32{6, 6, 6} // the key it will be dropped on
	// Pick the frame-40 key (the list is sorted, so it sits at index 1).
	v.BoneKeyIndex, v.BoneKeyMode = 1, 0
	if !v.MovePickedBoneKeyToFrame(70) {
		t.Fatal("the key was not moved")
	}
	if got, want := keyEditFrames([]float32{track.PosKey[0].Time, track.PosKey[1].Time, track.PosKey[2].Time}, 100), []int{20, 60, 70}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("after the move the keys sit on frames %v, want %v — the list must stay sorted", got, want)
	}
	if v.BoneKeyIndex != 2 {
		t.Errorf("the moved key is at index %d after re-sorting, want 2", v.BoneKeyIndex)
	}
	// Moving onto its own frame is not a move.
	if v.MovePickedBoneKeyToFrame(70) {
		t.Error("dropping a key on the frame it already sits on reported a change")
	}
	// Dropped on an occupied frame the dragged key wins: the frame carries
	// one key, and it holds the dragged key's value.
	if !v.MovePickedBoneKeyToFrame(60) {
		t.Fatal("the key was not moved onto the occupied frame")
	}
	if len(track.PosKey) != 2 {
		t.Fatalf("the clip holds %d position keys after the merge, want 2", len(track.PosKey))
	}
	if got, want := keyEditFrames([]float32{track.PosKey[0].Time, track.PosKey[1].Time}, 100), []int{20, 60}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("after the merge the keys sit on frames %v, want %v", got, want)
	}
	if track.PosKey[1].V != [3]float32{7, 7, 7} {
		t.Errorf("the surviving key holds %v, want the dragged key's value (7 7 7)", track.PosKey[1].V)
	}
	if v.BoneKeyIndex != 1 {
		t.Errorf("the dragged key is at index %d after the merge, want 1", v.BoneKeyIndex)
	}
}

func TestMovePickedBoneKeyClampsToTheClipAndRefusesWithoutAKey(t *testing.T) {
	v := testKeyEditViewer(100)
	// Nothing picked: no move, no crash.
	if v.MovePickedBoneKeyToFrame(10) {
		t.Error("a move with nothing picked reported it moved something")
	}
	v.AddBoneKeyAtFrame(50, 1)
	v.BoneKeyIndex, v.BoneKeyMode = 0, 1
	if !v.MovePickedBoneKeyToFrame(1000) {
		t.Fatal("the key was not moved")
	}
	track := v.BoneTrack()
	if got := frameAt(float64(track.RotKey[0].Time), 100); got != 99 {
		t.Errorf("a move past the end landed on frame %d, want the last (99)", got)
	}
}

// A snapshot copies the track so a later edit cannot reach back and change
// what was recorded, and restoring it puts the keys back exactly as they were.
func TestBoneTrackSnapshotAndRestoreRollsAnEditBack(t *testing.T) {
	v := testKeyEditViewer(100)
	v.AddBoneKeyAtFrame(20, 0)
	v.AddBoneKeyAtFrame(40, 0)
	v.AddBoneKeyAtFrame(30, 1)
	snap, ok := v.SnapshotBoneTrack()
	if !ok {
		t.Fatal("the snapshot reported nothing to record on a bone with keys")
	}
	// Mutate the live track: delete a position key and rewrite the rotation
	// key's value. The snapshot must not see either change.
	v.BoneKeyIndex, v.BoneKeyMode = 0, 0
	if !v.DeletePickedBoneKey() {
		t.Fatal("the delete did not happen")
	}
	track := v.BoneTrack()
	track.RotKey[0].V = [4]float32{9, 9, 9, 9}

	if len(snap.posKey) != 2 {
		t.Fatalf("the snapshot holds %d position keys, want 2 — it must be a copy, not the live track", len(snap.posKey))
	}
	if snap.rotKey[0].V == [4]float32{9, 9, 9, 9} {
		t.Error("the snapshot saw the rotation rewrite; it aliases the live track")
	}

	if !v.RestoreBoneTrack(snap) {
		t.Fatal("the restore reported failure on a track that still exists")
	}
	if len(track.PosKey) != 2 {
		t.Errorf("after restore the clip holds %d position keys, want 2", len(track.PosKey))
	}
	if got := keyEditFrames([]float32{track.PosKey[0].Time, track.PosKey[1].Time}, 100); got[0] != 20 || got[1] != 40 {
		t.Errorf("after restore the position keys sit on frames %v, want [20 40]", got)
	}
	if track.RotKey[0].V == [4]float32{9, 9, 9, 9} {
		t.Error("the restore left the rewritten rotation value in place")
	}
	if v.PoseValid {
		t.Error("the restore left the pose cache warm; the viewport would draw the pre-restore pose")
	}
}

// The panel's Undo stack pops the most recent edit first and reports an empty
// stack rather than restoring nothing.
func TestUndoBoneKeyEditPopsTheMostRecentEdit(t *testing.T) {
	v := testKeyEditViewer(100)
	ui := &uiState{}
	// First edit: two position keys, snapshotted before the change as the
	// handler does.
	snap, _ := v.SnapshotBoneTrack()
	v.AddBoneKeyAtFrame(20, 0)
	v.AddBoneKeyAtFrame(40, 0)
	PushBoneUndo(ui, snap, true)
	// Second edit: add a rotation key, snapshot first.
	snap2, _ := v.SnapshotBoneTrack()
	v.AddBoneKeyAtFrame(30, 1)
	PushBoneUndo(ui, snap2, true)

	if len(ui.UnitBoneUndo) != 2 {
		t.Fatalf("the undo stack holds %d steps, want 2", len(ui.UnitBoneUndo))
	}
	// Undo once: the rotation key goes, the two position keys stay.
	if !UndoBoneKeyEdit(ui, v) {
		t.Fatal("undo reported nothing to restore with two steps on the stack")
	}
	track := v.BoneTrack()
	if len(track.RotKey) != 0 {
		t.Errorf("after one undo the clip holds %d rotation keys, want 0", len(track.RotKey))
	}
	if len(track.PosKey) != 2 {
		t.Errorf("after one undo the clip holds %d position keys, want 2 — only the last edit is rolled back", len(track.PosKey))
	}
	// Undo again: the position keys go, back to the empty track.
	if !UndoBoneKeyEdit(ui, v) {
		t.Fatal("undo reported nothing to restore with one step on the stack")
	}
	if len(track.PosKey) != 0 {
		t.Errorf("after two undos the clip holds %d position keys, want 0", len(track.PosKey))
	}
	// A third undo has nothing to restore and says so instead of crashing.
	if UndoBoneKeyEdit(ui, v) {
		t.Error("undo on an empty stack reported it restored something")
	}
}

// The strip only offers the keys it draws: with a bone picked, that bone's
// keys are what a press snaps to and reports, and the clip's own marks — the
// other bones' keys — are neither snapped to nor selected.
func TestScrubSeekOffersThePickedBonesKeysAndOnlyThose(t *testing.T) {
	v := testKeyEditViewer(100)
	v.Model.Bones = append(v.Model.Bones, WhmBone{Name: "child", Parent: 0})
	v.Anim.Bones = append(v.Anim.Bones, WhmTrack{Bone: 1, Name: "child"})
	v.SetRenderSize(800, 600)
	left, width, ok := v.ScrubGeometry()
	if !ok {
		t.Fatal("the strip has no geometry at this render size")
	}
	// The picked bone keys frame 50; the other one keys frame 10, which the
	// strip does not draw while a bone is picked.
	v.Anim.Bones[0].PosKey = []WhmKey3{{Time: timeForFrame(50, 100), V: [3]float32{1}}}
	v.Anim.Bones[1].PosKey = []WhmKey3{{Time: timeForFrame(10, 100), V: [3]float32{2}}}

	otherMarkX := left + frameX(10, 100, width) + 5
	if picked := v.ScrubSeek(otherMarkX, true); picked {
		t.Error("a press beside the other bone's mark reported the picked bone's key")
	}
	if v.BoneKeyIndex != -1 {
		t.Errorf("the other bone's key was selected (index %d); the strip does not draw it", v.BoneKeyIndex)
	}
	if f, _ := v.CurrentFrame(); f != 11 {
		t.Errorf("without a drawn mark nearby the press landed on frame %d, want the frame under it (11)", f)
	}

	ownMarkX := left + frameX(50, 100, width) + 5
	if picked := v.ScrubSeek(ownMarkX, true); !picked {
		t.Error("a press beside the picked bone's own mark did not report its key")
	}
	if v.BoneKeyIndex != 0 || v.BoneKeyMode != 0 {
		t.Errorf("selection is index %d mode %d, want the picked bone's key (0, 0)",
			v.BoneKeyIndex, v.BoneKeyMode)
	}
	if f, _ := v.CurrentFrame(); f != 50 {
		t.Errorf("the playhead wound to frame %d, want the key's frame 50", f)
	}

	// With no bone picked the clip's own keyframes are what the strip marks,
	// and they snap again.
	v.BoneIndex = -1
	v.BoneKeyIndex, v.BoneKeyMode = -1, -1
	if picked := v.ScrubSeek(otherMarkX, true); picked {
		t.Error("a clip-level snap reported a bone key")
	}
	if f, _ := v.CurrentFrame(); f != 10 {
		t.Errorf("the press beside the clip's mark wound to frame %d, want 10", f)
	}
}

// ─── merged from viewer_material_diagnostics_test.go ────────────────────────────────────────

func TestMaterialTreePathIncludesArchiveAndVirtualPath(t *testing.T) {
	file := &modtree.TreeFile{
		FullPath: `Data\textures\slag_deposit.rtx`,
		Source: &modtree.FileSource{
			IsSGA:       true,
			ArchivePath: "/game/W40kData-SharedTextures.sga",
		},
	}
	if got, want := materialTreePath(file), "/game/W40kData-SharedTextures.sga :: Data\\textures\\slag_deposit.rtx"; got != want {
		t.Fatalf("materialTreePath = %q, want %q", got, want)
	}
}

func TestDecodeModelMaterialsReportsRTXFormat(t *testing.T) {
	got := decodeModelMaterials(map[string][]byte{"slag_deposit": diagnosticTestRTX(t, 8)}, true)
	diag := got.diagnostics["slag_deposit"]
	if diag.Format != "RTX / DXT1 (BC1)" || diag.Status != "decoded" {
		t.Fatalf("format/status = %q/%q, want RTX / DXT1 (BC1)/decoded", diag.Format, diag.Status)
	}
	if got.materials["slag_deposit"].Texture == nil {
		t.Fatal("diagnostic decode did not produce a usable material image")
	}
}

func TestModelMaterialDiagnosticsTableHandlesMissingViewer(t *testing.T) {
	ui := &uiState{}
	for _, tab := range []*fileTab{nil, {}} {
		if got := ModelMaterialDiagnosticsTable(ui, layout.Context{}, tab); got != (layout.Dimensions{}) {
			t.Errorf("material diagnostics for tab %v = %+v, want empty dimensions", tab, got)
		}
	}
}

func diagnosticTestRTX(t *testing.T, format uint32) []byte {
	t.Helper()
	attr := make([]byte, 16)
	binary.LittleEndian.PutUint32(attr[0:4], format)
	binary.LittleEndian.PutUint32(attr[4:8], 4)
	binary.LittleEndian.PutUint32(attr[8:12], 4)
	binary.LittleEndian.PutUint32(attr[12:16], 1)
	pixels := []byte{0, 0xf8, 0, 0, 0, 0, 0, 0}
	root := &chunky.Chunk{TypeID: "FOLDRTXTR", Children: []*chunky.Chunk{
		{TypeID: "FOLDIMAG", Children: []*chunky.Chunk{
			{TypeID: "DATAATTR", Payload: attr},
			{TypeID: "DATADATA", Payload: pixels},
		}},
	}}
	return append(chunky.Preamble(), chunky.Serialize(root)...)
}
