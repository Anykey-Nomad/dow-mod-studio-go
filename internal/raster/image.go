package raster

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"

	"golang.org/x/image/bmp"
)

const (
	maxImageDimension = 32768
	maxImagePixels    = 64 * 1024 * 1024
)

// ImageViewerExtensions is every extension the image viewer knows how to
// decode, and it is the list that decides which files open in it.
//
// It exists because the two used to be written out separately and drift apart:
// DecodeImageDataWithMips gained .rtx and .rsh while the routing in
// openTreeFile kept its shorter list, so clicking an .rtx fell through to the
// text editor and the viewer looked as though it had lost a format. One list,
// checked against the decoders by a test, is what stops that happening again.
var ImageViewerExtensions = []string{
	".jpg", ".jpeg", ".png", ".bmp", ".tga", ".dds", ".rtx", ".rsh",
}

// IsImageViewerFile reports whether a path is one the image viewer takes.
func IsImageViewerFile(virtualPath string) bool {
	ext := strings.ToLower(filepath.Ext(virtualPath))
	for _, want := range ImageViewerExtensions {
		if ext == want {
			return true
		}
	}
	return false
}

func DecodeImageData(data []byte, format string) (image.Image, string, error) {
	img, _, decodedFormat, err := DecodeImageDataWithMips(data, format)
	return img, decodedFormat, err
}

func DecodeImageDataWithMips(data []byte, format string) (image.Image, []image.Image, string, error) {
	if len(data) == 0 {
		return nil, nil, format, fmt.Errorf("empty image file")
	}
	var (
		img  image.Image
		mips []image.Image
		err  error
	)
	switch format {
	case "jpg", "jpeg":
		var cfg image.Config
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
		if err == nil {
			err = validateImageSize(cfg.Width, cfg.Height)
		}
		if err == nil {
			img, err = jpeg.Decode(bytes.NewReader(data))
		}
	case "tga":
		img, err = DecodeTGA(data)
		if err == nil {
			err = validateImageSize(img.Bounds().Dx(), img.Bounds().Dy())
		}
	case "png":
		var cfg image.Config
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
		if err == nil {
			err = validateImageSize(cfg.Width, cfg.Height)
		}
		if err == nil {
			img, err = png.Decode(bytes.NewReader(data))
		}
	case "bmp":
		var cfg image.Config
		cfg, err = bmp.DecodeConfig(bytes.NewReader(data))
		if err == nil {
			err = validateImageSize(cfg.Width, cfg.Height)
		}
		if err == nil {
			img, err = bmp.Decode(bytes.NewReader(data))
		}
	case "dds":
		img, mips, err = decodeDDSMipmaps(data)
	case "rtx":
		img, mips, err = decodeRtx(data)
	case "rsh":
		img, mips, err = DecodeRshImage(data, 0)
	default:
		err = fmt.Errorf("unsupported image format %q", format)
	}
	if err != nil {
		return nil, nil, format, err
	}
	if img == nil {
		return nil, nil, format, fmt.Errorf("decoder returned no image")
	}
	if err = validateImageSize(img.Bounds().Dx(), img.Bounds().Dy()); err != nil {
		return nil, nil, format, err
	}
	return img, mips, format, nil
}

func validateImageSize(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", w, h)
	}
	if w > maxImageDimension || h > maxImageDimension {
		return fmt.Errorf("image is too large (%dx%d; maximum is %d)", w, h, maxImageDimension)
	}
	if int64(w)*int64(h) > maxImagePixels {
		return fmt.Errorf("image is too large (%d pixels; maximum is %d)", w*h, maxImagePixels)
	}
	return nil
}

func DecodeTGA(data []byte) (image.Image, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("truncated TGA header")
	}
	w := int(binary.LittleEndian.Uint16(data[12:14]))
	h := int(binary.LittleEndian.Uint16(data[14:16]))
	if err := validateImageSize(w, h); err != nil {
		return nil, err
	}
	bpp := int(data[16])
	imageType := data[2]
	colorMapped := imageType == 1 || imageType == 9
	if imageType != 1 && imageType != 2 && imageType != 3 && imageType != 9 && imageType != 10 && imageType != 11 {
		return nil, fmt.Errorf("unsupported TGA image type %d", imageType)
	}
	if colorMapped && bpp != 8 {
		return nil, fmt.Errorf("color-mapped TGA must use 8-bit indices")
	}
	if bpp != 8 && bpp != 15 && bpp != 16 && bpp != 24 && bpp != 32 {
		return nil, fmt.Errorf("unsupported TGA pixel depth %d", bpp)
	}
	p := 18 + int(data[0])
	if p > len(data) {
		return nil, fmt.Errorf("truncated TGA image data")
	}
	var palette []color.NRGBA
	paletteFirst := 0
	if data[1] != 0 {
		first := int(binary.LittleEndian.Uint16(data[3:5]))
		paletteFirst = first
		length := int(data[5])
		entryBits := int(data[7])
		entryBytes := (entryBits + 7) / 8
		if entryBytes < 1 || p+length*entryBytes > len(data) {
			return nil, fmt.Errorf("truncated TGA color map")
		}
		palette = make([]color.NRGBA, first+length)
		for i := 0; i < length; i++ {
			c, err := tgaPixel(data[p+i*entryBytes:p+(i+1)*entryBytes], entryBits)
			if err != nil {
				return nil, err
			}
			palette[first+i] = c
		}
		p += length * entryBytes
	}
	bytesPerPixel := (bpp + 7) / 8
	if colorMapped {
		bytesPerPixel = 1
	}
	readPixel := func() (color.NRGBA, error) {
		if p+bytesPerPixel > len(data) {
			return color.NRGBA{}, fmt.Errorf("truncated TGA pixel data")
		}
		raw := data[p : p+bytesPerPixel]
		p += bytesPerPixel
		if colorMapped {
			index := int(raw[0])
			if index < paletteFirst || index >= len(palette) {
				return color.NRGBA{}, fmt.Errorf("TGA palette index %d out of range", index)
			}
			return palette[index], nil
		}
		return tgaPixel(raw, bpp)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	descriptor := data[17]
	topDown := descriptor&0x20 != 0
	rle := imageType == 9 || imageType == 10 || imageType == 11
	for row := 0; row < h; row++ {
		y := row
		if !topDown {
			y = h - 1 - row
		}
		x := 0
		for x < w {
			count := 1
			repeat := false
			if rle {
				if p >= len(data) {
					return nil, fmt.Errorf("truncated TGA RLE stream")
				}
				repeat = data[p]&0x80 != 0
				count = int(data[p]&0x7f) + 1
				p++
			}
			if repeat {
				c, err := readPixel()
				if err != nil {
					return nil, err
				}
				for i := 0; i < count && x < w; i++ {
					img.SetNRGBA(x, y, c)
					x++
				}
			} else {
				count = minInt(count, w-x)
				for i := 0; i < count; i++ {
					c, err := readPixel()
					if err != nil {
						return nil, err
					}
					img.SetNRGBA(x, y, c)
					x++
				}
			}
		}
	}
	return img, nil
}

func tgaPixel(data []byte, bpp int) (color.NRGBA, error) {
	if len(data) < (bpp+7)/8 {
		return color.NRGBA{}, fmt.Errorf("truncated TGA pixel")
	}
	switch bpp {
	case 8:
		return color.NRGBA{R: data[0], G: data[0], B: data[0], A: 255}, nil
	case 15, 16:
		v := binary.LittleEndian.Uint16(data)
		r := uint8((v >> 10) & 0x1f)
		g := uint8((v >> 5) & 0x3f)
		b := uint8(v & 0x1f)
		a := uint8(255)
		if bpp == 16 && v&0x8000 != 0 {
			a = 255
		}
		return color.NRGBA{R: expand5(uint8(r)), G: expand6(uint8(g)), B: expand5(uint8(b)), A: a}, nil
	case 24:
		return color.NRGBA{R: data[2], G: data[1], B: data[0], A: 255}, nil
	default:
		return color.NRGBA{R: data[2], G: data[1], B: data[0], A: data[3]}, nil
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func decodeDDS(data []byte) (image.Image, error) {
	img, _, err := decodeDDSMipmaps(data)
	return img, err
}

// decodeRtx reads a .rtx, the compiled texture the game actually loads. It is
// a Relic Chunky container rather than an image: DATAHEAD says how many images
// it holds, DATAATTR says the format and the size of the top level, and
// DATADATA carries the pixels as a mip chain of DXT blocks, or as an
// uncompressed TGA stream for the formats that are not compressed at all.
//
// This matters because the shipped effects name a .dds that does not exist:
// the game's own art/fx/textures_new/necron/gun_tube_beam.dds and its
// neighbours are stored as gun_tube_beam.rtx, and the FX editor's names are
// what an effect file refers to.
func decodeRtx(data []byte) (image.Image, []image.Image, error) {
	chunks, _ := chunky.Parse(data[minInt(chunky.PreambleSize, len(data)):], 0)
	var (
		attr   *chunky.Chunk
		pixels *chunky.Chunk
	)
	var walk func(cs []*chunky.Chunk)
	walk = func(cs []*chunky.Chunk) {
		for _, c := range cs {
			switch c.TypeID {
			case "DATAATTR":
				if attr == nil {
					attr = c
				}
			case "DATADATA":
				if pixels == nil {
					pixels = c
				}
			}
			walk(c.Children)
		}
	}
	walk(chunks)
	if attr == nil || pixels == nil {
		return nil, nil, fmt.Errorf("rtx has no image data")
	}
	if len(attr.Payload) < 16 {
		return nil, nil, fmt.Errorf("rtx image attributes are truncated")
	}
	format := int(binary.LittleEndian.Uint32(attr.Payload[0:4]))
	w := int(binary.LittleEndian.Uint32(attr.Payload[4:8]))
	h := int(binary.LittleEndian.Uint32(attr.Payload[8:12]))
	mipCount := int(binary.LittleEndian.Uint32(attr.Payload[12:16]))
	if err := validateImageSize(w, h); err != nil {
		return nil, nil, err
	}
	// The two uncompressed formats carry a TGA stream, header and all.
	if format == 0 || format == 2 {
		img, err := DecodeTGA(pixels.Payload)
		if err != nil {
			return nil, nil, err
		}
		return img, []image.Image{img}, nil
	}
	// Which block format the stored number means. The game's own writer sets
	// 8 for DXT1 and 11 for DXT5, with 10 for DXT3 in between.
	var (
		blockBytes     int
		explicitAlpha  bool
		dxt5AlphaBlock bool
	)
	switch format {
	case 8:
		blockBytes = 8
	case 10:
		blockBytes, explicitAlpha = 16, true
	case 11:
		blockBytes, explicitAlpha, dxt5AlphaBlock = 16, true, true
	default:
		return nil, nil, fmt.Errorf("unsupported rtx image format %d", format)
	}
	if mipCount < 1 {
		mipCount = 1
	}
	if mipCount > 20 {
		mipCount = 20
	}
	payload := pixels.Payload
	mips := make([]image.Image, 0, mipCount)
	levelW, levelH := w, h
	offset := 0
	for level := 0; level < mipCount && levelW > 0 && levelH > 0; level++ {
		size := ((levelW + 3) / 4) * ((levelH + 3) / 4) * blockBytes
		if offset+size > len(payload) {
			break
		}
		img, err := decodeDXT(payload[offset:offset+size], levelW, levelH, explicitAlpha, dxt5AlphaBlock)
		if err != nil {
			if level == 0 {
				return nil, nil, err
			}
			break
		}
		mips = append(mips, img)
		offset += size
		if levelW == 1 && levelH == 1 {
			break
		}
		levelW /= 2
		levelH /= 2
	}
	if len(mips) == 0 {
		return nil, nil, fmt.Errorf("rtx holds no mip level")
	}
	return mips[0], mips, nil
}

func decodeDDSMipmaps(data []byte) (image.Image, []image.Image, error) {
	if len(data) < 128 || string(data[:4]) != "DDS " {
		return nil, nil, fmt.Errorf("invalid DDS header")
	}
	headerSize := binary.LittleEndian.Uint32(data[4:8])
	if headerSize < 124 || int(headerSize) > len(data) {
		return nil, nil, fmt.Errorf("invalid DDS header size %d", headerSize)
	}
	// DDS header layout: height is at offset 12 and width at offset 16.
	// Offset 20 is pitch/linear size, not the image width.
	h := int(binary.LittleEndian.Uint32(data[12:16]))
	w := int(binary.LittleEndian.Uint32(data[16:20]))
	if err := validateImageSize(w, h); err != nil {
		return nil, nil, err
	}
	if string(data[84:88]) == "DX10" {
		return decodeDX10DDSMipmaps(data, w, h, int(binary.LittleEndian.Uint32(data[28:32])))
	}
	fourCC := binary.LittleEndian.Uint32(data[84:88])
	mipCount := int(binary.LittleEndian.Uint32(data[28:32]))
	if mipCount < 1 {
		mipCount = 1
	}
	if mipCount > 20 {
		mipCount = 20
	}
	payload := data[128:]
	mips := make([]image.Image, 0, mipCount)
	levelW, levelH := w, h
	offset := 0
	for level := 0; level < mipCount && levelW > 0 && levelH > 0; level++ {
		var (
			img  image.Image
			err  error
			size int
		)
		switch fourCC {
		case 0x31545844: // DXT1
			size = ((levelW + 3) / 4) * ((levelH + 3) / 4) * 8
			if offset+size > len(payload) {
				break
			}
			img, err = decodeDXT(payload[offset:offset+size], levelW, levelH, false, false)
		case 0x33545844, 0x32545844, 0x34545844, 0x35545844: // DXT2/3/4/5
			size = ((levelW + 3) / 4) * ((levelH + 3) / 4) * 16
			if offset+size > len(payload) {
				break
			}
			img, err = decodeDXT(payload[offset:offset+size], levelW, levelH, true, fourCC == 0x34545844 || fourCC == 0x35545844)
		default:
			bits := int(binary.LittleEndian.Uint32(data[88:92]))
			rowBytes := (levelW*bits + 7) / 8
			size = rowBytes * levelH
			if offset+size > len(payload) {
				break
			}
			img, err = decodeDDSUncompressed(data, payload[offset:offset+size], levelW, levelH)
		}
		if size == 0 || offset+size > len(payload) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		mips = append(mips, img)
		offset += size
		levelW = maxInt(1, levelW/2)
		levelH = maxInt(1, levelH/2)
	}
	if len(mips) == 0 {
		return nil, nil, fmt.Errorf("truncated DDS pixel data")
	}
	return mips[0], mips, nil
}

func decodeDX10DDSMipmaps(data []byte, w, h, mipCount int) (image.Image, []image.Image, error) {
	if len(data) < 148 {
		return nil, nil, fmt.Errorf("truncated DX10 DDS header")
	}
	dxgiFormat := binary.LittleEndian.Uint32(data[128:132])
	resourceDimension := binary.LittleEndian.Uint32(data[132:136])
	arraySize := int(binary.LittleEndian.Uint32(data[140:144]))
	if resourceDimension != 3 && resourceDimension != 4 {
		return nil, nil, fmt.Errorf("unsupported DX10 resource dimension %d", resourceDimension)
	}
	if arraySize < 1 {
		arraySize = 1
	}
	if mipCount < 1 {
		mipCount = 1
	}
	if mipCount > 20 {
		mipCount = 20
	}
	var blockBytes int
	var decode func([]byte, int, int) (image.Image, error)
	switch dxgiFormat {
	case 70, 71: // BC1_UNORM / BC1_UNORM_SRGB
		blockBytes = 8
		decode = func(src []byte, lw, lh int) (image.Image, error) { return decodeDXT(src, lw, lh, false, false) }
	case 74, 75: // BC2_UNORM / BC2_UNORM_SRGB
		blockBytes = 16
		decode = func(src []byte, lw, lh int) (image.Image, error) { return decodeDXT(src, lw, lh, true, false) }
	case 77, 78: // BC3_UNORM / BC3_UNORM_SRGB
		blockBytes = 16
		decode = func(src []byte, lw, lh int) (image.Image, error) { return decodeDXT(src, lw, lh, true, true) }
	case 98, 99: // BC7_UNORM / BC7_UNORM_SRGB
		blockBytes = 16
		decode = decodeBC7Image
	default:
		return nil, nil, fmt.Errorf("unsupported DXGI DDS format %d", dxgiFormat)
	}
	payload := data[148:]
	mips := make([]image.Image, 0, mipCount)
	lw, lh := w, h
	offset := 0
	for level := 0; level < mipCount && lw > 0 && lh > 0; level++ {
		size := ((lw + 3) / 4) * ((lh + 3) / 4) * blockBytes
		if offset+size > len(payload) {
			break
		}
		img, err := decode(payload[offset:offset+size], lw, lh)
		if err != nil {
			return nil, nil, err
		}
		mips = append(mips, img)
		offset += size
		lw = maxInt(1, lw/2)
		lh = maxInt(1, lh/2)
	}
	if len(mips) == 0 {
		return nil, nil, fmt.Errorf("truncated DX10 DDS pixel data")
	}
	// Array/cube resources are stored slice by slice. The viewer shows the
	// first slice; all of its mip levels remain available for inspection.
	_ = arraySize
	return mips[0], mips, nil
}

func decodeDDSUncompressed(header, payload []byte, w, h int) (image.Image, error) {
	bits := int(binary.LittleEndian.Uint32(header[88:92]))
	if bits != 16 && bits != 24 && bits != 32 {
		return nil, fmt.Errorf("unsupported uncompressed DDS format (%d bits per pixel)", bits)
	}
	bytesPerPixel := bits / 8
	rowBytes := (w*bits + 7) / 8
	if len(payload) < rowBytes*h {
		return nil, fmt.Errorf("truncated DDS pixel data")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rm := binary.LittleEndian.Uint32(header[92:96])
	gm := binary.LittleEndian.Uint32(header[96:100])
	bm := binary.LittleEndian.Uint32(header[100:104])
	am := binary.LittleEndian.Uint32(header[104:108])
	if rm == 0 && gm == 0 && bm == 0 {
		switch bits {
		case 16:
			rm, gm, bm = 0xF800, 0x07E0, 0x001F
		case 24, 32:
			rm, gm, bm = 0x00FF0000, 0x0000FF00, 0x000000FF
		}
	}
	for y := 0; y < h; y++ {
		row := payload[y*rowBytes:]
		for x := 0; x < w; x++ {
			var v uint32
			switch bytesPerPixel {
			case 2:
				v = uint32(binary.LittleEndian.Uint16(row[x*2:]))
			case 3:
				v = uint32(row[x*3]) | uint32(row[x*3+1])<<8 | uint32(row[x*3+2])<<16
			default:
				v = binary.LittleEndian.Uint32(row[x*4:])
			}
			a := uint8(255)
			if am != 0 {
				a = channelValue(v, am)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: channelValue(v, rm), G: channelValue(v, gm), B: channelValue(v, bm), A: a})
		}
	}
	return img, nil
}

func channelValue(v, mask uint32) uint8 {
	if mask == 0 {
		return 255
	}
	shift := 0
	for mask&1 == 0 {
		mask >>= 1
		shift++
	}
	bits := 0
	for mask&1 != 0 {
		mask >>= 1
		bits++
	}
	n := (v >> shift) & ((1 << bits) - 1)
	if bits == 8 {
		return uint8(n)
	}
	return uint8((n*255 + (1 << (bits - 1))) / (1<<bits - 1))
}

func decodeDXT(data []byte, w, h int, explicitAlpha, dxt5Alpha bool) (image.Image, error) {
	blockBytes := 8
	if explicitAlpha {
		blockBytes = 16
	}
	blocksX := (w + 3) / 4
	blocksY := (h + 3) / 4
	if len(data) < blocksX*blocksY*blockBytes {
		return nil, fmt.Errorf("truncated DDS DXT data")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for by := 0; by < blocksY; by++ {
		for bx := 0; bx < blocksX; bx++ {
			block := data[(by*blocksX+bx)*blockBytes:]
			// Where the two halves sit. A DXT1 block is colour only, so
			// it starts at zero. A DXT5 block is alpha first and colour
			// second, and a DXT3 block the other way round; reading
			// either pair of channels out of the wrong half yields
			// plausible bytes that decode into a flat purple.
			colourAt, alphaAt := 0, 0
			if explicitAlpha {
				if dxt5Alpha {
					colourAt, alphaAt = 8, 0
				} else {
					colourAt, alphaAt = 0, 8
				}
			}
			c0, c1 := unpack565(binary.LittleEndian.Uint16(block[colourAt:])), unpack565(binary.LittleEndian.Uint16(block[colourAt+2:]))
			colors := [4]color.NRGBA{c0, c1, color.NRGBA{}, color.NRGBA{}}
			if c0.R > c1.R || c0.G > c1.G || c0.B > c1.B {
				for i := 1; i < 3; i++ {
					colors[i] = color.NRGBA{R: uint8((2*int(c0.R) + int(c1.R)) / 3), G: uint8((2*int(c0.G) + int(c1.G)) / 3), B: uint8((2*int(c0.B) + int(c1.B)) / 3), A: 255}
				}
			} else {
				colors[2] = color.NRGBA{R: uint8((int(c0.R) + int(c1.R)) / 2), G: uint8((int(c0.G) + int(c1.G)) / 2), B: uint8((int(c0.B) + int(c1.B)) / 2), A: 255}
				colors[3] = color.NRGBA{A: 0}
			}
			indices := binary.LittleEndian.Uint32(block[colourAt+4:])
			var alphas [16]uint8
			if explicitAlpha {
				if dxt5Alpha {
					copy(alphas[:], decodeDXT5Alpha(block[alphaAt:alphaAt+8]))
				} else {
					for i := range alphas {
						nibble := block[alphaAt+i/2] >> ((i & 1) * 4)
						alphas[i] = uint8((uint16(nibble)*255 + 7) / 15)
					}
				}
			} else {
				for i := range alphas {
					alphas[i] = 255
				}
			}
			for py := 0; py < 4; py++ {
				for px := 0; px < 4; px++ {
					x, y := bx*4+px, by*4+py
					if x >= w || y >= h {
						continue
					}
					idx := (indices >> uint((py*4+px)*2)) & 3
					c := colors[idx]
					if explicitAlpha {
						c.A = alphas[py*4+px]
					}
					img.SetNRGBA(x, y, c)
				}
			}
		}
	}
	return img, nil
}

func unpack565(v uint16) color.NRGBA {
	r, g, b := v>>11, (v>>5)&0x3f, v&0x1f
	return color.NRGBA{R: expand5(uint8(r)), G: expand6(uint8(g)), B: expand5(uint8(b)), A: 255}
}
func expand5(v uint8) uint8 { return uint8(v<<3 | v>>2) }
func expand6(v uint8) uint8 { return uint8(v<<2 | v>>4) }

func decodeDXT5Alpha(data []byte) []uint8 {
	a0, a1 := data[0], data[1]
	alphas := make([]uint8, 16)
	for i := 0; i < 8; i++ {
		index := (uint16(data[2+i/3]) >> uint((i%3)*3)) & 7
		var a uint8
		if a0 > a1 {
			a = uint8((int(a0)*(7-int(index)) + int(a1)*int(index)) / 7)
		} else if index == 0 {
			a = a0
		} else if index == 1 {
			a = a1
		} else {
			a = uint8((int(a0)*(8-int(index)) + int(a1)*(int(index)-1)) / 7)
		}
		alphas[i] = a
		alphas[i+8] = a
	}
	return alphas
}
