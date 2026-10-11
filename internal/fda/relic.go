package fda

import (
	"math"
)

// Port of relic_lib.c from vgmstream
// Relic Codec decoder, a mono-interleave DCT-based codec.
// Decompiled from Relic's dec.exe

const (
	relicBufferSize        = 0x104
	relicSamplesPerFrame   = 512
	relicMaxChannels       = 2
	relicMaxScales         = 6
	relicBaseScale         = 10.0
	relicFreqMaskFactor    = 1.0
	relicCriticalBandCount = 27
	relicPi                = 3.14159265358979323846
	relicSizeLow           = 128
	relicSizeMid           = 256
	relicSizeHigh          = 512
	relicMaxSize           = relicSizeHigh
	relicMaxFreq           = relicMaxSize / 2
	relicMaxFFT            = relicMaxSize / 4
	relicMinBitrate        = 256
	relicMaxBitrate        = 2048
)

var criticalBandData = [relicCriticalBandCount]int16{
	0, 1, 2, 3, 4, 5, 6, 7,
	9, 11, 13, 15, 17, 20, 23, 27,
	31, 37, 43, 51, 62, 74, 89, 110,
	139, 180, 256,
}

type relicHandle struct {
	channels    int
	frameSize   int
	waveSize    int
	freqSize    int
	dctMode     int
	samplesMode int
	scales      [relicMaxScales]float32
	dct         [relicMaxSize]float32
	window      [relicMaxSize]float32
	exponents   [relicMaxChannels][relicMaxFreq]uint8
	freq1       [relicMaxFreq]float32
	freq2       [relicMaxFreq]float32
	waveCur     [relicMaxChannels][relicMaxSize]float32
	wavePrv     [relicMaxChannels][relicMaxSize]float32
}

func relicInitDct(dct *[relicMaxSize]float32, dctSize int) {
	dctQuarter := dctSize >> 2
	for i := 0; i < dctQuarter; i++ {
		temp := (float64(i) + 0.125) * (relicPi * 2.0) * (1.0 / float64(dctSize))
		dct[i] = float32(math.Sin(temp))
		dct[dctQuarter+i] = float32(math.Cos(temp))
	}
}

func relicApplyIdct(freq []float32, wave []float32, dct *[relicMaxSize]float32, dctSize int) {
	inRe := make([]float32, relicMaxFFT)
	inIm := make([]float32, relicMaxFFT)
	outRe := make([]float32, relicMaxFFT)
	outIm := make([]float32, relicMaxFFT)
	waveTmp := make([]float32, relicMaxSize)

	dctHalf := dctSize >> 1
	dctQuarter := dctSize >> 2
	dct3Quarter := 3 * (dctSize >> 2)

	// prerotation
	for i := 0; i < dctQuarter; i++ {
		coef1 := freq[2*i] * 0.5
		coef2 := freq[dctHalf-1-2*i] * 0.5
		inRe[i] = coef1*dct[dctQuarter+i] + coef2*dct[i]
		inIm[i] = -coef1*dct[i] + coef2*dct[dctQuarter+i]
	}

	// main FFT
	RelicMixFFT(dctQuarter, inRe, inIm, outRe, outIm)

	// postrotation, window and reorder
	factor := float32(8.0 / math.Sqrt(float64(dctSize)))
	for i := 0; i < dctQuarter; i++ {
		outReI := outRe[i]
		outRe[i] = (outRe[i]*dct[dctQuarter+i] + outIm[i]*dct[i]) * factor
		outIm[i] = (-outReI*dct[i] + outIm[i]*dct[dctQuarter+i]) * factor
		waveTmp[i*2] = outRe[i]
		waveTmp[i*2+dctHalf] = outIm[i]
	}
	for i := 1; i < dctSize; i += 2 {
		waveTmp[i] = -waveTmp[dctSize-1-i]
	}

	// wave mix
	for i := 0; i < dct3Quarter; i++ {
		wave[i] = waveTmp[dctQuarter+i]
	}
	for i := dct3Quarter; i < dctSize; i++ {
		wave[i] = -waveTmp[i-dct3Quarter]
	}
}

func relicDecodeFrameInternal(freq1, freq2 []float32, waveCur, wavePrv []float32, dct *[relicMaxSize]float32, window *[relicMaxSize]float32, dctSize int) {
	waveTmp := make([]float32, relicMaxSize)
	dctHalf := dctSize >> 1

	// copy for first half
	copy(waveCur, wavePrv)

	// transform frequency domain to time domain with DCT/FFT
	relicApplyIdct(freq1, waveTmp, dct, dctSize)
	relicApplyIdct(freq2, wavePrv, dct, dctSize)

	// overlap and apply window function
	for i := 0; i < dctHalf; i++ {
		waveCur[dctHalf+i] = waveTmp[i]*window[i] + waveCur[dctHalf+i]*window[dctHalf+i]
		wavePrv[i] = wavePrv[i]*window[i] + waveTmp[dctHalf+i]*window[dctHalf+i]
	}
}

func relicInitWindow(window *[relicMaxSize]float32, dctSize int) {
	for i := 0; i < dctSize; i++ {
		window[i] = float32(math.Sin(float64(i) * (relicPi / float64(dctSize))))
	}
}

func relicDecodeFrameBase(freq1, freq2 []float32, waveCur, wavePrv []float32, dct *[relicMaxSize]float32, window *[relicMaxSize]float32, dctMode, samplesMode int) {
	waveTmp := make([]float32, relicMaxSize)

	if samplesMode == relicSizeLow {
		relicDecodeFrameInternal(freq1, freq2, waveCur, wavePrv, dct, window, relicSizeLow)
	} else if samplesMode == relicSizeMid {
		if dctMode == relicSizeLow {
			relicDecodeFrameInternal(freq1, freq2, waveTmp, wavePrv, dct, window, relicSizeLow)
			for i := 0; i < 256-1; i += 2 {
				waveCur[i+0] = waveTmp[i>>1]
				waveCur[i+1] = waveTmp[i>>1]
			}
		} else {
			relicDecodeFrameInternal(freq1, freq2, waveCur, wavePrv, dct, window, relicSizeMid)
		}
	} else if samplesMode == relicSizeHigh {
		if dctMode == relicSizeLow {
			relicDecodeFrameInternal(freq1, freq2, waveTmp, wavePrv, dct, window, relicSizeLow)
			for i := 0; i < 512-1; i += 4 {
				waveCur[i+0] = waveTmp[i>>2]
				waveCur[i+1] = waveTmp[i>>2]
				waveCur[i+2] = waveTmp[i>>2]
				waveCur[i+3] = waveTmp[i>>2]
			}
		} else if dctMode == relicSizeMid {
			relicDecodeFrameInternal(freq1, freq2, waveTmp, wavePrv, dct, window, relicSizeMid)
			for i := 0; i < 512-1; i += 2 {
				waveCur[i+0] = waveTmp[i>>1]
				waveCur[i+1] = waveTmp[i>>1]
			}
		} else {
			relicDecodeFrameInternal(freq1, freq2, waveCur, wavePrv, dct, window, relicSizeHigh)
		}
	}
}

// Bit reader functions (LSB-first, like Vorbis)

func readUbits(bits, offset uint, buf []byte) uint32 {
	shift := offset - 8*(offset/8)
	mask := uint32((1 << bits) - 1)
	pos := offset / 8
	val := uint32(buf[pos]) | uint32(buf[pos+1])<<8 | uint32(buf[pos+2])<<16 | uint32(buf[pos+3])<<24
	return (val >> shift) & mask
}

func readSbits(bits, offset uint, buf []byte) int32 {
	val := readUbits(bits, offset, buf)
	if val>>(bits-1) == 1 {
		mask := uint32((1 << (bits - 1)) - 1)
		return -int32(val & mask)
	}
	return int32(val)
}

func relicInitDequantization(scales *[relicMaxScales]float32) {
	scales[0] = relicBaseScale
	for i := 1; i < relicMaxScales; i++ {
		scales[i] = scales[i-1] * scales[0]
	}
	for i := 0; i < relicMaxScales; i++ {
		denom := 1 << (uint(i) + 1)
		scales[i] = relicFreqMaskFactor / float32(denom-1) * scales[i]
	}
}

func relicUnpackFrame(buf []byte, bufSize int, freq1, freq2 []float32, scales *[relicMaxScales]float32, exponents []uint8, freqSize int) bool {
	freqHalf := freqSize >> 1
	// Clear output
	for i := range freq1 {
		freq1[i] = 0
	}
	for i := range freq2 {
		freq2[i] = 0
	}

	flags := readUbits(2, 0, buf)
	cbBits := int(readUbits(3, 2, buf))
	evBits := int(readUbits(2, 5, buf))
	eiBits := int(readUbits(4, 7, buf))
	var bitOffset int = 11
	maxOffset := bufSize * 8

	// Reset exponent indexes
	if flags&1 == 1 {
		for i := range exponents {
			exponents[i] = 0
		}
	}

	// Read packed exponents indexes for all bands
	if cbBits > 0 && evBits > 0 {
		pos := 0
		for i := 0; i < relicCriticalBandCount-1; i++ {
			if bitOffset+cbBits > maxOffset {
				return false
			}
			move := int(readUbits(uint(cbBits), uint(bitOffset), buf))
			bitOffset += cbBits
			if i > 0 && move == 0 {
				break
			}
			pos += move
			if pos+1 >= relicCriticalBandCount {
				return false
			}
			if bitOffset+evBits > maxOffset {
				return false
			}
			ev := int(readUbits(uint(evBits), uint(bitOffset), buf))
			bitOffset += evBits
			for j := int(criticalBandData[pos]); j < int(criticalBandData[pos+1]); j++ {
				exponents[j] = uint8(ev)
			}
		}
	}

	// Read quantized values
	if freqHalf > 0 && eiBits > 0 {
		// Read first part
		pos := 0
		for i := 0; i < relicMaxFreq; i++ {
			if bitOffset+eiBits > maxOffset {
				return false
			}
			move := int(readUbits(uint(eiBits), uint(bitOffset), buf))
			bitOffset += eiBits
			if i > 0 && move == 0 {
				break
			}
			pos += move
			if pos >= relicMaxFreq {
				return false
			}
			qvBits := exponents[pos]
			if bitOffset+int(qvBits)+2 > maxOffset {
				return false
			}
			qv := readSbits(uint(qvBits)+2, uint(bitOffset), buf)
			bitOffset += int(qvBits) + 2
			if qv != 0 && pos < freqHalf && qvBits < 6 {
				freq1[pos] = float32(qv) * scales[qvBits]
			}
		}

		// Read second part, or clone it
		if flags&2 == 2 {
			copy(freq2, freq1)
		} else {
			pos = 0
			for i := 0; i < relicMaxFreq; i++ {
				if bitOffset+eiBits > maxOffset {
					return false
				}
				move := int(readUbits(uint(eiBits), uint(bitOffset), buf))
				bitOffset += eiBits
				if i > 0 && move == 0 {
					break
				}
				pos += move
				if pos >= relicMaxFreq {
					return false
				}
				qvBits := exponents[pos]
				if bitOffset+int(qvBits)+2 > maxOffset {
					return false
				}
				qv := readSbits(uint(qvBits)+2, uint(bitOffset), buf)
				bitOffset += int(qvBits) + 2
				if qv != 0 && pos < freqHalf && qvBits < 6 {
					freq2[pos] = float32(qv) * scales[qvBits]
				}
			}
		}
	}

	return true
}

// --- Public API ---

func relicInit(channels, bitrate, codecRate int) *relicHandle {
	if channels < 0 || channels > relicMaxChannels {
		return nil
	}
	h := &relicHandle{}
	h.channels = channels

	if codecRate < 22050 {
		h.freqSize = relicSizeLow
	} else if codecRate == 22050 {
		h.freqSize = relicSizeMid
	} else {
		h.freqSize = relicSizeHigh
	}

	h.waveSize = relicSizeHigh
	h.dctMode = relicSizeHigh
	h.samplesMode = relicSizeHigh

	relicInitDct(&h.dct, relicSizeHigh)
	relicInitWindow(&h.window, relicSizeHigh)
	relicInitDequantization(&h.scales)

	if bitrate < relicMinBitrate || bitrate > relicMaxBitrate {
		return nil
	}
	h.frameSize = bitrate / 8

	return h
}

func relicGetFrameSize(h *relicHandle) int {
	if h == nil {
		return 0
	}
	return h.frameSize
}

func relicDecodeFrame(h *relicHandle, buf []byte, channel int) bool {
	// Clean extra bytes for bitreader
	for i := h.frameSize; i < relicBufferSize && i < len(buf); i++ {
		buf[i] = 0
	}
	ok := relicUnpackFrame(buf, relicBufferSize, h.freq1[:], h.freq2[:], &h.scales, h.exponents[channel][:], h.freqSize)
	if !ok {
		return false
	}
	relicDecodeFrameBase(h.freq1[:], h.freq2[:], h.waveCur[channel][:], h.wavePrv[channel][:], &h.dct, &h.window, h.dctMode, h.samplesMode)
	return true
}

func clamp16(val int32) int16 {
	if val > 32767 {
		return 32767
	} else if val < -32768 {
		return -32768
	}
	return int16(val)
}

func relicGetPCM16(h *relicHandle, sbuf []int16) {
	ichs := h.channels
	for s := 0; s < relicSamplesPerFrame; s++ {
		for ch := 0; ch < ichs; ch++ {
			pcm := clamp16(int32(h.waveCur[ch][s]))
			sbuf[s*ichs+ch] = pcm
		}
	}
}

func relicGetFloat(h *relicHandle, sbuf []float32) {
	pos := 0
	for s := 0; s < relicSamplesPerFrame; s++ {
		for ch := 0; ch < h.channels; ch++ {
			sbuf[pos] = h.waveCur[ch][s]
			pos++
		}
	}
}

// Exported form of the codec's fixed sizes, for callers that build buffers for
// the encoder or reason about frame sizes: the encoder lives in the application
// and needs the same numbers the decoder is built from.
const (
	MaxChannels     = relicMaxChannels
	MaxScales       = relicMaxScales
	MaxSize         = relicMaxSize
	MaxFreq         = relicMaxFreq
	MaxFFT          = relicMaxFFT
	SamplesPerFrame = relicSamplesPerFrame
	BufferSize      = relicBufferSize
	MinBitrate      = relicMinBitrate
	MaxBitrate      = relicMaxBitrate
)

// Exported accessors for the pieces the encoder shares with the decoder: the
// DCT and window tables it initialises, and the critical band layout that says
// where in the spectrum each band sits.
const CriticalBandCount = relicCriticalBandCount

// CriticalBands is where each band starts in the spectrum, as a fraction of the
// full table. The encoder walks the same layout the decoder unpacks against.
var CriticalBands = criticalBandData

// InitDCT, InitWindow and InitDequantization fill the tables the encoder and the
// decoder both build, so that a round trip through this package uses one copy.
func InitDCT(dct *[MaxSize]float32, dctSize int)  { relicInitDct(dct, dctSize) }
func InitWindow(w *[MaxSize]float32, dctSize int) { relicInitWindow(w, dctSize) }
func InitDequantization(s *[MaxScales]float32)    { relicInitDequantization(s) }
