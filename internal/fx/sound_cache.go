package fx

import "sync"

// SoundCache holds the samples a model's sounds decoded to, keyed by their
// virtual path.
//
// It is a type rather than a bare map because several fills run at once. Every
// sound that names a folder of variants gets its own goroutine, they all write
// into this one map, and the caller reads it as soon as the worker's answer is
// applied. A bare map under those three uses is a crash rather than a lost key,
// so the lock is the whole point of the type and Snapshot is the only way to
// read it.
//
// Copying out is deliberate. The player keeps what it is given for as long as
// the model is on screen, and a background fill may still be running; handing
// over the live map would mean the player reading it while a fill writes.
type SoundCache struct {
	mu      sync.RWMutex
	samples map[string]*SoundSample
}

func NewSoundCache() *SoundCache {
	return &SoundCache{samples: map[string]*SoundSample{}}
}

// Put records a decoded sample.
func (c *SoundCache) Put(path string, s *SoundSample) {
	if c == nil || path == "" || s == nil {
		return
	}
	c.mu.Lock()
	c.samples[path] = s
	c.mu.Unlock()
}

// Get returns a sample and whether it is there.
func (c *SoundCache) Get(path string) (*SoundSample, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	s, ok := c.samples[path]
	c.mu.RUnlock()
	return s, ok
}

// Len is how many samples are held.
func (c *SoundCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	n := len(c.samples)
	c.mu.RUnlock()
	return n
}

// Snapshot copies the cache into a plain map, which is what leaves this type
// and becomes the player's own storage.
func (c *SoundCache) Snapshot() map[string]*SoundSample {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.samples) == 0 {
		return map[string]*SoundSample{}
	}
	out := make(map[string]*SoundSample, len(c.samples))
	for k, v := range c.samples {
		out[k] = v
	}
	return out
}
