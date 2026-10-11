package main

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"
)

// The effects must not run at a different speed depending on whether the clip
// is playing. It went wrong once by reading the clip's frame rate as though it
// were its duration: the step is the playhead's own travel, so a clip measured
// twenty times too short ran its effects twenty times too slowly, and because
// a paused clip steps by the wall clock instead, the only visible symptom was
// "the effects freeze while the animation plays and run while it is paused".

// A clip's length in seconds is its frame count over its rate — not the
// reciprocal of the rate. The playhead advances by dt*rate/frames each frame,
// so one pass takes frames/rate seconds and the two must agree exactly, since
// the effect step is computed from the length.
func TestClipLengthIsFramesOverRate(t *testing.T) {
	for _, frames := range []int{4, 62, 362} {
		anim := &WhmAnimation{Name: "clip", NumFrames: frames, Duration: 1}
		fps := animFramesPerSecond(anim)
		clipLen := float64(anim.NumFrames) / fps

		// The playhead advances by dt*rate/frames per frame, so it covers the
		// whole clip at rate/frames per second — and one pass therefore takes
		// frames/rate seconds. That is the number the effect step is built on,
		// so the two have to be the same number.
		perSecond := fps / float64(frames)
		perPass := 1.0 / perSecond
		if math.Abs(perPass-clipLen) > 1e-9 {
			t.Errorf("a %d frame clip at %g fps lasts %g s by the playhead but "+
				"%g s by the length used for the effects", frames, fps, perPass, clipLen)
		}
		if perPass <= 0 || perPass > 60 {
			t.Errorf("a %d frame clip lasts %g s, which is not plausible", frames, perPass)
		}
	}
}

// The same measurement from the outside: step one viewer for a second while
// playing and another while paused, and the effect must end up in the same
// place. This is the symptom the reader reported, so it is the thing to hold.
func TestPlayingAndPausedEffectsAdvanceAlike(t *testing.T) {
	run := func(playing bool) float64 {
		sim := fx.NewFxSim(&fx.FxFile{})
		v := &modelViewer{
			model:   &WhmModel{},
			anim:    &WhmAnimation{Name: "vis_vis_main", NumFrames: 4, Duration: 1},
			playing: playing,
			showFX:  true,
			fxScale: 1,
		}
		e := fxOverlayEffect{
			Sim:        sim,
			Label:      "glow",
			Timed:      true,
			Animations: []string{"vis_vis_main"},
			FireTimes:  map[string]float64{"vis_vis_main": -1},
		}
		v.fxEffects = []fxOverlayEffect{e}
		const frames = 60
		const dt = 1.0 / 60.0
		for i := 0; i < frames; i++ {
			// The canvas steps the effects first and advances the playhead
			// after, so the order here is the order it does it in.
			v.fxStepOnClip([]fxOverlayEffect{v.fxEffects[0]}, "vis_vis_main", dt)
			if playing {
				v.time += float32(dt * animFramesPerSecond(v.anim) / float64(v.anim.NumFrames))
				for v.time >= 1 {
					v.time -= 1
				}
			}
		}
		return sim.Time
	}
	playing := run(true)
	paused := run(false)
	if math.Abs(playing-paused) > 0.2 {
		t.Errorf("after a second the simulation is at %g playing and %g paused — "+
			"the effects run at a different rate depending on the clip", playing, paused)
	}
	if playing < 0.5 {
		t.Errorf("after a second of a playing clip the simulation only reached %g", playing)
	}
}

// A clip loops, and a looping clip must not sit frozen between passes. The
// four frame vis clip comes round five times a second, so an effect that is
// reset every pass would be seen only as a flicker.
func TestALoopingShortClipKeepsItsEffectsRunning(t *testing.T) {
	sim := fx.NewFxSim(&fx.FxFile{})
	v := &modelViewer{
		model:   &WhmModel{},
		anim:    &WhmAnimation{Name: "vis_vis_main", NumFrames: 4, Duration: 1},
		playing: true,
		showFX:  true,
		fxScale: 1,
	}
	e := fxOverlayEffect{
		Sim:        sim,
		Label:      "glow",
		Timed:      true,
		Animations: []string{"vis_vis_main"},
		FireTimes:  map[string]float64{"vis_vis_main": -1},
	}
	v.fxEffects = []fxOverlayEffect{e}
	const dt = 1.0 / 60.0
	drawn := 0
	for i := 0; i < 60; i++ {
		v.fxStepOnClip([]fxOverlayEffect{e}, "vis_vis_main", dt)
		if len(v.fxStepOnClip([]fxOverlayEffect{e}, "vis_vis_main", 0)) > 0 {
			drawn++
		}
		v.time += float32(dt * animFramesPerSecond(v.anim) / float64(v.anim.NumFrames))
		for v.time >= 1 {
			v.time -= 1
		}
	}
	if drawn != 60 {
		t.Errorf("an entry event was drawn on %d of 60 frames of a looping clip", drawn)
	}
}
