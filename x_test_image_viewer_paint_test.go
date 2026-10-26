package main

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/op"
)

func TestImageViewerPaintsTheImage(t *testing.T) {
	size := image.Pt(1000, 700)
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		t.Skipf("no GPU backend: %v", err)
	}
	defer win.Release()

	// A saturated magenta image: no palette colour in the dark theme is
	// anywhere near it, so one hit in the screenshot proves the picture
	// reached the screen.
	const w, h = 64, 64
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xFF, B: 0xFF, A: 0xFF})
		}
	}

	ui := newUI(unitTestTheme())
	tab := &fileTab{
		path:        `Data\test\pink.png`,
		kind:        "image",
		imageData:   img,
		imageFormat: "PNG",
		imageZoom:   1,
	}
	ui.openTabs = []*fileTab{tab}
	ui.tabSel = 1
	ui.ensureTabClicks()

	var ops op.Ops
	gtx := newLayoutContextForTest(&ops, size.X, size.Y)
	ui.layoutContent(gtx)

	shot := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if err := win.Screenshot(shot); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}

	seen, minX, minY, maxX, maxY := 0, size.X, size.Y, -1, -1
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			c := shot.RGBAAt(x, y)
			if c.R > 0xF0 && c.B > 0xF0 && c.G < 0x40 {
				seen++
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("the image viewer drew no image pixels: the picture is not on screen")
	}
	t.Logf("image pixels: %d, bbox (%d,%d)-(%d,%d)", seen, minX, minY, maxX, maxY)
	// The 64×64 image at 100% must appear at its natural size, centred in
	// the canvas below the controls row — not stretched over the panel.
	if maxX-minX+1 != w || maxY-minY+1 != h {
		t.Errorf("the image is %dx%d on screen, want %dx%d at 100%% zoom",
			maxX-minX+1, maxY-minY+1, w, h)
	}
}

// ─── merged from x_test_image_decode_test.go ────────────────────────────────────────

func TestImageCache(t *testing.T) {
	ui := newUI(nil)
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	ui.cacheDecodedImage(`Data\attrib\textures\icon.dds`, img, nil, "DDS", 123)
	entry, ok := ui.cachedImage(`data/attrib/textures/icon.DDS`)
	if !ok || entry.data == nil || entry.bytes != 123 || entry.format != "DDS" {
		t.Fatalf("cache lookup failed: %#v", entry)
	}
	if ui.imageCacheBytes != 16 {
		t.Fatalf("unexpected cache size: %d", ui.imageCacheBytes)
	}
}
