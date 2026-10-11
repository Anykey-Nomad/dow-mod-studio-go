package fx

import (
	"math"

	"github.com/fogleman/fauxgl"
)

// A STYLE_BEAM node is not a particle. The engine draws it as a ribbon of
// quads stretched between two points, which is why the event that plays the
// effect has to name a second marker and ask for the beam to span the
// distance between the two. Nothing in the .lua says so: a beam file carries
// Length, Width, Segments and a scrolling Texture_U, and the endpoints are the
// one thing it cannot know by itself.
//
//   marker = marker_gun_tube_hilight     the near end
//   fx_other_marker = marker_gun_tube_01  the far end
//   fx_length = distance                  span exactly what is between them
//   fx_transform = self_to_other          aim the ribbon along the two
//
// That quartet is in the .whe file beside the model, not in the effect, which
// is why a beam that only reads its .lua has nothing to stretch between.

// fxBeamCurveMax caps a curve read into an animation key. The engine has no
// need for more and a file that writes them is already unreadable.
const fxBeamCurveMax = 64

// MaxBeamSegments caps the Segments property of a beam. The engine splits a
// beam into this many quads along its length; the files ask for five or eight,
// and the cap is what the vertex buffer is sized against.
const MaxBeamSegments = 32

// Beam is one STYLE_BEAM node: the ribbon's own shape, animation and map,
// with the endpoints filled in per frame from the markers the event named.
type Beam struct {
	Texture  string
	Blending int
	Lifetime float64

	Length   []FxKey // span, as a fraction of the gap between the markers
	Width    []FxKey // half width in model units
	Segments []FxKey
	Noise    []FxKey // sideways wobble of the ribbon, in model units
	UOffset  []FxKey // scroll along the ribbon
	URepeat  []FxKey
	Colour   []FxColourStop
	Arc      []FxVec3Key // bow of the ribbon, so a beam can curve
	Offset   []FxVec3Key
	// selfLit is the node's SelfIlluminated. No shipped beam writes it, so
	// this is false everywhere and a beam is dimmed by the scene's light
	// along with the model — which is what an engine that lights its FX in
	// the same pass as its models does.
	SelfLit bool
	// depthTest is the node's UseDepthTest, which the game writes as 0 on
	// every beam in these files: a beam is a glow that shows through the
	// unit firing it, which is most of what makes it read as energy.
	DepthTest bool
}

// beamFromNode reads a STYLE_BEAM node, reporting false for anything else.
func beamFromNode(n *FxNode) (Beam, bool) {
	if n.Style != "STYLE_BEAM" {
		return Beam{}, false
	}
	b := Beam{
		Texture:   n.String("Texture"),
		Blending:  int(n.Float("Blending", 2)),
		Lifetime:  n.Float("Duration", 1),
		DepthTest: n.Bool("UseDepthTest", false),
		SelfLit:   n.Bool("SelfIlluminated", false),
	}
	b.Length = beamCurve(n, "Length")
	b.Width = beamCurve(n, "Width")
	b.Segments = beamCurve(n, "Segments")
	b.Noise = beamCurve(n, "Noise")
	b.UOffset = beamCurve(n, "Texture_U_Offset")
	b.URepeat = beamCurve(n, "Texture_U_Repeat")
	if p, ok := n.Prop("Colour"); ok {
		b.Colour = p.Colour
	}
	if p, ok := n.Prop("Arc"); ok {
		b.Arc = p.Vec3
	}
	if p, ok := n.Prop("Offset"); ok {
		b.Offset = p.Vec3
	}
	if b.Lifetime <= 0 {
		b.Lifetime = 1
	}
	return b, true
}

// beamCurve reads one animation property of a beam, falling back to a
// constant curve so the sampler always has something to work with.
func beamCurve(n *FxNode, name string) []FxKey {
	p, ok := n.Prop(name)
	if !ok || len(p.Curve) == 0 {
		return nil
	}
	if len(p.Curve) > fxBeamCurveMax {
		return p.Curve[:fxBeamCurveMax]
	}
	return p.Curve
}

// spinCurve reads a Spin or SpinEpsilon curve, and it takes a plain number
// as readily as a curve.
//
// The two are written differently in the same kind of file, which is why this
// is not just beamCurve: the crystal's Spin curves are
// VARTYPE_ARRAY_TIMEFLOAT while its SpinEpsilonZ is a bare VARTYPE_FLOAT. Read
// as a curve alone, the epsilon comes back empty and every panel of the
// crystal draws square to its marker — which is what the fixed angles of the
// Spin curves alone would give.
func spinCurve(n *FxNode, name string) []FxKey {
	if c := beamCurve(n, name); c != nil {
		return c
	}
	p, ok := n.Prop(name)
	if !ok {
		return nil
	}
	// A bare number is a constant for the whole life of the effect.
	if p.Type == "VARTYPE_FLOAT" || p.Type == "VARTYPE_INT" {
		return []FxKey{{T: 0, V: p.Num}, {T: 1, V: p.Num}}
	}
	return nil
}

// BeamRibbon is one beam's geometry for this frame: the two ends it spans
// and the colour it has aged to.
type BeamRibbon struct {
	// From and To are the two ends the ribbon spans, To reached by walking
	// the Length fraction of the way from From along the gap.
	From, To fauxgl.Vector
	Width    float64
	R, G, B  float64
	Alpha    float64
	Texture  string
	// U0 is where the map starts along the ribbon and U1 where it ends, so
	// the Texture_U_Offset and Texture_U_Repeat curves can scroll it and
	// tile it.
	U0, U1   float64
	Segments int
	Noise    float64
	Arc      fauxgl.Vector
	Additive bool
	// SelfLit is the node's own SelfIlluminated. A ribbon that is not self
	// illuminated is scaled by the scene's light the way a lit model is, so
	// that turning the light down dims the beams with it.
	SelfLit bool
	// Across is the direction the ribbon is widened in. A beam leaves it
	// empty and lets the tessellator turn the cross-section towards the eye,
	// which is what makes a beam read as a shaft from any angle; a ring sets
	// it and Fixed, because a ring lying on a surface must stay in its own
	// plane rather than swing to face the camera.
	Across    fauxgl.Vector
	Fixed     bool
	Billboard bool
	// FlipV puts the map's top row at the far side of the ribbon from the
	// one it would otherwise sit on. A beam's map runs along its length, so
	// which way up it is only a question of taste; a ring lying in a
	// marker's plane is not, because the map's own shape says which way the
	// panel faces: the crystal's lower facets are drawn narrow at the top of
	// their map and widen downward, which is the gem upside down.
	FlipV bool
	// DepthTest is the node's own UseDepthTest. The Vulkan pass draws beams
	// through the model either way, since these files write it off, but the
	// value is carried through so the two paths can be told apart.
	DepthTest bool
}

// BeamRibbon samples a beam at normalised age u for the given endpoints.
func NewBeamRibbon(b Beam, from, to fauxgl.Vector, u float64, spans bool) BeamRibbon {
	dist := to.Sub(from).Length()
	// The Length curve means two different things, and which one it is falls
	// to the event rather than to the file. An event that named a second
	// marker and asked for fx_length = distance wants a fraction of the gap
	// between the two, so the ribbon spans the markers even though the file
	// wrote 0.8. An event with no second marker to measure against wants a
	// length in model units, and there the number is the span outright:
	// fire_muzzle_beam_combo writes 6.23, which read as a fraction is above
	// one and clamps to the whole gap, leaving the beam a stub at the muzzle
	// pointing wherever the marker's own axis happened to fall.
	spans = spans && len(b.Length) > 0
	fraction := SampleCurve(b.Length, u)
	if !spans {
		// Model units, so nothing is clamped: zero is a beam that has not
		// left the muzzle yet, which is how a file that ramps its length up
		// from nothing is meant to read.
		if fraction < 0 {
			fraction = 0
		}
	} else if fraction <= 0 || fraction > 1 {
		fraction = 1
	}
	segments := int(SampleCurve(b.Segments, u) + 0.5)
	if segments < 1 {
		segments = 1
	}
	if segments > MaxBeamSegments {
		segments = MaxBeamSegments
	}
	r := BeamRibbon{
		From:      from,
		To:        to,
		Width:     SampleCurve(b.Width, u),
		Segments:  segments,
		Texture:   b.Texture,
		Additive:  b.Blending == 2,
		DepthTest: b.DepthTest,
		Noise:     SampleCurve(b.Noise, u),
		Alpha:     1,
		SelfLit:   b.SelfLit,
	}
	// The scroll and the tiling are two halves of one coordinate: where the
	// map sits and how many times it runs along the ribbon.
	scroll := SampleCurve(b.UOffset, u)
	repeat := SampleCurve(b.URepeat, u)
	r.U0 = scroll
	r.U1 = scroll + repeat
	// The far end is the Length along the gap: a fraction of it when the beam
	// spans two markers, and that many model units when it runs on its own.
	if dist > 1e-9 {
		span := fraction
		if !spans {
			span = fraction / dist
		}
		r.To = from.Add(to.Sub(from).MulScalar(span))
	}
	if st := sampleColour(b.Colour, u); st != nil {
		r.R, r.G, r.B, r.Alpha = st.R, st.G, st.B, st.A
	}
	if a, ok := vec3Sample(b.Arc, u); ok {
		r.Arc = FileVec(a)
	}
	if o, ok := vec3Sample(b.Offset, u); ok {
		r.From = r.From.Add(FileVec(o))
	}
	return r
}

// vec3Sample samples a time/vector curve the way the scalar curves are
// sampled, reporting false when the curve is empty.
func vec3Sample(keys []FxVec3Key, u float64) (fauxgl.Vector, bool) {
	if len(keys) == 0 {
		return fauxgl.Vector{}, false
	}
	if len(keys) == 1 {
		k := keys[0]
		return fauxgl.Vector{X: k.X, Y: k.Y, Z: k.Z}, true
	}
	span := keys[len(keys)-1].T - keys[0].T
	if span <= 0 {
		k := keys[0]
		return fauxgl.Vector{X: k.X, Y: k.Y, Z: k.Z}, true
	}
	if u < 0 {
		u = 0
	}
	if u > 1 {
		u = 1
	}
	t := keys[0].T + u*span
	for i := 1; i < len(keys); i++ {
		if t <= keys[i].T {
			a, b := keys[i-1], keys[i]
			if b.T <= a.T {
				return fauxgl.Vector{X: b.X, Y: b.Y, Z: b.Z}, true
			}
			f := (t - a.T) / (b.T - a.T)
			return fauxgl.Vector{
				X: a.X + f*(b.X-a.X),
				Y: a.Y + f*(b.Y-a.Y),
				Z: a.Z + f*(b.Z-a.Z),
			}, true
		}
	}
	k := keys[len(keys)-1]
	return fauxgl.Vector{X: k.X, Y: k.Y, Z: k.Z}, true
}

// BeamVert is one corner of one quad of a ribbon, in model space with the
// map coordinate it reads. Two of these make an edge; a run of them along the
// ribbon makes the beam.
type BeamVert struct {
	Pos fauxgl.Vector
	U   float64
	V   float64
}

// BeamQuads tessellates one ribbon into quads. The engine builds a beam as a
// strip that walks the axis in Segments steps and puts a cross-section at each
// step: the cross-section is the Width across the beam, turned to face the eye
// as much as the axis allows, which is what keeps a beam reading as a round
// shaft rather than a flat card seen edge on. Noise nudges each step sideways
// and Arc bows the whole strip.
//
// The same tessellation feeds the Vulkan path and the CPU overlay, so the two
// cannot drift apart.
func BeamQuads(r BeamRibbon, eye fauxgl.Vector) []BeamVert {
	axis := r.To.Sub(r.From)
	length := axis.Length()
	if length < 1e-9 || r.Width <= 0 {
		return nil
	}
	dir := axis.MulScalar(1 / length)
	// The direction the ribbon turns across: perpendicular to the axis, and
	// otherwise as close to the eye as it can be, which is what faces it.
	var across fauxgl.Vector
	if r.Fixed {
		across = r.Across
	} else {
		across = eye.Sub(r.From).Cross(dir)
	}
	if n := across.Length(); n < 1e-6 {
		// Looking straight along the beam: any perpendicular will do, since
		// there is no width to speak of from here anyway.
		across = dir.Cross(fauxgl.Vector{X: 0, Y: 0, Z: 1})
		if n := across.Length(); n < 1e-6 {
			across = dir.Cross(fauxgl.Vector{X: 1})
		}
		if n := across.Length(); n < 1e-6 {
			return nil
		}
	}
	across = across.Normalize()
	// The bow is a displacement across the span, so the part of the arc that
	// runs along the span is not a bow at all: it lengthens the ribbon and
	// leaves it straight. Only the part across the span is kept, which is the
	// whole of what the arc means.
	//
	// The arc arrives already swapped into the renderer's axes by FileVec,
	// like every other vector these files write. It used to go in raw, on a
	// note that most beams write their arc on Y: across the 2002 shipped
	// effects not one arc is written on a single axis at all, and where one
	// is written on all three the third is the largest in every case, so that
	// note had the convention backwards as well as the axes unconverted.
	bow := r.Arc.Sub(dir.MulScalar(r.Arc.Dot(dir)))
	if bow.Length() < 1e-6 && r.Arc.Length() > 1e-6 {
		// The arc points exactly along the span, so there is nothing across
		// it to keep and the bow would vanish. The effect still asked for
		// one, so bow across it in the direction the ribbon already widens:
		// which way round it goes is a choice, but a beam that curves at
		// all is closer to the authored effect than a straight one.
		bow = across
	}
	steps := r.Segments
	if steps < 1 {
		steps = 1
	}
	verts := make([]BeamVert, 0, (steps+1)*2)
	for i := 0; i <= steps; i++ {
		f := float64(i) / float64(steps)
		p := r.From.Add(axis.MulScalar(f))
		// The bow and the wobble both push the middle of the ribbon off the
		// straight line between the two ends.
		bowAt := bow.MulScalar(math.Sin(f * math.Pi))
		wobble := across.MulScalar(0)
		if r.Noise != 0 {
			// Deterministic in f, so the ribbon does not crawl between
			// frames: the engine's noise is a function of position
			// along the beam, not of the clock.
			wobble = across.MulScalar(r.Noise * math.Sin(f*math.Pi*3.0+r.U0))
		}
		centre := p.Add(bowAt).Add(wobble)
		half := r.Width
		lo, hi := 0.0, 1.0
		if r.FlipV {
			lo, hi = 1, 0
		}
		verts = append(verts,
			BeamVert{Pos: centre.Sub(across.MulScalar(half)), U: r.U0 + (r.U1-r.U0)*f, V: lo},
			BeamVert{Pos: centre.Add(across.MulScalar(half)), U: r.U0 + (r.U1-r.U0)*f, V: hi},
		)
	}
	return verts
}

// BeamTriangles turns the paired verts of a ribbon into indices, one quad at
// a time down the strip.
func BeamTriangles(verts []BeamVert) []uint16 {
	n := len(verts) / 2
	if n < 2 {
		return nil
	}
	out := make([]uint16, 0, (n-1)*6)
	for i := 0; i+1 < n; i++ {
		a := uint16(i * 2)
		out = append(out, a, a+1, a+3, a, a+3, a+2)
	}
	return out
}

// A STYLE_RING is the third shape the engine knows, and by a long way the
// commonest: a flat quad of the node's Radius, sitting at the node's Offset in
// its marker's own frame, turned by the Spin curves and facing either the eye
// or the marker's plane as Billboard says. Nothing about it sprays and nothing
// about it spans two points, so reading it as a particle leaves it a disc
// pinned at the origin — which is why the ring at the muzzle of the very
// effect this file plays draws in the wrong place.
type Ring struct {
	Texture   string
	Blending  int
	Lifetime  float64
	billboard bool
	// selfLit is the node's SelfIlluminated, which 86 of the 218 shipped
	// rings set and 132 leave off: the two kinds are drawn at different
	// brightness under the same light.
	SelfLit bool
	Radius  []FxKey
	Colour  []FxColourStop
	Offset  []FxVec3Key
	spinX   []FxKey
	SpinY   []FxKey
	SpinZ   []FxKey
	Loop    bool
	// epsX, epsY and epsZ are the node's SpinEpsilon curves: how much of an
	// extra turn each axis may be given on top of its Spin curve, so that
	// several copies of one panel come out facing different ways. spinRandom
	// is the node's SpinRandom, which asks for that spread to be picked at
	// random rather than assigned in order.
	//
	// This is what puts the faces of the necron crystal apart. Its panels are
	// one STYLE_RING each with a fixed SpinZ, and crystal_top_ctr.lua sets
	// SpinEpsilonZ to 1 and SpinRandom to 1: the game's own arrangement of the
	// panels is a random spread, not the fixed angles the Spin curves name, so
	// a preview that reads only Spin draws every panel square and the crystal
	// comes out as one flat sheet.
	EpsX       []FxKey
	EpsY       []FxKey
	EpsZ       []FxKey
	SpinRandom bool
}

// RingFromNode reads a STYLE_RING node, reporting false for anything else.
func RingFromNode(n *FxNode) (Ring, bool) {
	if n.Style != "STYLE_RING" {
		return Ring{}, false
	}
	r := Ring{
		Texture:   n.String("Texture"),
		Blending:  int(n.Float("Blending", 2)),
		Lifetime:  n.Float("Duration", 1),
		billboard: n.Bool("Billboard", false),
		Loop:      n.Bool("Loop", true),
		SelfLit:   n.Bool("SelfIlluminated", false),
	}
	r.Radius = beamCurve(n, "Radius")
	r.spinX = beamCurve(n, "SpinX")
	r.SpinY = beamCurve(n, "SpinY")
	r.SpinZ = beamCurve(n, "SpinZ")
	r.EpsX = spinCurve(n, "SpinEpsilonX")
	r.EpsY = spinCurve(n, "SpinEpsilonY")
	r.EpsZ = spinCurve(n, "SpinEpsilonZ")
	r.SpinRandom = n.Bool("SpinRandom", false)
	if p, ok := n.Prop("Colour"); ok {
		r.Colour = p.Colour
	}
	if p, ok := n.Prop("Offset"); ok {
		r.Offset = p.Vec3
	}
	if r.Lifetime <= 0 {
		r.Lifetime = 1
	}
	if r.Radius == nil {
		r.Radius = []FxKey{{T: 0, V: fxRingDefaultRadius}, {T: 1, V: fxRingDefaultRadius}}
	}
	return r, true
}

// fxRingDefaultRadius is the size a ring with no Radius curve is drawn at.
const fxRingDefaultRadius = 0.1

// spread is panel index i's share of the node's SpinEpsilon spread, in 0..1.
//
// It is derived from the index rather than drawn at random, so a panel keeps
// the same face from one frame to the next: an effect re-rolled its angles
// every frame would flicker, and the crystal's faces would never settle. The
// node's SpinRandom is what asks for the spread to be scattered rather than
// laid out in order, and the game's own crystal panels set it, so both are
// honoured — randomly, but from a sequence fixed at build time rather than per
// frame.
func (r Ring) Spread(i int) float64 {
	if len(r.EpsX) == 0 && len(r.EpsY) == 0 && len(r.EpsZ) == 0 {
		return 0
	}
	if r.SpinRandom {
		return fxRingRandomShare(i)
	}
	// Evenly spaced is the closest thing to what the game shows for a set of
	// panels that together cover a shape: each one takes the next slice.
	return float64(i%RingSpreadBuckets) / float64(RingSpreadBuckets-1)
}

// RingSpreadBuckets is how many distinct faces an ordered spread hands out
// before it repeats. Eight is enough for a crystal and small enough that two
// neighbouring panels never land on the same angle.
const RingSpreadBuckets = 8

// fxRingRandomShare scatters a panel index over 0..1. It is a hash rather than
// a running generator so that the answer depends only on the index: the same
// panel gets the same face on every frame and every replay of the effect.
func fxRingRandomShare(i int) float64 {
	// An integer hash mixed and folded into the unit interval. The constants
	// are odd so that consecutive indices land far apart, which is what keeps
	// two neighbouring panels from coming out nearly parallel.
	h := uint64(i+1) * 0x9E3779B97F4A7C15
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31
	// The top 24 bits are the most mixed, and 2^24 divides exactly into the
	// unit interval, so this covers 0..1 without a rounding step at the top.
	return float64(h>>40) / float64(1<<24)
}

// RingRibbon samples a ring at age u into the quad it draws. centre is where
// the ring hangs, right and up the first two axes of the marker's own frame -
// the frame its Offset is written in - side the third, and camUp the camera's
// own up, which is what a billboard ring is squared to.
//
// A ring that keeps its own plane lays its map out the way the marker reads:
// the map's upright axis along the marker's right, which is the axis its model
// points up the crystal, and its width along the third, which runs around the
// crystal. The crystal's facets are the case that fixes it. Its eight lower
// panels stand on eight markers the model has already laid out around the gem,
// and each marker's own axes are up, outward and sideways: put the map's
// upright axis anywhere but up and every panel turns into a fin sticking out
// sideways, which is a crown of blades and not a gem.
//
// spread is this panel's share of its SpinEpsilon spread, 0..1: the ring does
// not pick one itself, so a panel keeps the face it was given for as long as
// it is on screen.
func RingRibbon(r Ring, centre, axX, axY, axZ fauxgl.Vector, camUp fauxgl.Vector, u, spread float64) BeamRibbon {
	half := SampleCurve(r.Radius, u)
	out := BeamRibbon{
		Texture:   r.Texture,
		Width:     half,
		Segments:  1,
		Additive:  r.Blending == 2,
		Alpha:     1,
		U0:        0,
		U1:        1,
		SelfLit:   r.SelfLit,
		Billboard: r.billboard,
	}
	if st := sampleColour(r.Colour, u); st != nil {
		out.R, out.G, out.B, out.Alpha = st.R, st.G, st.B, st.A
	}
	// The Offset is in the marker's own frame, which is the whole of how a
	// ring knows to sit at the end of a barrel rather than at its base. It is
	// measured across the second and third of the marker's axes, which are
	// the two columns MarkerBasis hands out.
	if o, ok := vec3Sample(r.Offset, u); ok {
		centre = centre.
			Add(axY.MulScalar(o.X)).
			Add(axZ.MulScalar(o.Y)).
			Add(CrossOf(axY, axZ).MulScalar(o.Z))
	}
	// A billboard ring squares itself to the eye; the others keep the
	// marker's own plane, which is what a ring lying on a surface does.
	//
	// A panel in that plane takes the map's upright axis along the marker's
	// X and its width along the marker's Z. The crystal's facets are the case
	// that fixes it: their markers point X up the gem, Y outward and Z the way
	// round it, and a panel laid out any other way is a fin sticking out
	// sideways rather than a face of the gem.
	axR, axU := axX, axZ
	if r.billboard {
		axR, axU = CamRight(camUp, centre), camUp
	}
	// The Spin curves are turns over the ring's life, in the same frame. The
	// SpinEpsilon curves are added on top: a panel told to spread over a whole
	// turn is turned by its own Spin plus its share of that spread, which is
	// what separates one panel of a crystal from the next.
	//
	// spread is the panel's own share of the spread, 0..1, handed in rather
	// than drawn here so that a panel keeps the same face for as long as it is
	// on screen. A ring that re-rolled its angle every frame would shimmer.
	axR, axU = rotateInPlane(axR, axU, (SampleCurve(r.spinX, u)+SampleCurve(r.EpsX, u)*spread)*math.Pi*2)
	axR, axU = rotateInPlane(axR, axU, (SampleCurve(r.SpinY, u)+SampleCurve(r.EpsY, u)*spread)*math.Pi*2)
	axR, axU = rotateInPlane(axR, axU, (SampleCurve(r.SpinZ, u)+SampleCurve(r.EpsZ, u)*spread)*math.Pi*2)
	out.From = centre.Sub(axU.MulScalar(half))
	out.To = centre.Add(axU.MulScalar(half))
	// Set, not left to the eye: the ribbon's own rule would turn this to
	// face the camera, which is exactly what a ring lying on a surface must
	// not do.
	out.Across = axR
	out.Fixed = true
	// The map's top row goes at the marker's up end. A ring in its own plane
	// knows which way is up and the shape of its own map says so: the lower
	// facets of the crystal are drawn narrow at the top of their map and
	// widen downward, and the upper facets the other way round, so the two
	// sets meet at a waist instead of flaring apart. Hanged the other way up
	// the same panels make a barrel. A billboard ring has no up of its own to
	// obey - the eye's is the only one there is - so it is left as it falls.
	out.FlipV = !r.billboard
	return out
}

// CamRight is the camera's own right, from its up and the way the viewer
// looks. The order of the cross product matters: the camera looks along
// towardsEye, so its right is camUp x towardsEye. The other order hands back
// the camera's left, which mirrors a billboard ring's Offset and turns its
// Spin the wrong way.
func CamRight(camUp fauxgl.Vector, towardsEye fauxgl.Vector) fauxgl.Vector {
	r := camUp.Cross(towardsEye)
	if n := r.Length(); n > 1e-6 {
		return r.Normalize()
	}
	// The eye sits on the up axis, which is every billboard ring of an effect
	// played at the model's own origin while the camera looks along the
	// ground: a horizontal view has a camera up of exactly Z, and the effect's
	// centre is then the origin the two vectors are measured from.
	//
	// Falling back on the screen-space sideways built from the up vector's own
	// X and Y is zero in exactly that case, and normalizing zero is not a
	// direction. Any axis across the up will do instead, so one is chosen
	// rather than assumed: the shortest of the three that is not parallel to it.
	if n := camUp.Length(); n > 1e-6 {
		u := camUp.Normalize()
		helper := fauxgl.Vector{X: 1}
		if s := math.Abs(u.X); s > math.Abs(u.Y) {
			helper = fauxgl.Vector{Y: 1}
			if s > math.Abs(u.Z) {
				helper = fauxgl.Vector{Z: 1}
			}
		}
		if r := u.Cross(helper); r.Length() > 1e-6 {
			return r.Normalize()
		}
	}
	return fauxgl.Vector{X: 1}
}

func CrossOf(a, b fauxgl.Vector) fauxgl.Vector {
	return fauxgl.Vector{X: a.Y*b.Z - a.Z*b.Y, Y: a.Z*b.X - a.X*b.Z, Z: a.X*b.Y - a.Y*b.X}
}

// rotateInPlane turns a pair of in-plane axes about their normal by ang.
func rotateInPlane(right, up fauxgl.Vector, ang float64) (fauxgl.Vector, fauxgl.Vector) {
	if ang == 0 {
		return right, up
	}
	c, s := math.Cos(ang), math.Sin(ang)
	return right.MulScalar(c).Add(up.MulScalar(s)), up.MulScalar(c).Sub(right.MulScalar(s))
}
