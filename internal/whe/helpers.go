package whe

// minInt is the smaller of two integers. The parsers use it to clamp a read to
// what is left of the buffer, so a short file truncates a chunk instead of
// reading past its end.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
