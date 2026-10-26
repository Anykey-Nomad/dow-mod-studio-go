package sgapack

import (
	"bytes"
	"compress/zlib"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SgaPackDir struct {
	Name     string
	Files    []string
	Children []*SgaPackDir
}

type SgaPackFile struct {
	Name       string
	DiskPath   string
	Data       []byte
	Compressed []byte
	Flags      uint16
	Offset     uint32
}

func PutU16(dst []byte, off int, v uint16) { binary.LittleEndian.PutUint16(dst[off:off+2], v) }
func PutU32(dst []byte, off int, v uint32) { binary.LittleEndian.PutUint32(dst[off:off+4], v) }

func WriteU16(w io.Writer, v uint16) { _ = binary.Write(w, binary.LittleEndian, v) }
func WriteU32(w io.Writer, v uint32) { _ = binary.Write(w, binary.LittleEndian, v) }

// writeSGA2 writes a small Relic SGA v2 archive with a single Data ToC.
// The layout follows CSgaCreator.cpp from the CDMS/Rainman source tree.
func WriteSGA2(output string, Files map[string]string, tocName string) error {
	if len(Files) > 65535 {
		return fmt.Errorf("too many files for SGA v2: %d", len(Files))
	}
	paths := make([]string, 0, len(Files))
	for virtualPath := range Files {
		paths = append(paths, virtualPath)
	}
	sort.Slice(paths, func(i, j int) bool { return strings.ToLower(paths[i]) < strings.ToLower(paths[j]) })

	root := &SgaPackDir{}
	byPath := map[string]*SgaPackDir{"": root}
	for _, virtualPath := range paths {
		rel := strings.TrimPrefix(strings.ReplaceAll(virtualPath, "/", "\\"), "Data\\")
		parts := strings.Split(rel, "\\")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		dir := root
		for i := 0; i < len(parts)-1; i++ {
			key := strings.Join(parts[:i+1], "\\")
			child := byPath[key]
			if child == nil {
				child = &SgaPackDir{Name: key}
				byPath[key] = child
				dir.Children = append(dir.Children, child)
			}
			dir = child
		}
		dir.Files = append(dir.Files, rel)
	}
	for _, dir := range byPath {
		sort.Slice(dir.Children, func(i, j int) bool {
			return strings.ToLower(dir.Children[i].Name) < strings.ToLower(dir.Children[j].Name)
		})
		sort.Slice(dir.Files, func(i, j int) bool { return strings.ToLower(dir.Files[i]) < strings.ToLower(dir.Files[j]) })
	}
	// SGA directory ranges describe a contiguous child range, so keep the
	// directory table in depth-first order (Corsix/Rainman SGAv2 layout).
	dirs := make([]*SgaPackDir, 0, len(byPath))
	var appendDir func(*SgaPackDir)
	appendDir = func(dir *SgaPackDir) {
		dirs = append(dirs, dir)
		for _, child := range dir.Children {
			appendDir(child)
		}
	}
	appendDir(root)
	packFiles := make([]SgaPackFile, 0, len(paths))
	for _, dir := range dirs {
		for _, rel := range dir.Files {
			virtualPath := "Data\\" + rel
			DiskPath, ok := Files[virtualPath]
			if !ok {
				return fmt.Errorf("missing packed file %s", virtualPath)
			}
			Data, err := os.ReadFile(DiskPath)
			if err != nil {
				return err
			}
			Compressed, err := CompressSGAData(Data)
			if err != nil {
				return err
			}
			Flags := uint16(0)
			if len(Compressed) < len(Data) {
				if len(Data) < 4096 {
					Flags = 0x20
				} else {
					Flags = 0x10
				}
			} else {
				Compressed = Data
			}
			packFiles = append(packFiles, SgaPackFile{Name: rel, DiskPath: DiskPath, Data: Data, Compressed: Compressed, Flags: Flags})
		}
	}
	if len(packFiles) > 65535 || len(dirs) > 65535 {
		return fmt.Errorf("SGA v2 table exceeds 16-bit limits")
	}

	const (
		tocBytes        = 140
		dirBytes        = 12
		fileBytes       = 20
		headerInfoBytes = 24
	)
	dirOffset := headerInfoBytes + tocBytes
	fileOffset := dirOffset + dirBytes*len(dirs)
	itemOffset := fileOffset + fileBytes*len(packFiles)
	nameBytes := 0
	for _, dir := range dirs {
		nameBytes += len(dir.Name) + 1
	}
	for _, file := range packFiles {
		nameBytes += len(file.Name) + 1
	}
	header := make([]byte, itemOffset+nameBytes)
	PutU32(header, 0, 24)
	PutU16(header, 4, 1)
	PutU32(header, 6, uint32(dirOffset))
	PutU16(header, 10, uint16(len(dirs)))
	PutU32(header, 12, uint32(fileOffset))
	PutU16(header, 16, uint16(len(packFiles)))
	PutU32(header, 18, uint32(itemOffset))
	PutU16(header, 22, uint16(len(packFiles)+len(dirs)))

	tocBase := 24
	copy(header[tocBase:tocBase+64], []byte(strings.ToLower(tocName)))
	copy(header[tocBase+64:tocBase+128], []byte("created by dow-mod-studio-go"))
	PutU16(header, tocBase+128, 0)
	PutU16(header, tocBase+130, uint16(len(dirs)))
	PutU16(header, tocBase+132, 0)
	PutU16(header, tocBase+134, uint16(len(packFiles)))
	PutU16(header, tocBase+136, 0)
	PutU16(header, tocBase+138, 0)

	dirNameOffsets := make([]uint32, len(dirs))
	fileNameOffsets := make([]uint32, len(packFiles))
	namePos := uint32(itemOffset)
	for i, dir := range dirs {
		dirNameOffsets[i] = namePos - uint32(itemOffset)
		namePos += uint32(len(dir.Name) + 1)
	}
	for i, file := range packFiles {
		fileNameOffsets[i] = namePos - uint32(itemOffset)
		namePos += uint32(len(file.Name) + 1)
	}
	fileIndex := 0
	for i, dir := range dirs {
		base := dirOffset + dirBytes*i
		PutU32(header, base, dirNameOffsets[i])
		PutU16(header, base+4, uint16(i+1))
		PutU16(header, base+6, uint16(i+1+len(dir.Children)))
		fileStart := fileIndex
		PutU16(header, base+8, uint16(fileStart))
		fileIndex += len(dir.Files)
		PutU16(header, base+10, uint16(fileIndex))
	}
	var dataOffset uint32
	for i := range packFiles {
		dataOffset = 0
		if i > 0 {
			dataOffset = packFiles[i-1].Offset + uint32(len(packFiles[i-1].Compressed))
		}
		packFiles[i].Offset = dataOffset
		base := fileOffset + fileBytes*i
		PutU32(header, base, fileNameOffsets[i])
		PutU16(header, base+4, packFiles[i].Flags)
		PutU32(header, base+8, packFiles[i].Offset)
		PutU32(header, base+12, uint32(len(packFiles[i].Compressed)))
		PutU32(header, base+16, uint32(len(packFiles[i].Data)))
	}
	for i, dir := range dirs {
		copy(header[itemOffset+int(dirNameOffsets[i]):], dir.Name)
		header[itemOffset+int(dirNameOffsets[i])+len(dir.Name)] = 0
	}
	for i, file := range packFiles {
		copy(header[itemOffset+int(fileNameOffsets[i]):], file.Name)
		header[itemOffset+int(fileNameOffsets[i])+len(file.Name)] = 0
	}

	var out bytes.Buffer
	out.WriteString("_ARCHIVE")
	WriteU32(&out, 2)
	out.Write(make([]byte, 16)) // header MD5 placeholder
	WriteUTF16Fixed(&out, "Made with dow-mod-studio-go", 64)
	out.Write(make([]byte, 16)) // Data MD5 placeholder
	WriteU32(&out, uint32(len(header)))
	WriteU32(&out, 180+uint32(len(header)))
	out.Write(header)
	for _, file := range packFiles {
		out.Write(file.Compressed)
	}
	bytesOut := out.Bytes()
	headerKey := []byte("DFC9AF62-FC1B-4180-BC27-11CCE87D3EFF")
	headerDigest := md5.Sum(append(append([]byte{}, headerKey...), header...))
	copy(bytesOut[156:172], headerDigest[:])
	dataKey := []byte("E01519D6-2DB7-4640-AF54-0A23319C56C3")
	dataDigest := md5.Sum(append(append([]byte{}, dataKey...), bytesOut[180:]...))
	copy(bytesOut[12:28], dataDigest[:])
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	return os.WriteFile(output, bytesOut, 0644)
}

func WriteUTF16Fixed(w io.Writer, value string, units int) {
	encoded := make([]uint16, units)
	runes := []rune(value)
	for i := 0; i < len(runes) && i < units; i++ {
		encoded[i] = uint16(runes[i])
	}
	for _, v := range encoded {
		WriteU16(w, v)
	}
}

func CompressSGAData(Data []byte) ([]byte, error) {
	var Compressed bytes.Buffer
	zw, err := zlib.NewWriterLevel(&Compressed, zlib.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(Data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return Compressed.Bytes(), nil
}
