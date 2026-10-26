package main

import (
	"dow-mod-studio/internal/locstrings"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/io/key"
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
func (ui *uiState) localeLabel() string {
	lang := strings.TrimSpace(ui.locale)
	if lang == "" {
		lang = localeDefault
	}
	return lang
}

// refreshLocales rediscovers the languages on disk and rebuilds the string
// table. It runs when a module is opened and when the game folder is typed
// into the settings, because both can bring a different set of .ucs files
// into reach.
func (ui *uiState) refreshLocales() {
	gamePath := strings.TrimSpace(ui.gamePath)
	moduleDir := localeModuleDir(ui.modulePath)
	langs := availableLocales(gamePath, moduleDir)

	// A language remembered from a previous session may not be installed any
	// more. Keeping it would leave the field naming a table that was never
	// built, so it falls back to the default rather than silently showing
	// nothing.
	if !containsFold(langs, ui.locale) {
		ui.locale = localeDefault
	}
	ui.localeList = langs
	ui.applyLocale()
}

// applyLocale rebuilds the string table for the chosen language and reopens the
// locale list's row count. The mod tree is deliberately left alone: switching
// the display language must not throw away the open tabs, and only the .ucs
// lookup depends on it.
func (ui *uiState) applyLocale() {
	lang := strings.TrimSpace(ui.locale)
	if lang == "" {
		lang = localeDefault
		ui.locale = lang
	}
	table := setLocale(ui.gamePath, ui.modulePath, lang)
	ui.ensureLocaleClicks(len(ui.localeList))
	if table.Files == 0 {
		ui.statusText = fmt.Sprintf("No %s .ucs files found under %s", lang, ui.localeSearchRoot())
		return
	}
	ui.statusText = fmt.Sprintf("Locale %s — %d entries from %d files", lang, table.Lines, table.Files)
}

// localeSearchRoot names the folder the language list was read from, for the
// status line when a language turns out to have no files.
func (ui *uiState) localeSearchRoot() string {
	roots := localeRoots(ui.gamePath, localeModuleDir(ui.modulePath))
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
func (ui *uiState) setLocaleLanguage(lang string) {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return
	}
	if strings.EqualFold(lang, ui.locale) {
		return
	}
	ui.locale = lang
	ui.applyLocale()
	ui.markStateDirty()
}

func (ui *uiState) ensureLocaleClicks(n int) {
	if len(ui.localeClicks) < n {
		old := ui.localeClicks
		clicks := make([]widget.Clickable, n)
		copy(clicks, old)
		ui.localeClicks = clicks
	} else if len(ui.localeClicks) > n {
		ui.localeClicks = ui.localeClicks[:n]
	}
}

// closeLocale puts the list away.
func (ui *uiState) closeLocale() {
	ui.localeOpen = false
}

// closeLocaleUnderOtherMenuBarPopups is the other half of the rule that opening
// the language list puts the menus and the search away: when one of them is the
// one that just opened, the list has to go instead, or it stays drawn on top of
// it. A click on the menu bar lands above the list's dismiss area, so nothing
// else would take it away.
func (ui *uiState) closeLocaleUnderOtherMenuBarPopups() {
	if ui.localeOpen && (ui.menuOpen >= 0 || ui.searchVisible) {
		ui.closeLocale()
	}
}

// ── input ──────────────────────────────────────────────────────────────────

// handleLocaleInput runs the language picker. The list is a plain clickable
// list rather than an editor driven one, so it needs no key handling beyond
// Escape.
func (ui *uiState) handleLocaleInput(gtx layout.Context) {
	if ui.localeTrigger.Clicked(gtx) {
		// All three hang off the menu bar, so only one of them is open.
		ui.closeSearch()
		ui.menuOpen = -1
		ui.localeOpen = !ui.localeOpen
		ui.ensureLocaleClicks(len(ui.localeList))
	}
	if !ui.localeOpen {
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
		ui.closeLocale()
		return
	}
	if ui.localeDismiss.Clicked(gtx) {
		ui.closeLocale()
		return
	}
	ui.ensureLocaleClicks(len(ui.localeList))
	for i := range ui.localeClicks {
		if i >= len(ui.localeList) {
			break
		}
		if ui.localeClicks[i].Clicked(gtx) {
			ui.setLocaleLanguage(ui.localeList[i])
			ui.closeLocale()
			return
		}
	}
}

// ── layout ─────────────────────────────────────────────────────────────────

// layoutLocaleSlot places the picker to the left of the object tools, sharing
// their margin so the three controls sit on one baseline.
func (ui *uiState) layoutLocaleSlot(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unit.Dp(unitPanelGapDp), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, ui.layoutLocaleField)
}

// layoutLocaleField is the closed field: the chosen language and a caret.
func (ui *uiState) layoutLocaleField(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(localeFieldHeightDp))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	label := ui.localeLabel()
	hover := ui.localeTrigger.Hovered() || ui.localeOpen
	bg := cContentBg
	if ui.localeOpen {
		bg = cMenuOpen
	} else if hover {
		bg = cSaveBg
	}
	return ui.localeTrigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// The content is recorded and replayed rather than laid out in place.
		// Gio paints in the order the ops were added, so a background filled
		// after the labels are drawn lands on top of them and the field comes
		// out as a solid block with no writing on it. The menus do it this way
		// for the same reason.
		macro := op.Record(gtx.Ops)
		inner := layout.Inset{Left: unit.Dp(localeFieldPadDp), Right: unit.Dp(localeFieldPadDp - 4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(11), "Locale")
					l.Color = cTextDim
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(11), label)
					l.Color = cText
					l.MaxLines = 1
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Label(ui.th, unit.Sp(9), "▾")
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
func (ui *uiState) layoutLocaleOverlay(gtx layout.Context) layout.Dimensions {
	if !ui.localeOpen {
		return layout.Dimensions{}
	}
	ui.ensureLocaleClicks(len(ui.localeList))
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
	rows := len(ui.localeList)
	if rows > maxRows {
		rows = maxRows
	}
	return layout.Stack{}.Layout(gtx,
		// The dismiss area starts below the menu bar, so the field itself
		// stays clickable while the list is open and can close it again.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			defer op.Offset(image.Pt(0, menuBarH)).Push(gtx.Ops).Pop()
			return ui.localeDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(listW, rows*rowH+2*(gtx.Dp(unit.Dp(localeListPadDp))+gtx.Dp(unit.Dp(localeListBorderDp))))
			xOff := gtx.Constraints.Max.X - listW - margin
			if xOff < 0 {
				xOff = 0
			}
			ui.localeDropBox = image.Rectangle{Min: image.Pt(xOff, menuBarH), Max: image.Pt(xOff+listW, menuBarH+sz.Y)}
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
							return ui.localeRow(gtx, i)
						}))
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
				})
			})
		}),
	)
}

func (ui *uiState) localeRow(gtx layout.Context, idx int) layout.Dimensions {
	if idx >= len(ui.localeList) {
		return layout.Dimensions{}
	}
	lang := ui.localeList[idx]
	click := &ui.localeClicks[idx]
	active := strings.EqualFold(lang, ui.localeLabel())
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
			l := material.Label(ui.th, unit.Sp(11), lang)
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
func (ui *uiState) jumpToLocaleEntry(id string) {
	entry, ok := localeLookup(id)
	if !ok {
		ui.statusText = fmt.Sprintf("No %s entry for $%s", ui.localeLabel(), id)
		return
	}
	ui.openLocaleFile(entry.File, entry.Line, id)
}

// openLocaleFile opens a game's .ucs in a text tab with the caret on line,
// selecting the id there.
//
// The tab is a plain "text" tab, but not a mod file: the .ucs tables live
// outside the mod tree and belong to the game, so the editor is read only and
// nothing is written back. Reading is the whole point of the jump, and
// rewriting a 3.4 MB table as UTF-8 by accident would be a nasty surprise.
func (ui *uiState) openLocaleFile(path string, line int, id string) {
	if idx := ui.findTabIndex(path); idx >= 0 {
		ui.tabSel = idx + 1
		ui.ensureTabClicks()
		ui.focusLocaleLine(ui.openTabs[idx], line, id)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		ui.statusText = fmt.Sprintf("Failed to read %s: %v", filepath.Base(path), err)
		return
	}
	text := trimBOM(decodeUCS(data))
	t := &fileTab{
		path:         path,
		kind:         "text",
		textOriginal: text,
		textBytes:    len(data),
		textLocale:   true,
	}
	t.textEditor.SingleLine = false
	t.textEditor.Submit = false
	t.textEditor.ReadOnly = true
	t.textEditor.SetText(text)
	ui.openTabs = append(ui.openTabs, t)
	ui.tabSel = len(ui.openTabs)
	ui.ensureTabClicks()
	ui.focusLocaleLine(t, line, id)
	ui.statusText = fmt.Sprintf("%s line %d — $%s", filepath.Base(path), line, id)
}

// focusLocaleLine selects the id on the given line and scrolls to it. SetCaret
// is used rather than SelectRange because it also asks the editor to bring the
// caret into view, which is the whole reason for jumping.
func (ui *uiState) focusLocaleLine(t *fileTab, line int, id string) {
	if t == nil || t.textEditor.Len() == 0 {
		return
	}
	start, end := localeLineRange(t.textEditor.Text(), line, id)
	t.textEditor.SetCaret(start, end)
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
