package editors_test

import (
	"dow-mod-studio/internal/app/editors"
	"testing"
)

func TestAudioOverlayTargets(t *testing.T) {
	if got := editors.FdaTargetWAV(`Data\sound\music\track.fda`); got != `Data\sound\music\track.wav` {
		t.Fatalf("FDA WAV target = %q", got)
	}
	if got := editors.WavTargetFDA(`Data\sound\music\track.wav`); got != `Data\sound\music\track.fda` {
		t.Fatalf("WAV FDA target = %q", got)
	}
}
