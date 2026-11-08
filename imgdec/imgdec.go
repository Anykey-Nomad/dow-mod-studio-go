// Package imgdec декодирует игровые текстуры: TGA, DDS, скомпилированные
// .rtx ремастера, RSH. Это публичная обёртка над internal/raster: внутренние
// пакеты недоступны другим Go-модулям, а Open-Kaurava рисует те же текстуры.
package imgdec

import (
	"image"

	"dow-mod-studio/internal/raster"
)

// Decode декодирует изображение; format — расширение без точки:
// "tga", "dds", "rtx", "rsh", "png", "jpg", "bmp".
func Decode(format string, data []byte) (image.Image, error) {
	img, _, err := raster.DecodeImageData(data, format)
	return img, err
}
