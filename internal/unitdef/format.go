package unitdef

import (
	"math"

	"dow-mod-studio/internal/rgd"
)

// BoneAngleDeg maps any angle onto the rotation fields' range of 1..360
// degrees, wrapping it into the circle first. Zero is reported as 360 rather
// than as 0, because the field range starts at one and a slider sitting at the
// bottom of its track and a value of zero are the same position.
//
// It lives here because the field specs above wrap it, and the root's pose
// panel applies it to every axis it shows: one definition, imported by both.
func BoneAngleDeg(d float64) float64 {
	d = math.Mod(d, 360)
	if d < 0 {
		d += 360
	}
	if d <= 0 {
		return 360
	}
	return d
}

// ItoaPlain is strconv.Itoa without the import: a plain decimal integer.
// Counting rows and frames goes through it everywhere a number is shown as
// text, so its spelling is worth pinning in one place.
func ItoaPlain(n int) string {
	if n < 0 {
		return "-" + ItoaPlain(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return ItoaPlain(n/10) + string(rune('0'+n%10))
}

// ItoaCount is a count with the noun it counts: "1 unit", "52 units". The
// singular is spelled out rather than derived, since the two words are not
// always one apart in the strings this formats.
func ItoaCount(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return ItoaPlain(n) + " " + many
}

// RgdLocIDOrEmpty is rgd.LocID with the failure folded into a usable empty
// id, so a value that names no localisation row reads as an empty one rather
// than as an error the caller has to unwrap.
func RgdLocIDOrEmpty(value string) string {
	id, _ := rgd.LocID(value)
	return id
}
