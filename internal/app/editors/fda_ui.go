package editors

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"fmt"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"io"
	"path/filepath"
	"strings"
	"time"
)

func FdaPropertiesTable(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
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
			l.Color = uikit.Rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	info := t.FdaFile.Info
	bitrateK := float64(info.BlockBitrate) * 86.1328125 * float64(info.Channels) / 1000.0
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "File", filepath.Base(t.Path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Channels", fmt.Sprintf("%d", info.Channels), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Sample Rate", fmt.Sprintf("%d Hz", info.SampleRate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Bits", fmt.Sprintf("%d", info.SampleSize), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Block Bitrate", fmt.Sprintf("%d", info.BlockBitrate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Bitrate", fmt.Sprintf("%.1f kbps", bitrateK), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Duration", FmtDuration(t.FdaTotal), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Position", FmtDuration(FdaPosFor(ui, t)), true)
		}),
	)
}

func FdaInfoTable(ui *uistate.State, gtx layout.Context, t *uistate.Tab) layout.Dimensions {
	if t.FdaFile == nil {
		return layout.Dimensions{}
	}
	fb := t.FdaFile.FBIF
	hdr := t.FdaFile.Header
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Version", fmt.Sprintf("%d.%d", hdr.MajorVer, hdr.MinorVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Plugin", fb.PluginName, true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Plugin Ver", fmt.Sprintf("%d", fb.PluginVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.TableRow2(ui, gtx, "User", fb.UserName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Burn Time", fb.BurnTime, false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "BeginLoop", fmt.Sprintf("%d", t.FdaFile.Info.BeginLoop), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "EndLoop", fmt.Sprintf("%d", t.FdaFile.Info.EndLoop), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "StartOffset", fmt.Sprintf("%d", t.FdaFile.Info.StartOffset), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Raw Data", fmt.Sprintf("%d bytes", len(t.FdaFile.RawData)), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return uikit.TableRow2(ui, gtx, "Decoded", fmt.Sprintf("%d bytes", len(t.FdaPcmBytes)), true)
		}),
	)
}

func FdaUpdateSliders(ui *uistate.State, gtx layout.Context, t *uistate.Tab) {
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

func FdaHandleSliderSeek(ui *uistate.State, gtx layout.Context, t *uistate.Tab) {
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

func FdaHandleVolumeSlider(ui *uistate.State, t *uistate.Tab) {
	if t.FdaVolFloat.Dragging() {
		pct := int(t.FdaVolFloat.Value*200 + 0.5)
		FdaSetVolumeFor(ui, t, pct)
	}
}

func FmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Millisecond)
	totalSec := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", totalSec/60, totalSec%60)
}

func BuildWavePeaks(pcm []byte, w int) []byte {
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

func FdaPosFor(ui *uistate.State, t *uistate.Tab) time.Duration {
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

func FdaSetVolumeFor(ui *uistate.State, t *uistate.Tab, pct int) {
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

func FdaPauseAllOthers(ui *uistate.State, keep *uistate.Tab) {
	for _, tab := range ui.OpenTabs {
		if tab != keep && tab.Kind == "fda" && tab.FdaPlayer != nil {
			if !tab.FdaPaused {
				tab.FdaPlayer.PauseAndStopReading()
				tab.FdaPaused = true
			}
		}
	}
}

func FdaTargetWAV(virtualPath string) string {
	return strings.TrimSuffix(virtualPath, filepath.Ext(virtualPath)) + ".wav"
}
