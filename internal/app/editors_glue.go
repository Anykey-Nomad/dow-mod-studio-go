package app

import (
	"bytes"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/locstrings"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"
)

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

func FdaSetVolume(ui *uiState, pct int) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "fda" {
		return
	}
	FdaSetVolumeFor(ui, t, pct)
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

// linkRows is the whole relationship list of one unit, in the order the list
// shows it: the roles the game itself groups by, in file order within each,
// and then the reverse links the same way.
func linkRows(g *modGraph, path string) []linkRow {
	if g == nil || path == "" {
		return nil
	}
	var out []linkRow
	for _, l := range g.Outgoing(path) {
		out = append(out, linkRow{
			Role: l.Role.Label(), Slot: l.Slot, Target: l.TargetName(),
			Path: l.To, Found: l.Resolved(), Outgoing: true,
		})
	}
	for _, l := range g.Incoming(path) {
		// A unit that refers to itself is its own production chain, and
		// saying so twice helps nobody.
		if strings.EqualFold(l.From, path) {
			continue
		}
		out = append(out, linkRow{
			Role: l.Role.Label(), Slot: l.Slot, Target: graphEntityName(g, l.From),
			Path: l.From, Found: l.Resolved(), Outgoing: false,
		})
	}
	return out
}

// graphEntityName is an entity's own name for itself, falling back to its file
// name when the graph does not know it — which happens for a path the
// inventory missed, and is worth showing rather than hiding.
func graphEntityName(g *modGraph, path string) string {
	if e := g.Entity(path); e != nil {
		return e.Name
	}
	return refStem(path)
}

// layoutUnitLinksList is the "Links" column.
func LayoutUnitLinksList(ui *uiState, gtx layout.Context) layout.Dimensions {
	sel := CurrentUnit(ui)
	if sel == nil {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	g := StartGraphLinkScan(ui)
	if g == nil {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	outgoing, incoming := groupLinkRows(linkRows(g, sel.Path))
	if len(outgoing) == 0 && len(incoming) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return LinkNote(ui, gtx, LinkScanNote(ui))
			})
		})
	}
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			add := func(title string, rows []linkRow) {
				if len(rows) == 0 {
					return
				}
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return LinkGroupTitle(ui, gtx, title, len(rows))
				}))
				for _, r := range rows {
					r := r
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return LinkRow(ui, gtx, r)
					}))
				}
			}
			add("This unit refers to", outgoing)
			add("Refers to this unit", incoming)
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LinkNote(ui, gtx, LinkScanNote(ui))
			}))
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// linkScanNote says how much of the mod the background read has covered, so a
// list that is still filling up does not look like a list that is finished.
//
// It counts the files that are actually read, not the units: the references
// live in abilities, add-ons, weapons, squads and research as much as in the
// units themselves, and a note that said "604 units" would be leaving out most
// of what it had just looked at.
func LinkScanNote(ui *uiState) string {
	g := GraphFor(ui)
	if g == nil {
		return "No mod is loaded."
	}
	files := len(g.EntitiesOfKind(graphLinkKinds()...))
	if ui.GraphLinksRunning {
		return "Reading the mod… " + itoa(len(g.Links)) + " references so far."
	}
	if files == 0 {
		return "The mod has no units, buildings, abilities, add-ons, weapons, squads or research."
	}
	return itoa(len(g.Links)) + " references read from " + itoa(files) + " files."
}

// layoutUnitProblemsList is the "Problems" column: every reference in the mod
// that names a file which is not there, one row per missing file rather than
// per occurrence.
func LayoutUnitProblemsList(ui *uiState, gtx layout.Context) layout.Dimensions {
	g := StartGraphLinkScan(ui)
	if g == nil {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	groups := brokenLinkGroups(g)
	if len(groups) == 0 {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				msg := "Every reference in the mod resolves."
				if ui.GraphLinksRunning {
					msg = "Reading the mod…"
				}
				return LinkNote(ui, gtx, msg)
			})
		})
	}
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LinkNote(ui, gtx, itoa(len(groups))+" references in the mod name a file that is not there.")
			}))
			for _, grp := range groups {
				grp := grp
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ProblemRow(ui, gtx, grp)
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// problemRow is one missing file and the files that still point at it.
func ProblemRow(ui *uiState, gtx layout.Context, grp brokenLinkGroup) layout.Dimensions {
	holders := itoa(len(grp.Holders))
	if len(grp.Holders) == 1 {
		holders = "1 file"
	} else {
		holders += " files"
	}
	return layout.Inset{Left: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					w := gtx.Dp(unit.Dp(150))
					gtx.Constraints.Min.X = w
					gtx.Constraints.Max.X = w
					l := material.Label(ui.Th, unit.Sp(10), grp.Role.Label())
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), grp.Ref)
						l.Color = uikit.CProblem
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return LinkNote(ui, gtx, "still named by "+holders+": "+clipName(holdersFirst(grp.Holders), 48))
					}),
				)
			}),
		)
	})
}

// holdersFirst is the first of the files pointing at a missing reference, which
// is the one worth naming; the rest are behind the count.
func holdersFirst(holders []string) string {
	if len(holders) == 0 {
		return ""
	}
	return refStem(holders[0])
}

// startGraphLinkScan reads every unit's and building's references in the
// background, into a fresh link list on the graph.
//
// A scan already running for the same tree is left alone, and a scan for a
// different tree is stopped rather than left writing into a graph nobody reads
// any more.
func StartGraphLinkScan(ui *uiState) *modGraph {
	g := GraphFor(ui)
	if g == nil {
		return nil
	}
	if ui.GraphLinksRunning && ui.GraphLinksGraph == g {
		return g
	}
	StopGraphLinkScan(ui)

	ui.GraphLinksGraph = g
	g.BeginLinkScan()
	ui.GraphLinksCh = make(chan graphLinkBatch, 8)
	stop := make(chan struct{})
	ui.GraphLinksStop = stop
	ui.GraphLinksRunning = true

	// The entities are copied so the goroutine does not walk a graph the UI
	// thread is free to replace.
	entities := append([]*graphEntity(nil), g.EntitiesOfKind(graphLinkKinds()...)...)
	ch := ui.GraphLinksCh
	go func() {
		batch := make([]graphLink, 0, graphLinkBatchSize)
		flush := func(done bool) bool {
			out := make([]graphLink, len(batch))
			copy(out, batch)
			select {
			case ch <- graphLinkBatch{Links: out, Done: done}:
				batch = batch[:0]
				return true
			case <-stop:
				return false
			}
		}
		for _, e := range entities {
			for _, l := range g.ScanEntityLinks(e) {
				batch = append(batch, l)
			}
			if len(batch) >= graphLinkBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
	return g
}

// pollImageDecodes is called on the UI goroutine. Workers only send results;
// all tab state is changed here, avoiding races with Gio layout code.
func PollImageDecodes(ui *uiState) {
	for _, t := range ui.OpenTabs {
		if t.ImageDecodeCh == nil {
			continue
		}
		select {
		case result := <-t.ImageDecodeCh:
			t.ImageDecodeCh = nil
			if t.ImageDecodeStop != nil {
				t.ImageDecodeStop()
				t.ImageDecodeStop = nil
			}
			t.ImageLoading = false
			t.ImageBytes = result.Bytes
			t.ImageFormat = strings.ToUpper(result.Format)
			t.ImageData = result.Data
			t.ImageMipmaps = result.Mipmaps
			t.ImageMipLevel = 0
			if result.Err != nil {
				t.ImageError = result.Err.Error()
				if ActiveTab(ui) == t {
					ui.StatusText = fmt.Sprintf("Failed to open %s: %s", filepath.Base(t.Path), t.ImageError)
				}
			} else {
				CacheDecodedImage(ui, t.Path, t.ImageData, t.ImageMipmaps, t.ImageFormat, t.ImageBytes)
				if ActiveTab(ui) == t {
					b := t.ImageData.Bounds()
					ui.StatusText = fmt.Sprintf("Opened %s — %s %dx%d", filepath.Base(t.Path), t.ImageFormat, b.Dx(), b.Dy())
				}
			}
		default:
		}
	}
}

func WavTogglePlayPause(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "wav" || t.WavPlayer == nil {
		return
	}
	if t.WavPaused {
		FdaPauseAllOthers(ui, nil)
		PauseOtherWavPlayers(ui, t)
		if t.WavSrc.Remaining() <= 0 {
			if _, err := t.WavPlayer.Seek(0, io.SeekStart); err != nil {
				ui.StatusText = fmt.Sprintf("WAV replay seek failed: %v", err)
				return
			}
		}
		t.WavPlayer.Play()
		t.WavPaused = false
		t.WavSeekFloat.Value = 0
		ui.StatusText = "Playing " + filepath.Base(t.Path)
	} else {
		t.WavPlayer.PauseAndStopReading()
		t.WavPaused = true
		ui.StatusText = "Paused " + filepath.Base(t.Path)
	}
}

func WavStop(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "wav" || t.WavPlayer == nil {
		return
	}
	t.WavPlayer.PauseAndStopReading()
	_, _ = t.WavPlayer.Seek(0, io.SeekStart)
	t.WavPaused = true
	t.WavSeekFloat.Value = 0
	ui.StatusText = "Stopped " + filepath.Base(t.Path)
}

func CheckWavAutoRepeat(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "wav" || t.WavPlayer == nil || t.WavSrc == nil || t.WavPaused {
		return
	}
	if !t.WavPlayer.IsPlaying() {
		if err := t.WavPlayer.Err(); err != nil {
			t.WavPaused = true
			ui.StatusText = fmt.Sprintf("WAV audio error: %v", err)
		} else if t.WavSrc.Remaining() <= 0 {
			if t.WavRepeat {
				if _, err := t.WavPlayer.Seek(0, io.SeekStart); err == nil {
					t.WavPlayer.Play()
					t.WavPaused = false
					t.WavSeekFloat.Value = 0
					ui.StatusText = "Repeating " + filepath.Base(t.Path)
					return
				}
			}
			t.WavPaused = true
			t.WavSeekFloat.Value = 1
			ui.StatusText = "WAV playback finished"
		} else {
			t.WavPlayer.Play()
		}
	}
}

func LayoutWavViewer(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t == nil {
		return layout.Dimensions{}
	}
	if t.WavError != "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "Failed: "+t.WavError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	if t.WavData == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), "No WAV data")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	WavUpdateSliders(ui, t)
	if t.WavPlayer != nil && !t.WavPaused {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(100 * time.Millisecond)})
	}
	if t.WavConvertClick.Clicked(gtx) {
		ConvertWavToFDA(ui, t)
	}
	pos := WavPosFor(ui, t)
	playLabel := "Play"
	if !t.WavPaused {
		playLabel = "Pause"
	}
	exportLabel := "Convert to FDA"
	if t.WavExporting {
		exportLabel = "Converting…"
	} else if t.WavExportDone != "" {
		exportLabel = "FDA created"
	}
	return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if !t.WavPaused {
							return PrimaryButton(ui, gtx, &t.WavPlayClick, playLabel)
						}
						return SmallButton(ui, gtx, &t.WavPlayClick, playLabel)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return SmallButton(ui, gtx, &t.WavStopClick, "Stop") }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bg := uikit.CSaveBg
						if t.WavRepeat {
							bg = uikit.CMenuOpen
						}
						return ToggleButton(ui, gtx, &t.WavRepeatClick, "Repeat", t.WavRepeat, bg)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return SmallButtonDisabled(ui, gtx, &t.WavConvertClick, exportLabel, t.WavExporting)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := "Vol"
						if t.WavVolumePct == 0 {
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
						slider := material.Slider(ui.Th, &t.WavVolFloat)
						slider.Color = rgb(0x6B6B6B)
						dims := slider.Layout(gtx)
						WavHandleVolumeSlider(ui, t)
						return dims
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("%d%%", t.WavVolumePct))
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
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWavWaveform(ui, gtx, t) }),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), fmtDuration(t.WavTotal))
						l.Color = uikit.CTextDim
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if t.WavExportDone == "" {
					return layout.Dimensions{}
				}
				l := material.Label(ui.Th, unit.Sp(9), fmt.Sprintf("FDA: %s", filepath.Base(t.WavExportDone)))
				l.Color = rgb(0x2E7D32)
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
		)
	})
}

// setLocaleLanguage picks a language, rebuilds the table and remembers the
// choice in the settings file with everything else.
func SetLocaleLanguage(ui *uiState, lang string) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return
	}
	if strings.EqualFold(lang, ui.Locale) {
		return
	}
	ui.Locale = lang
	ApplyLocale(ui)
	MarkStateDirty(ui)
}

// handleLocaleInput runs the language picker. The list is a plain clickable
// list rather than an editor driven one, so it needs no key handling beyond
// Escape.
func HandleLocaleInput(ui *uiState, gtx layout.Context) {
	if ui.LocaleTrigger.Clicked(gtx) {
		// All three hang off the menu bar, so only one of them is open.
		CloseSearch(ui)
		ui.MenuOpen = -1
		ui.LocaleOpen = !ui.LocaleOpen
		EnsureLocaleClicks(ui, len(ui.LocaleList))
	}
	if !ui.LocaleOpen {
		return
	}
	// Escape takes the list away, like it does the menus. The list has no
	// editor of its own, so nothing else would want the key.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		CloseLocale(ui)
		return
	}
	if ui.LocaleDismiss.Clicked(gtx) {
		CloseLocale(ui)
		return
	}
	EnsureLocaleClicks(ui, len(ui.LocaleList))
	for i := range ui.LocaleClicks {
		if i >= len(ui.LocaleList) {
			break
		}
		if ui.LocaleClicks[i].Clicked(gtx) {
			SetLocaleLanguage(ui, ui.LocaleList[i])
			CloseLocale(ui)
			return
		}
	}
}

// layoutLocaleOverlay is the open list, drawn over the window like the search
// results. It is laid out from the whole window rather than from the field, so
// its right edge is measured off the window the same margin the field uses.
func LayoutLocaleOverlay(ui *uiState, gtx layout.Context) layout.Dimensions {
	if !ui.LocaleOpen {
		return layout.Dimensions{}
	}
	EnsureLocaleClicks(ui, len(ui.LocaleList))
	menuBarH := gtx.Dp(unit.Dp(24))
	listW := gtx.Dp(unit.Dp(localeListWidthDp))
	rowH := gtx.Dp(unit.Dp(localeRowHeightDp))
	margin := gtx.Dp(unit.Dp(localeListMarginDp))
	// A long language list would run off the bottom; the panel is capped and
	// scrolls from there.
	maxRows := (gtx.Constraints.Max.Y - menuBarH - 2*gtx.Dp(unit.Dp(6))) / rowH
	if maxRows < 3 {
		maxRows = 3
	}
	rows := len(ui.LocaleList)
	if rows > maxRows {
		rows = maxRows
	}
	return layout.Stack{}.Layout(gtx,
		// The dismiss area starts below the menu bar, so the field itself
		// stays clickable while the list is open and can close it again.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			defer op.Offset(image.Pt(0, menuBarH)).Push(gtx.Ops).Pop()
			return ui.LocaleDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(listW, rows*rowH+2*(gtx.Dp(unit.Dp(localeListPadDp))+gtx.Dp(unit.Dp(localeListBorderDp))))
			xOff := gtx.Constraints.Max.X - listW - margin
			if xOff < 0 {
				xOff = 0
			}
			ui.LocaleDropBox = image.Rectangle{Min: image.Pt(xOff, menuBarH), Max: image.Pt(xOff+listW, menuBarH+sz.Y)}
			defer op.Offset(image.Pt(xOff, menuBarH)).Push(gtx.Ops).Pop()
			// A drop shadow, the way the menus do it, so the list reads as
			// being above the content.
			shadowOff := gtx.Dp(unit.Dp(2))
			st := clip.Rect{Max: image.Pt(listW+shadowOff, sz.Y+shadowOff)}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 28)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			st2 := clip.Rect{Max: sz}.Push(gtx.Ops)
			paint.ColorOp{Color: uikit.CPanelBg}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st2.Pop()
			b := widget.Border{Color: uikit.CBorder, Width: unit.Dp(1)}
			gtx.Constraints.Min = sz
			gtx.Constraints.Max = sz
			return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(localeListPadDp), Bottom: unit.Dp(localeListPadDp)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var ch []layout.FlexChild
					for i := 0; i < rows; i++ {
						i := i
						ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return LocaleRow(ui, gtx, i)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
				})
			})
		}),
	)
}

func writeLocaleEntry(projectDir, lang, id, text string) (string, int, error) {
	return locstrings.WriteLocaleEntry(projectDir, lang, id, text)
}

// saveLocaleEdit writes the dialog's text and reloads the table, so the change
// is on screen before the dialog closes rather than after the next module load.
//
// A create also points the field at the new id. Without that the string would
// be written and never read: the field would still hold $0, which is how the
// game spells "no string", and the unit would stay untranslated in the game
// while the app showed it translated.
func SaveLocaleEdit(ui *uiState) (string, error) {
	text := strings.TrimSpace(ui.LocaleEditEditor.Text())
	if text == "" {
		return "", fmt.Errorf("the text is empty — a localisation with no text shows as a blank in game")
	}
	id := ui.LocaleEditID
	if ui.LocaleEditCreate {
		free := allocateLocaleID()
		if free == "" {
			return "", fmt.Errorf("no free localisation id could be found")
		}
		id = free
	}
	path, line, err := writeLocaleEntry(ui.ProjectDir, LocaleLabel(ui), id, text)
	if err != nil {
		return "", err
	}
	// The table is cached per language, so without this the string that was
	// just written is missing from the copy every view reads and the edit looks
	// like it did nothing.
	invalidateLocaleCache()
	ApplyLocale(ui)

	if ui.LocaleEditCreate && ui.LocaleEditField != nil {
		// The field is what makes the new string reachable: until it holds the
		// id, the game still reads $0 and shows nothing. The parsed unit is
		// rebuilt from it so the panel shows the new text straight away.
		ui.LocaleEditField.WStrVal = "$" + id
		if d := CurrentUnitDetails(ui); d != nil {
			d.RebuildFromFile()
		}
		MarkRgdFieldDirty(ui, ui.LocaleEditFieldKey)
		// The same change, seen from the object window: the field that names
		// the new string is part of a unit nobody has written back yet, and
		// the string is worth nothing to the game until they are.
		MarkUnitFieldDirty(ui, ui.LocaleEditField)
	}
	return fmt.Sprintf("Saved $%s to %s line %d", id, filepath.Base(path), line), nil
}

// markRgdFieldDirty tells an open file view that its data has been changed and
// needs writing back. It is only that: the object window keeps the unit parsed
// in memory, so pointing one of its fields at a new id is a change to the
// parsed copy, and whether it reaches the .rgd on disk is the writer's business
// — the status line says which id was allocated so nothing about it is silent.
func MarkRgdFieldDirty(ui *uiState, key string) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "rgd" {
		return
	}
	t.RgdDirty = true
}

func HandleLocaleEditInput(ui *uiState, gtx layout.Context) {
	if !ui.LocaleEditDialog {
		return
	}
	// Escape has to be taken before the editor's own Update, or the editor uses
	// it to drop a text selection instead of closing the dialog.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		CloseLocaleEdit(ui)
		return
	}
	for {
		ev, ok := ui.LocaleEditEditor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			SaveLocaleEditAndClose(ui)
			return
		}
	}
	if ui.LocaleEditCancel.Clicked(gtx) {
		CloseLocaleEdit(ui)
		return
	}
	if ui.LocaleEditSave.Clicked(gtx) {
		SaveLocaleEditAndClose(ui)
	}
}

func SaveLocaleEditAndClose(ui *uiState) {
	msg, err := SaveLocaleEdit(ui)
	if err != nil {
		ui.LocaleEditError = err.Error()
		return
	}
	CloseLocaleEdit(ui)
	ui.StatusText = msg
}

// layoutLocaleEditDialog draws the editor over the window, the way the other
// dialogs do: a dimmed field behind, a card in the middle.
func LayoutLocaleEditDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			st := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 110)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(460))
				cardH := gtx.Dp(unit.Dp(240))
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = cardW, cardW
				gtx.Constraints.Min.Y, gtx.Constraints.Max.Y = cardH, cardH
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(18), Right: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return LocaleEditCaption(ui, gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return widgetBorderTextEditor(gtx, ui.Th, &ui.LocaleEditEditor, false, "")
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return LocaleEditNote(ui, gtx)
									}),
									layout.Flexed(1, flexGap()),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return LocaleEditButtons(ui, gtx)
									}),
								)
							})
						}),
					)
				})
			})
		}),
	)
}

func LocaleEditButtons(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.LocaleEditError == "" {
				return layout.Dimensions{}
			}
			l := material.Label(ui.Th, unit.Sp(10), ui.LocaleEditError)
			l.Color = rgb(0xE8A33D)
			l.MaxLines = 2
			return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, l.Layout)
		}),
		layout.Flexed(1, flexGap()),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &ui.LocaleEditCancel, "Cancel")
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return SmallButton(ui, gtx, &ui.LocaleEditSave, "Save")
		}),
	)
}

// localePanelRow is the same row in the right hand panel's two column shape.
func LocalePanelRow(ui *uiState, gtx layout.Context, label, id string, field *rgd.RgdEntry) layout.Dimensions {
	key := localeRowKey(field, label)
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return LocaleRowBody(ui, gtx, label, id, 24, unit.Sp(11), 40)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			LocaleRowAffordances(ui, gtx, id, key)
			return layout.Dimensions{}
		}),
	)
}

func localeEditPath(projectDir, lang string) (string, error) {
	return locstrings.LocaleEditPath(projectDir, lang)
}

// createNewObject writes a new unit or building of the given race from the
// template and returns the virtual path it wrote.
//
// It refuses rather than guesses: a name the file system would not take, and a
// name that already names a file of the mod or of the game, are both errors the
// dialog shows, because an object that overwrote somebody's would be worse than
// one that was not created.
func CreateNewObject(ui *uiState, kind newObjKind, race, name string) (string, error) {
	if ui.ModTree == nil {
		return "", fmt.Errorf("no module is loaded")
	}
	if err := newObjTemplateNameError(kind, name); err != nil {
		return "", err
	}
	if race == "" {
		return "", fmt.Errorf("no race is selected, so there is no folder to write the %s into", kind.NewObjNoun())
	}
	virtual := kind.NewObjPath(race, name)
	if ui.ModTree.FindFile(virtual) != nil {
		return "", fmt.Errorf("%s already exists", name)
	}
	var file *rgd.RgdFile
	if kind == newObjBuilding {
		file = newStructureRgd(structureTemplateName{Race: race, Name: name})
	} else {
		file = newUnitRgd(unitTemplateName{Race: race, Name: name})
	}
	data, err := file.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("the %s could not be written: %v", kind.NewObjNoun(), err)
	}
	if _, err := ui.ModTree.WriteOverlayFile(virtual, data); err != nil {
		return "", fmt.Errorf("the %s could not be saved: %v", kind.NewObjNoun(), err)
	}
	// The catalogue is a snapshot of the tree, so it is thrown away and read
	// again: that is what puts the new object in the list and in the pickers.
	ui.UnitCatalog = nil
	ui.UnitDetails = nil
	ui.UnitDetailsPath = ""
	SelectNewObject(ui, race, virtual)
	ui.StatusText = fmt.Sprintf("Created %s", name)
	MarkStateDirty(ui)
	return virtual, nil
}

// selectNewObject points the object window at the file that was just written,
// so the list opens on it rather than where it was before.
func SelectNewObject(ui *uiState, race, virtual string) {
	cat := UnitCatalogFor(ui)
	if cat == nil {
		return
	}
	for ri, r := range cat.Races {
		if !strings.EqualFold(r.Name, race) {
			continue
		}
		ui.UnitRaceSel = ri
		ui.UnitSel = 0
		// unitsOf is the list's own order: the race's troops, then its
		// structures. A new building is picked out of the second half, and a
		// new unit out of the first, without either case being written twice.
		for i, e := range r.UnitsOf() {
			if strings.EqualFold(e.Path, virtual) {
				ui.UnitSel = i
				return
			}
		}
		return
	}
}

// openNewObjDialog puts the dialog up to create a unit or a building of the
// given kind, with the name field ready and empty. The race is the one the
// object window was showing: the object belongs where the user was looking, and
// making them choose again would be a second question with the same answer.
func OpenNewObjDialog(ui *uiState, kind newObjKind) {
	race := CurrentRace(ui)
	if race == nil {
		ui.StatusText = "Choose a race before creating a " + kind.NewObjNoun()
		return
	}
	CloseUnitPickers(ui)
	ui.NewObjDialog = true
	ui.NewObjKind = kind
	ui.NewObjStatus = ""
	ui.NewObjNameEditor.SetText("")
}

// newObjFolderLabel is where a new object of the current race and kind will be
// written.
func NewObjFolderLabel(ui *uiState) string {
	race := CurrentRace(ui)
	if race == nil {
		return ""
	}
	return ui.NewObjKind.NewObjPath(race.Name, "name")
}

// layoutNewObjDialog draws the dialog as an overlay over the whole window: a
// catcher that takes the click meant for the window behind it, the card, and
// nothing else.
func LayoutNewObjDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	cardW := gtx.Dp(unit.Dp(420))
	cardH := gtx.Dp(unit.Dp(228))
	// Recorded for tests, the same as the picker list: without it a test has
	// to guess where the card landed and probes the window around it.
	ui.NewObjCardBox = image.Rect(gtx.Constraints.Max.X/2-cardW/2, gtx.Constraints.Max.Y/2-cardH/2, gtx.Constraints.Max.X/2+cardW/2, gtx.Constraints.Max.Y/2+cardH/2)
	// A Stack puts a child smaller than itself at the given corner, and its
	// zero value is the top left, so the card is centred by naming the
	// alignment rather than by wrapping it: layout.Center centres within the
	// constraints' minimum, which a stack's children do not carry.
	return layout.Stack{Alignment: layout.Center}.Layout(gtx,
		// The catcher first, so the card is painted over it and takes the
		// clicks meant for itself. It has to be a Stacked child: a Stack lays
		// its Expanded children out last, which would paint the catcher over
		// the card and swallow every click meant for the card.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return ui.NewObjDismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Pt(cardW, cardH)
			gtx.Constraints.Max = image.Pt(cardW, cardH)
			return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Stack{}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(4))
						return layout.Dimensions{Size: gtx.Constraints.Max}
					}),
					layout.Stacked(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(16), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(14), "New "+ui.NewObjKind.NewObjNoun())
									l.Font.Weight = font.Medium
									l.Color = uikit.CText
									return l.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(11), "Written to "+NewObjFolderLabel(ui))
									l.Color = uikit.CTextDim
									return l.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									h := gtx.Dp(unit.Dp(28))
									gtx.Constraints.Min.Y = h
									gtx.Constraints.Max.Y = h
									return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											ed := material.Editor(ui.Th, &ui.NewObjNameEditor, "")
											ed.TextSize = unit.Sp(11)
											ed.HintColor = uikit.CTextDim
											ed.Hint = NewObjHint(ui)
											return ed.Layout(gtx)
										})
									})
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if ui.NewObjStatus == "" {
										return layout.Dimensions{}
									}
									l := material.Label(ui.Th, unit.Sp(10), ui.NewObjStatus)
									l.Color = uikit.CTreeThis
									return l.Layout(gtx)
								}),
								// The buttons take the space left over and sit along its bottom
								// edge. They cannot be a rigid row below a flexible spacer: a
								// Flex lays out every rigid child before the flexible ones, so
								// such a row would sit right under the status line and leave
								// the card's foot empty.
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
										layout.Flexed(1, flexGap()),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return DialogButton(ui, gtx, &ui.NewObjCancel, "Cancel", false)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return DialogButton(ui, gtx, &ui.NewObjOk, "Create", true)
										}),
									)
								}),
							)
						})
					}),
				)
			})
		}),
	)
}

// handleNewObjDialogInput owns the dialog while it is up. It is read before
// everything else, so a click on the window behind it reaches nothing.
func HandleNewObjDialogInput(ui *uiState, gtx layout.Context) {
	submitted := false
	for {
		ev, ok := ui.NewObjNameEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	// A click on the dimmed window behind the card is a way out, the same as
	// Escape: both say "not now" and change nothing.
	if ui.NewObjDismissClick.Clicked(gtx) && !ui.NewObjOk.Clicked(gtx) && !ui.NewObjCancel.Clicked(gtx) {
		CloseNewObjDialog(ui)
		return
	}
	if ui.NewObjCancel.Clicked(gtx) {
		CloseNewObjDialog(ui)
		return
	}
	if !ui.NewObjOk.Clicked(gtx) && !submitted {
		// Escape leaves the dialog, which a dialog the keyboard can reach has
		// to allow.
		for {
			ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			if e, ok := ev.(key.Event); ok && e.State == key.Press {
				CloseNewObjDialog(ui)
				return
			}
		}
		return
	}
	race := CurrentRace(ui)
	if race == nil {
		ui.NewObjStatus = "No race is selected, so there is nowhere to write the " + ui.NewObjKind.NewObjNoun()
		return
	}
	virtual, err := CreateNewObject(ui, ui.NewObjKind, race.Name, ui.NewObjNameEditor.Text())
	if err != nil {
		// A refused name is said in the dialog and the dialog stays up, so the
		// name can be corrected instead of retyped.
		ui.NewObjStatus = err.Error()
		return
	}
	CloseNewObjDialog(ui)
	// The file is written; opening it is what makes the next step — the model,
	// the icon, the name id — a click away rather than a search through the mod.
	OpenRgd(ui, virtual)
}

func RgdEnsureValEditor(ui *uiState, t *fileTab, key, init string) *widget.Editor {
	if t.RgdValEditors == nil {
		t.RgdValEditors = make(map[string]*widget.Editor)
	}
	if ed, ok := t.RgdValEditors[key]; ok {
		return ed
	}
	ed := &widget.Editor{SingleLine: true, Submit: true}
	ed.SetText(init)
	t.RgdValEditors[key] = ed
	return ed
}

func RgdSyncStringSuggestions(ui *uiState, t *fileTab, e *rgd.RgdEntry, key string, gtx layout.Context) {
	if t == nil || e == nil || (e.Type != rgd.RgdTypeString && e.Type != rgd.RgdTypeWString) {
		return
	}
	suggestions := rgdVisibleSuggestions(t, e)
	clicks := RgdEnsureSuggestionClicks(ui, t, key, len(suggestions))
	for i := range clicks {
		if clicks[i].Clicked(gtx) {
			ed := RgdEnsureValEditor(ui, t, key, e.DisplayValue())
			ed.SetText(suggestions[i])
			RgdApplyEditorText(ui, t, e, key, suggestions[i])
			return
		}
	}
}

func RgdSyncEditor(ui *uiState, t *fileTab, e *rgd.RgdEntry, key string, gtx layout.Context) {
	if t == nil || e == nil || e.Type == rgd.RgdTypeBool || !rgdIsEditableType(e.Type) {
		return
	}
	ed := RgdEnsureValEditor(ui, t, key, e.DisplayValue())
	for {
		if _, ok := ed.Update(gtx); !ok {
			break
		}
	}
	RgdApplyEditorText(ui, t, e, key, ed.Text())
}

// rgdSyncAndApply processes editor Update events and applies live text changes to the RGD model.
// Must be called from handleInput with a valid gtx.
func RgdSyncAndApply(ui *uiState, gtx layout.Context) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "rgd" || t.RgdFile == nil {
		return
	}
	eSel := RgdSelectedEntryFor(ui, t)
	// ── Reference editor (only for tables) ──────────────────────────────────
	if eSel != nil && eSel.IsTable() {
		if t.RgdRefBound != t.RgdSelected {
			cur := eSel.RefString()
			t.RgdRefEditor.SetText(cur)
			t.RgdRefBound = t.RgdSelected
			t.RgdRefEditor.SingleLine = true
			t.RgdRefEditor.Submit = true
		}
		for {
			ev, ok := t.RgdRefEditor.Update(gtx)
			if !ok {
				break
			}
			if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
				// apply on Enter as well (live apply below also covers it)
			}
			_ = ev
		}
		newText := t.RgdRefEditor.Text()
		cur := eSel.RefString()
		if newText != cur {
			if strings.TrimSpace(newText) == "" {
				eSel.Ref = nil
			} else {
				if eSel.Ref == nil {
					eSel.Ref = &rgd.RgdEntry{Hash: rgd.RefHash, Name: "$REF", Type: rgd.RgdTypeString, StrVal: newText}
				} else {
					if eSel.Ref.Type == rgd.RgdTypeWString {
						eSel.Ref.WStrVal = newText
					} else {
						eSel.Ref.Type = rgd.RgdTypeString
						eSel.Ref.StrVal = newText
						eSel.Ref.WStrVal = ""
					}
				}
				eSel.Ref.Hash = rgd.RefHash
				if eSel.Ref.Name == "" {
					eSel.Ref.Name = "$REF"
				}
			}
			t.RgdDirty = true
		}
	}
	// ── Scalar value editors and Bool toggles ───────────────────────────────
	if eSel != nil && eSel.Type == rgd.RgdTypeBool {
		if RgdEnsureBoolClick(ui, t, t.RgdSelected).Clicked(gtx) {
			eSel.BoolVal = !eSel.BoolVal
			t.RgdDirty = true
		}
	} else if eSel != nil && (eSel.Type == rgd.RgdTypeString || eSel.Type == rgd.RgdTypeWString) {
		RgdSyncStringSuggestions(ui, t, eSel, t.RgdSelected, gtx)
		RgdSyncEditor(ui, t, eSel, t.RgdSelected, gtx)
	} else if eSel != nil && rgdIsEditableType(eSel.Type) {
		RgdSyncEditor(ui, t, eSel, t.RgdSelected, gtx)
	}
	if eSel != nil && eSel.IsTable() {
		for _, ch := range eSel.Children {
			key := t.RgdSelected + "/" + ch.Name
			if ch.Type == rgd.RgdTypeBool {
				if RgdEnsureBoolClick(ui, t, key).Clicked(gtx) {
					ch.BoolVal = !ch.BoolVal
					t.RgdDirty = true
				}
				continue
			}
			if ch.Type == rgd.RgdTypeString || ch.Type == rgd.RgdTypeWString {
				RgdSyncStringSuggestions(ui, t, ch, key, gtx)
				RgdSyncEditor(ui, t, ch, key, gtx)
				continue
			}
			if ch.IsTable() {
				refKey := key + "/$ref"
				refEditor := RgdEnsureValEditor(ui, t, refKey, ch.RefString())
				for {
					if _, ok := refEditor.Update(gtx); !ok {
						break
					}
				}
				if rgdApplyReferenceText(ch, refEditor.Text()) {
					t.RgdDirty = true
				}
				continue
			}
			RgdSyncEditor(ui, t, ch, key, gtx)
		}
	}
}

func TableRowBool(ui *uiState, gtx layout.Context, label string, t *fileTab, e *rgd.RgdEntry, key string, alt bool) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := uikit.CPanelBg
	if alt {
		bg = uikit.CSaveBg
	}
	click := RgdEnsureBoolClick(ui, t, key)
	value := "False"
	if e.BoolVal {
		value = "True"
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			st := clip.Rect{Max: sz}.Push(gtx.Ops)
			paint.ColorOp{Color: bg}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(11), label)
						l.Color = uikit.CText
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, uikit.CBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ToggleButton(ui, gtx, click, value, e.BoolVal, uikit.CSelectBg)
					})
				}),
			)
		}),
	)
}

// rgdTooltipForValue explains where a "$number" value came from. The two
// string types mean different things by it: a DT_WString is a localisation id,
// while a DT_String is a hash alias into the RGD dictionary.
func rgdTooltipForValue(e *rgd.RgdEntry) string {
	if e == nil || !rgd.IsNumericAlias(e.DisplayValue()) {
		return ""
	}
	alias := e.DisplayValue()
	if e.Type == rgd.RgdTypeWString {
		if entry, ok := localeLookup(rgdLocIDOrEmpty(alias)); ok {
			return fmt.Sprintf("Source: %s localisation string table\nID: %s\nFile: %s line %d",
				localeDefault, alias, filepath.Base(entry.File), entry.Line)
		}
		return fmt.Sprintf("Source: localisation string table\nID: %s", alias)
	}
	if rgd.AliasDisplayName(alias, rgd.InitDict()) != "" {
		return fmt.Sprintf("Source: RGD dictionary\nAlias: %s", alias)
	}
	return fmt.Sprintf("Source: field name fallback\nAlias: %s", alias)
}

func TableRowStringEditor(ui *uiState, gtx layout.Context, label string, t *fileTab, e *rgd.RgdEntry, key string, alt bool) layout.Dimensions {
	// A DT_String numeric alias is a dictionary reference, not a readable value,
	// so it shows the resolved name while the original alias stays in the model.
	// A DT_WString is a localisation id, which is shown as it is.
	if display := e.DisplayFieldValue(); display != e.DisplayValue() && e.Type == rgd.RgdTypeString {
		return TableRow2WithTooltip(ui, gtx, label, display, alt, rgdTooltipForValue(e))
	}
	suggestions := rgdVisibleSuggestions(t, e)
	clicks := RgdEnsureSuggestionClicks(ui, t, key, len(suggestions))
	children := make([]layout.FlexChild, 0, len(suggestions)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		ed := RgdEnsureValEditor(ui, t, key, e.DisplayValue())
		return TableRowEditor(ui, gtx, label, ed, alt, t.RgdValErrors[key])
	}))
	for i, suggestion := range suggestions {
		i, suggestion := i, suggestion
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := rgdSuggestionLabel(suggestion)
			if len(text) > 58 {
				text = text[:55] + "…"
			}
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return SmallButton(ui, gtx, &clicks[i], text)
			})
		}))
	}
	// A DT_WString carries a localisation id, so the row that says what the id
	// means in the chosen language goes under the value. A DT_String is a
	// dictionary alias and has nothing to resolve against.
	if e.Type == rgd.RgdTypeWString {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return TableRowLocaleText(ui, gtx, t, e, key, alt)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func RebuildRgdMidNodes(ui *uiState) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "rgd" {
		return
	}
	RebuildRgdMidNodesFor(ui, t)
}

func RgdFlattenChildren(ui *uiState, children []*rgd.RgdEntry, basePath string, depth int, out *[]rgdVisible) {
	t := ActiveTab(ui)
	if t == nil {
		return
	}
	RgdFlattenChildrenFor(ui, t, children, basePath, depth, out)
}

func RgdFindEntry(ui *uiState, path string) *rgd.RgdEntry {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "rgd" {
		return nil
	}
	return RgdFindEntryFor(ui, t, path)
}

func RgdSelectedEntry(ui *uiState) *rgd.RgdEntry {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "rgd" {
		return nil
	}
	return RgdSelectedEntryFor(ui, t)
}

func PropertiesTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	e := RgdSelectedEntryFor(ui, t)
	var name, typeLabel string
	if e == nil {
		name = "—"
		typeLabel = "—"
	} else {
		if t.RgdSelected == "GameData" || t.RgdSelected == "" {
			name = "GameData"
		} else if idx := strings.LastIndex(t.RgdSelected, "/"); idx >= 0 {
			name = t.RgdSelected[idx+1:]
		} else {
			name = e.Name
		}
		typeLabel = e.TypeLabel()
	}
	isTable := e != nil && e.IsTable()
	editableScalar := e != nil && rgdIsEditableType(e.Type)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, "Name", name, false) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableRow2(ui, gtx, "Data Type", typeLabel, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if isTable {
				return TableRowEditor(ui, gtx, "Reference", &t.RgdRefEditor, false, "")
			}
			return TableRow2(ui, gtx, "Reference", e.RefString(), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if e != nil && e.Type == rgd.RgdTypeBool {
				return TableRowBool(ui, gtx, "Value", t, e, t.RgdSelected, true)
			}
			if e != nil && (e.Type == rgd.RgdTypeString || e.Type == rgd.RgdTypeWString) {
				return TableRowStringEditor(ui, gtx, "Value", t, e, t.RgdSelected, true)
			}
			if editableScalar {
				ed := RgdEnsureValEditor(ui, t, t.RgdSelected, e.DisplayValue())
				return TableRowEditor(ui, gtx, "Value", ed, true, t.RgdValErrors[t.RgdSelected])
			}
			if e != nil && !isTable {
				return TableRow2(ui, gtx, "Value", e.DisplayFieldValue(), true)
			}
			return layout.Dimensions{}
		}),
	)
}

func ChildrenTable(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.RgdFile == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), "Select a .rgd file, then a node in the middle tree")
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		})
	}
	e := RgdSelectedEntryFor(ui, t)
	if e == nil {
		e = t.RgdFile.Root
	}
	if !e.IsTable() {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			val := e.DisplayFieldValue()
			if val == "" {
				val = "—"
			}
			msg := fmt.Sprintf("%s (%s) = %s — no children", e.Name, e.TypeLabel(), val)
			l := material.Label(ui.Th, unit.Sp(11), msg)
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	children := e.Children
	if len(children) == 0 {
		if ref := e.RefString(); ref != "" {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(10), fmt.Sprintf("Empty table — Reference: %s", ref))
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			})
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(10), "No children")
			l.Color = uikit.CTextDim
			return l.Layout(gtx)
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return TableHead(ui, gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return LayoutListWithScrollbar(ui, gtx, &t.RgdChildScroll, &t.RgdChildList, 28, len(children), func(gtx layout.Context, index int) layout.Dimensions {
				ch := children[index]
				alt := index%2 == 1
				key := t.RgdSelected + "/" + ch.Name
				if ch.IsTable() {
					refEditor := RgdEnsureValEditor(ui, t, key+"/$ref", ch.RefString())
					return TableRowEditor(ui, gtx, ch.Name, refEditor, alt, "")
				}
				if ch.Type == rgd.RgdTypeBool {
					return TableRowBool(ui, gtx, ch.Name, t, ch, key, alt)
				}
				if ch.Type == rgd.RgdTypeString || ch.Type == rgd.RgdTypeWString {
					return TableRowStringEditor(ui, gtx, ch.Name, t, ch, key, alt)
				}
				if rgdIsEditableType(ch.Type) {
					ed := RgdEnsureValEditor(ui, t, key, ch.DisplayValue())
					return TableRowEditor(ui, gtx, ch.Name, ed, alt, t.RgdValErrors[key])
				}
				val := ch.DisplayFieldValue()
				if len(val) > 64 {
					val = val[:61] + "…"
				}
				return TableRow2WithTooltip(ui, gtx, ch.Name, val, alt, rgdTooltipForValue(ch))
			})
		}),
	)
}

func TextActive(ui *uiState) bool {
	t := ActiveTab(ui)
	return t != nil && t.Kind == "text" && t.TextError == ""
}

func TextSyncAndApply(ui *uiState, gtx layout.Context) {
	t := ActiveTab(ui)
	if t == nil || t.Kind != "text" || t.TextError != "" {
		return
	}
	for {
		if _, ok := t.TextEditor.Update(gtx); !ok {
			break
		}
	}
	t.TextDirty = t.TextEditor.Text() != t.TextOriginal
}
