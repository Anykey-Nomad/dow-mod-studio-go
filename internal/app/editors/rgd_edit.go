package editors

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ── helpers for RGD textual editing ────────────────────────────────────────
func RgdEnsureEditorClick(ui *uistate.State, ed *widget.Editor) *uistate.RgdEditorClickState {
	if ui.RgdEditorClicks == nil {
		ui.RgdEditorClicks = make(map[*widget.Editor]*uistate.RgdEditorClickState)
	}
	state, ok := ui.RgdEditorClicks[ed]
	if !ok {
		state = &uistate.RgdEditorClickState{Editor: ed}
		ui.RgdEditorClicks[ed] = state
	}
	return state
}

func FocusClickedRgdEditor(ui *uistate.State, gtx layout.Context) {
	for _, state := range ui.RgdEditorClicks {
		if state.Click.Clicked(gtx) {
			gtx.Execute(key.FocusCmd{Tag: state.Editor})
		}
	}
}

func RgdEnsureSuggestionClicks(ui *uistate.State, t *uistate.Tab, key string, n int) []widget.Clickable {
	if t.RgdSuggestionClicks == nil {
		t.RgdSuggestionClicks = make(map[string][]widget.Clickable)
	}
	clicks := t.RgdSuggestionClicks[key]
	if len(clicks) < n {
		clicks = append(clicks, make([]widget.Clickable, n-len(clicks))...)
	} else if len(clicks) > n {
		clicks = clicks[:n]
	}
	t.RgdSuggestionClicks[key] = clicks
	return clicks
}

func RgdCollectStringValues(e *rgd.RgdEntry, values map[string]bool) {
	if e == nil {
		return
	}
	switch e.Type {
	case rgd.RgdTypeString:
		if e.StrVal != "" {
			values[e.StrVal] = true
		}
	case rgd.RgdTypeWString:
		if e.WStrVal != "" {
			values[e.WStrVal] = true
		}
	}
	if ref := e.RefString(); ref != "" {
		values[ref] = true
	}
	for _, child := range e.Children {
		RgdCollectStringValues(child, values)
	}
}

func RgdSuggestionsFor(t *uistate.Tab, e *rgd.RgdEntry) []string {
	if t == nil || t.RgdFile == nil || t.RgdFile.Root == nil || e == nil {
		return nil
	}
	values := make(map[string]bool)
	RgdCollectStringValues(t.RgdFile.Root, values)
	current := strings.TrimSpace(e.DisplayValue())
	delete(values, current)
	prefix := strings.ToLower(current)
	out := make([]string, 0, len(values))
	for value := range values {
		lower := strings.ToLower(value)
		if prefix == "" || strings.HasPrefix(lower, prefix) || strings.Contains(lower, prefix) {
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func RgdVisibleSuggestions(t *uistate.Tab, e *rgd.RgdEntry) []string {
	if t == nil || e == nil || (e.Type != rgd.RgdTypeString && e.Type != rgd.RgdTypeWString) {
		return nil
	}
	raw := RgdSuggestionsFor(t, e)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if rgd.IsNumericAlias(value) {
			if rgd.AliasDisplayName(value, rgd.InitDict()) == "" {
				continue
			}
		}
		out = append(out, value)
	}
	return out
}

func RgdSuggestionLabel(value string) string {
	if name := rgd.AliasDisplayName(value, rgd.InitDict()); name != "" {
		return name
	}
	return value
}

func RgdEnsureBoolClick(ui *uistate.State, t *uistate.Tab, key string) *widget.Clickable {
	if t.RgdValClicks == nil {
		t.RgdValClicks = make(map[string]*widget.Clickable)
	}
	if click, ok := t.RgdValClicks[key]; ok {
		return click
	}
	click := &widget.Clickable{}
	t.RgdValClicks[key] = click
	return click
}

func RgdIsEditableType(typ uint32) bool {
	switch typ {
	case rgd.RgdTypeFloat, rgd.RgdTypeInt, rgd.RgdTypeBool, rgd.RgdTypeString, rgd.RgdTypeWString:
		return true
	default:
		return false
	}
}

func RgdApplyScalarText(e *rgd.RgdEntry, text string) (bool, error) {
	if e == nil || !RgdIsEditableType(e.Type) {
		return false, fmt.Errorf("field type is not editable")
	}
	value := strings.TrimSpace(text)
	old := e.DisplayValue()
	switch e.Type {
	case rgd.RgdTypeFloat:
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return false, fmt.Errorf("invalid float")
		}
		e.FloatVal = float32(v)
	case rgd.RgdTypeInt:
		v, err := strconv.ParseUint(value, 0, 32)
		if err != nil {
			return false, fmt.Errorf("invalid integer")
		}
		e.IntVal = uint32(v)
	case rgd.RgdTypeBool:
		switch strings.ToLower(value) {
		case "true", "1", "yes", "on":
			e.BoolVal = true
		case "false", "0", "no", "off":
			e.BoolVal = false
		default:
			return false, fmt.Errorf("invalid boolean (use true/false)")
		}
	case rgd.RgdTypeString:
		e.StrVal = text
	case rgd.RgdTypeWString:
		e.WStrVal = text
	}
	return e.DisplayValue() != old, nil
}

func RgdApplyReferenceText(e *rgd.RgdEntry, text string) bool {
	if e == nil {
		return false
	}
	old := e.RefString()
	if strings.TrimSpace(text) == "" {
		e.Ref = nil
		return old != ""
	}
	if e.Ref == nil {
		e.Ref = &rgd.RgdEntry{Hash: rgd.RefHash, Name: "$REF", Type: rgd.RgdTypeString, StrVal: text}
	} else {
		if e.Ref.Type == rgd.RgdTypeWString {
			e.Ref.WStrVal = text
		} else {
			e.Ref.Type = rgd.RgdTypeString
			e.Ref.StrVal = text
			e.Ref.WStrVal = ""
		}
		e.Ref.Hash = rgd.RefHash
		if e.Ref.Name == "" {
			e.Ref.Name = "$REF"
		}
	}
	return old != text
}

func RgdApplyEditorText(ui *uistate.State, t *uistate.Tab, e *rgd.RgdEntry, key, text string) {
	if t == nil || e == nil || !RgdIsEditableType(e.Type) {
		return
	}
	changed, err := RgdApplyScalarText(e, text)
	if err != nil {
		if t.RgdValErrors == nil {
			t.RgdValErrors = make(map[string]string)
		}
		t.RgdValErrors[key] = err.Error()
		return
	}
	if t.RgdValErrors != nil {
		delete(t.RgdValErrors, key)
	}
	if changed {
		t.RgdDirty = true
	}
}

func RgdFlushPending(ui *uistate.State, t *uistate.Tab) {
	if t == nil || t.RgdFile == nil {
		return
	}
	eSel := RgdSelectedEntryFor(ui, t)
	if eSel != nil && eSel.IsTable() {
		cur := eSel.RefString()
		newText := t.RgdRefEditor.Text()
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
	if eSel != nil && RgdIsEditableType(eSel.Type) {
		if ed, ok := t.RgdValEditors[t.RgdSelected]; ok {
			RgdApplyEditorText(ui, t, eSel, t.RgdSelected, ed.Text())
		}
	}
	if eSel != nil && eSel.IsTable() {
		for _, ch := range eSel.Children {
			key := t.RgdSelected + "/" + ch.Name
			if ch.IsTable() {
				if ed, ok := t.RgdValEditors[key+"/$ref"]; ok && RgdApplyReferenceText(ch, ed.Text()) {
					t.RgdDirty = true
				}
				continue
			}
			if !RgdIsEditableType(ch.Type) {
				continue
			}
			if ed, ok := t.RgdValEditors[key]; ok {
				RgdApplyEditorText(ui, t, ch, key, ed.Text())
			}
		}
	}
}

func HandleRgdSave(ui *uistate.State, t *uistate.Tab) {
	if t == nil || t.Kind != "rgd" || t.RgdFile == nil {
		ui.StatusText = "Nothing to save"
		return
	}
	// flush any pending editor text that hasn't been applied via Update yet
	RgdFlushPending(ui, t)
	if len(t.RgdValErrors) > 0 {
		ui.StatusText = "Save failed: correct invalid RGD values first"
		return
	}
	if !t.RgdDirty {
		ui.StatusText = fmt.Sprintf("%s — no changes", filepath.Base(t.Path))
		return
	}
	data, err := t.RgdFile.MarshalBinary()
	if err != nil {
		ui.StatusText = fmt.Sprintf("Save failed: %v", err)
		log.Printf("marshal %q: %v", t.Path, err)
		return
	}
	if ui.ModTree == nil {
		ui.StatusText = "Save failed: no mod loaded"
		return
	}
	diskPath, err := ui.ModTree.WriteOverlayFile(t.Path, data)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Save failed: %v", err)
		log.Printf("write overlay %q: %v", t.Path, err)
		return
	}
	t.RgdDirty = false
	ui.StatusText = fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(t.Path), diskPath, len(data))
	log.Printf("saved rgd %q -> %q (%d bytes)", t.Path, diskPath, len(data))
}

// localeRowDisplay is what the row under a DT_WString value says, and the tip
// that goes with it. There are three cases and each is worth naming: the id
// resolves, the id is the game's empty marker, or the chosen language has no
// entry for it — and the last one has to say which language, or a blank row
// reads like a bug rather than a translation gap.
//
// It is kept apart from the layout so the wording can be pinned without
// laying a frame out.
func LocaleRowDisplay(ui *uistate.State, value string) (text, tooltip string) {
	id, ok := rgd.LocID(value)
	if !ok {
		return "", ""
	}
	// The tooltip is the one place the id is shown, so the row itself can stay
	// readable: it leads with the text and never with "$705754".
	lines := []string{"$" + id}
	if id == "0" {
		// The game's "no string" marker, on every unused text_NN slot.
		lines = append(lines, "The game writes $0 for an unused localisation slot",
			"", "Click to give it a string of its own")
		return "— (no localisation — click to add one)", strings.Join(lines, "\n")
	}
	if resolved := LocalisedString(id); resolved != "" {
		entry, _ := LocaleLookup(id)
		lines = append(lines, filepath.Base(entry.File), fmt.Sprintf("%s line %d", entry.File, entry.Line))
		if LocaleOverrideIsOurs(entry) {
			lines = append(lines, "from this mod's own file")
		}
		lines = append(lines, "", "Alt+click to open it there", "Alt+Ctrl+click to change the text")
		return resolved, strings.Join(lines, "\n")
	}
	lines = append(lines,
		fmt.Sprintf("No entry for $%s in the %s .ucs files", id, LocaleLabel(ui)),
		"", "Click to add one")
	return fmt.Sprintf("not in the %s string table", LocaleLabel(ui)), strings.Join(lines, "\n")
}

// tableRowLocaleText is the text a DT_WString resolves to in the chosen
// language, shown under the value itself. The value keeps its "$90100" form,
// because that is what the file says and what a write back needs; this row is
// what the id means.
//
// Alt+click on the text opens the .ucs it came from with the caret on the
// line, which is why the press is caught here rather than on a clickable: only
// the raw pointer event carries the modifier.
func TableRowLocaleText(ui *uistate.State, gtx layout.Context, t *uistate.Tab, e *rgd.RgdEntry, key string, alt bool) layout.Dimensions {
	if e == nil || e.Type != rgd.RgdTypeWString {
		return layout.Dimensions{}
	}
	text, tooltip := LocaleRowDisplay(ui, e.DisplayValue())
	if text == "" {
		// Not a localisation id at all, so there is no row to show.
		return layout.Dimensions{}
	}
	if len(text) > 96 {
		text = text[:93] + "…"
	}
	row := uikit.TableRow2WithTooltip(ui, gtx, "Text", text, alt, tooltip)
	// A row with no text is still worth a target: a click on it is how a field
	// with no localisation gets one.
	tag := RgdEnsureLocTag(ui, t, key)
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return row }),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// The pointer cursor is the only cue that the row does something,
			// and it costs no per-frame state to keep.
			pointer.CursorPointer.Add(gtx.Ops)
			PollLocaleLinkPress(ui, gtx, tag, e.DisplayValue(), e, key)
			return layout.Dimensions{}
		}),
	)
}

// rgdEnsureLocTag is the pointer target of one Alt+click row. The target is
// kept per tab because the same entry can be drawn in more than one tab, and
// Gio routes a press to the area that registered it.
func RgdEnsureLocTag(ui *uistate.State, t *uistate.Tab, key string) event.Tag {
	if t == nil {
		return nil
	}
	if t.RgdLocTags == nil {
		t.RgdLocTags = make(map[string]event.Tag)
	}
	tag, ok := t.RgdLocTags[key]
	if !ok {
		tag = uikit.NewPointerTag(key)
		t.RgdLocTags[key] = tag
	}
	return tag
}

// pollLocaleLinkPress notes a press on a localisation row. Alt asks for the
// .ucs, Alt+Ctrl for the editor, and a plain click asks for the editor too when
// there is no text to select. Opening a tab or a dialog is the input pass's
// job, so the request is left on the state for the next frame instead of acted
// on here.
func PollLocaleLinkPress(ui *uistate.State, gtx layout.Context, tag event.Tag, value string, e *rgd.RgdEntry, fieldKey string) {
	if tag == nil {
		return
	}
	id, ok := rgd.LocID(value)
	if !ok {
		return
	}
	for {
		raw, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		ev, ok := raw.(pointer.Event)
		if !ok || ev.Kind != pointer.Press {
			continue
		}
		action := LocaleRowActionFor(LocalisedString(id) != "", ev.Modifiers.Contain(key.ModAlt), ev.Modifiers.Contain(key.ModCtrl))
		switch action {
		case LocaleRowNothing:
			continue
		case LocaleRowJump:
			ui.LocalePending = id
		case LocaleRowEdit, LocaleRowCreate:
			ui.LocalePendingRequest = LocaleEditRequest{Id: id, Action: action}
			ui.LocalePendingField = e
			ui.LocalePendingFieldKey = fieldKey
		}
	}
}

func TableRowEditor(ui *uistate.State, gtx layout.Context, label string, ed *widget.Editor, alt bool, validationErr string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := uikit.CPanelBg
	if alt {
		bg = uikit.CSaveBg
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
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return uikit.VLine(gtx, uikit.CBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						// inner white field with border
						borderColor := uikit.CBorderLight
						if validationErr != "" {
							borderColor = uikit.Rgb(0xB00020)
						}
						b := widget.Border{Color: borderColor, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
						click := RgdEnsureEditorClick(ui, ed)
						return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									sz := gtx.Constraints.Max
									st := clip.Rect{Max: sz}.Push(gtx.Ops)
									paint.ColorOp{Color: uikit.CPanelBg}.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									st.Pop()
									return layout.Dimensions{Size: sz}
								}),
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										w := material.Editor(ui.Th, ed, "")
										w.TextSize = unit.Sp(11)
										w.HintColor = uikit.CTextDim
										return w.Layout(gtx)
									})
								}),
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									// The clickable covers the complete value cell. Its click is
									// consumed during input, while PassOp keeps the editor's own
									// text hit-testing intact.
									pass := pointer.PassOp{}.Push(gtx.Ops)
									dims := click.Click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Dimensions{Size: gtx.Constraints.Max}
									})
									pass.Pop()
									return dims
								}),
							)
						})
					})
				}),
			)
		}),
	)
}

func OpenRgd(ui *uistate.State, virtualPath string) {
	if ui.ModTree == nil {
		return
	}
	if idx := uikit.FindTabIndex(ui, virtualPath); idx >= 0 {
		ui.TabSel = idx + 1
		uikit.EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Opened %s", filepath.Base(virtualPath))
		return
	}
	data, err := ui.ModTree.ReadFile(virtualPath)
	if err != nil {
		t := &uistate.Tab{Path: virtualPath, Kind: "rgd", RgdError: err.Error(), RgdExpanded: map[string]bool{"GameData": true}, RgdSelected: "GameData"}
		ui.OpenTabs = append(ui.OpenTabs, t)
		ui.TabSel = len(ui.OpenTabs)
		uikit.EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t := &uistate.Tab{Path: virtualPath, Kind: "rgd", RgdError: err.Error(), RgdExpanded: map[string]bool{"GameData": true}, RgdSelected: "GameData"}
		ui.OpenTabs = append(ui.OpenTabs, t)
		ui.TabSel = len(ui.OpenTabs)
		uikit.EnsureTabClicks(ui)
		ui.StatusText = fmt.Sprintf("Failed to parse %s: %v", filepath.Base(virtualPath), err)
		return
	}
	t := &uistate.Tab{
		Path: virtualPath, Kind: "rgd",
		RgdFile: rf, RgdError: "",
		RgdExpanded:         map[string]bool{"GameData": true},
		RgdSelected:         "GameData",
		RgdValEditors:       make(map[string]*widget.Editor),
		RgdValErrors:        make(map[string]string),
		RgdValClicks:        make(map[string]*widget.Clickable),
		RgdSuggestionClicks: make(map[string][]widget.Clickable),
	}
	t.RgdRefEditor.SingleLine = true
	t.RgdRefEditor.Submit = true
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	uikit.EnsureTabClicks(ui)
	RebuildRgdMidNodesFor(ui, t)
	EnsureRgdMidClicks(ui, t, len(t.RgdMidNodes))
	ui.StatusText = fmt.Sprintf("Opened %s — %d top-level entries", filepath.Base(virtualPath), len(rf.Root.Children))
	log.Printf("openRgd %q: %d top entries", virtualPath, len(rf.Root.Children))
}

func EnsureRgdMidClicks(ui *uistate.State, t *uistate.Tab, n int) {
	if len(t.RgdMidClicks) < n {
		old := t.RgdMidClicks
		t.RgdMidClicks = make([]widget.Clickable, n)
		copy(t.RgdMidClicks, old)
	} else if len(t.RgdMidClicks) > n {
		t.RgdMidClicks = t.RgdMidClicks[:n]
	}
}

func RebuildRgdMidNodesFor(ui *uistate.State, t *uistate.Tab) {
	if t.RgdFile == nil || t.RgdFile.Root == nil {
		t.RgdMidNodes = nil
		return
	}
	if t.RgdExpanded == nil {
		t.RgdExpanded = map[string]bool{"GameData": true}
	}
	if t.RgdSelected == "" {
		t.RgdSelected = "GameData"
	}
	var out []uistate.RgdVisible
	root := t.RgdFile.Root
	out = append(out, uistate.RgdVisible{
		Name:     root.Name,
		Path:     "GameData",
		Depth:    0,
		IsTable:  root.IsTable(),
		HasKids:  len(root.Children) > 0,
		Expanded: t.RgdExpanded["GameData"],
		Selected: t.RgdSelected == "GameData",
		Type:     root.Type,
		Ref:      root.RefString(),
		Entry:    root,
	})
	if t.RgdExpanded["GameData"] {
		RgdFlattenChildrenFor(ui, t, root.Children, "GameData", 1, &out)
	}
	t.RgdMidNodes = out
}

func RgdFlattenChildrenFor(ui *uistate.State, t *uistate.Tab, children []*rgd.RgdEntry, basePath string, depth int, out *[]uistate.RgdVisible) {
	for _, ch := range children {
		path := basePath + "/" + ch.Name
		expanded := t.RgdExpanded[path]
		*out = append(*out, uistate.RgdVisible{
			Name:     ch.Name,
			Path:     path,
			Depth:    depth,
			IsTable:  ch.IsTable(),
			HasKids:  len(ch.Children) > 0,
			Expanded: expanded,
			Selected: t.RgdSelected == path,
			Type:     ch.Type,
			Ref:      ch.RefString(),
			Entry:    ch,
		})
		if ch.IsTable() && len(ch.Children) > 0 && expanded {
			RgdFlattenChildrenFor(ui, t, ch.Children, path, depth+1, out)
		}
	}
}

func RgdFindEntryFor(ui *uistate.State, t *uistate.Tab, path string) *rgd.RgdEntry {
	if t.RgdFile == nil || t.RgdFile.Root == nil {
		return nil
	}
	if path == "GameData" || path == "" {
		return t.RgdFile.Root
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] != "GameData" {
		return nil
	}
	cur := t.RgdFile.Root
	for _, seg := range parts[1:] {
		found := false
		for _, ch := range cur.Children {
			if ch.Name == seg || strings.EqualFold(ch.Name, seg) {
				cur = ch
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cur
}

func RgdSelectedEntryFor(ui *uistate.State, t *uistate.Tab) *rgd.RgdEntry {
	if t.RgdFile == nil {
		return nil
	}
	if e := RgdFindEntryFor(ui, t, t.RgdSelected); e != nil {
		return e
	}
	return t.RgdFile.Root
}

var ChildrenRows = [][]string{
	{"ability_ext", "ebpextensions\\ability_ext.lua"},
	{"addon_ext", "ebpextensions\\addon_ext.lua"},
	{"cost_ext", "ebpextensions\\cost_ext.lua"},
	{"deep_strike_ext", "ebpextensions\\deep_strike_ext.lua"},
	{"entity_blueprint_ext", "ebpextensions\\entity_blueprint_ext.lua"},
	{"event_manager_ext", "ebpextensions\\event_manager_ext.lua"},
	{"health_ext", "ebpextensions\\health_ext.lua"},
	{"hq_ext", "ebpextensions\\hq_ext.lua"},
	{"infiltration_ext", "ebpextensions\\infiltration_ext.lua"},
	{"modifier_apply_ext", "ebpextensions\\modifier_apply_ext.lua"},
	{"modifier_ext", "ebpextensions\\modifier_ext.lua"},
	{"offline_ext", "ebpextensions\\offline_ext.lua"},
	{"requirement_ext", "ebpextensions\\requirement_ext.lua"},
	{"research_ext", "ebpextensions\\research_ext.lua"},
	{"resource_ext", "ebpextensions\\resource_ext.lua"},
	{"sight_ext", "ebpextensions\\sight_ext.lua"},
	{"sim_entity_ext", "ebpextensions\\sim_entity_ext.lua"},
	{"spawner_ext", "ebpextensions\\spawner_ext.lua"},
	{"squad_hold_ext", "ebpextensions\\squad_hold_ext.lua"},
	{"structure_buildable_ext", "ebpextensions\\structure_buildable_ext.lua"},
	{"structure_ext", "ebpextensions\\structure_ext.lua"},
	{"type_ext", "ebpextensions\\type_ext.lua"},
	{"ui_ext", "ebpextensions\\ui_ext.lua"},
}
