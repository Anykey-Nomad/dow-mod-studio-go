package modtree

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ModFile holds parsed .module directives (DoW subset).
type ModFile struct {
	FilePath    string
	AppPath     string // dir containing .module, with trailing sep
	FileMapName string // base name without .module

	UIName      string
	Description string
	DllName     string
	ModFolder   string
	TextureFE   string
	TextureIcon string
	ModVersion  string

	DataFolders  map[int]string // DataFolder.N -> raw value
	Archives     map[int]string // ArchiveFile.N -> raw value (without forced .sga suffix handling)
	RequiredMods map[int]string // RequiredMod.N
	CompatMods   map[int]string // CompatableMod.N
}

// ResolveDynamics replaces Corsix dynamics (DupStrResolveDynamics / _DawnOfWarRemoveDynamics).
func ResolveDynamics(input, locale, extra string) string {
	// Input may contain %LOCALE%, %TEXTURE-LEVEL%, %SOUND-LEVEL%, %MODEL-LEVEL%
	// locale e.g. "English"
	out := input
	out = strings.ReplaceAll(out, "%LOCALE%", "Locale\\"+locale)
	out = strings.ReplaceAll(out, "%TEXTURE-LEVEL%", "Full")
	out = strings.ReplaceAll(out, "%SOUND-LEVEL%", "Full")
	out = strings.ReplaceAll(out, "%MODEL-LEVEL%", "High")
	if extra != "" {
		out += extra
	}
	return out
}

// ParseModule reads a .module file (DoW format, [global] section).
func ParseModule(path string) (*ModFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &ModFile{
		FilePath:     path,
		AppPath:      filepath.Dir(path) + string(os.PathSeparator),
		FileMapName:  strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		DataFolders:  make(map[int]string),
		Archives:     make(map[int]string),
		RequiredMods: make(map[int]string),
		CompatMods:   make(map[int]string),
	}

	scanner := bufio.NewScanner(f)
	// increase buffer for long lines
	const maxCap = 64 * 1024
	buf := make([]byte, 0, 4096)
	scanner.Buffer(buf, maxCap)

	bInGlobal := false
	seenSection := false

	for scanner.Scan() {
		line := scanner.Text()
		// strip comment at first ';' (original C++ does strchr ';'; comment may be ";;")
		if idx := strings.Index(line, ";"); idx >= 0 {
			line = line[:idx]
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		// Section header?
		if strings.HasPrefix(trim, "[") {
			rb := strings.Index(trim, "]")
			if rb > 0 {
				seenSection = true
				sec := strings.TrimSpace(trim[1:rb])
				if strings.EqualFold(sec, "global") {
					bInGlobal = true
				} else {
					// For CoH DataSource sections like [Data:common:0] we ignore for now.
					// Any non-global section ends global scope.
					bInGlobal = false
				}
				continue
			}
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		// If we haven't seen any section yet, treat as global (permissive, handles files without [global])
		if !bInGlobal && !seenSection {
			bInGlobal = true
		}
		if !bInGlobal {
			continue
		}
		keyRaw := strings.TrimSpace(line[:eq])
		valRaw := strings.TrimSpace(line[eq+1:])
		// key case-insensitive
		lk := strings.ToLower(keyRaw)
		switch {
		case lk == "uiname":
			m.UIName = valRaw
		case lk == "description":
			m.Description = valRaw
		case lk == "dllname":
			m.DllName = valRaw
		case lk == "modfolder":
			m.ModFolder = valRaw
		case lk == "texturefe":
			m.TextureFE = valRaw
		case lk == "textureicon":
			m.TextureIcon = valRaw
		case lk == "modversion":
			m.ModVersion = valRaw
		case strings.HasPrefix(lk, "datafolder."):
			numStr := keyRaw[len("DataFolder."):]
			if n, err := strconv.Atoi(strings.TrimSpace(numStr)); err == nil {
				if _, exists := m.DataFolders[n]; !exists {
					m.DataFolders[n] = valRaw
				}
			}
		case strings.HasPrefix(lk, "archivefile."):
			numStr := keyRaw[len("ArchiveFile."):]
			if n, err := strconv.Atoi(strings.TrimSpace(numStr)); err == nil {
				if _, exists := m.Archives[n]; !exists {
					// Original C++ for CModuleFile appends ".sga" at parse time.
					// We store raw as written; suffix handling done at load time.
					// To keep parity, we store valRaw as-is (may already contain .sga? usually not)
					m.Archives[n] = valRaw
				}
			}
		case strings.HasPrefix(lk, "requiredmod."):
			numStr := keyRaw[len("RequiredMod."):]
			if n, err := strconv.Atoi(strings.TrimSpace(numStr)); err == nil {
				if _, exists := m.RequiredMods[n]; !exists {
					m.RequiredMods[n] = valRaw
				}
			}
		case strings.HasPrefix(lk, "compatablemod."):
			numStr := keyRaw[len("CompatableMod."):]
			if n, err := strconv.Atoi(strings.TrimSpace(numStr)); err == nil {
				if _, exists := m.CompatMods[n]; !exists {
					m.CompatMods[n] = valRaw
				}
			}
		case strings.HasPrefix(lk, "compatiblemod."): // alternative spelling
			numStr := keyRaw[len("CompatibleMod."):]
			if n, err := strconv.Atoi(strings.TrimSpace(numStr)); err == nil {
				if _, exists := m.CompatMods[n]; !exists {
					m.CompatMods[n] = valRaw
				}
			}
		default:
			// ignore other keys (Playable, etc.)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// If no ModFolder explicitly, fallback to FileMapName? Not needed.
	return m, nil
}

// SortedKeys returns sorted int keys of a map.
func sortedKeysInt(m map[int]string) []int {
	ks := make([]int, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	return ks
}
