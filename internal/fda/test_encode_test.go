package fda

import (
	"bytes"
	"encoding/binary"
	"testing"

	"dow-mod-studio/internal/wav"
)

// The codec's own round trip: a PCM WAV encoded to FDA must parse back and
// decode to audio, which is what the event editor's WAV conversion relies on.
func TestEncodeWAVFDARoundTrip(t *testing.T) {
	frames := 1024
	samples := make([]int16, frames)
	for i := range samples {
		samples[i] = int16((i*97)%20000 - 10000)
	}
	raw := make([]byte, 44+len(samples)*2)
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], 1)
	binary.LittleEndian.PutUint32(raw[24:], 44100)
	binary.LittleEndian.PutUint32(raw[28:], 44100*2)
	binary.LittleEndian.PutUint16(raw[32:], 2)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(samples)*2))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(raw[44+i*2:], uint16(s))
	}
	parsed, err := wav.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := EncodeWAV(parsed, 2048)
	if err != nil {
		t.Fatal(err)
	}
	file, err := ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if file.Info.SampleRate != 44100 || file.Info.Channels != 1 || len(file.RawData) == 0 {
		t.Fatalf("unexpected FDA: %+v", file.Info)
	}
	decoded, err := DecodeFDAToPCM(file)
	if err != nil || len(decoded) == 0 {
		t.Fatalf("encoded FDA cannot be decoded: samples=%d err=%v", len(decoded), err)
	}
}

func TestEncodeWAVRejectsBadInput(t *testing.T) {
	if _, err := EncodeWAV(nil, 2048); err == nil {
		t.Fatal("EncodeWAV accepted a nil WAV")
	}
	bad := &wav.Info{SampleRate: 44100, Channels: 3, BitsPerSample: 16}
	if _, err := EncodeWAV(bad, 2048); err == nil {
		t.Fatal("EncodeWAV accepted three channels, which the codec cannot take")
	}
	w := &wav.Info{SampleRate: 44100, Channels: 1, BitsPerSample: 16, PCMData: make([]byte, 1024)}
	if _, err := EncodeWAV(w, 42); err == nil {
		t.Fatal("EncodeWAV accepted a bitrate outside the codec's range")
	}
}
