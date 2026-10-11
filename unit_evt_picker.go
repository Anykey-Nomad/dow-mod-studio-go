package main

import (
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/wav"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"dow-mod-studio/internal/modtree"
)

// The Events section's fields take a value from a list rather than only from
// typing: clicking the event name, the FX path, the sound path or the marker
// opens this picker over the object window. A value typed in the picker's
// filter is used as it is, for a name or path the list does not carry.

// evtPickTarget is the event field a picker fills in.
type evtPickTarget int

const (
	evtPickNone evtPickTarget = iota
	evtPickName
	evtPickFX
	evtPickSound
	evtPickMarker
)

// evtPickerMaxHits caps the rows the picker draws, so a folder of hundreds of
// effects does not lay out hundreds of rows a frame.
const evtPickerMaxHits = 300

// evtPicker is the list dialog. all is every value the target can take, hits
// is what the filter lets through, and clicks has one entry per hit.
type evtPicker struct {
	open   bool
	target evtPickTarget
	ed     widget.Editor
	filter string
	all    []string
	hits   []string
	clicks []widget.Clickable
	list   layout.List

	useTyped widget.Clickable
	browse   widget.Clickable
	cancel   widget.Clickable
}

// openEvtPicker shows the list for one field, with every value of it ready.
func (ui *uiState) openEvtPicker(t evtPickTarget) {
	p := &ui.evtPick
	p.open = true
	p.target = t
	p.all = ui.evtPickerItems(t)
	p.ed.SingleLine = true
	p.ed.Submit = true
	p.ed.SetText("")
	p.filter = ""
	ui.refreshEvtPickerHits()
}

// evtPickerItems lists the values a target can take, sorted and without
// repeats.
func (ui *uiState) evtPickerItems(t evtPickTarget) []string {
	var out []string
	switch t {
	case evtPickName:
		if ui.unitEvtDoc != nil {
			for _, def := range ui.unitEvtDoc.Events() {
				out = append(out, def.Name)
			}
		}
	case evtPickMarker:
		if v := ui.unitModelViewer(); v != nil && v.model != nil {
			for _, m := range v.model.Markers {
				out = append(out, m.Name)
			}
		}
	case evtPickFX:
		out = evtTreeFolderFiles(ui.modTree, `\art\fx\`, []string{".lua"}, true)
	case evtPickSound:
		out = evtTreeFolderFiles(ui.modTree, `\sound\`, []string{".fda", ".wav"}, false)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	uniq := out[:0]
	for i, s := range out {
		if i == 0 || !strings.EqualFold(s, uniq[len(uniq)-1]) {
			uniq = append(uniq, s)
		}
	}
	return uniq
}

// evtTreeFolderFiles lists the files of the mod tree that sit under one folder
// of the game's data and carry one of the extensions. The names are relative
// to that folder, backslash separated the way a .whe writes them. keepExt says
// whether the extension stays on the name: an effect's does, a sound's does
// not, because a .whe names a sound without one.
func evtTreeFolderFiles(tree *modtree.ModTree, folder string, exts []string, keepExt bool) []string {
	if tree == nil {
		return nil
	}
	var out []string
	lowerFolder := strings.ToLower(folder)
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if f == nil {
			return
		}
		full := strings.ReplaceAll(f.FullPath, "/", `\`)
		i := strings.Index(strings.ToLower(full), lowerFolder)
		if i < 0 {
			return
		}
		rel := full[i+len(folder):]
		ext := ""
		if k := strings.LastIndexByte(rel, '.'); k >= 0 && !strings.ContainsRune(rel[k:], '\\') {
			ext = strings.ToLower(rel[k:])
		}
		matched := false
		for _, e := range exts {
			if ext == e {
				matched = true
				break
			}
		}
		if !matched {
			return
		}
		if !keepExt {
			rel = rel[:len(rel)-len(ext)]
		}
		out = append(out, rel)
	})
	return out
}

// refreshEvtPickerHits filters the list by what is in the filter box.
func (ui *uiState) refreshEvtPickerHits() {
	p := &ui.evtPick
	q := strings.ToLower(strings.TrimSpace(p.filter))
	p.hits = p.hits[:0]
	for _, s := range p.all {
		if q == "" || strings.Contains(strings.ToLower(s), q) {
			p.hits = append(p.hits, s)
			if len(p.hits) >= evtPickerMaxHits {
				break
			}
		}
	}
	p.clicks = make([]widget.Clickable, len(p.hits))
}

// handleEvtPickerInput is the picker's input: typing narrows the list, a row
// picks its value, Use typed and Enter take the filter text as it is, and
// Cancel closes without writing anything.
func (ui *uiState) handleEvtPickerInput(gtx layout.Context) {
	p := &ui.evtPick
	submitted := false
	for {
		ev, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			submitted = true
		}
	}
	if text := strings.TrimSpace(p.ed.Text()); text != p.filter {
		p.filter = text
		ui.refreshEvtPickerHits()
	}
	if p.cancel.Clicked(gtx) {
		p.open = false
		return
	}
	if p.target == evtPickSound && p.browse.Clicked(gtx) {
		p.open = false
		ui.openSoundFilePicker()
		return
	}
	for i := range p.clicks {
		if p.clicks[i].Clicked(gtx) && i < len(p.hits) {
			ui.applyEvtPick(p.target, p.hits[i])
			return
		}
	}
	if p.useTyped.Clicked(gtx) || submitted {
		ui.applyEvtPick(p.target, p.filter)
	}
}

// applyEvtPick writes the chosen value into the field it was opened from and
// closes the picker. A chosen event name also fills in the FX, sound and marker
// its definition holds, so the form shows what picking it will fire.
func (ui *uiState) applyEvtPick(t evtPickTarget, value string) {
	value = strings.TrimSpace(value)
	ui.evtPick.open = false
	if value == "" {
		return
	}
	switch t {
	case evtPickName:
		ui.unitEvtNameEd.SetText(value)
		ui.unitEvtFxEd.SetText("")
		ui.unitEvtSndEd.SetText("")
		ui.unitEvtMarkerEd.SetText("")
		if ui.unitEvtDoc != nil {
			if def, ok := ui.unitEvtDoc.Events()[strings.ToLower(value)]; ok {
				if p, has := def.First("sound"); has {
					ui.unitEvtSndEd.SetText(p)
				} else if p, has := def.First("fx"); has {
					ui.unitEvtFxEd.SetText(p)
				}
				if m, has := def.First("marker"); has {
					ui.unitEvtMarkerEd.SetText(m)
				}
			}
		}
	case evtPickFX:
		ui.unitEvtFxEd.SetText(value)
	case evtPickSound:
		ui.unitEvtSndEd.SetText(value)
	case evtPickMarker:
		ui.unitEvtMarkerEd.SetText(value)
	}
}

// openSoundFilePicker shows the file dialog for the sound field. It uses the
// mod file tree, the sounds the game and the mod hold, and takes .fda or .wav.
// The conversion of a .wav happens here, on the editor's side, not in the
// dialog.
func (ui *uiState) openSoundFilePicker() {
	ui.openFilePicker(pickConfig{
		title:    "Choose a sound",
		subtitle: "An .fda from the mod file tree; a .wav is converted to .fda in this mod",
		hint:     `Data\sound\...`,
		exts:     []string{".fda", ".wav"},
		modTree:  true,
		onPick: func(path string) (bool, string) {
			field, err := ui.soundFieldFor(path)
			if err != nil {
				return false, err.Error()
			}
			ui.unitEvtSndEd.SetText(field)
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
func (ui *uiState) soundFieldFor(path string) (string, error) {
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
		if filepath.IsAbs(path) || ui.modTree == nil {
			raw, err = os.ReadFile(path)
		} else {
			raw, err = ui.modTree.ReadFile(path)
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
		if _, err := writeFDAOverlay(ui.modTree, target, fdaData); err != nil {
			return "", err
		}
		ui.statusText = fmt.Sprintf("Converted %s to %s", filepath.Base(path), target)
	}
	return rel, nil
}

// evtPickerTitle is what the picker's card says it is choosing.
func evtPickerTitle(t evtPickTarget) string {
	switch t {
	case evtPickName:
		return "Choose an event"
	case evtPickFX:
		return "Choose an FX"
	case evtPickSound:
		return "Choose a sound"
	case evtPickMarker:
		return "Choose a model marker"
	}
	return "Choose"
}

// layoutEvtPicker draws the picker over the window, the same card the
// Import dialog uses.
func (ui *uiState) layoutEvtPicker(gtx layout.Context) layout.Dimensions {
	p := &ui.evtPick
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
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							sz := gtx.Constraints.Max
							rrect(gtx, cPanelBg, sz, gtx.Dp(4))
							return layout.Dimensions{Size: sz}
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(14), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Label(ui.th, unit.Sp(14), evtPickerTitle(p.target))
										l.Color = cText
										return l.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										h := gtx.Dp(unit.Dp(28))
										gtx.Constraints.Min.Y = h
										gtx.Constraints.Max.Y = h
										return widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												ed := material.Editor(ui.th, &p.ed, "")
												ed.TextSize = unit.Sp(11)
												ed.HintColor = cTextDim
												ed.Hint = "filter, or type a value"
												return ed.Layout(gtx)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										note := "Nothing to choose from here: type the value."
										if len(p.all) > 0 {
											note = strconv.Itoa(len(p.hits)) + " of " + strconv.Itoa(len(p.all)) + " shown"
										}
										l := material.Label(ui.th, unit.Sp(9), note)
										l.Color = cTextDim
										l.MaxLines = 1
										return l.Layout(gtx)
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return ui.layoutListWithScrollbar(gtx, nil, &p.list, unitRowHeightDp, len(p.hits), func(gtx layout.Context, i int) layout.Dimensions {
											return p.clicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												l := material.Label(ui.th, unit.Sp(10), p.hits[i])
												l.Color = cText
												l.MaxLines = 1
												return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6)}.Layout(gtx, l.Layout)
											})
										})
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										children := []layout.FlexChild{}
										if p.target == evtPickSound {
											children = append(children,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return ui.smallButton(gtx, &p.browse, "Browse file...")
												}),
												layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
											)
										}
										children = append(children,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.smallButton(gtx, &p.useTyped, "Use typed")
											}),
											layout.Flexed(1, layout.Spacer{Width: unit.Dp(0)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return ui.smallButton(gtx, &p.cancel, "Cancel")
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
