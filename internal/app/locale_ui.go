package app

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/locstrings"
	"dow-mod-studio/internal/rgd"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Geometry of the language picker in the menu bar. It sits to the left of the
// object tools, on the same baseline as the search field.
const (
	localeFieldHeightDp = 22
	localeFieldPadDp    = 10
	localeListWidthDp   = 180
	localeRowHeightDp   = 22
	localeListMarginDp  = 8
	// The panel's own frame: a border around a vertical inset. The panel's
	// height has to carry both as well as the rows, or the last row is clipped
	// by however much the frame takes.
	localeListPadDp    = 3
	localeListBorderDp = 1
)

// ── model ──────────────────────────────────────────────────────────────────

// localeLabel is what the field reads when no game folder is known yet, or
// when the chosen language has no .ucs files at all. The ids still show as
// ids, so the field is never wrong, only the text behind them is missing.
func LocaleLabel(ui *uiState) string {
	lang := strings.TrimSpace(ui.Locale)
	if lang == "" {
		lang = localeDefault
	}
	return lang
}

// refreshLocales rediscovers the languages on disk and rebuilds the string
// table. It runs when a module is opened and when the game folder is typed
// into the settings, because both can bring a different set of .ucs files
// into reach.
func RefreshLocales(ui *uiState) {
	gamePath := strings.TrimSpace(ui.GamePath)
	moduleDir := localeModuleDir(ui.ModulePath)
	langs := availableLocales(gamePath, moduleDir)

	// A language remembered from a previous session may not be installed any
	// more. Keeping it would leave the field naming a table that was never
	// built, so it falls back to the default rather than silently showing
	// nothing.
	if !containsFold(langs, ui.Locale) {
		ui.Locale = localeDefault
	}
	ui.LocaleList = langs
	ApplyLocale(ui)
}

// applyLocale rebuilds the string table for the chosen language and reopens the
// locale list's row count. The mod tree is deliberately left alone: switching
// the display language must not throw away the open tabs, and only the .ucs
// lookup depends on it.
func ApplyLocale(ui *uiState) {
	lang := strings.TrimSpace(ui.Locale)
	if lang == "" {
		lang = localeDefault
		ui.Locale = lang
	}
	table := setLocale(ui.GamePath, ui.ModulePath, lang)
	EnsureLocaleClicks(ui, len(ui.LocaleList))
	if table.Files == 0 {
		ui.StatusText = fmt.Sprintf("No %s .ucs files found under %s", lang, LocaleSearchRoot(ui))
		return
	}
	ui.StatusText = fmt.Sprintf("Locale %s — %d entries from %d files", lang, table.Lines, table.Files)
}

// localeSearchRoot names the folder the language list was read from, for the
// status line when a language turns out to have no files.
func LocaleSearchRoot(ui *uiState) string {
	roots := localeRoots(ui.GamePath, localeModuleDir(ui.ModulePath))
	if len(roots) == 0 {
		return "(no game folder)"
	}
	return filepath.Join(roots[0], "Locale")
}

func containsFold(list []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, item := range list {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
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

func EnsureLocaleClicks(ui *uiState, n int) {
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
func CloseLocale(ui *uiState) {
	ui.LocaleOpen = false
}

// closeLocaleUnderOtherMenuBarPopups is the other half of the rule that opening
// the language list puts the menus and the search away: when one of them is the
// one that just opened, the list has to go instead, or it stays drawn on top of
// it. A click on the menu bar lands above the list's dismiss area, so nothing
// else would take it away.
func CloseLocaleUnderOtherMenuBarPopups(ui *uiState) {
	if ui.LocaleOpen && (ui.MenuOpen >= 0 || ui.SearchVisible) {
		CloseLocale(ui)
	}
}

// ── input ──────────────────────────────────────────────────────────────────

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

// ── layout ─────────────────────────────────────────────────────────────────

// layoutLocaleSlot places the picker to the left of the object tools, sharing
// their margin so the three controls sit on one baseline.
func LayoutLocaleSlot(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unit.Dp(unitPanelGapDp), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return LayoutLocaleField(ui, gtx) })
}

// layoutLocaleField is the closed field: the chosen language and a caret.
func LayoutLocaleField(ui *uiState, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(localeFieldHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	label := LocaleLabel(ui)
	hover := ui.LocaleTrigger.Hovered() || ui.LocaleOpen
	bg := cContentBg
	if ui.LocaleOpen {
		bg = cMenuOpen
	} else if hover {
		bg = cSaveBg
	}
	return ui.LocaleTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// The content is recorded and replayed rather than laid out in place.
		// Gio paints in the order the ops were added, so a background filled
		// after the labels are drawn lands on top of them and the field comes
		// out as a solid block with no writing on it. The menus do it this way
		// for the same reason.
		macro := op.Record(gtx.Ops)
		inner := layout.Inset{Left: unit.Dp(localeFieldPadDp), Right: unit.Dp(localeFieldPadDp - 4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), "Locale")
					l.Color = cTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), label)
					l.Color = cText
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(9), "▾")
					l.Color = cTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
			)
		})
		call := macro.Stop()
		sz := inner.Size
		sz.Y = h
		fillRect(gtx, bg, sz)
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
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
			paint.ColorOp{Color: cPanelBg}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			st2.Pop()
			b := widget.Border{Color: cBorder, Width: unit.Dp(1)}
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

func LocaleRow(ui *uiState, gtx layout.Context, idx int) layout.Dimensions {
	if idx >= len(ui.LocaleList) {
		return layout.Dimensions{}
	}
	lang := ui.LocaleList[idx]
	click := &ui.LocaleClicks[idx]
	active := strings.EqualFold(lang, LocaleLabel(ui))
	bg := cPanelBg
	switch {
	case click.Hovered():
		bg = cMenuHover
	case active:
		bg = cSaveBg
	}
	h := gtx.Dp(unit.Dp(localeRowHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Recorded and replayed so the row's own colour goes down first and the
		// language name is painted over it. Laid out in place, the fill lands
		// last and the row is a flat rectangle with nothing written on it.
		macro := op.Record(gtx.Ops)
		layout.Inset{Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, unit.Sp(11), lang)
			l.Color = cText
			l.MaxLines = 1
			return l.Layout(gtx)
		})
		call := macro.Stop()
		sz := image.Pt(gtx.Constraints.Max.X, h)
		fillRect(gtx, bg, sz)
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
}

// ── jumping into a .ucs ────────────────────────────────────────────────────

// jumpToLocaleEntry is what an Alt+click on a resolved string comes to. The id
// has to be in the table, otherwise there is no line to go to and the status
// line says which language is missing it.
func JumpToLocaleEntry(ui *uiState, id string) {
	entry, ok := localeLookup(id)
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
func OpenLocaleFile(ui *uiState, path string, line int, id string) {
	if idx := FindTabIndex(ui, path); idx >= 0 {
		ui.TabSel = idx + 1
		EnsureTabClicks(ui)
		FocusLocaleLine(ui, ui.OpenTabs[idx], line, id)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		ui.StatusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(path), err)
		return
	}
	text := trimBOM(decodeUCS(data))
	t := &fileTab{
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
	EnsureTabClicks(ui)
	FocusLocaleLine(ui, t, line, id)
	ui.StatusText = fmt.Sprintf("%s line %d — $%s", filepath.Base(path), line, id)
}

// focusLocaleLine selects the id on the given line and scrolls to it. SetCaret
// is used rather than SelectRange because it also asks the editor to bring the
// caret into view, which is the whole reason for jumping.
func FocusLocaleLine(ui *uiState, t *fileTab, line int, id string) {
	if t == nil || t.TextEditor.Len() == 0 {
		return
	}
	start, end := localeLineRange(t.TextEditor.Text(), line, id)
	t.TextEditor.SetCaret(start, end)
}

// localeLineRange is the rune range of the id at the start of a line, for the
// editor's caret. Gio counts runes, and a .ucs is full of non-ASCII text, so
// the line is walked rune by rune rather than by byte. When the id is not
// where it is expected the whole line is selected, which is still a usable
// answer for a file that changed under the index.
func localeLineRange(text string, line int, id string) (int, int) {
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

// ── localisation strings (internal/locstrings) ───────────────────────────────
//
// Reading a module's .ucs tables, allocating an id a mod's own strings can
// use and writing them back is internal/locstrings' work; the language picker
// and the editors keep the names they call it by.

type (
	localeEntry = locstrings.LocaleEntry
	localeTable = locstrings.LocaleTable
)

const (
	localeDefault      = locstrings.LocaleDefault
	localeDirName      = locstrings.LocaleDirName
	localeEditFileName = locstrings.LocaleEditFileName
)

func setLocaleOverrideDir(dir string)          { locstrings.SetLocaleOverrideDir(dir) }
func invalidateLocaleCache()                   { locstrings.InvalidateLocaleCache() }
func decodeUCS(data []byte) string             { return locstrings.DecodeUCS(data) }
func trimBOM(s string) string                  { return locstrings.TrimBOM(s) }
func localeModuleDir(modulePath string) string { return locstrings.LocaleModuleDir(modulePath) }
func localeRoots(gamePath, moduleDir string) []string {
	return locstrings.LocaleRoots(gamePath, moduleDir)
}
func availableLocales(gamePath, moduleDir string) []string {
	return locstrings.AvailableLocales(gamePath, moduleDir)
}
func setLocale(gamePath, modulePath, lang string) *localeTable {
	return locstrings.SetLocale(gamePath, modulePath, lang)
}
func localeLookup(id string) (localeEntry, bool) { return locstrings.LocaleLookup(id) }
func localisedString(id string) string           { return locstrings.LocalisedString(id) }
func allocateLocaleID() string                   { return locstrings.AllocateLocaleID() }
func writeLocaleEntry(projectDir, lang, id, text string) (string, int, error) {
	return locstrings.WriteLocaleEntry(projectDir, lang, id, text)
}
func localeOverrideIsOurs(entry localeEntry) bool { return locstrings.LocaleOverrideIsOurs(entry) }

// ─── merged from locale_edit_ui.go ───────────────────────────────────────────────

// The dialog behind Alt+Ctrl+click on a localised row, and behind a plain click
// on one that has no text yet.
//
// A change never goes into the game's own .ucs. Those files are the game's data
// to overwrite, not ours: they live under Program Files, they are shared by
// every mod on the machine, and a default install will not let us write them at
// all. So a change is an override in the open mod's own Locale folder, which is
// read before the engine's — the first definition of an id wins, which is
// exactly how a modder's own table overrides a string.

// localeRowAction is what a press on a localised row means.
type localeRowAction int

const (
	// localeRowNothing is a press that is not asking for anything: a plain
	// click on a row that already has its text, which is a selection.
	localeRowNothing localeRowAction = iota
	// localeRowEdit is Alt+Ctrl on a row with text: change what it says.
	localeRowEdit
	// localeRowCreate is a press on a row with no text: write some, under an id
	// of its own.
	localeRowCreate
	// localeRowJump is Alt on a row with text: open the .ucs it came from with
	// the caret on the line. It is what Alt+click already did in the file view.
	localeRowJump
)

// localeFieldForID is the rgd.RgdEntry behind a localisation row, found by the id
// the row shows. It is what the dialog changes when it has to give a field a
// new id, and a nil result is not a failure: a row with no entry behind it — a
// weapon's settings, say — can still have its text written, it just cannot be
// repointed.
func localeFieldForID(d *unitDetails, id string) *rgd.RgdEntry {
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

// localeRowActionFor reads a press on a localised row.
//
// The rule is the one the object window asks for and it is a function of three
// booleans rather than something buried in a layout: a row with no text takes a
// plain click, because there is nothing to select and a string to write, and a
// row that has text takes Alt+Ctrl, so that a plain click on it stays a
// selection and the editing gesture is always deliberate.
func localeRowActionFor(hasText, alt, ctrl bool) localeRowAction {
	if !hasText {
		return localeRowCreate
	}
	switch {
	case alt && ctrl:
		return localeRowEdit
	case alt:
		return localeRowJump
	}
	return localeRowNothing
}

// localeEditRequest is what a press on a row asked for. It is passed by value
// because a press is noticed during layout and acted on in the input pass, one
// frame later, and a request that can be dropped in between would be a press
// that silently did nothing.
type localeEditRequest struct {
	id     string
	action localeRowAction
}

// ── the request ─────────────────────────────────────────────────────────────

// openLocaleEdit puts the dialog up for an id. create asks for a new id to be
// allocated on save, which is what a field with no string of its own needs; it
// is only meaningful when the field's id is the game's empty marker.
func OpenLocaleEdit(ui *uiState, id string, action localeRowAction, field *rgd.RgdEntry, key string) {
	if action != localeRowEdit && action != localeRowCreate {
		return
	}
	text := localisedString(id)
	if action == localeRowEdit && text == "" {
		// Asked to edit something that is not there, which is a create.
		action = localeRowCreate
	}
	ui.LocaleEditDialog = true
	ui.LocaleEditID = id
	ui.LocaleEditCreate = action == localeRowCreate
	ui.LocaleEditField = field
	ui.LocaleEditFieldKey = key
	ui.LocaleEditError = ""
	ui.LocaleEditEditor.SetText(text)
}

// closeLocaleEdit puts the dialog away.
func CloseLocaleEdit(ui *uiState) {
	ui.LocaleEditDialog = false
	ui.LocaleEditID = ""
	ui.LocaleEditCreate = false
	ui.LocaleEditField = nil
	ui.LocaleEditFieldKey = ""
	ui.LocaleEditError = ""
	ui.LocaleEditEditor.SetText("")
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

// ── the dialog ──────────────────────────────────────────────────────────────

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
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Stack{}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(4)))
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

// localeEditCaption says which string is being changed, and whether it is a
// new one. An empty id is what a create starts from, because the field holds
// the game's "$0" marker and there is no id to name yet.
func LocaleEditCaption(ui *uiState, gtx layout.Context) layout.Dimensions {
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
					l.Color = cTextDim
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.Th, unit.Sp(11), r.value)
					l.Color = cText
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
func LocaleEditNote(ui *uiState, gtx layout.Context) layout.Dimensions {
	where := ui.ProjectDir
	if strings.TrimSpace(where) == "" {
		return layout.Dimensions{}
	}
	lang := LocaleLabel(ui)
	note := fmt.Sprintf("Saved to the mod's own file: %s", filepath.Join(where, localeDirName, lang, localeEditFileName))
	if ui.LocaleEditCreate {
		note += "\nA free id will be allocated and written into the unit's field."
	}
	l := material.Label(ui.Th, unit.Sp(10), note)
	l.Color = cTextDim
	return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, l.Layout)
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

// ── the row ─────────────────────────────────────────────────────────────────

// localeEditTag is the pointer target of one localised row. The rows are drawn
// inside lists that scroll and are rebuilt every frame, so the target is keyed
// by the field it belongs to rather than held per widget.
func LocaleEditTag(ui *uiState, key string) event.Tag {
	if key == "" {
		return nil
	}
	if ui.LocaleEditTags == nil {
		ui.LocaleEditTags = make(map[string]event.Tag)
	}
	tag, ok := ui.LocaleEditTags[key]
	if !ok {
		tag = newPointerTag(key)
		ui.LocaleEditTags[key] = tag
	}
	return tag
}

// pollLocaleEditPress reads a press on a localised row and turns it into a
// request for the input pass. The press is caught here rather than on a
// clickable because only the raw event carries the modifiers, and the request
// is left on the state because opening a dialog is not a layout's business.
func PollLocaleEditPress(ui *uiState, gtx layout.Context, tag event.Tag, id string) {
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
		action := localeRowActionFor(localisedString(id) != "", e.Modifiers.Contain(key.ModAlt), e.Modifiers.Contain(key.ModCtrl))
		if action == localeRowNothing {
			continue
		}
		ui.LocalePendingRequest = localeEditRequest{id: id, action: action}
	}
}

// localeEditHover registers the hover target that puts the id in the tooltip.
// The tooltip is the shared one the file view already uses, so a localised row
// says the same thing wherever it is drawn.
func LocaleEditHover(ui *uiState, gtx layout.Context, tag event.Tag, tooltip string) {
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
func LocaleRowTooltip(ui *uiState, id string) string {
	if strings.TrimSpace(id) == "" || id == "0" {
		return "This field has no localisation id — click to give it one"
	}
	lines := []string{"$" + id}
	if entry, ok := localeLookup(id); ok {
		lines = append(lines, filepath.Base(entry.File)+fmt.Sprintf(" line %d", entry.Line))
		if localeOverrideIsOurs(entry) {
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
func LocaleRowText(ui *uiState, id string) string {
	if strings.TrimSpace(id) == "" || id == "0" {
		return "— (no localisation — click to add one)"
	}
	if text := localisedString(id); text != "" {
		return text
	}
	return fmt.Sprintf("not in the %s string table — click to add one", LocaleLabel(ui))
}

// localeEditableRow draws a localised field as a line of the object window: the
// text it resolves to, the id on hover, and the presses that edit it.
func LocaleEditableRow(ui *uiState, gtx layout.Context, label, id string, field *rgd.RgdEntry, key string) layout.Dimensions {
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

// localeRowBody is the visible part of a localised row: its label and what it
// says. It is apart from the pointer handling so the panel and the list can
// share the text and differ only in size.
func LocaleRowBody(ui *uiState, gtx layout.Context, label, id string, rowDp unit.Dp, size unit.Sp, maxText int) layout.Dimensions {
	text := LocaleRowText(ui, id)
	if maxText > 3 && len(text) > maxText {
		text = text[:maxText-3] + "…"
	}
	rowH := gtx.Dp(rowDp)
	gtx.Constraints.Min.Y = rowH
	gtx.Constraints.Max.Y = rowH
	hasText := localisedString(id) != ""
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, size, label)
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.Th, size, text)
			// A string is text; a missing one names something that is not
			// there, and reads as a reference rather than as a value.
			if hasText {
				l.Color = cText
			} else {
				l.Color = cTextDim
			}
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// localeRowAffordances puts the hover tooltip and the press handling on a
// localised row. Both live in one Expanded layer over the row's own area, which
// is what lets a press carry its modifiers: only a raw pointer event has them.
func LocaleRowAffordances(ui *uiState, gtx layout.Context, id, key string) {
	tag := LocaleEditTag(ui, key)
	if tag == nil {
		return
	}
	LocaleEditHover(ui, gtx, tag, LocaleRowTooltip(ui, id))
	PollLocaleEditPress(ui, gtx, tag, id)
}

// decodeUCSDocument splits a .ucs file's bytes into its lines.
func decodeUCSDocument(data []byte) []string { return locstrings.DecodeUCSDocument(data) }

// ── more locstrings entry points used by the tests ──────────────────────

func localeOverrideRoot() string                  { return locstrings.LocaleOverrideRoot() }
func decodeUTF16(b []byte, bigEndian bool) string { return locstrings.DecodeUTF16(b, bigEndian) }
func parseUCS(data []byte, file string, index map[string]localeEntry) int {
	return locstrings.ParseUCS(data, file, index)
}
func isLocaleID(id string) bool { return locstrings.IsLocaleID(id) }
func localeSourceFiles(gamePath, moduleDir, lang string) []string {
	return locstrings.LocaleSourceFiles(gamePath, moduleDir, lang)
}
func buildLocaleTable(gamePath, moduleDir, lang string) *localeTable {
	return locstrings.BuildLocaleTable(gamePath, moduleDir, lang)
}
func currentLocaleTable() *localeTable { return locstrings.CurrentLocaleTable() }
func localeLineNumber(id string) int   { return locstrings.LocaleLineNumber(id) }
func resetLocaleTables()               { locstrings.ResetLocaleTables() }
func localeEditPath(projectDir, lang string) (string, error) {
	return locstrings.LocaleEditPath(projectDir, lang)
}
func writeUCSDocument(lines []string) []byte { return locstrings.WriteUCSDocument(lines) }
func ucsSanitiseText(text string) string     { return locstrings.UcsSanitiseText(text) }
func ucsSetEntry(lines []string, id, text string) ([]string, int) {
	return locstrings.UcsSetEntry(lines, id, text)
}
func ucsDeleteEntry(lines []string, id string) ([]string, bool) {
	return locstrings.UcsDeleteEntry(lines, id)
}
func readUCSDocument(path string) ([]string, error) { return locstrings.ReadUCSDocument(path) }

// localeState is the live table the tests reset before each case.
var localeState = &locstrings.LocaleState
