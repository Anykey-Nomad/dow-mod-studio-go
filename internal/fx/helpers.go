package fx

// clamp keeps v inside lo..hi. The curves an emitter follows are free to run
// past the range they are played over, and a sprite past its size or colour
// limits is not something a frame can draw.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
