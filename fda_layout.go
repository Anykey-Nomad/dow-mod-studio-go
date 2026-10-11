package main

import (
	"dow-mod-studio/internal/audioplayer"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (ui *uiState) fdaUpdateSliders(gtx layout.Context, t *fileTab) {
	if t.fdaTotal > 0 && !t.fdaSeekFloat.Dragging() {
		pos := ui.fdaPosFor(t)
		frac := float32(pos) / float32(t.fdaTotal)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		t.fdaSeekFloat.Value = frac
	}
	if t.fdaVolumePct < 0 {
		t.fdaVolumePct = 0
	}
	if t.fdaVolumePct > 200 {
		t.fdaVolumePct = 200
	}
	wantVolFrac := float32(t.fdaVolumePct) / 200
	if !t.fdaVolFloat.Dragging() {
		t.fdaVolFloat.Value = wantVolFrac
	}
}

func (ui *uiState) fdaHandleSliderSeek(gtx layout.Context, t *fileTab) {
	if t.fdaTotal == 0 || t.fdaChunkBuf == nil || t.fdaPlayer == nil || !t.fdaSeekFloat.Dragging() {
		return
	}
	target := time.Duration(float64(t.fdaSeekFloat.Value) * float64(t.fdaTotal))
	cur := ui.fdaPosFor(t)
	diff := target - cur
	if diff < 0 {
		diff = -diff
	}
	if diff > 250*time.Millisecond {
		b := int64(float64(target) / float64(time.Second) * float64(t.fdaBytesPerSec))
		if t.fdaChunkBuf.ChunkBytes() > 0 {
			b -= b % int64(t.fdaChunkBuf.ChunkBytes())
		}
		_, _ = t.fdaPlayer.Seek(b, io.SeekStart)
	}
}

func (ui *uiState) fdaHandleVolumeSlider(t *fileTab) {
	if t.fdaVolFloat.Dragging() {
		pct := int(t.fdaVolFloat.Value*200 + 0.5)
		ui.fdaSetVolumeFor(t, pct)
	}
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Millisecond)
	totalSec := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", totalSec/60, totalSec%60)
}

func buildWavePeaks(pcm []byte, w int) []byte {
	if w <= 0 {
		return nil
	}
	peaks := make([]byte, w)
	if len(pcm) < 2 {
		return peaks
	}
	totalSamples := len(pcm) / 2
	if totalSamples == 0 {
		return peaks
	}
	for x := 0; x < w; x++ {
		start := int(float64(x) * float64(totalSamples) / float64(w))
		end := int(float64(x+1) * float64(totalSamples) / float64(w))
		if end <= start {
			end = start + 1
			if end > totalSamples {
				end = totalSamples
			}
		}
		if start >= totalSamples {
			continue
		}
		if end > totalSamples {
			end = totalSamples
		}
		maxAbs := 0
		for s := start; s < end; s++ {
			off := s * 2
			if off+1 >= len(pcm) {
				break
			}
			v := int16(pcm[off]) | int16(pcm[off+1])<<8
			a := int(v)
			if a < 0 {
				a = -a
			}
			if a > maxAbs {
				maxAbs = a
				if maxAbs >= 32767 {
					break
				}
			}
		}
		b := maxAbs * 255 / 32768
		if b > 255 {
			b = 255
		}
		if b < 2 && maxAbs > 0 {
			b = 2
		}
		peaks[x] = byte(b)
	}
	return peaks
}

func (ui *uiState) layoutFdaPlayer(gtx layout.Context, t *fileTab) layout.Dimensions {
	ui.fdaUpdateSliders(gtx, t)
	if t.fdaPlayer != nil && !t.fdaPaused {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
	}
	if t.fdaError != "" {
		return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "Failed: "+t.fdaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.fdaFile == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "No FDA selected")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	pos := ui.fdaPosFor(t)
	isPlaying := !t.fdaPaused
	playLabel := "Play"
	if isPlaying {
		playLabel = "Pause"
	}
	exportLabel := "Save WAV"
	if t.fdaExporting {
		exportLabel = "Saving…"
	} else if t.fdaExportDone != "" {
		exportLabel = "Saved"
	}
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if isPlaying {
							return ui.primaryButton(gtx, &t.fdaPlayPause, playLabel)
						}
						return ui.smallButton(gtx, &t.fdaPlayPause, playLabel)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.fdaStopBtn, "Stop") }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bg := cSaveBg
						if t.fdaRepeat {
							bg = cMenuOpen
						}
						return ui.toggleButton(gtx, &t.fdaRepeatBtn, "Repeat", t.fdaRepeat, bg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &t.fdaExportBtn, exportLabel, t.fdaExporting)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := "Vol"
						if t.fdaVolumePct == 0 {
							lbl = "Mute"
						}
						l := material.Label(ui.th, unit.Sp(9), lbl)
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(70))
						gtx.Constraints.Max.X = gtx.Dp(unit.Dp(110))
						slider := material.Slider(ui.th, &t.fdaVolFloat)
						slider.Color = rgb(0x6B6B6B)
						dims := slider.Layout(gtx)
						ui.fdaHandleVolumeSlider(t)
						return dims
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("%d%%", t.fdaVolumePct))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmtDuration(pos))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutWaveform(gtx, t) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmtDuration(t.fdaTotal))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if audioplayer.SampleRate != 0 && (audioplayer.SampleRate != t.fdaSampleRate || audioplayer.Channels != t.fdaChannels) {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("⚠ context %d Hz/%d ch ≠ %d Hz/%d ch", audioplayer.SampleRate, audioplayer.Channels, t.fdaSampleRate, t.fdaChannels))
						l.Color = rgb(0xB00020)
						return l.Layout(gtx)
					})
				}
				if t.fdaExportDone != "" {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("WAV: %s", filepath.Base(t.fdaExportDone)))
						l.Color = rgb(0x2E7D32)
						return l.Layout(gtx)
					})
				}
				return layout.Dimensions{}
			}),
		)
	})
}
