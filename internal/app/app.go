package app

import (
	"context"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/applog"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
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

	"github.com/fogleman/fauxgl"
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
	GpuAPI string

	// forceSoftRenderer keeps the 3D viewport on the software rasteriser.
	ForceSoftRenderer bool

	Th *material.Theme

	MenuOpen       int
	MenuClicks     []widget.Clickable
	MenuItemClicks [][]widget.Clickable
	DismissClick   widget.Clickable   // transparent full-screen catcher
	TabSel         int                // 0=Module, 1..N=file tab (index+1)
	TabClicks      []widget.Clickable // len = 1+len(openTabs), [0]=Module
	TabCloses      []widget.Clickable // len = len(openTabs), per file tab close ×
	OpenTabs       []*fileTab

	// The tab row scrolls sideways under the wheel, so the bar can hold more
	// tabs than fit. tabScrollX is how far it is scrolled, in pixels, and it
	// is clamped every frame against the row's real width — the bar keeps no
	// idea of that width itself.
	TabScrollX int
	// tabBarWidth and tabRowWidth are what the clamp above needs, and the
	// input pass is the one that moves the offset — so the layout leaves the
	// numbers it measured behind for it. tabRowWidthPx is the row's width as
	// just measured, before it becomes next frame's tabRowWidth.
	TabBarWidth   int
	TabRowWidth   int
	TabRowWidthPx int
	// tabBarPointerTag is the whole bar as a pointer target. A press on it says
	// which tab was clicked by position, and a scroll on it moves the row; both
	// are recorded in the bar's own coordinates, which is what pointer.Event
	// reports for a target.
	TabBarPointerTag event.Tag
	// tabRects is where each tab was last drawn, in bar coordinates, so a
	// right press can be resolved to a tab without a clickable per tab that
	// only exists for this.
	TabRects []image.Rectangle
	// The tab context menu. tabContextPos is where it opens, in window
	// coordinates, and tabContextIndex is the tab it was opened on. Only
	// "close all tabs" is in it for now, and the item list is a slice so
	// another one is a line here and a row in layoutTabContextMenu.
	TabContextVisible  bool
	TabContextIndex    int
	TabContextPos      image.Point
	TabContextDismiss  widget.Clickable
	TabContextCloseAll widget.Clickable

	ImageCache      map[string]imageCacheEntry
	ImageCacheBytes int64
	ImageCacheSeq   uint64

	// modelGpu is the hardware renderer for the 3D preview, or nil when the
	// window's backend cannot provide one and the software renderer is used.
	// It is opened on first use because Gio creates the window's GPU with the
	// first frame.
	ModelGpu      render.Handle
	ModelGpuTried bool

	// mod tree
	ModTree          *modtree.ModTree
	Expanded         map[string]bool
	SelectedPath     string
	SelectedTreePath string
	KeyboardArea     string
	VisibleNodes     []modtree.VisibleNode
	// rowClicks is one gesture per visible row, in the order of visibleNodes.
	// A row's click is Gio's to decide: the press and the release that belong
	// to one row, and nothing the app has to remember in between.
	RowClicks []gesture.Click
	// leftTreePointerTag is the tree's one area of its own, over the whole
	// list. A row's gesture cannot see a right press — a click is the primary
	// button by definition — so the file menu still comes from here.
	LeftTreePointerTag event.Tag
	LeftRowHeightPx    int
	RowHover           []bool
	LastTreeClickPath  string
	LastTreeClickAt    time.Time
	LeftList           layout.List
	StatusText         string
	ModulePath         string
	GamePath           string
	// textEditorFormats are the file extensions a click in the tree or an
	// Enter opens in the built-in text editor, lower case and each with a
	// leading dot. It is a setting rather than a constant because a module
	// can carry scripts of its own naming, and Settings lets the list be
	// extended without a rebuild.
	TextEditorFormats  []string
	RecentFolders      []string
	RecentModules      []string
	RecentMenuExpanded bool
	StateDirty         bool
	LastStateSave      time.Time
	LastModPoll        time.Time
	ModSnapshot        string
	ModRefreshCh       chan modCatalogRefreshResult
	ModRefreshRunning  bool
	ModFileWatcher     *fsnotify.Watcher
	ModFileWatchSignal chan struct{}

	LeftPanelWidth  int
	RightPanelWidth int
	PanelSplitters  [2]panelSplitterState
	LeftScroll      panelScrollState
	MiddleScroll    panelScrollState
	RightScroll     panelScrollState

	// The file dialog every caller shares: Open Module, the timeline import
	// and the event editor's sound field. filePickOpen says it is up.
	FilePick     filePicker
	FilePickOpen bool

	SettingsDialog        bool
	SettingsThemeOriginal bool
	SettingsGameOriginal  string
	SettingsGameEditor    widget.Editor
	// settingsFormatEditor holds the list of file formats the built-in
	// text editor opens, as a person would type it. It is filled from
	// textEditorFormats when the dialog opens and read back on OK, so a
	// cancelled dialog changes nothing.
	SettingsFormatEditor   widget.Editor
	SettingsFormatOriginal string
	SettingsThemeClick     widget.Clickable
	SettingsOk             widget.Clickable
	SettingsCancel         widget.Clickable
	DarkTheme              bool

	// File association rows of the Settings dialog: one button per assocTypes
	// format, sized when the dialog opens. assocOn caches what the registry
	// said about each, so the button can tell an add from a removal.
	AssocClicks []widget.Clickable
	AssocOn     []bool

	ArchiveDialog       bool
	ArchiveOptions      []archiveOption
	ArchiveSelected     []bool
	ArchiveSelectClicks []widget.Clickable
	ArchiveList         layout.List
	ArchiveStatus       string
	ArchiveExtractClick widget.Clickable
	ArchiveCloseClick   widget.Clickable

	// left-panel file context menu and recycle-bin confirmation
	ContextMenuPath     string
	ContextMenuAnchor   int
	ContextMenuVisible  bool
	ContextDismissClick widget.Clickable
	ContextDuplicate    widget.Clickable
	ContextDelete       widget.Clickable
	FileDeleteDialog    bool
	FileDeletePath      string
	FileDeleteConfirm   widget.Clickable
	FileDeleteCancel    widget.Clickable
	ClearRecycleDialog  bool
	ClearRecycleConfirm widget.Clickable
	ClearRecycleCancel  widget.Clickable

	// window ref for title update (set in run)
	Win *app.Window

	// Hover tooltip for RGD values whose visible name comes from a numeric alias.
	RgdTooltipTags  map[string]event.Tag
	RgdTooltipText  string
	RgdEditorClicks map[*widget.Editor]*rgdEditorClickState

	// ── file search (top-right of the menu bar) ──
	// The field lives in the menu bar, but the results drop down as a panel
	// over the right side of the window so the tree stays visible underneath.
	SearchVisible    bool
	SearchEditor     widget.Editor
	SearchFieldClick widget.Clickable
	SearchDismiss    widget.Clickable
	SearchKind       modtree.SearchKind
	SearchKindClicks []widget.Clickable
	SearchHits       []modtree.SearchHit
	SearchHitClicks  []widget.Clickable
	SearchHover      []bool
	SearchSel        int
	SearchStatus     string
	SearchList       layout.List
	SearchScroll     panelScrollState
	// lastSearchQuery/lastSearchKind remember what the visible results were
	// built from, so the walk over the whole tree only runs when they change.
	LastSearchQuery string
	LastSearchKind  modtree.SearchKind
	SearchQuerySet  bool
	// searchHeldQuery is the query a dismissed search was dismissed at. The
	// panel reopens only when the field's text actually differs from it, so a
	// dismissed search stays down while its query is kept, and a stray editor
	// event cannot spring it back open over the content.
	SearchHeldQuery string

	// ── localisation ──
	// locale is the language "$id" values are resolved in. The table behind
	// it is built by setLocale, which the dropdown calls on every pick; the
	// field in the menu bar is the only other reader.
	Locale        string
	LocaleList    []string
	LocaleOpen    bool
	LocaleTrigger widget.Clickable
	LocaleDismiss widget.Clickable
	LocaleClicks  []widget.Clickable
	// localeDropBox is where the open list was last drawn, in window
	// coordinates. Nothing draws from it; a test aims a pixel probe at the
	// rows with it.
	LocaleDropBox image.Rectangle
	// localePending is the id an Alt+click asked for. The press is seen while
	// the row is laid out, and opening a tab belongs to the input pass, so the
	// id is left here for the next frame to act on.
	LocalePending string
	// localePendingRequest is the same arrangement for a press that wants the
	// edit dialog rather than a tab. The field and its key come with it, because
	// a press in the file view already knows which entry it was on and one in
	// the object window has to be looked up again.
	LocalePendingRequest  localeEditRequest
	LocalePendingField    *rgd.RgdEntry
	LocalePendingFieldKey string
	// localeEditTags is the pointer target of every localised row, keyed by the
	// field it belongs to: a row can be drawn more than once and both drawings
	// have to be the same target.
	LocaleEditTags map[string]event.Tag
	// The localisation editor. localeEditID is empty while a new string is
	// being written, and localeEditCreate says to allocate a free id on save
	// rather than change an existing entry.
	LocaleEditDialog   bool
	LocaleEditID       string
	LocaleEditCreate   bool
	LocaleEditEditor   widget.Editor
	LocaleEditError    string
	LocaleEditSave     widget.Clickable
	LocaleEditCancel   widget.Clickable
	LocaleEditField    *rgd.RgdEntry
	LocaleEditFieldKey string

	// ── unit / building object interface ──
	// The tool panel sits in the menu bar left of the search field; the button
	// opens the object interface as a tab of its own, which takes the space of
	// the file tree and the middle panel.
	UnitSection       int
	UnitToolClicks    []widget.Clickable
	UnitSectionClicks []widget.Clickable

	// Classic OE section state: which of the classic editor's header panels
	// are open, their click targets, the scroll of the panel column, and the
	// clip list's own click targets — separate from the Viewport section's, so
	// the two lists cannot both read one press.
	ClassicOEOpen       []bool
	ClassicOEClicks     []widget.Clickable
	ClassicOEScroll     panelScrollState
	ClassicOEAnimClicks []widget.Clickable

	// The Motions and Actions folders press like the tree's rows: one
	// click target per row, index-keyed because a folder's contents
	// change only with the .whe, never with expansion.
	ClassicOEMotionClicks []widget.Clickable
	ClassicOEActionClicks []widget.Clickable

	// The four folders of the Anims / Motions / Actions tree — which are
	// open, and the expand toggles of the action tree's nodes. The toggles
	// are keyed by node path rather than an index, because the visible rows
	// change with every expansion while the click targets have to survive
	// the re-render. Expansion state lives in its own map so that a key's
	// absence can mean "the default" — open for a root action, closed for
	// anything nested.
	ClassicOEFolderOpen   [classicOEFolderCount]bool
	ClassicOEFolderInit   bool
	ClassicOEFolderClicks [classicOEFolderCount]widget.Clickable
	ClassicOEExpandClicks map[string]*widget.Clickable
	ClassicOEExpandOpen   map[string]bool
	// classicOEJumpClicks is the other press target of a tree row: the
	// marker column toggles a branch, the rest of the row plays the
	// animation the node points at, so the two never share a click.
	ClassicOEJumpClicks map[string]*widget.Clickable

	// The double-click dialog of the Motions and Actions folders: which
	// editor is on show, the item it was opened for, and its own scroll
	// and buttons. It borrows the Events section's editor state — the
	// document, the fields and Save .whe — rather than keeping a second
	// copy of any of it.
	ClassicOEDialog     int // classicOEDialogNone / Timeline / Action
	ClassicOEDialogName string
	ClassicOEevtScroll  panelScrollState
	ClassicOEevtClose   widget.Clickable
	ClassicOEevtDim     widget.Clickable
	// classicOEevtCardTag is the area the editor card covers against the dim:
	// a press on the card's own empty space is taken here so it never reaches
	// the dim and closes the editor under the user's hand.
	ClassicOEevtCardTag struct{}
	// Double-click detection for the folder rows: the row last pressed
	// and when, in the same window the mod tree's file rows use.
	ClassicOELastClickKey      string
	ClassicOELastClickAt       time.Time
	ClassicOEModifierClicks    []widget.Clickable
	ClassicOEModifierEditors   [4]widget.Editor
	ClassicOEModifierFill      [4]string
	ClassicOEModifierName      string
	ClassicOEModifierBound     string
	ClassicOEModifierError     string
	ClassicOEModifierApply     widget.Clickable
	ClassicOESelectedUIEditors [20]widget.Editor
	ClassicOESelectedUIFill    [20]string
	ClassicOESelectedUIBound   string
	ClassicOESelectedUIError   string
	ClassicOESelectedUIApply   widget.Clickable
	ClassicOESelectedUIType    *widget.Enum
	ClassicOESelectedUIVisible *widget.Bool
	ClassicOEClauseClicks      []widget.Clickable
	ClassicOEClauseEditors     [5]widget.Editor
	ClassicOEClauseFill        [5]string
	ClassicOEClauseName        string
	ClassicOEClauseBound       string
	ClassicOEClauseError       string
	ClassicOEClauseApply       widget.Clickable
	ClassicOEConditionClicks   []widget.Clickable
	ClassicOEConditionEditors  []widget.Editor
	ClassicOEConditionFill     []string
	ClassicOEConditionName     string
	ClassicOEConditionBound    string
	ClassicOEConditionError    string
	ClassicOEConditionApply    widget.Clickable
	ClassicOEConditionAdd      widget.Clickable
	ClassicOEConditionRemove   widget.Clickable
	ClassicOESelectedUICreate  widget.Clickable
	ClassicOEWheSave           widget.Clickable
	ClassicOEWheRevert         widget.Clickable

	// The dialog browses the mod's own units rather than the open file, so it
	// carries its own selection. unitCatalog is built once per mod; the
	// indexes into it are what the pickers and the columns work from, and are
	// clamped rather than trusted, so a stale index after a mod change cannot
	// panic the layout.
	UnitCatalog    *unitCatalog
	UnitCatalogMod string
	// modGraph is the same tree read as a graph: every .rgd of Data\attrib
	// as an entity, and every reference a unit or building holds as a link.
	// It is built once per mod and read from the background scans, so it is
	// cached the way the catalogue is and dropped with it.
	ModGraph    *modGraph
	ModGraphMod string
	// graphLinksCh carries the background scan's batches of references,
	// graphLinksStop ends a scan, graphLinksRunning says one is going, and
	// graphLinksGraph is which graph it is writing into.
	GraphLinksCh      chan graphLinkBatch
	GraphLinksStop    chan struct{}
	GraphLinksRunning bool
	GraphLinksGraph   *modGraph
	UnitRaceSel       int
	UnitSel           int
	// unitPickRace and unitPickPath are the Object window's pickers as the
	// last session left them, waiting for a catalog to be resolved against.
	// They are consumed the first time the catalog is built and are empty
	// after that, so a mod opened later in the session is not dragged back to
	// a unit from the one before it.
	UnitPickRace string
	UnitPickPath string
	// unitNames maps a unit's lower cased virtual path to the localisation id
	// of its screen name, filled in the background by startUnitNameScan. The
	// text itself is never cached here: it is read from the locale table on the
	// UI thread, so changing the language relabels the lists for free.
	UnitNames map[string]string
	// unitNamesCh carries the scan's batches, unitNamesStop ends a scan that a
	// module reload has made pointless, and unitNamesTree says which tree the
	// running scan belongs to.
	UnitNamesCh      chan unitNameBatch
	UnitNamesStop    chan struct{}
	UnitNamesTree    *modtree.ModTree
	UnitNamesRunning bool
	UnitNamesDone    bool
	// unitDetails caches the parsed unit so switching sections does not re-read
	// the file, and unitDetailsPath says which unit it belongs to.
	UnitDetails     *unitDetails
	UnitDetailsPath string
	// unitIcons caches the decoded object icons of the open mod, keyed by the
	// unit's tree path, and unitIconsMod says which mod they belong to — the
	// same pairing unitDetails uses, for the same reason.
	UnitIcons    map[string]iconCacheEntry
	UnitIconsMod string
	// actionIcons caches the Classic OE action icons of the open mod, keyed by
	// the action's lower-cased name, and actionIconsMod says which mod they
	// belong to — the same pairing unitIcons uses, for the same reason.
	ActionIcons    map[string]iconCacheEntry
	ActionIconsMod string
	// unitInfoList and unitInfoScroll are the info column's scroll state. The
	// column holds a hundred scalars rather than the dozen it started with, so
	// it scrolls like the other lists in the app.
	UnitInfoList   layout.List
	UnitInfoScroll panelScrollState
	// unitSquads maps a unit's lower cased tree path to the squads it is part
	// of. The link only exists from the squad's side, so it is built by walking
	// every squad .rgd in the background: unitSquadsCh carries the batches,
	// unitSquadsStop ends a scan a module reload has made pointless, and
	// unitSquadsTree says which tree the running scan belongs to.
	UnitSquads        map[string][]unitSquadRef
	UnitSquadsCh      chan unitSquadBatch
	UnitSquadsStop    chan struct{}
	UnitSquadsTree    *modtree.ModTree
	UnitSquadsRunning bool
	UnitSquadsDone    bool
	// unitSquadList and unitSquadScroll are the Squads column's scroll state.
	UnitSquadList   layout.List
	UnitSquadScroll panelScrollState
	// picker state: whether the race or unit dropdown is open, and the click
	// lists of the dropdown rows.
	UnitRaceOpen   bool
	UnitListOpen   bool
	UnitRaceClicks []widget.Clickable
	UnitClicks     []widget.Clickable
	UnitList       layout.List
	UnitListScroll panelScrollState
	// unitRaceTrigger and unitListTrigger are the two dropdown fields.
	UnitRaceTrigger widget.Clickable
	UnitListTrigger widget.Clickable
	// unitNewTrigger and unitBuildingTrigger are the buttons that start a new
	// unit or a new building of the selected race, and the fields of the dialog
	// they open.
	UnitNewTrigger      widget.Clickable
	UnitBuildingTrigger widget.Clickable
	// unitFreezeTrigger and unitUnfreezeTrigger move the selected object out
	// of the mod and back into it.
	UnitFreezeTrigger   widget.Clickable
	UnitUnfreezeTrigger widget.Clickable
	// freezeBook is the mod's record of what has been frozen, kept because the
	// object window asks about it on every frame and it only changes when a
	// freeze does.
	FreezeBook         *freezeBook
	FreezeBookMod      string
	NewObjKind         newObjKind
	NewObjDialog       bool
	NewObjNameEditor   widget.Editor
	NewObjOk           widget.Clickable
	NewObjCancel       widget.Clickable
	NewObjStatus       string
	NewObjDismissClick widget.Clickable
	// newObjCardBox is where the new unit dialog's card was last drawn, in
	// window coordinates. Nothing draws from it; it is recorded so a test can
	// aim a pixel probe at the card's buttons instead of guessing.
	NewObjCardBox image.Rectangle
	// unitDismissClick is a transparent area over the columns, laid out only
	// while a dropdown is open, so a click that misses both the strip and the
	// list puts it away.
	UnitDismissClick widget.Clickable
	// unitDropBox is where the open dropdown was last drawn, in the content
	// area's own coordinates. Nothing draws from it; it is recorded so a test
	// can aim a pixel probe at the list instead of guessing where it is.
	UnitDropBox image.Rectangle
	// unitSelRow is which row of the weapons or abilities list is selected, and
	// unitRowClicks holds that list's click targets.
	UnitSelRow    int
	UnitRowClicks []widget.Clickable
	// unitInfoSel is which row of the info list the right column is editing,
	// unitInfoClicks holds that list's click targets, and unitInfoDirty says
	// the parsed unit has been changed in memory and not yet written back. The
	// one editor and one toggle are bound to whichever field is selected:
	// unitValBound is the field they are showing, unitValError the last refusal.
	UnitInfoSel    int
	UnitInfoClicks []widget.Clickable
	UnitInfoDirty  bool
	UnitValEditor  widget.Editor
	UnitValBound   string
	UnitValError   string
	UnitBoolClick  widget.Clickable
	// unitSaveClick, unitRevertClick and unitOpenRgdClick are the three buttons
	// under the value.
	UnitSaveClick    widget.Clickable
	UnitRevertClick  widget.Clickable
	UnitOpenRgdClick widget.Clickable

	// unitModel is the 3D preview of the object window's Viewport section. It
	// is a fileTab of its own rather than a field of the object tab, because
	// the preview is a per unit thing and the object tab is a singleton: the
	// same viewer a .whm tab uses, driven from here.
	UnitModel     *fileTab
	UnitModelCh   chan unitModelResult
	UnitModelFor  string // the unit .rgd the loaded model belongs to
	UnitModelPath string // the .whm that was found, empty when there is none
	UnitModelErr  string // why no model is on screen
	// unitModelStart is when the load in flight was started, so the answer can
	// say how much of the wait was the read and how much was the window not
	// repainting to collect it.
	UnitModelStart time.Time
	// unitAnimClicks are the animation rows of the viewport's side panel.
	UnitAnimClicks []widget.Clickable
	// unitMarkerClicks are the rows of the markers list, unitBoneClicks the
	// rows of the bones list, and unitMarkerListToggle the button that swaps
	// one list for the next: all three are lists of the same model's
	// contents and either may be the one being read, so one button between
	// them is enough.
	UnitMarkerClicks []widget.Clickable
	UnitBoneClicks   []widget.Clickable
	UnitFXListMode   bool
	// unitMeshListMode is the fifth list tab of the side panel: the model's
	// meshes with an eye toggle each, beside Animations, Markers, Bones and
	// FX. It shares the side panel's scroll state with the other tabs.
	UnitMeshListMode  bool
	UnitFXClicks      []widget.Clickable
	UnitListTabClicks [5]widget.Clickable
	// unitBoneKeyClicks are the rows of the currently picked bone's keyframes,
	// sized for the current bone whenever the bone list changes which bone is
	// picked.
	UnitBoneKeyClicks []widget.Clickable
	// unitBoneKeySel is which of those rows are picked: keyed by the kind of
	// key and the frame it sits on, so taking a key out of the track shifts
	// every index after it without moving the selection off the rows it was
	// put on. unitBoneKeySelAnchor is the row a Shift+click extends from
	// (mode −1 when there is none) and unitBoneKeySelBound the clip and bone
	// the selection belongs to — another of either starts a fresh one rather
	// than carrying frames over to keys that are not there.
	UnitBoneKeySel       map[boneKeyRef]bool
	UnitBoneKeySelAnchor boneKeyRef
	UnitBoneKeySelBound  string
	// A pointer press carries no usable modifiers in this panel, so Ctrl+click
	// and Shift+click read the key events unitListTrackModifiers keeps up to
	// date, ORed with the live OS state heldModifiers reports.
	UnitBoneKeyCtrlHeld  bool
	UnitBoneKeyShiftHeld bool
	UnitBoneKeyModLog    string
	// unitBoneKeySetClick applies the typed fields to their target keys, and
	// unitBoneKeySetAllClick applies only the axes that were typed over to
	// every key of that kind the selected bone has in the clip.
	UnitBoneKeySetClick    widget.Clickable
	UnitBoneKeySetAllClick widget.Clickable
	// unitBoneKeyAddPosClick and unitBoneKeyAddRotClick give the picked bone
	// a key of that kind on the frame the playhead is on,
	// unitBoneKeyBeforeClick and unitBoneKeyAfterClick add one on the nearest
	// free frame either side of every picked row, and
	// unitBoneKeyDelClick takes the picked rows away: the five the pose bar
	// offers for editing which frames the clip carries for the bone.
	UnitBoneKeyAddPosClick widget.Clickable
	UnitBoneKeyAddRotClick widget.Clickable
	UnitBoneKeyBeforeClick widget.Clickable
	UnitBoneKeyAfterClick  widget.Clickable
	UnitBoneKeyDelClick    widget.Clickable
	// unitBoneKeyFillText is the text each field was last filled with, so
	// "changed" is what was typed over rather than what parses differently.
	UnitBoneKeyFillText [2][3]string
	// unitBonePosEd and unitBoneRotEd are the picked key's position and
	// rotation typed in by axis: Enter in a field applies the triple to the
	// key, and the rotation fields take degrees of an XYZ turn rather than
	// the quaternion the file stores.
	UnitBonePosEd [3]widget.Editor
	UnitBoneRotEd [3]widget.Editor
	// unitBoneMirClick are the three "mirror angle" buttons: each flips the
	// sign of the rotation about its axis (X, Y, Z) on the key the Rot
	// fields show.
	UnitBoneMirClick [3]widget.Clickable
	// unitBoneAngleSlid are the three angle sliders above the mirror
	// buttons: each turns the rotation key the Rot fields show about
	// its own axis across the full -180..180 degree turn the fields
	// work in.
	UnitBoneAngleSlid [3]widget.Float
	// unitBoneKeyShiftEd is how many frames the shift moves the picked keys
	// by, and unitBoneKeyShiftBack / unitBoneKeyShiftFwd move them back and
	// forward by that many frames.
	UnitBoneKeyShiftEd   widget.Editor
	UnitBoneKeyShiftBack widget.Clickable
	UnitBoneKeyShiftFwd  widget.Clickable
	// unitBoneUndo is the stack of key-track snapshots taken before each
	// edit (add, delete, shift, mirror, set, slider), most recent last, so
	// Undo rolls back the last change to the picked bone's keys.
	// unitBoneKeyUndoClick is the Undo button. unitBoneSliderUndoArmed
	// marks that the current slider drag already has a snapshot, so a whole
	// drag is one undo step rather than one per frame.
	UnitBoneUndo            []boneTrackSnapshot
	UnitBoneKeyUndoClick    widget.Clickable
	UnitBoneSliderUndoArmed bool
	// unitBoneKeyBound is the selection the fields were last filled from and
	// unitBoneKeyFrame the frame they were filled at: a change of selection
	// refills them whatever the cursor is doing, a change of frame alone
	// leaves a group being typed into alone. unitBoneKeyErr says why the last
	// apply or save did nothing, and is cleared by the next success or
	// selection change.
	UnitBoneKeyBound string
	UnitBoneKeyFrame int
	UnitBoneKeyErr   string
	// unitBoneKeyBtnLog and unitBonePoseBarLog are diagnostics for dead
	// clicks: the first remembers the hover/press states of the pose bar's
	// eight buttons, the second whether the pose bar is laid out at all, so
	// the log shows on change only where a press stopped being noticed.
	UnitBoneKeyBtnLog  [26]bool
	UnitBonePoseBarLog string
	// unitBoneKeyFocusLog is which of the six value fields held the caret on
	// the last frame: typing and Enter both need it, so a caret that never
	// lands explains both at once.
	UnitBoneKeyFocusLog [6]bool
	// unitBoneKeySaveClick writes the edited bone keys back into the .whm,
	// and unitModelWhmDirty says there are edits to write. The flag is per
	// unit viewport because that is where the bone keys can be edited.
	UnitBoneKeySaveClick widget.Clickable
	UnitModelWhmDirty    bool
	// unitAnimFramesEd is the clip's frame count typed in and
	// unitAnimFramesSetClick applies it: the count is what the keyframe strip
	// counts the clip out in and what Save WHM rewrites in the clip's header.
	// unitAnimFramesSync is the clip and count the field was last filled from,
	// so a number being typed over is not taken away by the next frame.
	UnitAnimFramesEd       widget.Editor
	UnitAnimFramesSetClick widget.Clickable
	UnitAnimFramesSync     string
	UnitAnimFramesErr      string
	// unitListMode picks the list on show: 0 animations, 1 markers, 2 bones.
	UnitListMode int
	// unitViewportScroll is the side panel's scrollbar, and unitOpenModelClick
	// opens the .whm in a tab of its own, which is the way to reach the
	// converter from the object window.
	UnitViewportScroll panelScrollState
	UnitOpenModelClick widget.Clickable

	// "Import frame's timeline": a keyframe clipboard read out of another
	// .whm. unitImpClick is the button in the pose bar, unitImpDialog the
	// file dialog it opens, and unitImpModel the source .whm once picked —
	// the section below the keyframe list browses its clips and frames.
	UnitImpClick        widget.Clickable
	UnitImpStatus       string
	UnitImpModel        *WhmModel
	UnitImpPath         string
	UnitImpOpen         []bool // per source animation: expanded
	UnitImpAnimClicks   []widget.Clickable
	UnitImpFrameClicks  map[unitImpRef]*widget.Clickable
	UnitImpSel          map[unitImpRef]bool
	UnitImpAnchor       unitImpRef
	UnitImpCopyClick    widget.Clickable
	UnitImpReplaceClick widget.Clickable

	// Events section state: the selected model's .whe parsed into an editable
	// document, which carries the event definitions (DATAEVNT) and the
	// animation timelines (DATAMTON) that fire them at a moment in a clip.
	// unitEvtTree/unitEvtFor say which tree and which unit the document was
	// read for, so switching units or reloading a module re-reads it rather
	// than editing a stale copy.
	UnitEvtTree  *modtree.ModTree
	UnitEvtFor   string // the unit .rgd path the document was loaded for
	UnitEvtPath  string // virtual path of the .whe being edited
	UnitEvtDoc   *whe.Document
	UnitEvtTried bool // this unit was already attempted, so a missing .whe is not re-read every frame
	UnitEvtErr   string
	// unitEvtEntryEds are the value fields of the selected event's
	// definition, sized to its entries, and unitEvtEntryBound says which
	// definition they were last filled from. Enter in a field — or the Set
	// button, which does not depend on the field having focus — writes the
	// value into the in-memory document, and the viewport re-resolves from
	// the document so the edit shows before it is saved. unitEvtEntryFillText
	// is what each field showed when filled, so Set writes only what was
	// typed over.
	UnitEvtEntryEds      []widget.Editor
	UnitEvtEntryBound    string
	UnitEvtEntryFillText []string
	UnitEvtEntrySetClick widget.Clickable
	UnitEvtDirty         bool
	UnitEvtTimeline      string // selected timeline (DATAMTON) name
	UnitEvtSel           int    // selected event row of that timeline
	UnitEvtList          layout.List
	UnitEvtScroll        panelScrollState
	UnitEvtPanelScroll   panelScrollState
	UnitEvtActionsScroll panelScrollState
	UnitEvtActionSel     int // selected action of the DATAACTS list
	UnitEvtActionClicks  []widget.Clickable
	// unitEvtCopyAct and unitEvtPasteAct move one action of the DATAACTS
	// section between units, the way unitEvtCopyTl moves a whole timeline.
	UnitEvtCopyAct  widget.Clickable
	UnitEvtPasteAct widget.Clickable
	// unitActNameEd and unitActRename rename the picked action. A copied action
	// pastes under its own name, so renaming is how a name this .whe already
	// uses is resolved without hand-editing the file. unitActNameBound is the
	// action the field was last filled from.
	UnitActNameEd    widget.Editor
	UnitActNameBound string
	UnitActRename    widget.Clickable
	// unitEvtFocusAction says the reader is working on an action rather than a
	// timeline: the panel then shows the picked action's conditions and none of
	// the timeline's events, since the two are one panel and not two stacked.
	UnitEvtFocusAction bool
	UnitEvtTlClicks    []widget.Clickable
	UnitEvtClicks      []widget.Clickable
	// unitEvtAnimClicks are the per-row Remove buttons of the selected
	// timeline's animation list — one per Animations entry — so each blend
	// entry can be dropped individually.
	UnitEvtAnimClicks []widget.Clickable
	// unitMeshClicks are the eye toggles of the pinned mesh list at the bottom
	// of the Events panel — one per mesh of the unit's model — and
	// unitMeshScroll is that list's own scroller, so a long list scrolls
	// instead of pushing the panel's body away.
	UnitMeshClicks []widget.Clickable
	UnitMeshScroll panelScrollState
	// unitMeshNameClicks are the name half of each mesh row: pressing one
	// and pressing it again inside the window raises that mesh's material
	// editor, the dialog the eye toggle's button sits beside.
	UnitMeshNameClicks []widget.Clickable
	// unitMeshMat* drive the dialog a double press on a mesh row raises:
	// the mesh's material references in editable fields, with unitMeshMatOrig
	// holding the references the file holds so Cancel can put a previewed
	// texture back. Preview applies the fields to the model on show; Save
	// WHM writes them into the file through the .whm material patcher.
	UnitMeshMatOpen    bool
	UnitMeshMatName    string
	UnitMeshMatOrig    []string
	UnitMeshMatEditors []widget.Editor
	UnitMeshMatScroll  panelScrollState
	UnitMeshMatErr     string
	UnitMeshMatDim     widget.Clickable
	UnitMeshMatPreview widget.Clickable
	UnitMeshMatSave    widget.Clickable
	UnitMeshMatCancel  widget.Clickable
	// unitMeshMatCardTag is the area the card covers against the dim, the same
	// guard the Classic OE editor card takes.
	UnitMeshMatCardTag struct{}
	// unitActCondEds are the condition fields of the picked action's choices —
	// one per motion and sub-action link, in file order — with
	// unitActCondStaged holding the guard step each is edited as and
	// unitActCondBound naming the action they were last filled from. Apply
	// writes them into the in-memory document and Save .whe writes that to
	// the file, the same two steps the event form takes.
	UnitActCondEds     []widget.Editor
	UnitActCondStaged  []int32
	UnitActCondBound   string
	UnitActGuardClicks []widget.Clickable
	UnitActSel         int // the choice the chain tools act on: where the caret last was
	UnitActApply       widget.Clickable
	// The chain tools add, move and delete choices with the shape of an
	// if / else if / else chain kept intact: they act on the document the
	// way Apply does, so every step is one Save .whe away.
	UnitActAddIf     widget.Clickable
	UnitActAddElseIf widget.Clickable
	UnitActAddElse   widget.Clickable
	UnitActMoveUp    widget.Clickable
	UnitActMoveDown  widget.Clickable
	UnitActDelete    widget.Clickable
	// unitActNameEds are the target-name fields beside the conditions — one
	// per choice — so a newly added line can name the motion or action it
	// runs.
	UnitActNameEds []widget.Editor
	UnitActErr     string
	// The add form: event name, moment, FX path, sound path and marker. The
	// FX and sound paths are separate fields: an effect and a sound name
	// different things.
	UnitEvtNameEd   widget.Editor
	UnitEvtTimeEd   widget.Editor
	UnitEvtFxEd     widget.Editor
	UnitEvtSndEd    widget.Editor
	UnitEvtMarkerEd widget.Editor
	// unitEvtShiftEd is the batch time shift: seconds added to every selected
	// row, negative to move them earlier. unitEvtShift applies it, and so does
	// Enter in the field; the value is consumed by the press so a second one
	// cannot move the same rows twice.
	UnitEvtShiftEd widget.Editor
	// evtPick is the list dialog an event field opens on a click.
	EvtPick evtPicker
	// unitEvtBound is which timeline/row the editors were last filled from,
	// so a selection change refills them without touching what is being typed.
	UnitEvtBound    string
	UnitEvtFormErr  string
	UnitEvtAddEvent widget.Clickable
	UnitEvtAddFX    widget.Clickable
	UnitEvtAddSound widget.Clickable
	UnitEvtRemove   widget.Clickable
	// unitEvtSet and unitEvtApply are the form's two commit buttons: both
	// write the typed fields into the selected event in memory, and Save .whe
	// writes that to the file.
	UnitEvtSet    widget.Clickable
	UnitEvtApply  widget.Clickable
	UnitEvtShift  widget.Clickable
	UnitEvtSave   widget.Clickable
	UnitEvtRevert widget.Clickable
	// unitEvtSelSet is the whole selected group of event rows — Ctrl+click
	// toggles one, Shift+click takes the range from unitEvtSelAnchor — and it
	// is what Copy, Remove and the row highlight act on. unitEvtSel stays the
	// row whose definition and form are shown.
	UnitEvtSelSet    map[int]bool
	UnitEvtSelAnchor int
	// unitClips is the typed copy history shared by the copy buffers. Each
	// Copy adds an entry tagged with what it holds, and Paste takes the newest
	// entry that fits the field it is pressed in.
	UnitClips   clipHistory
	UnitEvtCopy widget.Clickable
	// unitEvtCopyTl copies the whole selected timeline — its events, its clips
	// and their definitions — rather than just the rows picked in it.
	UnitEvtCopyTl widget.Clickable
	UnitEvtPaste  widget.Clickable
	// unitClipCur is the history entry Paste takes when it fits the field; -1
	// when none is chosen. unitClipPrev and unitClipNext step it through the
	// entries that fit the events field.
	UnitClipCur  int
	UnitClipPrev widget.Clickable
	UnitClipNext widget.Clickable
	// unitEvtCtrlHeld and unitEvtShiftHeld mirror the modifier keys while
	// this section is on screen. They are only as good as this section's
	// Ctrl/Shift filters winning those key events, so a click also reads the
	// live OS state through heldModifiers; unitEvtModLog is the dedupe key
	// that keeps the diagnostic line to one per distinct modifier event.
	UnitEvtCtrlHeld  bool
	UnitEvtShiftHeld bool
	UnitEvtModLog    string
	// lastFormAction is the kind of the most recent operation the events form
	// attempted ("fx", "sound" or "remove"), recorded as it happens; the
	// commit of edited fields is applySelectedEventForm's job.
	LastFormAction string
	// evtPlayer is the Events section's audio: one event's sound auditioned
	// on its own, or the selected timeline's sounds played in order with a
	// playhead, pause/resume and a scrub strip. See unit_evt_play.go.
	EvtPlayer unitEvtPlayer

	// projectDir and unitsDir are this mod's folders under the program's
	// Projects tree, created when the module was opened.
	ProjectDir string
	// projectStatusErr is why the project folder could not be made, held until
	// the status line can say so without the module load being interrupted.
	ProjectStatusErr error
	UnitsDir         string
}

type rgdEditorClickState struct {
	editor *widget.Editor
	click  widget.Clickable
}

// fileTab holds per-opened-file state (RGD or FDA) — one tab per file in the mod tree.
type fileTab struct {
	Path string // virtual path like Data\attrib\... or Data\sound\....fda
	Kind string // "rgd", "text", or "fda" (lowercase ext without dot)

	// rgd state (when kind=="rgd")
	RgdFile             *rgd.RgdFile
	RgdError            string
	RgdExpanded         map[string]bool
	RgdSelected         string // e.g. "GameData", "GameData/health_ext"
	RgdMidList          layout.List
	RgdMidScroll        panelScrollState
	RgdMidNodes         []rgdVisible
	RgdMidClicks        []widget.Clickable
	RgdChildList        layout.List
	RgdChildScroll      panelScrollState
	RgdDirty            bool
	RgdRefEditor        widget.Editor
	RgdRefBound         string                        // which entry path the ref editor is bound to
	RgdValEditors       map[string]*widget.Editor     // key = RGD path; scalar values use the text editor
	RgdValErrors        map[string]string             // key = RGD path; invalid scalar text
	RgdValClicks        map[string]*widget.Clickable  // Bool toggles
	RgdSuggestionClicks map[string][]widget.Clickable // inline string suggestion choices
	RgdRefClick         widget.Clickable              // apply ref
	RgdApplyClick       widget.Clickable              // apply pending scalar value
	RgdBlank            widget.Clickable              // eats clicks so editor doesn't lose focus to tree

	// text state (when kind=="text": .rat, .con, .lua, .ai, and .ucs)
	TextEditor   widget.Editor
	TextOriginal string
	TextDirty    bool
	TextError    string
	TextBytes    int
	// textLocale marks a tab that holds a game's .ucs file rather than a mod
	// file. Those live outside the mod tree, so there is nowhere to write an
	// edit back to; the tab is read only and says so.
	TextLocale bool

	// rgdLocTags are the pointer targets of the Alt+click rows under a
	// DT_WString value, keyed the same way as rgdValEditors. They live here
	// because a press has to be caught on the row itself to see whether Alt
	// was held, and a click target cannot be reused across rows.
	RgdLocTags map[string]event.Tag

	// fda state (when kind=="fda")
	FdaFile        *fda.FDAFile
	FdaError       string
	FdaPcmBytes    []byte // decoded 16-bit LE interleaved
	FdaSampleRate  int
	FdaChannels    int
	FdaBytesPerSec int64
	FdaTotal       time.Duration
	FdaChunkBuf    *audioplayer.ChunkBuffer
	FdaSrc         *audioplayer.ChunkSource
	FdaPlayer      *oto.Player
	FdaPaused      bool
	FdaRepeat      bool
	FdaVolumePct   int // 0..200
	FdaExporting   bool
	FdaExportDone  string
	FdaSeekFloat   widget.Float
	FdaVolFloat    widget.Float
	FdaPlayPause   widget.Clickable
	FdaBack10      widget.Clickable
	FdaFwd10       widget.Clickable
	FdaBack30      widget.Clickable
	FdaFwd30       widget.Clickable
	FdaRepeatBtn   widget.Clickable
	FdaExportBtn   widget.Clickable
	FdaStopBtn     widget.Clickable

	// waveform (derived from fdaPcmBytes, lazy-built on demand)
	FdaWavePeaks     []byte // 0..255 per column, one byte per x-pixel at last build
	FdaWaveW         int    // width the peaks were built for
	FdaWaveClick     widget.Clickable
	FdaWaveFloat     widget.Float // optional: we use raw pointer drag instead, but keep for compat
	FdaWaveDragging  bool
	FdaWaveHoverFrac float32 // mouse hover frac for tooltip if needed

	// whm state (when kind=="whm")
	WhmInfo         *whm.ModelInfo
	WhmError        string
	WhmBytes        int
	WhmConverting   bool
	WhmExportDone   string
	WhmConvertClick widget.Clickable
	// whmIsSgm says the model tab was opened from an Object Editor .sgm
	// rather than a game .whm. The two share this state — both are models
	// with the same content — but only the .whm can be converted.
	WhmIsSgm bool

	// sga archive listing (when kind=="sga"): what an opened .sga carries,
	// flattened to one row per folder and file. Read-only.
	SgaRows   []sgaRow
	SgaError  string
	SgaList   layout.List
	SgaScroll panelScrollState

	// sgb state (when kind=="sgb"): a scenario map with editable placements.
	SgbMap            *sgb.Map
	SgbDirty          bool
	SgbCameraTag      event.Tag  // arrow-key focus while the 3D map view is active
	SgbCameraKeys     [4]bool    // held Up, Down, Left, Right arrows
	SgbCameraVelocity [2]float64 // forward and strafe velocity, in normalized axes
	SgbCameraLastMove time.Time
	SgbError          string
	SgbBytes          int
	SgbImage          *image.NRGBA
	SgbMask           uint8 // sgbOver* bits currently baked into sgbImage
	SgbToggleTypes    widget.Clickable
	SgbTogglePass     widget.Clickable
	SgbToggleDecals   widget.Clickable
	SgbToggleEntities widget.Clickable
	// sgbView3D trades the flat map image for the terrain mesh; the switch
	// is the button that does it.
	SgbView3D     bool
	SgbViewSwitch widget.Clickable
	// Entity selection belongs to its map tab so later edit operations can use
	// stable scenario entity IDs rather than renderer mesh indices.
	SgbSelectedEntities map[int]bool
	SgbSelectionAnchor  image.Point
	SgbSelectionCurrent image.Point
	SgbSelecting        bool
	SgbSelectionDrag    bool

	SgbObjectDragging bool
	SgbObjectRotating bool

	SgbDragHasPoint bool
	SgbDragLast     fauxgl.Vector
	SgbRotateLastX  float32
	SgbObjectMoved  bool

	// sgbEntityMeshIDs is delivered with the asynchronously built map scene.
	SgbEntityMeshIDs    map[string]int
	SgbAnimatedEntities []sgb.AnimatedEntity

	SgbObjectRoot             *modtree.TreeFolder
	SgbObjectExpanded         map[string]bool
	SgbObjectClicks           map[string]*widget.Clickable
	SgbObjectList             layout.List
	SgbObjectScroll           panelScrollState
	SgbObjectClear            widget.Clickable
	SgbPlayerList             *widget.Enum
	SgbPlayerSet              widget.Clickable
	SgbObjectSelected         string
	SgbObjectModelPath        string
	SgbObjectModelFile        string
	SgbObjectModel            *whm.Model
	SgbObjectModelCh          chan sgbObjectModelResult
	SgbObjectMaterialsCh      chan sgbObjectMaterialsResult
	SgbObjectMaterialsActive  sgbObjectMaterialRequest
	SgbObjectMaterialsPending []sgbObjectMaterialRequest
	SgbObjectMaterialsLoaded  map[string]bool
	SgbObjectMaterialsCache   map[string]sgbObjectMaterialsResult
	SgbEntityModelFiles       map[int]string
	SgbPlacementPending       *sgbPlacementRequest
	SgbNextEntityID           int

	// 3D preview state (when kind=="whm" and a model was decoded)
	Model             *modelViewer
	ModelMaterialCh   chan modelMaterialResult
	ModelListMode     int
	ModelListClicks   [4]widget.Clickable
	ModelListScroll   panelScrollState
	ModelAnimClicks   []widget.Clickable
	ModelMarkerClicks []widget.Clickable
	ModelBoneClicks   []widget.Clickable
	ModelFXClicks     []widget.Clickable
	ModelPlay         widget.Clickable
	ModelPrevAnim     widget.Clickable
	ModelNextAnim     widget.Clickable
	ModelStepBack     widget.Clickable
	ModelStepFwd      widget.Clickable
	ModelReset        widget.Clickable
	ModelSkeleton     widget.Clickable
	ModelFX           widget.Clickable
	// modelSFX turns the model's own sound on and off. It is a button rather
	// than nothing at all because a preview that starts making noise the
	// moment a model opens is a preview nobody can look at, and one that
	// cannot be stopped is worse than that.
	ModelSFX      widget.Clickable
	ModelGround   widget.Clickable
	ModelTexture  widget.Clickable
	ModelZoomIn   widget.Clickable
	ModelZoomOut  widget.Clickable
	ModelUVRotate widget.Clickable
	ModelUVMirror widget.Clickable
	ModelUVReset  widget.Clickable
	// image state (when kind=="image")
	ImageData       image.Image
	ImageMipmaps    []image.Image
	ImageMipLevel   int
	ImageError      string
	ImageBytes      int
	ImageFormat     string
	ImageLoading    bool
	ImageDecodeCh   chan imageDecodeResult
	ImageDecodeStop context.CancelFunc
	ImageFit        bool
	ImageZoom       float32
	ImageZoomIn     widget.Clickable
	ImageZoomOut    widget.Clickable
	ImageFitClick   widget.Clickable
	ImageMipPrev    widget.Clickable
	ImageMipNext    widget.Clickable

	// wav state (when kind=="wav")
	WavData         *wav.Info
	WavError        string
	WavExporting    bool
	WavExportDone   string
	WavConvertClick widget.Clickable
	WavPlayClick    widget.Clickable
	WavStopClick    widget.Clickable
	WavRepeatClick  widget.Clickable
	WavRepeat       bool
	WavVolumePct    int
	WavVolFloat     widget.Float
	WavSeekFloat    widget.Float
	WavWavePeaks    []byte
	WavWaveW        int
	WavPlayer       *oto.Player
	WavSrc          *audioplayer.ChunkSource
	WavPaused       bool
	WavBytesPerSec  int64
	WavTotal        time.Duration
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
	s := &uiState{Th: th, MenuOpen: -1, TabSel: 0, DarkTheme: true, GpuAPI: render.DefaultGPUAPI, KeyboardArea: "files", ImageCache: make(map[string]imageCacheEntry), ModRefreshCh: make(chan modCatalogRefreshResult, 1), ModFileWatchSignal: make(chan struct{}, 1), StateDirty: true, LeftTreePointerTag: newPointerTag("left-tree"), TabBarPointerTag: newPointerTag("tab-bar"), Locale: localeDefault}
	applyMaterialTheme(th, true)
	s.MenuClicks = make([]widget.Clickable, len(menus))
	s.MenuItemClicks = make([][]widget.Clickable, len(menus))
	for i := range menus {
		s.MenuItemClicks[i] = make([]widget.Clickable, len(menus[i].items))
	}
	s.Expanded = map[string]bool{"Data": true}
	s.NewObjNameEditor.SingleLine = true
	s.NewObjNameEditor.Submit = true
	s.SettingsGameEditor.SingleLine = true
	s.SettingsGameEditor.Submit = true
	s.SettingsFormatEditor.SingleLine = true
	s.SettingsFormatEditor.Submit = true
	// The frames field beside the shift buttons in the bone pose bar: a
	// whole number, with 1 standing in when it is empty or unreadable.
	s.UnitBoneKeyShiftEd.SingleLine = true
	s.UnitBoneKeyShiftEd.Submit = true
	s.UnitBoneKeyShiftEd.SetText("1")
	// The formats in force before any state file is read, so a frame laid
	// out before the settings are applied already opens the built-in list.
	s.TextEditorFormats = append([]string(nil), defaultTextFormats...)
	s.SearchEditor.SingleLine = true
	s.SearchEditor.Submit = true
	s.SearchKind = modtree.SearchAll
	s.SearchKindClicks = make([]widget.Clickable, len(modtree.SearchKinds))
	EnsureUnitClicks(s)
	s.TabClicks = make([]widget.Clickable, 1) // Module only
	s.TabCloses = nil
	s.OpenTabs = nil
	return s
}

// ── tab helpers ─────────────────────────────────────────────────────────────

func EnsureTabClicks(ui *uiState) {
	need := 1 + len(ui.OpenTabs)
	if len(ui.TabClicks) < need {
		old := ui.TabClicks
		n := make([]widget.Clickable, need)
		copy(n, old)
		ui.TabClicks = n
	} else if len(ui.TabClicks) > need {
		// shrink handled explicitly in closeTabAt; if reached here via unexpected path, truncate preserving Module+prefix
		ui.TabClicks = ui.TabClicks[:need]
	}
	if len(ui.TabCloses) < len(ui.OpenTabs) {
		old := ui.TabCloses
		n := make([]widget.Clickable, len(ui.OpenTabs))
		copy(n, old)
		ui.TabCloses = n
	} else if len(ui.TabCloses) > len(ui.OpenTabs) {
		ui.TabCloses = ui.TabCloses[:len(ui.OpenTabs)]
	}
	if ui.TabSel < 0 {
		ui.TabSel = 0
	}
	if ui.TabSel > len(ui.OpenTabs) {
		ui.TabSel = len(ui.OpenTabs)
	}
}

func FindTabIndex(ui *uiState, path string) int {
	for i, t := range ui.OpenTabs {
		if t.Path == path {
			return i
		}
	}
	return -1
}

func ActiveTab(ui *uiState) *fileTab {
	if ui.TabSel <= 0 || ui.TabSel > len(ui.OpenTabs) {
		return nil
	}
	return ui.OpenTabs[ui.TabSel-1]
}

func IsModuleActive(ui *uiState) bool { return ui.TabSel == 0 }

func FdaActive(ui *uiState) bool {
	t := ActiveTab(ui)
	if t == nil {
		return false
	}
	return t.Kind == "fda" && (t.FdaFile != nil || t.FdaError != "")
}
func RgdActive(ui *uiState) bool {
	t := ActiveTab(ui)
	if t == nil {
		return false
	}
	return t.Kind == "rgd" && (t.RgdFile != nil || t.RgdError != "")
}

// releaseTabResources hands back everything a tab holds outside the tab list:
// a running decode, the 3D preview's buffers, any audio player, and the
// dropdowns that belong to the tab. Closing one tab and closing them all both
// go through it, so a tab can never be dropped with something of it still
// running — which is what used to happen when they were closed in bulk.
func ReleaseTabResources(ui *uiState, t *fileTab) {
	if t == nil {
		return
	}
	// A dropdown belongs to the tab that is going away; leaving it open would
	// draw it over whatever takes its place.
	if isUnitTab(t) {
		ui.UnitRaceOpen = false
		ui.UnitListOpen = false
	}
	if t.ImageDecodeStop != nil {
		t.ImageDecodeStop()
		t.ImageDecodeStop = nil
	}
	// The 3D preview renders on a worker; drop the reference so a pending
	// channel write cannot keep the model alive after the tab is gone.
	if t.Model != nil {
		// The hardware renderer caches the model's buffers, which go back to
		// the driver along with the tab. Only an existing handle is asked, so
		// closing a tab never brings one up.
		if ui.ModelGpu != nil && t.Model.Model != nil {
			ui.ModelGpu.Forget(&t.Model.Model.Model)
		}
		t.Model = nil
	}
	if t.Kind == "fda" && t.FdaPlayer != nil {
		t.FdaPlayer.PauseAndStopReading()
		t.FdaPlayer = nil
	}
	if t.Kind == "wav" && t.WavPlayer != nil {
		t.WavPlayer.PauseAndStopReading()
		t.WavPlayer = nil
	}
}

func CloseTabAt(ui *uiState, idx int) {
	if idx < 0 || idx >= len(ui.OpenTabs) {
		return
	}
	t := ui.OpenTabs[idx]
	ReleaseTabResources(ui, t)
	oldSel := ui.TabSel
	ui.OpenTabs = append(ui.OpenTabs[:idx], ui.OpenTabs[idx+1:]...)
	// splice clickables to keep per-tab button state aligned
	// tabClicks: [0]=Module, [1]=tab0, [2]=tab1, ...
	if idx+1 < len(ui.TabClicks) {
		ui.TabClicks = append(ui.TabClicks[:idx+1], ui.TabClicks[idx+2:]...)
	}
	if idx < len(ui.TabCloses) {
		ui.TabCloses = append(ui.TabCloses[:idx], ui.TabCloses[idx+1:]...)
	}
	newLen := len(ui.OpenTabs)
	if oldSel == idx+1 {
		ui.TabSel = oldSel
		if ui.TabSel > newLen {
			ui.TabSel = newLen
		}
	} else if oldSel > idx+1 {
		ui.TabSel = oldSel - 1
		if ui.TabSel > newLen {
			ui.TabSel = newLen
		}
	} else {
		if ui.TabSel > newLen {
			ui.TabSel = newLen
		}
	}
	EnsureTabClicks(ui)
}

func CloseActiveFileTab(ui *uiState) {
	if ui.TabSel == 0 {
		return
	}
	CloseTabAt(ui, ui.TabSel-1)
}

func CloseAllFileTabs(ui *uiState) {
	for _, t := range ui.OpenTabs {
		ReleaseTabResources(ui, t)
	}
	// The object window's viewport is not a file tab, but it holds the same
	// viewer, and the mod it reads from is about to be closed underneath it.
	ResetUnitModel(ui)
	ui.OpenTabs = nil
	ui.TabSel = 0
	// The bar is empty now, so any scroll offset into it is meaningless and
	// has to go back to the left.
	ui.TabScrollX = 0
	CloseTabContextMenu(ui)
	EnsureTabClicks(ui)
}

func ClearRgd(ui *uiState) {
	t := ActiveTab(ui)
	if t != nil && t.Kind == "rgd" {
		CloseTabAt(ui, ui.TabSel-1)
	}
}
func ClearFda(ui *uiState) {
	t := ActiveTab(ui)
	if t != nil && t.Kind == "fda" {
		CloseTabAt(ui, ui.TabSel-1)
		return
	}
}
func CloseActiveViewer(ui *uiState) { CloseActiveFileTab(ui) }

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

func Run() {
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
			log.Printf("help shown, exiting 0; log=%q", applog.LogFilePath)
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
	ui.Win = w
	var ops op.Ops

	state, stateExists, stateErr := loadPersistedState()
	if stateErr != nil {
		log.Printf("load state: %v", stateErr)
	}
	if stateExists {
		ApplyPersistedSettings(ui, state)
	}
	// Gio creates its window GPU on the first frame. Install the persisted
	// choice before receiving that frame; otherwise Windows' Vulkan driver
	// sees an empty DOW_GPU_API and falls through to EGL.
	if err := render.SetGPUAPI(ui.GpuAPI); err != nil {
		return err
	}

	// An explicit module argument always wins over the saved session.
	if findFirstModuleArg() != "" {
		TryAutoLoad(ui)
	} else if !stateExists || !RestorePersistedState(ui, state) {
		TryAutoLoad(ui)
	}
	// A file the OS handed over (a registered association) opens once the
	// module is in place: the .whe lookup needs the unit catalog, and the
	// viewers read through the tree where the file lives in one.
	if arg := findFirstAssocArg(); arg != "" {
		OpenAssociatedFile(ui, arg)
	}

	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			if err := savePersistedState(ui); err != nil {
				log.Printf("save state on exit: %v", err)
			}
			StopModFileWatcher(ui)
			CloseAllFileTabs(ui)
			if ui.ModTree != nil {
				ui.ModTree.Close()
			}
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			MaybeSaveState(ui)
			PollModCatalog(ui)
			PollImageDecodes(ui)
			PollModelJobs(ui)
			PollUnitModel(ui)
			HandleInput(ui, gtx)
			Layout(ui, gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func SyncWindowTitle(ui *uiState) {
	if ui.Win == nil {
		return
	}
	if ui.ModulePath != "" {
		ui.Win.Option(app.Title(fmt.Sprintf("%s — DoW Mod Studio", filepath.Base(ui.ModulePath))))
	} else {
		ui.Win.Option(app.Title("DoW Mod Studio — No module"))
	}
}

func TryAutoLoad(ui *uiState) {
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
			LoadModule(ui, candidate)
			return
		}
		// If arg looked like .module but file not found, show error and don't fall back silently
		if strings.EqualFold(filepath.Ext(arg), ".module") {
			ui.StatusText = fmt.Sprintf("Module not found: %s — File → Open Module…", arg)
			log.Printf("tryAutoLoad: arg %q resolved to %q not found", arg, candidate)
			SyncWindowTitle(ui)
			return
		}
	}

	candidates := []string{}
	if ui.GamePath != "" {
		candidates = append(candidates, filepath.Join(ui.GamePath, "DXP3.module"), filepath.Join(ui.GamePath, "W40k.module"))
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
			LoadModule(ui, p)
			return
		}
	}
	ui.StatusText = "No module loaded — File → Open Module…"
	SyncWindowTitle(ui)
}

func LoadModule(ui *uiState, path string) {
	// stop audio and close all file tabs before tearing down tree
	StopModFileWatcher(ui)
	CloseAllFileTabs(ui)
	ClearImageCache(ui)
	if ui.ModTree != nil {
		ui.ModTree.Close()
		ui.ModTree = nil
	}
	// The previous mod's strings must stop being searched for before the new
	// mod's folder is known, or a table built now would still carry them.
	ui.ProjectDir = ""
	setLocaleOverrideDir("")
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	// clear previous state
	ui.Expanded = map[string]bool{"Data": true}
	ui.SelectedPath = ""
	ui.SelectedTreePath = "Data"
	ui.KeyboardArea = "files"
	ui.VisibleNodes = nil
	ui.RowClicks = nil

	// The hashed field names inside a .rgd resolve through the game's own
	// dictionary, which ships in the game folder rather than beside the
	// executable; the hard coded Windows install path in InitDict is only a
	// last resort. The module sits in the game folder, so its directory says
	// where to look before anything parses a .rgd.
	rgd.AddDictRoot(filepath.Dir(path))
	tree, err := modtree.BuildModTree(path, "English")
	if err != nil {
		ui.StatusText = fmt.Sprintf("Failed to load %s: %v", filepath.Base(path), err)
		log.Printf("loadModule %q: %v", path, err)
		if tree != nil {
			tree.Close()
		}
		ui.ModulePath = ""
		SyncWindowTitle(ui)
		return
	}
	ui.ModTree = tree
	ui.ModulePath = path
	if tree.AppPath != "" {
		ui.GamePath = tree.AppPath
	}
	RememberRecentFolder(ui, filepath.Dir(path))
	RememberRecentModule(ui, path)
	ui.ModSnapshot = currentModSnapshot(tree)
	StartModFileWatcher(ui, tree)
	ui.LastModPoll = time.Now()
	// The mod's own project folder has to exist and be known before the string
	// table is built, because it is searched first: a localisation written into
	// it has to be in the table the same moment it is written. A failure here
	// must not stop the module from opening, so it is logged and the message
	// folded into the status line instead.
	if dir, perr := ensureProjectDirs(tree, path); perr != nil {
		log.Printf("projects folder for %s: %v", filepath.Base(path), perr)
		ui.ProjectStatusErr = perr
	} else {
		ui.ProjectDir = dir
		ui.UnitsDir = filepath.Join(dir, unitsDirName)
		ui.ProjectStatusErr = nil
	}
	setLocaleOverrideDir(ui.ProjectDir)
	// A module brings its own Locale folder, and can add languages the engine
	// does not ship, so the picker is rediscovered with the module in place.
	// It also rebuilds the string table for the language already chosen.
	RefreshLocales(ui)
	// clear viewers (already closed)
	CloseAllFileTabs(ui)
	// expand Data only by default (user can drill down)
	ui.Expanded = map[string]bool{"Data": true}
	f, fi := tree.Count()
	ui.StatusText = fmt.Sprintf("Loaded %s — %d folders, %d files (%d sources)", filepath.Base(path), f, fi, len(tree.Sources))
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
			ui.Expanded[c.FullPath] = true
		}
	}
	// The result list points at files of the previous module, so drop it.
	ui.SearchHits = nil
	ui.SearchSel = 0
	RunSearch(ui, true)
	if ui.ProjectStatusErr != nil {
		ui.StatusText += fmt.Sprintf(" — projects folder unavailable: %v", ui.ProjectStatusErr)
	}
	SyncWindowTitle(ui)
	MarkStateDirty(ui)
}

// ── top-level layout ─────────────────────────────────────────────────────────

func Layout(ui *uiState, gtx layout.Context) layout.Dimensions {
	// window bg
	fillRect(gtx, cBg, gtx.Constraints.Max)

	// keep visibleNodes up to date for Layout to render
	ui.VisibleNodes = GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(ui.VisibleNodes))
	EnsureRowHover(ui, len(ui.VisibleNodes))

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutMenuBar(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cBorder, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(ui.OpenTabs) == 0 {
						return layout.Dimensions{}
					}
					return LayoutTabBar(ui, gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(ui.OpenTabs) == 0 {
						return layout.Dimensions{}
					}
					return hLine(gtx, cBorder, 1)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutContent(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutStatusBar(ui, gtx) }),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.MenuOpen < 0 {
				return layout.Dimensions{}
			}
			return LayoutMenuDropdown(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions { return LayoutSearchOverlay(ui, gtx) }),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions { return LayoutLocaleOverlay(ui, gtx) }),
		// The tab context menu is the last thing drawn, so it sits above the
		// bar it was opened from and above any other menu that is up.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.TabContextVisible {
				return layout.Dimensions{}
			}
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					pass := pointer.PassOp{}.Push(gtx.Ops)
					dims := ui.TabContextDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: gtx.Constraints.Max}
					})
					pass.Pop()
					return dims
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions { return LayoutTabContextMenu(ui, gtx) }),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.FilePickOpen {
				return layout.Dimensions{}
			}
			return LayoutFilePicker(ui, gtx)
		}),
		// The event picker sits above the import dialog: an event field
		// opens it from inside the Events section.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.EvtPick.open {
				return layout.Dimensions{}
			}
			return LayoutEvtPicker(ui, gtx)
		}),
		// The new unit dialog is above the open dialog: it is opened from the
		// object window, and a dialog that writes a file should be the one
		// thing the window is answering to.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.NewObjDialog {
				return layout.Dimensions{}
			}
			return LayoutNewObjDialog(ui, gtx)
		}),
		// The localisation editor is above the other dialogs: it is opened
		// from inside the object window, so anything already up would have to
		// be let go of first.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.LocaleEditDialog {
				return layout.Dimensions{}
			}
			return LayoutLocaleEditDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.SettingsDialog {
				return layout.Dimensions{}
			}
			return LayoutSettingsDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.FileDeleteDialog {
				return layout.Dimensions{}
			}
			return LayoutFileDeleteDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.ClearRecycleDialog {
				return layout.Dimensions{}
			}
			return LayoutClearRecycleDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.ArchiveDialog {
				return layout.Dimensions{}
			}
			return LayoutArchiveDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.RgdTooltipText == "" {
				return layout.Dimensions{}
			}
			return layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: cBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), ui.RgdTooltipText)
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

func LayoutMiddlePanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	tab := ActiveTab(ui)
	if tab != nil && tab.Kind == "fda" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutFdaPlayer(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "wav" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWavViewer(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "whm" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWhmViewer(ui, gtx, tab) }),
			)
		})
	}
	// Scenario map (.sgb) viewer
	if tab != nil && tab.Kind == "sgb" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutSgbViewer(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "image" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutImageViewer(ui, gtx, tab) }),
			)
		})
	}
	// Archive listing (.sga)
	if tab != nil && tab.Kind == "sga" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutSgaListing(ui, gtx, tab) }),
			)
		})
	}
	// Text/script editor (rat, con, lua, ai)
	if tab != nil && tab.Kind == "text" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutTextEditor(ui, gtx, tab) }),
			)
		})
	}
	// RGD or Module view
	if tab != nil && tab.Kind == "rgd" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if tab.RgdFile == nil {
						if tab.RgdError != "" {
							return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								col := rgb(0xB00020)
								l := material.Label(ui.Th, unit.Sp(11), "Failed: "+tab.RgdError)
								l.Color = col
								return l.Layout(gtx)
							})
						}
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(11), "No RGD — this tab is empty")
							l.Color = cTextDim
							return l.Layout(gtx)
						})
					}
					if len(tab.RgdMidNodes) == 0 {
						RebuildRgdMidNodesFor(ui, tab)
						EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
					}
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return LayoutListWithScrollbar(ui, gtx, &tab.RgdMidScroll, &tab.RgdMidList, 22, len(tab.RgdMidNodes), func(gtx layout.Context, index int) layout.Dimensions {
							v := tab.RgdMidNodes[index]
							return LayoutRgdMidRow(ui, gtx, tab, index, v)
						})
					})
				}),
			)
		})
	}
	// Module tab or empty
	return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, cHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Keep the empty-state message inside the panel with a clear left margin.
					gtx.Constraints.Min.X += gtx.Dp(unit.Dp(16))
					gtx.Constraints.Max.X -= gtx.Dp(unit.Dp(16))
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return centeredPanelLabel(gtx, ui.Th, "No file selected", unit.Sp(11), cTextDim)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return centeredPanelLabel(gtx, ui.Th, "Module tab — click a .rgd/.fda/.jpg/.tga/.dds file in the left tree to open in a new tab", unit.Sp(10), cTextDim)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if len(ui.OpenTabs) > 0 {
								return centeredPanelLabel(gtx, ui.Th, fmt.Sprintf("%d file tab(s) open — switch via the top tab bar", len(ui.OpenTabs)), unit.Sp(10), cTextDim)
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

func LayoutWaveform(ui *uiState, gtx layout.Context, t *fileTab) layout.Dimensions {
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
	dims := t.FdaSeekFloat.Layout(gtx, layout.Horizontal, unit.Dp(4))
	// Apply seek if user dragged/clicked
	FdaHandleSliderSeek(ui, gtx, t)

	// Build peaks if width changed or empty
	if len(t.FdaWavePeaks) != w || t.FdaWaveW != w {
		t.FdaWavePeaks = buildWavePeaks(t.FdaPcmBytes, w)
		t.FdaWaveW = w
	}
	peaks := t.FdaWavePeaks
	frac := t.FdaSeekFloat.Value
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
