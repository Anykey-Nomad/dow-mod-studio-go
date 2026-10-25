package main

import (
	"bytes"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"
)

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
