package shell

import (
	"dow-mod-studio/internal/app/editors"
	"dow-mod-studio/internal/app/fileops"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/modtree"
	"fmt"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"image"
	"path/filepath"
	"strings"
)

// WheelNotchDelta lives in uikit; the alias keeps every call site as it was.
const WheelNotchDelta = uikit.WheelNotchDelta

// GetVisibleNodes lives in uikit; the alias keeps every call site as it was.
var GetVisibleNodes = uikit.GetVisibleNodes

func EnsureRowHover(ui *UiState, n int) {
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
func EnsureRowClicks(ui *UiState, n int) {
	if len(ui.RowClicks) < n {
		old := ui.RowClicks
		ui.RowClicks = make([]gesture.Click, n)
		copy(ui.RowClicks, old)
	} else if len(ui.RowClicks) > n {
		ui.RowClicks = ui.RowClicks[:n]
	}
}

// IsEnterKey reports whether a key name is the platform's Enter.
//
// Gio has two names for it and a platform uses only one of them. Windows maps
// VK_RETURN to key.NameReturn; X11 and macOS can produce key.NameEnter as well.
// Filtering on one name alone is why Enter did nothing on Windows while the
// arrow keys worked: they have one name each, Enter has two.
func IsEnterKey(name key.Name) bool {
	return name == key.NameReturn || name == key.NameEnter
}

// TreeNavKeyNames is the set of keys the tree lists respond to, with Enter
// spelled under both of its names. A key filter takes one name, so the loop
// runs once per name rather than trying to match both in a single filter.
var TreeNavKeyNames = []key.Name{
	key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow,
	key.NameReturn, key.NameEnter,
}

// TreeActionKeyNames are the keys that do something to the selected row rather
// than move the selection. Delete is the one a file list cannot do without, and
// it is the one the file menu offers today: both go through the same
// confirmation, so neither can delete a file by accident.
//
// Gio has no plain "Delete": the key that deletes forwards is
// NameDeleteForward, which is what Windows delivers for VK_DELETE.
var TreeActionKeyNames = []key.Name{key.NameDeleteForward}

func OpenFileContextMenu(ui *UiState, n modtree.VisibleNode, anchor int) {
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

func CloseFileContextMenu(ui *UiState) {
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
func DeleteSelectedTreeFile(ui *UiState) {
	if ui.FileDeleteDialog {
		return
	}
	path := ui.SelectedPath
	if path == "" {
		return
	}
	if !fileops.CanDeleteCurrentModFile(ui, path) {
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
func OpenSettingsDialog(ui *UiState) {
	ui.SettingsThemeOriginal = ui.DarkTheme
	ui.SettingsGameOriginal = ui.GamePath
	ui.SettingsGameEditor.SetText(ui.GamePath)
	ui.SettingsFormatOriginal = fileops.FormatTextFormats(ui.TextEditorFormats)
	ui.SettingsFormatEditor.SetText(ui.SettingsFormatOriginal)
	// The association buttons are one per format, and what a click would do
	// depends on the registry right now, so the rows are sized and read here.
	ui.AssocClicks = make([]widget.Clickable, len(fileops.AssocTypes))
	fileops.RefreshAssocStates(ui)
	ui.SettingsDialog = true
}

func FindRgdReferenceFile(ui *UiState, ref string) string {
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

func NavigateRgdTree(ui *UiState, name key.Name) {
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
		editors.RgdFlushPending(ui, tab)
	}
	tab.RgdSelected = newPath
	editors.RebuildRgdMidNodesFor(ui, tab)
	editors.EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
	tab.RgdMidList.ScrollTo(idx)
	MarkStateDirty(ui)
}

func TreeRowAt(ui *UiState, y float32) int {
	if ui.LeftRowHeightPx <= 0 {
		return -1
	}
	local := y + float32(ui.LeftList.Position.Offset)
	return ui.LeftList.Position.First + int(local/float32(ui.LeftRowHeightPx))
}

// handleTabBarInput is the tab bar's own pointer handling: the wheel slides the
// row sideways, and a right press opens the context menu on whichever tab it
// landed on. Both are read from one area covering the whole bar, which the
// layout registers inside a PassOp so the tabs underneath keep their clicks.
func HandleTabBarInput(ui *UiState, gtx layout.Context) {
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
			ui.TabScrollX += int(e.Scroll.Y * TabWheelStepPx)
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
func TabContextPoint(ui *UiState, gtx layout.Context, x float32) image.Point {
	return image.Pt(int(x), TabBarTop(gtx)+gtx.Dp(unit.Dp(TabBarHeightDp)))
}

// handleTabContextMenuInput acts on the tab context menu and puts it away again.
// Escape closes it, a click elsewhere closes it, and the one item closes the
// tabs.
func HandleTabContextMenuInput(ui *UiState, gtx layout.Context) {
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

func KindLabel(k modtree.SourceKind) string {
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
var HeldModifiers = uikit.HeldModifiers
