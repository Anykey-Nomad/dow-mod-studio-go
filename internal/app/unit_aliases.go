package app

import "dow-mod-studio/internal/app/unit"

// The unit panels live in internal/app/unit. These aliases keep every call
// site in app — and in the tests — reading exactly as it did before the move.

const unitSectionClassicOE = unit.UnitSectionClassicOE

const classicOEPanelDp = unit.ClassicOEPanelDp

const classicOEKindHint = unit.ClassicOEKindHint

const classicOEKindAnims = unit.ClassicOEKindAnims

const classicOEKindEvents = unit.ClassicOEKindEvents

const classicOEKindConditions = unit.ClassicOEKindConditions

const classicOEKindClauses = unit.ClassicOEKindClauses

const classicOEKindModifiers = unit.ClassicOEKindModifiers

const classicOEKindMeshes = unit.ClassicOEKindMeshes

const classicOEKindSelectedUI = unit.ClassicOEKindSelectedUI

var classicOEHeaders = unit.ClassicOEHeaders

var classicOEAnimsHeader = unit.ClassicOEAnimsHeader

var EnsureClassicOEClicks = unit.EnsureClassicOEClicks

var LayoutClassicOEHeaderRow = unit.LayoutClassicOEHeaderRow

var classicOEHint = unit.ClassicOEHint

var ClassicOEListDoc = unit.ClassicOEListDoc

var ClassicOEStaticRow = unit.ClassicOEStaticRow

var classicOERows = unit.ClassicOERows

var classicOENote = unit.ClassicOENote

var classicOEUseNote = unit.ClassicOEUseNote

const classicOEFolderAnims = unit.ClassicOEFolderAnims

const classicOEFolderMotions = unit.ClassicOEFolderMotions

const classicOEFolderActions = unit.ClassicOEFolderActions

const classicOEFolderTree = unit.ClassicOEFolderTree

const classicOEFolderCount = unit.ClassicOEFolderCount

const classicOETreeMaxDepth = unit.ClassicOETreeMaxDepth

var EnsureClassicOEFolders = unit.EnsureClassicOEFolders

var LayoutClassicOEFolderRow = unit.LayoutClassicOEFolderRow

var LayoutClassicOEHint = unit.LayoutClassicOEHint

var ClassicOETreeActivate = unit.ClassicOETreeActivate

var classicOETreeJumpIndex = unit.ClassicOETreeJumpIndex

const classicOEDialogNone = unit.ClassicOEDialogNone

const classicOEDialogTimeline = unit.ClassicOEDialogTimeline

const classicOEDialogAction = unit.ClassicOEDialogAction

const classicOEDoubleClickDelay = unit.ClassicOEDoubleClickDelay

var ClassicOEDialogOpen = unit.ClassicOEDialogOpen

var ClassicOEDoubleClick = unit.ClassicOEDoubleClick

var classicOETreeRowDialog = unit.ClassicOETreeRowDialog

var classicOETreeRoots = unit.ClassicOETreeRoots

type classicOETreeRow = unit.ClassicOETreeRow

var classicOETreeRows = unit.ClassicOETreeRows

var LayoutClassicOEEventList = unit.LayoutClassicOEEventList

var LayoutClassicOEConditionList = unit.LayoutClassicOEConditionList

var LayoutClassicOEModifierList = unit.LayoutClassicOEModifierList

var ClassicOEModifierRow = unit.ClassicOEModifierRow

var ClassicOENamedCountRows = unit.ClassicOENamedCountRows

var EnsureClassicOEClause = unit.EnsureClassicOEClause

var clauseEditorValues = unit.ClauseEditorValues

var LayoutClassicOEClauseEditor = unit.LayoutClassicOEClauseEditor

var LayoutClassicOEClauseList = unit.LayoutClassicOEClauseList

var ClassicOEClauseRow = unit.ClassicOEClauseRow

var LayoutClassicOEConditionEditorList = unit.LayoutClassicOEConditionEditorList

var ClassicOEConditionRow = unit.ClassicOEConditionRow

var EnsureClassicOECondition = unit.EnsureClassicOECondition

var LayoutClassicOEConditionEditor = unit.LayoutClassicOEConditionEditor

var ApplyClassicOEClause = unit.ApplyClassicOEClause

var ApplyClassicOECondition = unit.ApplyClassicOECondition

var classicOESelectedUIFields = unit.ClassicOESelectedUIFields

var classicOESelectedUIMatrixFields = unit.ClassicOESelectedUIMatrixFields

var selectedUIValues = unit.SelectedUIValues

var selectedUIDisplayTypeCode = unit.SelectedUIDisplayTypeCode

var selectedUIFromValues = unit.SelectedUIFromValues

var selectedUIDisplayTypeCodeFromValue = unit.SelectedUIDisplayTypeCodeFromValue

var LayoutClassicOESelectedUIVisibility = unit.LayoutClassicOESelectedUIVisibility

var LayoutClassicOESelectedUIDisplayType = unit.LayoutClassicOESelectedUIDisplayType

var LayoutClassicOESelectedUIPair = unit.LayoutClassicOESelectedUIPair

var LayoutClassicOESelectedUITriple = unit.LayoutClassicOESelectedUITriple

var LayoutClassicOESelectedUIAxes = unit.LayoutClassicOESelectedUIAxes

var LayoutClassicOESelectedUIMatrix = unit.LayoutClassicOESelectedUIMatrix

var LayoutClassicOESelectedUIAxisHeadings = unit.LayoutClassicOESelectedUIAxisHeadings

var LayoutClassicOESelectedUIMatrixRow = unit.LayoutClassicOESelectedUIMatrixRow

var ClassicOEWheEditorField = unit.ClassicOEWheEditorField

var EnsureClassicOEModifier = unit.EnsureClassicOEModifier

var LayoutClassicOEModifierEditor = unit.LayoutClassicOEModifierEditor

var ClassicOEWheEditorRow = unit.ClassicOEWheEditorRow

var ClassicOEWheButtons = unit.ClassicOEWheButtons

var CreateClassicOESelectedUI = unit.CreateClassicOESelectedUI

var UpdateUnitSelectionIndicator = unit.UpdateUnitSelectionIndicator

var ApplyClassicOESelectedUI = unit.ApplyClassicOESelectedUI

var ApplyClassicOEModifier = unit.ApplyClassicOEModifier

const unitSectionEvents = unit.UnitSectionEvents

var unitEventsWhePath = unit.UnitEventsWhePath

var timelineMatchesClip = unit.TimelineMatchesClip

var SelectedUnitEventTimeline = unit.SelectedUnitEventTimeline

var SelectUnitEventTimeline = unit.SelectUnitEventTimeline

var EnsureUnitEventClicks = unit.EnsureUnitEventClicks

var EnsureUnitEventAnimClicks = unit.EnsureUnitEventAnimClicks

var EnsureUnitMeshClicks = unit.EnsureUnitMeshClicks

var EnsureUnitMeshNameClicks = unit.EnsureUnitMeshNameClicks

var HandleUnitMeshToggles = unit.HandleUnitMeshToggles

const unitMeshSectionMaxDp = unit.UnitMeshSectionMaxDp

const eyeToggleSizeDp = unit.EyeToggleSizeDp

var UnitMeshRows = unit.UnitMeshRows

var MeshRowsMessage = unit.MeshRowsMessage

var EyeToggle = unit.EyeToggle

var drawEyeGlyph = unit.DrawEyeGlyph

var clipEventKey = unit.ClipEventKey

var UnitEvtSelectedIndices = unit.UnitEvtSelectedIndices

var UnitEvtRowSelected = unit.UnitEvtRowSelected

var UnitEvtBlurFields = unit.UnitEvtBlurFields

var SyncUnitEventForm = unit.SyncUnitEventForm

var unitEventTimeEdit = unit.UnitEventTimeEdit

var unitEventTime = unit.UnitEventTime

var StatusEventAdded = unit.StatusEventAdded

var StepUnitClip = unit.StepUnitClip

var CopySelectedUnitEvents = unit.CopySelectedUnitEvents

var CopySelectedUnitTimeline = unit.CopySelectedUnitTimeline

var timelineClipFrom = unit.TimelineClipFrom

var timelineOf = unit.TimelineOf

type timelinePaste = unit.TimelinePaste

var timelinePasteMessage = unit.TimelinePasteMessage

var fxEventSlot = unit.FxEventSlot

var unitEventShift = unit.UnitEventShift

var unitEventShiftLabel = unit.UnitEventShiftLabel

var unitEventTimeLabel = unit.UnitEventTimeLabel

var UnitEventsMessage = unit.UnitEventsMessage

var SelectedUnitEvtAction = unit.SelectedUnitEvtAction

var EnsureUnitEvtActionClicks = unit.EnsureUnitEvtActionClicks

var CopySelectedUnitAction = unit.CopySelectedUnitAction

var actionMotionNames = unit.ActionMotionNames

var actionSubactions = unit.ActionSubactions

var actionIndexByName = unit.ActionIndexByName

var unitActLinks = unit.UnitActLinks

var unitActLinkAt = unit.UnitActLinkAt

var unitActGuardLabel = unit.UnitActGuardLabel

var EnsureUnitActConditions = unit.EnsureUnitActConditions

var ApplyUnitActConditions = unit.ApplyUnitActConditions

const actAddIf = unit.ActAddIf

const actAddElseIf = unit.ActAddElseIf

const actAddElse = unit.ActAddElse

var actIsHead = unit.ActIsHead

var actChainStart = unit.ActChainStart

var actChainEnd = unit.ActChainEnd

var actLocate = unit.ActLocate

var actCombined = unit.ActCombined

var actMoveLink = unit.ActMoveLink

var actAddLink = unit.ActAddLink

var actDeleteLink = unit.ActDeleteLink

var UnitActMutate = unit.UnitActMutate

var UnitActAddChoice = unit.UnitActAddChoice

var UnitActMoveChoice = unit.UnitActMoveChoice

var UnitActDeleteChoice = unit.UnitActDeleteChoice

var UnitActMoveEnabled = unit.UnitActMoveEnabled

var UnitActionConditions = unit.UnitActionConditions

var LayoutUnitActConditionRow = unit.LayoutUnitActConditionRow

var UnitActFieldEditor = unit.UnitActFieldEditor

var UnitActGuardChip = unit.UnitActGuardChip

var EnsureUnitEvtEntryEditors = unit.EnsureUnitEvtEntryEditors

var SyncUnitEvtEntryEditors = unit.SyncUnitEvtEntryEditors

var UnitEvtEntryRow = unit.UnitEvtEntryRow

var unitEventKindTag = unit.UnitEventKindTag

type evtPickTarget = unit.EvtPickTarget

const evtPickNone = unit.EvtPickNone

const evtPickName = unit.EvtPickName

const evtPickFX = unit.EvtPickFX

const evtPickSound = unit.EvtPickSound

const evtPickMarker = unit.EvtPickMarker

const evtPickerMaxHits = unit.EvtPickerMaxHits

type evtPicker = unit.EvtPicker

var OpenEvtPicker = unit.OpenEvtPicker

var EvtPickerItems = unit.EvtPickerItems

var evtTreeFolderFiles = unit.EvtTreeFolderFiles

var RefreshEvtPickerHits = unit.RefreshEvtPickerHits

var ApplyEvtPick = unit.ApplyEvtPick

var evtPickerTitle = unit.EvtPickerTitle

type unitEvtFire = unit.UnitEvtFire

type unitEvtPlayer = unit.UnitEvtPlayer

var EvtPlayerStopAll = unit.EvtPlayerStopAll

var unitEvtStartVoice = unit.UnitEvtStartVoice

var UnitEvtSelectedIsSound = unit.UnitEvtSelectedIsSound

var UnitEvtTimelinePlay = unit.UnitEvtTimelinePlay

var unitEvtFillRect = unit.UnitEvtFillRect

const squadRowHeightDp = unit.SquadRowHeightDp

var SquadEmptyMessage = unit.SquadEmptyMessage

var squadSizeLabel = unit.SquadSizeLabel

var squadScalars = unit.SquadScalars

var birthResourceOrder = unit.BirthResourceOrder

var birthResourceLabels = unit.BirthResourceLabels

var squadRowsForTest = unit.SquadRowsForTest

const squadRacesFolder = unit.SquadRacesFolder

type unitSquadRef = unit.UnitSquadRef

type unitSquadBatch = unit.UnitSquadBatch

const unitSquadBatchSize = unit.UnitSquadBatchSize

var StopUnitSquadScan = unit.StopUnitSquadScan

var PollUnitSquads = unit.PollUnitSquads

var SortUnitSquads = unit.SortUnitSquads

var sortSquadRefs = unit.SortSquadRefs

var squadRefLess = unit.SquadRefLess

var squadNameOf = unit.SquadNameOf

var readBirthResource = unit.ReadBirthResource

var collectSquadRgds = unit.CollectSquadRgds

var SquadsOfUnitOf = unit.SquadsOfUnitOf

const unitTabKind = unit.UnitTabKind

const unitTabLabel = unit.UnitTabLabel

const unitTabPath = unit.UnitTabPath

var isUnitTab = unit.IsUnitTab

var UnitTabIndex = unit.UnitTabIndex

var OpenUnitTab = unit.OpenUnitTab

var unitTabLabelFor = unit.UnitTabLabelFor

var unitTabHasNoFile = unit.UnitTabHasNoFile

type unitNameBatch = unit.UnitNameBatch

const unitNameBatchSize = unit.UnitNameBatchSize

var StartUnitNameScan = unit.StartUnitNameScan

var StopUnitNameScan = unit.StopUnitNameScan

var PollUnitNames = unit.PollUnitNames

var readUnitScreenNameID = unit.ReadUnitScreenNameID

var unitScreenNameIDOf = unit.UnitScreenNameIDOf

var unitFileLabel = unit.UnitFileLabel

var unitInfoFieldAt = unit.UnitInfoFieldAt

var RevertUnitDetails = unit.RevertUnitDetails

var rgdPathOf = unit.RgdPathOf

var unitBoolText = unit.UnitBoolText

var EnsureUnitInfoClicks = unit.EnsureUnitInfoClicks

var unitFieldNote = unit.UnitFieldNote

var unitInfoRowSelectable = unit.UnitInfoRowSelectable

var UnitInfoRowFrame = unit.UnitInfoRowFrame

var dimOverlayColor = unit.DimOverlayColor

type unitSection = unit.UnitSection

var unitSections = unit.UnitSections

var unitSectionNamed = unit.UnitSectionNamed

const unitLeftColumnDp = unit.UnitLeftColumnDp

const unitColHeaderHeight = unit.UnitColHeaderHeight

const unitColGapDp = unit.UnitColGapDp

const unitColMinWidthDp = unit.UnitColMinWidthDp

var CurrentUnitSection = unit.CurrentUnitSection

var unitToolButtons = unit.UnitToolButtons

const unitPanelGapDp = unit.UnitPanelGapDp

var UnitToolButton = unit.UnitToolButton

var frameMeasured = unit.FrameMeasured

var EnsureUnitClicks = unit.EnsureUnitClicks

var unitColumnWidths = unit.UnitColumnWidths

var UnitColumn = unit.UnitColumn

var LayoutUnitLeftColumn = unit.LayoutUnitLeftColumn

var LayoutUnitColumnHeader = unit.LayoutUnitColumnHeader

var LayoutUnitSectionList = unit.LayoutUnitSectionList

var LayoutUnitSectionRow = unit.LayoutUnitSectionRow

const actionIconCommandsDir = unit.ActionIconCommandsDir

var actionIconFile = unit.ActionIconFile

var actionIconHasWord = unit.ActionIconHasWord

type iconCacheEntry = unit.IconCacheEntry

var unitIconLayout = unit.UnitIconLayout

var folderIconLayout = unit.FolderIconLayout

const unitPickerStripHeightDp = unit.UnitPickerStripHeightDp

const unitTabPadDp = unit.UnitTabPadDp

const unitListVisibleRows = unit.UnitListVisibleRows

var ApplyPendingUnitPick = unit.ApplyPendingUnitPick

var UnitPickerField = unit.UnitPickerField

var unitPickerBorder = unit.UnitPickerBorder

var EnsureUnitRaceClicks = unit.EnsureUnitRaceClicks

var EnsureUnitDropdownClicks = unit.EnsureUnitDropdownClicks

var UnitColumnBody = unit.UnitColumnBody

var UnitSectionFrame = unit.UnitSectionFrame

const unitInfoRowHeightDp = unit.UnitInfoRowHeightDp

type unitWeaponRow = unit.UnitWeaponRow

var unitWeaponRows = unit.UnitWeaponRows

var unitAbilityRows = unit.UnitAbilityRows

var UnitGroupHeading = unit.UnitGroupHeading

var unitWeaponAt = unit.UnitWeaponAt

var unitWeaponSlotFields = unit.UnitWeaponSlotFields

var EnsureUnitRowClicks = unit.EnsureUnitRowClicks

var UnitPickerOpen = unit.UnitPickerOpen

var CloseUnitPickers = unit.CloseUnitPickers

var HandleUnitPickerKeys = unit.HandleUnitPickerKeys

type unitImpRef = unit.UnitImpRef

var UnitImportBoneIndex = unit.UnitImportBoneIndex

var unitImportFrames = unit.UnitImportFrames

var unitImportFrameKinds = unit.UnitImportFrameKinds

var UnitImportRowClick = unit.UnitImportRowClick

var writeImportPos = unit.WriteImportPos

var writeImportRot = unit.WriteImportRot

const unitViewportSideDp = unit.UnitViewportSideDp

var UnitModelSideEmpty = unit.UnitModelSideEmpty

const unitPlaybackStripDp = unit.UnitPlaybackStripDp

var unitPlaybackRow = unit.UnitPlaybackRow

var UnitModelTransportButtons = unit.UnitModelTransportButtons

const unitFrameLabelDp = unit.UnitFrameLabelDp

var UnitModelFrameLabel = unit.UnitModelFrameLabel

var UnitModelViewButtons = unit.UnitModelViewButtons

var UnitModelFileLabel = unit.UnitModelFileLabel

var UnitListModifiers = unit.UnitListModifiers

var BoneKeyRowPicked = unit.BoneKeyRowPicked

var boneFieldAxes = unit.BoneFieldAxes

var boneFieldTexts = unit.BoneFieldTexts

var UnitBoneKeyFieldsRow = unit.UnitBoneKeyFieldsRow

var UnitBoneKeyField = unit.UnitBoneKeyField

var ParseBoneGroup = unit.ParseBoneGroup

var ModifiedBoneAxes = unit.ModifiedBoneAxes

const maxAnimFrames = unit.MaxAnimFrames

var UnitAnimFramesRow = unit.UnitAnimFramesRow

var SyncAnimFramesField = unit.SyncAnimFramesField

var boneAngleSliderDeg = unit.BoneAngleSliderDeg

var BoneKeyShiftFrames = unit.BoneKeyShiftFrames

const boneUndoLimit = unit.BoneUndoLimit

var PushBoneUndo = unit.PushBoneUndo

var boneTrackKeys = unit.BoneTrackKeys

var UnitBoneKeyRow = unit.UnitBoneKeyRow

var unitViewportRowText = unit.UnitViewportRowText

var EnsureUnitAnimClicks = unit.EnsureUnitAnimClicks

var EnsureUnitMarkerClicks = unit.EnsureUnitMarkerClicks

var EnsureUnitBoneClicks = unit.EnsureUnitBoneClicks

var EnsureUnitBoneKeyClicks = unit.EnsureUnitBoneKeyClicks

var UnitModelLoaded = unit.UnitModelLoaded

var UnitModelAnimNames = unit.UnitModelAnimNames

var unitModelClipDuration = unit.UnitModelClipDuration

var Rrect = unit.Rrect

const unitSectionViewport = unit.UnitSectionViewport

const unitModelArtRoot = unit.UnitModelArtRoot

const unitGroundTexture = unit.UnitGroundTexture

var errNoUnitModel = unit.ErrNoUnitModel

type unitModelResult = unit.UnitModelResult

type unitModelSource = unit.UnitModelSource

var newUnitModelSource = unit.NewUnitModelSource

var unitAnimatorName = unit.UnitAnimatorName

var unitModelPath = unit.UnitModelPath

var ResetUnitModel = unit.ResetUnitModel

var selectObjectAction = unit.SelectObjectAction

var ActivateUnitAction = unit.ActivateUnitAction

var UnitModelViewer = unit.UnitModelViewer

var LookThroughCamera = unit.LookThroughCamera

var ResetCamera = unit.ResetCamera

var PanCamera = unit.PanCamera

var meshMaterialRefs = unit.MeshMaterialRefs

var applyMeshMaterialRefs = unit.ApplyMeshMaterialRefs

var equalRefs = unit.EqualRefs

var UnitMeshMatFieldText = unit.UnitMeshMatFieldText

var ReloadUnitModelMaterials = unit.ReloadUnitModelMaterials

var OpenUnitMeshMaterialDialog = unit.OpenUnitMeshMaterialDialog

var CloseUnitMeshMaterialDialog = unit.CloseUnitMeshMaterialDialog

var PreviewUnitMeshMaterialDialog = unit.PreviewUnitMeshMaterialDialog

var UnitMeshMatColumn = unit.UnitMeshMatColumn

var UnitMeshMatNaturalHeight = unit.UnitMeshMatNaturalHeight

var UnitMeshMatTitle = unit.UnitMeshMatTitle

var UnitMeshMatFields = unit.UnitMeshMatFields

var UnitMeshMatNote = unit.UnitMeshMatNote

var UnitMeshMatFooter = unit.UnitMeshMatFooter
