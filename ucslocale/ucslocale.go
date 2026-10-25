// Package ucslocale reads the localisation tables of Dawn of War and turns
// the ids that .rgd files carry into readable text.
//
// The game keeps its strings in .ucs files: one folder per language, one file
// per module, one line per entry:
//
//	<game>/Engine/Locale/<Language>/Engine.ucs
//	<game>/<Module>/Locale/<Language>/<Something>.ucs
//
// A line is an id, a tab and the text: "90020<TAB>Chaos". The files are
// UTF-16LE with a byte order mark, though a file saved by a modding tool
// comes out as plain UTF-8, so both are read.
//
// The ids are the value in the data files: race blueprints carry
// race_details/name_id = "$90020", and that id is what the game shows as the
// race's name. Reading the tables is therefore the way to get a name for a
// race, a squad or an ability.
package ucslocale

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

// DefaultLang — язык, который читается по умолчанию.
const DefaultLang = "English"

// Entry — одна запись таблицы: текст и место, откуда он взят.
type Entry struct {
	// Text — сам текст.
	Text string
	// File — файл .ucs, в котором текст найден.
	File string
	// Line — номер строки в файле (с единицы).
	Line int
}

// Table — индекс идентификаторов одного языка.
type Table struct {
	// Lang — язык таблицы.
	Lang string
	// Index — тексты по идентификаторам.
	Index map[string]Entry
	// Files — сколько файлов .ucs прочитано.
	Files int
	// Lines — сколько записей попало в индекс.
	Lines int
}

// cache хранит уже прочитанные таблицы: язык меняют редко, а таблица на
// полмиллиона строк читается не мгновенно.
var cache struct {
	mu    sync.Mutex
	langs map[string]*Table
}

// Load читает таблицы языка lang. gamePath — папка установленной игры,
// moduleDir — папка модуля, строки которого читаются первыми; пустая строка
// означает «искать во всех папках установки».
//
// Таблица кэшируется: повторный вызов с тем же языком ничего не читает.
func Load(gamePath, moduleDir, lang string) *Table {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		lang = DefaultLang
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.langs == nil {
		cache.langs = map[string]*Table{}
	}
	if t, ok := cache.langs[lang]; ok {
		return t
	}
	table := build(gamePath, moduleDir, lang)
	cache.langs[lang] = table
	return table
}

// build читает все .ucs языка и строит индекс.
func build(gamePath, moduleDir, lang string) *Table {
	table := &Table{Lang: lang, Index: map[string]Entry{}}
	for _, file := range SourceFiles(gamePath, moduleDir, lang) {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		table.Lines += Parse(data, file, table.Index)
	}
	table.Files = len(SourceFiles(gamePath, moduleDir, lang))
	return table
}

// SourceFiles перечисляет файлы .ucs языка в порядке чтения: сначала папка
// модуля, потом все папки установленной игры. Первый файл, задавший
// идентификатор, побеждает — так модуль может переопределить строку движка.
func SourceFiles(gamePath, moduleDir, lang string) []string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return nil
	}
	var files []string
	for _, root := range Roots(gamePath, moduleDir) {
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

// Roots перечисляет папки, в которых ищутся таблицы: папка модуля, затем все
// папки установленной игры (движок лежит среди них).
func Roots(gamePath, moduleDir string) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		dir = filepath.Clean(dir)
		key := strings.ToLower(dir)
		if seen[key] {
			return
		}
		seen[key] = true
		roots = append(roots, dir)
	}
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

// Parse разбирает один файл .ucs и добавляет его записи в индекс. Возвращает
// число добавленных записей.
//
// Записью считается строка, начинающаяся с цифр и табуляции. Остальное
// пропускается: таблицы правят руками, и пустая строка не должна быть
// ошибкой.
func Parse(data []byte, file string, index map[string]Entry) int {
	text := TrimBOM(Decode(data))
	added := 0
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		id := line[:tab]
		if !IsID(id) {
			continue
		}
		if _, seen := index[id]; seen {
			continue
		}
		index[id] = Entry{Text: line[tab+1:], File: file, Line: i + 1}
		added++
	}
	return added
}

// Lookup возвращает запись по идентификатору.
func (t *Table) Lookup(id string) (Entry, bool) {
	if t == nil || id == "" {
		return Entry{}, false
	}
	entry, ok := t.Index[id]
	return entry, ok
}

// Text возвращает текст по идентификатору или пустую строку.
func (t *Table) Text(id string) string {
	entry, ok := t.Lookup(id)
	if !ok {
		return ""
	}
	return entry.Text
}

// IsID проверяет, что строка — идентификатор: только цифры.
func IsID(id string) bool {
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

// Decode превращает содержимое .ucs в UTF-8. Файлы игры записаны в UTF-16LE
// с меткой порядка байтов; файл без метки считается уже UTF-8, что и
// получается у таблиц, сохранённых сторонним редактором.
func Decode(data []byte) string {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16(data[2:], false)
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16(data[2:], true)
	}
	return string(data)
}

// decodeUTF16 разбирает последовательность UTF-16.
func decodeUTF16(b []byte, bigEndian bool) string {
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

// TrimBOM убирает метку порядка байтов в начале текста: без неё первый
// идентификатор склеился бы с символом U+FEFF.
func TrimBOM(s string) string {
	return strings.TrimPrefix(s, string(rune(0xFEFF)))
}
