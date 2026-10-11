package main

import (
	"image"

	"dow-mod-studio/internal/raster"
)

// The image decoders (.tga, .dds, .rtx, .rsh and the BC7/DXT block codecs)
// live in internal/raster. This file keeps the names the app was calling,
// so the viewer, the FX overlay and the GPU renderers read the same calls
// they did before the move.

type RshImage = raster.RshImage
type RshMip = raster.RshMip

var imageViewerExtensions = raster.ImageViewerExtensions

func isImageViewerFile(virtualPath string) bool { return raster.IsImageViewerFile(virtualPath) }

func decodeImageData(data []byte, format string) (image.Image, string, error) {
	return raster.DecodeImageData(data, format)
}

func decodeImageDataWithMips(data []byte, format string) (image.Image, []image.Image, string, error) {
	return raster.DecodeImageDataWithMips(data, format)
}

func decodeTGA(data []byte) (image.Image, error) { return raster.DecodeTGA(data) }

func decodeRshImage(data []byte, index int) (image.Image, []image.Image, error) {
	return raster.DecodeRshImage(data, index)
}

func RshImages(data []byte) ([]RshImage, error) { return raster.RshImages(data) }

func RshRawImage(data []byte, index int) (RshImage, []RshMip, error) {
	return raster.RshRawImage(data, index)
}

// minInt and maxInt are general helpers, not image code: files across the app
// reach for them, so they stayed behind with the call sites rather than move
// into the decoder package.
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
