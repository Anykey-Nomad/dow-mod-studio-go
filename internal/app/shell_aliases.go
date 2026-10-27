package app

import "dow-mod-studio/internal/app/shell"

// The shell lives in internal/app/shell. These aliases keep every call
// site in app — and in the tests — reading exactly as it did before the move.

var rgb = shell.Rgb

var rgba = shell.Rgba

var setColorTheme = shell.SetColorTheme

var applyMaterialTheme = shell.ApplyMaterialTheme

type menuDef = shell.MenuDef

var menus = shell.Menus

// The original app package ran this at load time; Init keeps the same body,
// so app keeps the same init hook.
func init() { shell.Init() }

type uiState = shell.UiState

type rgdEditorClickState = shell.RgdEditorClickState

type fileTab = shell.FileTab

type rgdVisible = shell.RgdVisible

var newPointerTag = shell.NewPointerTag

var newUI = shell.NewUI

var EnsureTabClicks = shell.EnsureTabClicks

var FindTabIndex = shell.FindTabIndex

var ActiveTab = shell.ActiveTab

var IsModuleActive = shell.IsModuleActive

var FdaActive = shell.FdaActive

var RgdActive = shell.RgdActive

var ReleaseTabResources = shell.ReleaseTabResources

var CloseTabAt = shell.CloseTabAt

var CloseActiveFileTab = shell.CloseActiveFileTab

var CloseAllFileTabs = shell.CloseAllFileTabs

var ClearRgd = shell.ClearRgd

var ClearFda = shell.ClearFda

var CloseActiveViewer = shell.CloseActiveViewer

var findFirstModuleArg = shell.FindFirstModuleArg

var SyncWindowTitle = shell.SyncWindowTitle

var TryAutoLoad = shell.TryAutoLoad

var LoadModule = shell.LoadModule

var hLine = shell.HLine

var fillRect = shell.FillRect

var rrect = shell.Rrect

var LayoutWaveform = shell.LayoutWaveform

var centeredPanelLabel = shell.CenteredPanelLabel

var centeredPanelWidget = shell.CenteredPanelWidget

var IconButton = shell.IconButton

var MiniButton = shell.MiniButton

var MiniButtonDisabled = shell.MiniButtonDisabled

var SmallButton = shell.SmallButton

var SmallButtonDisabled = shell.SmallButtonDisabled

var PrimaryButton = shell.PrimaryButton

var ToggleButton = shell.ToggleButton

var sectionHeader = shell.SectionHeader

var LayoutStatusBar = shell.LayoutStatusBar

var SaveEligible = shell.SaveEligible

var HandleSgbSave = shell.HandleSgbSave

var ExportEligible = shell.ExportEligible

var HandleExportActiveTab = shell.HandleExportActiveTab

var TableHead = shell.TableHead

var vLine = shell.VLine

var TableRow2 = shell.TableRow2

var TableRow2WithTooltip = shell.TableRow2WithTooltip

const tabBarHeightDp = shell.TabBarHeightDp

const menuBarHeightDp = shell.MenuBarHeightDp

const tabContextWDP = shell.TabContextWDP

const tabContextRowDp = shell.TabContextRowDp

const tabContextPadDp = shell.TabContextPadDp

const tabContextInsetP = shell.TabContextInsetP

const tabWheelStepPx = shell.TabWheelStepPx

var tabBarTop = shell.TabBarTop

var LayoutTabBar = shell.LayoutTabBar

var RegisterTabBarPointer = shell.RegisterTabBarPointer

var LayoutTabRow = shell.LayoutTabRow

var tabLabelFor = shell.TabLabelFor

var TabMaxScroll = shell.TabMaxScroll

var ClampTabScroll = shell.ClampTabScroll

var TabAt = shell.TabAt

var Tab = shell.Tab

var OpenTabContextMenu = shell.OpenTabContextMenu

var CloseTabContextMenu = shell.CloseTabContextMenu

var tabContextItems = shell.TabContextItems

var tabContextMenuOrigin = shell.TabContextMenuOrigin

var LayoutTabContextMenu = shell.LayoutTabContextMenu

const wheelNotchDelta = shell.WheelNotchDelta

var GetVisibleNodes = shell.GetVisibleNodes

var EnsureRowHover = shell.EnsureRowHover

var EnsureRowClicks = shell.EnsureRowClicks

var isEnterKey = shell.IsEnterKey

var treeNavKeyNames = shell.TreeNavKeyNames

var treeActionKeyNames = shell.TreeActionKeyNames

var OpenFileContextMenu = shell.OpenFileContextMenu

var CloseFileContextMenu = shell.CloseFileContextMenu

var DeleteSelectedTreeFile = shell.DeleteSelectedTreeFile

var OpenSettingsDialog = shell.OpenSettingsDialog

var FindRgdReferenceFile = shell.FindRgdReferenceFile

var NavigateRgdTree = shell.NavigateRgdTree

var TreeRowAt = shell.TreeRowAt

var HandleTabBarInput = shell.HandleTabBarInput

var TabContextPoint = shell.TabContextPoint

var HandleTabContextMenuInput = shell.HandleTabContextMenuInput

var kindLabel = shell.KindLabel

var heldModifiers = shell.HeldModifiers

var MenuButton = shell.MenuButton

var recentModulesHeaderIndex = shell.RecentModulesHeaderIndex

var isEmptyRecycleBinMenuLabel = shell.IsEmptyRecycleBinMenuLabel

var EnsureMenuItemClicks = shell.EnsureMenuItemClicks

var MenuItem = shell.MenuItem

var LayoutSettingsDialog = shell.LayoutSettingsDialog

var LayoutAssocRows = shell.LayoutAssocRows

var LayoutClearRecycleDialog = shell.LayoutClearRecycleDialog

var LayoutFileDeleteDialog = shell.LayoutFileDeleteDialog

var DialogButton = shell.DialogButton

const searchFieldWidthDp = shell.SearchFieldWidthDp

const searchFieldHeightDp = shell.SearchFieldHeightDp

const searchPanelWidthDp = shell.SearchPanelWidthDp

const searchRowHeightDp = shell.SearchRowHeightDp

const searchPanelMarginDp = shell.SearchPanelMarginDp

const searchPanelTopDp = shell.SearchPanelTopDp

const searchResultLimit = shell.SearchResultLimit

const searchPanelMaxHeightDp = shell.SearchPanelMaxHeightDp

const searchPanelMinHeightDp = shell.SearchPanelMinHeightDp

var searchSourceColor = shell.SearchSourceColor

var searchFolderOf = shell.SearchFolderOf

var RunSearch = shell.RunSearch

var EnsureSearchClicks = shell.EnsureSearchClicks

var RevealInTree = shell.RevealInTree

var HandleSearchArrows = shell.HandleSearchArrows

var SearchPanelWanted = shell.SearchPanelWanted

var CloseSearch = shell.CloseSearch

var LayoutSearchSlot = shell.LayoutSearchSlot

var LayoutSearchField = shell.LayoutSearchField

var LayoutSearchPanel = shell.LayoutSearchPanel

var LayoutSearchFilters = shell.LayoutSearchFilters

var LayoutSearchStatus = shell.LayoutSearchStatus

var LayoutSearchResults = shell.LayoutSearchResults

var LayoutSearchRow = shell.LayoutSearchRow

var LayoutSearchName = shell.LayoutSearchName

var SearchNameLabel = shell.SearchNameLabel

var LayoutSearchPath = shell.LayoutSearchPath

var LayoutSearchOverlay = shell.LayoutSearchOverlay

var SearchResultPaths = shell.SearchResultPaths

var flexGap = shell.FlexGap

var rowGap = shell.RowGap

var panelFrame = shell.PanelFrame

var LayoutLeftPanel = shell.LayoutLeftPanel

var LayoutLeftPanelContent = shell.LayoutLeftPanelContent

var LayoutFileContextMenu = shell.LayoutFileContextMenu

var absorbDialogCardPress = shell.AbsorbDialogCardPress

type panelSplitterState = shell.PanelSplitterState

type panelScrollState = shell.PanelScrollState

var clampInt = shell.ClampInt

var EnsurePanelWidths = shell.EnsurePanelWidths

var ClampPanelWidths = shell.ClampPanelWidths

var PanelWidthDp = shell.PanelWidthDp

var PanelSplitterWidth = shell.PanelSplitterWidth

var panelSplitterLayoutOrigin = shell.PanelSplitterLayoutOrigin

var panelSplitterWidthFromDrag = shell.PanelSplitterWidthFromDrag

var LayoutPanelSplitter = shell.LayoutPanelSplitter

var FixedPanel = shell.FixedPanel

var PanelWithScroll = shell.PanelWithScroll

const panelScrollMeasureHeight = shell.PanelScrollMeasureHeight

const panelScrollWheelStepPx = shell.PanelScrollWheelStepPx

var PanelScrollView = shell.PanelScrollView

var panelScrollThumbSize = shell.PanelScrollThumbSize

var drawPanelScrollbar = shell.DrawPanelScrollbar

var LayoutListWithScrollbar = shell.LayoutListWithScrollbar

var LayoutTreeRow = shell.LayoutTreeRow

var RegisterTreeRowClick = shell.RegisterTreeRowClick

var TreeHasKeyboard = shell.TreeHasKeyboard

var hasSuffix = shell.HasSuffix

var MiddleTreeRow = shell.MiddleTreeRow

var LayoutRgdMidRow = shell.LayoutRgdMidRow

var MiddleHeader = shell.MiddleHeader

type persistedTab = shell.PersistedTab

type persistedState = shell.PersistedState

var stateFilePath = shell.StateFilePath

var loadPersistedState = shell.LoadPersistedState

var savePersistedState = shell.SavePersistedState

var MarkStateDirty = shell.MarkStateDirty

var MaybeSaveState = shell.MaybeSaveState

var ApplyPersistedSettings = shell.ApplyPersistedSettings

var RememberRecentModule = shell.RememberRecentModule

var RememberRecentFolder = shell.RememberRecentFolder

var samePath = shell.SamePath

type clipKind = shell.ClipKind

type clipEntry = shell.ClipEntry

type clipHistory = shell.ClipHistory

type boneKeyClip = shell.BoneKeyClip

type wheEventClip = shell.WheEventClip

type wheActionClip = shell.WheActionClip

const clipEvents = shell.ClipEvents

const clipBoneKeys = shell.ClipBoneKeys

const clipImportKeys = shell.ClipImportKeys

const clipActions = shell.ClipActions

const clipHistoryLimit = shell.ClipHistoryLimit
