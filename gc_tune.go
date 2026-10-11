package main

import (
	"log"
	"os"
	"runtime/debug"
)

// gcPercent is what the collector is told to aim at, as a multiple of the live
// heap: collect once the heap has grown to this many times what is still in
// use. Go's own default is 100, which on a program that redraws at frame rate
// means a collection every few hundred milliseconds of playback, and each one
// stops the world while it marks.
//
// Three hundred is the same trade the collector is already making with the
// heap, stretched: three collections an animation where there were nine, for
// three times the peak footprint of one. A preview holds a handful of decoded
// textures and a model's skeleton, so the heap it grows into is small next to
// what the machine has, and the collections that do happen are further apart
// and easier to miss.
//
// An environment that says otherwise wins, because a reader who has measured
// their own stutter with a different number knows more about it than this does.
const gcPercent = 300

// tuneCollector is called once, before any window is opened. It has no effect
// when GOGC is set in the environment, which is how a reader turns the pacing
// back to Go's own without rebuilding.
func tuneCollector() {
	if v := os.Getenv("GOGC"); v != "" {
		log.Printf("gc: GOGC=%s in the environment, leaving the collector alone", v)
		return
	}
	debug.SetGCPercent(gcPercent)
	log.Printf("gc: collecting at %d%% of the live heap", gcPercent)
}
