package fda

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// FDA constants
const (
	fdaSignature    = "Relic Chunky"
	fdaSignServ     = 0x001a0a0d
	fdaHeaderSize   = 24
	fdaChunkHdrSize = 20

	fdaChunkTypeData = "DATA"
	fdaChunkTypeFold = "FOLD"

	fdaChunkIDFBIF = "FBIF"
	fdaChunkIDFDA  = "FDA "
	fdaChunkIDINFO = "INFO"
	fdaChunkIDDATA = "DATA"
)

// FDAFile represents a parsed FDA audio file
type FDAFile struct {
	Header  FDAHeader
	FBIF    FBIFChunk
	Info    INFOChunk
	RawData []byte
}

// FDAHeader is the file header (24 bytes)
type FDAHeader struct {
	Signature [12]byte
	SignServ  [4]byte
	MajorVer  uint32
	MinorVer  uint32
}

// FDAChunkHeader is a generic chunk header (20 bytes + variable ChunkName)
type FDAChunkHeader struct {
	ChunkType    [4]byte
	ChunkID      [4]byte
	ChunkVer     uint32
	ChunkSize    uint32
	LenChunkName uint32
	ChunkName    string
}

// fixed part of the chunk header (without variable-length ChunkName)
type fdaChunkHdrFixed struct {
	ChunkType    [4]byte
	ChunkID      [4]byte
	ChunkVer     uint32
	ChunkSize    uint32
	LenChunkName uint32
}

// FBIFChunk is the File Burn Info chunk
type FBIFChunk struct {
	PluginName string
	PluginVer  uint32
	UserName   string
	BurnTime   string
}

// INFOChunk is the audio metadata chunk (28 bytes)
type INFOChunk struct {
	Channels     uint32
	SampleSize   uint32
	BlockBitrate uint32
	SampleRate   uint32
	BeginLoop    uint32
	EndLoop      uint32
	StartOffset  uint32
}

// DATAChunk is the audio data chunk header
type DATAChunk struct {
	DataSize uint32
}

// ParseFDA reads and parses an FDA file from disk.
func ParseFDA(filename string) (*FDAFile, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	return ParseFDAReader(f)
}

// ParseFDAReader parses an FDA image from any reader — this is how FDA
// files extracted from SGA archives are parsed (no temp file on disk).
func ParseFDAReader(f io.Reader) (*FDAFile, error) {
	file := &FDAFile{}

	// Read header
	if err := binary.Read(f, binary.LittleEndian, &file.Header); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Validate signature
	if string(file.Header.Signature[:]) != fdaSignature {
		return nil, fmt.Errorf("invalid signature: got %q, want %q", string(file.Header.Signature[:]), fdaSignature)
	}

	// Validate service signature
	signServ := binary.LittleEndian.Uint32(file.Header.SignServ[:])
	if signServ != fdaSignServ {
		return nil, fmt.Errorf("invalid service signature: got 0x%08x, want 0x%08x", signServ, fdaSignServ)
	}

	// Read chunks sequentially
	for {
		hdr, err := readChunkHeader(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read chunk header: %w", err)
		}

		chunkID := string(hdr.ChunkID[:])

		switch chunkID {
		case fdaChunkIDFBIF:
			if err := readFBIFChunk(f, &file.FBIF, hdr.ChunkSize); err != nil {
				return nil, fmt.Errorf("read FBIF: %w", err)
			}

		case fdaChunkIDFDA:
			// Container chunk - just skip, subchunks follow
			// We don't need to read its body

		case fdaChunkIDINFO:
			if err := binary.Read(f, binary.LittleEndian, &file.Info); err != nil {
				return nil, fmt.Errorf("read INFO: %w", err)
			}

		case fdaChunkIDDATA:
			var dc DATAChunk
			if err := binary.Read(f, binary.LittleEndian, &dc); err != nil {
				return nil, fmt.Errorf("read DATA header: %w", err)
			}
			file.RawData = make([]byte, dc.DataSize)
			if _, err := io.ReadFull(f, file.RawData); err != nil {
				return nil, fmt.Errorf("read DATA payload: %w", err)
			}

		default:
			// Unknown chunk - skip
			if hdr.ChunkSize > 0 {
				if _, err := io.CopyN(io.Discard, f, int64(hdr.ChunkSize)); err != nil {
					return nil, fmt.Errorf("skip unknown chunk: %w", err)
				}
			}
		}
	}

	return file, nil
}

func readChunkHeader(r io.Reader) (*FDAChunkHeader, error) {
	var fixed fdaChunkHdrFixed
	if err := binary.Read(r, binary.LittleEndian, &fixed); err != nil {
		return nil, err
	}
	hdr := &FDAChunkHeader{
		ChunkType:    fixed.ChunkType,
		ChunkID:      fixed.ChunkID,
		ChunkVer:     fixed.ChunkVer,
		ChunkSize:    fixed.ChunkSize,
		LenChunkName: fixed.LenChunkName,
	}
	if fixed.LenChunkName > 0 {
		name := make([]byte, fixed.LenChunkName)
		if _, err := io.ReadFull(r, name); err != nil {
			return nil, err
		}
		hdr.ChunkName = string(name)
	}
	return hdr, nil
}

func readFBIFChunk(r io.Reader, fbif *FBIFChunk, chunkSize uint32) error {
	var lenPluginName uint32
	if err := binary.Read(r, binary.LittleEndian, &lenPluginName); err != nil {
		return err
	}
	pluginName := make([]byte, lenPluginName)
	if _, err := io.ReadFull(r, pluginName); err != nil {
		return err
	}
	fbif.PluginName = string(pluginName)

	if err := binary.Read(r, binary.LittleEndian, &fbif.PluginVer); err != nil {
		return err
	}

	var lenUserName uint32
	if err := binary.Read(r, binary.LittleEndian, &lenUserName); err != nil {
		return err
	}
	userName := make([]byte, lenUserName)
	if _, err := io.ReadFull(r, userName); err != nil {
		return err
	}
	fbif.UserName = string(userName)

	var lenBurnTime uint32
	if err := binary.Read(r, binary.LittleEndian, &lenBurnTime); err != nil {
		return err
	}
	burnTime := make([]byte, lenBurnTime)
	if _, err := io.ReadFull(r, burnTime); err != nil {
		return err
	}
	fbif.BurnTime = string(burnTime)

	return nil
}

// PrintInfo displays parsed FDA file information
func (f *FDAFile) PrintInfo() {
	fmt.Printf("FDA File Info:\n")
	fmt.Printf("  Signature: %s\n", string(f.Header.Signature[:]))
	fmt.Printf("  Version:   %d.%d\n", f.Header.MajorVer, f.Header.MinorVer)
	fmt.Printf("  Plugin:    %s (v%d)\n", f.FBIF.PluginName, f.FBIF.PluginVer)
	fmt.Printf("  User:      %s\n", f.FBIF.UserName)
	fmt.Printf("  Created:   %s\n", f.FBIF.BurnTime)
	fmt.Printf("  Channels:  %d\n", f.Info.Channels)
	fmt.Printf("  SampleRate:%d Hz\n", f.Info.SampleRate)
	fmt.Printf("  BitsPerSample: %d\n", f.Info.SampleSize)
	fmt.Printf("  BlockBitrate: %d bits\n", f.Info.BlockBitrate)
	fmt.Printf("  DataSize:  %d bytes\n", len(f.RawData))
	// Calculate bitrate
	bitrate := float64(f.Info.BlockBitrate) * 86.1328125 * float64(f.Info.Channels) / 1000.0
	fmt.Printf("  Bitrate:   %.1f kbps\n", bitrate)
}
