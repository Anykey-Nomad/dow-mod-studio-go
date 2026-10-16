package main

import (
	"context"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/ebitengine/oto/v3"
)

// ── colours — classic Win32 / Corsix palette ──────────────────────────────────

func rgb(hex uint32) color.NRGBA {
	return color.NRGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xFF}
}
func rgba(r, g, b, a uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: a} }

var (
	// Dark is the default application theme. Keep these in one palette so
	// panels, controls, and text remain visually consistent.
	cBg          = rgb(0x1E1E1E)
	cMenuBg      = rgb(0x252526)
	cMenuHover   = rgb(0x343B45)
	cMenuOpen    = rgb(0x234A63)
	cBorder      = rgb(0x555555)
	cBorderLight = rgb(0x414141)
	cToolbarBg   = rgb(0x2D2D30)
	cTabBarBg    = rgb(0x333333)
	cTabOff      = rgb(0x292929)
	cTabOn       = rgb(0x1E1E1E)
	cContentBg   = rgb(0x181818)
	cPanelBg     = rgb(0x252526)
	cHeaderBg    = rgb(0x2D2D30)
	cHeaderLine  = rgb(0x4A4A4A)
	cSelectBg    = rgb(0x264A63)
	// cMarkerHi outlines the marker picked in the markers list. Red, because
	// nothing else in the panel is drawn in it and the point is to find one
	// small thing on a model quickly.
	cMarkerHi = rgb(0xFF3020)
	cSelectBd = rgb(0x3C78A8)
	// cFocusBd marks the row the keyboard is on, which is not the same thing as
	// the row that is selected: a panel can keep the selection while the middle
	// tree has the keyboard.
	cFocusBd   = rgb(0x8FC7FF)
	cText      = rgb(0xE6E6E6)
	cTextDim   = rgb(0xA0A0A0)
	cSaveBg    = rgb(0x303236)
	cSaveBd    = rgb(0x555555)
	cSaveHover = rgb(0x3A4652)
	// cProblem marks a reference that names a file which is not there, which
	// is the one thing in the object window that is certainly wrong.
	cProblem = rgb(0xE06C75)

	// tree source colors (like Corsix frmFiles: ThisMod, OtherMod, Engine)
	cTreeThis   = rgb(0xE06C75) // reddish — this mod
	cTreeOther  = rgb(0x82AAFF) // bluish — foreign / RequiredMod
	cTreeEngine = rgb(0xB0B0B0) // grey — engine
)

func setColorTheme(dark bool) {
	if dark {
		cBg, cMenuBg, cMenuHover, cMenuOpen = rgb(0x1E1E1E), rgb(0x252526), rgb(0x343B45), rgb(0x234A63)
		cBorder, cBorderLight, cToolbarBg, cTabBarBg = rgb(0x555555), rgb(0x414141), rgb(0x2D2D30), rgb(0x333333)
		cTabOff, cTabOn, cContentBg, cPanelBg = rgb(0x292929), rgb(0x1E1E1E), rgb(0x181818), rgb(0x252526)
		cHeaderBg, cHeaderLine, cSelectBg, cSelectBd = rgb(0x2D2D30), rgb(0x4A4A4A), rgb(0x264A63), rgb(0x3C78A8)
		cFocusBd = rgb(0x8FC7FF)
		cText, cTextDim, cSaveBg, cSaveBd, cSaveHover = rgb(0xE6E6E6), rgb(0xA0A0A0), rgb(0x303236), rgb(0x555555), rgb(0x3A4652)
		cTreeThis, cTreeOther, cTreeEngine = rgb(0xE06C75), rgb(0x82AAFF), rgb(0xB0B0B0)
		return
	}
	cBg, cMenuBg, cMenuHover, cMenuOpen = rgb(0xECECEC), rgb(0xF0F0F0), rgb(0xE5F3FF), rgb(0xCCE8FF)
	cBorder, cBorderLight, cToolbarBg, cTabBarBg = rgb(0xA0A0A0), rgb(0xC8C8C8), rgb(0xE8E8E8), rgb(0xD4D0C8)
	cTabOff, cTabOn, cContentBg, cPanelBg = rgb(0xE0E0E0), rgb(0xFFFFFF), rgb(0xD4D0C8), rgb(0xFFFFFF)
	cHeaderBg, cHeaderLine, cSelectBg, cSelectBd = rgb(0xE8E8E8), rgb(0xB0B0B0), rgb(0xCCE8FF), rgb(0x99CCFF)
	cFocusBd = rgb(0x1A5FB4)
	cText, cTextDim, cSaveBg, cSaveBd, cSaveHover = rgb(0x000000), rgb(0x505050), rgb(0xE1E1E1), rgb(0xADADAD), rgb(0xE5F3FF)
	cTreeThis, cTreeOther, cTreeEngine = rgb(0x802020), rgb(0x202080), rgb(0x808080)
}

func applyMaterialTheme(th *material.Theme, dark bool) {
	setColorTheme(dark)
	if th == nil {
		return
	}
	th.Palette = material.Palette{
		Bg: cBg, Fg: cText, ContrastBg: cSaveBg, ContrastFg: cText,
	}
}

// ── menu model ────────────────────────────────────────────────────────────────

type menuDef struct {
	name  string
	items []string
}

var menus []menuDef

func init() {
	if runtime.GOOS == "windows" {
		menus = []menuDef{
			{name: "File", items: []string{"New Mod...", "Open Module...", "Close", "—", "Settings", "—", "Associate .module files", "Remove .module association", "—", "Recent Modules", "View Recycle Bin", "Empty Recycle Bin", "Exit"}},
			{name: "Mod", items: []string{"Save", "Export File", "—", "Pack SGA...", "Extract SGA...", "—", "Burn Mod", "Module Settings..."}},
			{name: "Help", items: []string{"About DoW Mod Studio", "Documentation", "—", "Open log folder", "Check for Updates"}},
		}
	} else {
		menus = []menuDef{
			{name: "File", items: []string{"New Mod...", "Open Module...", "Close", "—", "Settings", "—", "Recent Modules", "View Recycle Bin", "Empty Recycle Bin", "Exit"}},
			{name: "Mod", items: []string{"Save", "Export File", "—", "Pack SGA...", "Extract SGA...", "—", "Burn Mod", "Module Settings..."}},
			{name: "Help", items: []string{"About DoW Mod Studio", "Documentation", "—", "Open log folder", "Check for Updates"}},
		}
	}
}

// ── UI state ─────────────────────────────────────────────────────────────────

type uiState struct {
	// forceSoftRenderer keeps the 3D viewport on the software rasteriser.
	forceSoftRenderer bool

	th *material.Theme

	menuOpen       int
	menuClicks     []widget.Clickable
	menuItemClicks [][]widget.Clickable
	dismissClick   widget.Clickable   // transparent full-screen catcher
	tabSel         int                // 0=Module, 1..N=file tab (index+1)
	tabClicks      []widget.Clickable // len = 1+len(openTabs), [0]=Module
	tabCloses      []widget.Clickable // len = len(openTabs), per file tab close ×
	openTabs       []*fileTab

	// The tab row scrolls sideways under the wheel, so the bar can hold more
	// tabs than fit. tabScrollX is how far it is scrolled, in pixels, and it
	// is clamped every frame against the row's real width — the bar keeps no
	// idea of that width itself.
	tabScrollX int
	// tabBarWidth and tabRowWidth are what the clamp above needs, and the
	// input pass is the one that moves the offset — so the layout leaves the
	// numbers it measured behind for it. tabRowWidthPx is the row's width as
	// just measured, before it becomes next frame's tabRowWidth.
	tabBarWidth   int
	tabRowWidth   int
	tabRowWidthPx int
	// tabBarPointerTag is the whole bar as a pointer target. A press on it says
	// which tab was clicked by position, and a scroll on it moves the row; both
	// are recorded in the bar's own coordinates, which is what pointer.Event
	// reports for a target.
	tabBarPointerTag event.Tag
	// tabRects is where each tab was last drawn, in bar coordinates, so a
	// right press can be resolved to a tab without a clickable per tab that
	// only exists for this.
	tabRects []image.Rectangle
	// The tab context menu. tabContextPos is where it opens, in window
	// coordinates, and tabContextIndex is the tab it was opened on. Only
	// "close all tabs" is in it for now, and the item list is a slice so
	// another one is a line here and a row in layoutTabContextMenu.
	tabContextVisible  bool
	tabContextIndex    int
	tabContextPos      image.Point
	tabContextDismiss  widget.Clickable
	tabContextCloseAll widget.Clickable

	imageCache      map[string]imageCacheEntry
	imageCacheBytes int64
	imageCacheSeq   uint64

	// modelGpu is the hardware renderer for the 3D preview, or nil when the
	// window's backend cannot provide one and the software renderer is used.
	// It is opened on first use because Gio creates the window's GPU with the
	// first frame.
	modelGpu      modelGpuHandle
	modelGpuTried bool

	// mod tree
	modTree          *modtree.ModTree
	expanded         map[string]bool
	selectedPath     string
	selectedTreePath string
	keyboardArea     string
	visibleNodes     []modtree.VisibleNode
	// rowClicks is one gesture per visible row, in the order of visibleNodes.
	// A row's click is Gio's to decide: the press and the release that belong
	// to one row, and nothing the app has to remember in between.
	rowClicks []gesture.Click
	// leftTreePointerTag is the tree's one area of its own, over the whole
	// list. A row's gesture cannot see a right press — a click is the primary
	// button by definition — so the file menu still comes from here.
	leftTreePointerTag event.Tag
	leftRowHeightPx    int
	rowHover           []bool
	lastTreeClickPath  string
	lastTreeClickAt    time.Time
	leftList           layout.List
	statusText         string
	modulePath         string
	gamePath           string
	// textEditorFormats are the file extensions a click in the tree or an
	// Enter opens in the built-in text editor, lower case and each with a
	// leading dot. It is a setting rather than a constant because a module
	// can carry scripts of its own naming, and Settings lets the list be
	// extended without a rebuild.
	textEditorFormats  []string
	recentFolders      []string
	recentModules      []string
	recentMenuExpanded bool
	stateDirty         bool
	lastStateSave      time.Time
	lastModPoll        time.Time
	modSnapshot        string
	modRefreshCh       chan modCatalogRefreshResult
	modRefreshRunning  bool
	modFileWatcher     *fsnotify.Watcher
	modFileWatchSignal chan struct{}

	leftPanelWidth  int
	rightPanelWidth int
	panelSplitters  [2]panelSplitterState
	leftScroll      panelScrollState
	middleScroll    panelScrollState
	rightScroll     panelScrollState

	// The file dialog every caller shares: Open Module, the timeline import
	// and the event editor's sound field. filePickOpen says it is up.
	filePick     filePicker
	filePickOpen bool

	settingsDialog        bool
	settingsThemeOriginal bool
	settingsGameOriginal  string
	settingsGameEditor    widget.Editor
	// settingsFormatEditor holds the list of file formats the built-in
	// text editor opens, as a person would type it. It is filled from
	// textEditorFormats when the dialog opens and read back on OK, so a
	// cancelled dialog changes nothing.
	settingsFormatEditor   widget.Editor
	settingsFormatOriginal string
	settingsThemeClick     widget.Clickable
	settingsOk             widget.Clickable
	settingsCancel         widget.Clickable
	darkTheme              bool

	archiveDialog       bool
	archiveOptions      []archiveOption
	archiveSelected     []bool
	archiveSelectClicks []widget.Clickable
	archiveList         layout.List
	archiveStatus       string
	archiveExtractClick widget.Clickable
	archiveCloseClick   widget.Clickable

	// left-panel file context menu and recycle-bin confirmation
	contextMenuPath     string
	contextMenuAnchor   int
	contextMenuVisible  bool
	contextDismissClick widget.Clickable
	contextDuplicate    widget.Clickable
	contextDelete       widget.Clickable
	fileDeleteDialog    bool
	fileDeletePath      string
	fileDeleteConfirm   widget.Clickable
	fileDeleteCancel    widget.Clickable
	clearRecycleDialog  bool
	clearRecycleConfirm widget.Clickable
	clearRecycleCancel  widget.Clickable

	// window ref for title update (set in run)
	win *app.Window

	// Hover tooltip for RGD values whose visible name comes from a numeric alias.
	rgdTooltipTags  map[string]event.Tag
	rgdTooltipText  string
	rgdEditorClicks map[*widget.Editor]*rgdEditorClickState

	// ── file search (top-right of the menu bar) ──
	// The field lives in the menu bar, but the results drop down as a panel
	// over the right side of the window so the tree stays visible underneath.
	searchVisible    bool
	searchEditor     widget.Editor
	searchFieldClick widget.Clickable
	searchDismiss    widget.Clickable
	searchKind       modtree.SearchKind
	searchKindClicks []widget.Clickable
	searchHits       []modtree.SearchHit
	searchHitClicks  []widget.Clickable
	searchHover      []bool
	searchSel        int
	searchStatus     string
	searchList       layout.List
	searchScroll     panelScrollState
	// lastSearchQuery/lastSearchKind remember what the visible results were
	// built from, so the walk over the whole tree only runs when they change.
	lastSearchQuery string
	lastSearchKind  modtree.SearchKind
	searchQuerySet  bool
	// searchHeldQuery is the query a dismissed search was dismissed at. The
	// panel reopens only when the field's text actually differs from it, so a
	// dismissed search stays down while its query is kept, and a stray editor
	// event cannot spring it back open over the content.
	searchHeldQuery string

	// ── localisation ──
	// locale is the language "$id" values are resolved in. The table behind
	// it is built by setLocale, which the dropdown calls on every pick; the
	// field in the menu bar is the only other reader.
	locale        string
	localeList    []string
	localeOpen    bool
	localeTrigger widget.Clickable
	localeDismiss widget.Clickable
	localeClicks  []widget.Clickable
	// localeDropBox is where the open list was last drawn, in window
	// coordinates. Nothing draws from it; a test aims a pixel probe at the
	// rows with it.
	localeDropBox image.Rectangle
	// localePending is the id an Alt+click asked for. The press is seen while
	// the row is laid out, and opening a tab belongs to the input pass, so the
	// id is left here for the next frame to act on.
	localePending string
	// localePendingRequest is the same arrangement for a press that wants the
	// edit dialog rather than a tab. The field and its key come with it, because
	// a press in the file view already knows which entry it was on and one in
	// the object window has to be looked up again.
	localePendingRequest  localeEditRequest
	localePendingField    *rgd.RgdEntry
	localePendingFieldKey string
	// localeEditTags is the pointer target of every localised row, keyed by the
	// field it belongs to: a row can be drawn more than once and both drawings
	// have to be the same target.
	localeEditTags map[string]event.Tag
	// The localisation editor. localeEditID is empty while a new string is
	// being written, and localeEditCreate says to allocate a free id on save
	// rather than change an existing entry.
	localeEditDialog   bool
	localeEditID       string
	localeEditCreate   bool
	localeEditEditor   widget.Editor
	localeEditError    string
	localeEditSave     widget.Clickable
	localeEditCancel   widget.Clickable
	localeEditField    *rgd.RgdEntry
	localeEditFieldKey string

	// ── unit / building object interface ──
	// The tool panel sits in the menu bar left of the search field; the button
	// opens the object interface as a tab of its own, which takes the space of
	// the file tree and the middle panel.
	unitSection       int
	unitToolClicks    []widget.Clickable
	unitSectionClicks []widget.Clickable

	// Classic OE section state: which of the classic editor's header panels
	// are open, their click targets, the scroll of the panel column, and the
	// clip list's own click targets — separate from the Viewport section's, so
	// the two lists cannot both read one press.
	classicOEOpen       []bool
	classicOEClicks     []widget.Clickable
	classicOEScroll     panelScrollState
	classicOEAnimClicks []widget.Clickable

	// The dialog browses the mod's own units rather than the open file, so it
	// carries its own selection. unitCatalog is built once per mod; the
	// indexes into it are what the pickers and the columns work from, and are
	// clamped rather than trusted, so a stale index after a mod change cannot
	// panic the layout.
	unitCatalog    *unitCatalog
	unitCatalogMod string
	// modGraph is the same tree read as a graph: every .rgd of Data\attrib
	// as an entity, and every reference a unit or building holds as a link.
	// It is built once per mod and read from the background scans, so it is
	// cached the way the catalogue is and dropped with it.
	modGraph    *modGraph
	modGraphMod string
	// graphLinksCh carries the background scan's batches of references,
	// graphLinksStop ends a scan, graphLinksRunning says one is going, and
	// graphLinksGraph is which graph it is writing into.
	graphLinksCh      chan graphLinkBatch
	graphLinksStop    chan struct{}
	graphLinksRunning bool
	graphLinksGraph   *modGraph
	unitRaceSel       int
	unitSel           int
	// unitPickRace and unitPickPath are the Object window's pickers as the
	// last session left them, waiting for a catalog to be resolved against.
	// They are consumed the first time the catalog is built and are empty
	// after that, so a mod opened later in the session is not dragged back to
	// a unit from the one before it.
	unitPickRace string
	unitPickPath string
	// unitNames maps a unit's lower cased virtual path to the localisation id
	// of its screen name, filled in the background by startUnitNameScan. The
	// text itself is never cached here: it is read from the locale table on the
	// UI thread, so changing the language relabels the lists for free.
	unitNames map[string]string
	// unitNamesCh carries the scan's batches, unitNamesStop ends a scan that a
	// module reload has made pointless, and unitNamesTree says which tree the
	// running scan belongs to.
	unitNamesCh      chan unitNameBatch
	unitNamesStop    chan struct{}
	unitNamesTree    *modtree.ModTree
	unitNamesRunning bool
	unitNamesDone    bool
	// unitDetails caches the parsed unit so switching sections does not re-read
	// the file, and unitDetailsPath says which unit it belongs to.
	unitDetails     *unitDetails
	unitDetailsPath string
	// unitIcons caches the decoded object icons of the open mod, keyed by the
	// unit's tree path, and unitIconsMod says which mod they belong to — the
	// same pairing unitDetails uses, for the same reason.
	unitIcons    map[string]iconCacheEntry
	unitIconsMod string
	// unitInfoList and unitInfoScroll are the info column's scroll state. The
	// column holds a hundred scalars rather than the dozen it started with, so
	// it scrolls like the other lists in the app.
	unitInfoList   layout.List
	unitInfoScroll panelScrollState
	// unitSquads maps a unit's lower cased tree path to the squads it is part
	// of. The link only exists from the squad's side, so it is built by walking
	// every squad .rgd in the background: unitSquadsCh carries the batches,
	// unitSquadsStop ends a scan a module reload has made pointless, and
	// unitSquadsTree says which tree the running scan belongs to.
	unitSquads        map[string][]unitSquadRef
	unitSquadsCh      chan unitSquadBatch
	unitSquadsStop    chan struct{}
	unitSquadsTree    *modtree.ModTree
	unitSquadsRunning bool
	unitSquadsDone    bool
	// unitSquadList and unitSquadScroll are the Squads column's scroll state.
	unitSquadList   layout.List
	unitSquadScroll panelScrollState
	// picker state: whether the race or unit dropdown is open, and the click
	// lists of the dropdown rows.
	unitRaceOpen   bool
	unitListOpen   bool
	unitRaceClicks []widget.Clickable
	unitClicks     []widget.Clickable
	unitList       layout.List
	unitListScroll panelScrollState
	// unitRaceTrigger and unitListTrigger are the two dropdown fields.
	unitRaceTrigger widget.Clickable
	unitListTrigger widget.Clickable
	// unitNewTrigger and unitBuildingTrigger are the buttons that start a new
	// unit or a new building of the selected race, and the fields of the dialog
	// they open.
	unitNewTrigger      widget.Clickable
	unitBuildingTrigger widget.Clickable
	// unitFreezeTrigger and unitUnfreezeTrigger move the selected object out
	// of the mod and back into it.
	unitFreezeTrigger   widget.Clickable
	unitUnfreezeTrigger widget.Clickable
	// freezeBook is the mod's record of what has been frozen, kept because the
	// object window asks about it on every frame and it only changes when a
	// freeze does.
	freezeBook         *freezeBook
	freezeBookMod      string
	newObjKind         newObjKind
	newObjDialog       bool
	newObjNameEditor   widget.Editor
	newObjOk           widget.Clickable
	newObjCancel       widget.Clickable
	newObjStatus       string
	newObjDismissClick widget.Clickable
	// newObjCardBox is where the new unit dialog's card was last drawn, in
	// window coordinates. Nothing draws from it; it is recorded so a test can
	// aim a pixel probe at the card's buttons instead of guessing.
	newObjCardBox image.Rectangle
	// unitDismissClick is a transparent area over the columns, laid out only
	// while a dropdown is open, so a click that misses both the strip and the
	// list puts it away.
	unitDismissClick widget.Clickable
	// unitDropBox is where the open dropdown was last drawn, in the content
	// area's own coordinates. Nothing draws from it; it is recorded so a test
	// can aim a pixel probe at the list instead of guessing where it is.
	unitDropBox image.Rectangle
	// unitSelRow is which row of the weapons or abilities list is selected, and
	// unitRowClicks holds that list's click targets.
	unitSelRow    int
	unitRowClicks []widget.Clickable
	// unitInfoSel is which row of the info list the right column is editing,
	// unitInfoClicks holds that list's click targets, and unitInfoDirty says
	// the parsed unit has been changed in memory and not yet written back. The
	// one editor and one toggle are bound to whichever field is selected:
	// unitValBound is the field they are showing, unitValError the last refusal.
	unitInfoSel    int
	unitInfoClicks []widget.Clickable
	unitInfoDirty  bool
	unitValEditor  widget.Editor
	unitValBound   string
	unitValError   string
	unitBoolClick  widget.Clickable
	// unitSaveClick, unitRevertClick and unitOpenRgdClick are the three buttons
	// under the value.
	unitSaveClick    widget.Clickable
	unitRevertClick  widget.Clickable
	unitOpenRgdClick widget.Clickable

	// unitModel is the 3D preview of the object window's Viewport section. It
	// is a fileTab of its own rather than a field of the object tab, because
	// the preview is a per unit thing and the object tab is a singleton: the
	// same viewer a .whm tab uses, driven from here.
	unitModel     *fileTab
	unitModelCh   chan unitModelResult
	unitModelFor  string // the unit .rgd the loaded model belongs to
	unitModelPath string // the .whm that was found, empty when there is none
	unitModelErr  string // why no model is on screen
	// unitAnimClicks and unitCamClicks are the animation and camera rows of
	// the viewport's side panel.
	unitAnimClicks []widget.Clickable
	unitCamClicks  []widget.Clickable
	// unitMarkerClicks are the rows of the markers list, unitBoneClicks the
	// rows of the bones list, and unitMarkerListToggle the button that swaps
	// one list for the next: all three are lists of the same model's
	// contents and either may be the one being read, so one button between
	// them is enough.
	unitMarkerClicks  []widget.Clickable
	unitBoneClicks    []widget.Clickable
	unitFXListMode    bool
	unitFXClicks      []widget.Clickable
	unitListTabClicks [4]widget.Clickable
	// unitBoneKeyClicks are the rows of the currently picked bone's keyframes,
	// sized for the current bone whenever the bone list changes which bone is
	// picked.
	unitBoneKeyClicks []widget.Clickable
	// unitBoneKeySel is which of those rows are picked: keyed by the kind of
	// key and the frame it sits on, so taking a key out of the track shifts
	// every index after it without moving the selection off the rows it was
	// put on. unitBoneKeySelAnchor is the row a Shift+click extends from
	// (mode −1 when there is none) and unitBoneKeySelBound the clip and bone
	// the selection belongs to — another of either starts a fresh one rather
	// than carrying frames over to keys that are not there.
	unitBoneKeySel       map[boneKeyRef]bool
	unitBoneKeySelAnchor boneKeyRef
	unitBoneKeySelBound  string
	// A pointer press carries no usable modifiers in this panel, so Ctrl+click
	// and Shift+click read the key events unitListTrackModifiers keeps up to
	// date, ORed with the live OS state heldModifiers reports.
	unitBoneKeyCtrlHeld  bool
	unitBoneKeyShiftHeld bool
	unitBoneKeyModLog    string
	// unitBoneKeySetClick applies the typed fields to their target keys, and
	// unitBoneKeySetAllClick applies only the axes that were typed over to
	// every key of that kind the selected bone has in the clip.
	unitBoneKeySetClick    widget.Clickable
	unitBoneKeySetAllClick widget.Clickable
	// unitBoneKeyAddPosClick and unitBoneKeyAddRotClick give the picked bone
	// a key of that kind on the frame the playhead is on,
	// unitBoneKeyBeforeClick and unitBoneKeyAfterClick add one on the nearest
	// free frame either side of every picked row, and
	// unitBoneKeyDelClick takes the picked rows away: the five the pose bar
	// offers for editing which frames the clip carries for the bone.
	unitBoneKeyAddPosClick widget.Clickable
	unitBoneKeyAddRotClick widget.Clickable
	unitBoneKeyBeforeClick widget.Clickable
	unitBoneKeyAfterClick  widget.Clickable
	unitBoneKeyDelClick    widget.Clickable
	// unitBoneKeyFillText is the text each field was last filled with, so
	// "changed" is what was typed over rather than what parses differently.
	unitBoneKeyFillText [2][3]string
	// unitBonePosEd and unitBoneRotEd are the picked key's position and
	// rotation typed in by axis: Enter in a field applies the triple to the
	// key, and the rotation fields take degrees of an XYZ turn rather than
	// the quaternion the file stores.
	unitBonePosEd [3]widget.Editor
	unitBoneRotEd [3]widget.Editor
	// unitBoneMirClick are the three "mirror angle" buttons: each flips the
	// sign of the rotation about its axis (X, Y, Z) on the key the Rot
	// fields show.
	unitBoneMirClick [3]widget.Clickable
	// unitBoneAngleSlid are the three angle sliders above the mirror
	// buttons: each turns the rotation key the Rot fields show about
	// its own axis across the full -180..180 degree turn the fields
	// work in.
	unitBoneAngleSlid [3]widget.Float
	// unitBoneKeyShiftEd is how many frames the shift moves the picked keys
	// by, and unitBoneKeyShiftBack / unitBoneKeyShiftFwd move them back and
	// forward by that many frames.
	unitBoneKeyShiftEd   widget.Editor
	unitBoneKeyShiftBack widget.Clickable
	unitBoneKeyShiftFwd  widget.Clickable
	// unitBoneUndo is the stack of key-track snapshots taken before each
	// edit (add, delete, shift, mirror, set, slider), most recent last, so
	// Undo rolls back the last change to the picked bone's keys.
	// unitBoneKeyUndoClick is the Undo button. unitBoneSliderUndoArmed
	// marks that the current slider drag already has a snapshot, so a whole
	// drag is one undo step rather than one per frame.
	unitBoneUndo            []boneTrackSnapshot
	unitBoneKeyUndoClick    widget.Clickable
	unitBoneSliderUndoArmed bool
	// unitBoneKeyBound is the selection the fields were last filled from and
	// unitBoneKeyFrame the frame they were filled at: a change of selection
	// refills them whatever the cursor is doing, a change of frame alone
	// leaves a group being typed into alone. unitBoneKeyErr says why the last
	// apply or save did nothing, and is cleared by the next success or
	// selection change.
	unitBoneKeyBound string
	unitBoneKeyFrame int
	unitBoneKeyErr   string
	// unitBoneKeyBtnLog and unitBonePoseBarLog are diagnostics for dead
	// clicks: the first remembers the hover/press states of the pose bar's
	// eight buttons, the second whether the pose bar is laid out at all, so
	// the log shows on change only where a press stopped being noticed.
	unitBoneKeyBtnLog  [26]bool
	unitBonePoseBarLog string
	// unitBoneKeyFocusLog is which of the six value fields held the caret on
	// the last frame: typing and Enter both need it, so a caret that never
	// lands explains both at once.
	unitBoneKeyFocusLog [6]bool
	// unitBoneKeySaveClick writes the edited bone keys back into the .whm,
	// and unitModelWhmDirty says there are edits to write. The flag is per
	// unit viewport because that is where the bone keys can be edited.
	unitBoneKeySaveClick widget.Clickable
	unitModelWhmDirty    bool
	// unitListMode picks the list on show: 0 animations, 1 markers, 2 bones.
	unitListMode int
	// unitViewportScroll is the side panel's scrollbar, and unitOpenModelClick
	// opens the .whm in a tab of its own, which is the way to reach the
	// converter from the object window.
	unitViewportScroll panelScrollState
	unitOpenModelClick widget.Clickable

	// "Import frame's timeline": a keyframe clipboard read out of another
	// .whm. unitImpClick is the button in the pose bar, unitImpDialog the
	// file dialog it opens, and unitImpModel the source .whm once picked —
	// the section below the keyframe list browses its clips and frames.
	unitImpClick        widget.Clickable
	unitImpStatus       string
	unitImpModel        *WhmModel
	unitImpPath         string
	unitImpOpen         []bool // per source animation: expanded
	unitImpAnimClicks   []widget.Clickable
	unitImpFrameClicks  map[unitImpRef]*widget.Clickable
	unitImpSel          map[unitImpRef]bool
	unitImpAnchor       unitImpRef
	unitImpCopyClick    widget.Clickable
	unitImpReplaceClick widget.Clickable

	// Events section state: the selected model's .whe parsed into an editable
	// document, which carries the event definitions (DATAEVNT) and the
	// animation timelines (DATAMTON) that fire them at a moment in a clip.
	// unitEvtTree/unitEvtFor say which tree and which unit the document was
	// read for, so switching units or reloading a module re-reads it rather
	// than editing a stale copy.
	unitEvtTree  *modtree.ModTree
	unitEvtFor   string // the unit .rgd path the document was loaded for
	unitEvtPath  string // virtual path of the .whe being edited
	unitEvtDoc   *whe.Document
	unitEvtTried bool // this unit was already attempted, so a missing .whe is not re-read every frame
	unitEvtErr   string
	// unitEvtEntryEds are the value fields of the selected event's
	// definition, sized to its entries, and unitEvtEntryBound says which
	// definition they were last filled from. Enter in a field — or the Set
	// button, which does not depend on the field having focus — writes the
	// value into the in-memory document, and the viewport re-resolves from
	// the document so the edit shows before it is saved. unitEvtEntryFillText
	// is what each field showed when filled, so Set writes only what was
	// typed over.
	unitEvtEntryEds      []widget.Editor
	unitEvtEntryBound    string
	unitEvtEntryFillText []string
	unitEvtEntrySetClick widget.Clickable
	unitEvtDirty         bool
	unitEvtTimeline      string // selected timeline (DATAMTON) name
	unitEvtSel           int    // selected event row of that timeline
	unitEvtList          layout.List
	unitEvtScroll        panelScrollState
	unitEvtPanelScroll   panelScrollState
	unitEvtActionsScroll panelScrollState
	unitEvtActionSel     int // selected action of the DATAACTS list
	unitEvtActionClicks  []widget.Clickable
	// unitEvtFocusAction says the reader is working on an action rather than a
	// timeline: the panel then shows the picked action's conditions and none of
	// the timeline's events, since the two are one panel and not two stacked.
	unitEvtFocusAction bool
	unitEvtTlClicks    []widget.Clickable
	unitEvtClicks      []widget.Clickable
	// unitActCondEds are the condition fields of the picked action's choices —
	// one per motion and sub-action link, in file order — with
	// unitActCondStaged holding the guard step each is edited as and
	// unitActCondBound naming the action they were last filled from. Apply
	// writes them into the in-memory document and Save .whe writes that to
	// the file, the same two steps the event form takes.
	unitActCondEds     []widget.Editor
	unitActCondStaged  []int32
	unitActCondBound   string
	unitActGuardClicks []widget.Clickable
	unitActSel         int // the choice the chain tools act on: where the caret last was
	unitActApply       widget.Clickable
	// The chain tools add, move and delete choices with the shape of an
	// if / else if / else chain kept intact: they act on the document the
	// way Apply does, so every step is one Save .whe away.
	unitActAddIf     widget.Clickable
	unitActAddElseIf widget.Clickable
	unitActAddElse   widget.Clickable
	unitActMoveUp    widget.Clickable
	unitActMoveDown  widget.Clickable
	unitActDelete    widget.Clickable
	// unitActNameEds are the target-name fields beside the conditions — one
	// per choice — so a newly added line can name the motion or action it
	// runs.
	unitActNameEds []widget.Editor
	unitActErr     string
	// The add form: event name, moment, FX path, sound path and marker. The
	// FX and sound paths are separate fields: an effect and a sound name
	// different things.
	unitEvtNameEd   widget.Editor
	unitEvtTimeEd   widget.Editor
	unitEvtFxEd     widget.Editor
	unitEvtSndEd    widget.Editor
	unitEvtMarkerEd widget.Editor
	// evtPick is the list dialog an event field opens on a click.
	evtPick evtPicker
	// unitEvtBound is which timeline/row the editors were last filled from,
	// so a selection change refills them without touching what is being typed.
	unitEvtBound    string
	unitEvtFormErr  string
	unitEvtAddEvent widget.Clickable
	unitEvtAddFX    widget.Clickable
	unitEvtAddSound widget.Clickable
	unitEvtRemove   widget.Clickable
	// unitEvtSet and unitEvtApply are the form's two commit buttons: both
	// write the typed fields into the selected event in memory, and Save .whe
	// writes that to the file.
	unitEvtSet    widget.Clickable
	unitEvtApply  widget.Clickable
	unitEvtSave   widget.Clickable
	unitEvtRevert widget.Clickable
	// unitEvtSelSet is the whole selected group of event rows — Ctrl+click
	// toggles one, Shift+click takes the range from unitEvtSelAnchor — and it
	// is what Copy, Remove and the row highlight act on. unitEvtSel stays the
	// row whose definition and form are shown.
	unitEvtSelSet    map[int]bool
	unitEvtSelAnchor int
	// unitClips is the typed copy history shared by the copy buffers. Each
	// Copy adds an entry tagged with what it holds, and Paste takes the newest
	// entry that fits the field it is pressed in.
	unitClips    clipHistory
	unitEvtCopy  widget.Clickable
	unitEvtPaste widget.Clickable
	// unitClipCur is the history entry Paste takes when it fits the field; -1
	// when none is chosen. unitClipPrev and unitClipNext step it through the
	// entries that fit the events field.
	unitClipCur  int
	unitClipPrev widget.Clickable
	unitClipNext widget.Clickable
	// unitEvtCtrlHeld and unitEvtShiftHeld mirror the modifier keys while
	// this section is on screen. They are only as good as this section's
	// Ctrl/Shift filters winning those key events, so a click also reads the
	// live OS state through heldModifiers; unitEvtModLog is the dedupe key
	// that keeps the diagnostic line to one per distinct modifier event.
	unitEvtCtrlHeld  bool
	unitEvtShiftHeld bool
	unitEvtModLog    string
	// lastFormAction is the kind of the most recent operation the events form
	// attempted ("fx", "sound" or "remove"), recorded as it happens; the
	// commit of edited fields is applySelectedEventForm's job.
	lastFormAction string
	// evtPlayer is the Events section's audio: one event's sound auditioned
	// on its own, or the selected timeline's sounds played in order with a
	// playhead, pause/resume and a scrub strip. See unit_evt_play.go.
	evtPlayer unitEvtPlayer

	// projectDir and unitsDir are this mod's folders under the program's
	// Projects tree, created when the module was opened.
	projectDir string
	// projectStatusErr is why the project folder could not be made, held until
	// the status line can say so without the module load being interrupted.
	projectStatusErr error
	unitsDir         string
}

type rgdEditorClickState struct {
	editor *widget.Editor
	click  widget.Clickable
}

// fileTab holds per-opened-file state (RGD or FDA) — one tab per file in the mod tree.
type fileTab struct {
	path string // virtual path like Data\attrib\... or Data\sound\....fda
	kind string // "rgd", "text", or "fda" (lowercase ext without dot)

	// rgd state (when kind=="rgd")
	rgdFile             *rgd.RgdFile
	rgdError            string
	rgdExpanded         map[string]bool
	rgdSelected         string // e.g. "GameData", "GameData/health_ext"
	rgdMidList          layout.List
	rgdMidScroll        panelScrollState
	rgdMidNodes         []rgdVisible
	rgdMidClicks        []widget.Clickable
	rgdChildList        layout.List
	rgdChildScroll      panelScrollState
	rgdDirty            bool
	rgdRefEditor        widget.Editor
	rgdRefBound         string                        // which entry path the ref editor is bound to
	rgdValEditors       map[string]*widget.Editor     // key = RGD path; scalar values use the text editor
	rgdValErrors        map[string]string             // key = RGD path; invalid scalar text
	rgdValClicks        map[string]*widget.Clickable  // Bool toggles
	rgdSuggestionClicks map[string][]widget.Clickable // inline string suggestion choices
	rgdRefClick         widget.Clickable              // apply ref
	rgdApplyClick       widget.Clickable              // apply pending scalar value
	rgdBlank            widget.Clickable              // eats clicks so editor doesn't lose focus to tree

	// text state (when kind=="text": .rat, .con, .lua, .ai, and .ucs)
	textEditor   widget.Editor
	textOriginal string
	textDirty    bool
	textError    string
	textBytes    int
	// textLocale marks a tab that holds a game's .ucs file rather than a mod
	// file. Those live outside the mod tree, so there is nowhere to write an
	// edit back to; the tab is read only and says so.
	textLocale bool

	// rgdLocTags are the pointer targets of the Alt+click rows under a
	// DT_WString value, keyed the same way as rgdValEditors. They live here
	// because a press has to be caught on the row itself to see whether Alt
	// was held, and a click target cannot be reused across rows.
	rgdLocTags map[string]event.Tag

	// fda state (when kind=="fda")
	fdaFile        *fda.FDAFile
	fdaError       string
	fdaPcmBytes    []byte // decoded 16-bit LE interleaved
	fdaSampleRate  int
	fdaChannels    int
	fdaBytesPerSec int64
	fdaTotal       time.Duration
	fdaChunkBuf    *audioplayer.ChunkBuffer
	fdaSrc         *audioplayer.ChunkSource
	fdaPlayer      *oto.Player
	fdaPaused      bool
	fdaRepeat      bool
	fdaVolumePct   int // 0..200
	fdaExporting   bool
	fdaExportDone  string
	fdaSeekFloat   widget.Float
	fdaVolFloat    widget.Float
	fdaPlayPause   widget.Clickable
	fdaBack10      widget.Clickable
	fdaFwd10       widget.Clickable
	fdaBack30      widget.Clickable
	fdaFwd30       widget.Clickable
	fdaRepeatBtn   widget.Clickable
	fdaExportBtn   widget.Clickable
	fdaStopBtn     widget.Clickable

	// waveform (derived from fdaPcmBytes, lazy-built on demand)
	fdaWavePeaks     []byte // 0..255 per column, one byte per x-pixel at last build
	fdaWaveW         int    // width the peaks were built for
	fdaWaveClick     widget.Clickable
	fdaWaveFloat     widget.Float // optional: we use raw pointer drag instead, but keep for compat
	fdaWaveDragging  bool
	fdaWaveHoverFrac float32 // mouse hover frac for tooltip if needed

	// whm state (when kind=="whm")
	whmInfo         *whm.ModelInfo
	whmError        string
	whmBytes        int
	whmConverting   bool
	whmExportDone   string
	whmConvertClick widget.Clickable

	// 3D preview state (when kind=="whm" and a model was decoded)
	model             *modelViewer
	modelMaterialCh   chan modelMaterialResult
	modelListMode     int
	modelListClicks   [4]widget.Clickable
	modelListScroll   panelScrollState
	modelAnimClicks   []widget.Clickable
	modelMarkerClicks []widget.Clickable
	modelBoneClicks   []widget.Clickable
	modelFXClicks     []widget.Clickable
	modelPlay         widget.Clickable
	modelPrevAnim     widget.Clickable
	modelNextAnim     widget.Clickable
	modelStepBack     widget.Clickable
	modelStepFwd      widget.Clickable
	modelReset        widget.Clickable
	modelSkeleton     widget.Clickable
	modelFX           widget.Clickable
	// modelSFX turns the model's own sound on and off. It is a button rather
	// than nothing at all because a preview that starts making noise the
	// moment a model opens is a preview nobody can look at, and one that
	// cannot be stopped is worse than that.
	modelSFX      widget.Clickable
	modelGround   widget.Clickable
	modelTexture  widget.Clickable
	modelZoomIn   widget.Clickable
	modelZoomOut  widget.Clickable
	modelUVRotate widget.Clickable
	modelUVMirror widget.Clickable
	modelUVReset  widget.Clickable
	// image state (when kind=="image")
	imageData       image.Image
	imageMipmaps    []image.Image
	imageMipLevel   int
	imageError      string
	imageBytes      int
	imageFormat     string
	imageLoading    bool
	imageDecodeCh   chan imageDecodeResult
	imageDecodeStop context.CancelFunc
	imageFit        bool
	imageZoom       float32
	imageZoomIn     widget.Clickable
	imageZoomOut    widget.Clickable
	imageFitClick   widget.Clickable
	imageMipPrev    widget.Clickable
	imageMipNext    widget.Clickable

	// wav state (when kind=="wav")
	wavData         *wav.Info
	wavError        string
	wavExporting    bool
	wavExportDone   string
	wavConvertClick widget.Clickable
	wavPlayClick    widget.Clickable
	wavStopClick    widget.Clickable
	wavRepeatClick  widget.Clickable
	wavRepeat       bool
	wavVolumePct    int
	wavVolFloat     widget.Float
	wavSeekFloat    widget.Float
	wavWavePeaks    []byte
	wavWaveW        int
	wavPlayer       *oto.Player
	wavSrc          *audioplayer.ChunkSource
	wavPaused       bool
	wavBytesPerSec  int64
	wavTotal        time.Duration
}

type rgdVisible struct {
	Name     string
	Path     string
	Depth    int
	IsTable  bool
	HasKids  bool
	Expanded bool
	Selected bool
	Type     uint32
	Ref      string
	Entry    *rgd.RgdEntry
}

// pointerTag identifies a pointer target of the app's own making, as opposed
// to a widget's, which brings its own tag with it.
//
// A tag has to be a distinct pointer, and a zero sized one is not:
// new(struct{}) hands out runtime.zerobase every time, so every zero sized tag
// in a program compares equal. Two targets sharing a tag share one queue of
// events, and whichever handler asks first takes them: the tab bar's handler
// runs before the file tree's, so it took the tree's press, the tree's press
// never opened anything, and a right press in the tree opened the tab bar's
// menu instead of the file's. The name is only here to make the tags readable
// in a debugger.
func newPointerTag(name string) event.Tag {
	return &struct{ name string }{name: name}
}

func newUI(th *material.Theme) *uiState {
	s := &uiState{th: th, menuOpen: -1, tabSel: 0, darkTheme: true, keyboardArea: "files", imageCache: make(map[string]imageCacheEntry), modRefreshCh: make(chan modCatalogRefreshResult, 1), modFileWatchSignal: make(chan struct{}, 1), stateDirty: true, leftTreePointerTag: newPointerTag("left-tree"), tabBarPointerTag: newPointerTag("tab-bar"), locale: localeDefault}
	applyMaterialTheme(th, true)
	s.menuClicks = make([]widget.Clickable, len(menus))
	s.menuItemClicks = make([][]widget.Clickable, len(menus))
	for i := range menus {
		s.menuItemClicks[i] = make([]widget.Clickable, len(menus[i].items))
	}
	s.expanded = map[string]bool{"Data": true}
	s.newObjNameEditor.SingleLine = true
	s.newObjNameEditor.Submit = true
	s.settingsGameEditor.SingleLine = true
	s.settingsGameEditor.Submit = true
	s.settingsFormatEditor.SingleLine = true
	s.settingsFormatEditor.Submit = true
	// The frames field beside the shift buttons in the bone pose bar: a
	// whole number, with 1 standing in when it is empty or unreadable.
	s.unitBoneKeyShiftEd.SingleLine = true
	s.unitBoneKeyShiftEd.Submit = true
	s.unitBoneKeyShiftEd.SetText("1")
	// The formats in force before any state file is read, so a frame laid
	// out before the settings are applied already opens the built-in list.
	s.textEditorFormats = append([]string(nil), defaultTextFormats...)
	s.searchEditor.SingleLine = true
	s.searchEditor.Submit = true
	s.searchKind = modtree.SearchAll
	s.searchKindClicks = make([]widget.Clickable, len(modtree.SearchKinds))
	s.ensureUnitClicks()
	s.tabClicks = make([]widget.Clickable, 1) // Module only
	s.tabCloses = nil
	s.openTabs = nil
	return s
}

// ── tab helpers ─────────────────────────────────────────────────────────────

func (ui *uiState) ensureTabClicks() {
	need := 1 + len(ui.openTabs)
	if len(ui.tabClicks) < need {
		old := ui.tabClicks
		n := make([]widget.Clickable, need)
		copy(n, old)
		ui.tabClicks = n
	} else if len(ui.tabClicks) > need {
		// shrink handled explicitly in closeTabAt; if reached here via unexpected path, truncate preserving Module+prefix
		ui.tabClicks = ui.tabClicks[:need]
	}
	if len(ui.tabCloses) < len(ui.openTabs) {
		old := ui.tabCloses
		n := make([]widget.Clickable, len(ui.openTabs))
		copy(n, old)
		ui.tabCloses = n
	} else if len(ui.tabCloses) > len(ui.openTabs) {
		ui.tabCloses = ui.tabCloses[:len(ui.openTabs)]
	}
	if ui.tabSel < 0 {
		ui.tabSel = 0
	}
	if ui.tabSel > len(ui.openTabs) {
		ui.tabSel = len(ui.openTabs)
	}
}

func (ui *uiState) findTabIndex(path string) int {
	for i, t := range ui.openTabs {
		if t.path == path {
			return i
		}
	}
	return -1
}

func (ui *uiState) activeTab() *fileTab {
	if ui.tabSel <= 0 || ui.tabSel > len(ui.openTabs) {
		return nil
	}
	return ui.openTabs[ui.tabSel-1]
}

func (ui *uiState) isModuleActive() bool { return ui.tabSel == 0 }

func (ui *uiState) fdaActive() bool {
	t := ui.activeTab()
	if t == nil {
		return false
	}
	return t.kind == "fda" && (t.fdaFile != nil || t.fdaError != "")
}
func (ui *uiState) rgdActive() bool {
	t := ui.activeTab()
	if t == nil {
		return false
	}
	return t.kind == "rgd" && (t.rgdFile != nil || t.rgdError != "")
}

// releaseTabResources hands back everything a tab holds outside the tab list:
// a running decode, the 3D preview's buffers, any audio player, and the
// dropdowns that belong to the tab. Closing one tab and closing them all both
// go through it, so a tab can never be dropped with something of it still
// running — which is what used to happen when they were closed in bulk.
func (ui *uiState) releaseTabResources(t *fileTab) {
	if t == nil {
		return
	}
	// A dropdown belongs to the tab that is going away; leaving it open would
	// draw it over whatever takes its place.
	if isUnitTab(t) {
		ui.unitRaceOpen = false
		ui.unitListOpen = false
	}
	if t.imageDecodeStop != nil {
		t.imageDecodeStop()
		t.imageDecodeStop = nil
	}
	// The 3D preview renders on a worker; drop the reference so a pending
	// channel write cannot keep the model alive after the tab is gone.
	if t.model != nil {
		// The hardware renderer caches the model's buffers, which go back to
		// the driver along with the tab. Only an existing handle is asked, so
		// closing a tab never brings one up.
		if ui.modelGpu != nil && t.model.model != nil {
			ui.modelGpu.forget(t.model.model)
		}
		t.model = nil
	}
	if t.kind == "fda" && t.fdaPlayer != nil {
		t.fdaPlayer.PauseAndStopReading()
		t.fdaPlayer = nil
	}
	if t.kind == "wav" && t.wavPlayer != nil {
		t.wavPlayer.PauseAndStopReading()
		t.wavPlayer = nil
	}
}

func (ui *uiState) closeTabAt(idx int) {
	if idx < 0 || idx >= len(ui.openTabs) {
		return
	}
	t := ui.openTabs[idx]
	ui.releaseTabResources(t)
	oldSel := ui.tabSel
	ui.openTabs = append(ui.openTabs[:idx], ui.openTabs[idx+1:]...)
	// splice clickables to keep per-tab button state aligned
	// tabClicks: [0]=Module, [1]=tab0, [2]=tab1, ...
	if idx+1 < len(ui.tabClicks) {
		ui.tabClicks = append(ui.tabClicks[:idx+1], ui.tabClicks[idx+2:]...)
	}
	if idx < len(ui.tabCloses) {
		ui.tabCloses = append(ui.tabCloses[:idx], ui.tabCloses[idx+1:]...)
	}
	newLen := len(ui.openTabs)
	if oldSel == idx+1 {
		ui.tabSel = oldSel
		if ui.tabSel > newLen {
			ui.tabSel = newLen
		}
	} else if oldSel > idx+1 {
		ui.tabSel = oldSel - 1
		if ui.tabSel > newLen {
			ui.tabSel = newLen
		}
	} else {
		if ui.tabSel > newLen {
			ui.tabSel = newLen
		}
	}
	ui.ensureTabClicks()
}

func (ui *uiState) closeActiveFileTab() {
	if ui.tabSel == 0 {
		return
	}
	ui.closeTabAt(ui.tabSel - 1)
}

func (ui *uiState) closeAllFileTabs() {
	for _, t := range ui.openTabs {
		ui.releaseTabResources(t)
	}
	// The object window's viewport is not a file tab, but it holds the same
	// viewer, and the mod it reads from is about to be closed underneath it.
	ui.resetUnitModel()
	ui.openTabs = nil
	ui.tabSel = 0
	// The bar is empty now, so any scroll offset into it is meaningless and
	// has to go back to the left.
	ui.tabScrollX = 0
	ui.closeTabContextMenu()
	ui.ensureTabClicks()
}

func (ui *uiState) clearRgd() {
	t := ui.activeTab()
	if t != nil && t.kind == "rgd" {
		ui.closeTabAt(ui.tabSel - 1)
	}
}
func (ui *uiState) clearFda() {
	t := ui.activeTab()
	if t != nil && t.kind == "fda" {
		ui.closeTabAt(ui.tabSel - 1)
		return
	}
}
func (ui *uiState) closeActiveViewer() { ui.closeActiveFileTab() }

// findFirstModuleArg returns the first CLI arg that looks like a .module file (ignoring flags).
// It strips surrounding quotes if present.
func findFirstModuleArg() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		trim := strings.Trim(a, "\"'")
		trim = strings.TrimSpace(trim)
		if trim == "" {
			continue
		}
		// Normalize slashes
		if strings.EqualFold(filepath.Ext(trim), ".module") {
			return trim
		}
		// Also accept plain name without extension that resolves to .module via fallback?
		// Don't return non-.module paths.
	}
	return ""
}

// ── entry ────────────────────────────────────────────────────────────────────

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--debug-markers" {
		os.Exit(runMarkerDebugCLI(os.Args[2:], os.Stdout, os.Stderr))
	}

	initLogger()
	defer closeLogger()
	defer handlePanic()

	// Before anything is loaded: the collector's pacing is set once, and a
	// reader who wants Go's own can still say so in the environment.
	tuneCollector()

	// Handle file-association CLI flags before GUI starts.
	// Flags are private/undocumented besides --help; they are used by installer/runner.
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "--register", "--register-assoc", "/register", "-register":
			if err := registerModuleAssociation(); err != nil {
				log.Printf("register failed: %v", err)
				fmt.Fprintf(os.Stderr, "register failed: %v\n", err)
				exitLogged(1)
			}
			msg := fmt.Sprintf("Registered .module association for %s", getExePath())
			log.Printf("%s", msg)
			fmt.Println(msg)
			exitLogged(0)
		case "--unregister", "--unregister-assoc", "--remove-assoc", "/unregister":
			if err := unregisterModuleAssociation(); err != nil {
				log.Printf("unregister failed: %v", err)
				fmt.Fprintf(os.Stderr, "unregister failed: %v\n", err)
				exitLogged(1)
			}
			log.Printf("Removed .module association")
			fmt.Println("Removed .module association")
			exitLogged(0)
		case "--assoc-status", "--status":
			ok, detail, _ := isModuleAssociated()
			if ok {
				log.Printf("assoc-status: associated true (%s)", detail)
				fmt.Printf("associated: true (%s)\n", detail)
			} else {
				if detail != "" {
					log.Printf("assoc-status: associated false (current: %q)", detail)
					fmt.Printf("associated: false (current: %q)\n", detail)
				} else {
					log.Printf("assoc-status: associated false")
					fmt.Printf("associated: false\n")
				}
			}
			exitLogged(0)
		case "--help", "-h", "/?":
			fmt.Println("Usage:")
			fmt.Println("  dow-mod-studio.exe [path/to/mod.module]")
			fmt.Println("  dow-mod-studio.exe --register        Register .module file association (current user, HKCU)")
			fmt.Println("  dow-mod-studio.exe --unregister      Remove .module file association")
			fmt.Println("  dow-mod-studio.exe --assoc-status    Show association status")
			fmt.Println("  dow-mod-studio.exe --debug-markers --module <file> --unit <name|path>  Dump model marker coordinates as JSON")
			log.Printf("help shown, exiting 0; log=%q", logFilePath)
			exitLogged(0)
		}
	}

	go func() {
		defer handlePanic()
		w := new(app.Window)
		w.Option(app.Title("Corsix's Mod Studio 0.5.5  \u2014  DoW Mod Studio Go (Gio)"), app.Size(unit.Dp(1280), unit.Dp(760)))
		if err := run(w); err != nil {
			log.Printf("run error: %v", err)
		}
		exitLogged(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.TextSize = unit.Sp(13)
	th.Palette = material.Palette{
		Bg:         cBg,
		Fg:         cText,
		ContrastBg: cSaveBg,
		ContrastFg: cText,
	}

	ui := newUI(th)
	ui.win = w
	var ops op.Ops

	state, stateExists, stateErr := loadPersistedState()
	if stateErr != nil {
		log.Printf("load state: %v", stateErr)
	}
	if stateExists {
		ui.applyPersistedSettings(state)
	}

	// An explicit module argument always wins over the saved session.
	if findFirstModuleArg() != "" {
		ui.tryAutoLoad()
	} else if !stateExists || !ui.restorePersistedState(state) {
		ui.tryAutoLoad()
	}

	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			if err := savePersistedState(ui); err != nil {
				log.Printf("save state on exit: %v", err)
			}
			ui.stopModFileWatcher()
			ui.closeAllFileTabs()
			if ui.modTree != nil {
				ui.modTree.Close()
			}
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.maybeSaveState()
			ui.pollModCatalog()
			ui.pollImageDecodes()
			ui.pollModelJobs()
			ui.pollUnitModel()
			ui.handleInput(gtx)
			ui.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (ui *uiState) syncWindowTitle() {
	if ui.win == nil {
		return
	}
	if ui.modulePath != "" {
		ui.win.Option(app.Title(fmt.Sprintf("%s — DoW Mod Studio", filepath.Base(ui.modulePath))))
	} else {
		ui.win.Option(app.Title("DoW Mod Studio — No module"))
	}
}

func (ui *uiState) tryAutoLoad() {
	// 1) Highest priority: explicit .module file from OS (double-click / Open With / CLI)
	if arg := findFirstModuleArg(); arg != "" {
		// Handle quoted, relative, and bare name forms similar to dialog resolution
		candidate := arg
		// If relative or bare, try to resolve
		if !filepath.IsAbs(candidate) {
			if abs, err := filepath.Abs(candidate); err == nil {
				if _, err2 := os.Stat(abs); err2 == nil {
					candidate = abs
				} else {
					// Try default DoW DE dir
					try := filepath.Join(`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`, candidate)
					if _, err3 := os.Stat(try); err3 == nil {
						candidate = try
					} else if !strings.EqualFold(filepath.Ext(candidate), ".module") {
						try2 := candidate + ".module"
						try2Abs := filepath.Join(`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition`, try2)
						if _, err4 := os.Stat(try2Abs); err4 == nil {
							candidate = try2Abs
						} else if abs2, err5 := filepath.Abs(try2); err5 == nil {
							if _, err6 := os.Stat(abs2); err6 == nil {
								candidate = abs2
							}
						}
					}
				}
			}
		}
		// Bare name without .module + path fallback also tried above
		if !strings.EqualFold(filepath.Ext(candidate), ".module") && strings.TrimSpace(candidate) != "" {
			if _, err := os.Stat(candidate + ".module"); err == nil {
				candidate = candidate + ".module"
			}
		}
		if _, err := os.Stat(candidate); err == nil {
			ui.loadModule(candidate)
			return
		}
		// If arg looked like .module but file not found, show error and don't fall back silently
		if strings.EqualFold(filepath.Ext(arg), ".module") {
			ui.statusText = fmt.Sprintf("Module not found: %s — File → Open Module…", arg)
			log.Printf("tryAutoLoad: arg %q resolved to %q not found", arg, candidate)
			ui.syncWindowTitle()
			return
		}
	}

	candidates := []string{}
	if ui.gamePath != "" {
		candidates = append(candidates, filepath.Join(ui.gamePath, "DXP3.module"), filepath.Join(ui.gamePath, "W40k.module"))
	}
	candidates = append(candidates,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/W40k.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP2.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/WXP.module`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE.module`,
		"DXP3.module",
		"W40k.module",
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			ui.loadModule(p)
			return
		}
	}
	ui.statusText = "No module loaded — File → Open Module…"
	ui.syncWindowTitle()
}

func (ui *uiState) loadModule(path string) {
	// stop audio and close all file tabs before tearing down tree
	ui.stopModFileWatcher()
	ui.closeAllFileTabs()
	ui.clearImageCache()
	if ui.modTree != nil {
		ui.modTree.Close()
		ui.modTree = nil
	}
	// The previous mod's strings must stop being searched for before the new
	// mod's folder is known, or a table built now would still carry them.
	ui.projectDir = ""
	setLocaleOverrideDir("")
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	// clear previous state
	ui.expanded = map[string]bool{"Data": true}
	ui.selectedPath = ""
	ui.selectedTreePath = "Data"
	ui.keyboardArea = "files"
	ui.visibleNodes = nil
	ui.rowClicks = nil

	tree, err := modtree.BuildModTree(path, "English")
	if err != nil {
		ui.statusText = fmt.Sprintf("Failed to load %s: %v", filepath.Base(path), err)
		log.Printf("loadModule %q: %v", path, err)
		if tree != nil {
			tree.Close()
		}
		ui.modulePath = ""
		ui.syncWindowTitle()
		return
	}
	ui.modTree = tree
	ui.modulePath = path
	if tree.AppPath != "" {
		ui.gamePath = tree.AppPath
	}
	ui.rememberRecentFolder(filepath.Dir(path))
	ui.rememberRecentModule(path)
	ui.modSnapshot = currentModSnapshot(tree)
	ui.startModFileWatcher(tree)
	ui.lastModPoll = time.Now()
	// The mod's own project folder has to exist and be known before the string
	// table is built, because it is searched first: a localisation written into
	// it has to be in the table the same moment it is written. A failure here
	// must not stop the module from opening, so it is logged and the message
	// folded into the status line instead.
	if dir, perr := ensureProjectDirs(tree, path); perr != nil {
		log.Printf("projects folder for %s: %v", filepath.Base(path), perr)
		ui.projectStatusErr = perr
	} else {
		ui.projectDir = dir
		ui.unitsDir = filepath.Join(dir, unitsDirName)
		ui.projectStatusErr = nil
	}
	setLocaleOverrideDir(ui.projectDir)
	// A module brings its own Locale folder, and can add languages the engine
	// does not ship, so the picker is rediscovered with the module in place.
	// It also rebuilds the string table for the language already chosen.
	ui.refreshLocales()
	// clear viewers (already closed)
	ui.closeAllFileTabs()
	// expand Data only by default (user can drill down)
	ui.expanded = map[string]bool{"Data": true}
	f, fi := tree.Count()
	ui.statusText = fmt.Sprintf("Loaded %s — %d folders, %d files (%d sources)", filepath.Base(path), f, fi, len(tree.Sources))
	if len(tree.Warnings) > 0 {
		log.Printf("warnings loading %s:", path)
		for _, w := range tree.Warnings {
			log.Printf("  %s", w)
		}
	}
	// if tree is tiny, auto-expand one more level
	if f < 50 {
		// expand children of Data
		for _, c := range tree.Root.Children {
			ui.expanded[c.FullPath] = true
		}
	}
	// The result list points at files of the previous module, so drop it.
	ui.searchHits = nil
	ui.searchSel = 0
	ui.runSearch(true)
	if ui.projectStatusErr != nil {
		ui.statusText += fmt.Sprintf(" — projects folder unavailable: %v", ui.projectStatusErr)
	}
	ui.syncWindowTitle()
	ui.markStateDirty()
}

// ── top-level layout ─────────────────────────────────────────────────────────

func (ui *uiState) Layout(gtx layout.Context) layout.Dimensions {
	// window bg
	fillRect(gtx, cBg, gtx.Constraints.Max)

	// keep visibleNodes up to date for Layout to render
	ui.visibleNodes = ui.getVisibleNodes()
	ui.ensureRowClicks(len(ui.visibleNodes))
	ui.ensureRowHover(len(ui.visibleNodes))

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.layoutMenuBar),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cBorder, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(ui.openTabs) == 0 {
						return layout.Dimensions{}
					}
					return ui.layoutTabBar(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(ui.openTabs) == 0 {
						return layout.Dimensions{}
					}
					return hLine(gtx, cBorder, 1)
				}),
				layout.Flexed(1, ui.layoutContent),
				layout.Rigid(ui.layoutStatusBar),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.menuOpen < 0 {
				return layout.Dimensions{}
			}
			return ui.layoutMenuDropdown(gtx)
		}),
		layout.Expanded(ui.layoutSearchOverlay),
		layout.Expanded(ui.layoutLocaleOverlay),
		// The tab context menu is the last thing drawn, so it sits above the
		// bar it was opened from and above any other menu that is up.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.tabContextVisible {
				return layout.Dimensions{}
			}
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					pass := pointer.PassOp{}.Push(gtx.Ops)
					dims := ui.tabContextDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: gtx.Constraints.Max}
					})
					pass.Pop()
					return dims
				}),
				layout.Stacked(ui.layoutTabContextMenu),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.filePickOpen {
				return layout.Dimensions{}
			}
			return ui.layoutFilePicker(gtx)
		}),
		// The event picker sits above the import dialog: an event field
		// opens it from inside the Events section.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.evtPick.open {
				return layout.Dimensions{}
			}
			return ui.layoutEvtPicker(gtx)
		}),
		// The new unit dialog is above the open dialog: it is opened from the
		// object window, and a dialog that writes a file should be the one
		// thing the window is answering to.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.newObjDialog {
				return layout.Dimensions{}
			}
			return ui.layoutNewObjDialog(gtx)
		}),
		// The localisation editor is above the other dialogs: it is opened
		// from inside the object window, so anything already up would have to
		// be let go of first.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.localeEditDialog {
				return layout.Dimensions{}
			}
			return ui.layoutLocaleEditDialog(gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.settingsDialog {
				return layout.Dimensions{}
			}
			return ui.layoutSettingsDialog(gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.fileDeleteDialog {
				return layout.Dimensions{}
			}
			return ui.layoutFileDeleteDialog(gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.clearRecycleDialog {
				return layout.Dimensions{}
			}
			return ui.layoutClearRecycleDialog(gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.archiveDialog {
				return layout.Dimensions{}
			}
			return ui.layoutArchiveDialog(gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.rgdTooltipText == "" {
				return layout.Dimensions{}
			}
			return layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.th, unit.Sp(10), ui.rgdTooltipText)
						l.Color = cText
						return l.Layout(gtx)
					})
				})
			})
		}),
	)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func hLine(gtx layout.Context, col color.NRGBA, h int) layout.Dimensions {
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	sz := image.Pt(gtx.Constraints.Max.X, h)
	stack := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
	return layout.Dimensions{Size: sz}
}

func fillRect(gtx layout.Context, col color.NRGBA, sz image.Point) {
	stack := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

func rrect(gtx layout.Context, col color.NRGBA, sz image.Point, r int) {
	stack := clip.RRect{Rect: image.Rectangle{Max: sz}, SE: r, SW: r, NE: r, NW: r}.Push(gtx.Ops)
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
}

// ── toolbar ──────────────────────────────────────────────────────────────────

// ── main content (3 panels) ──────────────────────────────────────────────────

// ── middle panel ─────────────────────────────────────────────────────────────

func (ui *uiState) layoutMiddlePanel(gtx layout.Context) layout.Dimensions {
	tab := ui.activeTab()
	if tab != nil && tab.kind == "fda" {
		return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.middleHeader),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutFdaPlayer(gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.kind == "wav" {
		return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.middleHeader),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutWavViewer(gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.kind == "whm" {
		return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.middleHeader),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutWhmViewer(gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.kind == "image" {
		return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.middleHeader),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutImageViewer(gtx, tab) }),
			)
		})
	}
	// Text/script editor (rat, con, lua, ai)
	if tab != nil && tab.kind == "text" {
		return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.middleHeader),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ui.layoutTextEditor(gtx, tab) }),
			)
		})
	}
	// RGD or Module view
	if tab != nil && tab.kind == "rgd" {
		return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(ui.middleHeader),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if tab.rgdFile == nil {
						if tab.rgdError != "" {
							return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								col := rgb(0xB00020)
								l := material.Label(ui.th, unit.Sp(11), "Failed: "+tab.rgdError)
								l.Color = col
								return l.Layout(gtx)
							})
						}
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.th, unit.Sp(11), "No RGD — this tab is empty")
							l.Color = cTextDim
							return l.Layout(gtx)
						})
					}
					if len(tab.rgdMidNodes) == 0 {
						ui.rebuildRgdMidNodesFor(tab)
						ui.ensureRgdMidClicks(tab, len(tab.rgdMidNodes))
					}
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return ui.layoutListWithScrollbar(gtx, &tab.rgdMidScroll, &tab.rgdMidList, 22, len(tab.rgdMidNodes), func(gtx layout.Context, index int) layout.Dimensions {
							v := tab.rgdMidNodes[index]
							return ui.layoutRgdMidRow(gtx, tab, index, v)
						})
					})
				}),
			)
		})
	}
	// Module tab or empty
	return ui.panelWithScroll(gtx, &ui.middleScroll, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(ui.middleHeader),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Keep the empty-state message inside the panel with a clear left margin.
					gtx.Constraints.Min.X += gtx.Dp(unit.Dp(16))
					gtx.Constraints.Max.X -= gtx.Dp(unit.Dp(16))
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return centeredPanelLabel(gtx, ui.th, "No file selected", unit.Sp(11), cTextDim)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return centeredPanelLabel(gtx, ui.th, "Module tab — click a .rgd/.fda/.jpg/.tga/.dds file in the left tree to open in a new tab", unit.Sp(10), cTextDim)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if len(ui.openTabs) > 0 {
								return centeredPanelLabel(gtx, ui.th, fmt.Sprintf("%d file tab(s) open — switch via the top tab bar", len(ui.openTabs)), unit.Sp(10), cTextDim)
							}
							return layout.Dimensions{}
						}),
					)
				})
			}),
		)
	})
}

// ── FDA player center UI helpers ────────────────────────────────────────────

func (ui *uiState) layoutWaveform(gtx layout.Context, t *fileTab) layout.Dimensions {
	h := gtx.Dp(unit.Dp(28))
	// When called from Flexed, Max/Min are already the flexed width.
	w := gtx.Constraints.Max.X
	if w < gtx.Dp(unit.Dp(80)) {
		w = gtx.Dp(unit.Dp(80))
	}
	sz := image.Pt(w, h)
	// Constrain to exact size
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz

	// Input: use existing seek Float so position stays in sync with audio.
	// Float.Layout registers pointer handling and updates Value on drag/click.
	dims := t.fdaSeekFloat.Layout(gtx, layout.Horizontal, unit.Dp(4))
	// Apply seek if user dragged/clicked
	ui.fdaHandleSliderSeek(gtx, t)

	// Build peaks if width changed or empty
	if len(t.fdaWavePeaks) != w || t.fdaWaveW != w {
		t.fdaWavePeaks = buildWavePeaks(t.fdaPcmBytes, w)
		t.fdaWaveW = w
	}
	peaks := t.fdaWavePeaks
	frac := t.fdaSeekFloat.Value
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	playX := int(float32(w) * frac)
	if playX < 0 {
		playX = 0
	}
	if playX > w {
		playX = w
	}

	// Background
	bg := cContentBg
	stack := clip.Rect{Max: sz}.Push(gtx.Ops)
	paint.ColorOp{Color: bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()

	// Border
	b := widget.Border{Color: cBorderLight, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}
	gtx.Constraints.Min = sz
	gtx.Constraints.Max = sz
	_ = b.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })

	// Centre line (subtle)
	midY := h/2 - 1
	st := clip.Rect(image.Rect(1, midY, w-1, midY+1)).Push(gtx.Ops)
	paint.ColorOp{Color: rgba(0, 0, 0, 10)}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()

	// Bars — 1px wide, gap 1px when w is large enough? For now 1px per column, topped/bottom centered.
	// Draw in two passes: unplayed grey, played blue, for speed use single loop.
	colPlayed := rgb(0x0078D4)
	colUnplayed := rgb(0x9EA8B3)
	// Slight dim for very low peaks: still visible
	for x := 0; x < w; x++ {
		p := 0
		if peaks != nil && x < len(peaks) {
			p = int(peaks[x])
		}
		if p == 0 {
			p = 1 // ensure 1px line for silence so track not empty
		}
		barH := (h - 6) * p / 255
		if barH < 2 {
			barH = 2
		}
		if barH > h-4 {
			barH = h - 4
		}
		y0 := (h - barH) / 2
		y1 := y0 + barH
		// clip 1px wide bar (leave 0 gap for dense view; if w>300 we could add gap but keep 1px)
		r := image.Rect(x, y0, x+1, y1)
		if r.Max.X > w {
			continue
		}
		if r.Min.X < 1 {
			r.Min.X = 1
		}
		if r.Max.X > w-1 {
			continue
		}
		st := clip.Rect(r).Push(gtx.Ops)
		if x < playX {
			paint.ColorOp{Color: colPlayed}.Add(gtx.Ops)
		} else {
			paint.ColorOp{Color: colUnplayed}.Add(gtx.Ops)
		}
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	// Playhead line 1px
	if playX >= 1 && playX < w-1 {
		st := clip.Rect(image.Rect(playX, 1, playX+1, h-1)).Push(gtx.Ops)
		paint.ColorOp{Color: rgb(0xE81123)}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
		knobR := gtx.Dp(unit.Dp(3))
		knobRect := image.Rect(playX-knobR, h/2-knobR, playX+knobR, h/2+knobR)
		paint.FillShape(gtx.Ops, rgb(0xE81123), clip.Ellipse(knobRect).Op(gtx.Ops))
	}

	return dims
}
