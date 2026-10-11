package main

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

type sgaPackDir struct {
	name     string
	files    []string
	children []*sgaPackDir
}

type sgaPackFile struct {
	name       string
	diskPath   string
	data       []byte
	compressed []byte
	flags      uint16
	offset     uint32
}

func putU16(dst []byte, off int, v uint16) { binary.LittleEndian.PutUint16(dst[off:off+2], v) }
func putU32(dst []byte, off int, v uint32) { binary.LittleEndian.PutUint32(dst[off:off+4], v) }

func writeU16(w io.Writer, v uint16) { _ = binary.Write(w, binary.LittleEndian, v) }
func writeU32(w io.Writer, v uint32) { _ = binary.Write(w, binary.LittleEndian, v) }

// writeSGA2 writes a small Relic SGA v2 archive with a single Data ToC.
// The layout follows CSgaCreator.cpp from the CDMS/Rainman source tree.
func writeSGA2(output string, files map[string]string, tocName string) error {
	if len(files) > 65535 {
		return fmt.Errorf("too many files for SGA v2: %d", len(files))
	}
	paths := make([]string, 0, len(files))
	for virtualPath := range files {
		paths = append(paths, virtualPath)
	}
	sort.Slice(paths, func(i, j int) bool { return strings.ToLower(paths[i]) < strings.ToLower(paths[j]) })

	root := &sgaPackDir{}
	byPath := map[string]*sgaPackDir{"": root}
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
				child = &sgaPackDir{name: key}
				byPath[key] = child
				dir.children = append(dir.children, child)
			}
			dir = child
		}
		dir.files = append(dir.files, rel)
	}
	for _, dir := range byPath {
		sort.Slice(dir.children, func(i, j int) bool {
			return strings.ToLower(dir.children[i].name) < strings.ToLower(dir.children[j].name)
		})
		sort.Slice(dir.files, func(i, j int) bool { return strings.ToLower(dir.files[i]) < strings.ToLower(dir.files[j]) })
	}
	// SGA directory ranges describe a contiguous child range, so keep the
	// directory table in depth-first order (Corsix/Rainman SGAv2 layout).
	dirs := make([]*sgaPackDir, 0, len(byPath))
	var appendDir func(*sgaPackDir)
	appendDir = func(dir *sgaPackDir) {
		dirs = append(dirs, dir)
		for _, child := range dir.children {
			appendDir(child)
		}
	}
	appendDir(root)
	packFiles := make([]sgaPackFile, 0, len(paths))
	for _, dir := range dirs {
		for _, rel := range dir.files {
			virtualPath := "Data\\" + rel
			diskPath, ok := files[virtualPath]
			if !ok {
				return fmt.Errorf("missing packed file %s", virtualPath)
			}
			data, err := os.ReadFile(diskPath)
			if err != nil {
				return err
			}
			compressed, err := compressSGAData(data)
			if err != nil {
				return err
			}
			flags := uint16(0)
			if len(compressed) < len(data) {
				if len(data) < 4096 {
					flags = 0x20
				} else {
					flags = 0x10
				}
			} else {
				compressed = data
			}
			packFiles = append(packFiles, sgaPackFile{name: rel, diskPath: diskPath, data: data, compressed: compressed, flags: flags})
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
		nameBytes += len(dir.name) + 1
	}
	for _, file := range packFiles {
		nameBytes += len(file.name) + 1
	}
	header := make([]byte, itemOffset+nameBytes)
	putU32(header, 0, 24)
	putU16(header, 4, 1)
	putU32(header, 6, uint32(dirOffset))
	putU16(header, 10, uint16(len(dirs)))
	putU32(header, 12, uint32(fileOffset))
	putU16(header, 16, uint16(len(packFiles)))
	putU32(header, 18, uint32(itemOffset))
	putU16(header, 22, uint16(len(packFiles)+len(dirs)))

	tocBase := 24
	copy(header[tocBase:tocBase+64], []byte(strings.ToLower(tocName)))
	copy(header[tocBase+64:tocBase+128], []byte("created by dow-mod-studio-go"))
	putU16(header, tocBase+128, 0)
	putU16(header, tocBase+130, uint16(len(dirs)))
	putU16(header, tocBase+132, 0)
	putU16(header, tocBase+134, uint16(len(packFiles)))
	putU16(header, tocBase+136, 0)
	putU16(header, tocBase+138, 0)

	dirNameOffsets := make([]uint32, len(dirs))
	fileNameOffsets := make([]uint32, len(packFiles))
	namePos := uint32(itemOffset)
	for i, dir := range dirs {
		dirNameOffsets[i] = namePos - uint32(itemOffset)
		namePos += uint32(len(dir.name) + 1)
	}
	for i, file := range packFiles {
		fileNameOffsets[i] = namePos - uint32(itemOffset)
		namePos += uint32(len(file.name) + 1)
	}
	fileIndex := 0
	for i, dir := range dirs {
		base := dirOffset + dirBytes*i
		putU32(header, base, dirNameOffsets[i])
		putU16(header, base+4, uint16(i+1))
		putU16(header, base+6, uint16(i+1+len(dir.children)))
		fileStart := fileIndex
		putU16(header, base+8, uint16(fileStart))
		fileIndex += len(dir.files)
		putU16(header, base+10, uint16(fileIndex))
	}
	var dataOffset uint32
	for i := range packFiles {
		dataOffset = 0
		if i > 0 {
			dataOffset = packFiles[i-1].offset + uint32(len(packFiles[i-1].compressed))
		}
		packFiles[i].offset = dataOffset
		base := fileOffset + fileBytes*i
		putU32(header, base, fileNameOffsets[i])
		putU16(header, base+4, packFiles[i].flags)
		putU32(header, base+8, packFiles[i].offset)
		putU32(header, base+12, uint32(len(packFiles[i].compressed)))
		putU32(header, base+16, uint32(len(packFiles[i].data)))
	}
	for i, dir := range dirs {
		copy(header[itemOffset+int(dirNameOffsets[i]):], dir.name)
		header[itemOffset+int(dirNameOffsets[i])+len(dir.name)] = 0
	}
	for i, file := range packFiles {
		copy(header[itemOffset+int(fileNameOffsets[i]):], file.name)
		header[itemOffset+int(fileNameOffsets[i])+len(file.name)] = 0
	}

	var out bytes.Buffer
	out.WriteString("_ARCHIVE")
	writeU32(&out, 2)
	out.Write(make([]byte, 16)) // header MD5 placeholder
	writeUTF16Fixed(&out, "Made with dow-mod-studio-go", 64)
	out.Write(make([]byte, 16)) // data MD5 placeholder
	writeU32(&out, uint32(len(header)))
	writeU32(&out, 180+uint32(len(header)))
	out.Write(header)
	for _, file := range packFiles {
		out.Write(file.compressed)
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

func writeUTF16Fixed(w io.Writer, value string, units int) {
	encoded := make([]uint16, units)
	runes := []rune(value)
	for i := 0; i < len(runes) && i < units; i++ {
		encoded[i] = uint16(runes[i])
	}
	for _, v := range encoded {
		writeU16(w, v)
	}
}

func compressSGAData(data []byte) ([]byte, error) {
	var compressed bytes.Buffer
	zw, err := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return compressed.Bytes(), nil
}
