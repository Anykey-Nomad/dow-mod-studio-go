package engine

// ClampFloat64 limits v to the [lo, hi] range. It is the shared bound helper
// for the viewer, the FX overlay and the renderers, so a value clamped one
// way in the view cannot be clamped another way in the frame.
func ClampFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// AbsInt is the absolute value of an int, used wherever pixel distances are
// compared against a tolerance.
func AbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ClampByte rounds an alpha-like float to the nearest byte, clamped to the
// 0..255 range.
func ClampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}
