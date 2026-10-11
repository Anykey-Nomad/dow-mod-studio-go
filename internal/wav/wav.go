// Package wav reads and writes the PCM WAV files the mod tools handle: the
// container only, with no knowledge of the codecs whose output it carries.
package wav

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Info is a parsed PCM WAV.
type Info struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
	PCMData       []byte
}

const (
	wavFormatPCM = 0x0001
)

type header struct {
	ChunkID   [4]byte // "RIFF"
	ChunkSize uint32
	Format    [4]byte // "WAVE"
}

type fmtChunk struct {
	ChunkID       [4]byte // "fmt "
	ChunkSize     uint32
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

type dataChunk struct {
	ChunkID   [4]byte // "data"
	ChunkSize uint32
}

// Parse reads PCM WAV bytes. Only little-endian 16-bit PCM is accepted, because
// the Relic encoder consumes PCM16.
func Parse(data []byte) (*Info, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a RIFF/WAVE file")
	}
	w := &Info{}
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		pos += 8
		if size < 0 || pos+size > len(data) {
			return nil, fmt.Errorf("truncated WAV chunk %q", id)
		}
		payload := data[pos : pos+size]
		switch id {
		case "fmt ":
			if len(payload) < 16 {
				return nil, fmt.Errorf("truncated WAV fmt chunk")
			}
			format := binary.LittleEndian.Uint16(payload[0:2])
			if format != 1 {
				return nil, fmt.Errorf("only PCM WAV is supported")
			}
			w.Channels = int(binary.LittleEndian.Uint16(payload[2:4]))
			w.SampleRate = int(binary.LittleEndian.Uint32(payload[4:8]))
			w.BitsPerSample = int(binary.LittleEndian.Uint16(payload[14:16]))
		case "data":
			w.PCMData = append([]byte(nil), payload...)
		}
		pos += size
		if size%2 != 0 { // RIFF chunks are word-aligned.
			pos++
		}
	}
	if w.SampleRate <= 0 || w.Channels <= 0 || w.BitsPerSample <= 0 || w.PCMData == nil {
		return nil, fmt.Errorf("WAV is missing fmt or data chunk")
	}
	if w.BitsPerSample != 16 {
		return nil, fmt.Errorf("only 16-bit PCM WAV is supported")
	}
	if len(w.PCMData)%(w.Channels*2) != 0 {
		return nil, fmt.Errorf("WAV PCM data is not aligned to complete frames")
	}
	return w, nil
}

// WriteFile writes PCM data to a WAV file.
func WriteFile(filename string, sampleRate, channels, bitsPerSample int, data []byte) error {
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create WAV file: %w", err)
	}
	defer f.Close()
	return Write(f, sampleRate, channels, bitsPerSample, data)
}

// Write writes PCM data as a WAV to any writer.
func Write(w io.Writer, sampleRate, channels, bitsPerSample int, data []byte) error {
	bytesPerSample := bitsPerSample / 8
	if bytesPerSample == 0 {
		bytesPerSample = 1
	}
	blockAlign := uint16(channels * bytesPerSample)
	byteRate := uint32(sampleRate) * uint32(blockAlign)

	fmtChunkDataSize := uint32(16)
	dataSize := uint32(len(data))
	riffChunkSize := 4 + 8 + fmtChunkDataSize + 8 + dataSize

	hdr := header{
		ChunkID:   [4]byte{'R', 'I', 'F', 'F'},
		ChunkSize: riffChunkSize,
		Format:    [4]byte{'W', 'A', 'V', 'E'},
	}
	if err := binary.Write(w, binary.LittleEndian, &hdr); err != nil {
		return fmt.Errorf("write RIFF header: %w", err)
	}
	fc := fmtChunk{
		ChunkID:       [4]byte{'f', 'm', 't', ' '},
		ChunkSize:     fmtChunkDataSize,
		AudioFormat:   wavFormatPCM,
		NumChannels:   uint16(channels),
		SampleRate:    uint32(sampleRate),
		ByteRate:      byteRate,
		BlockAlign:    blockAlign,
		BitsPerSample: uint16(bitsPerSample),
	}
	if err := binary.Write(w, binary.LittleEndian, &fc); err != nil {
		return fmt.Errorf("write fmt chunk: %w", err)
	}
	dc := dataChunk{
		ChunkID:   [4]byte{'d', 'a', 't', 'a'},
		ChunkSize: dataSize,
	}
	if err := binary.Write(w, binary.LittleEndian, &dc); err != nil {
		return fmt.Errorf("write data header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write audio data: %w", err)
	}
	return nil
}
