// Package modtree models the contents of a Dawn of War module: it parses the
// .module descriptor, builds the tree of folders and files from the game data,// the required mods and the local overlay, and answers searches over it.
package modtree

import (
	"dow-mod-studio/internal/sga"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// ── Virtual file tree (merged view) ───────────────────────────────────────────
// This mirrors CDoWFileView / CModuleFile / CFileMap merging logic:
// - one merged tree (single ToC "Data", root = Data)
// - sources are ordered: this mod (0), required mods (15000+N), engine (30001)
// - first registration wins for duplicate file (higher priority keeps file)
//   but folders are merged.
// We intentionally simplify: one ToC "Data" only, no multi-ToC.
// Coloring is done via file's SourceKind/ModName, similar to frmFiles::CFilesTreeItemData tags.

type SourceKind int

const (
	KindThisMod SourceKind = 0
	KindReqMod  SourceKind = 1
	KindEngine  SourceKind = 2
)

type FileSource struct {
	ModName     string // e.g. "DXP3" / "(Engine)" / required mod name
	SourceName  string // e.g. "Data", "W40kData.sga"
	Kind        SourceKind
	Priority    int // lower = higher priority (0=this, 15000+id, 30001=engine)
	IsSGA       bool
	SGAHandle   *sga.SGAFile // nil for FS sources; caller keeps handles open
	ArchivePath string       // absolute SGA path when IsSGA
	DiskRoot    string       // for FS sources: absolute path on disk that maps to virtual "Data"
}

type TreeFile struct {
	Name     string
	FullPath string // virtual path like "Data\attrib\ebps\..."
	Source   *FileSource
	// physical location (first registration wins)
	DiskPath  string // absolute FS path if !Source.IsSGA
	SGAFileID int    // valid if Source.IsSGA (index inside SGA)
}

type TreeFolder struct {
	Name     string // short, e.g. "attrib"
	FullPath string // e.g. "Data" or "Data\attrib"
	Parent   *TreeFolder
	Children []*TreeFolder
	Files    []*TreeFile

	// for quick lookup during building (case-insensitive)
	folderByLower map[string]*TreeFolder
	FileByLower   map[string]*TreeFile
}

func NewFolder(name, fullPath string, parent *TreeFolder) *TreeFolder {
	return &TreeFolder{
		Name:          name,
		FullPath:      fullPath,
		Parent:        parent,
		folderByLower: make(map[string]*TreeFolder),
		FileByLower:   make(map[string]*TreeFile),
	}
}

type ModTree struct {
	Root                *TreeFolder
	FileMapName         string // top mod name
	Locale              string
	AppPath             string
	Sources             []*FileSource
	ModFilePath         string
	ModFolder           string
	Warnings            []string
	WritableOverlayRoot string
	sgaHandles          []*sga.SGAFile // kept to Close later
	// pending are the sources found but not yet walked. Discovery is cheap
	// and must stay in order, because the order sources are added is the
	// order they win in; walking them is not cheap and is done all at once
	// at the end. See buildPending.
	pending []pendingSource

	// byNameMu guards the lazily built byName and preferred indexes below.
	byNameMu sync.Mutex
	// byName indexes files by their lower cased base name. A .whm material is
	// often just "necron_lord01" with no folder, so a path lookup misses it
	// even though the archive holds the texture. Built on first use.
	byName    map[string]*TreeFile
	preferred map[string]*TreeFile
	// materialCache remembers whether any loaded source holds the named game
	// terrain map, independently of its virtual folder. Ground lookup may use
	// this while source priorities are still represented by the merged tree.
	materialCache map[string]*TreeFile
}

// FindByName resolves a file from its base name alone. Several folders can hold
// a file of the same name, and for a texture the one in a "texture_share"
// folder is the intended one, so that is preferred over the first hit.
func (t *ModTree) FindByName(fileName string) *TreeFile {
	if t == nil || t.Root == nil || fileName == "" {
		return nil
	}
	// The index is built on first use, and two callers arriving together used
	// to both build it: one writing the map the other was writing. That is a
	// "concurrent map writes" crash rather than a wrong answer, and it shows up
	// as soon as anything reads by bare name from more than one goroutine —
	// which the preview's sound fills do, one per sound folder. The index is
	// built once under the lock and every caller waits for that one build.
	t.byNameMu.Lock()
	defer t.byNameMu.Unlock()
	if t.byName == nil {
		t.byName = make(map[string]*TreeFile)
		var walk func(f *TreeFolder, inTextures bool)
		walk = func(f *TreeFolder, inTextures bool) {
			inTextures = inTextures || strings.EqualFold(f.Name, "texture_share")
			for _, file := range f.Files {
				lower := strings.ToLower(file.Name)
				if _, seen := t.byName[lower]; seen {
					continue
				}
				t.byName[lower] = file
			}
			for _, child := range f.Children {
				walk(child, inTextures)
			}
		}
		// A second pass prefers the texture folders, which is where the game
		// keeps the maps a model refers to by bare name.
		walk(t.Root, false)
		var prefer func(f *TreeFolder, inTextures bool)
		prefer = func(f *TreeFolder, inTextures bool) {
			inTextures = inTextures || strings.EqualFold(f.Name, "texture_share")
			for _, file := range f.Files {
				lower := strings.ToLower(file.Name)
				if !inTextures {
					continue
				}
				if prev, seen := t.preferred[lower]; !seen || prev == nil {
					t.preferred[lower] = file
				}
			}
			for _, child := range f.Children {
				prefer(child, inTextures)
			}
		}
		t.preferred = make(map[string]*TreeFile)
		prefer(t.Root, false)
		for lower, file := range t.preferred {
			t.byName[lower] = file
		}
	}
	return t.byName[strings.ToLower(fileName)]
}

// FindMaterialByName finds a file by base name, preferring texture_share files
// and then the source with the highest module priority. It is useful for game
// assets whose virtual paths vary between modules but whose filenames are stable.
func (t *ModTree) FindMaterialByName(fileName string) *TreeFile {
	if t == nil || t.Root == nil || fileName == "" {
		return nil
	}
	want := strings.ToLower(fileName)
	var best *TreeFile
	bestTexture := false
	var walk func(*TreeFolder, bool)
	walk = func(folder *TreeFolder, inTextures bool) {
		inTextures = inTextures || strings.EqualFold(folder.Name, "texture_share")
		for _, file := range folder.Files {
			if !strings.EqualFold(file.Name, want) || file.Source == nil {
				continue
			}
			if best == nil || (inTextures && !bestTexture) ||
				(inTextures == bestTexture && file.Source.Priority < best.Source.Priority) {
				best, bestTexture = file, inTextures
			}
		}
		for _, child := range folder.Children {
			walk(child, inTextures)
		}
	}
	walk(t.Root, false)
	return best
}

func (t *ModTree) Close() {
	for _, s := range t.sgaHandles {
		if s != nil {
			_ = s.Close()
		}
	}
}

// BuildModTree is the main entry point: given a .module path and locale,
// parse, recursively load required mods, engine, data folders, SGAs, and build merged tree.
// moduleRelPath converts a relative path the way .module files write it —
// separators are backslashes, whatever the OS reading them — into an OS path.
// On Linux a backslash is an ordinary file name character, so joining
// "Locale\\English\\Data" untouched stats one directory of that literal name
// and the folder it names is silently never found.
func moduleRelPath(rel string) string {
	return filepath.FromSlash(strings.ReplaceAll(rel, "\\", "/"))
}

func BuildModTree(modulePath, locale string) (*ModTree, error) {
	if locale == "" {
		locale = "English"
	}
	// normalize modulePath
	absMod, err := filepath.Abs(modulePath)
	if err != nil {
		absMod = modulePath
	}
	mf, err := ParseModule(absMod)
	if err != nil {
		return nil, fmt.Errorf("parse module: %w", err)
	}
	if mf.ModFolder == "" {
		mf.ModFolder = mf.FileMapName
	}
	tree := &ModTree{
		Root:        NewFolder("Data", "Data", nil),
		FileMapName: mf.FileMapName,
		Locale:      locale,
		AppPath:     mf.AppPath,
		ModFilePath: absMod,
		ModFolder:   mf.ModFolder,
	}

	// Keep global visited-required to avoid cycles (key = AppPath + FileMapName)
	visited := make(map[string]bool)
	visited[strings.ToLower(absMod)] = true

	// 1) Load this mod: data folders + archives in order specified
	if err := tree.loadDoWMod(mf, KindThisMod, 0, locale); err != nil {
		return nil, err
	}

	// 2) Required mods (depth-first, as Corsix does: load required mod file then reload its resources recursively)
	//    Priority: 15000 + RequiredMod.N  (original CModuleFile uses 15000+N)
	reqKeys := sortedKeysInt(mf.RequiredMods)
	for _, k := range reqKeys {
		name := mf.RequiredMods[k]
		priorityBase := 15000 + k
		if err := tree.loadRequiredMod(mf.AppPath, name, priorityBase, locale, visited); err != nil {
			// Non-critical: log warning but continue (original swallows with catch)
			tree.Warnings = append(tree.Warnings, fmt.Sprintf("required mod %s: %v", name, err))
		}
	}

	// 3) Engine: folder Engine\Data and Engine.sga (priority 30001), mirroring CModuleFile::ReloadResources
	appPath := mf.AppPath
	// Engine\Data folder
	engDataPath := filepath.Join(appPath, "Engine", "Data")
	tree.tryAddFSFolder(engDataPath, "(Engine)", "Data", KindEngine, 30001, locale)
	// Engine.sga
	engSgaPath := filepath.Join(appPath, "Engine", "Engine.sga")
	if _, err := os.Stat(engSgaPath); err == nil {
		tree.tryAddSGA(engSgaPath, "(Engine)", "Engine.sga", KindEngine, 30001, locale)
	}

	// 4) DataGeneric (pipeline.ini) — last, to avoid conflicts
	tree.tryLoadDataGeneric(mf, locale, appPath)

	// Every source has now been found. Walk them all, then merge.
	tree.buildPending()

	// Sort recursively (case-insensitive, as Corsix does with _SortFolds/_SortFiles)
	tree.SortAll(tree.Root)

	return tree, nil
}

func (t *ModTree) tryLoadDataGeneric(mf *ModFile, locale, appPath string) {
	// pipeline.ini handling: check Engine\pipeline.ini? Actually original checks <AppPath>\pipeline.ini and looks for [project:<ModFolder>] DataGeneric=...
	pipelineCandidates := []string{
		filepath.Join(appPath, "pipeline.ini"),
		filepath.Join(appPath, "Engine", "pipeline.ini"),
	}
	for _, pip := range pipelineCandidates {
		if _, err := os.Stat(pip); err != nil {
			continue
		}
		m, err := ParsePipelineIni(pip)
		if err != nil {
			continue
		}
		key := "project:" + mf.ModFolder
		if v, ok := m[key]["DataGeneric"]; ok && v != "" {
			full := filepath.Join(appPath, moduleRelPath(v))
			t.tryAddFSFolder(full, mf.FileMapName, "DataGeneric", KindThisMod, 99, locale)
			// Also for required mods and engine
			reqKeys := sortedKeysInt(mf.RequiredMods)
			for _, k := range reqKeys {
				rName := mf.RequiredMods[k]
				if vr, ok := m["project:"+rName]["DataGeneric"]; ok && vr != "" {
					full2 := filepath.Join(appPath, moduleRelPath(vr))
					t.tryAddFSFolder(full2, rName, "DataGeneric", KindReqMod, 15000+k, locale)
				}
			}
			if ve, ok := m["project:Engine"]["DataGeneric"]; ok && ve != "" {
				full3 := filepath.Join(appPath, moduleRelPath(ve))
				t.tryAddFSFolder(full3, "(Engine)", "DataGeneric", KindEngine, 30001, locale)
			}
			return
		}
	}
	// fallback default: ModTools\DataGeneric\<ModFolder>
	fallback := filepath.Join(appPath, "ModTools", "DataGeneric", mf.ModFolder)
	t.tryAddFSFolder(fallback, mf.FileMapName, "DataGeneric", KindThisMod, 99, locale)
}

func ParsePipelineIni(path string) (map[string]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// very small INI parser (just enough for pipeline.ini)
	out := make(map[string]map[string]string)
	cur := ""
	buf := make([]byte, 0, 4096)
	// use scanner line by line
	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		// trim \r
		l = strings.TrimRight(l, "\r")
		// strip comment
		if idx := strings.Index(l, ";"); idx >= 0 {
			l = l[:idx]
		}
		trim := strings.TrimSpace(l)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			sec := strings.TrimSpace(trim[1 : len(trim)-1])
			cur = sec
			if _, ok := out[cur]; !ok {
				out[cur] = make(map[string]string)
			}
			continue
		}
		eq := strings.Index(l, "=")
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(l[:eq])
		v := strings.TrimSpace(l[eq+1:])
		if cur == "" {
			cur = "global"
			if _, ok := out[cur]; !ok {
				out[cur] = make(map[string]string)
			}
		}
		out[cur][k] = v
		_ = buf
	}
	return out, nil
}

func (t *ModTree) loadDoWMod(mf *ModFile, kind SourceKind, priorityBase int, locale string) error {
	appPath := mf.AppPath
	modFolderPath := filepath.Join(appPath, moduleRelPath(mf.ModFolder))

	// Heuristic for default write folder: prefer "Data" without dynamics.
	// We don't need to track output folder for the tree, just need to register all sources.
	// But we track which DataFolder is "Data" to mimic default write handling (not needed to display).

	// Load DataFolders in key order (1..N).
	keys := sortedKeysInt(mf.DataFolders)
	for _, k := range keys {
		raw := mf.DataFolders[k]
		resolved := moduleRelPath(ResolveDynamics(raw, locale, ""))
		fullPath := filepath.Join(modFolderPath, resolved)
		// dir name reported as raw? original uses raw DataFolder string as SourceName
		srcName := raw
		priority := priorityBase
		if kind == KindThisMod {
			priority = k // small priority for this mod's own folders (preserve order)
		} else if kind == KindReqMod {
			priority = priorityBase + k
		} else {
			priority = priorityBase
		}
		// Actually for this mod we want priority 0,1,2... but must keep engine lower priority.
		// So map: this mod 0..999, required 15000+, engine 30001
		if kind == KindThisMod {
			priority = k // 1..N: keeps file precedence as per .module order (lower N higher prio? Actually earlier = scanned first = higher prio in AddFileSource first win)
			// But first-registration-wins, so lower priority value should be registered first.
			// We'll register in key order; first registered wins, so order is correct.
		}
		t.tryAddFSFolder(fullPath, mf.FileMapName, srcName, kind, priority, locale)
	}

	// Load Archives in key order.
	aKeys := sortedKeysInt(mf.Archives)
	for _, k := range aKeys {
		raw := mf.Archives[k]
		// DoW: append ".sga" unless already there, per CModuleFile parsing it already added suffix.
		// Our stored raw may be without suffix; resolve dynamics then ensure suffix.
		suffix := ""
		if !strings.HasSuffix(strings.ToLower(raw), ".sga") {
			suffix = ".sga"
		}
		resolved := moduleRelPath(ResolveDynamics(raw, locale, suffix))
		fullPath := filepath.Join(modFolderPath, resolved)
		srcName := raw
		if !strings.HasSuffix(strings.ToLower(srcName), ".sga") {
			srcName += ".sga"
		}
		priority := k
		if kind == KindThisMod {
			priority = 1000 + k // archives after folders but still higher priority than required mods
		} else if kind == KindReqMod {
			priority = priorityBase + 1000 + k
		} else {
			priority = priorityBase
		}
		t.tryAddSGA(fullPath, mf.FileMapName, srcName, kind, priority, locale)
	}
	return nil
}

func (t *ModTree) loadRequiredMod(appPath, reqName string, priorityBase int, locale string, visited map[string]bool) error {
	modFile := filepath.Join(appPath, reqName+".module")
	lowerKey := strings.ToLower(modFile)
	if visited[lowerKey] {
		return nil // cycle already loaded
	}
	visited[lowerKey] = true
	mf, err := ParseModule(modFile)
	if err != nil {
		return err
	}
	if mf.ModFolder == "" {
		mf.ModFolder = mf.FileMapName
	}
	// Required mods are relative to AppPath; their own AppPath = directory of required .module (which is same dir)
	// but load with given priority base.
	if err := t.loadDoWMod(mf, KindReqMod, priorityBase, locale); err != nil {
		return err
	}
	// Recurse into its required mods
	reqKeys := sortedKeysInt(mf.RequiredMods)
	for _, k := range reqKeys {
		sub := mf.RequiredMods[k]
		subPrio := priorityBase + k // keep increasing? original: 15000+N for parent; child's required are 15000+childN but shared FileView still ordered by mod number?
		// We keep simple: 15000 + k for each level (child's own number).
		// To avoid huge offset, use 15000 + k for direct.
		_ = subPrio
		if err := t.loadRequiredMod(appPath, sub, 15000+k, locale, visited); err != nil {
			t.Warnings = append(t.Warnings, fmt.Sprintf("required mod %s (of %s): %v", sub, reqName, err))
		}
	}
	// Required mods also may have DataGeneric etc. — tryLoad below will handle parent-level only; for required mods we also load their DataGeneric
	t.tryLoadDataGenericForReq(mf, locale, appPath, priorityBase)
	return nil
}

func (t *ModTree) tryLoadDataGenericForReq(mf *ModFile, locale, appPath string, priorityBase int) {
	pipelineCandidates := []string{
		filepath.Join(appPath, "pipeline.ini"),
	}
	for _, pip := range pipelineCandidates {
		if _, err := os.Stat(pip); err != nil {
			continue
		}
		m, err := ParsePipelineIni(pip)
		if err != nil {
			continue
		}
		key := "project:" + mf.ModFolder
		if v, ok := m[key]["DataGeneric"]; ok && v != "" {
			full := filepath.Join(appPath, moduleRelPath(v))
			t.tryAddFSFolder(full, mf.FileMapName, "DataGeneric", KindReqMod, priorityBase, locale)
			return
		}
	}
	fallback := filepath.Join(appPath, "ModTools", "DataGeneric", mf.ModFolder)
	t.tryAddFSFolder(fallback, mf.FileMapName, "DataGeneric", KindReqMod, priorityBase, locale)
}

func (t *ModTree) tryAddFSFolder(fullPath, modName, sourceName string, kind SourceKind, priority int, locale string) {
	// Register the exact spelling the .module asked for and every sibling
	// directory that differs from it only in case. Windows folds directory
	// names, so a mod that wrote both "Data" and "data" has one folder there;
	// on a case-sensitive filesystem they are two folders holding a piece of
	// the mod each, and registering only the exact spelling shows whichever
	// half happened to match — for UA_DE that hid every unit the mod adds.
	var seen []os.FileInfo
	for _, candidate := range fsFolderVariants(fullPath) {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		dup := false
		for _, prev := range seen {
			if os.SameFile(prev, info) {
				dup = true
				break
			}
		}
		if dup {
			// The same directory under another spelling: a Windows
			// filesystem answers for both, and it must not be scanned twice.
			continue
		}
		seen = append(seen, info)
		src := &FileSource{
			ModName:    modName,
			SourceName: sourceName,
			Kind:       kind,
			Priority:   priority,
			IsSGA:      false,
			DiskRoot:   candidate,
		}
		t.Sources = append(t.Sources, src)
		t.pending = append(t.pending, pendingSource{kind: pendingFS, diskPath: candidate, src: src})
	}
}

// caseResolvedPath walks path one segment at a time and answers with the
// spelling the filesystem actually holds, matching every segment
// case-insensitively.
//
// A .module is written the way Windows reads it, where directory names fold;
// on a case-sensitive filesystem the same spelling is a different path, and
// a mod whose folder was installed under another case — or whose files were
// written under a lowercase data beside the Data the .module declares — is
// simply not there. A segment with no match keeps the spelling it was given,
// so a path about to be created is returned unchanged.
func caseResolvedPath(path string) string {
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	if parent == clean {
		// The root of the path: "/", a volume root, or ".".
		return clean
	}
	resolvedParent := caseResolvedPath(parent)
	entries, err := os.ReadDir(resolvedParent)
	if err != nil {
		return filepath.Join(resolvedParent, filepath.Base(clean))
	}
	base := filepath.Base(clean)
	var folded string
	for _, e := range entries {
		name := e.Name()
		if name == base {
			// The exact spelling wins, so a directory that exists under
			// both spellings is always picked the same way.
			return filepath.Join(resolvedParent, name)
		}
		if folded == "" && strings.EqualFold(name, base) {
			folded = name
		}
	}
	if folded != "" {
		return filepath.Join(resolvedParent, folded)
	}
	return filepath.Join(resolvedParent, base)
}

// fsFolderVariants lists fullPath and any sibling directory whose name matches
// its base name case-insensitively, the exact spelling first.
//
// The parent is resolved first, because that is where the mod's own folder
// sits: a .module that names UltimateApocalypse_THB while the folder was
// installed as ultimateapocalypse_thb used to leave ReadDir on a parent that
// was not there, so the mod contributed nothing at all to the tree and only
// the required mods showed.
func fsFolderVariants(fullPath string) []string {
	dir, base := filepath.Dir(fullPath), filepath.Base(fullPath)
	dir = caseResolvedPath(dir)
	out := []string{filepath.Join(dir, base)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if name == base || !strings.EqualFold(name, base) {
			continue
		}
		if !e.IsDir() {
			// A symlink to a directory still counts as one.
			if info, err := os.Stat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
				continue
			}
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func (t *ModTree) tryAddSGA(fullPath, modName, sourceName string, kind SourceKind, priority int, locale string) {
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return
	}
	sga, err := sga.OpenSGA(fullPath)
	if err != nil {
		t.Warnings = append(t.Warnings, fmt.Sprintf("open SGA %s: %v", fullPath, err))
		return
	}
	t.sgaHandles = append(t.sgaHandles, sga)
	src := &FileSource{
		ModName:     modName,
		SourceName:  sourceName,
		Kind:        kind,
		Priority:    priority,
		IsSGA:       true,
		SGAHandle:   sga,
		ArchivePath: fullPath,
	}
	t.Sources = append(t.Sources, src)
	t.pending = append(t.pending, pendingSource{kind: pendingSGA, diskPath: fullPath, src: src})
}

// ── Merge helpers (like CDoWFileView::_RawMapFolder and CFileMap::_RawMap) ──────────

// pendingKind says which of the two shapes a source has.
type pendingKind int

const (
	pendingFS pendingKind = iota
	pendingSGA
)

// pendingSource is a source that has been found but not yet walked.
type pendingSource struct {
	kind     pendingKind
	diskPath string
	src      *FileSource
}

// buildPending walks every source found so far and merges the results into the
// tree, in the order the sources were added.
//
// The two halves are split because they have opposite shapes. Finding a source
// is a handful of stats and has to stay in order: a mod's own folder wins over
// the engine's, and the order it is found in is the order it wins in.
// Walking one is thousands of directory reads or a whole archive's file table,
// and it touches nothing but its own source — so the walks run at the same
// time as each other, each into a tree of its own, and only the merging is
// sequential.
//
// That is most of the wall clock of opening a module. Measured on DXP3 with
// the base game installed: thirty-seven sources, thirty-four of them archives,
// and the directory walk alone was three fifths of the time.
func (t *ModTree) buildPending() {
	if len(t.pending) == 0 {
		return
	}
	roots := make([]*TreeFolder, len(t.pending))
	// One source per worker, so a source that takes a long time cannot hold
	// up the others behind it in a queue.
	workers := runtime.NumCPU()
	if workers > len(t.pending) {
		workers = len(t.pending)
	}
	var wg sync.WaitGroup
	var next int64
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(atomic.AddInt64(&next, 1)) - 1
				if i >= len(t.pending) {
					return
				}
				p := t.pending[i]
				// Every source is built under its own root named "Data",
				// which is the same virtual prefix the shared root has,
				// so a path built here is the path it would have had.
				root := NewFolder("Data", "Data", nil)
				switch p.kind {
				case pendingSGA:
					t.mergeSGA(p.src.SGAHandle, root, p.src)
				default:
					t.mergeFSFolder(p.diskPath, root, p.src)
				}
				roots[i] = root
			}
		}()
	}
	wg.Wait()
	for i, root := range roots {
		if root != nil {
			t.mergeSubtree(t.Root, root, t.pending[i].src)
		}
	}
	t.pending = nil
}

// mergeSubtree merges one already-built source tree into the tree of
// everything merged so far. It is where a source that overlaps another loses:
// a folder or a file already present is kept, and only what the source adds is
// taken, which is the same rule the old one-source-at-a-time merge applied by
// never looking at a source whose folder was already there.
func (t *ModTree) mergeSubtree(dst, srcFolder *TreeFolder, src *FileSource) {
	for _, file := range srcFolder.Files {
		t.addFileEntry(dst, file, src)
	}
	for _, child := range srcFolder.Children {
		lower := strings.ToLower(child.Name)
		target, ok := dst.folderByLower[lower]
		if !ok {
			target = NewFolder(child.Name, child.FullPath, dst)
			dst.Children = append(dst.Children, target)
			dst.folderByLower[lower] = target
		}
		t.mergeSubtree(target, child, src)
	}
}

func (t *ModTree) mergeFSFolder(diskPath string, dst *TreeFolder, src *FileSource) {
	entries, err := os.ReadDir(diskPath)
	if err != nil {
		return
	}
	// Build sorted slices for deterministic order (case-insensitive sort like _SortFolds/_SortFiles)
	sort.Slice(entries, func(a, b int) bool {
		la := strings.ToLower(entries[a].Name())
		lb := strings.ToLower(entries[b].Name())
		if la == lb {
			return entries[a].Name() < entries[b].Name()
		}
		return la < lb
	})
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			subDisk := filepath.Join(diskPath, name)
			subVirtual := t.GetOrCreateFolder(dst, name, src)
			t.mergeFSFolder(subDisk, subVirtual, src)
		} else {
			t.addFile(dst, name, src)
		}
	}
}

func (t *ModTree) mergeSGA(sga *sga.SGAFile, root *TreeFolder, src *FileSource) {
	// Mirror CModuleFile::_DoLoadArchive which maps entryPoint(0) = "Data" (or ToC 0) via MapSga.
	// For simplicity we merge every ToC separately, but remap ToC name "Data" only (others also map as-is under root).
	// The merged tree in Corsix's FileMap has separate ToC roots; but CDoWFileView always maps via VIterate entry point
	// which for SGAs is the ToC path (e.g. "Data"). Files from ToC "Data" map as "Data\attrib\..." under root's child "Data"?
	// However our root already IS "Data", so we merge directly.
	// For aliases other than "Data", we create a folder with that alias under root (rare).
	for ti := 0; ti < sga.ToCCount(); ti++ {
		tocName := sga.ToCName(ti)
		var dstRoot *TreeFolder
		if strings.EqualFold(tocName, "Data") {
			dstRoot = root
		} else {
			// Other ToCs (rare) become subfolder of root? original FileMap would have separate _TOC entry.
			// For display, create folder named tocName under root.
			dstRoot = t.GetOrCreateFolder(root, tocName, src)
		}
		dirID, err := sga.RootDir(ti)
		if err != nil {
			continue
		}
		t.mergeSGADir(sga, dirID, dstRoot, src)
	}
}

func (t *ModTree) mergeSGADir(sga *sga.SGAFile, dirID int, dst *TreeFolder, src *FileSource) {
	entries, err := sga.List(dirID)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir {
			sub := t.GetOrCreateFolder(dst, e.Name, src)
			t.mergeSGADir(sga, e.DirID, sub, src)
		} else {
			t.addFileSGA(dst, e.Name, src, e.FileID)
		}
	}
}

func (t *ModTree) GetOrCreateFolder(parent *TreeFolder, name string, src *FileSource) *TreeFolder {
	lower := strings.ToLower(name)
	if ex, ok := parent.folderByLower[lower]; ok {
		return ex
	}
	full := name
	if parent.FullPath != "" && parent.FullPath != "Data" {
		full = parent.FullPath + "\\" + name
	} else if parent.FullPath == "Data" {
		full = "Data\\" + name
	}
	// For root's children, full is "Data\<name>" ; root itself is "Data".
	if parent == t.Root && parent.FullPath == "Data" {
		full = "Data\\" + name
	}
	if parent.FullPath == "" {
		full = name
	}
	f := NewFolder(name, full, parent)
	parent.Children = append(parent.Children, f)
	parent.folderByLower[lower] = f
	return f
}

func (t *ModTree) addFile(parent *TreeFolder, name string, src *FileSource) {
	lower := strings.ToLower(name)
	if _, exists := parent.FileByLower[lower]; exists {
		return
	}
	full := parent.FullPath + "\\" + name
	f := &TreeFile{Name: name, FullPath: full, Source: src}
	// physical location: for FS compute disk path, for SGA find file id
	if !src.IsSGA {
		relAfterData := ""
		if parent == t.Root {
			relAfterData = name
		} else {
			rel := strings.TrimPrefix(full, "Data\\")
			relAfterData = rel
		}
		// relAfterData is like "attrib\ebps\...\monolith.rgd"; convert \ to OS sep
		parts := strings.Split(relAfterData, "\\")
		f.DiskPath = filepath.Join(append([]string{src.DiskRoot}, parts...)...)
	} else {
		// locate file id inside SGA by walking virtual path?
		// We can find via dir traversal: but simpler, search SGA handles for this parent path
		// Instead store by asking SGA: we have parent dst's virtual path, not SGA dir id.
		// We can search via SGA: brute force iterate all dirs to find matching virtual?
		// Alternative: during mergeSGADir we know fileID directly — modify merge to pass id.
		// For now leave SGAFileID = -1 and fill in mergeSGADir variant below.
		f.SGAFileID = -1
	}
	parent.Files = append(parent.Files, f)
	parent.FileByLower[lower] = f
}

func (t *ModTree) addFileSGA(parent *TreeFolder, name string, src *FileSource, fileID int) {
	lower := strings.ToLower(name)
	if _, exists := parent.FileByLower[lower]; exists {
		return
	}
	full := parent.FullPath + "\\" + name
	f := &TreeFile{Name: name, FullPath: full, Source: src, SGAFileID: fileID}
	parent.Files = append(parent.Files, f)
	parent.FileByLower[lower] = f
}

// addFileEntry puts one already-built file into a merged folder, keeping
// whichever source registered it first. It is addFile and addFileSGA's shared
// rule, split out because the parallel merge copies files that were built
// earlier rather than re-deriving them from a name and an id.
func (t *ModTree) addFileEntry(parent *TreeFolder, file *TreeFile, src *FileSource) {
	lower := strings.ToLower(file.Name)
	if _, exists := parent.FileByLower[lower]; exists {
		return
	}
	f := &TreeFile{
		Name:      file.Name,
		FullPath:  parent.FullPath + "\\" + file.Name,
		Source:    src,
		SGAFileID: file.SGAFileID,
	}
	if !src.IsSGA {
		f.DiskPath = file.DiskPath
	}
	parent.Files = append(parent.Files, f)
	parent.FileByLower[lower] = f
}

func (t *ModTree) SortAll(f *TreeFolder) {
	sort.Slice(f.Children, func(a, b int) bool {
		la := strings.ToLower(f.Children[a].Name)
		lb := strings.ToLower(f.Children[b].Name)
		if la == lb {
			return f.Children[a].Name < f.Children[b].Name
		}
		return la < lb
	})
	sort.Slice(f.Files, func(a, b int) bool {
		la := strings.ToLower(f.Files[a].Name)
		lb := strings.ToLower(f.Files[b].Name)
		if la == lb {
			return f.Files[a].Name < f.Files[b].Name
		}
		return la < lb
	})
	for _, c := range f.Children {
		t.SortAll(c)
	}
}

// ── overlay / writable helpers (CFileMap::_MakeFolderWritable + CModuleFile::_DoLoadFolder) ──

// preferredDataFolderName picks the writable folder name per CModuleFile::ReloadResources DoW heuristic:
// prefer "Data" exactly, else first without dynamics '%', else first DataFolder, else "Data".
func (t *ModTree) preferredDataFolderName() string {
	if t.ModFilePath != "" {
		if mf, err := ParseModule(t.ModFilePath); err == nil {
			keys := sortedKeysInt(mf.DataFolders)
			for _, k := range keys {
				if strings.EqualFold(strings.TrimSpace(mf.DataFolders[k]), "Data") {
					return mf.DataFolders[k]
				}
			}
			for _, k := range keys {
				raw := mf.DataFolders[k]
				if !strings.Contains(raw, "%") {
					return raw
				}
			}
			if len(keys) > 0 {
				return mf.DataFolders[keys[0]]
			}
		}
	}
	return "Data"
}

func (t *ModTree) ResolveWritableRoot() (string, error) {
	if t == nil {
		return "", fmt.Errorf("no mod tree")
	}
	if t.AppPath == "" {
		return "", fmt.Errorf("no AppPath")
	}
	raw := t.preferredDataFolderName()
	rel := raw
	if strings.Contains(raw, "%") {
		rel = ResolveDynamics(raw, t.Locale, "")
	}
	// rel may be like "Data" or "Locale\\English\\Data" — both are relative to ModFolder
	modFolder := t.FileMapName
	if t.ModFilePath != "" {
		if mf, err := ParseModule(t.ModFilePath); err == nil && mf.ModFolder != "" {
			modFolder = mf.ModFolder
		}
	}
	// The .module spells the folder the way Windows reads it. A filesystem
	// that folds case may hold the mod under another spelling, and writing to
	// the declared one would create a second Data beside the real data —
	// saves the scan never sees, because it registered the folder that was
	// already there.
	root := caseResolvedPath(filepath.Join(t.AppPath, modFolder, moduleRelPath(rel)))
	t.WritableOverlayRoot = root
	return root, nil
}

func (t *ModTree) ensureWritableRoot() (string, *FileSource, error) {
	root, err := t.ResolveWritableRoot()
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", nil, err
	}
	// find existing source for this root
	for _, s := range t.Sources {
		if !s.IsSGA && strings.EqualFold(filepath.Clean(s.DiskRoot), root) {
			return root, s, nil
		}
	}
	src := &FileSource{
		ModName:    t.FileMapName,
		SourceName: "Data",
		Kind:       KindThisMod,
		Priority:   0,
		IsSGA:      false,
		DiskRoot:   root,
	}
	t.Sources = append(t.Sources, src)
	// ensure root folder mapping exists (no need to merge, just virtual root)
	return root, src, nil
}

// OverlayPath maps a virtual path like "Data\\attrib\\ebps\\...\\file.rgd" to an
// absolute disk path inside the writable overlay folder, creating parent dirs.
func (t *ModTree) OverlayPath(virtualPath string) (string, error) {
	if virtualPath == "" {
		return "", fmt.Errorf("empty virtual path")
	}
	root, _, err := t.ensureWritableRoot()
	if err != nil {
		return "", err
	}
	normalized := strings.ReplaceAll(virtualPath, "/", "\\")
	rel := normalized
	if strings.HasPrefix(strings.ToLower(rel), "data\\") {
		rel = rel[5:]
	} else if strings.EqualFold(rel, "Data") {
		rel = ""
	}
	if rel == "" {
		return root, nil
	}
	parts := strings.Split(rel, "\\")
	disk := filepath.Clean(filepath.Join(append([]string{root}, parts...)...))
	relToRoot, err := filepath.Rel(filepath.Clean(root), disk)
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("overlay path escapes mod root: %s", virtualPath)
	}
	if err := os.MkdirAll(filepath.Dir(disk), 0755); err != nil {
		return "", err
	}
	return disk, nil
}

// WriteOverlayFile writes data to the overlay path for virtualPath, creating dirs
// and upserting the file into the merged tree so it appears as ThisMod.
func (t *ModTree) WriteOverlayFile(virtualPath string, data []byte) (string, error) {
	disk, err := t.OverlayPath(virtualPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(disk, data, 0644); err != nil {
		return "", err
	}
	t.upsertOverlayFile(virtualPath, disk)
	return disk, nil
}

func (t *ModTree) upsertOverlayFile(virtualPath, diskPath string) {
	if t.Root == nil {
		return
	}
	_, src, _ := t.ensureWritableRoot()
	if src == nil {
		src = &FileSource{ModName: t.FileMapName, SourceName: "Data", Kind: KindThisMod, Priority: 0, IsSGA: false, DiskRoot: filepath.Dir(diskPath)}
	}
	normalized := strings.ReplaceAll(virtualPath, "/", "\\")
	parts := strings.Split(normalized, "\\")
	if len(parts) == 0 {
		return
	}
	if strings.EqualFold(parts[0], "Data") {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return
	}
	fileName := parts[len(parts)-1]
	dirParts := parts[:len(parts)-1]
	cur := t.Root
	for _, seg := range dirParts {
		if seg == "" {
			continue
		}
		cur = t.GetOrCreateFolder(cur, seg, src)
	}
	lower := strings.ToLower(fileName)
	if ex, ok := cur.FileByLower[lower]; ok {
		ex.Source = src
		ex.DiskPath = diskPath
		ex.SGAFileID = -1
	} else {
		full := cur.FullPath + "\\" + fileName
		if cur == t.Root && cur.FullPath == "Data" && len(dirParts) == 0 {
			full = "Data\\" + fileName
		}
		f := &TreeFile{Name: fileName, FullPath: full, Source: src, DiskPath: diskPath, SGAFileID: -1}
		cur.Files = append(cur.Files, f)
		cur.FileByLower[lower] = f
	}
	// keep folder's files sorted for stable view
	sort.Slice(cur.Files, func(a, b int) bool {
		la := strings.ToLower(cur.Files[a].Name)
		lb := strings.ToLower(cur.Files[b].Name)
		if la == lb {
			return cur.Files[a].Name < cur.Files[b].Name
		}
		return la < lb
	})
	t.SortAll(t.Root)
}

// RemoveFile takes a file out of the merged tree, which is what a file that has
// been moved out of the mod needs: the tree is what the rest of the app
// believes the mod contains, so a file that is no longer on disk has to leave
// it too or every list keeps offering it.
//
// Only a file the mod owns can be removed. A file that came from a required
// mod or an archive is still there as far as the game is concerned, and
// dropping it from the tree would hide a file that is really being loaded.
func (t *ModTree) RemoveFile(virtualPath string) bool {
	f := t.FindFile(virtualPath)
	if f == nil {
		return false
	}
	if f.Source == nil || f.Source.IsSGA || f.Source.Kind != KindThisMod {
		return false
	}
	dir, base := VirtualDirAndBase(f.FullPath)
	folder := t.Root
	if dir != "" {
		folder = FindFolderByPath(t.Root, strings.TrimPrefix(dir, `Data\`))
		if folder == nil {
			return false
		}
	}
	lower := strings.ToLower(base)
	if folder.FileByLower[lower] != f {
		return false
	}
	delete(folder.FileByLower, lower)
	kept := folder.Files[:0]
	for _, existing := range folder.Files {
		if existing != f {
			kept = append(kept, existing)
		}
	}
	folder.Files = kept
	// A file that was only here because this mod added it may also have been
	// reachable by bare name, and that index is a cache of the tree. It is
	// guarded for the same reason FindByName's build is: a reader may be walking
	// the index at this moment.
	t.byNameMu.Lock()
	delete(t.byName, lower)
	t.byNameMu.Unlock()
	return true
}

// ── UI helpers: flattened visible tree for Gio ─────────────────────────────────

type VisibleNode struct {
	Name     string
	FullPath string
	Kind     SourceKind
	ModName  string
	IsDir    bool
	IsFile   bool
	Depth    int
	HasKids  bool
	Expanded bool
	Selected bool
}

func (t *ModTree) Flatten(expanded map[string]bool, selectedPath string) []VisibleNode {
	var out []VisibleNode
	// We display root's children (Data is root; Corsix displays entry point "Data" as root item?)
	// Original frmFiles::FillFromIDirectoryTraverser adds entry point "Data" as item then recurses.
	// For simplicity we emit "Data" as first item (expanded by default), then its children.
	out = append(out, VisibleNode{
		Name:     t.Root.Name,
		FullPath: t.Root.FullPath,
		IsDir:    true,
		Depth:    0,
		HasKids:  len(t.Root.Children)+len(t.Root.Files) > 0,
		Expanded: expanded[t.Root.FullPath],
		Selected: selectedPath == t.Root.FullPath,
	})
	if expanded[t.Root.FullPath] {
		t.flattenRecurse(t.Root, 1, expanded, selectedPath, &out)
	}
	return out
}

func (t *ModTree) flattenRecurse(folder *TreeFolder, depth int, expanded map[string]bool, sel string, out *[]VisibleNode) {
	// Folders first, then files (like Corsix iterator: folders then files)
	for _, c := range folder.Children {
		hasKids := len(c.Children)+len(c.Files) > 0
		exp := expanded[c.FullPath]
		*out = append(*out, VisibleNode{
			Name:     c.Name,
			FullPath: c.FullPath,
			IsDir:    true,
			Depth:    depth,
			HasKids:  hasKids,
			Expanded: exp,
		})
		if exp && hasKids {
			t.flattenRecurse(c, depth+1, expanded, sel, out)
		}
	}
	for _, f := range folder.Files {
		*out = append(*out, VisibleNode{
			Name:     f.Name,
			FullPath: f.FullPath,
			IsFile:   true,
			Depth:    depth,
			ModName:  f.Source.ModName,
			Kind:     f.Source.Kind,
			Selected: f.FullPath == sel,
		})
	}
}

// ── name search ───────────────────────────────────────────────────────────

// SearchKind selects which file types a search covers. Search is by file name
// for now; content search can be layered on later behind the same entry point.
type SearchKind int

const (
	SearchAll SearchKind = iota
	SearchRgd
	SearchLua
	SearchFda
	SearchArt
	SearchWhm
)

// SearchKinds is the order the filter chips appear in the UI.
var SearchKinds = []SearchKind{SearchAll, SearchRgd, SearchLua, SearchFda, SearchArt, SearchWhm}

// Extensions returns the file extensions the kind matches, without the dot.
// SearchAll matches everything, so it returns nil.
func (k SearchKind) Extensions() []string {
	switch k {
	case SearchRgd:
		return []string{".rgd"}
	case SearchLua:
		return []string{".lua"}
	case SearchFda:
		return []string{".fda"}
	case SearchArt:
		// The Definitive Edition ships no .art files at all — its texture
		// containers are .rsh — so a literal .art filter would be permanently
		// empty. The chip stands for the art/texture content, which is what
		// the user means when filtering for it.
		return []string{".art", ".rsh", ".dds", ".tga", ".texture"}
	case SearchWhm:
		return []string{".whm"}
	}
	return nil
}

// Label is the chip text shown in the UI.
func (k SearchKind) Label() string {
	switch k {
	case SearchAll:
		return "All"
	case SearchRgd:
		return "RGD"
	case SearchLua:
		return "Lua"
	case SearchFda:
		return "FDA"
	case SearchArt:
		return "ART"
	case SearchWhm:
		return "WHM"
	}
	return "All"
}

func (k SearchKind) matches(name string) bool {
	exts := k.Extensions()
	if len(exts) == 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// SearchHit is one matching file plus how it matched, so the UI can show a
// folder name next to a file name and both can be identical.
type SearchHit struct {
	File *TreeFile
	// Score is lower for a better match. Name matches beat folder matches.
	Score int
	// MatchIn tells which part of the path matched, for the UI.
	MatchIn string // "name" or "folder"
	// MatchWord is the query word that produced Score, so the UI can
	// highlight it inside the name without re-running the ranking.
	MatchWord string
}

// SearchWalk visits every file in the merged tree. The tree already holds this
// mod, every required mod, and the engine, so one walk covers all of them.
func SearchWalk(root *TreeFolder, visit func(f *TreeFile)) {
	if root == nil {
		return
	}
	for _, f := range root.Files {
		visit(f)
	}
	for _, c := range root.Children {
		SearchWalk(c, visit)
	}
}

// Search returns files whose name or containing folder matches the query.
// Matching is case-insensitive and supports "word1 word2" — all words must
// match somewhere in the file's own name or folder path, so "lord necro"
// finds data\...\necrons\necron_lord_tgd.whm. A leading "*" is accepted and
// ignored, matching the wildcard users are used to typing.
//
// Results are ordered best-first: this mod before required mods before the
// engine, and inside one source shorter paths first, so the most likely file
// is at the top. limit caps the returned list; limit <= 0 means no cap.
func (t *ModTree) Search(query string, kind SearchKind, limit int) []SearchHit {
	if t == nil || t.Root == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	query = strings.TrimPrefix(query, "*")
	if query == "" {
		return nil
	}
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}

	var hits []SearchHit
	SearchWalk(t.Root, func(f *TreeFile) {
		if !kind.matches(f.Name) {
			return
		}
		nameLower := strings.ToLower(f.Name)
		folderLower := strings.ToLower(strings.TrimSuffix(f.FullPath, f.Name))
		// Every word must appear in the name, in the folder path, or be split
		// between them; a word in neither rules the file out. The best
		// (lowest) per-word score becomes the file's score, and a folder-only
		// match is pushed past every name match so a same-named file in a
		// subfolder still wins.
		best := 1 << 30
		bestWord := ""
		for _, w := range words {
			s := scoreWord(nameLower, w)
			if s < 0 {
				// Not in the name: the folder is the only other place a word
				// can be. Check for absence before applying the bias, or an
				// absent word would come out as a near-zero score and match
				// every file in the tree.
				folderScore := scoreWord(folderLower, w)
				if folderScore < 0 {
					return
				}
				s = folderScore + folderScoreBias
			}
			if s < best {
				best, bestWord = s, w
			}
		}
		where := "name"
		if best >= folderScoreBias {
			where = "folder"
		}
		hits = append(hits, SearchHit{File: f, Score: best, MatchIn: where, MatchWord: bestWord})
	})

	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score < b.Score
		}
		ak, bk := sourceRank(a.File), sourceRank(b.File)
		if ak != bk {
			return ak < bk
		}
		if len(a.File.FullPath) != len(b.File.FullPath) {
			return len(a.File.FullPath) < len(b.File.FullPath)
		}
		return strings.ToLower(a.File.FullPath) < strings.ToLower(b.File.FullPath)
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// folderScoreBias pushes a folder match behind every possible name match, so
// the ranking puts files named like the query above files merely filed under a
// matching folder.
const folderScoreBias = 100

// scoreWord rates one word against one lower-cased string: 0 is a full match,
// 1 a prefix, 2 a substring, and -1 means the word is absent.
func scoreWord(hay, word string) int {
	switch {
	case hay == word:
		return 0
	case strings.HasPrefix(hay, word):
		return 1
	case strings.Contains(hay, word):
		return 2
	}
	return -1
}

// sourceRank orders sources the way the tree colours them: this mod first,
// then required mods, then the engine.
func sourceRank(f *TreeFile) int {
	if f == nil || f.Source == nil {
		return 3
	}
	switch f.Source.Kind {
	case KindThisMod:
		return 0
	case KindReqMod:
		return 1
	}
	return 2
}

// ── file access helpers ────────────────────────────────────────────────────

// FindFile resolves a virtual path like "Data\attrib\ebps\race_necron\structures\monolith.rgd"
// to its TreeFile. Search is case-insensitive per component.
func (t *ModTree) FindFile(virtualPath string) *TreeFile {
	if virtualPath == "" {
		return nil
	}
	normalized := strings.ReplaceAll(virtualPath, "/", "\\")
	parts := strings.Split(normalized, "\\")
	// drop leading Data if present (root is Data)
	if len(parts) > 0 && strings.EqualFold(parts[0], "Data") {
		parts = parts[1:]
		if len(parts) == 0 {
			return nil
		}
	}
	cur := t.Root
	for i, seg := range parts {
		lower := strings.ToLower(seg)
		isLast := i == len(parts)-1
		if isLast {
			if f, ok := cur.FileByLower[lower]; ok {
				return f
			}
			// maybe folder named like file? not found
			return nil
		}
		if nxt, ok := cur.folderByLower[lower]; ok {
			cur = nxt
		} else {
			return nil
		}
	}
	return nil
}

// FindFolder returns a virtual folder, or nil. It is what a caller that was
// given a folder's path rather than a file's needs: the sounds of a unit live
// in a folder of numbered variants, and listing one is the only way to see
// them all.
func (t *ModTree) FindFolder(virtualPath string) *TreeFolder {
	if t == nil || t.Root == nil || virtualPath == "" {
		return nil
	}
	normalized := strings.ReplaceAll(virtualPath, "/", "\\")
	parts := strings.Split(strings.Trim(normalized, `\`), "\\")
	if len(parts) > 0 && strings.EqualFold(parts[0], "Data") {
		parts = parts[1:]
	}
	cur := t.Root
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		nxt, ok := cur.folderByLower[strings.ToLower(seg)]
		if !ok {
			return nil
		}
		cur = nxt
	}
	return cur
}

// ReadFile reads raw bytes of a virtual file, resolving from either FS or SGA.
func (t *ModTree) ReadFile(virtualPath string) ([]byte, error) {
	f := t.FindFile(virtualPath)
	if f == nil {
		return nil, fmt.Errorf("file not found: %s", virtualPath)
	}
	return t.ReadTreeFile(f)
}

// HasFile reports whether a virtual path names a file the tree can read, and
// whether that file has anything in it, without reading it.
//
// Everything that walks a model's or an effect's file list asks this of every
// name it comes across, often for the same name twice, and the answers come
// out of the table of contents the tree was built from. Reading each file to
// find out whether it is there is the most wasteful thing a lookup can do.
func (t *ModTree) HasFile(virtualPath string) bool {
	return t.HasTreeFile(t.FindFile(virtualPath))
}

// HasTreeFile is HasFile for a file the tree has already found.
func (t *ModTree) HasTreeFile(f *TreeFile) bool {
	if f == nil || f.Source == nil {
		return false
	}
	if f.Source.IsSGA {
		if f.Source.SGAHandle == nil || f.SGAFileID < 0 {
			return false
		}
		return f.Source.SGAHandle.HasFile(f.SGAFileID)
	}
	if f.DiskPath == "" {
		return false
	}
	st, err := os.Stat(f.DiskPath)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func (t *ModTree) ReadTreeFile(f *TreeFile) ([]byte, error) {
	if f.Source.IsSGA {
		if f.Source.SGAHandle == nil {
			return nil, fmt.Errorf("sga handle missing")
		}
		if f.SGAFileID < 0 {
			return nil, fmt.Errorf("sga file id missing for %s", f.FullPath)
		}
		return f.Source.SGAHandle.ReadFile(f.SGAFileID)
	}
	// FS
	p := f.DiskPath
	if p == "" {
		// fallback: compute from DiskRoot if not stored? shouldn't happen
		return nil, fmt.Errorf("disk path missing for %s", f.FullPath)
	}
	return os.ReadFile(p)
}

// OpenedFileFromVirtual helper for UI (also returns source info).
func (t *ModTree) OpenedFileInfo(virtualPath string) (*TreeFile, []byte, error) {
	f := t.FindFile(virtualPath)
	if f == nil {
		return nil, nil, fmt.Errorf("not found")
	}
	b, err := t.ReadTreeFile(f)
	return f, b, err
}

// Debug: count totals
func (t *ModTree) Count() (folders, files int) {
	t.countRec(t.Root, &folders, &files)
	return
}
func (t *ModTree) countRec(f *TreeFolder, folders, files *int) {
	*folders += len(f.Children)
	*files += len(f.Files)
	for _, c := range f.Children {
		t.countRec(c, folders, files)
	}
}
