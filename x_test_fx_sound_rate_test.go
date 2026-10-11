//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"dow-mod-studio/internal/fda"
)

// A file that declares 22050 is played at 44100, because 22050 is the rate the
// game's own files get wrong and it is what makes them run at half length an
// octave down. These are the files whose true rate was settled by ear, so the
// correction is checked against them rather than against a formula.
func TestFxSoundBogusRateIsCorrected(t *testing.T) {
	cases := []struct {
		declared int
		want     int
		why      string
	}{
		{22050, 44100, "declares 22050 but is really 44100"},
		{44100, 44100, "a correct header is left alone"},
		{48000, 48000, "48 kHz is left alone"},
		{11025, 11025, "a rate that was never wrong stays as it is"},
		{0, 0, "an unreadable header is not guessed at"},
	}
	for _, c := range cases {
		if got := fxSoundDeclaredRate(c.declared); got != c.want {
			t.Errorf("fxSoundDeclaredRate(%d) = %d, want %d: %s",
				c.declared, got, c.want, c.why)
		}
	}
}

// The correction has to be exactly a change of rate and nothing else. The
// decoder was measured to return the same samples at every rate, so a file read
// at 44100 and the same file read at 22050 differ only in how long they last —
// and reading it at the corrected rate is what halves that.
func TestFxSoundRateCorrectionOnlyChangesDuration(t *testing.T) {
	path := `cmd/audiotest/land (1).fda`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no reference file: %v", err)
	}
	file, err := fda.ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pcm, _, err := fda.DecodeFDAToPCMAtRate(file, fxSoundRealRate)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	bad := float64(len(pcm)) / float64(fxSoundBogusRate)
	good := float64(len(pcm)) / float64(fxSoundRealRate)
	t.Logf("%s: %d frames -> %.2f s at the declared %d Hz, %.2f s at the real %d Hz",
		filepath.Base(path), len(pcm), bad, fxSoundBogusRate, good, fxSoundRealRate)
	if ratio := bad / good; ratio < 1.99 || ratio > 2.01 {
		t.Errorf("declared rate plays %.3fx slower, not the ~2x that was heard", ratio)
	}
	// The decoder must not be doing anything rate-dependent behind this,
	// because the whole correction assumes the samples do not change.
	again, _, err := fda.DecodeFDAToPCMAtRate(file, fxSoundBogusRate)
	if err != nil {
		t.Fatalf("decode at declared rate: %v", err)
	}
	if len(again) != len(pcm) {
		t.Errorf("decoding at %d Hz gave %d frames and at %d Hz gave %d; the samples are supposed to be the same",
			fxSoundBogusRate, len(again), fxSoundRealRate, len(pcm))
	}
}
