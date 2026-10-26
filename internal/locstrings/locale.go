package locstrings

import (
	"dow-mod-studio/internal/rgd"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

// ── localisation strings ───────────────────────────────────────────────────
//
// A DT_WString in a RGD holds a localisation id, e.g. "$90100" for a
// screen_name_id. Measured over the real DXP3 tree, these are keys into the
// game's string table and nothing else: 620 distinct ids across 401 files, all
// of the form "$<digits>", used by fields such as screen_name_id, help_text_id
// and text_01..text_08.
//
// The text behind an id lives in the .ucs tables next to the game data, one
// file per module and language:
//
//	<DoWDE>/Engine/Locale/<Language>/Engine.ucs
//	<DoWDE>/<Module>/Locale/<Language>/<Something>.ucs
//
// Measured format: UTF-16LE with a byte order mark, CRLF line endings, and one
// entry per line as "id<TAB>text". English Engine.ucs is 3.4 MB over 26439
// lines and carries the ids the units use (90100 "Armour", 705754 "Necron
// Lord", 4300000 "Act of Faith: Ascension"). Most lines are exactly two tab
// separated fields, but three English lines embed literal tabs in the text, so
// a line is split at the *first* tab only.
//
// The id is the value and is always shown as it is; rgd.LocIDDisplay appends the
// resolved text next to it, and Alt+click on the row opens the .ucs the text
// came from with the caret on the line. See localisedString and
// localeSourceFiles.

const LocaleDefault = "English"

// localeEntry is one localisation id resolved against a language's .ucs files.
// File and Line say where the text came from, which is what makes the jump
// possible; Line is 1-based, as an editor counts.
type LocaleEntry struct {
	Text string
	File string
	Line int
}

// localeTable is the id index of a single language.
type LocaleTable struct {
	Lang  string
	Index map[string]LocaleEntry
	Files int // .ucs files that were read
	Lines int // entries indexed
	Err   error
}

// The active table and the per-language cache behind it. rgd.RgdEntry has no
// pointer to uiState, so the table the value views read is a package level
// one, rebuilt whenever the chosen language or the open module changes. The
// views only ever read a finished table: a build is started explicitly from
// setLocale, never from inside a lookup, so laying out a frame cannot stall on
// reading 3.4 MB of UCS.
var LocaleState struct {
	mu    sync.RWMutex
	cur   *LocaleTable
	cache map[string]*LocaleTable
	// overrideDir is the open mod's own project folder, which is searched
	// before anything installed. It is here rather than in the arguments
	// because a string this tool writes has to be readable the moment after it
	// is written, and threading a third path through every reader would put
	// that burden on each caller instead.
	overrideDir string
	// reservedID is the last id handed out by allocateLocaleID. It is kept apart
	// from the table because an id is reserved before it is written, and the
	// table only learns of it once the write has been read back in.
	reservedID int64
}

// setLocaleOverrideDir says which project folder the mod's own strings live in.
// An empty dir puts it out of the search, which is what a closed mod and a test
// that only wants the game's own tables both want.
//
// It drops the built tables, because a table is a function of the folders it was
// read from and the folder has just changed.
func SetLocaleOverrideDir(dir string) {
	LocaleState.mu.Lock()
	LocaleState.overrideDir = strings.TrimSpace(dir)
	LocaleState.cache = nil
	LocaleState.cur = nil
	LocaleState.mu.Unlock()
}

// invalidateLocaleCache drops every built table so the next setLocale reads the
// files again. Writing a string has to call this: tables are cached per
// language, so without it the string that was just written is missing from the
// copy the views are reading and the edit looks like it did nothing.
func InvalidateLocaleCache() {
	LocaleState.mu.Lock()
	LocaleState.cache = nil
	LocaleState.cur = nil
	LocaleState.mu.Unlock()
}

// localeOverrideRoot is that folder, or "" when there is none.
func LocaleOverrideRoot() string {
	LocaleState.mu.RLock()
	defer LocaleState.mu.RUnlock()
	return LocaleState.overrideDir
}

// ── decoding ───────────────────────────────────────────────────────────────

// decodeUCS turns a .ucs file into UTF-8. The files are UTF-16 with a byte
// order mark, little endian in every installation measured; big endian is
// accepted too, and a file with no mark is taken as already being UTF-8, which
// is what a .ucs saved by an editor on a modding tool comes out as.
func DecodeUCS(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return DecodeUTF16(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return DecodeUTF16(data[2:], true)
	}
	return string(data)
}

func DecodeUTF16(b []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if bigEndian {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			units = append(units, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return string(utf16.Decode(units))
}

// ucsBOM is the byte order mark character U+FEFF. It is written as a rune
// rather than as an escape so that no literal byte order mark ends up in the
// source, which the compiler rejects.
const UcsBOM = string(rune(0xFEFF))

// trimBOM drops a leading U+FEFF. A UCS decoded without its mark consumed
// still starts with the character, and it would end up glued to the first id.
func TrimBOM(s string) string {
	return strings.TrimPrefix(s, UcsBOM)
}

// parseUCS indexes one .ucs file. A line is an entry when it starts with an
// all digit id followed by a tab; anything else (a blank line, a stray
// comment) is skipped rather than treated as an error, because the tables are
// hand edited. Entries already in index are kept, so the first file to define
// an id is the one that wins.
func ParseUCS(data []byte, file string, index map[string]LocaleEntry) int {
	text := TrimBOM(DecodeUCS(data))
	added := 0
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		id := line[:tab]
		if !IsLocaleID(id) {
			continue
		}
		if _, seen := index[id]; seen {
			continue
		}
		index[id] = LocaleEntry{
			Text: line[tab+1:],
			File: file,
			Line: i + 1,
		}
		added++
	}
	return added
}

func IsLocaleID(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// ── finding the files ──────────────────────────────────────────────────────

// localeModuleDir is the folder a module's own data lives in. The module is
// opened either as a folder or as its .module file, and the .ucs tables sit
// beside the data either way. The extension is checked as well as the file
// itself, because a path that cannot be stat'ed would otherwise be taken for a
// folder and quietly find no tables.
func LocaleModuleDir(modulePath string) string {
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" {
		return ""
	}
	if strings.EqualFold(filepath.Ext(modulePath), ".module") {
		return filepath.Dir(modulePath)
	}
	if info, err := os.Stat(modulePath); err == nil && !info.IsDir() {
		return filepath.Dir(modulePath)
	}
	return filepath.Clean(modulePath)
}

// localeRoots lists the folders that may hold a Locale folder for this game,
// the mod's own project folder first, so a string this tool wrote for the open
// mod is in the table the moment after it was written, then the open module,
// then every other installed module. Everything installed is one level under
// the game folder, which is the layout the engine uses: Engine, plus one folder
// per installed module.
func LocaleRoots(gamePath, moduleDir string) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(dir string) {
		dir = filepath.Clean(dir)
		if dir == "" || dir == "." || seen[strings.ToLower(dir)] {
			return
		}
		seen[strings.ToLower(dir)] = true
		roots = append(roots, dir)
	}
	add(LocaleOverrideRoot())
	add(moduleDir)
	gamePath = strings.TrimSpace(gamePath)
	if gamePath == "" {
		return roots
	}
	entries, err := os.ReadDir(gamePath)
	if err != nil {
		return roots
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	for _, name := range names {
		add(filepath.Join(gamePath, name))
	}
	return roots
}

// localeSourceFiles lists the .ucs files that make up one language, in the
// order they are read: the open module's own locale folder first, then every
// other installed module's, Engine included. Reading in that order and keeping
// the first definition of an id is what lets a mod override an engine string
// by shipping its own table.
func LocaleSourceFiles(gamePath, moduleDir, lang string) []string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return nil
	}
	var files []string
	for _, root := range LocaleRoots(gamePath, moduleDir) {
		matches, err := filepath.Glob(filepath.Join(root, "Locale", lang, "*.ucs"))
		if err != nil {
			continue
		}
		sort.Slice(matches, func(i, j int) bool {
			return strings.ToLower(matches[i]) < strings.ToLower(matches[j])
		})
		files = append(files, matches...)
	}
	return files
}

// availableLocales is the list offered in the menu bar: every language the
// engine ships, plus any extra one an installed module adds (the THB test
// module, for instance, carries Slovak and English_Chinese on top of the
// engine's thirteen). A game folder that cannot be read still offers English,
// which is what the mod tree itself is built with.
func AvailableLocales(gamePath, moduleDir string) []string {
	seen := map[string]bool{}
	var langs []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		langs = append(langs, name)
	}
	for _, root := range LocaleRoots(gamePath, moduleDir) {
		entries, err := os.ReadDir(filepath.Join(root, "Locale"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				add(e.Name())
			}
		}
	}
	add(LocaleDefault)
	sort.Slice(langs, func(i, j int) bool {
		return strings.ToLower(langs[i]) < strings.ToLower(langs[j])
	})
	return langs
}

// ── the table ──────────────────────────────────────────────────────────────

// buildLocaleTable reads every .ucs of one language and indexes it. This is
// the expensive call — English is 3.4 MB in one file — so it runs from an
// explicit language change rather than from the value views.
func BuildLocaleTable(gamePath, moduleDir, lang string) *LocaleTable {
	table := &LocaleTable{Lang: lang, Index: make(map[string]LocaleEntry)}
	files := LocaleSourceFiles(gamePath, moduleDir, lang)
	var firstErr error
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		table.Lines += ParseUCS(data, file, table.Index)
	}
	table.Files = len(files)
	table.Err = firstErr
	return table
}

// setLocale rebuilds the string table for lang and makes it the one the value
// views read. Tables are cached per language, so going back to a language that
// was already loaded costs nothing. The language is remembered even when the
// table turns out to be empty: a language with no .ucs files yet is still the
// one the user picked, and the ids stay visible as ids.
func SetLocale(gamePath, modulePath, lang string) *LocaleTable {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = LocaleDefault
	}
	moduleDir := LocaleModuleDir(modulePath)

	LocaleState.mu.RLock()
	cached := LocaleState.cache[lang]
	LocaleState.mu.RUnlock()

	var table *LocaleTable
	if cached != nil {
		table = cached
	} else {
		table = BuildLocaleTable(gamePath, moduleDir, lang)
		LocaleState.mu.Lock()
		if LocaleState.cache == nil {
			LocaleState.cache = make(map[string]*LocaleTable)
		}
		LocaleState.cache[lang] = table
		LocaleState.mu.Unlock()
	}

	LocaleState.mu.Lock()
	LocaleState.cur = table
	LocaleState.mu.Unlock()
	return table
}

// currentLocaleTable is the table the value views read.
func CurrentLocaleTable() *LocaleTable {
	LocaleState.mu.RLock()
	defer LocaleState.mu.RUnlock()
	return LocaleState.cur
}

// localeLookup resolves one id against the active table.
func LocaleLookup(id string) (LocaleEntry, bool) {
	table := CurrentLocaleTable()
	if table == nil || id == "" {
		return LocaleEntry{}, false
	}
	entry, ok := table.Index[id]
	return entry, ok
}

// localisedString is the text behind a localisation id in the chosen language,
// or "" when the id is not in the table. rgd.LocIDDisplay already appends
// whatever this returns, so a miss leaves the bare id on screen.
func LocalisedString(id string) string {
	entry, ok := LocaleLookup(id)
	if !ok {
		return ""
	}
	return entry.Text
}

func Init() {
	// The rgd package renders localisation ids but owns no locale state; the
	// application hands it the lookup.
	rgd.Localize = LocalisedString
}

// localeLineNumber is the line the id sits on in its .ucs, or 0 when the id is
// not in the table. Alt+click on a value uses it to land on the line.
func LocaleLineNumber(id string) int {
	entry, ok := LocaleLookup(id)
	if !ok {
		return 0
	}
	return entry.Line
}

// resetLocaleTables drops the cache. Tests use it so one fixture does not
// decide what the next one sees.
func ResetLocaleTables() {
	LocaleState.mu.Lock()
	LocaleState.cur = nil
	LocaleState.cache = nil
	LocaleState.reservedID = 0
	LocaleState.mu.Unlock()
}
