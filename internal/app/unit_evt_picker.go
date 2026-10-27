package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"fmt"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handleEvtPickerInput is the picker's input: typing narrows the list, a row
// picks its value, Use typed and Enter take the filter text as it is, and
// Cancel closes without writing anything.
func HandleEvtPickerInput(ui *uiState, gtx layout.Context) {
	p := &ui.EvtPick
	submitted := false
	for {
		ev, ok := p.Ed.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if text := strings.TrimSpace(p.Ed.Text()); text != p.Filter {
		p.Filter = text
		RefreshEvtPickerHits(ui)
	}
	if p.Cancel.Clicked(gtx) {
		p.Open = false
		return
	}
	if p.Target == evtPickSound && p.Browse.Clicked(gtx) {
		p.Open = false
		OpenSoundFilePicker(ui)
		return
	}
	for i := range p.Clicks {
		if p.Clicks[i].Clicked(gtx) && i < len(p.Hits) {
			ApplyEvtPick(ui, p.Target, p.Hits[i])
			return
		}
	}
	if p.UseTyped.Clicked(gtx) || submitted {
		ApplyEvtPick(ui, p.Target, p.Filter)
	}
}

// openSoundFilePicker shows the file dialog for the sound field. It uses the
// mod file tree, the sounds the game and the mod hold, and takes .fda or .wav.
// The conversion of a .wav happens here, on the editor's side, not in the
// dialog.
func OpenSoundFilePicker(ui *uiState) {
	OpenFilePicker(ui, pickConfig{
		Title:    "Choose a sound",
		Subtitle: "An .fda from the mod file tree; a .wav is converted to .fda in this mod",
		Hint:     `Data\sound\...`,
		Exts:     []string{".fda", ".wav"},
		ModTree:  true,
		OnPick: func(path string) (bool, string) {
			field, err := SoundFieldFor(ui, path)
			if err != nil {
				return false, err.Error()
			}
			ui.UnitEvtSndEd.SetText(field)
			return true, ""
		},
	})
}

// soundRelUnderData is the part of a path after its data\sound folder, without
// the extension, backslash separated. The path may be a disk path or a mod
// tree path, which starts at Data. It is "" when the path is not under the
// sound folder.
func soundRelUnderData(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	lower := strings.ToLower(p)
	const marker = `data\sound\`
	var rel string
	if strings.HasPrefix(lower, marker) {
		rel = p[len(marker):]
	} else if i := strings.LastIndex(lower, `\`+marker); i >= 0 {
		rel = p[i+1+len(marker):]
	} else {
		return ""
	}
	if e := pickExt(rel); e != "" {
		rel = rel[:len(rel)-len(e)]
	}
	return rel
}

// soundFieldFor turns a chosen file into the sound field's value: its path
// under data\sound without the extension. A .wav is first encoded to .fda at
// the same place in the mod folder, which is what the game plays; an .fda
// must already sit under data\sound.
func SoundFieldFor(ui *uiState, path string) (string, error) {
	ext := pickExt(path)
	rel := soundRelUnderData(path)
	if rel == "" {
		if ext == ".fda" {
			return "", fmt.Errorf("pick an .fda from data\\sound")
		}
		rel = strings.TrimSuffix(filepath.Base(path), ext)
	}
	if ext == ".wav" {
		var raw []byte
		var err error
		if filepath.IsAbs(path) || ui.ModTree == nil {
			raw, err = os.ReadFile(path)
		} else {
			raw, err = ui.ModTree.ReadFile(path)
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %v", path, err)
		}
		w, err := wav.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("not a WAV: %v", err)
		}
		fdaData, err := fda.EncodeWAV(w, 2048)
		if err != nil {
			return "", err
		}
		target := `Data\sound\` + rel + ".fda"
		if _, err := writeFDAOverlay(ui.ModTree, target, fdaData); err != nil {
			return "", err
		}
		ui.StatusText = fmt.Sprintf("Converted %s to %s", filepath.Base(path), target)
	}
	return rel, nil
}

// layoutEvtPicker draws the picker over the window, the same card the
// Import dialog uses.
func LayoutEvtPicker(ui *uiState, gtx layout.Context) layout.Dimensions {
	p := &ui.EvtPick
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			st := clip.Rect{Max: sz}.Push(gtx.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 60)}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st.Pop()
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cardW := gtx.Dp(unit.Dp(420))
				cardH := (gtx.Constraints.Max.Y * 9) / 10
				if minH := gtx.Dp(unit.Dp(300)); cardH < minH {
					cardH = minH
				}
				gtx.Constraints.Max.X = cardW
				gtx.Constraints.Min.X = cardW
				gtx.Constraints.Max.Y = cardH
				gtx.Constraints.Min.Y = cardH
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Constraints.Max
							rrect(gtx, uikit.CPanelBg, sz, gtx.Dp(4))
							return layout.Dimensions{Size: sz}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(14), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.Th, unit.Sp(14), evtPickerTitle(p.Target))
										l.Color = uikit.CText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										h := gtx.Dp(unit.Dp(28))
										gtx.Constraints.Min.Y = h
										gtx.Constraints.Max.Y = h
										return widget.Border{Color: uikit.CBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												Ed := material.Editor(ui.Th, &p.Ed, "")
												Ed.TextSize = unit.Sp(11)
												Ed.HintColor = uikit.CTextDim
												Ed.Hint = "filter, or type a value"
												return Ed.Layout(gtx)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										note := "Nothing to choose from here: type the value."
										if len(p.All) > 0 {
											note = strconv.Itoa(len(p.Hits)) + " of " + strconv.Itoa(len(p.All)) + " shown"
										}
										l := material.Label(ui.Th, unit.Sp(9), note)
										l.Color = uikit.CTextDim
										l.MaxLines = 1
										return l.Layout(gtx)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return LayoutListWithScrollbar(ui, gtx, nil, &p.List, unitRowHeightDp, len(p.Hits), func(gtx layout.Context, i int) layout.Dimensions {
											return p.Clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.Th, unit.Sp(10), p.Hits[i])
												l.Color = uikit.CText
												l.MaxLines = 1
												return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6)}.Layout(gtx, l.Layout)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										children := []layout.FlexChild{}
										if p.Target == evtPickSound {
											children = append(children,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return SmallButton(ui, gtx, &p.Browse, "Browse file...")
												}),
												layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
											)
										}
										children = append(children,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return SmallButton(ui, gtx, &p.UseTyped, "Use typed")
											}),
											layout.Flexed(1, layout.Spacer{Width: unit.Dp(0)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return SmallButton(ui, gtx, &p.Cancel, "Cancel")
											}),
										)
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
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

// unitEvtLoadSound resolves one sound path against the mod tree, through the
// same rules the game uses, and returns the decoded sample for it. The first
// file of the bundle plays: a folder of variants sounds different each time in
// the game, but a preview the user is checking wants the same answer twice.
func UnitEvtLoadSound(ui *uiState, p string) (*fx.SoundSample, string, error) {
	pl := &ui.EvtPlayer
	if pl.Cache == nil {
		pl.Cache = map[string]*fx.SoundSample{}
	}
	if ui.ModTree == nil {
		return nil, "", fmt.Errorf("no mod is loaded")
	}
	roots := materialSearchRoots(ui.ModTree)
	b, ok := fx.ResolveSound(ui.ModTree, roots, p)
	if !ok || len(b.Files) == 0 {
		return nil, "", whe.SoundMissing(p)
	}
	file := b.Files[0]
	if s, done := pl.Cache[file]; done {
		return s, file, nil
	}
	s, err := fxSoundDecodeForPlayback(ui.ModTree, roots, file)
	if err != nil {
		return nil, file, err
	}
	pl.Cache[file] = s
	return s, file, nil
}

// unitEvtPlaySelectedSound plays the selected event's sound. The path typed
// into the form wins over the saved one, so a retyped path can be auditioned
// before Set writes it into the document.
func UnitEvtPlaySelectedSound(ui *uiState) {
	tl := SelectedUnitEventTimeline(ui)
	if tl == nil || ui.UnitEvtDoc == nil || ui.UnitEvtSel < 0 || ui.UnitEvtSel >= len(tl.Events) {
		ui.StatusText = "Select a sound event first"
		return
	}
	ev := tl.Events[ui.UnitEvtSel]
	def, ok := ui.UnitEvtDoc.Events()[strings.ToLower(ev.Name)]
	if !ok {
		ui.StatusText = ev.Name + " has no definition in this .whe"
		return
	}
	path, _ := def.First("sound")
	if typed := strings.TrimSpace(ui.UnitEvtSndEd.Text()); typed != "" {
		path = typed
	}
	path = strings.TrimSpace(path)
	if path == "" {
		ui.StatusText = ev.Name + " names no sound"
		return
	}
	p := &ui.EvtPlayer
	if p.Audition != nil {
		p.Audition.Stop()
		p.Audition = nil
	}
	s, file, err := UnitEvtLoadSound(ui, path)
	if err != nil {
		ui.StatusText = "Play failed: " + err.Error()
		return
	}
	v, err := unitEvtStartVoice(s)
	if err != nil {
		ui.StatusText = "Play failed: " + err.Error()
		return
	}
	p.Audition = v
	ui.StatusText = "Playing " + filepath.Base(file)
}

// unitEvtPlayerTick is one frame of the transport. It reads the three buttons,
// advances the playhead, and starts whatever the playhead has crossed. While a
// timeline plays it asks for the next frame, which is what keeps the clock
// running when the user touches nothing.
func UnitEvtPlayerTick(ui *uiState, gtx layout.Context) {
	p := &ui.EvtPlayer
	if p.Playing && (p.ForDoc != ui.UnitEvtDoc || p.ForTimeline != ui.UnitEvtTimeline) {
		p.StopAll()
	}
	if p.TlStop.Clicked(gtx) {
		p.StopAll()
		ui.StatusText = "Playback stopped"
	}
	if p.TlPlay.Clicked(gtx) {
		if p.Playing {
			p.Paused = !p.Paused
		} else {
			UnitEvtTimelinePlay(ui, gtx)
		}
	}
	if p.PlayEv.Clicked(gtx) {
		UnitEvtPlaySelectedSound(ui)
	}
	if p.Audition != nil && !time.Now().Before(p.Audition.EndsAt) {
		p.Audition.Stop()
		p.Audition = nil
	}
	if !p.Playing {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	dt := now.Sub(p.Last).Seconds()
	p.Last = now
	if dt < 0 {
		dt = 0
	}
	// A stall — a modal dialog, a slow decode on the first play — must not
	// skip the sounds it put the playhead past; it counts as the frame it is.
	if dt > 0.25 {
		dt = 0.25
	}
	if !p.Paused && !p.Dragging {
		p.Pos += dt
	}
	for p.Next < len(p.Order) && p.Order[p.Next].At <= p.Pos {
		f := p.Order[p.Next]
		p.Next++
		s, _, err := UnitEvtLoadSound(ui, f.Path)
		if err != nil {
			log.Printf("evt play: %s: %v", f.Name, err)
			continue
		}
		v, err := unitEvtStartVoice(s)
		if err != nil {
			log.Printf("evt play: %s: %v", f.Name, err)
			continue
		}
		p.Voices = append(p.Voices, v)
	}
	p.Expire()
	if p.Pos >= p.Duration && len(p.Voices) == 0 {
		p.Playing = false
		p.Paused = false
	}
	gtx.Execute(op.InvalidateCmd{At: now.Add(16 * time.Millisecond)})
}

// layoutUnitEvtTransport is the row above the events: play/pause, stop, the
// playhead's time, and the scrub strip.
func LayoutUnitEvtTransport(ui *uiState, gtx layout.Context) layout.Dimensions {
	p := &ui.EvtPlayer
	noTl := ui.UnitEvtDoc == nil || SelectedUnitEventTimeline(ui) == nil
	btn := "Play"
	if p.Playing {
		if p.Paused {
			btn = "Resume"
		} else {
			btn = "Pause"
		}
	}
	return layout.Inset{Top: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &p.TlPlay, btn, noTl)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return SmallButtonDisabled(ui, gtx, &p.TlStop, "Stop", !p.Playing)
			}),
			layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				text := ""
				if p.Playing || p.Pos > 0 {
					text = fmt.Sprintf("%.2f / %.2f s", p.Pos, p.Duration)
				}
				l := material.Label(ui.Th, unit.Sp(9), text)
				l.Color = uikit.CTextDim
				l.MaxLines = 1
				return l.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return LayoutUnitEvtScrub(ui, gtx, 10)
				})
			}),
		)
	})
}

// layoutUnitEvtScrub is the strip itself: a played portion, a tick per sound
// in timeline order, and the playhead. Press or drag winds to the pointer's
// position — the whole strip is the target, so it needs no handle to aim at.
func LayoutUnitEvtScrub(ui *uiState, gtx layout.Context, hDp int) layout.Dimensions {
	p := &ui.EvtPlayer
	w := gtx.Constraints.Max.X
	if w < 1 {
		w = 1
	}
	h := gtx.Dp(unit.Dp(hDp))
	gtx.Constraints.Min = image.Pt(w, h)
	gtx.Constraints.Max = image.Pt(w, h)
	frac := 0.0
	if p.Duration > 0 {
		frac = math.Max(0, math.Min(1, p.Pos/p.Duration))
	}
	posX := int(float64(w) * frac)
	// Input first: the strip registers itself, reads whatever the pointer did
	// to it, and only then draws at the position those events settled on.
	stack := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
	event.Op(gtx.Ops, &p.Scrub)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &p.Scrub, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Press, pointer.Drag:
			if !p.Playing {
				break
			}
			f := float64(e.Position.X) / float64(w)
			p.Dragging = true
			p.Seek(math.Max(0, math.Min(1, f)))
		case pointer.Release, pointer.Cancel:
			p.Dragging = false
		}
	}
	stack.Pop()
	unitEvtFillRect(gtx, image.Rect(0, 0, w, h), uikit.CContentBg)
	if posX > 0 {
		unitEvtFillRect(gtx, image.Rect(0, 0, posX, h), uikit.CSelectBg)
	}
	if p.Duration > 0 {
		for _, f := range p.Order {
			x := int(float64(w) * (f.At / p.Duration))
			if x < 0 {
				x = 0
			}
			if x >= w {
				x = w - 1
			}
			unitEvtFillRect(gtx, image.Rect(x, 0, x+1, h), cEventSnd)
		}
	}
	unitEvtFillRect(gtx, image.Rect(posX, 0, minInt(posX+2, w), h), rgba(255, 255, 255, 200))
	return layout.Dimensions{Size: image.Pt(w, h)}
}
