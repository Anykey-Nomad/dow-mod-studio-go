package fx

import "dow-mod-studio/internal/wav"

// SoundSample is one decoded sample, ready to hand to a player.
type SoundSample struct {
	Info *wav.Info
	PCM  []int16
}
