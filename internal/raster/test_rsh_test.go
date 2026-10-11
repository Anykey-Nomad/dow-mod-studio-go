package raster

import (
	"os"
	"path/filepath"
	"testing"
)

const rshNecronsDir = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data\art\ebps\races\Necrons\texture_share`

func TestDecodeRshReal(t *testing.T) {
	img, mips, _, err := DecodeImageDataWithMips(mustReadRsh(t, "nec_warrior.rsh"), "rsh")
	if err != nil {
		t.Fatalf("decodeRshImage: %v", err)
	}
	if got := img.Bounds().Dx(); got != 512 {
		t.Errorf("width = %d, want 512", got)
	}
	if got := img.Bounds().Dy(); got != 512 {
		t.Errorf("height = %d, want 512", got)
	}
	if len(mips) < 5 {
		t.Errorf("mip levels = %d, want at least 5", len(mips))
	}
	// A .rsh must reach the same decoder as a .dds, so any image the block
	// decoder produces is acceptable; only alpha-less garbage would be a bug.
	if _, _, _, a := img.At(10, 10).RGBA(); a == 0 {
		t.Errorf("decoded pixel is fully transparent at 10,10")
	}
}

func TestRshImagesReal(t *testing.T) {
	data := mustReadRsh(t, "nec_warrior.rsh")
	imgs, err := RshImages(data)
	if err != nil {
		t.Fatalf("RshImages: %v", err)
	}
	if len(imgs) == 0 {
		t.Fatal("no images")
	}
	if imgs[0].Format != 11 {
		t.Errorf("first image format = %d, want 11 (DXT5)", imgs[0].Format)
	}
}

func TestRshMatchesDDS(t *testing.T) {
	// The .rsh path must produce exactly the same pixels as the DDS path for
	// the same block data, which is what the synthetic header promises.
	src := []byte{0xF8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	img, _, err := decodeRshSurface(8, 4, 4, 1, src)
	if err != nil {
		t.Fatalf("decodeRshSurface: %v", err)
	}
	dds := append(ddsHeaderForTest(4, 4, 1, 0x31545844), src...)
	want, _, err := decodeDDSMipmaps(dds)
	if err != nil {
		t.Fatalf("decodeDDSMipmaps: %v", err)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if img.At(x, y) != want.At(x, y) {
				t.Fatalf("pixel %d,%d = %v, want %v", x, y, img.At(x, y), want.At(x, y))
			}
		}
	}
}

func TestDecodeRshUnsupportedFormat(t *testing.T) {
	if _, _, err := decodeRshSurface(3, 4, 4, 1, make([]byte, 8)); err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
}

func TestDecodeRshTruncatedMips(t *testing.T) {
	// Some .rsh files declare more mips than they store; the first level must
	// still decode instead of failing the whole file.
	img, mips, err := decodeRshSurface(11, 64, 64, 10, make([]byte, 64*64))
	if err != nil {
		t.Fatalf("decodeRshSurface: %v", err)
	}
	if img == nil || len(mips) == 0 {
		t.Fatal("no image decoded")
	}
}

func ddsHeaderForTest(w, h, mips int, fourCC uint32) []byte {
	header := make([]byte, 128)
	copy(header, "DDS ")
	header[4] = 124
	put32 := func(off int, v uint32) {
		header[off] = byte(v)
		header[off+1] = byte(v >> 8)
		header[off+2] = byte(v >> 16)
		header[off+3] = byte(v >> 24)
	}
	put32(12, uint32(h))
	put32(16, uint32(w))
	put32(28, uint32(mips))
	put32(84, fourCC)
	return header
}

func mustReadRsh(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(rshNecronsDir, name))
	if err != nil {
		t.Skipf("game textures not available: %v", err)
	}
	return data
}

// TestRshRawMips checks the block compressed mip chain can be handed to a GPU
// without being decoded first, and that the split adds up to what a decoder
// would have consumed.
func TestRshRawMips(t *testing.T) {
	data := mustReadRsh(t, "nec_warrior.rsh")
	info, mips, err := RshRawImage(data, 0)
	if err != nil {
		t.Fatalf("RshRawImage: %v", err)
	}
	if info.Format != 11 {
		t.Fatalf("format = %d, want 11 (DXT5)", info.Format)
	}
	if len(mips) < 5 {
		t.Fatalf("mip levels = %d, want at least 5", len(mips))
	}
	block, _ := rshBlockBytes(info.Format)
	w, h := info.Width, info.Height
	total := 0
	for i, mip := range mips {
		want := ((w + 3) / 4) * ((h + 3) / 4) * block
		if len(mip.Data) != want {
			t.Errorf("mip %d is %d bytes, want %d", i, len(mip.Data), want)
		}
		if mip.Width != w || mip.Height != h {
			t.Errorf("mip %d is %dx%d, want %dx%d", i, mip.Width, mip.Height, w, h)
		}
		total += len(mip.Data)
		w, h = maxInt(1, w/2), maxInt(1, h/2)
	}
	// The decoded size of the same chain has to match, otherwise the GPU would
	// be handed a different chain than the decoder saw.
	img, decoded, err := DecodeRshImage(data, 0)
	if err != nil {
		t.Fatalf("decodeRshImage: %v", err)
	}
	if len(decoded) != len(mips) {
		t.Errorf("decoded %d levels, raw split gave %d", len(decoded), len(mips))
	}
	if got := img.Bounds().Dx(); got != info.Width {
		t.Errorf("decoded width %d, header says %d", got, info.Width)
	}
	t.Logf("%s: %dx%d format %d, %d levels, %d bytes of blocks", info.Name, info.Width, info.Height, info.Format, len(mips), total)
}

func TestRshBlockBytes(t *testing.T) {
	for _, tt := range []struct {
		format int32
		want   int
		ok     bool
	}{{8, 8, true}, {10, 16, true}, {11, 16, true}, {3, 0, false}} {
		got, ok := rshBlockBytes(tt.format)
		if got != tt.want || ok != tt.ok {
			t.Errorf("rshBlockBytes(%d) = %d, %v; want %d, %v", tt.format, got, ok, tt.want, tt.ok)
		}
	}
}

// TestSplitRshMipsTruncated covers a file that declares more levels than it
// stores: the levels that are present must still come back.
func TestSplitRshMipsTruncated(t *testing.T) {
	// BC3: a 16x16 level is 4x4 blocks of 16 bytes, 256 bytes in total.
	mips, ok := splitRshMips(16, 16, 4, 16, make([]byte, 256+64))
	if !ok {
		t.Fatal("no levels returned")
	}
	if len(mips) != 2 {
		t.Fatalf("levels = %d, want 2", len(mips))
	}
	if len(mips[0].Data) != 256 {
		t.Errorf("first level is %d bytes, want 256", len(mips[0].Data))
	}
	// Levels never shrink below one block.
	if got := len(mips[1].Data); got != 64 {
		t.Errorf("second level is %d bytes, want 64", got)
	}
}
