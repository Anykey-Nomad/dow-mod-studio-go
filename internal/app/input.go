package app

import (
	"dow-mod-studio/internal/app/uikit"
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
	"gioui.org/widget"
)

// WheelNotchDelta lives in uikit; the alias keeps every call site as it was.
const wheelNotchDelta = uikit.WheelNotchDelta

// GetVisibleNodes lives in uikit; the alias keeps every call site as it was.
var GetVisibleNodes = uikit.GetVisibleNodes

func EnsureRowHover(ui *uiState, n int) {
	if len(ui.RowHover) < n {
		old := ui.RowHover
		ui.RowHover = make([]bool, n)
		copy(ui.RowHover, old)
	} else if len(ui.RowHover) > n {
		ui.RowHover = ui.RowHover[:n]
	}
}

// ensureRowClicks keeps one click gesture per visible row, in step with the
// row list it belongs to. The gestures are kept when the list grows, so a
// scroll does not take a row's hover away with it.
func EnsureRowClicks(ui *uiState, n int) {
	if len(ui.RowClicks) < n {
		old := ui.RowClicks
		ui.RowClicks = make([]gesture.Click, n)
		copy(ui.RowClicks, old)
	} else if len(ui.RowClicks) > n {
		ui.RowClicks = ui.RowClicks[:n]
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

func HandleKeyboard(ui *uiState, gtx layout.Context) {
	if ui.SearchVisible {
		// The result list is the focus while the search panel is open.
		return
	}
	if ui.ContextMenuVisible {
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
				CloseFileContextMenu(ui)
				return
			}
		}
	}
	if TextActive(ui) {
		return
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "sgb" && tab.SgbView3D && tab.Model != nil && tab.SgbCameraTag != nil && gtx.Focused(tab.SgbCameraTag) {
		for {
			ev, ok := gtx.Event(key.Filter{Focus: tab.SgbCameraTag, Name: key.NameDeleteForward})
			if !ok {
				break
			}
			ke, ok := ev.(key.Event)
			if !ok || ke.State != key.Press {
				continue
			}
			DeleteSelectedSgbEntities(ui, tab)
			return
		}
		// The focused map viewer consumes its own arrows after input handling;
		// do not also move the file-tree selection on the same key press.
		return
	}
	if TreeHasKeyboard(ui) {
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
				DeleteSelectedTreeFile(ui)
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
			switch ui.KeyboardArea {
			case "rgd":
				if isEnterKey(name) {
					ActivateSelectedRgdNode(ui, gtx)
				} else {
					NavigateRgdTree(ui, name)
				}
			default:
				NavigateFileTree(ui, name)
			}
		}
	}
}

func NavigateFileTree(ui *uiState, name key.Name) {
	if ui.ModTree == nil || len(ui.VisibleNodes) == 0 {
		return
	}
	if isEnterKey(name) {
		OpenSelectedTreeFile(ui)
		return
	}
	idx := 0
	for i, node := range ui.VisibleNodes {
		if node.FullPath == ui.SelectedTreePath {
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
		if idx+1 < len(ui.VisibleNodes) {
			idx++
		}
	case key.NameLeftArrow:
		node := ui.VisibleNodes[idx]
		if node.IsDir && node.HasKids && ui.Expanded[node.FullPath] {
			delete(ui.Expanded, node.FullPath)
			break
		}
		for i := idx - 1; i >= 0; i-- {
			if ui.VisibleNodes[i].Depth < node.Depth {
				idx = i
				break
			}
		}
	case key.NameRightArrow:
		node := ui.VisibleNodes[idx]
		if node.IsDir && node.HasKids && !ui.Expanded[node.FullPath] {
			ui.Expanded[node.FullPath] = true
		}
	}
	node := ui.VisibleNodes[idx]
	ui.SelectedTreePath = node.FullPath
	if node.IsFile {
		ui.SelectedPath = node.FullPath
	} else {
		ui.SelectedPath = ""
	}
	ui.LeftList.ScrollTo(idx)
	MarkStateDirty(ui)
}

func OpenFileContextMenu(ui *uiState, n modtree.VisibleNode, anchor int) {
	if !n.IsFile {
		return
	}
	// A right press in the tree is a press in the tree, so the keys follow the
	// pointer there as they do on a left click.
	ui.KeyboardArea = "files"
	ui.SelectedTreePath = n.FullPath
	ui.SelectedPath = n.FullPath
	ui.ContextMenuPath = n.FullPath
	ui.ContextMenuAnchor = anchor
	ui.ContextMenuVisible = true
	MarkStateDirty(ui)
}

func CloseFileContextMenu(ui *uiState) {
	ui.ContextMenuVisible = false
	ui.ContextMenuPath = ""
}

// deleteSelectedTreeFile puts the selected file up for deletion, which is the
// same confirmation the file menu asks for and the only way a file ever leaves
// the mod. The key is a request, not an act: nothing is moved until Delete is
// pressed in the dialog itself.
//
// A file of another mod, or one inside an archive, is not the mod's to delete,
// and the menu's Delete is disabled for it — the key says so rather than
// opening a dialog that cannot be confirmed.
func DeleteSelectedTreeFile(ui *uiState) {
	if ui.FileDeleteDialog {
		return
	}
	path := ui.SelectedPath
	if path == "" {
		return
	}
	if !CanDeleteCurrentModFile(ui, path) {
		ui.StatusText = fmt.Sprintf("%s is not a file of this module, so it cannot be deleted here", filepath.Base(path))
		MarkStateDirty(ui)
		return
	}
	ui.FileDeletePath = path
	ui.FileDeleteDialog = true
	MarkStateDirty(ui)
}

// openSettingsDialog remembers what the fields say now, so Cancel has
// something to put back, and fills the editors from it. The format field is
// shown as a comma separated list rather than as a set: it is a thing a
// reader types, and what they typed should still be there when they come
// back to it.
func OpenSettingsDialog(ui *uiState) {
	ui.SettingsThemeOriginal = ui.DarkTheme
	ui.SettingsGameOriginal = ui.GamePath
	ui.SettingsGameEditor.SetText(ui.GamePath)
	ui.SettingsFormatOriginal = formatTextFormats(ui.TextEditorFormats)
	ui.SettingsFormatEditor.SetText(ui.SettingsFormatOriginal)
	// The association buttons are one per format, and what a click would do
	// depends on the registry right now, so the rows are sized and read here.
	ui.AssocClicks = make([]widget.Clickable, len(assocTypes))
	RefreshAssocStates(ui)
	ui.SettingsDialog = true
}

func OpenSelectedTreeFile(ui *uiState) {
	for _, n := range ui.VisibleNodes {
		if n.FullPath == ui.SelectedTreePath {
			OpenTreeFile(ui, n)
			return
		}
	}
}

func OpenTreeFile(ui *uiState, n modtree.VisibleNode) {
	if !n.IsFile {
		return
	}
	ui.SelectedPath = n.FullPath
	ui.SelectedTreePath = n.FullPath
	ui.StatusText = fmt.Sprintf("%s  —  %s  [%s]", n.FullPath, n.ModName, kindLabel(n.Kind))
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
		OpenImage(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	}
	switch strings.ToLower(filepath.Ext(n.FullPath)) {
	case ".rgd":
		OpenRgd(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".fda":
		OpenFda(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".wav":
		OpenWav(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".sgb":
		OpenSgb(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".whm", ".sgm":
		OpenWhm(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	}
	if IsEditorFile(ui, n.FullPath) {
		OpenTextFile(ui, n.FullPath)
	}
	MarkStateDirty(ui)
}

func ActivateSelectedRgdNode(ui *uiState, gtx layout.Context) {
	tab := ActiveTab(ui)
	if tab == nil || tab.Kind != "rgd" || tab.RgdFile == nil {
		return
	}
	RgdFlushPending(ui, tab)
	var selected *rgdVisible
	for i := range tab.RgdMidNodes {
		if tab.RgdMidNodes[i].Path == tab.RgdSelected {
			selected = &tab.RgdMidNodes[i]
			break
		}
	}
	if selected == nil || selected.Entry == nil {
		return
	}
	if refPath := FindRgdReferenceFile(ui, selected.Ref); refPath != "" {
		file := ui.ModTree.FindFile(refPath)
		if file != nil {
			OpenTreeFile(ui, modtree.VisibleNode{
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
	ui.RightScroll.Offset = 0
	if selected.IsTable {
		gtx.Execute(key.FocusCmd{Tag: &tab.RgdRefEditor})
	} else if selected.Entry.Type != rgd.RgdTypeBool && rgdIsEditableType(selected.Entry.Type) {
		ed := RgdEnsureValEditor(ui, tab, tab.RgdSelected, selected.Entry.DisplayValue())
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	ui.StatusText = "Properties: " + tab.RgdSelected
}

func FindRgdReferenceFile(ui *uiState, ref string) string {
	if ui.ModTree == nil {
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
		if file := ui.ModTree.FindFile(candidate); file != nil {
			return file.FullPath
		}
	}
	return ""
}

func NavigateRgdTree(ui *uiState, name key.Name) {
	tab := ActiveTab(ui)
	if tab == nil || tab.Kind != "rgd" || len(tab.RgdMidNodes) == 0 {
		return
	}
	idx := 0
	for i, node := range tab.RgdMidNodes {
		if node.Path == tab.RgdSelected {
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
		if idx+1 < len(tab.RgdMidNodes) {
			idx++
		}
	case key.NameLeftArrow:
		node := tab.RgdMidNodes[idx]
		if node.IsTable && node.HasKids && tab.RgdExpanded[node.Path] {
			delete(tab.RgdExpanded, node.Path)
			break
		}
		for i := idx - 1; i >= 0; i-- {
			if tab.RgdMidNodes[i].Depth < node.Depth {
				idx = i
				break
			}
		}
	case key.NameRightArrow:
		node := tab.RgdMidNodes[idx]
		if node.IsTable && node.HasKids && !tab.RgdExpanded[node.Path] {
			tab.RgdExpanded[node.Path] = true
		}
	}
	newPath := tab.RgdMidNodes[idx].Path
	if newPath != tab.RgdSelected {
		RgdFlushPending(ui, tab)
	}
	tab.RgdSelected = newPath
	RebuildRgdMidNodesFor(ui, tab)
	EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
	tab.RgdMidList.ScrollTo(idx)
	MarkStateDirty(ui)
}

func ActivateTreeRow(ui *uiState, index int, n modtree.VisibleNode) {
	ui.KeyboardArea = "files"
	ui.SelectedTreePath = n.FullPath
	if n.IsDir {
		ui.SelectedPath = ""
		if n.HasKids {
			if ui.Expanded[n.FullPath] {
				delete(ui.Expanded, n.FullPath)
			} else {
				ui.Expanded[n.FullPath] = true
			}
		}
	} else if n.IsFile {
		if IsExternalTextFile(ui, n.FullPath) && ui.LastTreeClickPath == n.FullPath && time.Since(ui.LastTreeClickAt) < 350*time.Millisecond {
			ui.LastTreeClickPath = ""
			ui.LastTreeClickAt = time.Time{}
			if err := OpenTreeFileExternally(ui, n.FullPath); err != nil {
				ui.StatusText = fmt.Sprintf("Open with system application failed: %v", err)
			}
		} else {
			ui.LastTreeClickPath = n.FullPath
			ui.LastTreeClickAt = time.Now()
			OpenTreeFile(ui, n)
		}
	} else {
		ui.LastTreeClickPath = ""
		ui.LastTreeClickAt = time.Time{}
	}
	MarkStateDirty(ui)
}

func TreeRowAt(ui *uiState, y float32) int {
	if ui.LeftRowHeightPx <= 0 {
		return -1
	}
	local := y + float32(ui.LeftList.Position.Offset)
	return ui.LeftList.Position.First + int(local/float32(ui.LeftRowHeightPx))
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
func HandleTreePointerInput(ui *uiState, gtx layout.Context, nodes []modtree.VisibleNode) {
	for i := range ui.RowClicks {
		if i >= len(nodes) {
			break
		}
		click := &ui.RowClicks[i]
		for {
			ev, ok := click.Update(gtx.Source)
			if !ok {
				break
			}
			if ev.Kind == gesture.KindClick && !ui.ContextMenuVisible {
				ActivateTreeRow(ui, i, nodes[i])
			}
		}
		if i < len(ui.RowHover) {
			ui.RowHover[i] = click.Hovered()
		}
	}
	if ui.LeftTreePointerTag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: ui.LeftTreePointerTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || !e.Buttons.Contain(pointer.ButtonSecondary) {
			continue
		}
		row := TreeRowAt(ui, e.Position.Y)
		if row >= 0 && row < len(nodes) && nodes[row].IsFile {
			OpenFileContextMenu(ui, nodes[row], row)
		}
	}
}

// handleTabBarInput is the tab bar's own pointer handling: the wheel slides the
// row sideways, and a right press opens the context menu on whichever tab it
// landed on. Both are read from one area covering the whole bar, which the
// layout registers inside a PassOp so the tabs underneath keep their clicks.
func HandleTabBarInput(ui *uiState, gtx layout.Context) {
	if ui.TabBarPointerTag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: ui.TabBarPointerTag, Kinds: pointer.Press | pointer.Scroll})
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
			ui.TabScrollX += int(e.Scroll.Y * tabWheelStepPx)
			// Clamping here as well as in the layout keeps the bar from
			// overshooting on a fast wheel and then snapping back a frame later.
			ClampTabScroll(ui, ui.TabBarWidth, ui.TabRowWidth)
		case pointer.Press:
			if !e.Buttons.Contain(pointer.ButtonSecondary) {
				continue
			}
			idx := TabAt(ui, e.Position)
			if idx < 0 {
				// A right press on the empty part of the bar is not a tab, so
				// there is nothing to open a menu for. It does put an open menu
				// away, which is what a click on the background means anywhere
				// else.
				CloseTabContextMenu(ui)
				continue
			}
			// e.Position is in the bar's own coordinates; the menu is drawn in
			// the window's, so the bar's top is added to put it in the right
			// place.
			OpenTabContextMenu(ui, idx, TabContextPoint(ui, gtx, e.Position.X))
		}
	}
}

// tabContextPoint is where a menu opened on a press at x inside the bar belongs
// in the window's coordinates: under the bar's bottom edge, at the click's
// horizontal position. It is a function of its own because the offset is easy to
// get wrong by the height of a band — a menu 25px too high sits on the bar it
// belongs to, and nobody would notice until they clicked it.
func TabContextPoint(ui *uiState, gtx layout.Context, x float32) image.Point {
	return image.Pt(int(x), tabBarTop(gtx)+gtx.Dp(unit.Dp(tabBarHeightDp)))
}

// handleTabContextMenuInput acts on the tab context menu and puts it away again.
// Escape closes it, a click elsewhere closes it, and the one item closes the
// tabs.
func HandleTabContextMenuInput(ui *uiState, gtx layout.Context) {
	if !ui.TabContextVisible {
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
		CloseTabContextMenu(ui)
		return
	}
	if ui.TabContextCloseAll.Clicked(gtx) {
		CloseTabContextMenu(ui)
		CloseAllFileTabs(ui)
		ui.StatusText = "Closed all tabs"
		return
	}
	if ui.TabContextDismiss.Clicked(gtx) {
		CloseTabContextMenu(ui)
	}
}

func HandleInput(ui *uiState, gtx layout.Context) {
	// The new unit dialog writes a file, so it is read before everything else
	// and nothing behind it is read while it is up.
	if ui.NewObjDialog {
		HandleNewObjDialogInput(ui, gtx)
		return
	}
	if ui.FileDeleteDialog {
		if ui.FileDeleteConfirm.Clicked(gtx) {
			path := ui.FileDeletePath
			ui.FileDeleteDialog = false
			ui.FileDeletePath = ""
			if err := DeleteCurrentModFile(ui, path); err != nil {
				ui.StatusText = fmt.Sprintf("Delete failed: %v", err)
			}
		} else if ui.FileDeleteCancel.Clicked(gtx) {
			ui.FileDeleteDialog = false
			ui.FileDeletePath = ""
			ui.StatusText = "File deletion cancelled"
		} else {
			// The dialog can be reached from the keyboard, so it has to be
			// leaveable from there too.
			for {
				ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
				if !ok {
					break
				}
				if e, ok := ev.(key.Event); ok && e.State == key.Press {
					ui.FileDeleteDialog = false
					ui.FileDeletePath = ""
					ui.StatusText = "File deletion cancelled"
					break
				}
			}
		}
		return
	}
	if ui.ClearRecycleDialog {
		if ui.ClearRecycleConfirm.Clicked(gtx) {
			ui.ClearRecycleDialog = false
			dir, err := ensureRecycleBinDir()
			if err == nil {
				err = clearRecycleDir(dir)
			}
			if err != nil {
				ui.StatusText = fmt.Sprintf("Empty Recycle Bin failed: %v", err)
			} else {
				ui.StatusText = "Recycle Bin emptied"
			}
		} else if ui.ClearRecycleCancel.Clicked(gtx) {
			ui.ClearRecycleDialog = false
			ui.StatusText = "Recycle Bin cleanup cancelled"
		}
		return
	}
	// The localisation editor is modal over the object window: it owns the keys
	// and the clicks, so nothing behind it is read while it is up. It sits above
	// the other dialogs in the stack, so it is resolved before them.
	if ui.LocaleEditDialog {
		HandleLocaleEditInput(ui, gtx)
		return
	}
	if ui.SettingsDialog {
		for {
			if _, ok := ui.SettingsGameEditor.Update(gtx); !ok {
				break
			}
		}
		for {
			if _, ok := ui.SettingsFormatEditor.Update(gtx); !ok {
				break
			}
		}
		if ui.SettingsThemeClick.Clicked(gtx) {
			ui.DarkTheme = !ui.DarkTheme
			applyMaterialTheme(ui.Th, ui.DarkTheme)
			MarkStateDirty(ui)
		}
		for i := range ui.AssocClicks {
			if ui.AssocClicks[i].Clicked(gtx) {
				ToggleAssociation(ui, i)
			}
		}
		if ui.SettingsOk.Clicked(gtx) {
			ui.GamePath = strings.TrimSpace(ui.SettingsGameEditor.Text())
			// The format list is read here rather than as it is typed,
			// so a half-finished entry never changes what a click opens.
			ui.TextEditorFormats = parseTextFormats(ui.SettingsFormatEditor.Text())
			// A new game folder means a different set of .ucs files, so the
			// language list and the string table are both rebuilt from it.
			RefreshLocales(ui)
			ui.SettingsDialog = false
			MarkStateDirty(ui)
		}
		if ui.SettingsCancel.Clicked(gtx) {
			ui.DarkTheme = ui.SettingsThemeOriginal
			ui.GamePath = ui.SettingsGameOriginal
			ui.TextEditorFormats = parseTextFormats(ui.SettingsFormatOriginal)
			applyMaterialTheme(ui.Th, ui.DarkTheme)
			ui.SettingsDialog = false
		}
		return
	}
	// The file dialog is modal and is shared by Open Module, the timeline
	// import and the sound field, so it answers before the rest of the input.
	if ui.FilePickOpen {
		HandleFilePicker(ui, gtx)
		return
	}
	if ui.ArchiveDialog {
		HandleArchiveDialogInput(ui, gtx)
		return
	}
	// The event picker is modal over the Events section the same way.
	if ui.EvtPick.Open {
		HandleEvtPickerInput(ui, gtx)
		return
	}
	// The object interface is a tab, so its pickers and rows are part of the
	// ordinary input path rather than a modal branch that swallows everything.
	// The tool button that opens it is polled on every frame, which is why it
	// runs before the rest.
	// The unit screen names are read in the background and arrive in batches.
	// Nothing else keeps the window repainting, so this has to ask for the
	// redraw itself or a list would sit on file names until the mouse moved.
	if PollUnitNames(ui) {
		gtx.Execute(op.InvalidateCmd{})
	}
	// The squads a unit belongs to are indexed in the background for the same
	// reason: the index is built by walking 431 squad files, and nothing else
	// would repaint the window when the results arrived.
	if PollUnitSquads(ui) {
		gtx.Execute(op.InvalidateCmd{})
	}
	// The references between a mod's units, buildings, weapons and abilities
	// are read in the background too, and for the same reason: reading six
	// hundred files takes seconds, and a relationships list that filled in
	// quietly would otherwise sit half-empty until the mouse moved.
	if PollGraphLinks(ui) {
		gtx.Execute(op.InvalidateCmd{})
	}
	HandleUnitToolInput(ui, gtx)
	if UnitTabActive(ui) {
		HandleUnitTabInput(ui, gtx)
	}
	// The language picker hangs off the menu bar like the menus and the search
	// field, and it puts both of them away when it opens, so it is polled first
	// and leaves the rest of the input alone when it is closed.
	HandleLocaleInput(ui, gtx)
	// The tab bar's wheel and right click, and then whatever the context menu
	// they opened wants. Both come before the tab clicks so a right press on a
	// tab is not also read as selecting it.
	HandleTabBarInput(ui, gtx)
	HandleTabContextMenuInput(ui, gtx)
	// An Alt+click on a resolved string was seen while the row was laid out;
	// opening the .ucs is what the next frame does with it.
	if ui.LocalePending != "" {
		id := ui.LocalePending
		ui.LocalePending = ""
		JumpToLocaleEntry(ui, id)
	}
	// Alt+Ctrl on a localised row, or a click on one with no text, was seen the
	// same way. The request is acted on here because putting a dialog up is the
	// input pass's business, not a layout's.
	if req := ui.LocalePendingRequest; req.Action != localeRowNothing {
		ui.LocalePendingRequest = localeEditRequest{}
		switch req.Action {
		case localeRowJump:
			JumpToLocaleEntry(ui, req.Id)
		case localeRowEdit, localeRowCreate:
			// A press in the file view knows its own entry; one in the object
			// window has to be looked up among the unit's rows.
			field := ui.LocalePendingField
			if field == nil {
				if d := CurrentUnitDetails(ui); d != nil {
					field = localeFieldForID(d, req.Id)
				}
			}
			fieldKey := ui.LocalePendingFieldKey
			if fieldKey == "" && field != nil {
				fieldKey = localeRowKey(field, "")
			}
			ui.LocalePendingField = nil
			ui.LocalePendingFieldKey = ""
			OpenLocaleEdit(ui, req.Id, req.Action, field, fieldKey)
		}
	}
	// The search field owns the arrow and Enter keys while it is open, so it
	// has to run before the tree's keyboard handling.
	HandleSearchInput(ui, gtx)
	HandleKeyboard(ui, gtx)

	for i := range ui.MenuClicks {
		if ui.MenuClicks[i].Clicked(gtx) {
			CloseFileContextMenu(ui)
			// A menu and the search panel both hang off the menu bar, so
			// opening a menu puts the search away.
			CloseSearch(ui)
			if ui.MenuOpen == i {
				ui.MenuOpen = -1
			} else {
				ui.MenuOpen = i
			}
			ui.RecentMenuExpanded = false
		}
	}
	if ui.MenuOpen >= 0 {
		items := MenuItemsForOpen(ui)
		EnsureMenuItemClicks(ui, ui.MenuOpen, len(items))
		for j := range ui.MenuItemClicks[ui.MenuOpen] {
			if ui.MenuItemClicks[ui.MenuOpen][j].Clicked(gtx) {
				if items[j] != "—" {
					label := items[j]
					if ui.MenuOpen == 0 && label == "Recent Modules" {
						ui.RecentMenuExpanded = !ui.RecentMenuExpanded
						continue
					}
					if ui.MenuOpen == 0 && ui.RecentMenuExpanded {
						header := recentModulesHeaderIndex()
						nameIndex := j - header - 2
						if header >= 0 && nameIndex >= 0 && nameIndex < len(ui.RecentModules) {
							LoadModule(ui, ui.RecentModules[nameIndex])
							ui.MenuOpen = -1
							ui.RecentMenuExpanded = false
							break
						}
					}
					if ui.MenuOpen == 0 && isEmptyRecycleBinMenuLabel(label) {
						ui.ClearRecycleDialog = true
					} else if ui.MenuOpen == 0 {
						switch label {
						case "Open Module...":
							OpenModulePicker(ui)
						case "Recent Modules":
							ui.RecentMenuExpanded = !ui.RecentMenuExpanded
							continue
						case "View Recycle Bin":
							if err := openRecycleFolder(); err != nil {
								ui.StatusText = fmt.Sprintf("Open Recycle Bin failed: %v", err)
							} else {
								ui.StatusText = "Opened Recycle Bin"
							}
						case "Settings":
							OpenSettingsDialog(ui)
						case "Close":
							StopModFileWatcher(ui)
							CloseAllFileTabs(ui)
							if ui.ModTree != nil {
								ui.ModTree.Close()
								ui.ModTree = nil
								ui.ModulePath = ""
								ui.VisibleNodes = nil
								ui.Expanded = map[string]bool{"Data": true}
								ui.SelectedPath = ""
								ui.StatusText = "No module loaded"
								SyncWindowTitle(ui)
							}
						case "Associate .module files":
							if err := registerModuleAssociation(); err != nil {
								ui.StatusText = fmt.Sprintf("Associate failed: %v", err)
							} else {
								ui.StatusText = fmt.Sprintf("Associated .module with %s — double-click any .module to open", getExePath())
							}
						case "Remove .module association":
							if err := unregisterModuleAssociation(); err != nil {
								ui.StatusText = fmt.Sprintf("Remove failed: %v", err)
							} else {
								ui.StatusText = "Removed .module association (HKCU)"
							}
						case "Exit":
							exitLogged(0)
						default:
							ui.StatusText = fmt.Sprintf("Menu: %s", label)
						}
					} else if ui.MenuOpen == 1 {
						switch label {
						case "Save":
							if SaveEligible(ui) {
								HandleSaveActiveTab(ui)
							} else if tab := ActiveTab(ui); tab != nil && tab.Kind == "sgb" && tab.SgbMap != nil {
								HandleSgbSave(ui, tab)
							} else {
								ui.StatusText = "Save — no unsaved changes"
							}
						case "Export File":
							if eligible, reason := ExportEligible(ui, ActiveTab(ui)); eligible {
								HandleExportActiveTab(ui)
							} else {
								ui.StatusText = "Export — " + reason
							}
						case "Pack SGA...":
							PackCurrentModSGA(ui)
						case "Extract SGA...":
							OpenArchiveDialog(ui)
						default:
							ui.StatusText = fmt.Sprintf("Mod menu: %s", label)
						}
					} else if ui.MenuOpen == 2 {
						switch label {
						case "Open log folder":
							if err := openLogFolder(); err != nil {
								ui.StatusText = fmt.Sprintf("Open log folder failed: %v — log=%s", err, logDir())
							} else {
								ui.StatusText = fmt.Sprintf("Opened log folder: %s", logDir())
							}
						default:
							ui.StatusText = fmt.Sprintf("Menu: %s → %s", menus[ui.MenuOpen].name, label)
						}
					} else {
						ui.StatusText = fmt.Sprintf("Menu: %s → %s", menus[ui.MenuOpen].name, label)
					}
					ui.MenuOpen = -1
				}
				// The click handler closes the menu, so stop this loop before
				// the next iteration indexes menuItemClicks with menuOpen == -1.
				break
			}
		}
		if ui.DismissClick.Clicked(gtx) {
			ui.MenuOpen = -1
		}
	}
	// The language list, the search panel and the menus all hang off the menu
	// bar, and only one of them can own the space under it. Opening the list
	// puts the other two away when it happens, so this is the other half of
	// that rule, run after both of them have been resolved.
	CloseLocaleUnderOtherMenuBarPopups(ui)
	EnsureTabClicks(ui)
	for i := range ui.TabClicks {
		if ui.TabClicks[i].Clicked(gtx) {
			ui.TabSel = i
			MarkStateDirty(ui)
		}
	}
	for idx := range ui.TabCloses {
		if ui.TabCloses[idx].Clicked(gtx) {
			CloseTabAt(ui, idx)
			MarkStateDirty(ui)
			break
		}
	}
	if FdaActive(ui) {
		tab := ActiveTab(ui)
		CheckFdaAutoRepeat(ui)
		if tab.FdaPlayPause.Clicked(gtx) {
			FdaTogglePlayPause(ui)
		}
		if tab.FdaBack10.Clicked(gtx) {
			FdaSeekBy(ui, -10*time.Second)
		}
		if tab.FdaFwd10.Clicked(gtx) {
			FdaSeekBy(ui, 10*time.Second)
		}
		if tab.FdaBack30.Clicked(gtx) {
			FdaSeekBy(ui, -30*time.Second)
		}
		if tab.FdaFwd30.Clicked(gtx) {
			FdaSeekBy(ui, 30*time.Second)
		}
		if tab.FdaRepeatBtn.Clicked(gtx) {
			tab.FdaRepeat = !tab.FdaRepeat
			ui.StatusText = fmt.Sprintf("Repeat: %v", tab.FdaRepeat)
		}
		if tab.FdaExportBtn.Clicked(gtx) {
			FdaExportWav(ui)
		}
		if tab.FdaStopBtn.Clicked(gtx) {
			if tab.FdaPlayer != nil {
				tab.FdaPlayer.PauseAndStopReading()
				_, _ = tab.FdaPlayer.Seek(0, io.SeekStart)
				tab.FdaPaused = true
				ui.StatusText = "Stopped"
			}
		}
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "wav" && tab.WavPlayer != nil {
		CheckWavAutoRepeat(ui)
		if tab.WavPlayClick.Clicked(gtx) {
			WavTogglePlayPause(ui)
		}
		if tab.WavStopClick.Clicked(gtx) {
			WavStop(ui)
		}
		if tab.WavRepeatClick.Clicked(gtx) {
			tab.WavRepeat = !tab.WavRepeat
			ui.StatusText = fmt.Sprintf("WAV Repeat: %v", tab.WavRepeat)
		}
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "whm" {
		if tab.WhmConvertClick.Clicked(gtx) {
			ConvertWhmToSgmFile(ui, tab)
		}
	}
	if RgdActive(ui) {
		FocusClickedRgdEditor(ui, gtx)
		RgdSyncAndApply(ui, gtx)
	}
	if TextActive(ui) {
		TextSyncAndApply(ui, gtx)
	}
	nodes := GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(nodes))
	EnsureRowHover(ui, len(nodes))
	HandleTreePointerInput(ui, gtx, nodes)
	if ui.ContextMenuVisible {
		if ui.ContextDuplicate.Clicked(gtx) {
			path := ui.ContextMenuPath
			CloseFileContextMenu(ui)
			if err := DuplicateCurrentModFile(ui, path); err != nil {
				ui.StatusText = fmt.Sprintf("Duplicate failed: %v", err)
			}
		} else if ui.ContextDelete.Clicked(gtx) && CanDeleteCurrentModFile(ui, ui.ContextMenuPath) {
			ui.FileDeletePath = ui.ContextMenuPath
			ui.FileDeleteDialog = true
			CloseFileContextMenu(ui)
		} else if ui.ContextDismissClick.Clicked(gtx) {
			CloseFileContextMenu(ui)
		}
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "rgd" {
		selBefore := tab.RgdSelected
		if len(tab.RgdMidNodes) == 0 {
			RebuildRgdMidNodesFor(ui, tab)
			EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
		}
		midNodes := tab.RgdMidNodes
		EnsureRgdMidClicks(ui, tab, len(midNodes))
		clickedAny := false
		for i, m := range midNodes {
			if tab.RgdMidClicks[i].Clicked(gtx) {
				ui.KeyboardArea = "rgd"
				if m.IsTable && m.HasKids {
					if tab.RgdExpanded[m.Path] {
						delete(tab.RgdExpanded, m.Path)
					} else {
						tab.RgdExpanded[m.Path] = true
					}
				}
				if m.Path != selBefore {
					RgdFlushPending(ui, tab)
				}
				tab.RgdSelected = m.Path
				clickedAny = true
				MarkStateDirty(ui)
			}
		}
		if clickedAny {
			RebuildRgdMidNodesFor(ui, tab)
			EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
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

// HeldModifiers lives in uikit; the alias keeps every call site as it was.
var heldModifiers = uikit.HeldModifiers
