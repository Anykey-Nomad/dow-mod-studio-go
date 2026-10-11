package wav

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteThenParseRoundTrip(t *testing.T) {
	pcm := make([]byte, 0, 8)
	for _, s := range []int16{1, -2, 300, -4000} {
		pcm = binary.LittleEndian.AppendUint16(pcm, uint16(s))
	}
	var buf bytes.Buffer
	if err := Write(&buf, 22050, 2, 16, pcm); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.SampleRate != 22050 || got.Channels != 2 || got.BitsPerSample != 16 {
		t.Fatalf("format = %d Hz %d ch %d bit, want 22050 Hz 2 ch 16 bit", got.SampleRate, got.Channels, got.BitsPerSample)
	}
	if !bytes.Equal(got.PCMData, pcm) {
		t.Fatalf("PCM data changed on the round trip")
	}
}

func TestParseRejectsNonWAV(t *testing.T) {
	if _, err := Parse([]byte("not a wave file at all")); err == nil {
		t.Fatal("Parse accepted non-RIFF data")
	}
}

func TestParseRejectsEightBit(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, 22050, 1, 8, []byte{1, 2, 3}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := Parse(buf.Bytes()); err == nil {
		t.Fatal("Parse accepted 8-bit PCM, which the encoder cannot take")
	}
}
