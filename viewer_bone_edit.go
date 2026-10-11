package main

import "math"

// This file is the typed side of bone key editing: what the fields in the pose
// bar write when Enter is pressed in one of them, and the conversion between
// the quaternion the rotation keys hold and the XYZ degrees the fields are
// typed in.

// setBoneKeyPosition writes a position into the picked position key. It fails
// when the picked key is not a position key or the bone has no such key,
// which is what lets a typed apply say nothing happened rather than write
// into the wrong track.
func (v *modelViewer) setBoneKeyPosition(p [3]float32) bool {
	if v == nil || v.anim == nil || v.boneKeyMode != 0 || v.boneKeyIndex < 0 {
		return false
	}
	t := v.boneTrack()
	if t == nil || v.boneKeyIndex >= len(t.PosKey) {
		return false
	}
	t.PosKey[v.boneKeyIndex].V = p
	LogInfo("bone: stored position bone=%q key=%d = (%.3f %.3f %.3f)", t.Name, v.boneKeyIndex, p[0], p[1], p[2])
	// The pose cache still holds the answer for the frame on show, and a
	// rewritten key gives a different one: drop it so the viewport redraws
	// the pose the edit produced rather than the one from before it.
	v.poseValid = false
	v.invalidate()
	return true
}

// setBoneKeyRotation writes a quaternion into the picked rotation key, for
// the same reasons as setBoneKeyPosition.
func (v *modelViewer) setBoneKeyRotation(q [4]float32) bool {
	if v == nil || v.anim == nil || v.boneKeyMode != 1 || v.boneKeyIndex < 0 {
		return false
	}
	t := v.boneTrack()
	if t == nil || v.boneKeyIndex >= len(t.RotKey) {
		return false
	}
	t.RotKey[v.boneKeyIndex].V = q
	LogInfo("bone: stored rotation bone=%q key=%d = (%.3f %.3f %.3f %.3f)", t.Name, v.boneKeyIndex, q[0], q[1], q[2], q[3])
	// Same as setBoneKeyPosition: the cached pose predates the edit.
	v.poseValid = false
	v.invalidate()
	return true
}

// boneKeyTarget is the key a typed apply of the given mode writes: the picked
// key when it is of that kind, otherwise the nearest key of that kind to the
// frame on show. -1 means the picked bone has no such key in the clip at all.
// Resolving the target the same way on the way in and on the way out is what
// keeps the fields showing the values that pressing Enter would write.
func (v *modelViewer) boneKeyTarget(frame, mode int) int {
	if v == nil || v.anim == nil {
		return -1
	}
	if v.boneKeyMode == mode && v.boneKeyIndex >= 0 {
		if t := v.boneTrack(); t != nil {
			n := len(t.PosKey)
			if mode == 1 {
				n = len(t.RotKey)
			}
			if v.boneKeyIndex < n {
				return v.boneKeyIndex
			}
		}
	}
	return v.boneKeyIndexForMode(frame, mode)
}

// boneKeyIndexForMode is the index of the picked bone's key of the given mode
// nearest to the frame, or -1 when the bone has no key of that kind. Unlike
// boneKeyFrame there is no distance limit: an apply aims at the key it would
// write, and the nearest one is it however far away it sits.
func (v *modelViewer) boneKeyIndexForMode(frame, mode int) int {
	t := v.boneTrack()
	if t == nil || v.anim == nil {
		return -1
	}
	best, bestDist := -1, int(1e9)
	// The two key kinds are read in their own loops: a bone may hold more
	// rotation keys than position keys, and indexing one list by the other
	// list's length panics on exactly such a track.
	if mode == 1 {
		for i := range t.RotKey {
			d := absInt(frameAt(float64(t.RotKey[i].Time), v.anim.NumFrames) - frame)
			if d < bestDist {
				best, bestDist = i, d
			}
		}
		return best
	}
	for i := range t.PosKey {
		d := absInt(frameAt(float64(t.PosKey[i].Time), v.anim.NumFrames) - frame)
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
func (v *modelViewer) windToPickedBoneKey() {
	if v == nil || v.anim == nil {
		return
	}
	track := v.boneTrack()
	if track == nil || v.boneKeyIndex < 0 {
		return
	}
	var keyTime float32
	switch v.boneKeyMode {
	case 0:
		if v.boneKeyIndex >= len(track.PosKey) {
			return
		}
		keyTime = track.PosKey[v.boneKeyIndex].Time
	case 1:
		if v.boneKeyIndex >= len(track.RotKey) {
			return
		}
		keyTime = track.RotKey[v.boneKeyIndex].Time
	default:
		return
	}
	frame := frameAt(float64(keyTime), v.anim.NumFrames)
	if cur, _ := v.currentFrame(); frame != cur {
		v.seekFrame(frame)
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
// that quatMatrix shows R = Rz·Ry·Rx, the matrix eulerDegFromQuat reads back.
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
// quatMatrix normalizes on the way in, so a key stored slightly unnormalized
// still comes back as angles rather than as garbage.
func eulerDegFromQuat(q [4]float32) [3]float64 {
	m := quatMatrix(q)
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
