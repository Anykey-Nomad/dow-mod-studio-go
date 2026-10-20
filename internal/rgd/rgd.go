// Package rgd parses and writes Dawn of War RGD files: the key/value
// containers the game stores units, buildings and abilities in. It resolves
// hashed attribute names through the game dictionary and serializes edited
// trees back to the binary format.
package rgd

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ── RGD types ───────────────────────────────────────────────────────────────

const (
	RgdTypeFloat    uint32 = 0
	RgdTypeInt      uint32 = 1
	RgdTypeBool     uint32 = 2
	RgdTypeString   uint32 = 3
	RgdTypeWString  uint32 = 4
	RgdTypeTable    uint32 = 100
	RgdTypeTableInt uint32 = 101
	RgdTypeNoData   uint32 = 254
)

const RefHash uint32 = 0x49D60FAE // $REF

// RgdEntry represents one field in a RGD table (IMetaNode).
type RgdEntry struct {
	Hash uint32
	Name string // resolved name or "0xXXXXXXXX" if unknown
	Type uint32

	// scalar values (valid depending on Type)
	FloatVal float32
	IntVal   uint32
	BoolVal  bool
	StrVal   string // for DT_String
	WStrVal  string // for DT_WString (Go UTF-8)

	// table values
	Children []*RgdEntry
	Ref      *RgdEntry // $REF entry (hash 0x49D60FAE) if present
}

func (e *RgdEntry) IsTable() bool { return e.Type == RgdTypeTable || e.Type == RgdTypeTableInt }

// RefString returns the reference path of a table, if any.
func (e *RgdEntry) RefString() string {
	if e.Ref == nil {
		return ""
	}
	switch e.Ref.Type {
	case RgdTypeString:
		return e.Ref.StrVal
	case RgdTypeWString:
		return e.Ref.WStrVal
	default:
		return ""
	}
}

// DisplayValue returns a short string representation of the entry's value
// (used in the Table Children grid).
func (e *RgdEntry) DisplayValue() string {
	switch e.Type {
	case RgdTypeFloat:
		return strconv.FormatFloat(float64(e.FloatVal), 'g', -1, 32)
	case RgdTypeInt:
		return strconv.FormatUint(uint64(e.IntVal), 10)
	case RgdTypeBool:
		if e.BoolVal {
			return "True"
		}
		return "False"
	case RgdTypeString:
		return e.StrVal
	case RgdTypeWString:
		return e.WStrVal
	case RgdTypeTable, RgdTypeTableInt:
		if s := e.RefString(); s != "" {
			return s
		}
		return "{}"
	default:
		return ""
	}
}

// TypeLabel returns the human readable type as shown in Corsix:
// Number / CoH UCS Ref / True False / Text / DoW UCS Ref / Table ...
// We use simplified labels but keep compatible with the dropdown:
// 0->Float, 1->Integer, 2->Bool, 3->String, 4->WString, 100->Table
// DisplayFieldValue keeps numeric UCS aliases out of the visible field value.
// The original alias remains untouched in StrVal/WStrVal for editing and saving.
//
// The two string types treat "$number" completely differently, so they are
// handled apart here:
//
//   - DT_WString holds a *localisation* id. Measured over the real DXP3 tree,
//     these are things like "$90100" (screen_name_id) or "$705750" (text_01),
//     i.e. keys into the game's string table. Treating them as dictionary
//     aliases and falling back to the field name threw the id away and showed
//     "screen_name_id" where the id itself is the only useful thing there is.
//     The id is shown verbatim, and localised text is appended when the
//     localisation table can supply it.
//   - DT_String holds an RGD dictionary alias, where the number is a hash of
//     the referenced name. Those resolve through the dictionary, and fall back
//     to the field name when the hash is not in it.
func (e *RgdEntry) DisplayFieldValue() string {
	value := e.DisplayValue()
	if e.Type == RgdTypeWString {
		return LocIDDisplay(value)
	}
	if e.Type != RgdTypeString {
		return value
	}
	if name := AliasDisplayName(value, InitDict()); name != "" {
		return name
	}
	if IsNumericAlias(value) {
		return e.Name
	}
	return value
}

// Localize resolves a localisation id to text in the language the application
// currently shows, or "" when the id is unknown. The application wires it to
// its locale table; nil keeps ids rendering as bare ids, since the package
// itself carries no locale state.
var Localize func(id string) string

// LocIDDisplay renders a DT_WString value. A bare "$id" is a localisation id
// and is returned unchanged, because the id is the value; if the id also
// resolves to text, the text is appended so the string reads naturally without
// hiding the id. Anything that is not a plain id is returned as it is.
func LocIDDisplay(value string) string {
	id, ok := LocID(value)
	if !ok {
		return value
	}
	if Localize != nil {
		if text := Localize(id); text != "" {
			return value + "  " + text
		}
	}
	// The value is returned rather than the bare id: the "$" is how the file
	// writes it, and it is what tells a reader this is a reference.
	return value
}

// LocID splits a DT_WString value into its bare localisation id. It accepts
// "$90100" and reports the id as "90100"; "$0" is the game's "no string" marker
// and is reported as empty so callers can treat it as unset.
func LocID(value string) (string, bool) {
	if len(value) < 2 || value[0] != '$' {
		return "", false
	}
	digits := value[1:]
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", false
		}
	}
	return digits, true
}

// LocIDIsUnset reports whether a DT_WString is the game's empty marker.
// "$0" appears on every optional text slot: measured 115 of the race unit rgds
// carry an empty ability_01, and unused text_NN slots are all "$0".
func LocIDIsUnset(value string) bool {
	id, ok := LocID(value)
	return ok && id == "0"
}

func IsNumericAlias(value string) bool {
	if len(value) < 2 || value[0] != '$' {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 10, 64)
	return err == nil
}

func AliasDisplayName(value string, dict map[uint32]string) string {
	if !IsNumericAlias(value) || dict == nil {
		return ""
	}
	name := dict[Hash(value[1:])]
	if name == "" || IsNumericAlias(name) {
		return ""
	}
	return name
}

func (e *RgdEntry) TypeLabel() string {
	switch e.Type {
	case RgdTypeFloat:
		return "Float"
	case RgdTypeInt:
		return "Integer"
	case RgdTypeBool:
		return "Bool"
	case RgdTypeString:
		return "String"
	case RgdTypeWString:
		return "WString"
	case RgdTypeTable, RgdTypeTableInt:
		return "Table"
	default:
		return fmt.Sprintf("Unknown(%d)", e.Type)
	}
}

// RgdFile is the parsed DATAAEGD chunk (GameData).
type RgdFile struct {
	HeaderVersion  uint32
	HeaderUnknown3 uint32 // always 1
	HeaderUnknown4 uint32 // v3 only
	HeaderUnknown5 uint32
	HeaderUnknown6 uint32
	ChunkVersion   uint32
	ChunkUnknown1  uint32 // v3 only
	ChunkUnknown2  uint32
	Descriptor     string
	Root           *RgdEntry // Name == "GameData", Type == Table
	Warnings       []string
}

// ── Jenkins hash (Bob Jenkins lookup3, as in hash.c) ───────────────────────

func Hash(s string) uint32 {
	k := []byte(s)
	var a, b, c uint32
	a = 0x9e3779b9
	b = 0x9e3779b9
	c = 0
	origLen := uint32(len(k))
	length := origLen
	i := 0
	for int(length) >= 12 {
		a += uint32(k[i]) | uint32(k[i+1])<<8 | uint32(k[i+2])<<16 | uint32(k[i+3])<<24
		b += uint32(k[i+4]) | uint32(k[i+5])<<8 | uint32(k[i+6])<<16 | uint32(k[i+7])<<24
		c += uint32(k[i+8]) | uint32(k[i+9])<<8 | uint32(k[i+10])<<16 | uint32(k[i+11])<<24
		mix(&a, &b, &c)
		i += 12
		length -= 12
	}
	c += origLen
	switch length {
	case 11:
		c += uint32(k[i+10]) << 24
		fallthrough
	case 10:
		c += uint32(k[i+9]) << 16
		fallthrough
	case 9:
		c += uint32(k[i+8]) << 8
		fallthrough
	case 8:
		b += uint32(k[i+7]) << 24
		fallthrough
	case 7:
		b += uint32(k[i+6]) << 16
		fallthrough
	case 6:
		b += uint32(k[i+5]) << 8
		fallthrough
	case 5:
		b += uint32(k[i+4])
		fallthrough
	case 4:
		a += uint32(k[i+3]) << 24
		fallthrough
	case 3:
		a += uint32(k[i+2]) << 16
		fallthrough
	case 2:
		a += uint32(k[i+1]) << 8
		fallthrough
	case 1:
		a += uint32(k[i])
	}
	mix(&a, &b, &c)
	return c
}

func mix(a, b, c *uint32) {
	*a -= *b
	*a -= *c
	*a ^= *c >> 13
	*b -= *c
	*b -= *a
	*b ^= *a << 8
	*c -= *a
	*c -= *b
	*c ^= *b >> 13
	*a -= *b
	*a -= *c
	*a ^= *c >> 12
	*b -= *c
	*b -= *a
	*b ^= *a << 16
	*c -= *a
	*c -= *b
	*c ^= *b >> 5
	*a -= *b
	*a -= *c
	*a ^= *c >> 3
	*b -= *c
	*b -= *a
	*b ^= *a << 10
	*c -= *a
	*c -= *b
	*c ^= *b >> 15
}

// ── Dictionary ─────────────────────────────────────────────────────────────

var rgdDict map[uint32]string

// embeddedRGDDict is the game's own dictionary, shipped with the package so
// hashed field names resolve even where no installed copy has been found yet —
// the Linux build, the test binary, or a module opened before the game folder
// is known. A dictionary found on disk still wins, because that one can carry
// the installation's own custom.txt additions.
//
//go:embed RGD_DIC.TXT
var embeddedRGDDict []byte

func InitDict() map[uint32]string {
	if rgdDict != nil {
		return rgdDict
	}
	m := make(map[uint32]string, 7000)
	// pre-populate "1".."9999" as in CRgdHashTable ctor
	for i := 1; i < 10000; i++ {
		s := strconv.Itoa(i)
		h := Hash(s)
		if _, ok := m[h]; !ok {
			m[h] = s
		}
	}
	// also "0"
	if _, ok := m[Hash("0")]; !ok {
		m[Hash("0")] = "0"
	}
	// try to load dictionary files from candidate locations
	candidates := []string{}
	// exe dir
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "Mod_Studio_Files", "dictionaries", "RGD_DIC.TXT"),
			filepath.Join(filepath.Dir(exe), "Mod_studio_files", "dictionaries", "RGD_DIC.TXT"),
			filepath.Join(filepath.Dir(exe), "dictionaries", "RGD_DIC.TXT"),
		)
		// also eval symlink
		if eval, err := filepath.EvalSymlinks(exe); err == nil {
			candidates = append(candidates,
				filepath.Join(filepath.Dir(eval), "Mod_Studio_Files", "dictionaries", "RGD_DIC.TXT"),
			)
		}
	}
	// hardcoded DoW DE locations
	candidates = append(candidates,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/Mod_studio_files/dictionaries/RGD_DIC.TXT`,
		`C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/Mod_Studio_Files/dictionaries/RGD_DIC.TXT`,
		`./Mod_Studio_Files/dictionaries/RGD_DIC.TXT`,
		`./Mod_studio_files/dictionaries/RGD_DIC.TXT`,
		`./dictionaries/RGD_DIC.TXT`,
		`./RGD_DIC.TXT`,
	)
	// also project-local copy if present
	candidates = append(candidates, `C:/Users/anyke/Downloads/CDMSSrc_055/Rainman_src/RGD_DIC.TXT`)

	seen := map[string]bool{}
	loaded := false
	for _, p := range candidates {
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, err := os.Stat(p); err != nil {
			continue
		}
		loadRgdDictionaryFile(m, p)
		loaded = true
		// also load custom.txt in same folder
		dir := filepath.Dir(p)
		custom := filepath.Join(dir, "custom.txt")
		if _, err := os.Stat(custom); err == nil {
			loadRgdDictionaryFile(m, custom)
		}
		break // use first found
	}
	if !loaded {
		loadRgdDictionaryReader(m, bytes.NewReader(embeddedRGDDict))
	}
	rgdDict = m
	return m
}

// AddDictRoot loads the dictionary that ships with a game installation rooted
// at dir, if it has one. InitDict only finds the dictionary beside the
// executable or at the hard coded Windows install path, but the game can live
// anywhere — on Linux under a mount prefix — and the folder of the open module
// says exactly where it is. Entries already resolved keep their value, so a
// second copy of the same dictionary cannot disagree with the first.
func AddDictRoot(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	m := InitDict()
	for _, sub := range []string{
		filepath.Join(dir, "Mod_studio_files", "dictionaries", "RGD_DIC.TXT"),
		filepath.Join(dir, "Mod_Studio_Files", "dictionaries", "RGD_DIC.TXT"),
		filepath.Join(dir, "dictionaries", "RGD_DIC.TXT"),
	} {
		if _, err := os.Stat(sub); err != nil {
			continue
		}
		loadRgdDictionaryFile(m, sub)
		custom := filepath.Join(filepath.Dir(sub), "custom.txt")
		if _, err := os.Stat(custom); err == nil {
			loadRgdDictionaryFile(m, custom)
		}
		return
	}
}

func loadRgdDictionaryFile(m map[uint32]string, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	loadRgdDictionaryReader(m, f)
}

func loadRgdDictionaryReader(m map[uint32]string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	// increase buffer
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// strip \r
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#RGD_DIC") {
			continue
		}
		// handle "# 0x.... is an unknown value!!" -> treat as unknown (skip)
		if strings.HasPrefix(trim, "# 0x") && strings.Contains(trim, "is an unknown value") {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			continue
		}
		// cut comment after #
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		codePart := strings.TrimSpace(line[:eq])
		tokenPart := strings.TrimSpace(line[eq+1:])
		// token: remove spaces/tabs (original removes spaces inside token)
		// But tokens normally have no spaces, so we just strip spaces
		tokenPart = strings.ReplaceAll(tokenPart, " ", "")
		tokenPart = strings.ReplaceAll(tokenPart, "\t", "")
		if tokenPart == "" {
			continue
		}
		// codePart should be like 0x12345678 (case insensitive, spaces ignored)
		codePart = strings.ReplaceAll(codePart, " ", "")
		codePart = strings.ReplaceAll(codePart, "\t", "")
		if strings.HasPrefix(strings.ToLower(codePart), "0x") {
			hexStr := codePart[2:]
			if len(hexStr) != 8 {
				continue
			}
			val, err := strconv.ParseUint(hexStr, 16, 32)
			if err != nil {
				continue
			}
			h := uint32(val)
			if _, exists := m[h]; !exists {
				m[h] = tokenPart
			}
		}
	}
}

func resolveHashName(h uint32, dict map[uint32]string) string {
	if dict != nil {
		if s, ok := dict[h]; ok {
			return s
		}
	}
	// numeric fallback for TableInt-like hashes: try to synthesize? Already covered by prepopulated 0..9999
	// Otherwise hex
	return fmt.Sprintf("0x%08X", h)
}

// ── Parsing ────────────────────────────────────────────────────────────────

func ParseRgd(data []byte) (*RgdFile, error) {
	dict := InitDict()
	rf, err := parseRgdWithDict(data, dict)
	if err != nil {
		return nil, err
	}
	return rf, nil
}

func parseRgdWithDict(data []byte, dict map[uint32]string) (*RgdFile, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("rgd too small")
	}
	// header check: "Relic Chunky\x0D\x0A\x1A\x00"
	if string(data[0:12]) != "Relic Chunky" || data[12] != 0x0D || data[13] != 0x0A || data[14] != 0x1A || data[15] != 0x00 {
		// allow without trailing 0 as alternative
		if string(data[0:12]) != "Relic Chunky" {
			return nil, fmt.Errorf("not a Relic Chunky file (bad magic %q)", data[0:16])
		}
	}
	le := binary.LittleEndian
	pos := 16
	if pos+4 > len(data) {
		return nil, fmt.Errorf("truncated header version")
	}
	hVer := le.Uint32(data[pos:])
	pos += 4
	if hVer != 1 && hVer != 3 {
		return nil, fmt.Errorf("unsupported chunky version %d", hVer)
	}
	if pos+4 > len(data) {
		return nil, fmt.Errorf("truncated header unknown3")
	}
	hUnknown3 := le.Uint32(data[pos:])
	pos += 4
	var hUnknown4, hUnknown5, hUnknown6 uint32
	if hVer == 3 {
		if pos+12 > len(data) {
			return nil, fmt.Errorf("truncated header v3")
		}
		hUnknown4 = le.Uint32(data[pos:])
		hUnknown5 = le.Uint32(data[pos+4:])
		hUnknown6 = le.Uint32(data[pos+8:])
		pos += 12
	}
	// iterate chunks
	var descriptor string
	var chunkData []byte
	var chunkVer uint32
	var chunkUnknown1, chunkUnknown2 uint32
	found := false
	for pos+8 <= len(data) {
		chunkType := string(data[pos : pos+8])
		pos += 8
		if pos+4 > len(data) {
			break
		}
		cVer := le.Uint32(data[pos:])
		pos += 4
		if pos+4 > len(data) {
			break
		}
		chunkLen := le.Uint32(data[pos:])
		_ = chunkLen
		pos += 4
		if pos+4 > len(data) {
			break
		}
		strLen := le.Uint32(data[pos:])
		pos += 4
		if pos+int(strLen) > len(data) {
			break
		}
		sStr := string(data[pos : pos+int(strLen)])
		pos += int(strLen)
		if hVer == 3 {
			if pos+8 > len(data) {
				break
			}
			chunkUnknown1 = le.Uint32(data[pos:])
			chunkUnknown2 = le.Uint32(data[pos+4:])
			pos += 8
		}
		if pos+8 > len(data) {
			break
		}
		crc := le.Uint32(data[pos:])
		pos += 4
		dataLen := le.Uint32(data[pos:])
		pos += 4
		if pos+int(dataLen) > len(data) {
			break
		}
		cData := data[pos : pos+int(dataLen)]
		pos += int(dataLen)
		if chunkType == "DATAAEGD" && !found {
			descriptor = sStr
			chunkVer = cVer
			chunkData = cData
			// optional CRC check
			if crc != 0 {
				calc := crc32.Checksum(cData, crc32.MakeTable(crc32.IEEE))
				// crc32 in file uses IEEE via zlib? Original uses crc32(0L, Z_NULL, 0) then crc32(crc, data)
				// That's same as IEEE.
				if calc != crc {
					// not fatal, as per Corsix comment about retarded tools with invalid CRCs, ignore
				}
			}
			found = true
			// don't break? There may be only one DATAAEGD, but we stop at first
			break
		} else {
			// ignore other chunks
		}
	}
	if !found {
		return nil, fmt.Errorf("no DATAAEGD chunk found")
	}
	// parse chunkData as table
	children, ref, err := parseRgdTable(chunkData, 0, dict)
	if err != nil {
		return nil, fmt.Errorf("parse DATAAEGD: %w", err)
	}
	root := &RgdEntry{
		Hash:     0,
		Name:     "GameData",
		Type:     RgdTypeTable,
		Children: children,
		Ref:      ref,
	}
	rf := &RgdFile{
		HeaderVersion:  hVer,
		HeaderUnknown3: hUnknown3,
		HeaderUnknown4: hUnknown4,
		HeaderUnknown5: hUnknown5,
		HeaderUnknown6: hUnknown6,
		ChunkVersion:   chunkVer,
		ChunkUnknown1:  chunkUnknown1,
		ChunkUnknown2:  chunkUnknown2,
		Descriptor:     descriptor,
		Root:           root,
	}
	if rf.HeaderUnknown3 == 0 {
		rf.HeaderUnknown3 = 1
	}
	return rf, nil
}

// parseRgdTable parses a table located at base offset within data.
// data is the chunk's pData slice (entire DATAAEGD data length).
func parseRgdTable(data []byte, base int, dict map[uint32]string) ([]*RgdEntry, *RgdEntry, error) {
	le := binary.LittleEndian
	if base+4 > len(data) {
		return nil, nil, fmt.Errorf("table at %d: out of bounds for keyCount", base)
	}
	keyCount := le.Uint32(data[base : base+4])
	// sanity: if keyCount absurdly large, error
	if keyCount > 50000 {
		return nil, nil, fmt.Errorf("implausible keyCount %d at %d", keyCount, base)
	}
	headersStart := base + 4
	dataStart := headersStart + int(keyCount)*12
	if dataStart > len(data) {
		return nil, nil, fmt.Errorf("table headers exceed data")
	}
	type hdr struct {
		hash uint32
		typ  uint32
		off  uint32
	}
	headers := make([]hdr, keyCount)
	for i := 0; i < int(keyCount); i++ {
		off := headersStart + i*12
		if off+12 > len(data) {
			return nil, nil, fmt.Errorf("header %d out of bounds", i)
		}
		headers[i].hash = le.Uint32(data[off:])
		headers[i].typ = le.Uint32(data[off+4:])
		headers[i].off = le.Uint32(data[off+8:])
		if headers[i].typ != RgdTypeFloat && headers[i].typ != RgdTypeInt && headers[i].typ != RgdTypeBool && headers[i].typ != RgdTypeString && headers[i].typ != RgdTypeWString && headers[i].typ != RgdTypeTable && headers[i].typ != RgdTypeTableInt && headers[i].typ != RgdTypeNoData {
			return nil, nil, fmt.Errorf("unknown type %d for hash %08x", headers[i].typ, headers[i].hash)
		}
	}
	var children []*RgdEntry
	var ref *RgdEntry
	for _, h := range headers {
		name := resolveHashName(h.hash, dict)
		// For unknown numeric table children, try to synthesize? Already handled via dict prepopulate.
		entry := &RgdEntry{Hash: h.hash, Name: name, Type: h.typ}
		valPos := dataStart + int(h.off)
		if valPos < 0 || valPos > len(data) {
			return nil, nil, fmt.Errorf("offset %d for %s out of bounds", h.off, name)
		}
		switch h.typ {
		case RgdTypeFloat:
			if valPos+4 > len(data) {
				return nil, nil, fmt.Errorf("float oob")
			}
			bits := le.Uint32(data[valPos:])
			entry.FloatVal = math.Float32frombits(bits)
		case RgdTypeInt:
			if valPos+4 > len(data) {
				return nil, nil, fmt.Errorf("int oob")
			}
			entry.IntVal = le.Uint32(data[valPos:])
		case RgdTypeBool:
			if valPos+1 > len(data) {
				return nil, nil, fmt.Errorf("bool oob")
			}
			entry.BoolVal = data[valPos] != 0
		case RgdTypeString:
			end := valPos
			for end < len(data) && data[end] != 0 {
				end++
			}
			if end >= len(data) {
				return nil, nil, fmt.Errorf("unterminated string for %s", name)
			}
			entry.StrVal = string(data[valPos:end])
		case RgdTypeWString:
			// read uint16 LE until 0
			end := valPos
			var u16 []uint16
			for end+1 < len(data) {
				v := le.Uint16(data[end:])
				if v == 0 {
					break
				}
				u16 = append(u16, v)
				end += 2
			}
			// check terminator present
			if end+1 >= len(data) && len(u16) > 0 {
				// may be missing null? treat as is
			}
			// decode UTF-16 to Go string
			if len(u16) > 0 {
				runes := utf16.Decode(u16)
				entry.WStrVal = string(runes)
			} else {
				entry.WStrVal = ""
			}
		case RgdTypeTable, RgdTypeTableInt:
			subChildren, subRef, err := parseRgdTable(data, valPos, dict)
			if err != nil {
				return nil, nil, fmt.Errorf("parse subtable %s: %w", name, err)
			}
			entry.Children = subChildren
			entry.Ref = subRef
			if h.typ == RgdTypeTableInt {
				entry.Type = RgdTypeTable
			}
		case RgdTypeNoData:
			// no value
		}
		if h.hash == RefHash {
			// this entry is the table's reference string (pExt)
			ref = entry
		} else {
			children = append(children, entry)
		}
	}
	sortRgdEntries(children)
	return children, ref, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ── Serialization (mirrors CRgdFile::Save / _WriteRawRgdData) ────────────────

func (rf *RgdFile) MarshalBinary() ([]byte, error) {
	if rf == nil || rf.Root == nil {
		return nil, fmt.Errorf("nil rgd")
	}
	hVer := rf.HeaderVersion
	if hVer == 0 {
		hVer = 1
	}
	hU3 := rf.HeaderUnknown3
	if hU3 == 0 {
		hU3 = 1
	}
	// Build DATAAEGD pData via _WriteRawRgdData equivalent
	pData, err := marshalRgdTable(rf.Root, false)
	if err != nil {
		return nil, err
	}
	crc := crc32.Checksum(pData, crc32.MakeTable(crc32.IEEE))
	// Chunk wrapper
	le := binary.LittleEndian
	chunkVer := rf.ChunkVersion
	if chunkVer == 0 {
		chunkVer = 1
	}
	desc := rf.Descriptor
	// Compute chunk length per C++: iStringLength + sizeof(CRC)+sizeof(DataLength)+iDataLength plus unknowns for v3
	chunkLen := uint32(len(desc)) + 8 + uint32(len(pData))
	// iChunkLength field stored but not critical; replicate C++: iStringLength + sizeof(CRC)+sizeof(DataLen)+iDataLen
	// (C++ does iStringLength + 8 + iDataLength; same).
	_ = chunkLen
	// Assemble file
	var out []byte
	header := make([]byte, 0, 16+4+4+12+8+4+4+4+4+4+len(desc)+8+4+4+len(pData))
	// magic
	header = append(header, []byte("Relic Chunky\x0D\x0A\x1A\x00")...)
	tmp4 := make([]byte, 4)
	le.PutUint32(tmp4, hVer)
	header = append(header, tmp4...)
	le.PutUint32(tmp4, hU3)
	header = append(header, tmp4...)
	if hVer == 3 {
		le.PutUint32(tmp4, rf.HeaderUnknown4)
		header = append(header, tmp4...)
		le.PutUint32(tmp4, rf.HeaderUnknown5)
		header = append(header, tmp4...)
		le.PutUint32(tmp4, rf.HeaderUnknown6)
		header = append(header, tmp4...)
	}
	out = append(out, header...)
	// DATAAEGD chunk
	out = append(out, []byte("DATAAEGD")...)
	le.PutUint32(tmp4, chunkVer)
	out = append(out, tmp4...)
	// iChunkLength
	cLen := uint32(len(desc)) + 8 + uint32(len(pData))
	// For v3, chunk includes two unknowns, accounted differently in C++?
	// C++ sets iChunkLength = iStringLength + sizeof(CRC)+sizeof(DataLength)+iDataLength (ignores unknowns)
	// but then writes unknowns before CRC. We keep same field value as C++.
	le.PutUint32(tmp4, cLen)
	out = append(out, tmp4...)
	le.PutUint32(tmp4, uint32(len(desc)))
	out = append(out, tmp4...)
	out = append(out, []byte(desc)...)
	if hVer == 3 {
		// chunk unknowns: default match New() if zero and hVer==3?
		u1 := rf.ChunkUnknown1
		u2 := rf.ChunkUnknown2
		// C++ New sets 0xffffffff / 0 for v3 chunk; keep caller values unless both zero then use those defaults
		if u1 == 0 && u2 == 0 {
			u1 = 0xffffffff
			u2 = 0
		}
		le.PutUint32(tmp4, u1)
		out = append(out, tmp4...)
		le.PutUint32(tmp4, u2)
		out = append(out, tmp4...)
	}
	le.PutUint32(tmp4, crc)
	out = append(out, tmp4...)
	le.PutUint32(tmp4, uint32(len(pData)))
	out = append(out, tmp4...)
	out = append(out, pData...)
	return out, nil
}

// Marshal is alias for MarshalBinary for convenience.
func (rf *RgdFile) Marshal() ([]byte, error) { return rf.MarshalBinary() }

// marshalRgdTable serializes one table (GameData or subtable). bTable101 indicates the parent
// wants numeric sort (sk_TableInt). This mirrors _WriteRawRgdData exactly including padding,
// TableInt detection, and recursion.
func marshalRgdTable(tbl *RgdEntry, bTable101 bool) ([]byte, error) {
	if tbl == nil || (tbl.Type != RgdTypeTable && tbl.Type != RgdTypeTableInt) {
		return nil, fmt.Errorf("not a table")
	}
	// Gather entries: children + Ref (pExt) if present, excluding NoData (none stored in our model)
	var vEntries []*RgdEntry
	for _, ch := range tbl.Children {
		if ch.Type == RgdTypeNoData {
			continue
		}
		vEntries = append(vEntries, ch)
	}
	if tbl.Ref != nil && tbl.Ref.Type != RgdTypeNoData {
		vEntries = append(vEntries, tbl.Ref)
	}
	// Sort
	if bTable101 {
		sort.Slice(vEntries, func(a, b int) bool {
			av := vEntries[a].Name
			bv := vEntries[b].Name
			ai, errA := strconv.Atoi(av)
			bi, errB := strconv.Atoi(bv)
			if errA == nil && errB == nil {
				return ai < bi
			}
			if errA == nil && errB != nil {
				return true
			}
			if errA != nil && errB == nil {
				return false
			}
			// fallback to string compare
			return strings.ToLower(av) < strings.ToLower(bv)
		})
	} else {
		// Use same sort as parsing: hash compare for 0x..., else trailing _number, else case-insensitive
		sortRgdEntries(vEntries)
	}
	keyCount := len(vEntries)
	buf := make([]byte, 0, 4+12*keyCount+256)
	tmp4 := make([]byte, 4)
	le := binary.LittleEndian
	le.PutUint32(tmp4, uint32(keyCount))
	buf = append(buf, tmp4...)
	headerStart := 4
	buf = append(buf, make([]byte, 12*keyCount)...)
	dataOffset := 4 + 12*keyCount
	iDataLen := 0
	for idx, e := range vEntries {
		iPad := 1
		switch e.Type {
		case RgdTypeFloat:
			iPad = 4
		case RgdTypeInt:
			iPad = 4
		case RgdTypeWString:
			iPad = 2
		case RgdTypeTable, RgdTypeTableInt:
			iPad = 4
		default:
			iPad = 1
		}
		pad := 0
		if iPad > 1 && iDataLen%iPad != 0 {
			pad = iPad - (iDataLen % iPad)
		}
		writtenType := e.Type
		bNumericChild := false
		if e.Type == RgdTypeTable || e.Type == RgdTypeTableInt {
			allNumeric := true
			if e.Ref != nil {
				allNumeric = false
			} else if len(e.Children) == 0 {
				allNumeric = false
			} else {
				for _, ch := range e.Children {
					if !isDigits(ch.Name) {
						allNumeric = false
						break
					}
				}
			}
			if allNumeric {
				writtenType = RgdTypeTableInt
				bNumericChild = true
			}
		}
		// Ensure buf length for pad
		for len(buf) < dataOffset+iDataLen+pad {
			buf = append(buf, 0)
		}
		iDataLen += pad
		// Write header at idx
		offPos := headerStart + idx*12
		le.PutUint32(buf[offPos:], e.Hash)
		le.PutUint32(buf[offPos+4:], writtenType)
		le.PutUint32(buf[offPos+8:], uint32(iDataLen))
		// Encode data at dataOffset+iDataLen
		switch e.Type {
		case RgdTypeFloat:
			bits := math.Float32bits(e.FloatVal)
			for len(buf) < dataOffset+iDataLen+4 {
				buf = append(buf, 0)
			}
			le.PutUint32(buf[dataOffset+iDataLen:], bits)
			iDataLen += 4
		case RgdTypeInt:
			for len(buf) < dataOffset+iDataLen+4 {
				buf = append(buf, 0)
			}
			le.PutUint32(buf[dataOffset+iDataLen:], e.IntVal)
			iDataLen += 4
		case RgdTypeBool:
			for len(buf) < dataOffset+iDataLen+1 {
				buf = append(buf, 0)
			}
			if e.BoolVal {
				buf[dataOffset+iDataLen] = 1
			} else {
				buf[dataOffset+iDataLen] = 0
			}
			iDataLen += 1
		case RgdTypeString:
			s := e.StrVal
			need := len(s) + 1
			for len(buf) < dataOffset+iDataLen+need {
				buf = append(buf, 0)
			}
			copy(buf[dataOffset+iDataLen:], s)
			buf[dataOffset+iDataLen+len(s)] = 0
			iDataLen += need
		case RgdTypeWString:
			u16 := utf16.Encode([]rune(e.WStrVal))
			need := (len(u16) + 1) * 2
			for len(buf) < dataOffset+iDataLen+need {
				buf = append(buf, 0)
			}
			for i, v := range u16 {
				le.PutUint16(buf[dataOffset+iDataLen+i*2:], v)
			}
			le.PutUint16(buf[dataOffset+iDataLen+len(u16)*2:], 0)
			iDataLen += need
		case RgdTypeTable, RgdTypeTableInt:
			sub, err := marshalRgdTable(e, bNumericChild)
			if err != nil {
				return nil, err
			}
			for len(buf) < dataOffset+iDataLen+len(sub) {
				buf = append(buf, 0)
			}
			copy(buf[dataOffset+iDataLen:], sub)
			iDataLen += len(sub)
		default:
			return nil, fmt.Errorf("unsupported type %d for %q", e.Type, e.Name)
		}
	}
	return buf, nil
}

func sortRgdEntries(list []*RgdEntry) {
	sort.Slice(list, func(a, b int) bool {
		pa := list[a]
		pb := list[b]
		// if either name is hex placeholder (starts with 0x), compare hashes? Original compares hash if sName==0
		// For our placeholder we can detect "0x" prefix means unknown -> compare hash
		aIsHex := strings.HasPrefix(pa.Name, "0x")
		bIsHex := strings.HasPrefix(pb.Name, "0x")
		if aIsHex || bIsHex {
			if aIsHex && !bIsHex {
				return true
			}
			if !aIsHex && bIsHex {
				return false
			}
			return pa.Hash < pb.Hash
		}
		// handle trailing _number
		ai := strings.LastIndex(pa.Name, "_")
		bi := strings.LastIndex(pb.Name, "_")
		aHasNum := false
		bHasNum := false
		var aBase, bBase string
		var aNum, bNum int
		if ai >= 0 {
			suf := pa.Name[ai+1:]
			if isDigits(suf) {
				aHasNum = true
				aBase = pa.Name[:ai]
				aNum, _ = strconv.Atoi(suf)
			}
		}
		if bi >= 0 {
			suf := pb.Name[bi+1:]
			if isDigits(suf) {
				bHasNum = true
				bBase = pb.Name[:bi]
				bNum, _ = strconv.Atoi(suf)
			}
		}
		if aHasNum && bHasNum {
			if strings.EqualFold(aBase, bBase) {
				return aNum < bNum
			}
			return strings.ToLower(aBase) < strings.ToLower(bBase)
		}
		return strings.ToLower(pa.Name) < strings.ToLower(pb.Name)
	})
}
