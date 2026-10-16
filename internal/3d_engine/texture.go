package engine

import (
	"image"
	"image/color"
	"math"

	"github.com/fogleman/fauxgl"
)

// Texture samples game textures in top-down row order, unlike
// fauxgl's built-in ImageTexture which flips V.
type Texture struct {
	img *image.RGBA
}

// NewTexture copies img into a tightly-bounded RGBA image and returns a
// top-down wrapping texture.
func NewTexture(img image.Image) fauxgl.Texture {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if src, ok := img.(*image.RGBA); ok {
		for y := 0; y < b.Dy(); y++ {
			copy(out.Pix[y*out.Stride:y*out.Stride+b.Dx()*4],
				src.Pix[(b.Min.Y+y)*src.Stride+b.Min.X*4:(b.Min.Y+y)*src.Stride+(b.Min.X+b.Dx())*4])
		}
		return Texture{img: out}
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return Texture{img: out}
}

func (t Texture) Sample(u, v float64) fauxgl.Color {
	b := t.img.Bounds()
	return texelColor(t.img.RGBAAt(b.Min.X+wrapTexel(u, b.Dx()), b.Min.Y+wrapTexel(v, b.Dy())))
}

// BilinearSample samples between four tiled texels.
func (t Texture) BilinearSample(u, v float64) fauxgl.Color {
	b := t.img.Bounds()
	x, y := wrapTexelF(u, b.Dx()-1), wrapTexelF(v, b.Dy()-1)
	x0, y0 := int(x), int(y)
	fx, fy := x-float64(x0), y-float64(y0)
	c00 := t.texel(x0, y0, b)
	c10 := t.texel(x0+1, y0, b)
	c01 := t.texel(x0, y0+1, b)
	c11 := t.texel(x0+1, y0+1, b)
	out := fauxgl.Color{}
	out = out.Add(c00.MulScalar((1 - fx) * (1 - fy)))
	out = out.Add(c10.MulScalar(fx * (1 - fy)))
	out = out.Add(c01.MulScalar((1 - fx) * fy))
	out = out.Add(c11.MulScalar(fx * fy))
	return out
}

func (t Texture) texel(x, y int, b image.Rectangle) fauxgl.Color {
	return texelColor(t.img.RGBAAt(b.Min.X+wrapTexelI(x, b.Dx()), b.Min.Y+wrapTexelI(y, b.Dy())))
}

func texelColor(c color.RGBA) fauxgl.Color {
	if c.A == 0 {
		return fauxgl.Color{}
	}
	return fauxgl.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: float64(c.A) / 255}
}

func wrapTexel(u float64, n int) int {
	if n < 1 {
		return 0
	}
	u -= math.Floor(u)
	i := int(u * float64(n))
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}

func wrapTexelF(u float64, maxIdx int) float64 {
	if maxIdx < 1 {
		return 0
	}
	u -= math.Floor(u)
	f := u * float64(maxIdx)
	if f > float64(maxIdx) {
		f = float64(maxIdx)
	}
	return f
}

func wrapTexelI(v, n int) int {
	if n < 1 {
		return 0
	}
	v %= n
	if v < 0 {
		v += n
	}
	return v
}
