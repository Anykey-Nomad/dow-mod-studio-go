package main

import "strings"

// uvNet is the texture mapping as two linear expressions: each output
// coordinate is a combination of the two input coordinates plus the offset.
// The coefficients are the mapping after the mirror, the scale and the turn
// have been multiplied together, which is the only form worth printing.
type uvNet struct {
	u   [2]float32 // coefficients of (u, v) for the new u
	v   [2]float32 // coefficients of (u, v) for the new v
	off [2]float32
}

// uvMaterials is the model's own list of textures, in the order its sub meshes
// name them and without repeats, with the shared mapping at the front under an
// empty name. The empty one is first because it is what a model with a single
// map, or a reader who has not yet found out that it has two, wants to be
// editing.
func (v *modelViewer) uvSelectionList() []string {
	out := []string{""}
	if v == nil || v.model == nil {
		return out
	}
	seen := make(map[string]bool, len(v.model.Meshes))
	for i := range v.model.Meshes {
		m := v.model.Meshes[i].Material
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// uvCurrent is the mapping the controls are editing right now: the selected
// material's own, or the one they all share. The pointer is what lets a
// button change the mapping without every reader of it having to know which
// one is selected.
func (v *modelViewer) uvCurrent() *uvMapping {
	if v == nil {
		return nil
	}
	if v.uvSelection == "" {
		return &v.uv
	}
	if v.uvMaterials == nil {
		v.uvMaterials = make(map[string]*uvMapping)
	}
	m, ok := v.uvMaterials[v.uvSelection]
	if !ok {
		// A material picked for the first time starts from the shared
		// mapping, which is almost always what it wants: the shared
		// mapping is what every other texture is using.
		shared := v.uv
		m = &shared
		v.uvMaterials[v.uvSelection] = m
	}
	return m
}

// selectUVMapping chooses which material the controls edit. The sliders are
// refilled from the new choice the next time the panel is laid out, not here,
// so that a button press does not move the numbers under a pointer that is
// still over the old ones.
func (v *modelViewer) selectUVMapping(material string) {
	if v == nil || v.uvSelection == material {
		return
	}
	v.uvPull()
	v.uvSelection = material
	v.uvSelectionDirty = true
	v.invalidate()
}

// uvOffset reads the two offset sliders as the shift of the map over the model,
// in units of the map itself.
func (v *modelViewer) uvOffset() [2]float32 {
	if v == nil {
		return [2]float32{}
	}
	return [2]float32{
		modelUVMinOffset + v.uvU.Value*(modelUVMaxOffset-modelUVMinOffset),
		modelUVMinOffset + v.uvV.Value*(modelUVMaxOffset-modelUVMinOffset),
	}
}

// uvScaleValue reads the scale slider. One is the middle of its travel and is
// what an untouched model is drawn with.
func (v *modelViewer) uvScaleValue() float32 {
	if v == nil {
		return 1
	}
	return modelUVMinScale + v.uvScale.Value*(modelUVMaxScale-modelUVMinScale)
}

// uvPush fills the sliders from the mapping they are editing. It runs when the
// selection changes, and once at the start, so that the controls always show
// the numbers of the mapping the frame is actually being drawn with.
func (v *modelViewer) uvPush() {
	m := v.uvCurrent()
	if m == nil {
		return
	}
	v.uvSelectionDirty = false
	scale := m.scale
	if scale <= 0 {
		scale = 1
	}
	v.uvU.Value = float32(clampFloat64(float64((m.offset[0]-modelUVMinOffset)/float32(modelUVMaxOffset-modelUVMinOffset)), 0, 1))
	v.uvV.Value = float32(clampFloat64(float64((m.offset[1]-modelUVMinOffset)/float32(modelUVMaxOffset-modelUVMinOffset)), 0, 1))
	v.uvScale.Value = float32(clampFloat64(float64((scale-modelUVMinScale)/(modelUVMaxScale-modelUVMinScale)), 0, 1))
}

// uvPull writes the sliders back into the mapping they are editing. It runs
// after the sliders are laid out, because that is when a drag has been read.
func (v *modelViewer) uvPull() {
	m := v.uvCurrent()
	if m == nil {
		return
	}
	m.offset = v.uvOffset()
	m.scale = v.uvScaleValue()
}

// resetUVMapping puts the selected texture's map back where the file has it. It
// is its own button because a reader who has been dragging the sliders needs
// one, and because "Reset view" only moves the camera. With one texture
// selected out of several it is that one and not the model that goes back.
func (v *modelViewer) resetUVMapping() {
	if v == nil {
		return
	}
	if m := v.uvCurrent(); m != nil {
		*m = uvIdentityMapping()
	}
	v.uvSelectionDirty = true
	v.invalidate()
}

// uvMappingNet multiplies the four operations together into uvNet.
//
// Multiplying them out is not tidiness, it is the whole point. The operations
// do not commute and they do not all cancel in the same places: mirroring U
// and then turning a half turn negates U twice over, which leaves U exactly as
// the file had it and flips V instead. A panel that printed the operations
// would say the model is mirrored when it is not, and a reader writing those
// numbers into a model would get something that looks almost right.
func uvMappingNet(mirror bool, turns int, scale float32, off [2]float32) uvNet {
	a, b, c, d := float32(1), float32(0), float32(0), float32(1)
	if mirror {
		a = -a
	}
	a, b, c, d = a*scale, b*scale, c*scale, d*scale
	switch ((turns % 4) + 4) % 4 {
	case 1: // (u, v) → (v, −u)
		a, b, c, d = c, d, -a, -b
	case 2: // (u, v) → (−u, −v)
		a, b, c, d = -a, -b, -c, -d
	case 3: // (u, v) → (−v, u)
		a, b, c, d = -c, -d, a, b
	}
	return uvNet{u: [2]float32{a, b}, v: [2]float32{c, d}, off: off}
}

// net is the viewer's own mapping in that form.
func (v *modelViewer) net() uvNet {
	m := uvMapping{}
	if cur := v.uvCurrent(); cur != nil {
		m = *cur
	}
	return uvMappingNet(m.mirror, m.rotate, m.scale, m.offset)
}

// uvMappingSummary writes the mapping as the two expressions it works out to,
// which is the form it can be written back down in: a line of this program's
// own code, or the pair of numbers a model would carry in two more UV channels.
func (v *modelViewer) uvMappingSummary() string {
	n := v.net()
	return uvNetTerm(n.u, n.off[0]) + ", " + uvNetTerm(n.v, n.off[1])
}

// uvNetTerm is one of those two expressions: whichever of u and v the mapping
// actually uses, multiplied, and then the shift added to it.
func uvNetTerm(coef [2]float32, off float32) string {
	var parts []string
	for i, axis := range [2]string{"u", "v"} {
		switch c := coef[i]; {
		case c > 0:
			parts = append(parts, axis+"×"+modelUVFixed(c, 3))
		case c < 0:
			parts = append(parts, "-"+axis+"×"+modelUVFixed(-c, 3))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "0")
	}
	s := strings.Join(parts, "+")
	if off != 0 {
		s += modelUVAdd(off)
	}
	return s
}

// uvMappingSequence is the order the mapping is applied in, which is how the
// line above can be checked by hand. It is kept because the two disagree often
// enough to be worth showing: a mirror and a half turn cancel on one axis and
// swap to the other, and seeing the steps beside the result is what makes that
// obvious rather than surprising.
func (v *modelViewer) uvMappingSequence() string {
	m := uvMapping{}
	if cur := v.uvCurrent(); cur != nil {
		m = *cur
	}
	var parts []string
	if m.mirror {
		parts = append(parts, "mirror U")
	}
	scale := m.scale
	if scale <= 0 {
		scale = 1
	}
	parts = append(parts, "×"+modelUVFixed(scale, 3))
	if turns := ((m.rotate % 4) + 4) % 4; turns != 0 {
		parts = append(parts, "turn "+itoaPlain(turns*90)+"°")
	}
	off := m.offset
	if off[0] != 0 || off[1] != 0 {
		parts = append(parts, "shift "+modelUVFixed(off[0], 3)+", "+modelUVFixed(off[1], 3))
	}
	return strings.Join(parts, " → ")
}

// uvMappingNote is the one thing about a mapping that is worth saying in words,
// or nothing. An offset of a whole number of map tiles is the same as no
// offset, because the map repeats exactly that often: a reader who has just
// dragged one to the far end of its travel and seen nothing happen is looking
// at a bug in the model, not at their own mistake.
func (v *modelViewer) uvMappingNote() string {
	off := [2]float32{}
	if cur := v.uvCurrent(); cur != nil {
		off = cur.offset
	}
	if uvWholeTiles(off[0]) && uvWholeTiles(off[1]) {
		return "A whole number of tiles either way: the same as no offset."
	}
	if uvWholeTiles(off[0]) {
		return "U is a whole number of tiles, so only V is moving the map."
	}
	if uvWholeTiles(off[1]) {
		return "V is a whole number of tiles, so only U is moving the map."
	}
	return ""
}

// uvUnmappedMeshes names the sub meshes that have no map of their own, which
// is the commonest reason a model's texture looks wrong and has nothing to do
// with the UVs at all: such a mesh is drawn from a one by one white map, so it
// comes out as an untextured part of an otherwise textured model, and moving
// the sliders will not change it.
func (v *modelViewer) uvUnmappedMeshes() string {
	if v == nil || v.model == nil || len(v.materials) == 0 {
		return ""
	}
	var missing []string
	for i := range v.model.Meshes {
		m := v.model.Meshes[i]
		if m.Material == "" {
			continue
		}
		if _, ok := v.materials[m.Material]; !ok {
			missing = append(missing, m.Name)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	const show = 3
	names := missing
	if len(names) > show {
		names = names[:show]
	}
	out := itoaPlain(len(missing)) + " of " + itoaPlain(len(v.model.Meshes)) + " meshes: "
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	if len(missing) > show {
		out += " and " + itoaPlain(len(missing)-show) + " more"
	}
	return out
}

// modelUVFixed is a decimal with a fixed number of places, always with a point
// and an ASCII minus, so that a value copied out of the panel is a value that
// can be pasted into a model or a line of code without being tidied up first.
func modelUVFixed(v float32, places int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	scale := 1
	for i := 0; i < places; i++ {
		scale *= 10
	}
	n := int(float64(v)*float64(scale) + 0.5)
	s := itoaPlain(n/scale) + "." + padZeroes(n%scale, places)
	if neg {
		s = "-" + s
	}
	return s
}

// padZeroes writes a small number with leading zeroes, so that a value reads
// the same width whichever it happens to be.
func padZeroes(n, places int) string {
	out := make([]byte, places)
	for i := places - 1; i >= 0; i-- {
		out[i] = byte('0' + n%10)
		n /= 10
	}
	return string(out)
}

// modelUVAdd is an offset written as it would be added, with the sign that
// makes it an addition either way, so that the summary reads as arithmetic
// rather than as a list of magnitudes.
func modelUVAdd(v float32) string {
	if v < 0 {
		return "-" + modelUVFixed(-v, 3)
	}
	return "+" + modelUVFixed(v, 3)
}
