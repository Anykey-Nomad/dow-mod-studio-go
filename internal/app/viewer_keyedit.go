package app

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uistate"
	"math"
	"sort"
)

// boneTrackSnapshot is one undo step: the picked bone's key track as it was
// before an edit. It names the clip and bone it was taken from so a restore
// writes into the track it came from rather than whatever is picked when the
// undo is pressed. The two key lists are copied outright — a key is a time and
// a fixed-size value, so the copy shares nothing with the live track.
type boneTrackSnapshot = uistate.BoneTrackSnapshot

// snapshotBoneTrack copies the picked bone's key track so an edit about to be
// made can be undone. It reports false when there is no clip or bone to record,
// which the callers use to skip pushing an undo entry that could restore
// nothing.
func SnapshotBoneTrack(v *modelViewer) (boneTrackSnapshot, bool) {
	if v == nil || v.Anim == nil || v.BoneIndex < 0 {
		return boneTrackSnapshot{}, false
	}
	t := BoneTrack(v)
	if t == nil {
		return boneTrackSnapshot{}, false
	}
	return boneTrackSnapshot{
		Anim:      v.Anim,
		BoneIndex: v.BoneIndex,
		PosKey:    append([]WhmKey3(nil), t.PosKey...),
		RotKey:    append([]WhmKey4(nil), t.RotKey...),
	}, true
}

// restoreBoneTrack writes a snapshot back into the clip and bone it was taken
// from and leaves the pose cache cold, so the viewport redraws the restored
// pose rather than the one the edit produced. It reports false when the track
// the snapshot named is not in its clip any more.
func RestoreBoneTrack(v *modelViewer, s boneTrackSnapshot) bool {
	if s.Anim == nil || s.BoneIndex < 0 {
		return false
	}
	var t *WhmTrack
	for i := range s.Anim.Bones {
		if s.Anim.Bones[i].Bone == s.BoneIndex {
			t = &s.Anim.Bones[i]
			break
		}
	}
	if t == nil {
		return false
	}
	t.PosKey = append([]WhmKey3(nil), s.PosKey...)
	t.RotKey = append([]WhmKey4(nil), s.RotKey...)
	v.PoseValid = false
	Invalidate(v)
	return true
}

// This file is the timeline side of bone key editing: giving the picked bone a
// key on the frame the playhead is on, taking the picked one away, and moving
// it along the strip. The typed side — writing values into a picked key —
// lives in viewer_bone_edit.go, and the strip these keys are shown on in
// model3d_scrub.go.
//
// The clip stores keyframes only: position and rotation keys at their own
// times, with everything between them interpolated by the renderer. A frame
// with no key is not stored anywhere, so "adding a keyframe" here means adding
// a key to the picked bone's track, and "the ordinary frames" are the strip's
// own ticks rather than anything the file holds.

// timeForFrame is frameAt read backwards: the normalized key time that frameAt
// maps onto exactly this frame. frameAt rounds a time against the clip's frame
// count, so the frame divided by that count is the one time that lands back on
// it — a key written at any other time would draw its mark a frame away from
// the frame it was dropped on, and the panel row under it would name a
// different frame than the strip shows.
func timeForFrame(frame, numFrames int) float32 {
	if numFrames < 1 {
		return 0
	}
	frame = minInt(maxInt(frame, 0), numFrames-1)
	return float32(frame) / float32(numFrames)
}

// addBoneKeyAtFrame gives the picked bone a key of the given kind (0 position,
// 1 rotation) on the given frame, taking its value from the pose on show — the
// same write "Set ... from pose" makes, so a new key records what the viewport
// is already showing and the clip keeps how it looks at that frame. The frame
// must be the frame the playhead is on: that is where the pose comes from.
//
// A key of that kind already on the frame is picked instead of being joined by
// a second one: two keys of one kind on a frame are the same frame twice, and
// the key already there carries the value a new one would be given anyway.
//
// The bool says whether a key was created. Selection always ends up on the key
// at the frame, created or found, which is what makes the panel's typed fields
// show what the next edit would write.
func AddBoneKeyAtFrame(v *modelViewer, frame, mode int) bool {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 || mode < 0 || mode > 1 {
		return false
	}
	t := BoneTrack(v)
	if t == nil {
		return false
	}
	frame = minInt(maxInt(frame, 0), v.Anim.NumFrames-1)
	// Already there? Pick it and say nothing was created.
	if mode == 0 {
		for i := range t.PosKey {
			if frameAt(float64(t.PosKey[i].Time), v.Anim.NumFrames) == frame {
				v.BoneKeyIndex, v.BoneKeyMode = i, 0
				Invalidate(v)
				return false
			}
		}
	} else {
		for i := range t.RotKey {
			if frameAt(float64(t.RotKey[i].Time), v.Anim.NumFrames) == frame {
				v.BoneKeyIndex, v.BoneKeyMode = i, 1
				Invalidate(v)
				return false
			}
		}
	}
	now := timeForFrame(frame, v.Anim.NumFrames)
	// The pose on show is read before the key exists: the moment the
	// placeholder lands in the track it is part of the curve, and a cold
	// pose cache would then evaluate the clip with the empty key it is
	// about to fill — the new key would hold zero instead of the pose.
	// With the cache warmed here, the write below reads the frame as it
	// looked before the edit, which is the value the key should keep.
	Pose(v)
	if mode == 0 {
		at := 0
		for at < len(t.PosKey) && t.PosKey[at].Time <= now {
			at++
		}
		t.PosKey = append(t.PosKey, WhmKey3{})
		copy(t.PosKey[at+1:], t.PosKey[at:])
		t.PosKey[at] = WhmKey3{Time: now}
		v.BoneKeyIndex, v.BoneKeyMode = at, 0
	} else {
		at := 0
		for at < len(t.RotKey) && t.RotKey[at].Time <= now {
			at++
		}
		t.RotKey = append(t.RotKey, WhmKey4{})
		copy(t.RotKey[at+1:], t.RotKey[at:])
		// Identity until the pose write below fills it in: a zero
		// quaternion would be normalized to no turn, but the write
		// replaces it either way.
		t.RotKey[at] = WhmKey4{Time: now, V: [4]float32{0, 0, 0, 1}}
		v.BoneKeyIndex, v.BoneKeyMode = at, 1
	}
	// The value comes from the pose on show, exactly as "Set ... from pose"
	// writes it into an existing key.
	if mode == 0 {
		SetBoneKeyPositionFromPose(v)
	} else {
		SetBoneKeyRotationFromPose(v)
	}
	v.PoseValid = false
	Invalidate(v)
	LogInfo("bone: added %s key at frame %d of bone %q", boneGroupLog(mode), frame, t.Name)
	return true
}

// boneKeyRef names one key of the picked bone's track by the kind of key it
// is and the frame it sits on. The panel's keyframe list addresses its rows
// this way rather than by index: taking a key out of the track shifts every
// index after it, while the frames of the keys that stay where they are do
// not move, so a selection of several rows would otherwise be pointing at
// other people's keys after the first deletion.
type boneKeyRef = uistate.BoneKeyRef

// boneKeyRefs is every key of the picked bone's track in the order the panel
// lists them: position keys first, rotation keys after, each kind in frame
// order — which is the order the track itself keeps.
func BoneKeyRefs(v *modelViewer) []boneKeyRef {
	if v == nil || v.Anim == nil {
		return nil
	}
	t := BoneTrack(v)
	if t == nil {
		return nil
	}
	out := make([]boneKeyRef, 0, len(t.PosKey)+len(t.RotKey))
	for i := range t.PosKey {
		out = append(out, boneKeyRef{Mode: 0, Frame: frameAt(float64(t.PosKey[i].Time), v.Anim.NumFrames)})
	}
	for i := range t.RotKey {
		out = append(out, boneKeyRef{Mode: 1, Frame: frameAt(float64(t.RotKey[i].Time), v.Anim.NumFrames)})
	}
	return out
}

// boneKeyIndexAtFrame is the index of the key of that kind sitting on that
// frame, or -1 when there is none. A key whose time rounds onto the frame
// counts as being on it, since that is what the list and the strip both call
// the frame it is on.
func BoneKeyIndexAtFrame(v *modelViewer, frame, mode int) int {
	if v == nil || v.Anim == nil {
		return -1
	}
	t := BoneTrack(v)
	if t == nil {
		return -1
	}
	if mode == 0 {
		for i := range t.PosKey {
			if frameAt(float64(t.PosKey[i].Time), v.Anim.NumFrames) == frame {
				return i
			}
		}
		return -1
	}
	if mode == 1 {
		for i := range t.RotKey {
			if frameAt(float64(t.RotKey[i].Time), v.Anim.NumFrames) == frame {
				return i
			}
		}
	}
	return -1
}

// pickedBoneKeyRef is the picked key as the list addresses a row: its kind
// and its frame, or no ref at all when nothing is picked. Unlike
// boneKeyFrameAt it counts a key standing on frame zero, which is a real key
// rather than the absence of one.
func PickedBoneKeyRef(v *modelViewer) (boneKeyRef, bool) {
	if !PickedBoneKeyValid(v) {
		return boneKeyRef{Mode: -1}, false
	}
	t := BoneTrack(v)
	keyTime := float32(0)
	if v.BoneKeyMode == 0 {
		keyTime = t.PosKey[v.BoneKeyIndex].Time
	} else {
		keyTime = t.RotKey[v.BoneKeyIndex].Time
	}
	return boneKeyRef{Mode: v.BoneKeyMode, Frame: frameAt(float64(keyTime), v.Anim.NumFrames)}, true
}

// freeBoneKeyFrame walks out from a frame in the given direction (−1 back,
// +1 on) and returns the first frame of the clip that carries no key of that
// kind, or −1 when the clip runs out first. That is where "+ before" and
// "+ after" put the key they add: the neighbouring frame when it is free,
// further out when a key already stands there.
func FreeBoneKeyFrame(v *modelViewer, from, mode, dir int) int {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 || (dir != -1 && dir != 1) {
		return -1
	}
	for f := from + dir; f >= 0 && f < v.Anim.NumFrames; f += dir {
		if BoneKeyIndexAtFrame(v, f, mode) < 0 {
			return f
		}
	}
	return -1
}

// insertBoneKeyCopy gives the picked bone a key of the given kind on the
// given frame, holding what the key it is copied from holds. This is how a
// key is added beside one on the list rather than on the frame the playhead
// stands on: the clip's motion is extended rather than yanked towards the
// pose the viewport happens to be showing, and the copy keeps the value the
// reader is about to edit in the typed fields.
//
// The bool says whether a key was created. A key of that kind already on the
// frame, a frame outside the clip, or a source that is not on the track all
// leave it alone — two keys of one kind on a frame are the same frame twice.
func InsertBoneKeyCopy(v *modelViewer, frame, mode, srcFrame int) bool {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 || mode < 0 || mode > 1 {
		return false
	}
	t := BoneTrack(v)
	if t == nil {
		return false
	}
	frame = minInt(maxInt(frame, 0), v.Anim.NumFrames-1)
	if BoneKeyIndexAtFrame(v, frame, mode) >= 0 {
		return false
	}
	now := timeForFrame(frame, v.Anim.NumFrames)
	if mode == 0 {
		src := BoneKeyIndexAtFrame(v, srcFrame, 0)
		if src < 0 {
			return false
		}
		val := t.PosKey[src].V
		at := 0
		for at < len(t.PosKey) && t.PosKey[at].Time <= now {
			at++
		}
		t.PosKey = append(t.PosKey, WhmKey3{})
		copy(t.PosKey[at+1:], t.PosKey[at:])
		t.PosKey[at] = WhmKey3{Time: now, V: val}
	} else {
		src := BoneKeyIndexAtFrame(v, srcFrame, 1)
		if src < 0 {
			return false
		}
		val := t.RotKey[src].V
		at := 0
		for at < len(t.RotKey) && t.RotKey[at].Time <= now {
			at++
		}
		t.RotKey = append(t.RotKey, WhmKey4{})
		copy(t.RotKey[at+1:], t.RotKey[at:])
		t.RotKey[at] = WhmKey4{Time: now, V: val}
	}
	// The list grew at a known place: the new key is the one to aim the
	// typed fields and the strip at, which is what the selection on show
	// should be on anyway.
	v.BoneKeyIndex, v.BoneKeyMode = BoneKeyIndexAtFrame(v, frame, mode), mode
	v.PoseValid = false
	Invalidate(v)
	LogInfo("bone: added %s key at frame %d of bone %q (copied from frame %d)",
		boneGroupLog(mode), frame, t.Name, srcFrame)
	return true
}

// deleteBoneKeyAt takes the key of that kind sitting on that frame out of the
// picked bone's track. The key the list or the strip had picked keeps pointing
// at itself when an earlier key leaves — its index slides down with the
// removal — and is cleared when the key removed is the picked one, so a later
// typed apply is never aimed at whatever slid into the gap.
func DeleteBoneKeyAt(v *modelViewer, frame, mode int) bool {
	if v == nil || v.Anim == nil {
		return false
	}
	idx := BoneKeyIndexAtFrame(v, frame, mode)
	if idx < 0 {
		return false
	}
	t := BoneTrack(v)
	if t == nil {
		return false
	}
	// Where the picked key sits when it is of this kind. It is read as a
	// frame rather than an index because the removal below moves every index
	// after the one it takes out.
	pickedFrame := -1
	if v.BoneKeyMode == mode && v.BoneKeyIndex >= 0 {
		if mode == 0 && v.BoneKeyIndex < len(t.PosKey) {
			pickedFrame = frameAt(float64(t.PosKey[v.BoneKeyIndex].Time), v.Anim.NumFrames)
		} else if mode == 1 && v.BoneKeyIndex < len(t.RotKey) {
			pickedFrame = frameAt(float64(t.RotKey[v.BoneKeyIndex].Time), v.Anim.NumFrames)
		}
	}
	switch mode {
	case 0:
		LogInfo("bone: removed position key %d at frame %d of bone %q", idx, frame, t.Name)
		t.PosKey = append(t.PosKey[:idx], t.PosKey[idx+1:]...)
	case 1:
		LogInfo("bone: removed rotation key %d at frame %d of bone %q", idx, frame, t.Name)
		t.RotKey = append(t.RotKey[:idx], t.RotKey[idx+1:]...)
	default:
		return false
	}
	if pickedFrame >= 0 {
		if at := BoneKeyIndexAtFrame(v, pickedFrame, mode); at >= 0 {
			v.BoneKeyIndex, v.BoneKeyMode = at, mode
		} else {
			v.BoneKeyIndex, v.BoneKeyMode = -1, -1
		}
	}
	v.PoseValid = false
	Invalidate(v)
	return true
}

// pickedBoneKeyValid says whether the picked key still exists. The lists
// shrink under the selection when a key is deleted or a drag drops one onto
// another, and a button that acts on the picked key has to be off then rather
// than aimed at whatever key slid into its place.
func PickedBoneKeyValid(v *modelViewer) bool {
	if v == nil || v.Anim == nil || v.BoneKeyIndex < 0 || v.BoneKeyMode < 0 {
		return false
	}
	t := BoneTrack(v)
	if t == nil {
		return false
	}
	if v.BoneKeyMode == 0 {
		return v.BoneKeyIndex < len(t.PosKey)
	}
	return v.BoneKeyIndex < len(t.RotKey)
}

// deletePickedBoneKey removes the picked key from the picked bone's track and
// leaves nothing picked: the key it pointed at is gone, and promoting a
// neighbour would silently aim the next typed apply at a key the reader never
// chose. The list stays sorted because removal cannot unsort it.
func DeletePickedBoneKey(v *modelViewer) bool {
	if v == nil || v.Anim == nil || v.BoneKeyIndex < 0 || v.BoneKeyMode < 0 {
		return false
	}
	t := BoneTrack(v)
	if t == nil {
		return false
	}
	switch v.BoneKeyMode {
	case 0:
		if v.BoneKeyIndex >= len(t.PosKey) {
			return false
		}
		LogInfo("bone: removed position key %d at frame %d of bone %q",
			v.BoneKeyIndex, frameAt(float64(t.PosKey[v.BoneKeyIndex].Time), v.Anim.NumFrames), t.Name)
		t.PosKey = append(t.PosKey[:v.BoneKeyIndex], t.PosKey[v.BoneKeyIndex+1:]...)
	case 1:
		if v.BoneKeyIndex >= len(t.RotKey) {
			return false
		}
		LogInfo("bone: removed rotation key %d at frame %d of bone %q",
			v.BoneKeyIndex, frameAt(float64(t.RotKey[v.BoneKeyIndex].Time), v.Anim.NumFrames), t.Name)
		t.RotKey = append(t.RotKey[:v.BoneKeyIndex], t.RotKey[v.BoneKeyIndex+1:]...)
	default:
		return false
	}
	v.BoneKeyIndex, v.BoneKeyMode = -1, -1
	v.PoseValid = false
	Invalidate(v)
	return true
}

// movePickedBoneKeyToFrame puts the picked key on the given frame and keeps
// its list sorted by time — sampleVec3 and sampleQuat walk their keys in
// order, so a list sorted wrong would play the clip wrong. A key of the same
// kind already on that frame is dropped rather than kept beside the moved one:
// two keys of one kind on a frame are the same frame twice, and the key being
// moved is the one the reader is dropping. The bool says whether anything
// moved; a drop on the frame the key already sits on changes nothing.
func MovePickedBoneKeyToFrame(v *modelViewer, frame int) bool {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 || v.BoneKeyIndex < 0 || v.BoneKeyMode < 0 {
		return false
	}
	t := BoneTrack(v)
	if t == nil {
		return false
	}
	frame = minInt(maxInt(frame, 0), v.Anim.NumFrames-1)
	now := timeForFrame(frame, v.Anim.NumFrames)
	if v.BoneKeyMode == 0 {
		if v.BoneKeyIndex >= len(t.PosKey) {
			return false
		}
		if frameAt(float64(t.PosKey[v.BoneKeyIndex].Time), v.Anim.NumFrames) == frame {
			return false
		}
		moved := t.PosKey[v.BoneKeyIndex]
		rest := make([]WhmKey3, 0, len(t.PosKey)-1)
		for i, k := range t.PosKey {
			if i == v.BoneKeyIndex {
				continue
			}
			if frameAt(float64(k.Time), v.Anim.NumFrames) == frame {
				continue
			}
			rest = append(rest, k)
		}
		moved.Time = now
		at := 0
		for at < len(rest) && rest[at].Time <= moved.Time {
			at++
		}
		rest = append(rest, WhmKey3{})
		copy(rest[at+1:], rest[at:])
		rest[at] = moved
		t.PosKey = rest
		v.BoneKeyIndex = at
	} else {
		if v.BoneKeyIndex >= len(t.RotKey) {
			return false
		}
		if frameAt(float64(t.RotKey[v.BoneKeyIndex].Time), v.Anim.NumFrames) == frame {
			return false
		}
		moved := t.RotKey[v.BoneKeyIndex]
		rest := make([]WhmKey4, 0, len(t.RotKey)-1)
		for i, k := range t.RotKey {
			if i == v.BoneKeyIndex {
				continue
			}
			if frameAt(float64(k.Time), v.Anim.NumFrames) == frame {
				continue
			}
			rest = append(rest, k)
		}
		moved.Time = now
		at := 0
		for at < len(rest) && rest[at].Time <= moved.Time {
			at++
		}
		rest = append(rest, WhmKey4{})
		copy(rest[at+1:], rest[at:])
		rest[at] = moved
		t.RotKey = rest
		v.BoneKeyIndex = at
	}
	v.PoseValid = false
	Invalidate(v)
	LogInfo("bone: moved %s key of bone %q to frame %d",
		boneGroupLog(v.BoneKeyMode), t.Name, frame)
	return true
}

// shiftKeyFrames works out the frame each key of one kind ends on when a set
// of them is shifted by delta frames. A key keeps the frame it is already on
// when the frame it would move to is taken by a key that is not shifting, or
// when a clamping shift has piled several keys onto the same end of the clip.
// It returns the frame each key lands on, how many actually moved and how many
// were held back. frames must be the keys' frames in ascending order.
func shiftKeyFrames(frames []int, moving map[int]bool, delta, n int) (target map[int]int, moved, blocked int) {
	static := map[int]bool{}
	for _, f := range frames {
		if !moving[f] {
			static[f] = true
		}
	}
	// The frame each shifting key wants, and the keys that cannot have it:
	// the frame is off the end of the clip but where they already stand, or a
	// key that is not shifting already holds it. Those stay put, and the frame
	// they stay on is reserved so a later key does not land on top of them.
	desired := map[int]int{}
	stay := map[int]bool{}
	for _, f := range frames {
		if !moving[f] {
			continue
		}
		d := f + delta
		if d < 0 {
			d = 0
		}
		if d > n-1 {
			d = n - 1
		}
		desired[f] = d
		if d == f || static[d] {
			stay[f] = true
		}
	}
	reserved := map[int]bool{}
	for f := range static {
		reserved[f] = true
	}
	for f := range stay {
		reserved[f] = true
	}
	target = map[int]int{}
	for _, f := range frames {
		if !moving[f] {
			target[f] = f
			continue
		}
		if stay[f] {
			target[f] = f
			if desired[f] != f {
				blocked++
			}
			continue
		}
		d := desired[f]
		if !reserved[d] {
			target[f] = d
			reserved[d] = true
			if d != f {
				moved++
			}
		} else {
			// The frame it wanted is held by a key that is staying put, so
			// this one stays where it is rather than landing on top of it.
			target[f] = f
			reserved[f] = true
			blocked++
		}
	}
	return target, moved, blocked
}

// shiftBoneKeys moves the picked keys of the picked bone's track by delta
// frames (negative back, positive forward) and keeps each list sorted by time,
// which is the order the samplers walk. Keys of a kind that are not being
// shifted hold their frame, and a shifted key whose new frame is already taken
// by one of those stays put rather than landing on it. The selection the
// caller holds is remapped onto the frames the keys ended on, so a run of
// shifts can carry a whole block along the clip without reselecting.
func ShiftBoneKeys(v *modelViewer, refs []boneKeyRef, delta int) (newRefs []boneKeyRef, moved, blocked int) {
	if v == nil || v.Anim == nil || v.Anim.NumFrames < 1 || delta == 0 {
		return nil, 0, 0
	}
	t := BoneTrack(v)
	if t == nil {
		return nil, 0, 0
	}
	n := v.Anim.NumFrames
	moving := [2]map[int]bool{{}, {}}
	for _, r := range refs {
		if r.Mode == 0 || r.Mode == 1 {
			moving[r.Mode][r.Frame] = true
		}
	}
	tgt := [2]map[int]int{}
	// Position keys: every key's frame and value are read before anything is
	// rewritten, then the whole list is rebuilt in frame order.
	if len(moving[0]) > 0 {
		frames := make([]int, 0, len(t.PosKey))
		orig := make(map[int]WhmKey3, len(t.PosKey))
		for _, k := range t.PosKey {
			f := frameAt(float64(k.Time), n)
			frames = append(frames, f)
			orig[f] = k
		}
		target, m, b := shiftKeyFrames(frames, moving[0], delta, n)
		tgt[0] = target
		moved += m
		blocked += b
		type item struct {
			f    int
			time float32
			v    [3]float32
		}
		out := make([]item, 0, len(frames))
		for _, f := range frames {
			nf, ok := target[f]
			if !ok {
				continue
			}
			tm := orig[f].Time
			if nf != f {
				tm = timeForFrame(nf, n)
			}
			out = append(out, item{nf, tm, orig[f].V})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].f < out[j].f })
		t.PosKey = t.PosKey[:0]
		for _, it := range out {
			t.PosKey = append(t.PosKey, WhmKey3{Time: it.time, V: it.v})
		}
	}
	// Rotation keys, the same way.
	if len(moving[1]) > 0 {
		frames := make([]int, 0, len(t.RotKey))
		orig := make(map[int]WhmKey4, len(t.RotKey))
		for _, k := range t.RotKey {
			f := frameAt(float64(k.Time), n)
			frames = append(frames, f)
			orig[f] = k
		}
		target, m, b := shiftKeyFrames(frames, moving[1], delta, n)
		tgt[1] = target
		moved += m
		blocked += b
		type item struct {
			f    int
			time float32
			v    [4]float32
		}
		out := make([]item, 0, len(frames))
		for _, f := range frames {
			nf, ok := target[f]
			if !ok {
				continue
			}
			tm := orig[f].Time
			if nf != f {
				tm = timeForFrame(nf, n)
			}
			out = append(out, item{nf, tm, orig[f].V})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].f < out[j].f })
		t.RotKey = t.RotKey[:0]
		for _, it := range out {
			t.RotKey = append(t.RotKey, WhmKey4{Time: it.time, V: it.v})
		}
	}
	// The selection follows the keys to the frames they landed on.
	for _, r := range refs {
		if r.Mode != 0 && r.Mode != 1 {
			continue
		}
		if nf, ok := tgt[r.Mode][r.Frame]; ok {
			newRefs = append(newRefs, boneKeyRef{Mode: r.Mode, Frame: nf})
		}
	}
	if moved > 0 {
		v.PoseValid = false
		Invalidate(v)
	}
	return newRefs, moved, blocked
}

// ─── merged from viewer_bone_edit.go ───────────────────────────────────────────────

// This file is the typed side of bone key editing: what the fields in the pose
// bar write when Enter is pressed in one of them, and the conversion between
// the quaternion the rotation keys hold and the XYZ degrees the fields are
// typed in.

// setBoneKeyPosition writes a position into the picked position key. It fails
// when the picked key is not a position key or the bone has no such key,
// which is what lets a typed apply say nothing happened rather than write
// into the wrong track.
func SetBoneKeyPosition(v *modelViewer, p [3]float32) bool {
	if v == nil || v.Anim == nil || v.BoneKeyMode != 0 || v.BoneKeyIndex < 0 {
		return false
	}
	t := BoneTrack(v)
	if t == nil || v.BoneKeyIndex >= len(t.PosKey) {
		return false
	}
	t.PosKey[v.BoneKeyIndex].V = p
	LogInfo("bone: stored position bone=%q key=%d = (%.3f %.3f %.3f)", t.Name, v.BoneKeyIndex, p[0], p[1], p[2])
	// The pose cache still holds the answer for the frame on show, and a
	// rewritten key gives a different one: drop it so the viewport redraws
	// the pose the edit produced rather than the one from before it.
	v.PoseValid = false
	Invalidate(v)
	return true
}

// setBoneKeyRotation writes a quaternion into the picked rotation key, for
// the same reasons as setBoneKeyPosition.
func SetBoneKeyRotation(v *modelViewer, q [4]float32) bool {
	if v == nil || v.Anim == nil || v.BoneKeyMode != 1 || v.BoneKeyIndex < 0 {
		return false
	}
	t := BoneTrack(v)
	if t == nil || v.BoneKeyIndex >= len(t.RotKey) {
		return false
	}
	t.RotKey[v.BoneKeyIndex].V = q
	LogInfo("bone: stored rotation bone=%q key=%d = (%.3f %.3f %.3f %.3f)", t.Name, v.BoneKeyIndex, q[0], q[1], q[2], q[3])
	// Same as setBoneKeyPosition: the cached pose predates the edit.
	v.PoseValid = false
	Invalidate(v)
	return true
}

// boneKeyTarget is the key a typed apply of the given mode writes: the picked
// key when it is of that kind, otherwise the nearest key of that kind to the
// frame on show. -1 means the picked bone has no such key in the clip at all.
// Resolving the target the same way on the way in and on the way out is what
// keeps the fields showing the values that pressing Enter would write.
func BoneKeyTarget(v *modelViewer, frame, mode int) int {
	if v == nil || v.Anim == nil {
		return -1
	}
	if v.BoneKeyMode == mode && v.BoneKeyIndex >= 0 {
		if t := BoneTrack(v); t != nil {
			n := len(t.PosKey)
			if mode == 1 {
				n = len(t.RotKey)
			}
			if v.BoneKeyIndex < n {
				return v.BoneKeyIndex
			}
		}
	}
	return BoneKeyIndexForMode(v, frame, mode)
}

// boneKeyIndexForMode is the index of the picked bone's key of the given mode
// nearest to the frame, or -1 when the bone has no key of that kind. Unlike
// boneKeyFrame there is no distance limit: an apply aims at the key it would
// write, and the nearest one is it however far away it sits.
func BoneKeyIndexForMode(v *modelViewer, frame, mode int) int {
	t := BoneTrack(v)
	if t == nil || v.Anim == nil {
		return -1
	}
	best, bestDist := -1, int(1e9)
	// The two key kinds are read in their own loops: a bone may hold more
	// rotation keys than position keys, and indexing one list by the other
	// list's length panics on exactly such a track.
	if mode == 1 {
		for i := range t.RotKey {
			d := engine.AbsInt(frameAt(float64(t.RotKey[i].Time), v.Anim.NumFrames) - frame)
			if d < bestDist {
				best, bestDist = i, d
			}
		}
		return best
	}
	for i := range t.PosKey {
		d := engine.AbsInt(frameAt(float64(t.PosKey[i].Time), v.Anim.NumFrames) - frame)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// windToPickedBoneKey puts the playhead on the picked key's frame when it is
// somewhere else: an edit only shows on the viewport where its key is, and
// "applied" should be something the eye can confirm without hunting for the
// frame. A playhead already on the frame is left where it is.
func WindToPickedBoneKey(v *modelViewer) {
	if v == nil || v.Anim == nil {
		return
	}
	track := BoneTrack(v)
	if track == nil || v.BoneKeyIndex < 0 {
		return
	}
	var keyTime float32
	switch v.BoneKeyMode {
	case 0:
		if v.BoneKeyIndex >= len(track.PosKey) {
			return
		}
		keyTime = track.PosKey[v.BoneKeyIndex].Time
	case 1:
		if v.BoneKeyIndex >= len(track.RotKey) {
			return
		}
		keyTime = track.RotKey[v.BoneKeyIndex].Time
	default:
		return
	}
	frame := frameAt(float64(keyTime), v.Anim.NumFrames)
	if cur, _ := CurrentFrame(v); frame != cur {
		SeekFrame(v, frame)
	}
}

// quatMulQ is the Hamilton product of two (x, y, z, w) quaternions.
func quatMulQ(a, b [4]float32) [4]float32 {
	return [4]float32{
		a[3]*b[0] + a[0]*b[3] + a[1]*b[2] - a[2]*b[1],
		a[3]*b[1] - a[0]*b[2] + a[1]*b[3] + a[2]*b[0],
		a[3]*b[2] + a[0]*b[1] - a[1]*b[0] + a[2]*b[3],
		a[3]*b[3] - a[0]*b[0] - a[1]*b[1] - a[2]*b[2],
	}
}

// quatSame reports whether two quaternions are the same rotation to within
// a fraction of a degree. q and -q are the same turn read from opposite
// sides, so the dot product's magnitude is what decides: an unchanged key
// compares equal however the stored signs came out.
func quatSame(a, b [4]float32) bool {
	norm := func(q [4]float32) float64 {
		return math.Sqrt(float64(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3]))
	}
	na, nb := norm(a), norm(b)
	if na == 0 || nb == 0 {
		return na == nb
	}
	dot := math.Abs(float64(a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3]))
	return dot/(na*nb) > 0.999999
}

// quatFromEulerDeg turns XYZ degrees into the quaternion the rotation keys
// hold: the X turn first, then Y, then Z, which is the order the fields are
// read in. The three half-angle axis quaternions are multiplied in reverse so
// that render.QuatMatrix shows R = Rz·Ry·Rx, the matrix eulerDegFromQuat reads back.
func quatFromEulerDeg(x, y, z float64) [4]float32 {
	const half = math.Pi / 360
	sx, cx := math.Sin(x*half), math.Cos(x*half)
	sy, cy := math.Sin(y*half), math.Cos(y*half)
	sz, cz := math.Sin(z*half), math.Cos(z*half)
	return quatMulQ(
		[4]float32{0, 0, float32(sz), float32(cz)},
		quatMulQ([4]float32{0, float32(sy), 0, float32(cy)},
			[4]float32{float32(sx), 0, 0, float32(cx)}),
	)
}

// eulerDegFromQuat is the inverse of quatFromEulerDeg: the XYZ degrees a
// quaternion turns about, read out of its rotation matrix for R = Rz·Ry·Rx.
// render.QuatMatrix normalizes on the way in, so a key stored slightly unnormalized
// still comes back as angles rather than as garbage.
func eulerDegFromQuat(q [4]float32) [3]float64 {
	m := render.QuatMatrix(q)
	sinBeta := -float64(m.X20)
	if sinBeta < -1 {
		sinBeta = -1
	}
	if sinBeta > 1 {
		sinBeta = 1
	}
	beta := math.Asin(sinBeta)
	const r2d = 180 / math.Pi
	if math.Abs(sinBeta) < 0.999999 {
		return [3]float64{
			math.Atan2(float64(m.X21), float64(m.X22)) * r2d,
			beta * r2d,
			math.Atan2(float64(m.X10), float64(m.X00)) * r2d,
		}
	}
	// At the poles the Y turn has swallowed the X and Z turns and the matrix
	// can only say what the two of them together came to: keep that in X and
	// drop Z, which is the usual gimbal-lock resolution.
	return [3]float64{math.Atan2(-float64(m.X01), float64(m.X11)) * r2d, beta * r2d, 0}
}
