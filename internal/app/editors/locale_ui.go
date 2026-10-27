package editors

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/app/uistate"
	appunit "dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/locstrings"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"os"
	"path/filepath"
	"strings"
)

// Geometry of the language picker in the menu bar. It sits to the left of the
// object tools, on the same baseline as the search field.
const (
	LocaleFieldHeightDp = 22
	LocaleFieldPadDp    = 10
	LocaleListWidthDp   = 180
	LocaleRowHeightDp   = 22
	LocaleListMarginDp  = 8
	// The panel's own frame: a border around a vertical inset. The panel's
	// height has to carry both as well as the rows, or the last row is clipped
	// by however much the frame takes.
	LocaleListPadDp    = 3
	LocaleListBorderDp = 1
)

// localeLabel is what the field reads when no game folder is known yet, or
// when the chosen language has no .ucs files at all. The ids still show as
// ids, so the field is never wrong, only the text behind them is missing.
func LocaleLabel(ui *uistate.State) string {
	lang := strings.TrimSpace(ui.Locale)
	if lang == "" {
		lang = LocaleDefault
	}
	return lang
}

// refreshLocales rediscovers the languages on disk and rebuilds the string
// table. It runs when a module is opened and when the game folder is typed
// into the settings, because both can bring a different set of .ucs files
// into reach.
func RefreshLocales(ui *uistate.State) {
	gamePath := strings.TrimSpace(ui.GamePath)
	moduleDir := LocaleModuleDir(ui.ModulePath)
	langs := AvailableLocales(gamePath, moduleDir)

	// A language remembered from a previous session may not be installed any
	// more. Keeping it would leave the field naming a table that was never
	// built, so it falls back to the default rather than silently showing
	// nothing.
	if !ContainsFold(langs, ui.Locale) {
		ui.Locale = LocaleDefault
	}
	ui.LocaleList = langs
	ApplyLocale(ui)
}

// applyLocale rebuilds the string table for the chosen language and reopens the
// locale list's row count. The mod tree is deliberately left alone: switching
// the display language must not throw away the open tabs, and only the .ucs
// lookup depends on it.
func ApplyLocale(ui *uistate.State) {
	lang := strings.TrimSpace(ui.Locale)
	if lang == "" {
		lang = LocaleDefault
		ui.Locale = lang
	}
	table := SetLocale(ui.GamePath, ui.ModulePath, lang)
	EnsureLocaleClicks(ui, len(ui.LocaleList))
	if table.Files == 0 {
		ui.StatusText = fmt.Sprintf("No %s .ucs files found under %s", lang, LocaleSearchRoot(ui))
		return
	}
	ui.StatusText = fmt.Sprintf("Locale %s — %d entries from %d files", lang, table.Lines, table.Files)
}

// localeSearchRoot names the folder the language list was read from, for the
// status line when a language turns out to have no files.
func LocaleSearchRoot(ui *uistate.State) string {
	roots := LocaleRoots(ui.GamePath, LocaleModuleDir(ui.ModulePath))
	if len(roots) == 0 {
		return "(no game folder)"
	}
	return filepath.Join(roots[0], "Locale")
}

func ContainsFold(list []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

func EnsureLocaleClicks(ui *uistate.State, n int) {
	if len(ui.LocaleClicks) < n {
		old := ui.LocaleClicks
		clicks := make([]widget.Clickable, n)
		copy(clicks, old)
		ui.LocaleClicks = clicks
	} else if len(ui.LocaleClicks) > n {
		ui.LocaleClicks = ui.LocaleClicks[:n]
	}
}

// closeLocale puts the list away.
func CloseLocale(ui *uistate.State) {
	ui.LocaleOpen = false
}

// closeLocaleUnderOtherMenuBarPopups is the other half of the rule that opening
// the language list puts the menus and the search away: when one of them is the
// one that just opened, the list has to go instead, or it stays drawn on top of
// it. A click on the menu bar lands above the list's dismiss area, so nothing
// else would take it away.
func CloseLocaleUnderOtherMenuBarPopups(ui *uistate.State) {
	if ui.LocaleOpen && (ui.MenuOpen >= 0 || ui.SearchVisible) {
		CloseLocale(ui)
	}
}

// layoutLocaleSlot places the picker to the left of the object tools, sharing
// their margin so the three controls sit on one baseline.
func LayoutLocaleSlot(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unit.Dp(appunit.UnitPanelGapDp), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutLocaleField(ui, gtx) })
}

// layoutLocaleField is the closed field: the chosen language and a caret.
func LayoutLocaleField(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(LocaleFieldHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	label := LocaleLabel(ui)
	hover := ui.LocaleTrigger.Hovered() || ui.LocaleOpen
	bg := uikit.CContentBg
	if ui.LocaleOpen {
		bg = uikit.CMenuOpen
	} else if hover {
		bg = uikit.CSaveBg
	}
	return ui.LocaleTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// The content is recorded and replayed rather than laid out in place.
		// Gio paints in the order the ops were added, so a background filled
		// after the labels are drawn lands on top of them and the field comes
		// out as a solid block with no writing on it. The menus do it this way
		// for the same reason.
		macro := op.Record(gtx.Ops)
		inner := layout.Inset{Left: unit.Dp(LocaleFieldPadDp), Right: unit.Dp(LocaleFieldPadDp - 4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), "Locale")
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), label)
					l.Color = uikit.CText
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), "▾")
					l.Color = uikit.CTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
			)
		})
		call := macro.Stop()
		sz := inner.Size
		sz.Y = h
		uikit.FillRect(gtx, bg, sz)
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
}

func LocaleRow(ui *uistate.State, gtx layout.Context, idx int) layout.Dimensions {
	if idx >= len(ui.LocaleList) {
		return layout.Dimensions{}
	}
	lang := ui.LocaleList[idx]
	click := &ui.LocaleClicks[idx]
	active := strings.EqualFold(lang, LocaleLabel(ui))
	bg := uikit.CPanelBg
	switch {
	case click.Hovered():
		bg = uikit.CMenuHover
	case active:
		bg = uikit.CSaveBg
	}
	h := gtx.Dp(unit.Dp(LocaleRowHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Recorded and replayed so the row's own colour goes down first and the
		// language name is painted over it. Laid out in place, the fill lands
		// last and the row is a flat rectangle with nothing written on it.
		macro := op.Record(gtx.Ops)
		layout.Inset{Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), lang)
			l.Color = uikit.CText
			l.MaxLines = 1
			return l.Layout(gtx)
		})
		call := macro.Stop()
		sz := image.Pt(gtx.Constraints.Max.X, h)
		uikit.FillRect(gtx, bg, sz)
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
}

// jumpToLocaleEntry is what an Alt+click on a resolved string comes to. The id
// has to be in the table, otherwise there is no line to go to and the status
// line says which language is missing it.
func JumpToLocaleEntry(ui *uistate.State, id string) {
	entry, ok := LocaleLookup(id)
	if !ok {
		ui.StatusText = fmt.Sprintf("No %s entry for $%s", LocaleLabel(ui), id)
		return
	}
	OpenLocaleFile(ui, entry.File, entry.Line, id)
}

// openLocaleFile opens a game's .ucs in a text tab with the caret on line,
// selecting the id there.
//
// The tab is a plain "text" tab, but not a mod file: the .ucs tables live
// outside the mod tree and belong to the game, so the editor is read only and
// nothing is written back. Reading is the whole point of the jump, and
// rewriting a 3.4 MB table as UTF-8 by accident would be a nasty surprise.
func OpenLocaleFile(ui *uistate.State, path string, line int, id string) {
	if idx := uikit.FindTabIndex(ui, path); idx >= 0 {
		ui.TabSel = idx + 1
		uikit.EnsureTabClicks(ui)
		FocusLocaleLine(ui, ui.OpenTabs[idx], line, id)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(path), err)
		return
	}
	text := TrimBOM(DecodeUCS(data))
	t := &uistate.Tab{
		Path:         path,
		Kind:         "text",
		TextOriginal: text,
		TextBytes:    len(data),
		TextLocale:   true,
	}
	t.TextEditor.SingleLine = false
	t.TextEditor.Submit = false
	t.TextEditor.ReadOnly = true
	t.TextEditor.SetText(text)
	ui.OpenTabs = append(ui.OpenTabs, t)
	ui.TabSel = len(ui.OpenTabs)
	uikit.EnsureTabClicks(ui)
	FocusLocaleLine(ui, t, line, id)
	ui.StatusText = fmt.Sprintf("%s line %d — $%s", filepath.Base(path), line, id)
}

// focusLocaleLine selects the id on the given line and scrolls to it. SetCaret
// is used rather than SelectRange because it also asks the editor to bring the
// caret into view, which is the whole reason for jumping.
func FocusLocaleLine(ui *uistate.State, t *uistate.Tab, line int, id string) {
	if t == nil || t.TextEditor.Len() == 0 {
		return
	}
	start, end := LocaleLineRange(t.TextEditor.Text(), line, id)
	t.TextEditor.SetCaret(start, end)
}

// LocaleLineRange is the rune range of the id at the start of a line, for the
// editor's caret. Gio counts runes, and a .ucs is full of non-ASCII text, so
// the line is walked rune by rune rather than by byte. When the id is not
// where it is expected the whole line is selected, which is still a usable
// answer for a file that changed under the index.
func LocaleLineRange(text string, line int, id string) (int, int) {
	if line < 1 || text == "" {
		return 0, 0
	}
	current := 1
	runes := 0
	for i, r := range text {
		if current == line {
			start := runes
			end := runes
			if id != "" && strings.HasPrefix(text[i:], id) {
				end = runes + len([]rune(id))
			} else {
				for j := i; j < len(text) && text[j] != '\n'; j++ {
					end++
				}
			}
			return start, end
		}
		if r == '\n' {
			current++
		}
		runes++
	}
	return 0, 0
}

type (
	LocaleEntry = locstrings.LocaleEntry
	LocaleTable = locstrings.LocaleTable
)

const (
	LocaleDefault      = locstrings.LocaleDefault
	LocaleDirName      = locstrings.LocaleDirName
	LocaleEditFileName = locstrings.LocaleEditFileName
)

func SetLocaleOverrideDir(dir string) { locstrings.SetLocaleOverrideDir(dir) }

func InvalidateLocaleCache() { locstrings.InvalidateLocaleCache() }

func DecodeUCS(data []byte) string { return locstrings.DecodeUCS(data) }

func TrimBOM(s string) string { return locstrings.TrimBOM(s) }

func LocaleModuleDir(modulePath string) string { return locstrings.LocaleModuleDir(modulePath) }

func LocaleRoots(gamePath, moduleDir string) []string {
	return locstrings.LocaleRoots(gamePath, moduleDir)
}

func AvailableLocales(gamePath, moduleDir string) []string {
	return locstrings.AvailableLocales(gamePath, moduleDir)
}

func SetLocale(gamePath, modulePath, lang string) *LocaleTable {
	return locstrings.SetLocale(gamePath, modulePath, lang)
}

func LocaleLookup(id string) (LocaleEntry, bool) { return locstrings.LocaleLookup(id) }

func LocalisedString(id string) string { return locstrings.LocalisedString(id) }

func AllocateLocaleID() string { return locstrings.AllocateLocaleID() }

func LocaleOverrideIsOurs(entry LocaleEntry) bool { return locstrings.LocaleOverrideIsOurs(entry) }

// LocaleRowAction is what a press on a localised row means.
type LocaleRowAction = uistate.LocaleRowAction

const (
	// LocaleRowNothing is a press that is not asking for anything: a plain
	// click on a row that already has its text, which is a selection.
	LocaleRowNothing LocaleRowAction = iota
	// LocaleRowEdit is Alt+Ctrl on a row with text: change what it says.
	LocaleRowEdit
	// LocaleRowCreate is a press on a row with no text: write some, under an id
	// of its own.
	LocaleRowCreate
	// LocaleRowJump is Alt on a row with text: open the .ucs it came from with
	// the caret on the line. It is what Alt+click already did in the file view.
	LocaleRowJump
)

// LocaleFieldForID is the rgd.RgdEntry behind a localisation row, found by the id
// the row shows. It is what the dialog changes when it has to give a field a
// new id, and a nil result is not a failure: a row with no entry behind it — a
// weapon's settings, say — can still have its text written, it just cannot be
// repointed.
func LocaleFieldForID(d *uistate.UnitDetails, id string) *rgd.RgdEntry {
	if d == nil || id == "" {
		return nil
	}
	for _, f := range d.Fields {
		if f.IsLocID && f.LocID == id && f.Entry != nil {
			return f.Entry
		}
	}
	return nil
}

// LocaleRowActionFor reads a press on a localised row.
//
// The rule is the one the object window asks for and it is a function of three
// booleans rather than something buried in a layout: a row with no text takes a
// plain click, because there is nothing to select and a string to write, and a
// row that has text takes Alt+Ctrl, so that a plain click on it stays a
// selection and the editing gesture is always deliberate.
func LocaleRowActionFor(hasText, alt, ctrl bool) LocaleRowAction {
	if !hasText {
		return LocaleRowCreate
	}
	switch {
	case alt && ctrl:
		return LocaleRowEdit
	case alt:
		return LocaleRowJump
	}
	return LocaleRowNothing
}

// LocaleEditRequest is what a press on a row asked for. It is passed by value
// because a press is noticed during layout and acted on in the input pass, one
// frame later, and a request that can be dropped in between would be a press
// that silently did nothing.
type LocaleEditRequest = uistate.LocaleEditRequest

// openLocaleEdit puts the dialog up for an id. create asks for a new id to be
// allocated on save, which is what a field with no string of its own needs; it
// is only meaningful when the field's id is the game's empty marker.
func OpenLocaleEdit(ui *uistate.State, id string, action LocaleRowAction, field *rgd.RgdEntry, key string) {
	if action != LocaleRowEdit && action != LocaleRowCreate {
		return
	}
	text := LocalisedString(id)
	if action == LocaleRowEdit && text == "" {
		// Asked to edit something that is not there, which is a create.
		action = LocaleRowCreate
	}
	ui.LocaleEditDialog = true
	ui.LocaleEditID = id
	ui.LocaleEditCreate = action == LocaleRowCreate
	ui.LocaleEditField = field
	ui.LocaleEditFieldKey = key
	ui.LocaleEditError = ""
	ui.LocaleEditEditor.SetText(text)
}

// closeLocaleEdit puts the dialog away.
func CloseLocaleEdit(ui *uistate.State) {
	ui.LocaleEditDialog = false
	ui.LocaleEditID = ""
	ui.LocaleEditCreate = false
	ui.LocaleEditField = nil
	ui.LocaleEditFieldKey = ""
	ui.LocaleEditError = ""
	ui.LocaleEditEditor.SetText("")
}

// localeEditCaption says which string is being changed, and whether it is a
// new one. An empty id is what a create starts from, because the field holds
// the game's "$0" marker and there is no id to name yet.
func LocaleEditCaption(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	title := "Localisation text"
	if ui.LocaleEditCreate {
		title = "New localisation"
	}
	rows := []struct{ label, value string }{
		{"String", title},
		{"Language", LocaleLabel(ui)},
	}
	if id := strings.TrimSpace(ui.LocaleEditID); id != "" && id != "0" {
		rows = append(rows, struct{ label, value string }{"Id", "$" + id})
	}
	children := make([]layout.FlexChild, 0, len(rows))
	for _, r := range rows {
		r := r
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), r.label)
					l.Color = uikit.CTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), r.value)
					l.Color = uikit.CText
					l.Font.Weight = font.Medium
					return l.Layout(gtx)
				}),
			)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// localeEditNote says where the text is going, and says it plainly: the mod's
// own folder, not the game's file. A translation that silently did not reach
// the game is worse than one that was never written.
func LocaleEditNote(ui *uistate.State, gtx layout.Context) layout.Dimensions {
	where := ui.ProjectDir
	if strings.TrimSpace(where) == "" {
		return layout.Dimensions{}
	}
	lang := LocaleLabel(ui)
	note := fmt.Sprintf("Saved to the mod's own file: %s", filepath.Join(where, LocaleDirName, lang, LocaleEditFileName))
	if ui.LocaleEditCreate {
		note += "\nA free id will be allocated and written into the unit's field."
	}
	l := material.Label(ui.Th, unit.Sp(10), note)
	l.Color = uikit.CTextDim
	return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, l.Layout)
}

// localeEditTag is the pointer target of one localised row. The rows are drawn
// inside lists that scroll and are rebuilt every frame, so the target is keyed
// by the field it belongs to rather than held per widget.
func LocaleEditTag(ui *uistate.State, key string) event.Tag {
	if key == "" {
		return nil
	}
	if ui.LocaleEditTags == nil {
		ui.LocaleEditTags = make(map[string]event.Tag)
	}
	tag, ok := ui.LocaleEditTags[key]
	if !ok {
		tag = uikit.NewPointerTag(key)
		ui.LocaleEditTags[key] = tag
	}
	return tag
}

// pollLocaleEditPress reads a press on a localised row and turns it into a
// request for the input pass. The press is caught here rather than on a
// clickable because only the raw event carries the modifiers, and the request
// is left on the state because opening a dialog is not a layout's business.
func PollLocaleEditPress(ui *uistate.State, gtx layout.Context, tag event.Tag, id string) {
	if tag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || e.Kind != pointer.Press {
			continue
		}
		action := LocaleRowActionFor(LocalisedString(id) != "", e.Modifiers.Contain(key.ModAlt), e.Modifiers.Contain(key.ModCtrl))
		if action == LocaleRowNothing {
			continue
		}
		ui.LocalePendingRequest = LocaleEditRequest{Id: id, Action: action}
	}
}

// localeEditHover registers the hover target that puts the id in the tooltip.
// The tooltip is the shared one the file view already uses, so a localised row
// says the same thing wherever it is drawn.
func LocaleEditHover(ui *uistate.State, gtx layout.Context, tag event.Tag, tooltip string) {
	if tag == nil || tooltip == "" {
		return
	}
	pointer.CursorPointer.Add(gtx.Ops)
	event.Op(gtx.Ops, tag)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Enter | pointer.Leave})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			ui.RgdTooltipText = tooltip
		case pointer.Leave:
			if ui.RgdTooltipText == tooltip {
				ui.RgdTooltipText = ""
			}
		}
	}
}

// localeRowTooltip is what a localised row says on hover: the id, which is the
// one thing the row itself cannot show, and where the text came from so a
// reader can tell the engine's string from the mod's override of it.
func LocaleRowTooltip(ui *uistate.State, id string) string {
	if strings.TrimSpace(id) == "" || id == "0" {
		return "This field has no localisation id — click to give it one"
	}
	lines := []string{"$" + id}
	if entry, ok := LocaleLookup(id); ok {
		lines = append(lines, filepath.Base(entry.File)+fmt.Sprintf(" line %d", entry.Line))
		if LocaleOverrideIsOurs(entry) {
			lines = append(lines, "from this mod's own file")
		}
	} else {
		lines = append(lines, fmt.Sprintf("not in the %s string table", LocaleLabel(ui)))
	}
	lines = append(lines, "", "Alt+Ctrl+click to change the text")
	return strings.Join(lines, "\n")
}

// localeRowText is what a localised row shows: the text, or why there is none.
// The id is deliberately not in it — the hover tooltip carries that, and a row
// that led with "$705754" would be unreadable.
func LocaleRowText(ui *uistate.State, id string) string {
	if strings.TrimSpace(id) == "" || id == "0" {
		return "— (no localisation — click to add one)"
	}
	if text := LocalisedString(id); text != "" {
		return text
	}
	return fmt.Sprintf("not in the %s string table — click to add one", LocaleLabel(ui))
}

// localeEditableRow draws a localised field as a line of the object window: the
// text it resolves to, the id on hover, and the presses that edit it.
func LocaleEditableRow(ui *uistate.State, gtx layout.Context, label, id string, field *rgd.RgdEntry, key string) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return LocaleRowBody(ui, gtx, label, id, 22, unit.Sp(10), 64)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			LocaleRowAffordances(ui, gtx, id, key)
			return layout.Dimensions{}
		}),
	)
}

// localeRowBody is the visible part of a localised row: its label and what it
// says. It is apart from the pointer handling so the panel and the list can
// share the text and differ only in size.
func LocaleRowBody(ui *uistate.State, gtx layout.Context, label, id string, rowDp unit.Dp, size unit.Sp, maxText int) layout.Dimensions {
	text := LocaleRowText(ui, id)
	if maxText > 3 && len(text) > maxText {
		text = text[:maxText-3] + "…"
	}
	rowH := gtx.Dp(rowDp)
	gtx.Constraints.Min.Y = rowH
	gtx.Constraints.Max.Y = rowH
	hasText := LocalisedString(id) != ""
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, size, label)
			l.Color = uikit.CTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, size, text)
			// A string is text; a missing one names something that is not
			// there, and reads as a reference rather than as a value.
			if hasText {
				l.Color = uikit.CText
			} else {
				l.Color = uikit.CTextDim
			}
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// localeRowAffordances puts the hover tooltip and the press handling on a
// localised row. Both live in one Expanded layer over the row's own area, which
// is what lets a press carry its modifiers: only a raw pointer event has them.
func LocaleRowAffordances(ui *uistate.State, gtx layout.Context, id, key string) {
	tag := LocaleEditTag(ui, key)
	if tag == nil {
		return
	}
	LocaleEditHover(ui, gtx, tag, LocaleRowTooltip(ui, id))
	PollLocaleEditPress(ui, gtx, tag, id)
}

// DecodeUCSDocument splits a .ucs file's bytes into its lines.
func DecodeUCSDocument(data []byte) []string { return locstrings.DecodeUCSDocument(data) }

func LocaleOverrideRoot() string { return locstrings.LocaleOverrideRoot() }

func DecodeUTF16(b []byte, bigEndian bool) string { return locstrings.DecodeUTF16(b, bigEndian) }

func ParseUCS(data []byte, file string, index map[string]LocaleEntry) int {
	return locstrings.ParseUCS(data, file, index)
}

func IsLocaleID(id string) bool { return locstrings.IsLocaleID(id) }

func LocaleSourceFiles(gamePath, moduleDir, lang string) []string {
	return locstrings.LocaleSourceFiles(gamePath, moduleDir, lang)
}

func BuildLocaleTable(gamePath, moduleDir, lang string) *LocaleTable {
	return locstrings.BuildLocaleTable(gamePath, moduleDir, lang)
}

func CurrentLocaleTable() *LocaleTable { return locstrings.CurrentLocaleTable() }

func LocaleLineNumber(id string) int { return locstrings.LocaleLineNumber(id) }

func ResetLocaleTables() { locstrings.ResetLocaleTables() }

func WriteUCSDocument(lines []string) []byte { return locstrings.WriteUCSDocument(lines) }

func UcsSanitiseText(text string) string { return locstrings.UcsSanitiseText(text) }

func UcsSetEntry(lines []string, id, text string) ([]string, int) {
	return locstrings.UcsSetEntry(lines, id, text)
}

func UcsDeleteEntry(lines []string, id string) ([]string, bool) {
	return locstrings.UcsDeleteEntry(lines, id)
}

func ReadUCSDocument(path string) ([]string, error) { return locstrings.ReadUCSDocument(path) }

// LocaleState is the live table the tests reset before each case.
var LocaleState = &locstrings.LocaleState
