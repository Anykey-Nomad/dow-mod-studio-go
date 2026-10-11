package main

import (
	"dow-mod-studio/internal/fx"
	"image"
	"image/color"
	"testing"

	"github.com/fogleman/fauxgl"
)

// A map that is transparent round its subject has to leave that subject
// alone. The necron crystal's panels are the case in the shipped art: the
// quad is a square of the node's Radius, and the map inside it is 78% empty,
// the shape of what is left being a tapered facet. Filling the empty space
// with the node's flat colour draws the whole square, which is why the
// crystal came out as a heap of green squares rather than a gem.
//
// The map is drawn here through the overlay's own ribbon path so the test
// covers the rule where it is applied, and not a copy of it.
func TestRingPanelLeavesItsTransparentMarginEmpty(t *testing.T) {
	const w, h = 96, 96
	// A 16x16 map with an opaque column down the middle and nothing else:
	// the same layout as CRYSTAL_BOT_01, a shape in a square of empty map.
	const n = 16
	tex := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if x >= 6 && x < 10 {
				tex.SetRGBA(x, y, color.RGBA{R: 40, G: 220, B: 60, A: 255})
				continue
			}
			tex.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 0})
		}
	}
	sim := &fx.FxSim{}
	sim.RingNodes = []fx.Ring{{
		Texture:  "panel",
		Blending: 1,
		Lifetime: 1,
		Radius:   []fx.FxKey{{T: 0, V: 1.2}, {T: 1, V: 1.2}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}
	opts := RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 6, Fov: 45}
	// The marker's own frame: up, outward and the way round the ring. All
	// three are needed - a ring laid out with its span and its width on the
	// same axis has no quad to draw at all.
	effects := []fxOverlayEffect{{Sim: sim,
		Right: fauxgl.Vector{X: 1},
		Up:    fauxgl.Vector{Y: 1},
		Axis:  fauxgl.Vector{Z: 1},
		Frame: [3]fauxgl.Vector{
			{X: 0, Y: 0, Z: 1}, // up
			{X: 1},             // outward
			{Y: 1},             // the way round
		}}}
	out := renderFxOverlay(effects, opts, w, h, map[string]*image.RGBA{"": tex})

	centre := out.RGBAAt(w/2, h/2)
	corner := out.RGBAAt(2, 2)
	if centre.A == 0 {
		t.Fatalf("the panel itself was not drawn: centre alpha 0")
	}
	if corner.A != 0 {
		t.Errorf("the empty margin of the map was filled in: corner is %#v, want nothing drawn", corner)
	}
	// The column must be a column: narrow beside the square it sits in. This
	// is the part that fails when the margin is filled, since the fill is
	// what makes the quad read as a square.
	drawn := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if out.RGBAAt(x, y).A != 0 {
				drawn++
			}
		}
	}
	share := float64(drawn) / float64(w*h)
	t.Logf("centre %#v, corner %#v, %d of %d pixels (%.1f%%) drawn", centre, corner, drawn, w*h, share*100)
	if share > 0.5 {
		t.Errorf("the panel covers %.1f%% of the frame: the map's empty margin is being drawn", share*100)
	}
}

// A map with no alpha of its own still fills the quad, which is the case the
// old rule was reaching for: a map that decodes fully opaque must not turn
// into a hole.
func TestOpaqueRingMapStillFillsItsQuad(t *testing.T) {
	const w, h = 96, 96
	const n = 16
	tex := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := range tex.Pix {
		tex.Pix[i] = 255
	}
	sim := &fx.FxSim{}
	sim.RingNodes = []fx.Ring{{
		Texture:  "flash",
		Blending: 1,
		Lifetime: 1,
		Radius:   []fx.FxKey{{T: 0, V: 1.2}, {T: 1, V: 1.2}},
		Colour:   []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
	}}
	opts := RenderOptions{Width: w, Height: h, Yaw: 0.7, Pitch: 0.2, Distance: 6, Fov: 45}
	effects := []fxOverlayEffect{{Sim: sim,
		Right: fauxgl.Vector{X: 1},
		Up:    fauxgl.Vector{Y: 1},
		Axis:  fauxgl.Vector{Z: 1},
		Frame: [3]fauxgl.Vector{
			{X: 0, Y: 0, Z: 1}, // up
			{X: 1},             // outward
			{Y: 1},             // the way round
		}}}
	out := renderFxOverlay(effects, opts, w, h, map[string]*image.RGBA{"": tex})
	drawn := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if out.RGBAAt(x, y).A != 0 {
				drawn++
			}
		}
	}
	if drawn == 0 {
		t.Fatal("an opaque map drew nothing at all")
	}
	t.Logf("an opaque map fills %d of %d pixels", drawn, w*h)
}

// The SpinEpsilon curves scatter the copies of one panel over the turn they
// name, so a file holding a single ring has no copies to scatter. Every
// crystal panel is a file of its own: the model lays eight of them out
// around the gem on eight markers, each with SpinEpsilonZ 1 and SpinRandom 1.
// Scattering a lone panel turns it by an angle no marker asked for, and eight
// panels at eight unrelated tilts are not a crystal.
func TestLoneRingIsNotTurnedByItsSpinEpsilon(t *testing.T) {
	panel := func() *fx.FxSim {
		s := &fx.FxSim{}
		s.RingNodes = []fx.Ring{{
			Texture:    "TEXTURES_NEW/NECRON/CRYSTAL_BOT_01.TGA",
			Blending:   1,
			Lifetime:   1,
			Radius:     []fx.FxKey{{T: 0, V: 1.36}, {T: 1, V: 1.36}},
			SpinZ:      []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0}},
			EpsZ:       []fx.FxKey{{T: 0, V: 1}},
			SpinRandom: true,
			Colour:     []fx.FxColourStop{{R: 1, G: 1, B: 1, A: 1}},
		}}
		return s
	}
	centre := fauxgl.Vector{Z: 8.977}
	right := fauxgl.Vector{X: -0.154, Y: 0.154, Z: 0.976}
	up := fauxgl.Vector{X: 0.690, Y: -0.690, Z: 0.218}
	side := fauxgl.Vector{X: 0.707, Y: 0.707}

	lone := panel().Rings(centre, side, right, up, fauxgl.Vector{Z: 1})
	if len(lone) != 1 {
		t.Fatalf("a file with one ring drew %d quads", len(lone))
	}
	// The same panel with its epsilon taken away, which is what the model
	// wants for a facet that has a marker of its own to stand on.
	upright := panel()
	upright.RingNodes[0].EpsZ = nil
	want := upright.Rings(centre, side, right, up, fauxgl.Vector{Z: 1})[0]
	got := lone[0]
	if got.Across.Sub(want.Across).Length() > 1e-9 {
		t.Errorf("a lone panel was turned by its SpinEpsilon: across %v, an unspread panel draws %v",
			got.Across, want.Across)
	}

	// A file that really does hold several rings still has its spread: that
	// is the case the curves are for.
	many := &fx.FxSim{}
	many.RingNodes = []fx.Ring{
		{Texture: "a", Lifetime: 1, Radius: []fx.FxKey{{V: 1}}, SpinZ: []fx.FxKey{{V: 0}},
			EpsZ: []fx.FxKey{{V: 1}}, SpinRandom: true},
		{Texture: "a", Lifetime: 1, Radius: []fx.FxKey{{V: 1}}, SpinZ: []fx.FxKey{{V: 0}},
			EpsZ: []fx.FxKey{{V: 1}}, SpinRandom: true},
	}
	both := many.Rings(centre, side, right, up, fauxgl.Vector{Z: 1})
	if len(both) != 2 {
		t.Fatalf("a file with two rings drew %d quads", len(both))
	}
	if both[0].Across.Sub(both[1].Across).Length() < 1e-6 {
		t.Error("two rings of one file came out on the same face, so the spread never reached them")
	}
}
