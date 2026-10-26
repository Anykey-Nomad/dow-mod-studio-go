package app

import "testing"

func TestAudioOverlayTargets(t *testing.T) {
	if got := fdaTargetWAV(`Data\sound\music\track.fda`); got != `Data\sound\music\track.wav` {
		t.Fatalf("FDA WAV target = %q", got)
	}
	if got := wavTargetFDA(`Data\sound\music\track.wav`); got != `Data\sound\music\track.fda` {
		t.Fatalf("WAV FDA target = %q", got)
	}
}
