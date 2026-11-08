package whm

import (
	"math"

	"github.com/fogleman/fauxgl"
)

// --- skinning ---------------------------------------------------------------

// Pose holds the skeleton at one point in time. Bones are the accumulated
// model space transforms, BindInv their inverses at bind time, so a vertex is
// moved by Bones[i] * BindInv[i]. At the bind pose that product is the
// identity, which is what leaves an unanimated model exactly as authored.
type Pose struct {
	Bones   []fauxgl.Matrix
	BindInv []fauxgl.Matrix
}

// TransformPoint applies a matrix to a point in model space.
func TransformPoint(m fauxgl.Matrix, p [3]float32) [3]float32 {
	v := m.MulPosition(vec3From(p))
	return [3]float32{float32(v.X), float32(v.Y), float32(v.Z)}
}

// TransformDirection applies a matrix's rotational/scaling part to a vector.
func TransformDirection(m fauxgl.Matrix, p [3]float32) [3]float32 {
	v := fauxgl.Vector{X: float64(p[0]), Y: float64(p[1]), Z: float64(p[2])}
	v = fauxgl.Vector{X: m.X00*v.X + m.X01*v.Y + m.X02*v.Z, Y: m.X10*v.X + m.X11*v.Y + m.X12*v.Z, Z: m.X20*v.X + m.X21*v.Y + m.X22*v.Z}
	if v.Length() > 1e-9 {
		v = v.Normalize()
	}
	return [3]float32{float32(v.X), float32(v.Y), float32(v.Z)}
}

// SkinMatrix returns the transform that maps a bind pose vertex of bone b into
// the current pose.
func (p Pose) SkinMatrix(bone int32) fauxgl.Matrix {
	if !boneInRange(bone, len(p.Bones)) || bone >= int32(len(p.BindInv)) {
		return fauxgl.Identity()
	}
	return p.Bones[bone].Mul(p.BindInv[bone])
}

// RestPose returns the bind pose of the skeleton.
func (m *Model) RestPose() Pose {
	return m.PoseAt(nil, 0)
}

// PoseAt evaluates the skeleton at normalised clip time t, or the bind pose
// when anim is nil. Key times are relative to the clip length and the game
// stores a few beyond 1, so t is clamped.
func (m *Model) PoseAt(anim *Animation, t float32) Pose {
	return m.PoseAtWithFrame(anim, t, fauxgl.Identity())
}

// PoseAtAnimations evaluates several clips at the same normalized time. Tracks
// for different bones are composed into one pose; when clips key the same bone,
// the later clip takes precedence.
func (m *Model) PoseAtAnimations(anims []*Animation, t float32) Pose {
	times := make([]float32, len(anims))
	for i := range times {
		times[i] = t
	}
	return m.PoseAtAnimationTimes(anims, times)
}

// PoseAtAnimationTimes combines clips sampled at their own normalized times.
// Each bone is posed by the clip that owns it: the most specific clip that
// actually moves it. If two clips are equally specific about a bone, the later
// clip takes precedence.
func (m *Model) PoseAtAnimationTimes(anims []*Animation, times []float32) Pose {
	if m == nil {
		return Pose{}
	}
	if len(anims) == 0 {
		return m.RestPose()
	}
	locals := make([]fauxgl.Matrix, len(m.Bones))
	for i, bone := range m.Bones {
		locals[i] = boneMatrix(bone.Pos, bone.Quat)
	}
	// Every clip of a model keys the whole skeleton, the ones that repeat a
	// bone's rest transform included. Letting the last clip win outright — the
	// plain blend this used to be — means a body idle (which moves all 33 bones)
	// erases the idle of every weapon, and each weapon's idle (one moving bone
	// over 32 still ones) erases the body: one clip on screen instead of the
	// unit moving as a whole. So a clip owns the bones it moves away from rest,
	// and the narrowest such clip owns the bone, which puts the whole-body clip
	// underneath the weapon clips and the aim layers above it.
	masks := make([][]bool, len(anims))
	broadness := make([]int, len(anims))
	for i, anim := range anims {
		if anim == nil {
			continue
		}
		masks[i] = anim.movedBones(m)
		for _, moved := range masks[i] {
			if moved {
				broadness[i]++
			}
		}
	}
	for boneIndex, bone := range m.Bones {
		owner := -1
		for clipIndex, anim := range anims {
			if anim == nil || boneIndex >= len(masks[clipIndex]) || !masks[clipIndex][boneIndex] {
				continue
			}
			if owner < 0 || broadness[clipIndex] <= broadness[owner] {
				owner = clipIndex
			}
		}
		if owner < 0 {
			continue
		}
		track := anims[owner].trackFor(boneIndex)
		if track == nil {
			continue
		}
		clipTime := float32(0)
		if owner < len(times) {
			clipTime = clampFloat32(times[owner], 0, 1)
		}
		locals[boneIndex] = boneMatrix(
			sampleVec3(track.PosKey, clipTime, bone.Pos),
			sampleQuat(track.RotKey, clipTime, bone.Quat),
		)
	}
	bind := m.accumulate(nil)
	pose := Pose{Bones: m.accumulate(locals), BindInv: make([]fauxgl.Matrix, len(m.Bones))}
	for i := range bind {
		pose.BindInv[i] = safeInverse(bind[i])
	}
	return pose
}

// movedBones reports, per skeleton bone, whether this clip poses it: a track
// whose keys leave the bone's rest transform. Keys sit in the same parent
// relative frame as the rest transform, so a clip that repeats the rest pose
// reads as still and contributes nothing to a combined pose.
func (a *Animation) movedBones(m *Model) []bool {
	if a == nil || m == nil {
		return nil
	}
	moved := make([]bool, len(m.Bones))
	for _, track := range a.Bones {
		if track.Bone < 0 || track.Bone >= len(m.Bones) {
			continue
		}
		if trackMovesBone(track, m.Bones[track.Bone]) {
			moved[track.Bone] = true
		}
	}
	return moved
}

// MovedBones reports, per skeleton bone, whether the clip poses it away from the
// rest pose: the mask the combined pose uses to decide ownership, and what a
// caller needs to tell whether two clips fight over the same part.
func (m *Model) MovedBones(a *Animation) []bool {
	return a.movedBones(m)
}

// trackMovesBone compares a track's keys against the bone's rest transform.
// Rotations compare by the angle between them, so a quaternion stored with the
// opposite sign — the same rotation — still reads as still.
func trackMovesBone(track Track, bone Bone) bool {
	for _, key := range track.PosKey {
		if math.Abs(float64(key.V[0]-bone.Pos[0])) > 1e-4 ||
			math.Abs(float64(key.V[1]-bone.Pos[1])) > 1e-4 ||
			math.Abs(float64(key.V[2]-bone.Pos[2])) > 1e-4 {
			return true
		}
	}
	for _, key := range track.RotKey {
		if quatDistance(key.V, bone.Quat) > 1e-5 {
			return true
		}
	}
	return false
}

// quatDistance is how far two rotations are apart, independent of the sign of
// the stored quaternion: 0 for the same rotation, 1 for a half turn apart.
func quatDistance(a, b [4]float32) float64 {
	dot := float64(a[0])*float64(b[0]) + float64(a[1])*float64(b[1]) +
		float64(a[2])*float64(b[2]) + float64(a[3])*float64(b[3])
	la := math.Sqrt(float64(a[0])*float64(a[0]) + float64(a[1])*float64(a[1]) +
		float64(a[2])*float64(a[2]) + float64(a[3])*float64(a[3]))
	lb := math.Sqrt(float64(b[0])*float64(b[0]) + float64(b[1])*float64(b[1]) +
		float64(b[2])*float64(b[2]) + float64(b[3])*float64(b[3]))
	if la < 1e-9 || lb < 1e-9 {
		return 1
	}
	if d := math.Abs(dot / (la * lb)); d < 1 {
		return 1 - d
	}
	return 0
}

// trackFor returns the clip's track for one bone, or nil when the clip carries
// no track for it.
func (a *Animation) trackFor(bone int) *Track {
	for i := range a.Bones {
		if a.Bones[i].Bone == bone {
			return &a.Bones[i]
		}
	}
	return nil
}

// PoseAtWithFrame evaluates animation relative to a placement's world matrix.
// Bind inverses are calculated before the frame is applied, preserving each
// copy's bind pose while moving all animated bones with its placement.
func (m *Model) PoseAtWithFrame(anim *Animation, t float32, frame fauxgl.Matrix) Pose {
	n := len(m.Bones)
	pose := Pose{
		Bones:   make([]fauxgl.Matrix, n),
		BindInv: make([]fauxgl.Matrix, n),
	}
	// Bind pose first; it is also the reference the animation deviates from.
	bind := m.accumulate(nil)
	for i, mat := range bind {
		pose.BindInv[i] = safeInverse(mat)
		bind[i] = frame.Mul(mat)
	}
	if anim == nil {
		pose.Bones = bind
		return pose
	}
	t = clampFloat32(t, 0, 1)
	// Bones with no key in this clip keep their bind transform.
	pose.Bones = m.accumulate(m.boneLocals(anim.Bones, t))
	for i := range pose.Bones {
		pose.Bones[i] = frame.Mul(pose.Bones[i])
	}
	return pose
}

// boneLocals returns the per bone local transform, one entry per skeleton
// bone. A nil track falls back to the bind values.
//
// A key replaces the bone's local transform outright, it is not a delta from
// it: the first key of an idle clip repeats the bone's own rest position
// exactly, so the key is stored in the same parent relative frame as the rest
// transform. Chaining the keyed locals through the parent frame is therefore
// all that is needed, and a clip that repeats the rest pose leaves the mesh
// untouched.
func (m *Model) boneLocals(tracks []Track, t float32) []fauxgl.Matrix {
	out := make([]fauxgl.Matrix, len(m.Bones))
	for i, bone := range m.Bones {
		out[i] = boneMatrix(bone.Pos, bone.Quat)
	}
	for k := range tracks {
		track := tracks[k]
		if track.Bone < 0 || track.Bone >= len(m.Bones) {
			continue
		}
		bone := m.Bones[track.Bone]
		out[track.Bone] = boneMatrix(
			sampleVec3(track.PosKey, t, bone.Pos),
			sampleQuat(track.RotKey, t, bone.Quat),
		)
	}
	return out
}

// RootFrame is the frame the reference importer parents a root bone to: a
// 90 degree rotation about X. The game stores the skeleton in that rotated
// frame, so without it every root bone, and with it the whole chain, lands in
// the wrong place: mirrored in Z for a building, so the skeleton ends up below
// the geometry it is supposed to move.
var RootFrame = fauxgl.Rotate(fauxgl.Vector{X: 1, Y: 0, Z: 0}, -math.Pi/2)

// applyRootFrame lifts a root bone's local transform into model space. fauxgl's
// a.Mul(b) applies a first and then b, so the frame goes on the left, which is
// what the reference importer's parent_mat @ orig_transform does.
func applyRootFrame(local fauxgl.Matrix) fauxgl.Matrix { return RootFrame.Mul(local) }

// accumulate multiplies every bone's local transform by its parent chain, so
// the result is in model space. A nil slice means the bind pose.
func (m *Model) accumulate(local []fauxgl.Matrix) []fauxgl.Matrix {
	if local == nil {
		local = m.boneLocals(nil, 0)
	}
	world := make([]fauxgl.Matrix, len(m.Bones))
	for i, bone := range m.Bones {
		mat := local[i]
		if bone.Parent >= 0 && bone.Parent < i {
			mat = world[bone.Parent].Mul(mat)
		} else {
			// A root bone hangs off the rotated parent frame.
			mat = applyRootFrame(mat)
		}
		world[i] = mat
	}
	return world
}

// safeInverse guards against a degenerate bone matrix, which would otherwise
// fill the frame with NaNs.
func safeInverse(m fauxgl.Matrix) fauxgl.Matrix {
	if d := m.Determinant(); d > -1e-9 && d < 1e-9 {
		return fauxgl.Identity()
	}
	return m.Inverse()
} // boneMatrix builds a bone's local transform from a stored translation and
// rotation. The order matters: the reference tools build these with a
// LocRotScale, which is a translation composed after the rotation, so the
// offset is rotated into the bone's own frame. Composing it the other way round
// leaves the offset in the parent's frame and the whole chain comes apart, with
// legs climbing towards the head and the spine diving into the pelvis.
//
// The rotation is turned into a matrix directly from the quaternion rather than
// through an axis and an angle, which would lose the sign of a half turn.
func boneMatrix(pos [3]float32, quat [4]float32) fauxgl.Matrix {
	return fauxgl.Translate(vec3From(pos)).Mul(QuatMatrix(quat))
} // quatMatrix converts a (x, y, z, w) quaternion into a rotation matrix.
// This is the textbook formula, not the transpose that matches fauxgl's own
// Rotate helper. The two differ by the sense of the rotation, and only this
// one puts the bones where the mesh they deform already is: with the transpose
// the leg chain climbs towards the head and the spine dives into the pelvis.
// TestBoneChainAgreesWithMesh pins that down against real game data.
func QuatMatrix(q [4]float32) fauxgl.Matrix {
	x, y, z, w := float64(q[0]), float64(q[1]), float64(q[2]), float64(q[3])
	if n := math.Sqrt(x*x + y*y + z*z + w*w); n > 1e-9 {
		x, y, z, w = x/n, y/n, z/n, w/n
	} else {
		return fauxgl.Identity()
	}
	return fauxgl.Matrix{
		X00: 1 - 2*(y*y+z*z), X01: 2 * (x*y - z*w), X02: 2 * (x*z + y*w), X03: 0,
		X10: 2 * (x*y + z*w), X11: 1 - 2*(x*x+z*z), X12: 2 * (y*z - x*w), X13: 0,
		X20: 2 * (x*z - y*w), X21: 2 * (y*z + x*w), X22: 1 - 2*(x*x+y*y), X23: 0,
		X30: 0, X31: 0, X32: 0, X33: 1,
	}
}

func vec3From(v [3]float32) fauxgl.Vector {
	return fauxgl.Vector{X: float64(v[0]), Y: float64(v[1]), Z: float64(v[2])}
}

// sampleVec3 linearly interpolates a key track, falling back to the rest value
// before the first and after the last key.
func sampleVec3(keys []Key3, t float32, fallback [3]float32) [3]float32 {
	if len(keys) == 0 {
		return fallback
	}
	t = clampFloat32(t, keys[0].Time, keys[len(keys)-1].Time)
	for i := 0; i+1 < len(keys); i++ {
		a, b := keys[i], keys[i+1]
		if t < a.Time || t > b.Time {
			continue
		}
		span := float64(b.Time - a.Time)
		f := 0.0
		if span > 1e-9 {
			f = float64(t-a.Time) / span
		}
		return [3]float32{
			a.V[0] + float32(f*float64(b.V[0]-a.V[0])),
			a.V[1] + float32(f*float64(b.V[1]-a.V[1])),
			a.V[2] + float32(f*float64(b.V[2]-a.V[2])),
		}
	}
	if t <= keys[0].Time {
		return keys[0].V
	}
	return keys[len(keys)-1].V
}

// sampleQuat interpolates a rotation track with nlerp, which is what the
// importer does and is accurate enough for game keyframes.
func sampleQuat(keys []Key4, t float32, fallback [4]float32) [4]float32 {
	if len(keys) == 0 {
		return fallback
	}
	t = clampFloat32(t, keys[0].Time, keys[len(keys)-1].Time)
	for i := 0; i+1 < len(keys); i++ {
		a, b := keys[i], keys[i+1]
		if t < a.Time || t > b.Time {
			continue
		}
		span := float64(b.Time - a.Time)
		f := 0.0
		if span > 1e-9 {
			f = float64(t-a.Time) / span
		}
		var out [4]float32
		dot := float64(a.V[0])*float64(b.V[0]) +
			float64(a.V[1])*float64(b.V[1]) +
			float64(a.V[2])*float64(b.V[2]) +
			float64(a.V[3])*float64(b.V[3])
		sign := 1.0
		if dot < 0 {
			sign = -1
		}
		for c := 0; c < 4; c++ {
			out[c] = a.V[c] + float32(f*sign*float64(b.V[c]-a.V[c]))
		}
		normalizeQuat(&out)
		return out
	}
	if t <= keys[0].Time {
		return keys[0].V
	}
	return keys[len(keys)-1].V
}

func normalizeQuat(q *[4]float32) {
	n := math.Sqrt(float64(q[0])*float64(q[0]) +
		float64(q[1])*float64(q[1]) +
		float64(q[2])*float64(q[2]) +
		float64(q[3])*float64(q[3]))
	if n < 1e-9 {
		*q = [4]float32{0, 0, 0, 1}
		return
	}
	for i := range q {
		q[i] = float32(float64(q[i]) / n)
	}
}

func clampFloat32(v, lo, hi float32) float32 {
	if math.IsNaN(float64(v)) {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// SkinnedVertex transforms a bind pose vertex by the pose. Weights are
// renormalised over the bones that actually exist, so vertices that reference a
// missing bone, or that carry untidy weights, still land somewhere sensible.
func SkinnedVertex(m *Model, pose Pose, v *Vertex) (fauxgl.Vector, fauxgl.Vector) {
	total := 0.0
	for s := 0; s < 4; s++ {
		if boneInRange(v.Bones[s], len(pose.Bones)) {
			total += float64(v.Weights[s])
		}
	}
	if total <= 1e-9 {
		return vec3From(v.Pos), vec3From(v.Normal)
	}
	pos := fauxgl.Vector{}
	normal := fauxgl.Vector{}
	for s := 0; s < 4; s++ {
		if !boneInRange(v.Bones[s], len(pose.Bones)) {
			continue
		}
		w := float64(v.Weights[s]) / total
		if w == 0 {
			continue
		}
		mat := pose.SkinMatrix(v.Bones[s])
		pos = pos.Add(mat.MulPosition(vec3From(v.Pos)).MulScalar(w))
		normal = normal.Add(mat.MulDirection(vec3From(v.Normal)).MulScalar(w))
	}
	if n := normal.Length(); n > 1e-9 {
		normal = normal.DivScalar(n)
	}
	return pos, normal
}

// boneInRange guards the skin slots, where an unused slot is stored as -1 and
// game data regularly points past the end of the skeleton.
func boneInRange(bone int32, count int) bool {
	return bone >= 0 && int(bone) < count
}

// BoneInRange reports whether a signed skin slot refers to a bone in the pose.
// Negative slots are unused in WHM vertices.
func BoneInRange(bone int32, count int) bool { return boneInRange(bone, count) }
