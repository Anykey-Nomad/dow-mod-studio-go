package main

import (
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/wav"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (ui *uiState) openWav(virtualPath string) {
	if ui.modTree == nil {
		return
	}
	if idx := ui.findTabIndex(virtualPath); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	data, err := ui.modTree.ReadFile(virtualPath)
	t := &fileTab{path: virtualPath, kind: "wav", wavVolumePct: 100}
	if err != nil {
		t.wavError = err.Error()
	} else {
		t.wavData, err = wav.Parse(data)
		if err != nil {
			t.wavError = err.Error()
		} else if err = ui.startWavPlayback(t); err != nil {
			t.wavError = err.Error()
		}
	}
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	if t.wavError != "" {
		ui.statusText = "WAV open failed: " + t.wavError
	} else {
		ui.statusText = "Opened " + filepath.Base(virtualPath)
	}
}

func wavPCM16(data []byte) []int16 {
	samples := make([]int16, len(data)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return samples
}

func (ui *uiState) pauseOtherWavPlayers(keep *fileTab) {
	for _, other := range ui.openTabs {
		if other != keep && other.kind == "wav" && other.wavPlayer != nil {
			other.wavPlayer.PauseAndStopReading()
			other.wavPaused = true
		}
	}
}

func (ui *uiState) startWavPlayback(t *fileTab) error {
	if t == nil || t.wavData == nil {
		return fmt.Errorf("no WAV data")
	}
	sr, ch := t.wavData.SampleRate, t.wavData.Channels
	ctx, err := audioplayer.Context()
	if err != nil {
		return fmt.Errorf("audio init: %w", err)
	}
	pcm := wavPCM16(t.wavData.PCMData)
	if sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		var err error
		pcm, err = fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			return fmt.Errorf("audio conversion: %w", err)
		}
		sr, ch = audioplayer.SampleRate, audioplayer.Channels
	}
	pcmBytes := fda.PCM16Bytes(pcm)
	cb := audioplayer.NewChunkBuffer(pcmBytes, sr, ch)
	src := audioplayer.NewChunkSource(cb)
	player := ctx.NewPlayer(src)
	player.SetVolume(float64(t.wavVolumePct) / 100)
	ui.fdaPauseAllOthers(nil)
	ui.pauseOtherWavPlayers(t)
	player.Play()
	t.wavPlayer = player
	t.wavSrc = src
	t.wavPaused = false
	t.wavBytesPerSec = int64(sr * ch * 2)
	t.wavTotal = time.Duration(float64(len(pcmBytes)) / float64(t.wavBytesPerSec) * float64(time.Second))
	return nil
}

func (ui *uiState) wavPosFor(t *fileTab) time.Duration {
	if t == nil || t.wavPlayer == nil || t.wavSrc == nil || t.wavBytesPerSec == 0 {
		return 0
	}
	heard := t.wavSrc.BytePos() - int64(t.wavPlayer.BufferedSize())
	if heard < 0 {
		heard = 0
	}
	if heard >= int64(t.wavSrc.TotalBytes()) {
		return t.wavTotal
	}
	return time.Duration(float64(heard) / float64(t.wavBytesPerSec) * float64(time.Second))
}

func (ui *uiState) wavSetVolume(t *fileTab, pct int) {
	if t == nil {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 200 {
		pct = 200
	}
	t.wavVolumePct = pct
	if t.wavPlayer != nil {
		t.wavPlayer.SetVolume(float64(pct) / 100)
	}
}

func (ui *uiState) wavUpdateSliders(t *fileTab) {
	if t == nil {
		return
	}
	if t.wavTotal > 0 && !t.wavSeekFloat.Dragging() {
		frac := float32(ui.wavPosFor(t)) / float32(t.wavTotal)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		t.wavSeekFloat.Value = frac
	}
	if t.wavVolumePct <= 0 && t.wavPlayer == nil {
		t.wavVolumePct = 100
	}
	if t.wavVolumePct < 0 {
		t.wavVolumePct = 0
	}
	if t.wavVolumePct > 200 {
		t.wavVolumePct = 200
	}
	if !t.wavVolFloat.Dragging() {
		t.wavVolFloat.Value = float32(t.wavVolumePct) / 200
	}
}

func (ui *uiState) wavHandleSliderSeek(t *fileTab) {
	if t == nil || t.wavTotal == 0 || t.wavSrc == nil || t.wavPlayer == nil || !t.wavSeekFloat.Dragging() {
		return
	}
	target := time.Duration(float64(t.wavSeekFloat.Value) * float64(t.wavTotal))
	cur := ui.wavPosFor(t)
	if diff := target - cur; diff > 250*time.Millisecond || diff < -250*time.Millisecond {
		b := int64(float64(target) / float64(time.Second) * float64(t.wavBytesPerSec))
		if t.wavSrc.ChunkBytes() > 0 {
			b -= b % int64(t.wavSrc.ChunkBytes())
		}
		_, _ = t.wavPlayer.Seek(b, io.SeekStart)
	}
}

func (ui *uiState) wavHandleVolumeSlider(t *fileTab) {
	if t != nil && t.wavVolFloat.Dragging() {
		ui.wavSetVolume(t, int(t.wavVolFloat.Value*200+0.5))
	}
}

func (ui *uiState) wavTogglePlayPause() {
	t := ui.activeTab()
	if t == nil || t.kind != "wav" || t.wavPlayer == nil {
		return
	}
	if t.wavPaused {
		ui.fdaPauseAllOthers(nil)
		ui.pauseOtherWavPlayers(t)
		if t.wavSrc.Remaining() <= 0 {
			if _, err := t.wavPlayer.Seek(0, io.SeekStart); err != nil {
				ui.statusText = fmt.Sprintf("WAV replay seek failed: %v", err)
				return
			}
		}
		t.wavPlayer.Play()
		t.wavPaused = false
		t.wavSeekFloat.Value = 0
		ui.statusText = "Playing " + filepath.Base(t.path)
	} else {
		t.wavPlayer.PauseAndStopReading()
		t.wavPaused = true
		ui.statusText = "Paused " + filepath.Base(t.path)
	}
}

func (ui *uiState) wavStop() {
	t := ui.activeTab()
	if t == nil || t.kind != "wav" || t.wavPlayer == nil {
		return
	}
	t.wavPlayer.PauseAndStopReading()
	_, _ = t.wavPlayer.Seek(0, io.SeekStart)
	t.wavPaused = true
	t.wavSeekFloat.Value = 0
	ui.statusText = "Stopped " + filepath.Base(t.path)
}

func (ui *uiState) checkWavAutoRepeat() {
	t := ui.activeTab()
	if t == nil || t.kind != "wav" || t.wavPlayer == nil || t.wavSrc == nil || t.wavPaused {
		return
	}
	if !t.wavPlayer.IsPlaying() {
		if err := t.wavPlayer.Err(); err != nil {
			t.wavPaused = true
			ui.statusText = fmt.Sprintf("WAV audio error: %v", err)
		} else if t.wavSrc.Remaining() <= 0 {
			if t.wavRepeat {
				if _, err := t.wavPlayer.Seek(0, io.SeekStart); err == nil {
					t.wavPlayer.Play()
					t.wavPaused = false
					t.wavSeekFloat.Value = 0
					ui.statusText = "Repeating " + filepath.Base(t.path)
					return
				}
			}
			t.wavPaused = true
			t.wavSeekFloat.Value = 1
			ui.statusText = "WAV playback finished"
		} else {
			t.wavPlayer.Play()
		}
	}
}

func wavDuration(t *fileTab) time.Duration {
	if t == nil || t.wavData == nil || t.wavData.SampleRate <= 0 || t.wavData.Channels <= 0 {
		return 0
	}
	return time.Duration(float64(len(t.wavData.PCMData)) / float64(t.wavData.SampleRate*t.wavData.Channels*2) * float64(time.Second))
}

func (ui *uiState) convertWavToFDA(t *fileTab) {
	if t == nil || t.kind != "wav" || t.wavData == nil || t.wavError != "" || t.wavExporting {
		return
	}
	t.wavExporting = true
	t.wavExportDone = ""
	fdaData, err := fda.EncodeWAV(t.wavData, 2048)
	if err != nil {
		t.wavExporting = false
		t.wavError = err.Error()
		ui.statusText = "WAV → FDA failed: " + err.Error()
		return
	}
	target := wavTargetFDA(t.path)
	diskPath, err := writeFDAOverlay(ui.modTree, target, fdaData)
	if err != nil {
		t.wavExporting = false
		t.wavError = err.Error()
		ui.statusText = "WAV → FDA failed: " + err.Error()
		return
	}
	t.wavExporting = false
	t.wavExportDone = diskPath
	ui.statusText = fmt.Sprintf("Converted %s → %s (%d bytes)", filepath.Base(t.path), diskPath, len(fdaData))
}

func (ui *uiState) layoutWavViewer(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	if t.wavError != "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "Failed: "+t.wavError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.wavData == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(11), "No WAV data")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	ui.wavUpdateSliders(t)
	if t.wavPlayer != nil && !t.wavPaused {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
	}
	if t.wavConvertClick.Clicked(gtx) {
		ui.convertWavToFDA(t)
	}
	pos := ui.wavPosFor(t)
	playLabel := "Play"
	if !t.wavPaused {
		playLabel = "Pause"
	}
	exportLabel := "Convert to FDA"
	if t.wavExporting {
		exportLabel = "Converting…"
	} else if t.wavExportDone != "" {
		exportLabel = "FDA created"
	}
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !t.wavPaused {
							return ui.primaryButton(gtx, &t.wavPlayClick, playLabel)
						}
						return ui.smallButton(gtx, &t.wavPlayClick, playLabel)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.smallButton(gtx, &t.wavStopClick, "Stop") }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bg := cSaveBg
						if t.wavRepeat {
							bg = cMenuOpen
						}
						return ui.toggleButton(gtx, &t.wavRepeatClick, "Repeat", t.wavRepeat, bg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.smallButtonDisabled(gtx, &t.wavConvertClick, exportLabel, t.wavExporting)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := "Vol"
						if t.wavVolumePct == 0 {
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
						slider := material.Slider(ui.th, &t.wavVolFloat)
						slider.Color = rgb(0x6B6B6B)
						dims := slider.Layout(gtx)
						ui.wavHandleVolumeSlider(t)
						return dims
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("%d%%", t.wavVolumePct))
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
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutWavWaveform(gtx, t) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(9), fmtDuration(t.wavTotal))
						l.Color = cTextDim
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.wavExportDone == "" {
					return layout.Dimensions{}
				}
				l := material.Label(ui.th, unit.Sp(9), fmt.Sprintf("FDA: %s", filepath.Base(t.wavExportDone)))
				l.Color = rgb(0x2E7D32)
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
		)
	})
}

func (ui *uiState) layoutWavWaveform(gtx layout.Context, t *fileTab) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	w := gtx.Constraints.Max.X
	if w < gtx.Dp(unit.Dp(80)) {
		w = gtx.Dp(unit.Dp(80))
	}
	sz := image.Pt(w, h)
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	dims := t.wavSeekFloat.Layout(gtx, layout.Horizontal, unit.Dp(4))
	ui.wavHandleSliderSeek(t)
	if len(t.wavWavePeaks) != w || t.wavWaveW != w {
		t.wavWavePeaks = buildWavePeaks(fda.PCM16Bytes(wavPCM16(t.wavData.PCMData)), w)
		t.wavWaveW = w
	}
	peaks := t.wavWavePeaks
	frac := t.wavSeekFloat.Value
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	playX := clampInt(int(float32(w)*frac), 0, w)

	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: cContentBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	b := widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })

	colPlayed := rgb(0x0078D4)
	colUnplayed := rgb(0x9EA8B3)
	for x := 0; x < w; x++ {
		p := 0
		if x < len(peaks) {
			p = int(peaks[x])
		}
		if p < 2 {
			p = 1
		}
		barH := (h - 6) * p / 255
		if barH < 2 {
			barH = 2
		}
		if barH > h-4 {
			barH = h - 4
		}
		y0 := (h - barH) / 2
		r := image.Rect(x, y0, x+1, y0+barH)
		if r.Min.X < 1 {
			r.Min.X = 1
		}
		if r.Max.X > w-1 {
			continue
		}
		st := clip.Rect(r).Push(gtx.Ops)
		if x < playX {
			paint.ColorOp{Color: colPlayed}.Add(gtx.Ops)
		} else {
			paint.ColorOp{Color: colUnplayed}.Add(gtx.Ops)
		}
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	if playX >= 1 && playX < w-1 {
		st := clip.Rect(image.Rect(playX, 1, playX+1, h-1)).Push(gtx.Ops)
		paint.ColorOp{Color: rgb(0xE81123)}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		knobR := gtx.Dp(unit.Dp(3))
		knobRect := image.Rect(playX-knobR, h/2-knobR, playX+knobR, h/2+knobR)
		paint.FillShape(gtx.Ops, rgb(0xE81123), clip.Ellipse(knobRect).Op(gtx.Ops))
	}
	return dims
}

func (ui *uiState) wavInfoTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil || t.wavData == nil {
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Name", filepath.Base(t.path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Format", "WAV PCM", true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Sample rate", fmt.Sprintf("%d Hz", t.wavData.SampleRate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Channels", fmt.Sprintf("%d", t.wavData.Channels), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Duration", fmtDuration(wavDuration(t)), false)
		}),
	)
}

// wavTargetFDA is the .fda a .wav converts to, at the same place in the mod.
func wavTargetFDA(virtualPath string) string {
	ext := filepath.Ext(virtualPath)
	return strings.TrimSuffix(virtualPath, ext) + ".fda"
}

// writeFDAOverlay writes converted audio into the mod folder, so ModFolder/Data
// resolution stays in one place.
func writeFDAOverlay(tree *modtree.ModTree, virtualPath string, data []byte) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("no mod loaded")
	}
	return tree.WriteOverlayFile(virtualPath, data)
}
