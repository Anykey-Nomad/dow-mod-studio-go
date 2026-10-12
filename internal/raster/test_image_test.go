package raster

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
)

func TestDecodeBC7Block(t *testing.T) {
	block := make([]byte, 16)
	block[0] = 1 // BC7 mode 0 prefix (leading one, zero continuation).
	img, err := decodeBC7Image(block, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.At(0, 0); got != (color.NRGBA{R: 0, G: 0, B: 0, A: 0xff}) {
		t.Fatalf("unexpected BC7 pixel: %#v", got)
	}
}

func TestDecodeDX10BC1Array(t *testing.T) {
	data := make([]byte, 148+8)
	copy(data[:4], "DDS ")
	binary.LittleEndian.PutUint32(data[4:8], 124)
	binary.LittleEndian.PutUint32(data[12:16], 4)
	binary.LittleEndian.PutUint32(data[16:20], 4)
	binary.LittleEndian.PutUint32(data[28:32], 1)
	copy(data[84:88], "DX10")
	binary.LittleEndian.PutUint32(data[128:132], 70) // BC1_UNORM
	binary.LittleEndian.PutUint32(data[132:136], 3)  // TEXTURE2D
	binary.LittleEndian.PutUint32(data[140:144], 2)  // two array elements
	img, _, _, err := DecodeImageDataWithMips(data, "dds")
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Fatalf("unexpected DX10 bounds: %v", img.Bounds())
	}
}

func TestDecodeImageDataJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := jpeg.Encode(&data, src, nil); err != nil {
		t.Fatal(err)
	}
	img, format, err := DecodeImageData(data.Bytes(), "jpg")
	if err != nil {
		t.Fatal(err)
	}
	if format != "jpg" || img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Fatalf("unexpected result: %s %v", format, img.Bounds())
	}
}

func TestDecodeImageDataTGA(t *testing.T) {
	data := make([]byte, 18+6)
	data[2] = 2
	binary.LittleEndian.PutUint16(data[12:14], 2)
	binary.LittleEndian.PutUint16(data[14:16], 1)
	data[16] = 24
	copy(data[18:], []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60})
	img, _, err := DecodeImageData(data, "tga")
	if err != nil {
		t.Fatal(err)
	}
	if got := img.At(0, 0); got != (color.NRGBA{R: 0x30, G: 0x20, B: 0x10, A: 0xff}) {
		t.Fatalf("unexpected first pixel: %#v", got)
	}
}

func TestDecodeImageDataPNGAndBMP(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, src); err != nil {
		t.Fatal(err)
	}
	var bmpData bytes.Buffer
	if err := bmp.Encode(&bmpData, src); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{{"png", pngData.Bytes()}, {"bmp", bmpData.Bytes()}} {
		img, format, err := DecodeImageData(tc.data, tc.name)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if format != tc.name || img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
			t.Fatalf("%s: unexpected result %s %v", tc.name, format, img.Bounds())
		}
	}
}

func TestDecodeTGAAlphaAttributeBits(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bpp       byte
		attribute byte
		pixel     []byte
		want      color.NRGBA
	}{
		{"32-bit without alpha attributes", 32, 0, []byte{0x10, 0x20, 0x30, 0x00}, color.NRGBA{R: 0x30, G: 0x20, B: 0x10, A: 255}},
		{"32-bit with alpha attributes", 32, 8, []byte{0x10, 0x20, 0x30, 0x40}, color.NRGBA{R: 0x30, G: 0x20, B: 0x10, A: 0x40}},
		{"32-bit with zero alpha attributes", 32, 1, []byte{0x10, 0x20, 0x30, 0x40}, color.NRGBA{R: 0x30, G: 0x20, B: 0x10, A: 255}},
		{"16-bit without alpha attributes", 16, 0, []byte{0x00, 0x00}, color.NRGBA{A: 255}},
		{"16-bit with alpha attributes clear", 16, 1, []byte{0x00, 0x00}, color.NRGBA{A: 0}},
		{"16-bit with alpha attributes set", 16, 1, []byte{0x00, 0x80}, color.NRGBA{A: 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, 18+len(tc.pixel))
			data[2] = 2
			binary.LittleEndian.PutUint16(data[12:14], 1)
			binary.LittleEndian.PutUint16(data[14:16], 1)
			data[16], data[17] = tc.bpp, tc.attribute
			copy(data[18:], tc.pixel)
			img, err := DecodeTGA(data)
			if err != nil {
				t.Fatal(err)
			}
			if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got != tc.want {
				t.Errorf("pixel = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDecodeColorMappedTGA(t *testing.T) {
	data := make([]byte, 18+3+1)
	data[1] = 1
	data[2] = 1
	data[5] = 1
	data[7] = 24
	binary.LittleEndian.PutUint16(data[12:14], 1)
	binary.LittleEndian.PutUint16(data[14:16], 1)
	data[16] = 8
	copy(data[18:], []byte{0x10, 0x20, 0x30})
	data[21] = 0
	img, _, err := DecodeImageData(data, "tga")
	if err != nil {
		t.Fatal(err)
	}
	if got := img.At(0, 0); got != (color.NRGBA{R: 0x30, G: 0x20, B: 0x10, A: 0xff}) {
		t.Fatalf("unexpected palette pixel: %#v", got)
	}
}

func TestDecodeDDSMipmaps(t *testing.T) {
	data := make([]byte, 128+16)
	copy(data[:4], "DDS ")
	binary.LittleEndian.PutUint32(data[4:8], 124)
	binary.LittleEndian.PutUint32(data[12:16], 4)
	binary.LittleEndian.PutUint32(data[16:20], 4)
	binary.LittleEndian.PutUint32(data[28:32], 2)
	binary.LittleEndian.PutUint32(data[84:88], 0x31545844)
	img, mipmaps, _, err := DecodeImageDataWithMips(data, "dds")
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 4 {
		t.Fatalf("unexpected base bounds: %v", img.Bounds())
	}
	if len(mipmaps) != 2 || mipmaps[1].Bounds().Dx() != 2 || mipmaps[1].Bounds().Dy() != 2 {
		t.Fatalf("unexpected mipmaps: %d levels", len(mipmaps))
	}
}

func TestDecodeDXT1(t *testing.T) {
	data := make([]byte, 128+16)
	copy(data[:4], "DDS ")
	binary.LittleEndian.PutUint32(data[4:8], 124)
	binary.LittleEndian.PutUint32(data[12:16], 8) // height
	binary.LittleEndian.PutUint32(data[16:20], 4) // width
	binary.LittleEndian.PutUint32(data[20:24], 64)
	binary.LittleEndian.PutUint32(data[84:88], 0x31545844) // DXT1
	binary.LittleEndian.PutUint16(data[128:130], 0xf800)
	binary.LittleEndian.PutUint16(data[130:132], 0x001f)
	img, _, err := DecodeImageData(data, "dds")
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Size(); got != (image.Point{X: 4, Y: 8}) {
		t.Fatalf("unexpected DXT1 dimensions: %v", got)
	}
	if got := img.At(0, 0); got != (color.NRGBA{R: 0xff, G: 0, B: 0, A: 0xff}) {
		t.Fatalf("unexpected DXT1 pixel: %#v", got)
	}
}

// TestDecodeDXTBlockHalves pins where the two halves of a DXT3 and a DXT5
// block sit: DXT5 stores alpha first and colour second, DXT3 the other way
// round. Reading either pair of channels out of the wrong half still decodes
// without complaint — it decodes into a flat purple, which is what every
// texture in the game looked like on the software path before this was
// pinned. A block is built here with a colour in one half and bytes in the
// other that would decode into that purple if they were read as colour.
func TestDecodeDXTBlockHalves(t *testing.T) {
	dds := func(fourCC uint32, block []byte) image.Image {
		data := make([]byte, 128+len(block))
		copy(data[:4], "DDS ")
		binary.LittleEndian.PutUint32(data[4:8], 124)
		binary.LittleEndian.PutUint32(data[12:16], 4)
		binary.LittleEndian.PutUint32(data[16:20], 4)
		binary.LittleEndian.PutUint32(data[84:88], fourCC)
		copy(data[128:], block)
		img, _, err := DecodeImageData(data, "dds")
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	// The alpha half, DXT5 style: a0 and a1 at the front, then six bytes of
	// 3-bit indices. As 5:6:5 colour these bytes make f9f8, which unpacks to
	// a bright magenta — the colour a wrong half reads as.
	alphaHalf := []byte{0xff, 0x00, 0xf9, 0xf8, 0x7b, 0xef, 0x73, 0xce}
	// The colour half: pure red over pure black, every pixel on the first
	// endpoint.
	colour := []byte{0x00, 0xf8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	// Only the colour channels are compared, and read back unpremultiplied:
	// the alpha half carries its own ramp, and this is about which half the
	// colour came out of.
	isRed := func(c color.Color) bool {
		n := color.NRGBAModel.Convert(c).(color.NRGBA)
		return n.R == 0xff && n.G == 0 && n.B == 0
	}

	dxt5 := dds(0x35545844, append(append([]byte{}, alphaHalf...), colour...))
	if !isRed(dxt5.At(0, 0)) {
		t.Errorf("DXT5 block read its colour out of the alpha half: %#v", dxt5.At(0, 0))
	}
	if !isRed(dxt5.At(3, 3)) {
		t.Errorf("DXT5 block, far corner: %#v", dxt5.At(3, 3))
	}

	// DXT3 is the other way round: four-bit alpha indices first, colour
	// second, with the same red colour to find in the second half.
	dxt3 := dds(0x33545844, append(append([]byte{}, colour...), alphaHalf[:8]...))
	if !isRed(dxt3.At(0, 0)) {
		t.Errorf("DXT3 block read its colour out of the alpha half: %#v", dxt3.At(0, 0))
	}
}
