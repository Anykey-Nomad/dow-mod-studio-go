package main

import (
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"image"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// wheelNotchDelta is the magnitude of one click of a mouse wheel, which is
// what a scroll amount is counted in. Windows reports WHEEL_DELTA (120) for
// each notch and Gio passes it through with the sign flipped, so a wheel event
// carrying ±120 is one click and anything smaller is a trackpad's finer report.
//
// It is named here because the amount is not a pixel count, and treating it as
// one is what makes a wheel that is meant to move a list by a few lines move it
// by five thousand instead.
const wheelNotchDelta = 120

func (ui *uiState) getVisibleNodes() []modtree.VisibleNode {
	if ui.modTree == nil {
		return nil
	}
	return ui.modTree.Flatten(ui.expanded, ui.selectedPath)
}

func (ui *uiState) ensureRowHover(n int) {
	if len(ui.rowHover) < n {
		old := ui.rowHover
		ui.rowHover = make([]bool, n)
		copy(ui.rowHover, old)
	} else if len(ui.rowHover) > n {
		ui.rowHover = ui.rowHover[:n]
	}
}

// ensureRowClicks keeps one click gesture per visible row, in step with the
// row list it belongs to. The gestures are kept when the list grows, so a
// scroll does not take a row's hover away with it.
func (ui *uiState) ensureRowClicks(n int) {
	if len(ui.rowClicks) < n {
		old := ui.rowClicks
		ui.rowClicks = make([]gesture.Click, n)
		copy(ui.rowClicks, old)
	} else if len(ui.rowClicks) > n {
		ui.rowClicks = ui.rowClicks[:n]
	}
}

// isEnterKey reports whether a key name is the platform's Enter.
//
// Gio has two names for it and a platform uses only one of them. Windows maps
// VK_RETURN to key.NameReturn; X11 and macOS can produce key.NameEnter as well.
// Filtering on one name alone is why Enter did nothing on Windows while the
// arrow keys worked: they have one name each, Enter has two.
func isEnterKey(name key.Name) bool {
	return name == key.NameReturn || name == key.NameEnter
}

// treeNavKeyNames is the set of keys the tree lists respond to, with Enter
// spelled under both of its names. A key filter takes one name, so the loop
// runs once per name rather than trying to match both in a single filter.
var treeNavKeyNames = []key.Name{
	key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow,
	key.NameReturn, key.NameEnter,
}

// treeActionKeyNames are the keys that do something to the selected row rather
// than move the selection. Delete is the one a file list cannot do without, and
// it is the one the file menu offers today: both go through the same
// confirmation, so neither can delete a file by accident.
//
// Gio has no plain "Delete": the key that deletes forwards is
// NameDeleteForward, which is what Windows delivers for VK_DELETE.
var treeActionKeyNames = []key.Name{key.NameDeleteForward}

func (ui *uiState) handleKeyboard(gtx layout.Context) {
	if ui.searchVisible {
		// The result list is the focus while the search panel is open.
		return
	}
	if ui.contextMenuVisible {
		for {
			ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok {
				continue
			}
			if e.State == key.Press {
				ui.closeFileContextMenu()
				return
			}
		}
	}
	if ui.textActive() {
		return
	}
	if ui.treeHasKeyboard() {
		for _, name := range treeActionKeyNames {
			for {
				ev, ok := gtx.Event(key.Filter{Name: name})
				if !ok {
					break
				}
				e, ok := ev.(key.Event)
				if !ok || e.State != key.Press {
					continue
				}
				ui.deleteSelectedTreeFile()
				return
			}
		}
	}
	for _, name := range treeNavKeyNames {
		for {
			ev, ok := gtx.Event(key.Filter{Name: name})
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			switch ui.keyboardArea {
			case "rgd":
				if isEnterKey(name) {
					ui.activateSelectedRgdNode(gtx)
				} else {
					ui.navigateRgdTree(name)
				}
			default:
				ui.navigateFileTree(name)
			}
		}
	}
}

func (ui *uiState) navigateFileTree(name key.Name) {
	if ui.modTree == nil || len(ui.visibleNodes) == 0 {
		return
	}
	if isEnterKey(name) {
		ui.openSelectedTreeFile()
		return
	}
	idx := 0
	for i, node := range ui.visibleNodes {
		if node.FullPath == ui.selectedTreePath {
			idx = i
			break
		}
	}
	switch name {
	case key.NameUpArrow:
		if idx > 0 {
			idx--
		}
	case key.NameDownArrow:
		if idx+1 < len(ui.visibleNodes) {
			idx++
		}
	case key.NameLeftArrow:
		node := ui.visibleNodes[idx]
		if node.IsDir && node.HasKids && ui.expanded[node.FullPath] {
			delete(ui.expanded, node.FullPath)
			break
		}
		for i := idx - 1; i >= 0; i-- {
			if ui.visibleNodes[i].Depth < node.Depth {
				idx = i
				break
			}
		}
	case key.NameRightArrow:
		node := ui.visibleNodes[idx]
		if node.IsDir && node.HasKids && !ui.expanded[node.FullPath] {
			ui.expanded[node.FullPath] = true
		}
	}
	node := ui.visibleNodes[idx]
	ui.selectedTreePath = node.FullPath
	if node.IsFile {
		ui.selectedPath = node.FullPath
	} else {
		ui.selectedPath = ""
	}
	ui.leftList.ScrollTo(idx)
	ui.markStateDirty()
}

func (ui *uiState) openFileContextMenu(n modtree.VisibleNode, anchor int) {
	if !n.IsFile {
		return
	}
	// A right press in the tree is a press in the tree, so the keys follow the
	// pointer there as they do on a left click.
	ui.keyboardArea = "files"
	ui.selectedTreePath = n.FullPath
	ui.selectedPath = n.FullPath
	ui.contextMenuPath = n.FullPath
	ui.contextMenuAnchor = anchor
	ui.contextMenuVisible = true
	ui.markStateDirty()
}

func (ui *uiState) closeFileContextMenu() {
	ui.contextMenuVisible = false
	ui.contextMenuPath = ""
}

// deleteSelectedTreeFile puts the selected file up for deletion, which is the
// same confirmation the file menu asks for and the only way a file ever leaves
// the mod. The key is a request, not an act: nothing is moved until Delete is
// pressed in the dialog itself.
//
// A file of another mod, or one inside an archive, is not the mod's to delete,
// and the menu's Delete is disabled for it — the key says so rather than
// opening a dialog that cannot be confirmed.
func (ui *uiState) deleteSelectedTreeFile() {
	if ui.fileDeleteDialog {
		return
	}
	path := ui.selectedPath
	if path == "" {
		return
	}
	if !ui.canDeleteCurrentModFile(path) {
		ui.statusText = fmt.Sprintf("%s is not a file of this module, so it cannot be deleted here", filepath.Base(path))
		ui.markStateDirty()
		return
	}
	ui.fileDeletePath = path
	ui.fileDeleteDialog = true
	ui.markStateDirty()
}

// openSettingsDialog remembers what the fields say now, so Cancel has
// something to put back, and fills the editors from it. The format field is
// shown as a comma separated list rather than as a set: it is a thing a
// reader types, and what they typed should still be there when they come
// back to it.
func (ui *uiState) openSettingsDialog() {
	ui.settingsThemeOriginal = ui.darkTheme
	ui.settingsRendererOrig = ui.rendererChoice()
	ui.settingsGameOriginal = ui.gamePath
	ui.settingsGameEditor.SetText(ui.gamePath)
	ui.settingsFormatOriginal = formatTextFormats(ui.textEditorFormats)
	ui.settingsFormatEditor.SetText(ui.settingsFormatOriginal)
	ui.settingsDialog = true
}

func (ui *uiState) openSelectedTreeFile() {
	for _, n := range ui.visibleNodes {
		if n.FullPath == ui.selectedTreePath {
			ui.openTreeFile(n)
			return
		}
	}
}

func (ui *uiState) openTreeFile(n modtree.VisibleNode) {
	if !n.IsFile {
		return
	}
	ui.selectedPath = n.FullPath
	ui.selectedTreePath = n.FullPath
	ui.statusText = fmt.Sprintf("%s  —  %s  [%s]", n.FullPath, n.ModName, kindLabel(n.Kind))
	// The formats that need a viewer of their own are opened by name, and
	// everything else is offered to the text editor. Keeping the two apart
	// is what lets the editor's list be a setting: a module can carry
	// scripts of any naming, and none of them need a case added here.
	//
	// The image formats are asked about as a list rather than spelled out as
	// a case, because that list is the same one the decoders answer to.
	// Spelling it out here is how .rtx and .rsh ended up decodable but
	// unopenable.
	if isImageViewerFile(n.FullPath) {
		ui.openImage(n.FullPath)
		ui.markStateDirty()
		return
	}
	switch strings.ToLower(filepath.Ext(n.FullPath)) {
	case ".rgd":
		ui.openRgd(n.FullPath)
		ui.markStateDirty()
		return
	case ".fda":
		ui.openFda(n.FullPath)
		ui.markStateDirty()
		return
	case ".wav":
		ui.openWav(n.FullPath)
		ui.markStateDirty()
		return
	case ".whm":
		ui.openWhm(n.FullPath)
		ui.markStateDirty()
		return
	}
	if ui.isEditorFile(n.FullPath) {
		ui.openTextFile(n.FullPath)
	}
	ui.markStateDirty()
}

func (ui *uiState) activateSelectedRgdNode(gtx layout.Context) {
	tab := ui.activeTab()
	if tab == nil || tab.kind != "rgd" || tab.rgdFile == nil {
		return
	}
	ui.rgdFlushPending(tab)
	var selected *rgdVisible
	for i := range tab.rgdMidNodes {
		if tab.rgdMidNodes[i].Path == tab.rgdSelected {
			selected = &tab.rgdMidNodes[i]
			break
		}
	}
	if selected == nil || selected.Entry == nil {
		return
	}
	if refPath := ui.findRgdReferenceFile(selected.Ref); refPath != "" {
		file := ui.modTree.FindFile(refPath)
		if file != nil {
			ui.openTreeFile(modtree.VisibleNode{
				Name:     file.Name,
				FullPath: file.FullPath,
				IsFile:   true,
				ModName:  file.Source.ModName,
				Kind:     file.Source.Kind,
			})
			return
		}
	}
	// RGD entries do not have a separate file to open. Keep the entry selected,
	// reveal its properties, and focus the editable property when possible.
	ui.rightScroll.offset = 0
	if selected.IsTable {
		gtx.Execute(key.FocusCmd{Tag: &tab.rgdRefEditor})
	} else if selected.Entry.Type != rgd.RgdTypeBool && rgdIsEditableType(selected.Entry.Type) {
		ed := ui.rgdEnsureValEditor(tab, tab.rgdSelected, selected.Entry.DisplayValue())
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	ui.statusText = "Properties: " + tab.rgdSelected
}

func (ui *uiState) findRgdReferenceFile(ref string) string {
	if ui.modTree == nil {
		return ""
	}
	ref = strings.Trim(strings.TrimSpace(ref), "\"'")
	if ref == "" || strings.Contains(ref, "://") {
		return ""
	}
	ref = strings.ReplaceAll(ref, "/", `\`)
	candidates := []string{ref}
	if !strings.HasPrefix(strings.ToLower(ref), `data\`) {
		candidates = append(candidates, `Data\`+ref)
	}
	for _, candidate := range candidates {
		if file := ui.modTree.FindFile(candidate); file != nil {
			return file.FullPath
		}
	}
	return ""
}

func (ui *uiState) navigateRgdTree(name key.Name) {
	tab := ui.activeTab()
	if tab == nil || tab.kind != "rgd" || len(tab.rgdMidNodes) == 0 {
		return
	}
	idx := 0
	for i, node := range tab.rgdMidNodes {
		if node.Path == tab.rgdSelected {
			idx = i
			break
		}
	}
	switch name {
	case key.NameUpArrow:
		if idx > 0 {
			idx--
		}
	case key.NameDownArrow:
		if idx+1 < len(tab.rgdMidNodes) {
			idx++
		}
	case key.NameLeftArrow:
		node := tab.rgdMidNodes[idx]
		if node.IsTable && node.HasKids && tab.rgdExpanded[node.Path] {
			delete(tab.rgdExpanded, node.Path)
			break
		}
		for i := idx - 1; i >= 0; i-- {
			if tab.rgdMidNodes[i].Depth < node.Depth {
				idx = i
				break
			}
		}
	case key.NameRightArrow:
		node := tab.rgdMidNodes[idx]
		if node.IsTable && node.HasKids && !tab.rgdExpanded[node.Path] {
			tab.rgdExpanded[node.Path] = true
		}
	}
	newPath := tab.rgdMidNodes[idx].Path
	if newPath != tab.rgdSelected {
		ui.rgdFlushPending(tab)
	}
	tab.rgdSelected = newPath
	ui.rebuildRgdMidNodesFor(tab)
	ui.ensureRgdMidClicks(tab, len(tab.rgdMidNodes))
	tab.rgdMidList.ScrollTo(idx)
	ui.markStateDirty()
}

func (ui *uiState) activateTreeRow(index int, n modtree.VisibleNode) {
	ui.keyboardArea = "files"
	ui.selectedTreePath = n.FullPath
	if n.IsDir {
		ui.selectedPath = ""
		if n.HasKids {
			if ui.expanded[n.FullPath] {
				delete(ui.expanded, n.FullPath)
			} else {
				ui.expanded[n.FullPath] = true
			}
		}
	} else if n.IsFile {
		if ui.isExternalTextFile(n.FullPath) && ui.lastTreeClickPath == n.FullPath && time.Since(ui.lastTreeClickAt) < 350*time.Millisecond {
			ui.lastTreeClickPath = ""
			ui.lastTreeClickAt = time.Time{}
			if err := ui.openTreeFileExternally(n.FullPath); err != nil {
				ui.statusText = fmt.Sprintf("Open with system application failed: %v", err)
			}
		} else {
			ui.lastTreeClickPath = n.FullPath
			ui.lastTreeClickAt = time.Now()
			ui.openTreeFile(n)
		}
	} else {
		ui.lastTreeClickPath = ""
		ui.lastTreeClickAt = time.Time{}
	}
	ui.markStateDirty()
}

func (ui *uiState) treeRowAt(y float32) int {
	if ui.leftRowHeightPx <= 0 {
		return -1
	}
	local := y + float32(ui.leftList.Position.Offset)
	return ui.leftList.Position.First + int(local/float32(ui.leftRowHeightPx))
}

// handleTreePointerInput is the file tree's pointer handling.
//
// Every row owns a gesture.Click over its own area, registered by layoutTreeRow
// when the row is laid out, and Gio decides what a click is: a press and a
// release that belong to the same row. Nothing here remembers that a press
// happened, and there is no distance to measure — a gesture that starts on one
// row and is released on another is cancelled by Gio, because the row it
// started on was left behind, which is the whole of what a drag over a list
// has to mean.
//
// What a gesture cannot see is a right press: a click is the primary button by
// definition, and Gio drops any other button. The file menu therefore still
// comes from the one area the tree keeps over the whole list, where a press of
// the secondary button names the row it landed on.
func (ui *uiState) handleTreePointerInput(gtx layout.Context, nodes []modtree.VisibleNode) {
	for i := range ui.rowClicks {
		if i >= len(nodes) {
			break
		}
		click := &ui.rowClicks[i]
		for {
			ev, ok := click.Update(gtx.Source)
			if !ok {
				break
			}
			if ev.Kind == gesture.KindClick && !ui.contextMenuVisible {
				ui.activateTreeRow(i, nodes[i])
			}
		}
		if i < len(ui.rowHover) {
			ui.rowHover[i] = click.Hovered()
		}
	}
	if ui.leftTreePointerTag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: ui.leftTreePointerTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || !e.Buttons.Contain(pointer.ButtonSecondary) {
			continue
		}
		row := ui.treeRowAt(e.Position.Y)
		if row >= 0 && row < len(nodes) && nodes[row].IsFile {
			ui.openFileContextMenu(nodes[row], row)
		}
	}
}

// handleTabBarInput is the tab bar's own pointer handling: the wheel slides the
// row sideways, and a right press opens the context menu on whichever tab it
// landed on. Both are read from one area covering the whole bar, which the
// layout registers inside a PassOp so the tabs underneath keep their clicks.
func (ui *uiState) handleTabBarInput(gtx layout.Context) {
	if ui.tabBarPointerTag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: ui.tabBarPointerTag, Kinds: pointer.Press | pointer.Scroll})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Scroll:
			// The wheel is vertical and the row is horizontal, so the vertical
			// amount is what moves it. A trackpad reporting both axes still
			// works: only the vertical component is read, which is the one the
			// gesture starts with. A positive amount is downwards, and the
			// row moves along with the wheel, not against it.
			if e.Scroll.Y == 0 {
				continue
			}
			ui.tabScrollX += int(e.Scroll.Y * tabWheelStepPx)
			// Clamping here as well as in the layout keeps the bar from
			// overshooting on a fast wheel and then snapping back a frame later.
			ui.clampTabScroll(ui.tabBarWidth, ui.tabRowWidth)
		case pointer.Press:
			if !e.Buttons.Contain(pointer.ButtonSecondary) {
				continue
			}
			idx := ui.tabAt(e.Position)
			if idx < 0 {
				// A right press on the empty part of the bar is not a tab, so
				// there is nothing to open a menu for. It does put an open menu
				// away, which is what a click on the background means anywhere
				// else.
				ui.closeTabContextMenu()
				continue
			}
			// e.Position is in the bar's own coordinates; the menu is drawn in
			// the window's, so the bar's top is added to put it in the right
			// place.
			ui.openTabContextMenu(idx, ui.tabContextPoint(gtx, e.Position.X))
		}
	}
}

// tabContextPoint is where a menu opened on a press at x inside the bar belongs
// in the window's coordinates: under the bar's bottom edge, at the click's
// horizontal position. It is a function of its own because the offset is easy to
// get wrong by the height of a band — a menu 25px too high sits on the bar it
// belongs to, and nobody would notice until they clicked it.
func (ui *uiState) tabContextPoint(gtx layout.Context, x float32) image.Point {
	return image.Pt(int(x), tabBarTop(gtx)+gtx.Dp(unit.Dp(tabBarHeightDp)))
}

// handleTabContextMenuInput acts on the tab context menu and puts it away again.
// Escape closes it, a click elsewhere closes it, and the one item closes the
// tabs.
func (ui *uiState) handleTabContextMenuInput(gtx layout.Context) {
	if !ui.tabContextVisible {
		return
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		ui.closeTabContextMenu()
		return
	}
	if ui.tabContextCloseAll.Clicked(gtx) {
		ui.closeTabContextMenu()
		ui.closeAllFileTabs()
		ui.statusText = "Closed all tabs"
		return
	}
	if ui.tabContextDismiss.Clicked(gtx) {
		ui.closeTabContextMenu()
	}
}

func (ui *uiState) handleInput(gtx layout.Context) {
	// The new unit dialog writes a file, so it is read before everything else
	// and nothing behind it is read while it is up.
	if ui.newObjDialog {
		ui.handleNewObjDialogInput(gtx)
		return
	}
	if ui.fileDeleteDialog {
		if ui.fileDeleteConfirm.Clicked(gtx) {
			path := ui.fileDeletePath
			ui.fileDeleteDialog = false
			ui.fileDeletePath = ""
			if err := ui.deleteCurrentModFile(path); err != nil {
				ui.statusText = fmt.Sprintf("Delete failed: %v", err)
			}
		} else if ui.fileDeleteCancel.Clicked(gtx) {
			ui.fileDeleteDialog = false
			ui.fileDeletePath = ""
			ui.statusText = "File deletion cancelled"
		} else {
			// The dialog can be reached from the keyboard, so it has to be
			// leaveable from there too.
			for {
				ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
				if !ok {
					break
				}
				if e, ok := ev.(key.Event); ok && e.State == key.Press {
					ui.fileDeleteDialog = false
					ui.fileDeletePath = ""
					ui.statusText = "File deletion cancelled"
					break
				}
			}
		}
		return
	}
	if ui.clearRecycleDialog {
		if ui.clearRecycleConfirm.Clicked(gtx) {
			ui.clearRecycleDialog = false
			dir, err := ensureRecycleBinDir()
			if err == nil {
				err = clearRecycleDir(dir)
			}
			if err != nil {
				ui.statusText = fmt.Sprintf("Empty Recycle Bin failed: %v", err)
			} else {
				ui.statusText = "Recycle Bin emptied"
			}
		} else if ui.clearRecycleCancel.Clicked(gtx) {
			ui.clearRecycleDialog = false
			ui.statusText = "Recycle Bin cleanup cancelled"
		}
		return
	}
	// The localisation editor is modal over the object window: it owns the keys
	// and the clicks, so nothing behind it is read while it is up. It sits above
	// the other dialogs in the stack, so it is resolved before them.
	if ui.localeEditDialog {
		ui.handleLocaleEditInput(gtx)
		return
	}
	if ui.settingsDialog {
		for {
			if _, ok := ui.settingsGameEditor.Update(gtx); !ok {
				break
			}
		}
		for {
			if _, ok := ui.settingsFormatEditor.Update(gtx); !ok {
				break
			}
		}
		if ui.settingsThemeClick.Clicked(gtx) {
			ui.darkTheme = !ui.darkTheme
			applyMaterialTheme(ui.th, ui.darkTheme)
			ui.markStateDirty()
		}
		if ui.settingsVkClick.Clicked(gtx) {
			ui.rendererPref = "vulkan"
			ui.forceSoftRenderer = false
			ui.markStateDirty()
		}
		if ui.settingsD3DClick.Clicked(gtx) {
			ui.rendererPref = "d3d11"
			ui.forceSoftRenderer = false
			ui.markStateDirty()
		}
		if ui.settingsSoftClick.Clicked(gtx) {
			ui.rendererPref = "software"
			ui.forceSoftRenderer = true
			ui.markStateDirty()
		}
		if ui.settingsOk.Clicked(gtx) {
			ui.gamePath = strings.TrimSpace(ui.settingsGameEditor.Text())
			// The format list is read here rather than as it is typed,
			// so a half-finished entry never changes what a click opens.
			ui.textEditorFormats = parseTextFormats(ui.settingsFormatEditor.Text())
			// A new game folder means a different set of .ucs files, so the
			// language list and the string table are both rebuilt from it.
			ui.refreshLocales()
			ui.settingsDialog = false
			ui.markStateDirty()
		}
		if ui.settingsCancel.Clicked(gtx) {
			ui.darkTheme = ui.settingsThemeOriginal
			ui.rendererPref = ui.settingsRendererOrig
			ui.forceSoftRenderer = ui.rendererPref == "software"
			ui.gamePath = ui.settingsGameOriginal
			ui.textEditorFormats = parseTextFormats(ui.settingsFormatOriginal)
			applyMaterialTheme(ui.th, ui.darkTheme)
			ui.settingsDialog = false
		}
		return
	}
	// The file dialog is modal and is shared by Open Module, the timeline
	// import and the sound field, so it answers before the rest of the input.
	if ui.filePickOpen {
		ui.handleFilePicker(gtx)
		return
	}
	if ui.archiveDialog {
		ui.handleArchiveDialogInput(gtx)
		return
	}
	// The event picker is modal over the Events section the same way.
	if ui.evtPick.open {
		ui.handleEvtPickerInput(gtx)
		return
	}
	// The object interface is a tab, so its pickers and rows are part of the
	// ordinary input path rather than a modal branch that swallows everything.
	// The tool button that opens it is polled on every frame, which is why it
	// runs before the rest.
	// The unit screen names are read in the background and arrive in batches.
	// Nothing else keeps the window repainting, so this has to ask for the
	// redraw itself or a list would sit on file names until the mouse moved.
	if ui.pollUnitNames() {
		gtx.Execute(op.InvalidateCmd{})
	}
	// The squads a unit belongs to are indexed in the background for the same
	// reason: the index is built by walking 431 squad files, and nothing else
	// would repaint the window when the results arrived.
	if ui.pollUnitSquads() {
		gtx.Execute(op.InvalidateCmd{})
	}
	// The references between a mod's units, buildings, weapons and abilities
	// are read in the background too, and for the same reason: reading six
	// hundred files takes seconds, and a relationships list that filled in
	// quietly would otherwise sit half-empty until the mouse moved.
	if ui.pollGraphLinks() {
		gtx.Execute(op.InvalidateCmd{})
	}
	ui.handleUnitToolInput(gtx)
	if ui.unitTabActive() {
		ui.handleUnitTabInput(gtx)
	}
	// The language picker hangs off the menu bar like the menus and the search
	// field, and it puts both of them away when it opens, so it is polled first
	// and leaves the rest of the input alone when it is closed.
	ui.handleLocaleInput(gtx)
	// The tab bar's wheel and right click, and then whatever the context menu
	// they opened wants. Both come before the tab clicks so a right press on a
	// tab is not also read as selecting it.
	ui.handleTabBarInput(gtx)
	ui.handleTabContextMenuInput(gtx)
	// An Alt+click on a resolved string was seen while the row was laid out;
	// opening the .ucs is what the next frame does with it.
	if ui.localePending != "" {
		id := ui.localePending
		ui.localePending = ""
		ui.jumpToLocaleEntry(id)
	}
	// Alt+Ctrl on a localised row, or a click on one with no text, was seen the
	// same way. The request is acted on here because putting a dialog up is the
	// input pass's business, not a layout's.
	if req := ui.localePendingRequest; req.action != localeRowNothing {
		ui.localePendingRequest = localeEditRequest{}
		switch req.action {
		case localeRowJump:
			ui.jumpToLocaleEntry(req.id)
		case localeRowEdit, localeRowCreate:
			// A press in the file view knows its own entry; one in the object
			// window has to be looked up among the unit's rows.
			field := ui.localePendingField
			if field == nil {
				if d := ui.currentUnitDetails(); d != nil {
					field = localeFieldForID(d, req.id)
				}
			}
			fieldKey := ui.localePendingFieldKey
			if fieldKey == "" && field != nil {
				fieldKey = localeRowKey(field, "")
			}
			ui.localePendingField = nil
			ui.localePendingFieldKey = ""
			ui.openLocaleEdit(req.id, req.action, field, fieldKey)
		}
	}
	// The search field owns the arrow and Enter keys while it is open, so it
	// has to run before the tree's keyboard handling.
	ui.handleSearchInput(gtx)
	ui.handleKeyboard(gtx)

	for i := range ui.menuClicks {
		if ui.menuClicks[i].Clicked(gtx) {
			ui.closeFileContextMenu()
			// A menu and the search panel both hang off the menu bar, so
			// opening a menu puts the search away.
			ui.closeSearch()
			if ui.menuOpen == i {
				ui.menuOpen = -1
			} else {
				ui.menuOpen = i
			}
			ui.recentMenuExpanded = false
		}
	}
	if ui.menuOpen >= 0 {
		items := ui.menuItemsForOpen()
		ui.ensureMenuItemClicks(ui.menuOpen, len(items))
		for j := range ui.menuItemClicks[ui.menuOpen] {
			if ui.menuItemClicks[ui.menuOpen][j].Clicked(gtx) {
				if items[j] != "—" {
					label := items[j]
					if ui.menuOpen == 0 && label == "Recent Modules" {
						ui.recentMenuExpanded = !ui.recentMenuExpanded
						continue
					}
					if ui.menuOpen == 0 && ui.recentMenuExpanded {
						header := recentModulesHeaderIndex()
						nameIndex := j - header - 2
						if header >= 0 && nameIndex >= 0 && nameIndex < len(ui.recentModules) {
							ui.loadModule(ui.recentModules[nameIndex])
							ui.menuOpen = -1
							ui.recentMenuExpanded = false
							break
						}
					}
					if ui.menuOpen == 0 && isEmptyRecycleBinMenuLabel(label) {
						ui.clearRecycleDialog = true
					} else if ui.menuOpen == 0 {
						switch label {
						case "Open Module...":
							ui.openModulePicker()
						case "Recent Modules":
							ui.recentMenuExpanded = !ui.recentMenuExpanded
							continue
						case "View Recycle Bin":
							if err := openRecycleFolder(); err != nil {
								ui.statusText = fmt.Sprintf("Open Recycle Bin failed: %v", err)
							} else {
								ui.statusText = "Opened Recycle Bin"
							}
						case "Settings":
							ui.openSettingsDialog()
						case "Close":
							ui.stopModFileWatcher()
							ui.closeAllFileTabs()
							if ui.modTree != nil {
								ui.modTree.Close()
								ui.modTree = nil
								ui.modulePath = ""
								ui.visibleNodes = nil
								ui.expanded = map[string]bool{"Data": true}
								ui.selectedPath = ""
								ui.statusText = "No module loaded"
								ui.syncWindowTitle()
							}
						case "Associate .module files":
							if err := registerModuleAssociation(); err != nil {
								ui.statusText = fmt.Sprintf("Associate failed: %v", err)
							} else {
								ui.statusText = fmt.Sprintf("Associated .module with %s — double-click any .module to open", getExePath())
							}
						case "Remove .module association":
							if err := unregisterModuleAssociation(); err != nil {
								ui.statusText = fmt.Sprintf("Remove failed: %v", err)
							} else {
								ui.statusText = "Removed .module association (HKCU)"
							}
						case "Exit":
							exitLogged(0)
						default:
							ui.statusText = fmt.Sprintf("Menu: %s", label)
						}
					} else if ui.menuOpen == 1 {
						switch label {
						case "Save":
							if ui.saveEligible() {
								ui.handleSaveActiveTab()
							} else {
								ui.statusText = "Save — no unsaved RGD/text changes"
							}
						case "Export File":
							if eligible, reason := ui.exportEligible(ui.activeTab()); eligible {
								ui.handleExportActiveTab()
							} else {
								ui.statusText = "Export — " + reason
							}
						case "Pack SGA...":
							ui.packCurrentModSGA()
						case "Extract SGA...":
							ui.openArchiveDialog()
						default:
							ui.statusText = fmt.Sprintf("Mod menu: %s", label)
						}
					} else if ui.menuOpen == 2 {
						switch label {
						case "Open log folder":
							if err := openLogFolder(); err != nil {
								ui.statusText = fmt.Sprintf("Open log folder failed: %v — log=%s", err, logDir())
							} else {
								ui.statusText = fmt.Sprintf("Opened log folder: %s", logDir())
							}
						default:
							ui.statusText = fmt.Sprintf("Menu: %s → %s", menus[ui.menuOpen].name, label)
						}
					} else {
						ui.statusText = fmt.Sprintf("Menu: %s → %s", menus[ui.menuOpen].name, label)
					}
					ui.menuOpen = -1
				}
				// The click handler closes the menu, so stop this loop before
				// the next iteration indexes menuItemClicks with menuOpen == -1.
				break
			}
		}
		if ui.dismissClick.Clicked(gtx) {
			ui.menuOpen = -1
		}
	}
	// The language list, the search panel and the menus all hang off the menu
	// bar, and only one of them can own the space under it. Opening the list
	// puts the other two away when it happens, so this is the other half of
	// that rule, run after both of them have been resolved.
	ui.closeLocaleUnderOtherMenuBarPopups()
	ui.ensureTabClicks()
	for i := range ui.tabClicks {
		if ui.tabClicks[i].Clicked(gtx) {
			ui.tabSel = i
			ui.markStateDirty()
		}
	}
	for idx := range ui.tabCloses {
		if ui.tabCloses[idx].Clicked(gtx) {
			ui.closeTabAt(idx)
			ui.markStateDirty()
			break
		}
	}
	if ui.fdaActive() {
		tab := ui.activeTab()
		ui.checkFdaAutoRepeat()
		if tab.fdaPlayPause.Clicked(gtx) {
			ui.fdaTogglePlayPause()
		}
		if tab.fdaBack10.Clicked(gtx) {
			ui.fdaSeekBy(-10 * time.Second)
		}
		if tab.fdaFwd10.Clicked(gtx) {
			ui.fdaSeekBy(10 * time.Second)
		}
		if tab.fdaBack30.Clicked(gtx) {
			ui.fdaSeekBy(-30 * time.Second)
		}
		if tab.fdaFwd30.Clicked(gtx) {
			ui.fdaSeekBy(30 * time.Second)
		}
		if tab.fdaRepeatBtn.Clicked(gtx) {
			tab.fdaRepeat = !tab.fdaRepeat
			ui.statusText = fmt.Sprintf("Repeat: %v", tab.fdaRepeat)
		}
		if tab.fdaExportBtn.Clicked(gtx) {
			ui.fdaExportWav()
		}
		if tab.fdaStopBtn.Clicked(gtx) {
			if tab.fdaPlayer != nil {
				tab.fdaPlayer.PauseAndStopReading()
				_, _ = tab.fdaPlayer.Seek(0, io.SeekStart)
				tab.fdaPaused = true
				ui.statusText = "Stopped"
			}
		}
	}
	if tab := ui.activeTab(); tab != nil && tab.kind == "wav" && tab.wavPlayer != nil {
		ui.checkWavAutoRepeat()
		if tab.wavPlayClick.Clicked(gtx) {
			ui.wavTogglePlayPause()
		}
		if tab.wavStopClick.Clicked(gtx) {
			ui.wavStop()
		}
		if tab.wavRepeatClick.Clicked(gtx) {
			tab.wavRepeat = !tab.wavRepeat
			ui.statusText = fmt.Sprintf("WAV Repeat: %v", tab.wavRepeat)
		}
	}
	if tab := ui.activeTab(); tab != nil && tab.kind == "whm" {
		if tab.whmConvertClick.Clicked(gtx) {
			ui.convertWhmToSgmFile(tab)
		}
	}
	if ui.rgdActive() {
		ui.focusClickedRgdEditor(gtx)
		ui.rgdSyncAndApply(gtx)
	}
	if ui.textActive() {
		ui.textSyncAndApply(gtx)
	}
	nodes := ui.getVisibleNodes()
	ui.ensureRowClicks(len(nodes))
	ui.ensureRowHover(len(nodes))
	ui.handleTreePointerInput(gtx, nodes)
	if ui.contextMenuVisible {
		if ui.contextDuplicate.Clicked(gtx) {
			path := ui.contextMenuPath
			ui.closeFileContextMenu()
			if err := ui.duplicateCurrentModFile(path); err != nil {
				ui.statusText = fmt.Sprintf("Duplicate failed: %v", err)
			}
		} else if ui.contextDelete.Clicked(gtx) && ui.canDeleteCurrentModFile(ui.contextMenuPath) {
			ui.fileDeletePath = ui.contextMenuPath
			ui.fileDeleteDialog = true
			ui.closeFileContextMenu()
		} else if ui.contextDismissClick.Clicked(gtx) {
			ui.closeFileContextMenu()
		}
	}
	if tab := ui.activeTab(); tab != nil && tab.kind == "rgd" {
		selBefore := tab.rgdSelected
		if len(tab.rgdMidNodes) == 0 {
			ui.rebuildRgdMidNodesFor(tab)
			ui.ensureRgdMidClicks(tab, len(tab.rgdMidNodes))
		}
		midNodes := tab.rgdMidNodes
		ui.ensureRgdMidClicks(tab, len(midNodes))
		clickedAny := false
		for i, m := range midNodes {
			if tab.rgdMidClicks[i].Clicked(gtx) {
				ui.keyboardArea = "rgd"
				if m.IsTable && m.HasKids {
					if tab.rgdExpanded[m.Path] {
						delete(tab.rgdExpanded, m.Path)
					} else {
						tab.rgdExpanded[m.Path] = true
					}
				}
				if m.Path != selBefore {
					ui.rgdFlushPending(tab)
				}
				tab.rgdSelected = m.Path
				clickedAny = true
				ui.markStateDirty()
			}
		}
		if clickedAny {
			ui.rebuildRgdMidNodesFor(tab)
			ui.ensureRgdMidClicks(tab, len(tab.rgdMidNodes))
		}
	}
}

func kindLabel(k modtree.SourceKind) string {
	switch k {
	case modtree.KindThisMod:
		return "this mod"
	case modtree.KindEngine:
		return "engine"
	default:
		return "required"
	}
}
