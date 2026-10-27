package app

import "dow-mod-studio/internal/app/sgb"

// The scene editor lives in internal/app/sgb. These aliases keep every call
// site in app — and in the tests — reading exactly as it did before the move.

var sceneHasMesh = sgb.SceneHasMesh

var sgbSceneAppendEntity = sgb.SgbSceneAppendEntity

var sgbSceneBounds = sgb.SgbSceneBounds

var sgbRemoveEntities = sgb.SgbRemoveEntities

var sgbRotateEntities = sgb.SgbRotateEntities

var sgbMoveEntitiesOnSurface = sgb.SgbMoveEntitiesOnSurface

var sgbNextPlacementID = sgb.SgbNextPlacementID

var canonicalSgbBlueprintPath = sgb.CanonicalSgbBlueprintPath

var sgbPlacementWorldToEntity = sgb.SgbPlacementWorldToEntity

var sgbScreenPointToWorld = sgb.SgbScreenPointToWorld

var sgbPickTerrainPoint = sgb.SgbPickTerrainPoint

var sgbRayTriangle = sgb.SgbRayTriangle

var sgbEntityIdleAnimationName = sgb.SgbEntityIdleAnimationName

var sgbBlueprintIdleAnimationName = sgb.SgbBlueprintIdleAnimationName

const sgbEnvironmentRoot = sgb.SgbEnvironmentRoot

type sgbEnvironmentEntry = sgb.SgbEnvironmentEntry

type sgbPlacementRequest = sgb.SgbPlacementRequest

var sgbNewPlayerList = sgb.SgbNewPlayerList

type sgbObjectModelResult = sgb.SgbObjectModelResult

type sgbObjectMaterialRequest = sgb.SgbObjectMaterialRequest

type sgbObjectMaterialsResult = sgb.SgbObjectMaterialsResult

var sgbEnvironmentEntries = sgb.SgbEnvironmentEntries

var EnsureSgbObjectTree = sgb.EnsureSgbObjectTree

var PollSgbObjectModel = sgb.PollSgbObjectModel

var clearSgbObjectSelection = sgb.ClearSgbObjectSelection

type sgbPlayerOption = sgb.SgbPlayerOption

var sgbPlayerOptions = sgb.SgbPlayerOptions

var sgbPlayerOwner = sgb.SgbPlayerOwner

var LayoutSgbPlayerSelector = sgb.LayoutSgbPlayerSelector

var sgbPlayerRadioChildren = sgb.SgbPlayerRadioChildren

var SetSelectedSgbOwner = sgb.SetSelectedSgbOwner

var StartSgbObjectMaterials = sgb.StartSgbObjectMaterials

var QueueSgbObjectMaterials = sgb.QueueSgbObjectMaterials

var LaunchNextSgbObjectMaterials = sgb.LaunchNextSgbObjectMaterials

var PollSgbObjectMaterials = sgb.PollSgbObjectMaterials

var ApplySgbObjectMaterials = sgb.ApplySgbObjectMaterials

var DeleteSelectedSgbEntities = sgb.DeleteSelectedSgbEntities

var sgbSelectedPlayerOwner = sgb.SgbSelectedPlayerOwner

var AppendSgbPlacement = sgb.AppendSgbPlacement

const sgbCameraDistance = sgb.SgbCameraDistance

const sgbCameraMinDistance = sgb.SgbCameraMinDistance

const sgbCameraPitch = sgb.SgbCameraPitch

const sgbCameraYaw = sgb.SgbCameraYaw

var sgbCameraDefaultDistance = sgb.SgbCameraDefaultDistance

const sgbTerrainMaxGrid = sgb.SgbTerrainMaxGrid

var sgbTerrainMaterial = sgb.SgbTerrainMaterial

var sgbTerrainModel = sgb.SgbTerrainModel

var sgbTerrainHeightAt = sgb.SgbTerrainHeightAt

var sgbApproachCameraVelocity = sgb.SgbApproachCameraVelocity

var SgbMoveCamera = sgb.SgbMoveCamera

const sgbTerrainTextureMaxDim = sgb.SgbTerrainTextureMaxDim

const sgbTerrainTextureMaxPixels = sgb.SgbTerrainTextureMaxPixels

var sgbBuildTerrainTexture = sgb.SgbBuildTerrainTexture

var sgbBuildTileTexture = sgb.SgbBuildTileTexture

var sgbEntityMaterials = sgb.SgbEntityMaterials

var sgbBlendTiles = sgb.SgbBlendTiles

var sgbBakeDecals = sgb.SgbBakeDecals

var sgbDrawDecal = sgb.SgbDrawDecal

var sgbTreeTexture = sgb.SgbTreeTexture

var sgbSample = sgb.SgbSample

var sgbBlendPixel = sgb.SgbBlendPixel

const sgbOverTypes = sgb.SgbOverTypes

const sgbOverPass = sgb.SgbOverPass

const sgbOverDecals = sgb.SgbOverDecals

const sgbOverEntities = sgb.SgbOverEntities

var OpenSgb = sgb.OpenSgb

type sgbToggle = sgb.SgbToggle

var sgbToggles = sgb.SgbToggles

var SgbOverlayBar = sgb.SgbOverlayBar

var DrawSgbCanvas = sgb.DrawSgbCanvas

var buildSgbImage = sgb.BuildSgbImage

var sgbEntityMark = sgb.SgbEntityMark

var sgbWorldToCell = sgb.SgbWorldToCell

var sgbDecalRadius = sgb.SgbDecalRadius

var blendGrid = sgb.BlendGrid

var sgbClampByte = sgb.SgbClampByte

var paintSgbSelection = sgb.PaintSgbSelection

var paintSgbSelectionMarquee = sgb.PaintSgbSelectionMarquee

var sgbCanvasToFramePoint = sgb.SgbCanvasToFramePoint

var sgbPickEntity = sgb.SgbPickEntity

var sgbRectangleEntities = sgb.SgbRectangleEntities

var sgbTriangleIntersectsRect = sgb.SgbTriangleIntersectsRect

var sgbPointInTriangle = sgb.SgbPointInTriangle

var sgbSegmentsIntersect = sgb.SgbSegmentsIntersect

var sgbSelectionRect = sgb.SgbSelectionRect

var sgbRectPath = sgb.SgbRectPath
