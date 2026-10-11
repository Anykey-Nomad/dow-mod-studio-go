package main

import (
	"image"
	"testing"
)

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
