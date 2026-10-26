//go:build windows || linux

package app

import (
	"image/color"
	"log"
	"math"
	"os"

	"github.com/fogleman/fauxgl"
)

const (
	modelGpuVertexStride = 64
	modelGpuMaxBones     = 256
)

var panelBackground = color.NRGBA{R: 0x1c, G: 0x1e, B: 0x24, A: 0xff}

func srgbToLinear(v uint8) float32 {
	c := float64(v) / 255
	if c <= 0.04045 {
		return float32(c)
	}
	return float32(math.Pow((c+0.055)/1.055, 2.4))
}

func fadeTintToBackground(tint color.RGBA, alpha float32) color.RGBA {
	if alpha >= 1 {
		return tint
	}
	if alpha <= 0 {
		bg := panelBackground
		return color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: tint.A}
	}
	a := float64(alpha)
	mix := func(v uint8, bg uint8) uint8 {
		lin := float64(v) / 255
		out := lin*a + float64(srgbToLinear(bg))*(1-a)
		if out <= 0 {
			return 0
		}
		if out >= 1 {
			return 255
		}
		return uint8(out*255 + 0.5)
	}
	bg := panelBackground
	return color.RGBA{
		R: mix(tint.R, bg.R),
		G: mix(tint.G, bg.G),
		B: mix(tint.B, bg.B),
		A: tint.A,
	}
}

func linearToSrgb(v float64) float64 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	if v <= 0.0031308 {
		return v * 12.92 * 255
	}
	return (1.055*math.Pow(v, 1/2.4) - 0.055) * 255
}

func hslslMatrix(dst []float32, m fauxgl.Matrix) {
	row := [16]float64{
		m.X00, m.X01, m.X02, m.X03,
		m.X10, m.X11, m.X12, m.X13,
		m.X20, m.X21, m.X22, m.X23,
		m.X30, m.X31, m.X32, m.X33,
	}
	for r := range 4 {
		for c := range 4 {
			dst[c*4+r] = float32(row[r*4+c])
		}
	}
}

func probeGpuFrame(f modelGpuFrame, context string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("3D preview: probe %s: GPU readback failed, not fatal: %v", context, r)
			ok = false
		}
	}()
	probeGpuFrameRead(f, context)
	return true
}

func probeGpuFrameRead(f modelGpuFrame, context string) {
	if os.Getenv("DOW_NO_GPU_PROBE") != "" {
		return
	}
	gf, ok := f.(*modelGpuVkFrame)
	if !ok || gf == nil || gf.tex == nil {
		log.Printf("3D preview: probe %s: frame is not a Vulkan frame", context)
		return
	}
	px, err := gf.tex.ReadPixels()
	if err != nil {
		log.Printf("3D preview: probe %s: readback failed: %v", context, err)
		return
	}
	stride := gf.size.X * 4
	if stride <= 0 {
		log.Printf("3D preview: probe %s: invalid frame size %v", context, gf.size)
		return
	}
	rows := len(px) / stride
	bgR, bgG, bgB := int(panelBackground.R), int(panelBackground.G), int(panelBackground.B)
	total, clearPix, darkPix, litPix := 0, 0, 0, 0
	lumaSum, lumaMax := 0, 0
	for y := 0; y < rows; y++ {
		row := px[y*stride : (y+1)*stride]
		for x := 0; x+3 < len(row); x += 4 {
			r, g, b := int(row[x]), int(row[x+1]), int(row[x+2])
			total++
			if absInt(r-bgR) <= 2 && absInt(g-bgG) <= 2 && absInt(b-bgB) <= 2 {
				clearPix++
				continue
			}
			l := (r*299 + g*587 + b*114) / 1000
			lumaSum += l
			if l > lumaMax {
				lumaMax = l
			}
			if l >= 40 {
				litPix++
			} else {
				darkPix++
			}
		}
	}
	if total == 0 {
		log.Printf("3D preview: probe %s: empty readback (%d bytes)", context, len(px))
		return
	}
	modelPix := total - clearPix
	avg := 0
	if modelPix > 0 {
		avg = lumaSum / modelPix
	}
	log.Printf("3D preview: probe %s: clear=%d%% model=%d%% (dark=%d%% lit=%d%%) modelAvgLuma=%d max=%d",
		context, clearPix*100/total, modelPix*100/total,
		darkPix*100/total, litPix*100/total, avg, lumaMax)
}
