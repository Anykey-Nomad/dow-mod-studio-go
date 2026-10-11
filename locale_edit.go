package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

// This is the writable half of the string table. Reading one is locale.go's
// job; here a mod's own strings are created and changed, which means choosing
// an id nothing else uses, finding the .ucs to put it in, and writing a file
// the engine will read back.
//
// Nothing here touches the game's own .ucs files. They live under Program Files
// and are the game's data to overwrite, not ours: editing "Necron Lord" in
// place would change it for every mod on the machine and would fail outright on
// a default install. So a change is written as an override into the open mod's
// own Locale folder, which is read before the engine's — the first definition of
// an id wins, so the override takes effect exactly as a modder's own file does.

// localeEditBase is the first id a string this tool adds gets.
//
// Measured on the English table: 81,548 ids, the largest 55,000,075, and
// nothing at or above 100,000,000 at all. A billion is therefore about
// eighteen times the highest id the game uses, which leaves a range no future
// engine string can reach into and no id of ours can collide with.
const localeEditBase = 1000000000

// localeEditFileName is the .ucs this tool writes into. The engine loads every
// .ucs in a module's Locale\<Language> folder by convention — a module needs no
// manifest entry for one — so a single well named file is enough, and putting
// the tool's strings in their own file keeps them apart from the modder's.
const localeEditFileName = "ModStudio.ucs"

// localeDirName is the folder inside a project that holds a mod's own strings.
const localeDirName = "Locale"

// allocateLocaleID returns an id that nothing in the loaded table holds.
//
// It always starts at localeEditBase and walks up, so a mod's strings get
// consecutive, predictable ids — 1000000000, 1000000001, and so on — rather
// than ids that depend on what some other mod happens to have used. The walk
// still checks the table, so an id that is somehow taken is stepped over rather
// than handed out twice.
//
// The id is reserved as it is handed out, not left to the write to record it.
// The table only learns of a string once it has been written and the table
// reloaded, and a second allocation can arrive before either has happened — a
// dialog left open, a write that failed, two presses in one frame. Without a
// reservation the second one would hand out the first's id, and the second
// string would quietly overwrite the first.
func allocateLocaleID() string {
	localeState.mu.Lock()
	start := int64(localeEditBase)
	if localeState.reservedID >= start {
		start = localeState.reservedID + 1
	}
	localeState.mu.Unlock()

	for id := start; id < int64(localeEditBase)+1000000; id++ {
		s := strconv.FormatInt(id, 10)
		if _, taken := localeLookup(s); taken {
			continue
		}
		localeState.mu.Lock()
		localeState.reservedID = id
		localeState.mu.Unlock()
		return s
	}
	return ""
}

// localeEditPath is the .ucs the tool writes for a language: the open mod's own
// project folder, Locale\<Language>\ModStudio.ucs. The project's folder is
// used rather than the installed module's because that is the one the user owns
// and can deploy from, and because it is the only one that is writable.
func localeEditPath(projectDir, lang string) (string, error) {
	projectDir = strings.TrimSpace(projectDir)
	lang = strings.TrimSpace(lang)
	if projectDir == "" {
		return "", fmt.Errorf("no mod is open, so there is nowhere to save a localisation")
	}
	if lang == "" {
		lang = localeDefault
	}
	dir := filepath.Join(projectDir, localeDirName, lang)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, localeEditFileName), nil
}

// decodeUCSDocument reads a .ucs into its lines, dropping the byte order mark
// and the line endings. It is the reading half of what writeUCSDocument does,
// and it exists apart from decodeUCS because a file being edited has to come
// back as lines it can be edited as — one entry per line, tab separated.
func decodeUCSDocument(data []byte) []string {
	text := trimBOM(decodeUCS(data))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// writeUCSDocument puts lines back as a .ucs: UTF-16LE with a byte order mark,
// one entry per line, CRLF between them and after the last. The mark matters
// as much as the encoding — without it the first id of the file comes out as
// one unprintable character glued to a number, which is the bug that eats the
// first entry of every table.
func writeUCSDocument(lines []string) []byte {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	out := make([]byte, 0, 2+len(b.String())*2)
	out = append(out, 0xFF, 0xFE) // UTF-16LE byte order mark
	for _, u := range utf16.Encode([]rune(ucsBOM + b.String())) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// ucsSanitiseText makes a string safe to put on a .ucs line. An entry is one
// line, so a newline in the text would silently become two entries and the
// second would have no id; a tab would split the entry the wrong way. Both are
// replaced with a space rather than dropped, so "a\nb" reads as "a b" instead
// of "ab".
func ucsSanitiseText(text string) string {
	replacer := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ", "\x00", "")
	return replacer.Replace(text)
}

// ucsSetEntry writes id and text into a .ucs, replacing the line if the id is
// already there and appending otherwise. It returns the lines written and the
// line number the entry ended up on, 1 based, which is what a caller shows the
// user.
func ucsSetEntry(lines []string, id, text string) ([]string, int) {
	entry := id + "\t" + ucsSanitiseText(text)
	for i, line := range lines {
		got, _, ok := strings.Cut(line, "\t")
		if ok && strings.TrimSpace(got) == id {
			out := append([]string{}, lines...)
			out[i] = entry
			return out, i + 1
		}
	}
	out := append(append([]string{}, lines...), entry)
	return out, len(out)
}

// ucsDeleteEntry drops an id from a .ucs, and reports whether it was there.
// Removing a string the mod added is the same operation as adding one, and
// leaving a dead entry behind is how a project folder fills with lies.
func ucsDeleteEntry(lines []string, id string) ([]string, bool) {
	for i, line := range lines {
		got, _, ok := strings.Cut(line, "\t")
		if ok && strings.TrimSpace(got) == id {
			out := append([]string{}, lines[:i]...)
			return append(out, lines[i+1:]...), true
		}
	}
	return lines, false
}

// readUCSDocument reads a .ucs from disk, treating a missing file as an empty
// one. That is deliberate: the first entry this tool writes has no file yet.
func readUCSDocument(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeUCSDocument(data), nil
}

// writeLocaleEntry sets id to text in the mod's own .ucs for the language,
// creating the file and its folders if they are not there yet, and returns the
// path it wrote and the line the entry is on.
func writeLocaleEntry(projectDir, lang, id, text string) (path string, line int, err error) {
	path, err = localeEditPath(projectDir, lang)
	if err != nil {
		return "", 0, err
	}
	lines, err := readUCSDocument(path)
	if err != nil {
		return "", 0, err
	}
	lines, line = ucsSetEntry(lines, id, text)
	if err := os.WriteFile(path, writeUCSDocument(lines), 0644); err != nil {
		return "", 0, err
	}
	return path, line, nil
}

// localeOverrideIsOurs reports whether an id's entry came from the mod's own
// .ucs rather than the engine's, which is what decides whether editing it
// changes a line in place or writes an override. Overwriting an engine string
// in the engine's file is not something this tool does, so an engine entry
// always becomes a new line in the mod's file.
func localeOverrideIsOurs(entry localeEntry) bool {
	if entry.File == "" {
		return false
	}
	return strings.EqualFold(filepath.Base(entry.File), localeEditFileName)
}
