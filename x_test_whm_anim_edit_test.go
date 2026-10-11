package main

import (
	"bytes"
	"dow-mod-studio/internal/whm"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The save path rebuilds one animation's bone section and moves the chunk size
// fields above it. What it must never do is disturb a byte it does not
// rewrite: a save of untouched keys is byte for byte a no-op, and a save of
// edited ones changes the keys and their chunk sizes and nothing else.

func TestPatchWhmAnimBoneKeysLeavesUntouchedKeysByteForByteTheSame(t *testing.T) {
	file := buildTestWhm()
	model, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	if len(model.Animations) != 1 {
		t.Fatalf("the test model has %d animations, want 1", len(model.Animations))
	}
	patched, err := whm.PatchAnimBoneKeys(file, &model.Model, 0)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if bytes.Equal(file, patched) {
		return
	}
	for i := 0; i < len(file) && i < len(patched); i++ {
		if file[i] != patched[i] {
			t.Fatalf("a save of untouched keys changed byte %d of %d (output is %d bytes)",
				i, len(file), len(patched))
		}
	}
	t.Fatalf("a save of untouched keys changed the file length: %d -> %d", len(file), len(patched))
}

func TestPatchWhmAnimBoneKeysWritesAddMoveAndRemove(t *testing.T) {
	file := buildTestWhm()
	model, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	anim := &model.Animations[0]
	track := &anim.Bones[0]
	if len(track.PosKey) != 2 || len(track.RotKey) != 2 {
		t.Fatalf("the fixture changed shape: %d position and %d rotation keys, want two of each",
			len(track.PosKey), len(track.RotKey))
	}
	// Move: the last position key goes to frame 10 (still after frame 0,
	// so the list stays sorted the way a drag along the strip keeps it).
	track.PosKey[1].Time = timeForFrame(10, anim.NumFrames)
	// Remove: the first rotation key leaves.
	track.RotKey = track.RotKey[1:]
	// Add: a position key on frame 15, appended behind the moved one.
	added := WhmKey3{Time: timeForFrame(15, anim.NumFrames), V: [3]float32{0.25, -0.5, 0.75}}
	track.PosKey = append(track.PosKey, added)

	patched, err := whm.PatchAnimBoneKeys(file, &model.Model, 0)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	// One key of each kind carries its own count entry beside it, so the
	// payload changes by 16 bytes for the added position key and loses 20
	// for the removed rotation key.
	if want := len(file) + 16 - 20; len(patched) != want {
		t.Fatalf("the patched file is %d bytes, want %d (original %d)", len(patched), want, len(file))
	}
	again, err := ParseWhmModel(patched)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	got := again.Animations[0]
	if got.NumFrames != anim.NumFrames || got.Name != anim.Name {
		t.Errorf("the clip came back as %q/%d frames, want %q/%d",
			got.Name, got.NumFrames, anim.Name, anim.NumFrames)
	}
	gotTrack := got.Bones[0]
	frames := keyEditFrames(posKeyTimes(gotTrack), anim.NumFrames)
	if len(frames) != 3 || frames[0] != 0 || frames[1] != 10 || frames[2] != 15 {
		t.Errorf("position keys sit on frames %v, want [0 10 15]", frames)
	}
	if v := gotTrack.PosKey[2].V; v != [3]float32{0.25, -0.5, 0.75} {
		t.Errorf("the added key holds %v after the round trip, want {0.25 -0.5 0.75}", v)
	}
	rotFrames := keyEditFrames(rotKeyTimes(gotTrack), anim.NumFrames)
	if len(rotFrames) != 1 || rotFrames[0] != anim.NumFrames-1 {
		t.Errorf("rotation keys sit on frames %v, want only the original last frame %d",
			rotFrames, anim.NumFrames-1)
	}
	// Everything the editor did not touch must come back untouched: the
	// mesh tracks, the camera block, the skeleton, the meshes — the whole
	// model with the one edited track held to the edit the editor made.
	want, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the original does not parse: %v", err)
	}
	want.Animations[0].Bones[0].PosKey = model.Animations[0].Bones[0].PosKey
	want.Animations[0].Bones[0].RotKey = model.Animations[0].Bones[0].RotKey
	if !reflect.DeepEqual(want, again) {
		t.Errorf("the round trip changed more than the edited track:\nwant %+v\ngot  %+v", want, again)
	}
}

func TestPatchWhmAnimBoneKeysRefusesToWriteOverTheWrongThing(t *testing.T) {
	file := buildTestWhm()
	// An index no animation answers to.
	model, err := ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	if _, err := whm.PatchAnimBoneKeys(file, &model.Model, 5); err == nil {
		t.Error("an out of range animation index saved without complaint")
	}
	// A clip the file does not carry: its frame count says so.
	model, err = ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	model.Animations[0].NumFrames = 99
	if _, err := whm.PatchAnimBoneKeys(file, &model.Model, 0); err == nil {
		t.Error("a clip with a different frame count saved over the file's own")
	}
	// A skeleton the file does not carry: its bone tracks say so.
	model, err = ParseWhmModel(file)
	if err != nil {
		t.Fatalf("the test model does not parse: %v", err)
	}
	model.Animations[0].Bones = nil
	if _, err := whm.PatchAnimBoneKeys(file, &model.Model, 0); err == nil {
		t.Error("an animation with different bone tracks saved over the file's own")
	}
	// Not a chunky file at all.
	if _, err := whm.PatchAnimBoneKeys([]byte("not a model"), &model.Model, 0); err == nil {
		t.Error("a non-chunky file saved without complaint")
	}
}

// firstAnimWithBoneKeys is the first animation of a parsed model that carries
// a bone with at least two position keys — enough to remove one, add one and
// still have the track left. -1 means the model has no such animation.
func firstAnimWithBoneKeys(model *WhmModel) int {
	for i := range model.Animations {
		for j := range model.Animations[i].Bones {
			if len(model.Animations[i].Bones[j].PosKey) >= 2 {
				return i
			}
		}
	}
	return -1
}

// realWhmFixture is the first animated .whm of the real install that parses
// with a bone worth editing. It skips — like the other checks against the real
// game — when the game is not there.
func realWhmFixture(t *testing.T) ([]byte, *WhmModel, int) {
	t.Helper()
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	checked := 0
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		for _, sub := range []string{"troops", "vehicles"} {
			files, err := os.ReadDir(filepath.Join(dowRacesDir, race.Name(), sub))
			if err != nil {
				continue
			}
			for _, e := range files {
				if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
					continue
				}
				raw, err := os.ReadFile(filepath.Join(dowRacesDir, race.Name(), sub, e.Name()))
				if err != nil {
					continue
				}
				model, err := ParseWhmModel(raw)
				if err != nil {
					continue
				}
				if idx := firstAnimWithBoneKeys(model); idx >= 0 {
					t.Logf("fixture %s: animation %q of %d, bone %q",
						e.Name(), model.Animations[idx].Name, len(model.Animations),
						trackWithTwoPosKeys(&model.Animations[idx]).Name)
					return raw, model, idx
				}
				checked++
				if checked >= 60 {
					t.Skipf("no animated .whm in the first %d files under %s", checked, dowRacesDir)
				}
			}
		}
	}
	t.Skipf("no animated .whm under %s", dowRacesDir)
	return nil, nil, -1
}

// trackWithTwoPosKeys is the track firstAnimWithBoneKeys settled on.
func trackWithTwoPosKeys(anim *WhmAnimation) *WhmTrack {
	for i := range anim.Bones {
		if len(anim.Bones[i].PosKey) >= 2 {
			return &anim.Bones[i]
		}
	}
	return nil
}

// A save that changes nothing has to leave a shipped model byte for byte as it
// was: the game reads every part of the file, not only the part this editor
// writes, and a rebuild that shuffled a name or a padding byte would be a
// change nobody asked for.
func TestPatchWhmAnimBoneKeysLeavesARealModelIdenticalWithoutEdits(t *testing.T) {
	raw, model, idx := realWhmFixture(t)
	patched, err := whm.PatchAnimBoneKeys(raw, &model.Model, idx)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if bytes.Equal(raw, patched) {
		return
	}
	for i := 0; i < len(raw) && i < len(patched); i++ {
		if raw[i] != patched[i] {
			t.Fatalf("a save of untouched keys changed byte %d of %d (output is %d bytes)",
				i, len(raw), len(patched))
		}
	}
	t.Fatalf("a save of untouched keys changed the file length: %d -> %d", len(raw), len(patched))
}

// A structural edit against a shipped model comes back out of the parser as
// exactly that edit: the whole re-read model is compared against the original
// with the same edit applied in memory, so a size field left behind, a name
// shifted by four bytes or a tail rewritten by mistake all fail the same
// comparison.
func TestPatchWhmAnimBoneKeysEditsARealModelAndNothingElse(t *testing.T) {
	raw, model, idx := realWhmFixture(t)
	anim := &model.Animations[idx]
	track := trackWithTwoPosKeys(anim)
	if track == nil {
		t.Fatal("the fixture's animation lost its track with two keys")
	}

	// Remove the track's last position key.
	track.PosKey = track.PosKey[:len(track.PosKey)-1]
	patched, err := whm.PatchAnimBoneKeys(raw, &model.Model, idx)
	if err != nil {
		t.Fatalf("patch after the removal: %v", err)
	}
	if want := len(raw) - 16; len(patched) != want {
		t.Fatalf("after removing a key the file is %d bytes, want %d (original %d)",
			len(patched), want, len(raw))
	}
	reparsed, err := ParseWhmModel(patched)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	want, err := ParseWhmModel(raw)
	if err != nil {
		t.Fatalf("the original does not parse: %v", err)
	}
	for j := range want.Animations[idx].Bones {
		if want.Animations[idx].Bones[j].Name == track.Name {
			want.Animations[idx].Bones[j].PosKey = track.PosKey
			break
		}
	}
	if !reflect.DeepEqual(want, reparsed) {
		t.Errorf("after the removal the model differs beyond the removed key:\nwant %+v\ngot  %+v", want, reparsed)
	}

	// Add a key on the clip's middle frame, sorted into the track.
	raw2, model2, idx2 := realWhmFixture(t)
	anim2 := &model2.Animations[idx2]
	track2 := trackWithTwoPosKeys(anim2)
	mid := WhmKey3{
		Time: timeForFrame(anim2.NumFrames/2, anim2.NumFrames),
		V:    [3]float32{0.125, -0.25, 0.5},
	}
	at := len(track2.PosKey)
	for i, k := range track2.PosKey {
		if k.Time > mid.Time {
			at = i
			break
		}
	}
	track2.PosKey = append(track2.PosKey, WhmKey3{})
	copy(track2.PosKey[at+1:], track2.PosKey[at:])
	track2.PosKey[at] = mid
	patched2, err := whm.PatchAnimBoneKeys(raw2, &model2.Model, idx2)
	if err != nil {
		t.Fatalf("patch after the addition: %v", err)
	}
	if want := len(raw2) + 16; len(patched2) != want {
		t.Fatalf("after adding a key the file is %d bytes, want %d (original %d)",
			len(patched2), want, len(raw2))
	}
	reparsed2, err := ParseWhmModel(patched2)
	if err != nil {
		t.Fatalf("the patched file does not parse: %v", err)
	}
	want2, err := ParseWhmModel(raw2)
	if err != nil {
		t.Fatalf("the original does not parse: %v", err)
	}
	for j := range want2.Animations[idx2].Bones {
		if want2.Animations[idx2].Bones[j].Name == track2.Name {
			want2.Animations[idx2].Bones[j].PosKey = track2.PosKey
			break
		}
	}
	if !reflect.DeepEqual(want2, reparsed2) {
		t.Errorf("after the addition the model differs beyond the added key:\nwant %+v\ngot  %+v", want2, reparsed2)
	}
}

// posKeyTimes and rotKeyTimes read a track's key times for the frame checks
// the key-editing tests make.
func posKeyTimes(t WhmTrack) []float32 {
	out := make([]float32, len(t.PosKey))
	for i, k := range t.PosKey {
		out[i] = k.Time
	}
	return out
}

func rotKeyTimes(t WhmTrack) []float32 {
	out := make([]float32, len(t.RotKey))
	for i, k := range t.RotKey {
		out[i] = k.Time
	}
	return out
}
