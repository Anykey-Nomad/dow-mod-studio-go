package sgbview

import (
	"math"
	"testing"
)

// TestFXDeathName — имя клипа или таймлайна считается смертным только по
// целому слову: иначе «soldier» содержал бы «die».
func TestFXDeathName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"syncdeath_1_shared", true},
		{"sync_death_1", true},
		{"die_1", true},
		{"death", true},
		{"straight_to_death_pose", true},
		{"corpse_pose", true},
		{"soldier", false},
		{"idle_1", false},
		{"move_firing", false},
		{"", false},
	}
	for _, c := range cases {
		if got := fxDeathName(c.name); got != c.want {
			t.Fatalf("fxDeathName(%q) = %v, ожидалось %v", c.name, got, c.want)
		}
	}
}

// TestFXEventTime — «на входе» (-1) и «на выходе» (огромное float) файла
// становятся нулём, а обычное время остаётся собой.
func TestFXEventTime(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{-1, 0},
		{0, 0},
		{1.82, 1.82},
		{3.402823e38, 0},
		{math.NaN(), 0},
		{601, 0},
	}
	for _, c := range cases {
		if got := fxEventTime(c.in); got != c.want {
			t.Fatalf("fxEventTime(%g) = %g, ожидалось %g", c.in, got, c.want)
		}
	}
}
