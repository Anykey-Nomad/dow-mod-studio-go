package app

import "dow-mod-studio/internal/app/editors"

// the format editors lives in internal/app/editors. These aliases keep every call
// site in app — and in the tests — reading exactly as it did before the move.

var FdaPropertiesTable = editors.FdaPropertiesTable

var FdaInfoTable = editors.FdaInfoTable

var FdaUpdateSliders = editors.FdaUpdateSliders

var FdaHandleSliderSeek = editors.FdaHandleSliderSeek

var FdaHandleVolumeSlider = editors.FdaHandleVolumeSlider

var fmtDuration = editors.FmtDuration

var buildWavePeaks = editors.BuildWavePeaks

var FdaPosFor = editors.FdaPosFor

var FdaSetVolumeFor = editors.FdaSetVolumeFor

var FdaPauseAllOthers = editors.FdaPauseAllOthers

var fdaTargetWAV = editors.FdaTargetWAV

type linkRow = editors.LinkRowData

var groupLinkRows = editors.GroupLinkRows

var LinkGroupTitle = editors.LinkGroupTitle

var LinkRow = editors.LinkRow

var LinkNote = editors.LinkNote

type brokenLinkGroup = editors.BrokenLinkGroup

var brokenLinkGroups = editors.BrokenLinkGroups

var clipName = editors.ClipName

type graphLinkBatch = editors.GraphLinkBatch

const graphLinkBatchSize = editors.GraphLinkBatchSize

var StopGraphLinkScan = editors.StopGraphLinkScan

var PollGraphLinks = editors.PollGraphLinks

type imageDecodeResult = editors.ImageDecodeResult

const imageCacheMaxBytes = editors.ImageCacheMaxBytes

const imageCacheMaxEntries = editors.ImageCacheMaxEntries

type imageCacheEntry = editors.ImageCacheEntry

var imageCacheKey = editors.ImageCacheKey

var CachedImage = editors.CachedImage

var CacheDecodedImage = editors.CacheDecodedImage

var imageCacheEntryCost = editors.ImageCacheEntryCost

var imageImagesCost = editors.ImageImagesCost

var ClearImageCache = editors.ClearImageCache

var OpenImage = editors.OpenImage

var StartImageDecode = editors.StartImageDecode

var SelectImageMip = editors.SelectImageMip

var LayoutImageViewer = editors.LayoutImageViewer

var DrawImageCanvas = editors.DrawImageCanvas

var ImageInfoTable = editors.ImageInfoTable

type RshImage = editors.RshImage

type RshMip = editors.RshMip

var imageViewerExtensions = editors.ImageViewerExtensions

var isImageViewerFile = editors.IsImageViewerFile

var decodeImageData = editors.DecodeImageData

var decodeImageDataWithMips = editors.DecodeImageDataWithMips

var decodeTGA = editors.DecodeTGA

var decodeRshImage = editors.DecodeRshImage

var RshImages = editors.RshImages

var RshRawImage = editors.RshRawImage

var minInt = editors.MinInt

var maxInt = editors.MaxInt

var OpenWav = editors.OpenWav

var wavPCM16 = editors.WavPCM16

var PauseOtherWavPlayers = editors.PauseOtherWavPlayers

var StartWavPlayback = editors.StartWavPlayback

var WavPosFor = editors.WavPosFor

var WavSetVolume = editors.WavSetVolume

var WavUpdateSliders = editors.WavUpdateSliders

var WavHandleSliderSeek = editors.WavHandleSliderSeek

var WavHandleVolumeSlider = editors.WavHandleVolumeSlider

var wavDuration = editors.WavDuration

var ConvertWavToFDA = editors.ConvertWavToFDA

var LayoutWavWaveform = editors.LayoutWavWaveform

var WavInfoTable = editors.WavInfoTable

var wavTargetFDA = editors.WavTargetFDA

var writeFDAOverlay = editors.WriteFDAOverlay

const localeFieldHeightDp = editors.LocaleFieldHeightDp

const localeFieldPadDp = editors.LocaleFieldPadDp

const localeListWidthDp = editors.LocaleListWidthDp

const localeRowHeightDp = editors.LocaleRowHeightDp

const localeListMarginDp = editors.LocaleListMarginDp

const localeListPadDp = editors.LocaleListPadDp

const localeListBorderDp = editors.LocaleListBorderDp

var LocaleLabel = editors.LocaleLabel

var RefreshLocales = editors.RefreshLocales

var ApplyLocale = editors.ApplyLocale

var LocaleSearchRoot = editors.LocaleSearchRoot

var containsFold = editors.ContainsFold

var EnsureLocaleClicks = editors.EnsureLocaleClicks

var CloseLocale = editors.CloseLocale

var CloseLocaleUnderOtherMenuBarPopups = editors.CloseLocaleUnderOtherMenuBarPopups

var LayoutLocaleSlot = editors.LayoutLocaleSlot

var LayoutLocaleField = editors.LayoutLocaleField

var LocaleRow = editors.LocaleRow

var JumpToLocaleEntry = editors.JumpToLocaleEntry

var OpenLocaleFile = editors.OpenLocaleFile

var FocusLocaleLine = editors.FocusLocaleLine

var localeLineRange = editors.LocaleLineRange

type localeEntry = editors.LocaleEntry

type localeTable = editors.LocaleTable

const localeDefault = editors.LocaleDefault

const localeDirName = editors.LocaleDirName

const localeEditFileName = editors.LocaleEditFileName

var setLocaleOverrideDir = editors.SetLocaleOverrideDir

var invalidateLocaleCache = editors.InvalidateLocaleCache

var decodeUCS = editors.DecodeUCS

var trimBOM = editors.TrimBOM

var localeModuleDir = editors.LocaleModuleDir

var localeRoots = editors.LocaleRoots

var availableLocales = editors.AvailableLocales

var setLocale = editors.SetLocale

var localeLookup = editors.LocaleLookup

var localisedString = editors.LocalisedString

var allocateLocaleID = editors.AllocateLocaleID

var localeOverrideIsOurs = editors.LocaleOverrideIsOurs

type localeRowAction = editors.LocaleRowAction

const localeRowNothing = editors.LocaleRowNothing

const localeRowEdit = editors.LocaleRowEdit

const localeRowCreate = editors.LocaleRowCreate

const localeRowJump = editors.LocaleRowJump

var localeFieldForID = editors.LocaleFieldForID

var localeRowActionFor = editors.LocaleRowActionFor

type localeEditRequest = editors.LocaleEditRequest

var OpenLocaleEdit = editors.OpenLocaleEdit

var CloseLocaleEdit = editors.CloseLocaleEdit

var LocaleEditCaption = editors.LocaleEditCaption

var LocaleEditNote = editors.LocaleEditNote

var LocaleEditTag = editors.LocaleEditTag

var PollLocaleEditPress = editors.PollLocaleEditPress

var LocaleEditHover = editors.LocaleEditHover

var LocaleRowTooltip = editors.LocaleRowTooltip

var LocaleRowText = editors.LocaleRowText

var LocaleEditableRow = editors.LocaleEditableRow

var LocaleRowBody = editors.LocaleRowBody

var LocaleRowAffordances = editors.LocaleRowAffordances

var decodeUCSDocument = editors.DecodeUCSDocument

var localeOverrideRoot = editors.LocaleOverrideRoot

var decodeUTF16 = editors.DecodeUTF16

var parseUCS = editors.ParseUCS

var isLocaleID = editors.IsLocaleID

var localeSourceFiles = editors.LocaleSourceFiles

var buildLocaleTable = editors.BuildLocaleTable

var currentLocaleTable = editors.CurrentLocaleTable

var localeLineNumber = editors.LocaleLineNumber

var resetLocaleTables = editors.ResetLocaleTables

var writeUCSDocument = editors.WriteUCSDocument

var ucsSanitiseText = editors.UcsSanitiseText

var ucsSetEntry = editors.UcsSetEntry

var ucsDeleteEntry = editors.UcsDeleteEntry

var readUCSDocument = editors.ReadUCSDocument

var localeState = editors.LocaleState

type newObjKind = editors.NewObjKind

const newObjUnit = editors.NewObjUnit

const newObjBuilding = editors.NewObjBuilding

var newObjTemplateNameError = editors.NewObjTemplateNameError

var CloseNewObjDialog = editors.CloseNewObjDialog

var NewObjHint = editors.NewObjHint

var RgdEnsureEditorClick = editors.RgdEnsureEditorClick

var FocusClickedRgdEditor = editors.FocusClickedRgdEditor

var RgdEnsureSuggestionClicks = editors.RgdEnsureSuggestionClicks

var rgdCollectStringValues = editors.RgdCollectStringValues

var rgdSuggestionsFor = editors.RgdSuggestionsFor

var rgdVisibleSuggestions = editors.RgdVisibleSuggestions

var rgdSuggestionLabel = editors.RgdSuggestionLabel

var RgdEnsureBoolClick = editors.RgdEnsureBoolClick

var rgdIsEditableType = editors.RgdIsEditableType

var rgdApplyScalarText = editors.RgdApplyScalarText

var rgdApplyReferenceText = editors.RgdApplyReferenceText

var RgdApplyEditorText = editors.RgdApplyEditorText

var RgdFlushPending = editors.RgdFlushPending

var HandleRgdSave = editors.HandleRgdSave

var LocaleRowDisplay = editors.LocaleRowDisplay

var TableRowLocaleText = editors.TableRowLocaleText

var RgdEnsureLocTag = editors.RgdEnsureLocTag

var PollLocaleLinkPress = editors.PollLocaleLinkPress

var TableRowEditor = editors.TableRowEditor

var OpenRgd = editors.OpenRgd

var EnsureRgdMidClicks = editors.EnsureRgdMidClicks

var RebuildRgdMidNodesFor = editors.RebuildRgdMidNodesFor

var RgdFlattenChildrenFor = editors.RgdFlattenChildrenFor

var RgdFindEntryFor = editors.RgdFindEntryFor

var RgdSelectedEntryFor = editors.RgdSelectedEntryFor

var childrenRows = editors.ChildrenRows

var isTextExtension = editors.IsTextExtension

var OpenTextFile = editors.OpenTextFile

var HandleTextSave = editors.HandleTextSave

var LayoutTextEditor = editors.LayoutTextEditor

var widgetBorderTextEditor = editors.WidgetBorderTextEditor

const syntaxGutterDp = editors.SyntaxGutterDp

var paintSyntaxHighlight = editors.PaintSyntaxHighlight

var itoa = editors.Itoa
