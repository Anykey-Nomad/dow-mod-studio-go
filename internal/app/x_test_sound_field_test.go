package app

import "testing"

func TestSoundRelUnderData(t *testing.T) {
	cases := map[string]string{
		`Data\sound\races\necrons\roar.fda`:  `races\necrons\roar`,
		`C:\Game\Data\sound\music\track.wav`: `music\track`,
		`C:/Game/Data/sound/music/track.wav`: `music\track`,
		`data\sound\phase_in.fda`:            `phase_in`,
		`C:\Game\Data\art\fx\blade.lua`:      ``,
	}
	for in, want := range cases {
		if got := soundRelUnderData(in); got != want {
			t.Errorf("soundRelUnderData(%q) = %q, want %q", in, got, want)
		}
	}
}
