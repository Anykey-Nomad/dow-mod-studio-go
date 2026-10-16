package main

import (
	"math"
	"sort"
	"time"

	"github.com/fogleman/fauxgl"
)

// absInt returns the absolute value of v.
func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// boneTrack returns the animation track that belongs to the picked bone, or nil
// when the picked bone has no track in the current clip.
func (v *modelViewer) boneTrack() *WhmTrack {
	if v.anim == nil || v.boneIndex < 0 || v.boneIndex >= len(v.model.Bones) {
		return nil
	}
	for i := range v.anim.Bones {
		if v.anim.Bones[i].Bone == v.boneIndex {
			return &v.anim.Bones[i]
		}
	}
	return nil
}

// boneKeyFrames is the set of frames on which the picked bone has a key, or nil
// when there is nothing to snap to.
func (v *modelViewer) boneKeyFrames() []int {
	t := v.boneTrack()
	if t == nil || v.anim.NumFrames < 1 {
		return nil
	}
	seen := make(map[int]bool)
	add := func(t float32) {
		seen[frameAt(float64(t), v.anim.NumFrames)] = true
	}
	for i := range t.PosKey {
		add(t.PosKey[i].Time)
	}
	for i := range t.RotKey {
		add(t.RotKey[i].Time)
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

// boneKeyCount is how many keyframes the picked bone has in the current clip,
// counting position and rotation keys together.
func (v *modelViewer) boneKeyCount() int {
	t := v.boneTrack()
	if t == nil {
		return 0
	}
	return len(t.PosKey) + len(t.RotKey)
}

// boneKeySelect picks the bone keyframe closest to the current playhead, and
// seeks so that frame is on show. When there is no key near the playhead the
// picked key is cleared instead.
func (v *modelViewer) boneKeySelect() {
	if v.boneIndex < 0 {
		v.boneKeyIndex = -1
		v.boneKeyMode = -1
		return
	}
	frame, _ := v.currentFrame()
	if idx, mode, ok := v.boneKeyFrame(frame); ok {
		v.boneKeyIndex = idx
		v.boneKeyMode = mode
		if f, ok := v.boneKeyFrameAt(); ok {
			v.seekFrame(f)
		}
	} else {
		v.boneKeyIndex = -1
		v.boneKeyMode = -1
	}
}

// setBoneKeyPositionFromPose writes the picked bone's pose position at the
// current playhead into the picked position key, which is what the "Set pos
// from pose" button in the bone parameters panel does.
func (v *modelViewer) setBoneKeyPositionFromPose() {
	if v.boneKeyMode != 0 || v.boneKeyIndex < 0 || v.boneIndex < 0 || v.anim == nil {
		return
	}
	t := v.boneTrack()
	if t == nil || v.boneKeyIndex >= len(t.PosKey) {
		return
	}
	bone := v.model.Bones[v.boneIndex]
	pose := v.pose()
	if v.boneIndex >= len(pose.Bones) {
		return
	}
	worldBone := pose.Bones[v.boneIndex]
	var worldParent fauxgl.Matrix
	if bone.Parent >= 0 && bone.Parent < len(pose.Bones) {
		worldParent = pose.Bones[bone.Parent]
	} else {
		worldParent = boneRootFrame
	}
	local := worldParent.Inverse().MulPosition(boneAtPose(worldBone))
	t.PosKey[v.boneKeyIndex].V = [3]float32{float32(local.X), float32(local.Y), float32(local.Z)}
	v.invalidate()
}

// setBoneKeyRotationFromPose writes the picked bone's pose rotation at the
// current playhead into the picked rotation key, which is what the "Set rot
// from pose" button in the bone parameters panel does.
func (v *modelViewer) setBoneKeyRotationFromPose() {
	if v.boneKeyMode != 1 || v.boneKeyIndex < 0 || v.boneIndex < 0 || v.anim == nil {
		return
	}
	track := v.boneTrack()
	if track == nil || v.boneKeyIndex >= len(track.RotKey) {
		return
	}
	bone := v.model.Bones[v.boneIndex]
	pose := v.pose()
	if v.boneIndex >= len(pose.Bones) {
		return
	}
	worldBone := pose.Bones[v.boneIndex]
	var worldParent fauxgl.Matrix
	if bone.Parent >= 0 && bone.Parent < len(pose.Bones) {
		worldParent = pose.Bones[bone.Parent]
	} else {
		worldParent = boneRootFrame
	}
	localMat := worldParent.Inverse().Mul(worldBone)
	track.RotKey[v.boneKeyIndex].V = matrixQuat(localMat)
	v.invalidate()
}

// matrixQuat extracts a quaternion (x, y, z, w) from the rotation part of a
// 4x4 matrix. It is the inverse of quatMatrix, which builds the matrix from
// that same [4]float32 layout, so a "Set rot from pose" write round-trips
// through the pose unchanged.
//
// fauxgl ships no quaternion type, so the standard Shepperd branches are
// written out against the matrix's named fields.
func matrixQuat(m fauxgl.Matrix) [4]float32 {
	tr := m.X00 + m.X11 + m.X22
	var q [4]float32
	if tr > 0 {
		s := math.Sqrt(tr+1.0) * 2.0
		q[3] = float32(0.25 * s)
		q[0] = float32((m.X21 - m.X12) / s)
		q[1] = float32((m.X02 - m.X20) / s)
		q[2] = float32((m.X10 - m.X01) / s)
		return q
	}
	if m.X00 > m.X11 && m.X00 > m.X22 {
		s := math.Sqrt(1.0+m.X00-m.X11-m.X22) * 2.0
		q[0] = float32(0.25 * s)
		q[1] = float32((m.X01 + m.X10) / s)
		q[2] = float32((m.X02 + m.X20) / s)
		q[3] = float32((m.X21 - m.X12) / s)
		return q
	}
	if m.X11 > m.X22 {
		s := math.Sqrt(1.0+m.X11-m.X00-m.X22) * 2.0
		q[0] = float32((m.X01 + m.X10) / s)
		q[1] = float32(0.25 * s)
		q[2] = float32((m.X12 + m.X21) / s)
		q[3] = float32((m.X02 - m.X20) / s)
		return q
	}
	s := math.Sqrt(1.0+m.X22-m.X00-m.X11) * 2.0
	q[0] = float32((m.X02 + m.X20) / s)
	q[1] = float32((m.X12 + m.X21) / s)
	q[2] = float32(0.25 * s)
	q[3] = float32((m.X10 - m.X01) / s)
	return q
}

// boneAtPose returns the world-space origin of a bone's pose matrix, which is
// the bone's position under the pose on show. MulPosition on the zero vector
// reads the translation column, the same idiom model3d_overlay.go uses for
// bone origins.
func boneAtPose(worldBone fauxgl.Matrix) fauxgl.Vector {
	return worldBone.MulPosition(fauxgl.Vector{})
}

// boneKeyFrame picks the bone keyframe index and mode closest to the given frame.
func (v *modelViewer) boneKeyFrame(frame int) (int, int, bool) {
	t := v.boneTrack()
	if t == nil {
		return -1, -1, false
	}
	bestIdx := 0
	bestMode := 0
	bestDist := int(1e9)
	for i := range t.PosKey {
		f := frameAt(float64(t.PosKey[i].Time), v.anim.NumFrames)
		d := absInt(f - frame)
		if d < bestDist {
			bestDist = d
			bestIdx = i
			bestMode = 0
		}
	}
	for i := range t.RotKey {
		f := frameAt(float64(t.RotKey[i].Time), v.anim.NumFrames)
		d := absInt(f - frame)
		if d < bestDist {
			bestDist = d
			bestIdx = i
			bestMode = 1
		}
	}
	if bestDist > absInt(v.anim.NumFrames) {
		return -1, -1, false
	}
	return bestIdx, bestMode, true
}

// boneKeyFrameAt returns the frame a bone keyframe sits on, and whether it has
// one to sit on. This is the frame the scrub bar can snap to for this bone key.
func (v *modelViewer) boneKeyFrameAt() (int, bool) {
	if v.boneKeyIndex < 0 || v.boneKeyMode < 0 || v.anim == nil || v.boneTrack() == nil {
		return 0, false
	}
	track := v.boneTrack()
	var keyTime float32
	switch v.boneKeyMode {
	case 0:
		if v.boneKeyIndex < len(track.PosKey) {
			keyTime = track.PosKey[v.boneKeyIndex].Time
		}
	case 1:
		if v.boneKeyIndex < len(track.RotKey) {
			keyTime = track.RotKey[v.boneKeyIndex].Time
		}
	}
	if keyTime <= 0 {
		return 0, false
	}
	return frameAt(float64(keyTime), v.anim.NumFrames), true
}

// pose evaluates the current animation time, or the bind pose when stopped.
//
// The result is kept until the playhead moves. One frame asks for it three or
// four times over - the effects need it, the marker overlay needs it, the
// renderer needs it - and each ask walks the whole skeleton, inverts every
// bone and writes two matrices per bone into fresh slices. During playback
// that is the largest thing the preview allocates, and it is the same answer
// every time.
func (v *modelViewer) pose() WhmPose {
	if v.anim == nil {
		return v.model.RestPose()
	}
	if v.poseValid && v.poseAnim == v.anim && v.poseTime == v.time {
		return v.poseCache
	}
	p := v.model.PoseAt(v.anim, v.time)
	v.poseCache, v.poseAnim, v.poseTime, v.poseValid = p, v.anim, v.time, true
	return p
}

// stepFrame moves the play head one frame of the clip forward or back, which
// is how a single pose of an animation is looked at. The model is held still
// while it is being wound: a step that the clock immediately moved on from
// would be a step that does nothing.
//
// The play head is kept inside the clip rather than wrapped, so that holding
// the button down walks to the first frame and stays there, which is what
// winding a tape by hand does. The last frame is the same as the first, so
// stepping past it lands on the start rather than on a frame that is not there.
func (v *modelViewer) stepFrame(delta int) {
	if v == nil || v.model == nil || v.anim == nil || delta == 0 {
		return
	}
	frames := v.anim.NumFrames
	if frames <= 0 {
		return
	}
	v.playing = false
	// The clock is zeroed so that pressing play afterwards starts from the
	// frame that was wound to rather than jumping a whole second ahead of it.
	v.lastFrame = time.Time{}
	step := float64(delta) / float64(frames)
	// The pose is sampled at the middle of a frame, so a play head of exactly
	// one is the last frame, not the one after it.
	t := float64(v.time) + step
	t = math.Mod(t, 1)
	if t < 0 {
		t++
	}
	v.time = float32(t)
	if float64(v.time) >= 1 {
		v.time = 0
	}
	v.invalidate()
}

// currentFrame is which frame of the clip is on show, and how many there are.
// A clip with no frame count reports none, since there is nothing to count. The
// play head is rounded rather than cut, so the frame a step lands on and the
// frame the readout names are the same one.
func (v *modelViewer) currentFrame() (int, int) {
	if v == nil || v.anim == nil || v.anim.NumFrames <= 0 {
		return 0, 0
	}
	frame := int(math.Floor(float64(v.anim.NumFrames) * float64(v.time)))
	return minInt(maxInt(frame, 0), v.anim.NumFrames-1), v.anim.NumFrames
}

// selectAnim switches the playing clip, wrapping around the list.
func (v *modelViewer) selectAnim(index int) {
	if v == nil {
		return
	}
	if v.model == nil || len(v.model.Animations) == 0 {
		v.anim = nil
		v.animIndex = 0
		v.invalidate()
		return
	}
	n := len(v.model.Animations)
	if index < 0 {
		index = n - 1
	}
	if index >= n {
		index = 0
	}
	v.animIndex = index
	v.anim = &v.model.Animations[index]
	v.time = 0
	v.invalidate()
}

// animFramesPerSecond derives a playback rate from a clip. Dawn of War stores a
// frame count and a duration but no explicit rate, so the duration wins and a
// game-like 20 fps is the fallback.
func animFramesPerSecond(anim *WhmAnimation) float64 {
	if anim == nil || anim.NumFrames <= 0 {
		return 20
	}
	if anim.Duration > 0 {
		return float64(anim.NumFrames) / float64(anim.Duration)
	}
	return 20
}

// animClipSeconds is how long one pass of a clip takes, which is what the .whe
// timelines count their event moments in: necron_monolith's add_on_3 is 3513
// frames long and its events are written at 43 to 51, so they only mean
// anything against the 58.5 seconds the clip takes to play.
func animClipSeconds(anim *WhmAnimation) float64 {
	if anim == nil || anim.NumFrames <= 0 {
		return 1
	}
	if s := float64(anim.NumFrames) / animFramesPerSecond(anim); s > 0 {
		return s
	}
	return 1
}
