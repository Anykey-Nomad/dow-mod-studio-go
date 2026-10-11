// Package audioplayer owns the process's one audio device context and the
// chunked PCM source that feeds it. FDA playback, WAV preview and the FX
// sound player all go through here, so the device is opened once and every
// sound is played in the one format it was opened with.
package audioplayer

import (
	"errors"
	"io"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// The one format this process plays at.
//
// SampleRate and Channels are fixed, and the rate is 48000 with two channels,
// because that is the one format Windows audio reliably plays at the rate it
// is told to.
//
// A context takes its format from whoever creates it first, and every sound in
// the game is mono at 22050 or 44100 Hz: FDA INFO declares one channel for all
// of them. Left to itself the context would therefore be created mono at
// whichever rate the first sound happened to carry, and oto documents that WASAPI
// is unlikely to support that — "Stereo with 48000 [Hz] is likely supported, but
// mono and/or other sample rates are unlikely supported" — so it falls back to
// WinMM, which opens a mono stream the device is then free to rate as it likes.
// That is what makes an overlapping sound come out stretched and pitched down
// while another, at the rate the device settled on, sounds right.
//
// So the context is 48000 Hz stereo and every sample is converted into it by
// the decode path before it reaches a player here.
const (
	SampleRate = 48000
	Channels   = 2
)

var (
	ctx     *oto.Context
	once    sync.Once
	initErr error
)

// Context returns the process-wide audio context, creating it on first use.
// It takes no format arguments on purpose: the context has exactly one and a
// caller asking for a different one gets the pinned one and must convert.
func Context() (*oto.Context, error) {
	once.Do(func() {
		op := &oto.NewContextOptions{}
		op.SampleRate = SampleRate
		op.ChannelCount = Channels
		op.Format = oto.FormatSignedInt16LE
		var ready chan struct{}
		ctx, ready, initErr = oto.NewContext(op)
		if initErr == nil {
			<-ready
		}
	})
	if initErr != nil {
		return nil, initErr
	}
	return ctx, nil
}

// chunkMillis is how long one buffered chunk of a ChunkBuffer holds.
const chunkMillis = 50

// ChunkBuffer splits a PCM stream into the fixed-size chunks a ChunkSource
// reads. Seeking lands on a chunk boundary, which is what keeps a seek cheap
// and a stream aligned.
type ChunkBuffer struct {
	chunks     map[int][]byte
	chunkBytes int
	numChunks  int
	totalBytes int
}

// NewChunkBuffer splits data into chunks sized for the given format.
func NewChunkBuffer(data []byte, sampleRate, channels int) *ChunkBuffer {
	bytesPerSample := channels * 2
	samples := (sampleRate*chunkMillis + 500) / 1000
	if samples < 1 {
		samples = 1
	}
	cb := &ChunkBuffer{
		chunks:     make(map[int][]byte),
		chunkBytes: samples * bytesPerSample,
	}
	for off, i := 0, 0; off < len(data); i++ {
		end := off + cb.chunkBytes
		if end > len(data) {
			end = len(data)
		}
		cb.chunks[i] = data[off:end]
		off = end
		cb.numChunks = i + 1
	}
	cb.totalBytes = len(data)
	return cb
}

// ChunkBytes is the size of one full chunk.
func (cb *ChunkBuffer) ChunkBytes() int { return cb.chunkBytes }

// NumChunks is how many chunks the buffer was split into.
func (cb *ChunkBuffer) NumChunks() int { return cb.numChunks }

// TotalBytes is the length of the whole stream in bytes.
func (cb *ChunkBuffer) TotalBytes() int { return cb.totalBytes }

// ChunkSource is the io.Reader/io.Seeker a player consumes a ChunkBuffer
// through. It is safe to read position and remaining length from another
// goroutine while a player is consuming it, which is how a seek bar tracks a
// sound that is playing.
type ChunkSource struct {
	cb  *ChunkBuffer
	mu  sync.Mutex
	idx int
	off int
}

// NewChunkSource returns a reader positioned at the start of cb.
func NewChunkSource(cb *ChunkBuffer) *ChunkSource {
	return &ChunkSource{cb: cb}
}

func (s *ChunkSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for total < len(p) {
		if s.idx >= s.cb.numChunks {
			return total, io.EOF
		}
		ch := s.cb.chunks[s.idx]
		n := copy(p[total:], ch[s.off:])
		total += n
		s.off += n
		if s.off >= len(ch) {
			s.idx++
			s.off = 0
		}
	}
	return total, nil
}

func (s *ChunkSource) bytePosLocked() int64 {
	if s.idx >= s.cb.numChunks {
		return int64(s.cb.totalBytes)
	}
	return int64(s.idx*s.cb.chunkBytes + s.off)
}

// BytePos is how far into the stream the reader has got.
func (s *ChunkSource) BytePos() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bytePosLocked()
}

// TotalBytes is the length of the whole stream, so a caller tracking position
// does not have to reach into the buffer for it.
func (s *ChunkSource) TotalBytes() int64 {
	return int64(s.cb.totalBytes)
}

// ChunkBytes is the size of one chunk — the granularity a seek lands on.
func (s *ChunkSource) ChunkBytes() int {
	return s.cb.chunkBytes
}

// Remaining is how many bytes are left to read.
func (s *ChunkSource) Remaining() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := int64(s.cb.totalBytes) - s.bytePosLocked()
	if n < 0 {
		return 0
	}
	return n
}

func (s *ChunkSource) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = s.bytePosLocked() + offset
	case io.SeekEnd:
		abs = int64(s.cb.totalBytes) + offset
	default:
		return 0, errors.New("ChunkSource: invalid whence")
	}
	if abs < 0 {
		abs = 0
	}
	if abs >= int64(s.cb.totalBytes) {
		s.idx = s.cb.numChunks
		s.off = 0
		return int64(s.cb.totalBytes), nil
	}
	if s.cb.chunkBytes > 0 {
		abs -= abs % int64(s.cb.chunkBytes)
	}
	s.idx = int(abs) / s.cb.chunkBytes
	s.off = 0
	return abs, nil
}
