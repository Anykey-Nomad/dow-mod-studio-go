package main

import (
	"dow-mod-studio/internal/fda"
	"math"
	"testing"
)

// A resampler is judged on the spectrum, not on the samples: a conversion can
// keep every sample in range, keep the duration, and still be wrong. What is
// audible on a sound effect is two things —
//
//   - imaging: doubling the rate duplicates everything above the original
//     Nyquist, folded back down into the audible band. That is the thin metallic
//     buzz that makes a resampled effect sound synthetic.
//   - passband droop: losing the top of the band as the frequency approaches
//     the corner, so a blade or a footfall arrives dull and quiet.
//
// Linear interpolation measured -17 dB of imaging at the image of a 5 kHz tone
// and lost 4.8 dB at 10 kHz. These are the numbers the replacement has to beat,
// and they are kept here so a future change to the resampler cannot quietly put
// them back.

// probeSine is a pure tone, which is what makes a spectral measurement
// meaningful: the only frequencies in it are the tone and whatever the
// conversion adds.
func probeSine(hz float64, i, rate int) float64 {
	return math.Sin(2 * math.Pi * hz * float64(i) / float64(rate))
}

// goertzel returns the magnitude of one frequency in a signal, by running the
// Goertzel recurrence. One pass, no transform, and it reads one exact bin,
// which is all a measurement of "is there energy at this frequency" needs.
func goertzel(s []int16, rate int, hz float64) float64 {
	if len(s) == 0 {
		return 0
	}
	w := 2 * math.Pi * hz / float64(rate)
	c := 2 * math.Cos(w)
	var q1, q2 float64
	for _, v := range s {
		q0 := float64(v) + c*q1 - q2
		q2, q1 = q1, q0
	}
	return math.Sqrt(q1*q1+q2*q2-c*q1*q2) / float64(len(s))
}

func resampleTone(t *testing.T, hz float64, inRate, outRate int) ([]int16, []int16) {
	t.Helper()
	// One second of tone, which is long enough that the kernel's edges are a
	// negligible part of the measurement.
	in := make([]int16, inRate)
	for i := range in {
		in[i] = int16(10000 * probeSine(hz, i, inRate))
	}
	out, err := fda.ResamplePCM16(in, inRate, 1, outRate, 1)
	if err != nil {
		t.Fatalf("%d -> %d Hz: %v", inRate, outRate, err)
	}
	return in, out
}

// Upsampling by two is the conversion the game's 22050 Hz sounds need, and the
// one linear interpolation was worst at.
func TestFxResampleRejectsImaging(t *testing.T) {
	const inRate, outRate = 22050, 44100
	const toneHz = 5000.0

	_, out := resampleTone(t, toneHz, inRate, outRate)
	signal := goertzel(out, outRate, toneHz)
	if signal <= 0 {
		t.Fatal("the tone itself is missing from the output")
	}
	// Doubling the rate puts images at k*inRate +/- tone.
	images := []float64{
		float64(inRate) - toneHz,
		float64(inRate) + toneHz,
	}
	for _, img := range images {
		if img >= outRate/2 {
			continue
		}
		m := goertzel(out, outRate, img)
		if m <= 0 {
			t.Errorf("image at %.0f Hz: no measurable level", img)
			continue
		}
		rel := 20 * math.Log10(m/signal)
		t.Logf("image at %7.0f Hz: %.1f dB relative to the signal", img, rel)
		if rel > -60 {
			t.Errorf("image at %.0f Hz is %.1f dB relative to the signal, want under -60 dB", img, rel)
		}
	}
}

// The level of the signal itself must not change across the band it carries.
// Linear interpolation lost 4.8 dB by 10 kHz.
func TestFxResamplePassbandIsFlat(t *testing.T) {
	const inRate, outRate = 22050, 44100
	for _, hz := range []float64{100, 1000, 5000, 8000, 10000} {
		in, out := resampleTone(t, hz, inRate, outRate)
		before := goertzel(in, inRate, hz)
		after := goertzel(out, outRate, hz)
		if before <= 0 || after <= 0 {
			t.Fatalf("%.0f Hz: tone missing (in %v, out %v)", hz, before, after)
		}
		d := 20 * math.Log10(after/before)
		t.Logf("%6.0f Hz: %+.2f dB", hz, d)
		if d < -1.0 || d > 1.0 {
			t.Errorf("%.0f Hz changed by %+.2f dB, want within 1 dB", hz, d)
		}
	}
}

// The kernel must not invent energy where the source had none, or the
// conversion is adding noise of its own.
func TestFxResampleAddsNoEnergyAboveSourceNyquist(t *testing.T) {
	const inRate, outRate = 22050, 44100
	// A tone above the source's own Nyquist, aliased into it by the source
	// itself. The converter must not add anything more on top.
	for _, hz := range []float64{13000, 15000} {
		in, out := resampleTone(t, hz, inRate, outRate)
		before := goertzel(in, inRate, hz)
		after := goertzel(out, outRate, hz)
		if before <= 0 || after <= 0 {
			continue
		}
		d := 20 * math.Log10(after/before)
		if d > 1.0 {
			t.Errorf("%.0f Hz grew by %+.2f dB through the conversion", hz, d)
		}
	}
}

// The length of a sample must not depend on how it is converted, or a sound
// drifts out of step with the animation that triggers it.
func TestFxResampleKeepsDuration(t *testing.T) {
	for _, inRate := range []int{22050, 44100} {
		in := make([]int16, inRate)
		for i := range in {
			in[i] = int16(9000 * probeSine(440, i, inRate))
		}
		for _, outRate := range []int{22050, 44100, 48000} {
			out, err := fda.ResamplePCM16(in, inRate, 1, outRate, 2)
			if err != nil {
				t.Fatalf("%d -> %d: %v", inRate, outRate, err)
			}
			got := float64(len(out)/2) / float64(outRate)
			want := float64(len(in)) / float64(inRate)
			if math.Abs(got-want) > 0.002 {
				t.Errorf("%d -> %d Hz: duration %.4fs, want %.4fs", inRate, outRate, got, want)
			}
		}
	}
}

// The pitch of a tone must survive the conversion. A resampler that changed the
// frequency would be the stretched, pitched-down sound this whole path exists to
// avoid.
func TestFxResampleKeepsPitch(t *testing.T) {
	const toneHz = 1000.0
	for _, c := range [][2]int{{22050, 44100}, {22050, 48000}, {44100, 48000}, {48000, 44100}} {
		_, out := resampleTone(t, toneHz, c[0], c[1])
		// zeroCrossings works in frames; the output here is mono.
		got := zeroCrossings(out, c[1], 1)
		if math.Abs(got-toneHz)/toneHz > 0.02 {
			t.Errorf("%d -> %d Hz: pitch %.1f Hz, want %.0f Hz", c[0], c[1], got, toneHz)
		}
	}
}
