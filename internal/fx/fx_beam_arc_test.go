package fx_test

import (
	"dow-mod-studio/internal/fx"
	"math"
	"testing"

	"github.com/fogleman/fauxgl"
)

// A beam's Arc is a bow: it moves the middle of the ribbon off the straight
// line between the two ends. Only the part of it that crosses the span can do
// that. The part that runs along the span just lengthens the ribbon, which is
// invisible — the curve the effect asked for never appears.
//
// The shipped files write Arc as a plain vector in model space, and most of
// them write it on Y, so a beam that happens to run vertically spends almost
// all of its bow on its own length. necron\ground_lightning_01 arcs 0.5 on Y
// between a ground marker and a chest marker 6.3 units apart, with the span
// running at 0.95 vertical: only 31% of that bow crossed the line, and the
// bolt came out straight.

// arcLateralShare is what fraction of a ribbon's bow crosses its span. The
// measurement differences two tessellations — the ribbon as authored against
// the same ribbon with the arc taken away and the noise cleared on both sides,
// since the wobble is across the span too and would otherwise be counted as
// part of the bow.
func arcLateralShare(r fx.BeamRibbon, eye fauxgl.Vector) (share, peak float64, ok bool) {
	withArc := r
	withArc.Noise = 0
	flat := withArc
	flat.Arc = fauxgl.Vector{}

	withVerts := fx.BeamQuads(withArc, eye)
	flatVerts := fx.BeamQuads(flat, eye)
	if len(withVerts) == 0 || len(withVerts) != len(flatVerts) {
		return 0, 0, false
	}
	dir := r.To.Sub(r.From)
	if n := dir.Length(); n > 1e-9 {
		dir = dir.MulScalar(1 / n)
	} else {
		return 0, 0, false
	}
	lateral := 0.0
	for i := range withVerts {
		w := withVerts[i].Pos.Sub(flatVerts[i].Pos)
		d := w.Length()
		if d > peak {
			peak = d
		}
		along := w.Dot(dir)
		if l := w.Sub(dir.MulScalar(along)).Length(); l > lateral {
			lateral = l
		}
	}
	if peak < 1e-9 {
		return 0, 0, true
	}
	return lateral / peak, peak, true
}

// A beam across the three axes, with an arc on each of them, must bow across
// its span every time. This is the case that was broken: an arc on the same
// axis the beam runs along has no across-component left to bow with.
func TestBeamArcAlwaysBowsAcrossTheSpan(t *testing.T) {
	eye := fauxgl.Vector{X: 2, Y: 1, Z: 3}
	spans := []struct {
		name string
		to   fauxgl.Vector
	}{
		{"along X", fauxgl.Vector{X: 6}},
		{"along Y, the case that was straight", fauxgl.Vector{Y: -6}},
		{"along Z", fauxgl.Vector{Z: 6}},
		{"diagonal, like the ork surge", fauxgl.Vector{X: 0.49, Y: 0.83, Z: 0.28}},
	}
	arcs := []struct {
		name string
		arc  fauxgl.Vector
	}{
		{"on Y, as the shipped lightning writes it", fauxgl.Vector{Y: 0.5}},
		{"on X", fauxgl.Vector{X: 0.5}},
		{"on Z", fauxgl.Vector{Z: 0.5}},
		{"diagonal", fauxgl.Vector{X: 0.3, Y: -0.2, Z: 0.4}},
	}
	for _, s := range spans {
		for _, a := range arcs {
			r := fx.BeamRibbon{
				From: fauxgl.Vector{}, To: s.to,
				Width: 0.5, Segments: 8,
				Arc: a.arc,
			}
			share, peak, ok := arcLateralShare(r, eye)
			if !ok {
				t.Fatalf("%s / %s: produced no comparable geometry", s.name, a.name)
			}
			if peak < 1e-9 {
				t.Errorf("%s / %s: the arc of %v bowed nothing at all, so the beam draws straight",
					s.name, a.name, a.arc)
				continue
			}
			if share < 0.99 {
				t.Errorf("%s / %s: arc %v bowed %.3f, of which only %.0f%% crossed the span",
					s.name, a.name, a.arc, peak, share*100)
			}
		}
	}
}

// The bow must still reach as far as the file asked, measured across the span.
// Projecting the arc drops the part that ran along the span, so a beam whose
// arc pointed mostly the wrong way keeps only what was left of it — the bow
// gets shallower. That is the honest reading of the file, but it must not go
// to nothing: an effect that wrote an arc of 0.5 should still curve.
func TestBeamArcKeepsItsDepthAcrossTheSpan(t *testing.T) {
	eye := fauxgl.Vector{X: 2, Y: 1, Z: 3}
	// The lightning as shipped: ground marker to chest marker, 6.31 apart,
	// span running at 0.95 vertical, arc 0.5 on Y.
	from := fauxgl.Vector{X: 0.8, Y: 1.2, Z: -0.4}
	to := fauxgl.Vector{X: 2.6, Y: -4.8, Z: -1.2}
	r := fx.BeamRibbon{
		From: from, To: to,
		Width: 0.5, Segments: 8,
		Arc: fauxgl.Vector{Y: -0.5},
	}
	share, peak, ok := arcLateralShare(r, eye)
	if !ok {
		t.Fatal("produced no comparable geometry")
	}
	if share < 0.99 {
		t.Errorf("the lightning's arc bowed only %.0f%% across its span", share*100)
	}
	// sin(pi/2) is 1 at the middle, so the peak bow is the arc's depth there.
	if peak < 0.05 {
		t.Errorf("the arc reached %.3f across a 6.3 unit span, which is no visible bow at all", peak)
	}
	t.Logf("ground lightning: arc 0.5 on Y across a %.1f vertical span bows %.3f across (%.0f%%)",
		to.Sub(from).Length(), peak, share*100)
}

// An arc that is exactly parallel to the span has nothing across it to keep,
// so the projection would leave a beam straight. The effect still asked for a
// curve, so the ribbon bows across in the direction it already widens.
func TestBeamArcParallelToSpanStillBows(t *testing.T) {
	eye := fauxgl.Vector{X: 2, Y: 1, Z: 3}
	r := fx.BeamRibbon{
		From: fauxgl.Vector{}, To: fauxgl.Vector{X: 4},
		Width: 0.5, Segments: 8,
		Arc: fauxgl.Vector{X: 0.5}, // exactly along the span
	}
	share, peak, ok := arcLateralShare(r, eye)
	if !ok {
		t.Fatal("produced no comparable geometry")
	}
	if peak < 1e-6 {
		t.Fatal("an arc written exactly along the span was dropped entirely, leaving a straight beam")
	}
	if share < 0.99 {
		t.Errorf("the fallback bow crossed only %.0f%% of the span", share*100)
	}
}

// A beam with no arc must still be drawn straight, and must not pick up a bow
// from the fallback above.
func TestBeamWithoutArcIsStraight(t *testing.T) {
	eye := fauxgl.Vector{X: 2, Y: 1, Z: 3}
	r := fx.BeamRibbon{
		From: fauxgl.Vector{}, To: fauxgl.Vector{Y: 4},
		Width: 0.5, Segments: 8,
	}
	share, peak, ok := arcLateralShare(r, eye)
	if !ok {
		t.Fatal("produced no comparable geometry")
	}
	if peak > 1e-9 {
		t.Errorf("a beam with no arc moved %.3f", peak)
	}
	// The drawn centreline must be the straight line between the ends: two
	// verts per step, the span is 4 long and there are 8 of them.
	const segments = 8
	verts := fx.BeamQuads(r, eye)
	for i := 0; i < len(verts); i += 2 {
		centre := verts[i].Pos.Add(verts[i+1].Pos).MulScalar(0.5)
		want := fauxgl.Vector{Y: 4 * float64(i/2) / segments}
		if centre.Sub(want).Length() > 1e-6 {
			t.Errorf("step %d drew its middle at %v, want the straight line at %v", i/2, centre, want)
		}
	}
	_ = share
}

// A ring is drawn in its own plane rather than turned towards the eye, so it
// has a fixed across direction. The bow has to use that same direction for the
// fallback, and must not drag the ribbon out of its plane.
func TestBeamArcRespectsAFixedPlane(t *testing.T) {
	from := fauxgl.Vector{}
	to := fauxgl.Vector{X: 4}
	r := fx.BeamRibbon{
		From: from, To: to,
		Width: 0.5, Segments: 8,
		Arc:   fauxgl.Vector{X: 0.5}, // parallel to the span: the fallback case
		Fixed: true,
		// A ring lying in the YZ plane of its own frame.
		Across: fauxgl.Vector{Y: 1},
	}
	verts := fx.BeamQuads(r, fauxgl.Vector{X: 2, Y: 1, Z: 3})
	if len(verts) == 0 {
		t.Fatal("produced no geometry")
	}
	// Every vertex must stay in the plane the fixed across describes: the
	// ribbon spans X and bows on Y, so Z stays zero throughout.
	for i, v := range verts {
		if math.Abs(v.Pos.Z) > 1e-6 {
			t.Fatalf("vertex %d left the fixed plane at z=%.4f", i, v.Pos.Z)
		}
	}
	// And it must actually have bowed, on Y.
	mid := verts[len(verts)/2].Pos
	if math.Abs(mid.Y) < 1e-6 {
		t.Error("a fixed-plane ribbon with a parallel arc drew no bow")
	}
}
