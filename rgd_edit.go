package main

import (
	"fmt"
	"image"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dow-mod-studio/internal/rgd"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ── helpers for RGD textual editing ────────────────────────────────────────
func (ui *uiState) rgdEnsureEditorClick(ed *widget.Editor) *rgdEditorClickState {
	if ui.rgdEditorClicks == nil {
		ui.rgdEditorClicks = make(map[*widget.Editor]*rgdEditorClickState)
	}
	state, ok := ui.rgdEditorClicks[ed]
	if !ok {
		state = &rgdEditorClickState{editor: ed}
		ui.rgdEditorClicks[ed] = state
	}
	return state
}

func (ui *uiState) focusClickedRgdEditor(gtx layout.Context) {
	for _, state := range ui.rgdEditorClicks {
		if state.click.Clicked(gtx) {
			gtx.Execute(key.FocusCmd{Tag: state.editor})
		}
	}
}

func (ui *uiState) rgdEnsureValEditor(t *fileTab, key, init string) *widget.Editor {
	if t.rgdValEditors == nil {
		t.rgdValEditors = make(map[string]*widget.Editor)
	}
	if ed, ok := t.rgdValEditors[key]; ok {
		return ed
	}
	ed := &widget.Editor{SingleLine: true, Submit: true}
	ed.SetText(init)
	t.rgdValEditors[key] = ed
	return ed
}

func (ui *uiState) rgdEnsureSuggestionClicks(t *fileTab, key string, n int) []widget.Clickable {
	if t.rgdSuggestionClicks == nil {
		t.rgdSuggestionClicks = make(map[string][]widget.Clickable)
	}
	clicks := t.rgdSuggestionClicks[key]
	if len(clicks) < n {
		clicks = append(clicks, make([]widget.Clickable, n-len(clicks))...)
	} else if len(clicks) > n {
		clicks = clicks[:n]
	}
	t.rgdSuggestionClicks[key] = clicks
	return clicks
}

func rgdCollectStringValues(e *rgd.RgdEntry, values map[string]bool) {
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
		rgdCollectStringValues(child, values)
	}
}

func rgdSuggestionsFor(t *fileTab, e *rgd.RgdEntry) []string {
	if t == nil || t.rgdFile == nil || t.rgdFile.Root == nil || e == nil {
		return nil
	}
	values := make(map[string]bool)
	rgdCollectStringValues(t.rgdFile.Root, values)
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

func rgdVisibleSuggestions(t *fileTab, e *rgd.RgdEntry) []string {
	if t == nil || e == nil || (e.Type != rgd.RgdTypeString && e.Type != rgd.RgdTypeWString) {
		return nil
	}
	raw := rgdSuggestionsFor(t, e)
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

func rgdSuggestionLabel(value string) string {
	if name := rgd.AliasDisplayName(value, rgd.InitDict()); name != "" {
		return name
	}
	return value
}

func (ui *uiState) rgdSyncStringSuggestions(t *fileTab, e *rgd.RgdEntry, key string, gtx layout.Context) {
	if t == nil || e == nil || (e.Type != rgd.RgdTypeString && e.Type != rgd.RgdTypeWString) {
		return
	}
	suggestions := rgdVisibleSuggestions(t, e)
	clicks := ui.rgdEnsureSuggestionClicks(t, key, len(suggestions))
	for i := range clicks {
		if clicks[i].Clicked(gtx) {
			ed := ui.rgdEnsureValEditor(t, key, e.DisplayValue())
			ed.SetText(suggestions[i])
			ui.rgdApplyEditorText(t, e, key, suggestions[i])
			return
		}
	}
}

func (ui *uiState) rgdEnsureBoolClick(t *fileTab, key string) *widget.Clickable {
	if t.rgdValClicks == nil {
		t.rgdValClicks = make(map[string]*widget.Clickable)
	}
	if click, ok := t.rgdValClicks[key]; ok {
		return click
	}
	click := &widget.Clickable{}
	t.rgdValClicks[key] = click
	return click
}

func rgdIsEditableType(typ uint32) bool {
	switch typ {
	case rgd.RgdTypeFloat, rgd.RgdTypeInt, rgd.RgdTypeBool, rgd.RgdTypeString, rgd.RgdTypeWString:
		return true
	default:
		return false
	}
}

func rgdApplyScalarText(e *rgd.RgdEntry, text string) (bool, error) {
	if e == nil || !rgdIsEditableType(e.Type) {
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

func rgdApplyReferenceText(e *rgd.RgdEntry, text string) bool {
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

func (ui *uiState) rgdApplyEditorText(t *fileTab, e *rgd.RgdEntry, key, text string) {
	if t == nil || e == nil || !rgdIsEditableType(e.Type) {
		return
	}
	changed, err := rgdApplyScalarText(e, text)
	if err != nil {
		if t.rgdValErrors == nil {
			t.rgdValErrors = make(map[string]string)
		}
		t.rgdValErrors[key] = err.Error()
		return
	}
	if t.rgdValErrors != nil {
		delete(t.rgdValErrors, key)
	}
	if changed {
		t.rgdDirty = true
	}
}

func (ui *uiState) rgdSyncEditor(t *fileTab, e *rgd.RgdEntry, key string, gtx layout.Context) {
	if t == nil || e == nil || e.Type == rgd.RgdTypeBool || !rgdIsEditableType(e.Type) {
		return
	}
	ed := ui.rgdEnsureValEditor(t, key, e.DisplayValue())
	for {
		if _, ok := ed.Update(gtx); !ok {
			break
		}
	}
	ui.rgdApplyEditorText(t, e, key, ed.Text())
}

func (ui *uiState) rgdFlushPending(t *fileTab) {
	if t == nil || t.rgdFile == nil {
		return
	}
	eSel := ui.rgdSelectedEntryFor(t)
	if eSel != nil && eSel.IsTable() {
		cur := eSel.RefString()
		newText := t.rgdRefEditor.Text()
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
			t.rgdDirty = true
		}
	}
	if eSel != nil && rgdIsEditableType(eSel.Type) {
		if ed, ok := t.rgdValEditors[t.rgdSelected]; ok {
			ui.rgdApplyEditorText(t, eSel, t.rgdSelected, ed.Text())
		}
	}
	if eSel != nil && eSel.IsTable() {
		for _, ch := range eSel.Children {
			key := t.rgdSelected + "/" + ch.Name
			if ch.IsTable() {
				if ed, ok := t.rgdValEditors[key+"/$ref"]; ok && rgdApplyReferenceText(ch, ed.Text()) {
					t.rgdDirty = true
				}
				continue
			}
			if !rgdIsEditableType(ch.Type) {
				continue
			}
			if ed, ok := t.rgdValEditors[key]; ok {
				ui.rgdApplyEditorText(t, ch, key, ed.Text())
			}
		}
	}
}

// rgdSyncAndApply processes editor Update events and applies live text changes to the RGD model.
// Must be called from handleInput with a valid gtx.
func (ui *uiState) rgdSyncAndApply(gtx layout.Context) {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" || t.rgdFile == nil {
		return
	}
	eSel := ui.rgdSelectedEntryFor(t)
	// ── Reference editor (only for tables) ──────────────────────────────────
	if eSel != nil && eSel.IsTable() {
		if t.rgdRefBound != t.rgdSelected {
			cur := eSel.RefString()
			t.rgdRefEditor.SetText(cur)
			t.rgdRefBound = t.rgdSelected
			t.rgdRefEditor.SingleLine = true
			t.rgdRefEditor.Submit = true
		}
		for {
			ev, ok := t.rgdRefEditor.Update(gtx)
			if !ok {
				break
			}
			if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
				// apply on Enter as well (live apply below also covers it)
			}
			_ = ev
		}
		newText := t.rgdRefEditor.Text()
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
			t.rgdDirty = true
		}
	}
	// ── Scalar value editors and Bool toggles ───────────────────────────────
	if eSel != nil && eSel.Type == rgd.RgdTypeBool {
		if ui.rgdEnsureBoolClick(t, t.rgdSelected).Clicked(gtx) {
			eSel.BoolVal = !eSel.BoolVal
			t.rgdDirty = true
		}
	} else if eSel != nil && (eSel.Type == rgd.RgdTypeString || eSel.Type == rgd.RgdTypeWString) {
		ui.rgdSyncStringSuggestions(t, eSel, t.rgdSelected, gtx)
		ui.rgdSyncEditor(t, eSel, t.rgdSelected, gtx)
	} else if eSel != nil && rgdIsEditableType(eSel.Type) {
		ui.rgdSyncEditor(t, eSel, t.rgdSelected, gtx)
	}
	if eSel != nil && eSel.IsTable() {
		for _, ch := range eSel.Children {
			key := t.rgdSelected + "/" + ch.Name
			if ch.Type == rgd.RgdTypeBool {
				if ui.rgdEnsureBoolClick(t, key).Clicked(gtx) {
					ch.BoolVal = !ch.BoolVal
					t.rgdDirty = true
				}
				continue
			}
			if ch.Type == rgd.RgdTypeString || ch.Type == rgd.RgdTypeWString {
				ui.rgdSyncStringSuggestions(t, ch, key, gtx)
				ui.rgdSyncEditor(t, ch, key, gtx)
				continue
			}
			if ch.IsTable() {
				refKey := key + "/$ref"
				refEditor := ui.rgdEnsureValEditor(t, refKey, ch.RefString())
				for {
					if _, ok := refEditor.Update(gtx); !ok {
						break
					}
				}
				if rgdApplyReferenceText(ch, refEditor.Text()) {
					t.rgdDirty = true
				}
				continue
			}
			ui.rgdSyncEditor(t, ch, key, gtx)
		}
	}
}

func (ui *uiState) handleRgdSave(t *fileTab) {
	if t == nil || t.kind != "rgd" || t.rgdFile == nil {
		ui.statusText = "Nothing to save"
		return
	}
	// flush any pending editor text that hasn't been applied via Update yet
	ui.rgdFlushPending(t)
	if len(t.rgdValErrors) > 0 {
		ui.statusText = "Save failed: correct invalid RGD values first"
		return
	}
	if !t.rgdDirty {
		ui.statusText = fmt.Sprintf("%s — no changes", filepath.Base(t.path))
		return
	}
	data, err := t.rgdFile.MarshalBinary()
	if err != nil {
		ui.statusText = fmt.Sprintf("Save failed: %v", err)
		log.Printf("marshal %q: %v", t.path, err)
		return
	}
	if ui.modTree == nil {
		ui.statusText = "Save failed: no mod loaded"
		return
	}
	diskPath, err := ui.modTree.WriteOverlayFile(t.path, data)
	if err != nil {
		ui.statusText = fmt.Sprintf("Save failed: %v", err)
		log.Printf("write overlay %q: %v", t.path, err)
		return
	}
	t.rgdDirty = false
	ui.statusText = fmt.Sprintf("Saved %s → %s (%d bytes)", filepath.Base(t.path), diskPath, len(data))
	log.Printf("saved rgd %q -> %q (%d bytes)", t.path, diskPath, len(data))
}

func (ui *uiState) tableRowBool(gtx layout.Context, label string, t *fileTab, e *rgd.RgdEntry, key string, alt bool) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := cPanelBg
	if alt {
		bg = cSaveBg
	}
	click := ui.rgdEnsureBoolClick(t, key)
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
						l := material.Label(ui.th, unit.Sp(11), label)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, cBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.toggleButton(gtx, click, value, e.BoolVal, cSelectBg)
					})
				}),
			)
		}),
	)
}

// ── row with inline editor ──────────────────────────────────────────────────

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

// localeRowDisplay is what the row under a DT_WString value says, and the tip
// that goes with it. There are three cases and each is worth naming: the id
// resolves, the id is the game's empty marker, or the chosen language has no
// entry for it — and the last one has to say which language, or a blank row
// reads like a bug rather than a translation gap.
//
// It is kept apart from the layout so the wording can be pinned without
// laying a frame out.
func (ui *uiState) localeRowDisplay(value string) (text, tooltip string) {
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
	if resolved := localisedString(id); resolved != "" {
		entry, _ := localeLookup(id)
		lines = append(lines, filepath.Base(entry.File), fmt.Sprintf("%s line %d", entry.File, entry.Line))
		if localeOverrideIsOurs(entry) {
			lines = append(lines, "from this mod's own file")
		}
		lines = append(lines, "", "Alt+click to open it there", "Alt+Ctrl+click to change the text")
		return resolved, strings.Join(lines, "\n")
	}
	lines = append(lines,
		fmt.Sprintf("No entry for $%s in the %s .ucs files", id, ui.localeLabel()),
		"", "Click to add one")
	return fmt.Sprintf("not in the %s string table", ui.localeLabel()), strings.Join(lines, "\n")
}

// tableRowLocaleText is the text a DT_WString resolves to in the chosen
// language, shown under the value itself. The value keeps its "$90100" form,
// because that is what the file says and what a write back needs; this row is
// what the id means.
//
// Alt+click on the text opens the .ucs it came from with the caret on the
// line, which is why the press is caught here rather than on a clickable: only
// the raw pointer event carries the modifier.
func (ui *uiState) tableRowLocaleText(gtx layout.Context, t *fileTab, e *rgd.RgdEntry, key string, alt bool) layout.Dimensions {
	if e == nil || e.Type != rgd.RgdTypeWString {
		return layout.Dimensions{}
	}
	text, tooltip := ui.localeRowDisplay(e.DisplayValue())
	if text == "" {
		// Not a localisation id at all, so there is no row to show.
		return layout.Dimensions{}
	}
	if len(text) > 96 {
		text = text[:93] + "…"
	}
	row := ui.tableRow2WithTooltip(gtx, "Text", text, alt, tooltip)
	// A row with no text is still worth a target: a click on it is how a field
	// with no localisation gets one.
	tag := ui.rgdEnsureLocTag(t, key)
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { return row }),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// The pointer cursor is the only cue that the row does something,
			// and it costs no per-frame state to keep.
			pointer.CursorPointer.Add(gtx.Ops)
			ui.pollLocaleLinkPress(gtx, tag, e.DisplayValue(), e, key)
			return layout.Dimensions{}
		}),
	)
}

// rgdEnsureLocTag is the pointer target of one Alt+click row. The target is
// kept per tab because the same entry can be drawn in more than one tab, and
// Gio routes a press to the area that registered it.
func (ui *uiState) rgdEnsureLocTag(t *fileTab, key string) event.Tag {
	if t == nil {
		return nil
	}
	if t.rgdLocTags == nil {
		t.rgdLocTags = make(map[string]event.Tag)
	}
	tag, ok := t.rgdLocTags[key]
	if !ok {
		tag = newPointerTag(key)
		t.rgdLocTags[key] = tag
	}
	return tag
}

// pollLocaleLinkPress notes a press on a localisation row. Alt asks for the
// .ucs, Alt+Ctrl for the editor, and a plain click asks for the editor too when
// there is no text to select. Opening a tab or a dialog is the input pass's
// job, so the request is left on the state for the next frame instead of acted
// on here.
func (ui *uiState) pollLocaleLinkPress(gtx layout.Context, tag event.Tag, value string, e *rgd.RgdEntry, fieldKey string) {
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
		action := localeRowActionFor(localisedString(id) != "", ev.Modifiers.Contain(key.ModAlt), ev.Modifiers.Contain(key.ModCtrl))
		switch action {
		case localeRowNothing:
			continue
		case localeRowJump:
			ui.localePending = id
		case localeRowEdit, localeRowCreate:
			ui.localePendingRequest = localeEditRequest{id: id, action: action}
			ui.localePendingField = e
			ui.localePendingFieldKey = fieldKey
		}
	}
}

func (ui *uiState) tableRowStringEditor(gtx layout.Context, label string, t *fileTab, e *rgd.RgdEntry, key string, alt bool) layout.Dimensions {
	// A DT_String numeric alias is a dictionary reference, not a readable value,
	// so it shows the resolved name while the original alias stays in the model.
	// A DT_WString is a localisation id, which is shown as it is.
	if display := e.DisplayFieldValue(); display != e.DisplayValue() && e.Type == rgd.RgdTypeString {
		return ui.tableRow2WithTooltip(gtx, label, display, alt, rgdTooltipForValue(e))
	}
	suggestions := rgdVisibleSuggestions(t, e)
	clicks := ui.rgdEnsureSuggestionClicks(t, key, len(suggestions))
	children := make([]layout.FlexChild, 0, len(suggestions)+1)
	children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		ed := ui.rgdEnsureValEditor(t, key, e.DisplayValue())
		return ui.tableRowEditor(gtx, label, ed, alt, t.rgdValErrors[key])
	}))
	for i, suggestion := range suggestions {
		i, suggestion := i, suggestion
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			text := rgdSuggestionLabel(suggestion)
			if len(text) > 58 {
				text = text[:55] + "…"
			}
			return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.smallButton(gtx, &clicks[i], text)
			})
		}))
	}
	// A DT_WString carries a localisation id, so the row that says what the id
	// means in the chosen language goes under the value. A DT_String is a
	// dictionary alias and has nothing to resolve against.
	if e.Type == rgd.RgdTypeWString {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRowLocaleText(gtx, t, e, key, alt)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// ── row with inline editor ──────────────────────────────────────────────────

func (ui *uiState) tableRowEditor(gtx layout.Context, label string, ed *widget.Editor, alt bool, validationErr string) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	bg := cPanelBg
	if alt {
		bg = cSaveBg
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
						l := material.Label(ui.th, unit.Sp(11), label)
						l.Color = cText
						return l.Layout(gtx)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return vLine(gtx, cBorderLight) }),
				layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(4), Right: unit.Dp(4), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						// inner white field with border
						borderColor := cBorderLight
						if validationErr != "" {
							borderColor = rgb(0xB00020)
						}
						b := widget.Border{Color: borderColor, Width: unit.Dp(1), CornerRadius: unit.Dp(2)}
						click := ui.rgdEnsureEditorClick(ed)
						return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Stack{}.Layout(gtx,
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									sz := gtx.Constraints.Max
									st := clip.Rect{Max: sz}.Push(gtx.Ops)
									paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									st.Pop()
									return layout.Dimensions{Size: sz}
								}),
								layout.Stacked(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										w := material.Editor(ui.th, ed, "")
										w.TextSize = unit.Sp(11)
										w.HintColor = cTextDim
										return w.Layout(gtx)
									})
								}),
								layout.Expanded(func(gtx layout.Context) layout.Dimensions {
									// The clickable covers the complete value cell. Its click is
									// consumed during input, while PassOp keeps the editor's own
									// text hit-testing intact.
									pass := pointer.PassOp{}.Push(gtx.Ops)
									dims := click.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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

// ─── merged from rgd_tree.go ───────────────────────────────────────────────

func (ui *uiState) openRgd(virtualPath string) {
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
	if err != nil {
		t := &fileTab{path: virtualPath, kind: "rgd", rgdError: err.Error(), rgdExpanded: map[string]bool{"GameData": true}, rgdSelected: "GameData"}
		ui.openTabs = append(ui.openTabs, t)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(virtualPath), err)
		return
	}
	rf, err := rgd.ParseRgd(data)
	if err != nil {
		t := &fileTab{path: virtualPath, kind: "rgd", rgdError: err.Error(), rgdExpanded: map[string]bool{"GameData": true}, rgdSelected: "GameData"}
		ui.openTabs = append(ui.openTabs, t)
		ui.tabSel = len(ui.openTabs)
		ui.ensureTabClicks()
		ui.statusText = fmt.Sprintf("Failed to parse %s: %v", filepath.Base(virtualPath), err)
		return
	}
	t := &fileTab{
		path: virtualPath, kind: "rgd",
		rgdFile: rf, rgdError: "",
		rgdExpanded:         map[string]bool{"GameData": true},
		rgdSelected:         "GameData",
		rgdValEditors:       make(map[string]*widget.Editor),
		rgdValErrors:        make(map[string]string),
		rgdValClicks:        make(map[string]*widget.Clickable),
		rgdSuggestionClicks: make(map[string][]widget.Clickable),
	}
	t.rgdRefEditor.SingleLine = true
	t.rgdRefEditor.Submit = true
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	ui.rebuildRgdMidNodesFor(t)
	ui.ensureRgdMidClicks(t, len(t.rgdMidNodes))
	ui.statusText = fmt.Sprintf("Opened %s — %d top-level entries", filepath.Base(virtualPath), len(rf.Root.Children))
	log.Printf("openRgd %q: %d top entries", virtualPath, len(rf.Root.Children))
}

func (ui *uiState) ensureRgdMidClicks(t *fileTab, n int) {
	if len(t.rgdMidClicks) < n {
		old := t.rgdMidClicks
		t.rgdMidClicks = make([]widget.Clickable, n)
		copy(t.rgdMidClicks, old)
	} else if len(t.rgdMidClicks) > n {
		t.rgdMidClicks = t.rgdMidClicks[:n]
	}
}

func (ui *uiState) rebuildRgdMidNodes() {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return
	}
	ui.rebuildRgdMidNodesFor(t)
}

func (ui *uiState) rebuildRgdMidNodesFor(t *fileTab) {
	if t.rgdFile == nil || t.rgdFile.Root == nil {
		t.rgdMidNodes = nil
		return
	}
	if t.rgdExpanded == nil {
		t.rgdExpanded = map[string]bool{"GameData": true}
	}
	if t.rgdSelected == "" {
		t.rgdSelected = "GameData"
	}
	var out []rgdVisible
	root := t.rgdFile.Root
	out = append(out, rgdVisible{
		Name:     root.Name,
		Path:     "GameData",
		Depth:    0,
		IsTable:  root.IsTable(),
		HasKids:  len(root.Children) > 0,
		Expanded: t.rgdExpanded["GameData"],
		Selected: t.rgdSelected == "GameData",
		Type:     root.Type,
		Ref:      root.RefString(),
		Entry:    root,
	})
	if t.rgdExpanded["GameData"] {
		ui.rgdFlattenChildrenFor(t, root.Children, "GameData", 1, &out)
	}
	t.rgdMidNodes = out
}

func (ui *uiState) rgdFlattenChildren(children []*rgd.RgdEntry, basePath string, depth int, out *[]rgdVisible) {
	t := ui.activeTab()
	if t == nil {
		return
	}
	ui.rgdFlattenChildrenFor(t, children, basePath, depth, out)
}

func (ui *uiState) rgdFlattenChildrenFor(t *fileTab, children []*rgd.RgdEntry, basePath string, depth int, out *[]rgdVisible) {
	for _, ch := range children {
		path := basePath + "/" + ch.Name
		expanded := t.rgdExpanded[path]
		*out = append(*out, rgdVisible{
			Name:     ch.Name,
			Path:     path,
			Depth:    depth,
			IsTable:  ch.IsTable(),
			HasKids:  len(ch.Children) > 0,
			Expanded: expanded,
			Selected: t.rgdSelected == path,
			Type:     ch.Type,
			Ref:      ch.RefString(),
			Entry:    ch,
		})
		if ch.IsTable() && len(ch.Children) > 0 && expanded {
			ui.rgdFlattenChildrenFor(t, ch.Children, path, depth+1, out)
		}
	}
}

func (ui *uiState) rgdFindEntry(path string) *rgd.RgdEntry {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return nil
	}
	return ui.rgdFindEntryFor(t, path)
}

func (ui *uiState) rgdFindEntryFor(t *fileTab, path string) *rgd.RgdEntry {
	if t.rgdFile == nil || t.rgdFile.Root == nil {
		return nil
	}
	if path == "GameData" || path == "" {
		return t.rgdFile.Root
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] != "GameData" {
		return nil
	}
	cur := t.rgdFile.Root
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

func (ui *uiState) rgdSelectedEntry() *rgd.RgdEntry {
	t := ui.activeTab()
	if t == nil || t.kind != "rgd" {
		return nil
	}
	return ui.rgdSelectedEntryFor(t)
}

func (ui *uiState) rgdSelectedEntryFor(t *fileTab) *rgd.RgdEntry {
	if t.rgdFile == nil {
		return nil
	}
	if e := ui.rgdFindEntryFor(t, t.rgdSelected); e != nil {
		return e
	}
	return t.rgdFile.Root
}

// ─── merged from rgd_view.go ───────────────────────────────────────────────

func (ui *uiState) propertiesTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	e := ui.rgdSelectedEntryFor(t)
	var name, typeLabel string
	if e == nil {
		name = "—"
		typeLabel = "—"
	} else {
		if t.rgdSelected == "GameData" || t.rgdSelected == "" {
			name = "GameData"
		} else if idx := strings.LastIndex(t.rgdSelected, "/"); idx >= 0 {
			name = t.rgdSelected[idx+1:]
		} else {
			name = e.Name
		}
		typeLabel = e.TypeLabel()
	}
	isTable := e != nil && e.IsTable()
	editableScalar := e != nil && rgdIsEditableType(e.Type)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Name", name, false) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Data Type", typeLabel, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if isTable {
				return ui.tableRowEditor(gtx, "Reference", &t.rgdRefEditor, false, "")
			}
			return ui.tableRow2(gtx, "Reference", e.RefString(), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if e != nil && e.Type == rgd.RgdTypeBool {
				return ui.tableRowBool(gtx, "Value", t, e, t.rgdSelected, true)
			}
			if e != nil && (e.Type == rgd.RgdTypeString || e.Type == rgd.RgdTypeWString) {
				return ui.tableRowStringEditor(gtx, "Value", t, e, t.rgdSelected, true)
			}
			if editableScalar {
				ed := ui.rgdEnsureValEditor(t, t.rgdSelected, e.DisplayValue())
				return ui.tableRowEditor(gtx, "Value", ed, true, t.rgdValErrors[t.rgdSelected])
			}
			if e != nil && !isTable {
				return ui.tableRow2(gtx, "Value", e.DisplayFieldValue(), true)
			}
			return layout.Dimensions{}
		}),
	)
}

var childrenRows = [][]string{
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

func (ui *uiState) childrenTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.rgdFile == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), "Select a .rgd file, then a node in the middle tree")
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		})
	}
	e := ui.rgdSelectedEntryFor(t)
	if e == nil {
		e = t.rgdFile.Root
	}
	if !e.IsTable() {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			val := e.DisplayFieldValue()
			if val == "" {
				val = "—"
			}
			msg := fmt.Sprintf("%s (%s) = %s — no children", e.Name, e.TypeLabel(), val)
			l := material.Label(ui.th, unit.Sp(11), msg)
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	children := e.Children
	if len(children) == 0 {
		if ref := e.RefString(); ref != "" {
			return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(ui.th, unit.Sp(10), fmt.Sprintf("Empty table — Reference: %s", ref))
				l.Color = cTextDim
				return l.Layout(gtx)
			})
		}
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), "No children")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutListWithScrollbar(gtx, &t.rgdChildScroll, &t.rgdChildList, 28, len(children), func(gtx layout.Context, index int) layout.Dimensions {
				ch := children[index]
				alt := index%2 == 1
				key := t.rgdSelected + "/" + ch.Name
				if ch.IsTable() {
					refEditor := ui.rgdEnsureValEditor(t, key+"/$ref", ch.RefString())
					return ui.tableRowEditor(gtx, ch.Name, refEditor, alt, "")
				}
				if ch.Type == rgd.RgdTypeBool {
					return ui.tableRowBool(gtx, ch.Name, t, ch, key, alt)
				}
				if ch.Type == rgd.RgdTypeString || ch.Type == rgd.RgdTypeWString {
					return ui.tableRowStringEditor(gtx, ch.Name, t, ch, key, alt)
				}
				if rgdIsEditableType(ch.Type) {
					ed := ui.rgdEnsureValEditor(t, key, ch.DisplayValue())
					return ui.tableRowEditor(gtx, ch.Name, ed, alt, t.rgdValErrors[key])
				}
				val := ch.DisplayFieldValue()
				if len(val) > 64 {
					val = val[:61] + "…"
				}
				return ui.tableRow2WithTooltip(gtx, ch.Name, val, alt, rgdTooltipForValue(ch))
			})
		}),
	)
}
