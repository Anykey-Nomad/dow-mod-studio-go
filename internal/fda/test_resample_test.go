package fda

import (
	"math"
	"testing"
)

func TestResamplePCM16Rate(t *testing.T) {
	// Upsampling 2 -> 4 Hz puts two output samples between each pair of input
	// ones. What matters is that the conversion doubles the count and keeps the
	// endpoints; the values in between are the kernel's business and are
	// measured spectrally in fx_resample_quality_test.go.
	//
	// The signal is longer than the kernel on purpose. A 32-tap kernel run over
	// a two-sample ramp is mostly clamped to the edges, and what comes out is the
	// shape of the truncation rather than of the signal.
	const n = 64
	in := make([]int16, n)
	for i := range in {
		in[i] = int16(i * 100)
	}
	got, err := ResamplePCM16(in, 2, 1, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2*n {
		t.Fatalf("length: got %d, want %d", len(got), 2*n)
	}
	// The endpoints come back within a few least significant bits rather than
	// exactly: a lowpass kernel reproduces a straight line to about -70 dB, and
	// at the last sample part of the kernel is past the end of the signal and
	// held there. Holding is the right choice — it neither fades the sound out
	// nor clicks it off — but it does mean the edge is approximate.
	const edgeTolerance = 16
	if d := pcmAbsDiff(int(got[0]), 0); d > edgeTolerance {
		t.Errorf("first sample: got %d, want 0 (off by %d)", got[0], d)
	}
	if d := pcmAbsDiff(int(got[len(got)-1]), int(in[n-1])); d > edgeTolerance {
		t.Errorf("last sample: got %d, want %d (off by %d)", got[len(got)-1], in[n-1], d)
	}
	// A ramp must come out a ramp: the kernel reproduces a straight line, so any
	// dip means the conversion is distorting rather than interpolating.
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Errorf("sample %d: %d dips below %d, the ramp is not preserved", i, got[i], got[i-1])
			break
		}
	}
}

func pcmAbsDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func TestResamplePCM16Channels(t *testing.T) {
	stereo, err := ResamplePCM16([]int16{100, 300}, 1, 2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(stereo) != 1 || stereo[0] != 200 {
		t.Fatalf("unexpected stereo downmix: %#v", stereo)
	}
	mono, err := ResamplePCM16([]int16{100, 300}, 1, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(mono) != 4 || mono[0] != 100 || mono[1] != 100 || mono[2] != 300 || mono[3] != 300 {
		t.Fatalf("unexpected mono upmix: %#v", mono)
	}
}

// A conversion that changes nothing but the channel count must not filter: at
// an unchanged rate the samples are the samples, and any filtering would be an
// audible change nobody asked for.
func TestResampleSameRateIsNotFiltered(t *testing.T) {
	in := []int16{0, 1000, -1000, 32767, -32768, 7}
	out, err := ResamplePCM16(in, 44100, 1, 44100, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("length: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("sample %d: got %d, want %d unchanged", i, out[i], in[i])
		}
	}
}

// An empty or single-sample signal must not panic or divide by zero.
func TestResampleDegenerate(t *testing.T) {
	for _, in := range [][]int16{nil, {0}, {5, 5, 5}} {
		if _, err := ResamplePCM16(in, 22050, 1, 44100, 1); err != nil {
			t.Errorf("%v: %v", in, err)
		}
	}
	// Nonsense formats are refused rather than guessed at.
	if _, err := ResamplePCM16([]int16{1, 2}, 0, 1, 44100, 1); err == nil {
		t.Error("zero input rate was accepted")
	}
	if _, err := ResamplePCM16([]int16{1, 2}, 22050, 3, 44100, 1); err == nil {
		t.Error("three channels were accepted")
	}
	if _, err := ResamplePCM16([]int16{1, 2, 3}, 22050, 2, 44100, 1); err == nil {
		t.Error("a sample count not divisible by the channels was accepted")
	}
}

// Every sound in the game is mono at 22050 or 44100, and the context is at
// neither, so these two conversions are the ones that run in practice.
func TestResampleGameFormats(t *testing.T) {
	for _, rate := range []int{22050, 44100} {
		in := make([]int16, rate/10)
		for i := range in {
			in[i] = int16(8000 * probeSine(300, i, rate))
		}
		out, err := ResamplePCM16(in, rate, 1, 48000, 2)
		if err != nil {
			t.Fatalf("%d Hz: %v", rate, err)
		}
		if len(out) == 0 {
			t.Fatalf("%d Hz: empty output", rate)
		}
		// Duration must survive the trip through the context format.
		got := float64(len(out)/2) / 48000
		want := float64(len(in)) / float64(rate)
		if diff := got - want; diff > 0.002 || diff < -0.002 {
			t.Errorf("%d Hz: duration %.4fs, want %.4fs", rate, got, want)
		}
	}
}

// probeSine is one sample of a sine, the shared shape the spectral assertions
// measure against; the root's quality test keeps its own copy.
func probeSine(hz float64, i, rate int) float64 {
	return math.Sin(2 * math.Pi * hz * float64(i) / float64(rate))
}
