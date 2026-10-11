//go:build windows

package main

import (
	"dow-mod-studio/internal/audioplayer"
	"os"
	"path/filepath"
	"testing"
)

// The point of the rate correction is what comes out the other end: a sample
// handed to the player has to be in the audio context's format and the right
// length. This runs the whole conversion on the one file whose true rate was
// settled by ear, and checks the duration came out at the real rate rather than
// the declared one.
func TestFxSoundDecodeForPlaybackProducesRightDuration(t *testing.T) {
	name := "land (1).fda"
	src := filepath.Join("cmd", "audiotest", name)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no reference file: %v", err)
	}
	// The conversion takes the mod tree, so the file is read the way the
	// preview reads it. nil tree means only the on-disk roots are searched, a
	// root is joined with the sound path, and a sound path is always rooted at
	// the game's sound folder because whe.SoundPathCleans prefixes it. So the
	// reference file is copied into a sound folder under a temporary root,
	// which is the shape the preview looks for.
	root := t.TempDir()
	soundDir := filepath.Join(root, "sound")
	if err := os.MkdirAll(soundDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read reference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(soundDir, name), data, 0o644); err != nil {
		t.Fatalf("stage reference: %v", err)
	}
	s, err := fxSoundDecodeForPlayback(nil, []string{root}, name)
	if err != nil {
		t.Fatalf("decode for playback: %v", err)
	}
	if s.Info.SampleRate != audioplayer.SampleRate || s.Info.Channels != audioplayer.Channels {
		t.Errorf("sample came out %d Hz/%d ch, want the context's %d Hz/%d ch",
			s.Info.SampleRate, s.Info.Channels, audioplayer.SampleRate, audioplayer.Channels)
	}
	frames := len(s.PCM) / audioplayer.Channels
	got := float64(frames) / float64(audioplayer.SampleRate)
	// At the declared 22050 this file would last 35.2 s. At its real 44100,
	// converted up to the 48 kHz context, it is 17.6 s.
	t.Logf("%s: %d frames at %d Hz = %.2f s", name, frames, s.Info.SampleRate, got)
	if got > 20 {
		t.Errorf("sample lasts %.2f s, which is the half-speed reading at the declared 22050 Hz", got)
	}
	if got < 16 {
		t.Errorf("sample lasts %.2f s, too short for the 17.6 s the real rate implies", got)
	}
}
