// Command audiotest decodes a Relic .fda and writes WAV files of the same audio
// at several playback rates, so the rate a sound is really recorded at can be
// settled by ear rather than by argument.
//
// It exists because of a file in the game that plays an octave low. The header
// of that file says one rate and its waveform is the same either way — decoding
// it at 22050 or at 44100 produces bit-identical samples — so the only question
// is what speed to play it at, and that is a question for listening, not for
// arithmetic. Feed it a 22050 Hz file and it writes:
//
//	declared.wav   the samples at the rate the file's header claims
//	44100.wav      the same samples, played at 44100 Hz
//	44100rs.wav    the same samples, properly resampled 22050 -> 44100
//
// The first two are the same waveform at two speeds, which is the comparison that
// answers the question. The third is there to show what an actual rate conversion
// sounds like, against the relabelling that is really being proposed.
//
// Usage:
//
//	audiotest <file.fda> [outdir]
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/fda"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: audiotest <file.fda> [outdir]")
		os.Exit(2)
	}
	in := os.Args[1]
	outDir := "."
	if len(os.Args) > 2 {
		outDir = os.Args[2]
	}
	// Made rather than assumed: a tool that dies on a missing output directory
	// after decoding the file looks like a broken decoder.
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "audiotest: create output directory:", err)
		os.Exit(1)
	}
	if err := run(in, outDir); err != nil {
		fmt.Fprintln(os.Stderr, "audiotest:", err)
		os.Exit(1)
	}
}

func run(in, outDir string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	file, err := fda.ParseFDAReader(strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	pcm, err := fda.DecodeFDAToPCM(file)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	declared := int(file.Info.SampleRate)
	ch := int(file.Info.Channels)
	if declared <= 0 || ch <= 0 {
		return fmt.Errorf("nonsense INFO: %d Hz, %d channels", declared, ch)
	}
	seconds := float64(len(pcm)/ch) / float64(declared)

	base := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
	fmt.Printf("%s\n", filepath.Base(in))
	fmt.Printf("  header says   %d Hz, %d channel(s), %d samples\n", declared, ch, len(pcm))
	fmt.Printf("  at that rate  %.3f s\n", seconds)
	for _, r := range []int{11025, 22050, 32000, 44100, 48000} {
		fmt.Printf("  played at %5d Hz: %.3f s\n", r, float64(len(pcm)/ch)/float64(r))
	}

	// The two answers, and a conversion to compare them against.
	writes := []struct {
		name    string
		rate    int
		samples []int16
		note    string
	}{
		{base + "_declared.wav", declared, pcm, "the header's own rate"},
		{base + "_at11025.wav", 11025, pcm, "same samples, half speed again"},
		{base + "_at44100.wav", 44100, pcm, "same samples, twice as fast"},
		{base + "_at48000.wav", 48000, pcm, "same samples, at 48000"},
	}
	if declared != 44100 {
		converted, err := resample(pcm, declared, ch, 44100, ch)
		if err != nil {
			return fmt.Errorf("resample: %w", err)
		}
		writes = append(writes, struct {
			name    string
			rate    int
			samples []int16
			note    string
		}{base + "_44100_resampled.wav", 44100, converted, "genuinely resampled up to 44100"})
	}

	for _, w := range writes {
		path := filepath.Join(outDir, w.name)
		if err := writeWAV(path, w.rate, ch, w.samples); err != nil {
			return err
		}
		d := float64(len(w.samples)/ch) / float64(w.rate)
		fmt.Printf("  wrote %-34s %5d Hz  %7.3f s   (%s)\n", w.name, w.rate, d, w.note)
	}
	fmt.Println()
	fmt.Println("Listen to the pair that differ only in speed: if the sound is right at")
	fmt.Println("one and wrong at the other, the wrong one names the file's true rate.")
	return nil
}

// resample converts PCM to a new rate and channel count with linear
// interpolation. The tool deliberately uses the simplest conversion there is:
// the question being put to the listener is which rate the file really is, and a
// filtered conversion would blur that comparison rather than sharpen it. The
// application itself uses a proper interpolating resampler.
func resample(samples []int16, inRate, inChannels, outRate, outChannels int) ([]int16, error) {
	inFrames := len(samples) / inChannels
	if inFrames == 0 {
		return nil, nil
	}
	outFrames := inFrames * outRate / inRate
	out := make([]int16, 0, outFrames*outChannels)
	for f := 0; f < outFrames; f++ {
		exact := int64(f) * int64(inRate)
		pos := exact / int64(outRate)
		next := pos + 1
		if next >= int64(inFrames) {
			next = int64(inFrames) - 1
		}
		frac := exact % int64(outRate)
		for c := 0; c < outChannels; c++ {
			a := channelSample(samples, inChannels, outChannels, int(pos), c)
			b := channelSample(samples, inChannels, outChannels, int(next), c)
			v := (a*int64(outRate) - a*frac + b*frac + int64(outRate)/2) / int64(outRate)
			out = append(out, clamp16(v))
		}
	}
	return out, nil
}

func channelSample(samples []int16, inChannels, outChannels, frame, outChannel int) int64 {
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

func clamp16(v int64) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// writeWAV writes a 16-bit PCM WAV.
func writeWAV(path string, rate, channels int, samples []int16) error {
	blockAlign := channels * 2
	byteRate := rate * blockAlign
	dataLen := len(samples) * 2

	buf := make([]byte, 0, 44+dataLen)
	le32 := func(v uint32) { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); buf = append(buf, b...) }
	le16 := func(v uint16) { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); buf = append(buf, b...) }
	tag := func(s string) { buf = append(buf, s...) }

	tag("RIFF")
	le32(uint32(36 + dataLen))
	tag("WAVE")
	tag("fmt ")
	le32(16)
	le16(1) // PCM
	le16(uint16(channels))
	le32(uint32(rate))
	le32(uint32(byteRate))
	le16(uint16(blockAlign))
	le16(16) // bits per sample
	tag("data")
	le32(uint32(dataLen))
	for _, s := range samples {
		le16(uint16(s))
	}
	return os.WriteFile(path, buf, 0o644)
}
