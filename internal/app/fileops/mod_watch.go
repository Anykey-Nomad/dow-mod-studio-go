package fileops

import (
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/modtree"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"hash/fnv"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ModCatalogPollInterval = 30 * time.Second

func CurrentModFolderRoot(tree *modtree.ModTree) string {
	if tree == nil {
		return ""
	}
	modFolder := tree.ModFolder
	if modFolder == "" {
		modFolder = tree.FileMapName
	}
	if modFolder == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(tree.AppPath, modFolder))
}

func PathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// CurrentModSnapshot returns a compact fingerprint of the current mod's
// writable filesystem sources and declared archives. It intentionally does
// not walk SGA contents: archive changes are detected by their file stat and
// cause a full merged-tree rebuild.
type ModCatalogWatchSource struct {
	isSGA       bool
	kind        modtree.SourceKind
	archivePath string
	diskRoot    string
}

type ModCatalogWatch struct {
	appPath         string
	modFolder       string
	fileMapName     string
	modFilePath     string
	writableOverlay string
	sources         []ModCatalogWatchSource
}

func WatchFromTree(tree *modtree.ModTree) ModCatalogWatch {
	if tree == nil {
		return ModCatalogWatch{}
	}
	watch := ModCatalogWatch{
		appPath:         tree.AppPath,
		modFolder:       tree.ModFolder,
		fileMapName:     tree.FileMapName,
		modFilePath:     tree.ModFilePath,
		writableOverlay: tree.WritableOverlayRoot,
	}
	if watch.writableOverlay == "" {
		watch.writableOverlay, _ = tree.ResolveWritableRoot()
	}
	for _, source := range tree.Sources {
		if source == nil {
			continue
		}
		watch.sources = append(watch.sources, ModCatalogWatchSource{
			isSGA:       source.IsSGA,
			kind:        source.Kind,
			archivePath: source.ArchivePath,
			diskRoot:    source.DiskRoot,
		})
	}
	return watch
}

func (watch ModCatalogWatch) modFolderRoot() string {
	modFolder := watch.modFolder
	if modFolder == "" {
		modFolder = watch.fileMapName
	}
	if modFolder == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(watch.appPath, modFolder))
}

func (watch ModCatalogWatch) roots() []string {
	roots := make(map[string]bool)
	modRoot := watch.modFolderRoot()
	for _, source := range watch.sources {
		if !source.isSGA && source.kind == modtree.KindThisMod && source.diskRoot != "" && PathWithinRoot(source.diskRoot, modRoot) {
			roots[filepath.Clean(source.diskRoot)] = true
		}
	}
	if watch.writableOverlay != "" && PathWithinRoot(watch.writableOverlay, modRoot) {
		roots[filepath.Clean(watch.writableOverlay)] = true
	}
	orderedRoots := make([]string, 0, len(roots))
	for root := range roots {
		orderedRoots = append(orderedRoots, root)
	}
	sort.Strings(orderedRoots)
	return orderedRoots
}

func (watch ModCatalogWatch) snapshot() string {
	orderedRoots := watch.roots()

	h := fnv.New64a()
	for _, root := range orderedRoots {
		_, _ = h.Write([]byte("root|" + root + "\n"))
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			_, _ = fmt.Fprintf(h, "%s|%t|%d|%d\n", filepath.ToSlash(rel), entry.IsDir(), info.Size(), info.ModTime().UnixNano())
			return nil
		})
	}

	if watch.modFilePath != "" {
		if info, err := os.Stat(watch.modFilePath); err == nil {
			_, _ = fmt.Fprintf(h, "module|%d|%d\n", info.Size(), info.ModTime().UnixNano())
		}
		for _, source := range watch.sources {
			if !source.isSGA || source.kind != modtree.KindThisMod || source.archivePath == "" {
				continue
			}
			if info, err := os.Stat(source.archivePath); err == nil {
				_, _ = fmt.Fprintf(h, "archive|%s|%d|%d\n", filepath.ToSlash(source.archivePath), info.Size(), info.ModTime().UnixNano())
			} else {
				_, _ = fmt.Fprintf(h, "archive|%s|missing\n", filepath.ToSlash(source.archivePath))
			}
		}
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

func CurrentModSnapshot(tree *modtree.ModTree) string {
	return WatchFromTree(tree).snapshot()
}

func AddWatchTree(watcher *fsnotify.Watcher, root string) error {
	seen := make(map[string]bool)
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		path = filepath.Clean(path)
		if seen[path] {
			return nil
		}
		seen[path] = true
		return watcher.Add(path)
	})
}

func StartModFileWatcherWorker(tree *modtree.ModTree, signal chan struct{}) (*fsnotify.Watcher, error) {
	if tree == nil || signal == nil {
		return nil, nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watch := WatchFromTree(tree)
	for _, root := range watch.roots() {
		if err := AddWatchTree(watcher, root); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	}
	// The module file and current-mod SGA archives are outside the recursive
	// data roots, so watch their containing directories as well.
	for _, file := range append([]string{watch.modFilePath}, CurrentModArchivePaths(watch)...) {
		if file == "" {
			continue
		}
		if err := watcher.Add(filepath.Dir(file)); err != nil {
			continue
		}
	}
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = AddWatchTree(watcher, event.Name)
					}
				}
				select {
				case signal <- struct{}{}:
				default:
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return watcher, nil
}

func CurrentModArchivePaths(watch ModCatalogWatch) []string {
	var paths []string
	for _, source := range watch.sources {
		if source.isSGA && source.kind == modtree.KindThisMod && source.archivePath != "" {
			paths = append(paths, source.archivePath)
		}
	}
	return paths
}

type ModCatalogRefreshResult = uistate.ModCatalogRefreshResult

func StopModFileWatcher(ui *uistate.State) {
	if ui.ModFileWatcher != nil {
		_ = ui.ModFileWatcher.Close()
		ui.ModFileWatcher = nil
	}
}

func StartModFileWatcher(ui *uistate.State, tree *modtree.ModTree) {
	StopModFileWatcher(ui)
	if ui.ModFileWatchSignal == nil {
		ui.ModFileWatchSignal = make(chan struct{}, 1)
	}
	for {
		select {
		case <-ui.ModFileWatchSignal:
		default:
			goto drained
		}
	}
drained:
	watcher, err := StartModFileWatcherWorker(tree, ui.ModFileWatchSignal)
	if err != nil {
		log.Printf("file watcher unavailable, falling back to periodic refresh: %v", err)
		return
	}
	ui.ModFileWatcher = watcher
}

func PollModCatalog(ui *uistate.State) {
	if ui.ModTree == nil || ui.ModulePath == "" {
		return
	}
	if result, ok := PollModCatalogResult(ui); ok {
		ApplyModCatalogResult(ui, result)
	}
	if ui.ModRefreshRunning {
		return
	}
	if ui.ModFileWatchSignal != nil {
		select {
		case <-ui.ModFileWatchSignal:
			ui.LastModPoll = time.Time{}
		default:
		}
	}
	now := time.Now()
	if !ui.LastModPoll.IsZero() && now.Sub(ui.LastModPoll) < ModCatalogPollInterval {
		return
	}
	ui.LastModPoll = now

	baseTree := ui.ModTree
	if ui.ModRefreshCh == nil {
		ui.ModRefreshCh = make(chan ModCatalogRefreshResult, 1)
	}
	watch := WatchFromTree(baseTree)
	knownSnapshot := ui.ModSnapshot
	modulePath := ui.ModulePath
	refreshCh := ui.ModRefreshCh
	ui.ModRefreshRunning = true
	go func() {
		result := ModCatalogRefreshResult{Path: modulePath, BaseTree: baseTree}
		result.Snapshot = watch.snapshot()
		if result.Snapshot == "" || result.Snapshot == knownSnapshot {
			refreshCh <- result
			return
		}
		result.NewTree, result.Err = modtree.BuildModTree(result.Path, "English")
		if result.NewTree != nil {
			result.Snapshot = WatchFromTree(result.NewTree).snapshot()
		}
		refreshCh <- result
	}()
}

func PollModCatalogResult(ui *uistate.State) (ModCatalogRefreshResult, bool) {
	if ui.ModRefreshCh == nil {
		return ModCatalogRefreshResult{}, false
	}
	select {
	case result := <-ui.ModRefreshCh:
		ui.ModRefreshRunning = false
		return result, true
	default:
		return ModCatalogRefreshResult{}, false
	}
}

func ApplyModCatalogResult(ui *uistate.State, result ModCatalogRefreshResult) {
	if result.Path != ui.ModulePath || result.BaseTree != ui.ModTree {
		if result.NewTree != nil {
			result.NewTree.Close()
		}
		return
	}
	if result.Err != nil {
		return
	}
	if result.NewTree == nil {
		ui.ModSnapshot = result.Snapshot
		return
	}
	ui.ModSnapshot = result.Snapshot
	oldPosition := ui.LeftList.Position
	oldTree := ui.ModTree
	ui.ModTree = result.NewTree
	oldTree.Close()
	ui.LeftList.Position = oldPosition
	ui.VisibleNodes = nil
	ui.RowClicks = nil
	ui.StatusText = "Mod catalog updated"
}
