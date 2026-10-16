//go:build windows || linux

package main

import (
	"dow-mod-studio/internal/fx"
	"encoding/binary"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// TestFxSpriteInputRecordLayout pins the bytes appendSpriteInput writes:
// fx_vk_expand.comp reads those exact offsets, and a field moved by four
// bytes still draws particles — just the wrong ones, which an end-to-end
// render would not tell apart.
func TestFxSpriteInputRecordLayout(t *testing.T) {
	r := &fxVkRenderer{spriteBytes: make([]byte, fxComputeSpriteInputFloats*4)}
	sp := fx.Sprite{
		Pos:         [3]float64{1.5, -2, 3.25},
		Radius:      0.75,
		R:           0.25,
		G:           0.5,
		B:           0.75,
		A:           0.5,
		Additive:    true,
		Horizontal:  true,
		PlaneNormal: [3]float64{1, 0, 0},
		Rotation:    0.75,
		SelfLit:     true,
	}
	// The record sits after the count header, at sprite index 3, and carries
	// the site added to the particle's own offset.
	r.appendSpriteInput([3]float64{10, 20, 30}, sp, 3)
	base := fxComputeSpriteCountFloats*4 + 3*fxComputeSpriteStrideFloats*4
	rec := r.spriteBytes[base : base+fxComputeSpriteStrideFloats*4]
	at := func(off int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(rec[off:]))
	}
	for i, want := range []float32{11.5, 18, 33.25} {
		if got := at(i * 4); got != want {
			t.Errorf("pos.%c = %v, want %v (site + particle offset)", "xyz"[i], got, want)
		}
	}
	if at(12) != 0 {
		t.Errorf("pos.w = %v, want the zero lane the shader ignores", at(12))
	}
	if at(16) != 0.75 {
		t.Errorf("radius = %v, want 0.75", at(16))
	}
	for off := 20; off < 32; off++ {
		if rec[off] != 0 {
			t.Fatalf("std430 padding byte %d = %#x, want 0", off, rec[off])
		}
	}
	for i, want := range []float32{0.25, 0.5, 0.75, 0.5} {
		if got := at(32 + 4*i); got != want {
			t.Errorf("tint[%d] = %v, want %v", i, got, want)
		}
	}
	for i, want := range []float32{0.75, 2, 1, 0} {
		if got := at(48 + 4*i); got != want {
			t.Errorf("orientation[%d] = %v, want %v", i, got, want)
		}
	}
	for i, want := range []float32{1, 0, 0, 0} {
		if got := at(64 + 4*i); got != want {
			t.Errorf("planeNormal[%d] = %v, want %v", i, got, want)
		}
	}
	// A particle whose file carries no Radius curve is drawn at the fallback
	// size, and the compute input has to see that the same way the CPU quad
	// build does.
	r.appendSpriteInput([3]float64{}, fx.Sprite{}, 0)
	zeroRecord := fxComputeSpriteCountFloats * 4 // record 0 starts after the count header
	if got := math.Float32frombits(binary.LittleEndian.Uint32(r.spriteBytes[zeroRecord+16:])); got != fxSpriteFallbackRadius {
		t.Errorf("zero-radius sprite recorded %v, want the fallback %v", got, fxSpriteFallbackRadius)
	}
	// Indices outside the buffer are ignored rather than written past it.
	r.appendSpriteInput([3]float64{}, sp, -1)
	r.appendSpriteInput([3]float64{}, sp, fxComputeMaxSprites)
}

// TestFxCPUVertexLayoutCarriesHorizontalOrientationAndLighting pins the CPU
// fallback vertex attributes consumed by fx_vk.vert.
func TestFxCPUVertexLayoutCarriesHorizontalOrientationAndLighting(t *testing.T) {
	r := &fxVkRenderer{vertsData: make([]byte, 0, fxVertexStride*fxVertsPerQuad)}
	sp := fx.Sprite{
		Pos:         [3]float64{1, 2, 3},
		Radius:      2,
		R:           0.2,
		G:           0.4,
		B:           0.6,
		A:           0.8,
		Horizontal:  true,
		PlaneNormal: [3]float64{1, 0, 0},
		Rotation:    math.Pi / 2,
		SelfLit:     true,
	}
	r.appendSprite([3]float64{10, 20, 30}, sp)
	if got, want := len(r.vertsData), fxVertexStride*fxVertsPerQuad; got != want {
		t.Fatalf("CPU sprite vertex bytes = %d, want %d", got, want)
	}
	vertex := r.vertsData[:fxVertexStride]
	at := func(off int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(vertex[off:]))
	}
	for i, want := range []float32{11, 22, 33, -1, -1, 2, 0.2, 0.4, 0.6, 0.8, 0, 1, float32(math.Pi / 2), 2, 1, 0, 1, 0, 0, 0} {
		if got := at(i * 4); got != want {
			t.Errorf("vertex float[%d] = %v, want %v", i, got, want)
		}
	}
}

// TestFxRibbonInputRecordLayout pins the bytes appendRibbonInput writes:
// fx_vk_ribbon.comp reads those exact offsets, and a field out of place still
// draws a ribbon — the wrong shape, which an end-to-end render would not
// separate from the right one.
func TestFxRibbonInputRecordLayout(t *testing.T) {
	r := &fxVkRenderer{ribbonBytes: make([]byte, fxComputeRibbonInputFloats*4)}
	rb := fx.BeamRibbon{
		From:     fauxgl.Vector{X: 1, Y: 2, Z: 3},
		To:       fauxgl.Vector{X: 1, Y: 2, Z: 5.5},
		Arc:      fauxgl.Vector{X: 0.75},
		Across:   fauxgl.Vector{X: 1},
		Width:    0.25,
		Noise:    0.5,
		U0:       0.25,
		U1:       1.5,
		R:        1,
		G:        0.5,
		B:        0.25,
		Alpha:    0.75,
		Segments: 4,
		FlipV:    true,
		Fixed:    true,
		SelfLit:  true,
	}
	eye := fauxgl.Vector{Z: -10}
	if got := r.appendRibbonInput(rb, eye, 3, 42); got != 4*6 {
		t.Fatalf("four segments expanded to %d vertices, want %d (fx.BeamTriangles per quad)", got, 4*6)
	}
	base := fxComputeRibbonCountFloats*4 + 3*fxComputeRibbonStrideFloats*4
	rec := r.ribbonBytes[base : base+fxComputeRibbonStrideFloats*4]
	at := func(off int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(rec[off:]))
	}
	u32 := func(off int) uint32 {
		return binary.LittleEndian.Uint32(rec[off:])
	}
	for _, c := range []struct {
		off  int
		want float32
		what string
	}{
		{0, 1, "from.x"}, {4, 2, "from.y"}, {8, 3, "from.z"}, {12, 0, "from.w"},
		{16, 1, "to.x"}, {20, 2, "to.y"}, {24, 5.5, "to.z"}, {28, 0, "to.w"},
		{32, 0.75, "arc.x"}, {36, 0, "arc.y"}, {40, 0, "arc.z"},
		{48, 1, "across.x"}, {60, 1, "across.w (fixed)"},
		{64, 1, "tint.r"}, {68, 0.5, "tint.g"}, {72, 0.25, "tint.b"}, {76, 0.75, "tint.a"},
		{80, 0.25, "width"}, {84, 0.5, "noise"}, {88, 0.25, "u0"}, {92, 1.5, "u1"},
		{96, 0, "eye.x"}, {100, 0, "eye.y"}, {104, -10, "eye.z"},
	} {
		if got := at(c.off); got != c.want {
			t.Errorf("%s = %v, want %v", c.what, got, c.want)
		}
	}
	if u32(112) != 4 {
		t.Errorf("segments = %d, want 4", u32(112))
	}
	if u32(116) != 3 {
		t.Errorf("flags = %d, want flipV and self-lit bits set", u32(116))
	}
	if u32(120) != 42 {
		t.Errorf("vertex offset = %d, want 42", u32(120))
	}
	// A ribbon the tessellator would refuse produces no record and no
	// vertices, so the group's range never claims them.
	origin := fauxgl.Vector{}
	if n := r.appendRibbonInput(fx.BeamRibbon{From: origin, To: origin, Width: 0.2}, eye, 0, 0); n != 0 {
		t.Errorf("empty span wrote %d vertices, want none", n)
	}
	if n := r.appendRibbonInput(fx.BeamRibbon{From: origin, To: origin.Add(fauxgl.Vector{Z: 2}), Width: 0}, eye, 0, 0); n != 0 {
		t.Errorf("zero width wrote %d vertices, want none", n)
	}
	if n := r.appendRibbonInput(rb, eye, -1, 0); n != 0 {
		t.Errorf("out-of-range index wrote %d vertices, want none", n)
	}
}
