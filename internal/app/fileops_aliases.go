package app

import "dow-mod-studio/internal/app/fileops"

// the file-operation tools lives in internal/app/fileops. These aliases keep every call
// site in app — and in the tests — reading exactly as it did before the move.

var findFirstAssocArg = fileops.FindFirstAssocArg

var ReadTabFile = fileops.ReadTabFile

type sgaRow = fileops.SgaRow

var OpenSgaArchive = fileops.OpenSgaArchive

var sgaArchiveRows = fileops.SgaArchiveRows

var sgaWalkRows = fileops.SgaWalkRows

var sgaSizeLabel = fileops.SgaSizeLabel

var RefreshAssocStates = fileops.RefreshAssocStates

var ToggleAssociation = fileops.ToggleAssociation

var assocTypeFor = fileops.AssocTypeFor

type assocType = fileops.AssocType

var moduleAssoc = fileops.ModuleAssoc

var assocTypes = fileops.AssocTypes

var associationsSupported = fileops.AssociationsSupported

var isExtAssociated = fileops.IsExtAssociated

var registerExtAssociation = fileops.RegisterExtAssociation

var unregisterExtAssociation = fileops.UnregisterExtAssociation

var isModuleAssociated = fileops.IsModuleAssociated

var registerModuleAssociation = fileops.RegisterModuleAssociation

var unregisterModuleAssociation = fileops.UnregisterModuleAssociation

var getExePath = fileops.GetExePath

type moduleCandidate = fileops.ModuleCandidate

const steamDowFolder = fileops.SteamDowFolder

var discoverModuleFiles = fileops.DiscoverModuleFiles

var ModuleRoots = fileops.ModuleRoots

var ResolveModulePath = fileops.ResolveModulePath

var defaultTextFormats = fileops.DefaultTextFormats

const defaultTextFormatList = fileops.DefaultTextFormatList

var parseTextFormats = fileops.ParseTextFormats

var formatTextFormats = fileops.FormatTextFormats

var hasTextFormat = fileops.HasTextFormat

var IsExternalTextFile = fileops.IsExternalTextFile

var IsEditorFile = fileops.IsEditorFile

var TextFormats = fileops.TextFormats

var moveFileToRecycle = fileops.MoveFileToRecycle

var moveFileKeeping = fileops.MoveFileKeeping

var copyFileThenRemove = fileops.CopyFileThenRemove

var countFilesInDir = fileops.CountFilesInDir

var clearRecycleDir = fileops.ClearRecycleDir

var CanDeleteCurrentModFile = fileops.CanDeleteCurrentModFile

var ManagedFileDiskPath = fileops.ManagedFileDiskPath

var duplicateVirtualPath = fileops.DuplicateVirtualPath

var virtualParentPath = fileops.VirtualParentPath

const pickMaxRows = fileops.PickMaxRows

type pickConfig = fileops.PickConfig

type pickRow = fileops.PickRow

type filePicker = fileops.FilePicker

var pickExt = fileops.PickExt

var pickFilterOf = fileops.PickFilterOf

var cleanDialogPath = fileops.CleanDialogPath

var pathExists = fileops.PathExists

var FpStartList = fileops.FpStartList

var FpStartRows = fileops.FpStartRows

var FpRebuild = fileops.FpRebuild

var discoverFolderRows = fileops.DiscoverFolderRows

var listPickFiles = fileops.ListPickFiles

type freezeFile = fileops.FreezeFile

type freezeMention = fileops.FreezeMention

type freezeRecord = fileops.FreezeRecord

type freezeBook = fileops.FreezeBook

const freezeBookVersion = fileops.FreezeBookVersion

const dependenciesFileName = fileops.DependenciesFileName

var projectUnitsDir = fileops.ProjectUnitsDir

var freezeBookPath = fileops.FreezeBookPath

var readFreezeBook = fileops.ReadFreezeBook

var writeFreezeBook = fileops.WriteFreezeBook

var freezeUnitsDir = fileops.FreezeUnitsDir

var FreezeBookFor = fileops.FreezeBookFor

var ForgetFreezeBook = fileops.ForgetFreezeBook

var ebpsSquadPathFor = fileops.EbpsSquadPathFor

var ClearRgdField = fileops.ClearRgdField

var rgdEntryAtPath = fileops.RgdEntryAtPath

var setRgdString = fileops.SetRgdString

const projectsDirName = fileops.ProjectsDirName

const unitsDirName = fileops.UnitsDirName

var programDataDir = fileops.ProgramDataDir

var projectsDir = fileops.ProjectsDir

var sanitizeFolderName = fileops.SanitizeFolderName

var projectDir = fileops.ProjectDir

var modProjectName = fileops.ModProjectName

var ensureProjectDirs = fileops.EnsureProjectDirs

var LayoutFreezeButtons = fileops.LayoutFreezeButtons

var IsFrozen = fileops.IsFrozen

var CanFreeze = fileops.CanFreeze

var FrozenRows = fileops.FrozenRows

type markerDebugReport = fileops.MarkerDebugReport

type markerDebugMaterial = fileops.MarkerDebugMaterial

type markerDebugBounds = fileops.MarkerDebugBounds

type markerDebugItem = fileops.MarkerDebugItem

var markerDebugResolve = fileops.MarkerDebugResolve

var readModelMaterialAtAuthoredPath = fileops.ReadModelMaterialAtAuthoredPath

var vectorArray = fileops.VectorArray

const modCatalogPollInterval = fileops.ModCatalogPollInterval

var currentModFolderRoot = fileops.CurrentModFolderRoot

var pathWithinRoot = fileops.PathWithinRoot

type modCatalogWatchSource = fileops.ModCatalogWatchSource

type modCatalogWatch = fileops.ModCatalogWatch

var watchFromTree = fileops.WatchFromTree

var currentModSnapshot = fileops.CurrentModSnapshot

var addWatchTree = fileops.AddWatchTree

var startModFileWatcher = fileops.StartModFileWatcherWorker

var currentModArchivePaths = fileops.CurrentModArchivePaths

type modCatalogRefreshResult = fileops.ModCatalogRefreshResult

var StopModFileWatcher = fileops.StopModFileWatcher

var StartModFileWatcher = fileops.StartModFileWatcher

var PollModCatalog = fileops.PollModCatalog

var PollModCatalogResult = fileops.PollModCatalogResult

var ApplyModCatalogResult = fileops.ApplyModCatalogResult

type archiveOption = fileops.ArchiveOption

var OpenArchiveDialog = fileops.OpenArchiveDialog

var CurrentModFiles = fileops.CurrentModFiles

var PackCurrentModSGA = fileops.PackCurrentModSGA

var addModuleArchive = fileops.AddModuleArchive

var ExtractSelectedArchives = fileops.ExtractSelectedArchives

var ExtractSGADir = fileops.ExtractSGADir

var HandleArchiveDialogInput = fileops.HandleArchiveDialogInput

var colorWhite = fileops.ColorWhite

var writeSGA2 = fileops.WriteSGA2
