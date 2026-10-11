//go:build windows

package main

import (
	"dow-mod-studio/internal/wav"
	"os"
	"path/filepath"
	"testing"
)

// The tool writes WAVs for a person to listen to, so a malformed file is worse
// than no file: it would be reported as "this variant sounds wrong" when what
// went wrong is the writing. Read them back with the application's own parser,
// which is the same one that has to cope with them later.
func TestAudiotestWrittenWavsAreValid(t *testing.T) {
	dir := filepath.Join(os.Getenv("TEMP"), "audiotest_in")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("no extracted inputs: %v", err)
	}
	_ = entries
	out := filepath.Join(os.Getenv("TEMP"), "audiotest_out")
	wavs, err := filepath.Glob(filepath.Join(out, "*.wav"))
	if err != nil || len(wavs) == 0 {
		t.Skipf("no written WAVs in %s", out)
	}
	for _, w := range wavs {
		data, err := os.ReadFile(w)
		if err != nil {
			t.Errorf("%s: %v", w, err)
			continue
		}
		info, err := wav.Parse(data)
		if err != nil {
			t.Errorf("%s: the application's own parser rejects it: %v", filepath.Base(w), err)
			continue
		}
		frames := len(info.PCMData) / 2 / info.Channels
		sec := float64(frames) / float64(info.SampleRate)
		t.Logf("%-34s %6d Hz %dch %8d frames %8.3f s  peak-safe=%v",
			filepath.Base(w), info.SampleRate, info.Channels, frames, sec, info.BitsPerSample == 16)
	}
}
