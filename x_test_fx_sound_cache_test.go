//go:build windows

package main

import (
	"dow-mod-studio/internal/fx"
	"fmt"
	"sync"
	"testing"
)

// The samples of a model's sounds are decoded by one goroutine per sound that
// names a folder of variants, all writing into one cache while the caller waits
// to read it. Go's map panics on exactly that — "concurrent map read and map
// write" — rather than losing a key, so the cache is what stands between a
// model with several sound folders and a crash.
//
// -race cannot be used to check this here (the build has no C compiler), so
// the test does what a race detector would: it writes from many goroutines at
// once and reads while they are running, which the old bare map could not
// survive.
func TestFxSoundCacheTakesConcurrentWritersAndReaders(t *testing.T) {
	c := fx.NewSoundCache()
	const writers, each = 8, 200

	var wg sync.WaitGroup
	stop := make(chan struct{})
	// Readers run while the writers do, because that is the pairing that
	// faults: a writer alone is fine, and a reader alone is fine.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := c.Snapshot()
				for k, s := range snap {
					if s == nil {
						t.Errorf("cache holds a nil sample for %q", k)
						return
					}
				}
				c.Get(fmt.Sprintf("sound_%d", r))
				c.Len()
			}
		}()
	}
	var writersWG sync.WaitGroup
	for w := 0; w < writers; w++ {
		writersWG.Add(1)
		go func(w int) {
			defer writersWG.Done()
			for i := 0; i < each; i++ {
				c.Put(fmt.Sprintf("sound_%d_%d", w, i), &fx.SoundSample{})
			}
		}(w)
	}
	// The writers are waited for first, and only then are the readers let go,
	// so the readers were reading throughout rather than after the fact.
	writersWG.Wait()
	close(stop)
	wg.Wait()

	if got := c.Len(); got != writers*each {
		t.Errorf("cache holds %d samples, want %d", got, writers*each)
	}
	snap := c.Snapshot()
	if len(snap) != writers*each {
		t.Errorf("snapshot holds %d samples, want %d", len(snap), writers*each)
	}
	// A snapshot is a copy: writing through the cache afterwards must not
	// change one the player already has.
	c.Put("sound_0_0", &fx.SoundSample{})
	if _, ok := snap["sound_0_0"]; !ok {
		t.Error("snapshot lost a sample that was there when it was taken")
	}
}
