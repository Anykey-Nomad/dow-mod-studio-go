// Command sgafind looks inside Relic SGA archives: it reports whether a file
// (or a folder) is in an archive, and it can extract what it finds.
//
// The archives are the ones Dawn of War loads from a .module, so a name from
// an FX file or a .rsh reference can be traced to the archive that actually
// holds it without unpacking anything by hand.
//
//	sgafind -sga DXP3/DXP3Data.sga -act check art/fx/vortex_ring.dds
//	sgafind -sga DXP3/DXP3Data.sga -act extract marked_target_00.tga
//	sgafind -mod AdvancedCampaign -act check *.tga
//	sgafind -sga DOWDE/DoWDE.sga -act extract -ext fda
//	sgafind -sga DOWDE/DoWDE.sga -act extract -ext fda,wav -out unpacked
//
// With -mod every archive the module loads is searched, this mod's own
// folders and archives first and the required mods after it, which is the
// order the game itself resolves names in.
//
// With -ext only files of those formats are taken, whatever names were
// given; with no names at all, every file of them is. Everything extracted
// is written under the folders it is filed under inside the archive, its
// entry point included: a file the archive files as music/example.fda under
// the entry point data lands in <archive folder>/data/music/example.fda,
// which is where the game keeps the same file loose. -out moves that root.
package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/sga"
)

func main() {
	var sgas stringsFlag
	var actions stringsFlag
	var exts stringsFlag
	mod := flag.String("mod", "", "module file or name to resolve the archives from, e.g. AdvancedCampaign")

	out := flag.String("out", "", "where extracted files go; by default beside the archive, keeping its entry point and internal folders")
	locale := flag.String("locale", "English", "locale used to expand %LOCALE% in a module")
	force := flag.Bool("force", false, "overwrite files that already exist when extracting")
	verbose := flag.Bool("v", false, "also print every entry that was walked past")
	missOnlyVerbose := flag.Bool("miss", false, "only print MISS lines when -v is also set")
	flag.Var(&sgas, "sga", "archive to search: a path, or a name beside the module (repeatable, or comma separated)")
	flag.Var(&actions, "act", "what to do: check, extract, list (repeatable, or comma separated). Default check")
	flag.Var(&exts, "ext", "only take files of these formats: fda or .fda (repeatable, or comma separated)")
	flag.Parse()

	if len(actions) == 0 {
		actions = append(actions, "check")
	}
	extList, err := normalizeExts(exts)
	if err != nil {
		fail("%v", err)
	}

	names := flag.Args()
	archives, err := resolveArchives(sgas, *mod, *locale)
	if err != nil {
		fail("%v", err)
	}
	if len(archives) == 0 {
		fail("nothing to search: give -sga <archive> or -mod <module>")
	}
	wantCheck, wantExtract, wantList, err := parseActions(actions)
	if err != nil {
		fail("%v", err)
	}
	if len(names) == 0 && len(extList) == 0 && !wantList {
		fail("nothing to look for: give a file or folder name, -ext <format>, or -act list")
	}

	missing := 0
	for _, a := range archives {
		f, err := sga.OpenSGA(a.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", a.path, err)
			continue
		}
		if wantList {
			listArchive(f, a, *verbose)
			f.Close()
			continue
		}
		entries := indexArchive(f, a, *verbose)
		if *verbose {
			fmt.Printf("%s: %d entries\n", a.path, len(entries))
		}
		for _, name := range names {
			hits := filterByExt(matchEntries(entries, name), extList)
			switch {
			case len(hits) == 0:
				if wantCheck && (*missOnlyVerbose && *verbose || !*missOnlyVerbose) {
					fmt.Printf("MISS %-48s %s\n", name, a.path)
					missing++
				}
			default:
				handleHits(f, a, hits, name, wantCheck, wantExtract, *out, *force)
			}
		}
		// With no names and -ext, every file of the wanted formats is the
		// selection: the way to unpack a whole format out of an archive.
		if len(names) == 0 && len(extList) > 0 {
			var hits []entry
			for _, e := range entries {
				if !e.IsDir && matchExt(e.Path, extList) {
					hits = append(hits, e)
				}
			}
			handleHits(f, a, hits, strings.Join(extList, ","), wantCheck, wantExtract, *out, *force)
		}
		f.Close()
	}
	if missing > 0 {
		os.Exit(1)
	}
}

// archive is one .sga on disk together with where its contents belong: the
// game unpacks an archive beside itself, keeping the folders inside it, so
// that is the default place an extracted file goes too.
type archive struct {
	path string
	root string
}

// resolveArchives turns the -sga and -mod flags into archives to work on.
//
// A mod is resolved through the same module parser the studio uses, so the
// list is the real load order: the mod's own data folders and archives, then
// every required mod's, then the engine. Data folders are skipped: a folder on
// disk is already readable and needs no archive.
func resolveArchives(names []string, mod, locale string) ([]archive, error) {
	var out []archive
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		key := strings.ToLower(clean)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, archive{path: clean, root: filepath.Dir(clean)})
	}
	// The app directory a mod-relative name is resolved against: the folder
	// holding the module, or the working directory when there is no module.
	appDir := "."
	if mod != "" {
		modulePath := mod
		if !strings.EqualFold(filepath.Ext(modulePath), ".module") {
			// A bare name: AdvancedCampaign or AdvancedCampaign.module.
			for _, cand := range []string{
				filepath.Join(modulePath, "..", modulePath+".module"),
				modulePath + ".module",
			} {
				if st, err := os.Stat(cand); err == nil && !st.IsDir() {
					modulePath = cand
					break
				}
			}
		}
		st, err := os.Stat(modulePath)
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", modulePath, err)
		}
		if st.IsDir() {
			entries, err := os.ReadDir(modulePath)
			if err != nil {
				return nil, err
			}
			found := ""
			for _, e := range entries {
				if strings.EqualFold(filepath.Ext(e.Name()), ".module") {
					found = filepath.Join(modulePath, e.Name())
					break
				}
			}
			if found == "" {
				return nil, fmt.Errorf("no .module in %s", modulePath)
			}
			modulePath = found
		}
		abs, err := filepath.Abs(modulePath)
		if err != nil {
			return nil, err
		}
		tree, err := modtree.BuildModTree(abs, locale)
		if err != nil {
			return nil, fmt.Errorf("module %s: %w", modulePath, err)
		}
		defer tree.Close()
		appDir = tree.AppPath
		for _, src := range tree.Sources {
			if src.IsSGA {
				add(src.ArchivePath)
			}
		}
		for _, w := range tree.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", w)
		}
	}
	for _, name := range names {
		p := strings.TrimSpace(name)
		if p == "" {
			continue
		}
		if filepath.IsAbs(p) {
			add(p)
			continue
		}
		// A name is looked for as given, then beside the module's folder,
		// then under the app directory: a mod's archives sit in its own
		// folder, but a required mod's sit in theirs.
		found := ""
		for _, cand := range []string{
			p,
			filepath.Join(appDir, p),
		} {
			if _, err := os.Stat(cand); err == nil {
				found = cand
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("archive %s not found", p)
		}
		add(found)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no archives resolved from -mod %s", mod)
	}
	return out, nil
}

func parseActions(actions []string) (check, extract, list bool, err error) {
	for _, a := range actions {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "check", "find", "probe":
			check = true
		case "extract", "get", "pull":
			extract = true
		case "list", "ls":
			list = true
		case "":
		default:
			return false, false, false, fmt.Errorf("unknown action %q", a)
		}
	}
	return check, extract, list, nil
}

// entry is one file or folder inside an archive.
type entry struct {
	Path   string // as it is filed under its entry point, slash separated
	Root   string // the entry point (ToC alias) the file sits under
	FileID int
	Size   int64
	IsDir  bool
}

// indexArchive walks an archive into a flat list of entries, which is what
// the name matching runs over: an SGA's directory table is a tree, and a name
// can be a file in it or a whole branch of it.
func indexArchive(f *sga.SGAFile, a archive, verbose bool) []entry {
	var out []entry
	var walk func(dir int, prefix string, depth int, alias string)
	walk = func(dir int, prefix string, depth int, alias string) {
		if depth > 32 {
			return
		}
		entries, err := f.List(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := e.Name
			if prefix != "" {
				p = prefix + "/" + e.Name
			}
			if e.IsDir {
				out = append(out, entry{Path: p, Root: alias, IsDir: true})
				walk(e.DirID, p, depth+1, alias)
				continue
			}
			out = append(out, entry{Path: p, Root: alias, FileID: e.FileID, Size: e.Size})
			if verbose {
				fmt.Printf("  %-10s %s/%s\n", humanSize(e.Size), alias, p)
			}
		}
	}
	for t := 0; t < f.ToCCount(); t++ {
		root, err := f.RootDir(t)
		if err != nil {
			continue
		}
		alias := f.ToCName(t)
		if alias == "" {
			alias = "Data"
		}
		walk(root, "", 0, alias)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// listArchive prints an archive's shape: its entry points and how much each
// holds, which is what tells a name that is simply absent from one.
func listArchive(f *sga.SGAFile, a archive, verbose bool) {
	fmt.Printf("%s: sga v%d, %d entry point(s)\n", a.path, f.Version(), f.ToCCount())
	for t := 0; t < f.ToCCount(); t++ {
		fmt.Printf("  [%d] %s\n", t, f.ToCName(t))
	}
	if !verbose {
		return
	}
	entries := indexArchive(f, a, true)
	fmt.Printf("  %d entries\n", len(entries))
}

// matchEntries finds the entries a name stands for. A name with no slash is
// matched against the file name alone, so a bare map is found wherever it
// sits; a name with slashes is matched as a path, which covers a folder as
// well as a file. A name holding * or ? is a pattern.
func matchEntries(entries []entry, name string) []entry {
	want := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	want = strings.TrimPrefix(want, "./")
	want = strings.TrimPrefix(want, "data/")
	if want == "" {
		return nil
	}
	base := path.Base(want)
	dir := path.Dir(want)
	if dir == "." {
		dir = ""
	}
	pattern := strings.ContainsAny(want, "*?[")
	var out []entry
	for _, e := range entries {
		lp := strings.ToLower(e.Path)
		if pattern {
			if ok, _ := path.Match(want, lp); ok {
				out = append(out, e)
				continue
			}
			if ok, _ := path.Match(base, path.Base(lp)); ok {
				out = append(out, e)
			}
			continue
		}
		if dir == "" {
			// A bare name is matched on the file name alone, so a map
			// is found wherever in the archive it sits.
			if path.Base(lp) == base {
				out = append(out, e)
			}
			continue
		}
		// A path is matched whole, or as a suffix of one: archives file
		// their contents under an entry point folder and a name taken
		// from inside the game never repeats it. A folder matches the
		// folder itself, whose children are collected below.
		if lp == want || strings.HasSuffix(lp, "/"+want) {
			out = append(out, e)
		}
	}
	// A folder name brings its children with it: asking about
	// "art/fx/necron/monolith" means everything the branch holds, which
	// is what the caller wants to see and what extract writes out.
	if len(out) == 1 && out[0].IsDir {
		branch := out[0].Path
		out = out[:0]
		for _, e := range entries {
			if e.Path != branch && strings.HasPrefix(e.Path, branch+"/") {
				out = append(out, e)
			}
		}
	}
	return out
}

// extract writes one entry out, into the folder the archive sits in and under
// the folders it is filed under inside, its entry point included: a file the
// archive files as art/fx/... under the entry point data unpacks to
// <archive folder>/data/art/fx/..., which is where the game keeps the same
// file loose. -out replaces that root, and -force overwrites what is already
// there.
func extract(f *sga.SGAFile, a archive, e entry, out string, force bool) (string, error) {
	data, err := f.ReadFile(e.FileID)
	if err != nil {
		return "", err
	}
	root := a.root
	if out != "" {
		root = out
	}
	dest := filepath.Join(root, filepath.FromSlash(e.Root), filepath.FromSlash(e.Path))
	if st, err := os.Stat(dest); err == nil && !st.IsDir() {
		if !force && st.Size() == int64(len(data)) {
			return dest, fmt.Errorf("already there")
		}
		if !force {
			return dest, fmt.Errorf("exists (%d bytes, archive has %d); use -force",
				st.Size(), len(data))
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

// handleHits reports and extracts the entries one name, or one -ext
// selection, stands for.
func handleHits(f *sga.SGAFile, a archive, hits []entry, name string, wantCheck, wantExtract bool, out string, force bool) {
	for _, h := range hits {
		line := fmt.Sprintf("HIT  %-48s %s :: %s (%s)",
			name, a.path, h.Path, humanSize(h.Size))
		if wantCheck {
			fmt.Println(line)
		}
		if wantExtract && !h.IsDir {
			dest, err := extract(f, a, h, out, force)
			if err != nil {
				fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", h.Path, err)
				continue
			}
			fmt.Println("SAVED", dest)
		}
	}
}

// filterByExt drops the entries that are not of the wanted formats. With no
// formats given it is the identity, so name matching works unchanged.
func filterByExt(hits []entry, exts []string) []entry {
	if len(exts) == 0 {
		return hits
	}
	out := hits[:0]
	for _, h := range hits {
		if h.IsDir || matchExt(h.Path, exts) {
			out = append(out, h)
		}
	}
	return out
}

// matchExt reports whether a path ends in one of the wanted formats.
func matchExt(p string, exts []string) bool {
	lp := strings.ToLower(p)
	for _, e := range exts {
		if strings.HasSuffix(lp, e) {
			return true
		}
	}
	return false
}

// normalizeExts turns what was typed into lower-case extensions with their
// dot, so fda, .FDA and .fda are all the same format.
func normalizeExts(in []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range in {
		e := strings.ToLower(strings.TrimSpace(raw))
		e = strings.TrimPrefix(e, "*")
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if strings.ContainsAny(e, `/\`) || strings.Contains(e, "..") {
			return nil, fmt.Errorf("bad format %q", raw)
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out, nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sgafind: "+format+"\n", args...)
	os.Exit(2)
}

// stringsFlag collects a flag that may be given more than once, or once with
// commas, which is how archives and actions read best on a command line.
type stringsFlag []string

func (f *stringsFlag) String() string { return strings.Join(*f, ",") }

func (f *stringsFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}
