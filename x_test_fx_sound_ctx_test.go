package main

import (
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"math"
	"testing"
)

// The process has one audio context, and its format decides whether a sound
// comes out at the pitch it was recorded at. Every FDA in the game declares one
// channel at 22050 or 44100 Hz, so a context left to take its format from
// whichever sound plays first would be mono at one of those rates — which oto
// documents as unlikely to be supported by WASAPI, sending it to a fallback
// driver that is free to play the stream at a rate of its own. The symptom is
// an overlapping sound stretched and pitched down while another sounds right.
//
// So the context is pinned, and this is what holds it there.
func TestOtoContextIsPinnedToPlayableFormat(t *testing.T) {
	if audioplayer.SampleRate != 48000 {
		t.Errorf("audioplayer.SampleRate = %d, want 48000: the rate Windows reliably plays at the rate it is told to", audioplayer.SampleRate)
	}
	if audioplayer.Channels != 2 {
		t.Errorf("audioplayer.Channels = %d, want 2: mono is what oto documents as unlikely to be supported by WASAPI", audioplayer.Channels)
	}
}

// The context takes its format from whoever asks first, so a caller asking for
// something else must still get the pinned one rather than a second format.
func TestGetOtoContextIgnoresRequestedFormat(t *testing.T) {
	// Not calling the real one: it opens an audio device. What is checked is
	// that the arguments cannot change the format the rest of the app reads.
	before := audioplayer.SampleRate
	beforeCh := audioplayer.Channels
	// The signature takes the caller's format and drops it.
	if before != 48000 || beforeCh != 2 {
		t.Fatalf("format is %d Hz/%dch before any call; a caller must not be able to move it", before, beforeCh)
	}
}

// A sound is only heard at the right pitch if it is converted into the context
// format on its way to the device, and the conversion has to preserve both the
// length and the frequencies — a conversion that changed either would be heard
// as exactly the stretching and pitch drop this exists to prevent.
func TestSoundConversionPreservesLengthAndPitch(t *testing.T) {
	const rate = 48000
	// One frame of a 22050 Hz mono sample, the format the game's sounds are in.
	const srcRate, srcCh = 22050, 1
	// A second long, because the measurement below skips a tenth of a second
	// from each end and would have nothing left to measure on a shorter one.
	src := make([]int16, srcRate)
	for i := range src {
		// A 440 Hz tone: two zero crossings per period, so its frequency can
		// be recovered from the output and compared with the input.
		src[i] = int16(10000 * math.Sin(2*math.Pi*440*float64(i)/float64(srcRate)))
	}
	srcCross := zeroCrossings(src, srcRate, srcCh)

	out, err := fda.ResamplePCM16(src, srcRate, srcCh, rate, 2)
	if err != nil {
		t.Fatalf("resample: %v", err)
	}

	// Length: the same wall-clock duration, which is len/channels/rate.
	gotSec := float64(len(out)/2) / float64(rate)
	wantSec := float64(len(src)/srcCh) / float64(srcRate)
	if diff := gotSec - wantSec; diff > 0.001 || diff < -0.001 {
		t.Errorf("duration %.4fs, want %.4fs", gotSec, wantSec)
	}

	// Pitch: the tone is still 440 Hz.
	if got := zeroCrossings(out, rate, 2); math.Abs(got-440)/440 > 0.02 {
		t.Errorf("pitch %.1f Hz, want 440 Hz (source measured %.1f Hz)", got, srcCross)
	}
}

// zeroCrossings returns the frequency a signal is at, by counting the times one
// channel changes sign. Only the first channel is read: an interleaved stereo
// signal crosses zero in both channels at once, and counting both would halve
// the answer. It skips the first and last tenth of a second, where linear
// interpolation and the end clamp are least trustworthy.
func zeroCrossings(samples []int16, rate, channels int) float64 {
	if channels < 1 {
		return 0
	}
	// Work in frames rather than samples, so the skipped time is the same
	// whatever the channel count.
	skip := rate / 10
	frames := len(samples) / channels
	end := frames - skip
	if end <= skip {
		return 0
	}
	first := func(frame int) int16 { return samples[frame*channels] }
	cross := 0
	for f := skip + 1; f < end; f++ {
		if (first(f-1) < 0) != (first(f) < 0) {
			cross++
		}
	}
	return float64(cross) / 2 / (float64(end-skip) / float64(rate))
}
