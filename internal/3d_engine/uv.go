package engine

// UVMapping describes the mirror, scale, quarter-turn and offset applied to
// one texture's coordinates.
type UVMapping struct {
	Rotate int
	Mirror bool
	Offset [2]float32
	Scale  float32
}

// TransformUV applies the same mapping order used by the hardware shaders:
// mirror, scale, rotation, then offset.
func TransformUV(u, v float32, m UVMapping) (float32, float32) {
	if m.Mirror {
		u = -u
	}
	if m.Scale > 0 && m.Scale != 1 {
		u *= m.Scale
		v *= m.Scale
	}
	switch ((m.Rotate % 4) + 4) % 4 {
	case 1:
		u, v = v, -u
	case 2:
		u, v = -u, -v
	case 3:
		u, v = -v, u
	}
	return u + m.Offset[0], v + m.Offset[1]
}
