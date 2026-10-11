package fda

import (
	"encoding/binary"
	"fmt"
	"math"

	"dow-mod-studio/internal/wav"
)

// This file holds the encode half of the Relic audio codec: WAV PCM in, FDA
// bytes out. It is the reverse of decode.go and relic.go, and it shares their
// frame size, tables and FFT.

// Relic Codec encoder - encodes PCM samples to Relic-compressed frames
// This is the reverse of the decoder in relic.go

type relicEncoder struct {
	channels    int
	bitrate     int
	codecRate   int
	frameSize   int
	scales      [relicMaxScales]float32
	dct         [relicMaxSize]float32
	window      [relicMaxSize]float32
	dctMode     int
	samplesMode int
	freqSize    int
	// Overlap state for OLA
	wavePrv [relicMaxChannels][relicMaxSize]float32
}

func relicEncInit(channels, bitrate, codecRate int) *relicEncoder {
	if channels < 1 || channels > relicMaxChannels {
		return nil
	}
	if bitrate < relicMinBitrate || bitrate > relicMaxBitrate {
		return nil
	}

	enc := &relicEncoder{
		channels:  channels,
		bitrate:   bitrate,
		codecRate: codecRate,
		frameSize: bitrate / 8,
	}

	if codecRate < 22050 {
		enc.freqSize = relicSizeLow
	} else if codecRate == 22050 {
		enc.freqSize = relicSizeMid
	} else {
		enc.freqSize = relicSizeHigh
	}

	enc.dctMode = relicSizeHigh
	enc.samplesMode = relicSizeHigh

	relicInitDct(&enc.dct, relicSizeHigh)
	relicInitWindow(&enc.window, relicSizeHigh)
	relicInitDequantization(&enc.scales)

	return enc
}

// Forward DCT - computes frequency coefficients from time-domain samples
// Uses the same DCT structure as the decoder's IDCT, but in reverse
func forwardDCT(wave []float32, freq []float32, dct *[relicMaxSize]float32, dctSize int) {
	dctHalf := dctSize >> 1
	dctQuarter := dctSize >> 2
	dct3Quarter := 3 * dctQuarter

	// Reverse the wave mix from IDCT
	// IDCT does: wave[i] = wave_tmp[dctQuarter + i] for i < dct3Quarter
	//            wave[i] = -wave_tmp[i - dct3Quarter] for i >= dct3Quarter
	waveTmp := make([]float32, relicMaxSize)
	for i := 0; i < dct3Quarter; i++ {
		waveTmp[dctQuarter+i] = wave[i]
	}
	for i := dct3Quarter; i < dctSize; i++ {
		waveTmp[i-dct3Quarter] = -wave[i]
	}

	// Reverse the reorder from IDCT
	// IDCT does: wave_tmp[i*2] = out_re[i], wave_tmp[i*2 + dctHalf] = out_im[i]
	//            wave_tmp[i] = -wave_tmp[dctSize-1-i] for odd i
	outRe := make([]float32, relicMaxFFT)
	outIm := make([]float32, relicMaxFFT)
	for i := 0; i < dctQuarter; i++ {
		outRe[i] = waveTmp[i*2]
		outIm[i] = waveTmp[i*2+dctHalf]
	}

	// Reverse postrotation
	// IDCT does: out_re[i] = (out_re[i]*dct[dctQuarter+i] + out_im[i]*dct[i]) * factor
	//            out_im[i] = (-out_re_i*dct[i] + out_im[i]*dct[dctQuarter+i]) * factor
	factor := float32(8.0 / math.Sqrt(float64(dctSize)))
	inRe := make([]float32, relicMaxFFT)
	inIm := make([]float32, relicMaxFFT)
	for i := 0; i < dctQuarter; i++ {
		// Solve the 2x2 system:
		// outRe[i] = (a*dct[dq+i] + b*dct[i]) * factor
		// outIm[i] = (-a*dct[i] + b*dct[dq+i]) * factor
		// where a = inRe[i], b = inIm[i] (from FFT output)
		// This is: [dct[dq+i] dct[i]] [a]   [outRe[i]/factor]
		//          [-dct[i]  dct[dq+i]] [b] = [outIm[i]/factor]
		dq := dctQuarter + i
		det := dct[dq]*dct[dq] + dct[i]*dct[i]
		if det == 0 {
			continue
		}
		invDet := float32(1.0) / det
		outReNorm := outRe[i] / factor
		outImNorm := outIm[i] / factor
		inRe[i] = (outReNorm*dct[dq] - outImNorm*dct[i]) * invDet
		inIm[i] = (outReNorm*dct[i] + outImNorm*dct[dq]) * invDet
	}

	// Inverse FFT (use forward FFT with conjugate inputs/outputs)
	// For real-valued IDCT, the inverse FFT can be computed using the forward FFT
	// by conjugating the input and output
	n := dctQuarter
	conjInRe := make([]float32, n)
	conjInIm := make([]float32, n)
	for i := 0; i < n; i++ {
		conjInRe[i] = inRe[i]
		conjInIm[i] = -inIm[i] // conjugate
	}
	conjOutRe := make([]float32, n)
	conjOutIm := make([]float32, n)
	RelicMixFFT(n, conjInRe, conjInIm, conjOutRe, conjOutIm)
	// Conjugate output and scale by 1/n
	for i := 0; i < n; i++ {
		inRe[i] = conjOutRe[i] / float32(n)
		inIm[i] = -conjOutIm[i] / float32(n) // conjugate
	}

	// Reverse prerotation
	// IDCT does: inRe[i] = coef1*dct[dq+i] + coef2*dct[i]
	//            inIm[i] = -coef1*dct[i] + coef2*dct[dq+i]
	// where coef1 = freq[2*i]*0.5, coef2 = freq[dctHalf-1-2*i]*0.5
	// Solve for freq:
	// inRe[i] = 0.5*(freq[2*i]*dct[dq+i] + freq[dh-1-2*i]*dct[i])
	// inIm[i] = 0.5*(-freq[2*i]*dct[i] + freq[dh-1-2*i]*dct[dq+i])
	for i := 0; i < dctQuarter; i++ {
		dq := dctQuarter + i
		det := dct[dq]*dct[dq] + dct[i]*dct[i]
		if det == 0 {
			continue
		}
		invDet := float32(1.0) / det
		// a = freq[2*i]*0.5, b = freq[dh-1-2*i]*0.5
		a := (inRe[i]*dct[dq] - inIm[i]*dct[i]) * invDet
		b := (inRe[i]*dct[i] + inIm[i]*dct[dq]) * invDet
		freq[2*i] = a * 2.0
		freq[dctHalf-1-2*i] = b * 2.0
	}
}

// quantizeAndPackFrame encodes one frame of frequency data into a bitstream
// Returns the packed frame bytes
func quantizeAndPackFrame(freq1, freq2 []float32, scales *[relicMaxScales]float32, freqSize int, isMonoCopy bool) []byte {
	freqHalf := freqSize >> 1

	// Determine flags
	var flags uint8 = 0
	if isMonoCopy {
		flags |= 2 // clone freq2 from freq1
	}

	// Determine exponents for each frequency bin
	exponents1 := make([]uint8, relicMaxFreq)
	exponents2 := make([]uint8, relicMaxFreq)
	assignExponents(freq1[:freqHalf], exponents1, scales)
	if !isMonoCopy {
		assignExponents(freq2[:freqHalf], exponents2, scales)
	}

	// Choose cb_bits, ev_bits, ei_bits based on data
	cbBits := determineCBBits(exponents1)
	evBits := determineEvBits(exponents1)
	eiBits := determineEIBits(exponents1)

	// Build bitstream
	var bits []byte
	bits = appendBits(bits, uint32(flags), 2)
	bits = appendBits(bits, uint32(cbBits), 3)
	bits = appendBits(bits, uint32(evBits), 2)
	bits = appendBits(bits, uint32(eiBits), 4)

	// Pack critical band exponents
	bits = packCriticalBandExponents(bits, exponents1, cbBits, evBits)

	// Pack quantized values for freq1
	bits = packQuantizedValues(bits, freq1[:freqHalf], exponents1, eiBits, scales)

	// Pack quantized values for freq2 (or skip if mono copy)
	if !isMonoCopy {
		bits = packQuantizedValues(bits, freq2[:freqHalf], exponents2, eiBits, scales)
	}

	// Pad to frame size (padded with zeros to relicBufferSize)
	frame := make([]byte, relicBufferSize)
	copy(frame, bits)
	return frame
}

// assignExponents determines the best exponent (qv_bits) for each frequency bin
func assignExponents(freq []float32, exponents []uint8, scales *[relicMaxScales]float32) {
	for i := range exponents {
		exponents[i] = 0
	}
	for i := 0; i < len(freq) && i < relicMaxFreq; i++ {
		if freq[i] == 0 {
			continue
		}
		absVal := float32(math.Abs(float64(freq[i])))
		// Find smallest exponent that can represent this value
		for exp := uint8(0); exp < relicMaxScales; exp++ {
			maxVal := scales[exp] * float32(int32(1<<(uint(exp)+1))-1) // max representable
			if absVal <= maxVal {
				exponents[i] = exp
				break
			}
		}
	}
}

func determineCBBits(exponents []uint8) int {
	// Use 3 bits for critical band position (covers all 27 bands)
	return 3
}

func determineEvBits(exponents []uint8) int {
	// Check if all exponents fit in fewer bits
	maxExp := uint8(0)
	for _, e := range exponents {
		if e > maxExp {
			maxExp = e
		}
	}
	if maxExp <= 1 {
		return 1
	}
	return 2
}

func determineEIBits(exponents []uint8) int {
	// ei_bits determines how many bits for position delta in value packing
	// Use 4 bits to cover all 256 frequency bins
	return 4
}

// packCriticalBandExponents packs exponents grouped by critical bands
func packCriticalBandExponents(bits []byte, exponents []uint8, cbBits, evBits int) []byte {
	// Group exponents by critical bands and find the dominant exponent per band
	prevPos := 0
	for i := 0; i < relicCriticalBandCount-1; i++ {
		start := int(criticalBandData[i])
		end := int(criticalBandData[i+1])
		if start >= relicMaxFreq {
			break
		}
		if end > relicMaxFreq {
			end = relicMaxFreq
		}

		// Find dominant exponent in this band
		dominantExp := uint8(0)
		for j := start; j < end; j++ {
			if exponents[j] > dominantExp {
				dominantExp = exponents[j]
			}
		}

		// Position delta from previous band
		delta := i - prevPos
		if delta < 0 {
			delta = 0
		}

		// Pack position delta
		bits = appendBits(bits, uint32(delta), cbBits)
		// Pack exponent value
		bits = appendBits(bits, uint32(dominantExp), evBits)

		// Apply exponent to all bins in this band
		for j := start; j < end; j++ {
			exponents[j] = dominantExp
		}

		prevPos = i
	}

	// Terminator (delta = 0)
	bits = appendBits(bits, 0, cbBits)

	return bits
}

// packQuantizedValues packs the quantized frequency values
func packQuantizedValues(bits []byte, freq []float32, exponents []uint8, eiBits int, scales *[relicMaxScales]float32) []byte {
	prevPos := 0
	for i := 0; i < len(freq) && i < relicMaxFreq; i++ {
		if freq[i] == 0 {
			continue
		}

		// Position delta
		delta := i - prevPos
		if delta < 0 {
			delta = 0
		}

		// Pack position delta
		bits = appendBits(bits, uint32(delta), eiBits)

		// Quantize value
		exp := exponents[i]
		if exp >= relicMaxScales {
			exp = relicMaxScales - 1
		}
		scale := scales[exp]
		qv := int32(math.Round(float64(freq[i] / scale)))

		// Clamp to signed range for (exp+2) bits
		maxQV := int32((1 << (exp + 1)) - 1)
		minQV := int32(-(1 << (exp + 1)))
		if qv > maxQV {
			qv = maxQV
		}
		if qv < minQV {
			qv = minQV
		}

		// Pack as signed value with (exp+2) bits
		bits = appendSignedBits(bits, qv, int(exp)+2)

		prevPos = i
	}

	// Terminator (delta = 0)
	bits = appendBits(bits, 0, eiBits)

	return bits
}

// appendBits appends unsigned bits to the bitstream (LSB first)
func appendBits(data []byte, val uint32, nbits int) []byte {
	for i := 0; i < nbits; i++ {
		bit := (val >> uint(i)) & 1
		// Find the bit position
		bitPos := len(data) * 8
		byteIdx := bitPos / 8
		bitIdx := bitPos % 8

		// Extend data if needed
		for byteIdx >= len(data) {
			data = append(data, 0)
		}

		if bit == 1 {
			data[byteIdx] |= 1 << uint(bitIdx)
		}
	}
	return data
}

// appendSignedBits appends a signed value as unsigned bits (LSB first)
func appendSignedBits(data []byte, val int32, nbits int) []byte {
	// Convert to unsigned for storage
	var uval uint32
	if val < 0 {
		// Two's complement
		uval = uint32(val) & ((1 << uint(nbits)) - 1)
	} else {
		uval = uint32(val)
	}
	return appendBits(data, uval, nbits)
}

// EncodeWAV encodes a parsed PCM WAV into an FDA byte stream at bitrate
// (bits per second per channel). The container is written around the packed
// frames so the result parses as a complete .fda file.
func EncodeWAV(w *wav.Info, bitrate int) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("no WAV data")
	}
	if w.Channels < 1 || w.Channels > relicMaxChannels {
		return nil, fmt.Errorf("unsupported WAV channel count %d", w.Channels)
	}
	if bitrate < relicMinBitrate || bitrate > relicMaxBitrate {
		return nil, fmt.Errorf("unsupported FDA bitrate %d", bitrate)
	}
	channels := w.Channels
	channelSamples := make([][]float32, channels)
	frames := len(w.PCMData) / (channels * 2)
	for ch := range channelSamples {
		channelSamples[ch] = make([]float32, frames)
	}
	for frame := 0; frame < frames; frame++ {
		for ch := 0; ch < channels; ch++ {
			off := (frame*channels + ch) * 2
			channelSamples[ch][frame] = float32(int16(binary.LittleEndian.Uint16(w.PCMData[off:off+2]))) / 32768.0
		}
	}
	encoder := relicEncInit(channels, bitrate, w.SampleRate)
	if encoder == nil {
		return nil, fmt.Errorf("failed to initialize Relic encoder")
	}
	framesPerChannel := frames / relicSamplesPerFrame
	all := make([]byte, 0, framesPerChannel*encoder.frameSize*channels)
	for i := 0; i < framesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			samples := make([]float32, relicSamplesPerFrame)
			start := i * relicSamplesPerFrame
			copy(samples, channelSamples[ch][start:])
			for j := range samples {
				samples[j] *= 32768.0
			}
			freq1 := make([]float32, relicMaxFreq)
			forwardDCT(samples, freq1, &encoder.dct, relicSizeHigh)
			packed := quantizeAndPackFrame(freq1, freq1, &encoder.scales, encoder.freqSize, channels > 1)
			all = append(all, packed[:encoder.frameSize]...)
		}
	}
	return buildFDABytes(channels, bitrate, w.SampleRate, all), nil
}

func buildFDABytes(channels, bitrate, sampleRate int, data []byte) []byte {
	info := INFOChunk{Channels: uint32(channels), SampleSize: 16, BlockBitrate: uint32(bitrate), SampleRate: uint32(sampleRate), EndLoop: 0xFFFFFFFF}
	out := make([]byte, 0, 128+len(data))
	out = append(out, []byte("Relic Chunky")...)
	out = append(out, 0x0d, 0x0a, 0x1a, 0x00)
	out = appendUint32(out, 1)
	out = appendUint32(out, 1)
	appendFDAHeader := func(kind, id string, size uint32, name string) {
		out = append(out, []byte(kind)...)
		out = append(out, []byte(id)...)
		out = appendUint32(out, 1)
		out = appendUint32(out, size)
		out = appendUint32(out, uint32(len(name)))
		out = append(out, []byte(name)...)
	}
	// FBIF has a small legacy metadata payload; its exact values are not used
	// by the codec, but keeping a valid chunk makes the FDA readable by tools.
	fbifName := "FileBurnInfo\x00"
	appendFDAHeader("DATA", "FBIF", 0, fbifName)
	pluginName, userName, burnTime := "RAW to FDA", "dow-mod-studio-go", "dow-mod-studio-go"
	out = appendUint32(out, uint32(len(pluginName)))
	out = append(out, []byte(pluginName)...)
	out = appendUint32(out, 1)
	out = appendUint32(out, uint32(len(userName)))
	out = append(out, []byte(userName)...)
	out = appendUint32(out, uint32(len(burnTime)))
	out = append(out, []byte(burnTime)...)
	fdaSize := uint32(28 + (4 + len(data)) + 40)
	appendFDAHeader("FOLD", "FDA ", fdaSize, "")
	appendFDAHeader("DATA", "INFO", 28, "")
	out = appendUint32(out, info.Channels)
	out = appendUint32(out, info.SampleSize)
	out = appendUint32(out, info.BlockBitrate)
	out = appendUint32(out, info.SampleRate)
	out = appendUint32(out, info.BeginLoop)
	out = appendUint32(out, info.EndLoop)
	out = appendUint32(out, info.StartOffset)
	appendFDAHeader("DATA", "DATA", uint32(4+len(data)), "")
	out = appendUint32(out, uint32(len(data)))
	out = append(out, data...)
	return out
}

func appendUint32(dst []byte, v uint32) []byte {
	return append(dst, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
