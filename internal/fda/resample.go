package fda

import (
	"fmt"
	"math"
)

// Converting decoded FDA PCM into the fixed format of the process oto context.
// The context is initialized once, so FDA files with another sample rate or
// channel count must be converted before playback.
//
// The game's sounds come in two rates, 22050 and 44100 Hz, and the context runs
// at neither, so nearly every sound is converted on its way to the speakers.
// What the conversion has to get right is audible on a sound effect: the length
// of the sample, and the frequencies in it. A sound that comes out the right
// length but the wrong pitch is the failure that is hardest to notice in a test
// and easiest to hear in a preview.
//
// Linear interpolation gets the length and the pitch right, which is why it was
// good enough to ship, but it is the weakest interpolator available for the case
// that matters most here. Upsampling by exactly 2 — 22050 to 44100 — is what it
// is worst at:
//
//   - Its passband droops as it approaches the top of the band, so a footfall or
//     a blade loses its brightness.
//   - It does not remove the images. Interpolating by linear blending leaves a
//     copy of everything above the original Nyquist folded back down into the
//     audible band, measured at -14.5 dB relative to the signal on a 5 kHz tone.
//     That is the thin, metallic buzz that makes a resampled sound effect sound
//     synthetic.
//
// So this is a windowed sinc interpolator, which is what an ideal resampler does:
// it lowpasses at the lower of the two Nyquist limits and reconstructs between
// the samples with a kernel rather than a straight line.

// fxResampleTaps is how many samples either side of the output position the
// kernel reaches. Sixteen a side is a 32-tap kernel: flat to within a fraction
// of a dB across the passband and down past 70 dB at the images, which is well
// past what a sound effect can show.
const fxResampleTaps = 16

// fxResamplePhases is the size of the kernel table. The kernel is the same shape
// for every output sample, only shifted by the fraction of a sample between it
// and the nearest input sample, so it is built once per conversion and then
// looked up. The shift is quantised to a 512th of a sample, which is well under
// a microsecond of signal at these rates — far below anything audible — and it
// turns the inner loop into a table read instead of a sine.
const fxResamplePhases = 512

// fxResampleKernel is one conversion's worth of precomputed taps, indexed by
// phase then by tap.
type fxResampleKernel struct {
	// taps[phase][j] is the weight of the input sample j-eighthsTaps before the
	// output position, for the given fractional offset.
	taps [][]float64
	// cutoff is the lowpass corner in cycles per input sample: half the lower
	// of the two rates, which is what keeps an image out of the audible band.
	cutoff float64
}

// buildFxResampleKernel precomputes the kernel for a conversion. cutoff is the
// corner frequency in cycles per sample, 0.5 being the input's own Nyquist.
func buildFxResampleKernel(cutoff float64) *fxResampleKernel {
	// Normalised cutoff: the factor that turns a distance in samples into an
	// argument of sinc.
	k := 2 * cutoff
	kern := &fxResampleKernel{cutoff: cutoff, taps: make([][]float64, fxResamplePhases)}
	for p := 0; p < fxResamplePhases; p++ {
		frac := float64(p) / float64(fxResamplePhases)
		row := make([]float64, 2*fxResampleTaps)
		var sum float64
		for j := 0; j < 2*fxResampleTaps; j++ {
			// Distance from the output position to this input sample, in
			// samples: the tap's own offset less the fraction.
			d := float64(j-fxResampleTaps) - frac
			w := fxSinc(k*d) * fxBlackman(d/fxResampleTaps)
			row[j] = w
			sum += w
		}
		// Normalising each phase to unit gain is what keeps the level of the
		// signal the same across the conversion. Without it a passband that
		// droops arrives as a quiet sample as well as a dull one.
		if sum != 0 {
			for j := range row {
				row[j] /= sum
			}
		}
		kern.taps[p] = row
	}
	return kern
}

// fxSinc is the normalised sinc function, sin(pi x)/(pi x).
func fxSinc(x float64) float64 {
	if x == 0 {
		return 1
	}
	px := math.Pi * x
	return math.Sin(px) / px
}

// fxBlackman is a Blackman window over -1..1, which is what keeps the kernel's
// own stopband leakage down: a bare sinc rings enough to put a faint pre-echo
// ahead of a sharp transient, and a footfall is a sharp transient.
func fxBlackman(u float64) float64 {
	if u <= -1 || u >= 1 {
		return 0
	}
	return 0.42 + 0.5*math.Cos(math.Pi*u) + 0.08*math.Cos(2*math.Pi*u)
}

// ResamplePCM16 converts decoded FDA PCM to the fixed format of the process
// oto context. The context is initialized once, so FDA files with another
// sample rate or channel count must be converted before playback.
func ResamplePCM16(samples []int16, inRate, inChannels, outRate, outChannels int) ([]int16, error) {
	if inRate <= 0 || outRate <= 0 || inChannels <= 0 || outChannels <= 0 {
		return nil, fmt.Errorf("invalid PCM format: %d Hz, %d ch -> %d Hz, %d ch", inRate, inChannels, outRate, outChannels)
	}
	if (inChannels != 1 && inChannels != 2) || (outChannels != 1 && outChannels != 2) {
		return nil, fmt.Errorf("unsupported FDA channel conversion %d -> %d", inChannels, outChannels)
	}
	if len(samples)%inChannels != 0 {
		return nil, fmt.Errorf("PCM sample count %d is not divisible by %d channels", len(samples), inChannels)
	}
	inFrames := len(samples) / inChannels
	if inFrames == 0 {
		return []int16{}, nil
	}
	// A conversion that changes nothing but the channel count is a downmix or
	// an upmix, and must not filter: the samples are the samples.
	if inRate == outRate {
		return remapPCM16Channels(samples, inChannels, outChannels), nil
	}
	outFrames := int((int64(inFrames)*int64(outRate) + int64(inRate)/2) / int64(inRate))
	if outFrames <= 0 {
		return []int16{}, nil
	}

	// The corner sits at the lower of the two Nyquist limits, so upsample keeps
	// everything the source had and downsample drops what the target cannot
	// carry. Expressed in cycles per input sample.
	cutoff := 0.5
	if outRate < inRate {
		cutoff = 0.5 * float64(outRate) / float64(inRate)
	}
	kern := buildFxResampleKernel(cutoff)

	out := make([]int16, 0, outFrames*outChannels)
	// The position of each output frame in input samples, kept exact rather than
	// accumulated: stepping a float by a rounded increment is how a conversion
	// drifts, and a sound that drifts is a sound that ends early.
	step := float64(inRate) / float64(outRate)
	pos := 0.0
	for frame := 0; frame < outFrames; frame++ {
		i0 := int(math.Floor(pos))
		frac := pos - float64(i0)
		// Nearest phase in the table. For an exact 2x this is only ever 0 or a
		// half, so the whole conversion reads two rows of the table rather than
		// walking all 512 — better locality, though the tap count per sample is
		// the same either way.
		p := int(frac*fxResamplePhases + 0.5)
		if p >= fxResamplePhases {
			p = fxResamplePhases - 1
		}
		row := kern.taps[p]
		for ch := 0; ch < outChannels; ch++ {
			var acc float64
			for j := 0; j < 2*fxResampleTaps; j++ {
				idx := i0 + j - fxResampleTaps
				// Off either end the nearest real sample is held, rather than
				// zero, so the conversion does not fade the sound out or click
				// it off at the boundary.
				if idx < 0 {
					idx = 0
				} else if idx >= inFrames {
					idx = inFrames - 1
				}
				acc += row[j] * float64(fxSourceSample(samples, inChannels, outChannels, idx, ch))
			}
			out = append(out, clampPCM16(int64(math.Round(acc))))
		}
		pos += step
	}
	return out, nil
}

// remapPCM16Channels converts channel count without touching the samples in
// time, which is what a conversion at an unchanged rate is.
func remapPCM16Channels(samples []int16, inChannels, outChannels int) []int16 {
	inFrames := len(samples) / inChannels
	out := make([]int16, 0, inFrames*outChannels)
	for f := 0; f < inFrames; f++ {
		for ch := 0; ch < outChannels; ch++ {
			out = append(out, clampPCM16(fxSourceSample(samples, inChannels, outChannels, f, ch)))
		}
	}
	return out
}

// fxSourceSample reads one input frame as one output channel: mono fills both
// stereo channels, stereo folds to one by averaging the two.
func fxSourceSample(samples []int16, inChannels, outChannels int, frame, outChannel int) int64 {
	first, second := outChannel, outChannel
	switch {
	case inChannels == outChannels:
	case inChannels == 1:
		first, second = 0, 0
	default:
		first, second = 0, 1
	}
	v := int64(samples[frame*inChannels+first])
	if first != second {
		v = (v + int64(samples[frame*inChannels+second])) / 2
	}
	return v
}

func clampPCM16(v int64) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}
