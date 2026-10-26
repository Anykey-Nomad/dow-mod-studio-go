// Package uistate holds the State the whole editor shares.
//
// The feature packages below internal/app need these fields, so the types
// cannot stay in package app; app keeps aliases so its own code reads
// unchanged while the UI files move out one category at a time.
package uistate

import (
	"context"
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/3d_viewer"
	"dow-mod-studio/internal/app/fxscene"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/cliphist"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modgraph"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/sgb"
	"dow-mod-studio/internal/unitdef"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"gioui.org/app"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"github.com/ebitengine/oto/v3"
	"github.com/fogleman/fauxgl"
	"github.com/fsnotify/fsnotify"
	"image"
	"math"
	"strings"
	"time"
)

type State struct {
	GpuAPI string

	// forceSoftRenderer keeps the 3D viewport on the software rasteriser.
	ForceSoftRenderer bool

	Th *material.Theme

	MenuOpen       int
	MenuClicks     []widget.Clickable
	MenuItemClicks [][]widget.Clickable
	DismissClick   widget.Clickable   // transparent full-screen catcher
	TabSel         int                // 0=Module, 1..N=file Tab (index+1)
	TabClicks      []widget.Clickable // len = 1+len(openTabs), [0]=Module
	TabCloses      []widget.Clickable // len = len(openTabs), per file Tab close ×
	OpenTabs       []*Tab

	// The Tab row scrolls sideways under the wheel, so the bar can hold more
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
	// which Tab was clicked by position, and a scroll on it moves the row; both
	// are recorded in the bar's own coordinates, which is what pointer.Event
	// reports for a target.
	TabBarPointerTag event.Tag
	// tabRects is where each Tab was last drawn, in bar coordinates, so a
	// right press can be resolved to a Tab without a clickable per Tab that
	// only exists for this.
	TabRects []image.Rectangle
	// The Tab context menu. tabContextPos is where it opens, in window
	// coordinates, and tabContextIndex is the Tab it was opened on. Only
	// "close all tabs" is in it for now, and the item list is a slice so
	// another one is a line here and a row in layoutTabContextMenu.
	TabContextVisible  bool
	TabContextIndex    int
	TabContextPos      image.Point
	TabContextDismiss  widget.Clickable
	TabContextCloseAll widget.Clickable

	ImageCache      map[string]ImageCacheEntry
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
	ModRefreshCh       chan ModCatalogRefreshResult
	ModRefreshRunning  bool
	ModFileWatcher     *fsnotify.Watcher
	ModFileWatchSignal chan struct{}

	LeftPanelWidth  int
	RightPanelWidth int
	PanelSplitters  [2]PanelSplitterState
	LeftScroll      PanelScrollState
	MiddleScroll    PanelScrollState
	RightScroll     PanelScrollState

	// The file dialog every caller shares: Open Module, the timeline import
	// and the event editor's sound field. filePickOpen says it is up.
	FilePick     FilePicker
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
	ArchiveOptions      []ArchiveOption
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
	RgdEditorClicks map[*widget.Editor]*RgdEditorClickState

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
	SearchScroll     PanelScrollState
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
	// the row is laid out, and opening a Tab belongs to the input pass, so the
	// id is left here for the next frame to act on.
	LocalePending string
	// localePendingRequest is the same arrangement for a press that wants the
	// edit dialog rather than a Tab. The field and its key come with it, because
	// a press in the file view already knows which entry it was on and one in
	// the object window has to be looked up again.
	LocalePendingRequest  LocaleEditRequest
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
	// opens the object interface as a Tab of its own, which takes the space of
	// the file tree and the middle panel.
	UnitSection       int
	UnitToolClicks    []widget.Clickable
	UnitSectionClicks []widget.Clickable

	// Classic OE section State: which of the classic editor's header panels
	// are open, their click targets, the scroll of the panel column, and the
	// clip list's own click targets — separate from the Viewport section's, so
	// the two lists cannot both read one press.
	ClassicOEOpen       []bool
	ClassicOEClicks     []widget.Clickable
	ClassicOEScroll     PanelScrollState
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
	// the re-render. Expansion State lives in its own map so that a key's
	// absence can mean "the default" — open for a root action, closed for
	// anything nested.
	ClassicOEFolderOpen   [ClassicOEFolderCount]bool
	ClassicOEFolderInit   bool
	ClassicOEFolderClicks [ClassicOEFolderCount]widget.Clickable
	ClassicOEExpandClicks map[string]*widget.Clickable
	ClassicOEExpandOpen   map[string]bool
	// classicOEJumpClicks is the other press target of a tree row: the
	// marker column toggles a branch, the rest of the row plays the
	// animation the node points at, so the two never share a click.
	ClassicOEJumpClicks map[string]*widget.Clickable

	// The double-click dialog of the Motions and Actions folders: which
	// editor is on show, the item it was opened for, and its own scroll
	// and buttons. It borrows the Events section's editor State — the
	// document, the fields and Save .whe — rather than keeping a second
	// copy of any of it.
	ClassicOEDialog     int // classicOEDialogNone / Timeline / Action
	ClassicOEDialogName string
	ClassicOEevtScroll  PanelScrollState
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
	// carries its own selection. UnitCatalog is built once per mod; the
	// indexes into it are what the pickers and the columns work from, and are
	// clamped rather than trusted, so a stale index after a mod change cannot
	// panic the layout.
	UnitCatalog    *UnitCatalog
	UnitCatalogMod string
	// ModGraph is the same tree read as a graph: every .rgd of Data\attrib
	// as an entity, and every reference a unit or building holds as a link.
	// It is built once per mod and read from the background scans, so it is
	// cached the way the catalogue is and dropped with it.
	ModGraph    *ModGraph
	ModGraphMod string
	// graphLinksCh carries the background scan's batches of references,
	// graphLinksStop ends a scan, graphLinksRunning says one is going, and
	// graphLinksGraph is which graph it is writing into.
	GraphLinksCh      chan GraphLinkBatch
	GraphLinksStop    chan struct{}
	GraphLinksRunning bool
	GraphLinksGraph   *ModGraph
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
	UnitNamesCh      chan UnitNameBatch
	UnitNamesStop    chan struct{}
	UnitNamesTree    *modtree.ModTree
	UnitNamesRunning bool
	UnitNamesDone    bool
	// UnitDetails caches the parsed unit so switching sections does not re-read
	// the file, and unitDetailsPath says which unit it belongs to.
	UnitDetails     *UnitDetails
	UnitDetailsPath string
	// unitIcons caches the decoded object icons of the open mod, keyed by the
	// unit's tree path, and unitIconsMod says which mod they belong to — the
	// same pairing UnitDetails uses, for the same reason.
	UnitIcons    map[string]IconCacheEntry
	UnitIconsMod string
	// actionIcons caches the Classic OE action icons of the open mod, keyed by
	// the action's lower-cased name, and actionIconsMod says which mod they
	// belong to — the same pairing unitIcons uses, for the same reason.
	ActionIcons    map[string]IconCacheEntry
	ActionIconsMod string
	// unitInfoList and unitInfoScroll are the info column's scroll State. The
	// column holds a hundred scalars rather than the dozen it started with, so
	// it scrolls like the other lists in the app.
	UnitInfoList   layout.List
	UnitInfoScroll PanelScrollState
	// unitSquads maps a unit's lower cased tree path to the squads it is part
	// of. The link only exists from the squad's side, so it is built by walking
	// every squad .rgd in the background: unitSquadsCh carries the batches,
	// unitSquadsStop ends a scan a module reload has made pointless, and
	// unitSquadsTree says which tree the running scan belongs to.
	UnitSquads        map[string][]UnitSquadRef
	UnitSquadsCh      chan UnitSquadBatch
	UnitSquadsStop    chan struct{}
	UnitSquadsTree    *modtree.ModTree
	UnitSquadsRunning bool
	UnitSquadsDone    bool
	// unitSquadList and unitSquadScroll are the Squads column's scroll State.
	UnitSquadList   layout.List
	UnitSquadScroll PanelScrollState
	// picker State: whether the race or unit dropdown is open, and the click
	// lists of the dropdown rows.
	UnitRaceOpen   bool
	UnitListOpen   bool
	UnitRaceClicks []widget.Clickable
	UnitClicks     []widget.Clickable
	UnitList       layout.List
	UnitListScroll PanelScrollState
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
	// FreezeBook is the mod's record of what has been frozen, kept because the
	// object window asks about it on every frame and it only changes when a
	// freeze does.
	FreezeBook         *FreezeBook
	FreezeBookMod      string
	NewObjKind         NewObjKind
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
	// is a Tab of its own rather than a field of the object Tab, because
	// the preview is a per unit thing and the object Tab is a singleton: the
	// same Viewer a .whm Tab uses, driven from here.
	UnitModel     *Tab
	UnitModelCh   chan UnitModelResult
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
	// unitMeshListMode is the fifth list Tab of the side panel: the model's
	// meshes with an eye toggle each, beside Animations, Markers, Bones and
	// FX. It shares the side panel's scroll State with the other tabs.
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
	UnitBoneKeySel       map[BoneKeyRef]bool
	UnitBoneKeySelAnchor BoneKeyRef
	UnitBoneKeySelBound  string
	// A pointer press carries no usable modifiers in this panel, so Ctrl+click
	// and Shift+click read the key events unitListTrackModifiers keeps up to
	// date, ORed with the live OS State heldModifiers reports.
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
	UnitBoneUndo            []BoneTrackSnapshot
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
	// opens the .whm in a Tab of its own, which is the way to reach the
	// converter from the object window.
	UnitViewportScroll PanelScrollState
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
	UnitImpFrameClicks  map[UnitImpRef]*widget.Clickable
	UnitImpSel          map[UnitImpRef]bool
	UnitImpAnchor       UnitImpRef
	UnitImpCopyClick    widget.Clickable
	UnitImpReplaceClick widget.Clickable

	// Events section State: the selected model's .whe parsed into an editable
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
	UnitEvtScroll        PanelScrollState
	UnitEvtPanelScroll   PanelScrollState
	UnitEvtActionsScroll PanelScrollState
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
	UnitMeshScroll PanelScrollState
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
	UnitMeshMatScroll  PanelScrollState
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
	EvtPick EvtPicker
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
	UnitClips   ClipHistory
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
	// live OS State through heldModifiers; unitEvtModLog is the dedupe key
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
	EvtPlayer UnitEvtPlayer

	// projectDir and unitsDir are this mod's folders under the program's
	// Projects tree, created when the module was opened.
	ProjectDir string
	// projectStatusErr is why the project folder could not be made, held until
	// the status line can say so without the module load being interrupted.
	ProjectStatusErr error
	UnitsDir         string
}

type Tab struct {
	Path string // virtual path like Data\attrib\... or Data\sound\....fda
	Kind string // "rgd", "text", or "fda" (lowercase ext without dot)

	// rgd State (when kind=="rgd")
	RgdFile             *rgd.RgdFile
	RgdError            string
	RgdExpanded         map[string]bool
	RgdSelected         string // e.g. "GameData", "GameData/health_ext"
	RgdMidList          layout.List
	RgdMidScroll        PanelScrollState
	RgdMidNodes         []RgdVisible
	RgdMidClicks        []widget.Clickable
	RgdChildList        layout.List
	RgdChildScroll      PanelScrollState
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

	// text State (when kind=="text": .rat, .con, .lua, .ai, and .ucs)
	TextEditor   widget.Editor
	TextOriginal string
	TextDirty    bool
	TextError    string
	TextBytes    int
	// textLocale marks a Tab that holds a game's .ucs file rather than a mod
	// file. Those live outside the mod tree, so there is nowhere to write an
	// edit back to; the Tab is read only and says so.
	TextLocale bool

	// rgdLocTags are the pointer targets of the Alt+click rows under a
	// DT_WString value, keyed the same way as rgdValEditors. They live here
	// because a press has to be caught on the row itself to see whether Alt
	// was held, and a click target cannot be reused across rows.
	RgdLocTags map[string]event.Tag

	// fda State (when kind=="fda")
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

	// whm State (when kind=="whm")
	WhmInfo         *whm.ModelInfo
	WhmError        string
	WhmBytes        int
	WhmConverting   bool
	WhmExportDone   string
	WhmConvertClick widget.Clickable
	// whmIsSgm says the model Tab was opened from an Object Editor .sgm
	// rather than a game .whm. The two share this State — both are models
	// with the same content — but only the .whm can be converted.
	WhmIsSgm bool

	// sga archive listing (when kind=="sga"): what an opened .sga carries,
	// flattened to one row per folder and file. Read-only.
	SgaRows   []SgaRow
	SgaError  string
	SgaList   layout.List
	SgaScroll PanelScrollState

	// sgb State (when kind=="sgb"): a scenario map with editable placements.
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
	// Entity selection belongs to its map Tab so later edit operations can use
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
	SgbObjectScroll           PanelScrollState
	SgbObjectClear            widget.Clickable
	SgbPlayerList             *widget.Enum
	SgbPlayerSet              widget.Clickable
	SgbObjectSelected         string
	SgbObjectModelPath        string
	SgbObjectModelFile        string
	SgbObjectModel            *whm.Model
	SgbObjectModelCh          chan SgbObjectModelResult
	SgbObjectMaterialsCh      chan SgbObjectMaterialsResult
	SgbObjectMaterialsActive  SgbObjectMaterialRequest
	SgbObjectMaterialsPending []SgbObjectMaterialRequest
	SgbObjectMaterialsLoaded  map[string]bool
	SgbObjectMaterialsCache   map[string]SgbObjectMaterialsResult
	SgbEntityModelFiles       map[int]string
	SgbPlacementPending       *SgbPlacementRequest
	SgbNextEntityID           int

	// 3D preview State (when kind=="whm" and a model was decoded)
	Model             *Viewer
	ModelMaterialCh   chan ModelMaterialResult
	ModelListMode     int
	ModelListClicks   [4]widget.Clickable
	ModelListScroll   PanelScrollState
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
	// image State (when kind=="image")
	ImageData       image.Image
	ImageMipmaps    []image.Image
	ImageMipLevel   int
	ImageError      string
	ImageBytes      int
	ImageFormat     string
	ImageLoading    bool
	ImageDecodeCh   chan ImageDecodeResult
	ImageDecodeStop context.CancelFunc
	ImageFit        bool
	ImageZoom       float32
	ImageZoomIn     widget.Clickable
	ImageZoomOut    widget.Clickable
	ImageFitClick   widget.Clickable
	ImageMipPrev    widget.Clickable
	ImageMipNext    widget.Clickable

	// wav State (when kind=="wav")
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

type Viewer struct {
	Model                *WhmModel
	Materials            map[string]render.WhmMaterial
	MaterialAliases      map[string]string
	Anim                 *WhmAnimation
	Anims                []*WhmAnimation
	AnimPlaybackDuration float64
	AnimatedEntities     []sgb.AnimatedEntity
	SgbAnimLast          time.Time
	// poseCache is the pose at poseTime of poseAnim, kept so a frame that
	// asks for the pose more than once walks the skeleton once.
	PoseCache          WhmPose
	PoseAnim           *WhmAnimation
	PoseTime           float32
	PoseValid          bool
	AnimIndex          int
	Playing            bool
	Time               float32 // normalised 0..1 within the clip
	ShowSkeleton       bool
	ShowSelection      bool
	SelectionIndicator *engine.SelectionIndicator
	Textured           bool
	// frame is the last software render, painted by the canvas.
	Frame *image.RGBA
	// frameCh delivers the render in flight; serial orders renders so a late
	// frame never replaces a newer one, and pending records a camera move that
	// arrived while a render was running.
	FrameCh chan ModelFrame
	Serial  uint64
	Pending bool
	// painted is the serial of the last software frame the canvas scheduled a
	// paint for, so a frame that lands between layouts gets one.
	Painted uint64
	// rendering is set while a frame is being produced.
	Rendering bool
	// gpuBroken and gpuFrame belong to the hardware path. gpuFrame is the last
	// frame drawn on the GPU; it lives in video memory, so painting it is a
	// hand over of a device handle rather than a copy. gpuRetries counts
	// renders deferred by a transient backend refusal (see modelGpuErrTransient):
	// past modelGpuRetryLimit the hardware path is given up on as if it had
	// failed outright.
	GpuBroken  bool
	GpuFrame   render.Frame
	GpuRetries int
	// GpuProbeW/H and gpuProbedTextured record which GPU frames have already
	// been read back, so the readback runs on the transitions that matter only.
	GpuProbeW, GpuProbeH int
	GpuProbedTextured    bool
	// rshData holds raw .rsh bytes for the Vulkan block-compressed path.
	RshData map[string][]byte
	// materialDiagnostics records where each map came from and which image
	// encoding decoded successfully, keyed by the model's material reference.
	MaterialDiagnostics map[string]ModelMaterialDiagnostic
	// sideControls puts the playback strip beside the canvas instead of under
	// it, which is what the object window's viewport wants: its canvas is a
	// third of the window wide and a strip of eight buttons does not fit under
	// it.
	SideControls bool

	viewer.CameraState
	// uv is the mapping every material is drawn with, and uvMaterials holds a
	// mapping of its own for the ones that need a different one. It is a
	// reading of the model rather than a setting of the app: a map can be
	// authored the wrong way round, and nothing in the file says so.
	Uv          render.UVMapping
	UvMaterials map[string]*render.UVMapping
	// uvSelection names the material the mapping controls are editing, and is
	// empty for the one they all share. uvSelectionDirty says the controls
	// still show the previous selection's numbers, which is what stops the
	// sliders being rewritten from a mapping before the reader has been shown
	// which one they are now looking at. uvTextureClicks are the buttons of
	// the texture list, sized as the panel is laid out.
	UvSelection      string
	UvSelectionDirty bool
	UvTextureClicks  []widget.Clickable
	// The three sliders are the widget's own 0..1 value rather than a float
	// the panel and the renderer both write to, so there is no second copy to
	// fall out of step; the real numbers are read out of them by uvPull.
	UvU, UvV, UvScale widget.Float
	// The rest of the mapping. The three sliders are the widget's own 0..1
	// value rather than a float the panel and the renderer both write to, so
	// there is no second copy to fall out of step; the real numbers are read
	// out of them by render.UVMapping.
	// uvExtent is the range of the model's own coordinates, measured once when
	// the model is opened: [minU, minV, maxU, maxV], with uvExtentOK false
	// for a model with no vertices. It is read out beside the mapping rather
	// than applied to it, so that the numbers a reader is given are the ones
	// the file actually holds.
	UvExtent   [4]float32
	UvExtentOK bool

	// Async material loading and viewport State.
	Loading         bool
	MaterialsLoaded bool
	Missing         int
	RenderW         int
	RenderH         int
	Stop            context.CancelFunc

	// showFX plays the model's effects over the frame. They live in the
	// events its entity blueprint references and hang off markers on its
	// bones, so they are resolved the first time the button is pressed and
	// simulated from then on, which is what keeps the canvas repainting
	// while the toggle is on.
	ShowFX      bool
	ShowFXAxes  bool
	ShowGround  bool
	GroundImage *image.RGBA
	FxEffects   []FxOverlayEffect
	FxAxesClick widget.Clickable
	FxTextures  map[string]*image.RGBA
	// FX checklist selection is reset with the clip, then defaults to the
	// events on that clip's timelines.
	FxSelectionAnim   string
	FxManualOn        map[int]bool
	FxManualOff       map[int]bool
	FxOverrides       map[int]FxAttributeOverride
	FxAttrClicks      []widget.Clickable
	FxAttrIndex       int
	FxAttrClick       widget.Clickable
	FxAttrOpen        bool
	FxAttrDragging    bool
	FxAttrDragX       float32
	FxAttrDragY       float32
	FxAttrWindowX     int
	FxAttrWindowY     int
	FxAttrPosX        widget.Editor
	FxAttrPosY        widget.Editor
	FxAttrPosZ        widget.Editor
	FxAttrRotX        widget.Editor
	FxAttrRotY        widget.Editor
	FxAttrRotZ        widget.Editor
	FxAttrSetPos      widget.Clickable
	FxAttrSetAngle    widget.Clickable
	FxAttrResetPos    widget.Clickable
	FxAttrResetRot    widget.Clickable
	FxAttrError       string
	FxAttrSetDistance widget.Clickable
	FxAttrDir         widget.Clickable
	FxAttrMarker      widget.Clickable
	FxAttrClose       widget.Clickable
	FxAttrTitle       widget.Clickable
	FxAttrDistance    widget.Editor
	FxAttrDragTag     event.Tag
	FxAttrDragOrigin  image.Point
	// fxScale uniformly adjusts model-local FX geometry while leaving marker
	// origins fixed. One is the authored model-space size; the viewport exposes
	// the factor to inspect effects whose sizes need manual calibration.
	FxScale float64
	// FxRotX, FxRotY and FxRotZ turn every effect about its own marker, in
	// degrees, around the marker's own axes rather than the model's: X/Y/Z are
	// the three columns of the marker's frame, so a beam turns with the bone
	// it hangs off instead of with the world it happens to sit in. The marker
	// itself does not move — only what is measured out from it. X lifts and
	// drops an effect, Y swings it side to side, Z spins it around the
	// direction it already faces. They are a dial, not a feature, and exist
	// because a marker's stored forward axis is not always the way the effect
	// actually points — which column of the marker's matrix is "forwards" is a
	// question the file answers in a way that is quicker to measure against the
	// picture than to read out of a thousand lines of it. Zero leaves every
	// effect as authored.
	FxRotX, FxRotY, FxRotZ float64
	// fxSound is the model's declared audio: what plays when the clip on screen
	// reaches the moments its timeline names. It is built from the same .whe the
	// effects are read out of, and is nil for a model that declares no sounds.
	FxSound *fx.SoundPlayer
	// fxSoundOn is what the sound button shows: whether the sounds play. It is
	// separate from the player's own flag so that switching them off and on
	// again does not decode anything a second time.
	FxSoundOn bool
	// fxOverlay is the software-rendered particle image and fxScene is its
	// Vulkan rendering input.
	FxOverlay *image.RGBA
	FxScene   *FxGpuScene
	FxCh      chan FxLoadResult
	// fxSoundCh carries the sound half of the same work. It is separate from
	// fxCh because sounds are wanted from the first frame on screen while the
	// effects wait for the FX button, and a worker that had to wait for both
	// would leave the audio waiting on a toggle nobody pressed.
	FxSoundCh chan FxLoadResult
	FxErr     string
	FxLast    time.Time
	// fxAnim is the clip the effects were last stepped for. A model with
	// timelines only plays the events its own clip fires, so a change of clip
	// is what tells the simulations to start again.
	FxAnim string
	// fxClipPrev is where the playhead was on the previous frame, in seconds
	// into the clip, and fxClipPrevAnim the clip it belonged to. The effects
	// are stepped by the difference between the two rather than by wall clock
	// time, which is what makes an event start when its clip reaches the
	// moment the timeline names instead of when the clip began.
	FxClipPrev     float64
	FxClipPrevAnim string
	// fxTimelines are the motion timelines the worker's parse produced. They are
	// kept on the Viewer rather than only inside the sound player because the
	// scrub bar reads them too: they are the only place that says which frames
	// of a clip something fires on.
	FxTimelines []whe.FxTimeline
	// scrubRect is where the frame strip was drawn last frame, so a press inside
	// it winds the playhead instead of starting a camera drag. It is empty when
	// there is nothing to draw.
	ScrubRect image.Rectangle

	// markerIndex is the marker the markers list has picked, or -1 for none.
	// The reader uses it to find one attachment point on a model that
	// carries a dozen, which is the question a model file answers worst.
	MarkerIndex int
	// boneIndex is the bone the bones list has picked, or -1 for none. The
	// overlay draws it in the pick colour and the panel shows its
	// parameters; it is separate from markerIndex because a bone and a
	// marker are picked out of different lists.
	BoneIndex int

	// boneKeyIndex and boneKeyMode describe the bone keyframe the user is
	// currently editing, or -1/-1 when nothing is picked out of the bone's
	// keyframes. boneKeyIndex counts position keys first, then rotation keys,
	// so the same index space is shared by PosKey and RotKey.
	BoneKeyIndex int
	BoneKeyMode  int

	// modelFile is the tree path of the .whm being shown, which is where its
	// own textures are looked for first: a model and the maps it uses sit
	// together far more often than not.
	ModelFile string

	Tag      event.Tag
	Dragging bool
	Panning  bool
	// seeking says a press landed on the frame strip rather than on the model,
	// so the drag that follows is a scrub and not a camera turn.
	Seeking bool
	// keyDrag says the press that started the seek landed on the picked bone's
	// keymark, so the drag that follows carries that key along the strip
	// instead of scrubbing the playhead. keyDragMoved says one of the drag's
	// steps actually put the key somewhere new — the step that makes the edit
	// worth marking dirty and reporting when the drag ends.
	KeyDrag      bool
	KeyDragMoved bool
	LastX        float32
	LastY        float32
	LastFrame    time.Time
	Redraw       bool
}

type WhmAnimation = whm.Animation

func (m *WhmModel) BoneIndexByName(name string) int {
	return m.Model.BoneIndexByName(name)
}

func (m *WhmModel) ModelCenter() (fauxgl.Vector, float64) {
	if m == nil {
		return engine.ModelCenter(nil)
	}
	return engine.ModelCenter(&m.Model)
}

type WhmModel struct {
	whm.Model
}

type WhmPose = whm.Pose

type ArchiveOption struct {
	Source *modtree.FileSource
	Label  string
}

type BoneKeyRef struct {
	Mode  int // 0 position, 1 rotation
	Frame int
}

type BoneTrackSnapshot struct {
	Anim      *WhmAnimation
	BoneIndex int
	PosKey    []WhmKey3
	RotKey    []WhmKey4
}

type ClipHistory = cliphist.ClipHistory

type EvtPicker struct {
	Open   bool
	Target EvtPickTarget
	Ed     widget.Editor
	Filter string
	All    []string
	Hits   []string
	Clicks []widget.Clickable
	List   layout.List

	UseTyped widget.Clickable
	Browse   widget.Clickable
	Cancel   widget.Clickable
}

func (fp *FilePicker) Allows(name string) bool {
	if len(fp.Cfg.Exts) == 0 {
		return true
	}
	ext := PickExt(name)
	for _, e := range fp.Cfg.Exts {
		if ext == e {
			return true
		}
	}
	return false
}

func (fp *FilePicker) SetText(s string) {
	fp.Ed.SetText(s)
	fp.Last = s
}

type FilePicker struct {
	Cfg    PickConfig
	Ed     widget.Editor
	Status string
	Dir    string // folder being browsed; "" while the start list is shown
	Last   string // the field's text as the dialog last saw it
	Filter string
	All    []PickRow
	Rows   []PickRow
	Clicks []widget.Clickable
	List   layout.List
	Ok     widget.Clickable
	Cancel widget.Clickable
}

func (b *FreezeBook) Record(path string) *FreezeRecord {
	if b == nil {
		return nil
	}
	for i := range b.Frozen {
		if strings.EqualFold(b.Frozen[i].Path, path) {
			return &b.Frozen[i]
		}
	}
	return nil
}

func (b *FreezeBook) IsFrozen(path string) bool { return b.Record(path) != nil }

func (b *FreezeBook) FrozenPaths() []string {
	if b == nil {
		return nil
	}
	out := make([]string, 0, len(b.Frozen))
	for _, r := range b.Frozen {
		out = append(out, r.Path)
	}
	return out
}

type FreezeBook struct {
	Version int            `json:"version"`
	Mod     string         `json:"mod"`
	Frozen  []FreezeRecord `json:"frozen"`
}

type FxAttributeOverride struct {
	Origin           fauxgl.Vector
	HasOrigin        bool
	Rotation         [3]float64
	ShowDirection    bool
	ShowMarker       bool
	ScaleDistance    float64
	HasScaleDistance bool
}

type FxGpuScene = fxscene.Scene

type FxLoadResult struct {
	Effects  []FxOverlayEffect
	Textures map[string]*image.RGBA
	// sounds is every sample the worker decoded, keyed by its virtual path.
	// Decoding a .fda takes long enough that doing it on the frame that first
	// plays one would drop the sound that asked for it.
	//
	// It is a cache rather than a map because the fills that decode a sound's
	// other variants run at the same time as each other and the caller reads
	// this as soon as the worker is done. SoundSet is how a caller gets at it.
	Sounds *fx.SoundCache
	// soundBundles is the event name to the files that may serve it: a sound
	// path in a .whe names a folder of variants, and which of them plays is
	// the game's choice at the moment it plays.
	SoundBundles map[string]fx.SoundBundle
	// timelines are the motion timelines of the .whe, which say when each of
	// those events fires in the clip. They came out of the same parse, so they
	// are carried rather than read again.
	Timelines []whe.FxTimeline
	Err       error
}

func (e *FxOverlayEffect) FxFireTime(anim string) float64 {
	if e.FireTimes != nil {
		if at, ok := e.FireTimes[strings.ToLower(anim)]; ok {
			return at
		}
	}
	return -1
}

func (e *FxOverlayEffect) FxTotalScale() float64 {
	s := e.Scale
	if s <= 0 {
		s = 1
	}
	if e.EventScale > 0 {
		s *= e.EventScale
	}
	return s
}

func (e *FxOverlayEffect) FxPlaysFor(anim string) bool {
	if !e.Timed {
		return true
	}
	if anim == "" {
		return false
	}
	for _, a := range e.Animations {
		if strings.EqualFold(a, anim) {
			return true
		}
	}
	return false
}

func (e *FxOverlayEffect) FxScaleOrOne() float64 {
	return e.FxTotalScale()
}

func (e *FxOverlayEffect) FxScaleRibbon(rb fx.BeamRibbon, preservesMarkerSpan bool) fx.BeamRibbon {
	scale := e.FxScaleOrOne()
	if preservesMarkerSpan {
		delta := rb.From.Sub(e.Origin).MulScalar(scale - 1)
		rb.From = rb.From.Add(delta)
		rb.To = rb.To.Add(delta)
	} else {
		rb.From = e.Origin.Add(rb.From.Sub(e.Origin).MulScalar(scale))
		rb.To = e.Origin.Add(rb.To.Sub(e.Origin).MulScalar(scale))
	}
	rb.Width *= scale
	rb.Arc = rb.Arc.MulScalar(scale)
	rb.Noise *= scale
	return e.FxTurnRibbon(rb)
}

func (e *FxOverlayEffect) FxSpansMarkers() bool {
	return e.Spans && e.HasOther
}

func (e *FxOverlayEffect) FxTurnVector(v fauxgl.Vector) fauxgl.Vector {
	return FxRotated(v, e.TurnAngles[0], e.TurnAngles[1], e.TurnAngles[2], e.TurnAxes)
}

func (e *FxOverlayEffect) FxMarkerRibbon(rb fx.BeamRibbon) fx.BeamRibbon {
	// The FX's Offset is local to the attachment, while the beam's span is
	// already expressed by its world-space marker endpoints. Transform the
	// local start offset, then translate both endpoints by the same amount.
	offset := rb.From.Sub(e.Origin)
	worldOffset := e.FxMarkerVector(offset)
	rb.From = e.Origin.Add(worldOffset)
	rb.To = rb.To.Add(worldOffset)
	rb.Arc = e.FxMarkerVector(rb.Arc)
	if e.AlignToOther && e.HasOther && (e.Sim == nil || !e.Sim.NoRotate) {
		direction := e.Far.Sub(e.Origin).Normalize()
		if direction.Length() > 1e-6 {
			rb.To = rb.From.Add(direction.MulScalar(rb.To.Sub(rb.From).Length()))
			forward := e.Frame[2].Normalize()
			rb.Arc = FxRotateVectorBetween(rb.Arc, forward, direction)
		}
	}
	return rb
}

func (e *FxOverlayEffect) FxTurnRibbon(rb fx.BeamRibbon) fx.BeamRibbon {
	if e.TurnAngles == [3]float64{} {
		return rb
	}
	centre := rb.From.Add(rb.To).MulScalar(0.5)
	halfSpan := rb.To.Sub(rb.From).MulScalar(0.5)
	centre = e.Origin.Add(e.FxTurnVector(centre.Sub(e.Origin)))
	if !rb.Billboard {
		halfSpan = e.FxTurnVector(halfSpan)
		if rb.Fixed {
			rb.Across = e.FxTurnVector(rb.Across)
		}
	}
	rb.From = centre.Sub(halfSpan)
	rb.To = centre.Add(halfSpan)
	rb.Arc = e.FxTurnVector(rb.Arc)
	return rb
}

func (e *FxOverlayEffect) FxTurnSprite(sp fx.Sprite) fx.Sprite {
	local := fauxgl.Vector{X: sp.Pos[0], Y: sp.Pos[1], Z: sp.Pos[2]}
	world := e.FxTurnVector(e.FxMarkerVector(local))
	sp.Pos = [3]float64{world.X, world.Y, world.Z}
	if sp.Horizontal {
		normal := fauxgl.Vector{X: sp.PlaneNormal[0], Y: sp.PlaneNormal[1], Z: sp.PlaneNormal[2]}
		if normal.Length() < 1e-6 {
			normal = fauxgl.Vector{Y: 1}
		}
		normal = e.FxTurnVector(e.FxMarkerVector(normal)).Normalize()
		sp.PlaneNormal = [3]float64{normal.X, normal.Y, normal.Z}
	}
	return sp
}

func (e *FxOverlayEffect) FxMarkerVector(v fauxgl.Vector) fauxgl.Vector {
	if e.Sim != nil && e.Sim.NoRotate {
		// No_Rotate drops the marker's rotation, not the coordinate swap:
		// the simulation keeps every vector in the file's own Y-up frame,
		// and this renderer is Z up, so the file axes are still carried
		// over the way FxFallbackFrame carries them.
		return FxFallbackFrame[0].MulScalar(v.X).
			Add(FxFallbackFrame[1].MulScalar(v.Y)).
			Add(FxFallbackFrame[2].MulScalar(v.Z))
	}
	if e.Frame == [3]fauxgl.Vector{} {
		return v
	}
	x, y, z := e.Frame[0], e.Frame[1], e.Frame[2]
	if x.Length() < 1e-6 || y.Length() < 1e-6 || z.Length() < 1e-6 {
		return v
	}
	return x.MulScalar(v.X).Add(y.MulScalar(v.Y)).Add(z.MulScalar(v.Z))
}

func (e *FxOverlayEffect) FxFarPoint() fauxgl.Vector {
	if e.HasOther {
		return e.Far
	}
	axis := e.Axis
	if n := axis.Length(); n > 1e-6 {
		axis = axis.Normalize()
	} else {
		// No marker to take a direction from, so nothing says which way the
		// beam runs. Up is the only direction the files themselves keep
		// meaning: these are ground strikes written to fall out of the sky,
		// and the renderer is Z up, so a bolt of twelve units leaves the
		// monolith standing rather than lying across it. The old fallback was
		// the model's own X, which sent awakening_lighting_ground's twelve
		// unit LIGHTNING_BEAM out sideways from the middle of the building at
		// ground height.
		axis = FxAxisZ
	}
	return e.Origin.Add(axis.MulScalar(FxBeamFreeLength))
}

func (e *FxOverlayEffect) FxRingFrame() (axX, axY, axZ fauxgl.Vector) {
	if e.Sim != nil && e.Sim.NoRotate {
		return FxFallbackFrame[0], FxFallbackFrame[1], FxFallbackFrame[2]
	}
	axX, axY, axZ = e.Frame[0], e.Frame[1], e.Frame[2]
	if axX.Length() < 1e-6 || axY.Length() < 1e-6 || axZ.Length() < 1e-6 {
		return FxFallbackFrame[0], FxFallbackFrame[1], FxFallbackFrame[2]
	}
	return axX, axY, axZ
}

type FxOverlayEffect struct {
	Sim   *fx.FxSim
	Label string
	// FxName is the effect file this came from, as the event named it. It
	// is kept for the viewport's readout: an event name on its own does not
	// say which of the game's several thousand effect files was played.
	FxName string
	// Marker is the index of the model's marker the effect plays at, or
	// -1 for one with no marker to play at. Origin is where that falls
	// under the pose on screen, and is refreshed every frame because a
	// marker on a moving bone moves with it.
	Marker int
	Origin fauxgl.Vector
	// Ground marks an effect whose event, effect path or attachment point
	// explicitly identifies it as a ground/dust effect. Its marker still
	// supplies the horizontal position, while its height is anchored to the
	// model's lowest bound so animated or orphaned effects do not float.
	Ground bool
	// OtherMarker is the far end of a beam, -1 when the effect named no
	// second marker. Far is that marker under the pose on screen, refreshed
	// for the same reason Origin is. Spans reports whether the beam should
	// take its Length from the gap between the two rather than in model
	// units.
	//
	// HasOther is what says the far end is real. The index alone cannot:
	// zero is a valid marker, so an effect built without one set would
	// quietly span to the model's first marker.
	HasOther    bool
	OtherMarker int
	Far         fauxgl.Vector
	Spans       bool
	// Axis is where the marker's own frame points, for a beam whose event
	// named no second marker. A beam with a partner spans the two; a beam
	// without one runs this far along its Length curve. Right and Up are the
	// marker's other two axes, which is the frame a ring's Offset is written
	// in and the plane a ring with Billboard off keeps.
	Axis  fauxgl.Vector
	Right fauxgl.Vector
	Up    fauxgl.Vector
	// AlignToOther comes from fx_transform = self_to_other. The local FX
	// forward axis (renderer-local +Z) then aims from the event marker to
	// its paired marker instead of following an arbitrary marker-frame axis.
	AlignToOther bool
	// Frame is the FX-local X/Y/Z frame in model space. By default that is
	// the marker frame; an FX root with No_Rotate keeps its authored frame
	// instead of inheriting the marker's orientation.
	Frame [3]fauxgl.Vector
	// TurnAxes and TurnAngles describe the correction rotation in the marker's
	// animated frame. Vectors are rotated about Origin, which remains fixed.
	TurnAxes        [3]fauxgl.Vector
	TurnAngles      [3]float64
	ManualOrigin    fauxgl.Vector
	HasManualOrigin bool
	ShowDirection   bool
	ShowMarker      bool
	Direction       fauxgl.Vector
	ScaleDistance   float64

	// Scale uniformly adjusts model-local FX positions and dimensions for the
	// preview. Zero and one both mean "as authored".
	Scale float64
	// EventScale is the fx_scale the event itself carried, applied uniformly
	// on top of Scale. Zero means the event named none.
	EventScale float64
	// Animations are the clips that fire this effect. With Timed set, an
	// effect in no clip at all plays on none of them, since something else
	// triggers it in the game.
	Animations []string
	Timed      bool
	// FireTimes is Animations again, but with the moment each clip fires this
	// event: the key is a clip name in lower case and the value the timeline's
	// own time. A clip in Animations with no entry here fires on entering it,
	// which is what a timeline written -1 means.
	//
	// It is kept beside Animations rather than inside it so that a model with
	// no timelines at all still builds: those effects have no time to honour
	// and play from the start, which is what an empty set has always meant.
	FireTimes map[string]float64
}

type GraphLinkBatch struct {
	Links []GraphLink
	Done  bool
}

type IconCacheEntry struct {
	Img   image.Image
	Tried bool
}

type ImageCacheEntry struct {
	Data     image.Image
	Mipmaps  []image.Image
	Format   string
	Bytes    int
	CachedAt uint64
}

type ImageDecodeResult struct {
	Data    image.Image
	Mipmaps []image.Image
	Format  string
	Bytes   int
	Err     error
}

type LocaleEditRequest struct {
	Id     string
	Action LocaleRowAction
}

type ModCatalogRefreshResult struct {
	Path     string
	BaseTree *modtree.ModTree
	Snapshot string
	NewTree  *modtree.ModTree
	Err      error
}

type ModGraph = modgraph.ModGraph

type ModelFrame struct {
	Image  *image.RGBA
	Serial uint64
}

type ModelMaterialDiagnostic struct {
	ResolvedPath string
	Format       string
	Status       string
}

type ModelMaterialResult struct {
	Materials   map[string]render.WhmMaterial
	Rsh         map[string][]byte
	Aliases     map[string]string
	Diagnostics map[string]ModelMaterialDiagnostic
	Missing     int
	// more says another result follows on the same channel. A map scene
	// delivers its terrain diffuse first and its entities second, so the
	// channel stays set until a result arrives without one following it.
	More bool
	// model replaces the Viewer's model outright: how a map scene grows
	// from the terrain-only mesh it opens with into the full scene with
	// the placed objects. A .whm Tab never sends one.
	Model *WhmModel
	// entityMeshes maps the scene's uniquely named entity meshes back to the
	// stable scenario IDs used by the map Tab's in-memory selection.
	EntityMeshes     map[string]int
	AnimatedEntities []sgb.AnimatedEntity
}

func (k NewObjKind) NewObjNoun() string {
	if k == NewObjBuilding {
		return "building"
	}
	return "unit"
}

func (k NewObjKind) NewObjPath(race, name string) string {
	if k == NewObjBuilding {
		return StructureTemplateName{Race: race, Name: name}.StructureTemplateVirtualPath()
	}
	return UnitTemplateName{Race: race, Name: name}.UnitTemplateVirtualPath()
}

type NewObjKind int

type PanelScrollState struct {
	Tag           event.Tag
	ScrollTag     event.Tag
	Offset        int
	ContentHeight int
	Viewport      int
	HasBar        bool
	HoverBar      bool
	Dragging      bool
	StartY        float32

	StartOffset int
}

type PanelSplitterState struct {
	Tag           event.Tag
	Hover         bool
	Dragging      bool
	LayoutOrigin  int
	StartPointerX float32

	StartLeft  int
	StartRight int
}

type RgdEditorClickState struct {
	Editor *widget.Editor
	Click  widget.Clickable
}

type RgdVisible struct {
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

type SgaRow struct {
	Path  string // the entry's path inside the archive, e.g. "Data\art\..."
	IsDir bool
	Size  int64
}

type SgbObjectMaterialRequest struct {
	Blueprint string
	ModelPath string
	Model     *whm.Model
	EntityID  int
}

type SgbObjectMaterialsResult struct {
	Request     SgbObjectMaterialRequest
	Materials   map[string]render.WhmMaterial
	Rsh         map[string][]byte
	Diagnostics map[string]ModelMaterialDiagnostic
	Missing     int
}

type SgbObjectModelResult struct {
	Blueprint string
	ModelPath string
	Model     *whm.Model
}

type SgbPlacementRequest struct {
	Blueprint string
	Point     fauxgl.Vector
	Owner     int
}

type UnitCatalog = unitdef.UnitCatalog

type UnitDetails = unitdef.UnitDetails

func (p *UnitEvtPlayer) StopVoices() {
	for _, v := range p.Voices {
		v.Stop()
	}
	p.Voices = nil
}

func (p *UnitEvtPlayer) Expire() {
	now := time.Now()
	kept := p.Voices[:0]
	for _, v := range p.Voices {
		if now.Before(v.EndsAt) {
			kept = append(kept, v)
			continue
		}
		v.Stop()
	}
	p.Voices = kept
}

func (p *UnitEvtPlayer) StopAll() {
	p.StopVoices()
	if p.Audition != nil {
		p.Audition.Stop()
		p.Audition = nil
	}
	p.Playing = false
	p.Paused = false
	p.Dragging = false
	p.Pos = 0
	p.Next = 0
	p.Order = nil
}

func (p *UnitEvtPlayer) Seek(frac float64) {
	if !p.Playing {
		return
	}
	p.Pos = frac * p.Duration
	p.StopVoices()
	for p.Next > 0 && p.Order[p.Next-1].At > p.Pos {
		p.Next--
	}
	for p.Next < len(p.Order) && p.Order[p.Next].At <= p.Pos {
		p.Next++
	}
}

type UnitEvtPlayer struct {
	PlayEv widget.Clickable // play the selected event's sound
	TlPlay widget.Clickable // play / pause / resume the selected timeline
	TlStop widget.Clickable // stop the timeline playback
	Scrub  struct{}         // pointer tag of the scrub strip

	// audition is the single event's sound while it sounds.
	Audition *fx.SoundVoice

	// Timeline playback State.
	Playing  bool
	Paused   bool
	Dragging bool
	// forTimeline and forDoc pin the playback to the selection it was started
	// from: picking another timeline or another .whe under a playing one
	// stops it rather than leaving sounds firing for a list not on screen.
	ForTimeline string
	ForDoc      *whe.Document
	Pos         float64 // playhead, seconds into the timeline
	Duration    float64 // moment of the last timed event
	Last        time.Time
	Order       []UnitEvtFire
	Next        int
	Voices      []*fx.SoundVoice
	// cache keys decoded samples by the file picked to play them, so a
	// footstep fired at five moments is decoded once.
	Cache map[string]*fx.SoundSample
}

type UnitImpRef struct {
	Anim  int
	Frame int
}

type UnitModelResult struct {
	Unit        string
	Path        string
	Model       *WhmModel
	GroundImage image.Image
	Work        time.Duration // reading, parsing and the ground image
	Err         error
}

type UnitNameBatch struct {
	Names map[string]string
	Done  bool
}

type UnitSquadBatch struct {
	// byUnit maps a lower cased unit path to the squads naming it. The unit
	// path is the key rather than the reference written in the squad, because
	// the dialog already knows a unit by its path and resolving the reference
	// back would be work done twice.
	ByUnit map[string][]UnitSquadRef
	Done   bool
}

type UnitSquadRef struct {
	// path is the squad's own .rgd in the tree.
	Path string
	// name is its file name without extension, which is how the game names it.
	Name string
	// race is the folder it sits under, e.g. "necrons".
	Race string
	// unitMin and unitMax are squad_loadout_ext's unit_min and unit_max: how
	// many of this unit the squad is built from. They are kept as the strings
	// the file holds rather than as numbers, because the column shows them
	// verbatim and a .rgd can carry a float where an int is expected.
	UnitMin string
	UnitMax string
	// birthResource is the squad's birth_resource table, the cost of raising
	// the squad as a whole. It is a separate cost from the unit's own
	// cost_ext, and it is the one a player pays.
	BirthResource map[string]string
	// path fields the unit the squad is made of, for the row's own reference.
	UnitRef string
}

type EvtPickTarget int

type FreezeRecord struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Race string `json:"race"`
	// FrozenAt is when, in the project's own terms: the record is read by a
	// person trying to work out what happened to their mod.
	FrozenAt string `json:"frozenAt"`
	// Files are the .rgd files that were moved out, in the order they were
	// moved: the ebps one first, then the sbps one.
	Files []FreezeFile `json:"files"`
	// Mentions are the references that were cleared, so they can be put back.
	Mentions []FreezeMention `json:"mentions"`
}

const FxBeamFreeLength = 1.0

var FxFallbackFrame = [3]fauxgl.Vector{
	{X: 1}, // the file's X, across
	{Z: 1}, // the file's Y, up
	{Y: 1}, // the file's Z, forward
}

type GraphLink = modgraph.GraphLink

type LocaleRowAction int

type PickConfig struct {
	Title, Subtitle, Hint string
	Exts                  []string // lower case with the dot; a bare name gets the first
	// modTree makes the picker use the mod file tree: its list is the tree's
	// files, folders are not browsed on disk, and only tree paths are accepted.
	// Sounds, FX and the timeline import ask for it; Open Module does not.
	ModTree bool
	Roots   func() []string
	Start   string
	// onPick receives the path that was chosen. It returns whether the dialog
	// may close, and a status line for the dialog when it may not.
	OnPick func(path string) (bool, string)
}

type PickRow struct {
	Path   string
	Label  string
	Folder string
	IsDir  bool
}

type StructureTemplateName = unitdef.StructureTemplateName

type UnitEvtFire struct {
	At   float64
	Name string
	Path string
}

type UnitTemplateName = unitdef.UnitTemplateName

// The four folders of the classic editor's tree, in the order the editor
// listed them: the clip names first, then the .whe's own motion (timeline)
// and action records, and finally the action hierarchy built from
// action-to-subaction links.
const (
	ClassicOEFolderAnims = iota
	ClassicOEFolderMotions
	ClassicOEFolderActions
	ClassicOEFolderTree
	ClassicOEFolderCount
)

// What the new-object dialog is being opened to create.
const (
	NewObjUnit NewObjKind = iota
	NewObjBuilding
)

// Fallback axes for effects without a marker frame. Marked effects use the
// three transformed columns of their own marker matrix instead.
var (
	FxAxisX = fauxgl.Vector{X: 1}
	FxAxisY = fauxgl.Vector{Y: 1}
	FxAxisZ = fauxgl.Vector{Z: 1}
)

type WhmKey3 = whm.Key3

type WhmKey4 = whm.Key4

// FreezeFile is one file moved out of the mod.
type FreezeFile struct {
	// From is the virtual path the file had in the mod.
	From string `json:"from"`
	// To is its name inside the project's Units folder.
	To string `json:"to"`
}

// FreezeMention is one reference to the frozen entity that was cleared.
type FreezeMention struct {
	// File is the virtual path of the file that held the reference.
	File string `json:"file"`
	// Field is the dotted path of the field, which is how it is found again.
	Field string `json:"field"`
	// Slot and Role are kept so the record reads as something a person can
	// check rather than as a list of offsets.
	Slot  string `json:"slot"`
	Role  string `json:"role"`
	Value string `json:"value"`
}

// pickExt is the lower case extension of a file name or path, dot included,
// or "" when the last part has none. A backslash or slash after the dot means
// the dot belongs to a folder.
func PickExt(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 || strings.ContainsAny(name[i:], `\/`) {
		return ""
	}
	return strings.ToLower(name[i:])
}

// fxRotated turns v by X, then Y, then Z around the supplied fixed axes in
// marker space. Keeping each axis fixed makes the fields independent. Angles
// are radians; the pivot is supplied by the caller as a vector relative to the
// marker origin.
func FxRotated(v fauxgl.Vector, rx, ry, rz float64, axes [3]fauxgl.Vector) fauxgl.Vector {
	if rx != 0 {
		v = FxRotateAbout(v, axes[0], rx)
	}
	if ry != 0 {
		v = FxRotateAbout(v, axes[1], ry)
	}
	if rz != 0 {
		v = FxRotateAbout(v, axes[2], rz)
	}
	return v
}

// fxRotateAbout turns v through angle radians around axis, by Rodrigues'
// formula: the part of v along the axis is fixed, the part across it sweeps in
// the circle the cross product names. It is the rotation matrix written out
// without building one, which matters here because it runs for every axis of
// every effect on every frame.
func FxRotateAbout(v, axis fauxgl.Vector, angle float64) fauxgl.Vector {
	if angle == 0 {
		return v
	}
	a := axis.Normalize()
	if a == (fauxgl.Vector{}) {
		// No frame to turn about: leaving the vector alone beats turning it
		// about an arbitrary axis and calling the result a heading.
		return v
	}
	c, s := math.Cos(angle), math.Sin(angle)
	return v.MulScalar(c).
		Add(a.Cross(v).MulScalar(s)).
		Add(a.MulScalar(a.Dot(v) * (1 - c)))
}

func FxRotateVectorBetween(v, from, to fauxgl.Vector) fauxgl.Vector {
	from, to = from.Normalize(), to.Normalize()
	if from.Length() < 1e-6 || to.Length() < 1e-6 {
		return v
	}
	cross := from.Cross(to)
	sin := cross.Length()
	cos := engine.ClampFloat64(from.Dot(to), -1, 1)
	if sin < 1e-6 {
		if cos > 0 {
			return v
		}
		axis := FxFallbackFrame[0].Sub(from.MulScalar(FxFallbackFrame[0].Dot(from))).Normalize()
		if axis.Length() < 1e-6 {
			return v
		}
		return axis.MulScalar(2 * axis.Dot(v)).Sub(v)
	}
	axis := cross.DivScalar(sin)
	return v.MulScalar(cos).
		Add(axis.Cross(v).MulScalar(sin)).
		Add(axis.MulScalar(axis.Dot(v) * (1 - cos)))
}
