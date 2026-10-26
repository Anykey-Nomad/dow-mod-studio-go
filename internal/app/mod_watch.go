package app

import (
	"dow-mod-studio/internal/modtree"
	"fmt"
	"log"

	"github.com/fsnotify/fsnotify"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const modCatalogPollInterval = 30 * time.Second

func currentModFolderRoot(tree *modtree.ModTree) string {
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

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// currentModSnapshot returns a compact fingerprint of the current mod's
// writable filesystem sources and declared archives. It intentionally does
// not walk SGA contents: archive changes are detected by their file stat and
// cause a full merged-tree rebuild.
type modCatalogWatchSource struct {
	isSGA       bool
	kind        modtree.SourceKind
	archivePath string
	diskRoot    string
}

type modCatalogWatch struct {
	appPath         string
	modFolder       string
	fileMapName     string
	modFilePath     string
	writableOverlay string
	sources         []modCatalogWatchSource
}

func watchFromTree(tree *modtree.ModTree) modCatalogWatch {
	if tree == nil {
		return modCatalogWatch{}
	}
	watch := modCatalogWatch{
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
		watch.sources = append(watch.sources, modCatalogWatchSource{
			isSGA:       source.IsSGA,
			kind:        source.Kind,
			archivePath: source.ArchivePath,
			diskRoot:    source.DiskRoot,
		})
	}
	return watch
}

func (watch modCatalogWatch) modFolderRoot() string {
	modFolder := watch.modFolder
	if modFolder == "" {
		modFolder = watch.fileMapName
	}
	if modFolder == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(watch.appPath, modFolder))
}

func (watch modCatalogWatch) roots() []string {
	roots := make(map[string]bool)
	modRoot := watch.modFolderRoot()
	for _, source := range watch.sources {
		if !source.isSGA && source.kind == modtree.KindThisMod && source.diskRoot != "" && pathWithinRoot(source.diskRoot, modRoot) {
			roots[filepath.Clean(source.diskRoot)] = true
		}
	}
	if watch.writableOverlay != "" && pathWithinRoot(watch.writableOverlay, modRoot) {
		roots[filepath.Clean(watch.writableOverlay)] = true
	}
	orderedRoots := make([]string, 0, len(roots))
	for root := range roots {
		orderedRoots = append(orderedRoots, root)
	}
	sort.Strings(orderedRoots)
	return orderedRoots
}

func (watch modCatalogWatch) snapshot() string {
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

func currentModSnapshot(tree *modtree.ModTree) string {
	return watchFromTree(tree).snapshot()
}

func addWatchTree(watcher *fsnotify.Watcher, root string) error {
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

func startModFileWatcher(tree *modtree.ModTree, signal chan struct{}) (*fsnotify.Watcher, error) {
	if tree == nil || signal == nil {
		return nil, nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	watch := watchFromTree(tree)
	for _, root := range watch.roots() {
		if err := addWatchTree(watcher, root); err != nil {
			_ = watcher.Close()
			return nil, err
		}
	}
	// The module file and current-mod SGA archives are outside the recursive
	// data roots, so watch their containing directories as well.
	for _, file := range append([]string{watch.modFilePath}, currentModArchivePaths(watch)...) {
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
						_ = addWatchTree(watcher, event.Name)
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

func currentModArchivePaths(watch modCatalogWatch) []string {
	var paths []string
	for _, source := range watch.sources {
		if source.isSGA && source.kind == modtree.KindThisMod && source.archivePath != "" {
			paths = append(paths, source.archivePath)
		}
	}
	return paths
}

type modCatalogRefreshResult struct {
	path     string
	baseTree *modtree.ModTree
	snapshot string
	newTree  *modtree.ModTree
	err      error
}

func StopModFileWatcher(ui *uiState) {
	if ui.ModFileWatcher != nil {
		_ = ui.ModFileWatcher.Close()
		ui.ModFileWatcher = nil
	}
}

func StartModFileWatcher(ui *uiState, tree *modtree.ModTree) {
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
	watcher, err := startModFileWatcher(tree, ui.ModFileWatchSignal)
	if err != nil {
		log.Printf("file watcher unavailable, falling back to periodic refresh: %v", err)
		return
	}
	ui.ModFileWatcher = watcher
}

func PollModCatalog(ui *uiState) {
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
	if !ui.LastModPoll.IsZero() && now.Sub(ui.LastModPoll) < modCatalogPollInterval {
		return
	}
	ui.LastModPoll = now

	baseTree := ui.ModTree
	if ui.ModRefreshCh == nil {
		ui.ModRefreshCh = make(chan modCatalogRefreshResult, 1)
	}
	watch := watchFromTree(baseTree)
	knownSnapshot := ui.ModSnapshot
	modulePath := ui.ModulePath
	refreshCh := ui.ModRefreshCh
	ui.ModRefreshRunning = true
	go func() {
		result := modCatalogRefreshResult{path: modulePath, baseTree: baseTree}
		result.snapshot = watch.snapshot()
		if result.snapshot == "" || result.snapshot == knownSnapshot {
			refreshCh <- result
			return
		}
		result.newTree, result.err = modtree.BuildModTree(result.path, "English")
		if result.newTree != nil {
			result.snapshot = watchFromTree(result.newTree).snapshot()
		}
		refreshCh <- result
	}()
}

func PollModCatalogResult(ui *uiState) (modCatalogRefreshResult, bool) {
	if ui.ModRefreshCh == nil {
		return modCatalogRefreshResult{}, false
	}
	select {
	case result := <-ui.ModRefreshCh:
		ui.ModRefreshRunning = false
		return result, true
	default:
		return modCatalogRefreshResult{}, false
	}
}

func ApplyModCatalogResult(ui *uiState, result modCatalogRefreshResult) {
	if result.path != ui.ModulePath || result.baseTree != ui.ModTree {
		if result.newTree != nil {
			result.newTree.Close()
		}
		return
	}
	if result.err != nil {
		return
	}
	if result.newTree == nil {
		ui.ModSnapshot = result.snapshot
		return
	}
	ui.ModSnapshot = result.snapshot
	oldPosition := ui.LeftList.Position
	oldTree := ui.ModTree
	ui.ModTree = result.newTree
	oldTree.Close()
	ui.LeftList.Position = oldPosition
	ui.VisibleNodes = nil
	ui.RowClicks = nil
	ui.StatusText = "Mod catalog updated"
}
