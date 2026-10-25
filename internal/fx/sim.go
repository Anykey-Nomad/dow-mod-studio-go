package fx

import (
	"math"

	"github.com/fogleman/fauxgl"
)

// A full reimplementation of the game's particle system is not the point
// here: the preview needs enough of it that an effect reads as the same
// effect. Sprays emit particles along a direction at their rate, each
// particle lives as long as the leaf effect it was born as, grows along the
// Radius curve and fades along the Colour ramp, and everything loops so the
// preview plays for as long as the toggle stays on.

// MaxParticles caps a preview: the game's own rate can ask for thousands
// of particles a second, which a viewport overlay has no use for.
const MaxParticles = 400

// Visual is what one particle looks like: the leaf node it comes from.
type Visual struct {
	Texture  string
	Blending int
	Lifetime float64
	Speed    float64
	Radius   []FxKey
	Colour   []FxColourStop
	// selfLit is the node's own SelfIlluminated: a particle that carries it
	// is its own light and ignores the scene's, and one that does not is
	// dimmed by whatever light the model is drawn under. Across the shipped
	// rings the flag is split about two to one, so reading it is the
	// difference between a spark that stays bright in a dark frame and one
	// that goes out with it.
	SelfLit bool
	// scale is the leaf's size over its life, and speedCurve its launch
	// speed. Both are written per leaf, not on the emitter, which is why
	// they are read off the child and not off the spray.
	Scale      []FxKey
	SpeedCurve []FxKey
	// A non-billboard STYLE_RING particle keeps a fixed plane instead of
	// turning to the camera. PlaneNormal is its local spray axis before the
	// event marker frame maps it into model space.
	Horizontal   bool
	PlaneNormal  [3]float64
	SpinX        []FxKey
	SpinY        []FxKey // effect-file Y is the vertical axis, so this is the horizontal-plane heading
	SpinZ        []FxKey // retained for other ring orientations and legacy files
	SpinEpsilonX []FxKey
	SpinEpsilonY []FxKey
	SpinEpsilonZ []FxKey
	SpinRandom   bool
}

// Emitter is one STYLE_SPRAY node with its runtime counters.
type Emitter struct {
	Rate      float64 // particles per second, the first key of Emitter_Rate
	RateCurve []FxKey
	Duration  float64
	Loop      bool
	Drag      float64
	Dev       float64 // cone half angle in radians
	Visuals   []Visual
	// volume is the half extents of the box the particles are born inside,
	// and offset the centre of that box in the emitter's own frame. The
	// files write them as vector curves, so they are sampled like any other
	// animation property rather than taken as constants.
	Volume []FxVec3Key
	Offset []FxVec3Key
	// speed is the curve a particle's launch speed follows: a spray ramps
	// its speed down over its life, which is most of what makes a cloud
	// spread and then hang.
	Speed []FxKey
	// scale is the leaf's own size multiplier over its life.
	Scale   []FxKey
	Gravity float64

	// owned are the paths of the nodes this emitter took as its particles, so
	// that the walk knows to step over them: a leaf under a spray is the look
	// of the puff the emitter births, not a shape standing on its own.
	Owned map[string]bool

	Age        float64 // seconds inside the current emission cycle
	Acc        float64 // fractional particle counter
	Cycle      int
	SpawnIndex int // stable id for particle-local random properties
}

// owns reports whether this emitter took n as one of its particles.
func (e *Emitter) owns(n *FxNode) bool {
	return e.Owned != nil && e.Owned[n.Effect]
}

// claim records that this emitter took n as one of its particles.
func (e *Emitter) claim(n *FxNode) {
	if e.Owned == nil {
		e.Owned = map[string]bool{}
	}
	e.Owned[n.Effect] = true
}

// Particle is one particle in flight, positioned relative to the marker
// the effect plays at.
type Particle struct {
	V        Visual
	Drag     float64
	grav     float64
	Pos      [3]float64
	Vel      [3]float64
	Age      float64
	SpriteID int // persists while older particles are removed from the slice
}

// FxSim plays one effect file forever. Positions are in model units,
// relative to the effect's origin (a marker, or the model centre).
type FxSim struct {
	Emitters []Emitter
	// NoRotate is the root FX node's No_Rotate flag. When set, the effect
	// keeps the FX file's own frame instead of inheriting its model marker's
	// orientation.
	NoRotate bool
	// Axis is the authored emission direction in the effect's own Y-up
	// frame; the marker frame carries it into model space.
	Axis      fauxgl.Vector
	Anchored  []Visual // leaf effects that hang at the origin on their own
	BeamNodes []Beam   // STYLE_BEAM nodes, drawn between the event's markers
	RingNodes []Ring   // STYLE_RING nodes: a flat quad in the marker's frame
	Particles []Particle
	Time      float64
	// The three lists below are rebuilt every frame and read once by the
	// renderer, so they are written into buffers kept here rather than into
	// fresh ones. A model with a dozen effects playing draws a few thousand
	// sprites and ribbons a second, and that is enough garbage to keep the
	// collector running through an animation - which is where a preview that
	// is otherwise smooth stutters.
	//
	// The slice a call hands back is only good until the next call of that
	// kind on the same simulation, which is what every caller wants: each
	// reads the frame's geometry and draws it before asking for anything
	// else of the same list.
	spriteBuf []Sprite
	beamBuf   []BeamRibbon
	ringBuf   []BeamRibbon
}

// NewFxSim walks a parsed file and prepares its emitters and anchored
// effects. Emitters sprays are read with their leaves; leaves under a spray
// belong to its particles, not to the world.
func NewFxSim(f *FxFile) *FxSim {
	s := &FxSim{}
	if f == nil {
		return s
	}
	root := f.Root()
	if root == nil {
		return s
	}
	s.NoRotate = root.Bool("No_Rotate", false)
	// The root emitter's authored direction is the spray's local axis, taken
	// as the file writes it: every other local vector below is read in the
	// same Y-up frame, and the marker frame (or fxFallbackFrame when the
	// event names none) is what turns the lot into model space.
	if p, ok := root.Prop("Emitter_Direction"); ok && p.Tuple.Length() > 1e-6 {
		s.Axis = p.Tuple.Normalize()
	}
	// add reads one node into the simulation, recursing through the combos
	// that hold others.
	var add func(n *FxNode)
	var walk func(path string)
	add = func(n *FxNode) {
		switch n.Style {
		case "STYLE_SPRAY":
			e := emitterFromNode(n, f)
			s.Emitters = append(s.Emitters, e)
			// A spray can hold a subtree of its own, and it is not always
			// only leaves: aspect_lightning_prod is a spray whose child is a
			// combo whose own child is a STYLE_BEAM. Stopping at the spray
			// meant that beam was parsed, reachable by name, and then never
			// collected — the file listed its effect and drew no beam.
			//
			// What the emitter already took as a particle is not a shape in
			// its own right: a leaf under a spray describes the puff the
			// emitter births, and collecting it again as a ring would draw
			// that puff twice, once moving and once standing still. Only what
			// the emitter left alone is walked on into.
			for _, c := range f.Children(n.Effect) {
				if !e.owns(c) {
					add(c)
				}
			}
		case "STYLE_COMBO":
			walk(n.Effect)
		default:
			if b, ok := beamFromNode(n); ok {
				s.BeamNodes = append(s.BeamNodes, b)
				return
			}
			if r, ok := RingFromNode(n); ok {
				s.RingNodes = append(s.RingNodes, r)
				return
			}
			if v, ok := VisualFromNode(n); ok {
				s.Anchored = append(s.Anchored, v)
			}
		}
	}
	walk = func(path string) {
		for _, n := range f.Children(path) {
			add(n)
		}
	}
	// A file written as one node with no path of its own: that node is the
	// whole effect, and there is no child of "root" to find it under.
	if f.Single {
		add(root)
		return s
	}
	walk(root.Effect)
	// A file whose root is itself an emitter (no children wrapper).
	if len(s.Emitters) == 0 && len(s.Anchored) == 0 && root.Style == "STYLE_SPRAY" {
		s.Emitters = append(s.Emitters, emitterFromNode(root, f))
	}
	return s
}

// emitterFromNode reads a STYLE_SPRAY node and the leaf visuals it sprays.
func emitterFromNode(n *FxNode, f *FxFile) Emitter {
	e := Emitter{
		Rate:     firstKey(n, "Emitter_Rate"),
		Duration: n.Float("Emitter_Duration", 1),
		Loop:     n.Bool("Emitter_Loop", true),
		Drag:     n.Float("Emitter_Drag", 1),
		Gravity:  n.Float("Particle_Gravity", 0),
	}
	e.RateCurve = fxEmitterCurve(n, "Emitter_Rate")
	// The FX editor's deviation is the half angle of the emission cone in
	// degrees; files write everything from 0 (a jet) to 180 (a burst). It is
	// written as a curve, so the first key is the angle it starts at.
	dev := firstKey(n, "Emitter_Deviation")
	if dev == 0 {
		dev = n.Float("Emitter_Deviation", 8)
	}
	e.Dev = dev * math.Pi / 180
	e.Volume = fxVec3CurveOf(n, "Emitter_Volume")
	e.Offset = fxVec3CurveOf(n, "Emitter_Offset")
	// A spray describes its particles on itself, not on the leaf: the leaf is
	// only the look of one puff. Where the leaf carries no curve of its own
	// the emitter's is the one that applies, so both are read here and the
	// leaf inherits them below.
	e.Scale = fxEmitterCurve(n, "Particle_Scale")
	e.Speed = fxEmitterCurve(n, "Particle_Speed")
	for _, c := range f.Children(n.Effect) {
		if v, ok := particleFromNode(c); ok {
			v.Scale = fxEmitterCurve(c, "Particle_Scale")
			v.SpeedCurve = fxEmitterCurve(c, "Particle_Speed")
			e.adopt(&v)
			e.Visuals = append(e.Visuals, v)
			e.claim(c)
		} else if c.Style == "STYLE_COMBO" {
			for _, cc := range f.Children(c.Effect) {
				if v, ok := particleFromNode(cc); ok {
					v.Scale = fxEmitterCurve(cc, "Particle_Scale")
					v.SpeedCurve = fxEmitterCurve(cc, "Particle_Speed")
					e.adopt(&v)
					e.Visuals = append(e.Visuals, v)
					e.claim(cc)
				}
			}
		}
	}
	return e
}

// adopt fills in the leaf's launch speed from the emitter's curve when the
// leaf carries none of its own. Particle_Speed is written on the spray, so a
// leaf that only carries a radius would otherwise fly at the built-in default
// instead of at the speed the file drew.
func (e *Emitter) adopt(v *Visual) {
	if len(v.SpeedCurve) == 0 {
		v.SpeedCurve = e.Speed
	}
	if len(v.Scale) == 0 {
		v.Scale = e.Scale
	}
	if len(v.SpeedCurve) > 0 {
		v.Speed = SampleCurve(v.SpeedCurve, 0)
	}
}

// fxEmitterCurve reads an emitter or particle animation property, falling back
// to a constant curve so the sampler always has something to work with.
func fxEmitterCurve(n *FxNode, name string) []FxKey {
	p, ok := n.Prop(name)
	if !ok || len(p.Curve) == 0 {
		return nil
	}
	if len(p.Curve) > fxBeamCurveMax {
		return p.Curve[:fxBeamCurveMax]
	}
	return p.Curve
}

// fxVec3CurveOf reads a vector property of an emitter, which the files write
// as a time/vector curve just like a beam's Arc.
func fxVec3CurveOf(n *FxNode, name string) []FxVec3Key {
	p, ok := n.Prop(name)
	if !ok {
		return nil
	}
	return p.Vec3
}

// fxEmitterPhase maps emitter age to the 0..1 domain used by its authored
// rate, volume, and offset curves.
func fxEmitterPhase(e *Emitter) float64 {
	if e == nil || e.Duration <= 0 {
		return 0
	}
	return clamp(e.Age/e.Duration, 0, 1)
}

// VisualFromNode reads the properties a sprite needs.
//
// A node's style says what shape it is on its own, but under a STYLE_SPRAY it
// says nothing at all: every child of an emitter is a description of the
// particles that emitter births, and the commonest of them in the game is a
// STYLE_RING — the ring of smoke a burst expands into. Refusing rings here
// leaves those emitters with nothing to emit, and their effect plays nowhere
// at all rather than in the wrong place.
//
// The shapes that are never particles are the ones that need something the
// emitter cannot give them: a spray and a combo are containers, not looks.
//
// A beam is a different case, and it is why this function refuses it while
// particleFromNode below accepts it: a beam hanging at an effect's root is
// drawn as a ribbon between the two markers its event names, and nothing else
// can draw it. The same beam under a spray is not that ribbon at all — see
// particleFromNode.
func VisualFromNode(n *FxNode) (Visual, bool) {
	if n.Style == "STYLE_SPRAY" || n.Style == "STYLE_COMBO" || n.Style == "STYLE_BEAM" {
		return Visual{}, false
	}
	v := Visual{
		Texture:      n.String("Texture"),
		Blending:     int(n.Float("Blending", 2)),
		Lifetime:     n.Float("Duration", 1),
		Speed:        n.Float("Particle_Speed", 2),
		SelfLit:      n.Bool("SelfIlluminated", false),
		Horizontal:   n.Style == "STYLE_RING" && !n.Bool("Billboard", false),
		SpinX:        spinCurve(n, "SpinX"),
		SpinY:        spinCurve(n, "SpinY"),
		SpinZ:        spinCurve(n, "SpinZ"),
		SpinEpsilonX: spinCurve(n, "SpinEpsilonX"),
		SpinEpsilonY: spinCurve(n, "SpinEpsilonY"),
		SpinEpsilonZ: spinCurve(n, "SpinEpsilonZ"),
		SpinRandom:   n.Bool("SpinRandom", false),
	}
	if p, ok := n.Prop("Radius"); ok {
		v.Radius = p.Curve
	}
	if p, ok := n.Prop("Colour"); ok {
		v.Colour = p.Colour
	}
	if v.Lifetime <= 0 {
		v.Lifetime = 1
	}
	return v, true
}

// particleFromNode reads what one particle of an emitter looks like, which is
// every leaf a spray has — including a STYLE_BEAM.
//
// Under a spray a beam is not a ribbon. Nothing names a second marker there,
// so there is nothing for a beam to span: what the engine actually births is a
// short glowing segment per particle, and all it still means by its own
// properties is how thick, how long, how bright and how long-lived that segment
// is. Those are the four numbers a billboard sprite needs as well, so the beam
// is read as a sprite of its Width and the Length, Segments, Arc and the
// marker pair are left behind: they describe the ribbon this node would have
// been at the root, which is not what is being drawn.
//
// This is what 64 of the 459 shipped emitters ask for — every one of their
// leaves is a beam — and reading none of them left those effects emitting
// nothing at all.
func particleFromNode(n *FxNode) (Visual, bool) {
	if n.Style != "STYLE_BEAM" {
		return VisualFromNode(n)
	}
	v := Visual{
		Texture:  n.String("Texture"),
		Blending: int(n.Float("Blending", 2)),
		Lifetime: n.Float("Duration", 1),
		// A beam node carries no SelfIlluminated at all — none of the 54
		// shipped ones does — so a beam read as a particle is a plain lit
		// one, the same as any other leaf of its emitter.
		SelfLit: n.Bool("SelfIlluminated", false),
	}
	// Width is the ribbon's half width, which is the same thing a sprite's
	// radius is: how far from the particle's own axis the glow reaches.
	v.Radius = beamCurve(n, "Width")
	if p, ok := n.Prop("Colour"); ok {
		v.Colour = p.Colour
	}
	if v.Lifetime <= 0 {
		v.Lifetime = 1
	}
	return v, true
}

// firstKey is the starting value of a curve — the rate a spray begins at.
func firstKey(n *FxNode, name string) float64 {
	p, ok := n.Prop(name)
	if !ok || len(p.Curve) == 0 {
		return 0
	}
	return p.Curve[0].V
}

// Reset empties the simulation so an effect starts again from nothing. It is
// what a switch of animation does to the effects its new clip fires: the
// game plays a timeline's entry events on the frame the clip starts, so a
// glow that had been running for a minute would not appear when it is asked
// for again.
func (s *FxSim) Reset() {
	s.Particles = nil
	s.Time = 0
	for i := range s.Emitters {
		s.Emitters[i].Age = 0
		s.Emitters[i].Acc = 0
		s.Emitters[i].Cycle = 0
		s.Emitters[i].SpawnIndex = 0
	}
}

// Step advances the simulation by dt seconds.
func (s *FxSim) Step(dt float64) {
	if dt <= 0 {
		return
	}
	if dt > 0.1 {
		dt = 0.1 // a stall must not teleport particles
	}
	s.Time += dt
	for ei := range s.Emitters {
		e := &s.Emitters[ei]
		if len(e.Visuals) == 0 || (e.Rate <= 0 && len(e.RateCurve) == 0) {
			continue
		}
		e.Age += dt
		if e.Duration > 0 && e.Age >= e.Duration {
			if !e.Loop {
				continue
			}
			e.Age = math.Mod(e.Age, e.Duration)
			e.Cycle++
			e.Acc = 0
		}
		phase := fxEmitterPhase(e)
		rate := e.Rate
		if len(e.RateCurve) > 0 {
			rate = SampleCurve(e.RateCurve, phase)
		}
		if rate <= 0 {
			continue
		}
		e.Acc += rate * dt
		for e.Acc >= 1 {
			e.Acc--
			if len(s.Particles) >= MaxParticles {
				break
			}
			// Emission follows the emitter's animated position. Sampling at
			// each simulation step preserves the authored vertical trail/drop
			// instead of pinning every particle to the offset at t=0.
			s.Spawn(e)
		}
	}
	kept := s.Particles[:0]
	for i := range s.Particles {
		p := &s.Particles[i]
		p.Age += dt
		if p.Age >= p.V.Lifetime {
			continue
		}
		damp := math.Pow(clamp(p.Drag, 0.01, 1), dt)
		p.Vel[0] *= damp
		p.Vel[1] *= damp
		p.Vel[2] *= damp
		// Particles live in the effect file's own Y-up frame, the same frame
		// its Volume and Offset are written in, so gravity pulls on Y here
		// and the attachment frame tilts it with the marker. Particle_Gravity
		// is an acceleration in model units per second: across the shipped
		// effects it is either 0 or exactly 1, and a unit a second is gentle
		// next to the launch speeds of several units a second the same files
		// write. Reading it as a per frame step made a particle fall sixty
		// times too fast, which is what put a plasma generator's core glow
		// several body lengths below the building instead of inside it.
		p.Vel[1] -= p.grav * dt
		p.Pos[0] += p.Vel[0] * dt
		p.Pos[1] += p.Vel[1] * dt
		p.Pos[2] += p.Vel[2] * dt
		kept = append(kept, *p)
	}
	s.Particles = kept
}

// spawn births one particle inside the emitter's authored direction cone.
func (s *FxSim) Spawn(e *Emitter) {
	spriteID := e.SpawnIndex
	e.SpawnIndex++
	v := e.Visuals[(e.Cycle+spriteID)%len(e.Visuals)]
	// Deterministic pseudo randoms: a preview that differs every run is
	// harder to compare, and seeding costs nothing.
	rnd := func(salt, mask uint32) float64 {
		h := uint32(spriteID)*salt + uint32(e.Cycle)*22695477
		return float64((h>>8)&mask) / float64(mask)
	}
	ang := rnd(1103515245, 0xffff) * 2 * math.Pi
	tilt := e.Dev * rnd(104729, 0xff)
	dirX := math.Sin(tilt) * math.Cos(ang)
	dirY := math.Sin(tilt) * math.Sin(ang)
	dirZ := math.Cos(tilt)
	axis := s.Axis
	if axis.Length() < 1e-6 {
		// The effects are authored Y up, so a spray that names no direction
		// of its own emits up that axis, which the frame turns into the
		// model's own up.
		axis = fauxgl.Vector{Y: 1}
	}
	axis = axis.Normalize()
	help := fauxgl.Vector{Z: 1}
	if math.Abs(axis.Z) > 0.95 {
		help = fauxgl.Vector{Y: 1}
	}
	right := help.Cross(axis).Normalize()
	up := axis.Cross(right).Normalize()
	direction := right.MulScalar(dirX).Add(up.MulScalar(dirY)).Add(axis.MulScalar(dirZ))
	// A particle is born inside the emitter's box, not at its centre: a
	// ground cloud spread over Emitter_Volume is the whole difference
	// between a puff and a volume of dust. Both vectors stay in the file's
	// Y-up frame; the attachment frame is what lays them into the model, so
	// converting them here as well would swap the vertical onto a
	// horizontal axis and make a falling ring drift sideways.
	phase := fxEmitterPhase(e)
	vol, volOK := vec3Sample(e.Volume, phase)
	off, offOK := vec3Sample(e.Offset, phase)
	var px, py, pz float64
	if volOK {
		px += (rnd(22695477, 0xffff)*2 - 1) * vol.X
		py += (rnd(374761393, 0xffff)*2 - 1) * vol.Y
		pz += (rnd(668265263, 0xffff)*2 - 1) * vol.Z
	}
	if offOK {
		px += off.X
		py += off.Y
		pz += off.Z
	}
	// The launch speed follows the leaf's own curve over its life, so a
	// burst that starts fast and slows hangs where the file says it does.
	speed := v.Speed
	if len(v.SpeedCurve) > 0 {
		speed = SampleCurve(v.SpeedCurve, 0)
	}
	s.Particles = append(s.Particles, Particle{
		V:    v,
		Drag: e.Drag,
		grav: e.Gravity,
		Pos:  [3]float64{px, py, pz}, Vel: [3]float64{direction.X * speed, direction.Y * speed, direction.Z * speed},
		SpriteID: spriteID,
	})

}

// Sprite is one thing to draw this frame: a billboarded disc at a position
// in the effect's own Y-up frame, with the size in model units. The marker
// frame the effect is attached to maps the position into model space.
type Sprite struct {
	Pos      [3]float64
	Radius   float64
	R, G, B  float64
	A        float64
	Texture  string
	Additive bool
	// SelfLit is the leaf's own SelfIlluminated, carried to the renderer so
	// it knows whether this particle answers to the scene's light.
	SelfLit bool
	// Horizontal keeps the quad in a fixed plane instead of facing the
	// camera. PlaneNormal is its model-space normal (zero means world +Z);
	// Rotation is the in-plane angle in radians.
	Horizontal  bool
	PlaneNormal [3]float64
	Rotation    float64
}

// Sprites returns the current frame of the effect: every live particle plus
// the anchored effects, which loop on the simulation clock.
func (s *FxSim) Sprites() []Sprite {
	out := s.spriteBuf[:0]
	if cap(out) < len(s.Particles)+len(s.Anchored) {
		out = make([]Sprite, 0, len(s.Particles)+len(s.Anchored))
	}
	for i := range s.Particles {
		p := &s.Particles[i]
		u := p.Age / p.V.Lifetime
		sp := spriteFor(p.V, u, p.Pos)
		sp.Horizontal = p.V.Horizontal
		if sp.Horizontal {
			sp.PlaneNormal = p.V.PlaneNormal
			sp.Rotation = SpriteSpin(p.V, u, p.SpriteID)
		}
		out = append(out, sp)
	}
	for i, v := range s.Anchored {
		u := math.Mod(s.Time+float64(i)*0.37, v.Lifetime) / v.Lifetime
		sp := spriteFor(v, u, [3]float64{})
		sp.Horizontal = v.Horizontal
		if sp.Horizontal {
			sp.PlaneNormal = v.PlaneNormal
			sp.Rotation = SpriteSpin(v, u, i)
		}
		out = append(out, sp)
	}
	s.spriteBuf = out
	return out
}

// SpriteSpin returns a ring particle's stable in-plane turn. The effect
// files are Y-up: for a horizontal, non-billboard ring its heading is SpinY,
// which becomes rotation about world Z after the Y/Z axis conversion. Other
// ring orientations retain the legacy SpinZ interpretation. SpinEpsilon is
// kept as a per-particle in-plane spread; it never tilts the fixed horizontal
// plane, even when the author stored the spread on another axis. The epsilon
// is shared across particles in order or with the file's deterministic random
// spread.
func SpriteSpin(v Visual, age float64, index int) float64 {
	turns := SampleCurve(v.SpinX, age) + SampleCurve(v.SpinY, age) + SampleCurve(v.SpinZ, age)
	share := float64(index%RingSpreadBuckets) / float64(RingSpreadBuckets-1)
	if v.SpinRandom {
		share = fxRingRandomShare(index)
	}
	turns += (SampleCurve(v.SpinEpsilonX, age) + SampleCurve(v.SpinEpsilonY, age) + SampleCurve(v.SpinEpsilonZ, age)) * share
	return turns * 2 * math.Pi
}

// Beams returns the ribbons the effect draws this frame, sampled at the
// endpoints the event named. A beam with no partner to span has nowhere to go,
// so it is left out rather than drawn at a length of zero.
//
// The age runs off the simulation clock the way the anchored leaves do: these
// beams loop, so the colour ramp and the scroll repeat for as long as the
// preview is on.
func (s *FxSim) Beams(from, to fauxgl.Vector, spans bool) []BeamRibbon {
	if len(s.BeamNodes) == 0 {
		return nil
	}
	out := s.beamBuf[:0]
	if cap(out) < len(s.BeamNodes) {
		out = make([]BeamRibbon, 0, len(s.BeamNodes))
	}
	for i := range s.BeamNodes {
		b := &s.BeamNodes[i]
		u := math.Mod(s.Time+float64(i)*0.37, b.Lifetime) / b.Lifetime
		out = append(out, NewBeamRibbon(*b, from, to, u, spans))
	}
	s.beamBuf = out
	return out
}

// spriteFor samples the curves of one visual at normalised age u.
func spriteFor(v Visual, u float64, pos [3]float64) Sprite {
	sp := Sprite{
		Pos:      pos,
		Radius:   SampleCurve(v.Radius, u),
		Texture:  v.Texture,
		Additive: v.Blending == 2,
		SelfLit:  v.SelfLit,
		A:        1,
	}
	// Particle_Scale is the leaf's own size over its life, on top of the
	// Radius curve: a puff that grows fast and then thins is written that
	// way rather than through the radius.
	if len(v.Scale) > 0 {
		sp.Radius *= SampleCurve(v.Scale, u)
	}
	if st := sampleColour(v.Colour, u); st != nil {
		sp.R, sp.G, sp.B, sp.A = st.R, st.G, st.B, st.A
	}
	if !sp.Additive && sp.A <= 0 {
		sp.A = 1
	}
	return sp
}

// SampleCurve samples a (time, value) curve at normalised age. The time
// axis of the files is whatever the author drew — seconds of a 2.5s effect
// or a 0..1 ramp — so it is rescaled to the curve's own span.
func SampleCurve(keys []FxKey, u float64) float64 {
	if len(keys) == 0 {
		return 0
	}
	if len(keys) == 1 {
		return keys[0].V
	}
	span := keys[len(keys)-1].T - keys[0].T
	if span <= 0 {
		return keys[0].V
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
				return b.V
			}
			f := (t - a.T) / (b.T - a.T)
			return a.V + f*(b.V-a.V)
		}
	}
	return keys[len(keys)-1].V
}

// sampleColour samples a colour ramp the same way.
func sampleColour(stops []FxColourStop, u float64) *FxColourStop {
	if len(stops) == 0 {
		return nil
	}
	if len(stops) == 1 {
		return &stops[0]
	}
	span := stops[len(stops)-1].T - stops[0].T
	if span <= 0 {
		return &stops[0]
	}
	if u < 0 {
		u = 0
	}
	if u > 1 {
		u = 1
	}
	t := stops[0].T + u*span
	for i := 1; i < len(stops); i++ {
		if t <= stops[i].T {
			a, b := stops[i-1], stops[i]
			if b.T <= a.T {
				return &stops[i]
			}
			f := (t - a.T) / (b.T - a.T)
			return &FxColourStop{
				T: t,
				R: a.R + f*(b.R-a.R),
				G: a.G + f*(b.G-a.G),
				B: a.B + f*(b.B-a.B),
				A: a.A + f*(b.A-a.A),
			}
		}
	}
	return &stops[len(stops)-1]
}

// Rings returns the quads the effect's rings draw this frame, sampled into
// the frame they sit in. centre is the marker's position and axX, axY and axZ
// the marker's own three axes: a ring that keeps its own plane is laid out in
// them, and its Offset is measured across the second and third. camUp is the
// camera's own up, which is what a billboard ring squares itself to.
func (s *FxSim) Rings(centre, axX, axY, axZ, camUp fauxgl.Vector) []BeamRibbon {
	if len(s.RingNodes) == 0 {
		return nil
	}
	out := s.ringBuf[:0]
	if cap(out) < len(s.RingNodes) {
		out = make([]BeamRibbon, 0, len(s.RingNodes))
	}
	// The SpinEpsilon curves scatter the copies of one panel over the turn
	// they name, so there is only something to scatter when the file holds
	// more than one ring. The crystal is the case that shows why: each of
	// its panels is a file of its own with one ring in it, spun by eight
	// markers the model has already laid out around the gem, and a spread
	// handed to a lone panel turns it by an arbitrary angle that no marker
	// asked for - eight panels at eight unrelated tilts instead of one
	// upright crystal.
	spread := len(s.RingNodes) > 1
	for i := range s.RingNodes {
		r := &s.RingNodes[i]
		u := math.Mod(s.Time+float64(i)*0.37, r.Lifetime) / r.Lifetime
		share := 0.0
		if spread {
			share = r.Spread(i)
		}
		out = append(out, RingRibbon(*r, centre, axX, axY, axZ, camUp, u, share))
	}
	s.ringBuf = out
	return out
}
