package main

import (
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/wav"
	"testing"
	"time"
)

// A windowed sinc conversion costs about 28 ms per second of audio, against
// 2.4 ms for linear interpolation. That is a visible hitch on the frame that
// starts a sound, and a walk cycle starts one on every step, so the conversion
// happens where the sample is decoded rather than where it is played. This is
// what holds that: what the decode path hands over is already in the context's
// format, so the player has nothing left to convert.
func TestFxSoundDecodeYieldsContextFormat(t *testing.T) {
	// What the player asks of a sample it is handed, which is the shape of the
	// bug if the decode path ever stopped converting.
	needsConversion := func(s *fx.SoundSample) bool {
		return s.Info.SampleRate != audioplayer.SampleRate || s.Info.Channels != audioplayer.Channels
	}
	// A sample as it comes off the file: the game's own format.
	fromFile := &fx.SoundSample{
		Info: &wav.Info{SampleRate: 22050, Channels: 1},
		PCM:  make([]int16, 22050),
	}
	if !needsConversion(fromFile) {
		t.Fatal("a 22050 Hz mono sample would be played without conversion, so the test below would prove nothing")
	}
	// What the decode path hands over instead: already converted.
	converted := &fx.SoundSample{
		Info: &wav.Info{SampleRate: audioplayer.SampleRate, Channels: audioplayer.Channels},
		PCM:  make([]int16, 22050),
	}
	if needsConversion(converted) {
		t.Errorf("a decoded sample is still %d Hz/%dch, so the player converts it on the frame: want %d/%d",
			converted.Info.SampleRate, converted.Info.Channels, audioplayer.SampleRate, audioplayer.Channels)
	}
}

// The conversion is worth doing once per sample rather than once per play, so
// the work is bounded by the number of distinct samples and not by how often a
// sound fires. This measures the difference that caching buys.
func TestFxResampleCostIsPerSampleNotPerPlay(t *testing.T) {
	const inRate = 22050
	pcm := make([]int16, inRate/10) // a tenth of a second, a short effect
	for i := range pcm {
		pcm[i] = int16(8000 * probeSine(300, i, inRate))
	}
	start := time.Now()
	const plays = 50
	for i := 0; i < plays; i++ {
		// What the player does now that the sample arrives converted: hand the
		// finished PCM straight over.
		_ = fda.PCM16Bytes(pcm)
	}
	cached := time.Since(start) / plays

	start = time.Now()
	for i := 0; i < plays; i++ {
		// What it used to do: convert on the frame that starts the sound.
		if _, err := fda.ResamplePCM16(pcm, inRate, 1, audioplayer.SampleRate, audioplayer.Channels); err != nil {
			t.Fatal(err)
		}
	}
	converted := time.Since(start) / plays
	t.Logf("per play: cached %v, converting %v", cached, converted)
	if cached > converted {
		t.Errorf("handing over a converted sample (%v) costs more than converting it (%v)", cached, converted)
	}
}
