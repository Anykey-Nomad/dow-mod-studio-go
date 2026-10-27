package app

import "dow-mod-studio/internal/app/viewer"

// The model viewer lives in internal/app/viewer. These aliases keep every
// call site in app — and in the tests — reading exactly as it did before
// the move.

type modelFrame = viewer.ModelFrame

type modelMaterialResult = viewer.ModelMaterialResult

type modelViewer = viewer.ModelViewer

var newModelViewer = viewer.NewModelViewer

var newObjectModelViewer = viewer.NewObjectModelViewer

var ModelClose = viewer.ModelClose

var Close = viewer.Close

var Invalidate = viewer.Invalidate

var SetRenderSize = viewer.SetRenderSize

var RenderModelOnGpu = viewer.RenderModelOnGpu

const modelGpuRetryLimit = viewer.ModelGpuRetryLimit

var modelGpuRetryLogged = viewer.ModelGpuRetryLogged

var modelGpuErrTransient = viewer.ModelGpuErrTransient

var RequestRender = viewer.RequestRender

var ModelData = viewer.ModelData

var SelectionIndicatorIfShown = viewer.SelectionIndicatorIfShown

var Options = viewer.Options

var ModelInfoTable = viewer.ModelInfoTable

var ModelMaterialDiagnosticsTable = viewer.ModelMaterialDiagnosticsTable

var diagnosticValue = viewer.DiagnosticValue

var modelPlayLabel = viewer.ModelPlayLabel

var modelClipLabel = viewer.ModelClipLabel

var onOff = viewer.OnOff

var ToggleSound = viewer.ToggleSound

var modelGroundLabel = viewer.ModelGroundLabel

const modelZoomStep = viewer.ModelZoomStep

var ZoomBy = viewer.ZoomBy

var modelZoomByDelta = viewer.ModelZoomByDelta

var hiddenMeshes = viewer.HiddenMeshes

var setMeshHidden = viewer.SetMeshHidden

var meshHidden = viewer.MeshHidden

var modelMeshNames = viewer.ModelMeshNames

var MeshAlphas = viewer.MeshAlphas

const modelUVMinOffset = viewer.ModelUVMinOffset

const modelUVMaxOffset = viewer.ModelUVMaxOffset

const modelUVMinScale = viewer.ModelUVMinScale

const modelUVMaxScale = viewer.ModelUVMaxScale

var modelRowHeight = viewer.ModelRowHeight

const modelZoomInLabel = viewer.ModelZoomInLabel

const modelZoomOutLabel = viewer.ModelZoomOutLabel

var uvTextureLabel = viewer.UvTextureLabel

var UvTextureButton = viewer.UvTextureButton

var EnsureUVTextureClicks = viewer.EnsureUVTextureClicks

var uvWholeTiles = viewer.UvWholeTiles

var modelMaterialsNeedLoad = viewer.ModelMaterialsNeedLoad

var fxListLabel = viewer.FxListLabel

var modelGpuSkipLogged = viewer.ModelGpuSkipLogged

var modelRenderingLogged = viewer.ModelRenderingLogged

var modelGPUPaintSkipLogged = viewer.ModelGPUPaintSkipLogged

var ModelGpuUnavailable = viewer.ModelGpuUnavailable

var ModelGpuRenderer = viewer.ModelGpuRenderer

var GeometryChangedModelGpu = viewer.GeometryChangedModelGpu

var ForgetModelGpu = viewer.ForgetModelGpu

var ReleaseModelGpu = viewer.ReleaseModelGpu

type boneTrackSnapshot = viewer.BoneTrackSnapshot

var SnapshotBoneTrack = viewer.SnapshotBoneTrack

var RestoreBoneTrack = viewer.RestoreBoneTrack

var timeForFrame = viewer.TimeForFrame

var AddBoneKeyAtFrame = viewer.AddBoneKeyAtFrame

type boneKeyRef = viewer.BoneKeyRef

var BoneKeyRefs = viewer.BoneKeyRefs

var BoneKeyIndexAtFrame = viewer.BoneKeyIndexAtFrame

var PickedBoneKeyRef = viewer.PickedBoneKeyRef

var FreeBoneKeyFrame = viewer.FreeBoneKeyFrame

var InsertBoneKeyCopy = viewer.InsertBoneKeyCopy

var DeleteBoneKeyAt = viewer.DeleteBoneKeyAt

var PickedBoneKeyValid = viewer.PickedBoneKeyValid

var DeletePickedBoneKey = viewer.DeletePickedBoneKey

var MovePickedBoneKeyToFrame = viewer.MovePickedBoneKeyToFrame

var shiftKeyFrames = viewer.ShiftKeyFrames

var ShiftBoneKeys = viewer.ShiftBoneKeys

var SetBoneKeyPosition = viewer.SetBoneKeyPosition

var SetBoneKeyRotation = viewer.SetBoneKeyRotation

var BoneKeyTarget = viewer.BoneKeyTarget

var BoneKeyIndexForMode = viewer.BoneKeyIndexForMode

var WindToPickedBoneKey = viewer.WindToPickedBoneKey

var quatMulQ = viewer.QuatMulQ

var quatSame = viewer.QuatSame

var quatFromEulerDeg = viewer.QuatFromEulerDeg

var eulerDegFromQuat = viewer.EulerDegFromQuat

type modelMaterialDiagnostic = viewer.ModelMaterialDiagnostic

var LoadModelMaterials = viewer.LoadModelMaterials

var resolveModelMaterials = viewer.ResolveModelMaterials

var resolveModelMaterialsWithPaths = viewer.ResolveModelMaterialsWithPaths

var modelMaterialReference = viewer.ModelMaterialReference

var attachMaterialSourcePaths = viewer.AttachMaterialSourcePaths

var rshMaterialFormat = viewer.RshMaterialFormat

var decodedMaterialFormat = viewer.DecodedMaterialFormat

var decodeModelMaterials = viewer.DecodeModelMaterials

var materialTextureExtensions = viewer.MaterialTextureExtensions

var materialCandidatePaths = viewer.MaterialCandidatePaths

var materialCandidateNames = viewer.MaterialCandidateNames

var materialDiskPaths = viewer.MaterialDiskPaths

var findMaterialsInTree = viewer.FindMaterialsInTree

var findMaterialInTree = viewer.FindMaterialInTree

var findMaterialFilesInTree = viewer.FindMaterialFilesInTree

var sourceMoreSpecific = viewer.SourceMoreSpecific

var treePathInFolder = viewer.TreePathInFolder

var materialSearchRoots = viewer.MaterialSearchRoots

var readModelMaterial = viewer.ReadModelMaterial

var readModelMaterialWithPath = viewer.ReadModelMaterialWithPath

var materialTreePath = viewer.MaterialTreePath

var findMaterialsOnDisk = viewer.FindMaterialsOnDisk

type uvNet = viewer.UvNet

var UvSelectionList = viewer.UvSelectionList

var UvCurrent = viewer.UvCurrent

var SelectUVMapping = viewer.SelectUVMapping

var UvOffset = viewer.UvOffset

var UvScaleValue = viewer.UvScaleValue

var UvPush = viewer.UvPush

var UvPull = viewer.UvPull

var ResetUVMapping = viewer.ResetUVMapping

var uvMappingNet = viewer.UvMappingNet

var Net = viewer.Net

var UvMappingSummary = viewer.UvMappingSummary

var uvNetTerm = viewer.UvNetTerm

var UvMappingSequence = viewer.UvMappingSequence

var UvMappingNote = viewer.UvMappingNote

var UvUnmappedMeshes = viewer.UvUnmappedMeshes

var modelUVFixed = viewer.ModelUVFixed

var padZeroes = viewer.PadZeroes

var modelUVAdd = viewer.ModelUVAdd

var BoneTrack = viewer.BoneTrack

var BoneKeyFrames = viewer.BoneKeyFrames

var BoneKeyCount = viewer.BoneKeyCount

var BoneKeySelect = viewer.BoneKeySelect

var SetBoneKeyPositionFromPose = viewer.SetBoneKeyPositionFromPose

var SetBoneKeyRotationFromPose = viewer.SetBoneKeyRotationFromPose

var matrixQuat = viewer.MatrixQuat

var boneAtPose = viewer.BoneAtPose

var BoneKeyFrame = viewer.BoneKeyFrame

var BoneKeyFrameAt = viewer.BoneKeyFrameAt

var Pose = viewer.Pose

var StepFrame = viewer.StepFrame

const frameReadTolerance = viewer.FrameReadTolerance

var CurrentFrame = viewer.CurrentFrame

var SelectAnim = viewer.SelectAnim

var SelectAnimations = viewer.SelectAnimations

var animationIndex = viewer.AnimationIndex

var longestAnimation = viewer.LongestAnimation

var animFramesPerSecond = viewer.AnimFramesPerSecond

var animClipSeconds = viewer.AnimClipSeconds

const scrubBarThickness = viewer.ScrubBarThickness

const scrubBarMargin = viewer.ScrubBarMargin

const scrubLabelPx = viewer.ScrubLabelPx

const scrubLabelHeight = viewer.ScrubLabelHeight

var cEventFX = viewer.CEventFX

var cEventSnd = viewer.CEventSnd

var cKeyFrame = viewer.CKeyFrame

var cBoneKey = viewer.CBoneKey

type animScrubEvent = viewer.AnimScrubEvent

var fxClipEvents = viewer.FxClipEvents

var frameAt = viewer.FrameAt

var ClipScrubEvents = viewer.ClipScrubEvents

var scrubHit = viewer.ScrubHit

var ScrubGeometry = viewer.ScrubGeometry

var MovePickedBoneKeyToX = viewer.MovePickedBoneKeyToX

var ScrubSeek = viewer.ScrubSeek

var SeekFrame = viewer.SeekFrame

var scrubBarAt = viewer.ScrubBarAt

var frameX = viewer.FrameX

var clipKeyFrames = viewer.ClipKeyFrames

const scrubKeySnapPx = viewer.ScrubKeySnapPx

var snapKeyFrame = viewer.SnapKeyFrame

var absF32 = viewer.AbsF32

var ModelAnimScrubOverlay = viewer.ModelAnimScrubOverlay

var scrubFill = viewer.ScrubFill

var sgbCameraDragRequested = viewer.SgbCameraDragRequested

var ModelGizmoOverlay = viewer.ModelGizmoOverlay

const gizmoMargin = viewer.GizmoMargin

var boneGroupLog = viewer.BoneGroupLog

var unitDefaultAnim = viewer.UnitDefaultAnim

// fxScaleDefaultValue lives in viewer; the alias keeps the call sites.
const fxScaleDefaultValue = viewer.FxScaleDefaultValue
