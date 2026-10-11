package fda

import (
	"fmt"
)

// DecodeFDAToPCM decodes Relic-compressed audio into an in-memory PCM buffer
// (16-bit, interleaved) and returns the parsed FDA info alongside. No files
// are written.
func DecodeFDAToPCM(fda *FDAFile) ([]int16, error) {
	pcm, _, err := DecodeFDAToPCMAtRate(fda, int(fda.Info.SampleRate))
	return pcm, err
}

// DecodeFDAToPCMAtRate decodes at a rate given by the caller rather than the
// one the file declares.
//
// The reason it exists is that the rate is not only how fast the result plays,
// it decides which frequency table the codec unpacks with — relicInit picks a
// table of 128, 256 or 512 bands from it, and the choice falls on a cliff at
// exactly 22050: below that one table, at exactly 22050 another, above it a
// third. So a file whose declared rate is wrong is not merely played at the
// wrong speed, it is unpacked with the wrong table and comes out wrong in a way
// no resampling afterwards can repair.
//
// It also returns how many frames failed to unpack, which is what tells a caller
// that guessed the rate wrong: the bit-packed band data does not fit the table it
// is being read with, and the decoder says so per frame.
func DecodeFDAToPCMAtRate(fda *FDAFile, codecRate int) ([]int16, int, error) {
	// Init Relic decoder
	channels := int(fda.Info.Channels)
	bitrate := int(fda.Info.BlockBitrate)

	decoder := relicInit(channels, bitrate, codecRate)
	if decoder == nil {
		return nil, 0, fmt.Errorf("failed to init Relic decoder (channels=%d, bitrate=%d, rate=%d)", channels, bitrate, codecRate)
	}
	defer relicFree(decoder)

	frameSize := relicGetFrameSize(decoder)
	if frameSize == 0 {
		return nil, 0, fmt.Errorf("invalid frame size")
	}

	// Calculate total samples
	// Frames are stored per-channel: [ch0_frame][ch1_frame][ch0_frame][ch1_frame]...
	totalBytes := len(fda.RawData)
	framesPerChannel := totalBytes / (frameSize * channels)
	totalSamples := framesPerChannel * relicSamplesPerFrame

	// Decode all frames
	pcmData := make([]int16, 0, totalSamples*channels)
	chBuf := make([]byte, relicBufferSize)
	failed := 0

	for i := 0; i < framesPerChannel; i++ {
		// Decode each channel from its own frame in the stream
		for ch := 0; ch < channels; ch++ {
			offset := (i*channels + ch) * frameSize
			// Copy frame data into buffer (padded to relicBufferSize)
			for j := 0; j < relicBufferSize; j++ {
				if j < frameSize && offset+j < totalBytes {
					chBuf[j] = fda.RawData[offset+j]
				} else {
					chBuf[j] = 0
				}
			}
			if !relicDecodeFrame(decoder, chBuf, ch) {
				failed++
			}
		}

		// Extract PCM samples for all channels
		samples := make([]int16, relicSamplesPerFrame*channels)
		relicGetPCM16(decoder, samples)
		pcmData = append(pcmData, samples...)

	}

	return pcmData, failed, nil
}

// relicFree releases a decoder handle. Go frees the memory when it goes out of
// scope, so there is nothing to do; the name is kept because the decoder's shape
// is a port of the C original and reads better with the pair together.
func relicFree(h *relicHandle) {
	_ = h
}
