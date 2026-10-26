package unitdef

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FieldKind says what a text field accepts.
type FieldKind int

const (
	// fieldNumber is any decimal number within its limits.
	fieldNumber FieldKind = iota
	// fieldInteger is a whole number within its limits.
	fieldInteger
	// fieldPositive is a number greater than zero.
	fieldPositive
	// fieldText is any text, trimmed.
	fieldText
	// fieldChoice is one of Choices, matched without regard to case.
	fieldChoice
)

// FieldSpec is the rule a field's text is read by. Bounded limits apply only
// when Bounded is set. A Wrap function, when set, maps a number onto the
// field's range instead of rejecting it — the way an angle is kept within a
// turn.
type FieldSpec struct {
	Kind    FieldKind
	Bounded bool
	Min     float64
	Max     float64
	Choices []string
	Wrap    func(float64) float64
}

// parse reads a field's text by its rule. A number comes back as a value
// within the limits, or wrapped into them, and text and choices come back
// trimmed; anything else is an error naming what the field takes.
func (s FieldSpec) Parse(text string) (float64, string, error) {
	text = strings.TrimSpace(text)
	switch s.Kind {
	case fieldText:
		return 0, text, nil
	case fieldChoice:
		for _, c := range s.Choices {
			if strings.EqualFold(c, text) {
				return 0, c, nil
			}
		}
		return 0, "", fmt.Errorf("%q is not one of %s", text, strings.Join(s.Choices, ", "))
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, "", fmt.Errorf("%q is not a number", text)
	}
	switch s.Kind {
	case fieldInteger:
		if f != math.Trunc(f) {
			return 0, "", fmt.Errorf("%q is not a whole number", text)
		}
	case fieldPositive:
		if f <= 0 {
			return 0, "", fmt.Errorf("%q must be greater than zero", text)
		}
	}
	if s.Wrap != nil {
		f = s.Wrap(f)
	}
	if s.Bounded && (f < s.Min || f > s.Max) {
		return 0, "", fmt.Errorf("%g is outside %g to %g", f, s.Min, s.Max)
	}
	return f, "", nil
}

// boneFieldSpecs are the rules the bone key fields are read by: positions take
// any number, and rotations are degrees kept within 1..360 by wrapping, so a
// negative angle is the same turn written positively.
var (
	BonePositionField = FieldSpec{Kind: fieldNumber}
	BoneRotationField = FieldSpec{Kind: fieldNumber, Wrap: BoneAngleDeg}
)
