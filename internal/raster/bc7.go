package raster

import (
	"fmt"
	"image"
	"image/color"
)

// BC7 uses a least-significant-bit-first bitstream, as do the other BCn
// formats. The tables below are the BPTC partition/anchor tables from the
// BC7 specification.
var bc7A2 = [...]uint8{
	15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
	15, 2, 8, 2, 2, 8, 8, 15, 2, 8, 2, 2, 8, 8, 2, 2,
	15, 15, 6, 8, 2, 8, 15, 15, 2, 8, 2, 2, 2, 15, 15, 6,
	6, 2, 6, 8, 15, 15, 2, 2, 15, 15, 15, 15, 15, 15, 2, 2,
	15,
}

var bc7A3 = [2][64]uint8{
	{
		3, 3, 15, 15, 8, 3, 15, 15, 8, 8, 6, 6, 6, 5, 3, 3,
		3, 3, 8, 15, 3, 3, 6, 10, 5, 8, 8, 6, 8, 5, 15, 15,
		8, 15, 3, 5, 6, 10, 8, 15, 15, 3, 15, 5, 15, 15, 15, 15,
		3, 15, 5, 5, 5, 8, 5, 10, 5, 10, 8, 13, 15, 12, 3, 3,
	},
	{
		15, 8, 8, 3, 15, 15, 3, 8, 15, 15, 15, 15, 15, 15, 15, 8,
		15, 8, 15, 3, 15, 8, 15, 8, 3, 15, 6, 10, 15, 15, 10, 8,
		15, 3, 15, 10, 10, 8, 9, 10, 6, 15, 8, 15, 3, 6, 6, 8,
		15, 3, 15, 15, 15, 15, 15, 15, 15, 15, 15, 3, 15, 15, 8,
	},
}

var bc7Factors = [3][16]uint8{
	{0, 21, 43, 64},
	{0, 9, 18, 27, 37, 46, 55, 64},
	{0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64},
}

var bc7P2 = [...]uint32{
	0xcccc, 0x8888, 0xeeee, 0xecc8, 0xc880, 0xfeec, 0xfec8, 0xec80,
	0xc800, 0xffec, 0xfe80, 0xe800, 0xffe8, 0xff00, 0xfff0, 0xf000,
	0xf710, 0x008e, 0x7100, 0x08ce, 0x008c, 0x7310, 0x3100, 0x8cce,
	0x088c, 0x3110, 0x6666, 0x366c, 0x17e8, 0x0ff0, 0x718e, 0x399c,
	0xaaaa, 0xf0f0, 0x5a5a, 0x33cc, 0x3c3c, 0x55aa, 0x9696, 0xa55a,
	0x73ce, 0x13c8, 0x324c, 0x3bdc, 0x6996, 0xc33c, 0x9966, 0x0660,
	0x0272, 0x04e4, 0x4e40, 0x2720, 0xc936, 0x936c, 0x39c6, 0x639c,
	0x9336, 0x9cc6, 0x817e, 0xe718, 0xccf0, 0x0fcc, 0x7744, 0xee22,
}

var bc7P3 = [...]uint32{
	0xaa685050, 0x6a5a5040, 0x5a5a4200, 0x5450a0a8, 0xa5a50000, 0xa0a05050, 0x5555a0a0, 0x5a5a5050,
	0xaa550000, 0xaa555500, 0xaaaa5500, 0x90909090, 0x94949494, 0xa4a4a4a4, 0xa9a59450, 0x2a0a4250,
	0xa5945040, 0x0a425054, 0xa5a5a500, 0x55a0a0a0, 0xa8a85454, 0x6a6a4040, 0xa4a45000, 0x1a1a0500,
	0x0050a4a4, 0xaaa59090, 0x14696914, 0x69691400, 0xa08585a0, 0xaa821414, 0x50a4a450, 0x6a5a0200,
	0xa9a58000, 0x5090a0a8, 0xa8a09050, 0x24242424, 0x00aa5500, 0x24924924, 0x24499224, 0x50a50a50,
	0x500aa550, 0xaaaa4444, 0x66660000, 0xa5a0a5a0, 0x50a050a0, 0x69286928, 0x44aaaa44, 0x66666600,
	0xaa444444, 0x54a854a8, 0x95809580, 0x96969600, 0xa85454a8, 0x80959580, 0xaa141414, 0x96960000,
	0xaaaa1414, 0xa05050a0, 0xa0a5a5a0, 0x96000000, 0x40804080, 0xa9a8a9a8, 0xaaaaaa44, 0x2a4a5254,
}

type bc7ModeInfo struct {
	subsets, partitionBits, rotationBits, selectionBits int
	colorBits, alphaBits, pbits, sharedPbits            int
	indexBits0, indexBits1                              int
}

var bc7Modes = [...]bc7ModeInfo{
	{3, 4, 0, 0, 4, 0, 1, 0, 3, 0},
	{2, 6, 0, 0, 6, 0, 0, 1, 3, 0},
	{3, 6, 0, 0, 5, 0, 0, 0, 2, 0},
	{2, 6, 0, 0, 7, 0, 1, 0, 2, 0},
	{1, 0, 2, 1, 5, 6, 0, 0, 2, 3},
	{1, 0, 2, 0, 7, 8, 0, 0, 2, 2},
	{1, 0, 0, 0, 7, 7, 1, 0, 4, 0},
	{2, 6, 0, 0, 5, 5, 1, 0, 2, 0},
}

type bcBitReader struct {
	data []byte
	pos  int
}

func (r *bcBitReader) read(n int) uint32 {
	if n == 0 {
		return 0
	}
	var v uint32
	for i := 0; i < n; i++ {
		byteIndex := r.pos >> 3
		if byteIndex >= len(r.data) {
			return v << uint(n-i-1)
		}
		if r.data[byteIndex]&(1<<uint(r.pos&7)) != 0 {
			v |= 1 << uint(n-i-1)
		}
		r.pos++
	}
	return v
}

func bc7Expand(v uint32, bits int) uint8 {
	if bits <= 0 || bits >= 8 {
		return uint8(v)
	}
	s := (v << uint(8-bits)) & 0xff
	return uint8(s | s>>uint(bits))
}

func bc7DecodeBlock(data []byte, out *[16]color.NRGBA) {
	r := bcBitReader{data: data}
	mode := 0
	for mode < 8 && r.read(1) == 0 {
		mode++
	}
	if mode == 8 {
		for i := range out {
			out[i] = color.NRGBA{}
		}
		return
	}
	mi := bc7Modes[mode]
	modePbits := mi.pbits
	if modePbits == 0 {
		modePbits = mi.sharedPbits
	}
	partition := 0
	if mi.partitionBits > 0 {
		partition = int(r.read(mi.partitionBits))
	}
	rotation := 0
	if mi.rotationBits > 0 {
		rotation = int(r.read(mi.rotationBits))
	}
	selection := 0
	if mi.selectionBits > 0 {
		selection = int(r.read(mi.selectionBits))
	}
	var er, eg, eb, ea [6]uint8
	for i := 0; i < mi.subsets; i++ {
		er[i*2] = uint8(r.read(mi.colorBits) << uint(modePbits))
		er[i*2+1] = uint8(r.read(mi.colorBits) << uint(modePbits))
	}
	for i := 0; i < mi.subsets; i++ {
		eg[i*2] = uint8(r.read(mi.colorBits) << uint(modePbits))
		eg[i*2+1] = uint8(r.read(mi.colorBits) << uint(modePbits))
	}
	for i := 0; i < mi.subsets; i++ {
		eb[i*2] = uint8(r.read(mi.colorBits) << uint(modePbits))
		eb[i*2+1] = uint8(r.read(mi.colorBits) << uint(modePbits))
	}
	if mi.alphaBits > 0 {
		for i := 0; i < mi.subsets; i++ {
			ea[i*2] = uint8(r.read(mi.alphaBits) << uint(modePbits))
			ea[i*2+1] = uint8(r.read(mi.alphaBits) << uint(modePbits))
		}
	} else {
		for i := range ea {
			ea[i] = 255
		}
	}
	if modePbits > 0 {
		for i := 0; i < mi.subsets; i++ {
			p0 := uint8(r.read(modePbits))
			p1 := p0
			if mi.sharedPbits == 0 {
				p1 = uint8(r.read(modePbits))
			}
			er[i*2] |= p0
			er[i*2+1] |= p1
			eg[i*2] |= p0
			eg[i*2+1] |= p1
			eb[i*2] |= p0
			eb[i*2+1] |= p1
			ea[i*2] |= p0
			ea[i*2+1] |= p1
		}
	}
	colorBits := mi.colorBits + modePbits
	for i := 0; i < mi.subsets*2; i++ {
		er[i] = bc7Expand(uint32(er[i]), colorBits)
		eg[i] = bc7Expand(uint32(eg[i]), colorBits)
		eb[i] = bc7Expand(uint32(eb[i]), colorBits)
		if mi.alphaBits > 0 {
			ea[i] = bc7Expand(uint32(ea[i]), mi.alphaBits+modePbits)
		}
	}
	hasIndex1 := mi.indexBits1 != 0
	factors0 := bc7Factors[mi.indexBits0-2]
	factors1 := factors0
	if hasIndex1 {
		factors1 = bc7Factors[mi.indexBits1-2]
	}
	offset0, offset1 := 0, mi.subsets*(16*mi.indexBits0-1)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			pixel := y*4 + x
			subset, anchor := 0, 0
			switch mi.subsets {
			case 2:
				subset = int((bc7P2[partition] >> uint(pixel)) & 1)
				if subset != 0 {
					anchor = int(bc7A2[partition])
				}
			case 3:
				subset = int((bc7P3[partition] >> uint(2*pixel)) & 3)
				if subset != 0 {
					anchor = int(bc7A3[subset-1][partition])
				}
			}
			isAnchor := pixel == anchor
			n0 := mi.indexBits0
			if isAnchor {
				n0--
			}
			index0 := int(r.read(n0))
			index1 := index0
			if hasIndex1 {
				n1 := mi.indexBits1
				if isAnchor {
					n1--
				}
				after := r.pos
				r.pos = offset1
				index1 = int(r.read(n1))
				r.pos = after
				offset1 += n1
			}
			offset0 += n0
			_ = offset0
			fc := int(factors0[index0])
			if selection != 0 {
				fc = int(factors1[index1])
			}
			fa := int(factors0[index0])
			if selection == 0 && hasIndex1 {
				fa = int(factors1[index1])
			}
			si := subset * 2
			rr := (int(er[si])*(64-fc) + int(er[si+1])*fc + 32) >> 6
			gg := (int(eg[si])*(64-fc) + int(eg[si+1])*fc + 32) >> 6
			bb := (int(eb[si])*(64-fc) + int(eb[si+1])*fc + 32) >> 6
			aa := (int(ea[si])*(64-fa) + int(ea[si+1])*fa + 32) >> 6
			switch rotation {
			case 1:
				rr, aa = aa, rr
			case 2:
				gg, aa = aa, gg
			case 3:
				bb, aa = aa, bb
			}
			out[pixel] = color.NRGBA{R: uint8(rr), G: uint8(gg), B: uint8(bb), A: uint8(aa)}
		}
	}
}

func decodeBC7Image(data []byte, w, h int) (image.Image, error) {
	if len(data) < ((w+3)/4)*((h+3)/4)*16 {
		return nil, fmt.Errorf("truncated BC7 data")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var block [16]color.NRGBA
	for by := 0; by < (h+3)/4; by++ {
		for bx := 0; bx < (w+3)/4; bx++ {
			off := (by*(w+3)/4 + bx) * 16
			bc7DecodeBlock(data[off:off+16], &block)
			for py := 0; py < 4; py++ {
				for px := 0; px < 4; px++ {
					x, y := bx*4+px, by*4+py
					if x < w && y < h {
						img.SetNRGBA(x, y, block[py*4+px])
					}
				}
			}
		}
	}
	return img, nil
}
