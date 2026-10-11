package raster

import (
	"dow-mod-studio/internal/chunky"
	"encoding/binary"
	"fmt"
	"image"
)

// Relic shared texture (.rsh) decoding.
//
// A .rsh file is a Relic Chunky stream, the same container the models use:
//
//	FOLDSHRF
//	  FOLDTXTR          one per image the material references; the first is the
//	                    diffuse map, the rest are generated "_default_*_spec" and
//	                    "_reflection" companions
//	    DATAHEAD        image_type, num_images
//	    FOLDIMAG
//	      DATAATTR v2   format, width, height, mip count
//	      DATADATA v2   mip chain of raw block data, no DDS header
//	  FOLDSHDR          material channels, not needed to view the texture
//
// The block data is byte identical to a DDS surface, so rather than write a
// second block decoder the header described by DATAATTR is synthesised and the
// existing DDS path does the work. Only the three formats Dawn of War ships are
// mapped: 8 = DXT1 (BC1), 10 = DXT3 (BC2), 11 = DXT5 (BC3).

// rshFormatFourCC maps a DATAATTR format id onto the matching DDS FourCC.
func rshFormatFourCC(format int32) (uint32, bool) {
	switch format {
	case 8: // DXT1 / BC1
		return 0x31545844, true
	case 10: // DXT3 / BC2
		return 0x33545844, true
	case 11: // DXT5 / BC3
		return 0x35545844, true
	}
	return 0, false
}

// rshBlockBytes returns the size of one compressed block of a .rsh surface.
// Dawn of War ships DXT1, DXT3 and DXT5 only, so BC1, BC2 and BC3 cover every
// file in the game.
func rshBlockBytes(format int32) (int, bool) {
	switch format {
	case 8: // DXT1 / BC1: eight bytes per four by four block
		return 8, true
	case 10, 11: // DXT3 / BC2 and DXT5 / BC3: sixteen bytes
		return 16, true
	}
	return 0, false
}

// RshMip is one mip level of a .rsh image, still block compressed. A GPU can
// take it as it is, so nothing has to be decoded on the CPU.
type RshMip struct {
	Width, Height int
	Data          []byte
}

// RshRawImage returns image number index of a .rsh file with its mip chain
// split out, but still compressed. The slices alias data, which the caller
// keeps alive until the upload is done.
func RshRawImage(data []byte, index int) (RshImage, []RshMip, error) {
	block, ok := 0, false
	var (
		info RshImage
		mips []RshMip
	)
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	found := -1
	for _, c := range chunks {
		for _, tx := range c.Children {
			if tx.TypeID != "FOLDTXTR" {
				continue
			}
			img := tx.Child("FOLDIMAG")
			if img == nil {
				continue
			}
			found++
			if found != index {
				continue
			}
			attr := img.Child("DATAATTR")
			dat := img.Child("DATADATA")
			if attr == nil || dat == nil || len(attr.Payload) < 16 {
				return RshImage{}, nil, fmt.Errorf("texture %d: missing DATAATTR or DATADATA", index)
			}
			info = RshImage{
				Name:   tx.Name,
				Format: int32(binary.LittleEndian.Uint32(attr.Payload[0:])),
				Width:  int(binary.LittleEndian.Uint32(attr.Payload[4:])),
				Height: int(binary.LittleEndian.Uint32(attr.Payload[8:])),
				Mips:   int(binary.LittleEndian.Uint32(attr.Payload[12:])),
			}
			block, ok = rshBlockBytes(info.Format)
			if !ok {
				return RshImage{}, nil, fmt.Errorf("unsupported .rsh texture format %d", info.Format)
			}
			if err := validateImageSize(info.Width, info.Height); err != nil {
				return RshImage{}, nil, err
			}
			mips, ok = splitRshMips(info.Width, info.Height, info.Mips, block, dat.Payload)
			if !ok {
				return RshImage{}, nil, fmt.Errorf("texture %d: mip chain is %d bytes, too short for %d levels", index, len(dat.Payload), info.Mips)
			}
			return info, mips, nil
		}
	}
	return RshImage{}, nil, fmt.Errorf("texture %d not found", index)
}

// splitRshMips walks a mip chain of equal sized blocks. Levels are stored one
// after another, each a quarter of the area of the last, and a level never
// drops below one block. A file that stops short of its declared mip count is
// not an error: the levels that are there are returned.
func splitRshMips(width, height, mips, blockBytes int, payload []byte) ([]RshMip, bool) {
	if mips < 1 {
		mips = 1
	}
	if mips > 20 {
		mips = 20
	}
	w, h := width, height
	out := make([]RshMip, 0, mips)
	for range mips {
		blocks := ((w + 3) / 4) * ((h + 3) / 4)
		size := blocks * blockBytes
		if size > len(payload) {
			break
		}
		out = append(out, RshMip{Width: w, Height: h, Data: payload[:size]})
		payload = payload[size:]
		w = maxInt(1, w/2)
		h = maxInt(1, h/2)
	}
	return out, len(out) > 0
}

// RshImage is one decoded image from a .rsh file.
type RshImage struct {
	Name   string
	Format int32
	Width  int
	Height int
	Mips   int
}

// RshImages lists every image a .rsh file holds, in file order.
func RshImages(data []byte) ([]RshImage, error) {
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	var out []RshImage
	for _, c := range chunks {
		for _, tx := range c.Children {
			if tx.TypeID != "FOLDTXTR" {
				continue
			}
			img := tx.Child("FOLDIMAG")
			if img == nil {
				continue
			}
			attr := img.Child("DATAATTR")
			dat := img.Child("DATADATA")
			if attr == nil || dat == nil || len(attr.Payload) < 16 {
				continue
			}
			out = append(out, RshImage{
				Name:   tx.Name,
				Format: int32(binary.LittleEndian.Uint32(attr.Payload[0:])),
				Width:  int(binary.LittleEndian.Uint32(attr.Payload[4:])),
				Height: int(binary.LittleEndian.Uint32(attr.Payload[8:])),
				Mips:   int(binary.LittleEndian.Uint32(attr.Payload[12:])),
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no FOLDIMAG image found")
	}
	return out, nil
}

// DecodeRshImage decodes image number index of a .rsh file with its mip chain.
func DecodeRshImage(data []byte, index int) (image.Image, []image.Image, error) {
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	found := -1
	for _, c := range chunks {
		for _, tx := range c.Children {
			if tx.TypeID != "FOLDTXTR" {
				continue
			}
			img := tx.Child("FOLDIMAG")
			if img == nil {
				continue
			}
			found++
			if found != index {
				continue
			}
			attr := img.Child("DATAATTR")
			dat := img.Child("DATADATA")
			if attr == nil || dat == nil || len(attr.Payload) < 16 {
				return nil, nil, fmt.Errorf("texture %d: missing DATAATTR or DATADATA", index)
			}
			return decodeRshSurface(
				int32(binary.LittleEndian.Uint32(attr.Payload[0:])),
				int(binary.LittleEndian.Uint32(attr.Payload[4:])),
				int(binary.LittleEndian.Uint32(attr.Payload[8:])),
				int(binary.LittleEndian.Uint32(attr.Payload[12:])),
				dat.Payload,
			)
		}
	}
	return nil, nil, fmt.Errorf("texture %d not found", index)
}

// decodeRshSurface wraps raw mip data in a DDS header and decodes it.
func decodeRshSurface(format int32, width, height, mips int, payload []byte) (image.Image, []image.Image, error) {
	fourCC, ok := rshFormatFourCC(format)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported .rsh texture format %d", format)
	}
	if err := validateImageSize(width, height); err != nil {
		return nil, nil, err
	}
	if mips < 1 {
		mips = 1
	}
	if mips > 20 {
		mips = 20
	}
	// The DDS header is 128 bytes; only the fields the decoder reads are set.
	header := make([]byte, 128)
	copy(header, "DDS ")
	binary.LittleEndian.PutUint32(header[4:], 124)
	binary.LittleEndian.PutUint32(header[12:], uint32(height))
	binary.LittleEndian.PutUint32(header[16:], uint32(width))
	binary.LittleEndian.PutUint32(header[20:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(header[28:], uint32(mips))
	binary.LittleEndian.PutUint32(header[84:], fourCC)

	dds := make([]byte, 0, 128+len(payload))
	dds = append(dds, header...)
	dds = append(dds, payload...)
	img, levels, err := decodeDDSMipmaps(dds)
	if err != nil {
		return nil, nil, err
	}
	return img, levels, nil
}
