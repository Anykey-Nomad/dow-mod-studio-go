package app

import "dow-mod-studio/internal/app/fx"

// The FX overlay lives in internal/app/fx. These aliases keep every call
// site in app — and in the tests — reading exactly as it did before the move.

var ModelFxChecklist = fx.ModelFxChecklist

var FxAttributeDialogLauncher = fx.FxAttributeDialogLauncher

var FxChecklistViewer = fx.FxChecklistViewer

var FxChecklistRow = fx.FxChecklistRow

var EnsureFXOverride = fx.EnsureFXOverride

var SyncFXAttributeEditors = fx.SyncFXAttributeEditors

var ApplyFXAttributeField = fx.ApplyFXAttributeField

var fxAttributeOriginAxis = fx.FxAttributeOriginAxis

var fxEditorValue = fx.FxEditorValue

var ModelFxAttributeDialog = fx.ModelFxAttributeDialog

var FxAttributeTitle = fx.FxAttributeTitle

var FxAttributeField = fx.FxAttributeField

var FxAttributeButtonRows = fx.FxAttributeButtonRows

var fxAttrToggleLabel = fx.FxAttrToggleLabel

var UpdateFXAttributeDrag = fx.UpdateFXAttributeDrag

var paintFxOverlay = fx.PaintFxOverlay

type fxGpuScene = fx.FxGpuScene

type fxGpuGroup = fx.FxGpuGroup

type fxGpuBeam = fx.FxGpuBeam

type fxSceneLight = fx.FxSceneLight

type fxGroupKey = fx.FxGroupKey

var fxGroupOrigin = fx.FxGroupOrigin

var fxBuildGpuScene = fx.FxBuildGpuScene

var sortFxGroups = fx.SortFxGroups

var modelFXLabel = fx.ModelFXLabel

const modelFXScaleStep = fx.ModelFXScaleStep

const modelFXScaleMin = fx.ModelFXScaleMin

const modelFXScaleMax = fx.ModelFXScaleMax

var ScaleFX = fx.ScaleFX

var modelFXScaleLabel = fx.ModelFXScaleLabel

var ToggleFX = fx.ToggleFX

var FxApply = fx.FxApply

var FxApplySoundsOnly = fx.FxApplySoundsOnly

var FxApplySounds = fx.FxApplySounds

var FxSoundTick = fx.FxSoundTick

var FxOrigins = fx.FxOrigins

var fxAxisX = fx.FxAxisX

var fxAxisY = fx.FxAxisY

var fxAxisZ = fx.FxAxisZ

var fxFallbackFrame = fx.FxFallbackFrame

var fxRotated = fx.FxRotated

var fxRotateAbout = fx.FxRotateAbout

var FxAdvance = fx.FxAdvance

var FxStepOnClip = fx.FxStepOnClip

var FxSelectionAnimName = fx.FxSelectionAnimName

var FxSelectionReset = fx.FxSelectionReset

var FxEffectSelected = fx.FxEffectSelected

var FxEffectVisible = fx.FxEffectVisible

var FxSetEffectSelected = fx.FxSetEffectSelected

var FxActiveEffects = fx.FxActiveEffects

var FxActiveIndices = fx.FxActiveIndices

var fxScaledEffects = fx.FxScaledEffects

type fxAttributeOverride = fx.FxAttributeOverride

type fxOverlayEffect = fx.FxOverlayEffect

var fxScaleSprite = fx.FxScaleSprite

var fxRotateVectorBetween = fx.FxRotateVectorBetween

var fxFrameToward = fx.FxFrameToward

var fxOrientSprite = fx.FxOrientSprite

const fxBeamFreeLength = fx.FxBeamFreeLength

var fxOverlayStep = fx.FxOverlayStep

var renderFxOverlay = fx.RenderFxOverlay

var drawFxHorizontalSprite = fx.DrawFxHorizontalSprite

var drawFxBillboard = fx.DrawFxBillboard

var fxProject = fx.FxProject

var sampleFxTexture = fx.SampleFxTexture

var blendFxPixel = fx.BlendFxPixel

var fxTextureCandidates = fx.FxTextureCandidates

var fxTexturePath = fx.FxTexturePath

type fxLoadResult = fx.FxLoadResult

var fxTimelineMap = fx.FxTimelineMap

type fxEventFire = fx.FxEventFire

var fxTimelineFires = fx.FxTimelineFires

var fxTimelineFireTimes = fx.FxTimelineFireTimes

type fxLoadOpts = fx.FxLoadOpts

var fxSoundAlwaysPlays = fx.FxSoundAlwaysPlays

const fxSoundBogusRate = fx.FxSoundBogusRate

const fxSoundRealRate = fx.FxSoundRealRate

var fxSoundDeclaredRate = fx.FxSoundDeclaredRate

var readFxWheFile = fx.ReadFxWheFile

var fxBeamOtherMarker = fx.FxBeamOtherMarker

var fxEventSpansDistance = fx.FxEventSpansDistance

var fxEventAlignsToOther = fx.FxEventAlignsToOther

var fxEventMarker = fx.FxEventMarker

var fxIsGroundEffect = fx.FxIsGroundEffect

var fxGroundOrigin = fx.FxGroundOrigin

var fxLuaPath = fx.FxLuaPath

var readFxLuaFile = fx.ReadFxLuaFile

var readFxTextureBytes = fx.ReadFxTextureBytes

var readFxTextureByName = fx.ReadFxTextureByName

var fxTextureTries = fx.FxTextureTries

var toRGBA = fx.ToRGBA

var drawFxBeam = fx.DrawFxBeam

var drawFxBeamQuad = fx.DrawFxBeamQuad

const markerArrowWorldLength = fx.MarkerArrowWorldLength

var PaintMarkerOverlay = fx.PaintMarkerOverlay

var MarkerHighlight = fx.MarkerHighlight

var MarkerArrow = fx.MarkerArrow

var paintMarkerArrow = fx.PaintMarkerArrow

var paintMarkerRing = fx.PaintMarkerRing

var PaintFXAttributeMarkers = fx.PaintFXAttributeMarkers

const fxAxesPairGap = fx.FxAxesPairGap

const fxAxesTriadLen = fx.FxAxesTriadLen

const fxAxesProbe = fx.FxAxesProbe

type fxOriginAxesEntry = fx.FxOriginAxesEntry

var fxOriginAxesKey = fx.FxOriginAxesKey

var fxOriginAxisFrames = fx.FxOriginAxisFrames

var FxAxesButton = fx.FxAxesButton

var ModelFxAxesOverlay = fx.ModelFxAxesOverlay

var LayoutFXOriginAxes = fx.LayoutFXOriginAxes

var paintFXAxesLine = fx.PaintFXAxesLine

var paintFXAxesDot = fx.PaintFXAxesDot

var paintFXAxesText = fx.PaintFXAxesText

var fxAxesColor = fx.FxAxesColor

var modelMarkerForwards = fx.ModelMarkerForwards
