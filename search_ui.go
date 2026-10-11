package main

import (
	"dow-mod-studio/internal/modtree"
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Geometry of the search field in the menu bar. The field and the results
// panel share the same right margin, so the panel lines up under the field it
// belongs to.
const (
	searchFieldWidthDp     = 240
	searchFieldHeightDp    = 22
	searchPanelWidthDp     = 420
	searchRowHeightDp      = 34
	searchPanelMarginDp    = 8
	searchPanelTopDp       = 26
	searchResultLimit      = 200
	searchPanelMaxHeightDp = 460
	searchPanelMinHeightDp = 140
)

// ── model ──────────────────────────────────────────────────────────────────

// searchSourceColor tints a result the way the tree tints its source, so a hit
// from the engine reads differently from one in the mod being edited.
func searchSourceColor(f *modtree.TreeFile) color.NRGBA {
	if f == nil || f.Source == nil {
		return cTextDim
	}
	switch f.Source.Kind {
	case modtree.KindThisMod:
		return cTreeThis
	case modtree.KindReqMod:
		return cTreeOther
	}
	return cTreeEngine
}

// searchFolderOf is the folder part of a virtual path, shown under the name.
func searchFolderOf(f *modtree.TreeFile) string {
	if f == nil {
		return ""
	}
	return strings.TrimSuffix(f.FullPath, f.Name)
}

// runSearch rebuilds the result list when the query, the filter, or the module
// has changed. Walking the merged tree is cheap but not free, and it must not
// run on frames where nothing was typed.
func (ui *uiState) runSearch(force bool) {
	q := strings.TrimSpace(ui.searchEditor.Text())
	kind := ui.searchKind
	if !force && ui.searchQuerySet && q == ui.lastSearchQuery && kind == ui.lastSearchKind {
		return
	}
	ui.lastSearchQuery = q
	ui.lastSearchKind = kind
	ui.searchQuerySet = true
	ui.searchSel = 0
	ui.searchScroll.offset = 0
	ui.searchHits = nil
	if q == "" || ui.modTree == nil {
		ui.ensureSearchClicks(0)
		ui.searchStatus = ""
		return
	}
	ui.searchHits = ui.modTree.Search(q, kind, searchResultLimit)
	ui.ensureSearchClicks(len(ui.searchHits))
	if len(ui.searchHits) == 0 {
		ui.searchStatus = fmt.Sprintf("No files match %q", q)
		return
	}
	if kind == modtree.SearchAll {
		ui.searchStatus = fmt.Sprintf("%d files", len(ui.searchHits))
	} else {
		ui.searchStatus = fmt.Sprintf("%d files (%s)", len(ui.searchHits), kind.Label())
	}
}

func (ui *uiState) ensureSearchClicks(n int) {
	if len(ui.searchHitClicks) < n {
		old := ui.searchHitClicks
		grown := make([]widget.Clickable, n)
		copy(grown, old)
		ui.searchHitClicks = grown
	}
	if len(ui.searchHover) < n {
		old := ui.searchHover
		grown := make([]bool, n)
		copy(grown, old)
		ui.searchHover = grown
	}
}

// openSearchResult activates a hit exactly as a double-click in the tree would,
// so a file opens in the same viewer no matter how it was reached.
func (ui *uiState) openSearchResult(i int) {
	if i < 0 || i >= len(ui.searchHits) {
		return
	}
	f := ui.searchHits[i].File
	if f == nil {
		return
	}
	// Reveal it in the tree first, so the left panel and the open tab agree.
	ui.revealInTree(f.FullPath)
	ui.openTreeFile(modtree.VisibleNode{
		Name:     f.Name,
		FullPath: f.FullPath,
		IsFile:   true,
		ModName:  f.Source.ModName,
		Kind:     f.Source.Kind,
	})
	// The query is kept so it can be refined, so the panel is held down for as
	// long as the field keeps that text.
	ui.closeSearch()
}

// revealInTree expands every folder on the way to a file and selects it, so a
// hit does not vanish from the tree the moment it is opened.
func (ui *uiState) revealInTree(fullPath string) {
	if ui.modTree == nil || fullPath == "" {
		return
	}
	ui.selectedPath = fullPath
	ui.selectedTreePath = fullPath
	if ui.expanded == nil {
		ui.expanded = map[string]bool{}
	}
	ui.expanded[ui.modTree.Root.FullPath] = true
	segs := strings.Split(strings.ReplaceAll(fullPath, "/", "\\"), "\\")
	prefix := ""
	// Stop one segment short: the last segment is the file itself.
	for _, seg := range segs[:maxInt(0, len(segs)-1)] {
		if prefix == "" {
			prefix = seg
		} else {
			prefix += "\\" + seg
		}
		ui.expanded[prefix] = true
	}
	ui.visibleNodes = ui.getVisibleNodes()
	ui.ensureRowClicks(len(ui.visibleNodes))
	ui.ensureRowHover(len(ui.visibleNodes))
}

// ── input ──────────────────────────────────────────────────────────────────

// handleSearchInput owns the search field's keyboard. It must run before
// handleKeyboard, otherwise the arrow keys move the file tree instead of the
// result list.
func (ui *uiState) handleSearchInput(gtx layout.Context) {
	// Ctrl+F opens the field from anywhere, including while a text file is
	// being edited. It is the one shortcut worth stealing for this.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.Name("F"), Required: key.ModShortcut})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		ui.searchVisible = true
		ui.searchHeldQuery = ""
		gtx.Execute(key.FocusCmd{Tag: &ui.searchEditor})
		break
	}
	// Clicking the field focuses it, which is all that is needed for typing to
	// work: the editor is part of the field, not of the panel below it.
	if ui.searchFieldClick.Clicked(gtx) {
		ui.searchVisible = true
		ui.searchHeldQuery = ""
		gtx.Execute(key.FocusCmd{Tag: &ui.searchEditor})
	}

	// Escape has to be taken before the editor's own Update, or the editor
	// would use it to drop a text selection instead of closing the search.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		if strings.TrimSpace(ui.searchEditor.Text()) != "" {
			ui.searchEditor.SetText("")
			ui.runSearch(true)
		} else {
			ui.closeSearch()
		}
		return
	}

	// The editor is laid out with the field on every frame, so updating it here
	// is what actually delivers the typed characters.
	submitted := false
	for {
		ev, ok := ui.searchEditor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			submitted = true
		case widget.ChangeEvent:
			// Deliberately does not lift the hold. The panel is reopened by
			// the query check below, which compares the text itself: a
			// ChangeEvent can arrive without the text having changed, and
			// letting one reopen the panel would put its dismiss catcher
			// back over the content and swallow every click on the file
			// tree.
		}
	}
	// Typing opens the panel, and emptying the field closes it again. A search
	// dismissed by a click outside stays closed for as long as its query is
	// kept, so the panel does not spring back on the next frame.
	if ui.searchPanelWanted(ui.searchEditor.Text()) {
		ui.searchVisible = true
	}
	if !ui.searchVisible {
		return
	}
	ui.runSearch(false)

	if ui.searchDismiss.Clicked(gtx) {
		ui.closeSearch()
		return
	}
	for i := range ui.searchKindClicks {
		if ui.searchKindClicks[i].Clicked(gtx) && i < len(modtree.SearchKinds) {
			ui.searchKind = modtree.SearchKinds[i]
			ui.runSearch(true)
		}
	}
	for i := range ui.searchHitClicks {
		if i < len(ui.searchHits) && ui.searchHitClicks[i].Clicked(gtx) {
			ui.openSearchResult(i)
			return
		}
	}
	if submitted {
		ui.openSearchResult(ui.searchSel)
		return
	}
	ui.handleSearchArrows(gtx)
}

// handleSearchArrows walks the result list with the arrow keys. Gio's editor
// consumes an arrow only when it can act on it, so whatever reaches us here is
// meant for the list.
func (ui *uiState) handleSearchArrows(gtx layout.Context) {
	if len(ui.searchHits) == 0 {
		return
	}
	rowH := gtx.Dp(unit.Dp(searchRowHeightDp))
	for _, name := range []key.Name{key.NameDownArrow, key.NameUpArrow} {
		for {
			ev, ok := gtx.Event(key.Filter{Name: name})
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			if name == key.NameDownArrow {
				ui.searchSel++
			} else {
				ui.searchSel--
			}
			ui.searchSel = clampInt(ui.searchSel, 0, len(ui.searchHits)-1)
			// Keep the selected row inside the viewport, mirroring what the
			// list's own scrolling would have done.
			top := ui.searchSel * rowH
			if top < ui.searchScroll.offset {
				ui.searchScroll.offset = top
			} else if top+rowH > ui.searchScroll.offset+ui.searchScroll.viewport {
				ui.searchScroll.offset = top + rowH - ui.searchScroll.viewport
			}
			ui.searchScroll.offset = clampInt(ui.searchScroll.offset, 0, maxInt(0, ui.searchScroll.contentHeight-ui.searchScroll.viewport))
		}
	}
}

// searchPanelWanted decides whether the results panel belongs on screen, from
// the field's text and the query the panel was last dismissed at. Typing
// brings it back; re-showing the same text does not.
//
// It is a function of the text rather than a flag an editor event can clear on
// the way past, and that is the whole point. While the panel is up it lays a
// dismiss catcher over the content, so a panel that springs back open by
// itself swallows every click on the file tree with no sign of why: files stop
// opening and folders stop expanding while the panel looks perfectly normal.
func (ui *uiState) searchPanelWanted(text string) bool {
	query := strings.TrimSpace(text)
	if query == "" {
		// Nothing to hold. An empty field has no query to spring back from,
		// and dropping the hold here is what lets the same text be typed
		// again after Escape emptied the field.
		ui.searchHeldQuery = ""
		return false
	}
	return query != ui.searchHeldQuery
}

// closeSearch puts the results panel away and holds it down for as long as the
// field keeps the query it was showing. Every path that hides the panel goes
// through here — the dismiss catcher, Escape, opening a result, a menu, the
// language picker — so none of them can leave it to spring back on the next
// frame and cover the content with its dismiss catcher.
func (ui *uiState) closeSearch() {
	ui.searchVisible = false
	ui.searchSel = 0
	ui.searchScroll.offset = 0
	ui.searchHeldQuery = strings.TrimSpace(ui.searchEditor.Text())
}

// ── layout ─────────────────────────────────────────────────────────────────

// layoutSearchSlot places the field at the right end of the menu bar.
func (ui *uiState) layoutSearchSlot(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Right: unit.Dp(searchPanelMarginDp), Top: unit.Dp(1), Bottom: unit.Dp(1)}.Layout(gtx, ui.layoutSearchField)
}

// layoutSearchField is the search field in the menu bar. The editor lives in
// the field itself rather than in the results panel, so it is laid out on
// every frame and can always take the keyboard focus — an editor that only
// exists while a panel is open cannot be focused the frame the panel opens.
func (ui *uiState) layoutSearchField(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Dp(unit.Dp(searchFieldWidthDp)), gtx.Dp(unit.Dp(searchFieldHeightDp)))
	gtx.Constraints.Min = size
	gtx.Constraints.Max = size
	hover := ui.searchFieldClick.Hovered() || ui.searchEditor.Focused(gtx)
	border := cBorder
	if hover {
		border = cSelectBd
	}
	return ui.searchFieldClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cContentBg, size, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: size}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(6), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(11), "Search")
							l.Color = cTextDim
							return l.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							ed := material.Editor(ui.th, &ui.searchEditor, "file name or folder")
							return ed.Layout(gtx)
						}),
					)
				})
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: border, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: size}
				})
			}),
		)
	})
}

// layoutSearchPanel draws the editor, the filter chips and the result list in a
// panel anchored under the menu bar's search field.
func (ui *uiState) layoutSearchPanel(gtx layout.Context) layout.Dimensions {
	w := gtx.Dp(unit.Dp(searchPanelWidthDp))
	margin := gtx.Dp(unit.Dp(searchPanelMarginDp))
	top := gtx.Dp(unit.Dp(searchPanelTopDp))
	maxH := gtx.Constraints.Max.Y - top - margin
	if maxH < gtx.Dp(unit.Dp(searchPanelMinHeightDp)) {
		maxH = gtx.Dp(unit.Dp(searchPanelMinHeightDp))
	}
	if maxH > gtx.Dp(unit.Dp(searchPanelMaxHeightDp)) {
		maxH = gtx.Dp(unit.Dp(searchPanelMaxHeightDp))
	}
	// Sit at the right edge of the window, in line with the field above.
	x := gtx.Constraints.Max.X - w - margin
	if x < 0 {
		x = 0
	}
	area := clip.Rect{Min: image.Pt(x, top), Max: image.Pt(x+w, top+maxH)}.Push(gtx.Ops)
	defer area.Pop()
	inner := gtx
	inner.Constraints.Min = image.Pt(w, maxH)
	inner.Constraints.Max = image.Pt(w, maxH)

	return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(inner, func(gtx layout.Context) layout.Dimensions {
		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				rrect(gtx, cPanelBg, gtx.Constraints.Max, gtx.Dp(unit.Dp(3)))
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(ui.layoutSearchFilters),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
						layout.Rigid(ui.layoutSearchStatus),
						layout.Flexed(1, ui.layoutSearchResults),
					)
				})
			}),
		)
	})
}

func (ui *uiState) layoutSearchFilters(gtx layout.Context) layout.Dimensions {
	var children []layout.FlexChild
	for i := range modtree.SearchKinds {
		i, k := i, modtree.SearchKinds[i]
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return ui.toggleButton(gtx, &ui.searchKindClicks[i], k.Label(), ui.searchKind == k, cSelectBg)
			})
		}))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

func (ui *uiState) layoutSearchStatus(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(ui.th, unit.Sp(10), ui.searchStatus)
		l.Color = cTextDim
		l.MaxLines = 1
		return l.Layout(gtx)
	})
}

func (ui *uiState) layoutSearchResults(gtx layout.Context) layout.Dimensions {
	if len(ui.searchHits) == 0 {
		return layout.Dimensions{}
	}
	ui.ensureSearchClicks(len(ui.searchHits))
	rowH := gtx.Dp(unit.Dp(searchRowHeightDp))
	ui.searchList.Axis = layout.Vertical
	return ui.layoutListWithScrollbar(gtx, &ui.searchScroll, &ui.searchList, rowH, len(ui.searchHits), func(gtx layout.Context, index int) layout.Dimensions {
		return ui.layoutSearchRow(gtx, index, ui.searchHits[index])
	})
}

// layoutSearchRow draws one hit: the file name with the matched word picked out,
// and the folder it lives in underneath.
func (ui *uiState) layoutSearchRow(gtx layout.Context, index int, hit modtree.SearchHit) layout.Dimensions {
	f := hit.File
	if f == nil || index >= len(ui.searchHitClicks) {
		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
	selected := index == ui.searchSel
	hover := index < len(ui.searchHover) && ui.searchHover[index]
	if selected || hover {
		bg := cMenuHover
		if selected {
			bg = cSelectBg
		}
		rect := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
		rrect(gtx, bg, gtx.Constraints.Max, gtx.Dp(unit.Dp(2)))
		rect.Pop()
	}
	return ui.searchHitClicks[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutSearchName(gtx, f.Name, hit)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutSearchPath(gtx, f)
				}),
			)
		})
	})
}

// layoutSearchName splits the file name around the matched word so the word
// stands out, which is why a row's first line is a flex of three labels.
func (ui *uiState) layoutSearchName(gtx layout.Context, name string, hit modtree.SearchHit) layout.Dimensions {
	word := hit.MatchWord
	at := -1
	if word != "" {
		if i := strings.Index(strings.ToLower(name), word); i >= 0 {
			at = i
		}
	}
	if at < 0 {
		return ui.searchNameLabel(gtx, name, cText)
	}
	before := name[:at]
	matched := name[at : at+len(word)]
	after := name[at+len(word):]
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.searchNameLabel(gtx, before, cText)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.searchNameLabel(gtx, matched, searchSourceColor(hit.File))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.searchNameLabel(gtx, after, cText)
		}),
	)
}

func (ui *uiState) searchNameLabel(gtx layout.Context, text string, col color.NRGBA) layout.Dimensions {
	l := material.Label(ui.th, unit.Sp(11), text)
	l.Color = col
	l.MaxLines = 1
	return l.Layout(gtx)
}

func (ui *uiState) layoutSearchPath(gtx layout.Context, f *modtree.TreeFile) layout.Dimensions {
	modName := ""
	if f.Source != nil {
		modName = f.Source.ModName
	}
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), searchFolderOf(f))
			l.Color = cTextDim
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(9), modName)
			l.Color = searchSourceColor(f)
			l.MaxLines = 1
			return l.Layout(gtx)
		}),
	)
}

// layoutSearchOverlay is the top-level layer: a click catcher so a click
// outside dismisses the search, with the panel stacked on top of it. The
// catcher starts below the menu bar, otherwise it would swallow clicks on the
// search field itself and the field could never be used again after a dismiss.
func (ui *uiState) layoutSearchOverlay(gtx layout.Context) layout.Dimensions {
	if !ui.searchVisible {
		return layout.Dimensions{}
	}
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			top := gtx.Dp(unit.Dp(searchPanelTopDp))
			below := clip.Rect{Min: image.Pt(0, top), Max: gtx.Constraints.Max}.Push(gtx.Ops)
			dims := ui.searchDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			below.Pop()
			return dims
		}),
		layout.Stacked(ui.layoutSearchPanel),
	)
}

// searchResultPaths lists the current hits as virtual paths, for tests.
func (ui *uiState) searchResultPaths() []string {
	out := make([]string, 0, len(ui.searchHits))
	for _, hit := range ui.searchHits {
		if hit.File != nil {
			out = append(out, hit.File.FullPath)
		}
	}
	return out
}
