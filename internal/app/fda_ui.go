package app

import (
	"bytes"
	"dow-mod-studio/internal/app/uikit"
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

func FdaPropertiesTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.FdaFile == nil && t.FdaError == "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), "No FDA selected")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	if t.FdaError != "" {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), t.FdaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	info := t.FdaFile.Info
	bitrateK := float64(info.BlockBitrate) * 86.1328125 * float64(info.Channels) / 1000.0
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "File", filepath.Base(t.Path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Channels", fmt.Sprintf("%d", info.Channels), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Sample Rate", fmt.Sprintf("%d Hz", info.SampleRate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Bits", fmt.Sprintf("%d", info.SampleSize), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Block Bitrate", fmt.Sprintf("%d", info.BlockBitrate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Bitrate", fmt.Sprintf("%.1f kbps", bitrateK), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Duration", fmtDuration(t.FdaTotal), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Position", fmtDuration(FdaPosFor(ui, t)), true)
		}),
	)
}

func FdaInfoTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.FdaFile == nil {
		return layout.Dimensions{}
	}
	fb := t.FdaFile.FBIF
	hdr := t.FdaFile.Header
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Version", fmt.Sprintf("%d.%d", hdr.MajorVer, hdr.MinorVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, "Plugin", fb.PluginName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Plugin Ver", fmt.Sprintf("%d", fb.PluginVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, "User", fb.UserName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, "Burn Time", fb.BurnTime, false) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "BeginLoop", fmt.Sprintf("%d", t.FdaFile.Info.BeginLoop), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "EndLoop", fmt.Sprintf("%d", t.FdaFile.Info.EndLoop), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "StartOffset", fmt.Sprintf("%d", t.FdaFile.Info.StartOffset), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Raw Data", fmt.Sprintf("%d bytes", len(t.FdaFile.RawData)), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRow2(ui, gtx, "Decoded", fmt.Sprintf("%d bytes", len(t.FdaPcmBytes)), true)
		}),
	)
}

// ─── merged from fda_layout.go ───────────────────────────────────────────────

func FdaUpdateSliders(ui *uiState, gtx layout.Context, t *fileTab) {
	if t.FdaTotal > 0 && !t.FdaSeekFloat.Dragging() {
		pos := FdaPosFor(ui, t)
		frac := float32(pos) / float32(t.FdaTotal)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		t.FdaSeekFloat.Value = frac
	}
	if t.FdaVolumePct < 0 {
		t.FdaVolumePct = 0
	}
	if t.FdaVolumePct > 200 {
		t.FdaVolumePct = 200
	}
	wantVolFrac := float32(t.FdaVolumePct) / 200
	if !t.FdaVolFloat.Dragging() {
		t.FdaVolFloat.Value = wantVolFrac
	}
}

func FdaHandleSliderSeek(ui *uiState, gtx layout.Context, t *fileTab) {
	if t.FdaTotal == 0 || t.FdaChunkBuf == nil || t.FdaPlayer == nil || !t.FdaSeekFloat.Dragging() {
		return
	}
	target := time.Duration(float64(t.FdaSeekFloat.Value) * float64(t.FdaTotal))
	cur := FdaPosFor(ui, t)
	diff := target - cur
	if diff < 0 {
		diff = -diff
	}
	if diff > 250*time.Millisecond {
		b := int64(float64(target) / float64(time.Second) * float64(t.FdaBytesPerSec))
		if t.FdaChunkBuf.ChunkBytes() > 0 {
			b -= b % int64(t.FdaChunkBuf.ChunkBytes())
		}
		_, _ = t.FdaPlayer.Seek(b, io.SeekStart)
	}
}

func FdaHandleVolumeSlider(ui *uiState, t *fileTab) {
	if t.FdaVolFloat.Dragging() {
		pct := int(t.FdaVolFloat.Value*200 + 0.5)
		FdaSetVolumeFor(ui, t, pct)
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

func LayoutFdaPlayer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	FdaUpdateSliders(ui, gtx, t)
	if t.FdaPlayer != nil && !t.FdaPaused {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
	}
	if t.FdaError != "" {
		return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.FdaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.FdaFile == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "No FDA selected")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	pos := FdaPosFor(ui, t)
	isPlaying := !t.FdaPaused
	playLabel := "Play"
	if isPlaying {
		playLabel = "Pause"
	}
	exportLabel := "Save WAV"
	if t.FdaExporting {
		exportLabel = "Saving…"
	} else if t.FdaExportDone != "" {
		exportLabel = "Saved"
	}
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if isPlaying {
							return PrimaryButton(ui, gtx, &t.FdaPlayPause, playLabel)
						}
						return SmallButton(ui, gtx, &t.FdaPlayPause, playLabel)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.FdaStopBtn, "Stop") }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bg := uikit.CSaveBg
						if t.FdaRepeat {
							bg = uikit.CMenuOpen
						}
						return ToggleButton(ui, gtx, &t.FdaRepeatBtn, "Repeat", t.FdaRepeat, bg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &t.FdaExportBtn, exportLabel, t.FdaExporting)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := "Vol"
						if t.FdaVolumePct == 0 {
							lbl = "Mute"
						}
						l := material.Label(ui.Th, unit.Sp(9), lbl)
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(70))
						gtx.Constraints.Max.X = gtx.Dp(unit.Dp(110))
						slider := material.Slider(ui.Th, &t.FdaVolFloat)
						slider.Color = rgb(0x6B6B6B)
						dims := slider.Layout(gtx)
						FdaHandleVolumeSlider(ui, t)
						return dims
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d%%", t.FdaVolumePct))
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmtDuration(pos))
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWaveform(ui, gtx, t) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmtDuration(t.FdaTotal))
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if audioplayer.SampleRate != 0 && (audioplayer.SampleRate != t.FdaSampleRate || audioplayer.Channels != t.FdaChannels) {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("⚠ context %d Hz/%d ch ≠ %d Hz/%d ch", audioplayer.SampleRate, audioplayer.Channels, t.FdaSampleRate, t.FdaChannels))
						l.Color = rgb(0xB00020)
						return l.Layout(gtx)
					})
				}
				if t.FdaExportDone != "" {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("WAV: %s", filepath.Base(t.FdaExportDone)))
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

func FdaPosFor(ui *uiState, t *fileTab) time.Duration {
	if t.FdaPlayer == nil || t.FdaSrc == nil || t.FdaChunkBuf == nil || t.FdaBytesPerSec == 0 {
		return 0
	}
	read := t.FdaSrc.BytePos()
	buffered := int64(t.FdaPlayer.BufferedSize())
	heard := read - buffered
	if heard < 0 {
		heard = 0
	}
	if heard >= int64(t.FdaChunkBuf.TotalBytes()) {
		return t.FdaTotal
	}
	if t.FdaChunkBuf.ChunkBytes() > 0 {
		heard -= heard % int64(t.FdaChunkBuf.ChunkBytes())
	}
	return time.Duration(float64(heard) / float64(t.FdaBytesPerSec) * float64(time.Second))
}

func FdaPos(ui *uiState) time.Duration {
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "fda" {
		return FdaPosFor(ui, tab)
	}
	return 0
}

func FdaSeekBy(ui *uiState, delta time.Duration) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "fda" || t.FdaPlayer == nil || t.FdaSrc == nil || t.FdaChunkBuf == nil {
		return
	}
	target := FdaPosFor(ui, t) + delta
	if target < 0 {
		target = 0
	}
	if target > t.FdaTotal {
		target = t.FdaTotal
	}
	b := int64(float64(target) / float64(time.Second) * float64(t.FdaBytesPerSec))
	if t.FdaChunkBuf.ChunkBytes() > 0 {
		b -= b % int64(t.FdaChunkBuf.ChunkBytes())
	}
	if _, err := t.FdaPlayer.Seek(b, io.SeekStart); err != nil {
		ui.StatusText = fmt.Sprintf("Seek error: %v", err)
		log.Printf("fdaSeekBy: %v", err)
	} else {
		ui.StatusText = fmt.Sprintf("Seek %+s → %s", delta, target.Truncate(time.Millisecond))
	}
}

func FdaTogglePlayPause(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.FdaPlayer == nil {
		return
	}
	if t.FdaPaused {
		FdaPauseAllOthers(ui, t)
		// After the source reaches EOF oto keeps the player stopped. Rewind
		// explicitly so Play can start the FDA again without requiring Stop.
		if t.FdaSrc != nil && t.FdaSrc.Remaining() <= 0 {
			if _, err := t.FdaPlayer.Seek(0, io.SeekStart); err != nil {
				ui.StatusText = fmt.Sprintf("Replay seek failed: %v", err)
				return
			}
		}
		t.FdaPlayer.Play()
		t.FdaPaused = false
		ui.StatusText = fmt.Sprintf("▶ %s", FdaPosFor(ui, t).Truncate(time.Millisecond))
	} else {
		t.FdaPlayer.PauseAndStopReading()
		t.FdaPaused = true
		ui.StatusText = fmt.Sprintf("⏸ %s", FdaPosFor(ui, t).Truncate(time.Millisecond))
	}
}

func FdaSetVolumeFor(ui *uiState, t *fileTab, pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 200 {
		pct = 200
	}
	t.FdaVolumePct = pct
	if t.FdaPlayer != nil {
		t.FdaPlayer.SetVolume(float64(pct) / 100)
	}
}

func FdaSetVolume(ui *uiState, pct int) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "fda" {
		return
	}
	FdaSetVolumeFor(ui, t, pct)
}

func FdaPauseAllOthers(ui *uiState, keep *fileTab) {
	for _, tab := range ui.OpenTabs {
		if tab != keep && tab.Kind == "fda" && tab.FdaPlayer != nil {
			if !tab.FdaPaused {
				tab.FdaPlayer.PauseAndStopReading()
				tab.FdaPaused = true
			}
		}
	}
}

func fdaTargetWAV(virtualPath string) string {
	return strings.TrimSuffix(virtualPath, filepath.Ext(virtualPath)) + ".wav"
}

func FdaExportWav(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "fda" || t.FdaFile == nil || len(t.FdaPcmBytes) == 0 || t.FdaExporting {
		return
	}
	t.FdaExporting = true
	t.FdaExportDone = ""
	target := fdaTargetWAV(t.Path)
	out, err := ui.ModTree.OverlayPath(target)
	if err != nil {
		t.FdaExporting = false
		ui.StatusText = fmt.Sprintf("Export failed: %v", err)
		return
	}
	data := make([]byte, len(t.FdaPcmBytes))
	copy(data, t.FdaPcmBytes)
	info := t.FdaFile.Info
	// Playback PCM is converted to the process-wide oto context format, so
	// exported WAV metadata must describe the converted stream as well.
	info.SampleRate = uint32(t.FdaSampleRate)
	info.Channels = uint32(t.FdaChannels)
	go func(tab *fileTab, outPath string) {
		err := fda.WriteWAV(outPath, &info, data)
		if err != nil {
			tab.FdaExporting = false
			ui.StatusText = fmt.Sprintf("Export failed: %v", err)
			log.Printf("fdaExportWav: %v", err)
			return
		}
		tab.FdaExporting = false
		tab.FdaExportDone = outPath
		ui.StatusText = fmt.Sprintf("Exported WAV: %s", outPath)
		log.Printf("fdaExportWav: wrote %s", outPath)
	}(t, out)
}

func OpenFda(ui *uiState, virtualPath string) {
	if idx := FindTabIndex(ui, virtualPath); idx >= 0 {
		ui.TabSel = idx + 1
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	// A virtual path comes from the open mod's tree; an absolute one is a
	// file the OS handed the studio through a file association.
	data, err := ReadTabFile(ui, virtualPath)
	if err != nil {
		tt := &fileTab{Path: virtualPath, Kind: "fda", FdaError: err.Error(), FdaVolumePct: 100}
		tt.FdaVolFloat.Value = 0.5
		ui.OpenTabs = append(ui.OpenTabs, tt)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	parsed, err := fda.ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		tt := &fileTab{Path: virtualPath, Kind: "fda", FdaError: err.Error(), FdaVolumePct: 100}
		tt.FdaVolFloat.Value = 0.5
		ui.OpenTabs = append(ui.OpenTabs, tt)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Failed to parse %s: %v", filepath.Base(virtualPath), err)
		return
	}
	pcm, err := fda.DecodeFDAToPCM(parsed)
	if err != nil {
		tt := &fileTab{Path: virtualPath, Kind: "fda", FdaError: err.Error(), FdaVolumePct: 100}
		tt.FdaVolFloat.Value = 0.5
		ui.OpenTabs = append(ui.OpenTabs, tt)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Decode failed %s: %v", filepath.Base(virtualPath), err)
		return
	}
	sr := int(parsed.Info.SampleRate)
	ch := int(parsed.Info.Channels)
	if sr == 0 || ch == 0 {
		tt := &fileTab{Path: virtualPath, Kind: "fda", FdaError: "invalid INFO (zero sample rate or channels)", FdaVolumePct: 100}
		tt.FdaVolFloat.Value = 0.5
		ui.OpenTabs = append(ui.OpenTabs, tt)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = tt.FdaError
		return
	}
	ctx, err := audioplayer.Context()
	if err != nil {
		tt := &fileTab{Path: virtualPath, Kind: "fda", FdaError: fmt.Sprintf("audio init: %v", err), FdaVolumePct: 100}
		tt.FdaVolFloat.Value = 0.5
		ui.OpenTabs = append(ui.OpenTabs, tt)
		ui.TabSel = len(ui.OpenTabs)
		EnsureTabClicks(ui)
		ui.StatusText = tt.FdaError
		return
	}
	if sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		pcm, err = fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			tt := &fileTab{Path: virtualPath, Kind: "fda", FdaError: fmt.Sprintf("audio conversion: %v", err), FdaVolumePct: 100}
			tt.FdaVolFloat.Value = 0.5
			ui.OpenTabs = append(ui.OpenTabs, tt)
			ui.TabSel = len(ui.OpenTabs)
			EnsureTabClicks(ui)
			ui.StatusText = tt.FdaError
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
	FdaPauseAllOthers(ui, nil)
	PauseOtherWavPlayers(ui, nil)
	player.Play()
	tt := &fileTab{
		Path: virtualPath, Kind: "fda",
		FdaFile: parsed, FdaError: "",
		FdaPcmBytes:   pcmBytes,
		FdaSampleRate: sr, FdaChannels: ch,
		FdaBytesPerSec: bps, FdaTotal: total,
		FdaChunkBuf: cb, FdaSrc: src, FdaPlayer: player,
		FdaPaused: false, FdaRepeat: false, FdaVolumePct: 100,
	}
	tt.FdaSeekFloat.Value = 0
	tt.FdaVolFloat.Value = 0.5
	ui.OpenTabs = append(ui.OpenTabs, tt)
	ui.TabSel = len(ui.OpenTabs)
	EnsureTabClicks(ui)
	ui.StatusText = fmt.Sprintf("Playing %s — %d Hz %d ch %s", filepath.Base(virtualPath), sr, ch, total.Truncate(time.Millisecond))
	if audioplayer.SampleRate != sr || audioplayer.Channels != ch {
		ui.StatusText += fmt.Sprintf(" ⚠ context %d Hz/%d ch", audioplayer.SampleRate, audioplayer.Channels)
	}
	log.Printf("openFda %q: %d Hz %d ch %d bytes total %s chunks %d", virtualPath, sr, ch, len(pcmBytes), total, cb.NumChunks())
}

func CheckFdaAutoRepeat(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "fda" || t.FdaPlayer == nil || t.FdaSrc == nil {
		return
	}
	if t.FdaPaused {
		return
	}
	if !t.FdaPlayer.IsPlaying() {
		if err := t.FdaPlayer.Err(); err != nil {
			ui.StatusText = fmt.Sprintf("Audio error: %v", err)
			t.FdaPlayer.PauseAndStopReading()
			t.FdaPaused = true
			return
		}
		if t.FdaSrc.Remaining() <= 0 {
			if t.FdaRepeat {
				if _, err := t.FdaPlayer.Seek(0, io.SeekStart); err != nil {
					ui.StatusText = fmt.Sprintf("Repeat seek failed: %v", err)
					t.FdaPaused = true
					return
				}
				t.FdaPlayer.Play()
				ui.StatusText = "Repeat: restart"
				return
			}
			t.FdaPlayer.PauseAndStopReading()
			t.FdaPaused = true
			ui.StatusText = "Track ended"
			return
		}
		t.FdaPlayer.Play()
	}
}
