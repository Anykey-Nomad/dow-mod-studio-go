package whm

import (
	"strings"

	"github.com/fogleman/fauxgl"
)

// Markers are the named attachment points a .whm carries in DATAMARK: FX,
// weapon barrels and similar things hang off them in game. A marker stores
// its parent bone by name plus a 3x3 rotation and a position; which space
// that position is in is what MarkerWorld settles, and the two readings are:
//
//   - bone local: the position is in the parent bone's own frame, so it moves
//     and turns with the bone like part of the skeleton;
//   - bind model space: the position is where the marker sits on the unposed
//     model, carried by the parent bone exactly like a vertex bound to it.
//
// The test that compares both against the model bounds picks the one the game
// uses.

// BoneIndexByName finds a bone by its name, -1 when there is none.
func (m *Model) BoneIndexByName(name string) int {
	for i, b := range m.Bones {
		if b.Name == name {
			return i
		}
	}
	return -1
}

// MarkerIndex returns the index of the marker with the given name, -1 when
// the model carries none by that name. An event names the marker it plays at in
// whatever case the file was written in, so the comparison ignores it.
//
// The comparison trims space as well. Two of the shipped necron immortals name
// 44 of their markers with a leading space — " marker_gauss01" and its kin —
// while the .whe beside the model asks for "marker_gauss01". An exact match
// finds neither, every gauss event falls back to the model's own axes, and the
// tube runs up Z instead of along the barrel. Whitespace is not part of a
// name, so it is not part of the comparison.
func (m *Model) MarkerIndex(name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i := range m.Markers {
		if strings.EqualFold(strings.TrimSpace(m.Markers[i].Name), name) {
			return i
		}
	}
	return -1
}

// markerParentIndex is the bone a marker hangs off, -1 when it hangs off
// nothing (an empty or unknown parent leaves the marker in model space).
func (m *Model) markerParentIndex(mk Marker) int {
	if mk.Parent == "" {
		return -1
	}
	return m.BoneIndexByName(mk.Parent)
}

// markerForwards is the direction each marker's effects leave in, worked out
// once per model from the marker's own matrix and the axis of the bone it hangs
// off.
//
// A marker's matrix is a frame, not a direction: none of its columns is
// reliably the marker's "forward". Measured over every shipped model and every
// marker whose parent bone actually owns vertices, taking column 1 as-is
// points away from that bone's mesh 60.2% of the time and column 1 negated
// 39.8% — so no fixed column, in either sign, is the answer. Scoring all six
// candidates per marker picks the one the marker was placed for, which is what
// a marker exists to say: it sits at the end of a barrel, not in the middle of
// it.
//
// What it is scored against is the parent bone's own axis rather than the
// centroid of that bone's mesh. Both are measurements of where the bone points,
// but only the bone's frame turns with the bone: a weapon whose mesh is
// authored around its pivot scores its centroid close to the pivot itself, and
// a marker on it is then "aimed at" by whichever column happens to point at the
// joint. Measured over 1157 marker pairs named _base/_end, _start/_end or
// _01/_02 in 763 shipped models, aiming at the bone's axis lines the chosen
// column up with the pair's own direction in 46.7% of cases against 20.3% for
// aiming at the centroid — and it is the only one of the readings tried that
// improves on the choice of column rather than replacing it.
//
// The value is filled on first use and kept on the model itself, because the
// answer depends only on the bind pose and walking every vertex of every bone is
// not something to do per frame.

// boneAim is the direction the parent bone of a marker points in, under pose:
// its second frame column turned by the bone itself.
//
// It is read as a local vector and then turned, rather than taken straight out
// of the bone's world matrix, because that is the reading the measurements
// settle on — 45.7% of marker pairs against 39.6% for the raw column, with the
// same 1157 pairs in 763 shipped models. The conjugation by diag(-1, 1, 1) that
// the rest of the marker's frame uses is not applied: it only chooses between
// the two signs of an aim, and both signs are scored here anyway.
func (m *Model) boneAim(bone int, pose Pose) (fauxgl.Vector, bool) {
	if bone < 0 || bone >= len(pose.Bones) {
		return fauxgl.Vector{}, false
	}
	local := fauxgl.Vector{
		X: pose.Bones[bone].X01,
		Y: pose.Bones[bone].X11,
		Z: pose.Bones[bone].X21,
	}
	rot := pose.Bones[bone].MulPosition(local).
		Sub(pose.Bones[bone].MulPosition(fauxgl.Vector{}))
	if rot.Length() < 1e-6 {
		return fauxgl.Vector{}, false
	}
	return rot.Normalize(), true
}

// ModelMarkerForwards picks, for each marker, which of its matrix's six columns
// — three axes in two signs — is the direction its effects leave in. The choice
// is the column's index and sign, not a finished vector, so that MarkerAxis can
// still turn it by the bone under whichever pose is on show.
func ModelMarkerForwards(m *Model) []int {
	if m.MarkerForwards != nil {
		return m.MarkerForwards
	}
	// markerAxisColumn1 is the reading this code has always used, kept for a
	// marker whose bone owns no mesh to aim against.
	const markerAxisColumn1 = 1
	out := make([]int, len(m.Markers))
	for i := range out {
		out[i] = markerAxisColumn1
	}
	pose := m.RestPose()

	// A bone's aim is the same for every marker on it, so it is worked out
	// once per bone rather than once per marker.
	aims := make(map[int]fauxgl.Vector)

	// The centroid of one bone's own vertices: the point a marker sitting on
	// that bone's tip is measured away from. It is the fallback aim for a bone
	// whose own frame says nothing, which is a bone at the root of the
	// skeleton: its second column is the only direction it has, and every
	// marker on it would be scored against the same line.
	centroid := func(bone int) (fauxgl.Vector, bool) {
		var sum fauxgl.Vector
		n := 0
		for j := range m.Vertices {
			v := &m.Vertices[j]
			if !boneInRange(v.Bones[0], len(pose.Bones)) || int(v.Bones[0]) != bone {
				continue
			}
			if float64(v.Weights[0]) < 0.5 {
				continue
			}
			p, _ := SkinnedVertex(m, pose, v)
			sum = sum.Add(p)
			n++
		}
		if n < 4 {
			return fauxgl.Vector{}, false
		}
		return sum.DivScalar(float64(n)), true
	}

	// A bone's centroid is the same for every marker on it, so it is worked
	// out once per bone rather than once per marker.
	centroids := make(map[int]fauxgl.Vector)
	for i := range m.Markers {
		mk := &m.Markers[i]
		b := m.BoneIndexByName(mk.Parent)
		if b < 0 || b >= len(pose.Bones) {
			continue
		}
		world, ok := m.MarkerWorld(i, pose, true)
		if !ok {
			continue
		}
		// What the marker's columns are scored against: the parent bone's
		// own axis, cached per bone because every marker on that bone is
		// scored against the same one. A bone whose own frame says nothing
		// falls back to the centroid of its mesh, which is measured per
		// marker and so is not cached.
		aim, got := aims[b]
		if !got {
			aim, got = m.boneAim(b, pose)
			aims[b] = aim
			_ = got
		}
		if aim.Length() < 1e-6 {
			centre, done := centroids[b]
			if !done {
				if got, ok := centroid(b); ok {
					centre = got
				}
				centroids[b] = centre
			}
			if aim = world.Sub(centre); aim.Length() < 1e-6 {
				// Neither the bone's frame nor its mesh says where it
				// points, so there is nothing to score against and
				// column one stands as the reading this has always
				// fallen back to.
				continue
			}
		}
		aim = aim.Normalize()

		// Score each of the frame's six directions — three columns in two
		// signs — by how well it points along that aim, and keep the best.
		// Stored as the column plus three when negated, so that MarkerAxis
		// can still turn it by the bone under the pose on show.
		best, bestDot := markerAxisColumn1, -2.0
		for col := 0; col < 3; col++ {
			for _, sign := range []float64{1, -1} {
				v := fauxgl.Vector{
					X: -sign * float64(mk.Matrix[col*3+0]),
					Y: sign * float64(mk.Matrix[col*3+1]),
					Z: sign * float64(mk.Matrix[col*3+2]),
				}
				if v.Length() < 1e-6 {
					continue
				}
				rot := pose.Bones[b].MulPosition(v).
					Sub(pose.Bones[b].MulPosition(fauxgl.Vector{}))
				if rot.Length() < 1e-6 {
					continue
				}
				dot := rot.Normalize().Dot(aim)
				if dot <= bestDot {
					continue
				}
				bestDot = dot
				best = col
				if sign < 0 {
					best = col + 3
				}
			}
		}
		out[i] = best
	}
	m.MarkerForwards = out
	return out
}

// MarkerAxis returns where marker i points in model space under pose.
//
// A marker's 3x3 Matrix is its own frame written column by column, and it is
// the only thing that says which way a marker's effect faces: a beam or a muzzle
// flash played at a marker with no second marker to span has to take its
// direction from here.
//
// Which column is the marker's forward is not fixed by the format, and no fixed
// choice works: measured against the mesh of the bone each marker hangs off,
// column one as-is points away from it 60.2% of the time and column one negated
// 39.8%, the other two columns land on a coin flip. So ModelMarkerForwards
// scores all six candidates per marker against that mesh and this reads back
// the winner. The pariah's marker_muzzle is one of the ones column one got
// wrong: it put a muzzle at the tip of a staff facing back down the shaft.
//
// A marker with no parent bone, or a bone outside the pose, has no frame to
// take an axis from, and reports false.
func (m *Model) MarkerAxis(i int, pose Pose, boneLocal bool) (fauxgl.Vector, bool) {
	if i < 0 || i >= len(m.Markers) {
		return fauxgl.Vector{}, false
	}
	mk := &m.Markers[i]
	// Which of the six columns is this marker's own forward.
	col := 1
	if c := ModelMarkerForwards(m); i < len(c) {
		col = c[i]
	}
	sign := 1.0
	negated := false
	if col > 2 {
		col -= 3
		negated = true
	}
	if negated {
		sign = -1
	}
	// The frame is conjugated by diag(-1, 1, 1); see MarkerFrameAxes, which
	// measures that against the markers of 48 shipped models. Scoring the raw
	// columns here would pick a different column than the frame the rest of
	// the renderer lays a marker's effect out in.
	local := fauxgl.Vector{
		X: -sign * float64(mk.Matrix[col*3+0]),
		Y: sign * float64(mk.Matrix[col*3+1]),
		Z: sign * float64(mk.Matrix[col*3+2]),
	}
	b := m.markerParentIndex(*mk)
	if b < 0 || b >= len(pose.Bones) {
		if n := local.Length(); n < 1e-6 {
			return fauxgl.Vector{}, false
		}
		// No bone to turn the axis, but the stored frame is still in the
		// game's y-up space: RootFrame lifts it the same way it lifts a
		// root bone, as MarkerWorld does with the position.
		return RootFrame.MulPosition(local).Normalize(), true
	}
	// Turned by the bone, minus its own translation so only the rotation is
	// left — the same reading MarkerWorld makes of the position.
	rot := pose.Bones[b].MulPosition(local).Sub(pose.Bones[b].MulPosition(fauxgl.Vector{}))
	if !boneLocal && b < len(pose.BindInv) {
		rot = pose.Bones[b].Mul(pose.BindInv[b]).MulPosition(local).
			Sub(pose.Bones[b].Mul(pose.BindInv[b]).MulPosition(fauxgl.Vector{}))
	}
	if n := rot.Length(); n < 1e-6 {
		return fauxgl.Vector{}, false
	}
	return rot.Normalize(), true
}

// MarkerFrameAxes returns the marker's own X/Y/Z axes in model space under
// pose. The marker matrix is column-major, so each column is one local axis.
// Rotations around these axes turn an effect with the marker instead of using
// the model's fixed world axes.
//
// The frame is conjugated by diag(-1, 1, 1), which flips the sign of each
// column's first component. The reference importer wraps a marker's rotation
// the same way, and a marker's own geometry says it is right: over 48 shipped
// models and 218 marker pairs named _base/_end, _start/_end or _01/_02, the
// best matching column lines up with the pair's own direction in 148 cases
// conjugated against 57 without it. The necron warrior's gun tube is the case
// that settles it — its base and end markers are 0.734 apart along the
// renderer's Z, the tube runs straight up the barrel as it should, and the
// conjugated column matches that direction exactly where the raw one points
// across the weapon. That is what put a beam across the barrel instead of
// along it.
func (m *Model) MarkerFrameAxes(i int, pose Pose, boneLocal bool) (x, y, z fauxgl.Vector, ok bool) {
	if i < 0 || i >= len(m.Markers) {
		return fauxgl.Vector{}, fauxgl.Vector{}, fauxgl.Vector{}, false
	}
	mk := &m.Markers[i]
	b := m.markerParentIndex(*mk)
	turn := func(v fauxgl.Vector) fauxgl.Vector {
		if b >= 0 && b < len(pose.Bones) {
			mat := pose.Bones[b]
			if !boneLocal && b < len(pose.BindInv) {
				mat = mat.Mul(pose.BindInv[b])
			}
			v = mat.MulPosition(v).Sub(mat.MulPosition(fauxgl.Vector{}))
		} else {
			// No parent bone: the frame is stored in the game's y-up
			// space, so RootFrame lifts it into model space exactly as
			// it lifts a root bone; see MarkerWorld.
			v = RootFrame.MulPosition(v)
		}
		if v.Length() < 1e-6 {
			return fauxgl.Vector{}
		}
		return v.Normalize()
	}
	axis := func(col int) fauxgl.Vector {
		return fauxgl.Vector{
			X: -float64(mk.Matrix[col*3+0]),
			Y: float64(mk.Matrix[col*3+1]),
			Z: float64(mk.Matrix[col*3+2]),
		}
	}
	x, y, z = turn(axis(0)), turn(axis(1)), turn(axis(2))
	return x, y, z, x.Length() > 0 && y.Length() > 0 && z.Length() > 0
}

// MarkerBasis returns the two axes of marker i's frame that lie across it, in
// model space under pose. The Matrix is written column by column and its three
// columns are the frame's own axes: the first of those is the forward axis
// MarkerAxis returns, the second is the one across it, and the two are already
// perpendicular, so the third comes from their cross. These are what a ring's
// Offset is measured in — without them every ring would be pinned at its
// marker's origin instead of sitting out along the barrel.
func (m *Model) MarkerBasis(i int, pose Pose, boneLocal bool) (right, up fauxgl.Vector, ok bool) {
	fwd, ok := m.MarkerAxis(i, pose, boneLocal)
	if !ok {
		return fauxgl.Vector{}, fauxgl.Vector{}, false
	}
	if i < 0 || i >= len(m.Markers) {
		return fauxgl.Vector{}, fauxgl.Vector{}, false
	}
	mk := &m.Markers[i]
	// Column two this time, not column one: taking the forward column again
	// would make the frame's two axes identical and collapse every Offset.
	// Conjugated like the rest of the frame; see MarkerFrameAxes.
	local := fauxgl.Vector{
		X: -float64(mk.Matrix[6]),
		Y: float64(mk.Matrix[7]),
		Z: float64(mk.Matrix[8]),
	}
	b := m.markerParentIndex(*mk)
	turn := func(v fauxgl.Vector) fauxgl.Vector {
		if b < 0 || b >= len(pose.Bones) {
			// Stored in the game's y-up space; lift it like a root
			// bone, matching MarkerWorld's treatment of the position.
			return RootFrame.MulPosition(v)
		}
		skel := pose.Bones[b].Mul(pose.BindInv[b])
		if !boneLocal && b < len(pose.BindInv) {
			skel = pose.Bones[b].Mul(pose.BindInv[b])
		} else {
			skel = pose.Bones[b]
		}
		out := skel.MulPosition(v).Sub(skel.MulPosition(fauxgl.Vector{}))
		if n := out.Length(); n > 1e-6 {
			return out.Normalize()
		}
		return fauxgl.Vector{}
	}
	up = turn(local)
	n := fwd.Cross(up)
	if n.Length() < 1e-6 {
		return fauxgl.Vector{}, fauxgl.Vector{}, false
	}
	return n.Normalize(), up.Normalize(), true
}

// MarkerWorld returns the position of marker i in model space under pose.
// boneLocal selects how the stored position is read; see the package comment
// above.
//
// A marker with no parent bone does not follow the skeleton, but its stored
// position is still in the same frame a root bone's local transform is read
// in — the game's y-up space. RootFrame lifts it into the renderer's z-up
// model space, the same lift the bone chain gives a root bone. Returning it
// raw leaves the marker's height on the depth axis: the necron gauss
// conduit's markers sit at heights 2.9, 4.2 and 5.5 and read back at z ≈ -0.6,
// which put its beams on the ground instead of through the three guides.
func (m *Model) MarkerWorld(i int, pose Pose, boneLocal bool) (fauxgl.Vector, bool) {
	if i < 0 || i >= len(m.Markers) {
		return fauxgl.Vector{}, false
	}
	mk := &m.Markers[i]
	pos := fauxgl.Vector{X: float64(mk.Pos[0]), Y: float64(mk.Pos[1]), Z: float64(mk.Pos[2])}
	b := m.markerParentIndex(*mk)
	if b < 0 || b >= len(pose.Bones) {
		return RootFrame.MulPosition(pos), true
	}
	if boneLocal {
		return pose.Bones[b].MulPosition(pos), true
	}
	if b >= len(pose.BindInv) {
		return pose.Bones[b].MulPosition(pos), true
	}
	return pose.Bones[b].Mul(pose.BindInv[b]).MulPosition(pos), true
}
