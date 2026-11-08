package app

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/unitdef"
	"dow-mod-studio/internal/wav"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/ebitengine/oto/v3"
)

// The Voices section is the pair the game keeps apart: the blueprint names the
// folder the unit is voiced in (ui_ext/speech_directory) and the speech lives in
// the mod's sound tree, one folder of takes per action. The section shows both
// halves — what the file declares, and which replies the mod actually carries —
// plays a take through the same decoder the WAV and FX editors use, and converts
// the selected take into the other format beside it, which is how a reply gets
// edited outside the studio and comes back as an .fda the game can play.

// CurrentUnitVoices resolves the selected unit's speech folder against the mod.
// It is a lookup in the tree's folder index rather than a disk read, so the
// section can resolve it while laying out instead of caching a stale copy: a
// reply dropped into the mod shows up on the next frame.
func CurrentUnitVoices(ui *uiState) unitdef.UnitVoiceSet {
	d := CurrentUnitDetails(ui)
	if d == nil {
		return unitdef.UnitVoiceSet{}
	}
	return unitdef.UnitVoices(d.Tree, d.File)
}

// SelectedUnitVoiceCategory is the category the right column shows: the row the
// middle column has selected, or the first one when the selection is out of
// range — which is what happens when the unit changes and the row count with
// it.
func SelectedUnitVoiceCategory(ui *uiState, set unitdef.UnitVoiceSet) (unitdef.UnitVoiceCategory, bool) {
	if len(set.Categories) == 0 {
		return unitdef.UnitVoiceCategory{}, false
	}
	return set.Categories[clampInt(ui.UnitSelRow, 0, len(set.Categories)-1)], true
}

// SelectedUnitVoiceFile is the take the convert buttons act on, clamped the same
// way: a category with fewer files than the last selection had must still leave
// a row highlighted rather than none.
func SelectedUnitVoiceFile(ui *uiState, cat unitdef.UnitVoiceCategory) (unitdef.UnitVoiceFile, bool) {
	if len(cat.Files) == 0 {
		return unitdef.UnitVoiceFile{}, false
	}
	return cat.Files[clampInt(ui.UnitVoiceFileSel, 0, len(cat.Files)-1)], true
}

// unitVoiceNote is one dim paragraph: a section's reason for being empty, said
// where its list would be.
func unitVoiceNote(ui *uiState, gtx layout.Context, msg string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.Th, unit.Sp(10), msg)
		l.Color = uikit.CTextDim
		return l.Layout(gtx)
	})
}

// unitVoiceSpeechRow is the value the blueprint declares, above the categories:
// the folder is the one thing the two halves of the section are joined by, so
// it is named first whether it resolved or not.
func unitVoiceSpeechRow(ui *uiState, gtx layout.Context, set unitdef.UnitVoiceSet) layout.Dimensions {
	value := set.Speech
	if value == "" {
		value = "(not declared)"
	}
	return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.Th, unit.Sp(8), "speech_directory")
				l.Color = uikit.CTextDim
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := value
				if len(text) > 48 {
					text = text[:45] + "…"
				}
				l := material.Label(ui.Th, unit.Sp(10), text)
				l.Color = uikit.CText
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
		)
	})
}

// LayoutUnitVoiceCategories is the middle column: the actions the unit has
// replies for, each with its take count. The names are the action names the
// .whe events and the order UI use, so the list reads as the moments the unit
// is voiced at.
func LayoutUnitVoiceCategories(ui *uiState, gtx layout.Context) layout.Dimensions {
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions { return UnitEmptyState(ui, gtx) })
	}
	set := CurrentUnitVoices(ui)
	return UnitSectionFrame(ui, gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return unitVoiceSpeechRow(ui, gtx, set) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				switch {
				case set.Suffix == "" && set.Speech == "":
					return unitVoiceNote(ui, gtx, "This unit declares no speech_directory, so it has no voice of its own. The game falls back to a shared acknowledgement.")
				case set.Suffix == "":
					return unitVoiceNote(ui, gtx, "speech_directory points at no speech tree: \""+set.Speech+"\" is not a Speech/Races/<race>/<unit> path, so there is nothing to resolve.")
				case len(set.Categories) == 0:
					return unitVoiceNote(ui, gtx, "No reply files under "+strings.Join(set.Searched, " or ")+".")
				}
				EnsureUnitRowClicks(ui, len(set.Categories))
				ui.UnitVoiceCatList.Axis = layout.Vertical
				return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.UnitVoiceCatList.Layout(gtx, len(set.Categories), func(gtx layout.Context, i int) layout.Dimensions {
						c := set.Categories[i]
						takes := fmt.Sprintf("%d", len(c.Files))
						if len(c.Files) == 1 {
							takes = "1 file"
						}
						return LayoutUnitSelectableRow(ui, gtx, i, len(set.Categories), takes, c.Name, "", false)
					})
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return uikit.SmallButton(ui, gtx, &ui.UnitVoiceNewActionClick, "New action…")
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitVoiceDelActionClick, "Delete", len(set.Categories) == 0)
						}),
					)
				})
			}),
		)
	})
}

// LayoutUnitVoicePanel is the right column: the files of the selected action,
// each with a Play button, the folder they were read from, and the conversion
// the selected file allows. A folder normally holds .fda takes; a .wav beside
// them is a take someone exported to edit, so both formats are listed and the
// convert buttons turn whichever is selected into the other, under the same
// name in the same folder.
func LayoutUnitVoicePanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	sec := CurrentUnitSection(ui)
	d := CurrentUnitDetails(ui)
	if d == nil || d.File == nil {
		return UnitColumn(ui, gtx, sec.RightTitle, "Select a unit to see its voice.")
	}
	set := CurrentUnitVoices(ui)
	dims := panelFrame(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutUnitColumnHeader(ui, gtx, sec.RightTitle)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				cat, ok := SelectedUnitVoiceCategory(ui, set)
				if !ok {
					return unitVoiceNote(ui, gtx, "No replies to show. The middle column says what the unit declares and where the mod was searched.")
				}
				if len(ui.UnitVoicePlayClicks) != len(cat.Files) {
					ui.UnitVoicePlayClicks = make([]widget.Clickable, len(cat.Files))
					ui.UnitVoiceFileClicks = make([]widget.Clickable, len(cat.Files))
				}
				ui.UnitVoiceFileList.Axis = layout.Vertical
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(9), unitVoiceClip(cat.Folder, 54))
									l.Color = uikit.CTextDim
									l.MaxLines = 1
									return l.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return uikit.SmallButton(ui, gtx, &ui.UnitVoiceStopClick, "Stop")
								}),
							)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return ui.UnitVoiceFileList.Layout(gtx, len(cat.Files), func(gtx layout.Context, i int) layout.Dimensions {
								sel := i == SelectedUnitVoiceFileIndex(ui, len(cat.Files))
								return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return unitVoiceFileRow(ui, gtx, i, cat.Files[i], sel)
								})
							})
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									text := "Select a file to convert it."
									sel, ok := SelectedUnitVoiceFile(ui, cat)
									if ok {
										text = unitVoiceClip(sel.Name, 34) + "  ·  " + strings.TrimPrefix(sel.Ext, ".")
									}
									l := material.Label(ui.Th, unit.Sp(9), text)
									l.Color = uikit.CTextDim
									l.MaxLines = 1
									return l.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									sel, ok := SelectedUnitVoiceFile(ui, cat)
									isFDA := ok && sel.Ext == ".fda"
									isWAV := ok && sel.Ext == ".wav"
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitVoiceConvWavClick, "→ WAV", !isFDA)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitVoiceConvFdaClick, "→ FDA", !isWAV)
										}),
									)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									_, has := SelectedUnitVoiceFile(ui, cat)
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return uikit.SmallButton(ui, gtx, &ui.UnitVoiceAddClick, "Add reply…")
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return uikit.SmallButtonDisabled(ui, gtx, &ui.UnitVoiceDeleteClick, "Delete", !has)
										}),
									)
								}),
							)
						})
					}),
				)
			}),
		)
	})
	if ui.UnitVoiceNewActionDialog {
		LayoutUnitVoiceNewActionDialog(ui, gtx)
	}
	return dims
}

// SelectedUnitVoiceFileIndex is the row the list draws as selected, clamped the
// same way the file selection is.
func SelectedUnitVoiceFileIndex(ui *uiState, count int) int {
	if count == 0 {
		return -1
	}
	return clampInt(ui.UnitVoiceFileSel, 0, count-1)
}

// unitVoiceFileRow is one take: the name, the format it is in, and Play. The row
// itself is what selects the file the convert buttons act on, so playing and
// converting are one click apart rather than two selections.
func unitVoiceFileRow(ui *uiState, gtx layout.Context, i int, f unitdef.UnitVoiceFile, sel bool) layout.Dimensions {
	bg := uikit.CPanelBg
	switch {
	case sel:
		bg = uikit.CSelectBg
	case ui.UnitVoiceFileClicks[i].Hovered():
		bg = uikit.CMenuHover
	}
	h := gtx.Dp(unit.Dp(unitRowHeightDp))
	sz := image.Pt(gtx.Constraints.Max.X, h)
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	// The name is the part that selects the file, and Play is a button of its
	// own beside it: a clickable row wrapped around the button would take the
	// press first and the button would never see it.
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if bg != uikit.CPanelBg {
				rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
			}
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			// The row's height is the click target for the name; the Play button
			// is a compact control and must not inherit it.
			gtx.Constraints.Min.Y = 0
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.UnitVoiceFileClicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(10), unitVoiceClip(f.Name, 26))
									l.Color = uikit.CText
									l.MaxLines = 1
									return l.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Label(ui.Th, unit.Sp(8), strings.TrimPrefix(f.Ext, "."))
									if f.Ext == ".wav" {
										l.Color = uikit.Rgb(0xC9A87C)
									} else {
										l.Color = uikit.CTextDim
									}
									return l.Layout(gtx)
								}),
							)
						})
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return uikit.SmallButton(ui, gtx, &ui.UnitVoicePlayClicks[i], "Play")
					}),
				)
			})
		}),
	)
}

// unitVoiceClip keeps a name or folder path inside its column: the paths under
// Data run to fifty characters and the right column is a third of the dialog.
func unitVoiceClip(text string, max int) string {
	if len(text) <= max || max < 4 {
		return text
	}
	return text[:max-3] + "…"
}

// HandleUnitVoiceInput reads the section's presses: the category rows, the take
// rows and their Play buttons, the convert buttons, and Stop. It runs whether or
// not the section is on show, so leaving it silences a take rather than leaving
// it playing behind another section.
func HandleUnitVoiceInput(ui *uiState, gtx layout.Context) {
	if ui.UnitSection != unitSectionNamed(unitSectionVoices) {
		StopUnitVoicePreview()
		// The dialog belongs to the section, so leaving the section puts it
		// away rather than leaving it up over another page.
		ui.UnitVoiceNewActionDialog = false
		return
	}
	if ui.UnitVoiceNewActionDialog {
		HandleUnitVoiceNewActionInput(ui, gtx)
		return
	}
	if ui.UnitVoiceStopClick.Clicked(gtx) {
		StopUnitVoicePreview()
		ui.StatusText = "Voice preview stopped"
		return
	}
	set := CurrentUnitVoices(ui)
	// The two action buttons are read before the early return below: an action
	// is what a first reply is added to, so the section has to offer one when
	// the unit has no categories at all.
	if ui.UnitVoiceNewActionClick.Clicked(gtx) {
		OpenUnitVoiceNewActionDialog(ui)
		return
	}
	if ui.UnitVoiceDelActionClick.Clicked(gtx) {
		DeleteUnitVoiceAction(ui)
		return
	}
	if len(set.Categories) == 0 {
		return
	}
	EnsureUnitRowClicks(ui, len(set.Categories))
	for i := range set.Categories {
		if i < len(ui.UnitRowClicks) && ui.UnitRowClicks[i].Clicked(gtx) {
			ui.UnitSelRow = i
			return
		}
	}
	cat, ok := SelectedUnitVoiceCategory(ui, set)
	if !ok {
		return
	}
	for i := range ui.UnitVoicePlayClicks {
		if i >= len(cat.Files) {
			break
		}
		if ui.UnitVoicePlayClicks[i].Clicked(gtx) {
			ui.UnitVoiceFileSel = i
			PlayUnitVoiceFile(ui, cat.Files[i].Path)
			return
		}
	}
	for i := range ui.UnitVoiceFileClicks {
		if i >= len(cat.Files) {
			break
		}
		if ui.UnitVoiceFileClicks[i].Clicked(gtx) {
			ui.UnitVoiceFileSel = i
			return
		}
	}
	sel, ok := SelectedUnitVoiceFile(ui, cat)
	if !ok {
		return
	}
	// The buttons are drawn disabled for the direction that would rewrite a file
	// with itself, and the same rule is repeated here rather than trusted to the
	// drawing: a click that arrives anyway must not decode a file for nothing.
	if ui.UnitVoiceConvWavClick.Clicked(gtx) {
		if sel.Ext == ".fda" {
			ConvertUnitVoiceToWAV(ui, sel)
		}
		return
	}
	if ui.UnitVoiceConvFdaClick.Clicked(gtx) {
		if sel.Ext == ".wav" {
			ConvertUnitVoiceToFDA(ui, sel)
		}
		return
	}
	if ui.UnitVoiceAddClick.Clicked(gtx) {
		OpenUnitVoiceAddPicker(ui, cat)
		return
	}
	if ui.UnitVoiceDeleteClick.Clicked(gtx) {
		DeleteUnitVoiceFile(ui, sel)
		return
	}
}

// unitVoicePlayer holds the one take allowed to sound, with the source it reads
// so both can be released when another take replaces it.
type unitVoicePlayer struct {
	mu     sync.Mutex
	player *oto.Player
	src    *audioplayer.ChunkSource
}

var unitVoicePreview unitVoicePlayer

// StopUnitVoicePreview silences the take that is playing, if any. It is cheap
// when nothing is playing, which is why the input path may call it every frame.
func StopUnitVoicePreview() {
	unitVoicePreview.mu.Lock()
	defer unitVoicePreview.mu.Unlock()
	unitVoicePreview.stopLocked()
}

func (v *unitVoicePlayer) stopLocked() {
	if v.player != nil {
		v.player.Pause()
	}
	if v.src != nil {
		_, _ = v.src.Seek(0, io.SeekStart)
	}
	v.player, v.src = nil, nil
}

// unitVoiceRate is the rate a speech take is really at, which is not the one its
// INFO chunk declares often enough that the label cannot be trusted as it
// stands.
//
// The samples a Relic file holds are the same whatever rate it is handed, so the
// rate is only a label on the audio: it decides how long the take lasts and how
// high it sounds, and nothing in the samples can repair it. What the labels look
// like was measured rather than assumed. Ultimate Apocalypse's necron speech tree
// declares 44100 for 140 of its 146 takes and 11025 or 24000 for the rest; read
// at the declared rate the 11025 takes run 17 seconds where a selection line is
// four, and at 44100 they fit. The base game's own speech declares 22050 and runs
// eight seconds where the line it holds is half that. In every case a take
// declared below 44100 came out two to four times too long and an octave or two
// too low — the "stretched" reply.
//
// So a label of 44100 or above is taken at its word, since those are the rates the
// game's re-encodes use and a music track labelled 44100 matches its length, and
// anything lower is read as the 44100 such audio was authored at instead.
func unitVoiceRate(declared int) int {
	if declared >= fxSoundRealRate {
		return declared
	}
	return fxSoundRealRate
}

// PlayUnitVoiceFile decodes one take and plays it, through the same codec the
// WAV editor uses. A take is normally an .fda and may also be a .wav beside it,
// so the format comes from the path. Decoding runs on the press rather than on a
// worker: a reply is a second of speech, and the press is already the moment the
// user asked to hear it.
func PlayUnitVoiceFile(ui *uiState, virtualPath string) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Tree == nil {
		ui.StatusText = "No mod is loaded."
		return
	}
	data, err := d.Tree.ReadFile(virtualPath)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	var pcm []int16
	var sr, ch int
	if unitVoiceExt(virtualPath) == ".wav" {
		w, err := wav.Parse(data)
		if err != nil {
			ui.StatusText = fmt.Sprintf("%s is not a PCM WAV: %v", filepath.Base(virtualPath), err)
			return
		}
		pcm, sr, ch = wavPCM16(w.PCMData), w.SampleRate, w.Channels
	} else {
		parsed, err := fda.ParseFDAReader(bytes.NewReader(data))
		if err != nil {
			ui.StatusText = fmt.Sprintf("%s is not an FDA: %v", filepath.Base(virtualPath), err)
			return
		}
		pcm, err = fda.DecodeFDAToPCM(parsed)
		if err != nil {
			ui.StatusText = fmt.Sprintf("Could not decode %s: %v", filepath.Base(virtualPath), err)
			return
		}
		sr, ch = int(parsed.Info.SampleRate), int(parsed.Info.Channels)
		if sr == 0 || ch == 0 {
			ui.StatusText = fmt.Sprintf("%s has no usable INFO chunk", filepath.Base(virtualPath))
			return
		}
		sr = unitVoiceRate(sr)
	}
	if sr <= 0 || ch <= 0 {
		ui.StatusText = fmt.Sprintf("%s has no usable sample rate or channel count", filepath.Base(virtualPath))
		return
	}
	ctx, err := audioplayer.Context()
	if err != nil {
		ui.StatusText = fmt.Sprintf("Audio is unavailable: %v", err)
		return
	}
	if sr != audioplayer.SampleRate || ch != audioplayer.Channels {
		pcm, err = fda.ResamplePCM16(pcm, sr, ch, audioplayer.SampleRate, audioplayer.Channels)
		if err != nil {
			ui.StatusText = fmt.Sprintf("Could not convert %s: %v", filepath.Base(virtualPath), err)
			return
		}
		sr, ch = audioplayer.SampleRate, audioplayer.Channels
	}
	pcmBytes := fda.PCM16Bytes(pcm)
	cb := audioplayer.NewChunkBuffer(pcmBytes, sr, ch)
	src := audioplayer.NewChunkSource(cb)
	player := ctx.NewPlayer(src)
	player.SetVolume(1)
	dur := time.Duration(0)
	if sr > 0 && ch > 0 {
		dur = time.Duration(float64(len(pcmBytes)) / float64(sr*ch*2) * float64(time.Second))
	}
	unitVoicePreview.mu.Lock()
	unitVoicePreview.stopLocked()
	player.Play()
	unitVoicePreview.player, unitVoicePreview.src = player, src
	unitVoicePreview.mu.Unlock()
	ui.StatusText = fmt.Sprintf("Playing %s — %s", filepath.Base(virtualPath), dur.Truncate(time.Millisecond))
}

// ConvertUnitVoiceToWAV writes the selected .fda beside itself as PCM WAV, which
// is what an outside editor can open. The rate is written as the one the take is
// heard at rather than as the one its INFO chunk declares, so a reply the game
// plays at 44100 does not come out of the studio an octave down; the samples
// themselves are the decoder's, resampled to nothing.
func ConvertUnitVoiceToWAV(ui *uiState, f unitdef.UnitVoiceFile) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Tree == nil {
		ui.StatusText = "No mod is loaded."
		return
	}
	data, err := d.Tree.ReadFile(f.Path)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not read %s: %v", f.Name, err)
		return
	}
	parsed, err := fda.ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		ui.StatusText = fmt.Sprintf("%s is not an FDA: %v", f.Name, err)
		return
	}
	pcm, err := fda.DecodeFDAToPCM(parsed)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not decode %s: %v", f.Name, err)
		return
	}
	info := parsed.Info
	if info.SampleRate == 0 || info.Channels == 0 || info.SampleSize == 0 {
		ui.StatusText = fmt.Sprintf("%s has no usable INFO chunk", f.Name)
		return
	}
	info.SampleRate = uint32(unitVoiceRate(int(info.SampleRate)))
	var buf bytes.Buffer
	if err := fda.WriteWAVTo(&buf, &info, fda.PCM16Bytes(pcm)); err != nil {
		ui.StatusText = fmt.Sprintf("Could not build a WAV from %s: %v", f.Name, err)
		return
	}
	target := unitVoiceSwapExt(f.Path, ".wav")
	disk, err := d.Tree.WriteOverlayFile(target, buf.Bytes())
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not write %s: %v", target, err)
		return
	}
	ui.StatusText = fmt.Sprintf("Converted %s → %s (%d bytes)", f.Name, disk, buf.Len())
}

// ConvertUnitVoiceToFDA encodes the selected .wav into the .fda the game plays,
// under the same name in the same folder and at the block bitrate the game's own
// speech uses, so a take edited outside replaces the one it came from.
func ConvertUnitVoiceToFDA(ui *uiState, f unitdef.UnitVoiceFile) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Tree == nil {
		ui.StatusText = "No mod is loaded."
		return
	}
	data, err := d.Tree.ReadFile(f.Path)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not read %s: %v", f.Name, err)
		return
	}
	w, err := wav.Parse(data)
	if err != nil {
		ui.StatusText = fmt.Sprintf("%s is not a PCM WAV: %v", f.Name, err)
		return
	}
	fdaData, err := fda.EncodeWAV(w, 2048)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not encode %s: %v", f.Name, err)
		return
	}
	target := unitVoiceSwapExt(f.Path, ".fda")
	disk, err := d.Tree.WriteOverlayFile(target, fdaData)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Could not write %s: %v", target, err)
		return
	}
	ui.StatusText = fmt.Sprintf("Converted %s → %s (%d bytes)", f.Name, disk, len(fdaData))
}

// unitVoiceExt is the lower-case extension of a mod tree path, which is
// backslash separated whether or not the host is.
func unitVoiceExt(p string) string {
	i := strings.LastIndexAny(p, `\/`)
	base := p
	if i >= 0 {
		base = p[i+1:]
	}
	if j := strings.LastIndexByte(base, '.'); j >= 0 {
		return strings.ToLower(base[j:])
	}
	return ""
}

// unitVoiceSwapExt is the path a converted take goes to: the same folder and the
// same base name with the other extension, which is how a take and the file it
// converts to are kept as one reply.
func unitVoiceSwapExt(p, ext string) string {
	i := strings.LastIndexAny(p, `\/`)
	dir, base := "", p
	if i >= 0 {
		dir, base = p[:i+1], p[i+1:]
	}
	if j := strings.LastIndexByte(base, '.'); j >= 0 {
		base = base[:j]
	}
	return dir + base + ext
}

// ── Adding and removing replies and actions ───────────────────────────────────

// OpenUnitVoiceAddPicker asks for a reply to add to the selected action. An .fda
// joins as it is and a .wav is encoded to .fda on the way in, because .fda is the
// only thing the game plays: a recording made in a WAV editor never has to be
// converted by hand first.
func OpenUnitVoiceAddPicker(ui *uiState, cat unitdef.UnitVoiceCategory) {
	OpenFilePicker(ui, pickConfig{
		Title:    "Add a reply",
		Subtitle: "An .fda joins " + cat.Name + " as it is; a .wav is encoded to .fda on the way in",
		Hint:     cat.Folder + `\reply.fda`,
		Exts:     []string{".fda", ".wav"},
		Start:    ui.GamePath,
		OnPick: func(path string) (bool, string) {
			return addUnitVoicePick(ui, cat, path)
		},
	})
}

// addUnitVoicePick writes the chosen file into the action's folder. It reads from
// disk or from the mod, whichever the path names, the way the timeline import
// does.
func addUnitVoicePick(ui *uiState, cat unitdef.UnitVoiceCategory, path string) (bool, string) {
	d := CurrentUnitDetails(ui)
	if d == nil || d.Tree == nil {
		return false, "No mod is loaded."
	}
	var raw []byte
	var err error
	if filepath.IsAbs(path) {
		raw, err = os.ReadFile(path)
	} else {
		raw, err = d.Tree.ReadFile(path)
	}
	if err != nil {
		return false, "Read failed: " + err.Error()
	}
	data := raw
	if unitVoiceExt(path) == ".wav" {
		w, err := wav.Parse(raw)
		if err != nil {
			return false, "Not a PCM WAV: " + err.Error()
		}
		data, err = fda.EncodeWAV(w, 2048)
		if err != nil {
			return false, "Could not encode: " + err.Error()
		}
	} else if _, err := fda.ParseFDAReader(bytes.NewReader(raw)); err != nil {
		// A file that does not parse is not something the game could play, and
		// adding it would put a silent take in the list.
		return false, "Not an FDA: " + err.Error()
	}
	name := unitVoiceBaseName(path) + ".fda"
	target := unitVoiceFreePath(d.Tree, cat.Folder, name)
	disk, err := d.Tree.WriteOverlayFile(target, data)
	if err != nil {
		return false, "Write failed: " + err.Error()
	}
	ui.UnitVoiceFileSel = 0
	ui.StatusText = fmt.Sprintf("Added %s to %s: %s", filepath.Base(path), cat.Name, disk)
	return true, ""
}

// unitVoiceBaseName is the file name without its folder or extension.
func unitVoiceBaseName(p string) string {
	base := p
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		base = p[i+1:]
	}
	if j := strings.LastIndexByte(base, '.'); j >= 0 {
		base = base[:j]
	}
	return base
}

// unitVoiceFreePath keeps an added reply from overwriting one that is already
// there: the same name with a number is the same take twice, rather than a
// replacement nobody asked for.
func unitVoiceFreePath(tree *modtree.ModTree, folder, name string) string {
	base := unitVoiceBaseName(name)
	candidate := folder + `\` + name
	for i := 2; i < 100 && tree.FindFile(candidate) != nil; i++ {
		candidate = fmt.Sprintf(`%s\%s_%d.fda`, folder, base, i)
	}
	return candidate
}

// DeleteUnitVoiceFile moves the selected reply to the Recycle Bin, through the
// same action the file tree uses, so a reply deleted from the section can still
// be brought back.
func DeleteUnitVoiceFile(ui *uiState, f unitdef.UnitVoiceFile) {
	if err := DeleteCurrentModFile(ui, f.Path); err != nil {
		ui.StatusText = "Could not delete " + f.Name + ": " + err.Error()
		return
	}
	ui.UnitVoiceFileSel = 0
	ui.StatusText = "Moved " + f.Name + " to the Recycle Bin"
}

// DeleteUnitVoiceAction removes an action: every reply it holds goes to the
// Recycle Bin and the folder that made it an action goes with them. A file that
// belongs to another mod or to an archive is left where it is and reported,
// because the section cannot remove what the game loads from elsewhere.
func DeleteUnitVoiceAction(ui *uiState) {
	set := CurrentUnitVoices(ui)
	cat, ok := SelectedUnitVoiceCategory(ui, set)
	if !ok {
		ui.StatusText = "Choose an action to delete"
		return
	}
	removed, kept := 0, 0
	for _, f := range cat.Files {
		if err := DeleteCurrentModFile(ui, f.Path); err != nil {
			kept++
			continue
		}
		removed++
	}
	if removed > 0 && kept == 0 && ui.ModTree != nil {
		if disk, err := ui.ModTree.OverlayPath(cat.Folder); err == nil {
			// The folder is empty once its files are in the bin; leaving it
			// would keep an action in the list that plays nothing.
			_ = os.Remove(disk)
		}
		_ = RefreshModTreePreservingUI(ui)
	}
	ui.UnitSelRow = 0
	ui.UnitVoiceFileSel = 0
	switch {
	case removed == 0 && kept == 0:
		ui.StatusText = cat.Name + " holds no file this mod owns"
	case kept > 0:
		ui.StatusText = fmt.Sprintf("Removed %d file(s) of %s; %d stayed: they belong to another mod or an archive", removed, cat.Name, kept)
	default:
		ui.StatusText = fmt.Sprintf("Removed the %s action (%d file(s) to the Recycle Bin)", cat.Name, removed)
	}
}

// OpenUnitVoiceNewActionDialog asks for the name of a new action — the folder
// under the unit's speech folder that the game looks for the action in.
func OpenUnitVoiceNewActionDialog(ui *uiState) {
	if UnitVoiceActionFolder(ui) == "" {
		ui.StatusText = "This unit declares no speech_directory, so there is no folder to add an action to"
		return
	}
	ui.UnitVoiceNewActionDialog = true
	ui.UnitVoiceNewActionStatus = ""
	ui.UnitVoiceNewActionEditor.SetText("")
}

func CloseUnitVoiceNewActionDialog(ui *uiState) {
	ui.UnitVoiceNewActionDialog = false
}

// UnitVoiceActionFolder is where a new action goes: the folder the unit's replies
// were found in, or the first one the game would look in when the mod keeps none
// yet.
func UnitVoiceActionFolder(ui *uiState) string {
	set := CurrentUnitVoices(ui)
	if set.Root != "" {
		return set.Root
	}
	if set.Suffix == "" {
		return ""
	}
	return unitdef.UnitVoiceFolderPath(unitdef.UnitVoiceRoots[0], set.Suffix)
}

// CreateUnitVoiceAction makes the action's folder in the mod. The game needs no
// file in it to treat it as an action; an empty one is where the next reply is
// added.
func CreateUnitVoiceAction(ui *uiState) {
	folder := UnitVoiceActionFolder(ui)
	if folder == "" || ui.ModTree == nil {
		ui.UnitVoiceNewActionStatus = "There is no speech folder to write into."
		return
	}
	name := strings.TrimSpace(ui.UnitVoiceNewActionEditor.Text())
	name = strings.Trim(name, `\/`)
	switch {
	case name == "":
		ui.UnitVoiceNewActionStatus = "Name the action first"
		return
	case strings.ContainsAny(name, `\/:*?"<>|`):
		ui.UnitVoiceNewActionStatus = `A folder name cannot hold \ / : * ? " < > |`
		return
	}
	target := folder + `\` + name
	disk, err := ui.ModTree.OverlayPath(target)
	if err != nil {
		ui.UnitVoiceNewActionStatus = err.Error()
		return
	}
	if err := os.MkdirAll(disk, 0755); err != nil {
		ui.UnitVoiceNewActionStatus = err.Error()
		return
	}
	ui.UnitVoiceNewActionDialog = false
	if err := RefreshModTreePreservingUI(ui); err != nil {
		ui.StatusText = "Action created, but the mod tree refresh failed: " + err.Error()
		return
	}
	for i, c := range CurrentUnitVoices(ui).Categories {
		if strings.EqualFold(c.Name, name) {
			ui.UnitSelRow = i
			break
		}
	}
	ui.UnitVoiceFileSel = 0
	ui.StatusText = "Added the " + name + " action to " + folder
}

// HandleUnitVoiceNewActionInput owns the dialog while it is up, so a click meant
// for the column behind it reaches nothing.
func HandleUnitVoiceNewActionInput(ui *uiState, gtx layout.Context) {
	submitted := false
	for {
		ev, ok := ui.UnitVoiceNewActionEditor.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if ui.UnitVoiceNewActionCancel.Clicked(gtx) {
		CloseUnitVoiceNewActionDialog(ui)
		ui.StatusText = "New action cancelled"
		return
	}
	if ui.UnitVoiceNewActionOk.Clicked(gtx) || submitted {
		CreateUnitVoiceAction(ui)
	}
}

// LayoutUnitVoiceNewActionDialog draws the naming card over the column: the same
// dim and border the other dialogs use, so a dialog looks like a dialog wherever
// it is raised.
func LayoutUnitVoiceNewActionDialog(ui *uiState, gtx layout.Context) layout.Dimensions {
	cardW := gtx.Dp(unit.Dp(380))
	cardH := gtx.Dp(unit.Dp(172))
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			st := clip.Rect{Max: sz}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 110)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Pt(cardW, cardH)
				gtx.Constraints.Max = image.Pt(cardW, cardH)
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, uikit.CPanelBg, gtx.Constraints.Max, gtx.Dp(4))
							return layout.Dimensions{Size: gtx.Constraints.Max}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(13), "New action")
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(9), "Folder in "+unitVoiceClip(UnitVoiceActionFolder(ui), 46))
										l.Color = uikit.CTextDim
										l.MaxLines = 1
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										h := gtx.Dp(unit.Dp(28))
										gtx.Constraints.Min.Y = h
										gtx.Constraints.Max.Y = h
										return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												ed := material.Editor(ui.Th, &ui.UnitVoiceNewActionEditor, "")
												ed.TextSize = unit.Sp(11)
												ed.HintColor = uikit.CTextDim
												ed.Hint = "move, attack, selection…"
												return ed.Layout(gtx)
											})
										})
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if ui.UnitVoiceNewActionStatus == "" {
											return layout.Dimensions{}
										}
										l := material.Label(ui.Th, unit.Sp(9), ui.UnitVoiceNewActionStatus)
										l.Color = rgb(0xB00020)
										l.MaxLines = 2
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
											layout.Flexed(1, flexGap()),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.UnitVoiceNewActionCancel, "Cancel", false)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return DialogButton(ui, gtx, &ui.UnitVoiceNewActionOk, "Create", true)
											}),
										)
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
