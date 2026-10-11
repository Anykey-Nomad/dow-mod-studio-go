package fda

import (
	"fmt"
	"io"

	"dow-mod-studio/internal/wav"
)

// DecodeFDAtoWAV decodes Relic-compressed audio and writes a PCM WAV file.
func DecodeFDAtoWAV(filename string, file *FDAFile) error {
	pcm, err := DecodeFDAToPCM(file)
	if err != nil {
		return err
	}
	if err := WriteWAV(filename, &file.Info, PCM16Bytes(pcm)); err != nil {
		return fmt.Errorf("write WAV: %w", err)
	}
	return nil
}

// PCM16Bytes turns decoded samples into the little endian bytes a WAV data
// chunk holds.
func PCM16Bytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		data[i*2] = byte(s)
		data[i*2+1] = byte(s >> 8)
	}
	return data
}

// WriteWAV writes PCM audio data to a WAV file.
func WriteWAV(filename string, info *INFOChunk, data []byte) error {
	return wav.WriteFile(filename, int(info.SampleRate), int(info.Channels), int(info.SampleSize), data)
}

// WriteWAVTo writes a PCM WAV to an arbitrary writer, which is how the 3D
// preview gets a decoded .fda into the shape the audio player takes without
// going near the disk.
func WriteWAVTo(w io.Writer, info *INFOChunk, data []byte) error {
	return wav.Write(w, int(info.SampleRate), int(info.Channels), int(info.SampleSize), data)
}
