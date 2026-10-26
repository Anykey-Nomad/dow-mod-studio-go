package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (ui *uiState) fdaPropertiesTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.fdaFile == nil && t.fdaError == "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), "No FDA selected")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	if t.fdaError != "" {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), t.fdaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	info := t.fdaFile.Info
	bitrateK := float64(info.BlockBitrate) * 86.1328125 * float64(info.Channels) / 1000.0
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "File", filepath.Base(t.path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Channels", fmt.Sprintf("%d", info.Channels), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Sample Rate", fmt.Sprintf("%d Hz", info.SampleRate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Bits", fmt.Sprintf("%d", info.SampleSize), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Block Bitrate", fmt.Sprintf("%d", info.BlockBitrate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Bitrate", fmt.Sprintf("%.1f kbps", bitrateK), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Duration", fmtDuration(t.fdaTotal), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Position", fmtDuration(ui.fdaPosFor(t)), true)
		}),
	)
}

func (ui *uiState) fdaInfoTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.fdaFile == nil {
		return layout.Dimensions{}
	}
	fb := t.fdaFile.FBIF
	hdr := t.fdaFile.Header
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Version", fmt.Sprintf("%d.%d", hdr.MajorVer, hdr.MinorVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Plugin", fb.PluginName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Plugin Ver", fmt.Sprintf("%d", fb.PluginVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "User", fb.UserName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Burn Time", fb.BurnTime, false) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "BeginLoop", fmt.Sprintf("%d", t.fdaFile.Info.BeginLoop), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "EndLoop", fmt.Sprintf("%d", t.fdaFile.Info.EndLoop), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "StartOffset", fmt.Sprintf("%d", t.fdaFile.Info.StartOffset), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Raw Data", fmt.Sprintf("%d bytes", len(t.fdaFile.RawData)), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Decoded", fmt.Sprintf("%d bytes", len(t.fdaPcmBytes)), true)
		}),
	)
}

// ─── merged from fda_layout.go ───────────────────────────────────────────────

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

// ─── merged from fda_player.go ───────────────────────────────────────────────

// ── uiState FDA helpers — per fileTab ─────────────────────────────────────────

func (ui *uiState) fdaPosFor(t *fileTab) time.Duration {
	if t.fdaPlayer == nil || t.fdaSrc == nil || t.fdaChunkBuf == nil || t.fdaBytesPerSec == 0 {
		return 0
	}
	read := t.fdaSrc.BytePos()
	buffered := int64(t.fdaPlayer.BufferedSize())
	heard := read - buffered
	if heard < 0 {
		heard = 0
	}
	if heard >= int64(t.fdaChunkBuf.TotalBytes()) {
		return t.fdaTotal
	}
	if t.fdaChunkBuf.ChunkBytes() > 0 {
		heard -= heard % int64(t.fdaChunkBuf.ChunkBytes())
	}
	return time.Duration(float64(heard) / float64(t.fdaBytesPerSec) * float64(time.Second))
}

func (ui *uiState) fdaPos() time.Duration {
	if tab := ui.activeTab(); tab != nil && tab.kind == "fda" {
		return ui.fdaPosFor(tab)
	}
	return 0
}

func (ui *uiState) fdaSeekBy(delta time.Duration) {
	t := ui.activeTab()
	if t == nil || t.kind != "fda" || t.fdaPlayer == nil || t.fdaSrc == nil || t.fdaChunkBuf == nil {
		return
	}
	target := ui.fdaPosFor(t) + delta
	if target < 0 {
		target = 0
	}
	if target > t.fdaTotal {
		target = t.fdaTotal
	}
	b := int64(float64(target) / float64(time.Second) * float64(t.fdaBytesPerSec))
	if t.fdaChunkBuf.ChunkBytes() > 0 {
		b -= b % int64(t.fdaChunkBuf.ChunkBytes())
	}
	if _, err := t.fdaPlayer.Seek(b, io.SeekStart); err != nil {
		ui.statusText = fmt.Sprintf("Seek error: %v", err)
		log.Printf("fdaSeekBy: %v", err)
	} else {
		ui.statusText = fmt.Sprintf("Seek %+s → %s", delta, target.Truncate(time.Millisecond))
	}
}

func (ui *uiState) fdaTogglePlayPause() {
	t := ui.activeTab()
	if t == nil || t.fdaPlayer == nil {
		return
	}
	if t.fdaPaused {
		ui.fdaPauseAllOthers(t)
		// After the source reaches EOF oto keeps the player stopped. Rewind
		// explicitly so Play can start the FDA again without requiring Stop.
		if t.fdaSrc != nil && t.fdaSrc.Remaining() <= 0 {
			if _, err := t.fdaPlayer.Seek(0, io.SeekStart); err != nil {
				ui.statusText = fmt.Sprintf("Replay seek failed: %v", err)
				return
			}
		}
		t.fdaPlayer.Play()
		t.fdaPaused = false
		ui.statusText = fmt.Sprintf("▶ %s", ui.fdaPosFor(t).Truncate(time.Millisecond))
	} else {
		t.fdaPlayer.PauseAndStopReading()
		t.fdaPaused = true
		ui.statusText = fmt.Sprintf("⏸ %s", ui.fdaPosFor(t).Truncate(time.Millisecond))
	}
}

func (ui *uiState) fdaSetVolumeFor(t *fileTab, pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 200 {
		pct = 200
	}
	t.fdaVolumePct = pct
	if t.fdaPlayer != nil {
		t.fdaPlayer.SetVolume(float64(pct) / 100)
	}
}

func (ui *uiState) fdaSetVolume(pct int) {
	t := ui.activeTab()
	if t == nil || t.kind != "fda" {
		return
	}
	ui.fdaSetVolumeFor(t, pct)
}

func (ui *uiState) fdaPauseAllOthers(keep *fileTab) {
	for _, tab := range ui.openTabs {
		if tab != keep && tab.kind == "fda" && tab.fdaPlayer != nil {
			if !tab.fdaPaused {
				tab.fdaPlayer.PauseAndStopReading()
				tab.fdaPaused = true
			}
		}
	}
}

func fdaTargetWAV(virtualPath string) string {
	return strings.TrimSuffix(virtualPath, filepath.Ext(virtualPath)) + ".wav"
}

func (ui *uiState) fdaExportWav() {
	t := ui.activeTab()
	if t == nil || t.kind != "fda" || t.fdaFile == nil || len(t.fdaPcmBytes) == 0 || t.fdaExporting {
		return
	}
	t.fdaExporting = true
	t.fdaExportDone = ""
	target := fdaTargetWAV(t.path)
	out, err := ui.modTree.OverlayPath(target)
	if err != nil {
		t.fdaExporting = false
		ui.statusText = fmt.Sprintf("Export failed: %v", err)
		return
	}
	data := make([]byte, len(t.fdaPcmBytes))
	copy(data, t.fdaPcmBytes)
	info := t.fdaFile.Info
	// Playback PCM is converted to the process-wide oto context format, so
	// exported WAV metadata must describe the converted stream as well.
	info.SampleRate = uint32(t.fdaSampleRate)
	info.Channels = uint32(t.fdaChannels)
	go func(tab *fileTab, outPath string) {
		err := fda.WriteWAV(outPath, &info, data)
		if err != nil {
			tab.fdaExporting = false
			ui.statusText = fmt.Sprintf("Export failed: %v", err)
			log.Printf("fdaExportWav: %v", err)
			return
		}
		tab.fdaExporting = false
		tab.fdaExportDone = outPath
		ui.statusText = fmt.Sprintf("Exported WAV: %s", outPath)
		log.Printf("fdaExportWav: wrote %s", outPath)
	}(t, out)
}

func (ui *uiState) openFda(virtualPath string) {
	if idx := ui.findTabIndex(virtualPath); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	// A virtual path comes from the open mod's tree; an absolute one is a
	// file the OS handed the studio through a file association.
	data, err := ui.readTabFile(virtualPath)
	if err != nil {
		tt := &fileTab{path: virtualPath, kind: "fda", fdaError: err.Error(), fdaVolumePct: 100}
		tt.fdaVolFloat.Value = 0.5
		ui.openTabs = append(ui.openTabs, tt)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	parsed, err := fda.ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		tt := &fileTab{path: virtualPath, kind: "fda", fdaError: err.Error(), fdaVolumePct: 100}
		tt.fdaVolFloat.Value = 0.5
		ui.openTabs = append(ui.openTabs, tt)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Failed to parse %s: %v", filepath.Base(virtualPath), err)
		return
	}
	pcm, err := fda.DecodeFDAToPCM(parsed)
	if err != nil {
		tt := &fileTab{path: virtualPath, kind: "fda", fdaError: err.Error(), fdaVolumePct: 100}
		tt.fdaVolFloat.Value = 0.5
		ui.openTabs = append(ui.openTabs, tt)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Decode failed %s: %v", filepath.Base(virtualPath), err)
		return
	}
	sr := int(parsed.Info.SampleRate)
	ch := int(parsed.Info.Channels)
	if sr == 0 || ch == 0 {
		tt := &fileTab{path: virtualPath, kind: "fda", fdaError: "invalid INFO (zero sample rate or channels)", fdaVolumePct: 100}
		tt.fdaVolFloat.Value = 0.5
		ui.openTabs = append(ui.openTabs, tt)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = tt.fdaError
		return
	}
	ctx, err := audioplayer.Context()
	if err != nil {
		tt := &fileTab{path: virtualPath, kind: "fda", fdaError: fmt.Sprintf("audio init: %v", err), fdaVolumePct: 100}
		tt.fdaVolFloat.Value = 0.5
		ui.openTabs = append(ui.openTabs, tt)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = tt.fdaError
		return
	}
	if sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		pcm, err = fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			tt := &fileTab{path: virtualPath, kind: "fda", fdaError: fmt.Sprintf("audio conversion: %v", err), fdaVolumePct: 100}
			tt.fdaVolFloat.Value = 0.5
			ui.openTabs = append(ui.openTabs, tt)
			ui.tabSel = len(ui.openTabs)
			ui.ensureTabClicks()
			ui.statusText = tt.fdaError
			return
		}
		sr, ch = audioplayer.SampleRate, audioplayer.Channels
	}
	pcmBytes := fda.PCM16Bytes(pcm)
	bps := int64(sr * ch * 2)
	total := time.Duration(float64(len(pcmBytes)) / float64(bps) * float64(time.Second))
	cb := audioplayer.NewChunkBuffer(pcmBytes, sr, ch)
	src := audioplayer.NewChunkSource(cb)
	player := ctx.NewPlayer(src)
	player.SetVolume(1.0)
	// enforce single playback: pause any other playing FDA before starting this one
	// new tab not yet in openTabs, so pause all existing
	ui.fdaPauseAllOthers(nil)
	ui.pauseOtherWavPlayers(nil)
	player.Play()
	tt := &fileTab{
		path: virtualPath, kind: "fda",
		fdaFile: parsed, fdaError: "",
		fdaPcmBytes:   pcmBytes,
		fdaSampleRate: sr, fdaChannels: ch,
		fdaBytesPerSec: bps, fdaTotal: total,
		fdaChunkBuf: cb, fdaSrc: src, fdaPlayer: player,
		fdaPaused: false, fdaRepeat: false, fdaVolumePct: 100,
	}
	tt.fdaSeekFloat.Value = 0
	tt.fdaVolFloat.Value = 0.5
	ui.openTabs = append(ui.openTabs, tt)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	ui.statusText = fmt.Sprintf("Playing %s — %d Hz %d ch %s", filepath.Base(virtualPath), sr, ch, total.Truncate(time.Millisecond))
	if audioplayer.SampleRate != sr || audioplayer.Channels != ch {
		ui.statusText += fmt.Sprintf(" ⚠ context %d Hz/%d ch", audioplayer.SampleRate, audioplayer.Channels)
	}
	log.Printf("openFda %q: %d Hz %d ch %d bytes total %s chunks %d", virtualPath, sr, ch, len(pcmBytes), total, cb.NumChunks())
}

func (ui *uiState) checkFdaAutoRepeat() {
	t := ui.activeTab()
	if t == nil || t.kind != "fda" || t.fdaPlayer == nil || t.fdaSrc == nil {
		return
	}
	if t.fdaPaused {
		return
	}
	if !t.fdaPlayer.IsPlaying() {
		if err := t.fdaPlayer.Err(); err != nil {
			ui.statusText = fmt.Sprintf("Audio error: %v", err)
			t.fdaPlayer.PauseAndStopReading()
			t.fdaPaused = true
			return
		}
		if t.fdaSrc.Remaining() <= 0 {
			if t.fdaRepeat {
				if _, err := t.fdaPlayer.Seek(0, io.SeekStart); err != nil {
					ui.statusText = fmt.Sprintf("Repeat seek failed: %v", err)
					t.fdaPaused = true
					return
				}
				t.fdaPlayer.Play()
				ui.statusText = "Repeat: restart"
				return
			}
			t.fdaPlayer.PauseAndStopReading()
			t.fdaPaused = true
			ui.statusText = "Track ended"
			return
		}
		t.fdaPlayer.Play()
	}
}
